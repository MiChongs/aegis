import { apiRequest, buildQuery } from "./client";

// ── 类型 ──

export type LotteryActivity = {
  id: number;
  appid: number;
  name: string;
  description?: string;
  uiMode: "wheel" | "grid";
  status: "draft" | "active" | "paused" | "ended";
  joinMode: "manual" | "auto" | "both";
  autoJoinRules?: Record<string, unknown>;
  costType: "free" | "points";
  costAmount: number;
  dailyLimit: number;
  totalLimit: number;
  startTime: string;
  endTime: string;
  seedHash?: string;
  seedValue?: string;
  chainTxHash?: string;
  chainNetwork?: string;
  createdAt: string;
  updatedAt: string;
};

export type LotteryPrize = {
  id: number;
  activityId: number;
  name: string;
  type: "points" | "experience" | "item" | "coupon" | "custom";
  value: string;
  imageUrl?: string;
  quantity: number;
  used: number;
  weight: number;
  position: number;
  isDefault: boolean;
  extra?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type LotteryDraw = {
  id: number;
  activityId: number;
  userId: number;
  prizeId: number;
  prizeSnapshot: Record<string, unknown>;
  drawSeed?: string;
  drawProof?: string;
  status: "awarded" | "claimed" | "expired";
  claimedAt?: string;
  createdAt: string;
};

export type LotteryDrawResult = {
  draw: LotteryDraw;
  prize: LotteryPrize;
  seedHash?: string;
  drawSeed?: string;
  drawProof?: string;
  prizeIndex: number;
};

export type LotterySeedCommitment = {
  id: number;
  activityId: number;
  round: number;
  seedHash: string;
  seedValue?: string;
  committedAt: string;
  revealedAt?: string;
  chainTxHash?: string;
};

export type LotteryVerifyResult = {
  valid: boolean;
  seedHash: string;
  seedValue?: string;
  chainTxHash?: string;
  chainNetwork?: string;
  message: string;
};

export type LotteryActivityStats = {
  activityId: number;
  totalDraws: number;
  totalUsers: number;
  participants: number;
  prizeStats: { prizeId: number; name: string; type: string; quantity: number; used: number; weight: number }[];
};

export type PagedResult<T> = {
  items: T[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

// ── 管理员 API ──

export function getAdminLotteryActivities(token: string, appKey: string, params?: { status?: string; keyword?: string; page?: number; limit?: number }) {
  return apiRequest<PagedResult<LotteryActivity>>(`/api/admin/apps/${appKey}/lottery/activities${buildQuery(params ?? {})}`, { token });
}

export function getAdminLotteryActivity(token: string, appKey: string, id: number) {
  return apiRequest<LotteryActivity>(`/api/admin/apps/${appKey}/lottery/activities/${id}`, { token });
}

export function createAdminLotteryActivity(token: string, appKey: string, payload: {
  name: string; description?: string; uiMode?: string; joinMode?: string;
  autoJoinRules?: Record<string, unknown>; costType?: string; costAmount?: number;
  dailyLimit?: number; totalLimit?: number; startTime: string; endTime: string;
}) {
  return apiRequest<LotteryActivity>(`/api/admin/apps/${appKey}/lottery/activities`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAdminLotteryActivity(token: string, appKey: string, id: number, payload: Record<string, unknown>) {
  return apiRequest<LotteryActivity>(`/api/admin/apps/${appKey}/lottery/activities/${id}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteAdminLotteryActivity(token: string, appKey: string, id: number) {
  return apiRequest<void>(`/api/admin/apps/${appKey}/lottery/activities/${id}`, { method: "DELETE", token });
}

export function commitLotterySeed(token: string, appKey: string, id: number) {
  return apiRequest<LotterySeedCommitment>(`/api/admin/apps/${appKey}/lottery/activities/${id}/commit-seed`, { method: "POST", token });
}

export function revealLotterySeed(token: string, appKey: string, id: number) {
  return apiRequest<LotterySeedCommitment>(`/api/admin/apps/${appKey}/lottery/activities/${id}/reveal-seed`, { method: "POST", token });
}

export function getAdminLotteryPrizes(token: string, appKey: string, activityId: number) {
  return apiRequest<LotteryPrize[]>(`/api/admin/apps/${appKey}/lottery/activities/${activityId}/prizes`, { token });
}

export function createAdminLotteryPrize(token: string, appKey: string, activityId: number, payload: {
  name: string; type: string; value?: string; imageUrl?: string; quantity?: number; weight?: number; position?: number; isDefault?: boolean;
}) {
  return apiRequest<LotteryPrize>(`/api/admin/apps/${appKey}/lottery/activities/${activityId}/prizes`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAdminLotteryPrize(token: string, appKey: string, activityId: number, prizeId: number, payload: Record<string, unknown>) {
  return apiRequest<LotteryPrize>(`/api/admin/apps/${appKey}/lottery/activities/${activityId}/prizes/${prizeId}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteAdminLotteryPrize(token: string, appKey: string, activityId: number, prizeId: number) {
  return apiRequest<void>(`/api/admin/apps/${appKey}/lottery/activities/${activityId}/prizes/${prizeId}`, { method: "DELETE", token });
}

export function getAdminLotteryDraws(token: string, appKey: string, activityId: number, params?: { page?: number; limit?: number }) {
  return apiRequest<PagedResult<LotteryDraw>>(`/api/admin/apps/${appKey}/lottery/activities/${activityId}/draws${buildQuery(params ?? {})}`, { token });
}

export function getAdminLotteryStats(token: string, appKey: string, activityId: number) {
  return apiRequest<LotteryActivityStats>(`/api/admin/apps/${appKey}/lottery/activities/${activityId}/stats`, { token });
}

// ── 用户 API ──

export function getUserLotteryActivities(token: string) {
  return apiRequest<LotteryActivity[]>("/api/lottery/activities", { token });
}

export function getUserLotteryActivity(token: string, id: number) {
  return apiRequest<{ activity: LotteryActivity; prizes: LotteryPrize[] }>(`/api/lottery/activities/${id}`, { token });
}

export function joinLotteryActivity(token: string, id: number) {
  return apiRequest<void>(`/api/lottery/activities/${id}/join`, { method: "POST", token });
}

export function drawLottery(token: string, id: number) {
  return apiRequest<LotteryDrawResult>(`/api/lottery/activities/${id}/draw`, { method: "POST", token });
}

export function getUserLotteryDraws(token: string, params?: { page?: number; limit?: number }) {
  return apiRequest<PagedResult<LotteryDraw>>(`/api/lottery/my-draws${buildQuery(params ?? {})}`, { token });
}

export function verifyLotteryActivity(token: string, id: number) {
  return apiRequest<LotteryVerifyResult>(`/api/lottery/activities/${id}/verify`, { token });
}
