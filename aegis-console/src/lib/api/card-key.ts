import { apiRequest, ApiError, buildQuery, joinApiUrl } from "@/lib/api/client";

/**
 * 卡密域 API。
 *
 * 一张卡有两种形态：授权卡（卡即登录凭证）与兑换卡（给已登录用户发权益），
 * 共用同一套生成、作废、核销与权益目录。
 */

export type CardKeyKind = "login" | "redeem";
export type CardKeyStatus = "unused" | "active" | "used" | "disabled";
export type CardKeyValidityMode = "permanent" | "fixed_until" | "days_from_first_use";

/** 权益取值形态，决定 Reward 上用哪个字段承载数量 */
export type CardKeyRewardValue = "amount" | "money" | "ref";

/**
 * 一档权益的自述。**由后端目录下发**，前端不得另抄一份 ——
 * 否则会同时招来「后端加了一档控制台配不出来」与「控制台能配、保存时报不支持」，
 * 而两边都没有报错提示。
 */
export type CardKeyRewardSpec = {
  type: string;
  label: string;
  hint: string;
  value: CardKeyRewardValue;
  unit?: string;
  min?: number;
  max?: number;
  needsLoginCard?: boolean;
};

export type CardKeyCatalog = {
  rewards: CardKeyRewardSpec[];
  kinds: CardKeyKind[];
  validityModes: CardKeyValidityMode[];
  maxRewards: number;
};

export type CardKeyReward = {
  type: string;
  amount?: number;
  /** 金额按字符串传输：JSON number 是双精度浮点，0.1 走一遍会变成 0.09999999999999999 */
  money?: string;
  refId?: number;
};

export type CardKeyRewardResult = {
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
};

export type CardKeyBatchStats = {
  total: number;
  unused: number;
  active: number;
  used: number;
  disabled: number;
  expired: number;
};

export type CardKeyBatch = {
  id: number;
  appid: number;
  name: string;
  kind: CardKeyKind;
  remark?: string;
  codePrefix?: string;
  segments: number;
  segmentLength: number;
  rewards: CardKeyReward[];
  maxDevices: number;
  validityMode: CardKeyValidityMode;
  validityDays: number;
  validUntil?: string;
  total: number;
  status: "active" | "disabled";
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
  stats?: CardKeyBatchStats;
};

export type CardKey = {
  id: number;
  appid: number;
  batchId: number;
  code: string;
  kind: CardKeyKind;
  status: CardKeyStatus;
  boundUserId?: number;
  maxDevices: number;
  activatedAt?: string;
  expiresAt?: string;
  usedAt?: string;
  disabledAt?: string;
  disabledReason?: string;
  remark?: string;
  createdAt: string;
  updatedAt: string;
  batchName?: string;
  boundAccount?: string;
  deviceCount: number;
};

export type CardKeyDevice = {
  id: number;
  cardKeyId: number;
  deviceId: string;
  deviceName?: string;
  firstSeenAt: string;
  lastSeenAt: string;
  seenCount: number;
};

export type CardKeyRedemption = {
  id: number;
  appid: number;
  cardKeyId: number;
  batchId?: number;
  code: string;
  userId: number;
  rewards: CardKeyReward[];
  results: CardKeyRewardResult[];
  source: "redeem" | "login" | "admin";
  deviceId?: string;
  clientIp?: string;
  userAgent?: string;
  operator?: string;
  createdAt: string;
  account?: string;
  batchName?: string;
};

export type CardKeyPage = {
  items: CardKey[];
  total: number;
  page: number;
  limit: number;
};

export type CardKeyRedemptionPage = {
  items: CardKeyRedemption[];
  total: number;
  page: number;
  limit: number;
};

export type GenerateCardKeysPayload = {
  name: string;
  kind: CardKeyKind;
  remark?: string;
  count: number;
  codePrefix?: string;
  segments?: number;
  segmentLength?: number;
  rewards: CardKeyReward[];
  maxDevices?: number;
  validityMode?: CardKeyValidityMode;
  validityDays?: number;
  validUntil?: string | null;
};

export type CardKeyListParams = {
  batchId?: number;
  status?: string;
  kind?: string;
  keyword?: string;
  userId?: number;
  page?: number;
  limit?: number;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/card-keys`;

// ── 目录 ──

export function getCardKeyCatalog(token: string, appKey: string) {
  return apiRequest<CardKeyCatalog>(`${base(appKey)}/catalog`, { token });
}

// ── 批次 ──

export function listCardKeyBatches(token: string, appKey: string) {
  return apiRequest<CardKeyBatch[]>(`${base(appKey)}/batches`, { token });
}

export function generateCardKeys(token: string, appKey: string, payload: GenerateCardKeysPayload) {
  return apiRequest<CardKeyBatch>(`${base(appKey)}/batches`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function setCardKeyBatchStatus(token: string, appKey: string, batchId: number, enabled: boolean) {
  return apiRequest<{ enabled: boolean }>(`${base(appKey)}/batches/${batchId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ enabled })
  });
}

export function deleteCardKeyBatch(token: string, appKey: string, batchId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/batches/${batchId}`, {
    method: "DELETE",
    token
  });
}

// ── 单卡 ──

export function listCardKeys(token: string, appKey: string, params?: CardKeyListParams) {
  return apiRequest<CardKeyPage>(`${base(appKey)}/codes${buildQuery(params ?? {})}`, { token });
}

export function disableCardKeys(token: string, appKey: string, ids: number[], reason?: string) {
  return apiRequest<{ affected: number }>(`${base(appKey)}/codes/disable`, {
    method: "POST",
    token,
    body: JSON.stringify({ ids, reason })
  });
}

export function restoreCardKeys(token: string, appKey: string, ids: number[]) {
  return apiRequest<{ affected: number }>(`${base(appKey)}/codes/restore`, {
    method: "POST",
    token,
    body: JSON.stringify({ ids })
  });
}

export function listCardKeyDevices(token: string, appKey: string, cardId: number) {
  return apiRequest<CardKeyDevice[]>(`${base(appKey)}/codes/${cardId}/devices`, { token });
}

export function unbindCardKeyDevice(token: string, appKey: string, cardId: number, deviceId: string) {
  return apiRequest<{ unbound: boolean }>(
    `${base(appKey)}/codes/${cardId}/devices/${encodeURIComponent(deviceId)}`,
    { method: "DELETE", token }
  );
}

// ── 核销记录 ──

export function listCardKeyRedemptions(
  token: string,
  appKey: string,
  params?: { batchId?: number; userId?: number; keyword?: string; page?: number; limit?: number }
) {
  return apiRequest<CardKeyRedemptionPage>(`${base(appKey)}/redemptions${buildQuery(params ?? {})}`, {
    token
  });
}

// ── 导出 ──
//
// 返回的是 CSV 二进制，不能走 apiRequest（它按 JSON 信封解析）；
// 也不能用裸 <a href> —— 令牌只在 Authorization 头里，直链会 401。
// 与 app-users.ts / organization.ts 的导出同一套做法。
//
// 按 batchId 导出而不是按当前筛选：生成完立刻导出时列表可能还没刷新到那一批，
// 按筛选导出会得到一份不完整的卡，而那份文件是要发给渠道的。
export async function downloadCardKeyBatchCsv(token: string, appKey: string, batchId: number) {
  const response = await fetch(joinApiUrl(`${base(appKey)}/batches/${batchId}/export`), {
    headers: { Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store"
  });
  if (!response.ok) {
    let message = `导出失败（HTTP ${response.status}）`;
    try {
      const body = await response.json();
      if (body?.message) message = body.message;
    } catch {
      // 响应不是 JSON，保留默认提示
    }
    throw new ApiError(message, { status: response.status });
  }

  const blob = await response.blob();
  const disposition = response.headers.get("content-disposition") || "";
  const match = /filename\*?=(?:UTF-8'')?"?([^;"]+)"?/i.exec(disposition);
  const filename = match ? decodeURIComponent(match[1]) : `card_keys_${batchId}.csv`;

  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
