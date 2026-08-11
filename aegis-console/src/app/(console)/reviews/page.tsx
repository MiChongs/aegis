"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import {
  useAdminAppsQuery,
  useAdminRoleApplicationStatisticsQuery,
  useAdminRoleApplicationsQuery,
  useAdminSiteAuditStatsQuery,
  useAdminSiteAuditsQuery,
  useAuditAdminSiteMutation,
  useReviewAdminRoleApplicationMutation,
  useToggleAdminSitePinMutation
} from "@/lib/admin-hooks";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { SurfaceCard } from "@/components/ui/surface-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

function formatDateTime(value?: string | null) {
  if (!value) {
    return "未记录";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "未记录";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

export default function ReviewsPage() {
  const appsQuery = useAdminAppsQuery();
  const [selectedAppId, setSelectedAppId] = useState<number | null>(null);
  const [siteStatus, setSiteStatus] = useState("pending");
  const [roleStatus, setRoleStatus] = useState("pending");

  const resolvedAppId = selectedAppId ?? (appsQuery.data || [])[0]?.id ?? null;
  const selectedApp = (appsQuery.data || []).find((item) => item.id === resolvedAppId) || null;
  const siteAuditsQuery = useAdminSiteAuditsQuery(selectedApp?.id || null, { page: 1, limit: 20, status: siteStatus });
  const siteStatsQuery = useAdminSiteAuditStatsQuery(selectedApp?.id || null);
  const roleAppsQuery = useAdminRoleApplicationsQuery(selectedApp?.id || null, { page: 1, limit: 20, status: roleStatus });
  const roleStatsQuery = useAdminRoleApplicationStatisticsQuery(selectedApp?.id || null);
  const auditSiteMutation = useAuditAdminSiteMutation();
  const togglePinMutation = useToggleAdminSitePinMutation();
  const reviewRoleMutation = useReviewAdminRoleApplicationMutation();

  if (appsQuery.isLoading) {
    return <LoadingState title="正在加载审核模块" description="正在读取站点与角色申请数据。" />;
  }

  if (!selectedApp) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Reviews" title="审核" description="当前没有可管理的应用。" />
        <EmptyState title="暂无应用" description="请先创建应用。" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Reviews"
        title="审核"
        description="站点审核与角色申请审核。"
        action={
          <Select value={String(resolvedAppId)} onValueChange={(value) => setSelectedAppId(Number(value))}>
            <SelectTrigger className="w-[240px]"><SelectValue placeholder="选择应用" /></SelectTrigger>
            <SelectContent>
              {(appsQuery.data || []).map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>{item.name} ({item.id})</SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
      />

      <section className="metrics-grid">
        <SurfaceCard><div className="space-y-1"><div className="text-sm text-muted-foreground">站点待审</div><div className="text-2xl font-semibold text-foreground">{siteStatsQuery.data?.pending || 0}</div></div></SurfaceCard>
        <SurfaceCard><div className="space-y-1"><div className="text-sm text-muted-foreground">站点已通过</div><div className="text-2xl font-semibold text-foreground">{siteStatsQuery.data?.approved || 0}</div></div></SurfaceCard>
        <SurfaceCard><div className="space-y-1"><div className="text-sm text-muted-foreground">角色待审</div><div className="text-2xl font-semibold text-foreground">{roleStatsQuery.data?.pending || 0}</div></div></SurfaceCard>
        <SurfaceCard><div className="space-y-1"><div className="text-sm text-muted-foreground">角色已通过</div><div className="text-2xl font-semibold text-foreground">{roleStatsQuery.data?.approved || 0}</div></div></SurfaceCard>
      </section>

      <Tabs defaultValue="sites" className="space-y-5">
        <TabsList>
          <TabsTrigger value="sites">站点审核</TabsTrigger>
          <TabsTrigger value="roles">角色申请</TabsTrigger>
        </TabsList>

        <TabsContent value="sites">
          <SurfaceCard>
            <div className="flex items-center justify-between pb-4">
              <Select value={siteStatus} onValueChange={setSiteStatus}>
                <SelectTrigger className="w-[220px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="pending">待审核</SelectItem>
                  <SelectItem value="approved">已通过</SelectItem>
                  <SelectItem value="rejected">已拒绝</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {!siteAuditsQuery.data?.list?.length ? (
              <EmptyState title="暂无站点记录" description="当前筛选条件下没有站点审核记录。" />
            ) : (
              <div className="table-shell">
                <Table>
                  <TableHeader><TableRow><TableHead>站点</TableHead><TableHead>提交用户</TableHead><TableHead>状态</TableHead><TableHead>更新时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
                  <TableBody>
                    {siteAuditsQuery.data.list.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell>
                          <div className="space-y-1">
                            <div className="font-semibold text-foreground">{item.name || `站点 ${item.id}`}</div>
                            <div className="text-xs text-muted-foreground">{item.url || "未记录地址"}</div>
                          </div>
                        </TableCell>
                        <TableCell>{item.nickname || item.account || `用户 ${item.userId || ""}`}</TableCell>
                        <TableCell><Badge variant={item.audit_status === "approved" ? "success" : item.audit_status === "rejected" ? "warning" : "secondary"}>{item.audit_status || item.status || "pending"}</Badge></TableCell>
                        <TableCell className="text-muted-foreground">{formatDateTime(item.updatedAt)}</TableCell>
                        <TableCell className="text-right">
                          <div className="flex justify-end gap-2">
                            <Button variant="outline" size="sm" onClick={() => void auditSiteMutation.mutateAsync({ appid: selectedApp.id, siteId: item.id, status: "approved" }).then(() => toast.success("站点已通过")).catch((error: unknown) => toast.error(error instanceof ApiError ? error.message : "操作失败"))}>通过</Button>
                            <Button variant="outline" size="sm" onClick={() => void auditSiteMutation.mutateAsync({ appid: selectedApp.id, siteId: item.id, status: "rejected", reason: "后台审核驳回" }).then(() => toast.success("站点已驳回")).catch((error: unknown) => toast.error(error instanceof ApiError ? error.message : "操作失败"))}>驳回</Button>
                            <Button variant="outline" size="sm" onClick={() => void togglePinMutation.mutateAsync({ appid: selectedApp.id, id: item.id, is_pinned: !item.is_pinned }).then(() => toast.success(item.is_pinned ? "已取消置顶" : "已置顶")).catch((error: unknown) => toast.error(error instanceof ApiError ? error.message : "操作失败"))}>{item.is_pinned ? "取消置顶" : "置顶"}</Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </SurfaceCard>
        </TabsContent>

        <TabsContent value="roles">
          <SurfaceCard>
            <div className="flex items-center justify-between pb-4">
              <Select value={roleStatus} onValueChange={setRoleStatus}>
                <SelectTrigger className="w-[220px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="pending">待审核</SelectItem>
                  <SelectItem value="approved">已通过</SelectItem>
                  <SelectItem value="rejected">已拒绝</SelectItem>
                  <SelectItem value="cancelled">已取消</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {!roleAppsQuery.data?.items?.length ? (
              <EmptyState title="暂无角色申请" description="当前筛选条件下没有角色申请记录。" />
            ) : (
              <div className="table-shell">
                <Table>
                  <TableHeader><TableRow><TableHead>申请用户</TableHead><TableHead>目标角色</TableHead><TableHead>优先级</TableHead><TableHead>状态</TableHead><TableHead>提交时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
                  <TableBody>
                    {roleAppsQuery.data.items.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell>
                          <div className="space-y-1">
                            <div className="font-semibold text-foreground">{item.nickname || item.account || `用户 ${item.userId || ""}`}</div>
                            <div className="text-xs text-muted-foreground">{item.reason || "未填写申请原因"}</div>
                          </div>
                        </TableCell>
                        <TableCell>{item.requestedRole || "--"}</TableCell>
                        <TableCell>{item.priority || "--"}</TableCell>
                        <TableCell><Badge variant={item.status === "approved" ? "success" : item.status === "rejected" ? "warning" : "secondary"}>{item.status || "pending"}</Badge></TableCell>
                        <TableCell className="text-muted-foreground">{formatDateTime(item.createdAt)}</TableCell>
                        <TableCell className="text-right">
                          <div className="flex justify-end gap-2">
                            <Button variant="outline" size="sm" onClick={() => void reviewRoleMutation.mutateAsync({ appid: selectedApp.id, id: item.id, action: "approved" }).then(() => toast.success("申请已通过")).catch((error: unknown) => toast.error(error instanceof ApiError ? error.message : "操作失败"))}>通过</Button>
                            <Button variant="outline" size="sm" onClick={() => void reviewRoleMutation.mutateAsync({ appid: selectedApp.id, id: item.id, action: "rejected", reviewReason: "后台审核驳回" }).then(() => toast.success("申请已驳回")).catch((error: unknown) => toast.error(error instanceof ApiError ? error.message : "操作失败"))}>驳回</Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </SurfaceCard>
        </TabsContent>
      </Tabs>
    </div>
  );
}
