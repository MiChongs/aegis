import {
  Boxes,
  CloudDownload,
  Cpu,
  Globe2,
  Network,
  Route,
  Server,
  Split,
  type LucideIcon
} from "lucide-react";
import type { Tone } from "@/components/users/detail/kit";
import type {
  EgressAction,
  EgressEndpoint,
  EgressGatewayData,
  EgressGraphCatalog,
  EgressMatch,
  EgressNodeType,
  EgressRuleData,
  EgressStrategy,
  MihomoCoreData,
  MihomoFieldSpec,
  MihomoGroupData,
  MihomoProviderData,
  MihomoProxyData,
  MihomoRulesData,
  MihomoTypeSpec
} from "@/lib/api/egress";

/**
 * 出海网关画布的展示元数据：节点与端口的颜色图标、动作与策略文案、新节点的默认配置、
 * 卡片上的一句话摘要，以及 mihomo 键值里点号路径的读写。
 */

export type EgressNodeVisual = { icon: LucideIcon; tone: Tone; accent: string };

export const NODE_VISUAL: Record<EgressNodeType, EgressNodeVisual> = {
  gateway: { icon: Globe2, tone: "primary", accent: "#6366f1" },
  rule: { icon: Route, tone: "violet", accent: "#8b5cf6" },
  endpoint: { icon: Server, tone: "info", accent: "#0ea5e9" },
  m_group: { icon: Boxes, tone: "warning", accent: "#f59e0b" },
  m_proxy: { icon: Network, tone: "success", accent: "#10b981" },
  m_provider: { icon: CloudDownload, tone: "info", accent: "#06b6d4" },
  m_rules: { icon: Split, tone: "danger", accent: "#ec4899" },
  m_core: { icon: Cpu, tone: "neutral", accent: "#64748b" }
};

/** 端口语义色：规则链靛蓝、路由去向翠绿、经由天蓝、成员琥珀、前置代理石板灰。 */
export const PORT_COLOR: Record<string, string> = {
  rules: "#6366f1",
  next: "#6366f1",
  hit: "#10b981",
  fallback: "#10b981",
  via: "#0ea5e9",
  members: "#f59e0b",
  dialer: "#64748b"
};

export const ACTION_LABEL: Record<EgressAction, string> = { proxy: "走代理", direct: "直连", reject: "拒绝" };

export const STRATEGY_LABEL: Record<EgressStrategy, string> = {
  failover: "故障转移",
  round_robin: "轮询",
  random: "随机",
  weighted: "加权随机",
  latency: "最低延迟"
};

export const PROTOCOL_LABEL: Record<string, string> = {
  direct: "直连",
  http: "HTTP",
  https: "HTTPS",
  socks5: "SOCKS5",
  socks5h: "SOCKS5（远端解析）",
  ssh: "SSH 隧道",
  trojan: "Trojan",
  shadowsocks: "Shadowsocks"
};

export const CORE_STATUS_LABEL: Record<string, { label: string; tone: Tone }> = {
  disabled: { label: "未启用", tone: "neutral" },
  missing: { label: "未安装", tone: "warning" },
  installing: { label: "安装中", tone: "info" },
  starting: { label: "启动中", tone: "info" },
  running: { label: "运行中", tone: "success" },
  crashed: { label: "异常退出", tone: "danger" },
  stopped: { label: "已停止", tone: "neutral" },
  failed: { label: "启动失败", tone: "danger" }
};

// ── 点号路径读写（mihomo 的嵌套键：ws-opts.path） ──

export function getPath(source: Record<string, unknown> | undefined, path: string): unknown {
  let current: unknown = source;
  for (const key of path.split(".")) {
    if (!current || typeof current !== "object") return undefined;
    current = (current as Record<string, unknown>)[key];
  }
  return current;
}

function isEmpty(value: unknown) {
  return (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0) ||
    (typeof value === "object" && !Array.isArray(value) && Object.keys(value as object).length === 0)
  );
}

/** 不可变地写入；写空值时删掉这个键，父对象空了也一并删掉，免得配置里堆满空壳。 */
export function setPath(source: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const [head, ...rest] = path.split(".");
  const next = { ...source };
  if (!rest.length) {
    if (isEmpty(value) && value !== false && value !== 0) delete next[head];
    else next[head] = value;
    return next;
  }
  const child = next[head] && typeof next[head] === "object" && !Array.isArray(next[head]) ? (next[head] as Record<string, unknown>) : {};
  const updated = setPath(child, rest.join("."), value);
  if (Object.keys(updated).length === 0) delete next[head];
  else next[head] = updated;
  return next;
}

/** 字段是否显示（showWhen：另一个字段取这些值；"*" 表示任意非空）。 */
export function fieldVisible(spec: MihomoFieldSpec, values: Record<string, unknown>) {
  if (!spec.showWhen) return true;
  return Object.entries(spec.showWhen).every(([key, allowed]) => {
    const raw = getPath(values, key);
    if (allowed.includes("*")) return !isEmpty(raw);
    const text = raw === undefined || raw === null ? "" : String(raw);
    return allowed.includes(text);
  });
}

// ── 新节点的默认配置 ──

let counter = 0;
export function shortCode() {
  counter = (counter + 1) % 1296;
  return `${Math.random().toString(36).slice(2, 5)}${counter.toString(36)}`;
}

function defaultsFor(spec: MihomoTypeSpec | undefined) {
  let out: Record<string, unknown> = {};
  for (const field of spec?.fields ?? []) {
    if (field.default !== undefined && field.default !== "" && !field.showWhen) out = setPath(out, field.key, field.default);
  }
  return out;
}

export function defaultProxy(type: string, catalog: EgressGraphCatalog, name?: string): Record<string, unknown> {
  const spec = catalog.mihomo.proxyTypes.find((item) => item.type === type);
  const base: Record<string, unknown> = { name: name ?? `${spec?.label ?? type} ${shortCode()}`, type };
  if (spec?.server) Object.assign(base, { server: "", port: 443 });
  return { ...base, ...defaultsFor(spec) };
}

export function defaultGroup(type: string, catalog: EgressGraphCatalog, name?: string): Record<string, unknown> {
  const spec = catalog.mihomo.groupTypes.find((item) => item.type === type);
  return { name: name ?? `${spec?.label ?? "策略组"} ${shortCode()}`, type, ...defaultsFor(spec) };
}

export function defaultNodeData(type: EgressNodeType, catalog: EgressGraphCatalog, index = 1): Record<string, unknown> {
  switch (type) {
    case "gateway":
      return { ...catalog.defaultGateway };
    case "rule":
      return { name: `规则 ${index}`, action: "proxy", match: { domainSuffixes: [] } } satisfies EgressRuleData;
    case "endpoint":
      return {
        name: `line-${shortCode()}`,
        protocol: "socks5h",
        address: "",
        tls: { enabled: false },
        ssh: {},
        shadowsocks: {}
      } satisfies EgressEndpoint;
    case "m_proxy":
      return { proxy: defaultProxy("vless", catalog) } satisfies MihomoProxyData;
    case "m_group":
      return { group: defaultGroup("select", catalog) } satisfies MihomoGroupData;
    case "m_provider":
      return {
        name: `订阅 ${shortCode()}`,
        provider: {
          type: "http",
          url: "",
          interval: 3600,
          "health-check": { enable: true, url: catalog.mihomo.defaultTestUrl, interval: 600, lazy: true }
        }
      } satisfies MihomoProviderData;
    case "m_rules":
      return { rules: [{ id: shortCode(), type: "MATCH", target: "DIRECT" }], ruleProviders: {} } satisfies MihomoRulesData;
    case "m_core":
      return JSON.parse(JSON.stringify(catalog.defaultCore)) as MihomoCoreData;
  }
}

// ── 卡片摘要 ──

export function matchSummary(match: EgressMatch | undefined): string[] {
  if (!match) return ["没有匹配条件"];
  if (match.matchAll) return ["匹配全部流量"];
  const parts: string[] = [];
  const list = (label: string, items?: Array<string | number>) => {
    if (!items?.length) return;
    const head = items.slice(0, 3).join("、");
    parts.push(`${label} ${head}${items.length > 3 ? ` 等 ${items.length} 项` : ""}`);
  };
  list("后缀", match.domainSuffixes);
  list("域名", match.domains);
  list("关键字", match.domainKeywords);
  list("正则", match.domainRegexps);
  list("网段", match.cidrs);
  list("端口", match.ports);
  list("协议", match.schemes);
  list("调用方", match.profiles);
  if (match.networks?.length === 1) parts.push(`仅 ${match.networks[0].toUpperCase()}`);
  if (match.excludeDomainSuffixes?.length) parts.push(`排除 ${match.excludeDomainSuffixes.length} 个后缀`);
  return parts.length ? parts : ["没有匹配条件"];
}

export function maskedLabel(value: unknown, secretKept: string) {
  if (value === secretKept) return "已保存";
  return typeof value === "string" ? value : "";
}

function hostOf(raw: unknown, secretKept: string) {
  if (raw === secretKept) return "地址已保存";
  if (typeof raw !== "string" || !raw) return "未填地址";
  try {
    return new URL(raw).host;
  } catch {
    return raw.slice(0, 32);
  }
}

export function nodeTitle(type: EgressNodeType, data: Record<string, unknown>) {
  switch (type) {
    case "gateway":
      return "网关设置";
    case "rule":
    case "endpoint":
    case "m_provider":
      return (data.name as string) || "未命名";
    case "m_proxy":
      return ((data.proxy as Record<string, unknown>)?.name as string) || "未命名节点";
    case "m_group":
      return ((data.group as Record<string, unknown>)?.name as string) || "未命名策略组";
    case "m_rules":
      return "mihomo 分流";
    case "m_core":
      return "mihomo 核心";
  }
}

export function nodeSummary(type: EgressNodeType, data: Record<string, unknown>, catalog?: EgressGraphCatalog): string[] {
  const secretKept = catalog?.secretKept ?? "";
  switch (type) {
    case "gateway": {
      const gateway = data as unknown as EgressGatewayData;
      return [
        gateway.enabled ? "网关已启用" : "网关未启用，所有出站直连",
        `未命中时${ACTION_LABEL[gateway.defaultAction ?? "direct"]}`,
        gateway.health?.enabled ? `每 ${gateway.health.intervalSeconds ?? 60} 秒探测线路` : "不主动探测线路"
      ];
    }
    case "rule": {
      const rule = data as unknown as EgressRuleData;
      return [...matchSummary(rule.match).slice(0, 2), `命中后${ACTION_LABEL[rule.action ?? "proxy"]}`];
    }
    case "endpoint": {
      const endpoint = data as unknown as EgressEndpoint;
      const lines = [`${PROTOCOL_LABEL[endpoint.protocol] ?? endpoint.protocol}${endpoint.address ? `  ${endpoint.address}` : ""}`];
      if (endpoint.region) lines.push(`地区 ${endpoint.region}`);
      return lines;
    }
    case "m_proxy": {
      const proxy = ((data as MihomoProxyData).proxy ?? {}) as Record<string, unknown>;
      const spec = catalog?.mihomo.proxyTypes.find((item) => item.type === proxy.type);
      const server = proxy.server ? `${proxy.server as string}${proxy.port ? `:${String(proxy.port)}` : ""}` : spec?.server ? "未填服务器" : "";
      const extras: string[] = [];
      if (proxy.network && proxy.network !== "tcp") extras.push(String(proxy.network));
      if (getPath(proxy, "reality-opts.public-key")) extras.push("REALITY");
      else if (proxy.tls) extras.push("TLS");
      if (proxy.flow) extras.push("Vision");
      return [[spec?.label ?? String(proxy.type ?? "未知类型"), ...extras].join(" / "), server].filter(Boolean);
    }
    case "m_group": {
      const group = ((data as MihomoGroupData).group ?? {}) as Record<string, unknown>;
      const spec = catalog?.mihomo.groupTypes.find((item) => item.type === group.type);
      const filter = group.filter ? `筛选 ${String(group.filter)}` : "";
      return [spec?.label ?? String(group.type ?? "策略组"), filter].filter(Boolean);
    }
    case "m_provider": {
      const provider = ((data as MihomoProviderData).provider ?? {}) as Record<string, unknown>;
      const kind = provider.type === "file" ? "本地文件" : provider.type === "inline" ? "内联" : hostOf(provider.url, secretKept);
      const interval = Number(provider.interval ?? 0);
      return [kind, interval ? `每 ${interval >= 3600 ? `${Math.round(interval / 3600)} 小时` : `${Math.round(interval / 60)} 分钟`}更新` : "不自动更新"];
    }
    case "m_rules": {
      const rules = (data as MihomoRulesData).rules ?? [];
      const sets = Object.keys((data as MihomoRulesData).ruleProviders ?? {}).length;
      return [`${rules.length} 条规则${sets ? `，${sets} 个规则集` : ""}`];
    }
    case "m_core": {
      const core = data as unknown as MihomoCoreData;
      return [`${core.settings?.mode === "global" ? "全局" : core.settings?.mode === "direct" ? "直连" : "规则"}模式，日志 ${core.settings?.logLevel ?? "info"}`, core.dnsEnabled ? "核心 DNS 已启用" : "使用系统 DNS"];
    }
  }
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let index = 0;
  let size = value;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index++;
  }
  return `${size >= 100 || index === 0 ? Math.round(size) : size.toFixed(1)} ${units[index]}`;
}

export function formatSpeed(value: number) {
  return `${formatBytes(value)}/s`;
}

/** 延迟的颜色：越快越绿。0 表示超时或未测。 */
export function delayTone(delay: number | undefined): Tone {
  if (!delay) return "neutral";
  if (delay < 200) return "success";
  if (delay < 500) return "warning";
  return "danger";
}
