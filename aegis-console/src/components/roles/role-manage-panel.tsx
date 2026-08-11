"use client";

import { useState } from "react";
import { Loader2, Plus, Shield, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import {
  useAdminRolesQuery,
  useAdminRolePermissionTreeQuery,
  useCreateCustomRoleMutation,
  useDeleteCustomRoleMutation,
  useRoleImpactPreviewQuery,
} from "@/lib/admin-hooks";
import type { RoleDefinition } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/data-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";

export function RoleManagePanel() {
  const rolesQuery = useAdminRolesQuery();
  const permTreeQuery = useAdminRolePermissionTreeQuery();
  const createMutation = useCreateCustomRoleMutation();
  const deleteMutation = useDeleteCustomRoleMutation();
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedImpact, setSelectedImpact] = useState<string | null>(null);

  if (rolesQuery.isLoading) return <LoadingState title="加载角色" />;
  const roles = rolesQuery.data || [];
  const builtIn = roles.filter((r) => !r.isCustom);
  const custom = roles.filter((r) => r.isCustom);

  const handleDelete = async (roleKey: string) => {
    try {
      await deleteMutation.mutateAsync({ roleKey });
      toast.success("角色已删除");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "删除失败");
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">角色列表</h3>
        <Sheet open={createOpen} onOpenChange={setCreateOpen}>
          <SheetTrigger asChild>
            <Button size="sm"><Plus className="size-3.5" />创建自定义角色</Button>
          </SheetTrigger>
          <SheetContent className="w-[480px] sm:max-w-lg overflow-y-auto">
            <SheetHeader><SheetTitle>创建自定义角色</SheetTitle></SheetHeader>
            <CreateRoleForm
              baseRoles={roles}
              permTree={permTreeQuery.data || []}
              onCreated={() => setCreateOpen(false)}
            />
          </SheetContent>
        </Sheet>
      </div>

      {/* 内置角色 */}
      <div className="space-y-2">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">内置角色</h4>
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {builtIn.map((r) => <RoleCard key={r.key} role={r} />)}
        </div>
      </div>

      <Separator />

      {/* 自定义角色 */}
      <div className="space-y-2">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">自定义角色</h4>
        {custom.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4 text-center">暂无自定义角色</p>
        ) : (
          <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
            {custom.map((r) => (
              <RoleCard key={r.key} role={r} onDelete={() => handleDelete(r.key)} deleting={deleteMutation.isPending} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function RoleCard({ role, onDelete, deleting }: { role: RoleDefinition; onDelete?: () => void; deleting?: boolean }) {
  return (
    <div className="rounded-xl border bg-card p-4 space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Shield className="size-4 text-muted-foreground" />
          <span className="text-sm font-semibold">{role.name}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <Badge variant={role.isCustom ? "outline" : "secondary"} className="text-[9px]">
            {role.isCustom ? "自定义" : "内置"}
          </Badge>
          <Badge variant="outline" className="text-[9px]">Lv.{role.level}</Badge>
        </div>
      </div>
      <p className="text-xs text-muted-foreground line-clamp-2">{role.description || "无描述"}</p>
      <div className="flex items-center justify-between text-[10px] text-muted-foreground">
        <span>{role.scope === "global" ? "全局范围" : "应用范围"} · {role.permissions?.length || 0} 项权限</span>
        {onDelete && (
          <Button variant="ghost" size="sm" className="h-6 px-2 text-destructive hover:text-destructive" onClick={onDelete} disabled={deleting}>
            <Trash2 className="size-3" />
          </Button>
        )}
      </div>
    </div>
  );
}

function CreateRoleForm({ baseRoles, permTree, onCreated }: {
  baseRoles: RoleDefinition[];
  permTree: Array<{ key: string; permissionGroups: Array<{ key: string; name: string; permissions: Array<{ code: string; name: string }> }> }>;
  onCreated: () => void;
}) {
  const createMutation = useCreateCustomRoleMutation();
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [level, setLevel] = useState("10");
  const [scope, setScope] = useState("app");
  const [baseRole, setBaseRole] = useState("");
  const [perms, setPerms] = useState<Set<string>>(new Set());

  // 从基础角色填充权限
  const handleBaseChange = (val: string) => {
    setBaseRole(val);
    if (val) {
      const base = baseRoles.find((r) => r.key === val);
      if (base?.permissions) setPerms(new Set(base.permissions));
    }
  };

  // 收集所有权限
  const allGroups = permTree.length > 0
    ? permTree[0].permissionGroups
    : [];

  const handleSubmit = async () => {
    if (!key || !name) { toast.error("请填写角色标识和名称"); return; }
    const roleKey = key.startsWith("custom_") ? key : `custom_${key}`;
    try {
      await createMutation.mutateAsync({
        roleKey, name, description: desc,
        level: parseInt(level) || 10, scope,
        baseRole: baseRole || undefined,
        permissions: Array.from(perms),
      });
      toast.success("角色创建成功");
      onCreated();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "创建失败");
    }
  };

  return (
    <div className="space-y-4 pt-4">
      <div className="space-y-1.5">
        <Label className="text-xs">角色标识</Label>
        <div className="flex items-center gap-1">
          <span className="text-xs text-muted-foreground font-mono">custom_</span>
          <Input value={key} onChange={(e) => setKey(e.target.value.replace(/[^a-z0-9_]/g, ""))} placeholder="my_role" className="font-mono" />
        </div>
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">名称</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="自定义角色名" />
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">描述</Label>
        <Input value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="角色用途说明" />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <Label className="text-xs">级别 (1-19)</Label>
          <Input type="number" min={1} max={19} value={level} onChange={(e) => setLevel(e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs">范围</Label>
          <Select value={scope} onValueChange={setScope}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="global">全局</SelectItem>
              <SelectItem value="app">应用</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">基于模板（可选）</Label>
        <Select value={baseRole || "none"} onValueChange={(v) => handleBaseChange(v === "none" ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="从零开始" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="none">从零开始</SelectItem>
            {baseRoles.filter((r) => !r.isCustom && r.key !== "super_admin").map((r) => (
              <SelectItem key={r.key} value={r.key}>{r.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <Separator />

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-xs">权限选择</Label>
          <span className="text-[10px] text-muted-foreground">{perms.size} 项已选</span>
        </div>
        <div className="max-h-[300px] overflow-y-auto space-y-3 rounded-lg border p-3">
          {allGroups.map((g) => (
            <div key={g.key} className="space-y-1">
              <div className="text-[10px] font-bold uppercase tracking-widest text-muted-foreground">{g.name}</div>
              {g.permissions.map((p) => (
                <label key={p.code} className="flex items-center gap-2 cursor-pointer hover:bg-muted/50 rounded px-1 py-0.5">
                  <input type="checkbox" className="rounded" checked={perms.has(p.code)}
                    onChange={(e) => {
                      const next = new Set(perms);
                      e.target.checked ? next.add(p.code) : next.delete(p.code);
                      setPerms(next);
                    }} />
                  <span className="text-xs">{p.name}</span>
                  <span className="text-[10px] text-muted-foreground font-mono ml-auto">{p.code}</span>
                </label>
              ))}
            </div>
          ))}
        </div>
      </div>

      <Button className="w-full" onClick={handleSubmit} disabled={createMutation.isPending}>
        {createMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
        创建角色
      </Button>
    </div>
  );
}
