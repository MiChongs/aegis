"use client";

import { useState, type ReactNode } from "react";
import {
  ArrowRight,
  Ban,
  BadgeCheck,
  Boxes,
  Fingerprint,
  Shuffle,
  Smartphone,
  CircleCheck,
  CircleDashed,
  Copy,
  CreditCard,
  Eye,
  Flame,
  Globe2,
  KeyRound,
  ListChecks,
  LockKeyhole,
  MapPin,
  MessageSquareText,
  Network,
  Server,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
  Timer,
  TriangleAlert,
  Undo2,
  UserRound,
  Wifi,
  type LucideIcon
} from "lucide-react";
import { toast } from "sonner";
import type { ThemedColor } from "@/components/commerce/commerce-charts";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type {
  RegIPAction,
  RegIPDecision,
  RegIPDevice,
  RegIPDeviceAction,
  RegIPLinked,
  RegIPEventKind,
  RegIPListKind,
  RegIPMethod,
  RegIPOutcome,
  RegIPPolicy,
  RegIPReason,
  RegIPRuleHit,
  RegIPScope
} from "@/lib/api/register-ip";
import { cn } from "@/lib/utils";

/**
 * 注册 IP 控制的展示口径与小部件：主体、处置、原因、记录类型、名单类型的文案 / 图标 / 配色，
 * 以及配额条、判定横幅、窗口选择、处置切换等。概览、策略、查询、名单、记录与模拟说同一套话。
 */

export { Block, DeviceLimitPager as Pager, formatCount, LimitStepper } from "@/components/apps/device-limit/device-limit-shared";

type BadgeVariant = "danger" | "warning" | "info" | "success" | "secondary" | "outline";

export const SCOPES: RegIPScope[] = ["ip", "subnet", "device", "ip_devices", "device_ips", "city", "app"];

/** 数的是「不同成员数」：地址下的不同设备、设备下的不同地址。必须有窗口 */
export function distinctScope(scope: RegIPScope) {
  return scope === "ip_devices" || scope === "device_ips";
}

/** 统计对象是设备：没有有效设备标识时不参与 */
export function deviceScope(scope: RegIPScope) {
  return scope === "device" || scope === "device_ips";
}

export const SCOPE_META: Record<RegIPScope, { label: string; short: string; icon: LucideIcon; tint: string; hint: string }> = {
  ip: {
    label: "同一地址",
    short: "地址",
    icon: Wifi,
    tint: "bg-indigo-500/10 text-indigo-600 ring-indigo-500/20 dark:text-indigo-400",
    hint: "IPv4 单个地址；IPv6 按单地址前缀聚合"
  },
  subnet: {
    label: "同一网段",
    short: "网段",
    icon: Network,
    tint: "bg-sky-500/10 text-sky-600 ring-sky-500/20 dark:text-sky-400",
    hint: "按网段前缀聚合，挡住同一机房的轮换地址"
  },
  device: {
    label: "同一设备",
    short: "设备",
    icon: Smartphone,
    tint: "bg-fuchsia-500/10 text-fuchsia-600 ring-fuchsia-500/20 dark:text-fuchsia-400",
    hint: "按设备标识计；换网络、开代理都是同一台"
  },
  ip_devices: {
    label: "地址下的设备",
    short: "地址→设备",
    icon: Boxes,
    tint: "bg-amber-500/10 text-amber-600 ring-amber-500/20 dark:text-amber-400",
    hint: "同一地址出现的不同设备数：群控、模拟器农场；老设备再来不算"
  },
  device_ips: {
    label: "设备换的地址",
    short: "设备→地址",
    icon: Shuffle,
    tint: "bg-rose-500/10 text-rose-600 ring-rose-500/20 dark:text-rose-400",
    hint: "同一设备用过的不同地址数：代理池轮换"
  },
  city: {
    label: "同一城市",
    short: "城市",
    icon: MapPin,
    tint: "bg-teal-500/10 text-teal-600 ring-teal-500/20 dark:text-teal-400",
    hint: "国家 / 省 / 市；属地查不到时不参与"
  },
  app: {
    label: "整个应用",
    short: "应用",
    icon: Globe2,
    tint: "bg-violet-500/10 text-violet-600 ring-violet-500/20 dark:text-violet-400",
    hint: "全应用的硬上限，必须有统计窗口"
  }
};

export const ACTIONS: RegIPAction[] = ["reject", "challenge", "observe"];

export const ACTION_META: Record<RegIPAction, { label: string; icon: LucideIcon; variant: BadgeVariant; active: string }> = {
  reject: { label: "拒绝", icon: Ban, variant: "danger", active: "bg-rose-500/12 text-rose-700 ring-rose-500/30 dark:text-rose-300" },
  challenge: {
    label: "加验",
    icon: ShieldQuestion,
    variant: "warning",
    active: "bg-amber-500/12 text-amber-700 ring-amber-500/30 dark:text-amber-300"
  },
  observe: { label: "观察", icon: Eye, variant: "info", active: "bg-sky-500/12 text-sky-700 ring-sky-500/30 dark:text-sky-300" }
};

export const METHODS: RegIPMethod[] = ["password", "sms", "oauth", "cardkey"];

export const METHOD_META: Record<RegIPMethod, { label: string; icon: LucideIcon }> = {
  password: { label: "密码", icon: KeyRound },
  sms: { label: "短信", icon: MessageSquareText },
  oauth: { label: "第三方", icon: UserRound },
  cardkey: { label: "卡密", icon: CreditCard }
};

export function methodLabel(method?: string | null) {
  return METHOD_META[method as RegIPMethod]?.label ?? (method || "未知");
}

export const REASON_META: Record<Exclude<RegIPReason, "">, { label: string; icon: LucideIcon }> = {
  disabled: { label: "未开启", icon: CircleDashed },
  missing_ip: { label: "缺少 IP", icon: ShieldAlert },
  missing_device: { label: "缺少设备", icon: Smartphone },
  invalid_device: { label: "无效设备", icon: Fingerprint },
  list_allow: { label: "放行名单", icon: ShieldCheck },
  list_deny: { label: "拒绝名单", icon: Ban },
  penalty: { label: "惩罚期", icon: LockKeyhole },
  geo: { label: "属地限制", icon: MapPin },
  isp: { label: "运营商限制", icon: Server },
  surge: { label: "注册突增", icon: Flame },
  rule: { label: "配额规则", icon: ListChecks },
  failure: { label: "存储故障", icon: TriangleAlert }
};

export function reasonLabel(reason?: string | null) {
  return reason ? (REASON_META[reason as Exclude<RegIPReason, "">]?.label ?? reason) : "—";
}

export const EVENT_KINDS: RegIPEventKind[] = ["rejected", "challenged", "challenge_passed", "observed", "penalized", "released"];

/** `color` 给图表，`variant` 给徽标：两套写的是同一组色 */
export const KIND_META: Record<RegIPEventKind, { label: string; icon: LucideIcon; variant: BadgeVariant; color: ThemedColor }> = {
  rejected: { label: "拒绝", icon: Ban, variant: "danger", color: { light: "#f43f5e", dark: "#fb7185" } },
  challenged: { label: "要求加验", icon: ShieldQuestion, variant: "warning", color: { light: "#f59e0b", dark: "#fbbf24" } },
  challenge_passed: { label: "加验后放行", icon: BadgeCheck, variant: "success", color: { light: "#10b981", dark: "#34d399" } },
  observed: { label: "观察", icon: Eye, variant: "info", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  penalized: { label: "进入惩罚期", icon: LockKeyhole, variant: "danger", color: { light: "#8b5cf6", dark: "#a78bfa" } },
  released: { label: "管理员解除", icon: Undo2, variant: "outline", color: { light: "#a1a1aa", dark: "#71717a" } }
};

export const REGISTERED_COLOR: ThemedColor = { light: "#6366f1", dark: "#818cf8" };

export const LIST_KINDS: RegIPListKind[] = ["allow", "deny", "elevated"];

export const LIST_META: Record<RegIPListKind, { label: string; description: string; icon: LucideIcon; variant: BadgeVariant; tint: string }> = {
  allow: {
    label: "放行",
    description: "不受任何规则与惩罚期约束",
    icon: ShieldCheck,
    variant: "success",
    tint: "bg-emerald-500/10 text-emerald-600 ring-emerald-500/20 dark:text-emerald-400"
  },
  deny: {
    label: "拒绝",
    description: "一律不许注册",
    icon: Ban,
    variant: "danger",
    tint: "bg-rose-500/10 text-rose-600 ring-rose-500/20 dark:text-rose-400"
  },
  elevated: {
    label: "放宽",
    description: "配额乘以倍数，适合校园网、大型 NAT 出口",
    icon: Timer,
    variant: "info",
    tint: "bg-sky-500/10 text-sky-600 ring-sky-500/20 dark:text-sky-400"
  }
};

export const OUTCOME_META: Record<RegIPOutcome, { label: string; icon: LucideIcon; tone: string; ring: string }> = {
  allow: {
    label: "放行",
    icon: CircleCheck,
    tone: "text-emerald-700 dark:text-emerald-300",
    ring: "border-emerald-500/30 bg-emerald-500/[0.06]"
  },
  challenge: {
    label: "要求图形验证码",
    icon: ShieldQuestion,
    tone: "text-amber-700 dark:text-amber-300",
    ring: "border-amber-500/30 bg-amber-500/[0.06]"
  },
  reject: { label: "拒绝注册", icon: Ban, tone: "text-rose-700 dark:text-rose-300", ring: "border-rose-500/30 bg-rose-500/[0.06]" }
};

/* ───────────────────── 时长 ───────────────────── */

export const WINDOW_PRESETS = [
  { value: 0, label: "累计" },
  { value: 600, label: "10 分钟" },
  { value: 3600, label: "1 小时" },
  { value: 6 * 3600, label: "6 小时" },
  { value: 86400, label: "1 天" },
  { value: 7 * 86400, label: "7 天" },
  { value: 30 * 86400, label: "30 天" }
];

/** 与后端 regip.WindowLabel 一致 */
export function windowLabel(seconds: number) {
  if (!seconds) return "累计";
  if (seconds % 86400 === 0) return seconds === 86400 ? "每天" : `每 ${seconds / 86400} 天`;
  if (seconds % 3600 === 0) return seconds === 3600 ? "每小时" : `每 ${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `每 ${seconds / 60} 分钟`;
  return `每 ${seconds} 秒`;
}

/** 与后端 regip.DurationLabel 一致 */
export function durationLabel(seconds: number) {
  if (seconds < 60) return `${Math.max(seconds, 1)} 秒`;
  if (seconds < 3600) return `${Math.ceil(seconds / 60)} 分钟`;
  if (seconds < 86400) return `${Math.ceil(seconds / 3600)} 小时`;
  return `${Math.ceil(seconds / 86400)} 天`;
}

export function ruleSummary(rule: { scope: RegIPScope; windowSeconds: number; limit: number }) {
  return `${SCOPE_META[rule.scope]?.label ?? rule.scope}${windowLabel(rule.windowSeconds)}最多 ${rule.limit} ${unitOf(rule.scope)}`;
}

/** 规则上限的单位：注册数是「个」，设备是「台」，地址是「个地址」 */
export function unitOf(scope: RegIPScope) {
  if (scope === "ip_devices") return "台";
  if (scope === "device_ips") return "个地址";
  return "个";
}

/* ───────────────────── 策略 ───────────────────── */

export function newRuleId() {
  return `r-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`;
}

/** 与后端 regip.DefaultPolicy 一致；服务端返回 defaults 时以服务端为准 */
export function defaultRegIPPolicy(): RegIPPolicy {
  return {
    enabled: false,
    mode: "enforce",
    network: { ipv4Subnet: 24, ipv6Host: 64, ipv6Subnet: 48 },
    rules: [
      { id: "ip-hour", name: "同一地址每小时", enabled: true, scope: "ip", windowSeconds: 3600, limit: 3, action: "challenge" },
      { id: "ip-day", name: "同一地址每天", enabled: true, scope: "ip", windowSeconds: 86400, limit: 10, action: "reject" },
      { id: "subnet-day", name: "同一网段每天", enabled: true, scope: "subnet", windowSeconds: 86400, limit: 50, action: "reject" },
      { id: "device-day", name: "同一设备每天", enabled: true, scope: "device", windowSeconds: 86400, limit: 3, action: "reject" },
      { id: "ip-devices-hour", name: "同一地址每小时出现的设备", enabled: true, scope: "ip_devices", windowSeconds: 3600, limit: 10, action: "challenge" },
      { id: "device-ips-day", name: "同一设备每天换的地址", enabled: true, scope: "device_ips", windowSeconds: 86400, limit: 5, action: "challenge" }
    ],
    penalty: { enabled: true, strikes: 5, strikeWindowSeconds: 3600, baseSeconds: 1800, multiplier: 2, maxSeconds: 7 * 86400, scope: "ip", device: true },
    surge: { enabled: false, windowMinutes: 5, baselineMinutes: 60, sensitivity: 4, minRegistrations: 30, action: "challenge", tightenFactor: 0.5 },
    isp: {
      enabled: false,
      keywords: ["阿里云", "腾讯云", "华为云", "百度云", "亚马逊", "Amazon", "Google", "Microsoft", "DigitalOcean", "Linode", "Vultr", "OVH", "Hetzner", "数据中心", "IDC"],
      action: "challenge"
    },
    geo: { mode: "off", regions: [], action: "reject", unknown: "allow" },
    device: { missing: "allow", invalid: "challenge", minLength: 8 },
    missingIp: "allow",
    failMode: "open",
    rejectMessage: ""
  };
}

/* ───────────────────── 小部件 ───────────────────── */

export function KindBadge({ kind }: { kind: RegIPEventKind }) {
  const meta = KIND_META[kind];
  if (!meta) return <Badge variant="outline" size="sm">{kind}</Badge>;
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} size="sm" className="gap-1">
      <Icon className="size-3" />
      {meta.label}
    </Badge>
  );
}

export function ActionBadge({ action }: { action: RegIPAction }) {
  const meta = ACTION_META[action];
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} size="sm" className="gap-1 font-normal">
      <Icon className="size-3" />
      {meta.label}
    </Badge>
  );
}

export function ListKindBadge({ kind, factor }: { kind: RegIPListKind; factor?: number }) {
  const meta = LIST_META[kind];
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} size="sm" className="gap-1">
      <Icon className="size-3" />
      {meta.label}
      {kind === "elevated" && factor ? ` ×${factor}` : ""}
    </Badge>
  );
}

export function ScopeGlyph({ scope, className }: { scope: RegIPScope; className?: string }) {
  const meta = SCOPE_META[scope] ?? SCOPE_META.ip;
  const Icon = meta.icon;
  return (
    <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg ring-1 ring-inset", meta.tint, className)}>
      <Icon className="size-4" />
    </span>
  );
}

/** 等宽地址文本，点一下复制 */
export function IPText({ value, className, onClick }: { value?: string | null; className?: string; onClick?: () => void }) {
  if (!value) return <span className="text-muted-foreground">—</span>;
  return (
    <span className={cn("group inline-flex max-w-full items-center gap-1 font-mono text-xs", className)}>
      {onClick ? (
        <button type="button" onClick={onClick} className="truncate hover:underline" title="查询这个地址">
          {value}
        </button>
      ) : (
        <span className="truncate">{value}</span>
      )}
      <button
        type="button"
        aria-label="复制"
        className="opacity-0 transition-opacity group-hover:opacity-100"
        onClick={() => {
          void navigator.clipboard?.writeText(value).then(() => toast.success("已复制"));
        }}
      >
        <Copy className="size-3 text-muted-foreground" />
      </button>
    </span>
  );
}

/**
 * 配额条：已用 / 上限。越界部分标红，阈值线标在上限处；
 * 比「3 / 10」更快读出离触发还差多少，以及触发后会怎样（条的颜色就是处置的颜色）。
 */
export function QuotaMeter({
  count,
  limit,
  action,
  className
}: {
  count: number;
  limit: number;
  action: RegIPAction;
  className?: string;
}) {
  const ratio = limit > 0 ? Math.min(count / limit, 1) : 0;
  const over = count >= limit;
  const fill =
    over && action === "reject"
      ? "bg-rose-500"
      : over && action === "challenge"
        ? "bg-amber-500"
        : over
          ? "bg-sky-500"
          : ratio >= 0.8
            ? "bg-amber-400"
            : "bg-emerald-500";
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className="relative h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
        <div className={cn("absolute inset-y-0 left-0 rounded-full transition-[width] duration-200", fill)} style={{ width: `${ratio * 100}%` }} />
      </div>
      <span className={cn("w-16 shrink-0 text-right text-[11px] tabular-nums", over ? "font-medium text-foreground" : "text-muted-foreground")}>
        {count.toLocaleString("zh-CN")} / {limit.toLocaleString("zh-CN")}
      </span>
    </div>
  );
}

/** 判定结果横幅：结论 + 原因 + 给用户看的提示 + 管理员看的细节 */
export function DecisionBanner({ decision, compact }: { decision: RegIPDecision; compact?: boolean }) {
  const shown: RegIPOutcome = decision.observed ?? decision.outcome;
  const meta = OUTCOME_META[decision.outcome];
  const observedMeta = decision.observed ? OUTCOME_META[decision.observed] : null;
  const Icon = (observedMeta ?? meta).icon;
  const reason = decision.reason ? REASON_META[decision.reason as Exclude<RegIPReason, "">] : null;
  return (
    <div className={cn("rounded-xl border px-4 py-3", OUTCOME_META[shown].ring)}>
      <div className="flex flex-wrap items-center gap-2">
        <Icon className={cn("size-5", OUTCOME_META[shown].tone)} />
        <span className={cn("text-sm font-semibold", OUTCOME_META[shown].tone)}>
          {observedMeta ? `观察：本该${observedMeta.label}` : meta.label}
        </span>
        {observedMeta ? <Badge variant="success" size="sm">实际放行</Badge> : null}
        {reason ? (
          <Badge variant="outline" size="sm" className="gap-1 font-normal">
            <reason.icon className="size-3" />
            {reason.label}
          </Badge>
        ) : null}
        {decision.ruleName ? <span className="text-xs text-muted-foreground">{decision.ruleName}</span> : null}
        {decision.challengePassed ? <Badge variant="success" size="sm">已过验证码</Badge> : null}
        {decision.factor && decision.factor > 1 ? <Badge variant="info" size="sm">配额 ×{decision.factor}</Badge> : null}
        {decision.tighten && decision.tighten < 1 ? <Badge variant="warning" size="sm">突增收紧 ×{decision.tighten}</Badge> : null}
      </div>
      {!compact && (decision.message || decision.detail) ? (
        <div className="mt-2 space-y-1 pl-7 text-xs">
          {decision.message ? <p>用户看到：「{decision.message}」</p> : null}
          {decision.detail ? <p className="text-muted-foreground">{decision.detail}</p> : null}
          {decision.retryAfter ? <p className="text-muted-foreground">约 {durationLabel(decision.retryAfter)}后可再试</p> : null}
        </div>
      ) : null}
    </div>
  );
}

/** 各规则这一次的计数 */
export function RuleHitList({ rules, empty = "没有生效的配额规则" }: { rules: RegIPRuleHit[]; empty?: string }) {
  if (!rules.length) return <p className="py-4 text-center text-xs text-muted-foreground">{empty}</p>;
  return (
    <ul className="divide-y divide-border">
      {rules.map((hit) => (
        <li key={hit.ruleId} className={cn("flex items-center gap-3 py-2.5", hit.skipped && "opacity-50")}>
          <ScopeGlyph scope={hit.scope} className="size-7" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-xs font-medium">{hit.name}</span>
              <ActionBadge action={hit.action} />
            </div>
            <div className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
              {hit.skipped ? hit.skipReason : `${hit.subject} · ${windowLabel(hit.windowSeconds)}`}
              {!hit.skipped && hit.limit !== hit.configured ? ` · 配置 ${hit.configured} → 生效 ${hit.limit}` : ""}
            </div>
          </div>
          {hit.skipped ? null : <QuotaMeter count={hit.count} limit={hit.limit} action={hit.action} className="w-44" />}
        </li>
      ))}
    </ul>
  );
}

/**
 * 分钟桶迷你折线：基线段灰、观察窗口段着色，虚线是触发线（按窗口均摊到每分钟）。
 */
export function SurgeSparkline({
  series,
  windowMinutes,
  threshold,
  active,
  height = 56
}: {
  series: number[];
  windowMinutes: number;
  threshold?: number;
  active?: boolean;
  height?: number;
}) {
  if (!series.length) return <div style={{ height }} className="grid place-items-center text-[11px] text-muted-foreground">暂无数据</div>;
  const width = 300;
  const perMinute = threshold && windowMinutes ? threshold / windowMinutes : 0;
  const peak = Math.max(1, perMinute, ...series);
  const step = series.length > 1 ? width / (series.length - 1) : width;
  const y = (v: number) => height - 4 - (v / peak) * (height - 8);
  const points = series.map((v, i) => `${(i * step).toFixed(1)},${y(v).toFixed(1)}`);
  const split = Math.max(0, series.length - windowMinutes - 1);
  const stroke = active ? "stroke-rose-500" : "stroke-indigo-500";
  return (
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="w-full" style={{ height }} aria-hidden>
      <polyline points={points.slice(0, split + 1).join(" ")} fill="none" className="stroke-muted-foreground/40" strokeWidth={1.5} />
      <polyline points={points.slice(split).join(" ")} fill="none" className={stroke} strokeWidth={2} />
      {perMinute > 0 ? (
        <line x1={0} x2={width} y1={y(perMinute)} y2={y(perMinute)} className="stroke-amber-500" strokeDasharray="4 4" strokeWidth={1} />
      ) : null}
    </svg>
  );
}

/** 处置切换：分段按钮，颜色即处置的颜色 */
export function ActionToggle({
  value,
  onChange,
  options = ACTIONS,
  disabled
}: {
  value: RegIPAction;
  onChange: (value: RegIPAction) => void;
  options?: RegIPAction[];
  disabled?: boolean;
}) {
  return (
    <div className={cn("inline-flex rounded-lg bg-muted p-0.5", disabled && "pointer-events-none opacity-50")}>
      {options.map((action) => {
        const meta = ACTION_META[action];
        const Icon = meta.icon;
        const active = value === action;
        return (
          <button
            key={action}
            type="button"
            onClick={() => onChange(action)}
            className={cn(
              "flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium transition-colors",
              active ? cn("ring-1 ring-inset", meta.active) : "text-muted-foreground hover:text-foreground"
            )}
          >
            <Icon className="size-3" />
            {meta.label}
          </button>
        );
      })}
    </div>
  );
}

/** 注册方式多选；全不选等于全选 */
export function MethodChips({ value, onChange }: { value?: RegIPMethod[] | null; onChange: (value: RegIPMethod[]) => void }) {
  const selected = value?.length ? value : [];
  const all = selected.length === 0;
  return (
    <div className="flex flex-wrap gap-1">
      <button
        type="button"
        onClick={() => onChange([])}
        className={cn(
          "rounded-md px-1.5 py-0.5 text-[11px] ring-1 ring-inset transition-colors",
          all ? "bg-primary/10 text-primary ring-primary/30" : "text-muted-foreground ring-border hover:text-foreground"
        )}
      >
        全部方式
      </button>
      {METHODS.map((method) => {
        const on = selected.includes(method);
        const Icon = METHOD_META[method].icon;
        return (
          <button
            key={method}
            type="button"
            onClick={() => {
              const next = on ? selected.filter((m) => m !== method) : [...selected, method];
              onChange(next.length === METHODS.length ? [] : next);
            }}
            className={cn(
              "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] ring-1 ring-inset transition-colors",
              on ? "bg-primary/10 text-primary ring-primary/30" : "text-muted-foreground ring-border hover:text-foreground"
            )}
          >
            <Icon className="size-3" />
            {METHOD_META[method].label}
          </button>
        );
      })}
    </div>
  );
}

const UNITS = [
  { key: "m", label: "分钟", seconds: 60 },
  { key: "h", label: "小时", seconds: 3600 },
  { key: "d", label: "天", seconds: 86400 }
] as const;

/** 统计窗口：常用值一键选，其余按 分钟 / 小时 / 天 输入。0 为累计 */
export function WindowPicker({
  value,
  onChange,
  allowLifetime = true
}: {
  value: number;
  onChange: (seconds: number) => void;
  allowLifetime?: boolean;
}) {
  const preset = WINDOW_PRESETS.find((item) => item.value === value && (allowLifetime || item.value > 0));
  const unit = value % 86400 === 0 && value > 0 ? UNITS[2] : value % 3600 === 0 && value > 0 ? UNITS[1] : UNITS[0];
  const [custom, setCustom] = useState(!preset);
  return (
    <div className="flex items-center gap-1.5">
      <Select
        value={custom ? "custom" : String(value)}
        onValueChange={(next) => {
          if (next === "custom") {
            setCustom(true);
            if (value === 0) onChange(3600);
            return;
          }
          setCustom(false);
          onChange(Number(next));
        }}
      >
        <SelectTrigger size="sm" className="w-24 text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {WINDOW_PRESETS.filter((item) => allowLifetime || item.value > 0).map((item) => (
            <SelectItem key={item.value} value={String(item.value)} className="text-xs">
              {item.label}
            </SelectItem>
          ))}
          <SelectItem value="custom" className="text-xs">
            自定义
          </SelectItem>
        </SelectContent>
      </Select>
      {custom ? (
        <>
          <Input
            type="number"
            min={1}
            value={Math.max(1, Math.round(value / unit.seconds))}
            onChange={(event) => onChange(Math.max(1, Number(event.target.value) || 1) * unit.seconds)}
            className="h-8 w-16 text-xs tabular-nums"
          />
          <Select
            value={unit.key}
            onValueChange={(key) => {
              const next = UNITS.find((u) => u.key === key) ?? UNITS[0];
              onChange(Math.max(1, Math.round(value / unit.seconds)) * next.seconds);
            }}
          >
            <SelectTrigger size="sm" className="w-20 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {UNITS.map((u) => (
                <SelectItem key={u.key} value={u.key} className="text-xs">
                  {u.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>
      ) : null}
    </div>
  );
}

/** 设备标识：等宽、过长截断、点击查询、悬停复制 */
export function DeviceText({ value, className, onClick }: { value?: string | null; className?: string; onClick?: () => void }) {
  if (!value) return <span className="text-muted-foreground">—</span>;
  const short = value.length > 22 ? `${value.slice(0, 10)}…${value.slice(-8)}` : value;
  return (
    <span className={cn("group inline-flex max-w-full items-center gap-1 font-mono text-xs", className)} title={value}>
      <Smartphone className="size-3 shrink-0 text-fuchsia-500" />
      {onClick ? (
        <button type="button" onClick={onClick} className="truncate hover:underline" title="查询这台设备">
          {short}
        </button>
      ) : (
        <span className="truncate">{short}</span>
      )}
      <button
        type="button"
        aria-label="复制"
        className="opacity-0 transition-opacity group-hover:opacity-100"
        onClick={() => {
          void navigator.clipboard?.writeText(value).then(() => toast.success("已复制"));
        }}
      >
        <Copy className="size-3 text-muted-foreground" />
      </button>
    </span>
  );
}

/** 设备标识的质量：有效 / 无效（附原因） */
export function DeviceQuality({ device }: { device?: RegIPDevice | null }) {
  if (!device || (!device.valid && !device.invalid)) return <Badge variant="outline" size="sm">未上报</Badge>;
  if (device.valid) {
    return (
      <Badge variant="success" size="sm" className="gap-1">
        <BadgeCheck className="size-3" /> 有效
      </Badge>
    );
  }
  return (
    <Badge variant="warning" size="sm" className="gap-1" title={device.invalidReason}>
      <Fingerprint className="size-3" /> 无效：{device.invalidReason}
    </Badge>
  );
}

const DEVICE_ACTIONS: Array<{ value: RegIPDeviceAction; label: string; icon: LucideIcon; active: string }> = [
  { value: "allow", label: "放行", icon: CircleCheck, active: "bg-emerald-500/12 text-emerald-700 ring-emerald-500/30 dark:text-emerald-300" },
  { value: "challenge", label: "加验", icon: ShieldQuestion, active: ACTION_META.challenge.active },
  { value: "reject", label: "拒绝", icon: Ban, active: ACTION_META.reject.active }
];

/** 缺少 / 无效设备的处置：放行、加验、拒绝 */
export function DeviceActionToggle({ value, onChange }: { value: RegIPDeviceAction; onChange: (value: RegIPDeviceAction) => void }) {
  return (
    <div className="inline-flex rounded-lg bg-muted p-0.5">
      {DEVICE_ACTIONS.map((item) => (
        <button
          key={item.value}
          type="button"
          onClick={() => onChange(item.value)}
          className={cn(
            "flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium transition-colors",
            value === item.value ? cn("ring-1 ring-inset", item.active) : "text-muted-foreground hover:text-foreground"
          )}
        >
          <item.icon className="size-3" />
          {item.label}
        </button>
      ))}
    </div>
  );
}

/**
 * 关联列表：地址下出现过的设备，或设备用过的地址。每行带「账号数」比例条，点一下跳到另一边的查询 ——
 * 地址与设备两张查询页靠它互相跳转，顺着一个可疑设备摸出它背后的整个出口池。
 */
export function LinkedList({
  items,
  kind,
  onOpen,
  live
}: {
  items: RegIPLinked[];
  kind: "device" | "ip";
  onOpen: (key: string) => void;
  /** 去重计数里此刻窗口内的成员（标「窗口内」） */
  live?: Set<string>;
}) {
  if (!items.length) return <p className="px-4 py-6 text-center text-xs text-muted-foreground">没有关联记录</p>;
  const peak = Math.max(1, ...items.map((item) => item.accounts));
  return (
    <ul className="divide-y divide-border">
      {items.map((item) => (
        <li key={item.key} className="relative px-4 py-2">
          <span className="absolute inset-y-0 left-0 bg-fuchsia-500 opacity-[0.06]" style={{ width: `${(item.accounts / peak) * 100}%` }} />
          <div className="relative flex items-center gap-2.5">
            <div className="min-w-0 flex-1">
              {kind === "device" ? (
                <DeviceText value={item.key} onClick={() => onOpen(item.key)} />
              ) : (
                <IPText value={item.key} onClick={() => onOpen(item.key)} />
              )}
              <div className="truncate text-[10px] text-muted-foreground">
                {item.location ? `${item.location} · ` : ""}
                {new Date(item.firstAt).toLocaleDateString("zh-CN")} – {new Date(item.lastAt).toLocaleDateString("zh-CN")}
              </div>
            </div>
            {live?.has(item.key) ? <Badge variant="info" size="sm">窗口内</Badge> : null}
            <span className="shrink-0 text-xs font-medium tabular-nums">
              {item.accounts}
              <span className="ml-0.5 text-[10px] font-normal text-muted-foreground">个账号</span>
            </span>
            <button type="button" aria-label="查询" onClick={() => onOpen(item.key)} className="text-muted-foreground hover:text-foreground">
              <ArrowRight className="size-3.5" />
            </button>
          </div>
        </li>
      ))}
    </ul>
  );
}

/** 一行「标签：值」，详情卡里用 */
export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 py-1.5 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right">{children}</span>
    </div>
  );
}

export function locationText(item: { country?: string; province?: string; city?: string }) {
  return [item.country, item.province, item.city].filter(Boolean).join(" / ") || "—";
}
