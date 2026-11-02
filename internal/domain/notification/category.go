package notification

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// 用户消息中心的分类目录、跳转动作与投递入参。规则见 docs/notification-center.md。
//
// 这里是分类的**单一事实源**：迁移 000108 的回填、写入时推分类、列表筛选、
// 偏好开关、控制台的分类下拉都以本文件为准。改了映射要同步迁移里的回填（只影响历史数据）。

// 分类 key。
const (
	CategorySystem       = "system"
	CategoryAnnouncement = "announcement"
	CategorySecurity     = "security"
	CategoryMembership   = "membership"
	CategoryReward       = "reward"
	CategoryTicket       = "ticket"
)

// CategorySpec 一个分类的自述。
type CategorySpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Mutable 推送提醒能否关闭；账号安全类不能关
	Mutable bool `json:"mutable"`
}

// categories 顺序即客户端展示顺序。
var categories = []CategorySpec{
	{Key: CategorySystem, Label: "系统通知", Mutable: true},
	{Key: CategoryAnnouncement, Label: "公告活动", Mutable: true},
	{Key: CategorySecurity, Label: "账号安全", Mutable: false},
	{Key: CategoryMembership, Label: "会员与订单", Mutable: true},
	{Key: CategoryReward, Label: "积分与奖励", Mutable: true},
	{Key: CategoryTicket, Label: "工单与反馈", Mutable: true},
}

// 服务端写入点用到的 type。新增写入点时先在这里登记，
// TestNotificationWriterTypesAreCategorized 会扫描源码核对。
const (
	TypeSystem          = "system"
	TypeAnnouncement    = "announcement"
	TypeSecurity        = "security"
	TypeAccountDeletion = "account_deletion"
	TypeSignIn          = "signin"
	TypeAutoSign        = "auto_sign"
	TypePointsStore     = "points_store"
	TypeInvite          = "invite"
	TypeLeaderboard     = "leaderboard"
	TypeLottery         = "lottery"
	TypeCardKey         = "cardkey"
	TypeTicket          = "ticket"
	TypeItemGrant       = "item_grant"
	TypeLevel           = "level"
)

// typeCategories 旧 type → 分类。没登记的 type 一律落 system。
var typeCategories = map[string]string{
	"system": CategorySystem,

	"announcement": CategoryAnnouncement,
	"campaign":     CategoryAnnouncement,
	"marketing":    CategoryAnnouncement,

	"security":       CategorySecurity,
	"geo_login":      CategorySecurity, // 异地登录
	"password_reset": CategorySecurity,
	"two_factor":     CategorySecurity,
	"passkey":        CategorySecurity,

	"vip":              CategoryMembership,
	"payment":          CategoryMembership,
	"order":            CategoryMembership,
	"wallet":           CategoryMembership,
	"refund":           CategoryMembership,
	"account_deletion": CategoryMembership,

	"signin":       CategoryReward,
	"auto_sign":    CategoryReward,
	"points_store": CategoryReward,
	"invite":       CategoryReward,
	"leaderboard":  CategoryReward,
	"lottery":      CategoryReward,
	"rewarded_ad":  CategoryReward,
	"cardkey":      CategoryReward,
	"item_grant":   CategoryReward,
	"level":        CategoryReward,

	"ticket":   CategoryTicket,
	"feedback": CategoryTicket,
}

// Categories 全部分类（副本），顺序即展示顺序。
func Categories() []CategorySpec {
	out := make([]CategorySpec, len(categories))
	copy(out, categories)
	return out
}

// FindCategory 按 key 找分类。
func FindCategory(key string) (CategorySpec, bool) {
	for _, spec := range categories {
		if spec.Key == key {
			return spec, true
		}
	}
	return CategorySpec{}, false
}

// ValidCategory key 是否在目录里。
func ValidCategory(key string) bool {
	_, ok := FindCategory(key)
	return ok
}

// CategoryLabel 分类的中文名；未知分类回落「系统通知」。
func CategoryLabel(key string) string {
	if spec, ok := FindCategory(key); ok {
		return spec.Label
	}
	return categories[0].Label
}

// CategoryOf 由 type 推出分类；没登记的 type 落 system。
func CategoryOf(notificationType string) string {
	if category, ok := typeCategories[strings.ToLower(strings.TrimSpace(notificationType))]; ok {
		return category
	}
	return CategorySystem
}

// TypeMapped type 是否在映射表里显式登记过（含显式落 system 的 "system"）。
func TypeMapped(notificationType string) bool {
	_, ok := typeCategories[strings.ToLower(strings.TrimSpace(notificationType))]
	return ok
}

// TypeCategories 映射表副本（文档与测试用）。
func TypeCategories() map[string]string {
	out := make(map[string]string, len(typeCategories))
	for key, value := range typeCategories {
		out[key] = value
	}
	return out
}

// PushMutable 该分类的推送提醒能否关闭。未知分类按可关处理（不会出现在偏好表里）。
func PushMutable(category string) bool {
	if spec, ok := FindCategory(category); ok {
		return spec.Mutable
	}
	return true
}

// PushEnabled 按偏好判定是否提醒：不可关的分类永远提醒；没有偏好行即开启。
func PushEnabled(category string, prefs map[string]bool) bool {
	if !PushMutable(category) {
		return true
	}
	if enabled, ok := prefs[category]; ok {
		return enabled
	}
	return true
}

// ── 级别 ──

const (
	LevelInfo    = "info"
	LevelSuccess = "success"
	LevelWarning = "warning"
	LevelError   = "error"
)

// NormalizeLevel 统一级别：空为 info；旧写法 critical / danger 归为 error；其它未知值返回 false。
func NormalizeLevel(level string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", LevelInfo:
		return LevelInfo, true
	case LevelSuccess:
		return LevelSuccess, true
	case LevelWarning, "warn":
		return LevelWarning, true
	case LevelError, "critical", "danger":
		return LevelError, true
	default:
		return "", false
	}
}

// ── 跳转动作 ──

const (
	ActionRoute = "route"
	ActionURL   = "url"
)

// 动作约束。
const (
	ActionLabelMaxRunes  = 16
	ActionURLMaxLength   = 2048
	ImageURLMaxLength    = 2048
	MaxBatchIDs          = 200
	TitleMaxRunes        = 200
	ExpiresAtMaxDuration = 366 * 24 * time.Hour
)

// Action 消息的跳转动作。
type Action struct {
	// Type route（App 内页面，Target 取路由键）| url（Target 为 https 地址）
	Type   string `json:"type"`
	Target string `json:"target"`
	// Label 按钮文案，可空（客户端用默认文案）
	Label string `json:"label,omitempty"`
}

// RouteSpec App 内页面的路由键。
type RouteSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// 路由键。
const (
	RoutePoints      = "points"
	RoutePointsStore = "points_store"
	RouteLeaderboard = "leaderboard"
	RouteLottery     = "lottery"
	RouteCardKeys    = "card_keys"
	RouteSignIn      = "signin"
	RouteMembership  = "membership"
	RouteWallet      = "wallet"
	RouteSecurity    = "security"
	RouteInvite      = "invite"
	RouteTickets     = "tickets"
	RouteProfile     = "profile"
	RouteLevel       = "level"
	RoutePromo       = "promo"
)

var routes = []RouteSpec{
	{Key: RoutePoints, Label: "积分中心"},
	{Key: RoutePointsStore, Label: "积分商城"},
	{Key: RouteLeaderboard, Label: "排行榜"},
	{Key: RouteLottery, Label: "幸运抽奖"},
	{Key: RouteCardKeys, Label: "卡密兑换"},
	{Key: RouteSignIn, Label: "签到"},
	{Key: RouteMembership, Label: "会员"},
	{Key: RouteWallet, Label: "钱包"},
	{Key: RouteSecurity, Label: "账号安全"},
	{Key: RouteInvite, Label: "邀请"},
	{Key: RouteTickets, Label: "工单"},
	{Key: RouteProfile, Label: "个人资料"},
	{Key: RouteLevel, Label: "我的等级"},
	{Key: RoutePromo, Label: "推广活动"},
}

// Routes 全部路由键（副本）。
func Routes() []RouteSpec {
	out := make([]RouteSpec, len(routes))
	copy(out, routes)
	return out
}

// ValidRoute 路由键是否在白名单里。
func ValidRoute(key string) bool {
	for _, spec := range routes {
		if spec.Key == key {
			return true
		}
	}
	return false
}

// RouteAction 服务端写入点用的便捷构造。
func RouteAction(route, label string) *Action {
	return &Action{Type: ActionRoute, Target: route, Label: label}
}

// NormalizeAction 校验并整理动作；nil 或 type 为空视为没有动作。返回的错误文案可直接给调用方。
func NormalizeAction(action *Action) (*Action, string) {
	if action == nil {
		return nil, ""
	}
	out := Action{
		Type:   strings.ToLower(strings.TrimSpace(action.Type)),
		Target: strings.TrimSpace(action.Target),
		Label:  strings.TrimSpace(action.Label),
	}
	if out.Type == "" && out.Target == "" {
		return nil, ""
	}
	if utf8.RuneCountInString(out.Label) > ActionLabelMaxRunes {
		return nil, "按钮文案不能超过 16 个字"
	}
	switch out.Type {
	case ActionRoute:
		if !ValidRoute(out.Target) {
			return nil, "不支持的应用内页面：" + out.Target
		}
	case ActionURL:
		if msg := ValidateHTTPSURL(out.Target, ActionURLMaxLength); msg != "" {
			return nil, "跳转链接" + msg
		}
	default:
		return nil, "动作类型只能是 route 或 url"
	}
	return &out, ""
}

// ValidateHTTPSURL 校验 https 绝对地址；合法返回空串，否则返回问题描述（不含主语）。
func ValidateHTTPSURL(raw string, maxLength int) string {
	if raw == "" {
		return "不能为空"
	}
	if len(raw) > maxLength {
		return "过长"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "必须是 https 地址"
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return "不能包含空白字符"
	}
	return ""
}
