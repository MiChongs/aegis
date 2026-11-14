import { apiRequest, buildQuery } from "./client";
import type { AdminAccount, AdminAssignment, TempPermission } from "./types";

/**
 * 管理员分级与权限体系（后端设计见 aegis/docs/admin-permissions.md）。
 *
 * - 身份档位：系统管理员 > 超级管理员 > 管理员。只能管理档位更低的人，谁都不能停用 / 改授权自己。
 * - 权限树：目录 → 菜单 → 操作；落进策略的只有操作节点的代码。
 * - 旧的合并权限点（app:read 等）是兼容组：持有它等于持有它名下的全部细分项。
 */

// ── 身份档位 ──

export type AdminTier = "system" | "super" | "admin";

export const TIER_LABEL: Record<AdminTier, string> = {
  system: "系统管理员",
  super: "超级管理员",
  admin: "管理员",
};

export function tierOf(
  account?: {
    tier?: string;
    isSuperAdmin?: boolean;
    isSystemAdmin?: boolean;
  } | null,
): AdminTier {
  if (!account) return "admin";
  if (
    account.tier === "system" ||
    account.tier === "super" ||
    account.tier === "admin"
  )
    return account.tier;
  if (account.isSystemAdmin) return "system";
  if (account.isSuperAdmin) return "super";
  return "admin";
}

export const TIER_RANK: Record<AdminTier, number> = {
  system: 3,
  super: 2,
  admin: 1,
};

// ── 权限树 ──

export type PermissionNodeKind = "dir" | "menu" | "action";
export type PermissionScope = "platform" | "app" | "both";

export type PermissionNode = {
  key: string;
  code?: string;
  name: string;
  kind: PermissionNodeKind;
  scope: PermissionScope;
  icon?: string;
  description?: string;
  danger?: boolean;
  /** 它所替换的旧合并权限点 */
  legacy?: string;
  /** 覆盖的接口规则数 */
  routes?: number;
  children?: PermissionNode[];
};

export type PermissionCatalog = {
  tree: PermissionNode[];
  /** 旧合并权限点 → 中文说明 */
  legacy: Record<string, string>;
};

export type PermissionRouteRule = {
  methods?: string[] | null;
  pattern: string;
  scope: "platform" | "app";
  note?: string;
};

export type PermissionRoutes = {
  code: string;
  name: string;
  path: string[];
  legacy?: string;
  danger?: boolean;
  routes: PermissionRouteRule[];
};

// ── 角色 ──

export type DataScope = "assigned" | "all" | "created" | "custom";

export const DATA_SCOPE_LABEL: Record<DataScope, string> = {
  assigned: "按授权绑定的应用",
  all: "全部应用",
  created: "本人创建的应用",
  custom: "指定应用",
};

export type RoleStatus = "active" | "disabled";

export type AccessRole = {
  key: string;
  name: string;
  description?: string;
  level: number;
  scope: "global" | "app";
  /** 展开后的生效权限点 */
  permissions: string[];
  isCustom: boolean;
  baseRole?: string;
  createdBy?: number | null;
  status: RoleStatus;
  sortOrder: number;
  dataScope: DataScope;
  dataAppIds: number[];
  allow: string[];
  deny: string[];
  memberCount: number;
  updatedAt?: string | null;
};

export type RoleMember = {
  adminId: number;
  account: string;
  displayName: string;
  avatar?: string;
  status: string;
  tier: AdminTier;
  appid?: number | null;
  appName?: string;
  grantedAt: string;
  lastLoginAt?: string | null;
};

export type RoleDetail = {
  role: AccessRole;
  builtin: boolean;
  overrideAllow: string[];
  overrideDeny: string[];
  effective: string[];
  legacy: string[];
  members: RoleMember[];
  editable: boolean;
  reason?: string;
};

export type RoleInput = {
  roleKey?: string;
  name: string;
  description?: string;
  level: number;
  scope?: "global" | "app";
  baseRole?: string | null;
  permissions?: string[];
  deny?: string[];
  status?: RoleStatus;
  sortOrder?: number;
  dataScope?: DataScope;
  dataAppIds?: number[];
};

// ── 管理员 ──

export type AdminCapabilities = {
  manageReason?: string;
  canStatus: boolean;
  canAccess: boolean;
  canGrant: boolean;
  canSession: boolean;
  canTier: boolean;
  isSelf: boolean;
};

export type AccessAssignment = AdminAssignment & {
  allApps?: boolean;
  dataScope?: DataScope;
};

export type PermissionSnapshot = {
  global: string[];
  apps: Record<string, string[]>;
  anyApp: string[];
  all: boolean;
};

export type AdminGrant = {
  appid?: number | null;
  permission: string;
  effect: "allow" | "deny";
  note?: string;
};

export type AccessAccount = AdminAccount & {
  isSystemAdmin?: boolean;
  tier?: AdminTier;
};

export type AdminListEntry = {
  account: AccessAccount;
  assignments?: AdminAssignment[];
  loginAvailability?: {
    source: string;
    available: boolean;
    reason?: string;
    checkedAt?: string;
  };
  capabilities: AdminCapabilities;
};

export type AdminDetail = {
  account: AccessAccount;
  assignments?: AdminAssignment[];
  resolved: AccessAssignment[];
  permissions: PermissionSnapshot;
  grants: AdminGrant[];
  tempPermissions: TempPermission[];
  capabilities: AdminCapabilities;
};

export type AccessUpdateInput = {
  tier: AdminTier;
  assignments: Array<{ roleKey: string; appid?: number | null }>;
};

export type AdminCreateInput = AccessUpdateInput & {
  account: string;
  password: string;
  displayName?: string;
  email?: string;
};

export type AuthorizationExplain = {
  adminId: number;
  permission: string;
  domain: string;
  isSuperAdmin: boolean;
  subjects: string[];
  decision: {
    allowed: boolean;
    effect: "allow" | "deny" | "none";
    subject?: string;
    rule?: string[];
  };
  tempPermission: boolean;
  allowed: boolean;
  summary: string;
};

/** /api/admin/auth/me 的回包 */
export type AccessSnapshot = {
  adminId: number;
  account: string;
  displayName: string;
  isSuperAdmin: boolean;
  isSystemAdmin?: boolean;
  tier?: AdminTier;
  assignments?: AccessAssignment[];
  permissions: PermissionSnapshot;
  selfService?: {
    canCreateApp: boolean;
    privileged: boolean;
    appQuota: number;
    appsCreated: number;
    reason?: string;
  };
};

// ── 接口 ──

const SYS = "/api/admin/system";
const json = (body: unknown) => JSON.stringify(body);

export function getAccessSnapshot(token: string) {
  return apiRequest<AccessSnapshot>("/api/admin/auth/me", { token });
}

export function getPermissionCatalog(token: string) {
  return apiRequest<PermissionCatalog>(`${SYS}/authz/catalog`, { token });
}

export function getPermissionRoutes(token: string, code: string) {
  return apiRequest<PermissionRoutes>(
    `${SYS}/authz/permission-routes${buildQuery({ code })}`,
    { token },
  );
}

export function listAccessRoles(token: string) {
  // 走 /profile/roles：任意已登录管理员可读，授权编辑者不必另持「角色权限：查看」
  return apiRequest<AccessRole[]>("/api/admin/profile/roles", { token });
}

export function getRoleDetail(token: string, roleKey: string) {
  return apiRequest<RoleDetail>(`${SYS}/roles/${encodeURIComponent(roleKey)}`, {
    token,
  });
}

export function createRole(token: string, input: RoleInput) {
  return apiRequest<AccessRole>(`${SYS}/roles`, {
    method: "POST",
    token,
    body: json(input),
  });
}

export function updateRole(token: string, roleKey: string, input: RoleInput) {
  return apiRequest<AccessRole>(`${SYS}/roles/${encodeURIComponent(roleKey)}`, {
    method: "PUT",
    token,
    body: json(input),
  });
}

export function deleteRole(token: string, roleKey: string, force = false) {
  return apiRequest<void>(
    `${SYS}/roles/${encodeURIComponent(roleKey)}${force ? "?force=true" : ""}`,
    { method: "DELETE", token },
  );
}

export function copyRole(
  token: string,
  source: string,
  input: { roleKey: string; name: string },
) {
  return apiRequest<AccessRole>(
    `${SYS}/roles/${encodeURIComponent(source)}/copy`,
    { method: "POST", token, body: json(input) },
  );
}

export function setRolePermissions(
  token: string,
  roleKey: string,
  input: { allow: string[]; deny: string[] },
) {
  return apiRequest<RoleDetail>(
    `${SYS}/roles/${encodeURIComponent(roleKey)}/permissions`,
    { method: "PUT", token, body: json(input) },
  );
}

export function setRoleStatus(
  token: string,
  roleKey: string,
  status: RoleStatus,
) {
  return apiRequest<void>(
    `${SYS}/roles/${encodeURIComponent(roleKey)}/status`,
    { method: "PUT", token, body: json({ status }) },
  );
}

export function addRoleMembers(
  token: string,
  roleKey: string,
  input: { adminIds: number[]; appid?: number | null },
) {
  return apiRequest<{ added: number }>(
    `${SYS}/roles/${encodeURIComponent(roleKey)}/members`,
    { method: "POST", token, body: json(input) },
  );
}

export function removeRoleMember(
  token: string,
  roleKey: string,
  adminId: number,
  appid?: number | null,
) {
  return apiRequest<void>(
    `${SYS}/roles/${encodeURIComponent(roleKey)}/members/${adminId}${buildQuery({ appid: appid ?? undefined })}`,
    { method: "DELETE", token },
  );
}

export function listAccessAdmins(token: string) {
  return apiRequest<AdminListEntry[]>(`${SYS}/admins`, { token });
}

export function getAdminDetail(token: string, adminId: number) {
  return apiRequest<AdminDetail>(`${SYS}/admins/${adminId}`, { token });
}

export function createAccessAdmin(token: string, input: AdminCreateInput) {
  return apiRequest<{ account: AccessAccount }>(`${SYS}/admins`, {
    method: "POST",
    token,
    body: json({ ...input, isSuperAdmin: input.tier !== "admin" }),
  });
}

export function updateAdminAccess(
  token: string,
  adminId: number,
  input: AccessUpdateInput,
) {
  return apiRequest<{ id: number }>(`${SYS}/admins/${adminId}/access`, {
    method: "PUT",
    token,
    body: json({ ...input, isSuperAdmin: input.tier !== "admin" }),
  });
}

export function updateAdminStatus(
  token: string,
  adminId: number,
  status: "active" | "disabled",
) {
  return apiRequest<{ id: number; status: string }>(
    `${SYS}/admins/${adminId}/status`,
    { method: "PUT", token, body: json({ status }) },
  );
}

export function forceLogout(token: string, adminId: number) {
  return apiRequest<{ revokedCount: number }>(
    `${SYS}/admins/${adminId}/force-logout`,
    { method: "POST", token, body: json({}) },
  );
}

export function setAdminGrants(
  token: string,
  adminId: number,
  grants: AdminGrant[],
) {
  return apiRequest<{ count: number }>(
    `${SYS}/authz/admins/${adminId}/grants`,
    { method: "PUT", token, body: json({ grants }) },
  );
}

export function explainAuthorization(
  token: string,
  input: { adminId: number; permission: string; appid?: number | null },
) {
  return apiRequest<AuthorizationExplain>(`${SYS}/authz/explain`, {
    method: "POST",
    token,
    body: json(input),
  });
}

export function grantTempPermission(
  token: string,
  input: {
    adminId: number;
    permission: string;
    appId?: number | null;
    reason: string;
    expiresAt: string;
  },
) {
  return apiRequest<TempPermission>(`${SYS}/temp-permissions`, {
    method: "POST",
    token,
    body: json(input),
  });
}

export function revokeTempPermission(token: string, permId: number) {
  return apiRequest<void>(`${SYS}/temp-permissions/${permId}/revoke`, {
    method: "POST",
    token,
  });
}

export function reloadPolicies(token: string) {
  return apiRequest<unknown>(`${SYS}/authz/reload`, { method: "POST", token });
}
