package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"aegis/internal/cluster"
	"aegis/internal/domain/coupon"
	notificationdomain "aegis/internal/domain/notification"
	paymentdomain "aegis/internal/domain/payment"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// CouponService 优惠系统（docs/coupons.md）：模板、用户券、领券中心、优惠活动、下单试算与订单上的优惠结算。
//
// 定价与资格判定全部是 domain/coupon 的纯函数；这里负责把事实（用户、券、活动、参与次数）取齐，
// 以及把结论落到仓储层的事务里。支付网关下单时调 QuoteForOrder，付款后调 SettleOrder。
type CouponService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	notifier   notificationDeliverer
	userEvents UserEventPublisher

	releaseTask clusterTask // 没付款的订单过期后释放券与名额
	expireTask  clusterTask // 过期券标记、已付款未结算的补偿
	closeOnce   sync.Once
	closeCh     chan struct{}
}

// 错误码 42310–42329（docs/coupons.md#错误码）。
const (
	errCodeCouponTemplateNotFound = 42310
	errCodePromotionNotFound      = 42311
	errCodeUserCouponNotFound     = 42312
	errCodeCouponUnavailable      = 42313
	errCodeCouponSoldOut          = 42314
	errCodeCouponClaimLimit       = 42315
	errCodeCouponAudience         = 42316
	errCodeCouponNotApplicable    = 42317
	errCodeCouponPriceChanged     = 42318
	errCodeCouponQuoteChanged     = 42319
	errCodeCouponTemplateInvalid  = 42320
	errCodePromotionInvalid       = 42321
	errCodeCouponRuleLocked       = 42322
	errCodeCouponInUse            = 42324
	errCodeCouponStatus           = 42325
)

// 订单过期多久之后才把券还回去：给渠道晚到的通知留一点时间，避免券刚还回去、上一单又付上了。
const couponReleaseGrace = 15 * time.Minute

// CouponEventChanged 用户的券变了（领到、用掉、返还、收回），客户端刷新券包。
const CouponEventChanged = "coupon.changed"

func NewCouponService(log *zap.Logger, pg *pgrepo.Repository) *CouponService {
	if log == nil {
		log = zap.NewNop()
	}
	return &CouponService{log: log, pg: pg, closeCh: make(chan struct{})}
}

// SetNotifier 接上站内信：收到券、返券时通知用户。
func (s *CouponService) SetNotifier(n notificationDeliverer) { s.notifier = n }

// SetEventPublisher 接上实时推送（coupon.changed）。
func (s *CouponService) SetEventPublisher(p UserEventPublisher) { s.userEvents = p }

// SetCluster 多副本下后台任务只由一个实例执行。
func (s *CouponService) SetCluster(c *cluster.Cluster) {
	s.releaseTask.bind(c, "coupon.release")
	s.expireTask.bind(c, "coupon.expire")
}

// Start 启动后台循环（API 进程）：每分钟释放过期订单占着的券与名额、补结算已付款的订单；每 10 分钟标记过期券。
func (s *CouponService) Start(ctx context.Context) {
	go func() {
		minute := time.NewTicker(time.Minute)
		defer minute.Stop()
		tenMinutes := time.NewTicker(10 * time.Minute)
		defer tenMinutes.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.closeCh:
				return
			case <-minute.C:
				if s.releaseTask.owned() {
					s.releaseExpired()
					s.retryUnsettled()
				}
			case <-tenMinutes.C:
				if s.expireTask.owned() {
					s.expireCoupons()
				}
			}
		}
	}()
}

// Close 停止后台循环。
func (s *CouponService) Close(context.Context) { s.closeOnce.Do(func() { close(s.closeCh) }) }

func (s *CouponService) releaseExpired() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := s.pg.ReleaseOrderPromotions(ctx, couponReleaseGrace, 200)
	if err != nil {
		s.log.Warn("release order promotions failed", zap.Error(err))
	}
	if n > 0 {
		s.log.Info("released order promotions", zap.Int("count", n))
	}
}

func (s *CouponService) retryUnsettled() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids, err := s.pg.ListUnsettledOrderPromotions(ctx, 100)
	if err != nil {
		s.log.Warn("list unsettled order promotions failed", zap.Error(err))
		return
	}
	for _, id := range ids {
		if _, err := s.SettleOrder(ctx, id); err != nil {
			s.log.Warn("settle order promotion failed", zap.Int64("orderId", id), zap.Error(err))
		}
	}
}

func (s *CouponService) expireCoupons() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.pg.ExpireUserCoupons(ctx, 5000); err != nil {
		s.log.Warn("expire user coupons failed", zap.Error(err))
	}
}

// ── 错误翻译 ──

func (s *CouponService) translate(err error) error {
	var unavailable *pgrepo.CouponUnavailableError
	var changed *pgrepo.CouponQuoteChangedError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &unavailable):
		code := errCodeCouponUnavailable
		status := http.StatusUnprocessableEntity
		switch unavailable.Code {
		case coupon.ReasonSoldOut:
			code, status = errCodeCouponSoldOut, http.StatusConflict
		case coupon.ReasonLimit:
			code, status = errCodeCouponClaimLimit, http.StatusConflict
		case coupon.ReasonAudience:
			code, status = errCodeCouponAudience, http.StatusForbidden
		}
		return apperrors.New(code, status, unavailable.Reason).WithData(map[string]any{"reasonCode": unavailable.Code})
	case errors.As(err, &changed):
		return apperrors.New(errCodeCouponQuoteChanged, http.StatusConflict, changed.Reason)
	case errors.Is(err, pgrepo.ErrCouponTemplateNotFound):
		return apperrors.New(errCodeCouponTemplateNotFound, http.StatusNotFound, "优惠券不存在")
	case errors.Is(err, pgrepo.ErrCouponPromotionNotFound):
		return apperrors.New(errCodePromotionNotFound, http.StatusNotFound, "优惠活动不存在")
	case errors.Is(err, pgrepo.ErrUserCouponNotFound):
		return apperrors.New(errCodeUserCouponNotFound, http.StatusNotFound, "这张券不存在")
	case errors.Is(err, pgrepo.ErrCouponRuleLocked):
		return apperrors.New(errCodeCouponRuleLocked, http.StatusConflict, "已经发出去的券不能再改优惠方式、面额、门槛、范围与有效期；可以新建一种券")
	case errors.Is(err, pgrepo.ErrCouponStockShrink):
		return apperrors.New(errCodeCouponTemplateInvalid, http.StatusBadRequest, "发放总量不能少于已发出的张数")
	case errors.Is(err, pgrepo.ErrCouponInUse):
		return apperrors.New(errCodeCouponInUse, http.StatusConflict, "已经发过券或有订单参与过，不能删除；可以把它结束")
	case errors.Is(err, pgrepo.ErrCouponStatusTransition):
		return apperrors.New(errCodeCouponStatus, http.StatusConflict, "不能改成这个状态（已结束的不能再上线）")
	case errors.Is(err, pgrepo.ErrUserCouponState):
		return apperrors.New(errCodeCouponStatus, http.StatusConflict, "只能收回还没使用、也没被订单占用的券")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return err
}

// ── 管理端：目录与概览 ──

// Catalog 控制台表单的目录。
func (s *CouponService) Catalog() map[string]any { return coupon.Catalog() }

// Overview 优惠概览。
func (s *CouponService) Overview(ctx context.Context, appID int64, tz string, days int) (*coupon.Overview, error) {
	return s.pg.CouponOverview(ctx, appID, tz, days)
}

// ── 管理端：模板 ──

func (s *CouponService) checkPlanRefs(ctx context.Context, appID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	plans, err := s.pg.ListVipPlans(ctx, appID, false)
	if err != nil {
		return err
	}
	known := map[int64]bool{}
	for _, p := range plans {
		known[p.ID] = true
	}
	for _, id := range ids {
		if !known[id] {
			return apperrors.New(errCodeCouponTemplateInvalid, http.StatusBadRequest, fmt.Sprintf("会员套餐 #%d 不存在", id))
		}
	}
	return nil
}

func (s *CouponService) prepareTemplate(ctx context.Context, in coupon.TemplateInput) (coupon.TemplateInput, error) {
	in = coupon.NormalizeTemplate(in)
	if err := coupon.ValidateTemplate(in); err != nil {
		return in, apperrors.New(errCodeCouponTemplateInvalid, http.StatusBadRequest, err.Error())
	}
	return in, s.checkPlanRefs(ctx, in.AppID, in.PlanIDs)
}

// CreateTemplate 新建券（草稿）。
func (s *CouponService) CreateTemplate(ctx context.Context, in coupon.TemplateInput) (*coupon.Template, error) {
	in, err := s.prepareTemplate(ctx, in)
	if err != nil {
		return nil, err
	}
	t, err := s.pg.CreateCouponTemplate(ctx, in)
	return t, s.translate(err)
}

// UpdateTemplate 修改券。
func (s *CouponService) UpdateTemplate(ctx context.Context, id int64, in coupon.TemplateInput) (*coupon.Template, error) {
	in, err := s.prepareTemplate(ctx, in)
	if err != nil {
		return nil, err
	}
	t, err := s.pg.UpdateCouponTemplate(ctx, id, in)
	return t, s.translate(err)
}

// GetTemplate 取一种券。
func (s *CouponService) GetTemplate(ctx context.Context, appID, id int64) (*coupon.Template, error) {
	t, err := s.pg.GetCouponTemplate(ctx, appID, id)
	return t, s.translate(err)
}

// ListTemplates 券列表。
func (s *CouponService) ListTemplates(ctx context.Context, q coupon.TemplateQuery) (coupon.Page[coupon.Template], error) {
	if q.Status != "" && !coupon.ValidTemplateStatus(q.Status) {
		return coupon.Page[coupon.Template]{}, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	return s.pg.ListCouponTemplates(ctx, q)
}

// SetTemplateStatus 上线 / 暂停 / 结束。
func (s *CouponService) SetTemplateStatus(ctx context.Context, appID, id int64, status, operator string) (*coupon.Template, error) {
	if !coupon.ValidTemplateStatus(status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	t, err := s.pg.SetCouponTemplateStatus(ctx, appID, id, status, operator)
	return t, s.translate(err)
}

// DeleteTemplate 删除没发过的券。
func (s *CouponService) DeleteTemplate(ctx context.Context, appID, id int64) error {
	return s.translate(s.pg.DeleteCouponTemplate(ctx, appID, id))
}

// IssueOutcome 管理员发券时一个人的结果。
type IssueOutcome struct {
	UserID  int64   `json:"userId"`
	OK      bool    `json:"ok"`
	Message string  `json:"message,omitempty"`
	Coupons []int64 `json:"couponIds,omitempty"`
}

// Issue 管理员给一批用户发券（每人一个事务，互不影响）。暂停中的券也能发。
func (s *CouponService) Issue(ctx context.Context, appID, templateID int64, userIDs []int64, count int, operator string, notify bool) ([]IssueOutcome, error) {
	if len(userIDs) == 0 || len(userIDs) > coupon.MaxIssueUsers {
		return nil, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("一次给 1–%d 个用户发券", coupon.MaxIssueUsers))
	}
	if count < 1 || count > coupon.MaxIssueCount {
		return nil, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("每人发 1–%d 张", coupon.MaxIssueCount))
	}
	t, err := s.pg.GetCouponTemplate(ctx, appID, templateID)
	if err != nil {
		return nil, s.translate(err)
	}
	if err := coupon.Grantable(*t, count*len(userIDs), true, time.Now()); err != nil {
		return nil, apperrors.New(errCodeCouponUnavailable, http.StatusUnprocessableEntity, err.Error())
	}
	seen := map[int64]bool{}
	out := make([]IssueOutcome, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid <= 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		result, err := s.pg.GrantCoupons(ctx, coupon.GrantInput{
			AppID: appID, UserID: uid, TemplateID: templateID, Count: count,
			Source: coupon.SourceAdmin, SourceRef: operator, IgnoreStatus: true,
		})
		if err != nil {
			out = append(out, IssueOutcome{UserID: uid, Message: couponErrorMessage(s.translate(err))})
			continue
		}
		ids := make([]int64, 0, len(result.Coupons))
		for _, c := range result.Coupons {
			ids = append(ids, c.ID)
		}
		out = append(out, IssueOutcome{UserID: uid, OK: true, Coupons: ids})
		if notify {
			s.notifyReceived(appID, uid, result.TemplateName, len(ids), coupon.TemplateFace(*t))
		} else {
			s.publishChanged(appID, uid)
		}
	}
	return out, nil
}

func couponErrorMessage(err error) string {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return err.Error()
}

// ListUserCoupons 管理端用户券列表。
func (s *CouponService) ListUserCoupons(ctx context.Context, q coupon.UserCouponQuery) (coupon.Page[coupon.UserCoupon], error) {
	if !coupon.ValidUserCouponStatus(q.Status) {
		return coupon.Page[coupon.UserCoupon]{}, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	page, err := s.pg.ListUserCoupons(ctx, q, true)
	if err != nil {
		return page, err
	}
	now := time.Now()
	for i := range page.Items {
		page.Items[i].Status = page.Items[i].DisplayStatus(now)
	}
	return page, nil
}

// RevokeUserCoupon 收回一张券。
func (s *CouponService) RevokeUserCoupon(ctx context.Context, appID, id int64, reason string) (*coupon.UserCoupon, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "管理员收回"
	}
	c, err := s.pg.RevokeUserCoupon(ctx, appID, id, reason)
	if err != nil {
		return nil, s.translate(err)
	}
	s.publishChanged(appID, c.UserID)
	return c, nil
}

// ── 管理端：活动 ──

func (s *CouponService) preparePromotion(ctx context.Context, in coupon.PromotionInput) (coupon.PromotionInput, error) {
	in = coupon.NormalizePromotion(in)
	if err := coupon.ValidatePromotion(in); err != nil {
		return in, apperrors.New(errCodePromotionInvalid, http.StatusBadRequest, err.Error())
	}
	if err := s.checkPlanRefs(ctx, in.AppID, in.PlanIDs); err != nil {
		return in, err
	}
	if len(in.PackageIDs) > 0 {
		packages, err := s.pg.ListWalletPackages(ctx, in.AppID, false)
		if err != nil {
			return in, err
		}
		known := map[int64]bool{}
		for _, p := range packages {
			known[p.ID] = true
		}
		for _, id := range in.PackageIDs {
			if !known[id] {
				return in, apperrors.New(errCodePromotionInvalid, http.StatusBadRequest, fmt.Sprintf("充值档位 #%d 不存在", id))
			}
		}
	}
	if len(in.Rule.RebateCoupons) > 0 {
		ids := make([]int64, 0, len(in.Rule.RebateCoupons))
		for _, item := range in.Rule.RebateCoupons {
			ids = append(ids, item.TemplateID)
		}
		templates, err := s.pg.GetCouponTemplatesByIDs(ctx, in.AppID, ids)
		if err != nil {
			return in, err
		}
		for _, id := range ids {
			t, ok := templates[id]
			if !ok {
				return in, apperrors.New(errCodePromotionInvalid, http.StatusBadRequest, fmt.Sprintf("返的券 #%d 不存在", id))
			}
			if t.Status == coupon.StatusEnded {
				return in, apperrors.New(errCodePromotionInvalid, http.StatusBadRequest, "返的券「"+t.Name+"」已结束")
			}
		}
	}
	return in, nil
}

// CreatePromotion 新建活动（草稿）。
func (s *CouponService) CreatePromotion(ctx context.Context, in coupon.PromotionInput) (*coupon.Promotion, error) {
	in, err := s.preparePromotion(ctx, in)
	if err != nil {
		return nil, err
	}
	p, err := s.pg.CreateCouponPromotion(ctx, in)
	if err != nil {
		return nil, s.translate(err)
	}
	return s.decoratePromotion(ctx, p), nil
}

// UpdatePromotion 修改活动。
func (s *CouponService) UpdatePromotion(ctx context.Context, id int64, in coupon.PromotionInput) (*coupon.Promotion, error) {
	in, err := s.preparePromotion(ctx, in)
	if err != nil {
		return nil, err
	}
	p, err := s.pg.UpdateCouponPromotion(ctx, id, in)
	if err != nil {
		return nil, s.translate(err)
	}
	return s.decoratePromotion(ctx, p), nil
}

// GetPromotion 取一期活动。
func (s *CouponService) GetPromotion(ctx context.Context, appID, id int64) (*coupon.Promotion, error) {
	p, err := s.pg.GetCouponPromotion(ctx, appID, id)
	if err != nil {
		return nil, s.translate(err)
	}
	return s.decoratePromotion(ctx, p), nil
}

// ListPromotions 活动列表。
func (s *CouponService) ListPromotions(ctx context.Context, q coupon.PromotionQuery) (coupon.Page[coupon.Promotion], error) {
	if q.Status != "" && !coupon.ValidTemplateStatus(q.Status) {
		return coupon.Page[coupon.Promotion]{}, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	page, err := s.pg.ListCouponPromotions(ctx, q)
	if err != nil {
		return page, err
	}
	for i := range page.Items {
		page.Items[i] = *s.decoratePromotion(ctx, &page.Items[i])
	}
	return page, nil
}

// decoratePromotion 补上返券的券名与默认角标。
func (s *CouponService) decoratePromotion(ctx context.Context, p *coupon.Promotion) *coupon.Promotion {
	if p == nil {
		return nil
	}
	p.Tag = coupon.PromotionTag(*p)
	if len(p.Rule.RebateCoupons) > 0 {
		ids := make([]int64, 0, len(p.Rule.RebateCoupons))
		for _, item := range p.Rule.RebateCoupons {
			ids = append(ids, item.TemplateID)
		}
		if templates, err := s.pg.GetCouponTemplatesByIDs(ctx, p.AppID, ids); err == nil {
			for i := range p.Rule.RebateCoupons {
				if t, ok := templates[p.Rule.RebateCoupons[i].TemplateID]; ok {
					p.Rule.RebateCoupons[i].TemplateName = t.Name
				}
			}
		}
	}
	return p
}

// SetPromotionStatus 上线 / 暂停 / 结束活动。
func (s *CouponService) SetPromotionStatus(ctx context.Context, appID, id int64, status, operator string) (*coupon.Promotion, error) {
	if !coupon.ValidTemplateStatus(status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	p, err := s.pg.SetCouponPromotionStatus(ctx, appID, id, status, operator)
	if err != nil {
		return nil, s.translate(err)
	}
	return s.decoratePromotion(ctx, p), nil
}

// DeletePromotion 删除没人参与过的活动。
func (s *CouponService) DeletePromotion(ctx context.Context, appID, id int64) error {
	return s.translate(s.pg.DeleteCouponPromotion(ctx, appID, id))
}

// ListOrders 订单优惠记录。
func (s *CouponService) ListOrders(ctx context.Context, q coupon.OrderPromotionQuery) (coupon.Page[coupon.OrderPromotion], error) {
	if !coupon.ValidOrderStatus(q.Status) {
		return coupon.Page[coupon.OrderPromotion]{}, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	return s.pg.ListOrderPromotions(ctx, q)
}

// ── 用户事实 ──

// UserFacts 资格判定用的用户事实；会员按会员段与永久身份补判。
func (s *CouponService) UserFacts(ctx context.Context, appID, userID int64) (coupon.UserFacts, error) {
	facts, err := s.pg.CouponUserFacts(ctx, appID, userID)
	if err != nil {
		return facts, s.translate(err)
	}
	if !facts.IsVip {
		if holdings, err := s.pg.GetVipHoldings(ctx, appID, userID); err == nil {
			facts.IsVip = holdings.Lifetime || len(holdings.Tiers) > 0
		}
	}
	return facts, nil
}

func (s *CouponService) promotionStates(ctx context.Context, appID, userID int64) ([]coupon.PromotionState, error) {
	promotions, err := s.pg.ListLiveCouponPromotions(ctx, appID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(promotions))
	for _, p := range promotions {
		ids = append(ids, p.ID)
	}
	usage, err := s.pg.CountPromotionUsage(ctx, appID, userID, ids)
	if err != nil {
		return nil, err
	}
	states := make([]coupon.PromotionState, 0, len(promotions))
	for _, p := range promotions {
		states = append(states, coupon.PromotionState{Promotion: p, UserUsed: usage[p.ID]})
	}
	return states, nil
}

// ── 用户端：领券中心 ──

// CenterItem 领券中心的一种券。
type CenterItem struct {
	Template   coupon.Template `json:"template"`
	Face       string          `json:"face"`
	Claimed    int64           `json:"claimed"`
	Remaining  int64           `json:"remaining"`
	CanClaim   bool            `json:"canClaim"`
	ReasonCode string          `json:"reasonCode,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Audience   string          `json:"audience"`
}

// Center 领券中心：开放领取的券与这个人能不能领。
func (s *CouponService) Center(ctx context.Context, appID, userID int64) ([]CenterItem, error) {
	templates, err := s.pg.ListClaimableCouponTemplates(ctx, appID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(templates))
	for _, t := range templates {
		ids = append(ids, t.ID)
	}
	claimed, err := s.pg.CountCouponClaims(ctx, appID, userID, ids)
	if err != nil {
		return nil, err
	}
	facts, err := s.UserFacts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]CenterItem, 0, len(templates))
	for _, t := range templates {
		item := CenterItem{Template: publicTemplate(t), Face: coupon.TemplateFace(t), Claimed: claimed[t.ID], Remaining: -1, Audience: coupon.AudienceLabel(t.Audience)}
		if t.TotalQuantity > 0 {
			item.Remaining = max(t.TotalQuantity-t.IssuedCount, 0)
		}
		code, reason := coupon.ClaimCheck(t, claimed[t.ID], facts, now)
		item.CanClaim, item.ReasonCode, item.Reason = code == "", code, reason
		out = append(out, item)
	}
	return out, nil
}

// publicTemplate 用户端看到的模板：去掉运营统计与操作人。
func publicTemplate(t coupon.Template) coupon.Template {
	t.CreatedBy, t.UpdatedBy = "", ""
	t.UsedCount, t.DiscountTotal = 0, decimal.Zero
	return t
}

// Claim 领一张。
func (s *CouponService) Claim(ctx context.Context, appID, userID, templateID int64) (*coupon.UserCoupon, error) {
	facts, err := s.UserFacts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	c, err := s.pg.ClaimCoupon(ctx, appID, userID, templateID, facts)
	if err != nil {
		return nil, s.translate(err)
	}
	if c.Template != nil {
		t := publicTemplate(*c.Template)
		c.Template = &t
	}
	s.publishChanged(appID, userID)
	return c, nil
}

// MyCouponsResult 我的券。
type MyCouponsResult struct {
	coupon.Page[coupon.UserCoupon]
	Summary map[string]int64 `json:"summary"`
}

// MyCoupons 我的券（按展示状态筛选，带各状态计数）。
func (s *CouponService) MyCoupons(ctx context.Context, appID, userID int64, status string, page, limit int) (*MyCouponsResult, error) {
	if !coupon.ValidUserCouponStatus(status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "状态不认识")
	}
	result, err := s.pg.ListUserCoupons(ctx, coupon.UserCouponQuery{AppID: appID, UserID: userID, Status: status, Page: page, Limit: limit}, false)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range result.Items {
		item := &result.Items[i]
		item.Status = item.DisplayStatus(now)
		if item.Template != nil {
			t := publicTemplate(*item.Template)
			item.Template = &t
		}
	}
	summary, err := s.pg.CouponSummary(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	return &MyCouponsResult{Page: result, Summary: summary}, nil
}

// ── 用户端：活动 ──

// PromotionBrief 用户端看到的活动。
type PromotionBrief struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Tag         string               `json:"tag"`
	Theme       string               `json:"theme"`
	Kind        string               `json:"kind"`
	Rule        coupon.PromotionRule `json:"rule"`
	Scopes      []string             `json:"scopes"`
	PlanIDs     []int64              `json:"planIds"`
	PackageIDs  []int64              `json:"packageIds"`
	Audience    string               `json:"audience"`
	StartsAt    *time.Time           `json:"startsAt,omitempty"`
	EndsAt      *time.Time           `json:"endsAt,omitempty"`
	Started     bool                 `json:"started"`
	// Eligible 这个人能不能参加（不看金额）；不能时 Reason 写明
	Eligible        bool   `json:"eligible"`
	ReasonCode      string `json:"reasonCode,omitempty"`
	Reason          string `json:"reason,omitempty"`
	StackWithCoupon bool   `json:"stackWithCoupon"`
}

// PlanPrice 会员套餐的活动价。
type PlanPrice struct {
	PlanID        int64                    `json:"planId"`
	OriginalPrice decimal.Decimal          `json:"originalPrice"`
	Price         decimal.Decimal          `json:"price"`
	Promotion     *coupon.AppliedPromotion `json:"promotion,omitempty"`
}

// PromotionsView 用户端的活动页与套餐的活动价。
type PromotionsView struct {
	Promotions []PromotionBrief `json:"promotions"`
	PlanPrices []PlanPrice      `json:"planPrices"`
	// UsableCoupons 手里可用的券有几张（入口角标）
	UsableCoupons int64 `json:"usableCoupons"`
}

// Promotions 进行中与即将开始的活动、每个在售套餐不用券时的活动价。
func (s *CouponService) Promotions(ctx context.Context, appID, userID int64) (*PromotionsView, error) {
	states, err := s.promotionStates(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	facts, err := s.UserFacts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	view := &PromotionsView{Promotions: []PromotionBrief{}, PlanPrices: []PlanPrice{}}
	for _, state := range states {
		p := state.Promotion
		brief := PromotionBrief{ID: p.ID, Name: p.Name, Description: p.Description, Tag: coupon.PromotionTag(p), Theme: p.Theme,
			Kind: p.Kind, Rule: p.Rule, Scopes: p.Scopes, PlanIDs: p.PlanIDs, PackageIDs: p.PackageIDs,
			Audience: coupon.AudienceLabel(p.Audience), StartsAt: p.StartsAt, EndsAt: p.EndsAt,
			Started: p.StartsAt == nil || !now.Before(*p.StartsAt), StackWithCoupon: p.StackWithCoupon}
		// 资格只看人群、次数与名额：拿一个范围内的目标去判
		target := coupon.Target{Scope: firstScope(p.Scopes)}
		if len(p.PlanIDs) > 0 {
			target.PlanID = p.PlanIDs[0]
		}
		if len(p.PackageIDs) > 0 {
			target.PackageID = p.PackageIDs[0]
		}
		code, reason := coupon.PromotionEligible(p, state.UserUsed, target, facts, now)
		brief.Eligible, brief.ReasonCode, brief.Reason = code == "", code, reason
		view.Promotions = append(view.Promotions, brief)
	}
	if plans, err := s.pg.ListVipPlans(ctx, appID, true); err == nil {
		for _, plan := range plans {
			if plan.IsTrial() || !plan.Price.IsPositive() {
				continue
			}
			price, applied := coupon.BestPromotionPrice(states, coupon.Target{Scope: coupon.ScopeVip, PlanID: plan.ID, Amount: plan.Price}, facts, now)
			view.PlanPrices = append(view.PlanPrices, PlanPrice{PlanID: plan.ID, OriginalPrice: plan.Price, Price: price, Promotion: applied})
		}
	}
	if summary, err := s.pg.CouponSummary(ctx, appID, userID); err == nil {
		view.UsableCoupons = summary[coupon.CouponAvailable]
	}
	return view, nil
}

func firstScope(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	return scopes[0]
}

// ── 试算与下单 ──

// QuoteRequest 试算 / 下单的优惠入参。
type QuoteRequest struct {
	AppID     int64
	UserID    int64
	Purpose   string
	PlanID    int64
	PackageID int64
	Amount    decimal.Decimal
	// CouponID 指定的券（>0）；CouponMode 为 auto 时自动选最优，none 或空时不用券
	CouponID   int64
	CouponMode string
}

// Quote 试算：活动、每张券能不能用、最后要付多少。指定的券用不了时返回 42317。
func (s *CouponService) Quote(ctx context.Context, req QuoteRequest) (*coupon.Quote, coupon.Reservation, error) {
	scope := coupon.ScopeOfPurpose(req.Purpose)
	if scope == "" {
		return nil, coupon.Reservation{}, apperrors.New(40091, http.StatusBadRequest, "不支持的订单用途: "+req.Purpose)
	}
	if !req.Amount.IsPositive() {
		return nil, coupon.Reservation{}, apperrors.New(40072, http.StatusBadRequest, "支付金额无效")
	}
	states, err := s.promotionStates(ctx, req.AppID, req.UserID)
	if err != nil {
		return nil, coupon.Reservation{}, err
	}
	facts, err := s.UserFacts(ctx, req.AppID, req.UserID)
	if err != nil {
		return nil, coupon.Reservation{}, err
	}
	var coupons []coupon.UserCoupon
	// 充值不能用券
	if scope != coupon.ScopeRecharge {
		coupons, err = s.pg.ListQuoteCoupons(ctx, req.AppID, req.UserID)
		if err != nil {
			return nil, coupon.Reservation{}, err
		}
	}
	result := coupon.PriceQuote(coupon.QuoteInput{
		Now:        time.Now(),
		Target:     coupon.Target{Scope: scope, PlanID: req.PlanID, PackageID: req.PackageID, Amount: req.Amount},
		User:       facts,
		Promotions: states,
		Coupons:    coupons,
		CouponMode: req.CouponMode,
		CouponID:   req.CouponID,
	})
	for i := range result.Quote.Coupons {
		option := &result.Quote.Coupons[i]
		option.Coupon.Status = option.Coupon.DisplayStatus(time.Now())
		if option.Coupon.Template != nil {
			t := publicTemplate(*option.Coupon.Template)
			option.Coupon.Template = &t
		}
	}
	if result.SelectedErr != nil {
		return &result.Quote, coupon.Reservation{}, apperrors.New(errCodeCouponNotApplicable, http.StatusUnprocessableEntity,
			"这张券不能用于这笔订单："+result.SelectedErr.Reason).
			WithData(map[string]any{"reasonCode": result.SelectedErr.ReasonCode, "couponId": req.CouponID})
	}
	return &result.Quote, result.Quote.Reservation(req.Purpose, req.PlanID, req.PackageID), nil
}

// ApplyDiscountMetadata 把优惠快照写进订单 metadata（客户端传来的同名键先清掉）。
func ApplyDiscountMetadata(metadata map[string]any, res coupon.Reservation) map[string]any {
	out := make(map[string]any, len(metadata)+9)
	for k, v := range metadata {
		out[k] = v
	}
	for _, key := range paymentdomain.DiscountMetaKeys {
		delete(out, key)
	}
	if res.Empty() {
		return out
	}
	out[paymentdomain.MetaKeyOriginalAmount] = res.OriginalAmount.StringFixed(2)
	out[paymentdomain.MetaKeyPromotionDiscount] = res.PromotionDiscount.StringFixed(2)
	out[paymentdomain.MetaKeyCouponDiscount] = res.CouponDiscount.StringFixed(2)
	if res.PromotionID > 0 {
		out[paymentdomain.MetaKeyPromotionID] = res.PromotionID
		out[paymentdomain.MetaKeyPromotionName] = res.PromotionName
	}
	if res.UserCouponID > 0 {
		out[paymentdomain.MetaKeyCouponID] = res.UserCouponID
		out[paymentdomain.MetaKeyCouponName] = res.CouponName
	}
	if res.RebatePromotionID > 0 {
		out[paymentdomain.MetaKeyRebatePromotionID] = res.RebatePromotionID
		out[paymentdomain.MetaKeyRebateName] = res.RebatePromotionName
	}
	return out
}

// SettleOrder 订单付款后的优惠结算（幂等）。返券到账时通知用户。
func (s *CouponService) SettleOrder(ctx context.Context, orderID int64) (*coupon.OrderPromotion, error) {
	rec, err := s.pg.SettleOrderPromotion(ctx, orderID)
	if err != nil || rec == nil {
		return rec, err
	}
	if rec.UserCouponID != nil || rec.RebateStatus == coupon.RebateGranted {
		s.publishChanged(rec.AppID, rec.UserID)
	}
	if rec.RebateStatus == coupon.RebateGranted && rec.SettledAt == nil {
		// SettledAt 为空说明是这一次刚结算的（仓储层返回的是结算前读出的记录）
		s.notifyRebate(rec)
	}
	return rec, nil
}

// OrderPromotion 一笔订单的优惠记录（用户端订单详情、管理端订单详情用）。
func (s *CouponService) OrderPromotion(ctx context.Context, appID int64, orderNo string) (*coupon.OrderPromotion, error) {
	rec, err := s.pg.GetOrderPromotion(ctx, appID, orderNo)
	if errors.Is(err, pgrepo.ErrOrderPromotionNotFound) {
		return nil, nil
	}
	return rec, err
}

// ── 通知 ──

func (s *CouponService) publishChanged(appID, userID int64) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.userEvents.PublishUserEvent(ctx, appID, userID, CouponEventChanged, map[string]any{})
	}()
}

func (s *CouponService) notifyReceived(appID, userID int64, name string, count int, face string) {
	s.publishChanged(appID, userID)
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: notificationdomain.TypeCoupon,
			Title:   "你收到了 " + strconv.Itoa(count) + " 张优惠券",
			Content: "「" + name + "」" + face + "，已放进你的券包。",
			Action:  notificationdomain.RouteAction(notificationdomain.RouteCoupons, "查看我的券"),
		})
	}()
}

func (s *CouponService) notifyRebate(rec *coupon.OrderPromotion) {
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: rec.AppID, UserID: rec.UserID, Type: notificationdomain.TypeCoupon,
			Title:   "下单返利已到账",
			Content: "订单 " + rec.OrderNo + " 参加了「" + rec.RebatePromotionName + "」：" + rec.RebateNote + "。",
			Action:  notificationdomain.RouteAction(notificationdomain.RouteCoupons, "查看我的券"),
		})
	}()
}

// checkCouponRef 奖励里配的券要存在、没结束（草稿可以先配上，上线后才发得出去）。
func checkCouponRef(ctx context.Context, pg *pgrepo.Repository, appID, templateID int64) error {
	refs, err := pg.CouponRefs(ctx, appID)
	if err != nil {
		return err
	}
	if _, ok := refs[templateID]; !ok {
		return apperrors.New(40000, http.StatusBadRequest, "选择的优惠券不存在")
	}
	t, err := pg.GetCouponTemplate(ctx, appID, templateID)
	if err != nil {
		return err
	}
	if t.Status == coupon.StatusEnded {
		return apperrors.New(40000, http.StatusBadRequest, "优惠券「"+t.Name+"」已结束，不能用作奖励")
	}
	return nil
}
