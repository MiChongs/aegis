"use client";

import { AlertCircle, Store } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { CardEmpty } from "@/components/overview/overview-kit";
import { useCommerceOverviewQuery, type CommerceWindow } from "@/lib/commerce-hooks";
import { ChannelCard } from "./overview/channel-card";
import { MoneyFlowCard } from "./overview/money-flow-card";
import { OrderHealthCard } from "./overview/order-health-card";
import { ReceiptCard } from "./overview/receipt-card";
import { TrendCard } from "./overview/trend-card";
import { WalletPoolCard } from "./overview/wallet-pool-card";

/**
 * 交易概览。
 *
 * 三口径同屏：订单（收了多少）、退款（退回去多少）、钱包（平台还欠用户多少）。
 * 这三个数来自三张表，一次取齐是刻意的 —— 分开拉会出现「订单已刷新、
 * 退款还是上一个时间窗」这种自相矛盾的画面，而人会照着它做决定。
 *
 * 版面按「钱从哪来 → 怎么走 → 留在哪」排：
 *   第一行 资金流向瀑布（已支付 → 退款 → 实收）与订单健康环；
 *   第二行 整宽的交易趋势，进项朝上、出项朝下；
 *   第三行 钱包资金池（时点余额 + 按类型的进出），右侧是渠道排行与凭证能力。
 * 换时间窗时沿用上一份数据、各卡右上角转圈，不整页闪回骨架。
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
  const data = query.data;

  if (!appKey) {
    return (
      <Card className="py-2">
        <CardEmpty icon={Store} title="未选择应用" description="在上方选择一个应用查看交易概览" />
      </Card>
    );
  }
  if (query.isLoading) {
    return <OverviewSkeleton />;
  }
  if (query.isError || !data) {
    return (
      <Card className="items-center gap-3 py-8">
        <CardEmpty icon={AlertCircle} title="交易概览加载失败" description={query.error instanceof Error ? query.error.message : undefined} />
        <Button size="sm" variant="outline" onClick={() => void query.refetch()}>
          重试
        </Button>
      </Card>
    );
  }

  const { orders, wallet, trend, receipt } = data;
  const refreshing = query.isPlaceholderData || (query.isFetching && !query.isLoading);

  return (
    <div className="grid min-w-0 gap-4 lg:grid-cols-3 lg:gap-5">
      <MoneyFlowCard orders={orders} refreshing={refreshing} className="lg:col-span-2" />
      <OrderHealthCard orders={orders} refreshing={refreshing} />
      <TrendCard trend={trend} walletCurrency={walletCurrency} refreshing={refreshing} className="lg:col-span-3" />
      <WalletPoolCard wallet={wallet} currency={walletCurrency} refreshing={refreshing} className="lg:col-span-2" />
      <div className="flex min-w-0 flex-col gap-4 lg:gap-5">
        <ChannelCard items={orders.byMethod} appKey={appKey} refreshing={refreshing} />
        <ReceiptCard receipt={receipt} className="flex-1" />
      </div>
    </div>
  );
}

/** 骨架与真实版面同栅格，加载完不跳动 */
function OverviewSkeleton() {
  return (
    <div className="grid min-w-0 gap-4 lg:grid-cols-3 lg:gap-5">
      <Skeleton className="h-[340px] rounded-xl lg:col-span-2" />
      <Skeleton className="h-[340px] rounded-xl" />
      <Skeleton className="h-[420px] rounded-xl lg:col-span-3" />
      <Skeleton className="h-[360px] rounded-xl lg:col-span-2" />
      <div className="flex flex-col gap-4 lg:gap-5">
        <Skeleton className="h-[170px] rounded-xl" />
        <Skeleton className="h-[170px] rounded-xl" />
      </div>
    </div>
  );
}
