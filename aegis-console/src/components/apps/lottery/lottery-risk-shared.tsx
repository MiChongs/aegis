"use client";

import { Ban, ChevronsDown, ShieldAlert, ShieldOff, Sparkle, type LucideIcon } from "lucide-react";
import type {
  LotteryCatalog,
  LotteryRiskAction,
  LotteryRiskActionSpec,
  LotteryRiskCheck,
  LotteryRiskOp,
  LotteryRiskSignalSpec
} from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/**
 * 风控的视觉口径与文案：信号、处置、比较方式。
 *
 * 规则编辑器、出奖方案、抽奖记录与风控总览都从这里取名字和颜色，同一种处置在哪儿都是同一个颜色。
 * 目录以后端下发的为准（catalog.riskSignals / riskActions）；老服务端没下发时用这里的回落表。
 */

const FALLBACK_SIGNALS: LotteryRiskSignalSpec[] = [
  { key: "deviceAccounts", label: "同设备今日账号", unit: "个", group: "聚集", op: "gt", threshold: 3, hint: "本活动今天在这台设备上抽过奖的账号数，含本人" },
  { key: "ipAccounts", label: "同 IP 今日账号", unit: "个", group: "聚集", op: "gt", threshold: 5, hint: "本活动今天从这个 IP 抽过奖的账号数，含本人" },
  { key: "subnetAccounts", label: "同网段今日账号", unit: "个", group: "聚集", op: "gt", threshold: 12, hint: "IPv4 按 /24、IPv6 按 /64 合并后的账号数" },
  { key: "deviceHistory", label: "设备历史账号", unit: "个", group: "设备", op: "gt", threshold: 5, hint: "这台设备在应用里登录过的账号总数" },
  { key: "accountDevices", label: "账号登录设备", unit: "台", group: "设备", op: "gt", threshold: 8, hint: "这个账号登录过的设备数" },
  { key: "noDevice", label: "缺少设备标识", unit: "", group: "设备", op: "gte", threshold: 1, bool: true, hint: "请求没有带设备号" },
  { key: "accountAgeHours", label: "注册时长", unit: "小时", group: "账号", op: "lt", threshold: 24, hint: "账号注册至今的小时数" },
  { key: "riskScore", label: "近 30 天风险分", unit: "分", group: "账号", op: "gte", threshold: 60, hint: "风控中心近 30 天的最高分" },
  { key: "burst10m", label: "近 10 分钟抽奖", unit: "次", group: "频率", op: "gt", threshold: 30, hint: "最近 10 分钟在本活动抽了几次" },
  { key: "todayDraws", label: "今日抽奖", unit: "次", group: "频率", op: "gt", threshold: 200, hint: "今天在本活动抽了几次" },
  { key: "winsToday", label: "今日中奖", unit: "次", group: "频率", op: "gt", threshold: 50, hint: "今天在本活动中了几次" },
  { key: "legacyCluster", label: "设备或网络聚集", unit: "", group: "聚集", op: "gt", threshold: 0, hidden: true, hint: "迁移前的记录" }
];

const FALLBACK_ACTIONS: LotteryRiskActionSpec[] = [
  { key: "damp", label: "降权", hint: "中奖奖品权重乘以系数，多条相乘", severity: 1 },
  { key: "noPity", label: "不触发保底", hint: "硬保底、软保底、伪随机分布按未连败计算", severity: 2 },
  { key: "rare", label: "停发稀有", hint: "公示概率低于设定值的中奖奖品不出", severity: 3 },
  { key: "floor", label: "只发兜底", hint: "兜底奖以外的中奖奖品都不出", severity: 4 },
  { key: "deny", label: "拒绝抽奖", hint: "本次不抽，次数与积分不扣，记一条风控事件", severity: 5 }
];

export function riskSignals(catalog?: LotteryCatalog | null) {
  return catalog?.riskSignals?.length ? catalog.riskSignals : FALLBACK_SIGNALS;
}

export function riskActions(catalog?: LotteryCatalog | null) {
  return catalog?.riskActions?.length ? catalog.riskActions : FALLBACK_ACTIONS;
}

export function riskSignalOf(key: string, catalog?: LotteryCatalog | null): LotteryRiskSignalSpec {
  return (
    riskSignals(catalog).find((spec) => spec.key === key) ??
    FALLBACK_SIGNALS.find((spec) => spec.key === key) ?? { key, label: key, unit: "", hint: "", group: "", op: "gt", threshold: 0 }
  );
}

export function riskActionOf(key: string | undefined, catalog?: LotteryCatalog | null): LotteryRiskActionSpec | undefined {
  if (!key) return undefined;
  return riskActions(catalog).find((spec) => spec.key === key) ?? FALLBACK_ACTIONS.find((spec) => spec.key === key);
}

export const RISK_OPS: Array<{ value: LotteryRiskOp; label: string }> = [
  { value: "gt", label: ">" },
  { value: "gte", label: "≥" },
  { value: "lt", label: "<" },
  { value: "lte", label: "≤" }
];

export const riskOpSymbol = (op: LotteryRiskOp) => RISK_OPS.find((item) => item.value === op)?.label ?? op;

type ActionVisual = { icon: LucideIcon; chip: string; solid: string; text: string; bar: string };

/** 处置的颜色从轻到重：天蓝 → 靛 → 琥珀 → 橙 → 玫红 */
const ACTION_VISUALS: Record<LotteryRiskAction, ActionVisual> = {
  damp: {
    icon: ChevronsDown,
    chip: "bg-sky-500/10 text-sky-700 ring-sky-500/25 dark:text-sky-300",
    solid: "bg-sky-500",
    text: "text-sky-600 dark:text-sky-400",
    bar: "bg-sky-400"
  },
  noPity: {
    icon: Sparkle,
    chip: "bg-indigo-500/10 text-indigo-700 ring-indigo-500/25 dark:text-indigo-300",
    solid: "bg-indigo-500",
    text: "text-indigo-600 dark:text-indigo-400",
    bar: "bg-indigo-400"
  },
  rare: {
    icon: ShieldOff,
    chip: "bg-amber-500/12 text-amber-700 ring-amber-500/30 dark:text-amber-300",
    solid: "bg-amber-500",
    text: "text-amber-600 dark:text-amber-400",
    bar: "bg-amber-400"
  },
  floor: {
    icon: ShieldAlert,
    chip: "bg-orange-500/12 text-orange-700 ring-orange-500/30 dark:text-orange-300",
    solid: "bg-orange-500",
    text: "text-orange-600 dark:text-orange-400",
    bar: "bg-orange-400"
  },
  deny: {
    icon: Ban,
    chip: "bg-rose-500/12 text-rose-700 ring-rose-500/30 dark:text-rose-300",
    solid: "bg-rose-500",
    text: "text-rose-600 dark:text-rose-400",
    bar: "bg-rose-500"
  }
};

export function riskActionVisual(action?: string): ActionVisual {
  return ACTION_VISUALS[(action ?? "") as LotteryRiskAction] ?? ACTION_VISUALS.damp;
}

/** 处置徽标：图标 + 名称（带细描边的色块） */
export function RiskActionChip({
  action,
  catalog,
  className,
  suffix
}: {
  action?: string;
  catalog?: LotteryCatalog | null;
  className?: string;
  suffix?: React.ReactNode;
}) {
  if (!action) return null;
  const visual = riskActionVisual(action);
  const Icon = visual.icon;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 whitespace-nowrap rounded-md px-1.5 py-0.5 text-[11px] font-medium ring-1 ring-inset",
        visual.chip,
        className
      )}
    >
      <Icon className="size-3" />
      {riskActionOf(action, catalog)?.label ?? action}
      {suffix}
    </span>
  );
}

/** 数值按信号的单位显示（是或否的信号显示「是 / 否」） */
export function riskValueText(value: number, spec: LotteryRiskSignalSpec) {
  if (spec.bool) return value >= 1 ? "是" : "否";
  const text = Number.isInteger(value) ? value.toLocaleString("zh-CN") : String(Number(value.toFixed(2)));
  return spec.unit ? `${text} ${spec.unit}` : text;
}

/** 一条规则的条件读成一句话：「同设备今日账号 > 3 个」 */
export function riskConditionText(
  rule: { signal: string; op: LotteryRiskOp; threshold: number },
  catalog?: LotteryCatalog | null
) {
  const spec = riskSignalOf(rule.signal, catalog);
  if (spec.bool) return spec.label;
  return `${spec.label} ${riskOpSymbol(rule.op)} ${riskValueText(rule.threshold, spec)}`;
}

/** 处置的参数：「×0.5」「公示低于 1%」 */
export function riskActionDetail(rule: { action: LotteryRiskAction; factor?: number; rareBelow?: number }) {
  if (rule.action === "damp" && rule.factor) return `×${Number(rule.factor.toFixed(3))}`;
  if (rule.action === "rare" && rule.rareBelow) return `公示低于 ${Number((rule.rareBelow * 100).toFixed(3))}%`;
  return "";
}

/** 取值相对阈值的位置（0–1）：用在方案里的读数条上，阈值固定在 2/3 处 */
export function riskGauge(check: LotteryRiskCheck) {
  const span = Math.max(check.threshold * 1.5, check.value, 1);
  return { value: Math.min(1, check.value / span), threshold: Math.min(1, check.threshold / span) };
}
