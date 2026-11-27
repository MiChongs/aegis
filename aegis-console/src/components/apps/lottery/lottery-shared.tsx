"use client";

import { Badge } from "@/components/ui/badge";
import { cosmeticRewardText } from "@/lib/cosmetic-names";
import { couponRewardText } from "@/lib/coupon-names";
import { formatStoreDate, termLabel } from "@/components/apps/points-store/points-store-shared";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import type {
  LotteryAdminActivity,
  LotteryDrawStatus,
  LotteryFeatureRef,
  LotteryLifetimePolicy,
  LotteryPlanRef,
  LotteryReward,
  LotteryRewardSpec,
  LotterySource,
  LotteryStatus,
  LotteryUIMode
} from "@/lib/api/lottery";
import { numberTicketName } from "@/lib/api/user-number";

/**
 * 抽奖的展示口径：状态、形态、来源、奖品摘要与公示概率。集中在一处，
 * 活动列表、编辑抽屉、统计与抽奖记录说的是同一套话。
 */

const STATUS_META: Record<LotteryStatus, { label: string; variant: "success" | "warning" | "secondary" | "outline" }> = {
  draft: { label: "草稿", variant: "outline" },
  active: { label: "进行中", variant: "success" },
  paused: { label: "已暂停", variant: "warning" },
  ended: { label: "已结束", variant: "secondary" }
};

export const LOTTERY_STATUSES: LotteryStatus[] = ["draft", "active", "paused", "ended"];

export function lotteryStatusLabel(status: LotteryStatus) {
  return STATUS_META[status]?.label ?? status;
}

export function LotteryStatusBadge({ status }: { status: LotteryStatus }) {
  const meta = STATUS_META[status] ?? STATUS_META.draft;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export const UI_MODE_LABEL: Record<LotteryUIMode, string> = {
  grid: "九宫格",
  wheel: "转盘"
};

export const SOURCE_LABEL: Record<LotterySource, string> = {
  free: "免费",
  bonus: "次数",
  points: "积分"
};

/** 已拥有奖品的处理方式：目录未下发时的回落文案；short 用在活动列表上 */
export const LIFETIME_POLICY_META: Record<LotteryLifetimePolicy, { label: string; short: string; hint: string }> = {
  points: { label: "折算为积分", short: "折算积分", hint: "会员天数按每天积分折算，永久套餐与永久功能给固定积分" },
  draws: { label: "改发抽奖次数", short: "改发次数", hint: "抽中已拥有的奖品时改发若干次抽奖" },
  exclude: { label: "不参与抽取", short: "不参与抽取", hint: "对已拥有的用户剔除这些奖品，概率按剩余奖品重算；保底奖仍按积分折算" },
  grant: { label: "照常发放", short: "照常发放", hint: "仍记一段会员或功能（永久会员实际拿不到新东西）" }
};

export const LIFETIME_POLICIES: LotteryLifetimePolicy[] = ["points", "draws", "exclude", "grant"];

/** 抽奖记录上「已折算」的标记 */
export function ConversionBadge() {
  return (
    <Badge variant="info" size="sm">
      已折算
    </Badge>
  );
}

const DRAW_STATUS_META: Record<LotteryDrawStatus, { label: string; variant: "success" | "warning" | "info" }> = {
  granted: { label: "已到账", variant: "success" },
  pending: { label: "待发放", variant: "warning" },
  fulfilled: { label: "已发放", variant: "info" }
};

export const DRAW_STATUSES: LotteryDrawStatus[] = ["granted", "pending", "fulfilled"];

export function drawStatusLabel(status: LotteryDrawStatus) {
  return DRAW_STATUS_META[status]?.label ?? status;
}

export function DrawStatusBadge({ status }: { status: LotteryDrawStatus }) {
  const meta = DRAW_STATUS_META[status] ?? DRAW_STATUS_META.granted;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/**
 * 九宫格外圈的位置 → 3×3 格子里的 [行, 列]。顺时针，从左上角起：
 *
 *   0 1 2
 *   7 G 3      （G 为中心的抽奖按钮）
 *   6 5 4
 *
 * 与 App 端的跑马灯顺序一致，改了这张表两边转出来的格子就对不上。
 */
export const GRID_CELLS: ReadonlyArray<readonly [number, number]> = [
  [0, 0],
  [0, 1],
  [0, 2],
  [1, 2],
  [2, 2],
  [2, 1],
  [2, 0],
  [1, 0]
];

type WeightedPrize = { weight: number; quantity: number; used: number };

function inStock(prize: WeightedPrize) {
  return prize.quantity < 0 || prize.used < prize.quantity;
}

/**
 * 常规抽取下每个奖品的概率（百分比，不取整），按传入顺序。
 * 与后端 `lottery.Probabilities` 同一算法：分母是有库存奖品的权重合计，售罄或权重为 0 的为 0。
 * 显示用 formatProbability。
 */
export function prizeProbabilities(prizes: WeightedPrize[]) {
  const total = prizes.reduce((sum, prize) => (inStock(prize) ? sum + Math.max(0, prize.weight) : sum), 0);
  return prizes.map((prize) => {
    if (total === 0 || !inStock(prize) || prize.weight <= 0) return 0;
    return (prize.weight * 100) / total;
  });
}

/**
 * 公示概率的取整（百分比），与后端 `lottery.PublicProbability` 同一规则：
 * 不小于 1% 的保留两位小数，小于 1% 的保留两位有效数字，抽得到的不会显示成 0。
 */
export function publicProbability(value: number) {
  if (!Number.isFinite(value) || value <= 0) return 0;
  if (value >= 1) return Math.round(value * 100) / 100;
  const scale = 10 ** (1 - Math.floor(Math.log10(value)));
  return Math.round(value * scale) / scale;
}

/** 百分比显示：28%、33.33%、0.013%、0.001% */
export function formatProbability(value: number) {
  return `${publicProbability(value).toLocaleString("zh-CN", { maximumFractionDigits: 20 })}%`;
}

/** 奖品类型的中文名（目录里查不到时原样显示 type） */
export function kindLabelOf(type: string, kinds: LotteryRewardSpec[]) {
  return kinds.find((spec) => spec.type === type)?.label ?? type;
}

/** 一个奖品发什么的一句话摘要（「积分 30 点」「会员 7 天」） */
export function describeLotteryReward(
  reward: LotteryReward,
  kinds: LotteryRewardSpec[],
  plans: LotteryPlanRef[],
  features: LotteryFeatureRef[]
) {
  const spec = kinds.find((item) => item.type === reward.type);
  const label = spec?.label ?? reward.type;
  switch (spec?.value) {
    case "ref": {
      const plan = plans.find((item) => item.id === reward.refId);
      return plan ? `${plan.name}（${vipPlanTerm(plan)}）` : `${label} #${reward.refId ?? 0}`;
    }
    case "feature": {
      const feature = features.find((item) => item.tag === reward.tag);
      return `${feature?.name || reward.tag || label} ${termLabel(reward.amount ?? 0)}`;
    }
    case "feature_minutes": {
      const feature = features.find((item) => item.tag === reward.tag);
      return `${feature?.name || reward.tag || label} ${reward.amount ?? 0} 分钟`;
    }
    case "text":
      return reward.text || label;
    case "number_ticket":
      return `${numberTicketName(reward.refId)} ×${reward.amount ?? 0}`;
    case "cosmetic":
      return cosmeticRewardText(reward.refId, reward.amount);
    case "coupon":
      return couponRewardText(reward.refId, reward.amount);
    case "amount":
      return `${label} ${reward.amount ?? 0}${spec.unit ? ` ${spec.unit}` : ""}`;
    default:
      return label;
  }
}

/**
 * 奖品没起名字时后端生成的名字（`lottery.DefaultPrizeName`），用作名称输入框的占位。
 * 只是预览：最终以保存后后端给的名字为准。
 */
export function defaultPrizeName(
  reward: LotteryReward,
  kinds: LotteryRewardSpec[],
  plans: LotteryPlanRef[],
  features: LotteryFeatureRef[]
) {
  switch (reward.type) {
    case "none":
      return kinds.find((item) => item.type === "none")?.label ?? "谢谢参与";
    case "points":
      return `${reward.amount ?? 0} 积分`;
    case "experience":
      return `${reward.amount ?? 0} 经验`;
    case "lottery_draws":
      return `抽奖 ${reward.amount ?? 0} 次`;
    case "vip_days":
      return `会员 ${reward.amount ?? 0} 天`;
    case "vip_lifetime":
      return "永久会员";
    case "ad_free":
      return `免广告 ${reward.amount ?? 0} 天`;
    case "number_change":
      return `改号卡 ${reward.amount ?? 0} 张`;
    case "number_draw":
      return `摇号 ${reward.amount ?? 0} 次`;
  }
  return Array.from(describeLotteryReward(reward, kinds, plans, features)).slice(0, 16).join("");
}

/** 活动时间窗：两头都有写「起 至 止」，只有一头写「起」或「截至」，都没有为空 */
export function activityWindow(startTime?: string, endTime?: string) {
  if (startTime && endTime) return `${formatStoreDate(startTime)} 至 ${formatStoreDate(endTime)}`;
  if (startTime) return `${formatStoreDate(startTime)} 起`;
  if (endTime) return `截至 ${formatStoreDate(endTime)}`;
  return "";
}

/** 进行中的活动此刻在不在时间窗内：未开始 / 已过期，都不是则为空 */
export function activityPhase(activity: Pick<LotteryAdminActivity, "status" | "startTime" | "endTime">, now = Date.now()) {
  if (activity.status !== "active") return null;
  if (activity.startTime && new Date(activity.startTime).getTime() > now) return "未开始";
  if (activity.endTime && new Date(activity.endTime).getTime() <= now) return "已过期";
  return null;
}

export function formatCount(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}
