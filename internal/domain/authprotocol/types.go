package authprotocol

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	oauthdomain "aegis/internal/domain/oauth"
)

const (
	ProtocolVersion = "aegis-app-v1"
	TransportV2     = "aegis-transport-v2"
	TransportAlgo   = "x25519-xchacha20-poly1305"
	SignatureScheme = "aegis-hmac-sha256"
	// SignaturePrefix 是 X-Aegis-Signature 头的版本前缀，便于日后平滑替换算法。
	//
	// v1 的待签名字符串**不含 query string**，因此只在请求没有 query 时才被接受
	// （见 VerifySignature）—— 否则 `?page=1` 可以被改成 `?page=999` 而签名照样通过。
	// 当初只有 POST + JSON body 的路由，这个洞碰不到；接口面铺开后必须堵上。
	SignaturePrefix   = "v1="
	SignaturePrefixV2 = "v2="
)

// SealedPayloadParam 是 sealed 档下无请求体方法（GET / DELETE / HEAD）承载密文的查询参数。
//
// 为什么不塞进 body：HTTP 允许 GET 带 body，但 OkHttp、URLSession、浏览器 fetch
// 全都拒绝构造这种请求 —— 恰恰是 Android / iOS / Web 三端。密文因此走 query，
// 明文（真正的 query string）解出来后回填 URL.RawQuery。
// 密文本身被 v2 签名覆盖，改不动。
const SealedPayloadParam = "_payload"

// 请求体上限。三处必须读同一份值：网关中间件用它截断读取、sealed 解密后用它复查、
// /config 用它告诉客户端「多大会被拒」—— 三处各写一个数字必然漂移，
// 而漂移的表现是「小文件能传、大文件报一个和大小无关的错」。
const (
	// MaxRequestBytes 普通请求（JSON / query）的上限。
	MaxRequestBytes = 8 << 20
	// MaxUploadBytes multipart 上传的上限，仅上传类路由适用。
	MaxUploadBytes = 32 << 20
	// MaxQueryBytes sealed 档解密后 query string 的上限。
	MaxQueryBytes = 8 << 10
)

// BodylessMethod 判定该方法在 sealed 档下走 query 承载而非 body 承载。
func BodylessMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		return true
	default:
		return false
	}
}

// 安全等级：三档共用同一批路径与同一份 JSON 结构，只改变请求"怎么包装"。
// 接入方因此可以先用 standard 跑通业务，再单独换掉一层 transport 适配器升档。
const (
	// LevelStandard HTTPS + X-Aegis-App-Key 头，纯 JSON。默认档。
	LevelStandard = "standard"
	// LevelSigned 额外要求 HMAC-SHA256 请求签名（防篡改 / 防重放）。
	LevelSigned = "signed"
	// LevelSealed 额外要求 Transport v2 端到端加密载荷。
	LevelSealed = "sealed"
)

// SecurityLevels 按防护强度从弱到强排列，控制台与校验共用同一份顺序。
var SecurityLevels = []string{LevelStandard, LevelSigned, LevelSealed}

// ValidSecurityLevel 判定等级取值是否受支持。
func ValidSecurityLevel(level string) bool {
	return slices.Contains(SecurityLevels, level)
}

// 认证方式。login 与 register 的可选集合刻意不同：
// 第三方登录能否自动建号由每个渠道自己的 allowRegister 决定，
// 若在这里再开一个 oauth 注册开关，同一件事就有两处配置，接入方无从判断哪个生效。
const (
	MethodPassword = "password"
	MethodSMS      = "sms"
	MethodOAuth    = "oauth"
)

var (
	// LoginMethods 应用可启用的登录方式。
	LoginMethods = []string{MethodPassword, MethodSMS, MethodOAuth}
	// RegisterMethods 应用可启用的注册方式。
	RegisterMethods = []string{MethodPassword, MethodSMS}
)

func ValidLoginMethod(method string) bool {
	return slices.Contains(LoginMethods, method)
}

func ValidRegisterMethod(method string) bool {
	return slices.Contains(RegisterMethods, method)
}

type RegistrationField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Mutable     bool   `json:"mutable"`
	Label       string `json:"label,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

type Policy struct {
	AppID                  int64               `json:"appId"`
	ProtocolVersion        string              `json:"protocolVersion"`
	Identifiers            []string            `json:"identifiers"`
	LoginMethods           []string            `json:"loginMethods"`
	RegisterMethods        []string            `json:"registerMethods"`
	RegistrationSchema     []RegistrationField `json:"registrationSchema"`
	RequireCaptcha         bool                `json:"requireCaptcha"`
	AutoLogin              bool                `json:"autoLoginAfterRegister"`
	SecurityLevel          string              `json:"securityLevel"`
	AllowLegacy            bool                `json:"allowLegacy"`
	SigningSecretSet       bool                `json:"signingSecretSet"`
	SigningSecretHint      string              `json:"signingSecretHint,omitempty"`
	SigningSecretRotatedAt *time.Time          `json:"signingSecretRotatedAt,omitempty"`
	CreatedAt              time.Time           `json:"createdAt"`
	UpdatedAt              time.Time           `json:"updatedAt"`
	// SigningSecretCipher 仅在仓储与服务之间流转，绝不出网。
	SigningSecretCipher string `json:"-"`
}

type PolicyPatch struct {
	Identifiers        []string            `json:"identifiers"`
	LoginMethods       []string            `json:"loginMethods"`
	RegisterMethods    []string            `json:"registerMethods"`
	RegistrationSchema []RegistrationField `json:"registrationSchema"`
	RequireCaptcha     *bool               `json:"requireCaptcha"`
	AutoLogin          *bool               `json:"autoLoginAfterRegister"`
	SecurityLevel      *string             `json:"securityLevel"`
	AllowLegacy        *bool               `json:"allowLegacy"`
}

type TransportKey struct {
	ID               int64      `json:"-"`
	AppID            int64      `json:"-"`
	KeyID            string     `json:"keyId"`
	Algorithm        string     `json:"algorithm"`
	PublicKey        []byte     `json:"-"`
	PrivateKeyCipher string     `json:"-"`
	Status           string     `json:"status"`
	NotBefore        time.Time  `json:"notBefore"`
	NotAfter         time.Time  `json:"notAfter"`
	CreatedAt        time.Time  `json:"createdAt"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
}

type PublicTransportKey struct {
	KeyID     string    `json:"keyId"`
	Algorithm string    `json:"algorithm"`
	PublicKey string    `json:"publicKey"`
	Status    string    `json:"status"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
}

// ─────────────────────────────────────────────────────────────────────
// GET /api/v1/apps/{appKey}/config —— 接入方唯一需要先拉的东西
// ─────────────────────────────────────────────────────────────────────

// AppBrief 应用公开身份，不含任何内部数字主键。
type AppBrief struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Status bool   `json:"status"`
}

// AuthCapability 描述"这个应用怎么登录、怎么注册"。
type AuthCapability struct {
	Identifiers        []string                     `json:"identifiers"`
	LoginMethods       []string                     `json:"loginMethods"`
	RegisterMethods    []string                     `json:"registerMethods"`
	RegistrationSchema []RegistrationField          `json:"registrationSchema"`
	Captcha            CaptchaRequirement           `json:"captcha"`
	AutoLogin          bool                         `json:"autoLoginAfterRegister"`
	RegisterEnabled    bool                         `json:"registerEnabled"`
	LoginEnabled       bool                         `json:"loginEnabled"`
	OAuthProviders     []oauthdomain.PublicProvider `json:"oauthProviders"`
}

// CaptchaRequirement 各入口到底要不要图形验证码 —— 服务端算好的**结论**。
//
// 之所以下发结论而不是配置，是因为这件事在服务端由三处独立开关共同决定：
//
//	Policy.RequireCaptcha                 接入协议策略上的强制开关（无视场景）
//	CaptchaAppConfig.RequireForLogin/…    应用验证码配置的分场景开关
//	config.Captcha.SMS.RequireCaptcha     平台级的短信前置图形验证码（防轰炸）
//
// 三者是"或"的关系，且分别属于三个管理入口。之前 /config 只下发了第一处，
// 于是「策略里没开、但应用验证码配置要求登录验证码」这种再普通不过的组合，
// 客户端根本无从知道要带验证码 —— 表现是登录直接被拒，而登录页上什么都没显示。
//
// 接入方不该去理解这三处的关系，也不该把判断复制一遍（复制就意味着服务端改了
// 之后客户端不跟着改）。这里给的是「调这个接口要不要先取验证码」的直接答案。
//
// 结论必须与真实执行点一致，这条由 auth_protocol_captcha_test.go 双向钉死。
type CaptchaRequirement struct {
	// Login 调 /auth/login 前是否必须先取验证码并回填 captchaId/captchaAnswer。
	Login bool `json:"login"`
	// Register 调 /auth/register 前是否必须先取验证码。
	Register bool `json:"register"`
	// SMS 调 /auth/sms/code 前是否必须先取图形验证码。
	//
	// 这一项由平台级配置决定，不区分 login / register —— 如实反映现状，
	// 而不是造两个恒等的字段让接入方以为它们会不同。
	SMS bool `json:"sms"`
}

// Required 返回某个入口是否需要验证码，入口名与客户端调用的接口一一对应。
func (c CaptchaRequirement) Required(entry string) bool {
	switch entry {
	case CaptchaEntryLogin:
		return c.Login
	case CaptchaEntryRegister:
		return c.Register
	case CaptchaEntrySMS:
		return c.SMS
	default:
		return false
	}
}

const (
	CaptchaEntryLogin    = "login"
	CaptchaEntryRegister = "register"
	CaptchaEntrySMS      = "sms"
)

// SignatureSpec signed 档需要的签名规格，字段齐全到可以直接照着实现。
type SignatureSpec struct {
	Scheme          string `json:"scheme"`
	Header          string `json:"header"`
	TimestampHeader string `json:"timestampHeader"`
	NonceHeader     string `json:"nonceHeader"`
	// Version 是当前应当使用的签名版本前缀（`v2=`）。
	Version string `json:"version"`
	// Canonical 是待签名字符串的构造模板，逐字节可读。
	Canonical string `json:"canonical"`
	// CanonicalLegacy 是 v1 模板，仅供仍在用旧签名的客户端对照；
	// 带 query string 的请求用它会被拒（40176）。
	CanonicalLegacy string `json:"canonicalLegacy"`
	MaxClockSkew    int    `json:"maxClockSkewSeconds"`
}

// TransportSpec sealed 档需要的端到端加密规格。
type TransportSpec struct {
	Protocol     string               `json:"protocol"`
	Algorithms   []string             `json:"algorithms"`
	ActiveKeyID  string               `json:"activeKeyId"`
	PublicKeys   []PublicTransportKey `json:"publicKeys"`
	MaxClockSkew int                  `json:"maxClockSkewSeconds"`
	ReplayWindow int                  `json:"replayWindowSeconds"`
	// HKDFSalt 说明密钥派生盐的构造方式，避免接入方靠猜。
	HKDFSalt string `json:"hkdfSalt"`
	// PayloadParam 无请求体方法（GET / DELETE / HEAD）承载密文的查询参数名。
	PayloadParam string `json:"payloadParam"`
	// BodylessMethods 明确列出走 query 承载的方法，客户端不必自己推断。
	BodylessMethods []string `json:"bodylessMethods"`
	// PlainContentTypeHeader 非 JSON 载荷（上传 / 下载）用它携带原始 Content-Type。
	PlainContentTypeHeader string `json:"plainContentTypeHeader"`
}

// ServerTime 供客户端校准时钟。
//
// signed / sealed 档最常见的接入故障就是移动设备时钟偏差超过 5 分钟后一路 40071，
// 而客户端根本不知道自己慢了。/config 免包装可读，因此它是唯一能在
// 「还没签成功」时拿到服务端时间的地方：客户端存下偏移量，之后所有请求带上校准后的时间戳。
type ServerTime struct {
	Unix int64  `json:"unix"`
	ISO  string `json:"iso"`
}

// Limits 客户端在发请求前就该知道的硬上限，省掉「传大了才知道」的一轮往返。
type Limits struct {
	MaxRequestBytes  int64 `json:"maxRequestBytes"`
	MaxUploadBytes   int64 `json:"maxUploadBytes"`
	ClockSkewSeconds int   `json:"clockSkewSeconds"`
	NonceMinLength   int   `json:"nonceMinLength"`
	NonceMaxLength   int   `json:"nonceMaxLength"`
}

// 客户端拿到错误码之后该怎么办。生成式 SDK 据此把业务码映射成可分类的异常，
// 而不是把 message 拿去做字符串匹配。
const (
	// RecoveryNone 无法自动恢复，必须由用户或接入方介入。
	RecoveryNone = "none"
	// RecoveryRefreshConfig 重新拉 /config 后重试一次（密钥轮换窗口）。
	RecoveryRefreshConfig = "refresh-config"
	// RecoverySyncClock 用 /config 的 serverTime 校准时钟后重试。
	RecoverySyncClock = "sync-clock"
	// RecoveryNewNonce 换一个新的随机 nonce 重发。
	RecoveryNewNonce = "new-nonce"
	// RecoveryRefreshToken 用 refreshToken 换一次新的 accessToken 后重试。
	RecoveryRefreshToken = "refresh-token"
	// RecoveryReauth 会话已不可恢复，回到登录页。
	RecoveryReauth = "reauth"
)

// ErrorDescriptor 机器可读的错误码目录条目。
type ErrorDescriptor struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Message string `json:"message"`
	// Recovery 见上面 Recovery* 常量。
	Recovery string `json:"recovery"`
	Hint     string `json:"hint,omitempty"`
}

// Operation 是 /config 下发的接口目录条目。
//
// Endpoints 那张 map 只有「键 → 路径」，缺了方法与是否需要 Bearer，
// 客户端还得回头查文档。Operation 把这两件事一起给出，
// 生成式 SDK 与调试工具可以直接照着构造请求。
type Operation struct {
	Key    string `json:"key"`
	Method string `json:"method"`
	Path   string `json:"path"`
	// Auth 为 true 表示必须带用户 Bearer 令牌。
	Auth bool `json:"auth"`
	// Unwrapped 为 true 表示该路径在任何安全等级下都免包装（仅 config 与 oauth 回跳）。
	Unwrapped bool `json:"unwrapped,omitempty"`
	// Upload 为 true 表示 multipart/form-data 上传。
	Upload bool `json:"upload,omitempty"`
	// Summary 一句话说明，直接进生成代码的注释。
	Summary string `json:"summary"`
}

// SecuritySpec 只下发当前等级真正需要的内容：standard 档两个指针都是 nil，
// 客户端不会看到任何用不上的密码学参数。
type SecuritySpec struct {
	Level        string         `json:"level"`
	AppKeyHeader string         `json:"appKeyHeader"`
	Signature    *SignatureSpec `json:"signature,omitempty"`
	Transport    *TransportSpec `json:"transport,omitempty"`
}

// Config 是 /config 的响应体。Endpoints 直接给出完整相对路径，
// 客户端不需要自己拼 appKey，也就不会拼错。
type Config struct {
	ProtocolVersion string         `json:"protocolVersion"`
	App             AppBrief       `json:"app"`
	Auth            AuthCapability `json:"auth"`
	Security        SecuritySpec   `json:"security"`
	// ServerTime 客户端据此校准时钟，见 ServerTime 的说明。
	ServerTime ServerTime        `json:"serverTime"`
	Limits     Limits            `json:"limits"`
	Endpoints  map[string]string `json:"endpoints"`
	// Operations 带方法与鉴权要求的完整接口目录。
	Operations []Operation `json:"operations"`
	// Errors 机器可读的错误码目录，供客户端把业务码映射成分类异常。
	Errors []ErrorDescriptor `json:"errors"`
}

// ─────────────────────────────────────────────────────────────────────
// 网关中间件用到的传输元数据
// ─────────────────────────────────────────────────────────────────────

type RequestMetadata struct {
	AppKey          string
	KeyID           string
	ClientPublicKey string
	Timestamp       string
	Nonce           string
	Method          string
	Path            string
	// PlainContentType sealed 档下非 JSON 载荷（上传）的原始 Content-Type。
	// 为空时按 JSON 处理，并沿用旧的合法性校验。
	PlainContentType string
}

// SignatureMetadata signed 档校验所需的全部输入。
type SignatureMetadata struct {
	AppKey    string
	Signature string
	Timestamp string
	Nonce     string
	Method    string
	Path      string
	// Query 是原样的 query string（不含 `?`），v2 签名把它算进去。
	// 不排序、不重新编码：客户端签的就是它放到线上的那串字节，
	// 任何语言都能逐字节复现，不存在「谁的 URL 编码规则不一样」的坑。
	Query string
	Body  []byte
}

type CryptoContext struct {
	Key          []byte
	AppID        int64
	AppKey       string
	KeyID        string
	RequestNonce []byte
	RequestAAD   []byte
}

// ─────────────────────────────────────────────────────────────────────
// 认证输入
// ─────────────────────────────────────────────────────────────────────

// LoginInput 单一登录入口：method 决定用哪组字段，新增方式不再新增路由。
//
//	password —— account + password
//	sms      —— phone + code
//	oauth    —— 走 /auth/oauth/* 三个接口，不经由本结构
type LoginInput struct {
	Method   string `json:"method"`
	Account  string `json:"account"`
	Password string `json:"password"`
	Phone    string `json:"phone"`
	// Code 短信验证码
	Code          string `json:"code"`
	DeviceID      string `json:"deviceId"`
	Device        string `json:"device"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type RegisterInput struct {
	Method        string          `json:"method"`
	Account       string          `json:"account"`
	Password      string          `json:"password"`
	Phone         string          `json:"phone"`
	Code          string          `json:"code"`
	Nickname      string          `json:"nickname"`
	Profile       json.RawMessage `json:"profile,omitempty"`
	DeviceID      string          `json:"deviceId"`
	Device        string          `json:"device"`
	CaptchaID     string          `json:"captchaId"`
	CaptchaAnswer string          `json:"captchaAnswer"`
}

// SMSCodeInput 申请短信验证码。purpose 决定这串码只能用于登录还是注册。
type SMSCodeInput struct {
	Phone   string `json:"phone"`
	Purpose string `json:"purpose"`
	// 图形验证码用于防短信轰炸，是否必填取决于应用的验证码策略
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

// ─────────────────────────────────────────────────────────────────────
// 接入自检
// ─────────────────────────────────────────────────────────────────────

// SelfTestStep 单步自检结果；失败时 Hint 给出可直接照做的修复动作。
type SelfTestStep struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	OK         bool   `json:"ok"`
	Skipped    bool   `json:"skipped,omitempty"`
	DurationMS int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

type SelfTestResult struct {
	OK            bool           `json:"ok"`
	SecurityLevel string         `json:"securityLevel"`
	BaseURL       string         `json:"baseUrl"`
	Steps         []SelfTestStep `json:"steps"`
	StartedAt     time.Time      `json:"startedAt"`
	DurationMS    int64          `json:"durationMs"`
}
