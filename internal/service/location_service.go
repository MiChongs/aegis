package service

import (
	"aegis/pkg/egress"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

type IPLocation struct {
	IP          string         `json:"ip"`
	Country     string         `json:"country"`
	CountryCode string         `json:"countryCode,omitempty"`
	Region      string         `json:"region"`
	City        string         `json:"city"`
	District    string         `json:"district,omitempty"`
	Location    string         `json:"location"`
	Timezone    string         `json:"timezone,omitempty"`
	ISP         string         `json:"isp,omitempty"`
	Coordinates *GeoCoordinate `json:"coordinates,omitempty"`
	Network     NetworkInfo    `json:"network"`
	Source      string         `json:"source"`
	ResolvedAt  time.Time      `json:"resolvedAt"`
	IsPrivate   bool           `json:"isPrivate"`
}

type GeoCoordinate struct {
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
}

type NetworkInfo struct {
	Type         string `json:"type,omitempty"`
	Organization string `json:"organization,omitempty"`
	ASN          string `json:"asn,omitempty"`
}

type cachedLocation struct {
	value     IPLocation
	expiresAt time.Time
}

// LocationService 的定位数据来自远程归属地接口：美团、宝塔面板、UAPIs，按 GEOIP_PROVIDERS
// 排优先级合并（见 location_provider.go），进程内与 Redis 两级缓存挡住重复查询。
type LocationService struct {
	log                *zap.Logger
	redis              *redislib.Client
	keyPrefix          string
	http               *http.Client
	enabled            bool
	group              singleflight.Group
	mu                 sync.RWMutex
	local              map[string]cachedLocation
	localTTL           time.Duration
	redisTTL           time.Duration
	failureTTL         time.Duration
	errorTTL           time.Duration
	cacheLookupTimeout time.Duration
	cacheWriteTimeout  time.Duration
	refreshTimeout     time.Duration
	providerTimeout    time.Duration
	sources            []*locationSource
	uapisKey           string
	uapisSource        string
}

// defaultLocationProviders 美团优先：国内地址到区县且有区划代码；宝塔补运营商并兜底 IPv4；
// UAPIs 有额度，只补前两个没查出来的（IPv6 主要靠它）。
var defaultLocationProviders = []string{"meituan", "btcn", "uapis"}

func NewLocationService(log *zap.Logger, redisClient *redislib.Client, keyPrefix string, geoCfg config.GeoIPConfig) *LocationService {
	if log == nil {
		log = zap.NewNop()
	}
	s := &LocationService{
		log:       log,
		redis:     redisClient,
		keyPrefix: strings.TrimSpace(keyPrefix),
		http:      egress.NewClient(egress.Profile{Name: "location.lookup", Timeout: 3 * time.Second}),
		enabled:   geoCfg.Enabled,
		local:     make(map[string]cachedLocation),
		localTTL:  10 * time.Minute,
		// 归属地很少变动，远程查询比本地库贵，Redis 留久一些
		redisTTL:           72 * time.Hour,
		failureTTL:         6 * time.Hour,
		errorTTL:           time.Minute,
		cacheLookupTimeout: 40 * time.Millisecond,
		cacheWriteTimeout:  250 * time.Millisecond,
		refreshTimeout:     4 * time.Second,
		providerTimeout:    2500 * time.Millisecond,
	}
	s.uapisKey = strings.TrimSpace(geoCfg.UAPIsKey)
	s.uapisSource = geoCfg.UAPIsSource
	switch s.uapisSource {
	case "":
		if s.uapisKey != "" {
			s.uapisSource = uapisSourceCommercial
		}
	case "standard", "default":
		s.uapisSource = ""
	}
	s.sources = s.buildSources(geoCfg)
	return s
}

func (s *LocationService) buildSources(geoCfg config.GeoIPConfig) []*locationSource {
	names := geoCfg.Providers
	if len(names) == 0 {
		names = defaultLocationProviders
	}
	qps := geoCfg.UAPIsQPS
	if qps <= 0 {
		qps = 4
	}
	ipv4 := func(addr netip.Addr) bool { return addr.Unmap().Is4() }
	sources := make([]*locationSource, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if seen[name] {
			continue
		}
		seen[name] = true
		switch name {
		case "meituan":
			sources = append(sources, &locationSource{name: name, strictCN: true, supports: ipv4, single: s.lookupMeituan, concurrency: 8})
		case "btcn", "bt":
			sources = append(sources, &locationSource{name: "btcn", supports: ipv4, batch: s.lookupBtCN, batchSize: btcnBatchSize})
		case "uapis":
			sources = append(sources, &locationSource{name: name, metered: true, single: s.lookupUAPIs, concurrency: 4,
				limiter: rate.NewLimiter(rate.Limit(qps), 1)})
		default:
			s.log.Warn("unknown location provider ignored", zap.String("provider", raw))
		}
	}
	return sources
}

func (s *LocationService) Close() {}

func (s *LocationService) CacheLookupTimeout() time.Duration {
	return s.cacheLookupTimeout
}

func (s *LocationService) DefaultLocation(ip string) IPLocation {
	ip = sanitizeIP(ip)
	now := time.Now().UTC()
	if ip == "" {
		return IPLocation{
			IP:         "",
			Location:   "",
			Source:     "default",
			ResolvedAt: now,
		}
	}

	addr, err := netip.ParseAddr(ip)
	if err == nil && isPrivateAddr(addr) {
		return IPLocation{
			IP:         ip,
			Location:   "内网地址",
			Source:     "private",
			ResolvedAt: now,
			IsPrivate:  true,
		}
	}

	return IPLocation{
		IP:         ip,
		Location:   "",
		Source:     "default",
		ResolvedAt: now,
	}
}

// GetCached 只读缓存，不发远程请求。
func (s *LocationService) GetCached(ctx context.Context, ip string) (IPLocation, bool) {
	ip = sanitizeIP(ip)
	if ip == "" {
		return IPLocation{}, false
	}
	if loc, ok := s.getLocal(ip); ok {
		return loc, true
	}
	if s.redis != nil {
		raw, err := s.redis.Get(ctx, s.redisKey(ip)).Bytes()
		if err == nil {
			var loc IPLocation
			if err := json.Unmarshal(raw, &loc); err != nil {
				s.log.Warn("location redis decode failed", zap.String("ip", ip), zap.Error(err))
			} else {
				if loc.IP == "" {
					loc.IP = ip
				}
				if loc.ResolvedAt.IsZero() {
					loc.ResolvedAt = time.Now().UTC()
				}
				s.setLocal(ip, loc, s.localTTL)
				return loc, true
			}
		} else if err != redislib.Nil && ctx.Err() == nil {
			s.log.Debug("location redis lookup failed", zap.String("ip", ip), zap.Error(err))
		}
	}
	return IPLocation{}, false
}

func (s *LocationService) Resolve(ctx context.Context, ip string) IPLocation {
	ip = sanitizeIP(ip)
	if ip == "" {
		return s.DefaultLocation(ip)
	}
	if loc, ok := s.GetCached(ctx, ip); ok {
		return loc
	}

	value, _, _ := s.group.Do(ip, func() (any, error) {
		if loc, ok := s.GetCached(ctx, ip); ok {
			return loc, nil
		}
		return s.lookupAndStore(ctx, []string{ip})[ip], nil
	})

	loc, _ := value.(IPLocation)
	if loc.IP == "" {
		return s.DefaultLocation(ip)
	}
	return loc
}

// ResolveMany 批量解析：缓存未命中的 IP 合并成一次远程请求。
func (s *LocationService) ResolveMany(ctx context.Context, ips []string) map[string]IPLocation {
	out := make(map[string]IPLocation, len(ips))
	misses := make([]string, 0, len(ips))
	for _, raw := range ips {
		ip := sanitizeIP(raw)
		if ip == "" {
			continue
		}
		if _, ok := out[ip]; ok {
			continue
		}
		if loc, ok := s.GetCached(ctx, ip); ok {
			out[ip] = loc
			continue
		}
		out[ip] = s.DefaultLocation(ip)
		misses = append(misses, ip)
	}
	for ip, loc := range s.lookupAndStore(ctx, misses) {
		out[ip] = loc
	}
	// 调用方按原始写法取值，带端口等写法也要能命中
	for _, raw := range ips {
		if _, ok := out[raw]; !ok {
			if loc, ok := out[sanitizeIP(raw)]; ok {
				out[raw] = loc
			}
		}
	}
	return out
}

func (s *LocationService) RefreshAsync(ip string) {
	ip = sanitizeIP(ip)
	if ip == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.refreshTimeout)
		defer cancel()
		_ = s.Resolve(ctx, ip)
	}()
}

// lookupAndStore 远程查询并写缓存。内网、非法地址不发请求；
// 网络失败只在进程内短暂记住，避免接口故障时每个请求都去撞超时。
func (s *LocationService) lookupAndStore(ctx context.Context, ips []string) map[string]IPLocation {
	out := make(map[string]IPLocation, len(ips))
	queries := make(map[string]string, len(ips))
	for _, ip := range ips {
		addr, err := netip.ParseAddr(ip)
		if err != nil || isPrivateAddr(addr) || !s.enabled {
			out[ip] = s.DefaultLocation(ip)
			continue
		}
		queries[ip] = addr.Unmap().String()
	}
	if len(queries) == 0 {
		return out
	}

	unique := make([]string, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		if _, ok := seen[query]; !ok {
			seen[query] = struct{}{}
			unique = append(unique, query)
		}
	}

	results, incomplete := s.lookupSources(ctx, unique)

	for ip, query := range queries {
		loc, ok := results[query]
		if !ok {
			out[ip] = s.DefaultLocation(ip)
			if incomplete[query] {
				s.setLocal(ip, out[ip], s.errorTTL)
			} else {
				s.persist(ip, out[ip], s.failureTTL)
			}
			continue
		}
		loc.IP = ip
		ttl := s.redisTTL
		if !loc.isResolved() {
			ttl = s.failureTTL
		}
		s.persist(ip, loc, ttl)
		out[ip] = loc
	}
	return out
}

func (s *LocationService) persist(ip string, loc IPLocation, ttl time.Duration) {
	s.setLocal(ip, loc, s.localTTL)
	if s.redis == nil {
		return
	}
	raw, err := json.Marshal(loc)
	if err != nil {
		s.log.Warn("location cache marshal failed", zap.String("ip", ip), zap.Error(err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cacheWriteTimeout)
	defer cancel()
	if err := s.redis.Set(ctx, s.redisKey(ip), raw, ttl).Err(); err != nil {
		s.log.Debug("location redis write failed", zap.String("ip", ip), zap.Error(err))
	}
}

func (s *LocationService) redisKey(ip string) string {
	if s.keyPrefix == "" {
		return "location:v4:ip:" + ip
	}
	return s.keyPrefix + ":location:v4:ip:" + ip
}

func (s *LocationService) getLocal(ip string) (IPLocation, bool) {
	s.mu.RLock()
	item, ok := s.local[ip]
	s.mu.RUnlock()
	if !ok {
		return IPLocation{}, false
	}
	if time.Now().After(item.expiresAt) {
		s.mu.Lock()
		delete(s.local, ip)
		s.mu.Unlock()
		return IPLocation{}, false
	}
	return item.value, true
}

func (s *LocationService) setLocal(ip string, loc IPLocation, ttl time.Duration) {
	s.mu.Lock()
	s.local[ip] = cachedLocation{
		value:     loc,
		expiresAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()
}

func (l IPLocation) isResolved() bool {
	return strings.TrimSpace(l.Location) != "" ||
		strings.TrimSpace(l.Country) != "" ||
		strings.TrimSpace(l.Region) != "" ||
		strings.TrimSpace(l.City) != "" ||
		strings.TrimSpace(l.ISP) != ""
}

func sanitizeIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	return strings.TrimSpace(raw)
}

func isPrivateAddr(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
}

func normalizeString(value string) string {
	return strings.TrimSpace(value)
}

func normalizeCountry(country, countryCode string) (string, string) {
	country = strings.TrimSpace(country)
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" && len(country) == 2 {
		countryCode = strings.ToUpper(country)
	}
	if country == "" || len(country) == 2 {
		switch countryCode {
		case "CN":
			return "中国", "CN"
		case "US":
			return "美国", "US"
		case "JP":
			return "日本", "JP"
		case "KR":
			return "韩国", "KR"
		case "HK":
			return "中国香港", "HK"
		case "MO":
			return "中国澳门", "MO"
		case "TW":
			return "中国台湾", "TW"
		}
	}
	return country, countryCode
}

func normalizeTimezone(country, timezone string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone != "" {
		return timezone
	}
	country = strings.ToLower(strings.TrimSpace(country))
	if country == "cn" || strings.Contains(country, "中国") || strings.Contains(country, "china") {
		return "Asia/Shanghai"
	}
	return ""
}

func normalizeISP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := map[string]string{
		"China Telecom": "电信",
		"中国电信":          "电信",
		"CHINANET":      "电信",
		"China Unicom":  "联通",
		"中国联通":          "联通",
		"UNICOM":        "联通",
		"China Mobile":  "移动",
		"中国移动":          "移动",
		"CMCC":          "移动",
		"中国广电":          "广电",
		"中国铁通":          "铁通",
		"CERNET":        "教育网",
	}
	for key, normalized := range replacer {
		if strings.Contains(value, key) {
			return normalized
		}
	}
	return value
}

func detectNetworkType(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "idc"), strings.Contains(lower, "datacenter"), strings.Contains(lower, "hosting"), strings.Contains(lower, "cloud"), strings.Contains(lower, "server"),
		strings.Contains(lower, "云"), strings.Contains(lower, "机房"), strings.Contains(lower, "数据中心"), strings.Contains(lower, "azure"), strings.Contains(lower, "amazon"):
		return "IDC"
	case strings.Contains(lower, "mobile"), strings.Contains(lower, "移动"):
		return "MOBILE"
	case strings.Contains(lower, "wifi"), strings.Contains(lower, "wireless"):
		return "WIFI"
	case strings.Contains(lower, "broadband"), strings.Contains(lower, "宽带"):
		return "BROADBAND"
	case strings.Contains(lower, "telecom"), strings.Contains(lower, "电信"):
		return "CTCC"
	case strings.Contains(lower, "unicom"), strings.Contains(lower, "联通"):
		return "CUCC"
	default:
		return "OTHER"
	}
}

func composeLocation(parts ...string) string {
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		values = append(values, part)
	}
	return strings.Join(values, " ")
}

func parseCoordinate(value string) *float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &f
}
