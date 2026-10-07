import { ApiError, apiRequest, buildQuery, joinApiUrl } from "./client";
import type { AuditFacets, AuditLog, AuditOverview, AuditPage } from "./types";

/**
 * 全局审计日志（仅超级管理员）。
 *
 * kind 默认传 operation：查看类请求（翻页、打开详情）不进默认列表，否则真正的变更会被淹没。
 */
export type AuditListParams = {
  kind?: string;
  category?: string;
  adminId?: string;
  appId?: string;
  status?: string;
  severity?: string;
  keyword?: string;
  sessionId?: string;
  startTime?: string;
  endTime?: string;
  page?: number;
  limit?: number;
};

const BASE = "/api/admin/system/audit-logs";

export function listAuditLogs(token: string, params: AuditListParams) {
  return apiRequest<AuditPage>(`${BASE}${buildQuery({ ...params })}`, { token });
}

export function getAuditLog(token: string, id: number) {
  return apiRequest<AuditLog>(`${BASE}/${id}`, { token });
}

export function getAuditOverview(token: string) {
  return apiRequest<AuditOverview>(`${BASE}/overview`, { token });
}

export function getAuditFacets(token: string) {
  return apiRequest<AuditFacets>(`${BASE}/facets`, { token });
}

/** 导出 CSV。需要带令牌，因此不能用 <a href>，取回后由浏览器另存。 */
export async function exportAuditLogs(token: string, params: Omit<AuditListParams, "page" | "limit">) {
  const response = await fetch(joinApiUrl(`${BASE}/export${buildQuery({ ...params })}`), {
    headers: { Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store"
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
  const match = /filename\*?=(?:UTF-8'')?"?([^;"]+)"?/i.exec(disposition);
  const filename = match ? decodeURIComponent(match[1]) : "audit-logs.csv";
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
