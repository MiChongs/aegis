package service

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// 登录一致性基线的保留时长。语义上是「设备/网络绑定」，因此取足够长的窗口；
// 到期自动回落为「无基线」——长期不登录的账号换机器不该被永久锁死。
const loginBaselineTTL = 180 * 24 * time.Hour

// 网段收敛粒度：IPv4 比到 /24、IPv6 比到 /48。
// 直接比完整 IP 会让同一宽带每次拨号都触发拦截，那是误报而不是防护。
const (
	loginBaselineIPv4Bits = 24
	loginBaselineIPv6Bits = 48
)

// LoginConsistencyService 登录一致性校验：把应用策略里的
// LoginCheckDevice / DeviceRebindInterval / LoginCheckIP / LoginCheckUser
// 落成真正的执行点。
//
// 判定对象是「该用户上一次被放行的登录指纹」（基线），不是全局黑白名单：
//   - 设备：换绑到新设备要满足换绑冷却
//   - 网络：本次登录 IP 与基线不在同一网段即拦截
//   - 属地：本次登录属地（国家 + 省/州）与基线不一致即拦截
//
// 三条都是**默认关闭**的强绑定策略，只对显式开启的应用生效。
// 开启后管理员需要一个解绑出口，否则用户换宽带就永久登不上 ——
// 出口是 Reset（控制台「重置登录绑定」）。
//
// 失败一律 fail-open：Redis 不可用时放行并告警，与防火墙限流、
// LoginGuardService 的取向一致 —— 风控组件挂掉不该连带把登录打死。
type LoginConsistencyService struct {
	log      *zap.Logger
	baseline *redisrepo.LoginBaselineRepository
	location *LocationService
}

// NewLoginConsistencyService 创建登录一致性校验服务。
// location 可为 nil：此时属地校验退化为不判定（缺数据不等于不一致）。
func NewLoginConsistencyService(log *zap.Logger, baseline *redisrepo.LoginBaselineRepository, location *LocationService) *LoginConsistencyService {
	return &LoginConsistencyService{log: log, baseline: baseline, location: location}
}

// Enforce 校验本次登录是否与基线一致，并在放行后把基线推进到本次指纹。
//
// 校验与推进刻意放在同一个调用里：拆成两个调用后，任何一条提前 return
// 的分支都可能漏掉推进，而漏推进的表现是「换绑冷却永远不过期」——
// 一种只在真实使用几周后才暴露的 bug。
func (s *LoginConsistencyService) Enforce(ctx context.Context, policy appdomain.Policy, appID, userID int64, deviceID, ip string) error {
	if s == nil || s.baseline == nil {
		return nil
	}
	// 三项全关时不产生任何 Redis I/O —— 绝大多数应用都走这条路径
	if !policy.LoginCheckDevice && !policy.LoginCheckIP && !policy.LoginCheckUser {
		return nil
	}
	if appID <= 0 || userID <= 0 {
		return nil
	}

	deviceID = strings.TrimSpace(deviceID)
	ip = strings.TrimSpace(ip)

	current, err := s.baseline.Get(ctx, appID, userID)
	if err != nil {
		s.log.Warn("登录基线读取失败（fail-open）",
			zap.Int64("app_id", appID), zap.Int64("user_id", userID), zap.Error(err))
		return nil
	}

	now := timeutil.NowUTC()
	next := appdomain.LoginBaseline{DeviceID: deviceID, IP: ip, UpdatedAt: now}
	if policy.LoginCheckUser {
		next.Region = s.resolveRegion(ctx, ip)
	}

	// 首次登录（或基线已过期）：建立基线并放行
	if current == nil {
		next.DeviceBoundAt = now
		s.commit(ctx, appID, userID, next)
		return nil
	}

	next.DeviceBoundAt = current.DeviceBoundAt
	if next.DeviceBoundAt.IsZero() {
		next.DeviceBoundAt = now
	}
	// 本次登录没带上某个维度的值时沿用基线，避免「少传一个字段就把绑定洗掉」
	if next.Region == "" {
		next.Region = current.Region
	}
	if next.IP == "" {
		next.IP = current.IP
	}
	if next.DeviceID == "" {
		next.DeviceID = current.DeviceID
	}

	if policy.LoginCheckDevice && deviceID != "" && current.DeviceID != "" && deviceID != current.DeviceID {
		if policy.DeviceRebindInterval > 0 {
			cooldown := time.Duration(policy.DeviceRebindInterval) * time.Second
			if elapsed := now.Sub(next.DeviceBoundAt); elapsed < cooldown {
				return apperrors.New(40320, http.StatusForbidden,
					fmt.Sprintf("设备换绑过于频繁，请 %s 后重试", humanizeLockDuration(cooldown-elapsed)))
			}
		}
		next.DeviceBoundAt = now
	}

	if policy.LoginCheckIP && ip != "" && current.IP != "" && !sameLoginNetwork(ip, current.IP) {
		return apperrors.New(40321, http.StatusForbidden,
			"当前网络与上次登录不一致，该应用已开启登录 IP 校验，请联系管理员重置登录绑定")
	}

	if policy.LoginCheckUser && next.Region != "" && current.Region != "" && next.Region != current.Region {
		return apperrors.New(40322, http.StatusForbidden,
			"当前登录属地与上次不一致，该应用已开启登录属地校验，请联系管理员重置登录绑定")
	}

	s.commit(ctx, appID, userID, next)
	return nil
}

// Inspect 读取某用户当前基线，供控制台展示「绑定了哪台设备 / 哪个网段 / 哪个属地」。
func (s *LoginConsistencyService) Inspect(ctx context.Context, appID, userID int64) (*appdomain.LoginBaseline, error) {
	if s == nil || s.baseline == nil {
		return nil, nil
	}
	return s.baseline.Get(ctx, appID, userID)
}

// Reset 清除基线：下次登录按首次处理并重建。开启强绑定策略后唯一的解绑出口。
func (s *LoginConsistencyService) Reset(ctx context.Context, appID, userID int64) error {
	if s == nil || s.baseline == nil {
		return nil
	}
	if appID <= 0 || userID <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "应用与用户标识不能为空")
	}
	return s.baseline.Delete(ctx, appID, userID)
}

// commit 写入基线。写失败只告警：本次登录已经通过校验，
// 因为一次写失败把用户挡在外面是更坏的结果。
func (s *LoginConsistencyService) commit(ctx context.Context, appID, userID int64, baseline appdomain.LoginBaseline) {
	if err := s.baseline.Set(ctx, appID, userID, baseline, loginBaselineTTL); err != nil {
		s.log.Warn("登录基线写入失败",
			zap.Int64("app_id", appID), zap.Int64("user_id", userID), zap.Error(err))
	}
}

// resolveRegion 归一化属地标识（国家码/省）。
// 取不到属地时返回空串，由调用方按「不判定」处理 —— 内网 IP、
// mmdb 未覆盖的地址都会走到这里，把它们当成「属地变更」会造成大面积误拦。
func (s *LoginConsistencyService) resolveRegion(ctx context.Context, ip string) string {
	if s.location == nil || ip == "" {
		return ""
	}
	loc := s.location.Resolve(ctx, ip)
	if loc.IsPrivate {
		return ""
	}
	country := strings.TrimSpace(loc.CountryCode)
	if country == "" {
		country = strings.TrimSpace(loc.Country)
	}
	region := strings.TrimSpace(loc.Region)
	if country == "" && region == "" {
		return ""
	}
	return strings.ToLower(country + "/" + region)
}

// sameLoginNetwork 判断两个 IP 是否落在同一收敛网段。
// 无法解析时返回 true（按一致处理）：解析失败是我们的问题，不该由用户承担拦截。
func sameLoginNetwork(a, b string) bool {
	left, err := netip.ParseAddr(strings.TrimSpace(a))
	if err != nil {
		return true
	}
	right, err := netip.ParseAddr(strings.TrimSpace(b))
	if err != nil {
		return true
	}
	left, right = left.Unmap(), right.Unmap()
	if left.Is4() != right.Is4() {
		return false
	}
	bits := loginBaselineIPv6Bits
	if left.Is4() {
		bits = loginBaselineIPv4Bits
	}
	prefix, err := left.Prefix(bits)
	if err != nil {
		return true
	}
	return prefix.Contains(right)
}
