import { apiRequest, buildQuery } from "./client";
import type { GeoBanEntry, IPBanEntry, IPBanMode, IPBanPage } from "./types";

/**
 * IP 封禁批量管理（docs/firewall.md「封禁」）。对齐后端：
 * internal/domain/firewall/types.go（IPBanFilter、IPBanStats、BanBatchResult、IPInspection）、
 * internal/transport/http/ip_ban_dto.go、ip_ban_batch_handlers.go。仅超级管理员。
 */

const BASE = "/api/admin/system/firewall";
type QueryParams = Parameters<typeof buildQuery>[0];

/** 来源：manual 管理员手封、auto 自动封禁（拦截次数越线）、risk 风控平台处置、rule 防火墙自定义规则 */
export type BanSource = "manual" | "auto" | "risk" | "rule" | string;
export type BanStatus = "active" | "expired" | "revoked";

/** 列表 / 计数 / 按筛选解封共用的筛选 */
export interface BanFilter {
  page?: number;
  pageSize?: number;
  ip?: string;
  status?: "all" | BanStatus | "";
  source?: BanSource | "all" | "";
  /** 国家代码 */
  country?: string;
  /** 响应模式；"default" = 沿用全局默认 */
  mode?: IPBanMode | "default" | "";
  /** IP 前缀 / 原因 / 触发规则 / 运营商 / 城市 */
  keyword?: string;
  /** 只看这么多秒内到期的 */
  expiringWithin?: number;
  permanent?: "true" | "false" | "";
  sort?: "created_desc" | "created_asc" | "expires_asc" | "ip";
}

export interface RankedItem {
  key: string;
  count: number;
}

export interface BanDaily {
  day: string;
  created: number;
  revoked: number;
  expired: number;
}

export interface BanStats {
  active: number;
  permanent: number;
  /** 24 小时内到期 */
  expiringSoon: number;
  created24h: number;
  revoked24h: number;
  expired24h: number;
  bySource: RankedItem[];
  /** key 为模式，"default" = 沿用全局默认 */
  byMode: RankedItem[];
  /** key 为国家代码，"??" = 未知 */
  byCountry: RankedItem[];
  /** 近 14 天 */
  daily: BanDaily[];
}

export interface BanBatchItem {
  ip: string;
  banId?: number;
  /** created | exists | invalid | failed | revoked | updated */
  status: string;
  error?: string;
}

export interface BanBatchResult {
  total: number;
  succeeded: number;
  skipped: number;
  failed: number;
  items: BanBatchItem[];
  /** 按筛选解封超过单次上限（5000）只处理了前一部分 */
  truncated?: boolean;
}

export interface BanDecision {
  banned: boolean;
  mode: string;
  reason: string;
  banId?: number;
  delayMs?: number;
}

/** 某个 IP 现在会不会被拦、被哪一层拦 */
export interface IPInspection {
  ip: string;
  blocked: boolean;
  /** ip_ban（精确封禁）/ geo_ban（地域规则）/ geo_fence（地理围栏） */
  layer?: "ip_ban" | "geo_ban" | "geo_fence";
  decision: BanDecision;
  activeBan?: IPBanEntry;
  history: IPBanEntry[];
  blocks24h: number;
  blocks7d: number;
  country: string;
  countryCode: string;
  region: string;
  city: string;
  isp: string;
  asn: string;
  geoRule?: GeoBanEntry;
}

export interface BanBatchCreateInput {
  ips: string[];
  reason?: string;
  /** 秒，0 = 永久 */
  duration: number;
  mode?: IPBanMode | "";
  delayMs?: number;
}

/** 修改生效中的封禁；字段缺省 = 不改 */
export interface BanUpdateInput {
  mode?: IPBanMode | "";
  reason?: string;
  /** 从现在起算的秒数，0 = 改为永久 */
  duration?: number;
  delayMs?: number;
}

export const firewallBansApi = {
  list: (token: string, filter: BanFilter) => apiRequest<IPBanPage>(`${BASE}/bans${buildQuery({ ...filter } as QueryParams)}`, { token }),
  count: (token: string, filter: BanFilter) =>
    apiRequest<{ total: number }>(`${BASE}/bans/count${buildQuery({ ...filter, page: undefined, pageSize: undefined } as QueryParams)}`, { token }),
  stats: (token: string) => apiRequest<BanStats>(`${BASE}/bans/stats`, { token }),
  inspect: (token: string, ip: string) => apiRequest<IPInspection>(`${BASE}/bans/inspect${buildQuery({ ip })}`, { token }),
  batchBan: (token: string, input: BanBatchCreateInput) =>
    apiRequest<BanBatchResult>(`${BASE}/bans/batch`, { method: "POST", token, body: JSON.stringify(input) }),
  /** 解封选中的记录 */
  unbanIds: (token: string, ids: number[], note?: string) =>
    apiRequest<BanBatchResult>(`${BASE}/bans/unban`, { method: "POST", token, body: JSON.stringify({ ids, note }) }),
  /** 按筛选解封全部匹配的生效中封禁；expected 必须等于确认时看到的条数，否则 40990 */
  unbanFilter: (token: string, filter: BanFilter, expected: number, note?: string) =>
    apiRequest<BanBatchResult>(`${BASE}/bans/unban`, {
      method: "POST",
      token,
      body: JSON.stringify({ filter: { ...filter, page: undefined, pageSize: undefined, status: "active" }, expected, note })
    }),
  update: (token: string, id: number, input: BanUpdateInput) =>
    apiRequest<IPBanEntry>(`${BASE}/bans/${id}`, { method: "PATCH", token, body: JSON.stringify(input) }),
  batchUpdate: (token: string, ids: number[], input: BanUpdateInput) =>
    apiRequest<BanBatchResult>(`${BASE}/bans/update`, { method: "POST", token, body: JSON.stringify({ ids, ...input }) }),
  unbanOne: (token: string, id: number) => apiRequest<{ banId: number; revokedAt: string }>(`${BASE}/bans/${id}`, { method: "DELETE", token }),
  geoBatch: (token: string, ids: number[], action: "enable" | "disable" | "delete") =>
    apiRequest<{ processed: number; total: number }>(`${BASE}/geo-bans/batch`, { method: "POST", token, body: JSON.stringify({ ids, action }) })
};
