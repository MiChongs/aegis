"use client";

import { type ReactNode, createContext, useCallback, useContext, useMemo } from "react";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminRolePermissionTreeQuery } from "@/lib/admin-hooks";

// ── 权限上下文 ──────────────────────────

type PermissionContextValue = {
  isSuperAdmin: boolean;
  roles: string[];
  permissions: Set<string>;
  loading: boolean;
};

const PermissionContext = createContext<PermissionContextValue>({
  isSuperAdmin: false,
  roles: [],
  permissions: new Set(),
  loading: true
});

// ── Provider ──────────────────────────

export function PermissionProvider({ children }: { children: ReactNode }) {
  const operator = useAuthStore((s) => s.operator);
  const permTreeQuery = useAdminRolePermissionTreeQuery();

  const value = useMemo<PermissionContextValue>(() => {
    const isSuperAdmin = !!operator?.isSuperAdmin;
    const assignments = operator?.assignments || [];
    const roleKeys = assignments.map((a) => a.roleKey).filter(Boolean) as string[];

    // 从权限树中解析当前用户拥有的所有权限代码
    const permissions = new Set<string>();
    if (permTreeQuery.data && roleKeys.length > 0) {
      for (const role of permTreeQuery.data) {
        if (roleKeys.includes(role.key)) {
          for (const group of role.permissionGroups || []) {
            for (const perm of group.permissions || []) {
              if (perm.description === "granted") {
                permissions.add(perm.code);
              }
            }
          }
        }
      }
    }

    return {
      isSuperAdmin,
      roles: isSuperAdmin ? ["super_admin", ...roleKeys] : roleKeys,
      permissions,
      loading: permTreeQuery.isLoading
    };
  }, [operator, permTreeQuery.data, permTreeQuery.isLoading]);

  return <PermissionContext.Provider value={value}>{children}</PermissionContext.Provider>;
}

// ── Hooks ──────────────────────────

/**
 * 检查当前管理员是否拥有指定权限
 *
 * @param permission - 权限代码，如 "system:admin:manage"
 * @param appId - 可选，检查特定应用范围的权限
 * @returns { allowed, loading }
 */
export function usePermission(permission?: string, appId?: number | null): { allowed: boolean; loading: boolean } {
  const ctx = useContext(PermissionContext);

  return useMemo(() => {
    // 超管拥有所有权限
    if (ctx.isSuperAdmin) return { allowed: true, loading: false };

    // 无权限要求则放行
    if (!permission) return { allowed: true, loading: ctx.loading };

    // 检查权限代码
    if (ctx.permissions.has(permission)) {
      // 如果需要检查应用范围
      if (appId != null) {
        const operator = useAuthStore.getState().operator;
        const hasAppScope = (operator?.assignments || []).some(
          (a) => a.appid === appId || a.appid == null // null = 全局范围
        );
        return { allowed: hasAppScope, loading: false };
      }
      return { allowed: true, loading: false };
    }

    return { allowed: false, loading: ctx.loading };
  }, [ctx, permission, appId]);
}

/**
 * 返回一个可复用的鉴权判定函数，用于批量过滤（如侧边栏导航分组）。
 *
 * `usePermission` 是 Hook，无法在列表循环里逐项调用；此处返回纯函数，
 * 可在 `useMemo` 内对任意长度的数组做过滤。
 */
export function usePermissionChecker(): (permission?: string, superAdmin?: boolean) => boolean {
  const ctx = useContext(PermissionContext);

  return useCallback(
    (permission?: string, superAdmin?: boolean) => {
      if (ctx.isSuperAdmin) return true;
      if (superAdmin) return false;
      if (!permission) return true;
      return ctx.permissions.has(permission);
    },
    [ctx]
  );
}

/**
 * 检查是否为超级管理员
 */
export function useIsSuperAdmin(): boolean {
  return useContext(PermissionContext).isSuperAdmin;
}

/**
 * 获取当前管理员的所有角色 key
 */
export function useRoles(): string[] {
  return useContext(PermissionContext).roles;
}

// ── 条件渲染组件 ──────────────────────────

type RequirePermissionProps = {
  /** 权限代码，如 "system:admin:manage" */
  permission?: string;
  /** 可选应用范围 */
  appId?: number | null;
  /** 是否要求超管 */
  superAdmin?: boolean;
  /** 无权限时的替代内容 */
  fallback?: ReactNode;
  children: ReactNode;
};

/**
 * 条件渲染：仅在拥有权限时显示 children
 *
 * 用法：
 * ```tsx
 * <RequirePermission permission="system:admin:manage">
 *   <AdminPanel />
 * </RequirePermission>
 *
 * <RequirePermission superAdmin>
 *   <DangerZone />
 * </RequirePermission>
 * ```
 */
export function RequirePermission({ permission, appId, superAdmin, fallback, children }: RequirePermissionProps) {
  const { allowed } = usePermission(permission);
  const isSuperAdmin = useIsSuperAdmin();

  if (superAdmin && !isSuperAdmin) return <>{fallback ?? null}</>;
  if (permission && !allowed) return <>{fallback ?? null}</>;

  return <>{children}</>;
}
