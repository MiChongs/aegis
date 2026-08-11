"use client";

import { useEffect, useState } from "react";
import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminAppsQuery, useAdminRolesQuery, useAdminRolePermissionTreeQuery, useUpdateAdminAccountAccessMutation } from "@/lib/admin-hooks";
import { PermissionTree } from "./permission-tree";

type AssignmentRow = { roleKey: string; appid: string };

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  admin: { id: number; account: string; displayName: string; isSuperAdmin: boolean; assignments?: Array<{ roleKey?: string; appid?: number | null }> } | null;
};

export function AdminEditDialog({ open, onOpenChange, admin }: Props) {
  const rolesQuery = useAdminRolesQuery();
  const appsQuery = useAdminAppsQuery();
  const permTreeQuery = useAdminRolePermissionTreeQuery();
  const mutation = useUpdateAdminAccountAccessMutation();

  const roles = rolesQuery.data || [];
  const apps = appsQuery.data || [];

  const [isSuperAdmin, setIsSuperAdmin] = useState(false);
  const [assignments, setAssignments] = useState<AssignmentRow[]>([]);
  const [previewRole, setPreviewRole] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !admin) return;
    setIsSuperAdmin(admin.isSuperAdmin);
    setAssignments(
      (admin.assignments || []).map((a) => ({
        roleKey: a.roleKey || "",
        appid: a.appid != null ? String(a.appid) : "__global__"
      }))
    );
    setPreviewRole(null);
  }, [open, admin]);

  function addRow() {
    setAssignments((prev) => [...prev, { roleKey: "", appid: "__global__" }]);
  }

  function removeRow(index: number) {
    setAssignments((prev) => prev.filter((_, i) => i !== index));
  }

  function updateRow(index: number, key: keyof AssignmentRow, value: string) {
    setAssignments((prev) => prev.map((row, i) => (i === index ? { ...row, [key]: value } : row)));
  }

  async function handleSave() {
    if (!admin) return;
    try {
      await mutation.mutateAsync({
        adminId: admin.id,
        isSuperAdmin,
        assignments: assignments
          .filter((a) => a.roleKey)
          .map((a) => ({
            roleKey: a.roleKey,
            appid: a.appid && a.appid !== "__global__" ? Number(a.appid) : null
          }))
      });
      toast.success("权限已更新");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "更新失败");
    }
  }

  // 获取预览角色的权限树
  const previewRoleData = permTreeQuery.data?.find((r) => r.key === previewRole);

  if (!admin) return null;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[92vw] max-w-lg overflow-y-auto">
        <SheetHeader>
          <SheetTitle className="text-sm">编辑权限 — {admin.displayName || admin.account}</SheetTitle>
        </SheetHeader>

        <div className="space-y-5 pt-3">
          {/* 超管开关 */}
          <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5">
            <Checkbox checked={isSuperAdmin} onCheckedChange={(v) => setIsSuperAdmin(v === true)} />
            <div>
              <span className="text-xs font-medium">超级管理员</span>
              <p className="text-[10px] text-muted-foreground">拥有所有权限，无需分配角色</p>
            </div>
          </label>

          {!isSuperAdmin && (
            <>
              <Separator />

              {/* 角色分配 */}
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <Label className="text-[10px] uppercase tracking-wider text-muted-foreground">角色分配</Label>
                  <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" onClick={addRow}>
                    <Plus className="size-3" />添加
                  </Button>
                </div>

                {assignments.length === 0 && (
                  <p className="py-4 text-center text-xs text-muted-foreground">暂无角色分配，点击"添加"</p>
                )}

                {assignments.map((row, i) => {
                  const selectedRole = roles.find((r) => r.key === row.roleKey);
                  const isAppScope = selectedRole?.scope === "app";

                  return (
                    <div key={i} className="flex items-start gap-2">
                      <div className="flex-1 space-y-1.5">
                        <div className="grid grid-cols-2 gap-2">
                          <Select value={row.roleKey || "__none__"} onValueChange={(v) => { updateRow(i, "roleKey", v === "__none__" ? "" : v); setPreviewRole(v === "__none__" ? null : v); }}>
                            <SelectTrigger className="h-8 text-xs"><SelectValue placeholder="选择角色" /></SelectTrigger>
                            <SelectContent>
                              <SelectItem value="__none__">选择角色</SelectItem>
                              {roles.filter((r) => r.key !== "super_admin").map((r) => (
                                <SelectItem key={r.key} value={r.key}>{r.name} ({r.scope === "global" ? "全局" : "应用"})</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <Select value={row.appid} onValueChange={(v) => updateRow(i, "appid", v)} disabled={!isAppScope}>
                            <SelectTrigger className="h-8 text-xs"><SelectValue placeholder="应用范围" /></SelectTrigger>
                            <SelectContent>
                              <SelectItem value="__global__">全局</SelectItem>
                              {apps.map((app) => <SelectItem key={app.id} value={String(app.id)}>{app.name}</SelectItem>)}
                            </SelectContent>
                          </Select>
                        </div>
                      </div>
                      <Button size="icon" variant="ghost" className="mt-0.5 size-7 text-muted-foreground" onClick={() => removeRow(i)}>
                        <Trash2 className="size-3" />
                      </Button>
                    </div>
                  );
                })}
              </div>

              {/* 权限预览 */}
              {previewRoleData && (
                <>
                  <Separator />
                  <div className="space-y-2">
                    <Label className="text-[10px] uppercase tracking-wider text-muted-foreground">
                      {previewRoleData.name} 权限预览
                    </Label>
                    <div className="rounded-lg border p-2">
                      <PermissionTree groups={previewRoleData.permissionGroups} compact />
                    </div>
                  </div>
                </>
              )}
            </>
          )}

          <Separator />

          <Button size="sm" className="h-8 gap-1 text-xs" disabled={mutation.isPending} onClick={handleSave}>
            {mutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Save className="size-3" />}
            {mutation.isPending ? "保存中..." : "保存权限"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
