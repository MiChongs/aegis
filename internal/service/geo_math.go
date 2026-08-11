package service

import (
	"fmt"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/planar"
)

// 本文件提供地理围栏 / 地理风控的纯内存几何计算，基于 paulmach/orb：
//   - haversineKM：球面距离（不可能旅行检测、圆形围栏）→ orb/geo.DistanceHaversine
//   - geoMultiPolygon：GeoJSON 解析（orb/geojson，带几何校验）
//     + 点包含判定（orb/planar，外环含、洞排除，与原射线法语义一致）
//
// 设计约束：位于防火墙请求热路径，禁止任何 I/O；orb 为纯内存计算库，
// 判定路径零分配，性能与原手写实现同量级（见 geo_math_test.go 基准）。
// 已知限制（与原实现一致）：平面判定在经度 ±180°（反子午线）跨越处不正确，
// 围栏请勿横跨反子午线。

// haversineKM 计算两个经纬度点的球面距离（公里）。
func haversineKM(lat1, lng1, lat2, lng2 float64) float64 {
	return geo.DistanceHaversine(orb.Point{lng1, lat1}, orb.Point{lng2, lat2}) / 1000
}

// geoMultiPolygon 编译后的多边形围栏。底层为 orb.MultiPolygon
// （polygon → ring（首环为外环，其余为洞）→ [lng, lat]），
// nil 值表示「无多边形几何」（圆形围栏）。
type geoMultiPolygon orb.MultiPolygon

// parseGeoJSONMultiPolygon 解析 GeoJSON Polygon / MultiPolygon 几何。
func parseGeoJSONMultiPolygon(raw []byte) (geoMultiPolygon, error) {
	g, err := geojson.UnmarshalGeometry(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 GeoJSON 失败: %w", err)
	}
	switch geom := g.Geometry().(type) {
	case orb.Polygon:
		return geoMultiPolygon(orb.MultiPolygon{geom}), nil
	case orb.MultiPolygon:
		return geoMultiPolygon(geom), nil
	default:
		return nil, fmt.Errorf("不支持的几何类型: %s（仅支持 Polygon / MultiPolygon）", g.Type)
	}
}

// contains 判断点 (lat, lng) 是否落在多边形围栏内（外环内且不在任何洞内）。
func (mp geoMultiPolygon) contains(lat, lng float64) bool {
	return planar.MultiPolygonContains(orb.MultiPolygon(mp), orb.Point{lng, lat})
}
