"use client";

import { CalendarClock, Crown, Gem, Infinity as InfinityIcon, ShieldCheck, Sparkles, Star, Users } from "lucide-react";
import {
  discountLabel,
  formatCompactPoints,
  formatYuan,
  packageTone
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import type { AIGatewayPackagePayload, AIGatewaySettings } from "@/lib/api/ai-gateway";
import { formatDateTimeMinute, parseInstant } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/** 报价时代入的身份：非会员不打折；会员按档位或通用会员的 packDiscount / packBonusRate。 */
export type QuoteIdentity = {
  key: string;
  label: string;
  isVip: boolean;
  tierId?: number;
  packDiscount: number;
  packBonusRate: number;
};

export const GUEST: QuoteIdentity = { key: "none", label: "非会员", isVip: false, packDiscount: 1, packBonusRate: 0 };

/** 由设置算出各身份的额度包优惠（档位按优先级在前，末尾是通用会员）。 */
export function quoteIdentities(settings: AIGatewaySettings | undefined, tierName: (id: number) => string): QuoteIdentity[] {
  const tiers = (settings?.memberTiers ?? []).map((tier) => ({
    key: `tier:${tier.tierId}`,
    label: tier.label || tierName(tier.tierId),
    isVip: true,
    tierId: tier.tierId,
    packDiscount: Number(tier.packDiscount) || 1,
    packBonusRate: Number(tier.packBonusRate) || 0
  }));
  return [
    GUEST,
    ...tiers,
    {
      key: "vip",
      label: "通用会员",
      isVip: true,
      packDiscount: Number(settings?.vipPackDiscount) || 1,
      packBonusRate: Number(settings?.vipPackBonusRate) || 0
    }
  ];
}

export type PackageQuote = {
  finalPrice: number;
  memberBonus: number;
  totalPoints: number;
  /** 元 / 万额度。 */
  unitPrice: number;
  saving: number;
  discounted: boolean;
};

/**
 * 与后端 aigateway.QuotePackage 同一条公式（docs/ai-billing.md「报价」），只用于预览：
 * finalPrice = memberDiscount ? floor₂(price × packDiscount)（至少 0.01，免费包保持 0）: price；
 * memberBonus = memberDiscount ? floor(points × packBonusRate) : 0。
 */
export function quotePackage(pkg: Pick<AIGatewayPackagePayload, "points" | "bonusPoints" | "price" | "memberDiscount">, identity: QuoteIdentity): PackageQuote {
  const price = Number(pkg.price) || 0;
  const points = Number(pkg.points) || 0;
  const bonus = Number(pkg.bonusPoints) || 0;
  const apply = pkg.memberDiscount && identity.isVip;
  const cents = Math.round(price * 100);
  const finalCents = !apply || cents === 0 ? cents : Math.max(1, Math.floor(cents * identity.packDiscount + 1e-6));
  const finalPrice = finalCents / 100;
  const memberBonus = apply ? Math.floor(points * identity.packBonusRate + 1e-6) : 0;
  const totalPoints = points + bonus + memberBonus;
  return {
    finalPrice,
    memberBonus,
    totalPoints,
    unitPrice: totalPoints > 0 ? (finalPrice / totalPoints) * 10_000 : 0,
    saving: Math.max(0, price - finalPrice),
    discounted: apply && (finalCents !== cents || memberBonus > 0)
  };
}

export type SaleState = { key: "off" | "upcoming" | "ended" | "soldout" | "live"; label: string; tone: string };

export function saleState(pkg: Pick<AIGatewayPackagePayload, "enabled" | "startsAt" | "endsAt" | "stock"> & { sold?: number }, now: number): SaleState {
  if (!pkg.enabled) return { key: "off", label: "已下架", tone: "bg-muted text-muted-foreground" };
  const starts = parseInstant(pkg.startsAt)?.getTime();
  const ends = parseInstant(pkg.endsAt)?.getTime();
  if (starts && starts > now) return { key: "upcoming", label: "未开始", tone: "bg-sky-500/10 text-sky-700 dark:text-sky-300" };
  if (ends && ends <= now) return { key: "ended", label: "已结束", tone: "bg-stone-500/10 text-stone-600 dark:text-stone-300" };
  if (pkg.stock > 0 && (pkg.sold ?? 0) >= pkg.stock) return { key: "soldout", label: "售罄", tone: "bg-rose-500/10 text-rose-700 dark:text-rose-300" };
  return { key: "live", label: "在售", tone: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" };
}

/**
 * 额度包卡片：长成用户在商店里看到的样子（配色头、角标、主推描边、划线价、到手额度、有效期），
 * 管理端再在底部 footer 里挂统计与操作。
 */
export function PackageCard({
  pkg,
  identity = GUEST,
  tierName,
  now,
  footer,
  className
}: {
  pkg: AIGatewayPackagePayload & { sold?: number };
  identity?: QuoteIdentity;
  tierName: (id: number) => string;
  now: number;
  footer?: React.ReactNode;
  className?: string;
}) {
  const tone = packageTone(pkg.tone);
  const quote = quotePackage(pkg, identity);
  const state = saleState(pkg, now);
  const price = Number(pkg.price) || 0;
  const strike = Number(pkg.originalPrice) > 0 ? Number(pkg.originalPrice) : quote.discounted && quote.finalPrice < price ? price : 0;
  const points = Number(pkg.points) || 0;
  const bonus = Number(pkg.bonusPoints) || 0;
  const stockRatio = pkg.stock > 0 ? Math.min(1, (pkg.sold ?? 0) / pkg.stock) : 0;
  const tierIds = pkg.tierIds ?? [];
  const restricted = tierIds.length > 0;

  return (
    <div
      className={cn(
        "group relative flex flex-col overflow-hidden rounded-2xl border bg-card transition-shadow hover:shadow-md",
        pkg.featured && cn("ring-2", tone.ring),
        state.key !== "live" && "opacity-[0.88]",
        className
      )}
    >
      <div className={cn("relative px-4 pb-4 pt-3.5 text-white", tone.solid)}>
        <div className="pointer-events-none absolute -right-6 -top-10 size-32 rounded-full bg-white/10" />
        <div className="pointer-events-none absolute -bottom-12 right-10 size-24 rounded-full bg-white/10" />
        <div className="relative flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              {pkg.featured ? <Star className="size-3.5 fill-white" /> : null}
              <h4 className="truncate text-sm font-semibold">{pkg.name || "未命名额度包"}</h4>
            </div>
            {pkg.subtitle ? <p className="mt-0.5 line-clamp-1 text-[11px] text-white/80">{pkg.subtitle}</p> : null}
          </div>
          {pkg.badge ? (
            <span className="shrink-0 rounded-full bg-white px-2 py-0.5 text-[10px] font-bold text-foreground shadow-sm">
              <span className={tone.text}>{pkg.badge}</span>
            </span>
          ) : null}
        </div>
        <div className="relative mt-3 flex items-baseline gap-1.5">
          <span className="font-mono text-3xl font-bold tracking-tight tabular-nums">{formatCompactPoints(quote.totalPoints)}</span>
          <span className="text-xs text-white/80">额度</span>
        </div>
        <div className="relative mt-0.5 flex flex-wrap gap-x-2 text-[10px] text-white/80">
          <span>{formatPoints(points)}</span>
          {bonus > 0 ? <span>+ 赠 {formatPoints(bonus)}</span> : null}
          {quote.memberBonus > 0 ? <span>+ {identity.label}多送 {formatPoints(quote.memberBonus)}</span> : null}
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-3 p-4">
        <div className="flex items-end justify-between gap-2">
          <div>
            <div className="flex items-baseline gap-1.5">
              <span className={cn("font-mono text-2xl font-bold tabular-nums", tone.text)}>
                {quote.finalPrice === 0 ? "免费" : formatYuan(quote.finalPrice)}
              </span>
              {strike > quote.finalPrice ? <span className="font-mono text-xs text-muted-foreground line-through">{formatYuan(strike)}</span> : null}
            </div>
            <div className="text-[10px] text-muted-foreground">
              {quote.totalPoints > 0 && quote.finalPrice > 0 ? `${formatYuan(quote.unitPrice)} / 万额度` : "—"}
            </div>
          </div>
          {quote.discounted ? (
            <span className="rounded-md bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700 dark:text-amber-300">
              {identity.label} {discountLabel(identity.packDiscount)}
            </span>
          ) : pkg.memberDiscount ? (
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <Crown className="size-3" />
              会员有优惠
            </span>
          ) : null}
        </div>

        {pkg.description ? <p className="line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">{pkg.description}</p> : null}

        <div className="flex flex-wrap gap-1.5 text-[10px]">
          <Chip icon={pkg.validityDays > 0 ? CalendarClock : InfinityIcon}>{pkg.validityDays > 0 ? `${pkg.validityDays} 天有效` : "永久有效"}</Chip>
          {restricted ? (
            <Chip icon={Gem}>{tierIds.map(tierName).join(" / ")} 专享</Chip>
          ) : pkg.audience === "vip" ? (
            <Chip icon={Crown}>会员专享</Chip>
          ) : (
            <Chip icon={Users}>所有人</Chip>
          )}
          {pkg.perUserLimit > 0 ? <Chip icon={ShieldCheck}>每人限 {pkg.perUserLimit} 次</Chip> : null}
          {pkg.monthlyLimit > 0 ? <Chip icon={ShieldCheck}>每月限 {pkg.monthlyLimit} 次</Chip> : null}
          {pkg.memberDiscount ? <Chip icon={Sparkles}>参与会员折扣</Chip> : null}
        </div>

        {pkg.stock > 0 ? (
          <div className="space-y-1">
            <div className="flex justify-between text-[10px] text-muted-foreground">
              <span>已售 {pkg.sold ?? 0}</span>
              <span>库存 {pkg.stock}</span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-muted">
              <div className={cn("h-full rounded-full", tone.solid)} style={{ width: `${stockRatio * 100}%` }} />
            </div>
          </div>
        ) : null}

        <div className="mt-auto flex flex-wrap items-center gap-1.5 text-[10px]">
          <span className={cn("rounded-md px-1.5 py-0.5 font-medium", state.tone)}>{state.label}</span>
          {pkg.startsAt || pkg.endsAt ? (
            <span className="text-muted-foreground">
              {pkg.startsAt ? formatDateTimeMinute(pkg.startsAt) : "即日"} 至 {pkg.endsAt ? formatDateTimeMinute(pkg.endsAt) : "长期"}
            </span>
          ) : null}
        </div>
      </div>
      {footer ? <div className="border-t bg-muted/30 px-4 py-2.5">{footer}</div> : null}
    </div>
  );
}

function Chip({ icon: Icon, children }: { icon: typeof Users; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-muted-foreground">
      <Icon className="size-3" />
      {children}
    </span>
  );
}
