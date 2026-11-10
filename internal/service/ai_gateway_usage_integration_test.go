package service

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
)

// 用户端用量统计（GET /ai/usage）对真实 Postgres 的聚合：状态口径、token 与扣费合计、
// 按设置时区的日 / 小时分桶与补零、按模型 / 密钥 / 接口 / 扣费来源分组、密钥与模型过滤、
// 只算本人与时间段内的调用。
//
// AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/service -run TestAIGatewayUsageIntegration
func TestAIGatewayUsageIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	gw := NewAIGatewayService(zap.NewNop(), pg, nil, nil)

	const appID = int64(31500)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ai-usage-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	settings := aigateway.DefaultSettings()
	settings.Enabled = true
	settings.Timezone = "Asia/Shanghai"
	if _, err := gw.SaveSettings(ctx, appID, settings, "test"); err != nil {
		t.Fatalf("保存设置失败：%v", err)
	}
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	// 上海 2026-11-10 15:20
	gw.now = func() time.Time { return time.Date(2026, 11, 10, 15, 20, 0, 0, shanghai) }

	me := insertTestUser(t, ctx, pool, appID, "usage-me", "0")
	other := insertTestUser(t, ctx, pool, appID, "usage-other", "0")

	var keyA int64
	if err := pool.QueryRow(ctx, `INSERT INTO ai_gateway_keys (appid, user_id, name, key_hash, key_hint)
VALUES ($1, $2, '脚本', repeat('a', 64), 'AAAA') RETURNING id`, appID, me).Scan(&keyA); err != nil {
		t.Fatal(err)
	}

	seq := 0
	insert := func(user, keyID int64, hint, endpoint, model, status string, at time.Time,
		in, out, images, charged int64, charges string, latency, firstToken int, stream bool) {
		t.Helper()
		seq++
		if _, err := pool.Exec(ctx, `INSERT INTO ai_gateway_requests (request_no, appid, user_id, key_id, key_hint, auth_kind,
  endpoint, model, stream, status, input_tokens, output_tokens, images, charged, charges, latency_ms, first_token_ms,
  created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, 'key', $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, $16, $17::timestamptz, $17::timestamptz + interval '1 hour')`,
			fmt.Sprintf("usage-%03d", seq), appID, user, keyID, hint, endpoint, model, stream,
			status, in, out, images, charged, charges, latency, firstToken, at); err != nil {
			t.Fatalf("插入调用记录失败：%v", err)
		}
	}
	day := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, shanghai) }
	integral := func(micro int64) string {
		return `[{"source":"integral","micro":` + strconv.FormatInt(micro, 10) + `,"units":` + strconv.FormatInt(micro/1_000_000, 10) + `}]`
	}

	// 今天：两次成功（一次流式）、一次中途断开、一次失败、一次进行中
	insert(me, keyA, "AAAA", "chat.completions", "gpt-x", "succeeded", day(10, 9), 1000, 500, 0, 3_000_000, integral(3_000_000), 800, 0, false)
	insert(me, keyA, "AAAA", "chat.completions", "gpt-x", "succeeded", day(10, 14), 2000, 1000, 0, 6_000_000, integral(6_000_000), 1200, 300, true)
	insert(me, 0, "", "messages", "claude-y", "aborted", day(10, 14), 300, 100, 0, 1_500_000, `[{"source":"credit","micro":1500000}]`, 2000, 500, true)
	insert(me, keyA, "AAAA", "chat.completions", "gpt-x", "failed", day(10, 15), 0, 0, 0, 0, `[]`, 100, 0, false)
	insert(me, keyA, "AAAA", "chat.completions", "gpt-x", "reserved", day(10, 15), 0, 0, 0, 0, `[]`, 0, 0, true)
	// 三天前：一张图与一次退款
	insert(me, keyA, "AAAA", "images.generations", "img-z", "succeeded", day(7, 20), 0, 0, 2, 10_000_000, integral(10_000_000), 9000, 0, false)
	insert(me, 0, "", "chat.completions", "gpt-x", "refunded", day(7, 21), 400, 200, 0, 2_000_000, integral(2_000_000), 700, 0, false)
	// 已删除的密钥（密钥表里没有）
	insert(me, 99999, "DEAD", "chat.completions", "gpt-x", "succeeded", day(8, 1), 10, 10, 0, 1_000_000, integral(1_000_000), 600, 0, false)
	// 区间外与别人的调用不算
	insert(me, keyA, "AAAA", "chat.completions", "gpt-x", "succeeded", day(1, 12), 99999, 99999, 0, 99_000_000, integral(99_000_000), 1, 0, false)
	insert(other, 0, "", "chat.completions", "gpt-x", "succeeded", day(10, 10), 77777, 77777, 0, 77_000_000, integral(77_000_000), 1, 0, false)

	session := &authdomain.Session{AppID: appID, UserID: me}

	week, err := gw.MyUsage(ctx, session, "7d", nil, "")
	if err != nil {
		t.Fatalf("7 天统计失败：%v", err)
	}
	tt := week.Totals
	if tt.Requests != 8 || tt.Succeeded != 5 || tt.Aborted != 1 || tt.Failed != 1 || tt.Refunded != 1 || tt.Pending != 1 || tt.Streamed != 3 {
		t.Fatalf("状态计数不对：%+v", tt)
	}
	if tt.InputTokens != 3710 || tt.OutputTokens != 1810 || tt.Images != 2 {
		t.Fatalf("token / 图片合计不对：%+v", tt)
	}
	// 扣费只算成功与中途断开：3 + 6 + 1.5 + 10 + 1 = 21.5；退款单列 2
	if tt.Charged.String() != "21.5" || tt.Refund.String() != "2" {
		t.Fatalf("扣费合计不对：charged=%s refund=%s", tt.Charged, tt.Refund)
	}
	if tt.AvgFirstTokenMs != 400 || tt.AvgLatencyMs == 0 || tt.P95LatencyMs < tt.AvgLatencyMs {
		t.Fatalf("延迟统计不对：%+v", tt)
	}
	if week.Bucket != aigateway.UsageBucketDay || len(week.Series) != 7 || week.Timezone != "Asia/Shanghai" {
		t.Fatalf("应按日 7 个桶：bucket=%s len=%d tz=%s", week.Bucket, len(week.Series), week.Timezone)
	}
	byDate := map[string]aigateway.UsagePoint{}
	for _, point := range week.Series {
		byDate[point.Time.In(shanghai).Format("01-02")] = point
	}
	if p := byDate["11-10"]; p.Requests != 5 || p.Failed != 1 || p.InputTokens != 3300 || p.Charged.String() != "10.5" {
		t.Fatalf("今天的桶不对：%+v", p)
	}
	if p := byDate["11-07"]; p.Requests != 2 || p.Charged.String() != "10" {
		t.Fatalf("11-07 的桶不对（退款不计扣费）：%+v", p)
	}
	if p := byDate["11-09"]; p.Requests != 0 || !p.Charged.IsZero() {
		t.Fatalf("没有调用的日子应补零：%+v", p)
	}

	models := map[string]aigateway.UsageGroup{}
	for _, g := range week.ByModel {
		models[g.Key] = g
	}
	if g := models["gpt-x"]; g.Requests != 6 || g.Failed != 1 || g.InputTokens != 3410 {
		t.Fatalf("gpt-x 分组不对：%+v", g)
	}
	// gpt-x 与 img-z 扣费都是 10 点，并列时按调用数降序；claude-y 1.5 点垫底
	if len(week.ByModel) != 3 || week.ByModel[0].Key != "gpt-x" || week.ByModel[1].Key != "img-z" || week.ByModel[2].Key != "claude-y" {
		t.Fatalf("模型分组应按扣费、再按调用数降序：%+v", week.ByModel)
	}
	keys := map[string]aigateway.UsageGroup{}
	for _, g := range week.ByKey {
		keys[g.Key] = g
	}
	if g := keys[strconv.FormatInt(keyA, 10)]; g.Label != "脚本" || g.Hint != "AAAA" || g.Requests != 5 {
		t.Fatalf("密钥分组不对：%+v", g)
	}
	if g := keys["0"]; g.Requests != 2 {
		t.Fatalf("登录令牌分组不对：%+v", g)
	}
	if g := keys["99999"]; g.Label != "" || g.Hint != "DEAD" {
		t.Fatalf("已删除的密钥应只有尾号：%+v", g)
	}
	endpoints := map[string]int64{}
	for _, g := range week.ByEndpoint {
		endpoints[g.Key] = g.Requests
	}
	if endpoints["chat.completions"] != 6 || endpoints["messages"] != 1 || endpoints["images.generations"] != 1 {
		t.Fatalf("接口分组不对：%+v", endpoints)
	}
	sources := map[string]string{}
	for _, s := range week.BySource {
		sources[s.Source] = s.Amount.String()
	}
	if sources["integral"] != "20" || sources["credit"] != "1.5" {
		t.Fatalf("扣费来源不对：%+v", sources)
	}

	today, err := gw.MyUsage(ctx, session, "today", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if today.Bucket != aigateway.UsageBucketHour || len(today.Series) != 24 || today.Totals.Requests != 5 {
		t.Fatalf("今天应按小时 24 个桶、5 次调用：bucket=%s len=%d %+v", today.Bucket, len(today.Series), today.Totals)
	}
	if p := today.Series[14]; p.Requests != 2 || p.InputTokens != 2300 {
		t.Fatalf("14 点的桶不对：%+v", p)
	}

	session0 := int64(0)
	viaSession, err := gw.MyUsage(ctx, session, "7d", &session0, "")
	if err != nil || viaSession.Totals.Requests != 2 {
		t.Fatalf("keyId=0 应只统计登录令牌的调用：%+v %v", viaSession, err)
	}
	onlyImg, err := gw.MyUsage(ctx, session, "7d", nil, "img-z")
	if err != nil || onlyImg.Totals.Requests != 1 || onlyImg.Totals.Images != 2 {
		t.Fatalf("按模型过滤不对：%+v %v", onlyImg, err)
	}

	month, err := gw.MyUsage(ctx, session, "30d", nil, "")
	if err != nil || len(month.Series) != 30 || month.Totals.Requests != 9 {
		t.Fatalf("30 天应包含 11-01 那一笔：%+v %v", month.Totals, err)
	}
}
