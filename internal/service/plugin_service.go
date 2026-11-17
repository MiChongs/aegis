package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"aegis/internal/cluster"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// PluginService 插件管理核心服务
type PluginService struct {
	log  *zap.Logger
	pg   *pgrepo.Repository
	expr *ExprEngine
	wasm *WASMRuntime

	// 内存缓存：hookName → 按 priority 排序的插件列表
	cacheMu   sync.RWMutex
	hookCache map[string][]plugindomain.Plugin

	// observers 进程内的钩子观察者（物品发放的事件触发）：每次钩子执行都通知，与有没有启用插件无关。
	// 观察者自己负责不阻塞。只在启动装配时注册，之后只读。
	observers []HookObserver

	// cluster 插件增删改、启停后通知其他实例重建钩子缓存与表达式编译缓存
	cluster clusterLink
}

// SetCluster 接上集群：订阅其他实例的插件变更广播，收到后按数据库重建本地缓存。
// 重建走 syncFromDB，它不写库、不广播，不会形成回环。
func (s *PluginService) SetCluster(c *cluster.Cluster) {
	if c == nil {
		return
	}
	s.cluster.set(c)
	c.Subscribe(cluster.TopicPluginHooks, func([]byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.syncFromDB(ctx)
	})
}

// changed 本地缓存已经刷新之后，通知其他实例跟上。
func (s *PluginService) changed(op string, id int64) {
	s.cluster.publish(s.log, cluster.TopicPluginHooks, clusterReloadNotice{Op: op, ID: id})
}

// syncFromDB 其他实例改了插件：重新编译全部启用中的表达式插件，再重建钩子缓存。
//
// 不按消息里的插件 ID 增量处理：改名、改脚本、启停交织时增量很容易漏一步，
// 启用中的插件通常只有几个到几十个，全量重编一遍的代价可以忽略。
// 编译失败只记日志、不改库里的状态 —— 发起变更的那个实例已经按它的结果写过状态了，
// 这边再写一遍只会让多个实例互相覆盖。
func (s *PluginService) syncFromDB(ctx context.Context) {
	plugins, err := s.pg.ListEnabledPlugins(ctx)
	if err != nil {
		s.log.Warn("按集群广播刷新插件缓存失败", zap.Error(err))
		return
	}
	// 先编译新的再换缓存：Compile 原地覆盖编译产物，不先卸载，免得重建期间正在执行的钩子扑空。
	enabled := make(map[string]struct{}, len(plugins))
	for i := range plugins {
		p := &plugins[i]
		enabled[p.Name] = struct{}{}
		if err := s.loadPlugin(ctx, p); err != nil {
			s.log.Warn("按集群广播重新编译插件失败", zap.String("plugin", p.Name), zap.Error(err))
		}
	}
	previous := s.cachedPluginNames()
	s.rebuildCache(plugins)
	// 已停用 / 已删除的插件：缓存里已经没有它了，再把编译产物清掉
	for name := range previous {
		if _, ok := enabled[name]; !ok {
			s.unloadPlugin(name)
		}
	}
}

// cachedPluginNames 当前钩子缓存里出现过的插件名（用于在重建前把已停用 / 已删除的编译产物一并清掉）。
func (s *PluginService) cachedPluginNames() map[string]struct{} {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	names := map[string]struct{}{}
	for _, items := range s.hookCache {
		for _, p := range items {
			names[p.Name] = struct{}{}
		}
	}
	return names
}

// HookObserver 进程内的钩子观察者。
type HookObserver func(hookName string, data map[string]any, meta plugindomain.HookMetadata)

// ObserveHooks 注册钩子观察者（启动装配时调用）。
func (s *PluginService) ObserveHooks(fn HookObserver) {
	if fn != nil {
		s.observers = append(s.observers, fn)
	}
}

// NewPluginService 创建插件服务
func NewPluginService(log *zap.Logger, pg *pgrepo.Repository) *PluginService {
	return &PluginService{
		log:       log,
		pg:        pg,
		expr:      NewExprEngine(log),
		wasm:      NewWASMRuntime(log),
		hookCache: make(map[string][]plugindomain.Plugin),
	}
}

// Initialize 启动时加载所有 enabled 插件
func (s *PluginService) Initialize(ctx context.Context) error {
	plugins, err := s.pg.ListEnabledPlugins(ctx)
	if err != nil {
		return err
	}
	for i := range plugins {
		p := &plugins[i]
		if err := s.loadPlugin(ctx, p); err != nil {
			s.log.Warn("插件加载失败", zap.String("plugin", p.Name), zap.Error(err))
			_ = s.pg.UpdatePluginStatus(ctx, p.ID, "error", err.Error())
		}
	}
	s.rebuildCache(plugins)
	s.log.Info("插件系统初始化完成", zap.Int("loaded", len(plugins)))

	// 触发系统启动钩子
	go s.ExecuteHook(context.Background(), HookSystemStartup, map[string]any{
		"pluginsLoaded": len(plugins),
	}, plugindomain.HookMetadata{})

	return nil
}

// Close 关闭运行时
func (s *PluginService) Close() {
	s.wasm.Close()
}

// ── CRUD ──

func (s *PluginService) CreatePlugin(ctx context.Context, input plugindomain.CreatePluginInput, createdBy *int64) (*plugindomain.Plugin, error) {
	// 验证 Expr 脚本可编译
	if input.Type == "expr" && input.ExprScript != "" {
		if err := s.expr.Compile(input.Name, input.ExprScript); err != nil {
			return nil, err
		}
		s.expr.Invalidate(input.Name)
	}
	return s.pg.CreatePlugin(ctx, input, createdBy)
}

func (s *PluginService) GetPlugin(ctx context.Context, id int64) (*plugindomain.Plugin, error) {
	return s.pg.GetPlugin(ctx, id)
}

func (s *PluginService) ListPlugins(ctx context.Context, q plugindomain.PluginListQuery) (*plugindomain.PluginListResult, error) {
	return s.pg.ListPlugins(ctx, q)
}

func (s *PluginService) UpdatePlugin(ctx context.Context, id int64, input plugindomain.UpdatePluginInput) (*plugindomain.Plugin, error) {
	p, err := s.pg.UpdatePlugin(ctx, id, input)
	if err != nil {
		return nil, err
	}
	// 如果已启用，重新编译
	if p.Status == "enabled" {
		if loadErr := s.loadPlugin(ctx, p); loadErr != nil {
			s.log.Warn("插件重新加载失败", zap.String("plugin", p.Name), zap.Error(loadErr))
			_ = s.pg.UpdatePluginStatus(ctx, id, "error", loadErr.Error())
		}
		s.refreshCache(ctx)
		s.changed("update", id)
	}
	return p, nil
}

func (s *PluginService) DeletePlugin(ctx context.Context, id int64) error {
	p, err := s.pg.GetPlugin(ctx, id)
	if err != nil {
		return err
	}
	if p != nil {
		s.unloadPlugin(p.Name)
	}
	if err := s.pg.DeletePlugin(ctx, id); err != nil {
		return err
	}
	s.refreshCache(ctx)
	s.changed("delete", id)
	return nil
}

// ── 生命周期 ──

func (s *PluginService) EnablePlugin(ctx context.Context, id int64) error {
	p, err := s.pg.GetPlugin(ctx, id)
	if err != nil || p == nil {
		return err
	}
	if err := s.loadPlugin(ctx, p); err != nil {
		_ = s.pg.UpdatePluginStatus(ctx, id, "error", err.Error())
		return err
	}
	if err := s.pg.UpdatePluginStatus(ctx, id, "enabled", ""); err != nil {
		return err
	}
	s.refreshCache(ctx)
	s.changed("enable", id)
	return nil
}

func (s *PluginService) DisablePlugin(ctx context.Context, id int64) error {
	p, err := s.pg.GetPlugin(ctx, id)
	if err != nil || p == nil {
		return err
	}
	s.unloadPlugin(p.Name)
	if err := s.pg.UpdatePluginStatus(ctx, id, "disabled", ""); err != nil {
		return err
	}
	s.refreshCache(ctx)
	s.changed("disable", id)
	return nil
}

// ── 钩子执行（核心） ──

// ExecuteHook 执行指定钩子上的所有插件
func (s *PluginService) ExecuteHook(ctx context.Context, hookName string, data map[string]any, meta plugindomain.HookMetadata) *plugindomain.HookResult {
	for _, observe := range s.observers {
		observe(hookName, data, meta)
	}
	s.cacheMu.RLock()
	plugins := s.hookCache[hookName]
	s.cacheMu.RUnlock()

	if len(plugins) == 0 {
		return &plugindomain.HookResult{Allow: true}
	}

	s.log.Debug("执行钩子", zap.String("hook", hookName), zap.Int("plugins", len(plugins)))

	merged := &plugindomain.HookResult{Allow: true}
	for _, p := range plugins {
		start := time.Now()
		var result *plugindomain.HookResult
		var execErr error

		switch p.Type {
		case "expr":
			env := make(map[string]any)
			for k, v := range data {
				env[k] = v
			}
			env["hookName"] = hookName
			env["ip"] = meta.IP
			env["userAgent"] = meta.UserAgent
			result, execErr = s.expr.Execute(ctx, p.Name, env)
		case "wasm":
			payload := plugindomain.HookPayload{
				HookName: hookName, Phase: "before", Data: data, Metadata: meta,
			}
			payloadBytes, _ := encodeJSON(payload)
			result, execErr = s.wasm.Execute(ctx, p.Name, payloadBytes)
		}

		duration := time.Since(start)
		status := "success"
		errMsg := ""
		if execErr != nil {
			status = "error"
			errMsg = execErr.Error()
			s.log.Warn("插件执行失败", zap.String("plugin", p.Name), zap.String("hook", hookName), zap.Error(execErr))
		}

		// 异步记录执行日志
		go func(p plugindomain.Plugin) {
			_ = s.pg.InsertHookExecution(context.Background(), plugindomain.HookExecution{
				PluginID: p.ID, PluginName: p.Name, HookName: hookName,
				Phase: "before", DurationMs: float64(duration.Microseconds()) / 1000,
				Status: status, Error: errMsg, CreatedAt: time.Now(),
			})
		}(p)

		if result != nil && !result.Allow {
			merged.Allow = false
			merged.Message = result.Message
			return merged
		}
		if result != nil && result.Data != nil {
			merged.Data = result.Data
		}
	}
	return merged
}

// ── 注册表 ──

func (s *PluginService) GetHookRegistry(ctx context.Context) (*plugindomain.PluginRegistryView, error) {
	hooks := GetAllHookDefinitions()
	plugins, err := s.pg.ListEnabledPlugins(ctx)
	if err != nil {
		return nil, err
	}

	bindings := make(map[string][]plugindomain.PluginHookSummary)
	for _, p := range plugins {
		for _, h := range p.Hooks {
			bindings[h.HookName] = append(bindings[h.HookName], plugindomain.PluginHookSummary{
				PluginID: p.ID, PluginName: p.Name, DisplayName: p.DisplayName,
				Phase: h.Phase, Priority: h.Priority, Type: p.Type, Status: p.Status,
			})
		}
	}
	return &plugindomain.PluginRegistryView{Hooks: hooks, Bindings: bindings}, nil
}

// ── 执行日志 ──

func (s *PluginService) ListHookExecutions(ctx context.Context, q plugindomain.HookExecutionListQuery) (*plugindomain.HookExecutionListResult, error) {
	return s.pg.ListHookExecutions(ctx, q)
}

// ── 内部方法 ──

func (s *PluginService) loadPlugin(_ context.Context, p *plugindomain.Plugin) error {
	switch p.Type {
	case "expr":
		return s.expr.Compile(p.Name, p.ExprScript)
	case "wasm":
		// WASM 模块需要先从存储获取字节，这里先跳过（通过 upload 接口单独加载）
		return nil
	}
	return nil
}

func (s *PluginService) unloadPlugin(name string) {
	s.expr.Invalidate(name)
	s.wasm.UnloadModule(name)
}

func (s *PluginService) refreshCache(ctx context.Context) {
	plugins, err := s.pg.ListEnabledPlugins(ctx)
	if err != nil {
		s.log.Error("刷新插件缓存失败", zap.Error(err))
		return
	}
	s.rebuildCache(plugins)
}

func (s *PluginService) rebuildCache(plugins []plugindomain.Plugin) {
	cache := make(map[string][]plugindomain.Plugin)
	for _, p := range plugins {
		for _, h := range p.Hooks {
			cache[h.HookName] = append(cache[h.HookName], p)
		}
	}
	s.cacheMu.Lock()
	s.hookCache = cache
	s.cacheMu.Unlock()

	// 日志：当前缓存状态
	totalBindings := 0
	for _, v := range cache {
		totalBindings += len(v)
	}
	s.log.Info("插件钩子缓存已重建", zap.Int("hooks", len(cache)), zap.Int("bindings", totalBindings), zap.Int("plugins", len(plugins)))
}

func encodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
