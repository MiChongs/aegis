"use client";

import { Copy } from "lucide-react";
import { useCosmeticRewardNames } from "@/components/apps/cosmetics/cosmetic-reward";
import { cosmeticRewardText } from "@/lib/cosmetic-names";
import { useCouponRewardNames } from "@/components/apps/coupons/coupon-shared";
import { couponRewardText } from "@/lib/coupon-names";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  CardKey,
  CardKeyAudience,
  CardKeyBatch,
  CardKeyBatchStats,
  CardKeyCatalog,
  CardKeyCharset,
  CardKeyKind,
  CardKeyRedemption,
  CardKeyReward,
  CardKeyRewardResult,
  CardKeyRewardSpec,
  CardKeyValidityMode
} from "@/lib/api/card-key";
import { numberTicketName } from "@/lib/api/user-number";
import { cn } from "@/lib/utils";

/**
 * 卡密域的**展示口径**：形态、状态、有效期、领取规则、权益摘要。
 *
 * 集中在一处的理由与会员域相同：同一张卡在批次页显示「授权卡」、在核销记录页
 * 显示「登录卡」的话，看的人无从判断这是不是两件事。
 * 枚举文案与后端 `internal/domain/cardkey` 的常量一一对应。
 */

const KIND_META: Record<CardKeyKind, { label: string; hint: string }> = {
  redeem: { label: "兑换卡", hint: "给已登录用户发权益；可以一卡一用，也可以做成多人各用一次的活动码" },
  login: { label: "授权卡", hint: "卡即登录凭证，首次使用自动建号并绑定；有授权期与可绑设备数" }
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

export const CHARSET_META: Record<CardKeyCharset, { label: string; hint: string; alphabet: string }> = {
  safe: { label: "防混淆", hint: "32 个字符，去掉 I、O、0、1", alphabet: "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" },
  digits: { label: "纯数字", hint: "0–9，适合电话报码与刮刮卡", alphabet: "0123456789" },
  letters: { label: "纯字母", hint: "24 个字母，去掉 I、O", alphabet: "ABCDEFGHJKLMNPQRSTUVWXYZ" }
};

export const AUDIENCE_LABEL: Record<CardKeyAudience, string> = {
  all: "所有人",
  vip: "仅会员",
  non_vip: "仅非会员"
};

export const SOURCE_LABEL: Record<CardKeyRedemption["source"], string> = {
  redeem: "用户兑换",
  login: "授权卡激活",
  admin: "管理员代兑"
};

export function redemptionSourceLabel(source: CardKeyRedemption["source"]) {
  return SOURCE_LABEL[source] ?? source;
}

type StateVariant = "success" | "warning" | "danger" | "info" | "secondary";

/**
 * 卡的显示状态。
 *
 * 「已过期」「部分使用」都不是后端的一档状态：前者是到期时间比出来的结论，
 * 后者是活动码用了一部分（status 仍是 unused，useCount > 0）。这里必须如实算出来，
 * 否则运营会拿着一张写着「未使用」的过期卡去排查客诉。
 *
 * 写成独立函数而不是在组件里直接比 `Date.now()`：后者是渲染期的不纯调用
 * （`react-hooks/purity` 会拒绝）。这里的时间只用于展示派生，真正的判定在服务端。
 */
export function resolveCardState(card: CardKey): { label: string; variant: StateVariant } {
  if (card.status === "disabled") return { label: "已作废", variant: "danger" };
  if (isExpired(card.expiresAt)) return { label: "已过期", variant: "warning" };
  switch (card.status) {
    case "unused":
      return card.useCount > 0
        ? { label: `部分使用 ${card.useCount}/${card.maxUses}`, variant: "info" }
        : { label: "未使用", variant: "secondary" };
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

/** 生效时间还没到 */
export function isPending(startsAt?: string) {
  if (!startsAt) return false;
  const time = new Date(startsAt).getTime();
  return Number.isFinite(time) && time > Date.now();
}

export function validityLabel(mode: CardKeyValidityMode, days: number, until?: string) {
  switch (mode) {
    case "days_from_first_use":
      return `激活后 ${days} 天`;
    case "fixed_until":
      return until ? `至 ${formatCardDate(until)}` : "统一到期";
    default:
      return "永不过期";
  }
}

export const VALIDITY_OPTIONS: Array<{ value: CardKeyValidityMode; label: string; hint: string }> = [
  { value: "permanent", label: "永不过期", hint: "卡与授权都没有终点" },
  { value: "days_from_first_use", label: "激活即计时", hint: "首次使用时开始计算，卖出到使用之间不计入" },
  { value: "fixed_until", label: "统一到期", hint: "整批在同一时刻到期，适合限时活动" }
];

/** 一批卡的领取规则摘要，只列与默认不同的那几条 */
export function batchRuleChips(batch: CardKeyBatch): string[] {
  const chips: string[] = [];
  if (batch.kind === "redeem" && batch.maxUses > 1) chips.push(`活动码 每码 ${formatCount(batch.maxUses)} 人次`);
  if (batch.perUserLimit > 0) chips.push(`每人限兑 ${batch.perUserLimit} 张`);
  if (batch.audience !== "all") chips.push(AUDIENCE_LABEL[batch.audience]);
  if (batch.kind === "login") {
    chips.push(`可绑 ${batch.maxDevices} 台设备`);
    chips.push(batch.selfUnbindLimit > 0 ? `每 30 天可自助解绑 ${batch.selfUnbindLimit} 次` : "不可自助解绑");
  }
  if (batch.origin === "imported") chips.push("导入");
  else if (batch.charset !== "safe") chips.push(CHARSET_META[batch.charset]?.label ?? batch.charset);
  return chips;
}

/** 由后端目录推出一项权益的人话摘要；套餐与功能给出名字而不是 ID */
export function describeReward(reward: CardKeyReward, catalog?: CardKeyCatalog | null): string {
  const spec = catalog?.rewards.find((item) => item.type === reward.type);
  if (!spec) return reward.type;
  switch (spec.value) {
    case "amount":
      return `${spec.label} ${formatCount(reward.amount ?? 0)} ${spec.unit ?? ""}`.trim();
    case "money":
      return `${spec.label} ${reward.money ?? "0"}`;
    case "ref": {
      const plan = catalog?.plans.find((item) => item.id === reward.refId);
      return plan ? `${spec.label} ${plan.name}` : `${spec.label} #${reward.refId ?? 0}`;
    }
    case "number_ticket":
      return `${numberTicketName(reward.refId)} ×${formatCount(reward.amount ?? 0)}`;
    case "feature": {
      const feature = catalog?.features.find((item) => item.tag === reward.feature);
      const name = feature?.name || reward.feature || "";
      return reward.amount ? `${spec.label} ${name} ${reward.amount} 天` : `${spec.label} ${name} 永久`;
    }
    case "cosmetic":
      return cosmeticRewardText(reward.refId, reward.amount);
    case "coupon":
      return couponRewardText(reward.refId, reward.amount);
    default:
      return spec.label;
  }
}

export function RewardSummary({
  rewards,
  catalog,
  className
}: {
  rewards: CardKeyReward[];
  catalog?: CardKeyCatalog | null;
  className?: string;
}) {
  useCosmeticRewardNames();
  useCouponRewardNames();
  if (!rewards.length) {
    return <span className="text-xs text-muted-foreground">无随卡权益</span>;
  }
  return (
    <div className={cn("flex flex-wrap gap-1", className)}>
      {rewards.map((reward, index) => (
        <Badge key={`${reward.type}-${reward.feature ?? ""}-${index}`} variant="outline" size="sm" className="font-normal">
          {describeReward(reward, catalog)}
        </Badge>
      ))}
    </div>
  );
}

/** 实际发放结果（核销流水的 results），排障看的是它而不是卡上配的 rewards */
export function ResultSummary({ results }: { results: CardKeyRewardResult[] }) {
  if (!results?.length) return <span className="text-xs text-muted-foreground">—</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {results.map((result, index) => (
        <Badge
          key={`${result.type}-${index}`}
          variant="outline"
          size="sm"
          className="font-normal"
          title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
        >
          {result.label} {result.detail}
        </Badge>
      ))}
    </div>
  );
}

export function rewardSpecOf(catalog: CardKeyCatalog | null | undefined, type: string): CardKeyRewardSpec | undefined {
  return catalog?.rewards.find((item) => item.type === type);
}

/**
 * 核销进度条：按状态分段（四段互斥，加起来就是总数）。
 *
 * 已核销与使用中是「已经落到用户头上」的；未使用里还有多少能提取、多少已交给渠道、
 * 多少已过期，用条下的数字说明 —— 这几个口径互相重叠，硬塞进一根条里会分不清。
 */
const PROGRESS_SEGMENTS: Array<{ key: keyof CardKeyBatchStats; label: string; className: string }> = [
  { key: "used", label: "已核销", className: "bg-sky-500" },
  { key: "active", label: "使用中", className: "bg-emerald-500" },
  { key: "unused", label: "未使用", className: "bg-muted-foreground/30" },
  { key: "disabled", label: "已作废", className: "bg-red-400" }
];

export function BatchProgress({ stats, total }: { stats?: CardKeyBatchStats; total: number }) {
  const denominator = Math.max(1, stats?.total ?? total);
  const extras: Array<[string, number]> = stats
    ? [
        ["可提取", stats.available],
        ["已提取", stats.distributed],
        ["已过期", stats.expired]
      ]
    : [];
  return (
    <div className="space-y-2">
      <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted">
        {PROGRESS_SEGMENTS.map((segment) => {
          const value = stats?.[segment.key] ?? 0;
          if (!value) return null;
          return (
            <div
              key={segment.key}
              className={cn("h-full", segment.className)}
              style={{ width: `${(value / denominator) * 100}%` }}
              title={`${segment.label} ${value}`}
            />
          );
        })}
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        <span className="tabular-nums">
          共 <span className="font-medium text-foreground">{formatCount(stats?.total ?? total)}</span> 张
        </span>
        {PROGRESS_SEGMENTS.map((segment) => {
          const value = stats?.[segment.key] ?? 0;
          if (!value) return null;
          return (
            <span key={segment.key} className="inline-flex items-center gap-1 tabular-nums">
              <span className={cn("size-2 rounded-full", segment.className)} />
              {segment.label} {formatCount(value)}
            </span>
          );
        })}
        {extras
          .filter(([, value]) => value > 0)
          .map(([label, value]) => (
            <span key={label} className="tabular-nums">
              {label} {formatCount(value)}
            </span>
          ))}
        {stats && stats.uses > stats.used + stats.active ? (
          <span className="tabular-nums">累计 {formatCount(stats.uses)} 人次</span>
        ) : null}
      </div>
    </div>
  );
}

export function formatCount(value?: number | null) {
  return new Intl.NumberFormat("zh-CN").format(value ?? 0);
}

/** 面值：0 不显示 */
export function formatFaceValue(value?: string | null) {
  const number = Number(value ?? 0);
  if (!Number.isFinite(number) || number <= 0) return "";
  return `¥${number.toFixed(2)}`;
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
  if (days >= 1) return `剩 ${days} 天`;
  const hours = Math.max(1, Math.floor(diff / 3_600_000));
  return `剩 ${hours} 小时`;
}

export async function copyText(text: string, success = "已复制") {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(success);
  } catch {
    toast.error("复制失败，请手动选择复制");
  }
}

export function CopyButton({ text, label, className }: { text: string; label?: string; className?: string }) {
  return (
    <Button
      type="button"
      size="icon"
      variant="ghost"
      className={cn("size-6 shrink-0 text-muted-foreground", className)}
      aria-label={label ?? "复制"}
      onClick={(event) => {
        event.stopPropagation();
        void copyText(text);
      }}
    >
      <Copy className="size-3" />
    </Button>
  );
}

/** 空列表占位 */
export function EmptyHint({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
      {children}
    </p>
  );
}
