package main

import (
	"net/url"
	"path"
	"sort"
	"strings"
)

// importer.go：从现网 Dokploy（Swarm 服务 + compose 容器）的实际运行配置里取值，填进 cluster.env。
// 读的是正在运行的服务与容器（docker inspect），而不是 Dokploy 库里加密的环境变量：运行中的才是真正生效的。

// mountInfo 挂载
type mountInfo struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"` // 容器
	Target      string `json:"Target"`      // Swarm 服务
}

func (m mountInfo) dest() string {
	if m.Destination != "" {
		return m.Destination
	}
	return m.Target
}

// runningUnit 一个现网服务或容器
type runningUnit struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Env      []string          `json:"env"`
	Mounts   []mountInfo       `json:"mounts"`
	Labels   map[string]string `json:"labels"`
	Networks map[string]struct {
		Aliases []string `json:"Aliases"`
	} `json:"networks"`
	Swarm bool `json:"-"`
}

func (u runningUnit) envMap() map[string]string {
	m := map[string]string{}
	for _, kv := range u.Env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func (u runningUnit) hasAlias(alias string) bool {
	for _, n := range u.Networks {
		for _, a := range n.Aliases {
			if a == alias {
				return true
			}
		}
	}
	return u.Labels["com.docker.compose.service"] == alias
}

func (u runningUnit) mountAt(dest string) (mountInfo, bool) {
	for _, m := range u.Mounts {
		if m.dest() == dest {
			return m, true
		}
	}
	return mountInfo{}, false
}

// importScript 列出 manager 上全部 Swarm 服务与容器的配置（含密钥，只经 ssh 传到本机内存，不落盘）
const importScript = `echo '@@SERVICES'
for s in $(docker service ls --format '{{.Name}}'); do
  docker service inspect --format '{"name":{{json .Spec.Name}},"image":{{json .Spec.TaskTemplate.ContainerSpec.Image}},"env":{{json .Spec.TaskTemplate.ContainerSpec.Env}},"mounts":{{json .Spec.TaskTemplate.ContainerSpec.Mounts}},"labels":{{json .Spec.Labels}}}' "$s"
done
echo '@@CONTAINERS'
for c in $(docker ps -q); do
  docker inspect --format '{"name":{{json .Name}},"image":{{json .Config.Image}},"env":{{json .Config.Env}},"mounts":{{json .Mounts}},"labels":{{json .Config.Labels}},"networks":{{json .NetworkSettings.Networks}}}' "$c"
done
`

// ImportResult 导入结果
type ImportResult struct {
	Values map[string]string
	// ExtraOrder 现网 aegis-api 里模板没有的键（按名字排序），追加到文件末尾
	ExtraOrder []string
	Notes      []string
}

// 栈文件给 aegis-api 覆盖的键：不从现网导入（地址以栈为准）
var stackOverriddenKeys = map[string]bool{
	"POSTGRES_DSN": true, "REDIS_ADDR": true, "REDIS_PASSWORD": true, "NATS_URL": true, "TEMPORAL_HOST_PORT": true,
	"RISK_ETCD_ENDPOINTS": true, "MUSIC_RPC_URL": true, "LISTEN_TOGETHER_RPC_URL": true, "MIGRATE_ON_BOOT": true,
	"AEGIS_ROLE": true, "AEGIS_NODE_ID": true, "HTTP_PORT": true, "WORKER_HEALTH_ADDR": true, "TZ": true,
}

// ImportOptions 识别现网服务的方式
type ImportOptions struct {
	APIService string // 现网 aegis-api 的 Swarm 服务名（Dokploy 的 appName）；空则按前缀 aegis-api 找
	LTService  string // 现网一起听的服务名；空则按前缀 listen-together 找
	Stack      string // 统一栈的名字：它的服务不当作现网
}

// ClassifyImport 从 docker inspect 的结果里取值
func ClassifyImport(services, containers []runningUnit, opt ImportOptions) ImportResult {
	res := ImportResult{Values: map[string]string{}}
	set := func(k, v string) {
		if v != "" {
			res.Values[k] = v
		}
	}
	note := func(s string) { res.Notes = append(res.Notes, s) }

	findService := func(explicit, prefix string) *runningUnit {
		for i := range services {
			s := &services[i]
			if strings.HasPrefix(s.Name, opt.Stack+"_") {
				continue
			}
			if (explicit != "" && s.Name == explicit) || (explicit == "" && strings.HasPrefix(s.Name, prefix)) {
				return s
			}
		}
		return nil
	}
	findContainer := func(alias string) *runningUnit {
		for i := range containers {
			c := &containers[i]
			if strings.HasPrefix(c.Labels["com.docker.swarm.service.name"], opt.Stack+"_") {
				continue
			}
			if c.hasAlias(alias) {
				return c
			}
		}
		return nil
	}

	// ── 主服务 ──
	if api := findService(opt.APIService, "aegis-api"); api != nil {
		env := api.envMap()
		var extra []string
		for k, v := range env {
			if stackOverriddenKeys[k] {
				continue
			}
			set(k, v)
			extra = append(extra, k)
		}
		sort.Strings(extra)
		res.ExtraOrder = extra
		if dsn := env["POSTGRES_DSN"]; dsn != "" {
			if u, err := url.Parse(dsn); err == nil {
				set("AEGIS_PG_USER", u.User.Username())
				if p, ok := u.User.Password(); ok {
					set("AEGIS_PG_PASSWORD", p)
				}
				set("AEGIS_PG_DB", strings.TrimPrefix(u.Path, "/"))
				set("AEGIS_PG_DSN_PARAMS", u.RawQuery)
				if h := u.Hostname(); h != "aegis-postgres" {
					note("现网 POSTGRES_DSN 的主机是 " + h + "，统一栈固定用 aegis-postgres")
				}
			}
		}
		if u, err := url.Parse(env["NATS_URL"]); err == nil && u.User != nil {
			set("AEGIS_NATS_USER", u.User.Username())
			if p, ok := u.User.Password(); ok {
				set("AEGIS_NATS_PASSWORD", p)
			}
		}
		set("AEGIS_REDIS_PASSWORD", env["REDIS_PASSWORD"])
		if m, ok := api.mountAt("/app/data"); ok {
			note("现网 aegis-api 的 /app/data 绑定在 " + m.Source + "：统一栈改用每个副本自己的命名卷，上传文件先按手册迁到对象存储")
		}
	} else {
		note("没找到现网 aegis-api 服务（名字以 aegis-api 开头）；可用 --api-service 指定")
	}

	// ── 一起听 ──
	if lt := findService(opt.LTService, "listen-together"); lt != nil {
		env := lt.envMap()
		if res.Values["LISTEN_TOGETHER_INTERNAL_TOKEN"] == "" {
			set("LISTEN_TOGETHER_INTERNAL_TOKEN", env["LT_INTERNAL_TOKEN"])
		}
		set("LISTEN_TOGETHER_ALLOWED_ORIGINS", env["LT_ALLOWED_ORIGINS"])
		for k, v := range env {
			switch k {
			case "LT_REDIS_URL", "LT_NATS_URL", "LT_TICKET_JWKS_URL", "LT_INTERNAL_TOKEN", "LT_ALLOWED_ORIGINS", "LT_PUBLIC_ADDR", "LT_INTERNAL_ADDR":
				continue
			}
			if strings.HasPrefix(k, "LT_") {
				set(k, v)
				res.ExtraOrder = append(res.ExtraOrder, k)
			}
		}
	}

	// ── 控制台 ──
	for i := range services {
		s := services[i]
		if !strings.HasPrefix(s.Name, "aegis-console") {
			continue
		}
		var pub []string
		for k := range s.envMap() {
			if strings.HasPrefix(k, "NEXT_PUBLIC_") {
				pub = append(pub, k)
			}
		}
		sort.Strings(pub)
		if len(pub) > 0 {
			note("现网控制台设了构建期变量 " + strings.Join(pub, "、") + "：console.Dockerfile 目前不接收它们，需要时请先给 Dockerfile 加 ARG")
		}
	}

	// ── 基础设施容器 ──
	if pg := findContainer("aegis-postgres"); pg != nil {
		env := pg.envMap()
		set("AEGIS_PG_IMAGE", pg.Image)
		if res.Values["AEGIS_PG_USER"] == "" {
			set("AEGIS_PG_USER", env["POSTGRES_USER"])
			set("AEGIS_PG_PASSWORD", env["POSTGRES_PASSWORD"])
			set("AEGIS_PG_DB", env["POSTGRES_DB"])
		}
		data := "/var/lib/postgresql/data"
		if v := env["PGDATA"]; v != "" {
			data = v
		}
		if m, ok := pg.mountAt(data); ok && m.Type == "bind" {
			set("AEGIS_PG_DIR", m.Source)
		} else {
			note("现网 Postgres 的数据目录不是绑定挂载（或 PGDATA 不在 " + data + "）：请手工核对 AEGIS_PG_DIR")
		}
	} else {
		note("没找到别名为 aegis-postgres 的容器")
	}
	if rd := findContainer("aegis-redis"); rd != nil {
		set("AEGIS_REDIS_IMAGE", rd.Image)
		if m, ok := rd.mountAt("/data"); ok && m.Type == "bind" {
			set("AEGIS_REDIS_DIR", m.Source)
		} else {
			note("现网 Redis 的 /data 不是绑定挂载：请手工核对 AEGIS_REDIS_DIR")
		}
		pw, rest := splitRequirePass(rd.envMap()["REDIS_ARGS"])
		if res.Values["AEGIS_REDIS_PASSWORD"] == "" {
			set("AEGIS_REDIS_PASSWORD", pw)
		}
		set("AEGIS_REDIS_EXTRA_ARGS", rest)
	}
	if nt := findContainer("aegis-nats"); nt != nil {
		set("AEGIS_NATS_IMAGE", nt.Image)
		var conf, data *mountInfo
		for i := range nt.Mounts {
			m := nt.Mounts[i]
			if strings.HasSuffix(m.dest(), ".conf") {
				conf = &m
			} else if m.Type == "bind" || m.Type == "volume" {
				data = &m
			}
		}
		if conf != nil {
			set("AEGIS_NATS_CONF", conf.Source)
		} else {
			note("现网 NATS 没有挂配置文件：统一栈要求 AEGIS_NATS_CONF，init 会按 AEGIS_NATS_USER / PASSWORD 生成一份")
		}
		if data != nil {
			if data.Type != "bind" {
				note("现网 NATS 数据在命名卷 " + data.Source + " 里，统一栈用绑定目录：切换前把卷内容拷到 AEGIS_NATS_DATA")
			} else {
				set("AEGIS_NATS_DATA", data.Source)
			}
			if data.dest() != "/data" {
				note("现网 NATS 数据挂在容器的 " + data.dest() + "，统一栈挂在 /data：请把 nats-server.conf 的 store_dir 改到 /data 下对应位置")
			}
		}
	}
	if tp := findContainer("aegis-temporal"); tp != nil {
		env := tp.envMap()
		set("AEGIS_TEMPORAL_IMAGE", tp.Image)
		set("TEMPORAL_DB_USER", env["POSTGRES_USER"])
		set("TEMPORAL_DB_PASSWORD", env["POSTGRES_PWD"])
		set("TEMPORAL_DB_NAME", env["DBNAME"])
		set("TEMPORAL_VISIBILITY_DB_NAME", env["VISIBILITY_DBNAME"])
		if seeds := env["POSTGRES_SEEDS"]; seeds != "" && seeds != "aegis-postgres" {
			note("现网 Temporal 连的数据库是 " + seeds + "，统一栈固定连 aegis-postgres")
		}
	}
	for i := range containers {
		c := containers[i]
		if strings.Contains(c.Image, "temporalio/ui") && !strings.HasPrefix(c.Labels["com.docker.swarm.service.name"], opt.Stack+"_") {
			set("AEGIS_TEMPORAL_UI_IMAGE", c.Image)
		}
	}

	// ── 风控 ──
	if ra := findContainer("risk-admin"); ra != nil {
		env := ra.envMap()
		for _, k := range []string{"RISK_REDIS_PASSWORD", "RISK_CLICKHOUSE_PASSWORD", "RISK_NEBULA_PASSWORD", "RISK_INTERNAL_TOKEN", "RISK_PROXY_CIDRS", "RISK_STREAM_MAX_BYTES"} {
			set(k, env[k])
		}
		// 沿用现网 risk-minio 的根凭据：objstore migrate 起临时 MinIO 读旧目录要用它，RustFS 也用同一对，
		// 搬过去后风控与 Milvus 的凭据不用改
		set("RISK_RUSTFS_ACCESS_KEY", env["RISK_S3_ACCESS_KEY"])
		set("RISK_RUSTFS_SECRET_KEY", env["RISK_S3_SECRET_KEY"])
		if u, err := url.Parse(env["RISK_PG_DSN"]); err == nil && u.User != nil {
			if user := u.User.Username(); user != "" && res.Values["AEGIS_PG_USER"] != "" && user != res.Values["AEGIS_PG_USER"] {
				note("现网风控用 Postgres 账号 " + user + "，统一栈改用主库账号 " + res.Values["AEGIS_PG_USER"] + "：确认它对库 aegis_risk 有权限")
			}
		}
	}
	if et := findContainer("risk-etcd"); et != nil {
		if m, ok := et.mountAt("/etcd-data"); ok && m.Type == "bind" {
			set("AEGIS_RISK_DIR", path.Dir(m.Source))
		}
	}
	if mr := findContainer("music-rec"); mr != nil {
		env := mr.envMap()
		set("MUSIC_INTERNAL_TOKEN", env["MUSIC_INTERNAL_TOKEN"])
		if p := env["MUSIC_CLICKHOUSE_PASSWORD"]; p != "" && res.Values["RISK_CLICKHOUSE_PASSWORD"] != "" && p != res.Values["RISK_CLICKHOUSE_PASSWORD"] {
			note("现网音乐服务的 ClickHouse 密码与风控的不同：统一栈统一用 RISK_CLICKHOUSE_PASSWORD")
		}
	}
	return res
}

// splitRequirePass 从 redis 参数里取出 --requirepass 的值，返回其余参数
func splitRequirePass(args string) (password, rest string) {
	f := strings.Fields(args)
	var keep []string
	for i := 0; i < len(f); i++ {
		if f[i] == "--requirepass" && i+1 < len(f) {
			password = strings.Trim(f[i+1], `"'`)
			i++
			continue
		}
		keep = append(keep, f[i])
	}
	return password, strings.Join(keep, " ")
}
