// Package realtime 每个人的实时听歌状态（Redis）：最近播放、正反馈、负反馈、不喜欢的歌手。
// ingest 每批事件写进来，推荐时读出来做种子与过滤，让两次训练之间的新行为当场生效。
//
// 写入全部幂等（同一事件写两次结果不变）：分数是事件时刻，用 ZADD GT 只往新里改；
// 「后来又听完了被跳过的歌」这种冲销在 Lua 里按时刻比较后才删，重投的旧事件删不掉新状态。
package realtime

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"aegis/services/music/contract"
	"aegis/services/music/internal/signal"
)

// 每类集合保留的条数与过期时间。
const (
	keepRecent   = 200
	keepPositive = 100
	keepNegative = 300
	keepArtists  = 100
	stateTTL     = 180 * 24 * time.Hour
)

// Store Redis 实时状态。
type Store struct {
	rdb    redis.UniversalClient
	prefix string
}

// New 构造。
func New(rdb redis.UniversalClient, prefix string) *Store {
	if prefix == "" {
		prefix = "mu:"
	}
	return &Store{rdb: rdb, prefix: prefix}
}

// Client 底层客户端（缓存与锁复用）。
func (s *Store) Client() redis.UniversalClient { return s.rdb }

// Prefix 键前缀。
func (s *Store) Prefix() string { return s.prefix }

func (s *Store) key(appID, userID int64, kind string) string {
	// {app:user} 哈希标签：同一个人的键落在同一个槽（Redis Cluster 下 Lua 才能一起操作）
	return s.prefix + "{" + strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(userID, 10) + "}:" + kind
}

// 正反馈：pos 记时刻（GT），posw 记那一次的分；同时把它从 neg 里冲销（只删更早的）。
const applyPositive = `
local ts = tonumber(ARGV[2])
redis.call('ZADD', KEYS[1], 'GT', ts, ARGV[1])
local cur = redis.call('ZSCORE', KEYS[1], ARGV[1])
if cur and tonumber(cur) == ts then redis.call('HSET', KEYS[2], ARGV[1], ARGV[3]) end
local neg = redis.call('ZSCORE', KEYS[3], ARGV[1])
if neg and tonumber(neg) < ts then redis.call('ZREM', KEYS[3], ARGV[1]) end
if redis.call('ZCARD', KEYS[1]) > tonumber(ARGV[4]) then
  local old = redis.call('ZRANGE', KEYS[1], 0, -(tonumber(ARGV[4]) + 1))
  redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -(tonumber(ARGV[4]) + 1))
  if #old > 0 then redis.call('HDEL', KEYS[2], unpack(old)) end
end
for i = 1, 3 do redis.call('PEXPIRE', KEYS[i], ARGV[5]) end
return 1`

// 负反馈：neg 记时刻（GT）；不感兴趣（强负反馈）还要从 pos 里删掉更早的。
const applyNegative = `
local ts = tonumber(ARGV[2])
redis.call('ZADD', KEYS[1], 'GT', ts, ARGV[1])
if ARGV[3] == '1' then
  local pos = redis.call('ZSCORE', KEYS[2], ARGV[1])
  if pos and tonumber(pos) < ts then
    redis.call('ZREM', KEYS[2], ARGV[1])
    redis.call('HDEL', KEYS[3], ARGV[1])
  end
end
if redis.call('ZCARD', KEYS[1]) > tonumber(ARGV[4]) then
  redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -(tonumber(ARGV[4]) + 1))
end
for i = 1, 3 do redis.call('PEXPIRE', KEYS[i], ARGV[5]) end
return 1`

// 取消收藏：删掉更早的正反馈。
const applyUnlike = `
local pos = redis.call('ZSCORE', KEYS[1], ARGV[1])
if pos and tonumber(pos) < tonumber(ARGV[2]) then
  redis.call('ZREM', KEYS[1], ARGV[1])
  redis.call('HDEL', KEYS[2], ARGV[1])
end
return 1`

// 撤销：删掉早于撤销时刻的标记（重投的旧撤销删不掉之后的新标记）。KEYS 任意个 ZSET。
const applyUndo = `
local ts = tonumber(ARGV[2])
for i = 1, #KEYS do
  local v = redis.call('ZSCORE', KEYS[i], ARGV[1])
  if v and tonumber(v) <= ts then redis.call('ZREM', KEYS[i], ARGV[1]) end
end
return 1`

// 暂不推荐：分数是到期时刻，按事件时刻 + 天数算（GT，重投结果不变）；同时记下标记时刻用于撤销比较。
const applySuppress = `
redis.call('ZADD', KEYS[1], 'GT', tonumber(ARGV[2]), ARGV[1])
redis.call('ZADD', KEYS[2], 'GT', tonumber(ARGV[3]), ARGV[1])
for i = 1, 2 do redis.call('PEXPIRE', KEYS[i], ARGV[4]) end
return 1`

// Params 每个应用影响实时状态的参数。
type Params struct {
	Weight       func(k signal.Kind) float64
	SuppressDays int
}

// Apply 把一批事件写进实时状态。params 给出某应用的权重与「听腻了」暂不推荐天数（应用配置）。
func (s *Store) Apply(ctx context.Context, events []contract.Event, params func(appID int64) Params) error {
	if len(events) == 0 {
		return nil
	}
	ttl := strconv.FormatInt(stateTTL.Milliseconds(), 10)
	pipe := s.rdb.Pipeline()
	touched := map[string]bool{}
	for i := range events {
		e := &events[i]
		if e.UserID <= 0 {
			continue
		}
		k := signal.Classify(e)
		ts := strconv.FormatInt(e.TS, 10)
		key := func(kind string) string { return s.key(e.AppID, e.UserID, kind) }
		// 歌手级不感兴趣 / 撤销：不需要作品键
		artist := ""
		if e.Track != nil {
			artist = contract.FirstArtist(e.Track.Artist)
		}
		if a := strings.TrimSpace(e.Props["artist"]); a != "" {
			artist = contract.FirstArtist(a)
		}
		reason := contract.DislikeReasonOf(e.Props)
		if reason == contract.DislikeArtist && artist != "" {
			switch e.Type {
			case contract.TypeDislike:
				pipe.ZAddGT(ctx, key("artneg"), redis.Z{Score: float64(e.TS), Member: artist})
				touched[key("artneg")] = true
			case contract.TypeUndislike:
				pipe.Eval(ctx, applyUndo, []string{key("artneg")}, artist, ts)
			}
		}
		if e.WorkKey == "" {
			continue
		}
		if e.Type == contract.TypePlayStart || e.Type == contract.TypePlayEnd {
			pipe.ZAddGT(ctx, key("recent"), redis.Z{Score: float64(e.TS), Member: e.WorkKey})
			touched[key("recent")] = true
		}
		// 点了、放了、收藏了：这首的曝光疲劳清零
		if e.Type == contract.TypePlayStart || e.Type == contract.TypeClick || signal.Positive(k) {
			pipe.HDel(ctx, key("expo"), e.WorkKey)
		}
		p := params(e.AppID)
		switch {
		case k == signal.KindUnlike:
			pipe.Eval(ctx, applyUnlike, []string{key("pos"), key("posw")}, e.WorkKey, ts)
		case signal.Positive(k):
			w := strconv.FormatFloat(p.Weight(k), 'f', 3, 64)
			pipe.Eval(ctx, applyPositive, []string{key("pos"), key("posw"), key("neg")}, e.WorkKey, ts, w, keepPositive, ttl)
		case signal.Negative(k):
			strong := "0"
			if k == signal.KindDislike {
				strong = "1"
				// 明确的不感兴趣单独记一份（「我的不喜欢」列表只列这些，不列早跳）
				if reason != contract.DislikeArtist {
					pipe.ZAddGT(ctx, key("dis"), redis.Z{Score: float64(e.TS), Member: e.WorkKey})
					touched[key("dis")] = true
				}
			}
			pipe.Eval(ctx, applyNegative, []string{key("neg"), key("pos"), key("posw")}, e.WorkKey, ts, strong, keepNegative, ttl)
		case k == signal.KindDislikeMild || k == signal.KindResolveFailed:
			days := p.SuppressDays
			if days <= 0 {
				days = 30
			}
			until := strconv.FormatInt(e.TS+int64(days)*24*3600*1000, 10)
			pipe.Eval(ctx, applySuppress, []string{key("sup"), key("supat")}, e.WorkKey, until, ts, ttl)
		case k == signal.KindDislikeVersion && e.TrackKey != "":
			pipe.ZAddGT(ctx, key("badver"), redis.Z{Score: float64(e.TS), Member: e.TrackKey})
			touched[key("badver")] = true
		case k == signal.KindUndislike:
			pipe.Eval(ctx, applyUndo, []string{key("neg"), key("dis")}, e.WorkKey, ts)
		case k == signal.KindUndislikeMild:
			// sup 的分是到期时刻，按 supat（标记时刻）比较后两边一起删
			pipe.Eval(ctx, applyUndoSuppress, []string{key("sup"), key("supat")}, e.WorkKey, ts)
		case k == signal.KindUndislikeVersion && e.TrackKey != "":
			pipe.Eval(ctx, applyUndo, []string{key("badver")}, e.TrackKey, ts)
		}
	}
	for key := range touched {
		limit := keepRecent
		switch {
		case strings.HasSuffix(key, ":artneg"):
			limit = keepArtists
		case strings.HasSuffix(key, ":dis"), strings.HasSuffix(key, ":badver"):
			limit = keepNegative
		}
		pipe.ZRemRangeByRank(ctx, key, 0, int64(-(limit + 1)))
		pipe.PExpire(ctx, key, stateTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

const applyUndoSuppress = `
local at = redis.call('ZSCORE', KEYS[2], ARGV[1])
if at and tonumber(at) <= tonumber(ARGV[2]) then
  redis.call('ZREM', KEYS[1], ARGV[1])
  redis.call('ZREM', KEYS[2], ARGV[1])
end
return 1`

// Undo 撤销接口直接改状态（同时会有一条 undislike 事件经流落地，两边幂等）。kind：track / artist / suppressed / version。
func (s *Store) Undo(ctx context.Context, appID, userID int64, kind, member string, at int64) error {
	key := func(k string) string { return s.key(appID, userID, k) }
	ts := strconv.FormatInt(at, 10)
	switch kind {
	case "track":
		return s.rdb.Eval(ctx, applyUndo, []string{key("neg"), key("dis")}, member, ts).Err()
	case "artist":
		return s.rdb.Eval(ctx, applyUndo, []string{key("artneg")}, member, ts).Err()
	case "suppressed":
		return s.rdb.Eval(ctx, applyUndoSuppress, []string{key("sup"), key("supat")}, member, ts).Err()
	case "version":
		return s.rdb.Eval(ctx, applyUndo, []string{key("badver")}, member, ts).Err()
	}
	return nil
}

// fatigueTTL 曝光疲劳计数的保留时长（近 7 天：最后一次曝光后 7 天整体过期）。
const fatigueTTL = 7 * 24 * time.Hour

// AddExposure 记一次推荐曝光（出推荐时调，每首 +1）。
func (s *Store) AddExposure(ctx context.Context, appID, userID int64, workKeys []string) error {
	if len(workKeys) == 0 {
		return nil
	}
	key := s.key(appID, userID, "expo")
	pipe := s.rdb.Pipeline()
	for _, k := range workKeys {
		pipe.HIncrBy(ctx, key, k, 1)
	}
	pipe.PExpire(ctx, key, fatigueTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// Entry 作品与时刻 / 分。
type Entry struct {
	WorkKey string
	At      int64
	Weight  float64
}

// State 一个人的实时状态。
type State struct {
	Recent          []Entry          // 最近播放，新在前
	Positive        []Entry          // 正反馈，新在前
	Negative        map[string]int64 // 负反馈（不感兴趣 + 早跳）→ 时刻
	Disliked        map[string]int64 // 明确的不感兴趣（作品）→ 时刻
	DislikedArtists map[string]int64 // 歌手（第一歌手名）→ 时刻
	Suppressed      map[string]int64 // 听腻了：作品 → 到期时刻
	BadVersions     map[string]int64 // 音质差 / 歌曲不对：音源版本 source:songId → 时刻
	Fatigue         map[string]int   // 近 7 天被推荐展示的次数（点了就清零）
}

// Empty 空状态（不读个人数据时用）。
func Empty() *State {
	return &State{Negative: map[string]int64{}, Disliked: map[string]int64{}, DislikedArtists: map[string]int64{},
		Suppressed: map[string]int64{}, BadVersions: map[string]int64{}, Fatigue: map[string]int{}}
}

// Load 读一个人的实时状态。
func (s *Store) Load(ctx context.Context, appID, userID int64) (*State, error) {
	pipe := s.rdb.Pipeline()
	recent := pipe.ZRevRangeWithScores(ctx, s.key(appID, userID, "recent"), 0, keepRecent-1)
	pos := pipe.ZRevRangeWithScores(ctx, s.key(appID, userID, "pos"), 0, keepPositive-1)
	posw := pipe.HGetAll(ctx, s.key(appID, userID, "posw"))
	neg := pipe.ZRangeWithScores(ctx, s.key(appID, userID, "neg"), 0, -1)
	art := pipe.ZRangeWithScores(ctx, s.key(appID, userID, "artneg"), 0, -1)
	dis := pipe.ZRangeWithScores(ctx, s.key(appID, userID, "dis"), 0, -1)
	sup := pipe.ZRangeByScoreWithScores(ctx, s.key(appID, userID, "sup"), &redis.ZRangeBy{Min: strconv.FormatInt(time.Now().UnixMilli(), 10), Max: "+inf"})
	bad := pipe.ZRangeWithScores(ctx, s.key(appID, userID, "badver"), 0, -1)
	expo := pipe.HGetAll(ctx, s.key(appID, userID, "expo"))
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	st := Empty()
	for _, z := range dis.Val() {
		st.Disliked[member(z)] = int64(z.Score)
	}
	for _, z := range sup.Val() {
		st.Suppressed[member(z)] = int64(z.Score)
	}
	for _, z := range bad.Val() {
		st.BadVersions[member(z)] = int64(z.Score)
	}
	for k, v := range expo.Val() {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			st.Fatigue[k] = n
		}
	}
	for _, z := range recent.Val() {
		st.Recent = append(st.Recent, Entry{WorkKey: member(z), At: int64(z.Score)})
	}
	weights := posw.Val()
	for _, z := range pos.Val() {
		key := member(z)
		w, _ := strconv.ParseFloat(weights[key], 64)
		st.Positive = append(st.Positive, Entry{WorkKey: key, At: int64(z.Score), Weight: w})
	}
	for _, z := range neg.Val() {
		st.Negative[member(z)] = int64(z.Score)
	}
	for _, z := range art.Val() {
		st.DislikedArtists[member(z)] = int64(z.Score)
	}
	return st, nil
}

func member(z redis.Z) string {
	if v, ok := z.Member.(string); ok {
		return v
	}
	return ""
}

// Forget 删掉一个人名下的全部键（实时状态、各种缓存）。同一个人的键都带 {app:user} 哈希标签，
// Redis Cluster 下落在同一个槽，所以扫描每个主节点、按前缀匹配即可。
func (s *Store) Forget(ctx context.Context, appID, userID int64) error {
	pattern := s.key(appID, userID, "*")
	var keys []string
	scan := func(ctx context.Context, c redis.UniversalClient) error {
		it := c.Scan(ctx, 0, pattern, 500).Iterator()
		for it.Next(ctx) {
			keys = append(keys, it.Val())
		}
		return it.Err()
	}
	var err error
	if cc, ok := s.rdb.(*redis.ClusterClient); ok {
		var mu sync.Mutex
		err = cc.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error {
			var local []string
			it := node.Scan(ctx, 0, pattern, 500).Iterator()
			for it.Next(ctx) {
				local = append(local, it.Val())
			}
			mu.Lock()
			keys = append(keys, local...)
			mu.Unlock()
			return it.Err()
		})
	} else {
		err = scan(ctx, s.rdb)
	}
	if err != nil {
		return err
	}
	for start := 0; start < len(keys); start += 200 {
		if err := s.rdb.Del(ctx, keys[start:min(start+200, len(keys))]...).Err(); err != nil {
			return err
		}
	}
	return nil
}

// UserKey 这个人名下的其它键（每日推荐缓存、电台去重、画像缓存）。
func (s *Store) UserKey(appID, userID int64, kind string) string { return s.key(appID, userID, kind) }
