"use client";

import { useMemo, useState } from "react";
import { ArrowRight, Check, Loader2, Scale, Sigma, Users, Wand2, X } from "lucide-react";
import { toast } from "sonner";
import { NumberField } from "@/components/apps/app-config-primitives";
import { EconomyWarnings, ExactAnalysisView } from "@/components/apps/lottery/compute/lottery-exact-view";
import { LotteryComputeRunner } from "@/components/apps/lottery/compute/lottery-compute-runner";
import {
  Metric,
  PERSONA_PRESETS,
  Pills,
  formatValue,
  personaPayload,
  type PersonaKey
} from "@/components/apps/lottery/compute/lottery-compute-shared";
import { percent } from "@/components/apps/lottery/lottery-plan-view";
import { formatCount, formatProbability } from "@/components/apps/lottery/lottery-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { LotteryActivityPayload } from "@/lib/api/lottery";
import type {
  LotterySolveCheck,
  LotterySolveFortune,
  LotterySolveGoal,
  LotterySolvePrizeGoal,
  LotterySolveResult
} from "@/lib/api/lottery-compute";
import { useLotteryDraftAnalysisQuery, useSolveLotteryMutation } from "@/lib/lottery-compute-hooks";

/**
 * 活动编辑器里的三个计算弹窗：都按编辑器当前（未保存也行）的奖盘与策略算，不写库、不发奖。
 *
 * | 弹窗 | 做什么 |
 * |---|---|
 * | 精确分析 | 综合概率对公示概率、每抽价值、各奖品多少抽能出 |
 * | 配平 | 给出目标（综合中奖率、每抽价值、某一格的综合概率……），反解出权重，确认后填回奖盘 |
 * | 推演 | 一批人连续抽若干天、演很多遍，看库存、预算与积分回报 |
 */

type DialogProps = {
  appKey: string;
  activityId?: number;
  /** 编辑器当前内容 */
  payload: () => LotteryActivityPayload;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

// ── 精确分析 ──

export function LotteryAnalysisDialog({ appKey, activityId, payload, open, onOpenChange }: DialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl">
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <DialogTitle className="flex items-center gap-2">
            <Sigma className="size-4" /> 精确分析
          </DialogTitle>
          <DialogDescription>按当前编辑内容计算，不抽样、不写入数据</DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1">
          <div className="px-6 py-5">{open ? <AnalysisBody appKey={appKey} activityId={activityId} payload={payload} /> : null}</div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function AnalysisBody({ appKey, activityId, payload }: Pick<DialogProps, "appKey" | "activityId" | "payload">) {
  // 打开时取一次编辑内容：弹窗开着的时候底下的表单改不了
  const [activity] = useState(payload);
  const [selected, setSelected] = useState<PersonaKey[]>(["user", "lifetime"]);
  const [shown, setShown] = useState<PersonaKey>("user");
  const request = useMemo(
    () => ({ activityId, activity, personas: selected.map((key) => personaPayload(key)) }),
    [activityId, activity, selected]
  );
  const query = useLotteryDraftAnalysisQuery(appKey, request);
  const current = query.data?.personas.find((item) => item.key === shown) ?? query.data?.personas[0];

  const toggle = (key: PersonaKey) => {
    const next = selected.includes(key) ? selected.filter((item) => item !== key) : [...selected, key];
    if (!next.length) return;
    setSelected(PERSONA_PRESETS.map((preset) => preset.key).filter((item) => next.includes(item)));
    if (!next.includes(shown)) setShown(next[0]);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
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

// ── 配平（奖盘求解）──

/** 求解弹窗里的一格奖品 */
export type SolverPrize = {
  position: number;
  name: string;
  /** 谢谢参与 */
  none: boolean;
  /** 当前的公示概率（百分比） */
  probability: number;
  weight: number;
};

type PrizeMode = "" | LotterySolvePrizeGoal["mode"];
type PrizeGoalDraft = { mode: PrizeMode; probability: string; min: string; max: string };

const PRIZE_MODES: Array<{ key: PrizeMode; label: string }> = [
  { key: "", label: "随动" },
  { key: "lock", label: "锁定" },
  { key: "target", label: "定概率" },
  { key: "range", label: "限区间" }
];

const TOTALS = ["auto", "10000", "100000", "1000000"] as const;

/** 百分比输入 → 0–1；空或不是正数时为 undefined */
function rate(text: string) {
  const value = Number(text);
  return text.trim() !== "" && Number.isFinite(value) && value > 0 ? value / 100 : undefined;
}

function amount(text: string) {
  const value = Number(text);
  return text.trim() !== "" && Number.isFinite(value) && value > 0 ? value : undefined;
}

export function LotterySolverDialog({
  appKey,
  activityId,
  payload,
  prizes,
  hasFortune,
  open,
  onOpenChange,
  onApply
}: DialogProps & {
  prizes: SolverPrize[];
  /** 活动开了每日运气：可以选按哪一天的运气算 */
  hasFortune: boolean;
  /** 把解出来的权重填回奖盘（按位置） */
  onApply: (weights: Map<number, number>) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-4xl">
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <DialogTitle className="flex items-center gap-2">
            <Scale className="size-4" /> 配平
          </DialogTitle>
          <DialogDescription>按目标反解权重，对现有奖盘改动最小；确认后填回奖盘</DialogDescription>
        </DialogHeader>
        {open ? (
          <SolverBody
            appKey={appKey}
            activityId={activityId}
            payload={payload}
            prizes={prizes}
            hasFortune={hasFortune}
            onApply={(weights) => {
              onApply(weights);
              onOpenChange(false);
            }}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function SolverBody({
  appKey,
  activityId,
  payload,
  prizes,
  hasFortune,
  onApply
}: Pick<DialogProps, "appKey" | "activityId" | "payload"> & {
  prizes: SolverPrize[];
  hasFortune: boolean;
  onApply: (weights: Map<number, number>) => void;
}) {
  const solve = useSolveLotteryMutation(appKey);
  const hasNone = prizes.some((prize) => prize.none);
  const [persona, setPersona] = useState<PersonaKey>("user");
  const [fortune, setFortune] = useState<LotterySolveFortune>("average");
  const [winRate, setWinRate] = useState("");
  const [realRate, setRealRate] = useState("");
  const [value, setValue] = useState("");
  const [valueMode, setValueMode] = useState<"max" | "equal">("max");
  const [pointsReturn, setPointsReturn] = useState("");
  const [total, setTotal] = useState<(typeof TOTALS)[number]>("auto");
  const [goals, setGoals] = useState<Record<number, PrizeGoalDraft>>({});
  const [result, setResult] = useState<LotterySolveResult | null>(null);

  const goalOf = (position: number): PrizeGoalDraft => goals[position] ?? { mode: "", probability: "", min: "", max: "" };
  const patchGoal = (position: number, next: Partial<PrizeGoalDraft>) =>
    setGoals({ ...goals, [position]: { ...goalOf(position), ...next } });

  const run = async () => {
    const prizeGoals: LotterySolvePrizeGoal[] = [];
    for (const prize of prizes) {
      const draft = goalOf(prize.position);
      if (draft.mode === "lock") prizeGoals.push({ position: prize.position, mode: "lock" });
      if (draft.mode === "target") {
        const probability = rate(draft.probability);
        if (!probability) {
          toast.error(`请填「${prize.name}」的目标概率`);
          return;
        }
        prizeGoals.push({ position: prize.position, mode: "target", probability });
      }
      if (draft.mode === "range") {
        const min = rate(draft.min);
        const max = rate(draft.max);
        if (!min && !max) {
          toast.error(`请填「${prize.name}」的概率区间`);
          return;
        }
        prizeGoals.push({ position: prize.position, mode: "range", min, max });
      }
    }
    const goal: LotterySolveGoal = {
      winRate: hasNone ? rate(winRate) : undefined,
      realRate: rate(realRate),
      value: amount(value),
      valueMode: amount(value) ? valueMode : undefined,
      pointsReturn: amount(pointsReturn),
      prizes: prizeGoals.length ? prizeGoals : undefined,
      total: total === "auto" ? undefined : Number(total)
    };
    if (!goal.winRate && !goal.realRate && !goal.value && !goal.pointsReturn && !prizeGoals.some((item) => item.mode !== "lock")) {
      toast.error("至少填一个目标");
      return;
    }
    try {
      setResult(
        await solve.mutateAsync({
          activityId,
          activity: payload(),
          persona: personaPayload(persona),
          fortune: hasFortune ? fortune : undefined,
          goal
        })
      );
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "求解失败");
    }
  };

  return (
    <>
      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-5 px-6 py-5">
          <div className="flex flex-wrap items-end gap-4">
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">目标提在谁的眼里</span>
              <Pills options={PERSONA_PRESETS} selected={[persona]} onToggle={setPersona} />
            </div>
            {hasFortune ? (
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">运气</span>
                <Pills
                  options={[
                    { key: "average", label: "平均" },
                    { key: "high", label: "最走运" },
                    { key: "low", label: "最倒霉" }
                  ]}
                  selected={[fortune]}
                  onToggle={setFortune}
                />
              </div>
            ) : null}
          </div>

          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <NumberField label="综合中奖率" value={winRate} unit="%" placeholder={hasNone ? "不设" : "没有谢谢参与"} disabled={!hasNone} onChange={setWinRate} />
            <NumberField label="真实奖品率" value={realRate} unit="%" placeholder="不设" onChange={setRealRate} />
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-[11px] font-medium">每抽期望价值</span>
                <ToggleGroup
                  type="single"
                  size="sm"
                  value={valueMode}
                  onValueChange={(next) => next && setValueMode(next as "max" | "equal")}
                >
                  <ToggleGroupItem value="max" className="h-5 px-1.5 text-[10px]">
                    上限
                  </ToggleGroupItem>
                  <ToggleGroupItem value="equal" className="h-5 px-1.5 text-[10px]">
                    等于
                  </ToggleGroupItem>
                </ToggleGroup>
              </div>
              <Input
                inputMode="decimal"
                value={value}
                placeholder="不设"
                onChange={(event) => setValue(event.target.value.replace(/[^\d.]/g, ""))}
                className="h-9 font-mono text-xs"
              />
            </div>
            <NumberField label="每抽积分回报上限" value={pointsReturn} unit="积分" placeholder="不设" onChange={setPointsReturn} />
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-[11px] font-medium">各奖品</span>
              <div className="flex items-center gap-1.5">
                <span className="text-[11px] text-muted-foreground">权重合计</span>
                <Select value={total} onValueChange={(next) => setTotal(next as (typeof TOTALS)[number])}>
                  <SelectTrigger className="h-7 w-28 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TOTALS.map((item) => (
                      <SelectItem key={item} value={item}>
                        {item === "auto" ? "自动" : formatCount(Number(item))}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="divide-y divide-border rounded-lg border border-border">
              {prizes.map((prize) => {
                const draft = goalOf(prize.position);
                return (
                  <div key={prize.position} className="grid grid-cols-[1.25rem_1fr_4.5rem_auto_11rem] items-center gap-2 px-2.5 py-1.5">
                    <span className="font-mono text-[11px] text-muted-foreground">{prize.position + 1}</span>
                    <span className="truncate text-xs">{prize.name}</span>
                    <span className="text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                      {formatProbability(prize.probability)}
                    </span>
                    <ToggleGroup
                      type="single"
                      size="sm"
                      value={draft.mode || "free"}
                      onValueChange={(next) => next && patchGoal(prize.position, { mode: (next === "free" ? "" : next) as PrizeMode })}
                    >
                      {PRIZE_MODES.map((mode) => (
                        <ToggleGroupItem key={mode.key || "free"} value={mode.key || "free"} className="h-6 px-1.5 text-[10px]">
                          {mode.label}
                        </ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                    <div className="flex items-center justify-end gap-1">
                      {draft.mode === "target" ? (
                        <PercentInput value={draft.probability} placeholder="综合概率" onChange={(text) => patchGoal(prize.position, { probability: text })} />
                      ) : null}
                      {draft.mode === "range" ? (
                        <>
                          <PercentInput value={draft.min} placeholder="下限" onChange={(text) => patchGoal(prize.position, { min: text })} />
                          <PercentInput value={draft.max} placeholder="上限" onChange={(text) => patchGoal(prize.position, { max: text })} />
                        </>
                      ) : null}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {result ? <SolveReport result={result} /> : null}
        </div>
      </ScrollArea>
      <footer className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
        <Button variant="outline" size="sm" onClick={run} disabled={solve.isPending}>
          {solve.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Wand2 className="size-3.5" />}
          求解
        </Button>
        <Button
          size="sm"
          disabled={!result}
          onClick={() => {
            if (!result) return;
            onApply(new Map(result.prizes.map((prize) => [prize.position, prize.newWeight])));
            toast.success("权重已填入奖盘，保存后生效");
          }}
        >
          <Check className="size-3.5" />
          填入奖盘
        </Button>
      </footer>
    </>
  );
}

function PercentInput({ value, placeholder, onChange }: { value: string; placeholder: string; onChange: (text: string) => void }) {
  return (
    <div className="relative w-[5.25rem]">
      <Input
        inputMode="decimal"
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value.replace(/[^\d.]/g, ""))}
        className="h-7 pr-5 text-right font-mono text-[11px]"
      />
      <span className="pointer-events-none absolute inset-y-0 right-1.5 flex items-center text-[10px] text-muted-foreground">%</span>
    </div>
  );
}

/** 目标值的写法：概率类写百分比，价值与积分写数 */
function checkValue(check: LotterySolveCheck, value: number) {
  return check.key === "value" || check.key === "pointsReturn" ? (value < 0 ? "抽不完" : formatValue(value)) : percent(value);
}

const CHECK_MODE: Record<LotterySolveCheck["mode"], string> = { equal: "=", max: "≤", min: "≥", lock: "=" };

function SolveReport({ result }: { result: LotterySolveResult }) {
  return (
    <div className="space-y-4 border-t border-border pt-5">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={result.feasible ? "success" : "warning"} size="sm">
          {result.feasible ? "目标全部达成" : "目标未能全部达成"}
        </Badge>
        <span className="text-[11px] text-muted-foreground">
          {result.persona}，运气 ×{formatValue(result.factor)}，权重合计 {formatCount(result.total)}
        </span>
      </div>

      {result.checks.length ? (
        <div className="divide-y divide-border rounded-lg border border-border">
          {result.checks.map((check) => (
            <div key={`${check.key}-${check.position}`} className="grid grid-cols-[1rem_1fr_auto] items-center gap-2 px-2.5 py-1.5 text-xs">
              {check.met ? (
                <Check className="size-3.5 text-emerald-600 dark:text-emerald-400" />
              ) : (
                <X className="size-3.5 text-rose-600 dark:text-rose-400" />
              )}
              <span className="truncate">
                {check.label} {CHECK_MODE[check.mode]} {checkValue(check, check.target)}
              </span>
              <span className="flex items-center gap-1 font-mono text-[11px] tabular-nums text-muted-foreground">
                {checkValue(check, check.before)}
                <ArrowRight className="size-3" />
                <span className="text-foreground">{checkValue(check, check.after)}</span>
              </span>
            </div>
          ))}
        </div>
      ) : null}

      <div className="grid gap-2 sm:grid-cols-4">
        <Metric label="综合中奖率" value={percent(result.after.winRate)} sub={`原 ${percent(result.before.winRate)}`} />
        <Metric label="真实奖品率" value={percent(result.after.realRate)} sub={`原 ${percent(result.before.realRate)}`} />
        <Metric label="每抽价值" value={formatValue(result.after.valuePerDraw)} sub={`原 ${formatValue(result.before.valuePerDraw)}`} />
        <Metric
          label="每抽积分回报"
          value={result.after.pointsReturn < 0 ? "抽不完" : formatValue(result.after.pointsReturn)}
          sub={`原 ${result.before.pointsReturn < 0 ? "抽不完" : formatValue(result.before.pointsReturn)}`}
        />
      </div>

      <div className="overflow-x-auto rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8">#</TableHead>
              <TableHead>奖品</TableHead>
              <TableHead className="text-right">权重</TableHead>
              <TableHead className="text-right">公示</TableHead>
              <TableHead className="text-right">综合</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {result.prizes.map((prize) => (
              <TableRow key={`${prize.prizeId}-${prize.position}`}>
                <TableCell className="font-mono text-[11px] text-muted-foreground">{prize.position + 1}</TableCell>
                <TableCell className="max-w-40 truncate text-xs">{prize.name || `位置 ${prize.position + 1}`}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  <Change before={formatCount(prize.weight)} after={formatCount(prize.newWeight)} />
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  <Change before={percent(prize.nominal)} after={percent(prize.newNominal)} />
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  <Change before={percent(prize.longRun)} after={percent(prize.newLongRun)} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <EconomyWarnings economy={result.economyAfter} />
      {result.notes.length ? (
        <ul className="space-y-0.5 text-[11px] text-muted-foreground">
          {result.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function Change({ before, after }: { before: string; after: string }) {
  if (before === after) return <span className="text-muted-foreground">{after}</span>;
  return (
    <span className="inline-flex items-center gap-1">
      <span className="text-muted-foreground">{before}</span>
      <ArrowRight className="size-3 text-muted-foreground" />
      {after}
    </span>
  );
}

// ── 推演 ──

export function LotteryComputeDialog({ appKey, activityId, payload, open, onOpenChange }: DialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl">
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <DialogTitle className="flex items-center gap-2">
            <Users className="size-4" /> 推演
          </DialogTitle>
          <DialogDescription>按当前编辑内容推演，不写入业务数据、不发奖</DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1">
          <div className="px-6 py-5">{open ? <ComputeBody appKey={appKey} activityId={activityId} payload={payload} /> : null}</div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function ComputeBody({ appKey, activityId, payload }: Pick<DialogProps, "appKey" | "activityId" | "payload">) {
  const [jobId, setJobId] = useState<number | null>(null);
  return <LotteryComputeRunner appKey={appKey} activityId={activityId} payload={payload} jobId={jobId} onJobChange={setJobId} />;
}
