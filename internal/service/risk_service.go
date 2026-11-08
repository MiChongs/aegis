package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	devicedomain "aegis/internal/domain/device"
	plugindomain "aegis/internal/domain/plugin"
	securitydomain "aegis/internal/domain/security"
	apperrors "aegis/pkg/errors"
	riskevents "aegis/services/risk/events"
	riskv1 "aegis/services/risk/kitex_gen/aegis/risk/v1"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

	"github.com/cloudwego/kitex/client"
	etcdreg "github.com/kitex-contrib/registry-etcd"
	"github.com/redis/go-redis/v9"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// 风控已拆成独立的风控平台（services/risk，docs/risk-platform.md）。主服务这一侧只剩接入层：
//
//   - Guard：业务入口调用，Kitex 经 etcd 发现 risk-decision 评估；超时或出错按放行处理并记日志
//   - Report：业务结果事件（登录成功 / 失败、注册、支付……）发进 RISK 流，流式特征、行为模式、关系图都从这里来
//   - 命令消费（risk_commands.go）：风控判出来要封号、封 IP、踢会话的，在主服务这里执行
//
// 业务代码的调用方式与旧风控中心完全一样（Guard / riskHook / 40398 / 40015），SDK 不用改。

const (
	errCodeRiskBlocked         = 40398
	errCodeRiskCaptchaRequired = 40015
)

// RiskDecisionServiceName risk-decision 在 etcd 里的服务名
const RiskDecisionServiceName = "aegis.risk.decision"

// RiskService 风控接入层
type RiskService struct {
	log    *zap.Logger
	cfg    config.RiskConfig
	client decisionservice.Client
	bus    *riskevents.Bus
	redis  *redis.Client
	prefix string

	mu           sync.RWMutex
	location     *LocationService
	ipRep        IPReputationProvider
	captchaProbe func(ctx context.Context, appID int64) bool
	accountBans  riskAccountBanner
	firewall     riskFirewall
	sessions     riskSessionRevoker

	stopCommands func()

	etcd       *clientv3.Client
	adminMu    sync.Mutex
	adminAddrs []string
	adminAt    time.Time
}

// NewRiskService 接风控平台。没配 RISK_ETCD_ENDPOINTS 时返回一个只放行的实例（开发环境不起风控也能用）。
//
// name 区分同一进程里的多个实例（API 与 Worker 在统一模式下同进程，各自的待发队列文件不能同名）。
func NewRiskService(cfg config.RiskConfig, log *zap.Logger, name, natsURL string, redisClient *redis.Client, keyPrefix string) *RiskService {
	s := &RiskService{log: log, cfg: cfg, redis: redisClient, prefix: keyPrefix}
	if provider := buildIPReputationProvider(cfg.IPReputation); provider != nil {
		s.ipRep = wrapIPReputationProvider(provider)
	}
	if len(cfg.EtcdEndpoints) == 0 {
		log.Warn("未配置 RISK_ETCD_ENDPOINTS：风控平台未接入，Guard 一律放行")
		return s
	}
	resolver, err := etcdreg.NewEtcdResolver(cfg.EtcdEndpoints)
	if err != nil {
		log.Error("风控 etcd 解析器创建失败，Guard 一律放行", zap.Error(err))
		return s
	}
	cli, err := decisionservice.NewClient(RiskDecisionServiceName, client.WithResolver(resolver), client.WithRPCTimeout(cfg.Timeout),
		client.WithConnectTimeout(200*time.Millisecond))
	if err != nil {
		log.Error("风控决策客户端创建失败，Guard 一律放行", zap.Error(err))
		return s
	}
	s.client = cli
	if etcdCli, err := clientv3.New(clientv3.Config{Endpoints: cfg.EtcdEndpoints, DialTimeout: 3 * time.Second}); err == nil {
		s.etcd = etcdCli
	} else {
		log.Warn("风控 etcd 客户端创建失败，管理接口改用 RISK_ADMIN_URL", zap.Error(err))
	}
	if natsURL != "" {
		bus, err := riskevents.Connect(natsURL, cfg.SpoolDir, name, func(format string, args ...any) {
			log.Warn(fmt.Sprintf(format, args...))
		})
		if err != nil {
			log.Error("风控事件总线连接失败，业务事件不上报", zap.Error(err))
		} else {
			s.bus = bus
		}
	}
	return s
}

func buildIPReputationProvider(cfg config.RiskIPReputationConfig) IPReputationProvider {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "ipqs", "ipqualityscore":
		if strings.TrimSpace(cfg.IPQS.APIKey) != "" {
			return NewIPQualityScoreProvider(cfg)
		}
	}
	return nil
}

// Enabled 风控平台是否接上
func (s *RiskService) Enabled() bool { return s != nil && s.client != nil }

// SetLocationService 接 IP 归属地（评估请求里的国家 / 省 / 市 / 运营商）
func (s *RiskService) SetLocationService(location *LocationService) {
	s.mu.Lock()
	s.location = location
	s.mu.Unlock()
}

// SetCaptchaProbe 接「应用有没有可用的图形验证码」判定
func (s *RiskService) SetCaptchaProbe(probe func(ctx context.Context, appID int64) bool) {
	s.mu.Lock()
	s.captchaProbe = probe
	s.mu.Unlock()
}

// Close 关掉命令消费与事件总线（待发事件先尽量发完）
func (s *RiskService) Close(_ context.Context) {
	if s == nil {
		return
	}
	if s.stopCommands != nil {
		s.stopCommands()
	}
	if s.bus != nil {
		s.bus.Close()
	}
	if s.etcd != nil {
		_ = s.etcd.Close()
	}
}

// Reload 热更新：超时与 IP 情报源（etcd 地址改了需要重启）
func (s *RiskService) Reload(cfg config.RiskConfig) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cfg.IPReputation = cfg.IPReputation
	if provider := buildIPReputationProvider(cfg.IPReputation); provider != nil {
		s.ipRep = wrapIPReputationProvider(provider)
	} else {
		s.ipRep = nil
	}
	s.mu.Unlock()
}

// RiskGuardOption Guard 的业务定制：各业务有自己的拒绝错误码与文案
type RiskGuardOption struct {
	Code    int
	Message string
}

// Guard 评估一次业务请求并翻译成业务错误：拦截 / 封禁 → 403，需要验证码 → 40015，其余放行（复核是事后的）。
// 风控自身出错时放行并记日志 —— 风控抖一下不该让全站登录不了。
func (s *RiskService) Guard(ctx context.Context, req securitydomain.RiskEvalRequest, opts ...RiskGuardOption) (*securitydomain.RiskEvalResult, error) {
	if !s.Enabled() {
		return nil, nil
	}
	result, err := s.Evaluate(ctx, req)
	if err != nil {
		s.log.Warn("风控评估失败，按放行处理", zap.String("scene", req.Scene), zap.Error(err))
		return nil, nil
	}
	return result, GuardError(result, opts...)
}

// Evaluate 评估（不翻译成业务错误）
func (s *RiskService) Evaluate(ctx context.Context, req securitydomain.RiskEvalRequest) (*securitydomain.RiskEvalResult, error) {
	if !s.Enabled() {
		return nil, errors.New("风控平台未接入")
	}
	in := s.buildRequest(ctx, req)
	resp, err := s.client.Evaluate(ctx, in)
	if err != nil {
		return nil, err
	}
	out := &securitydomain.RiskEvalResult{
		DecisionID: resp.GetDecisionId(), Action: resp.GetAction(), RiskLevel: resp.GetRiskLevel(), Score: int(resp.GetScore()),
		Reasons: resp.GetReasons(), DecidedBy: resp.GetDecidedBy(), Release: resp.GetRelease(), LatencyMS: int(resp.GetLatencyUs() / 1000),
		Degraded: resp.GetDegraded(),
	}
	for _, h := range resp.GetHits() {
		if h.GetMode() == "observe" {
			continue
		}
		out.Hits = append(out.Hits, securitydomain.RiskRuleHit{Key: h.GetKey(), Name: h.GetName(), Dimension: h.GetDimension(), Score: int(h.GetScore()), Reason: h.GetReason()})
	}
	return out, nil
}

// buildRequest 业务请求 → 评估请求：补齐归属地、IP 情报与验证码可用性。
// 这些都只读缓存：缓存没有就带空值去评估、后台去刷新，不让一次登录等外部情报源。
func (s *RiskService) buildRequest(ctx context.Context, req securitydomain.RiskEvalRequest) *riskv1.EvaluateRequest {
	in := &riskv1.EvaluateRequest{
		Scene: req.Scene, Account: strings.TrimSpace(req.Account), Ip: strings.TrimSpace(req.IP), UserAgent: req.UserAgent,
		CaptchaPassed: req.CaptchaPassed || riskCaptchaPassed(ctx), RequestId: req.RequestID, Phone: req.Phone, Email: req.Email,
		TargetUserId: req.TargetUserID,
	}
	if req.AppID != nil {
		in.AppId = *req.AppID
	}
	if req.UserID != nil {
		in.UserId = *req.UserID
	}
	if req.IdentityID != nil {
		in.IdentityId = *req.IdentityID
	}
	if in.Account == "" {
		if v, ok := req.Extra["account"].(string); ok {
			in.Account = strings.TrimSpace(v)
		}
	}
	if amount, ok := req.Extra["amount"]; ok {
		in.Amount = toFloat64(amount)
	}
	if len(req.Extra) > 0 {
		in.ExtraJson, _ = jsonMarshal(req.Extra)
	}
	dev := &riskv1.Device{Id: strings.TrimSpace(req.DeviceID), Attrs: map[string]string{}}
	for k, v := range req.Device {
		switch k {
		case "platform":
			dev.Platform = v
		case "brand":
			dev.Brand = v
		case "model":
			dev.Model = v
		case "os_version", "osVersion":
			dev.OsVersion = v
		case "app_version", "appVersion":
			dev.AppVersion = v
		case "screen":
			dev.Screen = v
		case "locale":
			dev.Locale = v
		case "timezone":
			dev.Timezone = v
		case "emulator":
			dev.Emulator = v == "true"
		case "rooted":
			dev.Rooted = v == "true"
		default:
			if v != "" {
				dev.Attrs[k] = v
			}
		}
	}
	// 业务没传设备要素时，取传输层放进上下文的客户端设备信息（登录、下单等请求头里的 X-Device-*）
	if dev.Model == "" && dev.Platform == "" && dev.Brand == "" {
		fillDeviceFromContext(ctx, dev)
	}
	in.Device = dev

	s.mu.RLock()
	location, ipRep, probe := s.location, s.ipRep, s.captchaProbe
	s.mu.RUnlock()
	geo := &riskv1.Geo{}
	if in.Ip != "" && location != nil {
		if loc, ok := location.GetCached(ctx, in.Ip); ok {
			geo.Country, geo.Province, geo.City, geo.Isp = loc.CountryCode, loc.Region, loc.City, loc.ISP
		} else {
			location.RefreshAsync(in.Ip)
		}
	}
	if in.Ip != "" && ipRep != nil {
		if rec, ok := s.cachedIPReputation(ctx, in.Ip); ok {
			geo.Proxy, geo.Vpn, geo.Tor, geo.Datacenter, geo.IpScore = rec.IsProxy, rec.IsVPN, rec.IsTor, rec.IsDatacenter, int32(rec.RiskScore)
			if geo.Isp == "" {
				geo.Isp = rec.ISP
			}
		}
	}
	in.Geo = geo
	if probe != nil && in.AppId > 0 {
		in.CaptchaUnavailable = !probe(ctx, in.AppId)
	}
	return in
}

// fillDeviceFromContext 用请求上下文里的客户端设备信息补设备要素
func fillDeviceFromContext(ctx context.Context, dev *riskv1.Device) {
	client := devicedomain.ClientFrom(ctx)
	extra := client.Extra.Normalize()
	dev.Model, dev.Platform = client.Model, client.Platform
	dev.Brand = firstNonEmpty(extra.Brand, extra.Manufacturer)
	dev.OsVersion, dev.AppVersion = extra.OSVersion, extra.AppVersion
	if extra.Codename != "" {
		dev.Attrs["codename"] = extra.Codename
	}
	if extra.OS != "" && dev.Platform == "" {
		dev.Platform = strings.ToLower(extra.OS)
	}
}

// DeviceFingerprint 请求上下文里的设备要素（事件上报用，键与风控平台一致）
func DeviceFingerprint(ctx context.Context) map[string]string {
	dev := &riskv1.Device{Attrs: map[string]string{}}
	fillDeviceFromContext(ctx, dev)
	out := map[string]string{}
	put := func(k, v string) {
		if v != "" {
			out[k] = v
		}
	}
	put("platform", strings.ToLower(dev.Platform))
	put("brand", dev.Brand)
	put("model", dev.Model)
	put("os_version", dev.OsVersion)
	put("app_version", dev.AppVersion)
	for k, v := range dev.Attrs {
		put("x."+k, v)
	}
	return out
}

// ObserveHook 插件钩子 → 风控业务事件（登录失败、注册、支付、退款、封号）。
// 钩子在这些业务的成功点上本来就会触发，挂在这里比在每个业务里再埋一次点可靠。
func (s *RiskService) ObserveHook(hookName string, data map[string]any, meta plugindomain.HookMetadata) {
	if s == nil || s.bus == nil {
		return
	}
	e := riskevents.Event{IP: meta.IP, UA: meta.UserAgent}
	if meta.AppID != nil {
		e.AppID = *meta.AppID
	} else if v, ok := data["appId"]; ok {
		e.AppID = int64(toFloat64(v))
	}
	if meta.UserID != nil {
		e.UserID = *meta.UserID
	} else if v, ok := data["userId"]; ok {
		e.UserID = int64(toFloat64(v))
	}
	if v, ok := data["account"].(string); ok {
		e.Account = v
	}
	if v, ok := data["ip"].(string); ok && e.IP == "" {
		e.IP = v
	}
	switch hookName {
	case HookAuthLoginFailed:
		e.Type, e.Scene = riskevents.TypeLoginFail, securitydomain.SceneLogin
	case HookUserRegistered:
		e.Type, e.Scene = riskevents.TypeRegister, securitydomain.SceneRegister
	case HookPaymentCompleted:
		e.Type, e.Scene = riskevents.TypePaymentPaid, securitydomain.ScenePayment
		e.Amount = toFloat64(data["amount"])
		e.Props = map[string]any{"orderId": data["orderId"]}
	case HookPaymentRefunded:
		e.Type, e.Scene = riskevents.TypePaymentRefund, securitydomain.ScenePayment
		e.Amount = toFloat64(data["amount"])
		e.Props = map[string]any{"orderId": data["orderId"], "refundNo": data["refundNo"]}
	case HookUserBanned:
		e.Type = riskevents.TypeAccountBan
		e.Props = map[string]any{"banType": data["banType"], "reason": data["reason"]}
	default:
		return
	}
	if e.AppID <= 0 {
		return
	}
	s.Report(context.Background(), e)
}

// GuardError 把评估结论翻译成业务错误（nil 为放行）
func GuardError(result *securitydomain.RiskEvalResult, opts ...RiskGuardOption) error {
	if result == nil {
		return nil
	}
	opt := RiskGuardOption{Code: errCodeRiskBlocked, Message: "当前请求存在风险，已被拦截"}
	if len(opts) > 0 {
		if opts[0].Code != 0 {
			opt.Code = opts[0].Code
		}
		if opts[0].Message != "" {
			opt.Message = opts[0].Message
		}
	}
	switch result.Action {
	case securitydomain.ActionBlock, securitydomain.ActionBan:
		return apperrors.New(opt.Code, http.StatusForbidden, opt.Message)
	case securitydomain.ActionCaptcha:
		return apperrors.New(errCodeRiskCaptchaRequired, http.StatusBadRequest, "当前环境存在风险，请先完成图形验证码")
	}
	return nil
}

// IsRiskRejection 错误是否为风控拒绝（调用方据此区分「风控拦了」与「业务失败」）
func IsRiskRejection(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && (appErr.Code == errCodeRiskBlocked || appErr.Code == errCodeRiskCaptchaRequired)
}

type riskCaptchaKey struct{}

// WithRiskCaptchaPassed 标记这次请求已经通过图形验证码（接入层校验通过后调用）
func WithRiskCaptchaPassed(ctx context.Context) context.Context {
	return context.WithValue(ctx, riskCaptchaKey{}, true)
}

func riskCaptchaPassed(ctx context.Context) bool {
	passed, _ := ctx.Value(riskCaptchaKey{}).(bool)
	return passed
}

// Report 发一条业务事件（异步安全：NATS 不可用时落本地待发队列，恢复后补发）
func (s *RiskService) Report(ctx context.Context, e riskevents.Event) {
	if s == nil || s.bus == nil {
		return
	}
	if e.ID == "" {
		e.ID = riskevents.NewID()
	}
	if e.TS == 0 {
		e.TS = time.Now().UnixMilli()
	}
	if e.IP != "" && e.IPSubnet == "" {
		e.IPSubnet = ipSubnet(e.IP)
	}
	if e.Email != "" && e.EmailDomain == "" {
		if at := strings.LastIndexByte(e.Email, '@'); at > 0 {
			e.EmailDomain = strings.ToLower(e.Email[at+1:])
		}
	}
	if err := s.bus.PublishEvent(context.WithoutCancel(ctx), e); err != nil {
		s.log.Warn("风控事件上报失败", zap.String("type", e.Type), zap.Error(err))
	}
}

// riskHook 业务服务接入风控的统一挂点：嵌进服务结构体，装配时 SetRiskService，
// 没接（测试、离线工具）时 guard 直接放行、report 什么都不做。
type riskHook struct {
	risk *RiskService
}

// SetRiskService 接入风控平台
func (h *riskHook) SetRiskService(risk *RiskService) { h.risk = risk }

func (h *riskHook) riskGuard(ctx context.Context, req securitydomain.RiskEvalRequest, opts ...RiskGuardOption) error {
	if h == nil || h.risk == nil {
		return nil
	}
	_, err := h.risk.Guard(ctx, req, opts...)
	return err
}

func (h *riskHook) riskReport(ctx context.Context, e riskevents.Event) {
	if h == nil || h.risk == nil {
		return
	}
	h.risk.Report(ctx, e)
}

// riskAdminPrefix risk-admin 在 etcd 里的注册前缀（hertz-contrib/registry/etcd 的键格式：前缀/服务名/地址）
const riskAdminPrefix = "hertz/registry-etcd/aegis.risk.admin/"

// AdminBaseURL 一个可用的 risk-admin 实例地址（10 秒缓存，多实例随机挑一个）
func (s *RiskService) AdminBaseURL(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("风控平台未接入")
	}
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	if time.Since(s.adminAt) < 10*time.Second && len(s.adminAddrs) > 0 {
		return "http://" + s.adminAddrs[rand.IntN(len(s.adminAddrs))], nil
	}
	if s.etcd != nil {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		resp, err := s.etcd.Get(cctx, riskAdminPrefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
		cancel()
		if err == nil {
			addrs := make([]string, 0, len(resp.Kvs))
			for _, kv := range resp.Kvs {
				if addr := strings.TrimPrefix(string(kv.Key), riskAdminPrefix); addr != "" {
					addrs = append(addrs, addr)
				}
			}
			if len(addrs) > 0 {
				s.adminAddrs, s.adminAt = addrs, time.Now()
				return "http://" + addrs[rand.IntN(len(addrs))], nil
			}
		}
	}
	if s.cfg.AdminURL != "" {
		return s.cfg.AdminURL, nil
	}
	return "", errors.New("没有可用的 risk-admin 实例")
}

// InternalToken 转发到风控服务时带的内部令牌
func (s *RiskService) InternalToken() string {
	if s == nil {
		return ""
	}
	return s.cfg.InternalToken
}

var riskAdminHTTP = &http.Client{Timeout: 5 * time.Second}

// AdminGet 主服务自己读风控平台的数据（报表、行为画像）：GET risk-admin /v1/<path>，解开 {code, data} 信封
func (s *RiskService) AdminGet(ctx context.Context, path string, query url.Values, out any) error {
	if !s.Enabled() {
		return errors.New("风控平台未接入")
	}
	base, err := s.AdminBaseURL(ctx)
	if err != nil {
		return err
	}
	u := base + "/v1/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Risk-Token", s.cfg.InternalToken)
	req.Header.Set("X-Risk-Operator", "aegis-server")
	resp, err := riskAdminHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || env.Code != 200 {
		return fmt.Errorf("risk-admin %s：%s", path, env.Message)
	}
	return json.Unmarshal(env.Data, out)
}

// UserMaxScore 用户近 days 天的最高风险分（5 分钟缓存；风控不可用时返回 0）
func (s *RiskService) UserMaxScore(ctx context.Context, appID, userID int64, days int) int {
	if !s.Enabled() || userID <= 0 {
		return 0
	}
	key := fmt.Sprintf("%srisk:maxscore:%d:%d:%d", s.prefix, appID, userID, days)
	if s.redis != nil {
		if v, err := s.redis.Get(ctx, key).Int(); err == nil {
			return v
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	var out struct {
		MaxScore int `json:"maxScore"`
	}
	if err := s.AdminGet(cctx, "users/score", url.Values{"app": {fmt.Sprint(appID)}, "user": {fmt.Sprint(userID)}, "days": {fmt.Sprint(days)}}, &out); err != nil {
		return 0
	}
	if s.redis != nil {
		_ = s.redis.Set(ctx, key, out.MaxScore, 5*time.Minute).Err()
	}
	return out.MaxScore
}
