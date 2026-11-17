package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// cmd_doctor.go：体检。只读，不改动任何东西。

// Check 一项检查
type Check struct {
	Level  string // ok / warn / fail
	Name   string
	Detail string
}

type checks []Check

func (c *checks) add(level, name, format string, args ...any) {
	*c = append(*c, Check{Level: level, Name: name, Detail: fmt.Sprintf(format, args...)})
}

// RenderChecks 输出检查结果，返回失败项数
func RenderChecks(cs []Check, p Palette) (string, int) {
	var b strings.Builder
	fails := 0
	for _, c := range cs {
		mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗"}[c.Level]
		if c.Level == "fail" {
			fails++
		}
		fmt.Fprintf(&b, "%s %-14s %s\n", p.Level(c.Level, mark), c.Name, c.Detail)
	}
	return b.String(), fails
}

// CheckLabels 标签齐不齐：每个标签至少一个节点，有状态标签至多一个
func CheckLabels(nodes []NodeInfo) []Check {
	var cs checks
	for _, l := range AllLabels {
		var on []string
		for _, n := range nodes {
			if n.Spec.Labels[l] == "true" {
				on = append(on, n.Description.Hostname)
			}
		}
		stateful := l != "aegis.app"
		switch {
		case len(on) == 0:
			cs.add("fail", "节点标签", "没有节点打 %s=true，依赖它的服务无法调度（aegisctl node label <节点> %s=true）", l, l)
		case stateful && len(on) > 1:
			cs.add("warn", "节点标签", "%s 打在 %d 个节点上（%s）：有状态数据是节点本地目录，应只留一个", l, len(on), strings.Join(on, "、"))
		default:
			cs.add("ok", "节点标签", "%s：%s", l, strings.Join(on, "、"))
		}
	}
	return cs
}

// CheckEnv cluster.env 的完整性：必填键、栈文件能否渲染、引号、Nebula 密码长度、连接数预算
func CheckEnv(f *EnvFile, tmpl *EnvFile, stackTmpl string) []Check {
	var cs checks
	env := f.Map()
	var empty []string
	required := map[string]bool{}
	for _, k := range tmpl.RequiredKeys() {
		required[k] = true
	}
	for _, k := range f.RequiredKeys() {
		required[k] = true
	}
	for k := range required {
		if strings.TrimSpace(env[k]) == "" {
			empty = append(empty, k)
		}
	}
	sort.Strings(empty)
	if len(empty) > 0 {
		cs.add("fail", "cluster.env", "必填键为空：%s", strings.Join(empty, " "))
	} else {
		cs.add("ok", "cluster.env", "%d 个键，必填项齐全", len(f.Keys()))
	}
	var missing []string
	for _, k := range tmpl.Keys() {
		if _, ok := f.Get(k); !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		cs.add("warn", "cluster.env", "模板里有、线上没有的键（aegisctl init 可补）：%s", strings.Join(missing, " "))
	}
	if q := QuotedValueKeys(f); len(q) > 0 {
		cs.add("warn", "cluster.env", "值两端带引号（docker 不去引号）：%s", strings.Join(q, " "))
	}
	vars := map[string]string{}
	for _, im := range Images {
		vars[im.TagVar] = "doctor"
	}
	vars[migrateTagVar] = "doctor"
	if _, err := renderStack(stackTmpl, env, vars, nil); err != nil {
		cs.add("fail", "栈文件", "%v", err)
	} else {
		cs.add("ok", "栈文件", "全部变量可解析")
	}
	if n := len([]rune(env["RISK_NEBULA_PASSWORD"])); n > 24 {
		cs.add("fail", "cluster.env", "RISK_NEBULA_PASSWORD 有 %d 个字符，Nebula 上限 24", n)
	}
	for _, k := range []string{"AEGIS_PG_PASSWORD", "AEGIS_REDIS_PASSWORD", "AEGIS_NATS_PASSWORD", "RISK_REDIS_PASSWORD", "RISK_CLICKHOUSE_PASSWORD", "AEGIS_MINIO_ROOT_PASSWORD"} {
		v := env[k]
		if v != "" && url.PathEscape(v) != v {
			cs.add("fail", "cluster.env", "%s 含 URL 特殊字符，拼进 DSN 后会解析错（只用字母数字）", k)
		}
	}
	for _, k := range []string{"JWT_SECRET", "SECURITY_MASTER_KEY", "ADMIN_API_TOKEN"} {
		if v := env[k]; strings.HasPrefix(v, "change-") {
			cs.add("fail", "cluster.env", "%s 还是示例值", k)
		}
	}
	return cs
}

// ConnBudget Postgres 连接预算：(api + worker + 滚动时多出的 2 个) × 每实例上限
func ConnBudget(env map[string]string, maxConnections int) Check {
	atoi := func(k string, def int) int {
		if n, err := strconv.Atoi(env[k]); err == nil {
			return n
		}
		return def
	}
	per := atoi("POSTGRES_MAX_CONNS", 40)
	inst := atoi("AEGIS_API_REPLICAS", 2) + atoi("AEGIS_WORKER_REPLICAS", 2) + 2
	need := inst * per
	// 风控、音乐、Temporal 也连这个库，留 40 条余量
	if need+40 > maxConnections {
		return Check{Level: "warn", Name: "连接预算", Detail: fmt.Sprintf("%d 个实例 × POSTGRES_MAX_CONNS %d = %d，加其他服务约 40，超过 max_connections %d", inst, per, need, maxConnections)}
	}
	return Check{Level: "ok", Name: "连接预算", Detail: fmt.Sprintf("%d 个实例 × %d = %d（max_connections %d）", inst, per, need, maxConnections)}
}

// ParseDF 解析 df -P 的输出：挂载点 → 使用率
func ParseDF(out string) map[string]int {
	res := map[string]int{}
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 6 {
			continue
		}
		pct, err := strconv.Atoi(strings.TrimSuffix(f[4], "%"))
		if err == nil {
			res[f[5]] = pct
		}
	}
	return res
}

func (a *App) cmdDoctor(ctx context.Context, args []string) error {
	if _, err := parseFlags(newFlagSet("doctor"), args); err != nil {
		return err
	}
	var cs checks
	// Swarm
	info, err := a.capture(ctx, `docker info --format '{{.Swarm.LocalNodeState}}|{{.Swarm.ControlAvailable}}|{{.ServerVersion}}|{{.Swarm.Managers}}|{{.Swarm.Nodes}}'`)
	if err != nil {
		cs.add("fail", "Swarm", "连不上 %s：%v", a.manager(), err)
		out, _ := RenderChecks(cs, a.pal)
		fmt.Fprint(a.out, out)
		return fmt.Errorf("体检未通过")
	}
	f := strings.Split(strings.TrimSpace(info), "|")
	if len(f) >= 5 && f[0] == "active" && f[1] == "true" {
		cs.add("ok", "Swarm", "Docker %s，%s 个 manager，%s 个节点", f[2], f[3], f[4])
		if f[3] == "2" {
			cs.add("warn", "Swarm", "2 个 manager 比 1 个更脆弱（任一失联即失去多数）：用 1 个或 3 个")
		}
	} else {
		cs.add("fail", "Swarm", "%s 不是可用的 manager（%s）", a.manager(), strings.TrimSpace(info))
	}

	nodes, err := a.loadNodes(ctx)
	if err == nil {
		for _, n := range nodes.infos {
			if n.Status.State != "ready" {
				cs.add("fail", "节点", "%s 状态 %s", n.Description.Hostname, n.Status.State)
			} else if n.Spec.Availability != "active" {
				cs.add("warn", "节点", "%s 可调度状态 %s", n.Description.Hostname, n.Spec.Availability)
			}
		}
		cs = append(cs, CheckLabels(nodes.infos)...)
	}

	// cluster.env 与栈文件
	envFile, _, envErr := a.remoteEnv(ctx)
	var env map[string]string
	if envErr != nil {
		cs.add("fail", "cluster.env", "%v", envErr)
	} else {
		env = envFile.Map()
		repo, err := a.repo()
		if err != nil {
			cs.add("warn", "栈文件", "%v", err)
		} else {
			tmplText, err1 := readRepoFile(repo, envTmplRelPath)
			stackText, err2 := readRepoFile(repo, stackRelPath)
			if err1 != nil || err2 != nil {
				cs.add("warn", "栈文件", "读不到本机模板")
			} else {
				tmpl, _ := ParseEnv(tmplText)
				cs = append(cs, CheckEnv(envFile, tmpl, stackText)...)
			}
			if trTmpl, err := readRepoFile(repo, traefikRelPath); err == nil {
				if _, err := RenderTraefik(trTmpl, env); err != nil {
					cs.add("fail", "Traefik", "%v", err)
				}
			}
		}
	}

	// 网络
	netOut, err := a.capture(ctx, fmt.Sprintf(`for n in %s %s; do docker network inspect --format '{{.Name}} {{.Driver}} {{.Attachable}} {{index .Options "encrypted"}}' "$n" 2>/dev/null || echo "$n missing"; done`, internalNetwork, publicNetwork))
	if err == nil {
		for _, l := range strings.Split(strings.TrimSpace(netOut), "\n") {
			p := strings.Fields(l)
			switch {
			case len(p) >= 2 && p[1] == "missing":
				cs.add("fail", "网络", "没有 %s（aegisctl init 会建）", p[0])
			case len(p) >= 3 && (p[1] != "overlay" || p[2] != "true"):
				cs.add("fail", "网络", "%s 不是可挂载的 overlay（%s）", p[0], l)
			case len(p) >= 3:
				enc := ""
				if len(p) >= 4 {
					enc = "，已加密"
				}
				cs.add("ok", "网络", "%s overlay%s", p[0], enc)
			}
		}
	}

	// 各节点：磁盘、时钟、数据目录
	if nodes.infos != nil {
		self := nodes.selfHostname()
		for _, n := range nodes.infos {
			if n.Status.State != "ready" {
				continue
			}
			host := a.cfg.HostForNode(n.Description.Hostname, self)
			name := n.Description.Hostname
			var dirs []DirSpec
			if env != nil {
				for _, l := range StatefulLabels {
					if n.Spec.Labels[l] == "true" {
						dirs = append(dirs, DirsForLabel(l, env)...)
					}
				}
			}
			var probe strings.Builder
			probe.WriteString("echo '@@DATE'; date +%s.%N\necho '@@DF'; df -P /srv /var/lib/docker 2>/dev/null || df -P /\necho '@@DIRS'\n")
			for _, d := range dirs {
				fmt.Fprintf(&probe, "[ -d %s ] || echo %s\n", ShellQuote(d.Path), ShellQuote(d.Path))
			}
			if n.Spec.Labels["aegis.db"] == "true" && env != nil {
				c := firstNonEmpty(env["AEGIS_NATS_CONF"], "/srv/aegis/nats/nats-server.conf")
				fmt.Fprintf(&probe, "[ -f %s ] || echo %s\n", ShellQuote(c), ShellQuote(c))
			}
			t0 := time.Now()
			out, err := Capture(ctx, a.ex, host, probe.String())
			t1 := time.Now()
			if err != nil {
				cs.add("warn", "节点 "+name, "ssh %s 不通，跳过磁盘与时钟检查（node join 时会记录 ssh 目标）", host)
				continue
			}
			sec := Sections(out)
			if remote, err := strconv.ParseFloat(strings.TrimSpace(sec["DATE"]), 64); err == nil {
				mid := float64(t0.UnixNano()+t1.UnixNano()) / 2 / 1e9
				off := remote - mid
				switch {
				case math.Abs(off) > 30:
					cs.add("fail", "时钟", "%s 与本机相差 %.1f 秒（Aegis 请求时间戳校验会失败，装 chrony 同步）", name, off)
				case math.Abs(off) > 2:
					cs.add("warn", "时钟", "%s 与本机相差 %.1f 秒", name, off)
				default:
					cs.add("ok", "时钟", "%s 偏差 %.2f 秒", name, off)
				}
			}
			for mount, pct := range ParseDF(sec["DF"]) {
				switch {
				case pct >= 90:
					cs.add("fail", "磁盘", "%s 的 %s 已用 %d%%", name, mount, pct)
				case pct >= 80:
					cs.add("warn", "磁盘", "%s 的 %s 已用 %d%%", name, mount, pct)
				default:
					cs.add("ok", "磁盘", "%s 的 %s 已用 %d%%", name, mount, pct)
				}
			}
			if miss := strings.Fields(sec["DIRS"]); len(miss) > 0 {
				cs.add("fail", "数据目录", "%s 上缺少：%s（aegisctl node label 重新打标签会建）", name, strings.Join(miss, " "))
			} else if len(dirs) > 0 {
				cs.add("ok", "数据目录", "%s 上 %d 个目录齐全", name, len(dirs))
			}
		}
	}

	// 镜像与仓库登录
	if env != nil {
		registry := strings.TrimSuffix(env["AEGIS_REGISTRY"], "/")
		st, err := a.loadState(ctx)
		if err == nil {
			cur := CurrentTags(st)
			var b strings.Builder
			host := strings.SplitN(registry, "/", 2)[0]
			fmt.Fprintf(&b, "grep -q %s ~/.docker/config.json 2>/dev/null && echo login-ok || echo login-missing\n", ShellQuote(`"`+host+`"`))
			for _, im := range Images {
				if t := cur[im.TagVar]; t != "" {
					ref := registry + "/" + im.Name + ":" + t
					fmt.Fprintf(&b, "docker manifest inspect %s >/dev/null 2>&1 && echo 'ok %s' || echo 'missing %s'\n", ShellQuote(ref), ref, ref)
				}
			}
			out, err := a.capture(ctx, b.String())
			if err == nil {
				for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
					switch {
					case l == "login-ok":
						cs.add("ok", "镜像仓库", "manager 已登录 %s", host)
					case l == "login-missing":
						cs.add("warn", "镜像仓库", "manager 的 ~/.docker/config.json 里没有 %s（私有镜像要 docker login）", host)
					case strings.HasPrefix(l, "missing "):
						cs.add("fail", "镜像", "拉不到 %s（节点重建任务时会失败）", strings.TrimPrefix(l, "missing "))
					}
				}
				if !strings.Contains(out, "missing ") && len(cur) > 0 {
					cs.add("ok", "镜像", "现网 %d 个镜像标签都可拉取", len(cur))
				}
			}
			if len(st.infos) == 0 {
				cs.add("warn", "栈", "栈 %s 还没有部署", a.stack())
			}
		}

		// Traefik
		dst := traefikPath(env)
		var domains []string
		for _, k := range []string{"AEGIS_API_DOMAINS", "AEGIS_CONSOLE_DOMAINS", "AEGIS_LISTEN_TOGETHER_DOMAINS"} {
			domains = append(domains, splitList(env[k])...)
		}
		var tb strings.Builder
		fmt.Fprintf(&tb, "[ -f %[1]s ] && echo file-ok || echo file-missing\n", ShellQuote(dst))
		tb.WriteString("docker ps --filter name=dokploy-traefik --format '{{.Names}} {{.Status}}' | head -n1 | sed 's/^/traefik /'\n")
		fmt.Fprintf(&tb, "d=$(dirname %s)\n", ShellQuote(dst))
		for _, dm := range domains {
			fmt.Fprintf(&tb, "for f in $(grep -l -F %s \"$d\"/*.yml \"$d\"/*.yaml 2>/dev/null); do [ \"$f\" = %s ] || echo \"other %s $(basename \"$f\")\"; done\n", ShellQuote("`"+dm+"`"), ShellQuote(dst), dm)
		}
		if out, err := a.capture(ctx, tb.String()); err == nil {
			others := map[string][]string{}
			for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
				p := strings.Fields(l)
				switch {
				case l == "file-ok":
					cs.add("ok", "Traefik", "%s 存在", dst)
				case l == "file-missing":
					cs.add("warn", "Traefik", "%s 不存在（aegisctl deploy 成功后写入，或 aegisctl traefik apply）", dst)
				case len(p) >= 3 && p[0] == "traefik":
					cs.add("ok", "Traefik", "%s %s", p[1], strings.Join(p[2:], " "))
				case len(p) == 3 && p[0] == "other":
					others[p[2]] = append(others[p[2]], p[1])
				}
			}
			if !strings.Contains(out, "traefik ") {
				cs.add("warn", "Traefik", "manager 上没有运行中的 dokploy-traefik 容器")
			}
			for file, ds := range others {
				cs.add("warn", "Traefik", "%s 里也有 %s 的路由（新栈优先级 %s 会先匹配；切换稳定后可在 Dokploy 删掉旧域名）", file, strings.Join(ds, "、"), firstNonEmpty(env["AEGIS_TRAEFIK_PRIORITY"], "1000"))
			}
		}

		// Postgres 连接预算
		if node, cid, err := a.locateTask(ctx, "postgres"); err == nil {
			host := a.cfg.HostForNode(node, nodes.selfHostname())
			out, err := Capture(ctx, a.ex, host, "docker exec "+ShellQuote(cid)+` sh -c 'psql -U "$POSTGRES_USER" -d postgres -tAc "show max_connections"'`)
			if n, perr := strconv.Atoi(strings.TrimSpace(out)); err == nil && perr == nil {
				cs = append(cs, ConnBudget(env, n))
			}
		}
	}

	out, fails := RenderChecks(cs, a.pal)
	fmt.Fprint(a.out, out)
	if fails > 0 {
		return fmt.Errorf("体检发现 %d 个问题", fails)
	}
	fmt.Fprintln(a.out, a.pal.Green("体检通过"))
	return nil
}
