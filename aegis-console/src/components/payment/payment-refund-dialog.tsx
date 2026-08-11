"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, Loader2, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { useAdminPaymentRefundableQuery, useCreateAdminPaymentRefundMutation } from "@/lib/admin-hooks";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

/**
 * 退款弹窗。
 *
 * 可退额度、是否支持部分退款、渠道是否支持退款，全部以后端 `refundable` 接口为准，
 * 前端不自行推断 —— 并发退款下只有服务端持有的行锁视图是准确的。
 */
export function PaymentRefundDialog({
  open,
  onOpenChange,
  appId,
  orderNo,
  onDone
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  appId?: number | null;
  orderNo?: string | null;
  onDone?: () => void;
}) {
  const refundableQuery = useAdminPaymentRefundableQuery(appId, orderNo, open);
  const createMutation = useCreateAdminPaymentRefundMutation();

  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [reverse, setReverse] = useState(true);

  const info = refundableQuery.data;
  const refundable = useMemo(() => Number(info?.refundable ?? 0), [info]);
  const partialAllowed = info?.partial_allowed !== false;

  // 只支持整单退的渠道：金额锁死为可退全额，输入框禁用
  const effectiveAmount = partialAllowed ? amount.trim() : "";
  const parsedAmount = effectiveAmount === "" ? refundable : Number(effectiveAmount);
  const amountInvalid =
    effectiveAmount !== "" && (!Number.isFinite(parsedAmount) || parsedAmount <= 0 || parsedAmount > refundable);

  const blocked = !info?.supported || refundable <= 0;

  async function handleSubmit() {
    if (!appId || !orderNo) return;
    try {
      const refund = await createMutation.mutateAsync({
        appid: appId,
        order_no: orderNo,
        amount: effectiveAmount || undefined,
        reason: reason.trim() || undefined,
        reverse_fulfillment: reverse
      });
      // 上游可能只是「受理」，如实转述状态，不谎报成功
      if (refund.status === "success") {
        toast.success(`退款成功，退款单 ${refund.refund_no}`);
      } else if (refund.status === "processing") {
        toast.info(`退款已受理，等待上游确认（${refund.refund_no}）`);
      } else {
        toast.warning(`退款未完成：${refund.error_message || refund.status}`);
      }
      onOpenChange(false);
      setAmount("");
      setReason("");
      onDone?.();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "退款发起失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">发起退款</DialogTitle>
        </DialogHeader>

        {refundableQuery.isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-9 w-full rounded-lg" />
            ))}
          </div>
        ) : !info ? (
          <p className="py-6 text-center text-sm text-muted-foreground">无法获取可退款信息</p>
        ) : (
          <div className="space-y-4">
            {/* 额度概览 */}
            <dl className="grid grid-cols-3 gap-2 rounded-xl border p-3 text-center">
              <Stat label="订单金额" value={info.amount} />
              <Stat label="已退金额" value={info.refunded_amount} />
              <Stat label="可退金额" value={info.refundable} highlight />
            </dl>

            {blocked ? (
              <div className="flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2.5">
                <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" />
                <p className="text-xs leading-relaxed">{info.reason || "该订单当前不可退款"}</p>
              </div>
            ) : (
              <>
                <div className="space-y-1">
                  <Label className="text-xs">
                    退款金额{!partialAllowed && <span className="ml-1 text-muted-foreground">（该渠道仅支持整单退）</span>}
                  </Label>
                  <Input
                    className="h-8 text-sm"
                    inputMode="decimal"
                    placeholder={`留空表示全额退款 ${info.refundable}`}
                    value={partialAllowed ? amount : info.refundable}
                    disabled={!partialAllowed}
                    onChange={(e) => setAmount(e.target.value)}
                  />
                  {amountInvalid && (
                    <p className="text-[11px] text-destructive">金额需大于 0 且不超过可退额度 {info.refundable}</p>
                  )}
                </div>

                <div className="space-y-1">
                  <Label className="text-xs">退款原因</Label>
                  <Textarea
                    className="text-xs"
                    rows={2}
                    placeholder="将随退款请求提交给支付渠道，并留存在退款单中"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </div>

                <label className="flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5">
                  <Switch checked={reverse} onCheckedChange={setReverse} className="mt-0.5" />
                  <span className="space-y-0.5">
                    <span className="block text-xs">同时回收已发放的权益</span>
                    <span className="block text-[11px] leading-relaxed text-muted-foreground">
                      扣回充值余额 / 回收积分 / 撤销会员时长。关闭则只退钱不收回，请谨慎。
                      会员与积分仅在全额退款时回收，部分退款需人工处理。
                    </span>
                  </span>
                </label>
              </>
            )}

            <div className="flex justify-end gap-2">
              <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button
                size="sm"
                variant="destructive"
                className="h-8 gap-1 text-xs"
                disabled={blocked || amountInvalid || createMutation.isPending}
                onClick={handleSubmit}
              >
                {createMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Undo2 className="size-3" />}
                确认退款
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Stat({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="space-y-0.5">
      <dt className="text-[10px] text-muted-foreground">{label}</dt>
      <dd className={`font-mono text-sm tabular-nums ${highlight ? "font-semibold text-foreground" : ""}`}>{value}</dd>
    </div>
  );
}
