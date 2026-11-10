"use client";

import { useSyncExternalStore, type ReactNode } from "react";
import { Bot, CircleHelp, ThumbsDown, ThumbsUp, type LucideIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { BAN_LEVEL_LABEL, BAN_REASON_CODE_LABEL } from "@/components/users/ban-form";
import type { AppealBan, AppealModelResult, AppealQueue, AppealUser, BanAppeal, EvidenceStrength } from "@/lib/api/ban-appeals";
import { cn } from "@/lib/utils";

/**
 * 封禁申诉与全站封禁共用的展示件：状态、模型结论、证据强度的文案与配色，一句话的拼法。
 * 只放展示，不碰数据。
 */

export const QUEUES: Array<{ value: AppealQueue; label: string; hint: string }> = [
  { value: "open", label: "待复审", hint: "还没有模型结论，等人工处理" },
  { value: "suggested", label: "模型已建议", hint: "模型给出了结论，等人工确认" },
  { value: "approved", label: "已通过", hint: "已解除封禁" },
  { value: "rejected", label: "已驳回", hint: "维持封禁" },
  { value: "withdrawn", label: "已撤回", hint: "用户自己撤回" },
];

export const isOpen = (a: Pick<BanAppeal, "status">) => a.status === "pending" || a.status === "reviewing";

type BadgeVariant = "success" | "warning" | "danger" | "info" | "secondary" | "outline";

/** 一条申诉此刻的状态：把存储状态与模型结论合成一个人话标签 */
export function appealState(a: Pick<BanAppeal, "status" | "model" | "decidedBy">): { label: string; variant: BadgeVariant } {
  switch (a.status) {
    case "approved":
      return { label: a.decidedBy === "model" ? "模型自动通过" : "已通过", variant: "success" };
    case "rejected":
      return { label: a.decidedBy === "model" ? "模型自动驳回" : "已驳回", variant: "danger" };
    case "withdrawn":
      return { label: "已撤回", variant: "secondary" };
    case "reviewing":
      return a.model?.verdict ? { label: "模型已建议", variant: "info" } : { label: "模型复审中", variant: "info" };
    default:
      return a.model?.verdict ? { label: "模型已建议", variant: "info" } : { label: "待复审", variant: "warning" };
  }
}

export function AppealStateBadge({ appeal }: { appeal: Pick<BanAppeal, "status" | "model" | "decidedBy"> }) {
  const s = appealState(appeal);
  return <Badge variant={s.variant} size="sm">{s.label}</Badge>;
}

const VERDICT: Record<string, { label: string; icon: LucideIcon; chip: string; bar: string }> = {
  approve: {
    label: "建议通过",
    icon: ThumbsUp,
    chip: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    bar: "bg-emerald-500",
  },
  reject: {
    label: "建议驳回",
    icon: ThumbsDown,
    chip: "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300",
    bar: "bg-rose-500",
  },
  escalate: {
    label: "建议人工",
    icon: CircleHelp,
    chip: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
    bar: "bg-amber-500",
  },
};

export const verdictMeta = (v?: string) => VERDICT[v ?? ""] ?? { label: "无结论", icon: Bot, chip: "border-border bg-muted/50 text-muted-foreground", bar: "bg-muted-foreground" };

export const pct = (n?: number | null) => `${Math.round(Math.max(0, Math.min(1, n ?? 0)) * 100)}%`;

/** 模型结论小标签：「建议通过 92%」 */
export function VerdictChip({ model, className }: { model?: AppealModelResult | null; className?: string }) {
  if (!model?.verdict) return null;
  const m = verdictMeta(model.verdict);
  const Icon = m.icon;
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] leading-4 font-medium", m.chip, className)}>
      <Icon className="size-3" />
      {m.label}
      <span className="tabular-nums opacity-80">{pct(model.confidence)}</span>
    </span>
  );
}

/** 置信度刻度：一条横条加一道门槛线 */
export function ConfidenceMeter({ value, verdict, threshold, className }: { value: number; verdict?: string; threshold?: number; className?: string }) {
  const m = verdictMeta(verdict);
  const ratio = Math.max(0, Math.min(1, value));
  return (
    <div className={cn("relative h-2 w-full overflow-visible rounded-full bg-muted", className)}>
      <div className={cn("h-full rounded-full transition-[width] duration-500", m.bar)} style={{ width: `${ratio * 100}%` }} />
      {threshold !== undefined && threshold > 0 ? (
        <span
          aria-hidden
          title={`自动处理门槛 ${pct(threshold)}`}
          className="absolute -top-1 h-4 w-0.5 -translate-x-1/2 rounded-full bg-foreground/70"
          style={{ left: `${Math.min(1, threshold) * 100}%` }}
        />
      ) : null}
    </div>
  );
}

export const EVIDENCE: Record<EvidenceStrength, { label: string; detail: string; variant: BadgeVariant; tone: string }> = {
  strong: { label: "证据确凿", detail: "风控记录清楚指向违规", variant: "danger", tone: "text-rose-600 dark:text-rose-400" },
  medium: { label: "有一定证据", detail: "有风险信号，但不足以单独定论", variant: "warning", tone: "text-amber-600 dark:text-amber-400" },
  weak: { label: "像是误判", detail: "风控记录很少或与违规无关", variant: "success", tone: "text-emerald-600 dark:text-emerald-400" },
};

export const evidenceMeta = (level?: string) => EVIDENCE[level as EvidenceStrength] ?? { label: "没有证据", detail: "风控平台没有相关记录", variant: "secondary" as const, tone: "text-muted-foreground" };

export const reasonCodeLabel = (code?: string) => (code ? BAN_REASON_CODE_LABEL[code] ?? code : "");

/** 显示名：昵称 → 登录名 → 用户 #ID */
export function userName(user: AppealUser | null | undefined, id: number) {
  return user?.nickname || user?.account || `用户 #${id}`;
}

/** 「全站永久封禁」「临时封禁」 */
export function banTitle(ban: Pick<AppealBan, "level" | "banType"> | null | undefined) {
  if (!ban) return "封禁";
  const level = ban.level === "platform" ? BAN_LEVEL_LABEL.platform : "";
  const type = ban.banType === "permanent" ? "永久" : ban.banType === "temporary" ? "临时" : "";
  return `${level}${type}封禁`;
}

export function LevelBadge({ level }: { level?: string }) {
  if (level !== "platform") return null;
  return <Badge variant="danger" size="sm">全站</Badge>;
}

/** 一组「标签：值」，标签在上 */
export function Facts({ items, className }: { items: Array<{ label: string; value: ReactNode; wide?: boolean; mono?: boolean }>; className?: string }) {
  const shown = items.filter((i) => i.value !== null && i.value !== undefined && i.value !== "");
  return (
    <dl className={cn("grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3", className)}>
      {shown.map((i) => (
        <div key={i.label} className={cn("min-w-0", i.wide && "col-span-2 sm:col-span-3")}>
          <dt className="text-[11px] leading-4 text-muted-foreground">{i.label}</dt>
          <dd className={cn("text-sm leading-5", i.wide ? "break-words whitespace-pre-wrap" : "truncate", i.mono && "font-mono text-xs leading-5")}>
            {i.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** 详情里的一块：小标题 + 内容 */
export function Block({ title, aside, children, className }: { title: string; aside?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn("flex min-w-0 flex-col gap-3", className)}>
      <header className="flex items-center justify-between gap-2">
        <h4 className="text-xs font-medium text-muted-foreground">{title}</h4>
        {aside}
      </header>
      {children}
    </section>
  );
}

const WIDE_QUERY = "(min-width: 1024px)";

/** 是否宽屏（lg 及以上）：宽屏把详情放在右栏，窄屏用对话框 */
export function useWideScreen() {
  return useSyncExternalStore(
    (notify) => {
      const mq = window.matchMedia(WIDE_QUERY);
      mq.addEventListener("change", notify);
      return () => mq.removeEventListener("change", notify);
    },
    () => window.matchMedia(WIDE_QUERY).matches,
    () => true,
  );
}
