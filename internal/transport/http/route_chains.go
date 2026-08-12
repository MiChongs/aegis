package httptransport

import (
	"maps"
	"sync"

	"github.com/gin-gonic/gin"
)

// 中间件链深度的采集。
//
// 为什么非要接 gin 的调试回调才能拿到这个数：`engine.Routes()` 返回的
// `gin.RouteInfo` 只有 Method / Path / Handler / HandlerFunc 四个字段，
// **不含中间件链**——最终 handler 之前挂了几层拦截器，路由表里查不到。
// 唯一暴露这个数的地方就是 `DebugPrintRouteFunc` 的 nuHandlers 形参。
//
// 而这个回调同时也是灭掉 gin 默认路由输出的唯一开关：gin 的 debugPrintRoute
// 在 DebugPrintRouteFunc 为 nil 时才打自己那行
//
//	[GIN-debug] GET  /api/v1/apps/:appkey/config --> ....AppConfig-fm (14 handlers)
//
// 九百多条路由就是九百多行滚屏，正好把启动横幅冲掉。接住它，两件事一起解决。
//
// 采集不到不是故障：gin 只在 debug 档回调这个函数，release 与 test 档下
// 链深度全为 0，路由清单里那一列会被 go-pretty 的 SuppressEmptyColumns 抹掉。

var (
	routeChainMu sync.Mutex
	// routeChainByPath 是进程级共享的，因为链深度只取决于路由注册代码本身：
	// 同一份 NewRouter 建出来的每个 engine（生产实例、各个测试实例）
	// 对同一条路由算出的层数必然相同，后写覆盖前写不会产生歧义。
	routeChainByPath = map[string]int{}
)

// captureRouteChains 在路由注册期间接住 gin 的路由调试回调，返回释放函数。
//
// 必须成对使用（`defer release()`）：DebugPrintRouteFunc 是 gin 的包级变量，
// 留着不还原会让同进程里其他人建的 engine 也被静音。
// 互斥锁一路持有到释放，因为这期间那个全局变量指向本次调用的闭包。
func captureRouteChains() (release func()) {
	routeChainMu.Lock()
	previous := gin.DebugPrintRouteFunc
	gin.DebugPrintRouteFunc = func(method, path, _ string, handlers int) {
		routeChainByPath[routeKey(method, path)] = handlers
	}
	return func() {
		gin.DebugPrintRouteFunc = previous
		routeChainMu.Unlock()
	}
}

// routeChainDepths 返回链深度表的快照，键为 `METHOD /path`。
func routeChainDepths() map[string]int {
	routeChainMu.Lock()
	defer routeChainMu.Unlock()
	return maps.Clone(routeChainByPath)
}
