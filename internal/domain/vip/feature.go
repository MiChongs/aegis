package vip

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// 会员功能标识（feature tag）。
//
// 「是不是会员」只有一个维度，而接入方一旦有两档会员（基础版能导出、
// 高级版还能用 AI），就需要问一个更细的问题：**这个人能不能用这个功能**。
//
// 做成目录而不是让接入方随手传字符串，是为了让拼错有报错：
// `exprot` 这种笔误在自由字符串方案下表现为"校验永远返回 false"，
// 没有任何一处说得出为什么；有目录才能在校验入口回一句「这个标识没登记过」。

// FeatureTagPattern 功能标识的合法形状：小写字母开头，可含数字、点、下划线、连字符。
//
// 与远程函数名同一套约定 —— 这类标识会出现在配置、日志、URL 与各语言的常量名里，
// 放开大小写与空格只会让同一个功能在不同地方写成三种样子。
var FeatureTagPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,63}$`)

// Feature 一个功能标识。
type Feature struct {
	ID          int64     `json:"id"`
	AppID       int64     `json:"appid"`
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsActive    bool      `json:"isActive"`
	SortOrder   int       `json:"sortOrder"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FeatureMutation 功能标识的创建 / 更新（指针为空表示不变更）。
//
// Tag 是定位键，创建后不可改：它已经被写进接入方的代码、以及每一条历史开通记录的
// 功能快照里。要改名改 Name 就够了，改 Tag 等于让所有历史权益指向一个不存在的功能。
type FeatureMutation struct {
	AppID       int64
	Tag         string
	Name        *string
	Description *string
	IsActive    *bool
	SortOrder   *int
}

// NormalizeFeatureTags 清洗一组功能标识：去空白、转小写、去重、排序。
//
// 排序不是洁癖：这组值会落进套餐配置与开通快照，顺序不定会让两次保存产生
// 不同的数组，diff 与幂等判断都跟着失真。
func NormalizeFeatureTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// FeatureVerdict 针对某一个功能标识的判定结论。
type FeatureVerdict struct {
	Tag string `json:"tag"`
	// Name 目录里的展示名，便于调用方直接把结论说给用户听
	Name string `json:"name,omitempty"`
	// Granted 当前会员权益是否覆盖这个功能
	Granted bool `json:"granted"`
}

// MembershipView 会员判定的紧凑投影，服务端校验接口用它作答。
//
// 不直接回整个 Entitlement：那里面还有试用资格（能不能领、为什么不能），
// 那是**客户端**要的东西 —— 服务端校验问的是"他现在有没有这个权益"，
// 多给的字段只会让调用方不知道该看哪一个。
type MembershipView struct {
	IsVIP            bool       `json:"isVip"`
	IsTrial          bool       `json:"isTrial"`
	Source           string     `json:"source"`
	PlanName         string     `json:"planName,omitempty"`
	ExpireAt         *time.Time `json:"expireAt,omitempty"`
	RemainingSeconds int64      `json:"remainingSeconds"`
	RemainingDays    int        `json:"remainingDays"`
	Features         []string   `json:"features"`
}

// View 把完整判定投影成校验用的紧凑结构。
//
// 投影而不是各拼各的：多一处组装就多一处会与判定漂移的地方。
func (e Entitlement) View() MembershipView {
	features := e.Features
	if features == nil {
		features = []string{}
	}
	return MembershipView{
		IsVIP:            e.IsVIP,
		IsTrial:          e.IsTrial,
		Source:           e.Source,
		PlanName:         e.PlanName,
		ExpireAt:         e.ExpireAt,
		RemainingSeconds: e.RemainingSeconds,
		RemainingDays:    e.RemainingDays,
		Features:         features,
	}
}

// HasFeature 当前会员权益是否覆盖某个功能标识。
//
// 不是会员就一律没有：过期用户的功能快照仍留在账本里，
// 只按标签命中会让一个到期三个月的用户继续用着高级功能。
func (e Entitlement) HasFeature(tag string) bool {
	if !e.IsVIP {
		return false
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return false
	}
	for _, item := range e.Features {
		if item == tag {
			return true
		}
	}
	return false
}

// VerifyQuery 一次服务端校验的输入。
//
// **用户身份只能来自访问令牌**，不接受调用方直接指定 userId ——
// 这不是多此一举，而是这套接口唯一守得住的边界：
//
//	接入方的后端几乎一定会把「当前请求是谁」交给它自己的客户端来说。
//	一旦这里收 userId，那条链路就是：客户端自报 42 → 接入方转发 42 →
//	我们如实回答"42 是会员" → 接入方放行**发起请求的那个人**。
//	攻击者只要知道任意一个会员的 userId 就能白嫖，而服务端密钥拦不住这件事
//	（犯错的正是持有密钥的那一方）。
//
// 令牌则是平台自己签发、自己验的：它同时证明了「是谁」和「这个人现在在场」。
// 需要按 userId 批量查（对账、到期提醒、客服工单）走管理端
// `/api/admin/apps/{appKey}/vip/entitlement`，那条路有管理员鉴权与审计。
type VerifyQuery struct {
	// UserID 由传输层从**已验证的**访问令牌里解出，不来自请求体
	UserID int64
	// Feature 要校验的功能标识；留空即只校验"是不是会员"（通用档）
	Feature string
}

// Verification 服务端校验的结论。
type Verification struct {
	// Granted 最终结论，调用方只看这一个字段就能决定放不放行：
	// 未指定功能标识时等于 isVip；指定了则还要求权益覆盖该标识。
	Granted bool `json:"granted"`
	// Matched 是否定位到了这个用户。定位不到会直接报错而不是回 false，
	// 这个字段恒为 true，留着是为了让响应自解释（调用方不必查文档确认）。
	Matched    bool            `json:"matched"`
	UserID     int64           `json:"userId"`
	Account    string          `json:"account,omitempty"`
	Membership MembershipView  `json:"membership"`
	Feature    *FeatureVerdict `json:"feature,omitempty"`
	// CheckedAt 服务端做出这个判定的时刻。调用方要缓存结论时按它算 TTL ——
	// 用本地时间算会把两端时钟差直接变成权益的提前失效或延后失效。
	CheckedAt time.Time `json:"checkedAt"`
}
