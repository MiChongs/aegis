package regip

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"go4.org/netipx"
)

// Address 一次注册的来源地址，以及按策略口径推导出的各个统计主体。
type Address struct {
	// Addr 规整后的地址（IPv4 映射的 IPv6 地址还原成 IPv4）
	Addr netip.Addr
	// Host 「单个地址」主体：IPv4 是 /32，IPv6 是 Network.IPv6Host 前缀
	Host netip.Prefix
	// Subnet 「网段」主体
	Subnet netip.Prefix
}

// Valid 是否拿到了可用的公网或内网地址。
func (a Address) Valid() bool { return a.Addr.IsValid() }

// ParseAddress 解析并规整注册 IP。带端口、带方括号、带区域标识的写法都认。
func ParseAddress(raw string, network Network) (Address, bool) {
	addr, ok := ParseIP(raw)
	if !ok {
		return Address{}, false
	}
	return AddressOf(addr, network), true
}

// ParseIP 只解析地址本身。
func ParseIP(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if addrPort, err := netip.ParseAddrPort(raw); err == nil {
		raw = addrPort.Addr().String()
	}
	raw = strings.Trim(raw, "[]")
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}

// AddressOf 按策略口径推导统计主体。
func AddressOf(addr netip.Addr, network Network) Address {
	addr = addr.Unmap().WithZone("")
	hostBits, subnetBits := 32, network.IPv4Subnet
	if addr.Is6() {
		hostBits, subnetBits = network.IPv6Host, network.IPv6Subnet
	}
	host, _ := addr.Prefix(hostBits)
	subnet, _ := addr.Prefix(subnetBits)
	return Address{Addr: addr, Host: host, Subnet: subnet}
}

// Prefix 某个统计主体对应的网段；city / app 没有网段。
func (a Address) Prefix(scope string) (netip.Prefix, bool) {
	switch scope {
	case ScopeIP:
		return a.Host, a.Host.IsValid()
	case ScopeSubnet:
		return a.Subnet, a.Subnet.IsValid()
	}
	return netip.Prefix{}, false
}

// SubjectKey 计数键里的主体部分。网段写成 CIDR，设备写成 device:<标识>，属地写成「国家/省/市」，应用就是 app。
// 返回空串表示这条规则这次无从统计（例如属地查不到、没有有效设备标识），不参与判定。
func SubjectKey(scope string, addr Address, dev Device, loc Location) string {
	switch scope {
	case ScopeIP, ScopeIPDevices:
		if addr.Host.IsValid() {
			return addr.Host.String()
		}
	case ScopeSubnet:
		if addr.Subnet.IsValid() {
			return addr.Subnet.String()
		}
	case ScopeDevice, ScopeDeviceIPs:
		if dev.Valid {
			return dev.Subject()
		}
	case ScopeCity:
		return loc.CityKey()
	case ScopeApp:
		return "app"
	}
	return ""
}

// MemberKey 去重型主体这一次要加入的成员：地址下的设备、设备下的地址。两边都要有才能数。
func MemberKey(scope string, addr Address, dev Device) string {
	switch scope {
	case ScopeIPDevices:
		if dev.Valid && addr.Host.IsValid() {
			return dev.ID
		}
	case ScopeDeviceIPs:
		if dev.Valid && addr.Host.IsValid() {
			return addr.Host.String()
		}
	}
	return ""
}

/* ───────────────────── 设备 ───────────────────── */

// Device 一次注册上报的设备标识。
type Device struct {
	// ID 规整后的标识（去空白、转小写）；Raw 是原样
	ID  string `json:"id,omitempty"`
	Raw string `json:"raw,omitempty"`
	// Valid 可以参与设备规则
	Valid bool `json:"valid"`
	// Invalid 上报了值但被判为无效，InvalidReason 说明为什么
	Invalid       bool   `json:"invalid,omitempty"`
	InvalidReason string `json:"invalidReason,omitempty"`
}

// Present 是否上报了值（有效或无效）。
func (d Device) Present() bool { return d.Valid || d.Invalid }

// Subject 设备作为统计 / 惩罚主体时的键。前缀与地址主体区分开，两边在同一个惩罚索引里不会撞。
func (d Device) Subject() string { return DeviceSubjectPrefix + d.ID }

// DeviceSubjectPrefix 设备主体的前缀。
const DeviceSubjectPrefix = "device:"

// MaxDeviceLength 设备标识最长多少字符。
const MaxDeviceLength = 128

// ParseDevice 规整并检查设备标识。占位值已在接入层（devicedomain.NormalizeID）当成没上报，
// 这里只看「像不像一台真实设备」：长度、可见字符、是不是同一个字符重复、是不是只有一两种字符。
func ParseDevice(raw string, minLength int) Device {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Device{}
	}
	if minLength <= 0 {
		minLength = 8
	}
	id := strings.ToLower(raw)
	out := Device{ID: id, Raw: raw}
	invalid := func(reason string) Device {
		out.Invalid, out.InvalidReason = true, reason
		return out
	}
	runes := []rune(id)
	switch {
	case len(runes) < minLength:
		return invalid(fmt.Sprintf("长度不足 %d", minLength))
	case len(runes) > MaxDeviceLength:
		return invalid(fmt.Sprintf("超过 %d 个字符", MaxDeviceLength))
	}
	distinct := map[rune]struct{}{}
	for _, r := range runes {
		if r <= 0x20 || r == 0x7f || r > 0x7e {
			return invalid("含空白、不可见或非 ASCII 字符")
		}
		switch r {
		case '-', ':', '_', '.', '{', '}':
			continue
		}
		distinct[r] = struct{}{}
	}
	if len(distinct) <= 2 {
		// aaaaaaaa、12121212、ffff-ffff：真实标识（UUID、Android ID、IDFV）几乎不可能只有两种字符
		return invalid("字符过于单一")
	}
	out.Valid = true
	return out
}

// DeviceListEntry 设备名单的一项。
type DeviceListEntry struct {
	ID        int64      `json:"id"`
	AppID     int64      `json:"appId"`
	DeviceID  string     `json:"deviceId"`
	Kind      string     `json:"kind"`
	Factor    float64    `json:"factor"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Hits      int64      `json:"hits"`
	LastHitAt *time.Time `json:"lastHitAt"`
	CreatedBy string     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// CompiledDeviceLists 设备名单：哈希表，O(1) 查。
type CompiledDeviceLists struct {
	entries map[string]DeviceListEntry
}

// CompileDeviceLists 编译生效中的设备名单项。
func CompileDeviceLists(entries []DeviceListEntry, now time.Time) *CompiledDeviceLists {
	out := &CompiledDeviceLists{entries: make(map[string]DeviceListEntry, len(entries))}
	for _, entry := range entries {
		if entry.ExpiresAt != nil && !entry.ExpiresAt.After(now) {
			continue
		}
		if !slices.Contains(ListKinds, entry.Kind) {
			continue
		}
		out.entries[strings.ToLower(strings.TrimSpace(entry.DeviceID))] = entry
	}
	return out
}

// Size 名单项数。
func (c *CompiledDeviceLists) Size() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

// Match 设备命中的名单项；没命中或设备无效返回 nil。
func (c *CompiledDeviceLists) Match(dev Device) *ListMatch {
	if c == nil || !dev.Valid {
		return nil
	}
	entry, ok := c.entries[dev.ID]
	if !ok {
		return nil
	}
	return &ListMatch{EntryID: entry.ID, Kind: entry.Kind, Target: ListTargetDevice, Device: entry.DeviceID, Factor: entry.Factor, Note: entry.Note}
}

// 名单命中的对象。
const (
	ListTargetIP     = "ip"
	ListTargetDevice = "device"
)

// CombineLists 地址名单与设备名单合并成一个结论：任一边拒绝 → 拒绝；否则任一边放行 → 放行；
// 否则取两边放宽倍数大的那个。地址在放行名单里但设备在拒绝名单里，按更危险的处理。
func CombineLists(ip, device *ListMatch) *ListMatch {
	pick := func(kind string) *ListMatch {
		for _, m := range []*ListMatch{ip, device} {
			if m != nil && m.Kind == kind {
				return m
			}
		}
		return nil
	}
	if m := pick(ListDeny); m != nil {
		return m
	}
	if m := pick(ListAllow); m != nil {
		return m
	}
	var best *ListMatch
	for _, m := range []*ListMatch{ip, device} {
		if m != nil && m.Kind == ListElevated && (best == nil || m.Factor > best.Factor) {
			best = m
		}
	}
	return best
}

// PrefixRange 网段的首末地址，给数据库按 btree 做范围扫描用：
// `ip >= first AND ip <= last` 能走 (appid, ip) 索引，`<<=` 在没有 GiST 索引时只能全扫。
func PrefixRange(prefix netip.Prefix) (netip.Addr, netip.Addr) {
	r := netipx.RangeOfPrefix(prefix.Masked())
	return r.From(), r.To()
}

// Location 判定用到的属地事实。
type Location struct {
	Country  string `json:"country,omitempty"`
	Province string `json:"province,omitempty"`
	City     string `json:"city,omitempty"`
	ISP      string `json:"isp,omitempty"`
	// Resolved 是否查到了属地；内网地址、接口失败、IPv6（宝塔接口不支持）都是 false
	Resolved bool `json:"resolved"`
}

// CityKey 「国家/省/市」，缺哪段就到哪段为止；国家都没有时返回空串。
func (l Location) CityKey() string {
	if !l.Resolved || strings.TrimSpace(l.Country) == "" {
		return ""
	}
	parts := []string{strings.TrimSpace(l.Country)}
	if p := strings.TrimSpace(l.Province); p != "" {
		parts = append(parts, p)
		if c := strings.TrimSpace(l.City); c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, "/")
}

// MatchRegion 属地是否落在名单项里：「中国」匹配全部中国地址，「中国/广东」只匹配广东。
// 省名按前缀比对，「广东」与「广东省」视为同一个。
func (l Location) MatchRegion(region string) bool {
	if !l.Resolved {
		return false
	}
	parts := strings.SplitN(region, "/", 2)
	if !sameRegionName(parts[0], l.Country) {
		return false
	}
	return len(parts) == 1 || sameRegionName(parts[1], l.Province)
}

func sameRegionName(want, got string) bool {
	want, got = strings.TrimSpace(want), strings.TrimSpace(got)
	if want == "" || got == "" {
		return false
	}
	if strings.EqualFold(want, got) {
		return true
	}
	trim := func(s string) string {
		for _, suffix := range []string{"省", "市", "自治区", "特别行政区", "壮族自治区", "回族自治区", "维吾尔自治区"} {
			s = strings.TrimSuffix(s, suffix)
		}
		return s
	}
	return trim(want) == trim(got)
}

// MatchISP 运营商名称里是否包含任一关键词（不分大小写），命中时返回那个关键词。
func MatchISP(isp string, keywords []string) (string, bool) {
	isp = strings.ToLower(strings.TrimSpace(isp))
	if isp == "" {
		return "", false
	}
	for _, keyword := range keywords {
		if strings.Contains(isp, strings.ToLower(keyword)) {
			return keyword, true
		}
	}
	return "", false
}

/* ───────────────────── 名单 ───────────────────── */

// 名单项类型。
const (
	// ListAllow 放行：不受任何规则与惩罚期约束（公司出口、学校机房）
	ListAllow = "allow"
	// ListDeny 拒绝：一律不许注册
	ListDeny = "deny"
	// ListElevated 放宽：配额乘以 Factor（大型 NAT 出口、校园网）
	ListElevated = "elevated"
)

// ListKinds 全部名单类型。
var ListKinds = []string{ListAllow, ListDeny, ListElevated}

// MaxListFactor 放宽倍数上限。
const MaxListFactor = 1000

// ListEntry 名单的一项：一个地址或一个网段。
type ListEntry struct {
	ID        int64      `json:"id"`
	AppID     int64      `json:"appId"`
	CIDR      string     `json:"cidr"`
	Kind      string     `json:"kind"`
	Factor    float64    `json:"factor"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Hits      int64      `json:"hits"`
	LastHitAt *time.Time `json:"lastHitAt"`
	CreatedBy string     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// Active 名单项此刻是否生效。
func (e ListEntry) Active(now time.Time) bool {
	return e.ExpiresAt == nil || e.ExpiresAt.After(now)
}

// ParsePrefix 名单值规整成网段：单个地址是 /32 或 /128，网段按掩码对齐（1.2.3.4/24 → 1.2.3.0/24）。
func ParsePrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, fmt.Errorf("地址不能为空")
	}
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("%q 不是有效的网段", raw)
		}
		addr := prefix.Addr()
		bits := prefix.Bits()
		if addr.Is4In6() && bits >= 96 {
			addr, bits = addr.Unmap(), bits-96
		}
		if addr.Is4() && bits < 8 || addr.Is6() && bits < 16 {
			return netip.Prefix{}, fmt.Errorf("%q 范围过大：IPv4 至少 /8、IPv6 至少 /16", raw)
		}
		return netip.PrefixFrom(addr.WithZone(""), bits).Masked(), nil
	}
	if strings.Contains(raw, "-") {
		return netip.Prefix{}, fmt.Errorf("%q：地址范围请写成网段（CIDR）", raw)
	}
	addr, ok := ParseIP(raw)
	if !ok {
		return netip.Prefix{}, fmt.Errorf("%q 不是有效的 IP 地址", raw)
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// ListMatch 地址命中的名单项。
type ListMatch struct {
	EntryID int64  `json:"entryId"`
	Kind    string `json:"kind"`
	// Target 命中的是地址名单还是设备名单
	Target string  `json:"target"`
	CIDR   string  `json:"cidr,omitempty"`
	Device string  `json:"device,omitempty"`
	Factor float64 `json:"factor,omitempty"`
	Note   string  `json:"note,omitempty"`
}

// CompiledLists 编译后的名单：放行与拒绝各一个 IPSet（有序区间表，二分查找 O(log n)），
// 放宽按前缀从长到短排好，取最具体的一项。热路径只读。
type CompiledLists struct {
	allow    *netipx.IPSet
	deny     *netipx.IPSet
	entries  []compiledEntry
	elevated []compiledEntry
	Size     int
}

type compiledEntry struct {
	prefix netip.Prefix
	entry  ListEntry
}

// CompileLists 把生效中的名单项编译成查找结构。无效项（被手工改坏的）跳过。
func CompileLists(entries []ListEntry, now time.Time) *CompiledLists {
	var allow, deny netipx.IPSetBuilder
	out := &CompiledLists{}
	for _, entry := range entries {
		if !entry.Active(now) {
			continue
		}
		prefix, err := ParsePrefix(entry.CIDR)
		if err != nil {
			continue
		}
		item := compiledEntry{prefix: prefix, entry: entry}
		switch entry.Kind {
		case ListAllow:
			allow.AddPrefix(prefix)
		case ListDeny:
			deny.AddPrefix(prefix)
		case ListElevated:
			out.elevated = append(out.elevated, item)
		default:
			continue
		}
		out.entries = append(out.entries, item)
		out.Size++
	}
	out.allow, _ = allow.IPSet()
	out.deny, _ = deny.IPSet()
	// 更具体的前缀优先；同样具体时更早建的优先，结果稳定
	byBits := func(a, b compiledEntry) int {
		if a.prefix.Bits() != b.prefix.Bits() {
			return b.prefix.Bits() - a.prefix.Bits()
		}
		return int(a.entry.ID - b.entry.ID)
	}
	slices.SortStableFunc(out.elevated, byBits)
	slices.SortStableFunc(out.entries, byBits)
	return out
}

// Match 地址命中的名单：拒绝优先于放行（同一地址既在拒绝又在放行里，按更危险的处理），
// 放行优先于放宽。没命中返回 nil。
func (c *CompiledLists) Match(addr netip.Addr) *ListMatch {
	if c == nil || !addr.IsValid() {
		return nil
	}
	if c.deny != nil && c.deny.Contains(addr) {
		return c.find(addr, ListDeny)
	}
	if c.allow != nil && c.allow.Contains(addr) {
		return c.find(addr, ListAllow)
	}
	for _, item := range c.elevated {
		if item.prefix.Contains(addr) {
			return toMatch(item)
		}
	}
	return nil
}

// find 在已知命中的类型里找出最具体的那一项，用来记命中次数与展示。
func (c *CompiledLists) find(addr netip.Addr, kind string) *ListMatch {
	for _, item := range c.entries {
		if item.entry.Kind == kind && item.prefix.Contains(addr) {
			return toMatch(item)
		}
	}
	return &ListMatch{Kind: kind, Target: ListTargetIP}
}

func toMatch(item compiledEntry) *ListMatch {
	return &ListMatch{EntryID: item.entry.ID, Kind: item.entry.Kind, Target: ListTargetIP, CIDR: item.prefix.String(), Factor: item.entry.Factor, Note: item.entry.Note}
}
