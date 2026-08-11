package response

import (
	"bytes"
	"html/template"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────
// HTML 首页 — Claude / Anthropic 设计语言
//
// 左上角品牌 logo 使用共享的 Claude Code 风格终端思考动画
// (brandLogoAnimated) — 所有对外页面视觉一致。
// ─────────────────────────────────────────────────────────────────────

// HomePageData 首页可填充的动态数据。
type HomePageData struct {
	Version   string
	BuildTime string
	CommitSHA string
	Uptime    string
}

// RenderHomepage 把首页 HTML 写入响应。
func RenderHomepage(c *gin.Context, data HomePageData) {
	setMetaHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=60")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'")

	view := homePageView{
		Data:      data,
		RequestID: requestID(c),
		GoVersion: runtime.Version(),
		Year:      time.Now().Year(),
	}
	if view.Data.Version == "" {
		view.Data.Version = "dev"
	}
	if view.Data.Uptime == "" {
		view.Data.Uptime = formatUptime(time.Since(processStart))
	}

	var buf bytes.Buffer
	if err := homePageTemplate.Execute(&buf, view); err != nil {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		_, _ = c.Writer.WriteString("Aegis — " + view.Data.Version)
		return
	}
	c.Status(200)
	_, _ = c.Writer.Write(buf.Bytes())
}

type homePageView struct {
	Data      HomePageData
	RequestID string
	GoVersion string
	Year      int
}

var processStart = time.Now()

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return time.Duration(days*24).String() + " " + time.Duration(hours*int(time.Hour)+minutes*int(time.Minute)).String()
	case hours > 0:
		return time.Duration(hours*int(time.Hour) + minutes*int(time.Minute)).String()
	default:
		return d.String()
	}
}

var homePageTemplate = template.Must(template.New("home").Parse(homePageSource))

const homePageSource = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="Aegis — 多租户身份与访问平台">
  <meta name="theme-color" content="#f5f4ed" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#141413" media="(prefers-color-scheme: dark)">
  <title>Aegis — 多租户身份平台</title>
  <style>
    :root {
      --bg: #f5f4ed; --bg-soft: #eee9de; --bg-ivory: #faf9f5; --surface: #ffffff;
      --text-primary: #141413; --text-body: #3d3d3a; --text-secondary: #5e5d59; --text-tertiary: #87867f;
      --rule: #e8e6dc; --rule-soft: #f0eee6;
      --brand: #c96442; --brand-hover: #b55230; --brand-soft: #e8c9b7;
      --warm-sand: #e8e6dc; --ring: #d1cfc5; --ring-deep: #b9b7ac;
      --dark-bg: #141413; --dark-surface: #30302e; --dark-text: #f5f4ed; --dark-text-secondary: #b0aea5; --dark-rule: #30302e;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #141413; --bg-soft: #1c1c1a; --bg-ivory: #1c1c1a; --surface: #1c1c1a;
        --text-primary: #f5f4ed; --text-body: #e6e4d8; --text-secondary: #b0aea5; --text-tertiary: #87867f;
        --rule: #30302e; --rule-soft: #242422;
        --brand: #d97757; --brand-hover: #e78a65; --brand-soft: #4d3024;
        --warm-sand: #30302e; --ring: #3d3d3a; --ring-deep: #4d4c48;
      }
    }
    *, *::before, *::after { box-sizing: border-box; }
    html, body { min-height: 100%; }
    body { margin: 0; background: var(--bg); color: var(--text-primary); font-family: "Anthropic Sans", "Inter", "PingFang SC", "Microsoft YaHei UI", system-ui, sans-serif; font-size: 16px; line-height: 1.6; -webkit-font-smoothing: antialiased; }
    .container { max-width: 1120px; margin: 0 auto; padding: 0 40px; }
    .nav { padding: 28px 0; display: flex; align-items: center; justify-content: space-between; gap: 24px; }
    .nav-links { display: inline-flex; align-items: center; gap: 28px; }
    .nav-link { font-size: 14.5px; color: var(--text-secondary); text-decoration: none; border-bottom: 1px solid transparent; padding-bottom: 1px; transition: color 140ms linear, border-color 140ms linear; }
    .nav-link:hover { color: var(--text-primary); border-bottom-color: var(--brand); }
    .nav-cta { display: inline-flex; align-items: center; gap: 6px; padding: 8px 14px; font-size: 14px; font-weight: 500; color: #faf9f5; background: var(--brand); border-radius: 10px; text-decoration: none; box-shadow: 0 0 0 1px var(--brand); transition: background 140ms linear, box-shadow 140ms linear; }
    .nav-cta:hover { background: var(--brand-hover); box-shadow: 0 0 0 1px var(--brand-hover); }
    .nav-cta .icon { width: 14px; height: 14px; display: inline-flex; }
    .hero { padding: 72px 0 96px; max-width: 820px; }
    .hero-kicker { display: inline-flex; align-items: center; gap: 8px; font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 15px; color: var(--brand); letter-spacing: 0.01em; }
    .hero-kicker::before { content: ""; width: 6px; height: 6px; background: var(--brand); border-radius: 50%; }
    .hero-rule { width: 48px; height: 1px; margin: 22px 0 28px; background: var(--brand); border: 0; }
    .hero h1 { margin: 0; font-family: "Anthropic Serif", Georgia, "Times New Roman", serif; font-size: clamp(48px, 7vw, 80px); font-weight: 500; line-height: 1.05; letter-spacing: -0.022em; color: var(--text-primary); }
    .hero h1 .accent { color: var(--brand); }
    .hero-lede { margin: 32px 0 0; max-width: 34em; font-size: 20px; line-height: 1.60; color: var(--text-body); }
    .hero-actions { margin-top: 44px; display: inline-flex; flex-wrap: wrap; align-items: center; gap: 28px; }
    .btn-primary { display: inline-flex; align-items: center; gap: 8px; min-height: 46px; padding: 12px 22px; font-family: inherit; font-size: 15.5px; font-weight: 500; line-height: 1; color: #faf9f5; background: var(--brand); text-decoration: none; border: 0; border-radius: 12px; box-shadow: 0 0 0 1px var(--brand); transition: background 140ms linear, box-shadow 140ms linear; }
    .btn-primary:hover { background: var(--brand-hover); box-shadow: 0 0 0 1px var(--brand-hover); }
    .btn-primary .icon { width: 16px; height: 16px; display: inline-flex; }
    .link-serif { display: inline-flex; align-items: baseline; gap: 6px; font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 16.5px; color: var(--text-secondary); text-decoration: none; border-bottom: 1px solid transparent; padding-bottom: 1px; transition: color 140ms linear, border-color 140ms linear; }
    .link-serif:hover { color: var(--text-primary); border-bottom-color: var(--brand); }
    .link-serif .arrow { font-family: "Anthropic Sans", system-ui, sans-serif; font-style: normal; }
    .section { padding: 80px 0; border-top: 1px solid var(--rule); }
    .section-head { max-width: 720px; margin-bottom: 48px; }
    .section-kicker { font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 14px; color: var(--brand); letter-spacing: 0.02em; }
    .section-title { margin: 14px 0 18px; font-family: "Anthropic Serif", Georgia, serif; font-size: clamp(34px, 4.5vw, 52px); font-weight: 500; line-height: 1.12; letter-spacing: -0.018em; color: var(--text-primary); }
    .section-lede { margin: 0; max-width: 32em; font-size: 18px; line-height: 1.65; color: var(--text-body); }
    .caps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 24px; }
    @media (max-width: 900px) { .caps { grid-template-columns: 1fr; } }
    .cap { padding: 28px 28px 32px; background: var(--bg-ivory); border-radius: 16px; box-shadow: 0 0 0 1px var(--rule-soft); }
    .cap-icon { display: inline-flex; align-items: center; justify-content: center; width: 44px; height: 44px; color: var(--brand); background: var(--bg-soft); border-radius: 12px; box-shadow: 0 0 0 1px var(--rule); margin-bottom: 20px; }
    .cap-icon svg { width: 22px; height: 22px; }
    .cap-title { margin: 0 0 10px; font-family: "Anthropic Serif", Georgia, serif; font-size: 24px; font-weight: 500; line-height: 1.20; letter-spacing: -0.01em; color: var(--text-primary); }
    .cap-body { margin: 0; font-size: 15.5px; line-height: 1.65; color: var(--text-secondary); }
    .cap-tags { margin: 18px 0 0; padding: 0; display: flex; flex-wrap: wrap; gap: 8px; list-style: none; }
    .cap-tag { font-family: "Anthropic Mono", "SF Mono", Consolas, monospace; font-size: 11.5px; color: var(--text-tertiary); padding: 3px 9px; background: var(--bg-soft); border-radius: 6px; box-shadow: inset 0 0 0 1px var(--rule); }
    .modules { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px 36px; }
    @media (max-width: 900px) { .modules { grid-template-columns: 1fr 1fr; } }
    @media (max-width: 620px) { .modules { grid-template-columns: 1fr; gap: 0; } .modules > .mod { padding: 16px 0; gap: 12px; } .modules > .mod:first-child { border-top: 0; padding-top: 0; } .modules > .mod:last-child { padding-bottom: 0; } .mod-icon { width: 28px; height: 28px; } .mod-icon svg { width: 18px; height: 18px; } }
    .mod { display: flex; align-items: flex-start; gap: 14px; padding: 18px 0; border-top: 1px solid var(--rule-soft); }
    .mod-icon { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; color: var(--text-secondary); margin-top: 2px; flex-shrink: 0; }
    .mod-icon svg { width: 20px; height: 20px; }
    .mod-body { min-width: 0; }
    .mod-title { margin: 0 0 4px; font-family: "Anthropic Serif", Georgia, serif; font-size: 18px; font-weight: 500; line-height: 1.25; letter-spacing: -0.008em; color: var(--text-primary); }
    .mod-desc { margin: 0; font-size: 14.5px; line-height: 1.6; color: var(--text-secondary); }
    .cta { margin-top: 40px; padding: 80px 0; background: var(--dark-bg); color: var(--dark-text); border-top: 1px solid var(--dark-rule); border-bottom: 1px solid var(--dark-rule); }
    .cta .container { display: grid; grid-template-columns: 1.2fr 1fr; gap: 40px; align-items: center; }
    @media (max-width: 900px) { .cta .container { grid-template-columns: 1fr; } }
    .cta-kicker { font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 14px; color: #d97757; letter-spacing: 0.02em; }
    .cta-title { margin: 14px 0 0; font-family: "Anthropic Serif", Georgia, serif; font-size: clamp(32px, 4.5vw, 48px); font-weight: 500; line-height: 1.12; letter-spacing: -0.018em; color: var(--dark-text); }
    .cta-body { margin: 18px 0 0; max-width: 32em; font-size: 17px; line-height: 1.65; color: var(--dark-text-secondary); }
    .cta-actions { display: inline-flex; flex-wrap: wrap; align-items: center; gap: 28px; }
    .cta-btn { display: inline-flex; align-items: center; gap: 8px; padding: 13px 22px; background: var(--dark-text); color: var(--dark-bg); border-radius: 12px; font-size: 15.5px; font-weight: 500; text-decoration: none; transition: background 140ms linear; }
    .cta-btn:hover { background: #e8c9b7; }
    .cta-btn .icon { width: 16px; height: 16px; display: inline-flex; }
    .cta-link { font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 16px; color: var(--dark-text-secondary); text-decoration: none; border-bottom: 1px solid transparent; transition: color 140ms linear, border-color 140ms linear; }
    .cta-link:hover { color: var(--dark-text); border-bottom-color: #d97757; }
    footer.foot { padding: 48px 0 56px; background: var(--bg); color: var(--text-tertiary); font-size: 13.5px; }
    .foot-grid { display: grid; grid-template-columns: 2fr 1fr 1fr 1fr; gap: 40px; padding-bottom: 36px; }
    @media (max-width: 900px) { .foot-grid { grid-template-columns: 1fr 1fr; } }
    .foot-brand-mark { display: inline-flex; align-items: center; gap: 10px; font-family: "Anthropic Serif", Georgia, serif; font-size: 20px; font-weight: 500; color: var(--text-primary); margin-bottom: 16px; }
    .foot-brand-mark .dot { display: inline-block; width: 9px; height: 9px; background: var(--brand); border-radius: 50%; }
    .foot-tagline { max-width: 30em; font-size: 14px; line-height: 1.6; color: var(--text-secondary); }
    .foot-col h4 { margin: 0 0 14px; font-family: "Anthropic Serif", Georgia, serif; font-size: 13px; font-style: italic; font-weight: 500; color: var(--text-secondary); letter-spacing: 0.02em; }
    .foot-col ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 10px; }
    .foot-col a { color: var(--text-body); text-decoration: none; font-size: 14px; border-bottom: 1px solid transparent; padding-bottom: 1px; transition: color 140ms linear, border-color 140ms linear; }
    .foot-col a:hover { color: var(--text-primary); border-bottom-color: var(--brand); }
    .foot-meta { padding-top: 24px; border-top: 1px solid var(--rule); display: flex; flex-wrap: wrap; align-items: center; gap: 10px 16px; font-family: "Anthropic Serif", Georgia, serif; font-style: italic; font-size: 13px; color: var(--text-tertiary); }
    .foot-meta .sep { color: var(--rule); font-style: normal; }
    .foot-meta code { font-family: "Anthropic Mono", "SF Mono", Consolas, monospace; font-style: normal; font-size: 12px; padding: 2px 7px; margin-left: 4px; background: var(--bg-soft); color: var(--text-body); border-radius: 6px; box-shadow: inset 0 0 0 1px var(--rule); }
    .foot-meta .status { display: inline-flex; align-items: center; gap: 6px; font-style: normal; }
    .foot-meta .status::before { content: ""; width: 7px; height: 7px; background: #6f9a5f; border-radius: 50%; }
    .foot-meta .grow { flex: 1; }
    @media (max-width: 680px) { .container { padding: 0 24px; } .nav { padding: 22px 0; gap: 16px; } .nav-links { gap: 16px; } .nav-link { font-size: 13.5px; } .nav-link-hide { display: none; } .hero { padding: 48px 0 64px; } .hero h1 { font-size: clamp(38px, 10vw, 56px); } .hero-lede { font-size: 17px; } .section { padding: 56px 0; } .cta { padding: 56px 0; } .foot-grid { gap: 28px; padding-bottom: 28px; } }
    ` + BrandLogoCSS + `
  </style>
</head>
<body>
  <div class="container">
    <nav class="nav" aria-label="主导航">
      ` + BrandLogoHTML + `
      <div class="nav-links">
        <a class="nav-link nav-link-hide" href="/announcements">公告</a>
        <a class="nav-link nav-link-hide" href="/status">状态</a>
        <a class="nav-link nav-link-hide" href="/cons-env">环境</a>
        <a class="nav-link nav-link-hide" href="/docs">文档</a>
        <a class="nav-link" href="/console">控制台</a>
        <a class="nav-cta" href="/login"><span>进入</span><span class="icon">` + iconArrowRight + `</span></a>
      </div>
    </nav>

    <section class="hero">
      <span class="hero-kicker">Multi-tenant Identity Platform</span>
      <hr class="hero-rule" aria-hidden="true">
      <h1>身份与访问，<br>以<span class="accent">专业</span>之名。</h1>
      <p class="hero-lede">
        Aegis 为现代应用提供完整的用户认证、多因子、权限模型与运行态可观测能力。
        一套平台，覆盖从登录到审计的全链路——安全、稳定、自洽。
      </p>
      <div class="hero-actions">
        <a class="btn-primary" href="/console"><span>进入控制台</span><span class="icon">` + iconArrowRight + `</span></a>
        <a class="link-serif" href="/docs"><span>阅读技术文档</span><span class="arrow" aria-hidden="true">→</span></a>
      </div>
    </section>
  </div>

  <div class="container">
    <section class="section" aria-labelledby="caps-title">
      <header class="section-head">
        <div class="section-kicker">Core Capabilities</div>
        <h2 class="section-title" id="caps-title">一个平台，三个支柱</h2>
        <p class="section-lede">围绕"身份"、"多租户"与"可观测"三条主线，Aegis 把通常需要数月集成的能力封装为开箱即用的服务。</p>
      </header>
      <div class="caps">
        <article class="cap">
          <div class="cap-icon">` + iconShieldCheck + `</div>
          <h3 class="cap-title">身份与认证</h3>
          <p class="cap-body">密码、TOTP、OAuth2、OIDC、SAML、LDAP——主流认证方式原生支持。自定义密码策略、风险评估、双因子全面覆盖。</p>
          <ul class="cap-tags"><li class="cap-tag">OAuth2</li><li class="cap-tag">OIDC</li><li class="cap-tag">SAML</li><li class="cap-tag">TOTP</li></ul>
        </article>
        <article class="cap">
          <div class="cap-icon">` + iconBuilding + `</div>
          <h3 class="cap-title">多租户应用</h3>
          <p class="cap-body">每个 App 独立的用户库与策略空间。Casbin RBAC 细粒度权限、组织与部门建模、审批链与角色申请工作流一应俱全。</p>
          <ul class="cap-tags"><li class="cap-tag">App 隔离</li><li class="cap-tag">Casbin</li><li class="cap-tag">审批流</li></ul>
        </article>
        <article class="cap">
          <div class="cap-icon">` + iconActivity + `</div>
          <h3 class="cap-title">运行态可观测</h3>
          <p class="cap-body">OpenTelemetry 分布式追踪、实时在线用户统计、审计日志与风控事件全收敛。一切操作可追溯、可回放。</p>
          <ul class="cap-tags"><li class="cap-tag">OTel</li><li class="cap-tag">审计</li><li class="cap-tag">WAF</li></ul>
        </article>
      </div>
    </section>
  </div>

  <div class="container">
    <section class="section" aria-labelledby="mods-title">
      <header class="section-head">
        <div class="section-kicker">Extended Modules</div>
        <h2 class="section-title" id="mods-title">不止于认证</h2>
        <p class="section-lede">Aegis 内置了一组延展能力，让身份平台直接承担起业务层的通用基础设施职责。</p>
      </header>
      <div class="modules">
        <div class="mod"><span class="mod-icon">` + iconLayers + `</span><div class="mod-body"><h3 class="mod-title">工作流引擎</h3><p class="mod-desc">基于 Temporal 的节点式工作流，可视化编排审批、通知、异步任务。</p></div></div>
        <div class="mod"><span class="mod-icon">` + iconDatabase + `</span><div class="mod-body"><h3 class="mod-title">多云存储</h3><p class="mod-desc">Aliyun OSS、AWS S3、Tencent COS、Azure Blob、Qiniu、WebDAV 一键切换。</p></div></div>
        <div class="mod"><span class="mod-icon">` + iconGift + `</span><div class="mod-body"><h3 class="mod-title">积分与抽奖</h3><p class="mod-desc">用户签到、积分等级、链上哈希承诺的可验证抽奖系统。</p></div></div>
        <div class="mod"><span class="mod-icon">` + iconBell + `</span><div class="mod-body"><h3 class="mod-title">实时通知</h3><p class="mod-desc">基于 NATS JetStream + WebSocket 的双向通知与在线状态追踪。</p></div></div>
        <div class="mod"><span class="mod-icon">` + iconUsersRound + `</span><div class="mod-body"><h3 class="mod-title">组织与部门</h3><p class="mod-desc">多层组织树、岗位、邀请审批与成员权限边界模型。</p></div></div>
        <div class="mod"><span class="mod-icon">` + iconKeyRound + `</span><div class="mod-body"><h3 class="mod-title">传输加密</h3><p class="mod-desc">应用级端到端加密：XChaCha20-Poly1305、AES-256-GCM、混合 RSA/ECDH。</p></div></div>
      </div>
    </section>
  </div>

  <section class="cta">
    <div class="container">
      <div>
        <div class="cta-kicker">For developers</div>
        <h2 class="cta-title">API 优先，文档先行。</h2>
        <p class="cta-body">完整的 OpenAPI 3.0 规范与 Postman 集合随版本同步导出。用你熟悉的方式集成，而不是适应我们。</p>
      </div>
      <div class="cta-actions">
        <a class="cta-btn" href="/docs"><span class="icon">` + iconBookOpen + `</span><span>查看 API 文档</span></a>
        <a class="cta-link" href="/docs/openapi.json">下载 OpenAPI 规范 →</a>
      </div>
    </div>
  </section>

  <footer class="foot">
    <div class="container">
      <div class="foot-grid">
        <div>
          <div class="foot-brand-mark"><span class="dot" aria-hidden="true"></span>Aegis</div>
          <p class="foot-tagline">生产级多租户身份平台。一套统一的身份与访问基础设施，让业务专注于业务。</p>
        </div>
        <div class="foot-col"><h4>Product</h4><ul><li><a href="/console">控制台</a></li><li><a href="/login">登录</a></li><li><a href="/status">状态页</a></li></ul></div>
        <div class="foot-col"><h4>Developers</h4><ul><li><a href="/docs">API 文档</a></li><li><a href="/docs/openapi.json">OpenAPI</a></li><li><a href="/cons-env">环境查看</a></li><li><a href="/healthz">健康检查</a></li></ul></div>
        <div class="foot-col"><h4>Platform</h4><ul><li><a href="/announcements">系统公告</a></li><li><a href="/api/system/monitor">系统监控</a></li><li><a href="/api/public/branding">公开配置</a></li></ul></div>
      </div>
      <div class="foot-meta">
        <span class="status">All systems operational</span>
        <span class="sep">·</span>
        <span>Aegis {{.Data.Version}}</span>
        {{if .Data.CommitSHA}}<span class="sep">·</span><span>{{.Data.CommitSHA}}</span>{{end}}
        {{if .Data.BuildTime}}<span class="sep">·</span><span>built {{.Data.BuildTime}}</span>{{end}}
        <span class="sep">·</span>
        <span>{{.GoVersion}}</span>
        {{if .RequestID}}<span class="sep">·</span><span>Request ID <code>{{.RequestID}}</code></span>{{end}}
        <span class="grow"></span>
        <span>© {{.Year}} Aegis Platform</span>
      </div>
    </div>
  </footer>
</body>
</html>`
