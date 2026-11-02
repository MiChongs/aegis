import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 抽奖域 API（管理端）。
 *
 * 应用级抽奖活动：九宫格（恰好 8 个奖品）或转盘（2–12 个），每人每天的免费次数、
 * 抽奖次数余额（看激励视频、卡密、积分商城等发放）、积分抽奖，以及「连续 N 次未中必中」的保底。
 * 结果只在服务端产生；活动连同全部奖品一次保存（整张奖盘的约束分开保存会有违反约束的中间态）。
 * 奖品表单由后端目录驱动（`/lottery/catalog`）。规则见 docs/lottery.md。
 */

export type LotteryUIMode = "grid" | "wheel";
export type LotteryStatus = "draft" | "active" | "paused" | "ended";
export type LotterySource = "free" | "bonus" | "points";
export type LotteryDrawStatus = "granted" | "pending" | "fulfilled";
/** 抽中已拥有的奖品怎么办：折算积分 / 改发抽奖次数 / 不参与抽取 / 照常发放 */
export type LotteryLifetimePolicy = "points" | "draws" | "exclude" | "grant";
/** 奖品取值形态：空为不需要取值（谢谢参与） */
export type LotteryRewardValue = "" | "amount" | "ref" | "feature" | "feature_minutes" | "text";

/** 一档奖品的自述，由后端目录下发，前端不另抄一份 */
export type LotteryRewardSpec = {
  type: string;
  label: string;
  hint: string;
  value: LotteryRewardValue;
  unit?: string;
  min?: number;
  max?: number;
};

export type LotteryPlanRef = {
  id: number;
  name: string;
  durationDays: number;
  lifetime: boolean;
  trial: boolean;
  features: string[];
};

export type LotteryFeatureRef = {
  tag: string;
  name: string;
  isActive: boolean;
};

/** 发「抽奖次数」的激励广告场景 */
export type LotteryVideoScene = {
  key: string;
  name: string;
  enabled: boolean;
  drawsPerView: number;
  dailyLimit: number;
};

export type LotteryCatalog = {
  kinds: LotteryRewardSpec[];
  uiModes: LotteryUIMode[];
  statuses: LotteryStatus[];
  sources: LotterySource[];
  gridPrizeCount: number;
  wheelMinPrizes: number;
  wheelMaxPrizes: number;
  /** 可选的会员套餐（已去掉试用套餐） */
  plans: LotteryPlanRef[];
  /** 可选的功能（只含启用中的） */
  features: LotteryFeatureRef[];
  videoScenes: LotteryVideoScene[];
  /** 激励广告总开关 */
  rewardedAds: boolean;
  /** 已拥有奖品的处理方式（文案由后端下发） */
  lifetimePolicies?: Array<{ key: LotteryLifetimePolicy; label: string; hint: string }>;
  /** 「已拥有时改发」可选的奖品类型 */
  swapKinds?: string[];
  lifetimeDefaults?: {
    policy: LotteryLifetimePolicy;
    pointsPerDay: number;
    pointsLifetime: number;
    draws: number;
  };
  /** 应用会员折算策略的单价：活动的每天 / 永久积分为 0 时按它折算 */
  conversionRates?: {
    perVipDay: number;
    perFeatureDay: number;
    perFeatureHour: number;
    perLifetime: number;
  };
  /** 行为画像的信号目录 */
  signals?: LotterySignalSpec[];
  /** 自定义规则里能用的变量 */
  ruleVariables?: LotteryRuleVariable[];
  /** 风控规则：信号与处置目录（处置从轻到重） */
  riskSignals?: LotteryRiskSignalSpec[];
  riskActions?: LotteryRiskActionSpec[];
  strategyLimits?: LotteryStrategyLimits;
  /** 人群特权：人群目录、条件表达式变量、取值上界 */
  perkAudiences?: LotteryPerkAudienceSpec[];
  perkVariables?: LotteryRuleVariable[];
  perkLimits?: LotteryPerkLimits;
  /** 可能「已拥有」、能设置改发的奖品类型 */
  ownableKinds?: string[];
};

// ── 人群特权 ──

export type LotteryPerkAudience = "all" | "member" | "lifetime" | "non_member" | "non_lifetime" | "ad_free";

export type LotteryPerkAudienceSpec = { key: LotteryPerkAudience; label: string; hint: string };

export type LotteryPerkLimits = {
  perks: number;
  name: number;
  extraFree: number;
  extraPoints: number;
  pointsDiscount: number;
  adSkipSlots: number;
  defaultAdSkipSlots: number;
  onlineMinutes: number;
  winBoost: number;
  pityCut: number;
};

/** 一条人群特权：谁（人群 + 今天的条件）得到什么（效果）。多条叠加：次数相加、免广告概率独立合并、折扣取最大、加成相乘 */
export type LotteryPerk = {
  name: string;
  disabled?: boolean;
  audience: LotteryPerkAudience;
  /** 今天签到之后才生效 */
  signedToday?: boolean;
  /** 今天在线满这么多分钟之后才生效 */
  onlineMinutes?: number;
  /** 附加条件（expr 表达式） */
  when?: string;
  /** 每天多几次免费抽奖 */
  extraFree?: number;
  /** 视频抽奖免看广告的概率（百分比 0–100），按顺序逐次判定，轮到且判中时变成 1 次免费 */
  adSkip?: number;
  /** 视频不限次时每天前几次参与判定 */
  adSkipSlots?: number;
  /** 积分抽奖省百分之几 */
  pointsDiscount?: number;
  /** 积分抽奖每天多几次（活动设了每日上限时） */
  extraPoints?: number;
  /** 保底提前几次 */
  pityCut?: number;
  /** 中奖奖品的权重系数（1–3） */
  winBoost?: number;
};

/** 一个人此刻生效的特权（叠加后） */
export type LotteryPerkEffects = {
  names: string[];
  extraFree?: number;
  /** 0–1 */
  adSkip?: number;
  adSkipSlots?: number;
  waived?: number;
  waivedUsed?: number;
  nextWaived?: boolean;
  pointsDiscount?: number;
  extraPoints?: number;
  pityCut?: number;
  winBoost?: number;
};

// ── 出奖策略 ──

/** 信号映射：saturate 越多越高（边际递减）/ decay 越大越低 / bool / ratio（0–1 原值） */
export type LotterySignalMap = "saturate" | "decay" | "bool" | "ratio";

export type LotterySignalSpec = {
  key: string;
  label: string;
  group: string;
  unit?: string;
  map: LotterySignalMap;
  /** 负向信号（风险）：乘法惩罚 */
  negative?: boolean;
  /** 半饱和值：达到它时映射为 0.5 */
  half?: number;
  /** 推荐权重 */
  weight: number;
};

export type LotteryRuleVariable = { name: string; type: "number" | "bool" | "string"; label: string };

export type LotteryStrategyLimits = {
  rules: number;
  ruleFactor: number;
  softPityStep: number;
  cooldownDraws: number;
  pacingBoost: number;
  behaviorBoost: number;
  prizeLimit: number;
  simulationUsers: number;
  simulationEach: number;
  simulationDraws: number;
  luckStrength?: number;
  controlGain?: number;
  fortuneSpread?: number;
  riskRules?: number;
};

// ── 风控规则（docs/lottery.md「风控规则」）──

export type LotteryRiskOp = "gt" | "gte" | "lt" | "lte";
/** 降权 / 不触发保底 / 停发稀有 / 只发兜底 / 拒绝抽奖（从轻到重） */
export type LotteryRiskAction = "damp" | "noPity" | "rare" | "floor" | "deny";

export type LotteryRiskSignalSpec = {
  key: string;
  label: string;
  unit: string;
  hint: string;
  group: string;
  /** 新建规则时的默认比较与阈值 */
  op: LotteryRiskOp;
  threshold: number;
  /** 取值只有 0 / 1 */
  bool?: boolean;
  /** 只出现在旧记录里，不能新建 */
  hidden?: boolean;
};

export type LotteryRiskActionSpec = { key: LotteryRiskAction; label: string; hint: string; severity: number };

export type LotteryRiskRule = {
  name?: string;
  signal: string;
  op: LotteryRiskOp;
  threshold: number;
  action: LotteryRiskAction;
  /** damp 的系数（0–1） */
  factor?: number;
  /** rare 的公示概率线（0–1） */
  rareBelow?: number;
  /** damp / rare 只作用于这些奖品类型 */
  kinds?: string[];
  disabled?: boolean;
};

/** 一条规则在一次抽奖里的评估 */
export type LotteryRiskCheck = {
  name?: string;
  signal: string;
  op: LotteryRiskOp;
  threshold: number;
  value: number;
  /** 取不到这个信号（按未命中） */
  missing?: boolean;
  hit: boolean;
  action: LotteryRiskAction;
  factor?: number;
  rareBelow?: number;
  kinds?: string[];
};

export type LotteryRiskReport = {
  checks: LotteryRiskCheck[];
  /** 命中规则里最重的处置 */
  action?: LotteryRiskAction;
  hits?: string[];
  noPity?: boolean;
};

export type LotteryRiskCount = { key: string; draws: number; denied: number };

export type LotteryRiskCluster = { key: string; accounts: number; draws: number; flagged: number; lastAt: string };

export type LotteryRiskEvent = {
  id: number;
  userId: number;
  account: string;
  nickname?: string;
  action: LotteryRiskAction;
  rules: string[];
  report?: LotteryRiskReport;
  source?: string;
  deviceId?: string;
  clientIp?: string;
  createdAt: string;
};

/** 风控总览 */
export type LotteryRiskOverview = {
  activityId: number;
  days: number;
  since: string;
  draws: number;
  users: number;
  flagged: number;
  flaggedUsers: number;
  denied: number;
  actions: LotteryRiskCount[];
  signals: LotteryRiskCount[];
  trend: Array<{ date: string; draws: number; flagged: number; denied: number }>;
  devices: LotteryRiskCluster[];
  ips: LotteryRiskCluster[];
  subnets: LotteryRiskCluster[];
  events: LotteryRiskEvent[];
  rules: LotteryRiskRule[];
  tookMs: number;
};

export type LotteryStrategyRule = {
  name: string;
  /** expr 表达式，空为总是生效 */
  when: string;
  kinds?: string[];
  positions?: number[];
  factor: number;
  disabled?: boolean;
};

export type LotteryBehaviorModel = {
  /** 空为全部信号的推荐权重 */
  signals?: Array<{ key: string; weight: number; half?: number }>;
  minFactor: number;
  maxBoost: number;
  kinds?: string[];
};

/** 多维度出奖策略：每一项都可以不设，全部不设时为纯加权随机 + 硬保底 */
export type LotteryStrategy = {
  softPity?: { start: number; step: number };
  pacing?: { slack: number; maxBoost: number };
  cooldown?: { draws: number; factor: number };
  /** paced：按当天时间进度放量 */
  budget?: { dailyPoints: number; paced?: boolean };
  /** 风控规则；deviceAccounts / ipAccounts 是旧字段（后端读入时转成只发兜底规则） */
  risk?: { rules?: LotteryRiskRule[]; deviceAccounts?: number; ipAccounts?: number };
  behavior?: LotteryBehaviorModel;
  rules?: LotteryStrategyRule[];
  /** 运气平衡：每 1σ 在对数几率上校正多少 */
  luck?: { strength: number };
  /** 每日运气：每人每天一个系数乘在真实奖品上，spread 为对数标准差 σ；supreme 为本活动的至尊加成（不写跟随应用设置） */
  fortune?: { spread: number; supreme?: LotterySupreme };
  /** prd 为伪随机分布（替代软保底）；空为独立抽取 */
  variance?: "" | "prd";
  /** 全站中奖率反馈（Beta 后验） */
  winRate?: { target: number; gain: number; prior?: number };
  /** 护栏：精确 I-投影 */
  guard?: {
    minWinRate: number;
    maxWinRate: number;
    maxValue: number;
    experienceRate?: number;
    drawValue?: number;
    kindCaps?: Record<string, number>;
  };
};

/** 行为画像（全部行为的原始值） */
export type LotteryBehaviorProfile = {
  registeredDays: number;
  onlineMinutes7d: number;
  onlineMinutes30d: number;
  onlineDays30d: number;
  loginDays30d: number;
  recencyDays: number;
  signStreak: number;
  signDays30d: number;
  signTotalDays: number;
  signedToday: boolean;
  member: boolean;
  lifetime: boolean;
  memberDays: number;
  memberRate: number;
  paidAmount: number;
  paidOrders: number;
  experience: number;
  level: number;
  integral: number;
  pointsEarned30d: number;
  invites: number;
  adViews30d: number;
  redemptions: number;
  devices: number;
  riskScore30d: number;
};

export type LotteryBehaviorScore = {
  score: number;
  factor: number;
  signals: Array<{ key: string; value: number; score: number; weight: number }>;
};

/** 一个奖品在一次抽奖里的系数与最终概率（k 为维度：stock / dailyLimit / userLimit / userDailyLimit / risk / pacing / budget / behavior / cooldown / rule / softPity） */
export type LotteryPrizeOdds = {
  id: number;
  pos: number;
  base: number;
  w: number;
  /** 0–1 */
  p: number;
  f?: Array<{ k: string; v: number; n?: string }>;
  blocked?: string;
};

/** 一次抽奖的出奖方案（记录的 trace） */
export type LotteryDrawPlan = {
  odds: LotteryPrizeOdds[];
  softPity?: number;
  /** 风控只发兜底或拒绝 */
  risk?: boolean;
  /** 风控逐条评估 */
  riskReport?: LotteryRiskReport;
  behavior?: LotteryBehaviorScore;
  winRate: number;
  /** 这一次的期望价值（积分当量） */
  value?: number;
  /** 控制层之前的中奖率 */
  fair?: number;
  /** 运气 z 分数 */
  luck?: number;
  /** 伪随机分布给这一次定的中奖率 */
  prd?: number;
  guardInfeasible?: boolean;
  pity?: boolean;
  picked?: number;
  fallback?: boolean;
  /** empty 所有奖品最终权重为 0 / contended 限量奖品被并发抢光 */
  fallbackReason?: "empty" | "contended";
  /** 这一次的兜底奖 ID */
  floor?: number;
  /** 这一次生效的人群特权 */
  perks?: LotteryPerkEffects;
  /** 今天的运气系数（真实奖品权重已乘过）；fortuneBase 为至尊加成之前的 */
  fortune?: number;
  fortuneBase?: number;
};

/** 距首次中奖次数的精确分布（吸收马尔可夫链） */
export type LotteryStreakAnalysis = {
  hazard: number[];
  survival: number[];
  mean: number;
  std: number;
  p50: number;
  p90: number;
  p99: number;
  longRunWinRate: number;
  never?: boolean;
};

/** 出奖审计：Mahalanobis χ² 整体检验与各奖品的边际检验 */
export type LotteryAuditResult = {
  draws: number;
  skipped: number;
  statistic: number;
  df: number;
  pValue: number;
  alpha: number;
  passed: boolean;
  prizes: Array<{
    prizeId: number;
    position: number;
    observed: number;
    expected: number;
    std: number;
    z: number;
    pValue: number;
    flagged?: boolean;
  }>;
};

export type LotteryDistribution = { mean: number; p50: number; p90: number; p99: number; max: number };

export type LotterySimulationResult = {
  users: number;
  draws: number;
  winRate: number;
  baseWinRate: number;
  pityHits: number;
  softPityDraws: number;
  firstWin: LotteryDistribution;
  neverWon: number;
  missStreak: LotteryDistribution;
  pointsIssued: number;
  winsPerUser?: LotteryDistribution;
  valuePerDraw?: number;
  prizes: Array<{
    prizeId: number;
    position: number;
    name: string;
    kind: string;
    wins: number;
    rate: number;
    baseRate: number;
    blocked: number;
  }>;
  behavior?: LotteryBehaviorScore;
  /** 虚拟用户运气系数的分布；有至尊加成时是加成之后的 */
  fortune?: LotteryDistribution;
  /** 有至尊加成时，加成之前的系数分布 */
  fortuneBase?: LotteryDistribution;
};

export type LotterySimulatePayload = {
  activityId?: number;
  /** 还没保存的编辑内容 */
  activity?: LotteryActivityPayload;
  userId?: number;
  profile?: Partial<LotteryBehaviorProfile>;
  users: number;
  drawsPerUser: number;
  source?: string;
  isolatedStock?: boolean;
  seed?: number;
  /** 不指定用户时，虚拟用户的特权身份 */
  perk?: {
    member?: boolean;
    lifetime?: boolean;
    adFree?: boolean;
    signedToday?: boolean;
    onlineMinutesToday?: number;
    /** 所在会员档位（至尊加成按档位分级时用；lifetime 时按永久开通算） */
    tiers?: number[];
  };
};

export type LotterySimulateResponse = {
  result: LotterySimulationResult;
  profile: LotteryBehaviorProfile;
  behavior?: LotteryBehaviorScore;
  /** 指定用户此刻再抽一次的方案（只对已保存的活动） */
  current?: LotteryDrawPlan;
  analytic?: LotteryStreakAnalysis;
  /** 模拟里计入的人群特权（保底提前、中奖加成） */
  perks?: LotteryPerkEffects;
  /** 活动实际生效的至尊加成与它的效果 */
  supreme?: LotterySupremeSimulation;
};

export type LotterySupremeSimulation = {
  title: string;
  source: LotterySupremeSource;
  /** 模拟的这个人（或虚拟用户）拿到了加成 */
  applied: boolean;
  grades: LotterySupremeGrade[];
  /** 指定真实用户时，他今天加成之前与之后的系数 */
  baseFactor?: number;
  factor?: number;
  /** 同一批随机数、不加成时的模拟结果 */
  baseline?: LotterySimulationResult;
};

export type LotteryReward = {
  type: string;
  /** 整数量；功能解锁时是天数（0 为永久），功能时长是分钟，免广告时长是天数 */
  amount?: number;
  refId?: number;
  tag?: string;
  text?: string;
};

export type LotteryRewardResult = {
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
  expireAt?: string;
  pending?: boolean;
};

export type LotteryPrize = {
  id: number;
  activityId: number;
  name: string;
  /** 与 reward.type 相同 */
  kind: string;
  reward: LotteryReward;
  imageUrl: string;
  /** 总库存，-1 为不限 */
  quantity: number;
  /** 已抽中次数（不限库存的奖品不维护） */
  used: number;
  /** 0 表示不参与常规抽取（只作保底奖或兜底奖） */
  weight: number;
  /** 在奖盘上的位置；九宫格为外圈顺时针 0–7，从左上角起 */
  position: number;
  fallback: boolean;
  /** 抽中的人已拥有这个奖品时改发什么；空为按活动的处理方式 */
  lifetimeReward?: LotteryReward | null;
  /** 全站每天最多出几个 / 每人最多中几次 / 每人每天最多中几次，0 为不限 */
  dailyLimit?: number;
  userLimit?: number;
  userDailyLimit?: number;
  /** 积分当量，0 为按类型估算 */
  value?: number;
  createdAt: string;
  updatedAt: string;
};

export type LotteryActivity = {
  id: number;
  appid: number;
  name: string;
  description: string;
  rules: string;
  uiMode: LotteryUIMode;
  status: LotteryStatus;
  startTime?: string;
  endTime?: string;
  freeDaily: number;
  pointsCost: number;
  /** 积分抽奖每日上限，0 为不限 */
  pointsDaily: number;
  /** 激励广告场景，空为不开放视频抽奖 */
  videoScene: string;
  /** 0 为关闭 */
  pityThreshold: number;
  pityPrizeId?: number;
  lifetimePolicy?: LotteryLifetimePolicy;
  /** 折算积分时每天会员 / 功能折多少积分 */
  lifetimePointsPerDay?: number;
  /** 永久套餐 / 永久功能折多少积分 */
  lifetimePointsLifetime?: number;
  /** 改发抽奖次数时发几次 */
  lifetimeDraws?: number;
  strategy?: LotteryStrategy;
  perks?: LotteryPerk[];
  sortOrder: number;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
};

/** 列表上的一行：活动 + 奖品 + 汇总 */
export type LotteryAdminActivity = LotteryActivity & {
  prizes: LotteryPrize[];
  totalDraws: number;
  totalUsers: number;
  todayDraws: number;
  pending: number;
  /** 积分账与套利检查：积分能换到次数时，抽一次期望拿回多少积分 */
  economy?: LotteryEconomy;
};

/** 一个活动的积分账（后端 `lottery.ActivityEconomy`） */
export type LotteryEconomy = {
  /** 用积分换一次抽奖的最低价（积分抽奖价、积分商城单价）；0 表示积分换不到次数 */
  price: number;
  /** 不拥有任何奖品的用户抽一次的期望积分回报；-1 为抽不完 */
  memberReturn: number;
  /** 已拥有全部会员与功能奖品的永久会员抽一次的期望积分回报（缩减之后） */
  lifetimeReturn: number;
  /** 永久会员抽中已拥有的奖品时按原价的几成折算（0–1）；1 为原价，只有原价折算构成套利时才小于 1 */
  lifetimeScale: number;
  /** 需要处理的问题 */
  warnings: string[];
};

export type LotteryPrizePayload = {
  /** 0 为新建；带 id 为更新（保留已抽中数量） */
  id: number;
  name: string;
  reward: LotteryReward;
  imageUrl: string;
  quantity: number;
  weight: number;
  position: number;
  fallback: boolean;
  /** 未设置时为 null */
  lifetimeReward: LotteryReward | null;
  dailyLimit: number;
  userLimit: number;
  userDailyLimit: number;
  value: number;
};

export type LotteryActivityPayload = {
  name: string;
  description: string;
  rules: string;
  uiMode: LotteryUIMode;
  status: LotteryStatus;
  startTime: string | null;
  endTime: string | null;
  freeDaily: number;
  pointsCost: number;
  pointsDaily: number;
  videoScene: string;
  pityThreshold: number;
  /** 保底奖在 prizes 里的 position；关闭保底时为 null */
  pityPosition: number | null;
  lifetimePolicy: LotteryLifetimePolicy;
  lifetimePointsPerDay: number;
  lifetimePointsLifetime: number;
  lifetimeDraws: number;
  strategy: LotteryStrategy;
  perks: LotteryPerk[];
  sortOrder: number;
  prizes: LotteryPrizePayload[];
};

export type LotteryDraw = {
  id: number;
  drawNo: string;
  appid: number;
  activityId: number;
  activityName?: string;
  userId: number;
  account?: string;
  nickname?: string;
  prizeId?: number;
  prizeName: string;
  prizeKind: string;
  reward: LotteryReward;
  position: number;
  source: LotterySource;
  /** source = bonus 时用的是哪一批次数 */
  ticket?: LotteryDrawTicket;
  pointsCost?: number;
  pity?: boolean;
  status: LotteryDrawStatus;
  results: LotteryRewardResult[];
  /** 抽中的奖品对这个人是已拥有的、改发了别的东西（prizeName / reward 仍是抽中的那一格） */
  conversion?: LotteryConversion;
  note?: string;
  handledBy?: string;
  handledAt?: string;
  requestId?: string;
  deviceId?: string;
  clientIp?: string;
  /** 出奖方案：各奖品的系数与最终概率、行为分 */
  trace?: LotteryDrawPlan;
  /** 命中的风控信号与最重的处置 */
  riskRules?: string[];
  riskAction?: LotteryRiskAction;
  createdAt: string;
};

export type LotteryConversion = {
  /** lifetime 永久会员已包含 / feature 已永久解锁该功能 */
  reason: "lifetime" | "feature";
  reward: LotteryReward;
  label: string;
  message: string;
};

export type LotteryDrawParams = {
  activityId?: number;
  userId?: number;
  status?: LotteryDrawStatus;
  source?: LotterySource;
  /** 只看抽中的（排除谢谢参与） */
  winning?: boolean;
  /** 检索式：单号、IP、数字、文本，或 user: prize: device: ip: no: id: 字段，空格分隔同时满足 */
  keyword?: string;
  /** any 命中过风控 / clean 未命中 / 信号键 */
  risk?: string;
  riskAction?: LotteryRiskAction;
  outcome?: "pity" | "fallback" | "conversion";
  prizeId?: number;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type LotteryPage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type LotteryStats = {
  activityId: number;
  days: number;
  summary: {
    todayDraws: number;
    todayUsers: number;
    todayWins: number;
    totalDraws: number;
    totalUsers: number;
    pointsSpent: number;
    pending: number;
    pityHits: number;
    /** 抽中已拥有的奖品、改发的次数 */
    converted?: number;
    /** 其中折算出去的积分 */
    convertedPoints?: number;
  };
  trend: Array<{ date: string; draws: number; wins: number; users: number }>;
  sources: Array<{ source: LotterySource; draws: number }>;
  prizes: Array<{ prizeId?: number; name: string; kind: string; draws: number; quantity: number; used: number; converted?: number }>;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/lottery`;

// ── 目录 ──

export function getLotteryCatalog(token: string, appKey: string) {
  return apiRequest<LotteryCatalog>(`${base(appKey)}/catalog`, { token });
}

// ── 活动 ──

export function listLotteryActivities(token: string, appKey: string) {
  return apiRequest<LotteryAdminActivity[]>(`${base(appKey)}/activities`, { token });
}

export function createLotteryActivity(token: string, appKey: string, payload: LotteryActivityPayload) {
  return apiRequest<LotteryAdminActivity>(`${base(appKey)}/activities`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateLotteryActivity(
  token: string,
  appKey: string,
  activityId: number,
  payload: LotteryActivityPayload
) {
  return apiRequest<LotteryAdminActivity>(`${base(appKey)}/activities/${activityId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

/** 上线前后端按当前奖盘再校验一次 */
export function setLotteryActivityStatus(token: string, appKey: string, activityId: number, status: LotteryStatus) {
  return apiRequest<LotteryAdminActivity>(`${base(appKey)}/activities/${activityId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

/** 有抽奖记录的活动不能删除（40927），改为结束 */
export function deleteLotteryActivity(token: string, appKey: string, activityId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/activities/${activityId}`, { method: "DELETE", token });
}

export function getLotteryStats(token: string, appKey: string, activityId: number, days: number) {
  return apiRequest<LotteryStats>(`${base(appKey)}/activities/${activityId}/stats${buildQuery({ days })}`, { token });
}

// ── 抽奖记录 ──

export function listLotteryDraws(token: string, appKey: string, params?: LotteryDrawParams) {
  return apiRequest<LotteryPage<LotteryDraw>>(`${base(appKey)}/draws${buildQuery(params ?? {})}`, { token });
}

/** 人工发放的奖品标记已发放；note 写进给用户的站内信（重复标记 40926） */
export function fulfillLotteryDraw(token: string, appKey: string, drawId: number, note: string) {
  return apiRequest<LotteryDraw>(`${base(appKey)}/draws/${drawId}/fulfill`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

// ── 策略模拟 ──

/** 虚拟用户连续抽奖的模拟（不写库、不发奖）；指定 userId 时附带此人的画像与下一次的出奖方案 */
export function simulateLottery(token: string, appKey: string, payload: LotterySimulatePayload) {
  return apiRequest<LotterySimulateResponse>(`${base(appKey)}/simulate`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 出奖审计：最近 days 天的记录是否服从每一次方案给出的概率 */
/** 风控总览（days 1–90） */
export function getLotteryRisk(token: string, appKey: string, activityId: number, days: number) {
  return apiRequest<LotteryRiskOverview>(`${base(appKey)}/activities/${activityId}/risk${buildQuery({ days })}`, { token });
}

export function getLotteryAudit(token: string, appKey: string, activityId: number, days: number) {
  return apiRequest<LotteryAuditResult>(`${base(appKey)}/activities/${activityId}/audit${buildQuery({ days })}`, { token });
}

// ── 运气控制（docs/lottery.md「运气控制」）──
//
// 管理员对每日运气的干预：按人（每人一条）或全站，带生效时间窗（默认从现在到明天零点），可撤销、全部留痕。
// 活动页、抽奖事务、单人模拟用同一个合成结果，用户看到的与抽到的是同一个系数；用户端看不到有没有被干预。

export type LotteryFortuneLevel = "burst" | "lucky" | "steady" | "quiet";
/** 重抽（换一个手气）/ 定档（z 定在某档或某系数）/ 偏移（z 加减） */
export type LotteryFortuneControlKind = "reroll" | "pin" | "shift";
export type LotteryFortuneControlStatus = "active" | "scheduled" | "expired" | "revoked";

export type LotteryFortuneLevelInfo = {
  level: LotteryFortuneLevel;
  title: string;
  /** 定档到这一档时用的 z */
  pinZ: number;
  /** 定档到这一档时的系数（按当前差异） */
  factor: number;
  /** 没有干预时的理论占比（0–1） */
  share: number;
  minZ: number;
};

export type LotteryFortuneDistribution = {
  users: number;
  levels: Record<LotteryFortuneLevel, number>;
  baseLevels: Record<LotteryFortuneLevel, number>;
  controlled: number;
  meanFactor: number;
  baseMeanFactor: number;
};

export type LotteryFortuneControl = {
  id: number;
  activityId: number;
  /** 为空是全站 */
  userId?: number | null;
  account?: string;
  nickname?: string;
  kind: LotteryFortuneControlKind;
  /** pin：定在的 z；shift：加减的量 */
  z: number;
  level?: LotteryFortuneLevel | "";
  startsAt: string;
  endsAt: string;
  note?: string;
  operator: string;
  revokedAt?: string | null;
  revokedBy?: string;
  createdAt: string;
  status: LotteryFortuneControlStatus;
};

// ── 至尊加成（docs/lottery.md「至尊加成」）──
//
// f' = max(f, min(max(f × 幅度, 保底), 上限))：接在管理员干预之后、与每日运气同一个生效点。
// 应用设置是默认，活动策略里的 fortune.supreme 覆盖它（enabled=false 即这个活动不加成）。

export type LotterySupremeScope = "lifetime" | "tiers";
export type LotterySupremeSource = "activity" | "app";

export type LotterySupremeTier = {
  tierId: number;
  /** 只认这个档位里的永久开通 */
  lifetimeOnly?: boolean;
  boost?: number;
  floor?: number;
  cap?: number;
};

export type LotterySupreme = {
  enabled: boolean;
  scope: LotterySupremeScope;
  /** lifetime 时为永久会员的参数；tiers 时为没落在任何一档的永久会员的参数 */
  boost?: number;
  floor?: number;
  cap?: number;
  tiers?: LotterySupremeTier[];
  title?: string;
};

export type LotterySupremeGrade = {
  boost: number;
  floor?: number;
  cap?: number;
  tierId?: number;
  tierName?: string;
};

export type LotterySettings = {
  appid: number;
  supreme: LotterySupreme;
  updatedBy?: string;
  updatedAt: string;
  tiers: Array<{ id: number; name: string; members: number; lifetimeMembers: number }>;
  /** 开了每日运气的活动各自用的是哪份至尊加成 */
  activities: Array<{ id: number; name: string; status: string; source: LotterySupremeSource; enabled: boolean }>;
};

export function getLotterySettings(token: string, appKey: string) {
  return apiRequest<LotterySettings>(`${base(appKey)}/settings`, { token });
}

export function saveLotterySettings(token: string, appKey: string, supreme: LotterySupreme) {
  return apiRequest<LotterySettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify({ supreme })
  });
}

export type LotteryFortuneOverview = {
  activityId: number;
  enabled: boolean;
  spread: number;
  low: number;
  high: number;
  levels: LotteryFortuneLevelInfo[];
  dayStart: string;
  resetsAt: string;
  today: LotteryFortuneDistribution;
  global: LotteryFortuneControl[];
  counts: { activeGlobal: number; activeUsers: number; scheduled: number };
  /** 实际生效的至尊加成与来源；今日分布不含加成 */
  supreme?: LotterySupreme;
  supremeSource?: LotterySupremeSource;
  /** 拿到加成的人最走运时的系数 */
  supremeHigh?: number;
};

/** 用户端的运气读数（与 App 看到的一致） */
export type LotteryFortuneReading = {
  factor: number;
  percentile: number;
  level: LotteryFortuneLevel;
  title: string;
  message: string;
  low: number;
  high: number;
  resetsAt: string;
  /** 至尊加成：上面的读数已是加成之后的 */
  supreme?: LotterySupremeReading;
};

export type LotterySupremeReading = {
  title: string;
  tier?: string;
  baseFactor: number;
  baseLevel: LotteryFortuneLevel;
  baseTitle: string;
  basePercentile: number;
  boost: number;
  floor?: number;
  cap?: number;
  lift: number;
  baseWinRate: number;
  message: string;
};

export type LotteryFortuneUserReading = {
  userId: number;
  enabled: boolean;
  base?: LotteryFortuneReading | null;
  current?: LotteryFortuneReading | null;
  baseZ: number;
  z: number;
  applied: LotteryFortuneControl[];
  controls: LotteryFortuneControl[];
  lifetime?: boolean;
  tiers?: Array<{ id: number; name: string; lifetime: boolean }>;
};

export type LotteryFortuneControlPayload = {
  userIds?: number[];
  allUsers?: boolean;
  kind: LotteryFortuneControlKind;
  level?: LotteryFortuneLevel;
  factor?: number;
  shift?: number;
  startsAt?: string;
  endsAt?: string;
  note?: string;
};

export type LotteryFortuneControlParams = {
  userId?: number;
  kind?: LotteryFortuneControlKind;
  status?: LotteryFortuneControlStatus;
  page?: number;
  limit?: number;
};

const fortuneBase = (appKey: string, activityId: number) => `${base(appKey)}/activities/${activityId}/fortune`;

export function getLotteryFortune(token: string, appKey: string, activityId: number) {
  return apiRequest<LotteryFortuneOverview>(fortuneBase(appKey, activityId), { token });
}

/** 调整运气差异（0 关闭每日运气） */
export function setLotteryFortuneSpread(token: string, appKey: string, activityId: number, spread: number) {
  return apiRequest<LotteryFortuneOverview>(`${fortuneBase(appKey, activityId)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify({ spread })
  });
}

export function getLotteryFortuneUser(token: string, appKey: string, activityId: number, userId: number) {
  return apiRequest<LotteryFortuneUserReading>(`${fortuneBase(appKey, activityId)}/users/${userId}`, { token });
}

export function listLotteryFortuneControls(
  token: string,
  appKey: string,
  activityId: number,
  params?: LotteryFortuneControlParams
) {
  return apiRequest<LotteryPage<LotteryFortuneControl>>(
    `${fortuneBase(appKey, activityId)}/controls${buildQuery(params ?? {})}`,
    { token }
  );
}

export function createLotteryFortuneControls(
  token: string,
  appKey: string,
  activityId: number,
  payload: LotteryFortuneControlPayload
) {
  return apiRequest<{ items: LotteryFortuneControl[] }>(`${fortuneBase(appKey, activityId)}/controls`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 预演：按今天抽过奖的人（或所选用户）算干预之后的分布，不落库 */
export function previewLotteryFortuneControl(
  token: string,
  appKey: string,
  activityId: number,
  payload: LotteryFortuneControlPayload
) {
  return apiRequest<LotteryFortuneDistribution>(`${fortuneBase(appKey, activityId)}/controls/preview`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 撤销几条；all 为真时撤销全部生效中与待生效的（恢复自然运气） */
export function revokeLotteryFortuneControls(
  token: string,
  appKey: string,
  activityId: number,
  input: { ids?: number[]; all?: boolean }
) {
  return apiRequest<{ revoked: number }>(`${fortuneBase(appKey, activityId)}/controls/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

// ── 抽奖次数（docs/lottery.md「抽奖次数」）──
//
// bonus 分批记账：每次发放一批（来源、是否兑换所得），每次增减一条流水。
// 扣除顺序只有一条：今日免费 → 赠送的次数（先到先用）→ 兑换的次数（先到先用）→ 积分。

export type LotteryTicketOrigin =
  | "rewarded_ad"
  | "points_store"
  | "cardkey"
  | "invite"
  | "leaderboard"
  | "lottery"
  | "item_grant"
  | "admin"
  | "legacy";

export type LotteryLedgerKind = "grant" | "consume" | "deduct";

export type LotteryDrawTicket = {
  lotId?: number;
  origin: LotteryTicketOrigin;
  originLabel: string;
  paid: boolean;
};

/** 余额按档拆开：gift 赠送的（先用）、purchased 兑换的（后用） */
export type LotteryTicketBalance = { gift: number; purchased: number };

export type LotteryTicketLot = {
  id: number;
  origin: LotteryTicketOrigin;
  originLabel: string;
  paid: boolean;
  title: string;
  ref?: string;
  amount: number;
  remaining: number;
  createdAt: string;
};

export type LotteryTicketAccount = {
  userId?: number;
  account?: string;
  nickname?: string;
  balance: number;
  tickets: LotteryTicketBalance;
  totalGranted: number;
  totalConsumed: number;
  /** 扣除顺序文案 */
  order: string[];
  /** 还有剩余的批次，排在前面的先用 */
  lots: LotteryTicketLot[];
  updatedAt?: string;
};

export type LotteryTicketEntry = {
  id: number;
  userId?: number;
  account?: string;
  nickname?: string;
  lotId?: number | null;
  kind: LotteryLedgerKind;
  kindLabel: string;
  origin: LotteryTicketOrigin;
  originLabel: string;
  paid: boolean;
  delta: number;
  balanceAfter: number;
  title: string;
  ref?: string;
  operator?: string;
  createdAt: string;
};

export type LotteryTicketOriginStat = {
  origin: LotteryTicketOrigin;
  originLabel: string;
  paid: boolean;
  granted: number;
  remaining: number;
  users: number;
};

export type LotteryTicketOverview = {
  holders: number;
  outstanding: number;
  tickets: LotteryTicketBalance;
  grantedToday: number;
  usedToday: number;
  origins: LotteryTicketOriginStat[];
  order: string[];
};

export type LotteryTicketLedgerParams = {
  userId?: number;
  kind?: LotteryLedgerKind;
  origin?: LotteryTicketOrigin;
  keyword?: string;
  page?: number;
  limit?: number;
};

export type LotteryTicketAdjustPayload = {
  /** 正数发放、负数扣减（按扣除顺序先扣赠送的） */
  amount: number;
  /** 发放的次数按兑换所得记（排在赠送的之后用） */
  paid?: boolean;
  reason: string;
};

const ticketsBase = (appKey: string) => `${base(appKey)}/tickets`;

export function getLotteryTicketOverview(token: string, appKey: string) {
  return apiRequest<LotteryTicketOverview>(ticketsBase(appKey), { token });
}

export function listLotteryTicketAccounts(
  token: string,
  appKey: string,
  params?: { keyword?: string; page?: number; limit?: number }
) {
  return apiRequest<LotteryPage<LotteryTicketAccount>>(`${ticketsBase(appKey)}/accounts${buildQuery(params ?? {})}`, {
    token
  });
}

export function listLotteryTicketLedger(token: string, appKey: string, params?: LotteryTicketLedgerParams) {
  return apiRequest<LotteryPage<LotteryTicketEntry>>(`${ticketsBase(appKey)}/ledger${buildQuery(params ?? {})}`, {
    token
  });
}

export function getLotteryTicketAccount(token: string, appKey: string, userId: number) {
  return apiRequest<LotteryTicketAccount>(`${ticketsBase(appKey)}/users/${userId}`, { token });
}

export function adjustLotteryTickets(token: string, appKey: string, userId: number, payload: LotteryTicketAdjustPayload) {
  return apiRequest<LotteryTicketAccount>(`${ticketsBase(appKey)}/users/${userId}/adjust`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

