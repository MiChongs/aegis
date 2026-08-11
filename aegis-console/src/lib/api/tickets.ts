import { apiRequest, buildQuery } from "./client";

// 工单系统 API。所有端点位于 /api/admin/tickets/*
//
// 权限说明：后端在中间件层只做「能不能进模块」的粗粒度闸门，
// 「能不能看/改这一条」由 service 层的 Scope + ActionSet 判定，
// 详情响应里的 `permissions` 字段就是后端算好的动作集，前端据此控制按钮显隐。

// ─────────────── 枚举 ───────────────

export type TicketStatus =
  | "open"
  | "processing"
  | "pending_user"
  | "pending_third_party"
  | "resolved"
  | "closed"
  | "cancelled";

export type TicketPriority = "low" | "normal" | "high" | "urgent";

export type TicketSLAState = "ontime" | "warning" | "breached" | "paused" | "met";

export type TicketSource = "console" | "app" | "api" | "email" | "bot" | "import";

// ─────────────── 实体 ───────────────

export type TicketActionSet = {
  view: boolean;
  reply: boolean;
  internalNote: boolean;
  viewInternal: boolean;
  edit: boolean;
  assign: boolean;
  changeStatus: boolean;
  close: boolean;
  reopen: boolean;
  delete: boolean;
  watch: boolean;
  manageWatchers: boolean;
  uploadAttachment: boolean;
};

export type TicketAttachment = {
  id: number;
  ticketId?: number;
  messageId?: number;
  fileName: string;
  contentType: string;
  sizeBytes: number;
  downloadUrl?: string;
  uploadedByType: string;
  uploadedById?: number;
  createdAt: string;
};

export type TicketMessage = {
  id: number;
  ticketId: number;
  authorType: "requester" | "agent" | "system";
  authorUserId?: number;
  authorAdminId?: number;
  authorName: string;
  internal: boolean;
  content: string;
  contentType: string;
  metadata?: Record<string, unknown>;
  editedAt?: string;
  createdAt: string;
  attachments?: TicketAttachment[];
};

export type TicketEvent = {
  id: number;
  ticketId: number;
  event: string;
  actorType: "user" | "admin" | "system";
  actorId?: number;
  actorName: string;
  fromValue?: string;
  toValue?: string;
  summary: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
};

export type TicketWatcher = {
  ticketId: number;
  adminId: number;
  account?: string;
  displayName?: string;
  createdAt: string;
};

export type TicketItem = {
  id: number;
  ticketNo: string;
  appid: number;
  appName?: string;
  requesterType: "user" | "admin";
  requesterUserId?: number;
  requesterAdminId?: number;
  requesterName: string;
  requesterContact?: string;
  categoryId?: number;
  categoryName?: string;
  title: string;
  status: TicketStatus;
  priority: TicketPriority;
  source: TicketSource;
  assigneeAdminId?: number;
  assigneeName?: string;
  groupId?: number;
  groupName?: string;
  slaPolicyId?: number;
  firstResponseDueAt?: string;
  resolveDueAt?: string;
  firstRespondedAt?: string;
  resolvedAt?: string;
  closedAt?: string;
  slaState: TicketSLAState;
  messageCount: number;
  lastMessageAt?: string;
  lastMessageRole?: string;
  reopenedCount: number;
  rating?: number;
  ratingComment?: string;
  ratedAt?: string;
  tags: string[];
  metadata?: Record<string, unknown>;
  locked: boolean;
  createdByAdminId?: number;
  createdAt: string;
  updatedAt: string;
  messages?: TicketMessage[];
  events?: TicketEvent[];
  attachments?: TicketAttachment[];
  watchers?: TicketWatcher[];
  permissions?: TicketActionSet;
};

export type TicketScopeInfo = {
  level: "all" | "app" | "personal";
  appIds?: number[];
  groupIds?: number[];
  label: string;
};

export type TicketListResponse = {
  items: TicketItem[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
  scope?: TicketScopeInfo;
};

export type TicketStats = {
  total: number;
  open: number;
  processing: number;
  pendingUser: number;
  resolved: number;
  closed: number;
  unassigned: number;
  mineAssigned: number;
  overdueFirstResponse: number;
  overdueResolve: number;
  createdToday: number;
  resolvedToday: number;
  byPriority: Record<string, number>;
  byCategory: { categoryId?: number; categoryName: string; count: number }[];
  avgFirstResponseMs: number;
  avgResolveMs: number;
  avgRating: number;
  ratingCount: number;
};

export type TicketTrendPoint = {
  date: string;
  created: number;
  resolved: number;
  closed: number;
};

export type TicketAgentStat = {
  adminId: number;
  account: string;
  displayName: string;
  assigned: number;
  resolved: number;
  open: number;
  avgFirstResponseMs: number;
  avgResolveMs: number;
  avgRating: number;
  breached: number;
};

// ─────────────── 配置实体 ───────────────

export type TicketFormField = {
  key: string;
  label: string;
  type: string;
  required: boolean;
  placeholder?: string;
  options?: string[];
};

export type TicketCategory = {
  id: number;
  appid: number;
  parentId?: number;
  key: string;
  name: string;
  description: string;
  defaultPriority: TicketPriority;
  defaultGroupId?: number;
  slaPolicyId?: number;
  formSchema: TicketFormField[];
  userSubmittable: boolean;
  sort: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type TicketGroupMember = {
  groupId: number;
  adminId: number;
  account?: string;
  displayName?: string;
  avatar?: string;
  role: "agent" | "leader";
  openCount: number;
  createdAt: string;
};

export type TicketGroup = {
  id: number;
  appid: number;
  key: string;
  name: string;
  description: string;
  assignStrategy: "manual" | "round_robin" | "least_open";
  enabled: boolean;
  members?: TicketGroupMember[];
  memberCount: number;
  openCount: number;
  createdAt: string;
  updatedAt: string;
};

export type TicketBusinessHours = {
  timezone: string;
  days: number[];
  start: string;
  end: string;
};

export type TicketSLAPolicy = {
  id: number;
  appid: number;
  name: string;
  description: string;
  firstResponseMinutes: Record<string, number>;
  resolveMinutes: Record<string, number>;
  businessHours?: TicketBusinessHours;
  warnRatio: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type TicketQuickReply = {
  id: number;
  appid: number;
  title: string;
  content: string;
  categoryId?: number;
  ownerAdminId?: number;
  usageCount: number;
  sort: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type TicketMetadata = {
  statuses: { value: TicketStatus; label: string }[];
  priorities: { value: TicketPriority; label: string; weight: number }[];
  sources: { value: TicketSource; label: string }[];
  slaStates: { value: TicketSLAState; label: string }[];
};

// ─────────────── 查询参数 ───────────────

export type TicketListParams = {
  appid?: number;
  status?: string;
  priority?: string;
  categoryId?: number;
  groupId?: number;
  assigneeId?: number;
  unassigned?: boolean;
  keyword?: string;
  tags?: string;
  slaState?: string;
  overdueOnly?: boolean;
  createdFrom?: string;
  createdTo?: string;
  sortBy?: string;
  sortDir?: string;
  includeClosed?: boolean;
  mine?: boolean;
  page?: number;
  limit?: number;
};

// ─────────────── 工单读接口 ───────────────

export function listTickets(token: string, params: TicketListParams = {}) {
  return apiRequest<TicketListResponse>(`/api/admin/tickets${buildQuery(params as Record<string, never>)}`, { token });
}

export function getTicket(token: string, id: number | string) {
  return apiRequest<TicketItem>(`/api/admin/tickets/${id}`, { token });
}

export function getTicketStats(token: string, appid?: number) {
  return apiRequest<TicketStats>(`/api/admin/tickets/stats${buildQuery({ appid })}`, { token });
}

export function getTicketTrend(token: string, days = 30, appid?: number) {
  return apiRequest<TicketTrendPoint[]>(`/api/admin/tickets/trend${buildQuery({ days, appid })}`, { token });
}

export function getTicketAgentStats(token: string, limit = 20, appid?: number) {
  return apiRequest<TicketAgentStat[]>(`/api/admin/tickets/agents${buildQuery({ limit, appid })}`, { token });
}

export function getTicketWorkbench(token: string) {
  return apiRequest<{ pending: number }>("/api/admin/tickets/workbench", { token });
}

export function getTicketMetadata(token: string) {
  return apiRequest<TicketMetadata>("/api/admin/tickets/metadata", { token });
}

export function ticketExportUrl(params: TicketListParams = {}) {
  return `/api/admin/tickets/export${buildQuery(params as Record<string, never>)}`;
}

// ─────────────── 工单写接口 ───────────────

export type TicketCreatePayload = {
  appid: number;
  requesterType?: "user" | "admin";
  requesterUserId?: number;
  requesterAdminId?: number;
  requesterName?: string;
  requesterContact?: string;
  categoryId?: number;
  title: string;
  content: string;
  priority?: TicketPriority;
  source?: TicketSource;
  assigneeAdminId?: number;
  groupId?: number;
  tags?: string[];
  metadata?: Record<string, unknown>;
  attachmentIds?: number[];
};

export function createTicket(token: string, payload: TicketCreatePayload) {
  return apiRequest<TicketItem>("/api/admin/tickets", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export type TicketReplyPayload = {
  content: string;
  contentType?: string;
  internal?: boolean;
  attachmentIds?: number[];
  nextStatus?: TicketStatus | "";
  quickReplyId?: number;
};

export function replyTicket(token: string, id: number, payload: TicketReplyPayload) {
  return apiRequest<TicketMessage>(`/api/admin/tickets/${id}/replies`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export type TicketUpdatePayload = {
  title?: string;
  categoryId?: number;
  priority?: TicketPriority;
  tags?: string[];
  locked?: boolean;
};

export function updateTicket(token: string, id: number, payload: TicketUpdatePayload) {
  return apiRequest<TicketItem>(`/api/admin/tickets/${id}`, {
    method: "PATCH",
    token,
    body: JSON.stringify(payload)
  });
}

export type TicketAssignPayload = {
  assigneeAdminId?: number | null;
  groupId?: number | null;
  autoPick?: boolean;
  reason?: string;
};

export function assignTicket(token: string, id: number, payload: TicketAssignPayload) {
  return apiRequest<TicketItem>(`/api/admin/tickets/${id}/assign`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function changeTicketStatus(
  token: string,
  id: number,
  payload: { status: TicketStatus; reason?: string; solution?: string }
) {
  return apiRequest<TicketItem>(`/api/admin/tickets/${id}/status`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteTicket(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/tickets/${id}`, { method: "DELETE", token });
}

export type TicketBulkPayload = {
  ids: number[];
  action: "assign" | "status" | "close" | "priority" | "tag" | "delete";
  assigneeAdminId?: number;
  groupId?: number;
  status?: TicketStatus;
  priority?: TicketPriority;
  tags?: string[];
  reason?: string;
};

export type TicketBulkResult = {
  requested: number;
  succeeded: number;
  failed?: { id: number; reason: string }[];
  action: string;
};

export function bulkTickets(token: string, payload: TicketBulkPayload) {
  return apiRequest<TicketBulkResult>("/api/admin/tickets/bulk", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function watchTicket(token: string, id: number, watch: boolean) {
  return apiRequest<{ watching: boolean }>(`/api/admin/tickets/${id}/watch?watch=${watch}`, {
    method: "POST",
    token
  });
}

export function setTicketWatchers(token: string, id: number, adminIds: number[]) {
  return apiRequest<TicketWatcher[]>(`/api/admin/tickets/${id}/watchers`, {
    method: "PUT",
    token,
    body: JSON.stringify({ adminIds })
  });
}

export function uploadTicketAttachment(token: string, file: File, appid: number, ticketId?: number) {
  const fd = new FormData();
  fd.append("file", file);
  fd.append("appid", String(appid));
  if (ticketId) fd.append("ticketId", String(ticketId));
  return apiRequest<TicketAttachment>("/api/admin/tickets/attachments", {
    method: "POST",
    token,
    body: fd
  });
}

// ─────────────── 配置接口 ───────────────

export function listTicketCategories(token: string, appid = 0) {
  return apiRequest<TicketCategory[]>(`/api/admin/tickets/categories${buildQuery({ appid })}`, { token });
}

export type TicketCategoryPayload = {
  appid: number;
  parentId?: number;
  key: string;
  name: string;
  description?: string;
  defaultPriority?: TicketPriority;
  defaultGroupId?: number;
  slaPolicyId?: number;
  formSchema?: TicketFormField[];
  userSubmittable?: boolean;
  sort?: number;
  enabled?: boolean;
};

export function saveTicketCategory(token: string, payload: TicketCategoryPayload, id?: number) {
  const path = id ? `/api/admin/tickets/categories/${id}` : "/api/admin/tickets/categories";
  return apiRequest<TicketCategory>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteTicketCategory(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/tickets/categories/${id}`, { method: "DELETE", token });
}

export function listTicketGroups(token: string, appid = 0) {
  return apiRequest<TicketGroup[]>(`/api/admin/tickets/groups${buildQuery({ appid })}`, { token });
}

export type TicketGroupPayload = {
  appid: number;
  key: string;
  name: string;
  description?: string;
  assignStrategy?: "manual" | "round_robin" | "least_open";
  enabled?: boolean;
};

export function saveTicketGroup(token: string, payload: TicketGroupPayload, id?: number) {
  const path = id ? `/api/admin/tickets/groups/${id}` : "/api/admin/tickets/groups";
  return apiRequest<TicketGroup>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteTicketGroup(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/tickets/groups/${id}`, { method: "DELETE", token });
}

export function setTicketGroupMembers(
  token: string,
  id: number,
  members: { adminId: number; role: "agent" | "leader" }[]
) {
  return apiRequest<TicketGroupMember[]>(`/api/admin/tickets/groups/${id}/members`, {
    method: "PUT",
    token,
    body: JSON.stringify({ members })
  });
}

export function listTicketSLAPolicies(token: string, appid = 0) {
  return apiRequest<TicketSLAPolicy[]>(`/api/admin/tickets/sla-policies${buildQuery({ appid })}`, { token });
}

export type TicketSLAPayload = {
  appid: number;
  name: string;
  description?: string;
  firstResponseMinutes: Record<string, number>;
  resolveMinutes: Record<string, number>;
  businessHours?: TicketBusinessHours | null;
  warnRatio?: number;
  enabled?: boolean;
};

export function saveTicketSLAPolicy(token: string, payload: TicketSLAPayload, id?: number) {
  const path = id ? `/api/admin/tickets/sla-policies/${id}` : "/api/admin/tickets/sla-policies";
  return apiRequest<TicketSLAPolicy>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteTicketSLAPolicy(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/tickets/sla-policies/${id}`, { method: "DELETE", token });
}

export function listTicketQuickReplies(token: string, appid = 0) {
  return apiRequest<TicketQuickReply[]>(`/api/admin/tickets/quick-replies${buildQuery({ appid })}`, { token });
}

export type TicketQuickReplyPayload = {
  appid: number;
  title: string;
  content: string;
  categoryId?: number;
  private?: boolean;
  sort?: number;
  enabled?: boolean;
};

export function saveTicketQuickReply(token: string, payload: TicketQuickReplyPayload, id?: number) {
  const path = id ? `/api/admin/tickets/quick-replies/${id}` : "/api/admin/tickets/quick-replies";
  return apiRequest<TicketQuickReply>(path, {
    method: id ? "PUT" : "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteTicketQuickReply(token: string, id: number, appid = 0) {
  return apiRequest<{ id: number }>(`/api/admin/tickets/quick-replies/${id}${buildQuery({ appid })}`, {
    method: "DELETE",
    token
  });
}
