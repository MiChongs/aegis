import type { FunctionCapability } from "@/lib/api/app-functions";

/**
 * 生成喂给 Monaco TypeScript 语言服务的 Aegis SDK 类型声明。
 *
 * **声明片段来自后端能力目录**（internal/domain/appfunction/capabilities.go），
 * 不是这里的一份副本。理由与勾选框读目录相同：SDK 真正绑定什么由 Go 决定，
 * 在前端另写一份类型，只会让「补全里有、运行时没有」这种错误
 * 一直拖到发版之后才暴露 —— 而编辑器提示的全部价值就是提前暴露它。
 *
 * 本文件只剩两件事：按已声明能力过滤，以及把片段拼成合法的 .d.ts。
 * 命名空间合并（user.read 出 get、user.write 出 ban，同属 aegis.user）
 * 必须在这里做对：漏了会在同一个接口里产生两个 `user:` 成员，
 * TypeScript 报重复声明，然后整份类型静默失效 —— 表现是补全突然什么都没有。
 */

/** 目录还没到位时的兜底：至少让 handle(ctx) 有类型，编辑器不至于满屏红。 */
const FALLBACK_BASE = `
declare interface AegisCaller {
  type: "user" | "admin" | "app";
  userId?: number;
  adminId?: number;
  keyId?: number;
}

declare interface AegisContext {
  eventId: string;
  appId: number;
  appKey: string;
  function: string;
  version: string;
  caller: AegisCaller;
  input: any;
  dryRun: boolean;
}

declare interface AegisCrypto { [key: string]: any }
declare interface AegisTime { [key: string]: any }
declare interface AegisConfig { [key: string]: any }
`.trim();

const SDK_HEAD = `declare interface AegisSDK {
  /** 写服务端日志，永远不会返回给调用方。console.log 是它的别名 */
  log(...args: any[]): void;
  /** 主动返回业务错误并终止本次调用（如授权过期、次数用尽） */
  fail(message: string, code?: number): never;
  crypto: AegisCrypto;
  time: AegisTime;
  /** 函数级配置，控制台上可改，改完立即生效、不需要发新版本 */
  config: AegisConfig;`;

const SDK_TAIL = `
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
`.trim();

export type SDKTypeSource = {
  /** 后端下发的基础类型（AegisContext / AegisUser / AegisCrypto …） */
  baseTypes?: string;
  capabilities: FunctionCapability[];
};

/**
 * 依据已声明能力生成完整 .d.ts。
 *
 * 旧能力名（user.profile.read）的映射由目录里的 `replacedBy` 负责，
 * 在前端再写一遍映射表就是第二份真相。
 */
export function buildAegisSDKTypes(declaredKeys: string[], source?: SDKTypeSource): string {
  const catalog = source?.capabilities ?? [];
  const declared = new Set(declaredKeys);
  for (const capability of catalog) {
    if (capability.replacedBy && declared.has(capability.key)) {
      declared.add(capability.replacedBy);
    }
  }

  const rootMembers: string[] = [];
  const namespaceOrder: string[] = [];
  const namespaceBody = new Map<string, string[]>();
  const interfaces: string[] = [];
  const seen = new Set<string>();

  for (const capability of catalog) {
    if (capability.deprecated || !capability.declaration) continue;
    if (!declared.has(capability.key)) continue;

    if (capability.namespace) {
      if (!namespaceBody.has(capability.namespace)) {
        namespaceOrder.push(capability.namespace);
        namespaceBody.set(capability.namespace, []);
      }
      namespaceBody.get(capability.namespace)!.push(capability.declaration.replace(/\n+$/, ""));
    } else if (!seen.has(capability.declaration)) {
      // kv.read / kv.write 贡献的是同一行成员，按文本去重
      rootMembers.push(capability.declaration.replace(/\n+$/, ""));
      seen.add(capability.declaration);
    }

    if (capability.interfaces && !seen.has(capability.interfaces)) {
      interfaces.push(capability.interfaces.trim());
      seen.add(capability.interfaces);
    }
  }

  const members = [
    ...rootMembers,
    ...namespaceOrder.map(
      (namespace) => `  ${namespace}: {${namespaceBody.get(namespace)!.join("")}\n  };`
    )
  ];

  return [
    (source?.baseTypes || FALLBACK_BASE).trim(),
    "",
    ...(interfaces.length ? [interfaces.join("\n\n"), ""] : []),
    SDK_HEAD,
    ...members,
    "}",
    "",
    SDK_TAIL,
    ""
  ].join("\n");
}
