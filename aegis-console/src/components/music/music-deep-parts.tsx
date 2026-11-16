"use client";

import { Badge } from "@/components/ui/badge";
import type { MusicDeepRanker, MusicDeepRunDetail } from "@/lib/api/music";
import { cn } from "@/lib/utils";
import { DEEP_PARAM_LABEL, liftText, num, pct, rankFeatureLabel } from "./music-format";
import { EmptyLine } from "./music-kit";

/**
 * 深度模型（第三期）的展示件：训练记录表、训练详情与总览共用，同一个版本在各处的数字与叫法一致。
 */

/** 表格里的指标对比：上条为模型、下条为热度基线，按两者较大值缩放 */
export function LiftBars({ value, baseline, className }: { value: number; baseline: number; className?: string }) {
  const max = Math.max(value, baseline, 0.0001);
  const better = value >= baseline;
  return (
    <span className={cn("flex w-28 min-w-0 flex-col gap-0.5", className)} title={`模型 ${pct(value)}，热度基线 ${pct(baseline)}`}>
      <span className="h-1.5 overflow-hidden rounded-full bg-muted">
        <span className={cn("block h-full rounded-full", better ? "bg-violet-500" : "bg-rose-500")} style={{ width: `${(value / max) * 100}%` }} />
      </span>
      <span className="h-1 overflow-hidden rounded-full bg-muted">
        <span className="block h-full rounded-full bg-zinc-400" style={{ width: `${(baseline / max) * 100}%` }} />
      </span>
    </span>
  );
}

/** 精排是否启用：表格与总览用的短徽标 */
export function DeepRankerBadge({ ranker }: { ranker: MusicDeepRanker }) {
  if (!ranker) return <span className="text-xs text-muted-foreground">—</span>;
  return (
    <Badge variant={ranker.enabled ? "success" : "secondary"} size="sm" title={!ranker.enabled ? ranker.reason : undefined}>
      {ranker.enabled ? "已启用" : "未启用"}
    </Badge>
  );
}

/**
 * LightGBM LambdaRank 精排：留出集 NDCG@10 对比「融合分」排序，特征重要性（分裂增益）按从大到小画横条。
 * 没导出时只给原因。
 */
export function DeepRankerSummary({ ranker }: { ranker: MusicDeepRanker }) {
  if (!ranker) return <EmptyLine>这个版本没有精排结果</EmptyLine>;
  if (!ranker.enabled) {
    return (
      <div className="flex min-w-0 flex-col gap-2">
        <Badge variant="secondary" size="sm" className="w-fit">
          未启用
        </Badge>
        <p className="text-xs text-muted-foreground">{ranker.reason || "未达到导出条件，线上继续用融合分排序。"}</p>
      </div>
    );
  }
  const ndcg = ranker.ndcg10 ?? 0;
  const base = ranker.baselineNdcg10 ?? 0;
  const max = Math.max(ndcg, base, 0.0001);
  const importance = [...(ranker.importance ?? [])].sort((a, b) => b.gain - a.gain);
  const maxGain = Math.max(0.0001, ...importance.map((f) => f.gain));
  const totalGain = importance.reduce((s, f) => s + Math.max(0, f.gain), 0);
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Badge variant="success" size="sm">
          已启用
        </Badge>
        <span className="text-xs text-muted-foreground tabular-nums">
          {num(ranker.samples)} 条样本，{num(ranker.groups)} 次推荐
        </span>
      </div>
      <ul className="flex min-w-0 flex-col gap-2">
        {[
          { key: "model", label: "精排", value: ndcg, bar: "bg-violet-500" },
          { key: "base", label: "融合分", value: base, bar: "bg-zinc-400" }
        ].map((row) => (
          <li key={row.key} className="grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)_4rem] items-center gap-3 text-xs">
            <span className="text-muted-foreground">{row.label}</span>
            <span className="h-2 overflow-hidden rounded-full bg-muted">
              <span className={cn("block h-full rounded-full", row.bar)} style={{ width: `${(row.value / max) * 100}%` }} />
            </span>
            <span className="text-right font-medium tabular-nums">{row.value.toFixed(3)}</span>
          </li>
        ))}
      </ul>
      <p className="text-[11px] text-muted-foreground">
        按时间留出最后 20% 的推荐算 NDCG@10，较融合分 {liftText(ndcg, base)}；线上最终分为融合分乘以（0.5 + 本次候选内的百分位）。
      </p>
      {importance.length ? (
        <div className="flex min-w-0 flex-col gap-2">
          <span className="text-[11px] font-medium text-muted-foreground">特征重要性（分裂增益）</span>
          <ul className="flex min-w-0 flex-col gap-1.5">
            {importance.map((f) => (
              <li key={f.name} className="grid min-w-0 grid-cols-[7.5rem_minmax(0,1fr)_3.5rem] items-center gap-3 text-xs">
                <span className="truncate" title={f.name}>
                  {rankFeatureLabel(f.name)}
                </span>
                <span className="h-2.5 min-w-0 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-sky-500" style={{ width: `${(Math.max(0, f.gain) / maxGain) * 100}%` }} />
                </span>
                <span className="text-right text-muted-foreground tabular-nums">{pct(totalGain ? Math.max(0, f.gain) / totalGain : 0, 1)}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}

/** 数值对齐：Go 推理与 PyTorch 导出的用例逐维对比，误差超过 1e-3 的版本不会被加载 */
export function ParitySummary({ parity }: { parity: MusicDeepRunDetail["parity"] }) {
  if (!parity) return <EmptyLine>还没有对齐结果（训练未完成，或推理服务尚未加载这个版本）</EmptyLine>;
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Badge variant={parity.ok ? "success" : "danger"} size="sm">
          {parity.ok ? "一致" : "不一致"}
        </Badge>
        <span className="text-xs text-muted-foreground tabular-nums">
          {num(parity.cases)} 个用例，最大误差 {Number.isFinite(parity.maxError) ? parity.maxError.toExponential(2) : "—"}
        </span>
      </div>
      <p className="text-[11px] text-muted-foreground">
        {parity.ok
          ? "推理服务的前向结果与训练端逐维误差都在 1e-3 以内。"
          : "推理服务拒绝加载这个版本，线上继续用上一个版本；需要排查导出格式或推理实现。"}
      </p>
    </div>
  );
}

/** 参数快照：已知键给中文名，嵌套对象与数组按 JSON 原样显示 */
export function DeepParams({ params }: { params: Record<string, unknown> | null | undefined }) {
  const entries = Object.entries(params ?? {});
  if (!entries.length) return <EmptyLine>没有参数记录</EmptyLine>;
  return (
    <dl className="grid min-w-0 grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
      {entries.map(([key, value]) => (
        <div key={key} className="flex min-w-0 items-baseline justify-between gap-3 text-xs">
          <dt className="truncate text-muted-foreground" title={key}>
            {DEEP_PARAM_LABEL[key] ?? key}
          </dt>
          <dd className="min-w-0 truncate font-medium tabular-nums" title={paramText(value)}>
            {paramText(value)}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function paramText(value: unknown): string {
  if (typeof value === "boolean") return value ? "开启" : "关闭";
  if (typeof value === "number") return Number.isFinite(value) ? String(Number(value.toPrecision(6))) : "—";
  if (typeof value === "string") return value || "—";
  if (value === null || value === undefined) return "—";
  return JSON.stringify(value);
}
