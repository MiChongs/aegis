"use client";

import { useMemo, useState } from "react";
import {
  CalendarClock,
  ChevronLeft,
  ChevronRight,
  ListChecks,
  Loader2,
  MoreHorizontal,
  Pencil,
  Play,
  Plus,
  RotateCcw,
  ScrollText,
  Trash2,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import type { AppFunction, AppFunctionTrigger, FunctionCatalog, FunctionTriggerEvent } from "@/lib/api/app-functions";
import {
  useDeleteTriggerMutation,
  useFunctionTriggerJobsQuery,
  useFunctionTriggersQuery,
  useRetryTriggerJobMutation,
  useSaveTriggerMutation
} from "@/lib/function-hooks";
import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty, OverviewCard } from "@/components/overview/overview-kit";
import { CopyButton } from "@/components/users/detail/kit/surface";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { describeCron, formatInZone } from "./function-cron";
import { FireTriggerDialog, TriggerEditorDialog } from "./function-trigger-dialogs";
import { JOB_STATUS, errorMessage, formatRelative, formatTime } from "./function-shared";

/** 与后端 MaxTriggersPerFunction / MaxTriggerAttempts 一致，只用于展示 */
const MAX_TRIGGERS = 16;
const MAX_ATTEMPTS = 5;

function triggerTitle(trigger: AppFunctionTrigger, events: FunctionTriggerEvent[]) {
  if (trigger.kind === "event") {
    return events.find((item) => item.key === trigger.event)?.label ?? trigger.event ?? "事件";
  }
  return describeCron(trigger.cron ?? "") ?? trigger.cron ?? "定时";
}

/**
 * 触发器：让函数由平台事件或定时计划拉起，而不只是等接入方来调。
 *
 * 上半部分是触发器卡片（是什么、下次何时跑、最近一次结果），下半部分是
 * 触发任务队列 —— 钩子与定时扫描只负责入队，执行与重试都在队列里，
 * 「事件发生了但函数没跑」要到这里看是排着、在重试还是已失败。
 */
export function FunctionTriggersPanel({
  appKey,
  selected,
  catalog,
  onViewInvocations
}: {
  appKey: string;
  selected: AppFunction;
  catalog?: FunctionCatalog;
  onViewInvocations: (triggerId: number) => void;
}) {
  const triggersQuery = useFunctionTriggersQuery(appKey, selected.name);
  const triggers = useMemo(() => triggersQuery.data ?? [], [triggersQuery.data]);
  const events = useMemo(() => catalog?.triggerEvents ?? [], [catalog]);

  const [editing, setEditing] = useState<{ trigger: AppFunctionTrigger | null } | null>(null);
  const [firing, setFiring] = useState<AppFunctionTrigger | null>(null);
  const [deleting, setDeleting] = useState<AppFunctionTrigger | null>(null);
  const [jobTrigger, setJobTrigger] = useState("all");

  const save = useSaveTriggerMutation(appKey);
  const remove = useDeleteTriggerMutation(appKey);

  async function toggleEnabled(trigger: AppFunctionTrigger, enabled: boolean) {
    try {
      // 带上整条触发器：后端修改时不沿用说明，只传 enabled 会把说明清空
      await save.mutateAsync({
        name: selected.name,
        triggerId: trigger.id,
        payload: {
          kind: trigger.kind,
          event: trigger.event,
          cron: trigger.cron,
          timezone: trigger.timezone,
          description: trigger.description,
          input: trigger.input ?? {},
          enabled
        }
      });
      toast.success(enabled ? "已启用" : "已停用");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  const callable = selected.status === "active" && Boolean(selected.activeVersion);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-medium">触发器</h3>
        <span className="font-data text-xs text-muted-foreground tabular-nums">
          {triggers.length} / {MAX_TRIGGERS}
        </span>
        {!callable && triggers.some((item) => item.enabled) ? (
          <Badge variant="warning" size="sm">
            函数不可被调用，触发会失败
          </Badge>
        ) : null}
        <Button
          size="sm"
          className="ml-auto h-8 text-xs"
          disabled={triggers.length >= MAX_TRIGGERS}
          onClick={() => setEditing({ trigger: null })}
        >
          <Plus className="size-3.5" />
          新建触发器
        </Button>
      </div>

      {triggersQuery.isLoading ? (
        <div className="grid gap-3 xl:grid-cols-2">
          <Skeleton className="h-40 rounded-xl" />
          <Skeleton className="h-40 rounded-xl" />
        </div>
      ) : !triggers.length ? (
        <Card className="py-8">
          <CardEmpty icon={Zap} title="暂无触发器" />
          <div className="flex justify-center">
            <Button size="sm" variant="outline" onClick={() => setEditing({ trigger: null })}>
              <Plus className="size-4" />
              新建触发器
            </Button>
          </div>
        </Card>
      ) : (
        <div className="grid gap-3 xl:grid-cols-2">
          {triggers.map((trigger) => (
            <TriggerCard
              key={trigger.id}
              trigger={trigger}
              title={triggerTitle(trigger, events)}
              busy={save.isPending && save.variables?.triggerId === trigger.id}
              onToggle={(value) => toggleEnabled(trigger, value)}
              onEdit={() => setEditing({ trigger })}
              onFire={() => setFiring(trigger)}
              onDelete={() => setDeleting(trigger)}
              onJobs={() => setJobTrigger(String(trigger.id))}
              onInvocations={() => onViewInvocations(trigger.id)}
            />
          ))}
        </div>
      )}

      <JobsCard
        appKey={appKey}
        functionName={selected.name}
        triggers={triggers}
        events={events}
        triggerFilter={jobTrigger}
        onTriggerFilter={setJobTrigger}
      />

      {editing ? (
        <TriggerEditorDialog
          key={editing.trigger?.id ?? "new"}
          appKey={appKey}
          selected={selected}
          catalog={catalog}
          trigger={editing.trigger}
          onClose={() => setEditing(null)}
        />
      ) : null}

      {firing ? (
        <FireTriggerDialog
          key={firing.id}
          appKey={appKey}
          functionName={selected.name}
          trigger={firing}
          spec={events.find((item) => item.key === firing.event)}
          title={triggerTitle(firing, events)}
          onClose={() => setFiring(null)}
        />
      ) : null}

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除触发器</AlertDialogTitle>
            <AlertDialogDescription>排队中的触发任务会一并删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={async () => {
                if (!deleting) return;
                try {
                  await remove.mutateAsync({ name: selected.name, triggerId: deleting.id });
                  if (jobTrigger === String(deleting.id)) setJobTrigger("all");
                  toast.success("触发器已删除");
                } catch (error) {
                  toast.error(errorMessage(error));
                }
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function TriggerCard({
  trigger,
  title,
  busy,
  onToggle,
  onEdit,
  onFire,
  onDelete,
  onJobs,
  onInvocations
}: {
  trigger: AppFunctionTrigger;
  title: string;
  busy: boolean;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onFire: () => void;
  onDelete: () => void;
  onJobs: () => void;
  onInvocations: () => void;
}) {
  const isEvent = trigger.kind === "event";
  const lastFailed = trigger.lastStatus && trigger.lastStatus !== "success" && trigger.lastStatus !== "succeeded";
  const failRate = trigger.fireCount ? trigger.failCount / trigger.fireCount : 0;

  return (
    <Card className={cn("gap-0 overflow-hidden py-0 transition-opacity", !trigger.enabled && "opacity-70")}>
      <div className="flex items-start gap-3 p-4">
        <TonalIcon icon={isEvent ? Zap : CalendarClock} tone={trigger.enabled ? (isEvent ? "violet" : "amber") : "muted"} />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <p className="truncate text-sm font-medium">{title}</p>
            <Badge variant="outline" size="sm" className="shrink-0 font-normal">
              {isEvent ? "事件" : "定时"}
            </Badge>
          </div>
          <p className="flex flex-wrap items-center gap-x-2 text-[11px] text-muted-foreground">
            <code className="font-mono">{isEvent ? trigger.event : trigger.cron}</code>
            {!isEvent && trigger.timezone ? <span>{trigger.timezone}</span> : null}
          </p>
          {trigger.description ? <p className="truncate text-xs text-muted-foreground">{trigger.description}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {busy ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
          <Switch checked={trigger.enabled} disabled={busy} onCheckedChange={onToggle} aria-label="启用" />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" aria-label="更多">
                <MoreHorizontal className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil className="size-4" />
                编辑
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onJobs}>
                <ListChecks className="size-4" />
                查看任务
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onInvocations}>
                <ScrollText className="size-4" />
                查看调用
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                <Trash2 className="size-4" />
                删除
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <div className="grid grid-cols-3 border-t text-xs">
        <Cell label={isEvent ? "最近触发" : "下次执行"}>
          {isEvent ? (
            <span title={formatTime(trigger.lastFireAt)}>{formatRelative(trigger.lastFireAt)}</span>
          ) : trigger.enabled && trigger.nextRunAt ? (
            <span title={formatTime(trigger.nextRunAt)}>{formatInZone(trigger.nextRunAt, trigger.timezone)}</span>
          ) : (
            <span className="text-muted-foreground">{trigger.enabled ? "—" : "已停用"}</span>
          )}
        </Cell>
        <Cell label="累计触发">
          <span className="font-data tabular-nums">{trigger.fireCount.toLocaleString("zh-CN")}</span>
        </Cell>
        <Cell label="失败">
          <span className={cn("font-data tabular-nums", trigger.failCount && "text-destructive")}>
            {trigger.failCount.toLocaleString("zh-CN")}
            {trigger.fireCount ? (
              <span className="ml-1 text-muted-foreground">{(failRate * 100).toFixed(0)}%</span>
            ) : null}
          </span>
        </Cell>
      </div>

      <div className="flex items-center gap-2 border-t bg-muted/20 px-4 py-2">
        {trigger.lastStatus ? (
          <Badge variant={lastFailed ? "danger" : "success"} size="sm" className="shrink-0">
            {lastFailed ? "上次失败" : "上次成功"}
          </Badge>
        ) : (
          <span className="text-[11px] text-muted-foreground">尚未触发</span>
        )}
        {!isEvent && trigger.lastFireAt ? (
          <span className="shrink-0 text-[11px] text-muted-foreground" title={formatTime(trigger.lastFireAt)}>
            {formatRelative(trigger.lastFireAt)}
          </span>
        ) : null}
        {lastFailed && trigger.lastError ? (
          <span className="min-w-0 truncate text-[11px] text-destructive" title={trigger.lastError}>
            {trigger.lastError}
          </span>
        ) : null}
        <Button size="xs" variant="outline" className="ml-auto shrink-0" onClick={onFire}>
          <Play className="size-3" />
          立即触发
        </Button>
      </div>
    </Card>
  );
}

function Cell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-0.5 border-r px-4 py-2 last:border-r-0">
      <p className="text-[11px] text-muted-foreground">{label}</p>
      <div className="truncate">{children}</div>
    </div>
  );
}

function JobsCard({
  appKey,
  functionName,
  triggers,
  events,
  triggerFilter,
  onTriggerFilter
}: {
  appKey: string;
  functionName: string;
  triggers: AppFunctionTrigger[];
  events: FunctionTriggerEvent[];
  triggerFilter: string;
  onTriggerFilter: (value: string) => void;
}) {
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const query = useFunctionTriggerJobsQuery(appKey, functionName, {
    status: status === "all" ? undefined : status,
    triggerId: triggerFilter === "all" ? undefined : Number(triggerFilter),
    page,
    limit: 20
  });
  const retry = useRetryTriggerJobMutation(appKey);
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / (query.data?.limit || 20)));
  const titles = useMemo(
    () => new Map(triggers.map((item) => [item.id, triggerTitle(item, events)])),
    [triggers, events]
  );

  return (
    <OverviewCard
      icon={ListChecks}
      tone="sky"
      title="触发任务"
      description={`共 ${total} 条`}
      contentClassName="px-0 sm:px-0"
      action={
        <>
          <Select
            value={triggerFilter}
            onValueChange={(value) => {
              onTriggerFilter(value);
              setPage(1);
            }}
          >
            <SelectTrigger size="sm" className="h-7 w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部触发器</SelectItem>
              {triggers.map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>
                  {titles.get(item.id)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={status}
            onValueChange={(value) => {
              setStatus(value);
              setPage(1);
            }}
          >
            <SelectTrigger size="sm" className="h-7 w-24 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              {Object.entries(JOB_STATUS).map(([key, meta]) => (
                <SelectItem key={key} value={key}>
                  {meta.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>
      }
      footer={
        total > 20 ? (
          <div className="flex w-full items-center justify-end gap-2 text-xs text-muted-foreground">
            <Button size="icon-sm" variant="ghost" disabled={page <= 1} onClick={() => setPage((v) => Math.max(1, v - 1))}>
              <ChevronLeft className="size-4" />
            </Button>
            <span className="font-data tabular-nums">
              {page} / {totalPages}
            </span>
            <Button size="icon-sm" variant="ghost" disabled={page >= totalPages} onClick={() => setPage((v) => v + 1)}>
              <ChevronRight className="size-4" />
            </Button>
          </div>
        ) : undefined
      }
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-4 sm:pl-5">事件 ID</TableHead>
            <TableHead>触发器</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className="text-right">尝试</TableHead>
            <TableHead>用户</TableHead>
            <TableHead>触发时间</TableHead>
            <TableHead>计划执行</TableHead>
            <TableHead>错误</TableHead>
            <TableHead className="pr-4 sm:pr-5" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((job) => {
            const meta = JOB_STATUS[job.status];
            return (
              <TableRow key={job.id}>
                <TableCell className="max-w-44 pl-4 sm:pl-5">
                  <span className="flex items-center gap-1">
                    <code className="truncate font-mono text-[11px]">{job.eventId}</code>
                    <CopyButton value={job.eventId} label="事件 ID" />
                  </span>
                </TableCell>
                <TableCell className="max-w-40 truncate text-xs">{titles.get(job.triggerId) ?? `#${job.triggerId}`}</TableCell>
                <TableCell>
                  <Badge variant={meta?.variant ?? "outline"} size="sm">
                    {meta?.label ?? job.status}
                  </Badge>
                </TableCell>
                <TableCell className="text-right font-data text-xs tabular-nums">
                  {job.attempts}
                  <span className="text-muted-foreground"> / {MAX_ATTEMPTS}</span>
                </TableCell>
                <TableCell className="font-data text-xs tabular-nums">{job.userId ? `#${job.userId}` : "—"}</TableCell>
                <TableCell className="text-[11px] text-muted-foreground" title={formatTime(job.firedAt)}>
                  {formatRelative(job.firedAt)}
                </TableCell>
                <TableCell className="text-[11px] text-muted-foreground" title={formatTime(job.runAfter)}>
                  {job.status === "pending" ? formatRelative(job.runAfter) : job.finishedAt ? `完成于${formatRelative(job.finishedAt)}` : "—"}
                </TableCell>
                <TableCell className="max-w-56 truncate text-[11px] text-destructive" title={job.lastError}>
                  {job.lastError || ""}
                </TableCell>
                <TableCell className="pr-4 text-right sm:pr-5">
                  {job.status === "failed" ? (
                    <Button
                      size="xs"
                      variant="outline"
                      disabled={retry.isPending && retry.variables?.jobId === job.id}
                      onClick={async () => {
                        try {
                          await retry.mutateAsync({ name: functionName, jobId: job.id });
                          toast.success("已放回队列");
                        } catch (error) {
                          toast.error(errorMessage(error));
                        }
                      }}
                    >
                      <RotateCcw className="size-3" />
                      重试
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            );
          })}
          {!items.length ? (
            <TableRow>
              <TableCell colSpan={9} className="py-10 text-center text-xs text-muted-foreground">
                {query.isLoading ? "加载中…" : "暂无触发任务"}
              </TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>
    </OverviewCard>
  );
}
