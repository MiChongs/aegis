package service

import (
	"aegis/internal/config"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// 登录防爆破维度。
const (
	loginGuardKindAccount = "acct"
	loginGuardKindIP      = "ip"
)

// SceneLoginBruteforce 防爆破锁定写入 risk_assessments 的场景标识。
const SceneLoginBruteforce = "login_bruteforce"

// LoginGuardService 登录防爆破：账号 + IP 双维度失败计数与指数退避锁定。
//
// 语义：
//   - 失败计数在滑动窗口（默认 15m）内累计；登录成功清零
//   - 账号维度阈值（默认 5）防单账号爆破；IP 维度阈值（默认 20）防撞库
//   - 触发锁定后清空计数并按 2 的幂递增锁定时长（5m → 10m → 20m → ... 封顶 24h），
//     退避级别记忆窗口与锁定上限一致
//   - Redis 故障一律 fail-open（只告警不阻断登录），与防火墙限流一致
//   - 每次锁定异步写入一条 risk_assessments（scene=login_bruteforce）供风控中心追溯
type LoginGuardService struct {
	cfg   config.LoginGuardConfig
	log   *zap.Logger
	redis *redisrepo.LoginGuardRepository
	pg    *pgrepo.Repository
}

// NewLoginGuardService 创建登录防爆破服务。
func NewLoginGuardService(cfg config.LoginGuardConfig, log *zap.Logger, redis *redisrepo.LoginGuardRepository, pg *pgrepo.Repository) *LoginGuardService {
	return &LoginGuardService{cfg: config.NormalizeLoginGuardConfig(cfg), log: log, redis: redis, pg: pg}
}

// Check 登录前置检查：账号或 IP 处于锁定期则拒绝。
// 两个维度的 Redis 查询互相独立，并行执行将前置检查耗时降为单次 RTT；
// 错误优先级固定为 账号锁 > IP 锁，与原串行语义一致。
func (s *LoginGuardService) Check(ctx context.Context, appID int64, account, ip string) error {
	if s == nil || !s.cfg.Enabled || s.redis == nil {
		return nil
	}
	var acctErr, ipErr error
	var g errgroup.Group
	if account != "" {
		g.Go(func() error {
			rem, err := s.redis.LockRemaining(ctx, loginGuardKindAccount, appID, account)
			if err != nil {
				s.log.Warn("login guard 账号锁查询失败（fail-open）", zap.Error(err))
			} else if rem > 0 {
				acctErr = apperrors.New(42901, http.StatusTooManyRequests,
					fmt.Sprintf("登录失败次数过多，该账号已被临时锁定，请 %s 后重试", humanizeLockDuration(rem)))
			}
			return nil
		})
	}
	if ip != "" {
		g.Go(func() error {
			rem, err := s.redis.LockRemaining(ctx, loginGuardKindIP, appID, ip)
			if err != nil {
				s.log.Warn("login guard IP 锁查询失败（fail-open）", zap.Error(err))
			} else if rem > 0 {
				ipErr = apperrors.New(42902, http.StatusTooManyRequests,
					fmt.Sprintf("当前网络环境登录异常已被临时限制，请 %s 后重试", humanizeLockDuration(rem)))
			}
			return nil
		})
	}
	_ = g.Wait()
	if acctErr != nil {
		return acctErr
	}
	return ipErr
}

// RegisterFailure 记录一次登录失败（账号不存在 / 密码错误统一计数，防账号枚举探测）。
// 账号 / IP 两个维度的计数与锁定判定互不依赖，并行执行。
func (s *LoginGuardService) RegisterFailure(ctx context.Context, appID int64, account, ip string) {
	if s == nil || !s.cfg.Enabled || s.redis == nil {
		return
	}
	var g errgroup.Group
	if account != "" {
		g.Go(func() error {
			cnt, err := s.redis.IncrFailure(ctx, loginGuardKindAccount, appID, account, s.cfg.Window)
			if err != nil {
				s.log.Warn("login guard 账号失败计数失败", zap.Error(err))
			} else if cnt >= int64(s.cfg.AccountThreshold) {
				s.lock(ctx, loginGuardKindAccount, appID, account, account, ip, cnt)
			}
			return nil
		})
	}
	if ip != "" {
		g.Go(func() error {
			cnt, err := s.redis.IncrFailure(ctx, loginGuardKindIP, appID, ip, s.cfg.Window)
			if err != nil {
				s.log.Warn("login guard IP 失败计数失败", zap.Error(err))
			} else if cnt >= int64(s.cfg.IPThreshold) {
				s.lock(ctx, loginGuardKindIP, appID, ip, account, ip, cnt)
			}
			return nil
		})
	}
	_ = g.Wait()
}

// RegisterSuccess 登录成功后清空两个维度的失败计数（不解除已生效的锁）。
func (s *LoginGuardService) RegisterSuccess(ctx context.Context, appID int64, account, ip string) {
	if s == nil || !s.cfg.Enabled || s.redis == nil {
		return
	}
	if account != "" {
		_ = s.redis.ClearFailure(ctx, loginGuardKindAccount, appID, account)
	}
	if ip != "" {
		_ = s.redis.ClearFailure(ctx, loginGuardKindIP, appID, ip)
	}
}

// lock 触发一次锁定：指数退避定时长、清空计数、记录风控评估。
func (s *LoginGuardService) lock(ctx context.Context, kind string, appID int64, subject, account, ip string, failCount int64) {
	lvl, err := s.redis.EscalateLevel(ctx, kind, appID, subject, s.cfg.MaxLockDuration)
	if err != nil {
		s.log.Warn("login guard 退避级别递增失败", zap.Error(err))
		lvl = 1
	}
	duration := lockDurationForLevel(s.cfg.BaseLockDuration, s.cfg.MaxLockDuration, lvl)
	if err := s.redis.SetLock(ctx, kind, appID, subject, duration); err != nil {
		s.log.Warn("login guard 写入锁定失败", zap.Error(err))
		return
	}
	_ = s.redis.ClearFailure(ctx, kind, appID, subject)

	kindLabel := "账号"
	if kind == loginGuardKindIP {
		kindLabel = "IP"
	}
	s.log.Warn("登录防爆破触发锁定",
		zap.String("kind", kindLabel),
		zap.Int64("app_id", appID),
		zap.String("account", account),
		zap.String("ip", ip),
		zap.Int64("fail_count", failCount),
		zap.Int64("level", lvl),
		zap.Duration("duration", duration),
	)
	s.recordAssessment(kind, appID, account, ip, failCount, lvl, duration)
}

// recordAssessment 异步写入风控评估记录（不阻塞登录请求路径）。
func (s *LoginGuardService) recordAssessment(kind string, appID int64, account, ip string, failCount, level int64, duration time.Duration) {
	if s.pg == nil {
		return
	}
	kindLabel := "account"
	if kind == loginGuardKindIP {
		kindLabel = "ip"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		appIDCopy := appID
		assessment := securitydomain.RiskAssessment{
			Scene: SceneLoginBruteforce,
			AppID: &appIDCopy,
			IP:    ip,
			MatchedRules: []securitydomain.MatchedRule{
				{RuleName: "bruteforce_" + kindLabel, Score: 80},
			},
			TotalScore: 80,
			RiskLevel:  "high",
			Action:     "block",
			ActionDetail: fmt.Sprintf("登录防爆破：%s 维度窗口内失败 %d 次，第 %d 次锁定 %s（account=%s ip=%s）",
				kindLabel, failCount, level, duration, maskAccount(account), ip),
		}
		if _, err := s.pg.CreateRiskAssessment(ctx, assessment); err != nil {
			s.log.Warn("login guard 风控评估写入失败", zap.Error(err))
		}
	}()
}

// lockDurationForLevel 按退避级别计算锁定时长：base × 2^(level-1)，封顶 max。
func lockDurationForLevel(base, max time.Duration, level int64) time.Duration {
	if level < 1 {
		level = 1
	}
	if level > 20 {
		level = 20 // 防位移溢出；2^19 × 5m 已远超任何合理上限
	}
	d := base << uint(level-1)
	if d > max || d <= 0 {
		return max
	}
	return d
}

// humanizeLockDuration 把剩余锁定时长转为用户可读文案。
func humanizeLockDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.1f 小时", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%d 分钟", int(d.Minutes())+1)
	default:
		return fmt.Sprintf("%d 秒", int(d.Seconds())+1)
	}
}

// maskAccount 审计文案中弱化账号明文（保留首尾各 2 字符）。
func maskAccount(account string) string {
	account = strings.TrimSpace(account)
	r := []rune(account)
	if len(r) <= 4 {
		return "***"
	}
	return string(r[:2]) + "***" + string(r[len(r)-2:])
}
