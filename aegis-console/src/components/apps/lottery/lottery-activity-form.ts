import { toLocalInput } from "@/components/apps/points-store/points-store-shared";
import {
  STRATEGY_LIMITS,
  seedStrategy,
  strategyPayload,
  validateStrategy,
  type StrategyDraft
} from "@/components/apps/lottery/lottery-strategy-form";
import {
  PERK_LIMITS,
  perksPayload,
  seedPerks,
  validatePerks,
  type PerkDraft
} from "@/components/apps/lottery/lottery-perks-form";
import type {
  LotteryActivityPayload,
  LotteryAdminActivity,
  LotteryCatalog,
  LotteryLifetimePolicy,
  LotteryReward,
  LotteryRewardSpec,
  LotteryStatus,
  LotteryUIMode
} from "@/lib/api/lottery";

/**
 * 活动编辑器的草稿模型：种子、载荷与校验。
 *
 * 校验照抄后端 `lottery.ValidateInput` 的规则与说法，目的是在保存前就能看到「还差什么」；
 * 它只是提示，最终以后端为准（40000 的原因原样 toast）。上界常量与后端 `lottery/types.go` 一致。
 */

export const LIMITS = {
  name: 32,
  description: 200,
  rules: 2000,
  prizeName: 16,
  freeDaily: 100,
  pointsDaily: 1000,
  pointsCost: 1_000_000,
  weight: 1_000_000,
  quantity: 100_000_000,
  pity: 1000,
  note: 500,
  lifetimePointsPerDay: 100_000,
  lifetimePointsLifetime: 10_000_000,
  lifetimeDraws: 100
} as const;

/** 已拥有奖品处理的默认值（与后端 `lottery/lifetime.go` 一致；目录下发时以目录为准）。每天 / 永久积分为 0 即跟随应用的会员折算策略 */
export const LIFETIME_DEFAULTS = {
  policy: "points" as LotteryLifetimePolicy,
  pointsPerDay: 0,
  pointsLifetime: 0,
  draws: 1
};

/** 应用会员折算策略的默认单价（与后端 `vip.DefaultConversionPolicy` 一致；目录下发时以目录为准） */
export const CONVERSION_RATES = { perVipDay: 10, perFeatureDay: 5, perFeatureHour: 10, perLifetime: 3650 };

/** 可能「已拥有」的奖品类型：只有它们能设置改发（目录未下发时的回落，与后端 `lottery.OwnableKinds` 一致） */
export const OWNABLE_KINDS = ["vip_days", "vip_plan", "feature", "vip_lifetime", "feature_time", "ad_free"];
/** 改发可选的类型（目录未下发时的回落） */
export const SWAP_KINDS = ["points", "experience", "lottery_draws"];

/** 一个奖品的草稿。数字一律按字符串存：存 number 的输入框清不空。 */
export type PrizeDraft = {
  /** 草稿内的行键，与后端 id 无关 */
  key: number;
  /** 后端奖品 id，0 为新建；更新时带回去才能保留已抽中数量 */
  id: number;
  name: string;
  type: string;
  amount: string;
  refId: number;
  tag: string;
  text: string;
  imageUrl: string;
  unlimited: boolean;
  quantity: string;
  /** 已抽中数量（只读） */
  used: number;
  weight: string;
  fallback: boolean;
  /** 已拥有时改发的类型；空为按活动的处理方式 */
  swapType: string;
  swapAmount: string;
  /** 全站每日限量 / 每人累计上限 / 每人每日上限，0 为不限 */
  dailyLimit: string;
  userLimit: string;
  userDailyLimit: string;
  /** 积分当量，0 为按类型估算 */
  value: string;
};

export type ActivityForm = {
  name: string;
  description: string;
  rules: string;
  uiMode: LotteryUIMode;
  status: LotteryStatus;
  startTime: string;
  endTime: string;
  freeDaily: string;
  pointsCost: string;
  pointsDaily: string;
  videoScene: string;
  pity: boolean;
  pityThreshold: string;
  /** 保底奖的行键：按行键记，奖品挪位置时保底跟着奖品走 */
  pityKey: number | null;
  lifetimePolicy: LotteryLifetimePolicy;
  lifetimePointsPerDay: string;
  lifetimePointsLifetime: string;
  lifetimeDraws: string;
  /** 多维度出奖策略 */
  strategy: StrategyDraft;
  /** 人群特权 */
  perks: PerkDraft[];
  sortOrder: string;
  /** 下标即位置：九宫格为外圈顺时针 0–7，转盘为从顶部起顺时针 */
  prizes: PrizeDraft[];
};

export type FormIssue = { scope: "basic" | "prizes" | "strategy" | "perks"; message: string };

export const nextPrizeKey = (prizes: PrizeDraft[]) => prizes.reduce((max, prize) => Math.max(max, prize.key), 0) + 1;

export function blankPrize(key: number, type: string, amount: number, weight: number): PrizeDraft {
  return {
    key,
    id: 0,
    name: "",
    type,
    amount: String(amount),
    refId: 0,
    tag: "",
    text: "",
    imageUrl: "",
    unlimited: true,
    quantity: "100",
    used: 0,
    weight: String(weight),
    fallback: false,
    swapType: "",
    swapAmount: "",
    dailyLimit: "0",
    userLimit: "0",
    userDailyLimit: "0",
    value: "0"
  };
}

/** 新活动的默认奖盘：8 格、权重合计 100，全部不限库存，「谢谢参与」作兜底 */
function defaultPrizes(): PrizeDraft[] {
  const rows: Array<[string, number, number]> = [
    ["points", 5, 20],
    ["none", 0, 30],
    ["points", 10, 15],
    ["experience", 20, 10],
    ["points", 50, 3],
    ["lottery_draws", 1, 7],
    ["points", 20, 5],
    ["experience", 50, 10]
  ];
  return rows.map(([type, amount, weight], index) => ({
    ...blankPrize(index + 1, type, amount, weight),
    fallback: type === "none"
  }));
}

export function seedForm(activity?: LotteryAdminActivity | null, catalog?: LotteryCatalog | null): ActivityForm {
  const defaults = catalog?.lifetimeDefaults ?? LIFETIME_DEFAULTS;
  if (!activity) {
    return {
      name: "",
      description: "",
      rules: "",
      uiMode: "grid",
      status: "draft",
      startTime: "",
      endTime: "",
      freeDaily: "1",
      pointsCost: "0",
      pointsDaily: "0",
      videoScene: "",
      pity: false,
      pityThreshold: "10",
      pityKey: null,
      lifetimePolicy: defaults.policy,
      lifetimePointsPerDay: String(defaults.pointsPerDay),
      lifetimePointsLifetime: String(defaults.pointsLifetime),
      lifetimeDraws: String(defaults.draws),
      strategy: seedStrategy(null),
      perks: [],
      sortOrder: "0",
      prizes: defaultPrizes()
    };
  }
  const prizes = [...activity.prizes]
    .sort((a, b) => a.position - b.position)
    .map<PrizeDraft>((prize, index) => ({
      key: index + 1,
      id: prize.id,
      name: prize.name,
      type: prize.reward?.type || prize.kind,
      amount: String(prize.reward?.amount ?? 0),
      refId: prize.reward?.refId ?? 0,
      tag: prize.reward?.tag ?? "",
      text: prize.reward?.text ?? "",
      imageUrl: prize.imageUrl ?? "",
      unlimited: prize.quantity < 0,
      quantity: String(prize.quantity < 0 ? Math.max(prize.used, 100) : prize.quantity),
      used: prize.used,
      weight: String(prize.weight),
      fallback: prize.fallback,
      swapType: prize.lifetimeReward?.type ?? "",
      swapAmount: prize.lifetimeReward?.type ? String(prize.lifetimeReward.amount ?? 0) : "",
      dailyLimit: String(prize.dailyLimit ?? 0),
      userLimit: String(prize.userLimit ?? 0),
      userDailyLimit: String(prize.userDailyLimit ?? 0),
      value: String(prize.value ?? 0)
    }));
  const pityPrize = activity.prizes.find((prize) => prize.id === activity.pityPrizeId);
  const pityKey = pityPrize ? prizes.find((draft) => draft.id === pityPrize.id)?.key ?? null : null;
  return {
    name: activity.name,
    description: activity.description ?? "",
    rules: activity.rules ?? "",
    uiMode: activity.uiMode,
    status: activity.status,
    startTime: toLocalInput(activity.startTime),
    endTime: toLocalInput(activity.endTime),
    freeDaily: String(activity.freeDaily),
    pointsCost: String(activity.pointsCost),
    pointsDaily: String(activity.pointsDaily),
    videoScene: activity.videoScene ?? "",
    pity: activity.pityThreshold > 0,
    pityThreshold: String(activity.pityThreshold > 0 ? activity.pityThreshold : 10),
    pityKey,
    // 迁移前的老数据没有这几项：回落默认
    lifetimePolicy: activity.lifetimePolicy || defaults.policy,
    lifetimePointsPerDay: String(activity.lifetimePointsPerDay ?? defaults.pointsPerDay),
    lifetimePointsLifetime: String(activity.lifetimePointsLifetime ?? defaults.pointsLifetime),
    lifetimeDraws: String(activity.lifetimeDraws || defaults.draws),
    strategy: seedStrategy(activity.strategy),
    perks: seedPerks(activity.perks),
    sortOrder: String(activity.sortOrder),
    prizes
  };
}

const toInt = (value: string) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? Math.trunc(parsed) : 0;
};

/** 草稿 → 奖品发的东西：只带取值形态需要的字段 */
export function rewardOf(prize: PrizeDraft, kinds: LotteryRewardSpec[]): LotteryReward {
  const spec = kinds.find((item) => item.type === prize.type);
  switch (spec?.value) {
    case "":
      return { type: prize.type };
    case "ref":
      return { type: prize.type, refId: prize.refId };
    case "number_ticket":
      return { type: prize.type, amount: toInt(prize.amount), refId: prize.refId >= 1 && prize.refId <= 4 ? prize.refId : 1 };
    case "feature":
    case "feature_minutes":
      return { type: prize.type, tag: prize.tag, amount: toInt(prize.amount) };
    case "cosmetic":
      return { type: prize.type, refId: prize.refId, amount: toInt(prize.amount) };
    case "text":
      return { type: prize.type, text: prize.text.trim() };
    default:
      return prize.type === "none" ? { type: "none" } : { type: prize.type, amount: toInt(prize.amount) };
  }
}

/** 草稿 → 已拥有时改发什么；非会员 / 功能奖品或未设置时为 null */
export function swapRewardOf(prize: PrizeDraft, ownable: string[] = OWNABLE_KINDS): LotteryReward | null {
  if (!ownable.includes(prize.type) || !prize.swapType) return null;
  return { type: prize.swapType, amount: toInt(prize.swapAmount) };
}

/** 按秒折算、向上取整（与后端 `vip.perSeconds` 同口径） */
function perSeconds(seconds: number, rate: number, unit: number): number {
  if (seconds <= 0 || rate <= 0) return 0;
  return Math.ceil((seconds * rate) / unit);
}

/** 已拥有的奖品折多少积分（与后端 `lottery.OwnedItem` 同口径）：活动填了单价以活动为准，否则跟随应用折算策略 */
export function ownedPoints(form: ActivityForm, prize: PrizeDraft, catalog?: LotteryCatalog | null): number {
  const rates = catalog?.conversionRates ?? CONVERSION_RATES;
  const perDay = toInt(form.lifetimePointsPerDay);
  const lifetime = toInt(form.lifetimePointsLifetime) || rates.perLifetime;
  const vipDay = perDay || rates.perVipDay;
  const featureDay = perDay || rates.perFeatureDay;
  const amount = toInt(prize.amount);
  switch (prize.type) {
    case "vip_days":
      return perSeconds(amount * 86400, vipDay, 86400);
    case "vip_lifetime":
      return lifetime;
    case "vip_plan": {
      const plan = catalog?.plans.find((item) => item.id === prize.refId);
      if (plan?.lifetime) return lifetime;
      return perSeconds((plan?.durationDays ?? 0) * 86400, vipDay, 86400);
    }
    case "feature":
      return amount === 0 ? lifetime : perSeconds(amount * 86400, featureDay, 86400);
    case "feature_time":
      // 功能时长始终按应用每小时单价
      return perSeconds(amount * 60, rates.perFeatureHour, 3600);
    case "ad_free":
      return perSeconds(amount * 86400, featureDay, 86400);
  }
  return 0;
}

const SWAP_UNIT: Record<string, string> = { points: "积分", experience: "经验", lottery_draws: "次抽奖" };

/** 这一格被已拥有的人抽中时改发什么（一句话）；不改发时为 null */
export function swapPreview(form: ActivityForm, prize: PrizeDraft, catalog?: LotteryCatalog | null): string | null {
  const ownable = catalog?.ownableKinds ?? OWNABLE_KINDS;
  if (!ownable.includes(prize.type)) return null;
  const own = swapRewardOf(prize, ownable);
  if (form.lifetimePolicy === "grant") return null;
  if (own) return `${own.amount ?? 0} ${SWAP_UNIT[own.type] ?? ""}`.trim();
  if (form.lifetimePolicy === "draws") return `${toInt(form.lifetimeDraws)} 次抽奖`;
  const points = ownedPoints(form, prize, catalog);
  return points > 0 ? `${Math.min(points, 100_000_000)} 积分` : null;
}

/** 概率与库存口径的数值形态（与列表上的奖品同形），供公示概率计算 */
export function weightedOf(prize: PrizeDraft) {
  return {
    weight: Math.max(0, toInt(prize.weight)),
    quantity: prize.unlimited ? -1 : Math.max(0, toInt(prize.quantity)),
    used: prize.used
  };
}

export function toPayload(
  form: ActivityForm,
  kinds: LotteryRewardSpec[],
  catalog?: LotteryCatalog | null
): LotteryActivityPayload {
  const pityIndex = form.pity ? form.prizes.findIndex((prize) => prize.key === form.pityKey) : -1;
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    rules: form.rules.trim(),
    uiMode: form.uiMode,
    status: form.status,
    startTime: form.startTime ? new Date(form.startTime).toISOString() : null,
    endTime: form.endTime ? new Date(form.endTime).toISOString() : null,
    freeDaily: toInt(form.freeDaily),
    pointsCost: toInt(form.pointsCost),
    pointsDaily: toInt(form.pointsDaily),
    videoScene: form.videoScene,
    pityThreshold: form.pity ? toInt(form.pityThreshold) : 0,
    pityPosition: pityIndex >= 0 ? pityIndex : null,
    lifetimePolicy: form.lifetimePolicy,
    lifetimePointsPerDay: toInt(form.lifetimePointsPerDay),
    lifetimePointsLifetime: toInt(form.lifetimePointsLifetime),
    lifetimeDraws: toInt(form.lifetimeDraws),
    strategy: strategyPayload(form.strategy, catalog),
    perks: perksPayload(form.perks),
    sortOrder: toInt(form.sortOrder),
    prizes: form.prizes.map((prize, index) => {
      const weighted = weightedOf(prize);
      return {
        id: prize.id,
        name: prize.name.trim(),
        reward: rewardOf(prize, kinds),
        imageUrl: prize.imageUrl.trim(),
        quantity: weighted.quantity,
        weight: weighted.weight,
        position: index,
        fallback: prize.fallback,
        lifetimeReward: swapRewardOf(prize, catalog?.ownableKinds ?? OWNABLE_KINDS),
        dailyLimit: Math.max(0, toInt(prize.dailyLimit)),
        userLimit: Math.max(0, toInt(prize.userLimit)),
        userDailyLimit: Math.max(0, toInt(prize.userDailyLimit)),
        value: Math.max(0, toInt(prize.value))
      };
    })
  };
}

const runes = (value: string) => Array.from(value).length;

/** 与后端 `ValidateInput` / `validatePrizes` 同一套规则与说法 */
export function validateForm(form: ActivityForm, catalog?: LotteryCatalog | null): FormIssue[] {
  const issues: FormIssue[] = [];
  const basic = (message: string) => issues.push({ scope: "basic", message });
  const prizes = (message: string) => issues.push({ scope: "prizes", message });

  const name = form.name.trim();
  if (!name) basic("活动名称不能为空");
  else if (runes(name) > LIMITS.name) basic(`活动名称不能超过 ${LIMITS.name} 字`);
  if (runes(form.description.trim()) > LIMITS.description) basic(`活动简介不能超过 ${LIMITS.description} 字`);
  if (runes(form.rules.trim()) > LIMITS.rules) basic(`活动规则不能超过 ${LIMITS.rules} 字`);
  if (form.startTime && form.endTime && new Date(form.endTime).getTime() <= new Date(form.startTime).getTime()) {
    basic("结束时间需要晚于开始时间");
  }
  const freeDaily = toInt(form.freeDaily);
  if (freeDaily < 0 || freeDaily > LIMITS.freeDaily) basic(`每日免费次数需要在 0–${LIMITS.freeDaily} 之间`);
  const pointsCost = toInt(form.pointsCost);
  if (pointsCost < 0 || pointsCost > LIMITS.pointsCost) basic(`每次消耗积分需要在 0–${LIMITS.pointsCost} 之间`);
  const pointsDaily = toInt(form.pointsDaily);
  if (pointsDaily < 0 || pointsDaily > LIMITS.pointsDaily) basic(`积分抽奖每日上限需要在 0–${LIMITS.pointsDaily} 之间`);

  const gridCount = catalog?.gridPrizeCount ?? 8;
  const wheelMin = catalog?.wheelMinPrizes ?? 2;
  const wheelMax = catalog?.wheelMaxPrizes ?? 12;
  const count = form.prizes.length;
  if (form.uiMode === "grid" && count !== gridCount) {
    prizes(`九宫格需要恰好 ${gridCount} 个奖品，当前 ${count} 个`);
  }
  if (form.uiMode === "wheel" && (count < wheelMin || count > wheelMax)) {
    prizes(`转盘需要 ${wheelMin}–${wheelMax} 个奖品，当前 ${count} 个`);
  }

  const kinds = catalog?.kinds ?? [];
  let totalWeight = 0;
  let unlimited = false;
  let fallbacks = 0;
  form.prizes.forEach((prize, index) => {
    const label = `第 ${index + 1} 个奖品`;
    if (runes(prize.name.trim()) > LIMITS.prizeName) prizes(`${label}的名称不能超过 ${LIMITS.prizeName} 字`);
    const image = prize.imageUrl.trim();
    if (image && !/^https?:\/\//.test(image)) prizes(`${label}的图片地址需以 http(s):// 开头`);

    const spec = kinds.find((item) => item.type === prize.type);
    if (catalog && !spec) prizes(`${label}：未登记的奖品类型：${prize.type}`);
    if (spec?.value === "amount") {
      const amount = toInt(prize.amount);
      if ((spec.min !== undefined && amount < spec.min) || (spec.max !== undefined && amount > spec.max)) {
        prizes(`${label}：${spec.label}需要在 ${spec.min ?? 0}–${spec.max ?? amount} 之间`);
      }
    }
    if (spec?.value === "ref" && !prize.refId) prizes(`${label}：请选择会员套餐`);
    if (spec?.value === "number_ticket") {
      const amount = toInt(prize.amount);
      if (amount < (spec.min ?? 1) || (spec.max !== undefined && amount > spec.max)) {
        prizes(`${label}：${spec.label}需要在 ${spec.min ?? 1}–${spec.max ?? amount} 张之间`);
      }
      if (!(prize.refId >= 1 && prize.refId <= 4)) prizes(`${label}：请选择靓号券等级`);
    }
    if (spec?.value === "feature") {
      if (!prize.tag) prizes(`${label}：请选择功能`);
      if (toInt(prize.amount) < 0) prizes(`${label}：天数不能为负`);
    }
    if (spec?.value === "feature_minutes") {
      if (!prize.tag) prizes(`${label}：请选择功能`);
      const amount = toInt(prize.amount);
      if ((spec.min !== undefined && amount < spec.min) || (spec.max !== undefined && amount > spec.max)) {
        prizes(`${label}：${spec.label}需要在 ${spec.min ?? 0}–${spec.max ?? amount} ${spec.unit ?? ""}之间`);
      }
    }
    if (spec?.value === "text" && !prize.text.trim()) prizes(`${label}：请填写发放内容`);
    if (spec?.value === "cosmetic") {
      if (!prize.refId) prizes(`${label}：请选择装扮`);
      const days = toInt(prize.amount);
      if (days < 0 || (spec.max !== undefined && days > spec.max)) prizes(`${label}：装扮天数需要在 0–${spec.max ?? 3650} 之间（0 为永久）`);
    }

    const weight = toInt(prize.weight);
    if (weight < 0 || weight > LIMITS.weight) prizes(`${label}的权重需要在 0–${LIMITS.weight} 之间`);
    if (!prize.unlimited) {
      const quantity = toInt(prize.quantity);
      if (quantity > LIMITS.quantity) prizes(`${label}的库存不能超过 ${LIMITS.quantity}`);
      if (quantity < prize.used) prizes(`${label}的库存不能少于已抽中的 ${prize.used} 份`);
    }
    totalWeight += Math.max(0, weight);
    if (prize.unlimited) unlimited = true;
    const limitMax = catalog?.strategyLimits?.prizeLimit ?? STRATEGY_LIMITS.prizeLimit;
    const limits = [toInt(prize.dailyLimit), toInt(prize.userLimit), toInt(prize.userDailyLimit)];
    if (limits.some((limit) => limit < 0 || limit > limitMax)) prizes(`${label}的限量需要在 0–${limitMax} 之间（0 为不限）`);
    if (limits[1] > 0 && limits[2] > limits[1]) prizes(`${label}的每人每日上限不能大于每人累计上限`);
    if (prize.fallback) {
      fallbacks += 1;
      if (!prize.unlimited) prizes(`${label}是兜底奖，库存必须不限`);
      if (limits.some((limit) => limit > 0)) prizes(`${label}是兜底奖，不能设置限量`);
    }
  });
  if (fallbacks > 1) prizes("最多设置一个兜底奖");
  if (count > 0 && totalWeight <= 0) prizes("奖品的权重合计必须大于 0");
  if (count > 0 && !unlimited) prizes("至少需要一个不限库存的奖品（通常是「谢谢参与」）");

  const perDay = toInt(form.lifetimePointsPerDay);
  const lifetimePoints = toInt(form.lifetimePointsLifetime);
  const lifetimeDraws = toInt(form.lifetimeDraws);
  if (perDay < 0 || perDay > LIMITS.lifetimePointsPerDay) basic(`每天折算积分需要在 0–${LIMITS.lifetimePointsPerDay} 之间`);
  if (lifetimePoints < 0 || lifetimePoints > LIMITS.lifetimePointsLifetime) {
    basic(`永久奖品折算积分需要在 0–${LIMITS.lifetimePointsLifetime} 之间`);
  }
  if (lifetimeDraws < 0 || lifetimeDraws > LIMITS.lifetimeDraws) basic(`改发抽奖次数需要在 0–${LIMITS.lifetimeDraws} 之间`);
  if (form.lifetimePolicy === "draws" && lifetimeDraws === 0) basic("改发抽奖次数时次数至少为 1");
  form.prizes.forEach((prize, index) => {
    const swap = swapRewardOf(prize, catalog?.ownableKinds ?? OWNABLE_KINDS);
    if (!swap) return;
    const spec = kinds.find((item) => item.type === swap.type);
    const amount = swap.amount ?? 0;
    if (spec && ((spec.min !== undefined && amount < spec.min) || (spec.max !== undefined && amount > spec.max))) {
      prizes(`第 ${index + 1} 个奖品的改发：${spec.label}需要在 ${spec.min ?? 0}–${spec.max ?? amount} 之间`);
    }
  });

  for (const message of validateStrategy(form.strategy, count, catalog?.strategyLimits ?? STRATEGY_LIMITS)) {
    issues.push({ scope: "strategy", message });
  }
  for (const message of validatePerks(form.perks, catalog?.perkLimits ?? PERK_LIMITS)) {
    issues.push({ scope: "perks", message });
  }

  if (form.pity) {
    const threshold = toInt(form.pityThreshold);
    if (threshold < 0 || threshold > LIMITS.pity) prizes(`保底次数需要在 0–${LIMITS.pity} 之间`);
    // 开关打开时 0 不当作「关闭」：那是开关的事，这里要一个真正的次数
    else if (threshold < 2) prizes(threshold === 1 ? "保底次数至少为 2（为 1 等于每次必中保底奖）" : "保底次数至少为 2");
    const pityPrize = form.prizes.find((prize) => prize.key === form.pityKey);
    if (threshold > 0) {
      if (!pityPrize) prizes("开启保底时需要选择保底奖品");
      else if (pityPrize.type === "none") prizes("保底奖品不能是「谢谢参与」");
    }
  }
  return issues;
}
