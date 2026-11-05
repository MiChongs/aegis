package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aegis/internal/service/chinageo"
)

// 宝塔面板的 IP 归属地接口：GET ?ip=a,b,c，按 IP 返回一个对象。
// 大陆地址给到区县与运营商，并附行政区划代码；不支持 IPv6（字段全空）；
// 保留地址的 country 为「保留」；Google DNS 等知名地址 country 是名称、没有国家代码。
var btcnEndpoint = "https://www.bt.cn/api/panel/get_ip_info"

const btcnBatchSize = 50

type btcnIPInfo struct {
	Continent   string `json:"continent"`
	Country     string `json:"country"`
	Province    string `json:"province"`
	City        string `json:"city"`
	Region      string `json:"region"`
	Carrier     string `json:"carrier"`
	Division    string `json:"division"`
	EnCountry   string `json:"en_country"`
	EnShortCode string `json:"en_short_code"`
	Longitude   string `json:"longitude"`
	Latitude    string `json:"latitude"`
}

// lookupBtCN 一次查询多个 IP，返回值以传入的 IP 为键。
func (s *LocationService) lookupBtCN(ctx context.Context, ips []string) (map[string]IPLocation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, btcnEndpoint+"?ip="+url.QueryEscape(strings.Join(ips, ",")), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aegis-location/1.0")

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var payload map[string]btcnIPInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make(map[string]IPLocation, len(ips))
	for _, ip := range ips {
		info, ok := payload[ip]
		if !ok {
			continue
		}
		loc := btcnLocation(ip, info)
		loc.ResolvedAt = now
		out[ip] = loc
	}
	return out, nil
}

func btcnLocation(ip string, info btcnIPInfo) IPLocation {
	trim := strings.TrimSpace
	country, province, city, district := trim(info.Country), trim(info.Province), trim(info.City), trim(info.Region)
	carrier := trim(info.Carrier)
	code := strings.ToUpper(trim(info.EnShortCode))

	loc := IPLocation{IP: ip, Source: "btcn"}
	if country == "保留" {
		loc.Location = "保留地址"
		loc.IsPrivate = true
		return loc
	}

	switch code {
	case "":
		// 没有国家代码时 country 是机构名（GoogleDNS、CloudFlareDNS），不当国家用
		loc.ISP = locationFirstNonEmpty(carrier, country)
		loc.Location = country
	case "CN":
		loc.Country, loc.CountryCode, loc.Timezone = "中国", "CN", "Asia/Shanghai"
		// 优先用行政区划代码换官方全称，代码缺失或对不上再按名称匹配
		place, ok := chinageo.ResolveCode(trim(info.Division))
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
		// 港澳台的 province 就是地区名本身，并入国家一栏
		loc.Country, loc.CountryCode = normalizeCountry("", code)
		loc.Timezone = map[string]string{"HK": "Asia/Hong_Kong", "MO": "Asia/Macau", "TW": "Asia/Taipei"}[code]
		if city != province {
			loc.City = city
		}
		loc.District = district
	default:
		loc.Country, loc.CountryCode = locationFirstNonEmpty(country, trim(info.EnCountry)), code
		loc.Region, loc.City, loc.District = province, city, district
	}

	if loc.Coordinates == nil {
		lat, lng := parseCoordinate(info.Latitude), parseCoordinate(info.Longitude)
		if lat != nil && lng != nil {
			loc.Coordinates = &GeoCoordinate{Latitude: lat, Longitude: lng}
		}
	}
	if loc.ISP == "" {
		loc.ISP = normalizeISP(carrier)
	}
	loc.Network = NetworkInfo{Type: detectNetworkType(carrier), Organization: carrier}
	if loc.Location == "" {
		loc.Location = composeLocation(loc.Country, loc.Region, loc.City, loc.District)
	}
	return loc
}

func locationFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
