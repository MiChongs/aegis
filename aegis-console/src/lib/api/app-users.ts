/**
 * 应用用户「单个用户」维度的管理端 API。
 *
 * 与 `apps.ts` 的分工：那里是应用维度（列表、统计、策略、应用级审计），
 * 这里是**一个用户**维度（封禁、钱包、他自己的审计流）。
 * 用户详情页需要的接口全在这个文件里，不必再去翻 469 行的 apps.ts。
 *
 * 列表类响应一律可能返回 `items: null`（Go 的 nil slice），
 * 调用方请用 `?? []` 兜底，不要假设它是数组。
 */
import { ApiError, apiRequest, buildQuery, joinApiUrl } from "./client";
import type { AdminAppUserListParams } from "./apps";
import type {
  AdminUserBan,
  AdminUserBanCreateInput,
  AdminUserBanListResult,
  UserLoginAuditList,
  UserSessionAuditList,
  UserWallet,
  UserWalletTransactionList,
  WalletAdjustResult
} from "./types";

const app = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;
const base = (appKey: string, userId: number | string) => `${app(appKey)}/users/${userId}`;

// ── 封禁 ──────────────────────────

export function getAdminUserBans(
  token: string,
  appKey: string,
  userId: number | string,
  params?: { status?: string; page?: number; limit?: number }
) {
  const query = buildQuery({ status: params?.status, page: params?.page, limit: params?.limit });
  return apiRequest<AdminUserBanListResult>(`${base(appKey, userId)}/bans${query}`, { token });
}

/** 当前生效的封禁；没有时后端返回 null，不是 404。 */
export function getAdminUserActiveBan(token: string, appKey: string, userId: number | string) {
  return apiRequest<AdminUserBan | null>(`${base(appKey, userId)}/bans/active`, { token });
}

export function createAdminUserBan(
  token: string,
  appKey: string,
  userId: number | string,
  payload: AdminUserBanCreateInput
) {
  return apiRequest<AdminUserBan>(`${base(appKey, userId)}/bans`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function revokeAdminUserBan(
  token: string,
  appKey: string,
  userId: number | string,
  banId: number,
  payload: { reason?: string }
) {
  return apiRequest<AdminUserBan>(`${base(appKey, userId)}/bans/${banId}/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 钱包 ──────────────────────────

export function getAdminUserWallet(token: string, appKey: string, userId: number | string) {
  return apiRequest<UserWallet>(`${base(appKey, userId)}/wallet`, { token });
}

export function getAdminUserWalletTransactions(
  token: string,
  appKey: string,
  userId: number | string,
  params?: { type?: string; page?: number; limit?: number }
) {
  const query = buildQuery({ type: params?.type, page: params?.page, limit: params?.limit });
  return apiRequest<UserWalletTransactionList>(`${base(appKey, userId)}/wallet/transactions${query}`, {
    token
  });
}

/**
 * 调整余额。amount 是**字符串**且可为负（"-12.50" 即扣款）。
 * 走应用维度的路由（调整目标由 body 里的 userId 指定），不是 /users/:userId 下。
 */
export function adjustAdminUserWallet(
  token: string,
  appKey: string,
  payload: { userId: number; amount: string; reason?: string }
) {
  return apiRequest<WalletAdjustResult>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/wallet/adjust`,
    { method: "POST", token, body: JSON.stringify(payload) }
  );
}

// ── 单用户审计 ──────────────────────────

export function getAdminUserLoginAudits(
  token: string,
  appKey: string,
  userId: number | string,
  params?: { status?: string; page?: number; limit?: number }
) {
  const query = buildQuery({ status: params?.status, page: params?.page, limit: params?.limit });
  return apiRequest<UserLoginAuditList>(`${base(appKey, userId)}/audits/login${query}`, { token });
}

export function getAdminUserSessionAudits(
  token: string,
  appKey: string,
  userId: number | string,
  params?: { eventType?: string; page?: number; limit?: number }
) {
  const query = buildQuery({ eventType: params?.eventType, page: params?.page, limit: params?.limit });
  return apiRequest<UserSessionAuditList>(`${base(appKey, userId)}/audits/sessions${query}`, { token });
}

// ── 批量操作 ──────────────────────────
//
// 三个批量端点后端早就有，此前前端一个都没接（列表连多选都没有）。
// 全部按「选中的 userId 列表」下发，不用「按当前筛选条件批量」那种写法 ——
// 后者在筛选条件与用户看到的列表存在时间差时会误伤（翻页期间有人注册）。

export type BatchResult = {
  requested?: number;
  updated?: number;
  created?: number;
  failed?: number;
  processedUserIds?: number[] | null;
  failedUserIds?: number[] | null;
  [key: string]: unknown;
};

export function batchUpdateAdminAppUserStatus(
  token: string,
  appKey: string,
  payload: {
    userIds: number[];
    enabled?: boolean;
    disabledReason?: string;
    clearDisabledEndTime?: boolean;
    disabledEndTime?: string;
  }
) {
  return apiRequest<BatchResult>(`${app(appKey)}/users/status/batch`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function batchCreateAdminAppUserBan(
  token: string,
  appKey: string,
  payload: { userIds: number[] } & AdminUserBanCreateInput
) {
  return apiRequest<BatchResult>(`${app(appKey)}/users/bans/batch`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function bulkNotifyAdminAppUsers(
  token: string,
  appKey: string,
  payload: {
    userIds: number[];
    type: string;
    title: string;
    content: string;
    level?: string;
  }
) {
  return apiRequest<BatchResult>(`${app(appKey)}/notifications/bulk`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── CSV 导出 ──────────────────────────
//
// 返回的是 CSV 二进制，不能走 apiRequest（它按 JSON 信封解析）；
// 也不能用裸 <a href> —— 令牌只在 Authorization 头里，直链会 401。
// 与 organization.ts 的 downloadXLSX 同一套做法。

export async function downloadAdminAppUsersCsv(
  token: string,
  appKey: string,
  params?: Omit<AdminAppUserListParams, "page" | "limit"> & { limit?: number }
) {
  const response = await fetch(joinApiUrl(`${app(appKey)}/users/export${buildQuery({ ...params })}`), {
    headers: { Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store"
  });
  if (!response.ok) {
    let message = `导出失败（HTTP ${response.status}）`;
    try {
      const body = await response.json();
      if (body?.message) message = body.message;
    } catch {
      // 响应不是 JSON，保留默认提示
    }
    throw new ApiError(message, { status: response.status });
  }

  const blob = await response.blob();
  const disposition = response.headers.get("content-disposition") || "";
  const match = /filename\*?=(?:UTF-8'')?"?([^;"]+)"?/i.exec(disposition);
  const filename = match ? decodeURIComponent(match[1]) : `app_users_${appKey}.csv`;

  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
