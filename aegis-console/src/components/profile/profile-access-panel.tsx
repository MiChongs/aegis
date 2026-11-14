"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { AppWindow, Globe2, KeyRound, Layers, ShieldAlert, Sparkles } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Panel, StatTile } from "@/components/users/detail/user-detail-shared";
import { TierBadge } from "@/components/access/access-kit";
import { PermissionTreeEditor, type GrantState } from "@/components/access/permission-tree-editor";
import { useAppOptions } from "@/components/access/pickers";
import { buildTreeIndex } from "@/components/access/tree-model";
import { usePermissionCatalogQuery } from "@/lib/access-hooks";
import { DATA_SCOPE_LABEL, getAccessSnapshot, tierOf } from "@/lib/api/access";
import type { AdminAssignment, RoleWithPermissions } from "@/lib/api/types";
import { useAuthStore } from "@/lib/auth-store";

/**
 * 「我的权限」：身份档位、按数据权限展开后的角色、各作用域的生效权限树。
 *
 * 数据只来自 `/auth/me` 的生效权限快照（与真实判定同一段展开代码），不再拿角色树拼「已授予」。
 * 旧的 props（roleTree / loading）保留签名兼容调用方，只用 roleTree 补角色名称。
 */
export function ProfileAccessPanel({
  isSuperAdmin,
  assignments,
  roleTree
}: {
  isSuperAdmin: boolean;
  assignments: AdminAssignment[];
  roleTree?: RoleWithPermissions[];
  loading?: boolean;
}) {
  const token = useAuthStore((s) => s.accessToken);
  const snapshotQuery = useQuery({
    queryKey: ["admin-session", token],
    queryFn: () => getAccessSnapshot(token as string),
    enabled: Boolean(token),
    staleTime: 30 * 1000
  });
  const catalogQuery = usePermissionCatalogQuery();
  const index = React.useMemo(() => buildTreeIndex(catalogQuery.data), [catalogQuery.data]);
  const apps = useAppOptions();
  const snapshot = snapshotQuery.data;
  const tier = snapshot ? tierOf(snapshot) : isSuperAdmin ? "super" : "admin";
  const resolved = snapshot?.assignments ?? assignments;
  const appIds = Object.keys(snapshot?.permissions?.apps ?? {});
  const [scope, setScope] = React.useState("platform");

  const union = React.useMemo(() => {
    const set = new Set<string>([...(snapshot?.permissions?.global ?? []), ...(snapshot?.permissions?.anyApp ?? [])]);
    for (const items of Object.values(snapshot?.permissions?.apps ?? {})) for (const item of items) set.add(item);
    return set;
  }, [snapshot]);

  const value = React.useMemo<GrantState>(() => {
    if (tier !== "admin" || snapshot?.permissions?.all) return new Map(index.codes.map((code) => [code, "allow" as const]));
    const codes =
      scope === "platform"
        ? snapshot?.permissions?.global ?? []
        : scope === "any"
          ? snapshot?.permissions?.anyApp ?? []
          : snapshot?.permissions?.apps?.[scope] ?? snapshot?.permissions?.anyApp ?? [];
    return new Map(codes.map((code) => [code, "allow" as const]));
  }, [tier, snapshot, scope, index.codes]);

  const danger = React.useMemo(
    () => [...union].filter((code) => index.entries.get(index.codeKey.get(code) ?? "")?.node.danger).length,
    [union, index]
  );
  const roleName = (key?: string) => roleTree?.find((role) => role.key === key)?.name ?? key;
  const appName = (id: string) => apps.find((app) => String(app.id) === id)?.name ?? `应用 #${id}`;

  return (
    <div className="space-y-4">
      <Panel title="我的身份" icon={<KeyRound className="size-4" />} action={<TierBadge tier={tier} />} bodyClassName="space-y-4 px-5 py-5">
        <div className="grid gap-3 sm:grid-cols-3">
          <StatTile label="角色" value={tier !== "admin" ? "—" : resolved.length} icon={<Layers className="size-3.5" />} />
          <StatTile
            label="生效权限"
            value={snapshotQuery.isLoading ? "…" : tier !== "admin" ? "全部" : union.size}
            tone={union.size > 0 || tier !== "admin" ? "success" : "default"}
            icon={<Sparkles className="size-3.5" />}
          />
          <StatTile label="高危操作" value={tier !== "admin" ? "全部" : danger} tone={danger > 0 ? "warning" : "default"} icon={<ShieldAlert className="size-3.5" />} />
        </div>

        {tier === "admin" &&
          (snapshotQuery.isLoading ? (
            <Skeleton className="h-11 w-full rounded-lg" />
          ) : resolved.length === 0 ? (
            <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">暂无角色分配</p>
          ) : (
            <div className="space-y-1.5">
              {resolved.map((assignment, i) => {
                const extra = assignment as typeof assignment & { allApps?: boolean; dataScope?: keyof typeof DATA_SCOPE_LABEL };
                return (
                  <div key={`${assignment.roleKey}-${assignment.appid ?? "g"}-${i}`} className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{roleName(assignment.roleKey)}</p>
                      <p className="truncate font-mono text-[11px] text-muted-foreground">{assignment.roleKey}</p>
                    </div>
                    <div className="flex shrink-0 items-center gap-1.5">
                      {extra.dataScope && extra.dataScope !== "assigned" && (
                        <Badge variant="outline" size="sm">
                          {DATA_SCOPE_LABEL[extra.dataScope]}
                        </Badge>
                      )}
                      <Badge variant={assignment.appid != null ? "secondary" : "info"} size="sm" className="gap-1">
                        {extra.allApps ? <Globe2 className="size-3" /> : <AppWindow className="size-3" />}
                        {extra.allApps ? "全部应用" : assignment.appid != null ? assignment.appName || `应用 ${assignment.appid}` : "平台级"}
                      </Badge>
                    </div>
                  </div>
                );
              })}
            </div>
          ))}
      </Panel>

      {catalogQuery.data ? (
        <Panel
          title="权限明细"
          icon={<Layers className="size-4" />}
          action={
            tier === "admin" && (
              <Select value={scope} onValueChange={setScope}>
                <SelectTrigger size="sm" className="h-8 w-40 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="platform">平台级</SelectItem>
                  <SelectItem value="any">任意应用</SelectItem>
                  {appIds.map((id) => (
                    <SelectItem key={id} value={id}>
                      {appName(id)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )
          }
          bodyClassName="px-0 py-0"
        >
          <PermissionTreeEditor index={index} mode="view" value={value} allowDeny={false} heightClassName="max-h-[60vh] min-h-64" />
        </Panel>
      ) : (
        <Skeleton className="h-64 w-full rounded-2xl" />
      )}
    </div>
  );
}
