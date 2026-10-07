package service

import (
	"context"
	"slices"
	"strings"

	"aegis/internal/auditcatalog"
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
	if log == nil {
		log = zap.NewNop()
	}
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
	page, err := s.pg.ListAuditLogs(ctx, filter)
	if err != nil || page == nil {
		return page, err
	}
	if page.Items == nil {
		page.Items = []systemdomain.AuditLog{}
	}
	for i := range page.Items {
		DescribeAuditLog(&page.Items[i])
	}
	return page, nil
}

func (s *AuditService) GetLog(ctx context.Context, id int64) (*systemdomain.AuditLog, error) {
	item, err := s.pg.GetAuditLog(ctx, id)
	if item != nil {
		DescribeAuditLog(item)
	}
	return item, err
}

func (s *AuditService) GetStats(ctx context.Context) (*systemdomain.AuditStats, error) {
	stats, err := s.pg.GetAuditStats(ctx)
	if stats != nil {
		for i := range stats.TopCategories {
			stats.TopCategories[i].Label = auditcatalog.ModuleLabel(auditcatalog.ModuleFor(stats.TopCategories[i].Key))
		}
	}
	return stats, err
}

// Overview 控制台顶部总览。
func (s *AuditService) Overview(ctx context.Context) (*systemdomain.AuditOverview, error) {
	overview, err := s.pg.GetAuditOverview(ctx)
	if overview != nil {
		for i := range overview.TopModules {
			overview.TopModules[i].Label = auditcatalog.ModuleLabel(auditcatalog.ModuleFor(overview.TopModules[i].Key))
		}
	}
	return overview, err
}

// Facets 筛选项。模块按目录顺序列全，没有日志的计数为 0。
func (s *AuditService) Facets(ctx context.Context) (*systemdomain.AuditFacets, error) {
	facets, err := s.pg.GetAuditFacets(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, item := range facets.Modules {
		counts[auditcatalog.ModuleFor(item.Key)] += item.Count
	}
	facets.Modules = facets.Modules[:0]
	for _, module := range auditcatalog.Modules() {
		facets.Modules = append(facets.Modules, systemdomain.AuditStatItem{Key: module.Key, Label: module.Label, Count: counts[module.Key]})
	}
	return facets, nil
}

func (s *AuditService) ExportLogs(ctx context.Context, filter systemdomain.AuditFilter) ([]systemdomain.AuditLog, error) {
	items, err := s.pg.ListAuditLogsForExport(ctx, filter)
	for i := range items {
		DescribeAuditLog(&items[i])
	}
	return items, err
}

// BackfillCatalog 把目录版本落后的历史日志按 (method, route) 改到当前目录的模块与类型。
// 服务启动时在后台跑一次；组合数不超过路由数，几百条 UPDATE。
func (s *AuditService) BackfillCatalog(ctx context.Context) {
	pairs, err := s.pg.ListStaleAuditRoutes(ctx, auditcatalog.Revision)
	if err != nil {
		s.log.Warn("审计目录回填：读取失败", zap.Error(err))
		return
	}
	var updated int64
	for _, pair := range pairs {
		op, ok := auditcatalog.Lookup(pair[0], pair[1])
		if !ok {
			continue
		}
		n, err := s.pg.BackfillAuditRoute(ctx, pgrepo.AuditRoutePair{Method: pair[0], Route: pair[1], Module: op.Module, Kind: op.Kind}, auditcatalog.Revision)
		if err != nil {
			s.log.Warn("审计目录回填：更新失败", zap.String("route", pair[1]), zap.Error(err))
			return
		}
		updated += n
	}
	if err := s.pg.MarkAuditCatalogRevision(ctx, auditcatalog.Revision); err != nil {
		s.log.Warn("审计目录回填：标记版本失败", zap.Error(err))
		return
	}
	if updated > 0 {
		s.log.Info("审计目录回填完成", zap.Int64("rows", updated), zap.Int("revision", auditcatalog.Revision))
	}
}

// DescribeAuditLog 由操作目录与请求信息补全展示字段（操作名、模块、对象、浏览器、系统、地点）。
func DescribeAuditLog(log *systemdomain.AuditLog) {
	display := auditcatalog.Describe(log.Method, log.Route, log.ResourceID, log.TargetName)
	if display.Known {
		log.OperationName = display.OperationName
		log.Category = display.Module
		if log.Kind == "" {
			log.Kind = display.Kind
		}
	} else {
		// 目录里没有的旧日志（路由为空、接口已下线）：沿用写入时的摘要
		log.OperationName = strings.TrimSpace(log.Summary)
		log.Category = auditcatalog.ModuleFor(log.Category)
		if log.OperationName == "" {
			log.OperationName = log.Method + " " + log.Path
		}
	}
	log.ModuleLabel = auditcatalog.ModuleLabel(log.Category)
	log.TargetType = display.TargetType
	log.TargetLabel = display.TargetLabel
	log.Browser, log.OS = auditcatalog.ParseUserAgent(log.UserAgent)
	parts := make([]string, 0, 3)
	for _, part := range []string{log.Country, log.Region, log.City} {
		if part = strings.TrimSpace(part); part != "" && !slices.Contains(parts, part) {
			parts = append(parts, part)
		}
	}
	log.Location = strings.Join(parts, " ")
}
