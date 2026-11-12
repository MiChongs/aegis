package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/proxyip"
	"aegis/services/risk/internal/sources"
	"aegis/services/risk/internal/stream"
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
	var fs forgetSet
	defer func() {
		// 从黑名单解除的对象，它自己的计数一并清零：否则计数还在高位，下一次请求又会被同一条规则拉回名单
		if _, problems := s.forget(ctx, fs); len(problems) > 0 {
			s.logf("名单移除后清理计数：%s", strings.Join(problems, "；"))
		}
	}()
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
		if !blocked {
			switch e.ListType {
			case bundle.ListDevice:
				fs.subject(bundle.EntityDevice, e.AppID, e.Value)
			case bundle.ListIP:
				fs.subject(bundle.EntityIP, e.AppID, e.Value)
			case bundle.ListUser:
				fs.subject(bundle.EntityUser, e.AppID, e.Value)
			case bundle.ListAccount:
				if e.AppID > 0 {
					fs.subject(bundle.EntityAccount, e.AppID, e.Value)
				}
			}
		}
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
	return append(problems, s.forgetCounters(ctx, nb, nil)...)
}

// refreshAfterDelete 删号之后：删用户点与只属于它们的点，设备与 IP 按剩下的使用者重写
func (s *Server) refreshAfterDelete(ctx context.Context, nb *neighborhood) (removed int, problems []string) {
	if s.Graph == nil {
		s.purgeFeatureCache(ctx, nb)
		return 0, s.forgetCounters(ctx, nb, nil)
	}
	var drop []string
	// 删号以后没有别人在用的设备：以它为主体的计数整个清掉
	var orphanDevices []string
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
			if len(rest) == 0 && d != "" {
				orphanDevices = append(orphanDevices, d)
			}
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
	return removed, append(problems, s.forgetCounters(ctx, nb, orphanDevices)...)
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

// forgetSet 要从计数里拿掉的对象：滑窗计数的标签（Redis）与流式特征的遗忘记录（RisingWave）
type forgetSet struct {
	subjects []string
	stones   []stream.Tombstone
}

// subject 被解除或删除的对象本身：以它为主体的计数整个清掉，它在别的计数里的贡献减掉
func (f *forgetSet) subject(entity string, appID int64, value string) {
	if value = strings.TrimSpace(value); value == "" {
		return
	}
	if tag := engine.CounterTag(entity, appID, value); tag != "" {
		f.subjects = append(f.subjects, tag)
	}
	if stream.ForgetKind(entity) {
		f.stones = append(f.stones, stream.Tombstone{Kind: entity, AppID: appID, Value: value})
	}
}

// forget 执行。滑窗计数按反向索引定位，只动这些对象的那一份；流式特征写遗忘表，由 RisingWave 重算后改写 Redis。
func (s *Server) forget(ctx context.Context, f forgetSet) (res sources.ForgetResult, problems []string) {
	if len(f.subjects) > 0 && s.Redis != nil {
		var err error
		if res, err = sources.NewRedis(s.Redis).Forget(ctx, f.subjects, nil); err != nil {
			problems = append(problems, "滑窗计数没有全部更新："+err.Error())
		}
	}
	if len(f.stones) > 0 && s.Cfg.RisingWaveDSN != "" {
		m, err := s.streamManager(ctx)
		if err == nil {
			err = m.Forget(ctx, f.stones, time.Now())
			m.Close(ctx)
		}
		if err != nil {
			problems = append(problems, "流式特征没有更新："+err.Error())
		}
	}
	return res, problems
}

// forgetCounters 解除或删号之后更新计数。
//
// 账号与它的登录名是主体：它们自己的计数清零，它们在地址、网段、共用设备、应用等计数里的贡献减掉，别人的不动。
// orphanDevices 是删号后已经没人用的设备，同样按主体处理；还有别人在用的设备只减这些账号的贡献（靠账号标签认出）。
func (s *Server) forgetCounters(ctx context.Context, nb *neighborhood, orphanDevices []string) []string {
	var fs forgetSet
	for _, uid := range nb.users {
		fs.subject(bundle.EntityUser, nb.appID, strconv.FormatInt(uid, 10))
	}
	for a := range nb.accounts {
		fs.subject(bundle.EntityAccount, nb.appID, a)
	}
	for _, d := range orphanDevices {
		fs.subject(bundle.EntityDevice, nb.appID, d)
	}
	res, problems := s.forget(ctx, fs)
	nb.countersCleared += res.Counters
	return problems
}

// MigrateCounters 把旧格式的滑窗次数并进新键（升级后第一次启动时有活干，之后是空转）
func (s *Server) MigrateCounters(ctx context.Context) {
	if s.Redis == nil {
		return
	}
	type def struct {
		entity string
		window int
	}
	defs := map[string]def{}
	add := func(fs []bundle.Feature) {
		for _, f := range fs {
			if f.Kind == bundle.FeatureVelocity && f.Velocity != nil {
				defs[f.Key] = def{f.Entity, f.Velocity.WindowSeconds}
			}
		}
	}
	// 已发布版本里的定义优先于目录（计数是按已发布的定义写的）
	if fs, err := s.Store.ListFeatures(ctx); err == nil {
		plain := make([]bundle.Feature, len(fs))
		for i, f := range fs {
			plain[i] = f.Feature
		}
		add(plain)
	}
	if states, err := s.Store.ListStates(ctx); err == nil {
		for _, st := range states {
			if st.PrimaryID == nil {
				continue
			}
			if rel, err := s.Store.GetRelease(ctx, *st.PrimaryID); err == nil && rel != nil {
				add(rel.Bundle.Features)
			}
		}
	}
	n, err := sources.NewRedis(s.Redis).MigrateLegacy(ctx, func(feature string) (string, int, bool) {
		d, ok := defs[feature]
		return d.entity, d.window, ok
	})
	if err != nil {
		s.logf("旧滑窗计数并入新格式：并入 %d 个桶后出错：%v", n, err)
	} else if n > 0 {
		s.logf("旧滑窗计数并入新格式：%d 个桶", n)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}
