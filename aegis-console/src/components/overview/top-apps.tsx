"use client";

import Link from "next/link";
import { LayoutGrid, Trophy } from "lucide-react";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import { CardEmpty, CardLink, formatCount, OverviewCard } from "./overview-kit";

/* 名字首字的底色按应用 ID 轮换，同一应用每次进来颜色一样 */
const MONOGRAM_TONES = [
  "bg-sky-500/12 text-sky-700 dark:text-sky-300",
  "bg-violet-500/12 text-violet-700 dark:text-violet-300",
  "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
  "bg-amber-500/12 text-amber-700 dark:text-amber-300",
  "bg-rose-500/12 text-rose-700 dark:text-rose-300",
  "bg-indigo-500/12 text-indigo-700 dark:text-indigo-300"
];

function monogram(name: string) {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  const first = Array.from(trimmed)[0]!;
  return /[a-z]/i.test(first) ? trimmed.slice(0, 2).toUpperCase() : first;
}

/** 应用活跃排行：近 7 天登录次数前六，条长按第一名归一 */
export function TopApps({ className }: { className?: string }) {
  const { data, isLoading } = useAdminDashboardQuery();
  const apps = data?.topApps ?? [];
  const max = Math.max(1, ...apps.map((app) => app.logins));

  return (
    <OverviewCard
      icon={Trophy}
      tone="amber"
      title="应用活跃排行"
      description="近 7 天登录次数"
      className={className}
      action={<CardLink href="/apps">全部</CardLink>}
    >
      {isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }, (_, index) => (
            <div key={index} className="flex items-center gap-3">
              <Skeleton className="size-8 rounded-lg" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3.5 w-28" />
                <Skeleton className="h-1.5 w-full" />
              </div>
            </div>
          ))}
        </div>
      ) : apps.length === 0 ? (
        <CardEmpty icon={LayoutGrid} title="暂无应用" />
      ) : (
        <ol className="-mx-2 flex flex-col">
          {apps.map((app, index) => (
            <li key={app.id}>
              <Link
                href={`/apps/${encodeURIComponent(app.appKey)}`}
                className="group/app flex min-w-0 items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-muted/60 focus-visible:bg-muted/60 focus-visible:outline-none"
              >
                <span className="w-4 shrink-0 text-center font-data text-[11px] text-muted-foreground tabular-nums">{index + 1}</span>
                <Avatar className="size-8 rounded-lg" preview={false}>
                  <AvatarFallback className={cn("rounded-lg text-xs font-semibold", MONOGRAM_TONES[app.id % MONOGRAM_TONES.length])}>
                    {monogram(app.name)}
                  </AvatarFallback>
                </Avatar>
                <span className="min-w-0 flex-1 space-y-1.5">
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate text-xs font-medium">{app.name}</span>
                    {!app.status ? (
                      <Badge variant="danger" size="sm" className="shrink-0">
                        已停用
                      </Badge>
                    ) : null}
                    <span className="ml-auto shrink-0 font-data text-xs font-semibold tabular-nums">{formatCount(app.logins)}</span>
                  </span>
                  <Progress value={(app.logins / max) * 100} className="h-1.5" aria-label={`${app.name} 登录次数`} />
                  <span className="flex min-w-0 items-center justify-between gap-2 text-[10px] text-muted-foreground">
                    <span className="truncate">用户 {formatCount(app.users)}</span>
                    <span className="shrink-0 tabular-nums">新增 {formatCount(app.registrations)}</span>
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ol>
      )}
    </OverviewCard>
  );
}
