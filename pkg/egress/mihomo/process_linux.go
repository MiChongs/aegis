//go:build linux

package mihomo

import "syscall"

// setParentDeathSignal 网关进程被强杀时让核心一起退出，避免留下占着端口的孤儿进程。
func setParentDeathSignal(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
