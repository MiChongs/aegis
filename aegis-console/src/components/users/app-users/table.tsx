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
import { Badge } from "@/components/ui/badge";
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

function vipState(expireAt?: string | null): "none" | "active" | "expired" {
  if (!expireAt) return "none";
  const time = new Date(expireAt).getTime();
  if (Number.isNaN(time) || new Date(expireAt).getUTCFullYear() <= 1) return "none";
  return time > Date.now() ? "active" : "expired";
}

async function copyText(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(`已复制${label}`);
  } catch {
    toast.error("复制失败，请手动选择复制");
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
  emptyText
}: {
  data: AdminAppUserItem[];
  loading: boolean;
  query: UserQueryState;
  onQueryChange: (next: UserQueryState) => void;
  selection: RowSelectionState;
  onSelectionChange: (next: RowSelectionState) => void;
  onRowClick: (user: AdminAppUserItem) => void;
  emptyText: string;
}) {
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
          const vip = vipState(item.vipExpireAt);
          return (
            <div className="flex items-center gap-3">
              <div className="relative shrink-0">
                <Avatar className="size-9 border">
                  <AvatarImage src={typeof item.avatar === "string" ? item.avatar : ""} />
                  <AvatarFallback className="text-[11px]">
                    {initials(item.nickname, item.account)}
                  </AvatarFallback>
                </Avatar>
                {/* 状态点长在头像上：扫一列头像就能数出受限的人，不用逐行看状态列 */}
                <span
                  aria-hidden
                  className={cn(
                    "absolute -bottom-px -right-px size-2.5 rounded-full ring-2 ring-card",
                    enabled ? "bg-emerald-500" : "bg-red-500"
                  )}
                />
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="truncate text-sm font-medium">
                    {item.nickname || item.account || `用户 ${item.id}`}
                  </span>
                  {vip === "active" ? (
                    <Crown aria-label="有效会员" className="size-3.5 shrink-0 text-amber-500" />
                  ) : null}
                </div>
                <div className="truncate text-[11px] text-muted-foreground">
                  {item.account && item.nickname ? `@${item.account} · ` : ""}
                  <span className="font-mono">#{item.id}</span>
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
            return <span className="text-xs text-muted-foreground/60">未留联系方式</span>;
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
          const vip = vipState(item.vipExpireAt);
          return (
            <div className="min-w-0 space-y-0.5">
              <div className="flex items-baseline gap-1 text-xs">
                <span className="text-muted-foreground">积分</span>
                <span className="font-medium tabular-nums">
                  {(item.integral ?? 0).toLocaleString("zh-CN")}
                </span>
                <span className="px-0.5 text-muted-foreground/50">·</span>
                <span className="text-muted-foreground">经验</span>
                <span className="font-medium tabular-nums">
                  {(item.experience ?? 0).toLocaleString("zh-CN")}
                </span>
              </div>
              {vip === "active" ? (
                <Badge variant="warning" size="sm" className="gap-1">
                  <Crown className="size-3" />至 {fmtDate(item.vipExpireAt)}
                </Badge>
              ) : vip === "expired" ? (
                <span className="text-[11px] text-muted-foreground/70">
                  会员已过期（{fmtDate(item.vipExpireAt)}）
                </span>
              ) : null}
            </div>
          );
        }
      },
      {
        id: "status",
        header: "状态",
        cell: ({ row }) => {
          const item = row.original;
          const enabled = item.enabled !== false;
          if (enabled) {
            return (
              <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[11px] font-medium text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-400">
                <span className="size-1.5 rounded-full bg-emerald-500" />
                正常
              </span>
            );
          }
          const until = item.disabledEndTime ? fmtTime(item.disabledEndTime) : "";
          return (
            <div className="min-w-0 space-y-0.5">
              <span className="inline-flex items-center gap-1.5 rounded-full border border-red-200 bg-red-50 px-2 py-0.5 text-[11px] font-medium text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-400">
                <span className="size-1.5 rounded-full bg-red-500" />
                受限{until !== "—" && until ? ` · 至 ${until.slice(5, 11)}` : ""}
              </span>
              {item.disabledReason ? (
                <div
                  className="max-w-[150px] truncate text-[11px] text-muted-foreground"
                  title={item.disabledReason}
                >
                  {item.disabledReason}
                </div>
              ) : null}
            </div>
          );
        }
      },
      {
        id: "register",
        header: "注册",
        cell: ({ row }) => {
          const item = row.original;
          const location = [item.registerProvince, item.registerCity].filter(Boolean).join(" ");
          return (
            <div className="min-w-0 space-y-0.5">
              <div
                className="truncate text-xs tabular-nums"
                title={fmtTime(item.registerTime || item.createdAt)}
              >
                {relative(item.registerTime || item.createdAt)}
              </div>
              {item.registerIP || location ? (
                <div className="truncate text-[11px] text-muted-foreground">
                  {item.registerIP ? <span className="font-mono">{item.registerIP}</span> : null}
                  {item.registerIP && location ? " · " : ""}
                  {location}
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
      <div className="overflow-hidden rounded-xl border bg-card text-card-foreground shadow-sm">
        <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {loading && !rows.length ? (
              <span>加载中…</span>
            ) : (
              <span className="tabular-nums">本页 {rows.length} 人</span>
            )}
            {selectedCount > 0 ? (
              <>
                <span className="text-muted-foreground/40">|</span>
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
                <DropdownMenuLabel className="text-xs">
                  排序字段（服务端排序，跨页有效）
                </DropdownMenuLabel>
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
                    降序（大 → 小 / 新 → 旧）
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="asc" className="text-xs">
                    升序（小 → 大 / 旧 → 新）
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
                      恢复默认（创建时间降序）
                    </DropdownMenuItem>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>

            <DensityToggle value={density} onChange={setDensity} />

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="icon"
                  variant="outline"
                  className="size-8"
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

        <div
          ref={scrollRef}
          className={cn("overflow-auto", virtualize && "max-h-[calc(100vh-28rem)] min-h-72")}
        >
          {/* 不用 shadcn 的 <Table> 外壳：它自带 overflow 容器，会抢走
              virtualizer 依赖的滚动元素（scrollRef 必须落在唯一的滚动容器上）。 */}
          <table className="w-full caption-bottom text-sm">
            <TableHeader className="sticky top-0 z-10 bg-card">
              <TableRow className="hover:bg-transparent">
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
                        "px-3 text-xs text-muted-foreground",
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
      </div>
    </TooltipProvider>
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
