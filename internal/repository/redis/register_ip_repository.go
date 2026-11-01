package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// RegisterIPRepository 注册 IP 控制在 Redis 上的计数状态。规则见 docs/register-ip.md。
//
// 一个应用的全部键都带同一个哈希标签 {regip:<appid>}，集群部署时落在同一个槽，脚本才能一次操作多个键。
//
//	w:<rule>:<subject>   滑动窗口日志（ZSET，成员是占位令牌，分值是毫秒时间）——上限不大时用，精确
//	b:<rule>:<subject>   滑动窗口计数（HASH，60 个子桶）——上限很大（整个应用每天几万）时用，
//	                     误差不超过窗口的 1/60，内存与上限无关
//	n:<rule>:<subject>   累计计数（STRING），缺失时由服务端按注册台账回填
//	p:<rule>:<subject>   累计规则的待确认占位（ZSET）：注册还没写完时先占着，防止并发注册一起钻过上限
//	x:<rule>:<subject>   去重计数（ZSET，成员是设备或地址，分值是最近一次出现的毫秒）——
//	                     地址下的不同设备、设备下的不同地址；同一成员再来只刷新时间，不多算
//	pen:<subject>        惩罚期（STRING，带过期），值是第几次进惩罚期
//	lvl:<subject>        惩罚等级（STRING），比惩罚期活得久，再犯时从这里接着翻倍
//	stk:<subject>        惩罚计次（ZSET）
//	penidx               惩罚期索引（ZSET，分值是到期毫秒），管理端列出与解除用
//	m:<unix 分钟>         每分钟注册数（STRING），突增检测的分钟桶
type RegisterIPRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewRegisterIPRepository 构造仓库。
func NewRegisterIPRepository(client *redislib.Client, keyPrefix string) *RegisterIPRepository {
	return &RegisterIPRepository{client: client, keyPrefix: keyPrefix}
}

// 计数方式。
const (
	RegisterIPCounterLog      = "w" // 滑动窗口日志
	RegisterIPCounterBucketed = "b" // 滑动窗口子桶计数
	RegisterIPCounterLifetime = "l" // 累计
	RegisterIPCounterDistinct = "x" // 窗口内不同成员数
)

// RegisterIPBucketedThreshold 上限超过这个数的窗口规则改用子桶计数。
const RegisterIPBucketedThreshold = 2000

// registerIPPendingTTL 累计规则的占位多久没确认就作废（注册中途进程崩溃的兜底）。
const registerIPPendingTTL = 5 * time.Minute

// registerIPLifetimeTTL 累计计数的缓存寿命；过期后按台账重新回填，每次确认都会续期。
const registerIPLifetimeTTL = 30 * 24 * time.Hour

// RegisterIPCounter 一条规则在这次判定里的计数器。
type RegisterIPCounter struct {
	Kind     string
	Rule     string
	Subject  string
	WindowMs int64
	Limit    int
	// Blocking 超限会挡住这次注册：有任何一个挡住的超限，脚本就不占位
	Blocking bool
	// Member 去重计数这一次加入的成员（设备标识或地址）
	Member string
	// Added 这次占位是否新加了成员（Reserve 之后填）；撤销时只删自己新加的，不碰别人早就在的
	Added bool
}

// RegisterIPReserveResult 计数脚本的结果。
type RegisterIPReserveResult struct {
	// NeedSeed 缺回填的累计计数器下标（非空时其余字段无意义）
	NeedSeed []int
	// PenaltyMs 地址惩罚期剩余毫秒，0 表示不在惩罚期
	PenaltyMs int64
	// DevicePenaltyMs 设备惩罚期剩余毫秒
	DevicePenaltyMs int64
	Reserved        bool
	// Counts 与传入的计数器一一对应：不含这一次的已有数量
	Counts []int
	// Added 去重计数这次是否新加了成员，与 Counts 对齐
	Added []bool
	// At 脚本用的 Redis 服务端时间（毫秒）。子桶计数撤销占位时要用同一个时刻定位子桶
	At int64
}

func (r *RegisterIPRepository) tag(appID int64) string {
	return fmt.Sprintf("%s{regip:%d}:", r.keyPrefix, appID)
}

func (r *RegisterIPRepository) counterKeys(appID int64, c RegisterIPCounter) []string {
	base := r.tag(appID)
	switch c.Kind {
	case RegisterIPCounterLifetime:
		return []string{base + "n:" + c.Rule + ":" + c.Subject, base + "p:" + c.Rule + ":" + c.Subject}
	case RegisterIPCounterBucketed:
		return []string{base + "b:" + c.Rule + ":" + c.Subject}
	case RegisterIPCounterDistinct:
		return []string{base + "x:" + c.Rule + ":" + c.Subject}
	}
	return []string{base + "w:" + c.Rule + ":" + c.Subject}
}

// PenaltyKey 惩罚期的键（主体是地址或网段的 CIDR 文本）。
func (r *RegisterIPRepository) penaltyKey(appID int64, subject string) string {
	return r.tag(appID) + "pen:" + subject
}

// registerIPReserveScript 一次数完全部规则，都没有「挡住的超限」才占位。数与占位在同一个脚本里，
// 并发的注册不可能都看到「还差一个」然后一起钻过去。
//
// KEYS[1] 地址惩罚期键，KEYS[2] 设备惩罚期键，之后按计数器顺序：日志 / 子桶 / 去重各 1 个键，累计 2 个键（计数、占位）。
// ARGV: token, reserve(0/1), penaltyBlocks(0/1), pendingTTLms, n, 然后每个计数器 kind, windowMs, limit, blocking, member。
// 返回 {-2, 缺回填的下标…} 或 {地址惩罚剩余毫秒, 设备惩罚剩余毫秒, 是否占位, 服务端毫秒, 计数…, 新增成员标记…}。
//
// 去重计数的「已有数量」不含这次的成员自己：地址下已有 A、B、C 三台设备，A 再来注册时数到的是 2，
// 上限 3 照样放行 —— 老设备不是新设备。
var registerIPReserveScript = redislib.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local token = ARGV[1]
local reserve = ARGV[2] == '1'
local penaltyBlocks = ARGV[3] == '1'
local pendingTTL = tonumber(ARGV[4])
local n = tonumber(ARGV[5])
local W = 5

local missing = {}
local ki = 3
for i = 1, n do
  local kind = ARGV[5 + (i - 1) * W + 1]
  if kind == 'l' then
    if redis.call('EXISTS', KEYS[ki]) == 0 then table.insert(missing, i - 1) end
    ki = ki + 2
  else
    ki = ki + 1
  end
end
if #missing > 0 then
  local out = {-2}
  for _, v in ipairs(missing) do table.insert(out, v) end
  return out
end

local pIP = redis.call('PTTL', KEYS[1])
if pIP < 0 then pIP = 0 end
local pDev = redis.call('PTTL', KEYS[2])
if pDev < 0 then pDev = 0 end
local blocked = penaltyBlocks and (pIP > 0 or pDev > 0)

local counts = {}
local fresh = {}
ki = 3
for i = 1, n do
  local base = 5 + (i - 1) * W
  local kind = ARGV[base + 1]
  local window = tonumber(ARGV[base + 2])
  local limit = tonumber(ARGV[base + 3])
  local blocking = ARGV[base + 4] == '1'
  local member = ARGV[base + 5]
  local c = 0
  fresh[i] = 0
  if kind == 'w' then
    redis.call('ZREMRANGEBYSCORE', KEYS[ki], '-inf', now - window)
    c = redis.call('ZCARD', KEYS[ki])
    ki = ki + 1
  elseif kind == 'b' then
    local bucket = math.max(1000, math.floor(window / 60))
    local current = math.floor(now / bucket)
    local oldest = current - math.floor(window / bucket) + 1
    local fields = redis.call('HGETALL', KEYS[ki])
    for j = 1, #fields, 2 do
      local idx = tonumber(fields[j])
      if idx < oldest then
        redis.call('HDEL', KEYS[ki], fields[j])
      else
        c = c + tonumber(fields[j + 1])
      end
    end
    ki = ki + 1
  elseif kind == 'x' then
    redis.call('ZREMRANGEBYSCORE', KEYS[ki], '-inf', now - window)
    c = redis.call('ZCARD', KEYS[ki])
    if redis.call('ZSCORE', KEYS[ki], member) then
      c = c - 1
    else
      fresh[i] = 1
    end
    ki = ki + 1
  else
    redis.call('ZREMRANGEBYSCORE', KEYS[ki + 1], '-inf', now - pendingTTL)
    c = tonumber(redis.call('GET', KEYS[ki]) or '0') + redis.call('ZCARD', KEYS[ki + 1])
    ki = ki + 2
  end
  counts[i] = c
  if blocking and c >= limit then blocked = true end
end

local reserved = 0
if reserve and not blocked then
  ki = 3
  for i = 1, n do
    local base = 5 + (i - 1) * W
    local kind = ARGV[base + 1]
    local window = tonumber(ARGV[base + 2])
    if kind == 'w' then
      redis.call('ZADD', KEYS[ki], now, token)
      redis.call('PEXPIRE', KEYS[ki], window + 60000)
      ki = ki + 1
    elseif kind == 'b' then
      local bucket = math.max(1000, math.floor(window / 60))
      redis.call('HINCRBY', KEYS[ki], tostring(math.floor(now / bucket)), 1)
      redis.call('PEXPIRE', KEYS[ki], window + bucket + 60000)
      ki = ki + 1
    elseif kind == 'x' then
      redis.call('ZADD', KEYS[ki], now, ARGV[base + 5])
      redis.call('PEXPIRE', KEYS[ki], window + 60000)
      ki = ki + 1
    else
      redis.call('ZADD', KEYS[ki + 1], now, token)
      redis.call('PEXPIRE', KEYS[ki + 1], pendingTTL * 2)
      ki = ki + 2
    end
  end
  reserved = 1
end

local out = {pIP, pDev, reserved, now}
for i = 1, n do table.insert(out, counts[i]) end
for i = 1, n do table.insert(out, fresh[i]) end
return out`)

// Reserve 数一遍并（在 reserve 为真且没有挡住的超限时）占位。penaltySubjects 是地址与设备两侧的惩罚主体，缺哪边传空串。
func (r *RegisterIPRepository) Reserve(ctx context.Context, appID int64, ipSubject, deviceSubject, token string, reserve, penaltyBlocks bool, counters []RegisterIPCounter) (*RegisterIPReserveResult, error) {
	keys := []string{r.penaltyKey(appID, orNone(ipSubject)), r.penaltyKey(appID, orNone(deviceSubject))}
	args := []any{token, boolArg(reserve), boolArg(penaltyBlocks), registerIPPendingTTL.Milliseconds(), len(counters)}
	for _, c := range counters {
		keys = append(keys, r.counterKeys(appID, c)...)
		args = append(args, c.Kind, c.WindowMs, c.Limit, boolArg(c.Blocking), c.Member)
	}
	raw, err := registerIPReserveScript.Run(ctx, r.client, keys, args...).Int64Slice()
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 && raw[0] == -2 {
		out := &RegisterIPReserveResult{}
		for _, v := range raw[1:] {
			out.NeedSeed = append(out.NeedSeed, int(v))
		}
		return out, nil
	}
	n := len(counters)
	if len(raw) != 4+2*n {
		return nil, fmt.Errorf("register ip reserve: unexpected reply length %d", len(raw))
	}
	out := &RegisterIPReserveResult{PenaltyMs: raw[0], DevicePenaltyMs: raw[1], Reserved: raw[2] == 1, At: raw[3], Counts: make([]int, n), Added: make([]bool, n)}
	for i := 0; i < n; i++ {
		out.Counts[i] = int(raw[4+i])
		out.Added[i] = raw[4+n+i] == 1
	}
	return out, nil
}

// orNone 缺失的惩罚主体用一个永远不存在的键占位，脚本的 KEYS 布局保持固定。
func orNone(subject string) string {
	if subject == "" {
		return "-"
	}
	return subject
}

// SeedLifetime 回填累计计数（只在缺失时写）。
func (r *RegisterIPRepository) SeedLifetime(ctx context.Context, appID int64, c RegisterIPCounter, count int) error {
	keys := r.counterKeys(appID, c)
	return r.client.SetNX(ctx, keys[0], count, registerIPLifetimeTTL).Err()
}

// registerIPCommitScript 注册写完：累计规则的占位转正（计数存在才加，缺失时下次按台账回填，台账里已有这一笔），
// 分钟桶加一。窗口规则的占位本身就是计数，不用动。
// KEYS: 分钟桶，之后每个累计计数器 2 个键；ARGV: token, 分钟桶 TTL 秒, 累计 TTL 秒。
var registerIPCommitScript = redislib.NewScript(`
redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
for i = 2, #KEYS, 2 do
  redis.call('ZREM', KEYS[i + 1], ARGV[1])
  if redis.call('EXISTS', KEYS[i]) == 1 then
    redis.call('INCR', KEYS[i])
    redis.call('EXPIRE', KEYS[i], tonumber(ARGV[3]))
  end
end
return 1`)

// Commit 确认占位。surgeTTL 是分钟桶要保留多久。
func (r *RegisterIPRepository) Commit(ctx context.Context, appID int64, token string, at time.Time, surgeTTL time.Duration, counters []RegisterIPCounter) error {
	keys := []string{r.minuteKey(appID, at.Unix()/60)}
	for _, c := range counters {
		if c.Kind == RegisterIPCounterLifetime {
			keys = append(keys, r.counterKeys(appID, c)...)
		}
	}
	return registerIPCommitScript.Run(ctx, r.client, keys, token, int64(surgeTTL.Seconds()), int64(registerIPLifetimeTTL.Seconds())).Err()
}

// registerIPRollbackScript 注册没成功：撤销占位，名额还回去。
// ARGV: token, 占位时刻（毫秒）, 然后每个计数器 kind, windowMs, member, added。
// 去重计数只删这次新加的成员：成员原本就在（老设备再来）时，撤销不能把它从集合里抹掉。
var registerIPRollbackScript = redislib.NewScript(`
local token = ARGV[1]
local at = tonumber(ARGV[2])
local ki = 1
local argi = 3
while ki <= #KEYS do
  local kind = ARGV[argi]
  local window = tonumber(ARGV[argi + 1])
  local member = ARGV[argi + 2]
  local added = ARGV[argi + 3] == '1'
  argi = argi + 4
  if kind == 'w' then
    redis.call('ZREM', KEYS[ki], token)
    ki = ki + 1
  elseif kind == 'b' then
    local bucket = math.max(1000, math.floor(window / 60))
    local field = tostring(math.floor(at / bucket))
    local v = tonumber(redis.call('HGET', KEYS[ki], field) or '0')
    if v > 1 then
      redis.call('HINCRBY', KEYS[ki], field, -1)
    elseif v == 1 then
      redis.call('HDEL', KEYS[ki], field)
    end
    ki = ki + 1
  elseif kind == 'x' then
    if added then redis.call('ZREM', KEYS[ki], member) end
    ki = ki + 1
  else
    redis.call('ZREM', KEYS[ki + 1], token)
    ki = ki + 2
  end
end
return 1`)

// Rollback 撤销占位。
func (r *RegisterIPRepository) Rollback(ctx context.Context, appID int64, token string, atMs int64, counters []RegisterIPCounter) error {
	keys := []string{}
	args := []any{token, atMs}
	for _, c := range counters {
		keys = append(keys, r.counterKeys(appID, c)...)
		args = append(args, c.Kind, c.WindowMs, c.Member, boolArg(c.Added))
	}
	if len(keys) == 0 {
		return nil
	}
	return registerIPRollbackScript.Run(ctx, r.client, keys, args...).Err()
}

// registerIPStrikeScript 记一次配额拒绝。窗口内次数达到阈值时：等级加一、清空计次，返回新等级；否则返回 0。
// KEYS: stk, lvl；ARGV: token, windowMs, strikes, 等级保留秒数。
var registerIPStrikeScript = redislib.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local window = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
redis.call('ZADD', KEYS[1], now, ARGV[1])
redis.call('PEXPIRE', KEYS[1], window)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[3]) then
  redis.call('DEL', KEYS[1])
  local level = redis.call('INCR', KEYS[2])
  redis.call('EXPIRE', KEYS[2], tonumber(ARGV[4]))
  return level
end
return 0`)

// Strike 记一次配额拒绝，达到阈值时返回新的惩罚等级（从 1 起），否则 0。
func (r *RegisterIPRepository) Strike(ctx context.Context, appID int64, subject, token string, window time.Duration, strikes int, levelTTL time.Duration) (int, error) {
	base := r.tag(appID)
	level, err := registerIPStrikeScript.Run(ctx, r.client, []string{base + "stk:" + subject, base + "lvl:" + subject},
		token, window.Milliseconds(), strikes, int64(levelTTL.Seconds())).Int()
	return level, err
}

// Penalize 让主体进入惩罚期。
func (r *RegisterIPRepository) Penalize(ctx context.Context, appID int64, subject string, level int, duration time.Duration) error {
	until := time.Now().Add(duration)
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, r.penaltyKey(appID, subject), level, duration)
	pipe.ZAdd(ctx, r.tag(appID)+"penidx", redislib.Z{Score: float64(until.UnixMilli()), Member: subject})
	_, err := pipe.Exec(ctx)
	return err
}

// RegisterIPPenalty 一个惩罚期。
type RegisterIPPenalty struct {
	Subject   string
	Level     int
	ExpiresAt time.Time
}

// Penalties 正在惩罚期的主体，最晚到期的在前。顺手清掉索引里已到期的。
func (r *RegisterIPRepository) Penalties(ctx context.Context, appID int64, limit int) ([]RegisterIPPenalty, int64, error) {
	index := r.tag(appID) + "penidx"
	now := time.Now().UnixMilli()
	if err := r.client.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(now, 10)).Err(); err != nil {
		return nil, 0, err
	}
	total, err := r.client.ZCard(ctx, index).Result()
	if err != nil {
		return nil, 0, err
	}
	items, err := r.client.ZRevRangeWithScores(ctx, index, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		return []RegisterIPPenalty{}, total, nil
	}
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = r.penaltyKey(appID, fmt.Sprint(item.Member))
	}
	levels, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, 0, err
	}
	out := make([]RegisterIPPenalty, 0, len(items))
	for i, item := range items {
		if levels[i] == nil {
			continue // 被解除了，索引还没来得及清
		}
		level, _ := strconv.Atoi(fmt.Sprint(levels[i]))
		out = append(out, RegisterIPPenalty{Subject: fmt.Sprint(item.Member), Level: level, ExpiresAt: time.UnixMilli(int64(item.Score))})
	}
	return out, total, nil
}

// PenaltyOf 主体的惩罚期剩余时长与等级；不在惩罚期时 remaining 为 0。
func (r *RegisterIPRepository) PenaltyOf(ctx context.Context, appID int64, subject string) (time.Duration, int, error) {
	base := r.tag(appID)
	pipe := r.client.Pipeline()
	ttl := pipe.PTTL(ctx, r.penaltyKey(appID, subject))
	level := pipe.Get(ctx, base+"lvl:"+subject)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redislib.Nil) {
		return 0, 0, err
	}
	remaining := ttl.Val()
	if remaining < 0 {
		remaining = 0
	}
	lvl, _ := strconv.Atoi(level.Val())
	return remaining, lvl, nil
}

// ClearPenalty 解除惩罚期；resetLevel 时连等级与计次一起清，下次从首次时长算起。
func (r *RegisterIPRepository) ClearPenalty(ctx context.Context, appID int64, subject string, resetLevel bool) error {
	base := r.tag(appID)
	keys := []string{r.penaltyKey(appID, subject)}
	if resetLevel {
		keys = append(keys, base+"lvl:"+subject, base+"stk:"+subject)
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, keys...)
	pipe.ZRem(ctx, base+"penidx", subject)
	_, err := pipe.Exec(ctx)
	return err
}

// ClearCounters 清空主体在给定规则上的计数（窗口与累计都清，累计会按台账重新回填）。
func (r *RegisterIPRepository) ClearCounters(ctx context.Context, appID int64, counters []RegisterIPCounter) error {
	keys := []string{}
	for _, c := range counters {
		base := r.tag(appID)
		// 同一主体的三种形态都删：规则改过上限后计数方式可能变了
		keys = append(keys, base+"w:"+c.Rule+":"+c.Subject, base+"b:"+c.Rule+":"+c.Subject,
			base+"n:"+c.Rule+":"+c.Subject, base+"p:"+c.Rule+":"+c.Subject, base+"x:"+c.Rule+":"+c.Subject)
	}
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}

// WindowOldest 滑动窗口日志里最早一笔的时间（算「什么时候能再注册」），没有返回零值。
func (r *RegisterIPRepository) WindowOldest(ctx context.Context, appID int64, c RegisterIPCounter) (time.Time, error) {
	if c.Kind != RegisterIPCounterLog {
		return time.Time{}, nil
	}
	items, err := r.client.ZRangeWithScores(ctx, r.counterKeys(appID, c)[0], 0, 0).Result()
	if err != nil || len(items) == 0 {
		return time.Time{}, err
	}
	return time.UnixMilli(int64(items[0].Score)), nil
}

func (r *RegisterIPRepository) minuteKey(appID int64, minute int64) string {
	return r.tag(appID) + "m:" + strconv.FormatInt(minute, 10)
}

// MinuteSeries 最近 n 个分钟桶，最老的在前、最后一个是当前分钟。
func (r *RegisterIPRepository) MinuteSeries(ctx context.Context, appID int64, now time.Time, n int) ([]int, error) {
	if n <= 0 {
		return []int{}, nil
	}
	current := now.Unix() / 60
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = r.minuteKey(appID, current-int64(n-1-i))
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]int, n)
	for i, value := range values {
		if value == nil {
			continue
		}
		out[i], _ = strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	}
	return out, nil
}

func boolArg(v bool) int {
	if v {
		return 1
	}
	return 0
}

// DistinctMembers 去重计数此刻的成员（最近出现的在前），IP 查询 / 设备查询里展示「这个地址最近出现了哪些设备」。
func (r *RegisterIPRepository) DistinctMembers(ctx context.Context, appID int64, c RegisterIPCounter, limit int) ([]redislib.Z, error) {
	if c.Kind != RegisterIPCounterDistinct {
		return nil, nil
	}
	key := r.counterKeys(appID, c)[0]
	if c.WindowMs > 0 {
		if err := r.client.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(time.Now().UnixMilli()-c.WindowMs, 10)).Err(); err != nil {
			return nil, err
		}
	}
	return r.client.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
}
