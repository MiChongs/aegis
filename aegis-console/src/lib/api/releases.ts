import { apiRequest, buildQuery } from "./client";
import type { VersionChannel, VersionItem, VersionListResult, VersionStats } from "./types";

// ── 版本 ──

export function getAdminVersions(
  token: string,
  appKey: string,
  params?: { page?: number; limit?: number; status?: string; platform?: string; channel_id?: number }
) {
  const q = buildQuery({ page: params?.page, limit: params?.limit, status: params?.status, platform: params?.platform, channel_id: params?.channel_id });
  return apiRequest<VersionListResult>(`/api/admin/apps/${appKey}/versions${q}`, { token });
}

export function getAdminVersionDetail(token: string, appKey: string, versionId: number) {
  return apiRequest<VersionItem>(`/api/admin/apps/${appKey}/versions/${versionId}`, { token });
}

export function createAdminVersion(token: string, appKey: string, payload: Record<string, unknown>) {
  return apiRequest<VersionItem>(`/api/admin/apps/${appKey}/versions`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateAdminVersion(token: string, appKey: string, versionId: number, payload: Record<string, unknown>) {
  return apiRequest<VersionItem>(`/api/admin/apps/${appKey}/versions/${versionId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function deleteAdminVersion(token: string, appKey: string, versionId: number) {
  return apiRequest<null>(`/api/admin/apps/${appKey}/versions/${versionId}`, {
    method: "DELETE", token,
  });
}

export function publishAdminVersion(token: string, appKey: string, versionId: number) {
  return apiRequest<VersionItem>(`/api/admin/apps/${appKey}/versions/${versionId}/publish`, {
    method: "POST", token,
  });
}

export function revokeAdminVersion(token: string, appKey: string, versionId: number) {
  return apiRequest<VersionItem>(`/api/admin/apps/${appKey}/versions/${versionId}/revoke`, {
    method: "POST", token,
  });
}

export function getAdminVersionStats(token: string, appKey: string) {
  return apiRequest<VersionStats>(`/api/admin/apps/${appKey}/versions/stats`, { token });
}

// ── 渠道 ──

export function getAdminVersionChannels(token: string, appKey: string) {
  return apiRequest<VersionChannel[]>(`/api/admin/apps/${appKey}/channels`, { token });
}

export function getAdminVersionChannelDetail(token: string, appKey: string, channelId: number) {
  return apiRequest<VersionChannel>(`/api/admin/apps/${appKey}/channels/${channelId}`, { token });
}

export function createAdminVersionChannel(token: string, appKey: string, payload: Record<string, unknown>) {
  return apiRequest<VersionChannel>(`/api/admin/apps/${appKey}/channels`, {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateAdminVersionChannel(token: string, appKey: string, channelId: number, payload: Record<string, unknown>) {
  return apiRequest<VersionChannel>(`/api/admin/apps/${appKey}/channels/${channelId}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function deleteAdminVersionChannel(token: string, appKey: string, channelId: number) {
  return apiRequest<null>(`/api/admin/apps/${appKey}/channels/${channelId}`, {
    method: "DELETE", token,
  });
}

export function getAdminVersionChannelUsers(
  token: string,
  appKey: string,
  channelId: number,
  params?: { page?: number; limit?: number }
) {
  const q = buildQuery({ page: params?.page, limit: params?.limit });
  return apiRequest<{ items: unknown[]; total: number }>(`/api/admin/apps/${appKey}/channels/${channelId}/users${q}`, { token });
}

export function addUsersToVersionChannel(
  token: string,
  appKey: string,
  channelId: number,
  userIds: number[]
) {
  return apiRequest<{ added: number; skipped: number }>(`/api/admin/apps/${appKey}/channels/${channelId}/users`, {
    method: "POST", token, body: JSON.stringify({ user_ids: userIds }),
  });
}

export function removeUsersFromVersionChannel(
  token: string,
  appKey: string,
  channelId: number,
  userIds: number[]
) {
  return apiRequest<{ removed: number }>(`/api/admin/apps/${appKey}/channels/${channelId}/users`, {
    method: "DELETE", token, body: JSON.stringify({ user_ids: userIds }),
  });
}

export function previewVersionChannelMatch(
  token: string,
  payload: { appid: number; channel_id?: number; targetAudience?: Record<string, unknown> }
) {
  return apiRequest<Record<string, unknown>>("/api/admin/app/version/channel/preview-match", {
    method: "POST", token, body: JSON.stringify(payload),
  });
}
