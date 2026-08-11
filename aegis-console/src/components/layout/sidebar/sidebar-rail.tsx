"use client";

import Link from "next/link";
import { useMemo } from "react";
import { LayoutGroup } from "motion/react";
import { Star } from "lucide-react";
import { activeChildTab, childHref, isItemActive, type NavigationGroup, type NavigationItem, type NavigationTarget } from "@/lib/navigation";
import { resolveTargets, useTargetIndex, useVisibleGroups } from "@/lib/navigation-hooks";
import { useSidebarStore } from "@/lib/sidebar-store";
import { cn } from "@/lib/utils";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ActivePill, PinToggle } from "@/components/layout/sidebar/sidebar-shared";
import { isTargetActive, type NavTreeProps } from "@/components/layout/sidebar/sidebar-nav";

/* ------------------------------------------------------------------ */
/*  折叠态：图标轨道                                                    */
/* ------------------------------------------------------------------ */

/**
 * 折叠态的核心矛盾：只剩一列图标，任何「二级信息」都必须靠悬浮浮层交付。
 * 因此这里的浮层不只显示标题，还把该页的全部页内面板列出来 ——
 * 收起侧边栏之后依然能一步直达 `/users?tab=roles`，而不是先展开再点两下。
 */
export function SidebarRailTree({ pathname, activeTab }: NavTreeProps) {
  const groups = useVisibleGroups();
  const pinned = useSidebarStore((s) => s.pinned);
  const targetIndex = useTargetIndex();
  const pinnedTargets = useMemo(() => resolveTargets(pinned, targetIndex), [pinned, targetIndex]);

  return (
    <LayoutGroup id="rail">
      <nav className="flex flex-col items-center gap-0.5">
        {pinnedTargets.length ? (
          <div className="flex w-full flex-col items-center gap-0.5">
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="mb-0.5 flex h-4 items-center justify-center">
                  <Star className="size-3 fill-amber-400 text-amber-500" />
                </span>
              </TooltipTrigger>
              <TooltipContent side="right" sideOffset={8}>收藏</TooltipContent>
            </Tooltip>
            {pinnedTargets.map((target) => (
              <RailPinnedItem key={target.key} target={target} pathname={pathname} activeTab={activeTab} />
            ))}
            <span aria-hidden className="my-1.5 h-px w-5 bg-sidebar-border" />
          </div>
        ) : null}

        {groups.map((group, groupIndex) => (
          <div key={group.key} className="flex w-full flex-col items-center gap-0.5">
            {groupIndex > 0 ? <span aria-hidden className="my-1.5 h-px w-5 bg-sidebar-border" /> : null}
            {group.items.map((item) => (
              <RailItem
                key={item.href}
                item={item}
                group={group}
                pathname={pathname}
                active={isItemActive(pathname, item)}
                activeTab={activeTab}
              />
            ))}
          </div>
        ))}
      </nav>
    </LayoutGroup>
  );
}

function RailPinnedItem({
  target,
  pathname,
  activeTab
}: {
  target: NavigationTarget;
  pathname: string;
  activeTab: string | null;
}) {
  const Icon = target.icon;
  const active = isTargetActive(target, pathname, activeTab);

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Link
          href={target.key}
          className={cn(
            "relative flex size-8 items-center justify-center rounded-lg transition-colors",
            "text-muted-foreground hover:bg-sidebar-accent hover:text-accent-foreground",
            active && "text-foreground"
          )}
        >
          {active ? <ActivePill layoutId="rail-pin-pill" /> : null}
          <Icon className="relative z-10 size-3.5" />
        </Link>
      </TooltipTrigger>
      <TooltipContent side="right" sideOffset={8}>
        <p>{target.title}</p>
        {/* tooltip 是反色底（bg-foreground），次要文案不能用 muted-foreground */}
        <p className="text-[10px] text-background/70">{target.isChild ? target.itemTitle : target.groupTitle}</p>
      </TooltipContent>
    </Tooltip>
  );
}

function RailItem({
  item,
  group,
  pathname,
  active,
  activeTab
}: {
  item: NavigationItem;
  group: NavigationGroup;
  pathname: string;
  active: boolean;
  activeTab: string | null;
}) {
  const Icon = item.icon;
  const children = item.children;
  const currentTab = activeChildTab(pathname, item, activeTab);

  const trigger = (
    <Link
      className={cn(
        "relative flex size-9 items-center justify-center rounded-lg transition-colors",
        "text-muted-foreground hover:bg-sidebar-accent hover:text-accent-foreground",
        active && "text-foreground"
      )}
      href={item.href}
    >
      {active ? <ActivePill layoutId="rail-pill" /> : null}
      <Icon className="relative z-10 size-4" />
    </Link>
  );

  // 无子项：普通 tooltip
  if (!children?.length) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>{trigger}</TooltipTrigger>
        <TooltipContent side="right" sideOffset={8}>
          <p>{item.title}</p>
          <p className="text-[10px] text-background/70">{group.title}</p>
        </TooltipContent>
      </Tooltip>
    );
  }

  // 有子项：悬浮浮层里直接列出，收起状态下也能直达页内面板
  return (
    <HoverCard openDelay={120} closeDelay={100}>
      <HoverCardTrigger asChild>{trigger}</HoverCardTrigger>
      <HoverCardContent side="right" align="start" sideOffset={8} className="w-52 p-1">
        {/* `group` 不能少：PinToggle 未收藏时是 opacity-0 group-hover:opacity-100，
            缺了这个类它永远不显形，等于这里没有收藏入口 */}
        <div className="group flex items-start gap-1 px-2 py-1.5">
          <div className="min-w-0 flex-1">
            <p className="truncate text-xs font-medium">{item.title}</p>
            <p className="truncate text-[10px] text-muted-foreground">{item.summary || group.title}</p>
          </div>
          <PinToggle targetKey={item.href} title={item.title} size="sm" />
        </div>
        <div className="max-h-80 overflow-y-auto">
          {children.map((child) => {
            const href = childHref(item, child);
            const childActive = active && currentTab === child.tab;
            return (
              <div
                key={child.tab}
                className={cn(
                  "group flex items-center rounded-md pr-0.5 transition-colors",
                  "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                  childActive && "bg-accent/60 font-medium text-foreground"
                )}
              >
                <Link href={href} className="min-w-0 flex-1 truncate px-2 py-1.5 text-xs">
                  {child.title}
                </Link>
                <PinToggle targetKey={href} title={child.title} size="sm" />
              </div>
            );
          })}
        </div>
      </HoverCardContent>
    </HoverCard>
  );
}
