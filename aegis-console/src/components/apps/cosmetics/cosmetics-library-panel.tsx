"use client";

import { useMemo, useState } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent
} from "@dnd-kit/core";
import { SortableContext, arrayMove, rectSortingStrategy, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  Archive,
  ArchiveRestore,
  Copy,
  EyeOff,
  FilePen,
  GripVertical,
  MoreHorizontal,
  Paintbrush,
  PencilLine,
  Plus,
  Shirt,
  Trash2,
  Undo2,
  Users
} from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  FilterBar,
  FilterSelect,
  ResetButton,
  SearchField,
  StateBlock,
  ToneIcon,
  errorMessage,
  useDebouncedValue
} from "@/components/users/detail/kit";
import { ApiError } from "@/lib/api/client";
import type { CosmeticCatalog, CosmeticItem, CosmeticSlot, CosmeticStatus } from "@/lib/api/cosmetics";
import {
  useCosmeticItemsQuery,
  useDeleteCosmeticItemMutation,
  useDuplicateCosmeticItemMutation,
  useReorderCosmeticItemsMutation,
  useSetCosmeticStatusMutation
} from "@/lib/cosmetic-hooks";
import { cn } from "@/lib/utils";
import { CosmeticItemDialog } from "./cosmetic-item-dialog";
import { CosmeticItemPreview } from "./cosmetic-preview";
import {
  RarityBadge,
  SLOT_META,
  SLOT_ORDER,
  STATUS_META,
  StatusBadge,
  formatCount,
  raritiesOf,
  rarityOf,
  slotLabel,
  unlockLabel
} from "./cosmetic-shared";

export type LibraryFilter = { slot?: CosmeticSlot; dirtyOnly?: boolean };

/**
 * 装扮库：按槽位分组的卡片网格。卡片上实时播放已发布的设计稿（没发布过画草稿），
 * 稀有度色条、状态、未发布修改、持有与佩戴人数；拖动卡片调整同一槽位内的顺序（只在没有状态 / 稀有度 /
 * 关键词筛选时可拖：筛掉一部分再排，没看见的那些会被挤到奇怪的位置）。
 */
export function CosmeticsLibraryPanel({
  appKey,
  catalog,
  can,
  initialFilter,
  onOpenDesigner,
  onOpenOwnerships
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  can: { write: boolean; delete: boolean; publish: boolean };
  initialFilter?: LibraryFilter;
  onOpenDesigner: (itemId: number) => void;
  onOpenOwnerships: (cosmeticId: number) => void;
}) {
  const [slot, setSlot] = useState<string>(initialFilter?.slot ?? "");
  const [status, setStatus] = useState("");
  const [rarity, setRarity] = useState("");
  const [keyword, setKeyword] = useState("");
  const [dirtyOnly, setDirtyOnly] = useState(Boolean(initialFilter?.dirtyOnly));
  const debounced = useDebouncedValue(keyword.trim());
  const itemsQuery = useCosmeticItemsQuery(appKey, { slot, status, rarity, keyword: debounced });

  const [editing, setEditing] = useState<{ item: CosmeticItem | null; slot: CosmeticSlot; key: number } | null>(null);
  const [deleting, setDeleting] = useState<CosmeticItem | null>(null);
  const [archiveInstead, setArchiveInstead] = useState<CosmeticItem | null>(null);
  const [order, setOrder] = useState<Record<string, number[]>>({});

  const remove = useDeleteCosmeticItemMutation(appKey);
  const duplicate = useDuplicateCosmeticItemMutation(appKey);
  const setItemStatus = useSetCosmeticStatusMutation(appKey);
  const reorder = useReorderCosmeticItemsMutation(appKey);

  const items = useMemo(() => {
    const list = itemsQuery.data ?? [];
    return dirtyOnly ? list.filter((item) => item.draftDirty || item.revision === 0) : list;
  }, [itemsQuery.data, dirtyOnly]);
  const sortable = !status && !rarity && !debounced && !dirtyOnly && can.write;

  const groups = useMemo(() => {
    return SLOT_ORDER.map((key) => {
      const inSlot = items.filter((item) => item.slot === key);
      const local = order[key];
      const sorted = local
        ? [...inSlot].sort((a, b) => {
            const ia = local.indexOf(a.id);
            const ib = local.indexOf(b.id);
            return (ia < 0 ? Number.MAX_SAFE_INTEGER : ia) - (ib < 0 ? Number.MAX_SAFE_INTEGER : ib);
          })
        : inSlot;
      return { slot: key, items: sorted };
    }).filter((group) => group.items.length > 0 || (slot && group.slot === slot));
  }, [items, order, slot]);

  const filtersActive = Boolean(slot || status || rarity || keyword || dirtyOnly);
  const resetFilters = () => {
    setSlot("");
    setStatus("");
    setRarity("");
    setKeyword("");
    setDirtyOnly(false);
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const onDragEnd = (group: CosmeticSlot, ids: number[]) => async (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const next = arrayMove(ids, ids.indexOf(Number(active.id)), ids.indexOf(Number(over.id)));
    setOrder((current) => ({ ...current, [group]: next }));
    try {
      await reorder.mutateAsync(next);
      toast.success("排序已保存");
    } catch (error) {
      toast.error(errorMessage(error, "排序保存失败"));
      setOrder((current) => {
        const copy = { ...current };
        delete copy[group];
        return copy;
      });
    }
  };

  const changeStatus = async (item: CosmeticItem, next: CosmeticStatus) => {
    try {
      await setItemStatus.mutateAsync({ itemId: item.id, status: next });
      toast.success(next === "active" ? `「${item.name}」已上架` : next === "archived" ? `「${item.name}」已下架` : `「${item.name}」已转为草稿`);
      setArchiveInstead(null);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  const doDuplicate = async (item: CosmeticItem) => {
    try {
      const copy = await duplicate.mutateAsync(item.id);
      toast.success(`已复制为「${copy.name}」`);
    } catch (error) {
      toast.error(errorMessage(error, "复制失败"));
    }
  };

  const askDelete = (item: CosmeticItem) => {
    if ((item.owners ?? 0) > 0) setArchiveInstead(item);
    else setDeleting(item);
  };

  const doDelete = async () => {
    if (!deleting) return;
    const target = deleting;
    try {
      await remove.mutateAsync(target.id);
      toast.success(`已删除「${target.name}」`);
      setDeleting(null);
    } catch (error) {
      setDeleting(null);
      if (error instanceof ApiError && error.code === 40956) setArchiveInstead(target);
      else toast.error(errorMessage(error, "删除失败"));
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <Card className="gap-0 py-0">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-5">
          <FilterBar>
            <FilterSelect
              value={slot}
              onChange={setSlot}
              label="槽位"
              options={SLOT_ORDER.map((key) => ({ value: key, label: slotLabel(key, catalog) }))}
            />
            <FilterSelect
              value={status}
              onChange={setStatus}
              label="状态"
              options={(Object.keys(STATUS_META) as CosmeticStatus[]).map((key) => ({ value: key, label: STATUS_META[key].label }))}
            />
            <FilterSelect
              value={rarity}
              onChange={setRarity}
              label="稀有度"
              options={raritiesOf(catalog).map((spec) => ({ value: spec.key, label: spec.label }))}
            />
            <SearchField value={keyword} onChange={setKeyword} placeholder="名称、编码、系列" />
            <label className="flex h-8 cursor-pointer items-center gap-2 rounded-md border px-2.5 text-sm">
              <Switch checked={dirtyOnly} onCheckedChange={setDirtyOnly} className="scale-90" />
              <span className="text-muted-foreground">只看未发布</span>
            </label>
            <ResetButton visible={filtersActive} onClick={resetFilters} />
          </FilterBar>
          {can.write ? (
            <Button size="sm" onClick={() => setEditing({ item: null, slot: (slot as CosmeticSlot) || "avatar_frame", key: Date.now() })}>
              <Plus />
              新建装扮
            </Button>
          ) : null}
        </div>
      </Card>

      {itemsQuery.isLoading ? (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5">
          {Array.from({ length: 8 }, (_, index) => (
            <Skeleton key={index} className="h-[244px] rounded-xl" />
          ))}
        </div>
      ) : itemsQuery.isError && !itemsQuery.data ? (
        <Card className="py-0">
          <StateBlock kind="error" description={errorMessage(itemsQuery.error)} onRetry={() => void itemsQuery.refetch()} />
        </Card>
      ) : groups.length === 0 ? (
        <Card className="py-0">
          <StateBlock
            icon={Shirt}
            title={filtersActive ? "没有符合条件的装扮" : "暂无装扮"}
            action={
              !filtersActive && can.write ? (
                <Button size="sm" onClick={() => setEditing({ item: null, slot: "avatar_frame", key: Date.now() })}>
                  <Plus />
                  新建装扮
                </Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        groups.map((group) => {
          const meta = SLOT_META[group.slot];
          const ids = group.items.map((item) => item.id);
          return (
            <section key={group.slot} className="flex flex-col gap-3">
              <div className="flex items-center gap-2.5">
                <ToneIcon icon={meta.icon} tone={meta.tone} size="sm" />
                <h3 className="text-sm font-semibold">{slotLabel(group.slot, catalog)}</h3>
                <span className="text-xs tabular-nums text-muted-foreground">{group.items.length}</span>
                {can.write ? (
                  <Button
                    size="xs"
                    variant="ghost"
                    className="ml-auto text-muted-foreground"
                    onClick={() => setEditing({ item: null, slot: group.slot, key: Date.now() })}
                  >
                    <Plus />
                    新建
                  </Button>
                ) : null}
              </div>
              {group.items.length === 0 ? (
                <Card className="py-0">
                  <StateBlock compact title={`暂无${slotLabel(group.slot, catalog)}`} />
                </Card>
              ) : (
                <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd(group.slot, ids)}>
                  <SortableContext items={ids} strategy={rectSortingStrategy} disabled={!sortable}>
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5">
                      {group.items.map((item) => (
                        <ItemCard
                          key={item.id}
                          appKey={appKey}
                          item={item}
                          catalog={catalog}
                          can={can}
                          sortable={sortable}
                          onDesign={() => onOpenDesigner(item.id)}
                          onEdit={() => setEditing({ item, slot: item.slot, key: Date.now() })}
                          onDuplicate={() => void doDuplicate(item)}
                          onStatus={(next) => void changeStatus(item, next)}
                          onDelete={() => askDelete(item)}
                          onOwnerships={() => onOpenOwnerships(item.id)}
                        />
                      ))}
                    </div>
                  </SortableContext>
                </DndContext>
              )}
            </section>
          );
        })
      )}

      {editing ? (
        <CosmeticItemDialog
          key={editing.key}
          open
          onOpenChange={(open) => !open && setEditing(null)}
          appKey={appKey}
          catalog={catalog}
          item={editing.item}
          defaultSlot={editing.slot}
          onCreated={(item, openDesigner) => {
            if (openDesigner) onOpenDesigner(item.id);
          }}
        />
      ) : null}

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && !remove.isPending && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{deleting?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>设计稿与全部版本一并删除，不可恢复。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <Button variant="destructive" onClick={() => void doDelete()} disabled={remove.isPending}>
              删除
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={Boolean(archiveInstead)} onOpenChange={(open) => !open && setArchiveInstead(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>「{archiveInstead?.name}」不能删除</AlertDialogTitle>
            <AlertDialogDescription>已有用户持有过这件装扮。下架后用户看不到它，已持有的也不能再戴。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            {archiveInstead && archiveInstead.status !== "archived" && can.write ? (
              <Button onClick={() => void changeStatus(archiveInstead, "archived")} disabled={setItemStatus.isPending}>
                <Archive />
                下架
              </Button>
            ) : null}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ItemCard({
  appKey,
  item,
  catalog,
  can,
  sortable,
  onDesign,
  onEdit,
  onDuplicate,
  onStatus,
  onDelete,
  onOwnerships
}: {
  appKey: string;
  item: CosmeticItem;
  catalog?: CosmeticCatalog | null;
  can: { write: boolean; delete: boolean; publish: boolean };
  sortable: boolean;
  onDesign: () => void;
  onEdit: () => void;
  onDuplicate: () => void;
  onStatus: (status: CosmeticStatus) => void;
  onDelete: () => void;
  onOwnerships: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: item.id,
    disabled: !sortable
  });
  const rarity = rarityOf(item.rarity, catalog);
  const unpublished = item.revision === 0;

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn("group relative", isDragging && "z-10 opacity-80")}
    >
      <Card className="gap-0 overflow-hidden py-0 transition-shadow hover:shadow-sm">
        <div className="h-1" style={{ backgroundColor: rarity.color }} aria-hidden />
        <button
          type="button"
          onClick={onDesign}
          className="relative flex h-36 items-center justify-center bg-muted/40 transition-colors hover:bg-muted/70"
          aria-label={`打开「${item.name}」的设计器`}
        >
          <CosmeticItemPreview
            appKey={appKey}
            item={item}
            width={item.slot === "profile_card" || item.slot === "name_style" ? 200 : 120}
            height={item.slot === "profile_card" ? 112 : 120}
          />
          <span className="absolute top-2 left-2 flex flex-wrap gap-1">
            <StatusBadge status={item.status} />
            {item.draftDirty && !unpublished ? (
              <Badge variant="warning" size="sm">
                未发布修改
              </Badge>
            ) : null}
            {unpublished ? (
              <Badge variant="outline" size="sm" className="bg-background">
                未发布
              </Badge>
            ) : null}
          </span>
          {item.hidden ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="absolute top-2 right-2 inline-flex size-6 items-center justify-center rounded-md bg-background/90 text-muted-foreground">
                  <EyeOff className="size-3.5" />
                </span>
              </TooltipTrigger>
              <TooltipContent>不在装扮中心列出</TooltipContent>
            </Tooltip>
          ) : null}
        </button>
        <div className="flex flex-col gap-2 border-t px-3 py-2.5">
          <div className="flex items-start gap-1.5">
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-sm font-medium">{item.name}</span>
              <span className="truncate font-mono text-[11px] text-muted-foreground">{item.code}</span>
            </div>
            {sortable ? (
              <button
                type="button"
                ref={setActivatorNodeRef}
                {...attributes}
                {...listeners}
                className="inline-flex size-6 shrink-0 cursor-grab items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 hover:bg-accent focus-visible:opacity-100 active:cursor-grabbing"
                aria-label="拖动排序"
              >
                <GripVertical className="size-3.5" />
              </button>
            ) : null}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="icon-xs" variant="ghost" aria-label="更多操作">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                <DropdownMenuItem onSelect={onDesign}>
                  <Paintbrush />
                  {can.write ? "设计" : "查看设计"}
                </DropdownMenuItem>
                {can.write ? (
                  <DropdownMenuItem onSelect={onEdit}>
                    <PencilLine />
                    编辑信息
                  </DropdownMenuItem>
                ) : null}
                {can.write ? (
                  <DropdownMenuItem onSelect={onDuplicate}>
                    <Copy />
                    复制
                  </DropdownMenuItem>
                ) : null}
                <DropdownMenuItem onSelect={onOwnerships}>
                  <Users />
                  持有记录
                </DropdownMenuItem>
                {can.write ? <DropdownMenuSeparator /> : null}
                {can.write && item.status !== "active" ? (
                  <DropdownMenuItem disabled={unpublished} onSelect={() => onStatus("active")}>
                    <ArchiveRestore />
                    {unpublished ? "发布后才能上架" : "上架"}
                  </DropdownMenuItem>
                ) : null}
                {can.write && item.status === "active" ? (
                  <DropdownMenuItem onSelect={() => onStatus("archived")}>
                    <Archive />
                    下架
                  </DropdownMenuItem>
                ) : null}
                {can.write && item.status !== "draft" ? (
                  <DropdownMenuItem onSelect={() => onStatus("draft")}>
                    <Undo2 />
                    转为草稿
                  </DropdownMenuItem>
                ) : null}
                {can.delete ? (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                      <Trash2 />
                      删除
                    </DropdownMenuItem>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <div className="flex flex-wrap items-center gap-1">
            <RarityBadge rarity={item.rarity} catalog={catalog} />
            <Badge variant="outline" size="sm" className="font-normal text-muted-foreground">
              {unlockLabel(item, catalog)}
            </Badge>
            {item.revision > 0 ? (
              <Badge variant="outline" size="sm" className="font-normal text-muted-foreground tabular-nums">
                <FilePen />v{item.revision}
              </Badge>
            ) : null}
          </div>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <button type="button" onClick={onOwnerships} className="tabular-nums hover:text-foreground">
              <span className="font-medium text-foreground">{formatCount(item.owners)}</span> 持有
            </button>
            <span className="tabular-nums">
              <span className="font-medium text-foreground">{formatCount(item.equipped)}</span> 佩戴
            </span>
            {item.series ? <span className="ml-auto truncate">{item.series}</span> : null}
          </div>
        </div>
      </Card>
    </div>
  );
}
