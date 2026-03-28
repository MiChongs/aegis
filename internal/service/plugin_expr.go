package service

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	plugindomain "aegis/internal/domain/plugin"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"go.uber.org/zap"
)

// ExprEngine Expr 表达式引擎（预编译 + 缓存）
type ExprEngine struct {
	log     *zap.Logger
	cacheMu sync.RWMutex
	cache   map[string]*vm.Program
}

// NewExprEngine 创建 Expr 引擎
func NewExprEngine(log *zap.Logger) *ExprEngine {
	return &ExprEngine{log: log, cache: make(map[string]*vm.Program)}
}

// Compile 预编译脚本并缓存
func (e *ExprEngine) Compile(pluginName, script string) error {
	program, err := expr.Compile(script, e.exprOptions()...)
	if err != nil {
		return fmt.Errorf("expr compile: %w", err)
	}
	e.cacheMu.Lock()
	e.cache[pluginName] = program
	e.cacheMu.Unlock()
	return nil
}

// Execute 执行已编译的脚本
func (e *ExprEngine) Execute(ctx context.Context, pluginName string, env map[string]any) (*plugindomain.HookResult, error) {
	e.cacheMu.RLock()
	program, ok := e.cache[pluginName]
	e.cacheMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("expr program not found: %s", pluginName)
	}

	// 超时控制
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// 注入内置函数到环境
	env["deny"] = exprDeny
	env["allow"] = exprAllow
	env["now"] = time.Now
	env["hour"] = func() int { return time.Now().Hour() }
	env["minute"] = func() int { return time.Now().Minute() }
	env["weekday"] = func() int { return int(time.Now().Weekday()) } // 0=周日 6=周六
	env["date"] = func() string { return time.Now().Format("01-02") }
	env["datetime"] = func() string { return time.Now().Format("2006-01-02 15:04:05") }
	env["matchCIDR"] = exprMatchCIDR

	ch := make(chan exprResult, 1)
	go func() {
		output, err := expr.Run(program, env)
		ch <- exprResult{output: output, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("expr timeout: %s", pluginName)
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return normalizeExprOutput(r.output), nil
	}
}

// Invalidate 清除缓存
func (e *ExprEngine) Invalidate(pluginName string) {
	e.cacheMu.Lock()
	delete(e.cache, pluginName)
	e.cacheMu.Unlock()
}

func (e *ExprEngine) exprOptions() []expr.Option {
	return []expr.Option{
		expr.AllowUndefinedVariables(),
	}
}

type exprResult struct {
	output any
	err    error
}

// 内置函数

func exprDeny(message string) map[string]any {
	return map[string]any{"allow": false, "message": message}
}

func exprAllow() map[string]any {
	return map[string]any{"allow": true}
}

func exprMatchCIDR(ip, cidr string) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return network.Contains(net.ParseIP(ip))
}

func normalizeExprOutput(output any) *plugindomain.HookResult {
	result := &plugindomain.HookResult{Allow: true}
	switch v := output.(type) {
	case bool:
		result.Allow = v
	case map[string]any:
		if allow, ok := v["allow"].(bool); ok {
			result.Allow = allow
		}
		if msg, ok := v["message"].(string); ok {
			result.Message = msg
		}
		if data, ok := v["data"].(map[string]any); ok {
			result.Data = data
		}
	}
	return result
}
