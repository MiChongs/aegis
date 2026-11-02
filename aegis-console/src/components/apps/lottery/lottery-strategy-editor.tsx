"use client";

import { useMemo } from "react";
import {
  Activity,
  Braces,
  Dices,
  Scale,
  ShieldCheck,
  Target,
  Fingerprint,
  Gauge,
  Hourglass,
  Plus,
  Sparkles,
  Timer,
  Trash2,
  Wallet,
  type LucideIcon
} from "lucide-react";
import { FieldGroup, NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import { STRATEGY_LIMITS, type RuleDraft, type StrategyDraft } from "@/components/apps/lottery/lottery-strategy-form";
import { kindLabelOf } from "@/components/apps/lottery/lottery-shared";
import { RiskRulesEditor } from "@/components/apps/lottery/lottery-risk-editor";
import { SupremeFields, SupremeModePicker, type SupremeTierOption } from "@/components/apps/lottery/lottery-supreme-editor";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { LotteryCatalog, LotterySignalSpec } from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/** 常用规则：选中后填进一条新规则，名字、条件、作用对象、系数都可以再改 */
const RULE_TEMPLATES: Array<Omit<RuleDraft, "key" | "disabled">> = [
  { name: "新人首抽", when: "user.totalDraws == 0 && user.registeredDays < 7", kinds: [], positions: [], factor: "2" },
  { name: "连签一周", when: "user.signStreak >= 7", kinds: ["points"], positions: [], factor: "1.5" },
  { name: "高活跃", when: "user.onlineMinutes7d >= 300 && user.onlineDays30d >= 15", kinds: [], positions: [], factor: "1.3" },
  { name: "会员加成", when: "user.member && user.memberRate >= 0.5", kinds: [], positions: [], factor: "1.2" },
  { name: "邀请达人", when: "user.invites >= 5", kinds: [], positions: [], factor: "1.3" },
  { name: "长期未活跃回归", when: "user.recencyDays >= 30", kinds: [], positions: [], factor: "1.5" },
  { name: "今日已中两次", when: "user.winsToday >= 2", kinds: [], positions: [], factor: "0.5" },
  { name: "晚间时段", when: "draw.hour >= 20 && draw.hour < 23", kinds: [], positions: [], factor: "1.2" },
  { name: "活动收尾放量", when: "activity.progress >= 0.9 && prize.limited", kinds: [], positions: [], factor: "2" },
  { name: "高风险账号", when: "user.riskScore30d >= 60", kinds: [], positions: [], factor: "0" }
];

type Dimension = {
  key: "softPity" | "pacing" | "cooldown" | "budget" | "risk" | "behavior";
  label: string;
  hint: string;
  icon: LucideIcon;
};

const DIMENSIONS: Dimension[] = [
  { key: "behavior", label: "行为画像", hint: "按全部行为给中奖概率加减", icon: Fingerprint },
  { key: "softPity", label: "软保底", hint: "连续未中后逐次抬高中奖率", icon: Gauge },
  { key: "pacing", label: "库存匀速释放", hint: "限量奖品按活动进度放量", icon: Hourglass },
  { key: "cooldown", label: "中奖冷却", hint: "近期中过的奖品降低权重", icon: Timer },
  { key: "budget", label: "积分预算", hint: "当日积分过半后逐步收紧", icon: Wallet },
  { key: "risk", label: "风控规则", hint: "按设备、网络、账号与频率拦截刷奖", icon: Activity }
];

type Control = {
  key: "luck" | "fortune" | "prd" | "winRate" | "guard";
  label: string;
  hint: string;
  icon: LucideIcon;
};

/** 概率控制层：在各维度系数之后整体校正这一次的分布（docs/lottery.md「概率控制层」） */
const CONTROLS: Control[] = [
  { key: "luck", label: "运气平衡", hint: "按实际与应得中奖次数的偏差校正", icon: Scale },
  { key: "fortune", label: "每日运气", hint: "每人每天手气不同，平均不变", icon: Sparkles },
  { key: "prd", label: "伪随机分布", hint: "长期中奖率不变，减少连败与连中", icon: Dices },
  { key: "winRate", label: "全站中奖率目标", hint: "按今日全站中奖率闭环调节", icon: Target },
  { key: "guard", label: "护栏", hint: "中奖率区间与期望价值上限", icon: ShieldCheck }
];

/** σ → 最倒霉与最走运（±2σ）的人真实奖品权重的倍数，与后端 lottery/fortune.go 同一个式子 */
function fortuneHint(raw: string) {
  const spread = Number(raw);
  if (!Number.isFinite(spread) || spread <= 0) return "每人每天一个系数，乘在兜底奖以外的奖品上";
  const at = (z: number) => Math.exp(spread * z - (spread * spread) / 2);
  return `系数 ${at(-2).toFixed(2)}–${at(2).toFixed(2)}，最走运的人真实奖品的权重是最倒霉的 ${(at(2) / at(-2)).toFixed(1)} 倍`;
}

const MAP_HINT: Record<string, string> = {
  saturate: "越多越高",
  decay: "越久越低",
  bool: "是或否",
  ratio: "比例"
};

/**
 * 出奖策略编辑：六个维度的开关与参数、行为画像的信号权重、自定义规则。
 * 只改草稿；载荷与校验在 lottery-strategy-form.ts。
 */
/** 已启用的策略项数（维度、概率控制各算一项，自定义规则整体算一项） */
export function strategyEnabledCount(value: StrategyDraft) {
  return (
    DIMENSIONS.filter((dimension) => value[dimension.key]).length +
    CONTROLS.filter((control) => value[control.key]).length +
    (value.rules.length ? 1 : 0)
  );
}

export function LotteryStrategyEditor({
  value,
  onChange,
  catalog,
  prizeLabels,
  embedded,
  supremeTiers = []
}: {
  value: StrategyDraft;
  onChange: (next: StrategyDraft) => void;
  catalog?: LotteryCatalog | null;
  /** 奖盘上每一格的名字（下标即位置） */
  prizeLabels: string[];
  /** 嵌在外层已有标题的分区里时不再画自己的分组标题 */
  embedded?: boolean;
  /** 应用的会员档位（至尊加成按档位分级时选） */
  supremeTiers?: SupremeTierOption[];
}) {
  const limits = catalog?.strategyLimits ?? STRATEGY_LIMITS;
  const kinds = catalog?.kinds ?? [];
  const winningKinds = kinds.filter((spec) => spec.type !== "none");
  const patch = (next: Partial<StrategyDraft>) => onChange({ ...value, ...next });
  const enabled = strategyEnabledCount(value);

  const content = (
    <>
      <div className="grid gap-2 sm:grid-cols-2">
        {DIMENSIONS.map((dimension) => {
          const Icon = dimension.icon;
          return (
            <SwitchRow
              key={dimension.key}
              label={dimension.label}
              hint={dimension.hint}
              icon={<Icon className="size-3.5" />}
              checked={value[dimension.key]}
              onChange={(checked) => patch({ [dimension.key]: checked } as Partial<StrategyDraft>)}
            />
          );
        })}
      </div>

      <span className="block pt-1 text-[11px] font-medium text-muted-foreground">概率控制</span>
      <div className="grid gap-2 sm:grid-cols-2">
        {CONTROLS.map((control) => {
          const Icon = control.icon;
          return (
            <SwitchRow
              key={control.key}
              label={control.label}
              hint={control.hint}
              icon={<Icon className="size-3.5" />}
              checked={value[control.key]}
              disabled={control.key === "prd" && value.softPity}
              onChange={(checked) => patch({ [control.key]: checked } as Partial<StrategyDraft>)}
            />
          );
        })}
      </div>

      {value.luck ? (
        <DimensionBox title="运气平衡">
          <NumberField
            label="校正强度"
            value={value.luckStrength}
            min={0}
            max={limits.luckStrength ?? 2}
            unit="每 1σ 的对数几率"
            onChange={(next) => patch({ luckStrength: next })}
          />
          <p className="self-end text-[11px] text-muted-foreground">
            0.3 时，比应得少中 2σ 的用户中奖率约提高 1.8 倍
          </p>
        </DimensionBox>
      ) : null}

      {value.fortune ? (
        <DimensionBox title="每日运气">
          <NumberField
            label="运气差异"
            value={value.fortuneSpread}
            min={0}
            max={limits.fortuneSpread ?? 1}
            unit="σ"
            onChange={(next) => patch({ fortuneSpread: next })}
          />
          <p className="self-end text-[11px] text-muted-foreground">
            {fortuneHint(value.fortuneSpread)}
          </p>
        </DimensionBox>
      ) : null}

      {value.fortune ? (
        <div className="space-y-3 rounded-xl border border-border p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-[11px] font-medium text-muted-foreground">至尊加成</span>
            <SupremeModePicker value={value.supremeMode} onChange={(supremeMode) => patch({ supremeMode })} />
          </div>
          {value.supremeMode === "custom" ? (
            <SupremeFields
              value={value.supreme}
              onChange={(supreme) => patch({ supreme })}
              tiers={supremeTiers}
              spread={Number(value.fortuneSpread) || undefined}
            />
          ) : (
            <p className="text-[11px] text-muted-foreground">
              {value.supremeMode === "off"
                ? "本活动的永久会员按自然运气抽奖"
                : "按「运气控制」里的应用设置给永久会员或指定档位加成"}
            </p>
          )}
        </div>
      ) : null}

      {value.prd ? (
        <p className="rounded-xl border border-border px-3 py-2.5 text-[11px] text-muted-foreground">
          距上次中奖第 n 次的中奖率为 n·C，C 按当前中奖率反解，长期中奖率不变
        </p>
      ) : null}

      {value.winRate ? (
        <DimensionBox title="全站中奖率目标">
          <NumberField
            label="目标中奖率"
            value={value.winRateTarget}
            min={0}
            max={100}
            unit="%"
            onChange={(next) => patch({ winRateTarget: next })}
          />
          <NumberField
            label="增益"
            value={value.winRateGain}
            min={0}
            max={limits.controlGain ?? 20}
            unit="1 为完全拉回"
            onChange={(next) => patch({ winRateGain: next })}
          />
          <NumberField
            label="先验强度"
            value={value.winRatePrior}
            min={1}
            unit="次，样本少于它时少调"
            onChange={(next) => patch({ winRatePrior: next })}
          />
        </DimensionBox>
      ) : null}

      {value.guard ? (
        <GuardPanel value={value} patch={patch} winningKinds={winningKinds} />
      ) : null}

      {value.behavior ? (
        <BehaviorPanel value={value} patch={patch} signals={catalog?.signals ?? []} winningKinds={winningKinds} limits={limits} />
      ) : null}

      {value.softPity ? (
        <DimensionBox title="软保底">
          <NumberField
            label="起点"
            value={value.softPityStart}
            min={1}
            unit="连续未中次数"
            onChange={(next) => patch({ softPityStart: next })}
          />
          <NumberField
            label="每次提升"
            value={value.softPityStep}
            min={0}
            max={limits.softPityStep}
            unit="百分点"
            onChange={(next) => patch({ softPityStep: next })}
          />
          <p className="text-[11px] text-muted-foreground sm:col-span-2">
            连续未中 {value.softPityStart || "N"} 次起，每多未中一次，中奖总概率提高 {value.softPityStep || "x"} 个百分点
          </p>
        </DimensionBox>
      ) : null}

      {value.pacing ? (
        <DimensionBox title="库存匀速释放">
          <NumberField
            label="提前量"
            value={value.pacingSlack}
            min={0}
            max={100}
            unit="% 库存"
            onChange={(next) => patch({ pacingSlack: next })}
          />
          <NumberField
            label="落后时最多加速"
            value={value.pacingBoost}
            min={1}
            max={limits.pacingBoost}
            unit="倍，1 为不加速"
            onChange={(next) => patch({ pacingBoost: next })}
          />
          <p className="text-[11px] text-muted-foreground sm:col-span-2">需要设置结束时间，只作用于限量奖品</p>
        </DimensionBox>
      ) : null}

      {value.cooldown ? (
        <DimensionBox title="中奖冷却">
          <NumberField
            label="冷却窗口"
            value={value.cooldownDraws}
            min={1}
            max={limits.cooldownDraws}
            unit="最近几次"
            onChange={(next) => patch({ cooldownDraws: next })}
          />
          <NumberField
            label="权重系数"
            value={value.cooldownFactor}
            min={0}
            max={1}
            unit="0–1"
            onChange={(next) => patch({ cooldownFactor: next })}
          />
        </DimensionBox>
      ) : null}

      {value.budget ? (
        <DimensionBox title="积分预算">
          <NumberField
            label="每日积分预算"
            value={value.budgetPoints}
            min={1}
            unit="积分，含折算"
            onChange={(next) => patch({ budgetPoints: next })}
          />
          <div className="self-end">
            <SwitchRow
              label="按时间进度放量"
              checked={value.budgetPaced}
              onChange={(checked) => patch({ budgetPaced: checked })}
            />
          </div>
        </DimensionBox>
      ) : null}

      {value.risk ? (
        <RiskRulesEditor
          rules={value.riskRules}
          onChange={(riskRules) => patch({ riskRules })}
          catalog={catalog}
          max={limits.riskRules ?? STRATEGY_LIMITS.riskRules ?? 16}
        />
      ) : null}

      <RulesPanel
        rules={value.rules}
        onChange={(rules) => patch({ rules })}
        catalog={catalog}
        prizeLabels={prizeLabels}
        max={limits.rules}
      />
    </>
  );
  return embedded ? (
    <div className="space-y-2.5">{content}</div>
  ) : (
    <FieldGroup label="出奖策略" hint={enabled ? `已启用 ${enabled} 项` : "未启用，按权重随机"}>
      {content}
    </FieldGroup>
  );
}

function DimensionBox({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3 rounded-xl border border-border p-3">
      <span className="text-[11px] font-medium text-muted-foreground">{title}</span>
      <div className="grid gap-3 sm:grid-cols-2">{children}</div>
    </div>
  );
}

// ── 护栏 ──

function GuardPanel({
  value,
  patch,
  winningKinds
}: {
  value: StrategyDraft;
  patch: (next: Partial<StrategyDraft>) => void;
  winningKinds: Array<{ type: string; label: string }>;
}) {
  return (
    <div className="space-y-3 rounded-xl border border-border p-3">
      <span className="text-[11px] font-medium text-muted-foreground">护栏</span>
      <div className="grid gap-3 sm:grid-cols-3">
        <NumberField
          label="中奖率下限"
          value={value.guardMin}
          min={0}
          max={100}
          unit="%"
          placeholder="不限"
          onChange={(next) => patch({ guardMin: next })}
        />
        <NumberField
          label="中奖率上限"
          value={value.guardMax}
          min={0}
          max={100}
          unit="%"
          placeholder="不限"
          onChange={(next) => patch({ guardMax: next })}
        />
        <NumberField
          label="每次期望价值上限"
          value={value.guardValue}
          min={0}
          unit="积分当量"
          placeholder="不限"
          onChange={(next) => patch({ guardValue: next })}
        />
        <NumberField
          label="经验折算"
          value={value.guardExpRate}
          min={0}
          unit="积分/点"
          placeholder="0.1"
          onChange={(next) => patch({ guardExpRate: next })}
        />
        <NumberField
          label="抽奖次数折算"
          value={value.guardDrawValue}
          min={0}
          unit="积分/次"
          placeholder="积分抽奖价"
          onChange={(next) => patch({ guardDrawValue: next })}
        />
      </div>
      <div className="space-y-1.5">
        <span className="text-[11px] font-medium">奖品类型单次占比上限</span>
        <div className="grid gap-2 sm:grid-cols-3">
          {winningKinds.map((spec) => (
            <div key={spec.type} className="flex items-center gap-2">
              <span className="w-20 shrink-0 truncate text-[11px] text-muted-foreground">{spec.label}</span>
              <Input
                type="number"
                min={0}
                max={100}
                step={0.1}
                value={value.guardCaps[spec.type] ?? ""}
                placeholder="不限"
                onChange={(event) => patch({ guardCaps: { ...value.guardCaps, [spec.type]: event.target.value } })}
                className="h-7 font-mono text-xs tabular-nums"
                aria-label={`${spec.label}占比上限`}
              />
              <span className="text-[10px] text-muted-foreground">%</span>
            </div>
          ))}
        </div>
      </div>
      <p className="text-[10px] text-muted-foreground">在满足全部约束的分布中取与原分布差异最小的一个</p>
    </div>
  );
}

// ── 行为画像 ──

function BehaviorPanel({
  value,
  patch,
  signals,
  winningKinds,
  limits
}: {
  value: StrategyDraft;
  patch: (next: Partial<StrategyDraft>) => void;
  signals: LotterySignalSpec[];
  winningKinds: Array<{ type: string; label: string }>;
  limits: { behaviorBoost: number };
}) {
  const groups = useMemo(() => {
    const map = new Map<string, LotterySignalSpec[]>();
    for (const spec of signals) map.set(spec.group, [...(map.get(spec.group) ?? []), spec]);
    return [...map.entries()];
  }, [signals]);
  const weightOf = (spec: LotterySignalSpec) => value.signals[spec.key]?.weight ?? String(spec.weight);
  const halfOf = (spec: LotterySignalSpec) => value.signals[spec.key]?.half ?? "";
  const setSignal = (spec: LotterySignalSpec, next: Partial<{ weight: string; half: string }>) =>
    patch({
      signals: {
        ...value.signals,
        [spec.key]: { weight: weightOf(spec), half: halfOf(spec), ...next }
      }
    });
  const totalWeight = signals.reduce((sum, spec) => (spec.negative ? sum : sum + Math.max(0, Number(weightOf(spec)) || 0)), 0);

  return (
    <div className="space-y-3 rounded-xl border border-border p-3">
      <span className="text-[11px] font-medium text-muted-foreground">行为画像</span>
      <div className="grid gap-3 sm:grid-cols-2">
        <NumberField
          label="行为分为 0 时"
          value={value.behaviorMin}
          min={0}
          max={1}
          unit="中奖权重 ×"
          onChange={(next) => patch({ behaviorMin: next })}
        />
        <NumberField
          label="行为分满分时"
          value={value.behaviorMax}
          min={1}
          max={limits.behaviorBoost}
          unit="中奖权重 ×"
          onChange={(next) => patch({ behaviorMax: next })}
        />
      </div>
      <div className="space-y-1.5">
        <span className="text-[11px] font-medium">作用于</span>
        <ToggleGroup
          type="multiple"
          variant="outline"
          size="sm"
          value={value.behaviorKinds}
          onValueChange={(next) => patch({ behaviorKinds: next })}
          className="flex-wrap justify-start"
        >
          {winningKinds.map((spec) => (
            <ToggleGroupItem key={spec.type} value={spec.type} className="h-7 px-2.5 text-[11px]">
              {spec.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <p className="text-[10px] text-muted-foreground">不选为全部中奖奖品</p>
      </div>
      <SwitchRow
        label="自定义信号权重"
        hint={value.customSignals ? undefined : `使用推荐权重，共 ${signals.length} 项信号`}
        checked={value.customSignals}
        onChange={(checked) => patch({ customSignals: checked })}
      />
      {value.customSignals ? (
        <div className="space-y-3">
          {groups.map(([group, specs]) => (
            <div key={group} className="space-y-1">
              <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{group}</span>
              <div className="divide-y divide-border rounded-lg border border-border">
                {specs.map((spec) => {
                  const weight = Math.max(0, Number(weightOf(spec)) || 0);
                  const share = !spec.negative && totalWeight > 0 ? (weight / totalWeight) * 100 : 0;
                  return (
                    <div key={spec.key} className="grid grid-cols-[1fr_5.5rem_6rem] items-center gap-2 px-2.5 py-1.5">
                      <div className="min-w-0">
                        <div className="flex items-center gap-1.5 text-xs">
                          <span className="truncate">{spec.label}</span>
                          {spec.negative ? (
                            <span className="shrink-0 rounded bg-rose-500/10 px-1 text-[10px] text-rose-600 dark:text-rose-400">惩罚</span>
                          ) : null}
                        </div>
                        <div className="mt-0.5 flex items-center gap-2 text-[10px] text-muted-foreground">
                          <span>{MAP_HINT[spec.map] ?? spec.map}</span>
                          {!spec.negative ? (
                            <span className="h-1 w-16 overflow-hidden rounded-full bg-muted">
                              <span className="block h-full rounded-full bg-emerald-500" style={{ width: `${share}%` }} />
                            </span>
                          ) : null}
                        </div>
                      </div>
                      <Input
                        type="number"
                        min={0}
                        step={0.5}
                        value={weightOf(spec)}
                        onChange={(event) => setSignal(spec, { weight: event.target.value })}
                        className="h-7 font-mono text-xs tabular-nums"
                        aria-label={`${spec.label}权重`}
                      />
                      {spec.map === "saturate" || spec.map === "decay" ? (
                        <Input
                          type="number"
                          min={0}
                          value={halfOf(spec)}
                          placeholder={`${spec.half ?? ""}${spec.unit ?? ""}`}
                          onChange={(event) => setSignal(spec, { half: event.target.value })}
                          className="h-7 font-mono text-xs tabular-nums"
                          aria-label={`${spec.label}半饱和值`}
                        />
                      ) : (
                        <span />
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          ))}
          <p className="text-[10px] text-muted-foreground">左为权重，右为半饱和值（达到它时该项得 0.5 分），权重 0 为不参与</p>
        </div>
      ) : null}
    </div>
  );
}

// ── 自定义规则 ──

function RulesPanel({
  rules,
  onChange,
  catalog,
  prizeLabels,
  max
}: {
  rules: RuleDraft[];
  onChange: (rules: RuleDraft[]) => void;
  catalog?: LotteryCatalog | null;
  prizeLabels: string[];
  max: number;
}) {
  const nextKey = rules.reduce((value, rule) => Math.max(value, rule.key), 0) + 1;
  const add = (template?: Omit<RuleDraft, "key" | "disabled">) =>
    onChange([
      ...rules,
      { key: nextKey, disabled: false, ...(template ?? { name: "", when: "", kinds: [], positions: [], factor: "1" }) }
    ]);
  const update = (key: number, next: Partial<RuleDraft>) =>
    onChange(rules.map((rule) => (rule.key === key ? { ...rule, ...next } : rule)));

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-[11px] font-medium">
          <Braces className="size-3.5 text-muted-foreground" />
          自定义规则
          <span className="text-muted-foreground">
            {rules.length}/{max}
          </span>
        </span>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="outline" className="h-7" disabled={rules.length >= max}>
              <Plus className="size-3.5" /> 添加规则
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-64">
            <DropdownMenuItem onSelect={() => add()}>空白规则</DropdownMenuItem>
            <DropdownMenuLabel className="text-[10px] text-muted-foreground">常用</DropdownMenuLabel>
            {RULE_TEMPLATES.map((template) => (
              <DropdownMenuItem key={template.name} onSelect={() => add(template)}>
                <span className="truncate">{template.name}</span>
                <span className="ml-auto font-mono text-[10px] text-muted-foreground">×{template.factor}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {rules.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
          条件成立时给指定奖品的权重乘以系数
        </p>
      ) : (
        rules.map((rule, index) => (
          <RuleCard
            key={rule.key}
            index={index}
            rule={rule}
            catalog={catalog}
            prizeLabels={prizeLabels}
            onChange={(next) => update(rule.key, next)}
            onRemove={() => onChange(rules.filter((item) => item.key !== rule.key))}
          />
        ))
      )}
    </div>
  );
}

function RuleCard({
  index,
  rule,
  catalog,
  prizeLabels,
  onChange,
  onRemove
}: {
  index: number;
  rule: RuleDraft;
  catalog?: LotteryCatalog | null;
  prizeLabels: string[];
  onChange: (next: Partial<RuleDraft>) => void;
  onRemove: () => void;
}) {
  const kinds = catalog?.kinds ?? [];
  const variables = catalog?.ruleVariables ?? [];
  const insert = (name: string) => onChange({ when: rule.when ? `${rule.when.trimEnd()} ${name}` : name });

  return (
    <div className={cn("space-y-2.5 rounded-xl border border-border p-3", rule.disabled && "opacity-60")}>
      <div className="flex items-center gap-2">
        <span className="w-5 shrink-0 text-center font-mono text-[11px] text-muted-foreground">{index + 1}</span>
        <Input
          value={rule.name}
          maxLength={32}
          placeholder="规则名称"
          onChange={(event) => onChange({ name: event.target.value })}
          className="h-8 text-xs"
        />
        <div className="flex shrink-0 items-center gap-1">
          <span className="text-[11px] text-muted-foreground">×</span>
          <Input
            type="number"
            min={0}
            step={0.1}
            value={rule.factor}
            onChange={(event) => onChange({ factor: event.target.value })}
            className="h-8 w-20 font-mono text-xs tabular-nums"
            aria-label="系数"
          />
        </div>
        <Switch checked={!rule.disabled} onCheckedChange={(checked) => onChange({ disabled: !checked })} aria-label="启用" />
        <Button size="icon" variant="ghost" className="size-8 shrink-0" onClick={onRemove} aria-label="删除规则">
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      <div className="space-y-1">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-medium">条件</span>
          <Popover>
            <PopoverTrigger asChild>
              <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px]">
                <Sparkles className="size-3" /> 变量
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-80 p-0">
              <ScrollArea className="h-72">
                <div className="p-1">
                  {variables.map((variable) => (
                    <button
                      key={variable.name}
                      type="button"
                      onClick={() => insert(variable.name)}
                      className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted"
                    >
                      <span className="font-mono text-[11px]">{variable.name}</span>
                      <span className="truncate text-[10px] text-muted-foreground">{variable.label}</span>
                    </button>
                  ))}
                </div>
              </ScrollArea>
            </PopoverContent>
          </Popover>
        </div>
        <Textarea
          value={rule.when}
          rows={2}
          spellCheck={false}
          placeholder="user.signStreak >= 7 && user.onlineMinutes7d >= 120"
          onChange={(event) => onChange({ when: event.target.value })}
          className="min-h-0 font-mono text-xs"
        />
        <p className="text-[10px] text-muted-foreground">留空为总是生效，保存时由服务端校验</p>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <div className="space-y-1">
          <span className="text-[11px] font-medium">奖品类型</span>
          <ToggleGroup
            type="multiple"
            variant="outline"
            size="sm"
            value={rule.kinds}
            onValueChange={(next) => onChange({ kinds: next })}
            className="flex-wrap justify-start"
          >
            {kinds.map((spec) => (
              <ToggleGroupItem key={spec.type} value={spec.type} className="h-7 px-2 text-[11px]">
                {kindLabelOf(spec.type, kinds)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
        <div className="space-y-1">
          <span className="text-[11px] font-medium">奖品位置</span>
          <ToggleGroup
            type="multiple"
            variant="outline"
            size="sm"
            value={rule.positions.map(String)}
            onValueChange={(next) => onChange({ positions: next.map(Number).sort((a, b) => a - b) })}
            className="flex-wrap justify-start"
          >
            {prizeLabels.map((label, position) => (
              <ToggleGroupItem
                key={position}
                value={String(position)}
                title={label}
                className="h-7 w-7 p-0 font-mono text-[11px]"
              >
                {position + 1}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
      </div>
      <p className="text-[10px] text-muted-foreground">类型与位置都不选时作用于全部奖品，都选时须同时满足</p>
    </div>
  );
}
