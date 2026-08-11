"use client";

import Link from "next/link";
import { useEffect, useMemo } from "react";
import { AnimatePresence, LayoutGroup, motion, Reorder, useDragControls, useReducedMotion } from "motion/react";
import { ChevronDown, ChevronRight, GripVertical, Star } from "lucide-react";
import {
  activeChildTab,
  childHref,
  isItemActive,
  type NavigationGroup,
  type NavigationItem,
  type NavigationTarget
} from "@/lib/navigation";
import { resolveTargets, useTargetIndex, useVisibleGroups } from "@/lib/navigation-hooks";
import { useSidebarStore } from "@/lib/sidebar-store";
import { cn } from "@/lib/utils";
import {
  ActiveChildBar,
  ActivePill,
  PinToggle,
  SIDEBAR_COLLAPSE_DURATION,
  SIDEBAR_EASE
} from "@/components/layout/sidebar/sidebar-shared";

export type NavTreeProps = {
  pathname: string;
  /** 当前 `?tab=`，用于高亮页内子项 */
  activeTab: string | null;
  onNavigate?: () => void;
  /**
   * 同一棵树可能同时存在两份（桌面侧边栏 + 移动端抽屉）。
   * `layoutId` 必须按实例隔离，否则两份会抢同一块滑动高亮。
   */
  scope?: string;
};

/* ------------------------------------------------------------------ */
/*  展开态：收藏区 + 分组 → 页面 → 页内子项                              */
/* ------------------------------------------------------------------ */

export function SidebarNavTree({ pathname, activeTab, onNavigate, scope = "desktop" }: NavTreeProps) {
  const groups = useVisibleGroups();
  const closedGroups = useSidebarStore((s) => s.closedGroups);
  const expandedItem = useSidebarStore((s) => s.expandedItem);
  const toggleGroup = useSidebarStore((s) => s.toggleGroup);
  const toggleItem = useSidebarStore((s) => s.toggleItem);
  const ensureGroupOpen = useSidebarStore((s) => s.ensureGroupOpen);
  const ensureItemExpanded = useSidebarStore((s) => s.ensureItemExpanded);
  const reduced = useReducedMotion();

  // 路由所在的分组 / 子树，进入时自动展开（此后手动折叠不会被再次撑开）
  const activeGroupKey = useMemo(
    () => groups.find((g) => g.items.some((item) => isItemActive(pathname, item)))?.key ?? null,
    [groups, pathname]
  );
  const activeParentHref = useMemo(() => {
    const hit = groups
      .flatMap((g) => g.items)
      .find((item) => item.children?.length && isItemActive(pathname, item));
    return hit?.href ?? null;
  }, [groups, pathname]);

  useEffect(() => {
    if (activeGroupKey) ensureGroupOpen(activeGroupKey);
    if (activeParentHref) ensureItemExpanded(activeParentHref);
  }, [activeGroupKey, activeParentHref, ensureGroupOpen, ensureItemExpanded]);

  return (
    <div className="flex flex-col gap-3">
      <PinnedSection pathname={pathname} activeTab={activeTab} onNavigate={onNavigate} scope={scope} />

      <LayoutGroup id={`${scope}-tree`}>
        <nav className="flex flex-col gap-3">
          {groups.map((group) => (
            <NavGroup
              key={group.key}
              group={group}
              pathname={pathname}
              activeTab={activeTab}
              open={!closedGroups.includes(group.key)}
              expandedItem={expandedItem}
              onToggleGroup={() => toggleGroup(group.key)}
              onToggleItem={toggleItem}
              onNavigate={onNavigate}
              reduced={!!reduced}
            />
          ))}
        </nav>
      </LayoutGroup>
    </div>
  );
}

/* ── 收藏区 ── */

/**
 * 收藏区。
 *
 * 存在的理由：侧边栏有 130+ 个跳转目标，但每个管理员日常只在其中 5～8 个之间来回。
 * 把这几个提到顶部，比记住它们各自埋在哪个分组里快得多。
 * 顺序可拖，因为"常用"本身就是有优先级的。
 */
function PinnedSection({ pathname, activeTab, onNavigate, scope = "desktop" }: NavTreeProps) {
  const pinned = useSidebarStore((s) => s.pinned);
  const setPinned = useSidebarStore((s) => s.setPinned);
  const index = useTargetIndex();
  const targets = useMemo(() => resolveTargets(pinned, index), [pinned, index]);

  if (!targets.length) return null;

  return (
    <div>
      <div className="mb-1 flex items-center gap-1.5 px-3 py-0.5 text-[11px] font-medium tracking-wide text-muted-foreground">
        <Star className="size-3 fill-amber-400 text-amber-500" />
        <span>收藏</span>
        <span className="ml-auto text-[10px] text-muted-foreground/60">拖动排序</span>
      </div>
      <LayoutGroup id={`${scope}-pins`}>
        <Reorder.Group
          as="div"
          axis="y"
          values={targets.map((t) => t.key)}
          // 收到的是"可见收藏"的新顺序：解析不出来的 key（页面下线 / 无权访问）
          // 借这次写回一并清掉，收藏区因此不会积累死链
          onReorder={setPinned}
          className="flex flex-col gap-0.5"
        >
          {targets.map((target) => (
            <PinnedRow
              key={target.key}
              target={target}
              pathname={pathname}
              activeTab={activeTab}
              onNavigate={onNavigate}
            />
          ))}
        </Reorder.Group>
      </LayoutGroup>
    </div>
  );
}

function PinnedRow({
  target,
  pathname,
  activeTab,
  onNavigate
}: {
  target: NavigationTarget;
  pathname: string;
  activeTab: string | null;
  onNavigate?: () => void;
}) {
  const controls = useDragControls();
  const Icon = target.icon;
  const active = isTargetActive(target, pathname, activeTab);

  return (
    <Reorder.Item
      as="div"
      value={target.key}
      dragListener={false}
      dragControls={controls}
      className={cn(
        "group relative flex items-center rounded-lg pr-1 transition-colors",
        "text-muted-foreground",
        active ? "text-accent-foreground" : "hover:bg-sidebar-accent/60 hover:text-foreground"
      )}
    >
      {active ? <ActivePill layoutId="pin-pill" /> : null}
      <button
        type="button"
        aria-label="拖动排序"
        onPointerDown={(event) => controls.start(event)}
        className="relative z-10 flex size-5 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground/40 opacity-0 transition-opacity group-hover:opacity-100 active:cursor-grabbing"
      >
        <GripVertical className="size-3.5" />
      </button>
      <Link
        href={target.key}
        onClick={onNavigate}
        title={target.isChild ? `${target.itemTitle} · ${target.title}` : target.title}
        className="relative z-10 flex min-w-0 flex-1 items-center gap-2 py-1.5 text-sm"
      >
        <Icon className={cn("size-3.5 shrink-0", active && "text-foreground")} />
        <span className={cn("truncate", active && "font-medium text-foreground")}>{target.title}</span>
        {target.isChild ? (
          <span className="shrink-0 truncate text-[10px] text-muted-foreground/60">{target.itemTitle}</span>
        ) : null}
      </Link>
      <PinToggle targetKey={target.key} title={target.title} size="sm" />
    </Reorder.Item>
  );
}

/**
 * 收藏 / 最近里的目标是否正是当前页。
 *
 * 与 `isItemActive` 的区别：这里的 key 可能带 `?tab=`，所以路径命中之后
 * 还要再比一次 tab —— 否则收藏了「用户 › 角色权限」，只要人在 `/users` 就会亮。
 */
export function isTargetActive(target: NavigationTarget, pathname: string, activeTab: string | null): boolean {
  const [href, query] = target.key.split("?");
  if (!(pathname === href || pathname.startsWith(`${href}/`))) return false;
  if (!query) return true;
  return `tab=${activeTab}` === query;
}

/* ── 分组 ── */

function NavGroup({
  group,
  pathname,
  activeTab,
  open,
  expandedItem,
  onToggleGroup,
  onToggleItem,
  onNavigate,
  reduced
}: {
  group: NavigationGroup;
  pathname: string;
  activeTab: string | null;
  open: boolean;
  expandedItem: string | null;
  onToggleGroup: () => void;
  onToggleItem: (href: string) => void;
  onNavigate?: () => void;
  reduced: boolean;
}) {
  const groupActive = group.items.some((item) => isItemActive(pathname, item));

  return (
    <div>
      <button
        type="button"
        onClick={onToggleGroup}
        aria-expanded={open}
        title={group.summary}
        className="group/label mb-1 flex w-full items-center gap-1.5 rounded-md px-3 py-0.5 text-left text-[11px] font-medium tracking-wide text-muted-foreground transition-colors hover:text-foreground"
      >
        <span className="truncate">{group.title}</span>
        {!open && groupActive ? <span className="size-1 shrink-0 rounded-full bg-foreground/60" /> : null}
        {!open ? (
          <span className="ml-auto text-[10px] tabular-nums text-muted-foreground/50">{group.items.length}</span>
        ) : null}
        <ChevronDown
          className={cn(
            "size-3 shrink-0 transition-[transform,opacity] duration-200",
            open ? "ml-auto opacity-0 group-hover/label:opacity-60" : "-rotate-90 opacity-60"
          )}
        />
      </button>

      <AnimatePresence initial={false}>
        {open ? (
          <motion.div
            key="items"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={reduced ? { duration: 0 } : { duration: SIDEBAR_COLLAPSE_DURATION, ease: SIDEBAR_EASE }}
            className="overflow-hidden"
          >
            <div className="flex flex-col gap-0.5">
              {group.items.map((item) => (
                <NavItemRow
                  key={item.href}
                  item={item}
                  pathname={pathname}
                  active={isItemActive(pathname, item)}
                  activeTab={activeTab}
                  expanded={expandedItem === item.href}
                  onToggle={() => onToggleItem(item.href)}
                  onNavigate={onNavigate}
                  reduced={reduced}
                />
              ))}
            </div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}

/* ── 页面（二级） ── */

function NavItemRow({
  item,
  pathname,
  active,
  activeTab,
  expanded,
  onToggle,
  onNavigate,
  reduced
}: {
  item: NavigationItem;
  pathname: string;
  active: boolean;
  activeTab: string | null;
  expanded: boolean;
  onToggle: () => void;
  onNavigate?: () => void;
  reduced: boolean;
}) {
  const Icon = item.icon;
  const children = item.children;
  const showChildren = !!children?.length && expanded;
  const currentTab = activeChildTab(pathname, item, activeTab);

  return (
    <div>
      <div
        className={cn(
          "group relative flex items-center rounded-lg pr-1 transition-colors",
          "text-muted-foreground",
          active ? "text-accent-foreground" : "hover:bg-sidebar-accent/60 hover:text-foreground"
        )}
      >
        {active ? <ActivePill layoutId="nav-pill" /> : null}
        <Link
          className="relative z-10 flex min-w-0 flex-1 items-center gap-3 px-3 py-2 text-sm"
          href={item.href}
          onClick={onNavigate}
          title={item.summary}
        >
          <Icon className={cn("size-4 shrink-0 transition-colors", active && "text-foreground")} />
          <span className={cn("truncate", active && "font-medium text-foreground")}>{item.title}</span>
        </Link>
        <PinToggle targetKey={item.href} title={item.title} />
        {children?.length ? (
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={showChildren}
            title={showChildren ? `收起${item.title}子项` : `展开${item.title}子项`}
            className="relative z-10 flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground/60 transition-colors hover:bg-background/70 hover:text-foreground"
          >
            <ChevronRight className={cn("size-3.5 transition-transform duration-200", showChildren && "rotate-90")} />
          </button>
        ) : null}
      </div>

      <AnimatePresence initial={false}>
        {showChildren && children ? (
          <motion.div
            key="children"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={reduced ? { duration: 0 } : { duration: SIDEBAR_COLLAPSE_DURATION, ease: SIDEBAR_EASE }}
            className="overflow-hidden"
          >
            <div className="mt-0.5 mb-1 ml-5 flex flex-col gap-px border-l border-sidebar-border pl-2">
              {children.map((child) => {
                const href = childHref(item, child);
                const childActive = active && currentTab === child.tab;
                return (
                  <div
                    key={child.tab}
                    className={cn(
                      "group relative flex items-center rounded-md pr-0.5 transition-colors",
                      "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-accent-foreground",
                      childActive && "text-foreground"
                    )}
                  >
                    {childActive ? <ActiveChildBar layoutId={`child-bar-${item.href}`} /> : null}
                    <Link
                      href={href}
                      onClick={onNavigate}
                      className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-xs"
                    >
                      <span
                        className={cn(
                          "size-1 shrink-0 rounded-full bg-current transition-opacity",
                          childActive ? "opacity-100" : "opacity-40"
                        )}
                      />
                      <span className={cn("truncate", childActive && "font-medium")}>{child.title}</span>
                    </Link>
                    <PinToggle targetKey={href} title={child.title} size="sm" />
                  </div>
                );
              })}
            </div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
