"use client";

import Link from "next/link";
import { History, Loader2, XCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatShortTime, formatTime, isPast, relativeTime } from "@/components/users/detail/user-detail-shared";
import type { AdminSessionRecord } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { ClientIcon, parseClient, shortId } from "./session-shared";

/**
 * 一条管理员登录会话。管理员详情抽屉与在线会话页的「管理员」视图共用。
 *
 * 当前会话不给撤销按钮：撤销自己正在用的会话等于把自己踢下线，
 * 那件事应该走「退出登录」，而不是在列表里误点出来。
 */
export function AdminSessionRow({
  session,
  current,
  showAdmin,
  revoking,
  onRevoke,
  className
}: {
  session: AdminSessionRecord;
  current: boolean;
  /** 在线会话页里同一张表混着多位管理员，需要标出是谁 */
  showAdmin?: boolean;
  revoking?: boolean;
  onRevoke?: (session: AdminSessionRecord) => void;
  className?: string;
}) {
  const client = parseClient(session.userAgent);
  const label = session.device ? `${session.device}${client.browser ? ` / ${client.browser}` : ""}` : client.label;
  const expired = isPast(session.expiresAt);

  return (
    <div className={cn("flex items-start gap-3 py-3", className)}>
      <span className="mt-0.5 grid size-9 shrink-0 place-items-center rounded-xl border bg-muted/40">
        <ClientIcon kind={client.kind} />
      </span>

      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          {showAdmin ? (
            <span className="truncate text-sm font-medium">{session.adminName || session.adminAccount || `管理员 #${session.adminId}`}</span>
          ) : null}
          <span className={cn("truncate", showAdmin ? "text-xs text-muted-foreground" : "text-sm font-medium")} title={session.userAgent}>
            {label}
          </span>
          {current ? <Badge variant="success" size="sm">当前会话</Badge> : null}
          {expired ? <Badge variant="outline" size="sm">已过期</Badge> : null}
        </div>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          <span className="font-mono">{session.ip || "未知 IP"}</span>
          <span title={formatTime(session.lastActiveAt)}>{relativeTime(session.lastActiveAt)}活跃</span>
          <span className="hidden sm:inline" title={formatTime(session.issuedAt)}>
            登录于 {formatShortTime(session.issuedAt)}
          </span>
          <span title={formatTime(session.expiresAt)}>{expired ? "已到期" : `${relativeTime(session.expiresAt)}到期`}</span>
        </div>

        <div className="flex flex-wrap items-center gap-x-3 text-[11px] text-muted-foreground/80">
          <span title={session.id}>会话 <span className="font-mono">{shortId(session.id)}</span></span>
          <span className="sm:hidden">登录于 {formatShortTime(session.issuedAt)}</span>
          <Link
            href={`/audit?session=${encodeURIComponent(session.id)}`}
            className="inline-flex items-center gap-1 underline-offset-2 hover:text-foreground hover:underline"
          >
            <History className="size-3" />
            操作记录
          </Link>
        </div>
      </div>

      {onRevoke && !current ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-8 shrink-0 gap-1 px-2 text-xs text-destructive hover:text-destructive"
          disabled={revoking}
          onClick={() => onRevoke(session)}
        >
          {revoking ? <Loader2 className="size-3.5 animate-spin" /> : <XCircle className="size-3.5" />}
          <span className="hidden sm:inline">撤销</span>
        </Button>
      ) : null}
    </div>
  );
}
