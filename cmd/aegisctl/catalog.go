package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// catalog.go：镜像、服务分组、节点标签与数据目录的清单。与 deploy/swarm/aegis-stack.yml 保持一致。

// ImageSpec 一个自建镜像
type ImageSpec struct {
	Name       string   // 镜像名（仓库前缀之后）
	Alias      []string // build 命令里可用的简称
	TagVar     string   // 栈文件里的标签变量
	Dockerfile string
	Context    string
	Services   []string          // 用这个镜像的服务
	BuildArgs  map[string]string // 构建参数 → cluster.env 键（值为空串时跳过）
	FixedArgs  map[string]string // 固定构建参数
}

// Images 全部自建镜像
var Images = []ImageSpec{
	{
		Name: "aegis-server", Alias: []string{"server", "api", "worker"}, TagVar: "AEGIS_TAG",
		Dockerfile: "deploy/docker/Dockerfile", Context: ".",
		Services:  []string{"aegis-api", "aegis-worker", "aegis-migrate"},
		BuildArgs: map[string]string{"GOPROXY": "AEGIS_BUILD_GOPROXY"},
	},
	{
		Name: "aegis-console", Alias: []string{"console"}, TagVar: "AEGIS_CONSOLE_TAG",
		Dockerfile: "deploy/docker/console.Dockerfile", Context: "aegis-console",
		Services: []string{"aegis-console"},
		BuildArgs: map[string]string{
			"AEGIS_API_BACKEND": "AEGIS_CONSOLE_API_BACKEND",
			// 构建期内联进前端产物，取 cluster.env 里的同名键
			"NEXT_PUBLIC_AEGIS_CONSOLE_NAME":  "NEXT_PUBLIC_AEGIS_CONSOLE_NAME",
			"NEXT_PUBLIC_AEGIS_PLATFORM_NAME": "NEXT_PUBLIC_AEGIS_PLATFORM_NAME",
			"NEXT_PUBLIC_AEGIS_ENVIRONMENT":   "NEXT_PUBLIC_AEGIS_ENVIRONMENT",
		},
	},
	{
		Name: "aegis-listen-together", Alias: []string{"listen-together", "lt"}, TagVar: "AEGIS_LISTEN_TOGETHER_TAG",
		Dockerfile: "deploy/docker/listen-together.Dockerfile", Context: ".",
		Services:  []string{"listen-together"},
		BuildArgs: map[string]string{"GOPROXY": "AEGIS_BUILD_GOPROXY"},
		FixedArgs: map[string]string{"VERSION": "$TAG"},
	},
	{
		Name: "aegis-risk", Alias: []string{"risk"}, TagVar: "AEGIS_RISK_TAG",
		Dockerfile: "deploy/docker/risk.Dockerfile", Context: ".",
		Services:  []string{"risk-migrate", "risk-decision-1", "risk-decision-2", "risk-admin", "risk-ingest"},
		BuildArgs: map[string]string{"GOPROXY": "AEGIS_BUILD_GOPROXY"},
		FixedArgs: map[string]string{"VERSION": "$TAG"},
	},
	{
		Name: "aegis-risk-cep", Alias: []string{"risk-cep", "cep", "flink"}, TagVar: "AEGIS_RISK_CEP_TAG",
		Dockerfile: "deploy/docker/risk-cep.Dockerfile", Context: ".",
		Services:  []string{"risk-flink-jobmanager", "risk-flink-taskmanager"},
		BuildArgs: map[string]string{"MAVEN_OPTS": "AEGIS_BUILD_MAVEN_OPTS", "MAVEN_MIRROR": "AEGIS_BUILD_MAVEN_MIRROR"},
	},
	{
		Name: "aegis-music", Alias: []string{"music"}, TagVar: "AEGIS_MUSIC_TAG",
		Dockerfile: "deploy/docker/music.Dockerfile", Context: ".",
		Services:  []string{"music-rec", "music-ingest", "music-trainer"},
		BuildArgs: map[string]string{"GOPROXY": "AEGIS_BUILD_GOPROXY"},
		FixedArgs: map[string]string{"VERSION": "$TAG"},
	},
	{
		Name: "aegis-music-deep", Alias: []string{"music-deep", "deep"}, TagVar: "AEGIS_MUSIC_DEEP_TAG",
		Dockerfile: "deploy/docker/music-deep.Dockerfile", Context: ".",
		Services:  []string{"music-deep"},
		BuildArgs: map[string]string{"PIP_INDEX_URL": "AEGIS_BUILD_PIP_INDEX_URL"},
	},
}

// migrateTagVar 迁移任务单独的标签变量：deploy 先只换它，迁移成功后再换 api / worker
const migrateTagVar = "AEGIS_MIGRATE_TAG"

// imageForService 服务用的镜像
func imageForService(svc string) *ImageSpec {
	for i := range Images {
		for _, s := range Images[i].Services {
			if s == svc {
				return &Images[i]
			}
		}
	}
	return nil
}

// ResolveImages build 参数里的镜像名或简称；为空表示全部
func ResolveImages(names []string) ([]ImageSpec, error) {
	if len(names) == 0 {
		return Images, nil
	}
	var out []ImageSpec
	seen := map[string]bool{}
	for _, n := range names {
		var found *ImageSpec
		for i := range Images {
			if Images[i].Name == n || contains(Images[i].Alias, n) || contains(Images[i].Services, n) {
				found = &Images[i]
				break
			}
		}
		if found == nil {
			return nil, usagef("不认识的镜像 %q（可用：%s）", n, strings.Join(imageNames(), "、"))
		}
		if !seen[found.Name] {
			seen[found.Name] = true
			out = append(out, *found)
		}
	}
	return out, nil
}

func imageNames() []string {
	n := make([]string, len(Images))
	for i, im := range Images {
		n[i] = im.Name
	}
	return n
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// 服务分组：deploy --only、rollback 可以写组名
var serviceGroups = map[string][]string{
	"app":   {"aegis-migrate", "aegis-api", "aegis-worker"},
	"web":   {"aegis-console", "listen-together"},
	"infra": {"postgres", "redis", "nats", "temporal", "temporal-ui", "aegis-minio"},
	"risk": {"risk-etcd", "risk-redis-1", "risk-redis-2", "risk-redis-3", "risk-redis-init", "risk-minio", "risk-milvus",
		"risk-nebula-metad", "risk-nebula-storaged", "risk-nebula-graphd", "risk-clickhouse", "risk-risingwave",
		"risk-migrate", "risk-decision-1", "risk-decision-2", "risk-admin", "risk-ingest",
		"risk-flink-jobmanager", "risk-flink-taskmanager"},
	"music": {"music-rec", "music-ingest", "music-trainer", "music-deep"},
}

// 可以 scale 的服务（其余要么是有状态的，要么是固定实例）
var scalable = map[string]string{
	"aegis-api":       "AEGIS_API_REPLICAS",
	"aegis-worker":    "AEGIS_WORKER_REPLICAS",
	"aegis-console":   "AEGIS_CONSOLE_REPLICAS",
	"listen-together": "AEGIS_LISTEN_TOGETHER_REPLICAS",
}

// ExpandServices 把逗号分隔的服务名与组名展开成集合，并校验都在栈文件里
func ExpandServices(spec string, known []string) (map[string]bool, error) {
	set := map[string]bool{}
	knownSet := map[string]bool{}
	for _, k := range known {
		knownSet[k] = true
	}
	for _, item := range splitList(spec) {
		if g, ok := serviceGroups[item]; ok {
			for _, s := range g {
				set[s] = true
			}
			continue
		}
		if !knownSet[item] {
			return nil, usagef("栈文件里没有服务 %q（组名可用 %s）", item, strings.Join(groupNames(), "、"))
		}
		set[item] = true
	}
	// app 组里的服务要带上迁移任务
	if set["aegis-api"] || set["aegis-worker"] {
		set["aegis-migrate"] = true
	}
	return set, nil
}

func groupNames() []string {
	var n []string
	for k := range serviceGroups {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// ─────────────────────────── 节点标签与数据目录 ───────────────────────────

// StatefulLabels 有状态标签：同一时刻只该在一个节点上
var StatefulLabels = []string{"aegis.db", "aegis.storage", "aegis.risk", "aegis.music"}

// AllLabels init 时打给第一个 manager 的全部标签
var AllLabels = append([]string{"aegis.app"}, StatefulLabels...)

// DirSpec 节点上要准备的目录
type DirSpec struct {
	Path    string
	Mode    string
	Owner   string // 空表示不改属主
	Recurse bool   // 每次都 chown -R（与原 risk-volume-init 一致）
	NewOnly bool   // 只在新建时设属主（已有数据不动）
}

// DirsForLabel 打了某个标签的节点上需要的目录（路径取自 cluster.env）
func DirsForLabel(label string, env map[string]string) []DirSpec {
	get := func(k, def string) string {
		if v := strings.TrimSpace(env[k]); v != "" {
			return v
		}
		return def
	}
	switch label {
	case "aegis.db":
		conf := get("AEGIS_NATS_CONF", "/srv/aegis/nats/nats-server.conf")
		return []DirSpec{
			{Path: get("AEGIS_PG_DIR", "/srv/aegis/postgres"), Mode: "700"},
			{Path: get("AEGIS_REDIS_DIR", "/srv/aegis/redis"), Mode: "755"},
			{Path: path.Dir(conf), Mode: "700"},
			{Path: get("AEGIS_NATS_DATA", "/srv/aegis/nats/data"), Mode: "700"},
		}
	case "aegis.storage":
		return []DirSpec{{Path: get("AEGIS_MINIO_DIR", "/srv/aegis/minio"), Mode: "700"}}
	case "aegis.risk":
		root := get("AEGIS_RISK_DIR", "/srv/aegis/risk")
		var dirs []DirSpec
		for _, d := range []string{"etcd", "redis-1", "redis-2", "redis-3", "minio", "nebula-meta", "nebula-storage", "risingwave"} {
			dirs = append(dirs, DirSpec{Path: root + "/" + d, Mode: "755"})
		}
		dirs = append(dirs,
			DirSpec{Path: root + "/clickhouse", Mode: "755", Owner: "101:101", NewOnly: true},
			DirSpec{Path: root + "/milvus", Mode: "755", Owner: "999:999", Recurse: true},
			DirSpec{Path: root + "/flink", Mode: "755", Owner: "9999:9999", Recurse: true},
			DirSpec{Path: root + "/spool", Mode: "755", Owner: "10001:10001", Recurse: true},
			DirSpec{Path: root + "/spool/decision-1", Mode: "755", Owner: "10001:10001", Recurse: true},
			DirSpec{Path: root + "/spool/decision-2", Mode: "755", Owner: "10001:10001", Recurse: true},
			DirSpec{Path: root + "/spool/admin", Mode: "755", Owner: "10001:10001", Recurse: true},
		)
		return dirs
	}
	return nil
}

// PrepareDirsScript 建目录、设属主的脚本（幂等；已有目录不改权限，只有 Recurse 的会每次 chown）
func PrepareDirsScript(dirs []DirSpec) string {
	var b strings.Builder
	for _, d := range dirs {
		p := ShellQuote(d.Path)
		fmt.Fprintf(&b, "if [ ! -d %s ]; then mkdir -p %s && chmod %s %s", p, p, d.Mode, p)
		if d.Owner != "" && (d.NewOnly || d.Recurse) {
			fmt.Fprintf(&b, " && chown %s %s", d.Owner, p)
		}
		fmt.Fprintf(&b, " && echo \"新建 %s\"; fi\n", d.Path)
		if d.Recurse && d.Owner != "" {
			fmt.Fprintf(&b, "chown -R %s %s\n", d.Owner, p)
		}
	}
	return b.String()
}

// NatsConfig 首次生成的 NATS 配置（账户 APP，与现网结构相同）
func NatsConfig(user, password string) string {
	return fmt.Sprintf(`# 由 aegisctl init 生成；之后 aegisctl 不再覆盖本文件
server_name: aegis-nats
listen: 0.0.0.0:4222
http: 0.0.0.0:8222
max_payload: 8MB

jetstream {
  store_dir: /data
  max_file: 8G
  max_mem: 1G
}

accounts {
  APP {
    jetstream: enabled
    users: [
      { user: %q, password: %q }
    ]
  }
}
`, user, password)
}
