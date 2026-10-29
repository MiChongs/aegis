package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aegis/internal/domain/egressgraph"
	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 出海网关画布：草稿、应用、版本回滚，以及托管的 mihomo 核心。
//
// 画布是网关配置的唯一来源——应用时编译出网关路由表与核心配置，先让核心接受新配置，
// 再热替换网关；任何一步失败都停在旧配置上。
//
// 多进程部署（API 与 Worker 分开、多副本）时，每个进程各有一份网关与核心。
// 应用只发生在收到请求的进程里，其余进程由 syncLoop 发现版本号变化后自行应用。

const (
	egressGraphKey     = "platform.egress.graph"
	egressHistoryKey   = "platform.egress.graph.history"
	egressHistoryLimit = 20
	egressSyncInterval = 20 * time.Second
)

// storedGraphState 落库形态：密钥已加密。
type storedGraphState struct {
	Draft          *egressgraph.Graph `json:"draft,omitempty"`
	DraftRevision  int64              `json:"draftRevision"`
	DraftUpdatedAt *time.Time         `json:"draftUpdatedAt,omitempty"`
	DraftUpdatedBy *int64             `json:"draftUpdatedBy,omitempty"`
	Applied        *egressgraph.Graph `json:"applied,omitempty"`
	AppliedVersion int64              `json:"appliedVersion"`
	AppliedAt      *time.Time         `json:"appliedAt,omitempty"`
	AppliedBy      *int64             `json:"appliedBy,omitempty"`
}

// AttachMihomo 接上进程级 mihomo 核心。owner 为 true 的实例负责启动时载入画布与跨进程同步。
func (s *EgressService) AttachMihomo(core *mihomo.Core, owner bool, mirror string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.core = core
	s.owner = owner
	s.mirror = mirror
	s.mu.Unlock()
}

func (s *EgressService) mihomoCore() (*mihomo.Core, error) {
	s.mu.RLock()
	core := s.core
	s.mu.RUnlock()
	if core == nil {
		return nil, apperrors.New(50303, http.StatusServiceUnavailable, "mihomo 核心未装配")
	}
	return core, nil
}

func (s *EgressService) requireGraph() error {
	if s == nil || s.gw == nil {
		return apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	if s.pg == nil {
		return apperrors.New(50302, http.StatusServiceUnavailable, "出海网关配置存储不可用")
	}
	return nil
}

func (s *EgressService) loadGraphState(ctx context.Context) (storedGraphState, error) {
	var state storedGraphState
	record, err := s.pg.GetPlatformSetting(ctx, egressGraphKey)
	if err != nil {
		return state, err
	}
	if record == nil || len(record.Value) == 0 || strings.TrimSpace(string(record.Value)) == "null" {
		return state, nil
	}
	if err := json.Unmarshal(record.Value, &state); err != nil {
		return state, fmt.Errorf("解析出海网关画布失败: %w", err)
	}
	return state, nil
}

func (s *EgressService) saveGraphState(ctx context.Context, actorID *int64, state storedGraphState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: egressGraphKey, Value: payload, UpdatedBy: actorID})
	return err
}

// sealGraph / openGraph 加解密画布里的密钥。
func (s *EgressService) sealGraph(graph egressgraph.Graph) (egressgraph.Graph, error) {
	return egressgraph.WalkSecrets(graph, func(_ egressgraph.Node, _ string, value string) (string, error) {
		return encryptSecret(s.key, value)
	})
}

func (s *EgressService) openGraph(graph *egressgraph.Graph) egressgraph.Graph {
	if graph == nil {
		return egressgraph.Graph{}
	}
	opened, err := egressgraph.WalkSecrets(*graph, func(_ egressgraph.Node, _ string, value string) (string, error) {
		plaintext, err := decryptSecret(s.key, value)
		if err != nil {
			return value, nil
		}
		return plaintext, nil
	})
	if err != nil {
		return *graph
	}
	return opened
}

func graphHash(graph egressgraph.Graph) string {
	normalized := egressgraph.Graph{Nodes: graph.Nodes, Edges: graph.Edges}
	payload, _ := json.Marshal(normalized)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:8])
}

// semanticHash 只看配置与连线，不看位置：拖动节点不算「与已应用的不同」。
func semanticHash(graph egressgraph.Graph) string {
	type slim struct {
		ID   string          `json:"id"`
		Type string          `json:"type"`
		Data json.RawMessage `json:"data,omitempty"`
	}
	nodes := make([]slim, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes = append(nodes, slim{ID: node.ID, Type: node.Type, Data: node.Data})
	}
	payload, _ := json.Marshal(map[string]any{"nodes": nodes, "edges": graph.Edges})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:8])
}

// GraphView 管理端读取画布：有草稿读草稿，没有就读已应用的，都没有就把当前配置画成画布。
func (s *EgressService) GraphView(ctx context.Context) (egressgraph.View, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.View{}, err
	}
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return egressgraph.View{}, err
	}
	return s.buildView(state), nil
}

func (s *EgressService) buildView(state storedGraphState) egressgraph.View {
	s.mu.RLock()
	mirror, core := s.mirror, s.core
	s.mu.RUnlock()

	view := egressgraph.View{
		DraftRevision:  state.DraftRevision,
		DraftUpdatedAt: state.DraftUpdatedAt,
		DraftUpdatedBy: state.DraftUpdatedBy,
		AppliedVersion: state.AppliedVersion,
		AppliedAt:      state.AppliedAt,
		AppliedBy:      state.AppliedBy,
		Catalog:        egressgraph.BuildCatalog(mirror),
		Runtime:        s.gw.Stats(),
	}
	var graph egressgraph.Graph
	switch {
	case state.Draft != nil:
		graph, view.Origin = s.openGraph(state.Draft), "draft"
	case state.Applied != nil:
		graph, view.Origin = s.openGraph(state.Applied), "applied"
	default:
		graph, view.Origin = egressgraph.FromConfig(s.gw.Config()), "config"
	}
	if state.Applied != nil {
		applied := s.openGraph(state.Applied)
		view.Dirty = semanticHash(applied) != semanticHash(graph)
		view.Active = s.graphActive()
	}
	view.Compile = egressgraph.ViewOf(egressgraph.Compile(graph))
	view.Graph = egressgraph.Mask(graph)
	if core != nil {
		view.Core = core.Status()
	}
	return view
}

func (s *EgressService) graphActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.graphVersion > 0 && s.dbOverride
}

// previousGraph 恢复占位密钥时参照的旧画布：草稿优先，其次已应用的，再次当前配置。
func (s *EgressService) previousGraph(state storedGraphState) egressgraph.Graph {
	switch {
	case state.Draft != nil:
		previous := s.openGraph(state.Draft)
		if state.Applied != nil {
			applied := s.openGraph(state.Applied)
			previous.Nodes = append(previous.Nodes, applied.Nodes...)
		}
		return previous
	case state.Applied != nil:
		return s.openGraph(state.Applied)
	default:
		return egressgraph.FromConfig(s.gw.Config())
	}
}

func issuesError(issues []egressgraph.Issue) error {
	errors := 0
	first := ""
	for _, issue := range issues {
		if issue.Level == "error" {
			if errors == 0 {
				first = issue.Message
			}
			errors++
		}
	}
	if errors == 0 {
		return nil
	}
	if errors == 1 {
		return apperrors.New(40000, http.StatusBadRequest, first)
	}
	return apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("%s（另有 %d 个错误）", first, errors-1))
}

// SaveGraphDraft 保存草稿，不影响线上。
func (s *EgressService) SaveGraphDraft(ctx context.Context, actorID *int64, req egressgraph.SaveRequest) (egressgraph.View, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.View{}, err
	}
	s.graphMu.Lock()
	defer s.graphMu.Unlock()
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return egressgraph.View{}, err
	}
	if req.Revision != state.DraftRevision {
		return egressgraph.View{}, apperrors.New(40900, http.StatusConflict, "画布刚被别人改过，请刷新后再保存")
	}
	graph, issues := egressgraph.Restore(req.Graph, s.previousGraph(state))
	if err := issuesError(issues); err != nil {
		return egressgraph.View{}, err
	}
	sealed, err := s.sealGraph(graph)
	if err != nil {
		return egressgraph.View{}, apperrors.New(50001, http.StatusInternalServerError, "加密画布密钥失败")
	}
	now := time.Now()
	state.Draft = &sealed
	state.DraftRevision++
	state.DraftUpdatedAt = &now
	state.DraftUpdatedBy = actorID
	if err := s.saveGraphState(ctx, actorID, state); err != nil {
		return egressgraph.View{}, err
	}
	return s.buildView(state), nil
}

// ValidateGraph 校验画布并预览编译结果（核心配置里的密钥已打码）。
func (s *EgressService) ValidateGraph(ctx context.Context, graph egressgraph.Graph) (egressgraph.ValidateResult, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.ValidateResult{}, err
	}
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return egressgraph.ValidateResult{}, err
	}
	restored, issues := egressgraph.Restore(graph, s.previousGraph(state))
	result := egressgraph.Compile(restored)
	result.Issues = append(issues, result.Issues...)
	out := egressgraph.ValidateResult{Compile: egressgraph.ViewOf(result), Routes: egressgraph.PreviewOf(result.Config)}

	masked := egressgraph.Compile(egressgraph.Mask(restored))
	if !masked.Profile.Empty() {
		if yaml, err := mihomo.Render(masked.Profile, previewSystem(masked.Targets)); err == nil {
			out.YAML = string(yaml)
		}
	}
	return out, nil
}

func previewSystem(targets []string) mihomo.System {
	return mihomo.System{ControllerAddr: "127.0.0.1:<控制器端口>", Secret: "<随机密钥>", InboundPort: 7890, Password: "<随机口令>", TargetPorts: mihomo.PlaceholderPorts(targets)}
}

// ApplyGraph 应用画布：编译 → 核心接受新配置 → 网关热替换 → 落库并记一个版本。
func (s *EgressService) ApplyGraph(ctx context.Context, actorID *int64, req egressgraph.ApplyRequest) (egressgraph.View, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.View{}, err
	}
	s.graphMu.Lock()
	defer s.graphMu.Unlock()
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return egressgraph.View{}, err
	}
	if req.Revision != state.DraftRevision {
		return egressgraph.View{}, apperrors.New(40900, http.StatusConflict, "画布刚被别人改过，请刷新后再应用")
	}
	graph, issues := egressgraph.Restore(req.Graph, s.previousGraph(state))
	if err := issuesError(issues); err != nil {
		return egressgraph.View{}, err
	}
	result := egressgraph.Compile(graph)
	if err := issuesError(result.Issues); err != nil {
		return egressgraph.View{}, err
	}
	if err := s.activate(ctx, result); err != nil {
		return egressgraph.View{}, err
	}

	sealed, err := s.sealGraph(graph)
	if err != nil {
		return egressgraph.View{}, apperrors.New(50001, http.StatusInternalServerError, "加密画布密钥失败")
	}
	now := time.Now()
	state.Draft = &sealed
	state.DraftRevision++
	state.DraftUpdatedAt = &now
	state.DraftUpdatedBy = actorID
	state.Applied = &sealed
	state.AppliedVersion++
	state.AppliedAt = &now
	state.AppliedBy = actorID
	if err := s.saveGraphState(ctx, actorID, state); err != nil {
		return egressgraph.View{}, err
	}
	s.markGraphApplied(state.AppliedVersion, actorID, now)
	s.appendHistory(ctx, actorID, egressgraph.HistoryEntry{
		Version: state.AppliedVersion, AppliedAt: now, AppliedBy: actorID, Note: strings.TrimSpace(req.Note),
		Summary: result.Summary, Graph: &sealed,
	})
	s.log.Info("出海网关画布已应用",
		zap.Int64("version", state.AppliedVersion),
		zap.Int("rules", len(result.Config.Rules)),
		zap.Int("endpoints", len(result.Config.Endpoints)),
		zap.Bool("mihomo", !result.Profile.Empty()),
	)
	return s.buildView(state), nil
}

// activate 让编译结果生效。核心先行：核心拒绝就不动网关；网关拒绝就把核心退回旧配置。
func (s *EgressService) activate(ctx context.Context, result egressgraph.Result) error {
	if err := s.gw.ValidateConfig(result.Config); err != nil {
		return apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	core, coreErr := s.mihomoCore()
	if !result.Profile.Empty() && coreErr != nil {
		return coreErr
	}
	var previous mihomo.Profile
	var previousTargets []string
	hadPrevious := false
	if core != nil {
		previous, hadPrevious = core.Profile()
		previousTargets = s.currentTargets()
		applyCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		err := core.Apply(applyCtx, result.Profile, result.Targets)
		cancel()
		if err != nil {
			return apperrors.New(40000, http.StatusBadRequest, "mihomo 核心没有接受这份配置: "+err.Error())
		}
	}
	cfg := result.Config
	cfg.Source = "graph"
	if err := s.gw.Reload(cfg); err != nil {
		if core != nil && hadPrevious {
			restoreCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			_ = core.Apply(restoreCtx, previous, previousTargets)
			cancel()
		}
		return apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	s.mu.Lock()
	s.targets = result.Targets
	s.mu.Unlock()
	return nil
}

func (s *EgressService) currentTargets() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.targets...)
}

func (s *EgressService) markGraphApplied(version int64, actorID *int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graphVersion = version
	s.dbOverride = true
	s.updatedBy = actorID
	s.updatedAt = &at
}

func (s *EgressService) loadHistory(ctx context.Context) ([]egressgraph.HistoryEntry, error) {
	record, err := s.pg.GetPlatformSetting(ctx, egressHistoryKey)
	if err != nil || record == nil || len(record.Value) == 0 {
		return nil, err
	}
	var entries []egressgraph.HistoryEntry
	if err := json.Unmarshal(record.Value, &entries); err != nil {
		return nil, nil
	}
	return entries, nil
}

func (s *EgressService) appendHistory(ctx context.Context, actorID *int64, entry egressgraph.HistoryEntry) {
	entries, err := s.loadHistory(ctx)
	if err != nil {
		s.log.Warn("读取出海网关版本记录失败", zap.Error(err))
	}
	entries = append([]egressgraph.HistoryEntry{entry}, entries...)
	if len(entries) > egressHistoryLimit {
		entries = entries[:egressHistoryLimit]
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return
	}
	if _, err := s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: egressHistoryKey, Value: payload, UpdatedBy: actorID}); err != nil {
		s.log.Warn("写入出海网关版本记录失败", zap.Error(err))
	}
}

// GraphHistory 最近的应用记录（不含画布本体）。
func (s *EgressService) GraphHistory(ctx context.Context) ([]egressgraph.HistoryEntry, error) {
	if err := s.requireGraph(); err != nil {
		return nil, err
	}
	entries, err := s.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]egressgraph.HistoryEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Graph = nil
		out = append(out, entry)
	}
	return out, nil
}

// GraphVersion 取某个历史版本的画布（密钥打码），用于载入编辑器查看或改后重新应用。
func (s *EgressService) GraphVersion(ctx context.Context, version int64) (egressgraph.Graph, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.Graph{}, err
	}
	entries, err := s.loadHistory(ctx)
	if err != nil {
		return egressgraph.Graph{}, err
	}
	for _, entry := range entries {
		if entry.Version == version && entry.Graph != nil {
			return egressgraph.Mask(s.openGraph(entry.Graph)), nil
		}
	}
	return egressgraph.Graph{}, apperrors.New(40400, http.StatusNotFound, "这个版本已不在记录里")
}

// RollbackGraph 把某个历史版本重新应用一次（记为新版本）。
func (s *EgressService) RollbackGraph(ctx context.Context, actorID *int64, version int64) (egressgraph.View, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.View{}, err
	}
	entries, err := s.loadHistory(ctx)
	if err != nil {
		return egressgraph.View{}, err
	}
	var target *egressgraph.Graph
	for _, entry := range entries {
		if entry.Version == version {
			target = entry.Graph
			break
		}
	}
	if target == nil {
		return egressgraph.View{}, apperrors.New(40400, http.StatusNotFound, "这个版本已不在记录里")
	}
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return egressgraph.View{}, err
	}
	// 历史版本里的密钥是加密的明文副本，不经占位还原。
	return s.ApplyGraph(ctx, actorID, egressgraph.ApplyRequest{
		Graph: s.openGraph(target), Revision: state.DraftRevision, Note: fmt.Sprintf("回滚到第 %d 版", version),
	})
}

// DiscardGraphDraft 丢弃草稿，回到已应用的画布。
func (s *EgressService) DiscardGraphDraft(ctx context.Context, actorID *int64) (egressgraph.View, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.View{}, err
	}
	s.graphMu.Lock()
	defer s.graphMu.Unlock()
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return egressgraph.View{}, err
	}
	state.Draft = state.Applied
	state.DraftRevision++
	now := time.Now()
	state.DraftUpdatedAt = &now
	state.DraftUpdatedBy = actorID
	if err := s.saveGraphState(ctx, actorID, state); err != nil {
		return egressgraph.View{}, err
	}
	return s.buildView(state), nil
}

// ImportGraph 把 mihomo / Clash 配置或分享链接并进画布，返回新画布（还没保存）。
func (s *EgressService) ImportGraph(ctx context.Context, req egressgraph.ImportRequest) (egressgraph.ImportResult, error) {
	if err := s.requireGraph(); err != nil {
		return egressgraph.ImportResult{}, err
	}
	content := req.Content
	if strings.TrimSpace(req.URL) != "" {
		fetched, err := fetchSubscription(ctx, req.URL)
		if err != nil {
			return egressgraph.ImportResult{}, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		content = fetched
	}
	imported, err := mihomo.Import([]byte(content))
	if err != nil {
		return egressgraph.ImportResult{}, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if req.Options.Mode == "" {
		req.Options.Mode = egressgraph.ImportMerge
	}
	merged, notes := egressgraph.Import(req.Graph, imported.Profile, req.Options)
	notes = append(imported.Warnings, notes...)
	return egressgraph.ImportResult{
		Graph:  merged,
		Format: imported.Format,
		Notes:  notes,
		Added: egressgraph.Summary{
			Proxies: len(imported.Profile.Proxies), Groups: len(imported.Profile.ProxyGroups),
			Providers: len(imported.Profile.ProxyProviders), MihomoRules: len(imported.Profile.Rules),
			RuleProviders: len(imported.Profile.RuleProviders),
		},
		Compile: egressgraph.ViewOf(egressgraph.Compile(merged)),
	}, nil
}

// fetchSubscription 下载订阅内容。地址由超级管理员填写，仍然拒绝内网目标；
// User-Agent 带 clash.meta，多数机场据此直接下发 Clash 格式。
func fetchSubscription(ctx context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("订阅地址无效")
	}
	client := egress.NewClient(egress.Profile{Name: "egress.subscription", Timeout: 30 * time.Second, BlockPrivateTargets: true})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "clash.meta/aegis")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("下载订阅失败: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("订阅地址返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return "", fmt.Errorf("读取订阅失败: %v", err)
	}
	return string(body), nil
}

// ExportGraph 导出编译出的核心配置。includeSecrets 为 false 时密钥打码。
func (s *EgressService) ExportGraph(ctx context.Context, req egressgraph.ExportRequest) (string, error) {
	if err := s.requireGraph(); err != nil {
		return "", err
	}
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return "", err
	}
	graph, _ := egressgraph.Restore(req.Graph, s.previousGraph(state))
	if !req.IncludeSecrets {
		graph = egressgraph.Mask(graph)
	}
	result := egressgraph.Compile(graph)
	if result.Profile.Empty() {
		return "", apperrors.New(40000, http.StatusBadRequest, "画布上没有 mihomo 节点、订阅或策略组")
	}
	yaml, err := mihomo.Render(result.Profile, previewSystem(result.Targets))
	if err != nil {
		return "", apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return string(yaml), nil
}

// initializeGraph 启动时载入已应用的画布。网关立刻按画布路由；核心在后台拉起
// （可能要先下载），这期间 mihomo 线路拨号失败、健康检查会把流量切到其它线路。
func (s *EgressService) initializeGraph(ctx context.Context) (bool, error) {
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return false, err
	}
	if state.Applied == nil {
		return false, nil
	}
	if err := s.applyStoredGraph(state, false); err != nil {
		s.log.Error("载入出海网关画布失败，沿用 .env 配置", zap.Error(err))
		return false, nil
	}
	return true, nil
}

// applyStoredGraph 应用库里的已应用画布。wait 为 false 时核心在后台拉起。
func (s *EgressService) applyStoredGraph(state storedGraphState, wait bool) error {
	graph := s.openGraph(state.Applied)
	result := egressgraph.Compile(graph)
	if err := issuesError(result.Issues); err != nil {
		return err
	}
	cfg := result.Config
	cfg.Source = "graph"
	if err := s.gw.Reload(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.targets = result.Targets
	core := s.core
	s.mu.Unlock()
	at := time.Now()
	if state.AppliedAt != nil {
		at = *state.AppliedAt
	}
	s.markGraphApplied(state.AppliedVersion, state.AppliedBy, at)
	if core == nil {
		return nil
	}
	run := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		if err := core.Apply(ctx, result.Profile, result.Targets); err != nil {
			s.log.Error("mihomo 核心载入画布配置失败", zap.Error(err))
			return err
		}
		return nil
	}
	if wait {
		return run()
	}
	go func() { _ = run() }()
	return nil
}

// StartSync 跨进程同步：其它进程应用了新版本时，本进程跟着应用。只有 owner 实例运行。
func (s *EgressService) StartSync(ctx context.Context) {
	if s == nil || s.gw == nil || s.pg == nil {
		return
	}
	s.mu.RLock()
	owner := s.owner
	s.mu.RUnlock()
	if !owner {
		return
	}
	go func() {
		ticker := time.NewTicker(egressSyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.syncOnce(ctx)
			}
		}
	}()
}

func (s *EgressService) syncOnce(ctx context.Context) {
	if !s.graphMu.TryLock() {
		return
	}
	defer s.graphMu.Unlock()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	state, err := s.loadGraphState(readCtx)
	cancel()
	if err != nil {
		return
	}
	s.mu.RLock()
	local := s.graphVersion
	s.mu.RUnlock()
	switch {
	case state.Applied != nil && state.AppliedVersion != local:
		s.log.Info("发现出海网关画布新版本，跟随应用", zap.Int64("version", state.AppliedVersion), zap.Int64("local", local))
		if err := s.applyStoredGraph(state, true); err != nil {
			s.log.Warn("跟随应用出海网关画布失败", zap.Error(err))
		}
	case state.Applied == nil && local > 0:
		// 其它进程把画布撤掉了（恢复为环境变量配置）。
		s.mu.Lock()
		s.graphVersion = 0
		s.dbOverride = false
		baseline := s.envConfig.Clone()
		core := s.core
		s.mu.Unlock()
		_ = s.gw.Reload(baseline)
		if core != nil {
			_ = core.Apply(ctx, mihomo.Profile{}, nil)
		}
	}
}

// clearGraph 撤掉已应用的画布（恢复为环境变量配置时调用），草稿保留。
func (s *EgressService) clearGraph(ctx context.Context, actorID *int64) error {
	state, err := s.loadGraphState(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.graphVersion = 0
	core := s.core
	s.mu.Unlock()
	if core != nil {
		_ = core.Apply(ctx, mihomo.Profile{}, nil)
	}
	if state.Applied == nil {
		return nil
	}
	if state.Draft == nil {
		state.Draft = state.Applied
	}
	state.Applied = nil
	return s.saveGraphState(ctx, actorID, state)
}

// --- 核心运行态 ---

// CoreStatusView 核心状态 + 最近的流量采样。
type CoreStatusView struct {
	mihomo.Status
	Traffic []mihomo.TrafficSample `json:"traffic"`
}

func (s *EgressService) CoreStatus() (CoreStatusView, error) {
	core, err := s.mihomoCore()
	if err != nil {
		return CoreStatusView{}, err
	}
	return CoreStatusView{Status: core.Status(), Traffic: core.Traffic()}, nil
}

// InstallCore 安装（或升级）核心；核心本该在跑但没在跑时顺带拉起。
func (s *EgressService) InstallCore(ctx context.Context, version string) (mihomo.InstallResult, error) {
	core, err := s.mihomoCore()
	if err != nil {
		return mihomo.InstallResult{}, err
	}
	result, err := core.Install(ctx, version)
	if err != nil {
		return result, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if _, has := core.Profile(); has {
		if err := core.Restart(ctx); err != nil {
			s.log.Warn("安装后重启 mihomo 核心失败", zap.Error(err))
		}
	} else if state, err := s.loadGraphState(ctx); err == nil && state.Applied != nil {
		_ = s.applyStoredGraph(state, true)
	}
	return result, nil
}

func (s *EgressService) RestartCore(ctx context.Context) error {
	core, err := s.mihomoCore()
	if err != nil {
		return err
	}
	if _, has := core.Profile(); !has {
		state, err := s.loadGraphState(ctx)
		if err == nil && state.Applied != nil {
			if err := s.applyStoredGraph(state, true); err != nil {
				return apperrors.New(40000, http.StatusBadRequest, err.Error())
			}
			return nil
		}
	}
	if err := core.Restart(ctx); err != nil {
		return apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return nil
}

func (s *EgressService) StopCore() error {
	core, err := s.mihomoCore()
	if err != nil {
		return err
	}
	core.Stop()
	return nil
}

func (s *EgressService) coreClient() (*mihomo.Client, error) {
	core, err := s.mihomoCore()
	if err != nil {
		return nil, err
	}
	client, err := core.Client()
	if err != nil {
		return nil, apperrors.New(40900, http.StatusConflict, err.Error())
	}
	return client, nil
}

func coreError(err error) error {
	if err == nil {
		return nil
	}
	return apperrors.New(40000, http.StatusBadRequest, err.Error())
}

// CoreProxiesView 节点、策略组与订阅、规则集的运行态。
type CoreProxiesView struct {
	Proxies       map[string]mihomo.ProxyInfo        `json:"proxies"`
	Providers     map[string]mihomo.ProviderInfo     `json:"providers"`
	RuleProviders map[string]mihomo.RuleProviderInfo `json:"ruleProviders"`
}

func (s *EgressService) CoreProxies(ctx context.Context) (CoreProxiesView, error) {
	client, err := s.coreClient()
	if err != nil {
		return CoreProxiesView{}, err
	}
	proxies, err := client.Proxies(ctx)
	if err != nil {
		return CoreProxiesView{}, coreError(err)
	}
	providers, _ := client.Providers(ctx)
	rules, _ := client.RuleProviders(ctx)
	return CoreProxiesView{Proxies: proxies, Providers: providers, RuleProviders: rules}, nil
}

// CoreDelayRequest 延迟测试。Group 为 true 时测组内全部节点。
type CoreDelayRequest struct {
	Name      string `json:"name" binding:"required"`
	Group     bool   `json:"group"`
	URL       string `json:"url"`
	TimeoutMs int    `json:"timeoutMs"`
}

func (s *EgressService) CoreDelay(ctx context.Context, req CoreDelayRequest) (map[string]int, error) {
	client, err := s.coreClient()
	if err != nil {
		return nil, err
	}
	testURL := strings.TrimSpace(req.URL)
	if testURL == "" {
		testURL = mihomo.DefaultTestURL
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 5 * time.Second
	}
	if req.Group {
		out, err := client.GroupDelay(ctx, req.Name, testURL, timeout)
		return out, coreError(err)
	}
	delay, err := client.Delay(ctx, req.Name, testURL, timeout)
	if err != nil {
		return map[string]int{req.Name: 0}, nil
	}
	return map[string]int{req.Name: delay}, nil
}

func (s *EgressService) CoreSelect(ctx context.Context, group, name string) error {
	client, err := s.coreClient()
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return coreError(client.Unfix(ctx, group))
	}
	return coreError(client.Select(ctx, group, name))
}

func (s *EgressService) CoreConnections(ctx context.Context) (mihomo.Connections, error) {
	client, err := s.coreClient()
	if err != nil {
		return mihomo.Connections{}, err
	}
	out, err := client.Connections(ctx)
	return out, coreError(err)
}

func (s *EgressService) CoreCloseConnection(ctx context.Context, id string) error {
	client, err := s.coreClient()
	if err != nil {
		return err
	}
	return coreError(client.CloseConnection(ctx, id))
}

// CoreProviderAction 更新订阅 / 规则集，或对订阅做一次健康检查。
func (s *EgressService) CoreProviderAction(ctx context.Context, kind, name, action string) error {
	client, err := s.coreClient()
	if err != nil {
		return err
	}
	switch {
	case kind == "rule" && action == "update":
		return coreError(client.UpdateRuleProvider(ctx, name))
	case kind == "proxy" && action == "healthcheck":
		return coreError(client.HealthcheckProvider(ctx, name))
	case kind == "proxy":
		return coreError(client.UpdateProvider(ctx, name))
	default:
		return apperrors.New(40000, http.StatusBadRequest, "不支持的操作")
	}
}

func (s *EgressService) CoreRules(ctx context.Context) ([]mihomo.RuleInfo, error) {
	client, err := s.coreClient()
	if err != nil {
		return nil, err
	}
	out, err := client.Rules(ctx)
	return out, coreError(err)
}

func (s *EgressService) CoreLogs(after uint64, limit int) ([]mihomo.LogLine, error) {
	core, err := s.mihomoCore()
	if err != nil {
		return nil, err
	}
	return core.Logs(after, limit), nil
}

func (s *EgressService) CoreDNSQuery(ctx context.Context, name, qtype string) (map[string]any, error) {
	client, err := s.coreClient()
	if err != nil {
		return nil, err
	}
	out, err := client.DNSQuery(ctx, name, qtype)
	return out, coreError(err)
}

// CoreMaintenance 维护操作：geo 重新下载 Geo 数据库，flush-dns 清空 DNS 缓存。
func (s *EgressService) CoreMaintenance(ctx context.Context, action string) error {
	client, err := s.coreClient()
	if err != nil {
		return err
	}
	switch action {
	case "geo":
		return coreError(client.UpdateGeo(ctx))
	case "flush-dns":
		return coreError(client.FlushDNSCache(ctx))
	default:
		return apperrors.New(40000, http.StatusBadRequest, "不支持的操作")
	}
}
