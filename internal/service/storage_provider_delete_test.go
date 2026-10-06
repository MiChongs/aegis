package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	storagedomain "aegis/internal/domain/storage"
)

// 每一家提供商都必须实现物理删除。
//
// 这是可选接口，漏实现不会有编译错误，只会让用户云存储在那家提供商上
// 「账面已释放、存储桶一直在涨」—— 而且只在线上换了提供商之后才暴露。
func TestEveryStorageProviderImplementsDelete(t *testing.T) {
	t.Parallel()

	providers := map[string]storageProvider{
		storagedomain.ProviderS3:          newS3StorageProvider(nil),
		storagedomain.ProviderMinIO:       newMinIOProvider(nil),
		storagedomain.ProviderAliyunOSS:   newAliyunOSSProvider(nil),
		storagedomain.ProviderTencentCOS:  newTencentCOSProvider(nil),
		storagedomain.ProviderQiniuKodo:   newQiniuKodoProvider(nil),
		storagedomain.ProviderWebDAV:      newWebDAVProvider(),
		storagedomain.ProviderOneDrive:    newOneDriveProvider(nil, nil, ""),
		storagedomain.ProviderDropbox:     newDropboxProvider(nil, nil, ""),
		storagedomain.ProviderGoogleDrive: newGoogleDriveProvider(nil, ""),
		storagedomain.ProviderAzureBlob:   newAzureBlobProvider(),
		storagedomain.ProviderLocal:       newLocalStorageProvider(),
	}
	for name, provider := range providers {
		if _, ok := provider.(storageObjectDeleter); !ok {
			t.Errorf("存储提供商 %s 没有实现 Delete", name)
		}
		// 熔断包装必须把删除转发下去，否则包装之后能力就丢了。
		cfg := &storagedomain.Config{ID: 1, Scope: storagedomain.ScopeGlobal, Provider: name, ConfigName: "t"}
		if _, ok := wrapStorageProvider(cfg, provider).(storageObjectDeleter); !ok {
			t.Errorf("存储提供商 %s 包装之后丢了 Delete", name)
		}
	}
}

func TestLocalProviderDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := &storagedomain.Config{
		Scope:      storagedomain.ScopeGlobal,
		Provider:   storagedomain.ProviderLocal,
		ConfigData: map[string]any{"root_dir": root},
	}
	provider := newLocalStorageProvider()
	ctx := context.Background()
	key := "cloud/1/2/favorites/default/r1.bin"
	if _, err := provider.Upload(ctx, cfg, storagedomain.UploadInput{
		ObjectKey: key, Content: bytes.NewReader([]byte(`{"a":1}`)), ContentLength: 7,
	}); err != nil {
		t.Fatalf("上传失败：%v", err)
	}
	if err := deleteStoredObject(ctx, provider, cfg, key); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); !os.IsNotExist(err) {
		t.Fatalf("删除之后文件应当不存在，得到 %v", err)
	}
	if err := deleteStoredObject(ctx, provider, cfg, key); err != nil {
		t.Fatalf("重复删除应当成功（幂等），得到 %v", err)
	}
	if err := deleteStoredObject(ctx, provider, cfg, "../escape"); err == nil {
		t.Fatal("越出根目录的键必须被拒绝")
	}
}

type uploadOnlyProvider struct{ storageProvider }

func TestDeleteReportsUnsupportedProvider(t *testing.T) {
	t.Parallel()

	err := deleteStoredObject(context.Background(), uploadOnlyProvider{}, &storagedomain.Config{}, "a")
	if !errors.Is(err, errStorageDeleteUnsupported) {
		t.Fatalf("不支持删除的提供商应当报 errStorageDeleteUnsupported，得到 %v", err)
	}
}
