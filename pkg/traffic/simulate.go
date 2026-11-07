package traffic

import (
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Simulate 模拟一个请求经过规则表的过程，不消耗任何配额。
// draft 非空时用它替换同 ID 的规则（或追加到末尾）再模拟，供编辑器「先试再存」。
func (e *Engine) Simulate(in SimulateInput, draft *Rule) SimulateResult {
	c := e.cur.Load()
	r, ip := buildSimRequest(in)
	path := lowerPath(r.URL.Path)
	res := SimulateResult{Class: ClassNormal, Verdict: "allow", Steps: []SimulateStep{}}

	cfg := c.cfg
	switch {
	case !cfg.Settings.Enabled:
		res.Exempt, res.ExemptReason = true, "流量管理未开启"
	case r.Method == http.MethodOptions:
		res.Exempt, res.ExemptReason = true, "跨域预检请求"
	case path == "/healthz" || path == "/readyz":
		res.Exempt, res.ExemptReason = true, "探活路径"
	case isUpgrade(r):
		res.Exempt, res.ExemptReason = true, "WebSocket 连接"
	case cfg.Settings.ExemptAdmin && strings.HasPrefix(path, "/api/admin/"):
		res.Exempt, res.ExemptReason = true, "管理端免检"
	case c.exempt.match(path):
		res.Exempt, res.ExemptReason = true, "免检路径"
	}
	if res.Exempt {
		return res
	}

	// 编译后的规则按 ID 索引；草稿单独编译，状态是临时的（余量满额）。
	byID := make(map[string]*ruleRT, len(c.rules))
	for _, rt := range c.rules {
		byID[rt.spec.ID] = rt
	}
	rules := cfg.Rules
	if draft != nil {
		d := NormalizeRule(*draft)
		if d.ID == "" {
			d.ID = "__draft__"
		}
		d.Enabled = true
		replaced := false
		next := make([]Rule, 0, len(rules)+1)
		for _, x := range rules {
			if x.ID == d.ID {
				next = append(next, d)
				replaced = true
			} else {
				next = append(next, x)
			}
		}
		if !replaced {
			next = append(next, d)
		}
		rules = next
		byID[d.ID] = e.compileRule(d, newRuleState(d.ID), cfg.Settings, cfg.Settings.Cluster.Enabled && e.co != nil, int(e.instances.Load()))
	}

	now := mono()
	stopped := false
	classSet := false
	for _, spec := range rules {
		step := SimulateStep{RuleID: spec.ID, RuleName: spec.Name, Mode: spec.Mode, Terminal: spec.Terminal}
		rt := byID[spec.ID]
		switch {
		case stopped:
			step.Outcome, step.Miss = "skip", "已被前序规则终止"
		case !spec.Enabled || rt == nil:
			step.Outcome, step.Miss = "skip", "规则未启用"
		default:
			if miss := simMiss(rt, r, path, ip); miss != "" {
				step.Outcome, step.Miss = "skip", miss
				break
			}
			step.Matched = true
			if !classSet {
				res.Class, classSet = spec.Class, true
			}
			if spec.Mode == ModeBypass {
				step.Outcome, step.Detail = "bypass", "直接放行"
				stopped = true
				break
			}
			blocked, detail := simEnforce(e, rt, r, ip, now, &step)
			switch {
			case blocked && spec.Mode == ModeObserve:
				step.Outcome, step.Detail = "observe", detail
				if res.Verdict == "allow" {
					res.Verdict = "observe"
				}
			case blocked:
				step.Outcome, step.Detail = "reject", detail
				res.Verdict = "reject"
				stopped = true
			default:
				step.Outcome, step.Detail = "pass", detail
			}
			if spec.Terminal {
				stopped = true
			}
		}
		res.Steps = append(res.Steps, step)
	}
	if res.Verdict != "reject" && e.ovl.active.Load() && res.Class != ClassCritical {
		res.Steps = append(res.Steps, SimulateStep{RuleName: "过载保护", Outcome: "observe", Mode: ModeEnforce, Detail: "过载保护正在生效，本档位的请求可能被丢弃"})
	}
	return res
}

func buildSimRequest(in SimulateInput) (*http.Request, netip.Addr) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	raw := strings.TrimPrefix(strings.TrimSpace(in.Query), "?")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		if raw == "" {
			raw = path[i+1:]
		}
		path = path[:i]
	}
	r := &http.Request{
		Method: method,
		URL:    &url.URL{Path: path, RawQuery: raw},
		Host:   strings.TrimSpace(in.Host),
		Header: http.Header{},
	}
	for k, v := range in.Headers {
		if strings.TrimSpace(k) != "" {
			r.Header.Set(k, v)
		}
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(in.IP))
	if err != nil {
		ip = netip.MustParseAddr("203.0.113.10")
	}
	return r, ip
}

func simMiss(rt *ruleRT, r *http.Request, path string, ip netip.Addr) string {
	switch {
	case rt.methods != 0 && rt.methods&methodBit(r.Method) == 0:
		return "请求方法不符"
	case len(rt.paths) > 0 && !rt.paths.match(path):
		return "路径不符"
	case len(rt.excludes) > 0 && rt.excludes.match(path):
		return "路径已排除"
	case len(rt.hosts) > 0 && !matchHost(rt.hosts, r.Host):
		return "主机名不符"
	}
	if rt.appKeys != nil {
		if _, ok := rt.appKeys[requestAppKey(r)]; !ok {
			return "应用不符"
		}
	}
	switch {
	case rt.cidrs != nil && !rt.cidrs.Contains(ip.Unmap()):
		return "来源网段不符"
	case rt.exCidrs != nil && rt.exCidrs.Contains(ip.Unmap()):
		return "来源网段已排除"
	case !matchHeaders(rt.headers, r.Header):
		return "请求头条件不符"
	}
	return ""
}

// simEnforce 只读地估算每项控制的余量。
func simEnforce(e *Engine, rt *ruleRT, r *http.Request, ip netip.Addr, now int64, step *SimulateStep) (bool, string) {
	queue := rt.queueSize > 0
	if w := rt.warm; w != nil {
		p := w.limiter.params.Load()
		rem := p.remaining(w.limiter.tat.Load(), now)
		step.Keys = append(step.Keys, SimKey{Kind: "warmup", Value: "预热上限 " + strconv.FormatFloat(w.currentCeiling(), 'f', 0, 64) + "/秒", Remaining: rem, Capacity: int(p.burst), WouldPass: rem >= 1})
		if rem < 1 && !queue {
			return true, "预热上限已满"
		}
	}
	for _, l := range rt.limits {
		k := SimKey{Kind: l.spec.Key, Capacity: l.spec.Burst}
		switch {
		case l.lease != nil:
			k.Value = "集群总额"
			k.Remaining = float64(max(0, l.lease.tokens.Load()))
			k.WouldPass = k.Remaining >= 1 || l.lease.mode() == "fallback"
		case l.global != nil:
			k.Value = "全局"
			p := l.global.params.Load()
			k.Remaining = p.remaining(l.global.tat.Load(), now)
			k.Capacity = int(p.burst)
			k.WouldPass = k.Remaining >= 1
		default:
			h, ok := l.keyOf(e.seed, r, ip)
			if !ok {
				k.Value, k.WouldPass, k.Remaining = "未取到键值，不限制", true, float64(l.spec.Burst)
				step.Keys = append(step.Keys, k)
				continue
			}
			k.Value = l.keyDisplay(r, ip)
			p := l.keyed.params.Load()
			k.Remaining = p.remaining(l.keyed.peek(h), now)
			k.Capacity = int(p.burst)
			k.WouldPass = k.Remaining >= 1
		}
		step.Keys = append(step.Keys, k)
		if !k.WouldPass {
			if queue && l.lease == nil {
				return false, "限速已满，将排队等待（最长 " + strconv.FormatInt(rt.queueWait/int64(time.Millisecond), 10) + " 毫秒）"
			}
			return true, "限速已满"
		}
	}
	if cl := rt.conc; cl != nil {
		infl, lim := cl.inflight.Load(), cl.limit.Load()
		if infl >= lim {
			if queue {
				return false, "并发已满，将排队等待"
			}
			return true, "并发已满（" + strconv.FormatInt(infl, 10) + "/" + strconv.FormatInt(lim, 10) + "）"
		}
		return false, "并发余量 " + strconv.FormatInt(lim-infl, 10)
	}
	return false, ""
}
