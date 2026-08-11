"use client";

import { useMemo } from "react";
import { Check, Minus } from "lucide-react";
import { useRoleMatrixQuery } from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { LoadingState, EmptyState } from "@/components/ui/data-state";
import { cn } from "@/lib/utils";

export function RoleMatrixEditor() {
  const { data: matrix, isLoading } = useRoleMatrixQuery();

  const groupedRows = useMemo(() => {
    if (!matrix) return [];
    const groups: Array<{ key: string; name: string; rows: typeof matrix.rows }> = [];
    let current: (typeof groups)[0] | null = null;
    for (const row of matrix.rows) {
      if (!current || current.key !== row.groupKey) {
        current = { key: row.groupKey, name: row.groupName, rows: [] };
        groups.push(current);
      }
      current.rows.push(row);
    }
    return groups;
  }, [matrix]);

  if (isLoading) return <LoadingState title="加载权限矩阵" description="" />;
  if (!matrix) return <EmptyState title="无数据" description="" />;

  const roles = matrix.roles;

  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full text-xs">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="sticky left-0 z-10 bg-muted/50 px-3 py-2.5 text-left font-semibold min-w-[200px]">权限</th>
            {roles.map((r) => (
              <th key={r.key} className="px-2 py-2.5 text-center font-semibold min-w-[90px]">
                <div className="space-y-1">
                  <div className="truncate">{r.name}</div>
                  <Badge variant={r.isCustom ? "outline" : "secondary"} className="text-[9px]">
                    {r.isCustom ? "自定义" : r.scope === "global" ? "全局" : "应用"}
                  </Badge>
                </div>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {groupedRows.map((group) => (
            <GroupSection key={group.key} group={group} roles={roles} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function GroupSection({ group, roles }: {
  group: { key: string; name: string; rows: Array<{ permissionCode: string; permissionName: string; grants: Record<string, boolean> }> };
  roles: Array<{ key: string }>;
}) {
  return (
    <>
      <tr className="border-b bg-muted/30">
        <td colSpan={roles.length + 1} className="sticky left-0 px-3 py-1.5 text-[10px] font-bold uppercase tracking-widest text-muted-foreground">
          {group.name}
        </td>
      </tr>
      {group.rows.map((row) => (
        <tr key={row.permissionCode} className="border-b hover:bg-muted/20 transition-colors">
          <td className="sticky left-0 z-10 bg-card px-3 py-2 font-medium">
            <div>{row.permissionName}</div>
            <div className="text-[10px] text-muted-foreground font-mono">{row.permissionCode}</div>
          </td>
          {roles.map((r) => (
            <td key={r.key} className="px-2 py-2 text-center">
              {row.grants[r.key] ? (
                <Check className={cn("mx-auto size-4", "text-emerald-600 dark:text-emerald-400")} />
              ) : (
                <Minus className="mx-auto size-3.5 text-muted-foreground/30" />
              )}
            </td>
          ))}
        </tr>
      ))}
    </>
  );
}
