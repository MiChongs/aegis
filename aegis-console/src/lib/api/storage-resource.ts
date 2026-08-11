import { apiRequest, buildQuery, joinApiUrl } from "./client";
import type {
  StorageObject, StorageRule, StorageCDNConfig, StorageImageRule, StorageUsageStats, StorageUsageSnapshot,
  StorageObjectQuery, StorageObjectListResult, StorageBatchAction, StorageBatchResult, StorageObjectAccessLink,
} from "./types";

// ── 文件管理 ──

/**
 * 列表 / 子目录 / 汇总一次返回。三者由同一组筛选条件推导，
 * 拆成三个请求会出现「文件已翻到第 3 页、目录树还停在上一次筛选」。
 */
export function listStorageObjects(token: string, params?: StorageObjectQuery) {
  return apiRequest<StorageObjectListResult>(
    `/api/admin/system/storage/objects${buildQuery(params as Record<string, string | number | boolean | undefined>)}`,
    { token },
  );
}

export function getStorageObject(token: string, objectId: number) {
  return apiRequest<StorageObject>(`/api/admin/system/storage/objects/${objectId}`, { token });
}

/** 批量移入回收站 / 恢复 / 永久删除 —— 一个请求，一个事务 */
export function batchMutateStorageObjects(token: string, action: StorageBatchAction, ids: number[]) {
  return apiRequest<StorageBatchResult>("/api/admin/system/storage/objects/batch", {
    method: "POST", token, body: JSON.stringify({ action, ids }),
  });
}

/**
 * 为对象签发访问链接。私有桶拿到的是限时代理票据地址（免登录、到期即失效），
 * 因此可以直接塞进 <img src> 或 <a href>。
 */
export function createStorageObjectLink(token: string, objectId: number, options?: { download?: boolean; expiresIn?: number }) {
  return apiRequest<StorageObjectAccessLink>(`/api/admin/system/storage/objects/${objectId}/link`, {
    method: "POST", token,
    body: JSON.stringify({ download: options?.download ?? false, expiresIn: options?.expiresIn ?? 600 }),
  });
}

/** 缩略图地址。需要 Authorization 头，因此不能直接当 <img src>，见 useStorageThumbnail */
export function storageThumbnailUrl(objectId: number, width = 192) {
  return joinApiUrl(`/api/admin/system/storage/objects/${objectId}/thumbnail?w=${width}`);
}

export function softDeleteStorageObject(token: string, objectId: number) {
  return apiRequest<void>(`/api/admin/system/storage/objects/${objectId}`, { method: "DELETE", token });
}

export function restoreStorageObject(token: string, objectId: number) {
  return apiRequest<void>(`/api/admin/system/storage/objects/${objectId}/restore`, { method: "POST", token });
}

export function permanentDeleteStorageObject(token: string, objectId: number) {
  return apiRequest<void>(`/api/admin/system/storage/objects/${objectId}/permanent`, { method: "DELETE", token });
}

export function listTrashObjects(token: string, params?: { configId?: number; page?: number; limit?: number }) {
  return apiRequest<{ items: StorageObject[]; total: number }>(`/api/admin/system/storage/trash?${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}

export function cleanupTrash(token: string, olderThanDays?: number) {
  return apiRequest<{ deleted: number }>("/api/admin/system/storage/trash/cleanup", { method: "POST", token, body: JSON.stringify({ olderThanDays: olderThanDays || 30 }) });
}

// ── 规则 ──

export function listStorageRules(token: string, params?: { configId?: number; appId?: number }) {
  return apiRequest<StorageRule[]>(`/api/admin/system/storage/rules?${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}

export function createStorageRule(token: string, data: { configId?: number; appId?: number; name: string; ruleType: string; ruleData: Record<string, unknown> }) {
  return apiRequest<StorageRule>("/api/admin/system/storage/rules", { method: "POST", token, body: JSON.stringify(data) });
}

export function updateStorageRule(token: string, ruleId: number, data: { name?: string; ruleData?: Record<string, unknown>; isActive?: boolean }) {
  return apiRequest<void>(`/api/admin/system/storage/rules/${ruleId}`, { method: "PUT", token, body: JSON.stringify(data) });
}

export function deleteStorageRule(token: string, ruleId: number) {
  return apiRequest<void>(`/api/admin/system/storage/rules/${ruleId}`, { method: "DELETE", token });
}

// ── CDN ──

export function getCDNConfig(token: string, configId: number) {
  return apiRequest<StorageCDNConfig>(`/api/admin/system/storage/cdn/${configId}`, { token });
}

export function upsertCDNConfig(token: string, configId: number, data: Record<string, unknown>) {
  return apiRequest<StorageCDNConfig>(`/api/admin/system/storage/cdn/${configId}`, { method: "PUT", token, body: JSON.stringify(data) });
}

export function deleteCDNConfig(token: string, configId: number) {
  return apiRequest<void>(`/api/admin/system/storage/cdn/${configId}`, { method: "DELETE", token });
}

// ── 图片规则 ──

export function listImageRules(token: string, configId?: number) {
  const q = configId ? `?configId=${configId}` : "";
  return apiRequest<StorageImageRule[]>(`/api/admin/system/storage/image-rules${q}`, { token });
}

export function createImageRule(token: string, data: { configId?: number; name: string; ruleType: string; ruleData: Record<string, unknown> }) {
  return apiRequest<StorageImageRule>("/api/admin/system/storage/image-rules", { method: "POST", token, body: JSON.stringify(data) });
}

export function deleteImageRule(token: string, ruleId: number) {
  return apiRequest<void>(`/api/admin/system/storage/image-rules/${ruleId}`, { method: "DELETE", token });
}

// ── 用量统计 ──

export function getStorageUsage(token: string, configId?: number) {
  const q = configId ? `?configId=${configId}` : "";
  return apiRequest<StorageUsageStats>(`/api/admin/system/storage/usage${q}`, { token });
}

export function getStorageUsageHistory(token: string, configId: number, days?: number) {
  return apiRequest<StorageUsageSnapshot[]>(`/api/admin/system/storage/usage/history?configId=${configId}&days=${days || 30}`, { token });
}
