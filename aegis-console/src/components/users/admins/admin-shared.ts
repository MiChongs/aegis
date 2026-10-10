import type {
  AdminAccount,
  AdminAssignment,
  AdminLoginAvailability,
  OnlineAdmin,
  RoleDefinition
} from "@/lib/api/types";

/**
 * 管理员页的数据整形与筛选。
 *
 * 后端 `/admins` 返回 Profile[]（`{ account, assignments, loginAvailability? }`），
 * 这里展平成一行一个管理员；loginAvailability 只有超管会话才会附带。
 */
export type AdminRecord = AdminAccount & {
  assignments: AdminAssignment[];
  loginAvailability?: AdminLoginAvailability;
};

export function flattenAdmins(data: unknown): AdminRecord[] {
  if (!Array.isArray(data)) return [];
  return data.map((item) => {
    if (item && typeof item === "object" && "account" in item && typeof item.account === "object" && item.account !== null) {
      const profile = item as {
        account: AdminAccount;
        assignments?: AdminAssignment[] | null;
        loginAvailability?: AdminLoginAvailability;
      };
      return {
        ...profile.account,
        assignments: profile.assignments ?? [],
        loginAvailability: profile.loginAvailability
      };
    }
    return { ...(item as AdminAccount), assignments: [] };
  });
}

export const AUTH_SOURCE_LABEL: Record<string, string> = {
  password: "本地账号",
  ldap: "LDAP",
  oidc: "OIDC",
  saml: "SAML"
};

export function authSource(admin: Pick<AdminAccount, "authSource">) {
  return (admin.authSource || "password").toLowerCase();
}

export function authSourceLabel(admin: Pick<AdminAccount, "authSource">) {
  const src = authSource(admin);
  return AUTH_SOURCE_LABEL[src] ?? src.toUpperCase();
}

export function isActive(admin: Pick<AdminAccount, "status">) {
  return admin.status !== "disabled";
}

/** 第三方认证源探测失败：账号本身正常，但当前可能登不进来。 */
export function isLoginBroken(admin: AdminRecord) {
  const value = admin.loginAvailability;
  return Boolean(value && value.source && value.source !== "password" && !value.available);
}

export function loginBrokenReason(admin: AdminRecord) {
  const value = admin.loginAvailability;
  if (!value) return "";
  return value.reason?.trim() || `${value.source.toUpperCase()} 认证源不可用`;
}

export function adminName(admin: Pick<AdminAccount, "displayName" | "account">) {
  return admin.displayName || admin.account;
}

export function adminInitials(admin: Pick<AdminAccount, "displayName" | "account">) {
  return String(admin.displayName || admin.account || "AG").trim().slice(0, 2).toUpperCase();
}

/** 角色分配的展示文本：角色名取角色目录，范围是「全局」或应用名。 */
export type AssignmentView = { key: string; roleKey: string; roleName: string; scope: string; global: boolean };

export function describeAssignments(assignments: AdminAssignment[], roles: Map<string, RoleDefinition>): AssignmentView[] {
  return assignments
    .filter((a) => a.roleKey)
    .map((a, index) => {
      const role = roles.get(a.roleKey as string);
      const global = a.appid == null;
      return {
        key: `${a.roleKey}-${a.appid ?? "g"}-${index}`,
        roleKey: a.roleKey as string,
        roleName: role?.name || (a.roleKey as string),
        scope: global ? "全局" : a.appName || `应用 #${a.appid}`,
        global
      };
    });
}

// ── 筛选 ──────────────────────────

/** 概览分段与列表筛选共用一个维度：点概览里的数字就是筛这一类。 */
export type AdminScope = "all" | "active" | "disabled" | "super" | "online" | "broken";
export type AdminSourceFilter = "all" | "password" | "ldap" | "oidc" | "saml";
export type AdminSort = "lastLogin" | "created" | "name";

export type AdminFilters = {
  keyword: string;
  scope: AdminScope;
  source: AdminSourceFilter;
  sort: AdminSort;
};

export const DEFAULT_FILTERS: AdminFilters = { keyword: "", scope: "all", source: "all", sort: "lastLogin" };

export const SORT_LABEL: Record<AdminSort, string> = {
  lastLogin: "最近登录",
  created: "创建时间",
  name: "名称"
};

function timeOf(value?: string | null) {
  if (!value) return 0;
  const t = new Date(value).getTime();
  return Number.isNaN(t) ? 0 : t;
}

export function matchesScope(admin: AdminRecord, scope: AdminScope, online: Map<number, OnlineAdmin>) {
  switch (scope) {
    case "active":
      return isActive(admin);
    case "disabled":
      return !isActive(admin);
    case "super":
      return Boolean(admin.isSuperAdmin);
    case "online":
      return online.has(admin.id);
    case "broken":
      return isLoginBroken(admin);
    default:
      return true;
  }
}

export function filterAdmins(list: AdminRecord[], filters: AdminFilters, online: Map<number, OnlineAdmin>) {
  const q = filters.keyword.trim().toLowerCase();
  const result = list.filter((admin) => {
    if (!matchesScope(admin, filters.scope, online)) return false;
    if (filters.source !== "all" && authSource(admin) !== filters.source) return false;
    if (!q) return true;
    return [admin.account, admin.displayName, admin.email, admin.phone, admin.previousAccount, String(admin.id)]
      .some((v) => v?.toLowerCase().includes(q));
  });
  return result.sort((a, b) => {
    if (filters.sort === "name") return adminName(a).localeCompare(adminName(b), "zh-CN");
    if (filters.sort === "created") return timeOf(b.createdAt) - timeOf(a.createdAt);
    return timeOf(b.lastLoginAt) - timeOf(a.lastLoginAt);
  });
}
