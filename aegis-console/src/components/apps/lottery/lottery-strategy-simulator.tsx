"use client";

import { useState } from "react";
import { Crown, FlaskConical, Loader2, Play } from "lucide-react";
import { toast } from "sonner";
import { NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import { BehaviorScoreView, DrawPlanView, percent } from "@/components/apps/lottery/lottery-plan-view";
import { SOURCE_LABEL, formatCount } from "@/components/apps/lottery/lottery-shared";
import { STRATEGY_LIMITS } from "@/components/apps/lottery/lottery-strategy-form";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ApiError } from "@/lib/api/client";
import type {
  LotteryStreakAnalysis,
  LotterySimulationResult,
  LotteryActivityPayload,
  LotteryCatalog,
  LotteryDistribution,
  LotterySimulatePayload,
  LotterySimulateResponse,
  LotterySource
} from "@/lib/api/lottery";
import { useLotterySettingsQuery, useSimulateLotteryMutation } from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

type PerkIdentity = "none" | "member" | "lifetime" | "adFree" | "signed";

/** 模拟时虚拟用户可选的特权身份 */
const PERK_IDENTITY: Record<PerkIdentity, { label: string; facts: NonNullable<LotterySimulatePayload["perk"]> }> = {
  none: { label: "普通用户", facts: {} },
  member: { label: "会员", facts: { member: true } },
  lifetime: { label: "永久会员", facts: { member: true, lifetime: true } },
  adFree: { label: "免广告用户", facts: { adFree: true } },
  signed: { label: "今天已签到", facts: { signedToday: true } }
};

/**
 * 策略模拟：按编辑器里当前（未保存也行）的奖盘与策略，让一批虚拟用户各自连续抽若干次。
 * 填用户 ID 时用这个真实用户的全部行为与本活动进度，并给出他此刻再抽一次的出奖方案。
 * 只算不写，不发奖。
 */
export function LotteryStrategySimulator({
  appKey,
  activityId,
  payload,
  catalog,
  open,
  onOpenChange
}: {
  appKey: string;
  activityId?: number;
  /** 编辑器当前内容 */
  payload: () => LotteryActivityPayload;
  catalog?: LotteryCatalog | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const limits = catalog?.strategyLimits ?? STRATEGY_LIMITS;
  const simulate = useSimulateLotteryMutation(appKey);
  const [users, setUsers] = useState("500");
  const [draws, setDraws] = useState("30");
  const [source, setSource] = useState<LotterySource>("free");
  const [userId, setUserId] = useState("");
  const [isolated, setIsolated] = useState(false);
  const [identity, setIdentity] = useState<PerkIdentity>("none");
  const [tierIds, setTierIds] = useState<number[]>([]);
  const [result, setResult] = useState<LotterySimulateResponse | null>(null);
  const tiers = useLotterySettingsQuery(appKey).data?.tiers ?? [];
  const tieredIdentity = identity === "member" || identity === "lifetime";

  const total = (Number(users) || 0) * (Number(draws) || 0);
  const run = async () => {
    if (total > limits.simulationDraws) {
      toast.error(`总抽奖次数不能超过 ${formatCount(limits.simulationDraws)}`);
      return;
    }
    try {
      const response = await simulate.mutateAsync({
        activityId,
        activity: payload(),
        userId: Number(userId) > 0 ? Number(userId) : undefined,
        users: Number(users) || 1,
        drawsPerUser: Number(draws) || 1,
        source,
        isolatedStock: isolated,
        perk:
          Number(userId) > 0 || identity === "none"
            ? undefined
            : { ...PERK_IDENTITY[identity].facts, tiers: tieredIdentity && tierIds.length ? tierIds : undefined }
      });
      setResult(response);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  };

  const nameOf = (id: number, position: number) =>
    result?.result.prizes.find((prize) => prize.prizeId === id)?.name ||
    result?.result.prizes.find((prize) => prize.position === position)?.name ||
    `位置 ${position + 1}`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-3xl">
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <DialogTitle className="flex items-center gap-2">
            <FlaskConical className="size-4" /> 策略模拟
          </DialogTitle>
          <DialogDescription>按当前编辑内容计算，不写入数据、不发奖</DialogDescription>
        </DialogHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            <div className="grid gap-3 sm:grid-cols-4">
              <NumberField
                label="虚拟用户"
                value={users}
                min={1}
                max={limits.simulationUsers}
                unit="人"
                onChange={setUsers}
              />
              <NumberField
                label="每人抽奖"
                value={draws}
                min={1}
                max={limits.simulationEach}
                unit="次"
                onChange={setDraws}
              />
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">次数来源</span>
                <Select value={source} onValueChange={(value) => setSource(value as LotterySource)}>
                  <SelectTrigger className="h-9 w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(["free", "bonus", "points"] as LotterySource[]).map((item) => (
                      <SelectItem key={item} value={item}>
                        {SOURCE_LABEL[item]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">用户 ID</span>
                <Input
                  inputMode="numeric"
                  value={userId}
                  placeholder="可选"
                  onChange={(event) => setUserId(event.target.value.replace(/\D/g, ""))}
                  className="h-9 font-mono text-xs"
                />
              </div>
            </div>
            {Number(userId) > 0 ? null : (
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">虚拟用户的特权身份</span>
                <div role="radiogroup" className="flex flex-wrap gap-1.5">
                  {(Object.keys(PERK_IDENTITY) as PerkIdentity[]).map((key) => (
                    <button
                      key={key}
                      type="button"
                      role="radio"
                      aria-checked={identity === key}
                      onClick={() => setIdentity(key)}
                      className={cn(
                        "h-7 rounded-lg border px-2.5 text-[11px] font-medium transition-colors",
                        identity === key
                          ? "border-foreground/80 bg-foreground text-background"
                          : "border-border bg-card text-muted-foreground hover:text-foreground"
                      )}
                    >
                      {PERK_IDENTITY[key].label}
                    </button>
                  ))}
                </div>
                <p className="text-[10px] text-muted-foreground">按这一身份判定人群特权与至尊加成，保底提前与中奖加成计入模拟</p>
                {tieredIdentity && tiers.length ? (
                  <div className="space-y-1 pt-1">
                    <span className="text-[11px] font-medium">所在会员档位</span>
                    <div className="flex flex-wrap gap-1.5">
                      {tiers.map((tier) => {
                        const on = tierIds.includes(tier.id);
                        return (
                          <button
                            key={tier.id}
                            type="button"
                            aria-pressed={on}
                            onClick={() => setTierIds(on ? tierIds.filter((id) => id !== tier.id) : [...tierIds, tier.id])}
                            className={cn(
                              "h-7 rounded-lg border px-2.5 text-[11px] font-medium transition-colors",
                              on
                                ? "border-foreground/80 bg-foreground text-background"
                                : "border-border bg-card text-muted-foreground hover:text-foreground"
                            )}
                          >
                            {tier.name}
                          </button>
                        );
                      })}
                    </div>
                    <p className="text-[10px] text-muted-foreground">至尊加成按会员档位分级时用；永久会员按永久开通算</p>
                  </div>
                ) : null}
              </div>
            )}
            <div className="flex flex-wrap items-center gap-3">
              <div className="min-w-56 flex-1">
                <SwitchRow
                  label="每人独立库存"
                  hint="关闭时所有虚拟用户共享库存与每日限量"
                  checked={isolated}
                  onChange={setIsolated}
                />
              </div>
              <Button onClick={run} disabled={simulate.isPending} className="shrink-0">
                {simulate.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                运行 {formatCount(total)} 次
              </Button>
            </div>

            {result ? <SimulationReport result={result} nameOf={nameOf} catalog={catalog} hasUser={Number(userId) > 0} /> : null}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function SimulationReport({
  result,
  nameOf,
  catalog,
  hasUser
}: {
  result: LotterySimulateResponse;
  nameOf: (id: number, position: number) => string;
  catalog?: LotteryCatalog | null;
  hasUser: boolean;
}) {
  const sim = result.result;
  const delta = sim.winRate - sim.baseWinRate;
  const maxRate = Math.max(0.0001, ...sim.prizes.map((prize) => Math.max(prize.rate, prize.baseRate)));

  return (
    <div className="space-y-5">
      <div className="grid gap-2 sm:grid-cols-4">
        <Stat
          label="实际中奖率"
          value={percent(sim.winRate)}
          sub={`公示 ${percent(sim.baseWinRate)}`}
          tone={delta > 0.0005 ? "up" : delta < -0.0005 ? "down" : undefined}
        />
        <Stat label="首次中奖" value={distText(sim.firstWin, "p50")} sub={`P90 ${distText(sim.firstWin, "p90")}，未中过 ${sim.neverWon} 人`} />
        <Stat label="最长连续未中" value={distText(sim.missStreak, "p90")} sub={`P90，最多 ${sim.missStreak.max} 次`} />
        <Stat
          label="保底"
          value={`${formatCount(sim.pityHits)} 次`}
          sub={sim.softPityDraws ? `软保底生效 ${formatCount(sim.softPityDraws)} 次` : "软保底未生效"}
        />
      </div>
      {result.perks?.names.length ? (
        <p className="rounded-xl bg-emerald-500/[0.07] px-3 py-2 text-[11px] text-emerald-800 dark:text-emerald-300">
          计入人群特权：{result.perks.names.join("、")}
          {result.perks.pityCut ? `，保底提前 ${result.perks.pityCut} 次` : ""}
          {result.perks.winBoost ? `，中奖 ×${result.perks.winBoost}` : ""}
        </p>
      ) : null}
      {result.supreme ? <SupremeView result={result} /> : null}
      <p className="text-[11px] text-muted-foreground">
        共 {formatCount(sim.users)} 人、{formatCount(sim.draws)} 次，发出积分 {formatCount(sim.pointsIssued)}
        {sim.valuePerDraw !== undefined ? `，平均每次 ${sim.valuePerDraw.toFixed(2)} 积分当量` : ""}
        {sim.winsPerUser ? `，每人中奖 P50 ${sim.winsPerUser.p50} 次、P90 ${sim.winsPerUser.p90} 次` : ""}
      </p>

      {result.analytic && !result.analytic.never ? <AnalyticView analytic={result.analytic} sim={sim} /> : null}

      <div className="space-y-1.5">
        <span className="text-[11px] font-medium">各奖品出现比例</span>
        <div className="divide-y divide-border rounded-lg border border-border">
          {sim.prizes.map((prize) => (
            <div key={`${prize.prizeId}-${prize.position}`} className="grid grid-cols-[1.25rem_1fr_7rem] items-center gap-2 px-2.5 py-1.5">
              <span className="font-mono text-[11px] text-muted-foreground">{prize.position + 1}</span>
              <div className="min-w-0 space-y-1">
                <div className="flex items-center gap-2 text-xs">
                  <span className="truncate">{prize.name}</span>
                  {prize.blocked ? (
                    <span className="shrink-0 text-[10px] text-amber-600 dark:text-amber-400">
                      受限 {formatCount(prize.blocked)} 次
                    </span>
                  ) : null}
                </div>
                <div className="relative h-1.5 overflow-hidden rounded-full bg-muted">
                  <span
                    className="absolute inset-y-0 left-0 rounded-full bg-muted-foreground/30"
                    style={{ width: `${(prize.baseRate / maxRate) * 100}%` }}
                  />
                  <span
                    className="absolute inset-y-0 left-0 rounded-full bg-emerald-500"
                    style={{ width: `${(prize.rate / maxRate) * 100}%`, opacity: 0.85 }}
                  />
                </div>
              </div>
              <div className="text-right font-mono text-[11px] tabular-nums">
                {percent(prize.rate)}
                <span className="ml-1 text-muted-foreground">/ {percent(prize.baseRate)}</span>
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-muted-foreground">绿色为模拟结果，灰色为公示概率</p>
      </div>

      {hasUser ? <ProfileView result={result} catalog={catalog} /> : result.behavior ? (
        <BehaviorScoreView score={result.behavior} signals={catalog?.signals} />
      ) : null}

      {result.current ? (
        <div className="space-y-1.5">
          <span className="text-[11px] font-medium">此用户下一次抽奖</span>
          <DrawPlanView plan={result.current} prizeName={nameOf} signals={catalog?.signals} catalog={catalog} />
        </div>
      ) : null}
    </div>
  );
}

/** 至尊加成的效果：加成前后的运气系数，与同一批随机数、不加成的对照组逐项对比 */
function SupremeView({ result }: { result: LotterySimulateResponse }) {
  const supreme = result.supreme!;
  const sim = result.result;
  const baseline = supreme.baseline;
  const source = supreme.source === "activity" ? "活动单独设置" : "跟随应用设置";
  if (!supreme.applied) {
    return (
      <p className="rounded-xl bg-muted px-3 py-2 text-[11px] text-muted-foreground">
        {supreme.title}（{source}）：模拟的{result.current ? "这位用户" : "虚拟用户"}不在加成范围内，按自然运气抽奖
      </p>
    );
  }
  const before = supreme.baseFactor ?? sim.fortuneBase?.mean;
  const after = supreme.factor ?? sim.fortune?.mean;
  const rows: Array<{ label: string; plain?: string; lifted: string }> = [
    { label: "实际中奖率", plain: baseline ? percent(baseline.winRate) : undefined, lifted: percent(sim.winRate) },
    {
      label: "每次积分当量",
      plain: baseline?.valuePerDraw !== undefined ? baseline.valuePerDraw.toFixed(2) : undefined,
      lifted: sim.valuePerDraw !== undefined ? sim.valuePerDraw.toFixed(2) : "—"
    },
    { label: "发出积分", plain: baseline ? formatCount(baseline.pointsIssued) : undefined, lifted: formatCount(sim.pointsIssued) }
  ];
  return (
    <div className="space-y-2 rounded-xl border border-amber-500/30 bg-amber-500/[0.04] p-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="flex items-center gap-1.5 text-[11px] font-medium">
          <Crown className="size-3.5 text-amber-600 dark:text-amber-400" />
          {supreme.title}（{source}）
        </span>
        {before !== undefined && after !== undefined ? (
          <span className="font-mono text-[11px] tabular-nums">
            {supreme.factor !== undefined ? "运气系数" : "平均运气系数"} ×{before.toFixed(2)} → ×{after.toFixed(2)}
          </span>
        ) : null}
      </div>
      {supreme.grades.length ? (
        <p className="text-[10px] text-muted-foreground">
          参数：
          {supreme.grades
            .map(
              (grade) =>
                `${grade.tierName ?? (grade.tierId ? `档位 ${grade.tierId}` : "永久会员")} 幅度 ×${grade.boost || 1}` +
                (grade.floor ? `，保底 ${grade.floor}` : "") +
                (grade.cap ? `，上限 ${grade.cap}` : "")
            )
            .join("；")}
        </p>
      ) : null}
      <div className="grid gap-2 sm:grid-cols-3">
        {rows.map((row) => (
          <Stat key={row.label} label={row.label} value={row.lifted} sub={row.plain ? `不加成 ${row.plain}` : undefined} />
        ))}
      </div>
      {baseline ? (
        <div className="divide-y divide-border rounded-lg border border-border bg-card">
          {sim.prizes.map((prize, index) => {
            const plain = baseline.prizes[index];
            return (
              <div key={`${prize.prizeId}-${prize.position}`} className="flex items-center justify-between gap-2 px-2.5 py-1 text-[11px]">
                <span className="truncate">{prize.name}</span>
                <span className="shrink-0 font-mono tabular-nums">
                  {plain ? <span className="text-muted-foreground">{percent(plain.rate)} → </span> : null}
                  {percent(prize.rate)}
                </span>
              </div>
            );
          })}
        </div>
      ) : null}
      <p className="text-[10px] text-muted-foreground">对照组与加成组使用同一批随机数，差异只来自至尊加成</p>
    </div>
  );
}

function ProfileView({ result, catalog }: { result: LotterySimulateResponse; catalog?: LotteryCatalog | null }) {
  const profile = result.profile as unknown as Record<string, number | boolean>;
  const signals = catalog?.signals ?? [];
  return (
    <div className="space-y-2">
      <span className="text-[11px] font-medium">用户行为</span>
      <div className="grid gap-1.5 sm:grid-cols-3">
        {signals.map((spec) => {
          const raw = profile[spec.key];
          const text =
            typeof raw === "boolean"
              ? raw
                ? "是"
                : "否"
              : spec.key === "memberRate"
                ? percent(Number(raw) || 0, 0)
                : `${Number((Number(raw) || 0).toFixed(1)).toLocaleString("zh-CN")}${spec.unit && spec.unit !== "%" ? ` ${spec.unit}` : ""}`;
          return (
            <div key={spec.key} className="flex items-baseline justify-between gap-2 rounded-md bg-muted px-2 py-1 text-[11px]">
              <span className="truncate text-muted-foreground">{spec.label}</span>
              <span className="shrink-0 font-mono tabular-nums">{text}</span>
            </div>
          );
        })}
      </div>
      {result.behavior ? <BehaviorScoreView score={result.behavior} signals={signals} /> : null}
    </div>
  );
}

function distText(dist: LotteryDistribution, key: "p50" | "p90") {
  return dist[key] ? `第 ${dist[key]} 次` : "—";
}

function Stat({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: "up" | "down" }) {
  return (
    <div className="space-y-0.5 rounded-xl bg-muted px-3 py-2.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div
        className={cn(
          "font-mono text-lg font-semibold tabular-nums",
          tone === "up" && "text-emerald-600 dark:text-emerald-400",
          tone === "down" && "text-amber-600 dark:text-amber-400"
        )}
      >
        {value}
      </div>
      {sub ? <div className="truncate text-[10px] text-muted-foreground">{sub}</div> : null}
    </div>
  );
}

/**
 * 首中精确分布（吸收马尔可夫链）：每个连败状态的中奖率曲线与精确的均值、分位数，旁边对照模拟值。
 * 长期中奖率 = 1/E[T]（更新报酬定理）。
 */
function AnalyticView({ analytic, sim }: { analytic: LotteryStreakAnalysis; sim: LotterySimulationResult }) {
  const shown = analytic.hazard.slice(0, 40);
  const width = 320;
  const height = 64;
  const step = shown.length > 1 ? width / (shown.length - 1) : width;
  const path = shown.map((h, k) => `${k === 0 ? "M" : "L"}${(k * step).toFixed(1)},${(height - h * height).toFixed(1)}`).join(" ");
  return (
    <div className="space-y-2 rounded-xl border border-border p-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[11px] font-medium">首次中奖精确分布</span>
        <span className="text-[10px] text-muted-foreground">括号内为模拟值</span>
      </div>
      <div className="grid gap-2 sm:grid-cols-4">
        <Stat label="期望次数" value={analytic.mean.toFixed(2)} sub={`标准差 ${analytic.std.toFixed(2)}（${sim.firstWin.mean.toFixed(2)}）`} />
        <Stat label="中位" value={`第 ${analytic.p50} 次`} sub={`模拟 ${sim.firstWin.p50 || "—"}`} />
        <Stat label="P90 / P99" value={`${analytic.p90} / ${analytic.p99}`} sub={`模拟 ${sim.firstWin.p90 || "—"} / ${sim.firstWin.p99 || "—"}`} />
        <Stat label="长期中奖率" value={percent(analytic.longRunWinRate)} sub={`模拟 ${percent(sim.winRate)}`} />
      </div>
      <div className="space-y-1">
        <span className="text-[10px] text-muted-foreground">连续未中 k 次后下一次的中奖率（k = 0–{shown.length - 1}）</span>
        <svg viewBox={`0 0 ${width} ${height}`} className="h-16 w-full" preserveAspectRatio="none" role="img" aria-label="中奖率曲线">
          <line x1="0" y1={height} x2={width} y2={height} className="stroke-border" strokeWidth="1" />
          <path d={path} fill="none" className="stroke-emerald-500" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
        </svg>
      </div>
    </div>
  );
}
