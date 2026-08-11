import { apiRequest, buildQuery } from "./client";
import type {
  AppMonitorOverview,
  MonitorAppBrief,
  MonitorOverview,
  OnlineStats,
  OnlineUserItem,
  PagedResult
} from "./types";

export function getSystemOnlineStats(token: string) {
  return apiRequest<OnlineStats>("/api/admin/system/online/stats", { token });
}

/**
 * `/api/system/monitor/**` 在后端挂了 `AdminAuth + RequireSuperAdmin`，
 * 控制台侧必须带上管理员 token，否则整块可用性监测拿到 401 变成「监测数据不可用」。
 * token 可空：公开状态页仍按匿名请求发出（结果由后端决定）。
 */
export function getSystemMonitor(token?: string | null) {
  return apiRequest<MonitorOverview>("/api/system/monitor", { token });
}

export function getSystemMonitorApps(token?: string | null) {
  return apiRequest<MonitorAppBrief[]>("/api/system/monitor/apps", { token });
}

export function getSystemMonitorComponents(token?: string | null) {
  return apiRequest<Pick<MonitorOverview, "status" | "score" | "availabilityRate" | "checkedAt" | "runtime" | "counts" | "components" | "infrastructure" | "modules">>(
    "/api/system/monitor/components",
    { token }
  );
}

export function getAppMonitor(appId: number | string, token?: string | null) {
  return apiRequest<AppMonitorOverview>(`/api/system/monitor/apps/${appId}`, { token });
}

export function getAppMonitorComponents(appId: number | string, token?: string | null) {
  return apiRequest<Pick<AppMonitorOverview, "status" | "score" | "availabilityRate" | "checkedAt" | "runtime" | "counts" | "app" | "entrypoints" | "modules" | "components" | "infrastructure">>(
    `/api/system/monitor/apps/${appId}/components`,
    { token }
  );
}

export type MonitorHistoryPoint = {
  t: number;   // unix ms
  s: string;   // status
  l?: number;  // latency ms
};

export type MonitorHistoryRange = "hour" | "day" | "week" | "month";

export function getSystemMonitorHistory(keys: string[], range_: MonitorHistoryRange = "hour", token?: string | null) {
  const query = `?keys=${keys.join(",")}&range=${range_}`;
  return apiRequest<Record<string, MonitorHistoryPoint[]>>(`/api/system/monitor/history${query}`, { token });
}

export function getAppMonitorHistory(appId: number | string, keys: string[], range_: MonitorHistoryRange = "hour", token?: string | null) {
  const query = `?keys=${keys.join(",")}&range=${range_}`;
  return apiRequest<Record<string, MonitorHistoryPoint[]>>(`/api/system/monitor/apps/${appId}/history${query}`, { token });
}

export function getAppOnlineStats(token: string, appId: number | string) {
  return apiRequest<OnlineStats>(`/api/admin/system/online/apps/${appId}`, { token });
}

export function getAppOnlineUsers(
  token: string,
  appId: number | string,
  params?: { page?: number; limit?: number }
) {
  const query = buildQuery({
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<OnlineUserItem>>(`/api/admin/system/online/apps/${appId}/users${query}`, { token });
}