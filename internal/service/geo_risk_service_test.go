package service

import (
	"testing"
	"time"

	"aegis/internal/config"
	geodomain "aegis/internal/domain/geo"

	"go.uber.org/zap"
)

func newTestGeoRiskService() *GeoRiskService {
	return NewGeoRiskService(zap.NewNop(), config.GeoRiskConfig{Enabled: true}, nil, nil, nil)
}

func f64(v float64) *float64 { return &v }

func testProfile(lastLat, lastLng float64, lastAt time.Time, countries []string, logins int64) *geodomain.Profile {
	return &geodomain.Profile{
		UserID:         1,
		AppID:          1,
		KnownCountries: countries,
		LastLat:        f64(lastLat),
		LastLng:        f64(lastLng),
		LastCountry:    countries[len(countries)-1],
		LastLoginAt:    &lastAt,
		LoginCount:     logins,
	}
}

func findingRules(findings []geoRiskFinding) map[string]bool {
	out := map[string]bool{}
	for _, f := range findings {
		out[f.rule] = true
	}
	return out
}

func TestAssessImpossibleTravel(t *testing.T) {
	s := newTestGeoRiskService()
	now := time.Now().UTC()
	// 1 小时前在北京，现在在纽约（约 11000km → 速度远超 900km/h）
	p := testProfile(39.9042, 116.4074, now.Add(-time.Hour), []string{"CN"}, 10)
	evt := geodomain.LoginEvent{
		UserID: 1, AppID: 1, IP: "8.8.8.8",
		CountryCode: "US", Lat: f64(40.7128), Lng: f64(-74.0060),
		CreatedAt: now,
	}
	rules := findingRules(s.assess(p, evt))
	if !rules[geodomain.RuleImpossibleTravel] {
		t.Fatal("应命中不可能旅行")
	}
	if !rules[geodomain.RuleNewCountry] {
		t.Fatal("应同时命中新国家")
	}
}

func TestAssessNormalTravelNoFinding(t *testing.T) {
	s := newTestGeoRiskService()
	now := time.Now().UTC()
	// 12 小时前在北京，现在在上海（~1067km，约 89km/h，正常）
	p := testProfile(39.9042, 116.4074, now.Add(-12*time.Hour), []string{"CN"}, 10)
	evt := geodomain.LoginEvent{
		UserID: 1, AppID: 1, IP: "1.2.3.4",
		CountryCode: "CN", Lat: f64(31.2304), Lng: f64(121.4737),
		CreatedAt: now,
	}
	if findings := s.assess(p, evt); len(findings) != 0 {
		t.Fatalf("正常移动不应命中, got %+v", findings)
	}
}

func TestAssessShortJitterIgnored(t *testing.T) {
	s := newTestGeoRiskService()
	now := time.Now().UTC()
	// 1 分钟内 50km 跳动（GeoIP 城市级抖动）：速度极高但位移低于 MinTravelKM，应忽略
	p := testProfile(39.9042, 116.4074, now.Add(-time.Minute), []string{"CN"}, 10)
	evt := geodomain.LoginEvent{
		UserID: 1, AppID: 1, IP: "1.2.3.4",
		CountryCode: "CN", Lat: f64(40.3), Lng: f64(116.6),
		CreatedAt: now,
	}
	rules := findingRules(s.assess(p, evt))
	if rules[geodomain.RuleImpossibleTravel] {
		t.Fatal("短距离定位抖动不应命中不可能旅行")
	}
}

func TestAssessNewCountryRequiresHistory(t *testing.T) {
	s := newTestGeoRiskService()
	now := time.Now().UTC()
	evt := geodomain.LoginEvent{
		UserID: 1, AppID: 1, IP: "8.8.8.8",
		CountryCode: "US", CreatedAt: now,
	}
	// 仅 2 次历史登录（< 默认阈值 3）→ 不触发
	p := testProfile(39.9, 116.4, now.Add(-48*time.Hour), []string{"CN"}, 2)
	if rules := findingRules(s.assess(p, evt)); rules[geodomain.RuleNewCountry] {
		t.Fatal("历史登录次数不足时不应命中新国家")
	}
	// 5 次历史登录 → 触发
	p = testProfile(39.9, 116.4, now.Add(-48*time.Hour), []string{"CN"}, 5)
	if rules := findingRules(s.assess(p, evt)); !rules[geodomain.RuleNewCountry] {
		t.Fatal("应命中新国家")
	}
}

func TestAssessFarFromHome(t *testing.T) {
	s := newTestGeoRiskService()
	now := time.Now().UTC()
	p := testProfile(39.9, 116.4, now.Add(-48*time.Hour), []string{"CN", "US"}, 50)
	p.HomeLat, p.HomeLng = f64(39.9), f64(116.4)
	p.HomeRadiusM = 50_000 // 常驻半径 50km → 阈值 max(150km, 500km) = 500km
	evt := geodomain.LoginEvent{
		UserID: 1, AppID: 1, IP: "8.8.8.8",
		CountryCode: "US", Lat: f64(40.7128), Lng: f64(-74.0060), // 纽约，距北京 ~11000km
		CreatedAt: now,
	}
	if rules := findingRules(s.assess(p, evt)); !rules[geodomain.RuleFarFromHome] {
		t.Fatal("应命中远离常驻地")
	}
}

func TestAssessNoProfileNoFindings(t *testing.T) {
	s := newTestGeoRiskService()
	evt := geodomain.LoginEvent{UserID: 1, AppID: 1, IP: "8.8.8.8", CountryCode: "US", CreatedAt: time.Now().UTC()}
	if findings := s.assess(nil, evt); findings != nil {
		t.Fatal("无画像（首次登录）不应有任何命中")
	}
}

func TestGeoRiskLevel(t *testing.T) {
	cases := []struct {
		score      int
		wantLevel  string
		wantAction string
	}{
		{85, "high", "review"},
		{60, "medium", "pass"},
		{15, "low", "pass"},
	}
	for _, c := range cases {
		level, action := geoRiskLevel(c.score)
		if level != c.wantLevel || action != c.wantAction {
			t.Fatalf("score %d → (%s, %s), want (%s, %s)", c.score, level, action, c.wantLevel, c.wantAction)
		}
	}
}
