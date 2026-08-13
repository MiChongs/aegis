package appfunction

import (
	"encoding/json"
	"strings"
)

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
	CapLockAcquire      = "lock.acquire"
	CapNotificationSend = "notification.send"
	CapRealtimePush     = "realtime.push"
	CapEmailSend        = "email.send"
	CapAuditWrite       = "audit.write"
	CapGeoRead          = "geo.read"
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
	CapGroupIntel    = "intel"
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
	// Members 这项能力贡献的成员名。有 Namespace 时是命名空间**内**的成员
	// （user.read 出 get / entitlement），无 Namespace 时是挂在 aegis 根上的成员。
	//
	// 它存在的唯一理由是让**静态分析器**能反查「`aegis.points.add` 需要哪项能力」。
	// 从 Declaration 里正则抠成员名也能凑合，但那份文本是给人看的 TypeScript，
	// 改一次注释就可能让反查静默失灵 —— 而失灵的表现是「发布通过、调用时 TypeError」，
	// 恰好是这套检查要拦的那件事。`TestCapabilityMembersAppearInDeclaration` 钉住两者一致。
	Members []string `json:"members,omitempty"`
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
		Members:   []string{"get", "entitlement"},
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
		Members:   []string{"ban", "unban"},
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
		Members:   []string{"add", "deduct"},
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
		Members:   []string{"status", "hasFeature"},
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
		Members:   []string{"grant", "revoke"},
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
		Members:   []string{"get"},
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
		Members:   []string{"adjust"},
		Declaration: `
    /** 调整当前调用者余额。amount 传字符串以免丢精度；正数入账、负数扣减 */
    adjust(amount: string | number, reason?: string): AegisWallet;`,
	},
	{
		Key: CapKVRead, Group: CapGroupState,
		Label: "读取 KV", API: "aegis.kv.get / list / has",
		Hint: "服务端独占状态，客户端读不到也伪造不了",
		Risk:    RiskLow,
		Members: []string{"kv"},
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
		Members: []string{"kv"},
		Declaration: `
  /** 服务端独占的键值状态，客户端既读不到也伪造不了 */
  kv: AegisKV;`,
		Interfaces: kvInterfaces,
	},
	{
		Key: CapLockAcquire, Group: CapGroupState,
		Label: "分布式锁", API: "aegis.lock.acquire(key, ttl)",
		Hint: "跨实例互斥。「先查后写」那段临界区没有它就挡不住并发重复领取",
		Risk: RiskMedium, Mutating: true,
		Namespace: "lock",
		Members:   []string{"acquire", "release", "run"},
		Declaration: `
    /** 抢一把锁。抢到返回令牌，没抢到返回 null（不阻塞、不排队） */
    acquire(key: string, ttlSeconds?: number): string | null;
    /** 归还锁。令牌不匹配时不做任何事并返回 false —— 不会误删别人续上的那把 */
    release(key: string, token: string): boolean;
    /** 在锁内跑一段。抢不到锁抛错；无论正常返回还是抛错都会释放 */
    run<T>(key: string, fn: () => T, ttlSeconds?: number): T;`,
	},
	{
		Key: CapNotificationSend, Group: CapGroupReach,
		Label: "发送站内信", API: "aegis.notify.send()",
		Hint: "推送给当前调用者，进 notifications 表并实时下发",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "notify",
		Members:   []string{"send"},
		Declaration: `
    /** 给当前调用者发站内信，返回投递条数 */
    send(title: string, content: string, options?: { level?: "info" | "warning" | "critical"; type?: string }): number;`,
	},
	{
		Key: CapRealtimePush, Group: CapGroupReach,
		Label: "实时推送", API: "aegis.realtime.send(event, data)",
		Hint: "只推给当前调用者已连上的客户端，不落库、不补发 —— 掉线即丢",
		Risk: RiskMedium, Mutating: true, RequiresUser: true,
		Namespace: "realtime",
		Members:   []string{"send"},
		Declaration: `
    /** 给当前调用者的在线连接推一条事件。离线时返回 false，不会排队等他上线 */
    send(event: string, data?: Record<string, any>): boolean;`,
	},
	{
		Key: CapEmailSend, Group: CapGroupReach,
		Label: "发送邮件", API: "aegis.email.send()",
		Hint: "恒发往调用者账号绑定的邮箱 —— 脚本填不了收件人",
		Risk: RiskHigh, Mutating: true, RequiresUser: true,
		Namespace: "email",
		Members:   []string{"send"},
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
		Members:   []string{"log"},
		Declaration: `
    /** 写入平台审计日志 */
    log(action: string, summary?: string): void;`,
	},
	{
		Key: CapGeoRead, Group: CapGroupIntel,
		Label: "IP 归属地", API: "aegis.geo.lookup(ip)",
		Hint: "GeoIP2 + ASN，查不到时返回的是「未知」而不是 null，判断请看 resolved",
		Risk:      RiskLow,
		Namespace: "geo",
		Members:   []string{"lookup"},
		Declaration: `
    /** 查 IP 归属地与运营商。查不到时 resolved 为 false，其余字段是空串 */
    lookup(ip: string): AegisGeoLocation;`,
		Interfaces: `
declare interface AegisGeoLocation {
  ip: string;
  /** 查到了才是 true。内网地址与查不到的地址都是 false */
  resolved: boolean;
  country: string;
  countryCode: string;
  region: string;
  city: string;
  timezone: string;
  isp: string;
  asn: string;
  latitude?: number;
  longitude?: number;
  /** 内网 / 回环 / 链路本地地址 */
  private: boolean;
}`,
	},
	{
		Key: CapHTTPFetch, Group: CapGroupEgress,
		Label: "出站 HTTP", API: "aegis.fetch(url, options)",
		Hint: "仅 HTTPS、禁重定向、连接时重新解析并拒绝内网与云元数据地址",
		Risk: RiskHigh, Mutating: true,
		Members: []string{"fetch"},
		Declaration: `
  /** 出站 HTTPS 请求。禁止重定向，且拒绝解析到内网 / 元数据地址的域名 */
  fetch(url: string, options?: {
    method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
    headers?: Record<string, string>;
    /** 对象会被 JSON 序列化；字符串原样发送 */
    body?: any;
    /** 表单编码的请求体（自动带 Content-Type），与 body 二选一 */
    form?: Record<string, string>;
    /** 查询参数，会拼到 url 上 */
    query?: Record<string, string>;
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
  /** 调用方传入的 input 字段。配了入参 schema 时这里是它生成出来的具体类型 */
  input: AegisInput;
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
  /** Keccak 家族的 SHA-3，与 sha256 不是同一个算法，别拿它对接 SHA-2 的签名 */
  sha3(input: string): string;
  /** CRC32（IEEE 多项式）十六进制。校验和不是摘要，别拿它防篡改 */
  crc32(input: string): string;
  hmacMd5(key: string, data: string): string;
  hmacSha1(key: string, data: string): string;
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
  /** 密码学随机字节的 base64。size 取值 1..64 */
  randomBytes(size?: number): string;
  /** 闭区间 [min, max] 的密码学随机整数 */
  randomInt(min: number, max: number): number;
  uuid(): string;
  /** 定长比较，不会因为提前返回而泄露前缀信息 */
  timingSafeEqual(a: string, b: string): boolean;
  /** AES-256-GCM 加密，返回 base64（nonce 已拼在密文前）。key 任意长度，内部按 SHA-256 派生 */
  aesEncrypt(key: string, plaintext: string): string;
  /** AES-256-GCM 解密。密文被改过会抛错而不是返回垃圾 */
  aesDecrypt(key: string, ciphertext: string): string;
  /** 签发 HS256/HS384/HS512 的 JWT。claims 里没有 iat 时自动补 */
  jwtSign(secret: string, claims: Record<string, any>, options?: { alg?: "HS256" | "HS384" | "HS512"; expiresIn?: number }): string;
  /** 校验并解出 JWT。签名不对、已过期都返回 { valid: false, error }，不抛错 */
  jwtVerify(secret: string, token: string): { valid: boolean; claims?: Record<string, any>; error?: string };
  /** 校验 TOTP 动态口令（RFC 6238，默认 30 秒步长，容忍前后各一个窗口） */
  totpVerify(secret: string, code: string): boolean;
  /** bcrypt 哈希。cost 取值 4..14，默认 10 */
  bcryptHash(password: string, cost?: number): string;
  bcryptVerify(hash: string, password: string): boolean;
  /** PBKDF2-HMAC-SHA256，返回十六进制 */
  pbkdf2(password: string, salt: string, iterations?: number, keyLength?: number): string;
  /** HKDF-SHA256 派生，返回十六进制 */
  hkdf(secret: string, salt: string, info: string, keyLength?: number): string;
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
  /** ISO 周键 "YYYY-Www"（周一起算），做周榜与周额度用它 */
  weekKey(offsetWeeks?: number): string;
  /** 按时区取日键。做「按北京时间日切」的额度时用 "Asia/Shanghai" */
  dayKeyIn(timezone: string, offsetDays?: number): string;
  /** 用 Go 的参考时间布局格式化（如 "2006-01-02 15:04:05"），timezone 省略即 UTC */
  format(layout: string, unixMillis?: number, timezone?: string): string;
  /** 解析时间字符串为 Unix 毫秒；解析不出来返回 0 */
  parse(value: string, layout?: string): number;
  /** 在某个时刻上加减，返回 Unix 毫秒 */
  add(unixMillis: number, amount: number, unit?: "second" | "minute" | "hour" | "day" | "month" | "year"): number;
  /** 两个时刻相差多少（默认秒） */
  diff(fromUnixMillis: number, toUnixMillis: number, unit?: "second" | "minute" | "hour" | "day"): number;
  /** 当天零点（按时区），Unix 毫秒 */
  startOfDay(unixMillis?: number, timezone?: string): number;
  /** cron 表达式的下一个触发时刻（Unix 毫秒）；表达式非法抛错 */
  cronNext(expression: string, afterUnixMillis?: number): number;
}

/** 文本处理。都是纯计算，不需要声明任何能力 */
declare interface AegisText {
  /** Go text/template 渲染。逻辑放模板里比在脚本里拼字符串安全（自动转义留给 sanitizeHtml） */
  template(tpl: string, data: Record<string, any>): string;
  /** 转成 URL 友好的短横线串 */
  slugify(input: string): string;
  /** 中文转拼音。style: "normal"（默认）/ "tone" / "initials" */
  pinyin(input: string, style?: "normal" | "tone" | "initials"): string;
  /** 遮掉邮箱本地部分：z***g@example.com */
  maskEmail(input: string): string;
  /** 遮掉手机号中间四位 */
  maskPhone(input: string): string;
  /** 按 Unicode 字符数截断（不会把汉字劈成两半），超出时补省略号 */
  truncate(input: string, length: number, ellipsis?: string): string;
  /** 富文本转纯文本 */
  stripHtml(input: string): string;
  /** 按白名单净化 HTML：放行排版标签，拒绝 style 与事件属性 */
  sanitizeHtml(input: string): string;
  /** 转义成可安全插进 HTML 的文本 */
  escapeHtml(input: string): string;
  /** Unicode 字符数（不是字节数） */
  length(input: string): number;
}

/** 编解码。接第三方接口时最常用的那几种格式 */
declare interface AegisEncoding {
  yamlParse(input: string): any;
  yamlStringify(value: any): string;
  /** 解析 CSV，第一行为表头时返回对象数组 */
  csvParse(input: string, options?: { header?: boolean; delimiter?: string }): any[];
  csvStringify(rows: any[], options?: { header?: boolean; columns?: string[] }): string;
  /** XML 转普通对象。对接老系统时省掉一整套手写解析 */
  xmlToJson(input: string): any;
  /** 对象转 XML，root 默认 "xml"（微信支付那套报文就是这个形状） */
  jsonToXml(value: Record<string, any>, root?: string): string;
  /** 解析 query string，重复键取数组 */
  queryParse(input: string): Record<string, any>;
  /** 拼 query string，键按字典序排列 —— 签名场景要的就是稳定顺序 */
  queryStringify(value: Record<string, any>): string;
  urlEncode(input: string): string;
  urlDecode(input: string): string;
  /** gzip 压缩，返回 base64 */
  gzip(input: string): string;
  /** 解 base64 gzip */
  gunzip(base64Input: string): string;
}

/** 定点小数。钱的加减乘除一律走它 —— JS 的 number 是双精度浮点，0.1+0.2 落在钱上就是对不上账 */
declare interface AegisDecimal {
  add(a: string | number, b: string | number): string;
  sub(a: string | number, b: string | number): string;
  mul(a: string | number, b: string | number): string;
  /** 除法必须给精度，否则 1/3 没有终止小数 */
  div(a: string | number, b: string | number, scale?: number): string;
  /** -1 / 0 / 1 */
  cmp(a: string | number, b: string | number): number;
  /** 四舍五入到指定小数位 */
  round(value: string | number, scale?: number): string;
  abs(value: string | number): string;
  isZero(value: string | number): boolean;
  /** 按千分位格式化，用于展示 */
  format(value: string | number, scale?: number): string;
}

/** JSON 路径取值。深层可选取值不必写一串 a && a.b && a.b[0] */
declare interface AegisJSONUtil {
  /** 按路径取值，如 "data.items.0.name"；取不到返回 undefined */
  get(value: any, path: string): any;
  /** 路径是否存在（区分「不存在」与「值是 null」） */
  exists(value: any, path: string): boolean;
  /** 带缩进的 JSON 文本，调试用 */
  pretty(value: any): string;
  /** 解析失败返回 fallback 而不是抛错 */
  parse(text: string, fallback?: any): any;
}

/** 入参校验。写在脚本第一段，把「输入不对」和「逻辑不对」分开 */
declare interface AegisValidate {
  /** 按 JSON Schema 校验，返回全部错误（不是遇到第一个就停） */
  schema(schema: Record<string, any>, value: any): { valid: boolean; errors: string[] };
  email(value: string): boolean;
  url(value: string): boolean;
  ip(value: string): boolean;
  uuid(value: string): boolean;
  /** 中国大陆手机号 */
  phone(value: string): boolean;
  json(value: string): boolean;
}

declare interface AegisUserAgentInfo {
  /** 浏览器 / 客户端名 */
  name: string;
  version: string;
  os: string;
  osVersion: string;
  /** 机型字符串，常为空 —— 要判「是不是手机」请看 kind */
  device: string;
  /** desktop / mobile / tablet / bot */
  kind: "desktop" | "mobile" | "tablet" | "bot";
  mobile: boolean;
  tablet: boolean;
  desktop: boolean;
  bot: boolean;
}

/** 控制台上可随时改、不需要发新版本的函数级参数 */
declare interface AegisConfig {
  [key: string]: any;
}

// ── 沙箱里真实存在的标准全局 ────────────────────────────────────────
// 这里没有的东西运行时就没有。document / window / setTimeout / require
// 一个都不存在，编辑器会如实标红。

/** Node 风格的字节缓冲。纯内存类型，没有任何文件或网络能力 */
declare interface AegisBuffer extends Uint8Array {
  toString(encoding?: string, start?: number, end?: number): string;
  equals(other: AegisBuffer): boolean;
  slice(start?: number, end?: number): AegisBuffer;
}

declare const Buffer: {
  from(value: string | ArrayBuffer | ArrayLike<number>, encoding?: string): AegisBuffer;
  alloc(size: number, fill?: number): AegisBuffer;
  concat(list: AegisBuffer[], totalLength?: number): AegisBuffer;
  byteLength(value: string, encoding?: string): number;
  isBuffer(value: any): boolean;
};

declare class URLSearchParams {
  constructor(init?: string | Record<string, string> | URLSearchParams);
  append(name: string, value: string): void;
  delete(name: string): void;
  get(name: string): string | null;
  getAll(name: string): string[];
  has(name: string): boolean;
  set(name: string, value: string): void;
  sort(): void;
  forEach(callback: (value: string, name: string) => void): void;
  toString(): string;
}

declare class URL {
  constructor(input: string, base?: string);
  hash: string;
  host: string;
  hostname: string;
  href: string;
  readonly origin: string;
  password: string;
  pathname: string;
  port: string;
  protocol: string;
  search: string;
  readonly searchParams: URLSearchParams;
  username: string;
  toString(): string;
}

declare class TextEncoder {
  readonly encoding: string;
  encode(input?: string): Uint8Array;
}

declare class TextDecoder {
  constructor(label?: string);
  readonly encoding: string;
  decode(input?: ArrayBuffer | Uint8Array): string;
}

/** base64 → 二进制串（与浏览器同名同语义） */
declare function atob(input: string): string;
/** 二进制串 → base64 */
declare function btoa(input: string): string;
`

// SDKDeclaration 按已声明的能力拼出完整 .d.ts。
//
// 放在服务端而不是控制台，是为了让「编辑器提示什么」与「运行时绑定什么」
// 只有一处定义。控制台拿到目录后原样拼接，不再自己维护一份类型。
func SDKDeclaration(capabilityKeys []string) string {
	return SDKDeclarationWithInput(capabilityKeys, nil)
}

// SDKDeclarationWithInput 在能力类型之外，再按入参 schema 生成 `ctx.input` 的具体类型。
//
// 分成两个函数而不是加一个参数：绝大多数调用方（目录下发、测试）不关心入参，
// 而让它们全部传一个 nil 只是噪音。
func SDKDeclarationWithInput(capabilityKeys []string, inputSchema json.RawMessage) string {
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
	// 入参类型：没配 schema 时退回 any，与加这项之前的行为一致。
	// 注意它必须在 BaseDeclaration 之后 —— AegisContext 引用了它。
	if declaration := InputSchemaDeclaration(inputSchema); declaration != "" {
		builder.WriteString(declaration)
		builder.WriteString("\n\n")
	} else {
		builder.WriteString("/** 未配置入参 schema，因此这里是 any。配好之后 ctx.input 会有真实字段 */\n")
		builder.WriteString("declare type " + InputTypeName + " = any;\n\n")
	}
	for _, block := range interfaces {
		builder.WriteString(block)
		builder.WriteString("\n\n")
	}
	builder.WriteString(`declare interface AegisSDK {
  /** 写服务端日志，永远不会返回给调用方。console.log 是它的别名 */
  log(...args: any[]): void;
  /** 主动返回业务错误并终止本次调用（如授权过期、次数用尽） */
  fail(message: string, code?: number): never;
  /** 断言。条件不成立即以 40001 终止，省掉一堆 if + fail */
  assert(condition: any, message: string, code?: number): void;
  crypto: AegisCrypto;
  time: AegisTime;
  text: AegisText;
  encoding: AegisEncoding;
  /** 定点小数运算，钱的加减乘除走它 */
  decimal: AegisDecimal;
  json: AegisJSONUtil;
  validate: AegisValidate;
  /** 解析 User-Agent 字符串 */
  ua: { parse(userAgent: string): AegisUserAgentInfo };
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

// BaseSDKMembers 是免声明即可用的那批 `aegis` 根成员。
//
// 静态分析器靠它区分「这个成员本来就有」与「这个成员需要声明能力」。
// 顺序即控制台上的展示顺序，与 SDKDeclaration 里的 AegisSDK 头部一一对应。
func BaseSDKMembers() []string {
	return []string{
		"log", "fail", "assert", "crypto", "time",
		"text", "encoding", "decimal", "json", "validate", "ua", "config",
	}
}

// SandboxGlobals 是沙箱里真实存在的全局标识符。
//
// 它与「禁用全局」是同一枚硬币的两面：这里没有的名字在运行时就是
// ReferenceError，因此静态分析器可以在发布前把它说出来，而不是等一次真实调用。
func SandboxGlobals() []string {
	return []string{
		"aegis", "console", "handle",
		"Buffer", "URL", "URLSearchParams", "TextEncoder", "TextDecoder", "atob", "btoa",
	}
}

// CapabilitiesForMember 反查「`aegis.<root>` 或 `aegis.<root>.<member>` 需要哪项能力」。
//
//	known  — 这个成员在目录里存在（false 表示 SDK 上压根没有它）
//	needed — 能满足它的能力键；任意一项被声明即可（user.get 只要 user.read，
//	         而裸取 `aegis.user` 无从判断要读还是要写，两项都算数）
//
// member 传空串表示只看根成员。
func CapabilitiesForMember(root, member string) (needed []string, known bool) {
	for _, base := range BaseSDKMembers() {
		if base == root {
			// 免声明成员的下级不再校验：crypto / text 这些是纯计算，
			// 逐个函数名钉死只会让加一个工具函数变成一次跨仓库改动。
			return nil, true
		}
	}
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		if capability.Deprecated {
			continue
		}
		if capability.Namespace != "" {
			if capability.Namespace != root {
				continue
			}
			// 命名空间对上了：没指定成员，或成员确实由这项能力贡献
			if member != "" && !containsString(capability.Members, member) {
				continue
			}
		} else if !containsString(capability.Members, root) {
			continue
		}
		if _, duplicate := seen[capability.Key]; duplicate {
			continue
		}
		seen[capability.Key] = struct{}{}
		needed = append(needed, capability.Key)
	}
	if len(needed) > 0 {
		return needed, true
	}
	// 命名空间存在但成员名拼错了（aegis.user.gett）：仍然算「知道这个命名空间」，
	// 由调用方按成员级别报错，否则会误报成「缺少能力声明」把人带偏。
	for _, capability := range capabilities {
		if !capability.Deprecated && capability.Namespace == root {
			return nil, false
		}
	}
	return nil, false
}

// NamespaceExists 目录里有没有这个命名空间（不论成员名对不对）。
func NamespaceExists(root string) bool {
	for _, base := range BaseSDKMembers() {
		if base == root {
			return true
		}
	}
	for _, capability := range capabilities {
		if capability.Deprecated {
			continue
		}
		if capability.Namespace == root || containsString(capability.Members, root) {
			return true
		}
	}
	return false
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
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
	// MaxLogLines 试跑回传的日志行数上限。作者会写循环打日志，
	// 而「后面的日志去哪了」必须能在界面上答得出来。
	MaxLogLines int `json:"maxLogLines"`
	// MaxLockSeconds 单把分布式锁的最长持有时间
	MaxLockSeconds int `json:"maxLockSeconds"`
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
		{
			Key:     "jwt",
			Title:   "签发短期访问令牌",
			Summary: "给接入方的自有服务签一张带权益的 JWT，密钥不出服务端",
			Capabilities: []string{
				CapUserRead, CapVipRead, CapKVRead,
			},
			Source: templateJWT,
		},
		{
			Key:     "lock",
			Title:   "加锁的一次性发放",
			Summary: "临界区加分布式锁：并发重复提交在多实例下也只发一次",
			Capabilities: []string{
				CapUserRead, CapKVRead, CapKVWrite, CapLockAcquire, CapPointsWrite,
			},
			Source: templateLock,
		},
		{
			Key:     "webhook",
			Title:   "校验第三方回调",
			Summary: "验签 + 防重放 + 入参 schema 校验，三件事都在服务端做完",
			Capabilities: []string{
				CapKVRead, CapKVWrite, CapAuditWrite,
			},
			Source: templateWebhook,
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

const templateJWT = `// 给接入方的自有服务签一张短期令牌。
//
// 令牌里写的是**服务端算出来的**权益，签名密钥只存在 KV 里 ——
// 接入方的服务只需验签，不必再回头问一次「这个人是不是会员」。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  aegis.assert(me, "需要用户身份调用", 40100);
  aegis.assert(!me.banned, "账号不可用", 40311);

  const secret = String(aegis.kv.get("jwt-secret") || "");
  aegis.assert(secret, "服务端尚未配置签名密钥", 50001);

  const ttl = Number(aegis.config.tokenTtlSeconds || 900);
  const entitlement = aegis.vip.status();

  const token = aegis.crypto.jwtSign(secret, {
    sub: String(me.id),
    aud: ctx.appKey,
    vip: entitlement ? entitlement.isVip : false,
    features: entitlement ? entitlement.features : [],
    jti: aegis.crypto.uuid()
  }, { alg: "HS256", expiresIn: ttl });

  return { token: token, expiresIn: ttl, tokenType: "Bearer" };
}
`

const templateLock = `// 一次性发放，临界区上锁。
//
// 只用 kv.has 判重挡不住并发：两个请求可能同时读到「没领过」。
// 锁是跨实例的，因此多副本部署下同样只放行一个。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const me = aegis.user.get();
  aegis.assert(me, "需要用户身份调用", 40100);

  const task = String(ctx.input.task || "").trim();
  aegis.assert(task, "缺少 task 参数", 40001);

  const claimKey = "claim:" + task;

  // run() 在锁内执行，无论正常返回还是抛错都会释放；抢不到锁直接抛错
  return aegis.lock.run("claim:" + me.id + ":" + task, function () {
    if (aegis.kv.user.has(claimKey)) {
      aegis.fail("该奖励已领取", 40901);
    }
    const points = Number(aegis.config.rewardPoints || 100);
    aegis.kv.user.set(claimKey, { at: aegis.time.iso() }, 0);
    const balance = aegis.points.add(points, "一次性奖励 " + task);
    return { ok: true, points: points, balance: balance };
  }, 10);
}
`

const templateWebhook = `// 校验第三方回调。
//
// 三件事缺一不可：报文形状对不对、签名是不是对方发的、这条是不是重放的。
// 前两件在客户端做等于没做，第三件客户端根本做不了。

/** @param {AegisContext} ctx */
function handle(ctx) {
  const shape = aegis.validate.schema({
    type: "object",
    required: ["orderNo", "amount", "timestamp", "sign"],
    properties: {
      orderNo: { type: "string", minLength: 1 },
      amount: { type: "string" },
      timestamp: { type: "number" },
      sign: { type: "string" }
    }
  }, ctx.input);
  if (!shape.valid) aegis.fail("回调报文不合法：" + shape.errors.join("; "), 40001);

  // 时间戳窗口：签名合法但十天前的报文同样不该被接受
  const skew = Math.abs(aegis.time.unix() - Number(ctx.input.timestamp));
  if (skew > Number(aegis.config.maxSkewSeconds || 300)) {
    aegis.fail("回调时间戳超出容忍窗口", 40002);
  }

  // 签名串按字典序拼，与对方文档一致；queryStringify 保证顺序稳定
  const secret = String(aegis.kv.get("webhook-secret") || "");
  aegis.assert(secret, "服务端尚未配置回调密钥", 50001);
  const expected = aegis.crypto.hmacSha256(secret, aegis.encoding.queryStringify({
    orderNo: ctx.input.orderNo,
    amount: ctx.input.amount,
    timestamp: ctx.input.timestamp
  }));
  if (!aegis.crypto.timingSafeEqual(expected, String(ctx.input.sign))) {
    aegis.audit.log("webhook.bad_signature", "订单 " + ctx.input.orderNo);
    aegis.fail("签名校验失败", 40103);
  }

  // 防重放：同一订单号只处理一次，键留 7 天
  const seen = aegis.kv.incr("webhook:" + ctx.input.orderNo, 1, 604800);
  if (seen > 1) return { ok: true, replayed: true };

  aegis.audit.log("webhook.accepted", "订单 " + ctx.input.orderNo);
  return { ok: true, replayed: false, orderNo: ctx.input.orderNo };
}
`
