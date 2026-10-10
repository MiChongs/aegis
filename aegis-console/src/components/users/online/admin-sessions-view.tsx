"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, LogOut, Search, ShieldCheck, X } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { formatTime, relativeTime } from "@/components/users/detail/user-detail-shared";
import { adminInitials, flattenAdmins } from "@/components/users/admins/admin-shared";
import { ApiError } from "@/lib/api-client";
import type { AdminSessionRecord, OnlineAdmin } from "@/lib/api/types";
import {
  useAdminAccountsQuery,
  useAdminSessionsQuery,
  useAllSessionsQuery,
  useForceLogoutMutation,
  useRevokeSessionMutation
} from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import { AdminSessionRow } from "./admin-session-row";
import { ConfirmActionDialog, parseClient } from "./session-shared";

const PAGE_SIZE = 30;

type Pending =
  | { kind: "session"; session: AdminSessionRecord }
  | { kind: "admin"; admin: OnlineAdmin };

/**
 * 管理员会话（仅超管）。
 *
 * 上面一排是在线管理员，点一位就只看他的会话（走按管理员查询的接口，拿到的是全量，
 * 不受分页影响）；不选时是全站有效会话的分页列表。
 */
export function AdminSessionsView({
  onlineAdmins,
  onlineLoading,
  currentSessionId,
  selfId,
  autoRefresh
}: {
  onlineAdmins: OnlineAdmin[];
  onlineLoading: boolean;
  currentSessionId?: string;
  /** 自己不出现「强制下线」：那等于把当前会话一起踢掉，应走退出登录 */
  selfId: number | null;
  autoRefresh: number | false;
}) {
  const [selected, setSelected] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState("");
  const [pending, setPending] = useState<Pending | null>(null);

  const accountsQuery = useAdminAccountsQuery();
  const allQuery = useAllSessionsQuery(page, PAGE_SIZE, { enabled: selected == null, refetchInterval: autoRefresh });
  const oneQuery = useAdminSessionsQuery(selected, { enabled: selected != null });
  const revokeMutation = useRevokeSessionMutation();
  const forceLogoutMutation = useForceLogoutMutation();

  const avatars = useMemo(
    () => new Map(flattenAdmins(accountsQuery.data).map((a) => [a.id, a])),
    [accountsQuery.data]
  );

  const sessions = useMemo(() => {
    const source = selected != null ? oneQuery.data ?? [] : allQuery.data?.items ?? [];
    const q = keyword.trim().toLowerCase();
    return source
      .filter((s) => !s.isRevoked)
      .filter((s) => {
        if (!q) return true;
        return [s.ip, s.adminAccount, s.adminName, s.device, parseClient(s.userAgent).label, s.id]
          .some((v) => v?.toLowerCase().includes(q));
      })
      .sort((a, b) => new Date(b.lastActiveAt).getTime() - new Date(a.lastActiveAt).getTime());
  }, [selected, oneQuery.data, allQuery.data, keyword]);

  const loading = selected != null ? oneQuery.isLoading : allQuery.isLoading;
  const total = selected != null ? sessions.length : allQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil((allQuery.data?.total ?? 0) / PAGE_SIZE));
  const totalSessions = onlineAdmins.reduce((sum, a) => sum + a.sessionCount, 0);

  async function confirmPending() {
    if (!pending) return;
    try {
      if (pending.kind === "session") {
        await revokeMutation.mutateAsync(pending.session.id);
        toast.success("会话已撤销");
      } else {
        const result = await forceLogoutMutation.mutateAsync(pending.admin.adminId);
        toast.success(`已撤销 ${result?.revokedCount ?? pending.admin.sessionCount} 个会话`);
        if (selected === pending.admin.adminId) setSelected(null);
      }
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  const pendingName =
    pending?.kind === "admin"
      ? pending.admin.displayName || pending.admin.account
      : pending?.session.adminName || pending?.session.adminAccount || "";

  return (
    <div className="space-y-3">
      {/* 在线管理员 */}
      <div className="rounded-2xl border bg-card">
        <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <ShieldCheck className="size-4 text-muted-foreground" />
            在线管理员
          </h3>
          <span className="text-xs text-muted-foreground tabular-nums">
            {onlineLoading ? "" : `${onlineAdmins.length} 位，${totalSessions} 个会话`}
          </span>
        </div>
        {onlineLoading ? (
          <div className="flex gap-2 overflow-hidden p-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[60px] w-52 shrink-0 rounded-xl" />
            ))}
          </div>
        ) : onlineAdmins.length === 0 ? (
          <p className="px-4 py-5 text-sm text-muted-foreground">当前没有在线的管理员</p>
        ) : (
          <div className="flex gap-2 overflow-x-auto p-3 [scrollbar-width:thin]">
            {onlineAdmins.map((a) => {
              const active = selected === a.adminId;
              const account = avatars.get(a.adminId);
              return (
                <div
                  key={a.adminId}
                  className={cn(
                    "flex w-56 shrink-0 items-center gap-2 rounded-xl border p-2 pr-1.5 transition-colors",
                    active ? "border-foreground/30 bg-muted" : "hover:bg-muted/50"
                  )}
                >
                  <button
                    type="button"
                    aria-pressed={active}
                    className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
                    onClick={() => { setSelected(active ? null : a.adminId); setKeyword(""); }}
                  >
                    <span className="relative inline-flex shrink-0">
                      <Avatar className="size-9 rounded-xl border">
                        <AvatarImage src={account?.avatar} alt={a.displayName || a.account} />
                        <AvatarFallback className="rounded-xl text-[11px] font-medium">
                          {adminInitials({ displayName: a.displayName, account: a.account })}
                        </AvatarFallback>
                      </Avatar>
                      <span className="absolute -right-0.5 -bottom-0.5 size-3 rounded-full border-2 border-background bg-emerald-500" />
                    </span>
                    <span className="min-w-0">
                      <span className="block truncate text-sm font-medium">{a.displayName || a.account}</span>
                      <span className="block truncate text-[11px] text-muted-foreground" title={formatTime(a.lastActiveAt)}>
                        {a.sessionCount} 个会话，{relativeTime(a.lastActiveAt)}活跃
                      </span>
                    </span>
                  </button>
                  {a.adminId === selfId ? (
                    <span className="shrink-0 px-1.5 text-[11px] text-muted-foreground">本人</span>
                  ) : (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
                      aria-label={`强制 ${a.displayName || a.account} 下线`}
                      title="强制下线"
                      onClick={() => setPending({ kind: "admin", admin: a })}
                    >
                      <LogOut className="size-3.5" />
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 会话列表 */}
      <div className="rounded-2xl border bg-card">
        <div className="flex flex-col gap-2 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="truncate text-sm font-semibold">
              {selected != null
                ? `${onlineAdmins.find((a) => a.adminId === selected)?.displayName || "所选管理员"} 的会话`
                : "全部有效会话"}
            </h3>
            {selected != null ? (
              <Button variant="ghost" size="sm" className="h-7 gap-1 px-2 text-xs" onClick={() => setSelected(null)}>
                <X className="size-3" />
                查看全部
              </Button>
            ) : null}
          </div>
          <div className="relative w-full sm:w-64">
            <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索管理员、IP 或客户端"
              className="h-8 pl-8 text-sm"
            />
          </div>
        </div>

        {loading ? (
          <div className="divide-y px-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex gap-3 py-3">
                <Skeleton className="size-9 rounded-xl" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-48" />
                  <Skeleton className="h-3 w-64 max-w-full" />
                  <Skeleton className="h-3 w-28" />
                </div>
              </div>
            ))}
          </div>
        ) : sessions.length === 0 ? (
          <p className="px-4 py-10 text-center text-sm text-muted-foreground">
            {keyword ? "没有匹配的会话" : "暂无有效会话"}
          </p>
        ) : (
          <div className="divide-y px-4">
            {sessions.map((s) => (
              <AdminSessionRow
                key={s.id}
                session={s}
                showAdmin
                current={s.id === currentSessionId}
                revoking={revokeMutation.isPending && pending?.kind === "session" && pending.session.id === s.id}
                onRevoke={(session) => setPending({ kind: "session", session })}
              />
            ))}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 border-t px-4 py-2.5 text-xs text-muted-foreground">
          <span className="tabular-nums">
            {loading ? "" : keyword ? `匹配 ${sessions.length} 个` : `共 ${total.toLocaleString("zh-CN")} 个有效会话`}
          </span>
          {selected == null && totalPages > 1 ? (
            <div className="flex items-center gap-1.5">
              <Button variant="outline" size="icon" className="size-7" aria-label="上一页" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="size-3.5" />
              </Button>
              <span className="min-w-14 text-center tabular-nums">{page} / {totalPages}</span>
              <Button variant="outline" size="icon" className="size-7" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                <ChevronRight className="size-3.5" />
              </Button>
            </div>
          ) : null}
        </div>
      </div>

      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.kind === "admin" ? `强制 ${pendingName} 下线？` : "撤销该登录会话？"}
        description={
          pending?.kind === "admin"
            ? `将撤销其全部 ${pending.admin.sessionCount} 个登录会话，对方需要重新登录。`
            : `${pendingName ? `${pendingName} ` : ""}来自 ${pending?.session.ip || "未知 IP"} 的会话将立即失效，对方需要重新登录。`
        }
        confirmLabel={pending?.kind === "admin" ? "强制下线" : "撤销会话"}
        pending={revokeMutation.isPending || forceLogoutMutation.isPending}
        onConfirm={confirmPending}
      />
    </div>
  );
}
