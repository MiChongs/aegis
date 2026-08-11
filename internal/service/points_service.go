package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type PointsService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	location *time.Location
	// 排行榜并发合并：多个用户同时请求同一榜单页时只打一次 DB
	rankingFlight singleflight.Group
}

func NewPointsService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *PointsService {
	return &PointsService{log: log, pg: pg, sessions: sessions, location: timeutil.DefaultLocation()}
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
		return s.getCachedPointsRanking(ctx, session, "integral", page, limit)
	case "experience":
		return s.getCachedPointsRanking(ctx, session, "experience", page, limit)
	case "level":
		return s.getCachedPointsRanking(ctx, session, "level", page, limit)
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

// getCachedPointsRanking 为 integral / experience / level 提供 Redis 缓存 + singleflight 防击穿
func (s *PointsService) getCachedPointsRanking(ctx context.Context, session *authdomain.Session, rankingType string, page int, limit int) (*pointdomain.RankingResponse, error) {
	// 读 Redis
	scope, ttl := s.pointsRankingScope(rankingType)
	var cached pointdomain.RankingResponse
	if s.sessions != nil {
		found, err := s.sessions.GetRankingCache(ctx, "points", session.AppID, rankingType, scope, page, limit, &cached)
		if err != nil {
			s.log.Warn("read points ranking cache failed", zap.Error(err), zap.String("type", rankingType), zap.Int64("appid", session.AppID))
		} else if found {
			// 单独查"我的排名"，不能用缓存（每个用户不同）
			if session.UserID > 0 {
				my, err := s.loadMyPointsRank(ctx, rankingType, session)
				if err == nil {
					cached.MyRank = my
				}
			}
			return &cached, nil
		}
	}

	// singleflight：同一 appID+type+page+limit 合并并发
	flightKey := fmt.Sprintf("points:%d:%s:%d:%d", session.AppID, rankingType, page, limit)
	result, err, _ := s.rankingFlight.Do(flightKey, func() (any, error) {
		return s.loadPointsRanking(ctx, rankingType, session, page, limit)
	})
	if err != nil {
		return nil, err
	}
	resp, ok := result.(*pointdomain.RankingResponse)
	if !ok || resp == nil {
		return nil, apperrors.New(50001, http.StatusInternalServerError, "排行榜加载失败")
	}

	// 写缓存（不含 myRank）
	if s.sessions != nil {
		payload := *resp
		payload.MyRank = nil
		if err := s.sessions.SetRankingCache(ctx, "points", session.AppID, rankingType, scope, page, limit, payload, ttl); err != nil {
			s.log.Warn("write points ranking cache failed", zap.Error(err), zap.String("type", rankingType), zap.Int64("appid", session.AppID))
		}
	}
	return resp, nil
}

// loadPointsRanking 加载积分/经验/等级排行（不走缓存）
func (s *PointsService) loadPointsRanking(ctx context.Context, rankingType string, session *authdomain.Session, page int, limit int) (*pointdomain.RankingResponse, error) {
	switch rankingType {
	case "integral":
		return s.pg.GetIntegralRankings(ctx, session.AppID, page, limit, session.UserID)
	case "experience":
		return s.pg.GetExperienceRankings(ctx, session.AppID, page, limit, session.UserID)
	case "level":
		return s.pg.GetLevelRankings(ctx, session.AppID, page, limit, session.UserID)
	default:
		return nil, apperrors.New(40009, http.StatusBadRequest, "不支持的排行类型")
	}
}

// loadMyPointsRank 单查"我自己的"排名（每用户独立，不缓存）
func (s *PointsService) loadMyPointsRank(ctx context.Context, rankingType string, session *authdomain.Session) (*pointdomain.RankingItem, error) {
	if session.UserID <= 0 {
		return nil, nil
	}
	switch rankingType {
	case "integral":
		return s.pg.GetMyIntegralRank(ctx, session.AppID, session.UserID)
	case "experience":
		return s.pg.GetMyExperienceRank(ctx, session.AppID, session.UserID)
	case "level":
		return s.pg.GetMyLevelRank(ctx, session.AppID, session.UserID)
	default:
		return nil, nil
	}
}

// pointsRankingScope 按类型返回缓存 scope + TTL（积分数据变化相对慢，TTL 可略长）
func (s *PointsService) pointsRankingScope(rankingType string) (string, time.Duration) {
	switch rankingType {
	case "integral":
		return "global", 60 * time.Second
	case "experience":
		return "global", 90 * time.Second
	case "level":
		return "global", 90 * time.Second
	default:
		return "global", 60 * time.Second
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

	// 集合化批量调整：单事务单语句完成全部用户的锁定/更新/记账。
	// 原实现按用户逐个开事务（1000 用户 = 1000 事务、数千次网络往返），
	// 现为 1 次往返；逐用户成败明细由 SQL 直接返回
	results, failures, err := s.pg.BatchAdjustUserIntegralByAdmin(ctx, userIDs, appID, signedAmount, reason, options)
	if err != nil {
		return nil, err
	}
	return &pointdomain.BatchIntegralAdjustResult{
		AppID:          appID,
		OperationType:  op,
		Amount:         amount,
		RequestedCount: len(userIDs),
		SuccessCount:   len(results),
		FailedCount:    len(failures),
		Results:        results,
		Failures:       failures,
	}, nil
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
