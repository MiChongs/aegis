/**
 * 生成喂给 Monaco TypeScript 语言服务的 Aegis SDK 类型声明。
 *
 * 按当前函数「已声明的 capabilities」动态裁剪：没勾选的能力不会出现在 .d.ts 里，
 * 编辑器就会像运行时一样把 `aegis.points` 判为不存在。
 * 编辑期的可见性与沙箱的实际绑定保持一致，避免写完才发现调不通。
 */

const BASE = `
/** 远程函数调用上下文。由 Aegis 构造，客户端无法伪造其中的身份字段。 */
declare interface AegisCaller {
  /** user | admin | key */
  type: string;
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
}

declare interface AegisUser {
  id: number;
  account: string;
  /** 账号是否启用 */
  enabled: boolean;
  /** 是否处于封禁中（含临时封禁） */
  banned: boolean;
  /** VIP 是否仍在有效期内 —— 由服务端现算，客户端伪造不了 */
  vip: boolean;
  vipExpireAt?: string;
  vipRemainingSeconds?: number;
  integral: number;
  experience: number;
  createdAt: string;
  nickname?: string;
  /** 设备机器码 */
  markcode?: string;
  role?: string;
}

declare interface AegisFetchResult {
  status: number;
  text: string;
  /** 响应可被解析为 JSON 时存在 */
  json?: any;
}

declare interface AegisCrypto {
  sha256(input: string): string;
  hmacSha256(key: string, data: string): string;
  /** size 取值 1..64，默认 16 */
  randomHex(size?: number): string;
}
`.trim();

const KV_INTERFACE = `
declare interface AegisKVNamespace {
  /** 读取，不存在或已过期返回 null */
  get(key: string): any;
  /** 写入。ttlSeconds 省略或为 0 表示永不过期 */
  set(key: string, value: any, ttlSeconds?: number): void;
  /** 原子自增，返回自增后的值。频次限制、剩余次数依赖它的原子性 */
  incr(key: string, delta?: number, ttlSeconds?: number): number;
  del(key: string): boolean;
}

declare interface AegisKV extends AegisKVNamespace {
  /** 按调用者隔离的命名空间，脚本无法跨用户读写 */
  user: AegisKVNamespace;
}
`.trim();

type CapabilityMember = {
  /** 触发该成员的 capability */
  capability: string;
  /** 注入 AegisSDK 接口的成员声明 */
  member: string;
  /** 需要一并输出的接口定义 */
  extra?: string;
};

const CAPABILITY_MEMBERS: CapabilityMember[] = [
  {
    capability: "user.read",
    member: `
  /** 读取用户状态。省略 userId 时读当前调用者；跨应用的用户返回 null */
  user: {
    get(userId?: number): AegisUser | null;
  };`
  },
  {
    capability: "points.write",
    member: `
  /** 积分读写，走正式积分流水；余额不足会抛错 */
  points: {
    /** 增加积分，返回变更后余额 */
    add(amount: number, reason?: string): number;
    /** 扣减积分，返回变更后余额 */
    deduct(amount: number, reason?: string): number;
  };`
  },
  {
    capability: "vip.write",
    member: `
  vip: {
    /** 按天延长当前调用者的会员有效期 */
    grant(days: number, reason?: string): { days: number; userId: number } | null;
  };`
  },
  {
    capability: "kv",
    member: `
  /** 服务端独占的键值状态，客户端既读不到也伪造不了 */
  kv: AegisKV;`,
    extra: KV_INTERFACE
  },
  {
    capability: "notification.send",
    member: `
  notify: {
    /** 给当前调用者发通知，返回投递条数 */
    send(title: string, content: string, options?: { level?: string; type?: string }): number;
  };`
  },
  {
    capability: "audit.write",
    member: `
  audit: {
    /** 写入平台审计日志 */
    log(action: string, summary?: string): void;
  };`
  },
  {
    capability: "http.fetch",
    member: `
  /** 出站 HTTPS 请求。禁止重定向，且拒绝解析到内网/元数据地址的域名 */
  fetch(url: string, options?: {
    method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
    headers?: Record<string, string>;
    body?: any;
  }): AegisFetchResult;`
  }
];

/**
 * 依据已声明能力生成完整 .d.ts。
 *
 * kv.read / kv.write 任一存在即注入 kv 命名空间 —— 与运行时 bindKV 的判定一致。
 */
export function buildAegisSDKTypes(capabilities: string[]): string {
  const declared = new Set(capabilities);
  // 兼容 000056 时期的旧能力名
  if (declared.has("user.profile.read")) declared.add("user.read");
  if (declared.has("kv.read") || declared.has("kv.write")) declared.add("kv");

  const members: string[] = [];
  const extras: string[] = [];
  for (const entry of CAPABILITY_MEMBERS) {
    if (!declared.has(entry.capability)) continue;
    members.push(entry.member);
    if (entry.extra) extras.push(entry.extra);
  }

  return `${BASE}

${extras.join("\n\n")}

declare interface AegisSDK {
  /** 写服务端日志，永远不会返回给调用方 */
  log(...args: any[]): void;
  /** 主动返回业务错误并终止本次调用（如授权过期、次数用尽） */
  fail(message: string, code?: number): never;
  crypto: AegisCrypto;
${members.join("\n")}
}

/** 注入脚本的全局对象。未声明的能力在这里不会出现。 */
declare const aegis: AegisSDK;
`;
}
