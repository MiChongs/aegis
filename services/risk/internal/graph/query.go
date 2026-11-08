package graph

import (
	"fmt"
	"strings"
	"time"

	"aegis/services/risk/internal/bundle"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

// FeatureQuery 图特征模板生成的 nGQL。主体点 ID 由调用方按主体类型算好。
// 时间窗用边上的 last_seen 过滤；同应用约束用点 ID 前缀（用户点带应用号）。
func FeatureQuery(template, vid string, appID int64, withinDays int, now time.Time) (string, error) {
	since := int64(0)
	if withinDays > 0 {
		since = now.Add(-time.Duration(withinDays) * 24 * time.Hour).Unix()
	}
	src := Quote(vid)
	appPrefix := Quote(fmt.Sprintf("u:%d:", appID))
	recent := fmt.Sprintf("properties(edge).last_seen >= %d", since)
	shared := func(edge string) string {
		return fmt.Sprintf(
			"GO FROM %s OVER %s WHERE %s YIELD dst(edge) AS x | GO FROM $-.x OVER %s REVERSELY WHERE %s AND src(edge) != %s AND src(edge) STARTS WITH %s YIELD DISTINCT src(edge) AS other | YIELD count(*) AS n",
			src, edge, recent, edge, recent, src, appPrefix)
	}
	switch template {
	case bundle.GraphDeviceAccounts:
		return shared(EdgeUsesDevice), nil
	case bundle.GraphIPAccounts:
		return shared(EdgeUsesIP), nil
	case bundle.GraphPhoneAccounts:
		return shared(EdgeHasPhone), nil
	case bundle.GraphBannedNeighbors:
		return fmt.Sprintf(
			"GO FROM %s OVER uses_device, uses_ip, has_phone WHERE %s YIELD dst(edge) AS x | GO FROM $-.x OVER uses_device, uses_ip, has_phone REVERSELY WHERE src(edge) != %s AND src(edge) STARTS WITH %s YIELD DISTINCT src(edge) AS other | FETCH PROP ON user $-.other YIELD user.banned AS banned | YIELD count(*) AS n WHERE $-.banned == true",
			src, recent, src, appPrefix), nil
	case bundle.GraphInviteChildren:
		return fmt.Sprintf("GO FROM %s OVER invited WHERE %s YIELD DISTINCT dst(edge) AS c | YIELD count(*) AS n", src, recent), nil
	case bundle.GraphInviteTree:
		return fmt.Sprintf("GO 1 TO 3 STEPS FROM %s OVER invited YIELD DISTINCT dst(edge) AS c | YIELD count(*) AS n", src), nil
	case bundle.GraphDeviceUsers, bundle.GraphIPUsers:
		edge := EdgeUsesDevice
		if template == bundle.GraphIPUsers {
			edge = EdgeUsesIP
		}
		return fmt.Sprintf("GO FROM %s OVER %s REVERSELY WHERE %s AND src(edge) STARTS WITH %s YIELD DISTINCT src(edge) AS u | YIELD count(*) AS n",
			src, edge, recent, appPrefix), nil
	}
	return "", fmt.Errorf("未知图特征模板 %q", template)
}

// SubjectVID 主体在图里的点 ID
func SubjectVID(entity string, appID int64, value string) string {
	switch entity {
	case bundle.EntityUser:
		var uid int64
		fmt.Sscan(value, &uid)
		return UserVID(appID, uid)
	case bundle.EntityAccount:
		return AccountVID(appID, value)
	case bundle.EntityDevice:
		return DeviceVID(value)
	case bundle.EntityIP:
		return IPVID(value)
	case bundle.EntityIPSubnet:
		return SubnetVID(value)
	case bundle.EntityPhone:
		return PhoneVID(value)
	}
	return ""
}

// Count 跑一条返回单列 n 的查询
func (c *Client) Count(stmt string) (float64, error) {
	rs, err := c.Exec(stmt)
	if err != nil {
		return 0, err
	}
	if rs.GetRowSize() == 0 {
		return 0, nil
	}
	rec, err := rs.GetRowValuesByIndex(0)
	if err != nil {
		return 0, err
	}
	v, err := rec.GetValueByIndex(0)
	if err != nil {
		return 0, err
	}
	n, err := v.AsInt()
	if err != nil {
		return 0, err
	}
	return float64(n), nil
}

// Node 关系图上的一个点（控制台画图用）
type Node struct {
	ID    string         `json:"id"`
	Kind  string         `json:"kind"`
	Label string         `json:"label"`
	Props map[string]any `json:"props"`
	Depth int            `json:"depth"`
}

// Link 一条边
type Link struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen"`
	Count     int64  `json:"count"`
}

// Subgraph 关系图
type Subgraph struct {
	Center    string `json:"center"`
	Nodes     []Node `json:"nodes"`
	Links     []Link `json:"links"`
	Truncated bool   `json:"truncated"`
}

// Expand 从一个点向外展开 depth 跳，每跳每个点最多 fanout 条边，总点数不超过 maxNodes。
// 不用 GET SUBGRAPH：热门 IP 上挂着上万账号时它会把整张子图搬回来。
func (c *Client) Expand(center string, depth, fanout, maxNodes int) (*Subgraph, error) {
	if depth <= 0 {
		depth = 2
	}
	if fanout <= 0 {
		fanout = 50
	}
	if maxNodes <= 0 {
		maxNodes = 300
	}
	g := &Subgraph{Center: center}
	seen := map[string]int{center: 0}
	order := []string{center}
	linkSeen := map[string]bool{}
	frontier := []string{center}
	edges := strings.Join(Edges(), ", ")
	for hop := 1; hop <= depth && len(frontier) > 0; hop++ {
		quoted := make([]string, len(frontier))
		for i, v := range frontier {
			quoted[i] = Quote(v)
		}
		stmt := fmt.Sprintf(
			"GO FROM %s OVER %s BIDIRECT YIELD src(edge) AS s, dst(edge) AS d, type(edge) AS t, properties(edge).first_seen AS f, properties(edge).last_seen AS l, properties(edge).count AS c | LIMIT %d",
			strings.Join(quoted, ", "), edges, fanout*len(frontier))
		rs, err := c.Exec(stmt)
		if err != nil {
			return nil, err
		}
		var next []string
		for i := 0; i < rs.GetRowSize(); i++ {
			rec, err := rs.GetRowValuesByIndex(i)
			if err != nil {
				continue
			}
			s := str(rec, 0)
			d := str(rec, 1)
			t := str(rec, 2)
			key := s + ">" + d + ":" + t
			if !linkSeen[key] {
				linkSeen[key] = true
				g.Links = append(g.Links, Link{Source: s, Target: d, Kind: t, FirstSeen: num(rec, 3), LastSeen: num(rec, 4), Count: num(rec, 5)})
			}
			for _, v := range []string{s, d} {
				if _, ok := seen[v]; ok {
					continue
				}
				if len(seen) >= maxNodes {
					g.Truncated = true
					continue
				}
				seen[v] = hop
				order = append(order, v)
				next = append(next, v)
			}
		}
		frontier = next
	}
	// 只保留两端都在图里的边
	kept := g.Links[:0]
	for _, l := range g.Links {
		if _, a := seen[l.Source]; a {
			if _, b := seen[l.Target]; b {
				kept = append(kept, l)
			}
		}
	}
	g.Links = kept
	props, err := c.fetchProps(order)
	if err != nil {
		return nil, err
	}
	for _, id := range order {
		p := props[id]
		if p == nil {
			p = map[string]any{}
		}
		g.Nodes = append(g.Nodes, Node{ID: id, Kind: Kind(id), Label: label(id, p), Props: p, Depth: seen[id]})
	}
	return g, nil
}

func (c *Client) fetchProps(ids []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	byTag := map[string][]string{}
	for _, id := range ids {
		if k := Kind(id); k != "" {
			byTag[k] = append(byTag[k], Quote(id))
		}
	}
	for tag, vids := range byTag {
		rs, err := c.Exec(fmt.Sprintf("FETCH PROP ON %s %s YIELD id(vertex) AS id, properties(vertex) AS p", tag, strings.Join(vids, ", ")))
		if err != nil {
			return nil, err
		}
		for i := 0; i < rs.GetRowSize(); i++ {
			rec, err := rs.GetRowValuesByIndex(i)
			if err != nil {
				continue
			}
			id := str(rec, 0)
			pv, err := rec.GetValueByIndex(1)
			if err != nil {
				continue
			}
			m, err := pv.AsMap()
			if err != nil {
				continue
			}
			props := map[string]any{}
			for k, v := range m {
				props[k] = plain(&v)
			}
			out[id] = props
		}
	}
	return out, nil
}

func label(id string, p map[string]any) string {
	pick := func(k string) string {
		if s, ok := p[k].(string); ok && s != "" {
			return s
		}
		return ""
	}
	switch Kind(id) {
	case TagUser:
		if n := pick("nickname"); n != "" {
			return n
		}
		if n := pick("name"); n != "" {
			return n
		}
		return "用户 " + strings.TrimPrefix(id[strings.LastIndexByte(id, ':')+1:], "")
	case TagAccount:
		return pick("name")
	case TagDevice:
		if m := pick("model"); m != "" {
			return strings.TrimSpace(pick("brand") + " " + m)
		}
		return strings.TrimPrefix(id, "d:")
	case TagIP:
		return strings.TrimPrefix(id, "ip:")
	case TagSubnet:
		return strings.TrimPrefix(id, "sn:")
	case TagPhone:
		return pick("masked")
	case TagEmail:
		return pick("masked")
	case TagIdentity:
		if n := pick("nickname"); n != "" {
			return n
		}
		return providerName(pick("provider")) + " 账号"
	case TagPayer:
		if m := pick("masked"); m != "" {
			return m
		}
		return pick("channel") + " 付款账号"
	case TagPasskey:
		if n := pick("name"); n != "" {
			return n
		}
		return "通行密钥"
	case TagClient:
		if n := pick("name"); n != "" {
			return n
		}
		return pick("client_id")
	}
	return id
}

// providerName 第三方登录渠道的显示名
func providerName(p string) string {
	switch strings.ToLower(p) {
	case "qq":
		return "QQ"
	case "google":
		return "Google"
	case "wechat", "weixin":
		return "微信"
	case "github":
		return "GitHub"
	case "apple":
		return "Apple"
	case "":
		return "第三方"
	}
	return p
}

func str(rec *nebula.Record, i int) string {
	v, err := rec.GetValueByIndex(i)
	if err != nil {
		return ""
	}
	s, _ := v.AsString()
	return s
}

func num(rec *nebula.Record, i int) int64 {
	v, err := rec.GetValueByIndex(i)
	if err != nil {
		return 0
	}
	n, _ := v.AsInt()
	return n
}

func plain(v *nebula.ValueWrapper) any {
	switch {
	case v.IsString():
		s, _ := v.AsString()
		return s
	case v.IsInt():
		n, _ := v.AsInt()
		return n
	case v.IsBool():
		b, _ := v.AsBool()
		return b
	case v.IsFloat():
		f, _ := v.AsFloat()
		return f
	case v.IsNull():
		return nil
	}
	return v.String()
}
