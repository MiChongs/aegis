"use client";

import { useState } from "react";
import { BarChart3, MonitorSmartphone, Settings2, ShieldCheck, Users } from "lucide-react";
import { AdPolicyConfigPanel } from "@/components/apps/ad-policy/ad-policy-config-panel";
import { AdPolicyConsentsPanel } from "@/components/apps/ad-policy/ad-policy-consents-panel";
import { AdPolicyOverviewPanel } from "@/components/apps/ad-policy/ad-policy-overview-panel";
import { AdPolicySplashPanel } from "@/components/apps/ad-policy/ad-policy-splash-panel";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { cn } from "@/lib/utils";

/**
 * 广告服务区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 多少人同意、多少人拒绝、多少人要重新选择，开屏每天展示多少、失败多少 |
 * | 用户 | 某个人现在是什么选择、什么时候在哪里改过 |
 * | 开屏 | 某一次开屏的结局、耗时与平台错误码 |
 * | 策略 | 拒绝时是否只给基础服务、基础服务有哪些、会员是否免除、开屏怎么限频 |
 *
 * 视图状态不进 URL，理由同激励广告区块：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "consents", label: "用户", icon: Users },
  { key: "splash", label: "开屏", icon: MonitorSmartphone },
  { key: "config", label: "策略", icon: Settings2 }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppAdPolicyPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");

  if (!appKey) {
    return <NoAppSelected icon={<ShieldCheck className="size-5" />} />;
  }

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

      {view === "overview" ? <AdPolicyOverviewPanel appKey={appKey} onOpenConfig={() => setView("config")} /> : null}
      {view === "consents" ? <AdPolicyConsentsPanel appKey={appKey} /> : null}
      {view === "splash" ? <AdPolicySplashPanel appKey={appKey} /> : null}
      {view === "config" ? <AdPolicyConfigPanel appKey={appKey} /> : null}
    </div>
  );
}
