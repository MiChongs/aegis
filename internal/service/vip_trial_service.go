package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 会员判定与试用期会员。
//
// ── 判定为什么要收在一个入口 ──
//
// 「这个用户是不是会员」以前是三处各写一遍的表达式（仓储、远程函数 SDK、CSV 导出）。
// 三处都只回答"是/否"，而客户端真正要的是四个答案：是不是会员、还剩多久、
// 是不是试用、试用还能不能领。少任何一个，界面就只能猜 ——
// 试用期用户被引导去"续费"、已付费用户被弹"免费试用"，都是这么来的。
//
// 所以判定只有一个入口 `ResolveEntitlement`，事实一次查齐、结论由纯函数
// `vipdomain.Evaluate` 算出。HTTP 接口、远程函数 SDK、管理端看到的是同一份结论。

// 试用相关的业务错误码。
//
// 每一个判据对应一个码，客户端据此分支；文案与 `vipdomain.TrialReasonMessage`
// 同源，避免"接口说一句、状态里写另一句"。
const (
	errCodeTrialDeviceRequired   = 40040 // 400
	errCodeTrialAlreadyClaimed   = 40373 // 403
	errCodeTrialMemberActive     = 40374 // 403
	errCodeTrialDeviceClaimed    = 40375 // 403
	errCodeTrialPlanNotPurchase  = 40376 // 403
	errCodeTrialNotAvailable     = 40484 // 404
	errCodeTrialClaimNotFound    = 40485 // 404（管理端）
	errCodeTrialPlanKindInvalid  = 40014 // 400
	errCodeTrialPlanDuplicated   = 40018 // 400
	errCodeTrialPlanMustBeFree   = 40019 // 400
	errCodeMembershipUserMissing = 40401 // 404
)

// ── 判定 ──

// ResolveEntitlement 判定一个用户在一个应用里的会员权益。
//
// deviceID 只影响「还能不能领试用」这一段：应用开了设备维度去重时，
// 同一台设备已经有人领过就不给再领。传空串即"这次不判设备"，
// 此时若规则要求设备标识，资格判据会是 device_required 而不是静默放行 ——
// 开着的开关放行等于没有这个开关。
func (s *VipService) ResolveEntitlement(ctx context.Context, appID int64, userID int64, deviceID string) (*vipdomain.Entitlement, error) {
	if appID <= 0 || userID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID与应用ID不能为空")
	}
	facts, err := s.pg.GetVipEntitlementFacts(ctx, appID, userID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, apperrors.New(errCodeMembershipUserMissing, http.StatusNotFound, "用户不存在")
		}
		return nil, err
	}

	plan, err := s.pg.GetActiveTrialPlan(ctx, appID)
	if err != nil {
		return nil, err
	}
	if plan != nil {
		facts.TrialPlan = plan.TrialRef()
		// 设备维度只在「真有可能领得到」时才查：已经领过、已经是会员的用户，
		// 结论与设备无关，多打一次库只是浪费。
		alreadyMember := facts.ExpireAt != nil && facts.ExpireAt.After(time.Now())
		if plan.TrialDeviceLimited && facts.Claim == nil && !alreadyMember {
			if strings.TrimSpace(deviceID) == "" {
				facts.DeviceMissing = true
			} else {
				claimed, err := s.pg.TrialDeviceClaimed(ctx, appID, deviceID, userID)
				if err != nil {
					return nil, err
				}
				facts.DeviceClaimed = claimed
			}
		}
	}

	entitlement := vipdomain.Evaluate(*facts, time.Now())
	return &entitlement, nil
}

// MyEntitlement 当前登录用户的会员权益（设备标识取自会话）。
func (s *VipService) MyEntitlement(ctx context.Context, session *authdomain.Session) (*vipdomain.Entitlement, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	return s.ResolveEntitlement(ctx, session.AppID, session.UserID, session.DeviceID)
}

// IsMember 只要一个布尔的调用方走这里（远程函数 SDK、导出、内部判定）。
//
// 判定失败时返回 error 而不是 false：把"查不出来"当成"不是会员"，
// 表现就是一次数据库抖动让所有付费用户瞬间失去权益。
func (s *VipService) IsMember(ctx context.Context, appID int64, userID int64) (bool, error) {
	entitlement, err := s.ResolveEntitlement(ctx, appID, userID, "")
	if err != nil {
		return false, err
	}
	return entitlement.IsVIP, nil
}

// ── 试用领取 ──

// TrialOffer 当前应用的试用配置与该用户的资格（客户端渲染"免费试用"入口用）。
func (s *VipService) TrialOffer(ctx context.Context, session *authdomain.Session) (*vipdomain.TrialOffer, error) {
	entitlement, err := s.MyEntitlement(ctx, session)
	if err != nil {
		return nil, err
	}
	offer := entitlement.TrialOffer
	return &offer, nil
}

// ClaimTrial 领取试用。
//
// 没有幂等键：试用天然一人一次，唯一约束就是幂等键。网络重试落在仓储的
// "还在试用期内即返回上次结果"分支上，不会看到一句莫名其妙的"你已经领过了"。
func (s *VipService) ClaimTrial(ctx context.Context, session *authdomain.Session, clientIP string) (*vipdomain.TrialClaimResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	return s.claimTrialFor(ctx, session.AppID, session.UserID, session.DeviceID, clientIP, "")
}

// AdminClaimTrialFor 管理员代某用户领取试用（客服补发场景，走同一套资格判定）。
//
// 刻意不给"跳过资格检查"的开关：要跳过资格就是直接送天数，那件事
// `AdminGrantVip` 已经能做且会如实记成 admin_grant —— 混在试用里会污染转化率。
func (s *VipService) AdminClaimTrialFor(ctx context.Context, appID int64, userID int64, operator string) (*vipdomain.TrialClaimResult, error) {
	return s.claimTrialFor(ctx, appID, userID, "", "", strings.TrimSpace(operator))
}

func (s *VipService) claimTrialFor(ctx context.Context, appID int64, userID int64, deviceID string, clientIP string, operator string) (*vipdomain.TrialClaimResult, error) {
	plan, err := s.pg.GetActiveTrialPlan(ctx, appID)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, apperrors.New(errCodeTrialNotAvailable, http.StatusNotFound,
			vipdomain.TrialReasonMessage(vipdomain.TrialReasonNotConfigured))
	}
	// 管理员代领没有设备上下文，此时设备维度无从判定 —— 但它是一次有留痕的人工动作，
	// 因此按"这条规则管的是自助领取"处理，不拦。
	if plan.TrialDeviceLimited && operator == "" && strings.TrimSpace(deviceID) == "" {
		return nil, apperrors.New(errCodeTrialDeviceRequired, http.StatusBadRequest,
			vipdomain.TrialReasonMessage(vipdomain.TrialReasonDeviceRequired))
	}

	result, err := s.pg.ClaimVipTrial(ctx, pgrepo.TrialClaimInput{
		AppID:    appID,
		UserID:   userID,
		Plan:     *plan,
		DeviceID: deviceID,
		ClientIP: clientIP,
		Operator: operator,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrTrialAlreadyClaimed):
			return nil, apperrors.New(errCodeTrialAlreadyClaimed, http.StatusForbidden,
				vipdomain.TrialReasonMessage(vipdomain.TrialReasonAlreadyClaimed))
		case errors.Is(err, pgrepo.ErrTrialMemberActive):
			return nil, apperrors.New(errCodeTrialMemberActive, http.StatusForbidden,
				vipdomain.TrialReasonMessage(vipdomain.TrialReasonMemberActive))
		case errors.Is(err, pgrepo.ErrTrialDeviceClaimed):
			return nil, apperrors.New(errCodeTrialDeviceClaimed, http.StatusForbidden,
				vipdomain.TrialReasonMessage(vipdomain.TrialReasonDeviceClaimed))
		case errors.Is(err, pgrepo.ErrUserNotFound):
			return nil, apperrors.New(errCodeMembershipUserMissing, http.StatusNotFound, "用户不存在")
		default:
			return nil, err
		}
	}
	if !result.Replayed {
		s.log.Info("vip trial claimed",
			zap.Int64("appid", appID), zap.Int64("userId", userID),
			zap.Int64("planId", plan.ID), zap.Int("days", plan.DurationDays),
			zap.String("operator", operator))
	}
	return result, nil
}

// ── 管理端 ──

// AdminEntitlement 管理端查某用户的会员权益（含试用历史与资格）。
func (s *VipService) AdminEntitlement(ctx context.Context, appID int64, userID int64) (*vipdomain.Entitlement, error) {
	return s.ResolveEntitlement(ctx, appID, userID, "")
}

// AdminListTrialClaims 试用领取记录 + 汇总（累计 / 试用中 / 已转化）。
func (s *VipService) AdminListTrialClaims(ctx context.Context, appID int64, page int, limit int) ([]vipdomain.TrialClaim, int64, vipdomain.TrialSummary, error) {
	if appID <= 0 {
		return nil, 0, vipdomain.TrialSummary{}, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	page, limit = normalizePageLimit(page, limit, 200)
	return s.pg.ListVipTrialClaims(ctx, appID, page, limit)
}

// AdminResetTrialClaim 清除某用户的试用领取记录，让他可以再领一次。
//
// 只恢复资格，不动已经发出去的会员时长 —— 那是两件事。客服要的是"让他重领"，
// 顺手扣掉时长会变成用户眼里的"我的会员没了"。
func (s *VipService) AdminResetTrialClaim(ctx context.Context, appID int64, userID int64, operator string) error {
	if appID <= 0 || userID <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "用户ID与应用ID不能为空")
	}
	deleted, err := s.pg.DeleteVipTrialClaim(ctx, appID, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(errCodeTrialClaimNotFound, http.StatusNotFound, "该用户没有试用领取记录")
	}
	s.log.Info("vip trial eligibility reset",
		zap.Int64("appid", appID), zap.Int64("userId", userID), zap.String("operator", operator))
	return nil
}
