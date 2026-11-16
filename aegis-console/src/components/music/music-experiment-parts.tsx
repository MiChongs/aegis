"use client";

import type { MusicExperimentVariant } from "@/lib/api/music";
import { cn } from "@/lib/utils";
import { CHANNEL_LABEL } from "./music-format";

/**
 * 实验列表、编辑器与效果页共用的小件：变体配色、流量分配条、覆盖参数的一句话。
 * 同一个变体在三处必须是同一个颜色。
 */

/** 变体配色：A 为对照用中性的天蓝，其后依次区分 */
export const VARIANT_COLOR = [
  "var(--color-sky-500, #0ea5e9)",
  "var(--color-violet-500, #8b5cf6)",
  "var(--color-amber-500, #f59e0b)",
  "var(--color-emerald-500, #10b981)",
  "var(--color-rose-500, #f43f5e)",
  "var(--color-teal-500, #14b8a6)"
];

/** 流量分配条：各变体按百分比着色，剩下的不进实验 */
export function TrafficSplit({ variants, className }: { variants: Array<Pick<MusicExperimentVariant, "key" | "traffic">>; className?: string }) {
  const used = variants.reduce((s, v) => s + (Number.isFinite(v.traffic) ? v.traffic : 0), 0);
  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <div className="flex h-2.5 min-w-0 overflow-hidden rounded-full bg-muted">
        {variants.map((v, i) =>
          v.traffic > 0 ? (
            <span
              key={v.key}
              className="h-full transition-[width] duration-300"
              style={{ width: `${Math.min(100, v.traffic)}%`, background: VARIANT_COLOR[i % VARIANT_COLOR.length] }}
              title={`${v.key}：${v.traffic}%`}
            />
          ) : null
        )}
      </div>
      <span className={cn("text-[11px] text-muted-foreground tabular-nums", used > 100 && "text-rose-600 dark:text-rose-400")}>
        {used > 100 ? `流量合计 ${used}%，超过 100%` : used < 100 ? `${used}% 的用户进入实验，其余 ${100 - used}% 不参与` : "全部用户进入实验"}
      </span>
    </div>
  );
}

/** 覆盖参数的一句话：「口味模型 1.2，探索占比 0.2，关闭精排，固定第 12 版模型」 */
export function overridesText(v: Pick<MusicExperimentVariant, "overrides">) {
  const o = v.overrides ?? {};
  const parts: string[] = [];
  for (const [k, val] of Object.entries(o.blend ?? {})) if (typeof val === "number") parts.push(`${CHANNEL_LABEL[k] ?? k}权重 ${val}`);
  if (typeof o.exploreRatio === "number") parts.push(`探索占比 ${o.exploreRatio}`);
  if (typeof o.artistCap === "number") parts.push(`同歌手最多 ${o.artistCap} 首`);
  if (typeof o.ranker === "boolean") parts.push(o.ranker ? "开启精排" : "关闭精排");
  if (typeof o.foldIn === "boolean") parts.push(o.foldIn ? "开启实时折入" : "关闭实时折入");
  if (typeof o.modelRunId === "number" && o.modelRunId > 0) parts.push(`固定第 ${o.modelRunId} 版模型`);
  return parts.join("，");
}
