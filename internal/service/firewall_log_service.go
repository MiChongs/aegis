package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	fw "aegis/internal/firewall"
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
		EventDetail:  evt.EventDetail,
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

	// 只有真正拦下的事件参与封禁：观察模式「本该拦截」、自定义规则的记录动作、出站命中都不算。
	isBlock := evt.Action == "" || evt.Action == firewalldomain.EventActionBlock
	if s.banService != nil && evt.IP != "" && isBlock {
		// 自定义规则要求的封禁：命中即封，不等阈值。
		if evt.Reason == "custom_rule" && evt.BanSeconds != 0 {
			duration := evt.BanSeconds
			if duration < 0 {
				duration = 0 // 永久
			}
			_, banErr := s.banService.BanIPWithOptions(ctx, evt.IP, fmt.Sprintf("自定义规则「%s」命中即封", evt.RuleName), duration, 0, BanIPOptions{
				Source: "rule", TriggerRule: fmt.Sprintf("firewall-rule:%d", evt.RuleRef),
			})
			if banErr != nil && !strings.Contains(banErr.Error(), "已被封禁") {
				s.log.Warn("自定义规则封禁失败", zap.String("ip", evt.IP), zap.Int64("rule", evt.RuleRef), zap.Error(banErr))
			}
		} else if !strings.HasPrefix(evt.Reason, "banned_") && evt.Reason != "banned_ip" {
			// 自动封禁评估（跳过已封禁 IP 的事件避免重复评估）
			if banErr := s.banService.EvaluateAutoBan(ctx, evt.IP); banErr != nil {
				s.log.Warn("自动封禁评估失败", zap.String("ip", evt.IP), zap.Error(banErr))
			}
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

// GetLog 获取单条防火墙日志详情。v1 的 WAF 事件只记了打断请求的那条规则 ID，这里按规则目录补上说明。
func (s *FirewallLogService) GetLog(ctx context.Context, id int64) (*firewalldomain.FirewallLog, error) {
	item, err := s.pg.GetFirewallLog(ctx, id)
	if err != nil || item == nil {
		return item, err
	}
	if len(item.Matches) == 0 && item.WAFRuleID != nil {
		if msg, group, ok := fw.RuleMessage(*item.WAFRuleID); ok {
			item.Matches = []firewalldomain.MatchedRule{{ID: *item.WAFRuleID, Message: msg, Group: group, Data: item.WAFData}}
		}
	}
	return item, nil
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
