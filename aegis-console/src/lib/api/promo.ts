import { apiRequest, buildQuery } from "@/lib/api/client";
import type { InviteReward, InviteRewardResult } from "@/lib/api/invite";
import type { PointsStorePage, PointsStoreRewardSpec } from "@/lib/api/points-store";

/**
 * 推广活动域 API。
 *
 * 一期活动有两项任务：邀请好友（活动期内邀请满某档人数自己领奖）与分享推广（在允许的平台发文、
 * 提交链接、审核通过后发奖）。奖励目录与发放沿用邀请（`invite.RewardCatalog`），平台目录与写死的
 * 禁用平台来自 `/promo/catalog`。能否领取、能否提交全部在后端判定，控制台只配置、审核与展示。
 * 规则见 docs/promo.md。
 */

export type PromoStatus = "draft" | "online" | "offline";
export type PromoPhase = "upcoming" | "active" | "ended";
export type PromoSubmissionStatus = "pending" | "approved" | "rejected";
export type PromoReviewAction = "approve" | "reject";

/** 一项权益，与邀请同一套目录与账本 */
export type PromoReward = InviteReward;
export type PromoRewardResult = InviteRewardResult;

export type PromoTier = {
  count: number;
  title?: string;
  rewards: PromoReward[];
};

export type PromoInviteTask = {
  enabled: boolean;
  tiers: PromoTier[];
  leaderboard: boolean;
  leaderboardSize: number;
};

export type PromoShareTask = {
  enabled: boolean;
  /** 启用的平台键（目录里的） */
  platforms: string[];
  /** 每篇审核通过的文章发一次 */
  rewards: PromoReward[];
  maxApprovedPerUser: number;
  maxApprovedPerPlatform: number;
  dailySubmitLimit: number;
  /** 额外禁用的域名（写死的禁用平台之外） */
  blockedDomains: string[];
  /** 推荐文案：{code} {link} {app} {platform} */
  shareTemplate: string;
};

export type PromoConfig = {
  claimGraceDays: number;
  invite: PromoInviteTask;
  share: PromoShareTask;
};

export type PromoCampaign = {
  id: number;
  appid: number;
  title: string;
  subtitle: string;
  rulesText: string;
  status: PromoStatus;
  startsAt: string;
  endsAt: string;
  config: PromoConfig;
  createdBy?: string;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
};

/** 管理端列表里的一期活动，带简要统计 */
export type PromoAdminCampaign = PromoCampaign & {
  phase: PromoPhase;
  /** 领过档位或提交过的人数 */
  participants: number;
  /** 档位领取次数 */
  tierClaims: number;
  pendingSubmissions: number;
  approvedSubmissions: number;
};

export type PromoCampaignPayload = {
  title: string;
  subtitle: string;
  rulesText: string;
  startsAt: string;
  endsAt: string;
  config: PromoConfig;
};

export type PromoCampaignParams = {
  status?: PromoStatus;
  keyword?: string;
  page?: number;
  limit?: number;
};

/** 可以发文推广的平台 */
export type PromoPlatform = {
  key: string;
  name: string;
  hint?: string;
  domains: string[];
  packageName?: string;
  defaultOn?: boolean;
};

/** 写死的禁用平台：任何活动都不认，控制台关不掉 */
export type PromoForbiddenPlatform = {
  name: string;
  domains: string[];
};

/** 目录里的上限；缺省时控制台按 docs/promo.md 的数值兜底，最终以后端校验为准 */
export type PromoCatalogLimits = Partial<{
  maxRewards: number;
  maxTiers: number;
  maxTierCount: number;
  maxTitleLength: number;
  maxSubtitleLength: number;
  maxRulesLength: number;
  maxTierTitleLength: number;
  maxShareLength: number;
  maxDurationDays: number;
  maxClaimGraceDays: number;
  minLeaderboardSize: number;
  maxLeaderboardSize: number;
  maxPerUserLimit: number;
  maxBlockedDomains: number;
  maxRejectReasonLength: number;
  maxReviewBatch: number;
}>;

export type PromoCatalog = {
  platforms: PromoPlatform[];
  forbidden: PromoForbiddenPlatform[];
  rewards: PointsStoreRewardSpec[];
  defaults?: Partial<PromoConfig>;
  limits?: PromoCatalogLimits;
};

export type PromoSubmission = {
  id: number;
  appid: number;
  campaignId: number;
  userId: number;
  platform: string;
  url: string;
  note?: string;
  status: PromoSubmissionStatus;
  rejectReason?: string;
  results: PromoRewardResult[] | null;
  reviewedBy?: string;
  reviewedAt?: string;
  deviceId?: string;
  clientIp?: string;
  createdAt: string;
  updatedAt: string;
  platformName?: string;
  campaignTitle?: string;
  userAccount?: string;
  userNickname?: string;
  userAvatar?: string;
};

export type PromoSubmissionParams = {
  campaignId?: number;
  status?: PromoSubmissionStatus;
  platform?: string;
  userId?: number;
  keyword?: string;
  page?: number;
  limit?: number;
};

export type PromoReviewPayload = {
  ids: number[];
  action: PromoReviewAction;
  reason: string;
};

/** 一条提交的审核结论 */
export type PromoReviewOutcome = {
  id: number;
  ok: boolean;
  status?: PromoSubmissionStatus;
  reason?: string;
  message?: string;
  results?: PromoRewardResult[];
};

export type PromoSubmissionCounts = {
  pending: number;
  approved: number;
  rejected: number;
};

export type PromoStats = {
  participants: number;
  invited: number;
  pendingInvites: number;
  tierClaims: Array<{ count: number; claimed: number }>;
  submissions: PromoSubmissionCounts;
  byPlatform: Array<PromoSubmissionCounts & { platform: string; name: string }>;
  daily: Array<{ date: string; invited: number; submitted: number; approved: number }>;
};

/** 排行榜一行（管理端不打码，带账号） */
export type PromoRankEntry = {
  rank: number;
  userId?: number;
  account?: string;
  nickname: string;
  avatar?: string;
  count: number;
  reachedAt?: string;
};

export type PromoPage<T> = PointsStorePage<T>;

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/promo`;

// ── 目录 ──

export function getPromoCatalog(token: string, appKey: string) {
  return apiRequest<PromoCatalog>(`${base(appKey)}/catalog`, { token });
}

// ── 活动 ──

export function listPromoCampaigns(token: string, appKey: string, params?: PromoCampaignParams) {
  return apiRequest<PromoPage<PromoAdminCampaign>>(`${base(appKey)}/campaigns${buildQuery(params ?? {})}`, { token });
}

export function getPromoCampaign(token: string, appKey: string, id: number) {
  return apiRequest<PromoCampaign>(`${base(appKey)}/campaigns/${id}`, { token });
}

/** 新建的活动默认是草稿 */
export function createPromoCampaign(token: string, appKey: string, payload: PromoCampaignPayload) {
  return apiRequest<PromoCampaign>(`${base(appKey)}/campaigns`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updatePromoCampaign(token: string, appKey: string, id: number, payload: PromoCampaignPayload) {
  return apiRequest<PromoCampaign>(`${base(appKey)}/campaigns/${id}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function setPromoCampaignStatus(token: string, appKey: string, id: number, status: PromoStatus) {
  return apiRequest<PromoCampaign>(`${base(appKey)}/campaigns/${id}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

/** 只有草稿与下线、且没人领取或提交过的活动能删；其余后端拒绝，错误原样展示 */
export function deletePromoCampaign(token: string, appKey: string, id: number) {
  return apiRequest<unknown>(`${base(appKey)}/campaigns/${id}`, { method: "DELETE", token });
}

export function getPromoStats(token: string, appKey: string, id: number) {
  return apiRequest<PromoStats>(`${base(appKey)}/campaigns/${id}/stats`, { token });
}

/** 后端返回 `{ items }`（与用户端排行同形，管理端不带 me）；兼容直接返回数组 */
export async function getPromoLeaderboard(token: string, appKey: string, id: number) {
  const data = await apiRequest<{ items?: PromoRankEntry[] | null } | PromoRankEntry[] | null>(
    `${base(appKey)}/campaigns/${id}/leaderboard`,
    { token }
  );
  if (Array.isArray(data)) return data;
  return data?.items ?? [];
}

// ── 分享提交 ──

export function listPromoSubmissions(token: string, appKey: string, params?: PromoSubmissionParams) {
  return apiRequest<PromoPage<PromoSubmission>>(`${base(appKey)}/submissions${buildQuery(params ?? {})}`, { token });
}

/** 批量审核（ids 最多 100 个，逐条处理）。后端返回 `{ items }`；兼容直接返回数组 */
export async function reviewPromoSubmissions(token: string, appKey: string, payload: PromoReviewPayload) {
  const data = await apiRequest<{ items?: PromoReviewOutcome[] | null } | PromoReviewOutcome[] | null>(
    `${base(appKey)}/submissions/review`,
    {
      method: "POST",
      token,
      body: JSON.stringify(payload)
    }
  );
  if (Array.isArray(data)) return data;
  return data?.items ?? [];
}
