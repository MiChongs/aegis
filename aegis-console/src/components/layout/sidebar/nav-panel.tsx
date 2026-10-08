"use client";

import Link from "next/link";
import { LayoutGroup } from "motion/react";
import { PanelLeftClose, Search, Star } from "lucide-react";
import { PINNED_SECTION_KEY } from "@/components/layout/sidebar/nav-rail";
import { PinnedSection } from "@/components/layout/sidebar/sidebar-nav";
import {
  ActiveChildBar,
  ActivePill,
  Kbd,
  PinToggle,
  useMetaKeyLabel
} from "@/components/layout/sidebar/sidebar-shared";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  activeChildTab,
  childHref,
  isItemActive,
  type NavigationGroup,
  type NavigationItem
} from "@/lib/navigation";
import { useSidebarStore } from "@/lib/sidebar-store";
import { cn } from "@/lib/utils";

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="px-3 pt-1 pb-1.5 text-[11px] font-medium tracking-wider text-muted-foreground uppercase">
      {children}
    </p>
  );
}

function PanelItem({
  item,
  active,
  onNavigate
}: {
  item: NavigationItem;
  active: boolean;
  onNavigate?: () => void;
}) {
  const Icon = item.icon;
  return (
    <div
      className={cn(
        "group relative flex items-center rounded-lg pr-1 transition-colors",
        active ? "text-foreground" : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground"
      )}
    >
      {active ? <ActivePill layoutId="panel-pill" /> : null}
      <Link
        href={item.href}
        onClick={onNavigate}
        title={item.summary}
        className="relative z-10 flex min-w-0 flex-1 items-center gap-2.5 px-3 py-[7px] text-sm"
      >
        <Icon className="size-4 shrink-0" />
        <span className={cn("truncate", active && "font-medium")}>{item.title}</span>
      </Link>
      <PinToggle targetKey={item.href} title={item.title} />
      {item.children?.length ? (
        <span
          className={cn(
            "relative z-10 mr-1 rounded-md border px-1.5 text-[10px] leading-4 tabular-nums transition-opacity",
            active ? "border-sidebar-border text-muted-foreground" : "border-transparent text-muted-foreground/60",
            "group-hover:opacity-0"
          )}
          aria-label={`${item.children.length} 个子项`}
        >
          {item.children.length}
        </span>
      ) : null}
    </div>
  );
}

/**
 * 次级面板：显示轨道上选中分组的页面。
 *
 * 当前页面若有页内子项（`?tab=`），子项作为面板的第二个区块平铺出来，
 * 而不是在页面行下面再缩进一层 —— 两个区块各有标题，层级一眼可辨，
 * 也不会出现子项一展开就把同组其它页面挤出视野的情况。
 */
export function NavPanel({
  group,
  selectedKey,
  pathname,
  activeTab,
  onOpenPalette,
  onNavigate
}: {
  group: NavigationGroup | null;
  selectedKey: string | null;
  pathname: string;
  activeTab: string | null;
  onOpenPalette: () => void;
  onNavigate?: () => void;
}) {
  const toggleSidebar = useSidebarStore((s) => s.toggle);
  const metaKey = useMetaKeyLabel();
  const pinnedMode = selectedKey === PINNED_SECTION_KEY;

  const activeItem = group?.items.find((item) => isItemActive(pathname, item)) ?? null;
  const children = activeItem?.children?.length ? activeItem.children : null;
  const currentTab = activeItem ? activeChildTab(pathname, activeItem, activeTab) : null;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 pr-2 pl-4">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold">{pinnedMode ? "收藏" : (group?.title ?? "导航")}</p>
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={toggleSidebar}
              aria-label="收起侧栏"
              className="flex size-7 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground"
            >
              <PanelLeftClose className="size-4" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="right" sideOffset={8}>
            <p>收起侧栏</p>
            <p className="text-[10px] text-background/70">{metaKey} B</p>
          </TooltipContent>
        </Tooltip>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="px-2 pb-3">
          {pinnedMode ? (
            <PinnedPanel pathname={pathname} activeTab={activeTab} onNavigate={onNavigate} />
          ) : group ? (
            <LayoutGroup id="panel-tree">
              <SectionLabel>{group.summary}</SectionLabel>
              <nav aria-label={group.title} className="flex flex-col gap-0.5">
                {group.items.map((item) => (
                  <PanelItem
                    key={item.href}
                    item={item}
                    active={activeItem?.href === item.href}
                    onNavigate={onNavigate}
                  />
                ))}
              </nav>

              {activeItem && children ? (
                <div className="mt-4 border-t border-sidebar-border pt-3">
                  <SectionLabel>{activeItem.title}</SectionLabel>
                  <div className="ml-3 flex flex-col gap-px border-l border-sidebar-border pl-2">
                    {children.map((child) => {
                      const href = childHref(activeItem, child);
                      const childActive = currentTab === child.tab;
                      return (
                        <div
                          key={child.tab}
                          className={cn(
                            "group relative flex items-center rounded-md pr-0.5 transition-colors",
                            childActive
                              ? "text-foreground"
                              : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground"
                          )}
                        >
                          {childActive ? <ActiveChildBar layoutId="panel-child-bar" /> : null}
                          <Link
                            href={href}
                            onClick={onNavigate}
                            className="flex min-w-0 flex-1 items-center px-2 py-1.5 text-[13px]"
                          >
                            <span className={cn("truncate", childActive && "font-medium")}>{child.title}</span>
                          </Link>
                          <PinToggle targetKey={href} title={child.title} size="sm" />
                        </div>
                      );
                    })}
                  </div>
                </div>
              ) : null}
            </LayoutGroup>
          ) : null}
        </div>
      </ScrollArea>

      <div className="shrink-0 border-t border-sidebar-border p-3">
        <button
          type="button"
          onClick={onOpenPalette}
          className="group w-full rounded-xl border border-sidebar-border bg-background/50 p-3 text-left transition-colors hover:border-ring/40 hover:bg-background"
        >
          <span className="flex items-center gap-2 text-xs font-medium">
            <Search className="size-3.5 text-muted-foreground" />
            快速跳转
            <Kbd className="ml-auto">{metaKey} K</Kbd>
          </span>
          <span className="mt-1.5 block text-[11px] leading-relaxed text-muted-foreground">
            输入页面名称或拼音首字母，直达任意页面与面板。
          </span>
        </button>
      </div>
    </div>
  );
}

function PinnedPanel({
  pathname,
  activeTab,
  onNavigate
}: {
  pathname: string;
  activeTab: string | null;
  onNavigate?: () => void;
}) {
  const pinned = useSidebarStore((s) => s.pinned);
  if (!pinned.length) {
    return (
      <div className="px-3 py-10 text-center">
        <Star className="mx-auto size-5 text-muted-foreground/60" />
        <p className="mt-2 text-xs text-muted-foreground">在任意页面行上点星标即可加入收藏。</p>
      </div>
    );
  }
  return <PinnedSection pathname={pathname} activeTab={activeTab} onNavigate={onNavigate} scope="panel" />;
}
