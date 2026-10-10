package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	rewardedad "aegis/internal/domain/rewardedad"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 激励广告的业务错误码（403 / 404 / 409 的空段，见 docs/rewarded-ads.md）。
//
// 只有「这次上报本身不成立」才报错；限额、冷却这类「观看成立但不发奖」的结论
// 不是错误 —— 记录已经落库，结论放在返回体的 status / reason 里，客户端据此分支。
const (
	errCodeRewardedAdDisabled         = 40350 // 403 应用没有开启激励广告
	errCodeRewardedAdSceneUnavailable = 40351 // 403 场景不存在或已停用
	errCodeRewardedAdUserMismatch     = 40352 // 403 这次观看属于另一个账号
	errCodeRewardedAdViewNotFound     = 40451 // 404
	errCodeRewardedAdAlreadyGranted   = 40930 // 409
)

const (
	rewardedAdStatsDefaultDays = 14
	rewardedAdStatsMaxDays     = 90
	// rewardedAdUserTagLen 用户标识里签名部分的长度（十六进制位）。
	// 64 位足以挡住猜测，又不至于让平台后台的用户列里全是一长串。
	rewardedAdUserTagLen = 16
)

// RewardedAdService 应用级激励广告。
//
// 一次观看由两方确认：广告平台的服务端回调（验签）与客户端上报（带用户令牌），
// 两方以平台的 trans_id 对上，结算与发奖在仓储层的单事务里完成（见 RecordRewardedAdView）。
//
// 密钥安全：平台的 Security Key 以 AES-GCM 密文落库（派生自 SECURITY_MASTER_KEY），
// 管理端只看得到「配没配」与末尾提示，任何出网响应都不回传明文。
type RewardedAdService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	secretKey  []byte
	userKey    []byte
	apiBaseURL string
}

func NewRewardedAdService(log *zap.Logger, pg *pgrepo.Repository, cfg config.Config) *RewardedAdService {
	if log == nil {
		log = zap.NewNop()
	}
	// 主密钥按用途各自派生，互不复用：密文密钥泄露不应顺带让人能伪造用户标识。
	secret := sha256.Sum256([]byte("aegis.rewarded-ad.master\x00" + cfg.Security.MasterKey))
	user := sha256.Sum256([]byte("aegis.rewarded-ad.user\x00" + cfg.Security.MasterKey))
	return &RewardedAdService{
		log:        log,
		pg:         pg,
		secretKey:  secret[:],
		userKey:    user[:],
		apiBaseURL: strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/"),
	}
}

// ── 管理端：配置 ──

// AdminConfig 读取配置。没配过时返回一份默认值（未启用、灰鲸、双方确认）。
func (s *RewardedAdService) AdminConfig(ctx context.Context, appID int64, appKey string) (*rewardedad.AdminConfig, error) {
	cfg, err := s.pg.GetRewardedAdConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.adminView(cfg, appKey), nil
}

// SaveConfig 保存配置。密钥留空表示不修改。
func (s *RewardedAdService) SaveConfig(ctx context.Context, input rewardedad.SaveConfigInput, appKey string) (*rewardedad.AdminConfig, error) {
	current, err := s.pg.GetRewardedAdConfig(ctx, input.AppID)
	if err != nil {
		return nil, err
	}

	next := rewardedad.Config{
		AppID:         input.AppID,
		Enabled:       input.Enabled,
		Provider:      strings.TrimSpace(input.Provider),
		ProviderAppID: strings.TrimSpace(input.ProviderAppID),
		VerifyMode:    strings.TrimSpace(input.VerifyMode),
		DailyLimit:    input.DailyLimit,
		UpdatedBy:     input.Operator,
	}
	if next.Provider == "" {
		next.Provider = rewardedad.ProviderHuijing
	}
	if next.Provider != rewardedad.ProviderHuijing {
		return nil, apperrors.New(40000, http.StatusBadRequest, "暂不支持该广告平台")
	}
	if !rewardedad.ValidProviderAppID(next.ProviderAppID) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "平台应用 ID 需为 64 位以内的字母或数字")
	}
	if next.VerifyMode == "" {
		next.VerifyMode = rewardedad.VerifyDual
	}
	if !rewardedad.ValidVerifyMode(next.VerifyMode) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的校验模式")
	}
	if next.DailyLimit < 0 || next.DailyLimit > rewardedad.MaxDailyLimit {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"每日总次数需在 0–"+strconv.Itoa(rewardedad.MaxDailyLimit)+" 之间")
	}

	next.Scenes = make([]rewardedad.Scene, 0, len(input.Scenes))
	for _, scene := range input.Scenes {
		next.Scenes = append(next.Scenes, rewardedad.NormalizeScene(scene))
	}
	if err := rewardedad.ValidateScenes(next.Scenes); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if err := s.checkPlansExist(ctx, input.AppID, next.Scenes); err != nil {
		return nil, err
	}

	// 密钥：留空沿用、显式清除、或替换。
	if current != nil {
		next.SecurityKeyCipher = current.SecurityKeyCipher
		next.SecurityKeyHint = current.SecurityKeyHint
	}
	if input.ClearSecurityKey {
		next.SecurityKeyCipher = ""
		next.SecurityKeyHint = ""
	}
	if key := strings.TrimSpace(input.SecurityKey); key != "" {
		if utf8.RuneCountInString(key) > 256 {
			return nil, apperrors.New(40000, http.StatusBadRequest, "Security Key 过长")
		}
		cipher, err := encryptSecret(s.secretKey, key)
		if err != nil {
			return nil, err
		}
		next.SecurityKeyCipher = cipher
		next.SecurityKeyHint = rewardedAdKeyHint(key)
	}

	// 开关打开前把「开了也发不出去」的配置挡下来：配置项必须有执行点，
	// 一个开着却永远验不过签的开关比关着更糟 —— 用户看完广告什么都拿不到。
	if next.Enabled {
		hasScene := false
		for _, scene := range next.Scenes {
			if scene.Enabled {
				hasScene = true
				break
			}
		}
		if !hasScene {
			return nil, apperrors.New(40000, http.StatusBadRequest, "启用前至少需要一个启用中的奖励场景")
		}
		if next.VerifyMode != rewardedad.VerifyClient && next.SecurityKeyCipher == "" {
			return nil, apperrors.New(40000, http.StatusBadRequest,
				"当前校验模式要验证平台回调签名，请先填写 Security Key")
		}
	}

	saved, err := s.pg.SaveRewardedAdConfig(ctx, next)
	if err != nil {
		return nil, err
	}
	return s.adminView(saved, appKey), nil
}

func (s *RewardedAdService) checkPlansExist(ctx context.Context, appID int64, scenes []rewardedad.Scene) error {
	needed := false
	for _, scene := range scenes {
		for _, reward := range scene.Rewards {
			if reward.Type == cardkeydomain.RewardVipPlan {
				needed = true
			}
		}
	}
	if !needed {
		return nil
	}
	plans, err := s.pg.ListVipPlans(ctx, appID, false)
	if err != nil {
		return err
	}
	known := make(map[int64]bool, len(plans))
	lifetime := make(map[int64]bool, len(plans))
	for _, plan := range plans {
		known[plan.ID] = true
		lifetime[plan.ID] = plan.Lifetime
	}
	for _, scene := range scenes {
		for _, reward := range scene.Rewards {
			if reward.Type != cardkeydomain.RewardVipPlan {
				continue
			}
			if !known[reward.RefID] {
				return apperrors.New(40000, http.StatusBadRequest, "场景「"+scene.Key+"」选择的会员套餐不存在")
			}
			// 激励广告是可以每天反复看的；永久会员看一次就到头了，之后每次观看都只是白发赠送积分
			if lifetime[reward.RefID] {
				return apperrors.New(40000, http.StatusBadRequest, "场景「"+scene.Key+"」不能以永久套餐作为奖励")
			}
		}
	}
	return nil
}

func (s *RewardedAdService) adminView(cfg *rewardedad.Config, appKey string) *rewardedad.AdminConfig {
	callback, absolute := s.callbackURL(appKey)
	view := &rewardedad.AdminConfig{
		Provider:         rewardedad.ProviderHuijing,
		VerifyMode:       rewardedad.VerifyDual,
		Scenes:           []rewardedad.Scene{},
		CallbackURL:      callback,
		CallbackAbsolute: absolute,
		Catalog:          rewardedad.RewardCatalog(),
		VerifyModes:      rewardedad.VerifyModes(),
		Providers:        rewardedad.Providers(),
	}
	if cfg == nil {
		return view
	}
	view.Enabled = cfg.Enabled
	view.Provider = cfg.Provider
	view.ProviderAppID = cfg.ProviderAppID
	view.HasSecurityKey = cfg.SecurityKeyCipher != ""
	view.SecurityKeyHint = cfg.SecurityKeyHint
	view.VerifyMode = cfg.VerifyMode
	view.DailyLimit = cfg.DailyLimit
	view.Scenes = cfg.Scenes
	view.UpdatedBy = cfg.UpdatedBy
	updated := cfg.UpdatedAt
	view.UpdatedAt = &updated
	return view
}

// callbackURL 填进广告平台「配置回调URL」的地址。
//
// 参数名与灰鲸后台默认模板一致（userId / transId / sign …），平台会把宏替换后以 GET 调用；
// 配了 API_BASE_URL 时给出绝对地址，否则只给路径，由控制台补上当前站点。
func (s *RewardedAdService) callbackURL(appKey string) (string, bool) {
	path := "/api/apps/" + url.PathEscape(appKey) + "/ads/huijing/callback"
	if s.apiBaseURL == "" {
		return path, false
	}
	return s.apiBaseURL + path, true
}

func rewardedAdKeyHint(key string) string {
	if len(key) <= 8 {
		return "已配置"
	}
	return "…" + key[len(key)-4:]
}

// ── 管理端：记录、统计、补发 ──

// ListViews 观看记录分页。
func (s *RewardedAdService) ListViews(ctx context.Context, query rewardedad.ViewQuery) (*rewardedad.ViewPage, error) {
	page, err := s.pg.ListRewardedAdViews(ctx, query)
	if err != nil {
		return nil, err
	}
	cfg, err := s.pg.GetRewardedAdConfig(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	for index := range page.Items {
		if scene, ok := cfg.FindScene(page.Items[index].Scene); ok {
			page.Items[index].SceneName = scene.Name
		}
	}
	return page, nil
}

// Stats 统计窗口内的概览与趋势。缺失的日期补零，前端不必自己对齐横轴。
func (s *RewardedAdService) Stats(ctx context.Context, appID int64, days int) (*rewardedad.Stats, error) {
	if days <= 0 {
		days = rewardedAdStatsDefaultDays
	}
	if days > rewardedAdStatsMaxDays {
		days = rewardedAdStatsMaxDays
	}
	loc := timeutil.DefaultLocation()
	today := rewardedAdDayStart(time.Now(), loc)
	since := today.AddDate(0, 0, -(days - 1))

	stats, err := s.pg.RewardedAdStats(ctx, appID, since, loc.String())
	if err != nil {
		return nil, err
	}
	stats.Days = days

	byDate := make(map[string]rewardedad.StatsDay, len(stats.Trend))
	for _, day := range stats.Trend {
		byDate[day.Date] = day
	}
	trend := make([]rewardedad.StatsDay, 0, days)
	for offset := 0; offset < days; offset++ {
		date := since.AddDate(0, 0, offset).Format("2006-01-02")
		day, ok := byDate[date]
		if !ok {
			day = rewardedad.StatsDay{Date: date}
		}
		trend = append(trend, day)
	}
	stats.Trend = trend
	if last := trend[len(trend)-1]; last.Date == today.Format("2006-01-02") {
		stats.Summary.TodayViews = last.Views
		stats.Summary.TodayGranted = last.Granted
		stats.Summary.TodayRejected = last.Rejected
		stats.Summary.TodayUsers = last.Users
	}

	cfg, err := s.pg.GetRewardedAdConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	for index := range stats.Scenes {
		if scene, ok := cfg.FindScene(stats.Scenes[index].Scene); ok {
			stats.Scenes[index].Name = scene.Name
		}
	}
	return stats, nil
}

// GrantView 管理端补发：一条 pending / rejected 的记录不再等另一方、不再看限额，直接发。
//
// 用于「平台回调到了、客户端没来得及上报」这类用户投诉。只能补发一次：已发放的记录报 409。
func (s *RewardedAdService) GrantView(ctx context.Context, appID, viewID int64, sceneKey, operator string) (*rewardedad.View, error) {
	view, err := s.pg.GetRewardedAdView(ctx, appID, viewID)
	if err != nil {
		return nil, s.translate(err)
	}
	if view.Status == rewardedad.StatusGranted {
		return nil, s.translate(pgrepo.ErrRewardedAdAlreadyGranted)
	}
	if view.UserID == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "这条记录没有关联到用户，无法补发")
	}
	cfg, err := s.pg.GetRewardedAdConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	sceneKey = strings.TrimSpace(sceneKey)
	if sceneKey == "" {
		sceneKey = view.Scene
	}
	if _, ok := cfg.FindScene(sceneKey); !ok {
		return nil, apperrors.New(errCodeRewardedAdSceneUnavailable, http.StatusForbidden, "请选择要补发的奖励场景")
	}
	policy := s.policy(cfg, time.Now())
	policy.ForceGrant = true
	result, err := s.pg.RecordRewardedAdView(ctx, rewardedad.RecordInput{
		AppID:    appID,
		UserID:   view.UserID,
		TransID:  view.TransID,
		Side:     rewardedad.SideAdmin,
		Scene:    sceneKey,
		Operator: operator,
	}, policy)
	if err != nil {
		return nil, s.translate(err)
	}
	result.Account = view.Account
	return result, nil
}

// ── 客户端 ──

// RewardedAdClaimInput 客户端上报一次看完的广告。
type RewardedAdClaimInput struct {
	AppID       int64
	UserID      int64
	Scene       string
	TransID     string
	PlacementID string
	Verified    *bool
	ErrorCode   string
	DeviceID    string
	ClientIP    string
}

// ClientStatus 当前用户的激励广告状态：开没开、每个场景还能看几次、多久之后能看。
func (s *RewardedAdService) ClientStatus(ctx context.Context, appID, userID int64) (*rewardedad.ClientStatus, error) {
	now := time.Now()
	status := &rewardedad.ClientStatus{Scenes: []rewardedad.ClientScene{}, ServerTime: now.UTC(), Remaining: -1}
	cfg, err := s.pg.GetRewardedAdConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return status, nil
	}
	status.Enabled = true
	status.Provider = cfg.Provider
	status.ProviderAppID = cfg.ProviderAppID
	status.VerifyMode = cfg.VerifyMode
	status.UserID = s.userToken(appID, userID)
	status.DailyLimit = cfg.DailyLimit

	policy := s.policy(cfg, now)
	usage, err := s.pg.RewardedAdUsage(ctx, appID, userID, policy.DayStart)
	if err != nil {
		return nil, err
	}
	status.TodayCount = usage.TodayTotal
	if cfg.DailyLimit > 0 {
		status.Remaining = max(cfg.DailyLimit-usage.TodayTotal, 0)
	}

	planNames := s.planNames(ctx, appID, cfg.Scenes)
	for _, scene := range cfg.Scenes {
		if !scene.Enabled {
			continue
		}
		status.Scenes = append(status.Scenes, buildClientScene(scene, usage, status.Remaining, planNames, now))
	}
	return status, nil
}

// buildClientScene 一个场景的剩余次数与冷却。纯函数，便于表驱动测试。
func buildClientScene(scene rewardedad.Scene, usage *rewardedad.Usage, appRemaining int,
	planNames map[int64]string, now time.Time) rewardedad.ClientScene {
	item := rewardedad.ClientScene{
		Key:             scene.Key,
		Name:            scene.Name,
		PlacementID:     scene.PlacementID,
		Rewards:         scene.Rewards,
		RewardSummary:   rewardedad.DescribeRewards(scene.Rewards, planNames),
		DailyLimit:      scene.DailyLimit,
		TodayCount:      usage.TodayByScene[scene.Key],
		CooldownSeconds: scene.CooldownSeconds,
		Remaining:       -1,
	}
	if scene.DailyLimit > 0 {
		item.Remaining = max(scene.DailyLimit-item.TodayCount, 0)
	}
	// 应用级总限额更紧时以它为准：场景说还能看 3 次、但今天总共只剩 1 次，就是 1 次。
	if appRemaining >= 0 && (item.Remaining < 0 || appRemaining < item.Remaining) {
		item.Remaining = appRemaining
	}
	if scene.CooldownSeconds > 0 {
		if last, ok := usage.LastByScene[scene.Key]; ok {
			next := last.Add(time.Duration(scene.CooldownSeconds) * time.Second)
			if now.Before(next) {
				item.CooldownRemaining = int(next.Sub(now).Seconds() + 0.999)
				nextUTC := next.UTC()
				item.NextAvailableAt = &nextUTC
			}
		}
	}
	item.Available = item.Remaining != 0 && item.CooldownRemaining == 0
	return item
}

// Claim 客户端上报一次看完的广告（也用于轮询结果：同一个 transId 再报一次即可）。
func (s *RewardedAdService) Claim(ctx context.Context, input RewardedAdClaimInput) (*rewardedad.ClaimResult, error) {
	input.TransID = strings.TrimSpace(input.TransID)
	input.Scene = strings.ToLower(strings.TrimSpace(input.Scene))
	if !rewardedad.ValidTransID(input.TransID) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "transId 无效")
	}
	now := time.Now()
	cfg, err := s.pg.GetRewardedAdConfig(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, apperrors.New(errCodeRewardedAdDisabled, http.StatusForbidden, "激励广告暂未开放")
	}
	scene, ok := cfg.FindScene(input.Scene)
	if !ok || !scene.Enabled {
		return nil, apperrors.New(errCodeRewardedAdSceneUnavailable, http.StatusForbidden, "该奖励活动不存在或已下线")
	}

	view, err := s.pg.RecordRewardedAdView(ctx, rewardedad.RecordInput{
		AppID:          input.AppID,
		UserID:         input.UserID,
		TransID:        input.TransID,
		Side:           rewardedad.SideClient,
		Scene:          scene.Key,
		PlacementID:    strings.TrimSpace(input.PlacementID),
		ClientVerified: input.Verified,
		ClientError:    strings.TrimSpace(input.ErrorCode),
		DeviceID:       strings.TrimSpace(input.DeviceID),
		ClientIP:       input.ClientIP,
		Now:            now.UTC(),
	}, s.policy(cfg, now))
	if err != nil {
		return nil, s.translate(err)
	}

	result := &rewardedad.ClaimResult{
		TransID:   view.TransID,
		Scene:     view.Scene,
		Status:    view.Status,
		Reason:    view.Reason,
		Message:   rewardedad.ReasonMessage(view.Status, view.Reason),
		Results:   view.Results,
		GrantedAt: view.GrantedAt,
		Remaining: -1,
	}
	// 剩余次数是展示用的，查不出来不影响这次结算的结论。
	if usage, err := s.pg.RewardedAdUsage(ctx, input.AppID, input.UserID, s.policy(cfg, now).DayStart); err == nil {
		appRemaining := -1
		if cfg.DailyLimit > 0 {
			appRemaining = max(cfg.DailyLimit-usage.TodayTotal, 0)
		}
		result.Remaining = buildClientScene(scene, usage, appRemaining, nil, now).Remaining
	}
	return result, nil
}

// MyViews 我的观看记录。只保留用户该看到的字段。
func (s *RewardedAdService) MyViews(ctx context.Context, appID, userID int64, page, limit int) (*rewardedad.ViewPage, error) {
	result, err := s.ListViews(ctx, rewardedad.ViewQuery{AppID: appID, UserID: userID, Page: page, Limit: limit})
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		item := &result.Items[index]
		item.Account = ""
		item.Extra = ""
		item.NetworkID = ""
		item.ClientIP = ""
		item.DeviceID = ""
		item.ClientError = ""
		item.Operator = ""
	}
	return result, nil
}

// ── 平台回调 ──

// HuijingCallbackInput 灰鲸服务端回调带来的参数（GET 查询串）。
type HuijingCallbackInput struct {
	AppID        int64
	UserID       string
	TransID      string
	Sign         string
	PlacementID  string
	RewardName   string
	RewardAmount int
	NetworkID    string
	Extra        string
}

// HuijingCallback 处理一次平台回调，返回给平台的 isValid。
//
// isValid 回答的是「这次观看算不算数」：验签不过、应用没开、用户认不出、被限额挡下都是 false；
// 双方确认模式下等客户端上报时是 true（观看本身成立）。平台没收到响应会每 200ms 重试 3 次，
// 重试落在同一行上，结论不变。
func (s *RewardedAdService) HuijingCallback(ctx context.Context, input HuijingCallbackInput) (bool, string) {
	cfg, err := s.pg.GetRewardedAdConfig(ctx, input.AppID)
	if err != nil {
		s.log.Error("rewarded ad callback: load config failed", zap.Int64("appid", input.AppID), zap.Error(err))
		return false, "internal"
	}
	if cfg == nil || !cfg.Enabled || cfg.Provider != rewardedad.ProviderHuijing {
		return false, rewardedad.ReasonDisabled
	}
	transID := strings.TrimSpace(input.TransID)
	if !rewardedad.ValidTransID(transID) {
		return false, "trans_invalid"
	}
	if cfg.SecurityKeyCipher == "" {
		// client 模式不验签；回调仍然记一笔，供对账。
		if cfg.VerifyMode != rewardedad.VerifyClient {
			return false, "key_missing"
		}
	} else {
		key, err := decryptSecret(s.secretKey, cfg.SecurityKeyCipher)
		if err != nil {
			s.log.Error("rewarded ad callback: decrypt key failed", zap.Int64("appid", input.AppID), zap.Error(err))
			return false, "internal"
		}
		if !rewardedad.VerifyHuijingSign(key, transID, input.Sign) {
			s.log.Warn("rewarded ad callback: bad signature",
				zap.Int64("appid", input.AppID), zap.String("transId", transID))
			return false, "bad_sign"
		}
	}

	now := time.Now()
	userID := s.parseUserToken(input.AppID, strings.TrimSpace(input.UserID))
	view, err := s.pg.RecordRewardedAdView(ctx, rewardedad.RecordInput{
		AppID:        input.AppID,
		UserID:       userID,
		TransID:      transID,
		Side:         rewardedad.SideServer,
		Scene:        rewardedad.SceneFromExtra(input.Extra),
		PlacementID:  strings.TrimSpace(input.PlacementID),
		RewardName:   strings.TrimSpace(input.RewardName),
		RewardAmount: input.RewardAmount,
		NetworkID:    strings.TrimSpace(input.NetworkID),
		Extra:        input.Extra,
		Now:          now.UTC(),
	}, s.policy(cfg, now))
	if err != nil {
		s.log.Error("rewarded ad callback: record failed", zap.Int64("appid", input.AppID),
			zap.String("transId", transID), zap.Error(err))
		return false, "internal"
	}
	if view.Status == rewardedad.StatusRejected {
		return false, view.Reason
	}
	return true, view.Status
}

// ── 内部 ──

func (s *RewardedAdService) policy(cfg *rewardedad.Config, now time.Time) rewardedad.SettlePolicy {
	policy := rewardedad.SettlePolicy{DayStart: rewardedAdDayStart(now, timeutil.DefaultLocation())}
	if cfg == nil {
		return policy
	}
	policy.Enabled = cfg.Enabled
	policy.VerifyMode = cfg.VerifyMode
	policy.DailyLimit = cfg.DailyLimit
	policy.Scenes = cfg.Scenes
	return policy
}

func rewardedAdDayStart(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func (s *RewardedAdService) planNames(ctx context.Context, appID int64, scenes []rewardedad.Scene) map[int64]string {
	names := map[int64]string{}
	for _, scene := range scenes {
		for _, reward := range scene.Rewards {
			if reward.Type != cardkeydomain.RewardVipPlan {
				continue
			}
			if len(names) == 0 {
				plans, err := s.pg.ListVipPlans(ctx, appID, false)
				if err != nil {
					return names
				}
				for _, plan := range plans {
					names[plan.ID] = plan.Name
				}
			}
			return names
		}
	}
	return names
}

// userToken 传给广告 SDK 的用户标识：「用户 ID.签名」。
//
// 平台的回调签名只覆盖 trans_id，不覆盖 userId —— 不带签名的话，谁能伪造一次回调，
// 谁就能把奖励记到任意账号头上。带上签名后，回调里的 userId 只能是我们发出去的那一个。
func (s *RewardedAdService) userToken(appID, userID int64) string {
	return strconv.FormatInt(userID, 10) + "." + s.userTag(appID, userID)
}

// AdUserToken 同一个带签名的用户标识，供开屏等其他广告位传给 SDK：
// 平台后台里同一个人的激励视频与开屏展示对得上，广告数据都挂在账号上。
func (s *RewardedAdService) AdUserToken(appID, userID int64) string {
	return s.userToken(appID, userID)
}

func (s *RewardedAdService) userTag(appID, userID int64) string {
	mac := hmac.New(sha256.New, s.userKey)
	mac.Write([]byte(strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(userID, 10)))
	return hex.EncodeToString(mac.Sum(nil))[:rewardedAdUserTagLen]
}

// parseUserToken 认出回调里的用户；认不出返回 0（记录仍落库，结论是 user_not_found）。
func (s *RewardedAdService) parseUserToken(appID int64, token string) int64 {
	idPart, tag, ok := strings.Cut(token, ".")
	if !ok {
		return 0
	}
	userID, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || userID <= 0 {
		return 0
	}
	if !hmac.Equal([]byte(strings.ToLower(tag)), []byte(s.userTag(appID, userID))) {
		return 0
	}
	return userID
}

func (s *RewardedAdService) translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrRewardedAdViewNotFound):
		return apperrors.New(errCodeRewardedAdViewNotFound, http.StatusNotFound, "观看记录不存在")
	case errors.Is(err, pgrepo.ErrRewardedAdUserMismatch):
		return apperrors.New(errCodeRewardedAdUserMismatch, http.StatusForbidden, "这次观看不属于当前账号")
	case errors.Is(err, pgrepo.ErrRewardedAdAlreadyGranted):
		return apperrors.New(errCodeRewardedAdAlreadyGranted, http.StatusConflict, "这条记录已经发放过奖励")
	case pgrepo.IsUniqueViolation(err):
		s.log.Warn("rewarded ad view hit unique constraint", zap.Error(err))
		return apperrors.New(errCodeRewardedAdAlreadyGranted, http.StatusConflict, "这次观看正在结算，请稍后重试")
	}
	return err
}
