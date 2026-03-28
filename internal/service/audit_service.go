package service

import (
	"context"

	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// AuditService 管理员操作审计服务
type AuditService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

func NewAuditService(log *zap.Logger, pg *pgrepo.Repository) *AuditService {
	return &AuditService{log: log, pg: pg}
}

// Record 异步记录审计日志（fire-and-forget，不阻塞业务）
func (s *AuditService) Record(entry systemdomain.AuditEntry) {
	if entry.Status == "" {
		entry.Status = "success"
	}
	go func() {
		if err := s.pg.InsertAuditLog(context.Background(), entry); err != nil {
			s.log.Warn("审计日志写入失败", zap.Error(err), zap.String("action", entry.Action))
		}
	}()
}

func (s *AuditService) ListLogs(ctx context.Context, filter systemdomain.AuditFilter) (*systemdomain.AuditPage, error) {
	return s.pg.ListAuditLogs(ctx, filter)
}

func (s *AuditService) GetLog(ctx context.Context, id int64) (*systemdomain.AuditLog, error) {
	return s.pg.GetAuditLog(ctx, id)
}

func (s *AuditService) GetStats(ctx context.Context) (*systemdomain.AuditStats, error) {
	return s.pg.GetAuditStats(ctx)
}

func (s *AuditService) ExportLogs(ctx context.Context, filter systemdomain.AuditFilter) ([]systemdomain.AuditLog, error) {
	return s.pg.ListAuditLogsForExport(ctx, filter)
}
