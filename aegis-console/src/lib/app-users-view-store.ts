import { create } from "zustand";
import { persist } from "zustand/middleware";

/**
 * 应用用户列表的「保存的视图」。
 *
 * 存的是查询串（`serializeQuery()` 的产物去掉前缀），不是结构化条件对象 ——
 * 这样新增筛选字段时旧视图**自动继续可用**，也不会因为 UserQueryState
 * 加了字段就要写一次 persist migration。代价是视图里可能存着已删除的参数，
 * `parseQuery()` 会把不认识的忽略掉，这正是想要的行为。
 *
 * 视图按应用隔离：不同应用的用户群不一样，「近 7 天注册的受限用户」
 * 在 A 应用有意义不代表在 B 应用有意义，混在一起只会让列表越用越乱。
 */

export type SavedUserView = {
  id: string;
  name: string;
  appKey: string;
  /** URLSearchParams 序列化结果，含 tab/app */
  query: string;
  createdAt: number;
};

/** 单应用视图上限。超过这个数就不再是"常用视图"，而是另一个需要管理的列表。 */
const MAX_VIEWS_PER_APP = 12;

type ViewState = {
  views: SavedUserView[];
  save: (view: Omit<SavedUserView, "id" | "createdAt">) => void;
  remove: (id: string) => void;
  rename: (id: string, name: string) => void;
};

/**
 * 不用 crypto.randomUUID：视图是纯本地数据，id 只要在本机唯一即可，
 * 而 randomUUID 在非安全上下文（http 局域网访问控制台）下不存在。
 */
function nextId(existing: SavedUserView[]) {
  const max = existing.reduce((acc, item) => {
    const num = Number(item.id.replace(/\D/g, ""));
    return Number.isFinite(num) && num > acc ? num : acc;
  }, 0);
  return `view-${max + 1}`;
}

export const useUserViewStore = create<ViewState>()(
  persist(
    (set) => ({
      views: [],
      save: (view) =>
        set((state) => {
          const name = view.name.trim();
          if (!name) return state;
          // 同名即覆盖：反复「保存当前筛选」不该攒出一堆同名视图
          const others = state.views.filter(
            (item) => !(item.appKey === view.appKey && item.name === name)
          );
          const scoped = others.filter((item) => item.appKey === view.appKey);
          const trimmed =
            scoped.length >= MAX_VIEWS_PER_APP
              ? others.filter((item) => item.id !== scoped[0]?.id)
              : others;
          return {
            views: [
              ...trimmed,
              { ...view, name, id: nextId(state.views), createdAt: Date.now() }
            ]
          };
        }),
      remove: (id) => set((state) => ({ views: state.views.filter((item) => item.id !== id) })),
      rename: (id, name) =>
        set((state) => ({
          views: state.views.map((item) =>
            item.id === id ? { ...item, name: name.trim() || item.name } : item
          )
        }))
    }),
    { name: "aegis-app-user-views" }
  )
);
