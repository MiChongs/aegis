package auditcatalog

// 消息通知：管理员通知中心（渠道、订阅、模板、投递）、站内信、邮件通道与消息模板。
func init() {
	Register("notify",
		// 管理员通知中心
		View("GET /api/admin/notify/catalog", "查看通知事件目录", ""),
		View("GET /api/admin/notify/channels", "查看通知渠道", ""),
		View("GET /api/admin/notify/channels/:id", "查看通知渠道详情", "通知渠道"),
		Write("POST /api/admin/notify/channels", "新建通知渠道", "通知渠道", SeverityMedium),
		Write("PUT /api/admin/notify/channels/:id", "编辑通知渠道", "通知渠道", SeverityMedium),
		Write("DELETE /api/admin/notify/channels/:id", "删除通知渠道", "通知渠道", SeverityMedium),
		Write("POST /api/admin/notify/channels/:id/test", "发送通知渠道测试消息", "通知渠道", SeverityLow),
		View("GET /api/admin/notify/subscriptions", "查看通知订阅", ""),
		Write("POST /api/admin/notify/subscriptions", "新建通知订阅", "通知订阅", SeverityLow),
		Write("PUT /api/admin/notify/subscriptions/:id", "编辑通知订阅", "通知订阅", SeverityLow),
		Write("DELETE /api/admin/notify/subscriptions/:id", "删除通知订阅", "通知订阅", SeverityMedium),
		View("GET /api/admin/notify/templates", "查看通知模板", ""),
		View("POST /api/admin/notify/templates/preview", "预览通知模板", "通知模板"),
		Write("POST /api/admin/notify/templates", "新建通知模板", "通知模板", SeverityLow),
		Write("PUT /api/admin/notify/templates/:id", "编辑通知模板", "通知模板", SeverityLow),
		Write("DELETE /api/admin/notify/templates/:id", "删除通知模板", "通知模板", SeverityMedium),
		View("GET /api/admin/notify/deliveries", "查看通知投递记录", ""),
		View("GET /api/admin/notify/deliveries/stats", "查看通知投递统计", ""),
		Write("DELETE /api/admin/notify/deliveries", "清理通知投递记录", "通知投递记录", SeverityHigh),

		// 应用站内信
		View("GET /api/admin/apps/:appkey/notifications", "查看站内信", ""),
		Export("GET /api/admin/apps/:appkey/notifications/export", "导出站内信", "站内信"),
		Write("POST /api/admin/apps/:appkey/notifications/bulk", "群发站内信", "站内信", SeverityHigh),
		Write("DELETE /api/admin/apps/:appkey/notifications", "批量删除站内信", "站内信", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/notifications/delete-by-filter", "按条件删除站内信", "站内信", SeverityHigh),

		// 平台邮件通道
		View("GET /api/admin/system/email/channel", "查看平台生效邮件通道", ""),
		View("GET /api/admin/system/email/providers", "查看邮件服务商", ""),
		View("GET /api/admin/system/email/configs", "查看平台邮件配置", ""),
		View("GET /api/admin/system/email/configs/:configId", "查看平台邮件配置详情", "邮件配置"),
		Write("POST /api/admin/system/email/configs", "新建平台邮件配置", "邮件配置", SeverityMedium),
		Write("PUT /api/admin/system/email/configs/:configId", "编辑平台邮件配置", "邮件配置", SeverityMedium),
		Write("DELETE /api/admin/system/email/configs/:configId", "删除平台邮件配置", "邮件配置", SeverityHigh),
		Write("POST /api/admin/system/email/configs/:configId/test", "发送平台测试邮件", "邮件配置", SeverityLow),
		View("GET /api/admin/system/email/deliveries", "查看平台邮件投递记录", ""),
		View("GET /api/admin/system/email/stats", "查看平台邮件投递统计", ""),

		// 应用邮件通道（兼容接口）
		View("POST /api/admin/app/email-config/channel", "查看应用生效邮件通道", ""),
		View("POST /api/admin/app/email-config/providers", "查看邮件服务商（兼容接口）", ""),
		View("POST /api/admin/app/email-config/list", "查看应用邮件配置", ""),
		View("POST /api/admin/app/email-config/detail", "查看应用邮件配置详情", "邮件配置"),
		Write("POST /api/admin/app/email-config/create", "新建应用邮件配置", "邮件配置", SeverityMedium),
		Write("POST /api/admin/app/email-config/update", "编辑应用邮件配置", "邮件配置", SeverityMedium),
		Write("POST /api/admin/app/email-config/delete", "删除应用邮件配置", "邮件配置", SeverityHigh),
		Write("POST /api/admin/app/email-config/test", "发送应用测试邮件", "邮件配置", SeverityLow),
		View("POST /api/admin/app/email-config/deliveries", "查看应用邮件投递记录", ""),
		View("POST /api/admin/app/email-config/stats", "查看应用邮件投递统计", ""),

		// 消息模板
		View("GET /api/admin/system/templates", "查看消息模板", ""),
		View("GET /api/admin/system/templates/:code", "查看消息模板详情", "消息模板"),
		View("POST /api/admin/system/templates/:code/preview", "预览消息模板", "消息模板"),
		Write("POST /api/admin/system/templates", "新建消息模板", "消息模板", SeverityLow),
		Write("PUT /api/admin/system/templates/:code", "编辑消息模板", "消息模板", SeverityMedium),
		Write("DELETE /api/admin/system/templates/:code", "删除消息模板", "消息模板", SeverityMedium),
	)
}
