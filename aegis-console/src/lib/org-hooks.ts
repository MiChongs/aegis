"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as api from "@/lib/api/organization";
import { useAdminToken } from "@/lib/admin-hooks";

// 组织域的 React Query hooks。
//
// 失效策略：组织架构的读取面高度交叉（改一个部门会同时影响树、成员列表、
// 概览统计三处），因此写操作一律按「组织」整体失效，不做细粒度 key 维护 ——
// 精确失效在这里省不下几个请求，却很容易漏掉一处让界面显示旧数据。

const orgScope = (orgId?: string | null) => ["org", orgId] as const;

function useInvalidateOrg() {
  const queryClient = useQueryClient();
  return (orgId?: string | null) => {
    void queryClient.invalidateQueries({ queryKey: ["org"] });
    if (orgId) void queryClient.invalidateQueries({ queryKey: orgScope(orgId) });
  };
}

// ── 元数据 ──

export function useOrgMetadataQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["org-metadata", token],
    queryFn: () => api.getOrgMetadata(token as string),
    enabled: Boolean(token),
    // 枚举与权限目录由后端代码定义，一个会话内不会变
    staleTime: 30 * 60 * 1000,
  });
}

// ── 组织 ──

export function useOrganizationsQuery(params?: api.OrgListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["org", "list", params, token],
    queryFn: () => api.listOrganizations(token as string, params),
    enabled: Boolean(token),
  });
}

export function useOrganizationTreeQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["org", "tree", token],
    queryFn: () => api.getOrganizationTree(token as string),
    enabled: Boolean(token),
  });
}

export function useOrganizationQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "detail", token],
    queryFn: () => api.getOrganization(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useOrgOverviewQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "overview", token],
    queryFn: () => api.getOrgOverview(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useCreateOrganizationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: (payload: api.CreateOrgPayload) => api.createOrganization(token as string, payload),
    onSuccess: () => invalidate(),
  });
}

export function useUpdateOrganizationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: Record<string, unknown> }) =>
      api.updateOrganization(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeleteOrganizationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: (orgId: string) => api.deleteOrganization(token as string, orgId),
    onSuccess: () => invalidate(),
  });
}

export function useTransferOrganizationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, newOwnerAdminId }: { orgId: string; newOwnerAdminId: number }) =>
      api.transferOrganization(token as string, orgId, newOwnerAdminId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useOrgActivityQuery(orgId?: string | null, params?: { action?: string; page?: number; limit?: number }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "activity", params, token],
    queryFn: () => api.listOrgActivity(token as string, orgId as string, params),
    enabled: Boolean(token && orgId),
  });
}

// ── 部门 ──

export function useDepartmentTreeQuery(orgId?: string | null, status?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "dept-tree", status, token],
    queryFn: () => api.getDepartmentTree(token as string, orgId as string, status),
    enabled: Boolean(token && orgId),
  });
}

export function useCreateDepartmentMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: api.CreateDeptPayload }) =>
      api.createDepartment(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUpdateDepartmentMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, deptId, payload }: { orgId: string; deptId: string; payload: Record<string, unknown> }) =>
      api.updateDepartment(token as string, orgId, deptId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useMoveDepartmentMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, deptId, parentId, sortOrder }: { orgId: string; deptId: string; parentId: string; sortOrder?: number }) =>
      api.moveDepartment(token as string, orgId, deptId, parentId, sortOrder),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeleteDepartmentMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, deptId, strategy }: { orgId: string; deptId: string; strategy?: string }) =>
      api.deleteDepartment(token as string, orgId, deptId, strategy),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useReorderDepartmentsMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, order }: { orgId: string; order: Record<string, number> }) =>
      api.reorderDepartments(token as string, orgId, order),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

// ── 组织成员 ──

export function useOrgMembersQuery(orgId?: string | null, params?: api.MemberListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "members", params, token],
    queryFn: () => api.listOrgMembers(token as string, orgId as string, params),
    enabled: Boolean(token && orgId),
  });
}

export function useAddOrgMemberMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: api.AddMemberPayload }) =>
      api.addOrgMember(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUpdateOrgMemberMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, adminId, payload }: { orgId: string; adminId: number; payload: Record<string, unknown> }) =>
      api.updateOrgMember(token as string, orgId, adminId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useRemoveOrgMemberMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, adminId }: { orgId: string; adminId: number }) =>
      api.removeOrgMember(token as string, orgId, adminId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useAssignMemberDeptsMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, adminId, deptIds, primaryDeptId, replace }: {
      orgId: string; adminId: number; deptIds: string[]; primaryDeptId?: string; replace?: boolean;
    }) => api.assignMemberDepartments(token as string, orgId, adminId, { deptIds, primaryDeptId, replace }),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useAssignableAdminsQuery(orgId?: string | null, keyword = "") {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "assignable", keyword, token],
    queryFn: () => api.searchAssignableAdmins(token as string, orgId as string, keyword),
    enabled: Boolean(token && orgId),
  });
}

// ── 部门成员 ──

export function useDepartmentMembersQuery(orgId?: string | null, deptId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "dept-members", deptId, token],
    queryFn: () => api.listDepartmentMembers(token as string, orgId as string, deptId as string),
    enabled: Boolean(token && orgId && deptId),
  });
}

export function useSetDeptMemberMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, deptId, adminId, payload }: {
      orgId: string; deptId: string; adminId: number; payload: api.SetDeptMemberPayload;
    }) => api.setDepartmentMember(token as string, orgId, deptId, adminId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useRemoveDeptMemberMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, deptId, adminId }: { orgId: string; deptId: string; adminId: number }) =>
      api.removeDepartmentMember(token as string, orgId, deptId, adminId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useReportingChainQuery(orgId?: string | null, deptId?: string | null, adminId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "reporting-chain", deptId, adminId, token],
    queryFn: () => api.getReportingChain(token as string, orgId as string, deptId as string, adminId as number),
    enabled: Boolean(token && orgId && deptId && adminId),
  });
}

// ── 邀请 ──

export function useInviteMembersMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: api.InvitePayload }) =>
      api.inviteOrgMembers(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useMyInvitationsQuery(role: "sent" | "received", status?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["org-invitations", role, status, token],
    queryFn: () => api.listMyInvitations(token as string, { role, status }),
    enabled: Boolean(token),
  });
}

export function usePendingInvitationCountQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["org-invitations", "count", token],
    queryFn: () => api.countPendingInvitations(token as string),
    enabled: Boolean(token),
  });
}

export function useRespondInvitationMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ inviteId, action }: { inviteId: string; action: "accept" | "reject" | "cancel" }) =>
      api.respondInvitation(token as string, inviteId, action),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["org-invitations"] });
      void queryClient.invalidateQueries({ queryKey: ["org"] });
    },
  });
}

// ── 岗位 ──

export function usePositionsQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "positions", token],
    queryFn: () => api.listPositions(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useCreatePositionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: api.PositionPayload }) =>
      api.createPosition(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUpdatePositionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, posId, payload }: { orgId: string; posId: string; payload: api.PositionPayload }) =>
      api.updatePosition(token as string, orgId, posId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeletePositionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, posId }: { orgId: string; posId: string }) =>
      api.deletePosition(token as string, orgId, posId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

// ── 组织角色 ──

export function useOrgRolesQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "roles", token],
    queryFn: () => api.listOrgRoles(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useCreateOrgRoleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: api.OrgRolePayload }) =>
      api.createOrgRole(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUpdateOrgRoleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, roleId, payload }: { orgId: string; roleId: string; payload: api.OrgRolePayload }) =>
      api.updateOrgRole(token as string, orgId, roleId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeleteOrgRoleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, roleId }: { orgId: string; roleId: string }) =>
      api.deleteOrgRole(token as string, orgId, roleId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useOrgRoleGrantsQuery(orgId?: string | null, roleId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "role-grants", roleId, token],
    queryFn: () => api.listOrgRoleGrants(token as string, orgId as string, roleId as string),
    enabled: Boolean(token && orgId && roleId),
  });
}

export function useGrantOrgRoleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, roleId, adminIds, scopeDeptId }: {
      orgId: string; roleId: string; adminIds: number[]; scopeDeptId?: string;
    }) => api.grantOrgRole(token as string, orgId, roleId, adminIds, scopeDeptId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useRevokeOrgRoleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, roleId, adminId }: { orgId: string; roleId: string; adminId: number }) =>
      api.revokeOrgRole(token as string, orgId, roleId, adminId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

// ── 应用绑定 ──

export function useOrgAppsQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "apps", token],
    queryFn: () => api.listOrgApps(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useBindOrgAppMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, appId, owned }: { orgId: string; appId: number; owned?: boolean }) =>
      api.bindOrgApp(token as string, orgId, appId, owned),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUnbindOrgAppMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, appId }: { orgId: string; appId: number }) =>
      api.unbindOrgApp(token as string, orgId, appId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

// ── 审批 ──

export function useApprovalChainsQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "approval-chains", token],
    queryFn: () => api.listApprovalChains(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useCreateApprovalChainMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: Parameters<typeof api.createApprovalChain>[2] }) =>
      api.createApprovalChain(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUpdateApprovalChainMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, chainId, payload }: { orgId: string; chainId: string; payload: Parameters<typeof api.updateApprovalChain>[3] }) =>
      api.updateApprovalChain(token as string, orgId, chainId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeleteApprovalChainMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, chainId }: { orgId: string; chainId: string }) =>
      api.deleteApprovalChain(token as string, orgId, chainId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useApprovalInstancesQuery(orgId?: string | null, params?: { status?: string; page?: number; limit?: number }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "approvals", params, token],
    queryFn: () => api.listApprovalInstances(token as string, orgId as string, params),
    enabled: Boolean(token && orgId),
  });
}

export function useMyPendingApprovalsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["org-approvals", "pending", token],
    queryFn: () => api.listMyPendingApprovals(token as string),
    enabled: Boolean(token),
  });
}

export function useDecideApprovalMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ instanceId, action, comment }: { instanceId: string; action: "approved" | "rejected"; comment?: string }) =>
      api.decideApproval(token as string, instanceId, action, comment),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["org-approvals"] });
      void queryClient.invalidateQueries({ queryKey: ["org"] });
    },
  });
}

// ── 权限模板 ──

export function useOrgPermTemplatesQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "perm-templates", token],
    queryFn: () => api.listOrgPermTemplates(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useCreatePermTemplateMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: Parameters<typeof api.createOrgPermTemplate>[2] }) =>
      api.createOrgPermTemplate(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeletePermTemplateMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, templateId }: { orgId: string; templateId: string }) =>
      api.deleteOrgPermTemplate(token as string, orgId, templateId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useApplyPermTemplateMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, templateId, adminIds, scopeDeptId, roleName }: {
      orgId: string; templateId: string; adminIds: number[]; scopeDeptId?: string; roleName?: string;
    }) => api.applyPermTemplate(token as string, orgId, templateId, { adminIds, scopeDeptId, roleName }),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

// ── 协作组 ──

export function useCollabGroupsQuery(orgId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...orgScope(orgId), "collab-groups", token],
    queryFn: () => api.listCollabGroups(token as string, orgId as string),
    enabled: Boolean(token && orgId),
  });
}

export function useCreateCollabGroupMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, payload }: { orgId: string; payload: api.CollabGroupPayload }) =>
      api.createCollabGroup(token as string, orgId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useUpdateCollabGroupMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, groupId, payload }: { orgId: string; groupId: string; payload: api.CollabGroupPayload }) =>
      api.updateCollabGroup(token as string, orgId, groupId, payload),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

export function useDeleteCollabGroupMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, groupId }: { orgId: string; groupId: string }) =>
      api.deleteCollabGroup(token as string, orgId, groupId),
    onSuccess: (_data, { orgId }) => invalidate(orgId),
  });
}

// ── 导入导出 ──

export function useOrgExport() {
  const token = useAdminToken();
  return {
    exportOrg: (orgId: string) => api.downloadOrgExport(token as string, orgId),
    downloadTemplate: (orgId: string) => api.downloadImportTemplate(token as string, orgId),
  };
}

export function useImportOrgMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateOrg();
  return useMutation({
    mutationFn: ({ orgId, file, dryRun }: { orgId: string; file: File; dryRun: boolean }) =>
      api.importOrganization(token as string, orgId, file, dryRun),
    // 仅校验不落库，没必要刷缓存
    onSuccess: (_data, { orgId, dryRun }) => {
      if (!dryRun) invalidate(orgId);
    },
  });
}
