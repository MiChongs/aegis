"use client";

import { useState } from "react";
import { Coins, CreditCard, Mail, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { CurrencySelect } from "@/components/commerce/currency-select";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { PaymentConfigPanel } from "@/components/payment/payment-config-panel";
import {
  useAdminAppCommerceSettingsQuery,
  useUpdateAdminAppCommerceSettingsMutation
} from "@/lib/admin-hooks";
import { usePaymentReceiptCapabilityQuery } from "@/lib/commerce-hooks";
import type { AppCommerceSettings } from "@/lib/api/types";
import {
  FieldGroup,
  NoAppSelected,
  NumberField,
  SectionCard,
  SwitchRow
} from "@/components/apps/app-config-primitives";

/**
 * 应用支付配置 = 渠道市场（PaymentConfigPanel）+ 交易参数。
 *
 * 交易参数的四个字段**必须整体提交**：后端的 PUT 是全量覆盖。
 * 这里此前只发 `integralPerCurrency`，于是每次点「保存」都会把
 * `receiptEmailOnPaid` 静默关掉、把 `receiptLocale` 清空 ——
 * 表现是「凭证自动寄送前几天还好好的，改了下兑换率就不发了」。
 */
export function AppPaymentPanel({ appId, appKey }: { appId?: number | null; appKey?: string | null }) {
  if (!appId || !appKey) return <NoAppSelected icon={<CreditCard className="size-8" />} />;
  return (
    <div className="space-y-6">
      <CommerceSettingsCard appId={appId} appKey={appKey} />
      <PaymentConfigPanel appId={appId} />
    </div>
  );
}

/** 「跟随用户设置」的哨兵值：Select 不接受空字符串作为选项值 */
const AUTO_LOCALE = "__auto__";

type Draft = {
  scope: string;
  integralPerCurrency: string;
  receiptEmailOnPaid: boolean;
  receiptLocale: string;
  walletCurrency: string;
};

function CommerceSettingsCard({ appId, appKey }: { appId: number; appKey: string }) {
  const query = useAdminAppCommerceSettingsQuery(appKey);
  const mutation = useUpdateAdminAppCommerceSettingsMutation(appKey);
  const capabilityQuery = usePaymentReceiptCapabilityQuery(appId);

  // 草稿按 appKey 绑定作用域，无草稿时从服务端值派生（不用 useEffect 同步）
  const [draft, setDraft] = useState<Draft | null>(null);
  const server = query.data;
  const current: Draft =
    draft?.scope === appKey
      ? draft
      : {
          scope: appKey,
          integralPerCurrency: String(server?.integralPerCurrency ?? 100),
          receiptEmailOnPaid: Boolean(server?.receiptEmailOnPaid),
          receiptLocale: server?.receiptLocale?.trim() || "",
          walletCurrency: server?.walletCurrency?.trim() || "CNY"
        };
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });

  const parsedRate = Number(current.integralPerCurrency);
  const rateValid = Number.isFinite(parsedRate) && parsedRate >= 1 && parsedRate <= 1_000_000;
  const currencyValid = /^[A-Za-z]{3}$/.test(current.walletCurrency.trim());
  const valid = rateValid && currencyValid;

  const locales = capabilityQuery.data?.locales ?? [];

  async function handleSave() {
    if (!rateValid) {
      toast.error("积分兑换率必须在 1-1000000 之间");
      return;
    }
    if (!currencyValid) {
      toast.error("钱包币种需为 3 位 ISO 4217 代码");
      return;
    }
    const payload: AppCommerceSettings = {
      integralPerCurrency: Math.round(parsedRate),
      receiptEmailOnPaid: current.receiptEmailOnPaid,
      receiptLocale: current.receiptLocale,
      walletCurrency: current.walletCurrency.trim().toUpperCase()
    };
    try {
      await mutation.mutateAsync(payload);
      toast.success("交易设置已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  return (
    <SectionCard
      icon={<Coins className="size-4" />}
      title="交易设置"
    >
      {query.isLoading ? (
        <Skeleton className="h-64 w-full rounded-xl" />
      ) : (
        <div className="space-y-5">
          <FieldGroup label="积分兑换率">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_1fr] sm:items-start">
              <div className="space-y-2">
                <NumberField
                  label="每单位金额兑换"
                  unit="积分"
                  min={1}
                  max={1000000}
                  value={current.integralPerCurrency}
                  onChange={(value) => patch({ integralPerCurrency: value })}
                  hint={rateValid ? `支付 10 元 → ${Math.round(parsedRate) * 10} 积分` : "取值范围 1-1000000"}
                />
                {/* 常见档位一键填入：绝大多数应用用的就是这四个之一 */}
                <div className="flex flex-wrap gap-1.5">
                  {["10", "100", "500", "1000"].map((preset) => (
                    <Button
                      key={preset}
                      size="sm"
                      variant={current.integralPerCurrency === preset ? "default" : "outline"}
                      className="h-6 px-2 font-mono text-[11px] tabular-nums"
                      onClick={() => patch({ integralPerCurrency: preset })}
                    >
                      {preset}
                    </Button>
                  ))}
                </div>
              </div>
            </div>
          </FieldGroup>

          <FieldGroup label="钱包记账币种">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_1fr] sm:items-start">
              <div className="space-y-1.5">
                <Label className="text-[11px] font-medium">币种</Label>
                <CurrencySelect
                  value={current.walletCurrency}
                  onChange={(code) => patch({ walletCurrency: code })}
                />
                {!currencyValid && (
                  <p className="text-[10px] leading-snug text-muted-foreground">需 3 位 ISO 4217 代码</p>
                )}
              </div>
            </div>
          </FieldGroup>

          <FieldGroup label="凭证寄送">
            <div className="space-y-3">
              <SwitchRow
                icon={<Mail className="size-3.5" />}
                label="支付成功后自动寄送凭证"
                checked={current.receiptEmailOnPaid}
                onChange={(value) => patch({ receiptEmailOnPaid: value })}
              />
              <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_1fr] sm:items-start">
                <div className="space-y-1.5">
                  <Label className="text-[11px] font-medium">凭证语言</Label>
                  <Select
                    value={current.receiptLocale || AUTO_LOCALE}
                    onValueChange={(value) => patch({ receiptLocale: value === AUTO_LOCALE ? "" : value })}
                  >
                    <SelectTrigger className="h-8 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={AUTO_LOCALE}>跟随用户设置</SelectItem>
                      {locales.map((locale) => (
                        <SelectItem key={locale.tag} value={locale.tag}>
                          {locale.nativeName}
                          {locale.available ? "" : "（缺字体，降为英文）"}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </div>
          </FieldGroup>

          <div className="flex justify-end">
            <Button size="sm" disabled={mutation.isPending || !valid} onClick={() => void handleSave()}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中..." : "保存交易设置"}
            </Button>
          </div>
        </div>
      )}
    </SectionCard>
  );
}
