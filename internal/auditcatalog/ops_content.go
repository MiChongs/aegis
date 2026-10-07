package auditcatalog

// 内容运营：应用轮播图与公告、平台横幅与系统公告、法律文书、设备型号名称库。
func init() {
	Register("content",
		// 应用内容
		View("GET /api/admin/apps/:appkey/content/overview", "查看内容总览", ""),
		View("GET /api/admin/apps/:appkey/banners", "查看轮播图", ""),
		Export("GET /api/admin/apps/:appkey/banners/export", "导出轮播图", "轮播图"),
		Write("POST /api/admin/apps/:appkey/banners", "新建轮播图", "轮播图", SeverityLow),
		Write("POST /api/admin/apps/:appkey/banners/image", "上传轮播图图片", "轮播图", SeverityLow),
		Write("PUT /api/admin/apps/:appkey/banners/:bannerId", "编辑轮播图", "轮播图", SeverityLow),
		Write("PUT /api/admin/apps/:appkey/banners/order", "调整轮播图顺序", "轮播图", SeverityLow),
		Write("DELETE /api/admin/apps/:appkey/banners/:bannerId", "删除轮播图", "轮播图", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/banners", "批量删除轮播图", "轮播图", SeverityMedium),
		View("GET /api/admin/apps/:appkey/notices", "查看应用公告", ""),
		Export("GET /api/admin/apps/:appkey/notices/export", "导出应用公告", "公告"),
		Write("POST /api/admin/apps/:appkey/notices", "新建应用公告", "公告", SeverityLow),
		Write("PUT /api/admin/apps/:appkey/notices/:noticeId", "编辑应用公告", "公告", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/notices/:noticeId", "删除应用公告", "公告", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/notices", "批量删除应用公告", "公告", SeverityMedium),

		// 平台横幅
		View("GET /api/admin/system/banners", "查看平台横幅", ""),
		View("GET /api/admin/system/banners/active", "查看生效中的平台横幅", ""),
		View("GET /api/admin/system/banners/:id", "查看平台横幅详情", "平台横幅"),
		Write("POST /api/admin/system/banners", "新建平台横幅", "平台横幅", SeverityLow),
		Write("POST /api/admin/system/banners/upload", "上传平台横幅图片", "平台横幅", SeverityLow),
		Write("PUT /api/admin/system/banners/:id", "编辑平台横幅", "平台横幅", SeverityLow),
		Write("DELETE /api/admin/system/banners/:id", "删除平台横幅", "平台横幅", SeverityMedium),
		Write("POST /api/admin/system/banners/bulk-delete", "批量删除平台横幅", "平台横幅", SeverityMedium),

		// 系统公告
		View("GET /api/admin/system/announcements", "查看系统公告", ""),
		View("GET /api/admin/system/announcements/:id", "查看系统公告详情", "系统公告"),
		Write("POST /api/admin/system/announcements", "新建系统公告", "系统公告", SeverityLow),
		Write("PUT /api/admin/system/announcements/:id", "编辑系统公告", "系统公告", SeverityMedium),
		Write("POST /api/admin/system/announcements/:id/publish", "发布系统公告", "系统公告", SeverityMedium),
		Write("POST /api/admin/system/announcements/:id/archive", "归档系统公告", "系统公告", SeverityMedium),
		Write("DELETE /api/admin/system/announcements/:id", "删除系统公告", "系统公告", SeverityMedium),

		// 法律文书
		View("GET /api/admin/system/legal/documents", "查看法律文书", ""),
		View("GET /api/admin/system/legal/documents/:docType/:locale", "查看法律文书详情", "法律文书"),
		View("POST /api/admin/system/legal/documents/:docType/:locale/preview", "预览法律文书", "法律文书"),
		Write("PUT /api/admin/system/legal/documents/:docType/:locale", "保存法律文书", "法律文书", SeverityHigh),
		Write("DELETE /api/admin/system/legal/documents/:docType/:locale", "删除法律文书", "法律文书", SeverityHigh),

		// 设备型号名称库
		View("GET /api/admin/system/device-marketing-names", "查看设备型号名称", ""),
		View("GET /api/admin/system/device-marketing-names/manufacturers", "查看设备厂商", ""),
		View("GET /api/admin/system/device-marketing-names/lookup", "查询设备型号名称", ""),
		View("GET /api/admin/system/device-marketing-names/:id", "查看设备型号名称详情", "设备型号"),
		Write("POST /api/admin/system/device-marketing-names", "新建设备型号名称", "设备型号", SeverityLow),
		Write("PUT /api/admin/system/device-marketing-names/:id", "编辑设备型号名称", "设备型号", SeverityLow),
		Write("DELETE /api/admin/system/device-marketing-names/:id", "删除设备型号名称", "设备型号", SeverityMedium),
		Write("POST /api/admin/system/device-marketing-names/seed", "补齐内置设备型号名称", "", SeverityLow),
	)
}
