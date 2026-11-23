"use client";

import { Suspense } from "react";
import { NuqsAdapter } from "nuqs/adapters/next/app";
import { Inbox, Settings2, TrendingUp } from "lucide-react";
import { LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { DeskPage } from "@/components/service-desk/desk-page";
import { TicketAnalytics } from "@/components/tickets/ticket-analytics";

// 意见反馈。反馈就是 kind=feedback 的工单，界面与工单中心共用服务台（components/service-desk）。
//
// 页面状态全在地址栏（nuqs）：
//   /feedback?id=123                    直接打开某条反馈（站内信与通知的落点）
//   /feedback?app=12                    只看某个应用的反馈；不带 app 时我创建的应用排在前面
//   /feedback?own=true                  只看我创建的应用
//   /feedback?view=open&assignee=me     这类链接可以直接发给同事
// 分类、处理组、SLA 与快捷回复在工单中心的「工单配置」里，按应用分开配。

export default function FeedbackPage() {
  return (
    <Suspense fallback={<LoadingState title="加载意见反馈" />}>
      <WidgetBoundary title="意见反馈加载失败">
        <NuqsAdapter>
          <DeskPage
            kind="feedback"
            eyebrow="服务台"
            tabs={[
              { value: "desk", label: "反馈台", icon: Inbox },
              { value: "analytics", label: "统计分析", icon: TrendingUp, content: <TicketAnalytics kind="feedback" /> },
              { value: "settings", label: "分类与快捷回复", icon: Settings2, href: "/tickets?tab=settings" }
            ]}
          />
        </NuqsAdapter>
      </WidgetBoundary>
    </Suspense>
  );
}
