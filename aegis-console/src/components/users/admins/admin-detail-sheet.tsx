"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import {
  ArrowUpRight,
  Ban,
  CheckCircle2,
  CircleAlert,
  Clock3,
  History,
  KeyRound,
  LogOut,
  MonitorSmartphone,
  ShieldAlert,
  UserRound
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import {
  EMPTY,
  Fact,
  Facts,
  formatDate,
  formatShortTime,
  formatTime,
  relativeTime
} from "@/components/users/detail/user-detail-shared";
import { AdminSessionRow } from "@/components/users/online/admin-session-row";
import { ConfirmActionDialog } from "@/components/users/online/session-shared";
import { ApiError } from "@/lib/api-client";
import type { AdminSessionRecord, OnlineAdmin, RoleDefinition } from "@/lib/api/types";
import { useAdminSessionsQuery, useRevokeSessionMutation } from "@/lib/admin-hooks";
import { useAuditLogsInfiniteQuery } from "@/lib/audit-hooks";
import { cn } from "@/lib/utils";
import { AdminAvatar, type AdminAction } from "./admin-list";
import {
  adminName,
  authSource,
  authSourceLabel,
  describeAssignments,
  isActive,
  isLoginBroken,
  loginBrokenReason,
  type AdminRecord
} from "./admin-shared";

const CONTACT_LABEL: Record<string, string> = {
  wechat: "微信",
  qq: "QQ",
  telegram: "Telegram",
  discord: "Discord",
  twitter: "X",
  github: "GitHub",
  phone: "电话",
  email: "邮箱",
  other: "其他"
};

export function AdminDetailSheet({
  admin,
  open,
  onOpenChange,
  roles,
  onlineEntry,
  self,
  superViewer,
  currentSessionId,
  onAction
}: {
  admin: AdminRecord | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  roles: Map<string, RoleDefinition>;
  onlineEntry?: OnlineAdmin;
  self: boolean;
  /** 会话、认证健康、审计都是超管专属接口 */
  superViewer: boolean;
  currentSessionId?: string;
  onAction: (action: AdminAction, admin: AdminRecord) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full gap-0 overflow-y-auto p-0 sm:max-w-xl">
        {admin ? (
          <AdminDetailBody
            admin={admin}
            roles={roles}
            onlineEntry={onlineEntry}
            self={self}
            superViewer={superViewer}
            currentSessionId={currentSessionId}
            onAction={onAction}
          />
        ) : (
          <SheetHeader className="sr-only">
            <SheetTitle>管理员详情</SheetTitle>
          </SheetHeader>
        )}
      </SheetContent>
    </Sheet>
  );
}

function AdminDetailBody({
  admin,
  roles,
  onlineEntry,
  self,
  superViewer,
  currentSessionId,
  onAction
}: {
  admin: AdminRecord;
  roles: Map<string, RoleDefinition>;
  onlineEntry?: OnlineAdmin;
  self: boolean;
  superViewer: boolean;
  currentSessionId?: string;
  onAction: (action: AdminAction, admin: AdminRecord) => void;
}) {
  const active = isActive(admin);
  const broken = superViewer && isLoginBroken(admin);
  const assignments = describeAssignments(admin.assignments, roles);
  const local = authSource(admin) === "password";
  const contacts = (admin.contacts ?? []).filter((c) => c.value);

  return (
    <>
      {/* 头部：身份 + 主要操作 */}
      <SheetHeader className="gap-4 border-b p-5 pr-12">
        <div className="flex items-start gap-3.5">
          <AdminAvatar admin={admin} online={superViewer && Boolean(onlineEntry)} className="size-14 rounded-2xl" />
          <div className="min-w-0 flex-1 space-y-1">
            <SheetTitle className="truncate text-base">{adminName(admin)}</SheetTitle>
            <SheetDescription className="truncate font-mono text-xs">
              {admin.account}
              <span className="ml-2 font-sans">ID {admin.id}</span>
            </SheetDescription>
            <div className="flex flex-wrap gap-1.5 pt-1">
              <Badge variant={active ? "success" : "outline"} size="sm">{active ? "正常" : "已停用"}</Badge>
              {admin.isSuperAdmin ? <Badge variant="info" size="sm">超级管理员</Badge> : <Badge variant="outline" size="sm">管理员</Badge>}
              <Badge variant="outline" size="sm">{authSourceLabel(admin)}</Badge>
              {self ? <Badge variant="secondary" size="sm">本人</Badge> : null}
              {superViewer && onlineEntry ? <Badge variant="success" size="sm">在线</Badge> : null}
            </div>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => onAction("edit-access", admin)}>
            <KeyRound className="size-3.5" />
            编辑权限
          </Button>
          {!self ? (
            <>
              {superViewer && onlineEntry ? (
                <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => onAction("force-logout", admin)}>
                  <LogOut className="size-3.5" />
                  强制下线
                </Button>
              ) : null}
              <Button
                size="sm"
                variant="outline"
                className={cn("h-8 gap-1.5", active && "text-destructive hover:text-destructive")}
                onClick={() => onAction("toggle-status", admin)}
              >
                {active ? <Ban className="size-3.5" /> : <CheckCircle2 className="size-3.5" />}
                {active ? "停用账号" : "启用账号"}
              </Button>
            </>
          ) : null}
        </div>
      </SheetHeader>

      <div className="space-y-6 p-5">
        {broken ? (
          <div className="flex gap-2.5 rounded-xl border border-red-200 bg-red-50/60 p-3 text-sm dark:border-red-900/60 dark:bg-red-950/30">
            <ShieldAlert className="mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400" />
            <div className="min-w-0 space-y-0.5">
              <p className="font-medium text-red-700 dark:text-red-300">该账号可能无法登录</p>
              <p className="text-xs break-all text-red-700/80 dark:text-red-300/80">{loginBrokenReason(admin)}</p>
              {admin.loginAvailability?.checkedAt ? (
                <p className="text-[11px] text-red-700/60 dark:text-red-300/60">探测于 {formatTime(admin.loginAvailability.checkedAt)}</p>
              ) : null}
            </div>
          </div>
        ) : null}

        {!active ? (
          <div className="flex gap-2.5 rounded-xl border bg-muted/40 p-3 text-sm">
            <CircleAlert className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <p className="text-muted-foreground">账号已停用，无法登录控制台。启用后恢复原有角色与权限。</p>
          </div>
        ) : null}

        {/* 关键指标 */}
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <MiniStat label="最近登录" value={admin.lastLoginAt ? relativeTime(admin.lastLoginAt) : "从未登录"} hint={formatShortTime(admin.lastLoginAt)} />
          <MiniStat
            label="活跃会话"
            value={superViewer ? String(onlineEntry?.sessionCount ?? 0) : EMPTY}
            hint={superViewer ? (onlineEntry ? `${relativeTime(onlineEntry.lastActiveAt)}活跃` : "当前离线") : "仅超级管理员可见"}
          />
          <MiniStat label="角色分配" value={admin.isSuperAdmin ? "全部" : String(assignments.length)} hint={admin.isSuperAdmin ? "超级管理员" : "条授权"} />
          <MiniStat label="加入时间" value={formatDate(admin.createdAt)} hint={admin.createdAt ? relativeTime(admin.createdAt) : undefined} />
        </div>

        {/* 角色与范围 */}
        <Section icon={<KeyRound className="size-4" />} title="角色与授权范围">
          {admin.isSuperAdmin ? (
            <p className="rounded-xl border bg-muted/30 px-3 py-2.5 text-sm text-muted-foreground">
              超级管理员拥有平台全部权限，不受角色分配约束。
            </p>
          ) : assignments.length === 0 ? (
            <p className="rounded-xl border border-amber-200 bg-amber-50/60 px-3 py-2.5 text-sm text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
              尚未分配角色，该账号登录后无任何可用功能。
            </p>
          ) : (
            <ul className="divide-y rounded-xl border">
              {assignments.map((item) => {
                const role = roles.get(item.roleKey);
                return (
                  <li key={item.key} className="flex items-start justify-between gap-3 px-3 py-2.5">
                    <div className="min-w-0">
                      <div className="text-sm font-medium">{item.roleName}</div>
                      {role?.description ? <div className="text-xs text-muted-foreground">{role.description}</div> : null}
                      <div className="font-mono text-[11px] text-muted-foreground/80">{item.roleKey}</div>
                    </div>
                    <Badge variant={item.global ? "secondary" : "outline"} size="sm" className="shrink-0">
                      {item.scope}
                    </Badge>
                  </li>
                );
              })}
            </ul>
          )}
        </Section>

        {/* 账号资料 */}
        <Section icon={<UserRound className="size-4" />} title="账号资料">
          <Facts>
            <Fact label="用户名" value={admin.account} mono />
            <Fact label="显示名称" value={admin.displayName} />
            <Fact label="邮箱" value={admin.email} />
            <Fact label="手机号" value={admin.phone} mono />
            <Fact label="生日" value={formatDate(admin.birthday)} />
            <Fact label="登录方式" value={authSourceLabel(admin)} hint={local ? undefined : "由外部身份源管理密码"} />
            {admin.previousAccount ? (
              <Fact label="曾用名" value={admin.previousAccount} mono hint={`改名于 ${formatTime(admin.accountChangedAt)}`} />
            ) : null}
            {local ? <Fact label="密码更新" value={formatTime(admin.passwordChangedAt)} hint={admin.passwordChangedAt ? relativeTime(admin.passwordChangedAt) : "创建后未修改"} /> : null}
            <Fact label="创建时间" value={formatTime(admin.createdAt)} />
            <Fact label="资料更新" value={formatTime(admin.updatedAt)} />
          </Facts>
        </Section>

        {contacts.length > 0 || admin.bio ? (
          <Section icon={<UserRound className="size-4" />} title="联系方式与简介">
            {contacts.length > 0 ? (
              <Facts>
                {contacts.map((c, i) => (
                  <Fact key={`${c.platform}-${i}`} label={c.label || CONTACT_LABEL[c.platform] || c.platform} value={c.value} />
                ))}
              </Facts>
            ) : null}
            {admin.bio ? <p className="mt-3 text-sm leading-6 whitespace-pre-wrap text-muted-foreground">{admin.bio}</p> : null}
          </Section>
        ) : null}

        {superViewer ? (
          <>
            <AdminSessionsSection admin={admin} currentSessionId={currentSessionId} />
            <AdminRecentAudits adminId={admin.id} />
          </>
        ) : null}
      </div>
    </>
  );
}

function MiniStat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="min-w-0 rounded-xl border px-3 py-2.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-sm font-semibold tabular-nums">{value}</div>
      {hint ? <div className="truncate text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

function Section({
  icon,
  title,
  action,
  children
}: {
  icon: ReactNode;
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-2.5">
      <div className="flex items-center justify-between gap-3">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <span className="text-muted-foreground">{icon}</span>
          {title}
        </h3>
        {action}
      </div>
      {children}
    </section>
  );
}

function AdminSessionsSection({ admin, currentSessionId }: { admin: AdminRecord; currentSessionId?: string }) {
  const query = useAdminSessionsQuery(admin.id);
  const revokeMutation = useRevokeSessionMutation();
  const [pending, setPending] = useState<AdminSessionRecord | null>(null);
  const sessions = (query.data ?? []).filter((s) => !s.isRevoked);

  async function confirmRevoke() {
    if (!pending) return;
    try {
      await revokeMutation.mutateAsync(pending.id);
      toast.success("会话已撤销");
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "撤销失败");
    }
  }

  return (
    <Section
      icon={<MonitorSmartphone className="size-4" />}
      title="登录会话"
      action={<span className="text-xs text-muted-foreground tabular-nums">{query.isLoading ? "" : `${sessions.length} 个有效`}</span>}
    >
      {query.isLoading ? (
        <div className="divide-y rounded-xl border px-3">
          {Array.from({ length: 2 }).map((_, i) => (
            <div key={i} className="flex gap-3 py-3">
              <Skeleton className="size-9 rounded-xl" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3.5 w-40" />
                <Skeleton className="h-3 w-56" />
                <Skeleton className="h-3 w-24" />
              </div>
            </div>
          ))}
        </div>
      ) : sessions.length === 0 ? (
        <p className="rounded-xl border border-dashed px-3 py-6 text-center text-sm text-muted-foreground">暂无有效会话</p>
      ) : (
        <div className="divide-y rounded-xl border px-3">
          {sessions.map((s) => (
            <AdminSessionRow
              key={s.id}
              session={s}
              current={s.id === currentSessionId}
              revoking={revokeMutation.isPending && pending?.id === s.id}
              onRevoke={setPending}
            />
          ))}
        </div>
      )}

      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title="撤销该登录会话？"
        description={`来自 ${pending?.ip || "未知 IP"} 的会话将立即失效，对方需要重新登录。`}
        confirmLabel="撤销会话"
        pending={revokeMutation.isPending}
        onConfirm={confirmRevoke}
      />
    </Section>
  );
}

function AdminRecentAudits({ adminId }: { adminId: number }) {
  const query = useAuditLogsInfiniteQuery({ adminId: String(adminId), kind: "operation" });
  const items = (query.data?.pages[0]?.items ?? []).slice(0, 6);
  const total = query.data?.pages[0]?.total ?? 0;

  return (
    <Section
      icon={<History className="size-4" />}
      title="最近操作"
      action={
        <Link
          href={`/audit?admin=${adminId}`}
          className="inline-flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground"
        >
          全部 {total > 0 ? total.toLocaleString("zh-CN") : ""}
          <ArrowUpRight className="size-3" />
        </Link>
      }
    >
      {query.isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-11 w-full rounded-lg" />
          ))}
        </div>
      ) : query.isError ? (
        <p className="rounded-xl border border-dashed px-3 py-6 text-center text-sm text-muted-foreground">审计记录暂不可用</p>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed px-3 py-6 text-center text-sm text-muted-foreground">暂无操作记录</p>
      ) : (
        <ol className="relative space-y-3 border-l pl-4">
          {items.map((log) => {
            const failed = log.status !== "success";
            return (
              <li key={log.id} className="relative">
                <span
                  className={cn(
                    "absolute top-1.5 -left-[21px] size-2 rounded-full ring-4 ring-background",
                    failed ? "bg-red-500" : "bg-muted-foreground/40"
                  )}
                />
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm">
                      {log.operationName || log.action}
                      {failed ? <span className="ml-1.5 text-xs text-red-600 dark:text-red-400">失败</span> : null}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {[log.moduleLabel, log.targetName || log.targetLabel, log.appName].filter(Boolean).join(" / ") || EMPTY}
                    </div>
                  </div>
                  <span className="flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground" title={formatTime(log.createdAt)}>
                    <Clock3 className="size-3" />
                    {relativeTime(log.createdAt)}
                  </span>
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </Section>
  );
}
