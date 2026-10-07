package auditcatalog

// 安全与风控：防火墙与 IP 封禁、地理封禁与围栏、风控规则与评估、出站代理、流量爬坡、身份源连通性测试。
func init() {
	Register("security",
		// 防火墙与 IP 封禁
		View("GET /api/admin/system/firewall/stats", "查看防火墙统计", ""),
		View("GET /api/admin/system/firewall/logs", "查看防火墙日志", ""),
		View("GET /api/admin/system/firewall/logs/:logId", "查看防火墙日志详情", "防火墙日志"),
		Write("DELETE /api/admin/system/firewall/logs", "清理防火墙日志", "防火墙日志", SeverityHigh),
		View("GET /api/admin/system/firewall/bans", "查看 IP 封禁列表", ""),
		View("GET /api/admin/system/firewall/bans/modes", "查看 IP 封禁方式", ""),
		Write("POST /api/admin/system/firewall/bans", "封禁 IP", "IP", SeverityHigh),
		Write("DELETE /api/admin/system/firewall/bans/:banId", "解除 IP 封禁", "IP 封禁", SeverityHigh),

		// 地理封禁与地理围栏
		View("GET /api/admin/system/firewall/geo-bans", "查看地区封禁规则", ""),
		Write("POST /api/admin/system/firewall/geo-bans", "保存地区封禁规则", "地区封禁规则", SeverityHigh),
		Write("PATCH /api/admin/system/firewall/geo-bans/:id", "启停地区封禁规则", "地区封禁规则", SeverityHigh),
		Write("DELETE /api/admin/system/firewall/geo-bans/:id", "删除地区封禁规则", "地区封禁规则", SeverityHigh),
		View("GET /api/admin/system/firewall/geo-fences", "查看地理围栏", ""),
		View("POST /api/admin/system/firewall/geo-fences/preview", "预览地理围栏", "地理围栏"),
		Write("POST /api/admin/system/firewall/geo-fences", "新建地理围栏", "地理围栏", SeverityHigh),
		Write("PUT /api/admin/system/firewall/geo-fences/:id", "编辑地理围栏", "地理围栏", SeverityHigh),
		Write("PATCH /api/admin/system/firewall/geo-fences/:id", "启停地理围栏", "地理围栏", SeverityHigh),
		Write("DELETE /api/admin/system/firewall/geo-fences/:id", "删除地理围栏", "地理围栏", SeverityHigh),

		// 地理分析
		View("GET /api/admin/system/geo/heatmap", "查看访问热力图", ""),
		View("GET /api/admin/system/geo/clusters", "查看访问聚类", ""),
		View("GET /api/admin/system/geo/trail", "查看用户地理轨迹", "用户"),

		// 风控
		View("GET /api/admin/system/risk/dashboard", "查看风控总览", ""),
		View("GET /api/admin/system/risk/metadata", "查看风控配置项", ""),
		View("GET /api/admin/system/risk/rules", "查看风控规则", ""),
		View("GET /api/admin/system/risk/rules/:id", "查看风控规则详情", "风控规则"),
		Write("POST /api/admin/system/risk/rules", "新建风控规则", "风控规则", SeverityHigh),
		Write("PUT /api/admin/system/risk/rules/:id", "编辑风控规则", "风控规则", SeverityHigh),
		Write("DELETE /api/admin/system/risk/rules/:id", "删除风控规则", "风控规则", SeverityHigh),
		View("POST /api/admin/system/risk/rules/:id/simulate", "模拟风控规则", "风控规则"),
		View("POST /api/admin/system/risk/expression/validate", "校验风控表达式", ""),
		View("POST /api/admin/system/risk/simulate", "模拟风控评估", ""),
		View("POST /api/admin/system/risk/evaluate", "试算风险评估", ""),
		View("GET /api/admin/system/risk/actions", "查看风控处置动作", ""),
		Write("POST /api/admin/system/risk/actions", "新建风控处置动作", "风控处置动作", SeverityHigh),
		Write("PUT /api/admin/system/risk/actions/:id", "编辑风控处置动作", "风控处置动作", SeverityHigh),
		Write("DELETE /api/admin/system/risk/actions/:id", "删除风控处置动作", "风控处置动作", SeverityHigh),
		View("GET /api/admin/system/risk/assessments", "查看风险评估记录", ""),
		View("GET /api/admin/system/risk/assessments/:id", "查看风险评估详情", "风险评估"),
		Write("DELETE /api/admin/system/risk/assessments", "清理风险评估记录", "风险评估", SeverityHigh),
		View("POST /api/admin/system/risk/assessments/:id/replay", "重放风险评估", "风险评估"),
		Write("POST /api/admin/system/risk/assessments/:id/review", "人工复核风险评估", "风险评估", SeverityMedium),
		View("GET /api/admin/system/risk/reviews/pending", "查看待复核评估", ""),
		View("GET /api/admin/system/risk/devices", "查看风控设备", ""),
		View("GET /api/admin/system/risk/devices/suspicious", "查看可疑设备", ""),
		View("GET /api/admin/system/risk/devices/:deviceId", "查看设备指纹", "设备"),
		Write("PUT /api/admin/system/risk/devices/:id/tag", "标记设备风险", "设备", SeverityHigh),
		View("GET /api/admin/system/risk/ips", "查看高风险 IP", ""),
		View("GET /api/admin/system/risk/ips/:ip", "查看 IP 风险", "IP"),
		Write("POST /api/admin/system/risk/ips/:ip/refresh", "刷新 IP 信誉", "IP", SeverityLow),
		Write("PUT /api/admin/system/risk/ips/:id/tag", "标记 IP 风险", "IP", SeverityHigh),

		// 出站代理
		View("GET /api/admin/system/egress", "查看出站代理设置", ""),
		Write("PUT /api/admin/system/egress", "修改出站代理设置", "", SeverityHigh),
		Write("POST /api/admin/system/egress/reset", "重置出站代理设置", "", SeverityHigh),
		View("POST /api/admin/system/egress/test", "测试出站代理", ""),
		View("POST /api/admin/system/egress/probe", "探测出站连通性", ""),
		View("POST /api/admin/system/egress/explain", "解析出站路由", ""),

		// 流量爬坡
		View("GET /api/admin/system/traffic-ramp", "查看流量爬坡设置", ""),
		View("GET /api/admin/system/traffic-ramp/stats", "查看流量爬坡统计", ""),
		Write("PUT /api/admin/system/traffic-ramp", "修改流量爬坡设置", "", SeverityHigh),
		Write("POST /api/admin/system/traffic-ramp/reset-stats", "重置流量爬坡统计", "", SeverityMedium),

		// 身份源与验证码
		View("POST /api/admin/system/ldap/test", "测试 LDAP 连接", ""),
		View("POST /api/admin/system/oidc/test", "测试 OIDC 配置", ""),
		View("POST /api/admin/system/saml/test", "测试 SAML 配置", ""),
		View("POST /api/admin/system/captcha/preview", "预览动态验证码", ""),
		View("GET /api/admin/oauth-providers/templates", "查看第三方登录模板", ""),
	)
}
