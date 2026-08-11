import { apiRequest, buildQuery } from "./client";

// 管理员收件箱 API（/api/admin/notifications/*）。
//
// 与「应用用户站内信」是两套独立收件箱：
// 用户站内信落 notifications（外键 users），管理员通知落 admin_notifications（外键 admin_accounts）。
// 本模块只服务控制台侧；收件人恒为当前会话管理员，接口不接受 adminId 入参。

export type AdminInboxLevel = "info" | "success" | "warning" | "critical";
export type AdminInboxStatus = "unread" | "read";

export type AdminInboxItem = {
  id: number;
  adminId: number;
  type: string;
  title: string;
  content: string;
  level: AdminInboxLevel;
  status: AdminInboxStatus;
  resource?: string;
  resourceId?: string;
  /** 控制台内部相对路径，如 /tickets?id=42 */
  link?: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
  readAt?: string;
};

export type AdminInboxPage = {
  items: AdminInboxItem[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
  /** 未读总数，不受当前筛选影响 —— 角标直接用它 */
  unread: number;
};

export type AdminInboxMutationResult = {
  adminId: number;
  affected: number;
  unread: number;
};

export type AdminInboxParams = {
  status?: AdminInboxStatus | "all";
  type?: string;
  level?: AdminInboxLevel;
  resource?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

export function listAdminInbox(token: string, params: AdminInboxParams = {}) {
  return apiRequest<AdminInboxPage>(`/api/admin/notifications${buildQuery(params)}`, { token });
}

export function getAdminInboxUnread(token: string) {
  return apiRequest<{ unread: number }>("/api/admin/notifications/unread-count", { token });
}

/** ids 为空表示「全部已读」 */
export function markAdminInboxRead(token: string, ids?: number[]) {
  return apiRequest<AdminInboxMutationResult>("/api/admin/notifications/read", {
    method: "POST",
    token,
    body: JSON.stringify({ ids: ids ?? [] })
  });
}

/** ids 为空时：onlyRead=true 清空已读，否则清空全部 */
export function deleteAdminInbox(token: string, ids?: number[], onlyRead = false) {
  return apiRequest<AdminInboxMutationResult>("/api/admin/notifications/delete", {
    method: "POST",
    token,
    body: JSON.stringify({ ids: ids ?? [], onlyRead })
  });
}
