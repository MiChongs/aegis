"use client";

import { useState } from "react";
import {
  Building2, ChevronRight, Crown, FolderTree, GripVertical, Loader2,
  Pencil, Plus, Trash2, UserMinus, Users,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { DepartmentMember, DepartmentNode, OrgAccess } from "@/lib/api/types";
import {
  useCreateDepartmentMutation, useDeleteDepartmentMutation, useDepartmentMembersQuery,
  useDepartmentTreeQuery, useMoveDepartmentMutation, useOrgMetadataQuery,
  usePositionsQuery, useRemoveDeptMemberMutation, useSetDeptMemberMutation,
  useUpdateDepartmentMutation,
} from "@/lib/org-hooks";
import { cn } from "@/lib/utils";
import { flattenDeptOptions, OrgRoleBadge, useOrgCan } from "./org-shared";

function errMsg(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback;
}

/* ================================================================== */
/*  组织结构：左侧部门树 + 右侧部门详情                                 */
/* ================================================================== */

export function OrgStructurePanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const [selectedDept, setSelectedDept] = useState<string | null>(null);
  const treeQuery = useDepartmentTreeQuery(orgId);
  const tree = treeQuery.data ?? [];

  return (
    <div className="grid gap-4 lg:grid-cols-[340px_1fr]">
      <Card>
        <CardContent className="p-3">
          <DeptTree
            orgId={orgId} access={access} tree={tree}
            loading={treeQuery.isLoading}
            selected={selectedDept} onSelect={setSelectedDept}
          />
        </CardContent>
      </Card>
      <Card>
        <CardContent className="p-4">
          {selectedDept ? (
            <DeptDetail orgId={orgId} deptId={selectedDept} access={access} tree={tree} />
          ) : (
            <div className="flex h-64 items-center justify-center text-sm text-muted-foreground">
              选择左侧部门查看成员与详情
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

/* ── 部门树 ── */

function DeptTree({
  orgId, access, tree, loading, selected, onSelect,
}: {
  orgId: string; access?: OrgAccess | null; tree: DepartmentNode[];
  loading: boolean; selected: string | null; onSelect: (id: string) => void;
}) {
  const can = useOrgCan(access);
  const moveMutation = useMoveDepartmentMutation();
  const [creatingUnder, setCreatingUnder] = useState<string | null | undefined>(undefined);
  const [dragging, setDragging] = useState<string | null>(null);
  const [dropTarget, setDropTarget] = useState<string | null>(null);

  const canWrite = can("org:dept:write");

  const handleDrop = async (targetId: string) => {
    if (!dragging || dragging === targetId) return;
    try {
      await moveMutation.mutateAsync({ orgId, deptId: dragging, parentId: targetId });
      toast.success("部门已移动");
    } catch (e) {
      // 后端会拦下「移到自己子树里」这类会成环的操作，把原话透出来
      toast.error(errMsg(e, "移动失败"));
    } finally {
      setDragging(null);
      setDropTarget(null);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">部门</span>
        {canWrite && (
          <Button variant="ghost" size="sm" className="h-6 px-2" onClick={() => setCreatingUnder(null)}>
            <Plus className="size-3" />
          </Button>
        )}
      </div>

      {canWrite && dragging && (
        <div
          onDragOver={(e) => { e.preventDefault(); setDropTarget("__root__"); }}
          onDragLeave={() => setDropTarget(null)}
          onDrop={async () => {
            if (!dragging) return;
            try {
              await moveMutation.mutateAsync({ orgId, deptId: dragging, parentId: "" });
              toast.success("已移动到顶级");
            } catch (e) { toast.error(errMsg(e, "移动失败")); }
            finally { setDragging(null); setDropTarget(null); }
          }}
          className={cn(
            "rounded-md border border-dashed px-2 py-1.5 text-center text-[10px] transition-colors",
            dropTarget === "__root__" ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground",
          )}
        >
          拖到这里移动为顶级部门
        </div>
      )}

      {loading ? (
        <div className="py-6 text-center text-xs text-muted-foreground">加载中…</div>
      ) : tree.length === 0 ? (
        <p className="py-6 text-center text-xs text-muted-foreground">暂无部门</p>
      ) : (
        <ScrollArea className="max-h-[540px] min-h-0">
          <div className="space-y-0.5 pr-2">
            {tree.map((node) => (
              <DeptNode
                key={node.id} node={node} depth={0}
                selected={selected} onSelect={onSelect}
                canWrite={canWrite}
                onAddChild={(id) => setCreatingUnder(id)}
                dragging={dragging} setDragging={setDragging}
                dropTarget={dropTarget} setDropTarget={setDropTarget}
                onDrop={handleDrop}
              />
            ))}
          </div>
        </ScrollArea>
      )}

      {creatingUnder !== undefined && (
        <CreateDeptDialog
          orgId={orgId} parentId={creatingUnder} tree={tree}
          onClose={() => setCreatingUnder(undefined)}
        />
      )}
    </div>
  );
}

function DeptNode({
  node, depth, selected, onSelect, canWrite, onAddChild,
  dragging, setDragging, dropTarget, setDropTarget, onDrop,
}: {
  node: DepartmentNode; depth: number; selected: string | null;
  onSelect: (id: string) => void; canWrite: boolean; onAddChild: (id: string) => void;
  dragging: string | null; setDragging: (id: string | null) => void;
  dropTarget: string | null; setDropTarget: (id: string | null) => void;
  onDrop: (targetId: string) => void;
}) {
  const [expanded, setExpanded] = useState(true);
  const hasChildren = node.children.length > 0;

  return (
    <div>
      <div
        draggable={canWrite}
        onDragStart={() => setDragging(node.id)}
        onDragEnd={() => { setDragging(null); setDropTarget(null); }}
        onDragOver={(e) => { if (dragging && dragging !== node.id) { e.preventDefault(); setDropTarget(node.id); } }}
        onDragLeave={() => setDropTarget(null)}
        onDrop={(e) => { e.preventDefault(); onDrop(node.id); }}
        className={cn(
          "group flex cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-xs transition-colors",
          selected === node.id ? "bg-primary/10 font-medium text-primary" : "hover:bg-muted/50",
          dropTarget === node.id && "ring-1 ring-primary",
          dragging === node.id && "opacity-40",
        )}
        style={{ paddingLeft: `${depth * 14 + 8}px` }}
        onClick={() => onSelect(node.id)}
      >
        {hasChildren ? (
          <button type="button" className="shrink-0" onClick={(e) => { e.stopPropagation(); setExpanded(!expanded); }}>
            <ChevronRight className={cn("size-3 transition-transform", expanded && "rotate-90")} />
          </button>
        ) : <span className="size-3" />}

        {canWrite && <GripVertical className="size-3 shrink-0 text-muted-foreground/40 opacity-0 group-hover:opacity-100" />}
        <FolderTree className="size-3 shrink-0 text-muted-foreground" />
        <span className="flex-1 truncate">{node.name}</span>

        {/* 直属人数 / 含子部门总人数 —— 两个数字含义不同，分开显示 */}
        <Badge variant="outline" className="shrink-0 text-[9px] tabular-nums">
          {node.memberCount}
          {node.totalMemberCount > node.memberCount && (
            <span className="text-muted-foreground">/{node.totalMemberCount}</span>
          )}
        </Badge>

        {canWrite && (
          <button
            type="button" className="hidden rounded p-0.5 hover:bg-muted group-hover:block"
            onClick={(e) => { e.stopPropagation(); onAddChild(node.id); }}
          >
            <Plus className="size-2.5" />
          </button>
        )}
      </div>

      {expanded && node.children.map((child) => (
        <DeptNode
          key={child.id} node={child} depth={depth + 1}
          selected={selected} onSelect={onSelect} canWrite={canWrite} onAddChild={onAddChild}
          dragging={dragging} setDragging={setDragging}
          dropTarget={dropTarget} setDropTarget={setDropTarget} onDrop={onDrop}
        />
      ))}
    </div>
  );
}

/* ── 创建部门 ── */

function CreateDeptDialog({
  orgId, parentId, tree, onClose,
}: { orgId: string; parentId: string | null; tree: DepartmentNode[]; onClose: () => void }) {
  const mutation = useCreateDepartmentMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [kind, setKind] = useState("department");
  const [parent, setParent] = useState(parentId ?? "");
  const [description, setDescription] = useState("");

  const options = flattenDeptOptions(tree);

  const submit = async () => {
    if (!name.trim() || !code.trim()) { toast.error("请填写部门名称与代码"); return; }
    try {
      await mutation.mutateAsync({ orgId, payload: { name, code, kind, parentId: parent || undefined, description } });
      toast.success("部门已创建");
      onClose();
    } catch (e) { toast.error(errMsg(e, "创建失败")); }
  };

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent>
        <DialogHeader><DialogTitle>新建部门</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">名称</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="技术中心" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">代码</Label>
              <Input
                value={code} className="font-mono"
                onChange={(e) => setCode(e.target.value.replace(/[^a-zA-Z0-9\-_]/g, ""))}
                placeholder="tech-center"
              />
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">上级部门</Label>
              <Select value={parent || "__root__"} onValueChange={(v) => setParent(v === "__root__" ? "" : v)}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="__root__">（顶级部门）</SelectItem>
                  {options.map((o) => <SelectItem key={o.id} value={o.id}>{o.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">类型</Label>
              <Select value={kind} onValueChange={setKind}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.deptKinds ?? []).map((k) => <SelectItem key={k.value} value={k.value}>{k.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">描述</Label>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
          </div>
          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 创建部门
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/* ── 部门详情 ── */

function DeptDetail({
  orgId, deptId, access, tree,
}: { orgId: string; deptId: string; access?: OrgAccess | null; tree: DepartmentNode[] }) {
  const can = useOrgCan(access);
  const membersQuery = useDepartmentMembersQuery(orgId, deptId);
  const removeMutation = useRemoveDeptMemberMutation();
  const [editing, setEditing] = useState(false);
  const [editingMember, setEditingMember] = useState<DepartmentMember | null>(null);
  const [deleting, setDeleting] = useState(false);

  const members = membersQuery.data ?? [];
  const dept = findDept(tree, deptId);
  const canWrite = can("org:dept:write");
  const canMember = can("org:member:write");

  if (!dept) return <div className="py-10 text-center text-sm text-muted-foreground">部门不存在或已被移除</div>;

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h3 className="truncate text-base font-semibold">{dept.name}</h3>
            <Badge variant="outline" className="font-mono text-[9px]">{dept.code}</Badge>
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {dept.description || "暂无描述"}
            {dept.leaderName && <span className="ml-2">负责人：{dept.leaderName}</span>}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <Badge variant="outline" className="tabular-nums">{members.length} 人</Badge>
          {canWrite && (
            <>
              <Button variant="ghost" size="sm" className="h-7" onClick={() => setEditing(true)}>
                <Pencil className="size-3" />
              </Button>
              <Button
                variant="ghost" size="sm"
                className="h-7 text-destructive hover:text-destructive"
                onClick={() => setDeleting(true)}
              >
                <Trash2 className="size-3" />
              </Button>
            </>
          )}
        </div>
      </div>

      <Separator />

      <div className="flex items-center gap-2 text-sm font-semibold">
        <Users className="size-4" />成员
      </div>

      {members.length === 0 ? (
        <EmptyState title="该部门暂无成员" description="在「成员」标签页中把组织成员分配到这里" />
      ) : (
        <div className="space-y-2">
          {members.map((m) => (
            <div key={m.adminId} className="group flex items-center gap-3 rounded-lg border px-3 py-2">
              <Avatar className="size-8">
                <AvatarImage src={m.avatar} />
                <AvatarFallback>{(m.displayName || m.account)[0]}</AvatarFallback>
              </Avatar>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="truncate text-sm font-medium">{m.displayName || m.account}</span>
                  {m.isLeader && (
                    <Badge variant="success" className="gap-0.5 text-[9px]">
                      <Crown className="size-2.5" />负责人
                    </Badge>
                  )}
                  <OrgRoleBadge role={m.orgRole} />
                  {m.positionName && <Badge variant="outline" className="text-[9px]">{m.positionName}</Badge>}
                </div>
                <div className="flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground">
                  <span>{m.account}</span>
                  {m.jobTitle && <span>{m.jobTitle}</span>}
                  {m.reportingName && <span>汇报给 {m.reportingName}</span>}
                  {m.delegateName && <span>代理：{m.delegateName}</span>}
                </div>
              </div>
              {canMember && (
                <div className="hidden items-center gap-1 group-hover:flex">
                  <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={() => setEditingMember(m)}>
                    <Pencil className="size-3" />
                  </Button>
                  <Button
                    variant="ghost" size="sm" className="h-6 px-1.5 text-destructive hover:text-destructive"
                    onClick={async () => {
                      try {
                        await removeMutation.mutateAsync({ orgId, deptId, adminId: m.adminId });
                        toast.success("已移出部门（仍保留组织成员身份）");
                      } catch (e) { toast.error(errMsg(e, "移出失败")); }
                    }}
                  >
                    <UserMinus className="size-3" />
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {editing && <EditDeptSheet orgId={orgId} dept={dept} members={members} onClose={() => setEditing(false)} />}
      {editingMember && (
        <DeptMemberSheet
          orgId={orgId} deptId={deptId} member={editingMember} members={members}
          onClose={() => setEditingMember(null)}
        />
      )}
      {deleting && (
        <DeleteDeptDialog orgId={orgId} dept={dept} onClose={() => setDeleting(false)} />
      )}
    </div>
  );
}

function findDept(nodes: DepartmentNode[], id: string): DepartmentNode | null {
  for (const node of nodes) {
    if (node.id === id) return node;
    const found = findDept(node.children, id);
    if (found) return found;
  }
  return null;
}

/* ── 编辑部门 ── */

function EditDeptSheet({
  orgId, dept, members, onClose,
}: { orgId: string; dept: DepartmentNode; members: DepartmentMember[]; onClose: () => void }) {
  const mutation = useUpdateDepartmentMutation();
  const [name, setName] = useState(dept.name);
  const [code, setCode] = useState(dept.code);
  const [description, setDescription] = useState(dept.description);
  const [leaderId, setLeaderId] = useState(dept.leaderId ? String(dept.leaderId) : "__none__");
  const [status, setStatus] = useState(dept.status);

  const submit = async () => {
    const payload: Record<string, unknown> = { name, code, description, status };
    if (leaderId === "__none__") payload.clearLeader = true;
    else payload.leaderId = Number(leaderId);
    try {
      await mutation.mutateAsync({ orgId, deptId: dept.id, payload });
      toast.success("部门已更新");
      onClose();
    } catch (e) { toast.error(errMsg(e, "更新失败")); }
  };

  return (
    <Sheet open onOpenChange={(o) => { if (!o) onClose(); }}>
      <SheetContent className="sm:max-w-md">
        <SheetHeader><SheetTitle>编辑部门</SheetTitle></SheetHeader>
        <div className="mt-4 space-y-3 px-4">
          <div className="space-y-1.5">
            <Label className="text-xs">名称</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">代码</Label>
            <Input value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">负责人</Label>
            <Select value={leaderId} onValueChange={setLeaderId}>
              <SelectTrigger className="h-9 text-xs"><SelectValue placeholder="选择负责人" /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">（不设负责人）</SelectItem>
                {members.map((m) => (
                  <SelectItem key={m.adminId} value={String(m.adminId)}>{m.displayName || m.account}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[10px] text-muted-foreground">负责人须先是该部门成员</p>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">状态</Label>
            <Select value={status} onValueChange={setStatus}>
              <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="active">正常</SelectItem>
                <SelectItem value="disabled">停用</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">描述</Label>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
          </div>
          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 保存
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

/* ── 删除部门：策略必须由操作者显式选择 ── */

function DeleteDeptDialog({ orgId, dept, onClose }: { orgId: string; dept: DepartmentNode; onClose: () => void }) {
  const mutation = useDeleteDepartmentMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [strategy, setStrategy] = useState("restrict");

  const hasChildren = dept.childCount > 0;
  const hasMembers = dept.memberCount > 0;

  return (
    <AlertDialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除部门「{dept.name}」</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3">
              <p>
                {hasChildren && <>该部门下还有 <strong>{dept.childCount}</strong> 个子部门。</>}
                {hasMembers && <>该部门有 <strong>{dept.memberCount}</strong> 名成员。</>}
                {!hasChildren && !hasMembers && "该部门为空，可直接删除。"}
              </p>
              <div className="space-y-1.5 text-left">
                <Label className="text-xs">子部门与成员的处置方式</Label>
                <Select value={strategy} onValueChange={setStrategy}>
                  <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {(meta?.deleteStrategies ?? []).map((s) => (
                      <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {strategy === "cascade" && (
                  <p className="text-[10px] text-destructive">
                    整棵子树将被删除，成员退回组织但保留组织成员身份。此操作不可撤销。
                  </p>
                )}
              </div>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            onClick={async (e) => {
              e.preventDefault();
              try {
                await mutation.mutateAsync({ orgId, deptId: dept.id, strategy });
                toast.success("部门已删除");
                onClose();
              } catch (err) { toast.error(errMsg(err, "删除失败")); }
            }}
          >
            确认删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/* ── 部门成员属性（岗位 / 汇报线 / 代理 / 负责人） ── */

function DeptMemberSheet({
  orgId, deptId, member, members, onClose,
}: {
  orgId: string; deptId: string; member: DepartmentMember;
  members: DepartmentMember[]; onClose: () => void;
}) {
  const mutation = useSetDeptMemberMutation();
  const positionsQuery = usePositionsQuery(orgId);
  const [positionId, setPositionId] = useState(member.positionId || "__none__");
  const [jobTitle, setJobTitle] = useState(member.jobTitle ?? "");
  const [reportingTo, setReportingTo] = useState(member.reportingTo ? String(member.reportingTo) : "__none__");
  const [delegateTo, setDelegateTo] = useState(member.delegateTo ? String(member.delegateTo) : "__none__");
  const [delegateUntil, setDelegateUntil] = useState(member.delegateExpiresAt?.slice(0, 16) ?? "");
  const [isLeader, setIsLeader] = useState(member.isLeader);

  const others = members.filter((m) => m.adminId !== member.adminId);
  const positions = positionsQuery.data ?? [];

  const submit = async () => {
    const payload: Record<string, unknown> = {
      isLeader,
      positionId: positionId === "__none__" ? "" : positionId,
      jobTitle,
    };
    if (reportingTo === "__none__") payload.clearReporting = true;
    else payload.reportingTo = Number(reportingTo);
    if (delegateTo === "__none__") payload.clearDelegate = true;
    else {
      payload.delegateTo = Number(delegateTo);
      if (delegateUntil) payload.delegateExpiresAt = new Date(delegateUntil).toISOString();
    }

    try {
      await mutation.mutateAsync({ orgId, deptId, adminId: member.adminId, payload });
      toast.success("成员信息已更新");
      onClose();
    } catch (e) { toast.error(errMsg(e, "更新失败")); }
  };

  return (
    <Sheet open onOpenChange={(o) => { if (!o) onClose(); }}>
      <SheetContent className="overflow-y-auto sm:max-w-md">
        <SheetHeader><SheetTitle>{member.displayName || member.account}</SheetTitle></SheetHeader>
        <div className="mt-4 space-y-4 px-4">
          <div className="flex items-center justify-between rounded-lg border px-3 py-2">
            <div>
              <Label className="text-xs font-semibold">部门负责人</Label>
              <p className="text-[10px] text-muted-foreground">一个部门只能有一位负责人</p>
            </div>
            <Switch checked={isLeader} onCheckedChange={setIsLeader} />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">岗位</Label>
            <Select value={positionId} onValueChange={setPositionId}>
              <SelectTrigger className="h-9 text-xs"><SelectValue placeholder="选择岗位" /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">（无岗位）</SelectItem>
                {positions.map((p) => (
                  <SelectItem key={p.id} value={p.id}>{p.name}（{p.code}）</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={jobTitle} onChange={(e) => setJobTitle(e.target.value)}
              placeholder="职位名称，如：高级工程师" className="h-8 text-xs"
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">汇报给</Label>
            <Select value={reportingTo} onValueChange={setReportingTo}>
              <SelectTrigger className="h-9 text-xs"><SelectValue placeholder="选择上级" /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">（无上级）</SelectItem>
                {others.map((m) => (
                  <SelectItem key={m.adminId} value={String(m.adminId)}>{m.displayName || m.account}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[10px] text-muted-foreground">上级须为同部门成员；成环的设置会被后端拒绝</p>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">权限代理</Label>
            <Select value={delegateTo} onValueChange={setDelegateTo}>
              <SelectTrigger className="h-9 text-xs"><SelectValue placeholder="代理给" /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">（不代理）</SelectItem>
                {others.map((m) => (
                  <SelectItem key={m.adminId} value={String(m.adminId)}>{m.displayName || m.account}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            {delegateTo !== "__none__" && (
              <Input
                type="datetime-local" value={delegateUntil}
                onChange={(e) => setDelegateUntil(e.target.value)} className="h-8 text-xs"
              />
            )}
          </div>

          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 保存
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

/* ── 组织切换器 ── */

export function OrgSwitcher({
  organizations, activeId, onSelect,
}: {
  organizations: { id: string; name: string; status: string; viewerRole?: string }[];
  activeId: string | null;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {organizations.map((o) => (
        <Button
          key={o.id}
          variant={activeId === o.id ? "default" : "outline"}
          size="sm"
          onClick={() => onSelect(o.id)}
          className="gap-1.5"
        >
          <Building2 className="size-3.5" />
          {o.name}
          {o.viewerRole && <OrgRoleBadge role={o.viewerRole} />}
        </Button>
      ))}
    </div>
  );
}
