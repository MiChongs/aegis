package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path"
	"sort"
	"strings"
)

// cmd_objstore.go：对象存储的数据搬迁。
//
// 统一栈的对象存储是 RustFS。旧的 MinIO 数据目录（现网风控的 /srv/aegis/risk/minio）不能直接给 RustFS 用：
// 两者的盘上格式不同。搬法是在数据所在节点上临时起一个 MinIO 读旧目录，用 rclone 把全部存储桶逐个对象
// 复制进 RustFS（按校验和比对，可重复执行，已复制的对象会跳过），复制完再单向核对一遍，最后删掉临时 MinIO。
//
// 搬迁期间不能有程序在写：Milvus 必须停着（它的段文件就在这里），旧 MinIO 也不能同时打开同一个目录。

const (
	objstoreDefaultSrcImage    = "milvusdb/minio:RELEASE.2024-12-18T13-15-44Z"
	objstoreDefaultRcloneImage = "rclone/rclone:1.76.0"
)

// objstoreTarget 目标存储：服务名 → 凭据键、节点标签、默认的旧数据目录
type objstoreTarget struct {
	Service   string
	Label     string
	AccessKey string
	SecretKey string
	// DefaultFrom 旧 MinIO 数据目录的默认值（相对 cluster.env 里的根目录键）
	RootKey     string
	RootDefault string
	SubDir      string
	// Writers 搬迁期间必须停着的容器名片段
	Writers []string
}

var objstoreTargets = map[string]objstoreTarget{
	"risk-rustfs": {
		Service: "risk-rustfs", Label: "aegis.risk",
		AccessKey: "RISK_RUSTFS_ACCESS_KEY", SecretKey: "RISK_RUSTFS_SECRET_KEY",
		RootKey: "AEGIS_RISK_DIR", RootDefault: "/srv/aegis/risk", SubDir: "minio",
		Writers: []string{"risk-milvus", "risk-admin", "risk-minio"},
	},
	"aegis-rustfs": {
		Service: "aegis-rustfs", Label: "aegis.storage",
		AccessKey: "AEGIS_RUSTFS_ACCESS_KEY", SecretKey: "AEGIS_RUSTFS_SECRET_KEY",
		RootKey: "AEGIS_MINIO_DIR", RootDefault: "/srv/aegis/minio",
		Writers: []string{"aegis-minio"},
	},
}

// ObjstoreMigrateOptions 生成搬迁脚本的参数
type ObjstoreMigrateOptions struct {
	FromDir     string
	Network     string
	DestHost    string // 目标在 overlay 上的别名，如 risk-rustfs
	SrcImage    string
	RcloneImage string
	SrcAccess   string
	SrcSecret   string
	DstAccess   string
	DstSecret   string
	Writers     []string
	Force       bool // 跳过「写入方必须停着」的检查
	CheckOnly   bool // 只核对，不复制
}

// ObjstoreMigrateScript 在数据所在节点上执行的脚本。经 ssh 标准输入下发，密钥只落到 600 的临时 env 文件里，
// 不出现在任何进程的命令行参数中。
func ObjstoreMigrateScript(o ObjstoreMigrateOptions) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("set -euo pipefail")
	w("FROM=%s", ShellQuote(o.FromDir))
	w("NET=%s", ShellQuote(o.Network))
	w(`SRC="aegis-objmig-src-$$"`)
	w(`[ -d "$FROM" ] || { echo "旧数据目录 $FROM 不存在" >&2; exit 2; }`)
	// 任何容器还挂着这个目录（旧 MinIO 没停）就不动：两个 MinIO 进程同时打开一个目录会写坏它
	w(`for c in $(docker ps -q); do`)
	w(`  if docker inspect --format '{{range .Mounts}}{{.Source}}{{"\n"}}{{end}}' "$c" | grep -Fxq "$FROM"; then`)
	w(`    echo "容器 $(docker inspect --format '{{.Name}}' "$c") 还挂着 $FROM，先停掉它" >&2; exit 3`)
	w(`  fi`)
	w(`done`)
	if !o.Force && len(o.Writers) > 0 {
		pattern := strings.Join(o.Writers, "|")
		w(`busy=$(docker ps --format '{{.Names}}' | grep -E %s || true)`, ShellQuote(pattern))
		w(`if [ -n "$busy" ]; then echo "这些容器还在运行，搬迁期间必须停着（--force 跳过检查）：" >&2; echo "$busy" >&2; exit 4; fi`)
	}
	w(`work=$(mktemp -d); chmod 700 "$work"`)
	w(`cleanup() { docker rm -f "$SRC" >/dev/null 2>&1 || true; rm -rf "$work"; }`)
	w(`trap cleanup EXIT`)
	// 临时 MinIO 用旧的根凭据启动：旧目录里的 IAM 配置是用它加密的
	w(`cat > "$work/src.env" <<'AEGIS_EOF'`)
	w("MINIO_ROOT_USER=%s", o.SrcAccess)
	w("MINIO_ROOT_PASSWORD=%s", o.SrcSecret)
	w("AEGIS_EOF")
	w(`cat > "$work/rclone.env" <<AEGIS_EOF`)
	for _, line := range []string{
		// 全部配置走环境变量，不读也不写配置文件
		"RCLONE_CONFIG=/dev/null",
		"RCLONE_CONFIG_SRC_TYPE=s3",
		"RCLONE_CONFIG_SRC_PROVIDER=Minio",
		"RCLONE_CONFIG_SRC_ENDPOINT=http://${SRC}:9000",
		"RCLONE_CONFIG_SRC_ACCESS_KEY_ID=" + o.SrcAccess,
		"RCLONE_CONFIG_SRC_SECRET_ACCESS_KEY=" + o.SrcSecret,
		"RCLONE_CONFIG_SRC_REGION=us-east-1",
		"RCLONE_CONFIG_SRC_FORCE_PATH_STYLE=true",
		"RCLONE_CONFIG_DST_TYPE=s3",
		"RCLONE_CONFIG_DST_PROVIDER=Other",
		"RCLONE_CONFIG_DST_ENDPOINT=http://" + o.DestHost + ":9000",
		"RCLONE_CONFIG_DST_ACCESS_KEY_ID=" + o.DstAccess,
		"RCLONE_CONFIG_DST_SECRET_ACCESS_KEY=" + o.DstSecret,
		"RCLONE_CONFIG_DST_REGION=us-east-1",
		"RCLONE_CONFIG_DST_FORCE_PATH_STYLE=true",
	} {
		// 值里只有字母数字（cluster.env 的密钥都是这样生成的），heredoc 里的 $ 只用于 ${SRC}
		w("%s", line)
	}
	w("AEGIS_EOF")
	w(`docker run -d --name "$SRC" --network "$NET" --env-file "$work/src.env" -v "$FROM:/data" %s server /data >/dev/null`, ShellQuote(o.SrcImage))
	w(`echo "→ 临时 MinIO 已启动（读 $FROM）"`)
	w(`ok=; for i in $(seq 1 60); do if docker exec "$SRC" mc ready local >/dev/null 2>&1; then ok=1; break; fi; sleep 2; done`)
	w(`[ -n "$ok" ] || { echo "临时 MinIO 没有就绪：" >&2; docker logs --tail 30 "$SRC" >&2; exit 5; }`)
	rclone := fmt.Sprintf(`docker run --rm --network "$NET" --env-file "$work/rclone.env" %s`, ShellQuote(o.RcloneImage))
	w(`%s lsd dst: >/dev/null || { echo "连不上目标存储 %s（服务没起来，或凭据不对）" >&2; exit 6; }`, rclone, o.DestHost)
	w(`echo "→ 旧存储里的桶："; %s lsd src:`, rclone)
	if !o.CheckOnly {
		// 逐桶复制（rclone 在根上 copy 也会建桶，但逐桶能把进度和失败对应到桶）
		w(`for bucket in $(%s lsf --dirs-only src: | tr -d /); do`, rclone)
		w(`  echo "→ 复制桶 $bucket"`)
		w(`  %s copy "src:$bucket" "dst:$bucket" --checksum --transfers 8 --stats 10s --stats-one-line --stats-log-level NOTICE`, rclone)
		w(`done`)
	}
	w(`echo "→ 核对（旧存储里的每个对象都要在 RustFS 里且内容一致）"`)
	w(`fail=0; for bucket in $(%s lsf --dirs-only src: | tr -d /); do`, rclone)
	w(`  %s check "src:$bucket" "dst:$bucket" --one-way --checksum || fail=1`, rclone)
	w(`  echo "  $bucket：旧 $(%s size --json "src:$bucket" | tr -d '\n')  新 $(%s size --json "dst:$bucket" | tr -d '\n')"`, rclone, rclone)
	w(`done`)
	w(`[ "$fail" = 0 ] || { echo "核对不通过：有对象没复制过去或内容不一致，可以再执行一次 objstore migrate 补齐" >&2; exit 7; }`)
	w(`echo "✓ 核对通过"`)
	return b.String()
}

func (a *App) cmdObjstore(ctx context.Context, args []string) error {
	if len(args) == 0 || (args[0] != "migrate" && args[0] != "check") {
		return usagef("aegisctl objstore migrate|check [--to risk-rustfs|aegis-rustfs] [--from-dir 目录] [--force] [--yes]")
	}
	sub := args[0]
	fs := flag.NewFlagSet("objstore "+sub, flag.ContinueOnError)
	to := fs.String("to", "risk-rustfs", "目标 RustFS 服务：risk-rustfs 或 aegis-rustfs")
	from := fs.String("from-dir", "", "旧 MinIO 的数据目录（默认 risk：$AEGIS_RISK_DIR/minio）")
	srcImage := fs.String("src-image", objstoreDefaultSrcImage, "读旧目录用的 MinIO 镜像（与现网旧 MinIO 同版本）")
	rcloneImage := fs.String("rclone-image", objstoreDefaultRcloneImage, "rclone 镜像")
	srcKeys := fs.String("src-keys", "", "旧 MinIO 根凭据在 cluster.env 里的两个键名（逗号分隔，默认与目标相同）")
	force := fs.Bool("force", false, "不检查 Milvus 等写入方是否已停")
	yes := fs.Bool("yes", false, "不再确认")
	if _, err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	target, ok := objstoreTargets[*to]
	if !ok {
		names := make([]string, 0, len(objstoreTargets))
		for k := range objstoreTargets {
			names = append(names, k)
		}
		sort.Strings(names)
		return usagef("--to 只能是 %s", strings.Join(names, " / "))
	}
	envFile, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	env := envFile.Map()
	fromDir := *from
	if fromDir == "" {
		fromDir = firstNonEmpty(env[target.RootKey], target.RootDefault)
		if target.SubDir != "" {
			fromDir = path.Join(fromDir, target.SubDir)
		}
	}
	if !path.IsAbs(fromDir) || path.Clean(fromDir) == "/" {
		return fmt.Errorf("--from-dir 必须是绝对路径且不能是根目录：%q", fromDir)
	}
	srcAK, srcSK := target.AccessKey, target.SecretKey
	if *srcKeys != "" {
		parts := strings.Split(*srcKeys, ",")
		if len(parts) != 2 {
			return usagef("--src-keys 要两个键名，逗号分隔")
		}
		srcAK, srcSK = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	for _, k := range []string{srcAK, srcSK, target.AccessKey, target.SecretKey} {
		if !isAlnum(env[k]) {
			return fmt.Errorf("cluster.env 的 %s 为空或含字母数字以外的字符（搬迁脚本要求只用字母数字）", k)
		}
	}

	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	var node string
	for _, n := range nodes.infos {
		if n.Spec.Labels[target.Label] == "true" {
			node = n.Description.Hostname
			break
		}
	}
	if node == "" {
		return fmt.Errorf("没有打 %s=true 标签的节点，不知道旧数据在哪台机器上", target.Label)
	}
	host := a.cfg.HostForNode(node, nodes.selfHostname())
	opts := ObjstoreMigrateOptions{
		FromDir: fromDir, Network: internalNetwork, DestHost: target.Service,
		SrcImage: *srcImage, RcloneImage: *rcloneImage,
		SrcAccess: env[srcAK], SrcSecret: env[srcSK], DstAccess: env[target.AccessKey], DstSecret: env[target.SecretKey],
		Writers: target.Writers, Force: *force, CheckOnly: sub == "check",
	}
	if sub == "migrate" {
		a.infof("把 %s:%s 的旧 MinIO 数据搬进 %s", node, fromDir, target.Service)
		a.infof("搬迁期间这些服务必须停着：%s", strings.Join(target.Writers, "、"))
		if !*yes && !a.confirm("继续？") {
			return errors.New("已取消")
		}
	}
	err = a.ex.Run(ctx, Remote{Host: host, Script: ObjstoreMigrateScript(opts), Stdout: a.out, Stderr: a.errOut})
	result := "success"
	if err != nil {
		result = "failed"
	}
	if sub == "migrate" {
		a.appendHistory(ctx, HistoryEntry{Action: "objstore-migrate", Services: []string{target.Service}, Result: result})
	}
	return err
}

func isAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
