"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { useEffect } from "react";
import { currentTargetKey } from "@/lib/navigation";
import { useSidebarStore } from "@/lib/sidebar-store";

/**
 * 最近访问打点。
 *
 * 只记「命中导航目录的路径」：`/apps/{appKey}?tab=oauth` 会被归到 `/apps?tab=oauth`，
 * 而 `/profile` 这类不在目录里的页面不入账 —— 最近访问要能一键回去，
 * 记一条跳不回原处的路径没有意义。
 *
 * 单独抽成一个渲染 `null` 的组件，是为了把 `useSearchParams()` 的 Suspense 边界
 * 收窄到这里：套在整个 Shell 上会让主内容区在边界 resolve 时整体重挂载。
 */
export function RecentTracker() {
  const pathname = usePathname();
  const activeTab = useSearchParams().get("tab");
  const pushRecent = useSidebarStore((s) => s.pushRecent);

  useEffect(() => {
    const key = currentTargetKey(pathname, activeTab);
    if (key) pushRecent(key);
  }, [pathname, activeTab, pushRecent]);

  return null;
}
