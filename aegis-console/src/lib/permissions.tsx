"use client";

import {
  type ReactNode,
  createContext,
  useCallback,
  useContext,
  useMemo,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@/lib/auth-store";
import { usePermissionCatalogQuery } from "@/lib/access-hooks";
import {
  getAccessSnapshot,
  tierOf,
  type AccessSnapshot,
  type AdminTier,
  type PermissionNode,
} from "@/lib/api/access";

/**
 * 控制台的权限上下文。
 *
 * 唯一来源是后端 `/api/admin/auth/me` 下发的**生效权限快照**：服务端用与真实判定同一段代码
 * 展开（含通配、继承、禁止、数据权限、临时权限），前端不再自己维护「角色 → 权限」的副本 ——
 * 那份副本一旦漂移，表现就是「按钮在、点了 403」。
 *
 * 判定口径：
 *   - 超级管理员 / 系统管理员：一切放行；
 *   - 不带应用：平台级、任意应用、或任意一个应用里持有即算（用于导航与页面入口）；
 *   - 带应用：该应用的完整集合（已并入全局授权），没单独列出的应用用「任意应用」集合。
 *
 * 旧的合并权限点（app:read、system:admin:manage …）仍可传入：它等于「名下任意一个细分项」，
 * 映射关系来自权限树。新代码请直接用细分权限点。
 */

type PermissionContextValue = {
  loading: boolean;
  isSuperAdmin: boolean;
  isSystemAdmin: boolean;
  tier: AdminTier;
  roles: string[];
  /** 平台级生效集合 */
  global: Set<string>;
  /** 未单独列出的任意应用里生效的集合 */
  anyApp: Set<string>;
  /** 每个应用的完整集合 */
  apps: Map<number, Set<string>>;
  /** 所有作用域的并集 */
  union: Set<string>;
  /** 旧合并权限点 → 名下细分项 */
  legacy: Map<string, string[]>;
};

const EMPTY = new Set<string>();

const PermissionContext = createContext<PermissionContextValue>({
  loading: true,
  isSuperAdmin: false,
  isSystemAdmin: false,
  tier: "admin",
  roles: [],
  global: EMPTY,
  anyApp: EMPTY,
  apps: new Map(),
  union: EMPTY,
  legacy: new Map(),
});

function collectLegacy(
  nodes: PermissionNode[] | undefined,
  out: Map<string, string[]>,
) {
  for (const node of nodes ?? []) {
    if (node.kind === "action" && node.code && node.legacy) {
      const list = out.get(node.legacy) ?? [];
      list.push(node.code);
      out.set(node.legacy, list);
    }
    collectLegacy(node.children, out);
  }
}

export function PermissionProvider({ children }: { children: ReactNode }) {
  const token = useAuthStore((s) => s.accessToken);
  const operator = useAuthStore((s) => s.operator);
  const snapshotQuery = useQuery({
    queryKey: ["admin-session", token],
    queryFn: () => getAccessSnapshot(token as string),
    enabled: Boolean(token),
    retry: false,
    staleTime: 30 * 1000,
  });
  const catalogQuery = usePermissionCatalogQuery();

  const value = useMemo<PermissionContextValue>(() => {
    const snapshot = snapshotQuery.data as AccessSnapshot | undefined;
    const isSuperAdmin = Boolean(
      snapshot?.isSuperAdmin ?? operator?.isSuperAdmin,
    );
    const tier = snapshot ? tierOf(snapshot) : isSuperAdmin ? "super" : "admin";
    const global = new Set(snapshot?.permissions?.global ?? []);
    const anyApp = new Set(snapshot?.permissions?.anyApp ?? []);
    const apps = new Map<number, Set<string>>();
    const union = new Set<string>([...global, ...anyApp]);
    for (const [id, items] of Object.entries(
      snapshot?.permissions?.apps ?? {},
    )) {
      apps.set(Number(id), new Set(items));
      for (const item of items) union.add(item);
    }
    const legacy = new Map<string, string[]>();
    collectLegacy(catalogQuery.data?.tree, legacy);
    const roleKeys = (snapshot?.assignments ?? operator?.assignments ?? [])
      .map((a) => a.roleKey)
      .filter((key): key is string => Boolean(key));
    return {
      loading: snapshotQuery.isLoading,
      isSuperAdmin: isSuperAdmin || Boolean(snapshot?.permissions?.all),
      isSystemAdmin: tier === "system",
      tier,
      roles: isSuperAdmin ? ["super_admin", ...roleKeys] : roleKeys,
      global,
      anyApp,
      apps,
      union,
      legacy,
    };
  }, [
    snapshotQuery.data,
    snapshotQuery.isLoading,
    catalogQuery.data,
    operator,
  ]);

  return (
    <PermissionContext.Provider value={value}>
      {children}
    </PermissionContext.Provider>
  );
}

// ── 判定 ──

function holds(set: Set<string>, code: string, legacy: Map<string, string[]>) {
  if (set.has(code)) return true;
  if (code.endsWith(":*")) {
    const prefix = code.slice(0, -1);
    for (const item of set) if (item.startsWith(prefix)) return true;
    return false;
  }
  const children = legacy.get(code);
  return Boolean(children?.some((child) => set.has(child)));
}

function evaluate(
  ctx: PermissionContextValue,
  permission: string | string[] | undefined,
  appId?: number | null,
) {
  if (ctx.isSuperAdmin) return true;
  if (!permission || (Array.isArray(permission) && permission.length === 0))
    return true;
  const codes = Array.isArray(permission) ? permission : [permission];
  const set = appId != null ? (ctx.apps.get(appId) ?? ctx.anyApp) : ctx.union;
  // 应用集合已由服务端并入全局授权，这里不再自己做并集
  return codes.some((code) => holds(set, code, ctx.legacy));
}

/**
 * 检查当前管理员是否拥有指定权限（数组表示「任意一项」）。
 *
 * @param permission 权限代码，如 "system:admin:read"
 * @param appId 可选：按某个应用判定
 */
export function usePermission(
  permission?: string | string[],
  appId?: number | null,
): { allowed: boolean; loading: boolean } {
  const ctx = useContext(PermissionContext);
  return useMemo(
    () => ({ allowed: evaluate(ctx, permission, appId), loading: ctx.loading }),
    [ctx, permission, appId],
  );
}

/**
 * 返回一个可复用的判定函数，用于批量过滤（侧边栏导航、按钮组）。
 * 不带应用：任何作用域持有即算。`superAdmin` 为真时只放行超管（及系统管理员）。
 */
export function usePermissionChecker(): (
  permission?: string | string[],
  superAdmin?: boolean,
) => boolean {
  const ctx = useContext(PermissionContext);
  return useCallback(
    (permission?: string | string[], superAdmin?: boolean) => {
      if (ctx.isSuperAdmin) return true;
      if (superAdmin) return false;
      return evaluate(ctx, permission);
    },
    [ctx],
  );
}

/** 按某个应用判定的检查函数（应用详情页、应用用户等页面用）。 */
export function useAppPermissionChecker(
  appId?: number | null,
): (permission?: string | string[]) => boolean {
  const ctx = useContext(PermissionContext);
  return useCallback(
    (permission?: string | string[]) =>
      evaluate(ctx, permission, appId ?? null),
    [ctx, appId],
  );
}

/** 是否为超级管理员（含系统管理员） */
export function useIsSuperAdmin(): boolean {
  return useContext(PermissionContext).isSuperAdmin;
}

/** 是否为系统管理员 */
export function useIsSystemAdmin(): boolean {
  return useContext(PermissionContext).isSystemAdmin;
}

/** 当前身份档位 */
export function useTier(): AdminTier {
  return useContext(PermissionContext).tier;
}

/** 当前管理员的全部角色 key */
export function useRoles(): string[] {
  return useContext(PermissionContext).roles;
}

/** 权限快照是否仍在加载（首屏用来避免「先显示无权限、再闪成有权限」） */
export function usePermissionsLoading(): boolean {
  return useContext(PermissionContext).loading;
}

// ── 条件渲染 ──

type RequirePermissionProps = {
  /** 权限代码；数组表示任意一项 */
  permission?: string | string[];
  /** 可选应用范围 */
  appId?: number | null;
  /** 是否要求超管 */
  superAdmin?: boolean;
  /** 无权限时的替代内容 */
  fallback?: ReactNode;
  children: ReactNode;
};

/**
 * 条件渲染：仅在拥有权限时显示 children。
 *
 * ```tsx
 * <RequirePermission permission="system:admin:read"><AdminPanel /></RequirePermission>
 * <RequirePermission superAdmin><DangerZone /></RequirePermission>
 * ```
 */
export function RequirePermission({
  permission,
  appId,
  superAdmin,
  fallback,
  children,
}: RequirePermissionProps) {
  const { allowed } = usePermission(permission, appId);
  const isSuperAdmin = useIsSuperAdmin();
  if (superAdmin && !isSuperAdmin) return <>{fallback ?? null}</>;
  if (permission && !allowed) return <>{fallback ?? null}</>;
  return <>{children}</>;
}
