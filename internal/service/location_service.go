package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type LocationService struct {
	log                 *zap.Logger
	redis               *redislib.Client
	keyPrefix           string
	http                *http.Client
	group               singleflight.Group
	mu                  sync.RWMutex
	local               map[string]cachedLocation
	localTTL            time.Duration
	redisTTL            time.Duration
	failureTTL          time.Duration
	cacheLookupTimeout  time.Duration
	cacheWriteTimeout   time.Duration
	refreshTimeout      time.Duration
	providerTimeout     time.Duration
	geo                 *geoDatabaseResolver
	allowRemoteFallback bool
}

func NewLocationService(log *zap.Logger, redisClient *redislib.Client, keyPrefix string, geoCfg config.GeoIPConfig) *LocationService {
	if log == nil {
		log = zap.NewNop()
	}
	service := &LocationService{
		log:                 log,
		redis:               redisClient,
		keyPrefix:           strings.TrimSpace(keyPrefix),
		http:                &http.Client{Timeout: 1500 * time.Millisecond},
		local:               make(map[string]cachedLocation),
		localTTL:            10 * time.Minute,
		redisTTL:            6 * time.Hour,
		failureTTL:          15 * time.Minute,
		cacheLookupTimeout:  40 * time.Millisecond,
		cacheWriteTimeout:   250 * time.Millisecond,
		refreshTimeout:      4 * time.Second,
		providerTimeout:     1200 * time.Millisecond,
		allowRemoteFallback: geoCfg.AllowRemoteFallback,
	}
	resolver, err := newGeoDatabaseResolver(log, geoCfg)
	if err != nil {
		log.Warn("init geoip database resolver failed", zap.Error(err))
	} else {
		service.geo = resolver
	}
	return service
}

func (s *LocationService) Close() {
	if s == nil || s.geo == nil {
		return
	}
	s.geo.Close()
}

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
	if loc, ok := s.lookupGeoDatabase(ip); ok {
		s.persistAsync(ip, loc, s.redisTTL)
		return loc, true
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
		loc := s.lookup(ctx, ip)
		ttl := s.redisTTL
		if !loc.isResolved() {
			ttl = s.failureTTL
		}
		s.persist(ip, loc, ttl)
		return loc, nil
	})

	loc, _ := value.(IPLocation)
	if loc.IP == "" {
		return s.DefaultLocation(ip)
	}
	return loc
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

func (s *LocationService) lookup(ctx context.Context, ip string) IPLocation {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return s.DefaultLocation(ip)
	}
	if isPrivateAddr(addr) {
		return s.DefaultLocation(ip)
	}
	if loc, ok := s.lookupGeoDatabase(ip); ok {
		return loc
	}
	if !s.allowRemoteFallback {
		return s.DefaultLocation(ip)
	}

	providers := []struct {
		name string
		fn   func(context.Context, string) (IPLocation, error)
	}{
		{name: "mir6", fn: s.lookupMir6},
		{name: "ipinfo", fn: s.lookupIPInfo},
		{name: "ipapi", fn: s.lookupIPAPI},
		{name: "geojs", fn: s.lookupGeoJS},
	}

	for _, provider := range providers {
		providerCtx, cancel := context.WithTimeout(ctx, s.providerTimeout)
		loc, err := provider.fn(providerCtx, ip)
		cancel()
		if err == nil && loc.isResolved() {
			return loc
		}
		if err != nil && ctx.Err() == nil {
			s.log.Debug("location provider failed", zap.String("provider", provider.name), zap.String("ip", ip), zap.Error(err))
		}
	}
	return s.DefaultLocation(ip)
}

func (s *LocationService) lookupGeoDatabase(ip string) (IPLocation, bool) {
	if s == nil || s.geo == nil {
		return IPLocation{}, false
	}
	loc, err := s.geo.Lookup(ip)
	if err != nil {
		s.log.Debug("geoip database lookup failed", zap.String("ip", ip), zap.Error(err))
		return IPLocation{}, false
	}
	if !loc.isResolved() {
		return IPLocation{}, false
	}
	return loc, true
}

func (s *LocationService) lookupMir6(ctx context.Context, ip string) (IPLocation, error) {
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Country     string `json:"country"`
			CountryCode string `json:"countryCode"`
			Province    string `json:"province"`
			City        string `json:"city"`
			Districts   string `json:"districts"`
			Timezone    string `json:"timezone"`
			ISP         string `json:"isp"`
		} `json:"data"`
	}
	if err := s.fetchJSON(ctx, fmt.Sprintf("https://api.mir6.com/api/ip?ip=%s&type=json", ip), &payload); err != nil {
		return IPLocation{}, err
	}
	if payload.Code != 200 {
		return IPLocation{}, fmt.Errorf("mir6 status %d", payload.Code)
	}
	return s.buildLocation(ip, payload.Data.Country, payload.Data.CountryCode, payload.Data.Province, payload.Data.City, payload.Data.Districts, payload.Data.Timezone, payload.Data.ISP, payload.Data.ISP, "", "mir6"), nil
}

func (s *LocationService) lookupIPInfo(ctx context.Context, ip string) (IPLocation, error) {
	var payload struct {
		IP       string `json:"ip"`
		Country  string `json:"country"`
		Region   string `json:"region"`
		City     string `json:"city"`
		Timezone string `json:"timezone"`
		Org      string `json:"org"`
		Loc      string `json:"loc"`
	}
	if err := s.fetchJSON(ctx, fmt.Sprintf("https://ipinfo.io/%s/json", ip), &payload); err != nil {
		return IPLocation{}, err
	}
	loc := s.buildLocation(ip, payload.Country, payload.Country, payload.Region, payload.City, "", payload.Timezone, payload.Org, payload.Org, extractASN(payload.Org), "ipinfo")
	loc.Coordinates = parseCoordinatePair(payload.Loc)
	return loc, nil
}

func (s *LocationService) lookupIPAPI(ctx context.Context, ip string) (IPLocation, error) {
	var payload struct {
		IP          string      `json:"ip"`
		CountryName string      `json:"country_name"`
		CountryCode string      `json:"country_code"`
		Region      string      `json:"region"`
		City        string      `json:"city"`
		Timezone    string      `json:"timezone"`
		Org         string      `json:"org"`
		ASN         string      `json:"asn"`
		Latitude    interface{} `json:"latitude"`
		Longitude   interface{} `json:"longitude"`
	}
	if err := s.fetchJSON(ctx, fmt.Sprintf("https://ipapi.co/%s/json/", ip), &payload); err != nil {
		return IPLocation{}, err
	}
	loc := s.buildLocation(ip, payload.CountryName, payload.CountryCode, payload.Region, payload.City, "", payload.Timezone, payload.Org, payload.Org, payload.ASN, "ipapi")
	loc.Coordinates = buildCoordinates(parseFloat(payload.Latitude), parseFloat(payload.Longitude))
	return loc, nil
}

func (s *LocationService) lookupGeoJS(ctx context.Context, ip string) (IPLocation, error) {
	var payload struct {
		IP           string `json:"ip"`
		Country      string `json:"country"`
		CountryCode  string `json:"country_code"`
		Region       string `json:"region"`
		City         string `json:"city"`
		Timezone     string `json:"timezone"`
		Organization string `json:"organization_name"`
		Latitude     string `json:"latitude"`
		Longitude    string `json:"longitude"`
	}
	if err := s.fetchJSON(ctx, fmt.Sprintf("https://get.geojs.io/v1/ip/geo/%s.json", ip), &payload); err != nil {
		return IPLocation{}, err
	}
	loc := s.buildLocation(ip, payload.Country, payload.CountryCode, payload.Region, payload.City, "", payload.Timezone, payload.Organization, payload.Organization, "", "geojs")
	loc.Coordinates = buildCoordinates(parseFloat(payload.Latitude), parseFloat(payload.Longitude))
	return loc, nil
}

func (s *LocationService) fetchJSON(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aegis-location/1.0")

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *LocationService) buildLocation(ip, country, countryCode, region, city, district, timezone, isp, organization, asn, source string) IPLocation {
	displayCountry, normalizedCountryCode := normalizeCountry(country, countryCode)
	loc := IPLocation{
		IP:          ip,
		Country:     displayCountry,
		CountryCode: normalizedCountryCode,
		Region:      normalizeString(region),
		City:        normalizeString(city),
		District:    normalizeString(district),
		Location:    composeLocation(displayCountry, region, city, district),
		Timezone:    normalizeTimezone(displayCountry, timezone),
		ISP:         normalizeISP(isp),
		Network: NetworkInfo{
			Type:         detectNetworkType(isp),
			Organization: normalizeString(organization),
			ASN:          normalizeString(asn),
		},
		Source:     source,
		ResolvedAt: time.Now().UTC(),
	}
	if loc.Country == "" && loc.Region == "" && loc.City == "" && loc.ISP == "" {
		return s.DefaultLocation(ip)
	}
	return loc
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

func (s *LocationService) persistAsync(ip string, loc IPLocation, ttl time.Duration) {
	go s.persist(ip, loc, ttl)
}

func (s *LocationService) redisKey(ip string) string {
	if s.keyPrefix == "" {
		return "location:ip:" + ip
	}
	return s.keyPrefix + ":location:ip:" + ip
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
	case strings.Contains(lower, "idc"), strings.Contains(lower, "datacenter"), strings.Contains(lower, "hosting"), strings.Contains(lower, "cloud"), strings.Contains(lower, "server"):
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

func extractASN(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) > 0 && strings.HasPrefix(strings.ToUpper(fields[0]), "AS") {
		return fields[0]
	}
	return ""
}

func parseCoordinatePair(value string) *GeoCoordinate {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return nil
	}
	return buildCoordinates(parseFloat(parts[0]), parseFloat(parts[1]))
}

func parseFloat(value interface{}) *float64 {
	switch v := value.(type) {
	case float64:
		return &v
	case float32:
		f := float64(v)
		return &f
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

func buildCoordinates(latitude, longitude *float64) *GeoCoordinate {
	if latitude == nil && longitude == nil {
		return nil
	}
	return &GeoCoordinate{Latitude: latitude, Longitude: longitude}
}
