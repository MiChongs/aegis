"use client";

import { useMemo, useState } from "react";
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  GripVertical,
  ListFilter,
  MoreHorizontal,
  Pencil,
  Plus,
  ScrollText,
  Sparkles,
  Trash2
} from "lucide-react";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { TabStack } from "@/components/traffic/traffic-kit";
import { RULE_ACTION_META, ToneBadge, formatBanSeconds } from "@/components/firewall/firewall-kit";
import type { FirewallRule, FirewallRuleAction, FirewallRuleInput } from "@/lib/api/firewall";
import { useIPBanModesQuery } from "@/lib/admin-hooks";
import { formatRelative } from "@/lib/datetime";
import {
  useDeleteFirewallRuleMutation,
  useFirewallRulesQuery,
  useFirewallRuntimeQuery,
  useReorderFirewallRulesMutation,
  useToggleFirewallRuleMutation
} from "@/lib/firewall-hooks";
import { useFirewallIntents } from "@/lib/firewall-store";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { ExprCode, ExprTokens, RULE_TEMPLATES } from "./rule-expression";
import { RuleEditorDialog, type RuleEditorTarget } from "./rule-editor-dialog";

type FirewallTabKey = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/**
 * 自定义规则页签（docs/firewall.md §5）。
 *
 * 列表顺序就是求值顺序（priority 升序，同优先级按 ID）。拖动或上下移动后把整串 ID 交给
 * `rules/reorder`，后端按新顺序重写 priority；请求在途时先按本地顺序显示，免得列表跳回去再跳过来。
 */
export function FirewallRulesTab({ onNavigate }: { onNavigate: (tab: FirewallTabKey) => void }) {
  const rulesQuery = useFirewallRulesQuery();
  const runtime = useFirewallRuntimeQuery();
  const banModes = useIPBanModesQuery();
  const toggle = useToggleFirewallRuleMutation();
  const remove = useDeleteFirewallRuleMutation();
  const reorder = useReorderFirewallRulesMutation();
  const canWrite = usePermissionChecker()("system:firewall:policy");
  const setEventsFilter = useFirewallIntents((state) => state.setEventsFilter);

  const [editor, setEditor] = useState<RuleEditorTarget | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<FirewallRule | null>(null);
  const [pendingOrder, setPendingOrder] = useState<number[] | null>(null);

  const sorted = useMemo(
    () => [...(rulesQuery.data ?? [])].sort((a, b) => a.priority - b.priority || a.id - b.id),
    [rulesQuery.data]
  );
  const rules = useMemo(() => {
    if (!pendingOrder) return sorted;
    const byId = new Map(sorted.map((rule) => [rule.id, rule]));
    const ordered = pendingOrder.map((id) => byId.get(id)).filter((rule): rule is FirewallRule => Boolean(rule));
    return ordered.length === sorted.length ? ordered : sorted;
  }, [sorted, pendingOrder]);

  const modeLabel = useMemo(() => {
    const map = new Map((banModes.data?.modes ?? []).map((mode) => [mode.value as string, mode.label]));
    return (mode: string) => (mode ? (map.get(mode) ?? mode) : "403 JSON");
  }, [banModes.data]);

  const ruleErrors = runtime.data?.ruleErrors ?? [];
  const nextPriority = sorted.length ? Math.max(...sorted.map((rule) => rule.priority)) + 10 : 100;

  const openEditor = (rule?: FirewallRule, seed?: FirewallRuleInput) => {
    setEditor({
      key: Date.now(),
      rule,
      seed: seed ? { ...seed, priority: seed.priority ?? nextPriority } : { name: "", expression: "", action: "block", priority: nextPriority }
    });
    setEditorOpen(true);
  };

  const commitOrder = (ids: number[]) => {
    setPendingOrder(ids);
    reorder.mutate(ids, { onSettled: () => setPendingOrder(null) });
  };

  const move = (index: number, delta: number) => {
    const ids = rules.map((rule) => rule.id);
    const target = index + delta;
    if (target < 0 || target >= ids.length) return;
    commitOrder(arrayMove(ids, index, target));
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const ids = rules.map((rule) => rule.id);
    commitOrder(arrayMove(ids, ids.indexOf(Number(active.id)), ids.indexOf(Number(over.id))));
  };

  const counts = (Object.keys(RULE_ACTION_META) as FirewallRuleAction[]).map((action) => ({
    action,
    count: rules.filter((rule) => rule.enabled && rule.action === action).length
  }));
  const disabledCount = rules.filter((rule) => !rule.enabled).length;

  return (
    <TabStack>
      {ruleErrors.length > 0 ? (
        <div className="flex min-w-0 items-start gap-2.5 rounded-xl border border-amber-500/30 bg-amber-500/10 px-3.5 py-2.5 text-amber-800 dark:text-amber-300">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium">{ruleErrors.length} 条规则在本副本编译失败，未生效</p>
            <ul className="mt-1 flex flex-col gap-0.5">
              {ruleErrors.map((error, index) => (
                <li key={index} className="font-mono text-[11px] leading-4 break-all">
                  {error}
                </li>
              ))}
            </ul>
          </div>
        </div>
      ) : null}

      <div className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-3 rounded-2xl border border-border bg-card px-4 py-3 sm:px-5">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <span className="text-2xl leading-7 font-semibold tracking-tight tabular-nums">{rules.length}</span>
          <span className="text-xs text-muted-foreground">条规则</span>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
          {counts.map(({ action, count }) => (
            <span key={action} className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <ToneBadge tone={RULE_ACTION_META[action].tone}>{RULE_ACTION_META[action].label}</ToneBadge>
              <span className="font-medium text-foreground tabular-nums">{count}</span>
            </span>
          ))}
          {disabledCount ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
              <Badge variant="outline" size="sm">
                停用
              </Badge>
              <span className="font-medium text-foreground tabular-nums">{disabledCount}</span>
            </span>
          ) : null}
        </div>
        {canWrite ? (
          <Button size="sm" className="ml-auto" onClick={() => openEditor()}>
            <Plus />
            新建规则
          </Button>
        ) : null}
      </div>

      {rulesQuery.isLoading ? (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 3 }).map((_, index) => (
            <Skeleton key={index} className="h-32 rounded-2xl" />
          ))}
        </div>
      ) : rules.length === 0 ? (
        <RulesEmpty canWrite={canWrite} onTemplate={(seed) => openEditor(undefined, seed)} onBlank={() => openEditor()} />
      ) : (
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={rules.map((rule) => rule.id)} strategy={verticalListSortingStrategy}>
            <ol className="flex flex-col gap-3">
              {rules.map((rule, index) => (
                <RuleCard
                  key={rule.id}
                  rule={rule}
                  index={index}
                  last={index === rules.length - 1}
                  modeLabel={modeLabel(rule.blockMode)}
                  canWrite={canWrite}
                  dragDisabled={!canWrite || reorder.isPending}
                  onToggle={(enabled) => toggle.mutate({ id: rule.id, enabled })}
                  onEdit={() => openEditor(rule)}
                  onMove={(delta) => move(index, delta)}
                  onEvents={() => {
                    setEventsFilter({ ruleRef: rule.id });
                    onNavigate("events");
                  }}
                  onDelete={() => setPendingDelete(rule)}
                />
              ))}
            </ol>
          </SortableContext>
        </DndContext>
      )}

      <RuleEditorDialog target={editor} open={editorOpen} onOpenChange={setEditorOpen} />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除规则「{pendingDelete?.name}」</AlertDialogTitle>
            <AlertDialogDescription>删除后立即在所有副本生效，命中计数一并清除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (pendingDelete) remove.mutate(pendingDelete.id);
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
  last,
  modeLabel,
  canWrite,
  dragDisabled,
  onToggle,
  onEdit,
  onMove,
  onEvents,
  onDelete
}: {
  rule: FirewallRule;
  index: number;
  last: boolean;
  modeLabel: string;
  canWrite: boolean;
  dragDisabled: boolean;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onMove: (delta: number) => void;
  onEvents: () => void;
  onDelete: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: rule.id,
    disabled: dragDisabled
  });
  const meta = RULE_ACTION_META[rule.action] ?? RULE_ACTION_META.block;

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "relative flex min-w-0 flex-col gap-2.5 rounded-2xl border border-border bg-card px-3 py-3 sm:px-4",
        isDragging && "z-10 border-foreground/30",
        !rule.enabled && "bg-muted/30"
      )}
    >
      <div className="flex min-w-0 items-start gap-2 sm:gap-3">
        {canWrite ? (
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
        ) : null}
        <span className="mt-1.5 shrink-0 font-mono text-xs text-muted-foreground tabular-nums">{String(index + 1).padStart(2, "0")}</span>
        <div className="min-w-0 flex-1 pt-0.5">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {canWrite ? (
              <button type="button" onClick={onEdit} className="min-w-0 truncate text-left text-sm font-semibold hover:underline">
                {rule.name || "未命名规则"}
              </button>
            ) : (
              <span className="min-w-0 truncate text-sm font-semibold">{rule.name || "未命名规则"}</span>
            )}
            <ToneBadge tone={meta.tone}>{meta.label}</ToneBadge>
            {!rule.enabled ? (
              <Badge variant="outline" size="sm" className="text-muted-foreground">
                停用
              </Badge>
            ) : null}
          </div>
          {rule.description ? <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{rule.description}</p> : null}
        </div>
        <Switch
          checked={rule.enabled}
          onCheckedChange={onToggle}
          disabled={!canWrite}
          aria-label={rule.enabled ? "停用" : "启用"}
          className="mt-1.5"
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon-sm" variant="ghost" aria-label="更多">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onEvents}>
              <ScrollText />
              查看命中事件
            </DropdownMenuItem>
            {canWrite ? (
              <>
                <DropdownMenuItem onSelect={onEdit}>
                  <Pencil />
                  编辑
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onMove(-1)} disabled={index === 0 || dragDisabled}>
                  <ArrowUp />
                  上移
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onMove(1)} disabled={last || dragDisabled}>
                  <ArrowDown />
                  下移
                </DropdownMenuItem>
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

      <div className={cn("flex min-w-0 flex-col gap-2", canWrite ? "sm:pl-[60px]" : "sm:pl-7")}>
        <ExprCode source={rule.expression} className={cn(!rule.enabled && "opacity-60")} />
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <span>
            命中 <span className="font-medium text-foreground tabular-nums">{rule.hits.toLocaleString()}</span>
          </span>
          <span>最近 {rule.lastHitAt ? formatRelative(rule.lastHitAt) : "从未"}</span>
          {rule.action === "block" ? (
            <>
              <span>
                响应 <span className="text-foreground">{modeLabel}</span>
              </span>
              <span className={cn(rule.banSeconds !== 0 && "text-rose-600 dark:text-rose-400")}>
                {rule.banSeconds === 0 ? "不封禁" : `封禁 ${formatBanSeconds(rule.banSeconds)}`}
              </span>
            </>
          ) : null}
          <span className="tabular-nums">优先级 {rule.priority}</span>
        </div>
      </div>
    </li>
  );
}

/* ── 空状态 ── */

function RulesEmpty({ canWrite, onTemplate, onBlank }: { canWrite: boolean; onTemplate: (seed: FirewallRuleInput) => void; onBlank: () => void }) {
  const quick = RULE_TEMPLATES.slice(0, 3);
  return (
    <section className="flex min-w-0 flex-col gap-4 rounded-2xl border border-border bg-card px-4 py-5 sm:px-6 sm:py-6">
      <div className="flex min-w-0 items-start gap-3">
        <TonalIcon icon={ListFilter} tone="sky" size="lg" />
        <div className="min-w-0">
          <h3 className="text-sm font-semibold">暂无自定义规则</h3>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            自定义规则在入口段按顺序求值：用表达式描述请求（IP、路径、方法、请求头、归属地、时间），命中即拦截、放行、跳过 WAF 或记录。它在解密之前执行，被拦下的请求不会消耗签名校验与解密。
          </p>
        </div>
      </div>
      {canWrite ? (
        <>
          <div className="grid min-w-0 gap-2.5 md:grid-cols-3">
            {quick.map((tpl) => (
              <button
                key={tpl.key}
                type="button"
                onClick={() => onTemplate(tpl.input)}
                className="flex min-w-0 flex-col gap-2 rounded-xl border border-border p-3 text-left transition-colors hover:border-foreground/30 hover:bg-accent/40"
              >
                <span className="flex min-w-0 items-center gap-1.5">
                  <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-xs font-semibold">{tpl.name}</span>
                  <ToneBadge tone={RULE_ACTION_META[tpl.input.action].tone}>{RULE_ACTION_META[tpl.input.action].label}</ToneBadge>
                </span>
                <code className="block font-mono text-[11px] leading-4 break-all whitespace-pre-wrap">
                  <ExprTokens source={tpl.input.expression} />
                </code>
              </button>
            ))}
          </div>
          <div>
            <Button size="sm" variant="outline" onClick={onBlank}>
              <Plus />
              空白规则
            </Button>
          </div>
        </>
      ) : null}
    </section>
  );
}
