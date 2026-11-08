"use client";

import { useMemo, useState } from "react";
import { Copy, Download, Mail, MoreHorizontal, ReceiptText, Undo2 } from "lucide-react";
import { toast } from "sonner";
import type { AdminPaymentOrder } from "@/lib/api-client";
import { useAdminPaymentOrdersQuery } from "@/lib/admin-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { formatMoney } from "@/components/commerce/commerce-format";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { PaymentBrandBadge } from "./payment-brand-icon";
import { ListPagination, ListState, RefreshButton, SearchBox, StatusSegments } from "./payment-list-parts";
import { usePaymentMethodMeta, usePaymentOrderActions, type MethodMeta } from "./payment-order-actions";
import {
  ORDER_STATUS_FILTERS,
  OrderReviewBadge,
  OrderStatusBadge,
  formatOrderTime,
  orderPaidDifference,
  orderPaidDifferenceLabel,
  orderRefundLabel,
  relativeOrderTime
} from "./payment-order-meta";

const ALL = "__all__";
const PAGE_SIZE = 20;
const GRID = "md:grid-cols-[minmax(0,2.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_2.5rem]";

export function PaymentOrdersPanel({
  appId,
  appKey,
  receiptLocale,
  receiptLocaleLabel
}: {
  appId?: number | null;
  appKey?: string | null;
  receiptLocale?: string;
  receiptLocaleLabel?: string;
}) {
  const [status, setStatus] = useState("");
  const [method, setMethod] = useState(ALL);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [keywordInput, setKeywordInput] = useState("");
  const keyword = useDebouncedValue(keywordInput.trim(), 300);
  const [page, setPage] = useState(1);

  const actions = usePaymentOrderActions({ appId, appKey, receiptLocale, receiptLocaleLabel });
  const { methods } = usePaymentMethodMeta();

  const filters = useMemo(
    () => ({
      status: status || undefined,
      payment_method: method === ALL ? undefined : method,
      keyword: keyword || undefined,
      user_id: user?.id,
      page,
      limit: PAGE_SIZE
    }),
    [status, method, keyword, user, page]
  );
  const ordersQuery = useAdminPaymentOrdersQuery(appId, filters);

  const data = ordersQuery.data;
  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, data?.totalPages ?? 1);
  const filtered = Boolean(status || method !== ALL || keyword || user);

  function resetPage<T>(setter: (value: T) => void) {
    return (value: T) => {
      setter(value);
      setPage(1);
    };
  }

  function clearFilters() {
    setStatus("");
    setMethod(ALL);
    setUser(null);
    setKeywordInput("");
    setPage(1);
  }

  return (
    <TooltipProvider delayDuration={300}>
      <div className="space-y-3">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center">
          <StatusSegments label="订单状态" options={ORDER_STATUS_FILTERS} value={status} onChange={resetPage(setStatus)} />
          <div className="flex flex-1 flex-wrap items-center gap-2">
            <SearchBox
              className="w-full sm:w-auto sm:min-w-56 sm:flex-1 xl:max-w-72"
              placeholder="搜索订单号、上游单号或商品"
              value={keywordInput}
              onChange={resetPage(setKeywordInput)}
            />
            <Select value={method} onValueChange={resetPage(setMethod)}>
              <SelectTrigger className="h-9 min-w-0 flex-1 sm:w-40 sm:flex-none">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>全部支付方式</SelectItem>
                {methods.map((m) => (
                  <SelectItem key={m.method} value={m.method}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="min-w-0 flex-1 sm:w-48 sm:flex-none">
              <UserPicker appKey={appKey} value={user} onChange={resetPage(setUser)} placeholder="按用户筛选" />
            </div>
            <div className="ml-auto flex items-center gap-1">
              {filtered ? (
                <Button size="sm" variant="ghost" className="h-9 text-muted-foreground" onClick={clearFilters}>
                  清除筛选
                </Button>
              ) : null}
              <RefreshButton fetching={ordersQuery.isFetching} onClick={() => void ordersQuery.refetch()} />
            </div>
          </div>
        </div>

        <div className="overflow-hidden rounded-2xl border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
          <div className={cn("hidden items-center gap-4 border-b bg-muted/30 px-4 py-2.5 text-xs font-medium text-muted-foreground md:grid", GRID)}>
            <span>订单</span>
            <span className="text-right">金额</span>
            <span>状态</span>
            <span>时间</span>
            <span className="sr-only">操作</span>
          </div>

          {ordersQuery.isLoading ? (
            <ul className="divide-y">
              {Array.from({ length: 6 }).map((_, i) => (
                <li key={i} className="flex items-center gap-3 px-4 py-3.5">
                  <Skeleton className="size-9 rounded-xl" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-40" />
                    <Skeleton className="h-3 w-56" />
                  </div>
                  <Skeleton className="h-5 w-16" />
                  <Skeleton className="hidden h-5 w-16 md:block" />
                  <Skeleton className="hidden h-5 w-16 md:block" />
                </li>
              ))}
            </ul>
          ) : ordersQuery.isError ? (
            <ListState
              icon={<ReceiptText className="size-5" />}
              title="订单加载失败"
              action={
                <Button size="sm" variant="outline" onClick={() => void ordersQuery.refetch()}>
                  重试
                </Button>
              }
            />
          ) : items.length === 0 ? (
            <ListState
              icon={<ReceiptText className="size-5" />}
              title={filtered ? "没有符合条件的订单" : "暂无订单"}
              hint={filtered ? "调整筛选条件后再试" : "用户下单后，订单会出现在这里"}
              action={
                filtered ? (
                  <Button size="sm" variant="outline" onClick={clearFilters}>
                    清除筛选
                  </Button>
                ) : null
              }
            />
          ) : (
            <ul className={cn("divide-y transition-opacity", ordersQuery.isFetching && "opacity-60")}>
              {items.map((order) => (
                <OrderRow
                  key={order.id}
                  order={order}
                  method={actions.method(order.payment_method)}
                  onOpen={() => actions.openDetail(order.order_no)}
                  onRefund={() => actions.openRefund(order.order_no)}
                  onDownload={() => actions.downloadReceipt(order.order_no)}
                  onEmail={() => actions.emailReceipt({ orderNo: order.order_no, subject: order.subject, userId: order.user_id })}
                  downloading={actions.downloading}
                />
              ))}
            </ul>
          )}

          {items.length > 0 ? <ListPagination page={page} totalPages={totalPages} total={total} unit="笔" onPage={setPage} /> : null}
        </div>

        {actions.dialogs}
      </div>
    </TooltipProvider>
  );
}

function OrderRow({
  order,
  method,
  onOpen,
  onRefund,
  onDownload,
  onEmail,
  downloading
}: {
  order: AdminPaymentOrder;
  method: MethodMeta;
  onOpen: () => void;
  onRefund: () => void;
  onDownload: () => void;
  onEmail: () => void;
  downloading: boolean;
}) {
  const refundLabel = orderRefundLabel(order.refund_status);
  const refundable = order.status === "paid" && order.refund_status !== "full";
  const paidDiff = orderPaidDifference(order);
  const review = Boolean(order.review_reason);
  return (
    <li
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cn(
        "grid cursor-pointer grid-cols-[minmax(0,1fr)_auto_2.5rem] items-center gap-x-4 gap-y-1 px-4 py-3 transition-colors outline-none hover:bg-muted/40 focus-visible:bg-muted/50",
        GRID
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        <PaymentBrandBadge slug={method.icon} brandColor={method.brandColor} name={method.name} />
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-foreground">{order.subject || "未命名商品"}</p>
          <p className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
            <span className="truncate font-mono">{order.order_no}</span>
            <button
              type="button"
              aria-label="复制订单号"
              className="shrink-0 opacity-0 transition-opacity hover:text-foreground focus-visible:opacity-100 [li:hover_&]:opacity-100"
              onClick={(e) => {
                e.stopPropagation();
                void navigator.clipboard.writeText(order.order_no);
                toast.success("已复制订单号");
              }}
            >
              <Copy className="size-3" />
            </button>
          </p>
          <p className="mt-0.5 flex gap-2 text-xs text-muted-foreground">
            <span className="truncate">{method.name}</span>
            {order.user_id ? <span className="shrink-0 font-mono">用户 #{order.user_id}</span> : null}
          </p>
        </div>
      </div>

      <div className="text-right">
        <p className="font-mono text-sm font-semibold tabular-nums">{formatMoney(order.amount)}</p>
        {paidDiff ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <p
                className={cn(
                  "text-xs tabular-nums",
                  paidDiff.diff < 0 ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400"
                )}
              >
                实付 {formatMoney(paidDiff.paid)}
              </p>
            </TooltipTrigger>
            <TooltipContent>{orderPaidDifferenceLabel(paidDiff.diff)}</TooltipContent>
          </Tooltip>
        ) : null}
        {refundLabel ? <p className="text-xs text-sky-600 tabular-nums dark:text-sky-400">已退 {formatMoney(order.refunded_amount)}</p> : null}
        <div className="mt-1 flex flex-wrap justify-end gap-1 md:hidden">
          <OrderStatusBadge status={order.status} />
          {review ? <OrderReviewBadge /> : null}
        </div>
      </div>

      <div className="hidden flex-col items-start gap-1 md:flex">
        <OrderStatusBadge status={order.status} />
        {review ? <OrderReviewBadge /> : null}
        {refundLabel ? <span className="text-xs text-muted-foreground">{refundLabel}</span> : null}
      </div>

      <div className="hidden md:block">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="text-sm text-muted-foreground tabular-nums">{relativeOrderTime(order.createdAt)}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>创建 {formatOrderTime(order.createdAt, true)}</p>
            {order.paid_at ? <p>支付 {formatOrderTime(order.paid_at, true)}</p> : null}
          </TooltipContent>
        </Tooltip>
      </div>

      {/* 操作：吃掉行点击，点菜单不该顺带拉开详情 */}
      <div className="flex justify-end" onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" className="size-8" aria-label="订单操作">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onClick={onOpen}>
              <ReceiptText />
              查看详情
            </DropdownMenuItem>
            <DropdownMenuItem disabled={downloading} onClick={onDownload}>
              <Download />
              下载凭证
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onEmail}>
              <Mail />
              寄送凭证
            </DropdownMenuItem>
            {refundable ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={onRefund}>
                  <Undo2 />
                  退款
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  );
}
