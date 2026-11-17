package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ssh.go：在远端主机上执行命令。所有远端操作都经 Executor，测试里换成假的实现。

// Remote 一次远端执行
type Remote struct {
	Host string
	// Script 用 bash 执行的脚本。Stdin 为空时脚本经 ssh 的标准输入传过去（不出现在远端进程参数里，
	// 可以内嵌密钥）；Stdin 不为空或 TTY 时脚本作为 bash -c 的参数，此时脚本里不能有密钥
	Script string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// TTY 分配终端（交互命令、跟随日志时 Ctrl-C 能传到远端）
	TTY bool
}

// Executor 远端执行器
type Executor interface {
	Run(ctx context.Context, r Remote) error
}

// SSHExecutor 用系统的 ssh 命令执行
type SSHExecutor struct {
	Sudo    bool
	SSHArgs []string
}

func (e *SSHExecutor) baseArgs(tty bool) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=30"}
	if dir, err := controlDir(); err == nil {
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPath="+filepath.Join(dir, "%C"), "-o", "ControlPersist=120")
	}
	if tty {
		args = append(args, "-t")
	}
	return append(args, e.SSHArgs...)
}

func controlDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "aegisctl", "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Run 执行
func (e *SSHExecutor) Run(ctx context.Context, r Remote) error {
	if r.Host == "" {
		return fmt.Errorf("没有指定远端主机")
	}
	prefix := "bash"
	if e.Sudo {
		prefix = "sudo -n bash"
	}
	args := e.baseArgs(r.TTY)
	var stdin io.Reader
	if r.Stdin == nil && !r.TTY {
		args = append(args, r.Host, prefix+" -euo pipefail -s")
		stdin = strings.NewReader(r.Script)
	} else {
		args = append(args, r.Host, prefix+" -euo pipefail -c "+ShellQuote(r.Script))
		stdin = r.Stdin
		if r.TTY && stdin == nil {
			stdin = os.Stdin
		}
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if cmd.Stdout == nil {
		cmd.Stdout = io.Discard
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// ShellQuote 单引号转义
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_./:=@,+%", c)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin 把参数拼成命令行
func ShellJoin(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = ShellQuote(a)
	}
	return strings.Join(q, " ")
}

// WriteFileScript 生成把内容原子写入远端文件的脚本片段（内容用 base64 内嵌，只能经标准输入传脚本）
func WriteFileScript(path string, content []byte, mode string) string {
	enc := base64.StdEncoding.EncodeToString(content)
	var b strings.Builder
	tmp := path + ".aegisctl-tmp"
	fmt.Fprintf(&b, "umask 077\n")
	fmt.Fprintf(&b, "base64 -d > %s <<'AEGISCTL_EOF'\n", ShellQuote(tmp))
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteByte('\n')
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString("\nAEGISCTL_EOF\n")
	fmt.Fprintf(&b, "chmod %s %s\n", mode, ShellQuote(tmp))
	fmt.Fprintf(&b, "mv -f %s %s\n", ShellQuote(tmp), ShellQuote(path))
	return b.String()
}

// RemoteError 带上远端标准错误的错误
type RemoteError struct {
	Host   string
	Err    error
	Stderr string
}

func (e *RemoteError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if len(msg) > 2000 {
		msg = "…" + msg[len(msg)-2000:]
	}
	if msg == "" {
		return fmt.Sprintf("在 %s 上执行失败：%v", e.Host, e.Err)
	}
	return fmt.Sprintf("在 %s 上执行失败：%v\n%s", e.Host, e.Err, msg)
}

func (e *RemoteError) Unwrap() error { return e.Err }

// Capture 执行脚本并取回标准输出
func Capture(ctx context.Context, ex Executor, host, script string) (string, error) {
	var out, errb bytes.Buffer
	err := ex.Run(ctx, Remote{Host: host, Script: script, Stdout: &out, Stderr: &errb})
	if err != nil {
		return out.String(), &RemoteError{Host: host, Err: err, Stderr: errb.String()}
	}
	return out.String(), nil
}
