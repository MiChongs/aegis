package admin

import (
	"context"
	"net/netip"
	"strconv"
	"strings"
	"sync"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/proxyip"

	"github.com/redis/go-redis/v9"
)

// 解除风控、删除账号之后把关系图、设备向量和特征缓存一起改到与事实一致，不留旧状态：
//
//   - 封禁标记按「现在还有没有生效的黑名单 / 封号」重算，而不是简单置 false：同一台设备可能还在另一条黑名单里；
//   - 删号：用户点连边删掉；它的登录名、手机号、邮箱、第三方身份等只属于它的点一并删掉；
//     设备还有别人用就把向量挂到最近的使用者名下，没人用了且没被拉黑就删点删向量，被拉黑的留着（封禁标记要继续起作用）；
//     IP 重数用户数、重判共享出口，没人用又没被拉黑的删掉；
//   - 图特征缓存（rg:*，默认 1 分钟、可配更长）按受影响的用户、设备、IP、登录名逐个删除，
//     受影响的包括这些账号本身和与它们相连的账号（它们的「同设备账号数」「被封邻居数」都变了）。

// neighborhood 一批账号在关系图与流水里的全部相关对象（删点之前先收集）
type neighborhood struct {
	appID    int64
	users    []int64
	userVIDs map[string]bool
	entities map[string]graph.Node // 点 ID → 点
	related  []int64
	devices  map[string]bool // 设备码
	ips      map[string]bool // 具体 IP（名单与缓存按具体地址）
	accounts map[string]bool
	// countersCleared 收尾时删掉的特征缓存与计数键数
	countersCleared int
}

func (nb *neighborhood) list(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (s *Server) collectNeighborhood(ctx context.Context, appID int64, users []int64) *neighborhood {
	nb := &neighborhood{appID: appID, users: users, userVIDs: map[string]bool{}, entities: map[string]graph.Node{},
		devices: map[string]bool{}, ips: map[string]bool{}, accounts: map[string]bool{}}
	for _, uid := range users {
		nb.userVIDs[graph.UserVID(appID, uid)] = true
	}
	if s.Graph != nil {
		for _, uid := range users {
			nodes, err := s.Graph.Entities(appID, uid, 0)
			if err != nil {
				s.logf("关系图收尾：读用户 %d 的关联对象失败：%v", uid, err)
				continue
			}
			for _, n := range nodes {
				nb.entities[n.ID] = n
				switch n.Kind {
				case graph.TagDevice:
					if d, _ := n.Props["device_id"].(string); d != "" {
						nb.devices[d] = true
					}
				case graph.TagIP:
					if ip := strings.TrimPrefix(n.ID, "ip:"); !strings.Contains(ip, "/") {
						nb.ips[ip] = true
					}
				case graph.TagAccount:
					if a, _ := n.Props["name"].(string); a != "" {
						nb.accounts[a] = true
					}
				}
			}
		}
		if rel, err := s.Graph.RelatedUsers(appID, users, graph.RelatedOptions{PerEntity: 50, Max: 500}); err == nil {
			for _, r := range rel {
				nb.related = append(nb.related, r.UserID)
			}
		}
	}
	// 流水里的设备与具体 IP（IPv6 在图里是 /64 线路，名单与特征缓存按具体地址）
	if s.CH != nil && len(users) > 0 {
		var rows []struct {
			Device string `ch:"device_id"`
			IP     string `ch:"ip"`
		}
		if err := s.CH.Conn().Select(ctx, &rows, `SELECT DISTINCT device_id, ip FROM decisions WHERE app_id = ? AND user_id IN ? LIMIT 2000`, appID, users); err == nil {
			for _, r := range rows {
				if r.Device != "" {
					nb.devices[r.Device] = true
				}
				if r.IP != "" {
					nb.ips[r.IP] = true
				}
			}
		}
	}
	return nb
}

// hasBlock 这个对象现在是否还在生效的黑名单里（本应用或全平台）
func (s *Server) hasBlock(ctx context.Context, appID int64, listType, value string) bool {
	v, err := lists.Normalize(listType, value)
	if err != nil {
		return false
	}
	es, err := s.Store.EntriesFor(ctx, listType, v)
	if err != nil {
		return false
	}
	for _, e := range es {
		if e.Kind == bundle.KindBlock && (e.AppID == 0 || e.AppID == appID) {
			return true
		}
	}
	return false
}

// recomputeBanned 名单条目移除后，按剩下的名单重算对应设备、IP、用户的封禁标记（图与向量）
func (s *Server) recomputeBanned(ctx context.Context, removed []lists.Entry) {
	seen := map[string]bool{}
	for _, e := range removed {
		if e.Kind != bundle.KindBlock {
			continue
		}
		key := e.ListType + "|" + strconv.FormatInt(e.AppID, 10) + "|" + e.Value
		if seen[key] {
			continue
		}
		seen[key] = true
		blocked := s.hasBlock(ctx, e.AppID, e.ListType, e.Value)
		switch e.ListType {
		case bundle.ListDevice:
			if s.Graph != nil {
				_ = s.Graph.SetBanned(graph.DeviceVID(e.Value), blocked)
			}
			if s.Vector != nil && e.AppID > 0 {
				_ = s.Vector.SetBanned(ctx, e.AppID, e.Value, blocked)
			}
		case bundle.ListIP:
			if s.Graph != nil && graph.IPKey(e.Value) == e.Value {
				_ = s.Graph.SetBanned(graph.IPVID(e.Value), blocked)
			}
		case bundle.ListUser:
			// 用户点的封禁还取决于主服务的封号：由 release 按主服务给的结果重写
			if s.Graph != nil && e.AppID > 0 && blocked {
				uid, _ := strconv.ParseInt(e.Value, 10, 64)
				_ = s.Graph.SetBanned(graph.UserVID(e.AppID, uid), true)
			}
		}
	}
}

// refreshAfterRelease 解除之后：用户点按「仍在封号或仍在用户黑名单」重写，名下设备与 IP 按名单重算
func (s *Server) refreshAfterRelease(ctx context.Context, nb *neighborhood, stillBanned map[int64]bool) []string {
	var problems []string
	if s.Graph != nil {
		var gerr error
		for _, uid := range nb.users {
			banned := stillBanned[uid] || s.hasBlock(ctx, nb.appID, bundle.ListUser, strconv.FormatInt(uid, 10))
			if err := s.Graph.SetUserState(nb.appID, uid, banned); err != nil && gerr == nil {
				gerr = err
			}
		}
		for vid, n := range nb.entities {
			switch n.Kind {
			case graph.TagDevice:
				d, _ := n.Props["device_id"].(string)
				if d == "" {
					continue
				}
				blocked := s.hasBlock(ctx, nb.appID, bundle.ListDevice, d)
				if err := s.Graph.SetBanned(vid, blocked); err != nil && gerr == nil {
					gerr = err
				}
			case graph.TagIP:
				ip := strings.TrimPrefix(vid, "ip:")
				if strings.Contains(ip, "/") {
					continue
				}
				if err := s.Graph.SetBanned(vid, s.hasBlock(ctx, nb.appID, bundle.ListIP, ip)); err != nil && gerr == nil {
					gerr = err
				}
			}
		}
		if gerr != nil {
			problems = append(problems, "关系图封禁标记没有全部更新："+gerr.Error())
		}
	}
	if s.Vector != nil {
		for d := range nb.devices {
			_ = s.Vector.SetBanned(ctx, nb.appID, d, s.hasBlock(ctx, nb.appID, bundle.ListDevice, d))
		}
	}
	s.purgeFeatureCache(ctx, nb)
	return problems
}

// refreshAfterDelete 删号之后：删用户点与只属于它们的点，设备与 IP 按剩下的使用者重写
func (s *Server) refreshAfterDelete(ctx context.Context, nb *neighborhood) (removed int, problems []string) {
	if s.Graph == nil {
		s.purgeFeatureCache(ctx, nb)
		return 0, nil
	}
	var drop []string
	var gerr error
	keep := func(err error) {
		if err != nil && gerr == nil {
			gerr = err
		}
	}
	for vid, n := range nb.entities {
		switch n.Kind {
		case graph.TagDevice:
			d, _ := n.Props["device_id"].(string)
			rest, err := s.Graph.NeighborUsers(vid, nb.userVIDs, 1)
			if err != nil {
				keep(err)
				continue
			}
			blocked := d != "" && s.hasBlock(ctx, nb.appID, bundle.ListDevice, d)
			switch {
			case len(rest) > 0:
				if s.Vector != nil && d != "" {
					uid, _ := strconv.ParseInt(rest[0].VID[strings.LastIndexByte(rest[0].VID, ':')+1:], 10, 64)
					keep(s.Vector.SetOwner(ctx, nb.appID, d, uid, blocked))
				}
			case blocked:
				// 没人用了但在黑名单里：点留着，封禁标记继续让同设备的新账号命中
				keep(s.Graph.SetBanned(vid, true))
				if s.Vector != nil {
					keep(s.Vector.SetOwner(ctx, nb.appID, d, 0, true))
				}
			default:
				drop = append(drop, vid)
				if s.Vector != nil && d != "" {
					keep(s.Vector.Delete(ctx, nb.appID, []string{d}))
				}
			}
		case graph.TagIP:
			rest, err := s.Graph.NeighborUsers(vid, nb.userVIDs, 5000)
			if err != nil {
				keep(err)
				continue
			}
			ip := strings.TrimPrefix(vid, "ip:")
			blocked := !strings.Contains(ip, "/") && s.hasBlock(ctx, nb.appID, bundle.ListIP, ip)
			if len(rest) == 0 && !blocked {
				drop = append(drop, vid)
				continue
			}
			keep(s.Graph.SetIPUsers(vid, int64(len(rest)), len(rest) >= proxyip.SharedUsers))
		default:
			// 登录名、手机号、邮箱、第三方身份、付款账号、通行密钥：没有别的用户就删
			rest, err := s.Graph.NeighborUsers(vid, nb.userVIDs, 1)
			if err != nil {
				keep(err)
				continue
			}
			if len(rest) == 0 {
				drop = append(drop, vid)
			}
		}
	}
	for vid := range nb.userVIDs {
		drop = append(drop, vid)
	}
	if err := s.Graph.DeleteVertices(drop); err != nil {
		keep(err)
	} else {
		removed = len(drop)
	}
	if gerr != nil {
		problems = append(problems, "关系图没有全部更新："+gerr.Error())
	}
	s.purgeFeatureCache(ctx, nb)
	return removed, problems
}

// purgeFeatureCache 删受影响对象的图特征缓存（rg:<模板>:<天数>:<应用>:<对象类型>:<取值>）
func (s *Server) purgeFeatureCache(ctx context.Context, nb *neighborhood) {
	if s.Redis == nil {
		return
	}
	type def struct {
		template string
		days     int
		entity   string
	}
	defs := map[def]bool{}
	addDefs := func(fs []bundle.Feature) {
		for _, f := range fs {
			if f.Kind != bundle.FeatureGraph || f.Graph == nil {
				continue
			}
			entity := f.Entity
			if entity == "" {
				entity = bundle.EntityUser
			}
			defs[def{f.Graph.Template, f.Graph.WithinDays, entity}] = true
		}
	}
	if fs, err := s.Store.ListFeatures(ctx); err == nil {
		plain := make([]bundle.Feature, len(fs))
		for i, f := range fs {
			plain[i] = f.Feature
		}
		addDefs(plain)
	}
	// 已发布版本里的特征定义可能与当前目录不同（发布后又改过）：一并算上
	if states, err := s.Store.ListStates(ctx); err == nil {
		for _, st := range states {
			for _, id := range []*int64{st.PrimaryID, st.CandidateID} {
				if id == nil {
					continue
				}
				if rel, err := s.Store.GetRelease(ctx, *id); err == nil && rel != nil {
					addDefs(rel.Bundle.Features)
				}
			}
		}
	}
	values := map[string][]string{bundle.EntityUser: {}, bundle.EntityDevice: {}, bundle.EntityIP: {}, bundle.EntityAccount: {}}
	for _, uid := range append(append([]int64{}, nb.users...), nb.related...) {
		values[bundle.EntityUser] = append(values[bundle.EntityUser], strconv.FormatInt(uid, 10))
	}
	for d := range nb.devices {
		values[bundle.EntityDevice] = append(values[bundle.EntityDevice], d)
	}
	for ip := range nb.ips {
		values[bundle.EntityIP] = append(values[bundle.EntityIP], ip)
	}
	for a := range nb.accounts {
		values[bundle.EntityAccount] = append(values[bundle.EntityAccount], a, strings.ToLower(a))
	}
	app := strconv.FormatInt(nb.appID, 10)
	var keys []string
	for d := range defs {
		for _, v := range values[d.entity] {
			keys = append(keys, "rg:"+d.template+":"+strconv.Itoa(d.days)+":"+app+":"+d.entity+":"+v)
		}
	}
	s.delKeys(ctx, keys)
	nb.countersCleared += len(keys)
	s.purgeCounters(ctx, nb)
}

// delKeys 集群模式下多键 DEL 不能跨槽：逐个删，管道批量发
func (s *Server) delKeys(ctx context.Context, keys []string) {
	for i := 0; i < len(keys); i += 500 {
		batch := keys[i:min(i+500, len(keys))]
		p := s.Redis.Pipeline()
		for _, k := range batch {
			p.Del(ctx, k)
		}
		_, _ = p.Exec(ctx)
	}
}

// purgeCounters 删受影响对象的滑窗计数（rv: / rvd:）与流式特征（rf:）。
//
// 滑窗键 rv:{特征:应用|g:场景|*:取值}:桶、rvd:{…}:桶（去重计数是 HyperLogLog，删不掉单个成员，只能整键删）；
// 流式特征键 rf:特征:应用|g:取值（RisingWave 写回，下一笔事件到来时按物化视图重写）。
// 场景与特征太多，逐个拼键不现实：各主节点 SCAN 一遍按取值匹配。取值涵盖账号 ID、登录名、设备码、具体 IP 与所在网段。
func (s *Server) purgeCounters(ctx context.Context, nb *neighborhood) int {
	values := map[string]bool{}
	for _, uid := range nb.users {
		values[strconv.FormatInt(uid, 10)] = true
	}
	for d := range nb.devices {
		values[d] = true
	}
	for a := range nb.accounts {
		values[strings.ToLower(a)] = true
	}
	for ip := range nb.ips {
		values[ip] = true
		if sn := ipSubnet(ip); sn != "" {
			values[sn] = true
		}
	}
	if len(values) == 0 {
		return 0
	}
	var mu sync.Mutex
	var doomed []string
	scan := func(ctx context.Context, c redis.Cmdable) error {
		var cursor uint64
		for {
			keys, next, err := c.Scan(ctx, cursor, "r[vf]*", 2000).Result()
			if err != nil {
				return err
			}
			for _, k := range keys {
				if v, ok := counterValue(k); ok && values[v] {
					mu.Lock()
					doomed = append(doomed, k)
					mu.Unlock()
				}
			}
			cursor = next
			if cursor == 0 {
				return nil
			}
		}
	}
	var err error
	if cc, ok := s.Redis.(*redis.ClusterClient); ok {
		err = cc.ForEachMaster(ctx, func(ctx context.Context, c *redis.Client) error { return scan(ctx, c) })
	} else {
		err = scan(ctx, s.Redis)
	}
	if err != nil {
		s.logf("清理滑窗与流式计数时扫描失败：%v", err)
	}
	s.delKeys(ctx, doomed)
	nb.countersCleared += len(doomed)
	return len(doomed)
}

// counterValue 从计数键里取出主体取值：rv:{特征:范围:场景:取值}:桶、rvd:{…}:桶、rf:特征:范围:取值
func counterValue(key string) (string, bool) {
	switch {
	case strings.HasPrefix(key, "rv:{"), strings.HasPrefix(key, "rvd:{"):
		open := strings.IndexByte(key, '{')
		end := strings.LastIndexByte(key, '}')
		if open < 0 || end <= open {
			return "", false
		}
		parts := strings.SplitN(key[open+1:end], ":", 4)
		if len(parts) != 4 {
			return "", false
		}
		return parts[3], true
	case strings.HasPrefix(key, "rf:"):
		parts := strings.SplitN(strings.TrimPrefix(key, "rf:"), ":", 3)
		if len(parts) != 3 {
			return "", false
		}
		return parts[2], true
	}
	return "", false
}

// ipSubnet 与主服务上报事件时的网段一致：IPv4 /24、IPv6 /64
func ipSubnet(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 64
	}
	p, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return p.String()
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}
