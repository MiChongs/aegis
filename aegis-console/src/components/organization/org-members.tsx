"use client";

import { useState } from "react";
import {
  Building2, Loader2, Mail, MailCheck, MailX, Search, Send,
  UserCog, UserPlus, UserX,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { DepartmentNode, OrgAccess, OrgMember } from "@/lib/api/types";
import {
  useAddOrgMemberMutation, useAssignMemberDeptsMutation, useDepartmentTreeQuery,
  useInviteMembersMutation, useMyInvitationsQuery, useOrgMembersQuery, useOrgMetadataQuery,
  usePendingInvitationCountQuery, useRemoveOrgMemberMutation, useRespondInvitationMutation,
  useUpdateOrgMemberMutation,
} from "@/lib/org-hooks";
import { AdminPicker, flattenDeptOptions, OrgRoleBadge, StatusBadge, useOrgCan } from "./org-shared";

function errMsg(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback;
}

/* ================================================================== */
/*  组织成员                                                           */
/* ================================================================== */

export function OrgMembersPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  const [keyword, setKeyword] = useState("");
  const [orgRole, setOrgRole] = useState("__all__");
  const [deptId, setDeptId] = useState("__all__");
  const [unassigned, setUnassigned] = useState(false);
  const [page, setPage] = useState(1);
  const [adding, setAdding] = useState(false);
  const [inviting, setInviting] = useState(false);
  const [editing, setEditing] = useState<OrgMember | null>(null);

  const treeQuery = useDepartmentTreeQuery(orgId);
  const { data: meta } = useOrgMetadataQuery();
  const membersQuery = useOrgMembersQuery(orgId, {
    keyword: keyword || undefined,
    orgRole: orgRole === "__all__" ? undefined : orgRole,
    deptId: deptId === "__all__" ? undefined : deptId,
    includeSubDepts: deptId !== "__all__",
    unassigned: unassigned || undefined,
    page, limit: 20,
  });

  const members = membersQuery.data?.items ?? [];
  const total = membersQuery.data?.total ?? 0;
  const totalPages = membersQuery.data?.totalPages ?? 1;
  const deptOptions = flattenDeptOptions(treeQuery.data ?? []);
  const canWrite = can("org:member:write");
  const canInvite = can("org:member:invite");

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <UserCog className="size-4" />组织成员
            <Badge variant="outline" className="tabular-nums">{total}</Badge>
          </h3>
          <div className="flex items-center gap-1.5">
            {canInvite && (
              <Button size="sm" variant="outline" onClick={() => setInviting(true)}>
                <Send className="size-3.5" />邀请
              </Button>
            )}
            {canWrite && (
              <Button size="sm" onClick={() => setAdding(true)}>
                <UserPlus className="size-3.5" />直接添加
              </Button>
            )}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[200px] flex-1">
            <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
              placeholder="搜索账号 / 姓名 / 邮箱 / 工号"
              className="h-8 pl-8 text-xs"
            />
          </div>
          <Select value={orgRole} onValueChange={(v) => { setOrgRole(v); setPage(1); }}>
            <SelectTrigger className="h-8 w-32 text-xs"><SelectValue placeholder="角色" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">全部角色</SelectItem>
              {(meta?.builtinRoles ?? []).map((r) => (
                <SelectItem key={r.key} value={r.key}>{r.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={deptId} onValueChange={(v) => { setDeptId(v); setPage(1); }}>
            <SelectTrigger className="h-8 w-40 text-xs"><SelectValue placeholder="部门" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">全部部门</SelectItem>
              {deptOptions.map((o) => <SelectItem key={o.id} value={o.id}>{o.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Switch checked={unassigned} onCheckedChange={(v) => { setUnassigned(v); setPage(1); }} />
            未分配部门
          </label>
        </div>

        {membersQuery.isLoading ? (
          <div className="py-10 text-center text-xs text-muted-foreground">加载中…</div>
        ) : members.length === 0 ? (
          <EmptyState title="没有匹配的成员" description="调整筛选条件，或把管理员加入这个组织" />
        ) : (
          <div className="space-y-2">
            {members.map((m) => (
              <div key={m.adminId} className="group flex items-center gap-3 rounded-lg border px-3 py-2">
                <Avatar className="size-9">
                  <AvatarImage src={m.avatar} />
                  <AvatarFallback>{(m.displayName || m.account)[0]}</AvatarFallback>
                </Avatar>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{m.displayName || m.account}</span>
                    <OrgRoleBadge role={m.orgRole} />
                    {m.status !== "active" && <StatusBadge status={m.status} />}
                    {m.isSuperAdmin && <Badge variant="info" className="text-[9px]">平台超管</Badge>}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground">
                    <span>{m.account}</span>
                    {m.employeeNo && <span>工号 {m.employeeNo}</span>}
                    {m.title && <span>{m.title}</span>}
                  </div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    {m.departments.length === 0 ? (
                      <Badge variant="warning" className="text-[9px]">未分配部门</Badge>
                    ) : m.departments.map((d) => (
                      <Badge key={d.id} variant="outline" className="text-[9px]" title={d.fullName}>
                        {d.name}{d.isLeader ? " · 负责人" : ""}
                      </Badge>
                    ))}
                  </div>
                </div>
                {canWrite && (
                  <Button variant="ghost" size="sm" className="h-7 opacity-0 group-hover:opacity-100" onClick={() => setEditing(m)}>
                    <UserCog className="size-3.5" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}

        {totalPages > 1 && (
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>第 {page} / {totalPages} 页</span>
            <div className="flex gap-1.5">
              <Button size="sm" variant="outline" className="h-7" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
              <Button size="sm" variant="outline" className="h-7" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>下一页</Button>
            </div>
          </div>
        )}

        {adding && <AddMemberDialog orgId={orgId} tree={treeQuery.data ?? []} onClose={() => setAdding(false)} />}
        {inviting && <InviteDialog orgId={orgId} tree={treeQuery.data ?? []} onClose={() => setInviting(false)} />}
        {editing && (
          <MemberSheet
            orgId={orgId} member={editing} tree={treeQuery.data ?? []}
            onClose={() => setEditing(null)}
          />
        )}
      </CardContent>
    </Card>
  );
}

/* ── 直接添加成员 ── */

function AddMemberDialog({ orgId, tree, onClose }: { orgId: string; tree: DepartmentNode[]; onClose: () => void }) {
  const mutation = useAddOrgMemberMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [picked, setPicked] = useState<OrgMember[]>([]);
  const [orgRole, setOrgRole] = useState("member");
  const [deptId, setDeptId] = useState("__none__");

  const options = flattenDeptOptions(tree);

  const submit = async () => {
    if (picked.length === 0) { toast.error("请选择要加入的管理员"); return; }
    let ok = 0;
    for (const member of picked) {
      try {
        await mutation.mutateAsync({
          orgId,
          payload: {
            adminId: member.adminId, orgRole,
            deptIds: deptId === "__none__" ? [] : [deptId],
            primaryDeptId: deptId === "__none__" ? undefined : deptId,
          },
        });
        ok += 1;
      } catch (e) {
        toast.error(`${member.displayName || member.account}：${errMsg(e, "加入失败")}`);
      }
    }
    if (ok > 0) { toast.success(`已加入 ${ok} 人`); onClose(); }
  };

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader><DialogTitle>直接添加成员</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            直接添加会跳过邀请确认，被加入者立即成为组织成员。若希望对方自行确认，请改用「邀请」。
          </p>
          <AdminPicker orgId={orgId} selected={picked} onChange={setPicked} />
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">组织角色</Label>
              <Select value={orgRole} onValueChange={setOrgRole}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.builtinRoles ?? []).filter((r) => r.key !== "owner").map((r) => (
                    <SelectItem key={r.key} value={r.key}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">加入部门</Label>
              <Select value={deptId} onValueChange={setDeptId}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">（暂不分配）</SelectItem>
                  {options.map((o) => <SelectItem key={o.id} value={o.id}>{o.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
            加入组织{picked.length > 0 ? `（${picked.length} 人）` : ""}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ── 邀请 ── */

function InviteDialog({ orgId, tree, onClose }: { orgId: string; tree: DepartmentNode[]; onClose: () => void }) {
  const mutation = useInviteMembersMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [picked, setPicked] = useState<OrgMember[]>([]);
  const [orgRole, setOrgRole] = useState("member");
  const [deptId, setDeptId] = useState("__none__");
  const [message, setMessage] = useState("");

  const options = flattenDeptOptions(tree);

  const submit = async () => {
    if (picked.length === 0) { toast.error("请选择受邀人"); return; }
    try {
      const result = await mutation.mutateAsync({
        orgId,
        payload: {
          adminIds: picked.map((m) => m.adminId),
          deptId: deptId === "__none__" ? undefined : deptId,
          orgRole, message,
        },
      });
      // 已在组织里、或已有待处理邀请的会被后端跳过，如实告诉操作者
      const skipped = result.requested - result.created;
      toast.success(
        skipped > 0
          ? `已发出 ${result.created} 份邀请，${skipped} 人已在组织中或已有待处理邀请`
          : `已发出 ${result.created} 份邀请`,
      );
      onClose();
    } catch (e) { toast.error(errMsg(e, "邀请失败")); }
  };

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader><DialogTitle>邀请加入组织</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <AdminPicker orgId={orgId} selected={picked} onChange={setPicked} />
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">组织角色</Label>
              <Select value={orgRole} onValueChange={setOrgRole}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.builtinRoles ?? []).filter((r) => r.key !== "owner").map((r) => (
                    <SelectItem key={r.key} value={r.key}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">目标部门</Label>
              <Select value={deptId} onValueChange={setDeptId}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">（仅加入组织）</SelectItem>
                  {options.map((o) => <SelectItem key={o.id} value={o.id}>{o.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">邀请留言</Label>
            <Textarea value={message} onChange={(e) => setMessage(e.target.value)} rows={2} placeholder="欢迎加入团队" className="text-xs" />
          </div>
          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
            发送邀请{picked.length > 0 ? `（${picked.length} 人）` : ""}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ── 成员档案 ── */

function MemberSheet({
  orgId, member, tree, onClose,
}: { orgId: string; member: OrgMember; tree: DepartmentNode[]; onClose: () => void }) {
  const updateMutation = useUpdateOrgMemberMutation();
  const assignMutation = useAssignMemberDeptsMutation();
  const removeMutation = useRemoveOrgMemberMutation();
  const { data: meta } = useOrgMetadataQuery();

  const [orgRole, setOrgRole] = useState(member.orgRole);
  const [employeeNo, setEmployeeNo] = useState(member.employeeNo);
  const [title, setTitle] = useState(member.title);
  const [status, setStatus] = useState(member.status);
  const [deptIds, setDeptIds] = useState<string[]>(member.departments.map((d) => d.id));
  const [primaryDept, setPrimaryDept] = useState(member.primaryDept?.id ?? "__none__");

  const options = flattenDeptOptions(tree);
  const isOwner = member.orgRole === "owner";

  const save = async () => {
    try {
      await updateMutation.mutateAsync({
        orgId, adminId: member.adminId,
        payload: {
          ...(isOwner ? {} : { orgRole }),
          employeeNo, title, status,
          primaryDeptId: primaryDept === "__none__" ? "" : primaryDept,
        },
      });
      await assignMutation.mutateAsync({
        orgId, adminId: member.adminId, deptIds, replace: true,
        primaryDeptId: primaryDept === "__none__" ? undefined : primaryDept,
      });
      toast.success("成员已更新");
      onClose();
    } catch (e) { toast.error(errMsg(e, "更新失败")); }
  };

  return (
    <Sheet open onOpenChange={(o) => { if (!o) onClose(); }}>
      <SheetContent className="overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            {member.displayName || member.account}
            <OrgRoleBadge role={member.orgRole} />
          </SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-4 px-4">
          {isOwner && (
            <p className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-[11px]">
              所有者的角色不能在这里修改，需要通过「组织设置 → 转让所有权」完成。
            </p>
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">组织角色</Label>
              <Select value={orgRole} onValueChange={setOrgRole} disabled={isOwner}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.builtinRoles ?? []).filter((r) => r.key !== "owner").map((r) => (
                    <SelectItem key={r.key} value={r.key}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">状态</Label>
              <Select value={status} onValueChange={setStatus}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.memberStatuses ?? []).map((s) => (
                    <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">工号</Label>
              <Input value={employeeNo} onChange={(e) => setEmployeeNo(e.target.value)} className="h-9 text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">职位</Label>
              <Input value={title} onChange={(e) => setTitle(e.target.value)} className="h-9 text-xs" />
            </div>
          </div>

          <Separator />

          <div className="space-y-2">
            <Label className="text-xs font-semibold">部门归属</Label>
            <div className="max-h-48 space-y-1 overflow-y-auto rounded-lg border p-2">
              {options.length === 0 ? (
                <p className="py-4 text-center text-[10px] text-muted-foreground">该组织还没有部门</p>
              ) : options.map((o) => (
                <label key={o.id} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-muted/50">
                  <input
                    type="checkbox"
                    checked={deptIds.includes(o.id)}
                    onChange={(e) => {
                      setDeptIds(e.target.checked ? [...deptIds, o.id] : deptIds.filter((id) => id !== o.id));
                    }}
                    className="size-3.5"
                  />
                  <span className="whitespace-pre">{o.label}</span>
                </label>
              ))}
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">主部门</Label>
              <Select value={primaryDept} onValueChange={setPrimaryDept}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">（不设主部门）</SelectItem>
                  {options.filter((o) => deptIds.includes(o.id)).map((o) => (
                    <SelectItem key={o.id} value={o.id}>{o.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <Button className="w-full" onClick={save} disabled={updateMutation.isPending || assignMutation.isPending}>
            {(updateMutation.isPending || assignMutation.isPending) && <Loader2 className="size-3.5 animate-spin" />} 保存
          </Button>

          {!isOwner && (
            <Button
              variant="outline" className="w-full text-destructive hover:text-destructive"
              disabled={removeMutation.isPending}
              onClick={async () => {
                try {
                  await removeMutation.mutateAsync({ orgId, adminId: member.adminId });
                  toast.success("已移出组织");
                  onClose();
                } catch (e) { toast.error(errMsg(e, "移出失败")); }
              }}
            >
              <UserX className="size-3.5" />移出组织
            </Button>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

/* ================================================================== */
/*  邀请中心                                                           */
/* ================================================================== */

export function InvitationsPanel() {
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card>
        <CardContent className="space-y-3 p-4">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <Mail className="size-4" />收到的邀请
            <PendingBadge />
          </h3>
          <InvitationList role="received" />
        </CardContent>
      </Card>
      <Card>
        <CardContent className="space-y-3 p-4">
          <h3 className="flex items-center gap-2 text-sm font-semibold"><Send className="size-4" />发出的邀请</h3>
          <InvitationList role="sent" />
        </CardContent>
      </Card>
    </div>
  );
}

export function PendingBadge() {
  const { data } = usePendingInvitationCountQuery();
  const count = data?.count ?? 0;
  if (count === 0) return null;
  return <Badge variant="danger" className="text-[9px]">{count}</Badge>;
}

function InvitationList({ role }: { role: "sent" | "received" }) {
  const query = useMyInvitationsQuery(role);
  const respond = useRespondInvitationMutation();
  const items = query.data?.items ?? [];

  const statusLabel: Record<string, string> = {
    pending: "待处理", accepted: "已接受", rejected: "已拒绝", expired: "已过期", cancelled: "已取消",
  };
  const statusVariant = (s: string) =>
    s === "accepted" ? "success" as const :
    s === "rejected" || s === "expired" ? "danger" as const : "outline" as const;

  const act = async (inviteId: string, action: "accept" | "reject" | "cancel", label: string) => {
    try {
      await respond.mutateAsync({ inviteId, action });
      toast.success(label);
    } catch (e) { toast.error(errMsg(e, "操作失败")); }
  };

  if (items.length === 0) {
    return <p className="py-8 text-center text-sm text-muted-foreground">暂无{role === "received" ? "收到" : "发出"}的邀请</p>;
  }

  return (
    <div className="space-y-2">
      {items.map((inv) => (
        <div key={inv.id} className="flex items-center gap-3 rounded-lg border px-3 py-2.5">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <Building2 className="size-3 text-muted-foreground" />
              <span className="truncate text-sm font-medium">
                {inv.orgName}{inv.deptName ? ` · ${inv.deptName}` : ""}
              </span>
              <Badge variant={statusVariant(inv.status)} className="text-[9px]">
                {statusLabel[inv.status] ?? inv.status}
              </Badge>
              <OrgRoleBadge role={inv.orgRole} />
            </div>
            <div className="mt-0.5 text-[10px] text-muted-foreground">
              {role === "received" ? `${inv.inviterName} 邀请你加入` : `邀请 ${inv.inviteeName} 加入`}
              {inv.message && <span className="ml-1.5">「{inv.message}」</span>}
            </div>
            <div className="mt-0.5 text-[9px] text-muted-foreground/60">
              {new Date(inv.createdAt).toLocaleString("zh-CN")}
              {inv.status === "pending" && (
                <span className="ml-2">截止 {new Date(inv.expiresAt).toLocaleDateString("zh-CN")}</span>
              )}
            </div>
          </div>
          {inv.status === "pending" && (
            <div className="flex shrink-0 items-center gap-1">
              {role === "received" ? (
                <>
                  <Button size="sm" className="h-7 text-xs" disabled={respond.isPending}
                    onClick={() => act(inv.id, "accept", "已接受邀请")}>
                    <MailCheck className="size-3" />接受
                  </Button>
                  <Button size="sm" variant="outline" className="h-7 text-xs" disabled={respond.isPending}
                    onClick={() => act(inv.id, "reject", "已拒绝邀请")}>
                    <MailX className="size-3" />拒绝
                  </Button>
                </>
              ) : (
                <Button size="sm" variant="outline" className="h-7 text-xs" disabled={respond.isPending}
                  onClick={() => act(inv.id, "cancel", "已撤回邀请")}>
                  撤回
                </Button>
              )}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
