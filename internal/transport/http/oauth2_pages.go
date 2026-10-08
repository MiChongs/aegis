package httptransport

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OAuth2 授权服务器的托管页面：登录 / 两步验证 / 授权同意 / 登出 / 设备码 / 错误。
//
// 页面由后端直接渲染，不依赖控制台：Hydra 把浏览器重定向过来时只带一个 challenge，
// 这几页必须在任何部署形态下都可用（含只起了后端、没起控制台的那种）。
// 不用任何脚本，表单原生提交，CSP 因此可以收到 default-src 'none'。

// oauth2PageView 所有页面共用的视图模型；每页只读自己用得到的字段。
type oauth2PageView struct {
	Title     string
	AppName   string
	Client    string
	ClientURI string
	LogoURI   string
	PolicyURI string
	TosURI    string
	Error     string
	Notice    string

	Challenge string
	CSRF      string

	// 登录
	Account        string
	Remember       bool
	CaptchaID      string
	CaptchaImage   template.URL
	CaptchaHint    string
	MFAChallengeID string
	MFAMethods     []string
	AllowRecovery  bool

	// 同意
	Scopes      []oauth2ScopeItem
	UserName    string
	UserAccount string
	UserAvatar  string

	// 设备码
	UserCode string
}

type oauth2ScopeItem struct {
	Scope       string
	Title       string
	Description string
	Required    bool
}

func renderOAuth2Page(c *gin.Context, status int, page string, view oauth2PageView) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	// 同意页被嵌进别人的 iframe 就是点击劫持：诱导用户点「允许」
	c.Header("X-Frame-Options", "DENY")
	// 不声明 form-action：表单提交后会经 Hydra 一路 302 到接入方的回调地址，
	// 浏览器对 form-action 的检查覆盖整条重定向链，写 'self' 会把正常授权拦在半路。
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src https: http: data:; base-uri 'none'; frame-ancestors 'none'")
	var buf bytes.Buffer
	if err := oauth2Templates.ExecuteTemplate(&buf, page, view); err != nil {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusInternalServerError, "页面渲染失败")
		return
	}
	c.Status(status)
	_, _ = c.Writer.Write(buf.Bytes())
}

var oauth2Templates = template.Must(template.New("oauth2").Parse(oauth2PageSource))

const oauth2PageSource = `
{{define "head"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex, nofollow">
  <meta name="color-scheme" content="light dark">
  <title>{{.Title}}{{if .AppName}} - {{.AppName}}{{end}}</title>
  <style>
    :root {
      --bg: #f5f4ed; --surface: #faf9f5; --field: #ffffff;
      --text: #141413; --body: #3d3d3a; --muted: #5e5d59; --faint: #87867f;
      --rule: #e8e6dc; --ring: #d1cfc5;
      --brand: #c96442; --brand-hover: #b55230; --on-brand: #faf9f5;
      --danger-bg: #f6e3dc; --danger: #8f2f17; --notice-bg: #e9efe3; --notice: #3f5f2c;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #141413; --surface: #1c1c1a; --field: #242422;
        --text: #f5f4ed; --body: #e6e4d8; --muted: #b0aea5; --faint: #87867f;
        --rule: #30302e; --ring: #3d3d3a;
        --brand: #d97757; --brand-hover: #e78a65; --on-brand: #141413;
        --danger-bg: #3a221a; --danger: #f0a58c; --notice-bg: #223020; --notice: #b5d39c;
      }
    }
    *, *::before, *::after { box-sizing: border-box; }
    html, body { margin: 0; min-height: 100%; }
    body { background: var(--bg); color: var(--text); font-family: "Inter", "PingFang SC", "Microsoft YaHei UI", system-ui, sans-serif; font-size: 15px; line-height: 1.6; -webkit-font-smoothing: antialiased; }
    .page { min-height: 100vh; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 32px 16px; }
    .card { width: 100%; max-width: 420px; background: var(--surface); border-radius: 18px; box-shadow: 0 0 0 1px var(--rule); padding: 32px 28px 28px; }
    .ident { display: flex; align-items: center; gap: 12px; margin-bottom: 22px; }
    .logo { width: 44px; height: 44px; border-radius: 12px; object-fit: cover; background: var(--bg); box-shadow: 0 0 0 1px var(--rule); flex-shrink: 0; }
    .mark { width: 44px; height: 44px; border-radius: 12px; display: inline-flex; align-items: center; justify-content: center; background: var(--brand); color: var(--on-brand); font-family: Georgia, serif; font-size: 20px; flex-shrink: 0; }
    .ident-text { min-width: 0; }
    .ident-app { font-size: 13px; color: var(--faint); }
    .ident-client { font-weight: 600; color: var(--text); overflow-wrap: anywhere; }
    h1 { margin: 0 0 6px; font-family: Georgia, "Songti SC", serif; font-size: 26px; font-weight: 500; letter-spacing: -0.01em; }
    .lede { margin: 0 0 22px; color: var(--muted); }
    .alert { margin: 0 0 18px; padding: 10px 12px; border-radius: 10px; font-size: 14px; background: var(--danger-bg); color: var(--danger); }
    .alert.ok { background: var(--notice-bg); color: var(--notice); }
    label.field { display: block; margin-bottom: 14px; }
    .label { display: block; margin-bottom: 6px; font-size: 13px; font-weight: 500; color: var(--body); }
    input[type=text], input[type=password] { width: 100%; height: 44px; padding: 0 12px; font: inherit; color: var(--text); background: var(--field); border: 0; border-radius: 10px; box-shadow: inset 0 0 0 1px var(--ring); outline: none; }
    input[type=text]:focus, input[type=password]:focus { box-shadow: inset 0 0 0 2px var(--brand); }
    input.code { letter-spacing: 0.3em; text-align: center; font-size: 20px; font-family: "SF Mono", Consolas, monospace; }
    .captcha { display: flex; gap: 10px; align-items: center; }
    .captcha img { height: 44px; border-radius: 10px; box-shadow: 0 0 0 1px var(--rule); background: #fff; }
    .check { display: flex; align-items: center; gap: 8px; margin: 4px 0 20px; color: var(--body); font-size: 14px; }
    .check input { width: 16px; height: 16px; accent-color: var(--brand); }
    .actions { display: flex; gap: 10px; }
    .actions.stack { flex-direction: column; }
    button { flex: 1; height: 44px; font: inherit; font-weight: 500; border: 0; border-radius: 10px; cursor: pointer; }
    .primary { background: var(--brand); color: var(--on-brand); }
    .primary:hover { background: var(--brand-hover); }
    .secondary { background: transparent; color: var(--body); box-shadow: inset 0 0 0 1px var(--ring); }
    .secondary:hover { background: var(--bg); }
    .who { display: flex; align-items: center; gap: 12px; padding: 12px; margin-bottom: 18px; border-radius: 12px; background: var(--bg); }
    .avatar { width: 36px; height: 36px; border-radius: 50%; object-fit: cover; background: var(--rule); }
    .who-name { font-weight: 500; }
    .who-account { font-size: 13px; color: var(--faint); }
    ul.scopes { list-style: none; margin: 0 0 18px; padding: 0; border-top: 1px solid var(--rule); }
    ul.scopes li { border-bottom: 1px solid var(--rule); }
    .scope { display: flex; gap: 12px; align-items: flex-start; padding: 12px 2px; }
    .scope input { margin-top: 4px; width: 16px; height: 16px; accent-color: var(--brand); flex-shrink: 0; }
    .scope-title { font-weight: 500; }
    .scope-desc { font-size: 13px; color: var(--muted); }
    .legal { margin: 0 0 18px; font-size: 13px; color: var(--faint); }
    .legal a, .foot a { color: var(--muted); }
    .foot { margin-top: 20px; font-size: 12.5px; color: var(--faint); text-align: center; }
    .hint { font-size: 13px; color: var(--faint); margin: -6px 0 14px; }
  </style>
</head>
<body><main class="page"><div class="card">
{{end}}

{{define "ident"}}
  <div class="ident">
    {{if .LogoURI}}<img class="logo" src="{{.LogoURI}}" alt="">{{else}}<span class="mark" aria-hidden="true">A</span>{{end}}
    <div class="ident-text">
      {{if .AppName}}<div class="ident-app">{{.AppName}}</div>{{end}}
      {{if .Client}}<div class="ident-client">{{if .ClientURI}}<a href="{{.ClientURI}}" rel="noopener noreferrer" target="_blank" style="color:inherit;text-decoration:none">{{.Client}}</a>{{else}}{{.Client}}{{end}}</div>{{end}}
    </div>
  </div>
  {{if .Error}}<p class="alert" role="alert">{{.Error}}</p>{{end}}
  {{if .Notice}}<p class="alert ok" role="status">{{.Notice}}</p>{{end}}
{{end}}

{{define "foot"}}
</div>
<p class="foot">身份认证由 Aegis 提供</p>
</main></body></html>
{{end}}

{{define "login"}}{{template "head" .}}{{template "ident" .}}
  <h1>登录</h1>
  <p class="lede">使用{{if .AppName}}{{.AppName}}{{end}}账号登录{{if .Client}}以继续使用{{.Client}}{{end}}。</p>
  <form method="post" action="/oauth2/login" autocomplete="on">
    <input type="hidden" name="challenge" value="{{.Challenge}}">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <label class="field"><span class="label">账号</span>
      <input type="text" name="account" value="{{.Account}}" autocomplete="username" autocapitalize="none" spellcheck="false" required autofocus></label>
    <label class="field"><span class="label">密码</span>
      <input type="password" name="password" autocomplete="current-password" required></label>
    {{if .CaptchaID}}
    <input type="hidden" name="captchaId" value="{{.CaptchaID}}">
    <label class="field"><span class="label">验证码</span>
      <span class="captcha"><input type="text" name="captchaAnswer" autocomplete="off" required>{{if .CaptchaImage}}<img src="{{.CaptchaImage}}" alt="验证码">{{end}}</span></label>
    {{if .CaptchaHint}}<p class="hint">{{.CaptchaHint}}</p>{{end}}
    {{end}}
    <label class="check"><input type="checkbox" name="remember" value="1"{{if .Remember}} checked{{end}}>在此设备上保持登录</label>
    <div class="actions">
      <button class="secondary" type="submit" name="action" value="cancel" formnovalidate>取消</button>
      <button class="primary" type="submit" name="action" value="login">登录</button>
    </div>
  </form>
{{template "foot" .}}{{end}}

{{define "mfa"}}{{template "head" .}}{{template "ident" .}}
  <h1>两步验证</h1>
  <p class="lede">请输入身份验证器中的 6 位验证码。</p>
  <form method="post" action="/oauth2/login/mfa" autocomplete="off">
    <input type="hidden" name="challenge" value="{{.Challenge}}">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <input type="hidden" name="mfa" value="{{.MFAChallengeID}}">
    {{if .Remember}}<input type="hidden" name="remember" value="1">{{end}}
    <label class="field"><span class="label">验证码</span>
      <input class="code" type="text" name="code" inputmode="numeric" autocomplete="one-time-code" maxlength="8" autofocus></label>
    {{if .AllowRecovery}}
    <label class="field"><span class="label">或输入恢复码</span>
      <input type="text" name="recoveryCode" autocomplete="off" spellcheck="false"></label>
    {{end}}
    <div class="actions">
      <button class="secondary" type="submit" name="action" value="cancel">取消</button>
      <button class="primary" type="submit" name="action" value="verify">验证</button>
    </div>
  </form>
{{template "foot" .}}{{end}}

{{define "consent"}}{{template "head" .}}{{template "ident" .}}
  <h1>授权请求</h1>
  <p class="lede">{{.Client}}请求访问你的{{if .AppName}}{{.AppName}}{{end}}账号。</p>
  <div class="who">
    {{if .UserAvatar}}<img class="avatar" src="{{.UserAvatar}}" alt="">{{end}}
    <div><div class="who-name">{{if .UserName}}{{.UserName}}{{else}}{{.UserAccount}}{{end}}</div>
    {{if .UserName}}<div class="who-account">{{.UserAccount}}</div>{{end}}</div>
  </div>
  <form method="post" action="/oauth2/consent">
    <input type="hidden" name="challenge" value="{{.Challenge}}">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <ul class="scopes">
      {{range .Scopes}}<li><label class="scope">
        <input type="checkbox" name="scope" value="{{.Scope}}" checked{{if .Required}} disabled{{end}}>
        <span><span class="scope-title">{{.Title}}</span><br><span class="scope-desc">{{.Description}}</span></span>
      </label></li>{{end}}
    </ul>
    <label class="check"><input type="checkbox" name="remember" value="1" checked>记住此授权，下次不再询问</label>
    {{if or .PolicyURI .TosURI}}<p class="legal">继续即表示你同意{{.Client}}的{{if .TosURI}}<a href="{{.TosURI}}" rel="noopener noreferrer" target="_blank">服务条款</a>{{end}}{{if and .PolicyURI .TosURI}}与{{end}}{{if .PolicyURI}}<a href="{{.PolicyURI}}" rel="noopener noreferrer" target="_blank">隐私政策</a>{{end}}。</p>{{end}}
    <div class="actions">
      <button class="secondary" type="submit" name="action" value="deny">拒绝</button>
      <button class="primary" type="submit" name="action" value="allow">允许</button>
    </div>
  </form>
{{template "foot" .}}{{end}}

{{define "logout"}}{{template "head" .}}{{template "ident" .}}
  <h1>退出登录</h1>
  <p class="lede">确认退出{{if .Client}}{{.Client}}及{{end}}在此浏览器上通过 Aegis 登录的全部应用？</p>
  <form method="post" action="/oauth2/logout">
    <input type="hidden" name="challenge" value="{{.Challenge}}">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="actions">
      <button class="secondary" type="submit" name="action" value="no">保持登录</button>
      <button class="primary" type="submit" name="action" value="yes">退出</button>
    </div>
  </form>
{{template "foot" .}}{{end}}

{{define "device"}}{{template "head" .}}{{template "ident" .}}
  <h1>设备登录</h1>
  <p class="lede">请输入设备屏幕上显示的代码。</p>
  <form method="post" action="/oauth2/device" autocomplete="off">
    <input type="hidden" name="challenge" value="{{.Challenge}}">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <label class="field"><span class="label">设备代码</span>
      <input class="code" type="text" name="userCode" value="{{.UserCode}}" autocapitalize="characters" spellcheck="false" required autofocus></label>
    <div class="actions"><button class="primary" type="submit">继续</button></div>
  </form>
{{template "foot" .}}{{end}}

{{define "message"}}{{template "head" .}}{{template "ident" .}}
  <h1>{{.Title}}</h1>
{{template "foot" .}}{{end}}
`
