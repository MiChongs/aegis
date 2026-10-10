"use client";

import {
  Ban,
  CheckCircle2,
  Eye,
  KeyRound,
  LogOut,
  MoreHorizontal,
  SearchX,
  ShieldAlert
} from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDate, formatTime, relativeTime } from "@/components/users/detail/user-detail-shared";
import type { OnlineAdmin, RoleDefinition } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  adminInitials,
  adminName,
  authSourceLabel,
  describeAssignments,
  isActive,
  isLoginBroken,
  loginBrokenReason,
  type AdminRecord
} from "./admin-shared";

export type AdminAction = "view" | "edit-access" | "toggle-status" | "force-logout";

type ListProps = {
  admins: AdminRecord[];
  loading: boolean;
  roles: Map<string, RoleDefinition>;
  online: Map<number, OnlineAdmin>;
  selfId: number | null;
  /** 会话类信息（在线、强制下线、认证健康）只对超管可见 */
  showSessions: boolean;
  filtered: boolean;
  onAction: (action: AdminAction, admin: AdminRecord) => void;
};

/**
 * 管理员列表：md 以上是表格，以下是卡片。
 *
 * 两种形态读的是同一组小部件（头像、角色、状态、操作菜单），
 * 所以手机上看到的信息与桌面一致，只是排布不同，不会有字段在小屏上「消失」。
 */
export function AdminList(props: ListProps) {
  const { admins, loading, filtered } = props;

  if (loading) return <AdminListSkeleton />;

  if (admins.length === 0) {
    return (
      <div className="flex min-h-48 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed px-6 text-center">
        <SearchX className="size-5 text-muted-foreground" />
        <p className="text-sm font-medium">{filtered ? "没有符合条件的管理员" : "暂无管理员"}</p>
        {filtered ? <p className="text-xs text-muted-foreground">调整搜索词或筛选条件后重试</p> : null}
      </div>
    );
  }

  return (
    <TooltipProvider delayDuration={120}>
      <AdminTable {...props} />
      <AdminCards {...props} />
    </TooltipProvider>
  );
}

// ── 桌面表格 ──────────────────────────

function AdminTable({ admins, roles, online, selfId, showSessions, onAction }: ListProps) {
  return (
    <div className="hidden overflow-hidden rounded-2xl border md:block">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/30 hover:bg-muted/30">
            <TableHead className="h-9 pl-4 text-xs">管理员</TableHead>
            <TableHead className="h-9 text-xs">角色与范围</TableHead>
            <TableHead className="h-9 text-xs">状态</TableHead>
            {showSessions ? <TableHead className="h-9 text-xs">在线</TableHead> : null}
            <TableHead className="h-9 text-xs">最近登录</TableHead>
            <TableHead className="hidden h-9 text-xs xl:table-cell">创建时间</TableHead>
            <TableHead className="h-9 w-12 pr-3" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {admins.map((admin) => (
            <TableRow
              key={admin.id}
              className="cursor-pointer"
              onClick={() => onAction("view", admin)}
            >
              <TableCell className="py-3 pl-4">
                <AdminIdentity admin={admin} online={showSessions && online.has(admin.id)} self={admin.id === selfId} />
              </TableCell>
              <TableCell className="max-w-72 py-3">
                <RoleChips admin={admin} roles={roles} max={2} />
              </TableCell>
              <TableCell className="py-3">
                <StatusCell admin={admin} showHealth={showSessions} />
              </TableCell>
              {showSessions ? (
                <TableCell className="py-3">
                  <OnlineCell entry={online.get(admin.id)} />
                </TableCell>
              ) : null}
              <TableCell className="py-3">
                <LastLogin value={admin.lastLoginAt} />
              </TableCell>
              <TableCell className="hidden py-3 text-xs text-muted-foreground tabular-nums xl:table-cell">
                {formatDate(admin.createdAt)}
              </TableCell>
              <TableCell className="py-3 pr-3 text-right" onClick={(e) => e.stopPropagation()}>
                <AdminActionsMenu
                  admin={admin}
                  self={admin.id === selfId}
                  online={online.has(admin.id)}
                  showSessions={showSessions}
                  onAction={onAction}
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

// ── 移动端卡片 ──────────────────────────

function AdminCards({ admins, roles, online, selfId, showSessions, onAction }: ListProps) {
  return (
    <ul className="space-y-2.5 md:hidden">
      {admins.map((admin) => {
        const entry = online.get(admin.id);
        return (
          <li key={admin.id} className="rounded-2xl border bg-card">
            <div className="flex items-start gap-3 p-3.5">
              <button
                type="button"
                className="min-w-0 flex-1 text-left"
                onClick={() => onAction("view", admin)}
              >
                <AdminIdentity admin={admin} online={showSessions && Boolean(entry)} self={admin.id === selfId} />
              </button>
              <AdminActionsMenu
                admin={admin}
                self={admin.id === selfId}
                online={Boolean(entry)}
                showSessions={showSessions}
                onAction={onAction}
              />
            </div>
            <button
              type="button"
              className="block w-full space-y-3 border-t px-3.5 py-3 text-left"
              onClick={() => onAction("view", admin)}
            >
              <RoleChips admin={admin} roles={roles} max={3} />
              <dl className="grid grid-cols-2 gap-x-4 gap-y-2.5 text-xs">
                <div className="space-y-0.5">
                  <dt className="text-muted-foreground">状态</dt>
                  <dd><StatusCell admin={admin} showHealth={showSessions} /></dd>
                </div>
                <div className="space-y-0.5">
                  <dt className="text-muted-foreground">登录方式</dt>
                  <dd>{authSourceLabel(admin)}</dd>
                </div>
                <div className="space-y-0.5">
                  <dt className="text-muted-foreground">最近登录</dt>
                  <dd><LastLogin value={admin.lastLoginAt} /></dd>
                </div>
                {showSessions ? (
                  <div className="space-y-0.5">
                    <dt className="text-muted-foreground">在线</dt>
                    <dd><OnlineCell entry={entry} /></dd>
                  </div>
                ) : (
                  <div className="space-y-0.5">
                    <dt className="text-muted-foreground">创建时间</dt>
                    <dd className="tabular-nums">{formatDate(admin.createdAt)}</dd>
                  </div>
                )}
              </dl>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

// ── 共用小部件 ──────────────────────────

export function AdminAvatar({
  admin,
  online,
  className
}: {
  admin: AdminRecord;
  online?: boolean;
  className?: string;
}) {
  return (
    <span className="relative inline-flex shrink-0">
      <Avatar className={cn("size-9 rounded-xl border", className)}>
        <AvatarImage src={admin.avatar} alt={adminName(admin)} />
        <AvatarFallback className="rounded-xl text-[11px] font-medium">{adminInitials(admin)}</AvatarFallback>
      </Avatar>
      {online ? (
        <span
          className="absolute -right-0.5 -bottom-0.5 size-3 rounded-full border-2 border-background bg-emerald-500"
          aria-label="在线"
        />
      ) : null}
    </span>
  );
}

function AdminIdentity({ admin, online, self }: { admin: AdminRecord; online: boolean; self: boolean }) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <AdminAvatar admin={admin} online={online} />
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-sm font-medium">{adminName(admin)}</span>
          {self ? <Badge variant="outline" size="sm">本人</Badge> : null}
        </div>
        <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span className="truncate font-mono">{admin.account}</span>
          <span className="hidden shrink-0 rounded border px-1 text-[10px] leading-4 lg:inline">{authSourceLabel(admin)}</span>
        </div>
        {admin.email ? <div className="truncate text-xs text-muted-foreground">{admin.email}</div> : null}
      </div>
    </div>
  );
}

export function RoleChips({
  admin,
  roles,
  max
}: {
  admin: AdminRecord;
  roles: Map<string, RoleDefinition>;
  max: number;
}) {
  if (admin.isSuperAdmin) {
    return (
      <Badge variant="info" size="sm" className="gap-1">
        <KeyRound />
        超级管理员
      </Badge>
    );
  }
  const items = describeAssignments(admin.assignments, roles);
  if (items.length === 0) {
    return <span className="text-xs text-amber-600 dark:text-amber-400">未分配角色</span>;
  }
  const visible = items.slice(0, max);
  const rest = items.slice(max);
  return (
    <div className="flex flex-wrap items-center gap-1">
      {visible.map((item) => (
        <span
          key={item.key}
          className="inline-flex max-w-full items-center overflow-hidden rounded-md border text-[11px] leading-5"
        >
          <span className="truncate px-1.5 font-medium">{item.roleName}</span>
          <span className="truncate border-l bg-muted/50 px-1.5 text-muted-foreground">{item.scope}</span>
        </span>
      ))}
      {rest.length > 0 ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="cursor-default rounded-md border px-1.5 text-[11px] leading-5 text-muted-foreground">
              +{rest.length}
            </span>
          </TooltipTrigger>
          <TooltipContent side="top" className="text-xs">
            {rest.map((item) => (
              <div key={item.key}>
                {item.roleName}（{item.scope}）
              </div>
            ))}
          </TooltipContent>
        </Tooltip>
      ) : null}
    </div>
  );
}

function StatusCell({ admin, showHealth }: { admin: AdminRecord; showHealth: boolean }) {
  const active = isActive(admin);
  const broken = showHealth && isLoginBroken(admin);
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="inline-flex items-center gap-1.5 text-xs">
        <span className={cn("size-1.5 rounded-full", active ? "bg-emerald-500" : "bg-zinc-400")} />
        {active ? "正常" : "已停用"}
      </span>
      {broken ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="danger" size="sm" className="cursor-help gap-1">
              <ShieldAlert />
              认证异常
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-sm text-xs leading-relaxed">
            <div className="font-medium">{admin.loginAvailability?.source.toUpperCase()} 认证源不可用，该账号可能无法登录</div>
            <div className="mt-1 opacity-80">{loginBrokenReason(admin)}</div>
          </TooltipContent>
        </Tooltip>
      ) : null}
    </div>
  );
}

function OnlineCell({ entry }: { entry?: OnlineAdmin }) {
  if (!entry) return <span className="text-xs text-muted-foreground">离线</span>;
  return (
    <span className="inline-flex flex-col text-xs">
      <span className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
        <span className="size-1.5 rounded-full bg-emerald-500" />
        {entry.sessionCount} 个会话
      </span>
      <span className="text-[11px] text-muted-foreground" title={formatTime(entry.lastActiveAt)}>
        {relativeTime(entry.lastActiveAt)}活跃
      </span>
    </span>
  );
}

function LastLogin({ value }: { value?: string | null }) {
  if (!value) return <span className="text-xs text-muted-foreground">从未登录</span>;
  return (
    <span className="inline-flex flex-col text-xs">
      <span>{relativeTime(value)}</span>
      <span className="text-[11px] text-muted-foreground tabular-nums">{formatTime(value)}</span>
    </span>
  );
}

export function AdminActionsMenu({
  admin,
  self,
  online,
  showSessions,
  onAction
}: {
  admin: AdminRecord;
  self: boolean;
  online: boolean;
  showSessions: boolean;
  onAction: (action: AdminAction, admin: AdminRecord) => void;
}) {
  const active = isActive(admin);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-8" aria-label="更多操作">
          <MoreHorizontal className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuItem onClick={() => onAction("view", admin)}>
          <Eye />
          查看详情
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onAction("edit-access", admin)}>
          <KeyRound />
          编辑权限
        </DropdownMenuItem>
        {!self ? (
          <>
            <DropdownMenuSeparator />
            {showSessions && online ? (
              <DropdownMenuItem onClick={() => onAction("force-logout", admin)}>
                <LogOut />
                强制下线
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuItem
              variant={active ? "destructive" : "default"}
              onClick={() => onAction("toggle-status", admin)}
            >
              {active ? <Ban /> : <CheckCircle2 />}
              {active ? "停用账号" : "启用账号"}
            </DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AdminListSkeleton() {
  return (
    <>
      <div className="hidden overflow-hidden rounded-2xl border md:block">
        <div className="h-9 border-b bg-muted/30" />
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="flex items-center gap-6 border-b px-4 py-3 last:border-b-0">
            <div className="flex w-56 items-center gap-3">
              <Skeleton className="size-9 rounded-xl" />
              <div className="space-y-1.5">
                <Skeleton className="h-3.5 w-24" />
                <Skeleton className="h-3 w-32" />
              </div>
            </div>
            <Skeleton className="h-5 w-40 rounded-md" />
            <Skeleton className="h-3.5 w-12" />
            <Skeleton className="h-8 w-20" />
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
                <Skeleton className="h-3 w-36" />
              </div>
            </div>
            <Skeleton className="h-5 w-32 rounded-md" />
            <div className="grid grid-cols-2 gap-3">
              <Skeleton className="h-8" />
              <Skeleton className="h-8" />
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
