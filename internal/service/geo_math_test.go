package service

import (
	"math"
	"testing"
)

func TestHaversineKM(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lng1, lat2, lng2 float64
		wantKM                 float64
		tolKM                  float64
	}{
		{"同一点", 39.9, 116.4, 39.9, 116.4, 0, 0.001},
		{"北京-上海", 39.9042, 116.4074, 31.2304, 121.4737, 1067, 15},
		{"北京-纽约", 39.9042, 116.4074, 40.7128, -74.0060, 11000, 100},
		{"赤道 1 经度", 0, 0, 0, 1, 111.19, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := haversineKM(c.lat1, c.lng1, c.lat2, c.lng2)
			if math.Abs(got-c.wantKM) > c.tolKM {
				t.Fatalf("haversineKM = %.2f, want %.2f ± %.2f", got, c.wantKM, c.tolKM)
			}
		})
	}
}

func TestParseGeoJSONMultiPolygon(t *testing.T) {
	t.Run("Polygon 自动升级为 MultiPolygon", func(t *testing.T) {
		raw := `{"type":"Polygon","coordinates":[[[116,39],[117,39],[117,41],[116,41],[116,39]]]}`
		mp, err := parseGeoJSONMultiPolygon([]byte(raw))
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if len(mp) != 1 || len(mp[0]) != 1 || len(mp[0][0]) != 5 {
			t.Fatalf("结构不符: %v", mp)
		}
	})
	t.Run("MultiPolygon", func(t *testing.T) {
		raw := `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,1],[0,0]]]]}`
		if _, err := parseGeoJSONMultiPolygon([]byte(raw)); err != nil {
			t.Fatalf("解析失败: %v", err)
		}
	})
	t.Run("不支持的类型", func(t *testing.T) {
		raw := `{"type":"Point","coordinates":[116,39]}`
		if _, err := parseGeoJSONMultiPolygon([]byte(raw)); err == nil {
			t.Fatal("应拒绝 Point 几何")
		}
	})
	t.Run("非法 JSON", func(t *testing.T) {
		if _, err := parseGeoJSONMultiPolygon([]byte(`{`)); err == nil {
			t.Fatal("应拒绝非法 JSON")
		}
	})
}

func TestMultiPolygonContains(t *testing.T) {
	// 北京周边方形围栏（lng 116~117, lat 39~41）
	raw := `{"type":"Polygon","coordinates":[[[116,39],[117,39],[117,41],[116,41],[116,39]]]}`
	mp, err := parseGeoJSONMultiPolygon([]byte(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !mp.contains(40.0, 116.5) {
		t.Fatal("围栏内的点应命中")
	}
	if mp.contains(40.0, 118.0) {
		t.Fatal("围栏外的点不应命中")
	}
	if mp.contains(38.0, 116.5) {
		t.Fatal("纬度越界的点不应命中")
	}
}

func TestMultiPolygonContainsWithHole(t *testing.T) {
	// 外环 (0,0)-(10,10)，洞 (4,4)-(6,6)
	raw := `{"type":"Polygon","coordinates":[
		[[0,0],[10,0],[10,10],[0,10],[0,0]],
		[[4,4],[6,4],[6,6],[4,6],[4,4]]
	]}`
	mp, err := parseGeoJSONMultiPolygon([]byte(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !mp.contains(2, 2) {
		t.Fatal("外环内、洞外的点应命中")
	}
	if mp.contains(5, 5) {
		t.Fatal("洞内的点不应命中")
	}
}

func TestMultiPolygonContainsDisjoint(t *testing.T) {
	// 两个互不相连的面：东亚一块 + 西欧一块
	raw := `{"type":"MultiPolygon","coordinates":[
		[[[110,30],[120,30],[120,40],[110,40],[110,30]]],
		[[[0,45],[10,45],[10,55],[0,55],[0,45]]]
	]}`
	mp, err := parseGeoJSONMultiPolygon([]byte(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !mp.contains(35, 115) {
		t.Fatal("第一个面内的点应命中")
	}
	if !mp.contains(50, 5) {
		t.Fatal("第二个面内的点应命中")
	}
	if mp.contains(35, 5) {
		t.Fatal("两面之间的点不应命中")
	}
}

// ── 基准：热路径性能对照（go test -bench=. -benchmem ./internal/service/）──

func BenchmarkHaversineKM(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = haversineKM(39.9042, 116.4074, 31.2304, 121.4737)
	}
}

func BenchmarkMultiPolygonContains(b *testing.B) {
	raw := `{"type":"Polygon","coordinates":[
		[[0,0],[10,0],[10,10],[0,10],[0,0]],
		[[4,4],[6,4],[6,6],[4,6],[4,4]]
	]}`
	mp, err := parseGeoJSONMultiPolygon([]byte(raw))
	if err != nil {
		b.Fatalf("解析失败: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mp.contains(2, 2)
		_ = mp.contains(5, 5)
		_ = mp.contains(20, 20)
	}
}
