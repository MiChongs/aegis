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

// 美团的 IP 定位：GET ?rgeo=true&ip=x，一次一个地址，只认 IPv4。
// 返回 {"data":{"lat","lng","ip","fromwhere","rgeo":{"country","province","adcode","city","district"}}}，
// 国内给到区县与行政区划代码，境外给到国家与一级行政区（「日本 东京都 新宿区」）；不带运营商。
// 查不到时 HTTP 200 + {"error":{"code":404,"type":"Data Not found","message":"Can Not Loc"}}。
// 2026 年年中起这个接口对所有地址都这样回（含不传 ip），熔断会把它停掉，恢复后自动重新启用。
//
// 逆地理编码 GET /group/v1/city/latlng/{lat},{lng}?tag=0 仍可用，IP 定位缺区县时拿坐标补一次。
var (
	meituanIPEndpoint     = "https://apimobile.meituan.com/locate/v2/ip/loc"
	meituanLatLngEndpoint = "https://apimobile.meituan.com/group/v1/city/latlng"
)

const meituanUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

type meituanIPResponse struct {
	Data struct {
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
		IP        string  `json:"ip"`
		FromWhere string  `json:"fromwhere"`
		RGeo      struct {
			Country  string `json:"country"`
			Province string `json:"province"`
			AdCode   string `json:"adcode"`
			City     string `json:"city"`
			District string `json:"district"`
		} `json:"rgeo"`
	} `json:"data"`
	Error *struct {
		Code    int    `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type meituanLatLngResponse struct {
	Data struct {
		Country   string  `json:"country"`
		Province  string  `json:"province"`
		City      string  `json:"city"`
		District  string  `json:"district"`
		IsForeign bool    `json:"isForeign"`
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
	} `json:"data"`
}

func (s *LocationService) lookupMeituan(ctx context.Context, ip string) (IPLocation, bool, error) {
	var payload meituanIPResponse
	if err := s.getMeituan(ctx, meituanIPEndpoint+"?rgeo=true&ip="+url.QueryEscape(ip), &payload); err != nil {
		return IPLocation{}, false, err
	}
	if payload.Error != nil {
		if payload.Error.Code == http.StatusNotFound {
			return IPLocation{}, false, nil
		}
		return IPLocation{}, false, fmt.Errorf("meituan error %d: %s", payload.Error.Code, payload.Error.Message)
	}
	rgeo := payload.Data.RGeo
	if strings.TrimSpace(rgeo.Country) == "" && strings.TrimSpace(rgeo.Province) == "" {
		return IPLocation{}, false, fmt.Errorf("meituan: malformed answer")
	}

	if strings.TrimSpace(rgeo.District) == "" && strings.TrimSpace(rgeo.Country) == "中国" && (payload.Data.Lat != 0 || payload.Data.Lng != 0) {
		// 只定位到城市时用坐标反查一次区县，失败不影响已有结果
		if geo, err := s.reverseMeituan(ctx, payload.Data.Lat, payload.Data.Lng); err == nil &&
			samePlaceName(geo.Data.Province, rgeo.Province) && samePlaceName(geo.Data.City, rgeo.City) {
			rgeo.District = strings.TrimSpace(geo.Data.District)
			rgeo.AdCode = ""
		}
	}
	payload.Data.RGeo = rgeo

	loc := meituanLocation(ip, payload)
	loc.ResolvedAt = time.Now().UTC()
	return loc, true, nil
}

func (s *LocationService) reverseMeituan(ctx context.Context, lat, lng float64) (meituanLatLngResponse, error) {
	var out meituanLatLngResponse
	target := fmt.Sprintf("%s/%s,%s?tag=0", meituanLatLngEndpoint,
		strconv.FormatFloat(lat, 'f', 6, 64), strconv.FormatFloat(lng, 'f', 6, 64))
	err := s.getMeituan(ctx, target, &out)
	return out, err
}

func (s *LocationService) getMeituan(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", meituanUserAgent)
	req.Header.Set("Referer", "https://www.meituan.com/")

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		_, _ = io.Copy(io.Discard, resp.Body)
		return &sourceCooldownError{err: fmt.Errorf("meituan rate limited"), wait: retryAfter(resp.Header, time.Minute)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func meituanLocation(ip string, payload meituanIPResponse) IPLocation {
	trim := strings.TrimSpace
	rgeo := payload.Data.RGeo
	country, province, city, district := trim(rgeo.Country), trim(rgeo.Province), trim(rgeo.City), trim(rgeo.District)

	loc := IPLocation{IP: ip, Source: "meituan"}
	code := countryCodeFromName(country)
	if code == "CN" {
		for name, special := range map[string]string{"香港": "HK", "澳门": "MO", "台湾": "TW"} {
			if strings.HasPrefix(province, name) {
				code = special
			}
		}
	}

	switch code {
	case "CN":
		loc.Country, loc.CountryCode, loc.Timezone = "中国", "CN", "Asia/Shanghai"
		place, ok := chinageo.ResolveCode(trim(rgeo.AdCode))
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
		if !samePlaceName(city, province) {
			loc.City = city
		}
		loc.District = district
	default:
		loc.Country, loc.CountryCode = country, code
		loc.Region = province
		if city != province {
			loc.City = city
		}
		loc.District = district
	}

	if loc.Coordinates == nil && (payload.Data.Lat != 0 || payload.Data.Lng != 0) {
		lat, lng := payload.Data.Lat, payload.Data.Lng
		loc.Coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
	}
	loc.Location = composeLocation(loc.Country, loc.Region, loc.City, loc.District)
	return loc
}

var specialRegionTimezone = map[string]string{"HK": "Asia/Hong_Kong", "MO": "Asia/Macau", "TW": "Asia/Taipei"}

// retryAfter 读 Retry-After（秒数），没有就用 fallback。
func retryAfter(header http.Header, fallback time.Duration) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After"))); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}
