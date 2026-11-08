package security

import "time"

// 风控评估与判定已拆到独立的风控平台（services/risk，docs/risk-platform.md）。
// 这里只留主服务接入层用到的契约：业务调用 RiskService.Guard 时传的请求与拿回的结论。

// 评估场景：与风控平台的内置场景一一对应（services/risk/internal/catalog）。
// 新的业务接入点可以直接用一个新场景键，在控制台上建同名场景并发布即可生效。
const (
	SceneRegister      = "register"
	SceneLogin         = "login"
	SceneAdminLogin    = "admin_login"
	ScenePasswordReset = "password_reset"
	SceneVerifyCode    = "verify_code"
	ScenePayment       = "payment"
	SceneInvite        = "invite"
	SceneLottery       = "lottery"
	SceneSignIn        = "signin"
	SceneCardKey       = "cardkey"
	ScenePointsRedeem  = "points_redeem"
	SceneAdReward      = "ad_reward"
)

// 处置，从轻到重
const (
	ActionPass    = "pass"
	ActionReview  = "review"
	ActionCaptcha = "captcha"
	ActionBlock   = "block"
	ActionBan     = "ban"
)

// IsRejection 处置是否拒绝了这次请求
func IsRejection(action string) bool { return action == ActionBlock || action == ActionBan }

// RiskEvalRequest 一次风控评估的输入
type RiskEvalRequest struct {
	Scene      string `json:"scene"`
	AppID      *int64 `json:"appId,omitempty"`
	UserID     *int64 `json:"userId,omitempty"`
	IdentityID *int64 `json:"identityId,omitempty"`
	// Account 登录名 / 邮箱 / 手机号。为空时回退读 Extra["account"]（旧调用方）
	Account   string `json:"account,omitempty"`
	IP        string `json:"ip"`
	DeviceID  string `json:"deviceId"`
	UserAgent string `json:"userAgent"`
	// CaptchaPassed 本次请求已经通过了图形验证码。命中「验证码」处置时据此放行，
	// 而不是让用户在同一个关卡上无限循环。
	CaptchaPassed bool `json:"captchaPassed,omitempty"`
	// RequestID 幂等键：同一个业务请求重试时传同一个值，风控返回第一次的结论（不重复计数、不重复处置）
	RequestID string `json:"requestId,omitempty"`
	// Device 客户端上报的设备指纹要素（品牌、型号、屏幕、系统版本……），进关系图与相似设备检索
	Device map[string]string `json:"device,omitempty"`
	Phone  string            `json:"phone,omitempty"`
	Email  string            `json:"email,omitempty"`
	// TargetUserID 邀请人 / 转赠对象等「对方」
	TargetUserID int64          `json:"targetUserId,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// RiskRuleHit 命中的规则
type RiskRuleHit struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Score     int    `json:"score"`
	Reason    string `json:"reason,omitempty"`
}

// RiskEvalResult 风控结论
type RiskEvalResult struct {
	DecisionID string        `json:"decisionId"`
	Action     string        `json:"action"`
	RiskLevel  string        `json:"riskLevel"`
	Score      int           `json:"score"`
	Reasons    []string      `json:"reasons,omitempty"`
	Hits       []RiskRuleHit `json:"hits,omitempty"`
	DecidedBy  string        `json:"decidedBy"`
	Release    string        `json:"release,omitempty"`
	LatencyMS  int           `json:"latencyMs"`
	// Degraded 超时或出错的数据源（这些特征按默认值算了）
	Degraded []string `json:"degraded,omitempty"`
}

// IPRiskRecord IP 情报（IPQS 等情报源的查询结果，主服务补进评估请求的网络情报）
type IPRiskRecord struct {
	IP           string    `json:"ip"`
	RiskTag      string    `json:"riskTag"`
	RiskScore    int       `json:"riskScore"`
	Country      string    `json:"country"`
	Region       string    `json:"region"`
	ISP          string    `json:"isp"`
	ASN          string    `json:"asn"`
	Source       string    `json:"source"`
	IsProxy      bool      `json:"isProxy"`
	IsVPN        bool      `json:"isVpn"`
	IsTor        bool      `json:"isTor"`
	IsDatacenter bool      `json:"isDatacenter"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
}
