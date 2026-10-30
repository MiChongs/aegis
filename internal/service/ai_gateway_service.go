package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	platformdomain "aegis/internal/domain/platform"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// AIGatewayService AI 中转网关：应用的终端用户凭自己的 API 密钥（或登录会话）调用 OpenAI 兼容接口，
// 按模型价格 × 倍率从免费额度、会员额度、赠送额度、积分、余额里依次扣费。
//
// 上游通道复用 AIProviderService 的链路（应用自有配置优先、回落平台共享），
// 计费的数值规则在 internal/domain/aigateway，事务在 ai_gateway_billing_repository.go。规格见 docs/ai-gateway.md。
type AIGatewayService struct {
	log  *zap.Logger
	pg   *pgrepo.Repository
	auth *AuthService
	ai   *AIProviderService
	now  func() time.Time
	// live 调用记录的实时推送（ai_gateway_live.go）。
	live *aiGatewayLiveHub
	// jobs 本实例上正在跑的异步任务（ai_gateway_jobs.go）。
	jobs *aiGatewayJobRegistry
	// catalog 公开模型目录的内存索引（ai_model_catalog.go）。
	catalog *aiModelCatalog
	// inflight 本实例上在途的对话中转，进程退出时取消以放回暂扣（ai_gateway_inflight.go）。
	inflight *aiGatewayInflight

	stop     chan struct{}
	stopOnce sync.Once
}

// NewAIGatewayService 构造网关服务。
func NewAIGatewayService(log *zap.Logger, pg *pgrepo.Repository, auth *AuthService, ai *AIProviderService) *AIGatewayService {
	s := &AIGatewayService{log: log, pg: pg, auth: auth, ai: ai, now: time.Now, live: newAIGatewayLiveHub(log),
		jobs: newAIGatewayJobRegistry(), catalog: newAIModelCatalog(), inflight: newAIGatewayInflight(),
		stop: make(chan struct{})}
	s.live.onCancel = s.jobs.cancel
	return s
}

// RelayBasePath OpenAI SDK 的 baseURL 应填的路径（相对站点根）。
const RelayBasePath = "/api/ai/v1"

const (
	// aiGatewayInflightDrainTimeout Stop 时等被取消的在途对话放回暂扣多久。
	aiGatewayInflightDrainTimeout = 5 * time.Second
	aiGatewaySweepInterval        = time.Minute
	aiGatewaySweepBatch           = 200
)

// ── 错误 ──

func aiGatewayError(code int, status int, message string) *apperrors.AppError {
	return apperrors.New(code, status, message)
}

// mapAIGatewayError 仓储层的哨兵错误 → 对外错误码（402xx 段）。
func mapAIGatewayError(err error) error {
	if err == nil {
		return nil
	}
	var insufficient *pgrepo.AIGatewayInsufficientError
	var idempotency *pgrepo.AIGatewayIdempotencyError
	var validation *aigateway.ValidationError
	switch {
	case errors.As(err, &insufficient):
		return aiGatewayError(40201, http.StatusPaymentRequired, fmt.Sprintf("余额不足：本次调用预计需要 %s 点，当前可用 %s 点",
			aigateway.MicroToPoints(insufficient.Need).Round(4).String(), aigateway.MicroToPoints(insufficient.Available).Round(4).String()))
	case errors.As(err, &idempotency):
		message := "相同的 Idempotency-Key 已有一次进行中或已成功的调用"
		if idempotency.RequestNo != "" {
			message += "（" + idempotency.RequestNo + "）"
		}
		return aiGatewayError(40213, http.StatusConflict, message)
	case errors.As(err, &validation):
		return aiGatewayError(40219, http.StatusBadRequest, validation.Message)
	case errors.Is(err, pgrepo.ErrAIGatewayAudience):
		return aiGatewayError(40205, http.StatusForbidden, "AI 服务仅对会员开放")
	case errors.Is(err, pgrepo.ErrAIGatewayVipOnlyModel):
		return aiGatewayError(40205, http.StatusForbidden, "该模型仅限会员使用")
	case errors.Is(err, pgrepo.ErrAIGatewayRateLimited):
		return aiGatewayError(40211, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
	case errors.Is(err, pgrepo.ErrAIGatewayConcurrency):
		return aiGatewayError(40212, http.StatusTooManyRequests, "同时进行的请求过多，请等前面的请求完成")
	case errors.Is(err, pgrepo.ErrAIGatewayDailyLimit):
		return aiGatewayError(40210, http.StatusPaymentRequired, "今日消费已达上限")
	case errors.Is(err, pgrepo.ErrAIGatewayKeySpendLimit):
		return aiGatewayError(40209, http.StatusPaymentRequired, "这把密钥的消费额度已用完")
	case errors.Is(err, pgrepo.ErrAIGatewayKeyUnusable):
		return aiGatewayError(40207, http.StatusUnauthorized, "API 密钥已停用或已过期")
	case errors.Is(err, pgrepo.ErrAIGatewayModelNotFound):
		return aiGatewayError(40203, http.StatusNotFound, "模型不存在或未开放")
	case errors.Is(err, pgrepo.ErrAIGatewayModelDuplicate):
		return aiGatewayError(40221, http.StatusConflict, "已有同名模型")
	case errors.Is(err, pgrepo.ErrAIGatewayKeyNotFound):
		return aiGatewayError(40216, http.StatusNotFound, "API 密钥不存在")
	case errors.Is(err, pgrepo.ErrAIGatewayKeyQuota):
		return aiGatewayError(40215, http.StatusConflict, "API 密钥数量已达上限")
	case errors.Is(err, pgrepo.ErrAIGatewayRequestNotFound):
		return aiGatewayError(40217, http.StatusNotFound, "调用记录不存在")
	case errors.Is(err, pgrepo.ErrAIGatewayNotRefundable):
		return aiGatewayError(40218, http.StatusConflict, "只有成功（含中途断开）且未退款的调用可以退款")
	case errors.Is(err, pgrepo.ErrAIGatewayCreditInsufficient):
		return aiGatewayError(40224, http.StatusConflict, "赠送额度不足，无法扣减")
	case errors.Is(err, pgrepo.ErrAIGatewayCreditRequestReused):
		return aiGatewayError(40225, http.StatusConflict, "请求编号已用于另一笔调整")
	case errors.Is(err, pgrepo.ErrAIGatewayJobNotFound):
		return aiGatewayError(40226, http.StatusNotFound, "任务不存在或结果已过期")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return err
}

// ── 后台回收 ──

// Start 起后台循环：每分钟把超时未结算的暂扣放回。多实例同时跑也安全：
// 放回在锁住调用记录后重查状态，已经被结算或被别的实例放回的直接跳过。
func (s *AIGatewayService) Start(ctx context.Context) {
	s.startCatalogLoop(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("ai gateway sweep loop panic", zap.Any("panic", r))
			}
		}()
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-timer.C:
				if released, err := s.SweepExpiredHolds(ctx); err != nil {
					s.log.Warn("回收超时的 AI 暂扣失败", zap.Error(err))
				} else if released > 0 {
					s.log.Warn("超时的 AI 暂扣已放回", zap.Int("count", released))
				}
				if _, interrupted, err := s.pg.SweepAIGatewayJobs(ctx, s.now()); err != nil {
					s.log.Warn("清理 AI 异步任务失败", zap.Error(err))
				} else if interrupted > 0 {
					s.log.Warn("中断的 AI 异步任务已判为失败", zap.Int64("count", interrupted))
				}
				timer.Reset(aiGatewaySweepInterval)
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *AIGatewayService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
		s.drainInFlight(aiGatewayInflightDrainTimeout)
		s.jobs.drain(aiGatewayJobDrainTimeout)
		s.live.close()
	})
}

// SweepExpiredHolds 放回已过期的暂扣，返回放回的条数。
func (s *AIGatewayService) SweepExpiredHolds(ctx context.Context) (int, error) {
	numbers, err := s.pg.ListExpiredAIGatewayHolds(ctx, s.now(), aiGatewaySweepBatch)
	if err != nil {
		return 0, err
	}
	released := 0
	for _, no := range numbers {
		record, err := s.pg.ReleaseAIGatewayRequest(ctx, aigateway.ReleaseInput{
			RequestNo: no, Status: aigateway.StatusExpired, ErrorCode: "hold_expired",
			ErrorMessage: "调用超时未结算，暂扣已自动放回，不扣费",
		})
		if err != nil {
			s.log.Warn("放回超时暂扣失败", zap.String("requestNo", no), zap.Error(err))
			continue
		}
		if record.Status == aigateway.StatusExpired {
			released++
			s.publishFinished(record)
		}
	}
	return released, nil
}

// ── 管理端：目录与设置 ──

// AIGatewayCatalog 控制台表单用的静态目录。
type AIGatewayCatalog struct {
	Sources  []aigateway.SourceMeta `json:"sources"`
	Kinds    []aigateway.KindMeta   `json:"kinds"`
	Defaults aigateway.Settings     `json:"defaults"`
	BaseURL  string                 `json:"baseUrl"`
	// KeyPrefix 用户密钥的前缀（控制台与 SDK 用它区分密钥与登录令牌）。
	KeyPrefix string `json:"keyPrefix"`
}

// Catalog 目录。
func (s *AIGatewayService) Catalog() AIGatewayCatalog {
	return AIGatewayCatalog{Sources: aigateway.Sources(), Kinds: aigateway.Kinds(), Defaults: aigateway.DefaultSettings(),
		BaseURL: RelayBasePath, KeyPrefix: aigateway.KeyPrefix}
}

// GetSettings 应用的网关设置。
func (s *AIGatewayService) GetSettings(ctx context.Context, appID int64) (*aigateway.Settings, error) {
	return s.pg.GetAIGatewaySettings(ctx, appID)
}

// SaveSettings 保存设置。
func (s *AIGatewayService) SaveSettings(ctx context.Context, appID int64, in aigateway.Settings, operator string) (*aigateway.Settings, error) {
	normalized, err := aigateway.NormalizeSettings(in)
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	return s.pg.SaveAIGatewaySettings(ctx, appID, normalized, operator)
}

// ── 管理端：模型 ──

// ListModels 应用的全部模型。
func (s *AIGatewayService) ListModels(ctx context.Context, appID int64) ([]aigateway.Model, error) {
	return s.pg.ListAIGatewayModels(ctx, appID, false)
}

func (s *AIGatewayService) normalizeModel(ctx context.Context, appID int64, in aigateway.ModelInput) (aigateway.Model, error) {
	model, err := aigateway.NormalizeModel(in)
	if err != nil {
		return model, mapAIGatewayError(err)
	}
	if model.ConfigID > 0 {
		if err := s.ai.ChannelExists(ctx, appID, model.ConfigID); err != nil {
			return model, aiGatewayError(40220, http.StatusBadRequest, "指定的 AI 通道不存在、已停用或不对本应用共享")
		}
	}
	return model, nil
}

// CreateModel 新建模型。
func (s *AIGatewayService) CreateModel(ctx context.Context, appID int64, in aigateway.ModelInput) (*aigateway.Model, error) {
	model, err := s.normalizeModel(ctx, appID, in)
	if err != nil {
		return nil, err
	}
	created, err := s.pg.CreateAIGatewayModel(ctx, appID, model)
	return created, mapAIGatewayError(err)
}

// UpdateModel 修改模型。进行中的调用按暂扣时的价格结算。
func (s *AIGatewayService) UpdateModel(ctx context.Context, appID, modelID int64, in aigateway.ModelInput) (*aigateway.Model, error) {
	// 没传能力声明的（旧版控制台、脚本）沿用原来的，不把已配好的能力冲成默认值。
	if in.Capabilities == nil {
		if current, err := s.pg.GetAIGatewayModel(ctx, appID, modelID); err == nil {
			capabilities := current.Capabilities
			in.Capabilities = &capabilities
		}
	}
	model, err := s.normalizeModel(ctx, appID, in)
	if err != nil {
		return nil, err
	}
	updated, err := s.pg.UpdateAIGatewayModel(ctx, appID, modelID, model)
	return updated, mapAIGatewayError(err)
}

// DeleteModel 删除模型。
func (s *AIGatewayService) DeleteModel(ctx context.Context, appID, modelID int64) error {
	return mapAIGatewayError(s.pg.DeleteAIGatewayModel(ctx, appID, modelID))
}

// ── 管理端：调用记录、密钥、统计、额度 ──

// ListRequests 调用记录。
func (s *AIGatewayService) ListRequests(ctx context.Context, query aigateway.RequestQuery) (*aigateway.Page[aigateway.Request], error) {
	page, err := s.pg.ListAIGatewayRequests(ctx, query)
	return page, mapAIGatewayError(err)
}

// GetRequest 一条调用记录，带追踪详情（每一次上游尝试、请求与响应摘要）。
func (s *AIGatewayService) GetRequest(ctx context.Context, appID int64, requestNo string) (*aigateway.Request, error) {
	record, err := s.pg.GetAIGatewayRequest(ctx, appID, 0, strings.TrimSpace(requestNo))
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	trace, err := s.pg.GetAIGatewayTrace(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	record.Trace = trace
	return record, nil
}

// LiveSummary 最近 minutes 分钟的调用概况（5–1440）。
func (s *AIGatewayService) LiveSummary(ctx context.Context, appID int64, minutes int) (*aigateway.LiveSummary, error) {
	minutes = min(max(minutes, 5), 1440)
	return s.pg.AIGatewayLiveSummary(ctx, appID, minutes, s.now())
}

// RefundRequest 管理员整笔退款。
func (s *AIGatewayService) RefundRequest(ctx context.Context, in aigateway.RefundInput) (*aigateway.Request, error) {
	in.RequestNo = strings.TrimSpace(in.RequestNo)
	reason := strings.TrimSpace(in.Reason)
	if len([]rune(reason)) > 200 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "退款原因不能超过 200 个字")
	}
	in.Reason = reason
	record, err := s.pg.RefundAIGatewayRequest(ctx, in)
	if err == nil {
		s.publishFinished(record)
	}
	return record, mapAIGatewayError(err)
}

// ListKeys 管理端密钥列表。
func (s *AIGatewayService) ListKeys(ctx context.Context, appID, userID int64, status string, page, limit int) (*aigateway.Page[aigateway.Key], error) {
	if status != "" && status != aigateway.KeyActive && status != aigateway.KeyDisabled && status != aigateway.KeyRevoked {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "状态只能是 active、disabled 或 revoked")
	}
	return s.pg.ListAIGatewayKeys(ctx, appID, userID, status, page, limit)
}

// SetKeyStatus 管理端启用 / 停用密钥。
func (s *AIGatewayService) SetKeyStatus(ctx context.Context, appID, keyID int64, status string) (*aigateway.Key, error) {
	if status != aigateway.KeyActive && status != aigateway.KeyDisabled {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "状态只能是 active 或 disabled")
	}
	key, err := s.pg.SetAIGatewayKeyStatus(ctx, appID, keyID, status)
	return key, mapAIGatewayError(err)
}

// Stats 应用统计。
func (s *AIGatewayService) Stats(ctx context.Context, appID int64, days int, timezone string) (*aigateway.Stats, error) {
	if days <= 0 {
		days = 30
	}
	if days > 180 {
		days = 180
	}
	if strings.TrimSpace(timezone) == "" {
		settings, err := s.pg.GetAIGatewaySettings(ctx, appID)
		if err != nil {
			return nil, err
		}
		timezone = settings.Timezone
	}
	return s.pg.AIGatewayStats(ctx, appID, days, timezone, s.now())
}

// UserOverview 管理端查看某个用户的网关账户（与用户自己看到的总览同一份）。
func (s *AIGatewayService) UserOverview(ctx context.Context, appID, userID int64) (*aigateway.Overview, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.overview(ctx, appID, userID, *settings)
}

// AdjustCredit 管理员增减赠送额度。
func (s *AIGatewayService) AdjustCredit(ctx context.Context, in aigateway.CreditAdjustInput) (*aigateway.CreditAdjustResult, error) {
	if in.Amount.IsZero() {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "调整数量不能为 0")
	}
	if in.Amount.Abs().GreaterThan(decimal.NewFromInt(100_000_000)) {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "单次调整不能超过 100,000,000 点")
	}
	if !in.Amount.Equal(in.Amount.Round(6)) {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "调整数量最多保留 6 位小数")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) > 200 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "调整原因不能超过 200 个字")
	}
	if in.Reason == "" {
		in.Reason = "管理员调整"
	}
	requestID := strings.TrimSpace(in.RequestID)
	if requestID != "" && (len(requestID) < 8 || len(requestID) > 64) {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "请求编号长度应为 8-64 位")
	}
	in.RequestID = requestID
	result, err := s.pg.AdjustAIGatewayCredit(ctx, in)
	return result, mapAIGatewayError(err)
}

// ListCredits 赠送额度流水。
func (s *AIGatewayService) ListCredits(ctx context.Context, appID, userID int64, page, limit int) (*aigateway.Page[aigateway.CreditEntry], error) {
	return s.pg.ListAIGatewayCredits(ctx, appID, userID, page, limit)
}

// ── 用户端 ──

// Overview 用户自己的网关总览：能用多少、从哪扣、今天花了多少。
func (s *AIGatewayService) Overview(ctx context.Context, session *authdomain.Session) (*aigateway.Overview, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	return s.overview(ctx, session.AppID, session.UserID, *settings)
}

func (s *AIGatewayService) overview(ctx context.Context, appID, userID int64, settings aigateway.Settings) (*aigateway.Overview, error) {
	now := s.now()
	facts, err := s.pg.AIGatewayAccountFacts(ctx, appID, userID, settings, now)
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	todaySpent, err := s.pg.AIGatewayTodaySpent(ctx, appID, userID, settings, now)
	if err != nil {
		return nil, err
	}
	out := buildAIGatewayOverview(settings, *facts, todaySpent)
	return &out, nil
}

// buildAIGatewayOverview 由事实算总览（纯函数，可用量的口径与暂扣一致）。
func buildAIGatewayOverview(settings aigateway.Settings, facts aigateway.AccountFacts, todaySpent int64) aigateway.Overview {
	profile := facts.Profile
	out := aigateway.Overview{
		Enabled:           settings.Enabled,
		AllowSessionToken: settings.AllowSessionToken,
		Audience:          settings.Audience,
		Eligible:          settings.Audience != aigateway.AudienceVip || profile.IsVip,
		Sources:           make([]aigateway.SourceMeta, 0, len(settings.Sources)),
		Profile:           profile,
		Factor:            settings.Factor(profile),
		Quotas:            []aigateway.QuotaView{},
		Credit:            aigateway.MicroToPoints(facts.Account.Credit),
		CreditFrozen:      aigateway.MicroToPoints(facts.Account.CreditFrozen),
		Integral:          facts.Integral,
		Wallet:            decimal.New(facts.WalletCents, -2),
		WalletRate:        settings.WalletRate,
		DailySpendLimit:   settings.DailySpendLimit,
		TodaySpent:        aigateway.MicroToPoints(todaySpent),
		TotalSpent:        aigateway.MicroToPoints(facts.Account.TotalSpent),
		TotalRequests:     facts.Account.TotalRequests,
		RPM:               settings.RPM,
		MaxConcurrent:     settings.MaxConcurrent,
		MaxKeysPerUser:    settings.MaxKeysPerUser,
		BaseURL:           RelayBasePath,
	}
	var available int64
	add := func(micro int64) {
		if micro > 0 && available <= (1<<62)-micro {
			available += micro
		}
	}
	for _, source := range settings.Sources {
		meta, ok := aigateway.FindSource(source)
		if !ok {
			continue
		}
		out.Sources = append(out.Sources, meta)
		switch source {
		case aigateway.SourceFreeDaily, aigateway.SourceVipDaily, aigateway.SourceVipMonthly:
			limit := settings.QuotaMicro(source)
			if limit <= 0 {
				continue
			}
			usage := facts.QuotaUsage[source]
			remaining := max(limit-usage.Used-usage.Reserved, 0)
			eligible := !meta.VipOnly || profile.IsVip
			out.Quotas = append(out.Quotas, aigateway.QuotaView{
				Source: source, Label: meta.Label, Period: usage.Period,
				Limit: aigateway.MicroToPoints(limit), Used: aigateway.MicroToPoints(usage.Used),
				Reserved: aigateway.MicroToPoints(usage.Reserved), Remaining: aigateway.MicroToPoints(remaining),
				Eligible: eligible,
			})
			if eligible {
				add(remaining)
			}
		case aigateway.SourceCredit:
			add(facts.Account.Credit)
		case aigateway.SourceIntegral:
			if facts.Integral > 0 && facts.Integral < (1<<62)/aigateway.MicroPerPoint {
				add(facts.Integral * aigateway.MicroPerPoint)
			}
		case aigateway.SourceWallet:
			add(aigateway.WalletAvailableMicro(facts.WalletCents, settings.WalletRate))
		}
	}
	if limit := settings.QuotaMicro(aigateway.SpendDaily); limit > 0 {
		usage := facts.QuotaUsage[aigateway.SpendDaily]
		available = min(available, max(limit-usage.Used-usage.Reserved, 0))
	}
	out.Available = aigateway.MicroToPoints(available)
	return out
}

// Models 用户可见的模型与有效单价（已乘上该用户的倍率）。
func (s *AIGatewayService) Models(ctx context.Context, session *authdomain.Session) ([]aigateway.UserModel, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.AIGatewayAccountFacts(ctx, session.AppID, session.UserID, *settings, s.now())
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	models, err := s.pg.ListAIGatewayModels(ctx, session.AppID, true)
	if err != nil {
		return nil, err
	}
	factor := settings.Factor(facts.Profile)
	out := make([]aigateway.UserModel, 0, len(models))
	for _, model := range models {
		item := aigateway.UserModel{
			Model: model.Model, Kind: model.Kind, DisplayName: model.DisplayName, Description: model.Description,
			VipOnly: model.VipOnly, Available: !model.VipOnly || facts.Profile.IsVip,
			Price: model.Price.Scaled(factor), MaxOutputTokens: model.MaxOutputTokens,
		}
		if model.Kind == aigateway.KindChat {
			capabilities := model.Capabilities
			item.Capabilities = &capabilities
		}
		out = append(out, item)
	}
	return out, nil
}

// ListMyKeys 我的密钥。
func (s *AIGatewayService) ListMyKeys(ctx context.Context, session *authdomain.Session) ([]aigateway.Key, error) {
	return s.pg.ListAIGatewayUserKeys(ctx, session.AppID, session.UserID)
}

// CreateKey 新建密钥：明文只在这次返回。
func (s *AIGatewayService) CreateKey(ctx context.Context, session *authdomain.Session, in aigateway.KeyInput) (*aigateway.CreatedKey, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, aiGatewayError(40202, http.StatusForbidden, "AI 服务未开启")
	}
	if settings.MaxKeysPerUser <= 0 {
		return nil, aiGatewayError(40215, http.StatusConflict, "本应用不允许创建 API 密钥，请在应用内直接使用")
	}
	normalized, err := aigateway.NormalizeKeyInput(in, true, s.now())
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	secret, hash, hint, err := aigateway.GenerateKey()
	if err != nil {
		return nil, err
	}
	name := "API 密钥"
	if normalized.Name != nil && *normalized.Name != "" {
		name = *normalized.Name
	}
	models := []string{}
	if normalized.Models != nil {
		models = *normalized.Models
	}
	var spendLimit int64
	if normalized.SpendLimit != nil {
		spendLimit = aigateway.PointsToMicroFloor(*normalized.SpendLimit)
	}
	key, err := s.pg.CreateAIGatewayKey(ctx, aigateway.Key{
		AppID: session.AppID, UserID: session.UserID, Name: name, Hint: hint, Models: models,
		SpendLimitMicro: spendLimit, ExpiresAt: normalized.ExpiresAt,
	}, hash, settings.MaxKeysPerUser)
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	return &aigateway.CreatedKey{Key: *key, Secret: secret}, nil
}

// UpdateKey 修改我的密钥。
func (s *AIGatewayService) UpdateKey(ctx context.Context, session *authdomain.Session, keyID int64, in aigateway.KeyInput) (*aigateway.Key, error) {
	normalized, err := aigateway.NormalizeKeyInput(in, false, s.now())
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	key, err := s.pg.UpdateAIGatewayKey(ctx, session.AppID, session.UserID, keyID, normalized)
	return key, mapAIGatewayError(err)
}

// DeleteKey 删除我的密钥。
func (s *AIGatewayService) DeleteKey(ctx context.Context, session *authdomain.Session, keyID int64) error {
	return mapAIGatewayError(s.pg.RevokeAIGatewayKey(ctx, session.AppID, session.UserID, keyID))
}

// ListMyRequests 我的调用记录。
func (s *AIGatewayService) ListMyRequests(ctx context.Context, session *authdomain.Session, query aigateway.RequestQuery) (*aigateway.Page[aigateway.Request], error) {
	query.AppID = session.AppID
	query.UserID = session.UserID
	page, err := s.pg.ListAIGatewayRequests(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		redactAIGatewayRequestForUser(&page.Items[i])
	}
	return page, nil
}

// GetMyRequest 我的一条调用记录。
func (s *AIGatewayService) GetMyRequest(ctx context.Context, session *authdomain.Session, requestNo string) (*aigateway.Request, error) {
	record, err := s.pg.GetAIGatewayRequest(ctx, session.AppID, session.UserID, strings.TrimSpace(requestNo))
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	redactAIGatewayRequestForUser(record)
	return record, nil
}

// redactAIGatewayRequestForUser 用户端不该看到的：上游通道、上游模型名、账号。
func redactAIGatewayRequestForUser(record *aigateway.Request) {
	record.ConfigID = 0
	record.Provider = ""
	record.UpstreamModel = ""
	record.Account = ""
	record.RefundedBy = ""
	record.ConfigName = ""
	record.UpstreamRequestID = ""
	record.Trace = nil
	record.Live = nil
}

// ListMyCredits 我的赠送额度流水。
func (s *AIGatewayService) ListMyCredits(ctx context.Context, session *authdomain.Session, page, limit int) (*aigateway.Page[aigateway.CreditEntry], error) {
	result, err := s.pg.ListAIGatewayCredits(ctx, session.AppID, session.UserID, page, limit)
	if err != nil {
		return nil, err
	}
	for i := range result.Items {
		result.Items[i].Operator = ""
	}
	return result, nil
}

// ── 中转鉴权 ──

// AIGatewayPrincipal 一次中转调用的调用者。
type AIGatewayPrincipal struct {
	AppID     int64
	UserID    int64
	AuthKind  string
	Key       *aigateway.Key
	Settings  aigateway.Settings
	ClientIP  string
	UserAgent string
}

// Authenticate 凭令牌认出调用者：sk-ag- 前缀是用户密钥，其余按登录会话的访问令牌处理。
//
// 密钥路径不经过会话校验，所以应用状态、平台治理与账号封禁都在这里补上 ——
// 少一项，被封的账号就能拿着之前领的密钥一直用下去。
func (s *AIGatewayService) Authenticate(ctx context.Context, token, clientIP, userAgent string) (*AIGatewayPrincipal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, aiGatewayError(40206, http.StatusUnauthorized,
			"缺少凭据：请在 Authorization: Bearer 里携带 API 密钥或登录令牌")
	}
	principal := &AIGatewayPrincipal{ClientIP: clientIP, UserAgent: userAgent}
	if aigateway.LooksLikeKey(token) {
		key, err := s.pg.FindAIGatewayKeyByHash(ctx, aigateway.HashKey(token))
		if err != nil {
			if errors.Is(err, pgrepo.ErrAIGatewayKeyNotFound) {
				return nil, aiGatewayError(40206, http.StatusUnauthorized, "API 密钥无效")
			}
			return nil, err
		}
		if !key.Usable(s.now()) {
			return nil, aiGatewayError(40207, http.StatusUnauthorized, "API 密钥已停用或已过期")
		}
		principal.AppID, principal.UserID, principal.Key, principal.AuthKind = key.AppID, key.UserID, key, aigateway.AuthKey
		if s.ai != nil && s.ai.governance != nil {
			if err := s.ai.governance.EnsureCapability(key.AppID, platformdomain.CapabilityAPI); err != nil {
				return nil, err
			}
		}
	} else {
		if s.auth == nil {
			return nil, aiGatewayError(40206, http.StatusUnauthorized, "API 密钥无效")
		}
		session, err := s.auth.ValidateAccessToken(ctx, token)
		if err != nil {
			var appErr *apperrors.AppError
			if errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusUnauthorized {
				return nil, aiGatewayError(40206, http.StatusUnauthorized, "API 密钥或登录令牌无效")
			}
			return nil, err
		}
		principal.AppID, principal.UserID, principal.AuthKind = session.AppID, session.UserID, aigateway.AuthSession
	}

	app, err := s.pg.GetAppByID(ctx, principal.AppID)
	if err != nil {
		return nil, err
	}
	if app == nil || !app.Status {
		return nil, apperrors.New(40470, http.StatusNotFound, "应用不存在或已停用")
	}
	settings, err := s.pg.GetAIGatewaySettings(ctx, principal.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, aiGatewayError(40202, http.StatusForbidden, "AI 服务未开启")
	}
	if principal.AuthKind == aigateway.AuthSession && !settings.AllowSessionToken {
		return nil, aiGatewayError(40214, http.StatusUnauthorized, "本应用要求使用 API 密钥调用，不接受登录令牌")
	}
	principal.Settings = *settings

	user, err := s.pg.GetUserByID(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != principal.AppID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if err := enforceAccountBan(ctx, s.pg, s.log, user, userdomain.AccountBanCheckpointSession); err != nil {
		return nil, err
	}
	return principal, nil
}

// RelayModels 调用者能调的模型（/v1/models）。
func (s *AIGatewayService) RelayModels(ctx context.Context, principal *AIGatewayPrincipal) ([]aigateway.Model, error) {
	models, err := s.pg.ListAIGatewayModels(ctx, principal.AppID, true)
	if err != nil {
		return nil, err
	}
	out := make([]aigateway.Model, 0, len(models))
	for _, model := range models {
		if principal.Key != nil && !principal.Key.AllowsModel(model.Model) {
			continue
		}
		out = append(out, model)
	}
	return out, nil
}

// resolveModel 按对外名找到模型，并核对接口种类与密钥白名单。
func (s *AIGatewayService) resolveModel(ctx context.Context, principal *AIGatewayPrincipal, name, endpoint string) (*aigateway.Model, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, aiGatewayError(40223, http.StatusBadRequest, "缺少 model 字段")
	}
	model, err := s.pg.GetAIGatewayModelByName(ctx, principal.AppID, name)
	if err != nil {
		if errors.Is(err, pgrepo.ErrAIGatewayModelNotFound) {
			return nil, aiGatewayError(40203, http.StatusNotFound, "模型「"+name+"」不存在或未开放")
		}
		return nil, err
	}
	if !model.Enabled {
		return nil, aiGatewayError(40203, http.StatusNotFound, "模型「"+name+"」不存在或未开放")
	}
	if kind := aigateway.EndpointKind(endpoint); kind != model.Kind {
		return nil, aiGatewayError(40204, http.StatusBadRequest, "模型「"+name+"」不支持这个接口")
	}
	if principal.Key != nil && !principal.Key.AllowsModel(model.Model) {
		return nil, aiGatewayError(40208, http.StatusForbidden, "这把密钥不允许使用模型「"+name+"」")
	}
	return model, nil
}
