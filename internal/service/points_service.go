package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

type PointsService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	location *time.Location
}

func NewPointsService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *PointsService {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	return &PointsService{log: log, pg: pg, sessions: sessions, location: location}
}

func (s *PointsService) GetOverview(ctx context.Context, session *authdomain.Session) (*pointdomain.Overview, error) {
	overview, err := s.pg.GetPointsOverview(ctx, session.UserID, session.AppID)
	if err != nil {
		return nil, err
	}
	if overview == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return overview, nil
}

func (s *PointsService) ListIntegralTransactions(ctx context.Context, session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error) {
	return s.pg.ListIntegralTransactions(ctx, session.UserID, session.AppID, page, limit)
}

func (s *PointsService) ListExperienceTransactions(ctx context.Context, session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error) {
	return s.pg.ListExperienceTransactions(ctx, session.UserID, session.AppID, page, limit)
}

func (s *PointsService) ListLevels(ctx context.Context) ([]pointdomain.LevelConfig, error) {
	return s.pg.ListLevelConfigs(ctx)
}

func (s *PointsService) GetMyLevel(ctx context.Context, session *authdomain.Session) (*pointdomain.LevelProfile, error) {
	profile, err := s.pg.GetUserLevelProfile(ctx, session.UserID, session.AppID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return profile, nil
}

func (s *PointsService) GetRankings(ctx context.Context, session *authdomain.Session, rankingType string, page int, limit int) (*pointdomain.RankingResponse, error) {
	switch rankingType {
	case "", "integral":
		return s.pg.GetIntegralRankings(ctx, session.AppID, page, limit, session.UserID)
	case "experience":
		return s.pg.GetExperienceRankings(ctx, session.AppID, page, limit, session.UserID)
	case "level":
		return s.pg.GetLevelRankings(ctx, session.AppID, page, limit, session.UserID)
	case "sign_today":
		return s.getCachedSignRanking(ctx, session, rankingType, page, limit)
	case "sign_consecutive":
		return s.getCachedSignRanking(ctx, session, rankingType, page, limit)
	case "sign_monthly":
		return s.getCachedSignRanking(ctx, session, rankingType, page, limit)
	default:
		return nil, apperrors.New(40009, http.StatusBadRequest, "不支持的排行类型")
	}
}

func (s *PointsService) GetLegacyRanking(ctx context.Context, session *authdomain.Session, appID int64, rankingType string, page int, limit int) (*pointdomain.RankingResponse, error) {
	if err := s.ensureSessionApp(session, appID); err != nil {
		return nil, err
	}
	return s.GetRankings(ctx, session, rankingType, page, limit)
}

func (s *PointsService) ResolveLegacyDailyRankingType(rankingType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(rankingType)) {
	case "", "today":
		return "sign_today", nil
	case "consecutive":
		return "sign_consecutive", nil
	case "monthly":
		return "sign_monthly", nil
	default:
		return "", apperrors.New(40009, http.StatusBadRequest, "排行榜类型无效，可选：today, consecutive, monthly")
	}
}

func (s *PointsService) GetAppStatistics(ctx context.Context, appID int64, timeRange int) (*pointdomain.AppStatistics, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	if timeRange <= 0 {
		timeRange = 30
	}
	if timeRange > 365 {
		timeRange = 365
	}
	now := time.Now().In(s.location)
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location).AddDate(0, 0, -(timeRange - 1))
	return s.pg.GetAppPointsStatistics(ctx, appID, timeRange, startDate, s.location.String())
}

func (s *PointsService) AdjustUserIntegral(ctx context.Context, userID int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.IntegralAdjustResult, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	if userID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID不能为空")
	}
	if amount == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "调整数量不能为0")
	}
	result, err := s.pg.AdjustUserIntegralByAdmin(ctx, userID, appID, amount, reason, options)
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrUserNotFound):
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		case errors.Is(err, pgrepo.ErrInsufficientIntegral):
			return nil, apperrors.New(40009, http.StatusBadRequest, "用户积分不足")
		default:
			return nil, err
		}
	}
	return result, nil
}

func (s *PointsService) AdjustUserExperience(ctx context.Context, userID int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.ExperienceAdjustResult, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	if userID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID不能为空")
	}
	if amount <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "调整数量必须大于0")
	}
	result, err := s.pg.AdjustUserExperienceByAdmin(ctx, userID, appID, amount, reason, options)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return nil, err
	}
	return result, nil
}

func (s *PointsService) BatchAdjustUserIntegral(ctx context.Context, userIDs []int64, appID int64, amount int64, operationType string, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.BatchIntegralAdjustResult, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	if len(userIDs) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户ID列表不能为空")
	}
	if len(userIDs) > 1000 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "批量操作最多支持1000个用户")
	}
	if amount <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "调整数量必须大于0")
	}

	op := strings.ToLower(strings.TrimSpace(operationType))
	if op == "" {
		op = "add"
	}
	if op != "add" && op != "consume" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "操作类型必须为add或consume")
	}

	signedAmount := amount
	if op == "consume" {
		signedAmount = -amount
	}

	result := &pointdomain.BatchIntegralAdjustResult{
		AppID:          appID,
		OperationType:  op,
		Amount:         amount,
		RequestedCount: len(userIDs),
		Results:        make([]pointdomain.IntegralAdjustResult, 0, len(userIDs)),
		Failures:       make([]pointdomain.BatchAdjustFailure, 0),
	}

	for _, userID := range userIDs {
		item, err := s.AdjustUserIntegral(ctx, userID, appID, signedAmount, reason, options)
		if err != nil {
			result.FailedCount++
			result.Failures = append(result.Failures, pointdomain.BatchAdjustFailure{
				UserID: userID,
				Error:  err.Error(),
			})
			continue
		}
		result.SuccessCount++
		result.Results = append(result.Results, *item)
	}

	return result, nil
}

func (s *PointsService) getCachedSignRanking(ctx context.Context, session *authdomain.Session, rankingType string, page int, limit int) (*pointdomain.RankingResponse, error) {
	scope, ttl := s.signRankingScope(rankingType)
	var cached pointdomain.RankingResponse
	if s.sessions != nil {
		found, err := s.sessions.GetRankingCache(ctx, "sign", session.AppID, rankingType, scope, page, limit, &cached)
		if err != nil {
			s.log.Warn("read sign ranking cache failed", zap.Error(err), zap.String("type", rankingType), zap.Int64("appid", session.AppID))
		} else if found {
			myRank, err := s.loadMySignRank(ctx, rankingType, session)
			if err != nil {
				return nil, err
			}
			cached.MyRank = myRank
			return &cached, nil
		}
	}

	response, err := s.loadSignRanking(ctx, rankingType, session, page, limit)
	if err != nil {
		return nil, err
	}
	if s.sessions != nil {
		cachePayload := *response
		cachePayload.MyRank = nil
		if err := s.sessions.SetRankingCache(ctx, "sign", session.AppID, rankingType, scope, page, limit, cachePayload, ttl); err != nil {
			s.log.Warn("write sign ranking cache failed", zap.Error(err), zap.String("type", rankingType), zap.Int64("appid", session.AppID))
		}
	}
	return response, nil
}

func (s *PointsService) loadSignRanking(ctx context.Context, rankingType string, session *authdomain.Session, page int, limit int) (*pointdomain.RankingResponse, error) {
	now := time.Now().In(s.location)
	switch rankingType {
	case "sign_today":
		return s.pg.GetTodaySignRankings(ctx, session.AppID, now, page, limit, session.UserID)
	case "sign_consecutive":
		return s.pg.GetConsecutiveSignRankings(ctx, session.AppID, page, limit, session.UserID)
	case "sign_monthly":
		return s.pg.GetMonthlySignRankings(ctx, session.AppID, now, page, limit, session.UserID)
	default:
		return nil, apperrors.New(40009, http.StatusBadRequest, "不支持的排行类型")
	}
}

func (s *PointsService) loadMySignRank(ctx context.Context, rankingType string, session *authdomain.Session) (*pointdomain.RankingItem, error) {
	now := time.Now().In(s.location)
	switch rankingType {
	case "sign_today":
		return s.pg.GetMyTodaySignRank(ctx, session.AppID, session.UserID, now)
	case "sign_consecutive":
		return s.pg.GetMyConsecutiveSignRank(ctx, session.AppID, session.UserID)
	case "sign_monthly":
		return s.pg.GetMyMonthlySignRank(ctx, session.AppID, session.UserID, now)
	default:
		return nil, apperrors.New(40009, http.StatusBadRequest, "不支持的排行类型")
	}
}

func (s *PointsService) signRankingScope(rankingType string) (string, time.Duration) {
	now := time.Now().In(s.location)
	switch rankingType {
	case "sign_today":
		return now.Format("2006-01-02"), 2 * time.Minute
	case "sign_monthly":
		return now.Format("2006-01"), 5 * time.Minute
	default:
		return "global", 5 * time.Minute
	}
}

func (s *PointsService) ensureSessionApp(session *authdomain.Session, appID int64) error {
	if session == nil {
		return apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	if appID > 0 && session.AppID != appID {
		return apperrors.New(40313, http.StatusForbidden, "应用不匹配")
	}
	return nil
}
