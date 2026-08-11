"use client";

import { useState } from "react";
import {
  Briefcase, CheckCircle2, ClipboardCheck, Link2, Loader2, Plus,
  Shield, Trash2, Users, XCircle,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { ApprovalStep, DepartmentNode, OrgAccess, OrgMember, OrgRole } from "@/lib/api/types";
import {
  useApprovalChainsQuery, useApprovalInstancesQuery, useBindOrgAppMutation,
  useCollabGroupsQuery, useCreateApprovalChainMutation, useCreateCollabGroupMutation,
  useCreateOrgRoleMutation, useCreatePermTemplateMutation, useDecideApprovalMutation,
  useDeleteApprovalChainMutation, useDeleteCollabGroupMutation, useDeleteOrgRoleMutation,
  useDeletePermTemplateMutation, useDepartmentTreeQuery, useGrantOrgRoleMutation,
  useMyPendingApprovalsQuery, useOrgAppsQuery, useOrgMetadataQuery, useOrgRoleGrantsQuery,
  useOrgRolesQuery, useOrgPermTemplatesQuery, useApplyPermTemplateMutation,
  useRevokeOrgRoleMutation, useUnbindOrgAppMutation, useUpdateApprovalChainMutation,
  useUpdateOrgRoleMutation, useCreatePositionMutation, useDeletePositionMutation,
  usePositionsQuery, useUpdatePositionMutation,
} from "@/lib/org-hooks";
import { AdminPicker, flattenDeptOptions, PermissionPicker, useOrgCan } from "./org-shared";

function errMsg(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback;
}

/* ================================================================== */
/*  组织角色与权限                                                     */
/* ================================================================== */

export function OrgRolesPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  const rolesQuery = useOrgRolesQuery(orgId);
  const deleteMutation = useDeleteOrgRoleMutation();
  const [editing, setEditing] = useState<OrgRole | null | "new">(null);
  const [granting, setGranting] = useState<OrgRole | null>(null);

  const roles = rolesQuery.data?.roles ?? [];
  const builtins = rolesQuery.data?.builtinRoles ?? [];
  const canWrite = can("org:role:write");

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardContent className="space-y-3 p-4">
          <h3 className="flex items-center gap-2 text-sm font-semibold"><Shield className="size-4" />内置角色</h3>
          <p className="text-[11px] text-muted-foreground">
            内置角色由平台定义、全组织一致，不可修改。需要更细的划分请创建自定义角色。
          </p>
          <div className="space-y-2">
            {builtins.map((r) => (
              <div key={r.key} className="rounded-lg border px-3 py-2">
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-medium">{r.name}</span>
                  <Badge variant="outline" className="font-mono text-[9px]">{r.key}</Badge>
                  <Badge variant="outline" className="text-[9px] tabular-nums">Lv.{r.level}</Badge>
                </div>
                <p className="mt-0.5 text-[10px] text-muted-foreground">{r.description}</p>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3 p-4">
          <div className="flex items-center justify-between">
            <h3 className="flex items-center gap-2 text-sm font-semibold"><Shield className="size-4" />自定义角色</h3>
            {canWrite && <Button size="sm" onClick={() => setEditing("new")}><Plus className="size-3.5" />新建</Button>}
          </div>

          {roles.length === 0 ? (
            <EmptyState title="暂无自定义角色" description="自定义角色可精确到权限点，并按部门限定管理范围" />
          ) : (
            <div className="space-y-2">
              {roles.map((r) => (
                <div key={r.id} className="group rounded-lg border px-3 py-2">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium">{r.name}</span>
                    <Badge variant="outline" className="font-mono text-[9px]">{r.roleKey}</Badge>
                    <Badge variant="outline" className="text-[9px] tabular-nums">{r.permissions.length} 项权限</Badge>
                    <Badge variant="outline" className="text-[9px] tabular-nums">{r.memberCount} 人</Badge>
                    <div className="ml-auto hidden items-center gap-1 group-hover:flex">
                      {canWrite && (
                        <>
                          <Button size="sm" variant="ghost" className="h-6 px-1.5 text-[10px]" onClick={() => setGranting(r)}>
                            授予
                          </Button>
                          <Button size="sm" variant="ghost" className="h-6 px-1.5 text-[10px]" onClick={() => setEditing(r)}>
                            编辑
                          </Button>
                          <Button
                            size="sm" variant="ghost" className="h-6 px-1.5 text-destructive"
                            onClick={async () => {
                              try {
                                await deleteMutation.mutateAsync({ orgId, roleId: r.id });
                                toast.success("角色已删除");
                              } catch (e) { toast.error(errMsg(e, "删除失败")); }
                            }}
                          >
                            <Trash2 className="size-3" />
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                  {r.description && <p className="mt-0.5 text-[10px] text-muted-foreground">{r.description}</p>}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {editing && (
        <RoleDialog
          orgId={orgId} access={access}
          role={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}
      {granting && <GrantRoleDialog orgId={orgId} role={granting} onClose={() => setGranting(null)} />}
    </div>
  );
}

function RoleDialog({
  orgId, role, access, onClose,
}: { orgId: string; role: OrgRole | null; access?: OrgAccess | null; onClose: () => void }) {
  const createMutation = useCreateOrgRoleMutation();
  const updateMutation = useUpdateOrgRoleMutation();
  const [roleKey, setRoleKey] = useState(role?.roleKey ?? "");
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [permissions, setPermissions] = useState<string[]>(role?.permissions ?? []);

  const pending = createMutation.isPending || updateMutation.isPending;

  const submit = async () => {
    if (!name.trim() || !roleKey.trim()) { toast.error("请填写角色名称与标识"); return; }
    if (permissions.length === 0) { toast.error("请至少选择一项权限"); return; }
    const payload = { roleKey, name, description, permissions };
    try {
      if (role) await updateMutation.mutateAsync({ orgId, roleId: role.id, payload });
      else await createMutation.mutateAsync({ orgId, payload });
      toast.success(role ? "角色已更新" : "角色已创建");
      onClose();
    } catch (e) { toast.error(errMsg(e, "保存失败")); }
  };

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader><DialogTitle>{role ? "编辑角色" : "新建组织角色"}</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">角色名称</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="人事专员" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">角色标识</Label>
              <Input
                value={roleKey} className="font-mono" disabled={Boolean(role)}
                onChange={(e) => setRoleKey(e.target.value.replace(/[^a-zA-Z0-9_]/g, ""))}
                placeholder="hr_specialist"
              />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">说明</Label>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
          </div>
          <Separator />
          <div className="space-y-2">
            <Label className="text-xs font-semibold">权限（{permissions.length} 项）</Label>
            <p className="text-[10px] text-muted-foreground">
              只能授予你自己具备的权限 —— 否则可以造一个超出自身权限的角色再授给自己。
            </p>
            {/* 可授予范围就是自己的权限集，与服务端的校验同一份依据 */}
            <PermissionPicker value={permissions} onChange={setPermissions} available={access?.permissions} />
          </div>
          <Button className="w-full" onClick={submit} disabled={pending}>
            {pending && <Loader2 className="size-3.5 animate-spin" />} 保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function GrantRoleDialog({ orgId, role, onClose }: { orgId: string; role: OrgRole; onClose: () => void }) {
  const grantMutation = useGrantOrgRoleMutation();
  const revokeMutation = useRevokeOrgRoleMutation();
  const grantsQuery = useOrgRoleGrantsQuery(orgId, role.id);
  const treeQuery = useDepartmentTreeQuery(orgId);
  const [picked, setPicked] = useState<OrgMember[]>([]);
  const [scopeDept, setScopeDept] = useState("__all__");

  const grants = grantsQuery.data ?? [];
  const options = flattenDeptOptions(treeQuery.data ?? []);

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader><DialogTitle>授予角色「{role.name}」</DialogTitle></DialogHeader>
        <div className="space-y-3">
          {/* 选择器不排除已在组织内的人 —— 授予角色的对象本来就必须是组织成员 */}
          <AdminPicker orgId={orgId} selected={picked} onChange={setPicked} placeholder="搜索组织成员" />

          <div className="space-y-1.5">
            <Label className="text-xs">管理范围</Label>
            <Select value={scopeDept} onValueChange={setScopeDept}>
              <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">整个组织</SelectItem>
                {options.map((o) => <SelectItem key={o.id} value={o.id}>{o.label} 及其子部门</SelectItem>)}
              </SelectContent>
            </Select>
            <p className="text-[10px] text-muted-foreground">
              限定到部门后，该角色的权限只在这棵子树内生效
            </p>
          </div>

          <Button
            className="w-full" disabled={grantMutation.isPending || picked.length === 0}
            onClick={async () => {
              try {
                const result = await grantMutation.mutateAsync({
                  orgId, roleId: role.id,
                  adminIds: picked.map((m) => m.adminId),
                  scopeDeptId: scopeDept === "__all__" ? undefined : scopeDept,
                });
                toast.success(`已授予 ${result.granted} 人`);
                setPicked([]);
              } catch (e) { toast.error(errMsg(e, "授予失败")); }
            }}
          >
            {grantMutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 授予
          </Button>

          <Separator />

          <div className="space-y-1.5">
            <Label className="text-xs font-semibold">已授予（{grants.length}）</Label>
            {grants.length === 0 ? (
              <p className="py-4 text-center text-xs text-muted-foreground">尚未授予任何人</p>
            ) : (
              <div className="max-h-48 space-y-1 overflow-y-auto">
                {grants.map((g) => (
                  <div key={`${g.adminId}-${g.scopeDept?.id ?? "all"}`} className="flex items-center gap-2 rounded-md border px-2 py-1.5">
                    <span className="flex-1 truncate text-xs">{g.displayName || g.account}</span>
                    <Badge variant="outline" className="text-[9px]">
                      {g.scopeDept ? g.scopeDept.name : "整个组织"}
                    </Badge>
                    <Button
                      size="sm" variant="ghost" className="h-6 px-1.5 text-destructive"
                      onClick={async () => {
                        try {
                          await revokeMutation.mutateAsync({ orgId, roleId: role.id, adminId: g.adminId });
                          toast.success("已撤销");
                        } catch (e) { toast.error(errMsg(e, "撤销失败")); }
                      }}
                    >
                      <Trash2 className="size-3" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ================================================================== */
/*  岗位                                                               */
/* ================================================================== */

export function PositionsPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  const query = usePositionsQuery(orgId);
  const createMutation = useCreatePositionMutation();
  const updateMutation = useUpdatePositionMutation();
  const deleteMutation = useDeletePositionMutation();
  const [editing, setEditing] = useState<{ id?: string; name: string; code: string; level: number; description: string } | null>(null);

  const positions = query.data ?? [];
  const canWrite = can("org:position:write");

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold"><Briefcase className="size-4" />岗位</h3>
          {canWrite && (
            <Button size="sm" onClick={() => setEditing({ name: "", code: "", level: 1, description: "" })}>
              <Plus className="size-3.5" />新建岗位
            </Button>
          )}
        </div>

        {positions.length === 0 ? (
          <EmptyState title="暂无岗位" description="岗位用于标记成员在部门中的职级，可在部门成员详情里分配" />
        ) : (
          <div className="space-y-2">
            {positions.map((p) => (
              <div key={p.id} className="group flex items-center gap-3 rounded-lg border px-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium">{p.name}</span>
                    <Badge variant="outline" className="font-mono text-[9px]">{p.code}</Badge>
                    <Badge variant="outline" className="text-[9px] tabular-nums">Lv.{p.level}</Badge>
                    <Badge variant="outline" className="text-[9px] tabular-nums">{p.memberCount} 人</Badge>
                  </div>
                  {p.description && <p className="text-[10px] text-muted-foreground">{p.description}</p>}
                </div>
                {canWrite && (
                  <div className="hidden items-center gap-1 group-hover:flex">
                    <Button
                      size="sm" variant="ghost" className="h-6 px-1.5 text-[10px]"
                      onClick={() => setEditing({ id: p.id, name: p.name, code: p.code, level: p.level, description: p.description })}
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm" variant="ghost" className="h-6 px-1.5 text-destructive"
                      onClick={async () => {
                        try {
                          await deleteMutation.mutateAsync({ orgId, posId: p.id });
                          toast.success("岗位已删除，持有该岗位的成员会自动解除");
                        } catch (e) { toast.error(errMsg(e, "删除失败")); }
                      }}
                    >
                      <Trash2 className="size-3" />
                    </Button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {editing && (
          <Dialog open onOpenChange={(o) => { if (!o) setEditing(null); }}>
            <DialogContent>
              <DialogHeader><DialogTitle>{editing.id ? "编辑岗位" : "新建岗位"}</DialogTitle></DialogHeader>
              <div className="space-y-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label className="text-xs">名称</Label>
                    <Input value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} placeholder="高级工程师" />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-xs">代码</Label>
                    <Input
                      value={editing.code} className="font-mono"
                      onChange={(e) => setEditing({ ...editing, code: e.target.value.replace(/[^a-zA-Z0-9\-_]/g, "") })}
                      placeholder="senior-eng"
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">职级</Label>
                  <Input
                    inputMode="numeric" value={String(editing.level)}
                    onChange={(e) => setEditing({ ...editing, level: Number(e.target.value.replace(/\D/g, "")) || 0 })}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">说明</Label>
                  <Textarea value={editing.description} onChange={(e) => setEditing({ ...editing, description: e.target.value })} rows={2} />
                </div>
                <Button
                  className="w-full" disabled={createMutation.isPending || updateMutation.isPending}
                  onClick={async () => {
                    if (!editing.name.trim() || !editing.code.trim()) { toast.error("请填写名称与代码"); return; }
                    const payload = { name: editing.name, code: editing.code, level: editing.level, description: editing.description };
                    try {
                      if (editing.id) await updateMutation.mutateAsync({ orgId, posId: editing.id, payload });
                      else await createMutation.mutateAsync({ orgId, payload });
                      toast.success("已保存");
                      setEditing(null);
                    } catch (e) { toast.error(errMsg(e, "保存失败")); }
                  }}
                >
                  保存
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
      </CardContent>
    </Card>
  );
}

/* ================================================================== */
/*  审批                                                               */
/* ================================================================== */

export function ApprovalsPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  const chainsQuery = useApprovalChainsQuery(orgId);
  const instancesQuery = useApprovalInstancesQuery(orgId, { limit: 20 });
  const pendingQuery = useMyPendingApprovalsQuery();
  const deleteMutation = useDeleteApprovalChainMutation();
  const updateMutation = useUpdateApprovalChainMutation();
  const decideMutation = useDecideApprovalMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [creating, setCreating] = useState(false);

  const chains = chainsQuery.data ?? [];
  const instances = instancesQuery.data?.items ?? [];
  const pending = pendingQuery.data ?? [];
  const canManage = can("org:approval:manage");

  const triggerLabel = (t: string) => meta?.approvalTriggers.find((x) => x.value === t)?.label ?? t;
  const statusLabels: Record<string, string> = {
    pending: "待审批", approved: "已通过", rejected: "已驳回", cancelled: "已取消",
  };

  const decide = async (instanceId: string, action: "approved" | "rejected") => {
    try {
      await decideMutation.mutateAsync({ instanceId, action });
      toast.success(action === "approved" ? "已通过" : "已驳回");
    } catch (e) { toast.error(errMsg(e, "操作失败")); }
  };

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardContent className="space-y-3 p-4">
          <div className="flex items-center justify-between">
            <h3 className="flex items-center gap-2 text-sm font-semibold"><ClipboardCheck className="size-4" />审批链</h3>
            {canManage && <Button size="sm" onClick={() => setCreating(true)}><Plus className="size-3.5" />新建</Button>}
          </div>
          <p className="text-[11px] text-muted-foreground">
            同一触发场景只能有一条启用中的审批链，避免「哪条生效」取决于查询顺序。
          </p>

          {chains.length === 0 ? (
            <EmptyState title="暂无审批链" description="为成员加入、部门变更等场景配置审批流程" />
          ) : (
            <div className="space-y-2">
              {chains.map((c) => (
                <div key={c.id} className="group rounded-lg border px-3 py-2">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium">{c.name}</span>
                    <Badge variant="outline" className="text-[9px]">{triggerLabel(c.triggerType)}</Badge>
                    <Badge variant={c.isActive ? "success" : "outline"} className="text-[9px]">
                      {c.isActive ? "启用" : "停用"}
                    </Badge>
                    <div className="ml-auto hidden items-center gap-1.5 group-hover:flex">
                      {canManage && (
                        <>
                          <Switch
                            checked={c.isActive}
                            onCheckedChange={async (v) => {
                              try {
                                await updateMutation.mutateAsync({ orgId, chainId: c.id, payload: { isActive: v } });
                                toast.success(v ? "已启用" : "已停用");
                              } catch (e) { toast.error(errMsg(e, "操作失败")); }
                            }}
                          />
                          <Button
                            size="sm" variant="ghost" className="h-6 px-1.5 text-destructive"
                            onClick={async () => {
                              try {
                                await deleteMutation.mutateAsync({ orgId, chainId: c.id });
                                toast.success("已删除");
                              } catch (e) { toast.error(errMsg(e, "删除失败")); }
                            }}
                          >
                            <Trash2 className="size-3" />
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    {c.steps.map((s, i) => (
                      <Badge key={i} variant="outline" className="text-[9px]">
                        {i + 1}. {meta?.approverTypes.find((t) => t.value === s.approverType)?.label ?? s.approverType}
                      </Badge>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3 p-4">
          {pending.length > 0 && (
            <>
              <h3 className="flex items-center gap-2 text-sm font-semibold">
                <ClipboardCheck className="size-4" />待我审批
                <Badge variant="danger" className="text-[9px]">{pending.length}</Badge>
              </h3>
              <div className="space-y-2">
                {pending.map((inst) => (
                  <div key={inst.id} className="flex items-center gap-3 rounded-lg border px-3 py-2">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5">
                        <span className="text-sm font-medium">{triggerLabel(inst.triggerType)}</span>
                        <Badge variant="outline" className="text-[9px] tabular-nums">
                          步骤 {inst.currentStep + 1}/{inst.totalSteps}
                        </Badge>
                      </div>
                      <span className="text-[10px] text-muted-foreground">
                        {inst.requesterName ? `${inst.requesterName} 发起 · ` : ""}
                        {new Date(inst.createdAt).toLocaleString("zh-CN")}
                      </span>
                    </div>
                    <div className="flex gap-1">
                      <Button size="sm" className="h-7 text-xs" disabled={decideMutation.isPending}
                        onClick={() => decide(inst.id, "approved")}>
                        <CheckCircle2 className="size-3" />通过
                      </Button>
                      <Button size="sm" variant="outline" className="h-7 text-xs" disabled={decideMutation.isPending}
                        onClick={() => decide(inst.id, "rejected")}>
                        <XCircle className="size-3" />驳回
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
              <Separator />
            </>
          )}

          <h3 className="text-sm font-semibold">审批记录</h3>
          {instances.length === 0 ? (
            <p className="py-8 text-center text-xs text-muted-foreground">暂无审批记录</p>
          ) : (
            <div className="space-y-2">
              {instances.map((inst) => (
                <div key={inst.id} className="rounded-lg border px-3 py-2">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium">{triggerLabel(inst.triggerType)}</span>
                    <Badge
                      variant={inst.status === "approved" ? "success" : inst.status === "rejected" ? "danger" : "outline"}
                      className="text-[9px]"
                    >
                      {statusLabels[inst.status] ?? inst.status}
                    </Badge>
                  </div>
                  <div className="text-[10px] text-muted-foreground">
                    {inst.requesterName ? `${inst.requesterName} · ` : ""}
                    {new Date(inst.createdAt).toLocaleString("zh-CN")}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {creating && <ApprovalChainDialog orgId={orgId} onClose={() => setCreating(false)} />}
    </div>
  );
}

function ApprovalChainDialog({ orgId, onClose }: { orgId: string; onClose: () => void }) {
  const mutation = useCreateApprovalChainMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [name, setName] = useState("");
  const [triggerType, setTriggerType] = useState("member_join");
  const [steps, setSteps] = useState<ApprovalStep[]>([
    { approverType: "leader", approverId: 0, order: 1 },
  ]);

  const submit = async () => {
    if (!name.trim()) { toast.error("请填写审批链名称"); return; }
    try {
      await mutation.mutateAsync({ orgId, payload: { name, triggerType, steps } });
      toast.success("审批链已创建");
      onClose();
    } catch (e) { toast.error(errMsg(e, "创建失败")); }
  };

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader><DialogTitle>新建审批链</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label className="text-xs">名称</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="新成员入职审批" />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">触发场景</Label>
            <Select value={triggerType} onValueChange={setTriggerType}>
              <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {(meta?.approvalTriggers ?? []).map((t) => (
                  <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-xs font-semibold">审批步骤</Label>
              <Button
                size="sm" variant="ghost" className="h-6 px-2 text-[10px]"
                onClick={() => setSteps([...steps, { approverType: "leader", approverId: 0, order: steps.length + 1 }])}
              >
                <Plus className="size-3" />加一步
              </Button>
            </div>
            {steps.map((step, i) => (
              <div key={i} className="flex items-center gap-2 rounded-lg border px-2 py-1.5">
                <span className="text-[10px] text-muted-foreground">{i + 1}</span>
                <Select
                  value={step.approverType}
                  onValueChange={(v) => {
                    const next = [...steps];
                    next[i] = { ...next[i], approverType: v, approverId: 0, approverRole: v === "org_role" ? "admin" : undefined };
                    setSteps(next);
                  }}
                >
                  <SelectTrigger className="h-8 flex-1 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {(meta?.approverTypes ?? []).map((t) => (
                      <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                {step.approverType === "org_role" && (
                  <Select
                    value={step.approverRole ?? "admin"}
                    onValueChange={(v) => {
                      const next = [...steps];
                      next[i] = { ...next[i], approverRole: v };
                      setSteps(next);
                    }}
                  >
                    <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {(meta?.builtinRoles ?? []).map((r) => (
                        <SelectItem key={r.key} value={r.key}>{r.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                {step.approverType === "admin" && (
                  <Input
                    inputMode="numeric" className="h-8 w-24 text-xs" placeholder="管理员 ID"
                    value={step.approverId ? String(step.approverId) : ""}
                    onChange={(e) => {
                      const next = [...steps];
                      next[i] = { ...next[i], approverId: Number(e.target.value.replace(/\D/g, "")) || 0 };
                      setSteps(next);
                    }}
                  />
                )}

                {steps.length > 1 && (
                  <Button
                    size="sm" variant="ghost" className="h-7 px-1.5 text-destructive"
                    onClick={() => setSteps(steps.filter((_, idx) => idx !== i).map((s, idx) => ({ ...s, order: idx + 1 })))}
                  >
                    <Trash2 className="size-3" />
                  </Button>
                )}
              </div>
            ))}
            <p className="text-[10px] text-muted-foreground">
              「部门负责人」按申请人所在部门动态解析，无需指定具体的人。
            </p>
          </div>

          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 创建
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ================================================================== */
/*  资源：权限模板 / 应用绑定 / 协作组                                  */
/* ================================================================== */

export function OrgResourcesPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  return (
    <div className="space-y-4">
      <div className="grid gap-4 lg:grid-cols-2">
        <PermTemplateCard orgId={orgId} canWrite={can("org:role:write")} />
        <OrgAppsCard orgId={orgId} canBind={can("org:app:bind")} isPlatform={access?.isPlatformAdmin || access?.isSuperAdmin} />
      </div>
      <CollabGroupsCard orgId={orgId} canWrite={can("org:dept:write")} />
    </div>
  );
}

function PermTemplateCard({ orgId, canWrite }: { orgId: string; canWrite: boolean }) {
  const query = useOrgPermTemplatesQuery(orgId);
  const createMutation = useCreatePermTemplateMutation();
  const deleteMutation = useDeletePermTemplateMutation();
  const applyMutation = useApplyPermTemplateMutation();
  const [creating, setCreating] = useState(false);
  const [applying, setApplying] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [permissions, setPermissions] = useState<string[]>([]);
  const [picked, setPicked] = useState<OrgMember[]>([]);

  const templates = query.data ?? [];

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold"><Shield className="size-4" />权限模板</h3>
          {canWrite && <Button size="sm" onClick={() => setCreating(true)}><Plus className="size-3.5" />新建</Button>}
        </div>
        <p className="text-[11px] text-muted-foreground">
          套用模板会生成一个同名组织角色并授予选中的成员 —— 模板本身不直接持有权限。
        </p>

        {templates.length === 0 ? (
          <EmptyState title="暂无模板" description="把常用的权限组合存为模板，批量授予时省去逐项勾选" />
        ) : (
          <div className="space-y-2">
            {templates.map((t) => (
              <div key={t.id} className="group rounded-lg border px-3 py-2">
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-medium">{t.name}</span>
                  {t.isDefault && <Badge variant="success" className="text-[9px]">默认</Badge>}
                  <Badge variant="outline" className="text-[9px] tabular-nums">{t.permissions.length} 项</Badge>
                  {canWrite && (
                    <div className="ml-auto hidden items-center gap-1 group-hover:flex">
                      <Button size="sm" variant="ghost" className="h-6 px-1.5 text-[10px]" onClick={() => setApplying(t.id)}>
                        套用
                      </Button>
                      <Button
                        size="sm" variant="ghost" className="h-6 px-1.5 text-destructive"
                        onClick={async () => {
                          try {
                            await deleteMutation.mutateAsync({ orgId, templateId: t.id });
                            toast.success("已删除");
                          } catch (e) { toast.error(errMsg(e, "删除失败")); }
                        }}
                      >
                        <Trash2 className="size-3" />
                      </Button>
                    </div>
                  )}
                </div>
                {t.description && <p className="mt-0.5 text-[10px] text-muted-foreground">{t.description}</p>}
              </div>
            ))}
          </div>
        )}

        {creating && (
          <Dialog open onOpenChange={(o) => { if (!o) setCreating(false); }}>
            <DialogContent className="max-h-[85vh] overflow-y-auto">
              <DialogHeader><DialogTitle>新建权限模板</DialogTitle></DialogHeader>
              <div className="space-y-3">
                <div className="space-y-1.5">
                  <Label className="text-xs">模板名称</Label>
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="人事管理" />
                </div>
                <PermissionPicker value={permissions} onChange={setPermissions} />
                <Button
                  className="w-full" disabled={createMutation.isPending}
                  onClick={async () => {
                    if (!name.trim() || permissions.length === 0) { toast.error("请填写名称并选择权限"); return; }
                    try {
                      await createMutation.mutateAsync({ orgId, payload: { name, permissions } });
                      toast.success("模板已创建");
                      setCreating(false); setName(""); setPermissions([]);
                    } catch (e) { toast.error(errMsg(e, "创建失败")); }
                  }}
                >
                  创建
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}

        {applying && (
          <Dialog open onOpenChange={(o) => { if (!o) { setApplying(null); setPicked([]); } }}>
            <DialogContent>
              <DialogHeader><DialogTitle>套用权限模板</DialogTitle></DialogHeader>
              <div className="space-y-3">
                <AdminPicker orgId={orgId} selected={picked} onChange={setPicked} placeholder="搜索组织成员" />
                <Button
                  className="w-full" disabled={applyMutation.isPending || picked.length === 0}
                  onClick={async () => {
                    try {
                      const result = await applyMutation.mutateAsync({
                        orgId, templateId: applying, adminIds: picked.map((m) => m.adminId),
                      });
                      toast.success(`已套用到 ${result.granted} 人（角色：${result.role.name}）`);
                      setApplying(null); setPicked([]);
                    } catch (e) { toast.error(errMsg(e, "套用失败")); }
                  }}
                >
                  {applyMutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 套用
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
      </CardContent>
    </Card>
  );
}

function OrgAppsCard({ orgId, canBind, isPlatform }: { orgId: string; canBind: boolean; isPlatform?: boolean }) {
  const query = useOrgAppsQuery(orgId);
  const bindMutation = useBindOrgAppMutation();
  const unbindMutation = useUnbindOrgAppMutation();
  const [appId, setAppId] = useState("");
  const [owned, setOwned] = useState(false);

  const bindings = query.data ?? [];

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h3 className="flex items-center gap-2 text-sm font-semibold"><Link2 className="size-4" />应用资源</h3>
        <p className="text-[11px] text-muted-foreground">
          「归属」表示应用属于该组织；「授权」只是允许访问，可跨组织共享。归属只能由平台管理员调整。
        </p>

        {canBind && (
          <div className="flex flex-wrap items-center gap-2">
            <Input
              inputMode="numeric" value={appId} className="h-8 w-28 text-xs" placeholder="应用 ID"
              onChange={(e) => setAppId(e.target.value.replace(/\D/g, ""))}
            />
            {isPlatform && (
              <label className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                <Switch checked={owned} onCheckedChange={setOwned} />转移归属
              </label>
            )}
            <Button
              size="sm" disabled={bindMutation.isPending}
              onClick={async () => {
                const id = Number(appId);
                if (!id) { toast.error("请输入应用 ID"); return; }
                try {
                  await bindMutation.mutateAsync({ orgId, appId: id, owned });
                  toast.success("已绑定");
                  setAppId("");
                } catch (e) { toast.error(errMsg(e, "绑定失败")); }
              }}
            >
              <Plus className="size-3.5" />绑定
            </Button>
          </div>
        )}

        {bindings.length === 0 ? (
          <p className="py-6 text-center text-xs text-muted-foreground">暂无绑定的应用</p>
        ) : (
          <div className="space-y-2">
            {bindings.map((b) => (
              <div key={`${b.appId}-${b.owned}`} className="flex items-center gap-2 rounded-lg border px-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{b.appName || `APP #${b.appId}`}</span>
                    <Badge variant={b.owned ? "success" : "outline"} className="text-[9px]">
                      {b.owned ? "归属" : "授权"}
                    </Badge>
                  </div>
                  <span className="font-mono text-[10px] text-muted-foreground">#{b.appId}</span>
                </div>
                {canBind && (
                  <Button
                    size="sm" variant="ghost" className="h-6 px-1.5 text-destructive"
                    onClick={async () => {
                      try {
                        await unbindMutation.mutateAsync({ orgId, appId: b.appId });
                        toast.success("已解绑");
                      } catch (e) { toast.error(errMsg(e, "解绑失败")); }
                    }}
                  >
                    <Trash2 className="size-3" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function CollabGroupsCard({ orgId, canWrite }: { orgId: string; canWrite: boolean }) {
  const query = useCollabGroupsQuery(orgId);
  const treeQuery = useDepartmentTreeQuery(orgId);
  const createMutation = useCreateCollabGroupMutation();
  const deleteMutation = useDeleteCollabGroupMutation();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [deptIds, setDeptIds] = useState<string[]>([]);
  const [permissions, setPermissions] = useState<string[]>([]);

  const groups = query.data ?? [];
  const options = flattenDeptOptions(treeQuery.data ?? []);

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold"><Users className="size-4" />跨部门协作组</h3>
          {canWrite && <Button size="sm" onClick={() => setCreating(true)}><Plus className="size-3.5" />新建</Button>}
        </div>

        {groups.length === 0 ? (
          <EmptyState title="暂无协作组" description="把多个部门编成一组，用于跨部门的临时项目协作" />
        ) : (
          <div className="grid gap-2 md:grid-cols-2">
            {groups.map((g) => (
              <div key={g.id} className="group rounded-lg border px-3 py-2">
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-medium">{g.name}</span>
                  <Badge variant="outline" className="text-[9px] tabular-nums">{g.departments.length} 个部门</Badge>
                  <Badge variant="outline" className="text-[9px] tabular-nums">{g.memberCount} 人</Badge>
                  {canWrite && (
                    <Button
                      size="sm" variant="ghost" className="ml-auto hidden h-6 px-1.5 text-destructive group-hover:flex"
                      onClick={async () => {
                        try {
                          await deleteMutation.mutateAsync({ orgId, groupId: g.id });
                          toast.success("已删除");
                        } catch (e) { toast.error(errMsg(e, "删除失败")); }
                      }}
                    >
                      <Trash2 className="size-3" />
                    </Button>
                  )}
                </div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {g.departments.map((d) => (
                    <Badge key={d.id} variant="outline" className="text-[9px]">{d.name}</Badge>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}

        {creating && (
          <Dialog open onOpenChange={(o) => { if (!o) setCreating(false); }}>
            <DialogContent className="max-h-[85vh] overflow-y-auto">
              <DialogHeader><DialogTitle>新建协作组</DialogTitle></DialogHeader>
              <div className="space-y-3">
                <div className="space-y-1.5">
                  <Label className="text-xs">名称</Label>
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="双十一项目组" />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">参与部门</Label>
                  <div className="max-h-40 space-y-1 overflow-y-auto rounded-lg border p-2">
                    {options.map((o) => (
                      <label key={o.id} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-muted/50">
                        <input
                          type="checkbox" className="size-3.5"
                          checked={deptIds.includes(o.id)}
                          onChange={(e) => setDeptIds(e.target.checked ? [...deptIds, o.id] : deptIds.filter((id) => id !== o.id))}
                        />
                        <span className="whitespace-pre">{o.label}</span>
                      </label>
                    ))}
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">共享权限</Label>
                  <PermissionPicker value={permissions} onChange={setPermissions} />
                </div>
                <Button
                  className="w-full" disabled={createMutation.isPending}
                  onClick={async () => {
                    if (!name.trim()) { toast.error("请填写名称"); return; }
                    try {
                      await createMutation.mutateAsync({ orgId, payload: { name, deptIds, permissions } });
                      toast.success("协作组已创建");
                      setCreating(false); setName(""); setDeptIds([]); setPermissions([]);
                    } catch (e) { toast.error(errMsg(e, "创建失败")); }
                  }}
                >
                  创建
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
      </CardContent>
    </Card>
  );
}
