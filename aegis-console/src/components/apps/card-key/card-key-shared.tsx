"use client";

import { Badge } from "@/components/ui/badge";
import type {
  CardKey,
  CardKeyKind,
  CardKeyRedemption,
  CardKeyReward,
  CardKeyRewardSpec,
  CardKeyValidityMode
} from "@/lib/api/card-key";

/**
 * 卡密域的**展示口径**：形态、状态、有效期、权益摘要。
 *
 * 集中在一处的理由与会员域相同：同一张卡在批次页显示「授权卡」、在核销记录页
 * 显示「登录卡」的话，看的人无从判断这是不是两件事。
 * 枚举文案与后端 `internal/domain/cardkey` 的常量一一对应。
 */

const KIND_META: Record<CardKeyKind, { label: string; hint: string }> = {
  login: { label: "授权卡", hint: "卡即登录凭证，首次使用自动建号并绑定；有授权期与可绑设备数" },
  redeem: { label: "兑换卡", hint: "给已登录用户发权益，核销一次即作废" }
};

export function cardKindLabel(kind: CardKeyKind) {
  return KIND_META[kind]?.label ?? kind;
}

export function cardKindHint(kind: CardKeyKind) {
  return KIND_META[kind]?.hint ?? "";
}

export function CardKindBadge({ kind }: { kind: CardKeyKind }) {
  return (
    <Badge variant={kind === "login" ? "info" : "secondary"} size="sm">
      {cardKindLabel(kind)}
    </Badge>
  );
}

/**
 * 卡的显示状态。
 *
 * 「已过期」不是后端的一档状态，它是到期时间比出来的结论 —— 这里也必须如实算出来，
 * 否则运营会拿着一张写着「未使用」的过期卡去排查客诉。
 *
 * 写成独立函数而不是在组件里直接比 `Date.now()`：后者是渲染期的不纯调用
 * （`react-hooks/purity` 会拒绝）。这里的时间只用于展示派生，真正的判定在服务端。
 */
export function resolveCardState(card: CardKey): {
  label: string;
  variant: "success" | "warning" | "danger" | "info" | "secondary";
} {
  if (card.status === "disabled") return { label: "已作废", variant: "danger" };
  if (isExpired(card.expiresAt)) return { label: "已过期", variant: "warning" };
  switch (card.status) {
    case "unused":
      return { label: "未使用", variant: "secondary" };
    case "active":
      return { label: "使用中", variant: "success" };
    case "used":
      return { label: "已核销", variant: "info" };
    default:
      return { label: card.status, variant: "secondary" };
  }
}

export function CardStateBadge({ card }: { card: CardKey }) {
  const state = resolveCardState(card);
  return (
    <Badge variant={state.variant} size="sm">
      {state.label}
    </Badge>
  );
}

export function isExpired(expiresAt?: string) {
  if (!expiresAt) return false;
  const time = new Date(expiresAt).getTime();
  return Number.isFinite(time) && time <= Date.now();
}

const VALIDITY_META: Record<CardKeyValidityMode, string> = {
  permanent: "永不过期",
  fixed_until: "统一到期",
  days_from_first_use: "激活即计时"
};

export function validityLabel(mode: CardKeyValidityMode, days: number, until?: string) {
  switch (mode) {
    case "days_from_first_use":
      return `激活后 ${days} 天`;
    case "fixed_until":
      return until ? `至 ${formatCardDate(until)}` : VALIDITY_META.fixed_until;
    default:
      return VALIDITY_META.permanent;
  }
}

/**
 * 一项权益的人话摘要。
 *
 * 依据是后端下发的目录（label / unit / value），不在这里另抄一份枚举 ——
 * 后端加一档权益时，控制台应当零改动就能把它显示出来。
 */
export function describeReward(reward: CardKeyReward, catalog: CardKeyRewardSpec[]): string {
  const spec = catalog.find((item) => item.type === reward.type);
  if (!spec) return reward.type;
  switch (spec.value) {
    case "amount":
      return `${spec.label} ${reward.amount ?? 0} ${spec.unit ?? ""}`.trim();
    case "money":
      return `${spec.label} ${reward.money ?? "0"}`;
    default:
      return spec.label;
  }
}

export function RewardSummary({
  rewards,
  catalog
}: {
  rewards: CardKeyReward[];
  catalog: CardKeyRewardSpec[];
}) {
  if (!rewards.length) {
    return <span className="text-xs text-muted-foreground">不带权益（卡本身即授权）</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {rewards.map((reward) => (
        <Badge key={reward.type} variant="outline" size="sm" className="font-normal">
          {describeReward(reward, catalog)}
        </Badge>
      ))}
    </div>
  );
}

const SOURCE_META: Record<CardKeyRedemption["source"], string> = {
  redeem: "用户兑换",
  login: "授权卡激活",
  admin: "管理员代兑"
};

export function redemptionSourceLabel(source: CardKeyRedemption["source"]) {
  return SOURCE_META[source] ?? source;
}

export function formatCardDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

/** 剩余授权时长。已过期与永久各有明确文案，不要用「—」把两者混在一起。 */
export function formatRemaining(expiresAt?: string) {
  if (!expiresAt) return "永久";
  const end = new Date(expiresAt).getTime();
  if (!Number.isFinite(end)) return "—";
  const diff = end - Date.now();
  if (diff <= 0) return "已过期";
  const days = Math.floor(diff / 86_400_000);
  if (days >= 1) return `${days} 天`;
  const hours = Math.max(1, Math.floor(diff / 3_600_000));
  return `${hours} 小时`;
}
