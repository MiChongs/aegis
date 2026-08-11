package response

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────
// HTML 状态页 (/status) — Claude / Anthropic 设计语言
// ─────────────────────────────────────────────────────────────────────

// StatusComponent 组件健康状况（与 service.MonitorComponent 字段对齐）。
type StatusComponent struct {
	Key       string
	Name      string
	Status    string // "available" / "degraded" / "unavailable"
	Severity  string
	Available bool
	Summary   string
	Detail    string
	CheckedAt time.Time
	DependsOn []string
}

// StatusCounts 状态汇总。
type StatusCounts struct {
	Total       int
	Available   int
	Degraded    int
	Unavailable int
}

// StatusRuntime 运行态信息。
type StatusRuntime struct {
	AppName       string
	Environment   string
	Version       string
	CheckedAt     time.Time
	StartedAt     time.Time
	UptimeSeconds int64
	Timezone      string

	GoVersion  string
	GoOS       string
	GoArch     string
	Goroutines int

	Hostname    string
	OS          string
	Platform    string
	KernelArch  string
	CPUModel    string
	CPUCores    int
	CPUThreads  int
	CPUUsage    float64
	MemTotal    uint64
	MemUsed     uint64
	MemUsedPct  float64
	DiskTotal   uint64
	DiskUsed    uint64
	DiskUsedPct float64
	PID         int
	ProcessMem  uint64

	MemAlloc uint64
	MemSys   uint64
	NumGC    uint32
}

// StatusPageData 渲染状态页所需的数据。
type StatusPageData struct {
	Status           string // available / degraded / unavailable
	Score            int
	AvailabilityRate float64
	Summary          string
	CheckedAt        time.Time
	Runtime          StatusRuntime
	Counts           StatusCounts
	Highlights       []string
	Infrastructure   []StatusComponent
	Modules          []StatusComponent
}

// RenderStatusPage 渲染 Claude 风格的完整状态页。
func RenderStatusPage(c *gin.Context, data StatusPageData) {
	setMetaHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=15")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'")

	view := statusView{
		Data:      data,
		RequestID: requestID(c),
		Year:      time.Now().Year(),
	}
	view.Derive()

	var buf bytes.Buffer
	if err := statusPageTemplate.Execute(&buf, view); err != nil {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		_, _ = c.Writer.WriteString(fmt.Sprintf("Aegis status: %s (score %d)\n", data.Status, data.Score))
		return
	}

	// 总体不可用时 HTTP 503；降级时 200 但附 Retry-After
	switch data.Status {
	case "unavailable":
		c.Status(503)
		c.Header("Retry-After", "30")
	case "degraded":
		c.Status(200)
		c.Header("Retry-After", "15")
	default:
		c.Status(200)
	}

	_, _ = c.Writer.Write(buf.Bytes())
}

// statusView 模板内部视图模型，附带派生字段。
type statusView struct {
	Data      StatusPageData
	RequestID string
	Year      int

	// 派生
	OverallLabel   string
	OverallCaption string
	OverallTone    string // good / warn / bad
	UptimeHuman    string
	CheckedHuman   string
	AvailabilityFX string
	GroupedModules []statusGroup
	MemHuman       string
	MemUsedHuman   string
	DiskHuman      string
	DiskUsedHuman  string
	ProcMemHuman   string
}

type statusGroup struct {
	Title      string
	Components []StatusComponent
}

// Derive 计算派生字段。
func (v *statusView) Derive() {
	switch v.Data.Status {
	case "available":
		v.OverallLabel = "全部系统运行中"
		v.OverallCaption = "All systems operational"
		v.OverallTone = "good"
	case "degraded":
		v.OverallLabel = "部分服务降级"
		v.OverallCaption = "Partial degradation"
		v.OverallTone = "warn"
	case "unavailable":
		v.OverallLabel = "存在不可用组件"
		v.OverallCaption = "Service disruption"
		v.OverallTone = "bad"
	default:
		v.OverallLabel = "状态未知"
		v.OverallCaption = "Status unknown"
		v.OverallTone = "warn"
	}
	v.UptimeHuman = formatUptimeSeconds(v.Data.Runtime.UptimeSeconds)
	v.CheckedHuman = v.Data.CheckedAt.Local().Format("2006-01-02 15:04:05")
	v.AvailabilityFX = fmt.Sprintf("%.2f%%", v.Data.AvailabilityRate)

	// 分组：把 modules 按前缀简单聚合
	v.GroupedModules = groupModules(v.Data.Modules)

	v.MemHuman = humanBytes(v.Data.Runtime.MemTotal)
	v.MemUsedHuman = humanBytes(v.Data.Runtime.MemUsed)
	v.DiskHuman = humanBytes(v.Data.Runtime.DiskTotal)
	v.DiskUsedHuman = humanBytes(v.Data.Runtime.DiskUsed)
	v.ProcMemHuman = humanBytes(v.Data.Runtime.ProcessMem)
}

func groupModules(mods []StatusComponent) []statusGroup {
	if len(mods) == 0 {
		return nil
	}
	return []statusGroup{{Title: "系统模块", Components: mods}}
}

func formatUptimeSeconds(s int64) string {
	if s <= 0 {
		return "—"
	}
	d := time.Duration(s) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%d天 %d小时", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%d小时 %d分钟", hours, minutes)
	}
	return fmt.Sprintf("%d分钟", minutes)
}

func humanBytes(b uint64) string {
	if b == 0 {
		return "—"
	}
	const unit = 1024.0
	f := float64(b)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	idx := 0
	for f >= unit && idx < len(units)-1 {
		f /= unit
		idx++
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f %s", f, units[idx])
	}
	return fmt.Sprintf("%.1f %s", f, units[idx])
}

// ─── 模板 ───

var statusPageTemplate = template.Must(template.New("status").Funcs(template.FuncMap{
	"lower":    strings.ToLower,
	"iconFor":  iconForComponentKey,
	"pct":      formatPct,
	"statusCn": statusLabel,
	"dependsJoin": func(deps []string) string {
		if len(deps) == 0 {
			return ""
		}
		return strings.Join(deps, " · ")
	},
	"hasGoodStatus": func(s string) bool { return s == "available" },
	"isDegraded":    func(s string) bool { return s == "degraded" },
	"isBad":         func(s string) bool { return s == "unavailable" },
}).Parse(statusPageSource))

func formatPct(v float64) string { return fmt.Sprintf("%.1f%%", v) }

func statusLabel(s string) string {
	switch s {
	case "available":
		return "正常"
	case "degraded":
		return "降级"
	case "unavailable":
		return "不可用"
	default:
		return "未知"
	}
}

// iconForComponentKey 按组件 key 返回合适的 lucide 图标 SVG。
func iconForComponentKey(key string) template.HTML {
	switch strings.ToLower(key) {
	case "postgres", "postgresql", "pg", "database":
		return template.HTML(iconDatabase)
	case "redis", "cache":
		return template.HTML(iconZap)
	case "nats", "messaging", "queue":
		return template.HTML(iconRadio)
	case "temporal", "workflow":
		return template.HTML(iconTimerCheck)
	case "auth", "authentication", "admin":
		return template.HTML(iconShieldCheck)
	case "realtime", "websocket", "ws":
		return template.HTML(iconActivity)
	case "storage":
		return template.HTML(iconHardDrive)
	case "email", "mail":
		return template.HTML(iconMail)
	case "points", "signin":
		return template.HTML(iconGift)
	case "notification":
		return template.HTML(iconBell)
	case "captcha":
		return template.HTML(iconShapes)
	case "firewall", "waf":
		return template.HTML(iconShieldCheck)
	case "location", "geoip":
		return template.HTML(iconMap)
	case "user":
		return template.HTML(iconUsersRound)
	case "app":
		return template.HTML(iconBuilding)
	case "lottery":
		return template.HTML(iconGift)
	case "audit":
		return template.HTML(iconClipboard)
	default:
		return template.HTML(iconCircleDot)
	}
}

// 额外 lucide 图标（首页已有的直接复用；这里补足本页新增的）
const (
	iconZap        = svgHead + `<path d="M13 2 3 14h9l-1 8 10-12h-9l1-8z"/></svg>`
	iconRadio      = svgHead + `<path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9"/><path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5"/><circle cx="12" cy="12" r="2"/><path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5"/><path d="M19.1 4.9C23 8.8 23 15.1 19.1 19"/></svg>`
	iconTimerCheck = svgHead + `<path d="M10 2h4"/><path d="m15.6 18.4-2.1-2.1"/><path d="m12 14 2-2"/><circle cx="12" cy="14" r="8"/></svg>`
	iconHardDrive  = svgHead + `<line x1="22" x2="2" y1="12" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" x2="6.01" y1="16" y2="16"/><line x1="10" x2="10.01" y1="16" y2="16"/></svg>`
	iconMail       = svgHead + `<rect width="20" height="16" x="2" y="4" rx="2"/><path d="m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7"/></svg>`
	iconShapes     = svgHead + `<path d="M8.3 10a.7.7 0 0 1-.626-1.079L11.4 3a.7.7 0 0 1 1.198-.043L16.3 8.9a.7.7 0 0 1-.572 1.1Z"/><rect x="3" y="14" width="7" height="7" rx="1"/><circle cx="17.5" cy="17.5" r="3.5"/></svg>`
	iconMap        = svgHead + `<path d="M14.106 5.553a2 2 0 0 0 1.788 0l3.659-1.83A1 1 0 0 1 21 4.619v12.764a1 1 0 0 1-.553.894l-4.553 2.277a2 2 0 0 1-1.788 0l-4.212-2.106a2 2 0 0 0-1.788 0l-3.659 1.83A1 1 0 0 1 3 19.381V6.618a1 1 0 0 1 .553-.894l4.553-2.277a2 2 0 0 1 1.788 0z"/><path d="M15 5.764v15"/><path d="M9 3.236v15"/></svg>`
	iconClipboard  = svgHead + `<rect width="8" height="4" x="8" y="2" rx="1" ry="1"/><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><path d="M9 14l2 2 4-4"/></svg>`
	iconCpu        = svgHead + `<rect width="16" height="16" x="4" y="4" rx="2"/><rect width="6" height="6" x="9" y="9"/><path d="M15 2v2"/><path d="M15 20v2"/><path d="M2 15h2"/><path d="M2 9h2"/><path d="M20 15h2"/><path d="M20 9h2"/><path d="M9 2v2"/><path d="M9 20v2"/></svg>`
	iconMemory     = svgHead + `<path d="M6 19v-3"/><path d="M10 19v-3"/><path d="M14 19v-3"/><path d="M18 19v-3"/><path d="M8 11V9"/><path d="M16 11V9"/><path d="M12 11V9"/><path d="M2 15h20"/><path d="M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1.1a2 2 0 0 0 0 3.837V17a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5.1a2 2 0 0 0 0-3.837Z"/></svg>`
	iconServer     = svgHead + `<rect width="20" height="8" x="2" y="2" rx="2" ry="2"/><rect width="20" height="8" x="2" y="14" rx="2" ry="2"/><line x1="6" x2="6.01" y1="6" y2="6"/><line x1="6" x2="6.01" y1="18" y2="18"/></svg>`
	iconRefresh    = svgHead + `<path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/><path d="M16 16h5v5"/></svg>`
	iconGauge      = svgHead + `<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/></svg>`
)

const statusPageSource = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="Aegis 系统状态 — 实时运行态与基础设施健康">
  <meta name="theme-color" content="#f5f4ed" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#141413" media="(prefers-color-scheme: dark)">
  <title>系统状态 · {{.OverallLabel}} — Aegis</title>
  <style>
    :root {
      --bg: #f5f4ed;
      --bg-soft: #eee9de;
      --bg-ivory: #faf9f5;
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
      --status-good: #6f9a5f;
      --status-good-soft: #e6efdf;
      --status-warn: #c38435;
      --status-warn-soft: #f4e6d0;
      --status-bad: #b53333;
      --status-bad-soft: #f3dcdc;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #141413;
        --bg-soft: #1c1c1a;
        --bg-ivory: #1c1c1a;
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
        --status-good: #8fb97f;
        --status-good-soft: #2a3527;
        --status-warn: #d99f56;
        --status-warn-soft: #3a2e1d;
        --status-bad: #d06a6a;
        --status-bad-soft: #3a2424;
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
    }

    .container { max-width: 1120px; margin: 0 auto; padding: 0 40px; }

    /* Nav */
    .nav {
      padding: 28px 0;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 24px;
    }
    .nav-brand {
      display: inline-flex; align-items: center; gap: 10px;
      font-family: "Anthropic Serif", Georgia, serif;
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
    .hero { padding: 48px 0 40px; }
    .hero-kicker {
      display: inline-flex; align-items: center; gap: 8px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic; font-size: 15px; color: var(--brand);
    }
    .hero-kicker::before {
      content: ""; width: 6px; height: 6px; background: var(--brand); border-radius: 50%;
    }
    .hero-rule { width: 48px; height: 1px; margin: 22px 0 28px; background: var(--brand); border: 0; }
    .hero h1 {
      margin: 0; font-family: "Anthropic Serif", Georgia, serif;
      font-size: clamp(42px, 6vw, 68px); font-weight: 500;
      line-height: 1.08; letter-spacing: -0.02em; color: var(--text-primary);
    }
    .overall-badge {
      display: inline-flex; align-items: center; gap: 10px;
      margin-top: 20px; padding: 8px 14px; border-radius: 999px;
      font-size: 13.5px; font-weight: 500; letter-spacing: 0.01em;
    }
    .overall-badge .pulse { width: 8px; height: 8px; border-radius: 50%; }
    .overall-badge.good { background: var(--status-good-soft); color: var(--status-good); }
    .overall-badge.good .pulse { background: var(--status-good); }
    .overall-badge.warn { background: var(--status-warn-soft); color: var(--status-warn); }
    .overall-badge.warn .pulse { background: var(--status-warn); }
    .overall-badge.bad { background: var(--status-bad-soft); color: var(--status-bad); }
    .overall-badge.bad .pulse { background: var(--status-bad); }
    .hero-sub {
      margin: 16px 0 0; font-size: 16px; color: var(--text-secondary); line-height: 1.65;
    }
    .hero-refresh {
      display: inline-flex; align-items: center; gap: 6px;
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      font-size: 14px; color: var(--text-secondary); text-decoration: none;
      margin-left: 14px;
      border-bottom: 1px solid transparent; padding-bottom: 1px;
      transition: color 140ms linear, border-color 140ms linear;
    }
    .hero-refresh:hover { color: var(--text-primary); border-bottom-color: var(--brand); }
    .hero-refresh .icon { width: 14px; height: 14px; display: inline-flex; }
    .hero-refresh .icon svg { width: 100%; height: 100%; }

    /* Stat tiles */
    .tiles {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 16px;
      margin-top: 40px;
    }
    @media (max-width: 900px) { .tiles { grid-template-columns: repeat(2, 1fr); } }
    .tile {
      padding: 22px 24px 20px;
      background: var(--bg-ivory);
      border-radius: 14px;
      box-shadow: 0 0 0 1px var(--rule-soft);
    }
    .tile-label {
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic; font-size: 13px;
      color: var(--text-secondary); letter-spacing: 0.01em;
    }
    .tile-value {
      margin-top: 6px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 32px; font-weight: 500; line-height: 1.1;
      letter-spacing: -0.01em; color: var(--text-primary);
    }
    .tile-hint {
      margin-top: 4px; font-size: 12.5px; color: var(--text-tertiary);
    }
    .tile-value .unit {
      font-size: 17px; font-style: italic; color: var(--text-secondary);
      margin-left: 4px; font-weight: 500;
    }

    /* Section */
    .section { padding: 60px 0; border-top: 1px solid var(--rule); }
    .section-head { margin-bottom: 36px; max-width: 720px; }
    .section-kicker {
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      font-size: 14px; color: var(--brand);
    }
    .section-title {
      margin: 14px 0 14px; font-family: "Anthropic Serif", Georgia, serif;
      font-size: clamp(30px, 4vw, 44px); font-weight: 500;
      line-height: 1.14; letter-spacing: -0.016em;
      color: var(--text-primary);
    }
    .section-lede {
      margin: 0; max-width: 32em; font-size: 17px;
      line-height: 1.65; color: var(--text-body);
    }

    /* Component rows */
    .components { display: grid; gap: 0; max-width: 920px; }
    .comp {
      display: flex;
      gap: 16px;
      padding: 20px 0;
      border-top: 1px solid var(--rule-soft);
    }
    .comp:first-child { border-top: 0; }
    .comp-icon {
      flex: 0 0 40px;
      display: inline-flex; align-items: center; justify-content: center;
      width: 40px; height: 40px; color: var(--text-secondary);
      background: var(--bg-soft); border-radius: 10px;
      box-shadow: 0 0 0 1px var(--rule);
      margin-top: 2px;
    }
    .comp-icon svg { width: 20px; height: 20px; }
    .comp-main { flex: 1 1 auto; min-width: 0; }
    .comp-head {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 16px;
      min-width: 0;
    }
    .comp-name {
      margin: 0;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 19px; font-weight: 500; line-height: 1.25;
      letter-spacing: -0.008em; color: var(--text-primary);
      min-width: 0;
      word-break: break-word;
    }
    .comp-summary { margin: 4px 0 0; font-size: 14.5px; color: var(--text-secondary); line-height: 1.55; }
    .comp-meta {
      margin-top: 8px; display: flex; flex-wrap: wrap; gap: 6px 16px;
      font-size: 12.5px; color: var(--text-tertiary);
      align-items: center;
    }
    .comp-meta code {
      font-family: "Anthropic Mono", "SF Mono", Consolas, monospace;
      font-size: 11.5px;
      padding: 2px 7px; border-radius: 6px;
      background: var(--bg-soft);
      color: var(--text-body);
      box-shadow: inset 0 0 0 1px var(--rule);
      letter-spacing: 0;
    }
    .status-pill {
      flex: 0 0 auto;
      display: inline-flex; align-items: center; gap: 7px;
      padding: 4px 10px; border-radius: 999px;
      font-size: 12px; font-weight: 500; white-space: nowrap;
      line-height: 1.4;
      transform: translateY(-1px);
    }
    .status-pill .dot { width: 6px; height: 6px; border-radius: 50%; }
    .status-pill.good { background: var(--status-good-soft); color: var(--status-good); }
    .status-pill.good .dot { background: var(--status-good); }
    .status-pill.warn { background: var(--status-warn-soft); color: var(--status-warn); }
    .status-pill.warn .dot { background: var(--status-warn); }
    .status-pill.bad { background: var(--status-bad-soft); color: var(--status-bad); }
    .status-pill.bad .dot { background: var(--status-bad); }

    /* Resource bars */
    .resources {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 24px;
    }
    @media (max-width: 900px) { .resources { grid-template-columns: 1fr; } }
    .resource {
      padding: 22px 24px;
      background: var(--bg-ivory);
      border-radius: 14px;
      box-shadow: 0 0 0 1px var(--rule-soft);
    }
    .resource-head {
      display: flex; align-items: center; justify-content: space-between; gap: 10px;
      margin-bottom: 14px;
    }
    .resource-title {
      display: inline-flex; align-items: center; gap: 10px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 18px; font-weight: 500; color: var(--text-primary);
    }
    .resource-title .icon { width: 18px; height: 18px; color: var(--text-secondary); display: inline-flex; }
    .resource-title .icon svg { width: 100%; height: 100%; }
    .resource-value {
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 24px; font-weight: 500; letter-spacing: -0.01em;
      color: var(--text-primary); line-height: 1;
    }
    .resource-value .unit { font-size: 14px; font-style: italic; color: var(--text-secondary); margin-left: 3px; font-weight: 500; }
    .bar {
      position: relative; width: 100%; height: 6px;
      background: var(--rule-soft); border-radius: 999px; overflow: hidden;
    }
    .bar-fill {
      position: absolute; left: 0; top: 0; bottom: 0; border-radius: 999px;
      background: var(--brand);
    }
    .bar-fill.warn { background: var(--status-warn); }
    .bar-fill.bad { background: var(--status-bad); }
    .resource-hint {
      margin-top: 10px; font-size: 12.5px; color: var(--text-tertiary);
    }

    /* Runtime key-value grid */
    .kv {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px 32px;
    }
    @media (max-width: 900px) { .kv { grid-template-columns: repeat(2, 1fr); } }
    @media (max-width: 620px) { .kv { grid-template-columns: 1fr; } }
    .kv-row {
      display: grid;
      grid-template-columns: 120px 1fr;
      gap: 12px;
      padding: 12px 0;
      border-top: 1px solid var(--rule-soft);
      font-size: 14px;
    }
    .kv-row dt {
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      color: var(--text-secondary);
    }
    .kv-row dd {
      margin: 0;
      font-family: "Anthropic Mono", "SF Mono", Consolas, monospace;
      font-size: 13px; color: var(--text-body); letter-spacing: 0;
      overflow-wrap: anywhere;
    }

    /* Footer */
    footer.foot { padding: 40px 0 56px; border-top: 1px solid var(--rule); color: var(--text-tertiary); }
    .foot-meta {
      display: flex; flex-wrap: wrap; align-items: center;
      gap: 10px 16px;
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      font-size: 13px;
    }
    .foot-meta .sep { color: var(--rule); font-style: normal; }
    .foot-meta .grow { flex: 1; }
    .foot-meta code {
      font-family: "Anthropic Mono", "SF Mono", Consolas, monospace;
      font-style: normal; font-size: 12px;
      padding: 2px 7px; margin-left: 4px;
      background: var(--bg-soft); color: var(--text-body);
      border-radius: 6px;
      box-shadow: inset 0 0 0 1px var(--rule);
    }

    @media (max-width: 680px) {
      .container { padding: 0 24px; }
      .nav { padding: 22px 0; gap: 16px; }
      .nav-link-hide { display: none; }
      .section { padding: 44px 0; }
      .tiles { grid-template-columns: 1fr 1fr; }
      .comp-head { flex-direction: column; align-items: flex-start; gap: 8px; }
      .comp-icon { flex: 0 0 36px; width: 36px; height: 36px; }
      .comp-icon svg { width: 18px; height: 18px; }
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
        <a class="nav-link active nav-link-hide" href="/status">状态</a>
        <a class="nav-link nav-link-hide" href="/docs">文档</a>
        <a class="nav-link" href="/console">控制台</a>
        <a class="nav-cta" href="/login">进入</a>
      </div>
    </nav>

    <!-- Hero -->
    <section class="hero">
      <span class="hero-kicker">Platform Status</span>
      <hr class="hero-rule" aria-hidden="true">
      <h1>{{.OverallLabel}}</h1>
      <div>
        <span class="overall-badge {{.OverallTone}}"><span class="pulse" aria-hidden="true"></span>{{.OverallCaption}}</span>
      </div>
      <p class="hero-sub">
        {{.Data.Summary}}
        <a class="hero-refresh" href="/status"><span class="icon">` + iconRefresh + `</span>刷新数据</a>
      </p>

      <!-- Stat tiles -->
      <div class="tiles">
        <div class="tile">
          <div class="tile-label">Availability</div>
          <div class="tile-value">{{.AvailabilityFX}}</div>
          <div class="tile-hint">健康组件占比</div>
        </div>
        <div class="tile">
          <div class="tile-label">Health Score</div>
          <div class="tile-value">{{.Data.Score}}<span class="unit">/ 100</span></div>
          <div class="tile-hint">可用 {{.Data.Counts.Available}} · 降级 {{.Data.Counts.Degraded}} · 不可用 {{.Data.Counts.Unavailable}}</div>
        </div>
        <div class="tile">
          <div class="tile-label">Uptime</div>
          <div class="tile-value">{{.UptimeHuman}}</div>
          <div class="tile-hint">自 {{.Data.Runtime.StartedAt.Local.Format "2006-01-02 15:04"}} 起</div>
        </div>
        <div class="tile">
          <div class="tile-label">Build</div>
          <div class="tile-value">{{if .Data.Runtime.Version}}{{.Data.Runtime.Version}}{{else}}dev{{end}}</div>
          <div class="tile-hint">{{.Data.Runtime.Environment}} · {{.Data.Runtime.GoVersion}}</div>
        </div>
      </div>
    </section>

    <!-- Infrastructure -->
    {{if .Data.Infrastructure}}
    <section class="section" aria-labelledby="infra-title">
      <header class="section-head">
        <div class="section-kicker">Infrastructure</div>
        <h2 class="section-title" id="infra-title">基础设施</h2>
        <p class="section-lede">数据库、缓存、消息总线与工作流引擎的实时可用性。</p>
      </header>
      <div class="components">
        {{range .Data.Infrastructure}}
        <article class="comp">
          <span class="comp-icon">{{iconFor .Key}}</span>
          <div class="comp-main">
            <div class="comp-head">
              <h3 class="comp-name">{{.Name}}</h3>
              <span class="status-pill {{if hasGoodStatus .Status}}good{{else if isDegraded .Status}}warn{{else}}bad{{end}}">
                <span class="dot" aria-hidden="true"></span>{{statusCn .Status}}
              </span>
            </div>
            {{if .Summary}}<p class="comp-summary">{{.Summary}}</p>{{end}}
            <div class="comp-meta">
              <code>{{.Key}}</code>
              {{if .Severity}}<span>Severity · {{.Severity}}</span>{{end}}
              {{if .DependsOn}}<span>Depends · {{dependsJoin .DependsOn}}</span>{{end}}
              <span>Checked · {{.CheckedAt.Local.Format "15:04:05"}}</span>
            </div>
          </div>
        </article>
        {{end}}
      </div>
    </section>
    {{end}}

    <!-- Modules -->
    {{if .Data.Modules}}
    <section class="section" aria-labelledby="mod-title">
      <header class="section-head">
        <div class="section-kicker">System Modules</div>
        <h2 class="section-title" id="mod-title">系统模块</h2>
        <p class="section-lede">业务域服务的实时状态。一个模块的降级不会影响其他模块。</p>
      </header>
      <div class="components">
        {{range .Data.Modules}}
        <article class="comp">
          <span class="comp-icon">{{iconFor .Key}}</span>
          <div class="comp-main">
            <div class="comp-head">
              <h3 class="comp-name">{{.Name}}</h3>
              <span class="status-pill {{if hasGoodStatus .Status}}good{{else if isDegraded .Status}}warn{{else}}bad{{end}}">
                <span class="dot" aria-hidden="true"></span>{{statusCn .Status}}
              </span>
            </div>
            {{if .Summary}}<p class="comp-summary">{{.Summary}}</p>{{end}}
            <div class="comp-meta">
              <code>{{.Key}}</code>
              {{if .DependsOn}}<span>Depends · {{dependsJoin .DependsOn}}</span>{{end}}
              <span>Checked · {{.CheckedAt.Local.Format "15:04:05"}}</span>
            </div>
          </div>
        </article>
        {{end}}
      </div>
    </section>
    {{end}}

    <!-- Resources -->
    <section class="section" aria-labelledby="res-title">
      <header class="section-head">
        <div class="section-kicker">Resource Utilization</div>
        <h2 class="section-title" id="res-title">系统资源</h2>
        <p class="section-lede">宿主机 CPU、内存、磁盘占用情况。高于 85% 会进入预警区。</p>
      </header>
      <div class="resources">
        <div class="resource">
          <div class="resource-head">
            <span class="resource-title"><span class="icon">` + iconCpu + `</span>CPU</span>
            <span class="resource-value">{{pct .Data.Runtime.CPUUsage}}</span>
          </div>
          <div class="bar"><span class="bar-fill {{if gt .Data.Runtime.CPUUsage 85.0}}bad{{else if gt .Data.Runtime.CPUUsage 70.0}}warn{{end}}" style="width: {{pct .Data.Runtime.CPUUsage}};"></span></div>
          <div class="resource-hint">{{.Data.Runtime.CPUCores}} cores · {{.Data.Runtime.CPUThreads}} threads</div>
        </div>
        <div class="resource">
          <div class="resource-head">
            <span class="resource-title"><span class="icon">` + iconMemory + `</span>Memory</span>
            <span class="resource-value">{{pct .Data.Runtime.MemUsedPct}}</span>
          </div>
          <div class="bar"><span class="bar-fill {{if gt .Data.Runtime.MemUsedPct 85.0}}bad{{else if gt .Data.Runtime.MemUsedPct 70.0}}warn{{end}}" style="width: {{pct .Data.Runtime.MemUsedPct}};"></span></div>
          <div class="resource-hint">{{.MemUsedHuman}} / {{.MemHuman}}</div>
        </div>
        <div class="resource">
          <div class="resource-head">
            <span class="resource-title"><span class="icon">` + iconHardDrive + `</span>Disk</span>
            <span class="resource-value">{{pct .Data.Runtime.DiskUsedPct}}</span>
          </div>
          <div class="bar"><span class="bar-fill {{if gt .Data.Runtime.DiskUsedPct 85.0}}bad{{else if gt .Data.Runtime.DiskUsedPct 70.0}}warn{{end}}" style="width: {{pct .Data.Runtime.DiskUsedPct}};"></span></div>
          <div class="resource-hint">{{.DiskUsedHuman}} / {{.DiskHuman}}</div>
        </div>
      </div>
    </section>

    <!-- Runtime details -->
    <section class="section" aria-labelledby="rt-title">
      <header class="section-head">
        <div class="section-kicker">Runtime Details</div>
        <h2 class="section-title" id="rt-title">运行态详情</h2>
        <p class="section-lede">进程与宿主机的详细信息，用于排障与审计。</p>
      </header>
      <dl class="kv">
        <div class="kv-row"><dt>App</dt><dd>{{.Data.Runtime.AppName}}</dd></div>
        <div class="kv-row"><dt>Environment</dt><dd>{{.Data.Runtime.Environment}}</dd></div>
        <div class="kv-row"><dt>Version</dt><dd>{{if .Data.Runtime.Version}}{{.Data.Runtime.Version}}{{else}}dev{{end}}</dd></div>

        <div class="kv-row"><dt>Hostname</dt><dd>{{.Data.Runtime.Hostname}}</dd></div>
        <div class="kv-row"><dt>Platform</dt><dd>{{.Data.Runtime.Platform}} ({{.Data.Runtime.OS}})</dd></div>
        <div class="kv-row"><dt>Kernel Arch</dt><dd>{{.Data.Runtime.KernelArch}}</dd></div>

        <div class="kv-row"><dt>Go Runtime</dt><dd>{{.Data.Runtime.GoVersion}} · {{.Data.Runtime.GoOS}}/{{.Data.Runtime.GoArch}}</dd></div>
        <div class="kv-row"><dt>Goroutines</dt><dd>{{.Data.Runtime.Goroutines}}</dd></div>
        <div class="kv-row"><dt>Timezone</dt><dd>{{.Data.Runtime.Timezone}}</dd></div>

        <div class="kv-row"><dt>CPU</dt><dd>{{.Data.Runtime.CPUModel}}</dd></div>
        <div class="kv-row"><dt>Process Memory</dt><dd>{{.ProcMemHuman}}</dd></div>
        <div class="kv-row"><dt>PID</dt><dd>{{.Data.Runtime.PID}}</dd></div>
      </dl>
    </section>

    <!-- Footer -->
    <footer class="foot">
      <div class="foot-meta">
        <span>Checked at {{.CheckedHuman}}</span>
        <span class="sep">·</span>
        <span>Aegis Platform</span>
        {{if .RequestID}}<span class="sep">·</span><span>Request ID <code>{{.RequestID}}</code></span>{{end}}
        <span class="grow"></span>
        <span>© {{.Year}}</span>
      </div>
    </footer>
  </div>
</body>
</html>`
