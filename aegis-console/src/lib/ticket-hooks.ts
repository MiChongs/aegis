"use client";

import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminAccountsQuery } from "@/lib/admin-hooks";
import {
  assignTicket,
  bulkTickets,
  changeTicketStatus,
  createTicket,
  deleteTicket,
  deleteTicketCategory,
  deleteTicketGroup,
  deleteTicketQuickReply,
  deleteTicketSLAPolicy,
  getTicket,
  getTicketAgentStats,
  getTicketMetadata,
  getTicketStats,
  getTicketTrend,
  getTicketWorkbench,
  listTicketCategories,
  listTicketGroups,
  listTicketQuickReplies,
  listTicketSLAPolicies,
  listTickets,
  replyTicket,
  saveTicketCategory,
  saveTicketGroup,
  saveTicketQuickReply,
  saveTicketSLAPolicy,
  setTicketGroupMembers,
  setTicketWatchers,
  updateTicket,
  uploadTicketAttachment,
  watchTicket,
  type TicketAssignPayload,
  type TicketBulkPayload,
  type TicketCategoryPayload,
  type TicketCreatePayload,
  type TicketGroupPayload,
  type TicketListParams,
  type TicketQuickReplyPayload,
  type TicketReplyPayload,
  type TicketSLAPayload,
  type TicketStatus,
  type TicketUpdatePayload
} from "@/lib/api/tickets";
import {
  deleteNotifyChannel,
  deleteNotifySubscription,
  deleteNotifyTemplate,
  getNotifyCatalog,
  getNotifyDeliveryStats,
  listNotifyChannels,
  listNotifyDeliveries,
  listNotifySubscriptions,
  listNotifyTemplates,
  previewNotifyTemplate,
  purgeNotifyDeliveries,
  saveNotifyChannel,
  saveNotifySubscription,
  saveNotifyTemplate,
  testNotifyChannel,
  type NotifyChannelPayload,
  type NotifyDeliveryParams,
  type NotifySubscriptionPayload,
  type NotifyTemplatePayload
} from "@/lib/api/notify-channels";

function useToken() {
  return useAuthStore((state) => state.accessToken);
}

// ─────────────── 管理员下拉选项 ───────────────

export type AdminOption = { id: number; name: string; account: string };

/**
 * 指派人 / 组成员选择器用的管理员列表。
 *
 * `/api/admin/system/admins` 实际返回的是 Profile[]（`{ account: {...}, assignments: [] }`），
 * 而 `lib/api/admin.ts` 把它标成了扁平的 AdminAccount[]。这里做一次形状兼容展平，
 * 两种结构都能吃，避免各处组件重复踩这个坑。
 */
export function useAdminOptions(): AdminOption[] {
  const query = useAdminAccountsQuery();
  return useMemo(() => {
    const data = query.data as unknown;
    if (!Array.isArray(data)) return [];
    return data
      .map((item) => {
        const record = item as Record<string, unknown>;
        const nested = record.account;
        const source =
          nested && typeof nested === "object" ? (nested as Record<string, unknown>) : record;
        const id = Number(source.id);
        if (!id) return null;
        const account = String(source.account ?? "");
        const displayName = String(source.displayName ?? "");
        return { id, account, name: displayName || account } satisfies AdminOption;
      })
      .filter((item): item is AdminOption => item !== null);
  }, [query.data]);
}

// 工单任何写操作都可能改变列表 / 详情 / 统计三处，统一失效避免遗漏
function invalidateTickets(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["tickets"] });
  qc.invalidateQueries({ queryKey: ["ticket-detail"] });
  qc.invalidateQueries({ queryKey: ["ticket-stats"] });
  qc.invalidateQueries({ queryKey: ["ticket-workbench"] });
}

// ─────────────── 工单读 ───────────────

export function useTicketsQuery(params: TicketListParams = {}, enabled = true) {
  const token = useToken();
  return useQuery({
    queryKey: ["tickets", token, params],
    queryFn: () => listTickets(token as string, params),
    enabled: Boolean(token) && enabled,
    // 工单状态变化频繁，但也不至于秒级；15s 内复用缓存，切页回来仍然即时
    staleTime: 15_000
  });
}

export function useTicketDetailQuery(id?: number | null) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-detail", token, id],
    queryFn: () => getTicket(token as string, id as number),
    enabled: Boolean(token && id)
  });
}

export function useTicketStatsQuery(appid?: number) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-stats", token, appid],
    queryFn: () => getTicketStats(token as string, appid),
    enabled: Boolean(token),
    staleTime: 30_000
  });
}

export function useTicketTrendQuery(days = 30, appid?: number) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-trend", token, days, appid],
    queryFn: () => getTicketTrend(token as string, days, appid),
    enabled: Boolean(token),
    staleTime: 60_000
  });
}

export function useTicketAgentStatsQuery(limit = 20, appid?: number) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-agents", token, limit, appid],
    queryFn: () => getTicketAgentStats(token as string, limit, appid),
    enabled: Boolean(token),
    staleTime: 60_000
  });
}

/** 侧边栏 / 工作台角标：我的待办数量 */
export function useTicketWorkbenchQuery() {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-workbench", token],
    queryFn: () => getTicketWorkbench(token as string),
    enabled: Boolean(token),
    staleTime: 30_000,
    refetchInterval: 60_000
  });
}

/** 枚举元数据：状态 / 优先级 / 来源，与后端保持单一事实源 */
export function useTicketMetadataQuery() {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-metadata", token],
    queryFn: () => getTicketMetadata(token as string),
    enabled: Boolean(token),
    staleTime: 30 * 60_000,
    gcTime: 60 * 60_000
  });
}

// ─────────────── 工单写 ───────────────

export function useCreateTicketMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: TicketCreatePayload) => createTicket(token as string, payload),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useReplyTicketMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; payload: TicketReplyPayload }) =>
      replyTicket(token as string, args.id, args.payload),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useUpdateTicketMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; payload: TicketUpdatePayload }) =>
      updateTicket(token as string, args.id, args.payload),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useAssignTicketMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; payload: TicketAssignPayload }) =>
      assignTicket(token as string, args.id, args.payload),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useChangeTicketStatusMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; status: TicketStatus; reason?: string; solution?: string }) =>
      changeTicketStatus(token as string, args.id, {
        status: args.status,
        reason: args.reason,
        solution: args.solution
      }),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useDeleteTicketMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteTicket(token as string, id),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useBulkTicketsMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: TicketBulkPayload) => bulkTickets(token as string, payload),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useWatchTicketMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; watch: boolean }) => watchTicket(token as string, args.id, args.watch),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useSetTicketWatchersMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; adminIds: number[] }) =>
      setTicketWatchers(token as string, args.id, args.adminIds),
    onSuccess: () => invalidateTickets(qc)
  });
}

export function useUploadTicketAttachmentMutation() {
  const token = useToken();
  return useMutation({
    mutationFn: (args: { file: File; appid: number; ticketId?: number }) =>
      uploadTicketAttachment(token as string, args.file, args.appid, args.ticketId)
  });
}

// ─────────────── 工单配置 ───────────────

export function useTicketCategoriesQuery(appid = 0) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-categories", token, appid],
    queryFn: () => listTicketCategories(token as string, appid),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });
}

export function useSaveTicketCategoryMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: TicketCategoryPayload; id?: number }) =>
      saveTicketCategory(token as string, args.payload, args.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-categories"] })
  });
}

export function useDeleteTicketCategoryMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteTicketCategory(token as string, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-categories"] })
  });
}

export function useTicketGroupsQuery(appid = 0) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-groups", token, appid],
    queryFn: () => listTicketGroups(token as string, appid),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });
}

export function useSaveTicketGroupMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: TicketGroupPayload; id?: number }) =>
      saveTicketGroup(token as string, args.payload, args.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-groups"] })
  });
}

export function useDeleteTicketGroupMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteTicketGroup(token as string, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-groups"] })
  });
}

export function useSetTicketGroupMembersMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; members: { adminId: number; role: "agent" | "leader" }[] }) =>
      setTicketGroupMembers(token as string, args.id, args.members),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-groups"] })
  });
}

export function useTicketSLAPoliciesQuery(appid = 0) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-sla", token, appid],
    queryFn: () => listTicketSLAPolicies(token as string, appid),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });
}

export function useSaveTicketSLAPolicyMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: TicketSLAPayload; id?: number }) =>
      saveTicketSLAPolicy(token as string, args.payload, args.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-sla"] })
  });
}

export function useDeleteTicketSLAPolicyMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteTicketSLAPolicy(token as string, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-sla"] })
  });
}

export function useTicketQuickRepliesQuery(appid = 0) {
  const token = useToken();
  return useQuery({
    queryKey: ["ticket-quick-replies", token, appid],
    queryFn: () => listTicketQuickReplies(token as string, appid),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });
}

export function useSaveTicketQuickReplyMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: TicketQuickReplyPayload; id?: number }) =>
      saveTicketQuickReply(token as string, args.payload, args.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-quick-replies"] })
  });
}

export function useDeleteTicketQuickReplyMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number; appid?: number }) =>
      deleteTicketQuickReply(token as string, args.id, args.appid ?? 0),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ticket-quick-replies"] })
  });
}

// ─────────────── 统一通知出口 ───────────────

export function useNotifyCatalogQuery() {
  const token = useToken();
  return useQuery({
    queryKey: ["notify-catalog", token],
    queryFn: () => getNotifyCatalog(token as string),
    enabled: Boolean(token),
    // 编译进后端二进制的静态目录，缓存久一点没有风险
    staleTime: 30 * 60_000,
    gcTime: 60 * 60_000
  });
}

export function useNotifyChannelsQuery(params: { appid?: number; kind?: string } = {}) {
  const token = useToken();
  return useQuery({
    queryKey: ["notify-channels", token, params],
    queryFn: () => listNotifyChannels(token as string, params),
    enabled: Boolean(token)
  });
}

function invalidateNotify(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["notify-channels"] });
  qc.invalidateQueries({ queryKey: ["notify-subscriptions"] });
}

export function useSaveNotifyChannelMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: NotifyChannelPayload; id?: number }) =>
      saveNotifyChannel(token as string, args.payload, args.id),
    onSuccess: () => invalidateNotify(qc)
  });
}

export function useDeleteNotifyChannelMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteNotifyChannel(token as string, id),
    onSuccess: () => invalidateNotify(qc)
  });
}

export function useTestNotifyChannelMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => testNotifyChannel(token as string, id),
    // 测试会写 last_status，刷新列表让状态灯即时生效
    onSettled: () => qc.invalidateQueries({ queryKey: ["notify-channels"] })
  });
}

export function useNotifySubscriptionsQuery(channelId?: number) {
  const token = useToken();
  return useQuery({
    queryKey: ["notify-subscriptions", token, channelId],
    queryFn: () => listNotifySubscriptions(token as string, channelId),
    enabled: Boolean(token)
  });
}

export function useSaveNotifySubscriptionMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: NotifySubscriptionPayload; id?: number }) =>
      saveNotifySubscription(token as string, args.payload, args.id),
    onSuccess: () => invalidateNotify(qc)
  });
}

export function useDeleteNotifySubscriptionMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteNotifySubscription(token as string, id),
    onSuccess: () => invalidateNotify(qc)
  });
}

export function useNotifyTemplatesQuery(appid = 0) {
  const token = useToken();
  return useQuery({
    queryKey: ["notify-templates", token, appid],
    queryFn: () => listNotifyTemplates(token as string, appid),
    enabled: Boolean(token)
  });
}

export function useSaveNotifyTemplateMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { payload: NotifyTemplatePayload; id?: number }) =>
      saveNotifyTemplate(token as string, args.payload, args.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notify-templates"] })
  });
}

export function useDeleteNotifyTemplateMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteNotifyTemplate(token as string, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notify-templates"] })
  });
}

export function usePreviewNotifyTemplateMutation() {
  const token = useToken();
  return useMutation({
    mutationFn: (payload: { titleTemplate: string; bodyTemplate: string; vars?: Record<string, unknown> }) =>
      previewNotifyTemplate(token as string, payload)
  });
}

export function useNotifyDeliveriesQuery(params: NotifyDeliveryParams = {}) {
  const token = useToken();
  return useQuery({
    queryKey: ["notify-deliveries", token, params],
    queryFn: () => listNotifyDeliveries(token as string, params),
    enabled: Boolean(token),
    staleTime: 10_000
  });
}

export function useNotifyDeliveryStatsQuery(days = 7) {
  const token = useToken();
  return useQuery({
    queryKey: ["notify-delivery-stats", token, days],
    queryFn: () => getNotifyDeliveryStats(token as string, days),
    enabled: Boolean(token),
    staleTime: 60_000
  });
}

export function usePurgeNotifyDeliveriesMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (days: number) => purgeNotifyDeliveries(token as string, days),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["notify-deliveries"] });
      qc.invalidateQueries({ queryKey: ["notify-delivery-stats"] });
    }
  });
}
