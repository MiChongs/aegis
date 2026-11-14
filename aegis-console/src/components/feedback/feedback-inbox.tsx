"use client";

import { memo, useEffect, useMemo, useRef, type RefObject } from "react";
import { motion } from "motion/react";
import { AlarmClock, ArrowDownUp, Check, ImageIcon, Inbox, ListFilter, RefreshCw, Search, Star, X } from "lucide-react";
import { ApiError } from "@/lib/api/client";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useTicketCategoriesQuery, type useTicketsInfiniteQuery } from "@/lib/ticket-hooks";
import type { TicketItem, TicketPriority, TicketStats } from "@/lib/api/tickets";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator
} from "@/components/ui/command";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { VirtualList, type VirtualListHandle } from "@/components/ui/virtual-list";
import { cn } from "@/lib/utils";
import {
  Kbd,
  PRIORITY_LABEL,
  PersonAvatar,
  STATUS_DOT,
  STATUS_LABEL,
  formatDateTime,
  formatRelativeTime,
  isAwaitingReply
} from "@/components/tickets/ticket-shared";
import { CategoryTag, flattenPreview } from "./feedback-kit";
import {
  FEEDBACK_SORTS,
  FEEDBACK_SORT_LABEL,
  countFeedbackFilters,
  type FeedbackSort,
  type FeedbackState,
  type FeedbackView,
  type SetFeedbackState
} from "./feedback-state";

// 收件箱：反馈台左栏。
//
// 一行只回答三件事：谁、说了什么、要不要我现在处理。
// 状态由上方页签表达，行内只在头像角上留一个色点；分类是色点加名字，不用实心徽标；
// 「等我回复」用加粗的名字和行首的圆点表达，像邮箱里的未读。

const ROW_HEIGHT = 86;

type ListQuery = ReturnType<typeof useTicketsInfiniteQuery>;

type Props = {
  state: FeedbackState;
  setState: SetFeedbackState;
  stats?: TicketStats;
  query: ListQuery;
  items: TicketItem[];
  total: number;
  searchRef: RefObject<HTMLInputElement | null>;
  onOpen: (id: number) => void;
};

export function FeedbackInbox({ state, setState, stats, query, items, total, searchRef, onOpen }: Props) {
  const filterCount = countFeedbackFilters(state);
  const filtered = filterCount > 0 || Boolean(state.q.trim());

  const views: { value: FeedbackView; label: string; count?: number }[] = [
    { value: "active", label: "待处理", count: stats ? stats.open + stats.processing + stats.pendingUser : undefined },
    { value: "open", label: "待受理", count: stats?.open },
    { value: "processing", label: "处理中", count: stats?.processing },
    { value: "pending_user", label: "待补充", count: stats?.pendingUser },
    { value: "resolved", label: "已解决", count: stats?.resolved },
    { value: "all", label: "全部", count: stats?.total }
  ];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-1 px-3 pt-3">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            ref={searchRef}
            value={state.q}
            onChange={(event) => void setState({ q: event.target.value })}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                void setState({ q: "" });
                event.currentTarget.blur();
              }
            }}
            placeholder="搜索标题、反馈人、内容"
            aria-label="搜索反馈"
            className="h-8 w-full rounded-lg bg-muted/60 pl-8 pr-8 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:bg-background focus-visible:ring-2 focus-visible:ring-ring/40"
          />
          {state.q ? (
            <button
              type="button"
              onClick={() => void setState({ q: "" })}
              aria-label="清空搜索"
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          ) : (
            <Kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2">/</Kbd>
          )}
        </div>

        <FilterMenu state={state} setState={setState} count={filterCount} />

        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button size="icon-sm" variant="ghost" aria-label="排序">
                  <ArrowDownUp />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>排序：{FEEDBACK_SORT_LABEL[state.sort]}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-36">
            <DropdownMenuRadioGroup value={state.sort} onValueChange={(value) => void setState({ sort: value as FeedbackSort })}>
              {FEEDBACK_SORTS.map((value) => (
                <DropdownMenuRadioItem key={value} value={value}>
                  {FEEDBACK_SORT_LABEL[value]}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* 视图页签：下划线跟着选中项滑动 */}
      <div
        role="tablist"
        aria-label="反馈视图"
        // 页签排不下时横向滚动；鼠标滚轮只有纵向，这里把它折算成横向
        onWheel={(event) => {
          if (event.deltaY) event.currentTarget.scrollLeft += event.deltaY;
        }}
        className="flex gap-4 overflow-x-auto border-b px-4 pt-2 [scrollbar-width:none]"
      >
        {views.map((item) => {
          const active = state.view === item.value;
          return (
            <button
              key={item.value}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => void setState({ view: item.value })}
              className={cn(
                "relative flex shrink-0 items-center gap-1.5 pb-2.5 pt-1.5 text-sm outline-none transition-colors focus-visible:text-foreground",
                active ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
              )}
            >
              {item.label}
              {item.count !== undefined ? <span className="text-xs tabular-nums text-muted-foreground">{item.count}</span> : null}
              {active ? (
                <motion.span
                  layoutId="feedback-view-indicator"
                  className="absolute inset-x-0 -bottom-px h-0.5 rounded-full bg-foreground"
                  transition={{ type: "spring", stiffness: 500, damping: 40 }}
                />
              ) : null}
            </button>
          );
        })}
      </div>

      {filterCount > 0 ? <ActiveFilters state={state} setState={setState} /> : null}

      <div className="relative min-h-0 flex-1">
        {query.isLoading ? (
          <InboxSkeleton />
        ) : query.isError ? (
          <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
            <p className="text-sm text-foreground">{query.error instanceof ApiError ? query.error.message : "列表加载失败"}</p>
            <Button size="sm" variant="outline" onClick={() => void query.refetch()}>
              重试
            </Button>
          </div>
        ) : (
          <InboxList
            items={items}
            selectedId={state.id}
            onOpen={onOpen}
            hasMore={Boolean(query.hasNextPage)}
            loadingMore={query.isFetchingNextPage}
            onLoadMore={() => void query.fetchNextPage()}
            showStatus={state.view === "active" || state.view === "all"}
            empty={
              <div className="flex flex-col items-center gap-3 px-6 py-20 text-center">
                <span className="flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">
                  <Inbox className="size-5" />
                </span>
                <p className="text-sm text-muted-foreground">{filtered ? "没有符合条件的反馈" : "暂无反馈"}</p>
              </div>
            }
          />
        )}
        {/* 换筛选条件时保留上一屏数据，用顶部细条提示正在取新数据 */}
        {query.isPlaceholderData ? (
          <div className="pointer-events-none absolute inset-x-0 top-0 h-0.5 animate-pulse bg-primary/60" />
        ) : null}
      </div>

      <div className="flex h-9 items-center justify-between border-t px-3 text-xs text-muted-foreground">
        <span className="tabular-nums">{query.isLoading ? "加载中" : `${items.length} / ${total}`}</span>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="icon-xs" variant="ghost" onClick={() => void query.refetch()} aria-label="刷新">
              <RefreshCw className={cn(query.isRefetching && !query.isFetchingNextPage && "animate-spin")} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>刷新</TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}

// ─────────────── 列表 ───────────────

function InboxList({
  items,
  selectedId,
  onOpen,
  hasMore,
  loadingMore,
  onLoadMore,
  showStatus,
  empty
}: {
  items: TicketItem[];
  selectedId: number | null;
  onOpen: (id: number) => void;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
  showStatus: boolean;
  empty: React.ReactNode;
}) {
  const listRef = useRef<VirtualListHandle>(null);

  // 键盘切换时让选中行留在可视区内
  const selectedIndex = selectedId ? items.findIndex((item) => item.id === selectedId) : -1;
  useEffect(() => {
    if (selectedIndex >= 0) listRef.current?.scrollToIndex(selectedIndex, { align: "auto" });
  }, [selectedIndex]);

  return (
    <VirtualList
      ref={listRef}
      items={items}
      estimateSize={ROW_HEIGHT}
      getItemKey={(index) => items[index]?.id ?? index}
      onEndReached={onLoadMore}
      hasMore={hasMore}
      isLoading={loadingMore}
      ariaLabel="反馈列表"
      className="h-full rounded-none"
      empty={empty}
      renderItem={(item) => (
        <InboxRow item={item} active={item.id === selectedId} showStatus={showStatus} onOpen={onOpen} />
      )}
    />
  );
}

const InboxRow = memo(function InboxRow({
  item,
  active,
  showStatus,
  onOpen
}: {
  item: TicketItem;
  active: boolean;
  showStatus: boolean;
  onOpen: (id: number) => void;
}) {
  const awaiting = isAwaitingReply(item);
  const lastAt = item.lastMessageAt || item.updatedAt;
  const preview = flattenPreview(item.preview);
  const images = item.imageCount ?? 0;
  const overdue = item.slaState === "breached" && awaiting;

  return (
    <button
      type="button"
      onClick={() => onOpen(item.id)}
      aria-current={active ? "true" : undefined}
      className={cn(
        "group/row relative flex h-full w-full gap-3 px-4 py-3 text-left outline-none transition-colors",
        active ? "bg-accent" : "hover:bg-muted/50 focus-visible:bg-muted/50"
      )}
    >
      {/* 等我回复：行首圆点，对应邮箱里的未读 */}
      {awaiting ? <span aria-hidden className="absolute left-1.5 top-[26px] size-1.5 rounded-full bg-primary" /> : null}

      <span className="relative mt-0.5 shrink-0">
        <PersonAvatar name={item.requesterName} src={item.requesterAvatar} className="size-9" />
        {showStatus ? (
          <span
            title={STATUS_LABEL[item.status]}
            className={cn("absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full ring-2 ring-card", STATUS_DOT[item.status], active && "ring-accent")}
          />
        ) : null}
      </span>

      <span className="min-w-0 flex-1">
        <span className="flex items-baseline gap-2">
          <span className={cn("truncate text-sm", awaiting ? "font-semibold text-foreground" : "font-medium text-foreground/80")}>
            {item.requesterName || "匿名用户"}
          </span>
          <CategoryTag name={item.categoryName} categoryKey={item.categoryKey} />
          <span className="ml-auto shrink-0 text-xs tabular-nums text-muted-foreground" title={formatDateTime(lastAt)}>
            {formatRelativeTime(lastAt)}
          </span>
        </span>

        <span className={cn("mt-0.5 block truncate text-sm", awaiting ? "text-foreground" : "text-foreground/80")}>{item.title}</span>

        <span className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
          <span className="min-w-0 flex-1 truncate">{preview || item.appName || " "}</span>
          {item.priority === "urgent" || item.priority === "high" ? (
            <span className={cn("shrink-0 font-medium", item.priority === "urgent" ? "text-destructive" : "text-amber-600 dark:text-amber-400")}>
              {PRIORITY_LABEL[item.priority]}
            </span>
          ) : null}
          {overdue ? (
            <span className="inline-flex shrink-0 items-center text-destructive" title="已超过响应时限">
              <AlarmClock className="size-3" />
            </span>
          ) : null}
          {images > 0 ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 tabular-nums" title={`${images} 张图片`}>
              <ImageIcon className="size-3" />
              {images}
            </span>
          ) : null}
          {item.rating ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 tabular-nums text-amber-500" title={`满意度 ${item.rating} 星`}>
              <Star className="size-3 fill-current" />
              {item.rating}
            </span>
          ) : null}
        </span>
      </span>
    </button>
  );
});

function InboxSkeleton() {
  return (
    <div aria-busy>
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="flex gap-3 px-4 py-3" style={{ height: ROW_HEIGHT }}>
          <Skeleton className="size-9 shrink-0 rounded-full" />
          <div className="flex-1 space-y-2 pt-0.5">
            <div className="flex justify-between">
              <Skeleton className="h-3.5 w-24" />
              <Skeleton className="h-3 w-12" />
            </div>
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  );
}

// ─────────────── 筛选 ───────────────

const PRIORITY_ORDER: TicketPriority[] = ["urgent", "high", "normal", "low"];

function useFilterOptions(appId: number | null) {
  const appsQuery = useAdminAppsQuery();
  // 指定应用时后端返回「该应用 + 平台级」分类；未指定时只有平台级
  const categoriesQuery = useTicketCategoriesQuery(appId ?? 0);
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const categories = useMemo(
    () => (categoriesQuery.data ?? []).filter((item) => item.kind === "feedback"),
    [categoriesQuery.data]
  );
  return { apps, categories };
}

function Mark({ on }: { on: boolean }) {
  return <Check className={cn("size-3.5", on ? "opacity-100" : "opacity-0")} />;
}

/** 筛选菜单：一个可检索的清单，点一项即生效，再点一次取消 */
function FilterMenu({ state, setState, count }: { state: FeedbackState; setState: SetFeedbackState; count: number }) {
  const { apps, categories } = useFilterOptions(state.app);

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button size={count ? "sm" : "icon-sm"} variant={count ? "secondary" : "ghost"} aria-label="筛选" className={cn(count > 0 && "px-2")}>
              <ListFilter />
              {count ? <span className="tabular-nums">{count}</span> : null}
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>筛选</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" className="w-60 p-0">
        <Command>
          <CommandInput placeholder="筛选条件" />
          <CommandList className="max-h-80">
            <CommandEmpty>没有匹配项</CommandEmpty>
            <CommandGroup heading="受理">
              <CommandItem onSelect={() => void setState({ assignee: state.assignee === "me" ? null : "me" })}>
                <Mark on={state.assignee === "me"} />
                我受理的
              </CommandItem>
              <CommandItem onSelect={() => void setState({ assignee: state.assignee === "none" ? null : "none" })}>
                <Mark on={state.assignee === "none"} />
                未指派
              </CommandItem>
            </CommandGroup>
            <CommandSeparator />
            <CommandGroup heading="优先级">
              {PRIORITY_ORDER.map((priority) => (
                <CommandItem
                  key={priority}
                  value={`优先级 ${PRIORITY_LABEL[priority]}`}
                  onSelect={() => void setState({ priority: state.priority === priority ? null : priority })}
                >
                  <Mark on={state.priority === priority} />
                  {PRIORITY_LABEL[priority]}
                </CommandItem>
              ))}
            </CommandGroup>
            {categories.length > 0 ? (
              <>
                <CommandSeparator />
                <CommandGroup heading="分类">
                  {categories.map((category) => (
                    <CommandItem
                      key={category.id}
                      value={`分类 ${category.name}`}
                      onSelect={() => void setState({ category: state.category === category.id ? null : category.id })}
                    >
                      <Mark on={state.category === category.id} />
                      <CategoryTag name={category.name} categoryKey={category.key} className="text-sm text-foreground" />
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            ) : null}
            {apps.length > 1 ? (
              <>
                <CommandSeparator />
                <CommandGroup heading="应用">
                  {apps.map((app) => (
                    <CommandItem
                      key={app.id}
                      value={`应用 ${app.name}`}
                      // 换应用后原分类可能不在新清单里，一并复位
                      onSelect={() => void setState({ app: state.app === app.id ? null : app.id, category: null })}
                    >
                      <Mark on={state.app === app.id} />
                      <span className="truncate">{app.name}</span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

/** 生效中的筛选条件：看得见，点一下去掉 */
function ActiveFilters({ state, setState }: { state: FeedbackState; setState: SetFeedbackState }) {
  const { apps, categories } = useFilterOptions(state.app);

  const chips: { key: string; label: string; clear: () => void }[] = [];
  if (state.assignee) {
    chips.push({ key: "assignee", label: state.assignee === "me" ? "我受理的" : "未指派", clear: () => void setState({ assignee: null }) });
  }
  if (state.priority) {
    chips.push({ key: "priority", label: `优先级 ${PRIORITY_LABEL[state.priority]}`, clear: () => void setState({ priority: null }) });
  }
  if (state.category !== null) {
    chips.push({
      key: "category",
      label: categories.find((item) => item.id === state.category)?.name ?? "分类",
      clear: () => void setState({ category: null })
    });
  }
  if (state.app !== null) {
    chips.push({
      key: "app",
      label: apps.find((item) => item.id === state.app)?.name ?? `应用 #${state.app}`,
      clear: () => void setState({ app: null, category: null })
    });
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5 border-b px-3 py-2">
      {chips.map((chip) => (
        <button
          key={chip.key}
          type="button"
          onClick={chip.clear}
          aria-label={`去掉筛选 ${chip.label}`}
          className="inline-flex h-6 items-center gap-1 rounded-full bg-muted pl-2.5 pr-1.5 text-xs text-foreground outline-none transition-colors hover:bg-muted/70 focus-visible:ring-2 focus-visible:ring-ring/40"
        >
          {chip.label}
          <X className="size-3 text-muted-foreground" />
        </button>
      ))}
    </div>
  );
}
