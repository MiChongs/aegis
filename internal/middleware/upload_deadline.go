package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// UploadReadDeadline 为文件上传放宽读超时。
//
// http.Server 的 ReadTimeout（缺省 5 秒）覆盖整个请求体的读取：它挡慢速攻击很有效，
// 却也让任何几十 MB 以上的上传在传到一半时被掐断 —— 下游只看到一个残缺的 multipart，
// 报「缺少上传文件」，日志里没有任何超时的痕迹。这里只对 multipart / octet-stream
// 的写请求把读截止时间推后，其余请求仍受全局 ReadTimeout 约束。
//
// 必须挂在所有读请求体的中间件（防火墙、WAF）之前。
func UploadReadDeadline(window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if window > 0 && isUploadRequest(c.Request) {
			_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(window))
		}
		c.Next()
	}
}

func isUploadRequest(req *http.Request) bool {
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	return strings.HasPrefix(contentType, "multipart/form-data") || strings.HasPrefix(contentType, "application/octet-stream")
}
