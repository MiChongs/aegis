"use client";

import { useEffect, useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  AppWindow,
  Copy,
  Crown,
  Database,
  KeyRound,
  Layers,
  MoreHorizontal,
  Pencil,
  Plus,
  Power,
  Search,
  ShieldAlert,
  Trash2,
  Users2,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import {
  useAccessRolesQuery,
  usePermissionCatalogQuery,
  useRoleDetailQuery,
  useSetRoleStatusMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { DATA_SCOPE_LABEL, type AccessRole } from "@/lib/api/access";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { CodeText, Pane } from "./access-kit";
import {
  RoleCopyDialog,
  RoleCreateDialog,
  RoleDeleteDialog,
} from "./role-dialogs";
import { RoleDataScopeTab } from "./role-data-scope-tab";
import { RoleInfoTab } from "./role-info-tab";
import { RoleMembersTab } from "./role-members-tab";
import { RolePermissionsTab } from "./role-permissions-tab";
import { buildTreeIndex, expandPatterns } from "./tree-model";

/**
 * 角色工作台：左侧角色列表，右侧角色详情（功能权限 / 数据权限 / 成员 / 基本信息）。
 *
 * 选中的角色与页签放在地址栏（`?tab=roles&role=…&view=…`），刷新与分享链接都能回到原处。
 */

type RoleView = "permissions" | "data" | "members" | "info";

export function RoleWorkspace() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const can = usePermissionChecker();
  const rolesQuery = useAccessRolesQuery();
  const catalogQuery = usePermissionCatalogQuery();
  const index = useMemo(
    () => buildTreeIndex(catalogQuery.data),
    [catalogQuery.data],
  );
  const roles = useMemo(() => rolesQuery.data ?? [], [rolesQuery.data]);

  const selectedKey = params.get("role") ?? roles[0]?.key ?? null;
  const rawView = params.get("view") as RoleView | null;
  const view: RoleView =
    rawView && ["permissions", "data", "members", "info"].includes(rawView)
      ? rawView
      : "permissions";

  const setQuery = (patch: Record<string, string | null>) => {
    const next = new URLSearchParams(params.toString());
    for (const [key, value] of Object.entries(patch)) {
      if (value === null) next.delete(key);
      else next.set(key, value);
    }
    router.replace(`${pathname}?${next.toString()}`, { scroll: false });
  };

  const [creating, setCreating] = useState(false);
  const [copying, setCopying] = useState<AccessRole | null>(null);
  const [deleting, setDeleting] = useState<AccessRole | null>(null);

  const selected = roles.find((role) => role.key === selectedKey) ?? null;

  // 选中的角色被删掉（或链接里的角色不存在）时落回第一个
  useEffect(() => {
    if (
      !rolesQuery.isLoading &&
      roles.length > 0 &&
      selectedKey &&
      !roles.some((role) => role.key === selectedKey)
    ) {
      setQuery({ role: roles[0]!.key });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rolesQuery.isLoading, roles, selectedKey]);

  return (
    <div className="grid min-w-0 gap-4 @container/roles lg:grid-cols-[19rem_minmax(0,1fr)]">
      <RoleList
        roles={roles}
        loading={rolesQuery.isLoading}
        selectedKey={selectedKey}
        onSelect={(key) => setQuery({ role: key })}
        canCreate={can("system:role:create")}
        onCreate={() => setCreating(true)}
      />
      {selected ? (
        <RoleDetailPane
          key={selected.key}
          role={selected}
          view={view}
          index={index}
          catalogReady={Boolean(catalogQuery.data)}
          onView={(next) =>
            setQuery({ view: next === "permissions" ? null : next })
          }
          onCopy={() => setCopying(selected)}
          onDelete={() => setDeleting(selected)}
        />
      ) : (
        <Pane className="min-h-96">
          <div className="grid h-full min-h-96 place-items-center text-sm text-muted-foreground">
            {rolesQuery.isLoading ? "加载中" : "暂无角色"}
          </div>
        </Pane>
      )}

      <RoleCreateDialog
        open={creating}
        onOpenChange={setCreating}
        roles={roles}
        onCreated={(key) => setQuery({ role: key, view: null })}
      />
      <RoleCopyDialog
        source={copying}
        onOpenChange={(open) => !open && setCopying(null)}
        onCreated={(key) => setQuery({ role: key, view: null })}
      />
      <RoleDeleteDialog
        role={deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
      />
    </div>
  );
}

// ── 列表 ──

function RoleList({
  roles,
  loading,
  selectedKey,
  onSelect,
  canCreate,
  onCreate,
}: {
  roles: AccessRole[];
  loading: boolean;
  selectedKey: string | null;
  onSelect: (key: string) => void;
  canCreate: boolean;
  onCreate: () => void;
}) {
  const [keyword, setKeyword] = useState("");
  const filtered = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    if (!k) return roles;
    return roles.filter(
      (role) =>
        role.name.toLowerCase().includes(k) ||
        role.key.toLowerCase().includes(k),
    );
  }, [roles, keyword]);
  const groups = [
    {
      key: "builtin",
      title: "内置角色",
      items: filtered.filter((role) => !role.isCustom),
    },
    {
      key: "global",
      title: "平台级自定义角色",
      items: filtered.filter(
        (role) => role.isCustom && role.scope === "global",
      ),
    },
    {
      key: "app",
      title: "应用级自定义角色",
      items: filtered.filter((role) => role.isCustom && role.scope === "app"),
    },
  ];

  return (
    <Pane
      title="角色"
      icon={KeyRound}
      aside={
        canCreate && (
          <Button
            size="sm"
            className="h-7 gap-1 px-2.5 text-xs"
            onClick={onCreate}
          >
            <Plus className="size-3.5" />
            新建
          </Button>
        )
      }
      className="lg:sticky lg:top-4 lg:max-h-[calc(100dvh-7rem)]"
      bodyClassName="flex min-h-0 flex-col"
    >
      <div className="border-b border-border/70 p-2.5">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索角色"
            className="h-8 pl-8 text-xs"
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
        {loading &&
          Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="flex items-center gap-2.5 px-2 py-2">
              <Skeleton className="size-8 rounded-lg" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-2.5 w-32" />
              </div>
            </div>
          ))}
        {groups.map(
          (group) =>
            group.items.length > 0 && (
              <div key={group.key} className="mb-2">
                <div className="flex items-center justify-between px-2 pt-2 pb-1 text-[11px] font-medium text-muted-foreground">
                  {group.title}
                  <span className="tabular-nums">{group.items.length}</span>
                </div>
                {group.items.map((role) => (
                  <RoleListItem
                    key={role.key}
                    role={role}
                    active={role.key === selectedKey}
                    onSelect={() => onSelect(role.key)}
                  />
                ))}
              </div>
            ),
        )}
        {!loading && filtered.length === 0 && (
          <div className="py-10 text-center text-xs text-muted-foreground">
            暂无匹配角色
          </div>
        )}
      </div>
    </Pane>
  );
}

function RoleListItem({
  role,
  active,
  onSelect,
}: {
  role: AccessRole;
  active: boolean;
  onSelect: () => void;
}) {
  const disabled = role.status === "disabled";
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-xl px-2 py-2 text-left transition-colors",
        active ? "bg-primary/8 ring-1 ring-primary/25" : "hover:bg-accent/60",
      )}
    >
      <TonalIcon
        icon={
          role.key === "super_admin"
            ? Crown
            : role.scope === "global"
              ? Layers
              : AppWindow
        }
        tone={
          disabled
            ? "muted"
            : role.key === "super_admin"
              ? "amber"
              : role.scope === "global"
                ? "violet"
                : "sky"
        }
        size="sm"
      />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5">
          <span
            className={cn(
              "truncate text-sm font-medium",
              disabled && "text-muted-foreground line-through",
            )}
          >
            {role.name}
          </span>
          {disabled && (
            <span className="shrink-0 rounded bg-muted px-1 text-[10px] text-muted-foreground">
              已停用
            </span>
          )}
        </div>
        <CodeText className="block truncate">{role.key}</CodeText>
      </div>
      <span className="flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground tabular-nums">
        <Users2 className="size-3" />
        {role.memberCount}
      </span>
    </button>
  );
}

// ── 详情 ──

function RoleDetailPane({
  role,
  view,
  index,
  catalogReady,
  onView,
  onCopy,
  onDelete,
}: {
  role: AccessRole;
  view: RoleView;
  index: ReturnType<typeof buildTreeIndex>;
  catalogReady: boolean;
  onView: (view: RoleView) => void;
  onCopy: () => void;
  onDelete: () => void;
}) {
  const can = usePermissionChecker();
  const detailQuery = useRoleDetailQuery(role.key);
  const statusMutation = useSetRoleStatusMutation();
  const detail = detailQuery.data;
  const canEdit = can("system:role:edit") && role.key !== "super_admin";
  const effective = useMemo(
    () => new Set(role.permissions),
    [role.permissions],
  );
  const dangerCount = useMemo(
    () =>
      [...effective].filter(
        (code) => index.entries.get(index.codeKey.get(code) ?? "")?.node.danger,
      ).length,
    [effective, index],
  );
  const coverage = useMemo(() => {
    const total = index.codes.filter((code) => {
      const scope = index.entries.get(index.codeKey.get(code) ?? "")?.node
        .scope;
      return role.scope === "global" || scope !== "platform";
    }).length;
    return total ? Math.round((effective.size / total) * 100) : 0;
  }, [effective, index, role.scope]);

  const toggleStatus = async () => {
    const next = role.status === "disabled" ? "active" : "disabled";
    try {
      await statusMutation.mutateAsync({ roleKey: role.key, status: next });
      toast.success(
        next === "active" ? `已启用「${role.name}」` : `已停用「${role.name}」`,
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  const base = useMemo(
    () =>
      role.isCustom ? new Set<string>() : expandPatterns(index, role.allow),
    [index, role.allow, role.isCustom],
  );

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <section
        className="overflow-hidden rounded-2xl border border-border bg-card"
        style={{ boxShadow: "var(--shadow-soft)" }}
      >
        <div className="flex flex-wrap items-start gap-4 p-5">
          <TonalIcon
            icon={
              role.key === "super_admin"
                ? Crown
                : role.scope === "global"
                  ? Layers
                  : AppWindow
            }
            tone={
              role.status === "disabled"
                ? "muted"
                : role.key === "super_admin"
                  ? "amber"
                  : role.scope === "global"
                    ? "violet"
                    : "sky"
            }
            size="lg"
          />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold tracking-tight">
                {role.name}
              </h2>
              <span
                className={cn(
                  "rounded-md px-1.5 py-0.5 text-[11px] font-medium",
                  role.isCustom
                    ? "bg-primary/10 text-primary"
                    : "bg-muted text-muted-foreground",
                )}
              >
                {role.isCustom ? "自定义" : "内置"}
              </span>
              <span
                className={cn(
                  "rounded-md px-1.5 py-0.5 text-[11px] font-medium",
                  role.scope === "global"
                    ? "bg-violet-500/10 text-violet-700 dark:text-violet-300"
                    : "bg-sky-500/10 text-sky-700 dark:text-sky-300",
                )}
              >
                {role.scope === "global" ? "平台级" : "应用级"}
              </span>
              {role.status === "disabled" && (
                <span className="rounded-md bg-rose-500/10 px-1.5 py-0.5 text-[11px] font-medium text-rose-600 dark:text-rose-400">
                  已停用
                </span>
              )}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              <CodeText>{role.key}</CodeText>
              <span>级别 {role.level}</span>
              {role.baseRole && <span>继承 {role.baseRole}</span>}
            </div>
            {role.description && (
              <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
                {role.description}
              </p>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {role.isCustom && canEdit && (
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1.5"
                onClick={toggleStatus}
                disabled={statusMutation.isPending}
              >
                <Power className="size-3.5" />
                {role.status === "disabled" ? "启用" : "停用"}
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8"
                  aria-label="更多"
                >
                  <MoreHorizontal className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                {role.isCustom && canEdit && (
                  <DropdownMenuItem onSelect={() => onView("info")}>
                    <Pencil className="size-4" />
                    编辑信息
                  </DropdownMenuItem>
                )}
                {can("system:role:create") && role.key !== "super_admin" && (
                  <DropdownMenuItem onSelect={onCopy}>
                    <Copy className="size-4" />
                    复制为新角色
                  </DropdownMenuItem>
                )}
                {role.isCustom && can("system:role:delete") && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                      <Trash2 className="size-4" />
                      删除角色
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
        <div className="grid grid-cols-2 border-t border-border/70 sm:grid-cols-4">
          <HeaderMetric
            icon={KeyRound}
            label="功能权限"
            value={`${effective.size}`}
            hint={`覆盖 ${coverage}%`}
          />
          <HeaderMetric
            icon={ShieldAlert}
            label="高危操作"
            value={`${dangerCount}`}
            tone={dangerCount > 0 ? "rose" : undefined}
          />
          <HeaderMetric
            icon={Users2}
            label="成员"
            value={`${role.memberCount}`}
          />
          <HeaderMetric
            icon={Database}
            label="数据权限"
            value={
              role.scope === "global"
                ? "全部"
                : DATA_SCOPE_LABEL[role.dataScope]
            }
            small
          />
        </div>
      </section>

      <Tabs
        value={view}
        onValueChange={(next) => onView(next as RoleView)}
        className="min-w-0"
      >
        <TabsList>
          <TabsTrigger value="permissions">功能权限</TabsTrigger>
          <TabsTrigger value="data">数据权限</TabsTrigger>
          <TabsTrigger value="members">成员</TabsTrigger>
          <TabsTrigger value="info">基本信息</TabsTrigger>
        </TabsList>
        <TabsContent value="permissions">
          {!catalogReady || !detail ? (
            <Pane className="min-h-96">
              <div className="space-y-2 p-4">
                {Array.from({ length: 10 }).map((_, i) => (
                  <Skeleton
                    key={i}
                    className="h-7"
                    style={{ marginLeft: (i % 3) * 22 }}
                  />
                ))}
              </div>
            </Pane>
          ) : (
            <RolePermissionsTab
              key={detail.role.updatedAt ?? detail.role.key}
              role={role}
              detail={detail}
              index={index}
              base={base}
              canEdit={canEdit && detail.editable}
            />
          )}
        </TabsContent>
        <TabsContent value="data">
          <RoleDataScopeTab role={role} canEdit={canEdit} />
        </TabsContent>
        <TabsContent value="members">
          <RoleMembersTab
            role={role}
            members={detail?.members ?? []}
            loading={detailQuery.isLoading}
          />
        </TabsContent>
        <TabsContent value="info">
          <RoleInfoTab role={role} canEdit={canEdit} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function HeaderMetric({
  icon: Icon,
  label,
  value,
  hint,
  tone,
  small,
}: {
  icon: typeof KeyRound;
  label: string;
  value: string;
  hint?: string;
  tone?: "rose";
  small?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-center gap-3 border-border/70 px-5 py-3 not-last:border-r max-sm:nth-[2]:border-r-0 max-sm:nth-[-n+2]:border-b">
      <Icon
        className={cn(
          "size-4 shrink-0",
          tone === "rose" ? "text-rose-500" : "text-muted-foreground",
        )}
      />
      <div className="min-w-0">
        <div className="text-[11px] text-muted-foreground">{label}</div>
        <div
          className={cn(
            "truncate font-semibold tabular-nums",
            small ? "text-sm" : "text-lg leading-6",
          )}
        >
          {value}
        </div>
        {hint && (
          <div className="text-[10px] text-muted-foreground">{hint}</div>
        )}
      </div>
    </div>
  );
}
