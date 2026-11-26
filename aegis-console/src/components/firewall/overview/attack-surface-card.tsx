"use client";

import { useMemo } from "react";
import NumberFlow from "@number-flow/react";
import { Cell, Pie, PieChart } from "recharts";
import { Layers, Lock } from "lucide-react";
import { Meter } from "@/components/apps/detail/app-detail-kit";
import { OverviewCard } from "@/components/overview/overview-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { STAGE_META, TRANSPORT_META } from "@/components/firewall/firewall-kit";
import type { FirewallEventQuery, FirewallStage, FirewallStatsV2, FirewallTransport } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";
import { formatShare } from "./overview-parts";

const STAGES: FirewallStage[] = ["edge", "payload", "response"];
const TRANSPORTS: FirewallTransport[] = ["sealed", "legacy", "plain"];

const STAGE_CONFIG = {
  edge: { label: STAGE_META.edge.label, theme: { light: "#0ea5e9", dark: "#38bdf8" } },
  payload: { label: STAGE_META.payload.label, theme: { light: "#8b5cf6", dark: "#a78bfa" } },
  response: { label: STAGE_META.response.label, theme: { light: "#f59e0b", dark: "#fbbf24" } }
} satisfies ChartConfig;

const STAGE_DOT: Record<FirewallStage, string> = {
  edge: "bg-sky-500",
  payload: "bg-violet-500",
  response: "bg-amber-500"
};

const TRANSPORT_BAR: Record<FirewallTransport, string> = {
  sealed: "bg-emerald-500",
  legacy: "bg-emerald-500/55",
  plain: "bg-muted-foreground/35"
};

function countsOf<K extends string>(items: FirewallStatsV2["stageCounts"] | undefined, keys: K[]) {
  const map = new Map((items ?? []).map((item) => [item.key, item.count] as const));
  return keys.map((key) => ({ key, count: map.get(key) ?? 0 }));
}

/**
 * 攻击面：拦截发生在哪一段、载荷是不是加密的。
 *
 * 规则集 v2 的要点是「加密流量在解密之后检查」：载荷来源里 sealed / legacy 的拦截在 v1 根本不会发生
 * （WAF 只看到密文），所以把这两类合在一起单独给一个数字。两组都只数拦截（action = block）。
 */
export function AttackSurfaceCard({
  stats,
  onPick,
  className
}: {
  stats: FirewallStatsV2;
  onPick: (filter: FirewallEventQuery) => void;
  className?: string;
}) {
  const stages = useMemo(() => countsOf(stats.stageCounts, STAGES), [stats.stageCounts]);
  const transports = useMemo(() => countsOf(stats.transportCounts, TRANSPORTS), [stats.transportCounts]);
  const stageTotal = stages.reduce((sum, item) => sum + item.count, 0);
  const transportTotal = transports.reduce((sum, item) => sum + item.count, 0);
  const decrypted = transports.filter((item) => item.key !== "plain").reduce((sum, item) => sum + item.count, 0);
  const pieData = stages.filter((item) => item.count > 0).map((item) => ({ ...item, fill: `var(--color-${item.key})` }));

  return (
    <OverviewCard icon={Layers} tone="violet" title="攻击面" description="只计拦截" className={className}>
      <section aria-label="检测阶段" className="flex min-w-0 items-center gap-4">
        <div className="relative size-[116px] shrink-0">
          {pieData.length === 0 ? (
            <div aria-hidden className="size-full rounded-full border-[14px] border-muted" />
          ) : (
            <ChartContainer config={STAGE_CONFIG} className="aspect-square size-full">
              <PieChart>
                <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel nameKey="key" />} />
                <Pie
                  data={pieData}
                  dataKey="count"
                  nameKey="key"
                  innerRadius={40}
                  outerRadius={56}
                  paddingAngle={pieData.length > 1 ? 2 : 0}
                  strokeWidth={0}
                  isAnimationActive={false}
                  onClick={(_, index) => {
                    const stage = pieData[index]?.key;
                    if (stage) onPick({ stage });
                  }}
                  className="cursor-pointer"
                >
                  {pieData.map((item) => (
                    <Cell key={item.key} fill={item.fill} />
                  ))}
                </Pie>
              </PieChart>
            </ChartContainer>
          )}
          <span className="pointer-events-none absolute inset-0 grid place-items-center text-center">
            <span>
              <span className="block font-data text-lg leading-6 font-semibold tabular-nums">
                <NumberFlow value={stageTotal} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
              </span>
              <span className="block text-[10px] text-muted-foreground">拦截</span>
            </span>
          </span>
        </div>
        <ul className="flex min-w-0 flex-1 flex-col gap-0.5">
          {stages.map((item) => {
            const meta = STAGE_META[item.key];
            return (
              <li key={item.key}>
                <button
                  type="button"
                  title={meta.hint}
                  onClick={() => onPick({ stage: item.key })}
                  className="flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                >
                  <span aria-hidden className={cn("size-2 shrink-0 rounded-full", STAGE_DOT[item.key])} />
                  <span className="truncate">{meta.label}</span>
                  <span className="ml-auto shrink-0 font-data font-medium tabular-nums">{item.count.toLocaleString("zh-CN")}</span>
                  <span className="w-10 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{formatShare(item.count, stageTotal)}</span>
                </button>
              </li>
            );
          })}
        </ul>
      </section>

      <section aria-label="载荷来源" className="mt-4 flex min-w-0 flex-col gap-3 border-t pt-4">
        <div className="flex min-w-0 items-end justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
              <Lock className="size-3.5 text-emerald-600 dark:text-emerald-400" />
              解密后检测的拦截
            </div>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="font-data text-2xl leading-7 font-semibold tabular-nums text-emerald-700 dark:text-emerald-400">
                <NumberFlow value={decrypted} locales="zh-CN" format={{ notation: decrypted >= 100_000 ? "compact" : "standard", maximumFractionDigits: 1 }} />
              </span>
              <span className="text-xs text-muted-foreground tabular-nums">{formatShare(decrypted, transportTotal)}</span>
            </div>
          </div>
        </div>
        <Meter
          className="h-2"
          segments={transports.map((item) => ({ value: item.count, className: TRANSPORT_BAR[item.key], label: TRANSPORT_META[item.key].label }))}
        />
        <ul className="-mx-2 flex flex-col gap-0.5">
          {transports.map((item) => {
            const meta = TRANSPORT_META[item.key];
            const Icon = meta.icon;
            return (
              <li key={item.key}>
                <button
                  type="button"
                  title={meta.hint}
                  onClick={() => onPick({ transport: item.key })}
                  className="flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                >
                  <span aria-hidden className={cn("size-2 shrink-0 rounded-[2px]", TRANSPORT_BAR[item.key])} />
                  <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{meta.label}</span>
                  <span className="ml-auto shrink-0 font-data font-medium tabular-nums">{item.count.toLocaleString("zh-CN")}</span>
                  <span className="w-10 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{formatShare(item.count, transportTotal)}</span>
                </button>
              </li>
            );
          })}
        </ul>
      </section>
    </OverviewCard>
  );
}
