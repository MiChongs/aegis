"use client";

import { History, MonitorSmartphone, ShieldCheck } from "lucide-react";
import {
  ConsentBadge,
  ModeBadge,
  SplashStatusBadge,
  consentSourceLabel,
  formatMillis
} from "@/components/apps/ad-policy/ad-policy-shared";
import { formatCardDate } from "@/components/apps/card-key/card-key-shared";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useUserAdProfileQuery } from "@/lib/ad-policy-hooks";
import { EmptyRow, Fact, Facts, Panel, StatTile } from "./user-detail-shared";

/**
 * 广告服务：这个用户对广告服务的选择、因此得到的服务模式，以及开屏广告的展示记录。
 *
 *   当前处境   选择、条款版本、会员免除与最终的服务模式（出自后端同一处判定）
 *   变更历史   每一次同意与撤回：在哪里、哪台设备、哪个 IP
 *   开屏       累计与最近几次开屏的结局
 */
export function UserAdPolicyTab({ appKey, userId }: { appKey: string; userId: number }) {
  const profileQuery = useUserAdProfileQuery(appKey, userId);
  const profile = profileQuery.data;

  if (profileQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full rounded-2xl" />
        <Skeleton className="h-56 w-full rounded-2xl" />
      </div>
    );
  }
  if (!profile) {
    return <EmptyRow text="暂无数据" />;
  }

  const consent = profile.consent;
  const outdated = Boolean(consent && consent.version < profile.consentVersion);

  return (
    <div className="space-y-4">
      <Panel icon={<ShieldCheck className="size-4" />} title="当前处境" action={<ModeBadge mode={profile.mode} />}>
        <Facts columns={2}>
          <Fact
            label="选择"
            value={consent ? <ConsentBadge accepted={consent.accepted} outdated={outdated} /> : "未选择"}
          />
          <Fact label="条款版本" mono value={consent ? `v${consent.version} / 当前 v${profile.consentVersion}` : `当前 v${profile.consentVersion}`} />
          <Fact label="来源" value={consent ? consentSourceLabel(consent.source) : undefined} />
          <Fact label="选择时间" value={consent ? formatCardDate(consent.decidedAt) : undefined} />
          <Fact
            label="会员"
            value={
              profile.vip ? (
                <Badge variant={profile.exempt ? "success" : "outline"} size="sm">
                  {profile.exempt ? "会员免除" : "会员"}
                </Badge>
              ) : (
                "否"
              )
            }
          />
          <Fact
            label="策略"
            value={!profile.configured ? "未配置" : profile.enabled ? "拒绝时仅提供基础服务" : "未要求同意"}
          />
        </Facts>
      </Panel>

      <Panel icon={<MonitorSmartphone className="size-4" />} title="开屏广告">
        <div className="grid gap-3 sm:grid-cols-4">
          <StatTile label="累计展示" value={profile.splash.shown} />
          <StatTile label="累计点击" value={profile.splash.clicked} tone={profile.splash.clicked ? "info" : "default"} />
          <StatTile label="累计失败" value={profile.splash.failed} tone={profile.splash.failed ? "warning" : "default"} />
          <StatTile
            label="今日展示"
            value={profile.splash.todayShown}
            hint={profile.splash.lastShownAt ? `上次 ${formatCardDate(profile.splash.lastShownAt)}` : undefined}
          />
        </div>
        {profile.recentSplash.length === 0 ? (
          <EmptyRow text="暂无开屏记录" />
        ) : (
          <Table className="mt-3">
            <TableHeader>
              <TableRow>
                <TableHead>结果</TableHead>
                <TableHead>广告位</TableHead>
                <TableHead>加载 / 展示</TableHead>
                <TableHead className="text-right">时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {profile.recentSplash.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <SplashStatusBadge status={item.status} />
                    {item.errorCode ? (
                      <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">{item.errorCode}</span>
                    ) : null}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{item.placementId || "—"}</TableCell>
                  <TableCell className="text-xs tabular-nums text-muted-foreground">
                    {formatMillis(item.loadMs)} / {formatMillis(item.shownMs)}
                  </TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">
                    {formatCardDate(item.occurredAt)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>

      <Panel icon={<History className="size-4" />} title="变更历史">
        {profile.logs.length === 0 ? (
          <EmptyRow />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>选择</TableHead>
                <TableHead>条款版本</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>设备</TableHead>
                <TableHead className="text-right">时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {profile.logs.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <ConsentBadge accepted={item.accepted} />
                  </TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">v{item.version}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{consentSourceLabel(item.source)}</TableCell>
                  <TableCell className="max-w-44 truncate font-mono text-[11px] text-muted-foreground">
                    {item.deviceId || "—"}
                  </TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">
                    {formatCardDate(item.createdAt)}
                    {item.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{item.clientIp}</p> : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>
    </div>
  );
}
