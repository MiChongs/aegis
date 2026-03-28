package httptransport

// IPBanCreateRequest 手动封禁 IP 请求
type IPBanCreateRequest struct {
	IP       string `json:"ip" binding:"required"`
	Reason   string `json:"reason"`
	Duration int64  `json:"duration"` // 秒，0=永久
}

// IPBanListRequest 封禁列表查询参数
type IPBanListRequest struct {
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
	IP       string `form:"ip"`
	Status   string `form:"status"` // all | active | expired | revoked
	Source   string `form:"source"` // all | manual | auto
}
