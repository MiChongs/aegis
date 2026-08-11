import { apiRequest, buildQuery } from "./client";
import type {
  PlatformBannerItem,
  PlatformBannerListResponse,
  PlatformBannerMutation
} from "./types";

// 所有端点位于 /api/admin/system/banners/*
// - /active 仅需 AdminAuth 即可读取（用于总览展示）
// - 其余写操作均由后端 RequireSuperAdmin 闸门保护，普通管理员访问返回 403

export type PlatformBannerListParams = {
  status?: boolean;
  type?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

export function listPlatformBanners(token: string, params: PlatformBannerListParams = {}) {
  const qs = buildQuery({
    status: typeof params.status === "boolean" ? String(params.status) : undefined,
    type: params.type,
    keyword: params.keyword,
    page: params.page,
    limit: params.limit
  });
  return apiRequest<PlatformBannerListResponse>(`/api/admin/system/banners${qs}`, { token });
}

export function getActivePlatformBanners(token: string) {
  return apiRequest<PlatformBannerItem[]>("/api/admin/system/banners/active", { token });
}

export function getPlatformBanner(token: string, id: number | string) {
  return apiRequest<PlatformBannerItem>(`/api/admin/system/banners/${id}`, { token });
}

export function createPlatformBanner(token: string, payload: PlatformBannerMutation) {
  return apiRequest<PlatformBannerItem>("/api/admin/system/banners", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updatePlatformBanner(
  token: string,
  id: number | string,
  payload: PlatformBannerMutation
) {
  return apiRequest<PlatformBannerItem>(`/api/admin/system/banners/${id}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deletePlatformBanner(token: string, id: number | string) {
  return apiRequest<{ id: number }>(`/api/admin/system/banners/${id}`, {
    method: "DELETE",
    token
  });
}

export function bulkDeletePlatformBanners(token: string, ids: number[]) {
  return apiRequest<{ affected: number }>("/api/admin/system/banners/bulk-delete", {
    method: "POST",
    token,
    body: JSON.stringify({ ids })
  });
}

// 图片上传响应：
//   - reference：持久化到 imageUrl 字段的规范形态（storage:// 引用）
//   - url：立即可用于预览的代理地址（带 ticket，30 分钟内有效）
export type PlatformBannerUploadResult = {
  reference: string;
  url: string;
  storage?: {
    config_id: number;
    provider: string;
    key: string;
    size: number;
    content_type?: string;
    url?: string;
  };
};

export function uploadPlatformBannerImage(token: string, file: File, configName?: string) {
  const fd = new FormData();
  fd.append("file", file);
  if (configName) fd.append("config_name", configName);
  return apiRequest<PlatformBannerUploadResult>("/api/admin/system/banners/upload", {
    method: "POST",
    token,
    body: fd
  });
}
