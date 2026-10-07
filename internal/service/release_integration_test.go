package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 发布中心的端到端集成测试：真实 Postgres，跑全部迁移（两遍，验证可重复执行），
// 再用真实仓储走一遍「建版本 → 发布 → 检测 → 上报 → 灰度 → 暂停 / 撤回 → 删除」。
//
// 需要一个空库：AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestReleaseCenterIntegration
// 没有设置时跳过。
func TestReleaseCenterIntegration(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	resetTestDatabase(t, ctx, pool)

	pg := pgrepo.New(pool)
	const appID = int64(10000)

	// 旧接口建的版本：迁移把它的下载地址迁成通用包，新引擎能读到
	legacyVersion, legacyCode, legacyURL := "0.9.0", int64(900), "https://cdn.example.com/legacy.apk"
	legacy, err := pg.UpsertAppVersion(ctx, appdomain.AppVersionMutation{
		AppID: appID, Version: &legacyVersion, VersionCode: &legacyCode, DownloadURL: &legacyURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, mustRead(t, "../../migrations/postgres/000088_release_center.up.sql")); err != nil {
		t.Fatal(err)
	}
	var legacyAssets int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_version_assets WHERE version_id = $1`, legacy.ID).Scan(&legacyAssets)
	if legacyAssets != 1 {
		t.Fatalf("legacy download_url should be backfilled once, got %d assets", legacyAssets)
	}

	svc := NewReleaseService(nil, pg, nil, time.UTC)
	base := "https://aegis.example.com"
	ptr := func(s string) *string { return &s }
	code := int64(1000)
	rollout := 100

	created, err := svc.Save(ctx, appdomain.ReleaseMutation{
		AppID: appID, Version: ptr("1.0.0"), VersionCode: &code, Title: ptr("首个正式版"),
		Notes:      ptr(`<h2>新功能</h2><p>支持<strong>富文本</strong><script>alert(1)</script></p>`),
		UpdateType: ptr("recommended"), RolloutPct: &rollout,
		Assets: []appdomain.ReleaseAssetInput{
			{Abi: "arm64-v8a", URL: "https://cdn.example.com/a64.apk", FileSize: 100, SHA256: strings.Repeat("a", 64)},
			{Abi: "universal", URL: "https://cdn.example.com/uni.apk", FileSize: 200},
		},
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != appdomain.ReleaseStatusDraft || strings.Contains(created.Notes, "script") ||
		created.Summary == "" || strings.ContainsAny(created.Summary, "*-") {
		t.Fatalf("unexpected draft: status=%s notes=%q summary=%q", created.Status, created.Notes, created.Summary)
	}
	if len(created.Assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(created.Assets))
	}
	if _, err := svc.Save(ctx, appdomain.ReleaseMutation{AppID: appID, Version: ptr("dup"), VersionCode: &code}, base); err == nil {
		t.Fatal("duplicate version code should be rejected")
	}

	client := appdomain.ReleaseClient{VersionCode: 950, Platform: "android", Abis: []string{"arm64-v8a"}, DeviceID: "dev-1"}
	if result, _ := svc.Check(ctx, appID, client, base); result.HasUpdate && result.Release.ID == created.ID {
		t.Fatal("draft must not be offered")
	}
	if _, err := svc.Publish(ctx, appID, created.ID, nil, base); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Check(ctx, appID, client, base)
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasUpdate || result.Release.ID != created.ID || result.Asset == nil || result.Asset.Abi != "arm64-v8a" {
		t.Fatalf("unexpected check result: %+v", result)
	}
	if result.Asset.URL != "" || result.Asset.DownloadURL != "https://cdn.example.com/a64.apk" {
		t.Fatalf("public asset should expose downloadUrl only: %+v", result.Asset)
	}
	if result.UpdateType != appdomain.ReleaseUpdateRecommended {
		t.Fatalf("expected recommended, got %s", result.UpdateType)
	}

	latest, err := svc.Latest(ctx, appID, appdomain.ReleaseClient{Platform: "android"}, base)
	if err != nil || latest == nil || latest.ID != created.ID {
		t.Fatalf("anonymous latest should be the public full release: %+v %v", latest, err)
	}
	history, err := svc.History(ctx, appID, appdomain.ReleaseClient{Platform: "android"}, 1, 10, base)
	if err != nil || history["total"].(int) != 2 {
		t.Fatalf("history should list the new and the legacy release: %+v %v", history, err)
	}

	if err := svc.RecordEvent(ctx, appID, created.ID, appdomain.ReleaseEventDownloaded, result.Asset.ID, client); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordEvent(ctx, appID, created.ID, appdomain.ReleaseEventInstalled, 0, client); err != nil {
		t.Fatal(err)
	}
	stats, err := svc.Stats(ctx, appID, created.ID)
	if err != nil || stats.Total.Offered != 1 || stats.Total.Downloaded != 1 || stats.Total.Installed != 1 || len(stats.Daily) != 1 {
		t.Fatalf("unexpected stats: %+v %v", stats, err)
	}

	if _, err := svc.SetRollout(ctx, appID, created.ID, 0, base); err != nil {
		t.Fatal(err)
	}
	if result, _ := svc.Check(ctx, appID, client, base); result.HasUpdate && result.Release.ID == created.ID {
		t.Fatal("0% rollout must not offer the release")
	}
	_, _ = svc.SetRollout(ctx, appID, created.ID, 100, base)

	// 定时发布：未到点不下发
	code2 := int64(1100)
	scheduled, err := svc.Save(ctx, appdomain.ReleaseMutation{AppID: appID, Version: ptr("1.1.0"), VersionCode: &code2,
		Assets: []appdomain.ReleaseAssetInput{{Abi: "universal", URL: "https://cdn.example.com/110.apk"}}}, base)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(time.Hour)
	if published, err := svc.Publish(ctx, appID, scheduled.ID, &at, base); err != nil || published.Status != appdomain.ReleaseStatusScheduled {
		t.Fatalf("expected scheduled: %+v %v", published, err)
	}
	if result, _ := svc.Check(ctx, appID, client, base); result.Release == nil || result.Release.ID != created.ID {
		t.Fatalf("scheduled release must not be offered before publishAt: %+v", result.Release)
	}

	if err := svc.Delete(ctx, appID, created.ID); err == nil {
		t.Fatal("live release must not be deletable")
	}
	for _, action := range []string{"pause", "resume", "revoke"} {
		if _, err := svc.Transition(ctx, appID, created.ID, action, base); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if err := svc.Delete(ctx, appID, created.ID); err != nil {
		t.Fatal(err)
	}

	overview, err := svc.Overview(ctx, appID, base)
	if err != nil || overview.Scheduled != 1 {
		t.Fatalf("unexpected overview: %+v %v", overview, err)
	}

	// 自助加入渠道
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account) VALUES ($1, 'release-tester') RETURNING id`, appID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	selfJoin := true
	name, codeStr := "Beta 体验计划", "beta"
	if _, err := pg.UpsertVersionChannel(ctx, appdomain.AppVersionChannelMutation{AppID: appID, Name: &name, Code: &codeStr, SelfJoin: &selfJoin}); err != nil {
		t.Fatal(err)
	}
	channels, err := svc.SetChannelMembership(ctx, appID, userID, "beta", true)
	if err != nil {
		t.Fatal(err)
	}
	joined := false
	for _, ch := range channels {
		if ch.Code == "beta" && ch.Joined {
			joined = true
		}
	}
	if !joined {
		t.Fatalf("user should have joined beta: %+v", channels)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
