"use client";

import type { CSSProperties, ReactNode } from "react";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import {
  NUMBER_TIERS,
  numberRightName,
  numberTierOf,
  type NumberDrawStatus,
  type NumberHoldKind,
  type NumberRight,
  type NumberRights,
  type NumberTierSpec
} from "@/lib/api/user-number";
import { cn } from "@/lib/utils";

/**
 * 用户号码的共享展示件：等级徽章、号码文本、换号方式 / 占用 / 摇号单状态文案、权益摘要。
 * 号码管理区块与用户详情的号码区块共用这一份，同一个号在两处长得一样。
 *
 * 等级颜色来自后端目录的建议主色（usernumber.Tiers()），客户端徽章用的也是这一份；
 * 底色与描边按主色取 12% / 30% 混合，深浅两档都可读。
 */

export function tierSpec(level: number | null | undefined, tiers?: NumberTierSpec[] | null): NumberTierSpec {
  return tiers?.find((tier) => tier.level === level) ?? numberTierOf(level);
}

export function tierStyle(color: string): CSSProperties {
  return {
    color,
    backgroundColor: `color-mix(in srgb, ${color} 12%, transparent)`,
    boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${color} 30%, transparent)`
  };
}

export function NumberTierBadge({
  tier,
  tiers,
  className
}: {
  tier: number | null | undefined;
  tiers?: NumberTierSpec[] | null;
  className?: string;
}) {
  const spec = tierSpec(tier, tiers);
  return (
    <span
      className={cn("inline-flex shrink-0 items-center rounded-md px-1.5 py-0.5 text-[11px] leading-4 font-medium", className)}
      style={tierStyle(spec.color)}
    >
      {spec.name}
    </span>
  );
}

/** 号码文本：等宽数字，可带等级徽章 */
export function NumberText({
  number,
  tier,
  tiers,
  className,
  size = "sm"
}: {
  number: number | string | null | undefined;
  tier?: number | null;
  tiers?: NumberTierSpec[] | null;
  className?: string;
  size?: "sm" | "lg";
}) {
  if (number === null || number === undefined || number === "" || number === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
      <span className={cn("font-mono tabular-nums", size === "lg" ? "text-2xl font-semibold tracking-wide" : "text-[13px]")}>
        {String(number)}
      </span>
      {tier !== undefined && tier !== null ? <NumberTierBadge tier={tier} tiers={tiers} /> : null}
    </span>
  );
}

/** 列表里的小号码标签：普通号灰色，靓号用等级色 */
export function NumberChip({ number, tier, className }: { number?: number | null; tier?: number | null; className?: string }) {
  if (!number) return null;
  const pretty = (tier ?? 0) > 0;
  const spec = numberTierOf(tier);
  return (
    <span
      title={pretty ? `号码（${spec.name}）` : "号码"}
      className={cn("shrink-0 rounded px-1 font-mono text-[10px] leading-4 tabular-nums", !pretty && "bg-muted", className)}
      style={pretty ? tierStyle(spec.color) : undefined}
    >
      {number}
    </span>
  );
}

export const SOURCE_LABEL: Record<string, string> = {
  initial: "初始号码",
  change: "改号卡",
  ticket: "靓号券",
  draw: "摇号",
  frozen: "换回旧号",
  admin: "管理员指定",
  grant: "物品发放",
  rollback: "回滚"
};

export const HOLD_KIND_META: Record<NumberHoldKind, { label: string; variant: "info" | "warning" | "secondary" }> = {
  reserved: { label: "保留号", variant: "info" },
  frozen: { label: "冻结号", variant: "warning" },
  candidate: { label: "摇号候选", variant: "secondary" }
};

export const HOLD_KIND_OPTIONS = (Object.keys(HOLD_KIND_META) as NumberHoldKind[]).map((key) => ({
  value: key,
  label: HOLD_KIND_META[key].label
}));

export function HoldKindBadge({ kind }: { kind: string }) {
  const meta = HOLD_KIND_META[kind as NumberHoldKind];
  return <Badge variant={meta?.variant ?? "outline"}>{meta?.label ?? kind}</Badge>;
}

export const DRAW_STATUS_META: Record<NumberDrawStatus, { label: string; variant: "info" | "success" | "secondary" }> = {
  open: { label: "待选号", variant: "info" },
  picked: { label: "已选号", variant: "success" },
  discarded: { label: "已放弃", variant: "secondary" },
  expired: { label: "已过期", variant: "secondary" }
};

export function DrawStatusBadge({ status }: { status: string }) {
  const meta = DRAW_STATUS_META[status as NumberDrawStatus];
  return <Badge variant={meta?.variant ?? "outline"}>{meta?.label ?? status}</Badge>;
}

/** 一种权益的名称（权益种类 + 等级），靓号券带等级色点 */
export function RightLabel({ kind, level, name }: { kind: string; level: number; name?: string }) {
  const label = name || numberRightName(kind, level);
  if (kind !== "ticket") return <span>{label}</span>;
  const spec = numberTierOf(level);
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: spec.color }} aria-hidden />
      {label}
    </span>
  );
}

/** 权益余额摊平成「种类 + 等级 + 余额」，按 改号卡、靓号券由低到高、摇号次数 排；余额为 0 的靓号券也列出 */
export function flattenRights(rights?: NumberRights | null): NumberRight[] {
  if (rights?.items?.length) return rights.items;
  const tickets = NUMBER_TIERS.filter((tier) => tier.level > 0).map((tier) => {
    const found = rights?.tickets?.find((item) => item.level === tier.level);
    return found ?? { kind: "ticket" as const, level: tier.level, name: numberRightName("ticket", tier.level), balance: 0, earned: 0, used: 0 };
  });
  return [
    { kind: "change", level: 0, name: "改号卡", balance: rights?.change ?? 0, earned: 0, used: 0 },
    ...tickets,
    { kind: "draw", level: 0, name: "摇号次数", balance: rights?.draw ?? 0, earned: 0, used: 0 }
  ];
}

export function rightUnit(kind: string) {
  return kind === "draw" ? "次" : "张";
}

/** 用户单元格：昵称或账号，点了进用户详情 */
export function NumberUserCell({
  appKey,
  userId,
  account,
  nickname,
  from
}: {
  appKey: string;
  userId?: number | null;
  account?: string;
  nickname?: string;
  from?: string;
}) {
  if (!userId) return <span className="text-xs text-muted-foreground">—</span>;
  const href = `/users/app-users/${encodeURIComponent(appKey)}/${userId}${from ? `?from=${encodeURIComponent(from)}` : ""}`;
  const primary = nickname?.trim() || account?.trim() || `用户 ${userId}`;
  return (
    <Link href={href} className="block min-w-0 hover:underline" onClick={(event) => event.stopPropagation()}>
      <div className="truncate text-xs font-medium">{primary}</div>
      {account && primary !== account ? <div className="truncate text-[11px] text-muted-foreground">{account}</div> : null}
    </Link>
  );
}

/** 规律名列表（逗号分隔，太长截断） */
export function PatternList({ names, empty = "无" }: { names?: string[] | null; empty?: ReactNode }) {
  if (!names?.length) return <span className="text-xs text-muted-foreground">{empty}</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {names.map((name) => (
        <Badge key={name} variant="outline" className="text-[11px] font-normal">
          {name}
        </Badge>
      ))}
    </span>
  );
}
