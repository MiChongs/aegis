"use client";

import { useSyncExternalStore } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@/lib/auth-store";
import { realtimeClient, type RealtimeStatus } from "@/lib/realtime";
import {
  deleteAdminInbox,
  getAdminInboxUnread,
  listAdminInbox,
  markAdminInboxRead,
  type AdminInboxParams
} from "@/lib/api/admin-inbox";

// 管理员收件箱 hooks。
//
// 刷新策略：正常路径由 WebSocket 的 `admin.notification.created` 驱动
// （见 realtime-provider.tsx），这里的 refetchInterval 只是**断线兜底**，
// 因此周期取得比较长（60s），不要为了"更实时"把它调短 —— 那等于退回轮询。

function useToken() {
  return useAuthStore((state) => state.accessToken);
}

export const ADMIN_INBOX_KEY = "admin-inbox";
export const ADMIN_INBOX_UNREAD_KEY = "admin-inbox-unread";

/**
 * 实时长连接状态。
 *
 * 通知能否"即时"到达完全取决于这条连接；断开时只剩 60s 兜底轮询。
 * 把它显式暴露到 UI，是为了避免"收不到通知"这类问题再次只能靠猜。
 * 用 useSyncExternalStore 而非 useState+useEffect：SSR 下不订阅，也不会有中间态闪烁。
 */
export function useRealtimeStatus(): RealtimeStatus {
  return useSyncExternalStore(
    (onChange) => realtimeClient.onStatus(() => onChange()),
    () => realtimeClient.status,
    () => "idle" as RealtimeStatus
  );
}

export function useAdminInboxQuery(params: AdminInboxParams = {}, enabled = true) {
  const token = useToken();
  return useQuery({
    queryKey: [ADMIN_INBOX_KEY, token, params],
    queryFn: () => listAdminInbox(token as string, params),
    enabled: Boolean(token) && enabled,
    staleTime: 10_000
  });
}

/** 角标未读数。WS 断线时靠 60s 兜底轮询保持大致准确 */
export function useAdminInboxUnreadQuery() {
  const token = useToken();
  return useQuery({
    queryKey: [ADMIN_INBOX_UNREAD_KEY, token],
    queryFn: () => getAdminInboxUnread(token as string),
    enabled: Boolean(token),
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true
  });
}

function invalidateInbox(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: [ADMIN_INBOX_KEY] });
  qc.invalidateQueries({ queryKey: [ADMIN_INBOX_UNREAD_KEY] });
}

export function useMarkAdminInboxReadMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids?: number[]) => markAdminInboxRead(token as string, ids),
    onSuccess: (result) => {
      // 服务端回传的 unread 是权威值，直接写进缓存避免"点了已读角标还挂着"的闪烁
      qc.setQueryData([ADMIN_INBOX_UNREAD_KEY, token], { unread: result.unread });
      invalidateInbox(qc);
    }
  });
}

export function useDeleteAdminInboxMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { ids?: number[]; onlyRead?: boolean }) =>
      deleteAdminInbox(token as string, args.ids, args.onlyRead ?? false),
    onSuccess: (result) => {
      qc.setQueryData([ADMIN_INBOX_UNREAD_KEY, token], { unread: result.unread });
      invalidateInbox(qc);
    }
  });
}
