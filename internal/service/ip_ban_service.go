package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"go.uber.org/zap"
)

// 默认自动封禁规则（从严到宽排列，首个匹配即触发）
// 阈值设置为宽松级别，避免正常管理操作误封
var defaultAutoBanRules = []firewalldomain.AutoBanRule{
	{
		Name:        "extreme_abuse",
		Window:      1 * time.Hour,
		Threshold:   5000,
		BanDuration: 24 * time.Hour,
		Severity:    firewalldomain.SeverityCritical,
	},
	{
		Name:           "critical_attack",
		Window:         30 * time.Minute,
		Threshold:      200,
		BanDuration:    6 * time.Hour,
		Severity:       firewalldomain.SeverityCritical,
		SeverityFilter: []string{firewalldomain.SeverityCritical},
	},
	{
		Name:        "sustained_attack",
		Window:      1 * time.Hour,
		Threshold:   1000,
		BanDuration: 2 * time.Hour,
		Severity:    firewalldomain.SeverityHigh,
	},
	{
		// fail2ban 式限流升级：短窗口内反复触发限流 → 临时封禁
		// （限流本身只拒绝单次请求，恶意源会持续打满；此规则把它挡在门外）
		Name:         "rate_limit_abuse",
		Window:       10 * time.Minute,
		Threshold:    120,
		BanDuration:  1 * time.Hour,
		Severity:     firewalldomain.SeverityHigh,
		ReasonFilter: []string{"rate_limited"},
	},
	{
		Name:        "high_frequency_block",
		Window:      10 * time.Minute,
		Threshold:   300,
		BanDuration: 30 * time.Minute,
		Severity:    firewalldomain.SeverityMedium,
	},
}

// ParseAutoBanRules 解析 FIREWALL_AUTO_BAN_RULES 环境变量（JSON 数组）。
// 字段示例：
//
//	[{"name":"rate_limit_abuse","window":"10m","threshold":120,
//	  "banDuration":"1h","severity":"high","reasonFilter":["rate_limited"]}]
//
// window / banDuration 使用 Go duration 字符串；banDuration 为 "0" 时表示永久封禁。
func ParseAutoBanRules(raw string) ([]firewalldomain.AutoBanRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var items []struct {
		Name           string   `json:"name"`
		Window         string   `json:"window"`
		Threshold      int      `json:"threshold"`
		BanDuration    string   `json:"banDuration"`
		Severity       string   `json:"severity"`
		SeverityFilter []string `json:"severityFilter"`
		ReasonFilter   []string `json:"reasonFilter"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("解析自动封禁规则 JSON 失败: %w", err)
	}
	rules := make([]firewalldomain.AutoBanRule, 0, len(items))
	for i, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			return nil, fmt.Errorf("规则 #%d 缺少 name", i+1)
		}
		window, err := time.ParseDuration(item.Window)
		if err != nil || window <= 0 {
			return nil, fmt.Errorf("规则 %s 的 window 无效（需正 duration，如 10m）", item.Name)
		}
		if item.Threshold <= 0 {
			return nil, fmt.Errorf("规则 %s 的 threshold 必须大于 0", item.Name)
		}
		var banDuration time.Duration
		if d := strings.TrimSpace(item.BanDuration); d != "" && d != "0" {
			banDuration, err = time.ParseDuration(d)
			if err != nil || banDuration < 0 {
				return nil, fmt.Errorf("规则 %s 的 banDuration 无效（duration 字符串，0=永久）", item.Name)
			}
		}
		severity := strings.ToLower(strings.TrimSpace(item.Severity))
		switch severity {
		case firewalldomain.SeverityLow, firewalldomain.SeverityMedium,
			firewalldomain.SeverityHigh, firewalldomain.SeverityCritical:
		case "":
			severity = firewalldomain.SeverityMedium
		default:
			return nil, fmt.Errorf("规则 %s 的 severity 无效（low/medium/high/critical）", item.Name)
		}
		rules = append(rules, firewalldomain.AutoBanRule{
			Name:           strings.TrimSpace(item.Name),
			Window:         window,
			Threshold:      item.Threshold,
			BanDuration:    banDuration,
			Severity:       severity,
			SeverityFilter: item.SeverityFilter,
			ReasonFilter:   item.ReasonFilter,
		})
	}
	return rules, nil
}

// ReadBanMode 返回当前全局默认封禁模式；由外部（通常是 PlatformSettings 或 FirewallConfig）注入。
type ReadBanMode func() string

// IPBanService IP 封禁业务逻辑
type IPBanService struct {
	readDefaultMode ReadBanMode
	geo             *GeoBanService
	fences          *GeoFenceService
	log             *zap.Logger
	pg              *pgrepo.Repository
	redis           *redisrepo.IPBanRepository
	location        *LocationService
	autoRules       []firewalldomain.AutoBanRule
}

// NewIPBanService 创建 IP 封禁服务
func NewIPBanService(log *zap.Logger, pg *pgrepo.Repository, redis *redisrepo.IPBanRepository, location *LocationService) *IPBanService {
	return &IPBanService{
		log:       log,
		pg:        pg,
		redis:     redis,
		location:  location,
		autoRules: defaultAutoBanRules,
	}
}

// SetDefaultModeReader 注入全局默认模式读取器（支持动态热更）。
func (s *IPBanService) SetDefaultModeReader(r ReadBanMode) {
	s.readDefaultMode = r
}

// SetGeoBanService 注入地域封禁服务；设置后 CheckBan 会在 IP 精确封禁
// 未命中时回退到地域规则评估（IP 优先，Geo 其次）。
func (s *IPBanService) SetGeoBanService(g *GeoBanService) {
	s.geo = g
}

// SetGeoFenceService 注入地理围栏服务；在地域封禁之后评估（粒度从细到粗）。
func (s *IPBanService) SetGeoFenceService(f *GeoFenceService) {
	s.fences = f
}

// SetAutoBanRules 覆盖默认自动封禁规则（FIREWALL_AUTO_BAN_RULES 配置驱动）。
// 传入空切片时保持默认规则不变。
func (s *IPBanService) SetAutoBanRules(rules []firewalldomain.AutoBanRule) {
	if len(rules) == 0 {
		return
	}
	s.autoRules = rules
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.Name)
	}
	s.log.Info("自动封禁规则已由配置覆盖", zap.Strings("rules", names))
}

// DefaultMode 返回当前全局默认封禁模式；未设置时回退到 "forbidden"。
func (s *IPBanService) DefaultMode() string {
	if s == nil || s.readDefaultMode == nil {
		return firewalldomain.BanModeForbidden
	}
	m := firewalldomain.NormalizeBanMode(s.readDefaultMode(), firewalldomain.BanModeForbidden)
	return m
}

// ──────────────────────────────────────
// BanChecker 接口实现（供防火墙中间件调用）
// ──────────────────────────────────────

// IsBanned 检查 IP 是否被封禁（保留向后兼容）
func (s *IPBanService) IsBanned(ctx context.Context, ip string) (bool, error) {
	return s.redis.IsBanned(ctx, ip)
}

// CheckBan 检查 IP 封禁并返回完整决策。
//
// 顺序：
//  1. 精确 IP 封禁（Redis，O(1)）
//  2. 未命中 → 地域 / ASN / ISP 规则（内存匹配，若已注入 GeoBanService）
//  3. 未命中 → 地理围栏（内存几何判定，若已注入 GeoFenceService）
//
// 实现 middleware.ExtendedBanChecker 接口。
func (s *IPBanService) CheckBan(ctx context.Context, ip string) (firewalldomain.BanDecision, error) {
	// 1) 精确 IP 封禁
	meta, err := s.redis.GetBanMeta(ctx, ip)
	if err == nil && meta != nil {
		return firewalldomain.BanDecision{
			Banned:  true,
			Mode:    meta.Mode, // 空时由 middleware 回退到全局默认
			Reason:  meta.Reason,
			BanID:   meta.BanID,
			DelayMs: meta.DelayMs,
		}, nil
	}
	if err == nil {
		if banned, _ := s.redis.IsBanned(ctx, ip); banned {
			return firewalldomain.BanDecision{
				Banned: true,
				Mode:   "",
				Reason: "banned_ip",
			}, nil
		}
	}

	// 2) 地域封禁（IP 未命中时回退）
	if s.geo != nil {
		dec, _ := s.geo.CheckIP(ctx, ip)
		if dec.Banned {
			return dec, nil
		}
	}

	// 3) 地理围栏（坐标级判定，内存几何计算）
	if s.fences != nil {
		dec, _ := s.fences.CheckIP(ctx, ip)
		if dec.Banned {
			return dec, nil
		}
	}

	return firewalldomain.BanDecision{Banned: false}, err
}

// ──────────────────────────────────────
// 手动封禁/解封（管理员 API）
// ──────────────────────────────────────

// BanIPOptions 手动封禁的可选参数。零值即走默认行为。
type BanIPOptions struct {
	Mode    string // 见 firewall.BanMode* 常量；空 → 使用全局默认（由 middleware 层决定）
	DelayMs int    // tarpit 模式的延迟毫秒，0 → 使用 config.TarpitDelayMs
}

// BanIP 手动封禁 IP（保留原签名，内部委托给 BanIPWithOptions）。
func (s *IPBanService) BanIP(ctx context.Context, ip string, reason string, durationSec int64, adminID int64) (*firewalldomain.IPBan, error) {
	return s.BanIPWithOptions(ctx, ip, reason, durationSec, adminID, BanIPOptions{})
}

// BanIPWithOptions 手动封禁 IP，支持指定响应模式。
func (s *IPBanService) BanIPWithOptions(ctx context.Context, ip string, reason string, durationSec int64, adminID int64, opts BanIPOptions) (*firewalldomain.IPBan, error) {
	// 检查是否已封禁
	existing, _ := s.pg.GetActiveIPBan(ctx, ip)
	if existing != nil {
		return nil, fmt.Errorf("该 IP 已被封禁（ID: %d）", existing.ID)
	}

	var expiresAt *time.Time
	duration := time.Duration(durationSec) * time.Second
	if durationSec > 0 {
		t := time.Now().UTC().Add(duration)
		expiresAt = &t
	}

	// 规范化 mode（未指定时存空值，由 middleware 回退到 DefaultBanMode）
	mode := firewalldomain.NormalizeBanMode(opts.Mode, "")
	if opts.Mode == "" {
		mode = ""
	}

	// GeoIP 解析
	ban := firewalldomain.IPBan{
		IP:          ip,
		Reason:      reason,
		Source:      "manual",
		Mode:        mode,
		TriggerRule: fmt.Sprintf("admin:%d", adminID),
		Severity:    firewalldomain.SeverityHigh,
		Duration:    durationSec,
		ExpiresAt:   expiresAt,
	}
	if s.location != nil {
		loc := s.location.Resolve(ctx, ip)
		ban.Country = loc.Country
		ban.CountryCode = loc.CountryCode
		ban.Region = loc.Region
		ban.City = loc.City
		ban.ISP = loc.ISP
	}

	id, err := s.pg.InsertIPBan(ctx, ban)
	if err != nil {
		return nil, fmt.Errorf("插入封禁记录失败: %w", err)
	}
	if id == 0 {
		return nil, fmt.Errorf("该 IP 已被封禁")
	}
	ban.ID = id

	// 写入 Redis（含 mode / delay，供 middleware 读取）
	if err := s.redis.SetBan(ctx, ip, redisrepo.BanMeta{
		BanID:    id,
		Reason:   reason,
		Source:   "manual",
		Mode:     mode,
		DelayMs:  opts.DelayMs,
		BannedAt: time.Now().UTC().Format(time.RFC3339),
	}, duration); err != nil {
		s.log.Warn("写入 Redis 封禁失败", zap.String("ip", ip), zap.Error(err))
	}

	s.log.Info("手动封禁 IP",
		zap.Int64("ban_id", id),
		zap.String("ip", ip),
		zap.String("reason", reason),
		zap.String("mode", mode),
		zap.Int64("duration_sec", durationSec),
		zap.Int64("admin_id", adminID),
	)
	return &ban, nil
}

// UnbanIP 手动解封
func (s *IPBanService) UnbanIP(ctx context.Context, banID int64, adminID int64) error {
	ban, err := s.pg.GetIPBan(ctx, banID)
	if err != nil {
		return fmt.Errorf("查询封禁记录失败: %w", err)
	}
	if ban == nil {
		return fmt.Errorf("封禁记录不存在")
	}
	if ban.Status != "active" {
		return fmt.Errorf("该封禁记录已非活跃状态（当前: %s）", ban.Status)
	}

	if err := s.pg.RevokeIPBan(ctx, banID, adminID); err != nil {
		return err
	}

	// 从 Redis 移除
	if err := s.redis.RemoveBan(ctx, ban.IP); err != nil {
		s.log.Warn("从 Redis 移除封禁失败", zap.String("ip", ban.IP), zap.Error(err))
	}

	s.log.Info("手动解封 IP",
		zap.Int64("ban_id", banID),
		zap.String("ip", ban.IP),
		zap.Int64("admin_id", adminID),
	)
	return nil
}

// ──────────────────────────────────────
// 查询
// ──────────────────────────────────────

// ListBans 分页查询封禁列表
func (s *IPBanService) ListBans(ctx context.Context, filter firewalldomain.IPBanFilter) (*firewalldomain.IPBanPage, error) {
	return s.pg.ListIPBans(ctx, filter)
}

// GetBan 按 ID 读取单条封禁记录（供 handler 审计/详情使用）。
func (s *IPBanService) GetBan(ctx context.Context, id int64) (*firewalldomain.IPBan, error) {
	return s.pg.GetIPBan(ctx, id)
}

// ──────────────────────────────────────
// 自动封禁引擎
// ──────────────────────────────────────

// EvaluateAutoBan 评估是否需要自动封禁（Worker 端调用）
func (s *IPBanService) EvaluateAutoBan(ctx context.Context, ip string) error {
	// 已封禁则跳过
	banned, _ := s.redis.IsBanned(ctx, ip)
	if banned {
		return nil
	}

	// 按规则严重性从高到低匹配
	for _, rule := range s.autoRules {
		since := time.Now().UTC().Add(-rule.Window)

		var count int
		var err error
		switch {
		case len(rule.ReasonFilter) > 0:
			count, err = s.pg.CountRecentBlocksByReason(ctx, ip, since, rule.ReasonFilter)
		case len(rule.SeverityFilter) > 0:
			count, err = s.pg.CountRecentBlocksBySeverity(ctx, ip, since, rule.SeverityFilter)
		default:
			count, err = s.pg.CountRecentBlocks(ctx, ip, since)
		}
		if err != nil {
			s.log.Warn("自动封禁查询拦截次数失败",
				zap.String("ip", ip),
				zap.String("rule", rule.Name),
				zap.Error(err),
			)
			continue
		}

		if count >= rule.Threshold {
			return s.executeBan(ctx, ip, rule, count)
		}
	}
	return nil
}

func (s *IPBanService) executeBan(ctx context.Context, ip string, rule firewalldomain.AutoBanRule, triggerCount int) error {
	durationSec := int64(rule.BanDuration.Seconds())
	var expiresAt *time.Time
	if durationSec > 0 {
		t := time.Now().UTC().Add(rule.BanDuration)
		expiresAt = &t
	}

	reason := fmt.Sprintf("自动封禁：触发规则 %s（%d 次拦截/%s）", rule.Name, triggerCount, rule.Window)

	ban := firewalldomain.IPBan{
		IP:           ip,
		Reason:       reason,
		Source:       "auto",
		TriggerRule:  rule.Name,
		Severity:     rule.Severity,
		Duration:     durationSec,
		ExpiresAt:    expiresAt,
		TriggerCount: triggerCount,
	}
	if s.location != nil {
		loc := s.location.Resolve(ctx, ip)
		ban.Country = loc.Country
		ban.CountryCode = loc.CountryCode
		ban.Region = loc.Region
		ban.City = loc.City
		ban.ISP = loc.ISP
	}

	id, err := s.pg.InsertIPBan(ctx, ban)
	if err != nil {
		return fmt.Errorf("插入自动封禁记录失败: %w", err)
	}
	if id == 0 {
		// ON CONFLICT: 已有活跃封禁，忽略
		return nil
	}

	// 写入 Redis
	if err := s.redis.SetBan(ctx, ip, redisrepo.BanMeta{
		BanID:    id,
		Reason:   reason,
		Source:   "auto",
		BannedAt: time.Now().UTC().Format(time.RFC3339),
	}, rule.BanDuration); err != nil {
		s.log.Warn("自动封禁写入 Redis 失败", zap.String("ip", ip), zap.Error(err))
	}

	durationLabel := "永久"
	if rule.BanDuration > 0 {
		durationLabel = rule.BanDuration.String()
	}
	s.log.Warn("自动封禁 IP",
		zap.Int64("ban_id", id),
		zap.String("ip", ip),
		zap.String("rule", rule.Name),
		zap.Int("trigger_count", triggerCount),
		zap.String("duration", durationLabel),
		zap.String("severity", rule.Severity),
	)
	return nil
}

// ──────────────────────────────────────
// 维护
// ──────────────────────────────────────

// SyncBansToRedis 启动时从 PostgreSQL 同步所有活跃封禁到 Redis
func (s *IPBanService) SyncBansToRedis(ctx context.Context) error {
	bans, err := s.pg.ListActiveIPBanIPs(ctx)
	if err != nil {
		return fmt.Errorf("查询活跃封禁列表失败: %w", err)
	}
	synced := 0
	for _, ban := range bans {
		// 计算剩余 TTL
		var ttl time.Duration
		if ban.ExpiresAt != nil {
			ttl = time.Until(*ban.ExpiresAt)
			if ttl <= 0 {
				continue // 已过期，跳过
			}
		}
		if err := s.redis.SetBan(ctx, ban.IP, redisrepo.BanMeta{
			BanID:    ban.ID,
			Reason:   ban.Reason,
			Source:   ban.Source,
			BannedAt: ban.CreatedAt.Format(time.RFC3339),
		}, ttl); err != nil {
			s.log.Warn("同步封禁到 Redis 失败", zap.String("ip", ban.IP), zap.Error(err))
			continue
		}
		synced++
	}
	s.log.Info("封禁列表同步到 Redis 完成", zap.Int("total", len(bans)), zap.Int("synced", synced))
	return nil
}

// CleanupExpired 清理已过期的封禁记录（PostgreSQL 端标记 expired）
func (s *IPBanService) CleanupExpired(ctx context.Context) (int64, error) {
	expired, err := s.pg.ExpireIPBans(ctx)
	if err != nil {
		return 0, err
	}
	if expired > 0 {
		s.log.Info("封禁记录过期清理完成", zap.Int64("expired", expired))
	}
	return expired, nil
}
