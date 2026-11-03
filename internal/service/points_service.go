package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/leaderboard"
	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type PointsService struct {
	// publicIDs 排行榜上的 userId 换成对外 ID
	publicIDs *UserPublicIDs
	log       *zap.Logger
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	location  *time.Location
	// 排行榜并发合并：多个用户同时请求同一榜单页时只打一次 DB
	rankingFlight singleflight.Group
}

func NewPointsService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *PointsService {
	return &PointsService{log: log, pg: pg, sessions: sessions, location: timeutil.DefaultLocation(), publicIDs: newUserPublicIDsFor(pg)}
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

// ListLevels 应用的全部等级（docs/levels.md）；userID 不为 0 时标出已达到的等级、按体系隐藏未达到的。
func (s *PointsService) ListLevels(ctx context.Context, appID, userID int64) ([]pointdomain.LevelConfig, error) {
	return s.pg.ListLevelConfigs(ctx, appID, userID)
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

// GetRankings 旧版排行榜。account 一律脱敏后返回（字段名不变，兼容老客户端）：
// 账号可能就是手机号、邮箱，排行榜是给所有人看的。规则同新排行榜（docs/leaderboard.md「身份展示」）。
func (s *PointsService) GetRankings(ctx context.Context, session *authdomain.Session, rankingType string, page int, limit int) (*pointdomain.RankingResponse, error) {
	resp, err := s.getRankings(ctx, session, rankingType, page, limit)
	if err != nil || resp == nil {
		return resp, err
	}
	out := maskRankingAccounts(resp)
	publicizeRankingUsers(ctx, s.publicIDs, out)
	return out, nil
}

// SetPublicIDs 接上全进程共用的对外 ID 换算。
func (s *PointsService) SetPublicIDs(publicIDs *UserPublicIDs) {
	s.publicIDs = publicIDs
}

// maskRankingAccounts 返回一份账号已脱敏的副本：singleflight 的结果在并发请求之间共享，不能原地改。
func maskRankingAccounts(resp *pointdomain.RankingResponse) *pointdomain.RankingResponse {
	out := *resp
	out.Items = make([]pointdomain.RankingItem, len(resp.Items))
	for i, item := range resp.Items {
		item.Account = leaderboard.MaskAccount(item.Account)
		out.Items[i] = item
	}
	if resp.MyRank != nil {
		my := *resp.MyRank
		my.Account = leaderboard.MaskAccount(my.Account)
		out.MyRank = &my
	}
	return &out
}

// publicizeRankingUsers 在 maskRankingAccounts 的副本上把 userId 换成对外 ID（docs/user-public-id.md）。
// 换不到的（刚注销）写 0，不把主键留在榜上。
func publicizeRankingUsers(ctx context.Context, publicIDs *UserPublicIDs, resp *pointdomain.RankingResponse) {
	ids := make([]int64, 0, len(resp.Items)+1)
	for _, item := range resp.Items {
		ids = append(ids, item.UserID)
	}
	if resp.MyRank != nil {
		ids = append(ids, resp.MyRank.UserID)
	}
	public := publicIDs.Map(ctx, ids)
	for i := range resp.Items {
		resp.Items[i].UserID = public[resp.Items[i].UserID]
	}
	if resp.MyRank != nil {
		resp.MyRank.UserID = public[resp.MyRank.UserID]
	}
}

func (s *PointsService) getRankings(ctx context.Context, session *authdomain.Session, rankingType string, page int, limit int) (*pointdomain.RankingResponse, error) {
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

// ── 积分 / 经验调整 ──
//
// 管理端入口（AdminAdjust*）在内部入口的基础上多校验单次数量上限与请求编号；
// 内部入口（AdjustUser*，抽奖、云函数等调用）只校验非零与原因长度。
// 两者都落到同一组仓储函数，余额不足、超过上限、int64 溢出在锁行之后统一判定。

// AdminAdjustIntegral 管理员调整积分（可加可减）。
func (s *PointsService) AdminAdjustIntegral(ctx context.Context, in pointdomain.AdjustInput, options pointdomain.AdminAdjustOptions) (*pointdomain.IntegralAdjustResult, error) {
	in.Kind = pointdomain.KindIntegral
	if err := in.Validate(); err != nil {
		return nil, mapPointsAdjustError(in.Kind, err)
	}
	result, err := s.pg.AdminAdjustIntegral(ctx, in, options)
	if err != nil {
		return nil, mapPointsAdjustError(in.Kind, err)
	}
	return result, nil
}

// AdminAdjustExperience 管理员调整经验：正数增加，负数扣减。扣减时 in.LevelMode 决定等级怎么处理
// （keep 保留现有等级，recalc 按扣减后的经验重新判定、可以降级）；已发的等级奖励都不收回。
func (s *PointsService) AdminAdjustExperience(ctx context.Context, in pointdomain.AdjustInput, options pointdomain.AdminAdjustOptions) (*pointdomain.ExperienceAdjustResult, error) {
	in.Kind = pointdomain.KindExperience
	if err := in.Validate(); err != nil {
		return nil, mapPointsAdjustError(in.Kind, err)
	}
	result, err := s.pg.AdminAdjustExperience(ctx, in, options)
	if err != nil {
		return nil, mapPointsAdjustError(in.Kind, err)
	}
	return result, nil
}

// AdjustUserIntegral 内部调整积分（抽奖消耗与奖励、云函数等），不受管理端单次上限约束。
func (s *PointsService) AdjustUserIntegral(ctx context.Context, userID int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.IntegralAdjustResult, error) {
	in, err := internalAdjustInput(pointdomain.KindIntegral, userID, appID, amount, reason)
	if err != nil {
		return nil, mapPointsAdjustError(pointdomain.KindIntegral, err)
	}
	result, err := s.pg.AdminAdjustIntegral(ctx, in, options)
	if err != nil {
		return nil, mapPointsAdjustError(in.Kind, err)
	}
	return result, nil
}

// AdjustUserExperience 内部增加经验，数量必须为正（云函数等自动路径不能扣经验，扣减只开放给管理员）。
func (s *PointsService) AdjustUserExperience(ctx context.Context, userID int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.ExperienceAdjustResult, error) {
	if amount < 0 {
		return nil, mapPointsAdjustError(pointdomain.KindExperience, pgrepo.ErrExperienceDeduction)
	}
	in, err := internalAdjustInput(pointdomain.KindExperience, userID, appID, amount, reason)
	if err != nil {
		return nil, mapPointsAdjustError(pointdomain.KindExperience, err)
	}
	result, err := s.pg.AdminAdjustExperience(ctx, in, options)
	if err != nil {
		return nil, mapPointsAdjustError(in.Kind, err)
	}
	return result, nil
}

func internalAdjustInput(kind pointdomain.Kind, userID, appID, amount int64, reason string) (pointdomain.AdjustInput, error) {
	in := pointdomain.AdjustInput{Kind: kind, AppID: appID, UserID: userID, Amount: amount}
	if appID <= 0 {
		return in, &pointdomain.ValidationError{Message: "应用ID不能为空"}
	}
	if userID <= 0 {
		return in, &pointdomain.ValidationError{Message: "用户ID不能为空"}
	}
	if amount == 0 {
		return in, &pointdomain.ValidationError{Message: "调整数量不能为0"}
	}
	// 内部调用的原因由代码拼出来（如「抽奖奖励: 奖品名」），超长就截断，不因此让业务失败
	in.Reason = truncateRunes(strings.TrimSpace(reason), pointdomain.MaxAdjustReasonRunes)
	return in, nil
}

// BatchAdjustUserIntegral 管理员批量调整积分。amount 为正数，方向由 operationType（add / consume）决定。
func (s *PointsService) BatchAdjustUserIntegral(ctx context.Context, userIDs []int64, appID int64, amount int64, operationType string, reason string, requestID string, options pointdomain.AdminAdjustOptions) (*pointdomain.BatchIntegralAdjustResult, error) {
	kind := pointdomain.KindIntegral
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "缺少应用 ID（appid）")
	}
	ids := uniquePositiveIDs(userIDs)
	if len(ids) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户 ID 列表不能为空")
	}
	if len(ids) > pointdomain.MaxAdminBatchUsers {
		return nil, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("批量操作最多支持 %d 个用户", pointdomain.MaxAdminBatchUsers))
	}
	if amount <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "调整数量必须大于 0，扣减请使用 operation_type=consume")
	}
	if err := pointdomain.ValidateAmount(kind, amount); err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}
	reason, err := pointdomain.NormalizeReason(reason)
	if err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}
	requestID, err = pointdomain.NormalizeRequestID(requestID)
	if err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}

	op := strings.ToLower(strings.TrimSpace(operationType))
	if op == "" {
		op = "add"
	}
	if op != "add" && op != "consume" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "操作类型必须为 add 或 consume")
	}
	signedAmount := amount
	if op == "consume" {
		signedAmount = -amount
	}

	results, failures, replayed, err := s.pg.AdminBatchAdjustIntegral(ctx, appID, ids, signedAmount, reason, requestID, options)
	if err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}
	if results == nil {
		results = []pointdomain.IntegralAdjustResult{}
	}
	if failures == nil {
		failures = []pointdomain.BatchAdjustFailure{}
	}
	return &pointdomain.BatchIntegralAdjustResult{
		AppID:          appID,
		OperationType:  op,
		Amount:         amount,
		RequestedCount: len(ids),
		SuccessCount:   len(results),
		FailedCount:    len(failures),
		Results:        results,
		Failures:       failures,
		Replayed:       replayed,
	}, nil
}

// BatchAdjustUserExperience 管理员批量调整经验。amount 为正数，方向由 operationType（add / consume）决定；
// 扣减时 levelMode 决定等级处理。逐人在自己的事务里执行（等级要逐人判定），一个人失败不影响其他人；
// 带请求编号时每人的编号为「编号-用户ID」，整批重试只会补上没成功的人。
func (s *PointsService) BatchAdjustUserExperience(ctx context.Context, userIDs []int64, appID int64, amount int64, operationType string,
	reason string, requestID string, levelMode string, options pointdomain.AdminAdjustOptions) (*pointdomain.BatchExperienceAdjustResult, error) {
	kind := pointdomain.KindExperience
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "缺少应用 ID（appid）")
	}
	ids := uniquePositiveIDs(userIDs)
	if len(ids) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户 ID 列表不能为空")
	}
	if len(ids) > pointdomain.MaxAdminBatchUsers {
		return nil, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("批量操作最多支持 %d 个用户", pointdomain.MaxAdminBatchUsers))
	}
	if amount <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "调整数量必须大于 0，扣减请使用 operation_type=consume")
	}
	if err := pointdomain.ValidateAmount(kind, amount); err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}
	mode, err := pointdomain.NormalizeLevelMode(levelMode)
	if err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}
	requestID, err = pointdomain.NormalizeRequestID(requestID)
	if err != nil {
		return nil, mapPointsAdjustError(kind, err)
	}
	op := strings.ToLower(strings.TrimSpace(operationType))
	if op == "" {
		op = "add"
	}
	if op != "add" && op != "consume" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "操作类型必须为 add 或 consume")
	}
	signed := amount
	if op == "consume" {
		signed = -amount
	}
	out := &pointdomain.BatchExperienceAdjustResult{
		AppID: appID, OperationType: op, Amount: amount, RequestedCount: len(ids),
		Results: []pointdomain.ExperienceAdjustResult{}, Failures: []pointdomain.BatchAdjustFailure{},
	}
	if op == "consume" {
		out.LevelMode = mode
	}
	for _, id := range ids {
		in := pointdomain.AdjustInput{Kind: kind, AppID: appID, UserID: id, Amount: signed, Reason: reason, LevelMode: mode}
		if requestID != "" {
			// 编号最长 64 位：超出时用摘要
			perUser := fmt.Sprintf("%s-%d", requestID, id)
			if len(perUser) > 64 {
				sum := sha256.Sum256([]byte(perUser))
				perUser = "b_" + hex.EncodeToString(sum[:20])
			}
			in.RequestID = perUser
		}
		result, err := s.AdminAdjustExperience(ctx, in, options)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			out.Failures = append(out.Failures, pointdomain.BatchAdjustFailure{UserID: id, Error: err.Error()})
			continue
		}
		out.Results = append(out.Results, *result)
	}
	out.SuccessCount, out.FailedCount = len(out.Results), len(out.Failures)
	return out, nil
}

// mapPointsAdjustError 把仓储与校验错误翻译成带业务码的错误，提示里带上当前余额。
func mapPointsAdjustError(kind pointdomain.Kind, err error) error {
	var validation *pointdomain.ValidationError
	var balance *pgrepo.BalanceError
	switch {
	case errors.As(err, &validation):
		return apperrors.New(40000, http.StatusBadRequest, validation.Message)
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在或不属于该应用")
	case errors.Is(err, pgrepo.ErrExperienceDeduction):
		return apperrors.New(40000, http.StatusBadRequest, "自动发放的经验不能为负数，扣减经验请由管理员调整")
	case errors.Is(err, pgrepo.ErrRequestIDConflict):
		return apperrors.New(40921, http.StatusConflict, "请求编号已用于另一笔调整，请刷新后重试")
	case errors.As(err, &balance) && errors.Is(err, pointdomain.ErrBalanceOverflow):
		return apperrors.New(40961, http.StatusBadRequest,
			fmt.Sprintf("调整后%s将超过上限 %s（当前 %s）", balance.Kind.Label(),
				pointdomain.GroupDigits(pointdomain.MaxBalance), pointdomain.GroupDigits(balance.Balance)))
	case errors.As(err, &balance):
		return apperrors.New(40009, http.StatusBadRequest,
			fmt.Sprintf("用户%s不足（当前 %s）", balance.Kind.Label(), pointdomain.GroupDigits(balance.Balance)))
	case errors.Is(err, pgrepo.ErrInsufficientIntegral):
		return apperrors.New(40009, http.StatusBadRequest, fmt.Sprintf("用户%s不足", kind.Label()))
	default:
		return err
	}
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
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
