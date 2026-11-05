package service

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"aegis/internal/service/chinageo"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// 归属地来源按 GEOIP_PROVIDERS 排出优先级（默认 meituan,btcn,uapis）。
//
// 查询分两轮：不限额的来源（美团、宝塔）并发查同一批地址；有额度的来源（UAPIs）
// 只补前一轮没查出来、缺运营商或缺国家代码的地址，IPv6 基本都落到这一轮。
// 合并时以优先级最高、且查到了的那份为准，后面的来源只填空：运营商、ASN、坐标、
// 同省同市下缺的区县。不同来源对同一地址说法不一（省份对不上）时不混拼。
//
// 每个来源各有一个熔断：连续失败三次后停用一段时间（1 分钟起翻倍，封顶 30 分钟），
// 接口明说要等多久（限流、额度用尽）就照它说的等。美团的 IP 定位从 2026 年年中起
// 对所有地址都回 404「Can Not Loc」，这种「整体失效但不报错」的情况靠 strictCN 识别：
// 别的来源判成国内的地址它却说查不到，记一次故障。

const (
	sourceFailureThreshold = 3
	sourceBaseCooldown     = time.Minute
	sourceMaxCooldown      = 30 * time.Minute
	sourceMaxWait          = 24 * time.Hour
)

type locationSource struct {
	name string
	// metered 有调用额度：只给不限额来源没查出来、或缺运营商的地址补查
	metered bool
	// strictCN 国内地址都该查得到：对别的来源判成国内的地址回「查不到」算一次故障
	strictCN bool
	supports func(netip.Addr) bool
	// batch 与 single 二选一：batch 一次查多个，single 一次查一个（false 表示明确查不到）
	batch       func(ctx context.Context, ips []string) (map[string]IPLocation, error)
	batchSize   int
	single      func(ctx context.Context, ip string) (IPLocation, bool, error)
	concurrency int
	limiter     *rate.Limiter
	health      sourceHealth
}

// sourceAnswer 一个来源对一批地址的回答。pending 是该查却没查成的地址（网络错误、超时、限流、熔断中），
// 这些地址的「查不到」不能当真，不写进长缓存。
type sourceAnswer struct {
	found   map[string]IPLocation
	missed  map[string]bool
	pending map[string]bool
}

// sourceCooldownError 接口明确告知需要等待（限流、额度用尽、密钥无效）。
type sourceCooldownError struct {
	err  error
	wait time.Duration
}

func (e *sourceCooldownError) Error() string { return e.err.Error() }
func (e *sourceCooldownError) Unwrap() error { return e.err }

type sourceHealth struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

func (h *sourceHealth) allow(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !now.Before(h.openUntil)
}

func (h *sourceHealth) succeed() {
	h.mu.Lock()
	h.failures = 0
	h.openUntil = time.Time{}
	h.mu.Unlock()
}

// fail 记一次故障，返回这次是否进入停用期以及停用多久。wait>0 时直接按它停用。
func (h *sourceHealth) fail(now time.Time, wait time.Duration) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if wait <= 0 && now.Before(h.openUntil) {
		// 停用前已经发出的并发请求陆续失败，不该把停用时长一路翻上去
		return 0, false
	}
	h.failures++
	if wait <= 0 {
		if h.failures < sourceFailureThreshold {
			return 0, false
		}
		wait = sourceBaseCooldown
		for i := sourceFailureThreshold; i < h.failures && wait < sourceMaxCooldown; i++ {
			wait *= 2
		}
		wait = min(wait, sourceMaxCooldown)
	}
	wait = min(wait, sourceMaxWait)
	if until := now.Add(wait); until.After(h.openUntil) {
		h.openUntil = until
	}
	return wait, true
}

func (s *LocationService) noteSourceFailure(src *locationSource, wait time.Duration, err error) {
	if cooldown, opened := src.health.fail(time.Now(), wait); opened {
		fields := []zap.Field{zap.String("provider", src.name), zap.Duration("cooldown", cooldown)}
		if err != nil {
			fields = append(fields, zap.Error(err))
		}
		s.log.Warn("location provider paused", fields...)
	}
}

// runSource 让一个来源查一批地址（已按来源的支持范围过滤）。
func (s *LocationService) runSource(ctx context.Context, src *locationSource, ips []string) sourceAnswer {
	answer := sourceAnswer{found: map[string]IPLocation{}, missed: map[string]bool{}, pending: map[string]bool{}}
	eligible := make([]string, 0, len(ips))
	for _, ip := range ips {
		if addr, err := netip.ParseAddr(ip); err == nil && (src.supports == nil || src.supports(addr)) {
			eligible = append(eligible, ip)
		}
	}
	if len(eligible) == 0 {
		return answer
	}
	if !src.health.allow(time.Now()) {
		for _, ip := range eligible {
			answer.pending[ip] = true
		}
		return answer
	}

	if src.batch != nil {
		s.runBatchSource(ctx, src, eligible, &answer)
		return answer
	}
	ctx, cancel := context.WithTimeout(ctx, s.providerTimeout)
	defer cancel()
	s.runSingleSource(ctx, src, eligible, &answer)
	return answer
}

func (s *LocationService) runBatchSource(ctx context.Context, src *locationSource, ips []string, answer *sourceAnswer) {
	size := max(src.batchSize, 1)
	for start := 0; start < len(ips); start += size {
		chunk := ips[start:min(start+size, len(ips))]
		if !src.health.allow(time.Now()) || ctx.Err() != nil {
			for _, ip := range chunk {
				answer.pending[ip] = true
			}
			continue
		}
		chunkCtx, cancel := context.WithTimeout(ctx, s.providerTimeout)
		got, err := src.batch(chunkCtx, chunk)
		cancel()
		if err != nil {
			for _, ip := range chunk {
				answer.pending[ip] = true
			}
			if ctx.Err() == nil {
				s.log.Debug("location provider failed", zap.String("provider", src.name), zap.Strings("ips", chunk), zap.Error(err))
			}
			s.noteSourceFailure(src, cooldownOf(err), err)
			continue
		}
		src.health.succeed()
		for _, ip := range chunk {
			if loc, ok := got[ip]; ok && loc.isResolved() {
				answer.found[ip] = loc
			} else {
				answer.missed[ip] = true
			}
		}
	}
}

func (s *LocationService) runSingleSource(ctx context.Context, src *locationSource, ips []string, answer *sourceAnswer) {
	var (
		mu        sync.Mutex
		responded bool
		lastErr   error
		wait      time.Duration
	)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range max(min(src.concurrency, len(ips)), 1) {
		wg.Go(func() {
			for ip := range jobs {
				loc, ok, err := s.querySingle(ctx, src, ip)
				mu.Lock()
				switch {
				case err != nil:
					answer.pending[ip] = true
					if !errors.Is(err, errSourcePaused) {
						lastErr = err
						wait = max(wait, cooldownOf(err))
					}
				case ok && loc.isResolved():
					responded = true
					answer.found[ip] = loc
				default:
					responded = true
					answer.missed[ip] = true
				}
				mu.Unlock()
			}
		})
	}
	for _, ip := range ips {
		jobs <- ip
	}
	close(jobs)
	wg.Wait()

	switch {
	case wait > 0:
		s.noteSourceFailure(src, wait, lastErr)
	case len(answer.found) > 0 || (responded && !src.strictCN):
		src.health.succeed()
	case lastErr != nil && !responded:
		if ctx.Err() == nil {
			s.log.Debug("location provider failed", zap.String("provider", src.name), zap.Error(lastErr))
		}
		s.noteSourceFailure(src, 0, lastErr)
	}
	// strictCN 来源只回了「查不到」：等合并完再判断是不是接口失效（见 lookupSources）
}

var errSourcePaused = errors.New("location provider paused")

func (s *LocationService) querySingle(ctx context.Context, src *locationSource, ip string) (IPLocation, bool, error) {
	if err := ctx.Err(); err != nil {
		return IPLocation{}, false, err
	}
	if !src.health.allow(time.Now()) {
		return IPLocation{}, false, errSourcePaused
	}
	if src.limiter != nil {
		if err := src.limiter.Wait(ctx); err != nil {
			return IPLocation{}, false, err
		}
	}
	return src.single(ctx, ip)
}

func cooldownOf(err error) time.Duration {
	var cooldown *sourceCooldownError
	if errors.As(err, &cooldown) {
		return cooldown.wait
	}
	return 0
}

// lookupSources 按优先级查询并合并。返回合并结果，以及因来源故障而没能查全的地址。
func (s *LocationService) lookupSources(ctx context.Context, ips []string) (map[string]IPLocation, map[string]bool) {
	answers := make([]sourceAnswer, len(s.sources))
	var wg sync.WaitGroup
	for i, src := range s.sources {
		if !src.metered {
			wg.Go(func() { answers[i] = s.runSource(ctx, src, ips) })
		}
	}
	wg.Wait()

	merged := make(map[string]IPLocation, len(ips))
	remerge := func(targets []string) {
		for _, ip := range targets {
			parts := make([]IPLocation, 0, len(answers))
			for _, answer := range answers {
				if loc, ok := answer.found[ip]; ok {
					parts = append(parts, loc)
				}
			}
			if loc, ok := mergeLocations(parts); ok {
				merged[ip] = loc
			}
		}
	}
	remerge(ips)
	s.judgeStrictSources(answers, merged)

	for i, src := range s.sources {
		if !src.metered {
			continue
		}
		need := make([]string, 0, len(ips))
		for _, ip := range ips {
			// 没查到、缺运营商、或只认出机构名（GoogleDNS 之类，没有国家代码，地理封禁用不上）
			if loc, ok := merged[ip]; !ok || (!loc.IsPrivate && (loc.ISP == "" || loc.CountryCode == "")) {
				need = append(need, ip)
			}
		}
		if len(need) == 0 || ctx.Err() != nil {
			break
		}
		answers[i] = s.runSource(ctx, src, need)
		remerge(need)
	}

	incomplete := make(map[string]bool)
	for _, ip := range ips {
		if _, ok := merged[ip]; ok {
			continue
		}
		for _, answer := range answers {
			if answer.pending[ip] {
				incomplete[ip] = true
				break
			}
		}
	}
	return merged, incomplete
}

// judgeStrictSources 美团这类国内地址都该查得到的来源，若本轮一个都没查到、
// 却对别的来源判成国内的地址说查不到，就当它这一轮失败。
func (s *LocationService) judgeStrictSources(answers []sourceAnswer, merged map[string]IPLocation) {
	for i, src := range s.sources {
		if !src.strictCN || len(answers[i].found) > 0 {
			continue
		}
		for ip := range answers[i].missed {
			if merged[ip].CountryCode == "CN" {
				s.noteSourceFailure(src, 0, errors.New("no answer for a mainland address"))
				break
			}
		}
	}
}

// mergeLocations 按优先级合并各来源的结果：第一份为准，后面的只填空。
func mergeLocations(parts []IPLocation) (IPLocation, bool) {
	var base IPLocation
	found := false
	sources := make([]string, 0, len(parts))
	geoChanged := false
	for _, part := range parts {
		if !part.isResolved() {
			continue
		}
		if !found {
			base, found = part, true
			sources = append(sources, part.Source)
			if part.IsPrivate {
				break
			}
			continue
		}
		used := false
		if base.Country == "" && base.CountryCode == "" && part.Country != "" {
			// 首选来源只给出机构名（GoogleDNS 之类），地理信息整份取后面的
			base.Country, base.CountryCode = part.Country, part.CountryCode
			base.Region, base.City, base.District = part.Region, part.City, part.District
			if base.Coordinates == nil {
				base.Coordinates = part.Coordinates
			}
			used, geoChanged = true, true
		}
		sameCountry := base.CountryCode == "" || part.CountryCode == "" || base.CountryCode == part.CountryCode
		if sameCountry {
			if base.CountryCode == "" && part.CountryCode != "" {
				base.CountryCode, used = part.CountryCode, true
			}
			switch {
			case base.Region == "" && base.City == "" && (part.Region != "" || part.City != ""):
				base.Region, base.City, base.District = part.Region, part.City, part.District
				used, geoChanged = true, true
			case samePlaceName(base.Region, part.Region) && base.City == "" && part.City != "":
				base.City, base.District = part.City, part.District
				used, geoChanged = true, true
			case samePlaceName(base.Region, part.Region) && samePlaceName(base.City, part.City) && base.District == "" && part.District != "":
				base.District = part.District
				used, geoChanged = true, true
			}
			if base.Timezone == "" && part.Timezone != "" {
				base.Timezone, used = part.Timezone, true
			}
			if base.Coordinates == nil && part.Coordinates != nil {
				base.Coordinates, used = part.Coordinates, true
			}
		}
		if base.ISP == "" && part.ISP != "" {
			base.ISP, used = part.ISP, true
		}
		if base.Network.Type == "" && part.Network.Type != "" {
			base.Network.Type, used = part.Network.Type, true
		}
		if base.Network.Organization == "" && part.Network.Organization != "" {
			base.Network.Organization, used = part.Network.Organization, true
		}
		if base.Network.ASN == "" && part.Network.ASN != "" {
			base.Network.ASN, used = part.Network.ASN, true
		}
		if used {
			sources = append(sources, part.Source)
		}
	}
	if !found {
		return IPLocation{}, false
	}
	if geoChanged {
		if base.CountryCode == "CN" {
			if place, ok := chinageo.Resolve(base.Region, base.City, base.District); ok {
				base.Region, base.City, base.District = place.Province, place.City, place.District
				lat, lng := place.Lat, place.Lng
				base.Coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
			}
		}
		base.Location = composeLocation(base.Country, base.Region, base.City, base.District)
	}
	base.Source = strings.Join(sources, "+")
	return base, true
}

// samePlaceName 比较地名，忽略「省 / 市 / 区 / 县」之类的后缀写法差异；任一方为空视为对得上。
func samePlaceName(a, b string) bool {
	a, b = trimPlaceSuffix(a), trimPlaceSuffix(b)
	if a == "" || b == "" {
		return true
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func trimPlaceSuffix(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{"特别行政区", "维吾尔自治区", "壮族自治区", "回族自治区", "自治区", "自治州", "地区", "省", "市", "区", "县"} {
		if trimmed := strings.TrimSuffix(name, suffix); trimmed != name && trimmed != "" {
			return trimmed
		}
	}
	return name
}
