package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// cmd_deploy.go：发布（deploy / migrate / rollback / history / traefik）。
//
// deploy 分三段，每段都是一次 `docker stack deploy --prune=false`，只带这一段的服务：
//  1. 除 aegis-api / aegis-worker / aegis-migrate 之外选中的服务（基础设施、风控、音乐、控制台、一起听）；
//  2. 迁移任务 aegis-migrate（新镜像），等它跑完；失败就停在这里，api / worker 保持原样；
//  3. aegis-api、aegis-worker 按槽位逐个滚动（stop-first，槽位卷不能被新旧任务同时打开；失败自动回滚）。
// 最后等全部选中的服务收敛，成功后再写 Traefik 路由，并记入部署历史。

const migrateMaxAttempts = 6 // 与栈文件里 aegis-migrate 的 restart_policy.max_attempts 一致

var appServices = map[string]bool{"aegis-api": true, "aegis-worker": true, "aegis-migrate": true}

// clusterState 栈里各服务的当前状态（按去掉栈名前缀的服务名）
type clusterState struct {
	rows  map[string]ServiceRow
	infos map[string]ServiceInfo
	now   time.Time // manager 的时钟
}

func (a *App) loadState(ctx context.Context) (clusterState, error) {
	filter := "label=com.docker.stack.namespace=" + a.stack()
	script := fmt.Sprintf(`echo '@@NOW'; date -u +%%s
echo '@@LS'; docker service ls --filter %[1]s --format '{{json .}}'
echo '@@INSPECT'; ids=$(docker service ls -q --filter %[1]s); if [ -n "$ids" ]; then docker service inspect --format %[2]s $ids; fi
`, ShellQuote(filter), ShellQuote(serviceInspectFormat))
	out, err := a.capture(ctx, script)
	if err != nil {
		return clusterState{}, err
	}
	return parseState(a.stack(), out)
}

func parseState(stack, out string) (clusterState, error) {
	sec := Sections(out)
	st := clusterState{rows: map[string]ServiceRow{}, infos: map[string]ServiceInfo{}}
	if sec, err := strconv.ParseInt(strings.TrimSpace(sec["NOW"]), 10, 64); err == nil {
		st.now = time.Unix(sec, 0)
	}
	rows, err := ParseJSONLines[ServiceRow](sec["LS"])
	if err != nil {
		return st, err
	}
	for _, r := range rows {
		st.rows[ShortService(stack, r.Name)] = r
	}
	infos, err := ParseJSONLines[ServiceInfo](sec["INSPECT"])
	if err != nil {
		return st, err
	}
	for _, i := range infos {
		st.infos[ShortService(stack, i.Name)] = i
	}
	return st, nil
}

// CurrentTags 正在运行的各镜像标签（按标签变量）
func CurrentTags(st clusterState) map[string]string {
	tags := map[string]string{}
	for _, im := range Images {
		for _, s := range im.Services {
			if s == "aegis-migrate" {
				continue
			}
			if info, ok := st.infos[s]; ok && info.Image != "" {
				_, tag := SplitImage(info.Image)
				tags[im.TagVar] = tag
				break
			}
		}
	}
	if info, ok := st.infos["aegis-migrate"]; ok {
		_, tags[migrateTagVar] = SplitImage(info.Image)
	}
	return tags
}

// TagPlan 每个镜像用哪个标签
type TagPlan struct {
	Tags     map[string]string // 标签变量 → 标签
	Fallback []string          // 没有请求的标签、沿用现网的镜像
}

// PlanTags 选中服务用到的镜像：请求的标签存在就用，否则沿用现网的，再否则用 cluster.env 里钉住的；都没有就报错。
// 没选中的镜像沿用现网标签。pinned 是 cluster.env 里的 *_TAG（第一次部署时栈还不存在，只能靠它给没改动的镜像定版本）。
func PlanTags(selected map[string]bool, want string, current, pinned map[string]string, exists func(image string) bool) (TagPlan, error) {
	plan := TagPlan{Tags: map[string]string{}}
	for _, im := range Images {
		needed := false
		for _, s := range im.Services {
			if selected[s] {
				needed = true
			}
		}
		cur := current[im.TagVar]
		pin := pinned[im.TagVar]
		switch {
		case !needed && cur != "":
			plan.Tags[im.TagVar] = cur
		case !needed:
			plan.Tags[im.TagVar] = want
		case exists(im.Name):
			plan.Tags[im.TagVar] = want
		case cur != "":
			plan.Tags[im.TagVar] = cur
			plan.Fallback = append(plan.Fallback, fmt.Sprintf("%s 没有标签 %s，沿用现网的 %s", im.Name, want, cur))
		case pin != "" && pin != want:
			plan.Tags[im.TagVar] = pin
			plan.Fallback = append(plan.Fallback, fmt.Sprintf("%s 没有标签 %s，用 cluster.env 里钉住的 %s", im.Name, want, pin))
		default:
			return plan, fmt.Errorf("镜像 %s 没有标签 %s，现网也没有在跑的版本：先 aegisctl build %s --tag %s（或等 CI 推完）", im.Name, want, im.Name, want)
		}
	}
	plan.Tags[migrateTagVar] = plan.Tags["AEGIS_TAG"]
	return plan, nil
}

// renderStack 在本机渲染选中服务的栈文件，用来在动手之前查出缺失的必填变量（部署时由 docker 自己插值）
func renderStack(tmpl string, env map[string]string, vars map[string]string, keep map[string]bool) (string, error) {
	text := tmpl
	if keep != nil {
		var err error
		if text, err = SelectServices(tmpl, keep); err != nil {
			return "", err
		}
	}
	lookup := func(k string) (string, bool) {
		if v, ok := vars[k]; ok {
			return v, true
		}
		v, ok := env[k]
		return v, ok
	}
	out, errs := Interpolate(text, lookup, true)
	if len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		seen := map[string]bool{}
		for _, e := range errs {
			var ie InterpolateError
			m := e.Error()
			if errors.As(e, &ie) {
				m = ie.Msg
			}
			if !seen[m] {
				seen[m] = true
				msgs = append(msgs, m)
			}
		}
		return "", fmt.Errorf("渲染栈文件失败：\n  %s", strings.Join(msgs, "\n  "))
	}
	return out, nil
}

// DeployScript manager 上执行的部署脚本：把 cluster.env 逐行 export（值原样，不经 shell 求值），
// 再加上镜像标签等变量，然后 docker stack deploy 从标准输入读栈文件。插值由 docker 在解析 YAML 之后做，
// 所以值里有 *、#、: 之类的字符也不会破坏 YAML。脚本作为 bash -c 的参数，里面没有密钥。
func DeployScript(stack, envFile string, vars map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `while IFS= read -r line || [ -n "$line" ]; do
  line="${line#"${line%%%%[![:space:]]*}"}"
  case "$line" in ''|'#'*) continue ;; esac
  key="${line%%%%=*}"
  case "$key" in *[!A-Za-z0-9_]*|[0-9]*|''|DOCKER_*|PATH|HOME|IFS) continue ;; esac
  export "$line"
done < %s
`, ShellQuote(envFile))
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%s\n", k, ShellQuote(vars[k]))
	}
	fmt.Fprintf(&b, "export AEGIS_ENV_FILE=%s\n", ShellQuote(envFile))
	b.WriteString("docker stack deploy --with-registry-auth --prune=false --resolve-image always -c - " + ShellQuote(stack) + "\n")
	return b.String()
}

// stackDeploy 部署栈文件里选中的服务（未渲染的原文，由 docker 插值）
func (a *App) stackDeploy(ctx context.Context, tmpl string, keep map[string]bool, vars map[string]string) error {
	text, err := SelectServices(tmpl, keep)
	if err != nil {
		return err
	}
	var out, errb strings.Builder
	err = a.ex.Run(ctx, Remote{Host: a.manager(), Script: DeployScript(a.stack(), remoteEnvFile, vars), Stdin: strings.NewReader(text), Stdout: &out, Stderr: &errb})
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l != "" {
			a.infof("  %s", a.pal.Dim(l))
		}
	}
	if err != nil {
		return &RemoteError{Host: a.manager(), Err: err, Stderr: errb.String()}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func subset(m map[string]bool, pred func(string) bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		if v && pred(k) {
			out[k] = true
		}
	}
	return out
}

// deployInputs 读本机模板与线上配置
type deployInputs struct {
	stackTmpl   string
	traefikTmpl string
	env         *EnvFile
	known       []string
}

func (a *App) loadDeployInputs(ctx context.Context) (deployInputs, error) {
	var in deployInputs
	repo, err := a.repo()
	if err != nil {
		return in, err
	}
	if in.stackTmpl, err = readRepoFile(repo, stackRelPath); err != nil {
		return in, err
	}
	if in.traefikTmpl, err = readRepoFile(repo, traefikRelPath); err != nil {
		return in, err
	}
	if in.known, err = StackServices(in.stackTmpl); err != nil {
		return in, err
	}
	if in.env, _, err = a.remoteEnv(ctx); err != nil {
		return in, err
	}
	return in, nil
}

func (a *App) checkImages(ctx context.Context, registry, tag string, images []ImageSpec) (map[string]bool, error) {
	var b strings.Builder
	for _, im := range images {
		ref := registry + "/" + im.Name + ":" + tag
		fmt.Fprintf(&b, "if docker manifest inspect %[1]s >/dev/null 2>&1 || docker image inspect %[1]s >/dev/null 2>&1; then echo 'ok %[2]s'; else echo 'missing %[2]s'; fi\n", ShellQuote(ref), im.Name)
	}
	out, err := a.capture(ctx, b.String())
	if err != nil {
		return nil, err
	}
	res := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			res[f[1]] = f[0] == "ok"
		}
	}
	return res, nil
}

func (a *App) cmdDeploy(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	tagFlag := fs.String("tag", "", "镜像标签（默认本机仓库 HEAD 的 sha-<7 位短哈希>）")
	only := fs.String("only", "", "只部署这些服务或组（逗号分隔；组：app、web、infra、risk、music）")
	timeout := fs.Duration("timeout", 15*time.Minute, "等待收敛的总时长")
	dryRun := fs.Bool("dry-run", false, "只显示计划")
	skipTraefik := fs.Bool("skip-traefik", false, "不写 Traefik 动态配置")
	skipCheck := fs.Bool("skip-image-check", false, "不检查镜像标签是否存在")
	forceUnlock := fs.Bool("force-unlock", false, "上次部署异常退出留下了锁时，先清掉")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	in, err := a.loadDeployInputs(ctx)
	if err != nil {
		return err
	}
	env := in.env.Map()
	selected := map[string]bool{}
	for _, s := range in.known {
		selected[s] = true
	}
	if *only != "" {
		if selected, err = ExpandServices(*only, in.known); err != nil {
			return err
		}
	}
	tag := *tagFlag
	if tag == "" {
		repo, _ := a.repo()
		sha, err := localGitHead(repo)
		if err != nil {
			return err
		}
		tag = TagForCommit(sha)
	}
	if !tagPattern.MatchString(tag) {
		return usagef("标签不合法：%q", tag)
	}
	registry := strings.TrimSuffix(env["AEGIS_REGISTRY"], "/")

	st, err := a.loadState(ctx)
	if err != nil {
		return err
	}
	current := CurrentTags(st)
	var needImages []ImageSpec
	for _, im := range Images {
		for _, s := range im.Services {
			if selected[s] {
				needImages = append(needImages, im)
				break
			}
		}
	}
	exists := map[string]bool{}
	if *skipCheck {
		for _, im := range needImages {
			exists[im.Name] = true
		}
	} else if len(needImages) > 0 {
		a.step("检查镜像 %s/*:%s", registry, tag)
		if exists, err = a.checkImages(ctx, registry, tag, needImages); err != nil {
			return err
		}
	}
	plan, err := PlanTags(selected, tag, current, env, func(n string) bool { return exists[n] })
	if err != nil {
		return err
	}
	for _, f := range plan.Fallback {
		a.warnf("%s", f)
	}
	// 先整体渲染一遍：缺变量在动手之前就报出来
	if _, err := renderStack(in.stackTmpl, env, plan.Tags, selected); err != nil {
		return err
	}
	traefik, err := RenderTraefik(in.traefikTmpl, env)
	if err != nil {
		return err
	}

	phaseA := subset(selected, func(s string) bool { return !appServices[s] })
	runMigrate := selected["aegis-api"] || selected["aegis-worker"] || selected["aegis-migrate"]
	phaseC := subset(selected, func(s string) bool { return s == "aegis-api" || s == "aegis-worker" })

	a.infof("%s 标签：", a.pal.Bold("发布计划"))
	for _, im := range Images {
		mark := ""
		if current[im.TagVar] != "" && current[im.TagVar] != plan.Tags[im.TagVar] {
			mark = a.pal.Yellow("（现网 " + current[im.TagVar] + "）")
		}
		a.infof("  %-22s %s %s", im.Name, plan.Tags[im.TagVar], mark)
	}
	if len(phaseA) > 0 {
		a.infof("  第一段：%s", strings.Join(sortedKeys(phaseA), " "))
	}
	if runMigrate {
		a.infof("  第二段：aegis-migrate（%s）", plan.Tags[migrateTagVar])
	}
	if len(phaseC) > 0 {
		a.infof("  第三段：%s", strings.Join(sortedKeys(phaseC), " "))
	}
	if *dryRun {
		a.infof("（--dry-run：未做任何改动）")
		return nil
	}

	if err := a.lock(ctx, *forceUnlock); err != nil {
		return err
	}
	defer a.unlock(context.WithoutCancel(ctx))
	started := a.now()
	since := st.now.Add(-5 * time.Second)
	if st.now.IsZero() {
		since = started.Add(-time.Minute)
	}
	entry := HistoryEntry{Action: "deploy", Tag: tag, Tags: plan.Tags, Services: sortedKeys(selected)}
	finish := func(result, msg string) {
		entry.Result, entry.Message = result, msg
		entry.Duration = a.now().Sub(started).Round(time.Second).String()
		a.appendHistory(context.WithoutCancel(ctx), entry)
	}

	if len(phaseA) > 0 {
		a.step("第一段：部署 %d 个服务", len(phaseA))
		if err := a.stackDeploy(ctx, in.stackTmpl, phaseA, plan.Tags); err != nil {
			finish("failed", "第一段 stack deploy 失败")
			return err
		}
		if phaseA["risk-redis-init"] {
			_, _ = a.capture(ctx, "docker service update --force --detach "+ShellQuote(a.svc("risk-redis-init"))+" >/dev/null 2>&1 || true")
		}
	}
	if runMigrate {
		if err := a.waitPostgres(ctx, 5*time.Minute); err != nil {
			finish("failed", err.Error())
			return err
		}
		a.step("第二段：数据库迁移（%s）", plan.Tags[migrateTagVar])
		migrateOnly := map[string]bool{"aegis-migrate": true}
		if err := a.runMigration(ctx, func() error { return a.stackDeploy(ctx, in.stackTmpl, migrateOnly, plan.Tags) }, *timeout); err != nil {
			finish("failed", "迁移失败，api / worker 未更新")
			return err
		}
	}
	if len(phaseC) > 0 {
		a.step("第三段：滚动更新 %s", strings.Join(sortedKeys(phaseC), " "))
		if err := a.stackDeploy(ctx, in.stackTmpl, phaseC, plan.Tags); err != nil {
			finish("failed", "第三段 stack deploy 失败")
			return err
		}
	}
	a.step("等待收敛")
	var waitFor []string
	for _, s := range sortedKeys(selected) {
		if s != "aegis-migrate" {
			waitFor = append(waitFor, s)
		}
	}
	failed, err := a.waitConverge(ctx, waitFor, since, *timeout-a.now().Sub(started))
	if err != nil {
		finish("failed", err.Error())
		return err
	}
	if len(failed) > 0 {
		names := a.handleFailures(ctx, failed)
		finish("rolled_back", "未收敛："+strings.Join(names, " "))
		return fmt.Errorf("%d 个服务没有更新成功：%s", len(failed), strings.Join(names, " "))
	}
	if !*skipTraefik {
		// 栈里还没有 aegis-api 时不写路由（例如切换前只部署 aegis-rustfs）：否则高优先级的新路由会把流量引到不存在的服务
		if st2, err := a.loadState(ctx); err == nil && st2.infos["aegis-api"].Name == "" {
			a.warnf("栈里还没有 aegis-api，这次不写 Traefik 路由")
		} else if err := a.applyTraefik(ctx, traefik, env); err != nil {
			finish("failed", "Traefik 配置写入失败")
			return err
		}
	}
	finish("success", "")
	a.infof("%s 用时 %s", a.pal.Green("发布完成。"), a.now().Sub(started).Round(time.Second))
	return nil
}

// waitPostgres 迁移前等 Postgres 起来（服务不存在时直接返回，迁移任务自己会重试）
func (a *App) waitPostgres(ctx context.Context, timeout time.Duration) error {
	deadline := a.now().Add(timeout)
	for {
		st, err := a.loadState(ctx)
		if err != nil {
			return err
		}
		row, ok := st.rows["postgres"]
		if !ok {
			return nil
		}
		r := ParseReplicas(row.Replicas)
		if r.Valid && r.Running >= r.Desired && r.Desired > 0 {
			return nil
		}
		if a.now().After(deadline) {
			return fmt.Errorf("Postgres 在 %s 内没有就绪（%s）", timeout, row.Replicas)
		}
		if err := sleepCtx(ctx, 3*time.Second); err != nil {
			return err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func jobIteration(st clusterState, svc string) int64 {
	if info, ok := st.infos[svc]; ok && info.Job != nil {
		return info.Job.JobIteration.Index
	}
	return -1
}

// runMigration 更新迁移任务并等它跑完。spec 没变时 Swarm 不会重跑，改用 --force
func (a *App) runMigration(ctx context.Context, update func() error, timeout time.Duration) error {
	st, err := a.loadState(ctx)
	if err != nil {
		return err
	}
	before := jobIteration(st, "aegis-migrate")
	if err := update(); err != nil {
		return err
	}
	if st, err = a.loadState(ctx); err != nil {
		return err
	}
	iter := jobIteration(st, "aegis-migrate")
	if iter == before {
		if _, err := a.capture(ctx, "docker service update --force --detach "+ShellQuote(a.svc("aegis-migrate"))+" >/dev/null"); err != nil {
			return err
		}
		if st, err = a.loadState(ctx); err != nil {
			return err
		}
		iter = jobIteration(st, "aegis-migrate")
	}
	if iter < 0 {
		return fmt.Errorf("栈里没有 aegis-migrate 任务")
	}
	deadline := a.now().Add(timeout)
	last := ""
	script := fmt.Sprintf("ids=$(docker service ps -q --no-trunc %s); if [ -n \"$ids\" ]; then docker inspect --format %s $ids; fi", ShellQuote(a.svc("aegis-migrate")), ShellQuote(taskInspectFormat))
	for {
		out, err := a.capture(ctx, script)
		if err != nil {
			return err
		}
		tasks, err := ParseJSONLines[TaskInfo](out)
		if err != nil {
			return err
		}
		o := EvaluateJob(tasks, iter, migrateMaxAttempts)
		line := fmt.Sprintf("运行中 %d，失败 %d/%d", o.Running, o.Failed, migrateMaxAttempts)
		if line != last {
			a.infof("  aegis-migrate：%s", line)
			last = line
		}
		if o.Done && o.Succeeded {
			a.infof("  %s", a.pal.Green("迁移完成"))
			return nil
		}
		if o.Done {
			a.printServiceLogs(ctx, "aegis-migrate", 60)
			return fmt.Errorf("迁移失败（%s），已中止：aegis-api / aegis-worker 保持原版本", firstNonEmpty(o.LastError, "见上方日志"))
		}
		if a.now().After(deadline) {
			a.printServiceLogs(ctx, "aegis-migrate", 60)
			return fmt.Errorf("迁移在 %s 内没有完成，已中止", timeout)
		}
		if err := sleepCtx(ctx, 3*time.Second); err != nil {
			return err
		}
	}
}

// waitConverge 等服务收敛，返回失败的服务
func (a *App) waitConverge(ctx context.Context, services []string, since time.Time, timeout time.Duration) ([]Convergence, error) {
	if timeout < time.Minute {
		timeout = time.Minute
	}
	deadline := a.now().Add(timeout)
	last := map[string]string{}
	for {
		st, err := a.loadState(ctx)
		if err != nil {
			return nil, err
		}
		var pending, failed []Convergence
		for _, s := range services {
			info, ok := st.infos[s]
			if !ok {
				pending = append(pending, Convergence{Service: s, Detail: "服务还没出现"})
				continue
			}
			if info.IsJob() {
				continue
			}
			c := EvaluateConvergence(info, ParseReplicas(st.rows[s].Replicas), since)
			c.Service = s
			line := fmt.Sprintf("%d/%d %s", c.Running, c.Desired, firstNonEmpty(c.State, "-"))
			if last[s] != line {
				last[s] = line
				level := "warn"
				if c.Done && !c.Failed {
					level = "ok"
				} else if c.Failed {
					level = "fail"
				}
				a.infof("  %-24s %s", s, a.pal.Level(level, line))
			}
			switch {
			case c.Failed:
				failed = append(failed, c)
			case !c.Done:
				pending = append(pending, c)
			}
		}
		if len(pending) == 0 {
			return failed, nil
		}
		if a.now().After(deadline) {
			for _, p := range pending {
				p.Failed = true
				p.Detail = firstNonEmpty(p.Detail, "等待超时")
				failed = append(failed, p)
			}
			return failed, nil
		}
		if err := sleepCtx(ctx, 4*time.Second); err != nil {
			return nil, err
		}
	}
}

// handleFailures 没收敛的服务：Swarm 没回滚的手动回滚，并打印最近日志
func (a *App) handleFailures(ctx context.Context, failed []Convergence) []string {
	var names []string
	for _, f := range failed {
		names = append(names, f.Service)
		a.infof("%s %s：%s", a.pal.Red("✗"), f.Service, firstNonEmpty(f.Detail, f.State))
		if f.State != "rollback_completed" && f.State != "rollback_started" {
			a.infof("  执行 docker service rollback %s", a.svc(f.Service))
			if _, err := a.capture(ctx, "docker service rollback --detach "+ShellQuote(a.svc(f.Service))+" >/dev/null"); err != nil {
				a.warnf("回滚 %s 失败：%v", f.Service, err)
			}
		}
		out, _ := a.capture(ctx, fmt.Sprintf("docker service ps --no-trunc --format '{{.Name}}  {{.Node}}  {{.CurrentState}}  {{.Error}}' %s | head -n 6", ShellQuote(a.svc(f.Service))))
		printIndented(a, out, "")
		a.printServiceLogs(ctx, f.Service, 40)
	}
	return names
}

func (a *App) printServiceLogs(ctx context.Context, svc string, n int) {
	out, err := a.capture(ctx, fmt.Sprintf("docker service logs --no-task-ids --tail %d %s 2>&1 | tail -n %d", n, ShellQuote(a.svc(svc)), n))
	if err != nil {
		a.warnf("取不到 %s 的日志：%v", svc, err)
		return
	}
	a.infof("  %s 最近日志：", svc)
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		a.infof("    %s", a.pal.Dim(l))
	}
}

// ─────────────────────────── 锁与历史 ───────────────────────────

func (a *App) lock(ctx context.Context, force bool) error {
	owner := operator() + " " + a.now().Format(time.RFC3339)
	script := ""
	if force {
		script += "rm -rf " + remoteLockDir + "\n"
	}
	script += fmt.Sprintf("if mkdir %[1]s 2>/dev/null; then printf '%%s\\n' %[2]s > %[1]s/owner; echo ok; else echo \"busy $(cat %[1]s/owner 2>/dev/null)\"; fi\n", remoteLockDir, ShellQuote(owner))
	out, err := a.capture(ctx, script)
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(out); s != "ok" {
		return fmt.Errorf("另一次部署正在进行（%s）；确认它已经结束后加 --force-unlock", strings.TrimPrefix(s, "busy "))
	}
	return nil
}

func (a *App) unlock(ctx context.Context) {
	_, _ = a.capture(ctx, "rm -rf "+remoteLockDir)
}

// HistoryEntry 部署历史的一行（/etc/aegis/deploy-history.jsonl）
type HistoryEntry struct {
	Time     string            `json:"time"`
	Operator string            `json:"operator"`
	Action   string            `json:"action"`
	Tag      string            `json:"tag,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
	Services []string          `json:"services,omitempty"`
	Result   string            `json:"result"`
	Message  string            `json:"message,omitempty"`
	Duration string            `json:"duration,omitempty"`
}

func (a *App) appendHistory(ctx context.Context, e HistoryEntry) {
	e.Time = a.now().Format(time.RFC3339)
	e.Operator = operator()
	data, _ := json.Marshal(e)
	script := fmt.Sprintf("install -d -m 700 %s\nprintf '%%s\\n' %s >> %s\nchmod 600 %s\n", remoteConfDir, ShellQuote(string(data)), remoteHistory, remoteHistory)
	if _, err := a.capture(ctx, script); err != nil {
		a.warnf("部署历史没写进去：%v", err)
	}
}

func (a *App) cmdHistory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	n := fs.Int("n", 20, "显示最近几条")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	out, err := a.capture(ctx, fmt.Sprintf("[ -f %[1]s ] && tail -n %[2]d %[1]s || true", remoteHistory, *n))
	if err != nil {
		return err
	}
	entries, err := ParseJSONLines[HistoryEntry](out)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		a.infof("还没有部署记录")
		return nil
	}
	fmt.Fprint(a.out, renderHistory(entries, a.pal))
	return nil
}

func renderHistory(entries []HistoryEntry, p Palette) string {
	t := Table{Header: []string{"时间", "操作", "标签", "结果", "用时", "操作人", "说明"}}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		level := map[string]string{"success": "ok", "failed": "fail", "rolled_back": "fail"}[e.Result]
		ts := e.Time
		if tt, err := time.Parse(time.RFC3339, e.Time); err == nil {
			ts = tt.Local().Format("01-02 15:04:05")
		}
		msg := e.Message
		if msg == "" && len(e.Services) > 0 && len(e.Services) <= 4 {
			msg = strings.Join(e.Services, " ")
		}
		t.Rows = append(t.Rows, []string{ts, e.Action, e.Tag, p.Level(level, e.Result), e.Duration, e.Operator, msg})
	}
	return t.Render(p)
}

// ─────────────────────────── migrate / rollback ───────────────────────────

func (a *App) cmdMigrate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	tag := fs.String("tag", "", "用哪个 aegis-server 标签跑迁移（默认沿用现网 aegis-api 的）")
	timeout := fs.Duration("timeout", 15*time.Minute, "等待时长")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	in, err := a.loadDeployInputs(ctx)
	if err != nil {
		return err
	}
	st, err := a.loadState(ctx)
	if err != nil {
		return err
	}
	current := CurrentTags(st)
	vars := map[string]string{}
	for k, v := range current {
		vars[k] = v
	}
	mt := firstNonEmpty(*tag, current["AEGIS_TAG"], current[migrateTagVar])
	if mt == "" {
		return fmt.Errorf("现网没有 aegis-server 在跑：用 --tag 指定")
	}
	vars[migrateTagVar] = mt
	migrateOnly := map[string]bool{"aegis-migrate": true}
	if _, err := renderStack(in.stackTmpl, in.env.Map(), vars, migrateOnly); err != nil {
		return err
	}
	if err := a.lock(ctx, false); err != nil {
		return err
	}
	defer a.unlock(context.WithoutCancel(ctx))
	started := a.now()
	a.step("数据库迁移（aegis-server:%s）", mt)
	err = a.runMigration(ctx, func() error { return a.stackDeploy(ctx, in.stackTmpl, migrateOnly, vars) }, *timeout)
	e := HistoryEntry{Action: "migrate", Tag: mt, Result: "success", Duration: a.now().Sub(started).Round(time.Second).String()}
	if err != nil {
		e.Result, e.Message = "failed", err.Error()
	}
	a.appendHistory(context.WithoutCancel(ctx), e)
	return err
}

func (a *App) cmdRollback(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	timeout := fs.Duration("timeout", 10*time.Minute, "等待时长")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("aegisctl rollback <服务|组|all> ...")
	}
	st, err := a.loadState(ctx)
	if err != nil {
		return err
	}
	var known []string
	for s := range st.infos {
		known = append(known, s)
	}
	sort.Strings(known)
	targets := map[string]bool{}
	for _, p := range pos {
		if p == "all" {
			for _, s := range known {
				if !st.infos[s].IsJob() {
					targets[s] = true
				}
			}
			continue
		}
		set, err := ExpandServices(p, known)
		if err != nil {
			return err
		}
		for s := range set {
			if info, ok := st.infos[s]; ok && !info.IsJob() {
				targets[s] = true
			}
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("没有可回滚的服务（一次性任务不能回滚）")
	}
	since := st.now.Add(-5 * time.Second)
	started := a.now()
	var done []string
	for _, s := range sortedKeys(targets) {
		_, err := a.capture(ctx, "docker service rollback --detach "+ShellQuote(a.svc(s))+" >/dev/null")
		if err != nil {
			if strings.Contains(err.Error(), "previous spec") {
				a.infof("  %s 没有上一个版本，跳过", s)
				continue
			}
			return err
		}
		a.infof("  %s 开始回滚", s)
		done = append(done, s)
	}
	if len(done) == 0 {
		return nil
	}
	failed, err := a.waitConverge(ctx, done, since, *timeout)
	e := HistoryEntry{Action: "rollback", Services: done, Result: "success", Duration: a.now().Sub(started).Round(time.Second).String()}
	if err == nil && len(failed) > 0 {
		var names []string
		for _, f := range failed {
			if f.State == "rollback_completed" {
				continue
			}
			names = append(names, f.Service)
		}
		if len(names) > 0 {
			err = fmt.Errorf("回滚没有收敛：%s", strings.Join(names, " "))
		}
	}
	if err != nil {
		e.Result, e.Message = "failed", err.Error()
	}
	a.appendHistory(context.WithoutCancel(ctx), e)
	if err == nil {
		a.infof("%s %s", a.pal.Green("回滚完成："), strings.Join(done, " "))
		a.warnf("回滚只改运行中的服务；下次 aegisctl deploy 会按指定的标签重新发布")
	}
	return err
}

// ─────────────────────────── Traefik ───────────────────────────

func traefikPath(env map[string]string) string {
	return firstNonEmpty(env["AEGIS_TRAEFIK_DYNAMIC_FILE"], "/etc/dokploy/traefik/dynamic/aegis.yml")
}

func (a *App) applyTraefik(ctx context.Context, content string, env map[string]string) error {
	dst := traefikPath(env)
	ts := a.now().Format("20060102-150405")
	staged := remoteConfDir + "/traefik-aegis.yml.new"
	script := fmt.Sprintf("install -d -m 700 %s %s\n", remoteConfDir, remoteTraefikBak)
	script += fmt.Sprintf("if [ ! -d \"$(dirname %[1]s)\" ]; then echo nodir; exit 0; fi\n", ShellQuote(dst))
	script += WriteFileScript(staged, []byte(content), "644")
	script += fmt.Sprintf(`if [ -f %[1]s ] && cmp -s %[2]s %[1]s; then rm -f %[2]s; echo same; exit 0; fi
if [ -f %[1]s ]; then cp -p %[1]s %[3]s/aegis.yml.%[4]s; fi
cp %[2]s %[1]s.aegisctl-tmp && chmod 644 %[1]s.aegisctl-tmp && mv -f %[1]s.aegisctl-tmp %[1]s && rm -f %[2]s
{ ls -1t %[3]s/aegis.yml.* 2>/dev/null || true; } | tail -n +21 | while read -r f; do rm -f -- "$f"; done
echo written
`, ShellQuote(dst), staged, remoteTraefikBak, ts)
	out, err := a.capture(ctx, script)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(out) {
	case "nodir":
		a.warnf("manager 上没有 %s 所在目录（不是 Dokploy 的 Traefik？），跳过路由配置", dst)
	case "same":
		a.infof("  Traefik 路由没有变化")
	default:
		a.infof("  Traefik 路由已写入 %s（旧文件备份在 %s）", dst, remoteTraefikBak)
	}
	return nil
}

func (a *App) cmdTraefik(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("aegisctl traefik show|apply|disable")
	}
	in, err := a.loadDeployInputs(ctx)
	if err != nil {
		return err
	}
	env := in.env.Map()
	switch args[0] {
	case "show":
		out, err := RenderTraefik(in.traefikTmpl, env)
		if err != nil {
			return err
		}
		fmt.Fprint(a.out, out)
		return nil
	case "apply":
		out, err := RenderTraefik(in.traefikTmpl, env)
		if err != nil {
			return err
		}
		return a.applyTraefik(ctx, out, env)
	case "disable":
		dst := traefikPath(env)
		ts := a.now().Format("20060102-150405")
		out, err := a.capture(ctx, fmt.Sprintf("install -d -m 700 %[3]s\nif [ -f %[1]s ]; then mv -f %[1]s %[3]s/aegis.yml.disabled-%[2]s; echo moved; else echo none; fi", ShellQuote(dst), ts, remoteTraefikBak))
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) == "moved" {
			a.infof("已移走 %s（备份在 %s），Traefik 约 2 秒内不再按它路由", dst, remoteTraefikBak)
		} else {
			a.infof("%s 本来就不存在", dst)
		}
		return nil
	}
	return usagef("未知的 traefik 子命令 %q", args[0])
}

// ─────────────────────────── down ───────────────────────────

// cmdDown 删除栈里的服务（不删数据目录与命名卷），用于切回 Dokploy 或整体停机维护
func (a *App) cmdDown(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	keep := fs.String("keep", "", "保留的服务或组（逗号分隔），如 aegis-rustfs")
	yes := fs.Bool("yes", false, "确认执行")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	st, err := a.loadState(ctx)
	if err != nil {
		return err
	}
	var known []string
	for s := range st.infos {
		known = append(known, s)
	}
	keepSet := map[string]bool{}
	if *keep != "" {
		if keepSet, err = ExpandServices(*keep, known); err != nil {
			return err
		}
	}
	var remove []string
	for _, s := range known {
		if !keepSet[s] {
			remove = append(remove, a.svc(s))
		}
	}
	sort.Strings(remove)
	if len(remove) == 0 {
		a.infof("没有要删除的服务")
		return nil
	}
	a.infof("将删除 %d 个服务（数据目录与命名卷保留）：%s", len(remove), strings.Join(remove, " "))
	if !*yes && !a.confirm("确认？") {
		return errors.New("已放弃（非交互时加 --yes）")
	}
	if _, err := a.capture(ctx, "docker service rm "+ShellJoin(remove...)+" >/dev/null"); err != nil {
		return err
	}
	// 等容器真正退出：有状态服务的数据目录要先放开，Dokploy 那边才能重新启动
	deadline := a.now().Add(3 * time.Minute)
	for {
		out, err := a.capture(ctx, "docker ps -q --filter label=com.docker.stack.namespace="+ShellQuote(a.stack())+" | wc -l")
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(strings.TrimSpace(out))
		if n <= len(keepSet) || a.now().After(deadline) {
			a.infof("manager 上剩余 %d 个栈容器（其他节点上的容器同样会退出）", n)
			break
		}
		if err := sleepCtx(ctx, 3*time.Second); err != nil {
			return err
		}
	}
	a.appendHistory(context.WithoutCancel(ctx), HistoryEntry{Action: "down", Services: remove, Result: "success"})
	return nil
}
