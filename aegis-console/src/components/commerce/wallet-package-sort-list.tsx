"use client";

import { useState } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowDown, ArrowDownUp, ArrowUp, GripVertical, Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { Section, errorMessage } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import type { AdminWalletPackage } from "@/lib/api/types";
import { useReorderWalletPackagesMutation } from "@/lib/commerce-hooks";
import { cn } from "@/lib/utils";
import { formatMoney } from "./commerce-format";
import { WalletPackageStatusBadge } from "./wallet-config-fields";

/**
 * 充值档位排序：拖动手柄（或聚焦后用方向键与空格）、或用上移下移按钮，保存时按新顺序整体提交。
 * 与积分商城的「调整顺序」同一套交互（points-store-sort-list）。
 */
export function WalletPackageSortList({
  appKey,
  items,
  currency,
  onDone
}: {
  appKey: string;
  items: AdminWalletPackage[];
  currency: string;
  onDone: () => void;
}) {
  const [order, setOrder] = useState(() => items.map((item) => item.id));
  const mutation = useReorderWalletPackagesMutation(appKey);
  const byId = new Map(items.map((item) => [item.id, item]));
  const rows = order.map((id) => byId.get(id)).filter((item): item is AdminWalletPackage => Boolean(item));
  const dirty = order.some((id, index) => id !== items[index]?.id);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    setOrder((current) => arrayMove(current, current.indexOf(Number(active.id)), current.indexOf(Number(over.id))));
  };

  const shift = (id: number, delta: -1 | 1) =>
    setOrder((current) => {
      const from = current.indexOf(id);
      const to = from + delta;
      return to < 0 || to >= current.length ? current : arrayMove(current, from, to);
    });

  const save = async () => {
    try {
      await mutation.mutateAsync(order);
      toast.success("排序已保存");
      onDone();
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section
      flush
      title="调整档位顺序"
      icon={ArrowDownUp}
      tone="primary"
      description="拖动手柄排序，或聚焦手柄后用方向键与空格。用户端按这个顺序展示档位。"
      actions={
        <>
          <Button variant="ghost" size="sm" onClick={onDone} disabled={mutation.isPending}>
            取消
          </Button>
          <Button size="sm" onClick={save} disabled={!dirty || mutation.isPending}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            保存顺序
          </Button>
        </>
      }
    >
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={order} strategy={verticalListSortingStrategy}>
          <ol className="flex flex-col divide-y">
            {rows.map((item, index) => (
              <SortableRow
                key={item.id}
                item={item}
                index={index}
                last={index === rows.length - 1}
                currency={currency}
                onUp={() => shift(item.id, -1)}
                onDown={() => shift(item.id, 1)}
              />
            ))}
          </ol>
        </SortableContext>
      </DndContext>
    </Section>
  );
}

function SortableRow({
  item,
  index,
  last,
  currency,
  onUp,
  onDown
}: {
  item: AdminWalletPackage;
  index: number;
  last: boolean;
  currency: string;
  onUp: () => void;
  onDown: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: item.id });
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "group flex items-center gap-3 bg-card px-3 py-2.5 sm:px-4",
        isDragging && "relative z-10 rounded-lg shadow-lg ring-1 ring-border"
      )}
    >
      <button
        ref={setActivatorNodeRef}
        type="button"
        aria-label={`拖动排序：${item.title}`}
        className="flex size-8 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:cursor-grabbing"
        {...attributes}
        {...listeners}
      >
        <GripVertical className="size-4" />
      </button>
      <span className="w-6 shrink-0 text-right text-xs tabular-nums text-muted-foreground">{index + 1}</span>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-medium">{item.title}</span>
        <span className="truncate text-xs text-muted-foreground tabular-nums">{formatMoney(item.amount, currency)}</span>
      </div>
      <WalletPackageStatusBadge item={item} />
      <div className="flex items-center gap-0.5">
        <Button variant="ghost" size="icon-sm" disabled={index === 0} onClick={onUp} aria-label="上移">
          <ArrowUp />
        </Button>
        <Button variant="ghost" size="icon-sm" disabled={last} onClick={onDown} aria-label="下移">
          <ArrowDown />
        </Button>
      </div>
    </li>
  );
}
