"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as access from "@/lib/api/access";
import { useAuthStore } from "@/lib/auth-store";

/**
 * 管理员与角色权限的 React Query 封装。
 *
 * 任何一次授权变更都可能改变「谁能做什么」，于是写操作成功后统一刷新这一组键：
 * 角色列表 / 详情、管理员列表 / 详情、旧的角色矩阵与关系图，以及自己的会话快照
 * （改到自己所在的角色时，侧边栏与按钮要跟着变）。
 */

const ACCESS_KEYS = [
  ["access-roles"],
  ["access-role"],
  ["access-admins"],
  ["access-admin"],
  ["admin-accounts"],
  ["admin-roles"],
  ["admin-role-permission-tree"],
  ["role-matrix"],
  ["role-graph"],
  ["admin-session"],
  ["temp-permissions"],
] as const;

function useToken() {
  return useAuthStore((state) => state.accessToken);
}

function useInvalidateAccess() {
  const qc = useQueryClient();
  return () =>
    Promise.all(
      ACCESS_KEYS.map((queryKey) =>
        qc.invalidateQueries({ queryKey: [...queryKey] }),
      ),
    );
}

export function usePermissionCatalogQuery() {
  const token = useToken();
  return useQuery({
    queryKey: ["access-catalog", token],
    queryFn: () => access.getPermissionCatalog(token as string),
    enabled: Boolean(token),
    // 权限树编译进二进制，同一次部署内不会变
    staleTime: 30 * 60 * 1000,
  });
}

export function usePermissionRoutesQuery(code?: string | null) {
  const token = useToken();
  return useQuery({
    queryKey: ["access-permission-routes", code, token],
    queryFn: () => access.getPermissionRoutes(token as string, code as string),
    enabled: Boolean(token && code),
    staleTime: 30 * 60 * 1000,
  });
}

export function useAccessRolesQuery(enabled = true) {
  const token = useToken();
  return useQuery({
    queryKey: ["access-roles", token],
    queryFn: () => access.listAccessRoles(token as string),
    enabled: Boolean(token && enabled),
  });
}

export function useRoleDetailQuery(roleKey?: string | null) {
  const token = useToken();
  return useQuery({
    queryKey: ["access-role", roleKey, token],
    queryFn: () => access.getRoleDetail(token as string, roleKey as string),
    enabled: Boolean(token && roleKey),
  });
}

export function useAccessAdminsQuery(enabled = true) {
  const token = useToken();
  return useQuery({
    queryKey: ["access-admins", token],
    queryFn: () => access.listAccessAdmins(token as string),
    enabled: Boolean(token && enabled),
  });
}

export function useAdminDetailQuery(adminId?: number | null) {
  const token = useToken();
  return useQuery({
    queryKey: ["access-admin", adminId, token],
    queryFn: () => access.getAdminDetail(token as string, adminId as number),
    enabled: Boolean(token && adminId),
  });
}

function useAccessMutation<TInput, TResult>(
  fn: (token: string, input: TInput) => Promise<TResult>,
) {
  const token = useToken();
  const invalidate = useInvalidateAccess();
  return useMutation({
    mutationFn: (input: TInput) => fn(token as string, input),
    onSuccess: () => invalidate(),
  });
}

export function useCreateRoleMutation() {
  return useAccessMutation((token, input: access.RoleInput) =>
    access.createRole(token, input),
  );
}

export function useUpdateRoleMutation() {
  return useAccessMutation(
    (token, { roleKey, ...input }: access.RoleInput & { roleKey: string }) =>
      access.updateRole(token, roleKey, input),
  );
}

export function useDeleteRoleMutation() {
  return useAccessMutation(
    (token, { roleKey, force }: { roleKey: string; force?: boolean }) =>
      access.deleteRole(token, roleKey, force),
  );
}

export function useCopyRoleMutation() {
  return useAccessMutation(
    (
      token,
      {
        source,
        roleKey,
        name,
      }: { source: string; roleKey: string; name: string },
    ) => access.copyRole(token, source, { roleKey, name }),
  );
}

export function useSetRolePermissionsMutation() {
  return useAccessMutation(
    (
      token,
      {
        roleKey,
        allow,
        deny,
      }: { roleKey: string; allow: string[]; deny: string[] },
    ) => access.setRolePermissions(token, roleKey, { allow, deny }),
  );
}

export function useSetRoleStatusMutation() {
  return useAccessMutation(
    (
      token,
      { roleKey, status }: { roleKey: string; status: access.RoleStatus },
    ) => access.setRoleStatus(token, roleKey, status),
  );
}

export function useAddRoleMembersMutation() {
  return useAccessMutation(
    (
      token,
      {
        roleKey,
        adminIds,
        appid,
      }: { roleKey: string; adminIds: number[]; appid?: number | null },
    ) => access.addRoleMembers(token, roleKey, { adminIds, appid }),
  );
}

export function useRemoveRoleMemberMutation() {
  return useAccessMutation(
    (
      token,
      {
        roleKey,
        adminId,
        appid,
      }: { roleKey: string; adminId: number; appid?: number | null },
    ) => access.removeRoleMember(token, roleKey, adminId, appid),
  );
}

export function useCreateAccessAdminMutation() {
  return useAccessMutation((token, input: access.AdminCreateInput) =>
    access.createAccessAdmin(token, input),
  );
}

export function useUpdateAdminAccessMutation() {
  return useAccessMutation(
    (
      token,
      { adminId, ...input }: access.AccessUpdateInput & { adminId: number },
    ) => access.updateAdminAccess(token, adminId, input),
  );
}

export function useUpdateAdminStatusMutation() {
  return useAccessMutation(
    (
      token,
      { adminId, status }: { adminId: number; status: "active" | "disabled" },
    ) => access.updateAdminStatus(token, adminId, status),
  );
}

export function useForceLogoutAdminMutation() {
  return useAccessMutation((token, adminId: number) =>
    access.forceLogout(token, adminId),
  );
}

export function useSetAdminGrantsMutation() {
  return useAccessMutation(
    (
      token,
      { adminId, grants }: { adminId: number; grants: access.AdminGrant[] },
    ) => access.setAdminGrants(token, adminId, grants),
  );
}

export function useGrantTempPermissionMutation() {
  return useAccessMutation(
    (
      token,
      input: {
        adminId: number;
        permission: string;
        appId?: number | null;
        reason: string;
        expiresAt: string;
      },
    ) => access.grantTempPermission(token, input),
  );
}

export function useRevokeTempPermissionMutation() {
  return useAccessMutation((token, permId: number) =>
    access.revokeTempPermission(token, permId),
  );
}

export function useExplainMutation() {
  const token = useToken();
  return useMutation({
    mutationFn: (input: {
      adminId: number;
      permission: string;
      appid?: number | null;
    }) => access.explainAuthorization(token as string, input),
  });
}

export function useReloadPoliciesMutation() {
  return useAccessMutation((token, _: void) => access.reloadPolicies(token));
}
