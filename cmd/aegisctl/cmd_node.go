package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"
)

// cmd_node.go：节点管理（join / ls / label / drain / active / rm）

type nodeSet struct {
	rows   []NodeRow
	infos  []NodeInfo
	selfID string
}

func (n nodeSet) byName(name string) *NodeInfo {
	for i := range n.infos {
		if n.infos[i].Description.Hostname == name || n.infos[i].ID == name || strings.HasPrefix(n.infos[i].ID, name) && len(name) >= 6 {
			return &n.infos[i]
		}
	}
	return nil
}

func (n nodeSet) selfHostname() string {
	for _, i := range n.infos {
		if i.ID == n.selfID {
			return i.Description.Hostname
		}
	}
	return ""
}

const nodesScript = `echo '@@SELF'; docker info --format '{{.Swarm.NodeID}}'
echo '@@NODES'; docker node ls --format '{{json .}}'
echo '@@INSPECT'; docker node inspect --format '{{json .}}' $(docker node ls -q)
`

func (a *App) loadNodes(ctx context.Context) (nodeSet, error) {
	out, err := a.capture(ctx, nodesScript)
	if err != nil {
		return nodeSet{}, err
	}
	return parseNodes(out)
}

func parseNodes(out string) (nodeSet, error) {
	sec := Sections(out)
	var ns nodeSet
	var err error
	ns.selfID = strings.TrimSpace(sec["SELF"])
	if ns.rows, err = ParseJSONLines[NodeRow](sec["NODES"]); err != nil {
		return ns, err
	}
	if ns.infos, err = ParseJSONLines[NodeInfo](sec["INSPECT"]); err != nil {
		return ns, err
	}
	sort.Slice(ns.infos, func(i, j int) bool { return ns.infos[i].Description.Hostname < ns.infos[j].Description.Hostname })
	return ns, nil
}

func (a *App) cmdNode(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("aegisctl node join|ls|label|drain|active|rm")
	}
	switch args[0] {
	case "join":
		return a.nodeJoin(ctx, args[1:])
	case "ls", "list":
		return a.nodeList(ctx)
	case "label":
		return a.nodeLabel(ctx, args[1:])
	case "drain", "active", "pause":
		if len(args) != 2 {
			return usagef("aegisctl node %s <节点>", args[0])
		}
		return a.nodeAvailability(ctx, args[1], args[0])
	case "rm", "remove":
		return a.nodeRemove(ctx, args[1:])
	}
	return usagef("未知的 node 子命令 %q", args[0])
}

// ParseLabels 解析 k=v,k2=v2；k- 表示删除
func ParseLabels(items []string) (add map[string]string, remove []string, err error) {
	add = map[string]string{}
	for _, raw := range items {
		for _, it := range splitList(raw) {
			if strings.HasSuffix(it, "-") && !strings.Contains(it, "=") {
				k := strings.TrimSuffix(it, "-")
				if !validLabelKey(k) {
					return nil, nil, usagef("标签名不合法：%q", k)
				}
				remove = append(remove, k)
				continue
			}
			k, v, ok := strings.Cut(it, "=")
			if !ok {
				v = "true"
			}
			if !validLabelKey(k) {
				return nil, nil, usagef("标签名不合法：%q", k)
			}
			if strings.ContainsAny(v, " \t\n'\"") {
				return nil, nil, usagef("标签值不能含空白与引号：%q", v)
			}
			add[k] = v
		}
	}
	return add, remove, nil
}

// LabelArgs docker node update 的参数（按键排序，便于测试与复现）
func LabelArgs(add map[string]string, remove []string) []string {
	keys := make([]string, 0, len(add))
	for k := range add {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var args []string
	for _, k := range keys {
		args = append(args, "--label-add", k+"="+add[k])
	}
	for _, k := range remove {
		args = append(args, "--label-rm", k)
	}
	return args
}

// JoinScript 新节点上执行的加入脚本；令牌从标准输入读，不出现在进程参数里
func JoinScript(managerAddr, advertise string) string {
	cmd := []string{"docker", "swarm", "join", "--token", `"$tok"`}
	if advertise != "" {
		cmd = append(cmd, "--advertise-addr", ShellQuote(advertise))
	}
	cmd = append(cmd, ShellQuote(managerAddr))
	return "read -r tok\n" + strings.Join(cmd, " ") + "\n"
}

// JoinAddr manager 的加入地址（docker info 的 NodeAddr 加 2377 端口；IPv6 加方括号）
func JoinAddr(nodeAddr string) string {
	nodeAddr = strings.TrimSpace(nodeAddr)
	if strings.Contains(nodeAddr, ":") && !strings.HasPrefix(nodeAddr, "[") {
		return "[" + nodeAddr + "]:2377"
	}
	return nodeAddr + ":2377"
}

const dockerInstallHint = `目标机器上没有可用的 Docker。请先在那台机器上安装 Docker Engine（aegisctl 不会替你执行）：
    curl -fsSL https://get.docker.com -o get-docker.sh
    sudo sh get-docker.sh            # 大陆机器可加 --mirror Aliyun
    sudo systemctl enable --now docker
节点之间需放行：2377/tcp（manager）、7946/tcp+udp、4789/udp；aegis-internal 加密时还要放行 ESP（IP 协议 50）`

func (a *App) nodeJoin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("node join", flag.ContinueOnError)
	role := fs.String("role", "worker", "worker 或 manager")
	labels := fs.String("labels", "aegis.app=true", "要打的标签，逗号分隔")
	advertise := fs.String("advertise-addr", "", "新节点的通告地址（多网卡时指定内网地址）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("aegisctl node join <ssh 主机> [--role worker|manager] [--labels k=v,...]")
	}
	host := pos[0]
	if *role != "worker" && *role != "manager" {
		return usagef("--role 只能是 worker 或 manager")
	}
	add, remove, err := ParseLabels([]string{*labels})
	if err != nil {
		return err
	}

	a.step("从 %s 取加入令牌", a.manager())
	out, err := a.capture(ctx, fmt.Sprintf(`echo "$(docker swarm join-token -q %s)|$(docker info --format '{{.Swarm.NodeAddr}}|{{.Swarm.Cluster.ID}}')"`, *role))
	if err != nil {
		return err
	}
	f := strings.Split(strings.TrimSpace(out), "|")
	if len(f) != 3 || f[0] == "" {
		return fmt.Errorf("取不到加入令牌（%s 是 manager 吗？）", a.manager())
	}
	token, mgrAddr, clusterID := f[0], JoinAddr(f[1]), f[2]

	a.step("检查 %s", host)
	probe, err := Capture(ctx, a.ex, host, `if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then echo nodocker; exit 0; fi
docker info --format '{{.Swarm.LocalNodeState}}|{{.Swarm.Cluster.ID}}|{{.Name}}|{{.ServerVersion}}'`)
	if err != nil {
		return err
	}
	probe = strings.TrimSpace(probe)
	if probe == "nodocker" {
		return fmt.Errorf("%s", dockerInstallHint)
	}
	p := strings.Split(probe, "|")
	if len(p) < 4 {
		return fmt.Errorf("看不懂 %s 上 docker info 的输出：%q", host, probe)
	}
	hostname := p[2]
	a.infof("  %s：Docker %s，主机名 %s", host, p[3], hostname)
	switch {
	case p[0] == "active" && p[1] == clusterID:
		a.infof("  已经在这个 Swarm 里，跳过加入")
	case p[0] == "active":
		return fmt.Errorf("%s 已在另一个 Swarm 里：先在那台机器上 docker swarm leave", host)
	default:
		a.step("加入 Swarm（%s，manager 地址 %s）", *role, mgrAddr)
		var errb strings.Builder
		if err := a.ex.Run(ctx, Remote{Host: host, Script: JoinScript(mgrAddr, *advertise), Stdin: strings.NewReader(token + "\n"), Stdout: a.out, Stderr: &errb}); err != nil {
			return &RemoteError{Host: host, Err: err, Stderr: errb.String() + "\n（确认节点间放行 2377/tcp、7946/tcp+udp、4789/udp）"}
		}
	}
	idOut, err := Capture(ctx, a.ex, host, `docker info --format '{{.Swarm.NodeID}}'`)
	if err != nil {
		return err
	}
	nodeID := strings.TrimSpace(idOut)

	a.cfg.Nodes[hostname] = host
	if err := a.cfg.Save(); err != nil {
		a.warnf("没能把 %s → %s 记进本机配置：%v", hostname, host, err)
	}
	if len(add) > 0 || len(remove) > 0 {
		if err := a.applyLabels(ctx, nodeID, hostname, add, remove); err != nil {
			return err
		}
	}
	a.infof("%s %s 已加入（节点 ID %s）", a.pal.Green("完成。"), hostname, shortID(nodeID))
	return nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// applyLabels 打标签；新增有状态标签时提醒唯一性，并在该节点上准备数据目录
func (a *App) applyLabels(ctx context.Context, nodeID, hostname string, add map[string]string, remove []string) error {
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	for _, l := range StatefulLabels {
		if add[l] != "true" {
			continue
		}
		for _, n := range nodes.infos {
			if n.ID != nodeID && n.Spec.Labels[l] == "true" {
				a.warnf("%s 上已经有 %s：有状态服务的数据是节点本地目录，两台都打会让任务在两份数据之间漂移。迁移步骤见手册「有状态服务换节点」", n.Description.Hostname, l)
			}
		}
	}
	args := LabelArgs(add, remove)
	if _, err := a.capture(ctx, "docker node update "+ShellJoin(args...)+" "+ShellQuote(nodeID)+" >/dev/null"); err != nil {
		return err
	}
	a.infof("  %s：%s", hostname, strings.Join(args, " "))

	var dirs []DirSpec
	var env *EnvFile
	for _, l := range StatefulLabels {
		if add[l] == "true" {
			if env == nil {
				if env, _, err = a.remoteEnv(ctx); err != nil {
					return err
				}
			}
			dirs = append(dirs, DirsForLabel(l, env.Map())...)
			if l == "aegis.db" {
				a.warnf("新的 aegis.db 节点上没有 NATS 配置文件时 nats 起不来：把 manager 上的 %s 拷过去", firstNonEmpty(env.Value("AEGIS_NATS_CONF"), "/srv/aegis/nats/nats-server.conf"))
			}
		}
	}
	if len(dirs) > 0 {
		host := a.cfg.HostForNode(hostname, nodes.selfHostname())
		a.step("在 %s 上准备数据目录", host)
		out, err := Capture(ctx, a.ex, host, PrepareDirsScript(dirs))
		if err != nil {
			return err
		}
		printIndented(a, out, "  目录都已存在")
	}
	return nil
}

func (a *App) nodeList(ctx context.Context) error {
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	tasksOut, err := a.capture(ctx, fmt.Sprintf("docker stack ps %s --filter desired-state=running --format '{{json .}}' 2>/dev/null || true", a.stack()))
	if err != nil {
		return err
	}
	tasks, err := ParseJSONLines[TaskRow](tasksOut)
	if err != nil {
		return err
	}
	fmt.Fprint(a.out, renderNodeTable(nodes, TasksPerNode(tasks), a.pal))
	return nil
}

func renderNodeTable(nodes nodeSet, perNode map[string]int, p Palette) string {
	t := Table{Header: []string{"节点", "ID", "角色", "状态", "可调度", "aegis 任务", "标签"}}
	for _, n := range nodes.infos {
		role := n.Spec.Role
		if n.ManagerStatus != nil && n.ManagerStatus.Leader {
			role = "manager（leader）"
		}
		state := n.Status.State
		stateCell := p.Level(map[bool]string{true: "ok", false: "fail"}[state == "ready"], state)
		avail := n.Spec.Availability
		availCell := p.Level(map[bool]string{true: "ok", false: "warn"}[avail == "active"], avail)
		name := n.Description.Hostname
		if n.ID == nodes.selfID {
			name += " *"
		}
		labels := strings.Join(AegisLabels(n.Spec.Labels), " ")
		if labels == "" {
			labels = p.Dim("（无）")
		}
		t.Rows = append(t.Rows, []string{name, shortID(n.ID), role, stateCell, availCell, fmt.Sprint(perNode[n.Description.Hostname]), labels})
	}
	return t.Render(p)
}

func (a *App) nodeLabel(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return usagef("aegisctl node label <节点> k=v [k2=v2 ...] [k3-]")
	}
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	n := nodes.byName(args[0])
	if n == nil {
		return fmt.Errorf("没有节点 %q", args[0])
	}
	add, remove, err := ParseLabels(args[1:])
	if err != nil {
		return err
	}
	return a.applyLabels(ctx, n.ID, n.Description.Hostname, add, remove)
}

func (a *App) nodeAvailability(ctx context.Context, name, avail string) error {
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	n := nodes.byName(name)
	if n == nil {
		return fmt.Errorf("没有节点 %q", name)
	}
	if _, err := a.capture(ctx, fmt.Sprintf("docker node update --availability %s %s >/dev/null", avail, ShellQuote(n.ID))); err != nil {
		return err
	}
	a.infof("%s 已设为 %s", n.Description.Hostname, avail)
	if avail == "drain" {
		for _, l := range StatefulLabels {
			if n.Spec.Labels[l] == "true" {
				a.warnf("%s 有 %s：固定在它上面的有状态服务会停下且无处可去，直到 aegisctl node active", n.Description.Hostname, l)
			}
		}
	}
	return nil
}

func (a *App) nodeRemove(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("node rm", flag.ContinueOnError)
	force := fs.Bool("force", false, "节点已经失联时直接从集群删除")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("aegisctl node rm <节点> [--force]")
	}
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	n := nodes.byName(pos[0])
	if n == nil {
		return fmt.Errorf("没有节点 %q", pos[0])
	}
	if n.ID == nodes.selfID {
		return fmt.Errorf("不能删除当前的 manager（%s）", n.Description.Hostname)
	}
	for _, l := range StatefulLabels {
		if n.Spec.Labels[l] == "true" && !*force {
			return fmt.Errorf("%s 有 %s：先把有状态服务迁走（手册「有状态服务换节点」），或加 --force", n.Description.Hostname, l)
		}
	}
	host := a.cfg.HostForNode(n.Description.Hostname, nodes.selfHostname())
	if !*force {
		a.step("排空 %s", n.Description.Hostname)
		if _, err := a.capture(ctx, "docker node update --availability drain "+ShellQuote(n.ID)+" >/dev/null"); err != nil {
			return err
		}
		deadline := a.now().Add(5 * time.Minute)
		for {
			out, err := a.capture(ctx, fmt.Sprintf("docker node ps %s --filter desired-state=running -q | wc -l", ShellQuote(n.ID)))
			if err != nil {
				return err
			}
			if strings.TrimSpace(out) == "0" {
				break
			}
			if a.now().After(deadline) {
				return fmt.Errorf("5 分钟内没排空，%s 上还有 %s 个任务", n.Description.Hostname, strings.TrimSpace(out))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
		if n.Spec.Role == "manager" {
			if _, err := a.capture(ctx, "docker node demote "+ShellQuote(n.ID)+" >/dev/null"); err != nil {
				return err
			}
		}
		a.step("在 %s 上退出 Swarm", host)
		if _, err := Capture(ctx, a.ex, host, "docker swarm leave >/dev/null"); err != nil {
			a.warnf("%v（继续从集群里删除）", err)
		}
		time.Sleep(3 * time.Second)
	}
	cmd := "docker node rm "
	if *force {
		cmd += "--force "
	}
	if _, err := a.capture(ctx, cmd+ShellQuote(n.ID)+" >/dev/null"); err != nil {
		return err
	}
	delete(a.cfg.Nodes, n.Description.Hostname)
	_ = a.cfg.Save()
	a.infof("%s 已从集群删除", n.Description.Hostname)
	return nil
}
