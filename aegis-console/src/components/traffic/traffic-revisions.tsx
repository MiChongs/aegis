"use client";

import { useMemo, useState } from "react";
import { ArrowRight, GitCompare, History, Loader2, RotateCcw } from "lucide-react";
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
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { CardEmpty } from "@/components/overview/overview-kit";
import type { RevisionSummary, TrafficConfig } from "@/lib/api/traffic";
import { formatDateTimeMinute } from "@/lib/datetime";
import {
  useRollbackTrafficMutation,
  useTrafficConfigQuery,
  useTrafficRevisionQuery,
  useTrafficRevisionsQuery,
  trafficErrorMessage
} from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { diffConfigs, type FieldChange, type RuleChange } from "./traffic-diff";
import { Panel, TabStack } from "./traffic-kit";
import { ACTION_LABEL } from "./traffic-format";

export function TrafficRevisionsTab() {
  const revisionsQuery = useTrafficRevisionsQuery();
  const configQuery = useTrafficConfigQuery();
  const [picked, setPicked] = useState<number | null>(null);
  const revisions = revisionsQuery.data ?? [];
  const current = configQuery.data?.revision;
  const selected = picked ?? revisions.find((item) => item.revision !== current)?.revision ?? revisions[0]?.revision ?? null;

  return (
    <TabStack>
      <div className="grid min-w-0 items-start gap-4 @4xl:grid-cols-[minmax(0,320px)_minmax(0,1fr)]">
        <Panel icon={History} tone="primary" title="版本" aside={<span className="text-xs text-muted-foreground tabular-nums">{revisions.length}</span>} bodyClassName="px-2 pb-2 sm:px-2 sm:pb-2">
          {revisionsQuery.isLoading ? (
            <div className="flex flex-col gap-2 px-2 pb-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-16 rounded-xl" />
              ))}
            </div>
          ) : revisions.length === 0 ? (
            <CardEmpty icon={History} title="暂无版本" />
          ) : (
            <ol className="flex flex-col gap-0.5 @4xl:max-h-[680px] @4xl:overflow-y-auto">
              {revisions.map((item) => (
                <RevisionItem
                  key={item.revision}
                  item={item}
                  current={item.revision === current}
                  active={item.revision === selected}
                  onSelect={() => setPicked(item.revision)}
                />
              ))}
            </ol>
          )}
        </Panel>

        {selected !== null && configQuery.data ? (
          <RevisionDetailPanel revision={selected} current={configQuery.data} />
        ) : (
          <Panel icon={GitCompare} title="对比">
            {revisionsQuery.isLoading || configQuery.isLoading ? <Skeleton className="h-48 rounded-xl" /> : <CardEmpty icon={GitCompare} title="暂无版本" />}
          </Panel>
        )}
      </div>
    </TabStack>
  );
}

function RevisionItem({
  item,
  current,
  active,
  onSelect
}: {
  item: RevisionSummary;
  current: boolean;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        aria-current={active || undefined}
        className={cn(
          "flex w-full min-w-0 items-start gap-3 rounded-xl px-3 py-2.5 text-left transition-colors",
          active ? "bg-accent" : "hover:bg-accent/60"
        )}
      >
        <span
          className={cn(
            "grid min-h-8 min-w-11 shrink-0 place-items-center rounded-lg px-1.5 font-mono text-xs font-semibold tabular-nums",
            current ? "bg-foreground text-background" : "bg-muted text-foreground"
          )}
        >
          r{item.revision}
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-medium">{ACTION_LABEL[item.action] ?? item.action}</span>
            {current ? <Badge variant="success" size="sm">当前</Badge> : null}
            {!item.enabled ? <Badge variant="outline" size="sm">总开关关</Badge> : null}
          </span>
          {item.note ? <span className="truncate text-xs text-muted-foreground">{item.note}</span> : null}
          <span className="flex min-w-0 flex-wrap gap-x-3 text-[11px] text-muted-foreground tabular-nums">
            <span>{formatDateTimeMinute(item.createdAt)}</span>
            <span>{item.ruleCount} 条规则</span>
          </span>
        </span>
      </button>
    </li>
  );
}

function RevisionDetailPanel({ revision, current }: { revision: number; current: TrafficConfig }) {
  const detailQuery = useTrafficRevisionQuery(revision);
  const rollback = useRollbackTrafficMutation();
  const detail = detailQuery.data;
  const diff = useMemo(() => (detail ? diffConfigs(current, detail.config) : null), [current, detail]);
  const isCurrent = revision === current.revision;

  return (
    <Panel
      icon={GitCompare}
      tone="amber"
      title={
        <span className="inline-flex items-center gap-2">
          <span className="font-mono">r{current.revision}</span>
          <ArrowRight className="size-3.5 text-muted-foreground" />
          <span className="font-mono">r{revision}</span>
        </span>
      }
      aside={
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button size="sm" variant="outline" disabled={isCurrent || !detail || rollback.isPending}>
              {rollback.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
              回滚
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>回滚到 r{revision}</AlertDialogTitle>
              <AlertDialogDescription>该版本的内容将发布为新版本，立即对全部实例生效。</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <AlertDialogAction onClick={() =>
                  rollback.mutate({ revision, note: `回滚到 r${revision}` }, { onSuccess: (config) => toast.success(`已回滚，当前 r${config.revision}`) })
                }>回滚</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      }
    >
      {detailQuery.isLoading ? (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-20 rounded-xl" />
          <Skeleton className="h-32 rounded-xl" />
        </div>
      ) : detailQuery.isError ? (
        <p className="text-sm text-muted-foreground">{trafficErrorMessage(detailQuery.error, "读取失败")}</p>
      ) : !diff || diff.empty || isCurrent ? (
        <CardEmpty icon={GitCompare} title="与当前一致" />
      ) : (
        <div className="flex flex-col gap-5">
          <div className="flex flex-wrap gap-2">
            <DiffCount label="恢复" count={diff.rules.filter((r) => r.kind === "restore").length} tone="emerald" />
            <DiffCount label="移除" count={diff.rules.filter((r) => r.kind === "remove").length} tone="rose" />
            <DiffCount label="变更" count={diff.rules.filter((r) => r.kind === "change").length} tone="amber" />
            <DiffCount label="全局设置" count={diff.settings.length} tone="sky" />
          </div>

          {diff.settings.length ? (
            <section className="flex flex-col gap-2">
              <h4 className="text-xs font-medium text-muted-foreground">全局设置</h4>
              <ChangeTable changes={diff.settings} />
            </section>
          ) : null}

          {diff.rules.length || diff.order ? (
            <section className="flex flex-col gap-2">
              <h4 className="text-xs font-medium text-muted-foreground">规则</h4>
              <div className="flex flex-col gap-2">
                {diff.rules.map((change) => (
                  <RuleChangeCard key={`${change.kind}-${change.id}`} change={change} />
                ))}
                {diff.order ? (
                  <div className="rounded-xl border border-border p-3">
                    <ChangeTable changes={[diff.order]} />
                  </div>
                ) : null}
              </div>
            </section>
          ) : null}
        </div>
      )}
    </Panel>
  );
}

const COUNT_TONE = {
  emerald: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
  rose: "bg-rose-500/12 text-rose-700 dark:text-rose-300",
  amber: "bg-amber-500/12 text-amber-700 dark:text-amber-300",
  sky: "bg-sky-500/12 text-sky-700 dark:text-sky-300"
} as const;

function DiffCount({ label, count, tone }: { label: string; count: number; tone: keyof typeof COUNT_TONE }) {
  return (
    <span className={cn("inline-flex items-baseline gap-1.5 rounded-lg px-2.5 py-1.5 text-xs", count ? COUNT_TONE[tone] : "bg-muted text-muted-foreground")}>
      {label}
      <span className="text-base leading-5 font-semibold tabular-nums">{count}</span>
    </span>
  );
}

const KIND_META: Record<RuleChange["kind"], { label: string; badge: "success" | "danger" | "warning" }> = {
  restore: { label: "恢复", badge: "success" },
  remove: { label: "移除", badge: "danger" },
  change: { label: "变更", badge: "warning" }
};

function RuleChangeCard({ change }: { change: RuleChange }) {
  const meta = KIND_META[change.kind];
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col gap-2 rounded-xl border p-3",
        change.kind === "restore" && "border-emerald-500/30",
        change.kind === "remove" && "border-rose-500/30",
        change.kind === "change" && "border-border"
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <Badge variant={meta.badge} size="sm">
          {meta.label}
        </Badge>
        <span className={cn("min-w-0 truncate text-sm font-medium", change.kind === "remove" && "line-through decoration-rose-500/60")}>
          {change.name}
        </span>
      </div>
      {change.changes.length ? <ChangeTable changes={change.changes} /> : null}
    </div>
  );
}

function ChangeTable({ changes }: { changes: FieldChange[] }) {
  return (
    <dl className="grid min-w-0 gap-x-4 gap-y-2 text-xs sm:grid-cols-[88px_minmax(0,1fr)]">
      {changes.map((change) => (
        <div key={change.label} className="contents">
          <dt className="pt-0.5 text-muted-foreground">{change.label}</dt>
          <dd className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="min-w-0 rounded bg-rose-500/10 px-1.5 py-0.5 break-all text-rose-800 line-through decoration-rose-500/50 dark:text-rose-200">
              {change.from}
            </span>
            <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
            <span className="min-w-0 rounded bg-emerald-500/10 px-1.5 py-0.5 break-all text-emerald-800 dark:text-emerald-200">{change.to}</span>
          </dd>
        </div>
      ))}
    </dl>
  );
}
