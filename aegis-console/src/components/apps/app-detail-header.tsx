"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, ChevronDown, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { appSectionHref } from "@/lib/app-sections";
import type { AppSummary } from "@/lib/api/types";
import type { GovernanceState } from "@/lib/api/platform-governance";
import { AppRowActions } from "@/components/apps/app-row-actions";
import { AppKeyText, AppStatusBadges, AppTile, formatAppDate } from "@/components/apps/app-shared";

/**
 * 应用详情页头部：始终回答「我在配哪个应用」。
 *
 * 应用选择器保留在这里，但语义变了 —— 它不再是「换掉下方面板的数据源」，
 * 而是**带着当前区块换一个应用**（`/apps/A?tab=oauth` → `/apps/B?tab=oauth`）。
 * 对着两个应用比对同一项配置时，这是唯一一个不用来回点的路径。
 */
export function AppDetailHeader({
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
  const router = useRouter();

  return (
    <div
      className="flex flex-col gap-4 rounded-2xl border border-border bg-card p-4 sm:p-5"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <AppTile name={app.name} seed={app.appKey} size="lg" />
          <div className="min-w-0 space-y-1.5">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-lg font-semibold tracking-tight">{app.name}</h1>
              <span className="font-mono text-xs text-muted-foreground">#{app.id}</span>
            </div>
            <AppKeyText appKey={app.appKey} />
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
              <span>创建于 {formatAppDate(app.createdAt)}</span>
              <span>更新于 {formatAppDate(app.updatedAt)}</span>
            </div>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <Button asChild size="sm" variant="ghost" className="h-8 gap-1 text-xs">
            <Link href="/apps">
              <ArrowLeft className="size-3.5" />
              应用列表
            </Link>
          </Button>
          {apps.length > 1 && (
            <Select
              value={app.appKey}
              onValueChange={(appKey) => router.push(appSectionHref(appKey, section))}
            >
              <SelectTrigger className="h-8 w-44 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {apps.map((item) => (
                  <SelectItem key={item.appKey} value={item.appKey}>
                    {item.name} ({item.id})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <AppRowActions
            app={app}
            onDelete={onDelete}
            trigger={
              <Button size="sm" variant="outline" className="h-8 gap-1 text-xs">
                操作
                <ChevronDown className="size-3" />
              </Button>
            }
          />
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border/70 pt-3">
        <AppStatusBadges app={app} governance={governanceState} size="default" />
        <div className="flex flex-wrap items-center gap-1">
          <RelatedLink href={`/users?tab=app-users&app=${encodeURIComponent(app.appKey)}`} label="用户" />
          <RelatedLink href={`/content?app=${encodeURIComponent(app.appKey)}`} label="内容" />
          <RelatedLink href={`/storage?tab=app&app=${encodeURIComponent(app.appKey)}`} label="存储" />
          <RelatedLink href="/releases" label="发布" />
        </div>
      </div>
    </div>
  );
}

function RelatedLink({ href, label }: { href: string; label: string }) {
  return (
    <Link
      href={href}
      className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      {label}
      <ExternalLink className="size-3" />
    </Link>
  );
}
