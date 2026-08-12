package middleware

import (
	"net"

	"aegis/pkg/clientip"

	"github.com/gin-gonic/gin"
)

const (
	peerIPContextKey         = clientip.ContextKeyPeerIP
	clientIPDetailContextKey = clientip.ContextKeyDetail
)

// ClientIP 把「这个请求真正来自哪个 IP」定下来，必须挂在中间件栈的**第一位**。
//
// 判定规则全在 [clientip] 里；这里只做一件事，但这件事决定了改造范围：
// **把判定结果写回 `Request.RemoteAddr`**，而不是另开一个「正确的取 IP 函数」
// 让全仓库改调用点。
//
// 理由是覆盖面。`c.ClientIP()` 在这个仓库里散布在 25 个文件、57 处调用中，
// 还有一部分在第三方中间件内部（限流、WAF、追踪都各自取过一次）。
// 换函数意味着每一处都要改对、且以后每一个新调用点都要记得别用 gin 那个 ——
// 漏掉任何一处的表现不是报错，而是那一处悄悄按代理地址限流、按代理地址封禁。
// 改写 RemoteAddr 则让 `c.ClientIP()`、`c.RemoteIP()`、`Request.RemoteAddr`
// 三种取法**同时**变正确，包括还没写出来的那些调用点。
//
// 与之配套，gin 自己的 `SetTrustedProxies` 必须置空（见 transport/http/router.go）：
// 两套转发头解析同时开着，只会让「到底谁说了算」变成一个没人答得上来的问题。
//
// 直连对端仍然保留在 `request.peer_ip` 里 —— 判定错时，排障要问的第一个问题
// 就是「那这个请求到底是从哪台机器连过来的」。
func ClientIP(resolver *clientip.Resolver) gin.HandlerFunc {
	if resolver == nil {
		// 零依赖装配（openapi / postman / routes 子命令）走这一支：
		// 那里只扫路由结构，没有真实请求，也就没有 IP 可判。
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		result := resolver.Resolve(c.Request)
		if result.Peer.IsValid() {
			c.Set(peerIPContextKey, result.Peer.String())
		}
		c.Set(clientIPDetailContextKey, result)

		if result.Valid() {
			ip := result.IP.String()
			// 端口沿用直连对端的：gin 的 RemoteIP() 用 SplitHostPort 解析这个字段，
			// 不带端口会让它返回空串，而空 IP 会被防火墙当成非法请求直接拒掉。
			port := result.PeerPort
			if port == "" {
				port = "0"
			}
			c.Request.RemoteAddr = net.JoinHostPort(ip, port)
			c.Set(clientIPContextKey, ip)
		}

		if resolver.DebugHeader() {
			// 只在显式打开时回显。判定依据里含内网拓扑（每一跳的地址），
			// 默认吐给所有人不合适；但排「线上拿到的 IP 不对」时，
			// 一次 curl 就能看到结论和它的全部依据，比翻日志快得多。
			c.Header("X-Aegis-Client-IP", result.IP.String())
			c.Header("X-Aegis-Client-IP-Source", result.String())
		}

		c.Next()
	}
}

// RequestPeerIP 直连对端地址（TCP 层真正连上来的那一个）。
// 与 [RequestClientIP] 的区别：挂在反代后面时，这里是反代，那里是最终用户。
func RequestPeerIP(c *gin.Context) string {
	if value, ok := c.Get(peerIPContextKey); ok {
		if ip, ok := value.(string); ok {
			return ip
		}
	}
	return ""
}

// RequestClientIPDetail 本次判定的完整过程（来源、命中的头、转发链、是否信任对端）。
func RequestClientIPDetail(c *gin.Context) (clientip.Result, bool) {
	value, exists := c.Get(clientIPDetailContextKey)
	if !exists {
		return clientip.Result{}, false
	}
	result, ok := value.(clientip.Result)
	return result, ok
}
