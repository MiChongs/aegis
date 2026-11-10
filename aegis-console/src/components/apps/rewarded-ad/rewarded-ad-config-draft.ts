import type { CardKeyReward, CardKeyRewardSpec } from "@/lib/api/card-key";
import type {
  RewardedAdConfig,
  RewardedAdLifetime,
  RewardedAdLifetimePolicy,
  RewardedAdLifetimePolicySpec,
  RewardedAdPlacement,
  RewardedAdScene,
  RewardedAdVerifyMode,
  SaveRewardedAdConfigPayload
} from "@/lib/api/rewarded-ad";

/**
 * 激励广告配置页的草稿：服务端数据 ⇄ 表单状态 ⇄ 保存载荷，以及保存前的检查。
 *
 * 数字项一律按字符串存（理由同卡密批次编辑器：存 number 的话输入框清不空）。
 * 密钥明文只在草稿里，保存成功即丢弃；服务端只回「配没配」与末 4 位提示。
 */

/** 送会员的两档：会员权益只在场景勾了其中一档时才有意义。 */
export const VIP_DAYS = "vip_days";
export const VIP_PLAN = "vip_plan";
/** 「功能时长」：永久会员使用时免扣，也算可能已拥有的一档 */
export const FEATURE_MINUTES = "feature_minutes";
export const OWNABLE = [VIP_DAYS, VIP_PLAN, FEATURE_MINUTES];

/** 功能时长新勾选时的默认分钟数，再夹到目录给的上下界内 */
export const DEFAULT_FEATURE_MINUTES = 60;

/** 与后端 rewardedad 的上界一致 */
export const MAX_PLACEMENTS = 20;
export const MAX_SCENES = 20;
const PLACEMENT_ID_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;

/** 后端没下发目录时的兜底（与服务端 rewardedad.LifetimePolicyCatalog 一致） */
export const FALLBACK_LIFETIME_POLICIES: RewardedAdLifetimePolicySpec[] = [
  { key: "convert", label: "按价值折算", hint: "会员按天、功能时长按小时折成积分、AI 额度、经验或抽奖次数，其余权益照发" },
  { key: "replace", label: "改发固定权益", hint: "已拥有的权益不再发放，改发一组固定的积分、经验、抽奖次数或 AI 额度" },
  { key: "hide", label: "不展示该场景", hint: "对已拥有的用户隐藏这个场景；仍有观看到达时按价值折算" },
  { key: "grant", label: "照常发放", hint: "仍记一段会员或功能时长（永久会员实际拿不到新东西）" }
];
export const FALLBACK_CONVERT_TARGETS = ["integral", "ai_credit", "experience", "lottery_draws"];

export type LifetimeDraft = {
  policy: RewardedAdLifetimePolicy;
  target: string;
  perVipDay: string;
  perFeatureHour: string;
  rewards: Record<string, string>;
};

/** uid 只给 React 列表用：场景标识与广告位 ID 都能改，不能拿它们当 key。 */
export type SceneDraft = {
  uid: string;
  key: string;
  name: string;
  placementId: string;
  enabled: boolean;
  dailyLimit: string;
  cooldownSeconds: string;
  /** feature 只对取值形态为 feature 的档位（功能时长）有意义 */
  rewards: Record<string, { amount: string; refId: number; feature: string }>;
  /** 会员权益：「会员天数」带的功能标识，以及送出的会员是否免广告 */
  features: string[];
  adFree: boolean;
  lifetime: LifetimeDraft;
};

export type PlacementDraft = {
  uid: string;
  id: string;
  /** 服务端已存的 ID（新加的为空）：改了 ID 时据此把已存的 Key 带过去 */
  savedId: string;
  name: string;
  /** 新填的 Key，留空表示沿用 */
  key: string;
  clearKey: boolean;
};

export type Draft = {
  scope: string;
  enabled: boolean;
  provider: string;
  providerAppId: string;
  /** 默认 Key：新填的值与是否清除 */
  defaultKey: string;
  clearDefaultKey: boolean;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: string;
  placements: PlacementDraft[];
  scenes: SceneDraft[];
};

let uidSeed = 0;
export const nextUid = (prefix: string) => `${prefix}-new-${++uidSeed}`;

export function lifetimeToDraft(rule?: RewardedAdLifetime): LifetimeDraft {
  const rewards: Record<string, string> = {};
  (rule?.rewards ?? []).forEach((reward) => {
    rewards[reward.type] = String(reward.amount ?? 1);
  });
  const unset = !rule?.perVipDay && !rule?.perFeatureHour;
  return {
    policy: rule?.policy || "convert",
    target: rule?.target || "integral",
    perVipDay: String(unset ? 10 : rule?.perVipDay ?? 0),
    perFeatureHour: String(unset ? 10 : rule?.perFeatureHour ?? 0),
    rewards
  };
}

function lifetimeToPayload(draft: LifetimeDraft): RewardedAdLifetime {
  switch (draft.policy) {
    case "replace":
      return {
        policy: "replace",
        rewards: Object.entries(draft.rewards).map(([type, amount]) => ({ type, amount: Number(amount) || 0 }))
      };
    case "grant":
      return { policy: "grant" };
    default:
      return {
        policy: draft.policy,
        target: draft.target,
        perVipDay: Number(draft.perVipDay) || 0,
        perFeatureHour: Number(draft.perFeatureHour) || 0
      };
  }
}

// 从服务端数据播种的条目用下标作 uid：草稿未落地前每次渲染都会重新播种，
// 随机 uid 会让输入框在每次重渲染时被卸载重建。
function sceneToDraft(scene: RewardedAdScene, index: number): SceneDraft {
  const rewards: SceneDraft["rewards"] = {};
  scene.rewards.forEach((reward) => {
    rewards[reward.type] = {
      amount: String(reward.amount ?? 1),
      refId: reward.refId ?? 0,
      feature: reward.feature ?? ""
    };
  });
  return {
    uid: `scene-${index}`,
    key: scene.key,
    name: scene.name,
    placementId: scene.placementId,
    enabled: scene.enabled,
    dailyLimit: String(scene.dailyLimit ?? 0),
    cooldownSeconds: String(scene.cooldownSeconds ?? 0),
    rewards,
    features: scene.membership?.features ?? [],
    // 没设置过的场景按免广告（与服务端默认一致）
    adFree: scene.membership?.adFree ?? true,
    lifetime: lifetimeToDraft(scene.lifetime)
  };
}

function placementToDraft(placement: RewardedAdPlacement, index: number): PlacementDraft {
  return {
    uid: `placement-${index}`,
    id: placement.id,
    savedId: placement.id,
    name: placement.name,
    key: "",
    clearKey: false
  };
}

/** 老后端不回 placements 时，从场景里推出广告位（与新后端读配置时的补登同一口径） */
function savedPlacements(config?: RewardedAdConfig): RewardedAdPlacement[] {
  if (config?.placements) return config.placements;
  const seen = new Set<string>();
  const result: RewardedAdPlacement[] = [];
  for (const scene of config?.scenes ?? []) {
    if (!scene.placementId || seen.has(scene.placementId)) continue;
    seen.add(scene.placementId);
    result.push({
      id: scene.placementId,
      name: scene.name,
      hasSecurityKey: false,
      keySource: config?.hasSecurityKey ? "default" : "missing",
      scenes: [],
      callbacks: 0
    });
  }
  return result;
}

export function seedDraft(scope: string, config?: RewardedAdConfig): Draft {
  return {
    scope,
    enabled: config?.enabled ?? false,
    provider: config?.provider || "apex",
    providerAppId: config?.providerAppId ?? "",
    defaultKey: "",
    clearDefaultKey: false,
    verifyMode: config?.verifyMode ?? "dual",
    dailyLimit: String(config?.dailyLimit ?? 0),
    placements: savedPlacements(config).map(placementToDraft),
    scenes: (config?.scenes ?? []).map(sceneToDraft)
  };
}

export function blankScene(existing: SceneDraft[], placementId: string): SceneDraft {
  const first = existing.length === 0;
  let index = existing.length + 1;
  while (existing.some((scene) => scene.key === `scene${index}`)) index += 1;
  return {
    uid: nextUid("scene"),
    key: first ? "vip" : `scene${index}`,
    name: first ? "看广告领会员" : "",
    placementId,
    enabled: true,
    dailyLimit: "3",
    cooldownSeconds: "60",
    rewards: { vip_days: { amount: "1", refId: 0, feature: "" } },
    features: [],
    adFree: true,
    lifetime: lifetimeToDraft()
  };
}

export function blankPlacement(values?: Partial<Pick<PlacementDraft, "id" | "name" | "key">>): PlacementDraft {
  return {
    uid: nextUid("placement"),
    id: values?.id ?? "",
    savedId: "",
    name: values?.name ?? "",
    key: values?.key ?? "",
    clearKey: false
  };
}

export function buildRewards(scene: SceneDraft, catalog: CardKeyRewardSpec[]): CardKeyReward[] {
  return catalog
    .filter((spec) => scene.rewards[spec.type])
    .map((spec) => {
      const value = scene.rewards[spec.type];
      switch (spec.value) {
        case "ref":
          return { type: spec.type, refId: Number(value.refId) };
        case "feature":
          return { type: spec.type, amount: Number(value.amount), feature: value.feature };
        default:
          return { type: spec.type, amount: Number(value.amount) };
      }
    });
}

export function buildPayload(draft: Draft, catalog: CardKeyRewardSpec[]): SaveRewardedAdConfigPayload {
  return {
    enabled: draft.enabled,
    provider: draft.provider,
    providerAppId: draft.providerAppId.trim(),
    securityKey: draft.defaultKey.trim() || undefined,
    clearSecurityKey: draft.clearDefaultKey || undefined,
    verifyMode: draft.verifyMode,
    dailyLimit: Number(draft.dailyLimit) || 0,
    placements: draft.placements.map((placement) => ({
      id: placement.id.trim(),
      previousId: placement.savedId && placement.savedId !== placement.id.trim() ? placement.savedId : undefined,
      name: placement.name.trim(),
      securityKey: placement.key.trim() || undefined,
      clearSecurityKey: placement.clearKey || undefined
    })),
    scenes: draft.scenes.map((scene) => ({
      key: scene.key.trim(),
      name: scene.name.trim(),
      placementId: scene.placementId.trim(),
      enabled: scene.enabled,
      rewards: buildRewards(scene, catalog),
      dailyLimit: Number(scene.dailyLimit) || 0,
      cooldownSeconds: Number(scene.cooldownSeconds) || 0,
      // 功能只对会员天数生效：没勾会员天数时一并清掉，否则服务端会拒收
      membership: {
        features: scene.rewards[VIP_DAYS] ? scene.features : [],
        adFree: scene.adFree
      },
      lifetime: lifetimeToPayload(scene.lifetime)
    }))
  };
}

// ── Key 状态 ──

/** 草稿里的一个广告位保存后会用哪把 Key 验签（「new」= 刚填了自己的 Key，保存后生效） */
export type DraftKeyState = "new" | "own" | "default" | "missing";

/** 保存后默认 Key 还在不在 */
export function draftHasDefaultKey(draft: Draft, config?: RewardedAdConfig) {
  if (draft.defaultKey.trim()) return true;
  return Boolean(config?.hasSecurityKey) && !draft.clearDefaultKey;
}

export function draftKeyState(
  placement: PlacementDraft,
  saved: RewardedAdPlacement | undefined,
  hasDefault: boolean
): DraftKeyState {
  if (placement.key.trim()) return "new";
  if (saved?.hasSecurityKey && !placement.clearKey) return "own";
  return hasDefault ? "default" : "missing";
}

// ── 保存前检查 ──

export type Issue = {
  level: "error" | "warning";
  text: string;
  /** 出问题的条目（场景或广告位的 uid），界面据此标红 */
  uid?: string;
};

/**
 * 保存前的检查，同时驱动「上线检查」与条目上的标红。error 挡保存，warning 只提示。
 * 与后端的校验同口径（后端仍是最终裁决），在这里先拦下是为了给出能直接照着改的位置。
 */
export function collectIssues(
  draft: Draft,
  config: RewardedAdConfig | undefined,
  catalog: CardKeyRewardSpec[]
): Issue[] {
  const issues: Issue[] = [];
  const savedById = new Map((config?.placements ?? []).map((item) => [item.id, item]));
  const hasDefault = draftHasDefaultKey(draft, config);

  const ids = new Map<string, number>();
  draft.placements.forEach((placement) => {
    const id = placement.id.trim();
    ids.set(id, (ids.get(id) ?? 0) + 1);
  });
  draft.placements.forEach((placement, index) => {
    const id = placement.id.trim();
    const label = placement.name.trim() || id || `第 ${index + 1} 个广告位`;
    if (!id) {
      issues.push({ level: "error", text: `${label}未填写广告位 ID`, uid: placement.uid });
    } else if (!PLACEMENT_ID_PATTERN.test(id)) {
      issues.push({ level: "error", text: `广告位「${label}」的 ID 只能包含字母、数字、下划线或短横线`, uid: placement.uid });
    } else if ((ids.get(id) ?? 0) > 1) {
      issues.push({ level: "error", text: `广告位 ID ${id} 重复登记`, uid: placement.uid });
    }
    if (id && !draft.scenes.some((scene) => scene.placementId === id)) {
      issues.push({ level: "warning", text: `广告位「${label}」还没有场景使用`, uid: placement.uid });
    }
  });
  if (draft.placements.length > MAX_PLACEMENTS) {
    issues.push({ level: "error", text: `最多登记 ${MAX_PLACEMENTS} 个广告位` });
  }

  const sceneKeys = new Map<string, number>();
  draft.scenes.forEach((scene) => sceneKeys.set(scene.key.trim(), (sceneKeys.get(scene.key.trim()) ?? 0) + 1));
  draft.scenes.forEach((scene, index) => {
    const label = scene.name.trim() || scene.key.trim() || `第 ${index + 1} 个场景`;
    if (!scene.name.trim()) issues.push({ level: "error", text: `${label}未填写名称`, uid: scene.uid });
    if (!scene.key.trim()) {
      issues.push({ level: "error", text: `场景「${label}」未填写标识`, uid: scene.uid });
    } else if ((sceneKeys.get(scene.key.trim()) ?? 0) > 1) {
      issues.push({ level: "error", text: `场景标识 ${scene.key.trim()} 重复`, uid: scene.uid });
    }
    if (!scene.placementId.trim()) {
      issues.push({ level: "error", text: `场景「${label}」未选择广告位`, uid: scene.uid });
    } else if (!draft.placements.some((placement) => placement.id.trim() === scene.placementId)) {
      issues.push({ level: "error", text: `场景「${label}」选择的广告位已不在列表中`, uid: scene.uid });
    }
    if (buildRewards(scene, catalog).length === 0) {
      issues.push({ level: "error", text: `场景「${label}」至少需要一项奖励`, uid: scene.uid });
    }
    const featureless = catalog.find(
      (spec) => spec.value === "feature" && scene.rewards[spec.type] && !scene.rewards[spec.type].feature
    );
    if (featureless) {
      issues.push({ level: "error", text: `场景「${label}」的${featureless.label}未选择功能`, uid: scene.uid });
    }
    if (
      OWNABLE.some((type) => scene.rewards[type]) &&
      scene.lifetime.policy === "replace" &&
      Object.keys(scene.lifetime.rewards).length === 0
    ) {
      issues.push({ level: "error", text: `场景「${label}」改发固定权益时至少选择一项`, uid: scene.uid });
    }
  });
  if (draft.scenes.length > MAX_SCENES) {
    issues.push({ level: "error", text: `最多配置 ${MAX_SCENES} 个场景` });
  }

  if (draft.enabled) {
    const active = draft.scenes.filter((scene) => scene.enabled);
    if (active.length === 0) {
      issues.push({ level: "error", text: "启用前至少需要一个启用中的场景" });
    }
    if (draft.verifyMode !== "client") {
      const reported = new Set<string>();
      for (const scene of active) {
        const placement = draft.placements.find((item) => item.id.trim() === scene.placementId);
        if (!placement || reported.has(placement.uid)) continue;
        const state = draftKeyState(placement, savedById.get(placement.savedId), hasDefault);
        if (state === "missing") {
          reported.add(placement.uid);
          issues.push({
            level: "error",
            text: `广告位「${placement.name.trim() || placement.id}」还没有 Security Key，回调会验签失败`,
            uid: placement.uid
          });
        }
      }
    }
  }
  if (draft.verifyMode === "client") {
    issues.push({ level: "warning", text: "当前校验模式不验证平台回调，任何人都能伪造观看，仅限联调使用" });
  }
  return issues;
}

// ── 批量登记 ──

export type PastedPlacement = { name: string; id: string; key: string };

const ID_TOKEN = /^\d{6,64}$/;
const KEY_TOKEN = /^(?=.*[A-Za-z])[A-Za-z0-9]{16,256}$/;

/**
 * 解析从 Apex 后台复制出来的广告位信息。每个广告位三项：名称、广告位 ID（纯数字）、Security Key（字母数字混合）。
 *
 * 认得几种常见排法：一项一行（广告位之间空不空行都行），或一个广告位一行、各项用制表符 / 逗号 / 竖线 / 空格隔开。
 * 按「这一项在当前条目里已经有了」切分条目，所以三项的先后顺序不限。
 */
export function parsePlacementPaste(text: string): PastedPlacement[] {
  const result: PastedPlacement[] = [];
  let current: PastedPlacement = { name: "", id: "", key: "" };
  const flush = () => {
    if (current.id || current.key) result.push(current);
    current = { name: "", id: "", key: "" };
  };
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) {
      flush();
      continue;
    }
    const parts = line.split(/[\t,，|｜]+|\s{2,}/).map((part) => part.trim()).filter(Boolean);
    // 单个空格分隔的「名称 ID Key」也认：只在拆出来能认出 ID 或 Key 时才按空格拆，避免把带空格的名称拆碎
    const tokens =
      parts.length === 1 && line.includes(" ") && line.split(/\s+/).some((part) => ID_TOKEN.test(part) || KEY_TOKEN.test(part))
        ? line.split(/\s+/)
        : parts;
    const names: string[] = [];
    for (const token of tokens) {
      if (ID_TOKEN.test(token)) {
        if (current.id) flush();
        current.id = token;
      } else if (KEY_TOKEN.test(token)) {
        if (current.key) flush();
        current.key = token;
      } else {
        names.push(token);
      }
    }
    if (names.length > 0) {
      if (current.name && (current.id || current.key)) flush();
      current.name = [current.name, ...names].filter(Boolean).join(" ");
    }
  }
  flush();
  return result;
}
