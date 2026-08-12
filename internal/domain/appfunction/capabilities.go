package appfunction

import "strings"

// 脚本能力键。声明即授权：未声明的能力在脚本里根本不会被绑定，
// `aegis.points` 直接是 undefined，而不是「调用时才报错」。
const (
	CapUserRead         = "user.read"
	CapUserWrite        = "user.write"
	CapPointsWrite      = "points.write"
	CapVipRead          = "vip.read"
	CapVipWrite         = "vip.write"
	CapWalletRead       = "wallet.read"
	CapWalletWrite      = "wallet.write"
	CapKVRead           = "kv.read"
	CapKVWrite          = "kv.write"
	CapNotificationSend = "notification.send"
	CapEmailSend        = "email.send"
	CapAuditWrite       = "audit.write"
	CapHTTPFetch        = "http.fetch"
)

// 000056 时期的旧能力名。当时 effects 只校验不执行，声明了也没有实际作用；
// 保留它们仅为让存量函数仍能通过更新校验，新函数请使用上面的能力名。
const (
	legacyCapStorageRead     = "storage.read"
	legacyCapStorageWrite    = "storage.write"
	legacyCapUserProfileRead = "user.profile.read"
	legacyCapUserTagWrite    = "user.tag.write"
)

// 能力分组，决定控制台上的排布顺序。
const (
	CapGroupIdentity = "identity"
	CapGroupAsset    = "asset"
	CapGroupState    = "state"
	CapGroupReach    = "reach"
	CapGroupAudit    = "audit"
	CapGroupEgress   = "egress"
	CapGroupLegacy   = "legacy"
)

// 风险档位。控制台按它给勾选框着色，`high` 需要作者明确知道自己在开什么。
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Capability 一项脚本能力的完整自述。
//
// 这张表是**单一事实源**：服务端校验、SDK 绑定、控制台的勾选框、
// 编辑器的类型提示全部读它。新增一种能力只需在这里加一行 + 在 SDK 里加一个
// binder，控制台与编辑器零改动即自动出现 —— 与支付渠道 `Describe()`、
// 风控条件目录是同一套做法。
//
// `TestCapabilityCatalogHasBinder` 双向钉死「目录 ↔ 绑定分支」：
// 目录多一条 → 勾了却没有对象；SDK 多一条 → 没声明也能调。
type Capability struct {
	Key   string `json:"key"`
	Group string `json:"group"`
	Label string `json:"label"`
	// API 在控制台上以等宽字体展示的调用形态。
	API  string `json:"api"`
	Hint string `json:"hint"`
	Risk string `json:"risk"`
	// Mutating 该能力会产生副作用（进 effects、进审计）。
	Mutating bool `json:"mutating"`
	// RequiresUser 只有「调用者是应用用户」时才可用；
	// 管理员调试与服务端密钥调用会在运行时被拒绝。
	RequiresUser bool `json:"requiresUser"`
	// Deprecated 旧能力名，仅为存量数据保留，不再绑定任何对象。
	Deprecated bool   `json:"deprecated,omitempty"`
	ReplacedBy string `json:"replacedBy,omitempty"`
	// Namespace 这项能力挂在 `aegis` 下的哪个对象上；为空表示直接挂在根上。
	//
	// 多项能力可以共用一个命名空间（user.read 出 get、user.write 出 ban），
	// 拼 .d.ts 时会合并成同一个对象 —— 否则同一个接口里会出现两个 `user:` 成员，
	// TypeScript 直接报重复声明，编辑器里整份类型全部失效。
	Namespace string `json:"namespace,omitempty"`
	// Declaration 该能力贡献的成员声明（TypeScript）。
	// 控制台把它拼进喂给 Monaco 的 .d.ts —— 补全里出现什么，运行时就绑定了什么。
	Declaration string `json:"declaration,omitempty"`
	// Interfaces 该能力额外需要的接口定义，与 Declaration 一起下发。
	Interfaces string `json:"interfaces,omitempty"`
}

// capabilities 顺序即控制台展示顺序：先按分组，组内按「读在写前、低危在高危前」。
var capabilities = []Capability{
	{
		Key: CapUserRead, Group: CapGroupIdentity,
		Label: "读取用户状态", API: "aegis.user.get()",
		Hint: "会员是否有效、是不是试用、积分、封禁状态 —— 反破解的根基",
		Risk:      RiskLow,
		Namespace: "user",
		Declaration: `
    /** 读取用户状态。省略 userId 时读当前调用者；不属于本应用的用户返回 null */
    get(userId?: number): AegisUser | null;
    /** 会员判定的完整结论（是否会员 / 还剩多久 / 是不是试用 / 试用还能不能领） */
    entitlement(userId?: number): AegisEntitlement | null;`,
	},
	{
		Key: CapUserWrite, Group: CapGroupIdentity,
		Label: "处置用户", API: "aegis.user.ban / unban",
		Hint: "封禁落成正式封禁记录（可撤销、可申诉、有操作人），不是翻一个布尔位",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "user",
		Declaration: `
    /** 封禁当前调用者。seconds 省略或为 0 表示永久封禁 */
    ban(reason: string, seconds?: number, options?: { scope?: string; type?: string }): { banId: number; endAt?: string };
    /** 撤销当前调用者生效中的封禁，返回是否确实撤销了一条 */
    unban(reason?: string): boolean;`,
	},
	{
		Key: CapPointsWrite, Group: CapGroupAsset,
		Label: "积分读写", API: "aegis.points.add / deduct",
		Hint: "走正式积分流水，余额不足由服务端拒绝",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "points",
		Declaration: `
    /** 增加积分，返回变更后余额 */
    add(amount: number, reason?: string): number;
    /** 扣减积分，返回变更后余额；余额不足会抛错 */
    deduct(amount: number, reason?: string): number;`,
	},
	{
		Key: CapVipRead, Group: CapGroupAsset,
		Label: "读取会员状态", API: "aegis.vip.status()",
		Hint: "只要会员结论时用它；要连同积分与封禁一起读请用 user.read",
		Risk:      RiskLow,
		Namespace: "vip",
		Declaration: `
    /** 会员判定的完整结论（只读） */
    status(userId?: number): AegisEntitlement | null;
    /** 是否拥有某个功能标识。判功能标识而不是套餐名 —— 后者运营随时会改 */
    hasFeature(tag: string, userId?: number): boolean;`,
	},
	{
		Key: CapVipWrite, Group: CapGroupAsset,
		Label: "发放会员", API: "aegis.vip.grant(days)",
		Hint: "按天延长会员有效期，进 vip_transactions 账本",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "vip",
		Declaration: `
    /** 按天延长当前调用者的会员有效期 */
    grant(days: number, reason?: string): { days: number; userId: number; expireAt?: string } | null;
    /** 按天收回会员有效期；到期时间不会被推到当前时刻之前 */
    revoke(days: number, reason?: string): { days: number; userId: number; expireAt?: string } | null;`,
	},
	{
		Key: CapWalletRead, Group: CapGroupAsset,
		Label: "读取余额", API: "aegis.wallet.get()",
		Hint: "金额一律是字符串（服务端用定点小数），转成 number 会丢分",
		Risk:      RiskLow,
		Namespace: "wallet",
		Declaration: `
    /** 钱包余额。金额全部是字符串 —— 不要转 number */
    get(userId?: number): AegisWallet | null;`,
		Interfaces: `
declare interface AegisWallet {
  userId: number;
  /** 定点小数字符串，例如 "128.50" */
  balance: string;
  frozen: string;
  totalRecharged: string;
  totalConsumed: string;
}`,
	},
	{
		Key: CapWalletWrite, Group: CapGroupAsset,
		Label: "调整余额", API: "aegis.wallet.adjust(amount)",
		Hint: "正数入账、负数扣减，走管理员调账流水；余额不足由服务端拒绝",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "wallet",
		Declaration: `
    /** 调整当前调用者余额。amount 传字符串以免丢精度；正数入账、负数扣减 */
    adjust(amount: string | number, reason?: string): AegisWallet;`,
	},
	{
		Key: CapKVRead, Group: CapGroupState,
		Label: "读取 KV", API: "aegis.kv.get / list / has",
		Hint: "服务端独占状态，客户端读不到也伪造不了",
		Risk: RiskLow,
		// kv 是一个有自己接口类型的对象，两项能力贡献的是同一行成员声明，
		// 拼装时按文本去重 —— 只勾其中一项也要有类型。
		Declaration: `
  /** 服务端独占的键值状态，客户端既读不到也伪造不了 */
  kv: AegisKV;`,
		Interfaces: kvInterfaces,
	},
	{
		Key: CapKVWrite, Group: CapGroupState,
		Label: "写入 KV", API: "aegis.kv.set / incr / del",
		Hint: "incr 是数据库层面的原子操作，频次限制与剩余次数依赖它",
		Risk: RiskMedium, Mutating: true,
		Declaration: `
  /** 服务端独占的键值状态，客户端既读不到也伪造不了 */
  kv: AegisKV;`,
		Interfaces: kvInterfaces,
	},
	{
		Key: CapNotificationSend, Group: CapGroupReach,
		Label: "发送站内信", API: "aegis.notify.send()",
		Hint: "推送给当前调用者，进 notifications 表并实时下发",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "notify",
		Declaration: `
    /** 给当前调用者发站内信，返回投递条数 */
    send(title: string, content: string, options?: { level?: "info" | "warning" | "critical"; type?: string }): number;`,
	},
	{
		Key: CapEmailSend, Group: CapGroupReach,
		Label: "发送邮件", API: "aegis.email.send()",
		Hint: "恒发往调用者账号绑定的邮箱 —— 脚本填不了收件人",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "email",
		Declaration: `
    /** 发往当前调用者绑定的邮箱。收件地址由服务端决定 */
    send(subject: string, htmlBody: string): boolean;`,
	},
	{
		Key: CapAuditWrite, Group: CapGroupAudit,
		Label: "写审计日志", API: "aegis.audit.log()",
		Hint: "留痕到平台审计，操作者记为 function:<函数名>",
		Risk: RiskLow, Mutating: true,
		Namespace: "audit",
		Declaration: `
    /** 写入平台审计日志 */
    log(action: string, summary?: string): void;`,
	},
	{
		Key: CapHTTPFetch, Group: CapGroupEgress,
		Label: "出站 HTTP", API: "aegis.fetch(url, options)",
		Hint: "仅 HTTPS、禁重定向、连接时重新解析并拒绝内网与云元数据地址",
		Risk: RiskHigh, Mutating: true,
		Declaration: `
  /** 出站 HTTPS 请求。禁止重定向，且拒绝解析到内网 / 元数据地址的域名 */
  fetch(url: string, options?: {
    method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
    headers?: Record<string, string>;
    /** 对象会被 JSON 序列化；字符串原样发送 */
    body?: any;
  }): AegisFetchResult;`,
	},

	// ── 兼容存量数据的旧能力名（不绑定任何对象） ──────────────────────
	{
		Key: legacyCapUserProfileRead, Group: CapGroupLegacy,
		Label: "读取用户资料（旧）", API: "—",
		Hint: "000056 时期的能力名，等价于 user.read", Risk: RiskLow,
		Deprecated: true, ReplacedBy: CapUserRead,
	},
	{
		Key: legacyCapUserTagWrite, Group: CapGroupLegacy,
		Label: "写用户标签（旧）", API: "—",
		Hint: "000056 时期 effects 只校验不执行，声明了也没有实际作用", Risk: RiskLow,
		Deprecated: true,
	},
	{
		Key: legacyCapStorageRead, Group: CapGroupLegacy,
		Label: "读取存储（旧）", API: "—",
		Hint: "同上，仅为让存量函数通过更新校验而保留", Risk: RiskLow,
		Deprecated: true,
	},
	{
		Key: legacyCapStorageWrite, Group: CapGroupLegacy,
		Label: "写入存储（旧）", API: "—",
		Hint: "同上，仅为让存量函数通过更新校验而保留", Risk: RiskLow,
		Deprecated: true,
	},
}

// kvInterfaces 由 kv.read 与 kv.write 共用；拼装时会去重。
const kvInterfaces = `
declare interface AegisKVNamespace {
  /** 读取，不存在或已过期返回 null */
  get(key: string): any;
  /** 键是否存在且未过期 */
  has(key: string): boolean;
  /** 按前缀列出键（不返回值），上限 200 条 */
  list(prefix?: string, limit?: number): string[];
  /** 写入。ttlSeconds 省略或为 0 表示永不过期 */
  set(key: string, value: any, ttlSeconds?: number): void;
  /** 原子自增，返回自增后的值。频次限制、剩余次数依赖它的原子性 */
  incr(key: string, delta?: number, ttlSeconds?: number): number;
  del(key: string): boolean;
}

declare interface AegisKV extends AegisKVNamespace {
  /** 按调用者隔离的命名空间，脚本无法跨用户读写 */
  user: AegisKVNamespace;
}`

// BaseDeclaration 是与能力无关、永远存在的那部分类型声明。
//
// 它同时是 goja 沙箱的实况说明：这里没有的东西运行时就没有。
// `lib` 只给 es2020（不含 DOM），因此 document / setTimeout / require
// 会被编辑器如实标红。
const BaseDeclaration = `
/** 远程函数调用上下文。由 Aegis 构造，客户端无法伪造其中的身份字段。 */
declare interface AegisCaller {
  /** user（应用用户）| admin（控制台调试）| app（服务端函数密钥） */
  type: "user" | "admin" | "app";
  /** 应用用户调用时存在 */
  userId?: number;
  /** 管理员在控制台调试时存在 */
  adminId?: number;
  /** 使用函数调用密钥（服务端）时存在 */
  keyId?: number;
}

declare interface AegisContext {
  /** 本次调用的幂等 ID，同一 eventId 重复提交会直接返回既有结果 */
  eventId: string;
  appId: number;
  appKey: string;
  /** 函数名 */
  function: string;
  /** 当前激活的版本号 */
  version: string;
  /** 调用者身份，由服务端认定 */
  caller: AegisCaller;
  /** 调用方传入的 input 字段 */
  input: any;
  /** 本次是不是控制台里的试跑（试跑不会真的写数据） */
  dryRun: boolean;
}

declare interface AegisEntitlement {
  /** 当前是不是会员 */
  isVip: boolean;
  /** 这段会员期是不是试用给的 */
  isTrial: boolean;
  /** 凭什么是会员：none / trial / wallet / payment_order / admin_grant */
  source: string;
  /** 当前生效的功能标识（各段未到期开通记录的并集） */
  features: string[];
  expireAt?: string;
  remainingSeconds: number;
  remainingDays: number;
  /** 试用还能不能领、不能的话为什么 */
  trialAvailable: boolean;
  trialReason?: string;
}

declare interface AegisUser {
  id: number;
  account: string;
  /** 账号是否启用 */
  enabled: boolean;
  /** 是否处于封禁中（含临时封禁） */
  banned: boolean;
  bannedUntil?: string;
  /** 会员是否仍在有效期内 —— 由服务端现算，客户端伪造不了 */
  vip: boolean;
  /** 当前这段会员期是不是试用给的（试用用户能不能用某个功能由脚本自己决定） */
  vipTrial: boolean;
  /** 凭什么是会员：none / trial / wallet / payment_order / admin_grant */
  vipSource: string;
  /** 当前生效的功能标识。判它而不是判套餐名 —— 套餐名是运营随时会改的展示文案 */
  vipFeatures: string[];
  vipExpireAt?: string;
  vipRemainingSeconds?: number;
  integral: number;
  experience: number;
  createdAt: string;
  nickname?: string;
  email?: string;
  /** 设备机器码 */
  markcode?: string;
  role?: string;
}

declare interface AegisFetchResult {
  status: number;
  /** status 落在 2xx */
  ok: boolean;
  headers: Record<string, string>;
  text: string;
  /** 响应可被解析为 JSON 时存在 */
  json?: any;
}

declare interface AegisCrypto {
  md5(input: string): string;
  sha1(input: string): string;
  sha256(input: string): string;
  sha512(input: string): string;
  hmacSha256(key: string, data: string): string;
  hmacSha512(key: string, data: string): string;
  base64Encode(input: string): string;
  base64Decode(input: string): string;
  base64UrlEncode(input: string): string;
  base64UrlDecode(input: string): string;
  hexEncode(input: string): string;
  hexDecode(input: string): string;
  /** size 取值 1..64，默认 16 */
  randomHex(size?: number): string;
  /** 闭区间 [min, max] 的密码学随机整数 */
  randomInt(min: number, max: number): number;
  uuid(): string;
  /** 定长比较，不会因为提前返回而泄露前缀信息 */
  timingSafeEqual(a: string, b: string): boolean;
}

declare interface AegisTime {
  /** 服务端当前时间，Unix 毫秒 */
  now(): number;
  /** 服务端当前时间，Unix 秒 */
  unix(): number;
  /** RFC3339 字符串，省略参数即当前时刻 */
  iso(unixMillis?: number): string;
  /** UTC 日键 "YYYY-MM-DD"，offsetDays 可取负数。做「每日额度」的键就用它 */
  dayKey(offsetDays?: number): string;
  /** UTC 月键 "YYYY-MM" */
  monthKey(offsetMonths?: number): string;
}

/** 控制台上可随时改、不需要发新版本的函数级参数 */
declare interface AegisConfig {
  [key: string]: any;
}
`

// SDKDeclaration 按已声明的能力拼出完整 .d.ts。
//
// 放在服务端而不是控制台，是为了让「编辑器提示什么」与「运行时绑定什么」
// 只有一处定义。控制台拿到目录后原样拼接，不再自己维护一份类型。
func SDKDeclaration(capabilityKeys []string) string {
	declared := NormalizeCapabilities(capabilityKeys)
	set := make(map[string]struct{}, len(declared))
	for _, key := range declared {
		set[key] = struct{}{}
	}

	var (
		rootMembers    []string
		namespaceOrder []string
		namespaceBody  = map[string][]string{}
		interfaces     []string
		seen           = map[string]struct{}{}
	)
	for _, capability := range capabilities {
		if capability.Deprecated || capability.Declaration == "" {
			continue
		}
		if _, ok := set[capability.Key]; !ok {
			continue
		}
		if capability.Namespace == "" {
			// kv.read / kv.write 贡献的是同一行成员，按文本去重
			if _, duplicated := seen[capability.Declaration]; !duplicated {
				rootMembers = append(rootMembers, strings.TrimRight(capability.Declaration, "\n"))
				seen[capability.Declaration] = struct{}{}
			}
		} else {
			// 同一命名空间由多项能力共同组成（user.read 出 get、user.write 出 ban），
			// 合并成一个对象；分开写会在同一个接口里产生两个同名成员。
			if _, exists := namespaceBody[capability.Namespace]; !exists {
				namespaceOrder = append(namespaceOrder, capability.Namespace)
			}
			namespaceBody[capability.Namespace] = append(namespaceBody[capability.Namespace],
				strings.TrimRight(capability.Declaration, "\n"))
		}
		if capability.Interfaces == "" {
			continue
		}
		if _, duplicated := seen[capability.Interfaces]; duplicated {
			continue
		}
		interfaces = append(interfaces, strings.TrimSpace(capability.Interfaces))
		seen[capability.Interfaces] = struct{}{}
	}

	members := rootMembers
	for _, namespace := range namespaceOrder {
		members = append(members, "  "+namespace+": {"+
			strings.Join(namespaceBody[namespace], "")+"\n  };")
	}

	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(BaseDeclaration))
	builder.WriteString("\n\n")
	for _, block := range interfaces {
		builder.WriteString(block)
		builder.WriteString("\n\n")
	}
	builder.WriteString(`declare interface AegisSDK {
  /** 写服务端日志，永远不会返回给调用方。console.log 是它的别名 */
  log(...args: any[]): void;
  /** 主动返回业务错误并终止本次调用（如授权过期、次数用尽） */
  fail(message: string, code?: number): never;
  crypto: AegisCrypto;
  time: AegisTime;
  /** 函数级配置，控制台上可改，改完立即生效、不需要发新版本 */
  config: AegisConfig;`)
	for _, member := range members {
		builder.WriteString("\n")
		builder.WriteString(member)
	}
	builder.WriteString(`
}

/** 注入脚本的全局对象。未声明的能力在这里不会出现。 */
declare const aegis: AegisSDK;

/** console 是 aegis.log 的别名，只进服务端日志 */
declare const console: {
  log(...args: any[]): void;
  info(...args: any[]): void;
  warn(...args: any[]): void;
  error(...args: any[]): void;
  debug(...args: any[]): void;
};
`)
	return builder.String()
}

// CapabilityCatalog 返回能力目录的只读快照。
func CapabilityCatalog() []Capability {
	out := make([]Capability, len(capabilities))
	copy(out, capabilities)
	return out
}

// CapabilityByKey 查一项能力；第二个返回值为 false 表示不存在。
func CapabilityByKey(key string) (Capability, bool) {
	for _, capability := range capabilities {
		if capability.Key == key {
			return capability, true
		}
	}
	return Capability{}, false
}

// IsKnownCapability 该能力键是否受支持（含为兼容保留的旧名）。
func IsKnownCapability(key string) bool {
	_, ok := CapabilityByKey(key)
	return ok
}

// NormalizeCapabilities 去重、去空白，并把旧能力名映射到等价的新能力上。
//
// 之所以在这里做映射而不是在每个消费方各判一次：判漏一处的表现是
// 「存量函数在这条链路上少了一项能力」，而那不会有任何报错。
func NormalizeCapabilities(keys []string) []string {
	set := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	add := func(key string) {
		if key == "" {
			return
		}
		if _, ok := set[key]; ok {
			return
		}
		set[key] = struct{}{}
		out = append(out, key)
	}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		add(key)
		if capability, ok := CapabilityByKey(key); ok && capability.ReplacedBy != "" {
			add(capability.ReplacedBy)
		}
	}
	return out
}

// RuntimeLimits 是脚本运行时的硬上限，随目录一起下发。
//
// 控制台把它渲染成说明文字：作者应当在写脚本之前就知道额度，
// 而不是在一次超限失败之后去翻文档。
type RuntimeLimits struct {
	MaxSDKCalls      int `json:"maxSdkCalls"`
	MaxSDKMutations  int `json:"maxSdkMutations"`
	MaxSDKFetches    int `json:"maxSdkFetches"`
	MaxSourceBytes   int `json:"maxSourceBytes"`
	MaxKVKeyLength   int `json:"maxKvKeyLength"`
	MaxKVValueBytes  int `json:"maxKvValueBytes"`
	MaxFetchBodySize int `json:"maxFetchBodyBytes"`
	MaxConfigBytes   int `json:"maxConfigBytes"`
	MaxTimeoutMs     int `json:"maxTimeoutMs"`
	MaxConcurrency   int `json:"maxConcurrency"`
}

// ScriptTemplate 控制台「从模板新建」用的示例脚本。
//
// 每个模板都带上它需要的能力：选中模板即可把 capabilities 一并勾好，
// 避免作者写完才发现少声明一项。
type ScriptTemplate struct {
	Key          string   `json:"key"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Capabilities []string `json:"capabilities"`
	Source       string   `json:"source"`
}

// ScriptTemplates 返回内置模板。
func ScriptTemplates() []ScriptTemplate {
	return []ScriptTemplate{
		{
			Key:     "starter",
			Title:   "空白骨架",
			Summary: "只有入口与调用者判定，从零开始写",
			Capabilities: []string{
				CapUserRead,
			},
			Source: templateStarter,
		},
		{
			Key:     "licence",
			Title:   "会员校验 + 每日额度",
			Summary: "会员判定 + 服务端独占计数器，反破解的标准写法",
			Capabilities: []string{
				CapUserRead, CapKVRead, CapKVWrite,
			},
			Source: templateLicence,
		},
		{
			Key:     "reward",
			Title:   "任务发奖",
			Summary: "一次性奖励：先用 KV 判重、再发积分与会员，最后发通知",
			Capabilities: []string{
				CapUserRead, CapKVRead, CapKVWrite, CapPointsWrite, CapVipWrite, CapNotificationSend,
			},
			Source: templateReward,
		},
		{
			Key:     "signature",
			Title:   "服务端签名下发",
			Summary: "密钥只存在于服务端，客户端拿到的只有一次性签名",
			Capabilities: []string{
				CapUserRead, CapKVRead,
			},
			Source: templateSignature,
		},
		{
			Key:     "proxy",
			Title:   "外部接口代理",
			Summary: "把第三方 API 的密钥留在服务端，顺带做配额与审计",
			Capabilities: []string{
				CapUserRead, CapKVRead, CapKVWrite, CapHTTPFetch, CapAuditWrite,
			},
			Source: templateProxy,
		},
	}
}

const templateStarter = `// 每次调用都是全新的运行时，没有跨请求状态。
// 只有在 capabilities 里声明过的能力，aegis 上才会出现对应的对象。
//
// 沙箱里没有 DOM、没有定时器、没有 require —— 编辑器会如实标红。
// handle 必须同步返回，不支持 async。

/** @param {AegisContext} ctx */
function handle(ctx) {
  // ctx.caller 是服务端认定的身份，客户端伪造不了
  const me = aegis.user.get();
  if (!me) {
    aegis.fail("需要用户身份调用", 40100);
  }

  return { ok: true, userId: me.id, at: aegis.time.iso() };
}
`

const templateLicence = `// 会员校验 + 每日额度。
//
// 这个函数无法在客户端被复现：它依赖的两件事都只存在于服务端 ——
// 会员判定结论，以及一个客户端既读不到也改不了的计数器。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  if (!me) aegis.fail("需要用户身份调用", 40100);
  if (me.banned) aegis.fail("账号不可用", 40311);
  if (!me.vip) aegis.fail("该功能仅限会员", 40310);

  // 试用会员要不要放行由脚本自己决定 —— 服务端只如实告诉你他是哪一档
  if (me.vipTrial) aegis.fail("该功能不对试用会员开放", 40310);

  // 每日额度上限做成函数配置：改额度不需要发新版本
  const quota = Number(aegis.config.dailyQuota || 100);

  // incr 是数据库层面的原子操作，并发调用不会读到同一个旧值
  const used = aegis.kv.user.incr("quota:" + aegis.time.dayKey(), 1, 86400);
  if (used > quota) aegis.fail("今日额度已用尽", 42901);

  return { ok: true, used: used, remaining: quota - used };
}
`

const templateReward = `// 一次性任务奖励。
//
// 顺序是刻意的：先判重、再发放。整个脚本不是一个大事务，
// 中途抛错不会回滚先前的写入，所以校验必须全部走在写入之前。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const task = String(ctx.input.task || "").trim();
  if (!task) aegis.fail("缺少 task 参数", 40001);

  const me = aegis.user.get();
  if (!me) aegis.fail("需要用户身份调用", 40100);
  if (me.banned) aegis.fail("账号不可用", 40311);

  // 判重键落在用户级命名空间，脚本无法跨用户读写
  const claimKey = "reward:" + task;
  if (aegis.kv.user.has(claimKey)) {
    aegis.fail("该奖励已领取", 40901);
  }

  const points = Number(aegis.config.rewardPoints || 100);
  const vipDays = Number(aegis.config.rewardVipDays || 0);

  aegis.kv.user.set(claimKey, { at: aegis.time.iso() }, 0);
  const balance = aegis.points.add(points, "任务奖励 " + task);
  if (vipDays > 0) {
    aegis.vip.grant(vipDays, "任务奖励 " + task);
  }
  aegis.notify.send("奖励已到账", "完成「" + task + "」获得 " + points + " 积分");

  return { ok: true, points: points, vipDays: vipDays, balance: balance };
}
`

const templateSignature = `// 服务端签名下发。
//
// 客户端拿到的是一次性签名，签名密钥从不离开服务端 ——
// 即便把客户端整个反编译，也造不出第二个合法签名。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  if (!me || !me.vip) aegis.fail("该功能仅限会员", 40310);

  // 密钥存在 KV 里（应用级），只有服务端读得到
  const secret = aegis.kv.get("signing-secret");
  if (!secret) aegis.fail("服务端尚未配置签名密钥", 50001);

  const nonce = aegis.crypto.randomHex(16);
  const issuedAt = aegis.time.unix();
  const payload = [me.id, nonce, issuedAt].join("\n");

  return {
    userId: me.id,
    nonce: nonce,
    issuedAt: issuedAt,
    expiresIn: 300,
    signature: aegis.crypto.hmacSha256(String(secret), payload)
  };
}
`

const templateProxy = `// 外部接口代理。
//
// 第三方 API Key 留在服务端，客户端只能通过这个函数间接使用它，
// 因此额度、频次、审计全部由平台说了算。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  if (!me || me.banned) aegis.fail("账号不可用", 40311);

  const perDay = Number(aegis.config.callsPerDay || 50);
  const used = aegis.kv.user.incr("proxy:" + aegis.time.dayKey(), 1, 86400);
  if (used > perDay) aegis.fail("今日调用次数已用尽", 42901);

  const endpoint = String(aegis.config.endpoint || "");
  const apiKey = String(aegis.kv.get("upstream-api-key") || "");
  if (!endpoint || !apiKey) aegis.fail("服务端尚未配置上游接口", 50001);

  const response = aegis.fetch(endpoint, {
    method: "POST",
    headers: { "Authorization": "Bearer " + apiKey, "Content-Type": "application/json" },
    body: { query: ctx.input.query || "" }
  });
  if (!response.ok) {
    aegis.audit.log("proxy.upstream_failed", "上游返回 " + response.status);
    aegis.fail("上游服务暂不可用", 50201);
  }

  return { ok: true, remaining: perDay - used, data: response.json };
}
`
