package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	riskevents "aegis/services/risk/events"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 风控判出来、要在主服务里执行的处置：封号 / 解封、防火墙封 IP / 解封、踢下线。
// 命令走 RISK_CMD 工作队列，主服务多实例共用一个持久消费者；按命令 ID 记「已执行」，重投不会重复封。

type riskAccountBanner interface {
	BanUser(ctx context.Context, appID int64, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error)
	RevokeBan(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error)
	GetActiveBan(ctx context.Context, appID int64, userID int64) (*userdomain.AccountBan, error)
}

type riskFirewall interface {
	BanIPWithOptions(ctx context.Context, ip string, reason string, durationSec int64, adminID int64, opts BanIPOptions) (*firewalldomain.IPBan, error)
	UnbanIP(ctx context.Context, banID int64, adminID int64) error
	ListBans(ctx context.Context, filter firewalldomain.IPBanFilter) (*firewalldomain.IPBanPage, error)
}

type riskSessionRevoker interface {
	AdminRevokeUserSessions(ctx context.Context, appID int64, userID int64) error
}

// SetAccountBanService 接账号封禁
func (s *RiskService) SetAccountBanService(bans riskAccountBanner) {
	s.mu.Lock()
	s.accountBans = bans
	s.mu.Unlock()
}

// SetFirewall 接防火墙
func (s *RiskService) SetFirewall(firewall riskFirewall) {
	s.mu.Lock()
	s.firewall = firewall
	s.mu.Unlock()
}

// SetSessionRevoker 接踢下线
func (s *RiskService) SetSessionRevoker(sessions riskSessionRevoker) {
	s.mu.Lock()
	s.sessions = sessions
	s.mu.Unlock()
}

// StartCommands 开始消费 RISK_CMD 里归主服务执行的命令
func (s *RiskService) StartCommands(ctx context.Context) error {
	if s == nil || s.bus == nil {
		return nil
	}
	subjects := make([]string, 0, len(riskevents.MainServerCommands()))
	for _, k := range riskevents.MainServerCommands() {
		subjects = append(subjects, riskevents.CommandSubject(k))
	}
	cons, err := s.bus.JetStream().CreateOrUpdateConsumer(ctx, riskevents.StreamCommands, jetstream.ConsumerConfig{
		Durable: "aegis-server-commands", FilterSubjects: subjects, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: time.Minute, MaxDeliver: 20, BackOff: []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute},
	})
	if err != nil {
		return err
	}
	cc, err := cons.Consume(func(m jetstream.Msg) {
		var cmd riskevents.Command
		if err := json.Unmarshal(m.Data(), &cmd); err != nil || cmd.ID == "" {
			_ = m.Term()
			return
		}
		hctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.execute(hctx, cmd); err != nil {
			s.log.Warn("风控命令执行失败，稍后重试", zap.String("id", cmd.ID), zap.String("kind", cmd.Kind), zap.Error(err))
			_ = m.Nak()
			return
		}
		_ = m.Ack()
	})
	if err != nil {
		return err
	}
	s.stopCommands = cc.Stop
	return nil
}

func (s *RiskService) doneKey(id string) string { return s.prefix + "risk:cmd:done:" + id }

// execute 执行一条命令；已执行过的直接返回
func (s *RiskService) execute(ctx context.Context, cmd riskevents.Command) error {
	if s.redis != nil {
		if n, err := s.redis.Exists(ctx, s.doneKey(cmd.ID)).Result(); err == nil && n > 0 {
			return nil
		}
	}
	s.mu.RLock()
	bans, firewall, sessions := s.accountBans, s.firewall, s.sessions
	s.mu.RUnlock()
	var until *time.Time
	if cmd.DurationSeconds > 0 {
		t := time.UnixMilli(cmd.TS).UTC().Add(time.Duration(cmd.DurationSeconds) * time.Second)
		if !t.After(time.Now()) {
			return s.markDone(ctx, cmd) // 处置时长在排队期间已经过了，不再执行
		}
		until = &t
	}
	reason := cmd.Reason
	if reason == "" {
		reason = "风控平台处置"
	}
	operator := userdomain.BanOperator{AdminName: "风控平台"}
	if cmd.Operator != "" {
		operator.AdminName = "风控平台（" + cmd.Operator + "）"
	}
	switch cmd.Kind {
	case "account_ban", "account_unban", "revoke_sessions":
		uid, err := strconv.ParseInt(cmd.Value, 10, 64)
		if err != nil || uid <= 0 || cmd.AppID <= 0 {
			return s.markDone(ctx, cmd) // 认不出用户：没法执行，也不该一直重试
		}
		switch cmd.Kind {
		case "account_ban":
			if bans == nil {
				return errors.New("账号封禁服务未接入")
			}
			if active, err := bans.GetActiveBan(ctx, cmd.AppID, uid); err == nil && active != nil {
				break // 已经在封禁中：不叠一条
			}
			banType := userdomain.AccountBanTypeTemporary
			if until == nil {
				banType = userdomain.AccountBanTypePermanent
			}
			if _, err := bans.BanUser(ctx, cmd.AppID, uid, userdomain.AccountBanCreateInput{
				BanType: banType, BanScope: userdomain.AccountBanScopeAll, Reason: reason, EndAt: until, Operator: operator,
				Evidence: map[string]any{"source": "risk", "commandId": cmd.ID, "decisionId": cmd.DecisionID, "via": cmd.Source},
			}); err != nil {
				return err
			}
			// account.ban 事件由封号服务的 user.onBanned 钩子统一上报（ObserveHook），这里不重复发
		case "account_unban":
			if bans == nil {
				return errors.New("账号封禁服务未接入")
			}
			active, err := bans.GetActiveBan(ctx, cmd.AppID, uid)
			if err == nil && active != nil {
				if _, err := bans.RevokeBan(ctx, cmd.AppID, uid, active.ID, userdomain.AccountBanRevokeInput{Reason: reason, Operator: operator}); err != nil {
					return err
				}
			}
			s.Report(ctx, riskevents.Event{Type: riskevents.TypeAccountUnban, AppID: cmd.AppID, UserID: uid})
		case "revoke_sessions":
			if sessions == nil {
				return errors.New("会话服务未接入")
			}
			if err := sessions.AdminRevokeUserSessions(ctx, cmd.AppID, uid); err != nil {
				return err
			}
		}
	case "firewall_ban":
		if firewall == nil {
			return errors.New("防火墙未接入")
		}
		if _, err := netip.ParseAddr(cmd.Value); err != nil {
			return s.markDone(ctx, cmd)
		}
		if _, err := firewall.BanIPWithOptions(ctx, cmd.Value, reason, cmd.DurationSeconds, 0,
			BanIPOptions{Source: "risk", TriggerRule: "risk:" + cmd.Source + ":" + cmd.ID}); err != nil {
			return err
		}
	case "firewall_unban":
		if firewall == nil {
			return errors.New("防火墙未接入")
		}
		page, err := firewall.ListBans(ctx, firewalldomain.IPBanFilter{IP: cmd.Value, Status: "active", Page: 1, PageSize: 50})
		if err != nil {
			return err
		}
		for _, b := range page.Items {
			if err := firewall.UnbanIP(ctx, b.ID, 0); err != nil {
				return err
			}
		}
	default:
		return s.markDone(ctx, cmd)
	}
	return s.markDone(ctx, cmd)
}

func (s *RiskService) markDone(ctx context.Context, cmd riskevents.Command) error {
	if s.redis == nil {
		return nil
	}
	// 记录保留到工作队列的最长保留时间之后，期间的重投都会被识别出来
	return s.redis.Set(ctx, s.doneKey(cmd.ID), cmd.Kind, 8*24*time.Hour).Err()
}

// ── IP 情报：只读缓存，没有就后台查 ──

func (s *RiskService) ipRepKey(ip string) string { return s.prefix + "risk:iprep:" + ip }

func (s *RiskService) cachedIPReputation(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, bool) {
	if s.redis == nil {
		return nil, false
	}
	raw, err := s.redis.Get(ctx, s.ipRepKey(ip)).Bytes()
	if err == nil {
		var rec securitydomain.IPRiskRecord
		if json.Unmarshal(raw, &rec) == nil {
			return &rec, true
		}
	}
	if errors.Is(err, redis.Nil) {
		go s.refreshIPReputation(ip)
	}
	return nil, false
}

func (s *RiskService) refreshIPReputation(ip string) {
	if addr, err := netip.ParseAddr(ip); err != nil || addr.IsPrivate() || addr.IsLoopback() {
		return
	}
	s.mu.RLock()
	provider, cfg := s.ipRep, s.cfg.IPReputation
	s.mu.RUnlock()
	if provider == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+time.Second)
	defer cancel()
	// 同一个 IP 并发只查一次
	if ok, err := s.redis.SetNX(ctx, s.ipRepKey(ip)+":lock", 1, 30*time.Second).Result(); err != nil || !ok {
		return
	}
	rec, err := provider.Lookup(ctx, ip)
	if err != nil || rec == nil {
		return
	}
	rec = normalizeIPRiskRecord(ip, rec)
	raw, _ := json.Marshal(rec)
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	_ = s.redis.Set(ctx, s.ipRepKey(ip), raw, ttl).Err()
}

// ── 小工具 ──

func ipSubnet(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 64
	}
	p, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return p.String()
}

func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func jsonMarshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("extra 无法序列化：%w", err)
	}
	return raw, nil
}
