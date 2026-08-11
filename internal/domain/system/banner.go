package system

import "time"

// PlatformBanner 平台级 Banner — 面向管理员的全局横幅。
// 与应用级 app.Banner 的差异：无 appid 作用域；image_url 专列；仅超管可管理。
type PlatformBanner struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	ImageURL    string     `json:"imageUrl"`                      // 持久化的规范形态（storage:// 引用 或 外链 URL）
	ImageDisplayURL string `json:"imageDisplayUrl,omitempty"`     // 读取时解析得到的可直接展示的 URL；持久化为空
	ClickURL    string     `json:"clickUrl,omitempty"`
	Type        string     `json:"type"` // info / notice / maintenance / release / security
	Position    int        `json:"position"`
	Status      bool       `json:"status"`
	StartTime   *time.Time `json:"startTime,omitempty"`
	EndTime     *time.Time `json:"endTime,omitempty"`
	CreatedBy   *int64     `json:"createdBy,omitempty"`
	ViewCount   int64      `json:"viewCount"`
	ClickCount  int64      `json:"clickCount"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// PlatformBannerMutation 新建/更新请求（指针字段支持 Patch 语义）。
type PlatformBannerMutation struct {
	ID          int64
	Title       *string
	Description *string
	ImageURL    *string
	ClickURL    *string
	Type        *string
	Position    *int
	Status      *bool
	StartTime   *time.Time
	EndTime     *time.Time
	CreatedBy   *int64
}

// PlatformBannerFilter 列表查询过滤。
type PlatformBannerFilter struct {
	Status  *bool
	Type    string
	Keyword string
	Limit   int
	Offset  int
}

// ValidPlatformBannerTypes 白名单校验使用。
var ValidPlatformBannerTypes = map[string]struct{}{
	"info":        {},
	"notice":      {},
	"maintenance": {},
	"release":     {},
	"security":    {},
}
