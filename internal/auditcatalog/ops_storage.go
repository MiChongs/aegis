package auditcatalog

// 存储：平台对象存储（对象、回收站、规则、图片处理、CDN）、应用存储配置与用户云存储。
func init() {
	Register("storage",
		View("GET /api/admin/system/storage/usage", "查看存储用量", ""),
		View("GET /api/admin/system/storage/usage/history", "查看存储用量趋势", ""),
		View("GET /api/admin/system/storage/objects", "查看存储对象", ""),
		View("GET /api/admin/system/storage/objects/:objectId", "查看存储对象详情", "存储对象"),
		View("GET /api/admin/system/storage/objects/:objectId/thumbnail", "查看存储对象缩略图", "存储对象"),
		Write("POST /api/admin/system/storage/objects/:objectId/link", "生成对象访问链接", "存储对象", SeverityLow),
		Write("POST /api/admin/system/storage/objects/:objectId/restore", "恢复存储对象", "存储对象", SeverityLow),
		Write("POST /api/admin/system/storage/objects/batch", "批量处理存储对象", "存储对象", SeverityHigh),
		Write("DELETE /api/admin/system/storage/objects/:objectId", "删除存储对象", "存储对象", SeverityMedium),
		Write("DELETE /api/admin/system/storage/objects/:objectId/permanent", "永久删除存储对象", "存储对象", SeverityHigh),
		View("GET /api/admin/system/storage/trash", "查看存储回收站", ""),
		Write("POST /api/admin/system/storage/trash/cleanup", "清空存储回收站", "", SeverityHigh),

		View("GET /api/admin/system/storage/rules", "查看存储规则", ""),
		Write("POST /api/admin/system/storage/rules", "新建存储规则", "存储规则", SeverityMedium),
		Write("PUT /api/admin/system/storage/rules/:ruleId", "编辑存储规则", "存储规则", SeverityMedium),
		Write("DELETE /api/admin/system/storage/rules/:ruleId", "删除存储规则", "存储规则", SeverityHigh),
		View("GET /api/admin/system/storage/image-rules", "查看图片处理规则", ""),
		Write("POST /api/admin/system/storage/image-rules", "新建图片处理规则", "图片处理规则", SeverityLow),
		Write("DELETE /api/admin/system/storage/image-rules/:ruleId", "删除图片处理规则", "图片处理规则", SeverityMedium),
		View("GET /api/admin/system/storage/cdn/:configId", "查看 CDN 配置", "CDN 配置"),
		Write("PUT /api/admin/system/storage/cdn/:configId", "保存 CDN 配置", "CDN 配置", SeverityMedium),
		Write("DELETE /api/admin/system/storage/cdn/:configId", "删除 CDN 配置", "CDN 配置", SeverityHigh),

		View("POST /api/admin/app/storage-config/list", "查看存储配置（兼容接口）", ""),
		View("POST /api/admin/app/storage-config/detail", "查看存储配置详情（兼容接口）", "存储配置"),
		Write("POST /api/admin/app/storage-config/create", "新建存储配置（兼容接口）", "存储配置", SeverityMedium),
		Write("POST /api/admin/app/storage-config/update", "编辑存储配置（兼容接口）", "存储配置", SeverityMedium),
		Write("POST /api/admin/app/storage-config/delete", "删除存储配置（兼容接口）", "存储配置", SeverityHigh),
		View("POST /api/admin/app/storage-config/test", "测试存储配置（兼容接口）", "存储配置"),

		View("GET /api/admin/apps/:appkey/cloud-storage/config", "查看云存储配置", ""),
		Write("PUT /api/admin/apps/:appkey/cloud-storage/config", "修改云存储配置", "", SeverityMedium),
		View("GET /api/admin/apps/:appkey/cloud-storage/stats", "查看云存储统计", ""),
		View("GET /api/admin/apps/:appkey/cloud-storage/users", "查看云存储用户", ""),
		Write("POST /api/admin/apps/:appkey/cloud-storage/purge-expired", "清理过期云存储回收站", "", SeverityHigh),
	)
}
