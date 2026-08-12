"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, Download, Mail, RefreshCw, Search, Undo2, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import {
  useAdminOrderRefundsQuery,
  useAdminPaymentMethodsQuery,
  useAdminPaymentOrderDetailQuery,
  useAdminPaymentOrdersQuery
} from "@/lib/admin-hooks";
import {
  useDownloadOrderReceiptMutation,
  useEmailOrderReceiptMutation,
  useReceiptRecipients
} from "@/lib/commerce-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { ReceiptEmailDialog } from "@/components/commerce/receipt-email-dialog";
import type { AdminPaymentOrder } from "@/lib/api-client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { PaymentBrandBadge } from "./payment-brand-icon";
import { PaymentRefundDialog } from "./payment-refund-dialog";
import { refundStatusBadge, reversalBadge } from "./payment-refunds-panel";

const ALL = "__all__";

const statusOptions = [
  { value: "pending", label: "待支付" },
  { value: "paid", label: "已支付" },
  { value: "expired", label: "已过期" },
  { value: "failed", label: "失败" }
];

function statusBadge(status: string) {
  switch (status) {
    case "paid":
      return <Badge className="bg-emerald-500/15 text-emerald-600 hover:bg-emerald-500/15">已支付</Badge>;
    case "pending":
      return <Badge className="bg-amber-500/15 text-amber-600 hover:bg-amber-500/15">待支付</Badge>;
    case "expired":
      return <Badge variant="secondary">已过期</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}


function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString("zh-CN", { hour12: false });
}

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
  const [status, setStatus] = useState(ALL);
  const [method, setMethod] = useState(ALL);
  const [keywordInput, setKeywordInput] = useState("");
  // 搜索即时生效（300ms 防抖），不再要求先打字再点一次「查询」
  const keyword = useDebouncedValue(keywordInput.trim(), 300);
  const [page, setPage] = useState(1);
  const [detailOrderNo, setDetailOrderNo] = useState<string | null>(null);
  const [refundOrderNo, setRefundOrderNo] = useState<string | null>(null);
  const [emailTarget, setEmailTarget] = useState<{ orderNo: string; subject: string; userId?: number | null } | null>(
    null
  );

  const downloadReceipt = useDownloadOrderReceiptMutation(appId);
  const emailReceipt = useEmailOrderReceiptMutation(appId);
  const recipients = useReceiptRecipients(appKey, emailTarget?.userId ?? null);

  async function handleDownloadReceipt(orderNo: string) {
    try {
      await downloadReceipt.mutateAsync({ order_no: orderNo, locale: receiptLocale });
      toast.success("凭证已生成");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "凭证生成失败");
    }
  }

  const filters = useMemo(
    () => ({
      status: status === ALL ? undefined : status,
      payment_method: method === ALL ? undefined : method,
      keyword: keyword || undefined,
      page,
      limit: 20
    }),
    [status, method, keyword, page]
  );

  const ordersQuery = useAdminPaymentOrdersQuery(appId, filters);
  const detailQuery = useAdminPaymentOrderDetailQuery(appId, detailOrderNo);
  const methodsQuery = useAdminPaymentMethodsQuery();

  // 支付方式的展示名与品牌图标同样来自后端渠道目录，与渠道配置面板保持一致
  const methods = useMemo(() => methodsQuery.data ?? [], [methodsQuery.data]);
  const metaByMethod = useMemo(() => new Map(methods.map((m) => [m.method, m] as const)), [methods]);

  const data = ordersQuery.data;
  const items = data?.items || [];
  const totalPages = data?.totalPages || 1;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 w-56 pl-8 pr-7 text-xs"
            placeholder="订单号 / 上游单号 / 商品名"
            value={keywordInput}
            onChange={(e) => { setKeywordInput(e.target.value); setPage(1); }}
          />
          {keywordInput ? (
            <button
              type="button"
              aria-label="清空关键词"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => { setKeywordInput(""); setPage(1); }}
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
        <Select value={status} onValueChange={(v) => { setStatus(v); setPage(1); }}>
          <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部状态</SelectItem>
            {statusOptions.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={method} onValueChange={(v) => { setMethod(v); setPage(1); }}>
          <SelectTrigger className="h-8 w-44 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部支付方式</SelectItem>
            {methods.map((m) => <SelectItem key={m.method} value={m.method}>{m.name}</SelectItem>)}
          </SelectContent>
        </Select>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs tabular-nums text-muted-foreground">共 {data?.total ?? 0} 笔</span>
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => void ordersQuery.refetch()}>
            <RefreshCw className="size-3" />刷新
          </Button>
        </div>
      </div>

      {ordersQuery.isLoading ? (
        <div className="space-y-3">{Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}</div>
      ) : items.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无订单</div>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>订单号</TableHead>
                <TableHead>商品</TableHead>
                <TableHead className="text-right">金额</TableHead>
                <TableHead>支付方式</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead className="text-right">凭证</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((order: AdminPaymentOrder) => (
                <TableRow key={order.id} className="cursor-pointer" onClick={() => setDetailOrderNo(order.order_no)}>
                  <TableCell className="font-mono text-xs">{order.order_no}</TableCell>
                  <TableCell className="max-w-40 truncate text-xs">{order.subject}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{order.amount}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    <span className="flex items-center gap-1.5">
                      <PaymentBrandBadge
                        slug={metaByMethod.get(order.payment_method)?.icon}
                        brandColor={metaByMethod.get(order.payment_method)?.brandColor}
                        size="sm"
                      />
                      {metaByMethod.get(order.payment_method)?.name ?? order.payment_method}
                    </span>
                  </TableCell>
                  <TableCell>{statusBadge(order.status)}</TableCell>
                  <TableCell className="text-xs tabular-nums text-muted-foreground">{order.user_id ?? "—"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(order.createdAt)}</TableCell>
                  {/* 凭证按钮吃掉行点击：点「下载」不该顺带把详情抽屉也拉开 */}
                  <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                    <div className="flex justify-end gap-1">
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-xs"
                        disabled={downloadReceipt.isPending}
                        onClick={() => void handleDownloadReceipt(order.order_no)}
                      >
                        <Download className="size-3" />下载
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-xs"
                        onClick={() => setEmailTarget({ orderNo: order.order_no, subject: order.subject, userId: order.user_id })}
                      >
                        <Mail className="size-3" />寄送
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {totalPages > 1 ? (
        <div className="flex items-center justify-end gap-2">
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
            <ChevronLeft className="size-3" />上一页
          </Button>
          <span className="text-xs tabular-nums text-muted-foreground">{page} / {totalPages}</span>
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
            下一页<ChevronRight className="size-3" />
          </Button>
        </div>
      ) : null}

      <Sheet open={Boolean(detailOrderNo)} onOpenChange={(open) => !open && setDetailOrderNo(null)}>
        <SheetContent side="right" className="w-[92vw] max-w-xl overflow-y-auto">
          <SheetHeader>
            <SheetTitle className="flex items-center justify-between gap-2 text-sm">
              订单详情
              {detailQuery.data?.order.status === "paid" && (
                <Button
                  size="sm"
                  variant="outline"
                  className="h-7 gap-1 text-xs"
                  onClick={() => setRefundOrderNo(detailQuery.data?.order.order_no ?? null)}
                >
                  <Undo2 className="size-3" />退款
                </Button>
              )}
            </SheetTitle>
          </SheetHeader>
          {detailQuery.isLoading ? (
            <div className="space-y-3 pt-4">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-8 w-full rounded-lg" />)}</div>
          ) : detailQuery.data ? (
            <div className="space-y-4 pt-4 text-xs">
              <div className="grid grid-cols-2 gap-x-4 gap-y-2">
                <DetailItem label="订单号" mono value={detailQuery.data.order.order_no} />
                <DetailItem label="上游单号" mono value={detailQuery.data.order.provider_order_no || "—"} />
                <DetailItem label="商品" value={detailQuery.data.order.subject} />
                <DetailItem label="金额" mono value={detailQuery.data.order.amount} />
                <DetailItem
                  label="支付方式"
                  value={metaByMethod.get(detailQuery.data.order.payment_method)?.name ?? detailQuery.data.order.payment_method}
                />
                <DetailItem label="状态" value={detailQuery.data.order.status} />
                <DetailItem label="履约状态" value={detailQuery.data.fulfillment_status === "done" ? `已履约（${formatTime(detailQuery.data.fulfilled_at)}）` : detailQuery.data.fulfillment_status === "none" ? "未履约 / 无需履约" : detailQuery.data.fulfillment_status || "—"} />
                <DetailItem label="已退金额" mono value={detailQuery.data.order.refunded_amount ?? "0.00"} />
                <DetailItem label="退款状态" value={orderRefundStatusLabel(detailQuery.data.order.refund_status)} />
                <DetailItem label="用户 ID" mono value={String(detailQuery.data.order.user_id ?? "—")} />
                <DetailItem label="客户端 IP" mono value={detailQuery.data.order.client_ip || "—"} />
                <DetailItem label="创建时间" value={formatTime(detailQuery.data.order.createdAt)} />
                <DetailItem label="支付时间" value={formatTime(detailQuery.data.order.paid_at)} />
                <DetailItem label="过期时间" value={formatTime(detailQuery.data.order.expire_at)} />
              </div>

              {detailQuery.data.order.metadata && Object.keys(detailQuery.data.order.metadata).length > 0 ? (
                <div className="space-y-1.5">
                  <p className="font-medium text-foreground">订单元数据</p>
                  <pre className="max-h-44 overflow-auto rounded-lg border bg-muted/30 p-3 font-mono text-[11px] leading-relaxed">
                    {JSON.stringify(detailQuery.data.order.metadata, null, 2)}
                  </pre>
                </div>
              ) : null}

              <OrderRefundList appId={appId} orderNo={detailQuery.data.order.order_no} />

              <div className="space-y-1.5">
                <p className="font-medium text-foreground">回调记录（{detailQuery.data.callback_logs?.length ?? 0}）</p>
                {detailQuery.data.callback_logs?.length ? (
                  <div className="space-y-2">
                    {detailQuery.data.callback_logs.map((log) => (
                      <div key={log.id} className="rounded-lg border px-3 py-2">
                        <div className="flex items-center justify-between">
                          <span className="flex items-center gap-1.5">
                            <span className={`inline-block size-1.5 rounded-full ${log.verification_status === "ok" || log.verification_status.includes("SUCCESS") ? "bg-emerald-500" : "bg-red-400"}`} />
                            <span className="font-medium">{log.verification_status}</span>
                          </span>
                          <span className="text-muted-foreground">{formatTime(log.created_at)}</span>
                        </div>
                        {log.message ? <p className="mt-1 text-muted-foreground">{log.message}</p> : null}
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-muted-foreground">暂无回调记录</p>
                )}
              </div>
            </div>
          ) : (
            <p className="pt-4 text-xs text-muted-foreground">加载失败</p>
          )}
        </SheetContent>
      </Sheet>

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
  );
}

/** 订单详情内嵌的退款记录列表 */
function OrderRefundList({ appId, orderNo }: { appId?: number | null; orderNo: string }) {
  const refundsQuery = useAdminOrderRefundsQuery(appId, orderNo);
  const items = refundsQuery.data ?? [];
  if (refundsQuery.isLoading) {
    return <Skeleton className="h-16 w-full rounded-lg" />;
  }
  if (items.length === 0) return null;
  return (
    <div className="space-y-1.5">
      <p className="font-medium text-foreground">退款记录（{items.length}）</p>
      <div className="space-y-2">
        {items.map((refund) => (
          <div key={refund.id} className="space-y-1 rounded-lg border px-3 py-2">
            <div className="flex items-center justify-between gap-2">
              <span className="font-mono text-[11px]">{refund.refund_no}</span>
              <span className="flex items-center gap-1.5">
                {refundStatusBadge(refund.status)}
                {reversalBadge(refund)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-muted-foreground">
              <span className="font-mono tabular-nums">-{refund.amount}</span>
              <span>{formatTime(refund.createdAt)}</span>
            </div>
            {refund.reason && <p className="text-muted-foreground">原因：{refund.reason}</p>}
            {refund.error_message && <p className="text-destructive">{refund.error_message}</p>}
            {refund.reversal_message && <p className="text-amber-600">{refund.reversal_message}</p>}
          </div>
        ))}
      </div>
    </div>
  );
}

/** 订单退款汇总状态 → 中文 */
function orderRefundStatusLabel(status?: string) {
  switch (status) {
    case "full":
      return "已全额退款";
    case "partial":
      return "部分退款";
    default:
      return "未退款";
  }
}

function DetailItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="space-y-0.5">
      <p className="text-muted-foreground">{label}</p>
      <p className={`break-all text-foreground ${mono ? "font-mono" : ""}`}>{value}</p>
    </div>
  );
}
