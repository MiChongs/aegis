"use client";

import { Suspense, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  AlertTriangle, Ban, BookUser, CheckCircle2, FileWarning, Fingerprint, Globe2,
  KeyRound, Loader2, Plus, Search, ShieldAlert, ShieldCheck, Tag, Trash2, Users, UserX, Wifi, X, XCircle
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { SectionHeading } from "@/components/ui/section-heading";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { AdminPanel } from "@/components/users/admin-panel";
import { AppUsersPanel } from "@/components/users/app-users/panel";
import { OnlinePanel } from "@/components/users/online-panel";
import { RolePermissionsPanel } from "@/components/users/role-permissions-panel";
import {
  useGlobalIdentitiesQuery, useGlobalIdentityQuery, useUpdateIdentityStatusMutation,
  useUpdateIdentityRiskMutation, useUpdateIdentityLifecycleMutation, useSyncIdentitiesMutation,
  useUserMasterTagsQuery, useCreateUserMasterTagMutation, useDeleteUserMasterTagMutation,
  useAssignTagMutation, useRemoveTagMutation,
  useUserSegmentsQuery, useCreateUserSegmentMutation, useDeleteUserSegmentMutation,
  useUserListEntriesQuery, useCreateUserListEntryMutation, useDeleteUserListEntryMutation,
  useUserAppealsQuery, useReviewAppealMutation,
  useDeactivationsQuery, useCancelDeactivationMutation, useDeactivateIdentityMutation,
  useAdminAppsQuery,
} from "@/lib/admin-hooks";
import type { GlobalIdentity, UserMasterTag } from "@/lib/api/types";
import { cn } from "@/lib/utils";

function UsersPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "admins";

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="用户与身份中心" />

      <Tabs value={tab} onValueChange={(v) => router.replace(`/users?tab=${v}`, { scroll: false })} className="space-y-6">
        <TabsList className="flex-wrap">
          <TabsTrigger value="admins"><ShieldCheck className="size-3.5" />管理员</TabsTrigger>
          <TabsTrigger value="app-users"><Users className="size-3.5" />应用用户</TabsTrigger>
          <TabsTrigger value="online"><Wifi className="size-3.5" />在线会话</TabsTrigger>
          <TabsTrigger value="roles"><KeyRound className="size-3.5" />角色权限</TabsTrigger>
          <TabsTrigger value="identities"><Fingerprint className="size-3.5" />用户主档</TabsTrigger>
          <TabsTrigger value="tags"><Tag className="size-3.5" />标签管理</TabsTrigger>
          <TabsTrigger value="segments"><BookUser className="size-3.5" />分群管理</TabsTrigger>
          <TabsTrigger value="lists"><Ban className="size-3.5" />黑白名单</TabsTrigger>
          <TabsTrigger value="risk"><ShieldAlert className="size-3.5" />风险用户</TabsTrigger>
          <TabsTrigger value="appeals"><FileWarning className="size-3.5" />用户申诉</TabsTrigger>
          <TabsTrigger value="deactivation"><UserX className="size-3.5" />注销管理</TabsTrigger>
        </TabsList>

        <TabsContent value="admins"><AdminPanel /></TabsContent>
        <TabsContent value="app-users"><AppUsersPanel /></TabsContent>
        <TabsContent value="online"><OnlinePanel /></TabsContent>
        <TabsContent value="roles"><RolePermissionsPanel /></TabsContent>
        <TabsContent value="identities"><IdentitiesPanel /></TabsContent>
        <TabsContent value="tags"><TagsPanel /></TabsContent>
        <TabsContent value="segments"><SegmentsPanel /></TabsContent>
        <TabsContent value="lists"><ListsPanel /></TabsContent>
        <TabsContent value="risk"><RiskPanel /></TabsContent>
        <TabsContent value="appeals"><AppealsPanel /></TabsContent>
        <TabsContent value="deactivation"><DeactivationPanel /></TabsContent>
      </Tabs>
    </div>
  );
}

export default function UsersPage() {
  return <Suspense><UsersPageInner /></Suspense>;
}

/* ================================================================== */
/*  用户主档面板                                                       */
/* ================================================================== */

function IdentitiesPanel() {
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState("");
  const [lifecycle, setLifecycle] = useState("");
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<number | null>(null);
  const query = useGlobalIdentitiesQuery({ keyword: keyword || undefined, status: status || undefined, lifecycleState: lifecycle || undefined, page, limit: 20 });
  const syncMut = useSyncIdentitiesMutation();
  const appsQuery = useAdminAppsQuery();
  const [syncAppId, setSyncAppId] = useState("");

  const items = query.data?.items || [];
  const total = query.data?.total || 0;

  const statusLabels: Record<string, string> = { active: "活跃", frozen: "冻结", deactivating: "注销中", deleted: "已删除" };
  const lifecycleLabels: Record<string, string> = { registered: "已注册", active: "活跃", silent: "沉默", churned: "流失", returning: "回归", deactivating: "注销中", deleted: "已删除" };
  const riskColors: Record<string, string> = { normal: "", low: "text-blue-600", medium: "text-amber-600", high: "text-orange-600", critical: "text-red-600" };

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 flex-wrap">
        <div className="relative flex-1 min-w-48 max-w-sm">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
          <Input value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }} placeholder="搜索邮箱/手机/姓名..." className="h-8 pl-8 text-xs" />
        </div>
        <Select value={status} onValueChange={(v) => { setStatus(v === "all" ? "" : v); setPage(1); }}>
          <SelectTrigger className="h-8 w-24 text-xs"><SelectValue placeholder="状态" /></SelectTrigger>
          <SelectContent><SelectItem value="all">全部</SelectItem>{Object.entries(statusLabels).map(([k, v]) => <SelectItem key={k} value={k}>{v}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={lifecycle} onValueChange={(v) => { setLifecycle(v === "all" ? "" : v); setPage(1); }}>
          <SelectTrigger className="h-8 w-24 text-xs"><SelectValue placeholder="生命周期" /></SelectTrigger>
          <SelectContent><SelectItem value="all">全部</SelectItem>{Object.entries(lifecycleLabels).map(([k, v]) => <SelectItem key={k} value={k}>{v}</SelectItem>)}</SelectContent>
        </Select>
        <div className="flex items-center gap-1 ml-auto">
          <Select value={syncAppId} onValueChange={setSyncAppId}>
            <SelectTrigger className="h-8 w-32 text-xs"><SelectValue placeholder="选择应用同步" /></SelectTrigger>
            <SelectContent>{(appsQuery.data || []).map((a) => <SelectItem key={a.id} value={String(a.id)}>{a.name}</SelectItem>)}</SelectContent>
          </Select>
          <Button size="sm" className="h-8" disabled={!syncAppId || syncMut.isPending} onClick={async () => {
            try { const r = await syncMut.mutateAsync(Number(syncAppId)); toast.success(`已同步 ${(r as { synced?: number })?.synced || 0} 个用户`); }
            catch (e) { toast.error(e instanceof ApiError ? e.message : "同步失败"); }
          }}>{syncMut.isPending ? <Loader2 className="size-3 animate-spin" /> : <Globe2 className="size-3" />}同步</Button>
        </div>
      </div>

      <Card><CardContent className="p-0">
        <div className="divide-y">
          {query.isLoading ? <div className="py-12 text-center text-sm text-muted-foreground">加载中...</div> : items.length === 0 ? <div className="py-12 text-center text-sm text-muted-foreground">暂无数据</div> : items.map((item) => (
            <button type="button" key={item.id} className="flex items-center gap-3 w-full px-4 py-2.5 text-left text-xs hover:bg-muted/30 transition-colors" onClick={() => setDetailId(item.id)}>
              <Fingerprint className="size-4 shrink-0 text-muted-foreground" />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5"><span className="font-medium">{item.displayName || item.email || item.phone || `#${item.id}`}</span>
                  <Badge variant={item.status === "active" ? "success" : item.status === "frozen" ? "danger" : "outline"} className="text-[9px]">{statusLabels[item.status] || item.status}</Badge>
                  <Badge variant="outline" className="text-[9px]">{lifecycleLabels[item.lifecycleState] || item.lifecycleState}</Badge>
                  {item.riskLevel !== "normal" && <Badge variant="danger" className="text-[9px]">{item.riskLevel}</Badge>}
                </div>
                <div className="text-muted-foreground">{[item.email, item.phone].filter(Boolean).join(" · ") || "--"}</div>
              </div>
              <span className="text-muted-foreground shrink-0">{new Date(item.createdAt).toLocaleDateString("zh-CN")}</span>
            </button>
          ))}
        </div>
        <div className="flex items-center justify-between px-4 py-2 border-t text-xs text-muted-foreground">
          <span>共 {total} 个身份</span>
          <div className="flex gap-1.5">
            <Button variant="outline" size="sm" className="h-6 text-[10px]" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
            <span>{page} / {Math.ceil(total / 20) || 1}</span>
            <Button variant="outline" size="sm" className="h-6 text-[10px]" disabled={page * 20 >= total} onClick={() => setPage(page + 1)}>下一页</Button>
          </div>
        </div>
      </CardContent></Card>

      {detailId && <IdentityDetailSheet id={detailId} onClose={() => setDetailId(null)} />}
    </div>
  );
}

function IdentityDetailSheet({ id, onClose }: { id: number; onClose: () => void }) {
  const query = useGlobalIdentityQuery(id);
  const statusMut = useUpdateIdentityStatusMutation();
  const riskMut = useUpdateIdentityRiskMutation();
  const item = query.data;

  if (!item) return null;
  return (
    <Sheet open onOpenChange={(o) => { if (!o) onClose(); }}>
      <SheetContent className="sm:max-w-lg overflow-y-auto">
        <SheetHeader><SheetTitle>身份详情 #{item.id}</SheetTitle></SheetHeader>
        <div className="space-y-4 mt-4 text-xs">
          <div className="grid gap-2 grid-cols-2">
            <div><span className="text-muted-foreground">邮箱</span><p className="font-mono">{item.email || "--"}</p></div>
            <div><span className="text-muted-foreground">手机</span><p className="font-mono">{item.phone || "--"}</p></div>
            <div><span className="text-muted-foreground">显示名</span><p>{item.displayName || "--"}</p></div>
            <div><span className="text-muted-foreground">状态</span><p>{item.status}</p></div>
            <div><span className="text-muted-foreground">生命周期</span><p>{item.lifecycleState}</p></div>
            <div><span className="text-muted-foreground">风险</span><p>{item.riskLevel} ({item.riskScore})</p></div>
          </div>
          {item.tags && item.tags.length > 0 && (
            <div className="flex flex-wrap gap-1">{item.tags.map((t) => <Badge key={t.id} style={{ backgroundColor: t.color + "20", color: t.color, borderColor: t.color + "40" }} className="text-[9px]">{t.name}</Badge>)}</div>
          )}
          {item.mappings && item.mappings.length > 0 && (<>
            <Separator />
            <h4 className="text-xs font-semibold">关联应用</h4>
            <div className="space-y-1">{item.mappings.map((m) => (
              <div key={m.id} className="flex items-center gap-2 rounded border px-2 py-1">
                <span className="font-medium">{m.appName || `APP #${m.appId}`}</span>
                <span className="text-muted-foreground">用户 #{m.userId}</span>
                {m.account && <span className="text-muted-foreground">{m.account}</span>}
              </div>
            ))}</div>
          </>)}
          <Separator />
          <div className="flex gap-2">
            <Button size="sm" variant={item.status === "frozen" ? "default" : "outline"} onClick={async () => {
              const next = item.status === "frozen" ? "active" : "frozen";
              try { await statusMut.mutateAsync({ id: item.id, status: next }); toast.success(next === "frozen" ? "已冻结" : "已解冻"); onClose(); }
              catch (e) { toast.error(e instanceof ApiError ? e.message : "操作失败"); }
            }}>{item.status === "frozen" ? "解冻" : "冻结"}</Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

/* ================================================================== */
/*  标签管理面板                                                       */
/* ================================================================== */

function TagsPanel() {
  const query = useUserMasterTagsQuery();
  const createMut = useCreateUserMasterTagMutation();
  const deleteMut = useDeleteUserMasterTagMutation();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [color, setColor] = useState("#6366f1");
  const tags = query.data || [];

  return (
    <Card><CardContent className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold flex items-center gap-2"><Tag className="size-4" />标签管理</h3>
        <Button size="sm" onClick={() => setCreating(!creating)}><Plus className="size-3.5" />新建标签</Button>
      </div>
      {creating && (
        <div className="flex items-end gap-2 rounded-lg border p-3">
          <div className="space-y-1 flex-1"><Label className="text-xs">名称</Label><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="VIP" className="h-8 text-xs" /></div>
          <div className="space-y-1"><Label className="text-xs">颜色</Label><Input type="color" value={color} onChange={(e) => setColor(e.target.value)} className="h-8 w-12 p-1" /></div>
          <Button size="sm" disabled={createMut.isPending} onClick={async () => {
            if (!name.trim()) { toast.error("请填写名称"); return; }
            try { await createMut.mutateAsync({ name, color }); toast.success("标签已创建"); setCreating(false); setName(""); }
            catch (e) { toast.error(e instanceof ApiError ? e.message : "创建失败"); }
          }}>创建</Button>
          <Button size="sm" variant="ghost" onClick={() => setCreating(false)}>取消</Button>
        </div>
      )}
      {tags.length === 0 ? <EmptyState title="暂无标签" description="" /> : (
        <div className="flex flex-wrap gap-2">{tags.map((t) => (
          <div key={t.id} className="flex items-center gap-1.5 rounded-full border px-3 py-1 group" style={{ borderColor: t.color + "40" }}>
            <span className="size-2.5 rounded-full" style={{ backgroundColor: t.color }} />
            <span className="text-xs font-medium">{t.name}</span>
            <button type="button" title="删除标签" className="hidden group-hover:block" onClick={async () => {
              try { await deleteMut.mutateAsync(t.id); toast.success("已删除"); } catch {}
            }}><X className="size-3 text-muted-foreground hover:text-destructive" /></button>
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  分群管理面板                                                       */
/* ================================================================== */

function SegmentsPanel() {
  const query = useUserSegmentsQuery();
  const createMut = useCreateUserSegmentMutation();
  const deleteMut = useDeleteUserSegmentMutation();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const segments = query.data || [];

  return (
    <Card><CardContent className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold flex items-center gap-2"><BookUser className="size-4" />用户分群</h3>
        <Button size="sm" onClick={() => setCreating(!creating)}><Plus className="size-3.5" />新建分群</Button>
      </div>
      {creating && (
        <div className="flex items-end gap-2 rounded-lg border p-3">
          <div className="space-y-1 flex-1"><Label className="text-xs">名称</Label><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="高价值用户" className="h-8 text-xs" /></div>
          <Button size="sm" disabled={createMut.isPending} onClick={async () => {
            if (!name.trim()) { toast.error("请填写名称"); return; }
            try { await createMut.mutateAsync({ name, segmentType: "static" }); toast.success("分群已创建"); setCreating(false); setName(""); }
            catch (e) { toast.error(e instanceof ApiError ? e.message : "创建失败"); }
          }}>创建</Button>
        </div>
      )}
      {segments.length === 0 ? <EmptyState title="暂无分群" description="" /> : (
        <div className="space-y-2">{segments.map((s) => (
          <div key={s.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 group">
            <div className="flex-1"><span className="text-sm font-medium">{s.name}</span>
              <div className="text-xs text-muted-foreground flex gap-2"><Badge variant="outline" className="text-[9px]">{s.segmentType}</Badge><span>{s.memberCount} 人</span></div>
            </div>
            <Button variant="ghost" size="sm" className="h-6 px-1.5 hidden group-hover:flex text-destructive" onClick={async () => {
              try { await deleteMut.mutateAsync(s.id); toast.success("已删除"); } catch {}
            }}><Trash2 className="size-3" /></Button>
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  黑白名单面板                                                       */
/* ================================================================== */

function ListsPanel() {
  const [listType, setListType] = useState("blacklist");
  const query = useUserListEntriesQuery(listType);
  const createMut = useCreateUserListEntryMutation();
  const deleteMut = useDeleteUserListEntryMutation();
  const [creating, setCreating] = useState(false);
  const [value, setValue] = useState("");
  const [reason, setReason] = useState("");
  const [field, setField] = useState("email");
  const items = query.data?.items || [];

  return (
    <Card><CardContent className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold flex items-center gap-2"><Ban className="size-4" />{listType === "blacklist" ? "黑名单" : "白名单"}</h3>
          <Select value={listType} onValueChange={setListType}>
            <SelectTrigger className="h-7 w-20 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value="blacklist">黑名单</SelectItem><SelectItem value="whitelist">白名单</SelectItem></SelectContent>
          </Select>
        </div>
        <Button size="sm" onClick={() => setCreating(!creating)}><Plus className="size-3.5" />添加</Button>
      </div>
      {creating && (
        <div className="flex items-end gap-2 flex-wrap rounded-lg border p-3">
          <div className="space-y-1"><Label className="text-xs">类型</Label>
            <Select value={field} onValueChange={setField}><SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="email">邮箱</SelectItem><SelectItem value="phone">手机</SelectItem><SelectItem value="ip">IP</SelectItem></SelectContent>
            </Select>
          </div>
          <div className="space-y-1 flex-1"><Label className="text-xs">值</Label><Input value={value} onChange={(e) => setValue(e.target.value)} placeholder={field === "email" ? "user@example.com" : field === "ip" ? "1.2.3.4" : "+8613800000000"} className="h-8 text-xs" /></div>
          <div className="space-y-1 flex-1"><Label className="text-xs">原因</Label><Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="恶意注册" className="h-8 text-xs" /></div>
          <Button size="sm" disabled={createMut.isPending} onClick={async () => {
            if (!value.trim()) { toast.error("请填写值"); return; }
            try { await createMut.mutateAsync({ listType, [field]: value.trim(), reason: reason.trim() }); toast.success("已添加"); setCreating(false); setValue(""); setReason(""); }
            catch (e) { toast.error(e instanceof ApiError ? e.message : "添加失败"); }
          }}>添加</Button>
        </div>
      )}
      {items.length === 0 ? <EmptyState title="暂无记录" description="" /> : (
        <div className="space-y-1.5">{items.map((e) => (
          <div key={e.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 text-xs group">
            <div className="flex-1"><span className="font-medium font-mono">{e.email || e.phone || e.ip || `ID #${e.identityId}`}</span>
              {e.reason && <p className="text-muted-foreground">{e.reason}</p>}
            </div>
            <span className="text-muted-foreground shrink-0">{new Date(e.createdAt).toLocaleDateString("zh-CN")}</span>
            <Button variant="ghost" size="sm" className="h-6 px-1.5 hidden group-hover:flex text-destructive" onClick={async () => {
              try { await deleteMut.mutateAsync(e.id); toast.success("已删除"); } catch {}
            }}><Trash2 className="size-3" /></Button>
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  风险用户面板                                                       */
/* ================================================================== */

function RiskPanel() {
  const query = useGlobalIdentitiesQuery({ riskLevel: "high", page: 1, limit: 50 });
  const riskMut = useUpdateIdentityRiskMutation();
  const items = query.data?.items || [];

  return (
    <Card><CardContent className="p-4 space-y-4">
      <h3 className="text-sm font-semibold flex items-center gap-2"><ShieldAlert className="size-4" />风险用户池</h3>
      {items.length === 0 ? <EmptyState title="暂无风险用户" description="风险等级为 high 或 critical 的用户将出现在此处" /> : (
        <div className="space-y-2">{items.map((item) => (
          <div key={item.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 text-xs">
            <AlertTriangle className="size-4 text-orange-500 shrink-0" />
            <div className="flex-1 min-w-0">
              <span className="font-medium">{item.displayName || item.email || `#${item.id}`}</span>
              <div className="text-muted-foreground">{item.email} · 风险分 {item.riskScore}</div>
            </div>
            <Badge variant="danger" className="text-[9px]">{item.riskLevel}</Badge>
            <Button size="sm" variant="outline" className="h-6 text-[10px]" onClick={async () => {
              try { await riskMut.mutateAsync({ id: item.id, riskScore: 0, riskLevel: "normal" }); toast.success("已标记为正常"); }
              catch (e) { toast.error(e instanceof ApiError ? e.message : "操作失败"); }
            }}>标记正常</Button>
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  用户申诉面板                                                       */
/* ================================================================== */

function AppealsPanel() {
  const [statusFilter, setStatusFilter] = useState("pending");
  const query = useUserAppealsQuery(statusFilter);
  const reviewMut = useReviewAppealMutation();
  const items = query.data?.items || [];
  const appealLabels: Record<string, string> = { unfreeze: "解冻申请", unban: "解封申请", data_request: "数据请求", deletion_cancel: "取消注销" };
  const statusLabels: Record<string, string> = { pending: "待审核", approved: "已通过", rejected: "已拒绝" };

  return (
    <Card><CardContent className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold flex items-center gap-2"><FileWarning className="size-4" />用户申诉</h3>
        <Select value={statusFilter} onValueChange={setStatusFilter}>
          <SelectTrigger className="h-7 w-24 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="pending">待审核</SelectItem><SelectItem value="approved">已通过</SelectItem><SelectItem value="rejected">已拒绝</SelectItem></SelectContent>
        </Select>
      </div>
      {items.length === 0 ? <EmptyState title="暂无申诉" description="" /> : (
        <div className="space-y-2">{items.map((a) => (
          <div key={a.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 text-xs">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1.5"><span className="font-medium">{a.identityName || `身份 #${a.identityId}`}</span>
                <Badge variant="outline" className="text-[9px]">{appealLabels[a.appealType] || a.appealType}</Badge>
                <Badge variant={a.status === "approved" ? "success" : a.status === "rejected" ? "danger" : "outline"} className="text-[9px]">{statusLabels[a.status]}</Badge>
              </div>
              <p className="text-muted-foreground mt-0.5">{a.reason}</p>
            </div>
            {a.status === "pending" && (
              <div className="flex gap-1 shrink-0">
                <Button size="sm" className="h-6 text-[10px]" onClick={async () => {
                  try { await reviewMut.mutateAsync({ id: a.id, action: "approved" }); toast.success("已通过"); } catch {}
                }}><CheckCircle2 className="size-3" /></Button>
                <Button size="sm" variant="outline" className="h-6 text-[10px]" onClick={async () => {
                  try { await reviewMut.mutateAsync({ id: a.id, action: "rejected" }); toast.success("已拒绝"); } catch {}
                }}><XCircle className="size-3" /></Button>
              </div>
            )}
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}

/* ================================================================== */
/*  注销管理面板                                                       */
/* ================================================================== */

function DeactivationPanel() {
  const query = useDeactivationsQuery();
  const cancelMut = useCancelDeactivationMutation();
  const items = query.data || [];

  return (
    <Card><CardContent className="p-4 space-y-4">
      <h3 className="text-sm font-semibold flex items-center gap-2"><UserX className="size-4" />注销请求</h3>
      {items.length === 0 ? <EmptyState title="暂无注销请求" description="" /> : (
        <div className="space-y-2">{items.map((d) => (
          <div key={d.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 text-xs">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1.5"><span className="font-medium">{d.identityName || `身份 #${d.identityId}`}</span>
                <Badge variant={d.status === "pending" ? "outline" : d.status === "cancelled" ? "success" : "danger"} className="text-[9px]">{d.status === "pending" ? "冷静期" : d.status === "cancelled" ? "已取消" : "已完成"}</Badge>
              </div>
              <p className="text-muted-foreground">{d.reason || "无"} · 冷静 {d.coolingDays} 天 · 截止 {new Date(d.scheduledAt).toLocaleDateString("zh-CN")}</p>
            </div>
            {d.status === "pending" && (
              <Button size="sm" variant="outline" className="h-6 text-[10px]" onClick={async () => {
                try { await cancelMut.mutateAsync(d.id); toast.success("已取消注销"); } catch (e) { toast.error(e instanceof ApiError ? e.message : "取消失败"); }
              }}>取消注销</Button>
            )}
          </div>
        ))}</div>
      )}
    </CardContent></Card>
  );
}
