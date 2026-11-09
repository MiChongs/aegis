package service

import (
	"context"
	"strings"

	"aegis/internal/domain/regip"
	redisrepo "aegis/internal/repository/redis"

	"go.uber.org/zap"
)

// 注册准入的计数随账号的删除与解除一起更新。
//
// 台账 register_ip_ledger.user_id 是 ON DELETE SET NULL：账号删掉后台账还在，地址、设备、属地的累计注册数仍把它算进去，
// 窗口计数（Redis）也一直挂着它那一笔。删号前先记下它的台账，删号后：
//   - 台账标为已解除（不再计入累计数）；
//   - 这些地址、网段、设备、属地的计数键删掉，累计数下次注册时按台账重数；窗口计数里的单笔删不掉，整键清掉；
//   - 应用级（scope=app）只清累计数，窗口计数是全应用的，不因为一个账号整体放开。
//
// 解除风控时：这些账号注册时与之后用过的地址、设备的窗口计数与惩罚期一并清掉（惩罚等级保留，与管理员手动解除一致），
// 台账不动（账号还在，注册记录是真实的）。

// RegisterIPSubject 一个要重算的注册主体：地址、设备、属地
type RegisterIPSubject struct {
	IP       string
	Device   string
	Country  string
	Province string
	City     string
}

// PrepareUserPurge 删号前取台账；返回删号成功后执行的收尾
func (s *RegisterIPService) PrepareUserPurge(ctx context.Context, appID, userID int64) func(context.Context) {
	if s == nil || s.pg == nil {
		return nil
	}
	refs, err := s.pg.RegisterIPLedgerOfUsers(ctx, appID, []int64{userID})
	if err != nil || len(refs) == 0 {
		return nil
	}
	return func(ctx context.Context) {
		var ids []int64
		subjects := make([]RegisterIPSubject, 0, len(refs))
		for _, r := range refs {
			if !r.Released {
				ids = append(ids, r.ID)
			}
			subjects = append(subjects, RegisterIPSubject{IP: r.IP, Device: r.Device, Country: r.Country, Province: r.Province, City: r.City})
		}
		if _, err := s.pg.ReleaseRegisterIPLedgerIDs(ctx, ids); err != nil {
			s.log.Warn("删号后注册台账没有标为已解除", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
		n := s.ClearSubjects(ctx, appID, subjects, false)
		s.events.Enqueue(regip.Event{AppID: appID, Kind: regip.EventReleased, At: s.now(), Operator: "账号删除",
			Detail: "账号已删除，注册计数按台账重算", Extra: map[string]any{"userId": userID, "countersCleared": n, "ledgerReleased": len(ids)}})
	}
}

// ReleaseUsers 解除风控：这些账号的注册台账里的地址、设备，加上额外给的地址与设备，清窗口计数与惩罚期
func (s *RegisterIPService) ReleaseUsers(ctx context.Context, appID int64, userIDs []int64, ips, devices []string, operator string) int {
	if s == nil || s.pg == nil {
		return 0
	}
	var subjects []RegisterIPSubject
	if refs, err := s.pg.RegisterIPLedgerOfUsers(ctx, appID, userIDs); err == nil {
		for _, r := range refs {
			subjects = append(subjects, RegisterIPSubject{IP: r.IP, Device: r.Device, Country: r.Country, Province: r.Province, City: r.City})
		}
	}
	for _, ip := range ips {
		subjects = append(subjects, RegisterIPSubject{IP: ip})
	}
	for _, d := range devices {
		subjects = append(subjects, RegisterIPSubject{Device: d})
	}
	n := s.ClearSubjects(ctx, appID, subjects, true)
	if n > 0 {
		s.events.Enqueue(regip.Event{AppID: appID, Kind: regip.EventReleased, At: s.now(), Operator: operator,
			Detail: "一键解除风控：清掉相关地址与设备的注册计数和惩罚期", Extra: map[string]any{"userIds": userIDs, "countersCleared": n}})
	}
	return n
}

// ClearSubjects 清这些主体在各规则里的计数（penalty 时连惩罚期一起清）；返回清掉的计数组数
func (s *RegisterIPService) ClearSubjects(ctx context.Context, appID int64, subjects []RegisterIPSubject, penalty bool) int {
	if s == nil || s.counters == nil || len(subjects) == 0 {
		return 0
	}
	policy, err := s.policyOf(ctx, appID)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	var counters []redisrepo.RegisterIPCounter
	penalties := map[string]bool{}
	for _, sub := range subjects {
		var addr regip.Address
		if strings.TrimSpace(sub.IP) != "" {
			if a, ok := regip.ParseAddress(sub.IP, policy.Network); ok {
				addr = a
			}
		}
		dev := regip.ParseDevice(sub.Device, policy.Device.MinLength)
		loc := regip.Location{Country: sub.Country, Province: sub.Province, City: sub.City, Resolved: strings.TrimSpace(sub.Country) != ""}
		for _, rule := range policy.Rules {
			// 应用级窗口计数是全应用的：不因为一个账号整体放开，只让累计数重数
			if rule.Scope == regip.ScopeApp && !rule.Lifetime() {
				continue
			}
			subject := regip.SubjectKey(rule.Scope, addr, dev, loc)
			if subject == "" {
				continue
			}
			key := rule.ID + "|" + subject
			if seen[key] {
				continue
			}
			seen[key] = true
			counters = append(counters, redisrepo.RegisterIPCounter{Rule: rule.ID, Subject: subject})
		}
		if penalty {
			if addr.Host.IsValid() {
				penalties[addr.Host.String()] = true
			}
			if dev.Valid {
				penalties[dev.Subject()] = true
			}
		}
	}
	if err := s.counters.ClearCounters(ctx, appID, counters); err != nil {
		s.log.Warn("注册计数没有清掉", zap.Int64("appid", appID), zap.Error(err))
		return 0
	}
	for subject := range penalties {
		_ = s.counters.ClearPenalty(ctx, appID, subject, false)
	}
	return len(counters)
}
