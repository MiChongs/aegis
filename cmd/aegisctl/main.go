// aegisctl：Aegis 统一栈（Docker Swarm）的运维命令行。
//
// 在运维者本机运行，经 ssh 到 manager（以及需要时到其他节点）执行 docker 命令。
// 唯一的配置真源是 manager 上的 /etc/aegis/cluster.env；栈文件与模板从本机仓库读取。
// 完整说明见 docs/distributed-deploy.md。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const usage = `aegisctl：Aegis 统一栈（Docker Swarm）运维命令行

用法：aegisctl [全局选项] <命令> [参数]

全局选项（可写在任意位置）：
  --manager <ssh 主机>   Swarm manager（默认取 config.json，再默认 hk-paas）
  --config <文件>        本机配置（默认 ~/.config/aegisctl/config.json）
  --repo <目录>          本机仓库根目录（默认从当前目录向上找）
  --no-color             不着色（也认 NO_COLOR 环境变量；输出不是终端时自动关闭）

集群：
  init [--import-dokploy] [--advertise-addr IP] [--dry-run]
                          准备 manager：/etc/aegis、cluster.env、数据目录、网络、节点标签
  node join <ssh 主机> [--role worker|manager] [--labels k=v,...] [--advertise-addr IP]
  node ls                 节点、标签、状态与每个节点上的 aegis 任务数
  node label <节点> k=v [k2=v2 ...] [k3-]
  node drain|active <节点>
  node rm <节点> [--force]
  doctor                  体检：Swarm、标签、cluster.env、磁盘、镜像、Traefik、时钟

配置（唯一真源：manager 上的 /etc/aegis/cluster.env）：
  config get [KEY ...] [--reveal]
  config set KEY=VALUE ... | config set KEY -（从标准输入读值）
  config unset KEY ...
  config edit             下载到临时文件、用 $EDITOR 编辑、确认后上传
  config diff             本机备份与线上的差异（只显示键名与值的 sha256 前 8 位）
  config pull | push [--yes]

发布：
  build [镜像 ...] [--ref 提交] [--tag 标签]   在 manager 上构建并推送镜像
  deploy [--tag 标签] [--only 服务,...] [--timeout 15m] [--dry-run]
  migrate [--tag 标签]    单独跑一次数据库迁移任务
  rollback <服务|组|all>
  down [--keep 服务,...] [--yes]   删除栈里的服务（数据目录与命名卷保留），切回 Dokploy 时用
  history [-n 20]
  traefik show|apply|disable

运行：
  status [--watch] [--interval 5s]
  logs <服务> [-f] [--since 10m] [--tail 200]
  scale <服务>=<N> ...
  exec <服务> -- <命令 ...>
  backup [--to 本机目录] [--keep N]
  objstore migrate [--to risk-rustfs|aegis-rustfs] [--from-dir 目录] [--force] [--yes]
                          把旧 MinIO 数据目录里的全部存储桶复制进 RustFS 并逐对象核对（可重复执行）
  objstore check [--to ...]   只核对，不复制
`

// App 一次命令执行的上下文
type App struct {
	cfg      *Config
	ex       Executor
	out      io.Writer
	errOut   io.Writer
	pal      Palette
	repoFlag string
	now      func() time.Time
	stdinTTY bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// globalFlags 从任意位置摘出全局选项（-- 之后的不动）
type globalFlags struct {
	manager, config, repo string
	noColor, help         bool
}

func extractGlobal(args []string) (globalFlags, []string, error) {
	var g globalFlags
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			continue
		}
		takeVal := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s 需要一个值", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "manager":
			g.manager, err = takeVal()
		case "config":
			g.config, err = takeVal()
		case "repo":
			g.repo, err = takeVal()
		case "no-color":
			g.noColor = true
		case "h", "help":
			g.help = true
		default:
			rest = append(rest, a)
		}
		if err != nil {
			return g, nil, err
		}
	}
	return g, rest, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	g, rest, err := extractGlobal(args)
	if err != nil {
		fmt.Fprintln(stderr, "错误：", err)
		return 2
	}
	if g.help || len(rest) == 0 || rest[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	cfg, err := LoadConfig(g.config)
	if err != nil {
		fmt.Fprintln(stderr, "错误：", err)
		return 2
	}
	if g.manager != "" {
		cfg.Manager = g.manager
	}
	app := &App{
		cfg:      cfg,
		ex:       &SSHExecutor{Sudo: cfg.Sudo, SSHArgs: cfg.SSHArgs},
		out:      stdout,
		errOut:   stderr,
		pal:      NewPalette(stdout, g.noColor),
		repoFlag: g.repo,
		now:      time.Now,
		stdinTTY: term.IsTerminal(int(os.Stdin.Fd())),
	}
	if err := app.dispatch(ctx, rest[0], rest[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(stderr, "用法错误：", ue.msg)
			return 2
		}
		fmt.Fprintln(stderr, app.pal.Red("错误："), err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

func (a *App) dispatch(ctx context.Context, cmd string, args []string) error {
	switch cmd {
	case "init":
		return a.cmdInit(ctx, args)
	case "node":
		return a.cmdNode(ctx, args)
	case "config":
		return a.cmdConfig(ctx, args)
	case "build":
		return a.cmdBuild(ctx, args)
	case "deploy":
		return a.cmdDeploy(ctx, args)
	case "migrate":
		return a.cmdMigrate(ctx, args)
	case "rollback":
		return a.cmdRollback(ctx, args)
	case "down":
		return a.cmdDown(ctx, args)
	case "history":
		return a.cmdHistory(ctx, args)
	case "traefik":
		return a.cmdTraefik(ctx, args)
	case "status":
		return a.cmdStatus(ctx, args)
	case "logs":
		return a.cmdLogs(ctx, args)
	case "scale":
		return a.cmdScale(ctx, args)
	case "exec":
		return a.cmdExec(ctx, args)
	case "backup":
		return a.cmdBackup(ctx, args)
	case "objstore":
		return a.cmdObjstore(ctx, args)
	case "doctor":
		return a.cmdDoctor(ctx, args)
	}
	return usagef("未知命令 %q，aegisctl help 查看全部命令", cmd)
}

// parseFlags 允许选项与位置参数交错（node join host --role worker）；-- 之后原样作为位置参数
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if len(args) > 0 && args[0] == "--" {
			return append(pos, args...), nil
		}
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageError{err.Error()}
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		if rest[0] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// ─────────────────────────── 常用远端操作 ───────────────────────────

func (a *App) manager() string { return a.cfg.Manager }

func (a *App) stack() string { return a.cfg.Stack }

func (a *App) svc(short string) string { return a.stack() + "_" + short }

func (a *App) capture(ctx context.Context, script string) (string, error) {
	return Capture(ctx, a.ex, a.manager(), script)
}

func (a *App) repo() (string, error) {
	if a.repoFlag != "" {
		return FindRepo(a.repoFlag)
	}
	return FindRepo(a.cfg.RepoDir)
}

// remoteEnv 读线上 cluster.env（只在内存里）
func (a *App) remoteEnv(ctx context.Context) (*EnvFile, string, error) {
	out, err := a.capture(ctx, fmt.Sprintf("if [ -f %[1]s ]; then echo present; cat %[1]s; else echo absent; fi", remoteEnvFile))
	if err != nil {
		return nil, "", err
	}
	status, body, _ := strings.Cut(out, "\n")
	if strings.TrimSpace(status) != "present" {
		return nil, "", fmt.Errorf("manager 上还没有 %s：先运行 aegisctl init", remoteEnvFile)
	}
	f, problems := ParseEnv(body)
	for _, p := range problems {
		fmt.Fprintln(a.errOut, a.pal.Yellow("注意："), remoteEnvFile, p)
	}
	return f, body, nil
}

// writeRemoteEnv 备份旧文件后原子写入
func (a *App) writeRemoteEnv(ctx context.Context, f *EnvFile) error {
	ts := a.now().Format("20060102-150405")
	script := fmt.Sprintf("install -d -m 700 %[1]s %[1]s/env-backups\nif [ -f %[2]s ]; then cp -p %[2]s %[1]s/env-backups/cluster.env.%[3]s; fi\n", remoteConfDir, remoteEnvFile, ts)
	script += WriteFileScript(remoteEnvFile, []byte(f.String()), "600")
	// 只留最近 20 份备份
	// 还没有备份时 ls 匹配不到会以 2 退出，pipefail 下整段脚本跟着失败，所以吞掉它的退出码
	script += fmt.Sprintf("{ ls -1t %s/env-backups/cluster.env.* 2>/dev/null || true; } | tail -n +21 | while read -r f; do rm -f -- \"$f\"; done\n", remoteConfDir)
	_, err := a.capture(ctx, script)
	return err
}

func operator() string {
	name := "unknown"
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	host, _ := os.Hostname()
	return name + "@" + host
}

func (a *App) infof(format string, args ...any) {
	fmt.Fprintf(a.out, format+"\n", args...)
}

func (a *App) step(format string, args ...any) {
	fmt.Fprintf(a.out, "%s %s\n", a.pal.Bold("▶"), fmt.Sprintf(format, args...))
}

func (a *App) warnf(format string, args ...any) {
	fmt.Fprintf(a.errOut, "%s %s\n", a.pal.Yellow("注意："), fmt.Sprintf(format, args...))
}

// confirm 交互确认；非终端时视为否
func (a *App) confirm(prompt string) bool {
	if !a.stdinTTY {
		return false
	}
	fmt.Fprintf(a.out, "%s [y/N] ", prompt)
	var ans string
	_, _ = fmt.Fscanln(os.Stdin, &ans)
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}

func newFlagSet(name string) *flag.FlagSet { return flag.NewFlagSet(name, flag.ContinueOnError) }
