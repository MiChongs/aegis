package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	pointdomain "aegis/internal/domain/points"
	apperrors "aegis/pkg/errors"
)

// ── 管理端：按用户查看积分 / 经验流水 ──

const (
	maxLedgerSummaryDays = 180
	maxLedgerPageLimit   = 100
)

func parseLedgerKind(kind string) (pointdomain.Kind, error) {
	switch pointdomain.Kind(strings.TrimSpace(kind)) {
	case pointdomain.KindIntegral:
		return pointdomain.KindIntegral, nil
	case pointdomain.KindExperience:
		return pointdomain.KindExperience, nil
	}
	return "", apperrors.New(40000, http.StatusBadRequest, "流水类型只能是 integral 或 experience")
}

// AdminListLedger 某用户的积分或经验流水，支持方向、分类、关键词与时间区间筛选。
func (s *PointsService) AdminListLedger(ctx context.Context, kind string, q pointdomain.LedgerQuery) (*pointdomain.LedgerPage, error) {
	k, err := parseLedgerKind(kind)
	if err != nil {
		return nil, err
	}
	q.Kind = k
	switch q.Direction {
	case "", "in", "out":
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "方向只能是 in 或 out")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 20
	}
	if q.Limit > maxLedgerPageLimit {
		q.Limit = maxLedgerPageLimit
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "开始时间必须早于结束时间")
	}
	return s.pg.AdminListPointLedger(ctx, q)
}

// AdminLedgerSummary 某用户的积分或经验汇总，days 天内的每日走势按 tz 切日（空则用服务器默认时区）。
func (s *PointsService) AdminLedgerSummary(ctx context.Context, kind string, appID, userID int64, days int, tz string) (*pointdomain.LedgerSummary, error) {
	k, err := parseLedgerKind(kind)
	if err != nil {
		return nil, err
	}
	if days < 0 {
		days = 0
	}
	if days > maxLedgerSummaryDays {
		days = maxLedgerSummaryDays
	}
	loc := s.location
	if tz = strings.TrimSpace(tz); tz != "" {
		parsed, err := time.LoadLocation(tz)
		if err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, "时区无效")
		}
		loc = parsed
	}
	return s.pg.AdminPointLedgerSummary(ctx, k, appID, userID, days, loc)
}

// AdminReverseIntegral 冲正一笔积分流水：按原金额反向调整一次。
//
// 请求编号由原流水号派生，同一笔只能冲正一次；重复提交返回首次结果（Replayed=true）。
// 冲正本身的流水（REV 编号派生的 ADI 流水）不能再被冲正，避免来回翻转。
func (s *PointsService) AdminReverseIntegral(ctx context.Context, appID, userID int64, transactionNo string, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.IntegralAdjustResult, error) {
	transactionNo = strings.TrimSpace(transactionNo)
	if transactionNo == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "缺少流水号")
	}
	original, err := s.pg.AdminGetPointLedgerTransaction(ctx, pointdomain.KindIntegral, appID, userID, transactionNo)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "流水不存在或不属于该用户")
	}
	if original.Amount == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "该流水金额为 0，无需冲正")
	}
	if reversed, _ := original.ExtraData["reversalOf"].(string); reversed != "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "冲正流水不能再次冲正")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = fmt.Sprintf("冲正 %s", original.TransactionNo)
	}
	sum := sha256.Sum256([]byte(original.TransactionNo))
	options.Extra = map[string]any{"reversalOf": original.TransactionNo}
	return s.AdminAdjustIntegral(ctx, pointdomain.AdjustInput{
		AppID:     appID,
		UserID:    userID,
		Amount:    -original.Amount,
		Reason:    reason,
		RequestID: "rev_" + hex.EncodeToString(sum[:16]),
	}, options)
}

// AdminReverseExperience 冲正一笔经验流水：按原数量反向调整一次（获得的扣回、扣减的还回）。
//
// 扣回时 levelMode 决定等级怎么处理（keep / recalc，见 pointdomain.LevelMode）；还回时按经验正常升级，
// 经过的达成奖励若已发过不会重复发。同一笔只能冲正一次，冲正流水不能再冲正。
func (s *PointsService) AdminReverseExperience(ctx context.Context, appID, userID int64, transactionNo string, reason string, levelMode string, options pointdomain.AdminAdjustOptions) (*pointdomain.ExperienceAdjustResult, error) {
	transactionNo = strings.TrimSpace(transactionNo)
	if transactionNo == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "缺少流水号")
	}
	mode, err := pointdomain.NormalizeLevelMode(levelMode)
	if err != nil {
		return nil, mapPointsAdjustError(pointdomain.KindExperience, err)
	}
	original, err := s.pg.AdminGetPointLedgerTransaction(ctx, pointdomain.KindExperience, appID, userID, transactionNo)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "流水不存在或不属于该用户")
	}
	if original.Amount == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "该流水数量为 0，无需冲正")
	}
	if reversed, _ := original.ExtraData["reversalOf"].(string); reversed != "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "冲正流水不能再次冲正")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = fmt.Sprintf("冲正 %s", original.TransactionNo)
	}
	sum := sha256.Sum256([]byte(original.TransactionNo))
	options.Extra = map[string]any{"reversalOf": original.TransactionNo}
	return s.AdminAdjustExperience(ctx, pointdomain.AdjustInput{
		AppID:     appID,
		UserID:    userID,
		Amount:    -original.Amount,
		Reason:    reason,
		RequestID: "rev_" + hex.EncodeToString(sum[:16]),
		LevelMode: mode,
	}, options)
}
