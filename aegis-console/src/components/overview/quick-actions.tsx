"use client";

import { useMemo } from "react";
import Link from "next/link";
import { ArrowUpRight, Zap } from "lucide-react";
import type { KitTone } from "@/components/apps/detail/app-detail-kit";
import { navigationItems } from "@/lib/navigation";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { OverviewCard } from "./overview-kit";

/**
 * 首屏快捷入口。
 *
 * 只登记 href 与色调，标题 / 图标 / 说明一律从 `navigation.ts` 反查，
 * 这样侧边栏改名换图标时这里自动跟随；href 若已从导航里移除则该项自动消失。
 */
const QUICK_ACTIONS: Array<{ href: string; tone: KitTone }> = [
  { href: "/apps", tone: "primary" },
  { href: "/users", tone: "sky" },
  { href: "/content", tone: "violet" },
  { href: "/reports", tone: "emerald" },
  { href: "/organization", tone: "amber" },
  { href: "/security", tone: "rose" },
  { href: "/storage", tone: "sky" },
  { href: "/workflows", tone: "violet" }
];

const TONE_PLATE: Record<KitTone, string> = {
  primary: "bg-primary/10 text-primary",
  emerald: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400",
  amber: "bg-amber-500/12 text-amber-600 dark:text-amber-400",
  sky: "bg-sky-500/12 text-sky-600 dark:text-sky-400",
  violet: "bg-violet-500/12 text-violet-600 dark:text-violet-400",
  rose: "bg-rose-500/12 text-rose-600 dark:text-rose-400",
  muted: "bg-muted text-muted-foreground"
};

export function QuickActions({ className }: { className?: string }) {
  const can = usePermissionChecker();

  const actions = useMemo(
    () =>
      QUICK_ACTIONS.flatMap(({ href, tone }) => {
        const item = navigationItems.find((nav) => nav.href === href);
        return item && can(item.permission, item.superAdmin) ? [{ ...item, tone }] : [];
      }),
    [can]
  );

  if (actions.length === 0) return null;

  return (
    <OverviewCard icon={Zap} tone="primary" title="快捷入口" className={className}>
      {/* 手机上是四列图标启动台，平板起换成带说明的磁贴 */}
      <nav aria-label="快捷入口" className="grid grid-cols-4 gap-x-2 gap-y-3 sm:grid-cols-2 sm:gap-2 md:grid-cols-4">
        {actions.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "group/action flex min-w-0 flex-col items-center gap-1.5 rounded-xl py-1 text-center transition-colors",
                "focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                "sm:flex-row sm:items-center sm:gap-3 sm:border sm:px-3 sm:py-2.5 sm:text-left sm:hover:bg-muted/50"
              )}
            >
              <span
                className={cn(
                  "grid size-11 shrink-0 place-items-center rounded-2xl transition-transform group-active/action:scale-95 sm:size-9 sm:rounded-lg",
                  TONE_PLATE[item.tone]
                )}
              >
                <Icon className="size-5 sm:size-4" />
              </span>
              <span className="w-full min-w-0 sm:flex-1">
                <span className="block truncate text-[11px] font-medium text-foreground sm:text-xs">{item.title}</span>
                <span className="hidden truncate text-[11px] text-muted-foreground sm:block">{item.summary}</span>
              </span>
              <ArrowUpRight className="hidden size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/action:opacity-70 sm:block" />
            </Link>
          );
        })}
      </nav>
    </OverviewCard>
  );
}
