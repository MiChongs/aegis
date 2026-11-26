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
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowDownToLine,
  ArrowUpToLine,
  Circle,
  CircleDashed,
  CircleUserRound,
  Copy,
  Eye,
  EyeOff,
  Film,
  GripVertical,
  Hexagon,
  ImageIcon,
  Lock,
  LockOpen,
  MoreHorizontal,
  Plus,
  Sparkles,
  Square,
  Star,
  TextCursorInput,
  Trash2,
  Type
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import type { Layer, ShapeKind } from "@/lib/api/cosmetics";
import { cn } from "@/lib/utils";
import {
  AVATAR_ID,
  LAYER_TYPE_LABEL,
  PLATE_ID,
  SHAPE_LABEL,
  createLayer,
  duplicateLayer,
  insertLayer,
  patchLayer,
  removeLayer,
  type NewLayerKind
} from "./design-ops";
import { useDesigner, useDesignerApi } from "./designer-store";

/**
 * 图层面板：最上面的图层排在最前（与画面一致）。头像框多一行「头像」、铭牌多一行「铭牌文字」：它上面的图层
 * 画在头像 / 文字之上，下面的画在之下——把图层拖过这一行就改了 placement。显隐写进设计稿，锁定只是前端状态（不入库）。
 */

const SHAPE_ICON: Record<ShapeKind, React.ComponentType<{ className?: string }>> = {
  rect: Square,
  circle: Circle,
  ring: CircleDashed,
  star: Star,
  polygon: Hexagon
};


/** 图层类型图标（组件形式，避免在渲染中「创建组件」） */
export function LayerIcon({ layer, className }: { layer: Layer; className?: string }) {
  switch (layer.type) {
    case "image":
      return <ImageIcon className={className} />;
    case "sprite":
      return <Film className={className} />;
    case "text":
      return <Type className={className} />;
    case "particles":
      return <Sparkles className={className} />;
  }
  switch (layer.shape) {
    case "circle":
      return <Circle className={className} />;
    case "ring":
      return <CircleDashed className={className} />;
    case "star":
      return <Star className={className} />;
    case "polygon":
      return <Hexagon className={className} />;
    default:
      return <Square className={className} />;
  }
}

/** 添加图层菜单（图层面板与空画布共用） */
export function AddLayerMenu({
  disabled,
  onAdd,
  onPickAsset,
  children
}: {
  disabled?: boolean;
  onAdd: (kind: NewLayerKind) => void;
  onPickAsset: (kind: "image" | "sprite") => void;
  children?: React.ReactNode;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        {children ?? (
          <Button size="icon-xs" variant="ghost" aria-label="添加图层">
            <Plus />
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-44">
        <DropdownMenuItem onSelect={() => onPickAsset("image")}>
          <ImageIcon />
          图片
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onPickAsset("sprite")}>
          <Film />
          序列帧
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onAdd({ type: "text" })}>
          <Type />
          文字
        </DropdownMenuItem>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <Square />
            形状
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-36">
            {(Object.keys(SHAPE_LABEL) as ShapeKind[]).map((shape) => {
              const Icon = SHAPE_ICON[shape];
              return (
                <DropdownMenuItem key={shape} onSelect={() => onAdd({ type: "shape", shape })}>
                  <Icon />
                  {SHAPE_LABEL[shape]}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuItem onSelect={() => onAdd({ type: "particles" })}>
          <Sparkles />
          粒子
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function DesignerLayers({
  readOnly,
  maxLayers,
  onPickAsset
}: {
  readOnly: boolean;
  maxLayers: number;
  onPickAsset: (kind: "image" | "sprite") => void;
}) {
  const api = useDesignerApi();
  const design = useDesigner((s) => s.design);
  const selected = useDesigner((s) => s.selected);
  const locked = useDesigner((s) => s.locked);
  const [renaming, setRenaming] = useState<string | null>(null);
  // 分界行：头像框是头像，铭牌是文字
  const markerId = design.avatar ? AVATAR_ID : design.plate ? PLATE_ID : null;
  const hasMarker = markerId !== null;
  const markerName = markerId === PLATE_ID ? "文字" : "头像";

  // 显示顺序：最上层在前
  const above = design.layers.filter((layer) => !hasMarker || layer.placement !== "below").reverse();
  const below = hasMarker ? design.layers.filter((layer) => layer.placement === "below").reverse() : [];
  const rows: string[] = markerId ? [...above.map((l) => l.id), markerId, ...below.map((l) => l.id)] : above.map((l) => l.id);
  const byId = new Map(design.layers.map((layer) => [layer.id, layer]));

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  /** 显示顺序 → 设计稿数组（先画的在前），并按分界行重新划分 placement */
  const applyOrder = (display: string[]) => {
    api.getState().update((d) => {
      const lookup = new Map(d.layers.map((layer) => [layer.id, layer]));
      const marker = markerId ? display.indexOf(markerId) : -1;
      const ordered = display
        .filter((id) => id !== markerId)
        .map((id) => {
          const layer = lookup.get(id)!;
          if (!hasMarker) return layer;
          const placement: Layer["placement"] = display.indexOf(id) > marker ? "below" : "above";
          return layer.placement === placement ? layer : { ...layer, placement };
        });
      return { ...d, layers: ordered.reverse() };
    });
  };

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    applyOrder(arrayMove(rows, rows.indexOf(String(active.id)), rows.indexOf(String(over.id))));
  };

  const add = (kind: NewLayerKind) => {
    if (design.layers.length >= maxLayers) return;
    const layer = createLayer(design, kind);
    api.getState().update((d) => insertLayer(d, layer, selected && selected !== AVATAR_ID && selected !== PLATE_ID ? selected : null));
    api.getState().select(layer.id);
  };

  const move = (id: string, to: "top" | "bottom") => {
    const rest = rows.filter((row) => row !== id);
    const marker = markerId ? rest.indexOf(markerId) : -1;
    const layer = byId.get(id);
    if (!layer) return;
    if (!hasMarker) {
      applyOrder(to === "top" ? [id, ...rest] : [...rest, id]);
      return;
    }
    // 只在自己所在的那一组里置顶 / 置底
    if (layer.placement === "below") {
      applyOrder(to === "top" ? [...rest.slice(0, marker + 1), id, ...rest.slice(marker + 1)] : [...rest, id]);
    } else {
      applyOrder(to === "top" ? [id, ...rest] : [...rest.slice(0, marker), id, ...rest.slice(marker)]);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-9 shrink-0 items-center justify-between gap-2 border-b px-3">
        <span className="text-xs font-semibold">
          图层
          <span className="ml-1.5 font-normal tabular-nums text-muted-foreground">
            {design.layers.length}/{maxLayers}
          </span>
        </span>
        {!readOnly ? <AddLayerMenu disabled={design.layers.length >= maxLayers} onAdd={add} onPickAsset={onPickAsset} /> : null}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
        {rows.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-3 py-8 text-center">
            <span className="text-xs text-muted-foreground">暂无图层</span>
            {!readOnly ? (
              <AddLayerMenu onAdd={add} onPickAsset={onPickAsset}>
                <Button size="xs" variant="outline">
                  <Plus />
                  添加图层
                </Button>
              </AddLayerMenu>
            ) : null}
          </div>
        ) : (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={rows} strategy={verticalListSortingStrategy} disabled={readOnly}>
              <ul className="flex flex-col gap-0.5">
                {rows.map((id) =>
                  id === markerId ? (
                    <MarkerRow key={id} id={id} active={selected === id} onSelect={() => api.getState().select(id)} />
                  ) : (
                    <LayerRow
                      key={id}
                      layer={byId.get(id)!}
                      active={selected === id}
                      locked={Boolean(locked[id])}
                      readOnly={readOnly}
                      renaming={renaming === id}
                      onRename={(name) => {
                        setRenaming(null);
                        if (name !== null) api.getState().update((d) => patchLayer(d, id, { name: name.trim().slice(0, 32) }));
                      }}
                      onStartRename={() => setRenaming(id)}
                      onSelect={() => api.getState().select(id)}
                      onToggleHidden={() => api.getState().update((d) => patchLayer(d, id, (layer) => ({ ...layer, hidden: !layer.hidden })))}
                      onToggleLock={() => api.getState().toggleLock(id)}
                      onDuplicate={() => {
                        if (design.layers.length >= maxLayers) return;
                        const source = byId.get(id)!;
                        const copy = duplicateLayer(design, source);
                        api.getState().update((d) => insertLayer(d, copy, id));
                        api.getState().select(copy.id);
                      }}
                      onDelete={() => {
                        api.getState().update((d) => removeLayer(d, id));
                        api.getState().select(null);
                      }}
                      onMove={(to) => move(id, to)}
                      markerName={markerName}
                      onPlacement={
                        hasMarker
                          ? () =>
                              api
                                .getState()
                                .update((d) =>
                                  patchLayer(d, id, (layer) => ({ ...layer, placement: layer.placement === "below" ? "above" : "below" }))
                                )
                          : undefined
                      }
                    />
                  )
                )}
              </ul>
            </SortableContext>
          </DndContext>
        )}
      </div>
    </div>
  );
}

function MarkerRow({ id, active, onSelect }: { id: string; active: boolean; onSelect: () => void }) {
  const { setNodeRef, transform, transition } = useSortable({ id, disabled: { draggable: true, droppable: false } });
  const plate = id === PLATE_ID;
  const Icon = plate ? TextCursorInput : CircleUserRound;
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "flex h-8 cursor-pointer items-center gap-2 rounded-md border border-dashed border-emerald-500/40 px-2 text-xs",
        active ? "bg-emerald-500/10" : "hover:bg-accent"
      )}
      onClick={onSelect}
    >
      <Icon className="size-3.5 text-emerald-600 dark:text-emerald-400" />
      <span className="font-medium">{plate ? "铭牌文字" : "头像"}</span>
      <span className="ml-auto text-[11px] text-muted-foreground">{plate ? "上方在文字之上" : "上方在头像之上"}</span>
    </li>
  );
}

function LayerRow({
  layer,
  active,
  locked,
  readOnly,
  renaming,
  onRename,
  onStartRename,
  onSelect,
  onToggleHidden,
  onToggleLock,
  onDuplicate,
  onDelete,
  onMove,
  markerName = "头像",
  onPlacement
}: {
  layer: Layer;
  active: boolean;
  locked: boolean;
  readOnly: boolean;
  renaming: boolean;
  onRename: (name: string | null) => void;
  onStartRename: () => void;
  onSelect: () => void;
  onToggleHidden: () => void;
  onToggleLock: () => void;
  onDuplicate: () => void;
  onDelete: () => void;
  onMove: (to: "top" | "bottom") => void;
  /** 分界行叫什么（头像 / 文字），「移到 X 之上」用 */
  markerName?: string;
  onPlacement?: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: layer.id,
    disabled: readOnly
  });
  const tracks = layer.tracks?.length ?? 0;
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "group flex h-8 items-center gap-1 rounded-md pr-1 pl-0.5 text-xs transition-colors",
        active ? "bg-primary/10 text-foreground" : "hover:bg-accent",
        isDragging && "z-10 bg-accent shadow-sm",
        layer.hidden && "text-muted-foreground"
      )}
      onClick={onSelect}
    >
      <button
        type="button"
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        className={cn("inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground/60", readOnly ? "invisible" : "cursor-grab")}
        aria-label="拖动调整层级"
        onClick={(event) => event.stopPropagation()}
      >
        <GripVertical className="size-3" />
      </button>
      <LayerIcon layer={layer} className="size-3.5 shrink-0 text-muted-foreground" />
      {renaming ? (
        <input
          autoFocus
          defaultValue={layer.name}
          maxLength={32}
          className="h-6 min-w-0 flex-1 rounded border bg-background px-1.5 text-xs outline-none"
          onClick={(event) => event.stopPropagation()}
          onBlur={(event) => onRename(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") onRename((event.target as HTMLInputElement).value);
            if (event.key === "Escape") onRename(null);
          }}
        />
      ) : (
        <span
          className="min-w-0 flex-1 truncate"
          onDoubleClick={(event) => {
            event.stopPropagation();
            if (!readOnly) onStartRename();
          }}
          title={`${layer.name || layer.id}（${LAYER_TYPE_LABEL[layer.type]}）`}
        >
          {layer.name || layer.id}
        </span>
      )}
      {tracks > 0 ? (
        <Badge variant="outline" size="sm" className="h-4 px-1 text-[10px] font-normal text-amber-600 dark:text-amber-400">
          {tracks}
        </Badge>
      ) : null}
      <button
        type="button"
        className={cn(
          "inline-flex size-5 shrink-0 items-center justify-center rounded hover:bg-background",
          locked ? "text-foreground" : "text-muted-foreground opacity-0 group-hover:opacity-100"
        )}
        onClick={(event) => {
          event.stopPropagation();
          onToggleLock();
        }}
        aria-label={locked ? "解锁" : "锁定"}
      >
        {locked ? <Lock className="size-3" /> : <LockOpen className="size-3" />}
      </button>
      <button
        type="button"
        disabled={readOnly}
        className={cn(
          "inline-flex size-5 shrink-0 items-center justify-center rounded hover:bg-background",
          layer.hidden ? "text-foreground" : "text-muted-foreground opacity-0 group-hover:opacity-100"
        )}
        onClick={(event) => {
          event.stopPropagation();
          onToggleHidden();
        }}
        aria-label={layer.hidden ? "显示" : "隐藏"}
      >
        {layer.hidden ? <EyeOff className="size-3" /> : <Eye className="size-3" />}
      </button>
      {!readOnly ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 group-hover:opacity-100 hover:bg-background data-[state=open]:opacity-100"
              onClick={(event) => event.stopPropagation()}
              aria-label="图层操作"
            >
              <MoreHorizontal className="size-3" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40" onClick={(event) => event.stopPropagation()}>
            <DropdownMenuItem onSelect={onStartRename}>重命名</DropdownMenuItem>
            <DropdownMenuItem onSelect={onDuplicate}>
              <Copy />
              复制
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onMove("top")}>
              <ArrowUpToLine />
              置顶
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onMove("bottom")}>
              <ArrowDownToLine />
              置底
            </DropdownMenuItem>
            {onPlacement ? (
              <DropdownMenuItem onSelect={onPlacement}>
                <CircleUserRound />
                {layer.placement === "below" ? `移到${markerName}之上` : `移到${markerName}之下`}
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 />
              删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </li>
  );
}
