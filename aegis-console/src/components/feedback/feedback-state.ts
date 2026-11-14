"use client";

import { parseAsInteger, parseAsString, parseAsStringLiteral, useQueryStates } from "nuqs";
import type { TicketListParams } from "@/lib/api/tickets";

// 反馈台的页面状态全部放在地址栏里（nuqs）。
//
// 选中哪一条、看哪个视图、搜了什么、筛了什么，都是「这一屏在看什么」的一部分：
// 放进 URL 之后刷新不丢、能发给同事、浏览器前进后退可用，站内信与飞书卡片的
// `/feedback?id=123` 深链也和页面内点击走同一条路径，不需要 useEffect 把 query 同步进本地 state。

export const FEEDBACK_VIEWS = ["active", "open", "processing", "pending_user", "resolved", "closed", "all"] as const;
export type FeedbackView = (typeof FEEDBACK_VIEWS)[number];

export const FEEDBACK_SORTS = ["updated", "created", "priority"] as const;
export type FeedbackSort = (typeof FEEDBACK_SORTS)[number];

export const FEEDBACK_SORT_LABEL: Record<FeedbackSort, string> = {
  updated: "最近更新",
  created: "最新提交",
  priority: "优先级"
};

const ACTIVE_STATUSES = "open,processing,pending_user,pending_third_party";

const PRIORITIES = ["urgent", "high", "normal", "low"] as const;
const ASSIGNEES = ["me", "none"] as const;

const parsers = {
  /** 打开的反馈 */
  id: parseAsInteger,
  view: parseAsStringLiteral(FEEDBACK_VIEWS).withDefault("active"),
  q: parseAsString.withDefault(""),
  sort: parseAsStringLiteral(FEEDBACK_SORTS).withDefault("updated"),
  app: parseAsInteger,
  category: parseAsInteger,
  priority: parseAsStringLiteral(PRIORITIES),
  /** me 我受理的 / none 未指派 */
  assignee: parseAsStringLiteral(ASSIGNEES)
};

export function useFeedbackState() {
  // replace：切视图、打字搜索不该各占一条历史记录；默认值不写进地址栏
  return useQueryStates(parsers, { history: "replace", clearOnDefault: true });
}

export type FeedbackState = ReturnType<typeof useFeedbackState>[0];
export type SetFeedbackState = ReturnType<typeof useFeedbackState>[1];

/** 除搜索与视图外，生效中的筛选条件数 */
export function countFeedbackFilters(state: FeedbackState) {
  return [state.app, state.category, state.priority, state.assignee].filter((value) => value !== null).length;
}

/** 页面状态 → 列表接口参数。keyword 由调用方传防抖后的值 */
export function feedbackListParams(state: FeedbackState, keyword: string): Omit<TicketListParams, "page"> {
  const params: Omit<TicketListParams, "page"> = {
    kind: "feedback",
    keyword: keyword || undefined,
    appid: state.app ?? undefined,
    categoryId: state.category ?? undefined,
    priority: state.priority ?? undefined,
    mine: state.assignee === "me" || undefined,
    unassigned: state.assignee === "none" || undefined,
    sortBy: state.sort === "updated" ? undefined : state.sort,
    limit: 30
  };
  // 「待处理」要显式列出状态：后端不传状态时只排除已关闭与已撤销，已解决的仍会混进来，
  // 列表条数就和页签上的计数对不上
  if (state.view === "all") params.includeClosed = true;
  else if (state.view === "active") params.status = ACTIVE_STATUSES;
  else params.status = state.view;
  return params;
}
