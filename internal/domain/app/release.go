package app

import "time"

// 发布中心（Release）。
//
// 与旧的 AppVersion 共用 app_versions 表，但对外是另一套 camelCase 契约：
// AppVersion 是旧控制台与 /api/user/check-version 的形状，原样保留；
// 新控制台、网关 /releases/* 与官网只认这里的类型。
//
// 一次发布由四件事决定谁能收到：
//  1. 渠道：版本挂在某个渠道上时，只有该渠道的成员（默认渠道 = 所有人）能收到
//  2. 可见范围：public 任何人 / signed_in 仅登录用户 / testers 仅内测名单
//  3. 定向条件：地区、语言、机型、ABI、系统版本、来源版本范围、排除名单（全部为「且」）
//  4. 灰度比例：按「版本 + 用户（或设备）」稳定哈希分桶，同一个人放量过程中不会忽进忽出
//
// 内测名单（testers）里的用户与设备跳过 3、4 两步：给测试同学的包不该被灰度挡住。

const (
	ReleaseStatusDraft     = "draft"
	ReleaseStatusScheduled = "scheduled"
	ReleaseStatusPublished = "published"
	ReleaseStatusPaused    = "paused"
	ReleaseStatusRevoked   = "revoked"

	ReleaseVisibilityPublic   = "public"
	ReleaseVisibilitySignedIn = "signed_in"
	ReleaseVisibilityTesters  = "testers"

	ReleaseUpdateOptional    = "optional"
	ReleaseUpdateRecommended = "recommended"
	ReleaseUpdateForce       = "force"

	ReleaseAssetUniversal = "universal"
)

var ValidReleaseStatuses = map[string]struct{}{
	ReleaseStatusDraft: {}, ReleaseStatusScheduled: {}, ReleaseStatusPublished: {},
	ReleaseStatusPaused: {}, ReleaseStatusRevoked: {},
}

var ValidReleaseVisibilities = map[string]struct{}{
	ReleaseVisibilityPublic: {}, ReleaseVisibilitySignedIn: {}, ReleaseVisibilityTesters: {},
}

var ValidReleaseUpdateTypes = map[string]struct{}{
	ReleaseUpdateOptional: {}, ReleaseUpdateRecommended: {}, ReleaseUpdateForce: {},
}

// ReleaseUpdateRank 更新类型的轻重次序，用于跨版本合并时取最重的一档。
func ReleaseUpdateRank(updateType string) int {
	switch updateType {
	case ReleaseUpdateForce:
		return 2
	case ReleaseUpdateRecommended:
		return 1
	default:
		return 0
	}
}

// ReleaseTargeting 定向条件。空字段表示不限；多个字段同时设置时取交集。
type ReleaseTargeting struct {
	// 内测名单：命中即收到，跳过定向条件与灰度
	TesterUserIDs   []int64  `json:"testerUserIds,omitempty"`
	TesterDeviceIDs []string `json:"testerDeviceIds,omitempty"`
	// 排除名单：命中即不收到，优先于一切
	ExcludeUserIDs   []int64  `json:"excludeUserIds,omitempty"`
	ExcludeDeviceIDs []string `json:"excludeDeviceIds,omitempty"`
	// 地区码（客户端上报，如 CN、HK），大小写不敏感
	Regions []string `json:"regions,omitempty"`
	// 语言标签前缀匹配：zh 命中 zh-CN 与 zh-TW
	Locales []string `json:"locales,omitempty"`
	// 机型关键字，包含匹配、大小写不敏感
	DeviceModels []string `json:"deviceModels,omitempty"`
	// 客户端支持的 ABI 中至少有一个在列表里
	Abis []string `json:"abis,omitempty"`
	// 系统版本（Android 为 API Level），0 = 不限
	MinOSVersion int `json:"minOsVersion,omitempty"`
	MaxOSVersion int `json:"maxOsVersion,omitempty"`
	// 来源版本范围：只给从这些版本升级的客户端，0 = 不限
	MinSourceVersionCode int64 `json:"minSourceVersionCode,omitempty"`
	MaxSourceVersionCode int64 `json:"maxSourceVersionCode,omitempty"`
}

// ReleaseAsset 一个安装包。
type ReleaseAsset struct {
	ID    int64  `json:"id"`
	Abi   string `json:"abi"`
	Label string `json:"label,omitempty"`
	// URL 是落库值（外链或 storage:// 引用），仅管理端可见；DownloadURL 是解析后的可访问地址
	URL           string `json:"url,omitempty"`
	DownloadURL   string `json:"downloadUrl"`
	FileSize      int64  `json:"fileSize"`
	SHA256        string `json:"sha256,omitempty"`
	Position      int    `json:"position"`
	DownloadCount int64  `json:"downloadCount"`
}

// ReleaseChannelRef 版本所在渠道的摘要。
type ReleaseChannelRef struct {
	ID    int64  `json:"id"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Level string `json:"level,omitempty"`
	Color string `json:"color,omitempty"`
}

// Release 管理端视图：全部字段。
type Release struct {
	ID               int64  `json:"id"`
	AppID            int64  `json:"appid"`
	Version          string `json:"version"`
	VersionCode      int64  `json:"versionCode"`
	Title            string `json:"title,omitempty"`
	Notes            string `json:"notes"`
	Summary          string `json:"summary,omitempty"`
	Platform         string `json:"platform"`
	MinOSVersion     string `json:"minOsVersion,omitempty"`
	UpdateType       string `json:"updateType"`
	MinSupportedCode int64  `json:"minSupportedCode"`
	Status           string `json:"status"`
	// EffectiveStatus 计入定时发布之后的实际状态：到点的 scheduled 即 published
	EffectiveStatus string             `json:"effectiveStatus"`
	Visibility      string             `json:"visibility"`
	Targeting       ReleaseTargeting   `json:"targeting"`
	RolloutPct      int                `json:"rolloutPct"`
	Channel         *ReleaseChannelRef `json:"channel,omitempty"`
	Assets          []ReleaseAsset     `json:"assets"`
	PublishAt       *time.Time         `json:"publishAt,omitempty"`
	PublishedAt     *time.Time         `json:"publishedAt,omitempty"`
	DownloadCount   int64              `json:"downloadCount"`
	CreatedBy       *int64             `json:"createdBy,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

// IsLive 此刻是否处于下发状态。
func (r *Release) IsLive(now time.Time) bool {
	switch r.Status {
	case ReleaseStatusPublished:
		return true
	case ReleaseStatusScheduled:
		return r.PublishAt != nil && !r.PublishAt.After(now)
	default:
		return false
	}
}

// PublicRelease 客户端与官网视图：不含定向条件、内测名单与落库的存储引用。
type PublicRelease struct {
	ID           int64              `json:"id"`
	Version      string             `json:"version"`
	VersionCode  int64              `json:"versionCode"`
	Title        string             `json:"title,omitempty"`
	Notes        string             `json:"notes"`
	Summary      string             `json:"summary,omitempty"`
	Platform     string             `json:"platform"`
	MinOSVersion string             `json:"minOsVersion,omitempty"`
	UpdateType   string             `json:"updateType"`
	Channel      *ReleaseChannelRef `json:"channel,omitempty"`
	Assets       []ReleaseAsset     `json:"assets"`
	PublishedAt  *time.Time         `json:"publishedAt,omitempty"`
}

// ReleaseAssetInput 保存版本时提交的安装包。整组替换：提交的就是最终集合。
type ReleaseAssetInput struct {
	ID       int64  `json:"id,omitempty"`
	Abi      string `json:"abi"`
	Label    string `json:"label,omitempty"`
	URL      string `json:"url"`
	FileSize int64  `json:"fileSize,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// ReleaseMutation 创建 / 更新。指针为 nil 表示不修改；Assets 为 nil 表示不动安装包。
type ReleaseMutation struct {
	ID               int64
	AppID            int64
	ChannelID        *int64
	ClearChannel     bool
	Version          *string
	VersionCode      *int64
	Title            *string
	Notes            *string
	Platform         *string
	MinOSVersion     *string
	UpdateType       *string
	MinSupportedCode *int64
	Visibility       *string
	Targeting        *ReleaseTargeting
	RolloutPct       *int
	Assets           []ReleaseAssetInput
	CreatedBy        *int64
}

// ReleaseListQuery 管理端列表过滤。
type ReleaseListQuery struct {
	Status    string
	Platform  string
	ChannelID int64
	Keyword   string
	Page      int
	Limit     int
}

type ReleaseListResult struct {
	Items      []Release `json:"items"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	Total      int64     `json:"total"`
	TotalPages int       `json:"totalPages"`
}

// ReleaseClient 一次检测里客户端是谁、在什么环境。
type ReleaseClient struct {
	VersionCode int64    `json:"versionCode"`
	Platform    string   `json:"platform"`
	Abis        []string `json:"abis,omitempty"`
	OSVersion   int      `json:"osVersion,omitempty"`
	DeviceID    string   `json:"deviceId,omitempty"`
	DeviceModel string   `json:"deviceModel,omitempty"`
	Locale      string   `json:"locale,omitempty"`
	Region      string   `json:"region,omitempty"`
	// 登录用户；0 表示未登录
	UserID int64 `json:"userId,omitempty"`
}

// ReleaseChangelogEntry 跨版本更新时，中间每个版本的说明。
type ReleaseChangelogEntry struct {
	Version     string     `json:"version"`
	VersionCode int64      `json:"versionCode"`
	Title       string     `json:"title,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Notes       string     `json:"notes"`
	UpdateType  string     `json:"updateType"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// ReleaseCheckResult 检测结果。没有更新时 HasUpdate=false，其余字段为空。
type ReleaseCheckResult struct {
	HasUpdate          bool  `json:"hasUpdate"`
	CurrentVersionCode int64 `json:"currentVersionCode"`
	// UpdateType 合并了跨越的全部版本：中间任何一个是强制更新，结果就是强制
	UpdateType string                  `json:"updateType,omitempty"`
	Release    *PublicRelease          `json:"release,omitempty"`
	Asset      *ReleaseAsset           `json:"asset,omitempty"`
	Changelog  []ReleaseChangelogEntry `json:"changelog,omitempty"`
	// Reason 强制更新的原因：release（版本本身强制）/ skipped（跨越的版本中有强制）/ unsupported（当前版本过旧）
	ForceReason string    `json:"forceReason,omitempty"`
	CheckedAt   time.Time `json:"checkedAt"`
	// NextCheckAfter 建议的下次自动检测间隔（秒）
	NextCheckAfter int `json:"nextCheckAfter"`
}

// ReleaseDecision 模拟检测时每个候选版本的判定过程。
type ReleaseDecision struct {
	ReleaseID   int64  `json:"releaseId"`
	Version     string `json:"version"`
	VersionCode int64  `json:"versionCode"`
	Eligible    bool   `json:"eligible"`
	Reason      string `json:"reason"`
	Bucket      int    `json:"bucket,omitempty"`
}

type ReleaseSimulation struct {
	Result    ReleaseCheckResult `json:"result"`
	Decisions []ReleaseDecision  `json:"decisions"`
}

// ReleaseEvent 客户端上报的漏斗事件。
const (
	ReleaseEventOffered    = "offered"
	ReleaseEventDownloaded = "downloaded"
	ReleaseEventInstalled  = "installed"
	ReleaseEventFailed     = "failed"
	ReleaseEventDismissed  = "dismissed"
)

var ValidReleaseEvents = map[string]struct{}{
	ReleaseEventDownloaded: {}, ReleaseEventInstalled: {}, ReleaseEventFailed: {}, ReleaseEventDismissed: {},
}

type ReleaseFunnel struct {
	Offered    int64 `json:"offered"`
	Downloaded int64 `json:"downloaded"`
	Installed  int64 `json:"installed"`
	Failed     int64 `json:"failed"`
	Dismissed  int64 `json:"dismissed"`
}

type ReleaseDailyStat struct {
	Day string `json:"day"`
	ReleaseFunnel
}

type ReleaseStats struct {
	ReleaseID int64              `json:"releaseId"`
	Total     ReleaseFunnel      `json:"total"`
	Daily     []ReleaseDailyStat `json:"daily"`
}

// ReleaseOverview 发布中心总览。
type ReleaseOverview struct {
	Total      int64 `json:"total"`
	Draft      int64 `json:"draft"`
	Scheduled  int64 `json:"scheduled"`
	Live       int64 `json:"live"`
	Paused     int64 `json:"paused"`
	Revoked    int64 `json:"revoked"`
	RollingOut int64 `json:"rollingOut"` // 下发中且未全量
	// Latest 每个渠道（含不限渠道）当前最新的下发版本
	Latest  []Release     `json:"latest"`
	Last14d ReleaseFunnel `json:"last14d"`
}

// ReleaseChannelView 客户端可见的渠道（自助加入用）。
type ReleaseChannelView struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Level       string `json:"level"`
	Color       string `json:"color,omitempty"`
	IsDefault   bool   `json:"isDefault"`
	SelfJoin    bool   `json:"selfJoin"`
	Joined      bool   `json:"joined"`
}
