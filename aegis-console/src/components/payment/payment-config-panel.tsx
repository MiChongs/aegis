"use client";

import { useCallback, useMemo, useState } from "react";
import { Plus, RefreshCw, Search, Star } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { PaymentProviderMeta } from "@/lib/api/types";
import {
  useAdminPaymentConfigsQuery,
  useAdminPaymentMethodsQuery,
  useCreateAdminPaymentConfigMutation,
  useUpdateAdminPaymentConfigMutation,
  useDeleteAdminPaymentConfigMutation,
  useTestAdminPaymentConfigMutation
} from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PaymentConfigForm } from "./payment-config-form";
import { PaymentOrdersPanel } from "./payment-orders-panel";
import { PaymentRefundsPanel } from "./payment-refunds-panel";
import { VipPlansPanel } from "./vip-plans-panel";
import { PaymentBrandBadge } from "./payment-brand-icon";
import { PaymentCapabilityBadges } from "./provider-fields";

type PaymentConfig = Record<string, unknown>;

// PaymentConfigPanel 支付管理总面板：渠道配置 / 交易订单 / VIP 套餐
export function PaymentConfigPanel({ appId }: { appId?: number | null }) {
  return (
    <Tabs defaultValue="channels" className="space-y-4">
      <TabsList className="h-8">
        <TabsTrigger value="channels" className="text-xs">渠道配置</TabsTrigger>
        <TabsTrigger value="orders" className="text-xs">交易订单</TabsTrigger>
        <TabsTrigger value="refunds" className="text-xs">退款单</TabsTrigger>
        <TabsTrigger value="vip" className="text-xs">VIP 套餐</TabsTrigger>
      </TabsList>
      <TabsContent value="channels">
        <PaymentChannelsPanel appId={appId} />
      </TabsContent>
      <TabsContent value="orders">
        <PaymentOrdersPanel appId={appId} />
      </TabsContent>
      <TabsContent value="refunds">
        <PaymentRefundsPanel appId={appId} />
      </TabsContent>
      <TabsContent value="vip">
        <VipPlansPanel appId={appId} />
      </TabsContent>
    </Tabs>
  );
}

function PaymentChannelsPanel({ appId }: { appId?: number | null }) {
  const configsQuery = useAdminPaymentConfigsQuery(appId);
  const methodsQuery = useAdminPaymentMethodsQuery();
  const createMutation = useCreateAdminPaymentConfigMutation();
  const updateMutation = useUpdateAdminPaymentConfigMutation();
  const deleteMutation = useDeleteAdminPaymentConfigMutation();
  const testMutation = useTestAdminPaymentConfigMutation();

  const [editConfig, setEditConfig] = useState<PaymentConfig | null>(null);
  const [createMethod, setCreateMethod] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [catalogOpen, setCatalogOpen] = useState(false);
  // 每次打开表单都自增，作为表单组件的 key：
  // 表单据此重新挂载并按最新 props 初始化，无需在其内部用 effect 同步 props → state
  const [formSession, setFormSession] = useState(0);

  const configs = useMemo(() => configsQuery.data ?? [], [configsQuery.data]);
  const methods = useMemo(() => methodsQuery.data ?? [], [methodsQuery.data]);
  const metaByMethod = useMemo(
    () => new Map(methods.map((m) => [m.method, m] as const)),
    [methods]
  );

  const handleEdit = useCallback((cfg: PaymentConfig) => {
    setCreateMethod(null);
    setEditConfig(cfg);
    setFormSession((s) => s + 1);
    setFormOpen(true);
  }, []);

  const handlePickMethod = useCallback((method: string) => {
    setEditConfig(null);
    setCreateMethod(method);
    setCatalogOpen(false);
    setFormSession((s) => s + 1);
    setFormOpen(true);
  }, []);

  async function handleSave(payload: Record<string, unknown>) {
    if (payload.config_id) {
      await updateMutation.mutateAsync(payload);
    } else {
      await createMutation.mutateAsync(payload);
    }
  }

  async function handleDelete(id: number) {
    if (!appId) return;
    await deleteMutation.mutateAsync({ appid: appId, config_id: id });
  }

  async function handleTest(id: number) {
    if (!appId) return undefined;
    return testMutation.mutateAsync({ appid: appId, config_id: id });
  }

  if (configsQuery.isLoading) {
    return (
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-28 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs tabular-nums text-muted-foreground">
          已接入 {configs.length} 个渠道配置 · 平台支持 {methods.length} 种支付方式
        </span>
        <div className="flex gap-1.5">
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => void configsQuery.refetch()}>
            <RefreshCw className="size-3" />刷新
          </Button>
          <Button size="sm" className="h-7 gap-1 text-xs" onClick={() => setCatalogOpen(true)}>
            <Plus className="size-3" />接入渠道
          </Button>
        </div>
      </div>

      {configs.length === 0 ? (
        <button
          type="button"
          onClick={() => setCatalogOpen(true)}
          className="flex w-full flex-col items-center gap-2 rounded-xl border border-dashed py-12 text-center transition-colors hover:border-foreground/30 hover:bg-muted/40"
        >
          <Plus className="size-5 text-muted-foreground" />
          <span className="text-sm font-medium">尚未接入任何支付渠道</span>
          <span className="text-xs text-muted-foreground">从 {methods.length} 种支持的支付方式中选择一个开始</span>
        </button>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {configs.map((cfg) => (
            <ConfiguredChannelCard
              key={Number(cfg.id)}
              config={cfg as PaymentConfig}
              meta={metaByMethod.get(String(cfg.payment_method))}
              onClick={() => handleEdit(cfg as PaymentConfig)}
            />
          ))}
        </div>
      )}

      <ChannelCatalogDialog
        open={catalogOpen}
        onOpenChange={setCatalogOpen}
        methods={methods}
        loading={methodsQuery.isLoading}
        configuredCounts={configs.reduce<Record<string, number>>((acc, cfg) => {
          const key = String(cfg.payment_method);
          acc[key] = (acc[key] ?? 0) + 1;
          return acc;
        }, {})}
        onPick={handlePickMethod}
      />

      <PaymentConfigForm
        key={formSession}
        open={formOpen}
        onOpenChange={setFormOpen}
        editConfig={editConfig}
        createMethod={createMethod}
        methods={methods}
        appId={appId}
        onSave={handleSave}
        onDelete={editConfig ? () => handleDelete(Number(editConfig.id)) : undefined}
        onTest={
          editConfig
            ? async () => {
                try {
                  return await handleTest(Number(editConfig.id));
                } catch (err) {
                  toast.error(err instanceof ApiError ? err.message : "连通测试失败");
                  throw err;
                }
              }
            : undefined
        }
        isSaving={createMutation.isPending || updateMutation.isPending}
        isDeleting={deleteMutation.isPending}
        isTesting={testMutation.isPending}
      />
    </div>
  );
}

/** 已接入渠道卡片 */
function ConfiguredChannelCard({
  config,
  meta,
  onClick
}: {
  config: PaymentConfig;
  meta?: PaymentProviderMeta;
  onClick: () => void;
}) {
  const enabled = config.enabled !== false;
  const method = String(config.payment_method ?? "");
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex flex-col gap-2.5 rounded-xl border p-3 text-left transition-colors hover:border-foreground/25 hover:bg-muted/40"
    >
      <div className="flex items-start gap-2.5">
        <PaymentBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name ?? method} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm font-medium">{String(config.config_name || "—")}</span>
            {config.is_default ? <Star className="size-3 shrink-0 fill-amber-400 text-amber-400" /> : null}
          </div>
          <span className="block truncate text-[11px] text-muted-foreground">{meta?.name ?? method}</span>
        </div>
        <span className="flex shrink-0 items-center gap-1.5 text-[11px]">
          <span className={`inline-block size-1.5 rounded-full ${enabled ? "bg-emerald-500" : "bg-zinc-300 dark:bg-zinc-600"}`} />
          {enabled ? "启用" : "停用"}
        </span>
      </div>
      {config.description ? (
        <p className="line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">{String(config.description)}</p>
      ) : null}
      {meta && <PaymentCapabilityBadges meta={meta} />}
    </button>
  );
}

/** 渠道目录：按分组展示全部可接入的支付方式 */
function ChannelCatalogDialog({
  open,
  onOpenChange,
  methods,
  loading,
  configuredCounts,
  onPick
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  methods: PaymentProviderMeta[];
  loading: boolean;
  configuredCounts: Record<string, number>;
  onPick: (method: string) => void;
}) {
  const [keyword, setKeyword] = useState("");

  const groups = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    const matched = kw
      ? methods.filter((m) =>
          [m.name, m.method, m.description, m.categoryName, ...(m.regions ?? []), ...(m.currencies ?? [])]
            .filter(Boolean)
            .some((v) => String(v).toLowerCase().includes(kw))
        )
      : methods;
    // 保持后端下发的顺序（已按分组归并），仅在此处切分成组
    const ordered: Array<{ category: string; items: PaymentProviderMeta[] }> = [];
    for (const item of matched) {
      const category = item.categoryName || "其他";
      const last = ordered.find((g) => g.category === category);
      if (last) last.items.push(item);
      else ordered.push({ category, items: [item] });
    }
    return ordered;
  }, [methods, keyword]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] w-[95vw] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-sm">选择要接入的支付渠道</DialogTitle>
        </DialogHeader>

        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-sm"
            placeholder="搜索渠道名称、地区或币种…"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
          />
        </div>

        {loading ? (
          <div className="grid gap-2 sm:grid-cols-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-20 w-full rounded-xl" />
            ))}
          </div>
        ) : groups.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">没有匹配的支付渠道</p>
        ) : (
          <div className="space-y-4">
            {groups.map((group) => (
              <div key={group.category} className="space-y-2">
                <h5 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                  {group.category}
                </h5>
                <div className="grid gap-2 sm:grid-cols-2">
                  {group.items.map((meta) => (
                    <CatalogCard
                      key={meta.method}
                      meta={meta}
                      configuredCount={configuredCounts[meta.method] ?? 0}
                      onPick={() => onPick(meta.method)}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function CatalogCard({
  meta,
  configuredCount,
  onPick
}: {
  meta: PaymentProviderMeta;
  configuredCount: number;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onPick}
      className="flex items-start gap-2.5 rounded-xl border p-3 text-left transition-colors hover:border-foreground/25 hover:bg-muted/40"
    >
      <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-medium">{meta.name}</span>
          {configuredCount > 0 && (
            <Badge variant="secondary" size="sm" className="shrink-0 font-normal">
              已接入 {configuredCount}
            </Badge>
          )}
        </div>
        {meta.description && (
          <p className="line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">{meta.description}</p>
        )}
        {!!meta.regions?.length && (
          <p className="truncate text-[10px] text-muted-foreground/80">覆盖：{meta.regions.join(" / ")}</p>
        )}
      </div>
    </button>
  );
}
