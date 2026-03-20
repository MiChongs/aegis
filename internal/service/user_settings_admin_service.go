package service

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	apperrors "aegis/pkg/errors"
)

func (s *UserService) GetAdminSettingsStats(ctx context.Context, appID int64) (*userdomain.AdminSettingsStatsResult, error) {
	totalUsers, err := s.pg.CountUsersByApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	categories := s.ListSettingCategories()
	stats := make(map[string]userdomain.SettingCoverageStat, len(categories))
	totalCoverage := 0.0
	for _, category := range categories {
		usersWithSettings, err := s.pg.CountUsersWithActiveSettingByCategory(ctx, appID, category)
		if err != nil {
			return nil, err
		}
		coverage := 0.0
		if totalUsers > 0 {
			coverage = roundFloat(float64(usersWithSettings) / float64(totalUsers) * 100)
		}
		stats[category] = userdomain.SettingCoverageStat{
			UsersWithSettings:    usersWithSettings,
			UsersWithoutSettings: totalUsers - usersWithSettings,
			Coverage:             coverage,
		}
		totalCoverage += coverage
	}
	recentSettings, err := s.pg.ListRecentUserSettingsByApp(ctx, appID, 10)
	if err != nil {
		return nil, err
	}
	avgCoverage := 0.0
	if len(categories) > 0 {
		avgCoverage = roundFloat(totalCoverage / float64(len(categories)))
	}
	return &userdomain.AdminSettingsStatsResult{
		AppID:          appID,
		TotalUsers:     totalUsers,
		SettingsStats:  stats,
		RecentSettings: recentSettings,
		Categories:     categories,
		Summary: userdomain.AdminSettingsStatsSummary{
			TotalCategories: len(categories),
			AvgCoverage:     avgCoverage,
		},
	}, nil
}

func (s *UserService) GetAdminUserSettings(ctx context.Context, appID int64, userID int64) (*userdomain.AdminUserSettingsView, error) {
	user, err := s.GetAdminUser(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	records, err := s.pg.ListUserSettingRecordsByUser(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	recordMap := make(map[string]userdomain.SettingRecord, len(records))
	recordInfo := make(map[string]userdomain.AdminUserSettingRecordInfo, len(records))
	configured := make([]string, 0, len(records))
	for _, record := range records {
		if !record.IsActive {
			continue
		}
		recordMap[record.Category] = record
		recordInfo[record.Category] = userdomain.AdminUserSettingRecordInfo{
			Version:   record.Version,
			CreatedAt: record.CreatedAt,
			UpdatedAt: record.UpdatedAt,
			IsActive:  record.IsActive,
		}
		configured = append(configured, record.Category)
	}
	settings := make(map[string]userdomain.Settings, len(s.ListSettingCategories()))
	missing := make([]string, 0)
	for _, category := range s.ListSettingCategories() {
		if record, ok := recordMap[category]; ok {
			settings[category] = userdomain.Settings{
				UserID:    record.UserID,
				Category:  record.Category,
				Settings:  deepCloneMap(record.Settings),
				Version:   record.Version,
				IsActive:  record.IsActive,
				CreatedAt: record.CreatedAt,
				UpdatedAt: record.UpdatedAt,
			}
			continue
		}
		missing = append(missing, category)
		settings[category] = userdomain.Settings{
			UserID:    userID,
			Category:  category,
			Settings:  deepCloneMap(defaultSettings(category)),
			Version:   1,
			IsActive:  true,
			UpdatedAt: time.Now().UTC(),
		}
	}
	return &userdomain.AdminUserSettingsView{
		User: userdomain.AdminUserBasic{
			ID:       user.ID,
			Account:  user.Account,
			Nickname: user.Nickname,
			Avatar:   user.Avatar,
			Email:    user.Email,
		},
		Settings:             settings,
		RecordInfo:           recordInfo,
		Categories:           s.ListSettingCategories(),
		ConfiguredCategories: configured,
		MissingCategories:    missing,
	}, nil
}

func (s *UserService) InitializeUserSettingsAdmin(ctx context.Context, appID int64, userID int64, categories []string) (*userdomain.SettingsInitializeResult, error) {
	if _, err := s.GetAdminUser(ctx, appID, userID); err != nil {
		return nil, err
	}
	normalized := normalizeSettingCategories(categories, s.ListSettingCategories())
	initialized, skipped, err := s.ensureUserSettings(ctx, appID, userID, normalized)
	if err != nil {
		return nil, err
	}
	return &userdomain.SettingsInitializeResult{
		AppID:                 appID,
		UserIDs:               []int64{userID},
		Categories:            normalized,
		ProcessedUsers:        1,
		InitializedCategories: initialized,
		SkippedExisting:       skipped,
	}, nil
}

func (s *UserService) BatchInitializeSettingsAdmin(ctx context.Context, appID int64, batchSize int, categories []string) (*userdomain.SettingsInitializeResult, error) {
	if batchSize <= 0 {
		batchSize = 50
	}
	if batchSize > 200 {
		batchSize = 200
	}
	users, err := s.pg.ListAdminUsersForExport(ctx, appID, "", nil, batchSize)
	if err != nil {
		return nil, err
	}
	normalized := normalizeSettingCategories(categories, s.ListSettingCategories())
	result := &userdomain.SettingsInitializeResult{
		AppID:      appID,
		UserIDs:    make([]int64, 0, len(users)),
		Categories: normalized,
	}
	for _, item := range users {
		initialized, skipped, err := s.ensureUserSettings(ctx, appID, item.ID, normalized)
		if err != nil {
			return nil, err
		}
		result.ProcessedUsers++
		result.UserIDs = append(result.UserIDs, item.ID)
		result.InitializedCategories += initialized
		result.SkippedExisting += skipped
	}
	return result, nil
}

func (s *UserService) CheckAndRepairSettings(ctx context.Context, appID int64, autoRepair bool) (*userdomain.SettingsIntegrityResult, error) {
	records, err := s.pg.ListUserSettingRecordsByApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	result := &userdomain.SettingsIntegrityResult{
		AppID:      appID,
		Issues:     make([]userdomain.SettingIntegrityIssue, 0),
		Repairs:    make([]userdomain.SettingIntegrityRepair, 0),
		AutoRepair: autoRepair,
	}
	for _, record := range records {
		if !record.IsActive {
			continue
		}
		defaults := defaultSettings(record.Category)
		if len(defaults) == 0 {
			continue
		}
		missing := findMissingKeys(record.Settings, defaults, "")
		if len(missing) == 0 {
			continue
		}
		result.Issues = append(result.Issues, userdomain.SettingIntegrityIssue{
			UserID:      record.UserID,
			Category:    record.Category,
			MissingKeys: missing,
			SettingID:   record.ID,
		})
		if autoRepair {
			merged := mergeWithDefaults(record.Settings, defaults)
			if err := s.pg.UpsertUserSettings(ctx, userdomain.Settings{
				UserID:    record.UserID,
				Category:  record.Category,
				Settings:  merged,
				Version:   record.Version + 1,
				IsActive:  true,
				UpdatedAt: time.Now().UTC(),
			}); err != nil {
				return nil, err
			}
			s.invalidateAdminUserCaches(ctx, appID, record.UserID)
			if record.Category == "autoSign" && s.publisher != nil {
				_ = s.publisher.PublishJSON(ctx, event.SubjectUserAutoSignSync, map[string]any{
					"user_id": record.UserID,
					"appid":   appID,
				})
			}
			result.Repairs = append(result.Repairs, userdomain.SettingIntegrityRepair{
				UserID:       record.UserID,
				Category:     record.Category,
				RepairedKeys: missing,
			})
		}
	}
	result.TotalIssues = len(result.Issues)
	return result, nil
}

func (s *UserService) CleanupInvalidSettingsAdmin(ctx context.Context, appID int64, dryRun bool) (*userdomain.SettingsCleanupResult, error) {
	invalid, err := s.pg.ListInvalidUserSettingsByApp(ctx, appID, 10000)
	if err != nil {
		return nil, err
	}
	result := &userdomain.SettingsCleanupResult{
		AppID:           appID,
		FoundInvalid:    len(invalid),
		DryRun:          dryRun,
		InvalidSettings: invalid,
	}
	if dryRun || len(invalid) == 0 {
		return result, nil
	}
	ids := make([]int64, 0, len(invalid))
	affectedUsers := make(map[int64]struct{}, len(invalid))
	for _, item := range invalid {
		ids = append(ids, item.ID)
		if item.UserID > 0 {
			affectedUsers[item.UserID] = struct{}{}
		}
	}
	cleaned, err := s.pg.DeleteUserSettingsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result.Cleaned = cleaned
	for userID := range affectedUsers {
		s.invalidateAdminUserCaches(ctx, appID, userID)
	}
	return result, nil
}

func (s *UserService) ensureUserSettings(ctx context.Context, appID int64, userID int64, categories []string) (int, int, error) {
	initialized := 0
	skipped := 0
	for _, category := range categories {
		current, err := s.pg.GetUserSettings(ctx, userID, category)
		if err != nil {
			return 0, 0, err
		}
		if current != nil && current.IsActive {
			skipped++
			continue
		}
		if err := s.pg.UpsertUserSettings(ctx, userdomain.Settings{
			UserID:    userID,
			Category:  category,
			Settings:  deepCloneMap(defaultSettings(category)),
			Version:   1,
			IsActive:  true,
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			return 0, 0, err
		}
		s.invalidateAdminUserCaches(ctx, appID, userID)
		if category == "autoSign" && s.publisher != nil {
			_ = s.publisher.PublishJSON(ctx, event.SubjectUserAutoSignSync, map[string]any{
				"user_id": userID,
				"appid":   appID,
			})
		}
		initialized++
	}
	return initialized, skipped, nil
}

func normalizeSettingCategories(input []string, valid []string) []string {
	if len(input) == 0 {
		return append([]string(nil), valid...)
	}
	validSet := make(map[string]struct{}, len(valid))
	for _, category := range valid {
		validSet[category] = struct{}{}
	}
	result := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, item := range input {
		category := strings.TrimSpace(item)
		if category == "" {
			continue
		}
		if _, ok := validSet[category]; !ok {
			continue
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		result = append(result, category)
	}
	if len(result) == 0 {
		return append([]string(nil), valid...)
	}
	return result
}

func findMissingKeys(current map[string]any, defaults map[string]any, prefix string) []string {
	missing := make([]string, 0)
	for key, defaultValue := range defaults {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		currentValue, ok := current[key]
		if !ok {
			missing = append(missing, fullKey)
			continue
		}
		defaultMap, defaultIsMap := defaultValue.(map[string]any)
		currentMap, currentIsMap := currentValue.(map[string]any)
		if defaultIsMap {
			if !currentIsMap {
				missing = append(missing, fullKey)
				continue
			}
			missing = append(missing, findMissingKeys(currentMap, defaultMap, fullKey)...)
		}
	}
	return missing
}

func mergeWithDefaults(current map[string]any, defaults map[string]any) map[string]any {
	result := deepCloneMap(defaults)
	for key, value := range current {
		defaultMap, defaultIsMap := result[key].(map[string]any)
		valueMap, valueIsMap := value.(map[string]any)
		if defaultIsMap && valueIsMap {
			result[key] = mergeWithDefaults(valueMap, defaultMap)
			continue
		}
		result[key] = deepCloneValue(value)
	}
	return result
}

func deepCloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = deepCloneValue(value)
	}
	return result
}

func deepCloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCloneMap(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = deepCloneValue(item)
		}
		return result
	default:
		return typed
	}
}

func roundFloat(value float64) float64 {
	return math.Round(value*100) / 100
}

func ensureAdminAppID(appID int64) error {
	if appID <= 0 {
		return apperrors.New(40020, http.StatusBadRequest, "应用标识不能为空")
	}
	return nil
}
