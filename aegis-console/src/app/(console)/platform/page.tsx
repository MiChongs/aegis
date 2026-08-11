"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import {
  AlertTriangle,
  Ban,
  Archive,
  Gavel,
  History,
  RotateCcw,
  Search,
  ShieldAlert,
  Snowflake,
  Users2,
  ZapOff
} from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { RequirePermission } from "@/lib/permissions";
import {
  useApplyGovernanceMutation,
  useBatchGovernanceMutation,
  useGovernanceActionsQuery,
  useGovernanceAppealsQuery,
  useGovernanceCatalogQuery,
  usePlatformAppGovernanceQuery,
  usePlatformAppsQuery,
  useReviewGovernanceAppealMutation,
  useRevokeAppSessionsMutation
} from "@/lib/platform-governance-hooks";
import type {
  GovernanceAction,
  GovernanceRestrictions,
  GovernanceState,
  GovernanceStateMeta,
  PlatformAppOverviewItem
} from "@/lib/api/platform-governance";
import { SectionHeading } from "@/components/ui/section-heading";
import { SurfaceCard } from "@/components/ui/surface-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Separator } from "@/components/ui/separator";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { cn } from "@/lib/utils";

// 状态的视觉编码与后端 severity 一一对应：颜色越重 = 影响面越大。
// 操作者在批量选中一屏应用时，靠颜色就能看出自己正在动多大的刀。
const STATE_STYLE: Record<GovernanceState, { label: string; className: string }> = {
  active: { label: "正常", className: "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" },
  restricted: { label: "部分受限", className: "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400" },
  frozen: { label: "冻结", className: "border-sky-500/40 bg-sky-500/10 text-sky-600 dark:text-sky-400" },
  suspended: { label: "停运", className: "border-orange-500/40 bg-orange-500/10 text-orange-600 dark:text-orange-400" },
  banned: { label: "封禁", className: "border-red-500/40 bg-red-500/10 text-red-600 dark:text-red-400" },
  archived: { label: "归档", className: "border-slate-500/40 bg-slate-500/10 text-slate-600 dark:text-slate-400" }
};

const ACTION_LABEL: Record<string, string> = {
  restrict: "部分受限",
  freeze: "冻结",
  suspend: "停运",
  ban: "封禁",
  archive: "归档",
  restore: "解除治理",
  update: "调整治理",
  expire: "到期自动恢复",
  appeal_approved: "申诉通过恢复",
  revoke_sessions: "强制下线"
};

const STATE_ICON: Record<GovernanceState, typeof Snowflake> = {
  active: RotateCcw,
  restricted: AlertTriangle,
  frozen: Snowflake,
  suspended: ZapOff,
  banned: Ban,
  archived: Archive
};

function formatDateTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message || fallback;
  if (error instanceof Error) return error.message || fallback;
  return fallback;
}

function StateBadge({ state }: { state: GovernanceState }) {
  const style = STATE_STYLE[state] ?? STATE_STYLE.active;
  const Icon = STATE_ICON[state] ?? RotateCcw;
  return (
    <Badge variant="outline" className={cn("gap-1", style.className)}>
      <Icon className="size-3" />
      {style.label}
    </Badge>
  );
}

const EMPTY_RESTRICTIONS: GovernanceRestrictions = {
  blockLogin: false,
  blockRegister: false,
  blockApi: false,
  blockPayment: false,
  blockStorage: false,
  blockNotification: false,
  blockAdminWrite: false
};

export default function PlatformGovernancePage() {
  // useSearchParams 必须包在 Suspense 内，否则 next build 报错、整页退化为客户端渲染
  return (
    <Suspense>
      <RequirePermission permission="platform:app:read">
        <PlatformGovernanceContent />
      </RequirePermission>
    </Suspense>
  );
}

function PlatformGovernanceContent() {
  // Tab 同步到 URL：侧边栏三级子项的链接是 /platform?tab=xxx，
  // 只存在于组件 state 的 Tab 点了不会切面板
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "apps";
  const [keyword, setKeyword] = useState("");
  const [stateFilter, setStateFilter] = useState("all");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number[]>([]);
  const [detailAppKey, setDetailAppKey] = useState<string | null>(null);
  const [actionTarget, setActionTarget] = useState<{ apps: PlatformAppOverviewItem[]; batch: boolean } | null>(null);

  const catalogQuery = useGovernanceCatalogQuery();
  const appsQuery = usePlatformAppsQuery({
    keyword: keyword.trim() || undefined,
    state: stateFilter === "all" ? undefined : stateFilter,
    page,
    limit: 20
  });

  const summary = appsQuery.data?.summary;
  // `?? []` 每次渲染都会造出新数组，直接进 useMemo 依赖会让它每次都重算
  const items = useMemo(() => appsQuery.data?.items ?? [], [appsQuery.data]);

  const toggleSelect = (appid: number) => {
    setSelected((prev) => (prev.includes(appid) ? prev.filter((id) => id !== appid) : [...prev, appid]));
  };

  const selectedApps = useMemo(
    () => items.filter((item) => selected.includes(item.appid)),
    [items, selected]
  );

  return (
    <div className="space-y-6">
      <SectionHeading
        eyebrow="平台治理"
        title="全站应用治理台"
        action={
          <div className="flex items-center gap-2">
            {selected.length > 0 ? (
              <Button
                size="sm"
                variant="outline"
                onClick={() => setActionTarget({ apps: selectedApps, batch: true })}
              >
                <Gavel className="mr-1 size-4" />
                批量治理（{selected.length}）
              </Button>
            ) : null}
          </div>
        }
      />

      <SummaryRow summary={summary} />

      <Tabs
        value={tab}
        onValueChange={(value) => router.replace(`/platform?tab=${value}`, { scroll: false })}
        className="space-y-4"
      >
        <TabsList>
          <TabsTrigger value="apps">应用</TabsTrigger>
          <TabsTrigger value="appeals">
            申诉
            {summary?.pendingAppeals ? (
              <Badge variant="outline" className="ml-2 border-amber-500/40 bg-amber-500/10 text-amber-600">
                {summary.pendingAppeals}
              </Badge>
            ) : null}
          </TabsTrigger>
          <TabsTrigger value="actions">治理流水</TabsTrigger>
        </TabsList>

        <TabsContent value="apps" className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value);
                  setPage(1);
                }}
                placeholder="搜索应用名称或 AppKey"
                className="w-64 pl-8"
              />
            </div>
            <Select
              value={stateFilter}
              onValueChange={(value) => {
                setStateFilter(value);
                setPage(1);
              }}
            >
              <SelectTrigger className="w-40">
                <SelectValue placeholder="全部状态" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                {(Object.keys(STATE_STYLE) as GovernanceState[]).map((state) => (
                  <SelectItem key={state} value={state}>
                    {STATE_STYLE[state].label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {appsQuery.isLoading ? (
            <LoadingState title="正在加载全站应用" description="正在汇总治理状态与用量指标。" />
          ) : items.length === 0 ? (
            <EmptyState title="没有匹配的应用" description="换个关键词或状态筛选试试。" />
          ) : (
            <SurfaceCard className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-10" />
                    <TableHead>应用</TableHead>
                    <TableHead>治理状态</TableHead>
                    <TableHead className="text-right">用户</TableHead>
                    <TableHead className="text-right">今日新增</TableHead>
                    <TableHead className="text-right">今日登录</TableHead>
                    <TableHead>到期</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => (
                    <TableRow key={item.appid}>
                      <TableCell>
                        <Checkbox
                          checked={selected.includes(item.appid)}
                          onCheckedChange={() => toggleSelect(item.appid)}
                          aria-label={`选择 ${item.name}`}
                        />
                      </TableCell>
                      <TableCell>
                        <div className="font-medium text-foreground">{item.name}</div>
                        <div className="text-xs text-muted-foreground">
                          #{item.appid}
                          {item.appKey ? ` · ${item.appKey.slice(0, 8)}…` : ""}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col items-start gap-1">
                          <StateBadge state={item.state} />
                          {item.reason ? (
                            <span className="line-clamp-1 max-w-52 text-xs text-muted-foreground">{item.reason}</span>
                          ) : null}
                          {item.appealStatus === "pending" ? (
                            <span className="text-xs text-amber-600">有待审申诉</span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-right tabular-nums">{item.totalUsers}</TableCell>
                      <TableCell className="text-right tabular-nums">{item.newUsersToday}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {item.loginSuccessToday}
                        {item.loginFailureToday > 0 ? (
                          <span className="ml-1 text-xs text-muted-foreground">/{item.loginFailureToday} 失败</span>
                        ) : null}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {item.state === "active" ? "—" : item.endAt ? formatDateTime(item.endAt) : "无限期"}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-1">
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => setDetailAppKey(item.appKey || String(item.appid))}
                          >
                            <History className="mr-1 size-4" />
                            详情
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => setActionTarget({ apps: [item], batch: false })}
                          >
                            <Gavel className="mr-1 size-4" />
                            治理
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </SurfaceCard>
          )}

          {appsQuery.data && appsQuery.data.totalPages > 1 ? (
            <div className="flex items-center justify-end gap-2">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                上一页
              </Button>
              <span className="text-xs text-muted-foreground">
                {appsQuery.data.page} / {appsQuery.data.totalPages}
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= appsQuery.data.totalPages}
                onClick={() => setPage((p) => p + 1)}
              >
                下一页
              </Button>
            </div>
          ) : null}
        </TabsContent>

        <TabsContent value="appeals">
          <AppealsPanel />
        </TabsContent>

        <TabsContent value="actions">
          <ActionsPanel />
        </TabsContent>
      </Tabs>

      <GovernanceActionSheet
        target={actionTarget}
        states={catalogQuery.data?.states ?? []}
        capabilities={catalogQuery.data?.capabilities ?? []}
        durations={catalogQuery.data?.durations ?? []}
        onClose={(done) => {
          setActionTarget(null);
          if (done) setSelected([]);
        }}
      />

      <GovernanceDetailSheet appKey={detailAppKey} onClose={() => setDetailAppKey(null)} />
    </div>
  );
}

function SummaryRow({ summary }: { summary?: { totalApps: number; activeApps: number; governedApps: number; totalUsers: number; newUsersToday: number; loginsToday: number; pendingAppeals: number; expiringSoon: number } }) {
  const tiles = [
    { label: "应用总数", value: summary?.totalApps ?? 0, hint: `${summary?.activeApps ?? 0} 个正常` },
    { label: "被治理应用", value: summary?.governedApps ?? 0, hint: `${summary?.expiringSoon ?? 0} 个 24h 内到期` },
    { label: "全站用户", value: summary?.totalUsers ?? 0, hint: `今日 +${summary?.newUsersToday ?? 0}` },
    { label: "今日登录", value: summary?.loginsToday ?? 0, hint: `${summary?.pendingAppeals ?? 0} 条待审申诉` }
  ];
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {tiles.map((tile) => (
        <SurfaceCard key={tile.label} className="p-4">
          <div className="text-xs text-muted-foreground">{tile.label}</div>
          <div className="mt-1 text-2xl font-semibold tabular-nums text-foreground">{tile.value}</div>
          <div className="mt-1 text-xs text-muted-foreground">{tile.hint}</div>
        </SurfaceCard>
      ))}
    </div>
  );
}

type ActionTarget = { apps: PlatformAppOverviewItem[]; batch: boolean };

function GovernanceActionSheet({
  target,
  states,
  capabilities,
  durations,
  onClose
}: {
  target: ActionTarget | null;
  states: GovernanceStateMeta[];
  capabilities: { key: string; field: keyof GovernanceRestrictions; name: string; description: string; enforcement: string }[];
  durations: { label: string; seconds: number }[];
  onClose: (done: boolean) => void;
}) {
  const [action, setAction] = useState<GovernanceAction>("freeze");
  const [reason, setReason] = useState("");
  const [durationSeconds, setDurationSeconds] = useState<number>(7 * 86400);
  const [restrictions, setRestrictions] = useState<GovernanceRestrictions>(EMPTY_RESTRICTIONS);
  const [revokeSessions, setRevokeSessions] = useState(true);
  const [notifyAdmins, setNotifyAdmins] = useState(true);

  const applyMutation = useApplyGovernanceMutation();
  const batchMutation = useBatchGovernanceMutation();

  const meta = states.find((item) => item.action === action);
  const isRestore = action === "restore";
  const pending = applyMutation.isPending || batchMutation.isPending;

  const submit = async () => {
    if (!target) return;
    const payload = {
      action,
      reason: reason.trim(),
      restrictions: action === "restrict" ? restrictions : undefined,
      durationSeconds: isRestore || meta?.permanent ? undefined : durationSeconds || undefined,
      revokeSessions,
      notifyAdmins
    };
    try {
      if (target.batch) {
        const result = await batchMutation.mutateAsync({
          ...payload,
          appids: target.apps.map((item) => item.appid)
        });
        // 批量结果逐条回报：失败的那几个是哪些、为什么，比"部分失败"有用得多
        if (result.failed > 0) {
          toast.warning(`成功 ${result.succeeded} 个，失败 ${result.failed} 个`, {
            description: result.items
              .filter((item) => !item.ok)
              .map((item) => `${item.appName || item.appid}：${item.error}`)
              .join("；")
          });
        } else {
          toast.success(`已对 ${result.succeeded} 个应用执行${ACTION_LABEL[action] ?? action}`);
        }
      } else {
        const app = target.apps[0];
        await applyMutation.mutateAsync({
          appKey: app.appKey || String(app.appid),
          payload
        });
        toast.success(`已对「${app.name}」执行${ACTION_LABEL[action] ?? action}`);
      }
      setReason("");
      onClose(true);
    } catch (error) {
      toast.error(errorMessage(error, "治理动作执行失败"));
    }
  };

  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => (open ? null : onClose(false))}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>
            {target?.batch ? `批量治理 ${target.apps.length} 个应用` : `治理「${target?.apps[0]?.name ?? ""}」`}
          </SheetTitle>
          <SheetDescription>
            治理结论由平台强制执行，应用管理员无法自行撤销；每一次动作都会写入流水并可被申诉。
          </SheetDescription>
        </SheetHeader>

        <div className="space-y-5 px-4 pb-6">
          <div className="space-y-2">
            <Label>治理档位</Label>
            <div className="grid gap-2">
              {states.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => {
                    setAction(item.action);
                    if (item.action === "restrict") {
                      setRestrictions(EMPTY_RESTRICTIONS);
                    }
                  }}
                  className={cn(
                    "rounded-lg border p-3 text-left transition",
                    action === item.action ? "border-primary bg-primary/5" : "border-border hover:bg-muted/40"
                  )}
                >
                  <div className="flex items-center gap-2">
                    <StateBadge state={item.key} />
                    {item.permanent ? (
                      <span className="text-xs text-muted-foreground">永久</span>
                    ) : null}
                    {item.requiresDanger ? (
                      <span className="flex items-center gap-1 text-xs text-red-500">
                        <ShieldAlert className="size-3" />
                        危险操作权限
                      </span>
                    ) : null}
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">{item.description}</div>
                </button>
              ))}
            </div>
          </div>

          {action === "restrict" ? (
            <div className="space-y-2">
              <Label>要停用的能力</Label>
              <div className="grid gap-2">
                {capabilities.map((capability) => (
                  <label
                    key={capability.key}
                    className="flex items-start gap-3 rounded-lg border border-border p-3 text-sm"
                  >
                    <Checkbox
                      checked={restrictions[capability.field]}
                      onCheckedChange={(checked) =>
                        setRestrictions((prev) => ({ ...prev, [capability.field]: checked === true }))
                      }
                    />
                    <span>
                      <span className="font-medium text-foreground">{capability.name}</span>
                      <span className="block text-xs text-muted-foreground">{capability.description}</span>
                      <span className="block text-[11px] text-muted-foreground/70">
                        执行点：{capability.enforcement}
                      </span>
                    </span>
                  </label>
                ))}
              </div>
            </div>
          ) : meta && !isRestore ? (
            <div className="rounded-lg border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
              该档位会停用：
              {capabilities
                .filter((capability) => meta.restrictions[capability.field])
                .map((capability) => capability.name)
                .join(" · ") || "（无）"}
            </div>
          ) : null}

          {!isRestore && !meta?.permanent ? (
            <div className="space-y-2">
              <Label>期限</Label>
              <Select
                value={String(durationSeconds)}
                onValueChange={(value) => setDurationSeconds(Number(value))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {durations.map((item) => (
                    <SelectItem key={item.seconds} value={String(item.seconds)}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                到期后自动恢复为正常；选「不设期限」则需要人工解除。
              </p>
            </div>
          ) : null}

          <div className="space-y-2">
            <Label>
              理由
              {!isRestore ? <span className="ml-1 text-red-500">*</span> : null}
            </Label>
            <Textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              rows={3}
              placeholder="写清楚触发治理的事实与依据，这段话会展示给被治理方"
            />
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={revokeSessions} onCheckedChange={(v) => setRevokeSessions(v === true)} />
              立即踢掉该应用全部在线用户
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={notifyAdmins} onCheckedChange={(v) => setNotifyAdmins(v === true)} />
              通知该应用的管理员
            </label>
          </div>

          <Separator />

          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => onClose(false)} disabled={pending}>
              取消
            </Button>
            <Button onClick={submit} disabled={pending}>
              {pending ? "执行中…" : `确认${ACTION_LABEL[action] ?? action}`}
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function GovernanceDetailSheet({ appKey, onClose }: { appKey: string | null; onClose: () => void }) {
  const detailQuery = usePlatformAppGovernanceQuery(appKey);
  const revokeMutation = useRevokeAppSessionsMutation();
  const detail = detailQuery.data;

  const revoke = async () => {
    if (!appKey) return;
    try {
      const record = await revokeMutation.mutateAsync({ appKey, reason: "平台强制下线" });
      toast.success(`已下线 ${record.revokedSessions} 个会话`);
    } catch (error) {
      toast.error(errorMessage(error, "强制下线失败"));
    }
  };

  return (
    <Sheet open={Boolean(appKey)} onOpenChange={(open) => (open ? null : onClose())}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{detail?.governance.appName || "应用治理详情"}</SheetTitle>
          <SheetDescription>当前治理结论、最近动作与待审申诉。</SheetDescription>
        </SheetHeader>

        <div className="space-y-5 px-4 pb-6">
          {detailQuery.isLoading ? (
            <LoadingState title="加载中" description="正在读取治理详情。" />
          ) : !detail ? (
            <EmptyState title="没有数据" description="未能读取该应用的治理详情。" />
          ) : (
            <>
              <div className="space-y-2 rounded-lg border border-border p-4">
                <div className="flex items-center justify-between">
                  <StateBadge state={detail.governance.state} />
                  <span className="text-xs text-muted-foreground">
                    {detail.governance.endAt ? `到期 ${formatDateTime(detail.governance.endAt)}` : "无到期时间"}
                  </span>
                </div>
                {detail.governance.reason ? (
                  <p className="text-sm text-foreground">{detail.governance.reason}</p>
                ) : null}
                <div className="text-xs text-muted-foreground">
                  操作人：{detail.governance.operatorName || "—"} · 更新于 {formatDateTime(detail.governance.updatedAt)}
                </div>
              </div>

              {detail.pendingAppeal ? (
                <div className="space-y-1 rounded-lg border border-amber-500/40 bg-amber-500/5 p-4">
                  <div className="text-sm font-medium text-amber-600">待审申诉</div>
                  <p className="text-sm text-foreground">{detail.pendingAppeal.content}</p>
                  <div className="text-xs text-muted-foreground">
                    {detail.pendingAppeal.submittedByName} 提交于 {formatDateTime(detail.pendingAppeal.createdAt)}
                  </div>
                </div>
              ) : null}

              {detail.canDanger ? (
                <Button variant="outline" size="sm" onClick={revoke} disabled={revokeMutation.isPending}>
                  <Users2 className="mr-1 size-4" />
                  {revokeMutation.isPending ? "下线中…" : "强制下线全部在线用户"}
                </Button>
              ) : null}

              <div className="space-y-2">
                <div className="text-sm font-medium text-foreground">最近动作</div>
                {detail.recentActions.length === 0 ? (
                  <p className="text-xs text-muted-foreground">暂无治理记录。</p>
                ) : (
                  <ol className="space-y-2">
                    {detail.recentActions.map((record) => (
                      <li key={record.id} className="rounded-lg border border-border p-3 text-sm">
                        <div className="flex items-center justify-between">
                          <span className="font-medium text-foreground">
                            {ACTION_LABEL[record.action] ?? record.action}
                          </span>
                          <span className="text-xs text-muted-foreground">{formatDateTime(record.createdAt)}</span>
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {record.fromState} → {record.toState} · {record.operatorName || "系统"}
                          {record.revokedSessions > 0 ? ` · 下线 ${record.revokedSessions} 个会话` : ""}
                        </div>
                        {record.reason ? <p className="mt-1 text-xs text-foreground">{record.reason}</p> : null}
                      </li>
                    ))}
                  </ol>
                )}
              </div>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function AppealsPanel() {
  const [status, setStatus] = useState("pending");
  const appealsQuery = useGovernanceAppealsQuery({ status: status === "all" ? undefined : status, page: 1, limit: 30 });
  const reviewMutation = useReviewGovernanceAppealMutation();
  const [notes, setNotes] = useState<Record<number, string>>({});

  const review = async (appealId: number, decision: "approved" | "rejected") => {
    try {
      await reviewMutation.mutateAsync({ appealId, decision, note: notes[appealId] || "", restore: decision === "approved" });
      toast.success(decision === "approved" ? "申诉已通过，治理已解除" : "申诉已驳回");
    } catch (error) {
      toast.error(errorMessage(error, "处理申诉失败"));
    }
  };

  const items = appealsQuery.data?.items ?? [];

  return (
    <div className="space-y-4">
      <Select value={status} onValueChange={setStatus}>
        <SelectTrigger className="w-40">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="pending">待审</SelectItem>
          <SelectItem value="approved">已通过</SelectItem>
          <SelectItem value="rejected">已驳回</SelectItem>
          <SelectItem value="all">全部</SelectItem>
        </SelectContent>
      </Select>

      {appealsQuery.isLoading ? (
        <LoadingState title="正在加载申诉" description="正在读取治理申诉队列。" />
      ) : items.length === 0 ? (
        <EmptyState title="没有申诉" description="当前筛选条件下没有申诉记录。" />
      ) : (
        <div className="space-y-3">
          {items.map((appeal) => (
            <SurfaceCard key={appeal.id} className="space-y-3 p-4">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="font-medium text-foreground">{appeal.appName || `应用 #${appeal.appid}`}</div>
                  <div className="text-xs text-muted-foreground">
                    {appeal.submittedByName || "未知提交人"} · {formatDateTime(appeal.createdAt)}
                    {appeal.stateSnapshot ? ` · 申诉时状态：${STATE_STYLE[appeal.stateSnapshot as GovernanceState]?.label ?? appeal.stateSnapshot}` : ""}
                  </div>
                </div>
                <Badge variant="outline">{appeal.status}</Badge>
              </div>
              <p className="whitespace-pre-wrap text-sm text-foreground">{appeal.content}</p>
              {appeal.status === "pending" ? (
                <div className="space-y-2">
                  <Textarea
                    rows={2}
                    placeholder="裁决说明（会通知提交人）"
                    value={notes[appeal.id] || ""}
                    onChange={(event) => setNotes((prev) => ({ ...prev, [appeal.id]: event.target.value }))}
                  />
                  <div className="flex justify-end gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={reviewMutation.isPending}
                      onClick={() => review(appeal.id, "rejected")}
                    >
                      驳回
                    </Button>
                    <Button size="sm" disabled={reviewMutation.isPending} onClick={() => review(appeal.id, "approved")}>
                      通过并解除治理
                    </Button>
                  </div>
                </div>
              ) : appeal.reviewNote ? (
                <div className="rounded-md bg-muted/40 p-2 text-xs text-muted-foreground">
                  {appeal.reviewAdminName}：{appeal.reviewNote}
                </div>
              ) : null}
            </SurfaceCard>
          ))}
        </div>
      )}
    </div>
  );
}

function ActionsPanel() {
  const [page, setPage] = useState(1);
  const actionsQuery = useGovernanceActionsQuery({ page, limit: 30 });
  const items = actionsQuery.data?.items ?? [];

  if (actionsQuery.isLoading) {
    return <LoadingState title="正在加载流水" description="正在读取全站治理动作记录。" />;
  }
  if (items.length === 0) {
    return <EmptyState title="没有治理记录" description="平台还没有对任何应用执行过治理动作。" />;
  }

  return (
    <div className="space-y-4">
      <SurfaceCard className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>时间</TableHead>
              <TableHead>应用</TableHead>
              <TableHead>动作</TableHead>
              <TableHead>状态变化</TableHead>
              <TableHead>操作人</TableHead>
              <TableHead>理由</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((record) => (
              <TableRow key={record.id}>
                <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                  {formatDateTime(record.createdAt)}
                </TableCell>
                <TableCell>{record.appName || `#${record.appid}`}</TableCell>
                <TableCell>{ACTION_LABEL[record.action] ?? record.action}</TableCell>
                <TableCell className="text-xs">
                  {STATE_STYLE[record.fromState]?.label ?? record.fromState} →{" "}
                  {STATE_STYLE[record.toState]?.label ?? record.toState}
                </TableCell>
                <TableCell className="text-xs">{record.operatorName || "系统"}</TableCell>
                <TableCell className="max-w-64 truncate text-xs text-muted-foreground">{record.reason || "—"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </SurfaceCard>
      {actionsQuery.data && actionsQuery.data.totalPages > 1 ? (
        <div className="flex items-center justify-end gap-2">
          <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
            上一页
          </Button>
          <span className="text-xs text-muted-foreground">
            {actionsQuery.data.page} / {actionsQuery.data.totalPages}
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={page >= actionsQuery.data.totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </div>
      ) : null}
    </div>
  );
}
