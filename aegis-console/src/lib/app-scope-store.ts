import { create } from "zustand";
import { persist } from "zustand/middleware";

/**
 * 「我上次在配哪个应用」+ 列表页视图偏好。
 *
 * 存在的理由是侧边栏那 13 个三级子项：它们的链接形如 `/apps?tab=oauth`，
 * 不带 appKey。有了这条记忆，点「第三方登录」才能落回你正在配的那个应用，
 * 而不是每次都回到第一个应用 —— 多应用实例上这个差别就是「能用」与「不能用」。
 */

export type AppListView = "grid" | "table";

type AppScopeState = {
  /** 最近打开过详情的应用 appKey */
  lastAppKey: string | null;
  /** 列表页视图：卡片 / 表格 */
  listView: AppListView;
  setLastAppKey: (appKey: string | null) => void;
  setListView: (view: AppListView) => void;
};

export const useAppScopeStore = create<AppScopeState>()(
  persist(
    (set) => ({
      lastAppKey: null,
      listView: "grid",
      setLastAppKey: (appKey) => set((state) => (state.lastAppKey === appKey ? state : { lastAppKey: appKey })),
      setListView: (listView) => set({ listView })
    }),
    { name: "aegis-console-app-scope" }
  )
);
