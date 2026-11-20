import { apiRequest, buildQuery } from "@/lib/api/client";
import type { UserNotificationAction } from "@/lib/user-notification-meta";

/**
 * 站内信群发任务 API（管理端）。契约见 docs/notification-center.md「群发任务」，
 * 类型与后端 internal/domain/notification/campaign.go 逐字段对应。
 *
 * 发起即落一个任务、立刻返回；任意实例认领后按用户 ID 升序分批写入。
 * 同一 `requestId` 重复提交返回第一次的任务（`created=false`），控制台每打开一次编辑器生成一个。
 */

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/notifications/campaigns`;

export type NotificationCampaignStatus =
  | "scheduled"
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "recalling"
  | "recalled";

export type NotificationAudienceScope = "all" | "users" | "filter";
export type NotificationAudienceAccount = "available" | "all" | "disabled";
export type NotificationAudienceMembership = "" | "member" | "non_member" | "lifetime" | "non_lifetime";

/** 目标人群（Go `notification.Audience`）。全站与指定用户只看账号状态与排除名单。 */
export type NotificationAudience = {
  scope: NotificationAudienceScope;
  userIds?: number[];
  excludeUserIds?: number[];
  account?: NotificationAudienceAccount;
  keyword?: string;
  membership?: NotificationAudienceMembership;
  registeredWithinDays?: number;
  registeredBeforeDays?: number;
  activeWithinDays?: number;
  inactiveDays?: number;
  minLevel?: number;
  maxLevel?: number;
  platforms?: string[];
  /** 最多发给多少人（按用户 ID 升序取前 N 个），0 不限 */
  limit?: number;
};

export type NotificationAudienceOption = { value: string; label: string };

export type NotificationAudiencePreset = {
  key: string;
  label: string;
  description: string;
  audience: NotificationAudience;
};

export type NotificationCategorySpec = { key: string; label: string; mutable: boolean };

export type NotificationCampaignCatalog = {
  presets: NotificationAudiencePreset[];
  platforms: NotificationAudienceOption[];
  memberships: NotificationAudienceOption[];
  accounts: NotificationAudienceOption[];
  categories: NotificationCategorySpec[];
  statuses: NotificationAudienceOption[];
  /** maxUserIds / maxDays / maxLevel / maxScheduleHours */
  limits: Record<string, number>;
};

export type NotificationAudienceSample = {
  userId: number;
  account: string;
  nickname: string;
  registeredAt: string;
  member: boolean;
  level: number;
  lastActiveAt: string | null;
};

export type NotificationAudiencePreview = {
  /** 归一后的人群 */
  audience: NotificationAudience;
  label: string;
  total: number;
  samples: NotificationAudienceSample[];
};

export type NotificationCampaign = {
  id: number;
  appid: number;
  requestId: string | null;
  type: string;
  category: string;
  categoryLabel: string;
  title: string;
  content: string;
  level: string;
  metadata?: Record<string, unknown>;
  action: UserNotificationAction | null;
  image: string | null;
  expiresAt: string | null;
  audience: NotificationAudience;
  audienceLabel: string;
  status: NotificationCampaignStatus;
  statusLabel: string;
  scheduledAt: string | null;
  /** 开始发送时人群的人数（估计值），开始前为 null */
  total: number | null;
  processed: number;
  delivered: number;
  skipped: number;
  recalled: number;
  /** 已读人数：只有详情接口现算，列表上为 null */
  readCount: number | null;
  attempts: number;
  message: string;
  operator: string;
  startedAt: string | null;
  finishedAt: string | null;
  recalledAt: string | null;
  heartbeatAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type NotificationCampaignPage = {
  items: NotificationCampaign[] | null;
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type NotificationCampaignPayload = {
  requestId?: string;
  type?: string;
  category?: string;
  title: string;
  content?: string;
  level?: string;
  action?: UserNotificationAction;
  image?: string;
  /** RFC 3339，一年内；定时发送时必须晚于定时时间 */
  expiresAt?: string;
  metadata?: Record<string, unknown>;
  /** RFC 3339，90 天内；已到点即立刻排队 */
  scheduledAt?: string;
  audience: NotificationAudience;
};

export type NotificationCampaignListParams = {
  status?: string;
  page?: number;
  limit?: number;
};

export function getNotificationCampaignCatalog(token: string, appKey: string) {
  return apiRequest<NotificationCampaignCatalog>(`${base(appKey)}/catalog`, { token });
}

/** 只数人不写数据：人数与最多 10 个抽样 */
export function previewNotificationAudience(token: string, appKey: string, audience: NotificationAudience) {
  return apiRequest<NotificationAudiencePreview>(`${base(appKey)}/preview`, {
    method: "POST",
    token,
    body: JSON.stringify({ audience })
  });
}

export function listNotificationCampaigns(token: string, appKey: string, params?: NotificationCampaignListParams) {
  return apiRequest<NotificationCampaignPage>(`${base(appKey)}${buildQuery(params ?? {})}`, { token });
}

export function createNotificationCampaign(token: string, appKey: string, payload: NotificationCampaignPayload) {
  return apiRequest<{ campaign: NotificationCampaign; created: boolean }>(base(appKey), {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getNotificationCampaign(token: string, appKey: string, campaignId: number) {
  return apiRequest<NotificationCampaign>(`${base(appKey)}/${campaignId}`, { token });
}

/** 取消定时、排队或发送中的任务；已发出的保留（已结束的 40958） */
export function cancelNotificationCampaign(token: string, appKey: string, campaignId: number) {
  return apiRequest<NotificationCampaign>(`${base(appKey)}/${campaignId}/cancel`, { method: "POST", token });
}

/** 撤回：后台按批把这个任务发出的消息从所有人的消息中心删掉（重复撤回 40958） */
export function recallNotificationCampaign(token: string, appKey: string, campaignId: number) {
  return apiRequest<NotificationCampaign>(`${base(appKey)}/${campaignId}/recall`, { method: "POST", token });
}
