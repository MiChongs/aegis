"use client";

import { useMemo, useState } from "react";
import {
  AppWindow,
  Ban,
  Globe2,
  Laptop,
  Lock,
  LogOut,
  Timer,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  useAdminSessionsQuery,
  useRevokeSessionMutation,
} from "@/lib/admin-hooks";
import {
  useAccessRolesQuery,
  useAdminDetailQuery,
  usePermissionCatalogQuery,
  useRevokeTempPermissionMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { DATA_SCOPE_LABEL, tierOf, type AdminDetail } from "@/lib/api/access";
import { formatDateTime, formatDateTimeMinute } from "@/lib/datetime";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import {
  AdminIdentity,
  CodeText,
  Pane,
  TierBadge,
  NodeIcon,
} from "./access-kit";
import { AdminAccessEditor } from "./admin-access-editor";
import { AdminGrantsEditor } from "./admin-grants-editor";
import { TempPermissionGrantForm } from "./temp-permission-form";
import {
  PermissionTreeEditor,
  type GrantState,
} from "./permission-tree-editor";
import { buildTreeIndex, permissionLabel, type TreeIndex } from "./tree-model";
import { useAppOptions } from "./pickers";

export type AdminSheetTab =
  "overview" | "access" | "grants" | "temp" | "effective" | "sessions";

export function AdminDetailSheet({
  adminId,
  initialTab,
  onClose,
}: {
  adminId: number | null;
  initialTab: AdminSheetTab;
  onClose: () => void;
}) {
  return (
    <Sheet open={adminId != null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        side="right"
        className="w-full gap-0 overflow-y-auto p-0 sm:max-w-3xl"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>管理员详情</SheetTitle>
        </SheetHeader>
        {adminId != null && (
          <SheetBody
            key={`${adminId}-${initialTab}`}
            adminId={adminId}
            initialTab={initialTab}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

/** 按「人 + 打开的页签」重建：从列表的不同入口打开时落在对应页签。 */
function SheetBody({
  adminId,
  initialTab,
}: {
  adminId: number;
  initialTab: AdminSheetTab;
}) {
  const [tab, setTab] = useState<AdminSheetTab>(initialTab);
  const detailQuery = useAdminDetailQuery(adminId);
  const catalogQuery = usePermissionCatalogQuery();
  const index = useMemo(
    () => buildTreeIndex(catalogQuery.data),
    [catalogQuery.data],
  );
  const detail = detailQuery.data;
  const can = usePermissionChecker();

  return (
    <>
      {!detail ? (
        <div className="space-y-4 p-6">
          <div className="flex items-center gap-3">
            <Skeleton className="size-11 rounded-full" />
            <div className="space-y-2">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-24" />
            </div>
          </div>
          <Skeleton className="h-40 w-full rounded-2xl" />
        </div>
      ) : (
        <div className="flex min-h-full flex-col">
          <DetailHeader detail={detail} />
          <Tabs
            value={tab}
            onValueChange={(next) => setTab(next as AdminSheetTab)}
            className="flex-1 gap-0"
          >
            <div className="sticky top-0 z-10 border-b bg-background px-5 pt-1 pb-2">
              <TabsList className="w-full justify-start overflow-x-auto">
                <TabsTrigger value="overview">概览</TabsTrigger>
                <TabsTrigger value="access">角色授权</TabsTrigger>
                <TabsTrigger value="grants">个人授权</TabsTrigger>
                <TabsTrigger value="temp">临时权限</TabsTrigger>
                <TabsTrigger value="effective">生效权限</TabsTrigger>
                {can("system:session:read") && (
                  <TabsTrigger value="sessions">会话</TabsTrigger>
                )}
              </TabsList>
            </div>
            <div className="p-5">
              <TabsContent value="overview">
                <OverviewTab detail={detail} index={index} />
              </TabsContent>
              <TabsContent value="access">
                <AdminAccessEditor detail={detail} />
              </TabsContent>
              <TabsContent value="grants">
                {catalogQuery.data ? (
                  <AdminGrantsEditor
                    key={detail.account.id}
                    detail={detail}
                    index={index}
                  />
                ) : (
                  <Skeleton className="h-96 rounded-2xl" />
                )}
              </TabsContent>
              <TabsContent value="temp">
                <TempTab detail={detail} index={index} />
              </TabsContent>
              <TabsContent value="effective">
                {catalogQuery.data ? (
                  <EffectiveTab detail={detail} index={index} />
                ) : (
                  <Skeleton className="h-96 rounded-2xl" />
                )}
              </TabsContent>
              <TabsContent value="sessions">
                <SessionsTab detail={detail} />
              </TabsContent>
            </div>
          </Tabs>
        </div>
      )}
    </>
  );
}

function DetailHeader({ detail }: { detail: AdminDetail }) {
  const account = detail.account;
  const tier = tierOf(account);
  return (
    <div className="border-b bg-muted/25 px-5 pt-6 pb-4">
      <div className="flex items-start gap-3 pr-8">
        <div className="min-w-0 flex-1">
          <AdminIdentity
            name={account.displayName}
            account={account.account}
            avatar={account.avatar}
            tier={tier}
            size="lg"
            muted={account.status === "disabled"}
          />
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1.5">
          <TierBadge tier={tier} />
          {account.status === "disabled" && (
            <span className="rounded-md bg-rose-500/10 px-1.5 py-0.5 text-[11px] font-medium text-rose-600 dark:text-rose-400">
              已停用
            </span>
          )}
        </div>
      </div>
      {detail.capabilities.manageReason && (
        <div className="mt-3 flex items-center gap-2 rounded-lg bg-muted/60 px-3 py-2 text-xs text-muted-foreground">
          <Lock className="size-3.5 shrink-0" />
          {detail.capabilities.manageReason}
        </div>
      )}
    </div>
  );
}

// ── 概览 ──

function OverviewTab({
  detail,
  index,
}: {
  detail: AdminDetail;
  index: TreeIndex;
}) {
  const account = detail.account;
  const tier = tierOf(account);
  const rolesQuery = useAccessRolesQuery();
  const roleName = (key?: string) =>
    rolesQuery.data?.find((r) => r.key === key)?.name ?? key;
  const union = useMemo(() => {
    const set = new Set<string>([
      ...detail.permissions.global,
      ...detail.permissions.anyApp,
    ]);
    for (const items of Object.values(detail.permissions.apps))
      for (const item of items) set.add(item);
    return set;
  }, [detail.permissions]);

  const coverage = useMemo(
    () =>
      index.roots.map((dir) => {
        const entry = index.entries.get(dir.key)!;
        const held =
          tier !== "admin"
            ? entry.leaves.length
            : entry.leaves.filter((code) => union.has(code)).length;
        return {
          key: dir.key,
          name: dir.name,
          icon: dir.icon,
          held,
          total: entry.leaves.length,
        };
      }),
    [index, union, tier],
  );

  return (
    <div className="flex flex-col gap-4">
      <Pane title="账号">
        <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-2">
          <Fact label="邮箱">{account.email || "—"}</Fact>
          <Fact label="登录方式">
            {(account.authSource || "password") === "password"
              ? "本地账号"
              : (account.authSource ?? "").toUpperCase()}
          </Fact>
          <Fact label="最近登录">{formatDateTime(account.lastLoginAt)}</Fact>
          <Fact label="创建时间">{formatDateTime(account.createdAt)}</Fact>
          {account.previousAccount && (
            <Fact label="原账号">@{account.previousAccount}</Fact>
          )}
        </dl>
      </Pane>

      <Pane
        title="权限覆盖"
        aside={
          <span className="text-xs text-muted-foreground tabular-nums">
            {tier !== "admin" ? "全部" : `${union.size} 项`}
          </span>
        }
      >
        <div className="grid gap-x-6 gap-y-2.5 p-4 sm:grid-cols-2">
          {coverage.map((item) => {
            const iconName = item.icon;
            const pct = item.total
              ? Math.round((item.held / item.total) * 100)
              : 0;
            return (
              <div key={item.key} className="flex items-center gap-2.5">
                <NodeIcon
                  name={iconName}
                  className="size-3.5 shrink-0 text-muted-foreground"
                />
                <span className="w-20 shrink-0 truncate text-xs">
                  {item.name}
                </span>
                <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                  <span
                    className={cn(
                      "block h-full rounded-full",
                      pct === 100 ? "bg-primary" : "bg-primary/60",
                    )}
                    style={{ width: `${pct}%` }}
                  />
                </span>
                <span className="w-12 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">
                  {item.held}/{item.total}
                </span>
              </div>
            );
          })}
        </div>
      </Pane>

      {tier === "admin" && (
        <Pane
          title="生效的角色"
          aside={
            <span className="text-xs text-muted-foreground">
              按数据权限展开
            </span>
          }
        >
          <div className="divide-y divide-border/60">
            {detail.resolved.length === 0 && (
              <div className="px-4 py-6 text-center text-sm text-muted-foreground">
                暂无角色
              </div>
            )}
            {detail.resolved.map((item, i) => (
              <div
                key={`${item.roleKey}-${item.appid ?? 0}-${i}`}
                className="flex items-center gap-3 px-4 py-2.5 text-sm"
              >
                <span className="min-w-0 flex-1 truncate font-medium">
                  {roleName(item.roleKey)}
                </span>
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  {item.allApps ? (
                    <Globe2 className="size-3.5" />
                  ) : (
                    <AppWindow className="size-3.5" />
                  )}
                  {item.allApps
                    ? "全部应用"
                    : item.appid
                      ? item.appName || `应用 #${item.appid}`
                      : "平台级"}
                </span>
                {item.dataScope && item.dataScope !== "assigned" && (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                    {DATA_SCOPE_LABEL[item.dataScope]}
                  </span>
                )}
              </div>
            ))}
          </div>
        </Pane>
      )}
    </div>
  );
}

function Fact({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 truncate">{children}</dd>
    </div>
  );
}

// ── 生效权限 ──

function EffectiveTab({
  detail,
  index,
}: {
  detail: AdminDetail;
  index: TreeIndex;
}) {
  const apps = useAppOptions();
  const tier = tierOf(detail.account);
  const appIds = Object.keys(detail.permissions.apps);
  const [scope, setScope] = useState<string>("platform");
  const codes = useMemo(() => {
    if (tier !== "admin" || detail.permissions.all) return new Set(index.codes);
    if (scope === "platform") return new Set(detail.permissions.global);
    if (scope === "any") return new Set(detail.permissions.anyApp);
    return new Set(detail.permissions.apps[scope] ?? detail.permissions.anyApp);
  }, [scope, detail.permissions, index.codes, tier]);
  const value = useMemo<GrantState>(
    () => new Map([...codes].map((code) => [code, "allow" as const])),
    [codes],
  );
  const name = (id: string) =>
    apps.find((app) => String(app.id) === id)?.name ?? `应用 #${id}`;

  return (
    <Pane
      title="生效权限"
      aside={
        tier === "admin" && (
          <Select value={scope} onValueChange={setScope}>
            <SelectTrigger size="sm" className="h-8 w-44 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="platform">平台级</SelectItem>
              <SelectItem value="any">任意应用</SelectItem>
              {appIds.map((id) => (
                <SelectItem key={id} value={id}>
                  {name(id)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      }
      bodyClassName="flex flex-col"
    >
      <PermissionTreeEditor
        index={index}
        mode="view"
        value={value}
        allowDeny={false}
        heightClassName="max-h-[calc(100dvh-20rem)] min-h-72"
      />
    </Pane>
  );
}

// ── 临时权限 ──

function TempTab({ detail, index }: { detail: AdminDetail; index: TreeIndex }) {
  const can = usePermissionChecker();
  const revoke = useRevokeTempPermissionMutation();
  const apps = useAppOptions();
  const canGrant =
    can("system:temp_perm:grant") &&
    detail.capabilities.canAccess !== false &&
    !detail.capabilities.isSelf &&
    tierOf(detail.account) === "admin";
  const items = detail.tempPermissions.filter((item) => !item.isRevoked);

  const doRevoke = async (id: number) => {
    try {
      await revoke.mutateAsync(id);
      toast.success("临时权限已撤销");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "撤销失败");
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {canGrant && (
        <TempPermissionGrantForm adminId={detail.account.id} index={index} />
      )}
      <Pane title="未过期的临时权限" icon={Timer}>
        <div className="divide-y divide-border/60">
          {items.length === 0 && (
            <div className="py-8 text-center text-sm text-muted-foreground">
              暂无临时权限
            </div>
          )}
          {items.map((item) => (
            <div
              key={item.id}
              className="flex flex-wrap items-center gap-3 px-4 py-2.5"
            >
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">
                  {permissionLabel(index, item.permission)}
                </div>
                <div className="flex flex-wrap items-center gap-x-3 text-[11px] text-muted-foreground">
                  <CodeText>{item.permission}</CodeText>
                  <span>
                    {item.appId
                      ? (apps.find((a) => a.id === item.appId)?.name ??
                        `应用 #${item.appId}`)
                      : "不限应用"}
                  </span>
                  {item.reason && (
                    <span className="truncate">{item.reason}</span>
                  )}
                </div>
              </div>
              <span className="text-xs text-muted-foreground tabular-nums">
                至 {formatDateTimeMinute(item.expiresAt)}
              </span>
              {can("system:temp_perm:revoke") && (
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground hover:text-rose-600"
                  aria-label="撤销"
                  onClick={() => void doRevoke(item.id)}
                >
                  <Trash2 className="size-4" />
                </Button>
              )}
            </div>
          ))}
        </div>
      </Pane>
    </div>
  );
}

// ── 会话 ──

function SessionsTab({ detail }: { detail: AdminDetail }) {
  const can = usePermissionChecker();
  const sessionsQuery = useAdminSessionsQuery(detail.account.id, {
    enabled: can("system:session:read"),
  });
  const revoke = useRevokeSessionMutation();
  const sessions = (sessionsQuery.data ?? []).filter((s) => !s.isRevoked);
  const canRevoke = detail.capabilities.canSession;

  return (
    <Pane
      title="登录会话"
      icon={Laptop}
      aside={
        <span className="text-xs text-muted-foreground tabular-nums">
          {sessions.length} 个
        </span>
      }
    >
      <div className="divide-y divide-border/60">
        {sessionsQuery.isLoading && (
          <div className="p-4">
            <Skeleton className="h-10" />
          </div>
        )}
        {!sessionsQuery.isLoading && sessions.length === 0 && (
          <div className="py-8 text-center text-sm text-muted-foreground">
            暂无会话
          </div>
        )}
        {sessions.map((session) => (
          <div
            key={session.id}
            className="flex flex-wrap items-center gap-3 px-4 py-2.5"
          >
            <Laptop className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm">
                {session.device || session.userAgent || "未知设备"}
              </div>
              <div className="text-[11px] text-muted-foreground tabular-nums">
                {session.ip} 活跃于 {formatDateTimeMinute(session.lastActiveAt)}
              </div>
            </div>
            {canRevoke && (
              <Button
                variant="ghost"
                size="sm"
                className="h-8 gap-1.5 text-muted-foreground hover:text-rose-600"
                onClick={async () => {
                  try {
                    await revoke.mutateAsync(session.id);
                    toast.success("会话已撤销");
                  } catch (err) {
                    toast.error(
                      err instanceof ApiError ? err.message : "撤销失败",
                    );
                  }
                }}
              >
                <LogOut className="size-3.5" />
                下线
              </Button>
            )}
          </div>
        ))}
      </div>
      {!canRevoke && sessions.length > 0 && (
        <div className="flex items-center gap-2 border-t px-4 py-2 text-[11px] text-muted-foreground">
          <Ban className="size-3" />
          {detail.capabilities.manageReason ??
            "缺少「管理员会话：强制下线」权限"}
        </div>
      )}
    </Pane>
  );
}
