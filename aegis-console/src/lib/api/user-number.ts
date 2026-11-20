import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 用户号码 API（管理端，docs/user-number.md「管理端接口」）。
 *
 * 类型与后端 internal/domain/usernumber/*.go、internal/service/user_number_service.go 逐字段对应。
 * 号码在请求里一律用字符串传（避免长号在 JS 里被当成数字处理），响应里是整数（≤ 12 位，number 精确）。
 *
 * 权限：读 `app:number:read`，改设置与保留号 `app:number:write`，指定 / 回滚 / 调权益 `app:number:grant`。
 */

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/numbers`;

export const NUMBER_READ = "app:number:read";
export const NUMBER_WRITE = "app:number:write";
export const NUMBER_GRANT = "app:number:grant";

// ── 等级 ──

export type NumberTierSpec = {
  level: number;
  key: string;
  name: string;
  /** 建议主色（#RRGGBB） */
  color: string;
};

/**
 * 五级的本地兜底，与 usernumber.Tiers() 一致。目录接口回来前、以及奖励编辑器里
 * 「靓号券等级」下拉（不值得为它单独请求号码目录）都用这一份。
 */
export const NUMBER_TIERS: NumberTierSpec[] = [
  { level: 0, key: "common", name: "普通", color: "#7A8594" },
  { level: 1, key: "fine", name: "精品", color: "#2F9E6E" },
  { level: 2, key: "rare", name: "稀有", color: "#2F6FDE" },
  { level: 3, key: "legend", name: "传说", color: "#8B4BDB" },
  { level: 4, key: "supreme", name: "至尊", color: "#D4A017" }
];

/** 靓号券可选的等级（1–4） */
export const NUMBER_TICKET_LEVELS = NUMBER_TIERS.filter((tier) => tier.level > 0);

export function numberTierOf(level?: number | null): NumberTierSpec {
  const index = Math.max(0, Math.min(NUMBER_TIERS.length - 1, Math.round(level ?? 0)));
  return NUMBER_TIERS[index];
}

/** 靓号券名称：「稀有靓号券」 */
export function numberTicketName(level?: number | null) {
  return `${numberTierOf(level && level > 0 ? level : 1).name}靓号券`;
}

/** 权益中文名，与 usernumber.RightName 同一口径 */
export function numberRightName(kind: string, level = 0) {
  switch (kind) {
    case "change":
      return "改号卡";
    case "draw":
      return "摇号次数";
    case "ticket":
      return numberTicketName(level);
  }
  return kind;
}

// ── 目录 ──

export type NumberRightKind = "change" | "ticket" | "draw";

export type NumberPattern = {
  key: string;
  name: string;
  example: string;
  defaultTier: number;
  /** 本应用里实际落在哪一级；-1 停用 */
  tier: number;
};

export type NumberRight = {
  kind: NumberRightKind;
  level: number;
  name: string;
  balance: number;
  earned: number;
  used: number;
};

export type NumberRights = {
  change: number;
  draw: number;
  tickets: NumberRight[] | null;
  items: NumberRight[] | null;
};

export type NumberCatalog = {
  tiers: NumberTierSpec[];
  patterns: NumberPattern[];
  rights: NumberRight[];
  origins: Array<{ key: string; label: string }>;
  limits: { hardMinLength: number; hardMaxLength: number; minDefaultLength: number };
};

// ── 设置 ──

export type NumberSettings = {
  enabled: boolean;
  minLength: number;
  maxLength: number;
  defaultLength: number;
  selfPick: boolean;
  cooldownDays: number;
  freezeDays: number;
  drawCandidates: number;
  drawHoldMinutes: number;
  /** 下标即等级 */
  drawWeights: number[];
  luckSpread: number;
  luckSupreme: boolean;
  /** 规律落在哪一级的覆盖（-1 停用）；不写的用默认 */
  patternTiers?: Record<string, number> | null;
  updatedBy?: string;
  updatedAt?: string | null;
};

export function defaultNumberSettings(): NumberSettings {
  return {
    enabled: true,
    minLength: 5,
    maxLength: 12,
    defaultLength: 9,
    selfPick: true,
    cooldownDays: 7,
    freezeDays: 90,
    drawCandidates: 6,
    drawHoldMinutes: 15,
    drawWeights: [70, 20, 7, 2.5, 0.5],
    luckSpread: 0.3,
    luckSupreme: true,
    patternTiers: null
  };
}

// ── 总览 ──

export type NumberHoldKind = "reserved" | "frozen" | "candidate";

export type NumberOverview = {
  users: number;
  numbered: number;
  byTier: Array<{ tier: number; count: number }> | null;
  changesToday: number;
  changes7d: number;
  drawsToday: number;
  openDraws: number;
  holds: Partial<Record<NumberHoldKind, number>> | null;
  outstanding: NumberRight[] | null;
};

// ── 记录 ──

export type NumberCandidate = { number: number; tier: number; patterns?: string[] | null };

export type NumberDrawStatus = "open" | "picked" | "discarded" | "expired";

export type NumberDraw = {
  id: number;
  appid: number;
  userId: number;
  candidates: NumberCandidate[] | null;
  luck: number;
  status: NumberDrawStatus;
  picked?: number | null;
  createdAt: string;
  expiresAt: string;
  closedAt?: string | null;
};

export type NumberHistorySource = "initial" | "change" | "ticket" | "draw" | "frozen" | "admin" | "grant" | "rollback";

export type NumberHistory = {
  id: number;
  appid: number;
  userId: number;
  oldNumber?: number | null;
  newNumber: number;
  tier: number;
  source: NumberHistorySource | string;
  rightKind?: string;
  rightLevel: number;
  drawId?: number | null;
  operator?: string;
  note?: string;
  createdAt: string;
  account?: string;
  nickname?: string;
};

export type NumberLedgerEntry = {
  id: number;
  appid: number;
  userId: number;
  kind: NumberRightKind | string;
  level: number;
  name: string;
  delta: number;
  balanceAfter: number;
  origin: string;
  originLabel: string;
  ref?: string;
  title?: string;
  operator?: string;
  createdAt: string;
  account?: string;
  nickname?: string;
};

export type NumberHold = {
  appid: number;
  number: number;
  kind: NumberHoldKind;
  tier: number;
  userId?: number | null;
  drawId?: number | null;
  note?: string;
  expiresAt?: string | null;
  createdBy?: string;
  createdAt: string;
};

export type NumberPage<T> = { items: T[] | null; total: number; page: number; limit: number };

export type NumberListParams = { userId?: number; kind?: string; page?: number; limit?: number };

// ── 查号与用户 ──

export type NumberInspect = {
  number: number;
  tier: number;
  tierName: string;
  patterns: string[] | null;
  valid: boolean;
  patternNames: string[] | null;
  ownerId?: number;
  ownerAccount?: string;
  ownerName?: string;
  hold?: NumberHold | null;
};

/** 今日号运（抽奖每日运气的同一模型）；没开号运时只有 odds / baseOdds */
export type NumberLuck = {
  factor?: number;
  percentile?: number;
  level?: string;
  title?: string;
  message?: string;
  low?: number;
  high?: number;
  resetsAt?: string;
  supreme?: unknown;
  odds: number[];
  baseOdds: number[];
};

export type NumberAdminUser = {
  userId: number;
  number: number;
  tier: number;
  tierName: string;
  patterns: string[] | null;
  patternNames: string[] | null;
  changedAt?: string | null;
  nextChangeAt?: string | null;
  rights: NumberRights;
  draw?: NumberDraw | null;
  frozen: NumberHold[] | null;
  history: NumberHistory[] | null;
  luck: NumberLuck;
};

export type NumberReserveResult = { reserved: number[] | null; skipped: string[] | null };

// ── 请求 ──

export function getNumberCatalog(token: string, appKey: string) {
  return apiRequest<NumberCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getNumberOverview(token: string, appKey: string) {
  return apiRequest<NumberOverview>(`${base(appKey)}/overview`, { token });
}

export function getNumberSettings(token: string, appKey: string) {
  return apiRequest<NumberSettings>(`${base(appKey)}/settings`, { token });
}

/** 整份替换 */
export function saveNumberSettings(token: string, appKey: string, settings: NumberSettings) {
  return apiRequest<NumberSettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(settings)
  });
}

/** 用一份未保存的设置试算某个号运下各级的概率（百分比，下标即等级） */
export function previewNumberOdds(token: string, appKey: string, settings: NumberSettings, luck: number) {
  return apiRequest<{ odds: number[]; luck: number }>(`${base(appKey)}/settings/odds`, {
    method: "POST",
    token,
    body: JSON.stringify({ settings, luck })
  });
}

export function inspectNumber(token: string, appKey: string, number: string) {
  return apiRequest<NumberInspect>(`${base(appKey)}/inspect${buildQuery({ number })}`, { token });
}

export function getNumberUser(token: string, appKey: string, userId: number) {
  return apiRequest<NumberAdminUser>(`${base(appKey)}/users/${userId}`, { token });
}

/** 指定号码：不扣权益、不看冷却；force 可以拿保留号、别人的冻结号与候选号（仍不能抢正在用的号） */
export function assignNumber(token: string, appKey: string, userId: number, payload: { number: string; force: boolean; note?: string }) {
  return apiRequest<NumberHistory>(`${base(appKey)}/users/${userId}/assign`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 回到某次换号之前的号码 */
export function rollbackNumber(token: string, appKey: string, userId: number, historyId: number) {
  return apiRequest<NumberHistory>(`${base(appKey)}/users/${userId}/rollback`, {
    method: "POST",
    token,
    body: JSON.stringify({ historyId })
  });
}

/** 调整权益余额：delta 为正发放、为负扣减（扣成负数 42953） */
export function adjustNumberRight(
  token: string,
  appKey: string,
  userId: number,
  payload: { kind: NumberRightKind; level: number; delta: number; note?: string }
) {
  return apiRequest<NumberRights>(`${base(appKey)}/users/${userId}/rights`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function listNumberHistory(token: string, appKey: string, params: NumberListParams) {
  return apiRequest<NumberPage<NumberHistory>>(`${base(appKey)}/history${buildQuery(params)}`, { token });
}

export function listNumberLedger(token: string, appKey: string, params: NumberListParams) {
  return apiRequest<NumberPage<NumberLedgerEntry>>(`${base(appKey)}/ledger${buildQuery(params)}`, { token });
}

export function listNumberDraws(token: string, appKey: string, params: NumberListParams) {
  return apiRequest<NumberPage<NumberDraw>>(`${base(appKey)}/draws${buildQuery(params)}`, { token });
}

export function listNumberHolds(token: string, appKey: string, params: NumberListParams) {
  return apiRequest<NumberPage<NumberHold>>(`${base(appKey)}/holds${buildQuery(params)}`, { token });
}

/** 保留一批号码（≤ 500，有人在用或格式不对的跳过）；expiresAt 为 RFC 3339，留空长期保留 */
export function reserveNumbers(token: string, appKey: string, payload: { numbers: string[]; note?: string; expiresAt?: string | null }) {
  return apiRequest<NumberReserveResult>(`${base(appKey)}/holds`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 删掉一条占用：保留号放出、冻结提前解除、候选释放 */
export function releaseNumberHold(token: string, appKey: string, number: number | string) {
  return apiRequest<null>(`${base(appKey)}/holds/${encodeURIComponent(String(number))}`, { method: "DELETE", token });
}
