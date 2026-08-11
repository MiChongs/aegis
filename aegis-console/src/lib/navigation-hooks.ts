"use client";

import { useMemo } from "react";
import { navigationGroups, navigationTargets, type NavigationGroup, type NavigationTarget } from "@/lib/navigation";
import { usePermissionChecker } from "@/lib/permissions";

/**
 * 按当前管理员权限过滤分组，整组不可见时连标题一起隐藏。
 *
 * 鉴权必须批量做：`usePermissionChecker()` 是 Hook，不能在 `items.map()` 里逐项调用。
 */
export function useVisibleGroups(): NavigationGroup[] {
  const can = usePermissionChecker();
  return useMemo(
    () =>
      navigationGroups
        .map((group) => ({ ...group, items: group.items.filter((item) => can(item.permission, item.superAdmin)) }))
        .filter((group) => group.items.length > 0),
    [can]
  );
}

/** 当前管理员可见的全部跳转目标（页面 + 页内子项），命令面板 / 收藏 / 最近访问共用 */
export function useVisibleTargets(): NavigationTarget[] {
  const can = usePermissionChecker();
  return useMemo(() => navigationTargets.filter((t) => can(t.permission, t.superAdmin)), [can]);
}

/**
 * key → 目标的索引。
 *
 * 收藏与最近访问只存 key，靠它还原标题与图标；查不到即视为该目标已下线或无权访问，
 * 调用方直接跳过 —— 这就是"收藏不会留下死链"的实现方式。
 */
export function useTargetIndex(): Map<string, NavigationTarget> {
  const targets = useVisibleTargets();
  return useMemo(() => new Map(targets.map((t) => [t.key, t])), [targets]);
}

/** 把一串 key 还原成目标列表，顺序保持不变，无效 key 自动丢弃 */
export function resolveTargets(keys: string[], index: Map<string, NavigationTarget>): NavigationTarget[] {
  const out: NavigationTarget[] = [];
  for (const key of keys) {
    const target = index.get(key);
    if (target) out.push(target);
  }
  return out;
}
