package service

import (
	"context"
	"testing"
	"time"
)

// TestAIGatewayInflightCancelOnShutdown 退出时在途调用被取消、原因可识别；收尾后 wait 返回；
// 关停之后新登记的调用直接以退出为由取消。
func TestAIGatewayInflightCancelOnShutdown(t *testing.T) {
	registry := newAIGatewayInflight()
	ctx, done := registry.track(context.Background())
	if registry.count() != 1 || ctx.Err() != nil {
		t.Fatalf("登记后应在途且未取消")
	}
	if canceled := registry.cancelAll(); canceled != 1 || !aiGatewayShuttingDown(ctx) {
		t.Fatalf("cancelAll 应取消在途调用并带上退出原因：%d %v", canceled, context.Cause(ctx))
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		done()
	}()
	if !registry.wait(time.Second) || registry.count() != 0 {
		t.Fatalf("收尾后 wait 应返回")
	}
	late, lateDone := registry.track(context.Background())
	defer lateDone()
	if !aiGatewayShuttingDown(late) {
		t.Fatalf("关停后新登记的调用应直接取消")
	}
	// 普通取消（客户端断开）不算退出。
	plain, cancel := context.WithCancel(context.Background())
	cancel()
	if aiGatewayShuttingDown(plain) {
		t.Fatalf("客户端断开不该被当成退出")
	}
}
