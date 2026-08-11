package response

import (
	"bytes"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────
// HTML 系统公告页 (/announcements) — Claude / Anthropic 设计语言
// ─────────────────────────────────────────────────────────────────────

// AnnouncementItem 单条公告（与 domain/system.Announcement 字段对齐）。
type AnnouncementItem struct {
	ID          int64
	Type        string     // info / warning / maintenance / update / security
	Title       string
	Content     string
	Level       string     // normal / important / critical
	Pinned      bool
	AdminName   string
	PublishedAt *time.Time
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AnnouncementsPageData 渲染公告页所需数据。
type AnnouncementsPageData struct {
	Items     []AnnouncementItem
	CheckedAt time.Time
	Summary   string // 可选：首页/状态提示摘要
}

// RenderAnnouncementsPage 渲染系统公告页。
func RenderAnnouncementsPage(c *gin.Context, data AnnouncementsPageData) {
	setMetaHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=30")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'")

	if data.CheckedAt.IsZero() {
		data.CheckedAt = time.Now()
	}
	sort.SliceStable(data.Items, func(i, j int) bool {
		a, b := data.Items[i], data.Items[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		at := a.CreatedAt
		if a.PublishedAt != nil {
			at = *a.PublishedAt
		}
		bt := b.CreatedAt
		if b.PublishedAt != nil {
			bt = *b.PublishedAt
		}
		return at.After(bt)
	})

	view := announcementsView{
		Data:      data,
		RequestID: requestID(c),
		Year:      time.Now().Year(),
	}
	view.Derive()

	var buf bytes.Buffer
	if err := announcementsTemplate.Execute(&buf, view); err != nil {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		_, _ = c.Writer.WriteString(fmt.Sprintf("Aegis — %d 条公告", len(data.Items)))
		return
	}
	c.Status(200)
	_, _ = c.Writer.Write(buf.Bytes())
}

type announcementsView struct {
	Data      AnnouncementsPageData
	RequestID string
	Year      int

	// 派生
	Total         int
	PinnedCount   int
	CriticalCount int
	ImportantCount int
	CheckedHuman  string
}

func (v *announcementsView) Derive() {
	v.Total = len(v.Data.Items)
	for _, it := range v.Data.Items {
		if it.Pinned {
			v.PinnedCount++
		}
		switch strings.ToLower(it.Level) {
		case "critical":
			v.CriticalCount++
		case "important":
			v.ImportantCount++
		}
	}
	v.CheckedHuman = v.Data.CheckedAt.Local().Format("2006-01-02 15:04:05")
}

// ─── 模板 ───

var announcementsTemplate = template.Must(template.New("announcements").Funcs(template.FuncMap{
	"typeIcon":       iconForAnnouncementType,
	"typeLabel":      announcementTypeLabel,
	"levelClass":     levelClass,
	"levelLabel":     levelLabel,
	"fmtTime":        formatAnnouncementTime,
	"fmtRelative":    formatRelative,
	"deref":          func(t *time.Time) time.Time { if t == nil { return time.Time{} }; return *t },
	"isNotZeroTime":  func(t *time.Time) bool { return t != nil && !t.IsZero() },
	"hasContent":     func(s string) bool { return strings.TrimSpace(s) != "" },
}).Parse(announcementsPageSource))

func iconForAnnouncementType(t string) template.HTML {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "warning":
		return template.HTML(iconAlertTriangleAnn)
	case "maintenance":
		return template.HTML(iconWrenchAnn)
	case "update":
		return template.HTML(iconSparklesAnn)
	case "security":
		return template.HTML(iconShieldAnn)
	case "info":
		fallthrough
	default:
		return template.HTML(iconInfoAnn)
	}
}

func announcementTypeLabel(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "warning":
		return "Warning"
	case "maintenance":
		return "Maintenance"
	case "update":
		return "Update"
	case "security":
		return "Security"
	case "info":
		return "Information"
	default:
		if t == "" {
			return "Information"
		}
		return t
	}
}

func levelClass(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return "bad"
	case "important":
		return "warn"
	default:
		return "good"
	}
}

func levelLabel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return "紧急"
	case "important":
		return "重要"
	case "normal":
		return "常规"
	default:
		if level == "" {
			return "常规"
		}
		return level
	}
}

func formatAnnouncementTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

func formatRelative(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours())/24)
	default:
		return t.Local().Format("2006-01-02")
	}
}

// Lucide 图标（stroke-width 1.6）
const (
	iconAlertTriangleAnn = svgHead + `<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>`
	iconWrenchAnn        = svgHead + `<path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>`
	iconSparklesAnn      = svgHead + `<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/><path d="M4 17v2"/><path d="M5 18H3"/></svg>`
	iconShieldAnn        = svgHead + `<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/></svg>`
	iconInfoAnn          = svgHead + `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`
	iconPinAnn           = svgHead + `<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/></svg>`
	iconCalendarAnn      = svgHead + `<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/></svg>`
	iconUserAnn          = svgHead + `<path d="M18 20a6 6 0 0 0-12 0"/><circle cx="12" cy="10" r="4"/><circle cx="12" cy="12" r="10"/></svg>`
	iconArchiveAnn       = svgHead + `<rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/></svg>`
	iconBellOffAnn       = svgHead + `<path d="M8.7 3A6 6 0 0 1 18 8a21.3 21.3 0 0 0 .6 5"/><path d="M17 17H3s3-2 3-9a4.67 4.67 0 0 1 .3-1.7"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/><path d="m2 2 20 20"/></svg>`
)

const announcementsPageSource = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="Aegis 系统公告 — 维护、更新与安全通告">
  <meta name="theme-color" content="#f5f4ed" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#141413" media="(prefers-color-scheme: dark)">
  <title>系统公告 — Aegis</title>
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
      --level-good: #6f9a5f;
      --level-good-soft: #e6efdf;
      --level-warn: #c38435;
      --level-warn-soft: #f4e6d0;
      --level-bad: #b53333;
      --level-bad-soft: #f3dcdc;
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
        --level-good: #8fb97f;
        --level-good-soft: #2a3527;
        --level-warn: #d99f56;
        --level-warn-soft: #3a2e1d;
        --level-bad: #d06a6a;
        --level-bad-soft: #3a2424;
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
    .hero { padding: 56px 0 40px; max-width: 860px; }
    .hero-kicker {
      display: inline-flex; align-items: center; gap: 8px;
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      font-size: 15px; color: var(--brand);
    }
    .hero-kicker::before { content: ""; width: 6px; height: 6px; background: var(--brand); border-radius: 50%; }
    .hero-rule { width: 48px; height: 1px; margin: 22px 0 28px; background: var(--brand); border: 0; }
    .hero h1 {
      margin: 0;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: clamp(40px, 5.8vw, 64px);
      font-weight: 500; line-height: 1.08; letter-spacing: -0.02em;
    }
    .hero-lede {
      margin: 24px 0 0; max-width: 36em; font-size: 18px;
      line-height: 1.64; color: var(--text-body);
    }

    /* Tiles */
    .tiles {
      display: grid; grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 16px; margin-top: 36px;
    }
    @media (max-width: 900px) { .tiles { grid-template-columns: repeat(2, 1fr); } }
    .tile {
      padding: 20px 22px 18px;
      background: var(--bg-ivory); border-radius: 14px;
      box-shadow: 0 0 0 1px var(--rule-soft);
    }
    .tile-label { font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 13px; color: var(--text-secondary); }
    .tile-value {
      margin-top: 6px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 30px; font-weight: 500; line-height: 1.1; letter-spacing: -0.01em;
    }
    .tile-value .unit { font-size: 13px; font-style: italic; color: var(--text-secondary); font-weight: 500; margin-left: 4px; }
    .tile-hint { margin-top: 4px; font-size: 12.5px; color: var(--text-tertiary); }

    /* Timeline / list */
    .section { padding: 56px 0; border-top: 1px solid var(--rule); }
    .section-head { margin-bottom: 40px; max-width: 720px; }
    .section-kicker {
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      font-size: 14px; color: var(--brand);
    }
    .section-title {
      margin: 14px 0 14px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: clamp(30px, 4vw, 44px); font-weight: 500;
      line-height: 1.14; letter-spacing: -0.017em;
    }
    .section-lede { margin: 0; max-width: 32em; font-size: 17px; line-height: 1.65; color: var(--text-body); }

    .announcements { display: grid; gap: 0; max-width: 920px; }
    .ann {
      display: flex; gap: 20px;
      padding: 28px 0;
      border-top: 1px solid var(--rule-soft);
      position: relative;
    }
    .ann:first-child { border-top: 0; }
    .ann.pinned::before {
      content: ""; position: absolute; top: 28px; bottom: 28px; left: -10px;
      width: 2px; border-radius: 2px; background: var(--brand);
    }

    .ann-icon {
      flex: 0 0 44px;
      display: inline-flex; align-items: center; justify-content: center;
      width: 44px; height: 44px; color: var(--text-secondary);
      background: var(--bg-soft); border-radius: 12px;
      box-shadow: 0 0 0 1px var(--rule);
      margin-top: 2px;
    }
    .ann-icon.good { color: var(--level-good); }
    .ann-icon.warn { color: var(--level-warn); }
    .ann-icon.bad { color: var(--level-bad); }
    .ann-icon svg { width: 22px; height: 22px; }

    .ann-main { flex: 1 1 auto; min-width: 0; }

    .ann-head {
      display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px;
      margin-bottom: 8px;
    }
    .ann-type {
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic; font-size: 13px;
      color: var(--brand); letter-spacing: 0.01em;
    }
    .ann-level {
      display: inline-flex; align-items: center; gap: 6px;
      padding: 3px 10px; border-radius: 999px;
      font-size: 11.5px; font-weight: 500;
      line-height: 1.4;
    }
    .ann-level .dot { width: 6px; height: 6px; border-radius: 50%; }
    .ann-level.good { background: var(--level-good-soft); color: var(--level-good); }
    .ann-level.good .dot { background: var(--level-good); }
    .ann-level.warn { background: var(--level-warn-soft); color: var(--level-warn); }
    .ann-level.warn .dot { background: var(--level-warn); }
    .ann-level.bad { background: var(--level-bad-soft); color: var(--level-bad); }
    .ann-level.bad .dot { background: var(--level-bad); }
    .ann-pinned {
      display: inline-flex; align-items: center; gap: 6px;
      font-family: "Anthropic Serif", Georgia, serif;
      font-style: italic; font-size: 12.5px; color: var(--brand);
    }
    .ann-pinned svg { width: 12px; height: 12px; }

    .ann-title {
      margin: 0;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: clamp(22px, 2.4vw, 28px); font-weight: 500;
      line-height: 1.22; letter-spacing: -0.012em;
      color: var(--text-primary);
      word-break: break-word;
    }
    .ann-content {
      margin: 12px 0 0;
      font-size: 15.5px; line-height: 1.72; color: var(--text-body);
      white-space: pre-wrap;
      word-break: break-word;
    }

    .ann-meta {
      margin-top: 16px;
      display: flex; flex-wrap: wrap; gap: 6px 20px;
      font-family: "Anthropic Serif", Georgia, serif; font-style: italic;
      font-size: 13px; color: var(--text-tertiary);
    }
    .ann-meta .item { display: inline-flex; align-items: center; gap: 6px; }
    .ann-meta .item svg { width: 13px; height: 13px; }
    .ann-meta .sep { color: var(--rule); font-style: normal; }

    /* Empty state */
    .empty {
      padding: 80px 0;
      text-align: center;
      max-width: 520px;
      margin: 0 auto;
    }
    .empty-icon {
      display: inline-flex; align-items: center; justify-content: center;
      width: 56px; height: 56px; color: var(--text-tertiary);
      background: var(--bg-soft); border-radius: 16px;
      box-shadow: 0 0 0 1px var(--rule);
      margin-bottom: 24px;
    }
    .empty-icon svg { width: 28px; height: 28px; }
    .empty-title {
      margin: 0;
      font-family: "Anthropic Serif", Georgia, serif;
      font-size: 26px; font-weight: 500; letter-spacing: -0.01em;
    }
    .empty-body {
      margin: 12px 0 0; font-size: 16px; line-height: 1.65;
      color: var(--text-secondary);
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
      .nav-link-hide { display: none; }
      .hero { padding: 40px 0 32px; }
      .section { padding: 44px 0; }
      .ann { gap: 14px; padding: 22px 0; }
      .ann-icon { flex: 0 0 36px; width: 36px; height: 36px; }
      .ann-icon svg { width: 18px; height: 18px; }
      .ann-title { font-size: 20px; }
      .ann.pinned::before { left: -8px; top: 22px; bottom: 22px; }
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
        <a class="nav-link active nav-link-hide" href="/announcements">公告</a>
        <a class="nav-link nav-link-hide" href="/status">状态</a>
        <a class="nav-link nav-link-hide" href="/docs">文档</a>
        <a class="nav-link" href="/console">控制台</a>
        <a class="nav-cta" href="/login">进入</a>
      </div>
    </nav>

    <!-- Hero -->
    <section class="hero">
      <span class="hero-kicker">System Announcements</span>
      <hr class="hero-rule" aria-hidden="true">
      <h1>平台公告</h1>
      <p class="hero-lede">
        {{if gt .Total 0}}
          当前有 {{.Total}} 条有效公告
          {{if gt .PinnedCount 0}}（{{.PinnedCount}} 条置顶{{end}}{{if gt .CriticalCount 0}}，{{.CriticalCount}} 条紧急{{end}}{{if gt .ImportantCount 0}}，{{.ImportantCount}} 条重要{{end}}{{if gt .PinnedCount 0}}）{{end}}。请及时关注维护时间窗、安全更新与功能发布。
        {{else}}
          系统当前没有进行中的公告。若有计划维护或重大更新，将在此汇总呈现。
        {{end}}
      </p>

      <!-- Tiles -->
      <div class="tiles">
        <div class="tile">
          <div class="tile-label">Total</div>
          <div class="tile-value">{{.Total}}</div>
          <div class="tile-hint">有效公告数</div>
        </div>
        <div class="tile">
          <div class="tile-label">Pinned</div>
          <div class="tile-value">{{.PinnedCount}}</div>
          <div class="tile-hint">置顶条目</div>
        </div>
        <div class="tile">
          <div class="tile-label">Critical</div>
          <div class="tile-value">{{.CriticalCount}}</div>
          <div class="tile-hint">紧急级别</div>
        </div>
        <div class="tile">
          <div class="tile-label">Important</div>
          <div class="tile-value">{{.ImportantCount}}</div>
          <div class="tile-hint">重要级别</div>
        </div>
      </div>
    </section>

    <!-- List -->
    <section class="section" aria-labelledby="list-title">
      <header class="section-head">
        <div class="section-kicker">Latest Updates</div>
        <h2 class="section-title" id="list-title">最新公告</h2>
        <p class="section-lede">置顶条目会置于最上方，其余按发布时间倒序排列。</p>
      </header>

      {{if gt .Total 0}}
      <div class="announcements">
        {{range .Data.Items}}
        <article class="ann {{if .Pinned}}pinned{{end}}">
          <span class="ann-icon {{levelClass .Level}}">{{typeIcon .Type}}</span>
          <div class="ann-main">
            <div class="ann-head">
              <span class="ann-type">{{typeLabel .Type}}</span>
              <span class="ann-level {{levelClass .Level}}">
                <span class="dot" aria-hidden="true"></span>{{levelLabel .Level}}
              </span>
              {{if .Pinned}}
              <span class="ann-pinned">` + iconPinAnn + `置顶</span>
              {{end}}
            </div>
            <h3 class="ann-title">{{.Title}}</h3>
            {{if hasContent .Content}}<div class="ann-content">{{.Content}}</div>{{end}}
            <div class="ann-meta">
              {{if isNotZeroTime .PublishedAt}}
              <span class="item">` + iconCalendarAnn + `发布于 {{fmtTime .PublishedAt}}{{with fmtRelative .PublishedAt}} · {{.}}{{end}}</span>
              {{end}}
              {{if .AdminName}}
              <span class="item">` + iconUserAnn + `{{.AdminName}}</span>
              {{end}}
              {{if isNotZeroTime .ExpiresAt}}
              <span class="item">` + iconArchiveAnn + `有效期至 {{fmtTime .ExpiresAt}}</span>
              {{end}}
            </div>
          </div>
        </article>
        {{end}}
      </div>
      {{else}}
      <div class="empty">
        <div class="empty-icon">` + iconBellOffAnn + `</div>
        <h3 class="empty-title">暂无公告</h3>
        <p class="empty-body">一切如常。当平台有维护、更新或安全通告时，我们会在这里第一时间发布。</p>
      </div>
      {{end}}
    </section>

    <!-- Footer -->
    <footer class="foot">
      <div class="foot-meta">
        <span>Aegis Platform</span>
        <span class="sep">·</span>
        <span>Updated {{.CheckedHuman}}</span>
        {{if .RequestID}}<span class="sep">·</span><span>Request ID <code>{{.RequestID}}</code></span>{{end}}
        <span class="grow"></span>
        <span>© {{.Year}}</span>
      </div>
    </footer>
  </div>
</body>
</html>`
