"use client";

import { Suspense } from "react";
import { NuqsAdapter } from "nuqs/adapters/next/app";
import { LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { FeedbackWorkspace } from "@/components/feedback/feedback-workspace";

// 意见反馈。
//
// 页面状态（选中项、视图、搜索、筛选、排序）由 nuqs 放在地址栏里：
// /feedback?id=123 直接打开某条反馈（站内信与通知的落点），
// /feedback?view=open&assignee=me 这类链接可以直接发给同事。
// NuqsAdapter 只包这一页：其余页面仍是各自读写 searchParams 的旧写法。

export default function FeedbackPage() {
  return (
    <Suspense fallback={<LoadingState title="加载意见反馈" />}>
      <WidgetBoundary title="意见反馈加载失败">
        <NuqsAdapter>
          <FeedbackWorkspace />
        </NuqsAdapter>
      </WidgetBoundary>
    </Suspense>
  );
}
