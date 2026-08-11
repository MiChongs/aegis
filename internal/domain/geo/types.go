// Package geo 定义地理风控与地理分析的领域类型。
//
// 设计分层（性能约束）：
//   - 请求热路径只使用内存判定（GeoFenceService 编译后的围栏），不触达本包之外的 DB 类型
//   - login_geo_events / user_geo_profiles 由 Worker 异步写入
//   - 分析查询（热力图/聚类/轨迹）全部走预聚合或带时间窗口的 PostGIS 查询
package geo

import (
	"encoding/json"
	"slices"
	"time"
)

// ──────────────────────────────────────
// 地理围栏
// ──────────────────────────────────────

// 围栏模式。
const (
	FenceModeDeny   = "deny"   // 区域内 → 拦截
	FenceModeAllow  = "allow"  // 存在任一 allow 围栏时，区域外 → 拦截
	FenceModeReview = "review" // 区域内 → 仅记录命中，不拦截
)

// AllFenceModes 返回所有围栏模式（DTO 校验/前端枚举用）。
func AllFenceModes() []string {
	return []string{FenceModeDeny, FenceModeAllow, FenceModeReview}
}

// Fence 地理围栏规则。多边形围栏与圆形围栏二选一：
//   - Fence 字段非空：MultiPolygon GeoJSON
//   - Center + RadiusM 非空：圆形围栏
type Fence struct {
	ID          int64           `json:"id"`
	AppID       *int64          `json:"appId,omitempty"` // NULL = 平台级
	Name        string          `json:"name"`
	Mode        string          `json:"mode"` // deny / allow / review
	Fence       json.RawMessage `json:"fence,omitempty"` // GeoJSON MultiPolygon
	CenterLat   *float64        `json:"centerLat,omitempty"`
	CenterLng   *float64        `json:"centerLng,omitempty"`
	RadiusM     *float64        `json:"radiusM,omitempty"`
	BanMode     string          `json:"banMode"` // 命中后的响应模式；空 = 平台默认
	Reason      string          `json:"reason"`
	Enabled     bool            `json:"enabled"`
	ExpiresAt   *time.Time      `json:"expiresAt,omitempty"`
	MatchCount  int64           `json:"matchCount"`
	LastMatchAt *time.Time      `json:"lastMatchAt,omitempty"`
	CreatedBy   *int64          `json:"createdBy,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// FenceMutation 创建/更新围栏的载荷。
type FenceMutation struct {
	AppID        *int64
	Name         string
	Mode         string
	FenceGeoJSON string // 空 = 圆形围栏
	CenterLat    *float64
	CenterLng    *float64
	RadiusM      *float64
	BanMode      string
	Reason       string
	Enabled      bool
	ExpiresAt    *time.Time
}

// FencePreview 围栏影响面回测结果。
type FencePreview struct {
	WindowDays    int   `json:"windowDays"`
	LoginMatches  int64 `json:"loginMatches"`  // 命中窗口内登录事件数
	BlockMatches  int64 `json:"blockMatches"`  // 命中窗口内防火墙拦截数
	UniqueUsers   int64 `json:"uniqueUsers"`   // 受影响用户数
}

// ──────────────────────────────────────
// 登录地理事件 & 用户画像
// ──────────────────────────────────────

// LoginEvent 单次登录的地理事件（写入 login_geo_events）。
type LoginEvent struct {
	UserID      int64      `json:"userId"`
	AppID       int64      `json:"appId"`
	IP          string     `json:"ip"`
	CountryCode string     `json:"countryCode"`
	Country     string     `json:"country"`
	Region      string     `json:"region"`
	City        string     `json:"city"`
	ASN         string     `json:"asn"`
	ISP         string     `json:"isp"`
	Lat         *float64   `json:"lat,omitempty"`
	Lng         *float64   `json:"lng,omitempty"`
	LoginType   string     `json:"loginType"`
	DeviceID    string     `json:"deviceId"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// Profile 用户地理画像（近线风控判定的唯一读取对象）。
type Profile struct {
	UserID         int64      `json:"userId"`
	AppID          int64      `json:"appId"`
	HomeLat        *float64   `json:"homeLat,omitempty"`
	HomeLng        *float64   `json:"homeLng,omitempty"`
	HomeRadiusM    float64    `json:"homeRadiusM"`
	KnownCountries []string   `json:"knownCountries"`
	LastLat        *float64   `json:"lastLat,omitempty"`
	LastLng        *float64   `json:"lastLng,omitempty"`
	LastCountry    string     `json:"lastCountry"`
	LastIP         string     `json:"lastIp"`
	LastLoginAt    *time.Time `json:"lastLoginAt,omitempty"`
	LoginCount     int64      `json:"loginCount"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// KnowsCountry 判断国家是否在画像的已知列表中。
func (p *Profile) KnowsCountry(code string) bool {
	if p == nil || code == "" {
		return false
	}
	return slices.Contains(p.KnownCountries, code)
}

// TrailPoint 用户登录轨迹点（管理端回放）。
type TrailPoint struct {
	IP          string    `json:"ip"`
	CountryCode string    `json:"countryCode"`
	Country     string    `json:"country"`
	Region      string    `json:"region"`
	City        string    `json:"city"`
	Lat         *float64  `json:"lat,omitempty"`
	Lng         *float64  `json:"lng,omitempty"`
	LoginType   string    `json:"loginType"`
	DeviceID    string    `json:"deviceId"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ──────────────────────────────────────
// 分析查询
// ──────────────────────────────────────

// 聚合事件类别（geo_stats_hourly.kind）。
const (
	StatsKindBlock = "block"
	StatsKindLogin = "login"
)

// HeatmapQuery 热力图查询参数。
type HeatmapQuery struct {
	Kind    string    // block / login
	Start   time.Time
	End     time.Time
	Country string // 可选：限定国家
	Limit   int
}

// HeatmapCell 热力图网格（geohash 精度 5，约 5km）。
type HeatmapCell struct {
	Geohash     string  `json:"geohash"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	CountryCode string  `json:"countryCode"`
	City        string  `json:"city"`
	Count       int64   `json:"count"`
}

// CountryStat 国家维度统计。
type CountryStat struct {
	CountryCode string `json:"countryCode"`
	Count       int64  `json:"count"`
}

// HeatmapResult 热力图响应。
type HeatmapResult struct {
	Cells     []HeatmapCell `json:"cells"`
	Countries []CountryStat `json:"countries"`
	Total     int64         `json:"total"`
}

// Cluster 攻击源空间聚类（DBSCAN）。
type Cluster struct {
	ClusterID int     `json:"clusterId"`
	Hits      int64   `json:"hits"`
	UniqueIPs int64   `json:"uniqueIps"`
	CenterLat float64 `json:"centerLat"`
	CenterLng float64 `json:"centerLng"`
	TopReason string  `json:"topReason"`
}

// ──────────────────────────────────────
// 风控评估
// ──────────────────────────────────────

// SceneGeoLogin 地理登录风控场景（写入 risk_assessments.scene）。
const SceneGeoLogin = "geo_login"

// 地理风控规则名（risk_assessments.matched_rules 中的 ruleName）。
const (
	RuleImpossibleTravel = "geo_impossible_travel"
	RuleNewCountry       = "geo_new_country"
	RuleFarFromHome      = "geo_far_from_home"
)
