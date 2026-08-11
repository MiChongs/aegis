"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { ArrowLeft, Bell, CheckCheck, Inbox, Megaphone, Trash2, WifiOff } from "lucide-react";
import { Popover as PopoverPrimitive } from "radix-ui";
import { Button } from "@/components/ui/button";
import { RichContent } from "@/components/ui/rich-editor";
import { useNotificationStore } from "@/lib/notification-store";
import {
  useAdminInboxQuery,
  useAdminInboxUnreadQuery,
  useDeleteAdminInboxMutation,
  useMarkAdminInboxReadMutation,
  useRealtimeStatus
} from "@/lib/inbox-hooks";
import type { AdminInboxItem } from "@/lib/api/admin-inbox";
import type { SystemAnnouncement } from "@/lib/api/types";
import { cn } from "@/lib/utils";

// 顶栏通知铃铛，两个来源合并在一处：
//
//   通知（inbox）—— 服务端 admin_notifications，工单指派 / SLA 超时等**针对你个人**的消息
//   公告（announcements）—— 平台广播，已读状态存在本地 store
//
// 角标 = 收件箱未读 + 未读公告。收件箱未读由 WebSocket 的
// `admin.notification.created` 事件驱动刷新（realtime-provider.tsx），断线时靠 60s 轮询兜底。

function fmtNotifDate(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const diff = Date.now() - date.getTime();
  if (diff >= 0 && diff < 60_000) return "刚刚";
  if (diff >= 0 && diff < 3600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

const announcementDot: Record<string, string> = {
  critical: "bg-red-500",
  important: "bg-amber-500",
  normal: "bg-blue-500"
};

const inboxDot: Record<string, string> = {
  critical: "bg-red-500",
  warning: "bg-amber-500",
  success: "bg-emerald-500",
  info: "bg-blue-500"
};

type Tab = "inbox" | "announcements";

export function NotificationBell() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<Tab>("inbox");
  const [selectedAnnouncement, setSelectedAnnouncement] = useState<number | null>(null);

  // ── 收件箱 ──
  const unreadQuery = useAdminInboxUnreadQuery();
  // 面板关着时不拉列表，省掉每个管理员每分钟一次的无谓查询
  const inboxQuery = useAdminInboxQuery({ page: 1, limit: 20 }, open);
  const markReadMut = useMarkAdminInboxReadMutation();
  const deleteMut = useDeleteAdminInboxMutation();

  // ── 公告 ──
  const announcements = useNotificationStore((s) => s.announcements);
  const readIds = useNotificationStore((s) => s.readIds);
  const markAnnouncementRead = useNotificationStore((s) => s.markRead);
  const markAllAnnouncementsRead = useNotificationStore((s) => s.markAllRead);

  // 长连接断开时通知不再即时到达，只剩兜底轮询 —— 必须让使用者看得见
  const realtimeStatus = useRealtimeStatus();
  const realtimeDown = realtimeStatus === "closed";

  const inboxUnread = unreadQuery.data?.unread ?? 0;
  const announcementUnread = useMemo(
    () => announcements.filter((a) => !readIds.includes(a.id)).length,
    [announcements, readIds]
  );
  const totalUnread = inboxUnread + announcementUnread;

  const selected = selectedAnnouncement
    ? announcements.find((a) => a.id === selectedAnnouncement) ?? null
    : null;

  const handleInboxClick = async (item: AdminInboxItem) => {
    if (item.status === "unread") {
      try {
        await markReadMut.mutateAsync([item.id]);
      } catch {
        /* 已读失败不阻断跳转 */
      }
    }
    if (item.link) {
      setOpen(false);
      router.push(item.link);
    }
  };

  const handleSelectAnnouncement = (id: number) => {
    markAnnouncementRead(id);
    setSelectedAnnouncement(id);
  };

  return (
    <PopoverPrimitive.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setSelectedAnnouncement(null);
      }}
    >
      <PopoverPrimitive.Trigger asChild>
        <Button variant="ghost" size="icon" className="relative size-8">
          <Bell className="size-3.5" />
          {totalUnread > 0 && (
            <span className="absolute -right-0.5 -top-0.5 flex size-4 items-center justify-center rounded-full bg-red-500 text-[9px] font-bold text-white">
              {totalUnread > 9 ? "9+" : totalUnread}
            </span>
          )}
          {totalUnread === 0 && realtimeDown && (
            <span
              title="实时连接已断开，通知可能延迟"
              className="absolute -right-0.5 -top-0.5 size-2 rounded-full bg-amber-500"
            />
          )}
        </Button>
      </PopoverPrimitive.Trigger>

      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="end"
          sideOffset={8}
          className="z-50 flex max-h-112 w-96 flex-col overflow-hidden rounded-xl border bg-popover shadow-xl animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95"
        >
          {selected ? (
            <AnnouncementDetail item={selected} onBack={() => setSelectedAnnouncement(null)} />
          ) : (
            <>
              {/* 标签页 */}
              <div className="flex shrink-0 items-center gap-1 border-b px-2 py-1.5">
                <TabButton
                  active={tab === "inbox"}
                  onClick={() => setTab("inbox")}
                  icon={<Inbox className="size-3" />}
                  label="通知"
                  badge={inboxUnread}
                />
                <TabButton
                  active={tab === "announcements"}
                  onClick={() => setTab("announcements")}
                  icon={<Megaphone className="size-3" />}
                  label="公告"
                  badge={announcementUnread}
                />
                <div className="ml-auto flex items-center gap-1">
                  {tab === "inbox" ? (
                    <>
                      {inboxUnread > 0 && (
                        <button
                          type="button"
                          onClick={() => markReadMut.mutate(undefined)}
                          className="flex items-center gap-1 rounded px-1.5 py-1 text-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                        >
                          <CheckCheck className="size-3" />
                          全部已读
                        </button>
                      )}
                      <button
                        type="button"
                        title="清空已读通知"
                        onClick={() => deleteMut.mutate({ onlyRead: true })}
                        className="flex items-center gap-1 rounded px-1.5 py-1 text-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                      >
                        <Trash2 className="size-3" />
                        清理
                      </button>
                    </>
                  ) : (
                    announcementUnread > 0 && (
                      <button
                        type="button"
                        onClick={markAllAnnouncementsRead}
                        className="flex items-center gap-1 rounded px-1.5 py-1 text-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                      >
                        <CheckCheck className="size-3" />
                        全部已读
                      </button>
                    )
                  )}
                </div>
              </div>

              {realtimeDown ? (
                <div className="flex shrink-0 items-center gap-1.5 border-b bg-amber-50 px-3 py-1.5 text-[11px] text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">
                  <WifiOff className="size-3 shrink-0" />
                  实时连接已断开，通知最长延迟 1 分钟
                </div>
              ) : null}

              <div className="min-h-0 flex-1 overflow-y-auto">
                {tab === "inbox" ? (
                  <InboxList
                    items={inboxQuery.data?.items ?? []}
                    loading={inboxQuery.isLoading}
                    onSelect={handleInboxClick}
                    onDelete={(id) => deleteMut.mutate({ ids: [id] })}
                  />
                ) : (
                  <AnnouncementList
                    items={announcements}
                    readIds={readIds}
                    onSelect={handleSelectAnnouncement}
                  />
                )}
              </div>
            </>
          )}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  );
}

function TabButton({
  active,
  onClick,
  icon,
  label,
  badge
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
  badge: number;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors",
        active ? "bg-accent font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
      )}
    >
      {icon}
      {label}
      {badge > 0 && (
        <span className="rounded-full bg-red-500 px-1 text-[9px] font-bold text-white">
          {badge > 99 ? "99+" : badge}
        </span>
      )}
    </button>
  );
}

function InboxList({
  items,
  loading,
  onSelect,
  onDelete
}: {
  items: AdminInboxItem[];
  loading: boolean;
  onSelect: (item: AdminInboxItem) => void;
  onDelete: (id: number) => void;
}) {
  if (loading) {
    return <div className="py-10 text-center text-xs text-muted-foreground">加载中...</div>;
  }
  if (items.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-muted-foreground">
        <Inbox className="mb-2 size-8 opacity-20" />
        <span className="text-xs">暂无通知</span>
      </div>
    );
  }
  return (
    <>
      {items.map((item) => {
        const isRead = item.status === "read";
        return (
          <div
            key={item.id}
            className={cn(
              "group flex items-start gap-2 border-b border-border/50 px-3 py-2.5 transition-colors last:border-b-0 hover:bg-accent/50",
              !isRead && "bg-accent/20"
            )}
          >
            <span
              className={cn(
                "mt-1.5 size-1.5 shrink-0 rounded-full",
                inboxDot[item.level] || inboxDot.info,
                isRead && "opacity-30"
              )}
            />
            <button
              type="button"
              onClick={() => onSelect(item)}
              className="min-w-0 flex-1 text-left"
              // 有 link 才是可跳转的，否则只是把它标为已读
              title={item.link ? "点击查看详情" : undefined}
            >
              <div className="flex items-center gap-1.5">
                <span className={cn("truncate text-xs", isRead ? "text-muted-foreground" : "font-medium")}>
                  {item.title}
                </span>
                {!isRead && <span className="size-1.5 shrink-0 rounded-full bg-blue-500" />}
              </div>
              {item.content && item.content !== item.title ? (
                <p className="mt-0.5 line-clamp-2 text-[11px] leading-4 text-muted-foreground">{item.content}</p>
              ) : null}
              <span className="mt-0.5 block text-[10px] text-muted-foreground">{fmtNotifDate(item.createdAt)}</span>
            </button>
            <button
              type="button"
              title="删除"
              onClick={() => onDelete(item.id)}
              className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-destructive group-hover:opacity-100"
            >
              <Trash2 className="size-3" />
            </button>
          </div>
        );
      })}
    </>
  );
}

function AnnouncementList({
  items,
  readIds,
  onSelect
}: {
  items: SystemAnnouncement[];
  readIds: number[];
  onSelect: (id: number) => void;
}) {
  if (items.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-10 text-muted-foreground">
        <Megaphone className="mb-2 size-8 opacity-20" />
        <span className="text-xs">暂无公告</span>
      </div>
    );
  }
  return (
    <>
      {items.map((item) => {
        const isRead = readIds.includes(item.id);
        return (
          <button
            key={item.id}
            type="button"
            onClick={() => onSelect(item.id)}
            className={cn(
              "w-full border-b border-border/50 px-3 py-2.5 text-left transition-colors last:border-b-0 hover:bg-accent/50",
              !isRead && "bg-accent/20"
            )}
          >
            <div className="flex items-start gap-2">
              <span
                className={cn(
                  "mt-1.5 size-1.5 shrink-0 rounded-full",
                  announcementDot[item.level] || announcementDot.normal,
                  isRead && "opacity-30"
                )}
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <span className={cn("truncate text-xs", isRead ? "text-muted-foreground" : "font-medium")}>
                    {item.title}
                  </span>
                  {!isRead && <span className="size-1.5 shrink-0 rounded-full bg-blue-500" />}
                </div>
                <div className="mt-0.5 flex items-center gap-2">
                  <span className="text-[10px] text-muted-foreground">
                    {fmtNotifDate(item.publishedAt || item.createdAt)}
                  </span>
                  {item.pinned && <span className="text-[10px] text-muted-foreground">置顶</span>}
                </div>
              </div>
            </div>
          </button>
        );
      })}
    </>
  );
}

function AnnouncementDetail({ item, onBack }: { item: SystemAnnouncement; onBack: () => void }) {
  return (
    <>
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2.5">
        <button
          type="button"
          title="返回列表"
          onClick={onBack}
          className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" />
        </button>
        <span className="flex-1 truncate text-xs font-medium">{item.title}</span>
      </div>
      <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={cn("size-2 shrink-0 rounded-full", announcementDot[item.level] || announcementDot.normal)}
          />
          <span className="text-[10px] text-muted-foreground">{item.adminName || "系统"}</span>
          <span className="text-[10px] text-muted-foreground">
            {fmtNotifDate(item.publishedAt || item.createdAt)}
          </span>
          {item.pinned && <span className="text-[10px] font-medium text-muted-foreground">置顶</span>}
        </div>
        <h3 className="text-sm font-semibold leading-snug">{item.title}</h3>
        {item.content ? (
          <RichContent html={item.content} className="text-xs" />
        ) : (
          <p className="text-xs text-muted-foreground">无正文内容。</p>
        )}
      </div>
    </>
  );
}
