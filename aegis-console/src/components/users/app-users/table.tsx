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
  ChevronsUpDown,
  Columns3,
  Crown,
  Rows2,
  Rows3,
  Rows4
} from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { AdminAppUserItem } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { SORT_LABELS, type SortField, type UserQueryState } from "./shared";

/**
 * 用户表。
 *
 * 三件事和旧版不一样，且都是刻意的：
 *
 * 1. **不注册 rowSortingFeature。** 排序发生在服务端 —— 点表头改的是查询参数，
 *    不是本地行序。旧版注册了客户端排序但数据是服务端分页的：点一下只排当前 20 条，
 *    翻页就乱。一个说谎的排序控件比没有排序更糟。
 * 2. **getRowId 用真实用户 ID。** 默认按行下标做 id，翻页或重新拉取之后
 *    选中状态会挪到别人身上 —— 批量封禁场景下这是会出事故的那种 bug。
 * 3. **行数多时虚拟滚动。** 每页可到 500 条，全量渲染会让滚动明显掉帧。
 */

const tableFeaturesSet = tableFeatures({ rowSelectionFeature, columnVisibilityFeature });

type UserColumnDef = ColumnDef<typeof tableFeaturesSet, AdminAppUserItem>;

/** 超过这个行数才启用虚拟滚动：小表格全量渲染更简单，也没有性能问题。 */
const VIRTUALIZE_THRESHOLD = 60;

export type Density = "compact" | "default" | "comfortable";

const DENSITY: Record<Density, { rowHeight: number; cellClass: string; icon: typeof Rows2 }> = {
  compact: { rowHeight: 40, cellClass: "py-1.5", icon: Rows4 },
  default: { rowHeight: 52, cellClass: "py-2.5", icon: Rows3 },
  comfortable: { rowHeight: 64, cellClass: "py-4", icon: Rows2 }
};

const COLUMN_LABELS: Record<string, string> = {
  user: "用户",
  contact: "联系方式",
  status: "状态",
  integral: "积分",
  experience: "经验",
  vip: "会员",
  registerIp: "注册 IP",
  register: "注册"
};

/** 可点表头排序的列 → 排序字段。没有映射的列表头不可点，也不显示排序图标。 */
const COLUMN_SORT: Partial<Record<string, SortField>> = {
  user: "account",
  contact: "email",
  integral: "integral",
  experience: "experience",
  vip: "vipExpireAt",
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
          const avatar = typeof item.avatar === "string" ? item.avatar : "";
          return (
            <div className="flex items-center gap-2.5">
              <Avatar className="size-7 rounded-md">
                <AvatarImage src={avatar} />
                <AvatarFallback className="rounded-md text-[10px]">
                  {initials(item.nickname, item.account)}
                </AvatarFallback>
              </Avatar>
              <div className="min-w-0">
                <div className="truncate text-sm font-medium">
                  {item.nickname || item.account || `#${item.id}`}
                </div>
                <div className="truncate text-[11px] text-muted-foreground">
                  {item.nickname && item.account ? item.account : `ID ${item.id}`}
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
          if (!email && !phone) return <span className="text-xs text-muted-foreground">—</span>;
          return (
            <div className="min-w-0">
              {email ? <div className="truncate text-xs">{email}</div> : null}
              {phone ? (
                <div className="truncate text-[11px] tabular-nums text-muted-foreground">{phone}</div>
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
          return (
            <div className="flex flex-col gap-0.5">
              <span className="flex items-center gap-1.5 text-xs">
                <span
                  className={cn(
                    "inline-block size-1.5 rounded-full",
                    enabled ? "bg-emerald-500" : "bg-red-500"
                  )}
                />
                {enabled ? "启用" : "受限"}
              </span>
              {!enabled && item.disabledReason ? (
                <span
                  className="max-w-[140px] truncate text-[11px] text-muted-foreground"
                  title={item.disabledReason}
                >
                  {item.disabledReason}
                </span>
              ) : null}
            </div>
          );
        }
      },
      {
        id: "integral",
        header: "积分",
        cell: ({ row }) => (
          <span className="tabular-nums">{(row.original.integral ?? 0).toLocaleString("zh-CN")}</span>
        )
      },
      {
        id: "experience",
        header: "经验",
        cell: ({ row }) => (
          <span className="tabular-nums">
            {(row.original.experience ?? 0).toLocaleString("zh-CN")}
          </span>
        )
      },
      {
        id: "vip",
        header: "会员",
        cell: ({ row }) => {
          const expire = row.original.vipExpireAt;
          if (!expire) return <span className="text-xs text-muted-foreground">—</span>;
          const active = new Date(expire).getTime() > Date.now();
          return (
            <Badge variant={active ? "warning" : "outline"} size="sm" className="gap-1">
              <Crown className="size-3" />
              {active ? "有效" : "已过期"}
            </Badge>
          );
        }
      },
      {
        id: "registerIp",
        header: "注册 IP",
        cell: ({ row }) => (
          <span className="font-mono text-[11px] text-muted-foreground">
            {row.original.registerIP || "—"}
          </span>
        )
      },
      {
        id: "register",
        header: "注册",
        cell: ({ row }) => {
          const item = row.original;
          const location = [item.registerProvince, item.registerCity].filter(Boolean).join(" ");
          return (
            <div className="min-w-0">
              <div
                className="truncate text-xs tabular-nums text-muted-foreground"
                title={fmtTime(item.registerTime || item.createdAt)}
              >
                {relative(item.registerTime || item.createdAt)}
              </div>
              {location ? (
                <div className="truncate text-[11px] text-muted-foreground/80">{location}</div>
              ) : null}
            </div>
          );
        }
      }
    ],
    []
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
  const colSpan = table.getVisibleFlatColumns().length + 1;

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

  function toggleSort(columnId: string) {
    const field = COLUMN_SORT[columnId];
    if (!field) return;
    if (query.sort === field) {
      onQueryChange({ ...query, order: query.order === "asc" ? "desc" : "asc", page: 1 });
      return;
    }
    onQueryChange({ ...query, sort: field, order: "desc", page: 1 });
  }

  // TooltipProvider 包住整棵子树：行高按钮的 tooltip 在工具栏里，
  // 只包表格容器会让它落在 Provider 之外（Radix 要求必须有 Provider 祖先）。
  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-2">
        <div className="flex items-center justify-end gap-1.5">
          <DensityToggle value={density} onChange={setDensity} />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="outline" className="h-8 gap-1.5 text-xs">
                <Columns3 className="size-3.5" />列
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-40">
              <DropdownMenuLabel className="text-xs">显示的列</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {table.getAllColumns().map((column) => (
                <DropdownMenuCheckboxItem
                  key={column.id}
                  className="text-xs"
                  checked={column.getIsVisible()}
                  onCheckedChange={(checked) => column.toggleVisibility(Boolean(checked))}
                >
                  {COLUMN_LABELS[column.id] ?? column.id}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        <div
          ref={scrollRef}
          className={cn(
            "overflow-auto rounded-xl border",
            virtualize && "max-h-[calc(100vh-26rem)] min-h-64"
          )}
        >
          <table className="w-full caption-bottom text-sm">
            <thead className="sticky top-0 z-10 bg-card">
              <tr className="border-b">
                <th className="w-10 px-3 py-2 text-left">
                  <Checkbox
                    checked={allSelected ? true : someSelected ? "indeterminate" : false}
                    aria-label="全选本页"
                    onCheckedChange={(checked) => table.toggleAllRowsSelected(Boolean(checked))}
                  />
                </th>
                {table.getHeaderGroups()[0]?.headers.map((header) => {
                  const field = COLUMN_SORT[header.column.id];
                  const active = field && query.sort === field;
                  return (
                    <th
                      key={header.id}
                      className={cn(
                        "px-3 py-2 text-left text-xs font-medium text-muted-foreground",
                        field && "cursor-pointer select-none hover:text-foreground"
                      )}
                      onClick={() => toggleSort(header.column.id)}
                    >
                      <span className="inline-flex items-center gap-1">
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {field ? (
                          active ? (
                            query.order === "asc" ? (
                              <ArrowUp className="size-3" />
                            ) : (
                              <ArrowDown className="size-3" />
                            )
                          ) : (
                            <ChevronsUpDown className="size-3 opacity-30" />
                          )
                        ) : null}
                      </span>
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>
              {loading && !rows.length ? (
                Array.from({ length: 6 }).map((_, index) => (
                  <tr key={index} className="border-b last:border-b-0">
                    <td colSpan={colSpan} className="px-3 py-2.5">
                      <Skeleton className="h-7 w-full rounded-md" />
                    </td>
                  </tr>
                ))
              ) : !rows.length ? (
                <tr>
                  <td colSpan={colSpan} className="h-40 px-6 text-center text-sm text-muted-foreground">
                    {emptyText}
                  </td>
                </tr>
              ) : (
                <>
                  {paddingTop > 0 ? (
                    <tr aria-hidden>
                      <td style={{ height: paddingTop }} colSpan={colSpan} />
                    </tr>
                  ) : null}
                  {visibleRows.map((row) => (
                    <tr
                      key={row.id}
                      data-selected={row.getIsSelected()}
                      style={virtualize ? { height: rowHeight } : undefined}
                      className={cn(
                        "cursor-pointer border-b transition-colors last:border-b-0",
                        "hover:bg-muted/40 data-[selected=true]:bg-accent/50"
                      )}
                      onClick={() => onRowClick(row.original)}
                    >
                      <td className="px-3" onClick={(event) => event.stopPropagation()}>
                        <Checkbox
                          checked={row.getIsSelected()}
                          aria-label={`选择 ${row.original.account ?? row.original.id}`}
                          onCheckedChange={(checked) => row.toggleSelected(Boolean(checked))}
                        />
                      </td>
                      {row.getVisibleCells().map((cell) => (
                        <td
                          key={cell.id}
                          className={cn("max-w-[220px] px-3 align-middle", DENSITY[density].cellClass)}
                        >
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      ))}
                    </tr>
                  ))}
                  {paddingBottom > 0 ? (
                    <tr aria-hidden>
                      <td style={{ height: paddingBottom }} colSpan={colSpan} />
                    </tr>
                  ) : null}
                </>
              )}
            </tbody>
          </table>
        </div>

        {query.sort !== "createdAt" || query.order !== "desc" ? (
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span>
              按「{SORT_LABELS[query.sort]}」{query.order === "asc" ? "升序" : "降序"}排列（服务端排序，跨页有效）
            </span>
            <Button
              size="sm"
              variant="ghost"
              className="h-5 px-1.5 text-[11px]"
              onClick={() => onQueryChange({ ...query, sort: "createdAt", order: "desc", page: 1 })}
            >
              恢复默认
            </Button>
          </div>
        ) : null}
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
