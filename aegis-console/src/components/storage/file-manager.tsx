"use client";

import * as React from "react";
import {
  ArrowDownAZ, ArrowUpAZ, ChevronLeft, ChevronRight, Filter, FolderOpen, HardDrive,
  Home, LayoutGrid, List, Loader2, RefreshCw, RotateCcw, Search, SlidersHorizontal,
  Trash2, X,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useBatchStorageObjectsMutation, useCleanupTrashMutation, useStorageObjectsQuery,
} from "@/lib/admin-hooks";
import type { StorageBatchAction, StorageObject, StorageObjectQuery, StorageObjectSort } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { FileDetailSheet } from "./file-detail-sheet";
import {
  FileKindIcon, FileThumbnail, FolderIcon, KIND_LABELS, MasqueradeBadge, ObjectStatusBadge,
  baseName, evictThumbnailCache, folderSegments, formatBytes, formatDate, parseSizeInput,
} from "./file-shared";

/* ================================================================== */
/*  常量                                                               */
/* ================================================================== */

const PAGE_SIZE_LIST = 30;
const PAGE_SIZE_GRID = 48;

/** 与后端 contentType 的大类前缀写法对齐（`image/` 结尾带斜杠即按前缀匹配） */
const TYPE_FILTERS: Array<{ value: string; label: string }> = [
  { value: "all", label: "全部类型" },
  { value: "image/", label: KIND_LABELS.image },
  { value: "video/", label: KIND_LABELS.video },
  { value: "audio/", label: KIND_LABELS.audio },
  { value: "application/pdf", label: KIND_LABELS.pdf },
  { value: "text/", label: KIND_LABELS.text },
];

const STATUS_FILTERS: Array<{ value: string; label: string }> = [
  { value: "active", label: "活跃" },
  { value: "deleted", label: "回收站" },
  { value: "pending_review", label: "待审核" },
  { value: "all", label: "全部状态" },
];

const SORT_OPTIONS: Array<{ value: StorageObjectSort; label: string }> = [
  { value: "createdAt", label: "上传时间" },
  { value: "size", label: "文件大小" },
  { value: "fileName", label: "文件名" },
  { value: "objectKey", label: "对象键" },
];

type ViewMode = "grid" | "list";

type Filters = {
  keyword: string;
  contentType: string;
  status: string;
  uploaderType: string;
  minSize: string;
  maxSize: string;
  createdFrom: string;
  createdTo: string;
};

const EMPTY_FILTERS: Filters = {
  keyword: "", contentType: "all", status: "active", uploaderType: "all",
  minSize: "", maxSize: "", createdFrom: "", createdTo: "",
};

/* ================================================================== */
/*  文件管理面板                                                       */
/* ================================================================== */

export type StorageConfigOption = { id: number; label: string; provider: string };

export function FileManagerPanel({
  configs, canPurge,
}: {
  configs: StorageConfigOption[];
  canPurge: boolean;
}) {
  const [view, setView] = React.useState<ViewMode>("grid");
  const [configId, setConfigId] = React.useState<string>("all");
  const [folder, setFolder] = React.useState("");
  const [filters, setFilters] = React.useState<Filters>(EMPTY_FILTERS);
  const [showAdvanced, setShowAdvanced] = React.useState(false);
  const [sort, setSort] = React.useState<StorageObjectSort>("createdAt");
  const [order, setOrder] = React.useState<"asc" | "desc">("desc");
  const [page, setPage] = React.useState(1);
  const [selected, setSelected] = React.useState<Set<number>>(new Set());
  const [detail, setDetail] = React.useState<StorageObject | null>(null);

  // 搜到东西的时候按目录逐层看没有意义 —— 命中的文件散在各级目录里。
  // 因此有关键字就自动切成平铺检索，没有就回到目录浏览。
  const folderView = filters.keyword.trim() === "";
  const limit = view === "grid" ? PAGE_SIZE_GRID : PAGE_SIZE_LIST;

  const query: StorageObjectQuery = React.useMemo(() => ({
    configId: configId === "all" ? undefined : Number(configId),
    folder: folderView ? folder : undefined,
    folderView,
    keyword: filters.keyword.trim() || undefined,
    contentType: filters.contentType === "all" ? undefined : filters.contentType,
    status: filters.status === "all" ? undefined : filters.status,
    uploaderType: filters.uploaderType === "all" ? undefined : filters.uploaderType,
    minSize: parseSizeInput(filters.minSize),
    maxSize: parseSizeInput(filters.maxSize),
    createdFrom: filters.createdFrom || undefined,
    createdTo: filters.createdTo || undefined,
    sort, order, page, limit,
  }), [configId, folder, folderView, filters, sort, order, page, limit]);

  const objectsQuery = useStorageObjectsQuery(query);
  const batchMutation = useBatchStorageObjectsMutation();
  const cleanupMutation = useCleanupTrashMutation();

  const data = objectsQuery.data;
  const objects = React.useMemo(() => data?.items ?? [], [data]);
  const folders = data?.folders ?? [];
  const total = data?.total ?? 0;
  const summary = data?.summary;
  const totalPages = Math.max(1, Math.ceil(total / limit));

  // 任一筛选条件变化都要回到第 1 页：停在第 5 页看一个只有 2 页的结果集
  // 会得到一个空列表，而它看起来和"没有数据"一模一样。
  const resetToFirstPage = React.useCallback(() => {
    setPage(1);
    setSelected(new Set());
  }, []);

  const patchFilters = React.useCallback((patch: Partial<Filters>) => {
    setFilters((prev) => ({ ...prev, ...patch }));
    resetToFirstPage();
  }, [resetToFirstPage]);

  const enterFolder = React.useCallback((path: string) => {
    setFolder(path);
    resetToFirstPage();
  }, [resetToFirstPage]);

  const toggleSelect = React.useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }, []);

  const allOnPageSelected = objects.length > 0 && objects.every((item) => selected.has(item.id));
  const toggleSelectAll = React.useCallback(() => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (objects.every((item) => next.has(item.id))) objects.forEach((item) => next.delete(item.id));
      else objects.forEach((item) => next.add(item.id));
      return next;
    });
  }, [objects]);

  const runBatch = React.useCallback(async (action: StorageBatchAction) => {
    const ids = [...selected];
    if (ids.length === 0) return;
    try {
      const result = await batchMutation.mutateAsync({ action, ids });
      ids.forEach((id) => evictThumbnailCache(id));
      setSelected(new Set());
      const verb = { delete: "移入回收站", restore: "恢复", purge: "永久删除" }[action];
      // 汇报服务端实际影响的条数而不是请求条数：恢复一批里混着本来就活跃的
      // 对象时，两者不相等，报请求数等于在撒谎。
      toast.success(
        `已${verb} ${result.affected} 个文件`,
        result.skipped > 0 ? { description: `另有 ${result.skipped} 个状态不满足条件，已跳过` } : undefined,
      );
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "批量操作失败");
    }
  }, [batchMutation, selected]);

  const activeFilterCount = countActiveFilters(filters);
  const configLabel = configs.find((item) => String(item.id) === configId)?.label;

  return (
    <div className="space-y-3">
      {/* ── 汇总条 ── */}
      <SummaryBar summary={summary} loading={objectsQuery.isLoading} />

      {/* ── 工具栏 ── */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-52 flex-1 sm:max-w-xs">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filters.keyword}
            onChange={(event) => patchFilters({ keyword: event.target.value })}
            placeholder="搜索文件名或对象键..."
            className="h-8 pl-8 pr-7 text-xs"
          />
          {filters.keyword ? (
            <button
              type="button" title="清除搜索"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => patchFilters({ keyword: "" })}
            >
              <X className="size-3" />
            </button>
          ) : null}
        </div>

        <Select value={configId} onValueChange={(value) => { setConfigId(value); setFolder(""); resetToFirstPage(); }}>
          <SelectTrigger className="h-8 w-40 text-xs"><SelectValue placeholder="存储配置" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部存储</SelectItem>
            {configs.map((item) => (
              <SelectItem key={item.id} value={String(item.id)}>{item.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={filters.contentType} onValueChange={(value) => patchFilters({ contentType: value })}>
          <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            {TYPE_FILTERS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>

        <Select value={filters.status} onValueChange={(value) => patchFilters({ status: value })}>
          <SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            {STATUS_FILTERS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>

        <Button
          variant={showAdvanced || activeFilterCount > 0 ? "default" : "outline"}
          size="sm" className="h-8 gap-1"
          onClick={() => setShowAdvanced((prev) => !prev)}
        >
          <SlidersHorizontal className="size-3.5" />
          筛选
          {activeFilterCount > 0 ? <Badge variant="outline" className="px-1 text-[9px]">{activeFilterCount}</Badge> : null}
        </Button>

        <div className="flex-1" />

        <SortControl sort={sort} order={order} onSortChange={(value) => { setSort(value); setPage(1); }} onOrderToggle={() => { setOrder((prev) => (prev === "asc" ? "desc" : "asc")); setPage(1); }} />

        <div className="flex items-center rounded-md border p-0.5">
          <ViewToggle icon={LayoutGrid} label="网格视图" active={view === "grid"} onClick={() => { setView("grid"); setPage(1); }} />
          <ViewToggle icon={List} label="列表视图" active={view === "list"} onClick={() => { setView("list"); setPage(1); }} />
        </div>

        <Button variant="outline" size="sm" className="h-8" title="刷新" onClick={() => objectsQuery.refetch()}>
          <RefreshCw className={cn("size-3.5", objectsQuery.isFetching && "animate-spin")} />
        </Button>
      </div>

      {showAdvanced ? (
        <AdvancedFilters filters={filters} onPatch={patchFilters} onReset={() => { setFilters(EMPTY_FILTERS); resetToFirstPage(); }} />
      ) : null}

      {/* ── 面包屑 ── */}
      {folderView ? (
        <FolderBreadcrumb folder={folder} onNavigate={enterFolder} />
      ) : (
        <p className="text-xs text-muted-foreground">
          搜索结果跨全部目录，共 {total} 个匹配文件
        </p>
      )}

      {/* ── 批量操作条 ── */}
      {selected.size > 0 ? (
        <BatchBar
          count={selected.size}
          pending={batchMutation.isPending}
          canPurge={canPurge}
          status={filters.status}
          onClear={() => setSelected(new Set())}
          onRun={runBatch}
        />
      ) : null}

      {/* ── 主体 ── */}
      <Card>
        <CardContent className="p-0">
          {objectsQuery.isLoading ? (
            <FileSkeleton view={view} />
          ) : objects.length === 0 && folders.length === 0 ? (
            <EmptyBrowser hasFilters={activeFilterCount > 0 || Boolean(filters.keyword)} folder={folder} />
          ) : view === "grid" ? (
            <GridView
              folders={folders} objects={objects} selected={selected}
              onEnterFolder={enterFolder} onToggle={toggleSelect} onOpen={setDetail}
            />
          ) : (
            <ListView
              folders={folders} objects={objects} selected={selected}
              allSelected={allOnPageSelected}
              onEnterFolder={enterFolder} onToggle={toggleSelect} onToggleAll={toggleSelectAll} onOpen={setDetail}
            />
          )}

          <Pagination
            page={page} totalPages={totalPages} total={total}
            fetching={objectsQuery.isFetching}
            onPrev={() => setPage((prev) => Math.max(1, prev - 1))}
            onNext={() => setPage((prev) => Math.min(totalPages, prev + 1))}
          />
        </CardContent>
      </Card>

      {/* ── 回收站清理 ── */}
      {filters.status === "deleted" && canPurge && total > 0 ? (
        <div className="flex items-center justify-between rounded-lg border border-dashed px-3 py-2 text-xs">
          <span className="text-muted-foreground">
            回收站里的文件仍占用存储空间，清理后不可恢复
          </span>
          <Button
            variant="outline" size="sm" className="h-7 gap-1"
            disabled={cleanupMutation.isPending}
            onClick={async () => {
              try {
                const result = await cleanupMutation.mutateAsync(30);
                toast.success(`已清理 ${(result as { deleted?: number })?.deleted ?? 0} 个文件`);
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "清理失败");
              }
            }}
          >
            {cleanupMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
            清理 30 天前的文件
          </Button>
        </div>
      ) : null}

      <FileDetailSheet
        object={detail}
        canPurge={canPurge}
        configLabel={configLabel}
        onOpenChange={(open) => { if (!open) setDetail(null); }}
        onMutated={(objectId) => {
          setSelected((prev) => { const next = new Set(prev); next.delete(objectId); return next; });
        }}
      />
    </div>
  );
}

/* ================================================================== */
/*  汇总条                                                             */
/* ================================================================== */

/*
 * 「当前范围里活跃多少、回收站多少」是两个独立事实，界面上必须同时给出。
 * 只显示当前筛选命中的那一档，管理员分不清"这个目录是空的"
 * 和"这个目录的文件全在回收站里"。
 */
function SummaryBar({ summary, loading }: { summary?: { totalFiles: number; totalSize: number; activeFiles: number; activeSize: number; deletedFiles: number; deletedSize: number }; loading: boolean }) {
  if (loading && !summary) {
    return <div className="grid gap-2 sm:grid-cols-3">{[0, 1, 2].map((key) => <Skeleton key={key} className="h-14 rounded-xl" />)}</div>;
  }
  if (!summary) return null;
  return (
    <div className="grid gap-2 sm:grid-cols-3">
      <StatTile icon={HardDrive} label="当前范围" value={formatBytes(summary.totalSize)} hint={`${summary.totalFiles.toLocaleString("zh-CN")} 个文件`} />
      <StatTile icon={FolderOpen} label="活跃" value={formatBytes(summary.activeSize)} hint={`${summary.activeFiles.toLocaleString("zh-CN")} 个文件`} />
      <StatTile icon={Trash2} label="回收站" value={formatBytes(summary.deletedSize)} hint={`${summary.deletedFiles.toLocaleString("zh-CN")} 个文件`} tone={summary.deletedFiles > 0 ? "warn" : undefined} />
    </div>
  );
}

function StatTile({
  icon: Icon, label, value, hint, tone,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string; value: string; hint: string; tone?: "warn";
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border bg-card px-3 py-2.5">
      <Icon className={cn("size-4 shrink-0", tone === "warn" ? "text-amber-500" : "text-muted-foreground")} />
      <div className="min-w-0">
        <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{label}</div>
        <div className="truncate font-mono text-sm font-semibold">{value}</div>
      </div>
      <div className="ml-auto shrink-0 text-[10px] text-muted-foreground">{hint}</div>
    </div>
  );
}

/* ================================================================== */
/*  筛选 / 排序                                                        */
/* ================================================================== */

function AdvancedFilters({
  filters, onPatch, onReset,
}: {
  filters: Filters;
  onPatch: (patch: Partial<Filters>) => void;
  onReset: () => void;
}) {
  return (
    <div className="grid gap-3 rounded-lg border bg-muted/20 p-3 sm:grid-cols-2 lg:grid-cols-4">
      <div className="space-y-1">
        <Label className="text-[10px] text-muted-foreground">上传者</Label>
        <Select value={filters.uploaderType} onValueChange={(value) => onPatch({ uploaderType: value })}>
          <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">不限</SelectItem>
            <SelectItem value="user">应用用户</SelectItem>
            <SelectItem value="admin">管理员</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-1">
        <Label className="text-[10px] text-muted-foreground">大小区间</Label>
        <div className="flex items-center gap-1">
          <Input value={filters.minSize} onChange={(event) => onPatch({ minSize: event.target.value })} placeholder="如 100k" className="h-8 text-xs" />
          <span className="text-xs text-muted-foreground">–</span>
          <Input value={filters.maxSize} onChange={(event) => onPatch({ maxSize: event.target.value })} placeholder="如 10M" className="h-8 text-xs" />
        </div>
      </div>
      <div className="space-y-1">
        <Label className="text-[10px] text-muted-foreground">上传时间（起）</Label>
        <Input type="date" value={filters.createdFrom} onChange={(event) => onPatch({ createdFrom: event.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1">
        <Label className="text-[10px] text-muted-foreground">上传时间（止）</Label>
        <div className="flex items-center gap-1">
          <Input type="date" value={filters.createdTo} onChange={(event) => onPatch({ createdTo: event.target.value })} className="h-8 text-xs" />
          <Button variant="ghost" size="sm" className="h-8 shrink-0 px-2" title="重置全部筛选" onClick={onReset}>
            <RotateCcw className="size-3.5" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function SortControl({
  sort, order, onSortChange, onOrderToggle,
}: {
  sort: StorageObjectSort;
  order: "asc" | "desc";
  onSortChange: (value: StorageObjectSort) => void;
  onOrderToggle: () => void;
}) {
  return (
    <div className="flex items-center gap-1">
      <Select value={sort} onValueChange={(value) => onSortChange(value as StorageObjectSort)}>
        <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
        <SelectContent>
          {SORT_OPTIONS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
        </SelectContent>
      </Select>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="outline" size="sm" className="h-8 px-2" onClick={onOrderToggle}>
            {order === "asc" ? <ArrowUpAZ className="size-3.5" /> : <ArrowDownAZ className="size-3.5" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{order === "asc" ? "升序" : "降序"}</TooltipContent>
      </Tooltip>
    </div>
  );
}

function ViewToggle({
  icon: Icon, label, active, onClick,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string; active: boolean; onClick: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button" onClick={onClick} aria-label={label} aria-pressed={active}
          className={cn(
            "flex size-7 items-center justify-center rounded transition-colors",
            active ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent",
          )}
        >
          <Icon className="size-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function countActiveFilters(filters: Filters): number {
  let count = 0;
  if (filters.uploaderType !== "all") count += 1;
  if (filters.minSize.trim()) count += 1;
  if (filters.maxSize.trim()) count += 1;
  if (filters.createdFrom) count += 1;
  if (filters.createdTo) count += 1;
  return count;
}

/* ================================================================== */
/*  面包屑                                                             */
/* ================================================================== */

function FolderBreadcrumb({ folder, onNavigate }: { folder: string; onNavigate: (path: string) => void }) {
  const segments = folderSegments(folder);
  return (
    <nav aria-label="目录路径" className="flex flex-wrap items-center gap-0.5 text-xs">
      <button
        type="button" onClick={() => onNavigate("")}
        className={cn(
          "inline-flex items-center gap-1 rounded px-1.5 py-1 transition-colors hover:bg-accent",
          segments.length === 0 ? "font-medium text-foreground" : "text-muted-foreground",
        )}
      >
        <Home className="size-3" />根目录
      </button>
      {segments.map((segment, index) => (
        <React.Fragment key={segment.path}>
          <ChevronRight className="size-3 shrink-0 text-muted-foreground/50" />
          <button
            type="button" onClick={() => onNavigate(segment.path)}
            className={cn(
              "max-w-40 truncate rounded px-1.5 py-1 transition-colors hover:bg-accent",
              index === segments.length - 1 ? "font-medium text-foreground" : "text-muted-foreground",
            )}
          >
            {segment.name}
          </button>
        </React.Fragment>
      ))}
    </nav>
  );
}

/* ================================================================== */
/*  批量操作条                                                         */
/* ================================================================== */

function BatchBar({
  count, pending, canPurge, status, onClear, onRun,
}: {
  count: number; pending: boolean; canPurge: boolean; status: string;
  onClear: () => void;
  onRun: (action: StorageBatchAction) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-xs">
      <span className="font-medium">已选择 {count} 个文件</span>
      <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={onClear}>
        <X className="size-3" />取消选择
      </Button>
      <div className="flex-1" />
      {status === "deleted" ? (
        <>
          <Button variant="outline" size="sm" className="h-7 gap-1" disabled={pending} onClick={() => onRun("restore")}>
            {pending ? <Loader2 className="size-3 animate-spin" /> : <RotateCcw className="size-3" />}恢复
          </Button>
          {canPurge ? (
            <Button variant="destructive" size="sm" className="h-7 gap-1" disabled={pending} onClick={() => onRun("purge")}>
              <Trash2 className="size-3" />永久删除
            </Button>
          ) : null}
        </>
      ) : (
        <Button variant="destructive" size="sm" className="h-7 gap-1" disabled={pending} onClick={() => onRun("delete")}>
          {pending ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}移入回收站
        </Button>
      )}
    </div>
  );
}

/* ================================================================== */
/*  网格视图                                                           */
/* ================================================================== */

function GridView({
  folders, objects, selected, onEnterFolder, onToggle, onOpen,
}: {
  folders: Array<{ name: string; path: string; fileCount: number; totalSize: number }>;
  objects: StorageObject[];
  selected: Set<number>;
  onEnterFolder: (path: string) => void;
  onToggle: (id: number) => void;
  onOpen: (object: StorageObject) => void;
}) {
  return (
    <div className="grid grid-cols-2 gap-2 p-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6">
      {folders.map((item) => (
        <button
          key={`folder:${item.path}`} type="button" onClick={() => onEnterFolder(item.path)}
          className="flex flex-col items-center gap-1.5 rounded-lg border border-dashed p-3 text-center transition-colors hover:border-solid hover:bg-accent/40"
        >
          <FolderIcon className="size-8" />
          <span className="w-full truncate text-xs font-medium">{item.name}</span>
          <span className="text-[10px] text-muted-foreground">{item.fileCount} 项 · {formatBytes(item.totalSize)}</span>
        </button>
      ))}

      {objects.map((object) => {
        const isSelected = selected.has(object.id);
        return (
          <div
            key={object.id}
            className={cn(
              "group relative overflow-hidden rounded-lg border transition-colors",
              isSelected ? "border-primary bg-primary/5" : "hover:bg-accent/30",
            )}
          >
            <div
              className={cn(
                "absolute left-1.5 top-1.5 z-10 transition-opacity",
                isSelected ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus-within:opacity-100",
              )}
            >
              <Checkbox
                checked={isSelected}
                onCheckedChange={() => onToggle(object.id)}
                aria-label={`选择 ${object.fileName || object.objectKey}`}
                className="bg-background/90 backdrop-blur"
              />
            </div>
            <button type="button" onClick={() => onOpen(object)} className="block w-full text-left">
              <FileThumbnail object={object} size={256} className="aspect-square w-full" />
              <div className="space-y-0.5 px-2 py-1.5">
                <div className="flex items-center gap-1">
                  <FileKindIcon contentType={object.contentType} className="size-3" />
                  <span className="min-w-0 flex-1 truncate text-[11px] font-medium">
                    {object.fileName || baseName(object.objectKey)}
                  </span>
                </div>
                <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                  <span className="font-mono">{formatBytes(object.size)}</span>
                  <span>{formatDate(object.createdAt)}</span>
                </div>
              </div>
            </button>
            {object.status !== "active" ? (
              <div className="absolute right-1.5 top-1.5"><ObjectStatusBadge status={object.status} /></div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

/* ================================================================== */
/*  列表视图                                                           */
/* ================================================================== */

function ListView({
  folders, objects, selected, allSelected, onEnterFolder, onToggle, onToggleAll, onOpen,
}: {
  folders: Array<{ name: string; path: string; fileCount: number; totalSize: number }>;
  objects: StorageObject[];
  selected: Set<number>;
  allSelected: boolean;
  onEnterFolder: (path: string) => void;
  onToggle: (id: number) => void;
  onToggleAll: () => void;
  onOpen: (object: StorageObject) => void;
}) {
  return (
    <div>
      <div className="flex items-center gap-3 border-b px-3 py-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        <Checkbox
          checked={allSelected} onCheckedChange={onToggleAll} aria-label="全选本页"
          disabled={objects.length === 0}
        />
        <span className="flex-1">名称</span>
        <span className="hidden w-24 text-right lg:block">类型</span>
        <span className="w-20 text-right">大小</span>
        <span className="hidden w-24 text-right sm:block">上传时间</span>
      </div>

      <div className="divide-y">
        {folders.map((item) => (
          <button
            key={`folder:${item.path}`} type="button" onClick={() => onEnterFolder(item.path)}
            className="flex w-full items-center gap-3 px-3 py-2 text-left text-xs transition-colors hover:bg-accent/40"
          >
            <span className="size-4 shrink-0" aria-hidden />
            <FolderIcon />
            <span className="min-w-0 flex-1 truncate font-medium">{item.name}</span>
            <span className="hidden w-24 text-right text-muted-foreground lg:block">目录</span>
            <span className="w-20 text-right font-mono text-muted-foreground">{formatBytes(item.totalSize)}</span>
            <span className="hidden w-24 text-right text-muted-foreground sm:block">{item.fileCount} 项</span>
          </button>
        ))}

        {objects.map((object) => {
          const isSelected = selected.has(object.id);
          return (
            <div
              key={object.id}
              className={cn("flex items-center gap-3 px-3 py-2 text-xs transition-colors", isSelected ? "bg-primary/5" : "hover:bg-accent/30")}
            >
              <Checkbox
                checked={isSelected} onCheckedChange={() => onToggle(object.id)}
                aria-label={`选择 ${object.fileName || object.objectKey}`}
              />
              <FileKindIcon contentType={object.contentType} />
              <button type="button" onClick={() => onOpen(object)} className="min-w-0 flex-1 text-left">
                <div className="flex items-center gap-1.5">
                  <span className="min-w-0 truncate font-medium transition-colors hover:text-primary">
                    {object.fileName || baseName(object.objectKey)}
                  </span>
                  {object.status !== "active" ? <ObjectStatusBadge status={object.status} /> : null}
                  <MasqueradeBadge metadata={object.metadata} />
                </div>
                <div className="truncate font-mono text-[10px] text-muted-foreground">{object.objectKey}</div>
              </button>
              <span className="hidden w-24 truncate text-right text-[10px] text-muted-foreground lg:block">
                {object.contentType || "--"}
              </span>
              <span className="w-20 shrink-0 text-right font-mono text-muted-foreground">{formatBytes(object.size)}</span>
              <span className="hidden w-24 shrink-0 text-right text-muted-foreground sm:block">{formatDate(object.createdAt)}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ================================================================== */
/*  状态与分页                                                         */
/* ================================================================== */

function FileSkeleton({ view }: { view: ViewMode }) {
  if (view === "grid") {
    return (
      <div className="grid grid-cols-2 gap-2 p-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6">
        {Array.from({ length: 12 }).map((_, index) => <Skeleton key={index} className="aspect-[4/5] rounded-lg" />)}
      </div>
    );
  }
  return (
    <div className="divide-y">
      {Array.from({ length: 8 }).map((_, index) => (
        <div key={index} className="flex items-center gap-3 px-3 py-2.5">
          <Skeleton className="size-4 rounded" />
          <Skeleton className="h-4 flex-1 rounded" />
          <Skeleton className="h-4 w-16 rounded" />
        </div>
      ))}
    </div>
  );
}

function EmptyBrowser({ hasFilters, folder }: { hasFilters: boolean; folder: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-1.5 py-20 text-center">
      {hasFilters ? <Filter className="size-8 text-muted-foreground/30" /> : <FolderOpen className="size-8 text-muted-foreground/30" />}
      <p className="text-sm font-medium text-muted-foreground">
        {hasFilters ? "没有匹配的文件" : folder ? "这个目录是空的" : "还没有任何文件"}
      </p>
      <p className="text-xs text-muted-foreground/60">
        {hasFilters ? "调整筛选条件或清空搜索关键字" : "应用上传文件后会自动索引到这里"}
      </p>
    </div>
  );
}

function Pagination({
  page, totalPages, total, fetching, onPrev, onNext,
}: {
  page: number; totalPages: number; total: number; fetching: boolean;
  onPrev: () => void; onNext: () => void;
}) {
  return (
    <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
      <span className="flex items-center gap-1.5">
        共 {total.toLocaleString("zh-CN")} 个文件
        {fetching ? <Loader2 className="size-3 animate-spin" /> : null}
      </span>
      <div className="flex items-center gap-1.5">
        <Button variant="outline" size="sm" className="h-6 gap-0.5 px-1.5 text-[10px]" disabled={page <= 1} onClick={onPrev}>
          <ChevronLeft className="size-3" />上一页
        </Button>
        <span className="tabular-nums">{page} / {totalPages}</span>
        <Button variant="outline" size="sm" className="h-6 gap-0.5 px-1.5 text-[10px]" disabled={page >= totalPages} onClick={onNext}>
          下一页<ChevronRight className="size-3" />
        </Button>
      </div>
    </div>
  );
}
