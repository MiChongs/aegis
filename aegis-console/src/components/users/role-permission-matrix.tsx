"use client";

import { Fragment } from "react";
import { Check, X } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { RoleWithPermissions } from "@/lib/api/types";

type Props = {
  roles: RoleWithPermissions[];
};

const roleLabels: Record<string, string> = {
  super_admin: "超管",
  platform_admin: "平台",
  app_admin: "应用管理",
  app_operator: "应用运营",
  app_auditor: "应用审计",
  app_viewer: "应用查看"
};

export function RolePermissionMatrix({ roles }: Props) {
  if (!roles.length) return null;

  // 从第一个角色获取全部权限分组（所有角色的分组结构相同）
  const groups = roles[0]?.permissionGroups || [];

  // 构建权限→角色授权矩阵
  const rolePermMap = new Map<string, Set<string>>();
  for (const role of roles) {
    const granted = new Set<string>();
    for (const g of role.permissionGroups) {
      for (const p of g.permissions) {
        if (p.description === "granted") granted.add(p.code);
      }
    }
    rolePermMap.set(role.key, granted);
  }

  return (
    <div className="overflow-hidden rounded-xl border">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="sticky left-0 z-10 bg-background">权限</TableHead>
              {roles.map((r) => (
                <TableHead key={r.key} className="text-center">
                  <div className="text-[10px]">{roleLabels[r.key] || r.name}</div>
                  <div className="text-[9px] text-muted-foreground">{r.scope === "global" ? "全局" : "应用"}</div>
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => (
              <Fragment key={group.key}>
                {/* 分组标题行 */}
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={roles.length + 1} className="bg-muted/30 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {group.name}
                  </TableCell>
                </TableRow>
                {/* 权限行 */}
                {group.permissions.map((perm) => (
                  <TableRow key={perm.code}>
                    <TableCell className="sticky left-0 z-10 bg-background py-1 text-[11px]">{perm.name}</TableCell>
                    {roles.map((r) => {
                      const granted = rolePermMap.get(r.key)?.has(perm.code) ?? false;
                      return (
                        <TableCell key={r.key} className="py-1 text-center">
                          {granted ? (
                            <Check className="mx-auto size-3.5 text-emerald-500" />
                          ) : (
                            <X className="mx-auto size-3.5 text-zinc-200 dark:text-zinc-700" />
                          )}
                        </TableCell>
                      );
                    })}
                  </TableRow>
                ))}
              </Fragment>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
