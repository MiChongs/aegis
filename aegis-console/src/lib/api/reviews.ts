import { apiRequest } from "./client";
import type {
  RoleApplicationItem,
  RoleApplicationListResult,
  RoleApplicationStatistics,
  SiteAuditStats,
  SiteItem,
  SiteListResult
} from "./types";

export function getAdminSites(
  token: string,
  payload: { appid: number; page?: number; limit?: number; keyword?: string; status?: string }
) {
  return apiRequest<SiteListResult>("/api/admin/app/site/list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminSiteAudits(
  token: string,
  payload: { appid: number; page?: number; limit?: number; keyword?: string; status?: string }
) {
  return apiRequest<SiteListResult>("/api/admin/app/site/audit-list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminSiteDetail(token: string, payload: { appid: number; id: number }) {
  return apiRequest<SiteItem>("/api/admin/app/site/detail", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function auditAdminSite(
  token: string,
  payload: { appid: number; siteId: number; status: string; reason?: string }
) {
  return apiRequest<SiteItem>("/api/admin/app/site/audit", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function batchAuditAdminSites(
  token: string,
  payload: { appid: number; siteIds: number[]; status: string; reason?: string }
) {
  return apiRequest<Record<string, unknown>>("/api/admin/app/site/batch-audit", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminSite(token: string, payload: Record<string, unknown>) {
  return apiRequest<SiteItem>("/api/admin/app/site/update", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminSite(token: string, payload: { appid: number; id: number }) {
  return apiRequest<null>("/api/admin/app/site/delete", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function toggleAdminSitePin(token: string, payload: { appid: number; id: number; is_pinned: boolean }) {
  return apiRequest<SiteItem>("/api/admin/app/site/toggle-pin", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminUserSites(token: string, payload: { appid: number; userId: number }) {
  return apiRequest<SiteListResult>("/api/admin/app/site/user-sites", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminSiteAuditStats(token: string, payload: { appid: number }) {
  return apiRequest<SiteAuditStats>("/api/admin/app/site/audit-stats", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminRoleApplications(
  token: string,
  payload: {
    appid: number;
    page?: number;
    limit?: number;
    status?: string;
    requestedRole?: string;
    priority?: string;
    keyword?: string;
  }
) {
  return apiRequest<RoleApplicationListResult>("/api/admin/app/role-application/list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminRoleApplicationDetail(token: string, payload: { appid: number; id: number }) {
  return apiRequest<RoleApplicationItem>("/api/admin/app/role-application/detail", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function reviewAdminRoleApplication(
  token: string,
  payload: { appid: number; id: number; action: string; reviewReason?: string }
) {
  return apiRequest<RoleApplicationItem>("/api/admin/app/role-application/review", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function batchReviewAdminRoleApplications(
  token: string,
  payload: { appid: number; ids: number[]; action: string; reviewReason?: string }
) {
  return apiRequest<Record<string, unknown>>("/api/admin/app/role-application/batch-review", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminRoleApplicationStatistics(token: string, payload: { appid: number }) {
  return apiRequest<RoleApplicationStatistics>("/api/admin/app/role-application/statistics", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}
