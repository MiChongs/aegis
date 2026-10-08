package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	devicedomain "aegis/internal/domain/device"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// 没有字典（服务为 nil）时的兜底：调用方不判空也能拿到一个可展示的 deviceInfo。
func TestDescribeFallbacksWithoutDictionary(t *testing.T) {
	var devices *DeviceMarketingService
	ctx := context.Background()
	cases := []struct {
		name string
		in   devicedomain.DescribeInput
		want devicedomain.Info
	}{
		{"上报了型号", devicedomain.DescribeInput{Model: "SM-X999", Platform: "Android"},
			devicedomain.Info{Name: "SM-X999", Identifier: "SM-X999", Platform: "android", Source: "client"}},
		{"旧会话只有名称", devicedomain.DescribeInput{Name: "Pixel 9", UserAgent: "okhttp (Linux; Android 15)"},
			devicedomain.Info{Name: "Pixel 9", Platform: "android", Source: "client"}},
		{"只有 UA", devicedomain.DescribeInput{UserAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/130"},
			devicedomain.Info{Name: "Chrome on Windows", Platform: "windows", Source: "user_agent"}},
		{"什么都没有", devicedomain.DescribeInput{}, devicedomain.Info{Source: "unknown"}},
	}
	for _, tc := range cases {
		if got := devices.Describe(ctx, tc.in); got != tc.want {
			t.Errorf("%s：得到 %+v，期望 %+v", tc.name, got, tc.want)
		}
	}
}

func TestMarketingDisplayName(t *testing.T) {
	cases := map[string]devicedomain.MarketingName{
		"Samsung Galaxy S21": {Manufacturer: "Samsung", MarketingName: "Galaxy S21"},
		"Xiaomi 13":          {Manufacturer: "Xiaomi", MarketingName: "Xiaomi 13"}, // 营销名已含厂商，不重复
		"iPhone 15":          {MarketingName: "iPhone 15"},
	}
	for want, item := range cases {
		if got := item.DisplayName(); got != want {
			t.Errorf("得到 %q，期望 %q", got, want)
		}
	}
}

func TestNormalizePlatform(t *testing.T) {
	for raw, want := range map[string]string{
		"Android": "android", "iPadOS": "ios", "OpenHarmony": "harmonyos", "Win32": "windows",
		"darwin": "macos", "H5": "web", "symbian": "",
	} {
		if got := devicedomain.NormalizePlatform(raw); got != want {
			t.Errorf("%q → %q，期望 %q", raw, got, want)
		}
	}
}

// captureJetStream 只实现 Publish，把会话签发时发出的事件截下来。
type captureJetStream struct {
	nats.JetStreamContext
	published map[string][][]byte
}

func (c *captureJetStream) Publish(subject string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	c.published[subject] = append(c.published[subject], data)
	return &nats.PubAck{}, nil
}

// 设备字典映射到用户接口的端到端测试：真实 Postgres + Redis。
//
//	AEGIS_TEST_PG_DSN=postgres://postgres:aegis@127.0.0.1:25432/postgres?sslmode=disable \
//	AEGIS_TEST_REDIS_ADDR=127.0.0.1:26379 go test ./internal/service -run TestDeviceInfoIntegration
//
// 链路：带原始型号登录 → 会话保存型号与平台 → /me/sessions 与管理端会话列表给出 deviceInfo →
// 登录事件经 worker 落库 → 登录记录给出 deviceInfo → 修改字典后立即生效 → 刷新沿用原始型号。
func TestDeviceInfoIntegration(t *testing.T) {
	redisAddr := os.Getenv("AEGIS_TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("未设置 AEGIS_TEST_REDIS_ADDR")
	}
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	rdb := redislib.NewClient(&redislib.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	sessions := redisrepo.NewSessionRepository(rdb, "devinfo-test-"+time.Now().Format("150405.000000"))
	js := &captureJetStream{published: map[string][][]byte{}}
	publisher := event.NewPublisher(js)
	log := zap.NewNop()

	cfg := config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("s", 48), TTL: time.Hour, RefreshTTL: 24 * time.Hour}}
	devices := NewDeviceMarketingService(log, pg)
	appService := NewAppService(log, pg, sessions)
	appService.SetDeviceDirectory(devices)
	auth := NewAuthService(cfg, log, pg, sessions, publisher, appService, nil)
	auth.SetDeviceDirectory(devices)
	users := NewUserService(log, pg, sessions, publisher, nil)
	users.SetDeviceDirectory(devices)

	entry, err := devices.Create(ctx, devicedomain.CreateInput{
		Platform: "android", Identifier: "AEGIS-T1", MarketingName: "Galaxy Test", Manufacturer: "Samsung",
		ManufacturerIconURL: "https://cdn.example.com/samsung.svg", DeviceImageURL: "https://cdn.example.com/t1.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := pg.UpsertApp(ctx, appdomain.App{Name: "设备测试", Status: true, RegisterStatus: true, LoginStatus: true})
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!"), bcrypt.MinCost)
	if _, err := pg.CreateUser(ctx, app.ID, "dora", string(hash)); err != nil {
		t.Fatal(err)
	}

	// 登录入口会把原始型号与平台挂到上下文上（见 transport/http enrichDeviceFromDict）
	loginCtx := devicedomain.WithClient(ctx, devicedomain.Client{Model: "AEGIS-T1", Platform: "android"})
	result, err := auth.PasswordLogin(loginCtx, app.ID, "dora", "Passw0rd!", "dev-1", entry.DisplayName(), "127.0.0.1", "okhttp/4.12")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.ValidateAccessToken(ctx, result.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if session.DeviceModel != "AEGIS-T1" || session.DevicePlatform != "android" {
		t.Fatalf("会话应保存原始型号与平台：%+v", session)
	}

	assertInfo := func(where string, info *devicedomain.Info, wantName string) {
		t.Helper()
		if info == nil || !info.Matched || info.Name != wantName || info.Identifier != "AEGIS-T1" ||
			info.Manufacturer != "Samsung" || info.ManufacturerIconURL == "" || info.DeviceImageURL == "" ||
			info.DictionaryID != entry.ID || info.Source != devicedomain.InfoSourceDictionary || info.Platform != "android" {
			t.Fatalf("%s 的 deviceInfo 不对：%+v", where, info)
		}
	}

	mine, err := users.ListSessions(ctx, session)
	if err != nil || len(mine.Items) != 1 {
		t.Fatalf("会话列表：%v %v", mine, err)
	}
	assertInfo("/me/sessions", mine.Items[0].DeviceInfo, "Samsung Galaxy Test")
	if mine.Items[0].Device != "Samsung Galaxy Test" {
		t.Fatalf("会话应带设备展示名：%q", mine.Items[0].Device)
	}
	adminView, err := users.AdminListUserSessions(ctx, app.ID, session.UserID)
	if err != nil || len(adminView) != 1 {
		t.Fatalf("管理端会话列表：%v %v", adminView, err)
	}
	assertInfo("管理端会话", adminView[0].DeviceInfo, "Samsung Galaxy Test")

	// 登录事件由 worker 落库，登录记录据 metadata 还原设备
	events := NewWorkerEventService(log, pg, sessions)
	for _, raw := range js.published[event.SubjectAuthLoginAuditRequested] {
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if err := events.HandleAuthLoginAudit(ctx, payload); err != nil {
			t.Fatal(err)
		}
	}
	audits, err := users.ListLoginAudits(ctx, session, userdomain.LoginAuditQuery{})
	if err != nil || len(audits.Items) == 0 {
		t.Fatalf("登录记录：%v %v", audits, err)
	}
	assertInfo("/me/audits/login", audits.Items[0].DeviceInfo, "Samsung Galaxy Test")
	appAudits, err := appService.ListLoginAudits(ctx, app.ID, appdomain.LoginAuditQuery{})
	if err != nil || len(appAudits.Items) == 0 {
		t.Fatalf("应用登录记录：%v %v", appAudits, err)
	}
	assertInfo("应用登录记录", appAudits.Items[0].DeviceInfo, "Samsung Galaxy Test")

	// 管理员修正字典：已有会话立即显示新名称（缓存随写入失效）
	newName := "Galaxy Test Ultra"
	if _, err := devices.Update(ctx, entry.ID, devicedomain.UpdateInput{MarketingName: &newName}); err != nil {
		t.Fatal(err)
	}
	mine, _ = users.ListSessions(ctx, session)
	assertInfo("改字典后的 /me/sessions", mine.Items[0].DeviceInfo, "Samsung Galaxy Test Ultra")

	// 刷新令牌签发的新会话沿用原始型号
	refreshed, err := auth.Refresh(ctx, result.RefreshToken, "dev-1", "127.0.0.1", "okhttp/4.12")
	if err != nil {
		t.Fatal(err)
	}
	next, err := auth.ValidateAccessToken(ctx, refreshed.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if next.DeviceModel != "AEGIS-T1" || next.DevicePlatform != "android" {
		t.Fatalf("刷新后的会话应沿用原始型号：%+v", next)
	}

	// 未收录的型号：未命中也缓存，补录之后立刻能命中
	miss := devices.Describe(ctx, devicedomain.DescribeInput{Model: "AEGIS-T2", Platform: "android"})
	if miss.Matched || miss.Name != "AEGIS-T2" || miss.Source != devicedomain.InfoSourceClient {
		t.Fatalf("未收录型号应原样展示：%+v", miss)
	}
	if _, err := devices.Create(ctx, devicedomain.CreateInput{Platform: "android", Identifier: "AEGIS-T2", MarketingName: "Redmi Test", Manufacturer: "Xiaomi"}); err != nil {
		t.Fatal(err)
	}
	if hit := devices.Describe(ctx, devicedomain.DescribeInput{Model: "AEGIS-T2", Platform: "android"}); !hit.Matched || hit.Name != "Xiaomi Redmi Test" {
		t.Fatalf("补录后应立即命中：%+v", hit)
	}
}
