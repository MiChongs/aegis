"use client";

import { useMemo, useState } from "react";
import {
  ArrowUpRight,
  BarChart3,
  Eye,
  EyeOff,
  Loader2,
  Package,
  Pencil,
  Plus,
  ReceiptText,
  Store,
  Trash2
} from "lucide-react";
import { toast } from "sonner";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { formatYuan, useNow } from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { CommerceBoard } from "@/components/apps/ai-gateway/ai-gateway-commerce-board";
import type { TierOption } from "@/components/apps/ai-gateway/ai-gateway-member-tiers";
import {
  GUEST,
  PackageCard,
  quoteIdentities,
  saleState
} from "@/components/apps/ai-gateway/ai-gateway-package-card";
import { PackageOrdersView } from "@/components/apps/ai-gateway/ai-gateway-package-orders";
import { AIGatewayPackageSheet, type PackageSheetTarget } from "@/components/apps/ai-gateway/ai-gateway-package-sheet";
import { StatTile } from "@/components/content/content-shared";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useAIGatewayCatalogQuery,
  useAIGatewayPackagesQuery,
  useAIGatewaySettingsQuery,
  useDeleteAIGatewayPackageMutation,
  useSaveAIGatewayPackageMutation
} from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayPackage, AIGatewayPackagePayload } from "@/lib/api/ai-gateway";
import { useAdminVipTiersQuery } from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";

const TABS = [
  { key: "packages", label: "额度包", icon: Package },
  { key: "orders", label: "订单", icon: ReceiptText },
  { key: "commerce", label: "生意", icon: BarChart3 }
] as const;

type TabKey = (typeof TABS)[number]["key"];

/** 管理端额度包 → 保存用的载荷（去掉统计与时间戳）。 */
function payloadOf(pkg: AIGatewayPackage): AIGatewayPackagePayload {
  return {
    name: pkg.name,
    subtitle: pkg.subtitle ?? "",
    description: pkg.description ?? "",
    badge: pkg.badge ?? "",
    tone: pkg.tone,
    featured: pkg.featured,
    points: pkg.points,
    bonusPoints: pkg.bonusPoints,
    price: pkg.price,
    originalPrice: pkg.originalPrice,
    validityDays: pkg.validityDays,
    audience: pkg.audience,
    tierIds: pkg.tierIds ?? [],
    memberDiscount: pkg.memberDiscount,
    perUserLimit: pkg.perUserLimit,
    monthlyLimit: pkg.monthlyLimit,
    stock: pkg.stock,
    startsAt: pkg.startsAt ?? null,
    endsAt: pkg.endsAt ?? null,
    enabled: pkg.enabled,
    sortOrder: pkg.sortOrder ?? 0
  };
}

/**
 * 额度商店（docs/ai-billing.md「额度包」）：额度包按用户看到的样子排成卡片网格（可切换身份看会员价），
 * 订单可筛选、按剩余比例退款，生意看板看营收、负债与到期。
 */
export function AIGatewayStorePanel({ appKey, onOpenSettings }: { appKey: string; onOpenSettings: () => void }) {
  const [tab, setTab] = useState<TabKey>("packages");
  const catalogQuery = useAIGatewayCatalogQuery(appKey);
  const settingsQuery = useAIGatewaySettingsQuery(appKey);
  const packagesQuery = useAIGatewayPackagesQuery(appKey);
  const catalog = catalogQuery.data;
  const vipTiersQuery = useAdminVipTiersQuery(catalog && !catalog.vipTiers ? appKey : null);
  const settings = settingsQuery.data;
  const packages = useMemo(
    () => [...(packagesQuery.data?.items ?? [])].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.id - b.id),
    [packagesQuery.data]
  );
  const tiers = useMemo<TierOption[]>(
    () => catalog?.vipTiers ?? (vipTiersQuery.data ?? []).map((tier) => ({ id: tier.id, name: tier.name })),
    [catalog?.vipTiers, vipTiersQuery.data]
  );
  const tierName = (id: number) => tiers.find((tier) => tier.id === id)?.name ?? `档位 #${id}`;
  const identities = quoteIdentities(settings, tierName);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3">
        <div className="flex gap-1 rounded-xl bg-muted/60 p-1">
          {TABS.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setTab(item.key)}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                tab === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
            </button>
          ))}
        </div>
        <StatusDot active={Boolean(settings?.store?.enabled)} labelActive="商店营业中" labelInactive="商店未开放，用户看不到额度包" />
        <Button size="xs" variant="ghost" className="ml-auto" onClick={onOpenSettings}>
          <Store className="size-3" />
          商店设置
          <ArrowUpRight className="size-3" />
        </Button>
      </div>

      {tab === "packages" ? (
        <PackagesView
          appKey={appKey}
          packages={packages}
          loading={packagesQuery.isLoading}
          tiers={tiers}
          identities={identities}
          tierName={tierName}
          notice={settings?.store?.notice ?? ""}
        />
      ) : null}
      {tab === "orders" ? <PackageOrdersView appKey={appKey} packages={packages} /> : null}
      {tab === "commerce" ? <CommerceBoard appKey={appKey} /> : null}
    </div>
  );
}

function PackagesView({
  appKey,
  packages,
  loading,
  tiers,
  identities,
  tierName,
  notice
}: {
  appKey: string;
  packages: AIGatewayPackage[];
  loading: boolean;
  tiers: TierOption[];
  identities: ReturnType<typeof quoteIdentities>;
  tierName: (id: number) => string;
  notice: string;
}) {
  const now = useNow(60_000);
  const [identityKey, setIdentityKey] = useState(GUEST.key);
  const [target, setTarget] = useState<PackageSheetTarget | null>(null);
  const [deleting, setDeleting] = useState<AIGatewayPackage | null>(null);
  const saveMutation = useSaveAIGatewayPackageMutation(appKey);
  const deleteMutation = useDeleteAIGatewayPackageMutation(appKey);
  const identity = identities.find((item) => item.key === identityKey) ?? GUEST;

  const live = packages.filter((pkg) => saleState(pkg, now).key === "live").length;
  const orders = packages.reduce((sum, pkg) => sum + (pkg.orders ?? 0), 0);
  const buyers = packages.reduce((sum, pkg) => sum + (pkg.buyers ?? 0), 0);
  const revenue = packages.reduce((sum, pkg) => sum + (Number(pkg.revenue) || 0), 0);
  const nextSort = packages.reduce((max, pkg) => Math.max(max, pkg.sortOrder ?? 0), 0) + 10;

  const setEnabled = async (pkg: AIGatewayPackage, enabled: boolean) => {
    try {
      await saveMutation.mutateAsync({ id: pkg.id, payload: { ...payloadOf(pkg), enabled } });
      toast.success(enabled ? `「${pkg.name}」已上架` : `「${pkg.name}」已下架`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const remove = async (pkg: AIGatewayPackage) => {
    try {
      await deleteMutation.mutateAsync(pkg.id);
      toast.success(`「${pkg.name}」已删除`);
      setDeleting(null);
    } catch (error) {
      setDeleting(null);
      if (error instanceof ApiError && error.code === 40234) {
        toast.error(`「${pkg.name}」已有订单，不能删除`, {
          description: "下架后用户就看不到它，已买的批次不受影响",
          action: pkg.enabled ? { label: "改为下架", onClick: () => void setEnabled(pkg, false) } : undefined
        });
        return;
      }
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  if (loading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-80 rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="在售" icon={Package} value={`${live} / ${packages.length}`} hint="此刻用户能买到的" />
        <StatTile label="累计订单" icon={ReceiptText} value={orders} />
        <StatTile label="累计买家" value={buyers} hint="按包分别计，同一人买两种算两次" />
        <StatTile label="累计营收" tone="positive" value={formatYuan(revenue)} />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted-foreground">按身份预览</span>
        <div className="flex flex-wrap gap-1">
          {identities.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setIdentityKey(item.key)}
              className={cn(
                "h-7 rounded-full border px-2.5 text-[11px] transition-colors",
                identityKey === item.key ? "border-foreground/30 bg-foreground text-background" : "hover:bg-muted"
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
        <Button size="sm" className="ml-auto" onClick={() => setTarget({ mode: "create", sortOrder: nextSort })}>
          <Plus className="size-3.5" />
          新建额度包
        </Button>
      </div>

      {packages.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-2xl border border-dashed py-14 text-center">
          <Package className="size-8 text-muted-foreground/40" />
          <div className="space-y-1">
            <p className="text-sm font-medium">还没有额度包</p>
            <p className="text-xs text-muted-foreground">用户用钱包余额买额度包，得到一个限期（或永久）的额度批次</p>
          </div>
          <Button size="sm" onClick={() => setTarget({ mode: "create", sortOrder: 10 })}>
            <Plus className="size-3.5" />
            新建第一个
          </Button>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {packages.map((pkg) => (
            <PackageCard
              key={pkg.id}
              pkg={pkg}
              identity={identity}
              tierName={tierName}
              now={now}
              footer={
                <div className="flex items-center gap-2">
                  <div className="flex min-w-0 flex-1 gap-3 text-[11px] text-muted-foreground tabular-nums">
                    <span>{pkg.orders ?? 0} 单</span>
                    <span>{pkg.buyers ?? 0} 人</span>
                    <span className="font-mono text-foreground">{formatYuan(pkg.revenue ?? 0)}</span>
                  </div>
                  <Button size="icon" variant="ghost" className="size-7" aria-label="编辑" onClick={() => setTarget({ mode: "edit", pkg })}>
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-7"
                    aria-label={pkg.enabled ? "下架" : "上架"}
                    title={pkg.enabled ? "下架" : "上架"}
                    disabled={saveMutation.isPending}
                    onClick={() => void setEnabled(pkg, !pkg.enabled)}
                  >
                    {pkg.enabled ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-7 text-muted-foreground hover:text-destructive"
                    aria-label="删除"
                    onClick={() => setDeleting(pkg)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              }
            />
          ))}
        </div>
      )}

      {notice ? (
        <div className="rounded-xl border bg-muted/30 px-4 py-3">
          <div className="mb-1 text-[11px] font-medium text-muted-foreground">购买须知（用户可见）</div>
          <p className="whitespace-pre-wrap text-xs leading-relaxed">{notice}</p>
        </div>
      ) : null}

      <AIGatewayPackageSheet
        appKey={appKey}
        target={target}
        tiers={tiers}
        identities={identities}
        tierName={tierName}
        onClose={() => setTarget(null)}
      />

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{deleting?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              只有没有订单的额度包能删除；已有订单的请改为下架。
              {(deleting?.orders ?? 0) > 0 ? `这个包已有 ${deleting?.orders} 笔订单，删除会被拒绝。` : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            {deleting && (deleting.orders ?? 0) > 0 && deleting.enabled ? (
              <AlertDialogAction
                onClick={(event) => {
                  event.preventDefault();
                  const pkg = deleting;
                  setDeleting(null);
                  void setEnabled(pkg, false);
                }}
              >
                改为下架
              </AlertDialogAction>
            ) : (
              <AlertDialogAction
                className="bg-destructive text-white hover:bg-destructive/90"
                disabled={deleteMutation.isPending}
                onClick={(event) => {
                  event.preventDefault();
                  if (deleting) void remove(deleting);
                }}
              >
                {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                删除
              </AlertDialogAction>
            )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
