import { create } from "zustand";
import { persist } from "zustand/middleware";

/** 折叠态轨道宽度（图标 + 左右呼吸），与 `SidebarRail` 的 size-9 图标对齐 */
export const SIDEBAR_RAIL_WIDTH = 60;
/** 展开态可拖拽区间：再窄放不下二级标题，再宽挤压内容区 */
export const SIDEBAR_MIN_WIDTH = 208;
export const SIDEBAR_MAX_WIDTH = 380;
export const SIDEBAR_DEFAULT_WIDTH = 240;

/** 最近访问保留条数：多于这个数就不再是"最近"，命令面板里也翻不完 */
const MAX_RECENTS = 8;

function clampWidth(width: number): number {
  if (!Number.isFinite(width)) return SIDEBAR_DEFAULT_WIDTH;
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

type SidebarState = {
  collapsed: boolean;
  /** 展开态宽度（px），由右边缘拖拽手柄改写 */
  width: number;
  /** 被手动折叠的分组 key（默认全部展开，只记录例外，这样新增分组天然可见） */
  closedGroups: string[];
  /**
   * 当前展开子项的页面 href。
   * 同时只保留一棵子树（手风琴），避免几个 Tab 大户同时展开把侧边栏撑得过长。
   */
  expandedItem: string | null;
  /**
   * 收藏的跳转目标 key（`/users` 或 `/users?tab=admins`），顺序即展示顺序。
   *
   * 只存 key 不存标题：标题由 `navigation.ts` 的目录实时解析，
   * 改个显示名不会让收藏区留下一条过期文案，删掉的页面也会自动从收藏里消失。
   */
  pinned: string[];
  /** 最近访问的目标 key，最新在前 */
  recents: string[];
  toggle: () => void;
  setCollapsed: (collapsed: boolean) => void;
  setWidth: (width: number) => void;
  resetWidth: () => void;
  toggleGroup: (key: string) => void;
  toggleItem: (href: string) => void;
  /** 路由切换时展开当前页面所在分组，保证「我在哪」始终可见 */
  ensureGroupOpen: (key: string) => void;
  /** 路由切换时展开当前页面的子树 */
  ensureItemExpanded: (href: string) => void;
  togglePin: (key: string) => void;
  /** 拖拽排序后整体覆盖 */
  setPinned: (keys: string[]) => void;
  pushRecent: (key: string) => void;
  clearRecents: () => void;
};

export const useSidebarStore = create<SidebarState>()(
  persist(
    (set) => ({
      collapsed: false,
      width: SIDEBAR_DEFAULT_WIDTH,
      closedGroups: [],
      expandedItem: null,
      pinned: [],
      recents: [],
      toggle: () => set((state) => ({ collapsed: !state.collapsed })),
      setCollapsed: (collapsed) => set({ collapsed }),
      setWidth: (width) => set({ width: clampWidth(width) }),
      resetWidth: () => set({ width: SIDEBAR_DEFAULT_WIDTH }),
      toggleGroup: (key) =>
        set((state) => ({
          closedGroups: state.closedGroups.includes(key)
            ? state.closedGroups.filter((k) => k !== key)
            : [...state.closedGroups, key]
        })),
      toggleItem: (href) => set((state) => ({ expandedItem: state.expandedItem === href ? null : href })),
      ensureGroupOpen: (key) =>
        set((state) =>
          state.closedGroups.includes(key) ? { closedGroups: state.closedGroups.filter((k) => k !== key) } : state
        ),
      ensureItemExpanded: (href) => set((state) => (state.expandedItem === href ? state : { expandedItem: href })),
      togglePin: (key) =>
        set((state) => ({
          pinned: state.pinned.includes(key) ? state.pinned.filter((k) => k !== key) : [...state.pinned, key]
        })),
      setPinned: (keys) => set({ pinned: keys }),
      pushRecent: (key) =>
        set((state) =>
          state.recents[0] === key
            ? state
            : { recents: [key, ...state.recents.filter((k) => k !== key)].slice(0, MAX_RECENTS) }
        ),
      clearRecents: () => set({ recents: [] })
    }),
    // 不升 version：persist 默认的浅合并会用上面的默认值补齐新增字段，
    // 老用户存的 { collapsed } 因此能直接带着走，不需要迁移函数
    { name: "aegis-console-sidebar" }
  )
);
