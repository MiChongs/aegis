package middleware

import (
	"fmt"
	"net/http"

	"aegis/pkg/crashlog"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CrashRecovery 返回 Gin 中间件，捕获 HTTP handler 中的 panic，
// 记录崩溃日志后返回 500 响应。替代 gin.Recovery()。
func CrashRecovery(log *zap.Logger, cl *crashlog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				component := fmt.Sprintf("http.handler[%s %s]", c.Request.Method, c.Request.URL.Path)

				if cl != nil {
					cl.Write(component, r, true)
				}

				log.Error("HTTP handler panic recovered",
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.String("client_ip", c.ClientIP()),
					zap.Any("panic", r),
					zap.Stack("stack"),
				)

				// 如果响应尚未写入，返回 500
				if !c.Writer.Written() {
					response.Error(c, http.StatusInternalServerError, 50000, "服务器内部错误")
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
