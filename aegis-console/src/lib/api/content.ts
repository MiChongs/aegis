import { apiRequest } from "./client";
import type { BannerItem, NoticeItem } from "./types";

export function getAdminBanners(token: string, appId: number | string) {
  return apiRequest<BannerItem[]>(`/api/admin/apps/${appId}/banners`, { token });
}

export function createAdminBanner(
  token: string,
  appId: number | string,
  payload: Partial<BannerItem>
) {
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

export function getAdminNotices(token: string, appId: number | string) {
  return apiRequest<NoticeItem[]>(`/api/admin/apps/${appId}/notices`, { token });
}

export function createAdminNotice(
  token: string,
  appId: number | string,
  payload: Partial<NoticeItem>
) {
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
