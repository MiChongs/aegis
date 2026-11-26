"use client";

import { useState } from "react";
import { BadgePercent, BarChart3, Boxes, Coins, KeyRound, ReceiptText, Route, Settings2, Store } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { AIGatewayCreditsPanel } from "@/components/apps/ai-gateway/ai-gateway-credits-panel";
import { AIGatewayKeysPanel } from "@/components/apps/ai-gateway/ai-gateway-keys-panel";
import { AIGatewayModelsPanel } from "@/components/apps/ai-gateway/ai-gateway-models-panel";
import { AIGatewayOverviewPanel } from "@/components/apps/ai-gateway/ai-gateway-overview-panel";
import { AIGatewayPricingPanel } from "@/components/apps/ai-gateway/ai-gateway-pricing-panel";
import { AIGatewayRequestsPanel } from "@/components/apps/ai-gateway/ai-gateway-requests-panel";
import { AIGatewaySettingsPanel } from "@/components/apps/ai-gateway/ai-gateway-settings-panel";
import { AIGatewayStorePanel } from "@/components/apps/ai-gateway/ai-gateway-store-panel";
import { cn } from "@/lib/utils";

/**
 * AI 中转区块（docs/ai-gateway.md）。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 调了多少、成功率、扣了多少、从哪些来源扣、哪些模型最费 |
 * | 模型 | 开放哪些模型、对外名与上游名、走哪条通道、各项单价 |
 * | 计费方案 | 时段价格（环形时钟 + 一周热力图）、会员档位优惠、价格模拟（docs/ai-billing.md） |
 * | 额度商店 | 额度包（按用户看到的样子预览）、订单与按剩余比例退款、额度生意统计 |
 * | 调用记录 | 实时概况与上游通道健康；每一次调用的上游链路、用量、耗时、倍率分解、扣费明细（含批次拆分）与内容摘要，实时更新；成功的可以整笔退款 |
 * | 密钥 | 用户自己领的 API 密钥，可停用 |
 * | 额度 | 某个用户现在能用多少、额度批次（可作废）、发放限期额度、调整永久额度、额度流水 |
 * | 设置 | 开关、扣费顺序、全局与等级倍率、免费额度、余额直付、新人礼、额度商店开关与须知、限制 |
 *
 * 上游通道在「AI 服务」区块配置，这里只决定「谁能用、怎么收钱」。
 * 视图状态不进 URL，理由同云存储区块：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "models", label: "模型", icon: Boxes },
  { key: "pricing", label: "计费方案", icon: BadgePercent },
  { key: "store", label: "额度商店", icon: Store },
  { key: "requests", label: "调用记录", icon: ReceiptText },
  { key: "keys", label: "密钥", icon: KeyRound },
  { key: "credits", label: "额度", icon: Coins },
  { key: "settings", label: "设置", icon: Settings2 }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppAIGatewayPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  const [requestFilter, setRequestFilter] = useState<{ userId?: number; keyId?: number; model?: string }>({});

  if (!appKey) {
    return <NoAppSelected icon={<Route className="size-5" />} />;
  }

  const openRequests = (filter: { userId?: number; keyId?: number; model?: string }) => {
    setRequestFilter(filter);
    setView("requests");
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <AIGatewayOverviewPanel
          appKey={appKey}
          onOpenSettings={() => setView("settings")}
          onOpenModels={() => setView("models")}
          onOpenRequests={openRequests}
        />
      ) : null}
      {view === "models" ? <AIGatewayModelsPanel appKey={appKey} /> : null}
      {view === "pricing" ? <AIGatewayPricingPanel appKey={appKey} /> : null}
      {view === "store" ? <AIGatewayStorePanel appKey={appKey} onOpenSettings={() => setView("settings")} /> : null}
      {view === "requests" ? (
        <AIGatewayRequestsPanel key={JSON.stringify(requestFilter)} appKey={appKey} initialFilter={requestFilter} />
      ) : null}
      {view === "keys" ? <AIGatewayKeysPanel appKey={appKey} onOpenRequests={openRequests} /> : null}
      {view === "credits" ? <AIGatewayCreditsPanel appKey={appKey} onOpenRequests={openRequests} /> : null}
      {view === "settings" ? (
        <AIGatewaySettingsPanel appKey={appKey} onOpenPricing={() => setView("pricing")} onOpenStore={() => setView("store")} />
      ) : null}
    </div>
  );
}
