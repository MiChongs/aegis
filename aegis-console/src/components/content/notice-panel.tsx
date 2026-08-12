"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  Archive,
  ChevronLeft,
  ChevronRight,
  Eye,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Rocket,
  Search,
  Trash2
} from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { NoticeItem } from "@/lib/api/types";
import {
  useAdminNoticesQuery,
  useDeleteAdminNoticeMutation,
  useUpdateAdminNoticeMutation
} from "@/lib/content-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { NoticeEditor } from "./notice-editor";
import {
  NOTICE_LEVELS,
  NOTICE_STATUSES,
  NOTICE_TYPES,
  ScheduleBadge,
  formatCount,
  formatDateTime,
  formatWindow,
  noticeLevel,
  noticeStatus,
  noticeType,
  resolveNoticeSchedule
} from "./content-shared";

const PAGE_SIZE = 12;

/**
 * 公告面板。
 *
 * 过滤在**服务端**：公告会持续累积，运营两年的应用有几千条，
 * 全量拉回来在前端筛既慢也会让分页总数说谎。
 *
 * 任何一项筛选变化都回到第 1 页 —— 不这么做会停在新条件下不存在的页码上，
 * 表现为「明明有数据却是空的」。
 */
export function NoticePanel({ appId }: { appId?: number | null }) {
  const [status, setStatus] = useState("all");
  const [type, setType] = useState("all");
  const [level, setLevel] = useState("all");
  const [keywordInput, setKeywordInput] = useState("");
  const [page, setPage] = useState(1);
  const keyword = useDebouncedValue(keywordInput, 300);
  const [editing, setEditing] = useState<{ open: boolean; item?: NoticeItem | null }>({ open: false });

  const query = useAdminNoticesQuery(appId, {
    status: status === "all" ? undefined : status,
    type: type === "all" ? undefined : type,
    level: level === "all" ? undefined : level,
    keyword: keyword || undefined,
    page,
    limit: PAGE_SIZE
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  function reset<T>(setter: (value: T) => void) {
    return (value: T) => {
      setter(value);
      setPage(1);
    };
  }

  const filterActive = status !== "all" || type !== "all" || level !== "all" || Boolean(keyword);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={status} onValueChange={reset(setStatus)}>
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {NOTICE_STATUSES.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={type} onValueChange={reset(setType)}>
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部类型</SelectItem>
            {NOTICE_TYPES.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={level} onValueChange={reset(setLevel)}>
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部级别</SelectItem>
            {NOTICE_LEVELS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="relative ml-auto">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 w-52 pl-8 text-xs"
            value={keywordInput}
            onChange={(event) => {
              setKeywordInput(event.target.value);
              setPage(1);
            }}
            placeholder="搜索标题或摘要"
          />
        </div>
        <Button size="sm" onClick={() => setEditing({ open: true, item: null })} disabled={!appId}>
          <Plus className="size-3.5" />
          新建
        </Button>
      </div>

      {query.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3].map((key) => (
            <Skeleton key={key} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : !items.length ? (
        <EmptyState
          title={filterActive ? "没有匹配的公告" : "还没有公告"}
          description={filterActive ? "换个筛选条件试试" : "公告发布后会随客户端的公告接口下发。"}
        />
      ) : (
        <div className="flex flex-col gap-2">
          {items.map((item) => (
            <NoticeRow key={item.id} item={item} appId={appId} onEdit={() => setEditing({ open: true, item })} />
          ))}
        </div>
      )}

      {total > 0 ? (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>共 {total} 条</span>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon"
              className="size-7"
              disabled={page <= 1}
              onClick={() => setPage((value) => value - 1)}
              aria-label="上一页"
            >
              <ChevronLeft className="size-3.5" />
            </Button>
            <span className="tabular-nums">
              {page} / {totalPages}
            </span>
            <Button
              variant="outline"
              size="icon"
              className="size-7"
              disabled={page >= totalPages}
              onClick={() => setPage((value) => value + 1)}
              aria-label="下一页"
            >
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        </div>
      ) : null}

      {editing.open ? (
        <NoticeEditor
          key={editing.item?.id ?? "create"}
          appId={appId}
          item={editing.item}
          open={editing.open}
          onOpenChange={(open) => setEditing((state) => ({ ...state, open }))}
        />
      ) : null}
    </div>
  );
}

function NoticeRow({
  item,
  appId,
  onEdit
}: {
  item: NoticeItem;
  appId?: number | null;
  onEdit: () => void;
}) {
  const updateMutation = useUpdateAdminNoticeMutation(appId);
  const deleteMutation = useDeleteAdminNoticeMutation(appId);

  const type = useMemo(() => noticeType(item.type), [item.type]);
  const level = useMemo(() => noticeLevel(item.level), [item.level]);
  const statusMeta = useMemo(() => noticeStatus(item.status), [item.status]);
  const schedule = useMemo(() => resolveNoticeSchedule(item), [item]);
  const TypeIcon = type.icon;

  async function mutate(data: Partial<NoticeItem>, message: string) {
    try {
      await updateMutation.mutateAsync({ noticeId: item.id, data });
      toast.success(message);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  async function remove() {
    try {
      await deleteMutation.mutateAsync(item.id);
      toast.success("已删除");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  return (
    <div className="group flex items-start gap-3 rounded-xl border bg-card px-4 py-3">
      <div className={cn("mt-1.5 size-2 shrink-0 rounded-full", level.dot)} />

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          {item.pinned ? <Pin className="size-3 shrink-0 text-amber-500" /> : null}
          <span className="truncate text-sm font-medium">{item.title || "未命名"}</span>
          <Badge variant="outline" size="sm" className="gap-1">
            <TypeIcon className="size-3" />
            {type.label}
          </Badge>
          <Badge variant={statusMeta.variant} size="sm">
            {statusMeta.label}
          </Badge>
          {item.status === "published" ? <ScheduleBadge meta={schedule} /> : null}
          {level.value !== "normal" ? (
            <Badge variant={level.variant} size="sm">
              {level.label}
            </Badge>
          ) : null}
        </div>

        {item.summary ? (
          <p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{item.summary}</p>
        ) : null}

        <div className="mt-1.5 flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
          <span>{formatWindow(item.startTime, item.endTime)}</span>
          <span>
            {item.status === "published" && item.publishedAt
              ? `发布于 ${formatDateTime(item.publishedAt)}`
              : `更新于 ${formatDateTime(item.updatedAt)}`}
          </span>
          <span className="flex items-center gap-1">
            <Eye className="size-3" />
            {formatCount(item.viewCount)}
          </span>
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-1">
        <Button variant="ghost" size="icon" className="size-8" onClick={onEdit} aria-label="编辑">
          <Pencil className="size-3.5" />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8" aria-label="更多操作">
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {item.status !== "published" ? (
              <DropdownMenuItem onClick={() => void mutate({ status: "published" }, "已发布")}>
                <Rocket className="mr-2 size-3.5" />
                发布
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem onClick={() => void mutate({ status: "archived" }, "已归档")}>
                <Archive className="mr-2 size-3.5" />
                归档
              </DropdownMenuItem>
            )}
            <DropdownMenuItem
              onClick={() => void mutate({ pinned: !item.pinned }, item.pinned ? "已取消置顶" : "已置顶")}
            >
              {item.pinned ? <PinOff className="mr-2 size-3.5" /> : <Pin className="mr-2 size-3.5" />}
              {item.pinned ? "取消置顶" : "置顶"}
            </DropdownMenuItem>
            {item.status === "archived" ? (
              <DropdownMenuItem onClick={() => void mutate({ status: "draft" }, "已转为草稿")}>
                <Pencil className="mr-2 size-3.5" />
                转为草稿
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuSeparator />
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <DropdownMenuItem
                  className="text-destructive focus:text-destructive"
                  onSelect={(event) => event.preventDefault()}
                >
                  <Trash2 className="mr-2 size-3.5" />
                  删除
                </DropdownMenuItem>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>删除「{item.title || "未命名"}」</AlertDialogTitle>
                  <AlertDialogDescription>
                    删除后无法恢复。只是想让它不再展示的话，归档更合适。
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>取消</AlertDialogCancel>
                  <AlertDialogAction onClick={() => void remove()}>删除</AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
