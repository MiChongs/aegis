package auditcatalog

// 数据报表：应用级统计与各类报表。全部只读，导出单独标出。
func init() {
	Register("report",
		View("GET /api/admin/apps/:appkey/stats", "查看应用统计", ""),
		View("GET /api/admin/apps/:appkey/stats/auth-sources", "查看登录来源统计", ""),
		View("GET /api/admin/apps/:appkey/stats/regions", "查看地区分布统计", ""),
		View("GET /api/admin/apps/:appkey/stats/user-trend", "查看用户增长趋势", ""),
		View("GET /api/admin/apps/:appkey/reports/active", "查看活跃用户报表", ""),
		View("GET /api/admin/apps/:appkey/reports/activity", "查看用户行为报表", ""),
		View("GET /api/admin/apps/:appkey/reports/channel", "查看渠道报表", ""),
		View("GET /api/admin/apps/:appkey/reports/device", "查看设备报表", ""),
		View("GET /api/admin/apps/:appkey/reports/funnel", "查看转化漏斗报表", ""),
		View("GET /api/admin/apps/:appkey/reports/login", "查看登录报表", ""),
		View("GET /api/admin/apps/:appkey/reports/notification", "查看通知报表", ""),
		View("GET /api/admin/apps/:appkey/reports/payment", "查看支付报表", ""),
		View("GET /api/admin/apps/:appkey/reports/region", "查看地区报表", ""),
		View("GET /api/admin/apps/:appkey/reports/registration", "查看注册报表", ""),
		View("GET /api/admin/apps/:appkey/reports/retention", "查看留存报表", ""),
		View("GET /api/admin/apps/:appkey/reports/risk", "查看风险报表", ""),
		Export("GET /api/admin/apps/:appkey/reports/export", "导出报表", ""),
	)
}
