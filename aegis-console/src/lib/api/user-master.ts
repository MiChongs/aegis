import { apiRequest, buildQuery } from "./client";
import type { GlobalIdentity, UserMasterTag, UserMasterSegment, UserListEntry, UserAppeal, DeactivationRequest, IdentityUserMapping } from "./types";

// ── 统一身份 ──

export function listIdentities(token: string, params?: { keyword?: string; status?: string; lifecycleState?: string; riskLevel?: string; tagId?: number; page?: number; limit?: number }) {
  return apiRequest<{ items: GlobalIdentity[]; total: number }>(`/api/admin/system/user-master/identities?${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}
export function getIdentity(token: string, id: number) {
  return apiRequest<GlobalIdentity>(`/api/admin/system/user-master/identities/${id}`, { token });
}
export function updateIdentityStatus(token: string, id: number, status: string) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${id}/status`, { method: "PUT", token, body: JSON.stringify({ status }) });
}
export function updateIdentityRisk(token: string, id: number, data: { riskScore: number; riskLevel: string }) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${id}/risk`, { method: "PUT", token, body: JSON.stringify(data) });
}
export function updateIdentityLifecycle(token: string, id: number, state: string) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${id}/lifecycle`, { method: "PUT", token, body: JSON.stringify({ state }) });
}
export function syncIdentities(token: string, appId: number) {
  return apiRequest<{ synced: number }>("/api/admin/system/user-master/sync/batch", { method: "POST", token, body: JSON.stringify({ appId }) });
}
export function mergeIdentity(token: string, id: number, targetId: number) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${id}/merge`, { method: "POST", token, body: JSON.stringify({ targetId }) });
}
export function deactivateIdentity(token: string, id: number, reason?: string, coolingDays?: number) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${id}/deactivate`, { method: "POST", token, body: JSON.stringify({ reason, coolingDays: coolingDays || 14 }) });
}

// ── 标签 ──

export function listTags(token: string) {
  return apiRequest<UserMasterTag[]>("/api/admin/system/user-master/tags", { token });
}
export function createTag(token: string, data: { name: string; color: string; description?: string }) {
  return apiRequest<UserMasterTag>("/api/admin/system/user-master/tags", { method: "POST", token, body: JSON.stringify(data) });
}
export function deleteTag(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/user-master/tags/${id}`, { method: "DELETE", token });
}
export function assignTag(token: string, identityId: number, tagId: number) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${identityId}/tags`, { method: "POST", token, body: JSON.stringify({ tagId }) });
}
export function removeTag(token: string, identityId: number, tagId: number) {
  return apiRequest<void>(`/api/admin/system/user-master/identities/${identityId}/tags/${tagId}`, { method: "DELETE", token });
}

// ── 分群 ──

export function listSegments(token: string) {
  return apiRequest<UserMasterSegment[]>("/api/admin/system/user-master/segments", { token });
}
export function createSegment(token: string, data: { name: string; description?: string; segmentType: string; rules?: Record<string, unknown> }) {
  return apiRequest<UserMasterSegment>("/api/admin/system/user-master/segments", { method: "POST", token, body: JSON.stringify(data) });
}
export function deleteSegment(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/user-master/segments/${id}`, { method: "DELETE", token });
}
export function listSegmentMembers(token: string, segmentId: number, params?: { page?: number; limit?: number }) {
  return apiRequest<{ items: GlobalIdentity[]; total: number }>(`/api/admin/system/user-master/segments/${segmentId}/members?${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}
export function addSegmentMember(token: string, segmentId: number, identityId: number) {
  return apiRequest<void>(`/api/admin/system/user-master/segments/${segmentId}/members`, { method: "POST", token, body: JSON.stringify({ identityId }) });
}
export function removeSegmentMember(token: string, segmentId: number, identityId: number) {
  return apiRequest<void>(`/api/admin/system/user-master/segments/${segmentId}/members/${identityId}`, { method: "DELETE", token });
}

// ── 黑白名单 ──

export function listUserListEntries(token: string, listType: string, params?: { page?: number; limit?: number }) {
  return apiRequest<{ items: UserListEntry[]; total: number }>(`/api/admin/system/user-master/user-lists?listType=${listType}&${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}
export function createUserListEntry(token: string, data: { listType: string; identityId?: number; email?: string; phone?: string; ip?: string; reason: string; expiresAt?: string }) {
  return apiRequest<UserListEntry>("/api/admin/system/user-master/user-lists", { method: "POST", token, body: JSON.stringify(data) });
}
export function deleteUserListEntry(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/user-master/user-lists/${id}`, { method: "DELETE", token });
}

// ── 申诉 ──

export function listAppeals(token: string, params?: { status?: string; page?: number; limit?: number }) {
  return apiRequest<{ items: UserAppeal[]; total: number }>(`/api/admin/system/user-master/appeals?${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}
export function reviewAppeal(token: string, id: number, data: { action: string; comment?: string }) {
  return apiRequest<void>(`/api/admin/system/user-master/appeals/${id}/review`, { method: "POST", token, body: JSON.stringify(data) });
}

// ── 注销 ──

export function listDeactivations(token: string) {
  return apiRequest<DeactivationRequest[]>("/api/admin/system/user-master/deactivations", { token });
}
export function cancelDeactivation(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/user-master/deactivations/${id}/cancel`, { method: "POST", token });
}
