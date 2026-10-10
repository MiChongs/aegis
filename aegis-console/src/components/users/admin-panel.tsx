"use client";

import { useCallback, useMemo, useState } from "react";
import { RefreshCw, Search, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  useAdminAccountsQuery,
  useAdminRolesQuery,
  useAdminSessionQuery,
  useForceLogoutMutation,
  useOnlineAdminsQuery,
  useUpdateAdminAccountStatusMutation
} from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import type { OnlineAdmin, RoleDefinition } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { AdminCreateDialog } from "./admin-create-dialog";
import { AdminEditDialog } from "./admin-edit-dialog";
import { AdminDetailSheet } from "./admins/admin-detail-sheet";
import { AdminList, type AdminAction } from "./admins/admin-list";
import { AdminOverview, type AdminOverviewCounts } from "./admins/admin-overview";
import {
  DEFAULT_FILTERS,
  SORT_LABEL,
  adminName,
  filterAdmins,
  flattenAdmins,
  matchesScope,
  type AdminFilters,
  type AdminRecord,
  type AdminSort,
  type AdminSourceFilter
} from "./admins/admin-shared";
import { ConfirmActionDialog } from "./online/session-shared";

type PendingAction = { kind: "disable" | "force-logout"; admin: AdminRecord };

const SOURCE_OPTIONS: Array<{ value: AdminSourceFilter; label: string }> = [
  { value: "all", label: "全部登录方式" },
  { value: "password", label: "本地账号" },
  { value: "ldap", label: "LDAP" },
  { value: "oidc", label: "OIDC" },
  { value: "saml", label: "SAML" }
];

export function AdminPanel() {
  const operator = useAuthStore((s) => s.operator);
  // 在线状态、会话、认证健康、审计都挂在超管专属接口上，与后端的权限门控一致
  const isSuperAdmin = operator?.isSuperAdmin ?? false;
  const selfId = operator?.id != null ? Number(operator.id) : null;

  const adminsQuery = useAdminAccountsQuery();
  const rolesQuery = useAdminRolesQuery();
  const onlineQuery = useOnlineAdminsQuery({ enabled: isSuperAdmin });
  const sessionQuery = useAdminSessionQuery();
  const statusMutation = useUpdateAdminAccountStatusMutation();
  const forceLogoutMutation = useForceLogoutMutation();

  const [filters, setFilters] = useState<AdminFilters>(DEFAULT_FILTERS);
  const [detailId, setDetailId] = useState<number | null>(null);
  const [editAdmin, setEditAdmin] = useState<AdminRecord | null>(null);
  const [pending, setPending] = useState<PendingAction | null>(null);

  const all = useMemo(() => flattenAdmins(adminsQuery.data), [adminsQuery.data]);
  const roles = useMemo(
    () => new Map<string, RoleDefinition>((rolesQuery.data ?? []).map((r) => [r.key, r])),
    [rolesQuery.data]
  );
  const online = useMemo(
    () => new Map<number, OnlineAdmin>((isSuperAdmin ? onlineQuery.data ?? [] : []).map((o) => [o.adminId, o])),
    [isSuperAdmin, onlineQuery.data]
  );

  const counts = useMemo<AdminOverviewCounts>(() => {
    const count = (scope: keyof AdminOverviewCounts) => all.filter((a) => matchesScope(a, scope, online)).length;
    return {
      all: all.length,
      active: count("active"),
      disabled: count("disabled"),
      super: count("super"),
      online: count("online"),
      broken: count("broken")
    };
  }, [all, online]);

  const visible = useMemo(() => filterAdmins(all, filters, online), [all, filters, online]);
  const filtered = filters.keyword.trim() !== "" || filters.scope !== "all" || filters.source !== "all";
  const detailAdmin = detailId != null ? all.find((a) => a.id === detailId) ?? null : null;

  const patch = (next: Partial<AdminFilters>) => setFilters((prev) => ({ ...prev, ...next }));

  const runToggle = useCallback(
    async (admin: AdminRecord, next: "active" | "disabled") => {
      try {
        await statusMutation.mutateAsync({ adminId: admin.id, status: next });
        toast.success(next === "active" ? `已启用 ${adminName(admin)}` : `已停用 ${adminName(admin)}`);
        setPending(null);
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : "操作失败");
      }
    },
    [statusMutation]
  );

  const handleAction = useCallback(
    (action: AdminAction, admin: AdminRecord) => {
      if (action === "view") setDetailId(admin.id);
      else if (action === "edit-access") setEditAdmin(admin);
      else if (action === "force-logout") setPending({ kind: "force-logout", admin });
      else if (action === "toggle-status") {
        // 启用不会造成损失，直接执行；停用会让对方立刻无法登录，先确认
        if (admin.status === "disabled") void runToggle(admin, "active");
        else setPending({ kind: "disable", admin });
      }
    },
    [runToggle]
  );

  async function confirmPending() {
    if (!pending) return;
    if (pending.kind === "disable") {
      await runToggle(pending.admin, "disabled");
      return;
    }
    try {
      const result = await forceLogoutMutation.mutateAsync(pending.admin.id);
      toast.success(result?.revokedCount != null ? `已撤销 ${result.revokedCount} 个会话` : "已强制下线");
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "强制下线失败");
    }
  }

  const refreshing = adminsQuery.isFetching || onlineQuery.isFetching;
  const pendingSessions = pending ? online.get(pending.admin.id)?.sessionCount : undefined;

  return (
    <div className="space-y-4">
      <AdminOverview
        counts={counts}
        scope={filters.scope}
        onScopeChange={(scope) => patch({ scope })}
        loading={adminsQuery.isLoading}
        showSessionMetrics={isSuperAdmin}
      />

      {/* 工具栏：小屏时搜索独占一行，筛选与操作换到下一行 */}
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <div className="relative w-full sm:w-72">
          <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="搜索名称、邮箱、手机号或 ID"
            className="h-9 pr-8 pl-8 text-sm"
            value={filters.keyword}
            onChange={(e) => patch({ keyword: e.target.value })}
          />
          {filters.keyword ? (
            <button
              type="button"
              aria-label="清除搜索"
              className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
              onClick={() => patch({ keyword: "" })}
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={filters.source} onValueChange={(v) => patch({ source: v as AdminSourceFilter })}>
            <SelectTrigger className="h-9 w-[8.5rem] text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SOURCE_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filters.sort} onValueChange={(v) => patch({ sort: v as AdminSort })}>
            <SelectTrigger className="h-9 w-[8.5rem] text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(SORT_LABEL) as AdminSort[]).map((key) => (
                <SelectItem key={key} value={key}>按{SORT_LABEL[key]}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-center gap-2 sm:ml-auto">
          <Button
            variant="outline"
            size="icon"
            className="size-9"
            aria-label="刷新"
            disabled={refreshing}
            onClick={() => {
              void adminsQuery.refetch();
              if (isSuperAdmin) void onlineQuery.refetch();
            }}
          >
            <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
          </Button>
          <AdminCreateDialog />
        </div>
      </div>

      <div className="flex min-h-5 items-center justify-between gap-3 text-xs text-muted-foreground">
        <span className="tabular-nums">
          {adminsQuery.isLoading ? "" : filtered ? `筛选出 ${visible.length} 位，共 ${all.length} 位` : `共 ${all.length} 位管理员`}
        </span>
        {filtered ? (
          <button type="button" className="hover:text-foreground" onClick={() => setFilters({ ...DEFAULT_FILTERS, sort: filters.sort })}>
            清除筛选
          </button>
        ) : null}
      </div>

      <AdminList
        admins={visible}
        loading={adminsQuery.isLoading}
        roles={roles}
        online={online}
        selfId={selfId}
        showSessions={isSuperAdmin}
        filtered={filtered}
        onAction={handleAction}
      />

      <AdminDetailSheet
        admin={detailAdmin}
        open={detailAdmin !== null}
        onOpenChange={(open) => !open && setDetailId(null)}
        roles={roles}
        onlineEntry={detailAdmin ? online.get(detailAdmin.id) : undefined}
        self={detailAdmin?.id === selfId}
        superViewer={isSuperAdmin}
        currentSessionId={sessionQuery.data?.tokenId}
        onAction={handleAction}
      />

      <AdminEditDialog
        open={editAdmin !== null}
        onOpenChange={(v) => { if (!v) setEditAdmin(null); }}
        admin={editAdmin ? {
          id: editAdmin.id,
          account: editAdmin.account,
          displayName: editAdmin.displayName,
          isSuperAdmin: !!editAdmin.isSuperAdmin,
          assignments: editAdmin.assignments
        } : null}
      />

      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.kind === "disable" ? `停用 ${pending ? adminName(pending.admin) : ""}？` : `强制 ${pending ? adminName(pending.admin) : ""} 下线？`}
        description={
          pending?.kind === "disable"
            ? "停用后该账号无法登录控制台，角色分配保留，可随时重新启用。"
            : `将撤销其全部登录会话${pendingSessions ? `（${pendingSessions} 个）` : ""}，对方需要重新登录。`
        }
        confirmLabel={pending?.kind === "disable" ? "停用账号" : "强制下线"}
        pending={statusMutation.isPending || forceLogoutMutation.isPending}
        onConfirm={confirmPending}
      />
    </div>
  );
}
