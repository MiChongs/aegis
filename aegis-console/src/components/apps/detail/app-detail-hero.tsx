"use client";

import Link from "next/link";
import { ArrowLeft, ArrowUpRight, Boxes, Ellipsis, FileText, HardDrive, Rocket, Users2 } from "lucide-react";
import type { AppSummary } from "@/lib/api/types";
import type { GovernanceState } from "@/lib/api/platform-governance";
import { AppRowActions } from "@/components/apps/app-row-actions";
import {
  AppAccessIndicators,
  AppHealthPill,
  AppKeyText,
  AppTile,
  appHealth,
  formatAppDate
} from "@/components/apps/app-shared";
import { AppSwitcher } from "@/components/apps/detail/app-switcher";

/**
 * 应用详情页头部：始终回答「我在配哪个应用、它现在能不能用」。
 *
 * 宽屏是一张横向名片：左边标识，右边切换与操作，下沿一条状态与关联入口。
 * 窄屏重新排：名称一行、AppKey 独占一行（三十多位的 key 不该和按钮抢宽度），
 * 关联入口变成可横滑的一排，整块高度控制在一屏的四分之一以内，
 * 免得进来先看到的全是头部、看不到配置。
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
  const related = [
    { href: `/app-users?app=${encodeURIComponent(app.appKey)}`, label: "用户", icon: Users2 },
    { href: `/content?app=${encodeURIComponent(app.appKey)}`, label: "内容", icon: FileText },
    { href: `/storage?tab=app&app=${encodeURIComponent(app.appKey)}`, label: "存储", icon: HardDrive },
    { href: "/releases", label: "发布", icon: Rocket }
  ];

  return (
    <header
      className="min-w-0 overflow-hidden rounded-2xl border border-border bg-card"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <div className="flex min-w-0 flex-col gap-3 p-3.5 sm:p-5">
        {/* 顶行：返回 + 右侧操作。窄屏把切换与操作收成图标，名称才放得下 */}
        <div className="flex items-center justify-between gap-2">
          <Link
            href="/apps"
            className="-ml-1 inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ArrowLeft className="size-3.5" />
            应用列表
          </Link>
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

        <div className="flex min-w-0 items-start gap-3 sm:gap-4">
          <AppTile name={app.name} seed={app.appKey} size="lg" className="max-sm:size-11 max-sm:text-base" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <h1 className="min-w-0 truncate text-base font-semibold tracking-tight sm:text-xl">{app.name}</h1>
              <span className="shrink-0 font-mono text-xs text-muted-foreground">#{app.id}</span>
              <AppHealthPill health={health} />
            </div>
            <AppKeyText appKey={app.appKey} className="max-w-full" />
          </div>
        </div>
      </div>

      {/* 下沿：开关状态 + 时间 + 关联入口。窄屏关联入口单独一行横滑 */}
      <div className="flex min-w-0 flex-col gap-2.5 border-t border-border/70 bg-muted/25 px-3.5 py-2.5 sm:px-5 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
          <AppAccessIndicators app={app} />
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
            <span>创建于 {formatAppDate(app.createdAt)}</span>
            <span>更新于 {formatAppDate(app.updatedAt)}</span>
          </div>
        </div>
        <nav
          aria-label="关联页面"
          className="-mx-3.5 flex min-w-0 items-center gap-1 overflow-x-auto px-3.5 [scrollbar-width:none] sm:-mx-5 sm:px-5 lg:mx-0 lg:px-0 [&::-webkit-scrollbar]:hidden"
        >
          <Boxes className="mr-0.5 size-3.5 shrink-0 text-muted-foreground/70 max-lg:hidden" />
          {related.map(({ href, label, icon: Icon }) => (
            <Link
              key={label}
              href={href}
              className="inline-flex h-7 shrink-0 items-center gap-1 rounded-full border border-border bg-background px-2.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground lg:border-transparent lg:bg-transparent"
            >
              <Icon className="size-3 lg:hidden" />
              {label}
              <ArrowUpRight className="size-3 opacity-60" />
            </Link>
          ))}
        </nav>
      </div>
    </header>
  );
}
