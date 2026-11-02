"use client";

import { useState } from "react";
import { BarChart3, Clover, Dices, ReceiptText, ShieldAlert, Ticket } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { LotteryActivitiesPanel } from "@/components/apps/lottery/lottery-activities-panel";
import { LotteryDrawsPanel } from "@/components/apps/lottery/lottery-draws-panel";
import { LotteryFortunePanel } from "@/components/apps/lottery/lottery-fortune-panel";
import { LotteryRiskPanel, type RiskDrawJump } from "@/components/apps/lottery/lottery-risk-panel";
import { LotteryStatsPanel } from "@/components/apps/lottery/lottery-stats-panel";
import { LotteryTicketsPanel } from "@/components/apps/lottery/lottery-tickets-panel";
import type { LotteryDrawStatus } from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/**
 * 抽奖区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 活动 | 有哪些活动、盘上有什么、各自的概率、怎么抽、抽得怎么样；上线 / 暂停 / 结束、编辑、删除 |
 * | 统计 | 一个活动每天抽多少、中多少、次数从哪来、每个奖品出了多少 |
 * | 抽奖记录 | 谁在什么时候抽中了什么、实际发出去了什么、为什么是这个结果；人工发放在这里标记已发放 |
 * | 风控 | 哪条规则拦了多少、怎么处置的；设备 / IP / 网段多账号聚集；被拒绝的请求 |
 * | 运气控制 | 每日运气的差异与今日分布；对指定用户或全站重抽、定档、偏移，预演、撤销与记录 |
 * | 抽奖次数 | 次数从哪来、谁手上有多少（赠送 / 兑换两档）、每一次获得与使用；给人发放或扣减 |
 *
 * 视图状态不进 URL，理由同积分商城：`?tab=` 已经被应用区块占用。
 * 结果只在服务端产生，控制台没有「试抽」入口：抽一次就是真的发了奖。
 */
const VIEWS = [
  { key: "activities", label: "活动", icon: Dices },
  { key: "stats", label: "统计", icon: BarChart3 },
  { key: "draws", label: "抽奖记录", icon: ReceiptText },
  { key: "risk", label: "风控", icon: ShieldAlert },
  { key: "fortune", label: "运气控制", icon: Clover },
  { key: "tickets", label: "抽奖次数", icon: Ticket }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppLotteryPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("activities");
  // 从活动卡片或统计跳过去时带上的活动与筛选条件
  const [activityId, setActivityId] = useState<number | undefined>(undefined);
  const [statusFilter, setStatusFilter] = useState<LotteryDrawStatus | undefined>(undefined);
  const [riskFilter, setRiskFilter] = useState<string | undefined>(undefined);
  const [keyword, setKeyword] = useState("");

  if (!appKey) {
    return <NoAppSelected icon={<Dices className="size-5" />} />;
  }

  const openDraws = (id?: number, status?: LotteryDrawStatus) => {
    setActivityId(id);
    setStatusFilter(status);
    setRiskFilter(undefined);
    setKeyword("");
    setView("draws");
  };

  const openRiskDraws = (jump: RiskDrawJump) => {
    setActivityId(jump.activityId);
    setStatusFilter(undefined);
    setRiskFilter(jump.risk);
    setKeyword(jump.keyword ?? "");
    setView("draws");
  };

  const openStats = (id: number) => {
    setActivityId(id);
    setView("stats");
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

      {view === "activities" ? (
        <LotteryActivitiesPanel
          appKey={appKey}
          onOpenStats={openStats}
          onOpenDraws={(id, pendingOnly) => openDraws(id, pendingOnly ? "pending" : undefined)}
        />
      ) : null}
      {view === "stats" ? (
        <LotteryStatsPanel
          appKey={appKey}
          activityId={activityId}
          onActivityChange={setActivityId}
          onOpenPending={(id) => openDraws(id, "pending")}
        />
      ) : null}
      {view === "fortune" ? (
        <LotteryFortunePanel appKey={appKey} activityId={activityId} onActivityChange={setActivityId} />
      ) : null}
      {view === "tickets" ? <LotteryTicketsPanel appKey={appKey} /> : null}
      {view === "risk" ? (
        <LotteryRiskPanel appKey={appKey} activityId={activityId} onActivityChange={setActivityId} onOpenDraws={openRiskDraws} />
      ) : null}
      {view === "draws" ? (
        <LotteryDrawsPanel
          appKey={appKey}
          activityId={activityId}
          status={statusFilter}
          risk={riskFilter}
          keyword={keyword}
          onActivityIdChange={setActivityId}
          onStatusChange={setStatusFilter}
          onRiskChange={setRiskFilter}
          onKeywordChange={setKeyword}
        />
      ) : null}
    </div>
  );
}
