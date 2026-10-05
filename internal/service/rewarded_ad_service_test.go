package service

import (
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	rewardedad "aegis/internal/domain/rewardedad"
)

func newRewardedAdServiceForTest(masterKey string) *RewardedAdService {
	var cfg config.Config
	cfg.Security.MasterKey = masterKey
	cfg.APIBaseURL = "https://api.example.com/"
	return NewRewardedAdService(nil, nil, cfg)
}

// 回调里的 userId 只能是我们发出去的那一个：换 ID、换应用、改签名都认不出。
func TestRewardedAdUserTokenRoundTrip(t *testing.T) {
	svc := newRewardedAdServiceForTest("master")
	token := svc.userToken(7, 42)
	if !strings.HasPrefix(token, "42.") || len(token) != len("42.")+rewardedAdUserTagLen {
		t.Fatalf("用户标识形态不对：%q", token)
	}
	if got := svc.parseUserToken(7, token); got != 42 {
		t.Fatalf("parseUserToken = %d, want 42", got)
	}
	if got := svc.parseUserToken(7, strings.ToUpper(token)); got != 42 {
		t.Fatalf("签名部分大小写不敏感，得到 %d", got)
	}

	forged := "43" + token[2:]
	cases := map[string]string{
		"换了用户 ID": forged,
		"缺签名":     "42",
		"空":       "",
		"非数字":     "abc." + token[3:],
		"负数":      "-42." + token[3:],
		"签名被改":    token[:len(token)-1] + "0",
	}
	for name, value := range cases {
		if value == token {
			continue
		}
		if got := svc.parseUserToken(7, value); got != 0 {
			t.Errorf("%s：应当认不出，得到 %d", name, got)
		}
	}
	if got := svc.parseUserToken(8, token); got != 0 {
		t.Errorf("别的应用不应认出这个标识，得到 %d", got)
	}
	other := newRewardedAdServiceForTest("another-master")
	if got := other.parseUserToken(7, token); got != 0 {
		t.Errorf("换了主密钥的部署不应认出这个标识，得到 %d", got)
	}
}

func TestRewardedAdCallbackURL(t *testing.T) {
	svc := newRewardedAdServiceForTest("master")
	url, absolute := svc.callbackURL("voyage")
	if !absolute || url != "https://api.example.com/api/apps/voyage/ads/huijing/callback" {
		t.Fatalf("callbackURL = %q (%v)", url, absolute)
	}
	svc.apiBaseURL = ""
	url, absolute = svc.callbackURL("voyage")
	if absolute || url != "/api/apps/voyage/ads/huijing/callback" {
		t.Fatalf("未配置 API_BASE_URL 时应只给路径，得到 %q", url)
	}
}

func TestBuildClientScene(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	scene := rewardedad.Scene{Key: "vip", Name: "看广告领会员", PlacementID: "p", Enabled: true,
		DailyLimit: 3, CooldownSeconds: 60}

	usage := &rewardedad.Usage{TodayByScene: map[string]int{"vip": 1},
		LastByScene: map[string]time.Time{"vip": now.Add(-20 * time.Second)}}
	item := buildClientScene(scene, usage, -1, nil, now)
	if item.Remaining != 2 || item.TodayCount != 1 {
		t.Fatalf("剩余次数不对：%+v", item)
	}
	if item.CooldownRemaining != 40 || item.Available {
		t.Fatalf("冷却中应不可看，剩余 40 秒：%+v", item)
	}

	// 应用级总限额更紧时以它为准。
	item = buildClientScene(scene, &rewardedad.Usage{TodayByScene: map[string]int{}, LastByScene: map[string]time.Time{}}, 1, nil, now)
	if item.Remaining != 1 || !item.Available {
		t.Fatalf("应用级只剩 1 次时场景也只剩 1 次：%+v", item)
	}

	// 用满。
	usage = &rewardedad.Usage{TodayByScene: map[string]int{"vip": 3}, LastByScene: map[string]time.Time{"vip": now.Add(-time.Hour)}}
	item = buildClientScene(scene, usage, -1, nil, now)
	if item.Remaining != 0 || item.Available || item.CooldownRemaining != 0 {
		t.Fatalf("用满后不可看且不再显示冷却：%+v", item)
	}

	// 不限次数。
	scene.DailyLimit = 0
	scene.CooldownSeconds = 0
	item = buildClientScene(scene, usage, -1, nil, now)
	if item.Remaining != -1 || !item.Available {
		t.Fatalf("不限次数时 remaining = -1 且可看：%+v", item)
	}
}

func TestRewardedAdDayStart(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	// UTC 16:30 已经是东八区的第二天 00:30。
	now := time.Date(2026, 10, 5, 16, 30, 0, 0, time.UTC)
	start := rewardedAdDayStart(now, loc)
	want := time.Date(2026, 10, 6, 0, 0, 0, 0, loc)
	if !start.Equal(want) {
		t.Fatalf("rewardedAdDayStart = %v, want %v", start, want)
	}
}

func TestRewardedAdKeyHintNeverLeaksKey(t *testing.T) {
	key := "D46C4341E83F33DB0DF2BC42816F21B7"
	hint := rewardedAdKeyHint(key)
	if strings.Contains(hint, key[:8]) || !strings.HasSuffix(hint, "21B7") {
		t.Fatalf("提示只应露出末 4 位：%q", hint)
	}
	if rewardedAdKeyHint("short") != "已配置" {
		t.Fatal("短密钥不露任何字符")
	}
}
