/**
 * 单个用户的积分 / 经验流水（管理端）。
 *
 * 方向按金额正负划分：`in` 入账、`out` 出账。时间区间 from / to 传 RFC 3339，
 * 左闭右开；控制台按浏览器时区把日期换算成瞬时值再传（见 lib/datetime）。
 */
import { apiRequest, buildQuery } from "./client";
import type { ExperienceLevelMode, PointsAdjustResult } from "./apps";

export type PointKind = "integral" | "experience";

export type PointTransaction = {
  id: number;
  transactionNo: string;
  userId: number;
  appid: number;
  type: string;
  category: string;
  amount: number;
  balanceBefore: number;
  balanceAfter: number;
  levelBefore?: number;
  levelAfter?: number;
  status: string;
  title: string;
  description?: string;
  sourceId?: number;
  sourceType?: string;
  multiplier: number;
  isLevelUp?: boolean;
  clientIp?: string;
  userAgent?: string;
  extraData?: Record<string, unknown> | null;
  createdAt: string;
};

export type PointLedgerPage = {
  items: PointTransaction[] | null;
  total: number;
  page: number;
  limit: number;
  sumIn: number;
  sumOut: number;
  kind: PointKind;
  balance: number;
};

export type PointLedgerParams = {
  direction?: "in" | "out";
  category?: string;
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type PointLedgerCategory = {
  category: string;
  title: string;
  in: number;
  out: number;
  count: number;
  lastAt?: string;
};

export type PointLedgerDay = {
  date: string;
  in: number;
  out: number;
  count: number;
  balance: number;
};

export type PointLedgerSummary = {
  kind: PointKind;
  balance: number;
  totalIn: number;
  totalOut: number;
  count: number;
  adminIn: number;
  adminOut: number;
  firstAt?: string;
  lastAt?: string;
  categories: PointLedgerCategory[] | null;
  days: number;
  timeZone: string;
  balanceBefore: number;
  daily: PointLedgerDay[] | null;
};

const base = (appKey: string, userId: number | string) =>
  `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/points`;

export function getUserPointLedger(
  token: string,
  appKey: string,
  userId: number | string,
  kind: PointKind,
  params?: PointLedgerParams
) {
  const query = buildQuery({ ...params });
  return apiRequest<PointLedgerPage>(`${base(appKey, userId)}/${kind}/transactions${query}`, { token });
}

export function getUserPointSummary(
  token: string,
  appKey: string,
  userId: number | string,
  kind: PointKind,
  params?: { days?: number; tz?: string }
) {
  const query = buildQuery({ days: params?.days, tz: params?.tz });
  return apiRequest<PointLedgerSummary>(`${base(appKey, userId)}/${kind}/summary${query}`, { token });
}

/**
 * 冲正一笔积分或经验流水。经验扣回时 levelMode 决定等级处理（keep 保留现有等级 / recalc 按经验重新判定）。
 */
export function reverseUserPoints(
  token: string,
  appKey: string,
  userId: number | string,
  kind: PointKind,
  transactionNo: string,
  options: { reason?: string; levelMode?: ExperienceLevelMode } = {}
) {
  return apiRequest<PointsAdjustResult>(
    `${base(appKey, userId)}/${kind}/transactions/${encodeURIComponent(transactionNo)}/reverse`,
    {
      method: "POST",
      token,
      body: JSON.stringify({ reason: options.reason || undefined, levelMode: kind === "experience" ? options.levelMode : undefined })
    }
  );
}
