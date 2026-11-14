"use client";

import { useMemo } from "react";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useAppPermissionChecker } from "@/lib/permissions";

/**
 * 按 appKey 判定的权限检查函数。
 *
 * 应用级面板手里通常只有 appKey，这里借应用列表（与切换器共用缓存）换成应用 ID 再按该应用判定；
 * 列表还没回来时退化为不分作用域的判定，回来后自动收窄。
 */
export function useAppKeyPermissionChecker(appKey?: string | null): (permission?: string | string[]) => boolean {
  const apps = useAdminAppsQuery();
  const appId = useMemo(
    () => (appKey ? (apps.data?.find((app) => app.appKey === appKey)?.id ?? null) : null),
    [apps.data, appKey]
  );
  return useAppPermissionChecker(appId);
}
