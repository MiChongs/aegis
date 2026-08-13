package httptransport

import (
	"net/http"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 路径存在、方法不匹配时的收口。挂在 router.NoMethod 上。
//
// gin 走到这里之前已经做完了两件事（见 gin.Engine.handleHTTPRequest）：
// 把该路径**真正支持**的方法算出来写进 `Allow` 头，并把状态预置为 405。
// 这里要做的只是不把它改坏 —— 之前这个位置回的是
// `501 服务能力暂未开放`，三处都不对：
//
//	语义   501 的定义是「服务器不认识这个方法」（对任何资源都不支持），
//	       而这里的事实是方法认识、只是这个资源不接受，那正是 405
//	指标   501 属于 5xx，于是「客户端把 GET 写成了 POST」会被计成服务端故障，
//	       直接抬高错误率与 SLO 违约，排查方向也被带向后端
//	重试   HTTP 客户端与 SDK 普遍对 5xx 重试、对 4xx 不重试，
//	       用错方法的调用方会带着必然失败的请求一直打回来
//
// `Allow` 头必须原样留着：RFC 9110 §15.5.6 要求 405 响应携带它，
// 调用方（尤其是生成式客户端）靠它自我纠正。
func methodNotAllowed(c *gin.Context) {
	allow := c.Writer.Header().Get("Allow")

	// OPTIONS 在这里兜底，而不是全部交给 CORS 中间件。
	//
	// gin-contrib/cors 只处理**带 `Origin` 头**的请求，且 CORS 未启用时
	// middleware.CORS 整个是直通的。因此这两类 OPTIONS 会一路落到这里：
	// 关闭 CORS 时浏览器发出的预检，以及非浏览器客户端拿 OPTIONS 探能力。
	// 让它们拿 405 等于说「本资源不支持 OPTIONS」，可 RFC 9110 §9.3.7 里
	// OPTIONS 问的恰恰是「这个资源支持什么」—— 答案就在 Allow 头里，
	// 已经算好了。带 Origin 的预检仍由 CORS 中间件在更靠前的位置短路，
	// 走不到这里，所以两条路径不会打架。
	if c.Request.Method == http.MethodOptions && allow != "" {
		response.NoContent(c)
		return
	}

	response.Error(c, http.StatusMethodNotAllowed, 40500, "请求方法不被此接口接受")
}
