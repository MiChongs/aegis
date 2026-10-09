"use client";

import * as React from "react";
import {
  columnVisibilityFeature,
  flexRender,
  rowSelectionFeature,
  tableFeatures,
  useTable,
  type ColumnDef,
  type ColumnVisibilityState,
  type RowSelectionState
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  AtSign,
  Columns3,
  Copy,
  LayoutGrid,
  List,
  Crown,
  ExternalLink,
  MoreHorizontal,
  Phone,
  Rows2,
  Rows3,
  Rows4,
  SearchX
} from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { AdminAppUserItem } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { SORT_LABELS, type SortField, type UserQueryState } from "./shared";

/**
 * 用户台账表 —— 按「管理员一眼要什么」重排的信息架构，不是旧八列的换皮。
 *
 * 列的组织原则：一列回答一个问题。
 * - **用户**：这是谁 —— 头像带启用状态点，名字旁挂会员冠，账号与 ID 在第二行。
 * - **联系方式**：怎么找到 TA —— 邮箱、手机各占一行，带图标可扫读。
 * - **权益**：TA 有什么 —— 积分/经验一行并排，会员到期单独一行。
 * - **状态**：TA 能不能用 —— 药丸态 + 受限原因与解禁时间就地展示，不用进详情。
 * - **注册**：TA 从哪来 —— 相对时间 + IP/属地合并一列（来源信息本来就是一件事）。
 * - **操作**：就地动作 —— 查看详情与复制 ID/账号/邮箱，省去「进详情只为复制个 ID」。
 *
 * 交互上的三个决定：
 * 1. **排序收进工具栏**。积分/经验/会员合并成「权益」后表头排序天然歧义，
 *    改为工具栏里一个显式的字段 + 方向选择器（服务端排序，跨页有效）；
 *    仅在语义唯一的「用户」「注册」表头保留点击排序作为快捷方式。
 * 2. **工具栏长在表格里**。行数、选中数、行高、列显隐都是这张表的属性，
 *    浮在表格外面像是另一个组件的遥控器。
 * 3. **行选中态用 data-state=selected** 走 shadcn 约定，选中行整行着色。
 *
 * 继承自旧版且必须保留的三个正确决定：不注册客户端排序特性（数据是服务端
 * 分页的，本地排序是说谎）；getRowId 用真实用户 ID（下标会让选中态漂移）；
 * 行数多时虚拟滚动（单页可到 500 条）。
 */

const tableFeaturesSet = tableFeatures({ rowSelectionFeature, columnVisibilityFeature });

type UserColumnDef = ColumnDef<typeof tableFeaturesSet, AdminAppUserItem>;

/** 超过这个行数才启用虚拟滚动：小表格全量渲染更简单，也没有性能问题。 */
const VIRTUALIZE_THRESHOLD = 60;

export type Density = "compact" | "default" | "comfortable";

const DENSITY: Record<Density, { rowHeight: number; cellClass: string; icon: typeof Rows2 }> = {
  compact: { rowHeight: 48, cellClass: "py-1.5", icon: Rows4 },
  default: { rowHeight: 60, cellClass: "py-2.5", icon: Rows3 },
  comfortable: { rowHeight: 72, cellClass: "py-3.5", icon: Rows2 }
};

/** 可显隐的列 → 菜单文案。操作列不进这个表，因此永远显示。 */
const COLUMN_LABELS: Record<string, string> = {
  user: "用户",
  contact: "联系方式",
  benefits: "权益",
  status: "状态",
  register: "注册"
};

/** 语义唯一、保留表头点击排序的列。合并列（权益/联系方式）排序有歧义，走工具栏。 */
const HEADER_SORT: Partial<Record<string, SortField>> = {
  user: "account",
  register: "createdAt"
};

function initials(nickname?: string | null, account?: string | null) {
  return String(nickname || account || "U").trim().slice(0, 2).toUpperCase();
}

function fmtTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  // Go 零值时间序列化成 0001-01-01，直接格式化会在界面上显示"0001/01/01"
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "—";
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function fmtDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "—";
  return date.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

function relative(value?: string | null) {
  if (!value) return "—";
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return "—";
  const diff = Date.now() - time;
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)} 小时前`;
  if (diff < 86_400_000 * 30) return `${Math.round(diff / 86_400_000)} 天前`;
  return fmtTime(value).slice(0, 10);
}

/**
 * 会员状态。永久会员（vipLifetimeAt 非空）优先：它没有到期时间，
 * vipExpireAt 此时要么为空、要么只是另买的限时那条线的到期时间。
 */
function vipState(
  item: Pick<AdminAppUserItem, "vipExpireAt" | "vipLifetimeAt">
): "none" | "lifetime" | "active" | "expired" {
  if (item.vipLifetimeAt) return "lifetime";
  const expireAt = item.vipExpireAt;
  if (!expireAt) return "none";
  const time = new Date(expireAt).getTime();
  if (Number.isNaN(time) || new Date(expireAt).getUTCFullYear() <= 1) return "none";
  return time > Date.now() ? "active" : "expired";
}

/** 省市相同（直辖市）时只写一次，避免出现「北京 北京」 */
function regionLabel(province?: string | null, city?: string | null) {
  const p = (province || "").trim();
  const c = (city || "").trim();
  if (!p) return c;
  if (!c || c === p) return p;
  return `${p} ${c}`;
}

function UserAvatar({
  item,
  enabled,
  className
}: {
  item: AdminAppUserItem;
  enabled: boolean;
  className?: string;
}) {
  return (
    <div className="relative shrink-0">
      <Avatar className={cn("ring-1 ring-border", className)} preview={false}>
        <AvatarImage src={typeof item.avatar === "string" ? item.avatar : ""} alt="" />
        <AvatarFallback className="text-[11px] font-medium">{initials(item.nickname, item.account)}</AvatarFallback>
      </Avatar>
      {/* 状态点长在头像上：扫一列头像就能数出受限的人 */}
      <span
        aria-label={enabled ? "正常" : "受限"}
        className={cn(
          "absolute -right-px -bottom-px size-2.5 rounded-full ring-2 ring-card",
          enabled ? "bg-emerald-500" : "bg-red-500"
        )}
      />
    </div>
  );
}

function VipMark({ lifetime }: { lifetime?: boolean }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-amber-500/12 px-1.5 py-px text-[10px] font-medium text-amber-700 dark:text-amber-300">
      <Crown className="size-2.5" />
      {lifetime ? "永久会员" : "会员"}
    </span>
  );
}

function StatusCell({ item }: { item: AdminAppUserItem }) {
  if (item.enabled !== false) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-700 dark:text-emerald-400">
        <span className="size-1.5 rounded-full bg-emerald-500" />
        正常
      </span>
    );
  }
  const until = fmtTime(item.disabledEndTime).slice(5);
  return (
    <div className="min-w-0 space-y-0.5">
      <span className="inline-flex items-center gap-1.5 rounded-full bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-700 dark:text-red-400">
        <span className="size-1.5 rounded-full bg-red-500" />
        受限
      </span>
      <div className="max-w-[180px] truncate text-[11px] text-muted-foreground" title={item.disabledReason || undefined}>
        {until ? `至 ${until}` : "长期"}
        {item.disabledReason ? `，${item.disabledReason}` : ""}
      </div>
    </div>
  );
}

async function copyText(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(`已复制${label}`);
  } catch {
    toast.error("复制失败");
  }
}

export function AppUsersTable({
  data,
  loading,
  query,
  onQueryChange,
  selection,
  onSelectionChange,
  onRowClick,
  emptyText,
  footer
}: {
  data: AdminAppUserItem[];
  loading: boolean;
  query: UserQueryState;
  onQueryChange: (next: UserQueryState) => void;
  selection: RowSelectionState;
  onSelectionChange: (next: RowSelectionState) => void;
  onRowClick: (user: AdminAppUserItem) => void;
  emptyText: string;
  /** 渲染在卡片底部的内容（分页） */
  footer?: React.ReactNode;
}) {
  const [view, setView] = React.useState<"table" | "grid">("table");
  const [density, setDensity] = React.useState<Density>("default");
  const [visibility, setVisibility] = React.useState<ColumnVisibilityState>({});

  const columns = React.useMemo<UserColumnDef[]>(
    () => [
      {
        id: "user",
        header: "用户",
        cell: ({ row }) => {
          const item = row.original;
          const enabled = item.enabled !== false;
          const vip = vipState(item);
          return (
            <div className="flex min-w-0 items-center gap-3">
              <UserAvatar item={item} enabled={enabled} className="size-10" />
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-1.5">
                  <span className="truncate text-sm font-medium">
                    {item.nickname || item.account || `用户 ${item.id}`}
                  </span>
                  {vip === "active" || vip === "lifetime" ? <VipMark lifetime={vip === "lifetime"} /> : null}
                </div>
                <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
                  {item.account && item.nickname ? <span className="truncate">@{item.account}</span> : null}
                  <span className="shrink-0 rounded bg-muted px-1 font-mono text-[10px] leading-4">#{item.id}</span>
                </div>
              </div>
            </div>
          );
        }
      },
      {
        id: "contact",
        header: "联系方式",
        cell: ({ row }) => {
          const { email, phone } = row.original;
          if (!email && !phone) {
            return <span className="text-xs text-muted-foreground/60">未填写</span>;
          }
          return (
            <div className="min-w-0 space-y-0.5">
              {email ? (
                <div className="flex items-center gap-1.5 text-xs">
                  <AtSign className="size-3 shrink-0 text-muted-foreground" />
                  <span className="truncate">{email}</span>
                </div>
              ) : null}
              {phone ? (
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Phone className="size-3 shrink-0" />
                  <span className="truncate tabular-nums">{phone}</span>
                </div>
              ) : null}
            </div>
          );
        }
      },
      {
        id: "benefits",
        header: "权益",
        cell: ({ row }) => {
          const item = row.original;
          const vip = vipState(item);
          return (
            <div className="min-w-0 space-y-1">
              <div className="flex items-baseline gap-3 text-xs">
                <span>
                  <span className="text-muted-foreground">积分 </span>
                  <span className="font-medium tabular-nums">{(item.integral ?? 0).toLocaleString("zh-CN")}</span>
                </span>
                <span>
                  <span className="text-muted-foreground">经验 </span>
                  <span className="font-medium tabular-nums">{(item.experience ?? 0).toLocaleString("zh-CN")}</span>
                </span>
              </div>
              {vip === "lifetime" ? (
                <div className="text-[11px] text-amber-600 dark:text-amber-400">永久会员</div>
              ) : vip === "active" ? (
                <div className="text-[11px] text-amber-600 dark:text-amber-400">
                  会员至 {fmtDate(item.vipExpireAt)}
                </div>
              ) : vip === "expired" ? (
                <div className="text-[11px] text-muted-foreground/70">会员已于 {fmtDate(item.vipExpireAt)} 到期</div>
              ) : null}
            </div>
          );
        }
      },
      {
        id: "status",
        header: "状态",
        cell: ({ row }) => <StatusCell item={row.original} />
      },
      {
        id: "register",
        header: "注册",
        cell: ({ row }) => {
          const item = row.original;
          const location = regionLabel(item.registerProvince, item.registerCity);
          return (
            <div className="min-w-0 space-y-0.5">
              <div className="truncate text-xs" title={fmtTime(item.registerTime || item.createdAt)}>
                {relative(item.registerTime || item.createdAt)}
              </div>
              {item.registerIp || location ? (
                <div className="flex min-w-0 items-center gap-1.5 truncate text-[11px] text-muted-foreground">
                  {location ? <span className="shrink-0">{location}</span> : null}
                  {item.registerIp ? <span className="truncate font-mono text-muted-foreground/80">{item.registerIp}</span> : null}
                </div>
              ) : null}
            </div>
          );
        }
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => {
          const item = row.original;
          return (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7 text-muted-foreground data-[state=open]:bg-accent"
                  aria-label={`用户 ${item.account ?? item.id} 的操作`}
                >
                  <MoreHorizontal className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuItem className="text-xs" onSelect={() => onRowClick(item)}>
                  <ExternalLink className="size-3.5" />
                  查看详情
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  className="text-xs"
                  onSelect={() => void copyText(String(item.id), "用户 ID")}
                >
                  <Copy className="size-3.5" />
                  复制用户 ID
                </DropdownMenuItem>
                {item.account ? (
                  <DropdownMenuItem
                    className="text-xs"
                    onSelect={() => void copyText(item.account ?? "", "账号")}
                  >
                    <Copy className="size-3.5" />
                    复制账号
                  </DropdownMenuItem>
                ) : null}
                {item.email ? (
                  <DropdownMenuItem
                    className="text-xs"
                    onSelect={() => void copyText(item.email ?? "", "邮箱")}
                  >
                    <Copy className="size-3.5" />
                    复制邮箱
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          );
        }
      }
    ],
    [onRowClick]
  );

  const table = useTable({
    features: tableFeaturesSet,
    data,
    columns,
    // 行 id 必须是用户 ID：默认的行下标在翻页/重拉之后会把选中状态挪到别人身上。
    getRowId: (row) => String(row.id),
    state: { rowSelection: selection, columnVisibility: visibility },
    onRowSelectionChange: (updater) =>
      onSelectionChange(typeof updater === "function" ? updater(selection) : updater),
    onColumnVisibilityChange: setVisibility
  });

  const rows = table.getRowModel().rows;
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const virtualize = rows.length > VIRTUALIZE_THRESHOLD;
  const rowHeight = DENSITY[density].rowHeight;
  const visibleColumns = table.getVisibleFlatColumns();
  const colSpan = visibleColumns.length + 1;

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 12,
    enabled: virtualize
  });

  const virtualRows = virtualizer.getVirtualItems();
  const paddingTop = virtualize && virtualRows.length ? virtualRows[0].start : 0;
  const paddingBottom =
    virtualize && virtualRows.length
      ? virtualizer.getTotalSize() - virtualRows[virtualRows.length - 1].end
      : 0;
  const visibleRows = virtualize ? virtualRows.map((item) => rows[item.index]) : rows;

  const allSelected = rows.length > 0 && rows.every((row) => row.getIsSelected());
  const someSelected = rows.some((row) => row.getIsSelected());
  const selectedCount = rows.filter((row) => row.getIsSelected()).length;
  const sortDefault = query.sort === "createdAt" && query.order === "desc";

  function applySort(field: SortField, order?: "asc" | "desc") {
    onQueryChange({
      ...query,
      sort: field,
      order: order ?? (query.sort === field ? (query.order === "asc" ? "desc" : "asc") : "desc"),
      page: 1
    });
  }

  // TooltipProvider 包住整棵子树：行高按钮的 tooltip 在工具栏里，
  // 只包表格容器会让它落在 Provider 之外（Radix 要求必须有 Provider 祖先）。
  return (
    <TooltipProvider delayDuration={200}>
      <div className="overflow-hidden rounded-2xl border bg-card text-card-foreground">
        <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <div className="inline-flex rounded-lg bg-muted p-0.5" role="tablist" aria-label="视图">
              {(
                [
                  { key: "table", label: "列表", icon: List },
                  { key: "grid", label: "卡片", icon: LayoutGrid }
                ] as const
              ).map((option) => (
                <button
                  key={option.key}
                  type="button"
                  role="tab"
                  aria-selected={view === option.key}
                  onClick={() => setView(option.key)}
                  className={cn(
                    "inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs transition-colors",
                    view === option.key
                      ? "bg-background font-medium text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <option.icon className="size-3.5" />
                  {option.label}
                </button>
              ))}
            </div>
            {loading && !rows.length ? (
              <span>正在加载</span>
            ) : (
              <span className="tabular-nums">本页 {rows.length} 人</span>
            )}
            {selectedCount > 0 ? (
              <>
                <span className="h-3 w-px bg-border" aria-hidden />
                <span className="font-medium text-foreground tabular-nums">
                  已选 {selectedCount}
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-1.5 text-[11px]"
                  onClick={() => onSelectionChange({})}
                >
                  取消选择
                </Button>
              </>
            ) : null}
          </div>

          <div className="ml-auto flex items-center gap-1.5">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="sm"
                  variant="outline"
                  data-active={!sortDefault}
                  className="h-8 gap-1.5 text-xs data-[active=true]:border-foreground/40"
                >
                  {query.order === "asc" ? (
                    <ArrowUp className="size-3.5" />
                  ) : (
                    <ArrowDown className="size-3.5" />
                  )}
                  {SORT_LABELS[query.sort]}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuLabel className="text-xs">排序字段</DropdownMenuLabel>
                <DropdownMenuRadioGroup
                  value={query.sort}
                  onValueChange={(value) => applySort(value as SortField, query.order)}
                >
                  {(Object.keys(SORT_LABELS) as SortField[]).map((field) => (
                    <DropdownMenuRadioItem key={field} value={field} className="text-xs">
                      {SORT_LABELS[field]}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
                <DropdownMenuSeparator />
                <DropdownMenuRadioGroup
                  value={query.order}
                  onValueChange={(value) => applySort(query.sort, value as "asc" | "desc")}
                >
                  <DropdownMenuRadioItem value="desc" className="text-xs">
                    降序
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="asc" className="text-xs">
                    升序
                  </DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
                {!sortDefault ? (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      className="text-xs text-muted-foreground"
                      onSelect={() => applySort("createdAt", "desc")}
                    >
                      <ArrowUpDown className="size-3.5" />
                      恢复默认
                    </DropdownMenuItem>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>

            {view === "table" ? <DensityToggle value={density} onChange={setDensity} /> : null}

            <DropdownMenu>
              <DropdownMenuTrigger asChild disabled={view !== "table"}>
                <Button
                  size="icon"
                  variant="outline"
                  className={cn("size-8", view !== "table" && "hidden")}
                  aria-label="选择显示的列"
                >
                  <Columns3 className="size-3.5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                <DropdownMenuLabel className="text-xs">显示的列</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {table
                  .getAllColumns()
                  .filter((column) => COLUMN_LABELS[column.id])
                  .map((column) => (
                    <DropdownMenuCheckboxItem
                      key={column.id}
                      className="text-xs"
                      checked={column.getIsVisible()}
                      onCheckedChange={(checked) => column.toggleVisibility(Boolean(checked))}
                    >
                      {COLUMN_LABELS[column.id]}
                    </DropdownMenuCheckboxItem>
                  ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {view === "grid" ? (
          <UserGrid
            rows={rows}
            loading={loading}
            emptyText={emptyText}
            onOpen={onRowClick}
          />
        ) : (
        <div
          ref={scrollRef}
          className={cn("overflow-auto", virtualize && "max-h-[calc(100vh-28rem)] min-h-72")}
        >
          {/* 不用 shadcn 的 <Table> 外壳：它自带 overflow 容器，会抢走
              virtualizer 依赖的滚动元素（scrollRef 必须落在唯一的滚动容器上）。 */}
          <table className="w-full caption-bottom text-sm">
            <TableHeader className="sticky top-0 z-10 bg-muted/40 backdrop-blur-none [&_tr]:border-b">
              <TableRow className="bg-card hover:bg-card">
                <TableHead className="w-10 px-3">
                  <Checkbox
                    checked={allSelected ? true : someSelected ? "indeterminate" : false}
                    aria-label="全选本页"
                    onCheckedChange={(checked) => table.toggleAllRowsSelected(Boolean(checked))}
                  />
                </TableHead>
                {table.getHeaderGroups()[0]?.headers.map((header) => {
                  const field = HEADER_SORT[header.column.id];
                  const active = field && query.sort === field;
                  return (
                    <TableHead
                      key={header.id}
                      className={cn(
                        "h-9 px-3 text-[11px] font-medium text-muted-foreground",
                        header.column.id === "actions" && "w-12",
                        field && "cursor-pointer select-none hover:text-foreground"
                      )}
                      onClick={field ? () => applySort(field) : undefined}
                    >
                      <span className="inline-flex items-center gap-1">
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {active ? (
                          query.order === "asc" ? (
                            <ArrowUp className="size-3" />
                          ) : (
                            <ArrowDown className="size-3" />
                          )
                        ) : null}
                      </span>
                    </TableHead>
                  );
                })}
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && !rows.length ? (
                Array.from({ length: 8 }).map((_, index) => (
                  <TableRow key={index} className="hover:bg-transparent">
                    <TableCell className="px-3">
                      <Skeleton className="size-4 rounded" />
                    </TableCell>
                    {visibleColumns.map((column) => (
                      <TableCell key={column.id} className="px-3 py-3">
                        {column.id === "user" ? (
                          <div className="flex items-center gap-3">
                            <Skeleton className="size-9 rounded-full" />
                            <div className="space-y-1.5">
                              <Skeleton className="h-3.5 w-24 rounded" />
                              <Skeleton className="h-3 w-16 rounded" />
                            </div>
                          </div>
                        ) : column.id === "actions" ? (
                          <Skeleton className="size-7 rounded-md" />
                        ) : (
                          <div className="space-y-1.5">
                            <Skeleton className="h-3.5 w-20 rounded" />
                            <Skeleton className="h-3 w-14 rounded" />
                          </div>
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : !rows.length ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={colSpan} className="h-52 whitespace-normal">
                    <div className="flex flex-col items-center justify-center gap-2 text-center">
                      <div className="flex size-10 items-center justify-center rounded-full bg-muted">
                        <SearchX className="size-5 text-muted-foreground" />
                      </div>
                      <p className="max-w-sm text-sm text-muted-foreground">{emptyText}</p>
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                <>
                  {paddingTop > 0 ? (
                    <tr aria-hidden>
                      <td style={{ height: paddingTop }} colSpan={colSpan} />
                    </tr>
                  ) : null}
                  {visibleRows.map((row) => (
                    <TableRow
                      key={row.id}
                      data-state={row.getIsSelected() ? "selected" : undefined}
                      style={virtualize ? { height: rowHeight } : undefined}
                      className="group cursor-pointer"
                      onClick={() => onRowClick(row.original)}
                    >
                      <TableCell className="px-3" onClick={(event) => event.stopPropagation()}>
                        <Checkbox
                          checked={row.getIsSelected()}
                          aria-label={`选择 ${row.original.account ?? row.original.id}`}
                          onCheckedChange={(checked) => row.toggleSelected(Boolean(checked))}
                        />
                      </TableCell>
                      {row.getVisibleCells().map((cell) => (
                        <TableCell
                          key={cell.id}
                          className={cn("max-w-[240px] px-3", DENSITY[density].cellClass)}
                          onClick={
                            cell.column.id === "actions"
                              ? (event) => event.stopPropagation()
                              : undefined
                          }
                        >
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                  {paddingBottom > 0 ? (
                    <tr aria-hidden>
                      <td style={{ height: paddingBottom }} colSpan={colSpan} />
                    </tr>
                  ) : null}
                </>
              )}
            </TableBody>
          </table>
        </div>
        )}
        {footer ? <div className="border-t px-3 py-2.5">{footer}</div> : null}
      </div>
    </TooltipProvider>
  );
}

/** 卡片视图：头像与身份信息优先，适合按人浏览；选择与批量操作仍在列表视图里做 */
function UserGrid({
  rows,
  loading,
  emptyText,
  onOpen
}: {
  rows: { id: string; original: AdminAppUserItem; getIsSelected: () => boolean; toggleSelected: (value?: boolean) => void }[];
  loading: boolean;
  emptyText: string;
  onOpen: (user: AdminAppUserItem) => void;
}) {
  if (loading && !rows.length) {
    return (
      <div className="grid gap-3 p-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
        {Array.from({ length: 8 }).map((_, index) => (
          <div key={index} className="space-y-3 rounded-xl border p-4">
            <div className="flex items-center gap-3">
              <Skeleton className="size-12 rounded-full" />
              <div className="space-y-1.5">
                <Skeleton className="h-3.5 w-24 rounded" />
                <Skeleton className="h-3 w-16 rounded" />
              </div>
            </div>
            <Skeleton className="h-3 w-full rounded" />
            <Skeleton className="h-3 w-2/3 rounded" />
          </div>
        ))}
      </div>
    );
  }
  if (!rows.length) {
    return (
      <div className="flex h-52 flex-col items-center justify-center gap-2 text-center">
        <div className="flex size-10 items-center justify-center rounded-full bg-muted">
          <SearchX className="size-5 text-muted-foreground" />
        </div>
        <p className="max-w-sm text-sm text-muted-foreground">{emptyText}</p>
      </div>
    );
  }
  return (
    <div className="grid gap-3 p-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
      {rows.map((row) => {
        const item = row.original;
        const enabled = item.enabled !== false;
        const vip = vipState(item);
        const selected = row.getIsSelected();
        return (
          <div
            key={row.id}
            role="button"
            tabIndex={0}
            onClick={() => onOpen(item)}
            onKeyDown={(event) => {
              if (event.key === "Enter") onOpen(item);
            }}
            data-state={selected ? "selected" : undefined}
            className="group relative flex cursor-pointer flex-col gap-3 rounded-xl border p-4 transition-colors hover:border-foreground/20 hover:bg-muted/30 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none data-[state=selected]:border-primary/40 data-[state=selected]:bg-primary/5"
          >
            <div className="absolute top-3 right-3" onClick={(event) => event.stopPropagation()}>
              <Checkbox
                checked={selected}
                aria-label={`选择 ${item.account ?? item.id}`}
                onCheckedChange={(checked) => row.toggleSelected(Boolean(checked))}
                className={cn("transition-opacity", !selected && "opacity-0 group-hover:opacity-100 focus-visible:opacity-100")}
              />
            </div>
            <div className="flex min-w-0 items-center gap-3 pr-6">
              <UserAvatar item={item} enabled={enabled} className="size-12" />
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-1.5">
                  <span className="truncate text-sm font-semibold">
                    {item.nickname || item.account || `用户 ${item.id}`}
                  </span>
                  {vip === "active" || vip === "lifetime" ? <VipMark lifetime={vip === "lifetime"} /> : null}
                </div>
                <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
                  {item.account ? <span className="truncate">@{item.account}</span> : null}
                  <span className="shrink-0 rounded bg-muted px-1 font-mono text-[10px] leading-4">#{item.id}</span>
                </div>
              </div>
            </div>
            <div className="min-h-9 space-y-1 text-xs">
              {item.email ? (
                <div className="flex items-center gap-1.5 truncate">
                  <AtSign className="size-3 shrink-0 text-muted-foreground" />
                  <span className="truncate">{item.email}</span>
                </div>
              ) : null}
              {item.phone ? (
                <div className="flex items-center gap-1.5 text-muted-foreground">
                  <Phone className="size-3 shrink-0" />
                  <span className="tabular-nums">{item.phone}</span>
                </div>
              ) : null}
              {!item.email && !item.phone ? <span className="text-muted-foreground/60">未填写联系方式</span> : null}
            </div>
            <div className="mt-auto flex items-end justify-between gap-2 border-t pt-3">
              <div className="flex gap-4 text-xs">
                <div>
                  <div className="text-[11px] text-muted-foreground">积分</div>
                  <div className="font-semibold tabular-nums">{(item.integral ?? 0).toLocaleString("zh-CN")}</div>
                </div>
                <div>
                  <div className="text-[11px] text-muted-foreground">经验</div>
                  <div className="font-semibold tabular-nums">{(item.experience ?? 0).toLocaleString("zh-CN")}</div>
                </div>
              </div>
              <div className="text-right">
                <StatusCell item={item} />
                <div className="mt-1 text-[11px] text-muted-foreground">{relative(item.registerTime || item.createdAt)}注册</div>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function DensityToggle({ value, onChange }: { value: Density; onChange: (next: Density) => void }) {
  const order: Density[] = ["compact", "default", "comfortable"];
  const Icon = DENSITY[value].icon;
  const label = value === "compact" ? "紧凑" : value === "default" ? "标准" : "宽松";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          size="icon"
          variant="outline"
          className="size-8"
          aria-label={`行高：${label}，点击切换`}
          onClick={() => onChange(order[(order.indexOf(value) + 1) % order.length])}
        >
          <Icon className="size-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="top" className="text-xs">
        行高：{label}
      </TooltipContent>
    </Tooltip>
  );
}
