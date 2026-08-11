package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	geodomain "aegis/internal/domain/geo"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// GeoFenceService 地理围栏服务。
//
// 设计（与 GeoBanService 同构）：
//   - 规则保存在 geo_fences 表（PostGIS 存储真实来源 + 管理端校验/回测）
//   - 启动时一次性加载并编译为内存几何，修改时 ReloadFromDB 热更
//   - 请求热路径判定全部走内存（haversine / 射线法，纳秒~微秒级），零数据库依赖
//   - 命中时异步累计 match_count
//
// 判定语义：
//   - deny  ：点落在围栏内 → 拦截
//   - allow ：存在任一启用的 allow 围栏时，点在所有 allow 围栏之外 → 拦截
//   - review：点落在围栏内 → 仅记录命中，不拦截
//   - 坐标无法解析（私网 IP / mmdb 未命中）→ 放行（fail-open）
type GeoFenceService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	location *LocationService

	mu     sync.RWMutex
	fences []compiledGeoFence

	version atomic.Int64
}

// compiledGeoFence 编译到内存的围栏（热路径只读结构）。
type compiledGeoFence struct {
	id      int64
	appID   *int64
	mode    string
	banMode string
	reason  string

	polygons geoMultiPolygon // 多边形围栏；nil 表示圆形围栏
	lat      float64         // 圆形围栏圆心
	lng      float64
	radiusKM float64
}

func (f *compiledGeoFence) contains(lat, lng float64) bool {
	if f.polygons != nil {
		return f.polygons.contains(lat, lng)
	}
	return haversineKM(f.lat, f.lng, lat, lng) <= f.radiusKM
}

// NewGeoFenceService 构造服务；locationService 用于 IP → 坐标解析（带本地缓存）。
func NewGeoFenceService(log *zap.Logger, pg *pgrepo.Repository, location *LocationService) *GeoFenceService {
	return &GeoFenceService{log: log, pg: pg, location: location}
}

// Initialize 启动时从 DB 加载全部围栏。
func (s *GeoFenceService) Initialize(ctx context.Context) error {
	return s.ReloadFromDB(ctx)
}

// ReloadFromDB 重新加载并编译所有启用中、未过期的围栏。
func (s *GeoFenceService) ReloadFromDB(ctx context.Context) error {
	all, err := s.pg.ListGeoFences(ctx, true)
	if err != nil {
		return err
	}
	now := time.Now()
	compiled := make([]compiledGeoFence, 0, len(all))
	for _, f := range all {
		if f.ExpiresAt != nil && !f.ExpiresAt.After(now) {
			continue
		}
		c := compiledGeoFence{
			id:      f.ID,
			appID:   f.AppID,
			mode:    f.Mode,
			banMode: f.BanMode,
			reason:  f.Reason,
		}
		if len(f.Fence) > 0 {
			mp, err := parseGeoJSONMultiPolygon(f.Fence)
			if err != nil {
				s.log.Warn("围栏几何编译失败，已跳过", zap.Int64("fence_id", f.ID), zap.Error(err))
				continue
			}
			c.polygons = mp
		} else if f.CenterLat != nil && f.CenterLng != nil && f.RadiusM != nil && *f.RadiusM > 0 {
			c.lat, c.lng, c.radiusKM = *f.CenterLat, *f.CenterLng, *f.RadiusM/1000
		} else {
			s.log.Warn("围栏缺少有效几何，已跳过", zap.Int64("fence_id", f.ID))
			continue
		}
		compiled = append(compiled, c)
	}
	s.mu.Lock()
	s.fences = compiled
	s.mu.Unlock()
	s.version.Add(1)
	s.log.Info("geo fences reloaded", zap.Int("count", len(compiled)))
	return nil
}

// Version 返回围栏集版本号（监控用）。
func (s *GeoFenceService) Version() int64 { return s.version.Load() }

// ──────────────────────────────────────
// 热路径判定
// ──────────────────────────────────────

// CheckIP 评估请求 IP 是否命中地理围栏。
// 调用侧应在 IP 精确封禁、地域封禁之后调用（粒度从细到粗）。
func (s *GeoFenceService) CheckIP(ctx context.Context, ip string) (firewalldomain.BanDecision, int64) {
	s.mu.RLock()
	fences := s.fences
	s.mu.RUnlock()
	if len(fences) == 0 || s.location == nil {
		return firewalldomain.BanDecision{}, 0
	}

	loc := s.location.Resolve(ctx, ip)
	if loc.IsPrivate || loc.Coordinates == nil ||
		loc.Coordinates.Latitude == nil || loc.Coordinates.Longitude == nil {
		// 无法定位 → 放行（fail-open）
		return firewalldomain.BanDecision{}, 0
	}
	lat, lng := *loc.Coordinates.Latitude, *loc.Coordinates.Longitude

	allowTotal, allowHit := 0, false
	for i := range fences {
		f := &fences[i]
		switch f.mode {
		case geodomain.FenceModeDeny:
			if f.contains(lat, lng) {
				s.recordMatchAsync(f.id)
				return firewalldomain.BanDecision{
					Banned: true,
					Mode:   f.banMode, // 空时由 middleware 回退到平台默认
					Reason: fenceReason(f, "geo_fence"),
					BanID:  f.id,
				}, f.id
			}
		case geodomain.FenceModeAllow:
			allowTotal++
			if !allowHit && f.contains(lat, lng) {
				allowHit = true
			}
		case geodomain.FenceModeReview:
			if f.contains(lat, lng) {
				s.recordMatchAsync(f.id)
				s.log.Debug("geo fence review hit",
					zap.Int64("fence_id", f.id), zap.String("ip", ip))
			}
		}
	}

	// 存在 allow 围栏且点不在任何 allow 围栏内 → 拦截
	if allowTotal > 0 && !allowHit {
		return firewalldomain.BanDecision{
			Banned: true,
			Mode:   "", // 平台默认响应模式
			Reason: "geo_fence_outside_allowed",
		}, 0
	}
	return firewalldomain.BanDecision{}, 0
}

func fenceReason(f *compiledGeoFence, fallback string) string {
	if strings.TrimSpace(f.reason) != "" {
		return f.reason
	}
	return fallback
}

func (s *GeoFenceService) recordMatchAsync(id int64) {
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.pg.IncrementGeoFenceMatch(bg, id, time.Now().UTC())
	}()
}

// ──────────────────────────────────────
// 管理端 CRUD（写入 PG 后热重载）
// ──────────────────────────────────────

// ListAll 返回全部围栏（含禁用/过期）供管理 UI 用。
func (s *GeoFenceService) ListAll(ctx context.Context) ([]geodomain.Fence, error) {
	return s.pg.ListGeoFences(ctx, false)
}

// GetByID 按 ID 读取单条围栏。
func (s *GeoFenceService) GetByID(ctx context.Context, id int64) (*geodomain.Fence, error) {
	return s.pg.GetGeoFence(ctx, id)
}

// validateMutation 规范化并校验围栏载荷（多边形交给 PostGIS 校验）。
func (s *GeoFenceService) validateMutation(ctx context.Context, m *geodomain.FenceMutation) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Mode = strings.ToLower(strings.TrimSpace(m.Mode))
	m.FenceGeoJSON = strings.TrimSpace(m.FenceGeoJSON)
	if m.Name == "" {
		return fmt.Errorf("围栏名称必填")
	}
	switch m.Mode {
	case geodomain.FenceModeDeny, geodomain.FenceModeAllow, geodomain.FenceModeReview:
	default:
		return fmt.Errorf("无效的围栏模式（允许: deny/allow/review）")
	}
	if m.BanMode != "" {
		m.BanMode = firewalldomain.NormalizeBanMode(m.BanMode, "")
	}
	hasPolygon := m.FenceGeoJSON != ""
	hasCircle := m.CenterLat != nil && m.CenterLng != nil && m.RadiusM != nil && *m.RadiusM > 0
	if hasPolygon == hasCircle {
		return fmt.Errorf("多边形围栏（fence GeoJSON）与圆形围栏（center+radius）必须二选一")
	}
	if hasPolygon {
		// 先用 Go 解析器验证可编译性（热路径同款逻辑），再交 PostGIS 校验拓扑
		if _, err := parseGeoJSONMultiPolygon([]byte(m.FenceGeoJSON)); err != nil {
			return err
		}
		if err := s.pg.ValidateFenceGeoJSON(ctx, m.FenceGeoJSON); err != nil {
			return err
		}
	}
	if hasCircle {
		if *m.CenterLat < -90 || *m.CenterLat > 90 || *m.CenterLng < -180 || *m.CenterLng > 180 {
			return fmt.Errorf("圆心坐标超出范围")
		}
	}
	return nil
}

// Create 创建围栏并热重载。
func (s *GeoFenceService) Create(ctx context.Context, m geodomain.FenceMutation, adminID *int64) (*geodomain.Fence, error) {
	if err := s.validateMutation(ctx, &m); err != nil {
		return nil, err
	}
	f, err := s.pg.CreateGeoFence(ctx, m, adminID)
	if err != nil {
		return nil, err
	}
	if err := s.ReloadFromDB(ctx); err != nil {
		s.log.Warn("geo fence reload failed after create", zap.Error(err))
	}
	return &f, nil
}

// Update 更新围栏并热重载。
func (s *GeoFenceService) Update(ctx context.Context, id int64, m geodomain.FenceMutation) (*geodomain.Fence, error) {
	if err := s.validateMutation(ctx, &m); err != nil {
		return nil, err
	}
	f, err := s.pg.UpdateGeoFence(ctx, id, m)
	if err != nil {
		return nil, err
	}
	if err := s.ReloadFromDB(ctx); err != nil {
		s.log.Warn("geo fence reload failed after update", zap.Error(err))
	}
	return &f, nil
}

// Toggle 启用/禁用围栏。
func (s *GeoFenceService) Toggle(ctx context.Context, id int64, enabled bool) error {
	if err := s.pg.UpdateGeoFenceStatus(ctx, id, enabled); err != nil {
		return err
	}
	return s.ReloadFromDB(ctx)
}

// Delete 删除围栏。
func (s *GeoFenceService) Delete(ctx context.Context, id int64) error {
	if err := s.pg.DeleteGeoFence(ctx, id); err != nil {
		return err
	}
	return s.ReloadFromDB(ctx)
}

// Preview 回测围栏影响面（PostGIS 离线计算，管理端按需调用）。
func (s *GeoFenceService) Preview(ctx context.Context, m geodomain.FenceMutation, windowDays int) (*geodomain.FencePreview, error) {
	if err := s.validateMutation(ctx, &m); err != nil {
		return nil, err
	}
	return s.pg.PreviewGeoFence(ctx, m, windowDays)
}
