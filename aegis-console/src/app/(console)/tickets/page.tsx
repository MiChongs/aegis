"use client";

import { Suspense, useCallback, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  AlarmClock,
  BellRing,
  Inbox,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  Ticket,
  TrendingUp,
  UserCheck
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { SectionHeading } from "@/components/ui/section-heading";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import {
  useBulkTicketsMutation,
  useCreateTicketMutation,
  useTicketAgentStatsQuery,
  useTicketCategoriesQuery,
  useTicketMetadataQuery,
  useTicketStatsQuery,
  useTicketTrendQuery,
  useTicketsQuery
} from "@/lib/ticket-hooks";
import type { TicketListParams, TicketPriority } from "@/lib/api/tickets";
import { TicketDetailSheet } from "@/components/tickets/ticket-detail-sheet";
import { TicketSettingsPanel } from "@/components/tickets/ticket-settings-panel";
import { NotifyCenterPanel } from "@/components/tickets/notify-center-panel";
import {
  PRIORITY_LABEL,
  PriorityBadge,
  SLABadge,
  StatusBadge,
  formatDuration,
  formatDue,
  formatRelativeTime
} from "@/components/tickets/ticket-shared";
import { cn } from "@/lib/utils";

// 工单中心。
//
// URL 约定：
//   /tickets?tab=inbox|mine|analytics|settings|notify   —— 面板切换
//   /tickets?id=123                                     —— 直接打开某工单详情
// 第二条是通知里「查看工单」按钮的落点：飞书卡片点进来就是这一屏。

export default function TicketsPage() {
  return (
    <Suspense fallback={<LoadingState title="加载工单中心" description="正在初始化..." />}>
      <WidgetBoundary title="工单中心加载失败">
        <TicketsPageInner />
      </WidgetBoundary>
    </Suspense>
  );
}

function TicketsPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "inbox";
  const detailId = searchParams.get("id");

  // 详情打开状态完全由 URL 驱动（单一事实源）：
  // 这样飞书卡片里的 /tickets?id=42 与页面内点击行为走同一条路径，
  // 也不需要用 effect 把 query 同步进本地 state（那会引发级联渲染）。
  const selectedTicket = detailId ? Number(detailId) : null;

  const openDetail = useCallback(
    (id: number) => router.replace(`/tickets?tab=${tab}&id=${id}`, { scroll: false }),
    [router, tab]
  );

  const closeDetail = useCallback(() => {
    router.replace(`/tickets?tab=${tab}`, { scroll: false });
  }, [router, tab]);

  const statsQuery = useTicketStatsQuery();
  const stats = statsQuery.data;

  return (
    <div className="space-y-5">
      <SectionHeading
        eyebrow="服务台"
        title="工单中心"
        action={
          <Button size="sm" variant="outline" onClick={() => statsQuery.refetch()}>
            <RefreshCw className="mr-1 size-3.5" />
            刷新
          </Button>
        }
      />

      {stats ? (
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
          <MetricTile label="待受理" value={stats.open} icon={<Inbox className="size-4" />} />
          <MetricTile label="处理中" value={stats.processing} icon={<Ticket className="size-4" />} />
          <MetricTile label="我的待办" value={stats.mineAssigned} icon={<UserCheck className="size-4" />} highlight />
          <MetricTile label="未指派" value={stats.unassigned} icon={<Plus className="size-4" />} />
          <MetricTile
            label="SLA 超时"
            value={stats.overdueFirstResponse + stats.overdueResolve}
            icon={<AlarmClock className="size-4" />}
            danger={stats.overdueFirstResponse + stats.overdueResolve > 0}
          />
        </div>
      ) : null}

      <Tabs value={tab} onValueChange={(value) => router.replace(`/tickets?tab=${value}`, { scroll: false })}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="inbox">
            <Inbox className="size-3.5" />
            工单台
          </TabsTrigger>
          <TabsTrigger value="mine">
            <UserCheck className="size-3.5" />
            我的待办
          </TabsTrigger>
          <TabsTrigger value="analytics">
            <TrendingUp className="size-3.5" />
            统计分析
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings2 className="size-3.5" />
            工单配置
          </TabsTrigger>
          <TabsTrigger value="notify">
            <BellRing className="size-3.5" />
            通知出口
          </TabsTrigger>
        </TabsList>

        <TabsContent value="inbox" className="mt-4">
          <TicketBoard onOpen={openDetail} />
        </TabsContent>
        <TabsContent value="mine" className="mt-4">
          <TicketBoard onOpen={openDetail} mineOnly />
        </TabsContent>
        <TabsContent value="analytics" className="mt-4">
          <AnalyticsPanel />
        </TabsContent>
        <TabsContent value="settings" className="mt-4">
          <SettingsTab />
        </TabsContent>
        <TabsContent value="notify" className="mt-4">
          <NotifyCenterPanel />
        </TabsContent>
      </Tabs>

      <TicketDetailSheet ticketId={selectedTicket} onClose={closeDetail} />
    </div>
  );
}

function MetricTile({
  label,
  value,
  icon,
  highlight,
  danger
}: {
  label: string;
  value: number;
  icon: React.ReactNode;
  highlight?: boolean;
  danger?: boolean;
}) {
  return (
    <Card className={cn(highlight && "border-primary/40")}>
      <CardContent className="flex items-center gap-3 p-4">
        <span
          className={cn(
            "flex size-8 items-center justify-center rounded-lg bg-muted text-muted-foreground",
            danger && "bg-destructive/10 text-destructive"
          )}
        >
          {icon}
        </span>
        <div>
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className={cn("text-lg font-semibold", danger ? "text-destructive" : "text-foreground")}>{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}

// ─────────────── 工单台 ───────────────

function TicketBoard({ onOpen, mineOnly }: { onOpen: (id: number) => void; mineOnly?: boolean }) {
  const metadataQuery = useTicketMetadataQuery();
  const appsQuery = useAdminAppsQuery();
  const bulkMut = useBulkTicketsMutation();

  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState("active");
  const [priority, setPriority] = useState("all");
  const [appId, setAppId] = useState("all");
  const [overdueOnly, setOverdueOnly] = useState(false);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number[]>([]);
  const [createOpen, setCreateOpen] = useState(false);

  const params = useMemo<TicketListParams>(() => {
    const query: TicketListParams = {
      keyword: keyword.trim() || undefined,
      priority: priority === "all" ? undefined : priority,
      appid: appId === "all" ? undefined : Number(appId),
      overdueOnly: overdueOnly || undefined,
      mine: mineOnly || undefined,
      page,
      limit: 20
    };
    // "active" 是默认视图：后端不传 statuses 时本来就排除终态，这里显式区分「全部」
    if (status === "all") query.includeClosed = true;
    else if (status !== "active") query.status = status;
    return query;
  }, [keyword, status, priority, appId, overdueOnly, mineOnly, page]);

  const listQuery = useTicketsQuery(params);
  const items = listQuery.data?.items ?? [];
  const scope = listQuery.data?.scope;

  const statuses = metadataQuery.data?.statuses ?? [];
  const priorities = metadataQuery.data?.priorities ?? [];

  const toggleSelect = (id: number) => {
    setSelected((prev) => (prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id]));
  };

  const handleBulk = async (action: "close" | "delete", extra?: Record<string, unknown>) => {
    if (selected.length === 0) return;
    try {
      const result = await bulkMut.mutateAsync({ ids: selected, action, ...extra } as never);
      setSelected([]);
      if (result.failed?.length) {
        notify.warning(`成功 ${result.succeeded} 条，失败 ${result.failed.length} 条`, {
          description: result.failed[0]?.reason
        });
      } else {
        notify.success(`已处理 ${result.succeeded} 条工单`);
      }
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "批量操作失败");
    }
  };

  return (
    <div className="space-y-3">
      {scope && scope.level !== "all" ? (
        <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">{scope.label}</p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-52 flex-1">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
            placeholder="搜索工单号 / 标题 / 提单人 / 会话内容"
            className="h-9 pl-8"
          />
        </div>
        <Select
          value={status}
          onValueChange={(value) => {
            setStatus(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-9 w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="active">未结单</SelectItem>
            <SelectItem value="all">全部状态</SelectItem>
            {statuses.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={priority}
          onValueChange={(value) => {
            setPriority(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-9 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部优先级</SelectItem>
            {priorities.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={appId}
          onValueChange={(value) => {
            setAppId(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-9 w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部应用</SelectItem>
            {(appsQuery.data ?? []).map((app) => (
              <SelectItem key={app.id} value={String(app.id)}>
                {app.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="sm"
          variant={overdueOnly ? "default" : "outline"}
          onClick={() => {
            setOverdueOnly((prev) => !prev);
            setPage(1);
          }}
        >
          <AlarmClock className="mr-1 size-3.5" />
          仅超时
        </Button>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="mr-1 size-3.5" />
          新建工单
        </Button>
      </div>

      {selected.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-xs">
          <span className="text-muted-foreground">已选 {selected.length} 条</span>
          <Button size="sm" variant="outline" onClick={() => handleBulk("close")}>
            批量关闭
          </Button>
          <Button size="sm" variant="ghost" className="text-destructive" onClick={() => handleBulk("delete")}>
            批量删除
          </Button>
          <Button size="sm" variant="ghost" className="ml-auto" onClick={() => setSelected([])}>
            取消选择
          </Button>
        </div>
      ) : null}

      {listQuery.isLoading ? (
        <LoadingState title="加载工单" description="正在按你的可见范围拉取工单..." />
      ) : items.length === 0 ? (
        <EmptyState
          title="没有符合条件的工单"
          description={mineOnly ? "当前没有指派给你的待办工单。" : "调整筛选条件，或新建一条工单试试。"}
        />
      ) : (
        <div className="space-y-1">
          {items.map((ticket) => {
            const due = ticket.firstRespondedAt ? formatDue(ticket.resolveDueAt) : formatDue(ticket.firstResponseDueAt);
            return (
              <Card key={ticket.id} className="transition-colors hover:border-primary/40">
                <CardContent className="flex items-start gap-3 p-3">
                  <Checkbox
                    checked={selected.includes(ticket.id)}
                    onCheckedChange={() => toggleSelect(ticket.id)}
                    className="mt-1"
                  />
                  <button
                    type="button"
                    className="min-w-0 flex-1 space-y-1 text-left"
                    onClick={() => onOpen(ticket.id)}
                  >
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-mono text-[11px] text-muted-foreground">{ticket.ticketNo}</span>
                      <span className="truncate text-sm font-medium text-foreground">{ticket.title}</span>
                      <StatusBadge status={ticket.status} />
                      <PriorityBadge priority={ticket.priority} />
                      <SLABadge state={ticket.slaState} />
                    </div>
                    <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                      <span>{ticket.requesterName}</span>
                      {ticket.categoryName ? <span>· {ticket.categoryName}</span> : null}
                      {ticket.appName ? <span>· {ticket.appName}</span> : null}
                      <span>· {ticket.messageCount} 条会话</span>
                      <span>· 更新于 {formatRelativeTime(ticket.updatedAt)}</span>
                    </div>
                  </button>
                  <div className="shrink-0 space-y-1 text-right">
                    <p className="text-[11px] text-muted-foreground">
                      {ticket.assigneeName || ticket.groupName || "未指派"}
                    </p>
                    {due.text !== "—" ? (
                      <p className={cn("text-[11px]", due.overdue ? "font-medium text-destructive" : "text-muted-foreground")}>
                        {due.text}
                      </p>
                    ) : null}
                  </div>
                </CardContent>
              </Card>
            );
          })}

          {(listQuery.data?.totalPages ?? 1) > 1 ? (
            <div className="flex items-center justify-center gap-2 pt-2">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((prev) => prev - 1)}>
                上一页
              </Button>
              <span className="text-xs text-muted-foreground">
                {listQuery.data?.page} / {listQuery.data?.totalPages}（共 {listQuery.data?.total} 条）
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= (listQuery.data?.totalPages ?? 1)}
                onClick={() => setPage((prev) => prev + 1)}
              >
                下一页
              </Button>
            </div>
          ) : null}
        </div>
      )}

      <CreateTicketDialog open={createOpen} onClose={() => setCreateOpen(false)} onCreated={onOpen} />
    </div>
  );
}

// ─────────────── 新建工单 ───────────────

function CreateTicketDialog({
  open,
  onClose,
  onCreated
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (id: number) => void;
}) {
  const appsQuery = useAdminAppsQuery();
  const createMut = useCreateTicketMutation();
  const [appId, setAppId] = useState("0");
  const categoriesQuery = useTicketCategoriesQuery(Number(appId) || 0);

  const [form, setForm] = useState({
    title: "",
    content: "",
    categoryId: "none",
    priority: "normal" as TicketPriority,
    requesterName: "",
    requesterContact: ""
  });

  const handleSubmit = async () => {
    if (!form.title.trim() || !form.content.trim()) {
      notify.warning("请填写标题与描述");
      return;
    }
    try {
      const ticket = await createMut.mutateAsync({
        appid: Number(appId) || 0,
        title: form.title.trim(),
        content: form.content.trim(),
        categoryId: form.categoryId === "none" ? undefined : Number(form.categoryId),
        priority: form.priority,
        requesterName: form.requesterName.trim() || undefined,
        requesterContact: form.requesterContact.trim() || undefined,
        source: "console"
      });
      notify.success(`工单 ${ticket.ticketNo} 已创建`);
      setForm({ title: "", content: "", categoryId: "none", priority: "normal", requesterName: "", requesterContact: "" });
      onClose();
      onCreated(ticket.id);
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "创建失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>新建工单</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1">
              <Label>所属应用</Label>
              <Select value={appId} onValueChange={setAppId}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">平台内部工单</SelectItem>
                  {(appsQuery.data ?? []).map((app) => (
                    <SelectItem key={app.id} value={String(app.id)}>
                      {app.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label>分类</Label>
              <Select
                value={form.categoryId}
                onValueChange={(value) => setForm((prev) => ({ ...prev, categoryId: value }))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">不指定</SelectItem>
                  {(categoriesQuery.data ?? []).map((category) => (
                    <SelectItem key={category.id} value={String(category.id)}>
                      {category.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1">
            <Label>标题</Label>
            <Input
              value={form.title}
              onChange={(event) => setForm((prev) => ({ ...prev, title: event.target.value }))}
              placeholder="一句话描述问题"
            />
          </div>
          <div className="space-y-1">
            <Label>问题描述</Label>
            <Textarea
              rows={4}
              value={form.content}
              onChange={(event) => setForm((prev) => ({ ...prev, content: event.target.value }))}
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1">
              <Label>优先级</Label>
              <Select
                value={form.priority}
                onValueChange={(value) => setForm((prev) => ({ ...prev, priority: value as TicketPriority }))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(["urgent", "high", "normal", "low"] as TicketPriority[]).map((priority) => (
                    <SelectItem key={priority} value={priority}>
                      {PRIORITY_LABEL[priority]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label>提单人</Label>
              <Input
                value={form.requesterName}
                onChange={(event) => setForm((prev) => ({ ...prev, requesterName: event.target.value }))}
                placeholder="留空取当前管理员"
              />
            </div>
            <div className="space-y-1">
              <Label>联系方式</Label>
              <Input
                value={form.requesterContact}
                onChange={(event) => setForm((prev) => ({ ...prev, requesterContact: event.target.value }))}
                placeholder="邮箱 / 手机"
              />
            </div>
          </div>
          <Button className="w-full" disabled={createMut.isPending} onClick={handleSubmit}>
            创建工单
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ─────────────── 统计 ───────────────

function AnalyticsPanel() {
  const statsQuery = useTicketStatsQuery();
  const trendQuery = useTicketTrendQuery(30);
  const agentsQuery = useTicketAgentStatsQuery(20);

  const stats = statsQuery.data;
  const trend = trendQuery.data ?? [];
  const agents = agentsQuery.data ?? [];
  const maxTrend = Math.max(1, ...trend.map((point) => Math.max(point.created, point.resolved)));

  if (statsQuery.isLoading) {
    return <LoadingState title="加载统计" description="正在聚合工单指标..." />;
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        <MetricTile label="今日新建" value={stats?.createdToday ?? 0} icon={<Plus className="size-4" />} />
        <MetricTile label="今日解决" value={stats?.resolvedToday ?? 0} icon={<TrendingUp className="size-4" />} />
        <Card>
          <CardContent className="space-y-1 p-4">
            <p className="text-xs text-muted-foreground">平均首次响应</p>
            <p className="text-lg font-semibold text-foreground">{formatDuration(stats?.avgFirstResponseMs ?? 0)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="space-y-1 p-4">
            <p className="text-xs text-muted-foreground">平均解决时长</p>
            <p className="text-lg font-semibold text-foreground">{formatDuration(stats?.avgResolveMs ?? 0)}</p>
          </CardContent>
        </Card>
      </div>

      {/* 轻量柱状趋势：不额外引图表库，避免为一屏统计拉进 Recharts 的体积 */}
      <Card>
        <CardContent className="space-y-3 p-4">
          <div className="flex items-center justify-between">
            <p className="text-sm font-medium text-foreground">近 30 天趋势</p>
            <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
              <span className="flex items-center gap-1">
                <span className="size-2 rounded-sm bg-primary" />
                新建
              </span>
              <span className="flex items-center gap-1">
                <span className="size-2 rounded-sm bg-emerald-500" />
                解决
              </span>
            </div>
          </div>
          {trend.length === 0 ? (
            <p className="py-6 text-center text-xs text-muted-foreground">暂无数据</p>
          ) : (
            <div className="flex h-32 items-end gap-1">
              {trend.map((point) => (
                <div key={point.date} className="flex flex-1 flex-col items-center gap-0.5" title={`${point.date}：新建 ${point.created} / 解决 ${point.resolved}`}>
                  <div className="flex w-full items-end justify-center gap-0.5">
                    <span
                      className="w-1/2 rounded-t bg-primary"
                      style={{ height: `${(point.created / maxTrend) * 100}px` }}
                    />
                    <span
                      className="w-1/2 rounded-t bg-emerald-500"
                      style={{ height: `${(point.resolved / maxTrend) * 100}px` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <div className="grid gap-3 lg:grid-cols-2">
        <Card>
          <CardContent className="space-y-2 p-4">
            <p className="text-sm font-medium text-foreground">分类分布</p>
            {stats?.byCategory?.length ? (
              <div className="space-y-1">
                {stats.byCategory.map((item) => (
                  <div key={`${item.categoryId ?? "none"}`} className="flex items-center justify-between text-xs">
                    <span className="text-muted-foreground">{item.categoryName}</span>
                    <span className="font-medium text-foreground">{item.count}</span>
                  </div>
                ))}
              </div>
            ) : (
              <p className="py-4 text-center text-xs text-muted-foreground">暂无数据</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-2 p-4">
            <p className="text-sm font-medium text-foreground">处理人绩效</p>
            {agents.length === 0 ? (
              <p className="py-4 text-center text-xs text-muted-foreground">暂无数据</p>
            ) : (
              <div className="space-y-1">
                {agents.map((agent) => (
                  <div key={agent.adminId} className="flex flex-wrap items-center gap-2 text-xs">
                    <span className="min-w-20 truncate text-foreground">{agent.displayName || agent.account}</span>
                    <span className="text-muted-foreground">受理 {agent.assigned}</span>
                    <span className="text-muted-foreground">解决 {agent.resolved}</span>
                    <span className="text-muted-foreground">首响 {formatDuration(agent.avgFirstResponseMs)}</span>
                    {agent.breached > 0 ? (
                      <Badge variant="danger" size="sm">
                        超时 {agent.breached}
                      </Badge>
                    ) : null}
                    {agent.avgRating > 0 ? (
                      <Badge variant="success" size="sm">
                        {agent.avgRating.toFixed(1)} 星
                      </Badge>
                    ) : null}
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

// ─────────────── 配置 ───────────────

function SettingsTab() {
  const appsQuery = useAdminAppsQuery();
  const [appId, setAppId] = useState("0");

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Label className="text-xs text-muted-foreground">配置范围</Label>
        <Select value={appId} onValueChange={setAppId}>
          <SelectTrigger className="h-9 w-52">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">平台级（对所有应用生效）</SelectItem>
            {(appsQuery.data ?? []).map((app) => (
              <SelectItem key={app.id} value={String(app.id)}>
                {app.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {appId === "0" ? (
          <Badge variant="secondary" size="sm">
            平台级配置仅超级管理员可修改
          </Badge>
        ) : null}
      </div>
      <TicketSettingsPanel appId={Number(appId) || 0} />
    </div>
  );
}
