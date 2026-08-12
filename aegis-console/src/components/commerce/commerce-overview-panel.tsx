"use client";

import { useMemo, useState } from "react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Label,
  Pie,
  PieChart,
  XAxis,
  YAxis
} from "recharts";
import {
  AlertTriangle,
  ArrowDownRight,
  ArrowUpRight,
  BadgeCheck,
  Coins,
  FileText,
  Landmark,
  Receipt,
  Undo2,
  Users
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { EmptyState } from "@/components/ui/data-state";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useCommerceOverviewQuery, type CommerceWindow } from "@/lib/commerce-hooks";
import { useAdminPaymentMethodsQuery } from "@/lib/admin-hooks";
import type { CommerceTrend, OrderGroupStat, WalletTypeStat } from "@/lib/api/types";
import { formatMoney, walletTypeLabel } from "./commerce-format";
import {
  ChartCard,
  MONEY_COLORS,
  ORDER_STATUS_COLORS,
  WALLET_TYPE_COLORS,
  buildChartConfig
} from "./commerce-charts";

const orderStatusLabels: Record<string, string> = {
  paid: "已支付",
  pending: "待支付",
  expired: "已过期",
  failed: "失败",
  cancelled: "已取消",
  closed: "已关闭"
};

const bucketLabels: Record<string, string> = { day: "按天", week: "按周", month: "按月" };

/**
 * 交易概览。
 *
 * 三口径同屏：订单（收了多少）、退款（退回去多少）、钱包（平台还欠用户多少）。
 * 这三个数来自三张表，一次取齐是刻意的 —— 分开拉会出现「订单已刷新、
 * 退款还是上一个时间窗」这种自相矛盾的画面，而人会照着它做决定。
 */
export function CommerceOverviewPanel({
  appKey,
  window,
  walletCurrency
}: {
  appKey?: string | null;
  window: CommerceWindow;
  /** 钱包记账币种。只标在钱包那一段 —— 订单金额是按渠道计价的，可能不止一种货币 */
  walletCurrency?: string;
}) {
  const query = useCommerceOverviewQuery(appKey, window);
  const methodsQuery = useAdminPaymentMethodsQuery();
  const data = query.data;

  if (!appKey) {
    return <EmptyState title="未选择应用" description="交易数据按应用隔离，请先在顶部选择一个应用。" />;
  }
  if (query.isLoading) {
    return (
      <div className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, index) => (
            <Skeleton key={index} className="h-24 rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-[300px] rounded-xl" />
      </div>
    );
  }
  if (query.isError || !data) {
    return <EmptyState title="加载失败" description="无法获取交易概览，请稍后重试。" />;
  }

  const { orders, wallet, trend, receipt } = data;

  return (
    <div className="space-y-6">
      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">订单与实收</h2>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <StatTile
            icon={<Coins className="size-4" />}
            label="已支付金额"
            value={formatMoney(orders.paidAmount)}
            hint={`${orders.paidOrders} 笔 · ${orders.payerCount} 位付费用户 · 按到账时间`}
          />
          <StatTile
            icon={<Undo2 className="size-4" />}
            label="已退款金额"
            value={formatMoney(orders.refundedAmount)}
            hint={`${orders.refundCount} 笔已成功退款 · 按退款成功时间`}
            tone="warning"
          />
          <StatTile
            icon={<BadgeCheck className="size-4" />}
            label="实收净额"
            value={formatMoney(orders.netAmount)}
            hint="已支付 − 已成功退款"
            tone="positive"
          />
          <StatTile
            icon={<FileText className="size-4" />}
            label="待支付"
            value={formatMoney(orders.pendingAmount)}
            hint={`${orders.pendingOrders} 笔待支付 / 共 ${orders.totalOrders} 笔下单`}
          />
        </div>
      </section>

      <TrendChart trend={trend} loading={query.isFetching && !data} />

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">钱包资金</h2>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <StatTile
            icon={<ArrowUpRight className="size-4" />}
            label="入账合计"
            value={formatMoney(wallet.totalIn, walletCurrency)}
            hint="充值 / 退款 / 调增"
            tone="positive"
          />
          <StatTile
            icon={<ArrowDownRight className="size-4" />}
            label="出账合计"
            value={formatMoney(wallet.totalOut, walletCurrency)}
            hint="消费 / 订单支付 / 会员开通"
          />
          <StatTile
            icon={<Users className="size-4" />}
            label="流水笔数"
            value={String(wallet.count)}
            hint={`${wallet.userCount} 位用户发生过资金往来`}
          />
          <StatTile
            icon={<Landmark className="size-4" />}
            label="余额合计"
            value={formatMoney(wallet.balance, walletCurrency)}
            hint="平台此刻的待兑付负债（不受时间窗影响）"
            tone="warning"
          />
        </div>
      </section>

      <div className="grid gap-4 lg:grid-cols-3">
        <OrderStatusDonut items={orders.byStatus} />
        <MethodBars items={orders.byMethod} methodNames={methodsQuery.data} />
        <WalletTypeBars items={wallet.byType} currency={walletCurrency} />
      </div>

      <ReceiptCapabilityCard
        supportsCJK={receipt.supportsCJK}
        fontStatus={receipt.fontStatus}
        fontNotes={receipt.fontNotes}
        localeCount={receipt.locales?.length ?? 0}
        defaultLocale={receipt.defaultLocale}
      />
    </div>
  );
}

// ── 趋势 ──

type TrendMetric = "revenue" | "wallet";

/**
 * 交易趋势。
 *
 * 实收 / 退款与钱包出入分两组切换，而不是六条线挤在一张图上：
 * 六条线的图看起来很全，但没有人能从里面读出任何结论。
 */
function TrendChart({ trend, loading }: { trend?: CommerceTrend; loading?: boolean }) {
  const [metric, setMetric] = useState<TrendMetric>("revenue");
  const bucket = trend?.bucket ?? "day";

  const rows = useMemo(
    () =>
      (trend?.points ?? []).map((point) => ({
        label: point.label,
        paid: Number(point.paidAmount),
        refunded: Number(point.refundedAmount),
        net: Number(point.netAmount),
        walletIn: Number(point.walletIn),
        walletOut: Number(point.walletOut),
        orders: point.paidOrders
      })),
    [trend]
  );

  const config = useMemo(
    () =>
      metric === "revenue"
        ? buildChartConfig([
            { key: "paid", label: "已支付", color: MONEY_COLORS.paid },
            { key: "refunded", label: "已退款", color: MONEY_COLORS.refunded },
            { key: "net", label: "实收净额", color: MONEY_COLORS.net }
          ])
        : buildChartConfig([
            { key: "walletIn", label: "钱包入账", color: MONEY_COLORS.walletIn },
            { key: "walletOut", label: "钱包出账", color: MONEY_COLORS.walletOut }
          ]),
    [metric]
  );

  const keys = metric === "revenue" ? (["paid", "refunded", "net"] as const) : (["walletIn", "walletOut"] as const);
  const allZero = rows.every((row) => keys.every((key) => !row[key]));

  return (
    <ChartCard
      title="交易趋势"
      description={`${bucketLabels[bucket] ?? "按天"}分桶 · 实收按到账时间、退款按退款成功时间、钱包按流水时间`}
      loading={loading}
      empty={rows.length === 0 || allZero}
      emptyText={rows.length === 0 ? "该时间窗内没有资金记录" : "该时间窗内金额均为 0"}
      height={300}
      action={
        <ToggleGroup
          type="single"
          size="sm"
          value={metric}
          onValueChange={(value) => value && setMetric(value as TrendMetric)}
          className="shrink-0"
        >
          <ToggleGroupItem value="revenue" className="h-7 px-2.5 text-xs">
            收入
          </ToggleGroupItem>
          <ToggleGroupItem value="wallet" className="h-7 px-2.5 text-xs">
            钱包
          </ToggleGroupItem>
        </ToggleGroup>
      }
    >
      <ChartContainer config={config} className="h-[300px] w-full">
        <AreaChart data={rows} margin={{ left: 4, right: 8, top: 8 }}>
          <defs>
            {keys.map((key) => (
              <linearGradient key={key} id={`fill-${key}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={`var(--color-${key})`} stopOpacity={0.35} />
                <stop offset="95%" stopColor={`var(--color-${key})`} stopOpacity={0.02} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} fontSize={11} />
          <YAxis tickLine={false} axisLine={false} width={56} fontSize={11} tickFormatter={compactNumber} />
          <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
          {keys.map((key) => (
            <Area
              key={key}
              type="monotone"
              dataKey={key}
              stroke={`var(--color-${key})`}
              fill={`url(#fill-${key})`}
              strokeWidth={2}
              dot={false}
              // 实收净额画成虚线：它是算出来的，不是一笔一笔真实发生的资金
              strokeDasharray={key === "net" ? "4 3" : undefined}
            />
          ))}
          <ChartLegend content={<ChartLegendContent className="flex-wrap gap-x-3 gap-y-1" />} />
        </AreaChart>
      </ChartContainer>
    </ChartCard>
  );
}

// ── 分布 ──

function OrderStatusDonut({ items }: { items: OrderGroupStat[] }) {
  const rows = useMemo(() => items.filter((item) => item.count > 0), [items]);
  const total = rows.reduce((sum, item) => sum + item.count, 0);
  const config = useMemo(
    () =>
      buildChartConfig(
        rows.map((item) => ({
          key: item.key,
          label: orderStatusLabels[item.key] ?? item.key,
          color: ORDER_STATUS_COLORS[item.key]
        }))
      ),
    [rows]
  );
  const paidShare = total > 0 ? (rows.find((item) => item.key === "paid")?.count ?? 0) / total : 0;

  return (
    <ChartCard title="订单状态分布" description="按下单时间统计" empty={total === 0} height={220}>
      <>
        <ChartContainer config={config} className="mx-auto h-[220px] w-full">
          <PieChart>
            <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
            <Pie data={rows} dataKey="count" nameKey="key" innerRadius={55} outerRadius={85} strokeWidth={2} paddingAngle={2}>
              {rows.map((item) => (
                <Cell key={item.key} fill={`var(--color-${item.key})`} />
              ))}
              <Label
                content={({ viewBox }) => {
                  if (!viewBox || !("cx" in viewBox)) return null;
                  return (
                    <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                      <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                        {total.toLocaleString("zh-CN")}
                      </tspan>
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                        笔订单
                      </tspan>
                    </text>
                  );
                }}
              />
            </Pie>
            <ChartLegend content={<ChartLegendContent nameKey="key" className="flex-wrap gap-x-3 gap-y-1" />} />
          </PieChart>
        </ChartContainer>
        <p className="text-[10px] text-muted-foreground">支付成功率 {(paidShare * 100).toFixed(1)}%</p>
      </>
    </ChartCard>
  );
}

function MethodBars({
  items,
  methodNames
}: {
  items: OrderGroupStat[];
  methodNames?: Array<{ method: string; name: string }>;
}) {
  const nameOf = useMemo(() => {
    const map = new Map((methodNames ?? []).map((item) => [item.method, item.name] as const));
    return (key: string) => map.get(key) ?? key;
  }, [methodNames]);

  const rows = useMemo(
    () =>
      [...items]
        .filter((item) => item.count > 0)
        .sort((a, b) => Number(b.amount) - Number(a.amount))
        .map((item) => ({ key: item.key, name: nameOf(item.key), amount: Number(item.amount), count: item.count })),
    [items, nameOf]
  );
  const config = useMemo(() => buildChartConfig([{ key: "amount", label: "金额" }]), []);

  return (
    <ChartCard title="支付渠道分布" description="按下单金额排序" empty={rows.length === 0} height={220}>
      <ChartContainer config={config} className="h-[220px] w-full">
        <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 12 }}>
          <CartesianGrid horizontal={false} strokeDasharray="3 3" />
          <XAxis type="number" tickLine={false} axisLine={false} fontSize={11} tickFormatter={compactNumber} />
          <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={92} fontSize={11} />
          <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
          <Bar dataKey="amount" radius={[0, 4, 4, 0]} barSize={16}>
            {rows.map((row, index) => (
              <Cell key={row.key} fill={`var(--chart-${(index % 5) + 1})`} />
            ))}
          </Bar>
        </BarChart>
      </ChartContainer>
    </ChartCard>
  );
}

function WalletTypeBars({ items, currency }: { items: WalletTypeStat[]; currency?: string }) {
  const rows = useMemo(
    () =>
      [...items]
        .filter((item) => item.count > 0)
        // 金额取绝对值：出账是负数，不取绝对值条形会朝反方向长出去
        .map((item) => ({ key: item.type, name: walletTypeLabel(item.type), amount: Math.abs(Number(item.amount)), count: item.count }))
        .sort((a, b) => b.amount - a.amount),
    [items]
  );
  const config = useMemo(
    () => buildChartConfig(rows.map((row) => ({ key: row.key, label: row.name, color: WALLET_TYPE_COLORS[row.key] }))),
    [rows]
  );

  return (
    <ChartCard
      title="钱包流水分布"
      description={currency ? `金额取绝对值 · ${currency}` : "金额取绝对值"}
      empty={rows.length === 0}
      height={220}
    >
      <ChartContainer config={config} className="h-[220px] w-full">
        <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 12 }}>
          <CartesianGrid horizontal={false} strokeDasharray="3 3" />
          <XAxis type="number" tickLine={false} axisLine={false} fontSize={11} tickFormatter={compactNumber} />
          <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={78} fontSize={11} />
          <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
          <Bar dataKey="amount" radius={[0, 4, 4, 0]} barSize={16}>
            {rows.map((row) => (
              <Cell key={row.key} fill={`var(--color-${row.key})`} />
            ))}
          </Bar>
        </BarChart>
      </ChartContainer>
    </ChartCard>
  );
}

// ── 其它 ──

/** 坐标轴上的紧凑金额：`128000` → `12.8万`。轴上写全位数会把标签挤成一团。 */
function compactNumber(value: number) {
  if (!Number.isFinite(value)) return "0";
  const abs = Math.abs(value);
  if (abs >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (abs >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(Math.round(value));
}

function StatTile({
  icon,
  label,
  value,
  hint,
  tone = "neutral"
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  hint?: string;
  tone?: "neutral" | "positive" | "warning";
}) {
  const toneClass =
    tone === "positive" ? "text-emerald-600" : tone === "warning" ? "text-amber-600" : "text-foreground";
  return (
    <Card>
      <CardContent className="space-y-1.5 p-4">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {icon}
          {label}
        </div>
        <div className={`font-mono text-xl font-semibold tabular-nums ${toneClass}`}>{value}</div>
        {hint ? <p className="text-[11px] leading-relaxed text-muted-foreground">{hint}</p> : null}
      </CardContent>
    </Card>
  );
}

/**
 * 凭证能力自述。
 *
 * 放在概览里而不是藏进设置页：缺中日韩字体时中文凭证会静默降级成英文，
 * 而这件事只有在用户下载到一份看不懂的 PDF 之后才会被发现。
 */
function ReceiptCapabilityCard({
  supportsCJK,
  fontStatus,
  fontNotes,
  localeCount,
  defaultLocale
}: {
  supportsCJK: boolean;
  fontStatus: string;
  fontNotes?: string[];
  localeCount: number;
  defaultLocale: string;
}) {
  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-center justify-between gap-3">
          <h3 className="flex items-center gap-1.5 text-sm font-medium">
            <Receipt className="size-4" />
            凭证能力
          </h3>
          {supportsCJK ? (
            <Badge className="bg-emerald-500/15 text-emerald-600 hover:bg-emerald-500/15">中日韩可用</Badge>
          ) : (
            <Badge className="bg-amber-500/15 text-amber-600 hover:bg-amber-500/15">缺中日韩字体</Badge>
          )}
        </div>
        <dl className="grid gap-2 text-xs sm:grid-cols-3">
          <div>
            <dt className="text-muted-foreground">可选语言</dt>
            <dd className="font-mono">{localeCount} 种</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">默认语言</dt>
            <dd className="font-mono">{defaultLocale || "en"}</dd>
          </div>
          <div className="sm:col-span-1">
            <dt className="text-muted-foreground">字体</dt>
            <dd className="truncate font-mono" title={fontStatus}>
              {fontStatus}
            </dd>
          </div>
        </dl>
        {!supportsCJK ? (
          <div className="flex gap-2 rounded-lg bg-amber-500/10 p-2.5 text-[11px] leading-relaxed text-amber-700 dark:text-amber-400">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
            <p>
              当前环境没有中日韩字体，中文 / 日文 / 韩文凭证会降级成英文。
              配置 <code className="font-mono">PAYMENT_RECEIPT_FONT_PATH</code> 或在镜像里安装一份中文字体即可。
            </p>
          </div>
        ) : null}
        {fontNotes && fontNotes.length > 0 ? (
          <ul className="space-y-1 text-[11px] text-muted-foreground">
            {fontNotes.map((note) => (
              <li key={note}>· {note}</li>
            ))}
          </ul>
        ) : null}
      </CardContent>
    </Card>
  );
}
