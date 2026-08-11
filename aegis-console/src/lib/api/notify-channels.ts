import { apiRequest, buildQuery } from "./client";

// 统一通知出口 API。端点位于 /api/admin/notify/*
//
// - 读操作需要 notify:channel:read / notify:delivery:read
// - 写操作与测试发送由后端 RequireSuperAdmin 二次把关（渠道里存着 IM 凭据）
// - 渠道配置表单由 /catalog 返回的元数据动态渲染，新增渠道类型时前端零改动

export type NotifyChannelKind =
  | "feishu_bot"
  | "feishu_app"
  | "dingtalk_bot"
  | "wecom_bot"
  | "slack_webhook"
  | "webhook"
  | "email"
  | "inapp"
  | "realtime";

export type NotifyConfigField = {
  key: string;
  label: string;
  type: "text" | "password" | "select" | "switch" | "textarea" | "tags";
  required: boolean;
  placeholder?: string;
  help?: string;
  options?: string[];
  default?: string;
};

export type NotifyChannelKindMeta = {
  kind: NotifyChannelKind;
  name: string;
  description: string;
  needsSecret: boolean;
  secretLabel?: string;
  fields: NotifyConfigField[];
  docUrl?: string;
};

export type NotifyEventMeta = {
  key: string;
  name: string;
  group: string;
  description: string;
};

export type NotifyQuietHours = {
  timezone: string;
  start: string;
  end: string;
};

export type NotifySubscription = {
  id: number;
  channelId: number;
  channelName?: string;
  channelKind?: NotifyChannelKind;
  eventKey: string;
  appid?: number;
  minPriority?: string;
  categoryIds?: number[];
  templateId?: number;
  quietHours?: NotifyQuietHours;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type NotifyChannel = {
  id: number;
  appid: number;
  key: string;
  name: string;
  kind: NotifyChannelKind;
  config: Record<string, unknown>;
  secretSet: boolean;
  secretHint?: string;
  enabled: boolean;
  rateLimitPerMinute: number;
  lastStatus?: string;
  lastError?: string;
  lastSentAt?: string;
  createdBy?: number;
  createdAt: string;
  updatedAt: string;
  subscriptions?: NotifySubscription[];
};

export type NotifyTemplate = {
  id: number;
  appid: number;
  key: string;
  name: string;
  eventKey: string;
  channelKind?: string;
  titleTemplate: string;
  bodyTemplate: string;
  cardTemplate?: Record<string, unknown>;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type NotifyDelivery = {
  id: number;
  channelId?: number;
  channelName?: string;
  channelKind: string;
  eventKey: string;
  appid: number;
  resource?: string;
  resourceId?: string;
  dedupeKey?: string;
  status: "pending" | "success" | "failed" | "skipped" | "dropped";
  attempt: number;
  requestSnippet?: string;
  responseSnippet?: string;
  error?: string;
  latencyMs: number;
  createdAt: string;
  completedAt?: string;
};

export type NotifyDeliveryPage = {
  items: NotifyDelivery[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type NotifyDeliveryStats = {
  total: number;
  success: number;
  failed: number;
  skipped: number;
  successPct: number;
  avgLatencyMs: number;
  byKind: Record<string, number>;
  byEvent: Record<string, number>;
};

// ─────────────── 元数据 ───────────────

export function getNotifyCatalog(token: string) {
  return apiRequest<{ kinds: NotifyChannelKindMeta[]; events: NotifyEventMeta[] }>(
    "/api/admin/notify/catalog",
    { token }
  );
}

// ─────────────── 渠道 ───────────────

export function listNotifyChannels(token: string, params: { appid?: number; kind?: string } = {}) {
  return apiRequest<NotifyChannel[]>(`/api/admin/notify/channels${buildQuery(params)}`, { token });
}

export function getNotifyChannel(token: string, id: number) {
  return apiRequest<NotifyChannel>(`/api/admin/notify/channels/${id}`, { token });
}

export type NotifyChannelPayload = {
  appid: number;
  key: string;
  name: string;
  kind: NotifyChannelKind;
  config: Record<string, unknown>;
  /** 留空 = 保持原值；"-" = 清空；其它 = 覆盖 */
  secret?: string;
  enabled?: boolean;
  rateLimitPerMinute?: number;
};

export function saveNotifyChannel(token: string, payload: NotifyChannelPayload, id?: number) {
  const path = id ? `/api/admin/notify/channels/${id}` : "/api/admin/notify/channels";
  return apiRequest<NotifyChannel>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteNotifyChannel(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/notify/channels/${id}`, { method: "DELETE", token });
}

export function testNotifyChannel(token: string, id: number) {
  return apiRequest<{ status: string; responseSnippet?: string; latencyMs: number }>(
    `/api/admin/notify/channels/${id}/test`,
    { method: "POST", token }
  );
}

// ─────────────── 订阅 ───────────────

export function listNotifySubscriptions(token: string, channelId?: number) {
  return apiRequest<NotifySubscription[]>(
    `/api/admin/notify/subscriptions${buildQuery({ channelId })}`,
    { token }
  );
}

export type NotifySubscriptionPayload = {
  channelId: number;
  eventKey: string;
  appid?: number | null;
  minPriority?: string;
  categoryIds?: number[];
  templateId?: number | null;
  quietHours?: NotifyQuietHours | null;
  enabled?: boolean;
};

export function saveNotifySubscription(token: string, payload: NotifySubscriptionPayload, id?: number) {
  const path = id ? `/api/admin/notify/subscriptions/${id}` : "/api/admin/notify/subscriptions";
  return apiRequest<NotifySubscription>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteNotifySubscription(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/notify/subscriptions/${id}`, { method: "DELETE", token });
}

// ─────────────── 模板 ───────────────

export function listNotifyTemplates(token: string, appid = 0) {
  return apiRequest<NotifyTemplate[]>(`/api/admin/notify/templates${buildQuery({ appid })}`, { token });
}

export type NotifyTemplatePayload = {
  appid: number;
  key: string;
  name: string;
  eventKey: string;
  channelKind?: string;
  titleTemplate?: string;
  bodyTemplate?: string;
  cardTemplate?: Record<string, unknown> | null;
  enabled?: boolean;
};

export function saveNotifyTemplate(token: string, payload: NotifyTemplatePayload, id?: number) {
  const path = id ? `/api/admin/notify/templates/${id}` : "/api/admin/notify/templates";
  return apiRequest<NotifyTemplate>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteNotifyTemplate(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/notify/templates/${id}`, { method: "DELETE", token });
}

export function previewNotifyTemplate(
  token: string,
  payload: { titleTemplate: string; bodyTemplate: string; vars?: Record<string, unknown> }
) {
  return apiRequest<{ title: string; body: string }>("/api/admin/notify/templates/preview", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ─────────────── 投递记录 ───────────────

export type NotifyDeliveryParams = {
  channelId?: number;
  eventKey?: string;
  status?: string;
  resource?: string;
  resourceId?: string;
  appid?: number;
  page?: number;
  limit?: number;
};

export function listNotifyDeliveries(token: string, params: NotifyDeliveryParams = {}) {
  return apiRequest<NotifyDeliveryPage>(`/api/admin/notify/deliveries${buildQuery(params)}`, { token });
}

export function getNotifyDeliveryStats(token: string, days = 7) {
  return apiRequest<NotifyDeliveryStats>(`/api/admin/notify/deliveries/stats${buildQuery({ days })}`, {
    token
  });
}

export function purgeNotifyDeliveries(token: string, days = 30) {
  return apiRequest<{ deleted: number }>(`/api/admin/notify/deliveries${buildQuery({ days })}`, {
    method: "DELETE",
    token
  });
}
