package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aegis/internal/config"
)

func TestCountryCodeFromName(t *testing.T) {
	for name, want := range map[string]string{
		"中国": "CN", "美国": "US", "日本": "JP", "韩国": "KR", "英国": "GB", "新加坡": "SG", "德国": "DE",
		"阿联酋": "AE", "阿拉伯联合酋长国": "AE", "中国香港": "HK", "俄罗斯": "RU", "GoogleDNS": "", "": "",
	} {
		if got := countryCodeFromName(name); got != want {
			t.Errorf("%q => %q, want %q", name, got, want)
		}
	}
}

func TestMeituanLocation(t *testing.T) {
	var cn meituanIPResponse
	cn.Data.Lat, cn.Data.Lng = 30.66, 113.83
	cn.Data.RGeo.Country, cn.Data.RGeo.Province, cn.Data.RGeo.City, cn.Data.RGeo.District, cn.Data.RGeo.AdCode = "中国", "湖北省", "孝感市", "汉川市", "420984"
	got := meituanLocation("1.2.3.4", cn)
	if got.CountryCode != "CN" || got.Region != "湖北省" || got.City != "孝感市" || got.District != "汉川市" ||
		got.Location != "中国 湖北省 孝感市 汉川市" || got.Coordinates == nil || got.Source != "meituan" || got.ISP != "" {
		t.Fatalf("mainland: %+v", got)
	}

	var jp meituanIPResponse
	jp.Data.Lat, jp.Data.Lng = 35.69, 139.70
	jp.Data.RGeo.Country, jp.Data.RGeo.Province, jp.Data.RGeo.City, jp.Data.RGeo.District = "日本", "东京都", "东京都", "新宿区"
	got = meituanLocation("1.2.3.4", jp)
	if got.CountryCode != "JP" || got.Region != "东京都" || got.City != "" || got.District != "新宿区" || got.Location != "日本 东京都 新宿区" {
		t.Fatalf("abroad: %+v", got)
	}

	var hk meituanIPResponse
	hk.Data.RGeo.Country, hk.Data.RGeo.Province, hk.Data.RGeo.City = "中国", "香港特别行政区", "香港特别行政区"
	got = meituanLocation("1.2.3.4", hk)
	if got.CountryCode != "HK" || got.Country != "中国香港" || got.City != "" || got.Timezone != "Asia/Hong_Kong" {
		t.Fatalf("hong kong: %+v", got)
	}
}

func TestUAPIsLocation(t *testing.T) {
	cases := []struct {
		name string
		info uapisIPInfo
		want IPLocation
	}{
		{
			name: "commercial mainland",
			info: uapisIPInfo{Region: "中国 湖北 孝感 汉川", ISP: "移动", ASN: "AS9808", LLC: "移动", District: "汉川", CityCode: "420984", Latitude: 30.66, Longitude: 113.83},
			want: IPLocation{Country: "中国", CountryCode: "CN", Region: "湖北省", City: "孝感市", District: "汉川市", ISP: "移动", Location: "中国 湖北省 孝感市 汉川市", Timezone: "Asia/Shanghai"},
		},
		{
			name: "standard mainland",
			info: uapisIPInfo{Region: "中国 湖北 孝感", ISP: "China Mobile Communications Group Co., Ltd.", LLC: "移动"},
			want: IPLocation{Country: "中国", CountryCode: "CN", Region: "湖北省", City: "孝感市", ISP: "移动", Location: "中国 湖北省 孝感市", Timezone: "Asia/Shanghai"},
		},
		{
			name: "ipv6 city code",
			info: uapisIPInfo{Region: "中国 吉林 长春", ISP: "电信", LLC: "电信", CityCode: "220100"},
			want: IPLocation{Country: "中国", CountryCode: "CN", Region: "吉林省", City: "长春市", ISP: "电信", Location: "中国 吉林省 长春市", Timezone: "Asia/Shanghai"},
		},
		{
			name: "hong kong",
			info: uapisIPInfo{Region: "中国 香港 香港岛", ISP: "香港电讯", LLC: "香港电讯", CityCode: "810101", TimeZone: "Asia/Shanghai"},
			want: IPLocation{Country: "中国香港", CountryCode: "HK", City: "香港岛", ISP: "香港电讯", Location: "中国香港 香港岛", Timezone: "Asia/Hong_Kong"},
		},
		{
			name: "abroad",
			info: uapisIPInfo{Region: "美国 California Mountain View", ISP: "谷歌", LLC: "谷歌", TimeZone: "America/Los_Angeles", Latitude: 37.38, Longitude: -122.08},
			want: IPLocation{Country: "美国", CountryCode: "US", Region: "California", City: "Mountain View", ISP: "谷歌", Location: "美国 California Mountain View", Timezone: "America/Los_Angeles"},
		},
		{
			name: "private",
			info: uapisIPInfo{Region: "私有地址", ISP: "N/A"},
			want: IPLocation{Location: "保留地址", IsPrivate: true},
		},
	}
	for _, tc := range cases {
		got := uapisLocation("1.2.3.4", tc.info)
		if got.Country != tc.want.Country || got.CountryCode != tc.want.CountryCode || got.Region != tc.want.Region ||
			got.City != tc.want.City || got.District != tc.want.District || got.ISP != tc.want.ISP ||
			got.Location != tc.want.Location || got.Timezone != tc.want.Timezone || got.IsPrivate != tc.want.IsPrivate {
			t.Fatalf("%s: got %+v", tc.name, got)
		}
	}
	if loc := uapisLocation("1.2.3.4", cases[0].info); loc.Network.ASN != "AS9808" || loc.Network.Type != "MOBILE" {
		t.Fatalf("network: %+v", loc.Network)
	}
}

func TestUAPIsCooldown(t *testing.T) {
	h := http.Header{}
	h.Set("RateLimit", `"visitor-rate";r=0;t=1,"visitor-quota";r=1455;uapi-unit="credits";t=2151722`)
	if got := uapisCooldown(h); got != time.Second {
		t.Fatalf("per-second limit: %s", got)
	}
	h.Set("RateLimit", `"visitor-rate";r=3,"visitor-quota";r=0;uapi-unit="credits";t=7200`)
	if got := uapisCooldown(h); got != 2*time.Hour {
		t.Fatalf("quota exhausted: %s", got)
	}
}

func TestMergeLocations(t *testing.T) {
	meituan := meituanLocation("1.2.3.4", func() meituanIPResponse {
		var r meituanIPResponse
		r.Data.RGeo.Country, r.Data.RGeo.Province, r.Data.RGeo.City, r.Data.RGeo.District, r.Data.RGeo.AdCode = "中国", "湖北省", "孝感市", "汉川市", "420984"
		return r
	}())
	btcn := btcnLocation("1.2.3.4", btcnIPInfo{Country: "中国", Province: "湖北", City: "孝感", Carrier: "移动", EnShortCode: "CN"})
	got, ok := mergeLocations([]IPLocation{meituan, btcn})
	if !ok || got.District != "汉川市" || got.ISP != "移动" || got.Network.Type != "MOBILE" || got.Source != "meituan+btcn" {
		t.Fatalf("fill isp: %+v", got)
	}

	// 首选来源只到城市，后面的同城来源补区县
	cityOnly := btcnLocation("1.2.3.4", btcnIPInfo{Country: "中国", Province: "湖北", City: "孝感", Carrier: "移动", EnShortCode: "CN"})
	district := uapisLocation("1.2.3.4", uapisIPInfo{Region: "中国 湖北 孝感 汉川", LLC: "移动", ASN: "AS9808", CityCode: "420984"})
	got, _ = mergeLocations([]IPLocation{cityOnly, district})
	if got.District != "汉川市" || got.Location != "中国 湖北省 孝感市 汉川市" || got.Network.ASN != "AS9808" || got.Source != "btcn+uapis" {
		t.Fatalf("fill district: %+v", got)
	}

	// 省份对不上不混拼
	other := uapisLocation("1.2.3.4", uapisIPInfo{Region: "中国 广东 深圳 南山", LLC: "电信", CityCode: "440305"})
	got, _ = mergeLocations([]IPLocation{cityOnly, other})
	if got.Region != "湖北省" || got.District != "" || got.ISP != "移动" || got.Source != "btcn" {
		t.Fatalf("conflict: %+v", got)
	}

	// 首选只给出机构名，地理信息取后面的
	named := btcnLocation("8.8.8.8", btcnIPInfo{Country: "GoogleDNS", Carrier: "GoogleDNS"})
	us := uapisLocation("8.8.8.8", uapisIPInfo{Region: "美国 California Mountain View", LLC: "谷歌", ASN: "AS15169"})
	got, _ = mergeLocations([]IPLocation{named, us})
	if got.CountryCode != "US" || got.ISP != "GoogleDNS" || got.Location != "美国 California Mountain View" {
		t.Fatalf("named: %+v", got)
	}

	if _, ok := mergeLocations([]IPLocation{{}, {}}); ok {
		t.Fatal("nothing resolved")
	}
}

func TestSourceHealth(t *testing.T) {
	var h sourceHealth
	now := time.Now()
	for i := 1; i < sourceFailureThreshold; i++ {
		if _, opened := h.fail(now, 0); opened {
			t.Fatalf("opened after %d failures", i)
		}
	}
	if wait, opened := h.fail(now, 0); !opened || wait != sourceBaseCooldown || h.allow(now) {
		t.Fatalf("third failure: %s %v", wait, opened)
	}
	if _, opened := h.fail(now, 0); opened {
		t.Fatal("in-flight failures during cooldown must not escalate")
	}
	later := now.Add(sourceBaseCooldown)
	if wait, _ := h.fail(later, 0); wait != 2*sourceBaseCooldown {
		t.Fatalf("doubling: %s", wait)
	}
	for range 10 {
		later = later.Add(sourceMaxCooldown)
		h.fail(later, 0)
	}
	now = later.Add(sourceMaxCooldown)
	if wait, _ := h.fail(now, 0); wait != sourceMaxCooldown {
		t.Fatalf("cap: %s", wait)
	}
	if !h.allow(now.Add(sourceMaxCooldown + time.Second)) {
		t.Fatal("half open after cooldown")
	}
	h.succeed()
	if !h.allow(now) {
		t.Fatal("success closes")
	}
	if wait, opened := h.fail(now, 5*time.Second); !opened || wait != 5*time.Second {
		t.Fatalf("explicit wait: %s %v", wait, opened)
	}
}

// 美团整体失效（全回 Can Not Loc）、宝塔查 IPv4、UAPIs 只补 IPv6。
func TestLocationSourceChain(t *testing.T) {
	var meituanCalls, btcnCalls, uapisCalls atomic.Int32
	var uapisIPs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/meituan"):
			meituanCalls.Add(1)
			_, _ = w.Write([]byte(`{"error":{"code":404,"type":"Data Not found","message":"Can Not Loc"}}`))
		case strings.HasPrefix(r.URL.Path, "/btcn"):
			btcnCalls.Add(1)
			out := map[string]btcnIPInfo{}
			for _, ip := range strings.Split(r.URL.Query().Get("ip"), ",") {
				out[ip] = btcnIPInfo{Country: "中国", Province: "湖北", City: "孝感", Region: "汉川", Carrier: "移动", Division: "420984", EnShortCode: "CN"}
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, "/uapis"):
			uapisCalls.Add(1)
			uapisIPs = append(uapisIPs, r.URL.Query().Get("ip"))
			_, _ = w.Write([]byte(`{"ip":"x","region":"中国 吉林 长春","isp":"电信","asn":"AS17897","llc":"电信","city_code":"220100"}`))
		}
	}))
	defer srv.Close()
	restore := swapLocationEndpoints(srv.URL)
	defer restore()

	s := NewLocationService(nil, nil, "", config.GeoIPConfig{Enabled: true})
	s.http = srv.Client()
	ctx := context.Background()

	got := s.ResolveMany(ctx, []string{"117.152.187.30", "240e:1::1", "10.0.0.1"})
	if loc := got["117.152.187.30"]; loc.District != "汉川市" || loc.ISP != "移动" || loc.Source != "btcn" {
		t.Fatalf("ipv4: %+v", loc)
	}
	if loc := got["240e:1::1"]; loc.City != "长春市" || loc.ISP != "电信" || loc.Source != "uapis" {
		t.Fatalf("ipv6: %+v", loc)
	}
	if !got["10.0.0.1"].IsPrivate {
		t.Fatal("private ip must not be sent")
	}
	if len(uapisIPs) != 1 || uapisIPs[0] != "240e:1::1" {
		t.Fatalf("uapis should only fill ipv6: %v", uapisIPs)
	}

	// 美团对国内地址连续说查不到，三轮后停用
	for i, ip := range []string{"1.1.1.2", "1.1.1.3"} {
		s.ResolveMany(ctx, []string{ip})
		if want := int32(i + 2); meituanCalls.Load() != want {
			t.Fatalf("meituan calls %d, want %d", meituanCalls.Load(), want)
		}
	}
	s.ResolveMany(ctx, []string{"1.1.1.5"})
	if meituanCalls.Load() != 3 {
		t.Fatalf("meituan should be paused, calls %d", meituanCalls.Load())
	}
	if loc := s.Resolve(ctx, "1.1.1.5"); loc.Region != "湖北省" {
		t.Fatalf("btcn still answers: %+v", loc)
	}
}

func TestLocationSourcePriority(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/meituan"):
			_, _ = w.Write([]byte(`{"data":{"lat":22.54,"lng":113.93,"ip":"1.2.3.4","rgeo":{"country":"中国","province":"广东省","adcode":"440305","city":"深圳市","district":"南山区"}}}`))
		case strings.HasPrefix(r.URL.Path, "/btcn"):
			_, _ = w.Write([]byte(`{"1.2.3.4":{"country":"中国","province":"广东","city":"深圳","carrier":"电信","en_short_code":"CN"}}`))
		case strings.HasPrefix(r.URL.Path, "/uapis"):
			t.Error("uapis must not be called when free sources answered")
		}
	}))
	defer srv.Close()
	restore := swapLocationEndpoints(srv.URL)
	defer restore()

	s := NewLocationService(nil, nil, "", config.GeoIPConfig{Enabled: true})
	s.http = srv.Client()
	loc := s.Resolve(context.Background(), "1.2.3.4")
	if loc.Source != "meituan+btcn" || loc.District != "南山区" || loc.ISP != "电信" || loc.Location != "中国 广东省 深圳市 南山区" {
		t.Fatalf("priority merge: %+v", loc)
	}

	// 只配宝塔时美团不参与
	s = NewLocationService(nil, nil, "", config.GeoIPConfig{Enabled: true, Providers: []string{"btcn"}})
	s.http = srv.Client()
	if loc := s.Resolve(context.Background(), "1.2.3.4"); loc.Source != "btcn" || loc.District != "" {
		t.Fatalf("btcn only: %+v", loc)
	}
}

func swapLocationEndpoints(base string) func() {
	oldMeituan, oldLatLng, oldBtCN, oldUAPIs := meituanIPEndpoint, meituanLatLngEndpoint, btcnEndpoint, uapisEndpoint
	meituanIPEndpoint, meituanLatLngEndpoint = base+"/meituan/ip", base+"/meituan/latlng"
	btcnEndpoint, uapisEndpoint = base+"/btcn", base+"/uapis"
	return func() {
		meituanIPEndpoint, meituanLatLngEndpoint, btcnEndpoint, uapisEndpoint = oldMeituan, oldLatLng, oldBtCN, oldUAPIs
	}
}

// 真实接口：LOCATION_LIVE=1 时运行，逐个来源打印结果。
func TestLocationProvidersLive(t *testing.T) {
	if os.Getenv("LOCATION_LIVE") == "" {
		t.Skip("LOCATION_LIVE not set")
	}
	s := NewLocationService(nil, nil, "", config.GeoIPConfig{Enabled: true, UAPIsKey: os.Getenv("GEOIP_UAPIS_KEY")})
	s.http = http.DefaultClient
	ctx := context.Background()
	for _, ip := range []string{"117.152.187.30", "60.12.1.1", "240e:1::1", "8.8.8.8", "1.36.0.1"} {
		if loc, ok, err := s.lookupMeituan(ctx, ip); err != nil || ok {
			t.Logf("meituan %s => %s (%v, %v)", ip, loc.Location, ok, err)
		} else {
			t.Logf("meituan %s => not found", ip)
		}
		loc, ok, err := s.lookupUAPIs(ctx, ip)
		t.Logf("uapis   %s => %s | %s | %s (%v, %v)", ip, loc.Location, loc.ISP, loc.Network.ASN, ok, err)
		time.Sleep(300 * time.Millisecond)
	}
	got := s.ResolveMany(ctx, []string{"117.152.187.30", "240e:1::1", "8.8.8.8"})
	for ip, loc := range got {
		t.Logf("chain   %s => %s | %s | %s", ip, loc.Location, loc.ISP, loc.Source)
	}
	if got["117.152.187.30"].Region != "湖北省" || got["240e:1::1"].Region == "" {
		t.Fatalf("unexpected: %+v", got)
	}
}
