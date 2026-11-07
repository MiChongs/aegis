"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import NumberFlow from "@number-flow/react";
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
import { toast } from "sonner";
import { FilePlus2, GripVertical, ListFilter, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { CardEmpty } from "@/components/overview/overview-kit";
import type { ResponseSpec, RuleStats, RuleTemplate, TrafficRule } from "@/lib/api/traffic";
import {
  useDeleteTrafficRuleMutation,
  useReorderTrafficRulesMutation,
  useRuleRateRing,
  useToggleTrafficRuleMutation,
  useTrafficConfigQuery,
  useTrafficOverviewQuery,
  useTrafficTemplatesQuery
} from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { RulePipeline } from "./rule-pipeline";
import { ClassBadge, ModeBadge, Panel, Spark, TabStack } from "./traffic-kit";
import { MODE_ORDER, fmtNum, normalizeRuleShape, rejectedOf } from "./traffic-format";

export function TrafficRulesTab() {
  const configQuery = useTrafficConfigQuery();
  const overviewQuery = useTrafficOverviewQuery(60);
  const reorder = useReorderTrafficRulesMutation();
  const toggle = useToggleTrafficRuleMutation();
  const remove = useDeleteTrafficRuleMutation();
  const [galleryOpen, setGalleryOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<TrafficRule | null>(null);

  const config = configQuery.data;
  const rules = useMemo(() => (config?.rules ?? []).map(normalizeRuleShape), [config?.rules]);
  const statsById = useMemo(
    () => new Map((overviewQuery.data?.local.rules ?? []).map((stat) => [stat.id, stat])),
    [overviewQuery.data?.local.rules]
  );

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const ids = rules.map((rule) => rule.id);
    const next = arrayMove(ids, ids.indexOf(String(active.id)), ids.indexOf(String(over.id)));
    reorder.mutate(next);
  };

  const counts = MODE_ORDER.map((mode) => ({
    mode,
    count: rules.filter((rule) => rule.enabled && rule.mode === mode).length
  }));

  return (
    <TabStack>
      <div className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-3 rounded-2xl border border-border bg-card px-4 py-3 sm:px-5">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <span className="text-2xl leading-7 font-semibold tracking-tight tabular-nums">{rules.length}</span>
          <span className="text-xs text-muted-foreground">条规则</span>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
          {counts.map(({ mode, count }) => (
            <span key={mode} className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <ModeBadge mode={mode} />
              <span className="font-medium text-foreground tabular-nums">{count}</span>
            </span>
          ))}
          {rules.some((rule) => !rule.enabled) ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <Badge variant="outline" size="sm">停用</Badge>
              <span className="font-medium text-foreground tabular-nums">{rules.filter((rule) => !rule.enabled).length}</span>
            </span>
          ) : null}
        </div>
        <Button size="sm" className="ml-auto" onClick={() => setGalleryOpen(true)}>
          <Plus />
          新建规则
        </Button>
      </div>

      {configQuery.isLoading ? (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-36 rounded-2xl" />
          ))}
        </div>
      ) : rules.length === 0 ? (
        <Panel icon={ListFilter} title="规则">
          <CardEmpty icon={ListFilter} title="暂无规则" />
        </Panel>
      ) : (
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={rules.map((rule) => rule.id)} strategy={verticalListSortingStrategy}>
            <ol className="flex flex-col gap-3">
              {rules.map((rule, index) => (
                <RuleCard
                  key={rule.id}
                  rule={rule}
                  index={index}
                  stats={statsById.get(rule.id)}
                  defaultResponse={config?.settings.response}
                  dragDisabled={reorder.isPending}
                  onToggle={(enabled) => toggle.mutate({ ruleId: rule.id, enabled })}
                  onDelete={() => setPendingDelete(rule)}
                />
              ))}
            </ol>
          </SortableContext>
        </DndContext>
      )}

      <TemplateGallery open={galleryOpen} onOpenChange={setGalleryOpen} defaultResponse={config?.settings.response} />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除规则「{pendingDelete?.name}」</AlertDialogTitle>
            <AlertDialogDescription>删除后立即对全部实例生效。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (pendingDelete) remove.mutate(pendingDelete.id, { onSuccess: () => toast.success("规则已删除") });
                setPendingDelete(null);
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

/* ── 规则卡片 ── */

function RuleCard({
  rule,
  index,
  stats,
  defaultResponse,
  dragDisabled,
  onToggle,
  onDelete
}: {
  rule: TrafficRule;
  index: number;
  stats?: RuleStats;
  defaultResponse?: ResponseSpec;
  dragDisabled: boolean;
  onToggle: (enabled: boolean) => void;
  onDelete: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: rule.id,
    disabled: dragDisabled
  });
  const ring = useRuleRateRing(rule.id);
  const rps = stats?.lastSecond.arrivals ?? 0;
  const rejected = rejectedOf(stats?.lastSecond);
  const href = `/traffic/rules/${encodeURIComponent(rule.id)}`;

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "relative flex min-w-0 flex-col rounded-2xl border border-border bg-card",
        isDragging && "z-10 border-foreground/30 bg-card",
        !rule.enabled && "bg-muted/30"
      )}
    >
      <div className="flex min-w-0 items-start gap-2 px-3 pt-3 sm:gap-3 sm:px-4 sm:pt-4">
        <button
          type="button"
          ref={setActivatorNodeRef}
          {...attributes}
          {...listeners}
          aria-label={`拖动排序 ${rule.name}`}
          className="grid h-8 w-6 shrink-0 cursor-grab touch-none place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground active:cursor-grabbing"
        >
          <GripVertical className="size-4" />
        </button>
        <span className="mt-1 shrink-0 font-mono text-xs text-muted-foreground tabular-nums">{String(index + 1).padStart(2, "0")}</span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <Link href={href} className="min-w-0 truncate text-sm font-semibold hover:underline">
              {rule.name || "未命名规则"}
            </Link>
            <ModeBadge mode={rule.mode} />
            <ClassBadge value={rule.class} />
            {rule.terminal ? (
              <Badge variant="outline" size="sm">
                命中即止
              </Badge>
            ) : null}
            {!rule.enabled ? (
              <Badge variant="outline" size="sm" className="text-muted-foreground">
                停用
              </Badge>
            ) : null}
          </div>
          {rule.description ? <p className="mt-0.5 truncate text-xs text-muted-foreground">{rule.description}</p> : null}
        </div>
        <Switch checked={rule.enabled} onCheckedChange={onToggle} aria-label={rule.enabled ? "停用" : "启用"} className="mt-1.5" />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon-sm" variant="ghost" aria-label="更多">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem asChild>
              <Link href={href}>
                <Pencil />
                编辑
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 />
              删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className="flex min-w-0 flex-wrap items-end gap-x-4 gap-y-3 px-3 pt-3 pb-3 sm:px-4 sm:pb-4 sm:pl-[68px]">
        <RulePipeline rule={rule} defaultResponse={defaultResponse} className={cn("min-w-0 flex-1", !rule.enabled && "opacity-60")} />
        <div className="ml-auto flex shrink-0 items-center gap-3">
          <Spark values={ring} width={88} height={28} className={rejected > 0 ? "text-rose-500" : "text-sky-500"} />
          <div className="flex min-w-14 flex-col items-end">
            <span className="text-base leading-5 font-semibold tabular-nums">
              <NumberFlow value={rps} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
            </span>
            <span className={cn("text-[11px] tabular-nums", rejected > 0 ? "text-rose-600 dark:text-rose-400" : "text-muted-foreground")}>
              {rejected > 0 ? `拒 ${fmtNum(rejected)}` : "/秒"}
            </span>
          </div>
        </div>
      </div>
    </li>
  );
}

/* ── 模板库 ── */

function TemplateGallery({
  open,
  onOpenChange,
  defaultResponse
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultResponse?: ResponseSpec;
}) {
  const templatesQuery = useTrafficTemplatesQuery(open);
  const groups = useMemo(() => {
    const map = new Map<string, RuleTemplate[]>();
    for (const template of templatesQuery.data ?? []) {
      const key = template.category || "其他";
      map.set(key, [...(map.get(key) ?? []), template]);
    }
    return [...map.entries()];
  }, [templatesQuery.data]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(88vh,820px)] flex-col gap-0 p-0 sm:max-w-3xl">
        <DialogHeader className="border-b border-border px-5 py-4">
          <DialogTitle>新建规则</DialogTitle>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <Link
            href="/traffic/rules/new"
            className="flex min-w-0 items-center gap-3 rounded-xl border border-dashed border-border px-4 py-3 transition-colors hover:border-foreground/40 hover:bg-accent"
          >
            <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
              <FilePlus2 className="size-4" />
            </span>
            <span className="text-sm font-medium">空白规则</span>
          </Link>
          {templatesQuery.isLoading ? (
            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-32 rounded-xl" />
              ))}
            </div>
          ) : (
            groups.map(([category, templates]) => (
              <section key={category} className="mt-5">
                <h4 className="mb-2 text-xs font-medium text-muted-foreground">{category}</h4>
                <div className="grid gap-3 sm:grid-cols-2">
                  {templates.map((template) => {
                    const rule = normalizeRuleShape(template.rule);
                    return (
                      <Link
                        key={template.id}
                        href={`/traffic/rules/new?template=${encodeURIComponent(template.id)}`}
                        className="flex min-w-0 flex-col gap-2.5 rounded-xl border border-border bg-card p-3 transition-colors hover:border-foreground/40 hover:bg-accent/40"
                      >
                        <div className="flex min-w-0 items-center gap-1.5">
                          <span className="min-w-0 flex-1 truncate text-sm font-semibold">{template.name}</span>
                          <ModeBadge mode={rule.mode} />
                        </div>
                        {template.description ? (
                          <p className="line-clamp-2 text-xs text-muted-foreground">{template.description}</p>
                        ) : null}
                        <RulePipeline rule={rule} defaultResponse={defaultResponse} />
                      </Link>
                    );
                  })}
                </div>
              </section>
            ))
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

