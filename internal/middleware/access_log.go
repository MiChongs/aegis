package middleware

import (
	"time"

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
