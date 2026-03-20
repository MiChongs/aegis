package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aegis/internal/config"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

type AutoSignService struct {
	cfg       config.AutoSignConfig
	log       *zap.Logger
	pg        *postgres.Repository
	schedules *redisrepo.AutoSignRepository
	signIn    *SignInService
	location  *time.Location
}

func NewAutoSignService(cfg config.AutoSignConfig, log *zap.Logger, pg *postgres.Repository, schedules *redisrepo.AutoSignRepository, signIn *SignInService) *AutoSignService {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	return &AutoSignService{
		cfg:       cfg,
		log:       log,
		pg:        pg,
		schedules: schedules,
		signIn:    signIn,
		location:  location,
	}
}

func (s *AutoSignService) RebuildSchedule(ctx context.Context) (int, error) {
	if !s.cfg.Enabled {
		return 0, nil
	}
	lastUserID := int64(0)
	scheduled := 0
	for {
		items, err := s.pg.ListAutoSignCandidatesAfterUserID(ctx, lastUserID, s.cfg.BatchSize)
		if err != nil {
			return scheduled, err
		}
		if len(items) == 0 {
			return scheduled, nil
		}
		for _, item := range items {
			lastUserID = item.UserID
			if !s.isEligible(item) {
				_ = s.schedules.Remove(ctx, item.AppID, item.UserID)
				continue
			}
			if err := s.schedules.Schedule(ctx, item.AppID, item.UserID, s.initialDue(time.Now().In(s.location), item.Time)); err != nil {
				return scheduled, err
			}
			scheduled++
		}
	}
}

func (s *AutoSignService) SyncUserSchedule(ctx context.Context, userID int64, appID int64) error {
	if !s.cfg.Enabled {
		return nil
	}
	item, err := s.pg.GetAutoSignCandidate(ctx, userID, appID)
	if err != nil {
		return err
	}
	if item == nil || !s.isEligible(*item) {
		return s.schedules.Remove(ctx, appID, userID)
	}
	return s.schedules.Schedule(ctx, appID, userID, s.initialDue(time.Now().In(s.location), item.Time))
}

func (s *AutoSignService) RunDue(ctx context.Context) (int, error) {
	if !s.cfg.Enabled {
		return 0, nil
	}
	entries, err := s.schedules.GetDue(ctx, time.Now().In(s.location), int64(s.cfg.BatchSize))
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, entry := range entries {
		if err := s.processEntry(ctx, entry); err != nil {
			s.log.Warn("auto sign process failed", zap.Int64("user_id", entry.UserID), zap.Int64("appid", entry.AppID), zap.Error(err))
		}
		processed++
	}
	return processed, nil
}

func (s *AutoSignService) ScheduledCount(ctx context.Context) int64 {
	count, err := s.schedules.Count(ctx)
	if err != nil {
		return 0
	}
	return count
}

func (s *AutoSignService) processEntry(ctx context.Context, entry redisrepo.AutoSignEntry) error {
	item, err := s.pg.GetAutoSignCandidate(ctx, entry.UserID, entry.AppID)
	if err != nil {
		return err
	}
	if item == nil || !s.isEligible(*item) {
		return s.schedules.Remove(ctx, entry.AppID, entry.UserID)
	}

	now := time.Now().In(s.location)
	nextDue := s.nextDayDue(now.Add(time.Second), item.Time)
	_, err = s.signIn.SignInForUser(ctx, entry.UserID, entry.AppID, "auto", "AegisAutoSignWorker", "", "")
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) && appErr.Code == 40902 {
			return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
		}
		if item.RetryOnFail {
			retryAt := now.Add(s.cfg.RetryDelay)
			return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, retryAt)
		}
		return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
	}
	return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
}

func (s *AutoSignService) isEligible(item userdomain.AutoSignCandidate) bool {
	return item.Enabled && item.SettingsEnabled && isPermanentVIP(item.VIPExpireAt)
}

func (s *AutoSignService) initialDue(reference time.Time, timeValue string) time.Time {
	hour, minute := s.parseScheduleTime(timeValue)
	due := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, s.location)
	if due.After(reference) {
		return due
	}
	return reference
}

func (s *AutoSignService) nextDayDue(reference time.Time, timeValue string) time.Time {
	hour, minute := s.parseScheduleTime(timeValue)
	next := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, s.location)
	if !next.After(reference) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func (s *AutoSignService) parseScheduleTime(timeValue string) (int, int) {
	hour, minute := 0, 0
	if _, err := fmt.Sscanf(timeValue, "%d:%d", &hour, &minute); err != nil {
		hour, minute = 0, 0
	}
	return hour, minute
}

func isPermanentVIP(value *time.Time) bool {
	return value != nil && value.Year() >= 2099
}
