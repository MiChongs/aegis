"use client";

import { createElement, type ReactNode } from "react";
import {
  Activity,
  AppWindow,
  Ban,
  BarChart3,
  Bell,
  BellDot,
  Bot,
  BrickWall,
  Bug,
  Building2,
  CheckCircle2,
  Cloud,
  Code2,
  Coins,
  Contact,
  Cpu,
  CreditCard,
  Crown,
  Database,
  Dices,
  FileCheck2,
  Fingerprint,
  GalleryHorizontalEnd,
  Gavel,
  Gift,
  GitBranch,
  Globe,
  Handshake,
  HardDrive,
  Headphones,
  History,
  Image as ImageIcon,
  Info,
  KeyRound,
  LifeBuoy,
  LineChart,
  Lock,
  type LucideIcon,
  Mail,
  Map as MapIcon,
  Megaphone,
  MessageSquareText,
  Milestone,
  Monitor,
  MonitorPlay,
  MonitorSmartphone,
  Network,
  Package,
  PackageCheck,
  PackagePlus,
  PlugZap,
  Puzzle,
  Radio,
  Rocket,
  Route,
  Scale,
  ScanFace,
  ScrollText,
  Send,
  ServerCog,
  Settings2,
  ShieldAlert,
  ShieldBan,
  ShieldCheck,
  ShieldHalf,
  ShoppingBag,
  SlidersHorizontal,
  Smartphone,
  Sparkles,
  Split,
  Sprout,
  Store,
  Ticket,
  Timer,
  TrendingUp,
  Trophy,
  UserCheck,
  UserCog,
  UserPlus,
  Users2,
  UsersRound,
  UserX,
  Wallet,
  Waves,
  Workflow,
  Wrench,
} from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  TIER_LABEL,
  type AdminTier,
  type PermissionScope,
} from "@/lib/api/access";
import { cn } from "@/lib/utils";

/**
 * 管理员与权限页的展示原语：档位、作用域、节点类型的徽标与配色。
 *
 * 档位配色固定：系统管理员紫、超级管理员琥珀、管理员中性 —— 列表、详情、角色成员、
 * 诊断结果里同一档位必须是同一个颜色，否则一眼认不出「这个人是不是动不了」。
 */

const ICONS: Record<string, LucideIcon> = {
  activity: Activity,
  "app-window": AppWindow,
  ban: Ban,
  "bar-chart": BarChart3,
  bell: Bell,
  "bell-dot": BellDot,
  bot: Bot,
  "brick-wall": BrickWall,
  bug: Bug,
  "building-2": Building2,
  "check-circle": CheckCircle2,
  cloud: Cloud,
  "code-2": Code2,
  coins: Coins,
  contact: Contact,
  cpu: Cpu,
  "credit-card": CreditCard,
  crown: Crown,
  database: Database,
  dices: Dices,
  "file-check-2": FileCheck2,
  fingerprint: Fingerprint,
  "gallery-horizontal-end": GalleryHorizontalEnd,
  gavel: Gavel,
  gift: Gift,
  "git-branch": GitBranch,
  globe: Globe,
  handshake: Handshake,
  "hard-drive": HardDrive,
  headphones: Headphones,
  history: History,
  image: ImageIcon,
  info: Info,
  "key-round": KeyRound,
  "life-buoy": LifeBuoy,
  "line-chart": LineChart,
  mail: Mail,
  map: MapIcon,
  megaphone: Megaphone,
  "message-square-text": MessageSquareText,
  milestone: Milestone,
  monitor: Monitor,
  "monitor-play": MonitorPlay,
  "monitor-smartphone": MonitorSmartphone,
  network: Network,
  package: Package,
  "package-check": PackageCheck,
  "package-plus": PackagePlus,
  "plug-zap": PlugZap,
  puzzle: Puzzle,
  radio: Radio,
  rocket: Rocket,
  route: Route,
  scale: Scale,
  "scan-face": ScanFace,
  "scroll-text": ScrollText,
  send: Send,
  "server-cog": ServerCog,
  "settings-2": Settings2,
  "shield-alert": ShieldAlert,
  "shield-ban": ShieldBan,
  "shield-check": ShieldCheck,
  "shield-half": ShieldHalf,
  "shopping-bag": ShoppingBag,
  "sliders-horizontal": SlidersHorizontal,
  smartphone: Smartphone,
  sparkles: Sparkles,
  split: Split,
  sprout: Sprout,
  store: Store,
  ticket: Ticket,
  timer: Timer,
  "trending-up": TrendingUp,
  trophy: Trophy,
  "user-check": UserCheck,
  "user-cog": UserCog,
  "user-plus": UserPlus,
  "users-2": Users2,
  "users-round": UsersRound,
  "user-x": UserX,
  wallet: Wallet,
  waves: Waves,
  workflow: Workflow,
  wrench: Wrench,
};

export function nodeIcon(name?: string): LucideIcon {
  return (name && ICONS[name]) || KeyRound;
}

/** 权限树节点的图标（按树里登记的名字取，未登记时用钥匙）。 */
export function NodeIcon({
  name,
  className,
}: {
  name?: string;
  className?: string;
}) {
  return createElement(nodeIcon(name), { className, "aria-hidden": true });
}

// ── 档位 ──

export const TIER_TONE: Record<
  AdminTier,
  { plate: string; text: string; ring: string; bar: string; icon: LucideIcon }
> = {
  system: {
    plate: "bg-violet-500/12 text-violet-700 dark:text-violet-300",
    text: "text-violet-700 dark:text-violet-300",
    ring: "ring-violet-500/30",
    bar: "bg-violet-500",
    icon: Crown,
  },
  super: {
    plate: "bg-amber-500/14 text-amber-700 dark:text-amber-300",
    text: "text-amber-700 dark:text-amber-300",
    ring: "ring-amber-500/30",
    bar: "bg-amber-500",
    icon: ShieldCheck,
  },
  admin: {
    plate: "bg-muted text-muted-foreground",
    text: "text-muted-foreground",
    ring: "ring-border",
    bar: "bg-sky-500",
    icon: UserCog,
  },
};

export function TierBadge({
  tier,
  size = "default",
}: {
  tier: AdminTier;
  size?: "sm" | "default";
}) {
  const tone = TIER_TONE[tier];
  const Icon = tone.icon;
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full font-medium whitespace-nowrap",
        size === "sm" ? "h-5 px-1.5 text-[11px]" : "h-6 px-2 text-xs",
        tone.plate,
      )}
    >
      <Icon className={size === "sm" ? "size-3" : "size-3.5"} />
      {TIER_LABEL[tier]}
    </span>
  );
}

// ── 作用域 / 类型 / 风险 ──

const SCOPE_LABEL: Record<PermissionScope, string> = {
  platform: "平台",
  app: "应用",
  both: "通用",
};

export function ScopeTag({
  scope,
  className,
}: {
  scope: PermissionScope;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-[18px] shrink-0 items-center rounded px-1.5 text-[10px] font-medium",
        scope === "platform" && "bg-sky-500/10 text-sky-700 dark:text-sky-300",
        scope === "app" &&
          "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
        scope === "both" && "bg-muted text-muted-foreground",
        className,
      )}
    >
      {SCOPE_LABEL[scope]}
    </span>
  );
}

export function DangerTag({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-[18px] shrink-0 items-center rounded bg-rose-500/10 px-1.5 text-[10px] font-medium text-rose-600 dark:text-rose-400",
        className,
      )}
    >
      高危
    </span>
  );
}

export function KindTag({ kind }: { kind: "dir" | "menu" | "action" }) {
  const label = kind === "dir" ? "目录" : kind === "menu" ? "菜单" : "操作";
  return (
    <span
      className={cn(
        "inline-flex h-[18px] shrink-0 items-center rounded px-1.5 text-[10px] font-medium",
        kind === "dir" && "bg-primary/10 text-primary",
        kind === "menu" &&
          "bg-violet-500/10 text-violet-700 dark:text-violet-300",
        kind === "action" && "bg-muted text-muted-foreground",
      )}
    >
      {label}
    </span>
  );
}

export function CodeText({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <code
      className={cn("font-mono text-[11px] text-muted-foreground", className)}
    >
      {children}
    </code>
  );
}

// ── 人 ──

export function initialOf(name?: string) {
  const text = (name ?? "").trim();
  return text ? Array.from(text)[0]!.toUpperCase() : "?";
}

export function AdminIdentity({
  name,
  account,
  avatar,
  tier,
  size = "default",
  muted,
  trailing,
}: {
  name?: string;
  account?: string;
  avatar?: string;
  tier?: AdminTier;
  size?: "sm" | "default" | "lg";
  muted?: boolean;
  trailing?: ReactNode;
}) {
  const label = name?.trim() || account || "未命名";
  return (
    <div
      className={cn("flex min-w-0 items-center gap-2.5", muted && "opacity-60")}
    >
      <Avatar
        preview={false}
        className={cn(
          "shrink-0 ring-2 ring-offset-1 ring-offset-background",
          size === "sm" ? "size-6" : size === "lg" ? "size-11" : "size-8",
          tier ? TIER_TONE[tier].ring : "ring-transparent",
        )}
      >
        {avatar && <AvatarImage src={avatar} alt={label} />}
        <AvatarFallback className="text-[11px]">
          {initialOf(label)}
        </AvatarFallback>
      </Avatar>
      <div className="min-w-0 flex-1">
        <div
          className={cn(
            "flex min-w-0 items-center gap-1.5",
            size === "lg" ? "text-base font-semibold" : "text-sm font-medium",
          )}
        >
          <span className="truncate">{label}</span>
          {trailing}
        </div>
        {account && account !== label && (
          <div className="truncate text-[11px] text-muted-foreground">
            @{account}
          </div>
        )}
      </div>
    </div>
  );
}

/** 不可操作时的锁定说明（悬停给出原因） */
export function LockedHint({
  reason,
  children,
}: {
  reason?: string;
  children: ReactNode;
}) {
  if (!reason) return <>{children}</>;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center gap-1">
          {children}
          <Lock className="size-3 text-muted-foreground" />
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-64">{reason}</TooltipContent>
    </Tooltip>
  );
}

// ── 分节 ──

export function Pane({
  title,
  icon: Icon,
  aside,
  children,
  className,
  bodyClassName,
}: {
  title?: ReactNode;
  icon?: LucideIcon;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section
      className={cn(
        "flex min-w-0 flex-col overflow-hidden rounded-2xl border border-border bg-card",
        className,
      )}
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      {(title || aside) && (
        <header className="flex min-h-12 items-center gap-2.5 border-b border-border/70 px-4 py-2.5">
          {Icon && <Icon className="size-4 text-muted-foreground" />}
          <div className="min-w-0 flex-1 truncate text-sm font-semibold tracking-tight">
            {title}
          </div>
          {aside && (
            <div className="flex shrink-0 items-center gap-1.5">{aside}</div>
          )}
        </header>
      )}
      <div className={cn("min-w-0 flex-1", bodyClassName)}>{children}</div>
    </section>
  );
}

/** 分段占比条（档位分布、覆盖率） */
export function SegmentBar({
  segments,
  className,
}: {
  segments: Array<{ value: number; className: string; label: string }>;
  className?: string;
}) {
  const total = segments.reduce((sum, item) => sum + item.value, 0);
  return (
    <div
      className={cn(
        "flex h-2 w-full overflow-hidden rounded-full bg-muted",
        className,
      )}
    >
      {total > 0 &&
        segments.map((item) =>
          item.value > 0 ? (
            <Tooltip key={item.label}>
              <TooltipTrigger asChild>
                <span
                  className={cn(
                    "h-full transition-[width] duration-500",
                    item.className,
                  )}
                  style={{ width: `${(item.value / total) * 100}%` }}
                />
              </TooltipTrigger>
              <TooltipContent>
                {item.label} {item.value}
              </TooltipContent>
            </Tooltip>
          ) : null,
        )}
    </div>
  );
}
