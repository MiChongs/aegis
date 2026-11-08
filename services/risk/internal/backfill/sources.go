package backfill

import (
	"context"
	"fmt"
	"strings"

	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/proxyip"
	"aegis/services/risk/internal/vector"

	"github.com/jackc/pgx/v5"
)

// epoch SQL 片段：时间戳转秒
func epoch(col string) string { return fmt.Sprintf("coalesce(extract(epoch from %s)::bigint, 0)", col) }

var sources = []source{
	{key: "users", label: "用户与资料", detail: "账号、昵称、邮箱、手机号、注册 IP、账号状态", run: srcUsers},
	{key: "register", label: "注册记录", detail: "注册时的 IP、设备与地区", run: srcRegister},
	{key: "login_audit", label: "登录记录", detail: "每次登录的 IP 与设备", run: srcLoginAudit},
	{key: "sessions", label: "会话活动", detail: "令牌签发、刷新与访问时的 IP", run: srcSessions},
	{key: "login_geo", label: "登录地理", detail: "登录 IP 的国家、地区与运营商", run: srcLoginGeo},
	{key: "login_devices", label: "登录设备", detail: "设备型号、系统版本、首次与最近登录", run: srcLoginDevices},
	{key: "fingerprints", label: "浏览器与设备指纹", detail: "网页端设备、浏览器与指纹标签", run: srcFingerprints},
	{key: "installs", label: "客户端安装", detail: "安装包所在设备与上报 IP", run: srcInstalls},
	{key: "oauth", label: "第三方登录绑定", detail: "QQ、Google 等第三方身份（同一身份跨应用相连）", run: srcOAuth},
	{key: "passkeys", label: "通行密钥", detail: "用户登记的通行密钥", run: srcPasskeys},
	{key: "invites", label: "邀请关系", detail: "谁邀请了谁，以及被邀请时的 IP 与设备", run: srcInvites},
	{key: "payments", label: "支付订单", detail: "下单 IP 与付款账号", run: srcPayments},
	{key: "activity", label: "业务活动", detail: "抽奖、签到、积分兑换、激励广告、卡密、AI 调用里的 IP 与设备", run: srcActivity},
	{key: "ip_intel", label: "IP 情报", detail: "代理、VPN、机房 IP 与运营商", run: srcIPIntel},
	{key: "finalize", label: "写入 IP、设备与相似向量", detail: "IP 点与所属网段、设备点、设备相似度向量", run: srcFinalize},
}

func srcUsers(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT u.appid, u.id, coalesce(u.account, ''), u.enabled, (u.disabled_end_time IS NULL OR u.disabled_end_time > now()) AS ban_active,
		` + epoch("u.created_at") + `, ` + epoch("coalesce(u.updated_at, u.created_at)") + `,
		coalesce(p.nickname, ''), coalesce(p.email, ''), coalesce(p.phone, ''), coalesce(p.register_ip, ''),
		coalesce(p.register_province, ''), coalesce(p.register_city, ''), coalesce(p.register_isp, ''),
		` + epoch("coalesce(p.register_time, u.created_at)") + `
		FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id ORDER BY u.id`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var (
			appID, uid, created, updated, regAt                 int64
			account, nick, email, phone, regIP, prov, city, isp string
			enabled, banActive                                  bool
		)
		if err := row.Scan(&appID, &uid, &account, &enabled, &banActive, &created, &updated, &nick, &email, &phone, &regIP, &prov, &city, &isp, &regAt); err != nil {
			return err
		}
		banned := !enabled && banActive
		status := "active"
		if banned {
			status = "banned"
		} else if !enabled {
			status = "disabled"
		}
		r.prog.Users++
		if err := r.user(appID, uid, created, updated, graph.Set("name", account), graph.Set("nickname", nick),
			graph.SetInt("registered_at", regAt), graph.Set("status", status), graph.SetBool("banned", banned)); err != nil {
			return err
		}
		uv := graph.UserVID(appID, uid)
		if account != "" {
			av := graph.AccountVID(appID, account)
			if err := r.batch.Vertex(graph.TagAccount, av, created, updated, graph.SetInt("app_id", appID), graph.Set("name", account)); err != nil {
				return err
			}
			if err := r.batch.Edge(graph.EdgeAccountOf, av, uv, created, updated, 1); err != nil {
				return err
			}
		}
		if email = strings.TrimSpace(email); strings.Contains(email, "@") {
			ev := graph.EmailVID(email)
			if err := r.batch.Vertex(graph.TagEmail, ev, created, updated, graph.Set("masked", graph.MaskEmail(email)), graph.Set("domain", engine.EmailDomain(email))); err != nil {
				return err
			}
			if err := r.batch.Edge(graph.EdgeHasEmail, uv, ev, created, updated, 1); err != nil {
				return err
			}
		}
		if phone = strings.TrimSpace(phone); len(phone) >= 7 {
			pv := graph.PhoneVID(phone)
			if err := r.batch.Vertex(graph.TagPhone, pv, created, updated, graph.Set("masked", graph.MaskPhone(phone)), graph.Set("prefix", engine.PhonePrefix(phone))); err != nil {
				return err
			}
			if err := r.batch.Edge(graph.EdgeHasPhone, uv, pv, created, updated, 1); err != nil {
				return err
			}
		}
		if ip := r.clientIP(regIP); ip != "" {
			info := r.noteIP(ip, regAt, regAt)
			info.addUser(appID, uid)
			if info.province == "" {
				info.province, info.city, info.isp = prov, city, isp
			}
			return r.batch.Edge(graph.EdgeRegisteredFrom, uv, graph.IPVID(ip), regAt, regAt, 1)
		}
		return nil
	})
}

func srcRegister(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT appid, coalesce(user_id, 0), coalesce(host(ip), ''), coalesce(device_id, ''), coalesce(country, ''), coalesce(province, ''),
		coalesce(city, ''), coalesce(isp, ''), ` + epoch("created_at") + ` FROM register_ip_ledger WHERE coalesce(user_id, 0) > 0`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var (
			appID, uid, ts                    int64
			ip, dev, country, prov, city, isp string
		)
		if err := row.Scan(&appID, &uid, &ip, &dev, &country, &prov, &city, &isp, &ts); err != nil {
			return err
		}
		if ip = r.clientIP(ip); ip != "" {
			info := r.noteIP(ip, ts, ts)
			info.addUser(appID, uid)
			if info.country == "" {
				info.country = country
			}
			if info.province == "" {
				info.province, info.city, info.isp = prov, city, isp
			}
			if err := r.batch.Edge(graph.EdgeRegisteredFrom, graph.UserVID(appID, uid), graph.IPVID(ip), ts, ts, 1); err != nil {
				return err
			}
		}
		return r.useDevice(graph.EdgeRegisteredOn, appID, uid, dev, ts, ts, 1, nil)
	})
}

func srcLoginAudit(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	n1, err := query(ctx, conn, `SELECT appid, user_id, host(login_ip), `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`, count(*)
		FROM login_audit_logs WHERE user_id > 0 AND login_ip IS NOT NULL GROUP BY 1, 2, 3`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var ip string
		if err := row.Scan(&appID, &uid, &ip, &first, &last, &n); err != nil {
			return err
		}
		return r.useIP(graph.EdgeUsesIP, appID, uid, ip, first, last, n)
	})
	if err != nil {
		return n1, err
	}
	n2, err := query(ctx, conn, `SELECT appid, user_id, device_id, `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`, count(*)
		FROM login_audit_logs WHERE user_id > 0 AND coalesce(device_id, '') <> '' GROUP BY 1, 2, 3`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var dev string
		if err := row.Scan(&appID, &uid, &dev, &first, &last, &n); err != nil {
			return err
		}
		return r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, nil)
	})
	return n1 + n2, err
}

func srcSessions(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	n1, err := query(ctx, conn, `SELECT appid, user_id, metadata->>'ip', `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`, count(*)
		FROM session_audit_logs WHERE user_id > 0 AND coalesce(metadata->>'ip', '') <> '' GROUP BY 1, 2, 3`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var ip string
		if err := row.Scan(&appID, &uid, &ip, &first, &last, &n); err != nil {
			return err
		}
		return r.useIP(graph.EdgeUsesIP, appID, uid, ip, first, last, n)
	})
	if err != nil {
		return n1, err
	}
	n2, err := query(ctx, conn, `SELECT appid, user_id, metadata->>'device_id', `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`, count(*)
		FROM session_audit_logs WHERE user_id > 0 AND coalesce(metadata->>'device_id', '') <> '' GROUP BY 1, 2, 3`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var dev string
		if err := row.Scan(&appID, &uid, &dev, &first, &last, &n); err != nil {
			return err
		}
		return r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, nil)
	})
	return n1 + n2, err
}

func srcLoginGeo(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	// 分区父表：各月份分区一起查
	n1, err := query(ctx, conn, `SELECT host(ip), max(coalesce(country, '')), max(coalesce(region, '')), max(coalesce(city, '')), max(coalesce(isp, '')),
		max(coalesce(asn, '')), `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`
		FROM login_geo_events WHERE ip IS NOT NULL GROUP BY 1`, func(row pgx.Rows) error {
		var ip, country, region, city, isp, asn string
		var first, last int64
		if err := row.Scan(&ip, &country, &region, &city, &isp, &asn, &first, &last); err != nil {
			return err
		}
		if ip = r.clientIP(ip); ip == "" {
			return nil
		}
		info := r.noteIP(ip, first, last)
		if country != "" {
			info.country = country
		}
		if region != "" {
			info.province = region
		}
		if city != "" {
			info.city = city
		}
		if isp != "" {
			info.isp = isp
		}
		if a := parseASN(asn); a > 0 {
			info.asn = a
		}
		return nil
	})
	if err != nil {
		return n1, err
	}
	n2, err := query(ctx, conn, `SELECT app_id, user_id, host(ip), coalesce(device_id, ''), `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`, count(*)
		FROM login_geo_events WHERE user_id > 0 AND ip IS NOT NULL GROUP BY 1, 2, 3, 4`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var ip, dev string
		if err := row.Scan(&appID, &uid, &ip, &dev, &first, &last, &n); err != nil {
			return err
		}
		if err := r.useIP(graph.EdgeUsesIP, appID, uid, ip, first, last, n); err != nil {
			return err
		}
		return r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, nil)
	})
	return n1 + n2, err
}

func srcLoginDevices(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT appid, user_id, device_key, coalesce(platform, ''), coalesce(model, ''), coalesce(name, ''),
		coalesce(extra->>'brand', extra->>'manufacturer', ''), coalesce(extra->>'osVersion', ''), coalesce(extra->>'appVersion', ''),
		coalesce(extra->>'os', ''), coalesce(last_ip, ''), ` + epoch("first_seen_at") + `, ` + epoch("last_seen_at") + `, coalesce(login_count, 1)
		FROM user_login_devices WHERE coalesce(device_key, '') <> ''`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var (
			appID, uid, first, last, n                                   int64
			dev, platform, model, name, brand, osv, appv, osName, lastIP string
		)
		if err := row.Scan(&appID, &uid, &dev, &platform, &model, &name, &brand, &osv, &appv, &osName, &lastIP, &first, &last, &n); err != nil {
			return err
		}
		if platform == "" {
			platform = osName
		}
		attrs := map[string]string{"platform": lowerPlatform(platform), "model": model, "brand": brand, "os_version": osv, "app_version": appv, "name": name}
		if err := r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, attrs); err != nil {
			return err
		}
		return r.useIP(graph.EdgeUsesIP, appID, uid, lastIP, last, last, 1)
	})
}

func srcFingerprints(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT coalesce(app_id, 0), coalesce(user_id, 0), device_id, coalesce(fingerprint->>'uaOs', ''), coalesce(fingerprint->>'uaOsVersion', ''),
		coalesce(fingerprint->>'uaBrowser', ''), coalesce(fingerprint->>'uaDevice', ''), coalesce(risk_tag, ''), coalesce(last_ip, ''),
		` + epoch("first_seen_at") + `, ` + epoch("last_seen_at") + `, coalesce(seen_count, 1)
		FROM device_fingerprints WHERE coalesce(device_id, '') <> ''`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var (
			appID, uid, first, last, n                    int64
			dev, uaOS, uaOSV, browser, uaDev, tag, lastIP string
		)
		if err := row.Scan(&appID, &uid, &dev, &uaOS, &uaOSV, &browser, &uaDev, &tag, &lastIP, &first, &last, &n); err != nil {
			return err
		}
		// 网页端：「Chrome（Windows）」这样的名字
		name := browser
		if uaOS != "" {
			if name == "" {
				name = uaOS
			} else {
				name += "（" + uaOS + "）"
			}
		}
		attrs := map[string]string{"platform": lowerPlatform(uaOS), "os_version": uaOSV, "model": uaDev, "name": name}
		if err := r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, attrs); err != nil {
			return err
		}
		switch strings.ToLower(tag) {
		case "block", "blocked", "blacklist", "banned":
			r.noteDevice(appID, uid, dev, first, last, nil).banned = true
		}
		return r.useIP(graph.EdgeUsesIP, appID, uid, lastIP, last, last, 1)
	})
}

func srcInstalls(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT appid, coalesce(user_id, 0), device_id, coalesce(platform, ''), coalesce(model, ''), coalesce(brand, ''),
		coalesce(os_version::text, ''), coalesce(version_name, ''), coalesce(ip, ''), ` + epoch("first_seen_at") + `, ` + epoch("last_seen_at") + `, coalesce(report_count, 1)
		FROM app_client_installs WHERE coalesce(device_id, '') <> ''`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var (
			appID, uid, first, last, n                int64
			dev, platform, model, brand, osv, ver, ip string
		)
		if err := row.Scan(&appID, &uid, &dev, &platform, &model, &brand, &osv, &ver, &ip, &first, &last, &n); err != nil {
			return err
		}
		if osv != "" && lowerPlatform(platform) == "android" {
			osv = "API " + osv
		}
		attrs := map[string]string{"platform": lowerPlatform(platform), "model": model, "brand": brand, "os_version": osv, "app_version": ver}
		if err := r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, attrs); err != nil {
			return err
		}
		return r.useIP(graph.EdgeUsesIP, appID, uid, ip, last, last, 1)
	})
}

func srcOAuth(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT appid, user_id, lower(provider), coalesce(nullif(union_id, ''), provider_user_id, ''),
		coalesce(raw_profile->>'nickname', raw_profile->>'name', ''), coalesce(raw_profile->>'province', ''), coalesce(raw_profile->>'city', ''),
		` + epoch("created_at") + `, ` + epoch("coalesce(updated_at, created_at)") + `
		FROM oauth_bindings WHERE user_id > 0`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var (
			appID, uid, first, last             int64
			provider, subject, nick, prov, city string
		)
		if err := row.Scan(&appID, &uid, &provider, &subject, &nick, &prov, &city, &first, &last); err != nil {
			return err
		}
		if subject == "" || provider == "" {
			return nil
		}
		iv := graph.IdentityVID(provider, subject)
		region := strings.TrimSpace(prov + " " + city)
		if err := r.batch.Vertex(graph.TagIdentity, iv, first, last, graph.Sets(graph.Set("provider", provider), graph.Set("nickname", nick), graph.Set("region", region))...); err != nil {
			return err
		}
		return r.batch.Edge(graph.EdgeBoundIdentity, graph.UserVID(appID, uid), iv, first, last, 1)
	})
}

func srcPasskeys(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT appid, user_id, encode(credential_id, 'hex'), coalesce(credential_name, ''), ` + epoch("created_at") + `,
		` + epoch("coalesce(last_used_at, updated_at, created_at)") + `, coalesce(sign_count, 0)
		FROM user_passkeys WHERE user_id > 0`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var cred, name string
		if err := row.Scan(&appID, &uid, &cred, &name, &first, &last, &n); err != nil {
			return err
		}
		pv := graph.PasskeyVID(cred)
		if err := r.batch.Vertex(graph.TagPasskey, pv, first, last, graph.Sets(graph.Set("name", name))...); err != nil {
			return err
		}
		return r.batch.Edge(graph.EdgeHasPasskey, graph.UserVID(appID, uid), pv, first, last, max(n, 1))
	})
}

func srcInvites(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT appid, inviter_id, invitee_id, coalesce(device_id, ''), coalesce(client_ip, ''), ` + epoch("created_at") + `
		FROM invite_relations WHERE inviter_id > 0 AND invitee_id > 0`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var appID, inviter, invitee, ts int64
		var dev, ip string
		if err := row.Scan(&appID, &inviter, &invitee, &dev, &ip, &ts); err != nil {
			return err
		}
		if err := r.user(appID, inviter, ts, ts); err != nil {
			return err
		}
		if err := r.batch.Edge(graph.EdgeInvited, graph.UserVID(appID, inviter), graph.UserVID(appID, invitee), ts, ts, 1); err != nil {
			return err
		}
		if err := r.useDevice(graph.EdgeUsesDevice, appID, invitee, dev, ts, ts, 1, nil); err != nil {
			return err
		}
		return r.useIP(graph.EdgeUsesIP, appID, invitee, ip, ts, ts, 1)
	})
}

func srcPayments(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	n1, err := query(ctx, conn, `SELECT appid, user_id, client_ip, `+epoch("min(created_at)")+`, `+epoch("max(created_at)")+`, count(*)
		FROM payment_orders WHERE user_id > 0 AND coalesce(client_ip, '') <> '' GROUP BY 1, 2, 3`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var ip string
		if err := row.Scan(&appID, &uid, &ip, &first, &last, &n); err != nil {
			return err
		}
		return r.useIP(graph.EdgeUsesIP, appID, uid, ip, first, last, n)
	})
	if err != nil {
		return n1, err
	}
	// 能认出付款人的渠道：爱发电回调里带付款用户
	n2, err := query(ctx, conn, `SELECT appid, user_id, 'afdian', raw_callback #>> '{order,user_id}', `+epoch("min(coalesce(paid_at, created_at))")+`,
		`+epoch("max(coalesce(paid_at, created_at))")+`, count(*)
		FROM payment_orders WHERE user_id > 0 AND jsonb_typeof(raw_callback) = 'object' AND coalesce(raw_callback #>> '{order,user_id}', '') <> ''
		GROUP BY 1, 2, 3, 4`, func(row pgx.Rows) error {
		var appID, uid, first, last, n int64
		var channel, account string
		if err := row.Scan(&appID, &uid, &channel, &account, &first, &last, &n); err != nil {
			return err
		}
		pv := graph.PayerVID(channel, account)
		if err := r.batch.Vertex(graph.TagPayer, pv, first, last, graph.Set("channel", channel), graph.Set("masked", mask(account))); err != nil {
			return err
		}
		return r.batch.Edge(graph.EdgePaidWith, graph.UserVID(appID, uid), pv, first, last, n)
	})
	return n1 + n2, err
}

// activityTables 带 IP / 设备的业务流水表
var activityTables = []struct{ table, app, ip, device, ts string }{
	{"lottery_draws", "appid", "client_ip", "device_id", "created_at"},
	{"daily_signins", "appid", "ip_address", "", "created_at"},
	{"points_store_redemptions", "appid", "client_ip", "device_id", "created_at"},
	{"app_rewarded_ad_views", "appid", "client_ip", "device_id", "created_at"},
	{"card_key_redemptions", "appid", "client_ip", "device_id", "created_at"},
	{"ai_gateway_requests", "appid", "client_ip", "", "created_at"},
}

func srcActivity(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	var total int64
	missing := 0
	for _, t := range activityTables {
		n, err := query(ctx, conn, fmt.Sprintf(`SELECT %s, user_id, %s, %s, %s, count(*) FROM %s WHERE user_id > 0 AND coalesce(%s, '') <> '' GROUP BY 1, 2, 3`,
			t.app, t.ip, epoch("min("+t.ts+")"), epoch("max("+t.ts+")"), t.table, t.ip), func(row pgx.Rows) error {
			var appID, uid, first, last, n int64
			var ip string
			if err := row.Scan(&appID, &uid, &ip, &first, &last, &n); err != nil {
				return err
			}
			return r.useIP(graph.EdgeUsesIP, appID, uid, ip, first, last, n)
		})
		total += n
		if err == errMissingTable {
			missing++
			continue
		}
		if err != nil {
			return total, fmt.Errorf("%s：%w", t.table, err)
		}
		if t.device == "" {
			continue
		}
		n, err = query(ctx, conn, fmt.Sprintf(`SELECT %s, user_id, %s, %s, %s, count(*) FROM %s WHERE user_id > 0 AND coalesce(%s, '') <> '' GROUP BY 1, 2, 3`,
			t.app, t.device, epoch("min("+t.ts+")"), epoch("max("+t.ts+")"), t.table, t.device), func(row pgx.Rows) error {
			var appID, uid, first, last, n int64
			var dev string
			if err := row.Scan(&appID, &uid, &dev, &first, &last, &n); err != nil {
				return err
			}
			return r.useDevice(graph.EdgeUsesDevice, appID, uid, dev, first, last, n, nil)
		})
		total += n
		if err != nil && err != errMissingTable {
			return total, fmt.Errorf("%s：%w", t.table, err)
		}
	}
	if missing == len(activityTables) {
		return 0, errMissingTable
	}
	return total, nil
}

func srcIPIntel(ctx context.Context, conn *pgx.Conn, r *Runner) (int64, error) {
	sql := `SELECT ip, coalesce(country, ''), coalesce(region, ''), coalesce(isp, ''), coalesce(asn, ''), coalesce(is_proxy, false), coalesce(is_vpn, false),
		coalesce(is_tor, false), coalesce(is_datacenter, false), ` + epoch("first_seen_at") + `, ` + epoch("last_seen_at") + ` FROM ip_risk_records`
	return query(ctx, conn, sql, func(row pgx.Rows) error {
		var ip, country, region, isp, asn string
		var proxy, vpn, tor, dc bool
		var first, last int64
		if err := row.Scan(&ip, &country, &region, &isp, &asn, &proxy, &vpn, &tor, &dc, &first, &last); err != nil {
			return err
		}
		ip = r.clientIP(ip)
		info, ok := r.ips[ip]
		if ip == "" || !ok { // 只补图上已有（有用户用过）的 IP，不把整张情报表搬进图
			return nil
		}
		info.hasRiskRow = true
		info.proxy, info.vpn, info.tor, info.dc = proxy, vpn, tor, dc
		if info.country == "" {
			info.country = country
		}
		if info.province == "" {
			info.province = region
		}
		if info.isp == "" {
			info.isp = isp
		}
		if a := parseASN(asn); a > 0 && info.asn == 0 {
			info.asn = a
		}
		return nil
	})
}

// srcFinalize 删掉图里的代理 / CDN 地址，再写 IP 点（带网段）、设备点与设备向量
func srcFinalize(ctx context.Context, _ *pgx.Conn, r *Runner) (int64, error) {
	var n int64
	if err := r.purgeProxies(); err != nil {
		return n, err
	}
	for ip, info := range r.ips {
		sets := graph.Sets(graph.Set("addr", ip), graph.Set("country", info.country), graph.Set("province", info.province),
			graph.Set("city", info.city), graph.Set("isp", info.isp))
		if info.asn > 0 {
			sets = append(sets, graph.SetInt("asn", info.asn))
		}
		if info.hasRiskRow {
			sets = append(sets, graph.SetBool("proxy", info.proxy), graph.SetBool("vpn", info.vpn), graph.SetBool("tor", info.tor), graph.SetBool("datacenter", info.dc))
		}
		users := int64(len(info.users))
		hosting := proxyip.IsHosting(info.isp, info.asn) || info.proxy || info.vpn || info.tor || info.dc
		shared := users >= proxyip.SharedUsers
		sets = append(sets, graph.SetInt("users", users), graph.SetBool("hosting", hosting), graph.SetBool("shared", shared))
		if hosting {
			r.prog.HostingIPs++
		}
		if shared {
			r.prog.SharedIPs++
		}
		if err := r.batch.Vertex(graph.TagIP, graph.IPVID(ip), info.first, info.last, sets...); err != nil {
			return n, err
		}
		if sn := engine.Subnet(ip); sn != "" {
			sv := graph.SubnetVID(sn)
			if err := r.batch.Vertex(graph.TagSubnet, sv, info.first, info.last, graph.Set("cidr", sn)); err != nil {
				return n, err
			}
			if err := r.batch.Edge(graph.EdgeInSubnet, graph.IPVID(ip), sv, info.first, info.last, 1); err != nil {
				return n, err
			}
		}
		n++
	}
	var vecs []vector.Device
	flushVec := func() error {
		if r.Vector == nil || len(vecs) == 0 {
			vecs = vecs[:0]
			return nil
		}
		err := r.Vector.Upsert(ctx, vecs)
		r.prog.Vectors += int64(len(vecs))
		vecs = vecs[:0]
		return err
	}
	for _, d := range r.devices {
		a := d.attrs
		sets := graph.Sets(graph.Set("device_id", d.id), graph.Set("platform", a["platform"]), graph.Set("brand", a["brand"]),
			graph.Set("model", a["model"]), graph.Set("os_version", a["os_version"]), graph.Set("app_version", a["app_version"]), graph.Set("name", a["name"]))
		if d.banned {
			sets = append(sets, graph.SetBool("banned", true))
		}
		if err := r.batch.Vertex(graph.TagDevice, graph.DeviceVID(d.id), d.first, d.last, sets...); err != nil {
			return n, err
		}
		n++
		// 没有任何型号 / 系统要素的设备不建向量（全零向量和谁都不像，只会占地方）
		if vec := vector.Embed(a); !vector.Empty(vec) {
			vecs = append(vecs, vector.Device{AppID: d.appID, DeviceID: d.id, UserID: d.userID, Banned: d.banned, UpdatedAt: d.last, Vec: vec})
			if len(vecs) >= 500 {
				if err := flushVec(); err != nil {
					return n, err
				}
			}
		}
	}
	return n, flushVec()
}

func parseASN(s string) int64 {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "AS"))
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// purgeProxies 删除代理 / CDN / 内网地址的 IP 点（连同它们的边），以及只由这些地址组成的网段点。
// 这些地址是历史上被误记成用户 IP 的，留在图上会把大量无关用户连在一起。
func (r *Runner) purgeProxies() error {
	if len(r.proxies) == 0 {
		return nil
	}
	if err := r.batch.Flush(); err != nil {
		return err
	}
	userSubnets := map[string]bool{}
	for ip := range r.ips {
		userSubnets[engine.Subnet(ip)] = true
	}
	var vids []string
	subnets := map[string]bool{}
	for ip := range r.proxies {
		vids = append(vids, graph.Quote(graph.IPVID(ip)))
		if sn := engine.Subnet(ip); sn != "" && !userSubnets[sn] && !subnets[sn] {
			subnets[sn] = true
			vids = append(vids, graph.Quote(graph.SubnetVID(sn)))
		}
	}
	for i := 0; i < len(vids); i += 50 {
		if _, err := r.Graph.Exec("DELETE VERTEX " + strings.Join(vids[i:min(i+50, len(vids))], ", ") + " WITH EDGE"); err != nil {
			return err
		}
	}
	r.prog.ProxyIPs = int64(len(r.proxies))
	if r.Logf != nil {
		r.Logf("关系图回灌：删除代理 / CDN 地址 %d 个、网段 %d 个", len(r.proxies), len(subnets))
	}
	return nil
}
