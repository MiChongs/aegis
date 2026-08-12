"use client";

import { useMemo, useState } from "react";
import Image from "next/image";
import { Reorder, useDragControls } from "motion/react";
import { toast } from "sonner";
import {
  GripVertical,
  Link2,
  MousePointerClick,
  Pencil,
  Plus,
  Search,
  Trash2
} from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { BannerItem } from "@/lib/api/types";
import {
  useAdminBannersQuery,
  useDeleteAdminBannerMutation,
  useReorderAdminBannersMutation,
  useUpdateAdminBannerMutation
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
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { BannerEditor } from "./banner-editor";
import { BannerPreview } from "./banner-preview";
import {
  BANNER_SLOTS,
  BannerThumbFallback,
  ScheduleBadge,
  bannerPreviewSrc,
  bannerSlot,
  clickRate,
  formatCount,
  formatWindow,
  resolveSchedule
} from "./content-shared";

/**
 * Banner 面板。
 *
 * 两条形状上的取舍：
 *
 *  1. **按展示位分组浏览，不做成一张大列表。** 五个展示位在客户端上是五个
 *     完全不同的位置，混在一起排序毫无意义 —— 首页轮播的第 2 条和开屏的
 *     第 2 条之间没有任何先后关系。
 *  2. **顺序靠拖，不靠填数字。** 旧版是一个「排序」数字输入框：要把第 5 条
 *     提到最前，得挨个改五条记录的数字，还得自己算避开重复值。
 */
export function BannerPanel({ appId }: { appId?: number | null }) {
  const [slot, setSlot] = useState(BANNER_SLOTS[0].value);
  const [keywordInput, setKeywordInput] = useState("");
  const keyword = useDebouncedValue(keywordInput, 300);
  const [editing, setEditing] = useState<{ open: boolean; item?: BannerItem | null }>({ open: false });

  const query = useAdminBannersQuery(appId, { keyword: keyword || undefined });
  const all = useMemo(() => query.data ?? [], [query.data]);
  const items = useMemo(() => all.filter((item) => (item.type ?? "hero") === slot), [all, slot]);

  const reorderMutation = useReorderAdminBannersMutation(appId);
  // 拖拽期间用本地顺序渲染，松手后写回服务端。不这么做的话每一帧都要等一次
  // 网络往返，列表会在手指底下反复回弹。
  const [dragOrder, setDragOrder] = useState<number[] | null>(null);
  const ordered = useMemo(() => {
    if (!dragOrder) return items;
    const index = new Map(items.map((item) => [item.id, item]));
    const sorted = dragOrder.map((id) => index.get(id)).filter(Boolean) as BannerItem[];
    return sorted.length === items.length ? sorted : items;
  }, [items, dragOrder]);

  async function commitOrder(ids: number[]) {
    setDragOrder(ids);
    try {
      // 只提交当前展示位的顺序会把其它位的 position 一起洗掉 ——
      // 后端按数组下标重写次序，没被提交的那些会保留旧值并与新值撞车。
      // 因此把其它位原样拼在后面，一次提交全量顺序。
      const rest = all.filter((item) => (item.type ?? "hero") !== slot).map((item) => item.id);
      await reorderMutation.mutateAsync([...ids, ...rest]);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "排序保存失败");
    } finally {
      setDragOrder(null);
    }
  }

  return (
    <div className="space-y-4">
      <BannerPreview items={all.filter((item) => (item.type ?? "hero") === slot)} slot={slot} />

      <div className="flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap items-center gap-1 rounded-lg border bg-card p-1">
          {BANNER_SLOTS.map((option) => {
            const Icon = option.icon;
            const count = all.filter((item) => (item.type ?? "hero") === option.value).length;
            return (
              <button
                key={option.value}
                type="button"
                onClick={() => setSlot(option.value)}
                className={cn(
                  "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs transition-colors",
                  slot === option.value
                    ? "bg-accent font-medium text-accent-foreground"
                    : "text-muted-foreground hover:bg-accent/50"
                )}
              >
                <Icon className="size-3.5" />
                {option.label}
                {count > 0 ? <span className="tabular-nums text-muted-foreground">{count}</span> : null}
              </button>
            );
          })}
        </div>

        <div className="relative ml-auto">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 w-52 pl-8 text-xs"
            value={keywordInput}
            onChange={(event) => setKeywordInput(event.target.value)}
            placeholder="搜索标题或链接"
          />
        </div>
        <Button size="sm" onClick={() => setEditing({ open: true, item: null })} disabled={!appId}>
          <Plus className="size-3.5" />
          新建
        </Button>
      </div>

      {query.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((key) => (
            <Skeleton key={key} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : !ordered.length ? (
        <EmptyState
          title={`${bannerSlot(slot).label}还没有素材`}
          description={keyword ? "没有匹配的 Banner，换个关键词试试" : `${bannerSlot(slot).hint}。点「新建」放一条上去。`}
        />
      ) : (
        <Reorder.Group
          as="div"
          axis="y"
          values={ordered.map((item) => item.id)}
          onReorder={(ids) => setDragOrder(ids as number[])}
          className="flex flex-col gap-2"
        >
          {ordered.map((item, index) => (
            <BannerRow
              key={item.id}
              item={item}
              index={index}
              appId={appId}
              onEdit={() => setEditing({ open: true, item })}
              onDragEnd={() => void commitOrder(ordered.map((entry) => entry.id))}
            />
          ))}
        </Reorder.Group>
      )}

      {editing.open ? (
        <BannerEditor
          // key 让抽屉在切换编辑对象时整体重挂载，表单状态因此不需要任何同步 effect
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

function BannerRow({
  item,
  index,
  appId,
  onEdit,
  onDragEnd
}: {
  item: BannerItem;
  index: number;
  appId?: number | null;
  onEdit: () => void;
  onDragEnd: () => void;
}) {
  const controls = useDragControls();
  const updateMutation = useUpdateAdminBannerMutation(appId);
  const deleteMutation = useDeleteAdminBannerMutation(appId);

  const slot = bannerSlot(item.type);
  const schedule = resolveSchedule(item.status, item.startTime, item.endTime);
  const rate = clickRate(item.viewCount, item.clickCount);
  const src = bannerPreviewSrc(item);

  async function toggle(next: boolean) {
    try {
      await updateMutation.mutateAsync({ bannerId: item.id, data: { status: next } });
      toast.success(next ? "已启用" : "已停用");
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
    <Reorder.Item
      as="div"
      value={item.id}
      dragListener={false}
      dragControls={controls}
      onDragEnd={onDragEnd}
      className="group flex items-center gap-3 rounded-xl border bg-card px-3 py-2.5"
    >
      <button
        type="button"
        aria-label="拖动排序"
        onPointerDown={(event) => controls.start(event)}
        className="flex size-6 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground/40 transition-colors hover:text-muted-foreground active:cursor-grabbing"
      >
        <GripVertical className="size-4" />
      </button>

      <span className="w-5 shrink-0 text-center text-xs tabular-nums text-muted-foreground">{index + 1}</span>

      <div className="relative h-12 w-20 shrink-0 overflow-hidden rounded-md border bg-muted">
        {src ? (
          <Image src={src} alt={item.title || ""} fill unoptimized sizes="80px" className="object-cover" />
        ) : (
          <BannerThumbFallback slot={slot} />
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{item.title || "未命名"}</span>
          <ScheduleBadge meta={schedule} />
        </div>
        <div className="mt-0.5 flex items-center gap-3 text-[11px] text-muted-foreground">
          <span>{formatWindow(item.startTime, item.endTime)}</span>
          {item.url ? (
            <span className="flex min-w-0 items-center gap-1">
              <Link2 className="size-3 shrink-0" />
              <span className="truncate">{item.url}</span>
            </span>
          ) : (
            <span>无跳转</span>
          )}
        </div>
      </div>

      <Tooltip>
        <TooltipTrigger asChild>
          <div className="hidden shrink-0 items-center gap-3 px-2 text-xs sm:flex">
            <div className="text-right">
              <div className="tabular-nums">{formatCount(item.viewCount)}</div>
              <div className="text-[10px] text-muted-foreground">曝光</div>
            </div>
            <div className="text-right">
              <div className="tabular-nums">{formatCount(item.clickCount)}</div>
              <div className="text-[10px] text-muted-foreground">点击</div>
            </div>
            <Badge variant={rate ? "info" : "outline"} size="sm" className="gap-1">
              <MousePointerClick className="size-3" />
              {rate ?? "—"}
            </Badge>
          </div>
        </TooltipTrigger>
        <TooltipContent>
          {rate
            ? `点击率 ${rate}`
            : "还没有曝光，点击率无从计算"}
        </TooltipContent>
      </Tooltip>

      <div className="flex shrink-0 items-center gap-1">
        <Switch
          checked={item.status !== false}
          onCheckedChange={(next) => void toggle(next)}
          aria-label="投放开关"
        />
        <Button variant="ghost" size="icon" className="size-8" onClick={onEdit} aria-label="编辑">
          <Pencil className="size-3.5" />
        </Button>
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 text-destructive" aria-label="删除">
              <Trash2 className="size-3.5" />
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>删除「{item.title || "未命名"}」</AlertDialogTitle>
              <AlertDialogDescription>
                删除后客户端将不再拉到这条 Banner，曝光与点击记录一并消失。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <AlertDialogAction onClick={() => void remove()}>删除</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </Reorder.Item>
  );
}
