"use client";

import { useMemo } from "react";
import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { navigationItems } from "@/lib/navigation";
import { usePermissionChecker } from "@/lib/permissions";

/**
 * 首屏快捷入口。
 *
 * 只登记 href，标题 / 图标 / 说明一律从 `navigation.ts` 反查，
 * 这样侧边栏改名换图标时这里自动跟随；href 若已从导航里移除则该项自动消失。
 */
const QUICK_ACTION_HREFS = [
  "/apps",
  "/users",
  "/content",
  "/reports",
  "/organization",
  "/security",
  "/storage",
  "/workflows",
];

export function QuickActions() {
  const can = usePermissionChecker();

  const actions = useMemo(
    () =>
      QUICK_ACTION_HREFS.map((href) => navigationItems.find((item) => item.href === href))
        .filter((item) => !!item)
        .filter((item) => can(item.permission, item.superAdmin)),
    [can]
  );

  if (actions.length === 0) return null;

  return (
    <section aria-label="快捷入口" className="space-y-2.5">
      <h2 className="px-0.5 text-[11px] font-medium tracking-wide text-muted-foreground">快捷入口</h2>
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-8">
        {actions.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              className="group/action flex flex-col gap-2 bg-card px-3.5 py-3 transition-colors hover:bg-muted/60"
            >
              <span className="flex items-center justify-between">
                <span className="flex size-7 items-center justify-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover/action:text-foreground">
                  <Icon className="size-3.5" />
                </span>
                <ArrowUpRight className="size-3 text-muted-foreground opacity-0 transition-opacity group-hover/action:opacity-70" />
              </span>
              <span className="min-w-0">
                <span className="block truncate text-xs font-medium text-foreground">{item.title}</span>
                <span className="block truncate text-[10px] text-muted-foreground">{item.summary}</span>
              </span>
            </Link>
          );
        })}
      </div>
    </section>
  );
}
