import { apiRequest, buildQuery } from "./client";
import type { AdminSessionRecord, TempPermission, AdminDelegation, OnlineAdmin } from "./types";

// ── 会话管理 ──

export function listAllSessions(token: string, params?: { page?: number; limit?: number }) {
  return apiRequest<{ items: AdminSessionRecord[]; total: number }>(`/api/admin/system/sessions?${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}

export function listAdminSessions(token: string, adminId: number) {
  return apiRequest<AdminSessionRecord[]>(`/api/admin/system/admins/${adminId}/sessions`, { token });
}

export function revokeSession(token: string, sessionId: string) {
  return apiRequest<void>(`/api/admin/system/sessions/${sessionId}/revoke`, { method: "POST", token });
}

export function forceLogoutAdmin(token: string, adminId: number) {
  return apiRequest<{ revoked: number }>(`/api/admin/system/admins/${adminId}/force-logout`, { method: "POST", token, body: JSON.stringify({}) });
}

export function listOnlineAdmins(token: string) {
  return apiRequest<OnlineAdmin[]>("/api/admin/system/admins/online", { token });
}

// ── 临时权限 ──

export function listTempPermissions(token: string, adminId?: number) {
  const q = adminId ? `?adminId=${adminId}` : "";
  return apiRequest<TempPermission[]>(`/api/admin/system/temp-permissions${q}`, { token });
}

export function grantTempPermission(token: string, data: { adminId: number; permission: string; appId?: number | null; reason: string; expiresAt: string }) {
  return apiRequest<TempPermission>("/api/admin/system/temp-permissions", { method: "POST", token, body: JSON.stringify(data) });
}

export function revokeTempPermission(token: string, permId: number) {
  return apiRequest<void>(`/api/admin/system/temp-permissions/${permId}/revoke`, { method: "POST", token });
}

// ── 代理授权 ──

export function listDelegations(token: string, params?: { adminId?: number; role?: string }) {
  const q = new URLSearchParams();
  if (params?.adminId) q.set("adminId", String(params.adminId));
  if (params?.role) q.set("role", params.role);
  const qs = q.toString();
  return apiRequest<AdminDelegation[]>(`/api/admin/system/delegations${qs ? `?${qs}` : ""}`, { token });
}

export function createDelegation(token: string, data: { delegatorId: number; delegateId: number; scope: string; scopeId?: number | null; reason: string; expiresAt: string }) {
  return apiRequest<AdminDelegation>("/api/admin/system/delegations", { method: "POST", token, body: JSON.stringify(data) });
}

export function revokeDelegation(token: string, delegationId: number) {
  return apiRequest<void>(`/api/admin/system/delegations/${delegationId}/revoke`, { method: "POST", token });
}
