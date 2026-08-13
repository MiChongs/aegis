package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	redisrepo "aegis/internal/repository/redis"
	"aegis/pkg/response"

	"github.com/bsm/redislock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// ─────────────────────────────────────────────────────────────────────────
// 幂等键（Idempotency-Key）
//
// 这一层要解决的是旧实现最根本的问题：**它把重复请求当成攻击来处理。**
//
// 旧行为是回 403「重复请求」。但对调用方来说，一次超时重试拿到 403 与
// 拿不到任何回应没有区别 —— 他仍然不知道第一次到底成没成功，
// 于是只能再试，或者去人工核对。真正有用的回答是把**第一次的那个响应**
// 原样还给他：状态码、响应体，一字不差。他因此能像第一次成功那样继续往下走。
//
// 这套语义就是 Stripe / Square 用了十年的那一套，也是 IETF
// draft-ietf-httpapi-idempotency-key-header 正在标准化的东西：
//
//	首次请求        执行，并把响应存下来
//	同键同内容重放  回放存下来的响应，带 Idempotent-Replayed: true
//	同键不同内容    422，明说这个键已经用于另一份请求体
//	正在执行中      409 + Retry-After，让调用方稍后再问
//
// 三层并发控制，从便宜到贵：
//
//  1. singleflight —— 同一进程内的并发同键请求直接合流，连 Redis 都不碰。
//     双击按钮在同一台实例上落地时，这一层就挡住了。
//  2. redislock —— 跨实例的执行中标记。用它而不是裸 SetNX，是因为释放锁
//     必须校验持有者：裸 SetNX + DEL 会在超时后删掉**别人**的锁，
//     那正是分布式锁最经典的一个错误。
//  3. Redis 记录 —— 已完成的响应快照，带 TTL。
// ─────────────────────────────────────────────────────────────────────────

const (
	// HeaderIdempotencyKey 客户端提供的幂等键，IETF 草案定的就是这个名字
	HeaderIdempotencyKey = "Idempotency-Key"
	// HeaderIdempotentReplayed 标记这次响应是回放出来的，不是重新执行的结果
	HeaderIdempotentReplayed = "Idempotent-Replayed"

	idempotencyKeyMinLen = 8
	idempotencyKeyMaxLen = 255

	// 能被缓存回放的响应体上限。超过这个尺寸的响应不缓存，
	// 只保留「已完成」的事实，重放时回 409 让调用方自己去查询结果。
	idempotencyBodyLimit = 256 << 10 // 256 KiB
)

// IdempotencyManager 幂等键的执行与回放
type IdempotencyManager struct {
	repo    *redisrepo.ReplayRepository
	locker  *redislock.Client
	group   singleflight.Group
	ttl     time.Duration
	lockTTL time.Duration
	log     *zap.Logger
}

// NewIdempotencyManager 创建幂等管理器。repo 为空时返回 nil，调用方按"未启用"处理。
func NewIdempotencyManager(repo *redisrepo.ReplayRepository, ttl, lockTTL time.Duration, log *zap.Logger) *IdempotencyManager {
	if repo == nil || repo.Client() == nil {
		return nil
	}
	if log == nil {
		log = zap.NewNop()
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if lockTTL <= 0 {
		lockTTL = 30 * time.Second
	}
	return &IdempotencyManager{
		repo:    repo,
		locker:  redislock.New(repo.Client()),
		ttl:     ttl,
		lockTTL: lockTTL,
		log:     log,
	}
}

// NormalizeIdempotencyKey 取出并校验幂等键。第二个返回值为 false 表示这次请求没带键。
func NormalizeIdempotencyKey(c *gin.Context) (string, bool, error) {
	raw := strings.TrimSpace(c.GetHeader(HeaderIdempotencyKey))
	if raw == "" {
		return "", false, nil
	}
	if len(raw) < idempotencyKeyMinLen || len(raw) > idempotencyKeyMaxLen {
		return "", true, errIdempotencyKeyInvalid
	}
	// 键会被拼进 Redis key，控制字符与冒号都不该出现在里面
	for _, r := range raw {
		if r < 0x21 || r > 0x7e || r == ':' {
			return "", true, errIdempotencyKeyInvalid
		}
	}
	return raw, true, nil
}

type idempotencyError string

func (e idempotencyError) Error() string { return string(e) }

const errIdempotencyKeyInvalid = idempotencyError("invalid idempotency key")

// Handle 按幂等语义处理这次请求。
//
// 返回 true 表示**已经写完响应**（回放或拒绝），调用方应当直接 Abort；
// 返回 false 表示这是首次执行，调用方继续走 c.Next()，
// 响应会在写出的同时被这里捕获并存档。
func (m *IdempotencyManager) Handle(c *gin.Context, key string, requestHash string) bool {
	if m == nil {
		return false
	}

	scope := idempotencyScope(c, key)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()

	// 已有完成记录 → 直接回放，不必碰锁
	if record, found, err := m.repo.LoadIdempotency(ctx, scope); err == nil && found {
		return m.replay(c, record, requestHash)
	} else if err != nil {
		// Redis 不可用时不阻断业务。幂等是"更好的重试体验"，
		// 不是安全边界；把它变成硬依赖会让 Redis 抖动升级成全站写操作不可用。
		m.log.Warn("idempotency: load failed, passing through", zap.Error(err))
		return false
	}

	// 同进程并发合流。
	//
	// singleflight 只用来选出"谁去真正执行"，**不能用它的返回值代表"响应已经写好"**：
	// 每个请求有各自的 ResponseWriter，领跑者写的是自己那一个。跟随者若只是拿到
	// 一个 handled=true 就从中间件返回，Gin 会继续往下走 —— 一个中间件不调用
	// c.Abort() 就等于放行。那正是这里踩过的坑：singleflight 明明只跑了一次，
	// handler 却执行了四遍。
	//
	// 因此跟随者必须自己产出响应：等领跑者结束后去读它存下的记录并回放。
	leader := false
	raw, _, _ := m.group.Do(scope, func() (any, error) {
		// 这个闭包只有领跑者的那一份会被执行，因此在这里置位是安全的
		leader = true

		lock, err := m.locker.Obtain(ctx, idempotencyLockKey(scope), m.lockTTL, nil)
		if errors.Is(err, redislock.ErrNotObtained) {
			// 另一台实例正在执行同一个键
			return flightOutcome{inFlight: true}, nil
		}
		if err != nil {
			m.log.Warn("idempotency: obtain lock failed, passing through", zap.Error(err))
			return flightOutcome{passthrough: true}, nil
		}

		// 拿到锁之后再查一次：上一个持有者可能刚刚写完记录并释放
		if _, found, loadErr := m.repo.LoadIdempotency(ctx, scope); loadErr == nil && found {
			_ = lock.Release(context.WithoutCancel(ctx))
			return flightOutcome{}, nil // 交给下面统一走回放
		}

		m.execute(c, scope, requestHash, lock)
		return flightOutcome{executed: true}, nil
	})
	outcome, _ := raw.(flightOutcome)

	// 领跑者：响应已经在 execute 里写完了
	if leader && outcome.executed {
		return true
	}
	// Redis 异常，退化为直通。幂等是"更好的重试体验"，不是安全边界
	if outcome.passthrough {
		return false
	}

	// 走到这里的有两类：跟随者，以及"分布式锁被别的实例持有"的领跑者。
	// 两类人要做的事一样 —— 看有没有已完成的记录，有就回放，没有说明还在跑。
	if record, found, err := m.repo.LoadIdempotency(ctx, scope); err == nil && found {
		return m.replay(c, record, requestHash)
	}
	return m.rejectInFlight(c)
}

// flightOutcome 领跑者闭包的结论。跟随者会收到同一份，但只读它的 passthrough。
type flightOutcome struct {
	executed    bool // 领跑者已在自己的上下文里写完响应
	inFlight    bool // 分布式锁被别的实例持有
	passthrough bool // Redis 异常，放行
}

// execute 首次执行：跑完 handler，把响应存档，最后释放锁
func (m *IdempotencyManager) execute(c *gin.Context, scope, requestHash string, lock *redislock.Lock) {
	recorder := &responseRecorder{ResponseWriter: c.Writer, limit: idempotencyBodyLimit}
	c.Writer = recorder

	defer func() {
		c.Writer = recorder.ResponseWriter
		// 释放用不带取消的 ctx：客户端断开会取消请求 ctx，
		// 那时锁还没释放，同一个键要等到 lockTTL 到期才能再被使用。
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 500*time.Millisecond)
		defer cancel()
		_ = lock.Release(releaseCtx)
	}()

	c.Next()

	status := recorder.Status()
	// 5xx 不存档：服务端自己出错时，重试**应该**真的重跑一遍，
	// 把一次 500 缓存 24 小时等于把一次偶发故障钉死成永久故障。
	if status >= http.StatusInternalServerError {
		return
	}

	record := redisrepo.IdempotencyRecord{
		Status:      status,
		RequestHash: requestHash,
		ContentType: recorder.Header().Get("Content-Type"),
		CompletedAt: time.Now().Unix(),
	}
	if !recorder.overflowed {
		record.Body = recorder.body.Bytes()
	}

	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 500*time.Millisecond)
	defer cancel()
	if err := m.repo.StoreIdempotency(storeCtx, scope, record, m.ttl); err != nil {
		m.log.Warn("idempotency: store failed", zap.Error(err), zap.String("scope", scope))
	}
}

// replay 回放首次的响应
func (m *IdempotencyManager) replay(c *gin.Context, record redisrepo.IdempotencyRecord, requestHash string) bool {
	// 同一个键配两份不同的请求体，几乎总是客户端复用键时出的 bug。
	// 静默回放旧结果会让"我明明改了参数却拿到上一次的结果"变成一个
	// 完全无从下手的问题，所以这里必须显式报错。
	if record.RequestHash != "" && requestHash != "" && record.RequestHash != requestHash {
		response.Error(c, http.StatusUnprocessableEntity, 40384,
			"该幂等键已用于另一份请求内容，请更换 Idempotency-Key")
		c.Abort()
		return true
	}

	// 响应体过大没被存下来：只能告知已完成，让调用方自己去查结果，
	// 不能编一个空响应回去。
	if len(record.Body) == 0 && record.Status != http.StatusNoContent {
		c.Header("Retry-After", "1")
		response.Error(c, http.StatusConflict, 40385,
			"该请求已处理完成，但响应体过大未被缓存，请改用查询接口获取结果")
		c.Abort()
		return true
	}

	c.Header(HeaderIdempotentReplayed, "true")
	c.Header("Cache-Control", "no-store")
	contentType := record.ContentType
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Data(record.Status, contentType, record.Body)
	c.Abort()
	return true
}

func (m *IdempotencyManager) rejectInFlight(c *gin.Context) bool {
	c.Header("Retry-After", "1")
	response.Error(c, http.StatusConflict, 40386,
		"相同幂等键的请求正在处理中，请稍后重试")
	c.Abort()
	return true
}

// idempotencyScope 把幂等键限定在「谁 + 哪个接口」之内。
//
// 只用键本身做作用域的话，两个不同的调用方碰巧用了同一个 UUID，
// 后来者会拿到前者的响应 —— 那是一次跨账号的数据泄露。
func idempotencyScope(c *gin.Context, key string) string {
	actor := c.GetHeader("Authorization")
	if actor == "" {
		actor = c.GetHeader("X-Admin-Token")
	}
	if actor == "" {
		actor = c.ClientIP()
	}
	return strings.Join([]string{
		sha256Hex([]byte(actor))[:16],
		strings.ToUpper(c.Request.Method),
		c.FullPath(),
		key,
	}, "|")
}

func idempotencyLockKey(scope string) string {
	return "idem-lock:" + sha256Hex([]byte(scope))
}

// ─────────────────────────────────────────────────────────────────────────
// 响应捕获
// ─────────────────────────────────────────────────────────────────────────

// responseRecorder 在写出响应的同时抄一份，供后续回放。
//
// 超过 limit 之后停止抄写并置 overflowed，但**不影响真实响应的写出** ——
// 缓存不下是缓存的问题，不该让这次请求的结果被截断。
type responseRecorder struct {
	gin.ResponseWriter
	body       bytes.Buffer
	limit      int
	overflowed bool
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	r.capture(data)
	return r.ResponseWriter.Write(data)
}

func (r *responseRecorder) WriteString(s string) (int, error) {
	r.capture([]byte(s))
	return r.ResponseWriter.WriteString(s)
}

func (r *responseRecorder) capture(data []byte) {
	if r.overflowed {
		return
	}
	if r.body.Len()+len(data) > r.limit {
		r.overflowed = true
		r.body.Reset()
		return
	}
	r.body.Write(data)
}

// canonicalRequestHash 请求内容指纹，用来识别"同一个键配了不同的请求体"
func canonicalRequestHash(method, path string, body []byte) string {
	var buf bytes.Buffer
	buf.WriteString(strings.ToUpper(method))
	buf.WriteByte('\n')
	buf.WriteString(path)
	buf.WriteByte('\n')
	buf.Write(body)
	return sha256Hex(buf.Bytes())
}