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
  /** 单实例同时执行上限 */
  maxConcurrency: number;
  /** 每分钟调用上限，0 表示不限（计数落在数据库上，跨实例准确） */
  rateLimitPerMin: number;
  /** 函数级参数，脚本里读作 aegis.config；改它不需要发新版本 */
  config: Record<string, unknown>;
  /**
   * 入参契约（JSON Schema），`{}` 表示不约束。
   *
   * 一份声明驱动三处：调用入口的前置校验、试跑输入框的补全与校验、
   * 以及编辑器里 ctx.input 的真实类型。
   */
  inputSchema: Record<string, unknown>;
  /**
   * 由 inputSchema 生成的 TypeScript 声明，**后端生成**，只出网不入库。
   *
   * 前端不做这个转换：再写一个转换器就有了第二份真相，
   * 而两份类型不一致的表现是「补全里有这个字段、运行时却没有」。
   */
  inputTypes?: string;
  /** 按契约造出的示例 input（同样由后端生成），试跑输入框拿它预填 */
  inputSample?: string;
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
  /** 发版说明 —— 回滚时要靠它决定滚到哪一版 */
  notes: string;
  sourceBytes: number;
  artifactSha256: string;
  status: "staged" | "active" | "retired";
  createdAt: string;
  activatedAt?: string;
};

/** 只有管理端的版本详情接口会带脚本正文，接入方拿不到 */
export type AppFunctionVersionDetail = AppFunctionVersion & { source: string };

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

export type AppFunctionEffect = {
  type: string;
  arguments: unknown;
  /** 试跑产生的副作用没有真的发生，只是记下了脚本想做什么 */
  simulated?: boolean;
};

export type AppFunctionResult = {
  eventId: string;
  version: string;
  output?: unknown;
  effects?: AppFunctionEffect[];
};

export type AppFunctionInvocation = {
  id: number;
  eventId: string;
  appId: number;
  functionId: number;
  versionId: number;
  callerType: string;
  callerId?: number;
  status: string;
  durationMs: number;
  requestSha256: string;
  responseSha256?: string;
  errorMessage?: string;
  result?: AppFunctionResult;
  createdAt: string;
};

export type AppFunctionInvocationPage = {
  list: AppFunctionInvocation[];
  total: number;
  page: number;
  limit: number;
};

export type AppFunctionStats = {
  windowHours: number;
  total: number;
  success: number;
  failed: number;
  running: number;
  successRate: number;
  avgMs: number;
  p95Ms: number;
  maxMs: number;
  lastInvokedAt?: string;
  topErrors: Array<{ message: string; count: number }>;
  buckets: Array<{ at: string; success: number; failed: number }>;
};

/**
 * 脚本日志是结构化的，不是拼好的字符串。
 *
 * 级别要着色、要能过滤，而「从 "warn 内容" 里把级别切出来」
 * 在消息本身以 warn 开头时就会切错。elapsedMs 是相对执行起点的毫秒数：
 * 沙箱里没有计时器，「哪一步慢」只能靠日志之间的间隔看出来。
 */
export type AppFunctionLogEntry = {
  level: string;
  message: string;
  elapsedMs: number;
};

export type AppFunctionDiagnosticSeverity = "error" | "warning" | "info";

/**
 * 静态检查的一条结论。行列从 1 起算，可直接喂给 Monaco 的 marker。
 *
 * `capabilities` 非空时表示「补上这项声明就好了」，控制台据此给一键修复 ——
 * 否则作者要自己从错误文案里把能力键抄到设置页去。
 */
export type AppFunctionDiagnostic = {
  severity: AppFunctionDiagnosticSeverity;
  rule: string;
  message: string;
  line: number;
  column: number;
  endColumn?: number;
  capabilities?: string[];
};

export type AppFunctionAnalysis = {
  ok: boolean;
  diagnostics: AppFunctionDiagnostic[];
  /** 脚本实际用到的能力 */
  usedCapabilities: string[];
  sourceBytes: number;
};

export type AppFunctionTestResult = {
  ok: boolean;
  durationMs: number;
  output?: unknown;
  effects: AppFunctionEffect[];
  logs: AppFunctionLogEntry[];
  error?: string;
  /** 非 0 表示脚本自己调了 aegis.fail()，属于业务判定而不是崩溃 */
  businessCode?: number;
  /** 抛错位置，用来在编辑器上直接标红那一行 */
  errorLine?: number;
  errorColumn?: number;
  stack?: string[];
  /** 与发布门禁同一套检查，试跑时顺带回一份 */
  diagnostics: AppFunctionDiagnostic[];
  sdkCalls: number;
  sdkMutations: number;
  sdkFetches: number;
};

export type AppFunctionKvEntry = {
  scope: "app" | "user";
  scopeId: number;
  key: string;
  value: unknown;
  truncated?: boolean;
  expiresAt?: string;
  updatedAt: string;
};

export type AppFunctionKvPage = {
  list: AppFunctionKvEntry[];
  total: number;
  page: number;
  limit: number;
};

/**
 * 能力目录由**后端**下发（internal/domain/appfunction/capabilities.go）。
 *
 * 在这里另抄一份会同时招来「后端加了一项、控制台勾不上」和
 * 「控制台能勾、保存时报不支持」两种漂移，而两边都没有报错提示 ——
 * 与风控条件目录、支付渠道自述是同一条约束。
 */
export type FunctionCapability = {
  key: string;
  group: string;
  label: string;
  api: string;
  hint: string;
  risk: "low" | "medium" | "high";
  mutating: boolean;
  requiresUser: boolean;
  deprecated?: boolean;
  replacedBy?: string;
  namespace?: string;
  /** 这项能力贡献的成员名，服务端静态分析器据此反查「这行需要哪项能力」 */
  members?: string[];
  /** 注入编辑器类型的成员声明，由 buildAegisSDKTypes 拼装 */
  declaration?: string;
  interfaces?: string;
};

export type FunctionRuntimeLimits = {
  maxSdkCalls: number;
  maxSdkMutations: number;
  maxSdkFetches: number;
  maxSourceBytes: number;
  maxKvKeyLength: number;
  maxKvValueBytes: number;
  maxFetchBodyBytes: number;
  maxConfigBytes: number;
  maxTimeoutMs: number;
  maxConcurrency: number;
  maxLogLines: number;
  maxLockSeconds: number;
};

export type FunctionScriptTemplate = {
  key: string;
  title: string;
  summary: string;
  capabilities: string[];
  source: string;
  /** 模板自带的入参契约，建函数时一并写入 */
  inputSchema?: string;
  /**
   * 试跑输入框的预填内容。
   *
   * 没有它的话，从模板新建的函数第一次试跑必然失败 —— 默认那句
   * `{"action":"ping"}` 满足不了任何一个模板的入参要求。
   */
  sampleInput?: string;
};

export type FunctionCatalog = {
  capabilities: FunctionCapability[];
  limits: FunctionRuntimeLimits;
  templates: FunctionScriptTemplate[];
  /** 与能力无关、永远存在的那部分 .d.ts（AegisContext / AegisUser / AegisCrypto…） */
  baseTypes: string;
  runtimeDefault: AppFunctionRuntime;
  /** 入参契约的起步骨架，比对着空编辑器回忆 JSON Schema 关键字强 */
  inputSchemaTemplate: string;
};

/** 能力分组的展示名。分组键由后端定义，这里只负责翻译。 */
export const CAPABILITY_GROUP_LABELS: Record<string, string> = {
  identity: "用户与身份",
  asset: "资产",
  state: "服务端状态",
  reach: "触达",
  audit: "留痕",
  intel: "情报",
  egress: "出网",
  legacy: "旧能力（仅兼容存量）"
};

export const CAPABILITY_RISK_LABELS: Record<string, string> = {
  low: "低风险",
  medium: "中风险",
  high: "高风险"
};

const appPath = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;
const functionPath = (appKey: string, name: string) =>
  `${appPath(appKey)}/functions/${encodeURIComponent(name)}`;

export function getAppFunctionCatalog(token: string, appKey: string) {
  return apiRequest<FunctionCatalog>(`${appPath(appKey)}/function-catalog`, { token });
}

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
    timeoutMs?: number;
    maxRequestBytes?: number;
    maxResponseBytes?: number;
    maxConcurrency?: number;
    rateLimitPerMin?: number;
    config?: Record<string, unknown>;
    inputSchema?: Record<string, unknown>;
  }
) {
  return apiRequest<AppFunction>(`${appPath(appKey)}/functions`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export type AppFunctionUpdate = Partial<
  Pick<
    AppFunction,
    | "description"
    | "status"
    | "capabilities"
    | "timeoutMs"
    | "maxRequestBytes"
    | "maxResponseBytes"
    | "maxConcurrency"
    | "rateLimitPerMin"
    | "config"
    | "inputSchema"
  >
>;

export function updateAppFunction(
  token: string,
  appKey: string,
  name: string,
  payload: AppFunctionUpdate
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

/** 取回某一版的脚本正文 —— 「派生新版本」与「查看历史」都靠它 */
export function getAppFunctionVersion(
  token: string,
  appKey: string,
  name: string,
  version: string
) {
  return apiRequest<AppFunctionVersionDetail>(
    `${functionPath(appKey, name)}/versions/${encodeURIComponent(version)}`,
    { token }
  );
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
    notes?: string;
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

export function deleteAppFunctionVersion(
  token: string,
  appKey: string,
  name: string,
  version: string
) {
  return apiRequest<{ deleted: boolean }>(
    `${functionPath(appKey, name)}/versions/${encodeURIComponent(version)}`,
    { method: "DELETE", token }
  );
}

/**
 * 试跑：不创建版本、不写调用审计、写操作只记录不执行。
 *
 * 失败时后端仍返回 200 —— 作者要的是错误内容加上那之前的日志与副作用清单。
 * 因此这里判断成功与否要看 `result.ok`，不是看有没有抛异常。
 */
export function testAppFunction(
  token: string,
  appKey: string,
  name: string,
  payload: {
    source: string;
    input?: unknown;
    config?: Record<string, unknown>;
    asUserId?: number;
    timeoutMs?: number;
  }
) {
  return apiRequest<AppFunctionTestResult>(`${functionPath(appKey, name)}/test`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/**
 * 静态检查：不执行任何代码，只回诊断。
 *
 * 与试跑分开是因为两者回答的不是同一个问题：试跑要用户身份、要真实读库、
 * 要几百毫秒；而作者敲代码时想知道的只是「我现在这份能不能发出去」。
 * 后端走的是与发布门禁**同一套**判定，因此不会出现「这里全绿、发布被拦」。
 */
export function analyzeAppFunction(
  token: string,
  appKey: string,
  name: string,
  source: string
) {
  return apiRequest<AppFunctionAnalysis>(`${functionPath(appKey, name)}/analyze`, {
    method: "POST",
    token,
    body: JSON.stringify({ source })
  });
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
  query: { status?: string; callerType?: string; eventId?: string; page?: number; limit?: number } = {}
) {
  return apiRequest<AppFunctionInvocationPage>(
    `${functionPath(appKey, name)}/invocations${buildQuery({ ...query })}`,
    { token }
  );
}

export function getAppFunctionStats(token: string, appKey: string, name: string, hours = 24) {
  return apiRequest<AppFunctionStats>(
    `${functionPath(appKey, name)}/stats${buildQuery({ hours })}`,
    { token }
  );
}

export function listAppFunctionKv(
  token: string,
  appKey: string,
  query: { scope?: string; scopeId?: number; prefix?: string; page?: number; limit?: number } = {}
) {
  return apiRequest<AppFunctionKvPage>(
    `${appPath(appKey)}/function-kv${buildQuery({ ...query })}`,
    { token }
  );
}

export function deleteAppFunctionKv(
  token: string,
  appKey: string,
  entry: { scope: string; scopeId: number; key: string }
) {
  return apiRequest<{ deleted: boolean }>(
    `${appPath(appKey)}/function-kv${buildQuery({ ...entry })}`,
    { method: "DELETE", token }
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
