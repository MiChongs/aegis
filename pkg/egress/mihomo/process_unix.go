//go:build !windows

package mihomo

import (
	"os"
	"os/exec"
	"syscall"
)

func interruptProcess(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeathSignal(cmd.SysProcAttr)
}
