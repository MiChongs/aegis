"use client";

import { useState } from "react";
import { Coins, CreditCard, Info, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PaymentConfigPanel } from "@/components/payment/payment-config-panel";
import {
  useAdminAppCommerceSettingsQuery,
  useUpdateAdminAppCommerceSettingsMutation
} from "@/lib/admin-hooks";
import { FieldGroup, NoAppSelected, NumberField, SectionCard } from "@/components/apps/app-config-primitives";

/**
 * 应用支付配置 = 渠道市场（PaymentConfigPanel）+ 交易参数。
 *
 * 交易参数此前完全没有入口：`PaymentService.prepareFulfillmentMetadata` 在
 * `integral_purchase` 订单里读 `settings.integralPerCurrency`，但没有任何管理接口
 * 能写这个键，所有应用都只能吃兜底的 100。
 */
export function AppPaymentPanel({ appId, appKey }: { appId?: number | null; appKey?: string | null }) {
  if (!appId || !appKey) return <NoAppSelected icon={<CreditCard className="size-8" />} />;
  return (
    <div className="space-y-6">
      <CommerceSettingsCard appKey={appKey} />
      <PaymentConfigPanel appId={appId} />
    </div>
  );
}

function CommerceSettingsCard({ appKey }: { appKey: string }) {
  const query = useAdminAppCommerceSettingsQuery(appKey);
  const mutation = useUpdateAdminAppCommerceSettingsMutation(appKey);

  // 草稿按 appKey 绑定作用域，无草稿时从服务端值派生（不用 useEffect 同步）
  const [draft, setDraft] = useState<{ scope: string; value: string } | null>(null);
  const rate = draft?.scope === appKey ? draft.value : String(query.data?.integralPerCurrency ?? 100);
  const setRate = (value: string) => setDraft({ scope: appKey, value });

  const parsed = Number(rate);
  const valid = Number.isFinite(parsed) && parsed >= 1 && parsed <= 1_000_000;

  async function handleSave() {
    if (!valid) {
      toast.error("积分兑换率必须在 1-1000000 之间");
      return;
    }
    try {
      await mutation.mutateAsync({ integralPerCurrency: Math.round(parsed) });
      toast.success("交易设置已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  return (
    <SectionCard
      icon={<Coins className="size-4" />}
      title="交易设置"
      description="积分直购订单的兑换率。发放数量由服务端按此计算，客户端无法指定。"
    >
      {query.isLoading ? (
        <Skeleton className="h-24 w-full rounded-xl" />
      ) : (
        <div className="space-y-4">
          <FieldGroup label="积分兑换率">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_1fr] sm:items-start">
              <NumberField
                label="每单位金额兑换"
                unit="积分"
                min={1}
                max={1000000}
                value={rate}
                onChange={setRate}
                hint={valid ? `支付 10 元 → ${Math.round(parsed) * 10} 积分` : "取值范围 1-1000000"}
              />
              <div className="rounded-xl bg-muted p-3">
                <div className="flex gap-2 text-[11px] leading-relaxed text-muted-foreground">
                  <Info className="mt-0.5 size-3.5 shrink-0" />
                  <p>
                    仅作用于 <code className="font-mono">purpose=integral_purchase</code> 的订单。
                    金额过小导致算出 0 积分时下单会被直接拒绝，不会产生「付了钱没积分」的订单。
                  </p>
                </div>
              </div>
            </div>
          </FieldGroup>
          <div className="flex justify-end">
            <Button size="sm" disabled={mutation.isPending || !valid} onClick={handleSave}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中..." : "保存交易设置"}
            </Button>
          </div>
        </div>
      )}
    </SectionCard>
  );
}
