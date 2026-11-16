"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Network, Play, Rocket, RotateCcw } from "lucide-react";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { MusicDeepRun } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useMusicDeepRunsQuery, usePublishMusicDeepMutation, useTrainMusicDeepMutation } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { DEEP_TRIGGER_LABEL, compact, elapsedText, liftText, num, pct, runDuration } from "./music-format";
import { EmptyLine, MusicError, MusicPager, RowsSkeleton } from "./music-kit";
import { DeepRankerBadge, LiftBars } from "./music-deep-parts";
import { MusicDeepSheet } from "./music-deep-sheet";
import { ModelStatusBadge } from "./music-model-parts";

const PAGE_SIZE = 20;

/**
 * 深度模型训练记录（第三期，docs/music-personalization-3.md「三、深度模型」）：
 * 序列模型（SASRec）的离线指标对比热度基线、LightGBM 精排是否启用、规模与耗时。
 *
 * 口味模型训练成功后会自动投递一次（触发方式「随口味模型」），这里也能手动立即训练。
 * 一个应用同时只有一个排队中或训练中的任务，有任务时「立即训练」不可用。发布比当前旧的版本即回滚。
 */
export function MusicDeep({ appKey, canManage }: { appKey: string; canManage: boolean }) {
  const [page, setPage] = useState(1);
  const [openRun, setOpenRun] = useState<number | null>(null);
  const [training, setTraining] = useState(false);
  const [note, setNote] = useState("");
  const [publishTarget, setPublishTarget] = useState<MusicDeepRun | null>(null);
  const query = useMusicDeepRunsQuery(appKey, page, PAGE_SIZE);
  const train = useTrainMusicDeepMutation(appKey);
  const publish = usePublishMusicDeepMutation(appKey);
  const items = query.data?.items ?? [];
  const current = items.find((r) => r.current);
  const busy = items.some((r) => r.status === "queued" || r.status === "running");

  const startTrain = async () => {
    try {
      await train.mutateAsync(note.trim() || undefined);
      toast.success("已加入训练队列");
      setTraining(false);
      setNote("");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "提交失败");
    }
  };

  const confirmPublish = async () => {
    if (!publishTarget) return;
    try {
      await publish.mutateAsync(publishTarget.id);
      toast.success(`深度模型第 ${publishTarget.id} 版已发布`);
      setPublishTarget(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "发布失败");
    }
  };

  const isRollback = (run: MusicDeepRun) => Boolean(current && run.id < current.id);

  return (
    <TabStack>
      <Panel
        icon={Network}
        tone="violet"
        title="深度模型训练记录"
        aside={
          <>
            {busy ? (
              <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" />
                训练进行中
              </span>
            ) : null}
            {canManage ? (
              <Button size="sm" disabled={busy} title={busy ? "已有排队中或训练中的任务" : undefined} onClick={() => setTraining(true)}>
                <Play />
                立即训练
              </Button>
            ) : null}
          </>
        }
      >
        {query.isError && !query.data ? (
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        ) : query.isLoading ? (
          <RowsSkeleton />
        ) : !items.length ? (
          <EmptyLine>暂无训练记录。口味模型训练成功后会自动训练一次，也可以立即训练。</EmptyLine>
        ) : (
          <div className="min-w-0 overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>版本</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>触发</TableHead>
                  <TableHead>NDCG@20 对比热度</TableHead>
                  <TableHead className="text-right">Recall@20</TableHead>
                  <TableHead>精排</TableHead>
                  <TableHead className="text-right">规模</TableHead>
                  <TableHead className="text-right">耗时</TableHead>
                  <TableHead className="text-right">创建</TableHead>
                  {canManage ? <TableHead className="text-right">操作</TableHead> : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((run) => {
                  const m = run.metrics;
                  const duration = runDuration(run);
                  return (
                    <TableRow key={run.id} className="cursor-pointer" onClick={() => setOpenRun(run.id)}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-medium tabular-nums">第 {run.id} 版</span>
                          {run.current ? (
                            <Badge variant="success" size="sm">
                              当前
                            </Badge>
                          ) : null}
                        </div>
                        {run.note ? (
                          <p className="mt-0.5 max-w-48 truncate text-[11px] text-muted-foreground" title={run.note}>
                            {run.note}
                          </p>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col items-start gap-0.5">
                          <ModelStatusBadge run={run} />
                          {run.status === "failed" && run.error ? (
                            <span className="max-w-40 truncate text-[11px] text-rose-600 dark:text-rose-400" title={run.error}>
                              {run.error}
                            </span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-xs">
                        {DEEP_TRIGGER_LABEL[run.trigger] ?? run.trigger}
                        {run.operator ? <span className="block max-w-28 truncate text-[11px] text-muted-foreground">{run.operator}</span> : null}
                      </TableCell>
                      <TableCell>
                        {m ? (
                          <div className="flex items-center gap-3">
                            <LiftBars value={m.ndcg20} baseline={m.baseline.ndcg20} />
                            <span className="flex flex-col text-xs leading-4 tabular-nums">
                              <span className="font-medium">{pct(m.ndcg20)}</span>
                              <span
                                className={cn(
                                  "text-[11px]",
                                  m.ndcg20 >= m.baseline.ndcg20 ? "text-emerald-600 dark:text-emerald-400" : "text-rose-600 dark:text-rose-400"
                                )}
                              >
                                较热度 {liftText(m.ndcg20, m.baseline.ndcg20)}
                              </span>
                            </span>
                          </div>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right text-xs tabular-nums">
                        {m ? (
                          <>
                            <span>{pct(m.recall20)}</span>
                            <span className="block text-[11px] text-muted-foreground">热度 {pct(m.baseline.recall20)}</span>
                          </>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">
                        <div className="flex flex-col items-start gap-0.5">
                          <DeepRankerBadge ranker={run.ranker ?? null} />
                          {run.ranker?.enabled && typeof run.ranker.ndcg10 === "number" ? (
                            <span className="text-[11px] text-muted-foreground tabular-nums">NDCG@10 {run.ranker.ndcg10.toFixed(3)}</span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-right text-[11px] leading-4 text-muted-foreground tabular-nums">
                        {run.users ? (
                          <>
                            <span className="block text-xs text-foreground">{num(run.users)} 人</span>
                            {num(run.items)} 首，{compact(run.sequences)} 条序列
                          </>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                      <TableCell className="text-right text-xs whitespace-nowrap tabular-nums">
                        {duration !== null ? elapsedText(duration) : run.status === "running" ? "进行中" : "—"}
                      </TableCell>
                      <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground" title={formatDateTimeMinute(run.createdAt)}>
                        {formatRelative(run.createdAt)}
                      </TableCell>
                      {canManage ? (
                        <TableCell className="text-right">
                          {run.status === "succeeded" && !run.current ? (
                            <Button
                              size="xs"
                              variant="outline"
                              onClick={(e) => {
                                e.stopPropagation();
                                setPublishTarget(run);
                              }}
                            >
                              {isRollback(run) ? <RotateCcw /> : <Rocket />}
                              {isRollback(run) ? "回滚到此版本" : "发布"}
                            </Button>
                          ) : null}
                        </TableCell>
                      ) : null}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
        <MusicPager total={query.data?.total ?? 0} page={page} pageSize={PAGE_SIZE} onPage={setPage} />
      </Panel>

      <MusicDeepSheet
        appKey={appKey}
        runId={openRun}
        canManage={canManage}
        currentId={current?.id}
        onPublish={(run) => setPublishTarget(run)}
        onOpenChange={(open) => !open && setOpenRun(null)}
      />

      <Dialog open={training} onOpenChange={(open) => !train.isPending && setTraining(open)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>立即训练深度模型</DialogTitle>
            <DialogDescription>按当前配置的深度模型参数训练，达标后自动发布。</DialogDescription>
          </DialogHeader>
          <Textarea value={note} onChange={(e) => setNote(e.target.value)} placeholder="备注（选填）" maxLength={200} rows={3} />
          <DialogFooter>
            <Button variant="outline" disabled={train.isPending} onClick={() => setTraining(false)}>
              取消
            </Button>
            <Button disabled={train.isPending} onClick={startTrain}>
              {train.isPending ? <Loader2 className="animate-spin" /> : <Play />}
              开始训练
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(publishTarget)} onOpenChange={(open) => !open && !publish.isPending && setPublishTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {publishTarget && isRollback(publishTarget) ? `回滚到深度模型第 ${publishTarget.id} 版？` : `发布深度模型第 ${publishTarget?.id ?? ""} 版？`}
            </AlertDialogTitle>
            <AlertDialogDescription>发布后序列模型通道与精排立即改用这个版本；推理服务数值对齐不通过时会拒绝加载，继续用原版本。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={publish.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={publish.isPending}
              onClick={(e) => {
                e.preventDefault();
                void confirmPublish();
              }}
            >
              {publish.isPending ? <Loader2 className="animate-spin" /> : null}
              {publishTarget && isRollback(publishTarget) ? "回滚" : "发布"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}
