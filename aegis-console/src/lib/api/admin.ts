import { ApiError, apiRequest, buildQuery } from "./client";
import type {
  AdminAccount,
  AdminAvatarUploadResponse,
  AdminContactInfo,
  AdminProfile,
  AuditPage,
  AuditStats,
  CustomRole,
  AdminDashboard,
  ImpactPreview,
  MessageTemplate,
  RenderResult,
  RoleDefinition,
  RoleGraph,
  RoleMatrix,
  RoleWithPermissions
} from "./types";

export function getAdminAccounts(token: string) {
  return apiRequest<AdminAccount[]>("/api/admin/system/admins", { token });
}

/**
 * 控制台首页工作台数据（待办、告警、近期审计、全站指标）。
 *
 * 后端 `/api/admin/dashboard` 不挂权限点，任意已登录管理员可读；
 * `components/dashboard/*` 的三个面板与问候语都依赖它。
 */
export function getAdminDashboard(token: string) {
  return apiRequest<AdminDashboard>("/api/admin/dashboard", { token });
}

export function getAdminProfile(token: string) {
  return apiRequest<AdminProfile>("/api/admin/profile", { token });
}

export function updateAdminProfile(
  token: string,
  payload: {
    displayName?: string;
    email?: string;
    avatar?: string;
    phone?: string;
    birthday?: string;
    bio?: string;
    contacts?: AdminContactInfo[];
  }
) {
  return apiRequest<AdminProfile>("/api/admin/profile", {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function uploadAdminAvatar(
  token: string,
  payload: {
    file: File;
    configName?: string;
  }
) {
  const endpoints = [
    "/api/admin/profile/avatar",
    "/api/admin/profile/upload-avatar",
    "/api/admin/avatar/upload",
    "/api/admin/avatar"
  ];

  const createBody = () => {
    const body = new FormData();
    body.append("file", payload.file);

    if (payload.configName) {
      body.append("config_name", payload.configName);
    }

    return body;
  };

  const tryUpload = async (index: number): Promise<AdminAvatarUploadResponse> => {
    try {
      return await apiRequest<AdminAvatarUploadResponse>(endpoints[index], {
        method: "POST",
        token,
        body: createBody()
      });
    } catch (error) {
      const isLast = index >= endpoints.length - 1;
      if (
        !isLast &&
        error instanceof ApiError &&
        (error.status === 404 || error.code === 40400 || error.code === 50100)
      ) {
        return tryUpload(index + 1);
      }
      throw error;
    }
  };

  return tryUpload(0);
}

export function getAdminRoles(token: string) {
  return apiRequest<RoleDefinition[]>("/api/admin/system/roles", { token });
}

export function getAdminRolePermissionTree(token: string) {
  return apiRequest<RoleWithPermissions[]>("/api/admin/profile/roles/permissions", { token });
}

export function createAdminAccount(
  token: string,
  payload: {
    account: string;
    password: string;
    displayName?: string;
    email?: string;
    isSuperAdmin?: boolean;
    assignments?: Array<{ roleKey: string; appid?: number | null }>;
  }
) {
  return apiRequest<AdminAccount>("/api/admin/system/admins", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminAccountStatus(
  token: string,
  adminId: number | string,
  payload: {
    status: "active" | "disabled";
  }
) {
  return apiRequest<{ id: number; status: string }>(`/api/admin/system/admins/${adminId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminAccountAccess(
  token: string,
  adminId: number | string,
  payload: {
    isSuperAdmin: boolean;
    assignments: Array<{ roleKey: string; appid?: number | null }>;
  }
) {
  return apiRequest<{ id: number }>(`/api/admin/system/admins/${adminId}/access`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

// ── RBAC 可视化编辑器 ──

export function createCustomRole(token: string, payload: {
  roleKey: string; name: string; description?: string;
  level: number; scope: string; baseRole?: string; permissions: string[];
}) {
  return apiRequest<CustomRole>("/api/admin/system/roles", { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateCustomRole(token: string, roleKey: string, payload: {
  name?: string; description?: string; level?: number; permissions: string[];
}) {
  return apiRequest<CustomRole>(`/api/admin/system/roles/${roleKey}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteCustomRole(token: string, roleKey: string, force?: boolean) {
  return apiRequest<void>(`/api/admin/system/roles/${roleKey}${force ? "?force=true" : ""}`, { method: "DELETE", token });
}

export function getRoleMatrix(token: string) {
  return apiRequest<RoleMatrix>("/api/admin/system/roles/matrix", { token });
}

export function getRoleGraph(token: string) {
  return apiRequest<RoleGraph>("/api/admin/system/roles/graph", { token });
}

export function getRoleImpactPreview(token: string, roleKey: string) {
  return apiRequest<ImpactPreview>(`/api/admin/system/roles/${roleKey}/impact`, { token });
}

// ── 消息模板 ──

export function listMessageTemplates(token: string) {
  return apiRequest<MessageTemplate[]>("/api/admin/system/templates", { token });
}
export function getMessageTemplate(token: string, code: string) {
  return apiRequest<MessageTemplate>(`/api/admin/system/templates/${code}`, { token });
}
export function createMessageTemplate(token: string, payload: { code: string; name: string; description?: string; channel: string; subject?: string; bodyHtml?: string; bodyText?: string; variables?: Array<{ key: string; name: string; example?: string }> }) {
  return apiRequest<MessageTemplate>("/api/admin/system/templates", { method: "POST", token, body: JSON.stringify(payload) });
}
export function updateMessageTemplate(token: string, code: string, payload: Record<string, unknown>) {
  return apiRequest<MessageTemplate>(`/api/admin/system/templates/${code}`, { method: "PUT", token, body: JSON.stringify(payload) });
}
export function deleteMessageTemplate(token: string, code: string) {
  return apiRequest<void>(`/api/admin/system/templates/${code}`, { method: "DELETE", token });
}
export function previewMessageTemplate(token: string, code: string, data: Record<string, string>) {
  return apiRequest<RenderResult>(`/api/admin/system/templates/${code}/preview`, { method: "POST", token, body: JSON.stringify({ data }) });
}

// ── 审计日志 ──

export function listAuditLogs(token: string, params: {
  action?: string;
  resource?: string;
  category?: string;
  severity?: string;
  status?: string;
  statusCode?: number;
  adminId?: number;
  ip?: string;
  country?: string;
  requestId?: string;
  traceId?: string;
  keyword?: string;
  startTime?: string;
  endTime?: string;
  page?: number;
  limit?: number;
}) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) { if (v !== undefined && v !== "") q.set(k, String(v)); }
  return apiRequest<AuditPage>(`/api/admin/system/audit-logs?${q.toString()}`, { token });
}
export function getAuditStats(token: string) {
  return apiRequest<AuditStats>("/api/admin/system/audit-logs/stats", { token });
}
