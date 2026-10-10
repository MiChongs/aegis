"use client";

import { useMemo, useState } from "react";
import {
  ArrowDownUp,
  BookOpen,
  ChevronDown,
  Copy,
  Flag,
  Gift,
  Layers,
  LayoutTemplate,
  Plus,
  RotateCcw,
  Settings2,
  Sparkles,
  Trash2,
  TrendingUp,
  Zap
} from "lucide-react";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useSignInRewardTemplatesQuery } from "@/lib/admin-hooks";
import type { SignInRewardMilestone, SignInRewardPolicy, SignInRewardRule } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  EXPRESSION_PRESETS,
  Field,
  NumInput,
  Panel,
  VARIABLES,
  clonePolicy,
  describeEffect,
  formatNumber,
  newMilestone,
  newRule
} from "./signin-shared";

const TIMEZONES = [
  "Asia/Shanghai",
  "Asia/Hong_Kong",
  "Asia/Taipei",
  "Asia/Singapore",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Europe/London",
  "Europe/Berlin",
  "America/New_York",
  "America/Los_Angeles",
  "UTC"
];

const TEMPLATE_META: Record<string, { title: string; summary: string }> = {
  balanced: { title: "均衡", summary: "系统默认行为，连签阶梯加周末与月初奖励，适合作为基线" },
  growth: { title: "增长", summary: "提高基础奖励与首签加成，工作日连签额外加成，拉动新用户与周内活跃" },
  retention: { title: "留存", summary: "降低基础奖励，加大连签步进与长周期里程碑礼包，鼓励长期坚持" }
};

type Updater = (update: (draft: SignInRewardPolicy) => SignInRewardPolicy) => void;

export function SignInPolicyEditor({
  draft,
  update,
  onRequestReset
}: {
  draft: SignInRewardPolicy;
  update: Updater;
  onRequestReset: () => void;
}) {
  const patch = <K extends keyof SignInRewardPolicy>(key: K, value: SignInRewardPolicy[K]) =>
    update((current) => ({ ...current, [key]: value }));

  return (
    <div className="space-y-4">
      <Panel title="基本设置" icon={<Settings2 className="size-4" />} description="关闭后用户仍可签到并累计连签，但不再发放积分与经验。">
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <div className="space-y-3">
            <SwitchRow
              label="发放签到奖励"
              hint={draft.enabled ? "签到时按本策略计算并发放积分与经验" : "已关闭，签到只记录不发奖"}
              icon={<Gift className="size-4" />}
              checked={draft.enabled}
              onChange={(value) => patch("enabled", value)}
            />
            <Field label="计日时区" hint="决定「今天」从几点开始，也决定周末、月初等规则按哪里的日期判断。">
              <Select value={draft.timezone} onValueChange={(value) => patch("timezone", value)}>
                <SelectTrigger className="h-9 w-full text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(TIMEZONES.includes(draft.timezone) ? TIMEZONES : [draft.timezone, ...TIMEZONES]).map((tz) => (
                    <SelectItem key={tz} value={tz}>{tz}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>
          <div className="space-y-3">
            <Field label="策略名称">
              <Input className="h-9 text-sm" value={draft.name} maxLength={64} onChange={(e) => patch("name", e.target.value)} />
            </Field>
            <Field label="策略说明">
              <Textarea className="min-h-[76px] text-sm" value={draft.description ?? ""} maxLength={500} onChange={(e) => patch("description", e.target.value)} placeholder="面向运营同事的备注，不展示给用户" />
            </Field>
          </div>
        </div>
      </Panel>

      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="基础奖励" icon={<Gift className="size-4" />} description="每次签到的起点，规则与里程碑在此之上叠加。">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="基础积分"><NumInput value={draft.baseIntegral} min={0} max={1_000_000} onChange={(v) => patch("baseIntegral", v)} suffix="积分" /></Field>
            <Field label="基础经验"><NumInput value={draft.baseExperience} min={0} max={1_000_000} onChange={(v) => patch("baseExperience", v)} suffix="经验" /></Field>
            <Field label="首签经验加成" hint="用户第一次签到额外获得" className="sm:col-span-2"><NumInput value={draft.firstSignInExperienceBonus} min={0} max={1_000_000} onChange={(v) => patch("firstSignInExperienceBonus", v)} suffix="经验" /></Field>
            <Field label="单次积分上限" hint="0 表示不限"><NumInput value={draft.maxIntegralReward} min={0} max={1_000_000} onChange={(v) => patch("maxIntegralReward", v)} suffix="积分" /></Field>
            <Field label="单次经验上限" hint="0 表示不限"><NumInput value={draft.maxExperienceReward} min={0} max={1_000_000} onChange={(v) => patch("maxExperienceReward", v)} suffix="经验" /></Field>
          </div>
        </Panel>

        <ExperienceGrowth draft={draft} patch={patch} />
      </div>

      <RulesSection draft={draft} update={update} />
      <MilestonesSection draft={draft} update={update} />
      <TemplatesSection onLoad={(policy) => update(() => ({ ...clonePolicy(policy), isDefault: false }))} />

      <section className="flex flex-col gap-3 rounded-2xl border border-dashed p-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-0.5">
          <h3 className="text-sm font-semibold">恢复系统默认</h3>
          <p className="text-xs text-muted-foreground">删除本应用的自定义策略，立即改用系统默认策略。此操作直接生效，无需保存。</p>
        </div>
        <Button variant="outline" size="sm" className="h-8 gap-1.5 text-destructive hover:text-destructive" onClick={onRequestReset}>
          <RotateCcw className="size-3.5" />
          恢复默认
        </Button>
      </section>
    </div>
  );
}

// ── 连签经验成长 ──────────────────────────

function ExperienceGrowth({
  draft,
  patch
}: {
  draft: SignInRewardPolicy;
  patch: <K extends keyof SignInRewardPolicy>(key: K, value: SignInRewardPolicy[K]) => void;
}) {
  const step = draft.consecutiveExperienceStep;
  const cap = draft.consecutiveExperienceStepCap;
  const extra = (day: number) => {
    const bonus = (day - 1) * step;
    return cap > 0 ? Math.min(bonus, cap) : bonus;
  };
  const capDay = step > 0 && cap > 0 ? Math.ceil(cap / step) + 1 : null;
  const bars = Array.from({ length: 30 }, (_, i) => extra(i + 1));
  const top = Math.max(1, ...bars);

  return (
    <Panel title="连签经验成长" icon={<TrendingUp className="size-4" />} description="连签越久，每天的基础经验越多，直到封顶。">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="每天递增"><NumInput value={step} min={0} max={100_000} onChange={(v) => patch("consecutiveExperienceStep", v)} suffix="经验" /></Field>
        <Field label="递增封顶" hint="0 表示不封顶"><NumInput value={cap} min={0} max={1_000_000} onChange={(v) => patch("consecutiveExperienceStepCap", v)} suffix="经验" /></Field>
      </div>
      <div className="mt-4 rounded-xl border bg-muted/30 p-3">
        <div className="mb-2 flex items-baseline justify-between gap-2 text-[11px] text-muted-foreground">
          <span>连签 1 到 30 天的额外经验</span>
          <span className="tabular-nums">{capDay ? `第 ${capDay} 天起封顶 +${formatNumber(cap)}` : step > 0 ? "不封顶" : "未启用递增"}</span>
        </div>
        <div className="flex h-16 items-end gap-[3px]" role="img" aria-label="连签额外经验走势">
          {bars.map((value, i) => (
            <div
              key={i}
              title={`第 ${i + 1} 天 +${value}`}
              className={cn("flex-1 rounded-t-sm", capDay && i + 1 >= capDay ? "bg-sky-500/50" : "bg-sky-500")}
              style={{ height: `${Math.max(4, (value / top) * 100)}%` }}
            />
          ))}
        </div>
        <div className="mt-1 flex justify-between text-[10px] text-muted-foreground tabular-nums"><span>1</span><span>10</span><span>20</span><span>30 天</span></div>
      </div>
      <div className="mt-3">
        <SwitchRow
          label="叠加等级经验倍率"
          hint="经验结算后再乘以用户当前等级对应的经验倍率"
          icon={<Layers className="size-4" />}
          checked={draft.applyLevelExperienceMultiplier}
          onChange={(value) => patch("applyLevelExperienceMultiplier", value)}
        />
      </div>
    </Panel>
  );
}

// ── 规则 ──────────────────────────

function RulesSection({ draft, update }: { draft: SignInRewardPolicy; update: Updater }) {
  // 展开状态按规则在草稿里的下标记：改规则标识、改优先级（列表重排）都不应把正在编辑的卡片收起
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const sorted = useMemo(
    () => draft.rules.map((rule, index) => ({ rule, index })).sort((a, b) => a.rule.priority - b.rule.priority || a.index - b.index),
    [draft.rules]
  );
  const groups = useMemo(() => {
    const map = new Map<string, number>();
    for (const rule of draft.rules) if (rule.group) map.set(rule.group, (map.get(rule.group) ?? 0) + 1);
    return map;
  }, [draft.rules]);

  const patchRule = (index: number, value: Partial<SignInRewardRule>) =>
    update((current) => ({ ...current, rules: current.rules.map((rule, i) => (i === index ? { ...rule, ...value } : rule)) }));

  function toggle(key: number) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function addRule() {
    const rule = newRule(draft.rules);
    update((current) => ({ ...current, rules: [...current.rules, rule] }));
    setExpanded((prev) => new Set(prev).add(draft.rules.length));
  }

  function duplicate(index: number) {
    const source = draft.rules[index];
    // 副本默认停用，免得保存后和原规则同时生效、奖励翻倍
    const copy: SignInRewardRule = { ...source, key: newRule(draft.rules).key, name: `${source.name} 副本`, priority: source.priority + 1, enabled: false };
    update((current) => ({ ...current, rules: [...current.rules, copy] }));
    setExpanded((prev) => new Set(prev).add(draft.rules.length));
  }

  return (
    <Panel
      title="加成规则"
      icon={<Zap className="size-4" />}
      description="满足条件时叠加倍率或固定加成。同一分组内按优先级从小到大只命中第一条，适合「连签阶梯」这类互斥档位；未分组的规则各自独立叠加。"
      action={
        <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={addRule}>
          <Plus className="size-3.5" />
          新增规则
        </Button>
      }
    >
      {sorted.length === 0 ? (
        <div className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">暂无规则，签到只发放基础奖励与里程碑奖励</div>
      ) : (
        <ul className="space-y-2">
          {sorted.map(({ rule, index }) => (
            <RuleCard
              key={index}
              rule={rule}
              open={expanded.has(index)}
              groupSize={rule.group ? groups.get(rule.group) ?? 0 : 0}
              onToggle={() => toggle(index)}
              onPatch={(value) => patchRule(index, value)}
              onDuplicate={() => duplicate(index)}
              onRemove={() => {
                update((current) => ({ ...current, rules: current.rules.filter((_, i) => i !== index) }));
                // 删除后后面的下标整体前移，展开状态跟着平移
                setExpanded((prev) => new Set([...prev].filter((i) => i !== index).map((i) => (i > index ? i - 1 : i))));
              }}
            />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function RuleCard({
  rule,
  open,
  groupSize,
  onToggle,
  onPatch,
  onDuplicate,
  onRemove
}: {
  rule: SignInRewardRule;
  open: boolean;
  groupSize: number;
  onToggle: () => void;
  onPatch: (value: Partial<SignInRewardRule>) => void;
  onDuplicate: () => void;
  onRemove: () => void;
}) {
  return (
    <li className={cn("overflow-hidden rounded-xl border transition-colors", !rule.enabled && "bg-muted/30", open && "border-foreground/20")}>
      <div className="flex items-start gap-3 p-3">
        <Switch className="mt-0.5" checked={rule.enabled} onCheckedChange={(value) => onPatch({ enabled: value })} aria-label={rule.enabled ? "停用规则" : "启用规则"} />
        <button type="button" className="min-w-0 flex-1 text-left" onClick={onToggle} aria-expanded={open}>
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={cn("text-sm font-medium", !rule.enabled && "text-muted-foreground")}>{rule.name || rule.key}</span>
            {rule.group ? (
              <Badge variant="outline" size="sm" className="gap-1" title={`${groupSize} 条规则互斥`}>
                <Layers className="size-3" />
                {rule.group}
              </Badge>
            ) : null}
            <span className="rounded bg-muted px-1.5 text-[10px] leading-4 text-muted-foreground tabular-nums">优先级 {rule.priority}</span>
          </div>
          <code className="mt-1 block truncate font-mono text-[11px] text-muted-foreground [font-variant-ligatures:none]">{rule.expression || "未设置条件"}</code>
          <div className={cn("mt-1 text-xs", rule.enabled ? "text-emerald-700 dark:text-emerald-300" : "text-muted-foreground")}>{describeEffect(rule)}</div>
        </button>
        <div className="flex shrink-0 items-center">
          <Button variant="ghost" size="icon" className="hidden size-8 sm:inline-flex" title="复制规则" onClick={onDuplicate}><Copy className="size-3.5" /></Button>
          <Button variant="ghost" size="icon" className="size-8 text-muted-foreground hover:text-destructive" title="删除规则" onClick={onRemove}><Trash2 className="size-3.5" /></Button>
          <Button variant="ghost" size="icon" className="size-8" title={open ? "收起" : "展开编辑"} onClick={onToggle}>
            <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
          </Button>
        </div>
      </div>

      {open ? (
        <div className="space-y-4 border-t bg-muted/20 p-3 sm:p-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Field label="规则名称"><Input className="h-9 text-sm" value={rule.name} onChange={(e) => onPatch({ name: e.target.value })} /></Field>
            <Field label="规则标识" hint="唯一，写入签到记录"><Input className="h-9 font-mono text-sm" value={rule.key} onChange={(e) => onPatch({ key: e.target.value.trim() })} /></Field>
            <Field label="互斥分组" hint="留空表示独立叠加"><Input className="h-9 text-sm" value={rule.group ?? ""} onChange={(e) => onPatch({ group: e.target.value })} placeholder="如 streak-tier" /></Field>
            <Field label="优先级" hint="数字越小越先判断"><NumInput value={rule.priority} min={1} max={100_000} onChange={(v) => onPatch({ priority: v })} /></Field>
          </div>

          <Field label="触发条件" hint="表达式需返回真或假，可用 && 与 || 组合多个条件。">
            <Textarea
              className="min-h-20 font-mono text-xs [font-variant-ligatures:none]"
              spellCheck={false}
              value={rule.expression}
              onChange={(e) => onPatch({ expression: e.target.value })}
              placeholder="consecutive_days >= 7 && is_weekend"
            />
            <div className="flex flex-wrap items-center gap-1.5 pt-1">
              {EXPRESSION_PRESETS.map((preset) => (
                <button
                  key={preset.expression}
                  type="button"
                  className="rounded-full border bg-background px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
                  onClick={() => onPatch({ expression: preset.expression })}
                  title={preset.expression}
                >
                  {preset.label}
                </button>
              ))}
              <VariableReference onInsert={(name) => onPatch({ expression: rule.expression ? `${rule.expression} && ${name}` : name })} />
            </div>
          </Field>

          <div>
            <div className="mb-2 text-xs font-medium">命中后的效果</div>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Field label="积分倍率增量" hint="0.5 即总倍率 +50%"><NumInput value={rule.integralMultiplierDelta} min={-1} max={20} step={0.1} onChange={(v) => onPatch({ integralMultiplierDelta: v })} /></Field>
              <Field label="积分固定加成" hint="计入倍率前的底数"><NumInput value={rule.integralBonus} min={-1_000_000} max={1_000_000} onChange={(v) => onPatch({ integralBonus: v })} suffix="积分" /></Field>
              <Field label="经验倍率增量"><NumInput value={rule.experienceMultiplierDelta} min={-1} max={20} step={0.1} onChange={(v) => onPatch({ experienceMultiplierDelta: v })} /></Field>
              <Field label="经验固定加成"><NumInput value={rule.experienceBonus} min={-1_000_000} max={1_000_000} onChange={(v) => onPatch({ experienceBonus: v })} suffix="经验" /></Field>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="奖励类型" hint="写入签到记录，用于统计与筛选"><Input className="h-9 font-mono text-sm" value={rule.bonusType ?? ""} onChange={(e) => onPatch({ bonusType: e.target.value.trim() })} /></Field>
            <Field label="奖励说明" hint="返回给客户端展示，如「周末双倍」"><Input className="h-9 text-sm" value={rule.bonusDescription ?? ""} onChange={(e) => onPatch({ bonusDescription: e.target.value })} /></Field>
          </div>
          <Field label="内部备注"><Textarea className="min-h-16 text-sm" value={rule.description ?? ""} onChange={(e) => onPatch({ description: e.target.value })} /></Field>
          <Button variant="outline" size="sm" className="h-8 gap-1.5 sm:hidden" onClick={onDuplicate}><Copy className="size-3.5" />复制规则</Button>
        </div>
      ) : null}
    </li>
  );
}

function VariableReference({ onInsert }: { onInsert: (name: string) => void }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="inline-flex items-center gap-1 rounded-full border border-dashed px-2 py-0.5 text-[11px] text-muted-foreground hover:text-foreground">
          <BookOpen className="size-3" />
          可用变量
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(92vw,26rem)] p-0">
        <div className="border-b px-3 py-2 text-xs font-medium">可用变量（点击追加到条件）</div>
        <ul className="max-h-72 overflow-y-auto py-1">
          {VARIABLES.map((variable) => (
            <li key={variable.name}>
              <button type="button" className="flex w-full items-start gap-3 px-3 py-1.5 text-left hover:bg-muted" onClick={() => onInsert(variable.example)}>
                <code className="w-32 shrink-0 font-mono text-[11px]">{variable.name}</code>
                <span className="min-w-0 flex-1 text-[11px]">
                  <span>{variable.label}</span>
                  <span className="ml-1 text-muted-foreground">{variable.type}</span>
                  <code className="block truncate font-mono text-[10px] text-muted-foreground">{variable.example}</code>
                </span>
              </button>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

// ── 里程碑 ──────────────────────────

function MilestonesSection({ draft, update }: { draft: SignInRewardPolicy; update: Updater }) {
  const sorted = useMemo(
    () => draft.milestones.map((m, index) => ({ m, index })).sort((a, b) => a.m.consecutiveDays - b.m.consecutiveDays || a.index - b.index),
    [draft.milestones]
  );
  const patchMilestone = (index: number, value: Partial<SignInRewardMilestone>) =>
    update((current) => ({ ...current, milestones: current.milestones.map((m, i) => (i === index ? { ...m, ...value } : m)) }));
  const duplicates = new Set(
    draft.milestones.map((m) => m.consecutiveDays).filter((day, i, all) => all.indexOf(day) !== i)
  );

  return (
    <Panel
      title="连签里程碑"
      icon={<Flag className="size-4" />}
      description="连签恰好达到某个天数的那一天发放一次性奖励，断签后重新累计可再次获得。"
      action={
        <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => update((current) => ({ ...current, milestones: [...current.milestones, newMilestone(current.milestones)] }))}>
          <Plus className="size-3.5" />
          新增里程碑
        </Button>
      }
    >
      {sorted.length === 0 ? (
        <div className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">暂无里程碑</div>
      ) : (
        <>
          {/* 时间轴 */}
          <div className="-mx-1 mb-4 overflow-x-auto px-1 pb-1">
            <ol className="relative flex min-w-max items-start gap-0 pt-1">
              {sorted.map(({ m }, i) => (
                <li key={`${m.consecutiveDays}-${i}`} className="relative flex w-28 flex-col items-center text-center">
                  {i > 0 ? <span className="absolute top-3 right-1/2 h-px w-28 bg-border" /> : null}
                  <span className="relative z-10 grid size-6 place-items-center rounded-full bg-orange-500 text-[10px] font-semibold text-white ring-4 ring-card">
                    <Flag className="size-3" />
                  </span>
                  <span className="mt-1.5 text-sm font-semibold tabular-nums">{m.consecutiveDays} 天</span>
                  <span className="text-[11px] text-muted-foreground tabular-nums">
                    {[m.integralBonus ? `+${formatNumber(m.integralBonus)} 积分` : "", m.experienceBonus ? `+${formatNumber(m.experienceBonus)} 经验` : ""].filter(Boolean).join("，") || "无奖励"}
                  </span>
                </li>
              ))}
            </ol>
          </div>

          <ul className="space-y-2">
            {sorted.map(({ m, index }) => (
              <li key={index} className={cn("rounded-xl border p-3", duplicates.has(m.consecutiveDays) && "border-red-300 dark:border-red-900")}>
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-[6.5rem_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
                  <Field label="连签天数"><NumInput value={m.consecutiveDays} min={1} max={10_000} onChange={(v) => patchMilestone(index, { consecutiveDays: v })} suffix="天" /></Field>
                  <Field label="积分加成"><NumInput value={m.integralBonus} min={-1_000_000} max={1_000_000} onChange={(v) => patchMilestone(index, { integralBonus: v })} /></Field>
                  <Field label="经验加成"><NumInput value={m.experienceBonus} min={-1_000_000} max={1_000_000} onChange={(v) => patchMilestone(index, { experienceBonus: v })} /></Field>
                  <Field label="说明" className="col-span-2 sm:col-span-1"><Input className="h-9 text-sm" value={m.description ?? ""} onChange={(e) => patchMilestone(index, { description: e.target.value })} /></Field>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-9 self-end justify-self-end text-muted-foreground hover:text-destructive max-sm:col-span-2 max-sm:w-full"
                    title="删除里程碑"
                    onClick={() => update((current) => ({ ...current, milestones: current.milestones.filter((_, i) => i !== index) }))}
                  >
                    <Trash2 className="size-3.5" />
                    <span className="sm:hidden">删除</span>
                  </Button>
                </div>
                {duplicates.has(m.consecutiveDays) ? <p className="mt-2 text-[11px] text-red-600 dark:text-red-400">与另一个里程碑天数重复</p> : null}
              </li>
            ))}
          </ul>
          <p className="mt-3 flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <ArrowDownUp className="size-3" />
            里程碑的积分加成会再乘以当天的积分倍率，经验加成同理。
          </p>
        </>
      )}
    </Panel>
  );
}

// ── 模板 ──────────────────────────

function TemplatesSection({ onLoad }: { onLoad: (policy: SignInRewardPolicy) => void }) {
  const query = useSignInRewardTemplatesQuery();
  const templates = query.data?.templates ?? {};
  const keys = Object.keys(TEMPLATE_META).filter((key) => templates[key]);

  return (
    <Panel title="策略模板" icon={<LayoutTemplate className="size-4" />} description="载入后替换当前草稿的全部内容，检查无误再保存。">
      {query.isLoading ? (
        <div className="grid gap-3 md:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-40 rounded-xl" />)}
        </div>
      ) : query.isError ? (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">
          模板加载失败
          <Button size="sm" variant="outline" className="h-8" onClick={() => void query.refetch()}>重试</Button>
        </div>
      ) : (
        <div className="grid gap-3 md:grid-cols-3">
          {keys.map((key) => {
            const policy = templates[key];
            return (
              <div key={key} className="flex flex-col rounded-xl border p-4">
                <div className="flex items-center gap-2">
                  <span className="grid size-7 place-items-center rounded-lg bg-primary/10 text-primary"><Sparkles className="size-3.5" /></span>
                  <span className="text-sm font-semibold">{TEMPLATE_META[key].title}</span>
                  <span className="font-mono text-[10px] text-muted-foreground">{key}</span>
                </div>
                <p className="mt-2 flex-1 text-xs leading-5 text-muted-foreground">{TEMPLATE_META[key].summary}</p>
                <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-[11px]">
                  <dt className="text-muted-foreground">基础积分</dt><dd className="text-right tabular-nums">{policy.baseIntegral}</dd>
                  <dt className="text-muted-foreground">基础经验</dt><dd className="text-right tabular-nums">{policy.baseExperience}</dd>
                  <dt className="text-muted-foreground">首签加成</dt><dd className="text-right tabular-nums">{policy.firstSignInExperienceBonus}</dd>
                  <dt className="text-muted-foreground">规则与里程碑</dt><dd className="text-right tabular-nums">{policy.rules.length} 条，{policy.milestones.length} 个</dd>
                </dl>
                <Button size="sm" variant="outline" className="mt-3 h-8" onClick={() => onLoad(policy)}>载入到草稿</Button>
              </div>
            );
          })}
        </div>
      )}
    </Panel>
  );
}
