"use client";

import type { ComponentType } from "react";
import { BadgeCheck, CalendarPlus, Crown, Dices, Gift, HandHelping, Hash, KeyRound, Shirt, Sparkles, Ticket, Wallet } from "lucide-react";
import { useCosmeticRewardNames } from "@/components/apps/cosmetics/cosmetic-reward";
import { cosmeticRewardText } from "@/lib/cosmetic-names";
import type { Tone } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import type {
  PointsStoreAudience,
  PointsStoreItem,
  PointsStoreItemPayload,
  PointsStoreLimitPeriod,
  PointsStoreRedemptionStatus,
  PointsStoreRestockPeriod,
  PointsStoreReward,
  PointsStoreRewardSpec
} from "@/lib/api/points-store";
import { numberTicketName } from "@/lib/api/user-number";
import type { VipFeature, VipPlan } from "@/lib/api/vip";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";

/**
 * 积分商城的展示口径：周期、对象、状态、权益摘要。集中在一处，
 * 商品列表、编辑抽屉、兑换记录与用户详情说的是同一套话。
 */

export const LIMIT_PERIOD_LABEL: Record<PointsStoreLimitPeriod, string> = {
  total: "累计",
  day: "每天",
  week: "每周",
  month: "每月"
};

/** 补货周期 */
export const RESTOCK_PERIOD_LABEL: Record<PointsStoreRestockPeriod, string> = {
  none: "不补货",
  day: "每日",
  week: "每周",
  month: "每月"
};

/** 名额与限兑周期的「本期」说法：今日 / 本周 / 本月 / 累计 */
export const CURRENT_PERIOD_LABEL: Record<PointsStoreLimitPeriod, string> = {
  total: "累计",
  day: "今日",
  week: "本周",
  month: "本月"
};

export const AUDIENCE_LABEL: Record<PointsStoreAudience, string> = {
  all: "所有用户",
  vip: "仅会员",
  non_vip: "仅非会员",
  lifetime: "仅永久会员"
};

const REDEMPTION_STATUS_META: Record<
  PointsStoreRedemptionStatus,
  { label: string; variant: "success" | "warning" | "secondary" }
> = {
  completed: { label: "已到账", variant: "success" },
  pending: { label: "待发放", variant: "warning" },
  cancelled: { label: "已取消", variant: "secondary" }
};

export function RedemptionStatusBadge({ status }: { status: PointsStoreRedemptionStatus }) {
  const meta = REDEMPTION_STATUS_META[status] ?? REDEMPTION_STATUS_META.completed;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export function redemptionStatusLabel(status: PointsStoreRedemptionStatus) {
  return REDEMPTION_STATUS_META[status]?.label ?? status;
}

/** 时长：0 天的功能解锁是永久，不写成「0 天」 */
export function termLabel(days: number, permanent = false) {
  if (permanent || days <= 0) return "永久";
  if (days % 365 === 0) return `${days / 365} 年`;
  if (days % 30 === 0) return `${days / 30} 个月`;
  return `${days} 天`;
}

/** 一项权益的一句话摘要 */
export function describeReward(
  reward: PointsStoreReward,
  catalog: PointsStoreRewardSpec[],
  plans: VipPlan[],
  features: VipFeature[]
) {
  const spec = catalog.find((item) => item.type === reward.type);
  const label = spec?.label ?? reward.type;
  switch (spec?.value) {
    case "ref": {
      const plan = plans.find((item) => item.id === reward.refId);
      return plan ? `${plan.name}（${vipPlanTerm(plan)}）` : `${label} #${reward.refId}`;
    }
    case "feature": {
      const feature = features.find((item) => item.tag === reward.tag);
      return `${feature?.name || reward.tag} ${termLabel(reward.amount ?? 0)}`;
    }
    case "money":
      return `${label} ¥${reward.money}`;
    case "text":
      return reward.text ?? label;
    case "number_ticket":
      return `${numberTicketName(reward.refId)} ×${reward.amount ?? 0}`;
    case "cosmetic":
      return cosmeticRewardText(reward.refId, reward.amount);
    case "amount":
      return `${label} ${reward.amount}${spec.unit ? ` ${spec.unit}` : ""}`;
    default:
      return label;
  }
}

export function RewardBadges({
  rewards,
  catalog,
  plans,
  features
}: {
  rewards: PointsStoreReward[];
  catalog: PointsStoreRewardSpec[];
  plans: VipPlan[];
  features: VipFeature[];
}) {
  // 装扮名称来自登记表：订阅后名字晚到也会重绘
  useCosmeticRewardNames();
  return (
    <div className="flex flex-wrap gap-1">
      {rewards.map((reward, index) => (
        <Badge key={`${reward.type}-${index}`} variant="outline" size="sm" className="font-normal">
          {describeReward(reward, catalog, plans, features)}
        </Badge>
      ))}
    </div>
  );
}

/** 商品此刻的上架状态：下架 / 未开始 / 已结束 / 售罄，都不是则为空 */
export function itemShelfState(item: PointsStoreItem, now = Date.now()) {
  if (item.status !== "active") return { label: "已下架", variant: "secondary" as const };
  if (item.endsAt && new Date(item.endsAt).getTime() <= now) return { label: "已结束", variant: "secondary" as const };
  if (item.startsAt && new Date(item.startsAt).getTime() > now) return { label: "未开始", variant: "info" as const };
  if (hasStock(item) && (liveStock(item) ?? 0) <= 0) {
    return { label: "已兑完", variant: "warning" as const };
  }
  return null;
}

export function formatPoints(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}

export function formatStoreDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

/** datetime-local 输入框要的本地时间串（YYYY-MM-DDTHH:mm） */
export function toLocalInput(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** 商品此刻在用户端的状态。和 itemShelfState 同一判据，多了「在售」，供筛选与统计。 */
export type ShelfKey = "on_sale" | "scheduled" | "ended" | "sold_out" | "inactive";

export const SHELF_META: Record<ShelfKey, { label: string; tone: Tone; color: string }> = {
  on_sale: { label: "在售", tone: "success", color: "bg-emerald-500" },
  scheduled: { label: "未开始", tone: "info", color: "bg-sky-500" },
  sold_out: { label: "已兑完", tone: "warning", color: "bg-amber-500" },
  ended: { label: "已结束", tone: "neutral", color: "bg-zinc-400 dark:bg-zinc-500" },
  inactive: { label: "已下架", tone: "neutral", color: "bg-zinc-300 dark:bg-zinc-700" }
};

export const SHELF_ORDER: ShelfKey[] = ["on_sale", "scheduled", "sold_out", "ended", "inactive"];

export function shelfKey(item: PointsStoreItem, now = Date.now()): ShelfKey {
  if (item.status !== "active") return "inactive";
  if (item.endsAt && new Date(item.endsAt).getTime() <= now) return "ended";
  if (item.startsAt && new Date(item.startsAt).getTime() > now) return "scheduled";
  if (hasStock(item) && (liveStock(item) ?? 0) <= 0) return "sold_out";
  return "on_sale";
}

export function hasStock(item: Pick<PointsStoreItem, "stock">) {
  return item.stock !== undefined && item.stock !== null;
}

/** 是否开了补货周期 */
export function restocks(item: Pick<PointsStoreItem, "restockPeriod" | "restockQuantity">) {
  return Boolean(item.restockPeriod && item.restockPeriod !== "none" && item.restockQuantity);
}

/**
 * 此刻的剩余库存。开了补货周期时库里的 stock 可能还属于上个周期（补货是惰性的），
 * 管理端列表另给了换算后的 currentStock，有就用它。
 */
export function liveStock(item: PointsStoreItem & { currentStock?: number }) {
  if (!hasStock(item)) return undefined;
  return item.currentStock ?? item.stock;
}

/** 分钟数 ↔「HH:MM」 */
export function minuteToTime(minute?: number | null) {
  if (minute === undefined || minute === null) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(Math.floor(minute / 60))}:${pad(minute % 60)}`;
}

export function timeToMinute(value: string) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(value.trim());
  if (!match) return null;
  const minute = Number(match[1]) * 60 + Number(match[2]);
  return minute >= 0 && minute <= 1440 ? minute : null;
}

/** 每日开放时段「10:00–22:00」，跨零点写「22:00–次日 02:00」；全天开放为空 */
export function openHoursLabel(item: Pick<PointsStoreItem, "dailyOpenMinute" | "dailyCloseMinute">) {
  const open = item.dailyOpenMinute;
  const close = item.dailyCloseMinute;
  if (open === undefined || open === null || close === undefined || close === null) return "";
  if (close === 0 || close === 1440) return `${minuteToTime(open)}–24:00`;
  return `${minuteToTime(open)}–${close < open ? "次日 " : ""}${minuteToTime(close)}`;
}

/** 权益类型的图标与色调；未登记的类型落到礼物图标 */
const REWARD_VISUAL: Record<string, { icon: ComponentType<{ className?: string }>; tone: Tone }> = {
  vip_plan: { icon: Crown, tone: "violet" },
  vip_days: { icon: CalendarPlus, tone: "violet" },
  feature: { icon: KeyRound, tone: "info" },
  experience: { icon: Sparkles, tone: "primary" },
  balance: { icon: Wallet, tone: "success" },
  lottery_draws: { icon: Ticket, tone: "warning" },
  manual: { icon: HandHelping, tone: "danger" },
  number_change: { icon: Hash, tone: "info" },
  number_ticket: { icon: BadgeCheck, tone: "warning" },
  number_draw: { icon: Dices, tone: "violet" },
  cosmetic: { icon: Shirt, tone: "violet" }
};

export function rewardVisual(type?: string) {
  return (type && REWARD_VISUAL[type]) || { icon: Gift, tone: "neutral" as Tone };
}

/** 一件商品的限制条件，逐条短句 */
export function itemRules(item: PointsStoreItem) {
  const rules: string[] = [];
  if (restocks(item)) rules.push(`${RESTOCK_PERIOD_LABEL[item.restockPeriod]}补货至 ${formatPoints(item.restockQuantity)} 件`);
  if (item.globalLimitCount > 0) {
    rules.push(
      item.globalLimitPeriod === "total"
        ? `全站限 ${formatPoints(item.globalLimitCount)} 份`
        : `全站${LIMIT_PERIOD_LABEL[item.globalLimitPeriod]}限 ${formatPoints(item.globalLimitCount)} 份`
    );
  }
  if (item.limitCount > 0) {
    rules.push(item.limitPeriod === "total" ? `每人限兑 ${item.limitCount} 次` : `每人${LIMIT_PERIOD_LABEL[item.limitPeriod]}限兑 ${item.limitCount} 次`);
  }
  if (item.userTotalLimit > 0) rules.push(`每人累计 ${item.userTotalLimit} 次`);
  const hours = openHoursLabel(item);
  if (hours) rules.push(`每日 ${hours} 开放`);
  if (item.audience !== "all") rules.push(AUDIENCE_LABEL[item.audience]);
  if (item.minLevel > 0) rules.push(`Lv.${item.minLevel} 起`);
  return rules;
}

/** 上下架时间：两头都有写「起 至 止」，只有一头写「起」或「截至」 */
export function saleWindow(startsAt?: string, endsAt?: string) {
  if (startsAt && endsAt) return `${formatStoreDate(startsAt)} 至 ${formatStoreDate(endsAt)}`;
  if (startsAt) return `${formatStoreDate(startsAt)} 起`;
  if (endsAt) return `截至 ${formatStoreDate(endsAt)}`;
  return "";
}

/** 已有商品 → 保存载荷（复制商品用）。 */
export function itemToPayload(item: PointsStoreItem, overrides: Partial<PointsStoreItemPayload> = {}): PointsStoreItemPayload {
  return {
    title: item.title,
    subtitle: item.subtitle ?? "",
    description: item.description ?? "",
    category: item.category ?? "",
    badge: item.badge ?? "",
    cost: item.cost,
    originalCost: item.originalCost ?? null,
    rewards: item.rewards,
    stock: hasStock(item) ? (liveStock(item) ?? 0) : null,
    restockPeriod: item.restockPeriod ?? "none",
    restockQuantity: item.restockQuantity ?? null,
    globalLimitPeriod: item.globalLimitPeriod ?? "day",
    globalLimitCount: item.globalLimitCount ?? 0,
    limitPeriod: item.limitPeriod,
    limitCount: item.limitCount,
    userTotalLimit: item.userTotalLimit ?? 0,
    dailyOpenMinute: item.dailyOpenMinute ?? null,
    dailyCloseMinute: item.dailyCloseMinute ?? null,
    audience: item.audience,
    minLevel: item.minLevel,
    startsAt: item.startsAt ?? null,
    endsAt: item.endsAt ?? null,
    status: item.status,
    sortOrder: item.sortOrder,
    ...overrides
  };
}
