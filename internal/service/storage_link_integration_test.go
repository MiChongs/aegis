package service

import (
	"io"
	"net/url"
	"strings"
	"testing"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 存储代理地址的端到端集成测试：真实 Postgres + 本地存储。
//
// 走一遍「上传拿到地址 → 换一个服务实例（重启、没有 Redis）仍能读 →
// 配了 root_path 的对象经 storage:// 引用解析不再 404 → 对象删除后地址失效」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestStorageLinkIntegration
func TestStorageLinkIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)

	cfg, err := pg.UpsertStorageConfig(ctx, storagedomain.Config{
		Scope:         storagedomain.ScopeGlobal,
		Provider:      storagedomain.ProviderLocal,
		ConfigName:    "local-test",
		AccessMode:    storagedomain.AccessPrivate,
		Enabled:       true,
		IsDefault:     true,
		ProxyDownload: true,
		RootPath:      "uploads",
		ConfigData:    map[string]any{"root_dir": t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}

	before := NewStorageService(zap.NewNop(), pg, nil, "test")
	before.SetLinkSigningKey("master")
	stored, err := before.UploadForApp(ctx, 0, storagedomain.UploadInput{
		FileName:      "shot.png",
		ContentType:   "image/png",
		ContentLength: 5,
		Content:       strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.Key, "uploads/") {
		t.Fatalf("对象键应含 root_path，得到 %q", stored.Key)
	}
	if !strings.HasPrefix(stored.URL, "/api/storage/proxy/") {
		t.Fatalf("上传返回的应是代理地址，得到 %q", stored.URL)
	}

	// 重启以后：新实例、同一主密钥、没有 Redis
	after := NewStorageService(zap.NewNop(), pg, nil, "test")
	after.SetLinkSigningKey("master")
	read := func(link string) (string, error) {
		t.Helper()
		token, err := url.PathUnescape(strings.TrimPrefix(link, "/api/storage/proxy/"))
		if err != nil {
			t.Fatal(err)
		}
		_, _, reader, err := after.OpenProxyObject(ctx, token)
		if err != nil {
			return "", err
		}
		defer reader.Body.Close()
		data, err := io.ReadAll(reader.Body)
		return string(data), err
	}
	if got, err := read(stored.URL); err != nil || got != "hello" {
		t.Fatalf("上传返回的地址重启后应仍可读，得到 %q %v", got, err)
	}

	// storage:// 引用里存的是完整键，解析时不能再拼一次 root_path
	permanent, err := after.PermanentObjectLink(ctx, 0, cfg.ID, stored.Key, true, "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := read(permanent); err != nil || got != "hello" {
		t.Fatalf("永久地址应可读，得到 %q %v", got, err)
	}

	// 旧的 Redis 票据在没有 Redis 时给出错误而不是崩溃
	if _, _, _, err := after.OpenProxyObject(ctx, "0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("没有 Redis 时旧票据不应可读")
	}

	if err := after.DeleteObject(ctx, cfg.ID, stored.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := read(permanent); err == nil {
		t.Fatal("对象删除后地址应失效")
	}
}
