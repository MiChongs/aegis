package service

import (
	"context"
	"math"
	"net/http"
	"time"

	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

type SignInService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	publisher *event.Publisher
	location  *time.Location
}

func NewSignInService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher) *SignInService {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	return &SignInService{
		log:       log,
		pg:        pg,
		sessions:  sessions,
		publisher: publisher,
		location:  location,
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

	stats, err := s.pg.GetSignStats(ctx, session.UserID, session.AppID)
	if err != nil {
		return nil, err
	}
	if stats != nil && stats.LastSignDate == today {
		return nil, apperrors.New(40902, http.StatusConflict, "今日已签到")
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
			return nil, apperrors.New(40902, http.StatusConflict, "今日已签到")
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
	baseIntegral := int64(10)
	multiplier := 1.0
	bonusType := "normal"
	bonusDescription := "普通签到奖励"

	switch {
	case consecutiveDays >= 30:
		multiplier = 3
		bonusType = "monthly_master"
		bonusDescription = "月度签到达人，奖励翻3倍"
	case consecutiveDays >= 14:
		multiplier = 2.5
		bonusType = "half_month"
		bonusDescription = "半月坚持奖励，奖励2.5倍"
	case consecutiveDays >= 7:
		multiplier = 2
		bonusType = "weekly"
		bonusDescription = "一周连签奖励，奖励翻倍"
	case consecutiveDays >= 3:
		multiplier = 1.5
		bonusType = "streak"
		bonusDescription = "连续签到奖励，奖励1.5倍"
	}

	if weekday := now.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
		multiplier += 0.5
		bonusDescription += " + 周末奖励"
	}
	if now.Day() <= 3 {
		multiplier += 0.3
		bonusDescription += " + 月初奖励"
	}
	if now.Day() == 15 {
		multiplier += 0.5
		bonusDescription += " + 月中特殊奖励"
	}

	integralReward := int64(math.Floor(float64(baseIntegral) * multiplier))
	if integralReward < 1 {
		integralReward = 1
	}

	experienceReward := int64(20)
	if totalSignIns == 0 {
		experienceReward += 100
	}
	if consecutiveDays > 1 {
		bonus := int64((consecutiveDays - 1) * 2)
		if bonus > 80 {
			bonus = 80
		}
		experienceReward += bonus
	}
	if milestone := milestoneReward(consecutiveDays); milestone > 0 {
		experienceReward += milestone
	}
	if weekday := now.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
		experienceReward += 15
	}

	levelMultiplier, err := s.pg.GetExperienceMultiplier(ctx, user.Experience)
	if err != nil {
		return userdomain.SignInReward{}, err
	}
	experienceReward = int64(math.Floor(float64(experienceReward) * levelMultiplier))
	if experienceReward < 1 {
		experienceReward = 1
	}

	return userdomain.SignInReward{
		BaseIntegral:     baseIntegral,
		IntegralReward:   integralReward,
		ExperienceReward: experienceReward,
		RewardMultiplier: multiplier,
		BonusType:        bonusType,
		BonusDescription: bonusDescription,
	}, nil
}

func milestoneReward(consecutiveDays int) int64 {
	switch consecutiveDays {
	case 7:
		return 100
	case 14:
		return 250
	case 30:
		return 600
	case 90:
		return 2000
	case 365:
		return 10000
	default:
		return 0
	}
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
