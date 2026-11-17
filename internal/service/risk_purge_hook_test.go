package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"

	"go.uber.org/zap"
)

// 任何路径删号之后都要通知风控收尾；风控关联删除自己通知，不能再重复一次
func TestRiskPurgeHookNotifiesRisk(t *testing.T) {
	var mu sync.Mutex
	var calls []map[string]any
	done := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/subjects/forget" || r.Header.Get("X-Risk-Token") != "tok" {
			t.Errorf("unexpected request %s token=%q", r.URL.Path, r.Header.Get("X-Risk-Token"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls = append(calls, body)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"graphRemoved": 3, "problems": []string{}}})
		done <- struct{}{}
	}))
	defer srv.Close()

	s := &RiskService{log: zap.NewNop(), cfg: config.RiskConfig{AdminURL: srv.URL, InternalToken: "tok"}, client: fakeDecisionClient{}}
	ctx := context.Background()

	after := s.PurgeHook(ctx, 10000, 3038, "acc")
	if after == nil {
		t.Fatal("普通删号应当返回收尾")
	}
	after(ctx)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("没有通知风控")
	}
	mu.Lock()
	got := calls[0]
	mu.Unlock()
	if got["appId"].(float64) != 10000 || len(got["userIds"].([]any)) != 1 || got["userIds"].([]any)[0].(float64) != 3038 {
		t.Fatalf("请求体不对：%v", got)
	}

	if s.PurgeHook(withRiskForgetHandled(ctx), 10000, 3039, "acc") != nil {
		t.Fatal("风控关联删除自己通知风控，通用收尾不应再通知")
	}
	var off *RiskService
	if off.PurgeHook(ctx, 10000, 1, "") != nil {
		t.Fatal("没接风控时不该有收尾")
	}
}
