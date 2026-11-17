"use client";

import { useState } from "react";
import { CheckCheck, PackageCheck } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, type AdminPaymentFulfillResult, type AdminPaymentOrder } from "@/lib/api-client";
import { useFulfillAdminPaymentOrderMutation } from "@/lib/admin-hooks";
import { formatMoney } from "@/components/commerce/commerce-format";
import { needsFulfillment, orderPurposeLabel } from "./payment-order-meta";

/**
 * 「钱收了、东西没发」的订单：补发，或确认已在别处补发（docs/payment-fulfillment.md）。
 *
 * 补发先跑一次预览（dry_run），把将要发放的内容与提示（用户已是永久会员、已用规则补过余额……）
 * 摆给管理员看，确认后才真正发放 —— 老订单多半已经被手工补过，直接发就是重复发放。
 */
export function OrderFulfillmentCard({ appId, order }: { appId?: number | null; order: AdminPaymentOrder }) {
  const mutation = useFulfillAdminPaymentOrderMutation();
  const [preview, setPreview] = useState<AdminPaymentFulfillResult | null>(null);
  const [manualOpen, setManualOpen] = useState(false);
  const [note, setNote] = useState("");

  if (!appId || order.status !== "paid" || !needsFulfillment(order.fulfillment_state)) return null;
  const purpose = orderPurposeLabel(order.purpose);
  const legacy = order.fulfillment_state === "unrecognized";

  const run = async (payload: { action: "fulfill" | "manual"; dry_run?: boolean; note?: string }) => {
    try {
      return await mutation.mutateAsync({ appid: appId, order_no: order.order_no, ...payload });
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "处理失败");
      return null;
    }
  };

  return (
    <div className="rounded-xl border border-red-500/30 bg-red-500/5 px-3.5 py-3 text-sm">
      <p className="font-medium text-foreground">已收款，{purpose ? `${purpose}还没发放` : "权益还没发放"}</p>
      <p className="mt-1 text-xs text-muted-foreground">
        {legacy
          ? "下单时没有写明用途（旧版客户端），系统不会自动补发。先确认用户是否已经手工补过，再选择处理方式。"
          : "下单时写明了用途，系统每 2 分钟自动重试一次；也可以现在补发。"}
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button
          size="sm"
          className="h-8 gap-1.5"
          disabled={mutation.isPending}
          onClick={async () => {
            const result = await run({ action: "fulfill", dry_run: true });
            if (result) setPreview(result);
          }}
        >
          <PackageCheck className="size-3.5" />
          补发
        </Button>
        <Button size="sm" variant="outline" className="h-8 gap-1.5" disabled={mutation.isPending} onClick={() => setManualOpen(true)}>
          <CheckCheck className="size-3.5" />
          已在别处补发
        </Button>
      </div>

      <AlertDialog open={Boolean(preview)} onOpenChange={(open) => !open && setPreview(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>补发{orderPurposeLabel(preview?.purpose) || "权益"}</AlertDialogTitle>
            <AlertDialogDescription>{previewSummary(order, preview)}</AlertDialogDescription>
          </AlertDialogHeader>
          {preview?.warnings?.length ? (
            <ul className="space-y-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-xs text-amber-800 dark:text-amber-300">
              {preview.warnings.map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              onClick={async (event) => {
                event.preventDefault();
                const result = await run({ action: "fulfill" });
                if (result?.applied) {
                  toast.success("已补发");
                  setPreview(null);
                }
              }}
            >
              确认补发
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={manualOpen} onOpenChange={setManualOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>标记为已在别处补发</AlertDialogTitle>
            <AlertDialogDescription>
              适用于已经手工开过会员、调过余额或用物品发放规则补过的订单。标记后平台不再发放，订单移出「待履约」。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Textarea value={note} onChange={(e) => setNote(e.target.value)} placeholder="备注（选填），例如：已手工开通永久会员" rows={3} />
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              onClick={async (event) => {
                event.preventDefault();
                const result = await run({ action: "manual", note: note.trim() });
                if (result?.applied) {
                  toast.success("已标记");
                  setManualOpen(false);
                  setNote("");
                }
              }}
            >
              确认标记
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 预览里一句话说清要发什么 */
function previewSummary(order: AdminPaymentOrder, preview: AdminPaymentFulfillResult | null) {
  const snapshot = preview?.snapshot ?? {};
  const user = order.user_id ? `用户 #${order.user_id}` : "该用户";
  switch (preview?.purpose) {
    case "vip_purchase": {
      const name = String(snapshot.vipPlanName ?? "会员套餐");
      const lifetime = snapshot.vipLifetime === true;
      const days = Number(snapshot.vipDurationDays ?? 0);
      const bonus = Number(snapshot.vipBonusIntegral ?? 0);
      return `将为${user}开通「${name}」${lifetime ? "（永久）" : days > 0 ? `（${days} 天）` : ""}${bonus > 0 ? `，并赠送 ${bonus} 积分` : ""}。`;
    }
    case "wallet_recharge": {
      const bonus = Number(snapshot.walletBonus ?? 0);
      return `将向${user}的余额本金入账 ${formatMoney(order.amount)}${bonus > 0 ? `，另赠送 ${formatMoney(String(bonus))}` : ""}。`;
    }
    case "integral_purchase":
      return `将向${user}发放 ${Number(snapshot.integralAmount ?? 0)} 积分。`;
    default:
      return "将按订单用途发放。";
  }
}
