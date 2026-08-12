"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { BarChart3, Crown, Receipt, Undo2, Wallet } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CommerceOverviewPanel } from "@/components/commerce/commerce-overview-panel";
import {
  CommerceRangePicker,
  commerceRangePreset,
  type CommerceRange
} from "@/components/commerce/commerce-range-picker";
import { ReceiptLocaleSelect } from "@/components/commerce/receipt-locale-select";
import { VipTransactionsPanel } from "@/components/commerce/vip-transactions-panel";
import { WalletTransactionsPanel } from "@/components/commerce/wallet-transactions-panel";
import { PaymentOrdersPanel } from "@/components/payment/payment-orders-panel";
import { PaymentRefundsPanel } from "@/components/payment/payment-refunds-panel";
import { useAdminAppCommerceSettingsQuery, useAdminAppsQuery } from "@/lib/admin-hooks";
import { usePaymentReceiptCapabilityQuery } from "@/lib/commerce-hooks";
import { useAppScopeStore } from "@/lib/app-scope-store";

/**
 * 交易中心。
 *
 * 与 `/apps/{appKey}?tab=payment` 的分工是**配置 vs 运营**：
 * 那边是渠道密钥、限额、积分兑换率这类应用配置；这里是订单、退款、
 * 钱包流水、会员开通这类**已经发生的**资金记录。同一件事只有一个入口。
 *
 * 五个页签合起来才是完整的资金视图，缺一块就会看错：
 *   - 订单只覆盖走支付渠道的钱；
 *   - 余额直购会员、业务消费、管理员调账**不产生订单**，只在钱包流水里；
 *   - 退款是反向资金流，不看它算出来的实收永远偏高。
 *
 * 时间窗与凭证语言提到页面级：它们对每个页签都成立，放到各面板里
 * 会让人在切页签时反复设置同一件事。
 */
const TABS = ["overview", "orders", "refunds", "wallet", "vip"] as const;
type CommerceTab = (typeof TABS)[number];

function resolveTab(value?: string | null): CommerceTab {
  return TABS.includes(value as CommerceTab) ? (value as CommerceTab) : "overview";
}

function CommercePageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = resolveTab(searchParams.get("tab"));

  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const { lastAppKey, setLastAppKey } = useAppScopeStore();
  const [picked, setPicked] = useState<string | null>(lastAppKey);

  // 当前应用是**派生**的，不用 effect 去回填：选中的应用还在列表里就用它，
  // 否则落到第一个。用 effect 同步会在应用列表到位的那一帧多渲染一次，
  // 而这一帧里所有子面板都会拿着空 appKey 发一轮请求。
  const appKey = useMemo(() => {
    if (picked && apps.some((item) => item.appKey === picked)) return picked;
    return apps[0]?.appKey ?? "";
  }, [picked, apps]);

  const currentApp = useMemo(() => apps.find((item) => item.appKey === appKey), [apps, appKey]);
  const appId = currentApp?.id ?? null;

  const [range, setRange] = useState<CommerceRange>(() => commerceRangePreset(29));

  const capabilityQuery = usePaymentReceiptCapabilityQuery(appId);
  const [localeOverride, setLocaleOverride] = useState<string | null>(null);
  const receiptLocale = localeOverride ?? capabilityQuery.data?.defaultLocale ?? "en";
  const receiptLocaleLabel = useMemo(
    () => capabilityQuery.data?.locales?.find((item) => item.tag === receiptLocale)?.nativeName ?? receiptLocale,
    [capabilityQuery.data, receiptLocale]
  );

  // 钱包币种来自应用级交易设置，与凭证上印的是同一个值
  const commerceQuery = useAdminAppCommerceSettingsQuery(appKey || undefined);
  const walletCurrency = commerceQuery.data?.walletCurrency || "CNY";

  function switchApp(value: string) {
    setPicked(value);
    setLastAppKey(value);
  }

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="交易中心" />

      <div className="flex flex-wrap items-center gap-2">
        <Select value={appKey} onValueChange={switchApp}>
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder="选择应用" />
          </SelectTrigger>
          <SelectContent>
            {apps.map((item) => (
              <SelectItem key={item.appKey} value={item.appKey || String(item.id)}>
                {item.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <CommerceRangePicker value={range} onChange={setRange} />
        <div className="ml-auto">
          <ReceiptLocaleSelect appId={appId} value={receiptLocale} onChange={setLocaleOverride} />
        </div>
      </div>

      <Tabs value={tab} onValueChange={(value) => router.replace(`/commerce?tab=${value}`, { scroll: false })}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="overview">
            <BarChart3 className="size-3.5" />
            概览
          </TabsTrigger>
          <TabsTrigger value="orders">
            <Receipt className="size-3.5" />
            订单
          </TabsTrigger>
          <TabsTrigger value="refunds">
            <Undo2 className="size-3.5" />
            退款
          </TabsTrigger>
          <TabsTrigger value="wallet">
            <Wallet className="size-3.5" />
            钱包流水
          </TabsTrigger>
          <TabsTrigger value="vip">
            <Crown className="size-3.5" />
            会员
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="pt-4">
          <CommerceOverviewPanel appKey={appKey} window={range} walletCurrency={walletCurrency} />
        </TabsContent>
        <TabsContent value="orders" className="pt-4">
          <PaymentOrdersPanel
            appId={appId}
            appKey={appKey}
            receiptLocale={receiptLocale}
            receiptLocaleLabel={receiptLocaleLabel}
          />
        </TabsContent>
        <TabsContent value="refunds" className="pt-4">
          <PaymentRefundsPanel appId={appId} />
        </TabsContent>
        <TabsContent value="wallet" className="pt-4">
          <WalletTransactionsPanel
            appKey={appKey}
            window={range}
            receiptLocale={receiptLocale}
            receiptLocaleLabel={receiptLocaleLabel}
            walletCurrency={walletCurrency}
          />
        </TabsContent>
        <TabsContent value="vip" className="pt-4">
          <VipTransactionsPanel appId={appId} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default function CommercePage() {
  return (
    <Suspense fallback={null}>
      <CommercePageInner />
    </Suspense>
  );
}
