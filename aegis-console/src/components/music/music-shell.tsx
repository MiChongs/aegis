"use client";

import { useMemo, type ReactNode } from "react";
import Link from "next/link";
import { ArrowLeft, ListMusic } from "lucide-react";
import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { usePermissionsLoading } from "@/lib/permissions";

/**
 * 音乐推荐三张整页（工作台、用户画像、作品详情）的外壳：返回链接 + 应用名 + 标题 + 右侧操作，
 * 以及读写权限。读要 `app:music:read`，写要 `app:music:manage`（权限树「应用 → 音乐推荐」）。
 */

export const MUSIC_READ = "app:music:read";
export const MUSIC_MANAGE = "app:music:manage";

export function useMusicAccess(appKey: string) {
  const can = useAppKeyPermissionChecker(appKey);
  const loading = usePermissionsLoading();
  return { loading, canRead: can(MUSIC_READ), canManage: can(MUSIC_MANAGE) };
}

export function useAppName(appKey: string) {
  const apps = useAdminAppsQuery();
  return useMemo(() => apps.data?.find((a) => a.appKey === appKey)?.name ?? appKey, [apps.data, appKey]);
}

export function MusicPageHeader({
  appKey,
  title,
  subtitle,
  backHref,
  backLabel,
  icon,
  actions
}: {
  appKey: string;
  title: ReactNode;
  subtitle?: ReactNode;
  backHref: string;
  backLabel: string;
  icon?: ReactNode;
  actions?: ReactNode;
}) {
  const appName = useAppName(appKey);
  return (
    <header className="flex min-w-0 flex-wrap items-center gap-3">
      <Button asChild size="icon-sm" variant="ghost" aria-label={backLabel} title={backLabel}>
        <Link href={backHref}>
          <ArrowLeft />
        </Link>
      </Button>
      {icon ?? <TonalIcon icon={ListMusic} tone="violet" />}
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-xs text-muted-foreground">{appName}</span>
        <h1 className="truncate text-lg leading-6 font-semibold tracking-tight">{title}</h1>
        {subtitle ? <div className="truncate text-xs text-muted-foreground">{subtitle}</div> : null}
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
  );
}

/** 权限门：加载中给占位，无读权限给一行说明 */
export function MusicAccessGate({ appKey, children }: { appKey: string; children: ReactNode }) {
  const { loading, canRead } = useMusicAccess(appKey);
  if (loading) return <LoadingState title="加载中" />;
  if (!canRead) return <EmptyState title="缺少「音乐推荐：查看」权限" />;
  return <>{children}</>;
}
