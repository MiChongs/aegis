package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 关联账号：一键解除 / 关联删除时，从一个用户出发找「同一个人或同一伙人」的其它账号。
//
// 只认两跳：用户 → 实体（设备、手机号、邮箱、第三方身份、付款账号、通行密钥、登录名、IP 线路）→ 其它用户，
// 外加用户之间的直接边（邀请、转赠目标）。不经过的实体：
//   - 网段：信息量太少；
//   - 第三方应用（OAuth2 客户端）：成千上万的人授权同一个应用，互不相干；
//   - 共享出口 / 机房出口的 IP：同一个运营商 NAT、同一家代理背后的人互不相干（与 ExpandWith 同一条规则）。
// 实体里只有 IP 线路算「弱关联」（家里同一条宽带的家人、同一个公司），其余都是「强关联」。

// RelatedReason 两个账号为什么被关联在一起
type RelatedReason struct {
	Kind  string `json:"kind"`  // device / phone / email / identity / payer / passkey / account / ip / invited / targets
	Label string `json:"label"` // 设备型号、打码的手机号、IP 线路……
	VID   string `json:"vid,omitempty"`
	Last  int64  `json:"lastSeen,omitempty"`
}

// RelatedUser 一个关联账号
type RelatedUser struct {
	UserID   int64           `json:"userId"`
	Name     string          `json:"name"`
	Banned   bool            `json:"banned"`
	Status   string          `json:"status,omitempty"`
	Strength string          `json:"strength"` // strong / weak
	Via      []int64         `json:"via"`      // 和哪几个起点账号关联
	Reasons  []RelatedReason `json:"reasons"`
	LastSeen int64           `json:"lastSeen"`
}

// RelatedOptions 查询参数
type RelatedOptions struct {
	Since int64 // 只看最近一次出现在这之后（秒）的关系；0 不限
	// PerEntity 每个实体最多带出多少个账号（一台被几百人共用的设备，带回几百人没有意义，按最近出现取前 N）
	PerEntity int
	Max       int // 最多返回多少个关联账号
}

// 不从这些实体继续找人
func skipEntity(kind string) bool {
	switch kind {
	case TagSubnet, TagClient, TagUser, "":
		return true
	}
	return false
}

// RelatedUsers 从一组用户（同一应用）出发找关联账号；起点账号本身不在结果里
func (c *Client) RelatedUsers(appID int64, userIDs []int64, o RelatedOptions) ([]RelatedUser, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	if o.PerEntity <= 0 {
		o.PerEntity = 30
	}
	if o.Max <= 0 {
		o.Max = 200
	}
	self := map[string]int64{}
	starts := make([]string, 0, len(userIDs))
	for _, uid := range userIDs {
		vid := UserVID(appID, uid)
		if _, ok := self[vid]; ok {
			continue
		}
		self[vid] = uid
		starts = append(starts, Quote(vid))
	}
	edges := relatedEdges()
	where := ""
	if o.Since > 0 {
		where = fmt.Sprintf(" WHERE properties(edge).last_seen >= %d", o.Since)
	}
	userPrefix := "u:" + strconv.FormatInt(appID, 10) + ":"

	type hit struct {
		uid     int64
		via     map[int64]bool
		reasons map[string]RelatedReason
		last    int64
	}
	hits := map[string]*hit{}
	touch := func(vid string, via int64, r RelatedReason) {
		if _, isSelf := self[vid]; isSelf || !strings.HasPrefix(vid, userPrefix) {
			return
		}
		uid, err := strconv.ParseInt(strings.TrimPrefix(vid, userPrefix), 10, 64)
		if err != nil || uid <= 0 {
			return
		}
		h := hits[vid]
		if h == nil {
			h = &hit{uid: uid, via: map[int64]bool{}, reasons: map[string]RelatedReason{}}
			hits[vid] = h
		}
		h.via[via] = true
		key := r.Kind + ">" + r.VID
		if old, ok := h.reasons[key]; !ok || r.Last > old.Last {
			h.reasons[key] = r
		}
		h.last = max(h.last, r.Last)
	}

	// 第一跳：起点 → 实体（以及直接连着的用户）
	rs, err := c.Exec(fmt.Sprintf(
		"GO FROM %s OVER %s BIDIRECT%s YIELD src(edge) AS s, dst(edge) AS d, type(edge) AS t, properties(edge).last_seen AS l",
		strings.Join(starts, ", "), edges, where))
	if err != nil {
		return nil, err
	}
	entityVia := map[string]map[int64]bool{}
	for i := 0; i < rs.GetRowSize(); i++ {
		rec, err := rs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		s, d, t, l := str(rec, 0), str(rec, 1), str(rec, 2), num(rec, 3)
		start, other := s, d
		if _, ok := self[s]; !ok {
			start, other = d, s
		}
		via, ok := self[start]
		if !ok {
			continue
		}
		if Kind(other) == TagUser {
			if t == EdgeInvited || t == EdgeTargets {
				touch(other, via, RelatedReason{Kind: t, Last: l})
			}
			continue
		}
		if skipEntity(Kind(other)) {
			continue
		}
		if entityVia[other] == nil {
			entityVia[other] = map[int64]bool{}
		}
		entityVia[other][via] = true
	}
	entities := make([]string, 0, len(entityVia))
	for id := range entityVia {
		entities = append(entities, id)
	}
	entities = c.dropWeakIPs(entities, "")
	if len(entities) > 0 {
		props, _ := c.fetchProps(entities)
		// 第二跳：实体 → 其它用户。按实体分批，每个实体只取最近出现的 PerEntity 个人
		for i := 0; i < len(entities); i += 40 {
			batch := entities[i:min(i+40, len(entities))]
			for _, ent := range batch {
				rows, err := c.Exec(fmt.Sprintf(
					"GO FROM %s OVER %s BIDIRECT%s YIELD src(edge) AS s, dst(edge) AS d, properties(edge).last_seen AS l | ORDER BY $-.l DESC | LIMIT %d",
					Quote(ent), edges, where, o.PerEntity+len(self)))
				if err != nil {
					return nil, err
				}
				r := RelatedReason{Kind: Kind(ent), Label: label(ent, props[ent]), VID: ent}
				for k := 0; k < rows.GetRowSize(); k++ {
					rec, err := rows.GetRowValuesByIndex(k)
					if err != nil {
						continue
					}
					s, d, l := str(rec, 0), str(rec, 1), num(rec, 2)
					other := s
					if other == ent {
						other = d
					}
					rr := r
					rr.Last = l
					for via := range entityVia[ent] {
						touch(other, via, rr)
					}
				}
			}
		}
	}

	out := make([]RelatedUser, 0, len(hits))
	ids := make([]string, 0, len(hits))
	for vid, h := range hits {
		ids = append(ids, vid)
		ru := RelatedUser{UserID: h.uid, Strength: "weak", LastSeen: h.last}
		for via := range h.via {
			ru.Via = append(ru.Via, via)
		}
		sort.Slice(ru.Via, func(a, b int) bool { return ru.Via[a] < ru.Via[b] })
		for _, r := range h.reasons {
			if r.Kind != TagIP {
				ru.Strength = "strong"
			}
			ru.Reasons = append(ru.Reasons, r)
		}
		sort.Slice(ru.Reasons, func(a, b int) bool {
			if ra, rb := reasonRank(ru.Reasons[a].Kind), reasonRank(ru.Reasons[b].Kind); ra != rb {
				return ra < rb
			}
			return ru.Reasons[a].Last > ru.Reasons[b].Last
		})
		out = append(out, ru)
	}
	if props, err := c.fetchProps(ids); err == nil {
		for i := range out {
			p := props[UserVID(appID, out[i].UserID)]
			if p == nil {
				continue
			}
			out[i].Name = label(UserVID(appID, out[i].UserID), p)
			out[i].Banned = p["banned"] == true
			out[i].Status, _ = p["status"].(string)
		}
	}
	// 强关联在前；同强度按关联理由多少、最近出现排序
	sort.Slice(out, func(a, b int) bool {
		if (out[a].Strength == "strong") != (out[b].Strength == "strong") {
			return out[a].Strength == "strong"
		}
		if len(out[a].Reasons) != len(out[b].Reasons) {
			return len(out[a].Reasons) > len(out[b].Reasons)
		}
		return out[a].LastSeen > out[b].LastSeen
	})
	if len(out) > o.Max {
		out = out[:o.Max]
	}
	return out, nil
}

// relatedEdges 找关联账号时走的边（不含网段与第三方应用授权）
func relatedEdges() string {
	var kinds []string
	for _, e := range Edges() {
		if e == EdgeInSubnet || e == EdgeAuthorized {
			continue
		}
		kinds = append(kinds, e)
	}
	return strings.Join(kinds, ", ")
}

func reasonRank(kind string) int {
	switch kind {
	case TagDevice:
		return 0
	case TagPhone, TagEmail, TagIdentity:
		return 1
	case TagPayer, TagPasskey, TagAccount:
		return 2
	case EdgeInvited, EdgeTargets:
		return 3
	}
	return 4
}

// Entities 一个用户直接连着的实体（设备、IP 线路、手机号……），用于列出它身上的名单与处置对象
func (c *Client) Entities(appID, userID int64, since int64) ([]Node, error) {
	where := ""
	if since > 0 {
		where = fmt.Sprintf(" WHERE properties(edge).last_seen >= %d", since)
	}
	center := UserVID(appID, userID)
	rs, err := c.Exec(fmt.Sprintf(
		"GO FROM %s OVER %s BIDIRECT%s YIELD src(edge) AS s, dst(edge) AS d, properties(edge).last_seen AS l | ORDER BY $-.l DESC | LIMIT 300",
		Quote(center), relatedEdges(), where))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for i := 0; i < rs.GetRowSize(); i++ {
		rec, err := rs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		other := str(rec, 0)
		if other == center {
			other = str(rec, 1)
		}
		if k := Kind(other); k == TagUser || k == "" || seen[other] {
			continue
		}
		seen[other] = true
		ids = append(ids, other)
	}
	props, err := c.fetchProps(ids)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(ids))
	for _, id := range ids {
		p := props[id]
		if p == nil {
			p = map[string]any{}
		}
		out = append(out, Node{ID: id, Kind: Kind(id), Label: label(id, p), Props: p, Depth: 1})
	}
	return out, nil
}

// MarkUserDeleted 账号已删除：点留着（同设备、同手机号的新账号仍能关联到它），状态记为 deleted 并按封禁计入图特征
func (c *Client) MarkUserDeleted(appID, userID int64) error {
	_, err := c.Exec(fmt.Sprintf("UPSERT VERTEX ON user %s SET status = \"deleted\", banned = true;", Quote(UserVID(appID, userID))))
	return err
}

// MarkUserReleased 一键解除：用户点的封禁标记清掉，状态回到 active
func (c *Client) MarkUserReleased(appID, userID int64) error {
	_, err := c.Exec(fmt.Sprintf("UPSERT VERTEX ON user %s SET status = \"active\", banned = false;", Quote(UserVID(appID, userID))))
	return err
}
