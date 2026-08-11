package service

import (
	oauthdomain "aegis/internal/domain/oauth"
)

// oauthTemplates 内置第三方登录渠道模板目录。
//
// 管理端"添加渠道"时点选模板即可自动填充端点、scope 与品牌信息，
// 只需再补 ClientID / ClientSecret 两项即可上线。
// Key 同时作为默认 slug（写入 oauth_bindings.provider），不可随意变更。
// Icon 取 Simple Icons 的 slug（前端 BrandIcon 据此渲染官方品牌图标）；
// Simple Icons 未收录的渠道（Microsoft/自定义）由前端用内置 mark 兜底。
var oauthTemplates = []oauthdomain.Template{
	{
		Key: "wechat", Kind: oauthdomain.KindWechat, Name: "微信开放平台", Icon: "wechat",
		Color: "#07C160", Category: "国内",
		Description: "微信「网站应用」扫码登录，适用于 PC 端网页。需在开放平台完成网站应用审核。",
		DocsURL:     "https://developers.weixin.qq.com/doc/oplatform/Website_App/WeChat_Login/Wechat_Login.html",
		ConsoleURL:  "https://open.weixin.qq.com/",
		AuthURL:     "https://open.weixin.qq.com/connect/qrconnect",
		TokenURL:    "https://api.weixin.qq.com/sns/oauth2/access_token",
		UserInfoURL: "https://api.weixin.qq.com/sns/userinfo",
		Scopes:      []string{"snsapi_login"},
		Fields: []oauthdomain.TemplateField{
			{Key: "clientId", Label: "AppID", Placeholder: "wx0000000000000000"},
			{Key: "clientSecret", Label: "AppSecret", Hint: "开放平台「应用详情」中的 AppSecret"},
		},
		Notes: []string{
			"回调域名需在开放平台「授权回调域」中登记，且只填域名（不含协议与路径）。",
			"UnionID 需要在开放平台下绑定同主体应用后才会返回。",
		},
	},
	{
		Key: "wechat_mp", Kind: oauthdomain.KindWechat, Name: "微信公众号", Icon: "wechat",
		Color: "#07C160", Category: "国内",
		Description: "微信公众平台网页授权（snsapi_userinfo），适用于微信内 H5 页面。",
		DocsURL:     "https://developers.weixin.qq.com/doc/offiaccount/OA_Web_Apps/Wechat_webpage_authorization.html",
		ConsoleURL:  "https://mp.weixin.qq.com/",
		AuthURL:     "https://open.weixin.qq.com/connect/oauth2/authorize",
		TokenURL:    "https://api.weixin.qq.com/sns/oauth2/access_token",
		UserInfoURL: "https://api.weixin.qq.com/sns/userinfo",
		Scopes:      []string{"snsapi_userinfo"},
		Notes:       []string{"仅在微信客户端内打开有效；网页授权域名需在公众号后台配置。"},
	},
	{
		Key: "qq", Kind: oauthdomain.KindQQ, Name: "QQ 互联", Icon: "qq",
		Color: "#1EBAFC", Category: "国内",
		Description: "QQ 互联网站应用登录，返回 openid；同主体应用可通过 unionid 打通。",
		DocsURL:     "https://wiki.connect.qq.com/使用authorization_code获取access_token",
		ConsoleURL:  "https://connect.qq.com/",
		AuthURL:     "https://graph.qq.com/oauth2.0/authorize",
		TokenURL:    "https://graph.qq.com/oauth2.0/token",
		UserInfoURL: "https://graph.qq.com/user/get_user_info",
		Scopes:      []string{"get_user_info"},
		Notes:       []string{"回调地址必须与 QQ 互联后台登记的完全一致（含协议、端口与查询串）。"},
	},
	{
		Key: "weibo", Kind: oauthdomain.KindWeibo, Name: "新浪微博", Icon: "sinaweibo",
		Color: "#E6162D", Category: "国内",
		Description: "微博开放平台网站接入，token 响应中直接返回 uid。",
		DocsURL:     "https://open.weibo.com/wiki/Oauth2/access_token",
		ConsoleURL:  "https://open.weibo.com/",
		AuthURL:     "https://api.weibo.com/oauth2/authorize",
		TokenURL:    "https://api.weibo.com/oauth2/access_token",
		UserInfoURL: "https://api.weibo.com/2/users/show.json",
		Scopes:      []string{"email"},
	},
	{
		Key: "gitee", Kind: oauthdomain.KindGeneric, Name: "Gitee 码云", Icon: "gitee",
		Color: "#C71D23", Category: "国内",
		Description: "Gitee 第三方应用授权，适合面向国内开发者的产品。",
		DocsURL:     "https://gitee.com/api/v5/oauth_doc",
		ConsoleURL:  "https://gitee.com/oauth/applications",
		AuthURL:     "https://gitee.com/oauth/authorize",
		TokenURL:    "https://gitee.com/oauth/token",
		UserInfoURL: "https://gitee.com/api/v5/user",
		Scopes:      []string{"user_info"},
	},
	{
		Key: "linuxdo", Kind: oauthdomain.KindGeneric, Name: "LINUX DO", Icon: "linux",
		Color: "#F0B400", Category: "社区",
		Description:    "LINUX DO Connect，社区账号一键登录；token 端点要求 HTTP Basic 认证。",
		DocsURL:        "https://connect.linux.do/",
		ConsoleURL:     "https://connect.linux.do/",
		AuthURL:        "https://connect.linux.do/oauth2/authorize",
		TokenURL:       "https://connect.linux.do/oauth2/token",
		UserInfoURL:    "https://connect.linux.do/api/user",
		Scopes:         []string{},
		TokenAuthStyle: oauthdomain.TokenAuthBasic,
	},
	{
		Key: "github", Kind: oauthdomain.KindGitHub, Name: "GitHub", Icon: "github",
		Color: "#181717", Category: "海外",
		Description: "GitHub OAuth App；主邮箱未公开时自动回退到 /user/emails 拉取。",
		DocsURL:     "https://docs.github.com/apps/oauth-apps",
		ConsoleURL:  "https://github.com/settings/developers",
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user",
		Scopes:      []string{"read:user", "user:email"},
	},
	{
		Key: "gitlab", Kind: oauthdomain.KindGeneric, Name: "GitLab", Icon: "gitlab",
		Color: "#FC6D26", Category: "海外",
		Description: "GitLab.com 或自建 GitLab；自建实例请把域名替换为你的地址。",
		DocsURL:     "https://docs.gitlab.com/ee/api/oauth2.html",
		ConsoleURL:  "https://gitlab.com/-/profile/applications",
		AuthURL:     "https://gitlab.com/oauth/authorize",
		TokenURL:    "https://gitlab.com/oauth/token",
		UserInfoURL: "https://gitlab.com/api/v4/user",
		Scopes:      []string{"read_user"},
	},
	{
		Key: "google", Kind: oauthdomain.KindGeneric, Name: "Google", Icon: "google",
		Color: "#4285F4", Category: "海外",
		Description: "Google Identity（OpenID Connect），返回标准 sub / email / picture。",
		DocsURL:     "https://developers.google.com/identity/protocols/oauth2/web-server",
		ConsoleURL:  "https://console.cloud.google.com/apis/credentials",
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
		Notes:       []string{"需要 refresh_token 时可在附加授权参数中加入 access_type=offline。"},
	},
	{
		Key: "microsoft", Kind: oauthdomain.KindMicrosoft, Name: "Microsoft", Icon: "microsoft",
		Color: "#00A4EF", Category: "海外",
		Description: "Microsoft Entra ID（Azure AD）；单租户请把 common 换成租户 ID。",
		DocsURL:     "https://learn.microsoft.com/entra/identity-platform/v2-oauth2-auth-code-flow",
		ConsoleURL:  "https://entra.microsoft.com/",
		AuthURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL: "https://graph.microsoft.com/oidc/userinfo",
		Scopes:      []string{"openid", "email", "profile", "User.Read"},
	},
	{
		Key: "discord", Kind: oauthdomain.KindGeneric, Name: "Discord", Icon: "discord",
		Color: "#5865F2", Category: "海外",
		Description: "Discord OAuth2，返回 id / username / avatar 哈希。",
		DocsURL:     "https://discord.com/developers/docs/topics/oauth2",
		ConsoleURL:  "https://discord.com/developers/applications",
		AuthURL:     "https://discord.com/oauth2/authorize",
		TokenURL:    "https://discord.com/api/oauth2/token",
		UserInfoURL: "https://discord.com/api/users/@me",
		Scopes:      []string{"identify", "email"},
	},
	{
		Key: "oidc", Kind: oauthdomain.KindGeneric, Name: "标准 OIDC", Icon: "openid",
		Color: "#F78C40", Category: "自定义",
		Description:       "任意符合 OpenID Connect 的身份源：填写发现文档中的三个端点即可。",
		AuthURL:           "",
		TokenURL:          "",
		UserInfoURL:       "",
		Scopes:            []string{"openid", "email", "profile"},
		RequiresEndpoints: true,
		Notes: []string{
			"端点可从 {issuer}/.well-known/openid-configuration 中的 authorization_endpoint、token_endpoint、userinfo_endpoint 抄写。",
		},
	},
	{
		Key: "custom", Kind: oauthdomain.KindGeneric, Name: "自定义 OAuth2", Icon: "custom",
		Color: "#64748B", Category: "自定义",
		Description:       "非标准返回结构的自建服务：可用字段映射把任意 JSON 路径对到用户字段。",
		RequiresEndpoints: true,
		Scopes:            []string{},
		Notes: []string{
			"字段映射支持点号路径，例如把 data.user.id 映射到唯一标识。",
			"若用户信息接口要求 access_token 作为查询参数，请把凭据方式改为「查询参数」。",
		},
	},
}

// oauthTemplateIndex 模板 key → 模板，供保存时按 kind 兜底端点。
var oauthTemplateIndex = func() map[string]oauthdomain.Template {
	index := make(map[string]oauthdomain.Template, len(oauthTemplates))
	for _, item := range oauthTemplates {
		index[item.Key] = item
	}
	return index
}()
