"use client";

import { ArrowUpRight, KeyRound, Link2, ListChecks, Waypoints } from "lucide-react";
import { BrandIcon, getBrandColor, getBrandIcon } from "@/components/ui/brand-icon";
import { Skeleton } from "@/components/ui/skeleton";
import { formatCount } from "@/components/apps/app-shared";
import { DetailPanel, Meter, PanelLink } from "@/components/apps/detail/app-detail-kit";
import { useAdminAppAuthSourcesQuery, useAdminAppLoginAuditsQuery } from "@/lib/admin-hooks";
import { appSectionHref } from "@/lib/app-sections";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import type { LoginAuditItem } from "@/lib/api/types";

/** 渠道 slug → 中文名。未收录的渠道回落到品牌图标库里的英文名，再回落到 slug 本身 */
const PROVIDER_NAME: Record<string, string> = {
  wechat: "微信",
  qq: "QQ",
  sinaweibo: "微博",
  github: "GitHub",
  gitee: "Gitee",
  gitlab: "GitLab",
  google: "Google",
  discord: "Discord",
  microsoft: "Microsoft",
  linux: "Linux DO",
  openid: "OpenID",
  password: "密码",
  passkey: "通行密钥",
  sms: "短信",
  email: "邮箱"
};

function providerName(slug?: string | null) {
  if (!slug) return "—";
  const key = slug.trim().toLowerCase();
  return PROVIDER_NAME[key] ?? getBrandIcon(key)?.title ?? slug;
}

/**
 * 登录方式构成：账号靠什么进来。
 *
 * 一个用户可以既设了密码又绑了几个渠道，所以「密码」「第三方」两条是各自占总用户的比例，
 * 不是互补的两段；渠道列表按绑定数排序，条长按最大渠道归一。
 */
export function InfoAuthSources({ appKey }: { appKey: string }) {
  const query = useAdminAppAuthSourcesQuery(appKey);
  const data = query.data;
  const total = data?.totalUsers ?? 0;
  const providers = [...(data?.providerBindings ?? [])].sort((a, b) => b.count - a.count);
  const topCount = Math.max(1, ...providers.map((item) => item.count));

  return (
    <DetailPanel
      icon={Waypoints}
      tone="violet"
      title="登录方式"
      description="密码与第三方渠道的覆盖情况"
      aside={
        <PanelLink href={appSectionHref(appKey, "oauth")}>
          渠道配置
          <ArrowUpRight className="size-3" />
        </PanelLink>
      }
    >
      {query.isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-14 w-full rounded-xl" />
          <Skeleton className="h-24 w-full rounded-xl" />
        </div>
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-2">
            <CoverageTile icon={KeyRound} label="设了密码" value={data?.passwordUsers} total={total} barClass="bg-sky-500" />
            <CoverageTile icon={Link2} label="绑了第三方" value={data?.oauthBoundUsers} total={total} barClass="bg-violet-500" />
          </div>

          <div>
            <div className="mb-2 flex items-center justify-between text-[11px] text-muted-foreground">
              <span>按渠道绑定数</span>
              <span className="tabular-nums">{providers.length} 个渠道</span>
            </div>
            {providers.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border px-3 py-5 text-center text-[11px] text-muted-foreground">
                还没有用户绑定第三方账号
              </div>
            ) : (
              <ul className="space-y-2">
                {providers.slice(0, 6).map((item) => {
                  const color = getBrandColor(item.source);
                  return (
                    <li key={item.source} className="flex min-w-0 items-center gap-2.5">
                      <span
                        className="grid size-7 shrink-0 place-items-center rounded-lg bg-muted"
                        style={color ? { color } : undefined}
                      >
                        <BrandIcon slug={item.source} className="size-3.5" title={providerName(item.source)} />
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-2 text-xs">
                          <span className="truncate font-medium">{providerName(item.source)}</span>
                          <span className="shrink-0 tabular-nums text-muted-foreground">{formatCount(item.count)}</span>
                        </div>
                        <div className="mt-1 h-1 overflow-hidden rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-violet-500/70 transition-[width] duration-500"
                            style={{ width: `${(item.count / topCount) * 100}%` }}
                          />
                        </div>
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      )}
    </DetailPanel>
  );
}

function CoverageTile({
  icon: Icon,
  label,
  value,
  total,
  barClass
}: {
  icon: typeof KeyRound;
  label: string;
  value?: number;
  total: number;
  barClass: string;
}) {
  const share = total > 0 && typeof value === "number" ? Math.round((value / total) * 1000) / 10 : null;
  return (
    <div className="min-w-0 space-y-2 rounded-xl border border-border/80 bg-background/60 p-3">
      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Icon className="size-3 shrink-0" />
        <span className="truncate">{label}</span>
      </div>
      <div className="flex items-baseline gap-1.5">
        <span className="truncate text-lg leading-6 font-semibold tabular-nums">{formatCount(value)}</span>
        {share !== null && <span className="text-[11px] text-muted-foreground tabular-nums">{share}%</span>}
      </div>
      <Meter
        segments={[
          { label, value: value ?? 0, className: barClass },
          { label: "其余", value: Math.max(0, total - (value ?? 0)), className: "bg-transparent" }
        ]}
        className="h-1"
      />
    </div>
  );
}

/**
 * 最近登录：最新几条登录记录（成功与失败都有），排查「用户说登不上」时先看这里。
 */
export function InfoRecentLogins({ appKey }: { appKey: string }) {
  const query = useAdminAppLoginAuditsQuery(appKey, { page: 1, limit: 6 });
  const items = query.data?.items ?? [];

  return (
    <DetailPanel
      icon={ListChecks}
      tone="sky"
      title="最近登录"
      description={query.data ? `共 ${formatCount(query.data.total)} 条登录记录` : "最新的登录尝试"}
      aside={
        <PanelLink href={appSectionHref(appKey, "audit")}>
          审计日志
          <ArrowUpRight className="size-3" />
        </PanelLink>
      }
      bodyClassName="px-2 pb-2 sm:px-2 sm:pb-2"
    >
      {query.isLoading ? (
        <div className="space-y-1.5 px-2 pb-2">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-10 w-full rounded-lg" />
          ))}
        </div>
      ) : query.isError ? (
        <div className="px-3 py-6 text-center text-[11px] text-muted-foreground">登录记录暂不可用</div>
      ) : items.length === 0 ? (
        <div className="px-3 py-6 text-center text-[11px] text-muted-foreground">还没有登录记录</div>
      ) : (
        <ul className="divide-y divide-border/60">
          {items.map((item) => (
            <LoginRow key={item.id} item={item} />
          ))}
        </ul>
      )}
    </DetailPanel>
  );
}

function LoginRow({ item }: { item: LoginAuditItem }) {
  const ok = item.status === "success";
  const who = item.nickname || item.account || (item.userId ? `#${item.userId}` : "未知账号");
  const via = item.provider || item.loginType;
  return (
    <li className="flex min-w-0 items-center gap-2.5 rounded-lg px-2 py-2 transition-colors hover:bg-accent/50">
      <span
        className={cn(
          "grid size-8 shrink-0 place-items-center rounded-full text-xs font-semibold",
          ok ? "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300" : "bg-red-500/10 text-red-600 dark:text-red-400"
        )}
        aria-hidden
      >
        {who.trim().slice(0, 1).toUpperCase()}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-xs font-medium">{who}</span>
          {via && (
            <span className="shrink-0 rounded bg-muted px-1 text-[10px] leading-4 text-muted-foreground">{providerName(via)}</span>
          )}
        </div>
        <div className="truncate font-mono text-[10px] text-muted-foreground">
          {item.loginIP || "—"}
          {item.deviceInfo?.name && <span className="ml-2 font-sans">{item.deviceInfo.name}</span>}
        </div>
      </div>
      <div className="shrink-0 text-right">
        <div className={cn("text-[11px] font-medium", ok ? "text-emerald-600 dark:text-emerald-400" : "text-red-600 dark:text-red-400")}>
          {ok ? "成功" : "失败"}
        </div>
        <div className="text-[10px] text-muted-foreground tabular-nums" title={formatDateTime(item.createdAt)}>
          {formatRelative(item.createdAt)}
        </div>
      </div>
    </li>
  );
}
