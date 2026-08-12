"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  createAdminBanner,
  createAdminNotice,
  deleteAdminBanner,
  deleteAdminBanners,
  deleteAdminNotice,
  deleteAdminNotices,
  getAdminBanners,
  getAdminNotices,
  getContentOverview,
  reorderAdminBanners,
  updateAdminBanner,
  updateAdminNotice,
  uploadAdminBannerImage,
  type AdminBannerListParams,
  type AdminNoticeListParams
} from "@/lib/api/content";
import type { BannerItem, NoticeItem } from "@/lib/api/types";

/**
 * 内容中心的 React Query hooks。
 *
 * 失效关系集中在这里而不是散在组件里：一次 Banner 写操作会同时影响
 * 「Banner 列表」与「内容总览」两处，散着写必然漏掉总览那张 ——
 * 表现为改完之后统计卡还停在旧数字，管理员会以为没保存成功。
 */

const OVERVIEW_KEY = "content-overview";
const BANNERS_KEY = "content-banners";
const NOTICES_KEY = "content-notices";

function useContentInvalidator(scope: "banner" | "notice") {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: [scope === "banner" ? BANNERS_KEY : NOTICES_KEY] }),
      queryClient.invalidateQueries({ queryKey: [OVERVIEW_KEY] })
    ]);
  };
}

export function useContentOverviewQuery(appId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [OVERVIEW_KEY, token, appId],
    queryFn: () => getContentOverview(token as string, appId as string | number),
    enabled: Boolean(token && appId)
  });
}

/* ───────────────────────── Banner ───────────────────────── */

export function useAdminBannersQuery(appId?: number | string | null, params: AdminBannerListParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [BANNERS_KEY, token, appId, params.status ?? "", params.type ?? "", params.keyword ?? ""],
    queryFn: () => getAdminBanners(token as string, appId as string | number, params),
    enabled: Boolean(token && appId)
  });
}

export function useCreateAdminBannerMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("banner");
  return useMutation({
    mutationFn: (payload: Partial<BannerItem>) => createAdminBanner(token as string, appId as string | number, payload),
    onSuccess: invalidate
  });
}

export function useUpdateAdminBannerMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("banner");
  return useMutation({
    mutationFn: (payload: { bannerId: number | string; data: Partial<BannerItem> }) =>
      updateAdminBanner(token as string, appId as string | number, payload.bannerId, payload.data),
    onSuccess: invalidate
  });
}

export function useDeleteAdminBannerMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("banner");
  return useMutation({
    mutationFn: (bannerId: number | string) => deleteAdminBanner(token as string, appId as string | number, bannerId),
    onSuccess: invalidate
  });
}

export function useDeleteAdminBannersMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("banner");
  return useMutation({
    mutationFn: (ids: number[]) => deleteAdminBanners(token as string, appId as string | number, ids),
    onSuccess: invalidate
  });
}

export function useReorderAdminBannersMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("banner");
  return useMutation({
    mutationFn: (ids: number[]) => reorderAdminBanners(token as string, appId as string | number, ids),
    onSuccess: invalidate
  });
}

export function useUploadBannerImageMutation(appId?: number | string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (file: File) => uploadAdminBannerImage(token as string, appId as string | number, file)
  });
}

/* ───────────────────────── 公告 ───────────────────────── */

export function useAdminNoticesQuery(appId?: number | string | null, params: AdminNoticeListParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      NOTICES_KEY,
      token,
      appId,
      params.status ?? "",
      params.type ?? "",
      params.level ?? "",
      params.keyword ?? "",
      params.page ?? 1,
      params.limit ?? 20
    ],
    queryFn: () => getAdminNotices(token as string, appId as string | number, params),
    enabled: Boolean(token && appId),
    // 翻页时保留上一页数据，避免表格在两页之间闪一下空态
    placeholderData: (previous) => previous
  });
}

export function useCreateAdminNoticeMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("notice");
  return useMutation({
    mutationFn: (payload: Partial<NoticeItem>) => createAdminNotice(token as string, appId as string | number, payload),
    onSuccess: invalidate
  });
}

export function useUpdateAdminNoticeMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("notice");
  return useMutation({
    mutationFn: (payload: { noticeId: number | string; data: Partial<NoticeItem> }) =>
      updateAdminNotice(token as string, appId as string | number, payload.noticeId, payload.data),
    onSuccess: invalidate
  });
}

export function useDeleteAdminNoticeMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("notice");
  return useMutation({
    mutationFn: (noticeId: number | string) => deleteAdminNotice(token as string, appId as string | number, noticeId),
    onSuccess: invalidate
  });
}

export function useDeleteAdminNoticesMutation(appId?: number | string | null) {
  const token = useAdminToken();
  const invalidate = useContentInvalidator("notice");
  return useMutation({
    mutationFn: (ids: number[]) => deleteAdminNotices(token as string, appId as string | number, ids),
    onSuccess: invalidate
  });
}
