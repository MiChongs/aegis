"use client";

import { useState, type ReactNode } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import type {
  SignInRewardMilestone,
  SignInRewardPolicy,
  SignInRewardRule
} from "@/lib/api/types";

/**
 * 签到奖励区块的共享原语：文案映射、表达式变量目录、草稿工具与数值输入。
 *
 * 计算顺序（与后端 calculateSignInRewardWithPolicy 一致，改这里的说明前先对一遍后端）：
 *   1. 积分底数 = 基础积分 + 命中规则的积分加成 + 命中里程碑的积分加成
 *   2. 经验底数 = 基础经验 + 首签加成（仅第一次） + min((连签-1)×步进, 步进上限)
 *                 + 命中规则的经验加成 + 命中里程碑的经验加成
 *   3. 积分 = ⌊积分底数 × (1 + Σ积分倍率增量)⌋；经验同理用经验倍率增量
 *   4. 开启「等级经验倍率」时经验再乘用户当前等级的倍率
 *   5. 两者最少为 1，再按上限截断（上限为 0 表示不限）
 *
 * 同一分组的规则按优先级从小到大，只命中第一条；未分组的规则各自独立。
 */

export const VARIABLES: Array<{ name: string; label: string; type: "数值" | "布尔"; example: string }> = [
  { name: "consecutive_days", label: "本次签到后的连签天数", type: "数值", example: "consecutive_days >= 7" },
  { name: "total_sign_ins", label: "此前累计签到次数", type: "数值", example: "total_sign_ins >= 100" },
  { name: "is_first_sign", label: "是否首次签到", type: "布尔", example: "is_first_sign" },
  { name: "is_weekend", label: "是否周六或周日", type: "布尔", example: "is_weekend" },
  { name: "weekday_iso", label: "星期（1 为周一，7 为周日）", type: "数值", example: "weekday_iso == 5" },
  { name: "weekday", label: "星期（0 为周日）", type: "数值", example: "weekday == 0" },
  { name: "day", label: "日期（1-31）", type: "数值", example: "day <= 3" },
  { name: "month", label: "月份（1-12）", type: "数值", example: "month == 12" },
  { name: "year", label: "年份", type: "数值", example: "year == 2026" },
  { name: "hour", label: "小时（0-23，策略时区）", type: "数值", example: "hour < 8" },
  { name: "minute", label: "分钟", type: "数值", example: "minute < 30" },
  { name: "user_experience", label: "用户当前经验", type: "数值", example: "user_experience < 1000" }
];

/** 常用条件，点一下填进表达式。 */
export const EXPRESSION_PRESETS: Array<{ label: string; expression: string }> = [
  { label: "连签满 7 天", expression: "consecutive_days >= 7" },
  { label: "周末", expression: "is_weekend" },
  { label: "工作日", expression: "weekday_iso <= 5" },
  { label: "月初三天", expression: "day <= 3" },
  { label: "早起（8 点前）", expression: "hour < 8" },
  { label: "首次签到", expression: "is_first_sign" },
  { label: "新用户（经验低于 500）", expression: "user_experience < 500" },
  { label: "老用户（累计 100 次）", expression: "total_sign_ins >= 100" }
];

export const SOURCE_LABEL: Record<string, string> = {
  manual: "手动签到",
  auto: "自动签到"
};

export function sourceLabel(source?: string) {
  const key = source || "manual";
  return SOURCE_LABEL[key] ?? key;
}

export const WEEKDAY_LABEL = ["", "周一", "周二", "周三", "周四", "周五", "周六", "周日"];

/** 奖励类型 → 可读名。规则自定义的类型优先取规则名，其次是内置词表。 */
const BUILTIN_BONUS_LABEL: Record<string, string> = {
  normal: "普通签到",
  compound: "组合加成",
  milestone: "里程碑",
  disabled: "奖励已关闭",
  weekend: "周末奖励",
  weekly: "7 天连签",
  streak: "3 天连签",
  half_month: "14 天连签",
  monthly_master: "30 天连签",
  month_start: "月初奖励",
  mid_month: "月中奖励",
  workday_bonus: "工作日冲刺"
};

export function buildBonusLabels(policy?: SignInRewardPolicy | null) {
  const labels: Record<string, string> = { ...BUILTIN_BONUS_LABEL };
  for (const rule of policy?.rules ?? []) {
    if (rule.bonusType) labels[rule.bonusType] = rule.name || labels[rule.bonusType] || rule.bonusType;
  }
  return labels;
}

export function bonusLabel(labels: Record<string, string>, type?: string) {
  const key = type || "normal";
  return labels[key] ?? key;
}

/** 规则与里程碑 key → 可读名，用于试算结果里的命中列表。 */
export function buildRuleNames(policy?: SignInRewardPolicy | null) {
  const names: Record<string, string> = {};
  for (const rule of policy?.rules ?? []) names[rule.key] = rule.name || rule.key;
  for (const m of policy?.milestones ?? []) names[`milestone_${m.consecutiveDays}`] = `${m.consecutiveDays} 天里程碑`;
  return names;
}

// ── 格式化 ──────────────────────────

export function formatNumber(value?: number | null, digits = 0) {
  if (typeof value !== "number" || !Number.isFinite(value)) return "0";
  return value.toLocaleString("zh-CN", { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

export function formatDelta(value: number, digits = 1) {
  const text = value.toLocaleString("zh-CN", { maximumFractionDigits: digits });
  return value > 0 ? `+${text}` : text;
}

export function formatDateTime(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

export function formatTime(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

/** 规则效果的一句话：「积分 ×1.5，经验 +15」。 */
export function describeEffect(item: Pick<SignInRewardRule, "integralMultiplierDelta" | "integralBonus" | "experienceMultiplierDelta" | "experienceBonus">) {
  const parts: string[] = [];
  if (item.integralMultiplierDelta) parts.push(`积分倍率 ${formatDelta(item.integralMultiplierDelta, 2)}`);
  if (item.integralBonus) parts.push(`积分 ${formatDelta(item.integralBonus)}`);
  if (item.experienceMultiplierDelta) parts.push(`经验倍率 ${formatDelta(item.experienceMultiplierDelta, 2)}`);
  if (item.experienceBonus) parts.push(`经验 ${formatDelta(item.experienceBonus)}`);
  return parts.length ? parts.join("，") : "无额外效果";
}

// ── 草稿 ──────────────────────────

export function clonePolicy(policy: SignInRewardPolicy): SignInRewardPolicy {
  return JSON.parse(JSON.stringify(policy)) as SignInRewardPolicy;
}

/** 后端保存时会排序、补默认值，比较前双方都按同一规则归一，避免「刚保存就显示有改动」。 */
function canonical(policy: SignInRewardPolicy) {
  const rules = [...(policy.rules ?? [])].sort((a, b) => a.priority - b.priority || a.key.localeCompare(b.key));
  const milestones = [...(policy.milestones ?? [])].sort((a, b) => a.consecutiveDays - b.consecutiveDays);
  const { isDefault: _ignored, ...rest } = policy;
  void _ignored;
  return JSON.stringify({ ...rest, rules, milestones });
}

export function isPolicyDirty(saved?: SignInRewardPolicy | null, draft?: SignInRewardPolicy | null) {
  if (!saved || !draft) return false;
  return canonical(saved) !== canonical(draft);
}

export function newRule(existing: SignInRewardRule[]): SignInRewardRule {
  let index = existing.length + 1;
  const keys = new Set(existing.map((rule) => rule.key));
  while (keys.has(`rule_${index}`)) index += 1;
  const maxPriority = existing.reduce((max, rule) => Math.max(max, rule.priority), 0);
  return {
    key: `rule_${index}`,
    name: `新规则 ${index}`,
    description: "",
    enabled: true,
    priority: maxPriority + 10,
    group: "",
    // 变量是下划线命名；控制台旧版这里写的是 consecutiveDays，保存会报表达式无效
    expression: "consecutive_days >= 1",
    bonusType: `rule_${index}`,
    bonusDescription: "",
    integralMultiplierDelta: 0,
    integralBonus: 0,
    experienceMultiplierDelta: 0,
    experienceBonus: 0
  };
}

export function newMilestone(existing: SignInRewardMilestone[]): SignInRewardMilestone {
  const used = new Set(existing.map((m) => m.consecutiveDays));
  const candidates = [7, 14, 30, 60, 90, 180, 365];
  const day = candidates.find((d) => !used.has(d)) ?? Math.max(0, ...existing.map((m) => m.consecutiveDays)) + 30;
  return { consecutiveDays: day, integralBonus: 0, experienceBonus: 100, bonusType: "milestone", description: `${day} 天里程碑奖励` };
}

/** 前端能确定的问题；表达式语法与取值范围由后端在保存与试算时校验。 */
export function localPolicyProblems(policy: SignInRewardPolicy) {
  const problems: string[] = [];
  if (!policy.timezone.trim()) problems.push("时区不能为空");
  const keys = new Map<string, number>();
  for (const rule of policy.rules) {
    if (!rule.key.trim()) problems.push(`规则「${rule.name || "未命名"}」缺少标识`);
    keys.set(rule.key, (keys.get(rule.key) ?? 0) + 1);
    if (rule.enabled && !rule.expression.trim()) problems.push(`规则「${rule.name || rule.key}」缺少触发条件`);
    if (/[a-z][A-Z]/.test(rule.expression)) problems.push(`规则「${rule.name || rule.key}」的表达式使用了驼峰变量，变量名应为下划线形式（如 consecutive_days）`);
  }
  for (const [key, count] of keys) if (key && count > 1) problems.push(`规则标识「${key}」重复`);
  const days = new Map<number, number>();
  for (const m of policy.milestones) {
    if (m.consecutiveDays <= 0) problems.push("里程碑天数必须大于 0");
    days.set(m.consecutiveDays, (days.get(m.consecutiveDays) ?? 0) + 1);
  }
  for (const [day, count] of days) if (count > 1) problems.push(`${day} 天里程碑重复`);
  return problems;
}

// ── 控件 ──────────────────────────

/**
 * 数值输入：输入过程中保留原文本（空串、单个负号、末尾小数点），失焦或合法时才回写数字，
 * 避免「清空后立刻变回 0」「打不出负号」这类受控数字输入框的老毛病。
 */
export function NumInput({
  value,
  onChange,
  min,
  max,
  step,
  className,
  suffix,
  "aria-label": ariaLabel
}: {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  className?: string;
  suffix?: string;
  "aria-label"?: string;
}) {
  const [text, setText] = useState(String(value ?? 0));
  // 外部改了值（载入模板、放弃修改）时同步文本；自己输入引起的变化文本本来就对得上
  const [synced, setSynced] = useState(value);
  if (value !== synced) {
    setSynced(value);
    if (Number(text) !== value || text.trim() === "") setText(String(value ?? 0));
  }
  const clamp = (n: number) => Math.min(max ?? Number.POSITIVE_INFINITY, Math.max(min ?? Number.NEGATIVE_INFINITY, n));
  return (
    <div className={cn("relative", className)}>
      <Input
        type="text"
        inputMode={step && step < 1 ? "decimal" : "numeric"}
        aria-label={ariaLabel}
        value={text}
        onChange={(event) => {
          const next = event.target.value;
          if (!/^-?\d*\.?\d*$/.test(next)) return;
          setText(next);
          const n = Number(next);
          if (next.trim() !== "" && next !== "-" && !next.endsWith(".") && Number.isFinite(n)) onChange(clamp(n));
        }}
        onBlur={() => {
          const n = Number(text);
          const fixed = text.trim() === "" || text === "-" || !Number.isFinite(n) ? 0 : clamp(n);
          setText(String(fixed));
          onChange(fixed);
        }}
        className={cn("h-9 font-mono text-sm tabular-nums", suffix && "pr-10")}
      />
      {suffix ? (
        <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">{suffix}</span>
      ) : null}
    </div>
  );
}

export function Field({
  label,
  hint,
  children,
  className
}: {
  label: string;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0 space-y-1.5", className)}>
      <Label className="text-xs font-medium">{label}</Label>
      {children}
      {hint ? <p className="text-[11px] leading-snug text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/** 指标卡：值 + 与对比值的差异。 */
export function MetricTile({
  label,
  value,
  hint,
  icon,
  delta,
  loading,
  tone
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  icon?: ReactNode;
  /** 相对变化百分比，null 表示无可比数据 */
  delta?: number | null;
  loading?: boolean;
  tone?: "integral" | "experience";
}) {
  return (
    <div className="min-w-0 rounded-2xl border bg-card px-4 py-3.5">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {icon ? (
          <span
            className={cn(
              "grid size-5 place-items-center rounded-md bg-muted",
              tone === "integral" && "bg-amber-500/10 text-amber-600 dark:text-amber-400",
              tone === "experience" && "bg-sky-500/10 text-sky-600 dark:text-sky-400"
            )}
          >
            {icon}
          </span>
        ) : null}
        <span className="truncate">{label}</span>
      </div>
      {loading ? (
        <div className="mt-2 h-7 w-20 animate-pulse rounded-md bg-muted" />
      ) : (
        <div className="mt-1.5 flex items-baseline gap-2">
          <span className="truncate text-2xl leading-8 font-semibold tracking-tight tabular-nums">{value}</span>
          {typeof delta === "number" && Number.isFinite(delta) ? (
            <span
              className={cn(
                "shrink-0 rounded-md px-1 text-[11px] font-medium tabular-nums",
                delta > 0 && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
                delta < 0 && "bg-red-500/10 text-red-700 dark:text-red-300",
                delta === 0 && "bg-muted text-muted-foreground"
              )}
            >
              {formatDelta(delta, 0)}%
            </span>
          ) : null}
        </div>
      )}
      <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{loading ? " " : hint}</div>
    </div>
  );
}

/** 卡片外壳：标题、说明、右侧操作。 */
export function Panel({
  title,
  description,
  icon,
  action,
  children,
  className,
  bodyClassName
}: {
  title: string;
  description?: ReactNode;
  icon?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={cn("overflow-hidden rounded-2xl border bg-card", className)} style={{ boxShadow: "var(--shadow-soft)" }}>
      <header className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2 border-b px-4 py-3.5 sm:px-5">
        <div className="flex min-w-0 items-start gap-2.5">
          {icon ? <span className="mt-0.5 text-muted-foreground">{icon}</span> : null}
          <div className="min-w-0 space-y-0.5">
            <h3 className="text-sm font-semibold tracking-tight">{title}</h3>
            {description ? <p className="text-xs leading-5 text-muted-foreground">{description}</p> : null}
          </div>
        </div>
        {action ? <div className="flex shrink-0 flex-wrap items-center gap-2">{action}</div> : null}
      </header>
      <div className={cn("p-4 sm:p-5", bodyClassName)}>{children}</div>
    </section>
  );
}
