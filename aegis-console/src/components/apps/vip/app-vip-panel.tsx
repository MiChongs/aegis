"use client";

import { useState } from "react";
import { Crown, Puzzle, Search, Sparkles } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { VipFeaturesPanel } from "@/components/apps/vip/vip-features-panel";
import { VipMemberPanel } from "@/components/apps/vip/vip-member-panel";
import { VipPlansPanel } from "@/components/apps/vip/vip-plans-panel";
import { VipTrialPanel } from "@/components/apps/vip/vip-trial-panel";
import { cn } from "@/lib/utils";

/**
 * 会员区块。
 *
 * 四个视图回答四个不同的问题，刻意不合并成一张长页面：
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 套餐 | 卖什么、试用发什么 |
 * | 功能标识 | 「会员」细分成哪些能力（接入方服务端按它校验） |
 * | 试用 | 领了多少、转化了多少、某个人还能不能领 |
 * | 会员查询 | 某个具体的人现在是什么状态，以及授予 |
 *
 * 这四段的视图状态**不进 URL**：`?tab=` 已经被应用区块占用，
 * 再塞一层会让「分享出去的链接指向哪」变成两个参数的组合问题。
 */
const VIEWS = [
  { key: "plans", label: "套餐", icon: Crown },
  { key: "features", label: "功能标识", icon: Puzzle },
  { key: "trial", label: "试用", icon: Sparkles },
  { key: "member", label: "会员查询", icon: Search }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppVipPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("plans");

  if (!appKey) {
    return <NoAppSelected icon={<Crown className="size-5" />} />;
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

      {view === "plans" ? <VipPlansPanel appKey={appKey} /> : null}
      {view === "features" ? <VipFeaturesPanel appKey={appKey} /> : null}
      {view === "trial" ? <VipTrialPanel appKey={appKey} /> : null}
      {view === "member" ? <VipMemberPanel appKey={appKey} /> : null}
    </div>
  );
}
