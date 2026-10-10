"use client";

import { useMemo, useState } from "react";
import { Bar, CartesianGrid, Cell, ComposedChart, Line, XAxis, YAxis } from "recharts";
import { Calculator, Coins, Flag, Loader2, Play, Sparkles, Target, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import { useTestAdminAppSignInRewardMutation } from "@/lib/admin-hooks";
import type { SignInRewardPolicy, SignInRewardPreview } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  Field,
  MetricTile,
  NumInput,
  Panel,
  WEEKDAY_LABEL,
  buildRuleNames,
  describeEffect,
  formatNumber
} from "./signin-shared";

const DAY_OPTIONS = [7, 14, 30, 60, 90] as const;

const CHART: ChartConfig = {
  integralReward: { label: "积分", theme: { light: "#f59e0b", dark: "#fbbf24" } },
  experienceReward: { label: "经验", theme: { light: "#0ea5e9", dark: "#38bdf8" } }
};

function toLocalInput(date: Date) {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** 环境变量的展示顺序与中文名。 */
const ENV_LABEL: Array<[string, string]> = [
  ["consecutive_days", "连签天数"],
  ["total_sign_ins", "此前累计签到"],
  ["is_first_sign", "首次签到"],
  ["is_weekend", "周末"],
  ["weekday_iso", "星期"],
  ["year", "年"],
  ["month", "月"],
  ["day", "日"],
  ["hour", "时"],
  ["minute", "分"],
  ["user_experience", "用户经验"]
];

export function SignInSimulator({
  appKey,
  draft,
  saved,
  dirty
}: {
  appKey: string;
  draft: SignInRewardPolicy;
  saved: SignInRewardPolicy;
  dirty: boolean;
}) {
  const mutation = useTestAdminAppSignInRewardMutation(appKey);
  const [start, setStart] = useState(() => toLocalInput(new Date()));
  const [consecutive, setConsecutive] = useState(1);
  const [totalSignIns, setTotalSignIns] = useState(0);
  const [experience, setExperience] = useState(0);
  const [days, setDays] = useState(30);
  const [target, setTarget] = useState<"draft" | "saved">("draft");
  const [result, setResult] = useState<SignInRewardPreview | null>(null);
  const [error, setError] = useState<string[] | null>(null);

  const usingDraft = dirty && target === "draft";
  const tested = result?.policy ?? (usingDraft ? draft : saved);
  const ruleNames = useMemo(() => buildRuleNames(tested), [tested]);
  const sim = result?.simulation;

  const chartData = useMemo(
    () => (sim?.days ?? []).map((day) => ({ ...day, label: `第${day.day}天` })),
    [sim]
  );

  const ruleHits = useMemo(() => {
    if (!result) return [];
    const hits = result.simulation?.ruleHits ?? {};
    const rows = result.policy.rules
      .filter((rule) => rule.enabled)
      .map((rule) => ({ key: rule.key, name: rule.name || rule.key, hits: hits[rule.key] ?? 0, milestone: false, group: rule.group }));
    for (const m of result.policy.milestones) {
      const key = `milestone_${m.consecutiveDays}`;
      rows.push({ key, name: `${m.consecutiveDays} 天里程碑`, hits: hits[key] ?? 0, milestone: true, group: "" });
    }
    return rows;
  }, [result]);

  async function run() {
    const date = new Date(start);
    if (Number.isNaN(date.getTime())) {
      toast.error("起始时间无效");
      return;
    }
    setError(null);
    try {
      const preview = await mutation.mutateAsync({
        occurredAt: date.toISOString(),
        consecutiveDays: Math.max(1, consecutive),
        totalSignIns: Math.max(0, totalSignIns),
        userExperience: Math.max(0, experience),
        simulateDays: days,
        policy: usingDraft ? draft : undefined
      });
      setResult(preview);
    } catch (err) {
      const message = err instanceof ApiError ? err.message : "试算失败";
      setError(message.split(/;\s*/).filter(Boolean));
      setResult(null);
    }
  }

  return (
    <div className="space-y-4">
      <Panel
        title="试算参数"
        icon={<Calculator className="size-4" />}
        description="从起始时间那天开始逐日推演连续签到：每天连签加一、累计签到加一，当天获得的经验计入用户经验。"
        action={
          dirty ? (
            <ToggleGroup type="single" value={target} onValueChange={(v) => v && setTarget(v as "draft" | "saved")} className="rounded-lg border bg-muted/40 p-0.5">
              <ToggleGroupItem value="draft" className="h-7 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">未保存的草稿</ToggleGroupItem>
              <ToggleGroupItem value="saved" className="h-7 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">已生效策略</ToggleGroupItem>
            </ToggleGroup>
          ) : (
            <Badge variant="outline" size="sm">按已生效策略</Badge>
          )
        }
      >
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
          <Field label="起始时间" hint={`按 ${(usingDraft ? draft : saved).timezone} 判断日期`} className="col-span-2 lg:col-span-1">
            <Input type="datetime-local" className="h-9 text-sm" value={start} onChange={(e) => setStart(e.target.value)} />
          </Field>
          <Field label="起始连签" hint="第一天的连签天数"><NumInput value={consecutive} min={1} max={10_000} onChange={setConsecutive} suffix="天" /></Field>
          <Field label="此前累计签到" hint="0 表示首次签到"><NumInput value={totalSignIns} min={0} max={1_000_000} onChange={setTotalSignIns} suffix="次" /></Field>
          <Field label="用户当前经验" hint="影响等级经验倍率"><NumInput value={experience} min={0} max={100_000_000} onChange={setExperience} /></Field>
          <Field label="推演天数" className="col-span-2 lg:col-span-1">
            <ToggleGroup type="single" value={String(days)} onValueChange={(v) => v && setDays(Number(v))} className="w-full rounded-lg border bg-muted/40 p-0.5">
              {DAY_OPTIONS.map((n) => (
                <ToggleGroupItem key={n} value={String(n)} className="h-8 flex-1 rounded-md px-0 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
                  {n}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </Field>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <Button className="h-9 gap-1.5" disabled={mutation.isPending} onClick={() => void run()}>
            {mutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
            {result ? "重新试算" : "开始试算"}
          </Button>
          {result ? (
            <span className="text-xs text-muted-foreground">
              {result.draft ? "按草稿计算" : "按已生效策略计算"}，起始 {new Date(result.occurredAt).toLocaleString("zh-CN")}
            </span>
          ) : null}
        </div>
        {error ? (
          <div className="mt-4 flex gap-2.5 rounded-xl border border-red-200 bg-red-50/60 p-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" />
            <div className="min-w-0 space-y-1">
              <p className="font-medium">策略未通过校验</p>
              <ul className="list-disc space-y-0.5 pl-4 text-xs break-all">{error.map((line) => <li key={line}>{line}</li>)}</ul>
            </div>
          </div>
        ) : null}
      </Panel>

      {!result ? (
        mutation.isPending ? (
          <div className="flex h-48 items-center justify-center rounded-2xl border border-dashed text-sm text-muted-foreground">
            <Loader2 className="mr-2 size-4 animate-spin" />
            正在推演
          </div>
        ) : !error ? (
          <div className="flex h-48 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed text-center">
            <Calculator className="size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">设置参数后开始试算，查看每天能拿到多少奖励、哪些规则会被触发</p>
          </div>
        ) : null
      ) : (
        <div className={cn("space-y-4 transition-opacity", mutation.isPending && "opacity-60")}>
          {/* 汇总 */}
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <MetricTile label="首日奖励" icon={<Target className="size-3" />} value={`${formatNumber(result.reward.integralReward)} / ${formatNumber(result.reward.experienceReward)}`} hint="积分 / 经验" />
            <MetricTile label={`${sim?.days.length ?? 0} 天合计积分`} icon={<Coins className="size-3" />} tone="integral" value={formatNumber(sim?.totalIntegral)} hint={`日均 ${formatNumber((sim?.totalIntegral ?? 0) / Math.max(1, sim?.days.length ?? 1), 1)}，单日最高 ${formatNumber(sim?.peakIntegral)}`} />
            <MetricTile label={`${sim?.days.length ?? 0} 天合计经验`} icon={<Sparkles className="size-3" />} tone="experience" value={formatNumber(sim?.totalExperience)} hint={`日均 ${formatNumber((sim?.totalExperience ?? 0) / Math.max(1, sim?.days.length ?? 1), 1)}，单日最高 ${formatNumber(sim?.peakExperience)}`} />
            <MetricTile label="里程碑" icon={<Flag className="size-3" />} value={formatNumber(sim?.days.filter((d) => d.milestone).length)} hint="推演期内达成次数" />
          </div>

          {/* 曲线 */}
          <Panel title="每日奖励曲线" description="柱为积分，线为经验；橙色柱表示当天达成里程碑。">
            <ChartContainer config={CHART} className="h-64 w-full">
              <ComposedChart data={chartData} margin={{ top: 8, right: 0, bottom: 0, left: -8 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/50" />
                <XAxis dataKey="day" tickLine={false} axisLine={false} fontSize={11} minTickGap={16} tickFormatter={(v) => `${v}`} />
                <YAxis yAxisId="integral" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={40} />
                <YAxis yAxisId="experience" orientation="right" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={44} />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      labelFormatter={(_, payload) => {
                        const day = payload?.[0]?.payload as (typeof chartData)[number] | undefined;
                        return day ? `第 ${day.day} 天，${day.date} ${WEEKDAY_LABEL[day.weekdayIso]}，连签 ${day.consecutiveDays} 天` : "";
                      }}
                    />
                  }
                />
                <Bar yAxisId="integral" dataKey="integralReward" radius={[3, 3, 0, 0]} maxBarSize={18}>
                  {chartData.map((day) => (
                    <Cell key={day.day} fill={day.milestone ? "#f97316" : "var(--color-integralReward)"} />
                  ))}
                </Bar>
                <Line yAxisId="experience" dataKey="experienceReward" type="monotone" stroke="var(--color-experienceReward)" strokeWidth={2} dot={false} />
              </ComposedChart>
            </ChartContainer>
          </Panel>

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            {/* 首日拆解 */}
            <Panel title="首日计算拆解" description="起始那一天命中的规则与最终结果">
              <ol className="space-y-2 text-sm">
                <li className="flex items-center justify-between gap-3 rounded-lg bg-muted/40 px-3 py-2">
                  <span>基础积分</span>
                  <span className="font-mono tabular-nums">{formatNumber(result.policy.baseIntegral)}</span>
                </li>
                {result.appliedRules.length === 0 ? (
                  <li className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">未命中任何规则或里程碑，按基础奖励发放</li>
                ) : (
                  result.appliedRules.map((rule) => (
                    <li key={rule.key} className="rounded-lg border px-3 py-2">
                      <div className="flex items-center justify-between gap-2">
                        <span className="min-w-0 truncate font-medium">{rule.name}</span>
                        {rule.consecutiveDays ? <Badge variant="warning" size="sm">里程碑</Badge> : rule.group ? <Badge variant="outline" size="sm">{rule.group}</Badge> : null}
                      </div>
                      <div className="mt-0.5 text-xs text-emerald-700 dark:text-emerald-300">{describeEffect(rule)}</div>
                      {rule.expression ? <code className="mt-0.5 block truncate font-mono text-[11px] text-muted-foreground [font-variant-ligatures:none]">{rule.expression}</code> : null}
                    </li>
                  ))
                )}
                <li className="grid grid-cols-3 gap-2 rounded-lg border-2 border-foreground/10 px-3 py-2.5 text-center">
                  <div>
                    <div className="text-[11px] text-muted-foreground">积分</div>
                    <div className="text-base font-semibold text-amber-700 tabular-nums dark:text-amber-300">{formatNumber(result.reward.integralReward)}</div>
                  </div>
                  <div>
                    <div className="text-[11px] text-muted-foreground">经验</div>
                    <div className="text-base font-semibold text-sky-700 tabular-nums dark:text-sky-300">{formatNumber(result.reward.experienceReward)}</div>
                  </div>
                  <div>
                    <div className="text-[11px] text-muted-foreground">积分倍率</div>
                    <div className="text-base font-semibold tabular-nums">×{formatNumber(result.reward.rewardMultiplier, 2)}</div>
                  </div>
                </li>
              </ol>
              {result.reward.bonusDescription ? <p className="mt-2 text-xs text-muted-foreground">返回给客户端的说明：{result.reward.bonusDescription}</p> : null}
              <div className="mt-4">
                <div className="mb-2 text-xs font-medium">首日表达式变量</div>
                <dl className="grid grid-cols-2 gap-1.5 sm:grid-cols-3">
                  {ENV_LABEL.filter(([key]) => key in result.environment).map(([key, label]) => (
                    <div key={key} className="min-w-0 rounded-lg bg-muted/40 px-2.5 py-1.5">
                      <dt className="truncate font-mono text-[10px] text-muted-foreground">{key}</dt>
                      <dd className="truncate text-xs">
                        {label}：{String(result.environment[key]) === "true" ? "是" : String(result.environment[key]) === "false" ? "否" : String(result.environment[key])}
                      </dd>
                    </div>
                  ))}
                </dl>
              </div>
            </Panel>

            {/* 规则命中 */}
            <Panel title="规则命中情况" description={`推演 ${sim?.days.length ?? 0} 天内每条已启用规则命中的天数`}>
              {ruleHits.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">没有已启用的规则或里程碑</p>
              ) : (
                <ul className="space-y-2">
                  {ruleHits.map((row) => {
                    const total = Math.max(1, sim?.days.length ?? 1);
                    return (
                      <li key={row.key} className="text-xs">
                        <div className="flex items-center justify-between gap-2">
                          <span className="flex min-w-0 items-center gap-1.5">
                            {row.milestone ? <Flag className="size-3 shrink-0 text-orange-500" /> : null}
                            <span className="truncate">{row.name}</span>
                            {row.group ? <span className="shrink-0 text-[10px] text-muted-foreground">{row.group}</span> : null}
                          </span>
                          <span className={cn("shrink-0 tabular-nums", row.hits === 0 && "text-muted-foreground")}>
                            {row.hits === 0 ? "未命中" : `${row.hits} 天`}
                          </span>
                        </div>
                        <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
                          <div className={cn("h-full rounded-full", row.milestone ? "bg-orange-500" : "bg-emerald-500")} style={{ width: `${(row.hits / total) * 100}%` }} />
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
              {ruleHits.some((row) => row.hits === 0 && !row.milestone) ? (
                <p className="mt-3 text-[11px] text-muted-foreground">未命中的规则可能被同组更高优先级的规则挡住，或条件在推演期内没有出现。</p>
              ) : null}
            </Panel>
          </div>

          {/* 逐日明细 */}
          <Panel title="逐日明细" bodyClassName="p-0">
            <div className="max-h-[28rem] overflow-y-auto">
              <Table>
                <TableHeader className="sticky top-0 z-10 bg-card">
                  <TableRow className="bg-muted/30 hover:bg-muted/30">
                    <TableHead className="h-9 pl-4 text-xs">日期</TableHead>
                    <TableHead className="h-9 text-right text-xs">连签</TableHead>
                    <TableHead className="h-9 text-right text-xs">积分</TableHead>
                    <TableHead className="h-9 text-right text-xs">经验</TableHead>
                    <TableHead className="hidden h-9 text-right text-xs sm:table-cell">累计积分</TableHead>
                    <TableHead className="hidden h-9 pr-4 text-xs md:table-cell">命中</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(sim?.days ?? []).map((day) => (
                    <TableRow key={day.day} className={cn(day.milestone && "bg-orange-500/5")}>
                      <TableCell className="py-2 pl-4">
                        <div className="text-sm tabular-nums">{day.date.slice(5)} <span className="text-xs text-muted-foreground">{WEEKDAY_LABEL[day.weekdayIso]}</span></div>
                        <div className="text-[11px] text-muted-foreground">第 {day.day} 天</div>
                        {/* 窄屏把命中规则放在日期下面 */}
                        {day.appliedRules.length ? (
                          <div className="mt-1 flex flex-wrap gap-1 md:hidden">
                            {day.appliedRules.map((key) => <span key={key} className="rounded bg-muted px-1 text-[10px]">{ruleNames[key] ?? key}</span>)}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell className="py-2 text-right text-sm tabular-nums">{day.consecutiveDays}</TableCell>
                      <TableCell className="py-2 text-right">
                        <div className="text-sm font-medium text-amber-700 tabular-nums dark:text-amber-300">{formatNumber(day.integralReward)}</div>
                        {day.rewardMultiplier !== 1 ? <div className="text-[10px] text-muted-foreground tabular-nums">×{formatNumber(day.rewardMultiplier, 2)}</div> : null}
                      </TableCell>
                      <TableCell className="py-2 text-right text-sm font-medium text-sky-700 tabular-nums dark:text-sky-300">{formatNumber(day.experienceReward)}</TableCell>
                      <TableCell className="hidden py-2 text-right text-xs text-muted-foreground tabular-nums sm:table-cell">{formatNumber(day.cumulativeIntegral)}</TableCell>
                      <TableCell className="hidden max-w-80 py-2 pr-4 md:table-cell">
                        {day.appliedRules.length ? (
                          <div className="flex flex-wrap gap-1">
                            {day.appliedRules.map((key) => (
                              <Badge key={key} variant={key.startsWith("milestone_") ? "warning" : "outline"} size="sm">{ruleNames[key] ?? key}</Badge>
                            ))}
                          </div>
                        ) : (
                          <span className="text-xs text-muted-foreground">基础奖励</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </Panel>
        </div>
      )}
    </div>
  );
}
