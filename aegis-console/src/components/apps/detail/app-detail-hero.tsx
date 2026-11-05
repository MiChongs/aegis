"use client";

import Link from "next/link";
import {
  ArrowUpRight,
  CalendarClock,
  ChevronLeft,
  Ellipsis,
  FileText,
  HardDrive,
  LogIn,
  Power,
  Rocket,
  UserPlus,
  Users2,
  type LucideIcon
} from "lucide-react";
import type { AppSummary } from "@/lib/api/types";
import type { GovernanceState } from "@/lib/api/platform-governance";
import { useAdminAppStatsQuery } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import { AppRowActions } from "@/components/apps/app-row-actions";
import { AppHealthPill, AppKeyText, AppTile, appHealth, formatAppDate, formatCount } from "@/components/apps/app-shared";
import { AppSwitcher } from "@/components/apps/detail/app-switcher";

/**
 * 应用详情页头部：始终回答「我在配哪个应用、它现在能不能用、它有多大」。
 *
 * 宽屏是一张横向名片：左边标识与三项开关状态，右边是带数字的关联入口
 * （用户、内容、存储、发布），一眼看到规模，点一下就过去。
 * 窄屏重新排：名称一行、AppKey 独占一行，开关与入口各自一排横滑，
 * 整块高度控制在一屏的四分之一左右，进来先看到的不全是头部。
 */
export function AppDetailHero({
  app,
  apps,
  section,
  governanceState,
  onDelete
}: {
  app: AppSummary;
  apps: AppSummary[];
  section: string;
  governanceState?: GovernanceState | null;
  onDelete: (app: AppSummary) => void;
}) {
  const health = appHealth(app, governanceState);
  const stats = useAdminAppStatsQuery(app.appKey).data;
  const enc = encodeURIComponent(app.appKey);
  const contentCount = stats ? stats.bannerCount + stats.noticeCount : undefined;

  const related: Array<{ href: string; label: string; icon: LucideIcon; value?: string; hint: string }> = [
    { href: `/app-users?app=${enc}`, label: "用户", icon: Users2, value: stats ? formatCount(stats.totalUsers) : undefined, hint: "账号管理" },
    { href: `/content?app=${enc}`, label: "内容", icon: FileText, value: contentCount === undefined ? undefined : formatCount(contentCount), hint: "横幅与公告" },
    { href: `/storage?tab=app&app=${enc}`, label: "存储", icon: HardDrive, hint: "文件与配额" },
    { href: "/releases", label: "发布", icon: Rocket, hint: "版本与渠道" }
  ];

  const access = [
    { label: "服务", on: app.status, icon: Power, reason: app.disabledReason },
    { label: "注册", on: app.registerStatus, icon: UserPlus, reason: app.disabledRegisterReason },
    { label: "登录", on: app.loginStatus, icon: LogIn, reason: app.disabledLoginReason }
  ];

  return (
    <header
      className="relative min-w-0 overflow-hidden rounded-2xl border border-border bg-card"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      {/* 顶行：返回 + 右侧切换与操作 */}
      <div className="flex items-center justify-between gap-2 border-b border-border/60 px-3 py-2 sm:px-4">
        <nav aria-label="位置" className="flex min-w-0 items-center gap-1 text-xs">
          <Link
            href="/apps"
            className="inline-flex h-7 shrink-0 items-center gap-0.5 rounded-md pr-1.5 pl-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ChevronLeft className="size-3.5" />
            应用列表
          </Link>
          <span className="text-muted-foreground/50">/</span>
          <span className="truncate font-medium">{app.name}</span>
        </nav>
        <div className="flex shrink-0 items-center gap-1.5">
          <div className="hidden sm:block">
            <AppSwitcher app={app} apps={apps} section={section} />
          </div>
          <div className="sm:hidden">
            <AppSwitcher app={app} apps={apps} section={section} compact />
          </div>
          <AppRowActions
            app={app}
            onDelete={onDelete}
            trigger={
              <button
                type="button"
                aria-label="应用操作"
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-background px-2.5 text-xs transition-colors hover:bg-accent max-sm:size-9 max-sm:justify-center max-sm:rounded-xl max-sm:border-transparent max-sm:bg-transparent max-sm:px-0"
              >
                <Ellipsis className="size-4" />
                <span className="max-sm:hidden">操作</span>
              </button>
            }
          />
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-4 p-3.5 sm:p-5 lg:flex-row lg:items-center lg:gap-6">
        {/* 标识 */}
        <div className="flex min-w-0 flex-1 items-start gap-3.5 sm:gap-4">
          <AppTile name={app.name} seed={app.appKey} size="lg" className="size-14 rounded-2xl text-xl max-sm:size-11 max-sm:text-base" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <h1 className="min-w-0 truncate text-lg font-semibold tracking-tight sm:text-2xl">{app.name}</h1>
              <span className="shrink-0 rounded-md bg-muted px-1.5 font-mono text-[11px] leading-5 text-muted-foreground">#{app.id}</span>
              <AppHealthPill health={health} />
            </div>
            <AppKeyText appKey={app.appKey} className="max-w-full" />
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 pt-0.5 text-[11px] text-muted-foreground">
              <span className="inline-flex items-center gap-1">
                <CalendarClock className="size-3" />
                创建于 {formatAppDate(app.createdAt)}
              </span>
              <span>更新于 {formatAppDate(app.updatedAt)}</span>
            </div>
          </div>
        </div>

        {/* 关联入口：带数字的小磁贴，窄屏横滑 */}
        <nav
          aria-label="关联页面"
          className="-mx-3.5 flex min-w-0 gap-2 overflow-x-auto px-3.5 [scrollbar-width:none] sm:-mx-5 sm:px-5 lg:mx-0 lg:grid lg:shrink-0 lg:grid-cols-4 lg:overflow-visible lg:px-0 [&::-webkit-scrollbar]:hidden"
        >
          {related.map(({ href, label, icon: Icon, value, hint }) => (
            <Link
              key={label}
              href={href}
              className="group flex w-28 shrink-0 flex-col gap-2 rounded-xl border border-border bg-background/60 p-2.5 transition-colors hover:border-primary/30 hover:bg-primary/[0.04] lg:w-[6.75rem]"
            >
              <span className="flex items-center justify-between">
                <span className="grid size-7 place-items-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:bg-primary/10 group-hover:text-primary">
                  <Icon className="size-3.5" />
                </span>
                <ArrowUpRight className="size-3.5 text-muted-foreground/50 transition-colors group-hover:text-primary" />
              </span>
              <span className="min-w-0">
                <span className="block truncate text-xs font-medium">
                  {label}
                  {value !== undefined && <span className="ml-1 font-semibold tabular-nums">{value}</span>}
                </span>
                <span className="block truncate text-[10px] text-muted-foreground">{hint}</span>
              </span>
            </Link>
          ))}
        </nav>
      </div>

      {/* 下沿：三项开关的当前状态（只读，改动在「基本信息」或操作菜单里） */}
      <div className="flex min-w-0 items-center gap-2 overflow-x-auto border-t border-border/60 bg-muted/25 px-3.5 py-2 [scrollbar-width:none] sm:px-5 [&::-webkit-scrollbar]:hidden">
        {access.map(({ label, on, icon: Icon, reason }) => (
          <span
            key={label}
            title={on ? `${label}已开启` : reason ? `${label}已关闭：${reason}` : `${label}已关闭`}
            className={cn(
              "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full border px-2.5 text-[11px]",
              on
                ? "border-border bg-background text-foreground/80"
                : "border-dashed border-amber-500/50 bg-amber-500/[0.06] text-amber-700 dark:text-amber-300"
            )}
          >
            <Icon className="size-3 shrink-0" />
            {label}
            <span className={cn("size-1.5 rounded-full", on ? "bg-emerald-500" : "bg-amber-500")} />
            <span className="font-medium">{on ? "开放" : "关闭"}</span>
            {!on && reason && <span className="max-w-48 truncate text-amber-700/80 dark:text-amber-300/80">{reason}</span>}
          </span>
        ))}
      </div>
    </header>
  );
}
