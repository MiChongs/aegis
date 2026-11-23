"use client";

import { Suspense } from "react";
import { NuqsAdapter } from "nuqs/adapters/next/app";
import { BellRing, Inbox, Settings2, TrendingUp } from "lucide-react";
import { LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { DeskPage } from "@/components/service-desk/desk-page";
import { TicketAnalytics } from "@/components/tickets/ticket-analytics";
import { TicketSettingsPanel } from "@/components/tickets/ticket-settings-panel";
import { NotifyCenterPanel } from "@/components/tickets/notify-center-panel";

// 工单中心。
//
// 页面状态全在地址栏（nuqs）：
//   /tickets?tab=analytics|settings|notify        页签（工作台是默认值，不写）
//   /tickets?id=123                               直接打开某张工单（站内信与飞书卡片的落点）
//   /tickets?app=12&view=overdue&assignee=me      工作台里看哪个应用、哪个视图
// 旧书签 ?tab=inbox / ?tab=mine 都落到工作台。

export default function TicketsPage() {
  return (
    <Suspense fallback={<LoadingState title="加载工单中心" />}>
      <WidgetBoundary title="工单中心加载失败">
        <NuqsAdapter>
          <DeskPage
            kind="ticket"
            eyebrow="服务台"
            tabs={[
              { value: "desk", label: "工作台", icon: Inbox },
              { value: "analytics", label: "统计分析", icon: TrendingUp, content: <TicketAnalytics kind="ticket" /> },
              { value: "settings", label: "工单配置", icon: Settings2, content: <TicketSettingsPanel /> },
              { value: "notify", label: "通知出口", icon: BellRing, content: <NotifyCenterPanel /> }
            ]}
          />
        </NuqsAdapter>
      </WidgetBoundary>
    </Suspense>
  );
}
