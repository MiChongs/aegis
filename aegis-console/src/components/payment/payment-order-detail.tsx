"use client";

import type { ReactNode } from "react";
import { Check, Copy, Download, Mail, Undo2, X } from "lucide-react";
import { toast } from "sonner";
import type { AdminPaymentOrderDetail } from "@/lib/api-client";
import { useAdminOrderRefundsQuery, useAdminPaymentOrderDetailQuery } from "@/lib/admin-hooks";
import { formatMoney } from "@/components/commerce/commerce-format";
import { Button } from "@/components/ui/button";
import { JsonViewer } from "@/components/ui/json-viewer";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { PaymentBrandBadge } from "./payment-brand-icon";
import {
  OrderReviewBadge,
  orderPaidDifference,
  orderPaidDifferenceLabel,
  OrderStatusBadge,
  callbackVerified,
  formatOrderTime,
  orderRefundLabel,
  refundStatusBadge,
  reversalBadge
} from "./payment-order-meta";

type MethodMeta = { name: string; icon?: string | null; brandColor?: string | null };

export function PaymentOrderDetailSheet({
  appId,
  orderNo,
  method,
  onOpenChange,
  onRefund,
  onDownloadReceipt,
  onEmailReceipt,
  downloading
}: {
  appId?: number | null;
  orderNo: string | null;
  method: (code?: string) => MethodMeta;
  onOpenChange: (open: boolean) => void;
  onRefund: (orderNo: string) => void;
  onDownloadReceipt: (orderNo: string) => void;
  onEmailReceipt: (order: AdminPaymentOrderDetail["order"]) => void;
  downloading?: boolean;
}) {
  const detailQuery = useAdminPaymentOrderDetailQuery(appId, orderNo);
  const detail = detailQuery.data;
  const order = detail?.order;
  const meta = method(order?.payment_method);
  const refundable = order?.status === "paid" && order.refund_status !== "full";
  const paidDiff = order ? orderPaidDifference(order) : null;

  return (
    <Sheet open={Boolean(orderNo)} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-[560px]">
        <SheetHeader className="shrink-0 gap-0 border-b px-5 pt-5 pb-4 sm:px-6">
          <SheetTitle className="sr-only">订单详情</SheetTitle>
          <SheetDescription className="sr-only">订单的支付、履约、退款与回调记录</SheetDescription>
          {detailQuery.isLoading || !order ? (
            <div className="space-y-3 pr-8">
              <Skeleton className="h-5 w-40" />
              <Skeleton className="h-9 w-36" />
              <Skeleton className="h-8 w-64" />
            </div>
          ) : (
            <div className="space-y-4">
              <div className="flex items-center gap-2.5 pr-8">
                <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} size="sm" />
                <span className="min-w-0 truncate text-sm text-muted-foreground">{meta.name}</span>
                <OrderStatusBadge status={order.status} className="ml-auto shrink-0" />
              </div>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-foreground">{order.subject || "未命名商品"}</p>
                <p className="mt-1 font-mono text-3xl font-semibold tracking-tight tabular-nums">{formatMoney(order.amount)}</p>
                {paidDiff ? (
                  <p
                    className={cn(
                      "mt-1 text-xs tabular-nums",
                      paidDiff.diff < 0 ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400"
                    )}
                  >
                    实付 {formatMoney(paidDiff.paid)}，{orderPaidDifferenceLabel(paidDiff.diff)}
                  </p>
                ) : null}
                {orderRefundLabel(order.refund_status) ? (
                  <p className="mt-1 text-xs text-sky-600 tabular-nums dark:text-sky-400">
                    {orderRefundLabel(order.refund_status)}，已退 {formatMoney(order.refunded_amount)}
                  </p>
                ) : null}
              </div>
              {order.review_reason ? (
                <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-3.5 py-3 text-sm">
                  <div className="flex items-center gap-2">
                    <OrderReviewBadge />
                    {order.review_at ? (
                      <span className="text-xs text-muted-foreground tabular-nums">{formatOrderTime(order.review_at, true)} 标记</span>
                    ) : null}
                  </div>
                  <p className="mt-2 text-foreground">{order.review_note || "实付与订单金额不一致，未自动结单。"}</p>
                  <p className="mt-1 text-xs text-muted-foreground">系统不会再自动查单。核实后请补发商品或在渠道后台退款。</p>
                </div>
              ) : null}
              <div className="flex flex-wrap gap-2">
                {refundable ? (
                  <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => onRefund(order.order_no)}>
                    <Undo2 className="size-3.5" />
                    退款
                  </Button>
                ) : null}
                <Button size="sm" variant="outline" className="h-8 gap-1.5" disabled={downloading} onClick={() => onDownloadReceipt(order.order_no)}>
                  <Download className="size-3.5" />
                  下载凭证
                </Button>
                <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => onEmailReceipt(order)}>
                  <Mail className="size-3.5" />
                  寄送凭证
                </Button>
              </div>
            </div>
          )}
        </SheetHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6">
          {detailQuery.isLoading ? (
            <div className="space-y-4">
              <Skeleton className="h-14 w-full rounded-xl" />
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-32 w-full rounded-xl" />
              ))}
            </div>
          ) : !detail || !order ? (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <p className="text-sm text-muted-foreground">订单详情加载失败</p>
              <Button size="sm" variant="outline" onClick={() => void detailQuery.refetch()}>
                重试
              </Button>
            </div>
          ) : (
            <div className="space-y-6">
              <OrderProgress detail={detail} />

              <Section title="订单">
                <Facts>
                  <Fact label="订单号" value={order.order_no} mono copyable />
                  <Fact label="上游单号" value={order.provider_order_no} mono copyable />
                  <Fact label="商品" value={order.subject} />
                  {order.body ? <Fact label="商品描述" value={order.body} wrap /> : null}
                  <Fact label="金额" value={formatMoney(order.amount)} mono />
                  {order.paid_amount ? (
                    <Fact
                      label="实付金额"
                      value={paidDiff ? `${formatMoney(order.paid_amount)}（${orderPaidDifferenceLabel(paidDiff.diff)}）` : formatMoney(order.paid_amount)}
                      mono
                    />
                  ) : null}
                  {Number(order.refunded_amount) > 0 ? <Fact label="已退金额" value={formatMoney(order.refunded_amount)} mono /> : null}
                </Facts>
              </Section>

              <Section title="支付与履约">
                <Facts>
                  <Fact label="支付方式" value={meta.name} />
                  <Fact label="支付时间" value={formatOrderTime(order.paid_at, true)} />
                  <Fact label="过期时间" value={formatOrderTime(order.expire_at, true)} />
                  <Fact label="业务通知" value={notifyLabel(order.notify_status)} />
                  <Fact label="履约" value={fulfillmentLabel(detail)} />
                </Facts>
              </Section>

              <Section title="用户与来源">
                <Facts>
                  <Fact label="用户" value={order.user_id ? `#${order.user_id}` : ""} mono />
                  <Fact label="客户端 IP" value={order.client_ip} mono copyable />
                </Facts>
              </Section>

              <OrderRefunds appId={appId} orderNo={order.order_no} />

              <Section title="支付回调" count={detail.callback_logs?.length ?? 0}>
                {detail.callback_logs?.length ? (
                  <ul className="divide-y rounded-xl border">
                    {detail.callback_logs.map((log) => {
                      const ok = callbackVerified(log.verification_status);
                      return (
                        <li key={log.id} className="flex gap-3 px-4 py-3">
                          <span
                            className={cn(
                              "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full",
                              ok ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" : "bg-red-500/10 text-red-600 dark:text-red-400"
                            )}
                          >
                            {ok ? <Check className="size-3" strokeWidth={3} /> : <X className="size-3" strokeWidth={3} />}
                          </span>
                          <div className="min-w-0 flex-1 space-y-0.5">
                            <div className="flex items-baseline justify-between gap-3">
                              <span className="text-sm font-medium">{ok ? "验签通过" : "验签失败"}</span>
                              <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatOrderTime(log.created_at, true)}</span>
                            </div>
                            {log.message ? <p className="text-xs break-all text-muted-foreground">{log.message}</p> : null}
                            {!ok ? <p className="font-mono text-[11px] break-all text-muted-foreground">{log.verification_status}</p> : null}
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                ) : (
                  <p className="rounded-xl border border-dashed px-4 py-5 text-center text-xs text-muted-foreground">暂无回调记录</p>
                )}
              </Section>

              {order.metadata && Object.keys(order.metadata).length > 0 ? (
                <Section title="订单元数据">
                  <JsonViewer value={JSON.stringify(order.metadata, null, 2)} height={180} />
                </Section>
              ) : null}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

/** 横向进度：创建、支付、履约，退过款再加一步。宽度不够时每步自动收窄，不换行。 */
function OrderProgress({ detail }: { detail: AdminPaymentOrderDetail }) {
  const order = detail.order;
  const steps: Array<{ label: string; time?: string | null; done: boolean; tone?: "danger" | "info" }> = [
    { label: "创建", time: order.createdAt, done: true },
    order.status === "expired"
      ? { label: "已过期", time: order.expire_at, done: true, tone: "danger" }
      : order.status === "failed"
        ? { label: "支付失败", time: order.updatedAt, done: true, tone: "danger" }
        : { label: "支付", time: order.paid_at, done: Boolean(order.paid_at) },
    { label: "履约", time: detail.fulfilled_at, done: detail.fulfillment_status === "done" }
  ];
  if (order.refund_status === "partial" || order.refund_status === "full") {
    steps.push({ label: order.refund_status === "full" ? "全额退款" : "部分退款", time: order.updatedAt, done: true, tone: "info" });
  }
  return (
    <ol className="grid rounded-xl border px-2 py-4" style={{ gridTemplateColumns: `repeat(${steps.length}, minmax(0, 1fr))` }}>
      {steps.map((step, index) => (
        <li key={step.label} className="relative flex flex-col items-center gap-1.5 px-1 text-center">
          {index > 0 ? (
            <span
              aria-hidden
              className={cn("absolute top-2.5 h-px", step.done ? "bg-foreground/25" : "bg-border")}
              style={{ left: "calc(-50% + 16px)", right: "calc(50% + 16px)" }}
            />
          ) : null}
          <span
            className={cn(
              "relative z-10 flex size-5 items-center justify-center rounded-full border",
              !step.done && "border-border bg-background",
              step.done && !step.tone && "border-emerald-500 bg-emerald-500 text-white",
              step.tone === "danger" && "border-red-500 bg-red-500 text-white",
              step.tone === "info" && "border-sky-500 bg-sky-500 text-white"
            )}
          >
            {step.done ? step.tone === "danger" ? <X className="size-3" strokeWidth={3} /> : <Check className="size-3" strokeWidth={3} /> : null}
          </span>
          <span className={cn("text-xs", step.done ? "font-medium text-foreground" : "text-muted-foreground")}>{step.label}</span>
          <span className="text-[11px] text-muted-foreground tabular-nums">{step.done ? formatOrderTime(step.time) || "无" : "未完成"}</span>
        </li>
      ))}
    </ol>
  );
}

function OrderRefunds({ appId, orderNo }: { appId?: number | null; orderNo: string }) {
  const refundsQuery = useAdminOrderRefundsQuery(appId, orderNo);
  const items = refundsQuery.data ?? [];
  if (refundsQuery.isLoading) return <Skeleton className="h-20 w-full rounded-xl" />;
  if (items.length === 0) return null;
  return (
    <Section title="退款记录" count={items.length}>
      <ul className="divide-y rounded-xl border">
        {items.map((refund) => (
          <li key={refund.id} className="space-y-1.5 px-4 py-3">
            <div className="flex items-center justify-between gap-3">
              <span className="font-mono text-sm font-semibold tabular-nums">-{formatMoney(refund.amount)}</span>
              <span className="flex flex-wrap justify-end gap-1.5">
                {refundStatusBadge(refund.status)}
                {reversalBadge(refund)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
              <span className="truncate font-mono">{refund.refund_no}</span>
              <span className="shrink-0 tabular-nums">{formatOrderTime(refund.createdAt, true)}</span>
            </div>
            {refund.reason ? <p className="text-xs text-muted-foreground">原因：{refund.reason}</p> : null}
            {refund.error_message ? <p className="text-xs break-all text-destructive">{refund.error_message}</p> : null}
            {refund.reversal_message ? <p className="text-xs text-amber-600 dark:text-amber-400">{refund.reversal_message}</p> : null}
          </li>
        ))}
      </ul>
    </Section>
  );
}

function Section({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  return (
    <section className="space-y-2.5">
      <h3 className="flex items-center gap-1.5 text-sm font-medium">
        {title}
        {count !== undefined ? <span className="text-xs font-normal text-muted-foreground tabular-nums">{count}</span> : null}
      </h3>
      {children}
    </section>
  );
}

function Facts({ children }: { children: ReactNode }) {
  return <dl className="divide-y rounded-xl border">{children}</dl>;
}

/** 一行键值：标签在左、值在右；长文本（如商品描述）用 wrap 换行展示，其余截断并提供复制。 */
function Fact({
  label,
  value,
  mono,
  copyable,
  wrap
}: {
  label: string;
  value?: string | null;
  mono?: boolean;
  copyable?: boolean;
  wrap?: boolean;
}) {
  const text = value?.trim() ? value : "";
  return (
    <div className="flex min-w-0 items-start gap-4 px-4 py-2.5">
      <dt className="w-20 shrink-0 pt-px text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("flex min-w-0 flex-1 items-center justify-end gap-1.5 text-right text-sm", mono && "font-mono text-[13px]")}>
        <span className={cn(wrap ? "break-all" : "truncate", !text && "text-muted-foreground")} title={text || undefined}>
          {text || "无"}
        </span>
        {copyable && text ? (
          <button
            type="button"
            aria-label={`复制${label}`}
            className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
            onClick={() => {
              void navigator.clipboard.writeText(text);
              toast.success(`已复制${label}`);
            }}
          >
            <Copy className="size-3.5" />
          </button>
        ) : null}
      </dd>
    </div>
  );
}

function fulfillmentLabel(detail: AdminPaymentOrderDetail) {
  switch (detail.fulfillment_status) {
    case "done":
      return "已履约";
    case "none":
      return "无需履约";
    case "pending":
      return "待履约";
    case "failed":
      return "履约失败";
    default:
      return detail.fulfillment_status || "";
  }
}

function notifyLabel(status?: string) {
  switch (status) {
    case "success":
    case "notified":
      return "已通知";
    case "pending":
      return "待通知";
    case "failed":
      return "通知失败";
    default:
      return status || "";
  }
}
