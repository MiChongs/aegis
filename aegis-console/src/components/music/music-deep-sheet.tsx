"use client";

import { Rocket, RotateCcw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import type { MusicDeepRun, MusicDeepRunDetail } from "@/lib/api/music";
import { formatDateTime } from "@/lib/datetime";
import { useMusicDeepRunQuery } from "@/lib/music-hooks";
import { DEEP_STAGE_LABEL, DEEP_TRIGGER_LABEL, compact, elapsedText, num, runDuration } from "./music-format";
import { EmptyLine, MusicError } from "./music-kit";
import { DeepParams, DeepRankerSummary, ParitySummary } from "./music-deep-parts";
import { ModelMetricsCompare, ModelStatusBadge } from "./music-model-parts";

const DEEP_METRIC_KEYS = ["ndcg20", "recall20", "hitRate20"] as const;

/**
 * 深度模型训练详情抽屉：序列模型指标对比热度、LightGBM 精排（NDCG@10 与特征重要性）、数值对齐、
 * 各阶段耗时、参数快照与日志。训练中每 5 秒刷新，结束后停止。
 */
export function MusicDeepSheet({
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
  onPublish: (run: MusicDeepRun) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useMusicDeepRunQuery(appKey, runId);
  const run = query.data;
  return (
    <Sheet open={runId !== null} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-xl">
        <SheetHeader className="border-b">
          <SheetTitle className="flex items-center gap-2">
            深度模型第 {runId} 版
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
  run: MusicDeepRunDetail;
  canManage: boolean;
  currentId?: number;
  onPublish: (run: MusicDeepRun) => void;
}) {
  const duration = runDuration(run);
  const stages = run.stages ?? [];
  const stageTotal = stages.reduce((s, st) => s + st.ms, 0);
  const rollback = currentId !== undefined && run.id < currentId;
  const log = run.log ?? [];
  return (
    <>
      <dl className="grid min-w-0 grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3">
        <Fact label="触发" value={`${DEEP_TRIGGER_LABEL[run.trigger] ?? run.trigger}${run.operator ? `（${run.operator}）` : ""}`} />
        <Fact label="创建" value={formatDateTime(run.createdAt)} />
        <Fact label="开始" value={formatDateTime(run.startedAt)} />
        <Fact label="结束" value={formatDateTime(run.finishedAt)} />
        <Fact label="耗时" value={duration !== null ? elapsedText(duration) : "—"} />
        <Fact label="发布" value={formatDateTime(run.publishedAt)} />
        <Fact label="用户" value={num(run.users)} />
        <Fact label="作品" value={num(run.items)} />
        <Fact label="序列" value={compact(run.sequences)} />
        {run.metrics ? <Fact label="训练轮数" value={`${num(run.metrics.epochs)} 轮`} /> : null}
        {run.metrics ? <Fact label="训练用时" value={elapsedText(run.metrics.trainMs)} /> : null}
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

      <Section title="序列模型离线评估">
        {run.metrics ? (
          <ModelMetricsCompare metrics={run.metrics} keys={DEEP_METRIC_KEYS} note="以每人最后一个正向行为为目标、在全部作品上打分" />
        ) : (
          <EmptyLine>暂无评估结果</EmptyLine>
        )}
      </Section>

      {run.status === "succeeded" || run.ranker ? (
        <Section title="精排（LightGBM）">
          <DeepRankerSummary ranker={run.ranker ?? null} />
        </Section>
      ) : null}

      <Section title="数值对齐">
        <ParitySummary parity={run.parity ?? null} />
      </Section>

      <Section title="阶段耗时">
        {stages.length ? (
          <ul className="flex min-w-0 flex-col gap-2">
            {stages.map((stage) => (
              <li key={stage.name} className="grid min-w-0 grid-cols-[6.5rem_minmax(0,1fr)_4.5rem] items-center gap-3 text-xs">
                <span className="truncate" title={stage.name}>
                  {DEEP_STAGE_LABEL[stage.name] ?? stage.name}
                </span>
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
        <DeepParams params={run.params} />
      </Section>

      <Section title="日志">
        {log.length ? (
          <pre className="max-h-80 overflow-auto rounded-lg border border-border bg-muted/40 p-3 font-mono text-[11px] leading-5 whitespace-pre-wrap">
            {log.join("\n")}
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
