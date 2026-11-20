import { apiRequest, ApiError, buildQuery, joinApiUrl } from "@/lib/api/client";

/**
 * 卡密域 API。
 *
 * 一张卡有两种形态：授权卡（卡即登录凭证）与兑换卡（给已登录用户发权益）。
 * 兑换卡又分一卡一用（maxUses = 1）与活动码（maxUses > 1，多人各用一次）。
 * 契约与后端 internal/transport/http/cardkey_handlers.go、internal/domain/cardkey 一一对应（docs/card-key.md）。
 */

export type CardKeyKind = "login" | "redeem";
export type CardKeyStatus = "unused" | "active" | "used" | "disabled";
export type CardKeyValidityMode = "permanent" | "fixed_until" | "days_from_first_use";
export type CardKeyCharset = "safe" | "digits" | "letters";
export type CardKeyAudience = "all" | "vip" | "non_vip";
export type CardKeyOrigin = "generated" | "imported";
export type CardKeySource = "redeem" | "login" | "admin";
export type CardKeyExpiryAction = "extend" | "set" | "permanent";

/** 列表筛选里的状态：后端四档之外，「部分使用」「已过期」是按次数与时间算出来的 */
export type CardKeyStatusFilter = CardKeyStatus | "partial" | "expired";

/**
 * 权益取值形态，决定 Reward 上用哪个字段承载数量。
 * feature 为功能标识 + 天数（Reward.feature + Reward.amount，0 为永久），可在一张卡里出现多次。
 * number_ticket 为靓号券：Reward.amount 张数 + Reward.refId 等级 1–4（docs/user-number.md）。
 */
export type CardKeyRewardValue = "amount" | "money" | "ref" | "feature" | "number_ticket" | "cosmetic";

/**
 * 一档权益的自述。**由后端目录下发**，前端不得另抄一份 ——
 * 否则会同时招来「后端加了一档控制台配不出来」与「控制台能配、保存时报不支持」。
 */
export type CardKeyRewardSpec = {
  type: string;
  label: string;
  hint: string;
  value: CardKeyRewardValue;
  unit?: string;
  min?: number;
  max?: number;
  repeatable?: boolean;
  needsLoginCard?: boolean;
};

export type CardKeyPlanRef = {
  id: number;
  name: string;
  durationDays: number;
  lifetime: boolean;
  trial: boolean;
  features?: string[];
};

export type CardKeyFeatureRef = {
  tag: string;
  name: string;
  isActive: boolean;
};

export type CardKeyCatalog = {
  rewards: CardKeyRewardSpec[];
  kinds: CardKeyKind[];
  validityModes: CardKeyValidityMode[];
  charsets: CardKeyCharset[];
  audiences: CardKeyAudience[];
  maxRewards: number;
  maxCount: number;
  plans: CardKeyPlanRef[];
  features: CardKeyFeatureRef[];
};

export type CardKeyReward = {
  type: string;
  amount?: number;
  /** 金额按字符串传输：JSON number 是双精度浮点，0.1 走一遍会变成 0.09999999999999999 */
  money?: string;
  refId?: number;
  /** 功能标识，只在取值形态为 feature 时携带 */
  feature?: string;
};

export type CardKeyRewardResult = {
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
  /** 功能解锁 / 授权时长发放后的到期时间 */
  expireAt?: string;
};

export type CardKeyBatchStats = {
  total: number;
  unused: number;
  active: number;
  used: number;
  disabled: number;
  expired: number;
  /** 已交给渠道 */
  distributed: number;
  /** 还能提取：未使用、未提取、未过期 */
  available: number;
  /** 累计使用人次（活动码一张卡算多次） */
  uses: number;
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
  charset: CardKeyCharset;
  origin: CardKeyOrigin;
  rewards: CardKeyReward[];
  maxDevices: number;
  maxUses: number;
  perUserLimit: number;
  audience: CardKeyAudience;
  startsAt?: string;
  validityMode: CardKeyValidityMode;
  validityDays: number;
  validUntil?: string;
  selfUnbindLimit: number;
  channel?: string;
  /** 面值，两位小数的字符串 */
  faceValue: string;
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
  maxUses: number;
  useCount: number;
  activatedAt?: string;
  expiresAt?: string;
  usedAt?: string;
  disabledAt?: string;
  disabledReason?: string;
  distributedAt?: string;
  distributionNote?: string;
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

export type CardKeyUnbind = {
  id: number;
  cardKeyId: number;
  deviceId: string;
  deviceName?: string;
  source: "self" | "admin";
  operator?: string;
  createdAt: string;
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
  source: CardKeySource;
  deviceId?: string;
  clientIp?: string;
  userAgent?: string;
  operator?: string;
  createdAt: string;
  account?: string;
  nickname?: string;
  batchName?: string;
  kind?: CardKeyKind;
};

export type CardKeyDetail = {
  card: CardKey;
  batch: CardKeyBatch;
  devices: CardKeyDevice[];
  redemptions: CardKeyRedemption[];
  unbinds: CardKeyUnbind[];
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

export type CardKeyStats = {
  batches: number;
  cards: number;
  unused: number;
  available: number;
  distributed: number;
  disabled: number;
  expired: number;
  activeLicenses: number;
  redemptions: number;
  redemptionsToday: number;
  users: number;
  faceValueIssued: string;
  faceValueUsed: string;
  trend: Array<{ day: string; redemptions: number; users: number }>;
  sources: Array<{ source: CardKeySource; count: number }>;
  topBatches: Array<{ batchId: number; name: string; kind: CardKeyKind; redemptions: number }>;
};

export type CardKeyRedeemResult = {
  code: string;
  kind: CardKeyKind;
  batchName?: string;
  results: CardKeyRewardResult[];
  expiresAt?: string;
  redeemedAt: string;
};

/** 生成（或导入）一批卡密。codes 非空时按导入处理，count 与卡面格式不再生效。 */
export type GenerateCardKeysPayload = {
  name: string;
  kind: CardKeyKind;
  remark?: string;
  count?: number;
  codes?: string[];
  codePrefix?: string;
  segments?: number;
  segmentLength?: number;
  charset?: CardKeyCharset;
  rewards: CardKeyReward[];
  maxDevices?: number;
  maxUses?: number;
  perUserLimit?: number;
  audience?: CardKeyAudience;
  startsAt?: string | null;
  validityMode?: CardKeyValidityMode;
  validityDays?: number;
  validUntil?: string | null;
  selfUnbindLimit?: number;
  channel?: string;
  faceValue?: string;
};

/** 修改批次：卡面格式、形态、有效期模式与单卡设备数不能改 */
export type UpdateCardKeyBatchPayload = {
  name: string;
  remark?: string;
  channel?: string;
  faceValue?: string;
  rewards: CardKeyReward[];
  perUserLimit?: number;
  audience?: CardKeyAudience;
  startsAt?: string | null;
  validUntil?: string | null;
  selfUnbindLimit?: number;
};

export type UpdateCardKeyPayload = {
  remark?: string;
  maxDevices?: number;
  expiryAction?: CardKeyExpiryAction;
  extendDays?: number;
  expiresAt?: string;
};

export type CardKeyListParams = {
  batchId?: number;
  status?: CardKeyStatusFilter;
  kind?: CardKeyKind;
  keyword?: string;
  userId?: number;
  /** yes 只看已提取 / no 只看未提取 */
  distributed?: "yes" | "no";
  page?: number;
  limit?: number;
};

export type CardKeyRedemptionParams = {
  batchId?: number;
  cardId?: number;
  userId?: number;
  source?: CardKeySource;
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type CardKeyExportOptions = {
  format: "csv" | "txt";
  /** 只导出仍可使用的卡 */
  unusedOnly?: boolean;
  /** 只导出还没交给渠道的卡 */
  undistributedOnly?: boolean;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/card-keys`;

function post<T>(token: string, path: string, body: unknown, method = "POST") {
  return apiRequest<T>(path, { method, token, body: JSON.stringify(body) });
}

// ── 目录与统计 ──

export function getCardKeyCatalog(token: string, appKey: string) {
  return apiRequest<CardKeyCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getCardKeyStats(token: string, appKey: string, days: number, tz: string) {
  return apiRequest<CardKeyStats>(`${base(appKey)}/stats${buildQuery({ days, tz })}`, { token });
}

// ── 批次 ──

export function listCardKeyBatches(token: string, appKey: string, params?: { keyword?: string; kind?: CardKeyKind }) {
  return apiRequest<CardKeyBatch[]>(`${base(appKey)}/batches${buildQuery(params ?? {})}`, { token });
}

export function getCardKeyBatch(token: string, appKey: string, batchId: number) {
  return apiRequest<CardKeyBatch>(`${base(appKey)}/batches/${batchId}`, { token });
}

export function generateCardKeys(token: string, appKey: string, payload: GenerateCardKeysPayload) {
  return post<CardKeyBatch>(token, `${base(appKey)}/batches`, payload);
}

export function updateCardKeyBatch(token: string, appKey: string, batchId: number, payload: UpdateCardKeyBatchPayload) {
  return post<CardKeyBatch>(token, `${base(appKey)}/batches/${batchId}`, payload, "PUT");
}

export function setCardKeyBatchStatus(token: string, appKey: string, batchId: number, enabled: boolean) {
  return post<{ enabled: boolean }>(token, `${base(appKey)}/batches/${batchId}/status`, { enabled }, "PUT");
}

export function deleteCardKeyBatch(token: string, appKey: string, batchId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/batches/${batchId}`, { method: "DELETE", token });
}

export function appendCardKeys(token: string, appKey: string, batchId: number, count: number) {
  return post<CardKeyBatch>(token, `${base(appKey)}/batches/${batchId}/append`, { count });
}

export function extractCardKeys(token: string, appKey: string, batchId: number, count: number, note: string) {
  return post<{ batch: CardKeyBatch; items: CardKey[] }>(token, `${base(appKey)}/batches/${batchId}/extract`, {
    count,
    note
  });
}

// ── 单卡 ──

export function listCardKeys(token: string, appKey: string, params?: CardKeyListParams) {
  return apiRequest<CardKeyPage>(`${base(appKey)}/codes${buildQuery(params ?? {})}`, { token });
}

export function getCardKeyDetail(token: string, appKey: string, cardId: number) {
  return apiRequest<CardKeyDetail>(`${base(appKey)}/codes/${cardId}`, { token });
}

export function updateCardKey(token: string, appKey: string, cardId: number, payload: UpdateCardKeyPayload) {
  return post<CardKey>(token, `${base(appKey)}/codes/${cardId}`, payload, "PUT");
}

export function disableCardKeys(token: string, appKey: string, ids: number[], reason?: string) {
  return post<{ affected: number }>(token, `${base(appKey)}/codes/disable`, { ids, reason });
}

export function restoreCardKeys(token: string, appKey: string, ids: number[]) {
  return post<{ affected: number }>(token, `${base(appKey)}/codes/restore`, { ids });
}

export function deleteCardKeys(token: string, appKey: string, ids: number[]) {
  return post<{ affected: number; skipped: number }>(token, `${base(appKey)}/codes/delete`, { ids });
}

export function distributeCardKeys(token: string, appKey: string, ids: number[], distributed: boolean, note?: string) {
  return post<{ affected: number }>(token, `${base(appKey)}/codes/distribute`, { ids, distributed, note });
}

export function unbindCardKeyDevice(token: string, appKey: string, cardId: number, deviceId: string) {
  return apiRequest<{ unbound: boolean }>(
    `${base(appKey)}/codes/${cardId}/devices/${encodeURIComponent(deviceId)}`,
    { method: "DELETE", token }
  );
}

export function resetCardKeyDevices(token: string, appKey: string, cardId: number) {
  return apiRequest<{ affected: number }>(`${base(appKey)}/codes/${cardId}/devices`, { method: "DELETE", token });
}

// ── 核销记录与代兑 ──

export function listCardKeyRedemptions(token: string, appKey: string, params?: CardKeyRedemptionParams) {
  return apiRequest<CardKeyRedemptionPage>(`${base(appKey)}/redemptions${buildQuery(params ?? {})}`, { token });
}

export function adminRedeemCardKey(
  token: string,
  appKey: string,
  payload: { code: string; userId?: number; account?: string }
) {
  return post<CardKeyRedeemResult>(token, `${base(appKey)}/redeem`, payload);
}

// ── 导出 ──
//
// 返回的是 CSV / TXT 二进制，不能走 apiRequest（它按 JSON 信封解析）；
// 也不能用裸 <a href> —— 令牌只在 Authorization 头里，直链会 401。
//
// 按 batchId 导出而不是按当前筛选：生成完立刻导出时列表可能还没刷新到那一批，
// 按筛选导出会得到一份不完整的卡，而那份文件是要发给渠道的。

export function cardKeyExportPath(appKey: string, batchId: number, options: CardKeyExportOptions) {
  return `${base(appKey)}/batches/${batchId}/export${buildQuery({
    format: options.format,
    status: options.unusedOnly ? "unused" : undefined,
    undistributed: options.undistributedOnly ? 1 : undefined
  })}`;
}

export async function downloadCardKeyBatch(
  token: string,
  appKey: string,
  batchId: number,
  options: CardKeyExportOptions
) {
  const response = await fetch(joinApiUrl(cardKeyExportPath(appKey, batchId, options)), {
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
  const filename = match ? decodeURIComponent(match[1]) : `card_keys_${batchId}.${options.format}`;
  saveBlob(blob, filename);
}

/** 把一段文本存成文件（提卡结果「下载 TXT」） */
export function saveTextFile(text: string, filename: string) {
  saveBlob(new Blob([text], { type: "text/plain;charset=utf-8" }), filename);
}

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
