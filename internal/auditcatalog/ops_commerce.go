package auditcatalog

// 商业化：会员、卡密、钱包、支付与退款、激励广告、签到奖励、抽奖。
func init() {
	Register("commerce",
		// 商业化设置
		View("GET /api/admin/apps/:appkey/commerce", "查看商业化设置", ""),
		View("GET /api/admin/apps/:appkey/commerce/overview", "查看商业化总览", ""),
		Write("PUT /api/admin/apps/:appkey/commerce", "修改商业化设置", "", SeverityHigh),

		// 会员
		View("GET /api/admin/apps/:appkey/vip/plans", "查看会员套餐", ""),
		Write("POST /api/admin/apps/:appkey/vip/plans", "保存会员套餐", "会员套餐", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/vip/plans/:planId", "删除会员套餐", "会员套餐", SeverityHigh),
		View("GET /api/admin/apps/:appkey/vip/features", "查看会员权益", ""),
		Write("POST /api/admin/apps/:appkey/vip/features", "保存会员权益", "会员权益", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/vip/features/:tag", "删除会员权益", "会员权益", SeverityHigh),
		View("GET /api/admin/apps/:appkey/vip/entitlement", "查看用户会员权益", "用户"),
		View("GET /api/admin/apps/:appkey/vip/transactions", "查看会员交易记录", ""),
		Write("POST /api/admin/apps/:appkey/vip/grant", "为用户开通会员", "用户", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/vip/revoke", "收回用户会员", "用户", SeverityHigh),
		View("GET /api/admin/apps/:appkey/vip/trial/claims", "查看会员试用领取记录", ""),
		Write("POST /api/admin/apps/:appkey/vip/trial/claims", "为用户开通会员试用", "用户", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/vip/trial/claims/:userId", "重置用户会员试用资格", "用户", SeverityMedium),

		// 卡密
		View("GET /api/admin/apps/:appkey/card-keys/catalog", "查看卡密商品目录", ""),
		View("GET /api/admin/apps/:appkey/card-keys/batches", "查看卡密批次", ""),
		Write("POST /api/admin/apps/:appkey/card-keys/batches", "生成卡密", "卡密批次", SeverityHigh),
		Write("PUT /api/admin/apps/:appkey/card-keys/batches/:batchId/status", "修改卡密批次状态", "卡密批次", SeverityHigh),
		Write("DELETE /api/admin/apps/:appkey/card-keys/batches/:batchId", "删除卡密批次", "卡密批次", SeverityHigh),
		Export("GET /api/admin/apps/:appkey/card-keys/batches/:batchId/export", "导出卡密", "卡密批次"),
		View("GET /api/admin/apps/:appkey/card-keys/codes", "查看卡密列表", ""),
		Write("POST /api/admin/apps/:appkey/card-keys/codes/disable", "停用卡密", "卡密", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/card-keys/codes/restore", "恢复卡密", "卡密", SeverityHigh),
		View("GET /api/admin/apps/:appkey/card-keys/codes/:cardId/devices", "查看卡密绑定设备", "卡密"),
		Write("DELETE /api/admin/apps/:appkey/card-keys/codes/:cardId/devices/:deviceId", "解绑卡密设备", "卡密", SeverityMedium),
		View("GET /api/admin/apps/:appkey/card-keys/redemptions", "查看卡密兑换记录", ""),

		// 钱包
		View("GET /api/admin/apps/:appkey/wallet/stats", "查看钱包统计", ""),
		View("GET /api/admin/apps/:appkey/wallet/transactions", "查看钱包流水", ""),
		Write("POST /api/admin/apps/:appkey/wallet/adjust", "调整用户余额", "用户", SeverityCritical),
		View("POST /api/admin/apps/:appkey/wallet/receipt", "生成钱包流水凭证", "钱包流水"),
		Write("POST /api/admin/apps/:appkey/wallet/receipt/email", "发送钱包流水凭证邮件", "钱包流水", SeverityLow),

		// 激励广告
		View("GET /api/admin/apps/:appkey/rewarded-ads/config", "查看激励广告配置", ""),
		Write("PUT /api/admin/apps/:appkey/rewarded-ads/config", "修改激励广告配置", "", SeverityHigh),
		View("GET /api/admin/apps/:appkey/rewarded-ads/stats", "查看激励广告统计", ""),
		View("GET /api/admin/apps/:appkey/rewarded-ads/views", "查看激励广告观看记录", ""),
		Write("POST /api/admin/apps/:appkey/rewarded-ads/views/:viewId/grant", "补发激励广告奖励", "观看记录", SeverityHigh),

		// 签到
		View("GET /api/admin/apps/:appkey/signin/records", "查看签到记录", ""),
		View("GET /api/admin/apps/:appkey/signin/stats", "查看签到统计", ""),
		View("GET /api/admin/apps/:appkey/signin-reward", "查看签到奖励配置", ""),
		Write("PUT /api/admin/apps/:appkey/signin-reward", "修改签到奖励配置", "", SeverityMedium),
		Write("POST /api/admin/apps/:appkey/signin-reward/reset", "重置签到奖励配置", "", SeverityHigh),
		View("POST /api/admin/apps/:appkey/signin-reward/test", "试算签到奖励", ""),

		// 抽奖
		View("GET /api/admin/apps/:appkey/lottery/activities", "查看抽奖活动列表", ""),
		View("GET /api/admin/apps/:appkey/lottery/activities/:id", "查看抽奖活动详情", "抽奖活动"),
		View("GET /api/admin/apps/:appkey/lottery/activities/:id/stats", "查看抽奖活动统计", "抽奖活动"),
		Write("POST /api/admin/apps/:appkey/lottery/activities", "新建抽奖活动", "抽奖活动", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/lottery/activities/:id", "编辑抽奖活动", "抽奖活动", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/lottery/activities/:id", "删除抽奖活动", "抽奖活动", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/lottery/activities/:id/seed/commit", "提交抽奖随机种子", "抽奖活动", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/lottery/activities/:id/seed/reveal", "公开抽奖随机种子", "抽奖活动", SeverityHigh),
		View("GET /api/admin/apps/:appkey/lottery/activities/:id/prizes", "查看抽奖奖品", "抽奖活动"),
		Write("POST /api/admin/apps/:appkey/lottery/activities/:id/prizes", "新建抽奖奖品", "抽奖奖品", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/lottery/prizes/:id", "编辑抽奖奖品", "抽奖奖品", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/lottery/prizes/:id", "删除抽奖奖品", "抽奖奖品", SeverityHigh),
		View("GET /api/admin/apps/:appkey/lottery/draws", "查看抽奖记录", ""),

		// 支付配置与订单（兼容接口）
		View("POST /api/admin/app/payment-config/list", "查看支付配置列表（兼容接口）", ""),
		View("POST /api/admin/app/payment-config/detail", "查看支付配置详情（兼容接口）", "支付配置"),
		Write("POST /api/admin/app/payment-config/create", "新建支付配置（兼容接口）", "支付配置", SeverityHigh),
		Write("POST /api/admin/app/payment-config/update", "修改支付配置（兼容接口）", "支付配置", SeverityHigh),
		Write("POST /api/admin/app/payment-config/delete", "删除支付配置（兼容接口）", "支付配置", SeverityHigh),
		View("POST /api/admin/app/payment-config/test", "测试支付配置（兼容接口）", "支付配置"),
		Write("POST /api/admin/app/payment-config/epay/init", "初始化易支付配置（兼容接口）", "支付配置", SeverityHigh),
		View("POST /api/admin/app/payment-config/methods", "查看支付方式（兼容接口）", ""),
		View("POST /api/admin/app/payment-config/receipt/options", "查看支付凭证选项（兼容接口）", ""),
		View("POST /api/admin/app/payment-config/orders/list", "查看订单列表（兼容接口）", ""),
		View("POST /api/admin/app/payment-config/orders/detail", "查看订单详情（兼容接口）", "订单"),
		View("POST /api/admin/app/payment-config/orders/receipt", "生成订单凭证（兼容接口）", "订单"),
		Write("POST /api/admin/app/payment-config/orders/receipt/email", "发送订单凭证邮件（兼容接口）", "订单", SeverityLow),
		View("POST /api/admin/app/payment-config/refunds/list", "查看退款列表（兼容接口）", ""),
		View("POST /api/admin/app/payment-config/refunds/order", "查看订单退款记录（兼容接口）", "订单"),
		View("POST /api/admin/app/payment-config/refunds/refundable", "查询订单可退金额（兼容接口）", "订单"),
		Write("POST /api/admin/app/payment-config/refunds/create", "发起退款（兼容接口）", "订单", SeverityCritical),
		Write("POST /api/admin/app/payment-config/refunds/sync", "同步退款状态（兼容接口）", "退款", SeverityMedium),
	)
}
