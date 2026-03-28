package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// FirewallLogService 防火墙安全日志业务逻辑
type FirewallLogService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	location   *LocationService
	banService *IPBanService
}

// NewFirewallLogService 创建防火墙日志服务
func NewFirewallLogService(log *zap.Logger, pg *pgrepo.Repository, location *LocationService, banService *IPBanService) *FirewallLogService {
	return &FirewallLogService{log: log, pg: pg, location: location, banService: banService}
}

// ──────────────────────────────────────
// NATS Worker 事件处理
// ──────────────────────────────────────

// HandleFirewallBlocked 处理防火墙拦截事件（由 Worker 消费 NATS 调用）
func (s *FirewallLogService) HandleFirewallBlocked(ctx context.Context, payload map[string]any) error {
	// 反序列化 BlockEvent
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	var evt firewalldomain.BlockEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal block event: %w", err)
	}

	// 构建日志记录并写入数据库
	record := firewalldomain.FirewallLog{
		RequestID:    evt.RequestID,
		IP:           evt.IP,
		Method:       evt.Method,
		Path:         evt.Path,
		QueryString:  evt.QueryString,
		UserAgent:    evt.UserAgent,
		Headers:      evt.Headers,
		Reason:       evt.Reason,
		HTTPStatus:   evt.HTTPStatus,
		ResponseCode: evt.ResponseCode,
		WAFRuleID:    evt.WAFRuleID,
		WAFAction:    evt.WAFAction,
		WAFData:      evt.WAFData,
		Severity:     evt.Severity,
		BlockedAt:    evt.BlockedAt,
	}

	id, err := s.pg.InsertFirewallLog(ctx, record)
	if err != nil {
		return fmt.Errorf("insert firewall log: %w", err)
	}

	// 异步解析 GeoIP 并回填（在 Worker 端同步执行，不影响请求延迟）
	if s.location != nil && evt.IP != "" {
		loc := s.location.Resolve(ctx, evt.IP)
		var lat, lng *float64
		if loc.Coordinates != nil {
			lat = loc.Coordinates.Latitude
			lng = loc.Coordinates.Longitude
		}
		if updateErr := s.pg.UpdateFirewallLogGeoIP(ctx, id,
			loc.Country, loc.CountryCode, loc.Region, loc.City,
			loc.ISP, loc.Network.ASN, loc.Timezone, lat, lng,
		); updateErr != nil {
			s.log.Warn("防火墙日志 GeoIP 回填失败",
				zap.Int64("log_id", id),
				zap.String("ip", evt.IP),
				zap.Error(updateErr),
			)
		}
	}

	s.log.Debug("防火墙拦截日志已记录",
		zap.Int64("log_id", id),
		zap.String("ip", evt.IP),
		zap.String("reason", evt.Reason),
		zap.String("severity", evt.Severity),
	)

	// 自动封禁评估（跳过已封禁 IP 的事件避免重复评估）
	if s.banService != nil && evt.IP != "" && evt.Reason != "banned_ip" {
		if banErr := s.banService.EvaluateAutoBan(ctx, evt.IP); banErr != nil {
			s.log.Warn("自动封禁评估失败", zap.String("ip", evt.IP), zap.Error(banErr))
		}
	}

	return nil
}

// ──────────────────────────────────────
// 查询 API
// ──────────────────────────────────────

// ListLogs 分页查询防火墙日志
func (s *FirewallLogService) ListLogs(ctx context.Context, filter firewalldomain.FirewallLogFilter) (*firewalldomain.FirewallLogPage, error) {
	return s.pg.ListFirewallLogs(ctx, filter)
}

// GetLog 获取单条防火墙日志详情
func (s *FirewallLogService) GetLog(ctx context.Context, id int64) (*firewalldomain.FirewallLog, error) {
	return s.pg.GetFirewallLog(ctx, id)
}

// GetStats 获取聚合统计
func (s *FirewallLogService) GetStats(ctx context.Context, start, end time.Time, granularity string) (*firewalldomain.FirewallStats, error) {
	return s.pg.FirewallLogStats(ctx, start, end, granularity, 10)
}

// CleanupLogs 清理指定时间之前的日志
func (s *FirewallLogService) CleanupLogs(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := s.pg.DeleteFirewallLogsBefore(ctx, before)
	if err != nil {
		return 0, err
	}
	s.log.Info("防火墙日志清理完成",
		zap.Time("before", before),
		zap.Int64("deleted", deleted),
	)
	return deleted, nil
}
