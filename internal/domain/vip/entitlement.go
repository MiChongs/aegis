package vip

import "time"

// 会员权益判定。
//
// ── 为什么要有这个文件 ──
//
// 「这个用户是不是会员」此前是一行到处重写的表达式：`vip_expire_at != nil &&
// vip_expire_at.After(now)`。它散落在仓储、远程函数 SDK、CSV 导出三处，
// 每一处都只能回答"是/否"，回答不了任何后续问题 —— 凭什么是会员、还剩多久、
// 是不是试用、试用还能不能领。而客户端要显示什么按钮、要不要弹升级引导，
// 全部取决于后面这几个答案。
//
// 于是这里把判定收成**一个纯函数**：输入是一次查询就能取齐的事实，
// 输出是一份自洽的结论。纯函数意味着这套判定不需要数据库就能测 ——
// 边界情况（刚好到期、试用中途买了付费、领过但已过期）用表驱动跑一遍即可。

// 会员来源：当前这段会员期是被什么给出来的。
const (
	SourceNone = "none" // 不是会员
	// SourceUnknown 是会员，但账本里找不到对应流水。
	// 老系统迁移进来的用户就是这种：到期时间是直接写进 users 的。
	// 谎报成某个具体渠道比说"不知道"更糟 —— 那会让对账的人去找一笔不存在的钱。
	SourceUnknown      = "unknown"
	SourceTrial        = ChannelTrial
	SourceWallet       = ChannelWallet
	SourcePaymentOrder = ChannelPaymentOrder
	SourceAdminGrant   = ChannelAdminGrant
)

// 试用资格判据。客户端按 reason 分支，不要匹配 message —— 后者会随文案调整变化。
const (
	TrialReasonEligible       = "eligible"
	TrialReasonNotConfigured  = "not_configured"
	TrialReasonAlreadyClaimed = "already_claimed"
	TrialReasonMemberActive   = "member_active"
	TrialReasonDeviceClaimed  = "device_claimed"
	TrialReasonDeviceRequired = "device_required"
)

// trialReasonMessages 判据的中文说明。
//
// 资格判定与领取失败说的必须是同一句话：接口回一句「试用资格已使用」、
// 而状态里写着另一句「你已经领过了」，用户会以为这是两回事。
var trialReasonMessages = map[string]string{
	TrialReasonEligible:       "可以领取试用",
	TrialReasonNotConfigured:  "当前应用未开放试用",
	TrialReasonAlreadyClaimed: "试用资格已使用",
	TrialReasonMemberActive:   "当前已是会员，无需领取试用",
	TrialReasonDeviceClaimed:  "该设备已领取过试用",
	TrialReasonDeviceRequired: "领取试用需要携带设备标识",
}

// TrialReasonMessage 判据对应的中文说明，未知判据返回空串。
func TrialReasonMessage(reason string) string { return trialReasonMessages[reason] }

// TrialPlanRef 当前启用中的试用套餐（判定只需要这几项）。
type TrialPlanRef struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	DurationDays  int    `json:"durationDays"`
	BonusIntegral int64  `json:"bonusIntegral"`
	DeviceLimited bool   `json:"deviceLimited"`
	Description   string `json:"description,omitempty"`
}

// TrialRef 把套餐压成判定用的引用。
//
// 构造点只有这一个：服务层解析资格、仓储层在领取成功后组装结论，用的必须是同一份 ——
// 各拼各的会让「刚领完那一刻返回的 trialOffer」与「随后查状态拿到的」说法不一致。
func (p Plan) TrialRef() *TrialPlanRef {
	return &TrialPlanRef{
		ID:            p.ID,
		Name:          p.Name,
		DurationDays:  p.DurationDays,
		BonusIntegral: p.BonusIntegral,
		DeviceLimited: p.TrialDeviceLimited,
		Description:   p.Description,
	}
}

// TrialClaim 一条试用领取记录（资格账本）。
type TrialClaim struct {
	ID     int64  `json:"id"`
	AppID  int64  `json:"appid"`
	UserID int64  `json:"userId"`
	PlanID *int64 `json:"planId,omitempty"`
	// Account 仅管理端列表填充，用户侧为空
	Account       string    `json:"account,omitempty"`
	PlanName      string    `json:"planName"`
	DurationDays  int       `json:"durationDays"`
	TrialEndsAt   time.Time `json:"trialEndsAt"`
	TransactionNo string    `json:"transactionNo"`
	DeviceID      string    `json:"deviceId,omitempty"`
	DeviceLocked  bool      `json:"deviceLocked"`
	ClientIP      string    `json:"clientIp,omitempty"`
	Operator      string    `json:"operator,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	// Converted 领取之后是否发生过付费开通（仅管理端列表填充）。
	// 开试用的理由就是这一列，没有它试用记录只是一堆流水。
	Converted bool `json:"converted,omitempty"`
}

// TrialState 用户的试用历史与当前状态（领取过才有）。
type TrialState struct {
	Active           bool      `json:"active"` // 现在仍在这段试用期内
	ClaimedAt        time.Time `json:"claimedAt"`
	EndsAt           time.Time `json:"endsAt"`
	DurationDays     int       `json:"durationDays"`
	PlanID           *int64    `json:"planId,omitempty"`
	PlanName         string    `json:"planName"`
	RemainingSeconds int64     `json:"remainingSeconds"`
}

// TrialOffer 现在能不能领试用，以及为什么不能。
type TrialOffer struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	// 以下仅在应用配置了试用套餐时有值（不可领时也给，客户端要展示"试用 7 天"这类文案）
	PlanID        int64  `json:"planId,omitempty"`
	PlanName      string `json:"planName,omitempty"`
	DurationDays  int    `json:"durationDays,omitempty"`
	BonusIntegral int64  `json:"bonusIntegral,omitempty"`
	DeviceLimited bool   `json:"deviceLimited,omitempty"`
	Description   string `json:"description,omitempty"`
}

// Entitlement 一个用户在一个应用里的会员权益全貌。
//
// `isVip` / `expireAt` / `remainingDays` 三项与旧的 Status 逐字一致 ——
// 已经发出去的客户端读的就是这三个字段，换名字等于让它们全部瞎掉。
type Entitlement struct {
	IsVIP bool `json:"isVip"`
	// IsTrial 当前这段会员期是试用给的。
	// 「是会员」与「是试用会员」是两个问题，客户端两个都要问：
	// 前者决定功能开不开，后者决定引导用户续费还是升级。
	IsTrial          bool       `json:"isTrial"`
	Source           string     `json:"source"`
	PlanName         string     `json:"planName,omitempty"`
	ExpireAt         *time.Time `json:"expireAt,omitempty"`
	RemainingSeconds int64      `json:"remainingSeconds"`
	RemainingDays    int        `json:"remainingDays"`
	// Features 当前生效的功能标识（尚未到期的开通记录的并集，见 EvalInput.Features）。
	// 不是会员时恒为空数组 —— 过期用户的功能快照仍在账本里，但权益已经不在了。
	Features   []string    `json:"features"`
	Trial      *TrialState `json:"trial,omitempty"`
	TrialOffer TrialOffer  `json:"trialOffer"`
}

// EvalInput 判定所需的全部事实，由仓储一次查询取齐。
type EvalInput struct {
	ExpireAt *time.Time
	// LastChannel / LastPlanName 最近一条 vip_transactions 的渠道与套餐名
	LastChannel  string
	LastPlanName string
	// Claim 该用户在本应用的试用领取记录，没领过为 nil
	Claim *TrialClaim
	// TrialPlan 当前启用中的试用套餐，未配置为 nil
	TrialPlan *TrialPlanRef
	// DeviceClaimed 同一设备已被（任何账号）领取过；仅在开启设备去重时查询
	DeviceClaimed bool
	// DeviceMissing 开启了设备去重，但这次请求没带设备标识
	DeviceMissing bool
	// Features 尚未到期的开通记录携带的功能标识并集。
	//
	// 取并集而不是"最近一次开通的那份"：会员期是顺延的，先买基础版再买高级版时
	// 两段都还没到期，用户理所当然认为两边的功能现在都能用。已经用完的那几段
	// （expire_after 已过）自然落在集合之外，权益随时间自己收敛。
	Features []string
}

// Evaluate 判定会员权益。纯函数：同样的输入永远得到同样的结论。
func Evaluate(in EvalInput, now time.Time) Entitlement {
	entitlement := Entitlement{Source: SourceNone, ExpireAt: in.ExpireAt, Features: []string{}}

	if in.ExpireAt != nil && in.ExpireAt.After(now) {
		remaining := in.ExpireAt.Sub(now)
		entitlement.IsVIP = true
		entitlement.Features = NormalizeFeatureTags(in.Features)
		entitlement.RemainingSeconds = int64(remaining.Seconds())
		// 天数沿用旧口径（向下取整）：控制台与客户端上已有的"还剩 N 天"都是这么算的，
		// 精确到秒的需求由 remainingSeconds 满足。
		entitlement.RemainingDays = int(remaining.Hours() / 24)
		entitlement.PlanName = in.LastPlanName
		entitlement.Source = normalizeSource(in.LastChannel)
	}

	if in.Claim != nil {
		state := &TrialState{
			ClaimedAt:    in.Claim.CreatedAt,
			EndsAt:       in.Claim.TrialEndsAt,
			DurationDays: in.Claim.DurationDays,
			PlanID:       in.Claim.PlanID,
			PlanName:     in.Claim.PlanName,
			Active:       now.Before(in.Claim.TrialEndsAt),
		}
		if state.Active {
			state.RemainingSeconds = int64(in.Claim.TrialEndsAt.Sub(now).Seconds())
		}
		entitlement.Trial = state

		// 「当前这段会员期是不是试用给的」= 到期时间恰好就是试用发到的那一刻。
		// 用户后来买了付费，到期时间被推远，这里自然不再相等 ——
		// 不需要任何状态迁移，也不会出现"买了付费还显示试用中"。
		if entitlement.IsVIP && state.Active && in.ExpireAt.Equal(in.Claim.TrialEndsAt) {
			entitlement.IsTrial = true
			entitlement.Source = SourceTrial
			entitlement.PlanName = in.Claim.PlanName
		}
	}

	entitlement.TrialOffer = evaluateTrialOffer(in, entitlement)
	return entitlement
}

// evaluateTrialOffer 试用资格判定。判据顺序即优先级，靠前的更"根本"。
func evaluateTrialOffer(in EvalInput, entitlement Entitlement) TrialOffer {
	if in.TrialPlan == nil {
		return TrialOffer{Reason: TrialReasonNotConfigured, Message: TrialReasonMessage(TrialReasonNotConfigured)}
	}
	offer := TrialOffer{
		PlanID:        in.TrialPlan.ID,
		PlanName:      in.TrialPlan.Name,
		DurationDays:  in.TrialPlan.DurationDays,
		BonusIntegral: in.TrialPlan.BonusIntegral,
		DeviceLimited: in.TrialPlan.DeviceLimited,
		Description:   in.TrialPlan.Description,
	}
	switch {
	case in.Claim != nil:
		offer.Reason = TrialReasonAlreadyClaimed
	case entitlement.IsVIP:
		// 会员期内领试用只是把到期时间再往后推 —— 那不是试用，是白送。
		offer.Reason = TrialReasonMemberActive
	case in.TrialPlan.DeviceLimited && in.DeviceMissing:
		offer.Reason = TrialReasonDeviceRequired
	case in.TrialPlan.DeviceLimited && in.DeviceClaimed:
		offer.Reason = TrialReasonDeviceClaimed
	default:
		offer.Available = true
		offer.Reason = TrialReasonEligible
	}
	offer.Message = TrialReasonMessage(offer.Reason)
	return offer
}

// normalizeSource 把账本里的渠道翻成会员来源，未登记的渠道一律算"说不清"。
func normalizeSource(channel string) string {
	switch channel {
	case ChannelTrial, ChannelWallet, ChannelPaymentOrder, ChannelAdminGrant:
		return channel
	default:
		return SourceUnknown
	}
}

// TrialSummary 试用领取的汇总（管理端列表附带）。
type TrialSummary struct {
	Total     int64 `json:"total"`     // 累计领取人数
	Active    int64 `json:"active"`    // 仍在试用期内
	Converted int64 `json:"converted"` // 领取后发生过付费开通
}

// TrialClaimResult 领取试用的结果。
type TrialClaimResult struct {
	Claim       TrialClaim   `json:"claim"`
	Transaction *Transaction `json:"transaction,omitempty"`
	Entitlement Entitlement  `json:"entitlement"`
	// Replayed 本次没有真正发放，返回的是此前那次领取的结果。
	// 领取接口没有幂等键（它天然一人一次），网络重试落在这里。
	Replayed bool `json:"replayed,omitempty"`
}
