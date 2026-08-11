"use client";

import { FormEvent, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { useAdminAppsQuery, useAdminRolesQuery, useCreateAdminAccountMutation } from "@/lib/admin-hooks";

type AssignmentRow = { roleKey: string; appid: string };

export function AdminCreateDialog() {
  const [open, setOpen] = useState(false);
  const [account, setAccount] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [isSuperAdmin, setIsSuperAdmin] = useState(false);
  const [assignments, setAssignments] = useState<AssignmentRow[]>([{ roleKey: "", appid: "__global__" }]);
  const [error, setError] = useState<string | null>(null);

  const createMutation = useCreateAdminAccountMutation();
  const rolesQuery = useAdminRolesQuery();
  const appsQuery = useAdminAppsQuery();
  const roles = (rolesQuery.data || []).filter((r) => r.key !== "super_admin");
  const apps = appsQuery.data || [];

  function reset() {
    setAccount("");
    setDisplayName("");
    setEmail("");
    setPassword("");
    setIsSuperAdmin(false);
    setAssignments([{ roleKey: "", appid: "__global__" }]);
    setError(null);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    // 超管不需要角色分配
    const finalAssignments = isSuperAdmin ? [] : assignments
      .filter((a) => a.roleKey)
      .map((a) => ({
        roleKey: a.roleKey,
        appid: a.appid && a.appid !== "__global__" ? Number(a.appid) : null
      }));

    try {
      await createMutation.mutateAsync({
        account,
        password,
        displayName,
        email,
        isSuperAdmin,
        assignments: finalAssignments
      });
      reset();
      setOpen(false);
      toast.success("管理员已创建");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) reset(); }}>
      <DialogTrigger asChild>
        <Button size="sm" className="h-8 gap-1 text-xs"><Plus className="size-3.5" />新建</Button>
      </DialogTrigger>
      <DialogContent className="max-w-md">
        <DialogHeader><DialogTitle className="text-sm">新建管理员</DialogTitle></DialogHeader>
        <form className="space-y-4" onSubmit={handleSubmit}>
          {/* 基本信息 */}
          <div className="grid gap-3 sm:grid-cols-2">
            <F label="账号" required><Input className="h-8 text-sm" placeholder="operator_a" value={account} onChange={(e) => setAccount(e.target.value)} required /></F>
            <F label="显示名称"><Input className="h-8 text-sm" placeholder="运营管理员" value={displayName} onChange={(e) => setDisplayName(e.target.value)} /></F>
            <F label="邮箱"><Input className="h-8 text-sm" placeholder="op@example.com" value={email} onChange={(e) => setEmail(e.target.value)} /></F>
            <F label="密码" required><Input className="h-8 text-sm" type="password" placeholder="初始密码" value={password} onChange={(e) => setPassword(e.target.value)} required /></F>
          </div>

          <Separator />

          {/* 超管开关 */}
          <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5">
            <Checkbox checked={isSuperAdmin} onCheckedChange={(v) => setIsSuperAdmin(v === true)} />
            <div>
              <span className="text-xs font-medium">超级管理员</span>
              <p className="text-[10px] text-muted-foreground">拥有所有权限，无需分配角色</p>
            </div>
          </label>

          {/* 角色分配（非超管时显示） */}
          {!isSuperAdmin && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label className="text-[10px] uppercase tracking-wider text-muted-foreground">角色分配</Label>
                <Button type="button" size="sm" variant="outline" className="h-6 gap-1 text-[10px]" onClick={() => setAssignments((prev) => [...prev, { roleKey: "", appid: "__global__" }])}>
                  <Plus className="size-2.5" />添加
                </Button>
              </div>
              {assignments.map((row, i) => {
                const selectedRole = roles.find((r) => r.key === row.roleKey);
                const isAppScope = selectedRole?.scope === "app";
                return (
                  <div key={i} className="flex items-center gap-2">
                    <Select value={row.roleKey || "__none__"} onValueChange={(v) => setAssignments((prev) => prev.map((r, j) => j === i ? { ...r, roleKey: v === "__none__" ? "" : v } : r))}>
                      <SelectTrigger className="h-8 flex-1 text-xs"><SelectValue placeholder="选择角色" /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none__">选择角色</SelectItem>
                        {roles.map((r) => <SelectItem key={r.key} value={r.key}>{r.name} ({r.scope === "global" ? "全局" : "应用"})</SelectItem>)}
                      </SelectContent>
                    </Select>
                    <Select value={row.appid} onValueChange={(v) => setAssignments((prev) => prev.map((r, j) => j === i ? { ...r, appid: v } : r))} disabled={!isAppScope}>
                      <SelectTrigger className="h-8 flex-1 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__global__">全局</SelectItem>
                        {apps.map((a) => <SelectItem key={a.id} value={String(a.id)}>{a.name}</SelectItem>)}
                      </SelectContent>
                    </Select>
                    {assignments.length > 1 && (
                      <Button type="button" size="icon" variant="ghost" className="size-7 shrink-0" onClick={() => setAssignments((prev) => prev.filter((_, j) => j !== i))}>
                        <Trash2 className="size-3" />
                      </Button>
                    )}
                  </div>
                );
              })}
            </div>
          )}

          {error && <p className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">{error}</p>}

          <DialogFooter>
            <Button size="sm" disabled={createMutation.isPending} type="submit">{createMutation.isPending ? "创建中..." : "创建"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function F({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <Label className="text-xs">{label}{required && <span className="text-destructive"> *</span>}</Label>
      {children}
    </div>
  );
}
