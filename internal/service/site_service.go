package service

import (
	"context"
	"net/http"
	"strings"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

type SiteService struct {
	pg *pgrepo.Repository
}

func NewSiteService(pg *pgrepo.Repository) *SiteService {
	return &SiteService{pg: pg}
}

func (s *SiteService) Create(ctx context.Context, session *authdomain.Session, mutation appdomain.SiteMutation) (*appdomain.Site, error) {
	if err := ensureAuthedApp(session, mutation.AppID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ptrString(mutation.Name)) == "" || strings.TrimSpace(ptrString(mutation.URL)) == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "网站名称和链接不能为空")
	}
	item, err := s.pg.CreateSite(ctx, appdomain.Site{
		AppID:       mutation.AppID,
		UserID:      session.UserID,
		Header:      ptrString(mutation.Header),
		Name:        ptrString(mutation.Name),
		URL:         ptrString(mutation.URL),
		Type:        defaultString(ptrString(mutation.Type), "other"),
		Description: ptrString(mutation.Description),
		Category:    ptrString(mutation.Category),
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *SiteService) Update(ctx context.Context, session *authdomain.Session, mutation appdomain.SiteMutation) (*appdomain.Site, error) {
	if err := ensureAuthedApp(session, mutation.AppID); err != nil {
		return nil, err
	}
	item, err := s.pg.UpdateSiteByUser(ctx, mutation.ID, session.UserID, mutation.AppID, mutation)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) Delete(ctx context.Context, session *authdomain.Session, siteID int64, appID int64) error {
	if err := ensureAuthedApp(session, appID); err != nil {
		return err
	}
	affected, err := s.pg.DeleteSiteByUser(ctx, siteID, session.UserID, appID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return nil
}

func (s *SiteService) MySites(ctx context.Context, session *authdomain.Session, query appdomain.SiteListQuery) (*appdomain.SiteListResult, error) {
	if err := ensureAuthedApp(session, session.AppID); err != nil {
		return nil, err
	}
	return s.pg.ListSitesByUser(ctx, session.UserID, session.AppID, query)
}

func (s *SiteService) Detail(ctx context.Context, session *authdomain.Session, siteID int64, appID int64) (*appdomain.Site, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetSiteByID(ctx, siteID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	if item.AuditStatus != "approved" && item.UserID != session.UserID {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) PublicList(ctx context.Context, session *authdomain.Session, appID int64, query appdomain.SiteListQuery) (*appdomain.SiteListResult, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	return s.pg.ListSitesPublic(ctx, appID, query)
}

func (s *SiteService) Search(ctx context.Context, session *authdomain.Session, appID int64, keyword string, page int, limit int) (*appdomain.SiteListResult, error) {
	return s.PublicList(ctx, session, appID, appdomain.SiteListQuery{Keyword: keyword, Page: page, Limit: limit})
}

func (s *SiteService) Resubmit(ctx context.Context, session *authdomain.Session, siteID int64, appID int64) (*appdomain.Site, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.UpdateSiteByUser(ctx, siteID, session.UserID, appID, appdomain.SiteMutation{})
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) AdminList(ctx context.Context, appID int64, query appdomain.SiteListQuery) (*appdomain.SiteListResult, error) {
	return s.pg.ListSitesByApp(ctx, appID, query)
}

func (s *SiteService) AdminDetail(ctx context.Context, appID int64, siteID int64) (*appdomain.Site, error) {
	item, err := s.pg.GetSiteByID(ctx, siteID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) AdminAudit(ctx context.Context, siteID int64, appID int64, adminID int64, status string, reason string) (*appdomain.Site, error) {
	if status != "approved" && status != "rejected" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "审核状态无效")
	}
	item, err := s.pg.AuditSite(ctx, siteID, appID, adminID, status, reason)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) AdminUpdate(ctx context.Context, appID int64, mutation appdomain.SiteMutation) (*appdomain.Site, error) {
	item, err := s.pg.UpdateSiteAdmin(ctx, mutation.ID, appID, mutation)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) AdminDelete(ctx context.Context, appID int64, siteID int64) error {
	affected, err := s.pg.DeleteSiteAdmin(ctx, siteID, appID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return nil
}

func (s *SiteService) AdminTogglePinned(ctx context.Context, appID int64, siteID int64, pinned bool) (*appdomain.Site, error) {
	item, err := s.pg.ToggleSitePinned(ctx, siteID, appID, pinned)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40420, http.StatusNotFound, "站点不存在")
	}
	return item, nil
}

func (s *SiteService) AdminAuditStats(ctx context.Context, appID int64) (*appdomain.SiteAuditStats, error) {
	return s.pg.GetSiteAuditStats(ctx, appID)
}

func (s *SiteService) AdminUserSites(ctx context.Context, appID int64, userID int64) (*appdomain.SiteListResult, error) {
	return s.pg.ListSitesByUser(ctx, userID, appID, appdomain.SiteListQuery{Page: 1, Limit: 100})
}

func ensureAuthedApp(session *authdomain.Session, appID int64) error {
	if session == nil {
		return apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	if appID > 0 && session.AppID != appID {
		return apperrors.New(40313, http.StatusForbidden, "应用不匹配")
	}
	return nil
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
