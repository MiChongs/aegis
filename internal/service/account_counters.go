package service

import (
	"context"
	"sync"
	"time"

	redisrepo "aegis/internal/repository/redis"

	"go.uber.org/zap"
)

// AccountCounters 账号删除、解除风控时，主服务里各处的计数一起更新：
//
//   - 注册准入：台账标为已解除、地址 / 设备 / 属地计数按台账重数（解除时清窗口计数与惩罚期），见 register_ip_accounts.go；
//   - 登录防爆破：登录名（解除时连同用过的地址）的失败计数、锁定、退避级别；
//   - 在线人数：删掉的账号残留的在线连接；
//   - 排行榜：应用的分页缓存（删掉的账号不该再上榜）；
//   - 风险分：UserMaxScore 的缓存。
//
// 风控平台侧的滑窗计数、流式特征、图特征缓存在 risk-admin 里清（subject_refresh.go）。
type AccountCounters struct {
	log        *zap.Logger
	registerIP *RegisterIPService
	loginGuard *LoginGuardService
	realtime   *redisrepo.RealtimeRepository
	sessions   *redisrepo.SessionRepository
	risk       *RiskService

	rankMu   sync.Mutex
	rankLast map[int64]time.Time
}

// NewAccountCounters 各依赖都可为 nil（对应的计数不处理）
func NewAccountCounters(log *zap.Logger, registerIP *RegisterIPService, loginGuard *LoginGuardService, realtime *redisrepo.RealtimeRepository,
	sessions *redisrepo.SessionRepository, risk *RiskService) *AccountCounters {
	return &AccountCounters{log: log, registerIP: registerIP, loginGuard: loginGuard, realtime: realtime, sessions: sessions, risk: risk,
		rankLast: map[int64]time.Time{}}
}

// PurgeHook 挂到 UserService.AddPurgeHook：删号前取注册台账，删号后各计数收尾
func (c *AccountCounters) PurgeHook(ctx context.Context, appID, userID int64, account string) func(context.Context) {
	if c == nil {
		return nil
	}
	var regAfter func(context.Context)
	if c.registerIP != nil {
		regAfter = c.registerIP.PrepareUserPurge(ctx, appID, userID)
	}
	return func(ctx context.Context) {
		if regAfter != nil {
			regAfter(ctx)
		}
		if c.loginGuard != nil && account != "" {
			c.loginGuard.ResetAccount(ctx, appID, account)
		}
		if c.realtime != nil {
			if _, err := c.realtime.RemoveUserConnections(ctx, appID, userID); err != nil {
				c.log.Warn("删号后在线连接没有清掉", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
			}
		}
		c.clearRanking(ctx, appID, false)
		if c.risk != nil {
			c.risk.forgetScoreCache(ctx, appID, []int64{userID})
		}
	}
}

// AfterBatch 批量删号结束后再清一次排行榜缓存（批量过程中按秒节流，最后一次兜底）
func (c *AccountCounters) AfterBatch(ctx context.Context, appID int64) {
	if c != nil {
		c.clearRanking(ctx, appID, true)
	}
}

func (c *AccountCounters) clearRanking(ctx context.Context, appID int64, force bool) {
	if c.sessions == nil {
		return
	}
	c.rankMu.Lock()
	if !force && time.Since(c.rankLast[appID]) < time.Second {
		c.rankMu.Unlock()
		return
	}
	c.rankLast[appID] = time.Now()
	c.rankMu.Unlock()
	if err := c.sessions.DeleteRankingCaches(ctx, appID); err != nil {
		c.log.Warn("排行榜缓存没有清掉", zap.Int64("appid", appID), zap.Error(err))
	}
}

// RiskOpsCountersResult 解除风控时主服务这边清掉的计数
type RiskOpsCountersResult struct {
	RegisterCounters int `json:"registerCounters"` // 注册准入计数组（含惩罚期）
	LoginGuards      int `json:"loginGuards"`      // 登录防爆破的登录名与地址
}

// Release 解除风控：这些账号（登录名、注册与用过的地址和设备）的注册准入计数与惩罚期、登录防爆破锁定一并清掉
func (c *AccountCounters) Release(ctx context.Context, appID int64, userIDs []int64, accounts, ips, devices []string, operator string) RiskOpsCountersResult {
	var out RiskOpsCountersResult
	if c == nil {
		return out
	}
	if c.registerIP != nil {
		out.RegisterCounters = c.registerIP.ReleaseUsers(ctx, appID, userIDs, ips, devices, operator)
	}
	if c.loginGuard != nil {
		for _, a := range accounts {
			if a != "" {
				c.loginGuard.ResetAccount(ctx, appID, a)
				out.LoginGuards++
			}
		}
		for _, ip := range ips {
			c.loginGuard.ResetIP(ctx, appID, ip)
			out.LoginGuards++
		}
	}
	if c.risk != nil {
		c.risk.forgetScoreCache(ctx, appID, userIDs)
	}
	return out
}
