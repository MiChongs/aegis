import { ApiError, apiRequest, buildQuery, joinApiUrl } from "./client";
import type {
  AdminWalletPackage,
  AdminWalletTransactionList,
  CommerceOverview,
  PaymentReceiptCapability,
  PaymentReceiptEmailResult,
  WalletAdjustBucket,
  WalletPackage,
  WalletPackagePayload,
  WalletSettings,
  WalletSettingsView,
  WalletStats
} from "./types";

/**
 * 交易中心的取数入口。
 *
 * 与 `configuration.ts` 的分工是「运营 vs 配置」：那边是渠道密钥、套餐定价这类
 * 应用配置，这边是订单、退款、钱包流水、凭证这类**已经发生的**资金记录。
 * 混在一起会让一个只想看今天流水的人，先翻过一整屏商户号输入框。
 */

const app = (appKey: string | number) => `/api/admin/apps/${appKey}`;

// ── 概览 ──

export function getCommerceOverview(
  token: string,
  appKey: string | number,
  params: { start?: string; end?: string } = {}
) {
  return apiRequest<CommerceOverview>(`${app(appKey)}/commerce/overview${buildQuery(params)}`, { token });
}

// ── 钱包流水（全应用）──

export function getAdminWalletTransactions(
  token: string,
  appKey: string | number,
  params: {
    userId?: number;
    type?: string;
    direction?: string;
    keyword?: string;
    start?: string;
    end?: string;
    page?: number;
    limit?: number;
  } = {}
) {
  return apiRequest<AdminWalletTransactionList>(`${app(appKey)}/wallet/transactions${buildQuery(params)}`, { token });
}

export function getAdminWalletStats(
  token: string,
  appKey: string | number,
  params: { start?: string; end?: string } = {}
) {
  return apiRequest<WalletStats>(`${app(appKey)}/wallet/stats${buildQuery(params)}`, { token });
}

/** bucket：cash 充值余额 / bonus 赠送余额 / 不传即默认（入账进赠送余额、扣减先扣赠送余额） */
export function adjustAdminWallet(
  token: string,
  appKey: string | number,
  payload: { userId: number; amount: string; reason?: string; bucket?: WalletAdjustBucket }
) {
  return apiRequest<unknown>(`${app(appKey)}/wallet/adjust`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 钱包设置与充值档位（docs/wallet.md）──
//
// 读走 app:wallet:read，写走 app:wallet:config。

export function getWalletSettings(token: string, appKey: string | number) {
  return apiRequest<WalletSettingsView>(`${app(appKey)}/wallet/settings`, { token });
}

/** 全量覆盖：九个字段整体提交（rate、currency 只读，不提交） */
export function updateWalletSettings(token: string, appKey: string | number, payload: WalletSettings) {
  return apiRequest<WalletSettingsView>(`${app(appKey)}/wallet/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export async function listWalletPackages(token: string, appKey: string | number) {
  const data = await apiRequest<{ items?: AdminWalletPackage[] | null }>(`${app(appKey)}/wallet/packages`, { token });
  return data.items ?? [];
}

export function createWalletPackage(token: string, appKey: string | number, payload: WalletPackagePayload) {
  return apiRequest<WalletPackage>(`${app(appKey)}/wallet/packages`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateWalletPackage(
  token: string,
  appKey: string | number,
  packageId: number,
  payload: WalletPackagePayload
) {
  return apiRequest<WalletPackage>(`${app(appKey)}/wallet/packages/${packageId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

/** 删除档位；已下的订单按快照履约，不受影响 */
export function deleteWalletPackage(token: string, appKey: string | number, packageId: number) {
  return apiRequest<{ id: number }>(`${app(appKey)}/wallet/packages/${packageId}`, { method: "DELETE", token });
}

/** 按 ids 的先后重写排序 */
export function reorderWalletPackages(token: string, appKey: string | number, ids: number[]) {
  return apiRequest<{ ids: number[] }>(`${app(appKey)}/wallet/packages-order`, {
    method: "PUT",
    token,
    body: JSON.stringify({ ids })
  });
}

// ── 凭证 ──

export function getPaymentReceiptCapability(token: string, appId: number) {
  return apiRequest<PaymentReceiptCapability>("/api/admin/app/payment-config/receipt/options", {
    method: "POST",
    token,
    body: JSON.stringify({ appid: appId })
  });
}

export function emailOrderReceipt(
  token: string,
  payload: { appid: number; order_no: string; email: string; locale?: string; documentType?: string; timezone?: string }
) {
  return apiRequest<PaymentReceiptEmailResult>("/api/admin/app/payment-config/orders/receipt/email", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function emailWalletReceipt(
  token: string,
  appKey: string | number,
  payload: { transactionNo: string; to: string; locale?: string; documentType?: string; timezone?: string }
) {
  return apiRequest<PaymentReceiptEmailResult>(`${app(appKey)}/wallet/receipt/email`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 凭证 PDF 下载 ──
//
// 凭证是二进制，走不了 apiRequest（它按 JSON 信封解包）。管理端两条下载都是
// **POST + Bearer**，因此也不能简单地把地址塞进 <a href>：浏览器发出的
// 那个请求不带任何头，后端会 401。统一在这里 fetch 成 blob 再触发下载。

async function downloadPdf(url: string, token: string, body: unknown, fallbackName: string) {
  const response = await fetch(joinApiUrl(url), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "X-Admin-Token": token,
      "Content-Type": "application/json"
    },
    body: JSON.stringify(body),
    cache: "no-store"
  });
  if (!response.ok) {
    let message = `凭证生成失败（HTTP ${response.status}）`;
    try {
      const payload = await response.json();
      if (payload?.message) message = payload.message;
    } catch {
      // 响应不是 JSON，保留默认提示
    }
    throw new ApiError(message, { status: response.status });
  }

  const blob = await response.blob();
  const disposition = response.headers.get("content-disposition") || "";
  const match = /filename\*?=(?:UTF-8'')?"?([^;"]+)"?/i.exec(disposition);
  const filename = match ? decodeURIComponent(match[1]) : fallbackName;

  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectUrl;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(objectUrl);
  return filename;
}

export function downloadOrderReceipt(
  token: string,
  payload: { appid: number; order_no: string; locale?: string; documentType?: string; timezone?: string }
) {
  return downloadPdf(
    "/api/admin/app/payment-config/orders/receipt",
    token,
    payload,
    `receipt_${payload.order_no}.pdf`
  );
}

export function downloadWalletReceipt(
  token: string,
  appKey: string | number,
  payload: { transactionNo: string; locale?: string; documentType?: string; timezone?: string }
) {
  return downloadPdf(
    `${app(appKey)}/wallet/receipt`,
    token,
    payload,
    `receipt_${payload.transactionNo}.pdf`
  );
}
