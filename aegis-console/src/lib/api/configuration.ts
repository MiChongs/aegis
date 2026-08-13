import { apiRequest, buildQuery } from "./client";
import type {
  AdminPaymentOrderDetail,
  AdminPaymentOrderList,
  AdminUserSettingsView,
  PaymentConfig,
  PaymentProviderMeta,
  PaymentRefund,
  PaymentRefundable,
  PaymentRefundList,
  SettingsCleanupResult,
  SettingsIntegrityResult,
  SettingsInitializeResult,
  UserSettingsStats,
  VipPlan,
  VipTransaction
} from "./types";

// ── 支付订单（管理端） ──

export function getAdminPaymentOrders(
  token: string,
  payload: {
    appid: number;
    status?: string;
    payment_method?: string;
    keyword?: string;
    user_id?: number;
    page?: number;
    limit?: number;
  }
) {
  return apiRequest<AdminPaymentOrderList>("/api/admin/app/payment-config/orders/list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminPaymentOrderDetail(token: string, payload: { appid: number; order_no: string }) {
  return apiRequest<AdminPaymentOrderDetail>("/api/admin/app/payment-config/orders/detail", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── VIP 套餐（管理端） ──

export function getAdminVipPlans(token: string, appId: number | string) {
  return apiRequest<VipPlan[]>(`/api/admin/apps/${appId}/vip/plans`, { token });
}

export function saveAdminVipPlan(
  token: string,
  appId: number | string,
  payload: {
    id?: number;
    name?: string;
    durationDays?: number;
    price?: string;
    originalPrice?: string;
    bonusIntegral?: number;
    description?: string;
    isActive?: boolean;
    sortOrder?: number;
  }
) {
  return apiRequest<VipPlan>(`/api/admin/apps/${appId}/vip/plans`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminVipPlan(token: string, appId: number | string, planId: number) {
  return apiRequest<{ deleted: boolean }>(`/api/admin/apps/${appId}/vip/plans/${planId}`, {
    method: "DELETE",
    token
  });
}

export function grantAdminVip(
  token: string,
  appId: number | string,
  payload: { userId: number; days: number; reason?: string; bonusIntegral?: number }
) {
  return apiRequest<VipTransaction>(`/api/admin/apps/${appId}/vip/grant`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminVipTransactions(
  token: string,
  appId: number | string,
  params: { userId?: number; page?: number; limit?: number }
) {
  const query = buildQuery({ userId: params.userId, page: params.page, limit: params.limit });
  return apiRequest<{ items: VipTransaction[]; total: number; page: number; limit: number }>(
    `/api/admin/apps/${appId}/vip/transactions${query}`,
    { token }
  );
}

export function getAdminUserSettingsStats(token: string, appId: number | string) {
  return apiRequest<UserSettingsStats>(`/api/admin/user-settings/stats?appid=${appId}`, { token });
}

export function getAdminUserSettings(
  token: string,
  params: { appid: number | string; userId: number | string }
) {
  const query = buildQuery({ appid: params.appid, userId: params.userId });
  return apiRequest<AdminUserSettingsView>(`/api/admin/user-settings/user${query}`, { token });
}

export function batchInitializeUserSettings(
  token: string,
  payload: { appid: number; batchSize?: number; categories?: string[] }
) {
  return apiRequest<SettingsInitializeResult>("/api/admin/user-settings/batch-initialize", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function initializeUserSettings(
  token: string,
  payload: { appid: number; userId: number; categories?: string[] }
) {
  return apiRequest<SettingsInitializeResult>("/api/admin/user-settings/initialize-user", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** autoRepair=true 会就地补齐缺失的默认键；false 只报告 */
export function checkUserSettingsIntegrity(
  token: string,
  params: { appid: number | string; autoRepair?: boolean }
) {
  const query = buildQuery({ appid: params.appid, autoRepair: params.autoRepair });
  return apiRequest<SettingsIntegrityResult>(`/api/admin/user-settings/check-integrity${query}`, { token });
}

/** dryRun=true 只统计不删除 —— 清理是不可逆操作，默认应先预检 */
export function cleanupUserSettings(
  token: string,
  params: { appid: number | string; dryRun?: boolean }
) {
  const query = buildQuery({ appid: params.appid, dryRun: params.dryRun });
  return apiRequest<SettingsCleanupResult>(`/api/admin/user-settings/cleanup${query}`, {
    method: "DELETE",
    token
  });
}
/**
 * 拉取全部支付渠道的自描述元数据（含配置字段 schema）。
 * 渠道市场与配置表单完全由该结果驱动，后端新增渠道时前端无需改动。
 */
export function getAdminPaymentMethods(token: string) {
  return apiRequest<PaymentProviderMeta[]>("/api/admin/app/payment-config/methods", {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}

export function getAdminPaymentConfigs(
  token: string,
  payload: { appid: number; payment_method?: string; enabled_only?: boolean }
) {
  return apiRequest<PaymentConfig[]>("/api/admin/app/payment-config/list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminPaymentConfigDetail(token: string, payload: { appid: number; config_id: number }) {
  return apiRequest<PaymentConfig>("/api/admin/app/payment-config/detail", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function createAdminPaymentConfig(token: string, payload: Record<string, unknown>) {
  return apiRequest<PaymentConfig>("/api/admin/app/payment-config/create", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminPaymentConfig(token: string, payload: Record<string, unknown>) {
  return apiRequest<PaymentConfig>("/api/admin/app/payment-config/update", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminPaymentConfig(token: string, payload: { appid: number; config_id: number }) {
  return apiRequest<null>("/api/admin/app/payment-config/delete", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function testAdminPaymentConfig(token: string, payload: { appid: number; config_id: number }) {
  return apiRequest<Record<string, unknown>>("/api/admin/app/payment-config/test", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 退款（管理端） ──

/** 查询订单可退款额度与渠道退款能力（发起退款前的前置校验依据） */
export function getAdminPaymentRefundable(token: string, payload: { appid: number; order_no: string }) {
  return apiRequest<PaymentRefundable>("/api/admin/app/payment-config/refunds/refundable", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 发起退款。amount 留空表示按剩余可退额度全额退款 */
export function createAdminPaymentRefund(
  token: string,
  payload: { appid: number; order_no: string; amount?: string; reason?: string; reverse_fulfillment?: boolean }
) {
  return apiRequest<PaymentRefund>("/api/admin/app/payment-config/refunds/create", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminPaymentRefunds(
  token: string,
  payload: { appid: number; status?: string; payment_method?: string; keyword?: string; page?: number; limit?: number }
) {
  return apiRequest<PaymentRefundList>("/api/admin/app/payment-config/refunds/list", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminOrderRefunds(token: string, payload: { appid: number; order_no: string }) {
  return apiRequest<PaymentRefund[]>("/api/admin/app/payment-config/refunds/order", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 主动向上游同步退款单状态（processing 态的补偿通道） */
export function syncAdminPaymentRefund(token: string, payload: { appid: number; refund_no: string }) {
  return apiRequest<PaymentRefund>("/api/admin/app/payment-config/refunds/sync", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function initEpayPaymentConfig(token: string, payload: { appid: number; epay_config: Record<string, unknown> }) {
  return apiRequest<PaymentConfig>("/api/admin/app/payment-config/epay/init", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}
