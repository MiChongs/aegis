"use client";

import { useMemo } from "react";
import { Cell, Label, Pie, PieChart } from "recharts";
import { Dices, Hash, Lock, RefreshCw, Repeat, Ticket, Users } from "lucide-react";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
import { MetricCard, Section } from "@/components/users/detail/kit";
import { formatInt } from "@/components/users/detail/kit/amount";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { NUMBER_TIERS, type NumberHoldKind, type NumberTierSpec } from "@/lib/api/user-number";
import { useNumberOverviewQuery } from "@/lib/user-number-hooks";
import { HOLD_KIND_META, RightLabel, rightUnit, tierSpec } from "./number-shared";

/**
 * 总览：发了多少号、各级多少个、换号与摇号的热度、占着的号与还没用掉的权益。
 * 各级颜色用目录下发的建议主色，与客户端徽章一致。
 */
export function NumberOverviewPanel({
  appKey,
  tiers,
  onOpenHolds
}: {
  appKey: string;
  tiers?: NumberTierSpec[] | null;
  onOpenHolds: (kind: NumberHoldKind) => void;
}) {
  const query = useNumberOverviewQuery(appKey);
  const data = query.data;
  const loading = query.isLoading;
  const tierList = tiers?.length ? tiers : NUMBER_TIERS;

  const config = useMemo(
    () => buildChartConfig(tierList.map((tier) => ({ key: tier.key, label: tier.name, color: { light: tier.color, dark: tier.color } }))),
    [tierList]
  );

  const rows = useMemo(
    () =>
      tierList.map((tier) => ({
        key: tier.key,
        level: tier.level,
        name: tier.name,
        value: data?.byTier?.find((item) => item.tier === tier.level)?.count ?? 0
      })),
    [data?.byTier, tierList]
  );
  const total = rows.reduce((sum, row) => sum + row.value, 0);
  const pretty = total - (rows.find((row) => row.level === 0)?.value ?? 0);
  const outstanding = (data?.outstanding ?? []).filter((right) => right.balance > 0);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <MetricCard label="已发号用户" value={formatInt(data?.numbered)} sub={`共 ${formatInt(data?.users)} 个用户`} icon={Users} tone="primary" loading={loading} />
        <MetricCard label="靓号" value={formatInt(pretty)} sub={total ? `占 ${((pretty / total) * 100).toFixed(1)}%` : undefined} icon={Hash} tone="violet" loading={loading} />
        <MetricCard label="今日换号" value={formatInt(data?.changesToday)} icon={Repeat} tone="info" loading={loading} />
        <MetricCard label="近 7 天换号" value={formatInt(data?.changes7d)} icon={RefreshCw} tone="info" loading={loading} />
        <MetricCard label="今日摇号" value={formatInt(data?.drawsToday)} icon={Dices} tone="warning" loading={loading} />
        <MetricCard label="待选号的摇号单" value={formatInt(data?.openDraws)} icon={Ticket} tone="warning" loading={loading} />
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <ChartCard title="等级分布" loading={loading} empty={total === 0} emptyText="暂无号码" height={220}>
          <div className="grid items-center gap-4 sm:grid-cols-[220px_minmax(0,1fr)]">
            <ChartContainer config={config} className="mx-auto aspect-square h-[220px]">
              <PieChart>
                <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
                <Pie data={rows.filter((row) => row.value > 0)} dataKey="value" nameKey="key" innerRadius={62} outerRadius={92} strokeWidth={2} paddingAngle={2}>
                  {rows
                    .filter((row) => row.value > 0)
                    .map((row) => (
                      <Cell key={row.key} fill={`var(--color-${row.key})`} />
                    ))}
                  <Label
                    content={({ viewBox }) => {
                      if (!viewBox || !("cx" in viewBox)) return null;
                      return (
                        <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) - 6} className="fill-foreground text-xl font-semibold">
                            {formatInt(total)}
                          </tspan>
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 14} className="fill-muted-foreground text-[11px]">
                            个号码
                          </tspan>
                        </text>
                      );
                    }}
                  />
                </Pie>
              </PieChart>
            </ChartContainer>
            <ul className="space-y-1.5">
              {[...rows].reverse().map((row) => {
                const spec = tierSpec(row.level, tierList);
                const share = total ? (row.value / total) * 100 : 0;
                return (
                  <li key={row.key} className="flex items-center gap-3 rounded-lg px-2 py-1.5">
                    <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: spec.color }} aria-hidden />
                    <span className="flex-1 text-xs">{spec.name}</span>
                    <span className="text-xs font-medium tabular-nums">{formatInt(row.value)}</span>
                    <span className="w-14 text-right text-[11px] tabular-nums text-muted-foreground">{share.toFixed(share > 0 && share < 0.1 ? 2 : 1)}%</span>
                  </li>
                );
              })}
            </ul>
          </div>
        </ChartCard>

        <div className="space-y-4">
          <Section title="占用" icon={Lock} tone="warning">
            <div className="grid grid-cols-3 gap-2">
              {(Object.keys(HOLD_KIND_META) as NumberHoldKind[]).map((kind) => (
                <button
                  key={kind}
                  type="button"
                  onClick={() => onOpenHolds(kind)}
                  className="rounded-lg border bg-muted/30 px-3 py-2.5 text-left transition-colors hover:bg-accent/40"
                >
                  <div className="text-[11px] text-muted-foreground">{HOLD_KIND_META[kind].label}</div>
                  <div className="mt-0.5 text-lg font-semibold tabular-nums">{loading ? "—" : formatInt(data?.holds?.[kind] ?? 0)}</div>
                </button>
              ))}
            </div>
          </Section>

          <Section title="未使用的权益" icon={Ticket} tone="violet" flush>
            {outstanding.length === 0 ? (
              <div className="px-5 py-6 text-center text-xs text-muted-foreground">{loading ? "加载中" : "暂无未使用的权益"}</div>
            ) : (
              <ul className="divide-y">
                {outstanding.map((right) => (
                  <li key={`${right.kind}-${right.level}`} className="flex items-center justify-between gap-3 px-4 py-2.5 text-xs sm:px-5">
                    <RightLabel kind={right.kind} level={right.level} name={right.name} />
                    <span className="font-medium tabular-nums">
                      {formatInt(right.balance)}
                      <span className="ml-0.5 font-normal text-muted-foreground">{rightUnit(right.kind)}</span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Section>
        </div>
      </div>
    </div>
  );
}
