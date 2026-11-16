"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ChartNoAxesCombined, FlaskConical, Loader2, Pencil, Play, Plus, Square, Trash2 } from "lucide-react";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ApiError } from "@/lib/api/client";
import type { MusicExperiment } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useDeleteMusicExperimentMutation, useMusicExperimentActionMutation, useMusicExperimentsQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { EXPERIMENT_STATUS } from "./music-format";
import { MusicExperimentEditor } from "./music-experiment-editor";
import { TrafficSplit, VARIANT_COLOR, overridesText } from "./music-experiment-parts";
import { MusicExperimentResults } from "./music-experiment-results";
import { EmptyLine, MusicError, RowsSkeleton, scenesText } from "./music-kit";

/**
 * A/B 实验（/experiments）：用户按「实验 ID + 用户 ID」哈希分桶，命中的变体覆盖本次推荐的参数，A 为对照。
 * 同一场景同时只能有一个运行中的实验（开始时服务端校验，冲突回 40900）；运行中的实验不能删除，先停止。
 */

type Action = { experiment: MusicExperiment; action: "start" | "stop" | "delete" };

export function MusicExperiments({ appKey, canManage }: { appKey: string; canManage: boolean }) {
  const query = useMusicExperimentsQuery(appKey);
  const act = useMusicExperimentActionMutation(appKey);
  const remove = useDeleteMusicExperimentMutation(appKey);
  const [editing, setEditing] = useState<MusicExperiment | "new" | null>(null);
  const [resultsOf, setResultsOf] = useState<MusicExperiment | null>(null);
  const [pending, setPending] = useState<Action | null>(null);
  const items = useMemo(() => [...(query.data?.items ?? [])].sort((a, b) => order(a) - order(b) || b.id - a.id), [query.data]);
  const running = items.filter((e) => e.status === "running");
  const busy = act.isPending || remove.isPending;

  const confirm = async () => {
    if (!pending) return;
    const { experiment, action } = pending;
    try {
      if (action === "delete") {
        await remove.mutateAsync(experiment.id);
        toast.success("实验已删除");
      } else {
        await act.mutateAsync({ id: experiment.id, action });
        toast.success(action === "start" ? "实验已开始" : "实验已停止");
      }
      setPending(null);
    } catch (err) {
      if (err instanceof ApiError && err.code === 40900) toast.error("所选场景已有运行中的实验，先停止那个实验再开始");
      else toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  return (
    <TabStack>
      <Panel
        icon={FlaskConical}
        tone="violet"
        title="A/B 实验"
        aside={
          canManage ? (
            <Button size="sm" onClick={() => setEditing("new")}>
              <Plus />
              新建实验
            </Button>
          ) : null
        }
      >
        {query.isError && !query.data ? (
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        ) : query.isLoading ? (
          <RowsSkeleton rows={4} />
        ) : (
          <div className="flex min-w-0 flex-col gap-3">
            <p className="text-sm leading-5 text-muted-foreground">
              {running.length
                ? `${running.length} 个实验运行中，覆盖 ${scenesText([...new Set(running.flatMap((e) => e.scenes))])}。`
                : "按用户分桶对比不同的推荐参数，A 为对照组。同一场景同时只能运行一个实验。"}
            </p>
            {!items.length ? (
              <EmptyLine>暂无实验</EmptyLine>
            ) : (
              <ul className="flex min-w-0 flex-col gap-3">
                {items.map((exp) => (
                  <ExperimentCard
                    key={exp.id}
                    experiment={exp}
                    canManage={canManage}
                    busy={busy}
                    onEdit={() => setEditing(exp)}
                    onResults={() => setResultsOf(exp)}
                    onAction={(action) => setPending({ experiment: exp, action })}
                  />
                ))}
              </ul>
            )}
          </div>
        )}
      </Panel>

      {editing ? (
        <MusicExperimentEditor
          key={editing === "new" ? "new" : editing.id}
          appKey={appKey}
          experiment={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}

      <MusicExperimentResults appKey={appKey} experiment={resultsOf} onOpenChange={(open) => !open && setResultsOf(null)} />

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && !busy && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pending?.action === "start" ? "开始实验？" : pending?.action === "stop" ? "停止实验？" : "删除实验？"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pending?.action === "start"
                ? `「${pending.experiment.name}」开始后，${scenesText(pending.experiment.scenes)}里分到各变体的用户立即按变体参数推荐。`
                : pending?.action === "stop"
                  ? `「${pending?.experiment.name ?? ""}」停止后所有用户回到应用配置，已有的效果数据保留。`
                  : `「${pending?.experiment.name ?? ""}」及其效果数据将被删除，不可恢复。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={busy}
              className={cn(pending?.action !== "start" && "bg-destructive text-white hover:bg-destructive/90")}
              onClick={(e) => {
                e.preventDefault();
                void confirm();
              }}
            >
              {busy ? <Loader2 className="animate-spin" /> : null}
              {pending?.action === "start" ? "开始" : pending?.action === "stop" ? "停止" : "删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

/** 运行中在前，其次草稿，最后已停止 */
function order(e: MusicExperiment) {
  return e.status === "running" ? 0 : e.status === "draft" ? 1 : 2;
}

function ExperimentCard({
  experiment: exp,
  canManage,
  busy,
  onEdit,
  onResults,
  onAction
}: {
  experiment: MusicExperiment;
  canManage: boolean;
  busy: boolean;
  onEdit: () => void;
  onResults: () => void;
  onAction: (action: "start" | "stop" | "delete") => void;
}) {
  const status = EXPERIMENT_STATUS[exp.status] ?? { label: exp.status, tone: "secondary" as const };
  const timeline =
    exp.status === "running" && exp.startedAt
      ? `${formatRelative(exp.startedAt)}开始`
      : exp.status === "stopped" && exp.stoppedAt
        ? `${formatDateTimeMinute(exp.startedAt)} 至 ${formatDateTimeMinute(exp.stoppedAt)}`
        : `${formatRelative(exp.updatedAt)}修改`;
  return (
    <li className="flex min-w-0 flex-col gap-3 rounded-xl border border-border p-3 sm:p-4">
      <div className="flex min-w-0 flex-wrap items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="truncate text-sm font-semibold">{exp.name}</span>
            <Badge variant={status.tone} size="sm">
              {status.label}
            </Badge>
            <span className="text-[11px] text-muted-foreground tabular-nums">#{exp.id}</span>
          </div>
          {exp.description ? <p className="line-clamp-2 text-xs leading-5 text-muted-foreground">{exp.description}</p> : null}
          <p className="text-[11px] text-muted-foreground">
            {scenesText(exp.scenes)}；{timeline}
            {exp.operator ? `，${exp.operator}` : ""}
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-1.5">
          {exp.status !== "draft" ? (
            <Button size="sm" variant="outline" onClick={onResults}>
              <ChartNoAxesCombined />
              效果
            </Button>
          ) : null}
          {canManage ? (
            <>
              {exp.status === "running" ? (
                <Button size="sm" variant="outline" disabled={busy} onClick={() => onAction("stop")}>
                  <Square />
                  停止
                </Button>
              ) : (
                <Button size="sm" variant="outline" disabled={busy} onClick={() => onAction("start")}>
                  <Play />
                  {exp.status === "stopped" ? "重新开始" : "开始"}
                </Button>
              )}
              <Button size="icon-sm" variant="ghost" aria-label="编辑" title="编辑" onClick={onEdit}>
                <Pencil />
              </Button>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label="删除"
                      disabled={busy || exp.status === "running"}
                      className="text-rose-600 hover:text-rose-700 dark:text-rose-400"
                      onClick={() => onAction("delete")}
                    >
                      <Trash2 />
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent>{exp.status === "running" ? "运行中不能删除，先停止" : "删除"}</TooltipContent>
              </Tooltip>
            </>
          ) : null}
        </div>
      </div>
      <TrafficSplit variants={exp.variants} />
      <ul className="grid min-w-0 gap-2 @3xl:grid-cols-2 @6xl:grid-cols-3">
        {exp.variants.map((v, i) => (
          <li key={v.key} className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/40 px-3 py-2">
            <div className="flex min-w-0 items-center gap-2 text-xs">
              <span aria-hidden className="size-2.5 shrink-0 rounded-[3px]" style={{ background: VARIANT_COLOR[i % VARIANT_COLOR.length] }} />
              <span className="font-medium">{v.key}</span>
              <span className="min-w-0 truncate">{v.name}</span>
              <span className="ml-auto text-muted-foreground tabular-nums">{v.traffic}%</span>
            </div>
            <p className="text-[11px] leading-4 text-muted-foreground">{overridesText(v) || (i === 0 ? "对照组，沿用应用配置" : "未覆盖参数")}</p>
          </li>
        ))}
      </ul>
    </li>
  );
}
