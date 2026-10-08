"use client";

import Link from "next/link";
import { motion, useReducedMotion } from "motion/react";
import { Star } from "lucide-react";
import { BrandLogo } from "@/components/layout/brand";
import { UserMenu } from "@/components/layout/topbar/user-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { NavigationGroup } from "@/lib/navigation";
import type { OperatorIdentity } from "@/lib/operator";
import { cn } from "@/lib/utils";

/** 收藏在轨道上占一个固定位置，用这个 key 区别于导航分组 */
export const PINNED_SECTION_KEY = "__pinned__";

/**
 * 左侧图标轨道：一个分组一个图标。
 *
 * 点击只切换右侧面板显示哪一组，不跳转 —— 先看清这一组有什么，再决定去哪。
 * 面板收起时点击则直接展开面板，同样不跳转，避免误点一下就丢掉当前页。
 * 当前路由所在的分组即使没被选中也带一个小圆点，回头时知道自己从哪来。
 */
export function NavRail({
  groups,
  selectedKey,
  routeGroupKey,
  hasPins,
  operator,
  onSelect,
  onOpenPalette,
  onLogout
}: {
  groups: NavigationGroup[];
  selectedKey: string | null;
  routeGroupKey: string | null;
  hasPins: boolean;
  operator: OperatorIdentity;
  onSelect: (key: string) => void;
  onOpenPalette: () => void;
  onLogout: () => void;
}) {
  const reduced = useReducedMotion();

  const entries = [
    ...(hasPins
      ? [{ key: PINNED_SECTION_KEY, title: "收藏", summary: "常用页面", icon: Star }]
      : []),
    ...groups.map((group) => ({ key: group.key, title: group.title, summary: group.summary, icon: group.icon }))
  ];

  return (
    <div className="flex h-full flex-col items-center gap-4 py-3">
      <Link href="/overview" aria-label="控制台首页" className="rounded-xl p-1 transition-opacity hover:opacity-85">
        <BrandLogo size="lg" className="size-8 rounded-xl" />
      </Link>

      <nav aria-label="导航分组" className="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-y-auto">
        {entries.map((entry) => {
          const selected = entry.key === selectedKey;
          const isRoute = entry.key === routeGroupKey;
          const Icon = entry.icon;
          return (
            <Tooltip key={entry.key}>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  aria-label={entry.title}
                  aria-current={selected ? "true" : undefined}
                  onClick={() => onSelect(entry.key)}
                  className={cn(
                    "relative flex size-10 items-center justify-center rounded-xl transition-colors",
                    "focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none",
                    selected ? "text-foreground" : "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground"
                  )}
                >
                  {selected ? (
                    <motion.span
                      layoutId="rail-pill"
                      aria-hidden
                      className="absolute inset-0 rounded-xl bg-sidebar-accent ring-1 ring-sidebar-border"
                      transition={reduced ? { duration: 0 } : { type: "spring", stiffness: 520, damping: 44, mass: 0.7 }}
                    />
                  ) : null}
                  <Icon
                    className={cn(
                      "relative size-[18px]",
                      entry.key === PINNED_SECTION_KEY && selected && "fill-amber-400 text-amber-500"
                    )}
                  />
                  {isRoute && !selected ? (
                    <span aria-hidden className="absolute top-1.5 right-1.5 size-1.5 rounded-full bg-foreground/70" />
                  ) : null}
                </button>
              </TooltipTrigger>
              <TooltipContent side="right" sideOffset={10}>
                <p className="font-medium">{entry.title}</p>
                <p className="text-[11px] text-background/70">{entry.summary}</p>
              </TooltipContent>
            </Tooltip>
          );
        })}
      </nav>

      <UserMenu variant="rail" operator={operator} onOpenPalette={onOpenPalette} onLogout={onLogout} />
    </div>
  );
}
