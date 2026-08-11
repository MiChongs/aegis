"use client";

import Link from "next/link";
import {
  AlertTriangle, ArrowRight, CheckCircle2, ClipboardList,
  ListChecks, Mail, Shield,
} from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  审计明细解析                                                        */
/*  detail 形如 `GET /api/admin/auth/me → 200 (0ms)`；解析失败按原文展示   */
/* ------------------------------------------------------------------ */

const DETAIL_RE = /^([A-Z]+)\s+(\S+)\s*(?:→|->)\s*(\d{3})(?:\s*\((\d+)\s*ms\))?/;

type ParsedDetail = {
  method: string;
  path: string;
  status: number;
  latency?: number;
};

function parseDetail(detail: string): ParsedDetail | null {
  const m = DETAIL_RE.exec(detail.trim());
  if (!m) return null;
  return { method: m[1], path: m[2], status: Number(m[3]), latency: m[4] ? Number(m[4]) : undefined };
}

function methodClass(method: string): string {
  const key = method.toLowerCase();
  return ["post", "put", "delete", "get"].includes(key) ? `chip-method--${key}` : "chip-method--other";
}

function statusClass(status: number): string {
  if (status >= 500) return "status-code-5xx";
  if (status >= 400) return "status-code-4xx";
  if (status >= 300) return "status-code-3xx";
  if (status >= 200) return "status-code-success";
  return "status-code-idle";
}

/**
 * 路径省略：审计路径里 UUID 在中间、动作名在末尾，直接 `truncate` 会把最有信息量的
 * 尾部切掉，这里改成中间打点、保头保尾。
 */
function elidePath(path: string, max = 46): string {
  if (path.length <= max) return path;
  const tail = Math.floor((max - 1) * 0.62);
  return `${path.slice(0, max - 1 - tail)}…${path.slice(-tail)}`;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

/* ------------------------------------------------------------------ */
/*  空状态 —— 统一用 lucide 线性图标，不使用 Emoji                        */
/* ------------------------------------------------------------------ */

function PanelEmpty({ icon: Icon, title, hint }: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  hint?: string;
}) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-1.5 py-8 text-center">
      <Icon className="size-7 text-muted-foreground/30" />
      <p className="text-xs text-muted-foreground">{title}</p>
      {hint ? <p className="text-[10px] text-muted-foreground/70">{hint}</p> : null}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  待办事项                                                            */
/* ------------------------------------------------------------------ */

export function PendingTasks() {
  const { data, isLoading } = useAdminDashboardQuery();

  const todos = [
    // 组织页的 Tab 只存在于组件 state，带 ?tab= 不会切面板，这里只能落到页面本身
    { icon: Mail, label: "部门邀请待处理", count: data?.pendingInvitations ?? 0, href: "/organization" },
    { icon: Shield, label: "角色申请待审核", count: data?.pendingRoleApps ?? 0, href: "/reviews" },
  ].filter((t) => t.count > 0);

  return (
    <Card className="h-full py-0">
      <CardContent className="flex h-full flex-col gap-3 p-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <ListChecks className="size-4 text-muted-foreground" />
            待办事项
          </h3>
          {todos.length > 0 ? (
            <Badge variant="danger" className="text-[9px]">{todos.reduce((s, t) => s + t.count, 0)}</Badge>
          ) : null}
        </div>

        {isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-10 rounded-lg" />
            <Skeleton className="h-10 rounded-lg" />
          </div>
        ) : todos.length === 0 ? (
          <PanelEmpty icon={CheckCircle2} title="暂无待办事项" hint="邀请与角色申请都已处理完" />
        ) : (
          <div className="flex flex-col gap-2">
            {todos.map((t) => (
              <Link
                key={t.href}
                href={t.href}
                className="group/todo flex items-center gap-3 rounded-lg border px-3 py-2 transition-colors hover:bg-muted/60"
              >
                <t.icon className="size-4 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate text-xs">{t.label}</span>
                <Badge variant="danger" className="text-[9px]">{t.count}</Badge>
                <ArrowRight className="size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/todo:opacity-100" />
              </Link>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

/* ------------------------------------------------------------------ */
/*  最近操作                                                            */
/* ------------------------------------------------------------------ */

export function RecentActivity() {
  const { data, isLoading } = useAdminDashboardQuery();
  const logs = data?.recentAuditLogs ?? [];

  return (
    <Card className="h-full py-0">
      <CardContent className="flex h-full flex-col gap-3 p-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <ClipboardList className="size-4 text-muted-foreground" />
            最近操作
          </h3>
          <Link
            href="/audit"
            className="inline-flex items-center gap-1 text-[10px] text-muted-foreground transition-colors hover:text-foreground"
          >
            查看全部 <ArrowRight className="size-3" />
          </Link>
        </div>

        {isLoading ? (
          <div className="space-y-1.5">
            {Array.from({ length: 5 }, (_, i) => <Skeleton key={i} className="h-7 rounded-md" />)}
          </div>
        ) : logs.length === 0 ? (
          <PanelEmpty icon={ClipboardList} title="暂无操作记录" hint="你的管理操作会实时记录在这里" />
        ) : (
          <ul className="flex flex-col">
            {logs.map((log, i) => {
              const parsed = parseDetail(log.detail || "");
              return (
                <li
                  key={i}
                  className="flex items-center gap-2.5 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-muted/50"
                >
                  {parsed ? (
                    <>
                      <span className={cn("chip-method shrink-0", methodClass(parsed.method))}>{parsed.method}</span>
                      <span className="min-w-0 flex-1 truncate font-data text-[11px] text-muted-foreground" title={parsed.path}>
                        {elidePath(parsed.path)}
                      </span>
                      <span className={cn("shrink-0 font-data text-[11px] font-medium", statusClass(parsed.status))}>
                        {parsed.status}
                      </span>
                      {typeof parsed.latency === "number" ? (
                        <span className="w-12 shrink-0 text-right font-data text-[10px] text-muted-foreground/60">
                          {parsed.latency}ms
                        </span>
                      ) : null}
                    </>
                  ) : (
                    <>
                      <Badge variant="outline" className="shrink-0 font-mono text-[9px]">
                        {log.action.split(".").pop()}
                      </Badge>
                      <span className="min-w-0 flex-1 truncate text-muted-foreground">{log.detail}</span>
                    </>
                  )}
                  <span className="w-9 shrink-0 text-right font-data text-[10px] text-muted-foreground/60">
                    {fmtTime(log.createdAt)}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

/* ------------------------------------------------------------------ */
/*  异常提醒 —— 无告警时整块不渲染                                        */
/* ------------------------------------------------------------------ */

export function SystemAlerts() {
  const { data } = useAdminDashboardQuery();
  const alerts = data?.alerts ?? [];

  if (alerts.length === 0) return null;

  return (
    <Card className="py-0">
      <CardContent className="space-y-3 p-4">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <AlertTriangle className="size-4 text-amber-500" />
          异常提醒
        </h3>
        <div className="grid gap-1.5 md:grid-cols-2">
          {alerts.map((alert, i) => (
            <div
              key={i}
              className={cn(
                "flex items-center gap-2 rounded-lg px-3 py-2 text-xs",
                alert.type === "firewall_block" && "bg-red-500/10 text-red-700 dark:text-red-400",
                alert.type === "login_failed" && "bg-amber-500/10 text-amber-700 dark:text-amber-400",
              )}
            >
              <AlertTriangle className="size-3.5 shrink-0" />
              <span className="font-medium">{alert.title}</span>
              <span className="truncate text-muted-foreground">{alert.detail}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
