package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"aegis/internal/domain/regip"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// 注册准入的设备一侧：设备名单快照、设备查询（与地址的关联）、设备名单管理。规则见 docs/register-ip.md。

// deviceListsFor 应用的设备名单快照：与地址名单同样的 15 秒缓存、singleflight、后台刷新、旧快照先顶上。
func (s *RegisterIPService) deviceListsFor(ctx context.Context, appID int64) *regip.CompiledDeviceLists {
	now := s.now()
	cached, _ := s.deviceLists.Load(appID)
	snapshot, _ := cached.(*registerDeviceListSnapshot)
	if snapshot != nil && now.Sub(snapshot.loadedAt) < registerIPListTTL {
		return snapshot.lists
	}
	load := func() *regip.CompiledDeviceLists {
		value, err, _ := s.listGroup.Do("d"+itoa64(appID), func() (any, error) {
			entries, err := s.pg.ListActiveRegisterDeviceLists(context.WithoutCancel(ctx), appID)
			if err != nil {
				return nil, err
			}
			compiled := regip.CompileDeviceLists(entries, s.now())
			s.deviceLists.Store(appID, &registerDeviceListSnapshot{lists: compiled, loadedAt: s.now()})
			return compiled, nil
		})
		if err != nil {
			s.log.Warn("设备名单加载失败", zap.Int64("appid", appID), zap.Error(err))
			if snapshot != nil {
				return snapshot.lists
			}
			return nil
		}
		compiled, _ := value.(*regip.CompiledDeviceLists)
		return compiled
	}
	if snapshot != nil {
		go load()
		return snapshot.lists
	}
	return load()
}

func (s *RegisterIPService) invalidateDeviceLists(appID int64) { s.deviceLists.Delete(appID) }

func (s *RegisterIPService) countDeviceHit(id int64) {
	s.hitsMu.Lock()
	s.deviceHits[id]++
	s.hitsMu.Unlock()
}

/* ───────────────────── 设备查询 ───────────────────── */

// RegisterDeviceInspection 设备查询的结果。
type RegisterDeviceInspection struct {
	Device  regip.Device           `json:"device"`
	List    *regip.DeviceListEntry `json:"list"`
	Match   *regip.ListMatch       `json:"match"`
	Penalty struct {
		Subject   string     `json:"subject"`
		Remaining int        `json:"remaining"`
		Level     int        `json:"level"`
		ExpiresAt *time.Time `json:"expiresAt"`
	} `json:"penalty"`
	// Decision 此刻这台设备（不带地址）按密码注册会怎样；地址一侧的规则不参与
	Decision regip.Decision    `json:"decision"`
	Usage    []regip.RuleUsage `json:"usage"`
	// Registrations 这台设备注册的账号；Active 是未解除的数量
	Registrations []regip.Registration `json:"registrations"`
	Active        int64                `json:"active"`
	// Addresses 这台设备注册时用过的地址（台账）；Recent 是去重计数里此刻窗口内的地址
	Addresses      []regip.LinkedItem `json:"addresses"`
	AddressesTotal int64              `json:"addressesTotal"`
	Recent         []RegisterIPMember `json:"recent"`
	Events         []regip.Event      `json:"events"`
}

// RegisterIPMember 去重计数里的一个成员与它最近一次出现的时间。
type RegisterIPMember struct {
	Key    string    `json:"key"`
	SeenAt time.Time `json:"seenAt"`
	RuleID string    `json:"ruleId"`
}

// InspectDevice 查一台设备此刻的全部状况。七样数据互不依赖，并行取。
func (s *RegisterIPService) InspectDevice(ctx context.Context, appID int64, raw string) (*RegisterDeviceInspection, error) {
	policy, err := s.policyOf(ctx, appID)
	if err != nil {
		return nil, err
	}
	dev := regip.ParseDevice(raw, policy.Device.MinLength)
	if !dev.Present() {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入设备标识")
	}
	out := &RegisterDeviceInspection{Device: dev, Usage: []regip.RuleUsage{}, Recent: []RegisterIPMember{}}
	probe := policy
	probe.Enabled = true
	g, gctx := errgroup.WithContext(ctx)
	var eval *registerIPEvaluation
	g.Go(func() error {
		eval = s.evaluate(gctx, RegisterIPRequest{AppID: appID, DeviceID: dev.Raw, Method: regip.MethodPassword}, probe, false, evalOptions{})
		return nil
	})
	g.Go(func() (err error) {
		out.List, err = s.pg.FindRegisterDeviceList(gctx, appID, dev.ID)
		return err
	})
	g.Go(func() (err error) {
		out.Registrations, out.Active, err = s.pg.RegisterIPLedgerByDevice(gctx, appID, dev.ID, 30)
		return err
	})
	g.Go(func() (err error) {
		out.Addresses, out.AddressesTotal, err = s.pg.RegisterIPAddressesOfDevice(gctx, appID, dev.ID, 30)
		return err
	})
	g.Go(func() (err error) {
		out.Events, _, err = s.pg.ListRegisterIPEvents(gctx, regip.EventQuery{AppID: appID, DeviceID: dev.ID, Page: 1, PageSize: 20})
		return err
	})
	if s.counters != nil && dev.Valid {
		g.Go(func() error {
			remaining, level, err := s.counters.PenaltyOf(gctx, appID, dev.Subject())
			if err != nil {
				return nil
			}
			out.Penalty.Subject, out.Penalty.Remaining, out.Penalty.Level = dev.Subject(), int(remaining.Seconds()), level
			if remaining > 0 {
				at := s.now().Add(remaining)
				out.Penalty.ExpiresAt = &at
			}
			return nil
		})
		// 去重计数里此刻窗口内的地址：多条「同一设备换地址」规则看到的成员一样，取第一条
		for _, rule := range policy.Rules {
			if !rule.Enabled || rule.Scope != regip.ScopeDeviceIPs {
				continue
			}
			counter := redisrepo.RegisterIPCounter{Kind: redisrepo.RegisterIPCounterDistinct, Rule: rule.ID, Subject: dev.Subject(), WindowMs: int64(rule.WindowSeconds) * 1000}
			ruleID := rule.ID
			g.Go(func() error {
				items, err := s.counters.DistinctMembers(gctx, appID, counter, 30)
				if err != nil {
					return nil
				}
				for _, item := range items {
					out.Recent = append(out.Recent, RegisterIPMember{Key: memberString(item.Member), SeenAt: time.UnixMilli(int64(item.Score)), RuleID: ruleID})
				}
				return nil
			})
			break
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out.Match = eval.facts.List
	out.Decision = eval.decision
	if !policy.Enabled {
		out.Decision = regip.Decide(policy, eval.facts)
		out.Decision.Rules = eval.decision.Rules
	}
	for _, hit := range eval.decision.Rules {
		if !regip.DeviceScope(hit.Scope) {
			continue
		}
		usage := regip.RuleUsage{RuleHit: hit, Remaining: max(0, hit.Limit-hit.Count)}
		for i, counter := range eval.counters {
			if policy.Rules[eval.ruleOf[i]].ID != hit.RuleID || s.counters == nil {
				continue
			}
			if oldest, err := s.counters.WindowOldest(ctx, appID, counter); err == nil && !oldest.IsZero() {
				reset := oldest.Add(time.Duration(hit.WindowSeconds) * time.Second)
				usage.ResetAt = &reset
			}
		}
		out.Usage = append(out.Usage, usage)
	}
	return out, nil
}

func memberString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

/* ───────────────────── 设备名单 ───────────────────── */

// AddDeviceLists 批量添加设备名单（一次最多 500 项）。无效的设备标识直接拒绝：放行一个 "aaaaaaaa" 等于放行所有伪造它的人。
func (s *RegisterIPService) AddDeviceLists(ctx context.Context, appID int64, in regip.ListInput, operator string) ([]regip.DeviceListEntry, error) {
	kind, factor, err := normalizeListInput(in.Kind, in.Factor)
	if err != nil {
		return nil, err
	}
	in.Kind, in.Factor = kind, factor
	policy, err := s.policyOf(ctx, appID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	devices := []string{}
	for _, value := range in.Values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' || r == '，' || r == ';' }) {
			dev := regip.ParseDevice(part, policy.Device.MinLength)
			if !dev.Present() {
				continue
			}
			if !dev.Valid && kind != regip.ListDeny {
				return nil, apperrors.New(40000, http.StatusBadRequest, "设备标识 "+part+" 无效（"+dev.InvalidReason+"），只能加入拒绝名单")
			}
			if !seen[dev.ID] {
				seen[dev.ID] = true
				devices = append(devices, dev.ID)
			}
		}
	}
	if len(devices) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请至少填写一个设备标识")
	}
	if len(devices) > 500 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多添加 500 项")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "到期时间需要晚于现在")
	}
	items, err := s.pg.UpsertRegisterDeviceLists(ctx, appID, devices, in, operator)
	if err != nil {
		return nil, err
	}
	s.invalidateDeviceLists(appID)
	return items, nil
}

// UpdateDeviceList 修改一项设备名单。
func (s *RegisterIPService) UpdateDeviceList(ctx context.Context, appID, id int64, in regip.ListUpdate) (*regip.DeviceListEntry, error) {
	kind, factor, err := normalizeListInput(in.Kind, in.Factor)
	if err != nil {
		return nil, err
	}
	in.Kind, in.Factor = kind, factor
	item, err := s.pg.UpdateRegisterDeviceList(ctx, appID, id, in)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40400, http.StatusNotFound, "名单项不存在")
	}
	s.invalidateDeviceLists(appID)
	return item, nil
}

// DeleteDeviceLists 删除设备名单项。
func (s *RegisterIPService) DeleteDeviceLists(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "请选择要删除的名单项")
	}
	n, err := s.pg.DeleteRegisterDeviceLists(ctx, appID, ids)
	if err != nil {
		return 0, err
	}
	s.invalidateDeviceLists(appID)
	return n, nil
}

// ListDeviceLists 设备名单列表。
func (s *RegisterIPService) ListDeviceLists(ctx context.Context, q regip.ListQuery) ([]regip.DeviceListEntry, int64, error) {
	q.Page, q.PageSize = normalizePageLimit(q.Page, q.PageSize, 200)
	return s.pg.ListRegisterDeviceLists(ctx, q)
}
