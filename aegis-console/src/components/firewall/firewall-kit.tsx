"use client";

import type { ReactNode } from "react";
import { Ban, Eye, FileText, Lock, LockOpen, Radar, ScanLine, Server, ShieldCheck, Waypoints } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { FirewallAction, FirewallRuleAction, FirewallStage, FirewallTransport } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";

/**
 * 防火墙工作台共用的文案与小部件。所有页签的中文说法都从这里取，别在组件里各写一套。
 */

/** 事件原因 → 一句话（「为什么拦」） */
export const REASON_LABEL: Record<string, string> = {
  waf_blocked: "WAF 拦截",
  waf_observed: "观察模式命中",
  waf_outbound: "出站泄露",
  waf_processing_error: "WAF 处理失败",
  body_too_large: "请求体超过检查上限",
  custom_rule: "自定义规则拦截",
  custom_log: "自定义规则记录",
  rate_limited: "触发限流",
  blocked_signature: "命中快速签名",
  blocked_path: "探测敏感路径",
  blocked_user_agent: "扫描器 UA",
  blocked_cidr: "黑名单网段",
  not_in_allowlist: "不在白名单",
  blocked_method: "禁用的请求方法",
  path_too_long: "路径过长",
  query_too_long: "查询串过长",
  invalid_ip: "无法识别的 IP",
  banned_ip: "已封禁 IP"
};

export function reasonLabel(reason: string) {
  if (REASON_LABEL[reason]) return REASON_LABEL[reason];
  if (reason.startsWith("banned_")) return "已封禁 IP";
  return reason || "未知";
}

export const ACTION_META: Record<FirewallAction, { label: string; tone: "danger" | "warning" | "info"; icon: typeof Ban }> = {
  block: { label: "拦截", tone: "danger", icon: Ban },
  observe: { label: "观察", tone: "warning", icon: Eye },
  log: { label: "记录", tone: "info", icon: FileText }
};

export const STAGE_META: Record<FirewallStage, { label: string; hint: string; icon: typeof Radar }> = {
  edge: { label: "入口", hint: "解密之前：IP、封禁、签名、自定义规则、限流", icon: Radar },
  payload: { label: "载荷", hint: "解密之后：Coraza WAF 检查明文", icon: ScanLine },
  response: { label: "出站", hint: "响应加密之前：数据泄露检测（只记录）", icon: Server }
};

export const TRANSPORT_META: Record<FirewallTransport, { label: string; hint: string; icon: typeof Lock }> = {
  plain: { label: "明文", hint: "未加密的请求", icon: LockOpen },
  sealed: { label: "Transport v2", hint: "接入网关 sealed 档：解密后检测", icon: Lock },
  legacy: { label: "旧加密", hint: "X-Aegis-Encrypted：解密后检测", icon: Waypoints }
};

export const RULE_ACTION_META: Record<FirewallRuleAction, { label: string; hint: string; tone: "danger" | "success" | "info" | "warning" }> = {
  block: { label: "拦截", hint: "直接拒绝，可同时封禁来源 IP", tone: "danger" },
  allow: { label: "放行", hint: "跳过其余入口检查与 WAF（封禁仍先生效）", tone: "success" },
  skip_waf: { label: "跳过 WAF", hint: "入口检查照常，只是这次不进 WAF", tone: "warning" },
  log: { label: "记录", hint: "记一条事件，继续往下检查", tone: "info" }
};

/** 事件严重性（防火墙自己的四档） */
export const SEVERITY_META: Record<string, { label: string; tone: "danger" | "warning" | "info" | "secondary"; dot: string }> = {
  critical: { label: "严重", tone: "danger", dot: "bg-red-500" },
  high: { label: "高危", tone: "warning", dot: "bg-orange-500" },
  medium: { label: "中危", tone: "info", dot: "bg-amber-400" },
  low: { label: "低危", tone: "secondary", dot: "bg-slate-400" }
};

/** CRS 规则自身的严重性 */
export const RULE_SEVERITY_META: Record<string, { label: string; className: string }> = {
  critical: { label: "严重", className: "border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400" },
  error: { label: "错误", className: "border-orange-500/30 bg-orange-500/10 text-orange-600 dark:text-orange-400" },
  warning: { label: "警告", className: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400" },
  notice: { label: "提示", className: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-400" }
};

/** 规则组中文名（与后端 catalog.go 的 groupMeta 一致；目录接口没拉到时兜底用） */
export const GROUP_NAME: Record<string, string> = {
  aegis: "Aegis 专属",
  baseline: "引擎基线",
  "901": "初始化",
  "905": "常见例外",
  "911": "方法限制",
  "913": "扫描器识别",
  "920": "协议规范",
  "921": "协议攻击",
  "922": "多段表单攻击",
  "930": "本地文件包含",
  "931": "远程文件包含",
  "932": "远程命令执行",
  "933": "PHP 注入",
  "934": "通用攻击",
  "941": "跨站脚本",
  "942": "SQL 注入",
  "943": "会话固定",
  "944": "Java 攻击",
  "949": "入站阻断评估",
  "950": "数据泄露",
  "951": "SQL 错误泄露",
  "952": "Java 错误泄露",
  "953": "PHP 错误泄露",
  "954": "IIS 错误泄露",
  "955": "WebShell 特征",
  "956": "Ruby 错误泄露",
  "959": "出站阻断评估",
  "980": "关联统计"
};

export function groupName(code: string | undefined | null) {
  if (!code) return "其他";
  return GROUP_NAME[code] ?? code;
}

/** 规则 ID → 所在组（CRS 规则 ID 的前三位就是文件编号） */
export function groupOfRule(id: number) {
  if (id >= 1100000 && id < 1200000) return "aegis";
  if (id >= 200000 && id < 300000) return "baseline";
  const text = String(id);
  return text.length === 6 ? text.slice(0, 3) : "";
}

/** 偏执等级的说法 */
export const PARANOIA_META: Record<number, { label: string; hint: string }> = {
  1: { label: "PL1 基础", hint: "误报最少，覆盖常见攻击（推荐）" },
  2: { label: "PL2 增强", hint: "加入编码绕过与更多关键字，需要先观察误报" },
  3: { label: "PL3 严格", hint: "对特殊字符非常敏感，适合高价值接口" },
  4: { label: "PL4 偏执", hint: "几乎任何特殊字符都会计分，仅限排查" }
};

/** 阈值预设 */
export const THRESHOLD_PRESETS = [
  { value: 5, label: "严格", hint: "一条严重命中即拦（CRS 官方值，推荐）" },
  { value: 10, label: "均衡", hint: "两条严重命中才拦" },
  { value: 20, label: "宽松", hint: "四条严重命中才拦，仅用于过渡期" }
] as const;

export function ToneBadge({ tone, children, className }: { tone: "danger" | "warning" | "info" | "success" | "secondary"; children: ReactNode; className?: string }) {
  return (
    <Badge variant={tone === "secondary" ? "secondary" : tone} size="sm" className={className}>
      {children}
    </Badge>
  );
}

export function ActionBadge({ action, className }: { action: FirewallAction; className?: string }) {
  const meta = ACTION_META[action] ?? ACTION_META.block;
  const Icon = meta.icon;
  return (
    <ToneBadge tone={meta.tone} className={className}>
      <Icon className="size-3" />
      {meta.label}
    </ToneBadge>
  );
}

export function TransportBadge({ transport, className }: { transport: FirewallTransport; className?: string }) {
  const meta = TRANSPORT_META[transport] ?? TRANSPORT_META.plain;
  const Icon = meta.icon;
  return (
    <span
      title={meta.hint}
      className={cn(
        "inline-flex h-5 items-center gap-1 rounded-md border px-1.5 text-[11px] font-medium",
        transport === "plain" ? "text-muted-foreground" : "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
        className
      )}
    >
      <Icon className="size-3" />
      {meta.label}
    </span>
  );
}

export function SeverityDot({ severity, className }: { severity: string; className?: string }) {
  const meta = SEVERITY_META[severity] ?? SEVERITY_META.medium;
  return <span title={meta.label} className={cn("inline-block size-2 shrink-0 rounded-full", meta.dot, className)} />;
}

export function RuleSeverityBadge({ severity, className }: { severity: string; className?: string }) {
  const meta = RULE_SEVERITY_META[severity];
  if (!meta) return null;
  return (
    <span className={cn("inline-flex h-5 items-center rounded-md border px-1.5 text-[11px] font-medium", meta.className, className)}>
      {meta.label}
    </span>
  );
}

/** 规则编号胶囊（等宽，点开看详情由调用方决定） */
export function RuleIdPill({ id, className, onClick }: { id: number; className?: string; onClick?: () => void }) {
  const Comp = onClick ? "button" : "span";
  return (
    <Comp
      type={onClick ? "button" : undefined}
      onClick={onClick}
      className={cn(
        "inline-flex h-5 items-center rounded-md bg-muted px-1.5 font-mono text-[11px] tabular-nums text-foreground/80",
        onClick && "transition-colors hover:bg-accent hover:text-foreground",
        className
      )}
    >
      #{id}
    </Comp>
  );
}

/** 「防火墙已关闭 / 观察模式」这类全局状态条 */
export function FirewallModeIcon({ detectionOnly, enabled, className }: { detectionOnly: boolean; enabled: boolean; className?: string }) {
  if (!enabled) return <ShieldCheck className={cn("size-4 text-muted-foreground", className)} />;
  return detectionOnly ? <Eye className={cn("size-4 text-amber-500", className)} /> : <ShieldCheck className={cn("size-4 text-emerald-500", className)} />;
}

/** 字节数 → 「13 MiB」 */
export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0";
  const units = ["B", "KiB", "MiB", "GiB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

/** 秒 → 「10 分钟」「永久」 */
export function formatBanSeconds(seconds: number) {
  if (seconds < 0) return "永久";
  if (seconds === 0) return "不封禁";
  if (seconds % 86400 === 0) return `${seconds / 86400} 天`;
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}
