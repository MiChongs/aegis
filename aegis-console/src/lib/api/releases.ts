import { apiRequest, buildQuery } from "./client";
import type {
  Release,
  ReleaseAssetUploadResult,
  ReleaseListResult,
  ReleaseOverview,
  ReleaseSavePayload,
  ReleaseSimulateInput,
  ReleaseSimulation,
  ReleaseStats,
  VersionChannel
} from "./types";

/**
 * 发布中心：/api/admin/apps/{appKey}/releases/*。
 * 渠道沿用 /channels；旧的 /versions 接口不再由控制台调用。
 */

export type ReleaseListParams = {
  page?: number;
  limit?: number;
  status?: string;
  platform?: string;
  channelId?: number;
  keyword?: string;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/releases`;

export function getAdminReleases(token: string, appKey: string, params: ReleaseListParams = {}) {
  return apiRequest<ReleaseListResult>(`${base(appKey)}${buildQuery({ ...params })}`, { token });
}

export function getAdminReleaseOverview(token: string, appKey: string) {
  return apiRequest<ReleaseOverview>(`${base(appKey)}/overview`, { token });
}

export function getAdminRelease(token: string, appKey: string, releaseId: number) {
  return apiRequest<Release>(`${base(appKey)}/${releaseId}`, { token });
}

export function createAdminRelease(token: string, appKey: string, payload: ReleaseSavePayload) {
  return apiRequest<Release>(base(appKey), { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAdminRelease(token: string, appKey: string, releaseId: number, payload: ReleaseSavePayload) {
  return apiRequest<Release>(`${base(appKey)}/${releaseId}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteAdminRelease(token: string, appKey: string, releaseId: number) {
  return apiRequest<{ id: number }>(`${base(appKey)}/${releaseId}`, { method: "DELETE", token });
}

/** publishAt 为空表示立即发布。 */
export function publishAdminRelease(token: string, appKey: string, releaseId: number, publishAt?: string | null) {
  return apiRequest<Release>(`${base(appKey)}/${releaseId}/publish`, {
    method: "POST",
    token,
    body: JSON.stringify(publishAt ? { publishAt } : {})
  });
}

export type ReleaseTransition = "pause" | "resume" | "revoke";

export function transitionAdminRelease(token: string, appKey: string, releaseId: number, action: ReleaseTransition) {
  return apiRequest<Release>(`${base(appKey)}/${releaseId}/${action}`, { method: "POST", token, body: "{}" });
}

export function setAdminReleaseRollout(token: string, appKey: string, releaseId: number, rolloutPct: number) {
  return apiRequest<Release>(`${base(appKey)}/${releaseId}/rollout`, {
    method: "PUT",
    token,
    body: JSON.stringify({ rolloutPct })
  });
}

export function getAdminReleaseStats(token: string, appKey: string, releaseId: number) {
  return apiRequest<ReleaseStats>(`${base(appKey)}/${releaseId}/stats`, { token });
}

export function simulateAdminRelease(token: string, appKey: string, input: ReleaseSimulateInput) {
  return apiRequest<ReleaseSimulation>(`${base(appKey)}/simulate`, {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

export function uploadAdminReleaseAsset(token: string, appKey: string, file: File) {
  const form = new FormData();
  form.append("file", file);
  return apiRequest<ReleaseAssetUploadResult>(`${base(appKey)}/assets`, { method: "POST", token, body: form });
}

// ── 渠道 ──

const channelBase = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/channels`;

export function getAdminVersionChannels(token: string, appKey: string) {
  return apiRequest<VersionChannel[]>(channelBase(appKey), { token });
}

export function createAdminVersionChannel(token: string, appKey: string, payload: Record<string, unknown>) {
  return apiRequest<VersionChannel>(channelBase(appKey), { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAdminVersionChannel(token: string, appKey: string, channelId: number, payload: Record<string, unknown>) {
  return apiRequest<VersionChannel>(`${channelBase(appKey)}/${channelId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminVersionChannel(token: string, appKey: string, channelId: number) {
  return apiRequest<null>(`${channelBase(appKey)}/${channelId}`, { method: "DELETE", token });
}

export type ChannelUser = { id: number; userId: number; account?: string; nickname?: string; avatar?: string };

export function getAdminVersionChannelUsers(
  token: string,
  appKey: string,
  channelId: number,
  params?: { page?: number; limit?: number }
) {
  const q = buildQuery({ page: params?.page, limit: params?.limit });
  return apiRequest<{ items: ChannelUser[]; total: number }>(`${channelBase(appKey)}/${channelId}/users${q}`, { token });
}

export function addUsersToVersionChannel(token: string, appKey: string, channelId: number, userIds: number[]) {
  return apiRequest<{ added: number; skipped: number }>(`${channelBase(appKey)}/${channelId}/users`, {
    method: "POST",
    token,
    body: JSON.stringify({ user_ids: userIds })
  });
}

export function removeUsersFromVersionChannel(token: string, appKey: string, channelId: number, userIds: number[]) {
  return apiRequest<{ removed: number }>(`${channelBase(appKey)}/${channelId}/users`, {
    method: "DELETE",
    token,
    body: JSON.stringify({ user_ids: userIds })
  });
}
