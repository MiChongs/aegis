"use client";

import { useMemo, useState } from "react";
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  Download,
  Mail,
  MoreHorizontal,
  ReceiptText,
  RefreshCw,
  Search,
  Undo2,
  X
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { AdminPaymentOrder } from "@/lib/api-client";
import { useAdminPaymentMethodsQuery, useAdminPaymentOrdersQuery } from "@/lib/admin-hooks";
import {
  useDownloadOrderReceiptMutation,
  useEmailOrderReceiptMutation,
  useReceiptRecipients
} from "@/lib/commerce-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { formatMoney } from "@/components/commerce/commerce-format";
import { ReceiptEmailDialog } from "@/components/commerce/receipt-email-dialog";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { PaymentBrandBadge } from "./payment-brand-icon";
import { PaymentOrderDetailSheet } from "./payment-order-detail";
import {
  ORDER_STATUS_FILTERS,
  OrderStatusBadge,
  formatOrderTime,
  orderRefundLabel,
  relativeOrderTime
} from "./payment-order-meta";
import { PaymentRefundDialog } from "./payment-refund-dialog";

const ALL = "__all__";
const PAGE_SIZE = 20;

type EmailTarget = { orderNo: string; subject: string; userId?: number | null };

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
  const [detailOrderNo, setDetailOrderNo] = useState<string | null>(null);
  const [refundOrderNo, setRefundOrderNo] = useState<string | null>(null);
  const [emailTarget, setEmailTarget] = useState<EmailTarget | null>(null);

  const downloadReceipt = useDownloadOrderReceiptMutation(appId);
  const emailReceipt = useEmailOrderReceiptMutation(appId);
  const recipients = useReceiptRecipients(appKey, emailTarget?.userId ?? null);

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
  const methodsQuery = useAdminPaymentMethodsQuery();

  // 支付方式的展示名与品牌图标来自后端渠道目录，与渠道配置面板一致
  const methods = useMemo(() => methodsQuery.data ?? [], [methodsQuery.data]);
  const methodMeta = useMemo(() => {
    const byCode = new Map(methods.map((m) => [m.method, m] as const));
    return (code?: string) => {
      const found = code ? byCode.get(code) : undefined;
      return { name: found?.name ?? code ?? "未知方式", icon: found?.icon, brandColor: found?.brandColor };
    };
  }, [methods]);

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

  async function handleDownloadReceipt(orderNo: string) {
    try {
      await downloadReceipt.mutateAsync({ order_no: orderNo, locale: receiptLocale });
      toast.success("凭证已生成");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "凭证生成失败");
    }
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
      <div className="space-y-4">
        {/* 状态分段 + 工具栏 */}
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
          <div className="inline-flex w-fit shrink-0 rounded-lg border bg-muted/40 p-0.5" role="tablist" aria-label="订单状态">
            {ORDER_STATUS_FILTERS.map((option) => (
              <button
                key={option.value || "all"}
                type="button"
                role="tab"
                aria-selected={status === option.value}
                onClick={() => resetPage(setStatus)(option.value)}
                className={cn(
                  "rounded-md px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground",
                  status === option.value && "bg-background text-foreground shadow-sm"
                )}
              >
                {option.label}
              </button>
            ))}
          </div>

          <div className="flex flex-1 flex-wrap items-center gap-2">
            <div className="relative min-w-56 flex-1 lg:max-w-80">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="h-9 pr-8 pl-9"
                placeholder="搜索订单号、上游单号或商品"
                value={keywordInput}
                onChange={(e) => resetPage(setKeywordInput)(e.target.value)}
              />
              {keywordInput ? (
                <button
                  type="button"
                  aria-label="清空搜索"
                  className="absolute top-1/2 right-2.5 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => resetPage(setKeywordInput)("")}
                >
                  <X className="size-4" />
                </button>
              ) : null}
            </div>
            <Select value={method} onValueChange={resetPage(setMethod)}>
              <SelectTrigger className="h-9 w-40">
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
            <div className="w-52">
              <UserPicker appKey={appKey} value={user} onChange={resetPage(setUser)} placeholder="按用户筛选" />
            </div>
            <div className="ml-auto flex items-center gap-1">
              {filtered ? (
                <Button size="sm" variant="ghost" className="h-9 text-muted-foreground" onClick={clearFilters}>
                  清除筛选
                </Button>
              ) : null}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-9"
                    aria-label="刷新"
                    onClick={() => void ordersQuery.refetch()}
                  >
                    <RefreshCw className={cn("size-4", ordersQuery.isFetching && "animate-spin")} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>刷新</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </div>

        {/* 列表 */}
        <div className="overflow-hidden rounded-xl border bg-card">
          <div className="hidden grid-cols-[minmax(0,2.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_2.5rem] items-center gap-4 border-b bg-muted/30 px-4 py-2.5 text-xs font-medium text-muted-foreground md:grid">
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
                    <Skeleton className="h-4 w-48" />
                    <Skeleton className="h-3 w-64" />
                  </div>
                  <Skeleton className="h-5 w-20" />
                  <Skeleton className="hidden h-5 w-16 md:block" />
                </li>
              ))}
            </ul>
          ) : ordersQuery.isError ? (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <p className="text-sm text-muted-foreground">订单加载失败</p>
              <Button size="sm" variant="outline" onClick={() => void ordersQuery.refetch()}>
                重试
              </Button>
            </div>
          ) : items.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-16 text-center">
              <span className="flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <ReceiptText className="size-5" />
              </span>
              <p className="text-sm font-medium">{filtered ? "没有符合条件的订单" : "暂无订单"}</p>
              <p className="text-xs text-muted-foreground">
                {filtered ? "调整筛选条件后再试" : "用户下单后，订单会出现在这里"}
              </p>
              {filtered ? (
                <Button size="sm" variant="outline" className="mt-2" onClick={clearFilters}>
                  清除筛选
                </Button>
              ) : null}
            </div>
          ) : (
            <ul className={cn("divide-y transition-opacity", ordersQuery.isFetching && "opacity-60")}>
              {items.map((order) => (
                <OrderRow
                  key={order.id}
                  order={order}
                  method={methodMeta(order.payment_method)}
                  onOpen={() => setDetailOrderNo(order.order_no)}
                  onRefund={() => setRefundOrderNo(order.order_no)}
                  onDownload={() => void handleDownloadReceipt(order.order_no)}
                  onEmail={() => setEmailTarget({ orderNo: order.order_no, subject: order.subject, userId: order.user_id })}
                  downloading={downloadReceipt.isPending}
                />
              ))}
            </ul>
          )}

          {items.length > 0 ? (
            <div className="flex items-center justify-between gap-3 border-t bg-muted/20 px-4 py-2.5">
              <span className="text-xs text-muted-foreground tabular-nums">
                共 {total} 笔，第 {page} / {totalPages} 页
              </span>
              <div className="flex items-center gap-1">
                <Button size="icon" variant="ghost" className="size-8" aria-label="上一页" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                  <ChevronLeft className="size-4" />
                </Button>
                {pageWindow(page, totalPages).map((p, i) =>
                  p === "…" ? (
                    <span key={`gap-${i}`} className="px-1 text-xs text-muted-foreground">…</span>
                  ) : (
                    <Button
                      key={p}
                      size="sm"
                      variant={p === page ? "secondary" : "ghost"}
                      className="h-8 min-w-8 px-2 tabular-nums"
                      onClick={() => setPage(p)}
                    >
                      {p}
                    </Button>
                  )
                )}
                <Button size="icon" variant="ghost" className="size-8" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
                  <ChevronRight className="size-4" />
                </Button>
              </div>
            </div>
          ) : null}
        </div>

        <PaymentOrderDetailSheet
          appId={appId}
          orderNo={detailOrderNo}
          method={methodMeta}
          onOpenChange={(open) => !open && setDetailOrderNo(null)}
          onRefund={setRefundOrderNo}
          onDownloadReceipt={(orderNo) => void handleDownloadReceipt(orderNo)}
          onEmailReceipt={(order) => setEmailTarget({ orderNo: order.order_no, subject: order.subject, userId: order.user_id })}
          downloading={downloadReceipt.isPending}
        />

        <PaymentRefundDialog
          open={Boolean(refundOrderNo)}
          onOpenChange={(open) => !open && setRefundOrderNo(null)}
          appId={appId}
          orderNo={refundOrderNo}
        />

        <ReceiptEmailDialog
          open={Boolean(emailTarget)}
          onOpenChange={(open) => !open && setEmailTarget(null)}
          title="寄送订单凭证"
          subject={emailTarget?.subject ?? ""}
          reference={emailTarget?.orderNo ?? ""}
          suggestions={recipients}
          localeLabel={receiptLocaleLabel}
          pending={emailReceipt.isPending}
          onSubmit={async (to) => {
            if (!emailTarget) return;
            await emailReceipt.mutateAsync({ order_no: emailTarget.orderNo, email: to, locale: receiptLocale });
            setEmailTarget(null);
          }}
        />
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
  method: { name: string; icon?: string | null; brandColor?: string | null };
  onOpen: () => void;
  onRefund: () => void;
  onDownload: () => void;
  onEmail: () => void;
  downloading: boolean;
}) {
  const refundLabel = orderRefundLabel(order.refund_status);
  const refundable = order.status === "paid" && order.refund_status !== "full";
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
      className="grid cursor-pointer grid-cols-[minmax(0,1fr)_auto_2.5rem] items-center gap-x-4 gap-y-1 px-4 py-3 transition-colors outline-none hover:bg-muted/40 focus-visible:bg-muted/50 md:grid-cols-[minmax(0,2.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_2.5rem]"
    >
      {/* 订单：品牌 + 商品 + 单号与用户 */}
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
          <p className="mt-0.5 text-xs text-muted-foreground">
            {method.name}
            {order.user_id ? <span className="ml-2 font-mono">用户 #{order.user_id}</span> : null}
          </p>
        </div>
      </div>

      {/* 金额 */}
      <div className="text-right">
        <p className="font-mono text-sm font-semibold tabular-nums">{formatMoney(order.amount)}</p>
        {refundLabel ? (
          <p className="text-xs text-sky-600 tabular-nums dark:text-sky-400">已退 {formatMoney(order.refunded_amount)}</p>
        ) : null}
        <div className="mt-1 md:hidden">
          <OrderStatusBadge status={order.status} />
        </div>
      </div>

      {/* 状态 */}
      <div className="hidden flex-col items-start gap-1 md:flex">
        <OrderStatusBadge status={order.status} />
        {refundLabel ? <span className="text-xs text-muted-foreground">{refundLabel}</span> : null}
      </div>

      {/* 时间 */}
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
              <ReceiptText />查看详情
            </DropdownMenuItem>
            <DropdownMenuItem disabled={downloading} onClick={onDownload}>
              <Download />下载凭证
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onEmail}>
              <Mail />寄送凭证
            </DropdownMenuItem>
            {refundable ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={onRefund}>
                  <Undo2 />退款
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  );
}

/** 分页窗口：首尾页 + 当前页前后各一页，中间用省略号。 */
function pageWindow(current: number, total: number): Array<number | "…"> {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const pages = new Set([1, total, current - 1, current, current + 1]);
  const sorted = [...pages].filter((p) => p >= 1 && p <= total).sort((a, b) => a - b);
  const out: Array<number | "…"> = [];
  sorted.forEach((p, i) => {
    if (i > 0 && p - (sorted[i - 1] as number) > 1) out.push("…");
    out.push(p);
  });
  return out;
}
