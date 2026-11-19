"use client";

import type { ReactNode } from "react";
import { percent } from "@/components/apps/lottery/lottery-plan-view";
import { Badge } from "@/components/ui/badge";
import type {
  LotteryComputeStatus,
  LotteryEstimate,
  LotteryPersonaPayload
} from "@/lib/api/lottery-compute";
import { cn } from "@/lib/utils";

/**
 * 计算引擎三个界面（精确分析、奖盘求解、推演）共用的口径：人群预设、数字的写法、小号指标块。
 * 概率一律沿用公示概率的取整规则（`percent`）：十万分之一的大奖不会被写成 0%。
 */

export type PersonaKey = "user" | "member" | "lifetime" | "adFree";

/** 人群预设：与策略模拟里虚拟用户的特权身份同一套。永久会员按已拥有奖盘里全部会员与功能奖品算 */
export const PERSONA_PRESETS: Array<{ key: PersonaKey; label: string; facts: LotteryPersonaPayload }> = [
  { key: "user", label: "普通用户", facts: {} },
  { key: "member", label: "会员", facts: { member: true } },
  { key: "lifetime", label: "永久会员", facts: { member: true, lifetime: true } },
  { key: "adFree", label: "免广告用户", facts: { adFree: true } }
];

export const personaPreset = (key: PersonaKey) => PERSONA_PRESETS.find((item) => item.key === key) ?? PERSONA_PRESETS[0];

export function personaPayload(key: PersonaKey, extra?: Partial<LotteryPersonaPayload>): LotteryPersonaPayload {
  const preset = personaPreset(key);
  return { key: preset.key, name: preset.label, ...preset.facts, ...extra };
}

/** 积分当量、积分回报这类量：两位小数，整数不带小数点 */
export function formatValue(value: number) {
  if (!Number.isFinite(value)) return "—";
  return value.toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}

/** 期望抽数：小的保留一位小数，上千的取整带分隔 */
export function formatDrawCount(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "—";
  if (value < 100) return value.toLocaleString("zh-CN", { maximumFractionDigits: 1 });
  return Math.round(value).toLocaleString("zh-CN");
}

/** 抽奖总次数：上万的写成「1,234 万」 */
export function formatDraws(value: number) {
  if (value >= 100_000) return `${(value / 10_000).toLocaleString("zh-CN", { maximumFractionDigits: 1 })} 万`;
  return value.toLocaleString("zh-CN");
}

/** 置信区间半宽（相对均值）；只有一个世界时没有区间 */
export function estimateSpread(estimate: LotteryEstimate) {
  if (estimate.n < 2 || estimate.mean === 0) return null;
  return Math.abs((estimate.high - estimate.low) / 2 / estimate.mean);
}

/** 比例的区间写法：「29.6% – 30.4%」 */
export function rateInterval(low: number, high: number) {
  return `${percent(Math.max(0, low))} – ${percent(Math.min(1, high))}`;
}

/** 用时：「0.8 秒」「12 秒」「3 分 5 秒」 */
export function formatDuration(ms: number) {
  if (ms < 1000) return `${(ms / 1000).toFixed(1)} 秒`;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}

const STATUS_META: Record<LotteryComputeStatus, { label: string; variant: "success" | "warning" | "info" | "danger" | "secondary" }> = {
  queued: { label: "排队中", variant: "secondary" },
  running: { label: "推演中", variant: "info" },
  succeeded: { label: "已完成", variant: "success" },
  failed: { label: "失败", variant: "danger" },
  cancelled: { label: "已取消", variant: "warning" }
};

export function ComputeStatusBadge({ status }: { status: LotteryComputeStatus }) {
  const meta = STATUS_META[status] ?? STATUS_META.queued;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 小号指标块：标签、数值、一行副文 */
export function Metric({
  label,
  value,
  sub,
  tone
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  tone?: "up" | "down" | "danger";
}) {
  return (
    <div className="min-w-0 space-y-0.5 rounded-xl bg-muted px-3 py-2.5">
      <div className="truncate text-[11px] text-muted-foreground">{label}</div>
      <div
        className={cn(
          "truncate font-mono text-lg font-semibold tabular-nums",
          tone === "up" && "text-emerald-600 dark:text-emerald-400",
          tone === "down" && "text-amber-600 dark:text-amber-400",
          tone === "danger" && "text-rose-600 dark:text-rose-400"
        )}
      >
        {value}
      </div>
      {sub ? <div className="truncate text-[10px] text-muted-foreground">{sub}</div> : null}
    </div>
  );
}

/** 一组可单选或多选的小药丸 */
export function Pills<T extends string>({
  options,
  selected,
  onToggle,
  multiple
}: {
  options: Array<{ key: T; label: string }>;
  selected: T[];
  onToggle: (key: T) => void;
  multiple?: boolean;
}) {
  return (
    <div role={multiple ? "group" : "radiogroup"} className="flex flex-wrap gap-1.5">
      {options.map((option) => {
        const on = selected.includes(option.key);
        return (
          <button
            key={option.key}
            type="button"
            role={multiple ? undefined : "radio"}
            aria-checked={multiple ? undefined : on}
            aria-pressed={multiple ? on : undefined}
            onClick={() => onToggle(option.key)}
            className={cn(
              "h-7 rounded-lg border px-2.5 text-[11px] font-medium transition-colors",
              on
                ? "border-foreground/80 bg-foreground text-background"
                : "border-border bg-card text-muted-foreground hover:text-foreground"
            )}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
