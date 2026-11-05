package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"aegis/internal/service/chinageo"
)

// UAPIs 的 IP 查询：GET /api/v1/network/ipinfo?ip=x[&source=commercial]，一次一个地址，支持 IPv6。
// 默认版给「国家 省 市」+ 运营商 + ASN；commercial 版多给区县、行政区划代码（city_code）与时区，每次扣 2 积分。
// 不带密钥按出口 IP 计：每秒 4 次、30 天 1500 积分；配 GEOIP_UAPIS_KEY 走 Authorization: Bearer。
// 限流与额度用尽都回 429，剩余额度在 ratelimit 头里（"visitor-quota";r=0;...;t=秒数）。
// 怪癖：私有地址 region=「私有地址」；香港 time_zone 也写 Asia/Shanghai；境外省市是英文。
var uapisEndpoint = "https://uapis.cn/api/v1/network/ipinfo"

const uapisSourceCommercial = "commercial"

type uapisIPInfo struct {
	IP        string  `json:"ip"`
	Region    string  `json:"region"`
	ISP       string  `json:"isp"`
	ASN       string  `json:"asn"`
	LLC       string  `json:"llc"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	District  string  `json:"district"`
	CityCode  string  `json:"city_code"`
	TimeZone  string  `json:"time_zone"`
}

func (s *LocationService) lookupUAPIs(ctx context.Context, ip string) (IPLocation, bool, error) {
	query := url.Values{"ip": {ip}}
	if s.uapisSource != "" {
		query.Set("source", s.uapisSource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uapisEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return IPLocation{}, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aegis-location/1.0")
	if s.uapisKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.uapisKey)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return IPLocation{}, false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, resp.Body)
		return IPLocation{}, false, &sourceCooldownError{err: fmt.Errorf("uapis rate limited"), wait: uapisCooldown(resp.Header)}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden:
		_, _ = io.Copy(io.Discard, resp.Body)
		return IPLocation{}, false, &sourceCooldownError{err: fmt.Errorf("uapis status %d", resp.StatusCode), wait: 10 * time.Minute}
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound:
		// 「无法解析目标地址」之类，换个时间问也一样
		_, _ = io.Copy(io.Discard, resp.Body)
		return IPLocation{}, false, nil
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		_, _ = io.Copy(io.Discard, resp.Body)
		return IPLocation{}, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var info uapisIPInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return IPLocation{}, false, err
	}
	loc := uapisLocation(ip, info)
	loc.ResolvedAt = time.Now().UTC()
	return loc, loc.isResolved(), nil
}

// uapisCooldown 额度用尽时等到额度窗口重置，否则只是每秒限流，等一秒。
func uapisCooldown(header http.Header) time.Duration {
	wait := retryAfter(header, time.Second)
	for _, policy := range strings.Split(header.Get("RateLimit"), ",") {
		var remaining, reset = -1, 0
		for _, field := range strings.Split(policy, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
			if !ok {
				continue
			}
			n, err := strconv.Atoi(strings.Trim(value, `"`))
			if err != nil {
				continue
			}
			switch key {
			case "r":
				remaining = n
			case "t":
				reset = n
			}
		}
		if remaining == 0 && reset > 0 {
			wait = max(wait, time.Duration(reset)*time.Second)
		}
	}
	return min(wait, sourceMaxWait)
}

func uapisLocation(ip string, info uapisIPInfo) IPLocation {
	trim := strings.TrimSpace
	loc := IPLocation{IP: ip, Source: "uapis"}
	fields := strings.Fields(info.Region)
	if len(fields) == 0 {
		return loc
	}
	if strings.Contains(fields[0], "私有") || strings.Contains(fields[0], "保留") || strings.Contains(fields[0], "本机") {
		loc.Location = "保留地址"
		loc.IsPrivate = true
		return loc
	}
	part := func(i int) string {
		if i < len(fields) {
			return fields[i]
		}
		return ""
	}
	country, province, city := part(0), part(1), part(2)
	district := locationFirstNonEmpty(info.District, part(3))
	cityCode := trim(info.CityCode)

	code := countryCodeFromName(country)
	if code == "CN" {
		switch {
		case strings.HasPrefix(cityCode, "81") || strings.HasPrefix(province, "香港"):
			code = "HK"
		case strings.HasPrefix(cityCode, "82") || strings.HasPrefix(province, "澳门"):
			code = "MO"
		case strings.HasPrefix(cityCode, "71") || strings.HasPrefix(province, "台湾"):
			code = "TW"
		}
	}

	switch code {
	case "CN":
		loc.Country, loc.CountryCode, loc.Timezone = "中国", "CN", "Asia/Shanghai"
		place, ok := chinageo.ResolveCode(cityCode)
		if !ok {
			place, ok = chinageo.Resolve(province, city, district)
		}
		if ok {
			loc.Region, loc.City, loc.District = place.Province, place.City, place.District
			lat, lng := place.Lat, place.Lng
			loc.Coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
		} else {
			loc.Region, loc.City, loc.District = province, city, district
		}
	case "HK", "MO", "TW":
		loc.Country, loc.CountryCode = normalizeCountry("", code)
		loc.Timezone = specialRegionTimezone[code]
		loc.City, loc.District = city, district
	default:
		// 境外省市是英文，城市名本身可能带空格（Mountain View），第二段以后整段当城市
		loc.Country, loc.CountryCode = country, code
		loc.Region, loc.City = province, strings.Join(fields[min(2, len(fields)):], " ")
		loc.District = trim(info.District)
		loc.Timezone = trim(info.TimeZone)
	}

	if loc.Coordinates == nil && (info.Latitude != 0 || info.Longitude != 0) {
		lat, lng := info.Latitude, info.Longitude
		loc.Coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
	}
	carrier := locationFirstNonEmpty(info.LLC, info.ISP)
	if carrier == "N/A" {
		carrier = ""
	}
	loc.ISP = normalizeISP(carrier)
	loc.Network = NetworkInfo{Type: detectNetworkType(carrier), Organization: locationFirstNonEmpty(info.ISP, carrier), ASN: trim(info.ASN)}
	if loc.Network.Organization == "N/A" {
		loc.Network.Organization = carrier
	}
	loc.Location = composeLocation(loc.Country, loc.Region, loc.City, loc.District)
	return loc
}
