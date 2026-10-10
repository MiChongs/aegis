"use client";

import { Fragment, useMemo, useState } from "react";
import Link from "next/link";
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  LogOut,
  MoreHorizontal,
  Search,
  Server,
  WifiOff,
  X
} from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatShortTime, formatTime, relativeTime, userInitials } from "@/components/users/detail/user-detail-shared";
import { ApiError } from "@/lib/api-client";
import type { AppSummary, OnlinePresenceConnection, OnlineUserItem } from "@/lib/api/types";
import {
  useAppOnlineStatsQuery,
  useAppOnlineUsersQuery,
  useRevokeOnlineUserSessionsMutation
} from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import { ClientIcon, ConfirmActionDialog, durationSince, parseClient, shortId } from "./session-shared";

/**
 * 应用在线用户。
 *
 * 一行是一个用户，展开后是这个用户的每条实时连接（多端同时在线时一人多条）。
 * 搜索只过滤当前页：presence 存在 Redis 有序集合里，后端分页不支持关键字，
 * 这里如实标注「在本页中」，不假装是全量搜索。
 */

const PAGE_SIZES = [20, 50, 100];
/** 心跳超过这个间隔仍未刷新，标为「心跳延迟」：连接可能已断，只是还没被清理 */
const STALE_MS = 2 * 60_000;

function isStale(value?: string) {
  if (!value) return false;
  const t = new Date(value).getTime();
  return !Number.isNaN(t) && Date.now() - t > STALE_MS;
}

function connectionsOf(item: OnlineUserItem): OnlinePresenceConnection[] {
  if (item.connectionSamples?.length) return item.connectionSamples;
  return item.sampleConnection ? [item.sampleConnection] : [];
}

function displayName(item: OnlineUserItem) {
  return item.nickname || item.account || `用户 #${item.userId}`;
}

function primaryIp(item: OnlineUserItem) {
  return item.ip || item.sampleConnection?.ip || "";
}

export function AppOnlineView({
  apps,
  appsLoading,
  autoRefresh
}: {
  apps: AppSummary[];
  appsLoading: boolean;
  autoRefresh: number | false;
}) {
  const [pickedAppId, setPickedAppId] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [keyword, setKeyword] = useState("");
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [pendingKick, setPendingKick] = useState<OnlineUserItem | null>(null);

  // 未选或所选应用已不在列表里时落到第一个应用
  const app = apps.find((a) => a.id === pickedAppId) ?? apps[0] ?? null;
  const appId = app?.id ?? null;
  const statsQuery = useAppOnlineStatsQuery(appId, { refetchInterval: autoRefresh });
  const usersQuery = useAppOnlineUsersQuery(appId, { page, limit }, { refetchInterval: autoRefresh });
  const kickMutation = useRevokeOnlineUserSessionsMutation();

  const items = useMemo(() => usersQuery.data?.items ?? [], [usersQuery.data]);
  const total = usersQuery.data?.total ?? 0;
  const totalPages = Math.max(1, usersQuery.data?.totalPages ?? 1);

  const visible = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    if (!q) return items;
    return items.filter((item) =>
      [item.account, item.nickname, primaryIp(item), String(item.userId), ...connectionsOf(item).map((c) => c.deviceId)]
        .some((v) => v?.toLowerCase().includes(q))
    );
  }, [items, keyword]);

  // 停留期间用户陆续下线，当前页可能已超出总页数
  const outOfRange = Boolean(usersQuery.data) && total > 0 && page > totalPages;

  function selectApp(id: number) {
    setPickedAppId(id);
    setPage(1);
    setExpanded(new Set());
  }

  function toggle(userId: number) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(userId)) next.delete(userId);
      else next.add(userId);
      return next;
    });
  }

  async function confirmKick() {
    if (!pendingKick || !app) return;
    try {
      await kickMutation.mutateAsync({ appKey: app.appKey, userId: pendingKick.userId });
      toast.success(`已撤销 ${displayName(pendingKick)} 的全部会话`);
      setPendingKick(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "强制下线失败");
    }
  }

  const stats = statsQuery.data;
  const loadingUsers = appsLoading || (appId != null && usersQuery.isLoading);

  if (!appsLoading && apps.length === 0) {
    return (
      <div className="flex min-h-48 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed px-6 text-center">
        <WifiOff className="size-5 text-muted-foreground" />
        <p className="text-sm font-medium">暂无可查看的应用</p>
        <p className="text-xs text-muted-foreground">创建应用或获得应用授权后，可在此查看实时在线用户</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* 应用选择 + 本应用统计 */}
      <div className="flex flex-col gap-3 rounded-2xl border bg-card p-3 sm:flex-row sm:items-center sm:justify-between">
        {appsLoading ? (
          <Skeleton className="h-9 w-full sm:w-56" />
        ) : (
          <Select value={appId != null ? String(appId) : ""} onValueChange={(v) => selectApp(Number(v))}>
            <SelectTrigger className="h-9 w-full text-sm sm:w-56">
              <SelectValue placeholder="选择应用" />
            </SelectTrigger>
            <SelectContent>
              {apps.map((a) => (
                <SelectItem key={a.id} value={String(a.id)}>
                  <span className="truncate">{a.name}</span>
                  {!a.status ? <span className="ml-1.5 text-xs text-muted-foreground">已停用</span> : null}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <dl className="grid grid-cols-3 gap-2 text-center sm:flex sm:gap-6 sm:text-left">
          <AppStat label="在线用户" value={stats?.onlineUsers} loading={statsQuery.isLoading || appsLoading} />
          <AppStat label="实时连接" value={stats?.onlineConnections} loading={statsQuery.isLoading || appsLoading} />
          <AppStat
            label="人均连接"
            value={stats && stats.onlineUsers > 0 ? (stats.onlineConnections / stats.onlineUsers).toFixed(1) : stats ? "0" : undefined}
            loading={statsQuery.isLoading || appsLoading}
          />
        </dl>
      </div>

      {/* 工具栏 */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative w-full sm:w-80">
          <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="在本页中搜索账号、昵称、IP 或设备 ID"
            className="h-9 pr-8 pl-8 text-sm"
          />
          {keyword ? (
            <button
              type="button"
              aria-label="清除搜索"
              className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
              onClick={() => setKeyword("")}
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
        <div className="flex items-center justify-between gap-2 sm:ml-auto">
          <span className="text-xs text-muted-foreground tabular-nums">
            {keyword ? `本页匹配 ${visible.length} 位` : `共 ${total.toLocaleString("zh-CN")} 位在线`}
          </span>
          {app ? (
            <Button asChild variant="outline" size="sm" className="h-9 gap-1.5">
              <Link href={`/app-users?app=${encodeURIComponent(app.appKey)}`}>
                <ExternalLink className="size-3.5" />
                <span className="hidden sm:inline">应用用户</span>
                <span className="sm:hidden">用户</span>
              </Link>
            </Button>
          ) : null}
        </div>
      </div>

      {loadingUsers ? (
        <OnlineSkeleton />
      ) : visible.length === 0 ? (
        <div className="flex min-h-48 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed px-6 text-center">
          <WifiOff className="size-5 text-muted-foreground" />
          <p className="text-sm font-medium">
            {keyword ? "本页没有匹配的在线用户" : outOfRange ? "本页用户已全部下线" : "当前没有在线用户"}
          </p>
          <p className="text-xs text-muted-foreground">
            {keyword ? "搜索仅覆盖当前页，可翻页或清除关键字" : outOfRange ? `当前共 ${totalPages} 页` : "客户端建立实时连接后会出现在这里"}
          </p>
          {outOfRange && !keyword ? (
            <Button variant="outline" size="sm" className="mt-1 h-8" onClick={() => setPage(totalPages)}>
              返回第 {totalPages} 页
            </Button>
          ) : null}
        </div>
      ) : (
        <>
          {/* 桌面表格 */}
          <div className="hidden overflow-hidden rounded-2xl border md:block">
            <Table>
              <TableHeader>
                <TableRow className="bg-muted/30 hover:bg-muted/30">
                  <TableHead className="h-9 w-8 pl-3" />
                  <TableHead className="h-9 text-xs">用户</TableHead>
                  <TableHead className="h-9 text-xs">连接</TableHead>
                  <TableHead className="h-9 text-xs">IP 地址</TableHead>
                  <TableHead className="hidden h-9 text-xs lg:table-cell">客户端</TableHead>
                  <TableHead className="h-9 text-xs">在线时长</TableHead>
                  <TableHead className="h-9 text-xs">最近心跳</TableHead>
                  <TableHead className="h-9 w-12 pr-3" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map((item) => {
                  const open = expanded.has(item.userId);
                  const conns = connectionsOf(item);
                  const client = parseClient(item.sampleConnection?.userAgent);
                  return (
                    <Fragment key={item.userId}>
                      <TableRow className="cursor-pointer" data-state={open ? "selected" : undefined} onClick={() => toggle(item.userId)}>
                        <TableCell className="py-2.5 pl-3">
                          <ChevronDown className={cn("size-4 text-muted-foreground transition-transform", !open && "-rotate-90")} />
                        </TableCell>
                        <TableCell className="py-2.5">
                          <OnlineIdentity item={item} />
                        </TableCell>
                        <TableCell className="py-2.5">
                          <Badge variant={item.connections > 1 ? "info" : "outline"} size="sm" className="tabular-nums">
                            {item.connections} 条
                          </Badge>
                        </TableCell>
                        <TableCell className="py-2.5 font-mono text-xs text-muted-foreground">{primaryIp(item) || "未知"}</TableCell>
                        <TableCell className="hidden max-w-56 py-2.5 lg:table-cell">
                          <span className="flex min-w-0 items-center gap-1.5 text-xs" title={item.sampleConnection?.userAgent}>
                            <ClientIcon kind={client.kind} className="size-3.5" />
                            <span className="truncate">{client.label}</span>
                            {conns.length > 1 ? <span className="shrink-0 text-muted-foreground">等</span> : null}
                          </span>
                        </TableCell>
                        <TableCell className="py-2.5 text-xs tabular-nums" title={formatTime(item.connectedAt)}>
                          {durationSince(item.connectedAt) || "未知"}
                        </TableCell>
                        <TableCell className="py-2.5">
                          <Heartbeat value={item.lastSeenAt} />
                        </TableCell>
                        <TableCell className="py-2.5 pr-3 text-right" onClick={(e) => e.stopPropagation()}>
                          <OnlineUserMenu item={item} app={app} onKick={setPendingKick} />
                        </TableCell>
                      </TableRow>
                      {open ? (
                        <TableRow className="bg-muted/20 hover:bg-muted/20">
                          <TableCell />
                          <TableCell colSpan={7} className="py-3 pr-4">
                            <ConnectionList connections={conns} total={item.connections} />
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  );
                })}
              </TableBody>
            </Table>
          </div>

          {/* 移动端卡片 */}
          <ul className="space-y-2.5 md:hidden">
            {visible.map((item) => {
              const open = expanded.has(item.userId);
              return (
                <li key={item.userId} className="rounded-2xl border bg-card">
                  <div className="flex items-start gap-2 p-3.5">
                    <div className="min-w-0 flex-1">
                      <OnlineIdentity item={item} />
                    </div>
                    <OnlineUserMenu item={item} app={app} onKick={setPendingKick} />
                  </div>
                  <dl className="grid grid-cols-2 gap-x-4 gap-y-2.5 border-t px-3.5 py-3 text-xs">
                    <div className="space-y-0.5">
                      <dt className="text-muted-foreground">IP 地址</dt>
                      <dd className="truncate font-mono">{primaryIp(item) || "未知"}</dd>
                    </div>
                    <div className="space-y-0.5">
                      <dt className="text-muted-foreground">实时连接</dt>
                      <dd className="tabular-nums">{item.connections} 条</dd>
                    </div>
                    <div className="space-y-0.5">
                      <dt className="text-muted-foreground">在线时长</dt>
                      <dd className="tabular-nums">{durationSince(item.connectedAt) || "未知"}</dd>
                    </div>
                    <div className="space-y-0.5">
                      <dt className="text-muted-foreground">最近心跳</dt>
                      <dd><Heartbeat value={item.lastSeenAt} /></dd>
                    </div>
                  </dl>
                  <button
                    type="button"
                    className="flex w-full items-center justify-between border-t px-3.5 py-2.5 text-xs text-muted-foreground"
                    aria-expanded={open}
                    onClick={() => toggle(item.userId)}
                  >
                    {open ? "收起连接明细" : "查看连接明细"}
                    <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
                  </button>
                  {open ? (
                    <div className="border-t bg-muted/20 px-3.5 py-3">
                      <ConnectionList connections={connectionsOf(item)} total={item.connections} />
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </>
      )}

      {/* 分页 */}
      {!loadingUsers && total > 0 ? (
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <div className="flex items-center gap-2">
            <span>每页</span>
            <Select value={String(limit)} onValueChange={(v) => { setLimit(Number(v)); setPage(1); }}>
              <SelectTrigger size="sm" className="h-8 w-20 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PAGE_SIZES.map((n) => (
                  <SelectItem key={n} value={String(n)}>{n} 条</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-center gap-1.5">
            <Button variant="outline" size="icon" className="size-8" aria-label="上一页" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              <ChevronLeft className="size-4" />
            </Button>
            <span className="min-w-16 text-center tabular-nums">{page} / {totalPages}</span>
            <Button variant="outline" size="icon" className="size-8" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </div>
      ) : null}

      <ConfirmActionDialog
        open={pendingKick !== null}
        onOpenChange={(open) => !open && setPendingKick(null)}
        title={`强制 ${pendingKick ? displayName(pendingKick) : ""} 下线？`}
        description={`将撤销该用户在「${app?.name ?? ""}」的全部登录会话，客户端需要重新登录。已建立的实时连接会在下次鉴权或心跳超时后从列表移除。`}
        confirmLabel="强制下线"
        pending={kickMutation.isPending}
        onConfirm={confirmKick}
      />
    </div>
  );
}

function AppStat({ label, value, loading }: { label: string; value?: number | string; loading: boolean }) {
  return (
    <div className="min-w-0 rounded-xl bg-muted/40 px-2 py-1.5 sm:bg-transparent sm:p-0">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className="text-lg leading-7 font-semibold tabular-nums">
        {loading ? <Skeleton className="mx-auto my-1 h-5 w-10 sm:mx-0" /> : typeof value === "number" ? value.toLocaleString("zh-CN") : value ?? "0"}
      </dd>
    </div>
  );
}

function OnlineIdentity({ item }: { item: OnlineUserItem }) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <span className="relative inline-flex shrink-0">
        <Avatar className="size-9 rounded-xl border">
          <AvatarFallback className="rounded-xl text-[11px] font-medium">{userInitials(item.nickname, item.account)}</AvatarFallback>
        </Avatar>
        <span
          className={cn(
            "absolute -right-0.5 -bottom-0.5 size-3 rounded-full border-2 border-background",
            isStale(item.lastSeenAt) ? "bg-amber-500" : "bg-emerald-500"
          )}
        />
      </span>
      <div className="min-w-0">
        <div className="truncate text-sm font-medium">{displayName(item)}</div>
        <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          {item.account && item.nickname ? <span className="truncate font-mono">{item.account}</span> : null}
          <span className="shrink-0 tabular-nums">ID {item.userId}</span>
        </div>
      </div>
    </div>
  );
}

function Heartbeat({ value }: { value?: string }) {
  const stale = isStale(value);
  return (
    <span className={cn("inline-flex flex-col text-xs", stale && "text-amber-600 dark:text-amber-400")} title={formatTime(value)}>
      <span>{relativeTime(value)}</span>
      {stale ? <span className="text-[11px]">心跳延迟</span> : null}
    </span>
  );
}

function OnlineUserMenu({
  item,
  app,
  onKick
}: {
  item: OnlineUserItem;
  app: AppSummary | null;
  onKick: (item: OnlineUserItem) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-8" aria-label="更多操作">
          <MoreHorizontal className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        {app ? (
          <DropdownMenuItem asChild>
            <Link href={`/app-users/${encodeURIComponent(app.appKey)}/${item.userId}`}>
              <ExternalLink />
              用户详情
            </Link>
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" disabled={!app} onClick={() => onKick(item)}>
          <LogOut />
          强制下线
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** 一位用户的连接明细。后端只回传样本（最多若干条），条数不足时注明。 */
function ConnectionList({ connections, total }: { connections: OnlinePresenceConnection[]; total: number }) {
  if (connections.length === 0) {
    return <p className="text-xs text-muted-foreground">暂无连接明细</p>;
  }
  return (
    <div className="space-y-2">
      <div className="grid gap-2 lg:grid-cols-2">
        {connections.map((c) => {
          const client = parseClient(c.userAgent);
          return (
            <div key={c.connectionId} className="min-w-0 rounded-xl border bg-background p-3">
              <div className="flex items-start gap-2.5">
                <ClientIcon kind={client.kind} className="mt-0.5" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium" title={c.userAgent}>{client.label}</div>
                  <div className="truncate text-[11px] text-muted-foreground" title={c.connectionId}>
                    连接 <span className="font-mono">{shortId(c.connectionId, 10, 6)}</span>
                  </div>
                </div>
                {isStale(c.lastSeenAt) ? <Badge variant="warning" size="sm">心跳延迟</Badge> : null}
              </div>
              <dl className="mt-2.5 grid grid-cols-[4.5rem_minmax(0,1fr)] gap-x-2 gap-y-1 text-xs">
                <ConnFact label="IP 地址" value={c.ip} mono />
                <ConnFact label="设备 ID" value={c.deviceId} mono />
                <ConnFact label="建立时间" value={formatShortTime(c.connectedAt)} hint={durationSince(c.connectedAt)} />
                <ConnFact label="最近心跳" value={relativeTime(c.lastSeenAt)} />
                <ConnFact label="接入节点" value={c.serverId} mono icon />
                <ConnFact label="令牌" value={c.tokenId ? shortId(c.tokenId) : undefined} mono title={c.tokenId} />
              </dl>
            </div>
          );
        })}
      </div>
      {total > connections.length ? (
        <p className="text-[11px] text-muted-foreground">
          共 {total} 条连接，此处展示其中 {connections.length} 条样本
        </p>
      ) : null}
    </div>
  );
}

function ConnFact({
  label,
  value,
  hint,
  mono,
  icon,
  title
}: {
  label: string;
  value?: string;
  hint?: string;
  mono?: boolean;
  icon?: boolean;
  title?: string;
}) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("flex min-w-0 items-center gap-1", !value && "text-muted-foreground")} title={title ?? value}>
        {icon && value ? <Server className="size-3 shrink-0 text-muted-foreground" /> : null}
        <span className={cn("truncate", mono && value && "font-mono text-[11px]")}>{value || "未上报"}</span>
        {hint ? <span className="shrink-0 text-muted-foreground">（{hint}）</span> : null}
      </dd>
    </>
  );
}

function OnlineSkeleton() {
  return (
    <>
      <div className="hidden overflow-hidden rounded-2xl border md:block">
        <div className="h-9 border-b bg-muted/30" />
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="flex items-center gap-6 border-b px-4 py-2.5 last:border-b-0">
            <Skeleton className="size-4" />
            <div className="flex w-52 items-center gap-3">
              <Skeleton className="size-9 rounded-xl" />
              <div className="space-y-1.5">
                <Skeleton className="h-3.5 w-24" />
                <Skeleton className="h-3 w-16" />
              </div>
            </div>
            <Skeleton className="h-5 w-12 rounded-full" />
            <Skeleton className="h-3.5 w-24" />
            <Skeleton className="h-3.5 w-28" />
            <Skeleton className="h-3.5 w-16" />
            <Skeleton className="ml-auto size-8 rounded-md" />
          </div>
        ))}
      </div>
      <div className="space-y-2.5 md:hidden">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="space-y-3 rounded-2xl border p-3.5">
            <div className="flex items-center gap-3">
              <Skeleton className="size-9 rounded-xl" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3.5 w-24" />
                <Skeleton className="h-3 w-16" />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Skeleton className="h-8" />
              <Skeleton className="h-8" />
              <Skeleton className="h-8" />
              <Skeleton className="h-8" />
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
