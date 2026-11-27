"use client";

import {
  BadgeCheck,
  CalendarClock,
  Clock3,
  Coins,
  Crown,
  Dices,
  Filter,
  Gauge,
  Gift,
  GitBranch,
  Hand,
  Hash,
  Infinity as InfinityIcon,
  KeyRound,
  type LucideIcon,
  Mail,
  MonitorSmartphone,
  Package,
  PackageOpen,
  Shirt,
  Shuffle,
  Sparkles,
  Square,
  Ticket,
  TicketPercent,
  Timer,
  TrendingUp,
  Users,
  Wallet,
  Zap
} from "lucide-react";
import type { Tone } from "@/components/users/detail/kit";
import type {
  ItemGrantAudienceData,
  ItemGrantCatalog,
  ItemGrantConditionData,
  ItemGrantConditionRule,
  ItemGrantGrantData,
  ItemGrantItem,
  ItemGrantLimitData,
  ItemGrantNodeType,
  ItemGrantNotifyData,
  ItemGrantRandomData,
  ItemGrantRecordStatus,
  ItemGrantRunStatus,
  ItemGrantSplitData,
  ItemGrantStatus,
  ItemGrantTriggerData,
  ItemGrantTriggerKind,
  ItemGrantWindowData
} from "@/lib/api/item-grant";
import { numberTicketName } from "@/lib/api/user-number";
import { cosmeticRewardText } from "@/lib/cosmetic-names";
import { couponRewardText } from "@/lib/coupon-names";

/**
 * 物品发放的展示元数据：节点的图标与色调、状态徽标、物品图标，以及节点在画布上的一句话摘要。
 * 文案口径与后端目录一致（目录给名称与单位，这里只负责把配置读成人话）。
 */

export type NodeVisual = {
  icon: LucideIcon;
  tone: Tone;
  /** 画布节点顶部色条与小地图的颜色 */
  accent: string;
};

export const NODE_VISUAL: Record<ItemGrantNodeType, NodeVisual> = {
  trigger: { icon: Zap, tone: "primary", accent: "#6366f1" },
  audience: { icon: Users, tone: "info", accent: "#0ea5e9" },
  condition: { icon: Filter, tone: "warning", accent: "#f59e0b" },
  window: { icon: Clock3, tone: "warning", accent: "#eab308" },
  split: { icon: Shuffle, tone: "violet", accent: "#8b5cf6" },
  limit: { icon: Gauge, tone: "violet", accent: "#a855f7" },
  grant: { icon: Gift, tone: "success", accent: "#10b981" },
  random: { icon: PackageOpen, tone: "success", accent: "#14b8a6" },
  notify: { icon: Mail, tone: "info", accent: "#3b82f6" },
  end: { icon: Square, tone: "neutral", accent: "#94a3b8" }
};

export const TRIGGER_ICON: Record<ItemGrantTriggerKind, LucideIcon> = {
  manual: Hand,
  schedule: CalendarClock,
  event: Zap
};

export const TRIGGER_LABEL: Record<ItemGrantTriggerKind, string> = {
  manual: "手动执行",
  schedule: "定时执行",
  event: "事件触发"
};

export const STATUS_META: Record<ItemGrantStatus, { label: string; variant: "success" | "secondary" | "warning" | "outline" }> = {
  draft: { label: "草稿", variant: "secondary" },
  active: { label: "生效中", variant: "success" },
  paused: { label: "已暂停", variant: "warning" },
  archived: { label: "已归档", variant: "outline" }
};

export const RUN_STATUS_META: Record<ItemGrantRunStatus, { label: string; variant: "info" | "success" | "danger" | "secondary" }> = {
  running: { label: "执行中", variant: "info" },
  succeeded: { label: "已完成", variant: "success" },
  failed: { label: "失败", variant: "danger" },
  cancelled: { label: "已取消", variant: "secondary" }
};

export const RECORD_STATUS_META: Record<
  ItemGrantRecordStatus,
  { label: string; variant: "success" | "warning" | "info" | "danger" }
> = {
  granted: { label: "已到账", variant: "success" },
  pending: { label: "待人工发放", variant: "warning" },
  fulfilled: { label: "已人工发放", variant: "info" },
  failed: { label: "发放失败", variant: "danger" }
};

export const ITEM_ICON: Record<string, LucideIcon> = {
  points: Coins,
  experience: TrendingUp,
  vip_days: Crown,
  vip_plan: BadgeCheck,
  vip_lifetime: InfinityIcon,
  feature: KeyRound,
  feature_time: Timer,
  ad_free: Sparkles,
  lottery_draws: Ticket,
  ai_credit: Sparkles,
  balance: Wallet,
  license_days: CalendarClock,
  device_slots: MonitorSmartphone,
  manual: Package,
  number_change: Hash,
  number_ticket: BadgeCheck,
  number_draw: Dices,
  number_assign: Hash,
  cosmetic: Shirt,
  coupon: TicketPercent
};

export const HANDLE_TONE: Record<string, { stroke: string; text: string; dot: string }> = {
  positive: { stroke: "#10b981", text: "text-emerald-600 dark:text-emerald-400", dot: "bg-emerald-500" },
  negative: { stroke: "#f43f5e", text: "text-rose-600 dark:text-rose-400", dot: "bg-rose-500" },
  neutral: { stroke: "#94a3b8", text: "text-muted-foreground", dot: "bg-slate-400" },
  branch: { stroke: "#8b5cf6", text: "text-violet-600 dark:text-violet-400", dot: "bg-violet-500" }
};

export const WEEKDAYS = ["一", "二", "三", "四", "五", "六", "日"];

export function minuteLabel(minute: number) {
  const value = Math.max(0, Math.min(1440, Math.round(minute)));
  const hours = Math.floor(value / 60);
  const minutes = value % 60;
  return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}`;
}

export function parseMinute(text: string) {
  const [hours, minutes] = text.split(":").map((part) => Number(part));
  if (!Number.isFinite(hours) || !Number.isFinite(minutes)) return 0;
  return Math.max(0, Math.min(1440, hours * 60 + minutes));
}

export function termLabel(days: number, permanent = false) {
  if (permanent) return "永久";
  if (days > 0 && days % 365 === 0) return `${days / 365} 年`;
  if (days > 0 && days % 30 === 0) return `${days / 30} 个月`;
  return `${days} 天`;
}

export function formatNumber(value?: number | null) {
  if (value === undefined || value === null || Number.isNaN(value)) return "—";
  return value.toLocaleString("zh-CN");
}

/** 一项物品的一句话：与后端 DescribeItem 同一口径。 */
export function describeItem(item: ItemGrantItem, catalog?: ItemGrantCatalog) {
  const spec = catalog?.items.find((candidate) => candidate.type === item.type);
  if (!spec) return item.type;
  const featureName = (tag?: string) => catalog?.features.find((feature) => feature.tag === tag)?.name || tag || "未选择功能";
  const expr = item.amountExpr ? "（按人计算）" : "";
  switch (spec.value) {
    case "":
      return spec.label;
    case "money":
      return `${spec.label} ${item.money || "0"} 元`;
    case "ref": {
      const plan = catalog?.plans.find((candidate) => candidate.id === item.refId);
      return plan ? `${plan.name}（${plan.lifetime ? "永久" : termLabel(plan.durationDays)}）` : "未选择套餐";
    }
    case "feature":
      return `${featureName(item.tag)} ${termLabel(item.amount ?? 0, (item.amount ?? 0) === 0)}`;
    case "feature_minutes":
      return `${featureName(item.tag)} ${item.amount ?? 0} 分钟${expr}`;
    case "text":
      if (item.type === "number_assign") return item.text ? `指定号码 ${item.text}` : "未填写号码";
      return item.text || "未填写";
    case "number_ticket":
      return `${numberTicketName(item.refId)} ×${formatNumber(item.amount ?? 0)}${expr}`;
    case "cosmetic":
      return `${cosmeticRewardText(item.refId, item.amount)}${item.equip ? "（发放时佩戴）" : ""}`;
    case "coupon":
      return couponRewardText(item.refId, item.amount);
  }
  switch (item.type) {
    case "vip_days":
      return `会员 ${termLabel(item.amount ?? 0)}${expr}`;
    case "ad_free":
      return `免广告 ${termLabel(item.amount ?? 0)}${expr}`;
    case "license_days":
      return `授权 ${termLabel(item.amount ?? 0)}${expr}`;
  }
  return `${spec.label} ${formatNumber(item.amount ?? 0)} ${spec.unit ?? ""}${expr}`.trim();
}

/** 可视化条件拼成表达式：与后端 RulesExpr 同一份规则（只用于展示）。 */
export function rulesExpr(mode: string, rules: ItemGrantConditionRule[]) {
  const parts = rules.map((rule) => {
    const field = rule.field.startsWith("event.") ? `(${rule.field} ?? 0)` : rule.field;
    const value = (index: number) => String(rule.value?.[index] ?? 0);
    switch (rule.op) {
      case "eq":
        return `${field} == ${value(0)}`;
      case "ne":
        return `${field} != ${value(0)}`;
      case "gt":
        return `${field} > ${value(0)}`;
      case "gte":
        return `${field} >= ${value(0)}`;
      case "lt":
        return `${field} < ${value(0)}`;
      case "lte":
        return `${field} <= ${value(0)}`;
      case "between":
        return `(${field} >= ${value(0)} && ${field} <= ${value(1)})`;
      case "in":
        return `${field} in [${(rule.value ?? []).join(", ")}]`;
      case "true":
        return `${field} == true`;
      case "false":
        return `${field} == false`;
    }
    return "";
  });
  return parts.filter(Boolean).join(mode === "any" ? " || " : " && ");
}

function ruleText(rule: ItemGrantConditionRule, catalog?: ItemGrantCatalog) {
  const field = catalog?.fields.find((candidate) => candidate.key === rule.field);
  const op = catalog?.operators.find((candidate) => candidate.op === rule.op);
  const name = field?.label ?? rule.field;
  const unit = field?.unit ? ` ${field.unit}` : "";
  if (!op) return name;
  if (op.arity === 0) return `${name}${op.label}`;
  if (op.arity === 2) return `${name} ${op.label} ${rule.value?.[0] ?? 0}–${rule.value?.[1] ?? 0}${unit}`;
  if (op.arity < 0) return `${name} ${op.label} ${(rule.value ?? []).join("、")}`;
  return `${name} ${op.label} ${rule.value?.[0] ?? 0}${unit}`;
}

/** 节点在画布上的摘要（几行短句）。 */
export function summarizeNode(type: ItemGrantNodeType, data: Record<string, unknown>, catalog?: ItemGrantCatalog): string[] {
  switch (type) {
    case "trigger": {
      const cfg = data as ItemGrantTriggerData;
      if (cfg.kind === "schedule") return ["定时执行", cfg.cron || "未设置 Cron"];
      if (cfg.kind === "event") {
        const event = catalog?.events.find((candidate) => candidate.key === cfg.event);
        return ["事件触发", event?.label ?? "未选择事件"];
      }
      return ["手动执行"];
    }
    case "audience": {
      const cfg = data as ItemGrantAudienceData;
      const lines = [catalog?.scopes.find((scope) => scope.key === cfg.scope)?.label ?? "全部用户"];
      if (cfg.registeredWithinDays) lines.push(`注册 ≤ ${cfg.registeredWithinDays} 天`);
      if (cfg.registeredBeforeDays) lines.push(`注册满 ${cfg.registeredBeforeDays} 天`);
      if (cfg.activeWithinDays) lines.push(`${cfg.activeWithinDays} 天内活跃`);
      if (cfg.minLevel || cfg.maxLevel) lines.push(`等级 ${cfg.minLevel || 0}–${cfg.maxLevel || "∞"}`);
      if (cfg.userIds?.length) lines.push(`名单 ${cfg.userIds.length} 人`);
      if (cfg.excludeUserIds?.length) lines.push(`排除 ${cfg.excludeUserIds.length} 人`);
      if (cfg.tierIds?.length) {
        const names = cfg.tierIds.map((id) => catalog?.tiers?.find((tier) => tier.id === id)?.name ?? `档位 #${id}`);
        lines.push(`${cfg.tierLifetimeOnly ? "永久持有" : "持有"} ${names.join("、")}`);
      }
      return lines;
    }
    case "condition": {
      const cfg = data as ItemGrantConditionData;
      if (cfg.mode === "expr") return [cfg.expr ? cfg.expr : "未填写表达式"];
      const rules = cfg.rules ?? [];
      if (rules.length === 0) return ["未设置条件"];
      const joiner = cfg.mode === "any" ? "任一满足" : "全部满足";
      return [joiner, ...rules.slice(0, 3).map((rule) => ruleText(rule, catalog)), ...(rules.length > 3 ? [`另 ${rules.length - 3} 条`] : [])];
    }
    case "window": {
      const cfg = data as ItemGrantWindowData;
      const lines: string[] = [];
      if (cfg.weekdays?.length) lines.push(`每周${cfg.weekdays.map((day) => WEEKDAYS[day - 1]).join("、")}`);
      if (cfg.startMinute !== cfg.endMinute) lines.push(`${minuteLabel(cfg.startMinute)}–${minuteLabel(cfg.endMinute)}`);
      if (cfg.dateFrom || cfg.dateTo) lines.push(`${cfg.dateFrom || "不限"} 至 ${cfg.dateTo || "不限"}`);
      return lines.length ? lines : ["全天"];
    }
    case "split": {
      const cfg = data as ItemGrantSplitData;
      const total = (cfg.branches ?? []).reduce((sum, branch) => sum + Math.max(0, branch.weight), 0) || 1;
      return [cfg.sticky ? "固定分配" : "每次随机"].concat(
        (cfg.branches ?? []).map((branch) => `${branch.label || branch.id} ${Math.round((branch.weight / total) * 100)}%`)
      );
    }
    case "limit": {
      const cfg = data as ItemGrantLimitData;
      const period = catalog?.periods.find((item) => item.key === cfg.period)?.label ?? "累计";
      const parts = [];
      if (cfg.perUser) parts.push(`每人 ${cfg.perUser} 次`);
      if (cfg.global) parts.push(`全站 ${formatNumber(cfg.global)} 人次`);
      return [period, parts.join("，") || "未设置"];
    }
    case "grant": {
      const cfg = data as ItemGrantGrantData;
      const items = cfg.items ?? [];
      if (items.length === 0) return ["未配置物品"];
      return items.slice(0, 4).map((item) => describeItem(item, catalog)).concat(items.length > 4 ? [`另 ${items.length - 4} 项`] : []);
    }
    case "random": {
      const cfg = data as ItemGrantRandomData;
      const options = cfg.options ?? [];
      const total = options.reduce((sum, option) => sum + Math.max(0, option.weight), 0) || 1;
      return [`抽 ${cfg.picks || 1} 个 / 共 ${options.length} 个`].concat(
        options.slice(0, 4).map((option) => `${option.label || option.id} ${((option.weight / total) * 100).toFixed(option.weight / total < 0.1 ? 1 : 0)}%`)
      );
    }
    case "notify": {
      const cfg = data as ItemGrantNotifyData;
      return [cfg.title || "未填写标题"];
    }
    case "end":
      return ["结束"];
  }
}

/** 节点的输出端口：随机分流按分支，其余按目录。 */
export function nodeOutputs(type: ItemGrantNodeType, data: Record<string, unknown>, catalog?: ItemGrantCatalog) {
  if (type === "split") {
    return ((data as ItemGrantSplitData).branches ?? []).map((branch) => ({
      id: branch.id,
      label: branch.label || branch.id,
      tone: "branch" as const
    }));
  }
  return catalog?.nodes.find((spec) => spec.type === type)?.outputs ?? [];
}
