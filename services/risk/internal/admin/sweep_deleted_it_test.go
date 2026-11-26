package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/backfill"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/redisx"
	"aegis/services/risk/internal/store"

	"github.com/jackc/pgx/v5"
)

// 主服务删号没通知到风控（普通删除、账号注销、风控当时不可用）时，回灌不能把账号写回来，清扫要把残留收尾。
// 用本地 risk 的 Postgres 另建一个库当主库。
// RISK_IT=1 go test ./services/risk/internal/admin -run SweepDeletedIT
func TestSweepDeletedIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	ctx := context.Background()
	cfg := config.Load()
	st, err := store.Open(ctx, cfg.PGDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rdb := redisx.New(cfg)
	defer rdb.Close()
	if err := graph.Migrate(cfg, t.Logf); err != nil {
		t.Fatal(err)
	}
	g, err := graph.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	// 假主库：apps、users（自增 ID）、user_profiles、login_geo_events
	mainDSN := mainTestDB(t, ctx, cfg.PGDSN)
	main, err := pgx.Connect(ctx, mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer main.Close(ctx)
	now := time.Now()
	app := int64(980000 + now.UnixNano()%9000)
	hexID := func() string {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		return hex.EncodeToString(b)
	}
	devShared, devLone := hexID(), hexID()
	n := now.UnixNano()
	ipShared := fmt.Sprintf("61.135.%d.%d", n%250+1, (n/250)%250+1)
	ipLone := fmt.Sprintf("61.136.%d.%d", n%250+1, (n/250)%250+1)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS login_geo_events, user_profiles, users, apps`,
		`CREATE TABLE apps (id bigint PRIMARY KEY)`,
		`CREATE TABLE users (id bigserial PRIMARY KEY, appid bigint NOT NULL, account text, enabled bool NOT NULL DEFAULT true,
			disabled_end_time timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz)`,
		`CREATE TABLE user_profiles (user_id bigint, nickname text, email text, phone text, register_ip text, register_province text,
			register_city text, register_isp text, register_time timestamptz)`,
		`CREATE TABLE login_geo_events (app_id bigint, user_id bigint, ip inet, device_id text, country text, region text, city text,
			isp text, asn text, created_at timestamptz NOT NULL DEFAULT now())`,
		fmt.Sprintf(`INSERT INTO apps VALUES (%d)`, app),
		// 1、2、3、4 号账号；2 号与 4 号随后删除（4 号是最后一个，只能靠序列发现）
		fmt.Sprintf(`INSERT INTO users (appid, account) VALUES (%d, 'a1'), (%d, 'a2'), (%d, 'a3'), (%d, 'a4')`, app, app, app, app),
		// 删号以后登录地理表里还留着 2 号与 4 号的行（主库线上就是这样）
		fmt.Sprintf(`INSERT INTO login_geo_events (app_id, user_id, ip, device_id) VALUES
			(%[1]d, 1, '%[2]s', '%[3]s'), (%[1]d, 2, '%[2]s', '%[3]s'), (%[1]d, 4, '%[4]s', '%[5]s')`, app, ipShared, devShared, ipLone, devLone),
		`DELETE FROM users WHERE id IN (2, 4)`,
	} {
		if _, err := main.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	// 关系图里是删号之前的样子：1、2 号共用设备与 IP，4 号自己一台设备、一个手机号
	ts := now.UnixMilli()
	if err := g.DeleteVertices([]string{graph.IPVID(ipShared), graph.IPVID(ipLone)}); err != nil {
		t.Fatal(err)
	}
	if err := g.Apply([]events.Event{
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 1, Account: "a1", IP: ipShared, DeviceID: devShared},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 2, Account: "a2", IP: ipShared, DeviceID: devShared},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 3, Account: "a3", IP: ipShared},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 4, Account: "a4", IP: ipLone, DeviceID: devLone, Phone: "1380000" + fmt.Sprint(n%10000)},
	}); err != nil {
		t.Fatal(err)
	}
	// 2 号只删了点、边还悬着（旧数据里出现过），清扫也要认出来
	if _, err := g.Exec(fmt.Sprintf("DELETE VERTEX %s;", graph.Quote(graph.UserVID(app, 2)))); err != nil {
		t.Fatal(err)
	}
	related := func() []int64 {
		rel, err := g.RelatedUsers(app, []int64{1}, graph.RelatedOptions{PerEntity: 50, Max: 50})
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, r := range rel {
			out = append(out, r.UserID)
		}
		slices.Sort(out)
		return out
	}
	if got := related(); !slices.Contains(got, 2) {
		t.Fatalf("准备数据：1 号应当关联到 2 号，实际 %v", got)
	}

	// 回灌：登录地理表里 2 号、4 号的行不能写回图
	r := &backfill.Runner{MainDSN: mainDSN, Graph: g, Logf: t.Logf}
	p, _ := r.Run(ctx) // 假主库缺很多表，来源会跳过；这里只看写入
	if p.SkippedDeleted == 0 {
		t.Fatalf("回灌应当挡掉已删除账号的写入，progress=%+v", p)
	}
	if props, err := g.Props([]string{graph.UserVID(app, 2), graph.UserVID(app, 4)}); err != nil || len(props) > 1 {
		// 4 号的点删号前就在（还没清扫），2 号的点不能被回灌重新建出来
		if _, back := props[graph.UserVID(app, 2)]; back || err != nil {
			t.Fatalf("回灌把已删除的 2 号写回来了：%v %v", props, err)
		}
	}

	s := &Server{Cfg: cfg, Store: st, Lists: lists.NewIndex(rdb), Redis: rdb, Logf: t.Logf}
	s.SetGraph(g)
	s.Cfg.PGAdminDSN = mainDSN
	swept, err := s.SweepDeletedUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 2 {
		t.Fatalf("应当收尾 2 个已删除账号，实际 %d", swept)
	}
	if left, err := g.PresentUsers(app, []int64{2, 4}); err != nil || len(left) > 0 {
		t.Fatalf("清扫之后图里还留着 %v（%v）", left, err)
	}
	if got := related(); slices.Contains(got, 2) || !slices.Contains(got, 3) {
		t.Fatalf("清扫之后 1 号的关联应当只剩现有账号（含 3 号），实际 %v", got)
	}
	props, err := g.Props([]string{graph.DeviceVID(devLone), graph.DeviceVID(devShared), graph.UserVID(app, 1), graph.UserVID(app, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := props[graph.DeviceVID(devLone)]; ok {
		t.Fatal("只被 4 号用过的设备应当一起删掉")
	}
	for _, vid := range []string{graph.DeviceVID(devShared), graph.UserVID(app, 1), graph.UserVID(app, 3)} {
		if _, ok := props[vid]; !ok {
			t.Fatalf("%s 不该被删", vid)
		}
	}
	// 再跑一次：没有残留，什么也不做
	if again, err := s.SweepDeletedUsers(ctx); err != nil || again != 0 {
		t.Fatalf("第二次清扫应当为 0，实际 %d（%v）", again, err)
	}
	_ = g.DeleteVertices([]string{graph.UserVID(app, 1), graph.UserVID(app, 3), graph.IPVID(ipShared), graph.IPVID(ipLone), graph.DeviceVID(devShared),
		graph.AccountVID(app, "a1"), graph.AccountVID(app, "a3")})
}

// mainTestDB 在本地 risk 的 Postgres 上建（或复用）一个当主库用的库，返回它的连接串
func mainTestDB(t *testing.T, ctx context.Context, riskDSN string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, riskDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE DATABASE risk_sweep_it`); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
	u, err := url.Parse(riskDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/risk_sweep_it"
	return u.String()
}
