import { apiRequest, buildQuery } from "./client";

// 有效反馈奖励积分（后端 docs/feedback-rewards.md）。
//
// 管理员在服务台给被采纳的意见反馈发积分；误发可撤回扣回。策略按应用配置，默认关闭。
// 能不能发看详情里的 permissions.reward / revokeReward（能处理这条反馈 + 该应用的积分调整权限），
// 这张单子本身能不能发看 reward.ineligible，前端不自己推断。

/** 一个应用的反馈奖励策略。上限字段 0 表示不限。 */
export type FeedbackRewardPolicy = {
  appid: number;
  enabled: boolean;
  /** 默认建议积分：分类没单独配时预填它 */
  defaultPoints: number;
  /** 反馈分类 key → 建议积分 */
  categoryPoints: Record<string, number>;
  /** 单条上限 */
  maxPoints: number;
  /** 每用户自然日 / 自然月累计上限（已撤回的不计） */
  userDailyLimit: number;
  userMonthlyLimit: number;
  /** 库里是否已有配置（没有时返回推荐默认值，且未开启） */
  configured: boolean;
  updatedByName?: string;
  updatedAt?: string;
};

export type FeedbackRewardPolicyPayload = Omit<FeedbackRewardPolicy, "configured" | "updatedByName" | "updatedAt">;

export type FeedbackRewardStatus = "granted" | "revoked";

export type FeedbackReward = {
  id: number;
  ticketId: number;
  appid: number;
  userId?: number;
  ticketNo: string;
  categoryKey?: string;
  points: number;
  reason?: string;
  status: FeedbackRewardStatus;
  grantTxnNo: string;
  grantedByAdminId?: number;
  grantedByName?: string;
  grantedAt: string;
  revokedAt?: string;
  revokedByName?: string;
  revokeReason?: string;
  revokeTxnNo?: string;
  /** 撤回时实际扣回的积分与没扣上的差额 */
  clawedBack: number;
  shortfall: number;
};

/** 详情里的奖励区块 */
export type FeedbackRewardInfo = {
  enabled: boolean;
  /** 按分类预填的积分 */
  suggestedPoints: number;
  maxPoints: number;
  /** 这次最多还能发多少（单条上限与该用户当日、当月余量取最小）；null 表示不限 */
  maxGrantable: number | null;
  /** 该用户今天 / 本月已领的反馈奖励 */
  usage: { daily: number; monthly: number };
  /** 这张单子本身不能发的原因；空表示可以 */
  ineligible?: string;
  active?: FeedbackReward;
  /** 全部记录，新的在前（含已撤回的） */
  history: FeedbackReward[];
};

export type FeedbackRewardGrantPayload = {
  /** 省略或 0 时按策略的建议值发 */
  points?: number;
  /** 理由，会附在给用户的站内信里 */
  reason?: string;
  /** 防重复提交：同一编号重复提交原样返回 */
  requestId?: string;
};

export type FeedbackRewardRevokePayload = {
  reason?: string;
  /** 用户余额不足时：false 整笔拒绝（默认），true 扣到 0 为止并记下差额 */
  allowPartial?: boolean;
  requestId?: string;
};

export type FeedbackRewardResult = {
  reward: FeedbackReward;
  balanceBefore: number;
  balanceAfter: number;
  /** 重复提交：已有同样的一笔，原样返回 */
  replayed: boolean;
  /** 是否已给用户发站内信 */
  notified: boolean;
  info?: FeedbackRewardInfo;
};

export function getFeedbackRewardPolicy(token: string, appid: number) {
  return apiRequest<FeedbackRewardPolicy>(`/api/admin/tickets/reward-policy${buildQuery({ appid })}`, { token });
}

export function saveFeedbackRewardPolicy(token: string, payload: FeedbackRewardPolicyPayload) {
  return apiRequest<FeedbackRewardPolicy>(`/api/admin/tickets/reward-policy${buildQuery({ appid: payload.appid })}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function grantFeedbackReward(token: string, ticketId: number, payload: FeedbackRewardGrantPayload) {
  return apiRequest<FeedbackRewardResult>(`/api/admin/tickets/${ticketId}/reward`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function revokeFeedbackReward(token: string, ticketId: number, payload: FeedbackRewardRevokePayload) {
  return apiRequest<FeedbackRewardResult>(`/api/admin/tickets/${ticketId}/reward/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 一次弹窗一个请求编号：连点、网络重试都只落一笔 */
export function newRewardRequestId() {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `fbr-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
