package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/qiniu/go-sdk/v7/auth/qbox"
	qiniustorage "github.com/qiniu/go-sdk/v7/storage"
	redislib "github.com/redis/go-redis/v9"
	"github.com/studio-b12/gowebdav"
	cos "github.com/tencentyun/cos-go-sdk-v5"
)

type s3StorageProvider struct {
	httpClient *http.Client
}

func newS3StorageProvider(httpClient *http.Client) storageProvider {
	return &s3StorageProvider{httpClient: httpClient}
}

func (p *s3StorageProvider) Name() string { return storagedomain.ProviderS3 }

func (p *s3StorageProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	output, err := client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{Bucket: &raw.Bucket, MaxKeys: aws.Int32(1)})
	if err != nil {
		return nil, err
	}
	return map[string]any{"bucket": raw.Bucket, "region": raw.Region, "keyCount": len(output.Contents)}, nil
}

func (p *s3StorageProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	uploader := transfermanager.New(client)
	result, err := uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:        &raw.Bucket,
		Key:           &input.ObjectKey,
		Body:          input.Content,
		ContentLength: nullableInt64ForAWS(input.ContentLength),
		ContentType:   nullableStringForAWS(input.ContentType),
		CacheControl:  nullableStringForAWS(input.CacheControl),
		Metadata:      input.Metadata,
	})
	if err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      raw.Bucket,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		ETag:        strings.Trim(stringValue(result.ETag), "\""),
		URL:         stringValue(result.Location),
	}, nil
}

func (p *s3StorageProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	output, err := client.GetObject(ctx, &awss3.GetObjectInput{Bucket: &raw.Bucket, Key: &objectKey})
	if err != nil {
		return nil, err
	}
	return &storagedomain.ObjectReader{
		Body:         output.Body,
		Size:         int64Value(output.ContentLength),
		ContentType:  stringValue(output.ContentType),
		ETag:         strings.Trim(stringValue(output.ETag), "\""),
		CacheControl: stringValue(output.CacheControl),
		LastModified: output.LastModified,
	}, nil
}

func (p *s3StorageProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeS3StorageConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey), nil
	}
	base := strings.TrimRight(strings.TrimSpace(raw.Endpoint), "/")
	if raw.ForcePathStyle {
		return base + "/" + raw.Bucket + "/" + encodeObjectPath(objectKey), nil
	}
	parsed, err := neturl.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Host = raw.Bucket + "." + parsed.Host
	parsed.Path = "/" + encodeObjectPath(objectKey)
	return parsed.String(), nil
}

func (p *s3StorageProvider) client(ctx context.Context, cfg *storagedomain.Config) (*awss3.Client, *storagedomain.S3Config, error) {
	raw, err := decodeS3StorageConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(raw.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(raw.AccessKeyID, raw.SecretAccessKey, raw.SessionToken)),
	)
	if err != nil {
		return nil, nil, err
	}
	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.UsePathStyle = raw.ForcePathStyle
		o.BaseEndpoint = &raw.Endpoint
	})
	return client, raw, nil
}

type aliyunOSSProvider struct {
	httpClient *http.Client
}

func newAliyunOSSProvider(httpClient *http.Client) storageProvider {
	return &aliyunOSSProvider{httpClient: httpClient}
}

func (p *aliyunOSSProvider) Name() string { return storagedomain.ProviderAliyunOSS }

func (p *aliyunOSSProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	bucket, raw, err := p.bucket(cfg)
	if err != nil {
		return nil, err
	}
	result, err := bucket.ListObjectsV2(oss.MaxKeys(1))
	if err != nil {
		return nil, err
	}
	return map[string]any{"bucket": raw.Bucket, "endpoint": raw.Endpoint, "keyCount": len(result.Objects)}, nil
}

func (p *aliyunOSSProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	bucket, raw, err := p.bucket(cfg)
	if err != nil {
		return nil, err
	}
	options := []oss.Option{}
	if input.ContentType != "" {
		options = append(options, oss.ContentType(input.ContentType))
	}
	if input.CacheControl != "" {
		options = append(options, oss.CacheControl(input.CacheControl))
	}
	if input.ContentLength > 0 {
		options = append(options, oss.ContentLength(input.ContentLength))
	}
	for key, value := range input.Metadata {
		options = append(options, oss.Meta(key, value))
	}
	if err := bucket.PutObject(input.ObjectKey, input.Content, options...); err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      raw.Bucket,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		URL:         composeAliyunOSSURL(cfg, raw, input.ObjectKey),
	}, nil
}

func (p *aliyunOSSProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	bucket, _, err := p.bucket(cfg)
	if err != nil {
		return nil, err
	}
	meta, _ := bucket.GetObjectDetailedMeta(objectKey)
	body, err := bucket.GetObject(objectKey)
	if err != nil {
		return nil, err
	}
	reader := &storagedomain.ObjectReader{Body: body}
	if meta != nil {
		reader.ContentType = meta.Get("Content-Type")
		reader.ETag = strings.Trim(meta.Get("ETag"), "\"")
		reader.CacheControl = meta.Get("Cache-Control")
		if length, parseErr := strconv.ParseInt(meta.Get("Content-Length"), 10, 64); parseErr == nil {
			reader.Size = length
		}
	}
	return reader, nil
}

func (p *aliyunOSSProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeAliyunOSSConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey), nil
	}
	return composeAliyunOSSURL(cfg, raw, objectKey), nil
}

func (p *aliyunOSSProvider) bucket(cfg *storagedomain.Config) (*oss.Bucket, *storagedomain.AliyunOSSConfig, error) {
	raw, err := decodeAliyunOSSConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	client, err := oss.New(raw.Endpoint, raw.AccessKeyID, raw.AccessKeySecret)
	if err != nil {
		return nil, nil, err
	}
	bucket, err := client.Bucket(raw.Bucket)
	if err != nil {
		return nil, nil, err
	}
	return bucket, raw, nil
}

type tencentCOSProvider struct {
	httpClient *http.Client
}

func newTencentCOSProvider(httpClient *http.Client) storageProvider {
	return &tencentCOSProvider{httpClient: httpClient}
}

func (p *tencentCOSProvider) Name() string { return storagedomain.ProviderTencentCOS }

func (p *tencentCOSProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	result, _, err := client.Bucket.Get(ctx, &cos.BucketGetOptions{MaxKeys: 1})
	if err != nil {
		return nil, err
	}
	return map[string]any{"bucket_url": firstNonEmpty(raw.BucketURL, raw.Endpoint), "objectCount": len(result.Contents)}, nil
}

func (p *tencentCOSProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	options := &cos.ObjectPutOptions{}
	if input.ContentType != "" || input.CacheControl != "" {
		options.ObjectPutHeaderOptions = &cos.ObjectPutHeaderOptions{
			ContentType:  input.ContentType,
			CacheControl: input.CacheControl,
		}
	}
	resp, err := client.Object.Put(ctx, input.ObjectKey, input.Content, options)
	if err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      firstNonEmpty(raw.BucketURL, raw.Endpoint),
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		ETag:        strings.Trim(resp.Header.Get("ETag"), "\""),
		URL:         composeTencentCOSURL(cfg, raw, input.ObjectKey),
	}, nil
}

func (p *tencentCOSProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	client, _, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.Object.Get(ctx, objectKey, nil)
	if err != nil {
		return nil, err
	}
	reader := &storagedomain.ObjectReader{
		Body:         resp.Body,
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         strings.Trim(resp.Header.Get("ETag"), "\""),
		CacheControl: resp.Header.Get("Cache-Control"),
	}
	if length, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil {
		reader.Size = length
	}
	return reader, nil
}

func (p *tencentCOSProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeTencentCOSConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey), nil
	}
	return composeTencentCOSURL(cfg, raw, objectKey), nil
}

func (p *tencentCOSProvider) client(cfg *storagedomain.Config) (*cos.Client, *storagedomain.TencentCOSConfig, error) {
	raw, err := decodeTencentCOSConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	baseURL := strings.TrimSpace(firstNonEmpty(raw.BucketURL, raw.Endpoint))
	parsed, err := neturl.Parse(baseURL)
	if err != nil {
		return nil, nil, err
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: parsed}, &http.Client{
		Transport: &cos.AuthorizationTransport{SecretID: raw.SecretID, SecretKey: raw.SecretKey},
		Timeout:   60 * time.Second,
	})
	return client, raw, nil
}

type qiniuKodoProvider struct {
	httpClient *http.Client
}

func newQiniuKodoProvider(httpClient *http.Client) storageProvider {
	return &qiniuKodoProvider{httpClient: httpClient}
}

func (p *qiniuKodoProvider) Name() string { return storagedomain.ProviderQiniuKodo }

func (p *qiniuKodoProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	raw, err := decodeQiniuKodoConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	mac := qbox.NewMac(raw.AccessKey, raw.SecretKey)
	cfgObj := qiniustorage.Config{Zone: qiniuZone(raw.Region), UseHTTPS: raw.UseHTTPS}
	bucketManager := qiniustorage.NewBucketManager(mac, &cfgObj)
	entries, _, _, _, err := bucketManager.ListFiles(raw.Bucket, "", "", "", 1)
	if err != nil {
		return nil, err
	}
	return map[string]any{"bucket": raw.Bucket, "domain": raw.Domain, "keyCount": len(entries)}, nil
}

func (p *qiniuKodoProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	raw, err := decodeQiniuKodoConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	putPolicy := qiniustorage.PutPolicy{Scope: raw.Bucket}
	mac := qbox.NewMac(raw.AccessKey, raw.SecretKey)
	upToken := putPolicy.UploadToken(mac)
	cfgObj := qiniustorage.Config{Zone: qiniuZone(raw.Region), UseHTTPS: raw.UseHTTPS}
	uploader := qiniustorage.NewFormUploader(&cfgObj)
	ret := qiniustorage.PutRet{}
	putExtra := qiniustorage.PutExtra{}
	if err := uploader.Put(ctx, &ret, upToken, input.ObjectKey, input.Content, input.ContentLength, &putExtra); err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      raw.Bucket,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		ETag:        ret.Hash,
		URL:         composeQiniuKodoURL(cfg, raw, input.ObjectKey),
	}, nil
}

func (p *qiniuKodoProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	raw, err := decodeQiniuKodoConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	targetURL := composeQiniuKodoURL(cfg, raw, objectKey)
	if cfg.AccessMode == storagedomain.AccessPrivate {
		targetURL = qiniustorage.MakePrivateURL(qbox.NewMac(raw.AccessKey, raw.SecretKey), qiniuBaseDomain(cfg, raw), objectKey, time.Now().Add(5*time.Minute).Unix())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		closeSilently(resp.Body)
		return nil, fmt.Errorf("qiniu open status=%d", resp.StatusCode)
	}
	reader := &storagedomain.ObjectReader{
		Body:         resp.Body,
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         strings.Trim(resp.Header.Get("ETag"), "\""),
		CacheControl: resp.Header.Get("Cache-Control"),
	}
	if length, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil {
		reader.Size = length
	}
	return reader, nil
}

func (p *qiniuKodoProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeQiniuKodoConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if cfg.AccessMode == storagedomain.AccessPrivate {
		return "", apperrors.New(40098, http.StatusBadRequest, "七牛私有资源需要代理访问")
	}
	return composeQiniuKodoURL(cfg, raw, objectKey), nil
}

type webDAVProvider struct{}

func newWebDAVProvider() storageProvider { return &webDAVProvider{} }

func (p *webDAVProvider) Name() string { return storagedomain.ProviderWebDAV }

func (p *webDAVProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	items, err := client.ReadDir("/")
	if err != nil {
		return nil, err
	}
	return map[string]any{"endpoint": raw.Endpoint, "entries": len(items)}, nil
}

func (p *webDAVProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	if err := ensureWebDAVDir(client, path.Dir("/"+input.ObjectKey)); err != nil {
		return nil, err
	}
	if err := client.WriteStream("/"+input.ObjectKey, input.Content, 0644); err != nil {
		return nil, err
	}
	return &storagedomain.StoredObject{
		Bucket:      raw.Endpoint,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		URL:         composeStaticObjectURL(firstNonEmpty(cfg.BaseURL, raw.Endpoint), input.ObjectKey),
	}, nil
}

func (p *webDAVProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	client, _, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	info, _ := client.Stat("/" + objectKey)
	stream, err := client.ReadStream("/" + objectKey)
	if err != nil {
		return nil, err
	}
	reader := &storagedomain.ObjectReader{Body: stream}
	if info != nil {
		reader.Size = info.Size()
		reader.FileName = info.Name()
		modified := info.ModTime()
		reader.LastModified = &modified
	}
	return reader, nil
}

func (p *webDAVProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeWebDAVConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	return composeStaticObjectURL(firstNonEmpty(cfg.BaseURL, raw.Endpoint), objectKey), nil
}

func (p *webDAVProvider) client(cfg *storagedomain.Config) (*gowebdav.Client, *storagedomain.WebDAVConfig, error) {
	raw, err := decodeWebDAVConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	client := gowebdav.NewClient(raw.Endpoint, raw.Username, raw.Password)
	return client, raw, nil
}

type oneDriveProvider struct {
	httpClient *http.Client
	redis      *redislib.Client
	keyPrefix  string
}

type oneDriveTokenCache struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

func newOneDriveProvider(httpClient *http.Client, redis *redislib.Client, keyPrefix string) storageProvider {
	return &oneDriveProvider{httpClient: httpClient, redis: redis, keyPrefix: keyPrefix}
}

func (p *oneDriveProvider) Name() string { return storagedomain.ProviderOneDrive }

func (p *oneDriveProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	raw, err := decodeOneDriveConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	resp, err := p.graphJSONRequest(ctx, cfg, http.MethodGet, p.itemURL(raw, ""), nil)
	if err != nil {
		return nil, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("onedrive health status=%d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return map[string]any{"drive_id": raw.DriveID, "root_name": payload["name"], "quota": payload["quota"]}, nil
}

func (p *oneDriveProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	raw, err := decodeOneDriveConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	if input.ContentLength <= 0 {
		return nil, apperrors.New(40102, http.StatusBadRequest, "OneDrive 上传必须提供文件大小")
	}
	sessionPayload := map[string]any{
		"item": map[string]any{
			"@microsoft.graph.conflictBehavior": "replace",
			"name":                              path.Base(input.ObjectKey),
		},
	}
	resp, err := p.graphJSONRequest(ctx, cfg, http.MethodPost, p.itemURL(raw, input.ObjectKey)+":/createUploadSession", sessionPayload)
	if err != nil {
		return nil, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("onedrive create upload session status=%d", resp.StatusCode)
	}
	var uploadSession struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploadSession); err != nil {
		return nil, err
	}
	if strings.TrimSpace(uploadSession.UploadURL) == "" {
		return nil, apperrors.New(40103, http.StatusBadGateway, "OneDrive 上传会话创建失败")
	}

	const chunkSize = 5 * 1024 * 1024
	buffer := make([]byte, chunkSize)
	var uploaded int64
	for uploaded < input.ContentLength {
		toRead := int64(chunkSize)
		if remain := input.ContentLength - uploaded; remain < toRead {
			toRead = remain
		}
		n, readErr := io.ReadFull(input.Content, buffer[:toRead])
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return nil, readErr
		}
		if n == 0 {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadSession.UploadURL, bytes.NewReader(buffer[:n]))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Length", strconv.Itoa(n))
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", uploaded, uploaded+int64(n)-1, input.ContentLength))
		req.Header.Set("Content-Type", "application/octet-stream")
		chunkResp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		closeSilently(chunkResp.Body)
		if chunkResp.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("onedrive upload chunk status=%d", chunkResp.StatusCode)
		}
		uploaded += int64(n)
	}
	return &storagedomain.StoredObject{
		Bucket:      raw.DriveID,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        input.ContentLength,
		ContentType: input.ContentType,
		URL:         composeOneDriveWebURL(cfg, raw, input.ObjectKey),
	}, nil
}

func (p *oneDriveProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	raw, err := decodeOneDriveConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	req, err := p.graphRequest(ctx, cfg, http.MethodGet, p.itemURL(raw, objectKey)+":/content", nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		closeSilently(resp.Body)
		return nil, fmt.Errorf("onedrive open status=%d", resp.StatusCode)
	}
	reader := &storagedomain.ObjectReader{
		Body:         resp.Body,
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         strings.Trim(resp.Header.Get("ETag"), "\""),
		CacheControl: resp.Header.Get("Cache-Control"),
		FileName:     path.Base(objectKey),
	}
	if length, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil {
		reader.Size = length
	}
	return reader, nil
}

func (p *oneDriveProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeOneDriveConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey), nil
	}
	resp, err := p.graphJSONRequest(ctx, cfg, http.MethodPost, p.itemURL(raw, objectKey)+":/createLink", map[string]any{
		"type":  "view",
		"scope": "anonymous",
	})
	if err != nil {
		return "", err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("onedrive create link status=%d", resp.StatusCode)
	}
	var payload struct {
		Link struct {
			WebURL string `json:"webUrl"`
		} `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Link.WebURL) == "" {
		return composeOneDriveWebURL(cfg, raw, objectKey), nil
	}
	return payload.Link.WebURL, nil
}

func (p *oneDriveProvider) graphJSONRequest(ctx context.Context, cfg *storagedomain.Config, method string, targetURL string, body any) (*http.Response, error) {
	headers := map[string]string{}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(raw))
		headers["Content-Type"] = "application/json"
	}
	req, err := p.graphRequest(ctx, cfg, method, targetURL, reader, headers)
	if err != nil {
		return nil, err
	}
	return p.httpClient.Do(req)
}

func (p *oneDriveProvider) graphRequest(ctx context.Context, cfg *storagedomain.Config, method string, targetURL string, body io.Reader, headers map[string]string) (*http.Request, error) {
	token, err := p.resolveToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

func (p *oneDriveProvider) resolveToken(ctx context.Context, cfg *storagedomain.Config) (string, error) {
	raw, err := decodeOneDriveConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if p.redis != nil {
		cacheKey := p.tokenCacheKey(cfg.ID)
		if cached, err := p.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
			var token oneDriveTokenCache
			if json.Unmarshal(cached, &token) == nil && strings.TrimSpace(token.AccessToken) != "" && token.ExpiresAt > time.Now().Unix()+30 {
				return token.AccessToken, nil
			}
		}
	}
	if strings.TrimSpace(raw.RefreshToken) != "" && strings.TrimSpace(raw.ClientID) != "" && strings.TrimSpace(raw.ClientSecret) != "" {
		return p.refreshToken(ctx, cfg, raw)
	}
	if strings.TrimSpace(raw.AccessToken) != "" {
		return raw.AccessToken, nil
	}
	return "", apperrors.New(40104, http.StatusBadRequest, "OneDrive 令牌已失效且缺少刷新配置")
}

func (p *oneDriveProvider) refreshToken(ctx context.Context, cfg *storagedomain.Config, raw *storagedomain.OneDriveConfig) (string, error) {
	if strings.TrimSpace(raw.RefreshToken) == "" || strings.TrimSpace(raw.ClientID) == "" || strings.TrimSpace(raw.ClientSecret) == "" {
		return "", apperrors.New(40104, http.StatusBadRequest, "OneDrive 令牌已失效且缺少刷新配置")
	}
	tenant := strings.TrimSpace(raw.TenantID)
	if tenant == "" {
		tenant = "common"
	}
	form := neturl.Values{}
	form.Set("client_id", raw.ClientID)
	form.Set("client_secret", raw.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", raw.RefreshToken)
	form.Set("scope", "offline_access Files.ReadWrite.All User.Read")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://login.microsoftonline.com/"+tenant+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("onedrive refresh status=%d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", apperrors.New(40105, http.StatusBadGateway, "OneDrive 刷新令牌失败")
	}
	if p.redis != nil {
		cache := oneDriveTokenCache{AccessToken: payload.AccessToken, ExpiresAt: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).Unix()}
		if rawBytes, err := json.Marshal(cache); err == nil {
			ttl := time.Duration(payload.ExpiresIn-60) * time.Second
			if ttl < time.Minute {
				ttl = time.Minute
			}
			_ = p.redis.Set(ctx, p.tokenCacheKey(cfg.ID), rawBytes, ttl).Err()
		}
	}
	return payload.AccessToken, nil
}

func (p *oneDriveProvider) itemURL(raw *storagedomain.OneDriveConfig, objectKey string) string {
	base := "https://graph.microsoft.com/v1.0/drives/" + neturl.PathEscape(strings.TrimSpace(raw.DriveID)) + "/root"
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	if objectKey == "" {
		return base
	}
	return base + ":/" + encodeObjectPath(objectKey)
}

func (p *oneDriveProvider) tokenCacheKey(configID int64) string {
	return fmt.Sprintf("%s:storage:onedrive:token:%d", p.keyPrefix, configID)
}

func composeOneDriveWebURL(cfg *storagedomain.Config, raw *storagedomain.OneDriveConfig, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	return "https://onedrive.live.com/?id=" + neturl.QueryEscape(strings.TrimSpace(raw.DriveID)) + "&path=%2F" + neturl.QueryEscape(strings.Trim(strings.TrimSpace(objectKey), "/"))
}

func composeStaticObjectURL(baseURL string, objectKey string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	objectKey = strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if baseURL == "" {
		return "/" + objectKey
	}
	return baseURL + "/" + encodeObjectPath(objectKey)
}

func composeAliyunOSSURL(cfg *storagedomain.Config, raw *storagedomain.AliyunOSSConfig, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	base := strings.TrimRight(strings.TrimSpace(raw.Endpoint), "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	parsed, err := neturl.Parse(base)
	if err != nil {
		return composeStaticObjectURL(base, objectKey)
	}
	parsed.Host = raw.Bucket + "." + parsed.Host
	parsed.Path = "/" + encodeObjectPath(objectKey)
	return parsed.String()
}

func composeTencentCOSURL(cfg *storagedomain.Config, raw *storagedomain.TencentCOSConfig, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	return composeStaticObjectURL(firstNonEmpty(raw.BucketURL, raw.Endpoint), objectKey)
}

func composeQiniuKodoURL(cfg *storagedomain.Config, raw *storagedomain.QiniuKodoConfig, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	return composeStaticObjectURL(qiniuBaseDomain(cfg, raw), objectKey)
}

func qiniuBaseDomain(cfg *storagedomain.Config, raw *storagedomain.QiniuKodoConfig) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	}
	domain := strings.TrimRight(strings.TrimSpace(raw.Domain), "/")
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		if raw.UseHTTPS {
			domain = "https://" + domain
		} else {
			domain = "http://" + domain
		}
	}
	return domain
}

func qiniuZone(region string) *qiniustorage.Zone {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "z0", "huadong", "east", "cn-east-1":
		return &qiniustorage.ZoneHuadong
	case "z1", "huabei", "north", "cn-north-1":
		return &qiniustorage.ZoneHuabei
	case "z2", "huanan", "south", "cn-south-1":
		return &qiniustorage.ZoneHuanan
	case "na0", "beimei", "us":
		return &qiniustorage.ZoneBeimei
	case "as0", "xinjiapo", "ap-southeast-1":
		return &qiniustorage.ZoneXinjiapo
	default:
		return &qiniustorage.ZoneHuadong
	}
}

func ensureWebDAVDir(client *gowebdav.Client, dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "/" || dir == "." {
		return nil
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		if err := client.Mkdir(current, os.ModePerm); err != nil && !strings.Contains(strings.ToLower(err.Error()), "405") && !strings.Contains(strings.ToLower(err.Error()), "exists") {
			return err
		}
	}
	return nil
}

func nullableStringForAWS(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func nullableInt64ForAWS(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func encodeObjectPath(objectKey string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(objectKey), "/"), "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		encoded = append(encoded, neturl.PathEscape(part))
	}
	return strings.Join(encoded, "/")
}
