package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	storagedomain "aegis/internal/domain/storage"
)

func TestLocalStorageProviderRejectsTraversalUpload(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "storage")
	cfg := &storagedomain.Config{
		Scope:    storagedomain.ScopeGlobal,
		Provider: storagedomain.ProviderLocal,
		ConfigData: map[string]any{
			"root_dir": root,
		},
	}
	outside := filepath.Join(parent, "outside.txt")
	_, err := newLocalStorageProvider().Upload(context.Background(), cfg, storagedomain.UploadInput{
		ObjectKey: "../outside.txt",
		FileName:  "outside.txt",
		Content:   bytes.NewBufferString("blocked"),
	})
	if err == nil {
		t.Fatal("期望目录穿越上传被拒绝")
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("根目录外文件不应被创建: %v", statErr)
	}
}

func TestLocalStorageProviderWritesAndReadsInsideRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "storage")
	cfg := &storagedomain.Config{
		Scope:    storagedomain.ScopeGlobal,
		Provider: storagedomain.ProviderLocal,
		ConfigData: map[string]any{
			"root_dir": root,
		},
	}
	provider := newLocalStorageProvider()
	_, err := provider.Upload(context.Background(), cfg, storagedomain.UploadInput{
		ObjectKey: "users/1/avatar.txt",
		FileName:  "avatar.txt",
		Content:   bytes.NewBufferString("safe"),
	})
	if err != nil {
		t.Fatalf("上传根目录内文件失败: %v", err)
	}
	reader, err := provider.Open(context.Background(), cfg, "users/1/avatar.txt")
	if err != nil {
		t.Fatalf("读取根目录内文件失败: %v", err)
	}
	defer reader.Body.Close()
	raw, err := io.ReadAll(reader.Body)
	if err != nil {
		t.Fatalf("读取文件内容失败: %v", err)
	}
	if string(raw) != "safe" {
		t.Fatalf("文件内容不匹配: %q", raw)
	}
}

func TestValidateStorageObjectKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"../secret", "a/../../secret", `C:\Windows\win.ini`, "/etc/passwd"} {
		if err := validateStorageObjectKey(key); err == nil {
			t.Errorf("期望拒绝非法对象键 %q", key)
		}
	}
	for _, key := range []string{"users/1/avatar.png", "2026/07/report.pdf"} {
		if err := validateStorageObjectKey(key); err != nil {
			t.Errorf("合法对象键 %q 被拒绝: %v", key, err)
		}
	}
}
