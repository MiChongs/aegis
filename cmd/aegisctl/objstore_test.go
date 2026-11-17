package main

import (
	"strings"
	"testing"
)

func TestObjstoreMigrateScriptKeepsSecretsOffCommandLines(t *testing.T) {
	s := ObjstoreMigrateScript(ObjstoreMigrateOptions{
		FromDir: "/srv/aegis/risk/minio", Network: "aegis-internal", DestHost: "risk-rustfs",
		SrcImage: objstoreDefaultSrcImage, RcloneImage: objstoreDefaultRcloneImage,
		SrcAccess: "OldAK", SrcSecret: "OldSecret123", DstAccess: "NewAK", DstSecret: "NewSecret456",
		Writers: []string{"risk-milvus", "risk-admin"},
	})
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "docker run") && (strings.Contains(line, "Secret123") || strings.Contains(line, "Secret456")) {
			t.Fatalf("密钥出现在 docker run 的参数里：%s", line)
		}
	}
	for _, want := range []string{
		"MINIO_ROOT_PASSWORD=OldSecret123", // 只在 600 的 env 文件里
		"RCLONE_CONFIG_DST_ENDPOINT=http://risk-rustfs:9000",
		`grep -E 'risk-milvus|risk-admin'`, // 写入方必须停着
		`grep -Fxq "$FROM"`,                // 旧目录不能被别的容器挂着
		"trap cleanup EXIT",
		"--one-way --checksum",
		"copy \"src:$bucket\" \"dst:$bucket\" --checksum",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("脚本里没有 %q", want)
		}
	}
}

func TestObjstoreCheckOnlyDoesNotCopy(t *testing.T) {
	s := ObjstoreMigrateScript(ObjstoreMigrateOptions{FromDir: "/x", Network: "n", DestHost: "d", CheckOnly: true, Force: true,
		SrcAccess: "a", SrcSecret: "b", DstAccess: "c", DstSecret: "d"})
	if strings.Contains(s, " copy ") {
		t.Fatal("check 不该复制")
	}
	if strings.Contains(s, "busy=") {
		t.Fatal("--force 时不该检查写入方")
	}
}

func TestIsAlnum(t *testing.T) {
	if !isAlnum("abcXYZ019") || isAlnum("") || isAlnum("a$b") || isAlnum("a b") {
		t.Fatal("isAlnum 判断有误")
	}
}
