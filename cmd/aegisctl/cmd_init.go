package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"strings"
)

// cmd_init.go：aegisctl init —— 准备 manager。可重复执行：已有的东西不覆盖，只补缺。

func (a *App) cmdInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	importDokploy := fs.Bool("import-dokploy", false, "从现网 Dokploy 正在运行的服务与容器导入配置、数据目录与镜像版本")
	apiService := fs.String("api-service", "", "现网 aegis-api 的 Swarm 服务名（默认按前缀 aegis-api 找）")
	ltService := fs.String("lt-service", "", "现网一起听的 Swarm 服务名（默认按前缀 listen-together 找）")
	advertise := fs.String("advertise-addr", "", "manager 还没加入 Swarm 时，docker swarm init 用的通告地址")
	dryRun := fs.Bool("dry-run", false, "只显示将要做的事，不改动 manager")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	tmplText, err := readRepoFile(repo, envTmplRelPath)
	if err != nil {
		return err
	}
	tmpl, problems := ParseEnv(tmplText)
	if len(problems) > 0 {
		return fmt.Errorf("模板 %s 有问题：%s", envTmplRelPath, strings.Join(problems, "；"))
	}

	// 1. Swarm
	a.step("检查 %s 上的 Docker 与 Swarm", a.manager())
	info, err := a.capture(ctx, `docker info --format '{{.Swarm.LocalNodeState}}|{{.Swarm.ControlAvailable}}|{{.Name}}'`)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimSpace(info), "|")
	if len(parts) < 3 {
		return fmt.Errorf("看不懂 docker info 的输出：%q", info)
	}
	switch {
	case parts[0] == "active" && parts[1] == "true":
		a.infof("  Swarm 已启用，%s 是 manager", parts[2])
	case parts[0] == "active":
		return fmt.Errorf("%s 在 Swarm 里但不是 manager：请改用 manager 节点（--manager）", a.manager())
	default:
		a.infof("  Swarm 未启用，将执行 docker swarm init")
		if !*dryRun {
			cmd := "docker swarm init"
			if *advertise != "" {
				cmd += " --advertise-addr " + ShellQuote(*advertise)
			}
			if _, err := a.capture(ctx, cmd+" >/dev/null"); err != nil {
				return fmt.Errorf("docker swarm init 失败（多网卡时用 --advertise-addr 指定内网地址）：%w", err)
			}
		}
	}

	// 2. cluster.env
	a.step("生成 / 补齐 %s", remoteEnvFile)
	existingText, err := a.capture(ctx, fmt.Sprintf("install -d -m 700 %[1]s; if [ -f %[2]s ]; then echo present; cat %[2]s; else echo absent; fi", remoteConfDir, remoteEnvFile))
	if err != nil {
		return err
	}
	status, body, _ := strings.Cut(existingText, "\n")
	var existing *EnvFile
	if strings.TrimSpace(status) == "present" {
		existing, _ = ParseEnv(body)
		a.infof("  已有 %s（%d 个键），只补缺失的键", remoteEnvFile, len(existing.Keys()))
	} else {
		a.infof("  %s 还不存在，按模板新建", remoteEnvFile)
	}
	imported := ImportResult{Values: map[string]string{}}
	if *importDokploy {
		a.step("从现网 Dokploy 导入")
		out, err := a.capture(ctx, importScript)
		if err != nil {
			return err
		}
		sec := Sections(out)
		services, err := ParseJSONLines[runningUnit](sec["SERVICES"])
		if err != nil {
			return fmt.Errorf("解析服务列表失败：%w", err)
		}
		containers, err := ParseJSONLines[runningUnit](sec["CONTAINERS"])
		if err != nil {
			return fmt.Errorf("解析容器列表失败：%w", err)
		}
		imported = ClassifyImport(services, containers, ImportOptions{APIService: *apiService, LTService: *ltService, Stack: a.stack()})
		a.infof("  从 %d 个服务、%d 个容器里取到 %d 个值", len(services), len(containers), len(imported.Values))
		for _, n := range imported.Notes {
			a.warnf("%s", n)
		}
	}
	note := "（" + a.now().Format("2006-01-02 15:04") + "）"
	merged, rep, err := MergeClusterEnv(tmpl, existing, imported.Values, imported.ExtraOrder, rand.Reader, note)
	if err != nil {
		return err
	}
	printKeyList(a, "按模板补上", rep.Added)
	printKeyList(a, "自动生成", rep.Generated)
	printKeyList(a, "取自现网", rep.Imported)
	printKeyList(a, "取自同文件其他键", rep.Copied)
	printKeyList(a, "现网多出的键（追加在末尾）", rep.Extra)
	if n := len([]rune(merged.Value("RISK_NEBULA_PASSWORD"))); n > 24 {
		a.warnf("RISK_NEBULA_PASSWORD 有 %d 个字符，超过 Nebula 的 24 字符上限", n)
	}
	changed := existing == nil || merged.String() != existing.String()
	if !*dryRun && changed {
		if err := a.writeRemoteEnv(ctx, merged); err != nil {
			return err
		}
		a.infof("  已写入 %s（600）", remoteEnvFile)
	} else if !changed {
		a.infof("  没有需要补的键")
	}
	env := merged.Map()

	// 3. 数据目录、NATS 配置、备份目录
	a.step("准备 manager 上的数据目录")
	var dirs []DirSpec
	for _, l := range StatefulLabels {
		dirs = append(dirs, DirsForLabel(l, env)...)
	}
	dirs = append(dirs, DirSpec{Path: firstNonEmpty(env["AEGIS_BACKUP_DIR"], "/srv/aegis/backups"), Mode: "700"})
	script := PrepareDirsScript(dirs)
	conf := firstNonEmpty(env["AEGIS_NATS_CONF"], "/srv/aegis/nats/nats-server.conf")
	natsConf := NatsConfig(env["AEGIS_NATS_USER"], env["AEGIS_NATS_PASSWORD"])
	script += fmt.Sprintf("if [ ! -f %s ]; then\n%secho \"新建 %s\"\nfi\n", ShellQuote(conf), WriteFileScript(conf, []byte(natsConf), "600"), conf)
	if *dryRun {
		for _, d := range dirs {
			a.infof("  %s", d.Path)
		}
	} else {
		out, err := a.capture(ctx, script)
		if err != nil {
			return err
		}
		printIndented(a, out, "  目录都已存在")
	}

	// 4. 网络
	a.step("建 overlay 网络")
	encrypted := strings.EqualFold(env["AEGIS_INTERNAL_ENCRYPTED"], "true")
	netScript := fmt.Sprintf(`if ! docker network inspect %[1]s >/dev/null 2>&1; then docker network create --driver overlay --attachable %[2]s %[1]s >/dev/null && echo "新建 %[1]s"; else echo "%[1]s 已存在"; fi
if ! docker network inspect %[3]s >/dev/null 2>&1; then docker network create --driver overlay --attachable %[3]s >/dev/null && echo "新建 %[3]s（没有 Dokploy 的集群）"; else echo "%[3]s 已存在"; fi
`, internalNetwork, map[bool]string{true: "--opt encrypted", false: ""}[encrypted], publicNetwork)
	if !*dryRun {
		out, err := a.capture(ctx, netScript)
		if err != nil {
			return err
		}
		printIndented(a, out, "")
	}

	// 5. 节点标签
	a.step("节点标签")
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	labeled := false
	var self *NodeInfo
	for i := range nodes.infos {
		n := &nodes.infos[i]
		if n.Spec.Labels["aegis.db"] == "true" {
			labeled = true
		}
		if n.ID == nodes.selfID {
			self = n
		}
	}
	if labeled {
		a.infof("  已有节点打了 aegis.db，不改动标签（用 aegisctl node label 调整）")
	} else if self != nil {
		var add []string
		for _, l := range AllLabels {
			add = append(add, "--label-add "+l+"=true")
		}
		a.infof("  给 %s 打上 %s", self.Description.Hostname, strings.Join(AllLabels, "、"))
		if !*dryRun {
			if _, err := a.capture(ctx, "docker node update "+strings.Join(add, " ")+" "+ShellQuote(self.ID)+" >/dev/null"); err != nil {
				return err
			}
		}
	}

	a.infof("")
	a.infof("%s 接下来：", a.pal.Green("完成。"))
	a.infof("  aegisctl config pull            备份 cluster.env 到本机")
	a.infof("  aegisctl config edit            核对域名、TRUSTED_PROXIES 等")
	a.infof("  aegisctl doctor                 体检")
	a.infof("  aegisctl build && aegisctl deploy")
	a.infof("私有镜像仓库要先在 manager 上 docker login %s", firstNonEmpty(env["AEGIS_REGISTRY"], "ghcr.io"))
	return nil
}

func printKeyList(a *App, title string, keys []string) {
	if len(keys) == 0 {
		return
	}
	a.infof("  %s（%d）：%s", title, len(keys), strings.Join(keys, " "))
}

func printIndented(a *App, out, empty string) {
	out = strings.TrimSpace(out)
	if out == "" {
		if empty != "" {
			a.infof("%s", empty)
		}
		return
	}
	for _, l := range strings.Split(out, "\n") {
		a.infof("  %s", l)
	}
}
