"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createCouponTemplate,
  createPromotion,
  deleteCouponTemplate,
  deletePromotion,
  getCouponCatalog,
  getCouponOverview,
  issueCoupons,
  listCouponTemplates,
  listOrderPromotions,
  listPromotions,
  listUserCoupons,
  quoteCoupon,
  revokeUserCoupon,
  setCouponTemplateStatus,
  setPromotionStatus,
  updateCouponTemplate,
  updatePromotion,
  type CouponQuotePayload,
  type CouponStatus,
  type CouponTemplatePayload,
  type CouponTemplateQuery,
  type OrderPromotionQuery,
  type PromotionPayload,
  type PromotionQuery,
  type UserCouponQuery
} from "@/lib/api/coupons";
import { useAdminToken } from "@/lib/admin-hooks";
import { rememberCouponNames } from "@/lib/coupon-names";
import { browserTimeZone } from "@/lib/datetime";

/**
 * 优惠系统 hooks（docs/coupons.md）。
 *
 * 写操作后优惠相关查询一起失效：发券会同时改变概览、券的已发数、用户券列表；改活动会影响试算。
 * 逐个挑着失效迟早漏一个。
 */

const KEY = {
  catalog: "admin-coupon-catalog",
  overview: "admin-coupon-overview",
  templates: "admin-coupon-templates",
  userCoupons: "admin-coupon-user-coupons",
  promotions: "admin-coupon-promotions",
  orders: "admin-coupon-orders",
  quote: "admin-coupon-quote"
} as const;

const ALL_KEYS = Object.values(KEY).filter((key) => key !== KEY.catalog);

export function useCouponCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getCouponCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 5 * 60_000
  });
}

export function useCouponOverviewQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  const tz = browserTimeZone();
  return useQuery({
    queryKey: [KEY.overview, token, appKey, tz, days],
    queryFn: () => getCouponOverview(token as string, appKey as string, tz, days),
    enabled: Boolean(token && appKey)
  });
}

export function useCouponTemplatesQuery(appKey: string | null | undefined, query: CouponTemplateQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.templates, token, appKey, query.status ?? "", query.keyword ?? "", query.page ?? 1, query.limit ?? 20],
    queryFn: async () => {
      const page = await listCouponTemplates(token as string, appKey as string, query);
      rememberCouponNames(page.items);
      return page;
    },
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

/** 选择器与名称登记用：一次取最多 200 种券（不分状态） */
export function useAllCouponTemplatesQuery(appKey?: string | null) {
  return useCouponTemplatesQuery(appKey, { limit: 200 });
}

export function useUserCouponsQuery(appKey: string | null | undefined, query: UserCouponQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.userCoupons,
      token,
      appKey,
      query.templateId ?? 0,
      query.userId ?? 0,
      query.status ?? "",
      query.source ?? "",
      query.keyword ?? "",
      query.page ?? 1,
      query.limit ?? 20
    ],
    queryFn: () => listUserCoupons(token as string, appKey as string, query),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function usePromotionsQuery(appKey: string | null | undefined, query: PromotionQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.promotions, token, appKey, query.kind ?? "", query.status ?? "", query.keyword ?? "", query.page ?? 1, query.limit ?? 20],
    queryFn: () => listPromotions(token as string, appKey as string, query),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useOrderPromotionsQuery(appKey: string | null | undefined, query: OrderPromotionQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.orders,
      token,
      appKey,
      query.promotionId ?? 0,
      query.templateId ?? 0,
      query.userId ?? 0,
      query.status ?? "",
      query.keyword ?? "",
      query.page ?? 1,
      query.limit ?? 20
    ],
    queryFn: () => listOrderPromotions(token as string, appKey as string, query),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

function useInvalidateCoupons() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(ALL_KEYS.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useSaveCouponTemplateMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: ({ id, payload }: { id?: number; payload: CouponTemplatePayload }) =>
      id ? updateCouponTemplate(token as string, appKey, id, payload) : createCouponTemplate(token as string, appKey, payload),
    onSuccess: invalidate
  });
}

export function useSetCouponTemplateStatusMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: ({ id, status }: { id: number; status: CouponStatus }) => setCouponTemplateStatus(token as string, appKey, id, status),
    onSuccess: invalidate
  });
}

export function useDeleteCouponTemplateMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: (id: number) => deleteCouponTemplate(token as string, appKey, id),
    onSuccess: invalidate
  });
}

export function useIssueCouponsMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: ({ id, ...payload }: { id: number; userIds: number[]; count: number; notify: boolean }) =>
      issueCoupons(token as string, appKey, id, payload),
    onSuccess: invalidate
  });
}

export function useRevokeUserCouponMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: ({ id, reason }: { id: number; reason?: string }) => revokeUserCoupon(token as string, appKey, id, reason),
    onSuccess: invalidate
  });
}

export function useSavePromotionMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: ({ id, payload }: { id?: number; payload: PromotionPayload }) =>
      id ? updatePromotion(token as string, appKey, id, payload) : createPromotion(token as string, appKey, payload),
    onSuccess: invalidate
  });
}

export function useSetPromotionStatusMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: ({ id, status }: { id: number; status: CouponStatus }) => setPromotionStatus(token as string, appKey, id, status),
    onSuccess: invalidate
  });
}

export function useDeletePromotionMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCoupons();
  return useMutation({
    mutationFn: (id: number) => deletePromotion(token as string, appKey, id),
    onSuccess: invalidate
  });
}

/** 试算是 POST 但只算不写：用 mutation 承载「点一下算一次」 */
export function useCouponQuoteMutation(appKey: string) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: CouponQuotePayload) => quoteCoupon(token as string, appKey, payload)
  });
}
