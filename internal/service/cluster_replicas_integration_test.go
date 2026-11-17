package service

import (
	"bytes"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	firewalldomain "aegis/internal/domain/firewall"
	paymentdomain "aegis/internal/domain/payment"
	plugindomain "aegis/internal/domain/plugin"
	storagedomain "aegis/internal/domain/storage"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 多副本协调的集成测试：真实 Postgres（全部迁移）+ 进程内 NATS，两个节点标识模拟两台机器。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestClusterReplicasIntegration
func TestClusterReplicasIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	log := zap.NewNop()
	const appID = int64(31400)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cluster-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, password_hash, enabled) VALUES ($1, 'cluster-alice', 'x', TRUE) RETURNING id`, appID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, email, phone) VALUES ($1, 'alice@example.com', '')`, userID); err != nil {
		t.Fatal(err)
	}
	url := startTestNATS(t)
	nodeA, nodeB := newTestNode(t, url, "node-a"), newTestNode(t, url, "node-b")

	// 在 A 节点导出的凭证，负载均衡把下载打到 B 节点（另一块本地盘）也要下得到。
	t.Run("凭证跨节点下载", func(t *testing.T) {
		a := &PaymentService{log: log, pg: pg, receiptCfg: config.PaymentReceiptConfig{RootDir: t.TempDir()}}
		b := &PaymentService{log: log, pg: pg, receiptCfg: config.PaymentReceiptConfig{RootDir: t.TempDir(), SigningKey: "cluster-test"}}
		now := time.Now().UTC()
		pdf := []byte("%PDF-1.7 cluster receipt")
		export := paymentdomain.ReceiptExport{BillID: randomReceiptID(16), OrderNo: "ORD-CLUSTER", FileName: "receipt_ORD-CLUSTER_en.pdf",
			Locale: "en", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := a.persistReceipt(ctx, appID, userID, export, pdf); err != nil {
			t.Fatal(err)
		}
		data, name, err := b.DownloadUserOrderReceipt(ctx, &authdomain.Session{AppID: appID, UserID: userID}, export.BillID)
		if err != nil || !bytes.Equal(data, pdf) || name != export.FileName {
			t.Fatalf("B 节点应下到同一份文件：%v %q", err, name)
		}
		if _, _, err := b.DownloadUserOrderReceipt(ctx, &authdomain.Session{AppID: appID, UserID: userID + 1}, export.BillID); err == nil || !strings.Contains(err.Error(), "账单不存在") {
			t.Fatalf("别的用户不应下到：%v", err)
		}
		expires := now.Add(time.Hour).Unix()
		if data, _, err := b.DownloadSignedReceipt(ctx, appID, export.BillID, expires, b.signReceiptLink(appID, export.BillID, expires)); err != nil || !bytes.Equal(data, pdf) {
			t.Fatalf("签名链接在 B 节点应可下载：%v", err)
		}

		expired := export
		expired.BillID = randomReceiptID(16)
		expired.ExpiresAt = now.Add(-time.Minute)
		if err := a.persistReceipt(ctx, appID, userID, expired, pdf); err != nil {
			t.Fatal(err)
		}
		if _, _, err := b.DownloadUserOrderReceipt(ctx, &authdomain.Session{AppID: appID, UserID: userID}, expired.BillID); err == nil || !strings.Contains(err.Error(), "过期") {
			t.Fatalf("过期凭证应返回已过期：%v", err)
		}
		if rec, _ := pg.GetPaymentReceiptExport(ctx, appID, expired.BillID); rec != nil {
			t.Fatal("过期凭证被访问后应删除")
		}

		stale := export
		stale.BillID = randomReceiptID(16)
		stale.ExpiresAt = now.Add(-time.Minute)
		if err := a.persistReceipt(ctx, appID, userID, stale, pdf); err != nil {
			t.Fatal(err)
		}
		a.purgeExpiredStoredReceipts()
		if rec, _ := pg.GetPaymentReceiptExport(ctx, appID, stale.BillID); rec != nil {
			t.Fatal("清理任务应删掉过期凭证")
		}
		if rec, _ := pg.GetPaymentReceiptExport(ctx, appID, export.BillID); rec == nil {
			t.Fatal("清理任务不应删掉未过期的凭证")
		}
	})

	// 申诉复审：认领即占租，租期内别的实例（在线触发或兜底）都认领不走；租期过了才可接手。
	t.Run("申诉复审认领", func(t *testing.T) {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO ban_appeals (appid, user_id, content, created_at) VALUES ($1, $2, '误封', NOW() - INTERVAL '1 minute') RETURNING id`,
			appID, userID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if ok, err := pg.ClaimAppealReview(ctx, id, "node-a", false); err != nil || !ok {
			t.Fatalf("A 应认领成功：%v %v", ok, err)
		}
		if ok, _ := pg.ClaimAppealReview(ctx, id, "node-b", false); ok {
			t.Fatal("租期内 B 不应再认领成功")
		}
		if ids, _ := pg.ClaimUnreviewedAppeals(ctx, "node-b", 20); slices.Contains(ids, id) {
			t.Fatal("租期内兜底任务不应认领这条")
		}
		// 认领者没了（复审跑到一半进程退出，状态停在复审中）：租期过后兜底任务接手
		if _, err := pool.Exec(ctx, `UPDATE ban_appeals SET status = 'reviewing', review_claimed_at = NOW() - INTERVAL '10 minutes' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		ids, err := pg.ClaimUnreviewedAppeals(ctx, "node-b", 20)
		if err != nil || !slices.Contains(ids, id) {
			t.Fatalf("租期过后兜底任务应接手：%v %v", ids, err)
		}
		if ids, _ := pg.ClaimUnreviewedAppeals(ctx, "node-a", 20); slices.Contains(ids, id) {
			t.Fatal("刚被 B 认领的不应再被 A 认领")
		}
		if ok, _ := pg.ClaimAppealReview(ctx, id, "node-a", true); !ok {
			t.Fatal("管理员重跑应无视现有租约")
		}
		var holder string
		_ = pool.QueryRow(ctx, `SELECT review_claimed_by FROM ban_appeals WHERE id = $1`, id).Scan(&holder)
		if holder != "node-a" {
			t.Fatalf("租约应记在 node-a 名下，实得 %q", holder)
		}
	})

	// 存储迁移：同一个任务只能有一个持有者；持有者心跳停了才可接手；暂停后持有者的心跳失败（据此停下）。
	t.Run("存储迁移认领与心跳", func(t *testing.T) {
		newConfig := func(name string) int64 {
			cfg, err := pg.UpsertStorageConfig(ctx, storagedomain.Config{Scope: storagedomain.ScopeGlobal, Provider: storagedomain.ProviderLocal,
				ConfigName: name, Enabled: true, AccessMode: storagedomain.AccessPrivate, ConfigData: map[string]any{"root_dir": t.TempDir()}})
			if err != nil {
				t.Fatal(err)
			}
			return cfg.ID
		}
		m, err := pg.CreateStorageMigration(ctx, storagedomain.MigrationInput{SourceConfigID: newConfig("cluster-src"), TargetConfigID: newConfig("cluster-dst"), SetRedirect: true}, "test")
		if err != nil {
			t.Fatal(err)
		}
		from := []string{storagedomain.MigrationPending, storagedomain.MigrationRunning, storagedomain.MigrationPaused, storagedomain.MigrationFailed}
		claim := func(owner string, force bool) bool {
			ok, err := pg.ClaimStorageMigration(ctx, m.ID, owner, from, storageMigrationStaleAfter, force)
			if err != nil {
				t.Fatal(err)
			}
			return ok
		}
		beat := func(owner string) bool {
			ok, err := pg.HeartbeatStorageMigration(ctx, m.ID, owner)
			if err != nil {
				t.Fatal(err)
			}
			return ok
		}
		if !claim("node-a", false) {
			t.Fatal("A 应认领成功")
		}
		if claim("node-b", false) {
			t.Fatal("A 心跳新鲜时 B 不应认领成功")
		}
		if beat("node-b") || !beat("node-a") {
			t.Fatal("只有持有者能续心跳")
		}
		if err := pg.ReleaseStorageMigration(ctx, m.ID, "node-a"); err != nil {
			t.Fatal(err)
		}
		if !claim("node-b", false) {
			t.Fatal("A 放手后 B 应认领成功")
		}
		if _, err := pool.Exec(ctx, `UPDATE storage_migrations SET heartbeat_at = NOW() - INTERVAL '10 minutes' WHERE id = $1`, m.ID); err != nil {
			t.Fatal(err)
		}
		if !claim("node-a", false) {
			t.Fatal("B 心跳过期后 A 应接手")
		}
		if beat("node-b") {
			t.Fatal("被接手之后旧持有者的心跳应失败")
		}
		if ok, _ := pg.SetStorageMigrationStatus(ctx, m.ID, storagedomain.MigrationPaused, []string{storagedomain.MigrationRunning}, ""); !ok {
			t.Fatal("暂停失败")
		}
		if beat("node-a") {
			t.Fatal("暂停之后持有者的心跳应失败，据此停下")
		}
		if !claim("node-c", true) {
			t.Fatal("单实例模式（force）应直接认领")
		}
	})

	// 平台设置：A 改了防火墙，B 收到广播后从数据库重载自己的防火墙运行时。
	t.Run("平台设置广播重载", func(t *testing.T) {
		fwA, fwB := &syncFirewall{}, &syncFirewall{}
		a := NewPlatformSettingsService(config.Config{}, log, pg, fwA, nil, nil, nil, nil)
		b := NewPlatformSettingsService(config.Config{}, log, pg, fwB, nil, nil, nil, nil)
		a.SetCluster(nodeA.Cluster)
		b.SetCluster(nodeB.Cluster)
		nodeA.flush(t)
		nodeB.flush(t)
		rate := "321-M"
		if _, err := a.UpdateSettings(ctx, nil, systemdomain.SettingsUpdate{Firewall: systemdomain.FirewallSettingsPatch{GlobalRate: &rate}}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 重载防火墙", func() bool { return fwB.lastGlobalRate() == rate })
		if fwA.count() != 1 {
			t.Fatalf("A 只应在本地重载一次（不处理自己的广播），实得 %d", fwA.count())
		}
	})

	// 地域封禁：A 增删规则，B 的内存规则跟着变。
	t.Run("地域封禁广播重载", func(t *testing.T) {
		a, b := NewGeoBanService(log, pg, nil), NewGeoBanService(log, pg, nil)
		a.SetCluster(nodeA.Cluster)
		b.SetCluster(nodeB.Cluster)
		nodeA.flush(t)
		nodeB.flush(t)
		if err := b.Initialize(ctx); err != nil {
			t.Fatal(err)
		}
		ruleCount := func() int {
			b.mu.RLock()
			defer b.mu.RUnlock()
			return len(b.rules)
		}
		ban, err := a.Upsert(ctx, firewalldomain.GeoBanMutation{ScopeType: "country", ScopeValue: "AQ", Reason: "cluster-test", Enabled: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 加载新规则", func() bool { return ruleCount() == 1 })
		if err := a.Delete(ctx, ban.ID); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 移除规则", func() bool { return ruleCount() == 0 })
	})

	// 插件：A 启用插件，B 的钩子缓存里出现它，停用后消失。
	t.Run("插件钩子广播重建", func(t *testing.T) {
		a, b := NewPluginService(log, pg), NewPluginService(log, pg)
		t.Cleanup(a.Close)
		t.Cleanup(b.Close)
		a.SetCluster(nodeA.Cluster)
		b.SetCluster(nodeB.Cluster)
		nodeA.flush(t)
		nodeB.flush(t)
		p, err := a.CreatePlugin(ctx, plugindomain.CreatePluginInput{Name: "cluster-test", DisplayName: "cluster", Type: "expr", ExprScript: "true",
			Hooks: []plugindomain.HookBinding{{HookName: HookPaymentCreated, Phase: "after"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		hooked := func() bool {
			b.cacheMu.RLock()
			defer b.cacheMu.RUnlock()
			return len(b.hookCache[HookPaymentCreated]) == 1
		}
		if err := a.EnablePlugin(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 的钩子缓存出现插件", hooked)
		if err := a.DisablePlugin(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 的钩子缓存移除插件", func() bool { return !hooked() })
	})

	// 管理端用户搜索：索引每节点一份，A 上的写入由 B 自己从数据库重读。
	t.Run("用户搜索索引跨节点", func(t *testing.T) {
		newSearch := func(node testNode) *AdminUserSearchService {
			s, err := NewAdminUserSearchService(log, pg, config.AdminUserSearchConfig{RootDir: t.TempDir(), Shards: 2})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			s.SetCluster(node.Cluster)
			node.flush(t)
			return s
		}
		a, b := newSearch(nodeA), newSearch(nodeB)
		docID := adminUserSearchDocID(appID, userID)
		indexed := func(s *AdminUserSearchService) bool {
			doc, err := s.shardFor(userID).Document(docID)
			return err == nil && doc != nil
		}
		if err := a.IndexUser(ctx, appID, userID); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 自己重读并索引该用户", func() bool { return indexed(b) })
		if err := a.DeleteUser(appID, userID); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "B 删除该用户的条目", func() bool { return !indexed(b) })
		b.indexedApps.Store(appID, struct{}{})
		a.ResyncApp(appID)
		waitUntil(t, "B 忘掉已同步标记", func() bool {
			_, ok := b.indexedApps.Load(appID)
			return !ok
		})
	})
}

// syncFirewall 并发安全的防火墙运行时假实现（广播在 NATS 的回调协程里处理）。
type syncFirewall struct {
	mu      sync.Mutex
	reloads int
	cfg     config.FirewallConfig
}

func (f *syncFirewall) CurrentConfig() config.FirewallConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}
func (f *syncFirewall) ReloadMeta() (uint64, time.Time)            { return uint64(f.count()), time.Time{} }
func (f *syncFirewall) ValidateConfig(config.FirewallConfig) error { return nil }
func (f *syncFirewall) Reload(cfg config.FirewallConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	f.cfg = cfg
	return nil
}
func (f *syncFirewall) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloads
}
func (f *syncFirewall) lastGlobalRate() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg.GlobalRate
}
