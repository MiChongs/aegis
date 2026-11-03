package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/levels"
	lotterydomain "aegis/internal/domain/lottery"
	pointdomain "aegis/internal/domain/points"
)

// 等级体系（docs/levels.md）：每个应用一套，发布后缓存在本实例（30 秒，发布时本实例立即失效）。
// 没有发布过的应用使用默认体系（由旧版全站等级表转换，与重构前完全一致）。

var (
	ErrLevelSchemeConflict = errors.New("level: draft was changed by someone else")
	ErrLevelVersionMissing = errors.New("level: scheme version not found")
	ErrLevelRewardMissing  = errors.New("level: reward record not found")
	ErrLevelRewardState    = errors.New("level: reward record is not in a state that allows this")
	ErrLevelJobBusy        = errors.New("level: a job is already running for this app")
	ErrLevelJobMissing     = errors.New("level: job not found")
)

const levelSchemeTTL = 30 * time.Second

type levelSchemeEntry struct {
	scheme  *levels.Scheme
	version int
	expires time.Time
}

type levelSchemeCache struct {
	mu       sync.RWMutex
	entries  map[int64]levelSchemeEntry
	fallback *levelSchemeEntry
	// kick 有升级或待发奖励写入后通知后台循环（由服务层注册）
	kick func()
}

// SetLevelWorkHook 有待发奖励或待发通知时调用（服务层用它唤醒后台循环）。
func (r *Repository) SetLevelWorkHook(kick func()) {
	r.levels.mu.Lock()
	r.levels.kick = kick
	r.levels.mu.Unlock()
}

func (r *Repository) levelWorkQueued() {
	r.levels.mu.RLock()
	kick := r.levels.kick
	r.levels.mu.RUnlock()
	if kick != nil {
		// 写入它的事务这时还没提交：稍等再唤醒，免得后台先扫到空
		time.AfterFunc(400*time.Millisecond, kick)
	}
}

// InvalidateLevelScheme 发布之后让本实例立即换上新体系。
func (r *Repository) InvalidateLevelScheme(appID int64) {
	r.levels.mu.Lock()
	delete(r.levels.entries, appID)
	r.levels.mu.Unlock()
}

// LevelScheme 应用当前生效的等级体系与版本号（0 为默认体系）。
func (r *Repository) LevelScheme(ctx context.Context, appID int64) (*levels.Scheme, int, error) {
	now := time.Now()
	r.levels.mu.RLock()
	entry, ok := r.levels.entries[appID]
	r.levels.mu.RUnlock()
	if ok && now.Before(entry.expires) {
		return entry.scheme, entry.version, nil
	}

	var raw []byte
	var version int
	err := r.pool.QueryRow(ctx, `SELECT published_graph, published_version FROM level_schemes WHERE appid = $1`, appID).
		Scan(&raw, &version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, err
	}
	var scheme *levels.Scheme
	if len(raw) > 0 && version > 0 {
		var graph levels.Graph
		if err := json.Unmarshal(raw, &graph); err == nil {
			scheme, _ = levels.Compile(graph)
		}
	}
	if scheme == nil {
		// 没发布过，或已发布的画布在新代码下编译不过（不应发生）：退回默认体系
		fallback, err := r.defaultLevelScheme(ctx)
		if err != nil {
			return nil, 0, err
		}
		scheme, version = fallback, 0
	}
	r.levels.mu.Lock()
	if r.levels.entries == nil {
		r.levels.entries = map[int64]levelSchemeEntry{}
	}
	r.levels.entries[appID] = levelSchemeEntry{scheme: scheme, version: version, expires: now.Add(levelSchemeTTL)}
	r.levels.mu.Unlock()
	return scheme, version, nil
}

// defaultLevelScheme 默认体系（旧版全站等级表转换而来，进程内缓存 5 分钟）。
func (r *Repository) defaultLevelScheme(ctx context.Context) (*levels.Scheme, error) {
	now := time.Now()
	r.levels.mu.RLock()
	fallback := r.levels.fallback
	r.levels.mu.RUnlock()
	if fallback != nil && now.Before(fallback.expires) {
		return fallback.scheme, nil
	}
	graph, err := r.DefaultLevelGraph(ctx)
	if err != nil {
		return nil, err
	}
	scheme, issues := levels.Compile(graph)
	if scheme == nil {
		return nil, fmt.Errorf("default level scheme does not compile: %v", issues)
	}
	r.levels.mu.Lock()
	r.levels.fallback = &levelSchemeEntry{scheme: scheme, expires: now.Add(5 * time.Minute)}
	r.levels.mu.Unlock()
	return scheme, nil
}

// DefaultLevelGraph 默认体系的画布：旧版全站等级表里启用的等级。
func (r *Repository) DefaultLevelGraph(ctx context.Context) (levels.Graph, error) {
	rows, err := r.pool.Query(ctx, `SELECT level, level_name, experience_required, exp_multiplier::float8,
COALESCE(icon, ''), COALESCE(color, ''), COALESCE(description, '')
FROM user_levels WHERE is_active = true ORDER BY level ASC`)
	if err != nil {
		return levels.Graph{}, err
	}
	defer rows.Close()
	var legacy []levels.LegacyLevel
	for rows.Next() {
		var row levels.LegacyLevel
		if err := rows.Scan(&row.Level, &row.Name, &row.Threshold, &row.Multiplier, &row.Icon, &row.Color, &row.Description); err != nil {
			return levels.Graph{}, err
		}
		legacy = append(legacy, row)
	}
	if err := rows.Err(); err != nil {
		return levels.Graph{}, err
	}
	return levels.DefaultGraph(legacy), nil
}

// ── 按 App 的等级 ──

// userLevelQ 一个人当前的等级：有等级记录就读记录（等级只升不降，记录是准的），没有时按经验现算
// （遇到晋级条件就停，不在这里取行为画像）。
func (r *Repository) userLevelQ(ctx context.Context, q queryExecutor, appID, userID, experience int64) (int, *levels.Scheme, error) {
	scheme, _, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return 1, nil, err
	}
	if userID <= 0 {
		level, _, _ := scheme.Resolve(experience, 0, nil)
		return level, scheme, nil
	}
	var level int
	err = q.QueryRow(ctx, `SELECT current_level FROM user_level_records WHERE user_id = $1 AND appid = $2`, userID, appID).Scan(&level)
	switch {
	case err == nil:
		return min(max(level, 1), scheme.MaxLevel()), scheme, nil
	case errors.Is(err, pgx.ErrNoRows):
		level, _, _ = scheme.Resolve(experience, 0, nil)
		return level, scheme, nil
	default:
		return 1, scheme, err
	}
}

// LevelMultipliers 签到用的经验 / 积分倍率（来自当前等级享有的权益）。
func (r *Repository) LevelMultipliers(ctx context.Context, appID, userID, experience int64) (float64, float64, error) {
	level, scheme, err := r.userLevelQ(ctx, r.pool, appID, userID, experience)
	if err != nil {
		return 1, 1, err
	}
	return scheme.Multiplier(level, levels.PrivilegeExpBoost), scheme.Multiplier(level, levels.PrivilegeIntegralBoost), nil
}

// ListLevelConfigs 应用的全部等级（GET /points/levels）。userID 不为 0 时标出已达到的等级。
func (r *Repository) ListLevelConfigs(ctx context.Context, appID, userID int64) ([]pointdomain.LevelConfig, error) {
	scheme, _, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}
	current := 0
	if userID > 0 {
		var experience int64
		if err := r.pool.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1 AND appid = $2`, userID, appID).Scan(&experience); err == nil {
			current, _, err = r.userLevelQ(ctx, r.pool, appID, userID, experience)
			if err != nil {
				return nil, err
			}
		}
	}
	refs, err := itemGrantRefsQ(ctx, r.pool, appID)
	if err != nil {
		return nil, err
	}
	out := make([]pointdomain.LevelConfig, 0, len(scheme.Levels))
	for i, level := range scheme.Levels {
		// 体系要求隐藏未达到的等级时只露出下一级
		if scheme.HideLocked && current > 0 && level.Level > current+1 {
			break
		}
		cfg := pointdomain.LevelConfig{
			ID: int64(level.Level), Level: level.Level, LevelName: level.Name, ExperienceRequired: level.Threshold,
			ExpMultiplier: scheme.Multiplier(level.Level, levels.PrivilegeExpBoost),
			Icon:          level.Icon, Color: level.Color, Description: level.Description,
			Privileges: levelPrivilegeViews(level.Privileges), Effective: levelPrivilegeViews(scheme.Effective(level.Level)),
			Rewards:  levelRewardViews(level.Rewards, refs),
			IsActive: true, SortOrder: level.Level, Reached: current >= level.Level,
		}
		if i+1 < len(scheme.Levels) {
			next := scheme.Levels[i+1].Threshold - level.Threshold
			cfg.ExperienceNext = &next
		}
		if level.Gate != nil && level.Gate.Source != "" {
			cfg.Gate = &pointdomain.LevelGateInfo{Label: level.Gate.Label, Hint: level.Gate.Hint}
		}
		out = append(out, cfg)
	}
	return out, nil
}

func levelPrivilegeViews(in []levels.Privilege) []pointdomain.LevelPrivilege {
	out := make([]pointdomain.LevelPrivilege, 0, len(in))
	for _, p := range in {
		out = append(out, pointdomain.LevelPrivilege{Kind: p.Kind, Label: p.Label, Key: p.Key, Value: p.Value, Text: p.Text, Icon: p.Icon, Level: p.Level})
	}
	return out
}

func levelRewardViews(in []levels.Reward, refs itemgrant.Refs) []pointdomain.LevelRewardInfo {
	out := make([]pointdomain.LevelRewardInfo, 0, len(in))
	for _, reward := range in {
		out = append(out, pointdomain.LevelRewardInfo{NodeID: reward.NodeID, Label: reward.Label, Items: describeItems(reward.Items, refs), Claim: reward.Claim})
	}
	return out
}

func describeItems(items []itemgrant.Item, refs itemgrant.Refs) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, itemgrant.DescribeItem(item, refs))
	}
	return out
}

// GetExperienceMultiplier 签到经验倍率（兼容旧调用）。
func (r *Repository) GetExperienceMultiplier(ctx context.Context, appID, userID, experience int64) (float64, error) {
	exp, _, err := r.LevelMultipliers(ctx, appID, userID, experience)
	return exp, err
}

// GetUserLevelProfile 我的等级：在一个短事务里按当前体系补判一次（晋级条件后来满足了、体系改过门槛），再返回进度。
func (r *Repository) GetUserLevelProfile(ctx context.Context, userID int64, appID int64) (*pointdomain.LevelProfile, error) {
	user, err := r.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, nil
	}
	profile, err := r.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	synced, err := r.syncUserLevel(ctx, tx, userID, appID, user.Experience, time.Now().UTC(), levelSyncOptions{Reason: levels.ReasonSync})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	info := synced.Info
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_rewards WHERE appid = $1 AND user_id = $2 AND status = 'claimable'`,
		appID, userID).Scan(&info.ClaimableRewards); err != nil {
		return nil, err
	}

	result := &pointdomain.LevelProfile{
		UserInfo: pointdomain.LevelUserInfo{
			// 只出现在用户端「我的等级」里：给对外 ID（docs/user-public-id.md）
			ID:         user.PublicID,
			Account:    user.Account,
			Integral:   user.Integral,
			Experience: user.Experience,
		},
		LevelInfo: *info,
	}
	if profile != nil {
		result.UserInfo.Nickname = profile.Nickname
		result.UserInfo.Avatar = profile.Avatar
	}
	return result, nil
}

// ── 同步等级记录 ──

type levelSyncOptions struct {
	Reason   string
	Operator string
	Note     string
	// ForceLevel 管理员强制升级：至少到这一级（越过门槛与晋级条件）
	ForceLevel int
	// Recompute 按经验从第一级重新判定（允许降级，但不低于锁定的等级），重算任务用
	Recompute bool
	// Lock 判定完把结果锁定为保底；Unlock 解除锁定（两者都没有时保持原样）
	Lock   bool
	Unlock bool
	// SkipRewards 不写这次经过的各级达成奖励（之后也不会再发，要发用「补发」）
	SkipRewards bool
	// Silent 不发升级通知（升级记录照写）
	Silent bool
	// Traces 记下每一道晋级条件的判定（单人推演用）
	Traces *[]levels.GateTrace
}

type levelSyncResult struct {
	Info    *pointdomain.LevelInfo
	From    int
	To      int
	Changed bool
}

// syncUserLevelRecord 经验变化后同步等级（经验账本、签到在事务里调用）。
func (r *Repository) syncUserLevelRecord(ctx context.Context, q queryExecutor, userID int64, appID int64,
	totalExperience int64, updatedAt time.Time) (*pointdomain.LevelInfo, error) {
	synced, err := r.syncUserLevel(ctx, q, userID, appID, totalExperience, updatedAt, levelSyncOptions{Reason: levels.ReasonExperience})
	if err != nil {
		return nil, err
	}
	return synced.Info, nil
}

// syncUserLevel 按应用的体系判定等级并写回记录。等级变了就写升级记录；升级经过的每一级的达成奖励各写一条
// （自动到账的进入待发放，由后台逐条在独立事务里发放，发放失败不影响这里的经验变化）。
func (r *Repository) syncUserLevel(ctx context.Context, q queryExecutor, userID, appID, totalExperience int64,
	updatedAt time.Time, opts levelSyncOptions) (*levelSyncResult, error) {
	scheme, version, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}

	query := `SELECT user_id, appid, current_level, current_experience, total_experience, next_level_experience, level_progress, highest_level, level_up_count, last_level_up_at, locked_level, capped_level FROM user_level_records WHERE user_id = $1 AND appid = $2`
	if _, isTx := q.(pgx.Tx); isTx {
		query += ` FOR UPDATE`
	}
	record, err := scanLevelRecord(q.QueryRow(ctx, query, userID, appID))
	if err != nil {
		return nil, err
	}
	floor, locked, capped := 0, 0, 0
	if record != nil {
		floor, locked, capped = record.CurrentLevel, record.LockedLevel, record.CappedLevel
	}

	// 晋级条件按需取行为画像（一次判定最多取一次）
	var profile *lotterydomain.BehaviorProfile
	check := func(level levels.Level) (bool, error) {
		if profile == nil {
			now := time.Now()
			p, err := r.LotteryBehaviorProfile(ctx, appID, userID, now, itemGrantDayStart(now))
			if err != nil {
				return false, err
			}
			profile = &p
		}
		passed, err := level.Gate.Pass(*profile)
		if opts.Traces != nil {
			trace := levels.GateTrace{Level: level.Level, NodeID: level.Gate.NodeID, Label: level.Gate.Label, Passed: passed && err == nil}
			if err != nil {
				trace.Error = err.Error()
			}
			*opts.Traces = append(*opts.Traces, trace)
		}
		if err != nil {
			// 条件算不出来（画像缺字段之类）按不满足处理，不让一次经验变化失败
			return false, nil
		}
		return passed, nil
	}

	start := floor
	switch {
	case opts.Recompute:
		// 锁定的等级是管理员给的，按经验重算也不降到它以下
		start = min(locked, scheme.MaxLevel())
	case opts.ForceLevel > 0:
		start = max(floor, min(opts.ForceLevel, scheme.MaxLevel()))
	}
	newLevel, blocked, err := scheme.Resolve(totalExperience, start, check)
	if err != nil {
		return nil, err
	}
	// 等级封顶（成长调控）：经验照常累计，等级停在封顶处；封顶低于现有等级时不在这里降级（降级只由管理员显式执行）
	cappedHit := false
	if capped > 0 && newLevel > capped {
		// 不低于保底：平时保底是现有等级，重算时是锁定的等级
		lower := floor
		if opts.Recompute {
			lower = start
		}
		newLevel, blocked, cappedHit = max(capped, min(lower, newLevel)), nil, true
	}

	switch {
	case opts.Unlock:
		locked = 0
	case opts.Lock:
		locked = newLevel
	}

	state := scheme.StateOf(newLevel, totalExperience)
	from := max(floor, 1)
	highest := newLevel
	levelUpCount := 0
	var lastLevelUpAt *time.Time
	if record != nil {
		highest = max(record.HighestLevel, newLevel)
		levelUpCount = record.LevelUpCount
		lastLevelUpAt = record.LastLevelUpAt
	}
	if newLevel > from {
		levelUpCount += newLevel - from
		ts := updatedAt.UTC()
		lastLevelUpAt = &ts
	}

	var nextLevelExperience any
	if !state.IsMaxLevel {
		nextLevelExperience = state.ExpToNextLevel
	}
	if _, err := q.Exec(ctx, `INSERT INTO user_level_records (user_id, appid, current_level, current_experience, total_experience, next_level_experience, level_progress, highest_level, level_up_count, last_level_up_at, locked_level, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $12, $11, $11)
ON CONFLICT (user_id, appid) DO UPDATE SET
    current_level = EXCLUDED.current_level,
    current_experience = EXCLUDED.current_experience,
    total_experience = EXCLUDED.total_experience,
    next_level_experience = EXCLUDED.next_level_experience,
    level_progress = EXCLUDED.level_progress,
    highest_level = EXCLUDED.highest_level,
    level_up_count = EXCLUDED.level_up_count,
    last_level_up_at = EXCLUDED.last_level_up_at,
    locked_level = EXCLUDED.locked_level,
    updated_at = EXCLUDED.updated_at`,
		userID, appID, newLevel, state.ExpInCurrentLevel, totalExperience, nextLevelExperience, state.Progress,
		highest, levelUpCount, lastLevelUpAt, updatedAt.UTC(), locked); err != nil {
		return nil, err
	}

	queued := false
	// 第一次建记录时的补判不算升级（新用户第一次打开等级页、老数据补建记录），只发经过的达成奖励
	if newLevel != from && !(record == nil && opts.Reason == levels.ReasonSync) {
		notify := levels.NotifyNone
		if newLevel > from && scheme.Notify && !opts.Silent {
			notify = levels.NotifyPending
			queued = true
		}
		if _, err := q.Exec(ctx, `INSERT INTO user_level_events (appid, user_id, from_level, to_level, floor_level, experience, reason, scheme_version, operator, note, notify_status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			appID, userID, from, newLevel, floor, totalExperience, opts.Reason, version, opts.Operator, truncateRunes(opts.Note, 200), notify); err != nil {
			return nil, err
		}
	}
	if newLevel > floor && !opts.SkipRewards {
		for _, reward := range scheme.RewardsBetween(floor, newLevel) {
			inserted, err := insertLevelReward(ctx, q, appID, userID, reward, version, levels.OriginLevelUp)
			if err != nil {
				return nil, err
			}
			if inserted && reward.Claim == levels.ClaimAuto {
				queued = true
			}
		}
	}
	if queued {
		r.levelWorkQueued()
	}

	info := levelInfoFromState(scheme, version, state, totalExperience)
	info.HighestLevel = highest
	info.LevelUpCount = levelUpCount
	info.LastLevelUpAt = lastLevelUpAt
	if blocked != nil && blocked.Gate != nil {
		info.BlockedBy = &pointdomain.LevelGateInfo{Label: blocked.Gate.Label, Hint: blocked.Gate.Hint}
	}
	if capped > 0 {
		info.CappedLevel = capped
		info.CapReached = cappedHit || newLevel >= capped
	}
	return &levelSyncResult{Info: info, From: from, To: newLevel, Changed: newLevel != from}, nil
}

func levelInfoFromState(scheme *levels.Scheme, version int, state levels.State, experience int64) *pointdomain.LevelInfo {
	current := state.Config
	info := &pointdomain.LevelInfo{
		CurrentLevel:       current.Level,
		CurrentLevelName:   current.Name,
		ExpMultiplier:      scheme.Multiplier(current.Level, levels.PrivilegeExpBoost),
		TotalExperience:    experience,
		ExpInCurrentLevel:  state.ExpInCurrentLevel,
		ExpRangeForLevel:   state.ExpRangeForLevel,
		ExpToNextLevel:     state.ExpToNextLevel,
		LevelProgress:      state.Progress,
		CurrentLevelMinExp: current.Threshold,
		HighestLevel:       current.Level,
		IsMaxLevel:         state.IsMaxLevel,
		Icon:               current.Icon,
		Color:              current.Color,
		Description:        current.Description,
		MaxLevel:           scheme.MaxLevel(),
		Privileges:         levelPrivilegeViews(scheme.Effective(current.Level)),
		SchemeName:         scheme.Name,
		SchemeVersion:      version,
		HideLocked:         scheme.HideLocked,
		CurrentLevelConfig: levelConfigView(scheme, current),
	}
	if state.Next != nil {
		info.NextLevel = state.Next.Level
		info.NextLevelName = state.Next.Name
		threshold := state.Next.Threshold
		info.NextLevelMinExp = &threshold
		info.NextLevelConfig = levelConfigView(scheme, *state.Next)
	}
	return info
}

// levelConfigView 不带奖励描述的精简一级（嵌在 LevelInfo 里）。
func levelConfigView(scheme *levels.Scheme, level levels.Level) *pointdomain.LevelConfig {
	cfg := &pointdomain.LevelConfig{
		ID: int64(level.Level), Level: level.Level, LevelName: level.Name, ExperienceRequired: level.Threshold,
		ExpMultiplier: scheme.Multiplier(level.Level, levels.PrivilegeExpBoost),
		Icon:          level.Icon, Color: level.Color, Description: level.Description,
		Privileges: levelPrivilegeViews(level.Privileges), Effective: levelPrivilegeViews(scheme.Effective(level.Level)),
		Rewards:  make([]pointdomain.LevelRewardInfo, 0, len(level.Rewards)),
		IsActive: true, SortOrder: level.Level,
	}
	for _, reward := range level.Rewards {
		cfg.Rewards = append(cfg.Rewards, pointdomain.LevelRewardInfo{NodeID: reward.NodeID, Label: reward.Label, Claim: reward.Claim, Items: []string{}})
	}
	if level.Gate != nil && level.Gate.Source != "" {
		cfg.Gate = &pointdomain.LevelGateInfo{Label: level.Gate.Label, Hint: level.Gate.Hint}
	}
	return cfg
}

// insertLevelReward 写一条达成奖励（同一个人同一级同一个节点只有一条）。
func insertLevelReward(ctx context.Context, q queryExecutor, appID, userID int64, reward levels.LevelReward,
	version int, origin string) (bool, error) {
	status := levels.RewardQueued
	if reward.Claim == levels.ClaimManual {
		status = levels.RewardClaimable
	}
	items, _ := json.Marshal(reward.Items)
	tag, err := q.Exec(ctx, `INSERT INTO user_level_rewards (appid, user_id, level, node_id, reward_key, label, items, claim_mode, status, scheme_version, origin)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (appid, user_id, reward_key) DO NOTHING`,
		appID, userID, reward.Level, reward.NodeID, reward.Key(reward.Level), truncateRunes(reward.Label, 64), items,
		reward.Claim, status, version, origin)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ── 达成奖励的发放 ──

const levelRewardColumns = `w.id, w.appid, w.user_id, COALESCE(u.account, ''), COALESCE(up.nickname, ''), w.level, w.node_id, w.label,
w.items, w.results, w.claim_mode, w.status, w.error, w.attempts, w.scheme_version, w.origin, w.granted_at,
w.handled_by, w.handled_note, w.created_at, w.updated_at`

const levelRewardJoins = `FROM user_level_rewards w
LEFT JOIN users u ON u.id = w.user_id
LEFT JOIN user_profiles up ON up.user_id = w.user_id`

func scanLevelReward(row interface{ Scan(dest ...any) error }) (*levels.RewardRecord, error) {
	var rec levels.RewardRecord
	var items, results []byte
	if err := row.Scan(&rec.ID, &rec.AppID, &rec.UserID, &rec.Account, &rec.Nickname, &rec.Level, &rec.NodeID, &rec.Label,
		&items, &results, &rec.Claim, &rec.Status, &rec.Error, &rec.Attempts, &rec.SchemeVersion, &rec.Origin, &rec.GrantedAt,
		&rec.HandledBy, &rec.HandledNote, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(items, &rec.Items)
	_ = json.Unmarshal(results, &rec.Results)
	if rec.Items == nil {
		rec.Items = []itemgrant.Item{}
	}
	if rec.Results == nil {
		rec.Results = []itemgrant.ItemResult{}
	}
	return &rec, nil
}

// GetLevelReward 取一条达成奖励记录（appID 为 0 时不限应用，后台循环用）。
func (r *Repository) GetLevelReward(ctx context.Context, appID, rewardID int64) (*levels.RewardRecord, error) {
	rec, err := scanLevelReward(r.pool.QueryRow(ctx, `SELECT `+levelRewardColumns+` `+levelRewardJoins+`
WHERE w.id = $1 AND ($2 = 0 OR w.appid = $2)`, rewardID, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLevelRewardMissing
	}
	if err == nil {
		refs, _ := itemGrantRefsQ(ctx, r.pool, rec.AppID)
		rec.ItemsText = describeItems(rec.Items, refs)
	}
	return rec, err
}

// ListLevelRewards 达成奖励记录。
func (r *Repository) ListLevelRewards(ctx context.Context, query levels.RewardQuery) (*levels.Page[levels.RewardRecord], error) {
	page, limit := normalizePage(query.Page, query.Limit)
	args := []any{query.AppID}
	where := []string{"w.appid = $1"}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.UserID > 0 {
		add("w.user_id = ?", query.UserID)
	}
	if query.Status != "" {
		add("w.status = ?", query.Status)
	}
	if query.Level > 0 {
		add("w.level = ?", query.Level)
	}
	if query.NodeID != "" {
		add("w.node_id = ?", query.NodeID)
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_rewards w WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+levelRewardColumns+` `+levelRewardJoins+` WHERE `+cond+`
ORDER BY CASE w.status WHEN 'claimable' THEN 0 WHEN 'failed' THEN 1 WHEN 'pending' THEN 2 WHEN 'queued' THEN 3 ELSE 4 END,
         w.created_at DESC, w.id DESC
LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &levels.Page[levels.RewardRecord]{Items: []levels.RewardRecord{}, Total: total, Page: page, Limit: limit}
	for rows.Next() {
		rec, err := scanLevelReward(rows)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	refs, err := itemGrantRefsQ(ctx, r.pool, query.AppID)
	if err != nil {
		return nil, err
	}
	for i := range out.Items {
		out.Items[i].ItemsText = describeItems(out.Items[i].Items, refs)
	}
	return out, nil
}

// QueuedLevelRewards 待自动发放的奖励（后台循环逐条发）。
func (r *Repository) QueuedLevelRewards(ctx context.Context, limit int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM user_level_rewards WHERE status = 'queued' ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LevelRewardGrantMode 发放的入口：后台自动发、用户领取、管理员重试。
type LevelRewardGrantMode int

const (
	LevelRewardAuto LevelRewardGrantMode = iota
	LevelRewardClaim
	LevelRewardRetry
)

// GrantLevelReward 发一条达成奖励：锁记录 → 锁用户 → 逐项走物品发放的账本入口 → 写回结果。
// 任何一项发不出去整笔回滚，记录标为失败（带原因），管理员可以重试。claimUser 不为 0 时只允许本人领取。
func (r *Repository) GrantLevelReward(ctx context.Context, rewardID int64, mode LevelRewardGrantMode, claimUser int64) (*levels.RewardRecord, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var appID, userID int64
	var level int
	var status, label, nodeID string
	var rawItems []byte
	err = tx.QueryRow(ctx, `SELECT appid, user_id, level, status, label, node_id, items FROM user_level_rewards WHERE id = $1 FOR UPDATE`, rewardID).
		Scan(&appID, &userID, &level, &status, &label, &nodeID, &rawItems)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLevelRewardMissing
	}
	if err != nil {
		return nil, err
	}
	if claimUser > 0 && claimUser != userID {
		return nil, ErrLevelRewardMissing
	}
	allowed := map[LevelRewardGrantMode]string{LevelRewardAuto: levels.RewardQueued, LevelRewardClaim: levels.RewardClaimable, LevelRewardRetry: levels.RewardFailed}
	if status != allowed[mode] {
		if mode == LevelRewardAuto {
			// 并发的另一个实例已经处理过
			return nil, nil
		}
		return nil, ErrLevelRewardState
	}
	var items []itemgrant.Item
	_ = json.Unmarshal(rawItems, &items)

	if err := lockUserTx(ctx, tx, appID, userID); err != nil {
		return nil, err
	}
	refs, err := itemGrantRefsQ(ctx, tx, appID)
	if err != nil {
		return nil, err
	}
	recordNo := "LVR" + strconv.FormatInt(rewardID, 10)
	grant := itemGrantContext{
		AppID: appID, UserID: userID, RecordID: rewardID, RecordNo: recordNo,
		Title: fmt.Sprintf("等级奖励 Lv.%d", level), Source: "等级奖励 " + label, Refs: refs, Now: time.Now(),
		Meta: map[string]any{"levelReward": rewardID, "level": level, "source": "level_reward"},
	}
	next := levels.RewardGranted
	results := make([]itemgrant.ItemResult, 0, len(items))
	for i, item := range items {
		if spec, ok := itemgrant.FindItemSpec(item.Type); ok && spec.Pending {
			next = levels.RewardPending
		}
		result, err := r.grantItemTx(ctx, tx, grant, i, itemgrant.PlannedItem{NodeID: nodeID, Item: item})
		if err != nil {
			_ = tx.Rollback(ctx)
			tx = nil
			message := itemGrantFailureMessage(err)
			if _, uerr := r.pool.Exec(ctx, `UPDATE user_level_rewards SET status = 'failed', error = $2, attempts = attempts + 1, updated_at = NOW() WHERE id = $1`,
				rewardID, message); uerr != nil {
				return nil, uerr
			}
			rec, gerr := r.GetLevelReward(ctx, 0, rewardID)
			if gerr != nil {
				return nil, gerr
			}
			return rec, nil
		}
		results = append(results, result)
	}
	rawResults, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE user_level_rewards SET status = $2, results = $3, error = '', attempts = attempts + 1,
granted_at = NOW(), updated_at = NOW() WHERE id = $1`, rewardID, next, rawResults); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetLevelReward(ctx, 0, rewardID)
}

// FulfillLevelReward 人工物品已处理。
func (r *Repository) FulfillLevelReward(ctx context.Context, appID, rewardID int64, operator, note string) (*levels.RewardRecord, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE user_level_rewards SET status = 'granted', handled_by = $3, handled_note = $4, updated_at = NOW()
WHERE id = $1 AND appid = $2 AND status = 'pending'`, rewardID, appID, operator, truncateRunes(note, 200))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetLevelReward(ctx, appID, rewardID); err != nil {
			return nil, err
		}
		return nil, ErrLevelRewardState
	}
	return r.GetLevelReward(ctx, appID, rewardID)
}

// BackfillLevelReward 补发：给已经达到（最高等级不低于）这份奖励所在等级、还没有这条记录的人写一条奖励记录。
func (r *Repository) BackfillLevelReward(ctx context.Context, appID int64, nodeID string) (int64, error) {
	scheme, version, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return 0, err
	}
	reward, ok := scheme.FindReward(nodeID)
	if !ok {
		return 0, ErrLevelRewardMissing
	}
	status := levels.RewardQueued
	if reward.Claim == levels.ClaimManual {
		status = levels.RewardClaimable
	}
	items, _ := json.Marshal(reward.Items)
	tag, err := r.pool.Exec(ctx, `INSERT INTO user_level_rewards (appid, user_id, level, node_id, reward_key, label, items, claim_mode, status, scheme_version, origin)
SELECT $1, lr.user_id, $2, $3, $4, $5, $6, $7, $8, $9, 'backfill'
FROM user_level_records lr JOIN users u ON u.id = lr.user_id AND u.appid = lr.appid
WHERE lr.appid = $1 AND GREATEST(lr.current_level, lr.highest_level) >= $2
ON CONFLICT (appid, user_id, reward_key) DO NOTHING`,
		appID, reward.Level, reward.NodeID, reward.Key(reward.Level), truncateRunes(reward.Label, 64), items, reward.Claim, status, version)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() > 0 && status == levels.RewardQueued {
		r.levelWorkQueued()
	}
	return tag.RowsAffected(), nil
}

// ── 升级通知 ──

// PendingLevelNotice 一条待发的升级通知。
type PendingLevelNotice struct {
	EventID   int64
	AppID     int64
	UserID    int64
	FromLevel int
	ToLevel   int
	Nickname  string
	Account   string
	Rewards   []levels.RewardRecord
	floor     int
}

// ClaimLevelNotices 领取一批待发的升级通知（多实例不重复：SKIP LOCKED + 改成 sending）。
func (r *Repository) ClaimLevelNotices(ctx context.Context, limit int) ([]PendingLevelNotice, error) {
	rows, err := r.pool.Query(ctx, `UPDATE user_level_events e SET notify_status = 'sending'
WHERE e.id IN (SELECT id FROM user_level_events WHERE notify_status = 'pending' ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED)
RETURNING e.id, e.appid, e.user_id, e.from_level, e.to_level, e.floor_level`, limit)
	if err != nil {
		return nil, err
	}
	var out []PendingLevelNotice
	for rows.Next() {
		var n PendingLevelNotice
		if err := rows.Scan(&n.EventID, &n.AppID, &n.UserID, &n.FromLevel, &n.ToLevel, &n.floor); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		n := &out[i]
		_ = r.pool.QueryRow(ctx, `SELECT u.account, COALESCE(up.nickname, '') FROM users u LEFT JOIN user_profiles up ON up.user_id = u.id WHERE u.id = $1`,
			n.UserID).Scan(&n.Account, &n.Nickname)
		page, err := r.ListLevelRewards(ctx, levels.RewardQuery{AppID: n.AppID, UserID: n.UserID, Limit: 50})
		if err != nil {
			return nil, err
		}
		for _, rec := range page.Items {
			if rec.Level > n.floor && rec.Level <= n.ToLevel {
				n.Rewards = append(n.Rewards, rec)
			}
		}
	}
	return out, nil
}

// FinishLevelNotice 通知发完。
func (r *Repository) FinishLevelNotice(ctx context.Context, eventID int64, sent bool) error {
	status := levels.NotifySent
	if !sent {
		status = levels.NotifyFailed
	}
	_, err := r.pool.Exec(ctx, `UPDATE user_level_events SET notify_status = $2 WHERE id = $1`, eventID, status)
	return err
}

// ResetStaleLevelNotices 发送中卡住的通知（实例在发送途中退出）退回待发。
func (r *Repository) ResetStaleLevelNotices(ctx context.Context, before time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_level_events SET notify_status = 'pending'
WHERE notify_status = 'sending' AND created_at < $1`, before)
	return err
}
