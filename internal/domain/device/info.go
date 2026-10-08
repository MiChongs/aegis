package device

import (
	"context"
	"strings"
)

// Info 对外接口里的一台设备：设备字典的完整映射。
//
// 会话、登录记录、扫码登录的发起端都用它描述设备。登录时只保存客户端上报的原始型号与平台
// （Client），展示信息在**读取时**按字典现查 —— 管理员修正或补录一条字典后，
// 已有会话与历史记录立刻显示新名称，不必等用户重新登录。
type Info struct {
	// Name 展示名：命中字典时为「厂商 营销名」，否则为客户端上报的名称，再否则为 UA 推断
	Name string `json:"name"`
	// Identifier 客户端上报的原始型号（Android Build.MODEL 如 SM-G998B，iOS 机器标识如 iPhone14,3）
	Identifier string `json:"identifier,omitempty"`
	// Platform android / ios / harmonyos / windows / macos / linux / web，未知时为空
	Platform            string `json:"platform,omitempty"`
	MarketingName       string `json:"marketingName,omitempty"`
	Manufacturer        string `json:"manufacturer,omitempty"`
	ManufacturerIconURL string `json:"manufacturerIconUrl,omitempty"`
	DeviceImageURL      string `json:"deviceImageUrl,omitempty"`
	// Brand / Codename / OS / OSVersion / AppVersion 客户端上报的补充信息，原样给出。
	// Manufacturer 在命中字典时取字典，未命中时取客户端上报
	Brand      string `json:"brand,omitempty"`
	Codename   string `json:"codename,omitempty"`
	OS         string `json:"os,omitempty"`
	OSVersion  string `json:"osVersion,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	// DictionaryID 命中的字典条目，控制台据此直达编辑
	DictionaryID int64 `json:"dictionaryId,omitempty"`
	// Matched 是否命中设备字典
	Matched bool `json:"matched"`
	// Source 展示名的来源：dictionary / client / user_agent / unknown
	Source string `json:"source"`
}

// 展示名来源
const (
	InfoSourceDictionary = "dictionary"
	InfoSourceClient     = "client"
	InfoSourceUserAgent  = "user_agent"
	InfoSourceUnknown    = "unknown"
)

// DescribeInput 解析一台设备所需的原始信息，字段都可以为空。
type DescribeInput struct {
	// Model 客户端上报的原始型号（新会话才有）
	Model string
	// Platform 客户端声明或登录时推断的平台
	Platform string
	DeviceID string
	// Name 已保存的设备名（旧会话里是登录时翻译过的名称，也可能就是原始型号）
	Name      string
	UserAgent string
	// Extra 客户端上报的补充信息（厂商、品牌、代号、系统、App 版本），旧会话没有
	Extra Extra
}

// Client 登录请求里客户端上报的原始设备信息，经请求上下文从传输层带到会话签发处。
type Client struct {
	Model    string
	Platform string
	Extra    Extra
}

// Extra 型号与平台之外的设备信息。都来自客户端自报，只用于展示与区分设备，不参与任何安全判定。
//
// 请求头（均可选）：X-Device-Manufacturer / X-Device-Brand / X-Device-Codename /
// X-Device-OS / X-Device-OS-Version / X-App-Version。
type Extra struct {
	// Manufacturer 厂商（Android Build.MANUFACTURER，如 Xiaomi）
	Manufacturer string `json:"manufacturer,omitempty"`
	// Brand 品牌（Android Build.BRAND，如 Redmi），同一厂商下的子品牌靠它区分
	Brand string `json:"brand,omitempty"`
	// Codename 设备代号（Android Build.DEVICE，如 houji），也作为字典的第二个查询键
	Codename string `json:"codename,omitempty"`
	// OS 系统名（Android / iOS / HarmonyOS / Windows …）
	OS string `json:"os,omitempty"`
	// OSVersion 系统版本（Android Build.VERSION.RELEASE，如 15）
	OSVersion string `json:"osVersion,omitempty"`
	// AppVersion 客户端应用版本
	AppVersion string `json:"appVersion,omitempty"`
}

// extraFieldLimit 单个字段的长度上限。都是客户端自报的展示信息，截断比拒绝请求合适
const extraFieldLimit = 64

func clip(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > extraFieldLimit {
		// 按字节截断后回退到合法的 UTF-8 边界
		value = strings.ToValidUTF8(value[:extraFieldLimit], "")
	}
	return value
}

// Normalize 去空白、截断超长值。
func (e Extra) Normalize() Extra {
	return Extra{
		Manufacturer: clip(e.Manufacturer),
		Brand:        clip(e.Brand),
		Codename:     clip(e.Codename),
		OS:           clip(e.OS),
		OSVersion:    clip(e.OSVersion),
		AppVersion:   clip(e.AppVersion),
	}
}

// IsZero 一项都没有。
func (e Extra) IsZero() bool { return e == Extra{} }

// Ref 有内容时返回指针，便于挂到 omitempty 的可选字段上。
func (e Extra) Ref() *Extra {
	e = e.Normalize()
	if e.IsZero() {
		return nil
	}
	return &e
}

// Value 从可选指针取值，nil 为零值。
func (e *Extra) Value() Extra {
	if e == nil {
		return Extra{}
	}
	return *e
}

type clientKey struct{}

// WithClient 把原始设备信息挂到上下文上。空值不覆盖已有的。
func WithClient(ctx context.Context, client Client) context.Context {
	client.Model = clip(client.Model)
	client.Platform = NormalizePlatform(client.Platform)
	client.Extra = client.Extra.Normalize()
	if client.Model == "" && client.Platform == "" && client.Extra.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, clientKey{}, client)
}

// ClientFrom 取出原始设备信息，没有时为零值。
func ClientFrom(ctx context.Context) Client {
	if ctx == nil {
		return Client{}
	}
	client, _ := ctx.Value(clientKey{}).(Client)
	return client
}

// NormalizePlatform 把各端五花八门的平台写法收敛成固定取值，不认识的返回空串。
func NormalizePlatform(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "android":
		return PlatformAndroid
	case "ios", "iphone", "ipad", "ipados":
		return PlatformIOS
	case "harmony", "harmonyos", "ohos", "openharmony":
		return "harmonyos"
	case "windows", "win", "win32", "win64":
		return "windows"
	case "macos", "mac", "osx", "darwin":
		return "macos"
	case "linux":
		return "linux"
	case "web", "browser", "h5":
		return "web"
	}
	return ""
}

// PlatformFromUserAgent 从 UA 推断平台。判不出返回空串。
func PlatformFromUserAgent(ua string) string {
	lower := strings.ToLower(ua)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "harmonyos") || strings.Contains(lower, "openharmony"):
		return "harmonyos"
	case strings.Contains(lower, "android"):
		return PlatformAndroid
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ipod") || strings.Contains(lower, "cfnetwork"):
		return PlatformIOS
	case strings.Contains(lower, "windows"):
		return "windows"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os"):
		return "macos"
	case strings.Contains(lower, "linux"):
		return "linux"
	}
	return ""
}

// GuessFromUserAgent 从 UA 粗略推断设备描述（如「Chrome on Windows」），用作没有任何上报名称时的兜底。
func GuessFromUserAgent(ua string) string {
	if ua == "" {
		return ""
	}
	lower := strings.ToLower(ua)

	os := ""
	switch {
	case strings.Contains(lower, "android"):
		os = "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ios") || strings.Contains(lower, "ipad"):
		os = "iOS"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os"):
		os = "macOS"
	case strings.Contains(lower, "windows"):
		os = "Windows"
	case strings.Contains(lower, "linux"):
		os = "Linux"
	}

	browser := ""
	switch {
	case strings.Contains(lower, "micromessenger"):
		browser = "WeChat"
	case strings.Contains(lower, "mqqbrowser"), strings.Contains(lower, "qqbrowser"):
		browser = "QQ Browser"
	case strings.Contains(lower, "edg/"):
		browser = "Edge"
	case strings.Contains(lower, "opr/"), strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome"):
		browser = "Chrome"
	case strings.Contains(lower, "safari"):
		browser = "Safari"
	}

	switch {
	case os != "" && browser != "":
		return browser + " on " + os
	case os != "":
		return os
	case browser != "":
		return browser
	}
	if len(ua) > 80 {
		return ua[:80]
	}
	return ua
}

// DisplayName 字典条目的展示名：「厂商 营销名」，营销名里已含厂商时不重复。
func (m *MarketingName) DisplayName() string {
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m.MarketingName)
	maker := strings.TrimSpace(m.Manufacturer)
	switch {
	case name == "":
		return maker
	case maker == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(maker)):
		return name
	}
	return maker + " " + name
}
