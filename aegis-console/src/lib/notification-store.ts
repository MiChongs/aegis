import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { SystemAnnouncement } from "@/lib/api/types";

type NotificationState = {
  /** 当前生效的公告列表 */
  announcements: SystemAnnouncement[];
  /** 已读的公告 ID（持久化） */
  readIds: number[];
  /** 已关闭的公告 ID（持久化，横幅不重复展示） */
  dismissedIds: number[];
  setAnnouncements: (items: SystemAnnouncement[]) => void;
  markRead: (id: number) => void;
  markAllRead: () => void;
  dismiss: (id: number) => void;
  clearDismissed: () => void;
};

export const useNotificationStore = create<NotificationState>()(
  persist(
    (set, get) => ({
      announcements: [],
      readIds: [],
      dismissedIds: [],
      setAnnouncements: (items) => set({ announcements: items }),
      markRead: (id) =>
        set((s) => ({
          readIds: s.readIds.includes(id) ? s.readIds : [...s.readIds, id],
        })),
      markAllRead: () =>
        set((s) => ({
          readIds: [...new Set([...s.readIds, ...s.announcements.map((a) => a.id)])],
        })),
      dismiss: (id) =>
        set((s) => ({
          dismissedIds: s.dismissedIds.includes(id) ? s.dismissedIds : [...s.dismissedIds, id],
        })),
      clearDismissed: () => set({ dismissedIds: [] }),
    }),
    { name: "aegis-console-notifications" }
  )
);
