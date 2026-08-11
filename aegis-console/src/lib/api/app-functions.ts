import { apiRequest, buildQuery } from "./client";

/**
 * 应用级远程函数：版本不可变，激活即切换 active_version。
 * 详见 docs/app-functions.md。
 */
export type AppFunctionRuntime = "script" | "wasm" | "http";

export type AppFunction = {
  id: number;
  appId: number;
  name: string;
  description: string;
  runtime: AppFunctionRuntime;
  status: "draft" | "active" | "disabled";
  activeVersion?: string;
  capabilities: string[];
  timeoutMs: number;
  maxRequestBytes: number;
  maxResponseBytes: number;
  createdAt: string;
  updatedAt: string;
};

export type AppFunctionVersion = {
  id: number;
  functionId: number;
  appId: number;
  version: string;
  endpointUrl?: string;
  responsePublicKey?: string;
  artifactSha256: string;
  status: "staged" | "active" | "retired";
  createdAt: string;
  activatedAt?: string;
};

export type AppFunctionKey = {
  id: number;
  appId: number;
  name: string;
  keyPrefix: string;
  status: string;
  lastUsedAt?: string;
  createdAt: string;
  revokedAt?: string;
};

/** 明文 secret 只在创建响应里出现一次，服务端只存 SHA-256 摘要 */
export type CreatedAppFunctionKey = AppFunctionKey & { secret: string };

export type AppFunctionInvocation = {
  id: number;
  eventId: string;
  appId: number;
  functionId: number;
  versionId: number;
  callerType: string;
  status: string;
  durationMs: number;
  requestSha256: string;
  responseSha256?: string;
  errorMessage?: string;
  result?: { eventId: string; version: string; output?: unknown; effects?: unknown[] };
  createdAt: string;
};

export type AppFunctionResult = {
  eventId: string;
  version: string;
  output?: unknown;
  effects?: unknown[];
};

/**
 * 脚本能力。声明即授权：未声明的能力在脚本里根本不会被绑定，
 * `aegis.points` 之类的对象直接是 undefined，而不是调用时才报错。
 */
export const FUNCTION_CAPABILITIES: Array<{
  value: string;
  label: string;
  api: string;
  hint: string;
}> = [
  {
    value: "user.read",
    label: "读取用户状态",
    api: "aegis.user.get()",
    hint: "VIP 是否有效、积分、封禁状态 —— 反破解的根基"
  },
  {
    value: "points.write",
    label: "积分读写",
    api: "aegis.points.add / deduct",
    hint: "走积分流水，余额不足会被服务端拒绝"
  },
  { value: "vip.write", label: "发放 VIP", api: "aegis.vip.grant(days)", hint: "按天延长会员有效期" },
  { value: "kv.read", label: "读取 KV", api: "aegis.kv.get()", hint: "服务端独占状态，客户端读不到" },
  {
    value: "kv.write",
    label: "写入 KV",
    api: "aegis.kv.set / incr / del",
    hint: "incr 是原子的，适合做次数限制"
  },
  { value: "notification.send", label: "发送通知", api: "aegis.notify.send()", hint: "推送给当前调用者" },
  { value: "audit.write", label: "写审计日志", api: "aegis.audit.log()", hint: "留痕到平台审计" },
  {
    value: "http.fetch",
    label: "出站 HTTP",
    api: "aegis.fetch(url, options)",
    hint: "仅 HTTPS 且拒绝内网地址；风险最高，按需开启"
  }
];

/** 新建 script 函数时预填的骨架，直接体现「服务端独占状态」的写法 */
export const SCRIPT_TEMPLATE = `// 每次调用都是全新的运行时，没有跨请求状态。
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
  if (me.banned) {
    aegis.fail("账号已被封禁", 40311);
  }
  if (!me.vip) {
    aegis.fail("该功能仅限会员", 40310);
  }

  // 服务端独占的计数器：客户端既读不到也改不了
  const usedToday = aegis.kv.user.incr("quota:" + new Date().toISOString().slice(0, 10), 1, 86400);
  if (usedToday > 100) {
    aegis.fail("今日额度已用尽", 42901);
  }

  // 真正的业务逻辑放在这里。它依赖上面这些服务端状态，
  // 因此无法在客户端本地复现 —— 这正是把逻辑搬到服务端的意义。
  return {
    ok: true,
    remaining: 100 - usedToday,
    token: aegis.crypto.hmacSha256(String(me.id), ctx.input.nonce || "")
  };
}
`;

const appPath = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;
const functionPath = (appKey: string, name: string) =>
  `${appPath(appKey)}/functions/${encodeURIComponent(name)}`;

export function listAppFunctions(token: string, appKey: string) {
  return apiRequest<AppFunction[]>(`${appPath(appKey)}/functions`, { token });
}

export function createAppFunction(
  token: string,
  appKey: string,
  payload: {
    name: string;
    description?: string;
    runtime: AppFunctionRuntime;
    capabilities: string[];
    timeoutMs: number;
    maxRequestBytes: number;
    maxResponseBytes: number;
  }
) {
  return apiRequest<AppFunction>(`${appPath(appKey)}/functions`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAppFunction(
  token: string,
  appKey: string,
  name: string,
  payload: Partial<
    Pick<
      AppFunction,
      "description" | "status" | "capabilities" | "timeoutMs" | "maxRequestBytes" | "maxResponseBytes"
    >
  >
) {
  return apiRequest<AppFunction>(functionPath(appKey, name), {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAppFunction(token: string, appKey: string, name: string) {
  return apiRequest<{ deleted: boolean }>(functionPath(appKey, name), {
    method: "DELETE",
    token
  });
}

export function listAppFunctionVersions(token: string, appKey: string, name: string) {
  return apiRequest<AppFunctionVersion[]>(`${functionPath(appKey, name)}/versions`, { token });
}

export function createAppFunctionVersion(
  token: string,
  appKey: string,
  name: string,
  payload: {
    version: string;
    endpointUrl?: string;
    responsePublicKey?: string;
    wasmBase64?: string;
    /** script 运行时的脚本正文，只在管理端流转，永不下发给接入方 */
    source?: string;
  }
) {
  return apiRequest<AppFunctionVersion>(`${functionPath(appKey, name)}/versions`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function activateAppFunctionVersion(
  token: string,
  appKey: string,
  name: string,
  version: string
) {
  return apiRequest<{ version: string }>(
    `${functionPath(appKey, name)}/versions/${encodeURIComponent(version)}/activate`,
    { method: "POST", token }
  );
}

export function invokeAppFunction(
  token: string,
  appKey: string,
  name: string,
  payload: { eventId?: string; input: unknown }
) {
  return apiRequest<AppFunctionResult>(`${functionPath(appKey, name)}/invoke`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function listAppFunctionInvocations(
  token: string,
  appKey: string,
  name: string,
  limit = 50
) {
  return apiRequest<AppFunctionInvocation[]>(
    `${functionPath(appKey, name)}/invocations${buildQuery({ limit })}`,
    { token }
  );
}

export function listAppFunctionKeys(token: string, appKey: string) {
  return apiRequest<AppFunctionKey[]>(`${appPath(appKey)}/function-keys`, { token });
}

export function createAppFunctionKey(token: string, appKey: string, name: string) {
  return apiRequest<CreatedAppFunctionKey>(`${appPath(appKey)}/function-keys`, {
    method: "POST",
    token,
    body: JSON.stringify({ name })
  });
}

export function revokeAppFunctionKey(token: string, appKey: string, keyId: number) {
  return apiRequest<{ revoked: boolean }>(`${appPath(appKey)}/function-keys/${keyId}`, {
    method: "DELETE",
    token
  });
}
