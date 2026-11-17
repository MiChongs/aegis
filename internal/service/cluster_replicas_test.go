package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"aegis/internal/cluster"
	"aegis/internal/config"
	notifydomain "aegis/internal/domain/notify"
	storagedomain "aegis/internal/domain/storage"

	"github.com/alicebob/miniredis/v2"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 多副本协调在服务层的单测：判定的默认值、跨节点广播送达后的动作、跨实例合计限速。
// 广播用进程内起的 NATS 服务器 + 两个不同节点标识的集群句柄模拟两台机器。

// startTestNATS 起一个只监听本机随机端口的 NATS 服务器。
func startTestNATS(t *testing.T) string {
	t.Helper()
	ns, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats 未就绪")
	}
	t.Cleanup(ns.Shutdown)
	return ns.ClientURL()
}

// testNode 一个模拟节点：集群句柄 + 它的 NATS 连接（订阅后要 Flush，确保服务器已登记订阅再让别的节点发消息）。
type testNode struct {
	*cluster.Cluster
	nc *nats.Conn
}

func newTestNode(t *testing.T, url, id string) testNode {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	bus := cluster.NewBus(nc, id, zap.NewNop())
	t.Cleanup(func() {
		bus.Close()
		nc.Close()
	})
	return testNode{Cluster: &cluster.Cluster{NodeID: id, Bus: bus}, nc: nc}
}

func (n testNode) flush(t *testing.T) {
	t.Helper()
	if err := n.nc.Flush(); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

// 没接集群、或集群没有选主器（单测、单机老部署）时，任务判定一律放行 —— 行为与改造前的单实例一致。
func TestClusterTaskWithoutElectorAlwaysOwned(t *testing.T) {
	var zero clusterTask
	if !zero.owned() {
		t.Fatal("零值判定应放行")
	}
	var nilTask *clusterTask
	if !nilTask.owned() {
		t.Fatal("nil 判定应放行")
	}
	var unbound clusterTask
	unbound.bind(nil, "x.y")
	if !unbound.owned() {
		t.Fatal("bind(nil) 之后应仍放行")
	}
	var bound clusterTask
	bound.bind(&cluster.Cluster{}, "x.y")
	if !bound.owned() {
		t.Fatal("没有 Elector 的集群句柄应放行")
	}

	var link clusterLink
	if link.attached() {
		t.Fatal("零值 clusterLink 不应视为已接集群")
	}
	link.publish(zap.NewNop(), cluster.TopicGeoBan, map[string]any{"op": "noop"}) // 不应 panic
	if link.nodeID() != "" {
		t.Fatal("没接集群时节点标识应为空")
	}
}

// 支付的四项后台任务各登记一个稳定的任务名，未调用 SetCluster 时全部放行。
func TestPaymentClusterTasksDefaultToOwned(t *testing.T) {
	s := &PaymentService{}
	for name, task := range map[string]*clusterTask{
		"expiry": &s.expiryTask, "refund": &s.refundSyncTask, "settle": &s.remoteSettleTask, "receipt": &s.receiptPurgeTask,
	} {
		if !task.owned() {
			t.Fatalf("%s：未接集群时应放行", name)
		}
	}
	s.SetCluster(&cluster.Cluster{})
	if !s.expiryTask.owned() || !s.remoteSettleTask.owned() {
		t.Fatal("没有 Elector 时应放行")
	}
}

// 暂停 / 取消请求打到没在跑这个任务的实例上：广播出去，持有它的实例收到后停下。
func TestStorageMigrationCancelReachesOwnerOnOtherNode(t *testing.T) {
	url := startTestNATS(t)
	nodeA, nodeB := newTestNode(t, url, "node-a"), newTestNode(t, url, "node-b")

	newStorage := func(node testNode) *StorageService {
		s := &StorageService{log: zap.NewNop()}
		s.migrations = newStorageMigrationRunner(s)
		s.SetCluster(node.Cluster)
		node.flush(t)
		return s
	}
	a, b := newStorage(nodeA), newStorage(nodeB)
	t.Cleanup(func() { localStorageClustered.Store(false) })

	// B 上「正在跑」迁移 #42
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.migrations.mu.Lock()
	b.migrations.running[42] = cancel
	b.migrations.mu.Unlock()

	a.stopMigration(42)
	waitUntil(t, "B 上的迁移任务被取消", func() bool { return ctx.Err() != nil })

	// 本地在跑的任务直接本地取消，不必经过广播
	localCtx, localCancel := context.WithCancel(context.Background())
	defer localCancel()
	a.migrations.mu.Lock()
	a.migrations.running[7] = localCancel
	a.migrations.mu.Unlock()
	if !a.migrations.cancel(7) || localCtx.Err() == nil {
		t.Fatal("本地任务应被直接取消")
	}
	if a.migrations.cancel(999) {
		t.Fatal("本地没有的任务不应报告已取消")
	}
}

// 广播自己发出的消息自己不处理：发起方本地已经改好了，再处理一遍只会白读一次库。
func TestClusterBusSkipsOwnMessages(t *testing.T) {
	url := startTestNATS(t)
	node := newTestNode(t, url, "node-a")
	other := newTestNode(t, url, "node-b")

	got := make(chan string, 4)
	node.Subscribe(cluster.TopicGeoFence, func(payload []byte) { got <- "self" })
	other.Subscribe(cluster.TopicGeoFence, func(payload []byte) { got <- "other" })
	node.flush(t)
	other.flush(t)

	var link clusterLink
	link.set(node.Cluster)
	link.publish(zap.NewNop(), cluster.TopicGeoFence, clusterReloadNotice{Op: "update", ID: 1})
	select {
	case who := <-got:
		if who != "other" {
			t.Fatalf("发起方不应收到自己的广播，收到方：%s", who)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("另一节点没收到广播")
	}
	select {
	case who := <-got:
		t.Fatalf("多收到一条：%s", who)
	case <-time.After(100 * time.Millisecond):
	}
}

// 平台设置广播只接受非空分区列表：空列表在 reloadPersisted 里表示「全部」，不能因为一条坏消息就全量重载。
func TestPlatformSettingsIgnoresEmptyClusterChange(t *testing.T) {
	fw := &recordingFirewall{}
	s := &PlatformSettingsService{log: zap.NewNop(), firewall: fw} // pg 为 nil：若真去重载会 panic
	s.handleClusterChange([]byte(`{"sections":[]}`))
	s.handleClusterChange([]byte(`not json`))
	if fw.reloads != 0 {
		t.Fatalf("不应重载，实得 %d 次", fw.reloads)
	}
}

// UAPIs 这类按密钥整体限额的上游：两个进程（两个 Redis 客户端）合计不超过上限。
func TestSharedWindowTakeCountsAcrossProcesses(t *testing.T) {
	mr := miniredis.RunT(t)
	clientA := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	clientB := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })

	ctx := context.Background()
	now := time.UnixMilli(1_800_000_000_250) // 窗口内第 250 毫秒
	allowed := 0
	for i, client := range []*redislib.Client{clientA, clientB, clientA, clientB} {
		ok, wait, err := sharedWindowTake(ctx, client, "t:geoip:rate:uapis", 3, time.Second, now)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			allowed++
			continue
		}
		if i != 3 {
			t.Fatalf("第 %d 次不应被拒", i+1)
		}
		if wait != 750*time.Millisecond {
			t.Fatalf("应等到下一个窗口（750ms），实得 %s", wait)
		}
	}
	if allowed != 3 {
		t.Fatalf("合计应放行 3 次，实得 %d", allowed)
	}
	// 下一个窗口重新计数
	if ok, _, _ := sharedWindowTake(ctx, clientB, "t:geoip:rate:uapis", 3, time.Second, now.Add(time.Second)); !ok {
		t.Fatal("下一个窗口应放行")
	}

	// 小于 1 的 QPS：窗口拉长，上限 1 次
	w := newSharedRateWindow(clientA, "t:slow", 0.5)
	if w.limit != 1 || w.period != 2*time.Second {
		t.Fatalf("0.5 QPS 应为每 2 秒 1 次，实得 %d/%s", w.limit, w.period)
	}
	if newSharedRateWindow(nil, "t:none", 4) != nil {
		t.Fatal("没有 Redis 时不应建共享窗口")
	}
	var nilWindow *sharedRateWindow
	if err := nilWindow.Wait(ctx); err != nil {
		t.Fatal("nil 共享窗口应直接放行")
	}
}

// 通知渠道限流接了 Redis 之后按全集群合计：两个通知出口（API 与 Worker / 多副本）共用一份额度。
func TestNotifyHubRateLimitSharedAcrossHubs(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	newHub := func() *NotifyHub {
		h := &NotifyHub{log: zap.NewNop(), rateState: map[int64]*notifyRateWindow{}}
		h.SetRedis(rdb, "t")
		return h
	}
	api, worker := newHub(), newHub()
	channel := &notifydomain.Channel{ID: 9, RateLimit: 2}
	ctx := context.Background()
	if !api.allowRate(ctx, channel) || !worker.allowRate(ctx, channel) {
		t.Fatal("额度内应放行")
	}
	if api.allowRate(ctx, channel) || worker.allowRate(ctx, channel) {
		t.Fatal("两个出口合计已用满，应拒绝")
	}

	// 没接 Redis：退回进程内计数，与改造前一致
	local := &NotifyHub{log: zap.NewNop(), rateState: map[int64]*notifyRateWindow{}}
	if !local.allowRate(ctx, channel) || !local.allowRate(ctx, channel) || local.allowRate(ctx, channel) {
		t.Fatal("进程内计数应在第 3 次拒绝")
	}
	// Redis 挂了：退回进程内计数，而不是一律放行或一律拒绝
	mr.Close()
	if !api.allowRate(ctx, &notifydomain.Channel{ID: 10, RateLimit: 1}) {
		t.Fatal("Redis 不可用时应退回进程内计数并放行首条")
	}
}

// 内存与数据库的历史曲线：接上集群后按节点分键，单实例保持原来的键。
func TestMonitorHistoryKeysPerNode(t *testing.T) {
	mon := NewMemoryMonitor(zap.NewNop(), nil, "t", time.Second, time.Minute)
	if got := mon.historyKey(); got != "t:memory:history" {
		t.Fatalf("单实例应沿用共享键，实得 %s", got)
	}
	mon.SetHistoryNode("node-a")
	if got := mon.historyKey(); got != "t:memory:history:node:node-a" {
		t.Fatalf("应按节点分键，实得 %s", got)
	}

	dbm := &DatabaseManager{prefix: "t"}
	if got := dbm.historyKey(); got != "t:database:history" {
		t.Fatalf("单实例应沿用共享键，实得 %s", got)
	}
	// 历史按副本的稳定标识分键（重启后接得上），不是每次启动都变的租约持有者 ID
	dbm.SetCluster(&cluster.Cluster{NodeID: "node-b-3f9a", Instance: "node-b"})
	if got := dbm.historyKey(); got != "t:database:history:node:node-b" {
		t.Fatalf("应按节点分键，实得 %s", got)
	}
	if !dbm.reaperTask.owned() {
		t.Fatal("没有 Elector 时清道夫判定应放行")
	}
}

// 本地存储池：接上集群后探活详情里如实写出多实例风险，单机不提示。
func TestLocalStorageHealthWarnsWhenClustered(t *testing.T) {
	cfg := &storagedomain.Config{
		Scope:      storagedomain.ScopeGlobal,
		Provider:   storagedomain.ProviderLocal,
		ConfigData: map[string]any{"root_dir": t.TempDir()},
	}
	provider := newLocalStorageProvider()
	details, err := provider.HealthCheck(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := details["warning"]; ok {
		t.Fatal("单机不应提示多实例风险")
	}
	localStorageClustered.Store(true)
	t.Cleanup(func() { localStorageClustered.Store(false) })
	details, err = provider.HealthCheck(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if details["multi_instance_safe"] != false || !strings.Contains(details["warning"].(string), "S3") {
		t.Fatalf("接上集群后应提示改用 S3 兼容存储池，实得 %v", details)
	}
}

// recordingFirewall 记录 Reload 调用的防火墙运行时假实现。
type recordingFirewall struct {
	reloads int
	last    config.FirewallConfig
}

func (f *recordingFirewall) CurrentConfig() config.FirewallConfig { return f.last }
func (f *recordingFirewall) ReloadMeta() (uint64, time.Time)      { return uint64(f.reloads), time.Time{} }
func (f *recordingFirewall) ValidateConfig(config.FirewallConfig) error {
	return nil
}
func (f *recordingFirewall) Reload(cfg config.FirewallConfig) error {
	f.reloads++
	f.last = cfg
	return nil
}
