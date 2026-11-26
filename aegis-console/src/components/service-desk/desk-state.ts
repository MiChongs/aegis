"use client";

import { parseAsBoolean, parseAsInteger, parseAsString, parseAsStringLiteral, useQueryStates } from "nuqs";
import type { TicketKind, TicketListParams } from "@/lib/api/tickets";

// 服务台的页面状态全部放在地址栏里（nuqs）。
//
// 选中哪一条、看哪个应用、哪个视图、搜了什么、筛了什么，都是「这一屏在看什么」的一部分：
// 放进 URL 之后刷新不丢、能发给同事、浏览器前进后退可用，站内信与飞书卡片的
// `?id=123` 深链也和页面内点击走同一条路径，不需要 useEffect 把 query 同步进本地 state。
// `app` 与统计分析页签共用：在工作台选了某个应用，切到统计看到的也是它。

// 状态共七个：进行中 open / processing / pending，已解决 resolved，终态 rejected / closed / cancelled。
// 「已关闭」视图把已撤销也算进去（统计接口的 closed 同口径），「不采纳」单列。
export const DESK_VIEWS = ["active", "open", "processing", "pending", "overdue", "resolved", "rejected", "closed", "all"] as const;
export type DeskView = (typeof DESK_VIEWS)[number];

export const DESK_SORTS = ["updated", "created", "priority", "due"] as const;
export type DeskSort = (typeof DESK_SORTS)[number];

export const DESK_SORT_LABEL: Record<DeskSort, string> = {
  updated: "最近更新",
  created: "最新提交",
  priority: "优先级",
  due: "最先到期"
};

export const ACTIVE_STATUSES = "open,processing,pending";

const PRIORITIES = ["urgent", "high", "normal", "low"] as const;
const ASSIGNEES = ["me", "none"] as const;

const parsers = {
  /** 打开的那一条 */
  id: parseAsInteger,
  view: parseAsStringLiteral(DESK_VIEWS).withDefault("active"),
  q: parseAsString.withDefault(""),
  sort: parseAsStringLiteral(DESK_SORTS).withDefault("updated"),
  /** 应用；不选 = 全部应用（我创建的排前面） */
  app: parseAsInteger,
  /** 只看我创建的应用 */
  own: parseAsBoolean.withDefault(false),
  category: parseAsInteger,
  priority: parseAsStringLiteral(PRIORITIES),
  /** me 我受理的 / none 未指派 */
  assignee: parseAsStringLiteral(ASSIGNEES)
};

export function useDeskState() {
  // replace：切视图、打字搜索不该各占一条历史记录；默认值不写进地址栏
  return useQueryStates(parsers, { history: "replace", clearOnDefault: true });
}

export type DeskState = ReturnType<typeof useDeskState>[0];
export type SetDeskState = ReturnType<typeof useDeskState>[1];

/** 除搜索、视图与应用外，生效中的筛选条件数 */
export function countDeskFilters(state: DeskState) {
  return [state.category, state.priority, state.assignee].filter((value) => value !== null).length;
}

/** 页面状态 → 列表接口参数。keyword 由调用方传防抖后的值 */
export function deskListParams(kind: TicketKind, state: DeskState, keyword: string): Omit<TicketListParams, "page"> {
  const params: Omit<TicketListParams, "page"> = {
    kind,
    keyword: keyword || undefined,
    appid: state.app ?? undefined,
    // 没选具体应用时：要么只看我创建的，要么我创建的排在前面
    owned: state.app === null && state.own ? true : undefined,
    ownedFirst: state.app === null && !state.own ? true : undefined,
    categoryId: state.category ?? undefined,
    priority: state.priority ?? undefined,
    mine: state.assignee === "me" || undefined,
    unassigned: state.assignee === "none" || undefined,
    sortBy: state.sort === "updated" ? undefined : state.sort,
    sortDir: state.sort === "due" ? "asc" : undefined,
    limit: 30
  };
  // 「待处理」要显式列出状态：后端不传状态时只排除终态（不采纳 / 已关闭 / 已撤销），已解决的仍会混进来，
  // 列表条数就和页签上的计数对不上
  if (state.view === "all") params.includeClosed = true;
  else if (state.view === "active") params.status = ACTIVE_STATUSES;
  else if (state.view === "overdue") {
    params.status = ACTIVE_STATUSES;
    params.overdueOnly = true;
  } else if (state.view === "closed") params.status = "closed,cancelled";
  else params.status = state.view;
  return params;
}
