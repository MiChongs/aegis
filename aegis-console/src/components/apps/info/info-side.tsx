"use client";

import Link from "next/link";
import {
  ArrowUpRight,
  ChevronRight,
  Fingerprint,
  Image as ImageIcon,
  LayoutGrid,
  Link2,
  Megaphone,
  Newspaper,
  Trash2,
  TriangleAlert
} from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { formatAppDate, formatCount } from "@/components/apps/app-shared";
import { CopyField, DetailPanel, Fact, PanelLink, useCopy } from "@/components/apps/detail/app-detail-kit";
import { useAdminAppStatsQuery } from "@/lib/admin-hooks";
import { appSectionHref, findAppSection } from "@/lib/app-sections";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { cn } from "@/lib/utils";
import type { AppDetail } from "@/lib/api/types";

/** 自创建起的整天数。放在组件外：渲染函数里直接读时钟会被纯度检查拦下 */
function daysSince(value?: string | null) {
  if (!value) return null;
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return null;
  return Math.max(0, Math.floor((Date.now() - time) / 86_400_000));
}

/**
 * 接入标识：接入方真正要抄走的三样东西（AppKey、接口地址、应用 ID），
 * 外加一段可以直接贴进 `.env` 的配置。安全等级、签名密钥与自检在「接入」区块。
 */
export function InfoIdentity({ app }: { app: AppDetail }) {
  const origin = useOrigin();
  // 显式配置了直连地址就用它，否则用控制台当前站点（同源反代场景），与「接入」区块同一规则
  const baseUrl = appConfig.apiBaseUrl || origin || "";
  const env = `AEGIS_BASE_URL=${baseUrl || "<接口地址>"}\nAEGIS_APP_KEY=${app.appKey}`;
  const { copied, copy } = useCopy();
  const age = daysSince(app.createdAt);

  return (
    <DetailPanel
      icon={Fingerprint}
      tone="primary"
      title="接入标识"
      description="客户端与服务端接入时使用"
      aside={
        <PanelLink href={appSectionHref(app.appKey, "auth-protocol")}>
          接入设置
          <ArrowUpRight className="size-3" />
        </PanelLink>
      }
    >
      <div className="space-y-3.5">
        <CopyField label="AppKey" value={app.appKey} />
        <CopyField label="接口地址" value={baseUrl} placeholder="当前站点" />

        <div className="overflow-hidden rounded-lg border border-border/70">
          <div className="flex items-center justify-between border-b border-border/70 bg-muted/40 px-2.5 py-1">
            <span className="font-mono text-[10px] text-muted-foreground">.env</span>
            <button
              type="button"
              onClick={() => copy(env, "环境变量")}
              className="text-[10px] text-muted-foreground transition-colors hover:text-foreground"
            >
              {copied === env ? "已复制" : "复制"}
            </button>
          </div>
          <pre className="overflow-x-auto px-2.5 py-2 font-mono text-[11px] leading-relaxed whitespace-pre text-foreground/90">{env}</pre>
        </div>

        <dl className="grid grid-cols-2 gap-x-3 gap-y-3 border-t border-border/70 pt-3.5">
          <Fact label="应用 ID" value={String(app.id)} mono />
          <Fact label="已运行" value={age === null ? "—" : `${formatCount(age)} 天`} />
          <Fact label="创建时间" value={formatAppDate(app.createdAt)} />
          <Fact label="更新时间" value={formatAppDate(app.updatedAt)} />
        </dl>
      </div>
    </DetailPanel>
  );
}

/** 内容与绑定：横幅、公告、第三方绑定三个计数，各自能点进去 */
export function InfoAssets({ appKey }: { appKey: string }) {
  const statsQuery = useAdminAppStatsQuery(appKey);
  const stats = statsQuery.data;
  const enc = encodeURIComponent(appKey);
  const items = [
    { label: "横幅", value: stats?.bannerCount, icon: ImageIcon, href: `/content?app=${enc}`, tone: "text-sky-600 dark:text-sky-400" },
    { label: "公告", value: stats?.noticeCount, icon: Megaphone, href: `/content?app=${enc}`, tone: "text-amber-600 dark:text-amber-400" },
    { label: "第三方绑定", value: stats?.oauthBindCount, icon: Link2, href: appSectionHref(appKey, "oauth"), tone: "text-violet-600 dark:text-violet-400" }
  ];
  return (
    <DetailPanel icon={Newspaper} title="内容与绑定" description="面向用户投放的内容与账号绑定" bodyClassName="pb-4 sm:pb-4">
      <div className="grid grid-cols-3 gap-2">
        {items.map(({ label, value, icon: Icon, href, tone }) => (
          <Link
            key={label}
            href={href}
            scroll={false}
            className="group min-w-0 rounded-xl border border-border/80 bg-background/60 p-2.5 transition-colors hover:border-primary/30 hover:bg-primary/[0.04]"
          >
            <Icon className={cn("size-4", tone)} />
            {statsQuery.isLoading ? (
              <Skeleton className="mt-2 h-5 w-8 rounded" />
            ) : (
              <div className="mt-1.5 truncate text-base font-semibold tabular-nums">{formatCount(value)}</div>
            )}
            <div className="truncate text-[10px] text-muted-foreground group-hover:text-foreground">{label}</div>
          </Link>
        ))}
      </div>
    </DetailPanel>
  );
}

/** 常用入口：按配置频率挑的几个区块 */
const SHORTCUTS = ["policy", "oauth", "auth-protocol", "payment", "vip", "email"];

export function InfoShortcuts({ appKey }: { appKey: string }) {
  return (
    <DetailPanel icon={LayoutGrid} title="常用配置" description="日常最常进出的区块" bodyClassName="px-2 pb-2 sm:px-2 sm:pb-2">
      <div className="grid grid-cols-1 gap-px">
        {SHORTCUTS.map((key) => {
          const meta = findAppSection(key);
          if (!meta) return null;
          const Icon = meta.icon;
          return (
            <Link
              key={key}
              href={appSectionHref(appKey, key)}
              scroll={false}
              className="group flex min-w-0 items-center gap-2.5 rounded-xl px-2.5 py-2 transition-colors hover:bg-accent active:bg-accent"
            >
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:bg-primary/10 group-hover:text-primary">
                <Icon className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-xs font-medium">{meta.title}</span>
                <span className="block truncate text-[10px] text-muted-foreground">{meta.summary}</span>
              </span>
              <ChevronRight className="size-3.5 shrink-0 text-muted-foreground/60 transition-transform group-hover:translate-x-0.5" />
            </Link>
          );
        })}
      </div>
    </DetailPanel>
  );
}

/** 危险操作：删除应用。确认弹窗由页面统一提供（与头部「操作」菜单同一个） */
export function InfoDangerZone({ appName, onDelete }: { appName: string; onDelete: () => void }) {
  return (
    <section className="min-w-0 rounded-2xl border border-red-500/25 bg-red-500/[0.03] p-4 sm:p-5">
      <div className="flex items-start gap-3">
        <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-red-500/10 text-red-600 dark:text-red-400">
          <TriangleAlert className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold tracking-tight">危险操作</h3>
          <p className="mt-0.5 text-[11px] leading-relaxed text-muted-foreground">
            删除「{appName}」将永久移除它的全部数据，不可撤销。只想暂时下线时，关闭「应用启用」即可。
          </p>
        </div>
      </div>
      <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
        <button
          type="button"
          onClick={onDelete}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-red-500/40 bg-background px-3 text-xs font-medium text-red-600 transition-colors hover:bg-red-500 hover:text-white dark:text-red-400"
        >
          <Trash2 className="size-3.5" />
          删除应用
        </button>
      </div>
    </section>
  );
}

