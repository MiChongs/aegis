package service

import (
	"context"
	"time"

	appdomain "aegis/internal/domain/app"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// ReportService 应用级报表分析服务
type ReportService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

// NewReportService 创建报表服务
func NewReportService(log *zap.Logger, pg *pgrepo.Repository) *ReportService {
	return &ReportService{log: log, pg: pg}
}

// RegistrationReport 注册趋势报表
func (s *ReportService) RegistrationReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RegistrationReport, error) {
	return s.pg.RegistrationReport(ctx, appID, start, end)
}

// LoginReport 登录趋势报表
func (s *ReportService) LoginReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.LoginReport, error) {
	return s.pg.LoginReport(ctx, appID, start, end)
}

// RetentionReport 留存分析报表
func (s *ReportService) RetentionReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RetentionReport, error) {
	return s.pg.RetentionReport(ctx, appID, start, end)
}

// ActiveReport 活跃用户报表
func (s *ReportService) ActiveReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.ActiveReport, error) {
	return s.pg.ActiveReport(ctx, appID, start, end)
}

// DeviceReport 设备分布报表
func (s *ReportService) DeviceReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.DeviceReport, error) {
	return s.pg.DeviceReport(ctx, appID, start, end)
}

// RegionReport 地域分布报表
func (s *ReportService) RegionReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RegionReport, error) {
	return s.pg.RegionReport(ctx, appID, start, end)
}

// ChannelReport 渠道来源报表
func (s *ReportService) ChannelReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.ChannelReport, error) {
	return s.pg.ChannelReport(ctx, appID, start, end)
}

// PaymentReport 支付报表
func (s *ReportService) PaymentReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.PaymentReport, error) {
	return s.pg.PaymentReport(ctx, appID, start, end)
}

// NotificationReport 通知报表
func (s *ReportService) NotificationReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.NotificationReport, error) {
	return s.pg.NotificationReport(ctx, appID, start, end)
}

// RiskReport 风控报表（全局数据，firewall_logs 无 appid）
func (s *ReportService) RiskReport(ctx context.Context, start, end time.Time) (*appdomain.RiskReport, error) {
	return s.pg.RiskReport(ctx, start, end)
}

// ActivityReport 抽奖活动报表
func (s *ReportService) ActivityReport(ctx context.Context, appID int64, start, end time.Time, activityID *int64) (*appdomain.ActivityReport, error) {
	return s.pg.ActivityReport(ctx, appID, start, end, activityID)
}

// FunnelReport 用户转化漏斗报表
func (s *ReportService) FunnelReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.FunnelReport, error) {
	return s.pg.FunnelReport(ctx, appID, start, end)
}
