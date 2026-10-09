package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	authdomain "aegis/internal/domain/auth"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// VipService 会员系统：套餐管理 / 状态查询 / 余额购买 / 管理端授予与扣减。
// 开通一律顺延；到期时间只在退款冲正与扣减（AdminRevokeVip）时变早。
// 永久会员是与之并列的另一条线（users.vip_lifetime_at）：永久开通不动到期时间，
// 只在退款冲正与取消永久会员（AdminRevokeVipLifetime）时失去。
// 所有变更落 vip_transactions 账本。
//
// 功能权益跟随套餐的**当前**配置：改套餐即对所有在期会员生效，包括拿掉功能（降级）。
// 判定规则见 internal/domain/vip/segment.go。
type VipService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
	// payments 凭证引擎的持有者；余额直购成功后由它按应用设置自动寄送凭证
	payments *PaymentService
}

func NewVipService(log *zap.Logger, pg *pgrepo.Repository) *VipService {
	return &VipService{log: log, pg: pg}
}

// SetPaymentService 注入凭证引擎（bootstrap 中调用）。
func (s *VipService) SetPaymentService(p *PaymentService) { s.payments = p }

// ── 用户侧 ──

// ListActivePlans 用户可见的在售套餐（不含试用，理由见仓储层注释）。
//
// 试用套餐的信息由 `/vip/status` 的 trialOffer 给出：那里同时带着「能不能领」，
// 而这里给不出这个答案 —— 一份列不出资格的试用卡片，客户端只能先渲染再报错。
//
// 永久会员已经包含的套餐标上 included：它们买了也拿不到任何新东西，购买会以 40378 拒绝。
// 标出来而不是从列表里拿掉 —— 永久会员看到一张空的套餐页，会以为是加载失败。
func (s *VipService) ListActivePlans(ctx context.Context, session *authdomain.Session) ([]vipdomain.Plan, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	plans, err := s.pg.ListPurchasableVipPlans(ctx, session.AppID)
	if err != nil || len(plans) == 0 {
		return plans, err
	}
	lifetime, features, err := s.pg.GetVipLifetimeCoverage(ctx, session.AppID, session.UserID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return plans, nil
		}
		return nil, err
	}
	for i := range plans {
		plans[i].Included = vipdomain.LifetimeIncludes(lifetime, features, plans[i])
	}
	return plans, nil
}

// errCodeVipLifetimeIncluded 永久会员已经包含该套餐的全部权益（购买 / 下单）
const errCodeVipLifetimeIncluded = 40378 // 403

// errVipLifetimeIncluded 购买与下单共用同一句话。
func errVipLifetimeIncluded() error {
	return apperrors.New(errCodeVipLifetimeIncluded, http.StatusForbidden, "永久会员已包含该套餐的全部权益，无需购买")
}

// ensurePlanNotIncluded 下单前拦住「永久会员已经包含」的套餐（在线直购走这里，余额购买在事务内判）。
func (s *VipService) ensurePlanNotIncluded(ctx context.Context, appID int64, userID int64, plan vipdomain.Plan) error {
	lifetime, features, err := s.pg.GetVipLifetimeCoverage(ctx, appID, userID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return err
	}
	if vipdomain.LifetimeIncludes(lifetime, features, plan) {
		return errVipLifetimeIncluded()
	}
	return nil
}

// 当前用户的会员状态见 `MyEntitlement`（vip_trial_service.go）——
// 「是不是会员」只有那一个判定入口，这里刻意不再留一个只答"是/否"的简版：
// 两个入口回答同一个问题，迟早会有一个说得不一样。

// MyTransactions 当前用户开通/续费记录
func (s *VipService) MyTransactions(ctx context.Context, session *authdomain.Session, page int, limit int) ([]vipdomain.Transaction, int64, error) {
	if session == nil {
		return nil, 0, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	page, limit = normalizePageLimit(page, limit, 100)
	return s.pg.ListVipTransactions(ctx, session.UserID, session.AppID, page, limit)
}

// PurchaseWithWallet 余额购买套餐。
// 套餐价格、时长、赠送积分均以服务端套餐配置为准（防客户端篡改）；
// idempotencyKey 防止网络重试导致重复扣款 / 重复续期。
func (s *VipService) PurchaseWithWallet(ctx context.Context, session *authdomain.Session, planID int64, idempotencyKey string, clientIP string) (*vipdomain.PurchaseResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	if planID <= 0 {
		return nil, apperrors.New(40084, http.StatusBadRequest, "套餐ID不能为空")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, apperrors.New(40082, http.StatusBadRequest, "必须提供幂等键 idempotencyKey，防止重复购买")
	}
	plan, err := s.requireActivePlan(ctx, session.AppID, planID)
	if err != nil {
		return nil, err
	}
	result, err := s.pg.PurchaseVipWithWallet(ctx, session.UserID, session.AppID, *plan,
		walletIdemKey(session.AppID, session.UserID, "vip:"+idempotencyKey), clientIP)
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrInsufficientBalance):
			return nil, apperrors.New(40083, http.StatusBadRequest, "余额不足，请先充值")
		case errors.Is(err, pgrepo.ErrUserNotFound):
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		case errors.Is(err, pgrepo.ErrVipLifetimeIncluded):
			return nil, errVipLifetimeIncluded()
		default:
			return nil, err
		}
	}
	// 余额直购是一笔实打实的购买，凭证待遇必须与「用支付宝买同一个套餐」一致。
	if s.payments != nil {
		result.Receipt = s.payments.BuildVipPurchaseReceipt(ctx, session, result.WalletTransactionNo)
		// 幂等重放不再寄一次：用户重试一下网络请求不该多收到一封收据
		if !result.Replayed && strings.TrimSpace(result.WalletTransactionNo) != "" {
			go s.payments.autoEmailWalletReceipt(session.AppID, result.WalletTransactionNo)
		}
	}
	return result, nil
}

// requireActivePlan 取在售套餐（购买场景）
func (s *VipService) requireActivePlan(ctx context.Context, appID int64, planID int64) (*vipdomain.Plan, error) {
	plan, err := s.pg.GetVipPlan(ctx, appID, planID)
	if err != nil {
		return nil, err
	}
	if plan == nil || !plan.IsActive {
		return nil, apperrors.New(40480, http.StatusNotFound, "套餐不存在或已下架")
	}
	// 试用套餐是 0 元的，走购买入口就是一个绕开资格判定的免费开通入口：
	// 换个幂等键再打一次，天数就又发一次。这条判断是它唯一的闸门。
	if plan.IsTrial() {
		return nil, apperrors.New(errCodeTrialPlanNotPurchase, http.StatusForbidden,
			"试用套餐只能领取，不能购买")
	}
	return plan, nil
}

// ── 管理端 ──

func (s *VipService) AdminListPlans(ctx context.Context, appID int64) ([]vipdomain.Plan, error) {
	return s.pg.ListVipPlans(ctx, appID, false)
}

func (s *VipService) AdminSavePlan(ctx context.Context, mutation vipdomain.PlanMutation) (*vipdomain.Plan, error) {
	if mutation.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	if err := s.validatePlanKind(ctx, &mutation); err != nil {
		return nil, err
	}
	// 套餐引用的功能标识必须都在目录里。不校验的话，套餐上一个拼错的标识
	// 不会有任何提示，直到几周后接入方来问「为什么买了高级版还是用不了导出」。
	if mutation.Features != nil {
		if err := s.EnsureFeatureTagsRegistered(ctx, mutation.AppID, *mutation.Features); err != nil {
			return nil, err
		}
	}
	lifetime, err := s.resolvePlanLifetime(ctx, &mutation)
	if err != nil {
		return nil, err
	}
	if mutation.ID == 0 {
		// 新建套餐的必填校验
		if mutation.Name == nil || strings.TrimSpace(*mutation.Name) == "" {
			return nil, apperrors.New(40085, http.StatusBadRequest, "套餐名称不能为空")
		}
		if !lifetime && (mutation.DurationDays == nil || *mutation.DurationDays <= 0) {
			return nil, apperrors.New(40086, http.StatusBadRequest, "套餐时长必须大于 0 天")
		}
		if mutation.Price == nil || mutation.Price.IsNegative() {
			return nil, apperrors.New(40087, http.StatusBadRequest, "套餐价格不能为负")
		}
	}
	if !lifetime && mutation.DurationDays != nil && *mutation.DurationDays <= 0 {
		return nil, apperrors.New(40086, http.StatusBadRequest, "套餐时长必须大于 0 天")
	}
	if mutation.Price != nil && mutation.Price.IsNegative() {
		return nil, apperrors.New(40087, http.StatusBadRequest, "套餐价格不能为负")
	}
	if mutation.BonusIntegral != nil && *mutation.BonusIntegral < 0 {
		return nil, apperrors.New(40088, http.StatusBadRequest, "赠送积分不能为负")
	}
	plan, err := s.pg.UpsertVipPlan(ctx, mutation)
	if err != nil {
		// vip_plans 上唯一的唯一索引就是「每个应用至多一个启用中的试用套餐」。
		// 不翻译的话，管理员在控制台上看到的是一句原始的 SQL 约束名。
		if pgrepo.IsUniqueViolation(err) {
			return nil, apperrors.New(errCodeTrialPlanDuplicated, http.StatusBadRequest,
				"该应用已有启用中的试用套餐，请先停用原有的那个")
		}
		return nil, err
	}
	if plan == nil {
		return nil, apperrors.New(40480, http.StatusNotFound, "套餐不存在")
	}
	return plan, nil
}

// validatePlanKind 校验套餐种类，并把试用套餐的隐含约束补齐。
//
// 试用套餐恒为 0 元这条**不静默改写**管理员填的价格，而是当场报错：
// 悄悄把 9.9 改成 0 存下去，控制台刷新后显示 0，没有人说得出为什么。
func (s *VipService) validatePlanKind(ctx context.Context, mutation *vipdomain.PlanMutation) error {
	kind := ""
	if mutation.Kind != nil {
		kind = strings.ToLower(strings.TrimSpace(*mutation.Kind))
		if kind == "" {
			kind = vipdomain.KindPaid
		}
		if kind != vipdomain.KindPaid && kind != vipdomain.KindTrial {
			return apperrors.New(errCodeTrialPlanKindInvalid, http.StatusBadRequest,
				"套餐类型只能是 paid（付费）或 trial（试用）")
		}
		mutation.Kind = &kind
	} else if mutation.ID > 0 {
		// 未指定种类时沿用库里的：控制台保存"改个名字"不该把试用套餐变成付费套餐
		existing, err := s.pg.GetVipPlan(ctx, mutation.AppID, mutation.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			kind = existing.Kind
		}
	}
	if kind != vipdomain.KindTrial {
		return nil
	}
	if mutation.Price != nil && mutation.Price.IsPositive() {
		return apperrors.New(errCodeTrialPlanMustBeFree, http.StatusBadRequest,
			"试用套餐必须是 0 元 —— 它只能领取，不能购买")
	}
	// 新建试用套餐时价格可以不填，这里补一个 0，免得撞上 CHECK 约束
	if mutation.Price == nil && mutation.ID == 0 {
		zero := decimal.Zero
		mutation.Price = &zero
	}
	return nil
}

// resolvePlanLifetime 校验并解出这个套餐是不是永久套餐。
//
// 永久只能在创建时定：已经卖出去的开通按开通时的形态记账（永久的没有终点、限时的在顺延链上），
// 套餐一改，「这个套餐卖的到底是什么」在账上就说不清了 —— 要换形态请新建一个套餐、下架旧的。
// 试用不能是永久的：永久的试用就是白送永久会员。
func (s *VipService) resolvePlanLifetime(ctx context.Context, mutation *vipdomain.PlanMutation) (bool, error) {
	lifetime := false
	if mutation.ID > 0 {
		existing, err := s.pg.GetVipPlan(ctx, mutation.AppID, mutation.ID)
		if err != nil {
			return false, err
		}
		if existing == nil {
			// 交给仓储层统一报「套餐不存在」
			return mutation.Lifetime != nil && *mutation.Lifetime, nil
		}
		if mutation.Lifetime != nil && *mutation.Lifetime != existing.Lifetime {
			return false, apperrors.New(errCodeTrialPlanKindInvalid, http.StatusBadRequest,
				"套餐创建后不能在永久与限时之间切换，请新建套餐")
		}
		lifetime = existing.Lifetime
	} else if mutation.Lifetime != nil {
		lifetime = *mutation.Lifetime
	}
	if lifetime && mutation.Kind != nil && *mutation.Kind == vipdomain.KindTrial {
		return false, apperrors.New(errCodeTrialPlanKindInvalid, http.StatusBadRequest, "试用套餐不能设为永久")
	}
	return lifetime, nil
}

func (s *VipService) AdminDeletePlan(ctx context.Context, appID int64, planID int64) error {
	deleted, err := s.pg.DeleteVipPlan(ctx, appID, planID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40480, http.StatusNotFound, "套餐不存在")
	}
	return nil
}

// AdminVipGrantInput 管理员发放会员的输入。
//
// 两种发放方式，二选一：
//   - PlanID > 0：按套餐发放。时长/赠送积分取自套餐 × Quantity，功能跟随套餐当前配置；
//     Days 与 Features 忽略 —— 套餐是运营定好的商品，发放时不允许现场改配置。
//     永久套餐只能发 1 份（永久乘以 N 还是永久，赠送积分却会乘上去）。
//   - PlanID == 0：自定义发放。Days 为必填时长，Features 为附带的权益标识
//     （必须已登记在会员功能目录，防止拼错的标识悄悄进账本）。它不挂任何套餐，
//     功能就是这里给的这一份，改哪个套餐都影响不到它。
//     Lifetime 为 true 时发的是永久会员，Days 忽略。
type AdminVipGrantInput struct {
	UserID        int64
	AppID         int64
	PlanID        int64
	Quantity      int
	Days          int
	Lifetime      bool
	Features      []string
	Reason        string
	BonusIntegral int64
	Operator      string
}

// AdminGrantVip 管理员发放会员（不动钱包，可附赠积分；到期时间只增不减）。
func (s *VipService) AdminGrantVip(ctx context.Context, in AdminVipGrantInput) (*vipdomain.Transaction, error) {
	if in.UserID <= 0 || in.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID与应用ID不能为空")
	}
	if in.BonusIntegral < 0 {
		return nil, apperrors.New(40088, http.StatusBadRequest, "赠送积分不能为负")
	}
	reason := strings.TrimSpace(in.Reason)

	grant := vipdomain.Grant{
		UserID:     in.UserID,
		AppID:      in.AppID,
		PayChannel: vipdomain.ChannelAdminGrant,
		PayAmount:  decimal.Zero,
		Operator:   in.Operator,
		Metadata:   map[string]any{"reason": reason},
	}

	if in.PlanID > 0 {
		quantity := in.Quantity
		if quantity <= 0 {
			quantity = 1
		}
		if quantity > 100 {
			return nil, apperrors.New(40086, http.StatusBadRequest, "单次发放数量不能超过 100")
		}
		plan, err := s.pg.GetVipPlan(ctx, in.AppID, in.PlanID)
		if err != nil {
			return nil, err
		}
		if plan == nil {
			return nil, apperrors.New(40480, http.StatusNotFound, "套餐不存在")
		}
		// 试用是资格制的（一人一次、可能限设备），走发放入口会绕开全部资格判定，
		// 且不落 trial claims 账本 —— 之后没人说得清这个人的试用资格用没用过。
		if plan.IsTrial() {
			return nil, apperrors.New(errCodeTrialPlanNotPurchase, http.StatusForbidden,
				"试用套餐请通过「代领试用」发放")
		}
		if plan.Lifetime && quantity > 1 {
			return nil, apperrors.New(40086, http.StatusBadRequest, "永久套餐单次只能发放 1 份")
		}
		grant.PlanID = &plan.ID
		grant.PlanName = plan.Name
		grant.Features = plan.Features
		grant.DurationDays = plan.DurationDays * quantity
		grant.Lifetime = plan.Lifetime
		grant.BonusIntegral = plan.BonusIntegral*int64(quantity) + in.BonusIntegral
		grant.Metadata["quantity"] = quantity
	} else {
		if !in.Lifetime && in.Days <= 0 {
			return nil, apperrors.New(40086, http.StatusBadRequest, "发放天数必须大于 0")
		}
		features := vipdomain.NormalizeFeatureTags(in.Features)
		if len(features) > 0 {
			if err := s.EnsureFeatureTagsRegistered(ctx, in.AppID, features); err != nil {
				return nil, err
			}
		}
		grant.PlanName = reason
		if grant.PlanName == "" {
			grant.PlanName = "管理员授予"
			if in.Lifetime {
				grant.PlanName = "管理员授予永久会员"
			}
		}
		grant.Features = features
		grant.BonusIntegral = in.BonusIntegral
		if in.Lifetime {
			grant.Lifetime = true
		} else {
			grant.DurationDays = in.Days
		}
	}

	txn, err := s.pg.GrantVip(ctx, grant)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return nil, err
	}
	return txn, nil
}

// errCodeVipNotActive 扣减天数时该用户当前不是会员
const errCodeVipNotActive = 40377 // 403

// vipRevokeMaxDays 单次扣减的上限。扣减本来就会在「此刻」截住，
// 这个上限只是不让一个离谱的数字（脚本里算错的毫秒数）进到日期运算里溢出。
const vipRevokeMaxDays = 36500

// AdminVipRevokeInput 扣减会员天数的输入。
type AdminVipRevokeInput struct {
	UserID   int64
	AppID    int64
	Days     int
	Reason   string
	Operator string
}

// AdminRevokeVip 扣减会员天数（远程函数 `aegis.vip.revoke`）。
//
// 从到期时间往回截，截过此刻即会员立即结束；排在截断点之后的会员段整段失效，
// 因此扣掉的恰好是后买的高级版那段时，高级版的功能会一并收回（见 truncateVipSegmentsTx）。
//
// 不是会员时报错而不是静默成功：脚本作者以为扣掉了、实际什么也没发生，
// 是比报错更难查的结果。
func (s *VipService) AdminRevokeVip(ctx context.Context, in AdminVipRevokeInput) (*vipdomain.Transaction, error) {
	if in.UserID <= 0 || in.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID与应用ID不能为空")
	}
	if in.Days <= 0 {
		return nil, apperrors.New(40086, http.StatusBadRequest, "扣减天数必须大于 0")
	}
	if in.Days > vipRevokeMaxDays {
		in.Days = vipRevokeMaxDays
	}
	reason := strings.TrimSpace(in.Reason)
	txn, err := s.pg.RevokeVipDays(ctx, vipdomain.Revoke{
		UserID:   in.UserID,
		AppID:    in.AppID,
		Days:     in.Days,
		Reason:   reason,
		Operator: in.Operator,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrUserNotFound):
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		case errors.Is(err, pgrepo.ErrVipNotActive):
			// 永久会员也会走到这里：扣天数只扣限时那条线，永久会员请用「取消永久会员」
			return nil, apperrors.New(errCodeVipNotActive, http.StatusForbidden, "该用户当前没有可扣减的限时会员时长")
		default:
			return nil, err
		}
	}
	s.log.Info("vip days revoked",
		zap.Int64("appid", in.AppID), zap.Int64("userId", in.UserID),
		zap.Int("days", in.Days), zap.String("operator", in.Operator), zap.String("reason", reason))
	return txn, nil
}

// errCodeVipLifetimeNotActive 取消永久会员时该用户不是永久会员
const errCodeVipLifetimeNotActive = 40379 // 403

// AdminVipRevokeLifetimeInput 取消永久会员的输入。
type AdminVipRevokeLifetimeInput struct {
	UserID   int64
	AppID    int64
	Reason   string
	Operator string
}

// AdminRevokeVipLifetime 取消永久会员（管理端 / 远程函数 `aegis.vip.revokeLifetime`）。
//
// 作废该用户全部仍生效的永久开通并清除永久身份；另买的限时会员不受影响。
// 不是永久会员时报错而不是静默成功，理由同 AdminRevokeVip。
// 用户付过钱的永久套餐请走订单退款：那条路会把钱一并退回，这里只收权益。
func (s *VipService) AdminRevokeVipLifetime(ctx context.Context, in AdminVipRevokeLifetimeInput) (*vipdomain.Transaction, error) {
	if in.UserID <= 0 || in.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID与应用ID不能为空")
	}
	reason := strings.TrimSpace(in.Reason)
	txn, err := s.pg.RevokeVipLifetime(ctx, vipdomain.RevokeLifetime{
		UserID:   in.UserID,
		AppID:    in.AppID,
		Reason:   reason,
		Operator: in.Operator,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrUserNotFound):
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		case errors.Is(err, pgrepo.ErrVipLifetimeNotActive):
			return nil, apperrors.New(errCodeVipLifetimeNotActive, http.StatusForbidden, "该用户不是永久会员")
		default:
			return nil, err
		}
	}
	s.log.Info("vip lifetime revoked",
		zap.Int64("appid", in.AppID), zap.Int64("userId", in.UserID),
		zap.String("operator", in.Operator), zap.String("reason", reason))
	return txn, nil
}

// AdminListTransactions 管理端查询 VIP 记录（userID 为 0 查全应用）
func (s *VipService) AdminListTransactions(ctx context.Context, appID int64, userID int64, page int, limit int) ([]vipdomain.Transaction, int64, error) {
	page, limit = normalizePageLimit(page, limit, 200)
	return s.pg.ListVipTransactions(ctx, userID, appID, page, limit)
}
