"use client";

import * as React from "react";
import { ChevronDown, KeyRound, Layers, ShieldUser, Sparkles } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Skeleton } from "@/components/ui/skeleton";
import { Panel, StatTile } from "@/components/users/detail/user-detail-shared";
import { PermissionTree } from "@/components/users/permission-tree";
import { cn } from "@/lib/utils";
import type { AdminAssignment, PermissionGroup, RoleWithPermissions } from "@/lib/api/types";

/**
 * 合并当前管理员实际生效的权限组。
 *
 * 同一个权限点可能被多个角色覆盖，只要**任一角色**授予了它就算有 ——
 * 逐组求并集时"授予"必须能覆盖"未授予"，反过来不行。
 * `description === "granted"` 是后端在这棵树上表达授予与否的方式（不是描述文案）。
 */
function mergeGroups(roles: RoleWithPermissions[]): PermissionGroup[] {
  const merged: PermissionGroup[] = [];
  for (const role of roles) {
    for (const group of role.permissionGroups || []) {
      const existing = merged.find((item) => item.key === group.key);
      if (!existing) {
        merged.push({ ...group, permissions: group.permissions.map((item) => ({ ...item })) });
        continue;
      }
      for (const permission of group.permissions) {
        const hit = existing.permissions.find((item) => item.code === permission.code);
        if (!hit) existing.permissions.push({ ...permission });
        else if (permission.description === "granted") hit.description = "granted";
      }
    }
  }
  return merged;
}

function grantedCount(groups: PermissionGroup[]) {
  return groups.reduce(
    (total, group) => total + group.permissions.filter((item) => item.description === "granted").length,
    0
  );
}

export function ProfileAccessPanel({
  isSuperAdmin,
  assignments,
  roleTree,
  loading
}: {
  isSuperAdmin: boolean;
  assignments: AdminAssignment[];
  roleTree?: RoleWithPermissions[];
  loading: boolean;
}) {
  const [open, setOpen] = React.useState(false);

  const myRoleKeys = assignments.map((item) => item.roleKey).filter(Boolean) as string[];
  const myRoles = (roleTree || []).filter((role) => isSuperAdmin || myRoleKeys.includes(role.key));
  const groups = mergeGroups(myRoles);
  const granted = grantedCount(groups);
  const total = groups.reduce((sum, group) => sum + group.permissions.length, 0);

  return (
    <div className="space-y-4">
      {isSuperAdmin ? (
        <Alert>
          <ShieldUser />
          <AlertTitle>超级管理员</AlertTitle>
          <AlertDescription>
            你的权限不来自角色分配，而是账号上的超管标志本身 —— 平台、应用、组织三个作用域下的
            全部权限点默认放行，包括后续新增的。撤销它需要另一位超级管理员操作。
          </AlertDescription>
        </Alert>
      ) : null}

      <Panel
        title="我的角色"
        icon={<KeyRound className="size-4" />}
        description="角色决定你能进哪些页面、能改哪些数据。要调整只能由有权限的管理员在「用户与权限」里改。"
        bodyClassName="space-y-4 px-5 py-5"
      >
        <div className="grid gap-3 sm:grid-cols-3">
          <StatTile label="角色" value={isSuperAdmin ? "—" : assignments.length} hint={isSuperAdmin ? "超管不经过角色" : "已分配"} icon={<KeyRound className="size-3.5" />} />
          <StatTile label="权限组" value={loading ? "…" : groups.length} hint="覆盖的功能域" icon={<Layers className="size-3.5" />} />
          <StatTile
            label="已授予权限"
            value={loading ? "…" : granted}
            hint={total ? `共 ${total} 项中` : undefined}
            tone={granted > 0 ? "success" : "default"}
            icon={<Sparkles className="size-3.5" />}
          />
        </div>

        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-11 w-full rounded-lg" />
            <Skeleton className="h-11 w-full rounded-lg" />
          </div>
        ) : assignments.length === 0 ? (
          <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
            {isSuperAdmin
              ? "没有角色分配 —— 超级管理员本来就不需要。"
              : "还没有分配任何角色，因此除了自助能力之外，你看不到其它管理页面。"}
          </p>
        ) : (
          <div className="space-y-1.5">
            {assignments.map((assignment, index) => {
              const role = (roleTree || []).find((item) => item.key === assignment.roleKey);
              const scoped = assignment.appid != null;
              return (
                <div
                  key={`${assignment.roleKey}-${assignment.appid ?? "global"}-${index}`}
                  className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{role?.name || assignment.roleKey}</p>
                    {/* 角色目录里查不到时，name 会退回 key —— 那就别把同一串字重复两遍 */}
                    {role?.name ? (
                      <p className="truncate font-mono text-[11px] text-muted-foreground">{assignment.roleKey}</p>
                    ) : null}
                  </div>
                  <Badge variant={scoped ? "secondary" : "info"} size="sm" className="shrink-0">
                    {scoped ? assignment.appName || `应用 ${assignment.appid}` : "全局作用域"}
                  </Badge>
                </div>
              );
            })}
          </div>
        )}
      </Panel>

      {groups.length > 0 ? (
        <Collapsible open={open} onOpenChange={setOpen}>
          <Panel
            title="权限明细"
            icon={<Layers className="size-4" />}
            description="按功能域列出你实际生效的权限点。多个角色叠加时，任一角色授予即为授予。"
            action={
              <CollapsibleTrigger asChild>
                <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs">
                  {open ? "收起" : `展开 ${groups.length} 组`}
                  <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
                </Button>
              </CollapsibleTrigger>
            }
            bodyClassName="px-5 py-0"
          >
            <CollapsibleContent>
              <div className="py-4">
                <PermissionTree groups={groups} compact />
              </div>
            </CollapsibleContent>
            {!open ? (
              <div className="flex flex-wrap gap-1.5 py-4">
                {groups.map((group) => {
                  const count = group.permissions.filter((item) => item.description === "granted").length;
                  return (
                    <Badge
                      key={group.key}
                      variant={count > 0 ? "secondary" : "outline"}
                      size="sm"
                      className={cn("gap-1", count === 0 && "text-muted-foreground/60")}
                    >
                      {group.name}
                      <span className="tabular-nums opacity-70">
                        {count}/{group.permissions.length}
                      </span>
                    </Badge>
                  );
                })}
              </div>
            ) : null}
          </Panel>
        </Collapsible>
      ) : null}
    </div>
  );
}
