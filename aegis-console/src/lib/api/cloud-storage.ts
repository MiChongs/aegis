import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 用户云存储域 API。
 *
 * 每个用户一块私有空间，按「命名空间 / 键」存放任意文档；内容走应用的存储配置，
 * 后端只存索引与账目。应用级部分（配置 / 概览 / 用户列表）在 `/cloud-storage` 下，
 * 单个用户的条目在 `/users/{userId}/cloud-storage` 下（权限与资料、钱包相同）。
 */

export type CloudNamespace = {
  key: string;
  name: string;
  description?: string;
};

export type CloudStorageTarget = {
  configId: number;
  configName: string;
  provider: string;
  scope: string;
  /** 解析失败的原因（没有可用存储配置时） */
  error?: string;
};

export type CloudStorageOption = {
  configId: number;
  configName: string;
  provider: string;
  scope: "app" | "global" | string;
  accessMode: "public" | "private" | string;
  enabled: boolean;
  isDefault: boolean;
};

export type CloudStorageCaps = {
  maxQuotaBytes: number;
  maxItemBytes: number;
  maxItems: number;
  maxRevisions: number;
  maxTrashRetentionDays: number;
  maxNamespaces: number;
};

export type CloudStorageConfig = {
  appid: number;
  enabled: boolean;
  /** 空串表示走应用默认存储 */
  storageConfigName: string;
  quotaBytes: number;
  maxItemBytes: number;
  maxItems: number;
  maxRevisions: number;
  trashRetentionDays: number;
  restrictNamespaces: boolean;
  namespaces: CloudNamespace[];
  updatedBy?: string;
  updatedAt?: string;
  configured: boolean;
  target?: CloudStorageTarget | null;
  storageOptions: CloudStorageOption[];
  caps: CloudStorageCaps;
};

export type SaveCloudStorageConfigPayload = {
  enabled: boolean;
  storageConfigName: string;
  quotaBytes: number;
  maxItemBytes: number;
  maxItems: number;
  maxRevisions: number;
  trashRetentionDays: number;
  restrictNamespaces: boolean;
  namespaces: CloudNamespace[];
};

export type CloudNamespaceUsage = {
  namespace: string;
  name?: string;
  itemCount: number;
  trashCount: number;
  storedBytes: number;
};

export type CloudUserState = {
  appid: number;
  userId: number;
  account?: string;
  nickname?: string;
  /** 管理员设置的配额覆盖；省略表示沿用应用默认 */
  quotaOverride?: number | null;
  /** 生效配额 */
  quotaBytes: number;
  frozen: boolean;
  frozenReason?: string;
  note?: string;
  usedBytes: number;
  itemCount: number;
  trashCount: number;
  revisionCount: number;
  lastWriteAt?: string | null;
  updatedBy?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type CloudLimits = {
  quotaBytes: number;
  maxItemBytes: number;
  maxItems: number;
  maxRevisions: number;
  trashRetentionDays: number;
  inlineContentBytes: number;
  jsonWriteBytes: number;
};

export type CloudAdminUser = CloudUserState & {
  enabled: boolean;
  limits: CloudLimits;
  namespaces: CloudNamespaceUsage[];
};

export type CloudDailyWrites = {
  day: string;
  writes: number;
  bytes: number;
  users: number;
};

export type CloudStorageStats = {
  users: number;
  frozenUsers: number;
  usedBytes: number;
  items: number;
  trashItems: number;
  revisions: number;
  writesToday: number;
  namespaces: CloudNamespaceUsage[];
  topUsers: CloudUserState[];
  trend: CloudDailyWrites[];
  storageTarget?: CloudStorageTarget | null;
};

export type CloudItemEncoding = "json" | "text" | "base64";

export type CloudItem = {
  id: number;
  namespace: string;
  key: string;
  revision: number;
  contentType: string;
  encoding: CloudItemEncoding;
  size: number;
  sha256: string;
  metadata: Record<string, unknown>;
  deviceId?: string;
  deleted: boolean;
  deletedAt?: string | null;
  /** 回收站条目被自动清除的时间 */
  purgeAt?: string | null;
  revisionCount: number;
  storedBytes: number;
  createdAt: string;
  updatedAt: string;
};

export type CloudItemContent = CloudItem & {
  contentRevision: number;
  /** json 编码时是任意 JSON 值，text 时是字符串，base64 时是 base64 字符串 */
  content?: unknown;
  /** 内容超过内联上限，需要用下载地址取 */
  contentOmitted?: boolean;
};

export type CloudRevisionSource = "write" | "upload" | "rollback" | "admin";

export type CloudRevision = {
  id: number;
  itemId: number;
  revision: number;
  contentType: string;
  encoding: CloudItemEncoding;
  size: number;
  sha256: string;
  metadata: Record<string, unknown>;
  deviceId?: string;
  source: CloudRevisionSource;
  restoredFrom?: number | null;
  operator?: string;
  current: boolean;
  createdAt: string;
};

export type CloudPage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
};

export type CloudUserParams = {
  keyword?: string;
  sort?: "usage" | "items" | "recent";
  frozen?: "true" | "false";
  page?: number;
  limit?: number;
};

export type CloudItemParams = {
  namespace?: string;
  keyword?: string;
  status?: "active" | "deleted" | "all";
  page?: number;
  limit?: number;
};

export type SaveCloudUserPayload = {
  /** null 表示沿用应用默认配额 */
  quotaBytes: number | null;
  frozen: boolean;
  frozenReason?: string;
  note?: string;
};

export type CloudLink = {
  configId: number;
  provider: string;
  key: string;
  url: string;
  accessMode: string;
  proxyRequired: boolean;
  expiresAt: string;
};

const appBase = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/cloud-storage`;
const userBase = (appKey: string, userId: number) =>
  `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/cloud-storage`;

// ── 应用级 ──

export function getCloudStorageConfig(token: string, appKey: string) {
  return apiRequest<CloudStorageConfig>(`${appBase(appKey)}/config`, { token });
}

export function saveCloudStorageConfig(token: string, appKey: string, payload: SaveCloudStorageConfigPayload) {
  return apiRequest<CloudStorageConfig>(`${appBase(appKey)}/config`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function getCloudStorageStats(token: string, appKey: string, days: number) {
  return apiRequest<CloudStorageStats>(`${appBase(appKey)}/stats${buildQuery({ days })}`, { token });
}

export function listCloudStorageUsers(token: string, appKey: string, params?: CloudUserParams) {
  return apiRequest<CloudPage<CloudUserState>>(`${appBase(appKey)}/users${buildQuery(params ?? {})}`, { token });
}

export function purgeExpiredCloudTrash(token: string, appKey: string) {
  return apiRequest<{ purged: number }>(`${appBase(appKey)}/purge-expired`, { method: "POST", token });
}

// ── 单个用户 ──

export function getCloudStorageUser(token: string, appKey: string, userId: number) {
  return apiRequest<CloudAdminUser>(userBase(appKey, userId), { token });
}

export function saveCloudStorageUser(token: string, appKey: string, userId: number, payload: SaveCloudUserPayload) {
  return apiRequest<CloudAdminUser>(userBase(appKey, userId), {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function purgeCloudStorageUser(token: string, appKey: string, userId: number) {
  return apiRequest<{ objects: number }>(userBase(appKey, userId), { method: "DELETE", token });
}

export function listCloudStorageUserItems(token: string, appKey: string, userId: number, params?: CloudItemParams) {
  return apiRequest<CloudPage<CloudItem>>(`${userBase(appKey, userId)}/items${buildQuery(params ?? {})}`, { token });
}

export function getCloudStorageUserItem(
  token: string,
  appKey: string,
  userId: number,
  itemId: number,
  revision?: number
) {
  return apiRequest<CloudItemContent>(
    `${userBase(appKey, userId)}/items/${itemId}${buildQuery({ revision })}`,
    { token }
  );
}

export function listCloudStorageUserItemRevisions(token: string, appKey: string, userId: number, itemId: number) {
  return apiRequest<{ items: CloudRevision[] }>(`${userBase(appKey, userId)}/items/${itemId}/revisions`, { token });
}

export function rollbackCloudStorageUserItem(
  token: string,
  appKey: string,
  userId: number,
  itemId: number,
  revision: number
) {
  return apiRequest<CloudItem>(`${userBase(appKey, userId)}/items/${itemId}/rollback`, {
    method: "POST",
    token,
    body: JSON.stringify({ revision })
  });
}

export function restoreCloudStorageUserItem(token: string, appKey: string, userId: number, itemId: number) {
  return apiRequest<CloudItem>(`${userBase(appKey, userId)}/items/${itemId}/restore`, { method: "POST", token });
}

export function deleteCloudStorageUserItem(
  token: string,
  appKey: string,
  userId: number,
  itemId: number,
  permanent: boolean
) {
  return apiRequest<CloudItem>(
    `${userBase(appKey, userId)}/items/${itemId}${buildQuery({ permanent: permanent ? "true" : undefined })}`,
    { method: "DELETE", token }
  );
}

export function linkCloudStorageUserItem(
  token: string,
  appKey: string,
  userId: number,
  itemId: number,
  revision?: number,
  download = true
) {
  return apiRequest<CloudLink>(`${userBase(appKey, userId)}/items/${itemId}/link`, {
    method: "POST",
    token,
    body: JSON.stringify({ revision: revision ?? 0, download })
  });
}
