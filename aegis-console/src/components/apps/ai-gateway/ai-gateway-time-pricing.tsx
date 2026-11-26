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
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  CalendarRange,
  ChevronDown,
  Clock3,
  Copy,
  GripVertical,
  Layers,
  Moon,
  PartyPopper,
  Plus,
  Search,
  Sun,
  Trash2,
  TrendingUp
} from "lucide-react";
import {
  TIME_TONES,
  TIME_TONE_KEYS,
  WEEKDAY_LABEL,
  discountLabel,
  formatFactor,
  newWindowId,
  parseClock,
  toneMeta,
  toneStrength,
  useNow,
  zonedNow,
  type WindowScope
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { PricingClock, WeekHeatmap, toneHours } from "@/components/apps/ai-gateway/ai-gateway-time-visual";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type {
  AIGatewayKindMeta,
  AIGatewayModel,
  AIGatewayTimePricing,
  AIGatewayTimeTone,
  AIGatewayTimeWindow
} from "@/lib/api/ai-gateway";
import { cn } from "@/lib/utils";

const MAX_WINDOWS = 32;
const MULTIPLIER_PRESETS = ["0.3", "0.5", "0.8", "1.2", "1.5", "2"];
const ALL_SCOPE = "__all__";

type Preset = { key: string; label: string; icon: typeof Moon; build: (today: string) => Omit<AIGatewayTimeWindow, "id"> };

const PRESETS: Preset[] = [
  {
    key: "night",
    label: "夜间闲时",
    icon: Moon,
    build: () => ({ name: "夜间闲时", tone: "off_peak", weekdays: [], start: "00:00", end: "08:00", multiplier: "0.5", models: [], kinds: [], from: null, to: null, enabled: true })
  },
  {
    key: "peak",
    label: "工作日高峰",
    icon: TrendingUp,
    build: () => ({ name: "工作日高峰", tone: "peak", weekdays: [1, 2, 3, 4, 5], start: "09:00", end: "18:00", multiplier: "1.2", models: [], kinds: [], from: null, to: null, enabled: true })
  },
  {
    key: "weekend",
    label: "周末特惠",
    icon: Sun,
    build: () => ({ name: "周末特惠", tone: "off_peak", weekdays: [6, 7], start: "00:00", end: "00:00", multiplier: "0.8", models: [], kinds: [], from: null, to: null, enabled: true })
  },
  {
    key: "promo",
    label: "限时活动",
    icon: PartyPopper,
    build: (today) => ({ name: "限时活动", tone: "promo", weekdays: [], start: "00:00", end: "00:00", multiplier: "0.3", models: [], kinds: [], from: today, to: today, enabled: true })
  }
];

/**
 * 时段价格：上面是这一周的价格长什么样（环形时钟 + 热力图），下面是时段列表（拖动排序，靠前的优先）。
 * 改动只进草稿，图随草稿即时变化；保存在计费方案的顶栏。
 */
export function TimePricingSection({
  value,
  onChange,
  timezone,
  models,
  kinds
}: {
  value: AIGatewayTimePricing;
  onChange: (next: AIGatewayTimePricing) => void;
  timezone: string;
  models: AIGatewayModel[];
  kinds: AIGatewayKindMeta[];
}) {
  const now = useNow(30_000);
  const zoned = zonedNow(new Date(now), timezone);
  const [scopeModel, setScopeModel] = useState<string>(ALL_SCOPE);
  const [pickedDay, setPickedDay] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);

  const selectedDay = pickedDay && pickedDay >= zoned.day ? pickedDay : zoned.day;
  const scope = useMemo<WindowScope>(() => {
    if (scopeModel === ALL_SCOPE) return {};
    const model = models.find((item) => item.model === scopeModel);
    return { model: scopeModel, kind: model?.kind };
  }, [scopeModel, models]);
  const effective = useMemo(() => (value.enabled ? value.windows : []), [value.enabled, value.windows]);
  const hours = useMemo(() => toneHours(effective, zoned.day, scope), [effective, zoned.day, scope]);
  const kindLabel = (key: string) => kinds.find((item) => item.key === key)?.label ?? key;

  const patchWindows = (windows: AIGatewayTimeWindow[]) => onChange({ ...value, windows });
  const patchWindow = (id: string, changes: Partial<AIGatewayTimeWindow>) =>
    patchWindows(value.windows.map((item) => (item.id === id ? { ...item, ...changes } : item)));

  const add = (preset?: Preset) => {
    if (value.windows.length >= MAX_WINDOWS) return;
    const id = newWindowId();
    const base = preset
      ? preset.build(zoned.day)
      : { name: "新时段", tone: "normal" as AIGatewayTimeTone, weekdays: [], start: "12:00", end: "14:00", multiplier: "1", models: [], kinds: [], from: null, to: null, enabled: true };
    onChange({ ...value, enabled: value.enabled || value.windows.length === 0, windows: [...value.windows, { id, ...base }] });
    setExpanded(id);
  };
  const duplicate = (window: AIGatewayTimeWindow) => {
    if (value.windows.length >= MAX_WINDOWS) return;
    const id = newWindowId();
    const index = value.windows.findIndex((item) => item.id === window.id);
    const windows = [...value.windows];
    windows.splice(index + 1, 0, { ...window, id, name: `${window.name} 副本` });
    patchWindows(windows);
    setExpanded(id);
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const ids = value.windows.map((item) => item.id);
    patchWindows(arrayMove(value.windows, ids.indexOf(String(active.id)), ids.indexOf(String(over.id))));
  };

  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 sm:px-5">
          <div className="flex min-w-0 items-center gap-2">
            <span className="grid size-8 place-items-center rounded-xl bg-violet-500/10 text-violet-600 ring-1 ring-inset ring-violet-500/20 dark:text-violet-400">
              <Clock3 className="size-4" />
            </span>
            <div className="min-w-0">
              <h3 className="text-sm font-semibold">这一周的价格</h3>
              <p className="text-[11px] text-muted-foreground">按 {timezone} 计时 · 点左侧日期切换时钟</p>
            </div>
          </div>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Select value={scopeModel} onValueChange={setScopeModel}>
              <SelectTrigger className="h-8 w-56 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_SCOPE}>对全部模型生效的时段</SelectItem>
                {models.map((model) => (
                  <SelectItem key={model.id} value={model.model}>
                    <span className="font-mono">{model.model}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </header>
        <div className="grid gap-5 p-4 sm:p-5 lg:grid-cols-[260px_minmax(0,1fr)]">
          <div className="space-y-3">
            <PricingClock windows={effective} day={selectedDay} today={zoned.day} nowMinute={zoned.minute} scope={scope} />
            <ToneHours hours={hours} />
          </div>
          <div className="min-w-0 space-y-4">
            {!value.enabled ? (
              <div className="rounded-xl border border-dashed px-3 py-2 text-xs text-muted-foreground">
                时段价格未开启：全天按标准价（×1）计。下面的时段保存后不生效，打开开关后才生效。
              </div>
            ) : null}
            <WeekHeatmap
              windows={effective}
              today={zoned.day}
              nowMinute={zoned.minute}
              scope={scope}
              selectedDay={selectedDay}
              onSelectDay={setPickedDay}
            />
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t border-dashed pt-3 text-[11px]">
              <span className="flex items-center gap-1.5 text-muted-foreground">
                <span className="size-2.5 rounded-sm bg-muted ring-1 ring-inset ring-border" />
                标准价 ×1
              </span>
              {effective
                .filter((item) => item.enabled)
                .map((item, index) => {
                  const tone = toneMeta(item.tone);
                  return (
                    <span key={item.id} className="flex items-center gap-1.5">
                      <span className="font-mono text-[10px] text-muted-foreground">#{index + 1}</span>
                      <span className={cn("size-2.5 rounded-sm", tone.solid)} style={{ opacity: toneStrength(Number(item.multiplier)) }} />
                      {item.name}
                      <span className={cn("font-mono", tone.text)}>{formatFactor(item.multiplier)}</span>
                      {(item.models?.length ?? 0) > 0 || (item.kinds?.length ?? 0) > 0 ? (
                        <span className="text-muted-foreground">（限定）</span>
                      ) : null}
                    </span>
                  );
                })}
            </div>
          </div>
        </div>
      </section>

      <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 sm:px-5">
          <div className="flex min-w-0 items-center gap-2">
            <span className="grid size-8 place-items-center rounded-xl bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
              <Layers className="size-4" />
            </span>
            <div className="min-w-0">
              <h3 className="text-sm font-semibold">时段</h3>
              <p className="text-[11px] text-muted-foreground">
                同时命中时取列表里靠前的一个 · 拖动调整优先级 · {value.windows.length} / {MAX_WINDOWS}
              </p>
            </div>
          </div>
          <div className="ml-auto flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground">与会员折扣</span>
              <ToggleGroup
                type="single"
                size="sm"
                value={value.stacking}
                onValueChange={(next) => next && onChange({ ...value, stacking: next as AIGatewayTimePricing["stacking"] })}
              >
                <ToggleGroupItem value="multiply" className="h-7 px-2.5 text-xs">相乘</ToggleGroupItem>
                <ToggleGroupItem value="best" className="h-7 px-2.5 text-xs">取更优惠</ToggleGroupItem>
              </ToggleGroup>
            </div>
            <label className="flex items-center gap-2 text-xs">
              <Switch checked={value.enabled} onCheckedChange={(enabled) => onChange({ ...value, enabled })} />
              {value.enabled ? "已开启" : "未开启"}
            </label>
          </div>
        </header>
        <div className="space-y-3 p-4 sm:p-5">
          <p className="rounded-lg bg-muted/50 px-3 py-2 text-[11px] leading-relaxed text-muted-foreground">
            {value.stacking === "multiply"
              ? "相乘：会员 8 折遇到夜间 5 折，合计 4 折。"
              : "取更优惠：会员折扣与闲时折扣只取更低的一个（会员 8 折遇到夜间 5 折按 5 折）；高峰加价（倍率 > 1）总是相乘。"}
          </p>

          {value.windows.length === 0 ? (
            <div className="rounded-xl border border-dashed px-4 py-8 text-center text-xs text-muted-foreground">
              还没有时段。从下面的模板开始，或新建一个空白时段。
            </div>
          ) : (
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
              <SortableContext items={value.windows.map((item) => item.id)} strategy={verticalListSortingStrategy}>
                <div className="space-y-2">
                  {value.windows.map((window, index) => (
                    <WindowCard
                      key={window.id}
                      window={window}
                      index={index}
                      open={expanded === window.id}
                      onToggle={() => setExpanded(expanded === window.id ? null : window.id)}
                      onChange={(changes) => patchWindow(window.id, changes)}
                      onDuplicate={() => duplicate(window)}
                      onRemove={() => patchWindows(value.windows.filter((item) => item.id !== window.id))}
                      models={models}
                      kinds={kinds}
                      kindLabel={kindLabel}
                    />
                  ))}
                </div>
              </SortableContext>
            </DndContext>
          )}

          <div className="flex flex-wrap items-center gap-2 pt-1">
            {PRESETS.map((preset) => (
              <Button key={preset.key} size="sm" variant="outline" disabled={value.windows.length >= MAX_WINDOWS} onClick={() => add(preset)}>
                <preset.icon className="size-3.5" />
                {preset.label}
              </Button>
            ))}
            <Button size="sm" variant="secondary" disabled={value.windows.length >= MAX_WINDOWS} onClick={() => add()}>
              <Plus className="size-3.5" />
              空白时段
            </Button>
          </div>
        </div>
      </section>
    </div>
  );
}

function ToneHours({ hours }: { hours: Map<string, number> }) {
  const total = [...hours.values()].reduce((sum, value) => sum + value, 0) || 1;
  const entries: Array<{ key: string; label: string; solid: string }> = [
    ...TIME_TONE_KEYS.map((key) => ({ key, label: TIME_TONES[key].label, solid: TIME_TONES[key].solid })),
    { key: "standard", label: "标准价", solid: "bg-muted-foreground/30" }
  ];
  return (
    <div className="space-y-2 rounded-xl border border-border px-3 py-2.5">
      <div className="text-[11px] font-medium text-muted-foreground">未来 7 天的时长构成</div>
      <div className="flex h-2 overflow-hidden rounded-full bg-muted">
        {entries.map((entry) => {
          const value = hours.get(entry.key) ?? 0;
          if (!value) return null;
          return <span key={entry.key} className={entry.solid} style={{ width: `${(value / total) * 100}%` }} />;
        })}
      </div>
      <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[11px]">
        {entries.map((entry) => {
          const value = hours.get(entry.key) ?? 0;
          return (
            <span key={entry.key} className={cn("flex items-center gap-1.5", value === 0 && "text-muted-foreground/60")}>
              <span className={cn("size-2 rounded-full", entry.solid)} />
              {entry.label}
              <span className="ml-auto font-mono tabular-nums">{Number(value.toFixed(1))} 时</span>
            </span>
          );
        })}
      </div>
    </div>
  );
}

function describeWeekdays(weekdays: number[]) {
  if (weekdays.length === 0 || weekdays.length === 7) return "每天";
  const sorted = [...weekdays].sort((a, b) => a - b);
  if (sorted.join() === "1,2,3,4,5") return "工作日";
  if (sorted.join() === "6,7") return "周末";
  return `周${sorted.map((day) => WEEKDAY_LABEL[day]).join("、")}`;
}

function describeRange(window: AIGatewayTimeWindow) {
  const start = parseClock(window.start);
  const end = parseClock(window.end);
  if (start === null || end === null) return "时间无效";
  if (start === end) return "全天";
  return `${window.start}–${window.end}${end < start ? "（次日）" : ""}`;
}

function WindowCard({
  window,
  index,
  open,
  onToggle,
  onChange,
  onDuplicate,
  onRemove,
  models,
  kinds,
  kindLabel
}: {
  window: AIGatewayTimeWindow;
  index: number;
  open: boolean;
  onToggle: () => void;
  onChange: (changes: Partial<AIGatewayTimeWindow>) => void;
  onDuplicate: () => void;
  onRemove: () => void;
  models: AIGatewayModel[];
  kinds: AIGatewayKindMeta[];
  kindLabel: (key: string) => string;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: window.id });
  const tone = toneMeta(window.tone);
  const multiplier = Number(window.multiplier);
  const scoped = (window.models?.length ?? 0) > 0 || (window.kinds?.length ?? 0) > 0;
  const weekdays = window.weekdays ?? [];
  const dated = Boolean(window.from || window.to);
  const invalidTime = parseClock(window.start) === null || parseClock(window.end) === null;

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        "overflow-hidden rounded-xl border border-l-4 bg-background",
        tone.ring,
        isDragging && "relative z-10 shadow-lg",
        !window.enabled && "opacity-60"
      )}
    >
      <div className="flex items-center gap-2 px-2 py-2 sm:px-3">
        <button
          type="button"
          className="grid size-7 shrink-0 cursor-grab place-items-center rounded-md text-muted-foreground hover:bg-muted active:cursor-grabbing"
          aria-label="拖动排序"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
        <span className="w-6 shrink-0 font-mono text-[10px] text-muted-foreground">#{index + 1}</span>
        <button type="button" onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-3 text-left">
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-2">
              <span className="truncate text-sm font-medium">{window.name || "未命名"}</span>
              <span className={cn("rounded px-1.5 py-px text-[10px]", tone.soft)}>{tone.label}</span>
            </span>
            <span className={cn("mt-0.5 flex flex-wrap gap-x-2 text-[11px] text-muted-foreground", invalidTime && "text-destructive")}>
              <span className="font-mono">{describeRange(window)}</span>
              <span>{describeWeekdays(weekdays)}</span>
              {dated ? (
                <span className="flex items-center gap-1">
                  <CalendarRange className="size-3" />
                  {window.from || "…"} 至 {window.to || "…"}
                </span>
              ) : null}
              {scoped ? (
                <span>
                  限
                  {[
                    (window.models?.length ?? 0) > 0 ? `${window.models?.length} 个模型` : null,
                    (window.kinds ?? []).map(kindLabel).join("、") || null
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              ) : null}
            </span>
          </span>
          <span className="shrink-0 text-right">
            <span className={cn("block font-mono text-base font-semibold tabular-nums", tone.text)}>{formatFactor(window.multiplier)}</span>
            <span className="block text-[10px] text-muted-foreground">{discountLabel(multiplier)}</span>
          </span>
        </button>
        <Switch checked={window.enabled} onCheckedChange={(enabled) => onChange({ enabled })} aria-label="启用" />
        <Button size="icon" variant="ghost" className="size-7" aria-label={open ? "收起" : "展开"} onClick={onToggle}>
          <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
        </Button>
      </div>

      {open ? (
        <div className="space-y-4 border-t border-dashed bg-muted/20 px-3 py-4 sm:px-4">
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
            <Field label="名称">
              <Input className="h-8 text-xs" maxLength={32} value={window.name} onChange={(event) => onChange({ name: event.target.value })} />
            </Field>
            <Field label="色调">
              <div className="flex gap-1">
                {TIME_TONE_KEYS.map((key) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => onChange({ tone: key })}
                    className={cn(
                      "flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-xs transition-colors",
                      window.tone === key ? "border-foreground/40 bg-background shadow-sm" : "border-transparent hover:bg-muted"
                    )}
                  >
                    <span className={cn("size-2.5 rounded-full", TIME_TONES[key].solid)} />
                    {TIME_TONES[key].label}
                  </button>
                ))}
              </div>
            </Field>
          </div>

          <div className="grid gap-3 lg:grid-cols-2">
            <Field label="时间" hint={describeRange(window)}>
              <div className="flex items-center gap-2">
                <Input type="time" className="h-8 w-28 font-mono text-xs" value={window.start} onChange={(event) => onChange({ start: event.target.value })} />
                <span className="text-xs text-muted-foreground">至</span>
                <Input type="time" className="h-8 w-28 font-mono text-xs" value={window.end} onChange={(event) => onChange({ end: event.target.value })} />
                <Button size="xs" variant="ghost" onClick={() => onChange({ start: "00:00", end: "00:00" })}>
                  全天
                </Button>
              </div>
            </Field>
            <Field label="星期" hint={`${describeWeekdays(weekdays)} · 跨零点按开始那天算`}>
              <div className="flex flex-wrap items-center gap-1">
                {[1, 2, 3, 4, 5, 6, 7].map((day) => {
                  const on = weekdays.includes(day);
                  return (
                    <button
                      key={day}
                      type="button"
                      onClick={() =>
                        onChange({
                          weekdays: on ? weekdays.filter((item) => item !== day) : [...weekdays, day].sort((a, b) => a - b)
                        })
                      }
                      className={cn(
                        "grid size-8 place-items-center rounded-lg border text-xs transition-colors",
                        on ? cn("border-transparent text-white", tone.solid) : "hover:bg-muted"
                      )}
                    >
                      {WEEKDAY_LABEL[day]}
                    </button>
                  );
                })}
                <span className="mx-1 h-5 w-px bg-border" />
                <Button size="xs" variant="ghost" onClick={() => onChange({ weekdays: [] })}>每天</Button>
                <Button size="xs" variant="ghost" onClick={() => onChange({ weekdays: [1, 2, 3, 4, 5] })}>工作日</Button>
                <Button size="xs" variant="ghost" onClick={() => onChange({ weekdays: [6, 7] })}>周末</Button>
              </div>
            </Field>
          </div>

          <Field label="倍率" hint="0.01–100：小于 1 打折，大于 1 加价">
            <div className="flex flex-wrap items-center gap-2">
              <Input
                className="h-8 w-24 font-mono text-xs"
                inputMode="decimal"
                value={window.multiplier}
                onChange={(event) => onChange({ multiplier: event.target.value })}
              />
              <span className={cn("min-w-16 text-xs font-medium", tone.text)}>{discountLabel(multiplier)}</span>
              {MULTIPLIER_PRESETS.map((preset) => (
                <Button
                  key={preset}
                  size="xs"
                  variant={window.multiplier === preset ? "secondary" : "outline"}
                  onClick={() => onChange({ multiplier: preset })}
                >
                  ×{preset}
                </Button>
              ))}
            </div>
          </Field>

          <div className="grid gap-3 lg:grid-cols-2">
            <Field label="限定模型" hint="不选表示全部模型">
              <ModelPicker models={models} value={window.models ?? []} onChange={(next) => onChange({ models: next })} />
            </Field>
            <Field label="限定种类" hint="不选表示全部种类；与模型同时填时两者都要满足">
              <div className="flex flex-wrap gap-1">
                {kinds.map((kind) => {
                  const on = (window.kinds ?? []).includes(kind.key);
                  return (
                    <button
                      key={kind.key}
                      type="button"
                      onClick={() =>
                        onChange({ kinds: on ? (window.kinds ?? []).filter((item) => item !== kind.key) : [...(window.kinds ?? []), kind.key] })
                      }
                      className={cn(
                        "h-7 rounded-full border px-2.5 text-[11px] transition-colors",
                        on ? "border-foreground/30 bg-foreground text-background" : "hover:bg-muted"
                      )}
                    >
                      {kind.label}
                    </button>
                  );
                })}
              </div>
            </Field>
          </div>

          <Field label="活动日期" hint="含当天；留空表示不限，跨零点按开始那天判断">
            <div className="flex flex-wrap items-center gap-2">
              <Input type="date" className="h-8 w-40 font-mono text-xs" value={window.from ?? ""} onChange={(event) => onChange({ from: event.target.value || null })} />
              <span className="text-xs text-muted-foreground">至</span>
              <Input type="date" className="h-8 w-40 font-mono text-xs" value={window.to ?? ""} onChange={(event) => onChange({ to: event.target.value || null })} />
              {dated ? (
                <Button size="xs" variant="ghost" onClick={() => onChange({ from: null, to: null })}>
                  不限日期
                </Button>
              ) : null}
            </div>
          </Field>

          <div className="flex items-center justify-between gap-2 border-t border-dashed pt-3">
            <span className="font-mono text-[10px] text-muted-foreground">id: {window.id}</span>
            <div className="flex gap-1.5">
              <Button size="xs" variant="outline" onClick={onDuplicate}>
                <Copy className="size-3" />
                复制
              </Button>
              <Button size="xs" variant="outline" className="text-destructive hover:text-destructive" onClick={onRemove}>
                <Trash2 className="size-3" />
                删除
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {hint ? <span className="truncate text-[10px] text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </div>
  );
}

/** 多选模型：弹出层里按名字过滤、勾选。 */
export function ModelPicker({
  models,
  value,
  onChange
}: {
  models: AIGatewayModel[];
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const needle = keyword.trim().toLowerCase();
  const filtered = models.filter((item) => !needle || item.model.toLowerCase().includes(needle) || item.displayName?.toLowerCase().includes(needle));
  const known = new Set(models.map((item) => item.model));
  return (
    <div className="flex flex-wrap items-center gap-1">
      {value.map((name) => (
        <Badge key={name} variant={known.has(name) ? "secondary" : "warning"} size="sm" className="gap-1 font-mono">
          {name}
          <button type="button" aria-label={`移除 ${name}`} className="opacity-60 hover:opacity-100" onClick={() => onChange(value.filter((item) => item !== name))}>
            ×
          </button>
        </Badge>
      ))}
      <Popover>
        <PopoverTrigger asChild>
          <Button size="xs" variant="outline">
            <Plus className="size-3" />
            {value.length === 0 ? "选择模型" : "增减"}
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-0">
          <div className="flex items-center gap-2 border-b px-3 py-2">
            <Search className="size-3.5 text-muted-foreground" />
            <input
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              placeholder="搜索模型"
              className="h-6 w-full bg-transparent text-xs outline-none"
            />
          </div>
          <div className="max-h-64 overflow-y-auto p-1">
            {filtered.length === 0 ? (
              <p className="px-2 py-4 text-center text-[11px] text-muted-foreground">没有匹配的模型</p>
            ) : (
              filtered.map((model) => {
                const on = value.includes(model.model);
                return (
                  <label key={model.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted">
                    <Checkbox
                      checked={on}
                      onCheckedChange={(checked) => onChange(checked === true ? [...value, model.model] : value.filter((item) => item !== model.model))}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-xs">{model.model}</span>
                      {model.displayName ? <span className="block truncate text-[10px] text-muted-foreground">{model.displayName}</span> : null}
                    </span>
                  </label>
                );
              })
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
