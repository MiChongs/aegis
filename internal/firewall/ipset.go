package firewall

import (
	"fmt"
	"net/netip"
	"strings"

	"go4.org/netipx"
)

// IPSet 不可变的网段集合（go4.org/netipx：区间合并后二分查找，规模再大也是 O(log n)，
// 取代旧实现里逐条 prefix.Contains 的线性扫描）。
type IPSet struct {
	set   *netipx.IPSet
	count int
}

// ParseIPSet 解析 IP 与 CIDR 混合的清单；单个地址按 /32 或 /128 处理。
func ParseIPSet(values []string) (*IPSet, error) {
	var b netipx.IPSetBuilder
	count := 0
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, fmt.Errorf("网段 %q 无效: %w", value, err)
			}
			b.AddPrefix(prefix.Masked())
		} else {
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return nil, fmt.Errorf("IP %q 无效: %w", value, err)
			}
			b.Add(addr.Unmap())
		}
		count++
	}
	set, err := b.IPSet()
	if err != nil {
		return nil, err
	}
	return &IPSet{set: set, count: count}, nil
}

// Contains 地址是否落在集合里（IPv4 映射的 IPv6 先还原成 IPv4）。
func (s *IPSet) Contains(addr netip.Addr) bool {
	if s == nil || s.count == 0 {
		return false
	}
	return s.set.Contains(addr.Unmap())
}

// Empty 是否为空。
func (s *IPSet) Empty() bool { return s == nil || s.count == 0 }

// Len 清单条目数（不是合并后的区间数）。
func (s *IPSet) Len() int {
	if s == nil {
		return 0
	}
	return s.count
}
