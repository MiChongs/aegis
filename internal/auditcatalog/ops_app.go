package auditcatalog

// 应用管理：应用本身、接入协议与密钥、加密、登录策略、验证码、第三方登录与治理申诉。
func init() {
	Register("app",
		View("GET /api/admin/apps", "查看应用列表", ""),
		Write("POST /api/admin/apps", "创建应用", "应用", SeverityMedium),
		View("GET /api/admin/apps/:appkey", "查看应用详情", "应用"),
		Write("PUT /api/admin/apps/:appkey", "编辑应用", "应用", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey", "删除应用", "应用", SeverityCritical),

		View("GET /api/admin/apps/:appkey/auth-protocol", "查看接入配置", ""),
		Write("PUT /api/admin/apps/:appkey/auth-protocol", "修改接入配置", "", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/auth-protocol/secret/rotate", "轮换应用密钥", "应用密钥", SeverityHigh),
		View("POST /api/admin/apps/:appkey/auth-protocol/selftest", "运行接入自检", ""),
		View("GET /api/admin/apps/:appkey/auth-protocol/transport/keys", "查看传输密钥", ""),
		Write("POST /api/admin/apps/:appkey/auth-protocol/transport/rotate", "轮换传输密钥", "传输密钥", SeverityHigh),
		Write("DELETE /api/admin/apps/:appkey/auth-protocol/transport/keys/:keyId", "撤销传输密钥", "传输密钥", SeverityHigh),

		View("GET /api/admin/apps/:appkey/encryption", "查看传输加密配置", ""),
		Write("PUT /api/admin/apps/:appkey/encryption", "修改传输加密配置", "", SeverityHigh),
		View("GET /api/admin/apps/:appkey/policy", "查看应用策略", ""),
		Write("PUT /api/admin/apps/:appkey/policy", "修改应用策略", "", SeverityMedium),
		View("GET /api/admin/apps/:appkey/password-policy", "查看密码策略", ""),
		Write("PUT /api/admin/apps/:appkey/password-policy", "修改密码策略", "", SeverityMedium),
		Write("POST /api/admin/apps/:appkey/password-policy/reset", "重置密码策略", "", SeverityMedium),
		View("POST /api/admin/apps/:appkey/password-policy/test", "测试密码策略", ""),
		View("GET /api/admin/apps/password-policy/templates", "查看密码策略模板", ""),

		View("GET /api/admin/apps/:appkey/captcha-config", "查看验证码配置", ""),
		Write("PUT /api/admin/apps/:appkey/captcha-config", "修改验证码配置", "", SeverityMedium),
		View("POST /api/admin/apps/:appkey/captcha-config/preview", "预览动态验证码", ""),
		Write("POST /api/admin/apps/:appkey/captcha-config/test-sms", "发送测试短信", "", SeverityLow),

		View("GET /api/admin/apps/:appkey/oauth-providers", "查看第三方登录", ""),
		View("GET /api/admin/apps/:appkey/oauth-providers/:provider", "查看第三方登录详情", "第三方登录"),
		Write("POST /api/admin/apps/:appkey/oauth-providers", "新增第三方登录", "第三方登录", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/oauth-providers/:provider", "编辑第三方登录", "第三方登录", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/oauth-providers/:provider/enabled", "启停第三方登录", "第三方登录", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/oauth-providers/:provider", "删除第三方登录", "第三方登录", SeverityHigh),
		View("POST /api/admin/apps/:appkey/oauth-providers/:provider/test", "测试第三方登录", "第三方登录"),
		Write("POST /api/admin/apps/:appkey/oauth-providers/reorder", "调整第三方登录顺序", "", SeverityLow),

		View("GET /api/admin/apps/:appkey/oauth2/overview", "查看 OAuth2 授权服务", ""),
		View("GET /api/admin/apps/:appkey/oauth2/clients", "查看 OAuth2 客户端", ""),
		View("GET /api/admin/apps/:appkey/oauth2/clients/:clientId", "查看 OAuth2 客户端详情", "OAuth2 客户端"),
		Write("POST /api/admin/apps/:appkey/oauth2/clients", "新建 OAuth2 客户端", "OAuth2 客户端", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/oauth2/clients/:clientId", "编辑 OAuth2 客户端", "OAuth2 客户端", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/oauth2/clients/:clientId", "删除 OAuth2 客户端", "OAuth2 客户端", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/oauth2/clients/:clientId/rotate-secret", "重新生成 OAuth2 客户端密钥", "OAuth2 客户端", SeverityHigh),

		View("GET /api/admin/apps/:appkey/login-baseline/:userid", "查看登录基线", "用户"),
		Write("DELETE /api/admin/apps/:appkey/login-baseline/:userid", "重置登录基线", "用户", SeverityMedium),

		View("GET /api/admin/apps/:appkey/governance", "查看治理状态", ""),
		View("GET /api/admin/apps/:appkey/governance/history", "查看治理记录", ""),
		Write("POST /api/admin/apps/:appkey/governance/appeals", "提交治理申诉", "治理申诉", SeverityLow),
		Write("POST /api/admin/apps/:appkey/governance/appeals/:appealId/withdraw", "撤回治理申诉", "治理申诉", SeverityLow),
	)
	Register("commerce",
		View("GET /api/admin/apps/signin-reward/templates", "查看签到奖励模板", ""),
	)
}
