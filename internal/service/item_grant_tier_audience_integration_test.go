package service

import (
	"slices"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/itemgrant"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 发放规则按会员档位筛人：同为永久会员的两档（领航者与领航者青春版）要分得开，
// 批量执行的 SQL 条件与单人判定（MatchAudience）必须同一口径。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55433/postgres go test ./internal/service -run TestItemGrantTierAudience
func TestItemGrantTierAudience(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	const appID = int64(31800)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'tier-audience') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	tier := func(name string) int64 {
		t.Helper()
		features := []string{}
		saved, err := vip.AdminSaveTier(ctx, vipdomain.TierMutation{AppID: appID, Name: &name, Features: &features})
		if err != nil {
			t.Fatal(err)
		}
		return saved.ID
	}
	navigator, youth := tier("领航者"), tier("领航者青春版")
	grant := func(account string, in AdminVipGrantInput) int64 {
		t.Helper()
		id := insertTestUser(t, ctx, pool, appID, account, "0")
		in.UserID, in.AppID, in.Operator = id, appID, "tester"
		if _, err := vip.AdminGrantVip(ctx, in); err != nil {
			t.Fatal(err)
		}
		return id
	}
	navUser := grant("tier-nav", AdminVipGrantInput{Lifetime: true, TierID: navigator})
	youthUser := grant("tier-youth", AdminVipGrantInput{Lifetime: true, TierID: youth})
	timedYouth := grant("tier-timed", AdminVipGrantInput{Days: 7, TierID: youth})
	insertTestUser(t, ctx, pool, appID, "tier-none", "0")

	count := func(audience itemgrant.AudienceConfig) int64 {
		t.Helper()
		n, err := pg.CountItemGrantCandidates(ctx, appID, &audience)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	cases := []struct {
		name     string
		audience itemgrant.AudienceConfig
		want     []int64
	}{
		{"永久会员", itemgrant.AudienceConfig{Scope: itemgrant.ScopeLifetime}, []int64{navUser, youthUser}},
		{"只要领航者", itemgrant.AudienceConfig{Scope: itemgrant.ScopeLifetime, TierIDs: []int64{navigator}, TierLifetimeOnly: true}, []int64{navUser}},
		{"青春版（含限时）", itemgrant.AudienceConfig{Scope: itemgrant.ScopeAll, TierIDs: []int64{youth}}, []int64{youthUser, timedYouth}},
		{"永久青春版", itemgrant.AudienceConfig{Scope: itemgrant.ScopeAll, TierIDs: []int64{youth}, TierLifetimeOnly: true}, []int64{youthUser}},
	}
	all := []int64{navUser, youthUser, timedYouth}
	for _, c := range cases {
		if got := count(c.audience); got != int64(len(c.want)) {
			t.Fatalf("%s：SQL 选出 %d 人，期望 %d", c.name, got, len(c.want))
		}
		for _, id := range all {
			basics, err := pg.ItemGrantBasics(ctx, appID, id)
			if err != nil {
				t.Fatal(err)
			}
			ok, _ := itemgrant.MatchAudience(c.audience, basics, time.Now())
			if ok != slices.Contains(c.want, id) {
				t.Fatalf("%s：用户 %d 单人判定 %v 与 SQL 不一致（tiers=%v lifetime=%v）", c.name, id, ok, basics.TierIDs, basics.LifetimeTierIDs)
			}
		}
	}
}
