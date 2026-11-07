// Package nativeplugin 内置插件宿主。
//
// 内置插件是随二进制发布的 Go 模块（与用户上传的 expr / wasm 钩子插件不同），
// 宿主负责：登记、按数据库里的启停记录启动、运行期启用 / 停用 / 重启、
// 多实例之间对齐启停状态（定时对账 native_plugins 表），以及进程退出时有序停止。
//
// 插件自己决定在请求链路里怎么挂：例如流量管理插件提供一个常驻的 Gin 中间件，
// 停用时中间件直接放行，因此启停不需要重建路由。
package nativeplugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	plugindomain "aegis/internal/domain/plugin"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// Plugin 内置插件契约。
type Plugin interface {
	Manifest() plugindomain.NativeManifest
	// Start 启动插件。返回错误时插件进入 failed 状态，不影响其他插件与主进程。
	Start(ctx context.Context) error
	// Stop 停止插件，必须在 ctx 截止前返回。
	Stop(ctx context.Context) error
	// Health 运行期健康信息（展示用，可以为 nil）。
	Health() map[string]any
}

// Store 启停记录的持久化。
type Store interface {
	LoadNativePlugins(ctx context.Context) (map[string]plugindomain.NativeRecord, error)
	SaveNativePlugin(ctx context.Context, id string, enabled bool, actor *int64) (plugindomain.NativeRecord, error)
}

// Host 插件宿主。
type Host struct {
	log   *zap.Logger
	store Store

	mu      sync.Mutex
	entries map[string]*entry
	order   []string
}

type entry struct {
	mu       sync.Mutex // 串行化同一插件的启停
	plugin   Plugin
	manifest plugindomain.NativeManifest
	enabled  bool
	state    string
	since    time.Time
	err      string
	rec      *plugindomain.NativeRecord
}

// ErrNotFound 插件不存在。
var ErrNotFound = apperrors.New(40492, http.StatusNotFound, "插件不存在")

// NewHost 构造宿主。
func NewHost(store Store, log *zap.Logger) *Host {
	if log == nil {
		log = zap.NewNop()
	}
	return &Host{log: log, store: store, entries: map[string]*entry{}}
}

// Register 登记插件（装配期调用）。重复 ID 会 panic：那是编码错误。
func (h *Host) Register(p Plugin) {
	m := p.Manifest()
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.entries[m.ID]; ok {
		panic(fmt.Sprintf("native plugin %q registered twice", m.ID))
	}
	h.entries[m.ID] = &entry{plugin: p, manifest: m, enabled: m.DefaultEnabled, state: plugindomain.NativeStateStopped, since: time.Now().UTC()}
	h.order = append(h.order, m.ID)
}

// Boot 读启停记录并启动应启用的插件。单个插件启动失败只记日志。
func (h *Host) Boot(ctx context.Context) {
	records, err := h.store.LoadNativePlugins(ctx)
	if err != nil {
		h.log.Warn("load native plugin records failed, using defaults", zap.Error(err))
		records = nil
	}
	for _, e := range h.list() {
		if rec, ok := records[e.manifest.ID]; ok {
			r := rec
			e.mu.Lock()
			e.enabled, e.rec = rec.Enabled, &r
			e.mu.Unlock()
		}
		if e.enabled {
			h.start(ctx, e)
		}
	}
}

// Run 定时对账启停记录，让在其他实例上的启停也传到本实例。阻塞到 ctx 结束。
func (h *Host) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.reconcile(ctx)
		}
	}
}

func (h *Host) reconcile(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	records, err := h.store.LoadNativePlugins(rctx)
	cancel()
	if err != nil {
		return
	}
	for _, e := range h.list() {
		rec, ok := records[e.manifest.ID]
		if !ok {
			continue
		}
		r := rec
		e.mu.Lock()
		e.rec = &r
		want := rec.Enabled
		running := e.state == plugindomain.NativeStateRunning
		failed := e.state == plugindomain.NativeStateFailed
		e.enabled = want
		e.mu.Unlock()
		switch {
		case want && !running && !failed:
			h.start(ctx, e)
		case !want && (running || failed):
			h.stop(ctx, e)
		}
	}
}

// Shutdown 逆序停止全部插件。
func (h *Host) Shutdown(ctx context.Context) {
	list := h.list()
	for i := len(list) - 1; i >= 0; i-- {
		h.stop(ctx, list[i])
	}
}

func (h *Host) list() []*entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*entry, 0, len(h.order))
	for _, id := range h.order {
		out = append(out, h.entries[id])
	}
	return out
}

func (h *Host) get(id string) (*entry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	return e, nil
}

func (h *Host) start(ctx context.Context, e *entry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == plugindomain.NativeStateRunning {
		return
	}
	e.state, e.since, e.err = plugindomain.NativeStateStarting, time.Now().UTC(), ""
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := safeCall(func() error { return e.plugin.Start(sctx) })
	cancel()
	if err != nil {
		e.state, e.err = plugindomain.NativeStateFailed, err.Error()
		h.log.Error("native plugin start failed", zap.String("plugin", e.manifest.ID), zap.Error(err))
		return
	}
	e.state, e.since = plugindomain.NativeStateRunning, time.Now().UTC()
	h.log.Info("native plugin started", zap.String("plugin", e.manifest.ID), zap.String("version", e.manifest.Version))
}

func (h *Host) stop(ctx context.Context, e *entry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == plugindomain.NativeStateStopped {
		return
	}
	wasFailed := e.state == plugindomain.NativeStateFailed
	e.state = plugindomain.NativeStateStopping
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err := safeCall(func() error { return e.plugin.Stop(sctx) })
	cancel()
	e.state, e.since = plugindomain.NativeStateStopped, time.Now().UTC()
	if err != nil && !wasFailed {
		e.err = err.Error()
		h.log.Warn("native plugin stop returned error", zap.String("plugin", e.manifest.ID), zap.Error(err))
	} else {
		e.err = ""
	}
	h.log.Info("native plugin stopped", zap.String("plugin", e.manifest.ID))
}

// safeCall 插件的 panic 不能带走宿主进程。
func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

// Enable 启用并启动插件（持久化，其他实例在下一次对账时跟进）。
func (h *Host) Enable(ctx context.Context, id string, actor *int64) (plugindomain.NativeView, error) {
	return h.setEnabled(ctx, id, true, actor)
}

// Disable 停用并停止插件。
func (h *Host) Disable(ctx context.Context, id string, actor *int64) (plugindomain.NativeView, error) {
	return h.setEnabled(ctx, id, false, actor)
}

func (h *Host) setEnabled(ctx context.Context, id string, enabled bool, actor *int64) (plugindomain.NativeView, error) {
	e, err := h.get(id)
	if err != nil {
		return plugindomain.NativeView{}, err
	}
	rec, err := h.store.SaveNativePlugin(ctx, id, enabled, actor)
	if err != nil {
		return plugindomain.NativeView{}, err
	}
	e.mu.Lock()
	e.enabled, e.rec = enabled, &rec
	e.mu.Unlock()
	if enabled {
		h.start(ctx, e)
	} else {
		h.stop(ctx, e)
	}
	return h.view(e), nil
}

// Restart 重启插件（仅在已启用时有效）。
func (h *Host) Restart(ctx context.Context, id string) (plugindomain.NativeView, error) {
	e, err := h.get(id)
	if err != nil {
		return plugindomain.NativeView{}, err
	}
	e.mu.Lock()
	enabled := e.enabled
	e.mu.Unlock()
	if !enabled {
		return plugindomain.NativeView{}, apperrors.New(40993, http.StatusConflict, "插件未启用，无法重启")
	}
	h.stop(ctx, e)
	h.start(ctx, e)
	return h.view(e), nil
}

// List 全部插件视图。
func (h *Host) List() []plugindomain.NativeView {
	list := h.list()
	out := make([]plugindomain.NativeView, 0, len(list))
	for _, e := range list {
		out = append(out, h.view(e))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return out
}

// Get 单个插件视图。
func (h *Host) Get(id string) (plugindomain.NativeView, error) {
	e, err := h.get(id)
	if err != nil {
		return plugindomain.NativeView{}, err
	}
	return h.view(e), nil
}

// Running 插件是否在运行。
func (h *Host) Running(id string) bool {
	e, err := h.get(id)
	if err != nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state == plugindomain.NativeStateRunning
}

func (h *Host) view(e *entry) plugindomain.NativeView {
	e.mu.Lock()
	v := plugindomain.NativeView{
		NativeManifest: e.manifest,
		Builtin:        true,
		Enabled:        e.enabled,
		State:          e.state,
		Error:          e.err,
	}
	since := e.since
	v.Since = &since
	if e.rec != nil {
		at := e.rec.UpdatedAt
		v.UpdatedAt, v.UpdatedBy = &at, e.rec.UpdatedBy
	}
	running := e.state == plugindomain.NativeStateRunning
	e.mu.Unlock()
	if running {
		_ = safeCall(func() error { v.Health = e.plugin.Health(); return nil })
	}
	if v.Capabilities == nil {
		v.Capabilities = []string{}
	}
	return v
}

// IsNotFound 判断是否为插件不存在。
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
