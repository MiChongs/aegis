package engine

import (
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"aegis/services/risk/internal/bundle"

	uaparser "github.com/mileusna/useragent"
)

// Device 客户端上报的设备要素
type Device struct {
	ID         string            `json:"id"`
	Platform   string            `json:"platform,omitempty"`
	Brand      string            `json:"brand,omitempty"`
	Model      string            `json:"model,omitempty"`
	OSVersion  string            `json:"osVersion,omitempty"`
	AppVersion string            `json:"appVersion,omitempty"`
	Screen     string            `json:"screen,omitempty"`
	Locale     string            `json:"locale,omitempty"`
	Timezone   string            `json:"timezone,omitempty"`
	Emulator   bool              `json:"emulator,omitempty"`
	Rooted     bool              `json:"rooted,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

// Geo 网络情报
type Geo struct {
	Country    string `json:"country,omitempty"`
	Province   string `json:"province,omitempty"`
	City       string `json:"city,omitempty"`
	ISP        string `json:"isp,omitempty"`
	ASN        int64  `json:"asn,omitempty"`
	Proxy      bool   `json:"proxy,omitempty"`
	VPN        bool   `json:"vpn,omitempty"`
	Tor        bool   `json:"tor,omitempty"`
	Datacenter bool   `json:"datacenter,omitempty"`
	IPScore    int    `json:"ipScore,omitempty"`
}

// Request 一次评估的输入（与 RPC 契约解耦，测试与模拟直接构造）
type Request struct {
	Scene         string `json:"scene"`
	AppID         int64  `json:"appId"`
	UserID        int64  `json:"userId,omitempty"`
	IdentityID    int64  `json:"identityId,omitempty"`
	Account       string `json:"account,omitempty"`
	IP            string `json:"ip,omitempty"`
	UserAgent     string `json:"userAgent,omitempty"`
	Device        Device `json:"device"`
	Geo           Geo    `json:"geo"`
	CaptchaPassed bool   `json:"captchaPassed,omitempty"`
	// CaptchaUnavailable 这个应用拿不到图形验证码：验证码处置转人工复核
	CaptchaUnavailable bool           `json:"captchaUnavailable,omitempty"`
	RequestID          string         `json:"requestId,omitempty"`
	Phone              string         `json:"phone,omitempty"`
	Email              string         `json:"email,omitempty"`
	TargetUserID       int64          `json:"targetUserId,omitempty"`
	Amount             float64        `json:"amount,omitempty"`
	Extra              map[string]any `json:"extra,omitempty"`
	Now                time.Time      `json:"now"`
}

// DeviceEnv 表达式里的 device.*
type DeviceEnv struct {
	ID         string `expr:"id" json:"id"`
	Platform   string `expr:"platform" json:"platform"`
	Brand      string `expr:"brand" json:"brand"`
	Model      string `expr:"model" json:"model"`
	OSVersion  string `expr:"os_version" json:"os_version"`
	AppVersion string `expr:"app_version" json:"app_version"`
	Emulator   bool   `expr:"emulator" json:"emulator"`
	Rooted     bool   `expr:"rooted" json:"rooted"`
	Present    bool   `expr:"present" json:"present"`
}

// ClientEnv 表达式里的 client.*（User-Agent 解析）
type ClientEnv struct {
	Browser  string `expr:"browser" json:"browser"`
	Version  string `expr:"version" json:"version"`
	OS       string `expr:"os" json:"os"`
	Mobile   bool   `expr:"mobile" json:"mobile"`
	Bot      bool   `expr:"bot" json:"bot"`
	Headless bool   `expr:"headless" json:"headless"`
	Empty    bool   `expr:"empty" json:"empty"`
	Length   int    `expr:"length" json:"length"`
}

// NetEnv 表达式里的 net.*
type NetEnv struct {
	Country    string `expr:"country" json:"country"`
	Province   string `expr:"province" json:"province"`
	City       string `expr:"city" json:"city"`
	ISP        string `expr:"isp" json:"isp"`
	ASN        int64  `expr:"asn" json:"asn"`
	Proxy      bool   `expr:"proxy" json:"proxy"`
	VPN        bool   `expr:"vpn" json:"vpn"`
	Tor        bool   `expr:"tor" json:"tor"`
	Datacenter bool   `expr:"datacenter" json:"datacenter"`
	Score      int    `expr:"score" json:"score"`
	IPv6       bool   `expr:"ipv6" json:"ipv6"`
	Private    bool   `expr:"private" json:"private"`
	Subnet     string `expr:"subnet" json:"subnet"`
}

// ListEnv 表达式里的 list.*
type ListEnv struct {
	Block bool     `expr:"block" json:"block"`
	Allow bool     `expr:"allow" json:"allow"`
	Watch bool     `expr:"watch" json:"watch"`
	Types []string `expr:"types" json:"types"` // 命中的名单类型
}

// Env 规则、派生特征看到的全部事实。用具名结构而不是 map：保存规则时就能做编译期校验，
// 写错变量名是当场的编译错误，而不是永远不命中的静默规则。
type Env struct {
	Scene         string             `expr:"scene" json:"scene"`
	AppID         int64              `expr:"app_id" json:"app_id"`
	UserID        int64              `expr:"user_id" json:"user_id"`
	Account       string             `expr:"account" json:"account"`
	IP            string             `expr:"ip" json:"ip"`
	DeviceID      string             `expr:"device_id" json:"device_id"`
	UA            string             `expr:"ua" json:"ua"`
	Phone         string             `expr:"phone" json:"phone"`
	PhonePrefix   string             `expr:"phone_prefix" json:"phone_prefix"`
	Email         string             `expr:"email" json:"email"`
	EmailDomain   string             `expr:"email_domain" json:"email_domain"`
	Amount        float64            `expr:"amount" json:"amount"`
	TargetUserID  int64              `expr:"target_user_id" json:"target_user_id"`
	CaptchaPassed bool               `expr:"captcha_passed" json:"captcha_passed"`
	CaptchaReady  bool               `expr:"captcha_available" json:"captcha_available"`
	LoggedIn      bool               `expr:"logged_in" json:"logged_in"`
	Hour          int                `expr:"hour" json:"hour"`
	Weekday       int                `expr:"weekday" json:"weekday"` // 1=周一 … 7=周日
	Device        DeviceEnv          `expr:"device" json:"device"`
	Client        ClientEnv          `expr:"client" json:"client"`
	Net           NetEnv             `expr:"net" json:"net"`
	List          ListEnv            `expr:"list" json:"list"`
	Extra         map[string]any     `expr:"extra" json:"extra"`
	F             map[string]float64 `expr:"F" json:"F"` // 特征
	M             map[string]float64 `expr:"M" json:"M"` // 模型分
}

// Variable 变量目录的一项（控制台上的变量提示）
type Variable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

// Variables 表达式变量目录。TestVariablesMatchEnv 钉死它与 Env 的字段一一对应。
func Variables() []Variable {
	return []Variable{
		{"scene", "string", "场景"}, {"app_id", "int", "应用 ID"}, {"user_id", "int", "用户 ID（未登录为 0）"},
		{"account", "string", "登录名 / 邮箱 / 手机号"}, {"ip", "string", "客户端 IP"}, {"device_id", "string", "设备 ID"},
		{"ua", "string", "User-Agent"}, {"phone", "string", "手机号"}, {"phone_prefix", "string", "手机号段（前 7 位）"},
		{"email", "string", "邮箱"}, {"email_domain", "string", "邮箱域名"}, {"amount", "float", "金额"},
		{"target_user_id", "int", "对方用户 ID"}, {"captcha_passed", "bool", "已通过图形验证码"}, {"captcha_available", "bool", "应用能出图形验证码"}, {"logged_in", "bool", "已登录"},
		{"hour", "int", "小时（0-23，服务器时区）"}, {"weekday", "int", "星期（1=周一）"},
		{"device.id", "string", "设备 ID"}, {"device.platform", "string", "平台"}, {"device.brand", "string", "品牌"},
		{"device.model", "string", "型号"}, {"device.os_version", "string", "系统版本"}, {"device.app_version", "string", "应用版本"},
		{"device.emulator", "bool", "模拟器"}, {"device.rooted", "bool", "已 Root / 越狱"}, {"device.present", "bool", "带了设备信息"},
		{"client.browser", "string", "浏览器"}, {"client.version", "string", "浏览器版本"}, {"client.os", "string", "操作系统"},
		{"client.mobile", "bool", "移动端"}, {"client.bot", "bool", "爬虫"}, {"client.headless", "bool", "无头浏览器"},
		{"client.empty", "bool", "空 UA"}, {"client.length", "int", "UA 长度"},
		{"net.country", "string", "国家（ISO 代码）"}, {"net.province", "string", "省份"}, {"net.city", "string", "城市"},
		{"net.isp", "string", "运营商"}, {"net.asn", "int", "ASN"}, {"net.proxy", "bool", "代理"}, {"net.vpn", "bool", "VPN"},
		{"net.tor", "bool", "Tor"}, {"net.datacenter", "bool", "机房 IP"}, {"net.score", "int", "IP 情报分"},
		{"net.ipv6", "bool", "IPv6"}, {"net.private", "bool", "内网地址"}, {"net.subnet", "string", "网段（/24 或 /64）"},
		{"list.block", "bool", "命中黑名单"}, {"list.allow", "bool", "命中白名单"}, {"list.watch", "bool", "命中观察名单"},
		{"list.types", "[]string", "命中的名单类型"},
		{"extra", "map", "场景入参（extra.<字段>）"}, {"F", "map", "特征（F.<键>）"}, {"M", "map", "模型分（M.<键>）"},
	}
}

// Subnet 网段：IPv4 取 /24、IPv6 取 /64
func Subnet(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 64
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.String()
}

// PhonePrefix 手机号段：去掉国家码与符号后的前 7 位
func PhonePrefix(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if strings.HasPrefix(phone, "+86") || (len(d) == 13 && strings.HasPrefix(d, "86")) {
		d = strings.TrimPrefix(d, "86")
	}
	if len(d) < 7 {
		return d
	}
	return d[:7]
}

// EmailDomain 邮箱域名（小写）
func EmailDomain(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

var headlessMarkers = []string{"headlesschrome", "phantomjs", "puppeteer", "playwright", "selenium", "webdriver", "electron/"}

var botMarkers = []string{"bot", "spider", "crawl", "curl/", "wget/", "python-requests", "python-urllib", "okhttp/", "go-http-client", "java/", "httpclient", "axios/", "node-fetch", "libwww"}

func parseClient(ua string) ClientEnv {
	out := ClientEnv{Length: len(ua), Empty: strings.TrimSpace(ua) == ""}
	if out.Empty {
		return out
	}
	parsed := uaparser.Parse(ua)
	out.Browser = parsed.Name
	out.Version = parsed.Version
	out.OS = parsed.OS
	out.Mobile = parsed.Mobile || parsed.Tablet
	out.Bot = parsed.Bot
	lower := strings.ToLower(ua)
	for _, m := range headlessMarkers {
		if strings.Contains(lower, m) {
			out.Headless = true
			break
		}
	}
	if !out.Bot {
		for _, m := range botMarkers {
			if strings.Contains(lower, m) {
				out.Bot = true
				break
			}
		}
	}
	return out
}

// Normalize 补齐派生字段：时间、网段、号段、域名
func (r *Request) Normalize() {
	if r.Now.IsZero() {
		r.Now = time.Now()
	}
	r.Scene = strings.TrimSpace(r.Scene)
	r.IP = strings.TrimSpace(r.IP)
	r.Account = strings.TrimSpace(r.Account)
	r.Email = strings.TrimSpace(r.Email)
	if r.Email == "" && strings.Contains(r.Account, "@") {
		r.Email = r.Account
	}
	if r.Device.ID == "" {
		if v, ok := r.Extra["deviceId"].(string); ok {
			r.Device.ID = v
		}
	}
	r.Geo.Country = strings.ToUpper(strings.TrimSpace(r.Geo.Country))
}

// BuildEnv 从请求装配表达式环境（特征与模型分稍后填入）
func BuildEnv(r Request) Env {
	weekday := int(r.Now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	env := Env{
		Scene: r.Scene, AppID: r.AppID, UserID: r.UserID, Account: r.Account, IP: r.IP, DeviceID: r.Device.ID,
		UA: r.UserAgent, Phone: r.Phone, PhonePrefix: PhonePrefix(r.Phone), Email: r.Email, EmailDomain: EmailDomain(r.Email),
		Amount: r.Amount, TargetUserID: r.TargetUserID, CaptchaPassed: r.CaptchaPassed, CaptchaReady: !r.CaptchaUnavailable, LoggedIn: r.UserID > 0,
		Hour: r.Now.Hour(), Weekday: weekday,
		Device: DeviceEnv{
			ID: r.Device.ID, Platform: strings.ToLower(r.Device.Platform), Brand: r.Device.Brand, Model: r.Device.Model,
			OSVersion: r.Device.OSVersion, AppVersion: r.Device.AppVersion, Emulator: r.Device.Emulator, Rooted: r.Device.Rooted,
			Present: r.Device.ID != "",
		},
		Client: parseClient(r.UserAgent),
		Net: NetEnv{
			Country: r.Geo.Country, Province: r.Geo.Province, City: r.Geo.City, ISP: r.Geo.ISP, ASN: r.Geo.ASN,
			Proxy: r.Geo.Proxy, VPN: r.Geo.VPN, Tor: r.Geo.Tor, Datacenter: r.Geo.Datacenter, Score: r.Geo.IPScore,
			Subnet: Subnet(r.IP),
		},
		Extra: r.Extra,
		F:     map[string]float64{},
		M:     map[string]float64{},
	}
	if env.Extra == nil {
		env.Extra = map[string]any{}
	}
	if addr, err := netip.ParseAddr(r.IP); err == nil {
		addr = addr.Unmap()
		env.Net.IPv6 = addr.Is6()
		env.Net.Private = addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()
	}
	return env
}

// EntityValue 特征主体在本次请求里的取值（空串 = 这次请求没有这个主体，特征取默认值）
func EntityValue(env *Env, entity string) string {
	switch entity {
	case bundle.EntityUser:
		if env.UserID > 0 {
			return itoa(env.UserID)
		}
	case bundle.EntityAccount:
		return strings.ToLower(env.Account)
	case bundle.EntityDevice:
		return env.DeviceID
	case bundle.EntityIP:
		return env.IP
	case bundle.EntityIPSubnet:
		return env.Net.Subnet
	case bundle.EntityPhone:
		return env.Phone
	case bundle.EntityEmailDomain:
		return env.EmailDomain
	case bundle.EntityApp:
		return itoa(env.AppID)
	}
	return ""
}

// asMap 把环境转成 zen / OPA 用的普通对象。手写而不是 JSON 往返：这是热路径，
// 而 OPA 对 map[string]any / []any / 基本类型之外的值会再走一遍反射序列化。
func (env *Env) asMap() map[string]any {
	types := make([]any, len(env.List.Types))
	for i, t := range env.List.Types {
		types[i] = t
	}
	return map[string]any{
		"scene": env.Scene, "app_id": env.AppID, "user_id": env.UserID, "account": env.Account, "ip": env.IP,
		"device_id": env.DeviceID, "ua": env.UA, "phone": env.Phone, "phone_prefix": env.PhonePrefix, "email": env.Email,
		"email_domain": env.EmailDomain, "amount": env.Amount, "target_user_id": env.TargetUserID,
		"captcha_passed": env.CaptchaPassed, "captcha_available": env.CaptchaReady, "logged_in": env.LoggedIn, "hour": env.Hour, "weekday": env.Weekday,
		"device": map[string]any{
			"id": env.Device.ID, "platform": env.Device.Platform, "brand": env.Device.Brand, "model": env.Device.Model,
			"os_version": env.Device.OSVersion, "app_version": env.Device.AppVersion, "emulator": env.Device.Emulator,
			"rooted": env.Device.Rooted, "present": env.Device.Present,
		},
		"client": map[string]any{
			"browser": env.Client.Browser, "version": env.Client.Version, "os": env.Client.OS, "mobile": env.Client.Mobile,
			"bot": env.Client.Bot, "headless": env.Client.Headless, "empty": env.Client.Empty, "length": env.Client.Length,
		},
		"net": map[string]any{
			"country": env.Net.Country, "province": env.Net.Province, "city": env.Net.City, "isp": env.Net.ISP, "asn": env.Net.ASN,
			"proxy": env.Net.Proxy, "vpn": env.Net.VPN, "tor": env.Net.Tor, "datacenter": env.Net.Datacenter, "score": env.Net.Score,
			"ipv6": env.Net.IPv6, "private": env.Net.Private, "subnet": env.Net.Subnet,
		},
		"list":  map[string]any{"block": env.List.Block, "allow": env.List.Allow, "watch": env.List.Watch, "types": types},
		"extra": plainValue(env.Extra),
		"F":     floatMap(env.F),
		"M":     floatMap(env.M),
	}
}

func floatMap(m map[string]float64) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// plainValue 任意 JSON 解出来的值已经是基本类型；其它类型走一次 JSON 往返
func plainValue(v any) any {
	switch x := v.(type) {
	case nil, bool, string, float64, int, int64, json.Number:
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plainValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plainValue(e)
		}
		return out
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// Fingerprint 设备指纹要素的扁平形态：事件、图谱、向量共用同一套键
func (d Device) Fingerprint() map[string]string {
	out := map[string]string{}
	put := func(k, v string) {
		if v != "" {
			out[k] = v
		}
	}
	put("platform", strings.ToLower(d.Platform))
	put("brand", d.Brand)
	put("model", d.Model)
	put("os_version", d.OSVersion)
	put("app_version", d.AppVersion)
	put("screen", d.Screen)
	put("locale", d.Locale)
	put("timezone", d.Timezone)
	if d.Emulator {
		out["emulator"] = "true"
	}
	if d.Rooted {
		out["rooted"] = "true"
	}
	for k, v := range d.Attrs {
		put("x."+k, v)
	}
	return out
}
