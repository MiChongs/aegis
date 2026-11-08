// Package lists 名单的 Redis 索引。
//
// 权威数据在 PostgreSQL（risk_list_entries，由 risk-admin / risk-ingest 写）；这里是决策热路径读的那份：
//
//   - 精确值类型（IP、设备、账号、用户 ID）：每个取值一个 Hash `rl:{类型:取值}`，字段是条目 ID，值是条目 JSON。
//     一次决策按取值 HGETALL，几个类型在一个管道里发。
//   - 小名单类型（网段、ASN、国家、UA 关键词、邮箱域名、手机号段）：每个类型一个 Hash `rl:small:类型`，
//     决策实例整份装进内存，`rl:small:ver` 变了就重载。
//
// 过期条目读的时候就跳过，后台清理只是回收空间。
package lists

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/engine"

	"github.com/redis/go-redis/v9"
)

const versionKey = "rl:small:ver"

// Entry 一条名单
type Entry struct {
	ID         string     `json:"id"`
	ListType   string     `json:"type"`
	Value      string     `json:"value"`
	Kind       string     `json:"kind"`
	AppID      int64      `json:"app,omitempty"` // 0 = 全平台
	Scenes     []string   `json:"scenes,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Source     string     `json:"source,omitempty"` // manual / rule / cep / import / review
	CreatedBy  string     `json:"by,omitempty"`
	DecisionID string     `json:"decision,omitempty"`
	ExpiresAt  *time.Time `json:"exp,omitempty"`
	CreatedAt  time.Time  `json:"at"`
}

// Active 条目在 now 是否有效
func (e Entry) Active(now time.Time) bool { return e.ExpiresAt == nil || e.ExpiresAt.After(now) }

func (e Entry) applies(appID int64, scene string) bool {
	if e.AppID != 0 && e.AppID != appID {
		return false
	}
	if len(e.Scenes) == 0 {
		return true
	}
	for _, s := range e.Scenes {
		if s == scene || s == "*" {
			return true
		}
	}
	return false
}

// Normalize 规范化取值：IP 规范写法、账号与域名小写、网段规范成网络地址
func Normalize(listType, value string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", errors.New("取值为空")
	}
	switch listType {
	case bundle.ListIP:
		addr, err := netip.ParseAddr(v)
		if err != nil {
			return "", errors.New("不是合法的 IP")
		}
		return addr.Unmap().String(), nil
	case bundle.ListCIDR:
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return "", errors.New("不是合法的网段")
		}
		return p.Masked().String(), nil
	case bundle.ListAccount, bundle.ListEmailDomain, bundle.ListUAKeyword:
		return strings.ToLower(v), nil
	case bundle.ListCountry:
		if len(v) != 2 {
			return "", errors.New("国家用两位 ISO 代码")
		}
		return strings.ToUpper(v), nil
	case bundle.ListASN:
		v = strings.TrimPrefix(strings.ToUpper(v), "AS")
		for _, r := range v {
			if r < '0' || r > '9' {
				return "", errors.New("ASN 只能是数字")
			}
		}
		return v, nil
	case bundle.ListPhonePrefix, bundle.ListUser:
		for _, r := range v {
			if r < '0' || r > '9' {
				return "", errors.New("只能是数字")
			}
		}
		return v, nil
	case bundle.ListDevice:
		return v, nil
	}
	return "", errors.New("未知名单类型")
}

func exactKey(listType, value string) string { return "rl:{" + listType + ":" + value + "}" }

func smallKey(listType string) string { return "rl:small:" + listType }

// putExact 写一个条目并维护键的过期时间：永久条目让键不过期；只有限时条目时键跟着最晚的那个过期。
// TTL = -1 且不止一个字段，说明已经有永久条目，保持不过期。
var putExact = redis.NewScript(`
local existed = redis.call('HLEN', KEYS[1])
local same = redis.call('HEXISTS', KEYS[1], ARGV[1])
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
local exp = tonumber(ARGV[3])
if exp == 0 then
  redis.call('PERSIST', KEYS[1])
  return 0
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl == -1 then
  -- 新键，或者唯一的条目就是被覆盖的这条：跟着它过期；否则有别的永久条目，保持不过期
  if existed == 0 or (existed == 1 and same == 1) then
    redis.call('PEXPIREAT', KEYS[1], exp)
  end
  return 0
end
redis.call('PEXPIREAT', KEYS[1], exp, 'GT')
return 0
`)

// Index 名单的 Redis 索引
type Index struct {
	rdb redis.UniversalClient

	snap    atomic.Pointer[snapshot]
	loadMu  sync.Mutex
	version string
}

// NewIndex 新建索引
func NewIndex(rdb redis.UniversalClient) *Index { return &Index{rdb: rdb} }

// Put 写入（同一 ID 覆盖）。精确值键的过期时间取所有条目里最晚的那个；有永久条目就不过期。
func (x *Index) Put(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	smallChanged := false
	_, err := x.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for _, e := range entries {
			raw, err := json.Marshal(e)
			if err != nil {
				return err
			}
			if bundle.ExactListType(e.ListType) {
				var exp int64
				if e.ExpiresAt != nil {
					exp = e.ExpiresAt.Add(time.Minute).UnixMilli()
				}
				putExact.Eval(ctx, p, []string{exactKey(e.ListType, e.Value)}, e.ID, raw, exp)
			} else {
				p.HSet(ctx, smallKey(e.ListType), e.ID, raw)
				smallChanged = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if smallChanged {
		return x.rdb.Incr(ctx, versionKey).Err()
	}
	return nil
}

// Remove 删除
func (x *Index) Remove(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	smallChanged := false
	_, err := x.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for _, e := range entries {
			if bundle.ExactListType(e.ListType) {
				p.HDel(ctx, exactKey(e.ListType, e.Value), e.ID)
			} else {
				p.HDel(ctx, smallKey(e.ListType), e.ID)
				smallChanged = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if smallChanged {
		return x.rdb.Incr(ctx, versionKey).Err()
	}
	return nil
}

// Exact 取精确值类型某个取值上的全部条目（含过期，供管理端核对）
func (x *Index) Exact(ctx context.Context, listType, value string) ([]Entry, error) {
	m, err := x.rdb.HGetAll(ctx, exactKey(listType, value)).Result()
	if err != nil {
		return nil, err
	}
	return decodeAll(m), nil
}

func decodeAll(m map[string]string) []Entry {
	out := make([]Entry, 0, len(m))
	for _, raw := range m {
		var e Entry
		if json.Unmarshal([]byte(raw), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// snapshot 小名单的内存快照
type snapshot struct {
	cidr    []cidrEntry
	byValue map[string][]Entry // 类型:取值 → 条目（ASN、国家）
	domains map[string][]Entry // 邮箱域名（含子域匹配）
	phones  []Entry            // 号段（前缀匹配）
	ua      []Entry            // UA 关键词（包含匹配）
}

type cidrEntry struct {
	prefix netip.Prefix
	entry  Entry
}

// Refresh 版本变了就重载小名单
func (x *Index) Refresh(ctx context.Context) error {
	ver, err := x.rdb.Get(ctx, versionKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	x.loadMu.Lock()
	defer x.loadMu.Unlock()
	if x.snap.Load() != nil && ver == x.version {
		return nil
	}
	s := &snapshot{byValue: map[string][]Entry{}, domains: map[string][]Entry{}}
	for _, t := range bundle.ListTypes() {
		if bundle.ExactListType(t) {
			continue
		}
		m, err := x.rdb.HGetAll(ctx, smallKey(t)).Result()
		if err != nil {
			return err
		}
		for _, e := range decodeAll(m) {
			switch t {
			case bundle.ListCIDR:
				if p, err := netip.ParsePrefix(e.Value); err == nil {
					s.cidr = append(s.cidr, cidrEntry{prefix: p, entry: e})
				}
			case bundle.ListEmailDomain:
				s.domains[e.Value] = append(s.domains[e.Value], e)
			case bundle.ListPhonePrefix:
				s.phones = append(s.phones, e)
			case bundle.ListUAKeyword:
				s.ua = append(s.ua, e)
			default:
				s.byValue[t+":"+e.Value] = append(s.byValue[t+":"+e.Value], e)
			}
		}
	}
	x.snap.Store(s)
	x.version = ver
	return nil
}

// Watch 每隔 interval 检查一次版本
func (x *Index) Watch(ctx context.Context, interval time.Duration, onErr func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := x.Refresh(ctx); err != nil && onErr != nil {
				onErr(err)
			}
		}
	}
}

// Check 决策用：查一次请求的全部取值
func (x *Index) Check(ctx context.Context, q engine.ListQuery) ([]engine.ListHit, error) {
	now := time.Now()
	var hits []engine.ListHit
	add := func(e Entry, value string) {
		if e.Active(now) && e.applies(q.AppID, q.Scene) {
			hits = append(hits, engine.ListHit{ListType: e.ListType, Kind: e.Kind, Value: value, ListKey: e.ID, Reason: e.Reason})
		}
	}

	// 小名单：内存
	if s := x.snap.Load(); s != nil {
		if ip := q.Values[bundle.ListCIDR]; ip != "" {
			if addr, err := netip.ParseAddr(ip); err == nil {
				addr = addr.Unmap()
				for _, c := range s.cidr {
					if c.prefix.Contains(addr) {
						add(c.entry, c.prefix.String())
					}
				}
			}
		}
		for _, t := range []string{bundle.ListASN, bundle.ListCountry} {
			if v := q.Values[t]; v != "" {
				for _, e := range s.byValue[t+":"+v] {
					add(e, v)
				}
			}
		}
		if d := q.Values[bundle.ListEmailDomain]; d != "" {
			for cur := d; cur != ""; {
				for _, e := range s.domains[cur] {
					add(e, cur)
				}
				i := strings.IndexByte(cur, '.')
				if i < 0 {
					break
				}
				cur = cur[i+1:]
			}
		}
		if ph := q.Values[bundle.ListPhonePrefix]; ph != "" {
			for _, e := range s.phones {
				if strings.HasPrefix(ph, e.Value) {
					add(e, e.Value)
				}
			}
		}
		if ua := strings.ToLower(q.Values[bundle.ListUAKeyword]); ua != "" {
			for _, e := range s.ua {
				if strings.Contains(ua, e.Value) {
					add(e, e.Value)
				}
			}
		}
	}

	// 精确值：Redis 管道
	type probe struct {
		value string
		cmd   *redis.MapStringStringCmd
	}
	var probes []probe
	_, err := x.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for t, v := range q.Values {
			if !bundle.ExactListType(t) || v == "" {
				continue
			}
			if t == bundle.ListIP {
				if norm, err := Normalize(t, v); err == nil {
					v = norm
				}
			}
			probes = append(probes, probe{value: v, cmd: p.HGetAll(ctx, exactKey(t, v))})
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return hits, err
	}
	for _, pr := range probes {
		for _, e := range decodeAll(pr.cmd.Val()) {
			add(e, pr.value)
		}
	}
	return hits, nil
}

// PurgeExpired 清掉精确值键里已过期的字段（键整体过期由 Redis 负责）
func (x *Index) PurgeExpired(ctx context.Context, entries []Entry) error {
	now := time.Now()
	var gone []Entry
	for _, e := range entries {
		if !e.Active(now) {
			gone = append(gone, e)
		}
	}
	return x.Remove(ctx, gone)
}
