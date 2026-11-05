import { create } from "zustand";
import { persist } from "zustand/middleware";

/**
 * 应用详情页区块导航的个人偏好：最近访问过的区块、折叠起来的分组。
 *
 * 三十多个区块里，一个管理员日常反复进出的通常只有四五个（开关、第三方登录、支付……）。
 * 「最近」一栏让这几个不用每次在长列表里找；折叠状态跨页面保留，
 * 不常用的整组收起后下次进来仍是收起的。与应用无关：换应用时导航习惯不变。
 */

const RECENT_LIMIT = 4;

type AppSectionNavState = {
  recent: string[];
  folded: string[];
  visit: (section: string) => void;
  toggleFolded: (group: string) => void;
  clearRecent: () => void;
};

export const useAppSectionNavStore = create<AppSectionNavState>()(
  persist(
    (set) => ({
      recent: [],
      folded: [],
      visit: (section) =>
        set((state) =>
          state.recent[0] === section
            ? state
            : { recent: [section, ...state.recent.filter((key) => key !== section)].slice(0, RECENT_LIMIT) }
        ),
      toggleFolded: (group) =>
        set((state) => ({
          folded: state.folded.includes(group) ? state.folded.filter((key) => key !== group) : [...state.folded, group]
        })),
      clearRecent: () => set({ recent: [] })
    }),
    { name: "aegis-console-app-section-nav" }
  )
);
