package service

import (
	"context"
	"time"

	appdomain "aegis/internal/domain/app"
)

func (s *AppService) GetAppSignInStats(ctx context.Context, appID int64, days int) (*appdomain.AppSignInStats, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 14
	}
	if days > 90 {
		days = 90
	}
	now := time.Now().In(s.location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	startDate := today.AddDate(0, 0, -(days - 1))
	return s.pg.GetAppSignInStats(ctx, appID, today, startDate, today)
}

func (s *AppService) ListAppSignInRecords(ctx context.Context, appID int64, query appdomain.AppSignInRecordQuery) (*appdomain.AppSignInRecordListResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	items, total, err := s.pg.ListAppSignInRecords(ctx, appID, appdomain.AppSignInRecordQuery{
		Keyword:  query.Keyword,
		Source:   query.Source,
		DateFrom: query.DateFrom,
		DateTo:   query.DateTo,
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	return &appdomain.AppSignInRecordListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPagesForService(total, limit),
	}, nil
}
