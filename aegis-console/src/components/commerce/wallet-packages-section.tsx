"use client";

import { useMemo, useState } from "react";
import { ArrowDownUp, Eye, EyeOff, Gift, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useNow } from "@/components/users/ban-form";
import { RecordTable, Section, StateBlock, errorMessage, type Column } from "@/components/users/detail/kit";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { isPositiveDecimal } from "@/components/users/detail/user-assets-adjust";
import type { AdminWalletPackage, WalletPackagePayload } from "@/lib/api/types";
import {
  useDeleteWalletPackageMutation,
  useSaveWalletPackageMutation,
  useWalletPackagesQuery
} from "@/lib/commerce-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { formatMoney } from "./commerce-format";
import {
  WALLET_PACKAGE_STATE_LABEL,
  WalletPackageStatusBadge,
  walletPackageState,
  type WalletPackageState
} from "./wallet-config-fields";
import { WalletPackageEditor, type WalletPackageEditorTarget } from "./wallet-package-editor";
import { WalletPackageSortList } from "./wallet-package-sort-list";

const STATE_ORDER: WalletPackageState[] = ["on_sale", "not_started", "ended", "inactive"];

function payloadOf(item: AdminWalletPackage, patch: Partial<WalletPackagePayload> = {}): WalletPackagePayload {
  return {
    title: item.title,
    amount: item.amount,
    bonusAmount: item.bonusAmount,
    bonusIntegral: item.bonusIntegral,
    badge: item.badge ?? "",
    description: item.description ?? "",
    perUserLimit: item.perUserLimit,
    isActive: item.isActive,
    sortOrder: item.sortOrder,
    startsAt: item.startsAt ?? null,
    endsAt: item.endsAt ?? null,
    ...patch
  };
}

function saleWindow(item: AdminWalletPackage) {
  if (!item.startsAt && !item.endsAt) return "长期";
  if (item.startsAt && item.endsAt) return `${formatDateTimeMinute(item.startsAt)} 至 ${formatDateTimeMinute(item.endsAt)}`;
  return item.startsAt ? `${formatDateTimeMinute(item.startsAt)} 起` : `至 ${formatDateTimeMinute(item.endsAt)}`;
}

/**
 * 充值档位管理：列表带销售统计，新建与编辑走对话框，上下架即时生效，删除前确认，排序进入拖拽模式。
 * 结构与积分商城的商品管理一致（points-store-items-panel）。
 *
 * 后端没有单独的上下架接口，切换上下架是带着档位全部字段的一次 PUT。
 */
export function WalletPackagesSection({
  appKey,
  currency,
  canConfig
}: {
  appKey: string;
  currency: string;
  canConfig: boolean;
}) {
  const query = useWalletPackagesQuery(appKey);
  const saveMutation = useSaveWalletPackageMutation(appKey);
  const deleteMutation = useDeleteWalletPackageMutation(appKey);
  const now = useNow();
  const [editor, setEditor] = useState<WalletPackageEditorTarget | null>(null);
  const [sorting, setSorting] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<AdminWalletPackage | null>(null);
  const [togglingId, setTogglingId] = useState<number | null>(null);

  const items = useMemo(() => query.data ?? [], [query.data]);
  const counts = useMemo(() => {
    const result = Object.fromEntries(STATE_ORDER.map((key) => [key, 0])) as Record<WalletPackageState, number>;
    for (const item of items) result[walletPackageState(item, now)] += 1;
    return result;
  }, [items, now]);
  const nextSortOrder = items.reduce((max, item) => Math.max(max, item.sortOrder), 0) + 10;

  const toggle = async (item: AdminWalletPackage, active: boolean) => {
    setTogglingId(item.id);
    try {
      await saveMutation.mutateAsync({ packageId: item.id, payload: payloadOf(item, { isActive: active }) });
      toast.success(active ? `已上架「${item.title}」` : `已下架「${item.title}」`);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    } finally {
      setTogglingId(null);
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.id);
      toast.success(`已删除「${pendingDelete.title}」`);
      setPendingDelete(null);
    } catch (error) {
      toast.error(errorMessage(error, "删除失败"));
    }
  };

  if (query.isLoading) {
    return (
      <Section title="充值档位" icon={Gift} tone="violet">
        <div className="flex flex-col gap-3">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      </Section>
    );
  }

  if (sorting) {
    return <WalletPackageSortList appKey={appKey} items={items} currency={currency} onDone={() => setSorting(false)} />;
  }

  const columns: Array<Column<AdminWalletPackage>> = [
    {
      id: "title",
      header: "档位",
      cell: (row) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate font-medium">{row.title}</span>
            {row.badge ? (
              <span className="shrink-0 rounded bg-violet-500/10 px-1.5 text-[11px] leading-[18px] text-violet-600 dark:text-violet-400">
                {row.badge}
              </span>
            ) : null}
          </span>
          {row.description ? <span className="truncate text-xs text-muted-foreground">{row.description}</span> : null}
        </div>
      )
    },
    {
      id: "amount",
      header: "充值金额",
      align: "right",
      width: "130px",
      cell: (row) => <span className="font-medium tabular-nums">{formatMoney(row.amount, currency)}</span>
    },
    {
      id: "bonus",
      header: "赠送",
      width: "150px",
      cell: (row) => {
        const parts = [
          isPositiveDecimal(row.bonusAmount) ? `余额 ${formatMoney(row.bonusAmount)}` : "",
          row.bonusIntegral > 0 ? `积分 ${row.bonusIntegral.toLocaleString("zh-CN")}` : ""
        ].filter(Boolean);
        return parts.length ? (
          <div className="flex flex-col text-xs tabular-nums">
            {parts.map((part) => (
              <span key={part}>{part}</span>
            ))}
          </div>
        ) : (
          <span className="text-xs text-muted-foreground">无</span>
        );
      }
    },
    {
      id: "limit",
      header: "限购",
      width: "100px",
      hideBelow: "md",
      cell: (row) => (
        <span className="text-xs text-muted-foreground">{row.perUserLimit > 0 ? `每人 ${row.perUserLimit} 次` : "不限"}</span>
      )
    },
    {
      id: "window",
      header: "售卖时间",
      hideBelow: "xl",
      cell: (row) => <span className="text-xs text-muted-foreground tabular-nums">{saleWindow(row)}</span>
    },
    {
      id: "stat",
      header: "销售",
      align: "right",
      width: "140px",
      cell: (row) => (
        <div className="flex flex-col items-end text-xs tabular-nums">
          <span className="text-sm">{formatMoney(row.stat?.amount, currency)}</span>
          <span className="text-muted-foreground">
            {(row.stat?.orders ?? 0).toLocaleString("zh-CN")} 笔，{(row.stat?.users ?? 0).toLocaleString("zh-CN")} 人
          </span>
        </div>
      )
    },
    { id: "state", header: "状态", width: "80px", cell: (row) => <WalletPackageStatusBadge item={row} /> },
    ...(canConfig
      ? [
          {
            id: "actions",
            header: "",
            align: "right" as const,
            width: "96px",
            cell: (row: AdminWalletPackage) => (
              <div className="flex items-center justify-end gap-1" onClick={(event) => event.stopPropagation()}>
                <Switch
                  checked={row.isActive}
                  disabled={togglingId === row.id}
                  onCheckedChange={(next) => toggle(row, next)}
                  aria-label="上架"
                />
                <PackageMenu
                  item={row}
                  onEdit={() => setEditor({ mode: "edit", item: row })}
                  onToggle={() => toggle(row, !row.isActive)}
                  onDelete={() => setPendingDelete(row)}
                />
              </div>
            )
          }
        ]
      : [])
  ];

  const description = items.length
    ? `共 ${items.length} 个，${STATE_ORDER.filter((key) => counts[key])
        .map((key) => `${WALLET_PACKAGE_STATE_LABEL[key]} ${counts[key]}`)
        .join("，")}。销售按已付款订单统计`
    : "按固定金额充值，可附赠余额与积分";

  return (
    <>
      <Section
        flush
        title="充值档位"
        icon={Gift}
        tone="violet"
        description={description}
        actions={
          canConfig ? (
            <>
              <Button variant="outline" size="sm" disabled={items.length < 2} onClick={() => setSorting(true)}>
                <ArrowDownUp />
                调整顺序
              </Button>
              <Button size="sm" onClick={() => setEditor({ mode: "create", sortOrder: nextSortOrder })}>
                <Plus />
                新建档位
              </Button>
            </>
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={items}
          rowKey={(row) => row.id}
          fetching={query.isFetching}
          error={query.error}
          onRetry={() => query.refetch()}
          onRowClick={canConfig ? (row) => setEditor({ mode: "edit", item: row }) : undefined}
          minWidth={760}
          empty={
            <StateBlock
              icon={Gift}
              title="还没有充值档位"
              description="档位是固定金额的充值选项，可以附赠余额与积分，例如「充 100 送 20」。没有档位时用户只能按自定义金额充值。"
              action={
                canConfig ? (
                  <Button size="sm" onClick={() => setEditor({ mode: "create", sortOrder: nextSortOrder })}>
                    <Plus />
                    新建第一个档位
                  </Button>
                ) : null
              }
            />
          }
        />
      </Section>

      <WalletPackageEditor appKey={appKey} target={editor} currency={currency} onClose={() => setEditor(null)} />

      <ConfirmActionDialog
        open={Boolean(pendingDelete)}
        onOpenChange={(open) => (!open && !deleteMutation.isPending ? setPendingDelete(null) : undefined)}
        title={`删除「${pendingDelete?.title ?? ""}」？`}
        description={
          pendingDelete && pendingDelete.stat?.orders
            ? `该档位已售 ${pendingDelete.stat.orders.toLocaleString("zh-CN")} 笔。删除后用户端立即不可见，已下的订单按下单时的快照履约，不受影响。只想暂时不卖可以改为下架。`
            : "删除后用户端立即不可见，已下的订单按下单时的快照履约，不受影响。只想暂时不卖可以改为下架。"
        }
        confirmLabel="确认删除"
        destructive
        pending={deleteMutation.isPending}
        onConfirm={confirmDelete}
      />
    </>
  );
}

function PackageMenu({
  item,
  onEdit,
  onToggle,
  onDelete
}: {
  item: AdminWalletPackage;
  onEdit: () => void;
  onToggle: () => void;
  onDelete: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="更多操作" onClick={(event) => event.stopPropagation()}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-36" onClick={(event) => event.stopPropagation()}>
        <DropdownMenuItem onSelect={onEdit}>
          <Pencil />
          编辑
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onToggle}>
          {item.isActive ? <EyeOff /> : <Eye />}
          {item.isActive ? "下架" : "上架"}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={onDelete}>
          <Trash2 />
          删除
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
