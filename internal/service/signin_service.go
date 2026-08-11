package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type SignInService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	publisher *event.Publisher
	location  *time.Location
	inFlight  singleflight.Group
}

const (
	signInLockTTL      = 20 * time.Second
	signInWaitRetries  = 8
	signInWaitInterval = 250 * time.Millisecond
)

func NewSignInService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher) *SignInService {
	return &SignInService{
		log:       log,
		pg:        pg,
		sessions:  sessions,
		publisher: publisher,
		location:  timeutil.DefaultLocation(),
	}
}

func (s *SignInService) GetStatus(ctx context.Context, session *authdomain.Session) (*userdomain.SignInStatus, error) {
	user, err := s.requireActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}

	stats, err := s.pg.GetSignStats(ctx, session.UserID, session.AppID)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(s.location)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	consecutiveDays := 0
	todaySigned := false
	var lastSignAt *time.Time
	lastSignedDate := ""
	totalSignIns := int64(0)
	totalIntegral := int64(0)
	totalExperience := int64(0)
	if stats != nil {
		lastSignedDate = stats.LastSignDate
		lastSignAt = stats.LastSignAt
		totalSignIns = stats.TotalSignDays
		totalIntegral = stats.TotalIntegralReward
		totalExperience = stats.TotalExperienceReward
		switch stats.LastSignDate {
		case today:
			todaySigned = true
			consecutiveDays = stats.ConsecutiveDays
		case yesterday:
			consecutiveDays = stats.ConsecutiveDays
		}
	}

	nextStreak := consecutiveDays + 1
	if todaySigned {
		nextStreak = consecutiveDays
	}

	status := &userdomain.SignInStatus{
		TodaySigned:     todaySigned,
		SignDate:        today,
		ConsecutiveDays: consecutiveDays,
		TotalSignIns:    totalSignIns,
		TotalIntegral:   totalIntegral,
		TotalExperience: totalExperience,
		LastSignAt:      lastSignAt,
		LastSignedDate:  lastSignedDate,
	}
	currentReward, err := s.calculateReward(ctx, now, user, nextStreak, totalSignIns)
	if err != nil {
		return nil, err
	}
	status.CurrentReward = currentReward
	return status, nil
}

func (s *SignInService) SignIn(ctx context.Context, session *authdomain.Session, source, deviceInfo, ipAddress, location string) (*userdomain.SignInResult, error) {
	return s.signInInternal(ctx, session.UserID, session.AppID, source, deviceInfo, ipAddress, location)
}

func (s *SignInService) SignInForUser(ctx context.Context, userID int64, appID int64, source, deviceInfo, ipAddress, location string) (*userdomain.SignInResult, error) {
	return s.signInInternal(ctx, userID, appID, source, deviceInfo, ipAddress, location)
}

func (s *SignInService) ListHistory(ctx context.Context, session *authdomain.Session, page int, limit int) (*userdomain.SignHistoryResult, error) {
	if _, err := s.requireActiveUser(ctx, session); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.pg.ListDailySigns(ctx, session.UserID, session.AppID, page, limit)
	if err != nil {
		return nil, err
	}
	return &userdomain.SignHistoryResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcSignHistoryPages(total, limit),
	}, nil
}

func (s *SignInService) ExportHistory(ctx context.Context, session *authdomain.Session, query userdomain.SignHistoryExportQuery) ([]userdomain.DailySignIn, error) {
	if _, err := s.requireActiveUser(ctx, session); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListDailySignsForExport(ctx, session.UserID, session.AppID, limit)
}

func (s *SignInService) signInInternal(ctx context.Context, userID int64, appID int64, source, deviceInfo, ipAddress, location string) (*userdomain.SignInResult, error) {
	session := &authdomain.Session{UserID: userID, AppID: appID}
	user, err := s.requireActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(s.location)
	today := now.Format("2006-01-02")

	result, err, _ := s.inFlight.Do(signInFlightKey(session.UserID, session.AppID, today), func() (any, error) {
		return s.signInWithDistributedLock(context.WithoutCancel(ctx), session, user, now, today, source, deviceInfo, ipAddress, location)
	})
	if err != nil {
		return nil, err
	}
	signInResult, _ := result.(*userdomain.SignInResult)
	if signInResult == nil {
		return nil, fmt.Errorf("sign in result is nil")
	}
	return signInResult, nil
}

func (s *SignInService) signInWithDistributedLock(ctx context.Context, session *authdomain.Session, user *userdomain.User, now time.Time, today string, source, deviceInfo, ipAddress, location string) (*userdomain.SignInResult, error) {
	acquired, err := s.sessions.AcquireSignInLock(ctx, session.AppID, session.UserID, today, signInLockTTL)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return s.waitForExistingSignInResult(ctx, session.UserID, session.AppID, today)
	}
	defer func() {
		if err := s.sessions.ReleaseSignInLock(ctx, session.AppID, session.UserID, today); err != nil {
			s.log.Warn("release sign-in lock failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.String("signDate", today), zap.Error(err))
		}
	}()
	return s.signInOnce(ctx, session, user, now, today, source, deviceInfo, ipAddress, location)
}

func (s *SignInService) signInOnce(ctx context.Context, session *authdomain.Session, user *userdomain.User, now time.Time, today string, source, deviceInfo, ipAddress, location string) (*userdomain.SignInResult, error) {
	stats, err := s.pg.GetSignStats(ctx, session.UserID, session.AppID)
	if err != nil {
		return nil, err
	}
	if stats != nil && stats.LastSignDate == today {
		record, recordErr := s.pg.GetDailySignByDate(ctx, session.UserID, session.AppID, today)
		if recordErr != nil {
			return nil, recordErr
		}
		if record != nil {
			return s.loadExistingSignInResult(ctx, session.UserID, session.AppID, today, stats)
		}
		s.log.Warn("sign stats out of sync with daily sign record, recreating sign-in",
			zap.Int64("user_id", session.UserID),
			zap.Int64("appid", session.AppID),
			zap.String("sign_date", today),
		)
	}

	totalSignIns := int64(0)
	currentStreak := 0
	if stats != nil {
		totalSignIns = stats.TotalSignDays
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		if stats.LastSignDate == yesterday {
			currentStreak = stats.ConsecutiveDays
		}
	}

	reward, err := s.calculateReward(ctx, now, user, currentStreak+1, totalSignIns)
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = "manual"
	}
	result, err := s.pg.CreateDailySign(ctx, session.UserID, session.AppID, reward, now, source, deviceInfo, ipAddress, location)
	if err != nil {
		switch err {
		case pgrepo.ErrAlreadySigned:
			return s.loadExistingSignInResult(ctx, session.UserID, session.AppID, today, nil)
		case pgrepo.ErrUserNotFound:
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		default:
			return nil, err
		}
	}

	_ = s.sessions.DeleteMyView(ctx, session.AppID, session.UserID)
	_ = s.publisher.PublishJSON(ctx, event.SubjectUserSignedIn, map[string]any{
		"user_id":           session.UserID,
		"appid":             session.AppID,
		"token_jti":         session.TokenID,
		"sign_date":         result.Record.SignDate,
		"source":            result.Record.SignInSource,
		"consecutive_days":  result.Record.ConsecutiveDays,
		"integral_reward":   result.Record.IntegralReward,
		"experience_reward": result.Record.ExperienceReward,
	})
	s.log.Info("user signed in",
		zap.Int64("user_id", session.UserID),
		zap.Int64("appid", session.AppID),
		zap.String("sign_date", result.Record.SignDate),
		zap.Int("consecutive_days", result.Record.ConsecutiveDays),
		zap.Int64("integral_reward", result.Record.IntegralReward),
		zap.Int64("experience_reward", result.Record.ExperienceReward),
	)
	return result, nil
}

func (s *SignInService) waitForExistingSignInResult(ctx context.Context, userID int64, appID int64, signDate string) (*userdomain.SignInResult, error) {
	for i := 0; i < signInWaitRetries; i++ {
		result, err := s.loadExistingSignInResult(ctx, userID, appID, signDate, nil)
		if err == nil {
			return result, nil
		}
		if !isPendingSignInResult(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(signInWaitInterval):
		}
	}
	return nil, apperrors.New(40903, http.StatusConflict, "签到请求处理中，请稍后刷新结果")
}

func (s *SignInService) loadExistingSignInResult(ctx context.Context, userID int64, appID int64, signDate string, stats *userdomain.SignStats) (*userdomain.SignInResult, error) {
	record, err := s.pg.GetDailySignByDate(ctx, userID, appID, signDate)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, apperrors.New(40902, http.StatusConflict, "今日已签到")
	}
	if stats == nil {
		stats, err = s.pg.GetSignStats(ctx, userID, appID)
		if err != nil {
			return nil, err
		}
	}

	totalSignIns := int64(1)
	if stats != nil && stats.TotalSignDays > 0 {
		totalSignIns = stats.TotalSignDays
	}

	return &userdomain.SignInResult{
		Record: *record,
		Reward: userdomain.SignInReward{
			IntegralReward:   record.IntegralReward,
			ExperienceReward: record.ExperienceReward,
			RewardMultiplier: record.RewardMultiplier,
			BonusType:        record.BonusType,
			BonusDescription: record.BonusDescription,
		},
		TotalSignIns:  totalSignIns,
		AlreadySigned: true,
	}, nil
}

func signInFlightKey(userID int64, appID int64, signDate string) string {
	return fmt.Sprintf("signin:%d:%d:%s", appID, userID, signDate)
}

func isPendingSignInResult(err error) bool {
	if err == nil {
		return false
	}
	if appErr, ok := err.(*apperrors.AppError); ok {
		return appErr.Code == 40902
	}
	return false
}

func (s *SignInService) requireActiveUser(ctx context.Context, session *authdomain.Session) (*userdomain.User, error) {
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != session.AppID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if !user.Enabled {
		return nil, apperrors.New(40301, http.StatusForbidden, "用户账户已被禁用")
	}
	if user.DisabledEndTime != nil && user.DisabledEndTime.After(time.Now()) {
		return nil, apperrors.New(40302, http.StatusForbidden, "用户账户暂时被冻结")
	}
	return user, nil
}

func (s *SignInService) calculateReward(ctx context.Context, now time.Time, user *userdomain.User, consecutiveDays int, totalSignIns int64) (userdomain.SignInReward, error) {
	app, err := s.pg.GetAppByID(ctx, user.AppID)
	if err != nil {
		return userdomain.SignInReward{}, err
	}
	policy := defaultSignInRewardPolicy()
	if app != nil {
		policy = resolveSignInRewardPolicy(&appdomain.App{
			ID:       app.ID,
			Name:     app.Name,
			AppKey:   app.AppKey,
			Settings: app.Settings,
		})
	}
	resolved, _, _, err := calculateSignInRewardWithPolicy(ctx, s.pg, policy, now, user.Experience, consecutiveDays, totalSignIns)
	if err != nil {
		return userdomain.SignInReward{}, err
	}
	return toUserSignInReward(resolved), nil
}

func calcSignHistoryPages(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}
