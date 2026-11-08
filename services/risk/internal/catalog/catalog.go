// Package catalog 内置场景、起步特征与规则模板。
//
// 内置场景对应主服务的接入点（Guard 调用处），首次迁移时写入；之后完全由控制台编辑，迁移不覆盖。
// 特征同理。规则模板不自动装：控制台上按场景一键安装，装完就是普通规则。
package catalog

import (
	"context"
	"errors"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/store"
)

func vel(key, name, entity string, window int) store.Feature {
	return store.Feature{Feature: bundle.Feature{Key: key, Name: name, Kind: bundle.FeatureVelocity, Entity: entity,
		Velocity: &bundle.VelocityDef{WindowSeconds: window, Agg: bundle.AggCount}}}
}

func distinct(key, name, entity, of string, window int, all bool) store.Feature {
	return store.Feature{Feature: bundle.Feature{Key: key, Name: name, Kind: bundle.FeatureVelocity, Entity: entity,
		Velocity: &bundle.VelocityDef{WindowSeconds: window, Agg: bundle.AggDistinct, DistinctOf: of, AllScenes: all}}}
}

func stream(key, name, entity string, events []string, agg, field string, window int, conds ...bundle.Condition) store.Feature {
	return store.Feature{Feature: bundle.Feature{Key: key, Name: name, Kind: bundle.FeatureStream, Entity: entity,
		Stream: &bundle.StreamDef{Events: events, Agg: agg, Field: field, WindowSeconds: window, Conditions: conds}}}
}

func graphF(key, name, entity, tpl string, days int) store.Feature {
	return store.Feature{Feature: bundle.Feature{Key: key, Name: name, Kind: bundle.FeatureGraph, Entity: entity,
		Graph: &bundle.GraphDef{Template: tpl, WithinDays: days, CacheTTL: 120}}}
}

func derived(key, name, expr string) store.Feature {
	return store.Feature{Feature: bundle.Feature{Key: key, Name: name, Kind: bundle.FeatureDerived, Expr: expr}}
}

// Features 起步特征
func Features() []store.Feature {
	blocked := bundle.Condition{Field: "action", Op: "in", Value: []any{"block", "ban"}}
	return []store.Feature{
		vel("ip_scene_1m", "IP 1 分钟请求数", bundle.EntityIP, 60),
		vel("ip_scene_10m", "IP 10 分钟请求数", bundle.EntityIP, 600),
		vel("ip_scene_1h", "IP 1 小时请求数", bundle.EntityIP, 3600),
		vel("account_scene_10m", "账号 10 分钟请求数", bundle.EntityAccount, 600),
		vel("account_scene_1h", "账号 1 小时请求数", bundle.EntityAccount, 3600),
		vel("device_scene_10m", "设备 10 分钟请求数", bundle.EntityDevice, 600),
		vel("device_scene_1h", "设备 1 小时请求数", bundle.EntityDevice, 3600),
		vel("user_scene_1h", "用户 1 小时请求数", bundle.EntityUser, 3600),
		vel("user_scene_1d", "用户 1 天请求数", bundle.EntityUser, 86400),
		vel("phone_scene_1h", "手机号 1 小时请求数", bundle.EntityPhone, 3600),
		distinct("ip_accounts_1h", "IP 1 小时出现的账号数", bundle.EntityIP, bundle.EntityAccount, 3600, true),
		distinct("device_accounts_1h", "设备 1 小时出现的账号数", bundle.EntityDevice, bundle.EntityAccount, 3600, true),
		distinct("device_accounts_1d", "设备 1 天出现的账号数", bundle.EntityDevice, bundle.EntityAccount, 86400, true),
		distinct("account_devices_1d", "账号 1 天用过的设备数", bundle.EntityAccount, bundle.EntityDevice, 86400, true),
		distinct("account_ips_1d", "账号 1 天用过的 IP 数", bundle.EntityAccount, bundle.EntityIP, 86400, true),
		distinct("subnet_accounts_1h", "网段 1 小时出现的账号数", bundle.EntityIPSubnet, bundle.EntityAccount, 3600, true),

		stream("account_login_fail_1h", "账号 1 小时登录失败次数", bundle.EntityAccount, []string{"login.fail"}, bundle.AggCount, "", 3600),
		stream("ip_login_fail_1h", "IP 1 小时登录失败次数", bundle.EntityIP, []string{"login.fail"}, bundle.AggCount, "", 3600),
		stream("ip_register_1d", "IP 1 天注册数", bundle.EntityIP, []string{"register.success"}, bundle.AggCount, "", 86400),
		stream("subnet_register_1d", "网段 1 天注册数", bundle.EntityIPSubnet, []string{"register.success"}, bundle.AggCount, "", 86400),
		stream("device_register_7d", "设备 7 天注册数", bundle.EntityDevice, []string{"register.success"}, bundle.AggCount, "", 7*86400),
		stream("user_paid_1d", "用户 1 天支付金额", bundle.EntityUser, []string{"payment.paid"}, bundle.AggSum, "amount", 86400),
		stream("user_refund_30d", "用户 30 天退款次数", bundle.EntityUser, []string{"payment.refund"}, bundle.AggCount, "", 30*86400),
		stream("user_blocked_7d", "用户 7 天被拦截次数", bundle.EntityUser, []string{"decision"}, bundle.AggCount, "", 7*86400, blocked),
		stream("device_blocked_7d", "设备 7 天被拦截次数", bundle.EntityDevice, []string{"decision"}, bundle.AggCount, "", 7*86400, blocked),
		stream("ip_blocked_1d", "IP 1 天被拦截次数", bundle.EntityIP, []string{"decision"}, bundle.AggCount, "", 86400, blocked),
		stream("user_scenes_1d", "用户 1 天触达的场景数", bundle.EntityUser, []string{"decision"}, bundle.AggDistinct, "scene", 86400),

		graphF("g_device_accounts", "关系图：同设备其他账号数", bundle.EntityUser, bundle.GraphDeviceAccounts, 30),
		graphF("g_ip_accounts", "关系图：同 IP 其他账号数", bundle.EntityUser, bundle.GraphIPAccounts, 7),
		graphF("g_phone_accounts", "关系图：同手机号其他账号数", bundle.EntityUser, bundle.GraphPhoneAccounts, 0),
		graphF("g_banned_neighbors", "关系图：两跳内被封账号数", bundle.EntityUser, bundle.GraphBannedNeighbors, 30),
		graphF("g_invite_children", "关系图：直接邀请人数", bundle.EntityUser, bundle.GraphInviteChildren, 0),
		graphF("g_invite_tree", "关系图：三层邀请树规模", bundle.EntityUser, bundle.GraphInviteTree, 0),
		graphF("g_device_users", "关系图：设备上的账号数", bundle.EntityDevice, bundle.GraphDeviceUsers, 30),
		graphF("g_ip_users", "关系图：IP 上的账号数", bundle.EntityIP, bundle.GraphIPUsers, 1),

		{Feature: bundle.Feature{Key: "v_similar_devices", Name: "向量：高度相似的设备数", Kind: bundle.FeatureVector,
			Vector: &bundle.VectorDef{Collection: "device", TopK: 20, MinScore: 0.97, Output: "similar_count"}}},
		{Feature: bundle.Feature{Key: "v_similar_banned", Name: "向量：相似设备里已封禁的数量", Kind: bundle.FeatureVector,
			Vector: &bundle.VectorDef{Collection: "device", TopK: 20, MinScore: 0.95, Output: "similar_banned"}}},

		derived("risky_network", "高危网络（代理 / VPN / Tor / 机房）", "net.proxy || net.vpn || net.tor || net.datacenter"),
		derived("suspicious_client", "可疑客户端（爬虫 / 无头 / 空 UA）", "client.bot || client.headless || client.empty"),
		derived("device_tampered", "设备被篡改（模拟器 / Root）", "device.emulator || device.rooted"),
	}
}

func scene(key, name, desc string, captcha bool, targets []string, autoBan bool) store.Scene {
	return store.Scene{
		Scene: bundle.Scene{Key: key, Name: name, Description: desc, TimeoutMS: 200, FailMode: "open", CaptchaSupported: captcha,
			Enforcement: bundle.Enforcement{Targets: targets, DurationSeconds: 86400, AutoBanAllowed: autoBan}},
		Bands: bundle.DefaultBands(), Builtin: true, Enabled: true,
	}
}

// Scenes 内置场景（与主服务 Guard 的接入点一一对应）
func Scenes() []store.Scene {
	acct := []string{"device", "ip"}
	out := []store.Scene{
		scene("login", "登录", "密码登录、卡密登录、OAuth2 登录页", true, acct, false),
		scene("admin_login", "管理员登录", "控制台登录", true, []string{"ip"}, false),
		scene("register", "注册", "密码注册与快速登录建号", true, acct, false),
		scene("password_reset", "找回密码", "发起密码找回", true, acct, false),
		scene("verify_code", "发送验证码", "短信 / 邮箱验证码发送", true, []string{"ip"}, false),
		scene("payment", "支付下单", "创建支付订单（extra.amount 为金额）", false, []string{"device"}, false),
		scene("invite", "邀请绑定", "填写邀请码", false, []string{"device"}, true),
		scene("lottery", "抽奖", "抽奖活动", false, []string{"device"}, true),
		scene("signin", "签到", "每日签到", false, []string{"device"}, true),
		scene("cardkey", "卡密兑换", "兑换卡与授权卡", false, []string{"device", "ip"}, false),
		scene("points_redeem", "积分兑换", "积分商城兑换", false, []string{"device"}, true),
		scene("ad_reward", "激励广告发奖", "看完激励视频后发奖", false, []string{"device"}, true),
	}
	for i := range out {
		switch out[i].Key {
		case "payment":
			out[i].Fields = []bundle.Field{{Key: "amount", Label: "金额", Type: bundle.FieldNumber}, {Key: "method", Label: "支付方式", Type: bundle.FieldString}}
		case "lottery":
			out[i].Fields = []bundle.Field{{Key: "activityId", Label: "活动 ID", Type: bundle.FieldNumber}, {Key: "times", Label: "抽数", Type: bundle.FieldNumber}}
		case "invite":
			out[i].Fields = []bundle.Field{{Key: "code", Label: "邀请码", Type: bundle.FieldString}}
		}
	}
	return out
}

// Template 规则模板
type Template struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scenes      []string     `json:"scenes"` // 适用场景（* = 全部）
	Rules       []store.Rule `json:"rules"`
}

func rule(key, name, dim, expr string, score int, decision, reason string) store.Rule {
	return store.Rule{Rule: bundle.Rule{Key: key, Name: name, Dimension: dim, Expr: expr, Score: score, Mode: bundle.ModeEnforce, Decision: decision, Reason: reason}, Enabled: true}
}

// Templates 规则模板
func Templates() []Template {
	return []Template{
		{Key: "baseline", Name: "基础防护", Description: "高危网络、可疑客户端、设备篡改、观察名单、关系与相似设备", Scenes: []string{"*"}, Rules: []store.Rule{
			rule("risky_network", "高危网络", bundle.DimNetwork, "F.risky_network == 1", 25, "", "来自代理 / VPN / 机房网络"),
			rule("suspicious_client", "可疑客户端", bundle.DimClient, "F.suspicious_client == 1", 30, "", "爬虫、无头浏览器或空 UA"),
			rule("device_tampered", "设备被篡改", bundle.DimDevice, "F.device_tampered == 1", 20, "", "模拟器或已 Root"),
			rule("watch_list", "观察名单", bundle.DimList, "list.watch", 30, "", "命中观察名单"),
			rule("banned_neighbors", "关联被封账号", bundle.DimGraph, "F.g_banned_neighbors >= 2", 40, "", "两跳内有 {{F.g_banned_neighbors}} 个被封账号"),
			rule("similar_banned_devices", "与被封设备高度相似", bundle.DimDevice, "F.v_similar_banned >= 1", 45, "", "与 {{F.v_similar_banned}} 台被封设备指纹几乎一致"),
			rule("device_farm", "设备农场", bundle.DimDevice, "F.v_similar_devices >= 10", 50, "", "同应用里有 {{F.v_similar_devices}} 台指纹几乎一样的设备"),
		}},
		{Key: "account_security", Name: "账号安全", Description: "撞库、爆破、一号多机", Scenes: []string{"login", "password_reset", "admin_login"}, Rules: []store.Rule{
			rule("ip_burst", "IP 请求突增", bundle.DimBehavior, "F.ip_scene_10m > 30", 40, "", "IP 10 分钟请求 {{F.ip_scene_10m}} 次"),
			rule("ip_many_accounts", "一 IP 多账号", bundle.DimBehavior, "F.ip_accounts_1h > 10", 50, "", "IP 1 小时出现 {{F.ip_accounts_1h}} 个账号"),
			rule("account_bruteforce", "账号被爆破", bundle.DimAccount, "F.account_login_fail_1h >= 5", 60, "", "账号 1 小时登录失败 {{F.account_login_fail_1h}} 次"),
			rule("ip_bruteforce", "IP 撞库", bundle.DimBehavior, "F.ip_login_fail_1h >= 20", 80, "", "IP 1 小时登录失败 {{F.ip_login_fail_1h}} 次"),
			rule("account_many_devices", "一号多机", bundle.DimAccount, "F.account_devices_1d >= 5", 30, "", "账号 1 天用了 {{F.account_devices_1d}} 台设备"),
		}},
		{Key: "registration", Name: "注册反作弊", Description: "批量注册、网段集中、设备复用", Scenes: []string{"register"}, Rules: []store.Rule{
			rule("ip_register_burst", "IP 批量注册", bundle.DimBehavior, "F.ip_register_1d >= 5", 50, "", "IP 1 天注册 {{F.ip_register_1d}} 个"),
			rule("subnet_register_burst", "网段集中注册", bundle.DimNetwork, "F.subnet_register_1d >= 30", 40, "", "网段 1 天注册 {{F.subnet_register_1d}} 个"),
			rule("device_reuse", "设备重复注册", bundle.DimDevice, "F.device_register_7d >= 3", 60, "", "设备 7 天注册 {{F.device_register_7d}} 个"),
			rule("device_many_users", "设备挂多号", bundle.DimGraph, "F.g_device_users >= 8", 80, "block", "设备上已有 {{F.g_device_users}} 个账号"),
		}},
		{Key: "marketing", Name: "营销反作弊", Description: "羊毛党、设备农场、邀请裂变", Scenes: []string{"invite", "lottery", "signin", "points_redeem", "ad_reward", "cardkey"}, Rules: []store.Rule{
			rule("device_accounts", "一机多号", bundle.DimDevice, "F.device_accounts_1d >= 3", 50, "", "设备 1 天出现 {{F.device_accounts_1d}} 个账号"),
			rule("graph_device_accounts", "同设备账号团", bundle.DimGraph, "F.g_device_accounts >= 5", 60, "", "与 {{F.g_device_accounts}} 个账号共用设备"),
			rule("invite_burst", "邀请裂变异常", bundle.DimGraph, "scene == 'invite' && F.g_invite_tree >= 200", 60, "", "三层邀请树 {{F.g_invite_tree}} 人"),
			rule("farm_ban", "设备农场封禁", bundle.DimDevice, "F.v_similar_banned >= 3 && F.device_tampered == 1", 0, "ban", "改机设备与多台被封设备相同"),
			rule("user_blocked_recently", "近期多次被拦", bundle.DimAccount, "F.user_blocked_7d >= 5", 40, "", "7 天被拦截 {{F.user_blocked_7d}} 次"),
		}},
		{Key: "payment", Name: "支付风控", Description: "大额、频繁退款、新设备大额", Scenes: []string{"payment"}, Rules: []store.Rule{
			rule("big_amount", "单笔大额", bundle.DimBehavior, "amount >= 2000", 30, "", "单笔 {{amount}}"),
			rule("daily_amount", "当日累计大额", bundle.DimBehavior, "F.user_paid_1d + amount >= 10000", 50, "", "当日累计 {{F.user_paid_1d + amount}}"),
			rule("refund_abuse", "频繁退款", bundle.DimAccount, "F.user_refund_30d >= 3", 60, "", "30 天退款 {{F.user_refund_30d}} 次"),
			rule("pay_risky_network", "高危网络支付", bundle.DimNetwork, "F.risky_network == 1 && amount >= 500", 40, "", "代理网络下单 {{amount}}"),
		}},
		{Key: "verify_code", Name: "短信轰炸", Description: "验证码接口刷量", Scenes: []string{"verify_code"}, Rules: []store.Rule{
			rule("ip_sms_burst", "IP 刷验证码", bundle.DimBehavior, "F.ip_scene_1m >= 5 || F.ip_scene_1h >= 30", 80, "", "IP 请求验证码过于频繁"),
			rule("phone_sms_burst", "号码被轰炸", bundle.DimAccount, "F.phone_scene_1h >= 8", 80, "block", "号码 1 小时请求 {{F.phone_scene_1h}} 次"),
		}},
	}
}

// Seed 写入缺的内置场景与起步特征（已存在的不动）
func Seed(ctx context.Context, s *store.Store) error {
	for _, f := range Features() {
		if _, err := s.GetFeature(ctx, f.Key); errors.Is(err, store.ErrNotFound) {
			if err := s.UpsertFeature(ctx, &f, "system"); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	for _, sc := range Scenes() {
		if _, err := s.GetScene(ctx, sc.Key); errors.Is(err, store.ErrNotFound) {
			if err := s.UpsertScene(ctx, &sc, "system"); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

// Apply 把模板装到场景上：已存在的同键规则跳过；observe 为真时以观察模式装
func Apply(ctx context.Context, s *store.Store, tplKey, sceneKey string, observe bool, by string) (installed, skipped int, err error) {
	var tpl *Template
	for _, t := range Templates() {
		if t.Key == tplKey {
			t := t
			tpl = &t
		}
	}
	if tpl == nil {
		return 0, 0, errors.New("模板不存在")
	}
	existing, err := s.ListRules(ctx, sceneKey)
	if err != nil {
		return 0, 0, err
	}
	have := map[string]bool{}
	for _, r := range existing {
		if r.Scene == sceneKey {
			have[r.Key] = true
		}
	}
	for _, r := range tpl.Rules {
		if have[r.Key] {
			skipped++
			continue
		}
		r.Scene = sceneKey
		r.TemplateKey = tpl.Key
		if observe {
			r.Mode = bundle.ModeObserve
		}
		if err := s.SaveRule(ctx, &r, by); err != nil {
			return installed, skipped, err
		}
		installed++
	}
	return installed, skipped, nil
}
