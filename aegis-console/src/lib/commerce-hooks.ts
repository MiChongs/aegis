"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as commerce from "@/lib/api/commerce";
import { useAdminAppUserQuery, useAdminProfileQuery, useAdminToken } from "@/lib/admin-hooks";

/**
 * 交易中心的 React Query hooks。
 *
 * 与风控 / 工单同一约定：一个域一个 hooks 文件，失效关系集中在这里定义。
 * 散落在组件里的失效调用是「调完账余额没变」这类问题最常见的根源。
 */

const COMMERCE_KEYS = {
  overview: ["commerce-overview"] as const,
  walletTransactions: ["commerce-wallet-transactions"] as const,
  walletStats: ["commerce-wallet-stats"] as const,
  receiptCapability: ["commerce-receipt-capability"] as const,
};

function useInvalidate() {
  const qc = useQueryClient();
  return (...keys: readonly (readonly string[])[]) => {
    keys.forEach((key) => qc.invalidateQueries({ queryKey: key }));
  };
}

export type CommerceWindow = { start?: string; end?: string };

export function useCommerceOverviewQuery(appKey?: string | null, window: CommerceWindow = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...COMMERCE_KEYS.overview, token, appKey, window.start, window.end],
    queryFn: () => commerce.getCommerceOverview(token!, appKey!, window),
    enabled: Boolean(token && appKey),
  });
}

export type WalletTransactionFilters = {
  userId?: number;
  type?: string;
  direction?: string;
  keyword?: string;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export function useAdminWalletTransactionsQuery(appKey?: string | null, filters: WalletTransactionFilters = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...COMMERCE_KEYS.walletTransactions, token, appKey, filters],
    queryFn: () => commerce.getAdminWalletTransactions(token!, appKey!, filters),
    enabled: Boolean(token && appKey),
    // 资金列表翻页时保留上一页，避免每次翻页整表闪一下骨架屏
    placeholderData: (previous) => previous,
  });
}

export function useAdminWalletStatsQuery(appKey?: string | null, window: CommerceWindow = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...COMMERCE_KEYS.walletStats, token, appKey, window.start, window.end],
    queryFn: () => commerce.getAdminWalletStats(token!, appKey!, window),
    enabled: Boolean(token && appKey),
  });
}

/**
 * 凭证能力自述。
 *
 * 与语言选择器绑在一起：缺中日韩字体时那几种语言会被降级成英文，
 * 提前灰掉比让用户下载到一份英文 PDF 之后再来问要好得多。
 */
export function usePaymentReceiptCapabilityQuery(appId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...COMMERCE_KEYS.receiptCapability, token, appId],
    queryFn: () => commerce.getPaymentReceiptCapability(token!, appId!),
    enabled: Boolean(token && appId),
    staleTime: 5 * 60 * 1000,
  });
}

/** 管理员调账。调完要连带刷新流水与资金面板 —— 只刷一个就会出现「余额变了、流水没这一条」。 */
export function useAdjustWalletMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (payload: { userId: number; amount: string; reason?: string }) =>
      commerce.adjustAdminWallet(token!, appKey!, payload),
    onSuccess: () =>
      invalidate(COMMERCE_KEYS.walletTransactions, COMMERCE_KEYS.walletStats, COMMERCE_KEYS.overview),
  });
}

export function useDownloadWalletReceiptMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: { transactionNo: string; locale?: string }) =>
      commerce.downloadWalletReceipt(token!, appKey!, payload),
  });
}

export function useEmailWalletReceiptMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: { transactionNo: string; to: string; locale?: string }) =>
      commerce.emailWalletReceipt(token!, appKey!, payload),
  });
}

export function useDownloadOrderReceiptMutation(appId?: number | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: { order_no: string; locale?: string }) =>
      commerce.downloadOrderReceipt(token!, { appid: appId!, ...payload }),
  });
}

export function useEmailOrderReceiptMutation(appId?: number | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: { order_no: string; email: string; locale?: string }) =>
      commerce.emailOrderReceipt(token!, { appid: appId!, ...payload }),
  });
}

/**
 * 凭证寄送的候选收件人。
 *
 * 九成场景就是「寄回给下单用户」或「寄给我自己」。把这两个地址算出来一键填入，
 * 省掉「切到用户详情页复制邮箱、切回来粘贴」这一趟 —— 那一趟里还有抄错的风险，
 * 而抄错的后果是把交易明细发给了别人。
 */
export function useReceiptRecipients(appKey?: string | null, userId?: number | null) {
  // 只在真的要寄的时候才拉用户详情：列表每行都预取一次没有意义
  const user = useAdminAppUserQuery(userId ? appKey : null, userId ?? null);
  const profile = useAdminProfileQuery();

  const suggestions: Array<{ email: string; label: string }> = [];
  const customerEmail = (user.data?.profile?.email || user.data?.email || "").trim();
  if (customerEmail) suggestions.push({ email: customerEmail, label: "下单用户" });
  const adminEmail = (profile.data?.account?.email || "").trim();
  if (adminEmail && adminEmail !== customerEmail) suggestions.push({ email: adminEmail, label: "我自己" });
  return suggestions;
}
