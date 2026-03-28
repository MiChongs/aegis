package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	plugindomain "aegis/internal/domain/plugin"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"go.uber.org/zap"
)

// WASMRuntime Wazero WASM 沙箱运行时
type WASMRuntime struct {
	log       *zap.Logger
	runtime   wazero.Runtime
	modulesMu sync.RWMutex
	modules   map[string]api.Module
}

// NewWASMRuntime 创建 WASM 运行时
func NewWASMRuntime(log *zap.Logger) *WASMRuntime {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithMemoryLimitPages(256)) // 16MB 内存上限

	// 注册 WASI 支持（标准 I/O）
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	return &WASMRuntime{
		log:     log,
		runtime: rt,
		modules: make(map[string]api.Module),
	}
}

// LoadModule 编译并实例化 WASM 模块
func (w *WASMRuntime) LoadModule(ctx context.Context, pluginName string, wasmBytes []byte) error {
	w.modulesMu.Lock()
	defer w.modulesMu.Unlock()

	// 卸载旧模块
	if old, ok := w.modules[pluginName]; ok {
		_ = old.Close(ctx)
		delete(w.modules, pluginName)
	}

	compiled, err := w.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("wasm compile: %w", err)
	}

	mod, err := w.runtime.InstantiateModule(ctx, compiled,
		wazero.NewModuleConfig().
			WithName(pluginName).
			WithStartFunctions()) // 不自动调用 _start
	if err != nil {
		return fmt.Errorf("wasm instantiate: %w", err)
	}

	w.modules[pluginName] = mod
	w.log.Info("WASM 模块已加载", zap.String("plugin", pluginName))
	return nil
}

// UnloadModule 卸载模块
func (w *WASMRuntime) UnloadModule(pluginName string) {
	w.modulesMu.Lock()
	defer w.modulesMu.Unlock()
	if mod, ok := w.modules[pluginName]; ok {
		_ = mod.Close(context.Background())
		delete(w.modules, pluginName)
		w.log.Info("WASM 模块已卸载", zap.String("plugin", pluginName))
	}
}

// Execute 调用 WASM 模块的 handle_hook 导出函数
func (w *WASMRuntime) Execute(ctx context.Context, pluginName string, payload []byte) (*plugindomain.HookResult, error) {
	w.modulesMu.RLock()
	mod, ok := w.modules[pluginName]
	w.modulesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("wasm module not found: %s", pluginName)
	}

	// 超时控制
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	// 查找导出函数
	handleHook := mod.ExportedFunction("handle_hook")
	if handleHook == nil {
		return nil, fmt.Errorf("wasm module %s: missing export 'handle_hook'", pluginName)
	}

	// 写入共享内存
	malloc := mod.ExportedFunction("malloc")
	free := mod.ExportedFunction("free")
	if malloc == nil || free == nil {
		return nil, fmt.Errorf("wasm module %s: missing memory management exports", pluginName)
	}

	// 分配输入内存
	inputLen := uint64(len(payload))
	results, err := malloc.Call(ctx, inputLen)
	if err != nil {
		return nil, fmt.Errorf("wasm malloc: %w", err)
	}
	inputPtr := results[0]
	defer func() { _, _ = free.Call(ctx, inputPtr) }()

	// 写入 payload
	if !mod.Memory().Write(uint32(inputPtr), payload) {
		return nil, fmt.Errorf("wasm memory write failed")
	}

	// 调用 handle_hook(ptr, len) -> ptr
	callResults, err := handleHook.Call(ctx, inputPtr, inputLen)
	if err != nil {
		return nil, fmt.Errorf("wasm handle_hook: %w", err)
	}

	// 读取输出（返回值为指向 JSON 结果的指针，约定前 4 字节为长度）
	if len(callResults) == 0 {
		return &plugindomain.HookResult{Allow: true}, nil
	}

	outPtr := uint32(callResults[0])
	if outPtr == 0 {
		return &plugindomain.HookResult{Allow: true}, nil
	}

	// 读取长度前缀
	lenBytes, ok := mod.Memory().Read(outPtr, 4)
	if !ok {
		return &plugindomain.HookResult{Allow: true}, nil
	}
	outLen := uint32(lenBytes[0]) | uint32(lenBytes[1])<<8 | uint32(lenBytes[2])<<16 | uint32(lenBytes[3])<<24

	if outLen == 0 || outLen > 1024*1024 { // 最大 1MB
		return &plugindomain.HookResult{Allow: true}, nil
	}

	outBytes, ok := mod.Memory().Read(outPtr+4, outLen)
	if !ok {
		return &plugindomain.HookResult{Allow: true}, nil
	}

	var result plugindomain.HookResult
	if err := json.Unmarshal(outBytes, &result); err != nil {
		return &plugindomain.HookResult{Allow: true}, nil
	}
	return &result, nil
}

// Close 关闭运行时
func (w *WASMRuntime) Close() {
	w.modulesMu.Lock()
	defer w.modulesMu.Unlock()
	for name, mod := range w.modules {
		_ = mod.Close(context.Background())
		delete(w.modules, name)
	}
	_ = w.runtime.Close(context.Background())
}
