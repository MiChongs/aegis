package clientip

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	cdnranges "github.com/realclientip/realclientip-go/ranges"
)

// 受信网段的预设名。
//
// 预设存在的理由是「把一串记不住的 CIDR 换成一个说得出口的名字」：
// TRUSTED_PROXIES=cloudflare 的意图一眼可读，而同样内容的 22 行 CIDR
// 半年后没人敢动 —— 不知道它当初为什么在那里，也不知道还准不准。
const (
	// PresetInfra 基础设施网段：环回 + RFC1918 + 链路本地 + CGNAT + IPv6 ULA。
	//
	// 这是默认值，因为它精确描述了「反代与本服务之间那一段网络」在容器平台上的样子：
	// Zeabur / Kubernetes / Docker Compose / ECS 里，入口网关到业务容器这一跳
	// 一定落在这些网段内。而对方是公网地址时它一条都不匹配 —— 直连公网的部署
	// 因此不会因为这个默认值多信任任何东西。
	PresetInfra = "infra"
	// PresetPrivate 仅 RFC1918 与 IPv6 ULA。
	PresetPrivate = "private"
	// PresetLoopback 仅环回。反代与本服务同机（含 Cloudflare Tunnel sidecar）时够用。
	PresetLoopback = "loopback"
	// PresetLinkLocal 链路本地。云厂商的元数据与部分 sidecar 走这一段。
	PresetLinkLocal = "link-local"
	// PresetCGNAT 运营商级 NAT（RFC 6598）。Kubernetes 与多数 PaaS 的 Pod 网段常落在这里。
	PresetCGNAT = "cgnat"
	// PresetCloudflare Cloudflare 的边缘出口网段。
	//
	// 站在 Cloudflare 后面时**必须**加它：CF 边缘是公网地址，不加的话
	// 「从右往左找第一个不受信条目」会停在 CF 边缘上，于是全站用户的 IP
	// 变成一小撮 Cloudflare 机房地址。
	PresetCloudflare = "cloudflare"
	// PresetGCPLoadBalancer Google 外部负载均衡与健康检查的固定出口段。
	PresetGCPLoadBalancer = "gcp-lb"
	// PresetAll 信任一切。**转发头因此完全可伪造**，只在纯内网、
	// 且确定不会有人直连本服务时才有意义。启动时会告警。
	PresetAll = "all"
	// PresetNone 不信任任何网段，等价于 strategy=peer。
	PresetNone = "none"
)

var presetRanges = map[string][]string{
	PresetPrivate: {
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"fc00::/7",       // RFC 4193 唯一本地地址
	},
	PresetLoopback: {
		"127.0.0.0/8", // RFC 1122
		"::1/128",     // RFC 4291
	},
	PresetLinkLocal: {
		"169.254.0.0/16", // RFC 3927
		"fe80::/10",      // RFC 4291
	},
	PresetCGNAT: {
		"100.64.0.0/10", // RFC 6598
	},
	PresetGCPLoadBalancer: {
		// Google 官方公布的外部 LB / 健康检查出口段，长期稳定。
		"35.191.0.0/16",
		"130.211.0.0/22",
	},
	PresetAll: {
		"0.0.0.0/0",
		"::/0",
	},
	PresetNone: {},
}

// presetAliases 常见的另一种叫法，避免「写对了意思、写错了字面」这类
// 只在运行时才暴露的配置错误。
var presetAliases = map[string]string{
	"rfc1918":     PresetPrivate,
	"internal":    PresetInfra,
	"kubernetes":  PresetInfra,
	"k8s":         PresetInfra,
	"docker":      PresetInfra,
	"localhost":   PresetLoopback,
	"linklocal":   PresetLinkLocal,
	"rfc6598":     PresetCGNAT,
	"carrier-nat": PresetCGNAT,
	"cf":          PresetCloudflare,
	"gcp":         PresetGCPLoadBalancer,
	"google-lb":   PresetGCPLoadBalancer,
	"any":         PresetAll,
	"*":           PresetAll,
}

// DefaultTrustedProxies 未配置 TRUSTED_PROXIES 时的默认受信集合。
//
// 从「谁都不信」改成「信任基础设施网段」是这次修复的核心：前者在任何容器平台上
// 都会让全站用户共用入口网关那一个 IP，而限流、封禁、地理风控、审计全都建立在
// 客户端 IP 上 —— 它们不会报错，只是从此对所有人一视同仁地失效。
func DefaultTrustedProxies() []string {
	return []string{PresetInfra}
}

// PresetNames 全部可用预设名（排序），用于错误提示与文档。
func PresetNames() []string {
	names := make([]string, 0, len(presetRanges)+2)
	names = append(names, PresetInfra, PresetCloudflare)
	for name := range presetRanges {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lookupPreset 展开一个预设名。第二个返回值说明这个名字是不是预设。
func lookupPreset(name string) ([]string, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := presetAliases[key]; ok {
		key = alias
	}
	switch key {
	case PresetInfra:
		items := make([]string, 0, 8)
		items = append(items, presetRanges[PresetPrivate]...)
		items = append(items, presetRanges[PresetLoopback]...)
		items = append(items, presetRanges[PresetLinkLocal]...)
		items = append(items, presetRanges[PresetCGNAT]...)
		return items, true
	case PresetCloudflare:
		// 直接用 realclientip 随库发布的那份清单，不在本仓库里再抄一份 ——
		// 抄一份就意味着有两处会过期，而过期的那一处不会报错，只会悄悄算错 IP。
		return append([]string(nil), cdnranges.Cloudflare...), true
	}
	items, ok := presetRanges[key]
	if !ok {
		return nil, false
	}
	return append([]string(nil), items...), true
}

// resolveTrustedEntries 把「预设名 + CIDR + 裸 IP」混排的配置展开成网段列表。
//
// 写错一个字符就静默少信任一个网段，是这类配置最典型的坑，因此这里对无法识别的
// 条目一律返回 error（启动即失败），而不是跳过 —— 少信任一个网段的表现是
// 「线上大部分请求的 IP 突然变成网关地址」，从现象倒查回配置文件要很久。
func resolveTrustedEntries(entries []string) ([]netip.Prefix, error) {
	var (
		prefixes []netip.Prefix
		seen     = make(map[string]struct{}, len(entries))
	)
	appendPrefix := func(prefix netip.Prefix) {
		key := prefix.String()
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if expanded, isPreset := lookupPreset(entry); isPreset {
			for _, item := range expanded {
				prefix, err := parsePrefixEntry(item)
				if err != nil {
					return nil, fmt.Errorf("预设 %s 内含非法网段 %q: %w", entry, item, err)
				}
				appendPrefix(prefix)
			}
			continue
		}
		prefix, err := parsePrefixEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES 里的 %q 既不是网段/IP 也不是预设名（可用预设：%s）: %w",
				entry, strings.Join(PresetNames(), " / "), err)
		}
		appendPrefix(prefix)
	}
	return prefixes, nil
}

// parsePrefixEntry 解析一条网段或单个地址。
//
// 顺带把「主机位没清零」的写法（10.0.0.1/8）纠正成网段本身：netip.ParsePrefix
// 接受它但 Contains 只按掩码比对，而 netip.Prefix.Masked 之前的值打印出来会
// 误导读日志的人 —— 让它显示成 10.0.0.0/8 才是它真正的含义。
func parsePrefixEntry(entry string) (netip.Prefix, error) {
	value := strings.TrimSpace(entry)
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}
