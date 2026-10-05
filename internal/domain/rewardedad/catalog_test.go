package rewardedad

import (
	"strings"
	"testing"
	"time"

	cardkeydomain "aegis/internal/domain/cardkey"
)

// 文档给的签名算法：sha256(securityKey + ":" + transId)。
// 期望值是 ToBid 系平台文档里公开的示例向量，独立于本实现；签名对不上时
// 错误只会说「不对」而不说「哪里不对」，所以要有字面量钉着。
func TestHuijingSignMatchesDocumentedAlgorithm(t *testing.T) {
	const (
		key   = "D46C4341E83F33DB0DF2BC42816F21B7"
		trans = "a78f36ed-95e5-4049-9647-dfc87e6df0e1"
		want  = "db10d4a82a32597a101841988bbee1bf5f3ebca9a512456817e301d89c721270"
	)
	if got := HuijingSign(key, trans); got != want {
		t.Fatalf("HuijingSign = %q, want %q", got, want)
	}
	if !VerifyHuijingSign(key, trans, strings.ToUpper(want)) {
		t.Fatal("大写的签名也应当通过")
	}
	got := HuijingSign("key", "trans")
	if VerifyHuijingSign("", "trans", got) || VerifyHuijingSign("key", "trans", "") {
		t.Fatal("密钥或签名为空时必须拒绝")
	}
	if VerifyHuijingSign("key", "other", got) {
		t.Fatal("换了 transId 的签名不应通过")
	}
}

func TestRewardCatalogOnlyOffersAllowedTypes(t *testing.T) {
	catalog := RewardCatalog()
	if len(catalog) != len(allowedRewardTypes) {
		t.Fatalf("目录应包含 %d 档，得到 %d", len(allowedRewardTypes), len(catalog))
	}
	for _, spec := range catalog {
		if spec.Type == cardkeydomain.RewardBalance || spec.Type == cardkeydomain.RewardDeviceSlots {
			t.Fatalf("%s 不应开放给激励广告", spec.Type)
		}
	}
	// 每一档都必须是卡密目录里真实存在的，否则控制台配得出、发放处认不得。
	for _, kind := range allowedRewardTypes {
		if _, ok := cardkeydomain.FindRewardSpec(kind); !ok {
			t.Fatalf("%s 不在卡密权益目录里", kind)
		}
	}
}

func TestValidateScenes(t *testing.T) {
	valid := Scene{Key: "vip", Name: "看广告领会员", PlacementID: "4867493348237563", Enabled: true,
		Rewards: []Reward{{Type: cardkeydomain.RewardVipDays, Amount: 1}}, DailyLimit: 3, CooldownSeconds: 60}
	if err := ValidateScenes([]Scene{valid}); err != nil {
		t.Fatalf("合法场景被拒：%v", err)
	}

	cases := map[string]func(s *Scene){
		"大写标识":   func(s *Scene) { s.Key = "VIP" },
		"空名称":    func(s *Scene) { s.Name = "" },
		"广告位非法":  func(s *Scene) { s.PlacementID = "a b" },
		"负的日限额":  func(s *Scene) { s.DailyLimit = -1 },
		"冷却过长":   func(s *Scene) { s.CooldownSeconds = MaxCooldownSeconds + 1 },
		"没有权益":   func(s *Scene) { s.Rewards = nil },
		"余额不开放":  func(s *Scene) { s.Rewards = []Reward{{Type: cardkeydomain.RewardBalance}} },
		"设备位不开放": func(s *Scene) { s.Rewards = []Reward{{Type: cardkeydomain.RewardDeviceSlots, Amount: 1}} },
		"会员天数越界": func(s *Scene) { s.Rewards = []Reward{{Type: cardkeydomain.RewardVipDays, Amount: 0}} },
	}
	for name, mutate := range cases {
		scene := valid
		scene.Rewards = append([]Reward(nil), valid.Rewards...)
		mutate(&scene)
		if err := ValidateScenes([]Scene{scene}); err == nil {
			t.Errorf("%s：应当被拒绝", name)
		}
	}

	if err := ValidateScenes([]Scene{valid, valid}); err == nil {
		t.Error("重复的场景标识应当被拒绝")
	}
}

func TestSceneFromExtra(t *testing.T) {
	cases := map[string]string{
		``:                                    "",
		`{"aegisScene":"VIP","HJ_BTD":"x"}`:   "vip",
		`{"scene":"points"}`:                  "points",
		`{"HJ_BTD":"x"}`:                      "",
		`aegisScene=vip&HJ_BTD=123`:           "vip",
		`not json and not query`:              "",
		`{"aegis_scene":" daily ","other":1}`: "daily",
		`{"aegisScene":123}`:                  "",
	}
	for input, want := range cases {
		if got := SceneFromExtra(input); got != want {
			t.Errorf("SceneFromExtra(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSettlePolicyReady(t *testing.T) {
	now := time.Now()
	serverOnly := &View{ServerVerifiedAt: &now}
	clientOnly := &View{ClientReportedAt: &now}
	both := &View{ServerVerifiedAt: &now, ClientReportedAt: &now}

	dual := SettlePolicy{VerifyMode: VerifyDual}
	if dual.Ready(serverOnly) || dual.Ready(clientOnly) || !dual.Ready(both) {
		t.Fatal("dual 模式必须两方到齐")
	}
	server := SettlePolicy{VerifyMode: VerifyServer}
	if !server.Ready(serverOnly) || server.Ready(clientOnly) {
		t.Fatal("server 模式只认服务端回调")
	}
	client := SettlePolicy{VerifyMode: VerifyClient}
	if client.Ready(serverOnly) || !client.Ready(clientOnly) {
		t.Fatal("client 模式只认客户端上报")
	}
	force := SettlePolicy{VerifyMode: VerifyDual, ForceGrant: true}
	if !force.Ready(&View{}) {
		t.Fatal("管理端补发不等另一方")
	}
}

func TestValidTransID(t *testing.T) {
	for _, ok := range []string{"a78f36ed-95e5-4049-9647-dfc87e6df0e1", "123456", "abc.def:ghi_jk"} {
		if !ValidTransID(ok) {
			t.Errorf("%q 应当合法", ok)
		}
	}
	for _, bad := range []string{"", "12345", "has space", strings.Repeat("a", 129), "<script>"} {
		if ValidTransID(bad) {
			t.Errorf("%q 应当非法", bad)
		}
	}
}
