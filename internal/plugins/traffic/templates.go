package trafficplugin

import "aegis/pkg/traffic"

// Template 规则模板：控制台「新建规则」的起点，选用后可以再改。
type Template struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Category    string       `json:"category"`
	Rule        traffic.Rule `json:"rule"`
}

// Templates 内置模板。路径取自真实路由表（internal/transport/http/testdata/routes.golden）。
func Templates() []Template {
	list := []Template{
		{
			ID: "site-warmup", Name: "全站突发爬坡", Category: "削峰",
			Description: "洪峰从基线速率起逐步放量，超出部分排队",
			Rule: traffic.Rule{
				Name: "全站突发爬坡", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match:  traffic.MatchSpec{Paths: []string{"/api/*"}},
				Warmup: &traffic.WarmupSpec{BaselineRPS: 2000, MaxRPS: 50000, StepPct: 25, IntervalMs: 1000, CooldownSeconds: 30},
				Queue:  &traffic.QueueSpec{Size: 20000, TimeoutMs: 2000},
			},
		},
		{
			ID: "per-ip", Name: "单 IP 限速", Category: "限速",
			Description: "每个来源 IP 的请求速率上限",
			Rule: traffic.Rule{
				Name: "单 IP 限速", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match:  traffic.MatchSpec{Paths: []string{"/api/*"}},
				Limits: []traffic.LimitSpec{{Key: traffic.KeyIP, Rate: 50, PeriodMs: 1000, Burst: 100}},
			},
		},
		{
			ID: "login-guard", Name: "登录防爆破", Category: "安全",
			Description: "限制单 IP 与单网段的登录尝试频率",
			Rule: traffic.Rule{
				Name: "登录防爆破", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassHigh,
				Match: traffic.MatchSpec{Methods: []string{"POST"}, Paths: []string{
					"/api/auth/login/password", "/api/auth/passkey/login", "/api/v1/apps/*/auth/login", "/api/v1/apps/*/auth/passkey/login", "/oauth2/login",
				}},
				Limits: []traffic.LimitSpec{
					{Key: traffic.KeyIP, Rate: 10, PeriodMs: 60_000, Burst: 10, Scope: traffic.ScopeCluster},
					{Key: traffic.KeySubnet, Rate: 60, PeriodMs: 60_000, Burst: 60, Scope: traffic.ScopeCluster},
				},
				Response: &traffic.ResponseSpec{Status: 429, Code: 42911, Message: "登录尝试过于频繁，请稍后再试", RetryAfterSeconds: 60},
			},
		},
		{
			ID: "register-flood", Name: "注册洪峰", Category: "安全",
			Description: "注册接口按 IP 限速并对总量预热",
			Rule: traffic.Rule{
				Name: "注册洪峰", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match:  traffic.MatchSpec{Methods: []string{"POST"}, Paths: []string{"/api/auth/register/password", "/api/v1/apps/*/auth/register"}},
				Limits: []traffic.LimitSpec{{Key: traffic.KeyIP, Rate: 5, PeriodMs: 60_000, Burst: 5, Scope: traffic.ScopeCluster}},
				Warmup: &traffic.WarmupSpec{BaselineRPS: 50, MaxRPS: 1000, StepPct: 20, IntervalMs: 1000, CooldownSeconds: 60},
				Queue:  &traffic.QueueSpec{Size: 2000, TimeoutMs: 3000},
			},
		},
		{
			ID: "sms-guard", Name: "短信验证码", Category: "安全",
			Description: "验证码发送按 IP 与全局双重限速",
			Rule: traffic.Rule{
				Name: "短信验证码", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match: traffic.MatchSpec{Methods: []string{"POST"}, Paths: []string{"/api/captcha/sms/send", "/api/v1/apps/*/auth/sms/code"}},
				Limits: []traffic.LimitSpec{
					{Key: traffic.KeyIP, Rate: 3, PeriodMs: 60_000, Burst: 3, Scope: traffic.ScopeCluster},
					{Key: traffic.KeyGlobal, Rate: 200, PeriodMs: 1000, Burst: 200, Scope: traffic.ScopeCluster},
				},
			},
		},
		{
			ID: "app-quota", Name: "应用配额", Category: "配额",
			Description: "每个应用的集群总速率上限",
			Rule: traffic.Rule{
				Name: "应用配额", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match:  traffic.MatchSpec{Paths: []string{"/api/v1/apps/*/*"}},
				Limits: []traffic.LimitSpec{{Key: traffic.KeyApp, Rate: 5000, PeriodMs: 1000, Burst: 10000, Scope: traffic.ScopeCluster}},
			},
		},
		{
			ID: "token-quota", Name: "单凭据限速", Category: "配额",
			Description: "每个登录凭据的请求速率上限",
			Rule: traffic.Rule{
				Name: "单凭据限速", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match:  traffic.MatchSpec{Paths: []string{"/api/*"}, Headers: []traffic.HeaderMatch{{Name: "Authorization", Op: "exists"}}},
				Limits: []traffic.LimitSpec{{Key: traffic.KeyToken, Rate: 30, PeriodMs: 1000, Burst: 60, Fallback: "skip"}},
			},
		},
		{
			ID: "ai-relay", Name: "AI 中转并发", Category: "并发",
			Description: "长连接接口按响应时间自适应并发",
			Rule: traffic.Rule{
				Name: "AI 中转并发", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassNormal,
				Match:       traffic.MatchSpec{Paths: []string{"/api/ai/v1/*"}},
				Concurrency: &traffic.ConcurrencySpec{Max: 400, Adaptive: true, MinLimit: 50, MaxLimit: 2000, LatencyToleranceX10: 30},
				Queue:       &traffic.QueueSpec{Size: 1000, TimeoutMs: 5000},
			},
		},
		{
			ID: "upload", Name: "上传并发", Category: "并发",
			Description: "限制同时进行的上传数",
			Rule: traffic.Rule{
				Name: "上传并发", Enabled: true, Mode: traffic.ModeEnforce, Class: traffic.ClassLow,
				Match: traffic.MatchSpec{Methods: []string{"POST", "PUT"}, Paths: []string{
					"/api/storage/upload", "/api/v1/apps/*/storage/upload", "/api/v1/apps/*/cloud/items/*/*/upload",
				}},
				Concurrency: &traffic.ConcurrencySpec{Max: 200},
				Queue:       &traffic.QueueSpec{Size: 500, TimeoutMs: 10000},
				Limits:      []traffic.LimitSpec{{Key: traffic.KeyIP, Rate: 20, PeriodMs: 60_000, Burst: 20}},
			},
		},
		{
			ID: "payment-priority", Name: "支付回调优先", Category: "放行",
			Description: "支付回调直接放行且不受过载保护影响",
			Rule: traffic.Rule{
				Name: "支付回调优先", Enabled: true, Mode: traffic.ModeBypass, Class: traffic.ClassCritical, Terminal: true,
				Match: traffic.MatchSpec{Paths: []string{"/api/public/pay/*"}},
			},
		},
		{
			ID: "crawler-observe", Name: "爬虫观察", Category: "观察",
			Description: "只统计不拦截，先看命中量再决定是否生效",
			Rule: traffic.Rule{
				Name: "爬虫观察", Enabled: true, Mode: traffic.ModeObserve, Class: traffic.ClassLow,
				Match:  traffic.MatchSpec{Headers: []traffic.HeaderMatch{{Name: "User-Agent", Op: "contains", Value: "bot"}}},
				Limits: []traffic.LimitSpec{{Key: traffic.KeyIP, Rate: 5, PeriodMs: 1000, Burst: 10}},
			},
		},
	}
	for i := range list {
		list[i].Rule = traffic.NormalizeRule(list[i].Rule)
	}
	return list
}
