package auditcatalog

// 登录与认证：管理员登录、单点登录、二次验证与登录前用到的公开配置、验证码。
func init() {
	Register("auth",
		Auth("POST /api/admin/auth/login", "登录控制台", SeverityLow),
		Auth("POST /api/admin/auth/logout", "退出登录", SeverityLow),
		Auth("POST /api/admin/auth/register", "注册管理员账号", SeverityMedium),
		Auth("POST /api/admin/auth/verify-mfa", "完成二次验证", SeverityLow),
		View("GET /api/admin/auth/me", "查看当前登录身份", ""),
		View("GET /api/admin/auth/self-service", "查看自助注册配置", ""),
		View("GET /api/admin/auth/ldap/config", "查看 LDAP 登录配置", ""),
		View("GET /api/admin/auth/oidc/config", "查看 OIDC 登录配置", ""),
		Auth("GET /api/admin/auth/oidc/authorize", "发起 OIDC 单点登录", SeverityLow),
		Auth("GET /api/admin/auth/oidc/callback", "OIDC 单点登录回调", SeverityLow),
		Auth("POST /api/admin/auth/oidc/exchange", "OIDC 单点登录换取会话", SeverityLow),
		View("GET /api/admin/auth/saml/config", "查看 SAML 登录配置", ""),
		View("GET /api/admin/auth/saml/metadata", "查看 SAML 元数据", ""),
		Auth("GET /api/admin/auth/saml/authorize", "发起 SAML 单点登录", SeverityLow),
		Auth("GET /api/admin/auth/saml/callback", "SAML 单点登录回调", SeverityLow),
		Auth("POST /api/admin/auth/saml/callback", "SAML 单点登录回调", SeverityLow),
		Auth("POST /api/admin/auth/saml/exchange", "SAML 单点登录换取会话", SeverityLow),
		View("GET /api/admin/captcha/config", "查看登录验证码配置", ""),
		View("POST /api/admin/captcha/generate", "获取登录验证码", ""),
		View("POST /api/admin/captcha/verify", "校验登录验证码", ""),
		View("POST /api/admin/captcha/verify-click", "校验点选验证码", ""),
	)
}
