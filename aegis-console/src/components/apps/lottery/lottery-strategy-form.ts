import type {
  LotteryCatalog,
  LotteryRiskAction,
  LotteryRiskOp,
  LotteryRiskRule,
  LotteryStrategy,
  LotteryStrategyLimits
} from "@/lib/api/lottery";
import {
  activitySupremePayload,
  seedSupreme,
  supremeModeOf,
  validateSupreme,
  type SupremeDraft,
  type SupremeMode
} from "@/components/apps/lottery/lottery-supreme-form";

/**
 * 出奖策略的草稿模型。数字按字符串存（输入框要能清空），开关单独存：关掉一项时保留填过的数，
 * 再打开不用重填。载荷只带打开的项，与后端 `lottery.NormalizeStrategy` 的口径一致。
 * 规则见 docs/lottery.md「出奖策略」。
 */

/** 后端目录未下发时的回落上界（与 `lottery/strategy.go`、`behavior.go`、`simulate.go` 一致） */
export const STRATEGY_LIMITS: LotteryStrategyLimits = {
  rules: 12,
  ruleFactor: 100,
  softPityStep: 50,
  cooldownDraws: 100,
  pacingBoost: 5,
  behaviorBoost: 10,
  prizeLimit: 1_000_000,
  simulationUsers: 5000,
  simulationEach: 1000,
  simulationDraws: 200_000,
  luckStrength: 2,
  controlGain: 20,
  fortuneSpread: 1,
  riskRules: 16
};

export type RuleDraft = {
  key: number;
  name: string;
  when: string;
  kinds: string[];
  /** 奖盘上的位置（下标） */
  positions: number[];
  factor: string;
  disabled: boolean;
};

export type SignalDraft = { weight: string; half: string };

/** 一条风控规则的草稿（阈值、系数按字符串存；稀有线按百分比存） */
export type RiskRuleDraft = {
  key: number;
  name: string;
  signal: string;
  op: LotteryRiskOp;
  threshold: string;
  action: LotteryRiskAction;
  factor: string;
  /** 百分比 */
  rareBelow: string;
  kinds: string[];
  disabled: boolean;
};

export type StrategyDraft = {
  softPity: boolean;
  softPityStart: string;
  softPityStep: string;
  pacing: boolean;
  /** 提前量，百分比 */
  pacingSlack: string;
  pacingBoost: string;
  cooldown: boolean;
  cooldownDraws: string;
  cooldownFactor: string;
  budget: boolean;
  budgetPoints: string;
  /** 按当天时间进度放量 */
  budgetPaced: boolean;
  risk: boolean;
  riskRules: RiskRuleDraft[];
  behavior: boolean;
  behaviorMin: string;
  behaviorMax: string;
  behaviorKinds: string[];
  /** 按信号键；不在表里的信号按推荐权重。customSignals 为假时整张表不下发（用推荐权重） */
  customSignals: boolean;
  signals: Record<string, SignalDraft>;
  rules: RuleDraft[];
  luck: boolean;
  luckStrength: string;
  fortune: boolean;
  fortuneSpread: string;
  /** 本活动的至尊加成：跟随应用设置 / 本活动不加成 / 本活动单独设置（只在开了每日运气时下发） */
  supremeMode: SupremeMode;
  supreme: SupremeDraft;
  /** 伪随机分布（与软保底二选一） */
  prd: boolean;
  winRate: boolean;
  /** 百分比 */
  winRateTarget: string;
  winRateGain: string;
  winRatePrior: string;
  guard: boolean;
  /** 百分比，空为不限 */
  guardMin: string;
  guardMax: string;
  guardValue: string;
  guardExpRate: string;
  guardDrawValue: string;
  /** 奖品类型 → 单次占比上限（百分比） */
  guardCaps: Record<string, string>;
};

const num = (value: number | undefined, fallback: number) => String(value ?? fallback);

export function seedStrategy(strategy?: LotteryStrategy | null): StrategyDraft {
  const s = strategy ?? {};
  const signals: Record<string, SignalDraft> = {};
  for (const signal of s.behavior?.signals ?? []) {
    signals[signal.key] = { weight: String(signal.weight), half: signal.half ? String(signal.half) : "" };
  }
  return {
    softPity: Boolean(s.softPity),
    softPityStart: num(s.softPity?.start, 5),
    softPityStep: num(s.softPity?.step, 5),
    pacing: Boolean(s.pacing),
    pacingSlack: String(Math.round((s.pacing?.slack ?? 0.05) * 1000) / 10),
    pacingBoost: num(s.pacing?.maxBoost, 1.5),
    cooldown: Boolean(s.cooldown),
    cooldownDraws: num(s.cooldown?.draws, 5),
    cooldownFactor: num(s.cooldown?.factor, 0.3),
    budget: Boolean(s.budget),
    budgetPoints: num(s.budget?.dailyPoints, 10000),
    budgetPaced: Boolean(s.budget?.paced),
    risk: Boolean(s.risk),
    riskRules: seedRiskRules(s.risk),
    behavior: Boolean(s.behavior),
    behaviorMin: num(s.behavior?.minFactor, 0.8),
    behaviorMax: num(s.behavior?.maxBoost, 1.5),
    behaviorKinds: s.behavior?.kinds ?? [],
    customSignals: (s.behavior?.signals?.length ?? 0) > 0,
    signals,
    rules: (s.rules ?? []).map((rule, index) => ({
      key: index + 1,
      name: rule.name ?? "",
      when: rule.when ?? "",
      kinds: rule.kinds ?? [],
      positions: rule.positions ?? [],
      factor: String(rule.factor),
      disabled: Boolean(rule.disabled)
    })),
    luck: Boolean(s.luck),
    luckStrength: num(s.luck?.strength, 0.3),
    fortune: Boolean(s.fortune),
    fortuneSpread: num(s.fortune?.spread, 0.3),
    supremeMode: supremeModeOf(s.fortune?.supreme),
    supreme: seedSupreme(s.fortune?.supreme?.enabled ? s.fortune.supreme : null),
    prd: s.variance === "prd",
    winRate: Boolean(s.winRate),
    winRateTarget: pct(s.winRate?.target, 30),
    winRateGain: num(s.winRate?.gain, 1),
    winRatePrior: num(s.winRate?.prior, 50),
    guard: Boolean(s.guard),
    guardMin: s.guard?.minWinRate ? pct(s.guard.minWinRate, 0) : "",
    guardMax: s.guard?.maxWinRate && s.guard.maxWinRate < 1 ? pct(s.guard.maxWinRate, 0) : "",
    guardValue: s.guard?.maxValue ? String(s.guard.maxValue) : "",
    guardExpRate: s.guard?.experienceRate ? String(s.guard.experienceRate) : "",
    guardDrawValue: s.guard?.drawValue ? String(s.guard.drawValue) : "",
    guardCaps: Object.fromEntries(Object.entries(s.guard?.kindCaps ?? {}).map(([kind, cap]) => [kind, pct(cap, 0)]))
  };
}

/** 旧字段 deviceAccounts / ipAccounts 与后端一样转成「只发兜底」规则，排在前面 */
function seedRiskRules(risk: LotteryStrategy["risk"]): RiskRuleDraft[] {
  const rules: LotteryRiskRule[] = [];
  if (risk?.deviceAccounts) {
    rules.push({ name: "同设备多账号", signal: "deviceAccounts", op: "gt", threshold: risk.deviceAccounts, action: "floor" });
  }
  if (risk?.ipAccounts) {
    rules.push({ name: "同网络多账号", signal: "ipAccounts", op: "gt", threshold: risk.ipAccounts, action: "floor" });
  }
  rules.push(...(risk?.rules ?? []));
  return rules.map((rule, index) => riskRuleDraft(rule, index + 1));
}

export function riskRuleDraft(rule: LotteryRiskRule, key: number): RiskRuleDraft {
  return {
    key,
    name: rule.name ?? "",
    signal: rule.signal,
    op: rule.op,
    threshold: String(rule.threshold),
    action: rule.action,
    factor: String(rule.factor ?? 0.5),
    rareBelow: String(Math.round((rule.rareBelow ?? 0.01) * 100000) / 1000),
    kinds: rule.kinds ?? [],
    disabled: Boolean(rule.disabled)
  };
}

/** 0–1 → 百分比字符串（保留一位小数） */
const pct = (value: number | undefined, fallback: number) =>
  String(value === undefined ? fallback : Math.round(value * 1000) / 10);

const toNumber = (value: string) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};
const toInt = (value: string) => Math.trunc(toNumber(value));

/** 草稿 → 载荷。positionOf 把规则里的位置按当前奖盘重新映射（奖品挪位置后仍指向同一格的下标） */
export function strategyPayload(draft: StrategyDraft, catalog?: LotteryCatalog | null): LotteryStrategy {
  const out: LotteryStrategy = {};
  if (draft.softPity) out.softPity = { start: toInt(draft.softPityStart), step: toNumber(draft.softPityStep) };
  if (draft.pacing) out.pacing = { slack: toNumber(draft.pacingSlack) / 100, maxBoost: toNumber(draft.pacingBoost) };
  if (draft.cooldown) out.cooldown = { draws: toInt(draft.cooldownDraws), factor: toNumber(draft.cooldownFactor) };
  if (draft.budget) out.budget = { dailyPoints: toInt(draft.budgetPoints), paced: draft.budgetPaced || undefined };
  if (draft.risk) {
    out.risk = {
      rules: draft.riskRules.map((rule) => ({
        name: rule.name.trim() || undefined,
        signal: rule.signal,
        op: rule.op,
        threshold: toNumber(rule.threshold),
        action: rule.action,
        factor: rule.action === "damp" ? toNumber(rule.factor) : undefined,
        rareBelow: rule.action === "rare" ? toNumber(rule.rareBelow) / 100 : undefined,
        kinds: (rule.action === "damp" || rule.action === "rare") && rule.kinds.length ? rule.kinds : undefined,
        disabled: rule.disabled || undefined
      }))
    };
  }
  if (draft.behavior) {
    out.behavior = {
      minFactor: toNumber(draft.behaviorMin),
      maxBoost: toNumber(draft.behaviorMax),
      kinds: draft.behaviorKinds.length ? draft.behaviorKinds : undefined
    };
    if (draft.customSignals) {
      out.behavior.signals = (catalog?.signals ?? []).map((spec) => {
        const own = draft.signals[spec.key];
        return {
          key: spec.key,
          weight: own ? toNumber(own.weight) : spec.weight,
          half: own?.half ? toNumber(own.half) : undefined
        };
      });
    }
  }
  if (draft.luck) out.luck = { strength: toNumber(draft.luckStrength) };
  if (draft.fortune) {
    out.fortune = { spread: toNumber(draft.fortuneSpread) };
    const supreme = activitySupremePayload(draft.supremeMode, draft.supreme);
    if (supreme) out.fortune.supreme = supreme;
  }
  if (draft.prd) out.variance = "prd";
  if (draft.winRate) {
    out.winRate = {
      target: toNumber(draft.winRateTarget) / 100,
      gain: toNumber(draft.winRateGain),
      prior: toNumber(draft.winRatePrior) || undefined
    };
  }
  if (draft.guard) {
    const caps = Object.fromEntries(
      Object.entries(draft.guardCaps)
        .filter(([, value]) => value.trim() !== "")
        .map(([kind, value]) => [kind, toNumber(value) / 100])
    );
    out.guard = {
      minWinRate: draft.guardMin.trim() ? toNumber(draft.guardMin) / 100 : 0,
      maxWinRate: draft.guardMax.trim() ? toNumber(draft.guardMax) / 100 : 0,
      maxValue: draft.guardValue.trim() ? toNumber(draft.guardValue) : 0,
      experienceRate: draft.guardExpRate.trim() ? toNumber(draft.guardExpRate) : undefined,
      drawValue: draft.guardDrawValue.trim() ? toNumber(draft.guardDrawValue) : undefined,
      kindCaps: Object.keys(caps).length ? caps : undefined
    };
  }
  if (draft.rules.length) {
    out.rules = draft.rules.map((rule) => ({
      name: rule.name.trim(),
      when: rule.when.trim(),
      kinds: rule.kinds.length ? rule.kinds : undefined,
      positions: rule.positions.length ? rule.positions : undefined,
      factor: toNumber(rule.factor),
      disabled: rule.disabled || undefined
    }));
  }
  return out;
}

/** 与后端 `ValidateStrategy` 同一套范围与说法（表达式本身由后端编译校验） */
export function validateStrategy(draft: StrategyDraft, prizeCount: number, limits: LotteryStrategyLimits): string[] {
  const issues: string[] = [];
  if (draft.softPity) {
    if (toInt(draft.softPityStart) < 1) issues.push("软保底起点至少为 1");
    const step = toNumber(draft.softPityStep);
    if (step <= 0 || step > limits.softPityStep) issues.push(`软保底每次提升需要在 0–${limits.softPityStep} 个百分点之间`);
  }
  if (draft.pacing) {
    const slack = toNumber(draft.pacingSlack);
    if (slack < 0 || slack > 100) issues.push("匀速释放的提前量需要在 0–100% 之间");
    const boost = toNumber(draft.pacingBoost);
    if (boost < 1 || boost > limits.pacingBoost) issues.push(`匀速释放的追赶倍数需要在 1–${limits.pacingBoost} 之间`);
  }
  if (draft.cooldown) {
    const draws = toInt(draft.cooldownDraws);
    if (draws < 1 || draws > limits.cooldownDraws) issues.push(`中奖冷却的次数需要在 1–${limits.cooldownDraws} 之间`);
    const factor = toNumber(draft.cooldownFactor);
    if (factor < 0 || factor >= 1) issues.push("中奖冷却的系数需要在 0–1 之间（不含 1）");
  }
  if (draft.budget && toInt(draft.budgetPoints) <= 0) issues.push("每日积分预算需要大于 0");
  if (draft.risk) {
    const max = limits.riskRules ?? 16;
    if (!draft.riskRules.length) issues.push("风控至少设置一条规则");
    if (draft.riskRules.length > max) issues.push(`风控规则最多 ${max} 条`);
    draft.riskRules.forEach((rule, index) => {
      const label = rule.name.trim() ? `风控规则「${rule.name.trim()}」` : `第 ${index + 1} 条风控规则`;
      const threshold = Number(rule.threshold);
      if (rule.threshold.trim() === "" || !Number.isFinite(threshold) || threshold < 0) issues.push(`${label}的阈值需要是非负数`);
      if (rule.action === "damp") {
        const factor = toNumber(rule.factor);
        if (factor <= 0 || factor >= 1) issues.push(`${label}的降权系数需要在 0–1 之间（不含 0 和 1）`);
      }
      if (rule.action === "rare") {
        const line = toNumber(rule.rareBelow);
        if (line <= 0 || line > 100) issues.push(`${label}的稀有概率线需要在 0–100% 之间`);
      }
    });
  }
  if (draft.behavior) {
    const min = toNumber(draft.behaviorMin);
    const maxBoost = toNumber(draft.behaviorMax);
    if (min < 0 || min > 1) issues.push("行为分为 0 时的系数需要在 0–1 之间");
    if (maxBoost < min || maxBoost > limits.behaviorBoost) issues.push(`行为分满分时的系数需要在 ${min}–${limits.behaviorBoost} 之间`);
  }
  if (draft.luck) {
    const strength = toNumber(draft.luckStrength);
    const max = limits.luckStrength ?? 2;
    if (strength <= 0 || strength > max) issues.push(`运气平衡强度需要在 0–${max} 之间`);
  }
  if (draft.fortune) {
    const spread = toNumber(draft.fortuneSpread);
    const max = limits.fortuneSpread ?? 1;
    if (spread <= 0 || spread > max) issues.push(`每日运气的差异需要在 0–${max} 之间`);
    if (draft.supremeMode === "custom") issues.push(...validateSupreme({ ...draft.supreme, enabled: true }));
  }
  if (draft.prd && draft.softPity) issues.push("伪随机分布与软保底只能二选一");
  if (draft.winRate) {
    const target = toNumber(draft.winRateTarget);
    const gain = toNumber(draft.winRateGain);
    const max = limits.controlGain ?? 20;
    if (target <= 0 || target >= 100) issues.push("全站目标中奖率需要在 0–100% 之间");
    if (gain <= 0 || gain > max) issues.push(`全站反馈增益需要在 0–${max} 之间`);
  }
  if (draft.guard) {
    const min = draft.guardMin.trim() ? toNumber(draft.guardMin) : 0;
    const max = draft.guardMax.trim() ? toNumber(draft.guardMax) : 100;
    if (min < 0 || min >= 100 || max <= 0 || max > 100) issues.push("中奖率上下限需要在 0–100% 之间");
    else if (min > max) issues.push("中奖率下限不能高于上限");
    if (toNumber(draft.guardValue) < 0) issues.push("期望价值上限不能为负");
    if (Object.values(draft.guardCaps).some((value) => value.trim() && (toNumber(value) < 0 || toNumber(value) > 100))) {
      issues.push("奖品类型占比上限需要在 0–100% 之间");
    }
    if (!draft.guardMin.trim() && !draft.guardMax.trim() && !draft.guardValue.trim() && !Object.values(draft.guardCaps).some((v) => v.trim())) {
      issues.push("护栏至少设置一项");
    }
  }
  if (draft.rules.length > limits.rules) issues.push(`自定义规则最多 ${limits.rules} 条`);
  draft.rules.forEach((rule, index) => {
    const label = rule.name.trim() ? `规则「${rule.name.trim()}」` : `第 ${index + 1} 条规则`;
    const factor = toNumber(rule.factor);
    if (factor < 0 || factor > limits.ruleFactor) issues.push(`${label}的系数需要在 0–${limits.ruleFactor} 之间`);
    if (rule.positions.some((position) => position >= prizeCount)) issues.push(`${label}里有超出奖盘的位置`);
  });
  return issues;
}

/** 维度标识 → 中文名（出奖方案与模拟结果里用） */
export const FACTOR_LABEL: Record<string, string> = {
  stock: "库存",
  dailyLimit: "全站每日限量",
  userLimit: "个人上限",
  userDailyLimit: "个人每日上限",
  risk: "风控",
  pacing: "匀速释放",
  budget: "积分预算",
  behavior: "行为画像",
  cooldown: "中奖冷却",
  rule: "规则",
  softPity: "软保底",
  luck: "运气平衡",
  fortune: "每日运气",
  prd: "伪随机分布",
  control: "全站反馈",
  guard: "护栏"
};
