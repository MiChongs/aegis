package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	notifydomain "aegis/internal/domain/notify"
	ticketdomain "aegis/internal/domain/ticket"
)

// 统一出口里三块最"沉默"的逻辑：签名算错只会收到飞书报错、
// 静默窗口算错会让人半夜被吵醒、订阅过滤算错会漏掉告警。

func TestFeishuSignMatchesOfficialAlgorithm(t *testing.T) {
	timestamp := "1700000000"
	secret := "test-secret"

	// 官方算法：以 "{timestamp}\n{secret}" 为 key，对空串做 HMAC-SHA256，再 base64
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	mac.Write([]byte(""))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if got := feishuSign(timestamp, secret); got != want {
		t.Fatalf("飞书签名不匹配：got=%s want=%s", got, want)
	}
}

func TestInQuietHoursAcrossMidnight(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("时区数据不可用，跳过：%v", err)
	}
	quiet := notifydomain.QuietHours{Timezone: "Asia/Shanghai", Start: "23:00", End: "08:00"}

	cases := []struct {
		hour int
		want bool
	}{
		{23, true},  // 窗口起点
		{2, true},   // 跨零点后
		{7, true},   // 结束前一小时
		{8, false},  // 结束点本身不在窗口内
		{12, false}, // 白天
		{22, false}, // 起点前一小时
	}
	for _, tc := range cases {
		now := time.Date(2026, 8, 10, tc.hour, 0, 0, 0, loc)
		if got := inQuietHours(quiet, now); got != tc.want {
			t.Fatalf("%02d:00 静默判定错误：got=%v want=%v", tc.hour, got, tc.want)
		}
	}
}

func TestInQuietHoursSameDayWindow(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("时区数据不可用，跳过：%v", err)
	}
	quiet := notifydomain.QuietHours{Timezone: "Asia/Shanghai", Start: "12:00", End: "14:00"}
	if !inQuietHours(quiet, time.Date(2026, 8, 10, 13, 0, 0, 0, loc)) {
		t.Fatal("窗口内应判定为静默")
	}
	if inQuietHours(quiet, time.Date(2026, 8, 10, 15, 0, 0, 0, loc)) {
		t.Fatal("窗口外不应判定为静默")
	}
}

func TestSubscriptionMatchesPriorityFloor(t *testing.T) {
	hub := &NotifyHub{}
	sub := &notifydomain.Subscription{MinPriority: "high"}

	if hub.subscriptionMatches(sub, &notifydomain.Event{Priority: ticketdomain.PriorityNormal}) {
		t.Fatal("低于下限的优先级不应命中订阅")
	}
	if !hub.subscriptionMatches(sub, &notifydomain.Event{Priority: ticketdomain.PriorityUrgent}) {
		t.Fatal("高于下限的优先级应命中订阅")
	}
	// 事件不带优先级时不做该维度过滤（如渠道测试消息）
	if !hub.subscriptionMatches(sub, &notifydomain.Event{}) {
		t.Fatal("事件无优先级时不应被优先级过滤挡掉")
	}
}

func TestSubscriptionMatchesCategoryWhitelist(t *testing.T) {
	hub := &NotifyHub{}
	sub := &notifydomain.Subscription{CategoryIDs: []int64{7, 9}}

	hit := int64(9)
	if !hub.subscriptionMatches(sub, &notifydomain.Event{CategoryID: &hit}) {
		t.Fatal("白名单内的分类应命中")
	}
	miss := int64(3)
	if hub.subscriptionMatches(sub, &notifydomain.Event{CategoryID: &miss}) {
		t.Fatal("白名单外的分类不应命中")
	}
	if hub.subscriptionMatches(sub, &notifydomain.Event{}) {
		t.Fatal("配置了分类白名单时，无分类事件不应命中")
	}
}

func TestSubscriptionQuietHoursLetsCriticalThrough(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("时区数据不可用，跳过：%v", err)
	}
	// 构造一个必然命中的静默窗口：覆盖当前时刻前后各一小时
	now := time.Now().In(loc)
	quiet := &notifydomain.QuietHours{
		Timezone: "Asia/Shanghai",
		Start:    now.Add(-time.Hour).Format("15:04"),
		End:      now.Add(time.Hour).Format("15:04"),
	}
	hub := &NotifyHub{}
	sub := &notifydomain.Subscription{QuietHours: quiet}

	if hub.subscriptionMatches(sub, &notifydomain.Event{Level: notifydomain.LevelInfo}) {
		t.Fatal("静默期内普通事件不应投递")
	}
	if !hub.subscriptionMatches(sub, &notifydomain.Event{Level: notifydomain.LevelCritical}) {
		t.Fatal("critical 事件必须穿透静默窗口")
	}
}

func TestRenderNotifyTemplateSubstitutesVars(t *testing.T) {
	got, err := renderNotifyTemplate("【{{.StatusLabel}}】{{.TicketNo}}", map[string]any{
		"StatusLabel": "处理中",
		"TicketNo":    "TK20260810000001",
	})
	if err != nil {
		t.Fatalf("模板渲染失败：%v", err)
	}
	if got != "【处理中】TK20260810000001" {
		t.Fatalf("渲染结果不符：%s", got)
	}
}

func TestSecretHintMasksValue(t *testing.T) {
	if got := secretHint("abcdefgh"); got != "****efgh" {
		t.Fatalf("密钥提示应只保留末 4 位，got=%s", got)
	}
	if got := secretHint("abc"); got != "***" {
		t.Fatalf("短密钥应全部掩码，got=%s", got)
	}
}
