// Package response 统一 HTTP 响应层。
//
// 核心能力：
//   - JSON 响应信封（含 code/message/data/requestId/timestamp/traceId）
//   - 分页数据信封（PagedData）
//   - 完整的成功/错误快捷方法
//   - 按内容协商自动返回 JSON 或 HTML 错误页
//   - HTML 错误页严格遵循项目 DESIGN.md（Claude / Anthropic 风格）
//   - 自动展开 *apperrors.AppError → HTTP 状态码 + 业务码
//   - 自动注入 X-Request-ID / X-Trace-ID 响应头
//   - 自动埋点 OpenTelemetry TraceID（若存在）
package response

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	apperrors "aegis/pkg/errors"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

// ─────────────────────────────────────────────────────────────────────
// Envelope / PagedData
// ─────────────────────────────────────────────────────────────────────

// Envelope 是所有 JSON 响应的统一信封。
type Envelope struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
	TraceID   string      `json:"traceId,omitempty"`
	Timestamp int64       `json:"timestamp,omitempty"` // Unix 毫秒
}

// PagedData 标准分页响应结构；配合 Paginated() 使用。
type PagedData struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page,omitempty"`
	PageSize int         `json:"pageSize,omitempty"`
	HasMore  bool        `json:"hasMore,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────
// 常量
// ─────────────────────────────────────────────────────────────────────

const (
	// 业务默认 code
	codeOK              = 200
	codeDefaultError    = 0
	codeInternalError   = 50000
	codeBadRequest      = 40000
	codeUnauthorized    = 40100
	codeForbidden       = 40300
	codeNotFound        = 40400
	codeConflict        = 40900
	codeUnprocessable   = 42200
	codeTooManyRequests = 42900
	codeUnavailable     = 50300

	headerRequestID = "X-Request-ID"
	headerTraceID   = "X-Trace-ID"
)

// ─────────────────────────────────────────────────────────────────────
// 成功响应
// ─────────────────────────────────────────────────────────────────────

// Success 向后兼容的通用成功响应（HTTP 始终 200）。
func Success(c *gin.Context, code int, message string, data interface{}) {
	writeJSON(c, http.StatusOK, Envelope{Code: code, Message: message, Data: data})
}

// OK 200 OK · code=200 · message="ok"。
func OK(c *gin.Context, data interface{}) {
	writeJSON(c, http.StatusOK, Envelope{Code: codeOK, Message: "ok", Data: data})
}

// OKWithMessage 200 OK · 自定义 message。
func OKWithMessage(c *gin.Context, message string, data interface{}) {
	if message == "" {
		message = "ok"
	}
	writeJSON(c, http.StatusOK, Envelope{Code: codeOK, Message: message, Data: data})
}

// Created 201 Created。
func Created(c *gin.Context, data interface{}) {
	writeJSON(c, http.StatusCreated, Envelope{Code: codeOK, Message: "created", Data: data})
}

// Accepted 202 Accepted —— 用于异步任务受理。
func Accepted(c *gin.Context, data interface{}) {
	writeJSON(c, http.StatusAccepted, Envelope{Code: codeOK, Message: "accepted", Data: data})
}

// NoContent 204 No Content —— 无响应体。
func NoContent(c *gin.Context) {
	setMetaHeaders(c)
	c.AbortWithStatus(http.StatusNoContent)
}

// Paginated 统一分页响应；page 为 0 时表示 cursor/不计页码。
func Paginated(c *gin.Context, list interface{}, total int64, page, pageSize int) {
	hasMore := false
	if pageSize > 0 {
		hasMore = int64(page)*int64(pageSize) < total
	}
	writeJSON(c, http.StatusOK, Envelope{
		Code:    codeOK,
		Message: "ok",
		Data: PagedData{
			List:     list,
			Total:    total,
			Page:     page,
			PageSize: pageSize,
			HasMore:  hasMore,
		},
	})
}

// ─────────────────────────────────────────────────────────────────────
// 错误响应
// ─────────────────────────────────────────────────────────────────────

// Error 向后兼容的错误响应；按内容协商返回 JSON 或 HTML。
func Error(c *gin.Context, httpStatus int, code int, message string) {
	publicMessage := sanitizeMessage(httpStatus, message)
	c.Header("Cache-Control", "no-store")

	if wantsHTML(c) {
		renderHTMLPage(c, httpStatus, publicMessage)
		c.Abort()
		return
	}

	writeJSON(c, httpStatus, Envelope{Code: code, Message: publicMessage})
	c.Abort()
}

// ErrorE 自动展开 *apperrors.AppError → HTTP 状态码 + 业务码；
// 其他错误视为 500。返回 true 表示已写入响应。
func ErrorE(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*apperrors.AppError); ok {
		Error(c, ae.HTTPStatus, ae.Code, ae.Message)
		return true
	}
	Error(c, http.StatusInternalServerError, codeInternalError, err.Error())
	return true
}

// BadRequest 400。
func BadRequest(c *gin.Context, code int, message string) {
	if code == 0 {
		code = codeBadRequest
	}
	Error(c, http.StatusBadRequest, code, message)
}

// Unauthorized 401。
func Unauthorized(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, codeUnauthorized, message)
}

// Forbidden 403。
func Forbidden(c *gin.Context, message string) {
	Error(c, http.StatusForbidden, codeForbidden, message)
}

// NotFound 404。
func NotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, codeNotFound, message)
}

// Conflict 409。
func Conflict(c *gin.Context, code int, message string) {
	if code == 0 {
		code = codeConflict
	}
	Error(c, http.StatusConflict, code, message)
}

// UnprocessableEntity 422。
func UnprocessableEntity(c *gin.Context, code int, message string) {
	if code == 0 {
		code = codeUnprocessable
	}
	Error(c, http.StatusUnprocessableEntity, code, message)
}

// TooManyRequests 429。
func TooManyRequests(c *gin.Context, message string) {
	Error(c, http.StatusTooManyRequests, codeTooManyRequests, message)
}

// Internal 500。message 为空时使用脱敏后的通用提示。
func Internal(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, codeInternalError, message)
}

// ServiceUnavailable 503。
func ServiceUnavailable(c *gin.Context, message string) {
	Error(c, http.StatusServiceUnavailable, codeUnavailable, message)
}

// ─────────────────────────────────────────────────────────────────────
// 内部工具：header 注入 / JSON 写入 / 取 ID / 内容协商
// ─────────────────────────────────────────────────────────────────────

func writeJSON(c *gin.Context, httpStatus int, env Envelope) {
	setMetaHeaders(c)
	env.RequestID = requestID(c)
	env.TraceID = traceID(c)
	env.Timestamp = time.Now().UnixMilli()
	c.JSON(httpStatus, env)
}

// setMetaHeaders 注入 X-Request-ID / X-Trace-ID 等元信息响应头。
func setMetaHeaders(c *gin.Context) {
	if rid := requestID(c); rid != "" {
		c.Header(headerRequestID, rid)
	}
	if tid := traceID(c); tid != "" {
		c.Header(headerTraceID, tid)
	}
}

// requestID 读取 gin.Context 中由 middleware.RequestID 注入的 request_id。
func requestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	rid, _ := value.(string)
	return rid
}

// traceID 读取 OpenTelemetry 当前 Span 的 TraceID（若有）。
func traceID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	span := trace.SpanFromContext(c.Request.Context())
	sc := span.SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// wantsHTML 决定是否渲染 HTML 错误页。支持 ?format=json|html 强制覆盖。
func wantsHTML(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	switch strings.ToLower(c.Query("format")) {
	case "json":
		return false
	case "html":
		return true
	}
	accept := strings.ToLower(c.GetHeader("Accept"))
	if strings.Contains(accept, "application/json") {
		return false
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	if strings.Contains(accept, "text/html") {
		return true
	}
	userAgent := strings.ToLower(c.GetHeader("User-Agent"))
	return accept == "" && strings.Contains(userAgent, "mozilla")
}

// sanitizeMessage 对错误信息做脱敏；5xx 一律使用通用提示避免信息泄露。
func sanitizeMessage(httpStatus int, fallback string) string {
	fallback = strings.TrimSpace(fallback)

	switch httpStatus {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusConflict, http.StatusUnprocessableEntity:
		if fallback != "" {
			return fallback
		}
		return "请求未能完成"
	case http.StatusTooManyRequests:
		return "请求过于频繁，请稍后再试"
	case http.StatusNotFound:
		if fallback != "" {
			return fallback
		}
		return "请求的资源不存在"
	case http.StatusNotImplemented:
		return "服务能力暂未开放"
	case http.StatusBadGateway:
		return "服务暂时不可用"
	case http.StatusServiceUnavailable:
		return "服务维护中，请稍后再试"
	case http.StatusInternalServerError:
		return "服务暂时不可用"
	default:
		if fallback == "" {
			return "请求未能完成"
		}
		return fallback
	}
}

// ─────────────────────────────────────────────────────────────────────
// HTML 错误页 — 严格遵循 DESIGN.md (Claude / Anthropic)
// ─────────────────────────────────────────────────────────────────────

type pageConfig struct {
	Title         string
	Caption       string
	Subtitle      string
	Hint          string
	ActionText    string
	ActionHref    string
	ActionNav     string // "reload" | "back" | "" — 由小型内联 JS 增强；空值按纯 href 跳转
	SecondaryTxt  string
	SecondaryURL  string
	SecondaryNav  string
}

type htmlPageData struct {
	Status    int
	RequestID string
	Config    pageConfig
}

func renderHTMLPage(c *gin.Context, httpStatus int, message string) {
	cfg := pageMeta(httpStatus, message)
	setMetaHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'")
	c.Status(httpStatus)

	data := htmlPageData{
		Status:    httpStatus,
		RequestID: requestID(c),
		Config:    cfg,
	}

	var buf bytes.Buffer
	if err := errorPageTemplate.Execute(&buf, data); err != nil {
		// 模板渲染失败 —— 降级为纯文本，避免 500 上加 500
		fallback := fmt.Sprintf("%d %s\n\n%s", httpStatus, cfg.Title, cfg.Subtitle)
		c.Header("Content-Type", "text/plain; charset=utf-8")
		_, _ = c.Writer.WriteString(fallback)
		return
	}
	_, _ = c.Writer.Write(buf.Bytes())
}

func pageMeta(httpStatus int, message string) pageConfig {
	message = strings.TrimSpace(message)
	switch httpStatus {
	case http.StatusBadRequest:
		return pageConfig{
			Title:        "请求无法被理解",
			Caption:      "Bad Request",
			Subtitle:     pick(message, "网关无法解析您的请求，可能是参数缺失、类型错误或格式不正确。"),
			Hint:         "请检查参数是否完整、是否符合接口文档再试。",
			ActionText:   "返回首页",
			ActionHref:   "/",
			SecondaryTxt: "返回上一页",
			SecondaryURL: "/",
			SecondaryNav: "back",
		}
	case http.StatusUnauthorized:
		return pageConfig{
			Title:        "访问需要凭证",
			Caption:      "Authorization Required",
			Subtitle:     pick(message, "您当前的会话已失效或尚未登录。请先完成身份验证再尝试访问此资源。"),
			Hint:         "前往登录页可继续，或联系管理员申请访问权限。",
			ActionText:   "去登录",
			ActionHref:   "/login",
			SecondaryTxt: "返回首页",
			SecondaryURL: "/",
		}
	case http.StatusForbidden:
		return pageConfig{
			Title:        "请求已被拒绝",
			Caption:      "Request Blocked",
			Subtitle:     pick(message, "这个资源对当前身份不可见。可能是权限不足，或请求未通过安全策略校验。"),
			Hint:         "确认账号拥有所需角色，或更换网络环境后再试。",
			ActionText:   "返回首页",
			ActionHref:   "/",
			SecondaryTxt: "联系管理员",
			SecondaryURL: "mailto:support@aegis.local",
		}
	case http.StatusNotFound:
		return pageConfig{
			Title:        "这里没有内容",
			Caption:      "Resource Not Found",
			Subtitle:     pick(message, "您访问的页面或资源可能已被移动、更名，或从未存在。"),
			Hint:         "检查地址栏拼写，或返回首页从导航重新进入。",
			ActionText:   "返回首页",
			ActionHref:   "/",
			SecondaryTxt: "返回上一页",
			SecondaryURL: "/",
			SecondaryNav: "back",
		}
	case http.StatusMethodNotAllowed:
		return pageConfig{
			Title:        "该方法不被允许",
			Caption:      "Method Not Allowed",
			Subtitle:     pick(message, "您使用的 HTTP 方法不被此端点接受。"),
			Hint:         "请确认接口文档中声明的请求方式（GET/POST 等）。",
			ActionText:   "返回首页",
			ActionHref:   "/",
			SecondaryTxt: "返回上一页",
			SecondaryURL: "/",
			SecondaryNav: "back",
		}
	case http.StatusConflict:
		return pageConfig{
			Title:        "资源状态冲突",
			Caption:      "Resource Conflict",
			Subtitle:     pick(message, "请求与资源的当前状态存在冲突，无法直接继续。"),
			Hint:         "刷新页面获取最新状态，或重新发起请求。",
			ActionText:   "刷新重试",
			ActionHref:   "",
			ActionNav:    "reload",
			SecondaryTxt: "返回首页",
			SecondaryURL: "/",
		}
	case http.StatusUnprocessableEntity:
		return pageConfig{
			Title:        "参数无法处理",
			Caption:      "Unprocessable Entity",
			Subtitle:     pick(message, "请求语法正确，但参数语义不符合业务规则。"),
			Hint:         "请按照接口校验规则调整字段后再试。",
			ActionText:   "返回上一页",
			ActionHref:   "/",
			ActionNav:    "back",
			SecondaryTxt: "返回首页",
			SecondaryURL: "/",
		}
	case http.StatusTooManyRequests:
		return pageConfig{
			Title:        "请求过于频繁",
			Caption:      "Rate Limit Applied",
			Subtitle:     "您的访问在短时间内达到阈值，网关已暂时降速保护后端。",
			Hint:         "请稍候片刻再试；若持续被限，可能需要调整调用频率或联系管理员放行。",
			ActionText:   "稍后重试",
			ActionHref:   "",
			ActionNav:    "reload",
			SecondaryTxt: "返回首页",
			SecondaryURL: "/",
		}
	case http.StatusNotImplemented:
		return pageConfig{
			Title:        "功能尚未开放",
			Caption:      "Capability Pending",
			Subtitle:     "当前版本的 Aegis 还未实现该端点。路线图中它已在计划内。",
			Hint:         "如需提前试用，请联系团队或在内部反馈渠道提出需求。",
			ActionText:   "返回首页",
			ActionHref:   "/",
			SecondaryTxt: "查看 API 文档",
			SecondaryURL: "/docs",
		}
	case http.StatusBadGateway:
		return pageConfig{
			Title:        "网关暂时失联",
			Caption:      "Gateway Unavailable",
			Subtitle:     "上游服务没有按时响应。可能是瞬时抖动，也可能正在滚动发布。",
			Hint:         "刷新通常即可恢复；若持续失败，请查看状态页。",
			ActionText:   "刷新重试",
			ActionHref:   "",
			ActionNav:    "reload",
			SecondaryTxt: "状态页",
			SecondaryURL: "/status",
		}
	case http.StatusServiceUnavailable:
		return pageConfig{
			Title:        "服务正在维护",
			Caption:      "Scheduled Maintenance",
			Subtitle:     "我们正在对系统进行计划内升级，以带来更稳定、更高效的体验。",
			Hint:         "请稍后再访问。公告栏通常会同步预计恢复时间。",
			ActionText:   "稍后再访问",
			ActionHref:   "",
			ActionNav:    "reload",
			SecondaryTxt: "查看公告",
			SecondaryURL: "/announcements",
		}
	case http.StatusGatewayTimeout:
		return pageConfig{
			Title:        "网关等待超时",
			Caption:      "Gateway Timeout",
			Subtitle:     pick(message, "上游服务在约定时间内未返回结果。"),
			Hint:         "稍后重试通常即可恢复。若持续超时，请联系运维。",
			ActionText:   "刷新重试",
			ActionHref:   "",
			ActionNav:    "reload",
			SecondaryTxt: "状态页",
			SecondaryURL: "/status",
		}
	case http.StatusInternalServerError:
		return pageConfig{
			Title:        "后端出了点意外",
			Caption:      "Unexpected Error",
			Subtitle:     "服务器遇到了未预期的异常。相关日志已记录，团队会尽快排查。",
			Hint:         "通常刷新即可恢复。若带有 Request ID，请在反馈时附上。",
			ActionText:   "刷新重试",
			ActionHref:   "",
			ActionNav:    "reload",
			SecondaryTxt: "返回首页",
			SecondaryURL: "/",
		}
	default:
		if httpStatus >= 500 {
			return pageConfig{
				Title:        "服务暂不可用",
				Caption:      "Service Notice",
				Subtitle:     pick(message, "网关返回了非预期的响应。"),
				Hint:         "请稍后再试，或回到首页继续其他操作。",
				ActionText:   "刷新重试",
				ActionHref:   "",
				ActionNav:    "reload",
				SecondaryTxt: "返回首页",
				SecondaryURL: "/",
			}
		}
		return pageConfig{
			Title:        "请求未能完成",
			Caption:      "Request Failed",
			Subtitle:     pick(message, "网关无法处理您的请求。"),
			Hint:         "请检查参数后重试，或返回首页。",
			ActionText:   "返回首页",
			ActionHref:   "/",
			SecondaryTxt: "返回上一页",
			SecondaryURL: "/",
			SecondaryNav: "back",
		}
	}
}

func pick(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

// MarshalJSON 方便外部包断言 Envelope 输出格式（例如 OpenAPI 生成器）。
func (e Envelope) MarshalJSON() ([]byte, error) {
	type alias Envelope
	return json.Marshal(alias(e))
}

// ─────────────────────────────────────────────────────────────────────
// HTML 模板 —— 启动时一次编译
// ─────────────────────────────────────────────────────────────────────

var errorPageTemplate = template.Must(template.New("error").Parse(htmlPageSource))

const htmlPageSource = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex, nofollow">
  <title>{{.Config.Title}} — Aegis</title>
  <style>
    :root {
      --bg: #f5f4ed;
      --bg-soft: #eee9de;
      --text-primary: #141413;
      --text-body: #3d3d3a;
      --text-secondary: #5e5d59;
      --text-tertiary: #87867f;
      --rule: #e8e6dc;
      --rule-soft: #f0eee6;
      --brand: #c96442;
      --brand-hover: #b55230;
      --warm-sand: #e8e6dc;
      --ring: #d1cfc5;
      --ring-deep: #b9b7ac;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #141413;
        --bg-soft: #1c1c1a;
        --text-primary: #f5f4ed;
        --text-body: #e6e4d8;
        --text-secondary: #b0aea5;
        --text-tertiary: #87867f;
        --rule: #30302e;
        --rule-soft: #242422;
        --brand: #d97757;
        --brand-hover: #e78a65;
        --warm-sand: #30302e;
        --ring: #3d3d3a;
        --ring-deep: #4d4c48;
      }
    }

    *, *::before, *::after { box-sizing: border-box; }
    html, body { min-height: 100%; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text-primary);
      font-family: "Anthropic Sans", "Inter", "PingFang SC", "Microsoft YaHei UI", system-ui, sans-serif;
      font-size: 16px;
      line-height: 1.6;
      -webkit-font-smoothing: antialiased;
      text-rendering: optimizeLegibility;
    }

    .page {
      min-height: 100vh;
      display: grid;
      grid-template-rows: auto 1fr auto;
      padding: 0 40px;
    }

    .top {
      max-width: 1120px;
      width: 100%;
      margin: 0 auto;
      padding: 36px 0 0;
    }
    .wordmark {
      display: inline-flex;
      align-items: center;
      gap: 10px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 22px;
      font-weight: 500;
      letter-spacing: -0.01em;
      color: var(--text-primary);
      text-decoration: none;
      line-height: 1;
    }
    .wordmark .mark {
      display: inline-block;
      width: 10px;
      height: 10px;
      background: var(--brand);
      border-radius: 50%;
      transform: translateY(-1px);
    }

    .main {
      max-width: 1120px;
      width: 100%;
      margin: 0 auto;
      display: flex;
      align-items: center;
      padding: 40px 0;
    }
    .article {
      max-width: 720px;
      width: 100%;
    }

    .kicker {
      display: inline-flex;
      align-items: baseline;
      gap: 10px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic;
      font-size: 15px;
      font-weight: 500;
      color: var(--brand);
      letter-spacing: 0.01em;
    }
    .kicker .code {
      font-family: "Anthropic Mono", "SF Mono", Consolas, monospace;
      font-style: normal;
      font-size: 13px;
      letter-spacing: 0.04em;
      color: var(--text-tertiary);
    }

    .rule {
      width: 48px;
      height: 1px;
      margin: 22px 0 30px;
      background: var(--brand);
      border: 0;
    }

    h1 {
      margin: 0;
      font-family: "Anthropic Serif", Georgia, "Times New Roman", serif;
      font-size: clamp(44px, 6.5vw, 72px);
      font-weight: 500;
      line-height: 1.06;
      letter-spacing: -0.02em;
      color: var(--text-primary);
    }

    .lede {
      margin: 28px 0 0;
      max-width: 36em;
      font-size: 20px;
      line-height: 1.60;
      color: var(--text-body);
    }

    .hint {
      margin: 12px 0 0;
      max-width: 38em;
      font-size: 17px;
      line-height: 1.65;
      color: var(--text-secondary);
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic;
    }

    .actions {
      display: inline-flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 28px;
      margin-top: 40px;
    }

    .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 44px;
      padding: 11px 22px;
      font-family: inherit;
      font-size: 15.5px;
      font-weight: 500;
      line-height: 1;
      text-decoration: none;
      border: 0;
      border-radius: 12px;
      cursor: pointer;
      background: var(--brand);
      color: #faf9f5;
      box-shadow: 0 0 0 1px var(--brand);
      transition: background 140ms linear, box-shadow 140ms linear;
    }
    .btn:hover {
      background: var(--brand-hover);
      box-shadow: 0 0 0 1px var(--brand-hover);
    }

    .link {
      display: inline-flex;
      align-items: baseline;
      gap: 6px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic;
      font-size: 16.5px;
      color: var(--text-secondary);
      text-decoration: none;
      border-bottom: 1px solid transparent;
      padding-bottom: 1px;
      transition: color 140ms linear, border-color 140ms linear;
    }
    .link:hover {
      color: var(--text-primary);
      border-bottom-color: var(--brand);
    }
    .link .arrow {
      font-family: "Anthropic Sans", system-ui, sans-serif;
      font-style: normal;
    }

    .bottom {
      max-width: 1120px;
      width: 100%;
      margin: 0 auto;
      padding: 32px 0 40px;
      border-top: 1px solid var(--rule);
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 10px 14px;
      color: var(--text-tertiary);
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic;
      font-size: 13.5px;
      letter-spacing: 0.01em;
    }
    .bottom .sep { color: var(--rule); font-style: normal; }
    .bottom .req code {
      font-family: "Anthropic Mono", "SF Mono", Consolas, monospace;
      font-style: normal;
      font-size: 12px;
      letter-spacing: 0;
      margin-left: 6px;
      padding: 2px 7px;
      background: var(--bg-soft);
      color: var(--text-body);
      border-radius: 6px;
      box-shadow: inset 0 0 0 1px var(--rule);
    }
    .bottom .grow { flex: 1; }

    @media (max-width: 680px) {
      .page { padding: 0 24px; }
      .top { padding-top: 24px; }
      .wordmark { font-size: 20px; }
      .main { padding: 24px 0; }
      h1 { font-size: clamp(34px, 9vw, 48px); letter-spacing: -0.015em; }
      .lede { font-size: 17px; margin-top: 22px; }
      .hint { font-size: 15.5px; }
      .actions { gap: 20px; margin-top: 32px; }
      .bottom { padding: 22px 0 28px; font-size: 12.5px; }
    }
    ` + BrandLogoCSS + `
  </style>
</head>
<body>
  <div class="page">
    <header class="top">
      ` + BrandLogoHTML + `
    </header>

    <main class="main">
      <article class="article">
        <div class="kicker"><span>{{.Config.Caption}}</span><span class="code">{{.Status}}</span></div>
        <hr class="rule" aria-hidden="true">
        <h1>{{.Config.Title}}</h1>
        <p class="lede">{{.Config.Subtitle}}</p>
        <p class="hint">{{.Config.Hint}}</p>
        <div class="actions">
          <a class="btn" href="{{if .Config.ActionHref}}{{.Config.ActionHref}}{{else}}#{{end}}"{{if .Config.ActionNav}} data-nav="{{.Config.ActionNav}}"{{end}}>{{.Config.ActionText}}</a>
          <a class="link" href="{{if .Config.SecondaryURL}}{{.Config.SecondaryURL}}{{else}}#{{end}}"{{if .Config.SecondaryNav}} data-nav="{{.Config.SecondaryNav}}"{{end}}>{{.Config.SecondaryTxt}} <span class="arrow" aria-hidden="true">→</span></a>
        </div>
      </article>
    </main>

    <footer class="bottom">
      <span>Aegis Platform</span>
      <span class="sep">·</span>
      <span>Official Gateway Response</span>
      {{if .RequestID}}<span class="sep">·</span><span class="req">Request ID <code>{{.RequestID}}</code></span>{{end}}
      <span class="grow"></span>
    </footer>
  </div>
  <script>
  (function(){
    document.addEventListener('click', function(e){
      var a = e.target.closest && e.target.closest('a[data-nav]');
      if (!a) return;
      var nav = a.getAttribute('data-nav');
      if (nav === 'back') {
        e.preventDefault();
        if (window.history && history.length > 1) {
          history.back();
        } else {
          location.href = a.getAttribute('href') || '/';
        }
      } else if (nav === 'reload') {
        e.preventDefault();
        location.reload();
      }
    });
  })();
  </script>
</body>
</html>`
