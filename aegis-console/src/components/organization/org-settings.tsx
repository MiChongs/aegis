"use client";

import { useRef, useState } from "react";
import {
  AlertTriangle, Building2, Crown, Download, FileSpreadsheet, History,
  Loader2, Trash2, Upload,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import type { OrgAccess, OrgImportResult, OrgMember, Organization } from "@/lib/api/types";
import {
  useDeleteOrganizationMutation, useImportOrgMutation, useOrgActivityQuery,
  useOrgExport, useOrgMetadataQuery, useTransferOrganizationMutation,
  useUpdateOrganizationMutation,
} from "@/lib/org-hooks";
import { AdminPicker, StatTile, StatusBadge, useOrgCan } from "./org-shared";

function errMsg(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback;
}

/* ================================================================== */
/*  组织设置                                                           */
/* ================================================================== */

export function OrgSettingsPanel({
  org, access, onDeleted,
}: { org: Organization; access?: OrgAccess | null; onDeleted: () => void }) {
  const can = useOrgCan(access);
  const updateMutation = useUpdateOrganizationMutation();
  const { data: meta } = useOrgMetadataQuery();

  const [name, setName] = useState(org.name);
  const [code, setCode] = useState(org.code);
  const [kind, setKind] = useState(org.kind);
  const [description, setDescription] = useState(org.description);
  const [industry, setIndustry] = useState(org.industry);
  const [region, setRegion] = useState(org.region);
  const [contactName, setContactName] = useState(org.contact.name);
  const [contactEmail, setContactEmail] = useState(org.contact.email);
  const [contactPhone, setContactPhone] = useState(org.contact.phone);
  const [status, setStatus] = useState(org.status);
  const [memberLimit, setMemberLimit] = useState(String(org.quota.memberLimit));
  const [deptLimit, setDeptLimit] = useState(String(org.quota.deptLimit));
  const [appLimit, setAppLimit] = useState(String(org.quota.appLimit));

  const [transferring, setTransferring] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const canWrite = can("org:write");
  const canDelete = can("org:delete");
  const canTransfer = can("org:transfer");
  const isPlatform = Boolean(access?.isSuperAdmin || access?.isPlatformAdmin);

  const save = async () => {
    const payload: Record<string, unknown> = {
      name, code, kind, description, industry, region,
      contactName, contactEmail, contactPhone, status,
    };
    // 配额只有平台侧能改；组织侧提交会被后端拒绝，索性不带上
    if (isPlatform) {
      payload.memberLimit = Number(memberLimit) || 0;
      payload.deptLimit = Number(deptLimit) || 0;
      payload.appLimit = Number(appLimit) || 0;
    }
    try {
      await updateMutation.mutateAsync({ orgId: org.id, payload });
      toast.success("组织资料已保存");
    } catch (e) { toast.error(errMsg(e, "保存失败")); }
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-4 p-4">
          <div className="flex items-center justify-between">
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              <Building2 className="size-4" />组织资料
            </h3>
            <div className="flex items-center gap-1.5">
              <StatusBadge status={org.status} />
              {/* 组织 ID 是对外稳定标识，接入方和工单里都会用到，做成可复制 */}
              <button
                type="button"
                className="rounded border px-1.5 py-0.5 font-mono text-[9px] text-muted-foreground hover:bg-muted"
                title="点击复制组织 ID"
                onClick={() => {
                  void navigator.clipboard.writeText(org.id);
                  toast.success("组织 ID 已复制");
                }}
              >
                {org.id}
              </button>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">组织名称</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} disabled={!canWrite} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">组织代码</Label>
              <Input value={code} onChange={(e) => setCode(e.target.value)} disabled={!canWrite} className="font-mono" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">类型</Label>
              <Select value={kind} onValueChange={setKind} disabled={!canWrite}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.orgKinds ?? []).map((k) => <SelectItem key={k.value} value={k.value}>{k.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">状态</Label>
              <Select value={status} onValueChange={setStatus} disabled={!canWrite}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.orgStatuses ?? []).map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
                </SelectContent>
              </Select>
              {status === "archived" && (
                <p className="text-[10px] text-warning">归档后组织变为只读，所有写操作都会被拒绝</p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">所属行业</Label>
              <Input value={industry} onChange={(e) => setIndustry(e.target.value)} disabled={!canWrite} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">地区</Label>
              <Input value={region} onChange={(e) => setRegion(e.target.value)} disabled={!canWrite} />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">简介</Label>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} disabled={!canWrite} />
          </div>

          <Separator />

          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label className="text-xs">联系人</Label>
              <Input value={contactName} onChange={(e) => setContactName(e.target.value)} disabled={!canWrite} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">联系邮箱</Label>
              <Input value={contactEmail} onChange={(e) => setContactEmail(e.target.value)} disabled={!canWrite} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">联系电话</Label>
              <Input value={contactPhone} onChange={(e) => setContactPhone(e.target.value)} disabled={!canWrite} />
            </div>
          </div>

          <Separator />

          <div className="space-y-2">
            <Label className="text-xs font-semibold">配额（0 表示不限）</Label>
            {!isPlatform && (
              <p className="text-[10px] text-muted-foreground">配额由平台管理员设定，组织内无法自行调整。</p>
            )}
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-1.5">
                <Label className="text-xs">成员上限</Label>
                <Input
                  inputMode="numeric" value={memberLimit} disabled={!isPlatform}
                  onChange={(e) => setMemberLimit(e.target.value.replace(/\D/g, ""))}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">部门上限</Label>
                <Input
                  inputMode="numeric" value={deptLimit} disabled={!isPlatform}
                  onChange={(e) => setDeptLimit(e.target.value.replace(/\D/g, ""))}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">应用上限</Label>
                <Input
                  inputMode="numeric" value={appLimit} disabled={!isPlatform}
                  onChange={(e) => setAppLimit(e.target.value.replace(/\D/g, ""))}
                />
              </div>
            </div>
          </div>

          {canWrite && (
            <Button onClick={save} disabled={updateMutation.isPending}>
              {updateMutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 保存资料
            </Button>
          )}
        </CardContent>
      </Card>

      {(canTransfer || canDelete) && (
        <Card className="border-destructive/30">
          <CardContent className="space-y-3 p-4">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-destructive">
              <AlertTriangle className="size-4" />高危操作
            </h3>

            {canTransfer && (
              <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    <Crown className="size-3.5" />转让所有权
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    当前所有者：{org.ownerName || "未设置"}。转让后你会降为管理员。
                  </p>
                </div>
                <Button size="sm" variant="outline" onClick={() => setTransferring(true)}>转让</Button>
              </div>
            )}

            {canDelete && (
              <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    <Trash2 className="size-3.5" />删除组织
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    部门、成员关系、角色与审批记录会一并销毁，不可恢复。若只是暂停使用，改用「停用」或「归档」。
                  </p>
                </div>
                <Button size="sm" variant="outline" className="text-destructive hover:text-destructive" onClick={() => setDeleting(true)}>
                  删除
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {transferring && <TransferDialog org={org} onClose={() => setTransferring(false)} />}
      {deleting && <DeleteOrgDialog org={org} onClose={() => setDeleting(false)} onDeleted={onDeleted} />}
    </div>
  );
}

function TransferDialog({ org, onClose }: { org: Organization; onClose: () => void }) {
  const mutation = useTransferOrganizationMutation();
  const [picked, setPicked] = useState<OrgMember[]>([]);

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent>
        <DialogHeader><DialogTitle>转让组织所有权</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            新所有者必须已经是本组织的在职成员。转让完成后，你的角色会变成「管理员」。
          </p>
          <AdminPicker orgId={org.id} selected={picked} onChange={(next) => setPicked(next.slice(-1))} placeholder="搜索组织成员" />
          <Button
            className="w-full" disabled={mutation.isPending || picked.length === 0}
            onClick={async () => {
              try {
                await mutation.mutateAsync({ orgId: org.id, newOwnerAdminId: picked[0].adminId });
                toast.success("所有权已转让");
                onClose();
              } catch (e) { toast.error(errMsg(e, "转让失败")); }
            }}
          >
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 确认转让
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DeleteOrgDialog({ org, onClose, onDeleted }: { org: Organization; onClose: () => void; onDeleted: () => void }) {
  const mutation = useDeleteOrganizationMutation();
  const [confirmText, setConfirmText] = useState("");

  return (
    <AlertDialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除组织「{org.name}」</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3">
              <p>
                将永久删除 <strong>{org.stats.deptCount}</strong> 个部门、
                <strong>{org.stats.memberCount}</strong> 条成员关系，以及全部角色、审批与操作记录。
                此操作不可撤销。
              </p>
              {org.stats.childCount > 0 && (
                <p className="text-destructive">
                  该组织下还有 {org.stats.childCount} 个下级组织，需要先处理它们。
                </p>
              )}
              <div className="space-y-1.5 text-left">
                <Label className="text-xs">输入组织代码 <span className="font-mono">{org.code}</span> 以确认</Label>
                <Input value={confirmText} onChange={(e) => setConfirmText(e.target.value)} className="font-mono" />
              </div>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={confirmText !== org.code}
            onClick={async (e) => {
              e.preventDefault();
              try {
                await mutation.mutateAsync(org.id);
                toast.success("组织已删除");
                onDeleted();
                onClose();
              } catch (err) { toast.error(errMsg(err, "删除失败")); }
            }}
          >
            永久删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/* ================================================================== */
/*  导入导出                                                           */
/* ================================================================== */

export function OrgDataPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  const { exportOrg, downloadTemplate } = useOrgExport();
  const importMutation = useImportOrgMutation();
  const fileRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [result, setResult] = useState<OrgImportResult | null>(null);
  const [busy, setBusy] = useState(false);

  const canImport = can("org:import");
  const canExport = can("org:export");

  const run = async (dryRun: boolean) => {
    if (!file) { toast.error("请先选择 Excel 文件"); return; }
    try {
      const res = await importMutation.mutateAsync({ orgId, file, dryRun });
      setResult(res);
      if (dryRun) {
        const fatal = res.issues.filter((i) => i.fatal).length;
        if (fatal > 0) toast.error(`校验发现 ${fatal} 个阻断性问题`);
        else toast.success("校验通过，可以正式导入");
      } else {
        toast.success(`导入完成：新增 ${res.memberAdded} 人、更新 ${res.memberUpdated} 人、新建 ${res.deptCreated} 个部门`);
      }
    } catch (e) { toast.error(errMsg(e, "导入失败")); }
  };

  const download = async (fn: () => Promise<void>, label: string) => {
    setBusy(true);
    try { await fn(); toast.success(label); }
    catch (e) { toast.error(errMsg(e, "下载失败")); }
    finally { setBusy(false); }
  };

  const fatalCount = result?.issues.filter((i) => i.fatal).length ?? 0;

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardContent className="space-y-3 p-4">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <Download className="size-4" />导出
          </h3>
          <p className="text-[11px] text-muted-foreground">
            导出的文件与导入模板同构，改完可以直接导回来。
          </p>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" disabled={!canExport || busy} onClick={() => download(() => exportOrg(orgId), "导出完成")}>
              <FileSpreadsheet className="size-3.5" />导出组织架构
            </Button>
            <Button
              size="sm" variant="outline" disabled={!canImport || busy}
              onClick={() => download(() => downloadTemplate(orgId), "模板已下载")}
            >
              <Download className="size-3.5" />下载导入模板
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3 p-4">
          <h3 className="flex items-center gap-2 text-sm font-semibold"><Upload className="size-4" />导入</h3>
          <p className="text-[11px] text-muted-foreground">
            建议先「仅校验」跑一遍：它会把所有问题一次列出来，而不是导到一半失败留下半个组织。
          </p>

          {!canImport ? (
            <p className="py-4 text-center text-xs text-muted-foreground">你没有导入权限</p>
          ) : (
            <>
              <input
                ref={fileRef} type="file" accept=".xlsx" className="hidden"
                onChange={(e) => { setFile(e.target.files?.[0] ?? null); setResult(null); }}
              />
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" variant="outline" onClick={() => fileRef.current?.click()}>选择文件</Button>
                {file && <span className="truncate text-xs text-muted-foreground">{file.name}</span>}
              </div>
              <div className="flex gap-2">
                <Button size="sm" variant="outline" disabled={!file || importMutation.isPending} onClick={() => run(true)}>
                  {importMutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 仅校验
                </Button>
                <Button
                  size="sm"
                  disabled={!file || importMutation.isPending || (result?.dryRun === true && fatalCount > 0)}
                  onClick={() => run(false)}
                >
                  正式导入
                </Button>
              </div>
            </>
          )}

          {result && (
            <div className="space-y-2 rounded-lg border p-3">
              <div className="flex flex-wrap items-center gap-1.5 text-xs">
                <Badge variant={result.dryRun ? "outline" : "success"} className="text-[9px]">
                  {result.dryRun ? "仅校验" : "已导入"}
                </Badge>
                <span className="text-muted-foreground">共 {result.totalRows} 行</span>
                {!result.dryRun && (
                  <>
                    <Badge variant="outline" className="text-[9px]">新增 {result.memberAdded}</Badge>
                    <Badge variant="outline" className="text-[9px]">更新 {result.memberUpdated}</Badge>
                    <Badge variant="outline" className="text-[9px]">新建部门 {result.deptCreated}</Badge>
                  </>
                )}
              </div>
              {result.issues.length === 0 ? (
                <p className="text-[11px] text-muted-foreground">没有发现问题。</p>
              ) : (
                <div className="max-h-48 space-y-1 overflow-y-auto">
                  {result.issues.map((issue, i) => (
                    <div key={i} className="flex items-start gap-1.5 text-[10px]">
                      <Badge variant={issue.fatal ? "danger" : "warning"} className="shrink-0 text-[8px]">
                        {issue.fatal ? "阻断" : "提示"}
                      </Badge>
                      <span className="text-muted-foreground">
                        {issue.rowNo > 0 && `第 ${issue.rowNo} 行 `}
                        {issue.field && `[${issue.field}] `}
                        {issue.message}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

/* ================================================================== */
/*  操作日志                                                           */
/* ================================================================== */

export function OrgActivityPanel({ orgId, access }: { orgId: string; access?: OrgAccess | null }) {
  const can = useOrgCan(access);
  const [page, setPage] = useState(1);
  const query = useOrgActivityQuery(orgId, { page, limit: 30 });

  if (!can("org:activity:read")) {
    return <EmptyState title="无权查看操作日志" description="需要「查看操作日志」权限" />;
  }

  const items = query.data?.items ?? [];
  const totalPages = query.data?.totalPages ?? 1;

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h3 className="flex items-center gap-2 text-sm font-semibold"><History className="size-4" />操作日志</h3>
        {query.isLoading ? (
          <div className="py-10 text-center text-xs text-muted-foreground">加载中…</div>
        ) : items.length === 0 ? (
          <EmptyState title="暂无操作记录" description="" />
        ) : (
          <div className="space-y-1.5">
            {items.map((log, i) => (
              <div key={`${log.createdAt}-${i}`} className="flex items-start gap-2 rounded-md border px-3 py-2">
                <Badge variant="outline" className="shrink-0 font-mono text-[9px]">{log.action}</Badge>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-xs">{log.summary}</p>
                  <p className="text-[10px] text-muted-foreground">
                    {log.actorName || "系统"} · {new Date(log.createdAt).toLocaleString("zh-CN")}
                  </p>
                </div>
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
      </CardContent>
    </Card>
  );
}

/* ================================================================== */
/*  概览                                                               */
/* ================================================================== */

export function OrgOverviewCards({ stats, roleBreakdown }: {
  stats: {
    memberTotal: number; memberActive: number; deptTotal: number; deptMaxDepth: number;
    positionTotal: number; appTotal: number; pendingInvites: number; unassignedMembers: number; childOrgs: number;
  };
  roleBreakdown: Record<string, number>;
}) {
  const { data: meta } = useOrgMetadataQuery();
  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="成员" value={stats.memberTotal} hint={`${stats.memberActive} 人在职`} />
        <StatTile label="部门" value={stats.deptTotal} hint={`层级深度 ${stats.deptMaxDepth}`} />
        <StatTile label="岗位" value={stats.positionTotal} />
        <StatTile label="绑定应用" value={stats.appTotal} />
        <StatTile label="待处理邀请" value={stats.pendingInvites} />
        <StatTile label="未分配部门" value={stats.unassignedMembers} hint="已入组织但未进任何部门" />
        <StatTile label="下级组织" value={stats.childOrgs} />
        <StatTile
          label="角色分布"
          value={Object.values(roleBreakdown).reduce((a, b) => a + b, 0)}
          hint={Object.entries(roleBreakdown)
            .map(([k, v]) => `${meta?.builtinRoles.find((r) => r.key === k)?.name ?? k} ${v}`)
            .join(" · ")}
        />
      </div>
    </div>
  );
}
