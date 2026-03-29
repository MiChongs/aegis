package service

import (
	"aegis/internal/config"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

type AutoSignService struct {
	mu        sync.RWMutex
	cfg       config.AutoSignConfig
	log       *zap.Logger
	pg        *postgres.Repository
	schedules *redisrepo.AutoSignRepository
	signIn    *SignInService
	location  *time.Location
}

const (
	autoSignDeviceInfo = "Aegis AutoSign HyperNode"
	autoSignIPAddress  = "mesh://autosign.aegis/core"
	autoSignLocation   = "Aegis Chrono Mesh · Autonomous Relay"
)

func NewAutoSignService(cfg config.AutoSignConfig, log *zap.Logger, pg *postgres.Repository, schedules *redisrepo.AutoSignRepository, signIn *SignInService) *AutoSignService {
	location := timeutil.MustDefaultRuleLocation(cfg.Timezone)
	return &AutoSignService{
		cfg:       cfg,
		log:       log,
		pg:        pg,
		schedules: schedules,
		signIn:    signIn,
		location:  location,
	}
}

func (s *AutoSignService) Reload(cfg config.AutoSignConfig) {
	location := timeutil.MustDefaultRuleLocation(cfg.Timezone)
	s.mu.Lock()
	s.cfg = cfg
	s.location = location
	s.mu.Unlock()
}

func (s *AutoSignService) CurrentConfig() config.AutoSignConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *AutoSignService) runtime() (config.AutoSignConfig, *time.Location) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, s.location
}

func (s *AutoSignService) CatchUpOnStartup(ctx context.Context) (int, int, error) {
	cfg, _ := s.runtime()
	if !cfg.Enabled {
		return 0, 0, nil
	}
	scheduled, err := s.RebuildSchedule(ctx)
	if err != nil {
		return scheduled, 0, err
	}
	processed, err := s.RunDue(ctx)
	return scheduled, processed, err
}

func (s *AutoSignService) RebuildSchedule(ctx context.Context) (int, error) {
	cfg, location := s.runtime()
	if !cfg.Enabled {
		return 0, nil
	}
	lastUserID := int64(0)
	scheduled := 0
	for {
		items, err := s.pg.ListAutoSignCandidatesAfterUserID(ctx, lastUserID, cfg.BatchSize)
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
			if err := s.schedules.Schedule(ctx, item.AppID, item.UserID, s.initialDue(timeutil.Now().In(location), item.Time, location)); err != nil {
				return scheduled, err
			}
			scheduled++
		}
	}
}

func (s *AutoSignService) SyncUserSchedule(ctx context.Context, userID int64, appID int64) error {
	cfg, location := s.runtime()
	if !cfg.Enabled {
		return nil
	}
	item, err := s.pg.GetAutoSignCandidate(ctx, userID, appID)
	if err != nil {
		return err
	}
	if item == nil || !s.isEligible(*item) {
		return s.schedules.Remove(ctx, appID, userID)
	}
	return s.schedules.Schedule(ctx, appID, userID, s.initialDue(timeutil.Now().In(location), item.Time, location))
}

func (s *AutoSignService) RunDue(ctx context.Context) (int, error) {
	cfg, location := s.runtime()
	if !cfg.Enabled {
		return 0, nil
	}
	entries, err := s.schedules.GetDue(ctx, timeutil.Now().In(location), int64(cfg.BatchSize))
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, entry := range entries {
		if err := s.processEntry(ctx, entry); err != nil {
			s.log.Warn("auto sign process failed", zap.Int64("user_id", entry.UserID), zap.Int64("appid", entry.AppID), zap.Error(err))
			continue
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
	cfg, location := s.runtime()
	item, err := s.pg.GetAutoSignCandidate(ctx, entry.UserID, entry.AppID)
	if err != nil {
		return err
	}
	if item == nil || !s.isEligible(*item) {
		return s.schedules.Remove(ctx, entry.AppID, entry.UserID)
	}

	now := timeutil.Now().In(location)
	nextDue := s.nextDayDue(now.Add(time.Second), item.Time, location)
	result, err := s.signIn.SignInForUser(ctx, entry.UserID, entry.AppID, "auto", autoSignDeviceInfo, autoSignIPAddress, s.autoSignLocation(item))
	if err != nil {
		if appErr, ok := errors.AsType[*apperrors.AppError](err); ok && appErr.Code == 40902 {
			return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
		}
		if item.RetryOnFail {
			retryAt := now.Add(cfg.RetryDelay)
			return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, retryAt)
		}
		return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
	}
	if result != nil {
		s.log.Info("auto sign completed",
			zap.Int64("user_id", entry.UserID),
			zap.Int64("appid", entry.AppID),
			zap.String("sign_date", result.Record.SignDate),
			zap.Bool("already_signed", result.AlreadySigned),
			zap.Int64("integral_reward", result.Record.IntegralReward),
			zap.Int64("experience_reward", result.Record.ExperienceReward),
		)
	}
	return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
}

func (s *AutoSignService) isEligible(item userdomain.AutoSignCandidate) bool {
	return item.Enabled && item.SettingsEnabled
}

func (s *AutoSignService) initialDue(reference time.Time, timeValue string, location *time.Location) time.Time {
	hour, minute := s.parseScheduleTime(timeValue)
	due := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, location)
	if due.After(reference) {
		return due
	}
	return reference
}

func (s *AutoSignService) nextDayDue(reference time.Time, timeValue string, location *time.Location) time.Time {
	hour, minute := s.parseScheduleTime(timeValue)
	next := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, location)
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

func (s *AutoSignService) autoSignLocation(item *userdomain.AutoSignCandidate) string {
	if item != nil && item.DisableLocationTracking {
		return ""
	}
	return autoSignLocation
}
