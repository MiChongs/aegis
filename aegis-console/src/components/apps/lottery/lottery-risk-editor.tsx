"use client";

import { ArrowRight, ChevronDown, ChevronUp, Plus, ShieldCheck, Trash2, Wand2 } from "lucide-react";
import { Segmented } from "@/components/apps/lottery/editor/editor-fields";
import { kindLabelOf } from "@/components/apps/lottery/lottery-shared";
import {
  RISK_OPS,
  RiskActionChip,
  riskActionDetail,
  riskActions,
  riskActionVisual,
  riskConditionText,
  riskSignalOf,
  riskSignals
} from "@/components/apps/lottery/lottery-risk-shared";
import { riskRuleDraft, type RiskRuleDraft } from "@/components/apps/lottery/lottery-strategy-form";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { LotteryCatalog, LotteryRiskAction, LotteryRiskRule } from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/** 推荐组合：从轻到重各一条，覆盖批量号、脚本与新号三类常见刷法 */
const RISK_PRESET: LotteryRiskRule[] = [
  { name: "同设备多账号", signal: "deviceAccounts", op: "gt", threshold: 3, action: "floor" },
  { name: "同 IP 多账号", signal: "ipAccounts", op: "gt", threshold: 5, action: "damp", factor: 0.5 },
  { name: "同网段批量号", signal: "subnetAccounts", op: "gt", threshold: 12, action: "rare", rareBelow: 0.01 },
  { name: "新注册账号", signal: "accountAgeHours", op: "lt", threshold: 24, action: "noPity" },
  { name: "高频脚本", signal: "burst10m", op: "gt", threshold: 30, action: "deny" },
  { name: "缺少设备标识", signal: "noDevice", op: "gte", threshold: 1, action: "deny" }
];

/** 单条模板（添加菜单里列出） */
const RISK_TEMPLATES: LotteryRiskRule[] = [
  ...RISK_PRESET,
  { name: "设备换号", signal: "deviceHistory", op: "gt", threshold: 5, action: "floor" },
  { name: "高风险账号", signal: "riskScore", op: "gte", threshold: 60, action: "floor" },
  { name: "今日中奖过多", signal: "winsToday", op: "gt", threshold: 50, action: "damp", factor: 0.3 }
];

/**
 * 风控规则编辑：每条规则读作一句话「当〈信号〉〈比较〉〈阈值〉时 → 〈处置〉」。
 * 只改草稿；载荷与校验在 lottery-strategy-form.ts（与后端 lottery/risk.go 同一口径）。
 */
export function RiskRulesEditor({
  rules,
  onChange,
  catalog,
  max
}: {
  rules: RiskRuleDraft[];
  onChange: (rules: RiskRuleDraft[]) => void;
  catalog?: LotteryCatalog | null;
  max: number;
}) {
  const nextKey = () => rules.reduce((value, rule) => Math.max(value, rule.key), 0) + 1;
  const add = (templates: LotteryRiskRule[]) => {
    let key = nextKey();
    const room = Math.max(0, max - rules.length);
    onChange([...rules, ...templates.slice(0, room).map((rule) => riskRuleDraft(rule, key++))]);
  };
  const blank = () => {
    const spec = riskSignals(catalog).find((item) => !item.hidden) ?? riskSignalOf("deviceAccounts", catalog);
    add([{ signal: spec.key, op: spec.op, threshold: spec.threshold, action: "floor" }]);
  };
  const update = (key: number, next: Partial<RiskRuleDraft>) =>
    onChange(rules.map((rule) => (rule.key === key ? { ...rule, ...next } : rule)));
  const move = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= rules.length) return;
    const next = [...rules];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next);
  };

  const active = rules.filter((rule) => !rule.disabled);
  const byAction = riskActions(catalog)
    .map((spec) => ({ spec, count: active.filter((rule) => rule.action === spec.key).length }))
    .filter((item) => item.count > 0);

  return (
    <div className="space-y-2.5 rounded-xl border border-border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-orange-500/12 text-orange-600 dark:text-orange-400">
            <ShieldCheck className="size-3.5" />
          </span>
          <div className="min-w-0">
            <p className="text-xs font-medium">
              风控规则
              <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">
                {rules.length}/{max}
              </span>
            </p>
            <p className="truncate text-[10px] text-muted-foreground">命中多条时各自生效，记录上按最重的一条归类</p>
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          {rules.length === 0 ? (
            <Button size="sm" variant="secondary" className="h-7" onClick={() => add(RISK_PRESET)}>
              <Wand2 className="size-3.5" /> 推荐组合
            </Button>
          ) : null}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="outline" className="h-7" disabled={rules.length >= max}>
                <Plus className="size-3.5" /> 添加
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-72">
              <DropdownMenuItem onSelect={blank}>空白规则</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuLabel className="text-[10px] text-muted-foreground">常用</DropdownMenuLabel>
              {RISK_TEMPLATES.map((template) => (
                <DropdownMenuItem key={template.name} onSelect={() => add([template])} className="gap-2">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-xs">{template.name}</span>
                    <span className="block truncate text-[10px] text-muted-foreground">{riskConditionText(template, catalog)}</span>
                  </span>
                  <RiskActionChip action={template.action} catalog={catalog} />
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {byAction.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {byAction.map(({ spec, count }) => (
            <RiskActionChip key={spec.key} action={spec.key} catalog={catalog} suffix={<span className="font-mono">{count}</span>} />
          ))}
        </div>
      ) : null}

      {rules.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-3 py-5 text-center text-[11px] text-muted-foreground">
          还没有规则。「推荐组合」一次加入同设备、同 IP、同网段、新号、高频与缺设备号六条
        </p>
      ) : (
        <div className="space-y-2">
          {rules.map((rule, index) => (
            <RiskRuleCard
              key={rule.key}
              index={index}
              total={rules.length}
              rule={rule}
              catalog={catalog}
              onChange={(next) => update(rule.key, next)}
              onMove={(delta) => move(index, delta)}
              onRemove={() => onChange(rules.filter((item) => item.key !== rule.key))}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function RiskRuleCard({
  index,
  total,
  rule,
  catalog,
  onChange,
  onMove,
  onRemove
}: {
  index: number;
  total: number;
  rule: RiskRuleDraft;
  catalog?: LotteryCatalog | null;
  onChange: (next: Partial<RiskRuleDraft>) => void;
  onMove: (delta: number) => void;
  onRemove: () => void;
}) {
  const spec = riskSignalOf(rule.signal, catalog);
  const signals = riskSignals(catalog).filter((item) => !item.hidden);
  const groups = [...new Set(signals.map((item) => item.group))];
  const actions = riskActions(catalog);
  const visual = riskActionVisual(rule.action);
  const kinds = (catalog?.kinds ?? []).filter((item) => item.type !== "none");
  const scoped = rule.action === "damp" || rule.action === "rare";
  const threshold = Number(rule.threshold);
  const preview = {
    signal: rule.signal,
    op: rule.op,
    threshold: Number.isFinite(threshold) ? threshold : 0,
    action: rule.action,
    factor: Number(rule.factor),
    rareBelow: Number(rule.rareBelow) / 100
  };

  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-xl border border-border bg-card/40 pl-3.5 transition-opacity",
        rule.disabled && "opacity-55"
      )}
    >
      {/* 左侧色条：这条规则的处置 */}
      <span className={cn("absolute inset-y-0 left-0 w-1", visual.solid)} aria-hidden />
      <div className="space-y-2.5 p-3 pl-1.5">
        <div className="flex items-center gap-2">
          <div className="flex shrink-0 flex-col">
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground disabled:opacity-30"
              onClick={() => onMove(-1)}
              disabled={index === 0}
              aria-label="上移"
            >
              <ChevronUp className="size-3" />
            </button>
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground disabled:opacity-30"
              onClick={() => onMove(1)}
              disabled={index === total - 1}
              aria-label="下移"
            >
              <ChevronDown className="size-3" />
            </button>
          </div>
          <span className="w-4 shrink-0 font-mono text-[11px] text-muted-foreground">{index + 1}</span>
          <Input
            value={rule.name}
            maxLength={32}
            placeholder={spec.label}
            onChange={(event) => onChange({ name: event.target.value })}
            className="h-8 text-xs"
          />
          <Switch checked={!rule.disabled} onCheckedChange={(checked) => onChange({ disabled: !checked })} aria-label="启用" />
          <Button size="icon" variant="ghost" className="size-8 shrink-0" onClick={onRemove} aria-label="删除规则">
            <Trash2 className="size-3.5" />
          </Button>
        </div>

        <div className="grid gap-2 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] lg:items-start">
          {/* 条件 */}
          <div className="space-y-1.5 rounded-lg bg-muted/50 p-2 dark:bg-input/20">
            <span className="text-[10px] font-medium text-muted-foreground">当</span>
            <Select
              value={rule.signal}
              onValueChange={(next) => {
                const nextSpec = riskSignalOf(next, catalog);
                onChange({ signal: next, op: nextSpec.op, threshold: String(nextSpec.threshold) });
              }}
            >
              <SelectTrigger className="h-8 w-full text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {groups.map((group) => (
                  <SelectGroup key={group}>
                    <SelectLabel className="text-[10px]">{group}</SelectLabel>
                    {signals
                      .filter((item) => item.group === group)
                      .map((item) => (
                        <SelectItem key={item.key} value={item.key} className="text-xs">
                          {item.label}
                        </SelectItem>
                      ))}
                  </SelectGroup>
                ))}
              </SelectContent>
            </Select>
            {spec.bool ? (
              <p className="text-[10px] text-muted-foreground">是或否的信号，成立即命中</p>
            ) : (
              <div className="flex items-center gap-1.5">
                <Segmented
                  size="sm"
                  value={rule.op}
                  options={RISK_OPS.map((item) => ({ value: item.value, label: item.label }))}
                  onChange={(next) => onChange({ op: next })}
                />
                <div className="relative min-w-0 flex-1">
                  <Input
                    type="number"
                    min={0}
                    value={rule.threshold}
                    onChange={(event) => onChange({ threshold: event.target.value })}
                    className={cn("h-7 font-mono text-xs tabular-nums", spec.unit && "pr-9")}
                    aria-label="阈值"
                  />
                  {spec.unit ? (
                    <span className="pointer-events-none absolute inset-y-0 right-2 flex items-center text-[10px] text-muted-foreground">
                      {spec.unit}
                    </span>
                  ) : null}
                </div>
              </div>
            )}
            <p className="text-[10px] leading-relaxed text-muted-foreground">{spec.hint}</p>
          </div>

          <ArrowRight className="mx-auto hidden size-4 self-center text-muted-foreground lg:block" />

          {/* 处置 */}
          <div className="space-y-1.5 rounded-lg bg-muted/50 p-2 dark:bg-input/20">
            <span className="text-[10px] font-medium text-muted-foreground">处置</span>
            <div className="grid grid-cols-5 gap-1">
              {actions.map((action) => {
                const selected = action.key === rule.action;
                const tone = riskActionVisual(action.key);
                const Icon = tone.icon;
                return (
                  <button
                    key={action.key}
                    type="button"
                    title={action.hint}
                    onClick={() => onChange({ action: action.key as LotteryRiskAction })}
                    className={cn(
                      "flex flex-col items-center gap-0.5 rounded-md px-1 py-1.5 text-[10px] font-medium ring-1 ring-inset transition-colors",
                      selected ? tone.chip : "text-muted-foreground ring-transparent hover:bg-background hover:text-foreground"
                    )}
                  >
                    <Icon className="size-3.5" />
                    <span className="whitespace-nowrap">{action.label}</span>
                  </button>
                );
              })}
            </div>
            {rule.action === "damp" ? (
              <div className="flex items-center gap-1.5">
                <span className="text-[11px] text-muted-foreground">权重 ×</span>
                <Input
                  type="number"
                  min={0}
                  max={1}
                  step={0.05}
                  value={rule.factor}
                  onChange={(event) => onChange({ factor: event.target.value })}
                  className="h-7 w-20 font-mono text-xs tabular-nums"
                  aria-label="降权系数"
                />
                <span className="text-[10px] text-muted-foreground">0–1，不含两端</span>
              </div>
            ) : null}
            {rule.action === "rare" ? (
              <div className="flex items-center gap-1.5">
                <span className="text-[11px] text-muted-foreground">公示概率低于</span>
                <Input
                  type="number"
                  min={0}
                  max={100}
                  step={0.1}
                  value={rule.rareBelow}
                  onChange={(event) => onChange({ rareBelow: event.target.value })}
                  className="h-7 w-20 font-mono text-xs tabular-nums"
                  aria-label="稀有概率线"
                />
                <span className="text-[11px] text-muted-foreground">% 的不出</span>
              </div>
            ) : null}
            {!scoped ? (
              <p className="text-[10px] leading-relaxed text-muted-foreground">
                {actions.find((action) => action.key === rule.action)?.hint}
              </p>
            ) : null}
          </div>
        </div>

        {scoped && kinds.length ? (
          <div className="space-y-1">
            <span className="text-[10px] font-medium text-muted-foreground">只作用于（不选为全部中奖奖品，兜底奖始终不受影响）</span>
            <ToggleGroup
              type="multiple"
              variant="outline"
              size="sm"
              value={rule.kinds}
              onValueChange={(next) => onChange({ kinds: next })}
              className="flex-wrap justify-start"
            >
              {kinds.map((item) => (
                <ToggleGroupItem key={item.type} value={item.type} className="h-6 px-2 text-[11px]">
                  {kindLabelOf(item.type, catalog?.kinds ?? [])}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        ) : null}

        <p className="flex flex-wrap items-center gap-1.5 text-[11px]">
          <span className="text-muted-foreground">读作</span>
          <span className="font-medium">{riskConditionText(preview, catalog)}</span>
          <span className="text-muted-foreground">时</span>
          <RiskActionChip action={rule.action} catalog={catalog} suffix={riskActionDetail(preview) ? ` ${riskActionDetail(preview)}` : null} />
        </p>
      </div>
    </div>
  );
}
