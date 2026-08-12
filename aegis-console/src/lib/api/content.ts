import { apiRequest, buildQuery } from "./client";
import type {
  BannerItem,
  ContentImageUploadResult,
  ContentOverview,
  NoticeItem,
  NoticeListResponse
} from "./types";

/**
 * 应用级内容中心。
 *
 * Banner 与公告的列表形状刻意不同，不是遗漏：
 *   - Banner 返回裸数组，因为它要拖拽排序，分页会让第 2 页的第 1 条拖不到第 1 页去
 *   - 公告返回分页信封，因为它会持续累积，运营两年的应用有几千条
 */

export type AdminBannerListParams = {
  status?: "enabled" | "disabled";
  type?: string;
  keyword?: string;
};

export type AdminNoticeListParams = {
  status?: string;
  type?: string;
  level?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

export function getContentOverview(token: string, appId: number | string) {
  return apiRequest<ContentOverview>(`/api/admin/apps/${appId}/content/overview`, { token });
}

/* ───────────────────────── Banner ───────────────────────── */

export function getAdminBanners(token: string, appId: number | string, params: AdminBannerListParams = {}) {
  return apiRequest<BannerItem[]>(`/api/admin/apps/${appId}/banners${buildQuery({ ...params })}`, { token });
}

export function createAdminBanner(token: string, appId: number | string, payload: Partial<BannerItem>) {
  return apiRequest<BannerItem>(`/api/admin/apps/${appId}/banners`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminBanner(
  token: string,
  appId: number | string,
  bannerId: number | string,
  payload: Partial<BannerItem>
) {
  return apiRequest<BannerItem>(`/api/admin/apps/${appId}/banners/${bannerId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminBanner(token: string, appId: number | string, bannerId: number | string) {
  return apiRequest<{ id: number }>(`/api/admin/apps/${appId}/banners/${bannerId}`, {
    method: "DELETE",
    token
  });
}

export function deleteAdminBanners(token: string, appId: number | string, ids: number[]) {
  return apiRequest<{ deleted?: number; ids?: number[] }>(`/api/admin/apps/${appId}/banners`, {
    method: "DELETE",
    token,
    body: JSON.stringify({ ids })
  });
}

/** 拖拽排序：一次提交完整顺序，返回重排后的完整列表。 */
export function reorderAdminBanners(token: string, appId: number | string, ids: number[]) {
  return apiRequest<BannerItem[]>(`/api/admin/apps/${appId}/banners/order`, {
    method: "PUT",
    token,
    body: JSON.stringify({ ids })
  });
}

/**
 * 上传 Banner 图片。
 * 返回的 `reference` 是要落库的持久形态（`storage://…`），`url` 只用于当场预览 ——
 * 后者带票据会过期，存进去过两天就是死链。
 */
export function uploadAdminBannerImage(token: string, appId: number | string, file: File, configName?: string) {
  const form = new FormData();
  form.append("file", file);
  if (configName) form.append("config_name", configName);
  return apiRequest<ContentImageUploadResult>(`/api/admin/apps/${appId}/banners/image`, {
    method: "POST",
    token,
    body: form
  });
}

/* ───────────────────────── 公告 ───────────────────────── */

export function getAdminNotices(token: string, appId: number | string, params: AdminNoticeListParams = {}) {
  return apiRequest<NoticeListResponse>(`/api/admin/apps/${appId}/notices${buildQuery({ ...params })}`, { token });
}

export function createAdminNotice(token: string, appId: number | string, payload: Partial<NoticeItem>) {
  return apiRequest<NoticeItem>(`/api/admin/apps/${appId}/notices`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminNotice(
  token: string,
  appId: number | string,
  noticeId: number | string,
  payload: Partial<NoticeItem>
) {
  return apiRequest<NoticeItem>(`/api/admin/apps/${appId}/notices/${noticeId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminNotice(token: string, appId: number | string, noticeId: number | string) {
  return apiRequest<{ id: number }>(`/api/admin/apps/${appId}/notices/${noticeId}`, {
    method: "DELETE",
    token
  });
}

export function deleteAdminNotices(token: string, appId: number | string, ids: number[]) {
  return apiRequest<{ deleted?: number; ids?: number[] }>(`/api/admin/apps/${appId}/notices`, {
    method: "DELETE",
    token,
    body: JSON.stringify({ ids })
  });
}
