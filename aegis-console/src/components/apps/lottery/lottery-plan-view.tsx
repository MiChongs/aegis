"use client";

import { useMemo, useState } from "react";
import { ArrowRight, CircleCheck, CircleDashed, CircleSlash, Search, ShieldAlert, Sparkles, Target } from "lucide-react";
import { Segmented } from "@/components/apps/lottery/editor/editor-fields";
import { KindGlyph } from "@/components/apps/lottery/editor/prize-visuals";
import { formatProbability } from "@/components/apps/lottery/lottery-shared";
import {
  RiskActionChip,
  riskActionDetail,
  riskActionVisual,
  riskConditionText,
  riskGauge,
  riskSignalOf,
  riskValueText
} from "@/components/apps/lottery/lottery-risk-shared";
import { FACTOR_LABEL } from "@/components/apps/lottery/lottery-strategy-form";
import { Input } from "@/components/ui/input";
import type {
  LotteryBehaviorScore,
  LotteryCatalog,
  LotteryDrawPlan,
  LotteryPrizeOdds,
  LotteryRiskCheck,
  LotterySignalSpec
} from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/**
 * 出奖方案：一次抽奖「为什么是这个结果」。
 *
 * 从上到下回答四个问题：抽中了什么、怎么抽中的（结论）→ 风控每条规则的取值与阈值 → 各维度动了哪些奖品 →
 * 每个奖品从基础概率到最终概率的变化（可筛选、排序、搜索）。抽奖记录的 trace、用户详情、模拟里「此人下一次」共用。
 *
 * 基础概率按这个人的奖盘算（已含人群特权与每日运气），原始权重放在悬停提示里：几亿的权重数字本身看不出任何东西。
 */

/** 0–1 的比例按百分比显示；不传 digits 时按公示概率取整，十万分之一显示 0.001% 而不是 0.00% */
export const percent = (value: number, digits?: number) =>
  digits === undefined ? formatProbability(value * 100) : `${(value * 100).toFixed(digits)}%`;

function factorText(value: number) {
  if (value === 0) return "停发";
  return `×${Number(value.toFixed(3))}`;
}

type OddsRow = LotteryPrizeOdds & { name: string; kind?: string; baseShare: number };
type RowFilter = "all" | "changed" | "blocked";
type RowSort = "position" | "final";

export function DrawPlanView({
  plan,
  prizeName,
  prizeKind,
  signals,
  catalog
}: {
  plan: LotteryDrawPlan;
  /** 按奖品 ID 取名字（奖品可能已被删，取不到时显示位置） */
  prizeName: (id: number, position: number) => string;
  /** 按奖品 ID 取类型（画图标用；取不到时用中性图标） */
  prizeKind?: (id: number) => string | undefined;
  signals?: LotterySignalSpec[];
  catalog?: LotteryCatalog | null;
}) {
  const rows = useMemo<OddsRow[]>(() => {
    const baseTotal = plan.odds.reduce((sum, odds) => sum + Math.max(0, odds.base), 0);
    return plan.odds.map((odds) => ({
      ...odds,
      name: prizeName(odds.id, odds.pos),
      kind: prizeKind?.(odds.id),
      baseShare: baseTotal > 0 ? Math.max(0, odds.base) / baseTotal : 0
    }));
  }, [plan.odds, prizeName, prizeKind]);
  const picked = rows.find((row) => row.id === plan.picked);
  const floor = rows.find((row) => row.id === plan.floor);

  return (
    <div className="space-y-4">
      <Verdict plan={plan} picked={picked} floor={floor} rows={rows} catalog={catalog} />
      {plan.riskReport || plan.risk ? <RiskSection plan={plan} catalog={catalog} /> : null}
      <FactorSummary rows={rows} />
      <OddsTable rows={rows} plan={plan} />
      {plan.perks?.names.length ? <PerksLine perks={plan.perks} /> : null}
      {plan.behavior ? <BehaviorScoreView score={plan.behavior} signals={signals} /> : null}
    </div>
  );
}

// ── 结论 ──

function Verdict({
  plan,
  picked,
  floor,
  rows,
  catalog
}: {
  plan: LotteryDrawPlan;
  picked?: OddsRow;
  floor?: OddsRow;
  rows: OddsRow[];
  catalog?: LotteryCatalog | null;
}) {
  const report = plan.riskReport;
  const severe = report?.action === "floor" || report?.action === "deny" || (!report && plan.risk);
  const firstHit = report?.checks.find((check) => check.hit && (check.action === "floor" || check.action === "deny"));
  const allZero = rows.every((row) => row.p === 0);

  let how: React.ReactNode;
  let tone = "border-emerald-500/30 bg-emerald-500/[0.06]";
  if (plan.pity) {
    how = "连续未中到达硬保底，直接发放保底奖";
    tone = "border-sky-500/30 bg-sky-500/[0.06]";
  } else if (plan.fallback && plan.fallbackReason === "contended") {
    how = "抽中的限量奖品在占库存时被并发抢光，改发兜底奖";
    tone = "border-amber-500/30 bg-amber-500/[0.06]";
  } else if (plan.fallback) {
    how = severe
      ? "风控只发兜底，兜底奖在这一次的权重为 0，按兜底规则直接发放"
      : allZero
        ? "这一次所有奖品的最终概率都是 0（库存、限量、预算或规则把奖盘清空了），改发兜底奖"
        : "改发兜底奖";
    tone = "border-amber-500/30 bg-amber-500/[0.06]";
  } else if (severe) {
    how = firstHit ? (
      <>
        风控「{firstHit.name || riskSignalOf(firstHit.signal, catalog).label}」命中，兜底奖以外的中奖奖品停发，只能抽到兜底奖
      </>
    ) : (
      "命中同设备或同网络上限（旧版记录，未记下取值），只能抽到兜底奖"
    );
    tone = "border-orange-500/30 bg-orange-500/[0.06]";
  } else if (picked) {
    how = <>按最终概率 {percent(picked.p)} 抽中</>;
  }

  const fairChanged = plan.fair !== undefined && Math.abs(plan.fair - plan.winRate) > 1e-6;
  const stats: Array<{ label: string; value: React.ReactNode; hint?: string }> = [
    {
      label: "中奖率",
      value: fairChanged ? (
        <span className="inline-flex items-center gap-1">
          <span className="text-muted-foreground">{percent(plan.fair ?? 0)}</span>
          <ArrowRight className="size-3 text-muted-foreground" />
          {percent(plan.winRate)}
        </span>
      ) : (
        percent(plan.winRate)
      ),
      hint: fairChanged ? "左为概率控制层之前，右为最终" : "这一次抽到中奖奖品的总概率"
    }
  ];
  if (plan.value) stats.push({ label: "期望价值", value: plan.value.toFixed(2), hint: "积分当量" });
  if (plan.fortune) {
    stats.push({
      label: "每日运气",
      value: plan.fortuneBase ? (
        <span className="inline-flex items-center gap-1">
          <span className="text-muted-foreground">×{plan.fortuneBase.toFixed(2)}</span>
          <ArrowRight className="size-3 text-muted-foreground" />×{plan.fortune.toFixed(2)}
        </span>
      ) : (
        `×${plan.fortune.toFixed(2)}`
      ),
      hint: plan.fortuneBase ? "至尊加成前后" : "乘在兜底奖以外的奖品上"
    });
  }
  if (plan.luck) stats.push({ label: "运气 z", value: `${plan.luck > 0 ? "+" : ""}${plan.luck.toFixed(2)}`, hint: "负数为比期望倒霉" });
  if (plan.prd) stats.push({ label: "伪随机", value: percent(plan.prd), hint: "伪随机分布给这一次定的中奖率" });
  if (plan.softPity) stats.push({ label: "软保底", value: `+${plan.softPity.toFixed(1)} 点`, hint: "中奖总概率提高的百分点" });

  return (
    <div className={cn("overflow-hidden rounded-xl border", tone)}>
      <div className="flex items-start gap-3 p-3.5">
        <KindGlyph type={picked?.kind ?? "manual"} size="lg" />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-muted-foreground">抽中</span>
            <span className="truncate text-base font-semibold">{picked?.name ?? "未记录"}</span>
            {plan.pity ? <Tag className="bg-sky-500/12 text-sky-700 dark:text-sky-300">保底</Tag> : null}
            {plan.fallback ? <Tag className="bg-amber-500/12 text-amber-700 dark:text-amber-300">改发兜底</Tag> : null}
            {picked && picked.id === floor?.id && !plan.fallback ? (
              <Tag className="bg-zinc-500/12 text-zinc-600 dark:text-zinc-300">兜底奖</Tag>
            ) : null}
            {plan.guardInfeasible ? <Tag className="bg-rose-500/12 text-rose-700 dark:text-rose-300">护栏不可行</Tag> : null}
          </div>
          {how ? <p className="text-xs leading-relaxed text-muted-foreground">{how}</p> : null}
        </div>
        {picked ? (
          <div className="shrink-0 text-right">
            <p className="font-mono text-lg font-semibold tabular-nums">{percent(picked.p)}</p>
            <p className="text-[10px] text-muted-foreground">这一次的概率</p>
          </div>
        ) : null}
      </div>
      <div className="grid grid-cols-2 gap-px border-t border-inherit bg-border/40 sm:grid-cols-3 lg:grid-cols-6">
        {stats.map((stat) => (
          <div key={stat.label} className="bg-background/70 px-3 py-2 dark:bg-background/40" title={stat.hint}>
            <p className="text-[10px] text-muted-foreground">{stat.label}</p>
            <p className="font-mono text-xs font-medium tabular-nums">{stat.value}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

function Tag({ className, children }: { className?: string; children: React.ReactNode }) {
  return <span className={cn("rounded px-1.5 py-px text-[10px] font-medium", className)}>{children}</span>;
}

// ── 风控 ──

function RiskSection({ plan, catalog }: { plan: LotteryDrawPlan; catalog?: LotteryCatalog | null }) {
  const report = plan.riskReport;
  const hits = report?.checks.filter((check) => check.hit).length ?? (plan.risk ? 1 : 0);
  return (
    <section className="space-y-2">
      <SectionTitle
        icon={<ShieldAlert className="size-3.5" />}
        title="风控"
        aside={
          report?.action ? (
            <RiskActionChip action={report.action} catalog={catalog} />
          ) : plan.risk ? (
            <RiskActionChip action="floor" catalog={catalog} />
          ) : (
            <span className="text-[11px] text-emerald-600 dark:text-emerald-400">全部通过</span>
          )
        }
        hint={report ? `${report.checks.length} 条规则，命中 ${hits} 条${report.noPity ? "，保底不触发" : ""}` : undefined}
      />
      {report ? (
        <div className="divide-y divide-border overflow-hidden rounded-xl border border-border">
          {report.checks.map((check, index) => (
            <RiskCheckRow key={`${check.signal}-${index}`} check={check} catalog={catalog} />
          ))}
        </div>
      ) : (
        <p className="rounded-xl border border-dashed border-border px-3 py-3 text-xs text-muted-foreground">
          旧版记录：命中了同设备或同网络账号数上限，当时没有记下具体取值与阈值
        </p>
      )}
    </section>
  );
}

function RiskCheckRow({ check, catalog }: { check: LotteryRiskCheck; catalog?: LotteryCatalog | null }) {
  const spec = riskSignalOf(check.signal, catalog);
  const visual = riskActionVisual(check.action);
  const gauge = riskGauge(check);
  const detail = riskActionDetail(check);
  return (
    <div className={cn("grid gap-2 px-3 py-2.5 sm:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_auto] sm:items-center", check.hit && "bg-muted/40")}>
      <div className="flex min-w-0 items-start gap-2">
        {check.missing ? (
          <CircleDashed className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
        ) : check.hit ? (
          <CircleSlash className={cn("mt-0.5 size-3.5 shrink-0", visual.text)} />
        ) : (
          <CircleCheck className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
        )}
        <div className="min-w-0">
          <p className={cn("truncate text-xs", check.hit && "font-medium")}>{check.name || spec.label}</p>
          <p className="truncate text-[10px] text-muted-foreground">
            {riskConditionText(check, catalog)}
            {check.missing ? "，这一次取不到，按未命中" : ""}
          </p>
        </div>
      </div>
      {spec.bool ? (
        <span className={cn("text-xs font-medium", check.hit ? visual.text : "text-muted-foreground")}>
          {check.missing ? "取不到" : riskValueText(check.value, spec)}
        </span>
      ) : (
        <div className="flex items-center gap-2">
          <span className="relative h-1.5 flex-1 rounded-full bg-muted">
            <span
              className={cn("absolute inset-y-0 left-0 rounded-full", check.hit ? visual.bar : "bg-emerald-400/70")}
              style={{ width: `${gauge.value * 100}%` }}
            />
            <span
              className="absolute -top-1 h-3.5 w-0.5 rounded-full bg-foreground/60"
              style={{ left: `calc(${gauge.threshold * 100}% - 1px)` }}
              title={`阈值 ${riskValueText(check.threshold, spec)}`}
            />
          </span>
          <span className={cn("w-16 shrink-0 text-right font-mono text-xs tabular-nums", check.hit && visual.text)}>
            {check.missing ? "—" : riskValueText(check.value, spec)}
          </span>
        </div>
      )}
      <div className="flex justify-end">
        <RiskActionChip
          action={check.action}
          catalog={catalog}
          className={cn(!check.hit && "opacity-45 grayscale")}
          suffix={detail ? ` ${detail}` : null}
        />
      </div>
    </div>
  );
}

// ── 各维度汇总 ──

function FactorSummary({ rows }: { rows: OddsRow[] }) {
  const items = useMemo(() => {
    const map = new Map<string, { key: string; label: string; blocked: number; up: number; down: number }>();
    for (const row of rows) {
      for (const factor of row.f ?? []) {
        const label = factor.k === "rule" && factor.n ? `规则「${factor.n.split("：")[0]}」` : (FACTOR_LABEL[factor.k] ?? factor.k);
        const key = factor.k === "rule" ? `rule:${label}` : factor.k;
        const item = map.get(key) ?? { key, label, blocked: 0, up: 0, down: 0 };
        if (factor.v === 0) item.blocked++;
        else if (factor.v > 1) item.up++;
        else if (factor.v < 1) item.down++;
        map.set(key, item);
      }
    }
    return [...map.values()];
  }, [rows]);
  if (!items.length) return null;
  return (
    <section className="space-y-2">
      <SectionTitle icon={<Sparkles className="size-3.5" />} title="各维度的作用" hint="按维度数一数动了几个奖品" />
      <div className="flex flex-wrap gap-1.5">
        {items.map((item) => (
          <span key={item.key} className="inline-flex items-center gap-1.5 rounded-lg border border-border px-2 py-1 text-[11px]">
            <span className="font-medium">{item.label}</span>
            {item.blocked ? <span className="text-rose-600 dark:text-rose-400">停发 {item.blocked}</span> : null}
            {item.up ? <span className="text-emerald-600 dark:text-emerald-400">调高 {item.up}</span> : null}
            {item.down ? <span className="text-amber-600 dark:text-amber-400">调低 {item.down}</span> : null}
          </span>
        ))}
      </div>
    </section>
  );
}

// ── 奖品表 ──

function OddsTable({ rows, plan }: { rows: OddsRow[]; plan: LotteryDrawPlan }) {
  const [filter, setFilter] = useState<RowFilter>("all");
  const [sort, setSort] = useState<RowSort>("position");
  const [keyword, setKeyword] = useState("");

  const changed = rows.filter((row) => row.f?.length).length;
  const blocked = rows.filter((row) => row.blocked || (row.f ?? []).some((factor) => factor.v === 0)).length;
  const visible = useMemo(() => {
    const needle = keyword.trim().toLowerCase();
    const list = rows.filter((row) => {
      if (filter === "changed" && !row.f?.length) return false;
      if (filter === "blocked" && !(row.blocked || (row.f ?? []).some((factor) => factor.v === 0))) return false;
      return !needle || row.name.toLowerCase().includes(needle) || String(row.pos + 1) === needle;
    });
    return sort === "final" ? [...list].sort((a, b) => b.p - a.p || a.pos - b.pos) : list;
  }, [rows, filter, sort, keyword]);
  const maxShare = Math.max(0.0001, ...rows.map((row) => Math.max(row.p, row.baseShare)));

  return (
    <section className="space-y-2">
      <SectionTitle icon={<Target className="size-3.5" />} title="每个奖品" hint="基础概率按这个人的奖盘算（已含特权与每日运气）" />
      <div className="flex flex-wrap items-center gap-2">
        <Segmented
          size="sm"
          value={filter}
          onChange={setFilter}
          options={[
            { value: "all", label: `全部 ${rows.length}` },
            { value: "changed", label: `有调整 ${changed}` },
            { value: "blocked", label: `停发 ${blocked}` }
          ]}
        />
        <Segmented
          size="sm"
          value={sort}
          onChange={setSort}
          options={[
            { value: "position", label: "按位置" },
            { value: "final", label: "按最终概率" }
          ]}
        />
        <div className="relative ml-auto w-40">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="奖品名或位置"
            className="h-7 pl-7 text-xs"
          />
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-border">
        <div className="hidden grid-cols-[2rem_minmax(0,1fr)_5.5rem_1rem_5.5rem] items-center gap-2 border-b border-border bg-muted/40 px-3 py-1.5 text-[10px] font-medium text-muted-foreground sm:grid">
          <span>位置</span>
          <span>奖品与调整</span>
          <span className="text-right">基础</span>
          <span />
          <span className="text-right">最终</span>
        </div>
        {visible.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">没有符合条件的奖品</p>
        ) : (
          <div className="divide-y divide-border">
            {visible.map((row) => (
              <OddsRowView key={`${row.id}-${row.pos}`} row={row} picked={plan.picked === row.id} floor={plan.floor === row.id} maxShare={maxShare} />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

function OddsRowView({ row, picked, floor, maxShare }: { row: OddsRow; picked: boolean; floor: boolean; maxShare: number }) {
  const stopped = row.p === 0 && (row.blocked || (row.f ?? []).some((factor) => factor.v === 0));
  const delta = row.p - row.baseShare;
  const blockNote = row.blocked ? (row.f ?? []).find((factor) => factor.k === row.blocked)?.n : undefined;
  return (
    <div className={cn("space-y-1.5 px-3 py-2.5", picked && "bg-emerald-500/[0.07]", stopped && !picked && "bg-muted/25")}>
      <div className="grid grid-cols-[2rem_minmax(0,1fr)_auto] items-center gap-2 sm:grid-cols-[2rem_minmax(0,1fr)_5.5rem_1rem_5.5rem]">
        <span className="font-mono text-[11px] text-muted-foreground">{row.pos + 1}</span>
        <div className="flex min-w-0 items-center gap-2">
          <KindGlyph type={row.kind ?? "manual"} size="sm" />
          <span className={cn("truncate text-xs", picked && "font-semibold", stopped && "text-muted-foreground line-through decoration-muted-foreground/40")}>
            {row.name}
          </span>
          {picked ? <Tag className="bg-emerald-500/15 text-emerald-700 dark:text-emerald-300">抽中</Tag> : null}
          {floor ? <Tag className="bg-zinc-500/12 text-zinc-600 dark:text-zinc-300">兜底</Tag> : null}
        </div>
        <span
          className="hidden text-right font-mono text-[11px] tabular-nums text-muted-foreground sm:block"
          title={`原始权重 ${row.base.toLocaleString("zh-CN")}`}
        >
          {percent(row.baseShare)}
        </span>
        <ArrowRight className="hidden size-3 text-muted-foreground/60 sm:block" />
        <span
          className={cn(
            "text-right font-mono text-xs font-medium tabular-nums",
            stopped ? "text-muted-foreground" : delta > 1e-6 ? "text-emerald-600 dark:text-emerald-400" : delta < -1e-6 ? "text-amber-600 dark:text-amber-400" : ""
          )}
          title={`最终权重 ${Number(row.w.toFixed(3)).toLocaleString("zh-CN")}`}
        >
          {stopped ? "停发" : percent(row.p)}
        </span>
      </div>

      <div className="ml-10 space-y-1.5">
        <span className="relative block h-1.5 overflow-hidden rounded-full bg-muted">
          {/* 浅色是基础概率，实色是最终概率 */}
          <span className="absolute inset-y-0 left-0 rounded-full bg-foreground/12" style={{ width: `${(row.baseShare / maxShare) * 100}%` }} />
          <span
            className={cn("absolute inset-y-0 left-0 rounded-full", picked ? "bg-emerald-500" : "bg-emerald-500/70")}
            style={{ width: `${(row.p / maxShare) * 100}%` }}
          />
        </span>
        {row.f?.length ? (
          <div className="flex flex-wrap gap-1">
            {row.f.map((factor, index) => (
              <span
                key={`${factor.k}-${index}`}
                title={factor.n}
                className={cn(
                  "rounded px-1.5 py-px text-[10px]",
                  factor.v === 0
                    ? "bg-rose-500/10 text-rose-600 dark:text-rose-400"
                    : factor.v > 1
                      ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
                      : factor.v === 1
                        ? "bg-muted text-muted-foreground"
                        : "bg-amber-500/10 text-amber-700 dark:text-amber-400"
                )}
              >
                {factor.k === "rule" && factor.n ? `规则「${factor.n.split("：")[0]}」` : (FACTOR_LABEL[factor.k] ?? factor.k)} {factorText(factor.v)}
                {factor.k !== "rule" && factor.k !== "risk" && factor.n ? `（${factor.n}）` : ""}
              </span>
            ))}
          </div>
        ) : null}
        {blockNote ? <p className="text-[10px] text-muted-foreground">{blockNote}</p> : null}
      </div>
    </div>
  );
}

// ── 特权与行为分 ──

function PerksLine({ perks }: { perks: NonNullable<LotteryDrawPlan["perks"]> }) {
  return (
    <section className="space-y-2">
      <SectionTitle icon={<Sparkles className="size-3.5" />} title="人群特权" hint={perks.names.join("、")} />
      <p className="rounded-xl border border-border px-3 py-2 text-xs text-muted-foreground">{perkEffectsText(perks) || "这一次没有改变概率"}</p>
    </section>
  );
}

function SectionTitle({ icon, title, hint, aside }: { icon: React.ReactNode; title: string; hint?: string; aside?: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2">
      <span className="flex size-5 items-center justify-center rounded-md bg-muted text-muted-foreground">{icon}</span>
      <span className="text-xs font-medium">{title}</span>
      {hint ? <span className="min-w-0 truncate text-[10px] text-muted-foreground">{hint}</span> : null}
      {aside ? <span className="ml-auto shrink-0">{aside}</span> : null}
    </div>
  );
}

export function BehaviorScoreView({ score, signals }: { score: LotteryBehaviorScore; signals?: LotterySignalSpec[] }) {
  const specOf = (key: string) => signals?.find((spec) => spec.key === key);
  return (
    <div className="space-y-2 rounded-xl border border-border p-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium">行为分</span>
        <span className="font-mono text-xs tabular-nums">
          {score.score.toFixed(3)}
          <span className="ml-2 text-muted-foreground">中奖权重 ×{score.factor.toFixed(3)}</span>
        </span>
      </div>
      <div className="grid gap-x-4 gap-y-1 sm:grid-cols-2">
        {score.signals.map((signal) => {
          const spec = specOf(signal.key);
          return (
            <div key={signal.key} className="flex items-center gap-2 text-[11px]">
              <span className="w-28 shrink-0 truncate text-muted-foreground">{spec?.label ?? signal.key}</span>
              <span className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                <span
                  className={cn("block h-full rounded-full", spec?.negative ? "bg-rose-500" : "bg-emerald-500")}
                  style={{ width: `${Math.min(100, signal.score * 100)}%` }}
                />
              </span>
              <span className="w-16 shrink-0 text-right font-mono tabular-nums">
                {spec?.key === "memberRate" ? percent(signal.value, 0) : Number(signal.value.toFixed(1)).toLocaleString("zh-CN")}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** 一次抽奖生效的特权效果 */
function perkEffectsText(perks: NonNullable<LotteryDrawPlan["perks"]>) {
  const parts: string[] = [];
  if (perks.extraFree) parts.push(`免费 +${perks.extraFree}`);
  if (perks.adSkip) parts.push(`免看广告 ${percent(perks.adSkip)}${perks.waivedUsed ? `（今天已用 ${perks.waivedUsed} 次）` : ""}`);
  if (perks.pointsDiscount) parts.push(`积分省 ${perks.pointsDiscount}%`);
  if (perks.extraPoints) parts.push(`积分次数 +${perks.extraPoints}`);
  if (perks.pityCut) parts.push(`保底提前 ${perks.pityCut}`);
  if (perks.winBoost) parts.push(`中奖 ×${perks.winBoost}`);
  return parts.join("，");
}
