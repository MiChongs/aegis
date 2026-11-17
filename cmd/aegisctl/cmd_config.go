package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// cmd_config.go：cluster.env 的读写。唯一真源在 manager 上；本机只放 pull 下来的备份（600）。
// 任何输出都不打印敏感值的明文（get --reveal 除外）。

func (a *App) cmdConfig(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("aegisctl config get|set|unset|edit|diff|pull|push")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "get":
		return a.configGet(ctx, rest)
	case "set":
		return a.configSet(ctx, rest)
	case "unset":
		return a.configUnset(ctx, rest)
	case "edit":
		return a.configEdit(ctx)
	case "diff":
		return a.configDiff(ctx, rest)
	case "pull":
		return a.configPull(ctx)
	case "push":
		return a.configPush(ctx, rest)
	}
	return usagef("未知的 config 子命令 %q", sub)
}

func (a *App) configGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("config get", flag.ContinueOnError)
	reveal := fs.Bool("reveal", false, "显示敏感值的明文")
	keys, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	f, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	if *reveal && !a.stdinTTY {
		return fmt.Errorf("--reveal 只能在交互终端里用（防止明文进日志与管道）")
	}
	if len(keys) == 0 {
		keys = f.Keys()
	}
	missing := 0
	for _, k := range keys {
		v, ok := f.Get(k)
		if !ok {
			fmt.Fprintf(a.out, "%s %s\n", k, a.pal.Dim("（未设置）"))
			missing++
			continue
		}
		fmt.Fprintf(a.out, "%s=%s\n", k, DisplayValue(k, v, *reveal))
	}
	if missing > 0 && len(keys) == missing {
		return errors.New("要找的键都没有设置")
	}
	return nil
}

// ParseAssignments 解析 KEY=VALUE；KEY - 或 KEY=- 表示从标准输入读
func ParseAssignments(args []string) (map[string]string, []string, error) {
	vals := map[string]string{}
	var fromStdin []string
	for i := 0; i < len(args); i++ {
		k, v, ok := strings.Cut(args[i], "=")
		if !ok {
			if i+1 < len(args) && args[i+1] == "-" {
				i++
				v, ok = "-", true
			}
		}
		if !ok || !envKeyPattern.MatchString(k) {
			return nil, nil, usagef("参数要写成 KEY=VALUE（从标准输入读值写 KEY -）：%q", args[i])
		}
		if v == "-" {
			fromStdin = append(fromStdin, k)
		} else {
			vals[k] = v
		}
	}
	if len(fromStdin) > 1 {
		return nil, nil, usagef("一次只能有一个键从标准输入读值")
	}
	return vals, fromStdin, nil
}

func (a *App) readSecretValue(key string) (string, error) {
	if a.stdinTTY {
		fmt.Fprintf(a.errOut, "输入 %s 的值（不回显）：", key)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(a.errOut)
		return string(b), err
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (a *App) configSet(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("aegisctl config set KEY=VALUE ... | aegisctl config set KEY -")
	}
	vals, fromStdin, err := ParseAssignments(args)
	if err != nil {
		return err
	}
	for _, k := range fromStdin {
		v, err := a.readSecretValue(k)
		if err != nil {
			return err
		}
		vals[k] = v
	}
	f, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	before := f.Map()
	for k, v := range vals {
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("%s 的值不能含换行", k)
		}
		f.Set(k, v)
	}
	changes := DiffEnv(before, f.Map())
	if len(changes) == 0 {
		a.infof("没有变化")
		return nil
	}
	if err := a.writeRemoteEnv(ctx, f); err != nil {
		return err
	}
	a.printChanges(changes)
	a.infof("已写入 %s。%s", remoteEnvFile, a.pal.Yellow("要 aegisctl deploy 后才对运行中的服务生效。"))
	return nil
}

func (a *App) configUnset(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return usagef("aegisctl config unset KEY ...")
	}
	f, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	before := f.Map()
	for _, k := range keys {
		if !f.Unset(k) {
			a.warnf("%s 本来就没有", k)
		}
	}
	changes := DiffEnv(before, f.Map())
	if len(changes) == 0 {
		return nil
	}
	if err := a.writeRemoteEnv(ctx, f); err != nil {
		return err
	}
	a.printChanges(changes)
	a.infof("已写入 %s。%s", remoteEnvFile, a.pal.Yellow("要 aegisctl deploy 后才对运行中的服务生效。"))
	return nil
}

func (a *App) printChanges(changes []EnvChange) {
	for _, c := range changes {
		switch c.Kind {
		case "added":
			fmt.Fprintf(a.out, "%s %s  sha256:%s\n", a.pal.Green("+"), c.Key, c.To)
		case "removed":
			fmt.Fprintf(a.out, "%s %s  sha256:%s\n", a.pal.Red("-"), c.Key, c.From)
		default:
			fmt.Fprintf(a.out, "%s %s  sha256:%s → sha256:%s\n", a.pal.Yellow("~"), c.Key, c.From, c.To)
		}
	}
}

func (a *App) configEdit(ctx context.Context) error {
	if !a.stdinTTY {
		return fmt.Errorf("config edit 需要交互终端")
	}
	f, body, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "aegisctl-edit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "cluster.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	for {
		cmd := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "sh", path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("编辑器退出异常：%w", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		edited, problems := ParseEnv(string(data))
		if len(problems) > 0 {
			a.warnf("%s", strings.Join(problems, "；"))
			if a.confirm("有不合法的行，重新编辑？") {
				continue
			}
			return errors.New("已放弃，线上文件未改动")
		}
		changes := DiffEnv(f.Map(), edited.Map())
		if len(changes) == 0 && string(data) == body {
			a.infof("没有变化")
			return nil
		}
		a.printChanges(changes)
		if q := QuotedValueKeys(edited); len(q) > 0 {
			a.warnf("这些键的值两端带引号（docker 不去引号，会原样进值里）：%s", strings.Join(q, " "))
		}
		if !a.confirm("写回 manager？") {
			return errors.New("已放弃，线上文件未改动")
		}
		if err := a.writeRemoteEnv(ctx, edited); err != nil {
			return err
		}
		a.infof("已写入 %s。%s", remoteEnvFile, a.pal.Yellow("要 aegisctl deploy 后才对运行中的服务生效。"))
		return nil
	}
}

func (a *App) readLocalBackup() (*EnvFile, error) {
	data, err := os.ReadFile(a.cfg.LocalEnvBackup())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("本机还没有备份 %s：先 aegisctl config pull", a.cfg.LocalEnvBackup())
		}
		return nil, err
	}
	f, _ := ParseEnv(string(data))
	return f, nil
}

func (a *App) configDiff(ctx context.Context, args []string) error {
	local, err := a.readLocalBackup()
	if err != nil {
		return err
	}
	remote, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	changes := DiffEnv(local.Map(), remote.Map())
	if len(changes) == 0 {
		a.infof("%s 本机备份与线上一致（%d 个键）", a.pal.Green("一致："), len(remote.Keys()))
		return nil
	}
	a.infof("本机备份 → 线上（+ 线上多出，- 线上没有，~ 值不同）：")
	a.printChanges(changes)
	return nil
}

func (a *App) configPull(ctx context.Context) error {
	_, body, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	path := a.cfg.LocalEnvBackup()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) != body {
		_ = os.WriteFile(path+".prev", old, 0o600)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	f, _ := ParseEnv(body)
	a.infof("已备份到 %s（600，%d 个键，sha256:%s）", path, len(f.Keys()), Fingerprint(body))
	return nil
}

func (a *App) configPush(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("config push", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "不确认直接上传")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	local, err := a.readLocalBackup()
	if err != nil {
		return err
	}
	remote, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	changes := DiffEnv(remote.Map(), local.Map())
	if len(changes) == 0 {
		a.infof("线上已与本机备份一致")
		return nil
	}
	a.infof("线上 → 本机备份（上传后线上将变成右边）：")
	a.printChanges(changes)
	if !*yes && !a.confirm("用本机备份覆盖线上？") {
		return errors.New("已放弃")
	}
	if err := a.writeRemoteEnv(ctx, local); err != nil {
		return err
	}
	a.infof("已上传。%s", a.pal.Yellow("要 aegisctl deploy 后才对运行中的服务生效。"))
	return nil
}
