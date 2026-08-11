package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// GeoBanService 地域 / ASN / ISP 封禁服务。
//
// 设计：
//   - 规则保存在 geo_bans 表
//   - 启动时一次性加载，修改时 ReloadFromDB 热更
//   - 所有匹配逻辑走内存中的副本（读多写少，RWMutex）
//   - 命中时异步累计 match_count（避免阻塞请求）
type GeoBanService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	location *LocationService

	mu    sync.RWMutex
	rules []firewalldomain.GeoBan // 仅 enabled 且未过期的规则，按 scopeType/value 内部索引

	version atomic.Int64
}

// NewGeoBanService 构造服务；传入 locationService 以解析 IP → 国家/地区/ASN/ISP。
func NewGeoBanService(log *zap.Logger, pg *pgrepo.Repository, location *LocationService) *GeoBanService {
	s := &GeoBanService{log: log, pg: pg, location: location}
	return s
}

// Initialize 启动时从 DB 加载全部规则。
func (s *GeoBanService) Initialize(ctx context.Context) error {
	return s.ReloadFromDB(ctx)
}

// ReloadFromDB 重新从数据库加载所有启用中规则。
func (s *GeoBanService) ReloadFromDB(ctx context.Context) error {
	all, err := s.pg.ListGeoBans(ctx, true)
	if err != nil {
		return err
	}
	// 过滤已过期
	now := time.Now()
	live := make([]firewalldomain.GeoBan, 0, len(all))
	for _, b := range all {
		if b.ExpiresAt != nil && !b.ExpiresAt.After(now) {
			continue
		}
		live = append(live, b)
	}
	s.mu.Lock()
	s.rules = live
	s.mu.Unlock()
	s.version.Add(1)
	s.log.Info("geo bans reloaded", zap.Int("count", len(live)))
	return nil
}

// ListAll 返回全部规则（含禁用/过期）供管理 UI 用。
func (s *GeoBanService) ListAll(ctx context.Context) ([]firewalldomain.GeoBan, error) {
	return s.pg.ListGeoBans(ctx, false)
}

// GetByID 按 ID 读取单条规则（供 handler 审计 / 详情使用）。
func (s *GeoBanService) GetByID(ctx context.Context, id int64) (*firewalldomain.GeoBan, error) {
	return s.pg.GetGeoBan(ctx, id)
}

// Upsert 创建或更新规则并热重载。
func (s *GeoBanService) Upsert(ctx context.Context, m firewalldomain.GeoBanMutation, adminID *int64) (*firewalldomain.GeoBan, error) {
	m.ScopeType = strings.ToLower(strings.TrimSpace(m.ScopeType))
	m.ScopeValue = strings.TrimSpace(m.ScopeValue)
	m.Mode = firewalldomain.NormalizeBanMode(m.Mode, "")
	if m.Mode == "__default__" { // UI 占位
		m.Mode = ""
	}
	b, err := s.pg.UpsertGeoBan(ctx, m, adminID)
	if err != nil {
		return nil, err
	}
	if err := s.ReloadFromDB(ctx); err != nil {
		s.log.Warn("geo ban reload failed after upsert", zap.Error(err))
	}
	return &b, nil
}

// Toggle 启用/禁用规则。
func (s *GeoBanService) Toggle(ctx context.Context, id int64, enabled bool) error {
	if err := s.pg.UpdateGeoBanStatus(ctx, id, enabled); err != nil {
		return err
	}
	return s.ReloadFromDB(ctx)
}

// Delete 删除规则。
func (s *GeoBanService) Delete(ctx context.Context, id int64) error {
	if err := s.pg.DeleteGeoBan(ctx, id); err != nil {
		return err
	}
	return s.ReloadFromDB(ctx)
}

// CheckIP 对请求 IP 评估是否命中地域封禁规则。
// 未命中返回 {Banned: false}；命中返回决策 + 命中的规则 ID。
// 调用侧应在 IP 精确封禁之后再调用此方法（IP 优先）。
func (s *GeoBanService) CheckIP(ctx context.Context, ip string) (firewalldomain.BanDecision, int64) {
	s.mu.RLock()
	rules := s.rules
	s.mu.RUnlock()
	if len(rules) == 0 || s.location == nil {
		return firewalldomain.BanDecision{}, 0
	}

	loc := s.location.Resolve(ctx, ip)
	if loc.IsPrivate {
		// 私网 IP 不走地域封禁
		return firewalldomain.BanDecision{}, 0
	}

	country := strings.ToUpper(strings.TrimSpace(loc.CountryCode))
	if country == "" {
		country = strings.ToUpper(strings.TrimSpace(loc.Country))
	}
	region := strings.TrimSpace(loc.Region)
	city := strings.TrimSpace(loc.City)
	asn := strings.ToUpper(strings.TrimSpace(loc.Network.ASN))
	isp := strings.TrimSpace(loc.ISP)
	if isp == "" {
		isp = strings.TrimSpace(loc.Network.Organization)
	}

	for _, r := range rules {
		val := strings.TrimSpace(r.ScopeValue)
		if val == "" {
			continue
		}
		match := false
		switch r.ScopeType {
		case firewalldomain.GeoScopeCountry:
			match = country != "" && strings.EqualFold(val, country)
		case firewalldomain.GeoScopeRegion:
			// 支持 "CN-BJ" 或 纯区域名
			match = matchRegion(val, country, region)
		case firewalldomain.GeoScopeCity:
			match = city != "" && strings.EqualFold(val, city)
		case firewalldomain.GeoScopeASN:
			match = asn != "" && strings.EqualFold(strings.TrimPrefix(val, "AS"), strings.TrimPrefix(asn, "AS"))
		case firewalldomain.GeoScopeISP:
			match = isp != "" && strings.Contains(strings.ToLower(isp), strings.ToLower(val))
		}
		if match {
			go func(id int64) {
				bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = s.pg.IncrementGeoBanMatch(bg, id, time.Now().UTC())
			}(r.ID)
			reason := r.Reason
			if strings.TrimSpace(reason) == "" {
				reason = "geo_banned:" + r.ScopeType + ":" + r.ScopeValue
			}
			return firewalldomain.BanDecision{
				Banned: true,
				Mode:   r.Mode, // 空时由 middleware 回退到默认
				Reason: reason,
				BanID:  r.ID,
			}, r.ID
		}
	}
	return firewalldomain.BanDecision{}, 0
}

// Version 返回规则集版本号（用于调试 / 暴露给监控）。
func (s *GeoBanService) Version() int64 { return s.version.Load() }

// matchRegion 匹配 "CN-BJ" 这类 country-region 格式，或纯 region 名。
func matchRegion(rule, country, region string) bool {
	if rule == "" {
		return false
	}
	if strings.Contains(rule, "-") {
		parts := strings.SplitN(rule, "-", 2)
		return strings.EqualFold(parts[0], country) && strings.EqualFold(parts[1], region)
	}
	return region != "" && strings.EqualFold(rule, region)
}
