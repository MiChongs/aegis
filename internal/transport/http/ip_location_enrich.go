package httptransport

import (
	"context"

	"aegis/internal/service"
)

// 列表接口的 IP 位置回填。
//
// 位置不落库：审计行里只有 IP，位置是**查询时**用当前 GeoIP 库得出的结论。
// 库每周更新，把当时的结论写进历史行反而会让同一个 IP 在不同记录里说法不一。
//
// 一页里同一个 IP 通常重复出现（同一台设备的多次登录），因此先按 IP 去重再解析：
// LocationService 内部虽然有多级缓存，但 Resolve 仍要走 singleflight 与本地锁，
// 20 条记录去重后往往只剩两三个 IP。
func (h *Handler) resolveIPLocations(ctx context.Context, ips []string) map[string]service.IPLocation {
	if h.location == nil || len(ips) == 0 {
		return nil
	}
	out := make(map[string]service.IPLocation, len(ips))
	for _, ip := range ips {
		if ip == "" {
			continue
		}
		if _, ok := out[ip]; ok {
			continue
		}
		out[ip] = h.location.Resolve(ctx, ip)
	}
	return out
}

// geoCoords 拆出经纬度指针。GeoIP 对内网地址没有结论，此时返回 (nil, nil) ——
// 补一个 (0,0) 会让所有内网来源在地图上堆到几内亚湾。
func geoCoords(loc service.IPLocation) (*float64, *float64) {
	if loc.Coordinates == nil {
		return nil, nil
	}
	return loc.Coordinates.Latitude, loc.Coordinates.Longitude
}
