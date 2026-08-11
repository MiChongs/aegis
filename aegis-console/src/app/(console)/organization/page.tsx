"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Briefcase, Building2, ClipboardCheck, Database, FolderTree, History,
  Loader2, Mail, Network, Plus, Settings2, Shield, Users,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  useCreateOrganizationMutation, useOrgMetadataQuery, useOrgOverviewQuery,
  useOrganizationQuery, useOrganizationsQuery,
} from "@/lib/org-hooks";
import { OrgChartPanel } from "@/components/organization/org-chart";
import {
  ApprovalsPanel, OrgResourcesPanel, OrgRolesPanel, PositionsPanel,
} from "@/components/organization/org-governance";
import { InvitationsPanel, OrgMembersPanel, PendingBadge } from "@/components/organization/org-members";
import {
  OrgActivityPanel, OrgDataPanel, OrgOverviewCards, OrgSettingsPanel,
} from "@/components/organization/org-settings";
import { OrgStructurePanel, OrgSwitcher } from "@/components/organization/org-structure";
import { StatusBadge } from "@/components/organization/org-shared";

/**
 * 组织与成员中心。
 *
 * 当前组织与 Tab 都同步到 URL（?org= / ?tab=），这样：
 *   · 侧边栏三级子项可以直接深链到某个面板
 *   · 「把这个组织的成员页发给同事」变成一件可行的事
 * 组织标识用的是后端下发的 UUID，不再暴露自增 ID。
 */
export default function OrganizationPage() {
  return (
    <Suspense fallback={<LoadingState title="加载组织" description="" />}>
      <OrganizationCenter />
    </Suspense>
  );
}

function OrganizationCenter() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const orgParam = searchParams.get("org");
  const tabParam = searchParams.get("tab") ?? "overview";

  const orgsQuery = useOrganizationsQuery({ limit: 100 });
  const organizations = orgsQuery.data?.items ?? [];

  // 当前组织由 URL 单一驱动，不另存一份本地 state。
  // 用 effect 把 query 同步进 state 会触发 react-hooks/set-state-in-effect，
  // 也会让「深链进来」和「页面内点击」走两条不同的路径。
  const activeOrg = orgParam ?? organizations[0]?.id ?? null;

  const detailQuery = useOrganizationQuery(activeOrg);
  const overviewQuery = useOrgOverviewQuery(activeOrg);

  const org = detailQuery.data?.organization;
  const access = detailQuery.data?.access;

  const setParams = (next: { org?: string | null; tab?: string }) => {
    const params = new URLSearchParams(searchParams.toString());
    if (next.org !== undefined) {
      if (next.org) params.set("org", next.org);
      else params.delete("org");
    }
    if (next.tab) params.set("tab", next.tab);
    router.replace(`?${params.toString()}`, { scroll: false });
  };

  if (orgsQuery.isLoading) return <LoadingState title="加载组织" description="" />;

  return (
    <div className="page-stack">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <SectionHeading eyebrow="控制台" title="组织与成员中心" />
        <CreateOrgDialog
          organizations={organizations}
          onCreated={(id) => setParams({ org: id, tab: "overview" })}
        />
      </div>

      {organizations.length === 0 ? (
        <EmptyState
          title="还没有组织"
          description="创建第一个组织后，就可以在这里管理部门、成员、岗位与权限"
        />
      ) : (
        <>
          <OrgSwitcher
            organizations={organizations}
            activeId={activeOrg}
            onSelect={(id) => setParams({ org: id })}
          />

          {detailQuery.isLoading || !org ? (
            <LoadingState title="加载组织详情" description="" />
          ) : (
            <>
              <OrgHeader org={org} />

              <Tabs value={tabParam} onValueChange={(tab) => setParams({ tab })}>
                <TabsList className="flex-wrap">
                  <TabsTrigger value="overview"><Building2 className="size-3.5" />概览</TabsTrigger>
                  <TabsTrigger value="structure"><FolderTree className="size-3.5" />组织结构</TabsTrigger>
                  <TabsTrigger value="members"><Users className="size-3.5" />成员</TabsTrigger>
                  <TabsTrigger value="chart"><Network className="size-3.5" />架构图</TabsTrigger>
                  <TabsTrigger value="positions"><Briefcase className="size-3.5" />岗位</TabsTrigger>
                  <TabsTrigger value="roles"><Shield className="size-3.5" />角色权限</TabsTrigger>
                  <TabsTrigger value="approvals"><ClipboardCheck className="size-3.5" />审批</TabsTrigger>
                  <TabsTrigger value="invitations" className="gap-1.5">
                    <Mail className="size-3.5" />邀请<PendingBadge />
                  </TabsTrigger>
                  <TabsTrigger value="resources"><Database className="size-3.5" />资源</TabsTrigger>
                  <TabsTrigger value="data"><Database className="size-3.5" />导入导出</TabsTrigger>
                  <TabsTrigger value="activity"><History className="size-3.5" />日志</TabsTrigger>
                  <TabsTrigger value="settings"><Settings2 className="size-3.5" />设置</TabsTrigger>
                </TabsList>

                <TabsContent value="overview" className="mt-3 space-y-4">
                  {overviewQuery.data ? (
                    <>
                      <OrgOverviewCards
                        stats={overviewQuery.data.stats}
                        roleBreakdown={overviewQuery.data.roleBreakdown}
                      />
                      {overviewQuery.data.topDepartments.length > 0 && (
                        <Card>
                          <CardContent className="space-y-2 p-4">
                            <h3 className="text-sm font-semibold">人数最多的部门</h3>
                            <div className="flex flex-wrap gap-1.5">
                              {overviewQuery.data.topDepartments.map((d) => (
                                <Badge key={d.id} variant="outline">{d.name}</Badge>
                              ))}
                            </div>
                          </CardContent>
                        </Card>
                      )}
                      {overviewQuery.data.recentActivity.length > 0 && (
                        <Card>
                          <CardContent className="space-y-2 p-4">
                            <h3 className="text-sm font-semibold">最近动态</h3>
                            <div className="space-y-1.5">
                              {overviewQuery.data.recentActivity.map((log, i) => (
                                <div key={`${log.createdAt}-${i}`} className="flex items-center gap-2 text-xs">
                                  <span className="text-muted-foreground">
                                    {new Date(log.createdAt).toLocaleString("zh-CN")}
                                  </span>
                                  <span className="truncate">{log.summary}</span>
                                  <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">
                                    {log.actorName}
                                  </span>
                                </div>
                              ))}
                            </div>
                          </CardContent>
                        </Card>
                      )}
                    </>
                  ) : (
                    <div className="py-10 text-center text-xs text-muted-foreground">加载概览…</div>
                  )}
                </TabsContent>

                <TabsContent value="structure" className="mt-3">
                  <OrgStructurePanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="members" className="mt-3">
                  <OrgMembersPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="chart" className="mt-3">
                  <OrgChartPanel orgId={org.id} />
                </TabsContent>

                <TabsContent value="positions" className="mt-3">
                  <PositionsPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="roles" className="mt-3">
                  <OrgRolesPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="approvals" className="mt-3">
                  <ApprovalsPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="invitations" className="mt-3">
                  <InvitationsPanel />
                </TabsContent>

                <TabsContent value="resources" className="mt-3">
                  <OrgResourcesPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="data" className="mt-3">
                  <OrgDataPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="activity" className="mt-3">
                  <OrgActivityPanel orgId={org.id} access={access} />
                </TabsContent>

                <TabsContent value="settings" className="mt-3">
                  <OrgSettingsPanel
                    org={org} access={access}
                    onDeleted={() => setParams({ org: null, tab: "overview" })}
                  />
                </TabsContent>
              </Tabs>
            </>
          )}
        </>
      )}
    </div>
  );
}

function OrgHeader({ org }: { org: import("@/lib/api/types").Organization }) {
  return (
    <Card>
      <CardContent className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-semibold">{org.name}</h2>
            <Badge variant="outline" className="font-mono text-[9px]">{org.code}</Badge>
            <StatusBadge status={org.status} />
            {org.parentName && (
              <Badge variant="outline" className="text-[9px]">隶属于 {org.parentName}</Badge>
            )}
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {org.description || "暂无简介"}
            {org.ownerName && <span className="ml-2">所有者：{org.ownerName}</span>}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-4 text-center">
          <div>
            <div className="text-lg font-semibold tabular-nums">{org.stats.memberCount}</div>
            <div className="text-[10px] text-muted-foreground">成员</div>
          </div>
          <div>
            <div className="text-lg font-semibold tabular-nums">{org.stats.deptCount}</div>
            <div className="text-[10px] text-muted-foreground">部门</div>
          </div>
          <div>
            <div className="text-lg font-semibold tabular-nums">{org.stats.appCount}</div>
            <div className="text-[10px] text-muted-foreground">应用</div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function CreateOrgDialog({
  organizations, onCreated,
}: {
  organizations: { id: string; name: string }[];
  onCreated: (id: string) => void;
}) {
  const mutation = useCreateOrganizationMutation();
  const { data: meta } = useOrgMetadataQuery();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [kind, setKind] = useState("enterprise");
  const [parentId, setParentId] = useState("__root__");
  const [description, setDescription] = useState("");

  const submit = async () => {
    if (!name.trim() || !code.trim()) { toast.error("请填写组织名称与代码"); return; }
    try {
      const org = await mutation.mutateAsync({
        name, code, kind, description,
        parentId: parentId === "__root__" ? undefined : parentId,
      });
      toast.success("组织已创建");
      setOpen(false);
      setName(""); setCode(""); setDescription("");
      onCreated(org.id);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "创建失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm"><Plus className="size-3.5" />新建组织</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>创建组织</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">组织名称</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="示例科技有限公司" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">组织代码</Label>
              <Input
                value={code} className="font-mono"
                onChange={(e) => setCode(e.target.value.replace(/[^a-zA-Z0-9\-_]/g, ""))}
                placeholder="example-tech"
              />
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">类型</Label>
              <Select value={kind} onValueChange={setKind}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(meta?.orgKinds ?? []).map((k) => <SelectItem key={k.value} value={k.value}>{k.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">上级组织</Label>
              <Select value={parentId} onValueChange={setParentId}>
                <SelectTrigger className="h-9 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="__root__">（顶级组织）</SelectItem>
                  {organizations.map((o) => <SelectItem key={o.id} value={o.id}>{o.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">简介</Label>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
          </div>
          <p className="text-[10px] text-muted-foreground">
            创建顶级组织需要平台管理员权限；创建下级组织只需在上级组织中拥有管理权限。
            你将自动成为新组织的所有者。
          </p>
          <Button className="w-full" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="size-3.5 animate-spin" />} 创建组织
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
