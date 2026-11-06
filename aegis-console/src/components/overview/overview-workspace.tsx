"use client";

import Link from "next/link";
import { AlertTriangle, ChevronRight, CircleCheck, ClipboardList, ListChecks, Mail, Shield, ShieldAlert } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Item, ItemActions, ItemContent, ItemDescription, ItemGroup, ItemMedia, ItemTitle } from "@/components/ui/item";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { formatCompact, formatDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { CardEmpty, CardLink, OverviewCard } from "./overview-kit";

/* 审计明细形如 `GET /api/admin/auth/me → 200 (0ms)`，解析失败时按原文展示。 */
const DETAIL_RE = /^([A-Z]+)\s+(\S+)\s*(?:→|->)\s*(\d{3})(?:\s*\((\d+)\s*ms\))?/;

function parseDetail(detail: string) {
  const matched = DETAIL_RE.exec(detail.trim());
  if (!matched) return null;
  return {
    method: matched[1]!,
    path: matched[2]!,
    status: Number(matched[3]),
    latency: matched[4] ? Number(matched[4]) : undefined
  };
}

function methodClass(method: string) {
  const key = method.toLowerCase();
  return ["post", "put", "delete", "get"].includes(key) ? `chip-method--${key}` : "chip-method--other";
}

function statusClass(status: number) {
  if (status >= 500) return "status-code-5xx";
  if (status >= 400) return "status-code-4xx";
  if (status >= 300) return "status-code-3xx";
  if (status >= 200) return "status-code-success";
  return "status-code-idle";
}

/** 审计路径里 UUID 在中间、动作名在末尾，截断时保头保尾。 */
function elidePath(path: string, max = 52) {
  if (path.length <= max) return path;
  const tail = Math.floor((max - 1) * 0.62);
  return `${path.slice(0, max - 1 - tail)}…${path.slice(-tail)}`;
}

const ALERT_TONE: Record<string, string> = {
  firewall_block: "bg-rose-500/12 text-rose-600 dark:text-rose-400",
  login_failed: "bg-amber-500/12 text-amber-600 dark:text-amber-400"
};

/**
 * 待处理：审批类待办与安全提醒合在一处。两者都是「需要有人去看一眼」，
 * 分成两块时没有提醒的那块常年空着。
 */
export function AttentionPanel({ className }: { className?: string }) {
  const { data, isLoading } = useAdminDashboardQuery();

  const todos = [
    // 组织页的 Tab 只存在于组件 state，带 ?tab= 不会切面板，这里只能落到页面本身
    { icon: Mail, label: "部门邀请待处理", count: data?.pendingInvitations ?? 0, href: "/organization" },
    { icon: Shield, label: "角色申请待审核", count: data?.pendingRoleApps ?? 0, href: "/reviews" }
  ].filter((todo) => todo.count > 0);
  const alerts = data?.alerts ?? [];
  const total = todos.reduce((sum, todo) => sum + todo.count, 0) + alerts.length;

  return (
    <OverviewCard
      icon={ListChecks}
      tone={total > 0 ? "rose" : "muted"}
      title="待处理"
      className={className}
      action={
        total > 0 ? (
          <Badge variant="danger" size="sm" className="font-data tabular-nums">
            {total}
          </Badge>
        ) : null
      }
    >
      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-14 rounded-lg" />
          <Skeleton className="h-14 rounded-lg" />
        </div>
      ) : total === 0 ? (
        <CardEmpty icon={CircleCheck} title="暂无待处理事项" />
      ) : (
        <ItemGroup className="gap-2">
          {todos.map((todo) => (
            <Item key={todo.href} variant="outline" size="sm" asChild>
              <Link href={todo.href}>
                <ItemMedia variant="icon" className="rounded-md border-transparent bg-primary/10 text-primary">
                  <todo.icon />
                </ItemMedia>
                <ItemContent className="min-w-0">
                  <ItemTitle className="text-xs">{todo.label}</ItemTitle>
                </ItemContent>
                <ItemActions>
                  <span className="font-data text-sm font-semibold tabular-nums">{todo.count}</span>
                  <ChevronRight className="size-4 text-muted-foreground" />
                </ItemActions>
              </Link>
            </Item>
          ))}
          {alerts.map((alert, index) => (
            <Item key={`${alert.type}-${index}`} variant="outline" size="sm" asChild>
              <Link href="/security">
                <ItemMedia variant="icon" className={cn("rounded-md border-transparent", ALERT_TONE[alert.type] ?? "bg-muted text-muted-foreground")}>
                  {alert.type === "firewall_block" ? <ShieldAlert /> : <AlertTriangle />}
                </ItemMedia>
                <ItemContent className="min-w-0">
                  <ItemTitle className="text-xs">{alert.title}</ItemTitle>
                  <ItemDescription className="truncate text-xs">{alert.detail}</ItemDescription>
                </ItemContent>
                <ItemActions>
                  <ChevronRight className="size-4 text-muted-foreground" />
                </ItemActions>
              </Link>
            </Item>
          ))}
        </ItemGroup>
      )}
    </OverviewCard>
  );
}

export function RecentActivity({ className }: { className?: string }) {
  const { data, isLoading } = useAdminDashboardQuery();
  const logs = data?.recentAuditLogs ?? [];

  return (
    <OverviewCard
      icon={ClipboardList}
      tone="violet"
      title="最近操作"
      description="本账号"
      className={className}
      action={<CardLink href="/audit">审计日志</CardLink>}
    >
      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-9 rounded-md" />
          ))}
        </div>
      ) : logs.length === 0 ? (
        <CardEmpty icon={ClipboardList} title="暂无操作记录" />
      ) : (
        <ol className="-mx-2 flex flex-col divide-y divide-border/60">
          {logs.map((log, index) => {
            const parsed = parseDetail(log.detail || "");
            const time = (
              <Tooltip>
                <TooltipTrigger asChild>
                  <time dateTime={log.createdAt} className="shrink-0 font-data text-[11px] text-muted-foreground tabular-nums">
                    {formatCompact(log.createdAt, "")}
                  </time>
                </TooltipTrigger>
                <TooltipContent>{formatDateTime(log.createdAt)}</TooltipContent>
              </Tooltip>
            );
            return (
              <li key={index} className="flex min-w-0 items-center gap-3 rounded-md px-2 py-2 text-xs transition-colors hover:bg-muted/50">
                {parsed ? (
                  <>
                    <span className={cn("chip-method w-14 shrink-0 justify-center", methodClass(parsed.method))}>{parsed.method}</span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-data text-[11px] text-foreground/80" title={parsed.path}>
                        {elidePath(parsed.path)}
                      </span>
                      {/* 手机上状态码与耗时折到路径下面一行，路径才放得下 */}
                      <span className="mt-0.5 flex items-center gap-2 sm:hidden">
                        <span className={cn("font-data text-[11px] font-medium", statusClass(parsed.status))}>{parsed.status}</span>
                        {typeof parsed.latency === "number" ? (
                          <span className="font-data text-[11px] text-muted-foreground">{parsed.latency}ms</span>
                        ) : null}
                      </span>
                    </span>
                    <span className={cn("hidden shrink-0 font-data text-[11px] font-medium sm:block", statusClass(parsed.status))}>{parsed.status}</span>
                    <span className="hidden w-12 shrink-0 text-right font-data text-[11px] text-muted-foreground sm:block">
                      {typeof parsed.latency === "number" ? `${parsed.latency}ms` : ""}
                    </span>
                    {time}
                  </>
                ) : (
                  <>
                    <Badge variant="outline" size="sm" className="shrink-0 font-mono">
                      {log.action.split(".").pop()}
                    </Badge>
                    <span className="min-w-0 flex-1 truncate text-muted-foreground">{log.detail}</span>
                    {time}
                  </>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </OverviewCard>
  );
}
