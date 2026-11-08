// Package proxyip 判断一个 IP 是不是代理 / CDN / 内网地址，而不是用户自己的出口。
//
// 主服务经 CDN 与 Traefik 接流量，历史上有一段时间把 CDN 节点、Traefik 的 overlay 地址记成了用户 IP；
// 这些地址会把成百上千个毫不相干的用户连成一团。写关系图（实时与回灌）前一律按这里排除。
//
// 范围来自 RISK_PROXY_CIDRS（逗号分隔，应与主服务的 TRUSTED_PROXIES 一致，另加代用户发请求的服务器出口），
// 内网、回环、链路本地、运营商级 NAT、IPv6 唯一本地地址总是排除。
package proxyip

import (
	"net/netip"
	"os"
	"strings"
	"sync"
)

// Set 一组网段
type Set struct{ prefixes []netip.Prefix }

var builtin = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "100.64.0.0/10", "0.0.0.0/8",
	"::1/128", "fc00::/7", "fe80::/10", "::/128",
}

// Parse 解析逗号 / 空白分隔的网段（单个 IP 按 /32 或 /128）；无法解析的项跳过
func Parse(list string) *Set {
	s := &Set{}
	for _, f := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == ';' }) {
		if p, err := netip.ParsePrefix(f); err == nil {
			s.prefixes = append(s.prefixes, p.Masked())
		} else if a, err := netip.ParseAddr(f); err == nil {
			s.prefixes = append(s.prefixes, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		}
	}
	return s
}

// Contains IP 是否落在任一网段里（无法解析的 IP 也当作不可用，返回 true）
func (s *Set) Contains(ip string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return true
	}
	a = a.Unmap()
	for _, p := range s.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Len 网段数
func (s *Set) Len() int { return len(s.prefixes) }

var (
	once    sync.Once
	current *Set
)

// Default 内置网段 + RISK_PROXY_CIDRS（进程内只读一次）
func Default() *Set {
	once.Do(func() {
		current = Parse(strings.Join(builtin, ",") + "," + os.Getenv("RISK_PROXY_CIDRS"))
	})
	return current
}

// IsProxy Default().Contains 的简写
func IsProxy(ip string) bool { return Default().Contains(ip) }
