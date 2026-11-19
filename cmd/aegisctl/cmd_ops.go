package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// cmd_ops.go：logs / scale / exec / backup

func (a *App) cmdLogs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "持续输出")
	fs.BoolVar(follow, "follow", false, "持续输出")
	sinceFlag := fs.String("since", "", "从多久之前开始（如 10m、2h、2026-11-17T10:00:00）")
	tail := fs.String("tail", "200", "最后多少行（all 为全部）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("aegisctl logs <服务> [-f] [--since 10m] [--tail 200]")
	}
	if *tail != "all" {
		if _, err := strconv.Atoi(*tail); err != nil {
			return usagef("--tail 要写数字或 all")
		}
	}
	cmd := []string{"docker", "service", "logs", "--timestamps", "--tail", *tail}
	if *follow {
		cmd = append(cmd, "--follow")
	}
	if *sinceFlag != "" {
		cmd = append(cmd, "--since", *sinceFlag)
	}
	cmd = append(cmd, a.svc(pos[0]))
	return a.ex.Run(ctx, Remote{Host: a.manager(), Script: ShellJoin(cmd...) + " 2>&1", TTY: *follow && a.stdinTTY, Stdin: nilIfTTY(*follow && a.stdinTTY), Stdout: a.out, Stderr: a.errOut})
}

func nilIfTTY(tty bool) io.Reader {
	if tty {
		return nil
	}
	return strings.NewReader("")
}

// ParseScale 解析 服务=副本数
func ParseScale(args []string) (map[string]int, error) {
	res := map[string]int{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		n, err := strconv.Atoi(v)
		if !ok || err != nil || n < 0 || n > 100 {
			return nil, usagef("要写成 服务=副本数（0 到 100）：%q", a)
		}
		if _, ok := scalable[k]; !ok {
			names := make([]string, 0, len(scalable))
			for s := range scalable {
				names = append(names, s)
			}
			return nil, usagef("%s 不能扩缩（可扩缩：%s；有状态服务固定 1 个副本）", k, strings.Join(sortedStrings(names), "、"))
		}
		res[k] = n
	}
	return res, nil
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (a *App) cmdScale(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("scale", flag.ContinueOnError)
	noPersist := fs.Bool("no-persist", false, "只改运行中的服务，不写回 cluster.env（下次 deploy 会恢复）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("aegisctl scale <服务>=<N> ...")
	}
	want, err := ParseScale(pos)
	if err != nil {
		return err
	}
	var parts []string
	for s, n := range want {
		parts = append(parts, a.svc(s)+"="+strconv.Itoa(n))
	}
	parts = sortedStrings(parts)
	a.step("docker service scale %s", strings.Join(parts, " "))
	if err := a.ex.Run(ctx, Remote{Host: a.manager(), Script: "docker service scale --detach " + ShellJoin(parts...), Stdout: a.out, Stderr: a.errOut}); err != nil {
		return err
	}
	if !*noPersist {
		f, _, err := a.remoteEnv(ctx)
		if err != nil {
			return err
		}
		for s, n := range want {
			f.Set(scalable[s], strconv.Itoa(n))
		}
		if err := a.writeRemoteEnv(ctx, f); err != nil {
			return err
		}
		a.infof("已同步写回 cluster.env，下次 deploy 保持这个副本数")
	}
	a.appendHistory(ctx, HistoryEntry{Action: "scale", Services: parts, Result: "success"})
	return nil
}

// locateTask 找一个运行中的任务：所在节点与容器 ID
func (a *App) locateTask(ctx context.Context, svc string) (node, container string, err error) {
	script := fmt.Sprintf(`t=$(docker service ps --no-trunc --filter desired-state=running --format '{{.ID}} {{.Node}} {{.CurrentState}}' %s | awk '$3=="Running"{print $1" "$2; exit}')
[ -n "$t" ] || { echo none; exit 0; }
set -- $t
echo "$2 $(docker inspect --format '{{with .Status.ContainerStatus}}{{.ContainerID}}{{end}}' "$1")"`, ShellQuote(a.svc(svc)))
	out, err := a.capture(ctx, script)
	if err != nil {
		return "", "", err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return "", "", fmt.Errorf("%s 没有正在运行的任务", svc)
	}
	return f[0], f[1], nil
}

func (a *App) cmdExec(ctx context.Context, args []string) error {
	pos, err := parseFlags(flag.NewFlagSet("exec", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) < 3 || pos[1] != "--" {
		return usagef("aegisctl exec <服务> -- <命令 ...>")
	}
	svc, cmd := pos[0], pos[2:]
	node, cid, err := a.locateTask(ctx, svc)
	if err != nil {
		return err
	}
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	host := a.cfg.HostForNode(node, nodes.selfHostname())
	fmt.Fprintf(a.errOut, "%s\n", a.pal.Dim(fmt.Sprintf("→ %s 上的 %s（%s）", node, svc, shortID(cid))))
	flags := "-i"
	if a.stdinTTY {
		flags = "-it"
	}
	script := "exec docker exec " + flags + " " + ShellQuote(cid) + " " + ShellJoin(cmd...)
	return a.ex.Run(ctx, Remote{Host: host, Script: script, TTY: a.stdinTTY, Stdin: nilIfTTYStdin(a.stdinTTY), Stdout: a.out, Stderr: a.errOut})
}

func nilIfTTYStdin(tty bool) io.Reader {
	if tty {
		return nil
	}
	return os.Stdin
}

// ─────────────────────────── backup ───────────────────────────

var backupNamePattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

// PruneBackupsScript 只删名字是「日期-时间」的目录，保留最近 keep 份
func PruneBackupsScript(base string, keep int) string {
	return fmt.Sprintf(`base=%s
n=0
for d in $(ls -1d "$base"/[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9] 2>/dev/null | sort -r); do
  n=$((n+1))
  if [ "$n" -gt %d ] && [ -d "$d" ]; then rm -rf -- "$d"; echo "删除旧备份 $(basename "$d")"; fi
done
`, ShellQuote(base), keep)
}

func validBackupBase(p string) bool {
	// 远端（Linux）路径：用 path 而不是 filepath，Windows 上后者会把斜杠换成反斜杠
	clean := path.Clean(p)
	return strings.HasPrefix(clean, "/") && clean != "/" && strings.Count(clean, "/") >= 2 && clean == p
}

func (a *App) cmdBackup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	to := fs.String("to", "", "同时下载到本机这个目录")
	keep := fs.Int("keep", 0, "manager 上保留几份（默认 cluster.env 的 AEGIS_BACKUP_KEEP，再默认 7）")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	envFile, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	env := envFile.Map()
	base := firstNonEmpty(env["AEGIS_BACKUP_DIR"], "/srv/aegis/backups")
	if !validBackupBase(base) {
		return fmt.Errorf("AEGIS_BACKUP_DIR 不是安全的绝对路径：%q", base)
	}
	if *keep <= 0 {
		*keep, _ = strconv.Atoi(env["AEGIS_BACKUP_KEEP"])
		if *keep <= 0 {
			*keep = 7
		}
	}
	nodes, err := a.loadNodes(ctx)
	if err != nil {
		return err
	}
	ts := a.now().Format("20060102-150405")
	dir := base + "/" + ts
	if _, err := a.capture(ctx, fmt.Sprintf("install -d -m 700 %s %s", ShellQuote(base), ShellQuote(dir))); err != nil {
		return err
	}
	a.infof("备份到 %s:%s", a.manager(), dir)

	// Postgres
	pgNode, pgCID, err := a.locateTask(ctx, "postgres")
	if err != nil {
		return err
	}
	a.step("Postgres：pg_dumpall（%s）", pgNode)
	dump := "docker exec " + ShellQuote(pgCID) + ` sh -c 'pg_dumpall -U "$POSTGRES_USER" --clean --if-exists' | gzip -c`
	if err := a.streamToManager(ctx, a.cfg.HostForNode(pgNode, nodes.selfHostname()), dump, dir+"/postgres.sql.gz"); err != nil {
		return fmt.Errorf("Postgres 备份失败：%w", err)
	}

	// Redis
	rdNode, rdCID, err := a.locateTask(ctx, "redis")
	if err != nil {
		return err
	}
	a.step("Redis：BGSAVE 后拷贝 RDB（%s）", rdNode)
	rdHost := a.cfg.HostForNode(rdNode, nodes.selfHostname())
	save := "docker exec " + ShellQuote(rdCID) + ` sh -c 'before=$(redis-cli LASTSAVE); redis-cli BGSAVE >/dev/null; i=0; while [ "$(redis-cli LASTSAVE)" = "$before" ]; do i=$((i+1)); [ $i -gt 300 ] && { echo "BGSAVE 超时" >&2; exit 1; }; sleep 1; done; echo ok'`
	if _, err := Capture(ctx, a.ex, rdHost, save); err != nil {
		return fmt.Errorf("Redis BGSAVE 失败：%w", err)
	}
	copyRDB := "docker exec " + ShellQuote(rdCID) + ` sh -c 'd=$(redis-cli CONFIG GET dir | sed -n 2p); f=$(redis-cli CONFIG GET dbfilename | sed -n 2p); cat "$d/$f"' | gzip -c`
	if err := a.streamToManager(ctx, rdHost, copyRDB, dir+"/redis-dump.rdb.gz"); err != nil {
		return fmt.Errorf("Redis 备份失败：%w", err)
	}

	// NATS 配置与 cluster.env
	a.step("NATS 配置与 cluster.env")
	conf := firstNonEmpty(env["AEGIS_NATS_CONF"], "/srv/aegis/nats/nats-server.conf")
	natsNode, _, err := a.locateTask(ctx, "nats")
	natsHost := a.manager()
	if err == nil {
		natsHost = a.cfg.HostForNode(natsNode, nodes.selfHostname())
	}
	if err := a.streamToManager(ctx, natsHost, "cat "+ShellQuote(conf), dir+"/nats-server.conf"); err != nil {
		a.warnf("NATS 配置没备份上：%v", err)
	}
	if _, err := a.capture(ctx, fmt.Sprintf("cp -p %s %s/cluster.env", remoteEnvFile, ShellQuote(dir))); err != nil {
		return err
	}

	out, err := a.capture(ctx, fmt.Sprintf("cd %s && chmod 600 * && sha256sum * > SHA256SUMS && du -sh . | cut -f1 && ls -l | tail -n +2 | awk '{print $5\"\\t\"$9}'\n", ShellQuote(dir))+PruneBackupsScript(base, *keep))
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 0 {
		a.infof("  合计 %s", lines[0])
		for _, l := range lines[1:] {
			a.infof("  %s", l)
		}
	}

	if *to != "" {
		local := filepath.Join(*to, ts)
		if err := os.MkdirAll(local, 0o700); err != nil {
			return err
		}
		a.step("下载到本机 %s", local)
		for _, name := range []string{"postgres.sql.gz", "redis-dump.rdb.gz", "nats-server.conf", "cluster.env", "SHA256SUMS"} {
			fh, err := os.OpenFile(filepath.Join(local, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			var errb strings.Builder
			err = a.ex.Run(ctx, Remote{Host: a.manager(), Script: "cat " + ShellQuote(dir+"/"+name), Stdout: fh, Stderr: &errb})
			fh.Close()
			if err != nil {
				return &RemoteError{Host: a.manager(), Err: err, Stderr: errb.String()}
			}
		}
	}
	a.infof("%s 恢复步骤见 docs/distributed-deploy.md「备份与恢复」", a.pal.Green("备份完成。"))
	return nil
}

// streamToManager 在 src 上执行命令，把标准输出写进 manager 上的文件（同一台机器时不经本机中转）
func (a *App) streamToManager(ctx context.Context, src, cmd, dst string) error {
	if src == a.manager() {
		_, err := a.capture(ctx, "umask 077\n"+cmd+" > "+ShellQuote(dst))
		return err
	}
	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		var errb strings.Builder
		err := a.ex.Run(ctx, Remote{Host: src, Script: cmd, Stdout: pw, Stderr: &errb})
		if err != nil {
			err = &RemoteError{Host: src, Err: err, Stderr: errb.String()}
		}
		pw.CloseWithError(err)
		errc <- err
	}()
	var errb strings.Builder
	err := a.ex.Run(ctx, Remote{Host: a.manager(), Script: "umask 077; cat > " + ShellQuote(dst), Stdin: pr, Stderr: &errb})
	srcErr := <-errc
	if srcErr != nil {
		return srcErr
	}
	if err != nil {
		return &RemoteError{Host: a.manager(), Err: err, Stderr: errb.String()}
	}
	return nil
}
