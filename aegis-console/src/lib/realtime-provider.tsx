"use client";

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { realtimeClient, type RealtimeEvent } from "@/lib/realtime";
import { useAuthStore } from "@/lib/auth-store";
import { useNotificationStore } from "@/lib/notification-store";
import { toast } from "sonner";
import { getActiveAnnouncements } from "@/lib/api/system";

/* ------------------------------------------------------------------ */
/*  浏览器通知                                                          */
/* ------------------------------------------------------------------ */

/** 请求浏览器通知权限（仅首次） */
function requestNotificationPermission() {
  if (typeof window === "undefined" || !("Notification" in window)) return;
  if (Notification.permission === "default") {
    void Notification.requestPermission();
  }
}

/** 发送浏览器原生通知 */
function showBrowserNotification(title: string, body?: string) {
  if (typeof window === "undefined" || !("Notification" in window)) return;
  if (Notification.permission !== "granted") return;
  // 页面在前台时不弹系统通知（避免打扰）
  if (document.visibilityState === "visible") return;
  try {
    new Notification(title, {
      body: body ? body.replace(/<[^>]*>/g, "").slice(0, 200) : undefined, // 去 HTML 标签
      icon: "/favicon.ico",
      tag: "aegis-announcement", // 同 tag 合并
    });
  } catch { /* Safari 等不支持 constructor 的降级忽略 */ }
}

/* ------------------------------------------------------------------ */
/*  Provider                                                           */
/* ------------------------------------------------------------------ */

export function RealtimeProvider({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.accessToken);
  const setAnnouncements = useNotificationStore((s) => s.setAnnouncements);
  const queryClient = useQueryClient();
  const tokenRef = useRef(token);
  useEffect(() => { tokenRef.current = token; }, [token]);

  // 连接 / 断开
  useEffect(() => {
    if (token) {
      realtimeClient.connect(token);
      requestNotificationPermission();
    } else {
      realtimeClient.disconnect();
    }
  }, [token]);

  // 页面卸载时断开
  useEffect(() => {
    const handleUnload = () => realtimeClient.disconnect();
    window.addEventListener("beforeunload", handleUnload);
    return () => window.removeEventListener("beforeunload", handleUnload);
  }, []);

  // 拉取初始公告
  useEffect(() => {
    if (!token) return;
    getActiveAnnouncements(token)
      .then((items) => setAnnouncements(items))
      .catch(() => {});
  }, [token, setAnnouncements]);

  // 监听 WebSocket 实时事件
  useEffect(() => {
    return realtimeClient.on("system.announcement", (evt: RealtimeEvent) => {
      // 刷新公告列表
      const t = tokenRef.current;
      getActiveAnnouncements(t || undefined)
        .then((items) => setAnnouncements(items))
        .catch(() => {});
      void queryClient.invalidateQueries({ queryKey: ["system-announcements"] });

      // 浏览器原生通知
      const title = (evt.title as string) || (evt.data?.title as string) || "系统公告";
      const content = (evt.data?.content as string) || "";
      showBrowserNotification(title, content);
    });
  }, [setAnnouncements, queryClient]);

  // 监听部门邀请事件
  useEffect(() => {
    if (!realtimeClient) return;
    const unsub1 = realtimeClient.on("dept.invitation.received", (evt: RealtimeEvent) => {
      const inviterName = (evt.data?.inviterName as string) || "某管理员";
      const deptName = (evt.data?.deptName as string) || "某部门";
      toast.info(`${inviterName} 邀请您加入「${deptName}」`, { duration: 8000 });
      void queryClient.invalidateQueries({ queryKey: ["my-invitations"] });
      void queryClient.invalidateQueries({ queryKey: ["invitation-count"] });
      if (typeof Notification !== "undefined" && Notification.permission === "granted" && document.hidden) {
        new Notification("部门邀请", { body: `${inviterName} 邀请您加入「${deptName}」` });
      }
    });
    const unsub2 = realtimeClient.on("dept.invitation.responded", (evt: RealtimeEvent) => {
      const inviteeName = (evt.data?.inviteeName as string) || "某管理员";
      const status = (evt.data?.status as string) || "";
      const deptName = (evt.data?.deptName as string) || "";
      const label = status === "accepted" ? "接受" : "拒绝";
      toast.info(`${inviteeName} ${label}了您对「${deptName}」的邀请`);
      void queryClient.invalidateQueries({ queryKey: ["my-invitations"] });
      void queryClient.invalidateQueries({ queryKey: ["dept-members"] });
      void queryClient.invalidateQueries({ queryKey: ["dept-tree"] });
    });
    return () => { unsub1(); unsub2(); };
  }, [realtimeClient, queryClient]);

  // 管理员收件箱：后端每写入一条通知就推 `admin.notification.created`，
  // 前端据此刷新角标与列表 —— 这条链路是角标实时性的来源，断了就只剩 60s 兜底轮询。
  useEffect(() => {
    return realtimeClient.on("admin.notification.created", (evt: RealtimeEvent) => {
      const unread = Number(evt.data?.unread);
      if (Number.isFinite(unread)) {
        // 服务端已经算好未读数，直接写缓存，省掉一次往返
        queryClient.setQueryData(["admin-inbox-unread", tokenRef.current], { unread });
      } else {
        void queryClient.invalidateQueries({ queryKey: ["admin-inbox-unread"] });
      }
      void queryClient.invalidateQueries({ queryKey: ["admin-inbox"] });
    });
  }, [queryClient]);

  // 工单实时事件：一律以 `ticket.` 前缀分发。
  // 后端把 refreshRequired 放进载荷，前端只需失效相关查询，
  // 不用为每种事件在前端各写一套增量合并逻辑。
  useEffect(() => {
    return realtimeClient.onAny((evt: RealtimeEvent) => {
      if (!evt.type?.startsWith("ticket.")) return;
      // 只处理面向管理员的那一份；同一事件也会推给提单人（audience=user）
      if (evt.data?.audience && evt.data.audience !== "admin") return;

      void queryClient.invalidateQueries({ queryKey: ["tickets"] });
      void queryClient.invalidateQueries({ queryKey: ["ticket-stats"] });
      void queryClient.invalidateQueries({ queryKey: ["ticket-workbench"] });
      const resourceId = evt.data?.resourceId;
      if (resourceId) {
        void queryClient.invalidateQueries({ queryKey: ["ticket-detail"] });
      }

      // 只有真正紧急的才打断当前操作；普通动态靠角标提示即可
      if (evt.data?.level === "critical") {
        const title = (evt.data?.title as string) || "工单需要处理";
        const summary = (evt.data?.summary as string) || "";
        toast.warning(title, { description: summary, duration: 8000 });
      }
    });
  }, [queryClient]);

  return <>{children}</>;
}
