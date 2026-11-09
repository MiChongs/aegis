"use client";

import type { ReactNode } from "react";
import {
  AtSign, Check, Fingerprint, Gift, Globe, KeyRound, Mail, Phone, Smartphone, User, UserPlus, Wallet, type LucideIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import type { OpsReason, OpsUser } from "@/lib/api/risk-ops";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/** 账号显示名：昵称 → 登录名 → 用户 #ID */
export function accountName(user: Pick<OpsUser, "nickname" | "account"> | null | undefined, id: number) {
  return user?.nickname || user?.account || `用户 #${id}`;
}

function initials(name: string) {
  const s = name.replace(/^用户 #/, "").trim();
  return (s.match(/[\p{Script=Han}]/u)?.[0] ?? s.slice(0, 2)).toUpperCase();
}

export function AccountAvatar({ user, id, size = "default" }: { user?: OpsUser | null; id: number; size?: "sm" | "default" | "lg" }) {
  const name = accountName(user, id);
  return (
    <Avatar className={cn(size === "sm" ? "size-7" : size === "lg" ? "size-11" : "size-9", "shrink-0")}>
      {user?.avatar ? <AvatarImage src={user.avatar} alt="" /> : null}
      <AvatarFallback className={cn("bg-muted font-medium text-muted-foreground", size === "sm" ? "text-[11px]" : "text-xs")}>{initials(name)}</AvatarFallback>
    </Avatar>
  );
}

/** 账号当前状态：风控封禁中 / 人工封禁中 / 正常 / 已不存在 */
export function AccountState({ user, graphBanned }: { user?: OpsUser | null; graphBanned?: boolean }) {
  if (!user) return <Badge variant="secondary" size="sm">账号已不存在</Badge>;
  if (user.ban) {
    const until = user.ban.endAt ? `至 ${formatDateTime(user.ban.endAt)}` : "永久";
    return (
      <Badge variant={user.banFromRisk ? "danger" : "warning"} size="sm" title={until}>
        {user.banFromRisk ? "风控封禁中" : "人工封禁中"}
      </Badge>
    );
  }
  if (graphBanned) return <Badge variant="warning" size="sm">名单拦截中</Badge>;
  return <Badge variant="success" size="sm">正常</Badge>;
}

/** 账号的一行说明：登录名、对外 ID、注册与最近登录 */
export function accountMeta(user: OpsUser | null | undefined, id: number): string {
  if (!user) return `用户 ID ${id}`;
  const parts = [user.account && user.account !== user.nickname ? `登录名 ${user.account}` : "", `ID ${user.publicId || user.id}`];
  parts.push(`注册于 ${formatRelative(user.createdAt)}`);
  if (user.lastLoginAt) parts.push(`最近登录 ${formatRelative(user.lastLoginAt)}`);
  return parts.filter(Boolean).join("，");
}

// ── 关联理由 ──

export const REASON_META: Record<string, { icon: LucideIcon; label: string }> = {
  device: { icon: Smartphone, label: "同一台设备" },
  phone: { icon: Phone, label: "同一个手机号" },
  email: { icon: Mail, label: "同一个邮箱" },
  identity: { icon: KeyRound, label: "同一个第三方账号" },
  payer: { icon: Wallet, label: "同一个付款账号" },
  passkey: { icon: Fingerprint, label: "同一把通行密钥" },
  account: { icon: AtSign, label: "同一个登录名" },
  ip: { icon: Globe, label: "同一条网络线路" },
  invited: { icon: UserPlus, label: "邀请关系" },
  targets: { icon: Gift, label: "转赠往来" },
};

export function ReasonChip({ reason }: { reason: OpsReason }) {
  const meta = REASON_META[reason.kind] ?? { icon: User, label: "有关联" };
  const Icon = meta.icon;
  const weak = reason.kind === "ip";
  return (
    <span
      className={cn(
        "inline-flex max-w-full items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] leading-4",
        weak ? "border-dashed text-muted-foreground" : "border-border bg-muted/40 text-foreground",
      )}
      title={reason.lastSeen ? `最近一次 ${formatDateTime(reason.lastSeen * 1000)}` : undefined}
    >
      <Icon className="size-3 shrink-0" />
      <span className="shrink-0">{meta.label}</span>
      {reason.label ? <span className="truncate text-muted-foreground">{reason.label}</span> : null}
    </span>
  );
}

// ── 步骤条 ──

export function Stepper({ steps, current }: { steps: string[]; current: number }) {
  return (
    <ol className="flex items-center gap-2 text-xs">
      {steps.map((label, i) => {
        const done = i < current;
        const active = i === current;
        return (
          <li key={label} className="flex min-w-0 items-center gap-2">
            {i > 0 ? <span className={cn("h-px w-6 shrink-0 sm:w-10", done || active ? "bg-primary/60" : "bg-border")} /> : null}
            <span
              className={cn(
                "grid size-5 shrink-0 place-items-center rounded-full border text-[11px] font-medium tabular-nums transition-colors",
                done && "border-primary bg-primary text-primary-foreground",
                active && "border-primary text-primary",
                !done && !active && "text-muted-foreground",
              )}
            >
              {done ? <Check className="size-3" /> : i + 1}
            </span>
            <span className={cn("truncate", active ? "font-medium text-foreground" : "text-muted-foreground")}>{label}</span>
          </li>
        );
      })}
    </ol>
  );
}

// ── 可勾选的一项处置 ──

export function MeasureRow({
  checked, onChange, icon: Icon, tone = "muted", title, detail, aside, disabled,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  icon: LucideIcon;
  tone?: "rose" | "amber" | "sky" | "muted" | "emerald";
  title: ReactNode;
  detail?: ReactNode;
  aside?: ReactNode;
  disabled?: boolean;
}) {
  const plate = {
    rose: "bg-rose-500/12 text-rose-600 dark:text-rose-400",
    amber: "bg-amber-500/12 text-amber-600 dark:text-amber-400",
    sky: "bg-sky-500/12 text-sky-600 dark:text-sky-400",
    emerald: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400",
    muted: "bg-muted text-muted-foreground",
  }[tone];
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-muted/50",
        !checked && "opacity-70",
        disabled && "cursor-not-allowed opacity-50",
      )}
    >
      <Checkbox checked={checked} onCheckedChange={(v) => onChange(v === true)} disabled={disabled} className="mt-1" />
      <span className={cn("grid size-7 shrink-0 place-items-center rounded-lg", plate)}>
        <Icon className="size-3.5" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm leading-5 text-foreground">{title}</span>
        {detail ? <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">{detail}</span> : null}
      </span>
      {aside ? <span className="shrink-0 pt-0.5">{aside}</span> : null}
    </label>
  );
}

/** 一段统计：大数字 + 说明 */
export function StatTile({ value, label, tone = "default" }: { value: ReactNode; label: string; tone?: "default" | "rose" | "emerald" }) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3">
      <div
        className={cn(
          "text-2xl font-semibold tabular-nums",
          tone === "rose" && "text-rose-600 dark:text-rose-400",
          tone === "emerald" && "text-emerald-600 dark:text-emerald-400",
        )}
      >
        {value}
      </div>
      <div className="mt-0.5 text-xs text-muted-foreground">{label}</div>
    </div>
  );
}
