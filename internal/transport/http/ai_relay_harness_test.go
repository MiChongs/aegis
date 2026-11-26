package httptransport

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// TestAIRelayHarness 给客户端 SDK 做端到端测试用：起一个真实的中转（假上游），把地址与密钥写进
// AEGIS_RELAY_HARNESS 目录（addr、key），直到目录里出现 stop 文件或 3 分钟后退出。平时跳过。
//
//	AEGIS_TEST_PG_DSN=…/aegis_aig_test AEGIS_RELAY_HARNESS=/tmp/h go test ./internal/transport/http -run TestAIRelayHarness
func TestAIRelayHarness(t *testing.T) {
	dir, dsn := os.Getenv("AEGIS_RELAY_HARNESS"), os.Getenv("AEGIS_TEST_PG_DSN")
	if dir == "" || dsn == "" {
		t.Skip("未设置 AEGIS_RELAY_HARNESS / AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetRelayTestDatabase(t, ctx, pool)
	pg := pgrepo.New(pool)
	ai := service.NewAIProviderService(zap.NewNop(), pg, "relay-test-master-key")
	gw := service.NewAIGatewayService(zap.NewNop(), pg, nil, ai)
	upstream := newRelayFakeUpstream(t)

	const appID = int64(32001)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'harness')`, appID); err != nil {
		t.Fatal(err)
	}
	name, provider, enabled := "fake", aidomain.ProviderCustomOpenAI, true
	if _, err := ai.Save(ctx, aidomain.ConfigMutation{AppID: appID, Name: &name, Provider: &provider, Enabled: &enabled,
		Settings: map[string]string{aidomain.KeyBaseURL: upstream.server.URL + "/v1"},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "upstream-secret"}}); err != nil {
		t.Fatal(err)
	}
	settings := aigateway.DefaultSettings()
	settings.Enabled, settings.Sources, settings.RPM, settings.MaxConcurrent = true, []string{aigateway.SourceWallet}, 0, 20
	settings.WalletRate = decimal.RequireFromString("0.01")
	if _, err := gw.SaveSettings(ctx, appID, settings, "test"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []aigateway.ModelInput{
		{Model: "chat", UpstreamModel: "up-chat", Kind: aigateway.KindChat, InputPrice: decimal.NewFromInt(1000), OutputPrice: decimal.NewFromInt(1000)},
		{Model: "edit", UpstreamModel: "up-edit", Kind: aigateway.KindImage, ImagePrice: decimal.NewFromInt(1)},
		{Model: "draw", UpstreamModel: "up-draw", Kind: aigateway.KindImage, ImagePrice: decimal.NewFromInt(2)},
		{Model: "tts", UpstreamModel: "up-tts", Kind: aigateway.KindSpeech, CharPrice: decimal.NewFromInt(1000)},
		{Model: "whisper", UpstreamModel: "up-whisper", Kind: aigateway.KindTranscription, MinutePrice: decimal.NewFromInt(2)},
	} {
		if _, err := gw.CreateModel(ctx, appID, input); err != nil {
			t.Fatal(err)
		}
	}
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, enabled) VALUES ($1, 'sdk-user', TRUE) RETURNING id`,
		appID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_wallets (user_id, appid, balance) VALUES ($1, $2, 1000.00)`, userID, appID); err != nil {
		t.Fatal(err)
	}
	created, err := gw.CreateKey(ctx, &authdomain.Session{AppID: appID, UserID: userID}, aigateway.KeyInput{})
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handler{aiGateway: gw}
	relay := router.Group("/api/ai/v1")
	relay.GET("/models", h.AIRelayModels)
	relay.POST("/chat/completions", h.AIRelayChatCompletions)
	relay.POST("/messages", h.AIRelayMessages)
	relay.POST("/images/generations", h.AIRelayImageGenerations)
	relay.POST("/images/edits", h.AIRelayImageEdits)
	relay.POST("/audio/speech", h.AIRelaySpeech)
	relay.POST("/audio/transcriptions", h.AIRelayTranscriptions)
	relay.GET("/jobs/:jobId", h.AIRelayJob)
	relay.GET("/jobs/:jobId/content", h.AIRelayJobContent)
	relay.DELETE("/jobs/:jobId", h.AIRelayCancelJob)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	_ = os.WriteFile(filepath.Join(dir, "key"), []byte(created.Secret), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "addr"), []byte("http://"+listener.Addr().String()), 0o600)
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
