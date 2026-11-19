import { apiRequest, buildQuery } from "@/lib/api/client";
import type {
  LotteryActivityPayload,
  LotteryEconomy,
  LotteryPage,
  LotteryPerkEffects,
  LotterySupremeGrade
} from "@/lib/api/lottery";

/**
 * 抽奖计算引擎 API（管理端）：精确分析、奖盘求解、大规模推演。规则见 docs/lottery.md「计算引擎」。
 *
 * 三者都只算不写：不发奖、不扣次数，推演任务只写它自己的任务表。
 * 精确分析与求解是同步的（毫秒级）；推演是落库的异步任务，发起后轮询任务拿进度与结果。
 */

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/lottery`;

// ── 人群 ──

/** 一种人群：身份由几个开关给出，与策略模拟里虚拟用户的特权身份同一套 */
export type LotteryPersonaPayload = {
  key?: string;
  name?: string;
  /** 占比（只对推演有用，任意正数，后端归一） */
  share?: number;
  /** 这类人每天抽几次（只对推演有用）；不传跟随场景 */
  drawsPerDay?: number;
  member?: boolean;
  /** 永久会员：按「已拥有奖盘里全部会员与功能奖品」算 */
  lifetime?: boolean;
  adFree?: boolean;
  signedToday?: boolean;
  onlineMinutesToday?: number;
  tiers?: number[];
};

// ── 精确分析 ──

export type LotteryExactPrize = {
  prizeId: number;
  position: number;
  name: string;
  kind: string;
  /** 公示概率（权重占比，0–1） */
  nominal: number;
  /** 综合概率（含保底、软保底、伪随机，0–1） */
  longRun: number;
  /** 积分当量 */
  value: number;
  /** 期望多少抽出第一个；never 为真时出不了 */
  hitMean: number;
  hitP50: number;
  hitP90: number;
  hitP99: number;
  never?: boolean;
  /** 1 / 10 / 50 / 100 连抽里至少出一个的概率 */
  within: Array<{ draws: number; probability: number }>;
};

export type LotteryExactFortuneBand = {
  factor: number;
  winRate: number;
  realRate: number;
  valuePerDraw: number;
  /** 各奖品的综合概率（与 prizes 同序） */
  prizes: number[];
};

export type LotteryExactAnalysis = {
  states: number;
  winRate: number;
  baseWinRate: number;
  /** 抽中兜底奖、谢谢参与以外奖品的综合概率 */
  realRate: number;
  baseRealRate: number;
  valuePerDraw: number;
  pointsPerDraw: number;
  redrawsPerDraw: number;
  /** 长期有多大比例的抽奖是硬保底给的 */
  pityShare: number;
  /** 两次中奖之间平均隔几抽 */
  cycleMean: number;
  prizes: LotteryExactPrize[];
  /** 开了每日运气时：最倒霉、最走运两端与全体平均 */
  fortune?: { low: LotteryExactFortuneBand; high: LotteryExactFortuneBand; mean: LotteryExactFortuneBand };
  /** 按固定状态处理、没有随时间演变的维度 */
  notes: string[];
};

export type LotteryAnalysisPersona = {
  key: string;
  name: string;
  /** 这份分析用的运气系数（自然运气 1，拿到至尊加成时是加成之后的） */
  factor: number;
  perks?: LotteryPerkEffects;
  supreme: LotterySupremeGrade[];
  /** 这类人用积分换一次抽奖的最低价；0 为换不到 */
  price: number;
  /** 每抽期望到账的积分回报（含已拥有奖品的折算）；-1 为抽不完 */
  return: number;
  /** 已拥有奖品按原价的几成折算 */
  scale: number;
  /** 抽中每一格实际到账的积分与抽奖次数（与 analysis.prizes 同序） */
  payout: number[];
  redraws: number[];
  analysis: LotteryExactAnalysis;
};

export type LotteryAnalysisResult = {
  personas: LotteryAnalysisPersona[];
  economy?: LotteryEconomy;
};

export type LotteryAnalyzePayload = {
  activityId?: number;
  /** 还没保存的编辑内容 */
  activity?: LotteryActivityPayload;
  personas?: LotteryPersonaPayload[];
};

/** 精确分析：每种人群的综合概率、每抽价值、各奖品多少抽能出。不抽样、不写库 */
export function analyzeLottery(token: string, appKey: string, payload: LotteryAnalyzePayload) {
  return apiRequest<LotteryAnalysisResult>(`${base(appKey)}/analyze`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 奖盘求解 ──

export type LotterySolvePrizeMode = "lock" | "target" | "range";

export type LotterySolvePrizeGoal = {
  position: number;
  mode: LotterySolvePrizeMode;
  /** 概率都是 0–1 */
  probability?: number;
  min?: number;
  max?: number;
};

export type LotterySolveGoal = {
  winRate?: number;
  realRate?: number;
  value?: number;
  valueMode?: "max" | "equal";
  pointsReturn?: number;
  prizes?: LotterySolvePrizeGoal[];
  total?: number;
};

export type LotterySolveFortune = "average" | "high" | "low";

export type LotterySolvePayload = {
  activityId?: number;
  activity?: LotteryActivityPayload;
  persona?: LotteryPersonaPayload;
  fortune?: LotterySolveFortune;
  goal: LotterySolveGoal;
};

export type LotterySolveSummary = {
  winRate: number;
  realRate: number;
  valuePerDraw: number;
  pointsPerDraw: number;
  redrawsPerDraw: number;
  /** 每抽期望积分回报；-1 为抽不完 */
  pointsReturn: number;
};

export type LotterySolveCheck = {
  key: string;
  label: string;
  mode: "equal" | "max" | "min" | "lock";
  target: number;
  before: number;
  after: number;
  met: boolean;
  /** 针对单个奖品的目标时是它的位置，否则为 -1 */
  position: number;
};

export type LotterySolvedPrize = {
  prizeId: number;
  position: number;
  name: string;
  kind: string;
  weight: number;
  newWeight: number;
  nominal: number;
  newNominal: number;
  longRun: number;
  newLongRun: number;
};

export type LotterySolveResult = {
  feasible: boolean;
  converged: boolean;
  iterations: number;
  total: number;
  /** 新旧权重占比的 KL 散度：改动幅度 */
  divergence: number;
  prizes: LotterySolvedPrize[];
  before: LotterySolveSummary;
  after: LotterySolveSummary;
  checks: LotterySolveCheck[];
  notes: string[];
  persona: string;
  factor: number;
  economyBefore?: LotteryEconomy;
  economyAfter?: LotteryEconomy;
};

/** 按目标解出新的权重。不保存：把 newWeight 填回表单，由管理员确认后保存 */
export function solveLottery(token: string, appKey: string, payload: LotterySolvePayload) {
  return apiRequest<LotterySolveResult>(`${base(appKey)}/solve`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 大规模推演 ──

/** 一个指标的估计：各平行世界的均值与 95% 置信区间 */
export type LotteryEstimate = { mean: number; stdErr: number; low: number; high: number; n: number };

export type LotteryComputeBucket = { from: number; to: number; count: number };

export type LotteryComputeDistribution = {
  mean: number;
  p50: number;
  p90: number;
  p99: number;
  max: number;
  buckets: LotteryComputeBucket[];
};

export type LotteryComputeQuantiles = { mean: number; p10: number; p50: number; p90: number; p99: number; max: number };

export type LotteryComputePrize = {
  prizeId: number;
  position: number;
  name: string;
  kind: string;
  wins: number;
  rate: LotteryEstimate;
  /** 全部世界合在一起的 Wilson 区间（稀有奖品看这个） */
  low: number;
  high: number;
  baseRate: number;
  /** 有多少次这个奖品出不了（硬性资格、预算、匀速释放、规则） */
  blocked: number;
  stockOut?: { probability: number; day: LotteryEstimate };
  dailyCap?: { share: number; at: number };
};

export type LotteryComputePersonaResult = {
  key: string;
  name: string;
  share: number;
  users: number;
  draws: number;
  winRate: LotteryEstimate;
  realRate: LotteryEstimate;
  valuePerDraw: LotteryEstimate;
  pointsPerDraw: LotteryEstimate;
  /** 每抽期望积分回报 P / (1 − Q) */
  return: LotteryEstimate;
  /** 这类人用积分买次数稳赚 */
  arbitrage?: boolean;
  prizes: number[];
};

export type LotteryComputeResult = {
  users: number;
  days: number;
  drawsPerDay: number;
  replications: number;
  draws: number;
  seed: string;
  winRate: LotteryEstimate;
  realRate: LotteryEstimate;
  valuePerDraw: LotteryEstimate;
  pointsPerDraw: LotteryEstimate;
  redrawsPerDraw: LotteryEstimate;
  pityShare: LotteryEstimate;
  softPityShare: LotteryEstimate;
  fallbackShare: LotteryEstimate;
  pointsPerWorld: LotteryEstimate;
  /** 一次真实奖品都没抽到的人占多少 */
  neverWon: LotteryEstimate;
  prizes: LotteryComputePrize[];
  personas: LotteryComputePersonaResult[];
  daily: Array<{ day: number; draws: number; winRate: number; realRate: number; value: number; points: number }>;
  stock: Array<{ prizeId: number; position: number; name: string; quantity: number; used: number[] }>;
  budget?: { dailyPoints: number; share: number; at: number; used: number };
  winsPerUser: LotteryComputeDistribution;
  missStreak: LotteryComputeDistribution;
  firstWin: LotteryComputeDistribution;
  valuePerUser: LotteryComputeQuantiles;
  pointsPerUser: LotteryComputeQuantiles;
  fortune?: LotteryComputeQuantiles;
  arbitrage?: { price: number; return: LotteryEstimate; probability: number; exceeds: boolean };
  precision: { valueError: number; rateError: number; enough: boolean };
};

export type LotteryComputeStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";

export type LotteryComputeJob = {
  id: number;
  appid: number;
  activityId?: number;
  activityName: string;
  name: string;
  status: LotteryComputeStatus;
  summary: {
    users: number;
    days: number;
    drawsPerDay: number;
    replications: number;
    draws: number;
    seed: string;
    source?: string;
    personas: Array<{ key: string; name: string; share: number; drawsPerDay: number }>;
  };
  /** 已合并 / 总共的平行世界数 */
  progress: number;
  total: number;
  message?: string;
  attempts: number;
  durationMs: number;
  createdBy: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  /** 完成后的结果；列表上不带 */
  result?: LotteryComputeResult;
};

export type LotteryComputePayload = {
  activityId?: number;
  activity?: LotteryActivityPayload;
  name?: string;
  users: number;
  days: number;
  drawsPerDay: number;
  replications: number;
  /** 十进制字符串（64 位整数超出 JSON 数字的安全范围）；不传随机 */
  seed?: string;
  source?: string;
  personas?: LotteryPersonaPayload[];
};

/** 发起一次推演：立刻返回排队中的任务（排队与执行中的到上限时 42922） */
export function createLotteryComputeJob(token: string, appKey: string, payload: LotteryComputePayload) {
  return apiRequest<LotteryComputeJob>(`${base(appKey)}/compute/jobs`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function listLotteryComputeJobs(
  token: string,
  appKey: string,
  params?: { activityId?: number; page?: number; limit?: number }
) {
  return apiRequest<LotteryPage<LotteryComputeJob>>(`${base(appKey)}/compute/jobs${buildQuery(params ?? {})}`, { token });
}

export function getLotteryComputeJob(token: string, appKey: string, jobId: number) {
  return apiRequest<LotteryComputeJob>(`${base(appKey)}/compute/jobs/${jobId}`, { token });
}

/** 取消排队或执行中的任务（已结束的原样返回） */
export function cancelLotteryComputeJob(token: string, appKey: string, jobId: number) {
  return apiRequest<LotteryComputeJob>(`${base(appKey)}/compute/jobs/${jobId}/cancel`, { method: "POST", token });
}

/** 删除已结束的任务（还在排队或执行时 40939） */
export function deleteLotteryComputeJob(token: string, appKey: string, jobId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/compute/jobs/${jobId}`, { method: "DELETE", token });
}
