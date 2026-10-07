"use client";

import type { ReactNode } from "react";
import { Check, Copy, Download, Mail, Undo2 } from "lucide-react";
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
  OrderStatusBadge,
  callbackVerified,
  formatOrderTime,
  orderRefundLabel
} from "./payment-order-meta";
import { refundStatusBadge, reversalBadge } from "./payment-refunds-panel";

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

  return (
    <Sheet open={Boolean(orderNo)} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="shrink-0 gap-3 border-b px-6 pt-5 pb-4">
          <SheetTitle className="sr-only">订单详情</SheetTitle>
          <SheetDescription className="sr-only">订单的支付、履约、退款与回调记录</SheetDescription>
          {detailQuery.isLoading || !order ? (
            <div className="flex items-center gap-3">
              <Skeleton className="size-12 rounded-2xl" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-4 w-40" />
                <Skeleton className="h-7 w-28" />
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-start gap-3 pr-8">
                <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} size="lg" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-foreground">{order.subject}</p>
                  <p className="mt-0.5 font-mono text-2xl font-semibold tracking-tight tabular-nums">
                    {formatMoney(order.amount)}
                  </p>
                  <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                    <OrderStatusBadge status={order.status} />
                    {orderRefundLabel(order.refund_status) ? (
                      <span className="text-xs text-sky-600 dark:text-sky-400">
                        {orderRefundLabel(order.refund_status)}，已退 {formatMoney(order.refunded_amount)}
                      </span>
                    ) : null}
                  </div>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                {order.status === "paid" && order.refund_status !== "full" ? (
                  <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => onRefund(order.order_no)}>
                    <Undo2 className="size-3.5" />退款
                  </Button>
                ) : null}
                <Button size="sm" variant="outline" className="h-8 gap-1.5" disabled={downloading} onClick={() => onDownloadReceipt(order.order_no)}>
                  <Download className="size-3.5" />下载凭证
                </Button>
                <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => onEmailReceipt(order)}>
                  <Mail className="size-3.5" />寄送凭证
                </Button>
              </div>
            </>
          )}
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          {detailQuery.isLoading ? (
            <div className="space-y-3">{Array.from({ length: 7 }).map((_, i) => <Skeleton key={i} className="h-9 w-full rounded-lg" />)}</div>
          ) : !detail || !order ? (
            <p className="py-10 text-center text-sm text-muted-foreground">订单详情加载失败</p>
          ) : (
            <div className="space-y-6">
              <Section title="进度">
                <OrderTimeline detail={detail} />
              </Section>

              <Section title="订单信息">
                <Facts>
                  <Fact label="订单号" value={order.order_no} mono copyable />
                  <Fact label="上游单号" value={order.provider_order_no} mono copyable />
                  <Fact label="商品" value={order.subject} />
                  {order.body ? <Fact label="商品描述" value={order.body} /> : null}
                  <Fact label="金额" value={formatMoney(order.amount)} mono />
                  <Fact label="已退金额" value={formatMoney(order.refunded_amount)} mono />
                </Facts>
              </Section>

              <Section title="支付">
                <Facts>
                  <Fact label="支付方式" value={meta.name} />
                  <Fact label="通知状态" value={notifyLabel(order.notify_status)} />
                  <Fact label="履约" value={fulfillmentLabel(detail)} />
                  <Fact label="过期时间" value={formatOrderTime(order.expire_at, true)} />
                </Facts>
              </Section>

              <Section title="用户与来源">
                <Facts>
                  <Fact label="用户" value={order.user_id ? `#${order.user_id}` : ""} mono />
                  <Fact label="客户端 IP" value={order.client_ip} mono copyable />
                </Facts>
              </Section>

              <OrderRefunds appId={appId} orderNo={order.order_no} />

              <Section title={`支付回调（${detail.callback_logs?.length ?? 0}）`}>
                {detail.callback_logs?.length ? (
                  <ul className="divide-y rounded-xl border">
                    {detail.callback_logs.map((log) => {
                      const ok = callbackVerified(log.verification_status);
                      return (
                        <li key={log.id} className="space-y-1 px-4 py-3">
                          <div className="flex items-center justify-between gap-3">
                            <span className="flex items-center gap-2 text-sm font-medium">
                              <span className={cn("size-1.5 rounded-full", ok ? "bg-emerald-500" : "bg-red-500")} />
                              {ok ? "验签通过" : "验签失败"}
                            </span>
                            <span className="text-xs text-muted-foreground tabular-nums">{formatOrderTime(log.created_at, true)}</span>
                          </div>
                          {log.message ? <p className="text-xs text-muted-foreground">{log.message}</p> : null}
                          {!ok ? <p className="font-mono text-[11px] text-muted-foreground">{log.verification_status}</p> : null}
                        </li>
                      );
                    })}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">暂无回调记录</p>
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

function OrderTimeline({ detail }: { detail: AdminPaymentOrderDetail }) {
  const order = detail.order;
  const steps: Array<{ label: string; time?: string | null; done: boolean; tone?: "danger" | "info" }> = [
    { label: "创建订单", time: order.createdAt, done: true },
    order.status === "expired"
      ? { label: "订单过期", time: order.expire_at, done: true, tone: "danger" }
      : order.status === "failed"
        ? { label: "支付失败", time: order.updatedAt, done: true, tone: "danger" }
        : { label: "完成支付", time: order.paid_at, done: Boolean(order.paid_at) },
    { label: "完成履约", time: detail.fulfilled_at, done: detail.fulfillment_status === "done" }
  ];
  if (order.refund_status === "partial" || order.refund_status === "full") {
    steps.push({ label: orderRefundLabel(order.refund_status), time: order.updatedAt, done: true, tone: "info" });
  }
  return (
    <ol className="relative space-y-4 pl-6">
      <span className="absolute top-1.5 bottom-1.5 left-[7px] w-px bg-border" aria-hidden />
      {steps.map((step) => (
        <li key={step.label} className="relative">
          <span
            className={cn(
              "absolute top-0.5 -left-6 flex size-4 items-center justify-center rounded-full border-2 bg-background",
              !step.done && "border-border",
              step.done && !step.tone && "border-emerald-500 bg-emerald-500 text-white",
              step.tone === "danger" && "border-red-500 bg-red-500 text-white",
              step.tone === "info" && "border-sky-500 bg-sky-500 text-white"
            )}
          >
            {step.done ? <Check className="size-2.5" strokeWidth={3} /> : null}
          </span>
          <div className="flex items-baseline justify-between gap-3">
            <span className={cn("text-sm", step.done ? "font-medium text-foreground" : "text-muted-foreground")}>{step.label}</span>
            <span className="text-xs text-muted-foreground tabular-nums">{step.done ? formatOrderTime(step.time, true) : "未完成"}</span>
          </div>
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
    <Section title={`退款记录（${items.length}）`}>
      <ul className="divide-y rounded-xl border">
        {items.map((refund) => (
          <li key={refund.id} className="space-y-1.5 px-4 py-3">
            <div className="flex items-center justify-between gap-3">
              <span className="font-mono text-sm font-medium tabular-nums">-{formatMoney(refund.amount)}</span>
              <span className="flex items-center gap-1.5">
                {refundStatusBadge(refund.status)}
                {reversalBadge(refund)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
              <span className="font-mono">{refund.refund_no}</span>
              <span className="tabular-nums">{formatOrderTime(refund.createdAt, true)}</span>
            </div>
            {refund.reason ? <p className="text-xs text-muted-foreground">原因：{refund.reason}</p> : null}
            {refund.error_message ? <p className="text-xs text-destructive">{refund.error_message}</p> : null}
            {refund.reversal_message ? <p className="text-xs text-amber-600">{refund.reversal_message}</p> : null}
          </li>
        ))}
      </ul>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2.5">
      <h3 className="text-xs font-medium text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

function Facts({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-1 gap-x-6 gap-y-3.5 rounded-xl border px-4 py-3.5 sm:grid-cols-2">{children}</dl>;
}

function Fact({ label, value, mono, copyable }: { label: string; value?: string | null; mono?: boolean; copyable?: boolean }) {
  const text = value?.trim() ? value : "";
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("flex min-w-0 items-center gap-1.5 text-sm text-foreground", mono && "font-mono text-[13px]")}>
        <span className="truncate" title={text}>{text || <span className="text-muted-foreground">无</span>}</span>
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
      return `已履约 ${formatOrderTime(detail.fulfilled_at, true)}`.trim();
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
