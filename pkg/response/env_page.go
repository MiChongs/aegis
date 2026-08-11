package response

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────
// HTML 环境查看页 (/cons-env) — Claude / Anthropic 设计语言
//
// 服务端诊断：IP、TLS、HTTP、UA-CH、Geo、Headers
// 客户端诊断：UA-CH 高熵、OS、屏幕、GPU、编解码器、图像格式、字体、
//   Storage、Capabilities、Permissions、Clock Skew、Fingerprint
// 数据只在客户端本地渲染，不回传。
// ─────────────────────────────────────────────────────────────────────

// EnvPageData 环境查看页所需的服务端已知字段。
type EnvPageData struct {
	// Session
	ClientIP    string
	IPVersion   string // "IPv4" | "IPv6" | ""
	IsPrivate   bool
	RemoteAddr  string
	RemotePort  string
	Method      string
	URL         string
	Host        string
	Protocol    string // HTTP/1.1 / HTTP/2.0 / HTTP/3
	SchemeHTTPS bool

	// Transport
	TLSVersion    string
	TLSCipher     string
	TLSServerName string
	ALPN          string

	// Client context
	UserAgent      string
	Referer        string
	Origin         string
	AcceptLang     string
	Accept         string
	AcceptEncoding string

	// UA-Client Hints (low entropy + Sec-Ch-Ua)
	UAClientBrand    string // Sec-Ch-Ua
	UAClientPlatform string // Sec-Ch-Ua-Platform
	UAClientMobile   string // Sec-Ch-Ua-Mobile

	// Forwarding / proxy
	ForwardedFor   string
	RealIP         string
	ForwardedProto string
	Via            string

	// Security fetch metadata
	SecFetchSite string
	SecFetchMode string
	SecFetchDest string

	// GeoIP (via LocationService)
	Country  string
	Region   string
	City     string
	ASN      string
	ISP      string
	Timezone string

	// Raw headers (whitelisted)
	Headers    map[string]string
	ServerTime time.Time // 用于客户端做 clock-skew 对比
	CheckedAt  time.Time
}

// RenderEnvPage 渲染客户端 / 服务端联合环境页。
func RenderEnvPage(c *gin.Context, data EnvPageData) {
	setMetaHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Accept-CH", "Sec-CH-UA-Platform-Version, Sec-CH-UA-Arch, Sec-CH-UA-Bitness, Sec-CH-UA-Full-Version-List, Sec-CH-UA-Model")
	c.Header("Permissions-Policy", "ch-ua-platform-version=(self), ch-ua-arch=(self), ch-ua-bitness=(self), ch-ua-model=(self)")
	c.Header("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
			"img-src data: blob:; connect-src 'self'; base-uri 'none'; form-action 'self'")

	if data.CheckedAt.IsZero() {
		data.CheckedAt = time.Now()
	}
	if data.ServerTime.IsZero() {
		data.ServerTime = data.CheckedAt
	}

	view := envView{
		Data:      data,
		RequestID: requestID(c),
		TraceID:   traceID(c),
		Year:      time.Now().Year(),
	}
	view.Derive()

	var buf bytes.Buffer
	if err := envPageTemplate.Execute(&buf, view); err != nil {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		_, _ = c.Writer.WriteString(fmt.Sprintf("Aegis Env Inspector\nIP: %s\nUA: %s\n", data.ClientIP, data.UserAgent))
		return
	}
	c.Status(200)
	_, _ = c.Writer.Write(buf.Bytes())
}

// ExtractEnvPageData 从 *gin.Context 抽取完整服务端字段。
func ExtractEnvPageData(c *gin.Context) EnvPageData {
	if c == nil || c.Request == nil {
		return EnvPageData{CheckedAt: time.Now(), ServerTime: time.Now()}
	}
	req := c.Request
	now := time.Now()

	clientIP := c.ClientIP()
	ip := net.ParseIP(clientIP)
	var ipVer string
	var isPriv bool
	if ip != nil {
		if ip.To4() != nil {
			ipVer = "IPv4"
		} else {
			ipVer = "IPv6"
		}
		isPriv = ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast()
	}

	remoteAddr := req.RemoteAddr
	remotePort := ""
	if _, p, err := net.SplitHostPort(remoteAddr); err == nil {
		remotePort = p
	}

	d := EnvPageData{
		ClientIP:       clientIP,
		IPVersion:      ipVer,
		IsPrivate:      isPriv,
		RemoteAddr:     remoteAddr,
		RemotePort:     remotePort,
		Method:         req.Method,
		URL:            req.URL.RequestURI(),
		Host:           req.Host,
		Protocol:       req.Proto,
		SchemeHTTPS:    req.TLS != nil,
		UserAgent:      req.UserAgent(),
		Referer:        req.Referer(),
		Origin:         req.Header.Get("Origin"),
		AcceptLang:     req.Header.Get("Accept-Language"),
		Accept:         req.Header.Get("Accept"),
		AcceptEncoding: req.Header.Get("Accept-Encoding"),

		UAClientBrand:    req.Header.Get("Sec-Ch-Ua"),
		UAClientPlatform: strings.Trim(req.Header.Get("Sec-Ch-Ua-Platform"), `"`),
		UAClientMobile:   req.Header.Get("Sec-Ch-Ua-Mobile"),

		ForwardedFor:   req.Header.Get("X-Forwarded-For"),
		RealIP:         req.Header.Get("X-Real-IP"),
		ForwardedProto: req.Header.Get("X-Forwarded-Proto"),
		Via:            req.Header.Get("Via"),

		SecFetchSite: req.Header.Get("Sec-Fetch-Site"),
		SecFetchMode: req.Header.Get("Sec-Fetch-Mode"),
		SecFetchDest: req.Header.Get("Sec-Fetch-Dest"),

		CheckedAt:  now,
		ServerTime: now,
		Headers:    collectSafeHeaders(req.Header),
	}

	if req.TLS != nil {
		d.TLSVersion = tlsVersionLabel(req.TLS.Version)
		d.TLSCipher = tls.CipherSuiteName(req.TLS.CipherSuite)
		d.TLSServerName = req.TLS.ServerName
		d.ALPN = req.TLS.NegotiatedProtocol
	}

	return d
}

func tlsVersionLabel(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

func collectSafeHeaders(h map[string][]string) map[string]string {
	// 白名单：只回显诊断相关头；Authorization / Cookie 等敏感头永不显示
	keep := map[string]bool{
		"User-Agent":                  true,
		"Accept":                      true,
		"Accept-Language":             true,
		"Accept-Encoding":             true,
		"Referer":                     true,
		"Origin":                      true,
		"Sec-Ch-Ua":                   true,
		"Sec-Ch-Ua-Mobile":            true,
		"Sec-Ch-Ua-Platform":          true,
		"Sec-Ch-Ua-Platform-Version":  true,
		"Sec-Ch-Ua-Arch":              true,
		"Sec-Ch-Ua-Bitness":           true,
		"Sec-Ch-Ua-Model":             true,
		"Sec-Ch-Ua-Full-Version-List": true,
		"Sec-Fetch-Site":              true,
		"Sec-Fetch-Mode":              true,
		"Sec-Fetch-Dest":              true,
		"Sec-Fetch-User":              true,
		"Dnt":                         true,
		"Upgrade-Insecure-Requests":   true,
		"Priority":                    true,
		"X-Forwarded-For":             true,
		"X-Real-Ip":                   true,
		"X-Forwarded-Proto":           true,
		"X-Forwarded-Host":            true,
		"Cf-Connecting-Ip":            true,
		"Cf-Ipcountry":                true,
		"Cf-Ray":                      true,
		"Cdn-Loop":                    true,
		"Via":                         true,
		"Connection":                  true,
		"Te":                          true,
	}
	out := map[string]string{}
	for k, v := range h {
		if !keep[k] || len(v) == 0 {
			continue
		}
		out[k] = strings.Join(v, ", ")
	}
	return out
}

type envView struct {
	Data         EnvPageData
	RequestID    string
	TraceID      string
	Year         int
	CheckedHuman string
	ServerTimeMs int64 // Unix ms — 给客户端做 clock-skew 对比
	UABrief      string
	UAVendor     string
	UAOS         string
	HeaderKeys   []string
}

func (v *envView) Derive() {
	v.CheckedHuman = v.Data.CheckedAt.Local().Format("2006-01-02 15:04:05 MST")
	v.ServerTimeMs = v.Data.ServerTime.UnixMilli()

	// 服务端简易 UA 解析（JS 侧会再精化）
	ua := v.Data.UserAgent
	v.UABrief, v.UAVendor, v.UAOS = parseUABrief(ua, v.Data.UAClientBrand, v.Data.UAClientPlatform)

	for k := range v.Data.Headers {
		v.HeaderKeys = append(v.HeaderKeys, k)
	}
	sort.Strings(v.HeaderKeys)
}

// parseUABrief 返回服务端视角的 (浏览器+版本, 渲染引擎, OS)。
// 优先使用 UA-CH，其次 UA 字符串。
func parseUABrief(ua, chBrand, chPlatform string) (string, string, string) {
	brief := "Unknown"
	engine := "Unknown"
	osg := chPlatform
	// 先看 Sec-Ch-Ua 作为权威 brand 来源
	if chBrand != "" {
		brands := strings.Split(chBrand, ",")
		for _, b := range brands {
			b = strings.TrimSpace(b)
			if strings.Contains(b, "Not") { // "Not.A/Brand"、"Not)A;Brand" 等占位
				continue
			}
			// "Google Chrome";v="123"
			parts := strings.SplitN(b, ";", 2)
			name := strings.Trim(parts[0], `" `)
			ver := ""
			if len(parts) > 1 {
				vp := strings.TrimPrefix(strings.TrimSpace(parts[1]), "v=")
				ver = strings.Trim(vp, `"`)
			}
			if name != "" && name != "Chromium" {
				if ver != "" {
					brief = name + " " + ver
				} else {
					brief = name
				}
			}
			switch {
			case strings.Contains(name, "Chrome"), strings.Contains(name, "Edge"), strings.Contains(name, "Opera"), strings.Contains(name, "Brave"), strings.Contains(name, "Chromium"):
				engine = "Blink"
			case strings.Contains(name, "Firefox"):
				engine = "Gecko"
			case strings.Contains(name, "Safari"):
				engine = "WebKit"
			}
		}
	}
	// 回退：用 UA 字符串
	if brief == "Unknown" {
		low := strings.ToLower(ua)
		switch {
		case strings.Contains(low, "edg/"):
			brief = "Edge"
			engine = "Blink"
		case strings.Contains(low, "opr/"), strings.Contains(low, "opera"):
			brief = "Opera"
			engine = "Blink"
		case strings.Contains(low, "firefox/"):
			brief = "Firefox"
			engine = "Gecko"
		case strings.Contains(low, "chrome/"):
			brief = "Chrome"
			engine = "Blink"
		case strings.Contains(low, "safari/"):
			brief = "Safari"
			engine = "WebKit"
		case strings.Contains(low, "curl/"):
			brief = "curl"
			engine = "—"
		}
	}
	if osg == "" {
		low := strings.ToLower(ua)
		switch {
		case strings.Contains(low, "windows"):
			osg = "Windows"
		case strings.Contains(low, "mac os"), strings.Contains(low, "macintosh"):
			osg = "macOS"
		case strings.Contains(low, "iphone"), strings.Contains(low, "ipad"), strings.Contains(low, "ipod"):
			osg = "iOS"
		case strings.Contains(low, "android"):
			osg = "Android"
		case strings.Contains(low, "linux"):
			osg = "Linux"
		case strings.Contains(low, "cros"):
			osg = "Chrome OS"
		}
	}
	return brief, engine, osg
}

// ─── 模板 ───

var envPageTemplate = template.Must(template.New("env").Funcs(template.FuncMap{
	"orDash": func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "—"
		}
		return s
	},
	"yesNo": func(b bool) string {
		if b {
			return "是"
		}
		return "否"
	},
}).Parse(envPageSource))

const (
	iconGlobeEnv    = svgHead + `<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20"/><path d="M12 2a14.5 14.5 0 0 1 0 20"/><path d="M2 12h20"/></svg>`
	iconMonitorEnv  = svgHead + `<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/></svg>`
	iconCpuEnv      = svgHead + `<rect width="16" height="16" x="4" y="4" rx="2"/><rect width="6" height="6" x="9" y="9"/><path d="M15 2v2"/><path d="M15 20v2"/><path d="M2 15h2"/><path d="M2 9h2"/><path d="M20 15h2"/><path d="M20 9h2"/><path d="M9 2v2"/><path d="M9 20v2"/></svg>`
	iconWifiEnv     = svgHead + `<path d="M5 13a10 10 0 0 1 14 0"/><path d="M8.5 16.5a5 5 0 0 1 7 0"/><path d="M2 8.82a15 15 0 0 1 20 0"/><line x1="12" x2="12.01" y1="20" y2="20"/></svg>`
	iconLangEnv     = svgHead + `<path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/></svg>`
	iconClockEnv    = svgHead + `<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>`
	iconDatabaseEnv = svgHead + `<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/></svg>`
	iconLockEnv     = svgHead + `<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>`
	iconKeyEnv      = svgHead + `<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/></svg>`
	iconHashEnv     = svgHead + `<line x1="4" x2="20" y1="9" y2="9"/><line x1="4" x2="20" y1="15" y2="15"/><line x1="10" x2="8" y1="3" y2="21"/><line x1="16" x2="14" y1="3" y2="21"/></svg>`
	iconLayersEnv   = svgHead + `<path d="M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12"/><path d="M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17"/></svg>`
	iconAccessEnv   = svgHead + `<circle cx="12" cy="12" r="10"/><path d="m16 12-4-4-4 4"/><path d="M12 16V8"/></svg>`
	iconFlaskEnv    = svgHead + `<path d="M10 2v7.31"/><path d="M14 9.3V2"/><path d="M8.5 2h7"/><path d="M14 9.3a6.5 6.5 0 1 1-4 0"/><path d="M5.52 16h12.96"/></svg>`
	iconCopyEnv     = svgHead + `<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>`
	iconMediaEnv    = svgHead + `<rect width="20" height="14" x="2" y="3" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/><path d="m10 8 5 3-5 3z"/></svg>`
	iconImageEnv    = svgHead + `<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/></svg>`
	iconTypeEnv     = svgHead + `<polyline points="4 7 4 4 20 4 20 7"/><line x1="9" x2="15" y1="20" y2="20"/><line x1="12" x2="12" y1="4" y2="20"/></svg>`
	iconGpuEnv      = svgHead + `<rect width="20" height="14" x="2" y="4" rx="2"/><circle cx="8" cy="11" r="2.5"/><circle cx="16" cy="11" r="2.5"/><path d="M2 18h20"/><path d="M6 18v2"/><path d="M18 18v2"/></svg>`
	iconShieldEnvX  = svgHead + `<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/></svg>`
)

const envPageSource = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex, nofollow">
  <meta name="description" content="Aegis 环境查看 — 客户端完整诊断">
  <meta name="theme-color" content="#f5f4ed" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#141413" media="(prefers-color-scheme: dark)">
  <title>环境查看 — Aegis</title>
  <style>
    :root {
      --bg:#f5f4ed; --bg-soft:#eee9de; --bg-ivory:#faf9f5;
      --text-primary:#141413; --text-body:#3d3d3a;
      --text-secondary:#5e5d59; --text-tertiary:#87867f;
      --rule:#e8e6dc; --rule-soft:#f0eee6;
      --brand:#c96442; --brand-hover:#b55230;
      --warm-sand:#e8e6dc; --ring:#d1cfc5;
      --code-bg:#141413; --code-text:#f5f4ed;
      --good:#6f9a5f; --good-soft:#e6efdf;
      --warn:#c38435; --warn-soft:#f4e6d0;
      --bad:#b53333; --bad-soft:#f3dcdc;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg:#141413; --bg-soft:#1c1c1a; --bg-ivory:#1c1c1a;
        --text-primary:#f5f4ed; --text-body:#e6e4d8;
        --text-secondary:#b0aea5; --text-tertiary:#87867f;
        --rule:#30302e; --rule-soft:#242422;
        --brand:#d97757; --brand-hover:#e78a65;
        --warm-sand:#30302e; --ring:#3d3d3a;
        --code-bg:#0e0e0d; --code-text:#e6e4d8;
        --good:#8fb97f; --good-soft:#2a3527;
        --warn:#d99f56; --warn-soft:#3a2e1d;
        --bad:#d06a6a; --bad-soft:#3a2424;
      }
    }
    *, *::before, *::after { box-sizing: border-box; }
    html, body { min-height: 100%; }
    body {
      margin: 0; background: var(--bg); color: var(--text-primary);
      font-family:"Anthropic Sans","Inter","PingFang SC","Microsoft YaHei UI",system-ui,sans-serif;
      font-size: 16px; line-height: 1.6; -webkit-font-smoothing: antialiased;
    }
    .container { max-width: 1120px; margin: 0 auto; padding: 0 40px; }

    /* Nav */
    .nav { padding: 28px 0; display: flex; align-items: center; justify-content: space-between; gap: 24px; }
    .nav-brand {
      display: inline-flex; align-items: center; gap: 10px;
      font-family:"Anthropic Serif",Georgia,serif;
      font-size: 22px; font-weight: 500; letter-spacing: -0.01em;
      color: var(--text-primary); text-decoration: none; line-height: 1;
    }
    .nav-brand .dot { width: 10px; height: 10px; background: var(--brand); border-radius: 50%; transform: translateY(-1px); }
    .nav-links { display: inline-flex; align-items: center; gap: 28px; }
    .nav-link {
      font-size: 14.5px; color: var(--text-secondary); text-decoration: none;
      border-bottom: 1px solid transparent; padding-bottom: 1px;
      transition: color 140ms linear, border-color 140ms linear;
    }
    .nav-link:hover { color: var(--text-primary); border-bottom-color: var(--brand); }
    .nav-link.active { color: var(--text-primary); border-bottom-color: var(--brand); }
    .nav-cta {
      display: inline-flex; align-items: center; gap: 6px;
      padding: 8px 14px; font-size: 14px; font-weight: 500;
      color: #faf9f5; background: var(--brand); border-radius: 10px;
      text-decoration: none; box-shadow: 0 0 0 1px var(--brand);
      transition: background 140ms linear, box-shadow 140ms linear;
    }
    .nav-cta:hover { background: var(--brand-hover); box-shadow: 0 0 0 1px var(--brand-hover); }

    /* Hero */
    .hero { padding: 48px 0 32px; max-width: 860px; }
    .hero-kicker {
      display: inline-flex; align-items: center; gap: 8px;
      font-family:"Anthropic Serif",Georgia,serif; font-style: italic;
      font-size: 15px; color: var(--brand);
    }
    .hero-kicker::before { content:""; width: 6px; height: 6px; background: var(--brand); border-radius: 50%; }
    .hero-rule { width: 48px; height: 1px; margin: 22px 0 28px; background: var(--brand); border: 0; }
    .hero h1 {
      margin: 0; font-family:"Anthropic Serif",Georgia,serif;
      font-size: clamp(42px, 5.8vw, 64px);
      font-weight: 500; line-height: 1.08; letter-spacing: -0.02em;
    }
    .hero-lede { margin: 22px 0 0; max-width: 38em; font-size: 18px; line-height: 1.62; color: var(--text-body); }

    /* Summary tiles */
    .summary { display: grid; grid-template-columns: repeat(4, minmax(0,1fr)); gap: 16px; margin-top: 32px; }
    @media (max-width:900px) { .summary { grid-template-columns: repeat(2,1fr); } }
    .sum {
      padding: 20px 22px 18px; background: var(--bg-ivory); border-radius: 14px;
      box-shadow: 0 0 0 1px var(--rule-soft);
    }
    .sum-label { font-family:"Anthropic Serif",Georgia,serif; font-style: italic; font-size: 13px; color: var(--text-secondary); }
    .sum-value {
      margin-top: 6px;
      font-family:"Anthropic Mono","SF Mono",Consolas,monospace;
      font-size: 17px; font-weight: 500; color: var(--text-primary);
      word-break: break-all;
    }
    .sum-hint { margin-top: 4px; font-size: 12.5px; color: var(--text-tertiary); }

    /* Section */
    .section { padding: 52px 0; border-top: 1px solid var(--rule); }
    .section-head { display: flex; align-items: center; gap: 16px; margin-bottom: 28px; }
    .section-icon {
      flex: 0 0 40px;
      display: inline-flex; align-items: center; justify-content: center;
      width: 40px; height: 40px; color: var(--brand);
      background: var(--bg-soft); border-radius: 10px;
      box-shadow: 0 0 0 1px var(--rule);
    }
    .section-icon svg { width: 20px; height: 20px; }
    .section-title-group { flex: 1; min-width: 0; }
    .section-kicker { font-family:"Anthropic Serif",Georgia,serif; font-style: italic; font-size: 13px; color: var(--brand); }
    .section-title {
      margin: 2px 0 0;
      font-family:"Anthropic Serif",Georgia,serif;
      font-size: clamp(26px, 3.4vw, 34px); font-weight: 500;
      line-height: 1.14; letter-spacing: -0.016em;
    }
    .section-lede { margin: 4px 0 0; font-size: 14.5px; color: var(--text-secondary); }

    .grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 40px; }
    @media (max-width: 800px) { .grid { grid-template-columns: 1fr; } }
    .row {
      display: grid; grid-template-columns: 170px 1fr; gap: 14px;
      padding: 14px 0; border-top: 1px solid var(--rule-soft);
      align-items: baseline;
    }
    .row:first-child, .grid > .row:nth-child(2) { border-top: 0; }
    @media (max-width: 800px) {
      .grid > .row:nth-child(2) { border-top: 1px solid var(--rule-soft); }
    }
    .row-label { font-family:"Anthropic Serif",Georgia,serif; font-style: italic; font-size: 13.5px; color: var(--text-secondary); }
    .row-value {
      min-width: 0;
      font-family:"Anthropic Mono","SF Mono",Consolas,monospace;
      font-size: 13.5px; color: var(--text-body);
      word-break: break-all;
    }
    .row-value.muted { color: var(--text-tertiary); font-style: italic; font-family:"Anthropic Serif",Georgia,serif; }

    .features { display: flex; flex-wrap: wrap; gap: 10px 12px; }
    .pill {
      display: inline-flex; align-items: center; gap: 8px;
      padding: 6px 12px; border-radius: 999px;
      font-family:"Anthropic Mono","SF Mono",Consolas,monospace;
      font-size: 12px; font-weight: 500;
      background: var(--bg-soft); color: var(--text-tertiary);
      box-shadow: inset 0 0 0 1px var(--rule);
    }
    .pill .d { width: 7px; height: 7px; border-radius: 50%; background: var(--text-tertiary); }
    .pill.good { color: var(--good); background: var(--good-soft); box-shadow: none; }
    .pill.good .d { background: var(--good); }
    .pill.warn { color: var(--warn); background: var(--warn-soft); box-shadow: none; }
    .pill.warn .d { background: var(--warn); }
    .pill.bad { color: var(--bad); background: var(--bad-soft); box-shadow: none; }
    .pill.bad .d { background: var(--bad); }

    .ua-block {
      font-family:"Anthropic Mono","SF Mono",Consolas,monospace;
      font-size: 13px; line-height: 1.65;
      color: var(--code-text); background: var(--code-bg);
      padding: 14px 18px; border-radius: 12px;
      overflow-x: auto; white-space: pre-wrap; word-break: break-all;
      margin: 10px 0 0;
    }
    .copy-btn {
      display: inline-flex; align-items: center; gap: 6px;
      margin-top: 10px; padding: 6px 12px;
      font-family: inherit; font-size: 12.5px; font-weight: 500;
      color: var(--text-secondary); background: var(--bg-soft);
      border: 0; border-radius: 8px; cursor: pointer;
      box-shadow: inset 0 0 0 1px var(--rule);
      transition: color 140ms linear, background 140ms linear;
    }
    .copy-btn:hover { color: var(--text-primary); background: var(--warm-sand); }
    .copy-btn svg { width: 14px; height: 14px; }

    .headers { display: grid; gap: 0; font-family:"Anthropic Mono","SF Mono",Consolas,monospace; font-size: 13px; }
    .hdr { display: grid; grid-template-columns: 230px 1fr; gap: 18px; padding: 10px 0; border-top: 1px solid var(--rule-soft); align-items: baseline; }
    .hdr:first-child { border-top: 0; }
    .hdr-name { color: var(--text-secondary); }
    .hdr-value { color: var(--text-body); word-break: break-all; }
    @media (max-width: 680px) {
      .hdr { grid-template-columns: 1fr; gap: 2px; }
    }

    footer.foot { padding: 36px 0 48px; border-top: 1px solid var(--rule); color: var(--text-tertiary); }
    .foot-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 10px 16px; font-family:"Anthropic Serif",Georgia,serif; font-style: italic; font-size: 13px; }
    .foot-meta .sep { color: var(--rule); font-style: normal; }
    .foot-meta .grow { flex: 1; }
    .foot-meta code {
      font-family:"Anthropic Mono","SF Mono",Consolas,monospace;
      font-style: normal; font-size: 12px;
      padding: 2px 7px; margin-left: 4px;
      background: var(--bg-soft); color: var(--text-body);
      border-radius: 6px;
      box-shadow: inset 0 0 0 1px var(--rule);
    }

    @media (max-width: 680px) {
      .container { padding: 0 24px; }
      .nav-link-hide { display: none; }
      .hero { padding: 32px 0 28px; }
      .section { padding: 40px 0; }
      .row { grid-template-columns: 1fr; gap: 2px; padding: 10px 0; }
      .row-label { margin-top: 6px; }
    }
    ` + BrandLogoCSS + `
  </style>
</head>
<body>
  <div class="container">
    <!-- Nav -->
    <nav class="nav" aria-label="主导航">
      ` + BrandLogoHTML + `
      <div class="nav-links">
        <a class="nav-link active nav-link-hide" href="/cons-env">环境</a>
        <a class="nav-link nav-link-hide" href="/announcements">公告</a>
        <a class="nav-link nav-link-hide" href="/status">状态</a>
        <a class="nav-link nav-link-hide" href="/docs">文档</a>
        <a class="nav-link" href="/console">控制台</a>
        <a class="nav-cta" href="/login">进入</a>
      </div>
    </nav>

    <!-- Hero -->
    <section class="hero">
      <span class="hero-kicker">Environment Inspector</span>
      <hr class="hero-rule" aria-hidden="true">
      <h1>环境查看</h1>
      <p class="hero-lede">
        对当前访问客户端的完整快照：浏览器、设备、屏幕、GPU、网络、编解码器、存储、
        权限与功能支持；所有浏览器数据仅在本地生成，不会回传。
      </p>

      <div class="summary">
        <div class="sum">
          <div class="sum-label">Client IP</div>
          <div class="sum-value">{{orDash .Data.ClientIP}}</div>
          <div class="sum-hint">{{orDash .Data.IPVersion}}{{if .Data.IsPrivate}} · private{{end}}</div>
        </div>
        <div class="sum">
          <div class="sum-label">Browser</div>
          <div class="sum-value" data-env="browserBrief">{{.UABrief}}</div>
          <div class="sum-hint" data-env="engine">{{.UAVendor}}</div>
        </div>
        <div class="sum">
          <div class="sum-label">Viewport</div>
          <div class="sum-value" data-env="viewport">—</div>
          <div class="sum-hint" data-env="viewportHint">像素比 —</div>
        </div>
        <div class="sum">
          <div class="sum-label">Timezone</div>
          <div class="sum-value" data-env="tz">—</div>
          <div class="sum-hint" data-env="tzOffset">—</div>
        </div>
      </div>
    </section>

    <!-- Session -->
    <section class="section" aria-labelledby="sec-session">
      <header class="section-head">
        <span class="section-icon">` + iconHashEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Session</div>
          <h2 class="section-title" id="sec-session">当前请求</h2>
          <p class="section-lede">本次请求在 Aegis 网关留下的追踪信息。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Method</div><div class="row-value">{{.Data.Method}}</div></div>
        <div class="row"><div class="row-label">URL</div><div class="row-value">{{orDash .Data.URL}}</div></div>
        <div class="row"><div class="row-label">Host</div><div class="row-value">{{orDash .Data.Host}}</div></div>
        <div class="row"><div class="row-label">Protocol</div><div class="row-value">{{orDash .Data.Protocol}}</div></div>
        <div class="row"><div class="row-label">Scheme</div><div class="row-value">{{if .Data.SchemeHTTPS}}HTTPS{{else}}HTTP{{end}}</div></div>
        <div class="row"><div class="row-label">Remote Port</div><div class="row-value">{{orDash .Data.RemotePort}}</div></div>
        <div class="row"><div class="row-label">Server Time</div><div class="row-value">{{.CheckedHuman}}</div></div>
        <div class="row"><div class="row-label">Client Time</div><div class="row-value" data-env="clientTime">—</div></div>
        <div class="row"><div class="row-label">Clock Skew</div><div class="row-value" data-env="clockSkew">—</div></div>
        <div class="row"><div class="row-label">Request ID</div><div class="row-value">{{orDash .RequestID}}</div></div>
        <div class="row"><div class="row-label">Trace ID</div><div class="row-value">{{orDash .TraceID}}</div></div>
      </div>
    </section>

    <!-- Network -->
    <section class="section" aria-labelledby="sec-net">
      <header class="section-head">
        <span class="section-icon">` + iconWifiEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Network</div>
          <h2 class="section-title" id="sec-net">网络与来源</h2>
          <p class="section-lede">服务端看到的客户端 IP、代理与链路特征。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Client IP</div><div class="row-value">{{orDash .Data.ClientIP}}{{if .Data.IPVersion}} · {{.Data.IPVersion}}{{end}}</div></div>
        <div class="row"><div class="row-label">Is Private</div><div class="row-value">{{yesNo .Data.IsPrivate}}</div></div>
        <div class="row"><div class="row-label">Remote Address</div><div class="row-value">{{orDash .Data.RemoteAddr}}</div></div>
        <div class="row"><div class="row-label">X-Real-IP</div><div class="row-value">{{orDash .Data.RealIP}}</div></div>
        <div class="row"><div class="row-label">X-Forwarded-For</div><div class="row-value">{{orDash .Data.ForwardedFor}}</div></div>
        <div class="row"><div class="row-label">X-Forwarded-Proto</div><div class="row-value">{{orDash .Data.ForwardedProto}}</div></div>
        <div class="row"><div class="row-label">Via</div><div class="row-value">{{orDash .Data.Via}}</div></div>
        <div class="row"><div class="row-label">Country</div><div class="row-value">{{orDash .Data.Country}}</div></div>
        <div class="row"><div class="row-label">Region / City</div><div class="row-value">{{orDash .Data.Region}}{{if .Data.City}} · {{.Data.City}}{{end}}</div></div>
        <div class="row"><div class="row-label">ASN / ISP</div><div class="row-value">{{orDash .Data.ASN}}{{if .Data.ISP}} · {{.Data.ISP}}{{end}}</div></div>
        <div class="row"><div class="row-label">Connection Type</div><div class="row-value" data-env="connectionType">—</div></div>
        <div class="row"><div class="row-label">Effective Type</div><div class="row-value" data-env="effectiveType">—</div></div>
        <div class="row"><div class="row-label">Downlink</div><div class="row-value" data-env="downlink">—</div></div>
        <div class="row"><div class="row-label">RTT</div><div class="row-value" data-env="rtt">—</div></div>
        <div class="row"><div class="row-label">Online</div><div class="row-value" data-env="online">—</div></div>
        <div class="row"><div class="row-label">Save-Data</div><div class="row-value" data-env="saveData">—</div></div>
      </div>
    </section>

    <!-- Transport -->
    {{if .Data.TLSVersion}}
    <section class="section" aria-labelledby="sec-tls">
      <header class="section-head">
        <span class="section-icon">` + iconLockEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Transport</div>
          <h2 class="section-title" id="sec-tls">传输层加密</h2>
          <p class="section-lede">TLS 协议、加密套件与 ALPN 协商结果。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">TLS Version</div><div class="row-value">{{.Data.TLSVersion}}</div></div>
        <div class="row"><div class="row-label">Cipher Suite</div><div class="row-value">{{orDash .Data.TLSCipher}}</div></div>
        <div class="row"><div class="row-label">SNI ServerName</div><div class="row-value">{{orDash .Data.TLSServerName}}</div></div>
        <div class="row"><div class="row-label">ALPN</div><div class="row-value">{{orDash .Data.ALPN}}</div></div>
      </div>
    </section>
    {{end}}

    <!-- Browser -->
    <section class="section" aria-labelledby="sec-ua">
      <header class="section-head">
        <span class="section-icon">` + iconGlobeEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Browser</div>
          <h2 class="section-title" id="sec-ua">浏览器</h2>
          <p class="section-lede">UA-Client Hints 与 UA 字符串解析结果。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Product</div><div class="row-value" data-env="browserBrief">{{.UABrief}}</div></div>
        <div class="row"><div class="row-label">Engine</div><div class="row-value" data-env="engine">{{.UAVendor}}</div></div>
        <div class="row"><div class="row-label">Vendor</div><div class="row-value" data-env="vendor">—</div></div>
        <div class="row"><div class="row-label">Full Version</div><div class="row-value" data-env="fullVersion">—</div></div>
        <div class="row"><div class="row-label">Brands (UA-CH)</div><div class="row-value">{{orDash .Data.UAClientBrand}}</div></div>
        <div class="row"><div class="row-label">Mobile Hint</div><div class="row-value">{{orDash .Data.UAClientMobile}}</div></div>
        <div class="row"><div class="row-label">Cookies Enabled</div><div class="row-value" data-env="cookieEnabled">—</div></div>
        <div class="row"><div class="row-label">Do Not Track</div><div class="row-value" data-env="dnt">—</div></div>
        <div class="row"><div class="row-label">Global Privacy</div><div class="row-value" data-env="gpc">—</div></div>
        <div class="row"><div class="row-label">PDF Viewer</div><div class="row-value" data-env="pdfViewer">—</div></div>
        <div class="row"><div class="row-label">WebDriver</div><div class="row-value" data-env="webdriver">—</div></div>
        <div class="row"><div class="row-label">Brave / Headless</div><div class="row-value" data-env="specialMode">—</div></div>
      </div>
      <div class="ua-block" data-env="userAgentFull">{{orDash .Data.UserAgent}}</div>
      <button class="copy-btn" type="button" data-copy="ua"><span>` + iconCopyEnv + `</span><span>复制 User-Agent</span></button>
    </section>

    <!-- System -->
    <section class="section" aria-labelledby="sec-os">
      <header class="section-head">
        <span class="section-icon">` + iconLayersEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">System</div>
          <h2 class="section-title" id="sec-os">操作系统 / 平台</h2>
          <p class="section-lede">通过 UA-CH high entropy 与 navigator.platform 联合推断。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Platform</div><div class="row-value" data-env="platform">{{orDash .UAOS}}</div></div>
        <div class="row"><div class="row-label">OS Version</div><div class="row-value" data-env="osVersion">—</div></div>
        <div class="row"><div class="row-label">Architecture</div><div class="row-value" data-env="arch">—</div></div>
        <div class="row"><div class="row-label">Bitness</div><div class="row-value" data-env="bitness">—</div></div>
        <div class="row"><div class="row-label">Device Model</div><div class="row-value" data-env="deviceModel">—</div></div>
        <div class="row"><div class="row-label">Mobile</div><div class="row-value" data-env="mobile">—</div></div>
        <div class="row"><div class="row-label">Touch Device</div><div class="row-value" data-env="touchDevice">—</div></div>
        <div class="row"><div class="row-label">Form Factor</div><div class="row-value" data-env="formFactor">—</div></div>
      </div>
    </section>

    <!-- Display -->
    <section class="section" aria-labelledby="sec-display">
      <header class="section-head">
        <span class="section-icon">` + iconMonitorEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Display</div>
          <h2 class="section-title" id="sec-display">显示与窗口</h2>
          <p class="section-lede">屏幕、视口、色深、多显示器检测。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Screen</div><div class="row-value" data-env="screen">—</div></div>
        <div class="row"><div class="row-label">Available</div><div class="row-value" data-env="screenAvail">—</div></div>
        <div class="row"><div class="row-label">Viewport</div><div class="row-value" data-env="viewport">—</div></div>
        <div class="row"><div class="row-label">Device Pixel Ratio</div><div class="row-value" data-env="dpr">—</div></div>
        <div class="row"><div class="row-label">Color Depth</div><div class="row-value" data-env="colorDepth">—</div></div>
        <div class="row"><div class="row-label">Orientation</div><div class="row-value" data-env="orientation">—</div></div>
        <div class="row"><div class="row-label">Color Scheme</div><div class="row-value" data-env="colorScheme">—</div></div>
        <div class="row"><div class="row-label">Contrast</div><div class="row-value" data-env="contrast">—</div></div>
        <div class="row"><div class="row-label">Reduced Motion</div><div class="row-value" data-env="reducedMotion">—</div></div>
        <div class="row"><div class="row-label">Color Gamut</div><div class="row-value" data-env="colorGamut">—</div></div>
        <div class="row"><div class="row-label">Dynamic Range</div><div class="row-value" data-env="hdr">—</div></div>
        <div class="row"><div class="row-label">Refresh Rate</div><div class="row-value" data-env="refreshRate">—</div></div>
        <div class="row"><div class="row-label">Multi-Display</div><div class="row-value" data-env="multiScreen">—</div></div>
      </div>
    </section>

    <!-- GPU -->
    <section class="section" aria-labelledby="sec-gpu">
      <header class="section-head">
        <span class="section-icon">` + iconGpuEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Graphics</div>
          <h2 class="section-title" id="sec-gpu">图形与 GPU</h2>
          <p class="section-lede">WebGL 渲染器、WebGPU 适配器与能力。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">WebGL</div><div class="row-value" data-env="webgl">—</div></div>
        <div class="row"><div class="row-label">WebGL2</div><div class="row-value" data-env="webgl2">—</div></div>
        <div class="row"><div class="row-label">Renderer</div><div class="row-value" data-env="webglRenderer">—</div></div>
        <div class="row"><div class="row-label">Vendor</div><div class="row-value" data-env="webglVendor">—</div></div>
        <div class="row"><div class="row-label">Max Texture</div><div class="row-value" data-env="webglMaxTex">—</div></div>
        <div class="row"><div class="row-label">Shading Lang</div><div class="row-value" data-env="webglGLSL">—</div></div>
        <div class="row"><div class="row-label">WebGPU</div><div class="row-value" data-env="webgpu">—</div></div>
        <div class="row"><div class="row-label">WebGPU Adapter</div><div class="row-value" data-env="webgpuAdapter">—</div></div>
      </div>
    </section>

    <!-- Hardware -->
    <section class="section" aria-labelledby="sec-hw">
      <header class="section-head">
        <span class="section-icon">` + iconCpuEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Hardware</div>
          <h2 class="section-title" id="sec-hw">硬件</h2>
          <p class="section-lede">CPU、内存、输入、电池。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Logical CPU</div><div class="row-value" data-env="hwCores">—</div></div>
        <div class="row"><div class="row-label">Device Memory</div><div class="row-value" data-env="deviceMemory">—</div></div>
        <div class="row"><div class="row-label">Heap Limit</div><div class="row-value" data-env="heapLimit">—</div></div>
        <div class="row"><div class="row-label">Touch Points</div><div class="row-value" data-env="touchPoints">—</div></div>
        <div class="row"><div class="row-label">Pointer</div><div class="row-value" data-env="pointer">—</div></div>
        <div class="row"><div class="row-label">Hover</div><div class="row-value" data-env="hover">—</div></div>
        <div class="row"><div class="row-label">Keyboard Layout</div><div class="row-value" data-env="keyboardLayout">—</div></div>
        <div class="row"><div class="row-label">Gamepads</div><div class="row-value" data-env="gamepads">—</div></div>
        <div class="row"><div class="row-label">Battery</div><div class="row-value" data-env="battery">—</div></div>
        <div class="row"><div class="row-label">Camera</div><div class="row-value" data-env="devCamera">—</div></div>
        <div class="row"><div class="row-label">Microphone</div><div class="row-value" data-env="devMic">—</div></div>
        <div class="row"><div class="row-label">Speaker</div><div class="row-value" data-env="devSpeaker">—</div></div>
      </div>
    </section>

    <!-- Locale -->
    <section class="section" aria-labelledby="sec-lang">
      <header class="section-head">
        <span class="section-icon">` + iconLangEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Locale &amp; Time</div>
          <h2 class="section-title" id="sec-lang">语言与时区</h2>
          <p class="section-lede">Accept-Language、Intl 与时区信息。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Browser Lang</div><div class="row-value" data-env="lang">—</div></div>
        <div class="row"><div class="row-label">Languages</div><div class="row-value" data-env="languages">—</div></div>
        <div class="row"><div class="row-label">Accept-Language</div><div class="row-value">{{orDash .Data.AcceptLang}}</div></div>
        <div class="row"><div class="row-label">Timezone</div><div class="row-value" data-env="tz">—</div></div>
        <div class="row"><div class="row-label">UTC Offset</div><div class="row-value" data-env="tzOffset">—</div></div>
        <div class="row"><div class="row-label">Calendar</div><div class="row-value" data-env="calendar">—</div></div>
        <div class="row"><div class="row-label">Numbering</div><div class="row-value" data-env="numberingSystem">—</div></div>
        <div class="row"><div class="row-label">Hour Cycle</div><div class="row-value" data-env="hourCycle">—</div></div>
      </div>
    </section>

    <!-- Media & Codecs -->
    <section class="section" aria-labelledby="sec-media">
      <header class="section-head">
        <span class="section-icon">` + iconMediaEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Media &amp; Codecs</div>
          <h2 class="section-title" id="sec-media">音视频编解码</h2>
          <p class="section-lede">浏览器对主流视频、音频编解码器的可播放报告。</p>
        </div>
      </header>
      <div class="features" data-env-codecs></div>
    </section>

    <!-- Image formats -->
    <section class="section" aria-labelledby="sec-img">
      <header class="section-head">
        <span class="section-icon">` + iconImageEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Image Formats</div>
          <h2 class="section-title" id="sec-img">图片格式</h2>
          <p class="section-lede">现代图片格式解码支持。</p>
        </div>
      </header>
      <div class="features" data-env-images></div>
    </section>

    <!-- Fonts -->
    <section class="section" aria-labelledby="sec-fonts">
      <header class="section-head">
        <span class="section-icon">` + iconTypeEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Fonts</div>
          <h2 class="section-title" id="sec-fonts">字体探测</h2>
          <p class="section-lede">对常见字体是否已安装进行测量（仅 CSS 层探测，非枚举）。</p>
        </div>
      </header>
      <div class="features" data-env-fonts></div>
    </section>

    <!-- Storage -->
    <section class="section" aria-labelledby="sec-storage">
      <header class="section-head">
        <span class="section-icon">` + iconDatabaseEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Storage</div>
          <h2 class="section-title" id="sec-storage">客户端存储</h2>
          <p class="section-lede">浏览器本地持久化能力的可用性与配额。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">localStorage</div><div class="row-value" data-env="localStorage">—</div></div>
        <div class="row"><div class="row-label">sessionStorage</div><div class="row-value" data-env="sessionStorage">—</div></div>
        <div class="row"><div class="row-label">IndexedDB</div><div class="row-value" data-env="indexedDb">—</div></div>
        <div class="row"><div class="row-label">Cookies</div><div class="row-value" data-env="cookieStatus">—</div></div>
        <div class="row"><div class="row-label">Cache Storage</div><div class="row-value" data-env="cacheStorage">—</div></div>
        <div class="row"><div class="row-label">OPFS</div><div class="row-value" data-env="opfs">—</div></div>
        <div class="row"><div class="row-label">Storage Quota</div><div class="row-value" data-env="quota">—</div></div>
        <div class="row"><div class="row-label">Persisted</div><div class="row-value" data-env="persisted">—</div></div>
      </div>
    </section>

    <!-- Security context -->
    <section class="section" aria-labelledby="sec-sec">
      <header class="section-head">
        <span class="section-icon">` + iconShieldEnvX + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Security Context</div>
          <h2 class="section-title" id="sec-sec">安全上下文</h2>
          <p class="section-lede">浏览器安全状态与 Sec-Fetch 元数据。</p>
        </div>
      </header>
      <div class="grid">
        <div class="row"><div class="row-label">Secure Context</div><div class="row-value" data-env="secureCtx">—</div></div>
        <div class="row"><div class="row-label">Cross-Origin Isolated</div><div class="row-value" data-env="crossIso">—</div></div>
        <div class="row"><div class="row-label">HTTPS</div><div class="row-value">{{yesNo .Data.SchemeHTTPS}}</div></div>
        <div class="row"><div class="row-label">Origin</div><div class="row-value">{{orDash .Data.Origin}}</div></div>
        <div class="row"><div class="row-label">Referrer</div><div class="row-value">{{orDash .Data.Referer}}</div></div>
        <div class="row"><div class="row-label">Sec-Fetch-Site</div><div class="row-value">{{orDash .Data.SecFetchSite}}</div></div>
        <div class="row"><div class="row-label">Sec-Fetch-Mode</div><div class="row-value">{{orDash .Data.SecFetchMode}}</div></div>
        <div class="row"><div class="row-label">Sec-Fetch-Dest</div><div class="row-value">{{orDash .Data.SecFetchDest}}</div></div>
        <div class="row"><div class="row-label">Canvas Hash</div><div class="row-value" data-env="canvasHash">—</div></div>
        <div class="row"><div class="row-label">Audio Hash</div><div class="row-value" data-env="audioHash">—</div></div>
      </div>
    </section>

    <!-- Capabilities -->
    <section class="section" aria-labelledby="sec-cap">
      <header class="section-head">
        <span class="section-icon">` + iconFlaskEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Capabilities</div>
          <h2 class="section-title" id="sec-cap">功能支持</h2>
          <p class="section-lede">运行时特性检测；绿色表示支持。</p>
        </div>
      </header>
      <div class="features" data-env-features></div>
    </section>

    <!-- Permissions -->
    <section class="section" aria-labelledby="sec-perm">
      <header class="section-head">
        <span class="section-icon">` + iconKeyEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Permissions</div>
          <h2 class="section-title" id="sec-perm">权限状态</h2>
          <p class="section-lede">当前授权状态（若浏览器允许查询）。</p>
        </div>
      </header>
      <div class="features" data-env-perms></div>
    </section>

    <!-- Headers -->
    {{if .HeaderKeys}}
    <section class="section" aria-labelledby="sec-hdr">
      <header class="section-head">
        <span class="section-icon">` + iconAccessEnv + `</span>
        <div class="section-title-group">
          <div class="section-kicker">Request Headers</div>
          <h2 class="section-title" id="sec-hdr">请求头</h2>
          <p class="section-lede">已过滤的诊断请求头；Authorization / Cookie 等敏感头不会显示。</p>
        </div>
      </header>
      <div class="headers">
        {{range .HeaderKeys}}
        <div class="hdr">
          <div class="hdr-name">{{.}}</div>
          <div class="hdr-value">{{index $.Data.Headers .}}</div>
        </div>
        {{end}}
      </div>
    </section>
    {{end}}

    <!-- Footer -->
    <footer class="foot">
      <div class="foot-meta">
        <span>Aegis Environment Inspector</span>
        <span class="sep">·</span>
        <span>Checked {{.CheckedHuman}}</span>
        {{if .RequestID}}<span class="sep">·</span><span>Request ID <code>{{.RequestID}}</code></span>{{end}}
        <span class="grow"></span>
        <span>© {{.Year}}</span>
      </div>
    </footer>
  </div>

  <script>window.__AEGIS_SERVER_TIME_MS__ = {{.ServerTimeMs}};</script>
  <script>
  (function(){
    'use strict';
    var $ = function(s){ return document.querySelectorAll(s); };
    function set(key, value){
      $('[data-env="'+key+'"]').forEach(function(el){
        el.textContent = (value === '' || value === undefined || value === null) ? '—' : String(value);
      });
    }
    function yesNo(b){ return b ? '是' : '否'; }
    function available(b){ return b ? '可用' : '不可用'; }

    // ── Browser / UA-CH ──
    var ua = navigator.userAgent || '';
    var uaData = navigator.userAgentData;
    var brief = 'Unknown', engine = 'Unknown', vendor = navigator.vendor || '';
    var special = [];

    function detectBrowserFromUA(){
      var m;
      if ((m = /Edg\/(\S+)/.exec(ua))) return ['Edge ' + m[1], 'Blink'];
      if ((m = /OPR\/(\S+)/.exec(ua))) return ['Opera ' + m[1], 'Blink'];
      if ((m = /SamsungBrowser\/(\S+)/.exec(ua))) return ['Samsung Browser ' + m[1], 'Blink'];
      if ((m = /UCBrowser\/(\S+)/.exec(ua))) return ['UC Browser ' + m[1], 'Blink'];
      if ((m = /FxiOS\/(\S+)/.exec(ua))) return ['Firefox iOS ' + m[1], 'WebKit'];
      if ((m = /CriOS\/(\S+)/.exec(ua))) return ['Chrome iOS ' + m[1], 'WebKit'];
      if ((m = /Firefox\/(\S+)/.exec(ua))) return ['Firefox ' + m[1], 'Gecko'];
      if ((m = /Chrome\/(\S+)/.exec(ua))) return ['Chrome ' + m[1], 'Blink'];
      if ((m = /Version\/(\S+).*Safari/.exec(ua))) return ['Safari ' + m[1], 'WebKit'];
      return ['Unknown', 'Unknown'];
    }

    if (uaData && uaData.brands) {
      var brands = uaData.brands.filter(function(b){ return !/^Not/.test(b.brand); });
      var primary = brands.find(function(b){
        return ['Google Chrome','Microsoft Edge','Opera','Brave','Firefox','Vivaldi','Arc'].indexOf(b.brand) >= 0;
      }) || brands[0];
      if (primary) {
        brief = primary.brand + ' ' + primary.version;
        if (/Chrome|Edge|Opera|Brave|Vivaldi|Arc|Chromium/.test(primary.brand)) engine = 'Blink';
        else if (/Firefox/.test(primary.brand)) engine = 'Gecko';
        else if (/Safari/.test(primary.brand)) engine = 'WebKit';
      }
    }
    if (brief === 'Unknown') {
      var pair = detectBrowserFromUA();
      brief = pair[0]; engine = pair[1];
    }

    // Brave / Headless / WebView
    if (navigator.brave && typeof navigator.brave.isBrave === 'function') {
      navigator.brave.isBrave().then(function(isBrave){
        if (isBrave) { special.push('Brave'); set('specialMode', special.join(' · ')); }
      }).catch(function(){});
    }
    if (/HeadlessChrome/.test(ua)) special.push('Headless');
    if (/wv\)|; wv\)|FBAN\/|FB_IAB\/|MicroMessenger|QQ\/|Twitter|Instagram|Line\//i.test(ua)) special.push('WebView/In-App');
    if (special.length === 0) special.push('普通模式');
    set('specialMode', special.join(' · '));

    set('browserBrief', brief);
    set('engine', engine);
    set('vendor', vendor || '—');
    set('fullVersion', (uaData && uaData.getHighEntropyValues) ? '采集中…' : (navigator.appVersion || '—'));
    set('cookieEnabled', yesNo(navigator.cookieEnabled));
    set('dnt', navigator.doNotTrack === '1' ? '启用' : (navigator.doNotTrack === '0' ? '未启用' : (navigator.doNotTrack || '未声明')));
    set('gpc', (navigator.globalPrivacyControl === true) ? '启用 (GPC)' : (navigator.globalPrivacyControl === false ? '未启用' : '未声明'));
    set('pdfViewer', yesNo(navigator.pdfViewerEnabled === true));
    set('webdriver', yesNo(navigator.webdriver === true));
    set('userAgentFull', ua);

    // ── Platform / OS detection ──
    // 立即用 UA 字符串给一个合理的结果（不再显示 "Win32" 这类 legacy 值），
    // 随后若支持 UA-CH high entropy 则精确升级到 Windows 11 / macOS Sonoma 等。
    function parsePlatformFromUA() {
      var u = ua, m, p = { platform: 'Unknown', version: '', arch: '', model: '', mobile: false };
      if ((m = /Windows NT ([\d.]+)/.exec(u))) {
        var nt = m[1];
        // NT 10.0 既可能是 Windows 10 也可能是 Windows 11 —— 需要 UA-CH 才能区分
        if (nt === '10.0') p.platform = 'Windows 10 / 11';
        else if (nt === '6.3') p.platform = 'Windows 8.1';
        else if (nt === '6.2') p.platform = 'Windows 8';
        else if (nt === '6.1') p.platform = 'Windows 7';
        else p.platform = 'Windows (NT ' + nt + ')';
        p.version = nt;
        if (/Win64|x64|WOW64/.test(u)) p.arch = 'x86_64';
        else if (/ARM64/.test(u)) p.arch = 'arm64';
        else p.arch = 'x86';
      } else if ((m = /Mac OS X ([\d_]+)/.exec(u))) {
        p.version = m[1].replace(/_/g, '.');
        p.platform = 'macOS ' + macOSCodename(p.version) + ' (' + p.version + ')';
        if (/Intel/.test(u)) p.arch = 'x86_64';
      } else if ((m = /iPhone OS ([\d_]+)/.exec(u))) {
        p.version = m[1].replace(/_/g, '.');
        p.platform = 'iOS ' + p.version;
        p.mobile = true;
      } else if (/iPad/.test(u)) {
        p.platform = 'iPadOS';
        if ((m = /CPU OS ([\d_]+)/.exec(u))) p.version = m[1].replace(/_/g, '.');
        if (p.version) p.platform += ' ' + p.version;
        p.mobile = true;
      } else if ((m = /Android ([\d.]+)/.exec(u))) {
        p.platform = 'Android ' + m[1];
        p.version = m[1];
        p.mobile = true;
        if ((m = /; ([^;)]+) Build\//.exec(u))) p.model = m[1].trim();
      } else if (/CrOS/.test(u)) {
        p.platform = 'Chrome OS';
      } else if (/Linux/.test(u)) {
        p.platform = 'Linux';
        if (/x86_64|Linux x86_64/.test(u)) p.arch = 'x86_64';
        else if (/aarch64/.test(u)) p.arch = 'arm64';
      } else if (/FreeBSD/.test(u)) {
        p.platform = 'FreeBSD';
      } else if (/OpenBSD/.test(u)) {
        p.platform = 'OpenBSD';
      }
      return p;
    }

    function macOSCodename(ver) {
      var major = parseInt(ver.split('.')[0], 10);
      var map = {
        15: 'Sequoia', 14: 'Sonoma', 13: 'Ventura', 12: 'Monterey',
        11: 'Big Sur', 10: 'Catalina / Mojave / High Sierra'
      };
      return map[major] || '';
    }

    // 立即用 UA 结果填充（避免出现 "Win32" 这种无意义值）
    (function(){
      var p = parsePlatformFromUA();
      set('platform', p.platform || navigator.platform || '—');
      if (p.version) set('osVersion', p.version);
      if (p.arch) set('arch', p.arch);
      if (p.model) set('deviceModel', p.model);
      set('mobile', yesNo(p.mobile || (uaData && uaData.mobile)));
    })();

    // 若支持 UA-CH high entropy，则精确升级
    if (uaData && uaData.getHighEntropyValues) {
      uaData.getHighEntropyValues([
        'platform','platformVersion','architecture','bitness',
        'model','uaFullVersion','fullVersionList','wow64','formFactor'
      ]).then(function(h){
        var platform = h.platform || '';
        var version = h.platformVersion || '';
        var friendly = platform;
        // Windows 11 = platformVersion major >= 13； Windows 10 = 1..12
        if (platform === 'Windows' && version) {
          var major = parseInt(version.split('.')[0], 10);
          if (major >= 13) friendly = 'Windows 11';
          else if (major >= 1) friendly = 'Windows 10';
          else friendly = 'Windows';
          friendly += ' (NT ' + version + ')';
        } else if (platform === 'macOS' && version) {
          var codename = macOSCodename(version);
          friendly = 'macOS' + (codename ? ' ' + codename : '') + ' ' + version;
        } else if (platform === 'Android' && version) {
          friendly = 'Android ' + version;
        } else if (platform === 'iOS' && version) {
          friendly = 'iOS ' + version;
        } else if (platform === 'Chrome OS' && version) {
          friendly = 'Chrome OS ' + version;
        } else if (platform === 'Linux' && version) {
          friendly = 'Linux ' + version;
        }
        if (friendly) set('platform', friendly);
        if (version) set('osVersion', version);
        if (h.architecture) set('arch', h.architecture + (h.wow64 ? ' (WoW64)' : ''));
        if (h.bitness) set('bitness', h.bitness + '-bit');
        if (h.model) set('deviceModel', h.model);
        if (h.formFactor) {
          set('formFactor', Array.isArray(h.formFactor) ? h.formFactor.join(', ') : h.formFactor);
        }
        set('mobile', yesNo(!!uaData.mobile));
        if (h.fullVersionList && h.fullVersionList.length) {
          var primary = h.fullVersionList.find(function(b){ return !/^Not/.test(b.brand); });
          if (primary) set('fullVersion', primary.brand + ' ' + primary.version);
        } else if (h.uaFullVersion) {
          set('fullVersion', h.uaFullVersion);
        }
      }).catch(function(){
        // 静默失败；UA 回退已在上面填充
      });
    }

    set('touchDevice', yesNo(('ontouchstart' in window) || (navigator.maxTouchPoints > 0)));

    // ── Display ──
    try {
      set('screen', screen.width + ' × ' + screen.height);
      set('screenAvail', screen.availWidth + ' × ' + screen.availHeight);
      set('colorDepth', (screen.colorDepth || '—') + ' bit');
      if (screen.orientation) set('orientation', screen.orientation.type + ' · ' + (screen.orientation.angle||0) + '°');
      if (window.getScreenDetails) {
        set('multiScreen', '可申请 · Window Management API');
      } else if (screen.isExtended !== undefined) {
        set('multiScreen', yesNo(screen.isExtended));
      } else {
        set('multiScreen', '不支持检测');
      }
    } catch(e){}
    set('viewport', window.innerWidth + ' × ' + window.innerHeight);
    set('viewportHint', '像素比 ' + (window.devicePixelRatio || 1));
    set('dpr', window.devicePixelRatio || 1);
    set('colorScheme', window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    set('contrast', window.matchMedia('(prefers-contrast: more)').matches ? 'more' :
        (window.matchMedia('(prefers-contrast: less)').matches ? 'less' : 'no-preference'));
    set('reducedMotion', window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'reduce' : 'no-preference');
    set('colorGamut', window.matchMedia('(color-gamut: rec2020)').matches ? 'rec2020' :
        (window.matchMedia('(color-gamut: p3)').matches ? 'p3' :
        (window.matchMedia('(color-gamut: srgb)').matches ? 'srgb' : 'unknown')));
    set('hdr', window.matchMedia('(dynamic-range: high)').matches ? 'high (HDR)' : 'standard');
    // Refresh rate via rAF sampling
    (function(){
      var t0 = performance.now(); var count = 0;
      function step(){ count++; if (performance.now() - t0 < 250) requestAnimationFrame(step); else {
        var hz = Math.round(count / ((performance.now() - t0)/1000));
        set('refreshRate', hz + ' Hz');
      } } requestAnimationFrame(step);
    })();

    // ── GPU (WebGL) ──
    (function(){
      var canvas = document.createElement('canvas');
      var gl = canvas.getContext('webgl2') || canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
      if (!gl) { set('webgl', '不支持'); set('webgl2', '不支持'); return; }
      var isWebGL2 = ('WebGL2RenderingContext' in window) && (gl instanceof WebGL2RenderingContext);
      set('webgl', '支持');
      set('webgl2', isWebGL2 ? '支持' : '不支持');
      var dbg = gl.getExtension('WEBGL_debug_renderer_info');
      var renderer = dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
      var vendor  = dbg ? gl.getParameter(dbg.UNMASKED_VENDOR_WEBGL)   : gl.getParameter(gl.VENDOR);
      set('webglRenderer', renderer || '—');
      set('webglVendor', vendor || '—');
      set('webglMaxTex', gl.getParameter(gl.MAX_TEXTURE_SIZE));
      set('webglGLSL', gl.getParameter(gl.SHADING_LANGUAGE_VERSION));
    })();
    // WebGPU
    if (navigator.gpu) {
      set('webgpu', '支持');
      navigator.gpu.requestAdapter().then(function(adapter){
        if (!adapter) { set('webgpuAdapter', '无可用适配器'); return; }
        var info = adapter.info || {};
        var parts = [info.vendor, info.architecture, info.device].filter(Boolean);
        set('webgpuAdapter', parts.length ? parts.join(' · ') : (adapter.name || 'available'));
      }).catch(function(){ set('webgpuAdapter', '查询失败'); });
    } else {
      set('webgpu', '不支持');
      set('webgpuAdapter', '—');
    }

    // ── Hardware ──
    set('hwCores', navigator.hardwareConcurrency || '—');
    set('deviceMemory', navigator.deviceMemory ? (navigator.deviceMemory + ' GiB (上界)') : '—');
    if (performance.memory && performance.memory.jsHeapSizeLimit) {
      set('heapLimit', formatBytes(performance.memory.jsHeapSizeLimit));
    } else { set('heapLimit', '—'); }
    set('touchPoints', navigator.maxTouchPoints || 0);
    set('pointer', window.matchMedia('(pointer: fine)').matches ? 'fine' :
        (window.matchMedia('(pointer: coarse)').matches ? 'coarse' : 'none'));
    set('hover', window.matchMedia('(hover: hover)').matches ? 'hover' : 'none');

    // Keyboard layout (requires secure context)
    if (navigator.keyboard && navigator.keyboard.getLayoutMap) {
      navigator.keyboard.getLayoutMap().then(function(m){
        var keyQ = m.get('KeyQ'); // 常见 layout 探针
        set('keyboardLayout', (keyQ === 'q') ? 'QWERTY' : (keyQ === 'a' ? 'AZERTY' : (keyQ === ',' ? 'Dvorak' : 'Other: ' + keyQ)));
      }).catch(function(){ set('keyboardLayout', '不可用'); });
    } else { set('keyboardLayout', '不支持'); }

    // Gamepads
    try {
      var pads = (navigator.getGamepads ? navigator.getGamepads() : []).filter(Boolean);
      set('gamepads', pads.length);
    } catch(e){ set('gamepads', '—'); }

    // Media devices count
    if (navigator.mediaDevices && navigator.mediaDevices.enumerateDevices) {
      navigator.mediaDevices.enumerateDevices().then(function(devs){
        var cams = devs.filter(function(d){ return d.kind==='videoinput'; }).length;
        var mics = devs.filter(function(d){ return d.kind==='audioinput'; }).length;
        var spks = devs.filter(function(d){ return d.kind==='audiooutput'; }).length;
        set('devCamera', cams + ' 个');
        set('devMic', mics + ' 个');
        set('devSpeaker', spks + ' 个');
      }).catch(function(){
        set('devCamera', '不可用'); set('devMic', '不可用'); set('devSpeaker', '不可用');
      });
    }

    // Battery
    if (navigator.getBattery) {
      navigator.getBattery().then(function(b){
        set('battery', Math.round(b.level*100) + '% · ' + (b.charging ? '充电中' : '未充电'));
      }).catch(function(){ set('battery', '不可用'); });
    } else { set('battery', '不可用'); }

    // ── Network ──
    var conn = navigator.connection || navigator.mozConnection || navigator.webkitConnection;
    if (conn) {
      set('connectionType', conn.type || '—');
      set('effectiveType', conn.effectiveType || '—');
      set('downlink', (conn.downlink != null) ? (conn.downlink + ' Mbps') : '—');
      set('rtt', (conn.rtt != null) ? (conn.rtt + ' ms') : '—');
      set('saveData', yesNo(conn.saveData));
    }
    set('online', navigator.onLine ? '在线' : '离线');

    // ── Locale ──
    set('lang', navigator.language || '—');
    set('languages', navigator.languages ? navigator.languages.join(', ') : '—');
    try {
      var ro = Intl.DateTimeFormat().resolvedOptions();
      set('tz', ro.timeZone || '—');
      set('calendar', ro.calendar || '—');
      set('numberingSystem', ro.numberingSystem || '—');
      set('hourCycle', ro.hourCycle || (ro.hour12 ? 'h12' : 'h23'));
    } catch(e){}
    var off = -new Date().getTimezoneOffset();
    var sign = off >= 0 ? '+' : '-'; var abs = Math.abs(off);
    set('tzOffset', 'UTC' + sign + String(Math.floor(abs/60)).padStart(2,'0') + ':' + String(abs%60).padStart(2,'0'));

    // ── Time / clock skew ──
    (function(){
      var srv = window.__AEGIS_SERVER_TIME_MS__ || 0;
      var now = Date.now();
      set('clientTime', new Date(now).toLocaleString());
      if (srv) {
        var diff = now - srv;
        var s = diff/1000;
        set('clockSkew', (diff > 0 ? '客户端超前 ' : '客户端落后 ') + Math.abs(s).toFixed(1) + ' 秒');
      }
    })();

    // ── Storage ──
    function probe(store){
      try { var k='__probe_'+Date.now(); store.setItem(k,'1'); store.removeItem(k); return true; } catch(e){ return false; }
    }
    set('localStorage', available(probe(window.localStorage)));
    set('sessionStorage', available(probe(window.sessionStorage)));
    set('indexedDb', available(!!window.indexedDB));
    set('cacheStorage', available(!!window.caches));
    set('cookieStatus', navigator.cookieEnabled ? '已启用' : '被禁用');
    set('opfs', available(!!(navigator.storage && navigator.storage.getDirectory)));
    if (navigator.storage && navigator.storage.estimate) {
      navigator.storage.estimate().then(function(est){
        set('quota', formatBytes(est.usage || 0) + ' / ' + formatBytes(est.quota || 0));
      }).catch(function(){ set('quota', '—'); });
    } else { set('quota', '不可用'); }
    if (navigator.storage && navigator.storage.persisted) {
      navigator.storage.persisted().then(function(p){ set('persisted', yesNo(p)); }).catch(function(){ set('persisted', '—'); });
    } else { set('persisted', '—'); }

    // ── Security context ──
    set('secureCtx', yesNo(window.isSecureContext === true));
    set('crossIso', yesNo(window.crossOriginIsolated === true));

    // ── Canvas fingerprint (short hash) ──
    try {
      var cv = document.createElement('canvas'); cv.width = 180; cv.height = 40;
      var ctx = cv.getContext('2d');
      ctx.textBaseline = 'top';
      ctx.font = "14px 'Arial'";
      ctx.fillStyle = '#f60'; ctx.fillRect(0,0,90,20);
      ctx.fillStyle = '#069'; ctx.fillText('Aegis ∆∑∞', 2, 15);
      ctx.fillStyle = 'rgba(102,204,0,0.6)';
      ctx.fillText('🌐 fingerprint', 4, 22);
      var d = cv.toDataURL();
      set('canvasHash', shortHash(d).slice(0, 16));
    } catch(e){ set('canvasHash', '不可用'); }

    // ── Audio fingerprint (short hash) ──
    (function(){
      try {
        var AC = window.OfflineAudioContext || window.webkitOfflineAudioContext;
        if (!AC) { set('audioHash', '不支持'); return; }
        var ctx = new AC(1, 44100, 44100);
        var osc = ctx.createOscillator(); osc.type = 'triangle'; osc.frequency.value = 1e4;
        var comp = ctx.createDynamicsCompressor();
        osc.connect(comp); comp.connect(ctx.destination); osc.start(0);
        ctx.oncomplete = function(ev){
          var data = ev.renderedBuffer.getChannelData(0);
          var sum = 0; for (var i=4500;i<5000;i++) sum += Math.abs(data[i]);
          set('audioHash', ('audio:' + sum.toFixed(12)).slice(0,20));
        };
        ctx.startRendering();
      } catch(e){ set('audioHash', '不可用'); }
    })();

    function shortHash(str){
      var h = 2166136261 >>> 0;
      for (var i=0;i<str.length;i++){ h ^= str.charCodeAt(i); h = (h + ((h<<1)+(h<<4)+(h<<7)+(h<<8)+(h<<24))) >>> 0; }
      return ('00000000' + h.toString(16)).slice(-8);
    }

    function formatBytes(b){
      if (!b) return '—';
      var u = ['B','KiB','MiB','GiB','TiB']; var i = 0;
      while (b >= 1024 && i < u.length-1) { b /= 1024; i++; }
      return (b >= 100 ? b.toFixed(0) : b.toFixed(1)) + ' ' + u[i];
    }

    // ── Capabilities pills ──
    (function(){
      var host = document.querySelector('[data-env-features]');
      if (!host) return;
      var feats = [
        ['Service Worker', 'serviceWorker' in navigator],
        ['Push API', 'PushManager' in window],
        ['Notifications', 'Notification' in window],
        ['WebRTC', !!window.RTCPeerConnection],
        ['WebAssembly', typeof WebAssembly !== 'undefined'],
        ['WebAssembly SIMD', (function(){try{return WebAssembly.validate(new Uint8Array([0,97,115,109,1,0,0,0,1,5,1,96,0,1,123,3,2,1,0,10,10,1,8,0,65,0,253,15,253,98,11]));}catch(e){return false;}})()],
        ['Shared Array Buffer', typeof SharedArrayBuffer !== 'undefined'],
        ['Atomics', typeof Atomics !== 'undefined'],
        ['Clipboard', !!(navigator.clipboard && navigator.clipboard.readText)],
        ['Share', !!navigator.share],
        ['Credentials', !!navigator.credentials],
        ['WebAuthn', !!window.PublicKeyCredential],
        ['Geolocation', 'geolocation' in navigator],
        ['Media Devices', !!(navigator.mediaDevices && navigator.mediaDevices.enumerateDevices)],
        ['getUserMedia', !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia)],
        ['getDisplayMedia', !!(navigator.mediaDevices && navigator.mediaDevices.getDisplayMedia)],
        ['MediaSession', 'mediaSession' in navigator],
        ['MediaSource', 'MediaSource' in window],
        ['EME / DRM', 'requestMediaKeySystemAccess' in navigator],
        ['Web Speech API', 'speechSynthesis' in window],
        ['Wake Lock', 'wakeLock' in navigator],
        ['Bluetooth', !!navigator.bluetooth],
        ['USB', !!navigator.usb],
        ['Serial', !!navigator.serial],
        ['HID', !!navigator.hid],
        ['NFC', 'NDEFReader' in window],
        ['Vibration', 'vibrate' in navigator],
        ['Intersection Observer', 'IntersectionObserver' in window],
        ['Resize Observer', 'ResizeObserver' in window],
        ['BroadcastChannel', !!window.BroadcastChannel],
        ['Payment Request', !!window.PaymentRequest],
        ['View Transitions', 'startViewTransition' in document],
        ['Popover API', HTMLElement.prototype.hasOwnProperty('popover')],
        ['Anchor Positioning', CSS && CSS.supports && CSS.supports('anchor-name', '--x')],
        ['Container Queries', CSS && CSS.supports && CSS.supports('container-type', 'inline-size')],
        ['CSS Nesting', CSS && CSS.supports && CSS.supports('selector(& > *)')],
        ['Intl Segmenter', !!(window.Intl && Intl.Segmenter)],
        ['Web Crypto', !!(window.crypto && window.crypto.subtle)],
        ['Trusted Types', 'TrustedTypes' in window || 'trustedTypes' in window],
      ];
      host.innerHTML = feats.map(function(f){
        var cls = f[1] ? 'good' : 'bad';
        return '<span class="pill '+cls+'"><span class="d"></span>'+f[0]+'</span>';
      }).join('');
    })();

    // ── Codecs pills ──
    (function(){
      var host = document.querySelector('[data-env-codecs]');
      if (!host) return;
      var v = document.createElement('video');
      var a = document.createElement('audio');
      function vcan(t){ return v.canPlayType(t) || 'no'; }
      function acan(t){ return a.canPlayType(t) || 'no'; }
      function classFor(r){ return r === 'probably' ? 'good' : (r === 'maybe' ? 'warn' : 'bad'); }
      var list = [
        ['H.264 (MP4)',  vcan('video/mp4; codecs="avc1.42E01E"')],
        ['H.265 / HEVC', vcan('video/mp4; codecs="hev1"')],
        ['AV1',          vcan('video/mp4; codecs="av01.0.05M.08"')],
        ['VP8 (WebM)',   vcan('video/webm; codecs="vp8"')],
        ['VP9 (WebM)',   vcan('video/webm; codecs="vp9"')],
        ['Theora',       vcan('video/ogg; codecs="theora"')],
        ['AAC',          acan('audio/mp4; codecs="mp4a.40.2"')],
        ['MP3',          acan('audio/mpeg')],
        ['Opus',         acan('audio/webm; codecs="opus"')],
        ['FLAC',         acan('audio/flac')],
        ['Vorbis',       acan('audio/ogg; codecs="vorbis"')],
        ['WAV',          acan('audio/wav')],
      ];
      host.innerHTML = list.map(function(c){
        var cls = classFor(c[1]);
        var label = c[1] === 'no' ? '不支持' : c[1];
        return '<span class="pill '+cls+'"><span class="d"></span>'+c[0]+' · '+label+'</span>';
      }).join('');
    })();

    // ── Image format decoding support ──
    (function(){
      var host = document.querySelector('[data-env-images]');
      if (!host) return;
      var formats = [
        ['WebP', 'data:image/webp;base64,UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=='],
        ['AVIF', 'data:image/avif;base64,AAAAIGZ0eXBhdmlmAAAAAGF2aWZtaWYxbWlhZk1BMUIAAADybWV0YQAAAAAAAAAoaGRscgAAAAAAAAAAcGljdAAAAAAAAAAAAAAAAGxpYmF2aWYAAAAADnBpdG0AAAAAAAEAAAAeaWxvYwAAAAAEQAABAAEAAAAAARIAAQAAAAAAAAAEAAAAI2lpbmYAAAAAAAEAAAAVaW5mZQIAAAAAAQAAYXYwMQAAAABqaXBycAAAAEtpcGNvAAAAFGlzcGUAAAAAAAAAAQAAAAEAAAAQcGl4aQAAAAADCAgIAAAADGF2MUOBDQwAAAAAE2NvbHJuY2x4AAIAAgAGgAAAABdpcG1hAAAAAAAAAAEAAQUBAgMEAAAAH21kYXQSAAoIGAAQCCAAAH8ABABAQFAABwA='],
        ['JXL (JPEG-XL)', 'data:image/jxl;base64,/woAENAAQABCABAAEA=='],
      ];
      var results = {};
      var pending = formats.length;
      formats.forEach(function(f){
        var img = new Image();
        img.onload = img.onerror = function(){
          results[f[0]] = (img.complete && img.naturalWidth > 0);
          if (--pending === 0) render();
        };
        img.src = f[1];
      });
      // Also detect HEIC / JXL capabilities via canvas createImageBitmap detection
      setTimeout(function(){
        if (pending > 0) { pending = 0; render(); } // 超时保底
      }, 1200);
      function render(){
        var static_ = [
          ['GIF', true], ['PNG', true], ['JPEG', true], ['SVG', true],
        ];
        static_.forEach(function(s){ results[s[0]] = s[1]; });
        var order = ['PNG','JPEG','GIF','SVG','WebP','AVIF','JXL (JPEG-XL)'];
        host.innerHTML = order.map(function(n){
          var ok = results[n];
          var cls = ok ? 'good' : 'bad';
          return '<span class="pill '+cls+'"><span class="d"></span>'+n+' · '+(ok?'支持':'不支持')+'</span>';
        }).join('');
      }
    })();

    // ── Fonts probe (width comparison against monospace baseline) ──
    (function(){
      var host = document.querySelector('[data-env-fonts]');
      if (!host) return;
      var probe = ['Arial','Helvetica','Times New Roman','Georgia','Courier New','Verdana','Tahoma',
                   'Segoe UI','PingFang SC','Microsoft YaHei','SimSun','SimHei','Noto Sans CJK SC',
                   'Roboto','Inter','SF Pro','Menlo','Monaco','Consolas','Cascadia Code','JetBrains Mono'];
      var baselineFonts = ['monospace','serif','sans-serif'];
      var testString = 'mmmmmmmmmmlli';
      var testSize = '72px';
      var body = document.body;
      var span = document.createElement('span');
      span.style.visibility = 'hidden';
      span.style.position = 'absolute';
      span.style.left = '-9999px';
      span.style.fontSize = testSize;
      span.textContent = testString;
      body.appendChild(span);
      // Baseline widths for each fallback family
      var baseW = {};
      baselineFonts.forEach(function(base){
        span.style.fontFamily = base;
        baseW[base] = span.offsetWidth;
      });
      var detected = [];
      probe.forEach(function(font){
        var hit = false;
        for (var i=0;i<baselineFonts.length && !hit;i++) {
          span.style.fontFamily = "'"+font+"',"+baselineFonts[i];
          if (span.offsetWidth !== baseW[baselineFonts[i]]) hit = true;
        }
        detected.push([font, hit]);
      });
      body.removeChild(span);
      host.innerHTML = detected.map(function(f){
        var cls = f[1] ? 'good' : 'bad';
        return '<span class="pill '+cls+'"><span class="d"></span>'+f[0]+'</span>';
      }).join('');
    })();

    // ── Permissions ──
    (function(){
      var host = document.querySelector('[data-env-perms]');
      if (!host) return;
      if (!(navigator.permissions && navigator.permissions.query)) {
        host.innerHTML = '<span class="pill">Permissions API 不可用</span>';
        return;
      }
      var probes = ['geolocation','notifications','camera','microphone','persistent-storage',
                    'clipboard-read','clipboard-write','accelerometer','gyroscope','magnetometer',
                    'midi','local-fonts','screen-wake-lock','display-capture'];
      Promise.all(probes.map(function(name){
        return navigator.permissions.query({name: name}).then(function(r){
          return { name: name, state: r.state };
        }).catch(function(){ return { name: name, state: 'unsupported' }; });
      })).then(function(list){
        host.innerHTML = list.map(function(p){
          var cls = p.state === 'granted' ? 'good' : (p.state === 'denied' ? 'bad' : (p.state === 'prompt' ? 'warn' : ''));
          var label = { granted: '已授权', denied: '已拒绝', prompt: '待询问', unsupported: '不支持' }[p.state] || p.state;
          return '<span class="pill '+cls+'"><span class="d"></span>'+p.name+' · '+label+'</span>';
        }).join('');
      });
    })();

    // Copy button
    document.querySelectorAll('[data-copy]').forEach(function(btn){
      btn.addEventListener('click', function(){
        try { navigator.clipboard.writeText(ua); } catch(e){}
        var txt = btn.lastChild; var old = txt.textContent;
        txt.textContent = '已复制';
        setTimeout(function(){ txt.textContent = old; }, 1400);
      });
    });
  })();
  </script>
</body>
</html>`
