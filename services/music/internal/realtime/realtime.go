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

// Apply 把一批事件写进实时状态。weight 函数给出某类反馈的分（应用配置里的权重）。
func (s *Store) Apply(ctx context.Context, events []contract.Event, weight func(appID int64, k signal.Kind) float64) error {
	if len(events) == 0 {
		return nil
	}
	ttl := strconv.FormatInt(stateTTL.Milliseconds(), 10)
	pipe := s.rdb.Pipeline()
	touched := map[string]bool{}
	for i := range events {
		e := &events[i]
		if e.WorkKey == "" || e.UserID <= 0 {
			continue
		}
		ts := strconv.FormatInt(e.TS, 10)
		recent := s.key(e.AppID, e.UserID, "recent")
		pos := s.key(e.AppID, e.UserID, "pos")
		posw := s.key(e.AppID, e.UserID, "posw")
		neg := s.key(e.AppID, e.UserID, "neg")
		if e.Type == contract.TypePlayStart || e.Type == contract.TypePlayEnd {
			pipe.ZAddGT(ctx, recent, redis.Z{Score: float64(e.TS), Member: e.WorkKey})
			touched[recent] = true
		}
		k := signal.Classify(e)
		switch {
		case k == signal.KindUnlike:
			pipe.Eval(ctx, applyUnlike, []string{pos, posw}, e.WorkKey, ts)
		case signal.Positive(k):
			w := strconv.FormatFloat(weight(e.AppID, k), 'f', 3, 64)
			pipe.Eval(ctx, applyPositive, []string{pos, posw, neg}, e.WorkKey, ts, w, keepPositive, ttl)
		case signal.Negative(k):
			strong := "0"
			if k == signal.KindDislike {
				strong = "1"
			}
			pipe.Eval(ctx, applyNegative, []string{neg, pos, posw}, e.WorkKey, ts, strong, keepNegative, ttl)
		}
		if e.Type == contract.TypeDislike && e.Props["scope"] == "artist" && e.Track != nil {
			if a := contract.FirstArtist(e.Track.Artist); a != "" {
				key := s.key(e.AppID, e.UserID, "artneg")
				pipe.ZAddGT(ctx, key, redis.Z{Score: float64(e.TS), Member: a})
				touched[key] = true
			}
		}
	}
	for key := range touched {
		limit := keepRecent
		if strings.HasSuffix(key, ":artneg") {
			limit = keepArtists
		}
		pipe.ZRemRangeByRank(ctx, key, 0, int64(-(limit + 1)))
		pipe.PExpire(ctx, key, stateTTL)
	}
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
	Negative        map[string]int64 // 负反馈 → 时刻
	DislikedArtists map[string]int64 // 歌手（原名）→ 时刻
}

// Load 读一个人的实时状态。
func (s *Store) Load(ctx context.Context, appID, userID int64) (*State, error) {
	pipe := s.rdb.Pipeline()
	recent := pipe.ZRevRangeWithScores(ctx, s.key(appID, userID, "recent"), 0, keepRecent-1)
	pos := pipe.ZRevRangeWithScores(ctx, s.key(appID, userID, "pos"), 0, keepPositive-1)
	posw := pipe.HGetAll(ctx, s.key(appID, userID, "posw"))
	neg := pipe.ZRangeWithScores(ctx, s.key(appID, userID, "neg"), 0, -1)
	art := pipe.ZRangeWithScores(ctx, s.key(appID, userID, "artneg"), 0, -1)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	st := &State{Negative: map[string]int64{}, DislikedArtists: map[string]int64{}}
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

// Forget 删掉一个人的全部实时状态与缓存（每日推荐、电台去重按日期命名，删今天与前两天；画像缓存按天数）。
func (s *Store) Forget(ctx context.Context, appID, userID int64) error {
	kinds := []string{"recent", "pos", "posw", "neg", "artneg"}
	for _, d := range []int{7, 30, 90, 365} {
		kinds = append(kinds, "profile:"+strconv.Itoa(d))
	}
	now := time.Now()
	for i := -1; i <= 2; i++ {
		day := now.AddDate(0, 0, -i).Format("20060102")
		kinds = append(kinds, "daily:"+day, "radio:"+day)
	}
	keys := make([]string, 0, len(kinds))
	for _, k := range kinds {
		keys = append(keys, s.key(appID, userID, k))
	}
	return s.rdb.Del(ctx, keys...).Err()
}

// UserKey 这个人名下的其它键（每日推荐缓存、电台去重、画像缓存）。
func (s *Store) UserKey(appID, userID int64, kind string) string { return s.key(appID, userID, kind) }
