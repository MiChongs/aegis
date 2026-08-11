"use client";

import { useMemo, useState } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAdminAppLoginAuditsQuery, useAdminAppSessionAuditsQuery, useAdminAppNotificationsQuery } from "@/lib/admin-hooks";

function fmtTime(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function str(v: unknown, fb = "—") { return typeof v === "string" && v.trim() ? v : fb; }

export function AppAuditPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<"login" | "session" | "notification">("login");

  if (!appKey) return <div className="py-12 text-center text-sm text-muted-foreground">请先选择应用</div>;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5 w-fit">
        {(["login", "session", "notification"] as const).map((v) => (
          <button
            key={v}
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${view === v ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setView(v)}
          >
            {{ login: "登录审计", session: "会话审计", notification: "通知" }[v]}
          </button>
        ))}
      </div>

      {view === "login" && <LoginAuditTable appKey={appKey} />}
      {view === "session" && <SessionAuditTable appKey={appKey} />}
      {view === "notification" && <NotificationTable appKey={appKey} />}
    </div>
  );
}

function LoginAuditTable({ appKey }: { appKey: string }) {
  const query = useAdminAppLoginAuditsQuery(appKey, { page: 1, limit: 20 });
  const items = useMemo(() => {
    const list = query.data;
    return Array.isArray(list) ? list : (list as any)?.items || [];
  }, [query.data]);

  if (query.isLoading) return <Skeleton className="h-40 w-full rounded-lg" />;
  if (!items.length) return <div className="py-8 text-center text-xs text-muted-foreground">暂无登录审计数据</div>;

  return (
    <div className="overflow-hidden rounded-xl border">
      <Table>
        <TableHeader><TableRow className="hover:bg-transparent">
          <TableHead>账号</TableHead><TableHead>登录方式</TableHead><TableHead>提供方</TableHead><TableHead>状态</TableHead><TableHead>IP</TableHead><TableHead>时间</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {items.map((item: Record<string, unknown>, i: number) => (
            <TableRow key={`${str(item.account)}-${i}`}>
              <TableCell className="text-xs font-medium">{str(item.account)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.loginType)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.provider)}</TableCell>
              <TableCell className="text-xs">{str(item.status) === "success" ? <span className="text-emerald-600">成功</span> : <span className="text-red-500">{str(item.status)}</span>}</TableCell>
              <TableCell className="font-mono text-xs text-muted-foreground">{str(item.loginIP)}</TableCell>
              <TableCell className="text-xs tabular-nums text-muted-foreground">{fmtTime(item.createdAt as string)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function SessionAuditTable({ appKey }: { appKey: string }) {
  const query = useAdminAppSessionAuditsQuery(appKey, { page: 1, limit: 20 });
  const items = useMemo(() => {
    const list = query.data;
    return Array.isArray(list) ? list : (list as any)?.items || [];
  }, [query.data]);

  if (query.isLoading) return <Skeleton className="h-40 w-full rounded-lg" />;
  if (!items.length) return <div className="py-8 text-center text-xs text-muted-foreground">暂无会话审计数据</div>;

  return (
    <div className="overflow-hidden rounded-xl border">
      <Table>
        <TableHeader><TableRow className="hover:bg-transparent">
          <TableHead>账号</TableHead><TableHead>事件</TableHead><TableHead>Token</TableHead><TableHead>时间</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {items.map((item: Record<string, unknown>, i: number) => (
            <TableRow key={`${str(item.account)}-${i}`}>
              <TableCell className="text-xs font-medium">{str(item.account)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.eventType)}</TableCell>
              <TableCell className="max-w-[120px] truncate font-mono text-xs text-muted-foreground">{str(item.tokenJTI)}</TableCell>
              <TableCell className="text-xs tabular-nums text-muted-foreground">{fmtTime(item.createdAt as string)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function NotificationTable({ appKey }: { appKey: string }) {
  const query = useAdminAppNotificationsQuery(appKey, { page: 1, limit: 20 });
  const items = useMemo(() => {
    const list = query.data;
    return Array.isArray(list) ? list : (list as any)?.items || [];
  }, [query.data]);

  if (query.isLoading) return <Skeleton className="h-40 w-full rounded-lg" />;
  if (!items.length) return <div className="py-8 text-center text-xs text-muted-foreground">暂无通知数据</div>;

  return (
    <div className="overflow-hidden rounded-xl border">
      <Table>
        <TableHeader><TableRow className="hover:bg-transparent">
          <TableHead>标题</TableHead><TableHead>类型</TableHead><TableHead>级别</TableHead><TableHead>时间</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {items.map((item: Record<string, unknown>, i: number) => (
            <TableRow key={`${item.id || i}`}>
              <TableCell className="text-xs font-medium">{str(item.title)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.type)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.level)}</TableCell>
              <TableCell className="text-xs tabular-nums text-muted-foreground">{fmtTime(item.createdAt as string)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
