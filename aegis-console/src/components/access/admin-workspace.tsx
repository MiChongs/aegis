"use client";

import { useMemo, useState } from "react";
import {
  CircleOff,
  Crown,
  KeyRound,
  LogOut,
  MoreHorizontal,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  UserCog,
  UserRoundX,
  Users2,
  Wifi,
} from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useOnlineAdminsQuery } from "@/lib/admin-hooks";
import {
  useAccessAdminsQuery,
  useAccessRolesQuery,
  useForceLogoutAdminMutation,
  useUpdateAdminStatusMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import {
  TIER_LABEL,
  tierOf,
  type AccessRole,
  type AdminListEntry,
  type AdminTier,
} from "@/lib/api/access";
import { formatDateTimeMinute } from "@/lib/datetime";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { AdminIdentity, SegmentBar, TIER_TONE, TierBadge } from "./access-kit";
import { AdminCreateDialog } from "./admin-create-dialog";
import { AdminDetailSheet, type AdminSheetTab } from "./admin-detail-sheet";

type Scope = "all" | AdminTier | "disabled" | "unassigned" | "online";

const SOURCE_LABEL: Record<string, string> = {
  password: "本地账号",
  ldap: "LDAP",
  oidc: "OIDC",
  saml: "SAML",
};

export function AdminWorkspace() {
  const can = usePermissionChecker();
  const adminsQuery = useAccessAdminsQuery();
  const rolesQuery = useAccessRolesQuery(
    can("system:role:read") || can("system:admin:read"),
  );
  const onlineQuery = useOnlineAdminsQuery({
    enabled: can("system:admin:read"),
  });
  const statusMutation = useUpdateAdminStatusMutation();
  const logoutMutation = useForceLogoutAdminMutation();

  const [scope, setScope] = useState<Scope>("all");
  const [keyword, setKeyword] = useState("");
  const [roleFilter, setRoleFilter] = useState("all");
  const [source, setSource] = useState("all");
  const [sheet, setSheet] = useState<{ id: number; tab: AdminSheetTab } | null>(
    null,
  );
  const [creating, setCreating] = useState(false);
  const [pending, setPending] = useState<{
    kind: "disable" | "logout";
    entry: AdminListEntry;
  } | null>(null);

  const admins = useMemo(() => adminsQuery.data ?? [], [adminsQuery.data]);
  const roles = useMemo(
    () =>
      new Map<string, AccessRole>(
        (rolesQuery.data ?? []).map((r) => [r.key, r]),
      ),
    [rolesQuery.data],
  );
  const online = useMemo(
    () => new Set((onlineQuery.data ?? []).map((o) => o.adminId)),
    [onlineQuery.data],
  );

  const counts = useMemo(() => {
    const c = {
      all: admins.length,
      system: 0,
      super: 0,
      admin: 0,
      disabled: 0,
      unassigned: 0,
      online: 0,
    };
    for (const entry of admins) {
      const tier = tierOf(entry.account);
      c[tier]++;
      if (entry.account.status === "disabled") c.disabled++;
      if (tier === "admin" && (entry.assignments?.length ?? 0) === 0)
        c.unassigned++;
      if (online.has(entry.account.id)) c.online++;
    }
    return c;
  }, [admins, online]);

  const visible = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    return admins
      .filter((entry) => {
        const tier = tierOf(entry.account);
        if (scope === "system" || scope === "super" || scope === "admin") {
          if (tier !== scope) return false;
        } else if (scope === "disabled" && entry.account.status !== "disabled")
          return false;
        else if (
          scope === "unassigned" &&
          !(tier === "admin" && (entry.assignments?.length ?? 0) === 0)
        )
          return false;
        else if (scope === "online" && !online.has(entry.account.id))
          return false;
        if (
          roleFilter !== "all" &&
          !(entry.assignments ?? []).some((a) => a.roleKey === roleFilter)
        )
          return false;
        if (
          source !== "all" &&
          (entry.account.authSource || "password") !== source
        )
          return false;
        if (!k) return true;
        return (
          entry.account.account.toLowerCase().includes(k) ||
          (entry.account.displayName ?? "").toLowerCase().includes(k) ||
          (entry.account.email ?? "").toLowerCase().includes(k)
        );
      })
      .sort((a, b) => {
        const rank = { system: 0, super: 1, admin: 2 } as const;
        const ta = rank[tierOf(a.account)];
        const tb = rank[tierOf(b.account)];
        if (ta !== tb) return ta - tb;
        return (b.account.lastLoginAt ?? "").localeCompare(
          a.account.lastLoginAt ?? "",
        );
      });
  }, [admins, scope, keyword, roleFilter, source, online]);

  const toggle = async (entry: AdminListEntry, next: "active" | "disabled") => {
    try {
      await statusMutation.mutateAsync({
        adminId: entry.account.id,
        status: next,
      });
      toast.success(
        next === "active"
          ? `已启用 ${entry.account.displayName || entry.account.account}`
          : `已停用 ${entry.account.displayName || entry.account.account}`,
      );
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  const logout = async (entry: AdminListEntry) => {
    try {
      const result = await logoutMutation.mutateAsync(entry.account.id);
      toast.success(`已下线 ${result.revokedCount} 个会话`);
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Overview
        counts={counts}
        scope={scope}
        onScope={setScope}
        showOnline={can("system:admin:read")}
      />

      <section
        className="overflow-hidden rounded-2xl border border-border bg-card"
        style={{ boxShadow: "var(--shadow-soft)" }}
      >
        <div className="flex flex-wrap items-center gap-2 border-b border-border/70 px-3 py-2.5">
          <div className="relative min-w-52 flex-1 sm:max-w-xs">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索账号、名称或邮箱"
              className="h-8 pl-8 text-xs"
            />
          </div>
          <Select value={roleFilter} onValueChange={setRoleFilter}>
            <SelectTrigger size="sm" className="h-8 w-40 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部角色</SelectItem>
              {[...roles.values()]
                .filter((role) => role.key !== "super_admin")
                .map((role) => (
                  <SelectItem key={role.key} value={role.key}>
                    {role.name}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
          <Select value={source} onValueChange={setSource}>
            <SelectTrigger size="sm" className="h-8 w-32 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部登录方式</SelectItem>
              {Object.entries(SOURCE_LABEL).map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="ml-auto flex items-center gap-1.5">
            <span className="text-xs text-muted-foreground tabular-nums">
              {visible.length} 位
            </span>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8"
                  aria-label="刷新"
                  onClick={() => void adminsQuery.refetch()}
                >
                  <RefreshCw
                    className={cn(
                      "size-4",
                      adminsQuery.isFetching && "animate-spin",
                    )}
                  />
                </Button>
              </TooltipTrigger>
              <TooltipContent>刷新</TooltipContent>
            </Tooltip>
            {can("system:admin:create") && (
              <Button
                size="sm"
                className="h-8 gap-1.5"
                onClick={() => setCreating(true)}
              >
                <Plus className="size-3.5" />
                新增管理员
              </Button>
            )}
          </div>
        </div>

        <div className="hidden grid-cols-[minmax(13rem,1.6fr)_7.5rem_minmax(12rem,2fr)_6.5rem_8.5rem_4.5rem_2.5rem] items-center gap-3 border-b border-border/70 bg-muted/25 px-4 py-2 text-[11px] font-medium text-muted-foreground lg:grid">
          <span>管理员</span>
          <span>身份</span>
          <span>角色授权</span>
          <span>登录方式</span>
          <span>最近登录</span>
          <span>状态</span>
          <span />
        </div>

        <div className="divide-y divide-border/60">
          {adminsQuery.isLoading &&
            Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="size-8 rounded-full" />
                <Skeleton className="h-3 w-40" />
                <Skeleton className="ml-auto h-3 w-24" />
              </div>
            ))}
          {!adminsQuery.isLoading && visible.length === 0 && (
            <div className="py-14 text-center text-sm text-muted-foreground">
              暂无管理员
            </div>
          )}
          {visible.map((entry) => (
            <AdminRow
              key={entry.account.id}
              entry={entry}
              roles={roles}
              online={online.has(entry.account.id)}
              onOpen={(tab) => setSheet({ id: entry.account.id, tab })}
              onToggle={(next) =>
                next === "disabled"
                  ? setPending({ kind: "disable", entry })
                  : void toggle(entry, "active")
              }
              onLogout={() => setPending({ kind: "logout", entry })}
            />
          ))}
        </div>
      </section>

      <AdminDetailSheet
        adminId={sheet?.id ?? null}
        initialTab={sheet?.tab ?? "overview"}
        onClose={() => setSheet(null)}
      />
      <AdminCreateDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(id) => setSheet({ id, tab: "access" })}
      />

      <AlertDialog
        open={Boolean(pending)}
        onOpenChange={(open) => !open && setPending(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pending?.kind === "disable" ? "停用管理员" : "强制下线"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pending?.kind === "disable"
                ? `${pending.entry.account.displayName || pending.entry.account.account} 将立即无法登录，其会话与 MCP 密钥同时失效。`
                : `${pending?.entry.account.displayName || pending?.entry.account.account} 的全部会话将被撤销。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={(event) => {
                event.preventDefault();
                if (!pending) return;
                void (pending.kind === "disable"
                  ? toggle(pending.entry, "disabled")
                  : logout(pending.entry));
              }}
            >
              {pending?.kind === "disable" ? "停用" : "下线"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ── 汇总 ──

function Overview({
  counts,
  scope,
  onScope,
  showOnline,
}: {
  counts: Record<
    "all" | AdminTier | "disabled" | "unassigned" | "online",
    number
  >;
  scope: Scope;
  onScope: (scope: Scope) => void;
  showOnline: boolean;
}) {
  const tiles: Array<{
    key: Scope;
    label: string;
    value: number;
    icon: typeof Users2;
    tone: string;
  }> = [
    {
      key: "all",
      label: "全部",
      value: counts.all,
      icon: Users2,
      tone: "text-foreground",
    },
    {
      key: "system",
      label: TIER_LABEL.system,
      value: counts.system,
      icon: Crown,
      tone: TIER_TONE.system.text,
    },
    {
      key: "super",
      label: TIER_LABEL.super,
      value: counts.super,
      icon: ShieldCheck,
      tone: TIER_TONE.super.text,
    },
    {
      key: "admin",
      label: TIER_LABEL.admin,
      value: counts.admin,
      icon: UserCog,
      tone: "text-sky-600 dark:text-sky-400",
    },
    {
      key: "unassigned",
      label: "未授权",
      value: counts.unassigned,
      icon: CircleOff,
      tone: "text-muted-foreground",
    },
    {
      key: "disabled",
      label: "已停用",
      value: counts.disabled,
      icon: UserRoundX,
      tone: "text-rose-600 dark:text-rose-400",
    },
    ...(showOnline
      ? [
          {
            key: "online" as Scope,
            label: "在线",
            value: counts.online,
            icon: Wifi,
            tone: "text-emerald-600 dark:text-emerald-400",
          },
        ]
      : []),
  ];
  return (
    <section
      className="overflow-hidden rounded-2xl border border-border bg-card"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <div className="flex flex-wrap items-center gap-3 px-5 pt-4">
        <div className="text-sm font-semibold tracking-tight">身份分布</div>
        <div className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
          {(["system", "super", "admin"] as const).map((tier) => (
            <span key={tier} className="inline-flex items-center gap-1.5">
              <span
                className={cn("size-2 rounded-full", TIER_TONE[tier].bar)}
              />
              {TIER_LABEL[tier]}
            </span>
          ))}
        </div>
      </div>
      <div className="px-5 pt-3">
        <SegmentBar
          className="h-2.5"
          segments={[
            {
              value: counts.system,
              className: TIER_TONE.system.bar,
              label: TIER_LABEL.system,
            },
            {
              value: counts.super,
              className: TIER_TONE.super.bar,
              label: TIER_LABEL.super,
            },
            {
              value: counts.admin - counts.unassigned,
              className: TIER_TONE.admin.bar,
              label: "已授权管理员",
            },
            {
              value: counts.unassigned,
              className: "bg-sky-500/30",
              label: "未授权管理员",
            },
          ]}
        />
      </div>
      <div className="grid grid-cols-2 gap-px p-3 sm:grid-cols-4 xl:grid-cols-7">
        {tiles.map((tile) => {
          const Icon = tile.icon;
          const active = scope === tile.key;
          return (
            <button
              key={tile.key}
              type="button"
              onClick={() =>
                onScope(active && tile.key !== "all" ? "all" : tile.key)
              }
              className={cn(
                "flex items-center gap-3 rounded-xl px-3 py-2.5 text-left transition-colors",
                active
                  ? "bg-primary/8 ring-1 ring-primary/25"
                  : "hover:bg-accent/60",
              )}
            >
              <Icon className={cn("size-4 shrink-0", tile.tone)} />
              <div className="min-w-0">
                <div className="text-[11px] text-muted-foreground">
                  {tile.label}
                </div>
                <div className="text-lg leading-6 font-semibold tabular-nums">
                  {tile.value}
                </div>
              </div>
            </button>
          );
        })}
      </div>
    </section>
  );
}

// ── 行 ──

function AdminRow({
  entry,
  roles,
  online,
  onOpen,
  onToggle,
  onLogout,
}: {
  entry: AdminListEntry;
  roles: Map<string, AccessRole>;
  online: boolean;
  onOpen: (tab: AdminSheetTab) => void;
  onToggle: (next: "active" | "disabled") => void;
  onLogout: () => void;
}) {
  const account = entry.account;
  const tier = tierOf(account);
  const caps = entry.capabilities;
  const active = account.status !== "disabled";
  const assignments = entry.assignments ?? [];
  const sourceKey = (account.authSource || "password").toLowerCase();
  const broken = entry.loginAvailability && !entry.loginAvailability.available;

  return (
    <div
      className="grid cursor-pointer grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 px-4 py-3 transition-colors hover:bg-accent/30 lg:grid-cols-[minmax(13rem,1.6fr)_7.5rem_minmax(12rem,2fr)_6.5rem_8.5rem_4.5rem_2.5rem]"
      onClick={() => onOpen("overview")}
    >
      <AdminIdentity
        name={account.displayName}
        account={account.account}
        avatar={account.avatar}
        tier={tier}
        muted={!active}
        trailing={
          <>
            {caps.isSelf && (
              <span className="rounded bg-primary/10 px-1 text-[10px] text-primary">
                我
              </span>
            )}
            {online && (
              <span
                className="size-1.5 shrink-0 rounded-full bg-emerald-500"
                aria-label="在线"
              />
            )}
          </>
        }
      />
      <div className="max-lg:order-last max-lg:col-span-2">
        <TierBadge tier={tier} size="sm" />
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-1 max-lg:col-span-2">
        {tier !== "admin" ? (
          <span className="text-xs text-muted-foreground">不受权限点约束</span>
        ) : assignments.length === 0 ? (
          <span className="text-xs text-muted-foreground">未授权</span>
        ) : (
          <>
            {assignments.slice(0, 3).map((a, i) => (
              <span
                key={`${a.roleKey}-${a.appid ?? 0}-${i}`}
                className="inline-flex h-6 max-w-full items-center gap-1 rounded-md border bg-muted/40 px-1.5 text-[11px]"
              >
                <KeyRound className="size-3 shrink-0 text-muted-foreground" />
                <span className="truncate">
                  {roles.get(a.roleKey ?? "")?.name ?? a.roleKey}
                </span>
                {a.appName && (
                  <span className="truncate text-muted-foreground">
                    @{a.appName}
                  </span>
                )}
              </span>
            ))}
            {assignments.length > 3 && (
              <span className="text-[11px] text-muted-foreground">
                +{assignments.length - 3}
              </span>
            )}
          </>
        )}
      </div>
      <div className="hidden text-xs lg:block">
        <span className={cn(broken && "text-amber-600 dark:text-amber-400")}>
          {SOURCE_LABEL[sourceKey] ?? sourceKey.toUpperCase()}
        </span>
      </div>
      <div className="hidden text-xs text-muted-foreground tabular-nums lg:block">
        {formatDateTimeMinute(account.lastLoginAt)}
      </div>
      <div
        onClick={(event) => event.stopPropagation()}
        className="max-lg:row-start-1 max-lg:col-start-2 max-lg:justify-self-end"
      >
        {caps.canStatus ? (
          <Switch
            checked={active}
            onCheckedChange={(checked) =>
              onToggle(checked ? "active" : "disabled")
            }
            aria-label={active ? "停用" : "启用"}
          />
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                <Switch checked={active} disabled aria-label="状态" />
              </span>
            </TooltipTrigger>
            <TooltipContent className="max-w-64">
              {caps.manageReason ?? "缺少「管理员：启用与停用」权限"}
            </TooltipContent>
          </Tooltip>
        )}
      </div>
      <div
        onClick={(event) => event.stopPropagation()}
        className="hidden justify-self-end lg:block"
      >
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              aria-label="操作"
            >
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onSelect={() => onOpen("overview")}>
              <Users2 className="size-4" />
              详情
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!caps.canAccess && !caps.canTier}
              onSelect={() => onOpen("access")}
            >
              <KeyRound className="size-4" />
              角色授权
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!caps.canGrant}
              onSelect={() => onOpen("grants")}
            >
              <ShieldCheck className="size-4" />
              个人授权
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              disabled={!caps.canSession}
              onSelect={onLogout}
            >
              <LogOut className="size-4" />
              强制下线
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
