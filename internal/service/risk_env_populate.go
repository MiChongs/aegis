package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/timeutil"

	redisrate "github.com/go-redis/redis_rate/v10"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ════════════════════════════════════════════════════════════
//  环境装配
// ════════════════════════════════════════════════════════════

func (s *RiskService) buildEvalEnv(ctx context.Context, req securitydomain.RiskEvalRequest, dryRun bool) *RiskEvalEnv {
	now := timeutil.Now()
	env := newRiskEvalEnv(now)

	env.Scene = req.Scene
	env.IP = strings.TrimSpace(req.IP)
	env.DeviceID = strings.TrimSpace(req.DeviceID)
	env.Account = strings.TrimSpace(req.Account)
	if env.Account == "" {
		env.Account = strings.TrimSpace(toString(req.Extra["account"]))
	}
	if req.AppID != nil {
		env.AppID = *req.AppID
	}
	if req.UserID != nil {
		env.UserID = *req.UserID
	}
	env.CaptchaPassed = req.CaptchaPassed
	env.applyUserAgent(req.UserAgent)
	env.AccountKind, env.EmailDomain, _ = accountShape(env.Account)

	// 先认人：登录 / 找回密码只带登录名，认出用户之后账号维度的计数与已登录场景归到同一主体上
	s.populateUserEnv(ctx, env)
	// 模拟与重放不该改动任何计数：模拟一百次不该让真实用户被「IP 高频」拦下。
	if !dryRun {
		s.populateRateLimitEnv(ctx, req, env)
	}
	s.populateVelocityEnv(ctx, env, dryRun)
	s.populateSceneVelocityEnv(ctx, env, dryRun)
	s.populateDeviceEnv(ctx, env)
	s.populateNetworkEnv(ctx, env)
	s.populateAccountEnv(ctx, env)

	for k, v := range req.Extra {
		if k == "account" {
			continue
		}
		env.Extra[k] = v
	}
	return env
}

// riskSubjectKey 账号维度的主体键：已登录用 user，否则用「应用 + 登录名」。
// 已登录场景（抽奖）只有 user_id，登录场景只有账号名 —— 两者分开记，同一个人的历史就断成两截，
// 所以登录名一律先按应用归一到小写。
func riskSubjectKey(env *RiskEvalEnv) string {
	if env.UserID > 0 {
		return fmt.Sprintf("u:%d", env.UserID)
	}
	if env.Account != "" {
		return fmt.Sprintf("a:%d:%s", env.AppID, strings.ToLower(env.Account))
	}
	return ""
}

// populateUserEnv 认出用户并算出账号年龄
func (s *RiskService) populateUserEnv(ctx context.Context, env *RiskEvalEnv) {
	if s.pg == nil || (env.UserID <= 0 && (env.AppID <= 0 || env.Account == "")) {
		return
	}
	userID, createdAt, err := s.pg.RiskUserFacts(ctx, env.AppID, env.UserID, env.Account)
	if err != nil {
		s.logWarn("查询用户事实失败", zap.Error(err))
		return
	}
	if createdAt == nil {
		return
	}
	env.UserKnown = true
	env.UserAgeHours = timeutil.Since(*createdAt).Hours()
	if env.UserID == 0 {
		env.UserID = userID
	}
}

// populateAccountEnv 账号维度：近 7 天高危次数、上一次归属地（依赖网络维度先解析出归属国）。
func (s *RiskService) populateAccountEnv(ctx context.Context, env *RiskEvalEnv) {
	subject := riskSubjectKey(env)
	if s.redis == nil || subject == "" {
		return
	}
	pipe := s.redis.Pipeline()
	trailCmd := pipe.Get(ctx, s.riskKey("trail", "7d", subject))
	geoCmd := pipe.Get(ctx, s.riskKey("lastgeo", "acct", subject))
	_, _ = pipe.Exec(ctx)
	if v, err := trailCmd.Int64(); err == nil {
		env.AccountRiskTrail = v
	}
	if v, err := geoCmd.Result(); err == nil {
		env.AccountLastCountry = v
	}
	if env.AccountLastCountry != "" && env.GeoKnown && env.GeoCountry != "" &&
		!strings.EqualFold(env.AccountLastCountry, env.GeoCountry) {
		env.GeoChanged = true
	}
}

// rememberAccountFacts 评估之后更新账号维度的跨请求事实
func (s *RiskService) rememberAccountFacts(ctx context.Context, env *RiskEvalEnv, level string) {
	s.rememberAccountFact(ctx, riskAccountFact{Subject: riskSubjectKey(env), Level: level, GeoKnown: env.GeoKnown,
		GeoCountry: env.GeoCountry})
}

// rememberAccountFact 账号的近期高风险次数与上次归属地（风控留痕写库之后调用，见 risk_trail.go）
func (s *RiskService) rememberAccountFact(ctx context.Context, fact riskAccountFact) {
	if s.redis == nil || fact.Subject == "" {
		return
	}
	pipe := s.redis.Pipeline()
	if fact.Level == securitydomain.LevelHigh || fact.Level == securitydomain.LevelCritical {
		key := s.riskKey("trail", "7d", fact.Subject)
		pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, 7*24*time.Hour)
	}
	if fact.GeoKnown && fact.GeoCountry != "" {
		pipe.Set(ctx, s.riskKey("lastgeo", "acct", fact.Subject), fact.GeoCountry, 30*24*time.Hour)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		s.logWarn("更新账号风险事实失败", zap.Error(err))
	}
}

// populateSceneVelocityEnv 场景小时频次：同一主体 / 设备 / IP 一小时内在本场景来了几次。
// 按整点分桶的固定窗口计数，INCR 一次往返；模拟时只读不写。
func (s *RiskService) populateSceneVelocityEnv(ctx context.Context, env *RiskEvalEnv, dryRun bool) {
	if s.redis == nil || env.Scene == "" {
		return
	}
	bucket := strconv.FormatInt(env.evaluatedAt.Unix()/3600, 10)
	type target struct {
		value string
		dest  *int64
		kind  string
	}
	targets := []target{
		{riskSubjectKey(env), &env.SceneSubjectHourly, "subject"},
		{env.DeviceID, &env.SceneDeviceHourly, "device"},
		{env.IP, &env.SceneIPHourly, "ip"},
	}
	pipe := s.redis.Pipeline()
	cmds := make([]any, len(targets))
	for i, t := range targets {
		if t.value == "" {
			continue
		}
		key := s.riskKey("scenehr", env.Scene+":"+t.kind+":"+bucket, t.value)
		if dryRun {
			cmds[i] = pipe.Get(ctx, key)
			continue
		}
		cmds[i] = pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, 2*time.Hour)
	}
	_, _ = pipe.Exec(ctx)
	for i, t := range targets {
		switch cmd := cmds[i].(type) {
		case *redislib.IntCmd:
			*t.dest = cmd.Val()
		case *redislib.StringCmd:
			if v, err := cmd.Int64(); err == nil {
				*t.dest = v
			}
		}
	}
}

// applyEnvOverrides 用模拟器传入的覆写值改写环境。
// 走 JSON 往返而不是逐字段 switch：字段名与 json 标签一致，
// 加一个新变量时这里零改动，不会出现「加了变量但模拟器覆写不到」。
func applyEnvOverrides(env *RiskEvalEnv, overrides map[string]any) {
	if len(overrides) == 0 {
		return
	}
	raw, err := json.Marshal(overrides)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, env)
	if env.Extra == nil {
		env.Extra = map[string]any{}
	}
}

func (s *RiskService) populateDeviceEnv(ctx context.Context, env *RiskEvalEnv) {
	if env.DeviceID == "" {
		return
	}
	fp, err := s.pg.GetDeviceFingerprint(ctx, env.DeviceID)
	if err != nil {
		s.logWarn("查询设备指纹失败", zap.String("deviceId", env.DeviceID), zap.Error(err))
		return
	}
	if fp == nil {
		return
	}
	env.DeviceKnown = true
	env.DeviceAgeHours = timeutil.Since(fp.FirstSeenAt).Hours()
	env.DeviceSeenCount = int64(fp.SeenCount)
	env.DeviceRiskTag = fp.RiskTag
	env.DeviceBlocked = fp.RiskTag == securitydomain.TagBlocked
}

func (s *RiskService) populateNetworkEnv(ctx context.Context, env *RiskEvalEnv) {
	if env.IP == "" {
		return
	}
	rt := s.runtime()

	// 归属地走 LocationService（宝塔归属地接口 + 缓存），免费、零配额。
	// 外部情报源只补充「是不是代理 / 信誉分多少」这类归属地给不出的判断。
	if rt.location != nil {
		loc := rt.location.Resolve(ctx, env.IP)
		env.GeoCountry = firstNonEmptyString(loc.CountryCode, loc.Country)
		env.GeoRegion = loc.Region
		env.GeoCity = loc.City
		env.GeoKnown = env.GeoCountry != ""
		env.IPASN = loc.Network.ASN
		env.IPISP = firstNonEmptyString(loc.ISP, loc.Network.Organization)
	}

	record := s.resolveIPRisk(ctx, env.IP)
	if record == nil {
		return
	}
	env.IPKnown = true
	env.IPIsProxy = record.IsProxy
	env.IPIsVPN = record.IsVPN
	env.IPIsTor = record.IsTor
	env.IPIsDatacenter = record.IsDatacenter
	env.IPRiskScore = record.RiskScore
	env.IPRiskTag = record.RiskTag
	env.IPTrusted = record.RiskTag == securitydomain.TagTrusted
	env.IPTotalBlocks = record.TotalBlocks
	if record.Country != "" {
		env.GeoCountry = record.Country
		env.GeoKnown = true
	}
	if record.Region != "" {
		env.GeoRegion = record.Region
	}
	env.IPASN = firstNonEmptyString(record.ASN, env.IPASN)
	env.IPISP = firstNonEmptyString(record.ISP, env.IPISP)
}

// populateVelocityEnv 统计「一个 IP / 设备在窗口内碰过多少个不同账号」。
//
// 用 Redis HyperLogLog（PFADD/PFCOUNT）而不是 Set：撞库场景下一个 IP 一小时
// 能试上万个账号，用 Set 存会把内存吃光；HLL 恒定 12KB、误差 0.81%，
// 而这里要回答的是「是不是远超阈值」而非精确计数，正是它的适用场景。
func (s *RiskService) populateVelocityEnv(ctx context.Context, env *RiskEvalEnv, dryRun bool) {
	if s.redis == nil {
		return
	}
	window := int64(velocityWindow / time.Second)
	// 已登录场景没有登录名，用主体键代替 —— 否则「同设备多账号」在抽奖、邀请上永远是 0
	member := env.Account
	if member == "" {
		member = riskSubjectKey(env)
	}

	if env.IP != "" && member != "" {
		env.IPAccountsSeen = s.trackCardinality(ctx, "ipacct", env.IP, member, window, dryRun)
	}
	if env.DeviceID != "" && member != "" {
		env.DeviceAccountsSeen = s.trackCardinality(ctx, "devacct", env.DeviceID, member, window, dryRun)
	}
	if member != "" && env.IP != "" {
		env.AccountIPsSeen = s.trackCardinality(ctx, "acctip", member, env.IP, window, dryRun)
	}
	if env.DeviceID != "" && env.IP != "" {
		env.DeviceIPsSeen = s.trackCardinality(ctx, "devip", env.DeviceID, env.IP, window, dryRun)
	}
}

func (s *RiskService) trackCardinality(ctx context.Context, scope, subject, member string, ttlSeconds int64, dryRun bool) int64 {
	key := s.riskKey("hll", scope, subject)
	if dryRun {
		count, err := s.redis.PFCount(ctx, key).Result()
		if err != nil {
			return 0
		}
		return count
	}
	pipe := s.redis.Pipeline()
	pipe.PFAdd(ctx, key, member)
	countCmd := pipe.PFCount(ctx, key)
	pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		s.logWarn("基数统计失败", zap.String("scope", scope), zap.Error(err))
		return 0
	}
	return countCmd.Val()
}

func (s *RiskService) populateRateLimitEnv(ctx context.Context, req securitydomain.RiskEvalRequest, env *RiskEvalEnv) {
	rt := s.runtime()
	if s.redis == nil {
		return
	}

	if s.rateLimiter == nil || !rt.cfg.RateLimit.Enabled {
		// 未启用 GCRA 限流时退回朴素计数器，至少让 ip_frequency 规则有数可用。
		ipReqKey := s.riskKey("count", req.Scene, env.IP)
		count, err := s.redis.Incr(ctx, ipReqKey).Result()
		if err != nil {
			return
		}
		if count == 1 {
			s.redis.Expire(ctx, ipReqKey, time.Minute)
		}
		env.IPRequestCount = count
		return
	}

	appID := ""
	if req.AppID != nil {
		appID = strconv.FormatInt(*req.AppID, 10)
	}
	account := normalizeRiskDimension(env.Account)
	deviceID := normalizeRiskDimension(env.DeviceID)
	scene := normalizeRiskDimension(req.Scene)
	ip := normalizeRiskDimension(env.IP)

	if count, limited, ok := s.takeRateSample(ctx, "ip", rt.cfg.RateLimit.IPPerMinute, scene, appID, ip); ok {
		env.IPRequestCount = count
		env.IPRateLimited = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "account", rt.cfg.RateLimit.AccountPerMinute, scene, appID, account); ok {
		env.AccountRequestCount = count
		env.AccountRateLimited = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "device", rt.cfg.RateLimit.DevicePerMinute, scene, appID, deviceID); ok {
		env.DeviceRequestCount = count
		env.DeviceRateLimited = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "account_device", rt.cfg.RateLimit.AccountDevicePerMinute, scene, appID, account, deviceID); ok {
		env.AccountDeviceRequestCount = count
		env.AccountDeviceRateLimited = limited
	}
}

func (s *RiskService) takeRateSample(ctx context.Context, scope string, perMinute int, parts ...string) (int64, bool, bool) {
	if s.rateLimiter == nil || perMinute <= 0 {
		return 0, false, false
	}
	// 维度值为空时不该占用配额：所有「没带账号的请求」会被归成同一个 key，
	// 于是它们互相把对方顶到限流线上。
	if !hasNonEmptyDimension(parts) {
		return 0, false, false
	}
	key := s.riskRateLimitKey(scope, parts...)
	result, err := s.rateLimiter.Allow(ctx, key, redisrate.PerMinute(perMinute))
	if err != nil {
		s.logWarn("risk rate limit check failed", zap.String("scope", scope), zap.Error(err))
		return 0, false, false
	}
	used := int64(perMinute - result.Remaining)
	used = max(used, 0)
	used = min(used, int64(perMinute))
	return used, result.Allowed == 0, true
}

// hasNonEmptyDimension 判断限流维度里是否至少有一个实际取值。
// parts 的前两项是场景与应用 ID（恒有值），真正的维度从第三项起。
func hasNonEmptyDimension(parts []string) bool {
	if len(parts) <= 2 {
		return false
	}
	for _, part := range parts[2:] {
		if strings.TrimSpace(part) != "" {
			return true
		}
	}
	return false
}

func (s *RiskService) riskRateLimitKey(scope string, parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = normalizeRiskDimension(part); part != "" {
			filtered = append(filtered, part)
		}
	}
	sum := sha1.Sum([]byte(strings.Join(filtered, "|")))
	return s.riskKey("rate", scope, hex.EncodeToString(sum[:]))
}

// riskKey 统一的 Redis 键构造。维度值可能很长（UA 派生的设备号）或含冒号，
// 一律哈希后再拼，避免键长失控与分隔符歧义。
func (s *RiskService) riskKey(kind, scope, subject string) string {
	if len(subject) > 48 || strings.ContainsAny(subject, ": \t\n") {
		sum := sha1.Sum([]byte(subject))
		subject = hex.EncodeToString(sum[:])
	}
	return fmt.Sprintf("%s:risk:%s:%s:%s", s.keyPrefix, kind, scope, subject)
}

// ════════════════════════════════════════════════════════════
//  IP 情报解析
// ════════════════════════════════════════════════════════════

func (s *RiskService) resolveIPRisk(ctx context.Context, ip string) *securitydomain.IPRiskRecord {
	rt := s.runtime()
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	value, err, _ := s.ipLookupFlight.Do(ip, func() (any, error) {
		if cached := s.readIPRiskCache(ctx, ip); cached != nil {
			return cached, nil
		}

		local, err := s.pg.GetIPRisk(ctx, ip)
		if err != nil {
			s.logWarn("load local ip risk failed", zap.String("ip", ip), zap.Error(err))
		}
		// 人工结论不被情报源覆盖：管理员加白 / 拉黑之后，
		// 下一次情报刷新不该把这个决定悄悄改回去。
		if local != nil && (local.Source == riskSourceManual || timeutil.Since(local.LastSeenAt) <= rt.cfg.IPReputation.CacheTTL) {
			s.writeIPRiskCache(ctx, local)
			return local, nil
		}
		if rt.provider == nil {
			return local, nil
		}

		record, err := rt.provider.Lookup(ctx, ip)
		if err != nil {
			if local != nil && rt.cfg.IPReputation.AllowStale {
				return local, nil
			}
			return nil, err
		}
		record = normalizeIPRiskRecord(ip, record)
		if record == nil {
			return local, nil
		}
		record.Source = rt.provider.Name()
		stored, upsertErr := s.pg.UpsertIPRisk(ctx, *record)
		if upsertErr != nil {
			s.logWarn("persist ip reputation failed", zap.String("ip", ip), zap.String("provider", rt.provider.Name()), zap.Error(upsertErr))
			s.writeIPRiskCache(ctx, record)
			return record, nil
		}
		s.writeIPRiskCache(ctx, stored)
		return stored, nil
	})
	if err != nil {
		s.logWarn("resolve ip reputation failed", zap.String("ip", ip), zap.Error(err))
		return nil
	}
	record, _ := value.(*securitydomain.IPRiskRecord)
	return record
}

const riskSourceManual = "manual"

func (s *RiskService) readIPRiskCache(ctx context.Context, ip string) *securitydomain.IPRiskRecord {
	rt := s.runtime()
	if s.redis == nil || rt.cfg.IPReputation.CacheTTL <= 0 {
		return nil
	}
	raw, err := s.redis.Get(ctx, s.ipRiskCacheKey(ip)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	var record securitydomain.IPRiskRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return normalizeIPRiskRecord(ip, &record)
}

func (s *RiskService) writeIPRiskCache(ctx context.Context, record *securitydomain.IPRiskRecord) {
	rt := s.runtime()
	if s.redis == nil || record == nil || rt.cfg.IPReputation.CacheTTL <= 0 {
		return
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := s.redis.Set(ctx, s.ipRiskCacheKey(record.IP), payload, rt.cfg.IPReputation.CacheTTL).Err(); err != nil {
		s.logWarn("write ip risk cache failed", zap.String("ip", record.IP), zap.Error(err))
	}
}

func (s *RiskService) invalidateIPRiskCache(ctx context.Context, ip string) {
	if s.redis == nil || strings.TrimSpace(ip) == "" {
		return
	}
	s.redis.Del(ctx, s.ipRiskCacheKey(ip))
}

func (s *RiskService) ipRiskCacheKey(ip string) string {
	return fmt.Sprintf("%s:risk:iprep:%s", s.keyPrefix, strings.TrimSpace(ip))
}
