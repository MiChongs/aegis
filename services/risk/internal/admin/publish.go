package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/decision"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/release"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/stream"
	riskv1 "aegis/services/risk/kitex_gen/aegis/risk/v1"

	"github.com/cloudwego/hertz/pkg/app"
)

func (s *Server) draftBundle(ctx context.Context, c *app.RequestContext) (any, error) {
	return s.Store.BuildBundle(ctx, c.Param("key"))
}

// compileDraft 编草稿并编译：返回问题列表（空 = 可以发布）
func (s *Server) compileDraft(ctx context.Context, key string) (bundle.Bundle, []engine.Issue, error) {
	b, err := s.Store.BuildBundle(ctx, key)
	if err != nil {
		return b, nil, err
	}
	p, err := engine.Compile(ctx, b, s.Models)
	if err != nil {
		var ce *engine.CompileError
		if errors.As(err, &ce) {
			return b, ce.Issues, nil
		}
		return b, nil, err
	}
	p.Dispose()
	return b, nil, nil
}

func (s *Server) validateScene(ctx context.Context, c *app.RequestContext) (any, error) {
	b, issues, err := s.compileDraft(ctx, c.Param("key"))
	if err != nil {
		return nil, err
	}
	if issues == nil {
		issues = []engine.Issue{}
	}
	return map[string]any{"ok": len(issues) == 0, "issues": issues, "features": len(b.Features), "rules": len(b.Rules), "models": len(b.Models)}, nil
}

type publishInput struct {
	Note          string `json:"note"`
	Mode          string `json:"mode"` // full（直接生效）/ canary（灰度）/ shadow（影子）
	CanaryPercent int    `json:"canaryPercent"`
}

// pushState 按发布状态写 etcd
func (s *Server) pushState(ctx context.Context, st store.State) error {
	if st.PrimaryID == nil {
		return release.Withdraw(ctx, s.Etcd, st.Scene)
	}
	p, err := s.Store.GetRelease(ctx, *st.PrimaryID)
	if err != nil {
		return err
	}
	rel := bundle.Release{Scene: st.Scene, Primary: p.Bundle, CanaryPercent: st.Canary, Shadow: st.Shadow}
	if st.CandidateID != nil {
		cand, err := s.Store.GetRelease(ctx, *st.CandidateID)
		if err != nil {
			return err
		}
		rel.Candidate = &cand.Bundle
	}
	_, err = release.Publish(ctx, s.Etcd, rel)
	return err
}

// syncStreamsFor 发布前把要用到的流式特征建好；删除不再被任何发布用到的
func (s *Server) syncStreamsFor(ctx context.Context, extra *bundle.Bundle) (*stream.SyncResult, error) {
	fs, err := s.liveStreamFeatures(ctx, extra)
	if err != nil {
		return nil, err
	}
	m, err := s.streamManager(ctx)
	if err != nil {
		return nil, err
	}
	defer m.Close(ctx)
	return m.Sync(ctx, fs)
}

func (s *Server) streamManager(ctx context.Context) (*stream.Manager, error) {
	m, err := stream.Connect(ctx, s.Cfg.RisingWaveDSN, s.Stream)
	if err != nil {
		return nil, err
	}
	m.OnDrop = func(ctx context.Context, durable string) {
		_ = s.Bus.JetStream().DeleteConsumer(ctx, events.StreamEvents, durable)
	}
	return m, nil
}

func (s *Server) publish(ctx context.Context, c *app.RequestContext) (any, error) {
	key := c.Param("key")
	var in publishInput
	if len(c.Request.Body()) > 0 {
		if err := bind(c, &in); err != nil {
			return nil, err
		}
	}
	if in.Mode == "" {
		in.Mode = "full"
	}
	b, issues, err := s.compileDraft(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, badData("草稿编译不通过", issues)
	}
	st, err := s.Store.GetState(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		st = &store.State{Scene: key}
	} else if err != nil {
		return nil, err
	}
	if in.Mode != "full" && st.PrimaryID == nil {
		return nil, bad("场景还没有正式版本，第一次只能直接发布")
	}
	sync, err := s.syncStreamsFor(ctx, &b)
	if err != nil {
		return nil, errors.New("流式特征同步失败：" + err.Error())
	}
	if len(sync.Failed) > 0 {
		return nil, badData("有流式特征建不出来", sync.Failed)
	}
	rel, err := s.Store.CreateRelease(ctx, b, in.Note, operator(c))
	if err != nil {
		return nil, err
	}
	switch in.Mode {
	case "full":
		st.PrimaryID, st.CandidateID, st.Canary, st.Shadow = &rel.ID, nil, 0, false
	case "canary":
		if in.CanaryPercent < 1 || in.CanaryPercent > 99 {
			return nil, bad("灰度比例要在 1-99 之间")
		}
		st.CandidateID, st.Canary, st.Shadow = &rel.ID, in.CanaryPercent, false
	case "shadow":
		st.CandidateID, st.Canary, st.Shadow = &rel.ID, 0, true
	default:
		return nil, bad("发布方式只能是 full / canary / shadow")
	}
	if err := s.pushState(ctx, *st); err != nil {
		return nil, errors.New("版本已保存，但下发 etcd 失败：" + err.Error())
	}
	if err := s.Store.SetState(ctx, *st, operator(c)); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "scene.publish", key, map[string]any{"version": rel.Version, "mode": in.Mode, "canary": in.CanaryPercent, "note": in.Note})
	return map[string]any{"release": map[string]any{"id": rel.ID, "version": rel.Version, "checksum": rel.Checksum}, "state": st, "streams": sync}, nil
}

func (s *Server) changeState(ctx context.Context, c *app.RequestContext, action string, mutate func(*store.State) error) (any, error) {
	key := c.Param("key")
	st, err := s.Store.GetState(ctx, key)
	if err != nil {
		return nil, bad("场景还没有发布")
	}
	if err := mutate(st); err != nil {
		return nil, err
	}
	if err := s.pushState(ctx, *st); err != nil {
		return nil, err
	}
	if err := s.Store.SetState(ctx, *st, operator(c)); err != nil {
		return nil, err
	}
	if _, err := s.syncStreamsFor(ctx, nil); err != nil && s.Logf != nil {
		s.Logf("流式特征清理失败：%v", err)
	}
	s.Store.Audit(ctx, operator(c), action, key, st)
	return st, nil
}

// promote 候选版本转正
func (s *Server) promote(ctx context.Context, c *app.RequestContext) (any, error) {
	return s.changeState(ctx, c, "scene.promote", func(st *store.State) error {
		if st.CandidateID == nil {
			return bad("没有候选版本")
		}
		st.PrimaryID, st.CandidateID, st.Canary, st.Shadow = st.CandidateID, nil, 0, false
		return nil
	})
}

// setTraffic 调灰度比例或结束候选
func (s *Server) setTraffic(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		CanaryPercent int  `json:"canaryPercent"`
		Shadow        bool `json:"shadow"`
		Drop          bool `json:"drop"` // 放弃候选版本
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	return s.changeState(ctx, c, "scene.traffic", func(st *store.State) error {
		if st.CandidateID == nil {
			return bad("没有候选版本")
		}
		if in.Drop {
			st.CandidateID, st.Canary, st.Shadow = nil, 0, false
			return nil
		}
		if !in.Shadow && (in.CanaryPercent < 1 || in.CanaryPercent > 99) {
			return bad("灰度比例要在 1-99 之间")
		}
		st.Canary, st.Shadow = in.CanaryPercent, in.Shadow
		if in.Shadow {
			st.Canary = 0
		}
		return nil
	})
}

// rollback 把任一历史版本设为正式版本
func (s *Server) rollback(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		ReleaseID int64 `json:"releaseId"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	r, err := s.Store.GetRelease(ctx, in.ReleaseID)
	if err != nil || r.Scene != c.Param("key") {
		return nil, bad("版本不存在")
	}
	if _, err := s.syncStreamsFor(ctx, &r.Bundle); err != nil {
		return nil, err
	}
	return s.changeState(ctx, c, "scene.rollback", func(st *store.State) error {
		st.PrimaryID, st.CandidateID, st.Canary, st.Shadow = &r.ID, nil, 0, false
		return nil
	})
}

// withdraw 下线：场景不再评估（Guard 按无发布放行）
func (s *Server) withdraw(ctx context.Context, c *app.RequestContext) (any, error) {
	return s.changeState(ctx, c, "scene.withdraw", func(st *store.State) error {
		st.PrimaryID, st.CandidateID, st.Canary, st.Shadow = nil, nil, 0, false
		return nil
	})
}

func (s *Server) listReleases(ctx context.Context, c *app.RequestContext) (any, error) {
	rs, err := s.Store.ListReleases(ctx, c.Param("key"))
	if err != nil {
		return nil, err
	}
	if rs == nil {
		rs = []store.Release{}
	}
	out := map[string]any{"releases": rs}
	if st, err := s.Store.GetState(ctx, c.Param("key")); err == nil {
		out["state"] = st
	}
	return out, nil
}

func (s *Server) getRelease(ctx context.Context, c *app.RequestContext) (any, error) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	return s.Store.GetRelease(ctx, id)
}

// ── 模拟 ──

type simulateInput struct {
	Request  engine.Request     `json:"request"`
	Draft    bool               `json:"draft"`    // 用草稿（默认用生效版本）
	Mode     string             `json:"mode"`     // fixed：特征与名单手动给定；live：走决策服务的真实数据源（只读）
	Features map[string]float64 `json:"features"` // fixed 模式的特征值
	Lists    []engine.ListHit   `json:"lists"`    // fixed 模式的名单命中
}

func (s *Server) simulate(ctx context.Context, c *app.RequestContext) (any, error) {
	key := c.Param("key")
	var in simulateInput
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	in.Request.Scene = key
	var b bundle.Bundle
	if in.Draft {
		var issues []engine.Issue
		var err error
		b, issues, err = s.compileDraft(ctx, key)
		if err != nil {
			return nil, err
		}
		if len(issues) > 0 {
			return nil, badData("草稿编译不通过", issues)
		}
	} else {
		st, err := s.Store.GetState(ctx, key)
		if err != nil || st.PrimaryID == nil {
			return nil, bad("场景没有生效的版本，请勾选「用草稿」")
		}
		r, err := s.Store.GetRelease(ctx, *st.PrimaryID)
		if err != nil {
			return nil, err
		}
		b = r.Bundle
	}
	if in.Mode == "live" {
		raw, _ := json.Marshal(b)
		resp, err := s.Decision.Simulate(ctx, &riskv1.SimulateRequest{Request: toRPC(in.Request), BundleJson: raw})
		if err != nil {
			return nil, err
		}
		var trace any
		_ = json.Unmarshal(resp.TraceJson, &trace)
		return map[string]any{"result": resp, "trace": trace}, nil
	}
	p, err := engine.Compile(ctx, b, s.Models)
	if err != nil {
		return nil, bad(err.Error())
	}
	defer p.Dispose()
	res, err := p.Evaluate(ctx, in.Request, engine.FixedSources{Features: in.Features, Lists: in.Lists}, engine.Options{DecisionID: "sim", Explain: true, Overrides: in.Features})
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": decision.ToResponse("sim", res, 0, false), "trace": res.Trace, "features": res.Features, "listHits": res.ListHits, "commands": res.Commands}, nil
}

// toRPC 引擎请求 → RPC 请求（模拟器的 live 模式）
func toRPC(r engine.Request) *riskv1.EvaluateRequest {
	extra, _ := json.Marshal(r.Extra)
	return &riskv1.EvaluateRequest{
		Scene: r.Scene, AppId: r.AppID, UserId: r.UserID, Account: r.Account, Ip: r.IP, UserAgent: r.UserAgent,
		CaptchaPassed: r.CaptchaPassed, CaptchaUnavailable: r.CaptchaUnavailable, Phone: r.Phone, Email: r.Email, TargetUserId: r.TargetUserID, Amount: r.Amount, ExtraJson: extra,
		Device: &riskv1.Device{Id: r.Device.ID, Platform: r.Device.Platform, Brand: r.Device.Brand, Model: r.Device.Model, OsVersion: r.Device.OSVersion,
			AppVersion: r.Device.AppVersion, Screen: r.Device.Screen, Locale: r.Device.Locale, Timezone: r.Device.Timezone, Emulator: r.Device.Emulator,
			Rooted: r.Device.Rooted, Attrs: r.Device.Attrs},
		Geo: &riskv1.Geo{Country: r.Geo.Country, Province: r.Geo.Province, City: r.Geo.City, Isp: r.Geo.ISP, Asn: r.Geo.ASN, Proxy: r.Geo.Proxy,
			Vpn: r.Geo.VPN, Tor: r.Geo.Tor, Datacenter: r.Geo.Datacenter, IpScore: int32(r.Geo.IPScore)},
		Explain: true,
	}
}
