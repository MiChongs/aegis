package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/shopspring/decimal"
)

// AI 额度的收费面：价格方案（时段、会员档位）、额度商店、额度批次、新人礼与经营统计。规格见 docs/ai-billing.md。

// mapAIGatewayCommerceError 额度商店与批次的哨兵错误 → 402xx（40232–40243）。
func mapAIGatewayCommerceError(err error) error {
	var unavailable *pgrepo.AIGatewayUnavailableError
	switch {
	case errors.As(err, &unavailable):
		if unavailable.Reason == aigateway.ReasonMembersOnly || unavailable.Reason == aigateway.ReasonTierRequired {
			return aiGatewayError(40243, http.StatusForbidden, aigateway.ReasonMessage(unavailable.Reason))
		}
		if unavailable.Reason == aigateway.ReasonStoreClosed {
			return aiGatewayError(40232, http.StatusNotFound, "额度包不存在或已下架")
		}
		return aiGatewayError(40235, http.StatusConflict, aigateway.ReasonMessage(unavailable.Reason))
	case errors.Is(err, pgrepo.ErrAIGatewayPackageNotFound):
		return aiGatewayError(40232, http.StatusNotFound, "额度包不存在或已下架")
	case errors.Is(err, pgrepo.ErrAIGatewayPackageInUse):
		return aiGatewayError(40234, http.StatusConflict, "额度包已有订单，不能删除，请改为下架")
	case errors.Is(err, pgrepo.ErrAIGatewayPriceChanged):
		return aiGatewayError(40236, http.StatusConflict, "价格已变化，请刷新后重试")
	case errors.Is(err, pgrepo.ErrAIGatewayWalletShort):
		return aiGatewayError(40237, http.StatusPaymentRequired, "余额不足，请先充值")
	case errors.Is(err, pgrepo.ErrAIGatewayOrderNotRefund):
		return aiGatewayError(40238, http.StatusConflict, "订单已退款，不能重复退款")
	case errors.Is(err, pgrepo.ErrAIGatewayOrderLotBusy):
		return aiGatewayError(40238, http.StatusConflict, "这笔额度正被调用占用，请稍后再退款")
	case errors.Is(err, pgrepo.ErrAIGatewayOrderNotFound):
		return aiGatewayError(40239, http.StatusNotFound, "订单不存在")
	case errors.Is(err, pgrepo.ErrAIGatewayWelcomeClaimed):
		return aiGatewayError(40240, http.StatusConflict, "新人礼未开放或已领取")
	case errors.Is(err, pgrepo.ErrAIGatewayLotNotFound):
		return aiGatewayError(40241, http.StatusNotFound, "额度批次不存在")
	case errors.Is(err, pgrepo.ErrAIGatewayLotUnavailable):
		return aiGatewayError(40241, http.StatusConflict, "额度批次已到期或已作废")
	case errors.Is(err, pgrepo.ErrAIGatewayRequestIDReused):
		return aiGatewayError(40242, http.StatusConflict, "请求编号已用于另一笔购买或发放")
	}
	return err
}

func validRequestID(id string) bool {
	return len(id) >= 8 && len(id) <= 64
}

// ── 设置里的会员档位 ──

// checkMemberTiers 会员档位优惠只能指向本应用的档位。
func (s *AIGatewayService) checkMemberTiers(ctx context.Context, appID int64, tiers []aigateway.MemberTier) error {
	if len(tiers) == 0 {
		return nil
	}
	known, err := s.vipTierNames(ctx, appID)
	if err != nil {
		return err
	}
	for _, tier := range tiers {
		if _, ok := known[tier.TierID]; !ok {
			return aiGatewayError(40219, http.StatusBadRequest, fmt.Sprintf("会员档位 %d 不存在", tier.TierID))
		}
	}
	return nil
}

func (s *AIGatewayService) vipTierNames(ctx context.Context, appID int64) (map[int64]string, error) {
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(tiers))
	for _, tier := range tiers {
		out[tier.ID] = tier.Name
	}
	return out, nil
}

// AIGatewayTierRef 档位目录项。
type AIGatewayTierRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// AIGatewayCatalogFull 控制台目录 + 本应用的会员档位与配色。
type AIGatewayCatalogFull struct {
	AIGatewayCatalog
	VipTiers     []AIGatewayTierRef `json:"vipTiers"`
	Tones        []string           `json:"tones"`
	PackageTones []string           `json:"packageTones"`
}

// CatalogFor 带应用档位的目录。
func (s *AIGatewayService) CatalogFor(ctx context.Context, appID int64) (*AIGatewayCatalogFull, error) {
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &AIGatewayCatalogFull{AIGatewayCatalog: s.Catalog(), VipTiers: make([]AIGatewayTierRef, 0, len(tiers)),
		Tones:        []string{aigateway.ToneNormal, aigateway.ToneOffPeak, aigateway.TonePeak, aigateway.TonePromo},
		PackageTones: aigateway.PackageTones}
	for _, tier := range tiers {
		out.VipTiers = append(out.VipTiers, AIGatewayTierRef{ID: tier.ID, Name: tier.Name})
	}
	return out, nil
}

// ── 价格方案 ──

// tierBenefits 各已配置档位的优惠（按优先级），末尾是通用会员（有任一优惠时）。
func tierBenefits(settings aigateway.Settings, names map[int64]string) []aigateway.MemberBenefit {
	out := make([]aigateway.MemberBenefit, 0, len(settings.MemberTiers)+1)
	for _, tier := range settings.MemberTiers {
		out = append(out, settings.TierMember(tier, names[tier.TierID]))
	}
	vip := settings.VipMember()
	one := decimal.NewFromInt(1)
	if !vip.Multiplier.Equal(one) || vip.Daily.Sign() > 0 || vip.Monthly.Sign() > 0 || vip.PackDiscount.LessThan(one) ||
		vip.PackBonusRate.Sign() > 0 {
		out = append(out, vip)
	}
	return out
}

// PricingPlan 用户端的价格方案：时段、我的倍率、各档位优惠对比、未来 7 天时间轴。
func (s *AIGatewayService) PricingPlan(ctx context.Context, session *authdomain.Session) (*aigateway.PricingPlan, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	facts, err := s.pg.AIGatewayAccountFacts(ctx, session.AppID, session.UserID, *settings, now)
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	names, err := s.vipTierNames(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	member := settings.Member(facts.Profile)
	windows := make([]aigateway.TimeWindow, 0, len(settings.TimePricing.Windows))
	for _, window := range settings.TimePricing.Windows {
		if window.Enabled {
			windows = append(windows, window)
		}
	}
	stacking := settings.TimePricing.Stacking
	if stacking == "" {
		stacking = aigateway.StackMultiply
	}
	return &aigateway.PricingPlan{
		Timezone: settings.Timezone, Now: now.In(settings.Location()), Enabled: settings.TimePricing.Enabled, Stacking: stacking,
		Base: settings.PriceMultiplier, Levels: settings.LevelMultipliers, FreeDaily: settings.FreeDaily, Windows: windows,
		Current:      settings.Pricing(member, facts.Profile.Level, "", "", now),
		NextChangeAt: settings.NextChange("", "", now),
		Timeline:     settings.Timeline("", "", now, 7),
		Member:       member,
		Tiers:        tierBenefits(*settings, names),
	}, nil
}

// AIGatewayPricingPreviewInput 控制台价格模拟的入参。
type AIGatewayPricingPreviewInput struct {
	Settings *aigateway.Settings `json:"settings"`
	Model    string              `json:"model"`
	Kind     string              `json:"kind"`
	TierID   int64               `json:"tierId"`
	IsVip    bool                `json:"isVip"`
	Level    int                 `json:"level"`
	At       *time.Time          `json:"at"`
}

// PricingPreview 价格模拟：用草稿设置（或已保存的）算某种用户在某一刻调用某个模型的倍率。
func (s *AIGatewayService) PricingPreview(ctx context.Context, appID int64, in AIGatewayPricingPreviewInput) (*aigateway.PricingPreview, error) {
	var settings aigateway.Settings
	if in.Settings != nil {
		normalized, err := aigateway.NormalizeSettings(*in.Settings)
		if err != nil {
			return nil, mapAIGatewayError(err)
		}
		settings = normalized
	} else {
		saved, err := s.pg.GetAIGatewaySettings(ctx, appID)
		if err != nil {
			return nil, err
		}
		settings = *saved
	}
	names, err := s.vipTierNames(ctx, appID)
	if err != nil {
		return nil, err
	}
	at := s.now()
	if in.At != nil {
		at = *in.At
	}
	level := max(in.Level, 1)
	profile := aigateway.Profile{IsVip: in.IsVip || in.TierID > 0, Level: level}
	if in.TierID > 0 {
		profile.TierIDs = []int64{in.TierID}
	}
	profile = profile.WithTierNames(names)
	member := settings.Member(profile)
	model := strings.TrimSpace(in.Model)
	kind := strings.TrimSpace(in.Kind)
	var price *aigateway.Price
	if model != "" {
		found, err := s.pg.GetAIGatewayModelByName(ctx, appID, model)
		if err == nil {
			kind = found.Kind
			scaled := found.Price
			price = &scaled
		} else if !errors.Is(err, pgrepo.ErrAIGatewayModelNotFound) {
			return nil, err
		}
	}
	breakdown := settings.Pricing(member, level, model, kind, at)
	if price != nil {
		scaled := price.Scaled(breakdown.Total)
		price = &scaled
	}
	return &aigateway.PricingPreview{
		Breakdown: breakdown, Member: member, Timeline: settings.Timeline(model, kind, at, 7),
		NextChangeAt: settings.NextChange(model, kind, at), Price: price,
	}, nil
}

// ── 用户端：额度商店 ──

// Store 用户端额度商店：在售额度包与我的报价。
func (s *AIGatewayService) Store(ctx context.Context, session *authdomain.Session) (*aigateway.PackageStore, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	facts, err := s.pg.AIGatewayAccountFacts(ctx, session.AppID, session.UserID, *settings, now)
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	member := settings.Member(facts.Profile)
	open := settings.Enabled && settings.Store.Enabled
	out := &aigateway.PackageStore{Enabled: open, Notice: settings.Store.Notice, Member: member,
		Wallet: decimal.New(facts.WalletCents, -2), Items: []aigateway.UserPackage{}}
	if !open {
		return out, nil
	}
	packages, err := s.pg.ListAIGatewayPackages(ctx, session.AppID, true, false)
	if err != nil {
		return nil, err
	}
	counts, err := s.pg.AIGatewayPurchaseCounts(ctx, session.AppID, session.UserID, pgrepo.MonthStart(now, settings.Location()))
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		// 已结束的限时包不再展示（未开始的照常展示倒计时）。
		if pkg.EndsAt != nil && !now.Before(*pkg.EndsAt) {
			continue
		}
		bought := counts[pkg.ID]
		ok, reason := aigateway.PackageAvailability(pkg, facts.Profile, open, bought[0], bought[1], now)
		quote := aigateway.QuotePackage(pkg, member)
		out.Items = append(out.Items, aigateway.BuildUserPackage(pkg, quote, ok, reason, bought[0], bought[1], facts.WalletCents))
	}
	return out, nil
}

// Purchase 用余额买额度包。
func (s *AIGatewayService) Purchase(ctx context.Context, session *authdomain.Session, packageID int64, requestID string,
	expectedPrice *decimal.Decimal, clientIP string) (*aigateway.PurchaseResult, error) {
	requestID = strings.TrimSpace(requestID)
	if !validRequestID(requestID) {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "请求编号 requestId 长度应为 8-64 位")
	}
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled || !settings.Store.Enabled {
		return nil, aiGatewayError(40233, http.StatusForbidden, "额度商店未开放")
	}
	result, err := s.pg.PurchaseAIGatewayPackage(ctx, aigateway.PurchaseInput{
		AppID: session.AppID, UserID: session.UserID, PackageID: packageID, RequestID: requestID,
		ExpectedPrice: expectedPrice, Settings: *settings, ClientIP: clientIP, Now: s.now(),
	})
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	return result, nil
}

// MyOrders 我的额度包订单。
func (s *AIGatewayService) MyOrders(ctx context.Context, session *authdomain.Session, page, limit int) (*aigateway.Page[aigateway.PackageOrder], error) {
	result, err := s.pg.ListAIGatewayOrders(ctx, aigateway.OrderQuery{AppID: session.AppID, UserID: session.UserID, Page: page, Limit: limit})
	if err != nil {
		return nil, err
	}
	for i := range result.Items {
		result.Items[i].UserID, result.Items[i].Account, result.Items[i].RefundedBy = 0, "", ""
		result.Items[i].LotAmount, result.Items[i].LotRemaining, result.Items[i].LotFrozen, result.Items[i].RefundEstimate = nil, nil, nil, nil
	}
	return result, nil
}

// MyLots 我的额度批次：scope=active 只要可用与暂扣中的，all 全部。
func (s *AIGatewayService) MyLots(ctx context.Context, session *authdomain.Session, scope string, page, limit int) (*aigateway.Page[aigateway.CreditLot], error) {
	if scope != "all" {
		scope = "active"
	}
	query := aigateway.LotQuery{AppID: session.AppID, UserID: session.UserID, Page: page, Limit: limit}
	if scope == "active" {
		query.Scope = "active"
	}
	result, err := s.pg.ListAIGatewayLots(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range result.Items {
		result.Items[i].UserID, result.Items[i].Account = 0, ""
	}
	return result, nil
}

// ClaimWelcome 领新人礼。
func (s *AIGatewayService) ClaimWelcome(ctx context.Context, session *authdomain.Session) (*aigateway.WelcomeResult, error) {
	settings, err := s.pg.GetAIGatewaySettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, aiGatewayError(40202, http.StatusForbidden, "AI 服务未开启")
	}
	result, err := s.pg.ClaimAIGatewayWelcome(ctx, session.AppID, session.UserID, settings.Welcome)
	return result, mapAIGatewayError(err)
}

// ── 管理端：额度包 ──

// ListPackages 全部额度包，带经营数据。
func (s *AIGatewayService) ListPackages(ctx context.Context, appID int64) ([]aigateway.Package, error) {
	return s.pg.ListAIGatewayPackages(ctx, appID, false, true)
}

func (s *AIGatewayService) normalizePackage(ctx context.Context, appID int64, in aigateway.PackageInput) (aigateway.Package, error) {
	pkg, err := aigateway.NormalizePackage(in)
	if err != nil {
		return pkg, mapAIGatewayError(err)
	}
	if len(pkg.TierIDs) > 0 {
		known, err := s.vipTierNames(ctx, appID)
		if err != nil {
			return pkg, err
		}
		for _, id := range pkg.TierIDs {
			if _, ok := known[id]; !ok {
				return pkg, aiGatewayError(40219, http.StatusBadRequest, fmt.Sprintf("会员档位 %d 不存在", id))
			}
		}
	}
	return pkg, nil
}

// CreatePackage 新建额度包。
func (s *AIGatewayService) CreatePackage(ctx context.Context, appID int64, in aigateway.PackageInput) (*aigateway.Package, error) {
	pkg, err := s.normalizePackage(ctx, appID, in)
	if err != nil {
		return nil, err
	}
	created, err := s.pg.CreateAIGatewayPackage(ctx, appID, pkg)
	return created, mapAIGatewayError(err)
}

// UpdatePackage 修改额度包（已买到的不受影响）。
func (s *AIGatewayService) UpdatePackage(ctx context.Context, appID, id int64, in aigateway.PackageInput) (*aigateway.Package, error) {
	pkg, err := s.normalizePackage(ctx, appID, in)
	if err != nil {
		return nil, err
	}
	updated, err := s.pg.UpdateAIGatewayPackage(ctx, appID, id, pkg)
	return updated, mapAIGatewayError(err)
}

// SetPackageEnabled 上架 / 下架。
func (s *AIGatewayService) SetPackageEnabled(ctx context.Context, appID, id int64, enabled bool) (*aigateway.Package, error) {
	pkg, err := s.pg.SetAIGatewayPackageEnabled(ctx, appID, id, enabled)
	return pkg, mapAIGatewayError(err)
}

// DeletePackage 删除没有订单的额度包。
func (s *AIGatewayService) DeletePackage(ctx context.Context, appID, id int64) error {
	return mapAIGatewayError(s.pg.DeleteAIGatewayPackage(ctx, appID, id))
}

// ── 管理端：订单与批次 ──

// ListOrders 订单。
func (s *AIGatewayService) ListOrders(ctx context.Context, query aigateway.OrderQuery) (*aigateway.Page[aigateway.PackageOrder], error) {
	if query.Status != "" && query.Status != aigateway.OrderPaid && query.Status != aigateway.OrderRefunded {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "订单状态只能是 paid 或 refunded")
	}
	return s.pg.ListAIGatewayOrders(ctx, query)
}

// RefundOrder 按剩余比例退额度包。
func (s *AIGatewayService) RefundOrder(ctx context.Context, in aigateway.OrderRefundInput) (*aigateway.PackageOrder, error) {
	in.OrderNo = strings.TrimSpace(in.OrderNo)
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) > 200 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "退款原因不能超过 200 个字")
	}
	order, err := s.pg.RefundAIGatewayOrder(ctx, in)
	return order, mapAIGatewayError(err)
}

// ListLots 批次。
func (s *AIGatewayService) ListLots(ctx context.Context, query aigateway.LotQuery) (*aigateway.Page[aigateway.CreditLot], error) {
	switch query.Status {
	case "", aigateway.LotActive, aigateway.LotExpired, aigateway.LotRevoked, aigateway.LotExhausted:
	default:
		return nil, aiGatewayError(40219, http.StatusBadRequest, "批次状态只能是 active、expired、revoked 或 exhausted")
	}
	switch query.Origin {
	case "", aigateway.LotPurchase, aigateway.LotWelcome, aigateway.LotAdmin, aigateway.LotGrant:
	default:
		return nil, aiGatewayError(40219, http.StatusBadRequest, "批次来源只能是 purchase、welcome、admin 或 grant")
	}
	return s.pg.ListAIGatewayLots(ctx, query)
}

// GrantLot 管理员发放限期额度。
func (s *AIGatewayService) GrantLot(ctx context.Context, in aigateway.LotGrantInput) (*aigateway.CreditLot, error) {
	if in.Amount.Sign() <= 0 || in.Amount.GreaterThan(decimal.NewFromInt(100_000_000)) || !in.Amount.Equal(in.Amount.Round(6)) {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "发放额度应在 0-100000000 点之间，最多 6 位小数")
	}
	if in.ValidityDays < 0 || in.ValidityDays > 3650 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "有效天数应在 0-3650 之间（0 为永久）")
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = "管理员发放"
	}
	if len([]rune(in.Title)) > 32 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "名称不能超过 32 个字")
	}
	in.RequestID = strings.TrimSpace(in.RequestID)
	if in.RequestID != "" && !validRequestID(in.RequestID) {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "请求编号长度应为 8-64 位")
	}
	in.Origin = aigateway.LotAdmin
	lot, err := s.pg.GrantAIGatewayLot(ctx, in)
	return lot, mapAIGatewayError(err)
}

// RevokeLot 作废批次剩余额度。
func (s *AIGatewayService) RevokeLot(ctx context.Context, appID, lotID int64, reason, operator string) (*aigateway.CreditLot, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 200 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "原因不能超过 200 个字")
	}
	lot, err := s.pg.RevokeAIGatewayLot(ctx, appID, lotID, reason, operator)
	return lot, mapAIGatewayError(err)
}

// CommerceStats 额度生意统计。
func (s *AIGatewayService) CommerceStats(ctx context.Context, appID int64, days int) (*aigateway.CommerceStats, error) {
	if days <= 0 {
		days = 30
	}
	days = min(days, 180)
	settings, err := s.pg.GetAIGatewaySettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.pg.AIGatewayCommerceStats(ctx, appID, days, settings.Timezone, s.now())
}
