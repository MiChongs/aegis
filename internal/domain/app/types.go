package app

import "time"

type App struct {
	ID                     int64          `json:"id"`
	Name                   string         `json:"name"`
	AppKey                 string         `json:"appKey,omitempty"`
	Status                 bool           `json:"status"`
	DisabledReason         string         `json:"disabledReason,omitempty"`
	RegisterStatus         bool           `json:"registerStatus"`
	DisabledRegisterReason string         `json:"disabledRegisterReason,omitempty"`
	LoginStatus            bool           `json:"loginStatus"`
	DisabledLoginReason    string         `json:"disabledLoginReason,omitempty"`
	Settings               map[string]any `json:"settings,omitempty"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
}

type AppMutation struct {
	ID                     int64
	Name                   *string
	AppKey                 *string
	Status                 *bool
	DisabledReason         *string
	RegisterStatus         *bool
	DisabledRegisterReason *string
	LoginStatus            *bool
	DisabledLoginReason    *string
	Settings               map[string]any
}

// Policy 应用级认证策略。
//
// 分三组，组间语义不重叠；**每一项都必须有明确执行点** ——
// 只落库不生效的开关会让管理员以为已经防住了，比没有这个开关更危险。
// 执行点索引见 service.AuthService 与 service.LoginConsistencyService。
//
//	设备绑定    LoginCheckDevice + DeviceRebindInterval
//	登录一致性  LoginCheckIP / LoginCheckUser（与上次成功登录比对）
//	会话与注册  MultiDeviceLogin / MultiDeviceLimit / RegisterCheckIP
//
// 注册验证码不在此处：能否要求验证码由应用的验证码配置（captcha.requireForRegister）
// 单独决定。两处配同一件事时接入方无从判断哪个生效。
type Policy struct {
	// LoginCheckDevice 登录必须显式携带设备标识，且与该用户已绑定设备一致。
	LoginCheckDevice bool `json:"loginCheckDevice"`
	// DeviceRebindInterval 设备换绑冷却秒数，0 = 不限制。
	// JSON 键沿用旧系统的 loginCheckDeviceTimeOut（原义即「登录换绑机器码间隔」），
	// 已发出去的客户端与导入的历史数据不该因为改叫法而失效。
	DeviceRebindInterval int `json:"loginCheckDeviceTimeOut"`
	// LoginCheckIP 登录 IP 与上次成功登录不在同一网段（IPv4 /24、IPv6 /48）时拦截。
	LoginCheckIP bool `json:"loginCheckIp"`
	// LoginCheckUser 登录属地（国家 + 省/州）与上次成功登录不一致时拦截。
	LoginCheckUser bool `json:"loginCheckUser"`
	// MultiDeviceLogin 是否允许多设备同时在线。
	MultiDeviceLogin bool `json:"multiDeviceLogin"`
	// MultiDeviceLimit 同时在线设备上限；MultiDeviceLogin 为 false 时恒为 1。
	MultiDeviceLimit int `json:"multiDeviceLimit"`
	// RegisterCheckIP 同一 IP 不允许重复注册。
	RegisterCheckIP bool `json:"registerCheckIp"`
}

// CommerceSettings 应用级交易设置。
//
// 目前只有积分兑换率一项，但它此前完全没有配置入口：
// PaymentService 在 integral_purchase 订单里读 settings.integralPerCurrency，
// 而任何管理接口都写不到这个键 —— 所有应用只能用兜底的 100。
type CommerceSettings struct {
	// IntegralPerCurrency 每单位金额兑换的积分数（integral_purchase 订单用）。
	// 由服务端计算发放数量，客户端不能指定。
	IntegralPerCurrency int `json:"integralPerCurrency"`
	// ReceiptEmailOnPaid 支付成功后自动把凭证寄到下单用户绑定的邮箱。
	// 用户没绑邮箱时静默跳过 —— 那不是错误，只是这条链路无处可送。
	ReceiptEmailOnPaid bool `json:"receiptEmailOnPaid"`
	// ReceiptLocale 自动寄送时的凭证语言（BCP 47）。留空则按用户设置协商，
	// 再协商不到用平台默认（en）。
	ReceiptLocale string `json:"receiptLocale,omitempty"`
	// WalletCurrency 钱包记账币种（ISO 4217）。
	//
	// 执行点是钱包流水凭证上的币种：钱包余额本身没有币种列（余额只是一个数），
	// 而一份印着数字却不说是哪国钱的凭证既不能报销也不能对账。
	// 应用级而不是平台级：同一个平台上完全可能一个应用收人民币、另一个收美元。
	WalletCurrency string `json:"walletCurrency,omitempty"`
}

// DefaultIntegralPerCurrency 未配置时的兑换率，与 PaymentService 的兜底值一致。
const DefaultIntegralPerCurrency = 100

// DefaultWalletCurrency 未配置时的钱包记账币种，与余额渠道自述的首选货币一致。
const DefaultWalletCurrency = "CNY"

// LoginBaseline 登录一致性基线：该用户上一次被放行的登录指纹。
// 存 Redis（登录路径上的读写，不该每次登录都打库），过期即视为无基线放行。
type LoginBaseline struct {
	DeviceID string `json:"deviceId,omitempty"`
	IP       string `json:"ip,omitempty"`
	// Region 归一化后的属地标识（country/province），空表示当次登录未能定位
	Region string `json:"region,omitempty"`
	// DeviceBoundAt 最近一次设备换绑时间，用于 DeviceRebindInterval 冷却判定
	DeviceBoundAt time.Time `json:"deviceBoundAt,omitzero"`
	UpdatedAt     time.Time `json:"updatedAt,omitzero"`
}

type TransportEncryptionPolicy struct {
	Enabled            bool   `json:"enabled"`
	Strict             bool   `json:"strict"`
	ResponseEncryption bool   `json:"responseEncryption"`
	Secret             string `json:"-"`
}

// TransportEncryptionView 加密配置视图（不含私钥）
type TransportEncryptionView struct {
	Enabled             bool     `json:"enabled"`
	Strict              bool     `json:"strict"`
	ResponseEncryption  bool     `json:"responseEncryption"`
	HasSecret           bool     `json:"hasSecret"`
	SecretHint          string   `json:"secretHint,omitempty"`
	AllowedAlgorithms   []string `json:"allowedAlgorithms"`
	SupportedAlgorithms []string `json:"supportedAlgorithms"`
	HasRSAKey           bool     `json:"hasRSAKey"`
	RSAPublicKey        string   `json:"rsaPublicKey,omitempty"`
	HasECDHKey          bool     `json:"hasECDHKey"`
	ECDHPublicKey       string   `json:"ecdhPublicKey,omitempty"`
}

// TransportEncryptionUpdate 加密配置更新
type TransportEncryptionUpdate struct {
	Enabled            *bool    `json:"enabled"`
	Strict             *bool    `json:"strict"`
	ResponseEncryption *bool    `json:"responseEncryption"`
	Secret             *string  `json:"secret"`
	AllowedAlgorithms  []string `json:"allowedAlgorithms,omitempty"`
	GenerateRSAKey     bool     `json:"generateRSAKey,omitempty"`
	GenerateECDHKey    bool     `json:"generateECDHKey,omitempty"`
}

type Stats struct {
	AppID              int64 `json:"appid"`
	TotalUsers         int64 `json:"totalUsers"`
	EnabledUsers       int64 `json:"enabledUsers"`
	DisabledUsers      int64 `json:"disabledUsers"`
	BannerCount        int64 `json:"bannerCount"`
	NoticeCount        int64 `json:"noticeCount"`
	OAuthBindCount     int64 `json:"oauthBindCount"`
	NewUsersToday      int64 `json:"newUsersToday"`
	NewUsersLast7Days  int64 `json:"newUsersLast7Days"`
	NewUsersLast30Days int64 `json:"newUsersLast30Days"`
	LoginSuccessToday  int64 `json:"loginSuccessToday"`
	LoginFailureToday  int64 `json:"loginFailureToday"`
}

type UserTrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type UserTrend struct {
	AppID    int64            `json:"appid"`
	Days     int              `json:"days"`
	TotalNew int64            `json:"totalNew"`
	Series   []UserTrendPoint `json:"series"`
}

type LoginAuditQuery struct {
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	Page    int    `json:"page"`
	Limit   int    `json:"limit"`
}

type LoginAuditExportQuery struct {
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	Limit   int    `json:"limit"`
}

type LoginAuditItem struct {
	ID        int64          `json:"id"`
	UserID    *int64         `json:"userId,omitempty"`
	AppID     int64          `json:"appid"`
	Account   string         `json:"account,omitempty"`
	Nickname  string         `json:"nickname,omitempty"`
	LoginType string         `json:"loginType"`
	Provider  string         `json:"provider,omitempty"`
	TokenJTI  string         `json:"tokenJti,omitempty"`
	LoginIP   string         `json:"loginIp,omitempty"`
	DeviceID  string         `json:"deviceId,omitempty"`
	UserAgent string         `json:"userAgent,omitempty"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type LoginAuditListResult struct {
	Items      []LoginAuditItem `json:"items"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"totalPages"`
}

type SessionAuditQuery struct {
	Keyword   string `json:"keyword"`
	EventType string `json:"eventType"`
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
}

type SessionAuditExportQuery struct {
	Keyword   string `json:"keyword"`
	EventType string `json:"eventType"`
	Limit     int    `json:"limit"`
}

type SessionAuditItem struct {
	ID        int64          `json:"id"`
	UserID    *int64         `json:"userId,omitempty"`
	AppID     int64          `json:"appid"`
	Account   string         `json:"account,omitempty"`
	Nickname  string         `json:"nickname,omitempty"`
	TokenJTI  string         `json:"tokenJti,omitempty"`
	EventType string         `json:"eventType"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type SessionAuditListResult struct {
	Items      []SessionAuditItem `json:"items"`
	Page       int                `json:"page"`
	Limit      int                `json:"limit"`
	Total      int64              `json:"total"`
	TotalPages int                `json:"totalPages"`
}

type RegionStatsQuery struct {
	Type  string `json:"type"`
	Limit int    `json:"limit"`
}

type RegionStatItem struct {
	Region     string `json:"region"`
	Code       string `json:"code,omitempty"`
	Parent     string `json:"parent,omitempty"`
	ParentPath string `json:"parentPath,omitempty"`
	Count      int64  `json:"count"`
}

type RegionStatsResult struct {
	AppID int64            `json:"appid"`
	Type  string           `json:"type"`
	Total int64            `json:"total"`
	Items []RegionStatItem `json:"items"`
}

type AuthSourceStatItem struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

type AuthSourceStats struct {
	AppID            int64                `json:"appid"`
	TotalUsers       int64                `json:"totalUsers"`
	PasswordUsers    int64                `json:"passwordUsers"`
	OAuthBoundUsers  int64                `json:"oauthBoundUsers"`
	ProviderBindings []AuthSourceStatItem `json:"providerBindings"`
}

// Banner 应用级轮播/投放位。
//
// Header 存的是**持久化形态**：外链 URL，或上传到对象存储后得到的
// `storage://{configID}/{objectKey}` 引用。浏览器不能直接访问后者，
// 因此读取时另外解析出 HeaderDisplayURL —— 与 system.PlatformBanner 同一套约定。
type Banner struct {
	ID     int64  `json:"id"`
	Header string `json:"header,omitempty"`
	// HeaderDisplayURL 读取时解析得到的可直接展示地址；不落库。
	HeaderDisplayURL string     `json:"headerDisplayUrl,omitempty"`
	Title            string     `json:"title"`
	Content          string     `json:"content,omitempty"`
	URL              string     `json:"url,omitempty"`
	Type             string     `json:"type"`
	Position         int        `json:"position"`
	Status           bool       `json:"status"`
	StartTime        *time.Time `json:"startTime,omitempty"`
	EndTime          *time.Time `json:"endTime,omitempty"`
	ViewCount        int64      `json:"viewCount"`
	ClickCount       int64      `json:"clickCount"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type BannerMutation struct {
	ID        int64
	Header    *string
	Title     *string
	Content   *string
	URL       *string
	Type      *string
	Position  *int
	Status    *bool
	StartTime *time.Time
	EndTime   *time.Time
}

// Notice 应用级公告。
//
// Summary 是服务端从 Content 提取的纯文本摘要：列表页、推送、客户端通知栏
// 要的都是这一段，让每一端各自解析一遍富文本既慢，解析结果也不会一致。
type Notice struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title,omitempty"`
	Content     string     `json:"content"`
	Summary     string     `json:"summary,omitempty"`
	Type        string     `json:"type"`
	Level       string     `json:"level"`
	Status      string     `json:"status"`
	Pinned      bool       `json:"pinned"`
	StartTime   *time.Time `json:"startTime,omitempty"`
	EndTime     *time.Time `json:"endTime,omitempty"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	ViewCount   int64      `json:"viewCount"`
	CreatedBy   *int64     `json:"createdBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type NoticeMutation struct {
	ID        int64
	Title     *string
	Content   *string
	Type      *string
	Level     *string
	Status    *string
	Pinned    *bool
	StartTime *time.Time
	EndTime   *time.Time
	CreatedBy *int64
}

// NoticeFilter 管理端列表过滤。Status / Type / Level 为空表示不限。
type NoticeFilter struct {
	Status  string
	Type    string
	Level   string
	Keyword string
	Limit   int
	Offset  int
}

// BannerFilter 管理端列表过滤。Banner 数量天然很少（一个投放位通常个位数），
// 因此不分页 —— 分页会让拖拽排序失去全局视野，第 2 页的第 1 条拖不到第 1 页去。
type BannerFilter struct {
	Status  *bool
	Type    string
	Keyword string
}

// ContentOverview 内容中心总览。一次取齐 Banner 与公告两侧的计数，
// 分开拉会出现「Banner 已刷新、公告还是上一次」的自相矛盾画面。
type ContentOverview struct {
	BannerTotal      int64 `json:"bannerTotal"`
	BannerLive       int64 `json:"bannerLive"`      // 启用且在投放窗口内
	BannerScheduled  int64 `json:"bannerScheduled"` // 启用但还没开始
	BannerExpired    int64 `json:"bannerExpired"`   // 启用但已过期
	BannerDisabled   int64 `json:"bannerDisabled"`
	BannerViews      int64 `json:"bannerViews"`
	BannerClicks     int64 `json:"bannerClicks"`
	NoticeTotal      int64 `json:"noticeTotal"`
	NoticePublished  int64 `json:"noticePublished"`
	NoticeDraft      int64 `json:"noticeDraft"`
	NoticeArchived   int64 `json:"noticeArchived"`
	NoticePinned     int64 `json:"noticePinned"`
	NoticeViews      int64 `json:"noticeViews"`
	LastPublishedAt  *time.Time `json:"lastPublishedAt,omitempty"`
}

// 展示位：客户端据此决定这条 Banner 画在哪儿。
// 一个接口返回全部 Banner，客户端按位取用，比每个位开一条接口好维护。
const (
	BannerSlotHero   = "hero"   // 首页轮播
	BannerSlotPopup  = "popup"  // 启动弹窗
	BannerSlotSplash = "splash" // 开屏
	BannerSlotNotice = "notice" // 通知条
	BannerSlotCard   = "card"   // 卡片位
)

var ValidBannerTypes = map[string]struct{}{
	BannerSlotHero:   {},
	BannerSlotPopup:  {},
	BannerSlotSplash: {},
	BannerSlotNotice: {},
	BannerSlotCard:   {},
}

const (
	NoticeStatusDraft     = "draft"
	NoticeStatusPublished = "published"
	NoticeStatusArchived  = "archived"
)

var ValidNoticeStatuses = map[string]struct{}{
	NoticeStatusDraft:     {},
	NoticeStatusPublished: {},
	NoticeStatusArchived:  {},
}

var ValidNoticeTypes = map[string]struct{}{
	"notice":      {},
	"activity":    {},
	"maintenance": {},
	"update":      {},
	"security":    {},
}

var ValidNoticeLevels = map[string]struct{}{
	"normal":    {},
	"important": {},
	"critical":  {},
}

type PasswordPolicy struct {
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	MinLength           int    `json:"minLength"`
	MaxLength           int    `json:"maxLength"`
	RequireUppercase    bool   `json:"requireUppercase"`
	RequireLowercase    bool   `json:"requireLowercase"`
	RequireNumbers      bool   `json:"requireNumbers"`
	RequireSpecialChars bool   `json:"requireSpecialChars"`
	MinScore            int    `json:"minScore"`
	MaxAge              int    `json:"maxAge"`
	PreventReuse        int    `json:"preventReuse"`
	IsDefault           bool   `json:"isDefault"`
}

type PasswordPolicyStats struct {
	TotalUsers      int64 `json:"totalUsers"`
	PasswordUsers   int64 `json:"passwordUsers"`
	CompliantUsers  int64 `json:"compliantUsers"`
	ComplianceRate  int64 `json:"complianceRate"`
	NeedChangeUsers int64 `json:"needChangeUsers"`
	NeedChangeRate  int64 `json:"needChangeRate"`
}

type PasswordPolicyView struct {
	AppID   int64                `json:"appid"`
	AppName string               `json:"appName"`
	Policy  PasswordPolicy       `json:"policy"`
	Stats   *PasswordPolicyStats `json:"stats,omitempty"`
}

// PasswordPatternMatch 一次可猜测模式的命中明细，由 zxcvbn 的匹配序列翻译而来。
//
// **刻意不返回命中的原文片段**：这个结构会经 `/password-policy/test` 出网，
// 也会进审计日志，回显密码子串等于把「被测密码」泄露出去。位置区间已经
// 足够告诉管理员「哪一段拖低了分数」。
type PasswordPatternMatch struct {
	Kind    string  `json:"kind"`             // dictionary / spatial / repeat / sequence / date / regex / bruteforce
	Label   string  `json:"label"`            // 面向人的中文名
	Source  string  `json:"source,omitempty"` // 字典来源（passwords / 中文弱口令 / 账号信息 …）
	Start   int     `json:"start"`            // 命中区间起点，1 起算（按字符不按字节）
	End     int     `json:"end"`              // 命中区间终点，闭区间
	Guesses float64 `json:"guesses,omitempty"`
}

type PasswordStrengthDetails struct {
	Length          int  `json:"length"` // 字符数（rune），不是字节数
	HasLowercase    bool `json:"hasLowercase"`
	HasUppercase    bool `json:"hasUppercase"`
	HasNumbers      bool `json:"hasNumbers"`
	HasSpecialChars bool `json:"hasSpecialChars"`
	// HasCommonPatterns 命中模式的中文名去重列表，保留此字段是为了兼容既有前端。
	// 新接入请读 Patterns，它带位置与来源。
	HasCommonPatterns []string               `json:"hasCommonPatterns"`
	Patterns          []PasswordPatternMatch `json:"patterns,omitempty"`
	// Entropy 口令的**猜测熵**（log2(guesses)，单位 bit），由 zxcvbn 估算。
	// 注意与旧实现的语义不同：旧值是每字符香农熵（0~4.5），只反映字符分布，
	// 一个 "abcabcabc" 也能拿到不低的分数。
	Entropy float64 `json:"entropy"`
	// ByteLength 字节数。bcrypt 的 72 字节硬上限按字节算，中文口令会远早于
	// 字符数上限触顶，因此必须单独暴露。
	ByteLength int `json:"byteLength"`
}

type PasswordRecommendation struct {
	Type     string `json:"type"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

type PasswordStrengthAnalysis struct {
	Score           int                      `json:"score"`
	Level           string                   `json:"level"`
	Feedback        []string                 `json:"feedback"`
	Details         PasswordStrengthDetails  `json:"details"`
	Recommendations []PasswordRecommendation `json:"recommendations"`
	// ZxcvbnScore zxcvbn 原生的 0~4 档位，与 Score（0~100）同源不同刻度。
	// 同时给出是为了让接入方能直接对齐社区通行的五档口径。
	ZxcvbnScore int `json:"zxcvbnScore"`
	// GuessesLog10 估算的猜测次数以 10 为底的对数。它才是评分的真正依据，
	// 0~100 分只是它的一个线性映射。
	GuessesLog10 float64 `json:"guessesLog10"`
	// CrackTime 离线慢哈希（1e4 次/秒，对应 bcrypt 这类）场景下的可读破解时长。
	// 选这个场景是因为平台确实用 bcrypt 存密码，报别的场景会误导。
	CrackTime string `json:"crackTime"`
}

type PasswordPolicyCheck struct {
	IsValid    bool                     `json:"isValid"`
	Violations []string                 `json:"violations"`
	Analysis   PasswordStrengthAnalysis `json:"analysis"`
	Policy     PasswordPolicy           `json:"policy"`
}

type PasswordPolicyTestSummary struct {
	IsValid         bool                     `json:"isValid"`
	Score           int                      `json:"score"`
	Level           string                   `json:"level"`
	Violations      []string                 `json:"violations"`
	Recommendations []PasswordRecommendation `json:"recommendations"`
}

type PasswordPolicyTestResult struct {
	Password         string                    `json:"password"`
	Policy           PasswordPolicy            `json:"policy"`
	StrengthAnalysis PasswordStrengthAnalysis  `json:"strengthAnalysis"`
	PolicyCheck      PasswordPolicyCheck       `json:"policyCheck"`
	Result           PasswordPolicyTestSummary `json:"result"`
}

type SignInRewardPolicy struct {
	Name                           string                  `json:"name"`
	Description                    string                  `json:"description,omitempty"`
	Enabled                        bool                    `json:"enabled"`
	Timezone                       string                  `json:"timezone"`
	BaseIntegral                   int64                   `json:"baseIntegral"`
	BaseExperience                 int64                   `json:"baseExperience"`
	FirstSignInExperienceBonus     int64                   `json:"firstSignInExperienceBonus"`
	ConsecutiveExperienceStep      int64                   `json:"consecutiveExperienceStep"`
	ConsecutiveExperienceStepCap   int64                   `json:"consecutiveExperienceStepCap"`
	ApplyLevelExperienceMultiplier bool                    `json:"applyLevelExperienceMultiplier"`
	MaxIntegralReward              int64                   `json:"maxIntegralReward"`
	MaxExperienceReward            int64                   `json:"maxExperienceReward"`
	Rules                          []SignInRewardRule      `json:"rules"`
	Milestones                     []SignInRewardMilestone `json:"milestones"`
	IsDefault                      bool                    `json:"isDefault"`
}

type SignInRewardRule struct {
	Key                       string  `json:"key"`
	Name                      string  `json:"name"`
	Description               string  `json:"description,omitempty"`
	Enabled                   bool    `json:"enabled"`
	Priority                  int     `json:"priority"`
	Group                     string  `json:"group,omitempty"`
	Expression                string  `json:"expression"`
	BonusType                 string  `json:"bonusType,omitempty"`
	BonusDescription          string  `json:"bonusDescription,omitempty"`
	IntegralMultiplierDelta   float64 `json:"integralMultiplierDelta"`
	IntegralBonus             int64   `json:"integralBonus"`
	ExperienceMultiplierDelta float64 `json:"experienceMultiplierDelta"`
	ExperienceBonus           int64   `json:"experienceBonus"`
}

type SignInRewardMilestone struct {
	ConsecutiveDays int64  `json:"consecutiveDays"`
	IntegralBonus   int64  `json:"integralBonus"`
	ExperienceBonus int64  `json:"experienceBonus"`
	BonusType       string `json:"bonusType,omitempty"`
	Description     string `json:"description,omitempty"`
}

type SignInRewardPolicyView struct {
	AppID   int64              `json:"appid"`
	AppName string             `json:"appName"`
	Policy  SignInRewardPolicy `json:"policy"`
}

type SignInRewardAppliedRule struct {
	Key                       string  `json:"key"`
	Name                      string  `json:"name"`
	Group                     string  `json:"group,omitempty"`
	BonusType                 string  `json:"bonusType,omitempty"`
	Description               string  `json:"description,omitempty"`
	Expression                string  `json:"expression,omitempty"`
	IntegralMultiplierDelta   float64 `json:"integralMultiplierDelta"`
	IntegralBonus             int64   `json:"integralBonus"`
	ExperienceMultiplierDelta float64 `json:"experienceMultiplierDelta"`
	ExperienceBonus           int64   `json:"experienceBonus"`
	ConsecutiveDays           int64   `json:"consecutiveDays,omitempty"`
}

type SignInRewardResolved struct {
	BaseIntegral     int64   `json:"baseIntegral"`
	IntegralReward   int64   `json:"integralReward"`
	ExperienceReward int64   `json:"experienceReward"`
	RewardMultiplier float64 `json:"rewardMultiplier"`
	BonusType        string  `json:"bonusType,omitempty"`
	BonusDescription string  `json:"bonusDescription,omitempty"`
}

type SignInRewardPreviewInput struct {
	OccurredAt      *time.Time `json:"occurredAt,omitempty"`
	ConsecutiveDays int        `json:"consecutiveDays"`
	TotalSignIns    int64      `json:"totalSignIns"`
	UserExperience  int64      `json:"userExperience"`
}

type SignInRewardPreview struct {
	AppID        int64                     `json:"appid"`
	AppName      string                    `json:"appName"`
	OccurredAt   time.Time                 `json:"occurredAt"`
	Timezone     string                    `json:"timezone"`
	Policy       SignInRewardPolicy        `json:"policy"`
	Reward       SignInRewardResolved      `json:"reward"`
	AppliedRules []SignInRewardAppliedRule `json:"appliedRules"`
	Environment  map[string]any            `json:"environment"`
}

type AppSignInStats struct {
	AppID                 int64                 `json:"appid"`
	Days                  int                   `json:"days"`
	TodaySignCount        int64                 `json:"todaySignCount"`
	TotalSignRecords      int64                 `json:"totalSignRecords"`
	UniqueSignedUsers     int64                 `json:"uniqueSignedUsers"`
	TotalIntegralReward   int64                 `json:"totalIntegralReward"`
	TotalExperienceReward int64                 `json:"totalExperienceReward"`
	AvgConsecutiveDays    float64               `json:"avgConsecutiveDays"`
	MaxConsecutiveDays    int64                 `json:"maxConsecutiveDays"`
	Trend                 []AppSignInTrendPoint `json:"trend"`
	Sources               []AppSignInSourceStat `json:"sources"`
}

type AppSignInTrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type AppSignInSourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

type AppSignInRecordQuery struct {
	Keyword  string     `json:"keyword"`
	Source   string     `json:"source"`
	DateFrom *time.Time `json:"dateFrom,omitempty"`
	DateTo   *time.Time `json:"dateTo,omitempty"`
	Page     int        `json:"page"`
	Limit    int        `json:"limit"`
}

type AppSignInRecordItem struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"userId"`
	AppID            int64     `json:"appid"`
	Account          string    `json:"account"`
	Nickname         string    `json:"nickname,omitempty"`
	Avatar           string    `json:"avatar,omitempty"`
	Email            string    `json:"email,omitempty"`
	Phone            string    `json:"phone,omitempty"`
	SignDate         string    `json:"signDate"`
	SignedAt         time.Time `json:"signedAt"`
	IntegralReward   int64     `json:"integralReward"`
	ExperienceReward int64     `json:"experienceReward"`
	ConsecutiveDays  int       `json:"consecutiveDays"`
	RewardMultiplier float64   `json:"rewardMultiplier"`
	BonusType        string    `json:"bonusType,omitempty"`
	BonusDescription string    `json:"bonusDescription,omitempty"`
	SignInSource     string    `json:"signInSource,omitempty"`
	DeviceInfo       string    `json:"deviceInfo,omitempty"`
	IPAddress        string    `json:"ipAddress,omitempty"`
	Location         string    `json:"location,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

type AppSignInRecordListResult struct {
	Items      []AppSignInRecordItem `json:"items"`
	Page       int                   `json:"page"`
	Limit      int                   `json:"limit"`
	Total      int64                 `json:"total"`
	TotalPages int                   `json:"totalPages"`
}

type Site struct {
	ID          int64          `json:"id"`
	AppID       int64          `json:"appid"`
	UserID      int64          `json:"userId"`
	Account     string         `json:"account,omitempty"`
	Nickname    string         `json:"nickname,omitempty"`
	Avatar      string         `json:"avatar,omitempty"`
	Header      string         `json:"header,omitempty"`
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Category    string         `json:"category,omitempty"`
	Status      string         `json:"status"`
	AuditStatus string         `json:"audit_status"`
	AuditReason string         `json:"audit_reason,omitempty"`
	IsPinned    bool           `json:"is_pinned"`
	ViewCount   int64          `json:"view_count"`
	LikeCount   int64          `json:"like_count"`
	Extra       map[string]any `json:"extra,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type SiteMutation struct {
	ID          int64
	AppID       int64
	UserID      int64
	Header      *string
	Name        *string
	URL         *string
	Type        *string
	Description *string
	Category    *string
}

type SiteListResult struct {
	List        []Site `json:"list"`
	Page        int    `json:"page"`
	Limit       int    `json:"limit"`
	Total       int64  `json:"total"`
	TotalPages  int    `json:"totalPages"`
	HasNextPage bool   `json:"hasNextPage"`
	HasPrevPage bool   `json:"hasPrevPage"`
	Cached      bool   `json:"cached,omitempty"`
}

type SiteListQuery struct {
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
	Keyword   string `json:"keyword"`
	SortBy    string `json:"sortBy"`
	SortOrder string `json:"sortOrder"`
	Category  string `json:"category"`
	Status    string `json:"status"`
}

type SiteAuditStats struct {
	AppID    int64            `json:"appid"`
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"byStatus"`
	Pending  int64            `json:"pending"`
	Approved int64            `json:"approved"`
	Rejected int64            `json:"rejected"`
}

type AppVersion struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	ChannelID     *int64         `json:"channel_id,omitempty"`
	ChannelName   string         `json:"channel_name,omitempty"`
	Version       string         `json:"version"`
	VersionCode   int64          `json:"version_code"`
	Description   string         `json:"description,omitempty"`
	ReleaseNotes  string         `json:"release_notes,omitempty"`
	DownloadURL   string         `json:"download_url,omitempty"`
	FileSize      int64          `json:"file_size"`
	FileHash      string         `json:"file_hash,omitempty"`
	ForceUpdate   bool           `json:"force_update"`
	UpdateType    string         `json:"update_type"`
	Platform      string         `json:"platform"`
	MinOSVersion  string         `json:"min_os_version,omitempty"`
	Status        string         `json:"status"`
	DownloadCount int64          `json:"download_count"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type AppVersionMutation struct {
	ID           int64
	AppID        int64
	ChannelID    *int64
	Version      *string
	VersionCode  *int64
	Description  *string
	ReleaseNotes *string
	DownloadURL  *string
	FileSize     *int64
	FileHash     *string
	ForceUpdate  *bool
	UpdateType   *string
	Platform     *string
	MinOSVersion *string
	Status       *string
	Metadata     map[string]any
}

type AppVersionListQuery struct {
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
	Status    string `json:"status"`
	Platform  string `json:"platform"`
	ChannelID int64  `json:"channel_id"`
}

type AppVersionListResult struct {
	Items      []AppVersion `json:"items"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	Total      int64        `json:"total"`
	TotalPages int          `json:"totalPages"`
}

type AppVersionChannel struct {
	ID             int64          `json:"id"`
	AppID          int64          `json:"appid"`
	Name           string         `json:"name"`
	Code           string         `json:"code"`
	Description    string         `json:"description,omitempty"`
	IsDefault      bool           `json:"is_default"`
	Status         bool           `json:"status"`
	Priority       int            `json:"priority"`
	Color          string         `json:"color,omitempty"`
	Level          string         `json:"level"`
	RolloutPct     int            `json:"rollout_pct"`
	Platforms      []string       `json:"platforms,omitempty"`
	MinVersionCode int64          `json:"min_version_code"`
	MaxVersionCode int64          `json:"max_version_code"`
	Rules          []ChannelRule  `json:"rules,omitempty"`
	TargetAudience map[string]any `json:"targetAudience,omitempty"`
	UserCount      int64          `json:"userCount,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// ChannelRule 灰度分发条件规则
type ChannelRule struct {
	Field string `json:"field"` // 匹配字段：platform / os_version / user_id / region / tag 等
	Op    string `json:"op"`    // 操作符：eq / neq / in / not_in / gt / lt / gte / lte / regex / contains
	Value any    `json:"value"` // 匹配值（字符串 / 数字 / 数组）
}

type AppVersionChannelMutation struct {
	ID             int64
	AppID          int64
	Name           *string
	Code           *string
	Description    *string
	IsDefault      *bool
	Status         *bool
	Priority       *int
	Color          *string
	Level          *string
	RolloutPct     *int
	Platforms      []string
	MinVersionCode *int64
	MaxVersionCode *int64
	Rules          []ChannelRule
	TargetAudience map[string]any
}

type AppVersionCheckResult struct {
	Version     *AppVersion `json:"version,omitempty"`
	ChannelName string      `json:"channelName,omitempty"`
}

type AppVersionStats struct {
	AppID          int64            `json:"appid"`
	TotalVersions  int64            `json:"totalVersions"`
	PublishedCount int64            `json:"publishedCount"`
	ChannelCount   int64            `json:"channelCount"`
	PlatformCounts map[string]int64 `json:"platformCounts"`
}
