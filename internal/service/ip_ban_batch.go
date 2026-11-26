package service

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// IP 封禁的批量管理（docs/firewall.md「封禁」）：批量封禁、按选中或按筛选批量解封、修改生效中的封禁、
// 概况统计与「这个 IP 现在会不会被拦」查询。

const (
	// MaxBanBatch 一次批量封禁最多多少个 IP
	MaxBanBatch = 1000
	// MaxUnbanBatch 按筛选批量解封一次最多处理多少条（超过的分批，结果里 truncated=true）
	MaxUnbanBatch = 5000
)

// BanIPsInput 批量封禁
type BanIPsInput struct {
	IPs      []string
	Reason   string
	Duration int64 // 秒，0 = 永久
	Mode     string
	DelayMs  int
}

// BanIPs 批量封禁：逐个校验、去重；已在封禁中的跳过；归属地并发查询（最多 8 路）。
func (s *IPBanService) BanIPs(ctx context.Context, in BanIPsInput, adminID int64) (*firewalldomain.BanBatchResult, error) {
	if in.Duration < 0 {
		return nil, apperrors.New(40001, http.StatusBadRequest, "封禁时长不能为负数")
	}
	if in.Mode != "" && firewalldomain.NormalizeBanMode(in.Mode, "") != in.Mode {
		return nil, apperrors.New(40001, http.StatusBadRequest, "响应模式无效")
	}
	seen := map[string]bool{}
	result := &firewalldomain.BanBatchResult{Items: []firewalldomain.BanBatchItem{}}
	var valid []string
	for _, raw := range in.IPs {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		addr, err := netip.ParseAddr(text)
		if err != nil {
			msg := "不是合法的 IP 地址"
			if strings.Contains(text, "/") {
				msg = "网段请到「访问控制 → 黑名单网段」里加"
			}
			result.Items = append(result.Items, firewalldomain.BanBatchItem{IP: text, Status: "invalid", Error: msg})
			result.Failed++
			continue
		}
		ip := addr.Unmap().String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		valid = append(valid, ip)
	}
	if len(valid)+result.Failed == 0 {
		return nil, apperrors.New(40001, http.StatusBadRequest, "没有可封禁的 IP")
	}
	if len(valid) > MaxBanBatch {
		return nil, apperrors.New(40001, http.StatusBadRequest, fmt.Sprintf("一次最多封禁 %d 个 IP", MaxBanBatch))
	}

	items := make([]firewalldomain.BanBatchItem, len(valid))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, ip := range valid {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			ban, err := s.BanIPWithOptions(ctx, ip, in.Reason, in.Duration, adminID, BanIPOptions{Mode: in.Mode, DelayMs: in.DelayMs})
			switch {
			case err == nil:
				items[i] = firewalldomain.BanBatchItem{IP: ip, BanID: ban.ID, Status: "created"}
			case strings.Contains(err.Error(), "已被封禁"):
				existing, _ := s.pg.GetActiveIPBan(ctx, ip)
				item := firewalldomain.BanBatchItem{IP: ip, Status: "exists", Error: "已在封禁中"}
				if existing != nil {
					item.BanID = existing.ID
				}
				items[i] = item
			default:
				items[i] = firewalldomain.BanBatchItem{IP: ip, Status: "failed", Error: err.Error()}
			}
		}(i, ip)
	}
	wg.Wait()
	for _, item := range items {
		switch item.Status {
		case "created":
			result.Succeeded++
		case "exists":
			result.Skipped++
		default:
			result.Failed++
		}
	}
	result.Items = append(items, result.Items...)
	result.Total = len(result.Items)
	s.log.Info("批量封禁 IP", zap.Int("total", result.Total), zap.Int("created", result.Succeeded), zap.Int("skipped", result.Skipped), zap.Int64("admin_id", adminID))
	return result, nil
}

// UnbanByIDs 批量解封选中的记录（已失效的跳过）。
func (s *IPBanService) UnbanByIDs(ctx context.Context, ids []int64, adminID int64, note string) (*firewalldomain.BanBatchResult, error) {
	if len(ids) == 0 {
		return nil, apperrors.New(40001, http.StatusBadRequest, "没有选中任何封禁")
	}
	if len(ids) > MaxUnbanBatch {
		return nil, apperrors.New(40001, http.StatusBadRequest, fmt.Sprintf("一次最多解封 %d 条", MaxUnbanBatch))
	}
	revoked, err := s.pg.RevokeIPBansByIDs(ctx, dedupeIDs(ids), adminID, strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	result := s.afterRevoke(ctx, revoked, adminID)
	result.Total = len(dedupeIDs(ids))
	result.Skipped = result.Total - result.Succeeded
	return result, nil
}

// UnbanByFilter 按筛选批量解封全部匹配的生效中封禁。expected 是管理员确认时看到的条数：
// 实际条数与之不符（期间有新封禁或别人先解封了）就拒绝，避免「确认 30 条、实际解了 3000 条」。
func (s *IPBanService) UnbanByFilter(ctx context.Context, filter firewalldomain.IPBanFilter, expected int64, adminID int64, note string) (*firewalldomain.BanBatchResult, error) {
	filter.Status = "active"
	if filter.IP == "" && filter.Source == "" && filter.Country == "" && filter.Mode == "" && filter.Keyword == "" &&
		filter.ExpiringWithin == 0 && filter.Permanent == nil && expected <= 0 {
		return nil, apperrors.New(40001, http.StatusBadRequest, "解封全部封禁需要确认条数")
	}
	total, err := s.pg.CountIPBans(ctx, filter)
	if err != nil {
		return nil, err
	}
	if expected > 0 && total != expected {
		return nil, apperrors.New(40990, http.StatusConflict, fmt.Sprintf("匹配的封禁已变为 %d 条（确认时是 %d 条），请刷新后重试", total, expected))
	}
	revoked, err := s.pg.RevokeIPBansByFilter(ctx, filter, adminID, strings.TrimSpace(note), MaxUnbanBatch)
	if err != nil {
		return nil, err
	}
	result := s.afterRevoke(ctx, revoked, adminID)
	result.Total = int(total)
	result.Truncated = total > int64(MaxUnbanBatch)
	return result, nil
}

// afterRevoke 解封落库后清 Redis（失败只记日志：Redis 键也有 TTL，且启动同步只同步生效中的）。
func (s *IPBanService) afterRevoke(ctx context.Context, revoked []firewalldomain.IPBan, adminID int64) *firewalldomain.BanBatchResult {
	result := &firewalldomain.BanBatchResult{Items: make([]firewalldomain.BanBatchItem, 0, len(revoked))}
	for _, ban := range revoked {
		if err := s.redis.RemoveBan(ctx, ban.IP); err != nil {
			s.log.Warn("从 Redis 移除封禁失败", zap.String("ip", ban.IP), zap.Error(err))
		}
		result.Items = append(result.Items, firewalldomain.BanBatchItem{IP: ban.IP, BanID: ban.ID, Status: "revoked"})
	}
	result.Succeeded = len(revoked)
	s.log.Info("批量解封 IP", zap.Int("revoked", len(revoked)), zap.Int64("admin_id", adminID))
	return result
}

// UpdateBan 修改一条生效中的封禁：响应模式、原因、时长（从现在起算，0 = 永久）、tarpit 延迟。
func (s *IPBanService) UpdateBan(ctx context.Context, id int64, patch firewalldomain.IPBanUpdate, adminID int64) (*firewalldomain.IPBan, error) {
	current, err := s.pg.GetIPBan(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, apperrors.New(40493, http.StatusNotFound, "封禁记录不存在")
	}
	if current.Status != "active" {
		return nil, apperrors.New(40991, http.StatusConflict, "这条封禁已失效，不能修改；需要的话重新封禁")
	}
	mode, reason, duration, expiresAt, delay := current.Mode, current.Reason, current.Duration, current.ExpiresAt, current.DelayMs
	if patch.Mode != nil {
		m := strings.TrimSpace(*patch.Mode)
		if m != "" && firewalldomain.NormalizeBanMode(m, "") != m {
			return nil, apperrors.New(40001, http.StatusBadRequest, "响应模式无效")
		}
		mode = m
	}
	if patch.Reason != nil {
		reason = strings.TrimSpace(*patch.Reason)
	}
	if patch.Duration != nil {
		if *patch.Duration < 0 {
			return nil, apperrors.New(40001, http.StatusBadRequest, "封禁时长不能为负数")
		}
		duration = *patch.Duration
		expiresAt = nil
		if duration > 0 {
			t := time.Now().UTC().Add(time.Duration(duration) * time.Second)
			expiresAt = &t
		}
	}
	if patch.DelayMs != nil {
		delay = max(0, min(*patch.DelayMs, 30000))
	}
	updated, err := s.pg.UpdateIPBan(ctx, id, mode, reason, duration, expiresAt, delay, adminID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, apperrors.New(40991, http.StatusConflict, "这条封禁刚刚失效，请刷新")
	}
	ttl := time.Duration(0)
	if updated.ExpiresAt != nil {
		ttl = time.Until(*updated.ExpiresAt)
	}
	if err := s.redis.SetBan(ctx, updated.IP, redisrepo.BanMeta{
		BanID: updated.ID, Reason: updated.Reason, Source: updated.Source, Mode: updated.Mode, DelayMs: updated.DelayMs,
		BannedAt: updated.CreatedAt.UTC().Format(time.RFC3339),
	}, ttl); err != nil {
		s.log.Warn("修改封禁后写 Redis 失败", zap.String("ip", updated.IP), zap.Error(err))
	}
	return updated, nil
}

// UpdateBans 批量修改（响应模式 / 时长），逐条执行，返回逐条结果。
func (s *IPBanService) UpdateBans(ctx context.Context, ids []int64, patch firewalldomain.IPBanUpdate, adminID int64) (*firewalldomain.BanBatchResult, error) {
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return nil, apperrors.New(40001, http.StatusBadRequest, "没有选中任何封禁")
	}
	if len(ids) > MaxBanBatch {
		return nil, apperrors.New(40001, http.StatusBadRequest, fmt.Sprintf("一次最多修改 %d 条", MaxBanBatch))
	}
	result := &firewalldomain.BanBatchResult{Total: len(ids), Items: make([]firewalldomain.BanBatchItem, 0, len(ids))}
	for _, id := range ids {
		ban, err := s.UpdateBan(ctx, id, patch, adminID)
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, firewalldomain.BanBatchItem{BanID: id, Status: "failed", Error: err.Error()})
			continue
		}
		result.Succeeded++
		result.Items = append(result.Items, firewalldomain.BanBatchItem{IP: ban.IP, BanID: id, Status: "updated"})
	}
	return result, nil
}

// Stats 封禁概况。
func (s *IPBanService) Stats(ctx context.Context) (*firewalldomain.IPBanStats, error) {
	return s.pg.IPBanStats(ctx)
}

// CountBans 按筛选计数（批量解封前确认）。
func (s *IPBanService) CountBans(ctx context.Context, filter firewalldomain.IPBanFilter) (int64, error) {
	return s.pg.CountIPBans(ctx, filter)
}

// Inspect 某个 IP 现在会不会被拦、被哪一层拦，以及它的封禁历史与近期拦截次数。
// 地域规则用 MatchIP（不累加命中计数），查询本身不改变任何统计。
func (s *IPBanService) Inspect(ctx context.Context, raw string) (*firewalldomain.IPInspection, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return nil, apperrors.New(40001, http.StatusBadRequest, "不是合法的 IP 地址")
	}
	ip := addr.Unmap().String()
	out := &firewalldomain.IPInspection{IP: ip, History: []firewalldomain.IPBan{}}
	if s.location != nil {
		loc := s.location.Resolve(ctx, ip)
		out.Country, out.CountryCode, out.Region, out.City, out.ISP, out.ASN = loc.Country, loc.CountryCode, loc.Region, loc.City, loc.ISP, loc.Network.ASN
	}
	if active, err := s.pg.GetActiveIPBan(ctx, ip); err == nil && active != nil && (active.ExpiresAt == nil || active.ExpiresAt.After(time.Now())) {
		out.ActiveBan = active
		out.Blocked = true
		out.Layer = "ip_ban"
		out.Decision = firewalldomain.BanDecision{Banned: true, Mode: active.Mode, Reason: active.Reason, BanID: active.ID, DelayMs: active.DelayMs}
	} else if meta, _ := s.redis.GetBanMeta(ctx, ip); meta != nil {
		// 库里没有但 Redis 里还有：多半是解封时 Redis 删除失败，照实报出来
		out.Blocked = true
		out.Layer = "ip_ban"
		out.Decision = firewalldomain.BanDecision{Banned: true, Mode: meta.Mode, Reason: meta.Reason + "（仅缓存中存在）", BanID: meta.BanID}
	}
	if !out.Blocked && s.geo != nil {
		if rule := s.geo.MatchIP(ctx, ip); rule != nil {
			out.Blocked = true
			out.Layer = "geo_ban"
			out.GeoRule = rule
			out.Decision = firewalldomain.BanDecision{Banned: true, Mode: rule.Mode, Reason: rule.Reason, BanID: rule.ID}
		}
	}
	if !out.Blocked && s.fences != nil {
		if dec, _ := s.fences.CheckIP(ctx, ip); dec.Banned {
			out.Blocked = true
			out.Layer = "geo_fence"
			out.Decision = dec
		}
	}
	if out.Blocked && out.Decision.Mode == "" {
		out.Decision.Mode = s.DefaultMode()
	}
	if history, err := s.pg.ListIPBanHistory(ctx, ip, 30); err == nil {
		out.History = history
	}
	now := time.Now().UTC()
	out.Blocks24h, _ = s.pg.CountRecentBlocks(ctx, ip, now.Add(-24*time.Hour))
	out.Blocks7d, _ = s.pg.CountRecentBlocks(ctx, ip, now.Add(-7*24*time.Hour))
	return out, nil
}

func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
