import { ApiError, apiRequest, buildQuery, joinApiUrl } from "./client";
import type {
  ApprovalChain, ApprovalInstance, ApprovalStep,
  CollaborationGroup, Department, DepartmentMember, DepartmentNode,
  OrgAccess, OrgActivityLog, OrgAppBinding, OrgImportResult, OrgInvitation,
  OrgMember, OrgMetadata, OrgOverview, OrgPage, OrgPermissionTemplate,
  OrgRole, OrgRoleGrant, Organization, OrganizationNode, Position, ReportingNode,
} from "./types";

// 组织域 API。所有实体一律用 UUID 定位，且每条子路径都挂在
// /organizations/{orgId} 之下 —— 组织归属由路径携带，后端据此做隔离校验。

const base = "/api/admin/system";
const org = (orgId: string) => `${base}/organizations/${orgId}`;

// ── 元数据 ──

export function getOrgMetadata(token: string) {
  return apiRequest<OrgMetadata>(`${base}/org-metadata`, { token });
}

// ── 组织 ──

export type OrgListParams = {
  keyword?: string;
  status?: string;
  kind?: string;
  parentId?: string;
  onlyMine?: boolean;
  page?: number;
  limit?: number;
};

export function listOrganizations(token: string, params?: OrgListParams) {
  return apiRequest<OrgPage<Organization>>(
    `${base}/organizations${buildQuery(params as Record<string, string | number | boolean | undefined>)}`,
    { token },
  );
}

export function getOrganizationTree(token: string) {
  return apiRequest<OrganizationNode[]>(`${base}/organizations/tree`, { token });
}

export function getOrganization(token: string, orgId: string) {
  return apiRequest<{ organization: Organization; access: OrgAccess }>(org(orgId), { token });
}

export function getOrgOverview(token: string, orgId: string) {
  return apiRequest<OrgOverview>(`${org(orgId)}/overview`, { token });
}

export type CreateOrgPayload = {
  name: string;
  code: string;
  kind?: string;
  parentId?: string;
  description?: string;
  logoURL?: string;
  contactName?: string;
  contactEmail?: string;
  contactPhone?: string;
  industry?: string;
  region?: string;
  memberLimit?: number;
  deptLimit?: number;
  appLimit?: number;
  expiresAt?: string;
  ownerAdminId?: number;
};

export function createOrganization(token: string, payload: CreateOrgPayload) {
  return apiRequest<Organization>(`${base}/organizations`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateOrganization(token: string, orgId: string, payload: Record<string, unknown>) {
  return apiRequest<Organization>(org(orgId), { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteOrganization(token: string, orgId: string) {
  return apiRequest<void>(org(orgId), { method: "DELETE", token });
}

export function transferOrganization(token: string, orgId: string, newOwnerAdminId: number) {
  return apiRequest<void>(`${org(orgId)}/transfer`, {
    method: "POST", token, body: JSON.stringify({ newOwnerAdminId }),
  });
}

export function listOrgActivity(token: string, orgId: string, params?: { action?: string; page?: number; limit?: number }) {
  return apiRequest<OrgPage<OrgActivityLog>>(
    `${org(orgId)}/activity${buildQuery(params as Record<string, string | number | boolean | undefined>)}`,
    { token },
  );
}

// ── 部门 ──

export function getDepartmentTree(token: string, orgId: string, status?: string) {
  return apiRequest<DepartmentNode[]>(`${org(orgId)}/departments${buildQuery({ status })}`, { token });
}

export function getDepartment(token: string, orgId: string, deptId: string) {
  return apiRequest<Department>(`${org(orgId)}/departments/${deptId}`, { token });
}

export type CreateDeptPayload = {
  parentId?: string;
  name: string;
  code: string;
  kind?: string;
  description?: string;
  sortOrder?: number;
  leaderId?: number;
  memberLimit?: number;
};

export function createDepartment(token: string, orgId: string, payload: CreateDeptPayload) {
  return apiRequest<Department>(`${org(orgId)}/departments`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateDepartment(token: string, orgId: string, deptId: string, payload: Record<string, unknown>) {
  return apiRequest<Department>(`${org(orgId)}/departments/${deptId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function moveDepartment(token: string, orgId: string, deptId: string, parentId: string, sortOrder?: number) {
  return apiRequest<void>(`${org(orgId)}/departments/${deptId}/move`, {
    method: "PUT", token, body: JSON.stringify({ parentId, sortOrder }),
  });
}

/** strategy: restrict（仅空部门）/ reparent（子部门上移）/ cascade（连子树一起删） */
export function deleteDepartment(token: string, orgId: string, deptId: string, strategy: string = "restrict") {
  return apiRequest<void>(`${org(orgId)}/departments/${deptId}?strategy=${strategy}`, { method: "DELETE", token });
}

export function reorderDepartments(token: string, orgId: string, order: Record<string, number>) {
  return apiRequest<void>(`${org(orgId)}/dept-order`, { method: "PUT", token, body: JSON.stringify({ order }) });
}

// ── 组织成员 ──

export type MemberListParams = {
  keyword?: string;
  orgRole?: string;
  status?: string;
  deptId?: string;
  includeSubDepts?: boolean;
  unassigned?: boolean;
  page?: number;
  limit?: number;
};

export function listOrgMembers(token: string, orgId: string, params?: MemberListParams) {
  return apiRequest<OrgPage<OrgMember>>(
    `${org(orgId)}/members${buildQuery(params as Record<string, string | number | boolean | undefined>)}`,
    { token },
  );
}

export function getOrgMember(token: string, orgId: string, adminId: number) {
  return apiRequest<OrgMember>(`${org(orgId)}/members/${adminId}`, { token });
}

export type AddMemberPayload = {
  adminId: number;
  orgRole?: string;
  employeeNo?: string;
  title?: string;
  deptIds?: string[];
  primaryDeptId?: string;
};

export function addOrgMember(token: string, orgId: string, payload: AddMemberPayload) {
  return apiRequest<OrgMember>(`${org(orgId)}/members`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateOrgMember(token: string, orgId: string, adminId: number, payload: Record<string, unknown>) {
  return apiRequest<OrgMember>(`${org(orgId)}/members/${adminId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function removeOrgMember(token: string, orgId: string, adminId: number) {
  return apiRequest<void>(`${org(orgId)}/members/${adminId}`, { method: "DELETE", token });
}

export function assignMemberDepartments(
  token: string, orgId: string, adminId: number,
  payload: { deptIds: string[]; primaryDeptId?: string; replace?: boolean },
) {
  return apiRequest<void>(`${org(orgId)}/members/${adminId}/departments`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

/** 成员选择器：搜索平台里还没进该组织的管理员 */
export function searchAssignableAdmins(token: string, orgId: string, keyword: string, limit = 20) {
  return apiRequest<OrgMember[]>(`${org(orgId)}/assignable-admins${buildQuery({ keyword, limit })}`, { token });
}

// ── 部门成员 ──

export function listDepartmentMembers(token: string, orgId: string, deptId: string) {
  return apiRequest<DepartmentMember[]>(`${org(orgId)}/departments/${deptId}/members`, { token });
}

export type SetDeptMemberPayload = {
  isLeader?: boolean;
  positionId?: string;
  jobTitle?: string;
  reportingTo?: number;
  clearReporting?: boolean;
  delegateTo?: number;
  delegateExpiresAt?: string;
  clearDelegate?: boolean;
};

export function setDepartmentMember(token: string, orgId: string, deptId: string, adminId: number, payload: SetDeptMemberPayload) {
  return apiRequest<void>(`${org(orgId)}/departments/${deptId}/members/${adminId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function removeDepartmentMember(token: string, orgId: string, deptId: string, adminId: number) {
  return apiRequest<void>(`${org(orgId)}/departments/${deptId}/members/${adminId}`, { method: "DELETE", token });
}

export function getReportingChain(token: string, orgId: string, deptId: string, adminId: number) {
  return apiRequest<ReportingNode[]>(`${org(orgId)}/departments/${deptId}/members/${adminId}/reporting-chain`, { token });
}

export function getDirectReports(token: string, orgId: string, deptId: string, adminId: number) {
  return apiRequest<ReportingNode[]>(`${org(orgId)}/departments/${deptId}/members/${adminId}/reports`, { token });
}

// ── 邀请 ──

export type InvitePayload = {
  adminIds: number[];
  deptId?: string;
  orgRole?: string;
  isLeader?: boolean;
  message?: string;
};

export function inviteOrgMembers(token: string, orgId: string, payload: InvitePayload) {
  return apiRequest<{ created: number; requested: number; invitations: OrgInvitation[] }>(
    `${org(orgId)}/invitations`, { method: "POST", token, body: JSON.stringify(payload) },
  );
}

export function listMyInvitations(token: string, params: { role?: string; status?: string; orgId?: string; page?: number; limit?: number }) {
  return apiRequest<OrgPage<OrgInvitation>>(
    `${base}/org-invitations${buildQuery(params as Record<string, string | number | boolean | undefined>)}`,
    { token },
  );
}

export function countPendingInvitations(token: string) {
  return apiRequest<{ count: number }>(`${base}/org-invitations/count`, { token });
}

export function respondInvitation(token: string, inviteId: string, action: "accept" | "reject" | "cancel") {
  return apiRequest<OrgInvitation>(`${base}/org-invitations/${inviteId}/${action}`, { method: "POST", token });
}

// ── 岗位 ──

export function listPositions(token: string, orgId: string) {
  return apiRequest<Position[]>(`${org(orgId)}/positions`, { token });
}

export type PositionPayload = { name: string; code: string; level?: number; description?: string };

export function createPosition(token: string, orgId: string, payload: PositionPayload) {
  return apiRequest<Position>(`${org(orgId)}/positions`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updatePosition(token: string, orgId: string, posId: string, payload: PositionPayload) {
  return apiRequest<Position>(`${org(orgId)}/positions/${posId}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deletePosition(token: string, orgId: string, posId: string) {
  return apiRequest<void>(`${org(orgId)}/positions/${posId}`, { method: "DELETE", token });
}

// ── 组织角色 ──

export function listOrgRoles(token: string, orgId: string) {
  return apiRequest<{ roles: OrgRole[]; builtinRoles: { key: string; name: string; description: string; level: number }[] }>(
    `${org(orgId)}/roles`, { token },
  );
}

export type OrgRolePayload = { roleKey: string; name: string; description?: string; permissions: string[] };

export function createOrgRole(token: string, orgId: string, payload: OrgRolePayload) {
  return apiRequest<OrgRole>(`${org(orgId)}/roles`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateOrgRole(token: string, orgId: string, roleId: string, payload: OrgRolePayload) {
  return apiRequest<OrgRole>(`${org(orgId)}/roles/${roleId}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteOrgRole(token: string, orgId: string, roleId: string) {
  return apiRequest<void>(`${org(orgId)}/roles/${roleId}`, { method: "DELETE", token });
}

export function listOrgRoleGrants(token: string, orgId: string, roleId: string) {
  return apiRequest<OrgRoleGrant[]>(`${org(orgId)}/roles/${roleId}/grants`, { token });
}

export function grantOrgRole(token: string, orgId: string, roleId: string, adminIds: number[], scopeDeptId?: string) {
  return apiRequest<{ granted: number }>(`${org(orgId)}/roles/${roleId}/grants`, {
    method: "POST", token, body: JSON.stringify({ adminIds, scopeDeptId }),
  });
}

export function revokeOrgRole(token: string, orgId: string, roleId: string, adminId: number) {
  return apiRequest<void>(`${org(orgId)}/roles/${roleId}/grants/${adminId}`, { method: "DELETE", token });
}

// ── 应用绑定 ──

export function listOrgApps(token: string, orgId: string) {
  return apiRequest<OrgAppBinding[]>(`${org(orgId)}/apps`, { token });
}

export function bindOrgApp(token: string, orgId: string, appId: number, owned = false) {
  return apiRequest<void>(`${org(orgId)}/apps`, { method: "POST", token, body: JSON.stringify({ appId, owned }) });
}

export function unbindOrgApp(token: string, orgId: string, appId: number) {
  return apiRequest<void>(`${org(orgId)}/apps/${appId}`, { method: "DELETE", token });
}

// ── 审批 ──

export function listApprovalChains(token: string, orgId: string) {
  return apiRequest<ApprovalChain[]>(`${org(orgId)}/approval-chains`, { token });
}

export function createApprovalChain(token: string, orgId: string, payload: { name: string; triggerType: string; steps: ApprovalStep[]; isActive?: boolean }) {
  return apiRequest<ApprovalChain>(`${org(orgId)}/approval-chains`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateApprovalChain(token: string, orgId: string, chainId: string, payload: { name?: string; steps?: ApprovalStep[]; isActive?: boolean }) {
  return apiRequest<ApprovalChain>(`${org(orgId)}/approval-chains/${chainId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function deleteApprovalChain(token: string, orgId: string, chainId: string) {
  return apiRequest<void>(`${org(orgId)}/approval-chains/${chainId}`, { method: "DELETE", token });
}

export function listApprovalInstances(token: string, orgId: string, params?: { status?: string; page?: number; limit?: number }) {
  return apiRequest<OrgPage<ApprovalInstance>>(
    `${org(orgId)}/approvals${buildQuery(params as Record<string, string | number | boolean | undefined>)}`,
    { token },
  );
}

export function getApprovalInstance(token: string, orgId: string, instanceId: string) {
  return apiRequest<ApprovalInstance>(`${org(orgId)}/approvals/${instanceId}`, { token });
}

export function listMyPendingApprovals(token: string) {
  return apiRequest<ApprovalInstance[]>(`${base}/org-approvals/pending`, { token });
}

export function decideApproval(token: string, instanceId: string, action: "approved" | "rejected", comment = "") {
  return apiRequest<ApprovalInstance>(`${base}/org-approvals/${instanceId}/decision`, {
    method: "POST", token, body: JSON.stringify({ action, comment }),
  });
}

// ── 权限模板 ──

export function listOrgPermTemplates(token: string, orgId: string) {
  return apiRequest<OrgPermissionTemplate[]>(`${org(orgId)}/perm-templates`, { token });
}

export function createOrgPermTemplate(token: string, orgId: string, payload: { name: string; description?: string; permissions: string[]; isDefault?: boolean }) {
  return apiRequest<OrgPermissionTemplate>(`${org(orgId)}/perm-templates`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function deleteOrgPermTemplate(token: string, orgId: string, templateId: string) {
  return apiRequest<void>(`${org(orgId)}/perm-templates/${templateId}`, { method: "DELETE", token });
}

export function applyPermTemplate(token: string, orgId: string, templateId: string, payload: { adminIds: number[]; scopeDeptId?: string; roleName?: string }) {
  return apiRequest<{ role: OrgRole; granted: number }>(`${org(orgId)}/perm-templates/${templateId}/apply`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

// ── 协作组 ──

export function listCollabGroups(token: string, orgId: string) {
  return apiRequest<CollaborationGroup[]>(`${org(orgId)}/collab-groups`, { token });
}

export type CollabGroupPayload = { name: string; description?: string; deptIds: string[]; permissions: string[] };

export function createCollabGroup(token: string, orgId: string, payload: CollabGroupPayload) {
  return apiRequest<CollaborationGroup>(`${org(orgId)}/collab-groups`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateCollabGroup(token: string, orgId: string, groupId: string, payload: CollabGroupPayload) {
  return apiRequest<void>(`${org(orgId)}/collab-groups/${groupId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function deleteCollabGroup(token: string, orgId: string, groupId: string) {
  return apiRequest<void>(`${org(orgId)}/collab-groups/${groupId}`, { method: "DELETE", token });
}

// ── 导入导出 ──
//
// 导出返回 xlsx 二进制，不能走 apiRequest（它按 JSON 信封解析）。
// 也不能用裸 <a href> —— 令牌只存在于 Authorization 头里，直链下载拿不到身份，
// 后端会 401。所以统一走 fetch 拿 blob 再触发下载。

async function downloadXLSX(token: string, path: string, fallbackName: string) {
  const response = await fetch(joinApiUrl(path), {
    headers: { Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store",
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
  const match = /filename\*=UTF-8''([^;]+)/i.exec(disposition);
  const filename = match ? decodeURIComponent(match[1]) : fallbackName;

  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function downloadOrgExport(token: string, orgId: string) {
  return downloadXLSX(token, `${org(orgId)}/export`, "组织架构.xlsx");
}

export function downloadImportTemplate(token: string, orgId: string) {
  return downloadXLSX(token, `${org(orgId)}/import-template`, "组织架构导入模板.xlsx");
}

export function importOrganization(token: string, orgId: string, file: File, dryRun: boolean) {
  const form = new FormData();
  form.append("file", file);
  form.append("dryRun", String(dryRun));
  return apiRequest<OrgImportResult>(`${org(orgId)}/import`, { method: "POST", token, body: form });
}
