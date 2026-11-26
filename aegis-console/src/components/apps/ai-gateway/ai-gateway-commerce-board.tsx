"use client";

import { useMemo, useState } from "react";
import { useTheme } from "next-themes";
import { Bar, CartesianGrid, Cell, ComposedChart, Label, Line, Pie, PieChart, XAxis, YAxis } from "recharts";
import { AlarmClock, Coins, Landmark, Package, ShoppingBag, Users } from "lucide-react";
import { formatCompactPoints, formatYuan } from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import { MONEY_COLORS, buildChartConfig, categoricalColor, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAIGatewayCommerceQuery } from "@/lib/ai-gateway-hooks";
import { cn } from "@/lib/utils";

const RANGES = [7, 30, 90] as const;
const DAILY_SERIES: Array<{ key: "revenue" | "refunded" | "orders"; label: string; color: ThemedColor }> = [
  { key: "revenue", label: "营收（元）", color: MONEY_COLORS.paid },
  { key: "refunded", label: "退款（元）", color: MONEY_COLORS.refunded },
  { key: "orders", label: "订单", color: MONEY_COLORS.net }
];
const EXPIRY_LABEL: Record<string, { label: string; tone: string }> = {
  "7d": { label: "7 天内", tone: "bg-rose-500" },
  "30d": { label: "8–30 天", tone: "bg-amber-500" },
  "90d": { label: "31–90 天", tone: "bg-sky-500" },
  later: { label: "90 天以后", tone: "bg-violet-500" },
  never: { label: "永久", tone: "bg-emerald-500" }
};

/**
 * 额度生意：卖了多少钱、多少人买、卖出多少额度；还欠用户多少没用掉的额度（负债）与它们什么时候到期；
 * 各时段的调用量与让利（按时段倍率折回的差额，加价时段为负）。
 */
export function CommerceBoard({ appKey }: { appKey: string }) {
  const [days, setDays] = useState<number>(30);
  const query = useAIGatewayCommerceQuery(appKey, days);
  const data = query.data;
  const { resolvedTheme } = useTheme();

  const dailyConfig = useMemo(() => buildChartConfig(DAILY_SERIES), []);
  const dailyRows = useMemo(
    () =>
      (data?.daily ?? []).map((day) => ({
        label: day.date.slice(5),
        revenue: Number(day.revenue),
        refunded: Number(day.refunded),
        orders: day.orders
      })),
    [data]
  );
  const packageRows = useMemo(
    () =>
      (data?.byPackage ?? [])
        .map((item, index) => ({ ...item, slot: `p${index}`, value: Number(item.revenue) }))
        .filter((item) => item.value > 0),
    [data]
  );
  const packageConfig = useMemo(
    () => buildChartConfig(packageRows.map((item, index) => ({ key: item.slot, label: item.name, color: categoricalColor(index) }))),
    [packageRows]
  );
  const packageRevenue = packageRows.reduce((sum, item) => sum + item.value, 0);
  /** 列表与环图同色：按环图里的序号取色，没有营收的包不进环图、用中性色。 */
  const packageColor = (packageId: number) => {
    const index = packageRows.findIndex((item) => item.packageId === packageId);
    if (index < 0) return undefined;
    const color = categoricalColor(index);
    return resolvedTheme === "dark" ? color.dark : color.light;
  };

  if (query.isLoading && !data) {
    return (
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-3 lg:grid-cols-6">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-20 rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-64 rounded-2xl" />
      </div>
    );
  }

  const outstanding = Number(data?.outstanding ?? 0);
  const permanent = Number(data?.outstandingPermanent ?? 0);
  const expiringSoon = Number(data?.expiringSoon ?? 0);
  const liability = outstanding + permanent || 1;
  const expiryTotal = (data?.expiry ?? []).reduce((sum, item) => sum + Number(item.amount), 0) || 1;
  const maxSaved = Math.max(1, ...(data?.windows ?? []).map((item) => Math.abs(Number(item.saved))));
  const maxRequests = Math.max(1, ...(data?.windows ?? []).map((item) => item.requests));
  const net = Number(data?.revenue ?? 0) - Number(data?.refunded ?? 0);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">近 {days} 天的额度包生意；负债与到期是此刻的存量</p>
        <ToggleGroup type="single" size="sm" value={String(days)} onValueChange={(value) => value && setDays(Number(value))}>
          {RANGES.map((range) => (
            <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
              {range} 天
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <StatTile label="营收" icon={Coins} tone="positive" value={formatYuan(data?.revenue)}
          hint={Number(data?.refunded) > 0 ? `退款 ${formatYuan(data?.refunded)} · 净 ${formatYuan(net)}` : "没有退款"} />
        <StatTile label="订单" icon={ShoppingBag} value={data?.orders ?? 0}
          hint={data && data.orders > 0 ? `客单价 ${formatYuan(Number(data.revenue) / data.orders)}` : undefined} />
        <StatTile label="买家" icon={Users} value={data?.buyers ?? 0}
          hint={data && data.buyers > 0 ? `人均 ${(data.orders / data.buyers).toFixed(1)} 单` : undefined} />
        <StatTile label="售出额度" icon={Package} value={formatCompactPoints(data?.pointsSold)}
          hint={data && Number(data.pointsSold) > 0 ? `均价 ${formatYuan((Number(data.revenue) / Number(data.pointsSold)) * 10_000)} / 万` : undefined} />
        <StatTile label="未消耗负债" icon={Landmark} value={formatCompactPoints(outstanding + permanent)}
          hint={`额度包 ${formatCompactPoints(outstanding)} · 永久 ${formatCompactPoints(permanent)}`} />
        <StatTile label="7 天内到期" icon={AlarmClock} value={formatCompactPoints(expiringSoon)}
          hint={outstanding > 0 ? `占额度包负债 ${Math.round((expiringSoon / outstanding) * 100)}%` : undefined} />
      </div>

      <ChartCard
        title="按日营收"
        description="柱为营收与退款（元），线为订单数"
        loading={query.isLoading}
        empty={dailyRows.every((row) => row.revenue === 0 && row.refunded === 0 && row.orders === 0)}
        height={240}
      >
        <ChartContainer config={dailyConfig} className="h-[240px] w-full">
          <ComposedChart data={dailyRows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={12} />
            <YAxis yAxisId="money" tickLine={false} axisLine={false} fontSize={11} width={44} />
            <YAxis yAxisId="orders" orientation="right" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={28} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar yAxisId="money" dataKey="revenue" fill="var(--color-revenue)" radius={[3, 3, 0, 0]} />
            <Bar yAxisId="money" dataKey="refunded" fill="var(--color-refunded)" radius={[3, 3, 0, 0]} />
            <Line yAxisId="orders" dataKey="orders" type="monotone" stroke="var(--color-orders)" strokeWidth={2} dot={false} />
            <ChartLegend content={<ChartLegendContent />} />
          </ComposedChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title="按额度包" description="营收占比" loading={query.isLoading} empty={packageRows.length === 0} height={220}>
          <div className="grid items-center gap-4 sm:grid-cols-[220px_minmax(0,1fr)]">
            <ChartContainer config={packageConfig} className="mx-auto h-[220px] w-full max-w-[220px]">
              <PieChart>
                <ChartTooltip content={<ChartTooltipContent nameKey="slot" hideLabel />} />
                <Pie data={packageRows} dataKey="value" nameKey="slot" innerRadius={58} outerRadius={88} strokeWidth={2} paddingAngle={2}>
                  {packageRows.map((item) => (
                    <Cell key={item.slot} fill={`var(--color-${item.slot})`} />
                  ))}
                  <Label
                    content={({ viewBox }) => {
                      if (!viewBox || !("cx" in viewBox)) return null;
                      return (
                        <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                          <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-base font-bold">
                            {formatYuan(packageRevenue)}
                          </tspan>
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                            营收
                          </tspan>
                        </text>
                      );
                    }}
                  />
                </Pie>
              </PieChart>
            </ChartContainer>
            <div className="space-y-2">
              {(data?.byPackage ?? []).map((item) => {
                const share = packageRevenue > 0 ? Number(item.revenue) / packageRevenue : 0;
                return (
                  <div key={item.packageId} className="space-y-1 text-xs">
                    <div className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-sm bg-muted-foreground/30" style={{ background: packageColor(item.packageId) }} />
                      <span className="min-w-0 flex-1 truncate font-medium">{item.name}</span>
                      <span className="font-mono tabular-nums">{formatYuan(item.revenue)}</span>
                    </div>
                    <div className="flex gap-3 pl-4 text-[10px] text-muted-foreground tabular-nums">
                      <span>{item.orders} 单</span>
                      <span>{formatCompactPoints(item.points)} 额度</span>
                      <span className="ml-auto">{Math.round(share * 100)}%</span>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        </ChartCard>

        <div className="space-y-4 rounded-xl border bg-card p-4">
          <div>
            <h3 className="text-sm font-medium">负债与到期</h3>
            <p className="text-[11px] text-muted-foreground">用户手里还没用掉的额度（不含暂扣中）</p>
          </div>
          <div className="space-y-1.5">
            <div className="flex h-3 overflow-hidden rounded-full bg-muted">
              <span className="bg-violet-500" style={{ width: `${(outstanding / liability) * 100}%` }} title={`额度包 ${formatPoints(outstanding)}`} />
              <span className="bg-emerald-500" style={{ width: `${(permanent / liability) * 100}%` }} title={`永久额度 ${formatPoints(permanent)}`} />
            </div>
            <div className="flex justify-between text-[11px] text-muted-foreground">
              <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm bg-violet-500" />额度包 {formatCompactPoints(outstanding)}</span>
              <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm bg-emerald-500" />永久 {formatCompactPoints(permanent)}</span>
            </div>
          </div>
          <div className="space-y-2 border-t border-dashed pt-3">
            <div className="text-[11px] font-medium text-muted-foreground">额度包按到期时间</div>
            {(data?.expiry ?? []).length === 0 ? (
              <p className="py-4 text-center text-xs text-muted-foreground">暂无</p>
            ) : (
              (data?.expiry ?? []).map((item) => {
                const meta = EXPIRY_LABEL[item.bucket] ?? { label: item.bucket, tone: "bg-muted-foreground" };
                const value = Number(item.amount);
                return (
                  <div key={item.bucket} className="grid grid-cols-[4.5rem_minmax(0,1fr)_4.5rem] items-center gap-2 text-[11px]">
                    <span className="text-muted-foreground">{meta.label}</span>
                    <span className="h-2 overflow-hidden rounded-full bg-muted">
                      <span className={cn("block h-full rounded-full", meta.tone)} style={{ width: `${(value / expiryTotal) * 100}%` }} />
                    </span>
                    <span className="text-right font-mono tabular-nums">{formatCompactPoints(value)}</span>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>

      <div className="rounded-xl border bg-card p-4">
        <div className="mb-3">
          <h3 className="text-sm font-medium">按时段的调用与让利</h3>
          <p className="text-[11px] text-muted-foreground">让利 = 按调用当时的时段倍率折回原价与实收的差；加价时段为负（多收）</p>
        </div>
        {(data?.windows ?? []).length === 0 ? (
          <p className="py-8 text-center text-xs text-muted-foreground">没有时段内的调用</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-xs">
              <thead>
                <tr className="border-b text-[11px] text-muted-foreground">
                  <th className="py-2 text-left font-medium">时段</th>
                  <th className="w-[24%] px-2 py-2 text-left font-medium">调用</th>
                  <th className="px-2 py-2 text-right font-medium">实收额度</th>
                  <th className="w-[32%] px-2 py-2 text-center font-medium">让利 ← · → 多收</th>
                </tr>
              </thead>
              <tbody>
                {(data?.windows ?? []).map((item) => {
                  const saved = Number(item.saved);
                  const width = (Math.abs(saved) / maxSaved) * 50;
                  return (
                    <tr key={item.windowId || "standard"} className="border-b last:border-0">
                      <td className="py-2 pr-2">
                        <span className="font-medium">{item.name || "标准价"}</span>
                        {item.windowId ? <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{item.windowId}</span> : null}
                      </td>
                      <td className="px-2 py-2">
                        <div className="flex items-center gap-2">
                          <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                            <span className="block h-full rounded-full bg-sky-500" style={{ width: `${(item.requests / maxRequests) * 100}%` }} />
                          </span>
                          <span className="w-12 text-right font-mono tabular-nums">{item.requests.toLocaleString("zh-CN")}</span>
                        </div>
                      </td>
                      <td className="px-2 py-2 text-right font-mono tabular-nums">{formatPoints(item.charged)}</td>
                      <td className="px-2 py-2">
                        <div className="relative h-4">
                          <span className="absolute inset-y-0 left-1/2 w-px bg-border" />
                          {saved > 0 ? (
                            <span className="absolute inset-y-0.5 rounded-l bg-emerald-500/80" style={{ right: "50%", width: `${width}%` }} />
                          ) : saved < 0 ? (
                            <span className="absolute inset-y-0.5 rounded-r bg-rose-500/80" style={{ left: "50%", width: `${width}%` }} />
                          ) : null}
                          <span
                            className={cn(
                              "absolute top-1/2 -translate-y-1/2 font-mono text-[10px] tabular-nums",
                              saved >= 0 ? "left-[calc(50%+6px)]" : "right-[calc(50%+6px)]"
                            )}
                          >
                            {saved === 0 ? "—" : saved > 0 ? `让 ${formatCompactPoints(saved)}` : `多 ${formatCompactPoints(-saved)}`}
                          </span>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
