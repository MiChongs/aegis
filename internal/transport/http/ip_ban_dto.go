package httptransport

import (
	"strings"

	firewalldomain "aegis/internal/domain/firewall"
)

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

// IPBanListRequest 封禁列表查询参数（列表、计数与按筛选批量解封共用）
type IPBanListRequest struct {
	Page           int    `form:"page" json:"page"`
	PageSize       int    `form:"pageSize" json:"pageSize"`
	IP             string `form:"ip" json:"ip"`
	Status         string `form:"status" json:"status"`   // all | active | expired | revoked
	Source         string `form:"source" json:"source"`   // all | manual | auto | risk | rule
	Country        string `form:"country" json:"country"` // 国家代码
	Mode           string `form:"mode" json:"mode"`       // 响应模式；default = 沿用全局默认
	Keyword        string `form:"keyword" json:"keyword"` // IP 前缀 / 原因 / 触发规则 / 运营商 / 城市
	ExpiringWithin int64  `form:"expiringWithin" json:"expiringWithin"`
	Permanent      string `form:"permanent" json:"permanent"` // "" | "true" | "false"
	Sort           string `form:"sort" json:"sort"`           // created_desc | created_asc | expires_asc | ip
}

func (r IPBanListRequest) filter() firewalldomain.IPBanFilter {
	f := firewalldomain.IPBanFilter{
		IP: strings.TrimSpace(r.IP), Status: r.Status, Source: r.Source, Country: r.Country, Mode: r.Mode,
		Keyword: r.Keyword, ExpiringWithin: r.ExpiringWithin, Sort: r.Sort, Page: r.Page, PageSize: r.PageSize,
	}
	switch r.Permanent {
	case "true":
		v := true
		f.Permanent = &v
	case "false":
		v := false
		f.Permanent = &v
	}
	return f
}

// IPBanBatchCreateRequest 批量封禁
type IPBanBatchCreateRequest struct {
	IPs      []string `json:"ips" binding:"required"`
	Reason   string   `json:"reason"`
	Duration int64    `json:"duration"` // 秒，0 = 永久
	Mode     string   `json:"mode"`
	DelayMs  int      `json:"delayMs"`
}

// IPBanBatchUnbanRequest 批量解封：给 ids 解封选中的；不给 ids 则按 filter 解封全部匹配的生效中封禁，
// 此时 expected 必须等于确认时看到的条数（不符返回 40990）。
type IPBanBatchUnbanRequest struct {
	IDs      []int64           `json:"ids"`
	Filter   *IPBanListRequest `json:"filter"`
	Expected int64             `json:"expected"`
	Note     string            `json:"note"`
}

// IPBanUpdateRequest 修改生效中的封禁（字段缺省 = 不改）；batch 接口带 ids。
type IPBanUpdateRequest struct {
	IDs      []int64 `json:"ids"`
	Mode     *string `json:"mode"`
	Reason   *string `json:"reason"`
	Duration *int64  `json:"duration"` // 从现在起算的秒数，0 = 永久
	DelayMs  *int    `json:"delayMs"`
}

func (r IPBanUpdateRequest) patch() firewalldomain.IPBanUpdate {
	return firewalldomain.IPBanUpdate{Mode: r.Mode, Reason: r.Reason, Duration: r.Duration, DelayMs: r.DelayMs}
}

// GeoBanBatchRequest 地域规则批量启用 / 停用 / 删除
type GeoBanBatchRequest struct {
	IDs    []int64 `json:"ids" binding:"required"`
	Action string  `json:"action" binding:"required"` // enable | disable | delete
}
