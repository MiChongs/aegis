package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"
)

// cmd_status.go：一屏总览。

// StatusModel 渲染总览需要的全部数据
type StatusModel struct {
	Stack    string
	Nodes    nodeSet
	State    clusterState
	Tasks    []TaskRow
	Probes   []HealthProbe
	ProbeErr string
	At       time.Time
}

const statusTasksScript = `docker stack ps %s --filter desired-state=running --format '{{json .}}' 2>/dev/null || true`

// probeScript 在 aegis-internal 上起一个临时容器，逐个探 api / worker 副本的 /readyz（tasks.<服务> 解析出每个任务的地址）
func probeScript(image string) string {
	return fmt.Sprintf(`docker run --rm -i --network %s --entrypoint sh %s -s <<'AEGISCTL_PROBE'
for spec in "api tasks.aegis-api 8088" "worker tasks.aegis-worker 8089"; do
  set -- $spec
  for ip in $(nslookup "$2" 2>/dev/null | awk '/^Name:/{f=1} f && /^Address/{print $NF}'); do
    code=$(curl -s -o /dev/null -w '%%{http_code}' -m 3 "http://$ip:$3/readyz" || true)
    echo "$1 $ip ${code:-000}"
  done
done
AEGISCTL_PROBE
`, internalNetwork, ShellQuote(image))
}

func (a *App) collectStatus(ctx context.Context, probe bool) (StatusModel, error) {
	m := StatusModel{Stack: a.stack(), At: a.now()}
	var err error
	if m.Nodes, err = a.loadNodes(ctx); err != nil {
		return m, err
	}
	if m.State, err = a.loadState(ctx); err != nil {
		return m, err
	}
	out, err := a.capture(ctx, fmt.Sprintf(statusTasksScript, ShellQuote(a.stack())))
	if err != nil {
		return m, err
	}
	if m.Tasks, err = ParseJSONLines[TaskRow](out); err != nil {
		return m, err
	}
	if probe {
		out, err := a.capture(ctx, probeScript(a.cfg.ProbeImage))
		if err != nil {
			m.ProbeErr = strings.TrimSpace(lastLine(err.Error()))
		}
		m.Probes = ParseHealthProbes(out)
	}
	return m, nil
}

// RenderStatus 渲染总览
func RenderStatus(m StatusModel, p Palette) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", p.Bold("Aegis 集群"), p.Dim(m.At.Format("2006-01-02 15:04:05")))

	// 节点
	perNode := TasksPerNode(m.Tasks)
	b.WriteString(renderNodeTable(m.Nodes, perNode, p))
	b.WriteByte('\n')

	// 服务
	dist := ServicesPerNode(m.Stack, m.Tasks)
	names := make([]string, 0, len(m.State.infos))
	for n := range m.State.infos {
		names = append(names, n)
	}
	sort.Strings(names)
	t := Table{Header: []string{"服务", "副本", "镜像标签", "最近更新", "分布"}}
	healthy, total := 0, 0
	for _, n := range names {
		info := m.State.infos[n]
		rep := ParseReplicas(m.State.rows[n].Replicas)
		_, tag := SplitImage(info.Image)
		var repCell string
		level := "ok"
		if info.IsJob() {
			repCell = fmt.Sprintf("任务 %d/%d 完成", rep.Completed, rep.Total)
			if rep.Total > 0 && rep.Completed < rep.Total && rep.Running == 0 {
				level = "fail"
			} else if rep.Running > 0 {
				level = "warn"
			}
		} else {
			total++
			repCell = fmt.Sprintf("%d/%d", rep.Running, rep.Desired)
			switch {
			case rep.Desired == 0:
				level = "warn"
			case rep.Running >= rep.Desired:
				healthy++
			case rep.Running == 0:
				level = "fail"
			default:
				level = "warn"
			}
		}
		upd := "-"
		if s := info.UpdateState(); s != "" {
			upd = s
			switch s {
			case "completed", "rollback_completed":
				if s == "rollback_completed" {
					upd = p.Yellow("已回滚")
				} else {
					upd = "完成"
				}
			case "updating", "rollback_started":
				upd = p.Yellow("更新中")
			case "paused", "rollback_paused":
				upd = p.Red("暂停（" + s + "）")
			}
			if info.Update != nil && !info.Update.CompletedAt.IsZero() && (s == "completed" || s == "rollback_completed") {
				upd += " " + p.Dim(since(m.At, info.Update.CompletedAt))
			}
		} else if !info.UpdatedAt.IsZero() {
			upd = p.Dim("创建于 " + since(m.At, info.UpdatedAt))
		}
		var parts []string
		for node, c := range dist[n] {
			parts = append(parts, fmt.Sprintf("%s×%d", node, c))
		}
		sort.Strings(parts)
		t.Rows = append(t.Rows, []string{n, p.Level(level, repCell), tag, upd, strings.Join(parts, " ")})
	}
	if len(names) == 0 {
		b.WriteString(p.Yellow("栈 " + m.Stack + " 还没有部署任何服务\n"))
	} else {
		b.WriteString(t.Render(p))
		summary := fmt.Sprintf("服务 %d/%d 副本齐全", healthy, total)
		lvl := "ok"
		if healthy < total {
			lvl = "warn"
		}
		fmt.Fprintf(&b, "\n%s\n", p.Level(lvl, summary))
	}

	// 健康探测
	if m.ProbeErr != "" || m.Probes != nil {
		b.WriteString("\n" + p.Bold("健康探测（/readyz）") + "\n")
		if m.ProbeErr != "" {
			fmt.Fprintf(&b, "  %s %s\n", p.Yellow("探测失败："), m.ProbeErr)
		}
		roles := map[string][2]int{}
		var detail []string
		for _, pr := range m.Probes {
			r := roles[pr.Role]
			r[1]++
			if pr.Code == "200" {
				r[0]++
			} else {
				detail = append(detail, fmt.Sprintf("%s %s 返回 %s", pr.Role, pr.Addr, pr.Code))
			}
			roles[pr.Role] = r
		}
		for _, role := range []string{"api", "worker"} {
			r := roles[role]
			lvl := "ok"
			if r[1] == 0 || r[0] == 0 {
				lvl = "fail"
			} else if r[0] < r[1] {
				lvl = "warn"
			}
			fmt.Fprintf(&b, "  %-8s %s\n", role, p.Level(lvl, fmt.Sprintf("%d/%d 就绪", r[0], r[1])))
		}
		for _, d := range detail {
			fmt.Fprintf(&b, "  %s\n", p.Yellow(d))
		}
	}
	return b.String()
}

func since(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "刚刚"
	case d < time.Minute:
		return fmt.Sprintf("%d 秒前", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	}
	return fmt.Sprintf("%d 天前", int(d.Hours()/24))
}

func (a *App) cmdStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	watch := fs.Bool("watch", false, "持续刷新")
	interval := fs.Duration("interval", 5*time.Second, "刷新间隔")
	noProbe := fs.Bool("no-probe", false, "不探测 /readyz")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	for {
		m, err := a.collectStatus(ctx, !*noProbe)
		if err != nil {
			return err
		}
		text := RenderStatus(m, a.pal)
		if *watch && a.pal.On {
			fmt.Fprint(a.out, "\x1b[H\x1b[2J")
		}
		fmt.Fprint(a.out, text)
		if !*watch {
			return nil
		}
		if err := sleepCtx(ctx, *interval); err != nil {
			return nil
		}
		if !a.pal.On {
			fmt.Fprintln(a.out, strings.Repeat("─", 40))
		}
	}
}
