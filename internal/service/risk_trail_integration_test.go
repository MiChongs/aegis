package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"

	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
)

// 风控留痕经 JetStream 写库的集成测试（真实 Postgres + 内嵌的 nats-server JetStream，文件存储）：
//
//   - 1000 条并发留痕全部落库，规则命中、IP 计数、设备出现次数各恰好加 1000；
//   - 写库协程停掉（模拟实例下线）期间发布的留痕留在流里，换一个实例启动后接着写完，一条不少；
//   - 同一条留痕重复发布（同一事件 ID）、重复写库（模拟提交后确认前崩溃的重投），记录与计数都只算一次；
//   - NATS 挂掉时发布拿不到确认，退回直接写库，一条不少。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestRiskTrailIntegration
func TestRiskTrailIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	var ruleID int64
	if err := pool.QueryRow(ctx, `INSERT INTO risk_rules (name, scene, condition_type, score)
VALUES ('trail-rule', 'lottery', 'expr', 10) RETURNING id`).Scan(&ruleID); err != nil {
		t.Fatalf("建规则失败：%v", err)
	}
	server, conn := startTestJetStream(t)
	app := int64(40600)

	newTrail := func() *riskTrail {
		trail := newRiskTrail(pg, zap.NewNop(), nil)
		if err := trail.attach(conn); err != nil {
			t.Fatal(err)
		}
		return trail
	}
	startTrail := func(trail *riskTrail) context.CancelFunc {
		runCtx, cancel := context.WithCancel(ctx)
		trail.start(runCtx)
		deadline := time.Now().Add(10 * time.Second)
		for !trail.ready.Load() {
			if time.Now().After(deadline) {
				t.Fatal("留痕流没有就绪")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return cancel
	}
	event := func(i int) riskTrailEvent {
		action := securitydomain.ActionPass
		if i%10 == 0 {
			action = securitydomain.ActionBlock
		}
		ev := riskTrailEventOf(securitydomain.RiskAssessment{
			Scene: securitydomain.SceneLottery, AppID: &app, IP: fmt.Sprintf("198.51.100.%d", i%4),
			DeviceID: fmt.Sprintf("trail-dev-%d", i%3), Action: action, RiskLevel: securitydomain.LevelLow,
		}, &RiskEvalEnv{GeoCountry: "中国"}, riskDecision{matched: []securitydomain.MatchedRule{{RuleID: ruleID}}})
		return ev
	}
	counts := func() (rows, requests, blocks, seen, hits int64) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT
  (SELECT COUNT(*) FROM risk_assessments WHERE app_id = $1),
  (SELECT COALESCE(SUM(total_requests), 0) FROM ip_risk_records WHERE ip LIKE '198.51.100.%'),
  (SELECT COALESCE(SUM(total_blocks), 0) FROM ip_risk_records WHERE ip LIKE '198.51.100.%'),
  (SELECT COALESCE(SUM(seen_count), 0) FROM device_fingerprints WHERE device_id LIKE 'trail-dev-%'),
  (SELECT hit_count FROM risk_rules WHERE id = $2)`, app, ruleID).Scan(&rows, &requests, &blocks, &seen, &hits); err != nil {
			t.Fatal(err)
		}
		return
	}
	expect := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			rows, requests, blocks, seen, hits := counts()
			if rows == want && requests == want && seen == want && hits == want && blocks == (want+9)/10 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("应有 %d 条留痕（拦截 %d），得到记录 %d、IP 请求 %d / 拦截 %d、设备 %d、规则命中 %d",
					want, (want+9)/10, rows, requests, blocks, seen, hits)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// ── 1000 条并发留痕 ──
	first := newTrail()
	stopFirst := startTrail(first)
	var wg sync.WaitGroup
	started := time.Now()
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			first.record(ctx, event(i))
		}()
	}
	wg.Wait()
	published := time.Since(started)
	expect(1000)
	t.Logf("1000 条并发发布用时 %v，全部落库用时 %v", published, time.Since(started))

	// ── 写库协程停掉：之后发布的留在流里；换一个实例接着写 ──
	first.stop(ctx)
	stopFirst()
	for i := 1000; i < 1200; i++ {
		first.record(ctx, event(i))
	}
	time.Sleep(300 * time.Millisecond)
	if rows, _, _, _, _ := counts(); rows != 1000 {
		t.Fatalf("写库协程停掉时不应再写库，得到 %d 条", rows)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(ctx, riskTrailStream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil || info.State.Msgs != 200 {
		t.Fatalf("流里应留着 200 条，得到 %+v %v", info.State, err)
	}
	second := newTrail()
	stopSecond := startTrail(second)
	defer stopSecond()
	expect(1200)
	if info, err := stream.Info(ctx); err != nil || info.State.Msgs != 0 {
		t.Fatalf("写完确认之后流应清空（工作队列），得到 %+v %v", info.State, err)
	}

	// ── 同一条重复发布、重复写库：只算一次 ──
	dup := event(1200)
	second.record(ctx, dup)
	second.record(ctx, dup)
	expect(1201)
	fresh, err := pg.WriteRiskTrail(ctx, []pgrepo.RiskTrailRecord{dup.RiskTrailRecord, dup.RiskTrailRecord})
	if err != nil || len(fresh) != 0 {
		t.Fatalf("重投的留痕不应再写，得到 %v %v", fresh, err)
	}
	expect(1201)

	// ── NATS 挂掉：退回直接写库 ──
	server.Shutdown()
	server.WaitForShutdown()
	for i := 1201; i < 1205; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			second.record(ctx, event(i))
		}()
	}
	wg.Wait()
	expect(1205)
}

// startTestJetStream 起一个内嵌的 nats-server（JetStream、文件存储在临时目录）。
func startTestJetStream(t *testing.T) (*natsserver.Server, *nats.Conn) {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats-server 没有就绪")
	}
	t.Cleanup(server.Shutdown)
	conn, err := nats.Connect(server.ClientURL(), nats.MaxReconnects(0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	return server, conn
}
