package httptransport

// IPBanCreateRequest 手动封禁 IP 请求
type IPBanCreateRequest struct {
	IP       string `json:"ip" binding:"required"`
	Reason   string `json:"reason"`
	Duration int64  `json:"duration"`          // 秒，0=永久
	Mode     string `json:"mode"`              // forbidden / silent_drop / connection_reset / tarpit / stealth_404 / teapot / rate_choke，空=沿用全局默认
	DelayMs  int    `json:"delayMs,omitempty"` // tarpit 模式延迟毫秒，0=沿用配置
}

// IPBanModesResponse 可选封禁模式列表（供前端下拉渲染）
type IPBanModesResponse struct {
	Default string           `json:"default"`
	Modes   []IPBanModeEntry `json:"modes"`
}

type IPBanModeEntry struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// IPBanListRequest 封禁列表查询参数
type IPBanListRequest struct {
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
	IP       string `form:"ip"`
	Status   string `form:"status"` // all | active | expired | revoked
	Source   string `form:"source"` // all | manual | auto
}
