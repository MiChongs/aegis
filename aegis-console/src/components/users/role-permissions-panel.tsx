"use client";

import { useState } from "react";
import { LoadingState } from "@/components/ui/data-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminRolePermissionTreeQuery } from "@/lib/admin-hooks";
import { PermissionTree } from "./permission-tree";
import { RolePermissionMatrix } from "./role-permission-matrix";

const roleLabels: Record<string, string> = {
  super_admin: "超级管理员",
  platform_admin: "平台管理员",
  app_admin: "应用管理员",
  app_operator: "应用运营员",
  app_auditor: "应用审计员",
  app_viewer: "应用查看员"
};

export function RolePermissionsPanel() {
  const query = useAdminRolePermissionTreeQuery();
  const roles = query.data || [];
  const [view, setView] = useState<"matrix" | "tree">("matrix");

  if (query.isLoading) return <LoadingState title="加载中" />;

  return (
    <div className="space-y-5">
      {/* 视图切换 */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">{roles.length} 个角色</span>
        <div className="flex items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5 w-fit">
          <button
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${view === "matrix" ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setView("matrix")}
          >
            权限矩阵
          </button>
          <button
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${view === "tree" ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setView("tree")}
          >
            角色详情
          </button>
        </div>
      </div>

      {view === "matrix" ? (
        <RolePermissionMatrix roles={roles} />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {roles.map((role) => (
            <div key={role.key} className="rounded-xl border p-3">
              <div className="mb-2">
                <div className="text-sm font-medium">{roleLabels[role.key] || role.name}</div>
                <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                  <span>{role.scope === "global" ? "全局" : "应用级"}</span>
                  <span>·</span>
                  <span>Level {role.level}</span>
                  {role.description && <span>· {role.description}</span>}
                </div>
              </div>
              <PermissionTree groups={role.permissionGroups} compact />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
