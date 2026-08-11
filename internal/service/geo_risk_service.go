package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"aegis/internal/config"
	geodomain "aegis/internal/domain/geo"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

// 地理风控内置默认阈值（配置为 0 时生效）。
const (
	defaultGeoMaxSpeedKMH        = 900.0 // 民航巡航速度上限附近
	defaultGeoMinTravelKM        = 100.0 // 过滤 GeoIP 城市级定位抖动
	defaultGeoNewCountryMinLogin = 3     // 新账号前几次登录不触发"新国家"
	defaultGeoProfileCacheTTL    = 72 * time.Hour
	defaultGeoTravelMaxGap       = 24 * time.Hour // 间隔过长的两次登录不做速度判定
	defaultGeoFarFromHomeMinKM   = 500.0          // "远离常驻地"最小绝对距离
)

// 风险分值（与 risk_assessments 体系对齐：>=70 high / >=40 medium / >0 low）。
const (
	scoreImpossibleTravel = 60
	scoreNewCountry       = 25
	scoreFarFromHome      = 15
)

// GeoRiskService 登录地理风控（近线，Worker 消费 NATS 登录事件驱动）。
//
// 数据流：
//  1. 解析登录事件 → LocationService（mmdb + 缓存）取坐标
//  2. 读用户地理画像（Redis 读主，miss 回源 PG）
//  3. 纯内存判定：不可能旅行 / 新国家 / 远离常驻地
//  4. 命中 → 写 risk_assessments（复用风控体系）+ 高危用户通知
//  5. 写 login_geo_events 明细 + 增量更新画像（PG + Redis 双写）
//
// 全链路在 Worker 异步执行，不增加登录请求延迟。
type GeoRiskService struct {
	log      *zap.Logger
	cfg      config.GeoRiskConfig
	pg       *pgrepo.Repository
	profiles *redisrepo.GeoProfileRepository
	location *LocationService

	// disabled：geo 表不存在（迁移未执行，如 PostGIS 镜像未更新）时自动熔断，
	// 避免 NATS 消息因永久性错误被无限 Nak 重投。
	disabled atomic.Bool
}

// NewGeoRiskService 构造服务并规范化配置默认值。
func NewGeoRiskService(log *zap.Logger, cfg config.GeoRiskConfig, pg *pgrepo.Repository, profiles *redisrepo.GeoProfileRepository, location *LocationService) *GeoRiskService {
	if cfg.MaxSpeedKMH <= 0 {
		cfg.MaxSpeedKMH = defaultGeoMaxSpeedKMH
	}
	if cfg.MinTravelKM <= 0 {
		cfg.MinTravelKM = defaultGeoMinTravelKM
	}
	if cfg.NewCountryMinLogins <= 0 {
		cfg.NewCountryMinLogins = defaultGeoNewCountryMinLogin
	}
	if cfg.ProfileCacheTTL <= 0 {
		cfg.ProfileCacheTTL = defaultGeoProfileCacheTTL
	}
	return &GeoRiskService{log: log, cfg: cfg, pg: pg, profiles: profiles, location: location}
}

// geoRiskFinding 单条命中结果。
type geoRiskFinding struct {
	rule   string
	score  int
	detail string
}

// HandleLoginEvent 处理登录审计事件（由 Worker 消费 NATS auth.login.audit.requested 调用，
// 与登录审计写入使用不同的 Queue Group，互不影响）。
func (s *GeoRiskService) HandleLoginEvent(ctx context.Context, payload map[string]any) error {
	if !s.cfg.Enabled || s.disabled.Load() {
		return nil
	}
	userID := int64FromAny(payload["user_id"])
	appID := int64FromAny(payload["appid"])
	ip := strings.TrimSpace(stringFromAny(payload["ip"]))
	if userID == 0 || appID == 0 || ip == "" {
		return nil
	}

	// 1) 解析地理位置（LocationService 自带本地 + Redis 缓存）
	loc := s.location.Resolve(ctx, ip)
	if loc.IsPrivate {
		return nil
	}
	evt := geodomain.LoginEvent{
		UserID:      userID,
		AppID:       appID,
		IP:          ip,
		CountryCode: strings.ToUpper(strings.TrimSpace(loc.CountryCode)),
		Country:     loc.Country,
		Region:      loc.Region,
		City:        loc.City,
		ASN:         loc.Network.ASN,
		ISP:         loc.ISP,
		LoginType:   stringFromAny(payload["login_type"]),
		DeviceID:    stringFromAny(payload["device_id"]),
		CreatedAt:   time.Now().UTC(),
	}
	if loc.Coordinates != nil {
		evt.Lat, evt.Lng = loc.Coordinates.Latitude, loc.Coordinates.Longitude
	}

	// 2) 读画像（Redis 读主，miss 回源 PG）
	profile := s.loadProfile(ctx, appID, userID)

	// 3) 内存判定
	findings := s.assess(profile, evt)

	// 4) 写评估记录 + 高危通知（失败仅告警，不阻断画像更新）
	if len(findings) > 0 {
		s.recordFindings(ctx, evt, findings)
	}

	// 5) 明细 + 画像双写
	if err := s.pg.InsertLoginGeoEvent(ctx, evt); err != nil {
		if s.disableIfSchemaMissing(err) {
			return nil
		}
		return fmt.Errorf("insert login geo event: %w", err)
	}
	if err := s.pg.TouchUserGeoProfileOnLogin(ctx, evt); err != nil {
		if s.disableIfSchemaMissing(err) {
			return nil
		}
		return fmt.Errorf("touch user geo profile: %w", err)
	}
	s.refreshProfileCache(ctx, profile, evt)
	return nil
}

// loadProfile 读取画像；任何失败都返回 nil（首登/缓存不可用时按"无画像"评估）。
func (s *GeoRiskService) loadProfile(ctx context.Context, appID, userID int64) *geodomain.Profile {
	if s.profiles != nil {
		p, err := s.profiles.Get(ctx, appID, userID)
		if err != nil {
			s.log.Warn("geo profile cache read failed", zap.Int64("user_id", userID), zap.Error(err))
		} else if p != nil {
			return p
		}
	}
	p, err := s.pg.GetUserGeoProfile(ctx, userID, appID)
	if err != nil {
		s.log.Warn("geo profile load failed", zap.Int64("user_id", userID), zap.Error(err))
		return nil
	}
	return p
}

// assess 对单次登录做纯内存风控判定。
func (s *GeoRiskService) assess(p *geodomain.Profile, evt geodomain.LoginEvent) []geoRiskFinding {
	if p == nil || p.LoginCount == 0 {
		// 无历史画像（首次登录）不做判定
		return nil
	}
	var findings []geoRiskFinding

	// ① 不可能旅行：上次登录点 → 本次登录点的位移速度超过阈值
	if evt.Lat != nil && evt.Lng != nil &&
		p.LastLat != nil && p.LastLng != nil && p.LastLoginAt != nil {
		gap := evt.CreatedAt.Sub(*p.LastLoginAt)
		if gap > 0 && gap <= defaultGeoTravelMaxGap {
			distKM := haversineKM(*p.LastLat, *p.LastLng, *evt.Lat, *evt.Lng)
			speedKMH := distKM / gap.Hours()
			if distKM >= s.cfg.MinTravelKM && speedKMH > s.cfg.MaxSpeedKMH {
				findings = append(findings, geoRiskFinding{
					rule:  geodomain.RuleImpossibleTravel,
					score: scoreImpossibleTravel,
					detail: fmt.Sprintf("位移 %.0fkm / %.1fh，速度 %.0fkm/h 超过阈值 %.0fkm/h（%s → %s）",
						distKM, gap.Hours(), speedKMH, s.cfg.MaxSpeedKMH, p.LastCountry, evt.CountryCode),
				})
			}
		}
	}

	// ② 新国家：历史登录国家列表之外的首次出现
	if evt.CountryCode != "" && p.LoginCount >= s.cfg.NewCountryMinLogins && !p.KnowsCountry(evt.CountryCode) {
		findings = append(findings, geoRiskFinding{
			rule:   geodomain.RuleNewCountry,
			score:  scoreNewCountry,
			detail: fmt.Sprintf("首次从新国家登录: %s（历史国家: %s）", evt.CountryCode, strings.Join(p.KnownCountries, ",")),
		})
	}

	// ③ 远离常驻地：距 90 天登录质心超过画像半径的 3 倍（且不低于绝对下限）
	if evt.Lat != nil && evt.Lng != nil && p.HomeLat != nil && p.HomeLng != nil && p.HomeRadiusM > 0 {
		distKM := haversineKM(*p.HomeLat, *p.HomeLng, *evt.Lat, *evt.Lng)
		thresholdKM := max(p.HomeRadiusM*3/1000, defaultGeoFarFromHomeMinKM)
		if distKM > thresholdKM {
			findings = append(findings, geoRiskFinding{
				rule:   geodomain.RuleFarFromHome,
				score:  scoreFarFromHome,
				detail: fmt.Sprintf("距常驻地 %.0fkm，超过阈值 %.0fkm", distKM, thresholdKM),
			})
		}
	}
	return findings
}

// recordFindings 写入 risk_assessments 并在高危时通知用户。
func (s *GeoRiskService) recordFindings(ctx context.Context, evt geodomain.LoginEvent, findings []geoRiskFinding) {
	total := 0
	matched := make([]securitydomain.MatchedRule, 0, len(findings))
	details := make([]string, 0, len(findings))
	for _, f := range findings {
		total += f.score
		matched = append(matched, securitydomain.MatchedRule{RuleName: f.rule, Score: f.score})
		details = append(details, f.detail)
	}
	level, action := geoRiskLevel(total)

	assessment := securitydomain.RiskAssessment{
		Scene:        geodomain.SceneGeoLogin,
		AppID:        &evt.AppID,
		UserID:       &evt.UserID,
		IP:           evt.IP,
		DeviceID:     evt.DeviceID,
		TotalScore:   total,
		RiskLevel:    level,
		MatchedRules: matched,
		Action:       action,
		ActionDetail: strings.Join(details, "；"),
	}
	if _, err := s.pg.CreateRiskAssessment(ctx, assessment); err != nil {
		s.log.Warn("geo risk assessment write failed",
			zap.Int64("user_id", evt.UserID), zap.Error(err))
	}
	s.log.Info("geo risk findings",
		zap.Int64("user_id", evt.UserID),
		zap.Int64("app_id", evt.AppID),
		zap.String("ip", evt.IP),
		zap.Int("score", total),
		zap.String("level", level),
		zap.String("detail", assessment.ActionDetail),
	)

	// 高危 → 给用户发送异地登录提醒
	if level == "high" {
		locText := strings.TrimSpace(strings.Join(nonEmpty(evt.Country, evt.Region, evt.City), " "))
		if locText == "" {
			locText = evt.IP
		}
		content := fmt.Sprintf("检测到您的账号于 %s 在 %s 登录（IP: %s）。如非本人操作，请立即修改密码。",
			evt.CreatedAt.Format("2006-01-02 15:04 MST"), locText, evt.IP)
		if err := s.pg.CreateUserNotification(ctx, evt.AppID, evt.UserID,
			"security", "异常登录提醒", content, "warning",
			map[string]any{"scene": geodomain.SceneGeoLogin, "ip": evt.IP, "score": total},
		); err != nil {
			s.log.Warn("geo risk notification failed", zap.Int64("user_id", evt.UserID), zap.Error(err))
		}
	}
}

// refreshProfileCache 用本次登录增量更新内存画像并写回 Redis（与 PG 的
// TouchUserGeoProfileOnLogin 保持一致的演进逻辑）。
func (s *GeoRiskService) refreshProfileCache(ctx context.Context, p *geodomain.Profile, evt geodomain.LoginEvent) {
	if s.profiles == nil {
		return
	}
	if p == nil {
		p = &geodomain.Profile{UserID: evt.UserID, AppID: evt.AppID}
	}
	if evt.CountryCode != "" && !p.KnowsCountry(evt.CountryCode) {
		p.KnownCountries = append(p.KnownCountries, evt.CountryCode)
	}
	if evt.Lat != nil && evt.Lng != nil {
		p.LastLat, p.LastLng = evt.Lat, evt.Lng
	}
	if evt.CountryCode != "" {
		p.LastCountry = evt.CountryCode
	}
	p.LastIP = evt.IP
	loginAt := evt.CreatedAt
	p.LastLoginAt = &loginAt
	p.LoginCount++
	p.UpdatedAt = time.Now().UTC()
	if err := s.profiles.Set(ctx, p, s.cfg.ProfileCacheTTL); err != nil {
		s.log.Warn("geo profile cache write failed", zap.Int64("user_id", evt.UserID), zap.Error(err))
	}
}

// InvalidateProfileCache 失效画像缓存（画像基线重算后由分析服务调用）。
func (s *GeoRiskService) InvalidateProfileCache(ctx context.Context, appID, userID int64) {
	if s.profiles == nil {
		return
	}
	_ = s.profiles.Delete(ctx, appID, userID)
}

// disableIfSchemaMissing 判定错误是否为表/函数缺失（迁移未执行）；
// 是则熔断本服务并返回 true（调用方应 Ack 消息而非 Nak 重投）。
func (s *GeoRiskService) disableIfSchemaMissing(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "42P01" || pgErr.Code == "42883") {
		// 42P01 undefined_table / 42883 undefined_function
		if s.disabled.CompareAndSwap(false, true) {
			s.log.Error("geo 风控数据表缺失，已自动熔断（请执行迁移 000054 并确认 PostGIS 镜像）",
				zap.String("pg_code", pgErr.Code), zap.Error(err))
		}
		return true
	}
	return false
}

// geoRiskLevel 分值 → 等级/动作（与 risk_assessments 体系对齐）。
func geoRiskLevel(score int) (level, action string) {
	switch {
	case score >= 70:
		return "high", "review"
	case score >= 40:
		return "medium", "pass"
	default:
		return "low", "pass"
	}
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
