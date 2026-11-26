package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
)

// AI 中转网关计费的端到端集成测试：真实 Postgres（全部迁移跑两遍）+ httptest 假上游，
// 经 AIProviderService 的真实通道链路调用。覆盖：
// 余额直付与零头凑整（单价 0.01 元/额度：1 额度 = 1 分，与旧的积分口径同一套算术）、失败不扣费、余额不足、
// 压低输出上限、每日免费额度、永久额度与幂等调整、
// 余额扣费与冻结、实际用量超出暂扣（补扣与 shortfall）、流式中途客户端断开与上游中断、
// 图像按实际张数计费、密钥消费上限与停用、暂扣过期回收与迟到结算、退款、并发抢余额，
// 最后核对全局不变式（没有残留暂扣、冻结归零、零头在界内、积分非负）。
//
// AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/service -run TestAIGatewayBillingIntegration
// 测试会 DROP SCHEMA public，切勿指向开发库。
func TestAIGatewayBillingIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	log := zap.NewNop()
	ai := NewAIProviderService(log, pg, "ai-gateway-test-master-key")
	gw := NewAIGatewayService(log, pg, nil, ai)
	upstream := newFakeAIUpstream(t)

	const appID = int64(31000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ai-gateway-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := ai.Save(ctx, aidomain.ConfigMutation{AppID: appID, Name: ptrTo("fake"),
		Provider: ptrTo(aidomain.ProviderCustomOpenAI), Enabled: ptrTo(true),
		Settings: map[string]string{aidomain.KeyBaseURL: upstream.server.URL + "/v1"},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "upstream-secret"}}); err != nil {
		t.Fatalf("建通道失败：%v", err)
	}

	settings := aigateway.DefaultSettings()
	settings.Enabled = true
	settings.Sources = []string{aigateway.SourceFreeDaily, aigateway.SourceCredit, aigateway.SourceWallet}
	settings.WalletRate = decimal.RequireFromString("0.01")
	settings.RPM = 0
	settings.MaxConcurrent = 100
	settings.DefaultMaxTokens = 1000
	saveSettings := func(mutate func(*aigateway.Settings)) {
		t.Helper()
		next := settings
		mutate(&next)
		if _, err := gw.SaveSettings(ctx, appID, next, "test"); err != nil {
			t.Fatalf("保存设置失败：%v", err)
		}
		settings = next
	}
	saveSettings(func(*aigateway.Settings) {})

	// 价格：输入 2000 点 / 百万 token（= 每 token 2000 微点），输出 4000。
	// 假上游报 500 输入 + 125 输出 → 1,000,000 + 500,000 = 1.5 点。
	chatPrice := func(model, upstreamModel string) aigateway.ModelInput {
		return aigateway.ModelInput{Model: model, UpstreamModel: upstreamModel, Kind: aigateway.KindChat,
			InputPrice: decimal.NewFromInt(2000), OutputPrice: decimal.NewFromInt(4000)}
	}
	for _, input := range []aigateway.ModelInput{
		chatPrice("chat", "up-ok"), chatPrice("chat-fail", "up-fail"), chatPrice("chat-big", "up-big"),
		chatPrice("chat-nousage", "up-nousage"), chatPrice("chat-stream-fail", "up-streamfail"),
		{Model: "img", UpstreamModel: "up-img", Kind: aigateway.KindImage, ImagePrice: decimal.NewFromInt(5)},
	} {
		if _, err := gw.CreateModel(ctx, appID, input); err != nil {
			t.Fatalf("建模型 %s 失败：%v", input.Model, err)
		}
	}
	if _, err := gw.CreateModel(ctx, appID, chatPrice("chat", "dup")); errCode(err) != 40221 {
		t.Fatalf("同名模型应返回 40221，得到 %v", err)
	}

	// 能力声明：建时写入、读回规整；改模型时不传能力沿用原值。
	thinker := chatPrice("chat-think", "up-ok")
	thinker.Capabilities = &aigateway.Capabilities{Vision: true, Tools: true, Reasoning: true,
		ReasoningEfforts: []string{"high", "None", "low"}, ContextLength: 200000}
	created, err := gw.CreateModel(ctx, appID, thinker)
	if err != nil {
		t.Fatalf("建思考模型失败：%v", err)
	}
	if got := created.Capabilities; !got.Vision || !got.Reasoning || got.ContextLength != 200000 ||
		strings.Join(got.ReasoningEfforts, ",") != "none,low,high" {
		t.Fatalf("能力声明没有按规整写入：%+v", got)
	}
	renamed := chatPrice("chat-think", "up-ok")
	renamed.DisplayName = "思考"
	updated, err := gw.UpdateModel(ctx, appID, created.ID, renamed)
	if err != nil {
		t.Fatalf("改思考模型失败：%v", err)
	}
	if !updated.Capabilities.Reasoning || len(updated.Capabilities.ReasoningEfforts) != 3 {
		t.Fatalf("改模型时不传能力应沿用原值：%+v", updated.Capabilities)
	}
	plain, err := pg.GetAIGatewayModelByName(ctx, appID, "chat")
	if err != nil || !plain.Capabilities.Tools || plain.Capabilities.Reasoning {
		t.Fatalf("没声明能力的模型应取默认（只开工具）：%+v %v", plain, err)
	}

	// prompt 1500 字节 → 估 500 + 4 = 504 输入 token；max_tokens 125 → 估算 1.508 点，暂扣 2 积分。
	prompt := strings.Repeat("a", 1500)
	// cents 是余额（分）；wallet 非 "0" 时以它为准（余额直付那一段用元写）。
	newUser := func(account string, cents int64, wallet string) (int64, *AIGatewayPrincipal, string) {
		t.Helper()
		if wallet == "0" {
			wallet = decimal.New(cents, -2).StringFixed(2)
		}
		userID := insertTestUser(t, ctx, pool, appID, account, wallet)
		created, err := gw.CreateKey(ctx, &authdomain.Session{AppID: appID, UserID: userID}, aigateway.KeyInput{Name: ptrTo("test")})
		if err != nil {
			t.Fatalf("建密钥失败：%v", err)
		}
		principal, err := gw.Authenticate(ctx, created.Secret, "127.0.0.1", "go-test")
		if err != nil {
			t.Fatalf("密钥鉴权失败：%v", err)
		}
		return userID, principal, created.Secret
	}
	chat := func(principal *AIGatewayPrincipal, model string, maxTokens int) (*AIGatewayChatResult, error) {
		return gw.RelayChat(ctx, principal, AIGatewayChatInput{
			Endpoint: aigateway.EndpointChat,
			Request: aidomain.ChatRequest{Model: model, MaxTokens: maxTokens,
				Messages: []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, prompt)}},
		}, nil)
	}
	mustChat := func(principal *AIGatewayPrincipal, model string) *AIGatewayChatResult {
		t.Helper()
		result, err := chat(principal, model, 125)
		if err != nil {
			t.Fatalf("调用 %s 失败：%v", model, err)
		}
		return result
	}
	// integralOf 余额（分）：沿用旧名，旧的积分断言原样成立。
	integralOf := func(userID int64) int64 {
		t.Helper()
		var v int64
		if err := pool.QueryRow(ctx, `SELECT (balance * 100)::bigint FROM user_wallets WHERE user_id = $1`, userID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	walletOf := func(userID int64) (string, string) {
		t.Helper()
		var balance, frozen string
		if err := pool.QueryRow(ctx, `SELECT balance::text, frozen::text FROM user_wallets WHERE user_id = $1`, userID).
			Scan(&balance, &frozen); err != nil {
			t.Fatal(err)
		}
		return balance, frozen
	}
	countRows := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	integralEntries := func(userID int64) int {
		return countRows(`SELECT COUNT(*) FROM wallet_transactions WHERE user_id = $1 AND type IN ('consume', 'ai_refund')`, userID)
	}
	// carryOf 余额零头在前（沿用旧的「积分零头」位置），第二个是已停用的积分零头。
	carryOf := func(userID int64) (int64, int64) {
		t.Helper()
		var integralCarry, walletCarry int64
		if err := pool.QueryRow(ctx, `SELECT integral_carry, wallet_carry FROM ai_gateway_accounts WHERE user_id = $1`, userID).
			Scan(&integralCarry, &walletCarry); err != nil {
			t.Fatal(err)
		}
		return walletCarry, integralCarry
	}

	// ── 1. 成功调用：只扣积分、一条流水、零头凑整 ──
	saveSettings(func(s *aigateway.Settings) { s.Sources = []string{aigateway.SourceWallet} })
	alice, alicePrincipal, _ := newUser("aig-alice", 10, "0")
	first := mustChat(alicePrincipal, "chat")
	if first.Response.Model != "chat" {
		t.Fatalf("响应里的模型名应换回对外名，得到 %s", first.Response.Model)
	}
	if upstream.lastModel() != "up-ok" {
		t.Fatalf("发给上游的应是上游模型名，得到 %s", upstream.lastModel())
	}
	if got := integralOf(alice); got != 9 {
		t.Fatalf("1.5 点应先扣 1 积分（零头 0.5 记账），余额应为 9，得到 %d", got)
	}
	if carry, _ := carryOf(alice); carry != 500_000 {
		t.Fatalf("零头应为 500000 微点，得到 %d", carry)
	}
	if first.Record.Status != aigateway.StatusSucceeded || first.Record.CostMicro != 1_500_000 ||
		first.Record.InputTokens != 500 || first.Record.OutputTokens != 125 || first.Record.UsageEstimated {
		t.Fatalf("调用记录不对：%+v", first.Record)
	}
	mustChat(alicePrincipal, "chat")
	if got := integralOf(alice); got != 7 {
		t.Fatalf("第二次 0.5 + 1.5 = 2 积分，余额应为 7，得到 %d", got)
	}
	if carry, _ := carryOf(alice); carry != 0 {
		t.Fatalf("零头应归零，得到 %d", carry)
	}
	if n := integralEntries(alice); n != 2 {
		t.Fatalf("两次成功调用应恰好两条积分流水（暂扣不记流水），得到 %d", n)
	}

	// ── 2. 上游失败：不扣费、不留流水、记录为 failed ──
	if _, err := chat(alicePrincipal, "chat-fail", 125); err == nil {
		t.Fatal("上游 500 应返回错误")
	}
	if got := integralOf(alice); got != 7 || integralEntries(alice) != 2 {
		t.Fatalf("失败调用不应扣费或写流水：余额 %d、流水 %d", integralOf(alice), integralEntries(alice))
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_requests WHERE user_id = $1 AND status = 'failed' AND charged = 0`, alice); n != 1 {
		t.Fatalf("应有一条 failed 记录，得到 %d", n)
	}

	// ── 3. 余额不足：直接拒绝，不留记录 ──
	broke, brokePrincipal, _ := newUser("aig-broke", 0, "0")
	if _, err := chat(brokePrincipal, "chat", 125); errCode(err) != 40201 {
		t.Fatalf("余额不足应返回 40201，得到 %v", err)
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_requests WHERE user_id = $1`, broke); n != 0 {
		t.Fatalf("被拒的调用不应留记录，得到 %d", n)
	}

	// ── 4. 余额不够整笔估算：压低输出上限 ──
	// 余额 2 点：输入 504 token 花 1.008 点，剩 0.992 点 / 每 token 0.004 点 = 248 个输出 token。
	clampUser, clampPrincipal, _ := newUser("aig-clamp", 2, "0")
	if _, err := chat(clampPrincipal, "chat", 4000); err != nil {
		t.Fatalf("压低输出后应能调用：%v", err)
	}
	if got := upstream.lastMaxTokens(); got != 248 {
		t.Fatalf("max_tokens 应被压到 248，上游收到 %d", got)
	}
	if got := integralOf(clampUser); got != 1 {
		t.Fatalf("结算 1.5 点后余额应为 1，得到 %d", got)
	}

	// ── 5. 每日免费额度先用，积分补差 ──
	saveSettings(func(s *aigateway.Settings) {
		s.Sources = []string{aigateway.SourceFreeDaily, aigateway.SourceWallet}
		s.FreeDaily = decimal.NewFromInt(1)
	})
	freeUser, freePrincipal, _ := newUser("aig-free", 10, "0")
	result := mustChat(freePrincipal, "chat")
	if len(result.Record.Charges) != 2 || result.Record.Charges[0].Source != aigateway.SourceFreeDaily ||
		result.Record.Charges[0].Micro != 1_000_000 || result.Record.Charges[1].Micro != 500_000 {
		t.Fatalf("应先用 1 点免费额度、再用 0.5 点积分：%+v", result.Record.Charges)
	}
	if got := integralOf(freeUser); got != 10 || integralEntries(freeUser) != 0 {
		t.Fatalf("0.5 点积分还在零头里，余额不变、不写流水：余额 %d 流水 %d", got, integralEntries(freeUser))
	}
	mustChat(freePrincipal, "chat")
	if got := integralOf(freeUser); got != 8 {
		t.Fatalf("免费额度用完后 0.5 + 1.5 = 2 积分，余额应为 8，得到 %d", got)
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_quota_usage WHERE user_id = $1 AND source = 'free_daily' AND used = 1000000 AND reserved = 0`, freeUser); n != 1 {
		t.Fatal("免费额度应记已用 1 点、暂扣归零")
	}

	// ── 6. 赠送额度：调整幂等、消费记流水 ──
	saveSettings(func(s *aigateway.Settings) {
		s.Sources = []string{aigateway.SourceCredit, aigateway.SourceWallet}
		s.FreeDaily = decimal.Zero
	})
	creditUser, creditPrincipal, _ := newUser("aig-credit", 0, "0")
	adjust := aigateway.CreditAdjustInput{AppID: appID, UserID: creditUser, Amount: decimal.NewFromInt(3), Reason: "测试", RequestID: "credit-req-0001"}
	if _, err := gw.AdjustCredit(ctx, adjust); err != nil {
		t.Fatal(err)
	}
	if replay, err := gw.AdjustCredit(ctx, adjust); err != nil || !replay.Replayed || !replay.Balance.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("同一请求编号应只生效一次：%+v %v", replay, err)
	}
	adjust.Amount = decimal.NewFromInt(5)
	if _, err := gw.AdjustCredit(ctx, adjust); errCode(err) != 40225 {
		t.Fatalf("编号挪用应返回 40225，得到 %v", err)
	}
	mustChat(creditPrincipal, "chat")
	overview, err := gw.UserOverview(ctx, appID, creditUser)
	if err != nil || !overview.Credit.Equal(decimal.RequireFromString("1.5")) || !overview.CreditFrozen.IsZero() {
		t.Fatalf("赠送额度应剩 1.5、冻结为 0：%+v %v", overview, err)
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_credit_ledger WHERE user_id = $1 AND kind = 'consume' AND amount = -1500000`, creditUser); n != 1 {
		t.Fatal("赠送额度消费应恰好一条流水")
	}

	// ── 7. 余额扣费：暂扣进冻结、结算一条流水、零头凑整 ──
	saveSettings(func(s *aigateway.Settings) { s.Sources = []string{aigateway.SourceWallet} })
	walletUser, walletPrincipal, _ := newUser("aig-wallet", 0, "1.00")
	mustChat(walletPrincipal, "chat")
	if balance, frozen := walletOf(walletUser); balance != "0.99" || frozen != "0.00" {
		t.Fatalf("1.5 分先扣 1 分：余额应为 0.99、冻结 0，得到 %s / %s", balance, frozen)
	}
	mustChat(walletPrincipal, "chat")
	if balance, _ := walletOf(walletUser); balance != "0.97" {
		t.Fatalf("第二次 0.5 + 1.5 = 2 分，余额应为 0.97，得到 %s", balance)
	}
	if n := countRows(`SELECT COUNT(*) FROM wallet_transactions WHERE user_id = $1 AND type = 'consume'`, walletUser); n != 2 {
		t.Fatalf("应恰好两条余额消费流水，得到 %d", n)
	}

	// ── 8. 实际用量远超暂扣：从实时余额补，补不上记 shortfall，绝不扣成负数 ──
	saveSettings(func(s *aigateway.Settings) { s.Sources = []string{aigateway.SourceWallet} })
	bigUser, bigPrincipal, _ := newUser("aig-big", 3, "0")
	big := mustChat(bigPrincipal, "chat-big")
	if big.Record.ShortfallMicro <= 0 || big.Record.ChargedMicro+big.Record.ShortfallMicro != big.Record.CostMicro {
		t.Fatalf("溢出应记 shortfall 且 charged + shortfall = cost：%+v", big.Record)
	}
	if got := integralOf(bigUser); got < 0 || got > 1 {
		t.Fatalf("溢出补扣后积分应在 0-1 之间且非负，得到 %d", got)
	}

	// ── 9. 上游没报用量：按内容估算并标记 ──
	estUser, estPrincipal, _ := newUser("aig-est", 10, "0")
	est := mustChat(estPrincipal, "chat-nousage")
	if !est.Record.UsageEstimated || est.Record.InputTokens != 504 {
		t.Fatalf("没报用量时应按估算计费并标记：%+v", est.Record)
	}
	_ = estUser

	// ── 10. 流式：客户端中途断开按已交付内容计费；上游中断不扣费 ──
	streamUser, streamPrincipal, _ := newUser("aig-stream", 10, "0")
	events := 0
	_, err = gw.RelayChat(ctx, streamPrincipal, AIGatewayChatInput{
		Endpoint: aigateway.EndpointChat, Stream: true,
		Request: aidomain.ChatRequest{Model: "chat", MaxTokens: 125,
			Messages: []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, prompt)}},
	}, func(event aidomain.StreamEvent) error {
		if event.Type == aidomain.StreamText {
			events++
			return fmt.Errorf("client gone")
		}
		return nil
	})
	if err == nil || events != 1 {
		t.Fatalf("客户端断开应返回错误：err=%v events=%d", err, events)
	}
	var aborted aigateway.Request
	if err := pool.QueryRow(ctx, `SELECT status, usage_estimated, output_tokens, charged FROM ai_gateway_requests
WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, streamUser).Scan(&aborted.Status, &aborted.UsageEstimated,
		&aborted.OutputTokens, &aborted.ChargedMicro); err != nil {
		t.Fatal(err)
	}
	// 交付了 "Hel"：1 个输出 token；504 × 2000 + 1 × 4000 = 1,012,000 微点
	if aborted.Status != aigateway.StatusAborted || !aborted.UsageEstimated || aborted.OutputTokens != 1 || aborted.ChargedMicro != 1_012_000 {
		t.Fatalf("中途断开应按已交付内容计费：%+v", aborted)
	}
	beforeFail := integralOf(streamUser)
	_, err = gw.RelayChat(ctx, streamPrincipal, AIGatewayChatInput{
		Endpoint: aigateway.EndpointChat, Stream: true,
		Request: aidomain.ChatRequest{Model: "chat-stream-fail", MaxTokens: 125,
			Messages: []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, prompt)}},
	}, func(aidomain.StreamEvent) error { return nil })
	if err == nil {
		t.Fatal("上游中途断开应返回错误")
	}
	if got := integralOf(streamUser); got != beforeFail {
		t.Fatalf("上游中途失败不应扣费：%d → %d", beforeFail, got)
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_requests WHERE user_id = $1 AND status = 'failed'`, streamUser); n != 1 {
		t.Fatalf("上游中断应记为 failed，得到 %d", n)
	}

	// ── 11. 图像：按实际返回的张数计费 ──
	imgUser, imgPrincipal, _ := newUser("aig-img", 20, "0")
	imgResult, err := gw.RelayPassthrough(ctx, imgPrincipal, AIGatewayPassthroughInput{
		Endpoint: aigateway.EndpointImageGenerate, Path: "images/generations", Model: "img",
		Build: func(upstreamModel string) ([]byte, string, error) {
			body, err := json.Marshal(map[string]any{"model": upstreamModel, "prompt": "cat", "n": 3})
			return body, "application/json", err
		},
		Estimate: aigateway.Meters{Images: 3},
		Meter: func(_ string, body []byte) (aigateway.Meters, bool, error) {
			return aigateway.MeterImages(body, "", "")
		},
	})
	if err != nil {
		t.Fatalf("图像调用失败：%v", err)
	}
	if imgResult.Record.Images != 2 || imgResult.Record.CostMicro != 10_000_000 || integralOf(imgUser) != 10 {
		t.Fatalf("要 3 张给了 2 张，应只收 10 点：%+v 余额 %d", imgResult.Record, integralOf(imgUser))
	}

	// ── 12. 密钥：消费上限与停用 ──
	keyUser := insertTestUser(t, ctx, pool, appID, "aig-key", "1.00")
	keySession := &authdomain.Session{AppID: appID, UserID: keyUser}
	limited, err := gw.CreateKey(ctx, keySession, aigateway.KeyInput{SpendLimit: ptrTo(decimal.NewFromInt(2))})
	if err != nil {
		t.Fatal(err)
	}
	keyPrincipal, err := gw.Authenticate(ctx, limited.Secret, "127.0.0.1", "go-test")
	if err != nil {
		t.Fatal(err)
	}
	mustChat(keyPrincipal, "chat")
	if _, err := chat(keyPrincipal, "chat", 125); errCode(err) != 40209 {
		t.Fatalf("密钥只剩 0.5 点，应返回 40209，得到 %v", err)
	}
	if _, err := gw.UpdateKey(ctx, keySession, limited.ID, aigateway.KeyInput{Status: aigateway.KeyDisabled}); err != nil {
		t.Fatal(err)
	}
	if key, _ := pg.GetAIGatewayKey(ctx, appID, keyUser, limited.ID); key.Name != "API 密钥" || key.SpendLimitMicro != 2_000_000 {
		t.Fatalf("只改状态不应动名称与上限：%+v", key)
	}
	if _, err := gw.Authenticate(ctx, limited.Secret, "127.0.0.1", "go-test"); errCode(err) != 40207 {
		t.Fatalf("停用的密钥应返回 40207，得到 %v", err)
	}
	if _, err := gw.Authenticate(ctx, aigateway.KeyPrefix+strings.Repeat("x", 43), "127.0.0.1", "go-test"); errCode(err) != 40206 {
		t.Fatalf("不存在的密钥应返回 40206，得到 %v", err)
	}

	// ── 13. 暂扣过期：后台回收放回；迟到的结算照样从实时余额扣 ──
	expUser, _, _ := newUser("aig-exp", 10, "0")
	model, err := pg.GetAIGatewayModelByName(ctx, appID, "chat")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := pg.ReserveAIGatewayRequest(ctx, aigateway.ReserveInput{AppID: appID, UserID: expUser,
		AuthKind: aigateway.AuthKey, Endpoint: aigateway.EndpointChat, Settings: settings, Model: *model,
		Estimate: aigateway.Meters{InputTokens: 504, OutputTokens: 125}, Now: time.Now().Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if got := integralOf(expUser); got != 8 {
		t.Fatalf("暂扣 2 积分后余额应为 8，得到 %d", got)
	}
	if released, err := gw.SweepExpiredHolds(ctx); err != nil || released != 1 {
		t.Fatalf("应回收 1 条过期暂扣：%d %v", released, err)
	}
	if got := integralOf(expUser); got != 10 {
		t.Fatalf("回收后积分应放回 10，得到 %d", got)
	}
	late, err := pg.SettleAIGatewayRequest(ctx, aigateway.SettleInput{RequestNo: stale.RequestNo,
		Meters: aigateway.Meters{InputTokens: 500, OutputTokens: 125}})
	if err != nil || late.Status != aigateway.StatusSucceeded || integralOf(expUser) != 9 {
		t.Fatalf("迟到的结算应从实时余额扣 1.5 点：%+v 余额 %d %v", late, integralOf(expUser), err)
	}
	if again, _ := pg.SettleAIGatewayRequest(ctx, aigateway.SettleInput{RequestNo: stale.RequestNo,
		Meters: aigateway.Meters{InputTokens: 500, OutputTokens: 125}}); again.ChargedMicro != late.ChargedMicro || integralOf(expUser) != 9 {
		t.Fatal("重复结算必须返回第一次的结论、不再扣费")
	}

	// ── 14. 退款：整笔退回，账本恒等式成立；不能退两次 ──
	refunded, err := gw.RefundRequest(ctx, aigateway.RefundInput{AppID: appID, RequestNo: first.RequestNo, Reason: "测试退款", Operator: "tester"})
	if err != nil || refunded.Status != aigateway.StatusRefunded {
		t.Fatalf("退款失败：%+v %v", refunded, err)
	}
	// alice：两次 1.5 共扣 3 积分、零头 0；退掉第一笔 1.5 → 退 2 积分、零头 0.5（欠 0.5）。
	if got := integralOf(alice); got != 9 {
		t.Fatalf("退款后积分应为 9，得到 %d", got)
	}
	if carry, _ := carryOf(alice); carry != 500_000 {
		t.Fatalf("退款后零头应为 500000，得到 %d", carry)
	}
	if _, err := gw.RefundRequest(ctx, aigateway.RefundInput{AppID: appID, RequestNo: first.RequestNo}); errCode(err) != 40218 {
		t.Fatalf("重复退款应返回 40218，得到 %v", err)
	}

	// ── 15. 并发：同一用户 30 个请求抢 5 积分，既不透支也不漏扣 ──
	raceUser, racePrincipal, _ := newUser("aig-race", 5, "0")
	upstream.delay.Store(int64(20 * time.Millisecond))
	var wg sync.WaitGroup
	var succeeded atomic.Int64
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := chat(racePrincipal, "chat", 125); err == nil {
				succeeded.Add(1)
			} else if code := errCode(err); code != 40201 {
				t.Errorf("并发调用只应因余额不足失败，得到 %v", err)
			}
		}()
	}
	wg.Wait()
	upstream.delay.Store(0)
	paid := 5 - integralOf(raceUser)
	carry, _ := carryOf(raceUser)
	if integralOf(raceUser) < 0 || paid*aigateway.MicroPerPoint+carry != succeeded.Load()*1_500_000 {
		t.Fatalf("并发后账不平：成功 %d 次，扣了 %d 积分，零头 %d", succeeded.Load(), paid, carry)
	}
	if succeeded.Load() < 2 {
		t.Fatalf("5 积分至少够成功 2 次，实际 %d", succeeded.Load())
	}

	// ── 全局不变式 ──
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_requests WHERE status = 'reserved'`); n != 0 {
		t.Fatalf("不应残留暂扣，得到 %d 条", n)
	}
	if n := countRows(`SELECT COUNT(*) FROM user_wallets WHERE appid = $1 AND frozen <> 0`, appID); n != 0 {
		t.Fatal("钱包冻结应全部归零")
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_accounts WHERE credit_frozen <> 0`); n != 0 {
		t.Fatal("赠送额度冻结应全部归零")
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_quota_usage WHERE reserved <> 0`); n != 0 {
		t.Fatal("周期额度的暂扣应全部归零")
	}
	if n := countRows(`SELECT COUNT(*) FROM ai_gateway_keys WHERE reserved <> 0`); n != 0 {
		t.Fatal("密钥的暂扣应全部归零")
	}
	if n := countRows(`SELECT COUNT(*) FROM users WHERE appid = $1 AND integral < 0`, appID); n != 0 {
		t.Fatal("积分不能为负")
	}
	if n := countRows(`SELECT COUNT(*) FROM integral_transactions WHERE appid = $1 AND title LIKE '%预扣%'`, appID); n != 0 {
		t.Fatal("不应出现任何暂扣流水")
	}

	stats, err := gw.Stats(ctx, appID, 7, "Asia/Shanghai")
	if err != nil || stats.Requests == 0 || stats.Failed == 0 || len(stats.BySource) == 0 || len(stats.Daily) != 7 {
		t.Fatalf("统计不对：%+v %v", stats, err)
	}
}

// fakeAIUpstream 按模型名决定行为的 OpenAI 兼容假上游。
type fakeAIUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	model  string
	maxTok int
	delay  atomic.Int64
}

func (f *fakeAIUpstream) lastModel() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.model
}

func (f *fakeAIUpstream) lastMaxTokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxTok
}

func newFakeAIUpstream(t *testing.T) *fakeAIUpstream {
	t.Helper()
	fake := &fakeAIUpstream{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model               string `json:"model"`
			Stream              bool   `json:"stream"`
			MaxTokens           int    `json:"max_tokens"`
			MaxCompletionTokens int    `json:"max_completion_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		fake.mu.Lock()
		fake.model = req.Model
		fake.maxTok = max(req.MaxTokens, req.MaxCompletionTokens)
		fake.mu.Unlock()
		if d := time.Duration(fake.delay.Load()); d > 0 {
			time.Sleep(d)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/generations"):
			_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://img/1.png"},{"url":"https://img/2.png"}]}`))
			return
		case !strings.HasSuffix(r.URL.Path, "/chat/completions"):
			http.NotFound(w, r)
			return
		}
		if req.Model == "up-fail" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		completion := 125
		if req.Model == "up-big" {
			completion = 100_000
		}
		usage := fmt.Sprintf(`,"usage":{"prompt_tokens":500,"completion_tokens":%d,"total_tokens":%d}`, completion, 500+completion)
		if req.Model == "up-nousage" {
			usage = ""
		}
		if !req.Stream {
			_, _ = fmt.Fprintf(w, `{"id":"c1","object":"chat.completion","model":%q,"choices":[{"index":0,`+
				`"message":{"role":"assistant","content":"Hello world"},"finish_reason":"stop"}]%s}`, req.Model, usage)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		chunk := func(delta string) {
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":%q,"+
				"\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", req.Model, delta)
			flusher.Flush()
		}
		chunk("Hel")
		if req.Model == "up-streamfail" {
			panic(http.ErrAbortHandler)
		}
		chunk("lo")
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[]%s}\n\n", req.Model, usage)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(fake.server.Close)
	return fake
}
