"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, Line, ComposedChart, Pie, PieChart, XAxis, YAxis } from "recharts";
import { BadgePercent, Coins, Megaphone, ReceiptText, TicketCheck, TicketPercent, Tickets, TrendingDown } from "lucide-react";
import { buildChartConfig, categoricalColor, ChartCard, MONEY_COLORS } from "@/components/commerce/commerce-charts";
import { compactMoney, formatMoney, formatPercent, moneyRatio, sumMoney, toCents } from "@/components/commerce/commerce-format";
import { MetricCard, Section, formatInt } from "@/components/users/detail/kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { CouponRankItem } from "@/lib/api/coupons";
import { useCouponOverviewQuery } from "@/lib/coupon-hooks";
import { SOURCE_LABEL } from "./coupon-shared";

/**
 * 概览：发了多少券、用了多少、优惠了多少钱、带来多少实收；近 N 天的发券 / 核销与订单趋势；
 * 券与活动的排行；券从哪些渠道发出去。金额求和与比例一律按「分」用 BigInt 算（commerce-format）。
 */
export function CouponOverviewPanel({ appKey }: { appKey: string }) {
  const [days, setDays] = useState(14);
  const query = useCouponOverviewQuery(appKey, days);
  const data = query.data;
  const loading = query.isLoading;

  const discount = data ? sumMoney([data.couponDiscount, data.promotionDiscount]) : "0";
  const useRate = data && data.issued > 0 ? data.used / data.issued : null;
  const discountRate = data ? moneyRatio(discount, data.originalAmount) : null;

  const trendConfig = useMemo(
    () =>
      buildChartConfig([
        { key: "issued", label: "发券", color: { light: "#6366f1", dark: "#818cf8" } },
        { key: "used", label: "核销", color: MONEY_COLORS.paid },
        { key: "orders", label: "优惠订单", color: { light: "#f59e0b", dark: "#fbbf24" } }
      ]),
    []
  );
  const moneyConfig = useMemo(
    () =>
      buildChartConfig([
        { key: "paid", label: "实收", color: MONEY_COLORS.paid },
        { key: "discount", label: "优惠", color: MONEY_COLORS.refunded }
      ]),
    []
  );
  const rows = useMemo(
    () =>
      (data?.trend ?? []).map((point) => ({
        label: point.date.slice(5),
        issued: point.issued,
        used: point.used,
        orders: point.orders,
        // 喂给图表的坐标值才转 number；提示框仍显示原始字符串
        paid: Number(toCents(point.paid)) / 100,
        discount: Number(toCents(point.discount)) / 100,
        paidText: point.paid,
        discountText: point.discount
      })),
    [data?.trend]
  );
  const [trendKind, setTrendKind] = useState<"count" | "money">("count");

  const sources = useMemo(() => (data?.sources ?? []).filter((item) => item.count > 0), [data?.sources]);
  const sourceConfig = useMemo(
    () => buildChartConfig(sources.map((item, index) => ({ key: item.source, label: SOURCE_LABEL[item.source] ?? item.source, color: categoricalColor(index) }))),
    [sources]
  );
  const sourceTotal = sources.reduce((sum, item) => sum + item.count, 0);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <MetricCard label="已发券" value={formatInt(data?.issued)} sub={`可用 ${formatInt(data?.available)} · 过期 ${formatInt(data?.expired)}`} icon={Tickets} tone="primary" loading={loading} />
        <MetricCard label="已核销" value={formatInt(data?.used)} sub={`核销率 ${formatPercent(useRate)}`} icon={TicketCheck} tone="success" loading={loading} />
        <MetricCard label="优惠订单" value={formatInt(data?.orders)} sub={`返券 ${formatInt(data?.rebateCoupons)} 张`} icon={ReceiptText} tone="info" loading={loading} />
        <MetricCard label="优惠后实收" value={formatMoney(data?.paidAmount)} sub={`原价 ${formatMoney(data?.originalAmount)}`} icon={Coins} tone="success" loading={loading} />
        <MetricCard label="让利总额" value={formatMoney(discount)} sub={`占原价 ${formatPercent(discountRate)}`} icon={TrendingDown} tone="danger" loading={loading} />
        <MetricCard
          label="进行中"
          value={`${formatInt(data?.activeTemplates)} / ${formatInt(data?.activePromotions)}`}
          sub={`券 ${formatInt(data?.templates)} 种 · 活动 ${formatInt(data?.promotions)} 期`}
          icon={BadgePercent}
          tone="violet"
          loading={loading}
        />
      </div>

      <ChartCard
        title={`近 ${days} 天`}
        description={trendKind === "count" ? "每天发出与核销的券、用了优惠的已付款订单" : "优惠订单的实收与让利（按浏览器时区分日）"}
        loading={loading}
        empty={rows.length === 0}
        height={240}
        action={
          <div className="flex items-center gap-2">
            <ToggleGroup type="single" size="sm" variant="outline" value={trendKind} onValueChange={(value) => value && setTrendKind(value as "count" | "money")}>
              <ToggleGroupItem value="count" className="px-2.5 text-xs">数量</ToggleGroupItem>
              <ToggleGroupItem value="money" className="px-2.5 text-xs">金额</ToggleGroupItem>
            </ToggleGroup>
            <ToggleGroup type="single" size="sm" variant="outline" value={String(days)} onValueChange={(value) => value && setDays(Number(value))}>
              {[7, 14, 30].map((value) => (
                <ToggleGroupItem key={value} value={String(value)} className="px-2.5 text-xs">
                  {value} 天
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        }
      >
        {trendKind === "count" ? (
          <ChartContainer config={trendConfig} className="h-[240px] w-full">
            <ComposedChart data={rows} margin={{ left: 0, right: 8, top: 8, bottom: 0 }} barCategoryGap="22%">
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={20} fontSize={11} />
              <YAxis tickLine={false} axisLine={false} width={36} fontSize={11} allowDecimals={false} />
              <ChartTooltip cursor={{ fillOpacity: 0.35 }} content={<ChartTooltipContent />} />
              <Bar dataKey="issued" fill="var(--color-issued)" radius={[3, 3, 0, 0]} />
              <Bar dataKey="used" fill="var(--color-used)" radius={[3, 3, 0, 0]} />
              <Line dataKey="orders" type="monotone" stroke="var(--color-orders)" strokeWidth={2} dot={false} />
            </ComposedChart>
          </ChartContainer>
        ) : (
          <ChartContainer config={moneyConfig} className="h-[240px] w-full">
            <BarChart data={rows} margin={{ left: 0, right: 8, top: 8, bottom: 0 }} barCategoryGap="22%">
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={20} fontSize={11} />
              <YAxis tickLine={false} axisLine={false} width={48} fontSize={11} tickFormatter={compactMoney} />
              <ChartTooltip
                cursor={{ fillOpacity: 0.35 }}
                content={
                  <ChartTooltipContent
                    formatter={(value, name, item) => {
                      const row = item?.payload as { paidText?: string; discountText?: string } | undefined;
                      const text = name === "paid" ? row?.paidText : row?.discountText;
                      return (
                        <span className="flex w-full items-center justify-between gap-3">
                          <span className="text-muted-foreground">{name === "paid" ? "实收" : "让利"}</span>
                          <span className="font-mono tabular-nums">{formatMoney(text ?? String(value))}</span>
                        </span>
                      );
                    }}
                  />
                }
              />
              <Bar dataKey="paid" stackId="m" fill="var(--color-paid)" />
              <Bar dataKey="discount" stackId="m" fill="var(--color-discount)" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ChartContainer>
        )}
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-3">
        <RankSection title="券排行" icon={TicketPercent} items={data?.topTemplates} loading={loading} empty="还没有订单用过券" discountLabel="券优惠" />
        <RankSection title="活动排行" icon={Megaphone} items={data?.topPromotions} loading={loading} empty="还没有订单参加过活动" discountLabel="活动优惠" />
        <ChartCard title="发券来源" description="各渠道发出去的券" loading={loading} empty={sourceTotal === 0} emptyText="还没有发过券" height={200}>
          <div className="grid items-center gap-3 sm:grid-cols-[160px_minmax(0,1fr)] lg:grid-cols-1 xl:grid-cols-[150px_minmax(0,1fr)]">
            <ChartContainer config={sourceConfig} className="mx-auto aspect-square h-[150px]">
              <PieChart>
                <ChartTooltip content={<ChartTooltipContent nameKey="source" hideLabel />} />
                <Pie data={sources} dataKey="count" nameKey="source" innerRadius={44} outerRadius={70} strokeWidth={2} paddingAngle={2}>
                  {sources.map((item) => (
                    <Cell key={item.source} fill={`var(--color-${item.source})`} />
                  ))}
                </Pie>
              </PieChart>
            </ChartContainer>
            <ul className="space-y-1">
              {sources.map((item) => (
                <li key={item.source} className="flex items-center gap-2 text-xs">
                  <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: `var(--color-${item.source})` }} aria-hidden />
                  <span className="flex-1 truncate">{SOURCE_LABEL[item.source] ?? item.source}</span>
                  <span className="tabular-nums">{formatInt(item.count)}</span>
                  <span className="w-12 text-right text-[11px] tabular-nums text-muted-foreground">
                    {formatPercent(sourceTotal ? item.count / sourceTotal : null)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        </ChartCard>
      </div>
    </div>
  );
}

function RankSection({
  title,
  icon,
  items,
  loading,
  empty,
  discountLabel
}: {
  title: string;
  icon: React.ComponentType<{ className?: string }>;
  items?: CouponRankItem[];
  loading?: boolean;
  empty: string;
  discountLabel: string;
}) {
  const list = items ?? [];
  const top = list.reduce((max, item) => Math.max(max, item.count), 0);
  const config = useMemo(() => buildChartConfig([{ key: "count", label: "订单", color: { light: "#6366f1", dark: "#818cf8" } }]), []);
  return (
    <Section title={title} icon={icon} tone="info" flush>
      {loading ? (
        <div className="px-5 py-8 text-center text-xs text-muted-foreground">加载中</div>
      ) : list.length === 0 ? (
        <div className="px-5 py-8 text-center text-xs text-muted-foreground">{empty}</div>
      ) : (
        <div className="px-3 py-3">
          <ChartContainer config={config} className="w-full" style={{ height: Math.max(120, list.length * 34) }}>
            <BarChart data={list} layout="vertical" margin={{ left: 0, right: 12, top: 0, bottom: 0 }} barCategoryGap="28%">
              <XAxis type="number" hide domain={[0, Math.max(top, 1)]} />
              <YAxis type="category" dataKey="name" width={96} tickLine={false} axisLine={false} fontSize={11} />
              <ChartTooltip
                cursor={{ fillOpacity: 0.3 }}
                content={
                  <ChartTooltipContent
                    hideLabel
                    formatter={(_value, _name, item) => {
                      const row = item?.payload as CouponRankItem | undefined;
                      if (!row) return null;
                      return (
                        <div className="grid min-w-40 gap-1 text-xs">
                          <div className="font-medium">{row.name}</div>
                          <div className="flex justify-between gap-3">
                            <span className="text-muted-foreground">订单</span>
                            <span className="tabular-nums">{formatInt(row.count)}</span>
                          </div>
                          <div className="flex justify-between gap-3">
                            <span className="text-muted-foreground">{discountLabel}</span>
                            <span className="font-mono tabular-nums">{formatMoney(row.discount)}</span>
                          </div>
                          <div className="flex justify-between gap-3">
                            <span className="text-muted-foreground">实收</span>
                            <span className="font-mono tabular-nums">{formatMoney(row.paid)}</span>
                          </div>
                        </div>
                      );
                    }}
                  />
                }
              />
              <Bar dataKey="count" fill="var(--color-count)" radius={[0, 4, 4, 0]} />
            </BarChart>
          </ChartContainer>
        </div>
      )}
    </Section>
  );
}
