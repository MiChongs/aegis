package service

import (
	"testing"
	"time"
)

// 测试共用的小工具（原先在旧风控中心的集成测试里）

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("等待超时")
}
