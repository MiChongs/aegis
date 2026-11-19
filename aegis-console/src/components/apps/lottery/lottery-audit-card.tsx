"use client";

import { FlaskConical, RefreshCw } from "lucide-react";
import { formatCount } from "@/components/apps/lottery/lottery-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { LotteryAuditResult, LotteryPrize } from "@/lib/api/lottery";
import { useLotteryAuditQuery } from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

/**
 * 逐笔复算：每条记录按方案里的权重与随机种子重抽一次，必须抽到记录上的那个奖品。
 * 统计检验看整体分布，几千次里改一次看不出来；复算是确定性的，能指到被改的那一条。
 * 种子功能上线之前的记录没有种子，不参与。
 */
function ReplayRow({
  replay,
  nameOf
}: {
  replay: NonNullable<LotteryAuditResult["replay"]>;
  nameOf: (prizeId: number) => string;
}) {
  if (replay.checked === 0) {
    return replay.unseeded ? (
      <p className="text-[10px] text-muted-foreground">逐笔复算：{formatCount(replay.unseeded)} 次没有种子，无法复算</p>
    ) : null;
  }
  const clean = replay.matched === replay.checked;
  return (
    <div className="space-y-2 border-t border-border pt-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={clean ? "success" : "danger"} size="sm">
          {clean ? "逐笔复算一致" : `${formatCount(replay.checked - replay.matched)} 条复算对不上`}
        </Badge>
        <span className="text-[11px] text-muted-foreground">
          复算 {formatCount(replay.checked)} 次
          {replay.unseeded ? `，${formatCount(replay.unseeded)} 次没有种子` : ""}
        </span>
      </div>
      {replay.mismatches.length ? (
        <div className="divide-y divide-border rounded-lg border border-border">
          {replay.mismatches.map((item) => (
            <div key={item.drawId} className="flex items-center justify-between gap-2 px-2.5 py-1.5 text-xs">
              <span className="truncate font-mono text-[11px]">{item.drawNo || `#${item.drawId}`}</span>
              <span className="shrink-0 text-[11px] text-muted-foreground">
                记录 {nameOf(item.recorded)}，复算 {item.replayed ? nameOf(item.replayed) : "种子无效"}
              </span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/**
 * 出奖审计：线上出奖是否服从每一次方案给出的概率。
 * 整体为多项分布的 Mahalanobis χ² 检验，逐个奖品为泊松二项 z 检验（Holm–Bonferroni 校正），α = 0.01。
 * 保底与改发兜底的那几次不是抽出来的，不计入。统计检验之外另有逐笔复算（ReplayRow）。
 */
export function LotteryAuditCard({
  appKey,
  activityId,
  days,
  prizes
}: {
  appKey: string;
  activityId: number;
  days: number;
  prizes: LotteryPrize[];
}) {
  const query = useLotteryAuditQuery(appKey, activityId, days);
  const audit = query.data;
  const nameOf = (id: number, position: number) =>
    prizes.find((prize) => prize.id === id)?.name ?? (position >= 0 ? `位置 ${position + 1}` : `奖品 #${id}`);

  return (
    <div className="rounded-xl border border-border">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <span className="flex items-center gap-1.5 text-sm font-medium">
          <FlaskConical className="size-3.5 text-muted-foreground" />
          出奖审计（{days} 天）
        </span>
        <Button
          size="icon"
          variant="ghost"
          className="size-7"
          onClick={() => query.refetch()}
          disabled={query.isFetching}
          aria-label="重新审计"
        >
          <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} />
        </Button>
      </div>
      {query.isLoading ? (
        <div className="space-y-2 p-4">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      ) : !audit || audit.draws === 0 ? (
        <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无可审计的记录</p>
      ) : (
        <div className="space-y-3 p-4">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={audit.passed ? "success" : "danger"} size="sm">
              {audit.passed ? "符合方案概率" : "存在显著偏差"}
            </Badge>
            <span className="font-mono text-xs tabular-nums">
              χ²({audit.df}) = {audit.statistic.toFixed(2)}，p = {audit.pValue < 0.0001 ? "< 0.0001" : audit.pValue.toFixed(4)}
            </span>
            <span className="text-[11px] text-muted-foreground">
              {formatCount(audit.draws)} 次{audit.skipped ? `，不计入 ${formatCount(audit.skipped)} 次` : ""}
            </span>
          </div>
          <div className="divide-y divide-border rounded-lg border border-border">
            <div className="grid grid-cols-[1fr_4.5rem_5.5rem_4rem_4.5rem] gap-2 px-2.5 py-1.5 text-[10px] text-muted-foreground">
              <span>奖品</span>
              <span className="text-right">实际</span>
              <span className="text-right">期望 ± σ</span>
              <span className="text-right">z</span>
              <span className="text-right">p</span>
            </div>
            {audit.prizes.map((prize) => (
              <div
                key={prize.prizeId}
                className={cn(
                  "grid grid-cols-[1fr_4.5rem_5.5rem_4rem_4.5rem] items-center gap-2 px-2.5 py-1.5 text-xs",
                  prize.flagged && "bg-rose-500/5"
                )}
              >
                <span className="truncate">{nameOf(prize.prizeId, prize.position)}</span>
                <span className="text-right font-mono tabular-nums">{formatCount(prize.observed)}</span>
                <span className="text-right font-mono tabular-nums text-muted-foreground">
                  {prize.expected.toFixed(1)} ± {prize.std.toFixed(1)}
                </span>
                <span
                  className={cn(
                    "text-right font-mono tabular-nums",
                    prize.flagged && "font-semibold text-rose-600 dark:text-rose-400"
                  )}
                >
                  {prize.z > 0 ? "+" : ""}
                  {prize.z.toFixed(2)}
                </span>
                <span className="text-right font-mono tabular-nums text-muted-foreground">
                  {prize.pValue < 0.0001 ? "< 0.0001" : prize.pValue.toFixed(4)}
                </span>
              </div>
            ))}
          </div>
          <p className="text-[10px] text-muted-foreground">
            期望按每一次的实际方案累计；α = {audit.alpha}，逐项检验经 Holm–Bonferroni 校正
          </p>
          {audit.replay ? <ReplayRow replay={audit.replay} nameOf={(id) => nameOf(id, -1)} /> : null}
        </div>
      )}
    </div>
  );
}
