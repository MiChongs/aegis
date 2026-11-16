"use client";

import { Rocket, RotateCcw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import type { MusicModelDetail, MusicModelRun } from "@/lib/api/music";
import { formatDateTime } from "@/lib/datetime";
import { useMusicModelQuery } from "@/lib/music-hooks";
import { STAGE_LABEL, TRIGGER_LABEL, bytesText, compact, elapsedText, num, runDuration } from "./music-format";
import { CONFIG_GROUPS, formatFieldValue, getPath } from "./music-config-fields";
import { EmptyLine, MusicError } from "./music-kit";
import { ModelMetricsCompare, ModelStatusBadge } from "./music-model-parts";

/**
 * 训练详情抽屉：指标对比基线、规模、各阶段耗时、训练时的参数快照与日志。
 * 训练中的版本每 5 秒刷新（日志随之变长），结束后停止。
 */
export function MusicModelSheet({
  appKey,
  runId,
  canManage,
  currentId,
  onPublish,
  onOpenChange
}: {
  appKey: string;
  runId: number | null;
  canManage: boolean;
  currentId?: number;
  onPublish: (run: MusicModelRun) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useMusicModelQuery(appKey, runId);
  const run = query.data;
  return (
    <Sheet open={runId !== null} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-xl">
        <SheetHeader className="border-b">
          <SheetTitle className="flex items-center gap-2">
            第 {runId} 版
            {run ? <ModelStatusBadge run={run} /> : null}
            {run?.current ? (
              <Badge variant="success" size="sm">
                当前
              </Badge>
            ) : null}
          </SheetTitle>
        </SheetHeader>
        <div className="flex min-w-0 flex-col gap-6 p-4">
          {query.isError ? (
            <MusicError error={query.error} onRetry={() => query.refetch()} />
          ) : !run ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-40 w-full" />
              <Skeleton className="h-40 w-full" />
            </div>
          ) : (
            <Detail run={run} canManage={canManage} currentId={currentId} onPublish={onPublish} />
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function Detail({
  run,
  canManage,
  currentId,
  onPublish
}: {
  run: MusicModelDetail;
  canManage: boolean;
  currentId?: number;
  onPublish: (run: MusicModelRun) => void;
}) {
  const duration = runDuration(run);
  const stageTotal = run.stages.reduce((s, st) => s + st.ms, 0);
  const rollback = currentId !== undefined && run.id < currentId;
  return (
    <>
      <dl className="grid min-w-0 grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3">
        <Fact label="触发" value={`${TRIGGER_LABEL[run.trigger] ?? run.trigger}${run.operator ? `（${run.operator}）` : ""}`} />
        <Fact label="创建" value={formatDateTime(run.createdAt)} />
        <Fact label="开始" value={formatDateTime(run.startedAt)} />
        <Fact label="结束" value={formatDateTime(run.finishedAt)} />
        <Fact label="耗时" value={duration !== null ? elapsedText(duration) : "—"} />
        <Fact label="发布" value={formatDateTime(run.publishedAt)} />
        <Fact label="用户" value={num(run.users)} />
        <Fact label="作品" value={num(run.items)} />
        <Fact label="交互" value={compact(run.interactions)} />
        <Fact label="模型大小" value={bytesText(run.sizeBytes)} />
        {run.note ? <Fact label="备注" value={run.note} className="col-span-2 sm:col-span-3" /> : null}
      </dl>

      {canManage && run.status === "succeeded" && !run.current ? (
        <Button size="sm" variant="outline" className="w-fit" onClick={() => onPublish(run)}>
          {rollback ? <RotateCcw /> : <Rocket />}
          {rollback ? "回滚到此版本" : "发布此版本"}
        </Button>
      ) : null}

      {run.status === "failed" && run.error ? (
        <p className="rounded-lg border border-rose-500/30 bg-rose-500/5 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">{run.error}</p>
      ) : null}

      <Section title="离线评估">
        {run.metrics ? <ModelMetricsCompare metrics={run.metrics} /> : <EmptyLine>暂无评估结果</EmptyLine>}
      </Section>

      <Section title="阶段耗时">
        {run.stages.length ? (
          <ul className="flex min-w-0 flex-col gap-2">
            {run.stages.map((stage) => (
              <li key={stage.name} className="grid min-w-0 grid-cols-[6.5rem_minmax(0,1fr)_4.5rem] items-center gap-3 text-xs">
                <span className="truncate">{STAGE_LABEL[stage.name] ?? stage.name}</span>
                <span className="h-2 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-sky-500" style={{ width: `${stageTotal ? (stage.ms / stageTotal) * 100 : 0}%` }} />
                </span>
                <span className="text-right text-muted-foreground tabular-nums">{elapsedText(stage.ms)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyLine>暂无阶段记录</EmptyLine>
        )}
      </Section>

      <Section title="参数快照">
        <div className="flex min-w-0 flex-col gap-4">
          {CONFIG_GROUPS.filter((g) => g.key !== "switch").map((group) => (
            <div key={group.key} className="min-w-0">
              <p className="mb-1.5 text-[11px] font-medium text-muted-foreground">{group.title}</p>
              <dl className="grid min-w-0 grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
                {group.fields.map((field) => (
                  <div key={field.path} className="flex min-w-0 items-baseline justify-between gap-3 text-xs">
                    <dt className="truncate text-muted-foreground">{field.label}</dt>
                    <dd className="shrink-0 font-medium tabular-nums">{formatFieldValue(field, getPath(run.params, field.path))}</dd>
                  </div>
                ))}
              </dl>
            </div>
          ))}
        </div>
      </Section>

      <Section title="日志">
        {run.log.length ? (
          <pre className="max-h-80 overflow-auto rounded-lg border border-border bg-muted/40 p-3 font-mono text-[11px] leading-5 whitespace-pre-wrap">
            {run.log.join("\n")}
          </pre>
        ) : (
          <EmptyLine>暂无日志</EmptyLine>
        )}
      </Section>
    </>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h4 className="text-sm font-semibold">{title}</h4>
      {children}
    </section>
  );
}

function Fact({ label, value, className }: { label: string; value: React.ReactNode; className?: string }) {
  return (
    <div className={className}>
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 truncate text-xs font-medium tabular-nums">{value}</dd>
    </div>
  );
}
