"use client";

import { useState } from "react";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import NumberFlow from "@number-flow/react";
import { CosmeticRewardInput } from "@/components/apps/cosmetics/cosmetic-reward";
import { CouponRewardInput } from "@/components/apps/coupons/coupon-shared";
import {
  ArrowRight,
  ChevronLeft,
  ChevronRight,
  GripVertical,
  ImageIcon,
  LifeBuoy,
  Plus,
  ShieldCheck,
  Trash2
} from "lucide-react";
import { motion } from "motion/react";
import { KindGlyph, kindVisual } from "@/components/apps/lottery/editor/prize-visuals";
import {
  Field,
  FIELD_SURFACE,
  Segmented,
  Stepper,
  TextField,
  ToggleTile
} from "@/components/apps/lottery/editor/editor-fields";
import {
  LIMITS,
  OWNABLE_KINDS,
  SWAP_KINDS,
  blankPrize,
  nextPrizeKey,
  rewardOf,
  type PrizeDraft
} from "@/components/apps/lottery/lottery-activity-form";
import { GRID_CELLS, defaultPrizeName, formatProbability } from "@/components/apps/lottery/lottery-shared";
import { termLabel } from "@/components/apps/points-store/points-store-shared";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { LotteryCatalog, LotteryUIMode } from "@/lib/api/lottery";
import { NUMBER_TICKET_LEVELS } from "@/lib/api/user-number";
import { cn } from "@/lib/utils";

/** Select 不接受空串：「按活动的处理方式」用这个占位 */
const SWAP_POLICY = "__policy";

const PERCENT_FORMAT = { minimumFractionDigits: 2, maximumFractionDigits: 2 } as const;
// 小于 1% 的按两位有效数字（与公示概率同一规则），十万分之一显示 0.001% 而不是 0.00%
const SMALL_PERCENT_FORMAT = { maximumSignificantDigits: 2 } as const;
const percentFormat = (value: number) => (value > 0 && value < 1 ? SMALL_PERCENT_FORMAT : PERCENT_FORMAT);

/**
 * 奖盘编辑：左边是盘面，右边改选中的一格。
 *
 * 九宫格是一张 3×3 的盘，外圈 8 格按顺时针从左上角起编号，中心是抽奖按钮（与 App 端同一张表，
 * 见 `GRID_CELLS`），拖一格到另一格上即交换位置；转盘是一列从顶部起顺时针的奖品，拖动排序、可增删，
 * 上方的转盘预览按同一顺序着色。两种形态共用右侧的单格表单，每格都显示按当前草稿算出的公示概率，
 * 改权重或库存立刻能看到整盘的变化。位置只是数组下标，保底按行键记，所以挪动不会丢保底。
 */
export function LotteryPrizeBoard({
  uiMode,
  prizes,
  probabilities,
  selectedKey,
  pityKey,
  catalog,
  swapPreview,
  onSelect,
  onChange
}: {
  uiMode: LotteryUIMode;
  prizes: PrizeDraft[];
  probabilities: number[];
  selectedKey: number | null;
  pityKey: number | null;
  catalog?: LotteryCatalog | null;
  /** 这一格被已拥有的人抽中时改发什么（一句话），不改发时为 null */
  swapPreview?: (prize: PrizeDraft) => string | null;
  onSelect: (key: number) => void;
  onChange: (prizes: PrizeDraft[]) => void;
}) {
  const kinds = catalog?.kinds ?? [];
  const plans = catalog?.plans ?? [];
  const features = catalog?.features ?? [];
  const wheelMin = catalog?.wheelMinPrizes ?? 2;
  const wheelMax = catalog?.wheelMaxPrizes ?? 12;

  const selectedIndex = Math.max(
    0,
    prizes.findIndex((prize) => prize.key === selectedKey)
  );
  const selected = prizes[selectedIndex];

  const displayName = (prize: PrizeDraft) =>
    prize.name.trim() || defaultPrizeName(rewardOf(prize, kinds), kinds, plans, features);

  const move = (from: number, to: number) => {
    if (to < 0 || to >= prizes.length || from === to) return;
    onChange(arrayMove(prizes, from, to));
  };

  /** 九宫格是固定位置：拖到另一格上是两格互换，而不是整体平移 */
  const swap = (from: number, to: number) => {
    if (to < 0 || to >= prizes.length || from === to) return;
    const next = [...prizes];
    [next[from], next[to]] = [next[to], next[from]];
    onChange(next);
  };

  const add = () => {
    const prize = blankPrize(nextPrizeKey(prizes), "points", 10, 10);
    onChange([...prizes, prize]);
    onSelect(prize.key);
  };

  const remove = (key: number) => {
    const index = prizes.findIndex((prize) => prize.key === key);
    const next = prizes.filter((prize) => prize.key !== key);
    onChange(next);
    const neighbour = next[Math.min(index, next.length - 1)];
    if (neighbour) onSelect(neighbour.key);
  };

  const cell = {
    prizes,
    probabilities,
    pityKey,
    selectedKey: selected?.key ?? null,
    displayName,
    onSelect
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 lg:grid-cols-[minmax(0,19rem)_minmax(0,1fr)]">
        <div className="min-w-0 space-y-3">
          {uiMode === "grid" ? (
            <GridBoard {...cell} onSwap={swap} />
          ) : (
            <WheelBoard
              {...cell}
              canAdd={prizes.length < wheelMax}
              canRemove={prizes.length > wheelMin}
              onMove={move}
              onAdd={add}
              onRemove={remove}
            />
          )}
        </div>

        {selected ? (
          <PrizeInspector
            key={selected.key}
            prize={selected}
            index={selectedIndex}
            total={prizes.length}
            uiMode={uiMode}
            probability={probabilities[selectedIndex] ?? 0}
            isPity={selected.key === pityKey}
            catalog={catalog}
            swapPreview={swapPreview}
            canRemove={uiMode === "wheel" && prizes.length > wheelMin}
            onRemove={() => remove(selected.key)}
            onNavigate={(offset) => {
              const target = prizes[(selectedIndex + offset + prizes.length) % prizes.length];
              if (target) onSelect(target.key);
            }}
            onShift={(offset) => (uiMode === "grid" ? swap : move)(selectedIndex, selectedIndex + offset)}
            onPatch={(next) => onChange(prizes.map((prize) => (prize.key === selected.key ? { ...prize, ...next } : prize)))}
            onFallback={(fallback) =>
              // 兜底奖只能有一个，且必须不限库存：勾上时顺手把其它的取消、把库存放开
              onChange(
                prizes.map((prize) =>
                  prize.key === selected.key
                    ? { ...prize, fallback, unlimited: fallback ? true : prize.unlimited }
                    : fallback
                      ? { ...prize, fallback: false }
                      : prize
                )
              )
            }
          />
        ) : (
          <div className="grid place-items-center rounded-2xl border border-dashed border-border px-4 py-12 text-xs text-muted-foreground">
            暂无奖品
          </div>
        )}
      </div>

      <DistributionBar
        prizes={prizes}
        probabilities={probabilities}
        selectedKey={selected?.key ?? null}
        displayName={displayName}
        onSelect={onSelect}
      />
    </div>
  );
}

type CellContext = {
  prizes: PrizeDraft[];
  probabilities: number[];
  pityKey: number | null;
  selectedKey: number | null;
  displayName: (prize: PrizeDraft) => string;
  onSelect: (key: number) => void;
};

function usePrizeSensors() {
  return useSensors(
    // 留 6px 的起拖距离，否则点一下选格子会被当成拖动
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );
}

/* ───────────────────────────── 九宫格 ───────────────────────────── */

function GridBoard({ onSwap, ...context }: CellContext & { onSwap: (from: number, to: number) => void }) {
  const sensors = usePrizeSensors();
  const [dragging, setDragging] = useState<number | null>(null);
  const { prizes, probabilities } = context;
  const winRate = prizes.reduce((sum, prize, index) => (prize.type === "none" ? sum : sum + (probabilities[index] ?? 0)), 0);

  const onDragStart = (event: DragStartEvent) => setDragging(Number(event.active.id));
  const onDragEnd = (event: DragEndEvent) => {
    setDragging(null);
    if (!event.over) return;
    const from = prizes.findIndex((prize) => prize.key === Number(event.active.id));
    const to = Number(String(event.over.id).replace("slot-", ""));
    onSwap(from, to);
  };
  const draggingIndex = dragging === null ? -1 : prizes.findIndex((prize) => prize.key === dragging);

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onDragCancel={() => setDragging(null)}
    >
      <div className="mx-auto w-full max-w-[19rem] rounded-[1.75rem] border border-border bg-muted/50 p-2 dark:bg-muted/20">
        <div className="grid aspect-square grid-cols-3 grid-rows-3 gap-2">
          {[0, 1, 2].flatMap((row) =>
            [0, 1, 2].map((col) => {
              if (row === 1 && col === 1) {
                return (
                  <div
                    key="go"
                    className="flex flex-col items-center justify-center gap-0.5 rounded-2xl bg-foreground text-background shadow-[inset_0_-3px_0_rgb(0_0_0/0.25)]"
                  >
                    <span className="text-base font-black tracking-[0.25em]">GO</span>
                    <span className="font-data text-[10px] tabular-nums opacity-60">
                      中奖 <NumberFlow value={Math.min(100, winRate)} format={{ maximumFractionDigits: 1 }} suffix="%" />
                    </span>
                  </div>
                );
              }
              const position = GRID_CELLS.findIndex(([r, c]) => r === row && c === col);
              return <GridSlot key={`slot-${position}`} position={position} context={context} />;
            })
          )}
        </div>
      </div>
      <p className="text-center text-[11px] text-muted-foreground">拖动格子可交换位置</p>
      <DragOverlay dropAnimation={{ duration: 180, easing: "cubic-bezier(0.22,1,0.36,1)" }}>
        {draggingIndex >= 0 ? (
          <div className="aspect-square w-[5.6rem] rotate-3 scale-105">
            <PrizeCellFace
              prize={prizes[draggingIndex]}
              position={draggingIndex}
              context={context}
              lifted
            />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function GridSlot({ position, context }: { position: number; context: CellContext }) {
  const prize = context.prizes[position];
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: `slot-${position}` });
  const { setNodeRef: setDragRef, attributes, listeners, isDragging } = useDraggable({
    id: prize ? prize.key : `empty-${position}`,
    disabled: !prize
  });

  if (!prize) {
    return (
      <div
        ref={setDropRef}
        className="grid place-items-center rounded-2xl border border-dashed border-border text-[11px] text-muted-foreground"
      >
        空
      </div>
    );
  }
  return (
    <div ref={setDropRef} className={cn("relative min-w-0 rounded-2xl transition-transform", isOver && "scale-[0.96]")}>
      {isOver ? <div className="pointer-events-none absolute -inset-1 rounded-[1.15rem] border-2 border-dashed border-foreground/40" /> : null}
      <button
        ref={setDragRef}
        type="button"
        {...attributes}
        {...listeners}
        onClick={() => context.onSelect(prize.key)}
        className={cn("block size-full touch-none rounded-2xl outline-none focus-visible:ring-2 focus-visible:ring-ring", isDragging && "opacity-30")}
      >
        <PrizeCellFace prize={prize} position={position} context={context} />
      </button>
    </div>
  );
}

function PrizeCellFace({
  prize,
  position,
  context,
  lifted
}: {
  prize: PrizeDraft;
  position: number;
  context: CellContext;
  lifted?: boolean;
}) {
  const active = prize.key === context.selectedKey;
  const visual = kindVisual(prize.type);
  const probability = context.probabilities[position] ?? 0;
  return (
    <div
      className={cn(
        "relative flex size-full cursor-grab flex-col items-center justify-center gap-1 overflow-hidden rounded-2xl bg-card p-1.5 text-center transition-[box-shadow,background-color] duration-150 active:cursor-grabbing",
        active ? cn("ring-2", visual.ring) : "ring-1 ring-border hover:ring-foreground/25",
        lifted && "shadow-xl ring-2 ring-foreground/30",
        prize.type === "none" && "bg-card/70"
      )}
    >
      <span className="absolute left-1.5 top-1 font-data text-[9px] tabular-nums text-muted-foreground/70">{position + 1}</span>
      <CellMarks prize={prize} isPity={prize.key === context.pityKey} />
      <KindGlyph type={prize.type} size="sm" />
      <span className={cn("line-clamp-2 w-full px-0.5 text-[11px] font-medium leading-tight", prize.type === "none" && "text-muted-foreground")}>
        {context.displayName(prize)}
      </span>
      <span className="font-data text-[10px] tabular-nums text-muted-foreground">
        <NumberFlow value={probability} format={percentFormat(probability)} suffix="%" />
      </span>
      <span className="absolute inset-x-0 bottom-0 h-0.5 bg-muted">
        <span className={cn("block h-full transition-[width] duration-300", visual.fill)} style={{ width: `${Math.min(100, probability)}%` }} />
      </span>
    </div>
  );
}

/** 格子右上角的小标记：保底、兜底、限量 */
function CellMarks({ prize, isPity }: { prize: PrizeDraft; isPity: boolean }) {
  const limited = !prize.unlimited || Number(prize.dailyLimit) > 0 || Number(prize.userLimit) > 0 || Number(prize.userDailyLimit) > 0;
  if (!isPity && !prize.fallback && !limited) return null;
  return (
    <span className="absolute right-1 top-1 flex gap-0.5">
      {isPity ? (
        <span className="grid size-3.5 place-items-center rounded-full bg-sky-500 text-white" title="保底奖">
          <ShieldCheck className="size-2.5" />
        </span>
      ) : null}
      {prize.fallback ? (
        <span className="grid size-3.5 place-items-center rounded-full bg-zinc-500 text-white" title="兜底奖">
          <LifeBuoy className="size-2.5" />
        </span>
      ) : null}
      {limited ? <span className="mt-[3px] size-2 rounded-full bg-amber-500" title="限量" /> : null}
    </span>
  );
}

/* ───────────────────────────── 转盘 ───────────────────────────── */

function WheelBoard({
  canAdd,
  canRemove,
  onMove,
  onAdd,
  onRemove,
  ...context
}: CellContext & {
  canAdd: boolean;
  canRemove: boolean;
  onMove: (from: number, to: number) => void;
  onAdd: () => void;
  onRemove: (key: number) => void;
}) {
  const sensors = usePrizeSensors();
  const { prizes } = context;
  const onDragEnd = (event: DragEndEvent) => {
    if (!event.over || event.active.id === event.over.id) return;
    const from = prizes.findIndex((prize) => prize.key === Number(event.active.id));
    const to = prizes.findIndex((prize) => prize.key === Number(event.over?.id));
    onMove(from, to);
  };

  return (
    <div className="space-y-3">
      <WheelPreview {...context} />
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={prizes.map((prize) => prize.key)} strategy={verticalListSortingStrategy}>
          <div className="space-y-1">
            {prizes.map((prize, index) => (
              <WheelRow
                key={prize.key}
                prize={prize}
                index={index}
                context={context}
                canRemove={canRemove}
                onRemove={() => onRemove(prize.key)}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>
      {canAdd ? (
        <button
          type="button"
          onClick={onAdd}
          className="flex h-9 w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-border text-xs text-muted-foreground transition-colors hover:border-foreground/30 hover:bg-muted/40 hover:text-foreground"
        >
          <Plus className="size-3.5" />
          添加奖品
        </button>
      ) : null}
    </div>
  );
}

/** 等分扇区的转盘预览：与 App 端一样每格同样大，概率不体现在扇区大小上 */
function WheelPreview({ prizes, selectedKey, onSelect }: CellContext) {
  const size = 200;
  const radius = size / 2 - 4;
  const center = size / 2;
  const count = Math.max(prizes.length, 1);
  const point = (angle: number, r: number) => [
    center + r * Math.sin(angle),
    center - r * Math.cos(angle)
  ];
  return (
    <div className="relative mx-auto aspect-square w-full max-w-[13rem]">
      <svg viewBox={`0 0 ${size} ${size}`} className="size-full">
        <circle cx={center} cy={center} r={radius + 3} className="fill-muted stroke-border" />
        {prizes.map((prize, index) => {
          const start = (index / count) * Math.PI * 2;
          const end = ((index + 1) / count) * Math.PI * 2;
          const [x1, y1] = point(start, radius);
          const [x2, y2] = point(end, radius);
          const [tx, ty] = point((start + end) / 2, radius * 0.68);
          const large = end - start > Math.PI ? 1 : 0;
          const active = prize.key === selectedKey;
          return (
            <g key={prize.key} className="cursor-pointer" onClick={() => onSelect(prize.key)}>
              <path
                d={`M ${center} ${center} L ${x1} ${y1} A ${radius} ${radius} 0 ${large} 1 ${x2} ${y2} Z`}
                className={cn(
                  "stroke-background transition-opacity",
                  WHEEL_FILL[prize.type] ?? "fill-slate-400",
                  index % 2 === 1 && "opacity-80",
                  selectedKey !== null && !active && "opacity-45"
                )}
                strokeWidth={1.5}
              />
              <text
                x={tx}
                y={ty}
                textAnchor="middle"
                dominantBaseline="central"
                className="pointer-events-none fill-white font-data text-[11px] font-semibold"
              >
                {index + 1}
              </text>
            </g>
          );
        })}
        <circle cx={center} cy={center} r={22} className="fill-foreground" />
        <text x={center} y={center} textAnchor="middle" dominantBaseline="central" className="fill-background text-[11px] font-black tracking-widest">
          GO
        </text>
        <path d={`M ${center - 7} 2 L ${center + 7} 2 L ${center} 16 Z`} className="fill-foreground" />
      </svg>
    </div>
  );
}

/** SVG 的填充色要用 fill-*，与 prize-visuals 里的 bg-* 同色 */
const WHEEL_FILL: Record<string, string> = {
  none: "fill-zinc-400 dark:fill-zinc-600",
  points: "fill-amber-400",
  experience: "fill-sky-400",
  vip_days: "fill-violet-400",
  vip_plan: "fill-fuchsia-400",
  feature: "fill-rose-400",
  lottery_draws: "fill-emerald-400",
  balance: "fill-teal-400",
  manual: "fill-orange-400",
  number_change: "fill-indigo-400",
  number_ticket: "fill-yellow-400",
  number_draw: "fill-cyan-400"
};

function WheelRow({
  prize,
  index,
  context,
  canRemove,
  onRemove
}: {
  prize: PrizeDraft;
  index: number;
  context: CellContext;
  canRemove: boolean;
  onRemove: () => void;
}) {
  const { setNodeRef, attributes, listeners, transform, transition, isDragging } = useSortable({ id: prize.key });
  const active = prize.key === context.selectedKey;
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        "group flex items-center gap-1.5 rounded-xl bg-card py-1 pl-1 pr-1.5 ring-1 transition-[box-shadow,background-color]",
        active ? cn("ring-2", kindVisual(prize.type).ring) : "ring-border hover:ring-foreground/25",
        isDragging && "relative z-10 shadow-lg"
      )}
    >
      <button
        type="button"
        {...attributes}
        {...listeners}
        className="grid h-7 w-5 shrink-0 cursor-grab touch-none place-items-center rounded-md text-muted-foreground/60 hover:bg-muted hover:text-foreground active:cursor-grabbing"
        aria-label="拖动排序"
      >
        <GripVertical className="size-3.5" />
      </button>
      <button type="button" onClick={() => context.onSelect(prize.key)} className="flex min-w-0 flex-1 items-center gap-2 text-left">
        <span className="w-4 shrink-0 text-center font-data text-[10px] tabular-nums text-muted-foreground">{index + 1}</span>
        <KindGlyph type={prize.type} size="sm" />
        <span className={cn("min-w-0 flex-1 truncate text-xs font-medium", prize.type === "none" && "text-muted-foreground")}>
          {context.displayName(prize)}
        </span>
        <span className="relative flex shrink-0 items-center gap-1">
          <CellMarks prize={prize} isPity={prize.key === context.pityKey} />
        </span>
        <span className="shrink-0 font-data text-[11px] tabular-nums text-muted-foreground">
          <NumberFlow value={context.probabilities[index] ?? 0} format={percentFormat(context.probabilities[index] ?? 0)} suffix="%" />
        </span>
      </button>
      <button
        type="button"
        disabled={!canRemove}
        onClick={onRemove}
        aria-label="删除奖品"
        className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground opacity-0 transition-[opacity,color] hover:bg-destructive/10 hover:text-destructive group-hover:opacity-100 disabled:hidden"
      >
        <Trash2 className="size-3" />
      </button>
    </div>
  );
}

/* ───────────────────────────── 分布条 ───────────────────────────── */

/** 整盘的概率分布：每段宽度即公示概率，点一段选中那一格 */
function DistributionBar({
  prizes,
  probabilities,
  selectedKey,
  displayName,
  onSelect
}: {
  prizes: PrizeDraft[];
  probabilities: number[];
  selectedKey: number | null;
  displayName: (prize: PrizeDraft) => string;
  onSelect: (key: number) => void;
}) {
  const total = probabilities.reduce((sum, value) => sum + value, 0);
  return (
    <div className="space-y-2 rounded-2xl border border-border bg-card p-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium">概率分布</span>
        <span className="text-[11px] text-muted-foreground">按权重与库存实时计算的公示概率</span>
      </div>
      <div className="flex h-3 gap-0.5 overflow-hidden rounded-full bg-muted">
        {total > 0
          ? prizes.map((prize, index) => {
              const value = probabilities[index] ?? 0;
              if (value <= 0) return null;
              return (
                <Tooltip key={prize.key}>
                  <TooltipTrigger asChild>
                    <motion.button
                      type="button"
                      layout
                      onClick={() => onSelect(prize.key)}
                      transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
                      style={{ flexGrow: value, flexBasis: 0 }}
                      className={cn(
                        "h-full min-w-1 transition-opacity",
                        kindVisual(prize.type).fill,
                        selectedKey !== null && prize.key !== selectedKey && "opacity-40 hover:opacity-80"
                      )}
                      aria-label={`${displayName(prize)} ${formatProbability(value)}`}
                    />
                  </TooltipTrigger>
                  <TooltipContent>
                    {index + 1}. {displayName(prize)} {formatProbability(value)}
                  </TooltipContent>
                </Tooltip>
              );
            })
          : null}
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {prizes.map((prize, index) => (
          <button
            key={prize.key}
            type="button"
            onClick={() => onSelect(prize.key)}
            className={cn(
              "flex items-center gap-1.5 text-[11px] transition-colors",
              prize.key === selectedKey ? "text-foreground" : "text-muted-foreground hover:text-foreground"
            )}
          >
            <span className={cn("size-2 rounded-full", kindVisual(prize.type).fill)} />
            <span className="max-w-[7rem] truncate">{displayName(prize)}</span>
            <span className="font-data tabular-nums">{formatProbability(probabilities[index] ?? 0)}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

/* ───────────────────────────── 单格表单 ───────────────────────────── */

function InspectorBlock({ title, aside, children }: { title: string; aside?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="space-y-3 border-t border-border/70 px-4 py-3.5 first:border-t-0">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{title}</span>
        {aside ? <span className="text-[11px] text-muted-foreground">{aside}</span> : null}
      </div>
      {children}
    </div>
  );
}

function PrizeInspector({
  prize,
  index,
  total,
  uiMode,
  probability,
  isPity,
  catalog,
  swapPreview,
  canRemove,
  onRemove,
  onNavigate,
  onShift,
  onPatch,
  onFallback
}: {
  prize: PrizeDraft;
  index: number;
  total: number;
  uiMode: LotteryUIMode;
  probability: number;
  isPity: boolean;
  catalog?: LotteryCatalog | null;
  swapPreview?: (prize: PrizeDraft) => string | null;
  canRemove: boolean;
  onRemove: () => void;
  onNavigate: (offset: number) => void;
  onShift: (offset: number) => void;
  onPatch: (next: Partial<PrizeDraft>) => void;
  onFallback: (fallback: boolean) => void;
}) {
  const kinds = catalog?.kinds ?? [];
  const plans = catalog?.plans ?? [];
  const features = catalog?.features ?? [];
  const spec = kinds.find((item) => item.type === prize.type);
  const placeholder = defaultPrizeName(rewardOf(prize, kinds), kinds, plans, features);
  const quantity = Number(prize.quantity) || 0;
  const remaining = Math.max(0, quantity - prize.used);
  const swapKinds = catalog?.swapKinds?.length ? catalog.swapKinds : SWAP_KINDS;
  const swapSpec = kinds.find((item) => item.type === prize.swapType);
  const preview = swapPreview?.(prize);
  const [imageFailed, setImageFailed] = useState(false);
  const image = prize.imageUrl.trim();

  const changeKind = (type: string) => {
    if (type === prize.type) return;
    const next = kinds.find((item) => item.type === type);
    onPatch({
      type,
      amount: String(next?.value === "feature" ? 7 : next?.value === "feature_minutes" ? 30 : next?.value === "cosmetic" ? 0 : next?.min ?? 1),
      refId: next?.value === "number_ticket" ? 1 : next?.value === "cosmetic" || next?.value === "coupon" ? 0 : (plans[0]?.id ?? 0),
      tag: features[0]?.tag ?? "",
      text: ""
    });
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
      className="min-w-0 overflow-hidden rounded-2xl border border-border bg-card"
    >
      <div className="flex items-center gap-3 border-b border-border/70 px-4 py-3">
        <KindGlyph type={prize.type} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold">{prize.name.trim() || placeholder}</div>
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span>
              第 {index + 1} {uiMode === "grid" ? "格" : "个"}
            </span>
            {prize.id ? <span className="tabular-nums">已抽中 {prize.used}</span> : <span>新奖品</span>}
            {isPity ? <span className="text-sky-600 dark:text-sky-400">保底奖</span> : null}
            {prize.fallback ? <span>兜底奖</span> : null}
          </div>
        </div>
        <div className="flex items-center gap-0.5">
          <IconButton label={uiMode === "grid" ? "与上一格交换" : "上移"} disabled={index === 0} onClick={() => onShift(-1)}>
            <ArrowRight className="size-3.5 rotate-180" />
          </IconButton>
          <IconButton label={uiMode === "grid" ? "与下一格交换" : "下移"} disabled={index === total - 1} onClick={() => onShift(1)}>
            <ArrowRight className="size-3.5" />
          </IconButton>
          <span className="mx-1 h-4 w-px bg-border" />
          <IconButton label="上一个" onClick={() => onNavigate(-1)}>
            <ChevronLeft className="size-4" />
          </IconButton>
          <IconButton label="下一个" onClick={() => onNavigate(1)}>
            <ChevronRight className="size-4" />
          </IconButton>
          {canRemove ? (
            <IconButton label="删除奖品" onClick={onRemove} danger>
              <Trash2 className="size-3.5" />
            </IconButton>
          ) : null}
        </div>
      </div>

      <InspectorBlock title="奖励">
        <div className="grid grid-cols-3 gap-1.5 sm:grid-cols-4">
          {kinds.map((item) => {
            const active = item.type === prize.type;
            const visual = kindVisual(item.type);
            const Icon = visual.icon;
            return (
              <Tooltip key={item.type}>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => changeKind(item.type)}
                    className={cn(
                      "flex min-w-0 flex-col items-center gap-1 rounded-xl border px-1 py-2 text-[11px] transition-colors",
                      active ? cn("border-transparent ring-2", visual.ring, visual.tone) : "border-border text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                    )}
                  >
                    <Icon className="size-4" />
                    <span className="w-full truncate text-center font-medium">{item.label}</span>
                  </button>
                </TooltipTrigger>
                {item.hint ? <TooltipContent>{item.hint}</TooltipContent> : null}
              </Tooltip>
            );
          })}
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="名称" aside="留空自动命名" className="sm:col-span-2">
            <TextField
              value={prize.name}
              maxLength={LIMITS.prizeName}
              placeholder={placeholder}
              onChange={(name) => onPatch({ name })}
            />
          </Field>

          {spec?.value === "amount" ? (
            <Field label={spec.label} aside={`${spec.min ?? 0}–${spec.max ?? "∞"}`} className="sm:col-span-2">
              <Stepper
                value={prize.amount}
                min={spec.min}
                max={spec.max}
                suffix={spec.unit}
                onChange={(amount) => onPatch({ amount })}
              />
            </Field>
          ) : null}

          {spec?.value === "ref" ? (
            <Field label="会员套餐" className="sm:col-span-2">
              {plans.length === 0 ? (
                <EmptyHint>暂无会员套餐</EmptyHint>
              ) : (
                <Select value={prize.refId ? String(prize.refId) : ""} onValueChange={(value) => onPatch({ refId: Number(value) })}>
                  <SelectTrigger className={cn(FIELD_SURFACE, "h-9 w-full border-0 shadow-none")}>
                    <SelectValue placeholder="选择套餐" />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {plans.map((plan) => (
                      <SelectItem key={plan.id} value={String(plan.id)}>
                        {plan.name}
                        <span className="ml-1.5 text-[11px] text-muted-foreground">{vipPlanTerm(plan)}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>
          ) : null}

          {spec?.value === "feature" ? (
            features.length === 0 ? (
              <EmptyHint className="sm:col-span-2">暂无功能标识</EmptyHint>
            ) : (
              <>
                <Field label="功能">
                  <Select value={prize.tag} onValueChange={(tag) => onPatch({ tag })}>
                    <SelectTrigger className={cn(FIELD_SURFACE, "h-9 w-full border-0 shadow-none")}>
                      <SelectValue placeholder="选择功能" />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {features.map((feature) => (
                        <SelectItem key={feature.tag} value={feature.tag}>
                          {feature.name || feature.tag}
                          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="时长" aside={termLabel(Number(prize.amount) || 0)}>
                  <Stepper value={prize.amount} min={0} max={spec.max} suffix="天" onChange={(amount) => onPatch({ amount })} />
                </Field>
              </>
            )
          ) : null}

          {spec?.value === "feature_minutes" ? (
            features.length === 0 ? (
              <EmptyHint className="sm:col-span-2">暂无功能标识</EmptyHint>
            ) : (
              <>
                <Field label="功能" aside="看激励广告换时长的那个功能">
                  <Select value={prize.tag} onValueChange={(tag) => onPatch({ tag })}>
                    <SelectTrigger className={cn(FIELD_SURFACE, "h-9 w-full border-0 shadow-none")}>
                      <SelectValue placeholder="选择功能" />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {features.map((feature) => (
                        <SelectItem key={feature.tag} value={feature.tag}>
                          {feature.name || feature.tag}
                          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="时长" aside={`${spec.min ?? 1}–${spec.max ?? "∞"} ${spec.unit ?? "分钟"}`}>
                  <Stepper
                    value={prize.amount}
                    min={spec.min}
                    max={spec.max}
                    step={10}
                    suffix={spec.unit ?? "分钟"}
                    onChange={(amount) => onPatch({ amount })}
                  />
                </Field>
              </>
            )
          ) : null}

          {spec?.value === "number_ticket" ? (
            <>
              <Field label="靓号券等级">
                <Select value={String(prize.refId || 1)} onValueChange={(value) => onPatch({ refId: Number(value) })}>
                  <SelectTrigger className={cn(FIELD_SURFACE, "h-9 w-full border-0 shadow-none")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {NUMBER_TICKET_LEVELS.map((tier) => (
                      <SelectItem key={tier.level} value={String(tier.level)}>
                        {tier.name}靓号券
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="张数" aside={`${spec.min ?? 1}–${spec.max ?? "∞"}`}>
                <Stepper
                  value={prize.amount}
                  min={spec.min ?? 1}
                  max={spec.max}
                  suffix={spec.unit ?? "张"}
                  onChange={(amount) => onPatch({ amount })}
                />
              </Field>
            </>
          ) : null}

          {spec?.value === "cosmetic" ? (
            <Field label="装扮与天数（0 为永久）" className="sm:col-span-2">
              <CosmeticRewardInput
                refId={prize.refId}
                amount={prize.amount}
                max={spec.max}
                onChange={(next) => onPatch({ ...(next.refId !== undefined ? { refId: next.refId } : {}), ...(next.amount !== undefined ? { amount: next.amount } : {}) })}
              />
            </Field>
          ) : null}

          {spec?.value === "coupon" ? (
            <Field label="优惠券与张数" className="sm:col-span-2">
              <CouponRewardInput
                refId={prize.refId}
                amount={prize.amount}
                min={spec.min ?? 1}
                max={spec.max ?? 20}
                onChange={(next) => onPatch({ ...(next.refId !== undefined ? { refId: next.refId } : {}), ...(next.amount !== undefined ? { amount: next.amount } : {}) })}
              />
            </Field>
          ) : null}

          {spec?.value === "text" ? (
            <Field label="发放内容" className="sm:col-span-2">
              <TextField value={prize.text} maxLength={64} placeholder="例如：定制贴纸一套" onChange={(text) => onPatch({ text })} />
            </Field>
          ) : null}
        </div>
      </InspectorBlock>

      <InspectorBlock
        title="概率"
        aside={
          <span className="font-data text-sm font-semibold tabular-nums text-foreground">
            <NumberFlow value={probability} format={percentFormat(probability)} suffix="%" />
          </span>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="权重" aside="按权重占比出奖">
            <Stepper value={prize.weight} min={0} max={LIMITS.weight} onChange={(weight) => onPatch({ weight })} />
          </Field>
          <Field label="积分当量" aside="0 为自动估算">
            <Stepper value={prize.value} min={0} onChange={(value) => onPatch({ value })} />
          </Field>
        </div>
        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
          <motion.div
            className={cn("h-full rounded-full", kindVisual(prize.type).fill)}
            animate={{ width: `${Math.min(100, probability)}%` }}
            transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
          />
        </div>
      </InspectorBlock>

      <InspectorBlock title="库存与限量" aside={prize.id ? `已抽中 ${prize.used}` : undefined}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Segmented
            size="sm"
            value={prize.unlimited ? "unlimited" : "limited"}
            options={[
              { value: "unlimited", label: "不限库存" },
              { value: "limited", label: "限量" }
            ]}
            onChange={(mode) => {
              const unlimited = mode === "unlimited";
              onPatch({ unlimited, fallback: unlimited ? prize.fallback : false });
            }}
          />
          {!prize.unlimited ? (
            <span className="text-[11px] text-muted-foreground">
              剩余 <span className="font-data tabular-nums text-foreground">{remaining}</span>
            </span>
          ) : null}
        </div>
        {!prize.unlimited ? (
          <div className="space-y-2">
            <Stepper value={prize.quantity} min={prize.used} max={LIMITS.quantity} suffix="份" onChange={(next) => onPatch({ quantity: next })} />
            {quantity > 0 ? (
              <div className="h-1 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-amber-500" style={{ width: `${Math.min(100, (prize.used / quantity) * 100)}%` }} />
              </div>
            ) : null}
          </div>
        ) : null}
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="全站每日" aside="0 不限">
            <Stepper value={prize.dailyLimit} min={0} disabled={prize.fallback} onChange={(dailyLimit) => onPatch({ dailyLimit })} />
          </Field>
          <Field label="每人累计" aside="0 不限">
            <Stepper value={prize.userLimit} min={0} disabled={prize.fallback} onChange={(userLimit) => onPatch({ userLimit })} />
          </Field>
          <Field label="每人每日" aside="0 不限">
            <Stepper value={prize.userDailyLimit} min={0} disabled={prize.fallback} onChange={(userDailyLimit) => onPatch({ userDailyLimit })} />
          </Field>
        </div>
        <ToggleTile
          checked={prize.fallback}
          onChange={onFallback}
          icon={<LifeBuoy />}
          label="兜底奖"
          hint="其它奖品都抽不出时发这一格；只能有一个，必须不限库存、不设限量"
        />
      </InspectorBlock>

      {(catalog?.ownableKinds ?? OWNABLE_KINDS).includes(prize.type) ? (
        <InspectorBlock title="已拥有时改发" aside={preview ? `改发 ${preview}` : "照常发放"}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Select
              value={prize.swapType || SWAP_POLICY}
              onValueChange={(value) => {
                const swapType = value === SWAP_POLICY ? "" : value;
                const nextSpec = kinds.find((item) => item.type === swapType);
                onPatch({
                  swapType,
                  swapAmount: swapType ? prize.swapAmount || String(Math.max(nextSpec?.min ?? 1, 1)) : ""
                });
              }}
            >
              <SelectTrigger className={cn(FIELD_SURFACE, "h-9 w-full border-0 shadow-none")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={SWAP_POLICY}>按活动的处理方式</SelectItem>
                {swapKinds.map((type) => (
                  <SelectItem key={type} value={type}>
                    {kinds.find((item) => item.type === type)?.label ?? type}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {prize.swapType ? (
              <Stepper
                value={prize.swapAmount}
                min={swapSpec?.min}
                max={swapSpec?.max}
                suffix={swapSpec?.unit}
                onChange={(swapAmount) => onPatch({ swapAmount })}
              />
            ) : null}
          </div>
          <p className="text-[11px] text-muted-foreground">永久会员已包含或已永久解锁时生效；活动选择照常发放时不生效。</p>
        </InspectorBlock>
      ) : null}

      <InspectorBlock title="图片">
        <div className="flex items-center gap-3">
          <div className="grid size-12 shrink-0 place-items-center overflow-hidden rounded-xl bg-muted text-muted-foreground">
            {image && /^https?:\/\//.test(image) && !imageFailed ? (
              // eslint-disable-next-line @next/next/no-img-element -- 任意外链图片，不走 next/image 的域名白名单
              <img src={image} alt="" className="size-full object-cover" onError={() => setImageFailed(true)} />
            ) : (
              <ImageIcon className="size-4" />
            )}
          </div>
          <div className="min-w-0 flex-1">
            <TextField
              value={prize.imageUrl}
              maxLength={512}
              placeholder="https://，留空用默认图标"
              onChange={(imageUrl) => {
                setImageFailed(false);
                onPatch({ imageUrl });
              }}
            />
          </div>
        </div>
      </InspectorBlock>
    </motion.div>
  );
}

function IconButton({
  label,
  disabled,
  danger,
  onClick,
  children
}: {
  label: string;
  disabled?: boolean;
  danger?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={label}
          disabled={disabled}
          onClick={onClick}
          className={cn(
            "grid size-7 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-30",
            danger && "hover:bg-destructive/10 hover:text-destructive"
          )}
        >
          {children}
        </button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function EmptyHint({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <p className={cn("rounded-xl border border-dashed border-border px-3 py-2.5 text-xs text-muted-foreground", className)}>
      {children}
    </p>
  );
}
