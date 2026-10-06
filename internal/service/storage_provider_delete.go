package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	storagedomain "aegis/internal/domain/storage"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/minio/minio-go/v7"
	"github.com/qiniu/go-sdk/v7/auth/qbox"
	qiniustorage "github.com/qiniu/go-sdk/v7/storage"
)

// 物理删除对象 —— storageProvider 的**可选**能力。
//
// 存储中心原本只有「索引表软删 / 永久删除索引行」，对象本身从不离开存储桶：
// 那对人工管理的文件是安全的默认值（误删可以从桶里捞回来）。用户云存储不一样，
// 每次写入都会产生一个新对象、超出保留数的旧修订要被裁掉，只删索引不删对象
// 等于让存储桶随写入次数无限增长，而配额账面上却显示「已释放」。
//
// 做成可选接口而不是加进 storageProvider：已有的十一家实现与它们的测试替身都不必改，
// 不支持删除的提供商返回 errStorageDeleteUnsupported，调用方据此把对象留在桶里并记日志。
//
// 所有实现都是**幂等**的：对象本来就不在算成功。清除是「尽力而为」的收尾动作，
// 重试一次已经删掉的对象不该变成一条告警。
type storageObjectDeleter interface {
	Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error
}

// errStorageDeleteUnsupported 该提供商不支持物理删除。
var errStorageDeleteUnsupported = errors.New("storage provider does not support delete")

// deleteStoredObject 通过提供商删除对象；不支持删除时返回 errStorageDeleteUnsupported。
func deleteStoredObject(ctx context.Context, provider storageProvider, cfg *storagedomain.Config, objectKey string) error {
	deleter, ok := provider.(storageObjectDeleter)
	if !ok {
		return errStorageDeleteUnsupported
	}
	return deleter.Delete(ctx, cfg, objectKey)
}

// Delete 熔断包装同样转发删除；内层不支持时原样报不支持，不计入熔断失败。
func (p *circuitBreakeredStorageProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	deleter, ok := p.upstream.(storageObjectDeleter)
	if !ok {
		return errStorageDeleteUnsupported
	}
	breakerName := circuitbreaker.Name(p.name, "delete")
	_, err := resilience.Execute(ctx, breakerName, storageResilienceOptions("delete"), func(callCtx context.Context) (struct{}, error) {
		return struct{}{}, deleter.Delete(callCtx, cfg, objectKey)
	})
	if err != nil {
		return classifyStorageBreakerError(p.upstream.Name(), err)
	}
	return nil
}

func (p *s3StorageProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return err
	}
	// S3 的 DeleteObject 对不存在的键同样回 204，天然幂等。
	_, err = client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &raw.Bucket, Key: &objectKey})
	return err
}

func (p *minioProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	client, raw, err := p.client(cfg)
	if err != nil {
		return err
	}
	return client.RemoveObject(ctx, raw.Bucket, objectKey, minio.RemoveObjectOptions{})
}

func (p *aliyunOSSProvider) Delete(_ context.Context, cfg *storagedomain.Config, objectKey string) error {
	bucket, _, err := p.bucket(cfg)
	if err != nil {
		return err
	}
	return bucket.DeleteObject(objectKey)
}

func (p *tencentCOSProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	client, _, err := p.client(cfg)
	if err != nil {
		return err
	}
	_, err = client.Object.Delete(ctx, objectKey)
	return err
}

func (p *qiniuKodoProvider) Delete(_ context.Context, cfg *storagedomain.Config, objectKey string) error {
	raw, err := decodeQiniuKodoConfig(cfg.ConfigData)
	if err != nil {
		return err
	}
	manager := qiniustorage.NewBucketManager(qbox.NewMac(raw.AccessKey, raw.SecretKey), &qiniustorage.Config{
		Zone:     qiniuZone(raw.Region),
		UseHTTPS: raw.UseHTTPS,
	})
	if err := manager.Delete(raw.Bucket, objectKey); err != nil {
		// 612：目标资源不存在。
		if strings.Contains(err.Error(), "612") || strings.Contains(strings.ToLower(err.Error()), "no such file") {
			return nil
		}
		return err
	}
	return nil
}

func (p *webDAVProvider) Delete(_ context.Context, cfg *storagedomain.Config, objectKey string) error {
	client, _, err := p.client(cfg)
	if err != nil {
		return err
	}
	// gowebdav 的 Remove 把 404 视为成功。
	return client.Remove("/" + objectKey)
}

func (p *localStorageProvider) Delete(_ context.Context, cfg *storagedomain.Config, objectKey string) error {
	localCfg, err := decodeLocalConfig(cfg.ConfigData)
	if err != nil {
		return err
	}
	absPath, err := secureLocalStoragePath(localCfg.RootDir, objectKey)
	if err != nil {
		return err
	}
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除文件失败: %w", err)
	}
	return nil
}

func (p *azureBlobProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	client, raw, err := p.client(cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteBlob(ctx, raw.Container, strings.Trim(strings.TrimSpace(objectKey), "/"), nil)
	if err != nil && bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil
	}
	return err
}

func (p *oneDriveProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	raw, err := decodeOneDriveConfig(cfg.ConfigData)
	if err != nil {
		return err
	}
	req, err := p.graphRequest(ctx, cfg, http.MethodDelete, p.itemURL(raw, objectKey), nil, nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode < http.StatusBadRequest {
		return nil
	}
	return fmt.Errorf("onedrive delete status=%d", resp.StatusCode)
}

func (p *dropboxProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	resp, err := p.apiJSON(ctx, cfg, "https://api.dropboxapi.com/2/files/delete_v2", map[string]any{
		"path": "/" + strings.Trim(strings.TrimSpace(objectKey), "/"),
	})
	if err != nil {
		return err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode < http.StatusBadRequest {
		return nil
	}
	// 409 + path_lookup/not_found：对象本来就不在。其余 409 仍按失败处理。
	if resp.StatusCode == http.StatusConflict {
		var body [512]byte
		n, _ := resp.Body.Read(body[:])
		if strings.Contains(string(body[:n]), "not_found") {
			return nil
		}
	}
	return fmt.Errorf("dropbox delete status=%d", resp.StatusCode)
}

func (p *googleDriveProvider) Delete(ctx context.Context, cfg *storagedomain.Config, objectKey string) error {
	svc, _, raw, err := p.client(ctx, cfg)
	if err != nil {
		return err
	}
	file, err := p.resolveFileByPath(ctx, svc, raw, strings.Trim(strings.TrimSpace(objectKey), "/"))
	if err != nil {
		// resolveFileByPath 找不到时回的是 40481：对象本来就不在。
		if appErr, ok := errors.AsType[*apperrors.AppError](err); ok && appErr.HTTPStatus == http.StatusNotFound {
			return nil
		}
		return err
	}
	call := svc.Files.Delete(file.Id)
	if raw.DriveID != "" {
		call = call.SupportsAllDrives(true)
	}
	return call.Context(ctx).Do()
}
