package graph

import (
	"fmt"
	"strings"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/proxyip"
)

// 边的 UPSERT：首次见到记 first_seen，之后只推 last_seen 与次数。
// at-least-once 投递下重放会让 count 多加，count 只作参考展示，判定都看 last_seen。
func upsertEdge(b *strings.Builder, edge, src, dst string, ts int64) {
	fmt.Fprintf(b, "UPSERT EDGE ON %s %s -> %s SET first_seen = CASE WHEN first_seen == 0 THEN %d ELSE first_seen END, last_seen = CASE WHEN last_seen < %d THEN %d ELSE last_seen END, count = count + 1;",
		edge, Quote(src), Quote(dst), ts, ts, ts)
}

func upsertVertex(b *strings.Builder, tag, vid string, ts int64, sets ...string) {
	all := append([]string{
		fmt.Sprintf("first_seen = CASE WHEN first_seen == 0 THEN %d ELSE first_seen END", ts),
		fmt.Sprintf("last_seen = CASE WHEN last_seen < %d THEN %d ELSE last_seen END", ts, ts),
	}, sets...)
	fmt.Fprintf(b, "UPSERT VERTEX ON %s %s SET %s;", tag, Quote(vid), strings.Join(all, ", "))
}

func setStr(k, v string) string { return k + " = " + Quote(v) }

// Statements 一个事件对应的图更新（一条 nGQL 里多个分号分隔的语句）
func Statements(e events.Event) string {
	ts := e.TS / 1000
	if ts <= 0 {
		return ""
	}
	var b strings.Builder

	var subject string
	switch {
	case e.UserID > 0:
		subject = UserVID(e.AppID, e.UserID)
		sets := []string{fmt.Sprintf("app_id = %d", e.AppID), fmt.Sprintf("uid = %d", e.UserID)}
		if e.Type == events.TypeDecision && e.Action != "" {
			sets = append(sets, setStr("last_action", e.Action), setStr("risk_level", e.Level))
		}
		if e.Account != "" {
			sets = append(sets, setStr("name", e.Account))
		}
		switch e.Type {
		case events.TypeAccountBan:
			sets = append(sets, "banned = true", setStr("status", "banned"))
		case events.TypeAccountUnban:
			sets = append(sets, "banned = false", setStr("status", "active"))
		}
		upsertVertex(&b, TagUser, subject, ts, sets...)
		if e.Account != "" {
			acc := AccountVID(e.AppID, e.Account)
			upsertVertex(&b, TagAccount, acc, ts, fmt.Sprintf("app_id = %d", e.AppID), setStr("name", e.Account))
			upsertEdge(&b, EdgeAccountOf, acc, subject, ts)
		}
	case e.Account != "":
		subject = AccountVID(e.AppID, e.Account)
		upsertVertex(&b, TagAccount, subject, ts, fmt.Sprintf("app_id = %d", e.AppID), setStr("name", e.Account))
	default:
		subject = ""
	}

	if e.DeviceID != "" {
		dv := DeviceVID(e.DeviceID)
		sets := []string{setStr("device_id", e.DeviceID)}
		for _, k := range []string{"platform", "brand", "model"} {
			if v := e.Device[k]; v != "" {
				sets = append(sets, setStr(k, v))
			}
		}
		if e.Device["emulator"] == "true" {
			sets = append(sets, "emulator = true")
		}
		if e.Device["rooted"] == "true" {
			sets = append(sets, "rooted = true")
		}
		upsertVertex(&b, TagDevice, dv, ts, sets...)
		if subject != "" {
			upsertEdge(&b, EdgeUsesDevice, subject, dv, ts)
		}
	}
	// 代理 / CDN / 内网地址不是用户的出口，不进图（否则会把大量无关用户连在一起）
	if e.IP != "" && !proxyip.IsProxy(e.IP) {
		iv := IPVID(e.IP)
		sets := []string{setStr("addr", e.IP)}
		if e.Country != "" {
			sets = append(sets, setStr("country", e.Country))
		}
		if e.Province != "" {
			sets = append(sets, setStr("province", e.Province))
		}
		if e.City != "" {
			sets = append(sets, setStr("city", e.City))
		}
		if e.ISP != "" {
			sets = append(sets, setStr("isp", e.ISP))
		}
		if e.ASN > 0 {
			sets = append(sets, fmt.Sprintf("asn = %d", e.ASN))
		}
		// 机房 / 云主机出口当场就能认出来；共享出口要按用户数统计，由每日回灌更新
		if proxyip.IsHosting(e.ISP, e.ASN) {
			sets = append(sets, "hosting = true")
		}
		upsertVertex(&b, TagIP, iv, ts, sets...)
		if subject != "" {
			upsertEdge(&b, EdgeUsesIP, subject, iv, ts)
		}
		if e.IPSubnet != "" {
			sv := SubnetVID(e.IPSubnet)
			upsertVertex(&b, TagSubnet, sv, ts, setStr("cidr", e.IPSubnet))
			upsertEdge(&b, EdgeInSubnet, iv, sv, ts)
		}
	}
	if e.UserID > 0 && e.Phone != "" {
		pv := PhoneVID(e.Phone)
		upsertVertex(&b, TagPhone, pv, ts, setStr("masked", MaskPhone(e.Phone)), setStr("prefix", e.PhonePrefix))
		upsertEdge(&b, EdgeHasPhone, subject, pv, ts)
	}
	if e.UserID > 0 && e.Email != "" {
		ev := EmailVID(e.Email)
		upsertVertex(&b, TagEmail, ev, ts, setStr("masked", MaskEmail(e.Email)), setStr("domain", e.EmailDomain))
		upsertEdge(&b, EdgeHasEmail, subject, ev, ts)
	}
	if e.UserID > 0 && e.TargetUserID > 0 && e.TargetUserID != e.UserID {
		target := UserVID(e.AppID, e.TargetUserID)
		upsertVertex(&b, TagUser, target, ts, fmt.Sprintf("app_id = %d", e.AppID), fmt.Sprintf("uid = %d", e.TargetUserID))
		edge := EdgeTargets
		if e.Type == events.TypeInviteBind {
			// 邀请绑定事件里 user 是被邀请人、target 是邀请人：边从邀请人指向被邀请人
			edge = EdgeInvited
			upsertEdge(&b, edge, target, subject, ts)
		} else {
			upsertEdge(&b, edge, subject, target, ts)
		}
	}
	return b.String()
}

// Apply 写入一批事件。按语句数分段提交：一次提交的语句太多，Nebula 的执行计划树会超过 512 层直接报错。
func (c *Client) Apply(evs []events.Event) error {
	var b strings.Builder
	n := 0
	flush := func() error {
		if n == 0 {
			return nil
		}
		_, err := c.Exec(b.String())
		b.Reset()
		n = 0
		return err
	}
	for _, e := range evs {
		if e.Shadow {
			continue
		}
		stmts := Statements(e)
		if stmts == "" {
			continue
		}
		k := strings.Count(stmts, ";")
		if n > 0 && n+k > MaxStatements {
			if err := flush(); err != nil {
				return err
			}
		}
		b.WriteString(stmts)
		n += k
	}
	return flush()
}

// SetBanned 标记封禁状态（设备 / IP 进黑名单、账号封禁时）
func (c *Client) SetBanned(vid string, banned bool) error {
	tag := Kind(vid)
	switch tag {
	case TagUser, TagDevice, TagIP:
	default:
		return nil
	}
	_, err := c.Exec(fmt.Sprintf("UPSERT VERTEX ON %s %s SET banned = %t;", tag, Quote(vid), banned))
	return err
}
