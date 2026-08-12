package middleware

import (
	"time"

	"aegis/pkg/clientip"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AccessLog 结构化访问日志，取代 gin.Logger()。
//
// 换掉 gin.Logger() 的理由不是它不好看，而是它与这套系统的日志出口不是一条链路：
// 它按自己的格式写 gin.DefaultWriter（默认 stdout）并自带 ANSI 着色，
// 而平台其余部分全部走 zap 结构化输出。两者混在同一个 stdout 里的后果是
//
//	{"level":"info","ts":...,"msg":"user login","app_id":3}
//	[GIN] 2026/08/12 - 10:23:01 | 200 |    1.9ms |  127.0.0.1 | GET  "/api/user/my"
//
// 采集端按 JSON 行解析，于是每一条请求日志都是一条解析失败；想按状态码或延迟
// 做告警更是无从下手——那些值只存在于一行没有结构的文本里。
//
// 字段选择：path 是实际请求路径，route 是命中的路由模板（`/api/admin/apps/:appkey`）。
// 两个都留是因为它们回答不同的问题——path 用来定位那一次具体请求，
// route 用来聚合「这个接口整体的延迟与错误率」。只留 path 的话，
// 带 ID 的路径会把同一个接口打散成成千上万个互不相干的 key。
func AccessLog(log *zap.Logger, skipPaths ...string) gin.HandlerFunc {
	if log == nil {
		return func(c *gin.Context) { c.Next() }
	}
	skip := make(map[string]struct{}, len(skipPaths))
	for _, path := range skipPaths {
		skip[path] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, skipped := skip[c.Request.URL.Path]; skipped {
			c.Next()
			return
		}

		started := time.Now()
		// 处理链有可能改写 URL（网关的 sealed 档会把密文 query 换成明文），
		// 而访问日志要记的是客户端实际发来的那个请求，所以先取下来
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", time.Since(started)),
			zap.String("ip", c.ClientIP()),
			zap.Int("size", c.Writer.Size()),
		}
		fields = append(fields, clientIPFields(c)...)
		if route := c.FullPath(); route != "" {
			fields = append(fields, zap.String("route", route))
		}
		if query != "" {
			fields = append(fields, zap.String("query", query))
		}
		if requestID := c.GetString("request_id"); requestID != "" {
			fields = append(fields, zap.String("request_id", requestID))
		}
		if ua := c.Request.UserAgent(); ua != "" {
			fields = append(fields, zap.String("user_agent", ua))
		}
		// handler 里 c.Error() 记下的错误：没有它，5xx 日志只说「500」，
		// 真正的原因要去另一条日志里按时间戳猜
		if errs := c.Errors.ByType(gin.ErrorTypePrivate).Errors(); len(errs) > 0 {
			fields = append(fields, zap.Strings("errors", errs))
		}

		log.Log(accessLogLevel(status), "http request", fields...)
	}
}

// clientIPFields 让访问日志自己交代 `ip` 这个值是怎么来的。
//
// 起因是一条查不下去的日志：`"ip": "8.221.123.21"` 每条请求都一样，而这一行里
// 没有任何东西能区分下面三种成因 —— 它们的处置完全不同：
//
//	对端就是它            客户端确实都从同一个出口来（代理 / VPN / 公司出口），判定正确
//	对端可信、链上停在它   前面有一跳公网代理（CDN / WAF / 云 LB）不在受信网段内
//	对端不可信、转发头被忽略 直连对端不在受信网段内，转发头整个没被采信
//
// 因此：`ip` 与直连对端不同就记 `peer`（第一种的 peer 与 ip 相同，一眼排除）；
// 判定退回了直连对端、请求里却带着转发头，就把被忽略的那条链也记下来 ——
// 那是「受信网段没配对」的确切信号，也是「全站 IP 收敛成同一个」最常见的成因。
//
// 平时只多一个 `peer` 字段，出问题时日志里直接有答案，不必先去改配置重启一轮。
func clientIPFields(c *gin.Context) []zap.Field {
	detail, ok := RequestClientIPDetail(c)
	if !ok {
		return nil
	}
	var fields []zap.Field
	if detail.Peer.IsValid() && detail.Peer != detail.IP {
		fields = append(fields, zap.String("peer", detail.Peer.String()))
	}
	if detail.Source == clientip.SourcePeer && len(detail.Chain) > 0 {
		fields = append(fields,
			zap.Bool("peer_trusted", detail.PeerTrusted),
			zap.Strings("forwarded_ignored", detail.Chain),
		)
	}
	return fields
}

// accessLogLevel 按状态码分级。
//
// 4xx 记 warn 而不是 info：这一档里混着「客户端用错了」和「有人在探接口」，
// 两者都值得在默认日志级别下被看到。5xx 一律 error——那是服务端自己的问题。
func accessLogLevel(status int) zapcore.Level {
	switch {
	case status >= 500:
		return zapcore.ErrorLevel
	case status >= 400:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}

// AccessLogSkipPaths 是默认跳过的路径：存活与就绪探针。
//
// 它们由编排系统每几秒打一次，且永远返回 200——记下来只会把真实流量冲淡，
// 而探针真的失败时有别的信号（Pod 重启、就绪门失败），不靠这条日志发现。
func AccessLogSkipPaths() []string {
	return []string{"/healthz", "/readyz"}
}
