"use client";

import { useMemo, useState } from "react";
import { ShieldAlert, ShieldCheck, UserCircle2 } from "lucide-react";
import { GeoHotspotPanel } from "@/components/security/geo-hotspot-panel";
import { MetricCard } from "@/components/dashboard/metric-card";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SurfaceCard } from "@/components/ui/surface-card";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  useAdminAppAuthSourcesQuery,
  useAdminAppLoginAuditsQuery,
  useAdminAppsQuery,
  useAdminAppSessionAuditsQuery,
  useAdminAppStatsQuery,
  useAdminAppTrendQuery,
  useAppOnlineStatsQuery,
  useSystemOnlineStatsQuery
} from "@/lib/admin-hooks";

function formatCount(value?: number | null) {
  return new Intl.NumberFormat("zh-CN").format(value || 0);
}

function formatDate(value?: string | null) {
  if (!value) {
    return "未记录";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "未记录";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

function getRecord(value: unknown) {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function getItems(value: unknown) {
  if (Array.isArray(value)) {
    return value.filter((item): item is Record<string, unknown> => typeof item === "object" && item !== null);
  }
  const record = getRecord(value);
  const items = record?.items;
  if (Array.isArray(items)) {
    return items.filter((item): item is Record<string, unknown> => typeof item === "object" && item !== null);
  }
  return [];
}

function getText(source: Record<string, unknown> | null | undefined, keys: string[], fallback = "未记录") {
  for (const key of keys) {
    const value = source?.[key];
    if (typeof value === "string" && value.trim() !== "") {
      return value;
    }
  }
  return fallback;
}

function getNumber(source: Record<string, unknown> | null | undefined, keys: string[], fallback = 0) {
  for (const key of keys) {
    const value = source?.[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
  }
  return fallback;
}

export function SecurityOverviewPanel() {
  const appsQuery = useAdminAppsQuery();
  const [selectedAppId, setSelectedAppId] = useState<number | null>(null);

  const selectedApp = useMemo(() => {
    const apps = appsQuery.data || [];
    if (!apps.length) {
      return null;
    }
    return apps.find((item) => item.id === selectedAppId) || apps[0];
  }, [appsQuery.data, selectedAppId]);

  const statsQuery = useAdminAppStatsQuery(selectedApp?.appKey);
  const trendQuery = useAdminAppTrendQuery(selectedApp?.appKey, 7);
  const authSourcesQuery = useAdminAppAuthSourcesQuery(selectedApp?.appKey);
  const loginAuditsQuery = useAdminAppLoginAuditsQuery(selectedApp?.appKey, { page: 1, limit: 6 });
  const sessionAuditsQuery = useAdminAppSessionAuditsQuery(selectedApp?.appKey, { page: 1, limit: 6 });
  const systemOnlineStatsQuery = useSystemOnlineStatsQuery();
  const appOnlineStatsQuery = useAppOnlineStatsQuery(selectedApp?.id);

  if (appsQuery.isLoading) {
    return <LoadingState title="正在加载安全数据" description="正在读取应用安全状态。" />;
  }

  if (!selectedApp) {
    return <EmptyState title="暂无应用数据" description="请先创建应用。" />;
  }

  const stats = statsQuery.data;
  const trend = trendQuery.data;
  const authSources = getItems(authSourcesQuery.data);
  const loginAudits = loginAuditsQuery.data?.items || [];
  const sessionAudits = sessionAuditsQuery.data?.items || [];
  const systemOnline = getRecord(systemOnlineStatsQuery.data);
  const appOnline = getRecord(appOnlineStatsQuery.data);

  const metrics = [
    {
      label: "成功登录",
      value: formatCount(stats?.loginSuccessToday),
      delta: `失败 ${formatCount(stats?.loginFailureToday)}`,
      detail: "今日登录"
    },
    {
      label: "在线用户",
      value: formatCount(getNumber(appOnline, ["onlineUsers", "users", "totalUsers"])),
      delta: `连接 ${formatCount(getNumber(appOnline, ["connections", "totalConnections"]))}`,
      detail: "当前应用"
    },
    {
      label: "近 7 日新增",
      value: formatCount(trend?.totalNew),
      delta: `启用用户 ${formatCount(stats?.enabledUsers)}`,
      detail: "用户增长"
    },
    {
      label: "全站在线",
      value: formatCount(getNumber(systemOnline, ["onlineUsers", "users", "totalUsers"])),
      delta: `连接 ${formatCount(getNumber(systemOnline, ["connections", "totalConnections"]))}`,
      detail: "实时状态"
    }
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold text-foreground">应用概览</h2>
          <div className="text-sm text-muted-foreground">登录、地区、审计与在线状态。</div>
        </div>
        <Select value={String(selectedApp.id)} onValueChange={(value) => setSelectedAppId(Number(value))}>
          <SelectTrigger className="w-full md:w-[240px]">
            <SelectValue placeholder="选择应用" />
          </SelectTrigger>
          <SelectContent>
            {(appsQuery.data || []).map((item) => (
              <SelectItem key={item.id} value={String(item.id)}>
                {item.name} ({item.id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <section className="metrics-grid">
        {metrics.map((item) => (
          <MetricCard key={item.label} {...item} />
        ))}
      </section>

      <SurfaceCard>
        <Tabs defaultValue="region" className="space-y-5">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
            <div className="space-y-2">
              <Badge variant="outline">分布</Badge>
              <h3 className="text-lg font-semibold text-foreground">地区与认证来源</h3>
            </div>
            <TabsList>
              <TabsTrigger value="region">地区</TabsTrigger>
              <TabsTrigger value="source">来源</TabsTrigger>
              <TabsTrigger value="overview">摘要</TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="region">
            <GeoHotspotPanel appKey={selectedApp.appKey} formatCount={formatCount} />
          </TabsContent>

          <TabsContent value="source">
            {authSources.length === 0 ? (
              <EmptyState title="暂无认证来源" description="未获取到认证来源统计。" />
            ) : (
              <div className="table-shell">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>来源</TableHead>
                      <TableHead className="text-right">认证次数</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {authSources.map((item, index) => {
                      const label = getText(item, ["source", "name", "label"], `来源 ${index + 1}`);
                      const count = getNumber(item, ["count", "value", "total"]);
                      return (
                        <TableRow key={`${label}-${index}`}>
                          <TableCell className="font-semibold text-foreground">{label}</TableCell>
                          <TableCell className="text-right">{formatCount(count)}</TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </TabsContent>

          <TabsContent value="overview">
            <div className="metrics-grid">
              <SurfaceCard>
                <div className="flex items-center gap-3">
                  <ShieldCheck className="size-5 text-primary" />
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">成功登录</div>
                    <div className="text-2xl font-semibold text-foreground">{formatCount(stats?.loginSuccessToday)}</div>
                  </div>
                </div>
              </SurfaceCard>
              <SurfaceCard>
                <div className="flex items-center gap-3">
                  <ShieldAlert className="size-5 text-primary" />
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">失败登录</div>
                    <div className="text-2xl font-semibold text-foreground">{formatCount(stats?.loginFailureToday)}</div>
                  </div>
                </div>
              </SurfaceCard>
              <SurfaceCard>
                <div className="flex items-center gap-3">
                  <UserCircle2 className="size-5 text-primary" />
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">当前在线</div>
                    <div className="text-2xl font-semibold text-foreground">
                      {formatCount(getNumber(appOnline, ["onlineUsers", "users", "totalUsers"]))}
                    </div>
                  </div>
                </div>
              </SurfaceCard>
            </div>
          </TabsContent>
        </Tabs>
      </SurfaceCard>

      <SurfaceCard>
        <div className="section-stack">
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-2">
              <Badge variant="outline">在线</Badge>
              <h3 className="text-lg font-semibold text-foreground">实时连接</h3>
            </div>
          </div>
          <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-4">
            <div className="rounded-[24px] border border-border/70 bg-card/60 px-5 py-4">
              <div className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">全站在线用户</div>
              <div className="mt-3 font-data text-3xl font-semibold text-foreground">
                {formatCount(getNumber(systemOnline, ["onlineUsers", "users", "totalUsers"]))}
              </div>
            </div>
            <div className="rounded-[24px] border border-border/70 bg-card/60 px-5 py-4">
              <div className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">全站连接数</div>
              <div className="mt-3 font-data text-3xl font-semibold text-foreground">
                {formatCount(getNumber(systemOnline, ["connections", "totalConnections"]))}
              </div>
            </div>
            <div className="rounded-[24px] border border-border/70 bg-card/60 px-5 py-4">
              <div className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">当前应用在线</div>
              <div className="mt-3 font-data text-3xl font-semibold text-foreground">
                {formatCount(getNumber(appOnline, ["onlineUsers", "users", "totalUsers"]))}
              </div>
            </div>
            <div className="rounded-[24px] border border-border/70 bg-card/60 px-5 py-4">
              <div className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">当前应用连接</div>
              <div className="mt-3 font-data text-3xl font-semibold text-foreground">
                {formatCount(getNumber(appOnline, ["connections", "totalConnections"]))}
              </div>
            </div>
          </div>
        </div>
      </SurfaceCard>

      <SurfaceCard>
        <Tabs defaultValue="login-audit" className="space-y-5">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
            <div className="space-y-2">
              <Badge variant="outline">审计</Badge>
              <h3 className="text-lg font-semibold text-foreground">审计记录</h3>
            </div>
            <TabsList>
              <TabsTrigger value="login-audit">登录</TabsTrigger>
              <TabsTrigger value="session-audit">会话</TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="login-audit">
            {loginAudits.length === 0 ? (
              <EmptyState title="暂无登录审计" description="当前应用暂无登录审计。" />
            ) : (
              <div className="table-shell">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>账号</TableHead>
                      <TableHead>登录方式</TableHead>
                      <TableHead>提供方</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead className="text-right">时间</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {loginAudits.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="font-semibold text-foreground">{item.account || item.nickname || `记录 ${item.id}`}</TableCell>
                        <TableCell>{item.loginType || "unknown"}</TableCell>
                        <TableCell>{item.provider || "local"}</TableCell>
                        <TableCell>{item.status || "unknown"}</TableCell>
                        <TableCell className="text-right text-muted-foreground">{formatDate(item.createdAt)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </TabsContent>

          <TabsContent value="session-audit">
            {sessionAudits.length === 0 ? (
              <EmptyState title="暂无会话审计" description="当前应用暂无会话审计。" />
            ) : (
              <div className="table-shell">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>账号</TableHead>
                      <TableHead>事件</TableHead>
                      <TableHead>Token JTI</TableHead>
                      <TableHead className="text-right">时间</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sessionAudits.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="font-semibold text-foreground">{item.account || item.nickname || `记录 ${item.id}`}</TableCell>
                        <TableCell>{item.eventType || "unknown"}</TableCell>
                        <TableCell>{item.tokenJTI || "无 TokenJTI"}</TableCell>
                        <TableCell className="text-right text-muted-foreground">{formatDate(item.createdAt)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </TabsContent>
        </Tabs>
      </SurfaceCard>
    </div>
  );
}
