package notification

import (
	"fmt"
	"strings"
	"time"

	devicedomain "aegis/internal/domain/device"
)

// 站内信群发任务（docs/notification-center.md「群发任务」）。
//
// 管理员群发不在请求里逐人投递：发起即落一个任务，后台按用户 ID 升序分批写入，
// 每批一条语句写消息与回执、同一事务推进游标。这里是任务与目标人群的形状和校验，全是纯函数。

// 任务状态。
const (
	CampaignScheduled = "scheduled"
	CampaignQueued    = "queued"
	CampaignRunning   = "running"
	CampaignSucceeded = "succeeded"
	CampaignFailed    = "failed"
	CampaignCancelled = "cancelled"
	CampaignRecalling = "recalling"
	CampaignRecalled  = "recalled"
)

// CampaignStatusLabel 状态中文名。
func CampaignStatusLabel(status string) string {
	switch status {
	case CampaignScheduled:
		return "定时"
	case CampaignQueued:
		return "排队中"
	case CampaignRunning:
		return "发送中"
	case CampaignSucceeded:
		return "已完成"
	case CampaignFailed:
		return "失败"
	case CampaignCancelled:
		return "已取消"
	case CampaignRecalling:
		return "撤回中"
	case CampaignRecalled:
		return "已撤回"
	}
	return status
}

// ValidCampaignStatus 列表筛选用。
func ValidCampaignStatus(status string) bool {
	switch status {
	case CampaignScheduled, CampaignQueued, CampaignRunning, CampaignSucceeded, CampaignFailed,
		CampaignCancelled, CampaignRecalling, CampaignRecalled:
		return true
	}
	return false
}

// 人群范围。
const (
	// AudienceAll 全站：应用下全部用户（仍受账号状态与排除名单约束）
	AudienceAll = "all"
	// AudienceUsers 指定用户
	AudienceUsers = "users"
	// AudienceFilter 按条件筛选
	AudienceFilter = "filter"
)

// 账号状态。
const (
	// AccountAvailable 可用账号：已启用、不在禁用期、没有生效中的封禁（默认）
	AccountAvailable = "available"
	// AccountAll 不论状态
	AccountAll = "all"
	// AccountDisabled 只发给被禁用的账号
	AccountDisabled = "disabled"
)

// 会员身份。
const (
	MembershipAny         = ""
	MembershipMember      = "member"
	MembershipNonMember   = "non_member"
	MembershipLifetime    = "lifetime"
	MembershipNonLifetime = "non_lifetime"
)

// 上限。
const (
	// MaxAudienceUserIDs 指定用户 / 排除名单最多多少人
	MaxAudienceUserIDs = 10000
	// MaxAudienceDays 天数类条件的上限
	MaxAudienceDays = 3650
	// MaxAudienceLevel 等级条件的上限
	MaxAudienceLevel = 1000
	// MaxCampaignSchedule 定时最远多久之后
	MaxCampaignSchedule = 90 * 24 * time.Hour
	// MaxCampaignRequestID 幂等键长度
	MaxCampaignRequestID = 64
)

// Audience 目标人群。全站与指定用户只看账号状态与排除名单；按条件筛选时其余条件同时满足（AND）。
type Audience struct {
	Scope string `json:"scope"`
	// UserIDs 指定用户（管理端用户 ID），scope=users 时必填
	UserIDs []int64 `json:"userIds,omitempty"`
	// ExcludeUserIDs 排除这些人（任何范围都生效）
	ExcludeUserIDs []int64 `json:"excludeUserIds,omitempty"`
	// Account 账号状态：available（默认）| all | disabled
	Account string `json:"account,omitempty"`
	// Keyword 账号、昵称、邮箱、手机号模糊匹配
	Keyword string `json:"keyword,omitempty"`
	// Membership 会员身份：空不限 | member | non_member | lifetime | non_lifetime
	Membership string `json:"membership,omitempty"`
	// RegisteredWithinDays 注册不超过 N 天（新用户）
	RegisteredWithinDays int `json:"registeredWithinDays,omitempty"`
	// RegisteredBeforeDays 注册满 N 天（老用户）
	RegisteredBeforeDays int `json:"registeredBeforeDays,omitempty"`
	// ActiveWithinDays 最近 N 天内登录或在线过（活跃用户）
	ActiveWithinDays int `json:"activeWithinDays,omitempty"`
	// InactiveDays 超过 N 天没登录也没在线过（沉默用户，召回）
	InactiveDays int `json:"inactiveDays,omitempty"`
	MinLevel     int `json:"minLevel,omitempty"`
	MaxLevel     int `json:"maxLevel,omitempty"`
	// Platforms 用过这些平台的设备登录（android / ios / harmonyos / windows / macos / linux / web）
	Platforms []string `json:"platforms,omitempty"`
	// Limit 最多发给多少人（按用户 ID 升序取前 N 个），0 不限
	Limit int64 `json:"limit,omitempty"`
}

// AudiencePlatforms 平台条件的取值（与 device.NormalizePlatform 一致）。
var AudiencePlatforms = []AudienceOption{
	{Value: devicedomain.PlatformAndroid, Label: "Android"},
	{Value: devicedomain.PlatformIOS, Label: "iOS"},
	{Value: "harmonyos", Label: "鸿蒙"},
	{Value: "windows", Label: "Windows"},
	{Value: "macos", Label: "macOS"},
	{Value: "linux", Label: "Linux"},
	{Value: "web", Label: "网页"},
}

// AudienceOption 下拉选项。
type AudienceOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AudiencePreset 预设人群：控制台一键选中。
type AudiencePreset struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Audience    Audience `json:"audience"`
}

// AudiencePresets 预设人群目录（顺序即展示顺序）。
func AudiencePresets() []AudiencePreset {
	return []AudiencePreset{
		{Key: "all", Label: "全站用户", Description: "应用下全部可用账号", Audience: Audience{Scope: AudienceAll}},
		{Key: "members", Label: "会员", Description: "当前有效的会员（含永久）", Audience: Audience{Scope: AudienceFilter, Membership: MembershipMember}},
		{Key: "non_members", Label: "非会员", Description: "当前不是会员的用户", Audience: Audience{Scope: AudienceFilter, Membership: MembershipNonMember}},
		{Key: "lifetime", Label: "永久会员", Description: "持有永久会员", Audience: Audience{Scope: AudienceFilter, Membership: MembershipLifetime}},
		{Key: "new_users", Label: "新用户", Description: "注册 7 天内", Audience: Audience{Scope: AudienceFilter, RegisteredWithinDays: 7}},
		{Key: "active", Label: "活跃用户", Description: "最近 7 天登录或在线过", Audience: Audience{Scope: AudienceFilter, ActiveWithinDays: 7}},
		{Key: "dormant", Label: "沉默用户", Description: "超过 30 天没有登录", Audience: Audience{Scope: AudienceFilter, InactiveDays: 30}},
		{Key: "android", Label: "Android 用户", Description: "用 Android 设备登录过", Audience: Audience{Scope: AudienceFilter, Platforms: []string{devicedomain.PlatformAndroid}}},
		{Key: "ios", Label: "iOS 用户", Description: "用 iOS 设备登录过", Audience: Audience{Scope: AudienceFilter, Platforms: []string{devicedomain.PlatformIOS}}},
	}
}

// NormalizeAudience 校验并归一目标人群：去重 ID、补默认值、清掉与范围无关的条件。返回的错误信息直接给管理员看。
func NormalizeAudience(in Audience) (Audience, string) {
	out := Audience{
		Scope:          strings.ToLower(strings.TrimSpace(in.Scope)),
		ExcludeUserIDs: dedupePositive(in.ExcludeUserIDs),
		Account:        strings.ToLower(strings.TrimSpace(in.Account)),
		Limit:          in.Limit,
	}
	if out.Scope == "" {
		switch {
		case len(in.UserIDs) > 0:
			out.Scope = AudienceUsers
		default:
			out.Scope = AudienceAll
		}
	}
	switch out.Account {
	case "":
		out.Account = AccountAvailable
	case AccountAvailable, AccountAll, AccountDisabled:
	default:
		return out, "账号状态只能是 available、all 或 disabled"
	}
	if len(out.ExcludeUserIDs) > MaxAudienceUserIDs {
		return out, fmt.Sprintf("排除名单最多 %d 人", MaxAudienceUserIDs)
	}
	if out.Limit < 0 {
		return out, "人数上限不能为负数"
	}
	switch out.Scope {
	case AudienceAll:
		return out, ""
	case AudienceUsers:
		out.UserIDs = dedupePositive(in.UserIDs)
		if len(out.UserIDs) == 0 {
			return out, "指定用户不能为空"
		}
		if len(out.UserIDs) > MaxAudienceUserIDs {
			return out, fmt.Sprintf("指定用户最多 %d 人，更多请改用条件筛选", MaxAudienceUserIDs)
		}
		return out, ""
	case AudienceFilter:
	default:
		return out, "人群范围只能是 all、users 或 filter"
	}

	out.Keyword = strings.TrimSpace(in.Keyword)
	if len([]rune(out.Keyword)) > 64 {
		return out, "关键词不能超过 64 个字"
	}
	out.Membership = strings.ToLower(strings.TrimSpace(in.Membership))
	switch out.Membership {
	case MembershipAny, MembershipMember, MembershipNonMember, MembershipLifetime, MembershipNonLifetime:
	case "all", "any":
		out.Membership = MembershipAny
	default:
		return out, "会员身份只能是 member、non_member、lifetime 或 non_lifetime"
	}
	for _, days := range []int{in.RegisteredWithinDays, in.RegisteredBeforeDays, in.ActiveWithinDays, in.InactiveDays} {
		if days < 0 || days > MaxAudienceDays {
			return out, fmt.Sprintf("天数只能在 0 到 %d 之间", MaxAudienceDays)
		}
	}
	out.RegisteredWithinDays, out.RegisteredBeforeDays = in.RegisteredWithinDays, in.RegisteredBeforeDays
	out.ActiveWithinDays, out.InactiveDays = in.ActiveWithinDays, in.InactiveDays
	if out.RegisteredWithinDays > 0 && out.RegisteredBeforeDays > 0 && out.RegisteredBeforeDays > out.RegisteredWithinDays {
		return out, "注册天数的范围是空的：注册满的天数大于注册不超过的天数"
	}
	if out.ActiveWithinDays > 0 && out.InactiveDays > 0 {
		return out, "活跃与沉默条件不能同时设置"
	}
	if in.MinLevel < 0 || in.MaxLevel < 0 || in.MinLevel > MaxAudienceLevel || in.MaxLevel > MaxAudienceLevel {
		return out, fmt.Sprintf("等级只能在 0 到 %d 之间", MaxAudienceLevel)
	}
	if in.MinLevel > 0 && in.MaxLevel > 0 && in.MinLevel > in.MaxLevel {
		return out, "最低等级不能高于最高等级"
	}
	out.MinLevel, out.MaxLevel = in.MinLevel, in.MaxLevel
	seen := map[string]bool{}
	for _, raw := range in.Platforms {
		platform := devicedomain.NormalizePlatform(raw)
		if platform == "" {
			return out, "不认识的平台：" + strings.TrimSpace(raw)
		}
		if !seen[platform] {
			seen[platform] = true
			out.Platforms = append(out.Platforms, platform)
		}
	}
	return out, ""
}

// AudienceLabel 人群的中文摘要（列表上展示）。
func AudienceLabel(a Audience) string {
	parts := []string{}
	switch a.Scope {
	case AudienceAll:
		parts = append(parts, "全站用户")
	case AudienceUsers:
		parts = append(parts, fmt.Sprintf("指定 %d 人", len(a.UserIDs)))
	default:
		parts = append(parts, "条件筛选")
	}
	switch a.Membership {
	case MembershipMember:
		parts = append(parts, "会员")
	case MembershipNonMember:
		parts = append(parts, "非会员")
	case MembershipLifetime:
		parts = append(parts, "永久会员")
	case MembershipNonLifetime:
		parts = append(parts, "非永久会员")
	}
	if a.RegisteredWithinDays > 0 {
		parts = append(parts, fmt.Sprintf("注册 %d 天内", a.RegisteredWithinDays))
	}
	if a.RegisteredBeforeDays > 0 {
		parts = append(parts, fmt.Sprintf("注册满 %d 天", a.RegisteredBeforeDays))
	}
	if a.ActiveWithinDays > 0 {
		parts = append(parts, fmt.Sprintf("%d 天内活跃", a.ActiveWithinDays))
	}
	if a.InactiveDays > 0 {
		parts = append(parts, fmt.Sprintf("%d 天未活跃", a.InactiveDays))
	}
	switch {
	case a.MinLevel > 0 && a.MaxLevel > 0:
		parts = append(parts, fmt.Sprintf("等级 %d–%d", a.MinLevel, a.MaxLevel))
	case a.MinLevel > 0:
		parts = append(parts, fmt.Sprintf("等级 ≥ %d", a.MinLevel))
	case a.MaxLevel > 0:
		parts = append(parts, fmt.Sprintf("等级 ≤ %d", a.MaxLevel))
	}
	if len(a.Platforms) > 0 {
		names := make([]string, 0, len(a.Platforms))
		for _, platform := range a.Platforms {
			names = append(names, platformLabel(platform))
		}
		parts = append(parts, strings.Join(names, "、"))
	}
	if a.Keyword != "" {
		parts = append(parts, "关键词「"+a.Keyword+"」")
	}
	switch a.Account {
	case AccountAll:
		parts = append(parts, "含禁用账号")
	case AccountDisabled:
		parts = append(parts, "仅禁用账号")
	}
	if len(a.ExcludeUserIDs) > 0 {
		parts = append(parts, fmt.Sprintf("排除 %d 人", len(a.ExcludeUserIDs)))
	}
	if a.Limit > 0 {
		parts = append(parts, fmt.Sprintf("最多 %d 人", a.Limit))
	}
	label := strings.Join(parts, "，")
	if runes := []rune(label); len(runes) > 250 {
		label = string(runes[:250]) + "…"
	}
	return label
}

func platformLabel(platform string) string {
	for _, option := range AudiencePlatforms {
		if option.Value == platform {
			return option.Label
		}
	}
	return platform
}

func dedupePositive(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// Campaign 一个群发任务。
type Campaign struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	RequestID     *string        `json:"requestId"`
	Type          string         `json:"type"`
	Category      string         `json:"category"`
	CategoryLabel string         `json:"categoryLabel"`
	Title         string         `json:"title"`
	Content       string         `json:"content"`
	Level         string         `json:"level"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Action        *Action        `json:"action"`
	Image         *string        `json:"image"`
	ExpiresAt     *time.Time     `json:"expiresAt"`
	Audience      Audience       `json:"audience"`
	AudienceLabel string         `json:"audienceLabel"`
	Status        string         `json:"status"`
	StatusLabel   string         `json:"statusLabel"`
	ScheduledAt   *time.Time     `json:"scheduledAt"`
	// Total 开始发送时人群的人数（估计值），开始前为 null
	Total     *int64 `json:"total"`
	MaxUserID *int64 `json:"-"`
	Cursor    int64  `json:"-"`
	Processed int64  `json:"processed"`
	Delivered int64  `json:"delivered"`
	Skipped   int64  `json:"skipped"`
	Recalled  int64  `json:"recalled"`
	// ReadCount 已读人数：只有详情接口现算，列表上为 null
	ReadCount   *int64     `json:"readCount"`
	Attempts    int        `json:"attempts"`
	Message     string     `json:"message"`
	Operator    string     `json:"operator"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	RecalledAt  *time.Time `json:"recalledAt"`
	HeartbeatAt *time.Time `json:"heartbeatAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Finished 任务是否已经结束发送（不会再写入新消息）。
func (c Campaign) Finished() bool {
	switch c.Status {
	case CampaignSucceeded, CampaignFailed, CampaignCancelled, CampaignRecalling, CampaignRecalled:
		return true
	}
	return false
}

// CampaignInput 发起群发：内容（与 Deliver 同口径）+ 人群 + 可选定时与幂等键。
type CampaignInput struct {
	AppID       int64
	RequestID   string
	Type        string
	Category    string
	Title       string
	Content     string
	Level       string
	Metadata    map[string]any
	Action      *Action
	Image       string
	ExpiresAt   *time.Time
	Audience    Audience
	ScheduledAt *time.Time
	Operator    string
}

// CampaignQuery 任务列表。
type CampaignQuery struct {
	AppID  int64
	Status string
	Page   int
	Limit  int
}

// CampaignPage 任务列表。
type CampaignPage struct {
	Items      []Campaign `json:"items"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}

// AudienceSample 预览里的一个人。
type AudienceSample struct {
	UserID       int64      `json:"userId"`
	Account      string     `json:"account"`
	Nickname     string     `json:"nickname"`
	RegisteredAt time.Time  `json:"registeredAt"`
	Member       bool       `json:"member"`
	Level        int        `json:"level"`
	LastActiveAt *time.Time `json:"lastActiveAt"`
}

// AudiencePreview 人群预览：人数与抽样。
type AudiencePreview struct {
	Audience Audience         `json:"audience"`
	Label    string           `json:"label"`
	Total    int64            `json:"total"`
	Samples  []AudienceSample `json:"samples"`
}

// CampaignCatalog 控制台表单用的目录。
type CampaignCatalog struct {
	Presets     []AudiencePreset `json:"presets"`
	Platforms   []AudienceOption `json:"platforms"`
	Memberships []AudienceOption `json:"memberships"`
	Accounts    []AudienceOption `json:"accounts"`
	Categories  []CategorySpec   `json:"categories"`
	Statuses    []AudienceOption `json:"statuses"`
	Limits      map[string]int64 `json:"limits"`
}

// BuildCampaignCatalog 目录。
func BuildCampaignCatalog() CampaignCatalog {
	statuses := []AudienceOption{}
	for _, status := range []string{CampaignScheduled, CampaignQueued, CampaignRunning, CampaignSucceeded, CampaignFailed,
		CampaignCancelled, CampaignRecalling, CampaignRecalled} {
		statuses = append(statuses, AudienceOption{Value: status, Label: CampaignStatusLabel(status)})
	}
	return CampaignCatalog{
		Presets:   AudiencePresets(),
		Platforms: AudiencePlatforms,
		Memberships: []AudienceOption{
			{Value: MembershipAny, Label: "不限"},
			{Value: MembershipMember, Label: "会员"},
			{Value: MembershipNonMember, Label: "非会员"},
			{Value: MembershipLifetime, Label: "永久会员"},
			{Value: MembershipNonLifetime, Label: "非永久会员"},
		},
		Accounts: []AudienceOption{
			{Value: AccountAvailable, Label: "可用账号"},
			{Value: AccountAll, Label: "全部账号（含禁用）"},
			{Value: AccountDisabled, Label: "仅禁用账号"},
		},
		Categories: Categories(),
		Statuses:   statuses,
		Limits: map[string]int64{
			"maxUserIds":       MaxAudienceUserIDs,
			"maxDays":          MaxAudienceDays,
			"maxLevel":         MaxAudienceLevel,
			"maxScheduleHours": int64(MaxCampaignSchedule / time.Hour),
		},
	}
}
