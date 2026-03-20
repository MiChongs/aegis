package response

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
}

func Success(c *gin.Context, code int, message string, data interface{}) {
	c.JSON(200, Envelope{Code: code, Message: message, Data: data, RequestID: requestID(c)})
}

func Error(c *gin.Context, httpStatus int, code int, message string) {
	publicMessage := sanitizeMessage(httpStatus, message)
	if wantsHTML(c) {
		renderHTMLPage(c, httpStatus, publicMessage)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(httpStatus, Envelope{Code: code, Message: publicMessage, RequestID: requestID(c)})
}

func requestID(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}

func wantsHTML(c *gin.Context) bool {
	if c.Request == nil {
		return false
	}
	accept := strings.ToLower(c.GetHeader("Accept"))
	userAgent := strings.ToLower(c.GetHeader("User-Agent"))
	if strings.Contains(accept, "application/json") {
		return false
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	if strings.Contains(accept, "text/html") {
		return true
	}
	return accept == "" && strings.Contains(userAgent, "mozilla")
}

func sanitizeMessage(httpStatus int, fallback string) string {
	switch httpStatus {
	case http.StatusUnauthorized:
		return "访问请求未获授权"
	case http.StatusForbidden:
		return "当前请求已被拦截"
	case http.StatusTooManyRequests:
		return "请求过于频繁，请稍后再试"
	case http.StatusNotFound:
		return "请求的页面不存在"
	case http.StatusNotImplemented:
		return "服务能力暂未开放"
	case http.StatusBadGateway:
		return "服务暂时不可用"
	case http.StatusServiceUnavailable:
		return "服务维护中，请稍后再试"
	case http.StatusInternalServerError:
		return "服务暂时不可用"
	default:
		if strings.TrimSpace(fallback) == "" {
			return "请求未能完成"
		}
		return fallback
	}
}

func renderHTMLPage(c *gin.Context, httpStatus int, message string) {
	title, caption, actionText, actionHref := pageMeta(httpStatus)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'")
	c.Status(httpStatus)
	_, _ = c.Writer.WriteString(buildHTMLPage(httpStatus, title, caption, message, actionText, actionHref))
}

func pageMeta(httpStatus int) (string, string, string, string) {
	switch httpStatus {
	case http.StatusUnauthorized:
		return "访问受限", "Authorization Required", "返回首页", "/"
	case http.StatusForbidden:
		return "请求已被拦截", "Request Blocked", "返回首页", "/"
	case http.StatusTooManyRequests:
		return "请求过于频繁", "Rate Limit Applied", "返回首页", "/"
	case http.StatusNotFound:
		return "页面不存在", "Resource Not Found", "返回首页", "/"
	case http.StatusNotImplemented:
		return "功能暂未开放", "Capability Pending", "返回首页", "/"
	case http.StatusBadGateway:
		return "服务暂不可达", "Gateway Unavailable", "返回首页", "/"
	case http.StatusServiceUnavailable:
		return "服务维护中", "Scheduled Maintenance", "返回首页", "/"
	default:
		return "服务暂不可用", "Service Notice", "返回首页", "/"
	}
}

func buildHTMLPage(httpStatus int, title, caption, message, actionText, actionHref string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%d %s</title>
  <style>
    :root {
      --bg: #f5f2eb;
      --panel: rgba(255, 255, 255, 0.76);
      --text: #15202b;
      --muted: #667281;
      --line: rgba(21, 32, 43, 0.12);
      --line-strong: rgba(21, 32, 43, 0.2);
      --accent: #8f6a3d;
      --shadow: 0 20px 56px rgba(21, 32, 43, 0.08);
    }
    * { box-sizing: border-box; }
    html, body { min-height: 100%%; }
    body {
      margin: 0;
      color: var(--text);
      background:
        linear-gradient(180deg, rgba(255,255,255,0.55), rgba(255,255,255,0.55)),
        linear-gradient(135deg, #f8f5ef 0%%, #eee6dc 100%%);
      font-family: "Segoe UI Variable Text", "PingFang SC", "Microsoft YaHei UI", sans-serif;
      -webkit-font-smoothing: antialiased;
      text-rendering: optimizeLegibility;
    }
    body::before {
      content: "";
      position: fixed;
      inset: 0;
      background:
        linear-gradient(rgba(21,32,43,0.03) 1px, transparent 1px),
        linear-gradient(90deg, rgba(21,32,43,0.03) 1px, transparent 1px);
      background-size: 56px 56px;
      mask-image: linear-gradient(to bottom, rgba(0,0,0,0.18), transparent 92%%);
      pointer-events: none;
    }
    .shell {
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 28px 20px;
    }
    .card {
      position: relative;
      width: min(860px, 100%%);
      padding: 36px 38px 32px;
      border: 1px solid var(--line);
      background: var(--panel);
      backdrop-filter: blur(10px);
      box-shadow: var(--shadow);
    }
    .card::after {
      content: "";
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      height: 1px;
      background: rgba(255,255,255,0.72);
    }
    .crest {
      display: inline-flex;
      align-items: center;
      gap: 10px;
      padding: 7px 11px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,0.55);
      color: var(--muted);
      font-size: 11px;
      letter-spacing: 0.14em;
      text-transform: uppercase;
    }
    .crest-mark {
      width: 8px;
      height: 8px;
      background: var(--accent);
    }
    .content {
      display: grid;
      grid-template-columns: 160px minmax(0, 1fr);
      gap: 34px;
      align-items: start;
      margin-top: 32px;
    }
    .code {
      padding-top: 4px;
      padding-right: 26px;
      border-right: 1px solid var(--line);
    }
    .code strong {
      display: block;
      margin: 0;
      font-family: "Segoe UI Variable Display", "Segoe UI", sans-serif;
      font-size: clamp(68px, 7vw, 108px);
      font-weight: 650;
      line-height: 0.9;
      letter-spacing: -0.06em;
      color: #101820;
      white-space: nowrap;
    }
    .code span {
      display: block;
      margin-top: 10px;
      color: var(--accent);
      font-size: 11px;
      letter-spacing: 0.16em;
      text-transform: uppercase;
    }
    .main {
      min-width: 0;
    }
    h1 {
      margin: 0;
      font-family: Georgia, "Times New Roman", serif;
      font-size: clamp(30px, 4vw, 46px);
      font-weight: 500;
      line-height: 1.16;
      letter-spacing: 0.01em;
      max-width: 12ch;
    }
    .caption {
      margin-top: 10px;
      color: var(--muted);
      font-size: 11px;
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }
    .divider {
      width: 64px;
      height: 1px;
      margin: 18px 0 16px;
      background: linear-gradient(90deg, var(--accent), rgba(143,106,61,0));
    }
    p {
      margin: 0;
      max-width: 34em;
      color: #465362;
      font-size: 15px;
      line-height: 1.72;
    }
    .actions {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      margin-top: 26px;
    }
    .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 146px;
      min-height: 46px;
      padding: 11px 18px;
      border: 1px solid var(--line-strong);
      background: rgba(255,255,255,0.78);
      color: var(--text);
      text-decoration: none;
      transition: background .18s ease, border-color .18s ease, transform .18s ease;
    }
    .btn:hover {
      transform: translateY(-1px);
      background: #fff;
      border-color: rgba(21, 32, 43, 0.26);
    }
    .btn-secondary {
      background: transparent;
      color: var(--muted);
    }
    .footer {
      margin-top: 30px;
      padding-top: 14px;
      border-top: 1px solid var(--line);
      color: var(--muted);
      font-size: 11px;
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }
    @media (max-width: 720px) {
      .card {
        padding: 24px 22px 22px;
      }
      .content {
        grid-template-columns: 1fr;
        gap: 22px;
      }
      .code {
        padding-top: 0;
        padding-right: 0;
        padding-bottom: 16px;
        border-right: 0;
        border-bottom: 1px solid var(--line);
      }
      .code strong {
        font-size: clamp(60px, 20vw, 84px);
      }
      h1 {
        max-width: none;
      }
      .actions {
        width: 100%%;
      }
      .btn {
        flex: 1 1 100%%;
      }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="card" aria-labelledby="page-title">
      <div class="crest"><span class="crest-mark"></span><span>AEGIS SERVICE NOTICE</span></div>
      <div class="content">
        <div class="code">
          <strong>%d</strong>
          <span>%s</span>
        </div>
        <div class="main">
          <h1 id="page-title">%s</h1>
          <div class="caption">%s</div>
          <div class="divider"></div>
          <p>%s</p>
          <div class="actions">
            <a class="btn" href="%s">%s</a>
            <a class="btn btn-secondary" href="/">站点首页</a>
          </div>
          <div class="footer">Official Gateway Response</div>
        </div>
      </div>
    </section>
  </main>
</body>
</html>`,
		httpStatus,
		template.HTMLEscapeString(title),
		httpStatus,
		template.HTMLEscapeString(caption),
		template.HTMLEscapeString(title),
		template.HTMLEscapeString(caption),
		template.HTMLEscapeString(message),
		template.HTMLEscapeString(actionHref),
		template.HTMLEscapeString(actionText),
	)
}
