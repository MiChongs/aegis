package auditcatalog

// 安全与风控：防火墙与 IP 封禁、地理封禁与围栏、风控规则与评估、出站代理、流量管理、身份源连通性测试。
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

		// 风控平台：整组转发到 risk-admin（操作明细另记在风控平台自己的审计里）
		View("GET /api/admin/system/risk/*path", "查看风控平台", ""),
		Write("POST /api/admin/system/risk/*path", "操作风控平台（发布、处置、复核、新建）", "风控平台", SeverityHigh),
		Write("PUT /api/admin/system/risk/*path", "修改风控平台配置", "风控平台", SeverityHigh),
		Write("PATCH /api/admin/system/risk/*path", "修改风控平台配置", "风控平台", SeverityHigh),
		Write("DELETE /api/admin/system/risk/*path", "删除风控平台配置", "风控平台", SeverityHigh),

		// 出站代理
		View("GET /api/admin/system/egress", "查看出站代理设置", ""),
		Write("PUT /api/admin/system/egress", "修改出站代理设置", "", SeverityHigh),
		Write("POST /api/admin/system/egress/reset", "重置出站代理设置", "", SeverityHigh),
		View("POST /api/admin/system/egress/test", "测试出站代理", ""),
		View("POST /api/admin/system/egress/probe", "探测出站连通性", ""),
		View("POST /api/admin/system/egress/explain", "解析出站路由", ""),
		View("GET /api/admin/system/egress/graph", "查看出海网关画布", ""),
		Write("PUT /api/admin/system/egress/graph", "保存出海网关画布草稿", "", SeverityLow),
		View("POST /api/admin/system/egress/graph/validate", "校验出海网关画布", ""),
		Write("POST /api/admin/system/egress/graph/apply", "应用出海网关画布", "", SeverityHigh),
		Write("POST /api/admin/system/egress/graph/discard", "丢弃出海网关画布草稿", "", SeverityLow),
		View("POST /api/admin/system/egress/graph/import", "导入 mihomo 配置到画布", ""),
		Write("POST /api/admin/system/egress/graph/export", "导出出海网关核心配置", "", SeverityHigh),
		View("GET /api/admin/system/egress/graph/history", "查看出海网关版本记录", ""),
		View("GET /api/admin/system/egress/graph/history/:version", "查看出海网关历史版本", "版本"),
		Write("POST /api/admin/system/egress/graph/rollback", "回滚出海网关版本", "", SeverityHigh),
		View("GET /api/admin/system/egress/mihomo/status", "查看 mihomo 核心状态", ""),
		Write("POST /api/admin/system/egress/mihomo/install", "安装 mihomo 核心", "", SeverityHigh),
		Write("POST /api/admin/system/egress/mihomo/restart", "重启 mihomo 核心", "", SeverityHigh),
		Write("POST /api/admin/system/egress/mihomo/stop", "停止 mihomo 核心", "", SeverityHigh),
		View("GET /api/admin/system/egress/mihomo/proxies", "查看 mihomo 节点与策略组", ""),
		View("POST /api/admin/system/egress/mihomo/delay", "测试 mihomo 节点延迟", ""),
		Write("POST /api/admin/system/egress/mihomo/select", "切换 mihomo 策略组节点", "", SeverityMedium),
		View("GET /api/admin/system/egress/mihomo/connections", "查看 mihomo 连接", ""),
		Write("POST /api/admin/system/egress/mihomo/connections/close", "关闭 mihomo 连接", "", SeverityMedium),
		Write("POST /api/admin/system/egress/mihomo/providers", "更新 mihomo 订阅或规则集", "", SeverityLow),
		View("GET /api/admin/system/egress/mihomo/rules", "查看 mihomo 生效规则", ""),
		View("GET /api/admin/system/egress/mihomo/logs", "查看 mihomo 日志", ""),
		View("POST /api/admin/system/egress/mihomo/dns", "经 mihomo 解析域名", ""),
		Write("POST /api/admin/system/egress/mihomo/maintenance", "维护 mihomo 核心", "", SeverityLow),

		// 流量管理（内置插件 traffic）
		View("GET /api/admin/system/traffic/overview", "查看流量总览", ""),
		View("GET /api/admin/system/traffic/config", "查看流量配置", ""),
		View("GET /api/admin/system/traffic/templates", "查看流量规则模板", ""),
		Write("PUT /api/admin/system/traffic/settings", "修改流量全局设置", "", SeverityHigh),
		Write("POST /api/admin/system/traffic/rules", "新建流量规则", "", SeverityHigh),
		Write("POST /api/admin/system/traffic/rules/reorder", "调整流量规则顺序", "", SeverityHigh),
		Write("PUT /api/admin/system/traffic/rules/:ruleId", "修改流量规则", "", SeverityHigh),
		Write("DELETE /api/admin/system/traffic/rules/:ruleId", "删除流量规则", "", SeverityHigh),
		Write("POST /api/admin/system/traffic/rules/:ruleId/toggle", "启停流量规则", "", SeverityHigh),
		View("GET /api/admin/system/traffic/rules/:ruleId/stats", "查看流量规则统计", ""),
		View("POST /api/admin/system/traffic/validate", "校验流量规则", ""),
		View("POST /api/admin/system/traffic/simulate", "模拟流量请求", ""),
		View("GET /api/admin/system/traffic/revisions", "查看流量配置修订", ""),
		View("GET /api/admin/system/traffic/revisions/:revision", "查看流量配置修订详情", ""),
		Write("POST /api/admin/system/traffic/rollback", "回滚流量配置", "", SeverityHigh),
		View("GET /api/admin/system/traffic/history", "查看流量历史", ""),
		View("GET /api/admin/system/traffic/events", "查看流量事件", ""),
		Write("POST /api/admin/system/traffic/reset-stats", "清零流量统计", "", SeverityMedium),
		// 内置插件
		View("GET /api/admin/system/native-plugins", "查看内置插件", ""),
		Write("POST /api/admin/system/native-plugins/:pluginId/enable", "启用内置插件", "", SeverityHigh),
		Write("POST /api/admin/system/native-plugins/:pluginId/disable", "停用内置插件", "", SeverityHigh),
		Write("POST /api/admin/system/native-plugins/:pluginId/restart", "重启内置插件", "", SeverityMedium),

		// 身份源与验证码
		View("POST /api/admin/system/ldap/test", "测试 LDAP 连接", ""),
		View("POST /api/admin/system/oidc/test", "测试 OIDC 配置", ""),
		View("POST /api/admin/system/saml/test", "测试 SAML 配置", ""),
		View("POST /api/admin/system/captcha/preview", "预览动态验证码", ""),
		View("GET /api/admin/oauth-providers/templates", "查看第三方登录模板", ""),
	)
}
