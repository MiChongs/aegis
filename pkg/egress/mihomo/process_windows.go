//go:build windows

package mihomo

import (
	"os"
	"os/exec"
)

// Windows 没有可投递给子进程的 SIGTERM，直接结束。
func interruptProcess(p *os.Process) error { return p.Kill() }

func configureProcess(*exec.Cmd) {}
