package auditcatalog

// 工单：工单处理、分类、处理组、快捷回复与 SLA 策略。
func init() {
	Register("support",
		View("GET /api/admin/tickets", "查看工单列表", ""),
		View("GET /api/admin/tickets/:ticketId", "查看工单详情", "工单"),
		View("GET /api/admin/tickets/workbench", "查看工单工作台", ""),
		View("GET /api/admin/tickets/metadata", "查看工单元数据", ""),
		View("GET /api/admin/tickets/stats", "查看工单统计", ""),
		View("GET /api/admin/tickets/trend", "查看工单趋势", ""),
		View("GET /api/admin/tickets/agents", "查看客服处理统计", ""),
		Export("GET /api/admin/tickets/export", "导出工单", ""),
		Write("POST /api/admin/tickets", "新建工单", "工单", SeverityLow),
		Write("PATCH /api/admin/tickets/:ticketId", "编辑工单", "工单", SeverityLow),
		Write("DELETE /api/admin/tickets/:ticketId", "删除工单", "工单", SeverityHigh),
		Write("POST /api/admin/tickets/:ticketId/assign", "指派工单", "工单", SeverityLow),
		Write("POST /api/admin/tickets/:ticketId/replies", "回复工单", "工单", SeverityLow),
		Write("POST /api/admin/tickets/:ticketId/status", "修改工单状态", "工单", SeverityLow),
		Write("POST /api/admin/tickets/:ticketId/watch", "关注工单", "工单", SeverityLow),
		Write("PUT /api/admin/tickets/:ticketId/watchers", "设置工单关注人", "工单", SeverityLow),
		Write("POST /api/admin/tickets/attachments", "上传工单附件", "工单附件", SeverityLow),
		Write("POST /api/admin/tickets/bulk", "批量处理工单", "工单", SeverityHigh),

		View("GET /api/admin/tickets/categories", "查看工单分类", ""),
		Write("POST /api/admin/tickets/categories", "新建工单分类", "工单分类", SeverityLow),
		Write("PUT /api/admin/tickets/categories/:id", "编辑工单分类", "工单分类", SeverityLow),
		Write("DELETE /api/admin/tickets/categories/:id", "删除工单分类", "工单分类", SeverityMedium),

		View("GET /api/admin/tickets/groups", "查看工单处理组", ""),
		Write("POST /api/admin/tickets/groups", "新建工单处理组", "处理组", SeverityLow),
		Write("PUT /api/admin/tickets/groups/:id", "编辑工单处理组", "处理组", SeverityLow),
		Write("PUT /api/admin/tickets/groups/:id/members", "设置处理组成员", "处理组", SeverityMedium),
		Write("DELETE /api/admin/tickets/groups/:id", "删除工单处理组", "处理组", SeverityMedium),

		View("GET /api/admin/tickets/quick-replies", "查看快捷回复", ""),
		Write("POST /api/admin/tickets/quick-replies", "新建快捷回复", "快捷回复", SeverityLow),
		Write("PUT /api/admin/tickets/quick-replies/:id", "编辑快捷回复", "快捷回复", SeverityLow),
		Write("DELETE /api/admin/tickets/quick-replies/:id", "删除快捷回复", "快捷回复", SeverityMedium),

		View("GET /api/admin/tickets/sla-policies", "查看 SLA 策略", ""),
		Write("POST /api/admin/tickets/sla-policies", "新建 SLA 策略", "SLA 策略", SeverityLow),
		Write("PUT /api/admin/tickets/sla-policies/:id", "编辑 SLA 策略", "SLA 策略", SeverityMedium),
		Write("DELETE /api/admin/tickets/sla-policies/:id", "删除 SLA 策略", "SLA 策略", SeverityMedium),
	)
}
