package device

import "time"

// 平台枚举
const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
)

// 来源枚举
const (
	SourceSeed   = "seed"   // 初始化种子数据
	SourceManual = "manual" // 管理员手工维护
	SourceImport = "import" // 批量导入
)

// MarketingName 设备识别码到营销名称的一条映射
type MarketingName struct {
	ID                  int64     `json:"id"`
	Platform            string    `json:"platform"`            // ios / android
	Identifier          string    `json:"identifier"`          // 机器标识（iPhone14,3 / SM-G998B）
	MarketingName       string    `json:"marketingName"`       // 营销名称（iPhone 13 Pro Max）
	Manufacturer        string    `json:"manufacturer"`        // 厂商（Apple / Samsung / Xiaomi / OPPO ...）
	ManufacturerIconURL string    `json:"manufacturerIconUrl"` // 厂商图标 URL
	DeviceImageURL      string    `json:"deviceImageUrl"`      // 设备渲染图 URL
	Source              string    `json:"source"`              // seed / manual / import
	Notes               string    `json:"notes"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// Filter 查询过滤条件
type Filter struct {
	Platform     string `json:"platform"`     // ios / android，为空则不过滤
	Keyword      string `json:"keyword"`      // identifier / marketingName 模糊匹配
	Source       string `json:"source"`
	Manufacturer string `json:"manufacturer"` // 精确厂商过滤
	Page         int    `json:"page"`
	Limit        int    `json:"limit"`
}

// Page 分页结果
type Page struct {
	Items []MarketingName `json:"items"`
	Total int64           `json:"total"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
}

// CreateInput 新建入参
type CreateInput struct {
	Platform            string `json:"platform"`
	Identifier          string `json:"identifier"`
	MarketingName       string `json:"marketingName"`
	Manufacturer        string `json:"manufacturer"`
	ManufacturerIconURL string `json:"manufacturerIconUrl"`
	DeviceImageURL      string `json:"deviceImageUrl"`
	Notes               string `json:"notes"`
}

// UpdateInput 更新入参（nil 代表不改）
type UpdateInput struct {
	MarketingName       *string `json:"marketingName,omitempty"`
	Manufacturer        *string `json:"manufacturer,omitempty"`
	ManufacturerIconURL *string `json:"manufacturerIconUrl,omitempty"`
	DeviceImageURL      *string `json:"deviceImageUrl,omitempty"`
	Notes               *string `json:"notes,omitempty"`
	Platform            *string `json:"platform,omitempty"`
	Identifier          *string `json:"identifier,omitempty"`
}

// SeedStats 种子/导入统计
type SeedStats struct {
	Parsed    int           `json:"parsed"`    // 解析出的总行数
	Inserted  int           `json:"inserted"`  // 实际新增行数（ON CONFLICT DO NOTHING）
	Duplicate int           `json:"duplicate"` // 命中已有记录的行数
	Elapsed   time.Duration `json:"-"`
	ElapsedMs int64         `json:"elapsedMs"`
}
