"use client";

import { useMemo, useState } from "react";
import { Check, Loader2, Search, X } from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useAssignableAdminsQuery, useOrgMetadataQuery } from "@/lib/org-hooks";
import type { DepartmentNode, OrgAccess, OrgMember } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * 权限闸门：按钮显隐一律读后端下发的 permissions，不在前端按角色推断。
 * 前端自己算必然与后端漂移，用户会遇到「点了才 403」。
 */
export function useOrgCan(access?: OrgAccess | null) {
  return useMemo(() => {
    const set = new Set(access?.permissions ?? []);
    return (permission: string) => set.has(permission);
  }, [access?.permissions]);
}

/** 组织角色徽标 */
export function OrgRoleBadge({ role }: { role?: string }) {
  const { data: meta } = useOrgMetadataQuery();
  if (!role) return null;
  const label = meta?.builtinRoles.find((r) => r.key === role)?.name ?? role;
  const variant =
    role === "owner" ? "success" as const :
    role === "admin" ? "info" as const :
    role === "manager" ? "warning" as const : "outline" as const;
  return <Badge variant={variant} className="text-[9px]">{label}</Badge>;
}

/** 组织 / 成员状态徽标 */
export function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; variant: "success" | "warning" | "danger" | "outline" }> = {
    active: { label: "正常", variant: "success" },
    suspended: { label: "停用", variant: "warning" },
    archived: { label: "已归档", variant: "outline" },
    left: { label: "已离开", variant: "danger" },
  };
  const item = map[status] ?? { label: status, variant: "outline" as const };
  return <Badge variant={item.variant} className="text-[9px]">{item.label}</Badge>;
}

/** 把部门树摊平成带缩进标签的选项，供各处下拉复用 */
export function flattenDeptOptions(nodes: DepartmentNode[], depth = 0): { id: string; label: string; name: string }[] {
  return nodes.flatMap((node) => [
    { id: node.id, label: `${"　".repeat(depth)}${node.name}`, name: node.name },
    ...flattenDeptOptions(node.children, depth + 1),
  ]);
}

/**
 * 成员选择器：搜索平台管理员并多选。
 *
 * 旧界面让操作者手输管理员数字 ID —— 没人记得住 ID，实际使用中只能先去
 * 管理员列表页翻一遍再回来抄。
 */
export function AdminPicker({
  orgId, selected, onChange, placeholder = "搜索账号 / 姓名 / 邮箱",
}: {
  orgId: string;
  selected: OrgMember[];
  onChange: (next: OrgMember[]) => void;
  placeholder?: string;
}) {
  const [keyword, setKeyword] = useState("");
  const { data: candidates, isFetching } = useAssignableAdminsQuery(orgId, keyword);

  const selectedIds = new Set(selected.map((m) => m.adminId));
  const toggle = (member: OrgMember) => {
    if (selectedIds.has(member.adminId)) {
      onChange(selected.filter((m) => m.adminId !== member.adminId));
    } else {
      onChange([...selected, member]);
    }
  };

  return (
    <div className="space-y-2">
      <div className="relative">
        <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          placeholder={placeholder}
          className="h-8 pl-8 text-xs"
        />
        {isFetching && <Loader2 className="absolute right-2.5 top-1/2 size-3.5 -translate-y-1/2 animate-spin text-muted-foreground" />}
      </div>

      {selected.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {selected.map((m) => (
            <Badge key={m.adminId} variant="outline" className="gap-1 pr-1 text-[10px]">
              {m.displayName || m.account}
              <button type="button" onClick={() => toggle(m)} className="rounded-full hover:bg-muted">
                <X className="size-2.5" />
              </button>
            </Badge>
          ))}
        </div>
      )}

      <ScrollArea className="h-44 rounded-lg border">
        <div className="p-1">
          {(candidates ?? []).length === 0 ? (
            <p className="py-6 text-center text-xs text-muted-foreground">
              {keyword ? "没有匹配的管理员" : "输入关键词搜索管理员"}
            </p>
          ) : (
            (candidates ?? []).map((m) => {
              const picked = selectedIds.has(m.adminId);
              return (
                <button
                  key={m.adminId}
                  type="button"
                  onClick={() => toggle(m)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors",
                    picked ? "bg-primary/10" : "hover:bg-muted/50",
                  )}
                >
                  <Avatar className="size-6">
                    <AvatarImage src={m.avatar} />
                    <AvatarFallback className="text-[9px]">{(m.displayName || m.account)[0]}</AvatarFallback>
                  </Avatar>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-xs font-medium">{m.displayName || m.account}</div>
                    <div className="truncate text-[10px] text-muted-foreground">
                      {m.account}{m.email ? ` · ${m.email}` : ""}
                    </div>
                  </div>
                  {picked && <Check className="size-3.5 shrink-0 text-primary" />}
                </button>
              );
            })
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

/** 权限勾选树，由后端下发的权限目录驱动 —— 后端加权限点前端零改动 */
export function PermissionPicker({
  value, onChange, available,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  /** 可授予范围；留空表示不限制（超管视角） */
  available?: string[];
}) {
  const { data: meta } = useOrgMetadataQuery();
  const selected = new Set(value);
  const grantable = available ? new Set(available) : null;

  const toggle = (code: string) => {
    const next = new Set(selected);
    if (next.has(code)) next.delete(code);
    else next.add(code);
    onChange([...next]);
  };

  if (!meta) return <p className="py-4 text-center text-xs text-muted-foreground">加载权限目录…</p>;

  return (
    <div className="space-y-3">
      {meta.permissionCatalog.map((group) => {
        const items = group.permissions.filter((p) => !grantable || grantable.has(p.code));
        if (items.length === 0) return null;
        const allPicked = items.every((p) => selected.has(p.code));
        return (
          <div key={group.key} className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold">{group.name}</span>
              <Button
                type="button" variant="ghost" size="sm" className="h-5 px-1.5 text-[10px]"
                onClick={() => {
                  const next = new Set(selected);
                  items.forEach((p) => (allPicked ? next.delete(p.code) : next.add(p.code)));
                  onChange([...next]);
                }}
              >
                {allPicked ? "取消全选" : "全选"}
              </Button>
            </div>
            <div className="flex flex-wrap gap-1">
              {items.map((p) => (
                <button
                  key={p.code}
                  type="button"
                  onClick={() => toggle(p.code)}
                  className={cn(
                    "rounded-md border px-2 py-1 text-[10px] transition-colors",
                    selected.has(p.code)
                      ? "border-primary bg-primary/10 text-primary"
                      : "border-border text-muted-foreground hover:bg-muted/50",
                  )}
                >
                  {p.name}
                </button>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}

/** 统计小卡 */
export function StatTile({ label, value, hint }: { label: string; value: number | string; hint?: string }) {
  return (
    <div className="rounded-lg border px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-xl font-semibold tabular-nums">{value}</div>
      {hint && <div className="text-[10px] text-muted-foreground">{hint}</div>}
    </div>
  );
}
