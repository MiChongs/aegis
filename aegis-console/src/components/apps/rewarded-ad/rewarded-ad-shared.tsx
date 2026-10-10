"use client";

import { Badge } from "@/components/ui/badge";
import type { CardKeyReward, CardKeyRewardSpec } from "@/lib/api/card-key";
import type { RewardedAdScene, RewardedAdVerifyMode, RewardedAdViewStatus } from "@/lib/api/rewarded-ad";

/** 观看记录状态 → 标签与徽标色。 */
const STATUS_META: Record<RewardedAdViewStatus, { label: string; variant: "success" | "warning" | "danger" }> = {
  granted: { label: "已发放", variant: "success" },
  pending: { label: "待确认", variant: "warning" },
  rejected: { label: "未发放", variant: "danger" }
};

export function RewardedAdStatusBadge({ status }: { status: RewardedAdViewStatus }) {
  const meta = STATUS_META[status] ?? { label: status, variant: "warning" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export const REWARDED_AD_STATUS_OPTIONS: Array<{ value: RewardedAdViewStatus; label: string }> = [
  { value: "granted", label: "已发放" },
  { value: "pending", label: "待确认" },
  { value: "rejected", label: "未发放" }
];

/** 拒发原因。与后端 rewardedad.Reason* 一一对应。 */
const REASON_LABELS: Record<string, string> = {
  disabled: "未启用",
  scene_unavailable: "场景已停用",
  daily_limit: "超出每日总次数",
  scene_daily_limit: "超出场景每日次数",
  cooldown: "冷却中",
  user_mismatch: "账号不一致",
  client_unverified: "SDK 校验未通过",
  user_not_found: "用户不存在"
};

export function rewardedAdReasonLabel(reason?: string) {
  if (!reason) return "";
  return REASON_LABELS[reason] ?? reason;
}

export const VERIFY_MODE_META: Record<RewardedAdVerifyMode, { title: string; description: string }> = {
  dual: { title: "双方确认", description: "平台回调验签 + 客户端上报，两方到齐才发放" },
  server: { title: "仅平台回调", description: "平台回调验签通过即发放" },
  client: { title: "仅客户端上报", description: "不验签，仅用于联调" }
};

/** 一组权益的一句话摘要（会员套餐显示套餐名）。 */
export function describeRewards(
  rewards: CardKeyReward[],
  catalog: CardKeyRewardSpec[],
  planNames: Record<number, string>
) {
  if (rewards.length === 0) return "未配置权益";
  return rewards
    .map((reward) => {
      const spec = catalog.find((item) => item.type === reward.type);
      if (!spec) return reward.type;
      if (spec.value === "ref") {
        return planNames[reward.refId ?? 0] ? `${spec.label}「${planNames[reward.refId ?? 0]}」` : spec.label;
      }
      return `${spec.label} ${reward.amount ?? 0} ${spec.unit ?? ""}`.trim();
    })
    .join("、");
}

/**
 * 场景送出的会员带什么：功能（只对会员天数生效，按展示名）与是否免广告。
 * 场景不送会员时返回空串。
 */
export function describeMembership(scene: RewardedAdScene, featureNames: Record<string, string>) {
  const types = scene.rewards.map((reward) => reward.type);
  const days = types.includes("vip_days");
  if (!days && !types.includes("vip_plan")) return "";
  const parts: string[] = [];
  const features = scene.membership?.features ?? [];
  if (days && features.length > 0) {
    parts.push(`功能 ${features.map((tag) => featureNames[tag] ?? tag).join("、")}`);
  }
  parts.push(scene.membership?.adFree === false ? "不免广告" : "免广告");
  return parts.join(" · ");
}

/** 冷却秒数的人话形式。 */
export function formatCooldown(seconds: number) {
  if (seconds <= 0) return "不限";
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}
