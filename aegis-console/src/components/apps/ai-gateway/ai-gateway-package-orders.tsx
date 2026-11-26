"use client";

import { useState } from "react";
import { CircleAlert, Loader2, ReceiptText, RotateCcw, Search, X } from "lucide-react";
import { toast } from "sonner";
import { CopyButton } from "@/components/apps/auth-protocol/auth-protocol-shared";
import {
  LOT_ORIGIN_LABEL,
  LotStatusBadge,
  OrderStatusBadge,
  formatYuan
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  useAIGatewayLotsQuery,
  useAIGatewayPackageOrdersQuery,
  useRefundAIGatewayPackageOrderMutation
} from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayCreditLot, AIGatewayPackage, AIGatewayPackageOrder } from "@/lib/api/ai-gateway";
import { formatDateTime, formatDateTimeMinute, fromLocalDateValue } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const LIMIT = 20;
const ALL = "__all__";
const REFUND_REASONS = ["用户申请退款", "误购", "服务异常补偿"];

/** 额度包订单：筛选、详情与按剩余比例退款。 */
export function PackageOrdersView({ appKey, packages }: { appKey: string; packages: AIGatewayPackage[] }) {
  const [status, setStatus] = useState(ALL);
  const [packageId, setPackageId] = useState(ALL);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [keyword, setKeyword] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<AIGatewayPackageOrder | null>(null);
  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);

  const query = useAIGatewayPackageOrdersQuery(appKey, {
    status: status === ALL ? undefined : status,
    packageId: packageId === ALL ? undefined : Number(packageId),
    userId: user?.id,
    keyword: debouncedKeyword || undefined,
    from: fromLocalDateValue(from, "start"),
    to: fromLocalDateValue(to, "end"),
    page,
    limit: LIMIT
  });
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / LIMIT));
  const filtered = status !== ALL || packageId !== ALL || user || keyword || from || to;
  const reset = (fn: () => void) => {
    fn();
    setPage(1);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-border bg-card px-3 py-2.5">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            placeholder="订单号前缀"
            className="h-8 w-44 pl-8 font-mono text-xs"
            onChange={(event) => reset(() => setKeyword(event.target.value))}
          />
        </div>
        <Select value={status} onValueChange={(value) => reset(() => setStatus(value))}>
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部状态</SelectItem>
            <SelectItem value="paid">已支付</SelectItem>
            <SelectItem value="refunded">已退款</SelectItem>
          </SelectContent>
        </Select>
        <Select value={packageId} onValueChange={(value) => reset(() => setPackageId(value))}>
          <SelectTrigger className="h-8 w-40 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部额度包</SelectItem>
            {packages.map((pkg) => (
              <SelectItem key={pkg.id} value={String(pkg.id)}>
                {pkg.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="w-60">
          <UserPicker appKey={appKey} value={user} onChange={(value) => reset(() => setUser(value))} />
        </div>
        <div className="flex items-center gap-1.5">
          <Input type="date" className="h-8 w-36 font-mono text-xs" value={from} onChange={(event) => reset(() => setFrom(event.target.value))} />
          <span className="text-xs text-muted-foreground">至</span>
          <Input type="date" className="h-8 w-36 font-mono text-xs" value={to} onChange={(event) => reset(() => setTo(event.target.value))} />
        </div>
        {filtered ? (
          <Button
            size="xs"
            variant="ghost"
            onClick={() =>
              reset(() => {
                setStatus(ALL);
                setPackageId(ALL);
                setUser(null);
                setKeyword("");
                setFrom("");
                setTo("");
              })
            }
          >
            <X className="size-3" />
            清除
          </Button>
        ) : null}
        <span className="ml-auto text-[11px] text-muted-foreground tabular-nums">共 {total} 笔</span>
      </div>

      <div className="overflow-hidden rounded-2xl border border-border bg-card">
        {query.isLoading ? (
          <Skeleton className="m-4 h-48 rounded-xl" />
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-14 text-xs text-muted-foreground">
            <ReceiptText className="size-6 opacity-40" />
            {filtered ? "没有符合条件的订单" : "还没有订单"}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>订单号</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>额度包</TableHead>
                <TableHead className="text-right">实付</TableHead>
                <TableHead className="text-right">额度</TableHead>
                <TableHead>到期</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((order) => (
                <TableRow key={order.orderNo} className="cursor-pointer" onClick={() => setSelected(order)}>
                  <TableCell className="font-mono text-xs">{order.orderNo}</TableCell>
                  <TableCell className="max-w-36 truncate text-xs">{order.account || `#${order.userId}`}</TableCell>
                  <TableCell className="text-xs">{order.packageName}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {Number(order.discount) > 0 ? <span className="mr-1.5 text-muted-foreground line-through">{formatYuan(order.listPrice)}</span> : null}
                    {formatYuan(order.price)}
                    {order.discountLabel ? <div className="text-[10px] text-amber-600 dark:text-amber-400">{order.discountLabel}</div> : null}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {formatPoints(order.totalPoints)}
                    {Number(order.bonusPoints) > 0 ? <div className="text-[10px] text-emerald-600 dark:text-emerald-400">含赠 {formatPoints(order.bonusPoints)}</div> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    {order.expiresAt ? formatDateTimeMinute(order.expiresAt) : "永久"}
                  </TableCell>
                  <TableCell>
                    <OrderStatusBadge status={order.status} />
                    {order.status === "refunded" ? <div className="mt-0.5 font-mono text-[10px] text-muted-foreground">退 {formatYuan(order.refundAmount)}</div> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">{formatDateTimeMinute(order.createdAt)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>
      {pages > 1 ? (
        <div className="flex items-center justify-end gap-2 text-xs">
          <Button size="xs" variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
          <span className="tabular-nums text-muted-foreground">{page} / {pages}</span>
          <Button size="xs" variant="outline" disabled={page >= pages} onClick={() => setPage(page + 1)}>下一页</Button>
        </div>
      ) : null}

      <OrderSheet appKey={appKey} order={selected} onClose={() => setSelected(null)} onUpdated={setSelected} />
    </div>
  );
}

function OrderSheet({
  appKey,
  order,
  onClose,
  onUpdated
}: {
  appKey: string;
  order: AIGatewayPackageOrder | null;
  onClose: () => void;
  onUpdated: (order: AIGatewayPackageOrder) => void;
}) {
  return (
    <Sheet open={Boolean(order)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        {order ? <OrderDetail key={order.orderNo} appKey={appKey} order={order} onUpdated={onUpdated} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function OrderDetail({
  appKey,
  order,
  onUpdated
}: {
  appKey: string;
  order: AIGatewayPackageOrder;
  onUpdated: (order: AIGatewayPackageOrder) => void;
}) {
  // 后端没有按编号取单个批次的接口：按用户查购买来的批次再找（一个用户买的额度包批次不会太多）
  const lotsQuery = useAIGatewayLotsQuery(appKey, { userId: order.userId, origin: "purchase", limit: 100 });
  const lot: AIGatewayCreditLot | undefined = lotsQuery.data?.items?.find((item) => item.id === order.lotId);
  const refundMutation = useRefundAIGatewayPackageOrderMutation(appKey);
  const [reason, setReason] = useState("");

  const amount = Number(lot?.amount ?? order.totalPoints) || 0;
  const remaining = Number(lot?.remaining ?? 0) || 0;
  const frozen = Number(lot?.frozen ?? 0) || 0;
  const ratio = amount > 0 ? remaining / amount : 0;
  // refundAmount = floor₂(finalPrice × remaining / amount)
  const estimate = Math.floor(Math.round(Number(order.price) * 100) * ratio + 1e-6) / 100;
  const refundable = order.status === "paid" && lot?.status !== "revoked";

  const refund = async () => {
    try {
      const result = await refundMutation.mutateAsync({ orderNo: order.orderNo, reason: reason.trim() });
      toast.success(`已退款 ${formatYuan(result.refundAmount)}，收回 ${formatPoints(result.refundPoints)} 额度`);
      onUpdated(result);
      setReason("");
    } catch (error) {
      if (error instanceof ApiError && error.code === 40238) {
        toast.error("这笔订单现在不能退：已退过，或批次里的额度正被调用占用（等调用结算后再试）");
        return;
      }
      toast.error(error instanceof ApiError ? error.message : "退款失败");
    }
  };

  return (
    <>
      <SheetHeader className="shrink-0 border-b px-5 py-4">
        <SheetTitle className="flex flex-wrap items-center gap-2 pr-8 font-mono text-sm">
          {order.orderNo}
          <CopyButton value={order.orderNo} />
          <OrderStatusBadge status={order.status} />
        </SheetTitle>
        <SheetDescription>{order.packageName} · {order.account || `用户 #${order.userId}`}</SheetDescription>
      </SheetHeader>
      <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-4">
        <div className="grid grid-cols-3 gap-2">
          <Metric label="实付" value={formatYuan(order.price)} hint={Number(order.discount) > 0 ? `原价 ${formatYuan(order.listPrice)}，省 ${formatYuan(order.discount)}` : undefined} />
          <Metric label="到手额度" value={formatPoints(order.totalPoints)} hint={Number(order.bonusPoints) > 0 ? `含赠送 ${formatPoints(order.bonusPoints)}` : undefined} />
          <Metric label="有效期" value={order.validityDays > 0 ? `${order.validityDays} 天` : "永久"} hint={order.expiresAt ? `到 ${formatDateTimeMinute(order.expiresAt)}` : undefined} />
        </div>

        <section className="space-y-2">
          <h4 className="text-xs font-medium text-muted-foreground">额度批次 #{order.lotId}</h4>
          {lotsQuery.isLoading ? (
            <Skeleton className="h-16 w-full rounded-xl" />
          ) : !lot ? (
            <p className="rounded-xl border border-dashed px-3 py-3 text-xs text-muted-foreground">没找到这个批次（可能已被清理）</p>
          ) : (
            <div className="space-y-2 rounded-xl border px-3 py-2.5">
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="font-medium">{lot.title}</span>
                <span className="text-muted-foreground">{LOT_ORIGIN_LABEL[lot.origin] ?? lot.origin}</span>
                <LotStatusBadge status={lot.status} />
              </div>
              <div className="flex h-2 overflow-hidden rounded-full bg-muted">
                <span className="bg-muted-foreground/40" style={{ width: `${amount > 0 ? ((amount - remaining - frozen) / amount) * 100 : 0}%` }} />
                <span className="bg-amber-500" style={{ width: `${amount > 0 ? (frozen / amount) * 100 : 0}%` }} />
                <span className="bg-violet-500" style={{ width: `${ratio * 100}%` }} />
              </div>
              <div className="flex flex-wrap gap-x-3 text-[11px] text-muted-foreground tabular-nums">
                <span>剩余 <span className="font-mono text-foreground">{formatPoints(lot.remaining)}</span> / {formatPoints(lot.amount)}（{Math.round(ratio * 1000) / 10}%）</span>
                {frozen > 0 ? <span className="text-amber-600 dark:text-amber-400">暂扣中 {formatPoints(lot.frozen)}</span> : null}
                <span>已用 {formatPoints(lot.used)}</span>
              </div>
            </div>
          )}
        </section>

        <section className="divide-y rounded-xl border text-xs">
          <Row label="用户">{order.account || `#${order.userId}`}</Row>
          <Row label="定价">
            {formatYuan(order.listPrice)}
            {order.discountLabel ? <span className="text-amber-600 dark:text-amber-400"> · {order.discountLabel}</span> : null}
          </Row>
          <Row label="额度构成">{formatPoints(order.points)} + 赠送 {formatPoints(order.bonusPoints)}</Row>
          {order.walletTxnNo ? (
            <Row label="钱包流水">
              <span className="font-mono">{order.walletTxnNo}</span>
            </Row>
          ) : null}
          <Row label="下单时间">{formatDateTime(order.createdAt)}</Row>
          {order.status === "refunded" ? (
            <Row label="退款">
              {formatYuan(order.refundAmount)}，收回 {formatPoints(order.refundPoints)} 额度
              <div className="text-muted-foreground">
                {formatDateTime(order.refundedAt)}
                {order.refundedBy ? ` · ${order.refundedBy}` : ""}
                {order.refundReason ? ` · ${order.refundReason}` : ""}
              </div>
            </Row>
          ) : null}
        </section>

        {refundable ? (
          <section className="space-y-3 rounded-xl border border-destructive/30 p-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h4 className="text-xs font-semibold">按剩余比例退款</h4>
                <p className="text-[11px] text-muted-foreground">退钱 = 实付 × 剩余 / 总量（分位向下取整），批次作废，钱按原拆分先补赠送余额</p>
              </div>
              <div className="text-right">
                <div className="text-[10px] text-muted-foreground">预计退款</div>
                <div className="font-mono text-lg font-semibold tabular-nums">{lot ? formatYuan(estimate) : "—"}</div>
              </div>
            </div>
            {lot ? (
              <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                <span className="font-mono">{formatYuan(order.price)}</span>×
                <span className="font-mono">{formatPoints(remaining)} / {formatPoints(amount)}</span>=
                <span className="font-mono text-foreground">{formatYuan(estimate)}</span>
              </div>
            ) : null}
            {frozen > 0 ? (
              <div className="flex items-start gap-1.5 rounded-lg bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-amber-700 dark:text-amber-300">
                <CircleAlert className="mt-0.5 size-3 shrink-0" />
                批次里有 {formatPoints(frozen)} 额度正被调用暂扣，现在退会被拒绝（40238），等调用结算后再退。
              </div>
            ) : null}
            <div className="flex flex-wrap gap-1.5">
              {REFUND_REASONS.map((item) => (
                <Button key={item} size="xs" variant={reason === item ? "secondary" : "outline"} onClick={() => setReason(item)}>
                  {item}
                </Button>
              ))}
            </div>
            <Textarea rows={2} maxLength={200} value={reason} placeholder="退款原因" className="text-xs" onChange={(event) => setReason(event.target.value)} />
            <div className="flex justify-end">
              <Button size="sm" variant="destructive" disabled={refundMutation.isPending || frozen > 0} onClick={() => void refund()}>
                {refundMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCcw className="size-3.5" />}
                退款{lot ? ` ${formatYuan(estimate)}` : ""}
              </Button>
            </div>
          </section>
        ) : null}
      </div>
    </>
  );
}

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-xl border px-3 py-2">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 font-mono text-sm font-semibold tabular-nums">{value}</div>
      {hint ? <div className={cn("text-[10px] text-muted-foreground")}>{hint}</div> : null}
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-2 px-3 py-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}
