"use client";

import { type ReactNode } from "react";
import {
  type CellData,
  type ColumnDef,
  createSortedRowModel,
  flexRender,
  type RowData,
  rowSortingFeature,
  sortFns,
  type SortingState,
  tableFeatures,
  useTable
} from "@tanstack/react-table";
import { ArrowUpDown } from "lucide-react";
import { useState } from "react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// v9 起功能按需注册（core row model 已内置，无需再传 getCoreRowModel）。
// 本表只用到排序，因此仅注册排序功能，其余特性会被 tree-shake 掉。
export const dataTableFeatures = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns
});

// v9 的 ColumnDef 首个泛型是「已注册功能集合」。此处统一导出别名，
// 供各列定义文件复用，避免每个文件都写 typeof dataTableFeatures。
// v9 收紧了泛型约束：TData 必须满足 RowData（对象或数组），TValue 必须满足 CellData。
export type DataTableColumnDef<TData extends RowData, TValue extends CellData = CellData> = ColumnDef<
  typeof dataTableFeatures,
  TData,
  TValue
>;

// 表级不再暴露 TValue：v9 的 useTable 在表维度将 TValue 固定为 unknown
//（同一表格各列的 value 类型互不相同），各列自身的取值类型仍由列定义保留。
type DataTableProps<TData extends RowData> = {
  columns: DataTableColumnDef<TData>[];
  data: TData[];
  onRowClick?: (row: TData) => void;
  emptyText?: string;
};

// 安全渲染：防止 react-table 在列缺少 cell/accessor 时将整行对象传给 React
function safeRender(node: unknown): ReactNode {
  if (node === null || node === undefined) return "—";
  if (typeof node === "string" || typeof node === "number" || typeof node === "boolean") return node;
  // React element（有 $$typeof 属性）
  if (typeof node === "object" && "$$typeof" in (node as Record<string, unknown>)) return node as ReactNode;
  // 其他对象（AdminAccount 等）不能直接渲染
  return String(node);
}

export function DataTable<TData extends RowData>({
  columns,
  data,
  onRowClick,
  emptyText = "暂无数据"
}: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>([]);

  const table = useTable({
    features: dataTableFeatures,
    data,
    columns,
    onSortingChange: setSorting,
    state: { sorting }
  });

  return (
    <div className="overflow-hidden rounded-xl border">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className="hover:bg-transparent">
              {headerGroup.headers.map((header) => (
                <TableHead
                  key={header.id}
                  className={cn("h-9 text-xs", header.column.getCanSort() && "cursor-pointer select-none")}
                  onClick={header.column.getToggleSortingHandler()}
                >
                  <div className="flex items-center gap-1">
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                    {header.column.getCanSort() && <ArrowUpDown className="size-3 text-muted-foreground/50" />}
                  </div>
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-32 text-center text-sm text-muted-foreground">
                {emptyText}
              </TableCell>
            </TableRow>
          ) : (
            table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                className={cn(onRowClick && "cursor-pointer")}
                onClick={() => onRowClick?.(row.original)}
              >
                {/* 未注册 columnVisibilityFeature（本表无列隐藏），core 的 getAllCells 语义等价 */}
                {row.getAllCells().map((cell) => (
                  <TableCell key={cell.id} className="py-2.5 text-sm">
                    {safeRender(flexRender(cell.column.columnDef.cell, cell.getContext()))}
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
