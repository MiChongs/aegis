package sources

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/internal/engine"

	"github.com/redis/go-redis/v9"
)

// 滑窗计数的存法（Redis Cluster，同一个计数的键用 {计数键} 哈希标签落在一个槽里）。
//
// 设计目标：解除风控、删除账号或设备时，计数只减掉它的那一份，别人的不动；并且不靠全集群扫描找键。
//
//	rc:{计数键}:<桶>   HASH  次数。字段 "#" 是本桶合计，其余字段是「贡献者组合 → 次数」
//	                          （组合 = 账号、登录名、设备三个标签用 \x1f 连起来，都没有时是 "-"）。
//	rd:{计数键}        ZSET  去重计数。成员 → 最近一次出现的秒数，过窗的随写入清掉；个数就是 ZCARD。精确，不是估算。
//	rd:{计数键}:w      ZSET  去重计数的出处。成员 + \x1e + 贡献者组合 → 最近一次的秒数。
//	                          同一个成员可能由几个人带来，删掉其中一个人以后要靠它判断这个成员还在不在。
//	ri:{标签}          ZSET  反向索引。这个对象参与过哪些计数：<角色><种类><窗口>:<计数键> → 到期秒数。
//	                          角色 S = 它是计数的主体，A = 它是贡献者，M = 它是去重成员；种类 c 次数 / d 去重。
//
// 旧格式 rv:{…}:<桶>（字符串）与 rvd:{…}:<桶>（HyperLogLog）不再写入：次数由 MigrateLegacy 并进新键；
// 去重的旧桶删不了单个成员，切换后一个窗口内只读来兜底（取新旧较大者），之后自然过期。
const (
	countPrefix       = "rc:{"
	distinctPrefix    = "rd:{"
	indexPrefix       = "ri:{"
	legacyCountPrefix = "rv:{"
	legacyHLLPrefix   = "rvd:{"
	cutoverKey        = "rc:cutover"

	totalField = "#"
	anonymous  = "-"
	tupleSep   = "\x1f"
	memberSep  = "\x1e"

	roleSubject = 'S'
	roleActor   = 'A'
	roleMember  = 'M'
	kindCount   = 'c'
	kindSet     = 'd'
)

// 次数：本桶合计与贡献者各加一，再把窗口内各桶的合计加起来（本次算在内）。
// KEYS[1] 当前桶，KEYS[2..] 窗口内其余桶。ARGV[1] 桶的存活秒数，ARGV[2] 贡献者组合。
var countAdd = redis.NewScript(`
redis.call('HINCRBY', KEYS[1], '#', 1)
redis.call('HINCRBY', KEYS[1], ARGV[2], 1)
redis.call('EXPIRE', KEYS[1], ARGV[1])
local sum = 0
for i = 1, #KEYS do
  local v = redis.call('HGET', KEYS[i], '#')
  if v then sum = sum + tonumber(v) end
end
return sum
`)

// 去重：记下成员与出处，清掉过窗的，返回窗口内的成员数。
// KEYS[1] 成员集合，KEYS[2] 出处集合，KEYS[3..] 旧格式的桶（只在切换后的一个窗口内传）。
// ARGV：现在秒数、窗口起点秒数、存活秒数、成员、贡献者组合。
var distinctAdd = redis.NewScript(`
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[4])
redis.call('ZADD', KEYS[2], ARGV[1], ARGV[4] .. '\30' .. ARGV[5])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
redis.call('EXPIRE', KEYS[2], ARGV[3])
local n = redis.call('ZCARD', KEYS[1])
if #KEYS > 2 then
  local legacy = {}
  for i = 3, #KEYS do legacy[#legacy + 1] = KEYS[i] end
  local ok, old = pcall(redis.call, 'PFCOUNT', unpack(legacy))
  if ok and type(old) == 'number' and old > n then n = old end
end
return n
`)

// 只读次数：窗口内各桶合计之和。KEYS 窗口内全部桶。
var countRead = redis.NewScript(`
local sum = 0
for i = 1, #KEYS do
  local v = redis.call('HGET', KEYS[i], '#')
  if v then sum = sum + tonumber(v) end
end
return sum
`)

// 只读去重：窗口内的成员数（不写入、不清理）。KEYS[1] 成员集合，KEYS[2..] 旧格式的桶。ARGV[1] 窗口起点秒数。
var distinctRead = redis.NewScript(`
local n = redis.call('ZCOUNT', KEYS[1], '(' .. ARGV[1], '+inf')
if #KEYS > 1 then
  local legacy = {}
  for i = 2, #KEYS do legacy[#legacy + 1] = KEYS[i] end
  local ok, old = pcall(redis.call, 'PFCOUNT', unpack(legacy))
  if ok and type(old) == 'number' and old > n then n = old end
end
return n
`)

// 反向索引：登记这个对象参与的计数，顺手清掉已过期的登记，键的寿命跟最晚到期的一条走。
// KEYS[1] 索引键。ARGV[1] 现在秒数，其后成对的（到期秒数, 登记项）。
var indexAdd = redis.NewScript(`
for i = 2, #ARGV, 2 do
  redis.call('ZADD', KEYS[1], 'GT', ARGV[i], ARGV[i + 1])
end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', '(' .. ARGV[1])
local top = redis.call('ZRANGE', KEYS[1], -1, -1, 'WITHSCORES')
if top[2] then redis.call('EXPIREAT', KEYS[1], math.ceil(tonumber(top[2])) + 60) end
return 1
`)

// 次数回退：各桶里凡是贡献者组合含有给定标签的字段，删掉并从合计里减去。
// KEYS 窗口内全部桶。ARGV 要回退的标签。返回减掉的次数。
var countForget = redis.NewScript(`
local tags = {}
for _, t in ipairs(ARGV) do tags[t] = true end
local removed = 0
for _, k in ipairs(KEYS) do
  local flat = redis.call('HGETALL', k)
  local sub = 0
  for i = 1, #flat, 2 do
    local f = flat[i]
    if f ~= '#' then
      for part in string.gmatch(f, '[^\31]+') do
        if tags[part] then
          sub = sub + tonumber(flat[i + 1])
          redis.call('HDEL', k, f)
          break
        end
      end
    end
  end
  if sub > 0 then
    local left = redis.call('HINCRBY', k, '#', -sub)
    if left <= 0 then redis.call('DEL', k) end
    removed = removed + sub
  end
end
return removed
`)

// 去重回退：删掉给定贡献者带来的出处、以及给定成员的全部出处；受影响的成员若还有别人带来的出处，
// 时间改成剩下出处里最近的一次，否则从成员集合里删掉。
// KEYS[1] 成员集合，KEYS[2] 出处集合。ARGV[1] 标签个数，其后是标签，再其后是要删的成员。返回删掉的成员数。
var distinctForget = redis.NewScript(`
local nt = tonumber(ARGV[1])
local tags, members = {}, {}
for i = 2, 1 + nt do tags[ARGV[i]] = true end
for i = 2 + nt, #ARGV do members[ARGV[i]] = true end
local touched, best = {}, {}
local all = redis.call('ZRANGE', KEYS[2], 0, -1, 'WITHSCORES')
for i = 1, #all, 2 do
  local e, score = all[i], tonumber(all[i + 1])
  local sep = string.find(e, '\30', 1, true)
  if sep then
    local m, tuple = string.sub(e, 1, sep - 1), string.sub(e, sep + 1)
    local hit = members[m]
    if not hit then
      for part in string.gmatch(tuple, '[^\31]+') do
        if tags[part] then hit = true break end
      end
    end
    if hit then
      redis.call('ZREM', KEYS[2], e)
      touched[m] = true
    elseif not best[m] or score > best[m] then
      best[m] = score
    end
  end
end
local removed = 0
for m in pairs(members) do touched[m] = true end
for m in pairs(touched) do
  if best[m] and not members[m] then
    redis.call('ZADD', KEYS[1], best[m], m)
  else
    removed = removed + redis.call('ZREM', KEYS[1], m)
  end
end
return removed
`)

// 旧次数桶并进新键：数值记在「无法归属」名下，寿命沿用旧键剩下的时间。KEYS[1] 旧键，KEYS[2] 新键。
var legacyMerge = redis.NewScript(`
local ok, v = pcall(redis.call, 'GET', KEYS[1])
if not ok or not v or not tonumber(v) then return 0 end
local ttl = redis.call('TTL', KEYS[1])
redis.call('HINCRBY', KEYS[2], '#', v)
redis.call('HINCRBY', KEYS[2], '-', v)
if ttl > 0 and redis.call('TTL', KEYS[2]) < ttl then redis.call('EXPIRE', KEYS[2], ttl) end
redis.call('DEL', KEYS[1])
return 1
`)

// clean 标签与成员里不许出现分隔符
func clean(s string) string {
	if strings.ContainsAny(s, tupleSep+memberSep) {
		s = strings.NewReplacer(tupleSep, "", memberSep, "").Replace(s)
	}
	return s
}

func actorTuple(actors []string) string {
	if len(actors) == 0 {
		return anonymous
	}
	parts := make([]string, 0, len(actors))
	for _, a := range actors {
		if a = clean(a); a != "" {
			parts = append(parts, a)
		}
	}
	if len(parts) == 0 {
		return anonymous
	}
	return strings.Join(parts, tupleSep)
}

func indexEntry(role, kind byte, window int, base string) string {
	return string([]byte{role, kind}) + strconv.Itoa(window) + ":" + base
}

// counterRef 索引登记项去掉角色后的部分：哪一个计数
type counterRef struct {
	kind   byte
	window int
	base   string
}

func parseEntry(e string) (role byte, ref counterRef, ok bool) {
	if len(e) < 4 {
		return 0, ref, false
	}
	i := strings.IndexByte(e, ':')
	if i < 3 {
		return 0, ref, false
	}
	w, err := strconv.Atoi(e[2:i])
	if err != nil || w <= 0 {
		return 0, ref, false
	}
	return e[0], counterRef{kind: e[1], window: w, base: e[i+1:]}, true
}

// tagValue 标签对应的取值（去重成员按取值存）：u:<账号>、a:<应用>:<登录名>、d:/i:/n:/p:<取值>
func tagValue(tag string) string {
	if len(tag) < 3 {
		return ""
	}
	v := tag[2:]
	if tag[0] == 'a' {
		if i := strings.IndexByte(v, ':'); i >= 0 {
			return v[i+1:]
		}
	}
	return v
}

// allBuckets 一个计数可能存在的全部桶（窗口内的，加上前后各一个容忍时钟偏差）
func allBuckets(prefix, base string, window int, now time.Time) []string {
	width := bucketWidth(window)
	cur := now.Unix()/width + 1
	n := (int64(window)+width-1)/width + 2
	keys := make([]string, 0, n)
	for i := int64(0); i < n; i++ {
		keys = append(keys, prefix+base+"}:"+strconv.FormatInt(cur-i, 10))
	}
	return keys
}

// ForgetResult 回退了多少
type ForgetResult struct {
	Counters int   `json:"counters"` // 涉及的计数个数
	Dropped  int   `json:"dropped"`  // 其中整个删掉的（主体本身被解除或删除）
	Removed  int64 `json:"removed"`  // 减掉的次数与去重成员数
}

// Forget 把一批对象从滑窗计数里拿掉。
//
// subjects 是被解除或删除的对象本身（账号、登录名、只属于它的设备、从名单解除的地址……）：
// 以它们为主体的计数整个删掉。actors 是只需要撤回贡献的对象。两者的贡献都会从别的计数里减掉
// （例如同一个地址上的注册次数、同一台设备上的账号数），别人的贡献不动。
// 标签用 engine.CounterTag 生成。
func (r *Redis) Forget(ctx context.Context, subjects, actors []string) (ForgetResult, error) {
	var res ForgetResult
	isSubject := map[string]bool{}
	var tags []string
	seen := map[string]bool{}
	for _, t := range subjects {
		if t = clean(t); t != "" && !seen[t] {
			seen[t], isSubject[t] = true, true
			tags = append(tags, t)
		}
	}
	for _, t := range actors {
		if t = clean(t); t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		return res, nil
	}
	sort.Strings(tags)

	type job struct {
		drop    bool
		tags    map[string]bool
		members map[string]bool
	}
	jobs := map[counterRef]*job{}
	var firstErr error
	keep := func(err error) {
		if err != nil && !errors.Is(err, redis.Nil) && firstErr == nil {
			firstErr = err
		}
	}
	for i := 0; i < len(tags); i += 200 {
		batch := tags[i:min(i+200, len(tags))]
		cmds := make([]*redis.StringSliceCmd, len(batch))
		_, err := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
			for j, t := range batch {
				cmds[j] = p.ZRange(ctx, indexPrefix+t+"}", 0, -1)
			}
			return nil
		})
		keep(err)
		for j, c := range cmds {
			tag := batch[j]
			for _, e := range c.Val() {
				role, ref, ok := parseEntry(e)
				if !ok {
					continue
				}
				jb := jobs[ref]
				if jb == nil {
					jb = &job{tags: map[string]bool{}, members: map[string]bool{}}
					jobs[ref] = jb
				}
				switch role {
				case roleSubject:
					if isSubject[tag] {
						jb.drop = true
					}
				case roleActor:
					jb.tags[tag] = true
				case roleMember:
					if v := tagValue(tag); v != "" {
						jb.members[v] = true
					}
				}
			}
		}
	}

	now := r.now()
	refs := make([]counterRef, 0, len(jobs))
	for ref := range jobs {
		refs = append(refs, ref)
	}
	for i := 0; i < len(refs); i += 100 {
		batch := refs[i:min(i+100, len(refs))]
		var counted []*redis.Cmd
		_, err := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
			for _, ref := range batch {
				jb := jobs[ref]
				res.Counters++
				switch ref.kind {
				case kindCount:
					buckets := allBuckets(countPrefix, ref.base, ref.window, now)
					legacy := allBuckets(legacyCountPrefix, ref.base, ref.window, now)
					if jb.drop {
						res.Dropped++
						p.Del(ctx, append(buckets, legacy...)...)
						continue
					}
					if len(jb.tags) > 0 {
						args := make([]any, 0, len(jb.tags))
						for t := range jb.tags {
							args = append(args, t)
						}
						counted = append(counted, countForget.Eval(ctx, p, buckets, args...))
					}
				case kindSet:
					set := distinctPrefix + ref.base + "}"
					// 旧格式的桶删不了单个成员：受影响的计数直接丢掉旧桶，改以新的精确集合为准
					legacy := allBuckets(legacyHLLPrefix, ref.base, ref.window, now)
					if jb.drop {
						res.Dropped++
						p.Del(ctx, append([]string{set, set + ":w"}, legacy...)...)
						continue
					}
					if len(jb.tags)+len(jb.members) > 0 {
						args := make([]any, 0, 1+len(jb.tags)+len(jb.members))
						args = append(args, len(jb.tags))
						for t := range jb.tags {
							args = append(args, t)
						}
						for m := range jb.members {
							args = append(args, m)
						}
						counted = append(counted, distinctForget.Eval(ctx, p, []string{set, set + ":w"}, args...))
						p.Del(ctx, legacy...)
					}
				}
			}
			return nil
		})
		keep(err)
		for _, c := range counted {
			if n, err := c.Int64(); err == nil {
				res.Removed += n
			}
		}
	}

	// 主体的索引整个删掉；只撤回贡献的对象，它自己作主体的计数还在，索引里只去掉贡献与成员两类登记
	_, err := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for _, t := range tags {
			if isSubject[t] {
				p.Del(ctx, indexPrefix+t+"}")
			}
		}
		return nil
	})
	keep(err)
	for _, t := range tags {
		if isSubject[t] {
			continue
		}
		key := indexPrefix + t + "}"
		entries, err := r.rdb.ZRange(ctx, key, 0, -1).Result()
		keep(err)
		var gone []any
		for _, e := range entries {
			if len(e) > 0 && e[0] != roleSubject {
				gone = append(gone, e)
			}
		}
		if len(gone) > 0 {
			keep(r.rdb.ZRem(ctx, key, gone...).Err())
		}
	}
	return res, firstErr
}

// legacySince 切换到新格式的时刻（秒）。第一个启动的进程写下，之后大家读同一个值；
// 去重计数在这之后的一个窗口内还要读旧桶兜底。
func (r *Redis) legacySince(ctx context.Context) int64 {
	r.cutoverOnce.Do(func() {
		now := r.now().Unix()
		r.cutover = now
		_ = r.rdb.SetNX(ctx, cutoverKey, now, 0).Err()
		if v, err := r.rdb.Get(ctx, cutoverKey).Int64(); err == nil && v > 0 {
			r.cutover = v
		}
	})
	return r.cutover
}

// LegacyFeature 旧计数键属于哪个特征：主体类型与窗口（用来给并进新键的计数补主体索引）
type LegacyFeature func(feature string) (entity string, window int, ok bool)

// MigrateLegacy 把旧格式的次数桶（rv:{…}:<桶>）并进新键，并给能认出主体的补上索引。可重复执行。
// 返回并入的桶数。
func (r *Redis) MigrateLegacy(ctx context.Context, lookup LegacyFeature) (int, error) {
	r.legacySince(ctx)
	var (
		mu   sync.Mutex
		keys []string
	)
	scan := func(ctx context.Context, c redis.Cmdable) error {
		var cursor uint64
		for {
			batch, next, err := c.Scan(ctx, cursor, legacyCountPrefix+"*", 2000).Result()
			if err != nil {
				return err
			}
			mu.Lock()
			keys = append(keys, batch...)
			mu.Unlock()
			if cursor = next; cursor == 0 {
				return nil
			}
		}
	}
	var err error
	if cc, ok := r.rdb.(*redis.ClusterClient); ok {
		err = cc.ForEachMaster(ctx, func(ctx context.Context, c *redis.Client) error { return scan(ctx, c) })
	} else {
		err = scan(ctx, r.rdb)
	}
	if err != nil {
		return 0, err
	}
	now := r.now()
	merged := 0
	index := map[string][]any{}
	for i := 0; i < len(keys); i += 200 {
		batch := keys[i:min(i+200, len(keys))]
		cmds := make([]*redis.Cmd, len(batch))
		_, perr := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
			for j, k := range batch {
				cmds[j] = legacyMerge.Eval(ctx, p, []string{k, "rc" + k[len("rv"):]})
			}
			return nil
		})
		if perr != nil && !errors.Is(perr, redis.Nil) && err == nil {
			err = perr
		}
		for j, c := range cmds {
			if n, e := c.Int64(); e != nil || n == 0 {
				continue
			}
			merged++
			if lookup == nil {
				continue
			}
			// rv:{特征:应用|g:场景|*:取值}:桶
			k := batch[j]
			end := strings.LastIndex(k, "}:")
			if end < 0 {
				continue
			}
			base := k[len(legacyCountPrefix):end]
			parts := strings.SplitN(base, ":", 4)
			if len(parts) != 4 {
				continue
			}
			entity, window, ok := lookup(parts[0])
			if !ok {
				continue
			}
			appID, _ := strconv.ParseInt(parts[1], 10, 64)
			tag := engine.CounterTag(entity, appID, parts[3])
			if tag == "" || (tag[0] == 'a' && appID == 0) {
				continue
			}
			expire := now.Unix() + int64(window) + bucketWidth(window) + 5
			index[tag] = append(index[tag], expire, indexEntry(roleSubject, kindCount, window, base))
		}
	}
	if len(index) > 0 {
		_, perr := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
			for tag, pairs := range index {
				indexAdd.Eval(ctx, p, []string{indexPrefix + clean(tag) + "}"}, append([]any{now.Unix()}, pairs...)...)
			}
			return nil
		})
		if perr != nil && !errors.Is(perr, redis.Nil) && err == nil {
			err = perr
		}
	}
	return merged, err
}
