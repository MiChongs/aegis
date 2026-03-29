package service

import (
	"context"
	"time"

	admindomain "aegis/internal/domain/admin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// SessionMgmtService 管理员会话管理、临时权限、代理授权
type SessionMgmtService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	realtime *RealtimeService
}

// NewSessionMgmtService 创建会话管理服务
func NewSessionMgmtService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, realtime *RealtimeService) *SessionMgmtService {
	if log == nil {
		log = zap.NewNop()
	}
	return &SessionMgmtService{log: log, pg: pg, sessions: sessions, realtime: realtime}
}

// ── 会话管理 ──

// ListAllSessions 分页列出所有活跃会话
func (s *SessionMgmtService) ListAllSessions(ctx context.Context, page, limit int) ([]admindomain.AdminSessionRecord, int64, error) {
	return s.pg.ListAllActiveSessions(ctx, page, limit)
}

// ListAdminSessions 列出指定管理员的会话
func (s *SessionMgmtService) ListAdminSessions(ctx context.Context, adminID int64) ([]admindomain.AdminSessionRecord, error) {
	return s.pg.ListAdminSessions(ctx, adminID)
}

// ForceLogout 强制踢出单个会话
func (s *SessionMgmtService) ForceLogout(ctx context.Context, sessionID string, operatorID int64) error {
	record, err := s.pg.GetAdminSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := s.pg.RevokeAdminSession(ctx, sessionID, operatorID); err != nil {
		return err
	}
	if s.sessions != nil {
		ttl := 24 * time.Hour
		if record != nil {
			if remaining := timeutil.Until(record.ExpiresAt); remaining > 0 {
				ttl = remaining
			}
			_ = s.sessions.RemoveAdminOnline(ctx, record.AdminID, sessionID)
		}
		_ = s.sessions.BlacklistToken(ctx, sessionID, ttl)
	}
	s.log.Info("管理员会话被强制撤销", zap.String("sessionId", sessionID), zap.Int64("operatorId", operatorID))
	return nil
}

// ForceLogoutAll 强制踢出指定管理员的所有会话
func (s *SessionMgmtService) ForceLogoutAll(ctx context.Context, adminID, operatorID int64) (int64, error) {
	// 1. 获取该管理员所有活跃会话以便逐个加入黑名单
	sessions, err := s.pg.ListAdminSessions(ctx, adminID)
	if err != nil {
		return 0, err
	}
	// 2. DB 批量撤销
	count, err := s.pg.RevokeAllAdminSessions(ctx, adminID, operatorID)
	if err != nil {
		return 0, err
	}
	// 3. 逐个加入 Redis 黑名单
	for _, sess := range sessions {
		_ = s.sessions.BlacklistToken(ctx, sess.ID, 24*time.Hour)
	}
	// 4. 清除在线状态
	_ = s.sessions.ClearAdminOnline(ctx, adminID)
	s.log.Info("管理员所有会话被强制撤销", zap.Int64("adminId", adminID), zap.Int64("count", count), zap.Int64("operatorId", operatorID))
	return count, nil
}

// ListOnlineAdmins 列出当前在线管理员摘要
func (s *SessionMgmtService) ListOnlineAdmins(ctx context.Context) ([]admindomain.OnlineAdmin, error) {
	ids, err := s.sessions.ListOnlineAdminIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []admindomain.OnlineAdmin{}, nil
	}
	items := make([]admindomain.OnlineAdmin, 0, len(ids))
	for _, id := range ids {
		activeSessions, err := s.pg.ListAdminSessions(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(activeSessions) == 0 {
			_ = s.sessions.ClearAdminOnline(ctx, id)
			continue
		}
		profile, err := s.pg.GetAdminAccessByID(ctx, id)
		if err != nil || profile == nil {
			continue
		}
		items = append(items, admindomain.OnlineAdmin{
			AdminID:      id,
			Account:      profile.Account.Account,
			DisplayName:  profile.Account.DisplayName,
			SessionCount: int64(len(activeSessions)),
			LastActiveAt: latestAdminSessionActivity(activeSessions),
		})
	}
	return items, nil
}

// ── 临时权限 ──

// GrantTempPermission 授予临时权限并通知目标管理员
func (s *SessionMgmtService) GrantTempPermission(ctx context.Context, adminID int64, permission string, appID *int64, grantedBy int64, reason string, expiresAt time.Time) (*admindomain.TempPermission, error) {
	tp, err := s.pg.GrantTempPermission(ctx, adminID, permission, appID, grantedBy, reason, expiresAt)
	if err != nil {
		return nil, err
	}
	s.log.Info("临时权限已授予", zap.Int64("adminId", adminID), zap.String("permission", permission), zap.Int64("grantedBy", grantedBy))
	return tp, nil
}

// ListTempPermissions 列出指定管理员或全部活跃临时权限
func (s *SessionMgmtService) ListTempPermissions(ctx context.Context, adminID *int64) ([]admindomain.TempPermission, error) {
	if adminID != nil && *adminID > 0 {
		return s.pg.ListTempPermissions(ctx, *adminID)
	}
	return s.pg.ListAllTempPermissions(ctx)
}

// RevokeTempPermission 撤销临时权限
func (s *SessionMgmtService) RevokeTempPermission(ctx context.Context, permID int64) error {
	if err := s.pg.RevokeTempPermission(ctx, permID); err != nil {
		return err
	}
	s.log.Info("临时权限已撤销", zap.Int64("permId", permID))
	return nil
}

// ── 代理授权 ──

// CreateDelegation 创建代理授权
func (s *SessionMgmtService) CreateDelegation(ctx context.Context, delegation admindomain.AdminDelegation) (*admindomain.AdminDelegation, error) {
	result, err := s.pg.CreateDelegation(ctx, delegation)
	if err != nil {
		return nil, err
	}
	s.log.Info("代理授权已创建", zap.Int64("delegatorId", delegation.DelegatorID), zap.Int64("delegateId", delegation.DelegateID))
	return result, nil
}

// ListDelegations 列出代理授权
func (s *SessionMgmtService) ListDelegations(ctx context.Context, adminID int64, role string) ([]admindomain.AdminDelegation, error) {
	return s.pg.ListDelegations(ctx, adminID, role)
}

// RevokeDelegation 撤销代理授权
func (s *SessionMgmtService) RevokeDelegation(ctx context.Context, delegationID int64) error {
	if err := s.pg.RevokeDelegation(ctx, delegationID); err != nil {
		return err
	}
	s.log.Info("代理授权已撤销", zap.Int64("delegationId", delegationID))
	return nil
}

// ── 清理 ──

// Cleanup 清理过期的会话、临时权限和代理授权
func (s *SessionMgmtService) Cleanup(ctx context.Context) {
	cutoff := timeutil.NowUTC()
	if n, err := s.pg.CleanupExpiredSessions(ctx, cutoff); err == nil && n > 0 {
		s.log.Info("清理过期会话记录", zap.Int64("count", n))
	} else if err != nil {
		s.log.Warn("cleanup expired admin sessions failed", zap.Error(err))
	}
	if n, err := s.pg.CleanupExpiredTempPermissions(ctx, cutoff); err == nil && n > 0 {
		s.log.Info("清理过期临时权限", zap.Int64("count", n))
	} else if err != nil {
		s.log.Warn("cleanup expired temp permissions failed", zap.Error(err))
	}
	if n, err := s.pg.CleanupExpiredDelegations(ctx, cutoff); err == nil && n > 0 {
		s.log.Info("清理过期代理授权", zap.Int64("count", n))
	} else if err != nil {
		s.log.Warn("cleanup expired delegations failed", zap.Error(err))
	}
	if n, err := s.cleanupAdminOnlineState(ctx); err == nil && n > 0 {
		s.log.Info("清理管理员在线残留", zap.Int64("count", n))
	} else if err != nil {
		s.log.Warn("cleanup stale admin online state failed", zap.Error(err))
	}
}

func (s *SessionMgmtService) cleanupAdminOnlineState(ctx context.Context) (int64, error) {
	if s.sessions == nil {
		return 0, nil
	}
	ids, err := s.sessions.ListOnlineAdminIDs(ctx)
	if err != nil {
		return 0, err
	}
	var removed int64
	for _, adminID := range ids {
		activeSessions, err := s.pg.ListAdminSessions(ctx, adminID)
		if err != nil {
			return removed, err
		}
		if len(activeSessions) == 0 {
			if err := s.sessions.ClearAdminOnline(ctx, adminID); err != nil {
				return removed, err
			}
			removed++
			continue
		}
		onlineSessionIDs, err := s.sessions.ListAdminOnlineSessionIDs(ctx, adminID)
		if err != nil {
			return removed, err
		}
		for _, sessionID := range staleAdminSessionIDs(onlineSessionIDs, activeSessions) {
			if err := s.sessions.RemoveAdminOnline(ctx, adminID, sessionID); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

func staleAdminSessionIDs(current []string, active []admindomain.AdminSessionRecord) []string {
	if len(current) == 0 {
		return nil
	}
	activeSet := make(map[string]struct{}, len(active))
	for _, item := range active {
		if item.ID != "" {
			activeSet[item.ID] = struct{}{}
		}
	}
	stale := make([]string, 0, len(current))
	seen := make(map[string]struct{}, len(current))
	for _, sessionID := range current {
		if sessionID == "" {
			continue
		}
		if _, duplicated := seen[sessionID]; duplicated {
			continue
		}
		seen[sessionID] = struct{}{}
		if _, ok := activeSet[sessionID]; !ok {
			stale = append(stale, sessionID)
		}
	}
	return stale
}

func latestAdminSessionActivity(items []admindomain.AdminSessionRecord) time.Time {
	var latest time.Time
	for _, item := range items {
		if item.LastActiveAt.After(latest) {
			latest = item.LastActiveAt
		}
	}
	return latest
}
