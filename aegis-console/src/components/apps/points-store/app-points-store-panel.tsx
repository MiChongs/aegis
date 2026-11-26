"use client";

import { useState } from "react";
import { BarChart3, Gift, Loader2, ReceiptText, ShieldCheck, ShoppingBag, Store } from "lucide-react";
import { toast } from "sonner";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { PointsStoreGiftPolicyPanel } from "@/components/apps/points-store/points-store-gift-policy-panel";
import { PointsStoreGiftsPanel } from "@/components/apps/points-store/points-store-gifts-panel";
import { PointsStoreItemsPanel } from "@/components/apps/points-store/points-store-items-panel";
import { PointsStoreOverviewPanel } from "@/components/apps/points-store/points-store-overview-panel";
import { PointsStoreRedemptionsPanel } from "@/components/apps/points-store/points-store-redemptions-panel";
import { formatPoints } from "@/components/apps/points-store/points-store-shared";
import { ToneIcon, errorMessage } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { PointsStoreRedemptionStatus } from "@/lib/api/points-store";
import { formatDateTime } from "@/lib/datetime";
import {
  usePointsStoreGiftsQuery,
  usePointsStoreItemsQuery,
  usePointsStoreSettingsQuery,
  usePointsStoreStatsQuery,
  useSavePointsStoreSettingsMutation
} from "@/lib/points-store-hooks";

/**
 * 积分商城区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 每天兑多少、花掉多少积分、哪些商品最受欢迎、还有几笔等人处理 |
 * | 商品 | 卖什么、多少积分、限量与限兑、卖得怎么样；上下架、排序、复制、批量处理 |
 * | 兑换记录 | 谁在什么时候兑了什么、实际发出去了什么；人工发放在这里处理 |
 * | 赠送 | 谁送了谁什么、风控为什么拦或送审；审核队列与撤销追回在这里处理 |
 * | 赠送风控 | 新号、同设备、同网络、频次与额度等判据的开关、阈值与处置 |
 *
 * 顶部状态条常驻：商城开关与两项最要紧的数（在售、待发放），换页签时不消失。
 * 视图状态不进 URL，理由同卡密区块：`?tab=` 已经被应用区块占用。
 * 用户的功能解锁在用户详情的「资产」页签里看与改，不在这里另开入口。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "items", label: "商品", icon: ShoppingBag },
  { key: "redemptions", label: "兑换记录", icon: ReceiptText },
  { key: "gifts", label: "赠送", icon: Gift },
  { key: "giftPolicy", label: "赠送风控", icon: ShieldCheck }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppPointsStorePanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  // 从商品或概览跳到兑换记录时带过去的筛选条件
  const [itemFilter, setItemFilter] = useState<number | undefined>(undefined);
  const [statusFilter, setStatusFilter] = useState<PointsStoreRedemptionStatus | undefined>(undefined);

  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const statsQuery = usePointsStoreStatsQuery(appKey, 14);
  // 审核队列的笔数挂在「赠送」页签上：只取一条，读 summary
  const giftsQuery = usePointsStoreGiftsQuery(appKey, { status: "reviewing", limit: 1 });
  const reviewing = giftsQuery.data?.summary.reviewing ?? 0;

  if (!appKey) {
    return <NoAppSelected icon={<ShoppingBag className="size-5" />} />;
  }

  const openRedemptions = (itemId?: number, status?: PointsStoreRedemptionStatus) => {
    setItemFilter(itemId);
    setStatusFilter(status);
    setView("redemptions");
  };

  const itemCount = itemsQuery.data?.length;
  const pending = statsQuery.data?.summary.pending ?? 0;

  return (
    <div className="flex flex-col gap-4">
      <StoreStatusBar
        appKey={appKey}
        activeItems={statsQuery.data?.summary.activeItems}
        pending={statsQuery.data ? Math.max(0, pending - reviewing) : undefined}
        onOpenItems={() => setView("items")}
        onOpenPending={() => openRedemptions(undefined, "pending")}
      />

      <Tabs value={view} onValueChange={(value) => setView(value as ViewKey)} className="gap-4">
        <div className="-mx-1 overflow-x-auto px-1">
          <TabsList className="h-10">
            {VIEWS.map((item) => (
              <TabsTrigger key={item.key} value={item.key} className="gap-1.5 px-3">
                <item.icon />
                {item.label}
                {item.key === "items" && itemCount !== undefined ? (
                  <span className="text-xs font-normal tabular-nums text-muted-foreground">{itemCount}</span>
                ) : null}
                {item.key === "redemptions" && pending - reviewing > 0 ? (
                  <Badge variant="warning" size="sm" className="tabular-nums">
                    {formatPoints(pending - reviewing)}
                  </Badge>
                ) : null}
                {item.key === "gifts" && reviewing > 0 ? (
                  <Badge variant="warning" size="sm" className="tabular-nums">
                    {formatPoints(reviewing)}
                  </Badge>
                ) : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>

      {view === "overview" ? (
        <PointsStoreOverviewPanel
          appKey={appKey}
          onOpenItems={() => setView("items")}
          onOpenRedemptions={openRedemptions}
        />
      ) : null}
      {view === "items" ? (
        <PointsStoreItemsPanel
          appKey={appKey}
          onOpenRedemptions={(itemId, pendingOnly) => openRedemptions(itemId, pendingOnly ? "pending" : undefined)}
        />
      ) : null}
      {view === "redemptions" ? (
        <PointsStoreRedemptionsPanel
          appKey={appKey}
          itemId={itemFilter}
          status={statusFilter}
          onItemIdChange={setItemFilter}
          onStatusChange={setStatusFilter}
          onOpenGifts={() => setView("gifts")}
        />
      ) : null}
      {view === "gifts" ? <PointsStoreGiftsPanel appKey={appKey} /> : null}
      {view === "giftPolicy" ? <PointsStoreGiftPolicyPanel appKey={appKey} /> : null}
    </div>
  );
}

/** 商城开关 + 在售 / 待发放。关闭时整条换成提醒色，用户端此时只显示余额与赚积分。 */
function StoreStatusBar({
  appKey,
  activeItems,
  pending,
  onOpenItems,
  onOpenPending
}: {
  appKey: string;
  activeItems?: number;
  pending?: number;
  onOpenItems: () => void;
  onOpenPending: () => void;
}) {
  const settingsQuery = usePointsStoreSettingsQuery(appKey);
  const saveMutation = useSavePointsStoreSettingsMutation(appKey);
  const settings = settingsQuery.data;
  const enabled = Boolean(settings?.enabled);

  const toggle = async (next: boolean) => {
    try {
      await saveMutation.mutateAsync(next);
      toast.success(next ? "积分商城已对用户开放" : "积分商城已关闭");
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  if (settingsQuery.isLoading) {
    return <Skeleton className="h-[74px] w-full rounded-xl" />;
  }

  return (
    <Card className="flex flex-col gap-0 py-0 sm:flex-row sm:items-stretch">
      <div className="flex min-w-0 flex-1 items-center gap-3 px-4 py-3.5 sm:px-5">
        <ToneIcon icon={Store} tone={enabled ? "success" : "warning"} size="lg" />
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold">{enabled ? "商城开放中" : "商城未开放"}</span>
            <span className={enabled ? "size-1.5 rounded-full bg-emerald-500" : "size-1.5 rounded-full bg-amber-500"} />
          </div>
          <p className="truncate text-xs text-muted-foreground">
            {enabled
              ? settings?.updatedAt
                ? `用户可以在 App 里兑换商品，${formatDateTime(settings.updatedAt)} 由${settings.updatedBy || "管理员"}开启`
                : "用户可以在 App 里用积分兑换商品"
              : "用户端只显示积分余额与赚积分，兑换区显示暂未开放"}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-3 border-t sm:flex sm:border-t-0 sm:border-l">
        <StatusFact label="在售商品" value={activeItems} onClick={onOpenItems} />
        <StatusFact label="待发放" value={pending} tone={pending ? "warning" : undefined} onClick={onOpenPending} />
        <label className="flex cursor-pointer flex-col justify-center gap-1.5 px-4 py-3 sm:min-w-[120px] sm:px-5">
          <span className="text-xs text-muted-foreground">对用户开放</span>
          <span className="flex items-center gap-2">
            <Switch checked={enabled} onCheckedChange={toggle} disabled={saveMutation.isPending} aria-label="对用户开放积分商城" />
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
          </span>
        </label>
      </div>
    </Card>
  );
}

function StatusFact({
  label,
  value,
  tone,
  onClick
}: {
  label: string;
  value?: number;
  tone?: "warning";
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex flex-col justify-center gap-0.5 border-r px-4 py-3 text-left transition-colors hover:bg-muted/40 sm:min-w-[112px] sm:px-5"
    >
      <span className="text-xs text-muted-foreground">{label}</span>
      {value === undefined ? (
        <Skeleton className="h-6 w-10" />
      ) : (
        <span
          className={
            tone === "warning"
              ? "text-lg font-semibold tabular-nums text-amber-600 dark:text-amber-400"
              : "text-lg font-semibold tabular-nums"
          }
        >
          {formatPoints(value)}
        </span>
      )}
    </button>
  );
}
