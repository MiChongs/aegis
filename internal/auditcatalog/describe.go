package auditcatalog

import (
	"regexp"
	"strings"
)

// Display 一条审计日志的展示字段。
type Display struct {
	Known         bool   // 目录里有这个接口
	OperationName string // 「上传安装包」
	Module        string // 「release」
	ModuleLabel   string // 「发布中心」
	TargetType    string // 「安装包」
	TargetLabel   string // 「voyage-1.0.apk」「#12」
	Kind          string
	Severity      string
}

// legacyModules 重构前写入的 category 到模块的映射：目录回填之前的旧行据此显示中文模块。
var legacyModules = map[string]string{
	"auth": "auth", "profile": "account", "account": "account",
	"admin": "admin", "rbac": "admin", "org": "org", "organization": "org",
	"app": "app", "user": "user", "content": "content", "release": "release",
	"storage": "storage", "workflow": "platform", "settings": "platform", "monitor": "platform",
	"audit": "platform", "api": "platform", "system": "platform", "platform": "platform",
	"security": "security", "risk": "security", "firewall": "security",
	"email": "notify", "notify": "notify", "notification": "notify",
	"payment": "commerce", "points": "commerce", "signin": "commerce", "lottery": "commerce", "vip": "commerce",
	"ticket": "support", "tickets": "support", "function": "function", "ai": "function", "report": "report",
}

// ModuleFor 把任意 category（新模块键或重构前的旧分类）归到模块键。
func ModuleFor(category string) string {
	if _, ok := moduleLabels[category]; ok {
		return category
	}
	if module, ok := legacyModules[strings.ToLower(strings.TrimSpace(category))]; ok {
		return module
	}
	return "platform"
}

// appParamKeys 应用标识不算操作对象：应用另有一列展示。
var appParamKeys = map[string]bool{"appkey": true, "appid": true, "app_id": true}

// Describe 由方法、路由与写入时记下的对象信息推导展示字段。
// resourceID 是中间件写入的路由参数串（"appkey=…,rid=12"），handler 也可能写入单个 ID。
func Describe(method, route, resourceID, targetName string) Display {
	display := Display{TargetLabel: strings.TrimSpace(targetName)}
	if op, ok := Lookup(method, route); ok {
		display.Known = true
		display.OperationName = op.Name
		display.Module = op.Module
		display.TargetType = op.Target
		display.Kind = op.Kind
		display.Severity = op.Severity
	}
	display.ModuleLabel = ModuleLabel(display.Module)
	if display.TargetLabel == "" && (display.TargetType != "" || !display.Known) {
		display.TargetLabel = targetFromResourceID(resourceID)
	}
	return display
}

func targetFromResourceID(resourceID string) string {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" || strings.HasPrefix(resourceID, "/") {
		return ""
	}
	if !strings.Contains(resourceID, "=") {
		return labelForID(resourceID)
	}
	last := ""
	for _, part := range strings.Split(resourceID, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || appParamKeys[strings.ToLower(strings.TrimSpace(key))] {
			continue
		}
		last = strings.TrimSpace(value)
	}
	return labelForID(last)
}

var numericID = regexp.MustCompile(`^\d+$`)

func labelForID(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return ""
	case numericID.MatchString(value):
		return "#" + value
	case len([]rune(value)) > 40:
		return string([]rune(value)[:12]) + "…"
	default:
		return value
	}
}

var (
	reEdge    = regexp.MustCompile(`Edg(?:e|A|iOS)?/(\d+)`)
	reOpera   = regexp.MustCompile(`OPR/(\d+)`)
	reChrome  = regexp.MustCompile(`(?:Chrome|CriOS)/(\d+)`)
	reFirefox = regexp.MustCompile(`(?:Firefox|FxiOS)/(\d+)`)
	reSafari  = regexp.MustCompile(`Version/(\d+)(?:\.\d+)?.*Safari/`)
	reAndroid = regexp.MustCompile(`Android (\d+)`)
	reIOS     = regexp.MustCompile(`(?:iPhone|CPU) OS (\d+)`)
	reWindows = regexp.MustCompile(`Windows NT (\d+\.\d+)`)
)

// ParseUserAgent 粗略识别浏览器与系统，只为一眼看出「从哪儿操作的」，不追求覆盖所有客户端。
func ParseUserAgent(ua string) (browser, system string) {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "", ""
	}
	match := func(re *regexp.Regexp, name string) string {
		if m := re.FindStringSubmatch(ua); m != nil {
			return name + " " + m[1]
		}
		return ""
	}
	for _, try := range []func() string{
		func() string { return match(reEdge, "Edge") },
		func() string { return match(reOpera, "Opera") },
		func() string { return match(reFirefox, "Firefox") },
		func() string { return match(reChrome, "Chrome") },
		func() string { return match(reSafari, "Safari") },
	} {
		if browser = try(); browser != "" {
			break
		}
	}
	if browser == "" {
		lower := strings.ToLower(ua)
		switch {
		case strings.Contains(lower, "curl"):
			browser = "curl"
		case strings.Contains(lower, "postman"):
			browser = "Postman"
		case strings.Contains(lower, "okhttp"):
			browser = "OkHttp"
		case strings.Contains(lower, "go-http-client"):
			browser = "Go 客户端"
		case strings.Contains(lower, "python"):
			browser = "Python 客户端"
		}
	}
	switch {
	case reAndroid.MatchString(ua):
		system = "Android " + reAndroid.FindStringSubmatch(ua)[1]
	case strings.Contains(ua, "iPad"):
		system = "iPadOS"
	case reIOS.MatchString(ua) && strings.Contains(ua, "iPhone"):
		system = "iOS " + reIOS.FindStringSubmatch(ua)[1]
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		system = "macOS"
	case reWindows.MatchString(ua):
		system = map[string]string{"10.0": "Windows 10/11", "6.3": "Windows 8.1", "6.1": "Windows 7"}[reWindows.FindStringSubmatch(ua)[1]]
		if system == "" {
			system = "Windows"
		}
	case strings.Contains(ua, "CrOS"):
		system = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		system = "Linux"
	}
	return browser, system
}
