package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/egressgraph"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
	apperrors "aegis/pkg/errors"
)

// 出海网关画布的端到端集成测试（真实 Postgres + 真实 mihomo 核心）：
// 当前配置画成画布 → 保存草稿（密钥打码、占位还原、并发保护）→ 应用（核心拉起、网关经核心出站）→
// 改画布再应用 → 回滚 → 另一个进程启动时按已应用的画布运行 → 恢复为环境变量配置。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55441/postgres MIHOMO_BINARY=/path/to/mihomo \
//
//	go test ./internal/service -run TestEgressGraphIntegration
func TestEgressGraphIntegration(t *testing.T) {
	binary := os.Getenv("MIHOMO_BINARY")
	if binary == "" {
		t.Skip("未设置 MIHOMO_BINARY")
	}
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok "+r.URL.Path)
	}))
	defer upstream.Close()

	envCfg := egress.Config{Enabled: true, Endpoints: []egress.EndpointConfig{{Name: "env-direct", Protocol: egress.ProtocolDirect}}}
	gw, err := egress.New(envCfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	core := mihomo.NewCore(mihomo.Options{Home: t.TempDir(), Binary: binary})
	defer core.Close()
	mihomo.SetDefault(core)
	defer mihomo.SetDefault(nil)

	svc := NewEgressService(zap.NewNop(), pg, gw, "integration-master-key", envCfg)
	svc.AttachMihomo(core, true, "")
	if err := svc.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	view, err := svc.GraphView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Origin != "config" || len(view.Graph.Nodes) != 2 {
		t.Fatalf("没有画布时应由当前配置生成：%s %d", view.Origin, len(view.Graph.Nodes))
	}

	raw := func(v any) json.RawMessage { data, _ := json.Marshal(v); return data }
	gateway := egressgraph.DefaultGateway()
	gateway.Health.Enabled = false
	graph := egressgraph.Graph{
		Nodes: []egressgraph.Node{
			{ID: "gw", Type: egressgraph.NodeGateway, Data: raw(gateway)},
			{ID: "r1", Type: egressgraph.NodeRule, Data: raw(egressgraph.RuleData{Name: "via-core", Match: egress.MatchConfig{Profiles: []string{"it.core"}}})},
			{ID: "r2", Type: egressgraph.NodeRule, Data: raw(egressgraph.RuleData{Name: "via-rules", Match: egress.MatchConfig{Profiles: []string{"it.rules"}}})},
			{ID: "ep", Type: egressgraph.NodeEndpoint, Data: raw(egress.EndpointConfig{Name: "socks-lab", Protocol: egress.ProtocolSOCKS5H, Address: "127.0.0.1:1", Password: "topsecret"})},
			{ID: "g", Type: egressgraph.NodeGroup, Data: raw(egressgraph.GroupData{Group: map[string]any{"name": "出口", "type": "select"}, Builtins: []string{"DIRECT"}})},
			{ID: "p", Type: egressgraph.NodeProxy, Data: raw(egressgraph.ProxyData{Proxy: map[string]any{"name": "本机", "type": "direct"}})},
			{ID: "m", Type: egressgraph.NodeRules, Data: raw(egressgraph.RulesData{Rules: []egressgraph.MihomoRule{
				{ID: "1", Type: "DOMAIN-SUFFIX", Payload: "example.com", Target: "REJECT"},
				{ID: "2", Type: "MATCH", Target: "出口"},
			}})},
		},
		Edges: []egressgraph.Edge{
			{ID: "e1", Source: "gw", SourceHandle: egressgraph.PortRules, Target: "r1"},
			{ID: "e2", Source: "r1", SourceHandle: egressgraph.PortNext, Target: "r2"},
			{ID: "e3", Source: "r1", SourceHandle: egressgraph.PortHit, Target: "g"},
			{ID: "e4", Source: "r2", SourceHandle: egressgraph.PortHit, Target: "m"},
			{ID: "e5", Source: "g", SourceHandle: egressgraph.PortMembers, Target: "p"},
		},
	}

	saved, err := svc.SaveGraphDraft(ctx, nil, egressgraph.SaveRequest{Graph: graph, Revision: 0})
	if err != nil {
		t.Fatalf("保存草稿: %v", err)
	}
	text, _ := json.Marshal(saved.Graph)
	if strings.Contains(string(text), "topsecret") || !strings.Contains(string(text), egressgraph.SecretKept) {
		t.Fatalf("读回的画布应打码")
	}
	if _, err := svc.SaveGraphDraft(ctx, nil, egressgraph.SaveRequest{Graph: graph, Revision: 0}); !isConflict(err) {
		t.Fatalf("旧版本号保存应冲突，得到 %v", err)
	}
	record, _ := pg.GetPlatformSetting(ctx, egressGraphKey)
	if strings.Contains(string(record.Value), "topsecret") {
		t.Fatalf("落库的密钥应加密")
	}

	// 用打码后的画布应用：密钥由服务端还原。
	applied, err := svc.ApplyGraph(ctx, nil, egressgraph.ApplyRequest{Graph: saved.Graph, Revision: saved.DraftRevision, Note: "首次"})
	if err != nil {
		t.Fatalf("应用: %v", err)
	}
	if applied.AppliedVersion != 1 || !applied.Active || applied.Core.Status != mihomo.StatusRunning {
		t.Fatalf("应用后状态不对: v%d active=%v core=%s", applied.AppliedVersion, applied.Active, applied.Core.Status)
	}
	for _, endpoint := range gw.Config().Endpoints {
		if endpoint.Name == "socks-lab" && endpoint.Password != "topsecret" {
			t.Fatalf("应用时密钥没有还原")
		}
	}

	get := func(profile, target string) (string, error) {
		client := gw.Client(egress.Profile{Name: profile, Timeout: 10 * time.Second})
		resp, err := client.Get(target)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body), nil
	}
	if body, err := get("it.core", upstream.URL+"/core"); err != nil || body != "ok /core" {
		t.Fatalf("经策略组出站失败: %q %v", body, err)
	}
	if _, err := get("it.rules", "http://www.example.com/"); err == nil {
		t.Fatalf("交给核心分流的 example.com 应被拒绝")
	}
	if body, err := get("it.rules", upstream.URL+"/rules"); err != nil || body != "ok /rules" {
		t.Fatalf("交给核心分流的其余流量应走兜底: %q %v", body, err)
	}
	proxies, err := svc.CoreProxies(ctx)
	if err != nil || proxies.Proxies["出口"].Now == "" {
		t.Fatalf("读不到策略组: %v", err)
	}
	if err := svc.CoreSelect(ctx, "出口", "DIRECT"); err != nil {
		t.Fatalf("切换节点: %v", err)
	}

	// 第二版：加一条拒绝规则。
	second := applied.Graph
	second.Nodes = append(second.Nodes, egressgraph.Node{ID: "r3", Type: egressgraph.NodeRule, Data: raw(egressgraph.RuleData{Name: "block", Action: egress.ActionReject, Match: egress.MatchConfig{Profiles: []string{"it.block"}}})})
	second.Edges = append(second.Edges, egressgraph.Edge{ID: "e6", Source: "r2", SourceHandle: egressgraph.PortNext, Target: "r3"})
	v2, err := svc.ApplyGraph(ctx, nil, egressgraph.ApplyRequest{Graph: second, Revision: applied.DraftRevision})
	if err != nil {
		t.Fatalf("应用第二版: %v", err)
	}
	if _, err := get("it.block", upstream.URL); !errors.Is(err, egress.ErrRejected) {
		t.Fatalf("第二版的拒绝规则没有生效: %v", err)
	}
	history, _ := svc.GraphHistory(ctx)
	if len(history) != 2 || history[0].Version != 2 {
		t.Fatalf("版本记录不对: %+v", history)
	}
	if _, err := svc.RollbackGraph(ctx, nil, 1); err != nil {
		t.Fatalf("回滚: %v", err)
	}
	if _, err := get("it.block", upstream.URL); errors.Is(err, egress.ErrRejected) {
		t.Fatalf("回滚后拒绝规则应消失")
	}
	_ = v2

	// 另一个进程：只读库启动，按已应用的画布运行。
	gw2, _ := egress.New(egress.Config{}, nil)
	defer gw2.Close()
	other := NewEgressService(zap.NewNop(), pg, gw2, "integration-master-key", egress.Config{})
	if err := other.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if cfg := gw2.Config(); cfg.Source != "graph" || len(cfg.Rules) != 2 {
		t.Fatalf("另一个进程没有按画布启动: %s %d", cfg.Source, len(cfg.Rules))
	}

	// 恢复为环境变量配置：画布撤掉、核心停下、草稿保留。
	if _, err := svc.ResetToEnv(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if core.Status().Status != mihomo.StatusDisabled || gw.Config().Source == "graph" {
		t.Fatalf("恢复后核心应停止、网关回到环境变量配置")
	}
	after, _ := svc.GraphView(ctx)
	if after.Origin != "draft" || after.Active {
		t.Fatalf("恢复后草稿应保留且不再生效: %s %v", after.Origin, after.Active)
	}
}

func isConflict(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == 40900
}
