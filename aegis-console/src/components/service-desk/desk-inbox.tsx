"use client";

import { memo, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { motion } from "motion/react";
import { useHotkeys } from "react-hotkeys-hook";
import { AlarmClock, Check, ImageIcon, Inbox, ListFilter, Lock, Paperclip, Star, X } from "lucide-react";
import { ApiError } from "@/lib/api/client";
import { useTicketCategoriesQuery, type useTicketsInfiniteQuery } from "@/lib/ticket-hooks";
import type { TicketItem, TicketPriority, TicketStats } from "@/lib/api/tickets";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { VirtualList, type VirtualListHandle } from "@/components/ui/virtual-list";
import { cn } from "@/lib/utils";
import {
  PRIORITY_LABEL,
  PersonAvatar,
  STATUS_DOT,
  STATUS_LABEL,
  formatDateTime,
  formatDue,
  formatRelativeTime,
  isAwaitingReply,
  isTerminalStatus
} from "@/components/tickets/ticket-shared";
import { AppMark, DeskAppPicker } from "./desk-app-rail";
import { useDesk } from "./desk-context";
import { BulkBar, RefreshButton, SearchField, SortMenu } from "./desk-inbox-head";
import { CategoryTag, flattenPreview } from "./desk-kit";
import { countDeskFilters, type DeskState, type DeskView, type SetDeskState } from "./desk-state";

// 收件箱：服务台的列表栏。
//
// 一行回答四件事：谁、哪个应用、说了什么、要不要我现在处理。
// 状态由上方页签表达，行内只在头像角上留一个色点；「等我回复」用行首色条与加粗表达，像邮箱里的未读；
// 看「全部应用」时每行带应用标记，自己创建的应用排在前面（ownedFirst），一眼分得清是哪个应用的。
// 头像位悬停变成勾选框，勾了几条之后顶部换成批量操作条。

const ROW_HEIGHT = 92;

type ListQuery = ReturnType<typeof useTicketsInfiniteQuery>;

export type ViewCounts = Partial<Record<DeskView, number>>;

type Props = {
  state: DeskState;
  setState: SetDeskState;
  counts: ViewCounts;
  query: ListQuery;
  items: TicketItem[];
  total: number;
  searchRef: RefObject<HTMLInputElement | null>;
  onOpen: (id: number) => void;
  /** 应用栏不在时（窄屏 / 收起），列表顶部给应用切换器 */
  showAppPicker: boolean;
  onSelectApp: (appid: number | null, own?: boolean) => void;
};

export function deskViewCounts(stats?: TicketStats, overdue?: number): ViewCounts {
  if (!stats) return {};
  return {
    active: stats.open + stats.processing + stats.pendingUser,
    open: stats.open,
    processing: stats.processing,
    pending_user: stats.pendingUser,
    overdue,
    resolved: stats.resolved,
    closed: stats.closed,
    all: stats.total
  };
}

const VIEWS: { value: DeskView; label: string }[] = [
  { value: "active", label: "待处理" },
  { value: "open", label: "待受理" },
  { value: "processing", label: "处理中" },
  { value: "pending_user", label: "待补充" },
  { value: "overdue", label: "已超时" },
  { value: "resolved", label: "已解决" },
  { value: "closed", label: "已关闭" },
  { value: "all", label: "全部" }
];

export function DeskInbox({ state, setState, counts, query, items, total, searchRef, onOpen, showAppPicker, onSelectApp }: Props) {
  const { noun } = useDesk();
  const filterCount = countDeskFilters(state);
  const filtered = filterCount > 0 || Boolean(state.q.trim());

  // 勾选：只保留当前列表里还在的那几条（换筛选后自动失效，不必另外清）
  const [picked, setPicked] = useState<Set<number>>(() => new Set());
  const selected = useMemo(() => items.filter((item) => picked.has(item.id)).map((item) => item.id), [items, picked]);
  const selecting = selected.length > 0;
  const toggle = (id: number) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const clear = () => setPicked(new Set());

  useHotkeys("x", () => state.id && toggle(state.id), { enabled: Boolean(state.id) }, [state.id]);
  useHotkeys("escape", clear, { enabled: selecting && !state.id, preventDefault: true });

  return (
    <div className="flex h-full min-h-0 flex-col">
      {showAppPicker ? (
        <div className="flex items-center gap-1 border-b px-2 py-1.5">
          <DeskAppPicker value={state.app} own={state.own} onSelect={onSelectApp} />
        </div>
      ) : null}

      <div className="flex h-12 shrink-0 items-center gap-1 px-3">
        {selecting ? (
          <BulkBar
            selected={selected}
            loaded={items.length}
            onSelectAll={() => setPicked(new Set(items.map((item) => item.id)))}
            onClear={clear}
          />
        ) : (
          <>
            <SearchField state={state} setState={setState} searchRef={searchRef} />
            <FilterMenu state={state} setState={setState} count={filterCount} />
            <SortMenu state={state} setState={setState} />
          </>
        )}
      </div>

      {/* 视图页签：下划线跟着选中项滑动 */}
      <div
        role="tablist"
        aria-label={`${noun}视图`}
        // 页签排不下时横向滚动；鼠标滚轮只有纵向，这里把它折算成横向
        onWheel={(event) => {
          if (event.deltaY) event.currentTarget.scrollLeft += event.deltaY;
        }}
        className="flex shrink-0 gap-4 overflow-x-auto border-b px-4 [scrollbar-width:none]"
      >
        {VIEWS.map((item) => {
          const active = state.view === item.value;
          const count = counts[item.value];
          return (
            <button
              key={item.value}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => void setState({ view: item.value })}
              className={cn(
                "relative flex shrink-0 items-center gap-1.5 pb-2.5 pt-1 text-sm outline-none transition-colors focus-visible:text-foreground",
                active ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
              )}
            >
              {item.label}
              {count !== undefined && count > 0 ? (
                <span
                  className={cn(
                    "rounded-full px-1.5 text-[11px] leading-4 tabular-nums",
                    item.value === "overdue"
                      ? "bg-destructive/10 text-destructive"
                      : active
                        ? "bg-foreground/10 text-foreground"
                        : "text-muted-foreground"
                  )}
                >
                  {count}
                </span>
              ) : null}
              {active ? (
                <motion.span
                  layoutId="desk-view-indicator"
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
            picked={picked}
            selecting={selecting}
            onToggle={toggle}
            onOpen={onOpen}
            hasMore={Boolean(query.hasNextPage)}
            loadingMore={query.isFetchingNextPage}
            onLoadMore={() => void query.fetchNextPage()}
            showStatus={state.view === "active" || state.view === "all" || state.view === "overdue"}
            showApp={state.app === null}
            empty={
              <div className="flex flex-col items-center gap-3 px-6 py-20 text-center">
                <span className="flex size-12 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
                  <Inbox className="size-5" />
                </span>
                <div className="space-y-1">
                  <p className="text-sm font-medium text-foreground">
                    {filtered ? `没有符合条件的${noun}` : state.view === "active" ? "都处理完了" : `暂无${noun}`}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {filtered ? "换个关键词，或去掉几个筛选条件" : state.view === "active" ? `新的${noun}进来会出现在这里` : "切到其他视图看看"}
                  </p>
                </div>
              </div>
            }
          />
        )}
        {/* 换筛选条件时保留上一屏数据，用顶部细条提示正在取新数据 */}
        {query.isPlaceholderData ? (
          <div className="pointer-events-none absolute inset-x-0 top-0 h-0.5 overflow-hidden bg-primary/15">
            <div className="desk-progress h-full w-1/3 rounded-full bg-primary" />
          </div>
        ) : null}
      </div>

      <div className="flex h-9 shrink-0 items-center justify-between border-t px-3 text-xs text-muted-foreground">
        <span className="tabular-nums">{query.isLoading ? "加载中" : `已加载 ${items.length} / 共 ${total} 条`}</span>
        <RefreshButton spinning={query.isRefetching && !query.isFetchingNextPage} onClick={() => void query.refetch()} />
      </div>
    </div>
  );
}

// ─────────────── 列表 ───────────────

function InboxList({
  items,
  selectedId,
  picked,
  selecting,
  onToggle,
  onOpen,
  hasMore,
  loadingMore,
  onLoadMore,
  showStatus,
  showApp,
  empty
}: {
  items: TicketItem[];
  selectedId: number | null;
  picked: Set<number>;
  selecting: boolean;
  onToggle: (id: number) => void;
  onOpen: (id: number) => void;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
  showStatus: boolean;
  showApp: boolean;
  empty: ReactNode;
}) {
  const { noun } = useDesk();
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
      ariaLabel={`${noun}列表`}
      className="h-full rounded-none"
      empty={empty}
      renderItem={(item) => (
        <InboxRow
          item={item}
          active={item.id === selectedId}
          checked={picked.has(item.id)}
          selecting={selecting}
          showStatus={showStatus}
          showApp={showApp}
          onOpen={onOpen}
          onToggle={onToggle}
        />
      )}
    />
  );
}

const PRIORITY_TEXT: Partial<Record<TicketPriority, string>> = {
  urgent: "text-destructive",
  high: "text-amber-600 dark:text-amber-400"
};

const InboxRow = memo(function InboxRow({
  item,
  active,
  checked,
  selecting,
  showStatus,
  showApp,
  onOpen,
  onToggle
}: {
  item: TicketItem;
  active: boolean;
  checked: boolean;
  selecting: boolean;
  showStatus: boolean;
  showApp: boolean;
  onOpen: (id: number) => void;
  onToggle: (id: number) => void;
}) {
  const awaiting = isAwaitingReply(item);
  const lastAt = item.lastMessageAt || item.updatedAt;
  const preview = flattenPreview(item.preview);
  const images = item.imageCount ?? 0;
  const files = item.attachmentCount ?? 0;
  const done = isTerminalStatus(item.status) || item.status === "resolved";
  // 时限：还没首响看首响，否则看解决
  const dueAt = !done ? (item.firstRespondedAt ? item.resolveDueAt : item.firstResponseDueAt) : undefined;
  const due = dueAt ? formatDue(dueAt) : null;
  const dueTone = due?.overdue ? "text-destructive" : item.slaState === "warning" ? "text-amber-600 dark:text-amber-400" : null;
  const priorityText = PRIORITY_TEXT[item.priority];

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={(event) => {
        if (event.shiftKey || event.metaKey || event.ctrlKey || selecting) onToggle(item.id);
        else onOpen(item.id);
      }}
      onKeyDown={(event) => {
        if (event.key === "Enter") onOpen(item.id);
        if (event.key === " ") {
          event.preventDefault();
          onToggle(item.id);
        }
      }}
      aria-current={active ? "true" : undefined}
      className={cn(
        "group/row relative flex h-full w-full cursor-pointer gap-3 border-b border-border/50 px-4 py-3 text-left outline-none transition-colors",
        active ? "bg-accent" : checked ? "bg-primary/5" : "hover:bg-muted/50 focus-visible:bg-muted/50"
      )}
    >
      {/* 等我回复：行首色条，对应邮箱里的未读 */}
      {awaiting ? <span aria-hidden className="absolute inset-y-3 left-0 w-[3px] rounded-r-full bg-primary" /> : null}

      <span className="relative mt-0.5 size-9 shrink-0">
        <span className={cn("absolute inset-0 transition-opacity", checked || selecting ? "opacity-0" : "group-hover/row:opacity-0")}>
          <PersonAvatar name={item.requesterName} src={item.requesterAvatar} className="size-9" />
          {showStatus ? (
            <span
              title={STATUS_LABEL[item.status]}
              className={cn(
                "absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full ring-2 ring-card",
                STATUS_DOT[item.status],
                active && "ring-accent"
              )}
            />
          ) : null}
        </span>
        <span
          className={cn(
            "absolute inset-0 flex items-center justify-center rounded-full transition-opacity",
            checked || selecting ? "opacity-100" : "opacity-0 group-hover/row:opacity-100"
          )}
          onClick={(event) => {
            event.stopPropagation();
            onToggle(item.id);
          }}
        >
          <Checkbox checked={checked} tabIndex={-1} aria-label={checked ? "取消勾选" : "勾选"} className="size-[18px] rounded-[5px]" />
        </span>
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

        <span className={cn("mt-0.5 flex items-center gap-1 text-sm", awaiting ? "text-foreground" : "text-foreground/80")}>
          {item.locked ? <Lock className="size-3 shrink-0 text-muted-foreground" aria-label="已锁定" /> : null}
          <span className="truncate">{item.title}</span>
        </span>

        <span className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          {showApp ? (
            <span className="flex min-w-0 max-w-[45%] shrink-0 items-center gap-1" title={item.appName || "平台内部"}>
              <AppMark appid={item.appid} name={item.appName || ""} className="size-4 rounded text-[9px]" />
              <span className="truncate">{item.appName || "平台内部"}</span>
            </span>
          ) : null}
          <span className="min-w-0 flex-1 truncate">{preview || " "}</span>
          {priorityText ? <span className={cn("shrink-0 font-medium", priorityText)}>{PRIORITY_LABEL[item.priority]}</span> : null}
          {due && dueTone ? (
            <span className={cn("inline-flex shrink-0 items-center gap-0.5", dueTone)} title={due.text}>
              <AlarmClock className="size-3" />
              {due.overdue ? "超时" : "将到期"}
            </span>
          ) : null}
          {images > 0 ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 tabular-nums" title={`${images} 张图片`}>
              <ImageIcon className="size-3" />
              {images}
            </span>
          ) : null}
          {files > 0 ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 tabular-nums" title={`${files} 个附件`}>
              <Paperclip className="size-3" />
              {files}
            </span>
          ) : null}
          {item.rating ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 tabular-nums text-amber-500" title={`满意度 ${item.rating} 星`}>
              <Star className="size-3 fill-current" />
              {item.rating}
            </span>
          ) : null}
          {item.assigneeAdminId ? (
            <span className="shrink-0" title={`受理人：${item.assigneeName || "未知"}`}>
              <PersonAvatar name={item.assigneeName} src={item.assigneeAvatar} size="sm" className="size-4 text-[8px]" />
            </span>
          ) : !done ? (
            <span className="shrink-0 rounded border border-dashed px-1 text-[10px] leading-4" title="还没有受理人">
              未指派
            </span>
          ) : null}
        </span>
      </span>
    </div>
  );
});

function InboxSkeleton() {
  return (
    <div aria-busy>
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="flex gap-3 border-b border-border/50 px-4 py-3" style={{ height: ROW_HEIGHT }}>
          <Skeleton className="size-9 shrink-0 rounded-full" />
          <div className="flex-1 space-y-2 pt-0.5">
            <div className="flex justify-between">
              <Skeleton className="h-3.5 w-24" />
              <Skeleton className="h-3 w-12" />
            </div>
            <Skeleton className="h-3.5 w-3/4" />
            <div className="flex gap-2">
              <Skeleton className="h-3 w-16" />
              <Skeleton className="h-3 flex-1" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// ─────────────── 筛选 ───────────────

const PRIORITY_ORDER: TicketPriority[] = ["urgent", "high", "normal", "low"];

function useFilterOptions(appId: number | null) {
  const { kind } = useDesk();
  // 指定应用时后端返回「该应用 + 平台级」分类；未指定时只有平台级
  const categoriesQuery = useTicketCategoriesQuery(appId ?? 0);
  const categories = useMemo(
    () => (categoriesQuery.data ?? []).filter((item) => (item.kind ?? "ticket") === kind),
    [categoriesQuery.data, kind]
  );
  return { categories };
}

function Mark({ on }: { on: boolean }) {
  return <Check className={cn("size-3.5", on ? "opacity-100" : "opacity-0")} />;
}

/** 筛选菜单：一个可检索的清单，点一项即生效，再点一次取消 */
export function FilterMenu({ state, setState, count }: { state: DeskState; setState: SetDeskState; count: number }) {
  const { categories } = useFilterOptions(state.app);

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
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

/** 生效中的筛选条件：看得见，点一下去掉 */
function ActiveFilters({ state, setState }: { state: DeskState; setState: SetDeskState }) {
  const { categories } = useFilterOptions(state.app);

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
