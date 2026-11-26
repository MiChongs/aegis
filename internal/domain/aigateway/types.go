// Package aigateway AI 中转网关：面向应用终端用户的 OpenAI 兼容调用面与它的计费。
//
// 计费的全部数值规则都在这个包里以纯函数出现（计价、倍率、按来源分摊、零头凑整），
// 仓储层在锁住用户行之后调用它们，服务层与控制台读同一份目录。规格见 docs/ai-gateway.md。
package aigateway

import (
	"time"

	"github.com/shopspring/decimal"
)

// MicroPerPoint 1 点 = 1,000,000 微点。点是网关的计价单位，对外叫「额度」。
// 2026-11-26 起 AI 额度是独立资产，与积分不互通（docs/ai-billing.md）。
const MicroPerPoint int64 = 1_000_000

// ── 扣费来源 ──
//
// 一次调用的费用按设置里 Sources 的顺序依次从这些来源里扣。
const (
	// SourceFreeDaily 每日免费额度：所有用户每天都有，按应用时区的日历日重置。
	SourceFreeDaily = "free_daily"
	// SourceVipDaily / SourceVipMonthly 会员额度：只有有效会员（含永久）可用。
	SourceVipDaily   = "vip_daily"
	SourceVipMonthly = "vip_monthly"
	// SourcePack 额度包：买来的、新人礼、管理员限期发放的额度批次，有到期时间，先到期的先用。
	SourcePack = "pack"
	// SourceCredit 永久额度：看激励广告、活动发放、调用退款进来，不过期。
	SourceCredit = "credit"
	// SourceIntegral 积分（已停用）：AI 额度与积分不再互通，设置里不能再选它。
	// 常量只为存量的暂扣、结算与退款按原规则走完。
	SourceIntegral = "integral"
	// SourceWallet 余额：按设置里的折算比例（每点多少元）扣，零头同样累计凑整。
	SourceWallet = "wallet"

	// SpendDaily 每日消费上限的用量键，与周期额度共用一张表，不是扣费来源。
	SpendDaily = "spend_daily"
)

// SourceMeta 一个扣费来源的自述，驱动控制台的排序与说明。
type SourceMeta struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// Periodic 按周期重置的额度；Period 为 day / month。
	Periodic bool   `json:"periodic"`
	Period   string `json:"period,omitempty"`
	VipOnly  bool   `json:"vipOnly,omitempty"`
}

var sourceCatalog = []SourceMeta{
	{Key: SourceFreeDaily, Label: "每日免费额度", Periodic: true, Period: "day",
		Hint: "所有用户每天可用的额度，按应用时区的日历日重置，当天用不完不累积"},
	{Key: SourceVipDaily, Label: "会员每日额度", Periodic: true, Period: "day", VipOnly: true,
		Hint: "有效会员（含永久会员）每天可用的额度"},
	{Key: SourceVipMonthly, Label: "会员每月额度", Periodic: true, Period: "month", VipOnly: true,
		Hint: "有效会员每个自然月可用的额度"},
	{Key: SourcePack, Label: "额度包",
		Hint: "用余额购买、新人礼与限期发放的额度，有效期内可用，先到期的先用"},
	{Key: SourceCredit, Label: "永久额度",
		Hint: "看激励广告、活动发放与调用退款得到的额度，不过期"},
	{Key: SourceWallet, Label: "余额直付",
		Hint: "额度用完后按「余额直付」的单价直接从钱包余额扣，不足 1 分的零头累计凑整"},
}

// legacySources 已停用、只在存量记录上出现的来源。
var legacySources = []SourceMeta{
	{Key: SourceIntegral, Label: "积分（已停用）", Hint: "AI 额度与积分已不互通，只出现在旧的调用记录里"},
}

// SourceLabel 来源的中文名（含已停用的来源）。
func SourceLabel(key string) string {
	if meta, ok := FindSource(key); ok {
		return meta.Label
	}
	for _, item := range legacySources {
		if item.Key == key {
			return item.Label
		}
	}
	return key
}

// Sources 扣费来源目录，顺序即默认的扣费顺序。
func Sources() []SourceMeta {
	out := make([]SourceMeta, len(sourceCatalog))
	copy(out, sourceCatalog)
	return out
}

// FindSource 按键取来源自述。
func FindSource(key string) (SourceMeta, bool) {
	for _, item := range sourceCatalog {
		if item.Key == key {
			return item, true
		}
	}
	return SourceMeta{}, false
}

// ── 模型种类与接口 ──

const (
	KindChat          = "chat"
	KindEmbedding     = "embedding"
	KindImage         = "image"
	KindSpeech        = "speech"
	KindTranscription = "transcription"
	KindRerank        = "rerank"
)

// 网关接口（记在调用记录的 endpoint 列）。
const (
	EndpointChat          = "chat.completions"
	EndpointMessages      = "messages"
	EndpointEmbeddings    = "embeddings"
	EndpointImageGenerate = "images.generations"
	EndpointImageEdit     = "images.edits"
	EndpointSpeech        = "audio.speech"
	EndpointTranscription = "audio.transcriptions"
	EndpointTranslation   = "audio.translations"
	EndpointRerank        = "rerank"
)

// KindMeta 模型种类自述：可用的接口与生效的价格项。
type KindMeta struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Endpoints []string `json:"endpoints"`
	// Prices 这一种类会用到的价格项（控制台只显示这些输入框）。
	Prices []string `json:"prices"`
}

// 价格项键。
const (
	PriceInput   = "inputPrice"
	PriceOutput  = "outputPrice"
	PriceRequest = "requestPrice"
	PriceImage   = "imagePrice"
	PriceChar    = "charPrice"
	PriceMinute  = "minutePrice"
)

var kindCatalog = []KindMeta{
	{Key: KindChat, Label: "对话", Endpoints: []string{EndpointChat, EndpointMessages},
		Prices: []string{PriceInput, PriceOutput, PriceRequest}},
	{Key: KindEmbedding, Label: "向量嵌入", Endpoints: []string{EndpointEmbeddings},
		Prices: []string{PriceInput, PriceRequest}},
	{Key: KindImage, Label: "图像生成", Endpoints: []string{EndpointImageGenerate, EndpointImageEdit},
		Prices: []string{PriceImage, PriceInput, PriceOutput, PriceRequest}},
	{Key: KindSpeech, Label: "语音合成", Endpoints: []string{EndpointSpeech},
		Prices: []string{PriceChar, PriceRequest}},
	{Key: KindTranscription, Label: "语音转写", Endpoints: []string{EndpointTranscription, EndpointTranslation},
		Prices: []string{PriceMinute, PriceInput, PriceOutput, PriceRequest}},
	{Key: KindRerank, Label: "重排序", Endpoints: []string{EndpointRerank},
		Prices: []string{PriceInput, PriceRequest}},
}

// Kinds 模型种类目录。
func Kinds() []KindMeta {
	out := make([]KindMeta, len(kindCatalog))
	copy(out, kindCatalog)
	return out
}

// ValidKind 种类是否登记过。
func ValidKind(kind string) bool {
	_, ok := findKind(kind)
	return ok
}

func findKind(kind string) (KindMeta, bool) {
	for _, item := range kindCatalog {
		if item.Key == kind {
			return item, true
		}
	}
	return KindMeta{}, false
}

// EndpointKind 某个接口要求的模型种类。
func EndpointKind(endpoint string) string {
	for _, item := range kindCatalog {
		for _, candidate := range item.Endpoints {
			if candidate == endpoint {
				return item.Key
			}
		}
	}
	return ""
}

// ── 设置 ──

// 使用对象。
const (
	AudienceAll = "all"
	AudienceVip = "vip"
)

// LevelMultiplier 等级倍率：用户等级 ≥ Level 时取这一档（取满足条件的最高档）。
type LevelMultiplier struct {
	Level      int             `json:"level"`
	Multiplier decimal.Decimal `json:"multiplier"`
}

// Settings 应用级网关设置。
type Settings struct {
	Enabled bool `json:"enabled"`
	// AllowSessionToken 允许直接用登录会话的访问令牌调用（App 内调用不必先领密钥）。
	AllowSessionToken bool   `json:"allowSessionToken"`
	Audience          string `json:"audience"`
	// Sources 启用的扣费来源，顺序即扣费顺序。
	Sources []string `json:"sources"`

	// 倍率：最终价格 = 模型单价 × PriceMultiplier × 会员倍率 × 等级倍率 × 时段倍率（见 Pricing）。
	PriceMultiplier decimal.Decimal `json:"priceMultiplier"`
	// VipMultiplier 等通用会员配置：是会员、但持有的档位都没在 MemberTiers 里配置时用它们。
	VipMultiplier    decimal.Decimal   `json:"vipMultiplier"`
	LevelMultipliers []LevelMultiplier `json:"levelMultipliers"`

	// 周期额度（点）。0 表示不提供。
	FreeDaily  decimal.Decimal `json:"freeDaily"`
	VipDaily   decimal.Decimal `json:"vipDaily"`
	VipMonthly decimal.Decimal `json:"vipMonthly"`
	// VipPackDiscount / VipPackBonusRate 通用会员买额度包的折扣（0.9 = 9 折）与多送比例（0.1 = 多送 10%）。
	VipPackDiscount  decimal.Decimal `json:"vipPackDiscount"`
	VipPackBonusRate decimal.Decimal `json:"vipPackBonusRate"`
	// MemberTiers 按会员档位的优惠，列表顺序即优先级（持有多个档位时取最靠前的）。
	MemberTiers []MemberTier `json:"memberTiers"`
	// TimePricing 时段价格。
	TimePricing TimePricing `json:"timePricing"`
	// Welcome 新人礼；Store 额度商店。
	Welcome WelcomeGift   `json:"welcome"`
	Store   StoreSettings `json:"store"`

	// WalletRate 余额直付：每 1 点（额度）扣多少元。
	WalletRate decimal.Decimal `json:"walletRate"`

	// 限制。DailySpendLimit 每人每天最多消费多少点（全部来源合计），0 不限。
	DailySpendLimit decimal.Decimal `json:"dailySpendLimit"`
	RPM             int             `json:"rpm"`
	MaxConcurrent   int             `json:"maxConcurrent"`
	MaxKeysPerUser  int             `json:"maxKeysPerUser"`
	// DefaultMaxTokens 请求没带 max_tokens 时按多少输出 token 估算暂扣。
	DefaultMaxTokens int `json:"defaultMaxTokens"`
	// Timezone 周期额度与每日上限按哪个时区切日。
	Timezone string `json:"timezone"`
	// RedactContent 调用追踪不留提示词与正文，只留参数与长度。
	RedactContent bool `json:"redactContent"`
	// CatalogAutoSync 每天从公开模型目录自动更新对话模型的能力声明（不动价格）。
	CatalogAutoSync bool `json:"catalogAutoSync"`

	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// DefaultSettings 默认设置：关闭；开启后先用免费与会员额度，再用额度包（先到期的先用）、永久额度，
// 最后按「余额直付」从钱包扣。
func DefaultSettings() Settings {
	return Settings{
		Enabled:           false,
		AllowSessionToken: true,
		Audience:          AudienceAll,
		Sources:           []string{SourceFreeDaily, SourceVipDaily, SourceVipMonthly, SourcePack, SourceCredit, SourceWallet},
		PriceMultiplier:   decimal.NewFromInt(1),
		VipMultiplier:     decimal.NewFromInt(1),
		LevelMultipliers:  []LevelMultiplier{},
		FreeDaily:         decimal.Zero,
		VipDaily:          decimal.Zero,
		VipMonthly:        decimal.Zero,
		VipPackDiscount:   decimal.NewFromInt(1),
		VipPackBonusRate:  decimal.Zero,
		MemberTiers:       []MemberTier{},
		TimePricing:       TimePricing{Stacking: StackMultiply, Windows: []TimeWindow{}},
		Welcome:           WelcomeGift{Amount: decimal.Zero, ValidityDays: 30},
		Store:             StoreSettings{Enabled: true},
		WalletRate:        decimal.RequireFromString("0.001"),
		DailySpendLimit:   decimal.Zero,
		RPM:               60,
		MaxConcurrent:     5,
		MaxKeysPerUser:    10,
		DefaultMaxTokens:  4096,
		Timezone:          "Asia/Shanghai",
	}
}

// HasSource 某个来源是否启用。
func (s Settings) HasSource(source string) bool {
	for _, item := range s.Sources {
		if item == source {
			return true
		}
	}
	return false
}

// Location 设置时区；无效时回落到 UTC（保存时已校验，这里只防存量脏数据）。
func (s Settings) Location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// QuotaMicroFor 周期额度对某位用户的上限（微点）：会员额度按他生效的会员档位取。
func (s Settings) QuotaMicroFor(source string, member MemberBenefit) int64 {
	switch source {
	case SourceVipDaily:
		if member.Source == MemberSourceNone {
			return 0
		}
		return PointsToMicroFloor(member.Daily)
	case SourceVipMonthly:
		if member.Source == MemberSourceNone {
			return 0
		}
		return PointsToMicroFloor(member.Monthly)
	}
	return s.QuotaMicro(source)
}

// QuotaMicro 周期额度的上限（微点），会员额度取通用会员配置。
func (s Settings) QuotaMicro(source string) int64 {
	switch source {
	case SourceFreeDaily:
		return PointsToMicroFloor(s.FreeDaily)
	case SourceVipDaily:
		return PointsToMicroFloor(s.VipDaily)
	case SourceVipMonthly:
		return PointsToMicroFloor(s.VipMonthly)
	case SpendDaily:
		return PointsToMicroFloor(s.DailySpendLimit)
	}
	return 0
}

// PeriodKey 周期额度在某一时刻所属的周期：日额度取日历日，月额度取年月。
func PeriodKey(source string, now time.Time, loc *time.Location) string {
	local := now.In(loc)
	if source == SourceVipMonthly {
		return local.Format("2006-01")
	}
	return local.Format("2006-01-02")
}

// Profile 计价时用到的用户事实。
type Profile struct {
	IsVip bool `json:"isVip"`
	Level int  `json:"level"`
	// TierIDs 此刻持有的会员档位（vip.Holdings.Tiers）；TierID / TierName 是计价生效的那个档位（没有为 0）。
	TierIDs  []int64 `json:"tierIds,omitempty"`
	TierID   int64   `json:"tierId,omitempty"`
	TierName string  `json:"tierName,omitempty"`
	// tierNames 档位名（不出网），Member 用它给没写 label 的档位取名。
	tierNames map[int64]string
}

// WithTierNames 记下档位名（来自会员判定的持有档位）。
func (p Profile) WithTierNames(names map[int64]string) Profile {
	p.tierNames = names
	return p
}

// levelFactor 等级倍率：取用户等级满足的最高一档。
func (s Settings) levelFactor(level int) decimal.Decimal {
	best := -1
	out := decimal.NewFromInt(1)
	for _, item := range s.LevelMultipliers {
		if level >= item.Level && item.Level > best {
			best = item.Level
			out = item.Multiplier
		}
	}
	return out
}

// Factor 该用户不含时段的价格倍率：全局 × 会员（按档位）× 等级。
func (s Settings) Factor(profile Profile) decimal.Decimal {
	member := s.Member(profile)
	return s.PriceMultiplier.Mul(member.Multiplier).Mul(s.levelFactor(profile.Level))
}

// ── 模型 ──

// Model 一个可调用的模型及其价格。
type Model struct {
	ID            int64  `json:"id"`
	AppID         int64  `json:"appid"`
	Model         string `json:"model"`
	UpstreamModel string `json:"upstreamModel"`
	Kind          string `json:"kind"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description"`
	ConfigID      int64  `json:"configId"`
	Enabled       bool   `json:"enabled"`
	VipOnly       bool   `json:"vipOnly"`
	Price
	MaxOutputTokens int `json:"maxOutputTokens"`
	// Capabilities 对话模型的能力声明（只对 chat 种类有意义）。
	Capabilities Capabilities `json:"capabilities"`
	SortOrder    int          `json:"sortOrder"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

// Upstream 发给上游的模型名。
func (m Model) Upstream() string {
	if m.UpstreamModel != "" {
		return m.UpstreamModel
	}
	return m.Model
}

// ModelInput 新建 / 修改模型的入参。
type ModelInput struct {
	Model           string                     `json:"model"`
	UpstreamModel   string                     `json:"upstreamModel"`
	Kind            string                     `json:"kind"`
	DisplayName     string                     `json:"displayName"`
	Description     string                     `json:"description"`
	ConfigID        int64                      `json:"configId"`
	Enabled         *bool                      `json:"enabled"`
	VipOnly         bool                       `json:"vipOnly"`
	Free            bool                       `json:"free"`
	InputPrice      decimal.Decimal            `json:"inputPrice"`
	OutputPrice     decimal.Decimal            `json:"outputPrice"`
	RequestPrice    decimal.Decimal            `json:"requestPrice"`
	ImagePrice      decimal.Decimal            `json:"imagePrice"`
	ImagePrices     map[string]decimal.Decimal `json:"imagePrices"`
	CharPrice       decimal.Decimal            `json:"charPrice"`
	MinutePrice     decimal.Decimal            `json:"minutePrice"`
	MaxOutputTokens int                        `json:"maxOutputTokens"`
	Capabilities    *Capabilities              `json:"capabilities"`
	SortOrder       int                        `json:"sortOrder"`
}

// Capabilities 对话模型能做什么。客户端据此决定发不发图片、带不带工具、给不给思考强度的选项；
// 网关不据此拒绝请求。
type Capabilities struct {
	Vision    bool `json:"vision"`
	Tools     bool `json:"tools"`
	Reasoning bool `json:"reasoning"`
	// ReasoningEfforts 可选的思考强度（none / minimal / low / medium / high / xhigh / max 的子集），
	// 空表示 low / medium / high。含 none 表示可以关闭思考。
	ReasoningEfforts []string `json:"reasoningEfforts"`
	// ContextLength 上下文长度（token），0 表示未知。
	ContextLength int `json:"contextLength"`
	// MaxOutput 模型本身的最大输出（token），0 表示未知。只是声明：与 Model.MaxOutputTokens 不同，
	// 后者是网关强加的上限，没带 max_tokens 的请求按它暂扣，把它设成 128K 会让每次调用都暂扣一大笔。
	MaxOutput int `json:"maxOutput"`
	// CatalogKey 最近一次从公开模型目录同步时对上的条目（供应商/模型），空表示没同步过。
	CatalogKey string `json:"catalogKey,omitempty"`
}

// DefaultCapabilities 没声明能力的模型：只开工具，与此前客户端的推断一致。
func DefaultCapabilities() Capabilities {
	return Capabilities{Tools: true, ReasoningEfforts: []string{}}
}

// UserModel 用户看到的模型：价格已乘上该用户的倍率。
type UserModel struct {
	Model       string `json:"model"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	VipOnly     bool   `json:"vipOnly"`
	// Available 当前用户能不能调（会员专享而用户不是会员时为 false）。
	Available bool `json:"available"`
	Price
	// BasePrice 不含时段倍率的单价；Factor 此刻的合计倍率；Window 此刻对这个模型生效的时段（没有为 null）。
	BasePrice       Price           `json:"basePrice"`
	Factor          decimal.Decimal `json:"factor"`
	Window          *TimeMatch      `json:"window"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	// Capabilities 只有对话模型带。
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}

// ── 密钥 ──

const (
	KeyActive   = "active"
	KeyDisabled = "disabled"
	KeyRevoked  = "revoked"
)

// Key 用户 API 密钥（不含明文）。
type Key struct {
	ID     int64    `json:"id"`
	AppID  int64    `json:"appid"`
	UserID int64    `json:"userId"`
	Name   string   `json:"name"`
	Hint   string   `json:"hint"`
	Status string   `json:"status"`
	Models []string `json:"models"`
	// 金额以点为单位出网（字符串，与钱包同一惯例），库里是微点。
	SpendLimit decimal.Decimal `json:"spendLimit"`
	Spent      decimal.Decimal `json:"spent"`
	ExpiresAt  *time.Time      `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time      `json:"lastUsedAt,omitempty"`
	LastUsedIP string          `json:"lastUsedIp,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`

	// 以下只在服务端使用。
	SpendLimitMicro int64 `json:"-"`
	SpentMicro      int64 `json:"-"`
	ReservedMicro   int64 `json:"-"`
	// Account 管理端列表带出持有人账号。
	Account string `json:"account,omitempty"`
}

// Usable 密钥此刻能否用于调用。
func (k Key) Usable(now time.Time) bool {
	if k.Status != KeyActive {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}

// AllowsModel 白名单为空表示不限。
func (k Key) AllowsModel(model string) bool {
	if len(k.Models) == 0 {
		return true
	}
	for _, item := range k.Models {
		if item == model {
			return true
		}
	}
	return false
}

// KeyInput 新建 / 修改密钥。修改时只改传了的字段：指针为 nil、Status 为空串表示不改。
type KeyInput struct {
	Name       *string          `json:"name"`
	Status     string           `json:"status"`
	Models     *[]string        `json:"models"`
	SpendLimit *decimal.Decimal `json:"spendLimit"`
	ExpiresAt  *time.Time       `json:"expiresAt"`
	// ClearExpiry 修改时清掉到期时间（ExpiresAt 为空本身表示「不改」）。
	ClearExpiry bool `json:"clearExpiry"`
}

// CreatedKey 新建密钥的结果：Secret 只出现这一次。
type CreatedKey struct {
	Key
	Secret string `json:"secret"`
}

// ── 调用记录 ──

// 调用记录状态。
const (
	StatusReserved  = "reserved"
	StatusSucceeded = "succeeded"
	// StatusAborted 流式输出中途客户端断开：按已经生成的内容计费。
	StatusAborted = "aborted"
	// StatusFailed 上游或网关失败：整笔放回，不扣费。
	StatusFailed = "failed"
	// StatusExpired 暂扣超时被后台回收（进程中断等），不扣费。
	StatusExpired = "expired"
	// StatusRefunded 成功后被管理员整笔退款。
	StatusRefunded = "refunded"
)

// 凭据种类。
const (
	AuthKey     = "key"
	AuthSession = "session"
)

// 暂扣的有效期：超过这个时长仍未结算的暂扣由后台循环放回。
// 流式请求可以持续很久（推理模型、长文本），给得更宽。
const (
	HoldTTL       = 30 * time.Minute
	StreamHoldTTL = 2 * time.Hour
)

// Hold 一个来源上的暂扣。
type Hold struct {
	Source string `json:"source"`
	// Micro 这个来源承担的点数（微点）。
	Micro int64 `json:"micro"`
	// Units 实际从账上移走的整数单位：积分为积分，余额为分。额度类来源为 0。
	Units  int64  `json:"units,omitempty"`
	Period string `json:"period,omitempty"`
	// Lots 额度包来源在各批次上的拆分（按扣费顺序）。
	Lots []LotPart `json:"lots,omitempty"`
}

// LotPart 一个额度批次承担的微点。
type LotPart struct {
	LotID int64 `json:"lotId"`
	Micro int64 `json:"micro"`
}

// Charge 一个来源上的实际扣费。
type Charge struct {
	Source string    `json:"source"`
	Micro  int64     `json:"micro"`
	Units  int64     `json:"units,omitempty"`
	Period string    `json:"period,omitempty"`
	TxnNo  string    `json:"txnNo,omitempty"`
	Lots   []LotPart `json:"lots,omitempty"`
}

// BillingSnapshot 暂扣那一刻的计费口径。结算、放回与退款都按它来，
// 调用进行中管理员改了扣费顺序、折算比例或额度，不影响这一笔。
type BillingSnapshot struct {
	Sources    []string        `json:"sources"`
	WalletRate decimal.Decimal `json:"walletRate"`
	Timezone   string          `json:"timezone"`
	// Quotas 各周期额度的上限（微点）。
	Quotas map[string]int64 `json:"quotas,omitempty"`
	// SpendLimit 每日消费上限（微点）与暂扣时所在的那一天。
	SpendLimit  int64   `json:"spendLimit,omitempty"`
	SpendPeriod string  `json:"spendPeriod,omitempty"`
	Profile     Profile `json:"profile"`
	// Member / Pricing 暂扣时生效的会员优惠与倍率分解（时段、叠加方式），结算与退款不再重算。
	Member  *MemberBenefit   `json:"member,omitempty"`
	Pricing *FactorBreakdown `json:"pricing,omitempty"`
}

// Location 快照里的时区。
func (b BillingSnapshot) Location() *time.Location {
	if loc, err := time.LoadLocation(b.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// Request 一次调用的记录。
type Request struct {
	ID             int64           `json:"id"`
	RequestNo      string          `json:"requestNo"`
	AppID          int64           `json:"appid"`
	UserID         int64           `json:"userId"`
	Account        string          `json:"account,omitempty"`
	KeyID          int64           `json:"keyId,omitempty"`
	KeyHint        string          `json:"keyHint,omitempty"`
	AuthKind       string          `json:"authKind"`
	Endpoint       string          `json:"endpoint"`
	Model          string          `json:"model"`
	UpstreamModel  string          `json:"upstreamModel,omitempty"`
	ConfigID       int64           `json:"configId,omitempty"`
	Provider       string          `json:"provider,omitempty"`
	Stream         bool            `json:"stream"`
	Status         string          `json:"status"`
	Price          Price           `json:"price"`
	Factor         decimal.Decimal `json:"factor"`
	Estimate       decimal.Decimal `json:"estimate"`
	Reserved       decimal.Decimal `json:"reserved"`
	Holds          []Hold          `json:"holds"`
	Billing        BillingSnapshot `json:"billing"`
	ClampMaxTokens int             `json:"clampMaxTokens,omitempty"`
	Meters
	UsageEstimated bool            `json:"usageEstimated"`
	Cost           decimal.Decimal `json:"cost"`
	Charged        decimal.Decimal `json:"charged"`
	Shortfall      decimal.Decimal `json:"shortfall"`
	Charges        []Charge        `json:"charges"`
	IntegralTxnNo  string          `json:"integralTxnNo,omitempty"`
	WalletTxnNo    string          `json:"walletTxnNo,omitempty"`
	ErrorCode      string          `json:"errorCode,omitempty"`
	ErrorMessage   string          `json:"errorMessage,omitempty"`
	ClientIP       string          `json:"clientIp,omitempty"`
	UserAgent      string          `json:"userAgent,omitempty"`
	LatencyMs      int             `json:"latencyMs"`
	TraceSummary
	// ConfigName 实际使用的通道名（联表取，通道删了就空）。
	ConfigName string `json:"configName,omitempty"`
	// Trace 追踪详情：只有管理端的单条查询带。
	Trace *Trace `json:"trace,omitempty"`
	// Live 进行中调用的实时进度：只在实时推送里有。
	Live         *LiveProgress `json:"live,omitempty"`
	RefundReason string        `json:"refundReason,omitempty"`
	RefundedBy   string        `json:"refundedBy,omitempty"`
	RefundedAt   *time.Time    `json:"refundedAt,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	ExpiresAt    time.Time     `json:"expiresAt"`
	SettledAt    *time.Time    `json:"settledAt,omitempty"`

	// 库里的微点原值，服务端用。
	EstimateMicro  int64 `json:"-"`
	ReservedMicro  int64 `json:"-"`
	CostMicro      int64 `json:"-"`
	ChargedMicro   int64 `json:"-"`
	ShortfallMicro int64 `json:"-"`
}

// RequestQuery 调用记录查询。
type RequestQuery struct {
	AppID    int64
	UserID   int64
	KeyID    int64
	Model    string
	Status   string
	Endpoint string
	// Keyword 按调用编号或上游请求编号查（前缀匹配）。
	Keyword  string
	ConfigID int64
	From     *time.Time
	To       *time.Time
	Page     int
	Limit    int
}

// Page 分页结果。
type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

// ── 暂扣与结算的入参 ──

// ReserveInput 暂扣的入参。估算用量由服务层给出，计价与分摊在仓储事务里用本包的纯函数做。
type ReserveInput struct {
	AppID    int64
	UserID   int64
	Key      *Key
	AuthKind string
	Endpoint string
	Stream   bool
	Settings Settings
	Model    Model
	Estimate Meters
	// Clampable 余额不够整笔估算时，能否把输出 token 上限压到付得起的数量（只有对话可以）。
	Clampable      bool
	IdempotencyKey string
	ClientIP       string
	UserAgent      string
	Now            time.Time
}

// ReserveResult 暂扣成功的结论。
type ReserveResult struct {
	RequestNo string
	Factor    decimal.Decimal
	Reserved  int64
	// ClampMaxTokens 非 0 时调用方必须把 max_tokens 压到这个值。
	ClampMaxTokens int
}

// SettleInput 结算的入参。
type SettleInput struct {
	RequestNo      string
	Status         string // succeeded / aborted
	Meters         Meters
	UsageEstimated bool
	UpstreamModel  string
	ConfigID       int64
	Provider       string
	LatencyMs      int
	Now            time.Time
}

// ReleaseInput 失败放回的入参。
type ReleaseInput struct {
	RequestNo    string
	Status       string // failed / expired
	ErrorCode    string
	ErrorMessage string
	ConfigID     int64
	Provider     string
	LatencyMs    int
}

// RefundInput 管理员退款。
type RefundInput struct {
	AppID     int64
	RequestNo string
	Reason    string
	Operator  string
}

// ── 用户视图 ──

// QuotaView 一个周期额度的当前状态。
type QuotaView struct {
	Source    string          `json:"source"`
	Label     string          `json:"label"`
	Period    string          `json:"period"`
	Limit     decimal.Decimal `json:"limit"`
	Used      decimal.Decimal `json:"used"`
	Reserved  decimal.Decimal `json:"reserved"`
	Remaining decimal.Decimal `json:"remaining"`
	// Eligible 当前用户能否使用（会员额度对非会员为 false）。
	Eligible bool `json:"eligible"`
}

// Overview 用户的网关总览。
type Overview struct {
	Enabled           bool            `json:"enabled"`
	AllowSessionToken bool            `json:"allowSessionToken"`
	Audience          string          `json:"audience"`
	Eligible          bool            `json:"eligible"`
	Unit              string          `json:"unit"`
	Sources           []SourceMeta    `json:"sources"`
	Profile           Profile         `json:"profile"`
	Member            MemberBenefit   `json:"member"`
	Factor            decimal.Decimal `json:"factor"`
	Pricing           FactorBreakdown `json:"pricing"`
	NextChangeAt      *time.Time      `json:"nextChangeAt"`
	Quotas            []QuotaView     `json:"quotas"`
	Pack              decimal.Decimal `json:"pack"`
	PackFrozen        decimal.Decimal `json:"packFrozen"`
	Credit            decimal.Decimal `json:"credit"`
	CreditFrozen      decimal.Decimal `json:"creditFrozen"`
	// Balance AI 额度合计：额度包 + 永久额度（不含暂扣中）。
	Balance decimal.Decimal `json:"balance"`
	// Expiring 7 天内到期的额度包合计与最早的到期时间。
	Expiring   *ExpiringCredit `json:"expiring"`
	Wallet     decimal.Decimal `json:"wallet"`
	WalletRate decimal.Decimal `json:"walletRate"`
	// Available 现在一共能用多少点（启用的来源合计，扣掉暂扣中的部分）。
	Available       decimal.Decimal `json:"available"`
	DailySpendLimit decimal.Decimal `json:"dailySpendLimit"`
	TodaySpent      decimal.Decimal `json:"todaySpent"`
	TotalSpent      decimal.Decimal `json:"totalSpent"`
	TotalRequests   int64           `json:"totalRequests"`
	RPM             int             `json:"rpm"`
	MaxConcurrent   int             `json:"maxConcurrent"`
	MaxKeysPerUser  int             `json:"maxKeysPerUser"`
	Welcome         WelcomeView     `json:"welcome"`
	Store           StoreView       `json:"store"`
	// BaseURL OpenAI SDK 的 baseURL 应填的路径（相对站点根）。
	BaseURL string `json:"baseUrl"`
}

// ExpiringCredit 即将到期的额度。
type ExpiringCredit struct {
	Amount decimal.Decimal `json:"amount"`
	At     time.Time       `json:"at"`
}

// WelcomeView 新人礼对这位用户的状态。
type WelcomeView struct {
	Enabled      bool            `json:"enabled"`
	Amount       decimal.Decimal `json:"amount"`
	ValidityDays int             `json:"validityDays"`
	Claimable    bool            `json:"claimable"`
	ClaimedAt    *time.Time      `json:"claimedAt"`
}

// StoreView 额度商店对用户是否开放。
type StoreView struct {
	Enabled bool `json:"enabled"`
}

// Account 网关账户的原始数值（微点）。
type Account struct {
	AppID         int64
	UserID        int64
	Credit        int64
	CreditFrozen  int64
	IntegralCarry int64
	WalletCarry   int64
	TotalSpent    int64
	TotalRequests int64
}

// AccountFacts 总览要的全部事实（仓储层一次取齐）。
type AccountFacts struct {
	Account     Account
	Profile     Profile
	Integral    int64
	WalletCents int64
	// Pack / PackFrozen 额度包批次此刻可用与暂扣中的合计（微点）；ExpiringMicro / ExpiringAt 7 天内到期的部分。
	Pack             int64
	PackFrozen       int64
	ExpiringMicro    int64
	ExpiringAt       *time.Time
	WelcomeClaimedAt *time.Time
	// QuotaUsage 当前周期的用量：键为来源。
	QuotaUsage map[string]QuotaUsage
}

// QuotaUsage 周期额度的用量（微点）。
type QuotaUsage struct {
	Period   string
	Used     int64
	Reserved int64
}

// CreditEntry 额度流水（永久额度与额度包）。
type CreditEntry struct {
	ID     int64           `json:"id"`
	Kind   string          `json:"kind"`
	Bucket string          `json:"bucket"`
	LotID  int64           `json:"lotId,omitempty"`
	Amount decimal.Decimal `json:"amount"`
	// BalanceAfter 这一桶变动后的余额；TotalAfter 变动后的 AI 额度合计（永久 + 额度包，不含暂扣）。
	BalanceAfter decimal.Decimal `json:"balanceAfter"`
	TotalAfter   decimal.Decimal `json:"totalAfter"`
	RefType      string          `json:"refType,omitempty"`
	RefID        string          `json:"refId,omitempty"`
	Remark       string          `json:"remark,omitempty"`
	Operator     string          `json:"operator,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// 额度流水种类。
const (
	CreditGrant       = "grant"
	CreditConsume     = "consume"
	CreditRefund      = "refund"
	CreditAdjust      = "adjust"
	CreditPurchase    = "purchase"
	CreditWelcome     = "welcome"
	CreditExpire      = "expire"
	CreditRevoke      = "revoke"
	CreditOrderRefund = "order_refund"
)

// 额度流水的桶。
const (
	BucketCredit = "credit"
	BucketPack   = "pack"
)

// CreditAdjustInput 管理员调整赠送额度。
type CreditAdjustInput struct {
	AppID     int64
	UserID    int64
	Amount    decimal.Decimal
	Reason    string
	RequestID string
	Operator  string
}

// CreditAdjustResult 调整结论。
type CreditAdjustResult struct {
	Balance  decimal.Decimal `json:"balance"`
	Replayed bool            `json:"replayed"`
}

// ── 统计 ──

// Stats 应用级统计。金额单位为点。
type Stats struct {
	Days          int             `json:"days"`
	Requests      int64           `json:"requests"`
	Succeeded     int64           `json:"succeeded"`
	Failed        int64           `json:"failed"`
	Users         int64           `json:"users"`
	InputTokens   int64           `json:"inputTokens"`
	OutputTokens  int64           `json:"outputTokens"`
	Images        int64           `json:"images"`
	Charged       decimal.Decimal `json:"charged"`
	Shortfall     decimal.Decimal `json:"shortfall"`
	Refunded      decimal.Decimal `json:"refunded"`
	BySource      []SourceTotal   `json:"bySource"`
	ByModel       []ModelTotal    `json:"byModel"`
	Daily         []DailyTotal    `json:"daily"`
	ReservedNow   int64           `json:"reservedNow"`
	CreditBalance decimal.Decimal `json:"creditBalance"`
}

// SourceTotal 按来源汇总的扣费。
type SourceTotal struct {
	Source string          `json:"source"`
	Amount decimal.Decimal `json:"amount"`
	// Units 积分为积分数，余额为分。
	Units int64 `json:"units"`
}

// ModelTotal 按模型汇总。
type ModelTotal struct {
	Model        string          `json:"model"`
	Requests     int64           `json:"requests"`
	InputTokens  int64           `json:"inputTokens"`
	OutputTokens int64           `json:"outputTokens"`
	Charged      decimal.Decimal `json:"charged"`
}

// DailyTotal 按日汇总。
type DailyTotal struct {
	Date     string          `json:"date"`
	Requests int64           `json:"requests"`
	Failed   int64           `json:"failed"`
	Charged  decimal.Decimal `json:"charged"`
}
