package service

import (
	"context"
	"fmt"
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
		Name:        "high_frequency_block",
		Window:      10 * time.Minute,
		Threshold:   300,
		BanDuration: 30 * time.Minute,
		Severity:    firewalldomain.SeverityMedium,
	},
}

// IPBanService IP 封禁业务逻辑
type IPBanService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	redis     *redisrepo.IPBanRepository
	location  *LocationService
	autoRules []firewalldomain.AutoBanRule
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

// ──────────────────────────────────────
// BanChecker 接口实现（供防火墙中间件调用）
// ──────────────────────────────────────

// IsBanned 检查 IP 是否被封禁
func (s *IPBanService) IsBanned(ctx context.Context, ip string) (bool, error) {
	return s.redis.IsBanned(ctx, ip)
}

// ──────────────────────────────────────
// 手动封禁/解封（管理员 API）
// ──────────────────────────────────────

// BanIP 手动封禁 IP
func (s *IPBanService) BanIP(ctx context.Context, ip string, reason string, durationSec int64, adminID int64) (*firewalldomain.IPBan, error) {
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

	// GeoIP 解析
	ban := firewalldomain.IPBan{
		IP:          ip,
		Reason:      reason,
		Source:      "manual",
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

	// 写入 Redis
	if err := s.redis.SetBan(ctx, ip, redisrepo.BanMeta{
		BanID:    id,
		Reason:   reason,
		Source:   "manual",
		BannedAt: time.Now().UTC().Format(time.RFC3339),
	}, duration); err != nil {
		s.log.Warn("写入 Redis 封禁失败", zap.String("ip", ip), zap.Error(err))
	}

	s.log.Info("手动封禁 IP",
		zap.Int64("ban_id", id),
		zap.String("ip", ip),
		zap.String("reason", reason),
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
		if len(rule.SeverityFilter) > 0 {
			count, err = s.pg.CountRecentBlocksBySeverity(ctx, ip, since, rule.SeverityFilter)
		} else {
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
