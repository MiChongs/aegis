"use client";

import { AlertTriangle, Clover } from "lucide-react";
import { Metric, formatDrawCount, formatValue } from "@/components/apps/lottery/compute/lottery-compute-shared";
import { percent } from "@/components/apps/lottery/lottery-plan-view";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { LotteryEconomy } from "@/lib/api/lottery";
import type { LotteryAnalysisPersona, LotteryExactFortuneBand } from "@/lib/api/lottery-compute";
import { cn } from "@/lib/utils";

/**
 * 一种人群的精确分析：综合概率（含保底、软保底、伪随机）对公示概率，每抽价值与积分回报，
 * 各奖品多少抽能出。数字由马尔可夫链的平稳分布直接算出，不是抽样，没有误差条。
 *
 * 综合概率与公示概率不同的地方才是要看的：保底奖会高于公示，其余奖品相应略低；
 * 没有保底、没有「谢谢参与」的奖盘两列相同。
 */
export function ExactAnalysisView({ persona }: { persona: LotteryAnalysisPersona }) {
  const analysis = persona.analysis;
  const hasNone = analysis.prizes.some((prize) => prize.kind === "none");
  const within = (prize: (typeof analysis.prizes)[number], draws: number) =>
    prize.within.find((item) => item.draws === draws)?.probability ?? 0;
  const delta = analysis.winRate - analysis.baseWinRate;
  const over = persona.price > 0 && (persona.return < 0 || persona.return > persona.price);

  return (
    <div className="space-y-4">
      <div className="grid gap-2 sm:grid-cols-3 lg:grid-cols-6">
        {hasNone ? (
          <Metric
            label="综合中奖率"
            value={percent(analysis.winRate)}
            sub={`公示 ${percent(analysis.baseWinRate)}`}
            tone={delta > 0.0005 ? "up" : delta < -0.0005 ? "down" : undefined}
          />
        ) : null}
        <Metric label="真实奖品率" value={percent(analysis.realRate)} sub={`公示 ${percent(analysis.baseRealRate)}`} />
        <Metric label="每抽价值" value={formatValue(analysis.valuePerDraw)} sub="积分当量" />
        <Metric
          label="每抽积分回报"
          value={persona.return < 0 ? "抽不完" : formatValue(persona.return)}
          sub={persona.price > 0 ? `买一次 ${persona.price} 积分` : "积分换不到次数"}
          tone={over ? "danger" : undefined}
        />
        <Metric
          label="保底占比"
          value={analysis.pityShare > 0 ? percent(analysis.pityShare) : "—"}
          sub={analysis.cycleMean > 0 ? `平均 ${formatDrawCount(analysis.cycleMean)} 抽中一次` : undefined}
        />
        {persona.scale < 1 ? (
          <Metric label="已拥有折算" value={percent(persona.scale, 0)} sub="按原价的比例发放" tone="down" />
        ) : (
          <Metric label="运气系数" value={`×${formatValue(persona.factor)}`} sub={persona.supreme.length ? "含至尊加成" : undefined} />
        )}
      </div>

      {persona.perks?.names.length ? (
        <p className="rounded-xl bg-emerald-500/[0.07] px-3 py-2 text-[11px] text-emerald-800 dark:text-emerald-300">
          计入人群特权：{persona.perks.names.join("、")}
          {persona.perks.pityCut ? `，保底提前 ${persona.perks.pityCut} 次` : ""}
          {persona.perks.winBoost ? `，中奖 ×${persona.perks.winBoost}` : ""}
        </p>
      ) : null}

      {analysis.fortune ? <FortuneBands bands={analysis.fortune} /> : null}

      <div className="overflow-x-auto rounded-xl border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8">#</TableHead>
              <TableHead>奖品</TableHead>
              <TableHead className="text-right">公示</TableHead>
              <TableHead className="text-right">综合</TableHead>
              <TableHead className="text-right">期望抽数</TableHead>
              <TableHead className="text-right">P50 / P90 / P99</TableHead>
              <TableHead className="text-right">十连出</TableHead>
              <TableHead className="text-right">百连出</TableHead>
              <TableHead className="text-right">到账积分</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {analysis.prizes.map((prize, index) => {
              const shifted = Math.abs(prize.longRun - prize.nominal) > Math.max(1e-9, prize.nominal * 0.005);
              const payout = persona.payout[index] ?? 0;
              const redraws = persona.redraws[index] ?? 0;
              return (
                <TableRow key={`${prize.prizeId}-${prize.position}`}>
                  <TableCell className="font-mono text-[11px] text-muted-foreground">{prize.position + 1}</TableCell>
                  <TableCell className="max-w-40 truncate text-xs">{prize.name || `位置 ${prize.position + 1}`}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {percent(prize.nominal)}
                  </TableCell>
                  <TableCell
                    className={cn(
                      "text-right font-mono text-xs tabular-nums",
                      shifted && prize.longRun > prize.nominal && "text-emerald-600 dark:text-emerald-400",
                      shifted && prize.longRun < prize.nominal && "text-amber-600 dark:text-amber-400"
                    )}
                  >
                    {percent(prize.longRun)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {prize.never ? "出不了" : formatDrawCount(prize.hitMean)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {prize.never
                      ? "—"
                      : [prize.hitP50, prize.hitP90, prize.hitP99].map((n) => (n ? n.toLocaleString("zh-CN") : "—")).join(" / ")}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {prize.never ? "—" : percent(within(prize, 10))}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {prize.never ? "—" : percent(within(prize, 100))}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {payout ? formatValue(payout) : redraws ? `${redraws} 次` : "—"}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>

      {analysis.notes.length ? (
        <ul className="space-y-0.5 text-[11px] text-muted-foreground">
          {analysis.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** 每日运气的三档：最倒霉、全体平均、最走运 */
function FortuneBands({
  bands
}: {
  bands: { low: LotteryExactFortuneBand; high: LotteryExactFortuneBand; mean: LotteryExactFortuneBand };
}) {
  const rows: Array<{ label: string; band: LotteryExactFortuneBand }> = [
    { label: "最倒霉", band: bands.low },
    { label: "全体平均", band: bands.mean },
    { label: "最走运", band: bands.high }
  ];
  return (
    <div className="space-y-2 rounded-xl border border-border p-3">
      <span className="flex items-center gap-1.5 text-[11px] font-medium">
        <Clover className="size-3.5 text-muted-foreground" />
        每日运气
      </span>
      <div className="grid gap-2 sm:grid-cols-3">
        {rows.map((row) => (
          <Metric
            key={row.label}
            label={`${row.label} ×${formatValue(row.band.factor)}`}
            value={percent(row.band.realRate)}
            sub={`每抽 ${formatValue(row.band.valuePerDraw)} 积分当量`}
          />
        ))}
      </div>
    </div>
  );
}

/** 积分账的警告（与活动列表上的同一份） */
export function EconomyWarnings({ economy }: { economy?: LotteryEconomy | null }) {
  if (!economy?.warnings.length) return null;
  return (
    <div className="space-y-1 rounded-xl border border-amber-500/30 bg-amber-500/[0.06] px-3 py-2">
      {economy.warnings.map((warning) => (
        <p key={warning} className="flex items-start gap-1.5 text-[11px] text-amber-800 dark:text-amber-300">
          <AlertTriangle className="mt-px size-3.5 shrink-0" />
          {warning}
        </p>
      ))}
    </div>
  );
}
