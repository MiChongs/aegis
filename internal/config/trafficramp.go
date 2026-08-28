package config

import "strings"

// TrafficRampConfig 突发流量爬坡（自适应准入）配置。
//
// 与防火墙限流的分工：防火墙按 IP 拒绝**恶意**流量（Redis 计数，跨实例），
// 爬坡器按实例整形**合法**流量的进入速度 —— 突发洪峰不是一次性放进后端
// （冷连接池 / 冷缓存 / 下游依赖会被瞬间打穿），而是从基线速率起按步长逐步
// 抬高准入上限，多出来的请求先短暂排队削峰，排不下再礼貌拒绝（429 + Retry-After）。
// 峰值退去后上限按同样的节奏回落到基线，不会一直敞着。
//
// 每个实例独立整形自己的进入流量，因此所有参数都是**单实例**口径：
// 多副本部署时集群总吞吐 = 副本数 × MaxRPS。
type TrafficRampConfig struct {
	// Enabled 默认关闭：升级不应引入一次静默的行为变更。
	Enabled bool
	// BaselineRPS 稳态准入速率（req/s）。低于它的流量永远直接放行。
	BaselineRPS int
	// MaxRPS 爬坡的顶。上限爬到这里之后仍供不应求就进入 saturated 状态。
	MaxRPS int
	// RampStepPct 每个爬坡周期把上限抬高「基线 × 该百分比」。
	RampStepPct int
	// RampIntervalMs 爬坡周期（毫秒）。周期越短爬得越快。
	RampIntervalMs int
	// CooldownSeconds 峰值退去后，需求持续低于基线多少秒才开始回落。
	// 立刻回落会在锯齿形流量下反复爬坡-回落抖动。
	CooldownSeconds int
	// QueueSize 排队削峰的最大等待请求数（0 = 不排队，超限直接拒绝）。
	QueueSize int
	// QueueTimeoutMs 单个请求最多等待多久（毫秒），超时按拒绝处理。
	QueueTimeoutMs int
	// MaxConcurrent 在途请求并发上限（0 = 不限制）。速率管「进入的快慢」，
	// 并发管「同时在处理的多少」—— 慢接口堆积时速率正常也可能把进程拖垮。
	MaxConcurrent int
	// ExemptPathPrefixes 免整形路径前缀（/healthz、/readyz、/api/ws 恒免，无需登记）。
	ExemptPathPrefixes []string
	// ExemptAdmin true 时 /api/admin/ 前缀免整形。默认开：洪峰恰恰是管理员
	// 最需要打开控制台看统计、调参数的时刻，把他们也拒之门外等于自锁。
	ExemptAdmin bool
	// RetryAfterSeconds 拒绝响应里 Retry-After 头的秒数。
	RetryAfterSeconds int
}

// NormalizeTrafficRampConfig 铺默认值并夹取非法区间。
// 与 NormalizeFirewallConfig 同一戒律：任何入口（env / 数据库 / 裸 struct）
// 进来的配置都先过这里，保证运行期拿到的永远是自洽的。
func NormalizeTrafficRampConfig(cfg TrafficRampConfig) TrafficRampConfig {
	if cfg.BaselineRPS <= 0 {
		cfg.BaselineRPS = 300
	}
	if cfg.BaselineRPS > 1_000_000 {
		cfg.BaselineRPS = 1_000_000
	}
	if cfg.MaxRPS <= 0 {
		cfg.MaxRPS = cfg.BaselineRPS * 10
	}
	if cfg.MaxRPS < cfg.BaselineRPS {
		cfg.MaxRPS = cfg.BaselineRPS
	}
	if cfg.MaxRPS > 5_000_000 {
		cfg.MaxRPS = 5_000_000
	}
	if cfg.RampStepPct <= 0 {
		cfg.RampStepPct = 20
	}
	if cfg.RampStepPct > 1000 {
		cfg.RampStepPct = 1000
	}
	if cfg.RampIntervalMs <= 0 {
		cfg.RampIntervalMs = 1000
	}
	if cfg.RampIntervalMs < 100 {
		cfg.RampIntervalMs = 100
	}
	if cfg.RampIntervalMs > 60_000 {
		cfg.RampIntervalMs = 60_000
	}
	if cfg.CooldownSeconds <= 0 {
		cfg.CooldownSeconds = 30
	}
	if cfg.CooldownSeconds > 3600 {
		cfg.CooldownSeconds = 3600
	}
	if cfg.QueueSize < 0 {
		cfg.QueueSize = 0
	}
	if cfg.QueueSize > 100_000 {
		cfg.QueueSize = 100_000
	}
	if cfg.QueueTimeoutMs <= 0 {
		cfg.QueueTimeoutMs = 2000
	}
	if cfg.QueueTimeoutMs > 60_000 {
		cfg.QueueTimeoutMs = 60_000
	}
	if cfg.MaxConcurrent < 0 {
		cfg.MaxConcurrent = 0
	}
	if cfg.MaxConcurrent > 1_000_000 {
		cfg.MaxConcurrent = 1_000_000
	}
	if cfg.RetryAfterSeconds <= 0 {
		cfg.RetryAfterSeconds = 3
	}
	if cfg.RetryAfterSeconds > 600 {
		cfg.RetryAfterSeconds = 600
	}
	prefixes := make([]string, 0, len(cfg.ExemptPathPrefixes))
	for _, p := range cfg.ExemptPathPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		prefixes = append(prefixes, strings.ToLower(p))
	}
	cfg.ExemptPathPrefixes = prefixes
	return cfg
}
