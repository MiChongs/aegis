"use client";

import { useMemo, useState } from "react";
import { Eye, Sigma, Square, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { EconomyWarnings, ExactAnalysisView } from "@/components/apps/lottery/compute/lottery-exact-view";
import { ComputeJobView, LotteryComputeRunner, jobTitle } from "@/components/apps/lottery/compute/lottery-compute-runner";
import {
  ComputeStatusBadge,
  PERSONA_PRESETS,
  Pills,
  formatDraws,
  formatDuration,
  personaPayload,
  type PersonaKey
} from "@/components/apps/lottery/compute/lottery-compute-shared";
import { formatCount } from "@/components/apps/lottery/lottery-shared";
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
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { LotteryComputeJob } from "@/lib/api/lottery-compute";
import { formatCompact } from "@/lib/datetime";
import {
  useCancelLotteryComputeJobMutation,
  useDeleteLotteryComputeJobMutation,
  useLotteryAnalysisQuery,
  useLotteryComputeJobsQuery
} from "@/lib/lottery-compute-hooks";
import { useLotteryActivitiesQuery } from "@/lib/lottery-hooks";

type Mode = "analysis" | "compute";

/**
 * 计算：已保存活动的精确分析与大规模推演。
 *
 * | 分区 | 回答什么 |
 * |---|---|
 * | 精确分析 | 每种人群的综合概率（含保底）对公示概率、每抽价值与积分回报、各奖品多少抽能出。不抽样，立刻出结果 |
 * | 推演 | 一批人按各自的身份连续抽若干天，共享库存、每日限量与预算，演很多遍：限量几天抽空、预算够不够、谁拿走多少、用积分买次数赚不赚，每个数带置信区间 |
 *
 * 两者都只算不写。还没保存的编辑内容在活动编辑器底栏里算（分析 / 配平 / 推演）。
 */
export function LotteryComputePanel({
  appKey,
  activityId,
  onActivityChange
}: {
  appKey: string;
  activityId?: number;
  onActivityChange: (activityId: number) => void;
}) {
  const [mode, setMode] = useState<Mode>("analysis");
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const activities = useMemo(() => activitiesQuery.data ?? [], [activitiesQuery.data]);
  const currentId = activityId && activities.some((item) => item.id === activityId) ? activityId : activities[0]?.id;

  if (activitiesQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }
  if (!currentId) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">暂无活动</p>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Select value={String(currentId)} onValueChange={(next) => onActivityChange(Number(next))}>
          <SelectTrigger className="h-8 w-60">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {activities.map((activity) => (
              <SelectItem key={activity.id} value={String(activity.id)}>
                {activity.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup type="single" size="sm" value={mode} onValueChange={(next) => next && setMode(next as Mode)} className="shrink-0">
          <ToggleGroupItem value="analysis" className="h-7 gap-1.5 px-2.5 text-xs">
            <Sigma className="size-3.5" />
            精确分析
          </ToggleGroupItem>
          <ToggleGroupItem value="compute" className="h-7 gap-1.5 px-2.5 text-xs">
            <Users className="size-3.5" />
            推演
          </ToggleGroupItem>
        </ToggleGroup>
      </div>

      {/* 换活动时整块重挂：人群选择、当前任务都是这个活动自己的 */}
      {mode === "analysis" ? (
        <AnalysisSection key={currentId} appKey={appKey} activityId={currentId} />
      ) : (
        <ComputeSection key={currentId} appKey={appKey} activityId={currentId} />
      )}
    </div>
  );
}

function AnalysisSection({ appKey, activityId }: { appKey: string; activityId: number }) {
  const [selected, setSelected] = useState<PersonaKey[]>(["user", "lifetime"]);
  const [shown, setShown] = useState<PersonaKey>("user");
  const personas = useMemo(() => selected.map((key) => personaPayload(key)), [selected]);
  const query = useLotteryAnalysisQuery(appKey, activityId, personas);
  const current = query.data?.personas.find((item) => item.key === shown) ?? query.data?.personas[0];

  const toggle = (key: PersonaKey) => {
    // 至少留一种；顺序按预设，不按点击先后
    const next = selected.includes(key) ? selected.filter((item) => item !== key) : [...selected, key];
    if (!next.length) return;
    setSelected(PERSONA_PRESETS.map((preset) => preset.key).filter((item) => next.includes(item)));
    if (!next.includes(shown)) setShown(next[0]);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border px-4 py-3">
        <div className="space-y-1.5">
          <span className="text-[11px] font-medium">人群</span>
          <Pills multiple options={PERSONA_PRESETS} selected={selected} onToggle={toggle} />
        </div>
        {query.data && query.data.personas.length > 1 ? (
          <ToggleGroup type="single" size="sm" value={current?.key} onValueChange={(next) => next && setShown(next as PersonaKey)}>
            {query.data.personas.map((persona) => (
              <ToggleGroupItem key={persona.key} value={persona.key} className="h-7 px-2.5 text-xs">
                {persona.name}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        ) : null}
      </div>
      {query.isLoading ? (
        <Skeleton className="h-72 w-full rounded-xl" />
      ) : query.isError ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {query.error instanceof ApiError ? query.error.message : "分析失败"}
        </p>
      ) : current ? (
        <>
          <EconomyWarnings economy={query.data?.economy} />
          <ExactAnalysisView persona={current} />
        </>
      ) : null}
    </div>
  );
}

function ComputeSection({ appKey, activityId }: { appKey: string; activityId: number }) {
  const [jobId, setJobId] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const [deleting, setDeleting] = useState<LotteryComputeJob | null>(null);
  const jobsQuery = useLotteryComputeJobsQuery(appKey, activityId, page);
  const cancel = useCancelLotteryComputeJobMutation(appKey);
  const remove = useDeleteLotteryComputeJobMutation(appKey);
  const jobs = jobsQuery.data;

  const act = async (run: () => Promise<unknown>, fallback: string) => {
    try {
      await run();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : fallback);
    }
  };

  return (
    <div className="space-y-4">
      <LotteryComputeRunner appKey={appKey} activityId={activityId} jobId={null} onJobChange={(id) => {
        setJobId(id);
        setPage(1);
      }} />

      <div className="rounded-xl border border-border">
        <div className="border-b border-border px-4 py-3 text-sm font-medium">任务</div>
        {jobsQuery.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-8 w-full" />
            ))}
          </div>
        ) : !jobs?.items.length ? (
          <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无任务</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>任务</TableHead>
                <TableHead>规模</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>发起</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {jobs.items.map((job) => {
                const running = job.status === "queued" || job.status === "running";
                return (
                  <TableRow key={job.id} data-state={job.id === jobId ? "selected" : undefined}>
                    <TableCell className="max-w-48 truncate text-xs">
                      {jobTitle(job)}
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">#{job.id}</span>
                    </TableCell>
                    <TableCell className="text-[11px] tabular-nums text-muted-foreground">
                      {formatCount(job.summary.users)} 人 × {job.summary.days} 天 × {job.summary.replications} 遍，
                      {formatDraws(job.summary.draws)} 次
                    </TableCell>
                    <TableCell className="text-xs">
                      <span className="flex items-center gap-1.5">
                        <ComputeStatusBadge status={job.status} />
                        <span className="text-[11px] tabular-nums text-muted-foreground">
                          {job.status === "running" ? `${job.progress}/${job.total}` : null}
                          {job.status === "succeeded" ? formatDuration(job.durationMs) : null}
                        </span>
                      </span>
                    </TableCell>
                    <TableCell className="text-[11px] text-muted-foreground">
                      {job.createdBy} {formatCompact(job.createdAt)}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button size="icon" variant="ghost" className="size-7" aria-label="查看" onClick={() => setJobId(job.id)}>
                          <Eye className="size-3.5" />
                        </Button>
                        {running ? (
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7"
                            aria-label="取消"
                            disabled={cancel.isPending}
                            onClick={() => act(() => cancel.mutateAsync(job.id), "取消失败")}
                          >
                            <Square className="size-3.5" />
                          </Button>
                        ) : (
                          <Button size="icon" variant="ghost" className="size-7" aria-label="删除" onClick={() => setDeleting(job)}>
                            <Trash2 className="size-3.5" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
        {jobs && jobs.totalPages > 1 ? (
          <div className="flex items-center justify-end gap-2 border-t border-border px-4 py-2 text-[11px] text-muted-foreground">
            <span>
              第 {jobs.page} / {jobs.totalPages} 页
            </span>
            <Button size="sm" variant="outline" className="h-7" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              上一页
            </Button>
            <Button size="sm" variant="outline" className="h-7" disabled={page >= jobs.totalPages} onClick={() => setPage(page + 1)}>
              下一页
            </Button>
          </div>
        ) : null}
      </div>

      {jobId ? <ComputeJobView appKey={appKey} jobId={jobId} /> : null}

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这个推演任务？</AlertDialogTitle>
            <AlertDialogDescription>任务与它的结果会一起删除，无法恢复。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const target = deleting;
                setDeleting(null);
                if (!target) return;
                if (target.id === jobId) setJobId(null);
                void act(() => remove.mutateAsync(target.id), "删除失败");
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
