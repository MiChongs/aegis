"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, RefreshCw, RotateCw, Search } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { PaymentRefund } from "@/lib/api/types";
import {
  useAdminPaymentMethodsQuery,
  useAdminPaymentRefundsQuery,
  useSyncAdminPaymentRefundMutation
} from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { PaymentBrandBadge } from "./payment-brand-icon";

const ALL = "__all__";

const statusOptions = [
  { value: "pending", label: "待提交" },
  { value: "processing", label: "处理中" },
  { value: "success", label: "已退款" },
  { value: "failed", label: "失败" },
  { value: "closed", label: "已关闭" }
];

export function refundStatusBadge(status: string) {
  switch (status) {
    case "success":
      return <Badge variant="success" size="sm">已退款</Badge>;
    case "processing":
      return <Badge variant="warning" size="sm">处理中</Badge>;
    case "pending":
      return <Badge variant="secondary" size="sm">待提交</Badge>;
    case "failed":
      return <Badge variant="danger" size="sm">失败</Badge>;
    case "closed":
      return <Badge variant="outline" size="sm">已关闭</Badge>;
    default:
      return <Badge variant="outline" size="sm">{status}</Badge>;
  }
}

/**
 * 履约冲正状态徽标。
 *
 * `failed` 是最需要被看见的状态：钱已经退出去了，但权益没收回来，
 * 必须人工介入，因此用 danger 变体而不是默认灰。
 */
export function reversalBadge(refund: PaymentRefund) {
  switch (refund.reversal_status) {
    case "done":
      return <Badge variant="success" size="sm">已回收</Badge>;
    case "skipped":
      return <Badge variant="outline" size="sm" title={refund.reversal_message}>未回收</Badge>;
    case "failed":
      return <Badge variant="danger" size="sm" title={refund.reversal_message}>回收失败</Badge>;
    default:
      return <span className="text-xs text-muted-foreground">—</span>;
  }
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString("zh-CN", { hour12: false });
}

export function PaymentRefundsPanel({ appId }: { appId?: number | null }) {
  const [status, setStatus] = useState(ALL);
  const [method, setMethod] = useState(ALL);
  const [keywordInput, setKeywordInput] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);

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

  const refundsQuery = useAdminPaymentRefundsQuery(appId, filters);
  const methodsQuery = useAdminPaymentMethodsQuery();
  const syncMutation = useSyncAdminPaymentRefundMutation();

  const methods = useMemo(() => methodsQuery.data ?? [], [methodsQuery.data]);
  const metaByMethod = useMemo(() => new Map(methods.map((m) => [m.method, m] as const)), [methods]);

  const data = refundsQuery.data;
  const items = data?.items ?? [];
  const totalPages = data?.totalPages ?? 1;

  function applySearch() {
    setPage(1);
    setKeyword(keywordInput.trim());
  }

  async function handleSync(refundNo: string) {
    if (!appId) return;
    try {
      const refund = await syncMutation.mutateAsync({ appid: appId, refund_no: refundNo });
      toast.success(`已同步：${refund.status}`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "同步失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 w-56 pl-8 text-xs"
            placeholder="退款单号 / 订单号 / 上游单号"
            value={keywordInput}
            onChange={(e) => setKeywordInput(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && applySearch()}
          />
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
        <Button size="sm" variant="outline" className="h-8 text-xs" onClick={applySearch}>查询</Button>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs tabular-nums text-muted-foreground">共 {data?.total ?? 0} 笔</span>
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => void refundsQuery.refetch()}>
            <RefreshCw className="size-3" />刷新
          </Button>
        </div>
      </div>

      {refundsQuery.isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}
        </div>
      ) : items.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无退款单</div>
      ) : (
        <div className="overflow-x-auto rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>退款单号</TableHead>
                <TableHead>关联订单</TableHead>
                <TableHead className="text-right">退款金额</TableHead>
                <TableHead>支付方式</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>权益回收</TableHead>
                <TableHead>操作人</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((refund) => (
                <TableRow key={refund.id}>
                  <TableCell className="font-mono text-xs">{refund.refund_no}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{refund.order_no}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{refund.amount}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    <span className="flex items-center gap-1.5">
                      <PaymentBrandBadge
                        slug={metaByMethod.get(refund.payment_method)?.icon}
                        brandColor={metaByMethod.get(refund.payment_method)?.brandColor}
                        size="sm"
                      />
                      {metaByMethod.get(refund.payment_method)?.name ?? refund.payment_method}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="flex flex-col gap-0.5">
                      {refundStatusBadge(refund.status)}
                      {refund.error_message && (
                        <span className="max-w-48 truncate text-[10px] text-muted-foreground" title={refund.error_message}>
                          {refund.error_message}
                        </span>
                      )}
                    </span>
                  </TableCell>
                  <TableCell>{reversalBadge(refund)}</TableCell>
                  <TableCell className="max-w-32 truncate text-xs text-muted-foreground">{refund.operator || "—"}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(refund.createdAt)}</TableCell>
                  <TableCell>
                    {/* 只有未达终态的退款单需要同步；终态单再查上游没有意义 */}
                    {(refund.status === "processing" || refund.status === "pending") && (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-xs"
                        disabled={syncMutation.isPending}
                        onClick={() => void handleSync(refund.refund_no)}
                      >
                        <RotateCw className="size-3" />同步
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-end gap-2">
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
            <ChevronLeft className="size-3" />上一页
          </Button>
          <span className="text-xs tabular-nums text-muted-foreground">{page} / {totalPages}</span>
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
            下一页<ChevronRight className="size-3" />
          </Button>
        </div>
      )}
    </div>
  );
}
