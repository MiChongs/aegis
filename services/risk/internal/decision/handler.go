// Package decision risk-decision 的 Kitex 处理器
package decision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/release"
	riskv1 "aegis/services/risk/kitex_gen/aegis/risk/v1"

	"github.com/redis/go-redis/v9"
)

// Handler 实现 riskv1.DecisionService
type Handler struct {
	Registry *release.Registry
	Sources  engine.Sources
	Lists    *lists.Index
	Bus      *events.Bus
	Redis    redis.UniversalClient
	Models   engine.ModelLoader
	Instance string
	Deps     func(context.Context) map[string]string
	Logf     func(string, ...any)
}

var _ riskv1.DecisionService = (*Handler)(nil)

// ToRequest RPC 请求 → 引擎请求
func ToRequest(in *riskv1.EvaluateRequest) (engine.Request, error) {
	req := engine.Request{
		Scene: in.GetScene(), AppID: in.GetAppId(), UserID: in.GetUserId(), IdentityID: in.GetIdentityId(), Account: in.GetAccount(),
		IP: in.GetIp(), UserAgent: in.GetUserAgent(), CaptchaPassed: in.GetCaptchaPassed(), CaptchaUnavailable: in.GetCaptchaUnavailable(), RequestID: in.GetRequestId(),
		Phone: in.GetPhone(), Email: in.GetEmail(), TargetUserID: in.GetTargetUserId(), Amount: in.GetAmount(),
		// 在这里定下评估时刻：引擎、事件、流水用同一个时间（引擎里的 Normalize 只改它自己那份副本）
		Now: time.Now(),
	}
	if d := in.GetDevice(); d != nil {
		req.Device = engine.Device{ID: d.GetId(), Platform: d.GetPlatform(), Brand: d.GetBrand(), Model: d.GetModel(), OSVersion: d.GetOsVersion(),
			AppVersion: d.GetAppVersion(), Screen: d.GetScreen(), Locale: d.GetLocale(), Timezone: d.GetTimezone(), Emulator: d.GetEmulator(),
			Rooted: d.GetRooted(), Attrs: d.GetAttrs()}
	}
	if g := in.GetGeo(); g != nil {
		req.Geo = engine.Geo{Country: g.GetCountry(), Province: g.GetProvince(), City: g.GetCity(), ISP: g.GetIsp(), ASN: g.GetAsn(),
			Proxy: g.GetProxy(), VPN: g.GetVpn(), Tor: g.GetTor(), Datacenter: g.GetDatacenter(), IPScore: int(g.GetIpScore())}
	}
	if raw := in.GetExtraJson(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &req.Extra); err != nil {
			return req, fmt.Errorf("extra_json 不是 JSON 对象：%w", err)
		}
	}
	if req.Scene == "" {
		return req, errors.New("scene 为空")
	}
	return req, nil
}

// ToResponse 引擎结论 → RPC 响应
func ToResponse(id string, res *engine.Result, latency time.Duration, withTrace bool) *riskv1.EvaluateResponse {
	out := &riskv1.EvaluateResponse{
		DecisionId: id, Action: res.Action, RiskLevel: res.Level, Score: int32(res.Score), Reasons: res.Reasons,
		DimensionScores: map[string]int32{}, ModelScores: res.Models, Release: res.Release, LatencyUs: latency.Microseconds(),
		DecidedBy: res.DecidedBy, Degraded: res.Degraded,
	}
	for k, v := range res.Dimensions {
		out.DimensionScores[k] = int32(v)
	}
	for _, h := range res.Hits {
		out.Hits = append(out.Hits, &riskv1.RuleHit{Key: h.Key, Name: h.Name, Dimension: h.Dimension, Score: int32(h.Score), Mode: h.Mode, Decision: h.Decision, Reason: h.Reason})
	}
	for _, h := range res.ListHits {
		out.ListHits = append(out.ListHits, &riskv1.ListHit{ListType: h.ListType, Kind: h.Kind, Value: h.Value, ListKey: h.ListKey, Reason: h.Reason})
	}
	for _, c := range res.Commands {
		out.Commands = append(out.Commands, &riskv1.Command{Id: c.ID, Kind: c.Kind, Target: c.Target, Value: c.Value, DurationSeconds: c.DurationSeconds, Reason: c.Reason})
	}
	if withTrace && res.Trace != nil {
		out.TraceJson, _ = json.Marshal(res.Trace)
	}
	return out
}

func subjectOf(r engine.Request) string {
	switch {
	case r.UserID > 0:
		return "u" + strconv.FormatInt(r.UserID, 10)
	case r.Device.ID != "":
		return "d" + r.Device.ID
	case r.Account != "":
		return "a" + r.Account
	}
	return "i" + r.IP
}

func idemKey(appID int64, scene, requestID string) string {
	return "ridem:{" + strconv.FormatInt(appID, 10) + ":" + scene + ":" + requestID + "}"
}

// Evaluate 线上评估
func (h *Handler) Evaluate(ctx context.Context, in *riskv1.EvaluateRequest) (*riskv1.EvaluateResponse, error) {
	start := time.Now()
	req, err := ToRequest(in)
	if err != nil {
		return nil, err
	}
	// 幂等：同一个请求重复评估返回第一次的结论（不再计数、不再处置）
	if req.RequestID != "" && h.Redis != nil {
		if raw, err := h.Redis.Get(ctx, idemKey(req.AppID, req.Scene, req.RequestID)).Bytes(); err == nil {
			var cached riskv1.EvaluateResponse
			if json.Unmarshal(raw, &cached) == nil {
				return &cached, nil
			}
		}
	}
	active := h.Registry.Get(req.Scene)
	if active == nil || active.Primary == nil {
		// 没有发布的场景：放行，但照样留痕，控制台能看到哪些场景还没配置
		id := events.NewID()
		resp := &riskv1.EvaluateResponse{DecisionId: id, Action: bundle.ActionPass, RiskLevel: bundle.LevelNone, DecidedBy: "no_release", LatencyUs: time.Since(start).Microseconds()}
		h.publish(ctx, req, &engine.Result{Action: bundle.ActionPass, Level: bundle.LevelNone, DecidedBy: "no_release"}, id, "", time.Since(start), false)
		return resp, nil
	}
	prog, shadow := active.Pick(subjectOf(req))
	if !sceneApplies(prog.Bundle.Scene, req.AppID) {
		id := events.NewID()
		return &riskv1.EvaluateResponse{DecisionId: id, Action: bundle.ActionPass, RiskLevel: bundle.LevelNone, DecidedBy: "scope", Release: prog.Bundle.Version, LatencyUs: time.Since(start).Microseconds()}, nil
	}
	id := events.NewID()
	res, err := prog.Evaluate(ctx, req, h.Sources, engine.Options{DecisionID: id, Explain: true})
	if err != nil {
		return nil, err
	}
	h.execute(ctx, req, id, res.Commands)
	latency := time.Since(start)
	resp := ToResponse(id, res, latency, in.GetExplain())
	h.publish(ctx, req, res, id, prog.Bundle.Version, latency, false)

	if shadow != nil {
		// 影子版本：同样的输入再评估一遍，只留痕不生效；滑窗计数已被正式版本加过，影子只读不再加
		go func(req engine.Request) {
			sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			sid := events.NewID()
			sstart := time.Now()
			sres, err := shadow.Evaluate(sctx, req, readOnly{h.Sources}, engine.Options{DecisionID: sid, Explain: true})
			if err == nil {
				h.publish(sctx, req, sres, sid, shadow.Bundle.Version, time.Since(sstart), true)
			}
		}(req)
	}
	if req.RequestID != "" && h.Redis != nil {
		if raw, err := json.Marshal(resp); err == nil {
			h.Redis.Set(ctx, idemKey(req.AppID, req.Scene, req.RequestID), raw, 10*time.Minute)
		}
	}
	return resp, nil
}

func sceneApplies(s bundle.Scene, appID int64) bool {
	if len(s.AppIDs) == 0 {
		return true
	}
	for _, id := range s.AppIDs {
		if id == appID {
			return true
		}
	}
	return false
}

// readOnly 影子评估与模拟用：滑窗计数不递增（这类特征按默认值，模拟器上可手动给值）
type readOnly struct{ engine.Sources }

func (readOnly) Velocity(context.Context, []engine.VelocityQuery) (map[string]float64, error) {
	return nil, nil
}

// execute 处置：名单当场写 Redis（下一次请求立即生效），全部命令进 RISK_CMD 由各执行方持久化 / 执行
func (h *Handler) execute(ctx context.Context, req engine.Request, decisionID string, cmds []engine.Command) {
	if len(cmds) == 0 {
		return
	}
	var entries []lists.Entry
	now := time.Now().UTC()
	for _, c := range cmds {
		if c.Kind == engine.CmdListAdd {
			listType := map[string]string{"device": bundle.ListDevice, "ip": bundle.ListIP, "account": bundle.ListAccount, "user": bundle.ListUser}[c.Target]
			if listType == "" {
				continue
			}
			value, err := lists.Normalize(listType, c.Value)
			if err != nil {
				continue
			}
			e := lists.Entry{ID: c.ID, ListType: listType, Value: value, Kind: bundle.KindBlock, Reason: c.Reason, Source: "decision", DecisionID: decisionID, CreatedAt: now}
			if listType == bundle.ListAccount || listType == bundle.ListUser {
				e.AppID = req.AppID
			}
			if c.DurationSeconds > 0 {
				exp := now.Add(time.Duration(c.DurationSeconds) * time.Second)
				e.ExpiresAt = &exp
			}
			entries = append(entries, e)
		}
	}
	if len(entries) > 0 && h.Lists != nil {
		if err := h.Lists.Put(ctx, entries); err != nil && h.Logf != nil {
			h.Logf("处置写名单失败（命令仍会经 RISK_CMD 持久化）：%v", err)
		}
	}
	if h.Bus == nil {
		return
	}
	for _, c := range cmds {
		cmd := events.Command{ID: c.ID, Kind: c.Kind, Target: c.Target, Value: c.Value, AppID: req.AppID, DurationSeconds: c.DurationSeconds,
			Reason: c.Reason, Source: "decision", DecisionID: decisionID}
		if err := h.Bus.PublishCommand(ctx, cmd); err != nil && h.Logf != nil {
			h.Logf("处置命令发布失败：%v", err)
		}
	}
}

// publish 决策事件进 RISK 流（ClickHouse、图谱、向量、RisingWave、Flink 都从这里读）
func (h *Handler) publish(ctx context.Context, req engine.Request, res *engine.Result, id, version string, latency time.Duration, shadow bool) {
	if h.Bus == nil {
		return
	}
	env := res.Env
	e := events.Event{
		ID: id, Type: events.TypeDecision, Scene: req.Scene, TS: req.Now.UnixMilli(), AppID: req.AppID, UserID: req.UserID,
		Account: req.Account, IP: req.IP, DeviceID: req.Device.ID, UA: req.UserAgent, Country: req.Geo.Country, Province: req.Geo.Province,
		City: req.Geo.City, ISP: req.Geo.ISP, ASN: req.Geo.ASN, Phone: req.Phone, Email: req.Email, Amount: req.Amount,
		TargetUserID: req.TargetUserID, Action: res.Action, Level: res.Level, Score: res.Score, Release: version,
		Reasons: res.Reasons, Latency: latency.Microseconds(), Device: req.Device.Fingerprint(), Features: res.Features,
		Models: res.Models, Shadow: shadow, Props: req.Extra,
	}
	if e.TS == 0 {
		e.TS = time.Now().UnixMilli()
	}
	if env != nil {
		e.IPSubnet, e.PhonePrefix, e.EmailDomain = env.Net.Subnet, env.PhonePrefix, env.EmailDomain
	}
	for _, hit := range res.Hits {
		e.Hits = append(e.Hits, hit.Key)
	}
	if res.Trace != nil {
		if raw, err := json.Marshal(struct {
			*engine.Trace
			ListHits  []engine.ListHit `json:"listHits"`
			Commands  []engine.Command `json:"commands,omitempty"`
			DecidedBy string           `json:"decidedBy"`
			Degraded  []string         `json:"degraded,omitempty"`
		}{res.Trace, res.ListHits, res.Commands, res.DecidedBy, res.Degraded}); err == nil {
			e.Trace = string(raw)
		}
	}
	if err := h.Bus.PublishEvent(ctx, e); err != nil && h.Logf != nil {
		h.Logf("决策事件发布失败：%v", err)
	}
}

// Simulate 模拟：可带草稿发布包；默认不计数、不处置、不留痕
func (h *Handler) Simulate(ctx context.Context, in *riskv1.SimulateRequest) (*riskv1.EvaluateResponse, error) {
	start := time.Now()
	req, err := ToRequest(in.GetRequest())
	if err != nil {
		return nil, err
	}
	var prog *engine.Program
	if raw := in.GetBundleJson(); len(raw) > 0 {
		var b bundle.Bundle
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("发布包不是合法 JSON：%w", err)
		}
		prog, err = engine.Compile(ctx, b, h.Models)
		if err != nil {
			return nil, err
		}
		defer prog.Dispose()
	} else if a := h.Registry.Get(req.Scene); a != nil && a.Primary != nil {
		prog = a.Primary
	} else {
		return nil, fmt.Errorf("场景 %s 没有生效的发布", req.Scene)
	}
	var src engine.Sources = readOnly{h.Sources}
	if in.GetSideEffects() {
		src = h.Sources
	}
	id := "sim-" + events.NewID()
	res, err := prog.Evaluate(ctx, req, src, engine.Options{DecisionID: id, Explain: true})
	if err != nil {
		return nil, err
	}
	if in.GetSideEffects() {
		h.execute(ctx, req, id, res.Commands)
		h.publish(ctx, req, res, id, prog.Bundle.Version, time.Since(start), false)
	}
	return ToResponse(id, res, time.Since(start), true), nil
}

// Validate 编译草稿发布包，返回全部问题
func (h *Handler) Validate(ctx context.Context, in *riskv1.ValidateRequest) (*riskv1.ValidateResponse, error) {
	var b bundle.Bundle
	if err := json.Unmarshal(in.GetBundleJson(), &b); err != nil {
		return &riskv1.ValidateResponse{Issues: []*riskv1.ValidateIssue{{Stage: "bundle", Message: err.Error()}}}, nil
	}
	p, err := engine.Compile(ctx, b, h.Models)
	if err != nil {
		var ce *engine.CompileError
		if errors.As(err, &ce) {
			out := &riskv1.ValidateResponse{}
			for _, i := range ce.Issues {
				out.Issues = append(out.Issues, &riskv1.ValidateIssue{Stage: i.Stage, Key: i.Key, Message: i.Message})
			}
			return out, nil
		}
		return nil, err
	}
	p.Dispose()
	return &riskv1.ValidateResponse{Ok: true}, nil
}

// Status 实例状态
func (h *Handler) Status(ctx context.Context, _ *riskv1.StatusRequest) (*riskv1.StatusResponse, error) {
	out := &riskv1.StatusResponse{Instance: h.Instance, Dependencies: map[string]string{}}
	for _, a := range h.Registry.All() {
		s := &riskv1.SceneRelease{Scene: a.Scene, LoadedAt: a.LoadedAt.UnixMilli(), CanaryPercent: int32(a.Canary), Shadow: a.Shadow, Error: a.Error}
		if a.Primary != nil {
			s.Version, s.Checksum = a.Primary.Bundle.Version, a.Primary.Bundle.Checksum
		}
		if a.Candidate != nil {
			s.Candidate = a.Candidate.Bundle.Version
		}
		out.Scenes = append(out.Scenes, s)
	}
	if h.Deps != nil {
		out.Dependencies = h.Deps(ctx)
	}
	if h.Bus != nil {
		out.Dependencies["spool_pending"] = strconv.Itoa(h.Bus.Pending())
	}
	return out, nil
}
