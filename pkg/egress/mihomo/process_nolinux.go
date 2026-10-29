//go:build !linux && !windows

package mihomo

import "syscall"

func setParentDeathSignal(*syscall.SysProcAttr) {}
