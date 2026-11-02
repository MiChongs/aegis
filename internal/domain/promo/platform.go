package promo

import (
	"net/url"
	"slices"
	"strings"
)

// Platform 一个可以发文推广的平台。
type Platform struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Hint 用户端的发文提示
	Hint string `json:"hint"`
	// Domains 认可的文章链接域名（按标签边界后缀匹配）
	Domains []string `json:"domains"`
	// PackageName 安卓包名，用来直接拉起这个 App 分享；公众号这类在后台发文的为空
	PackageName string `json:"packageName,omitempty"`
	// DefaultOn 新建活动时默认启用
	DefaultOn bool `json:"defaultOn"`
}

// ForbiddenPlatform 写死的禁用平台：任何活动都不认、控制台关不掉。
type ForbiddenPlatform struct {
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

var platforms = []Platform{
	{Key: "wechat_mp", Name: "微信公众号", Hint: "在公众号后台发布文章后，复制文章链接提交",
		Domains: []string{"mp.weixin.qq.com"}, DefaultOn: true},
	{Key: "xiaohongshu", Name: "小红书", Hint: "发布笔记后，点分享并复制链接提交",
		Domains: []string{"xiaohongshu.com", "xhslink.com"}, PackageName: "com.xingin.xhs", DefaultOn: true},
	{Key: "coolapk", Name: "酷安", Hint: "发布动态或图文后，点分享并复制链接提交",
		Domains: []string{"coolapk.com", "coolapk1s.com"}, PackageName: "com.coolapk.market", DefaultOn: true},
	{Key: "zhihu", Name: "知乎", Hint: "发布文章或回答后，复制链接提交",
		Domains: []string{"zhihu.com"}, PackageName: "com.zhihu.android"},
	{Key: "tieba", Name: "百度贴吧", Hint: "发帖后复制帖子链接提交",
		Domains: []string{"tieba.baidu.com"}, PackageName: "com.baidu.tieba"},
	{Key: "weibo", Name: "微博", Hint: "发布微博后复制链接提交",
		Domains: []string{"weibo.com", "weibo.cn"}, PackageName: "com.sina.weibo"},
}

var forbiddenPlatforms = []ForbiddenPlatform{
	{Name: "抖音", Domains: []string{"douyin.com", "iesdouyin.com", "amemv.com", "douyinvod.com", "douyinpic.com"}},
	{Name: "抖音火山版", Domains: []string{"huoshan.com"}},
}

// DefaultBlockedDomains 新建活动时默认的额外禁用域名（快手系），运营可以改。
func DefaultBlockedDomains() []string {
	return []string{"kuaishou.com", "gifshow.com", "kwai.com", "chenzhongtech.com"}
}

// PlatformCatalog 全部可选平台，顺序即展示顺序。
func PlatformCatalog() []Platform {
	out := make([]Platform, len(platforms))
	for index, platform := range platforms {
		platform.Domains = slices.Clone(platform.Domains)
		out[index] = platform
	}
	return out
}

// ForbiddenPlatforms 写死的禁用平台。
func ForbiddenPlatforms() []ForbiddenPlatform {
	out := make([]ForbiddenPlatform, len(forbiddenPlatforms))
	for index, platform := range forbiddenPlatforms {
		platform.Domains = slices.Clone(platform.Domains)
		out[index] = platform
	}
	return out
}

// ForbiddenNames 禁用平台的名称（用户端明示用）。
func ForbiddenNames() []string {
	out := make([]string, 0, len(forbiddenPlatforms))
	for _, platform := range forbiddenPlatforms {
		out = append(out, platform.Name)
	}
	return out
}

// FindPlatform 按键取平台。
func FindPlatform(key string) (Platform, bool) {
	for _, platform := range platforms {
		if platform.Key == key {
			platform.Domains = slices.Clone(platform.Domains)
			return platform, true
		}
	}
	return Platform{}, false
}

// PlatformName 平台名称，未登记的原样返回键。
func PlatformName(key string) string {
	if platform, ok := FindPlatform(key); ok {
		return platform.Name
	}
	return key
}

// DefaultPlatforms 默认启用的平台键。
func DefaultPlatforms() []string {
	out := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		if platform.DefaultOn {
			out = append(out, platform.Key)
		}
	}
	return out
}

// hostMatches 主机是否是 domain 或它的子域（按标签边界）：www.xiaohongshu.com 算，evilxiaohongshu.com 不算。
func hostMatches(host, domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func anyHostMatches(host string, domains []string) bool {
	for _, domain := range domains {
		if hostMatches(host, domain) {
			return true
		}
	}
	return false
}

// IsForbiddenHost 主机属于写死的禁用平台。
func IsForbiddenHost(host string) bool {
	host = strings.ToLower(host)
	for _, platform := range forbiddenPlatforms {
		if anyHostMatches(host, platform.Domains) {
			return true
		}
	}
	return false
}

// Link 解析后的文章链接。
type Link struct {
	URL  string
	Host string
	// Key 判重用：主机 + 路径（公众号 /s 另带文章标识）
	Key string
}

// ParseLink 解析并校验一个文章链接是否属于 platform、是否被禁用。
// 返回的 reason 为空表示可用。先查禁用：抖音的链接无论选了哪个平台都按「禁止」报，而不是「不属于这个平台」。
func ParseLink(raw string, platform Platform, blocked []string) (Link, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxURLLength {
		return Link{}, ReasonLinkInvalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return Link{}, ReasonLinkInvalid
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || !strings.Contains(host, ".") {
		return Link{}, ReasonLinkInvalid
	}
	if IsForbiddenHost(host) || anyHostMatches(host, blocked) {
		return Link{}, ReasonLinkForbidden
	}
	if !anyHostMatches(host, platform.Domains) {
		return Link{}, ReasonLinkPlatformMismatch
	}
	keyHost := strings.TrimPrefix(host, "www.")
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	key := keyHost + path
	if keyHost == "mp.weixin.qq.com" && path == "/s" {
		// 公众号的老式文章链接 /s?__biz=..&mid=..&idx=..&sn=..：路径相同，文章由查询串区分
		query := parsed.Query()
		parts := make([]string, 0, 4)
		for _, name := range []string{"__biz", "mid", "idx", "sn"} {
			if value := query.Get(name); value != "" {
				parts = append(parts, name+"="+value)
			}
		}
		if len(parts) == 0 {
			return Link{}, ReasonLinkInvalid
		}
		key += "?" + strings.Join(parts, "&")
	}
	if path == "" {
		// 只有域名没有路径的不是一篇文章（小红书短链也带路径）
		return Link{}, ReasonLinkInvalid
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return Link{URL: parsed.String(), Host: host, Key: key}, ""
}

// NormalizeDomain 清洗一个运营填写的域名：去协议、路径、端口与前导点，转小写。
func NormalizeDomain(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if index := strings.Index(value, "://"); index >= 0 {
		value = value[index+3:]
	}
	if index := strings.IndexAny(value, "/?#"); index >= 0 {
		value = value[:index]
	}
	if index := strings.LastIndex(value, ":"); index >= 0 {
		value = value[:index]
	}
	return strings.Trim(value, ".")
}
