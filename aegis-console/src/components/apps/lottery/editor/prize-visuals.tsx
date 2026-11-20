"use client";

import {
  Award,
  BadgeCheck,
  Clock,
  Coins,
  Crown,
  Dices,
  Gem,
  Gift,
  Hash,
  KeyRound,
  Package,
  Shirt,
  ShieldOff,
  Smile,
  Sparkles,
  Ticket,
  Wallet,
  type LucideIcon
} from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * 奖品类型的视觉口径：图标、色调与概率分布条上的颜色。
 *
 * 编辑器里格子、类型选择、分布条与右侧洞察都按这张表取色，同一种奖品在哪儿都是同一个颜色；
 * 目录里出现表外的新类型时回落到中性色，不会因为缺一行而崩。
 */
export type KindVisual = {
  icon: LucideIcon;
  /** 色调底 + 前景（图标块、选中的类型卡） */
  tone: string;
  /** 分布条与图例的实色 */
  fill: string;
  /** 格子选中时的描边 */
  ring: string;
  /** SVG 描边色（洞察里的环形图） */
  stroke: string;
};

const KIND_VISUALS: Record<string, KindVisual> = {
  none: {
    icon: Smile,
    tone: "bg-zinc-500/10 text-zinc-500 dark:text-zinc-400",
    fill: "bg-zinc-300 dark:bg-zinc-600",
    ring: "ring-zinc-400/60",
    stroke: "stroke-zinc-300 dark:stroke-zinc-600"
  },
  points: {
    icon: Coins,
    tone: "bg-amber-500/12 text-amber-600 dark:text-amber-400",
    fill: "bg-amber-400",
    ring: "ring-amber-500/60",
    stroke: "stroke-amber-400"
  },
  experience: {
    icon: Sparkles,
    tone: "bg-sky-500/12 text-sky-600 dark:text-sky-400",
    fill: "bg-sky-400",
    ring: "ring-sky-500/60",
    stroke: "stroke-sky-400"
  },
  vip_days: {
    icon: Crown,
    tone: "bg-violet-500/12 text-violet-600 dark:text-violet-400",
    fill: "bg-violet-400",
    ring: "ring-violet-500/60",
    stroke: "stroke-violet-400"
  },
  vip_plan: {
    icon: BadgeCheck,
    tone: "bg-fuchsia-500/12 text-fuchsia-600 dark:text-fuchsia-400",
    fill: "bg-fuchsia-400",
    ring: "ring-fuchsia-500/60",
    stroke: "stroke-fuchsia-400"
  },
  feature: {
    icon: KeyRound,
    tone: "bg-rose-500/12 text-rose-600 dark:text-rose-400",
    fill: "bg-rose-400",
    ring: "ring-rose-500/60",
    stroke: "stroke-rose-400"
  },
  vip_lifetime: {
    icon: Gem,
    tone: "bg-purple-500/12 text-purple-600 dark:text-purple-400",
    fill: "bg-purple-500",
    ring: "ring-purple-500/60",
    stroke: "stroke-purple-500"
  },
  feature_time: {
    icon: Clock,
    tone: "bg-pink-500/12 text-pink-600 dark:text-pink-400",
    fill: "bg-pink-400",
    ring: "ring-pink-500/60",
    stroke: "stroke-pink-400"
  },
  ad_free: {
    icon: ShieldOff,
    tone: "bg-lime-500/12 text-lime-700 dark:text-lime-400",
    fill: "bg-lime-400",
    ring: "ring-lime-500/60",
    stroke: "stroke-lime-400"
  },
  lottery_draws: {
    icon: Ticket,
    tone: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400",
    fill: "bg-emerald-400",
    ring: "ring-emerald-500/60",
    stroke: "stroke-emerald-400"
  },
  balance: {
    icon: Wallet,
    tone: "bg-teal-500/12 text-teal-600 dark:text-teal-400",
    fill: "bg-teal-400",
    ring: "ring-teal-500/60",
    stroke: "stroke-teal-400"
  },
  manual: {
    icon: Package,
    tone: "bg-orange-500/12 text-orange-600 dark:text-orange-400",
    fill: "bg-orange-400",
    ring: "ring-orange-500/60",
    stroke: "stroke-orange-400"
  },
  number_change: {
    icon: Hash,
    tone: "bg-indigo-500/12 text-indigo-600 dark:text-indigo-400",
    fill: "bg-indigo-400",
    ring: "ring-indigo-500/60",
    stroke: "stroke-indigo-400"
  },
  number_ticket: {
    icon: Award,
    tone: "bg-yellow-500/12 text-yellow-700 dark:text-yellow-400",
    fill: "bg-yellow-400",
    ring: "ring-yellow-500/60",
    stroke: "stroke-yellow-400"
  },
  number_draw: {
    icon: Dices,
    tone: "bg-cyan-500/12 text-cyan-600 dark:text-cyan-400",
    fill: "bg-cyan-400",
    ring: "ring-cyan-500/60",
    stroke: "stroke-cyan-400"
  },
  cosmetic: {
    icon: Shirt,
    tone: "bg-fuchsia-500/12 text-fuchsia-600 dark:text-fuchsia-400",
    fill: "bg-fuchsia-400",
    ring: "ring-fuchsia-500/60",
    stroke: "stroke-fuchsia-400"
  }
};

const FALLBACK_VISUAL: KindVisual = {
  icon: Gift,
  tone: "bg-slate-500/12 text-slate-600 dark:text-slate-400",
  fill: "bg-slate-400",
  ring: "ring-slate-500/60",
  stroke: "stroke-slate-400"
};

export function kindVisual(type: string): KindVisual {
  return KIND_VISUALS[type] ?? FALLBACK_VISUAL;
}

/** 奖品类型的色调图标块 */
export function KindGlyph({ type, size = "md", className }: { type: string; size?: "sm" | "md" | "lg"; className?: string }) {
  const visual = kindVisual(type);
  const Icon = visual.icon;
  return (
    <span
      className={cn(
        "grid shrink-0 place-items-center",
        size === "sm" && "size-6 rounded-md [&>svg]:size-3.5",
        size === "md" && "size-8 rounded-lg [&>svg]:size-4",
        size === "lg" && "size-10 rounded-xl [&>svg]:size-5",
        visual.tone,
        className
      )}
    >
      <Icon />
    </span>
  );
}
