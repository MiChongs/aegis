"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  FlaskConical,
  Loader2,
  Play,
  RotateCcw,
  Search,
  X
} from "lucide-react";
import { toast } from "sonner";
import type {
  AppFunction,
  AppFunctionInvocation,
  AppFunctionResult,
  AppFunctionTestResult,
  FunctionCatalog
} from "@/lib/api/app-functions";
import {
  useFunctionInvocationQuery,
  useFunctionInvocationsQuery,
  useFunctionTriggersQuery,
  useInvokeFunctionMutation,
  useReplayInvocationMutation
} from "@/lib/function-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { CopyButton } from "@/components/users/detail/kit/surface";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { describeCron } from "./function-cron";
import { JsonEditor } from "./json-editor";
import {
  CALLER_LABELS,
  DetailSection,
  EffectList,
  INVOCATION_STATUS,
  InvocationStatusBadge,
  JsonBlock,
  LogList,
  SOURCE_LABELS,
  SourceBadge,
  TestResultView,
  UsageMeters,
  errorMessage,
  formatDuration,
  formatRelative,
  formatTime
} from "./function-shared";

const PAGE_SIZE = 20;

/**
 * 调用审计 + 真实调用入口。
 *
 * 筛选是必需的而不是锦上添花：排障时看的从来不是「最近 20 条」，
 * 而是「失败的那几条」「定时触发的那几条」「这个用户的那几条」——
 * 一个每分钟几百次调用的函数，靠时间倒序永远翻不到它们。
 * 任一筛选变化都回到第 1 页；表格里的调用方可点，点了即按它筛选。
 */
export function FunctionInvocationsPanel({
  appKey,
  selected,
  catalog,
  initialInvocationId,
  initialTriggerId
}: {
  appKey: string;
  selected: AppFunction;
  catalog?: FunctionCatalog;
  initialInvocationId?: number;
  initialTriggerId?: number;
}) {
  const [status, setStatus] = useState("all");
  const [source, setSource] = useState("all");
  const [callerType, setCallerType] = useState("all");
  const [eventId, setEventId] = useState("");
  const [callerId, setCallerId] = useState("");
  const [triggerId, setTriggerId] = useState(initialTriggerId ? String(initialTriggerId) : "all");
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<number | null>(initialInvocationId ?? null);
  const [invokeOpen, setInvokeOpen] = useState(false);
  const debouncedEventId = useDebouncedValue(eventId.trim(), 300);
  const debouncedCallerId = useDebouncedValue(callerId, 300);

  const triggersQuery = useFunctionTriggersQuery(appKey, selected.name);
  const triggers = useMemo(() => triggersQuery.data ?? [], [triggersQuery.data]);
  const events = catalog?.triggerEvents ?? [];

  const query = useFunctionInvocationsQuery(appKey, selected.name, {
    status: status === "all" ? undefined : status,
    source: source === "all" ? undefined : source,
    callerType: callerType === "all" ? undefined : callerType,
    eventId: debouncedEventId || undefined,
    callerId: debouncedCallerId ? Number(debouncedCallerId) : undefined,
    triggerId: triggerId === "all" ? undefined : Number(triggerId),
    page,
    limit: PAGE_SIZE
  });
  const list = query.data?.list ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / (query.data?.limit || PAGE_SIZE)));

  function changeFilter(apply: () => void) {
    apply();
    setPage(1);
  }

  const filtered =
    status !== "all" || source !== "all" || callerType !== "all" || eventId || callerId || triggerId !== "all";

  function triggerLabel(id: number) {
    const trigger = triggers.find((item) => item.id === id);
    if (!trigger) return `#${id}`;
    if (trigger.kind === "event") return events.find((item) => item.key === trigger.event)?.label ?? trigger.event ?? `#${id}`;
    return describeCron(trigger.cron ?? "") ?? trigger.cron ?? `#${id}`;
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-medium">调用审计</h3>
        <span className="font-data text-xs text-muted-foreground tabular-nums">共 {total.toLocaleString("zh-CN")} 条</span>
        <Button size="sm" variant="outline" className="ml-auto h-8 text-xs" onClick={() => setInvokeOpen(true)}>
          <Play className="size-3.5" />
          真实调用
        </Button>
      </div>

      <Card className="gap-0 overflow-hidden py-0">
        <div className="flex flex-wrap items-center gap-2 border-b p-2.5">
          <div className="flex items-center gap-0.5 rounded-lg border p-0.5">
            {[["all", "全部"], ...Object.entries(INVOCATION_STATUS).map(([key, meta]) => [key, meta.label])].map(([key, label]) => (
              <button
                key={key}
                type="button"
                onClick={() => changeFilter(() => setStatus(key))}
                className={cn(
                  "rounded-md px-2 py-1 text-xs transition-colors",
                  status === key ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <Select value={source} onValueChange={(value) => changeFilter(() => setSource(value))}>
            <SelectTrigger size="sm" className="h-8 w-28 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部来源</SelectItem>
              {Object.entries(SOURCE_LABELS).map(([key, label]) => (
                <SelectItem key={key} value={key}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={callerType} onValueChange={(value) => changeFilter(() => setCallerType(value))}>
            <SelectTrigger size="sm" className="h-8 w-32 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部调用方</SelectItem>
              {Object.entries(CALLER_LABELS).map(([key, label]) => (
                <SelectItem key={key} value={key}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {triggers.length ? (
            <Select value={triggerId} onValueChange={(value) => changeFilter(() => setTriggerId(value))}>
              <SelectTrigger size="sm" className="h-8 w-36 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部触发器</SelectItem>
                {triggers.map((item) => (
                  <SelectItem key={item.id} value={String(item.id)}>
                    {triggerLabel(item.id)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
          <Input
            className="h-8 w-28 font-data text-xs"
            value={callerId}
            onChange={(event) => changeFilter(() => setCallerId(event.target.value.replace(/\D/g, "")))}
            placeholder="调用方 ID"
            inputMode="numeric"
          />
          <div className="relative min-w-40 flex-1">
            <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 pl-7 font-mono text-xs"
              value={eventId}
              onChange={(event) => changeFilter(() => setEventId(event.target.value))}
              placeholder="事件 ID 精确匹配"
            />
          </div>
          {filtered ? (
            <Button
              size="sm"
              variant="ghost"
              className="h-8 text-xs"
              onClick={() =>
                changeFilter(() => {
                  setStatus("all");
                  setSource("all");
                  setCallerType("all");
                  setEventId("");
                  setCallerId("");
                  setTriggerId("all");
                })
              }
            >
              <X className="size-3.5" />
              清除
            </Button>
          ) : null}
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-3">时间</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>来源</TableHead>
              <TableHead>调用方</TableHead>
              <TableHead>版本</TableHead>
              <TableHead className="text-right">耗时</TableHead>
              <TableHead>事件 ID</TableHead>
              <TableHead className="pr-3">错误</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.map((item) => (
              <TableRow
                key={item.id}
                className={cn("cursor-pointer", detailId === item.id && "bg-muted/60")}
                onClick={() => setDetailId(item.id)}
              >
                <TableCell className="pl-3 text-[11px] text-muted-foreground" title={formatTime(item.createdAt)}>
                  {formatRelative(item.createdAt)}
                </TableCell>
                <TableCell>
                  <InvocationStatusBadge status={item.status} />
                </TableCell>
                <TableCell>
                  <span className="flex items-center gap-1">
                    <SourceBadge source={item.source} />
                    {item.triggerId ? (
                      <span className="max-w-24 truncate text-[11px] text-muted-foreground">{triggerLabel(item.triggerId)}</span>
                    ) : null}
                  </span>
                </TableCell>
                <TableCell className="text-xs">
                  {CALLER_LABELS[item.callerType] ?? item.callerType}
                  {item.callerId ? (
                    <button
                      type="button"
                      className="ml-1 font-data text-muted-foreground hover:text-foreground hover:underline"
                      onClick={(event) => {
                        event.stopPropagation();
                        changeFilter(() => setCallerId(String(item.callerId)));
                      }}
                    >
                      #{item.callerId}
                    </button>
                  ) : null}
                </TableCell>
                <TableCell className="font-mono text-[11px] text-muted-foreground">{item.version || "—"}</TableCell>
                <TableCell className="text-right font-data text-xs tabular-nums">{formatDuration(item.durationMs)}</TableCell>
                <TableCell className="max-w-40 truncate font-mono text-[11px]">{item.eventId}</TableCell>
                <TableCell className="max-w-64 truncate pr-3 text-[11px] text-destructive" title={item.errorMessage}>
                  {item.errorCode ? <span className="mr-1 font-mono text-muted-foreground">{item.errorCode}</span> : null}
                  {item.errorMessage || ""}
                </TableCell>
              </TableRow>
            ))}
            {!list.length ? (
              <TableRow>
                <TableCell colSpan={8} className="py-12 text-center text-xs text-muted-foreground">
                  {query.isLoading ? "加载中…" : "暂无调用记录"}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>

        {total > PAGE_SIZE ? (
          <div className="flex items-center justify-end gap-2 border-t px-3 py-2 text-xs text-muted-foreground">
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
        ) : null}
      </Card>

      <Sheet open={detailId !== null} onOpenChange={(open) => !open && setDetailId(null)}>
        <SheetContent className="w-full gap-0 sm:max-w-2xl">
          {detailId !== null ? (
            <InvocationDetail
              key={detailId}
              appKey={appKey}
              functionName={selected.name}
              invocationId={detailId}
              fallback={list.find((item) => item.id === detailId)}
              catalog={catalog}
              triggerLabel={triggerLabel}
            />
          ) : null}
        </SheetContent>
      </Sheet>

      {invokeOpen ? <InvokeDialog appKey={appKey} selected={selected} onClose={() => setInvokeOpen(false)} /> : null}
    </div>
  );
}

/**
 * 一条调用的完整记录：入参、返回值、副作用时间线、结构化日志、额度用量，
 * 以及「用这次的入参重放」—— 在当前激活版本上试跑一遍，只记不做。
 * 线上那条失败调用为什么失败，过去只能去翻服务器日志。
 */
function InvocationDetail({
  appKey,
  functionName,
  invocationId,
  fallback,
  catalog,
  triggerLabel
}: {
  appKey: string;
  functionName: string;
  invocationId: number;
  fallback?: AppFunctionInvocation;
  catalog?: FunctionCatalog;
  triggerLabel: (id: number) => string;
}) {
  const query = useFunctionInvocationQuery(appKey, functionName, invocationId);
  const replay = useReplayInvocationMutation(appKey);
  const [replayResult, setReplayResult] = useState<AppFunctionTestResult | null>(null);
  const detail = query.data;
  const head = detail ?? fallback;
  const canReplay = Boolean(detail && !detail.inputTruncated && detail.input !== undefined);

  return (
    <>
      <SheetHeader className="space-y-2 border-b">
        <SheetTitle className="flex items-center gap-2 text-sm">
          {head ? <InvocationStatusBadge status={head.status} /> : null}
          <span className="truncate font-mono">{head?.eventId ?? `#${invocationId}`}</span>
          {head ? <CopyButton value={head.eventId} label="事件 ID" /> : null}
        </SheetTitle>
        <SheetDescription asChild>
          <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
            <Meta label="来源">
              {head?.source ? <SourceBadge source={head.source} /> : "—"}
              {head?.triggerId ? <span className="ml-1 text-muted-foreground">{triggerLabel(head.triggerId)}</span> : null}
            </Meta>
            <Meta label="调用方">
              {head ? CALLER_LABELS[head.callerType] ?? head.callerType : "—"}
              {head?.callerId ? <span className="ml-1 font-data text-muted-foreground">#{head.callerId}</span> : null}
            </Meta>
            <Meta label="版本">
              <span className="font-mono">{head?.version || "—"}</span>
            </Meta>
            <Meta label="耗时">
              <span className="font-data tabular-nums">{head ? formatDuration(head.durationMs) : "—"}</span>
            </Meta>
            <Meta label="时间">{formatTime(head?.createdAt)}</Meta>
            <Meta label="请求摘要">
              <span className="font-mono">{head?.requestSha256.slice(0, 12) ?? "—"}</span>
            </Meta>
            <Meta label="响应摘要">
              <span className="font-mono">{head?.responseSha256?.slice(0, 12) || "—"}</span>
            </Meta>
            <Meta label="记录编号">
              <span className="font-data">#{invocationId}</span>
            </Meta>
          </div>
        </SheetDescription>
      </SheetHeader>

      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-4 p-4">
          {query.isLoading && !detail ? <Skeleton className="h-40 w-full rounded-lg" /> : null}
          {query.isError ? <p className="text-xs text-destructive">{errorMessage(query.error)}</p> : null}

          {head?.errorMessage ? (
            <DetailSection title="错误">
              <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
                {head.errorCode ? <span className="mr-1.5 font-mono text-[11px] text-muted-foreground">{head.errorCode}</span> : null}
                <span className="break-all">{head.errorMessage}</span>
              </div>
            </DetailSection>
          ) : null}

          {detail ? (
            <>
              <DetailSection title="入参">
                {detail.inputTruncated ? (
                  <p className="rounded-lg border border-dashed p-3 text-xs text-muted-foreground">超过留存上限，未保留</p>
                ) : (
                  <JsonBlock value={detail.input ?? {}} />
                )}
              </DetailSection>

              {detail.result?.output !== undefined && detail.result?.output !== null ? (
                <DetailSection title="返回值">
                  <JsonBlock value={detail.result.output} />
                </DetailSection>
              ) : null}

              {detail.usage ? (
                <DetailSection title="额度">
                  <UsageMeters usage={detail.usage} limits={catalog?.limits} />
                </DetailSection>
              ) : null}

              <DetailSection title={`副作用（${detail.result?.effects?.length ?? 0}）`}>
                <EffectList effects={detail.result?.effects ?? []} timeline />
              </DetailSection>

              <DetailSection title={`日志（${detail.logs?.length ?? 0}）`}>
                <LogList logs={detail.logs ?? []} />
              </DetailSection>
            </>
          ) : null}

          <div className="space-y-3 rounded-xl border bg-muted/20 p-3">
            <div className="flex flex-wrap items-center gap-2">
              <FlaskConical className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">重放</span>
              <span className="text-[11px] text-muted-foreground">在激活版本上试跑，写操作不执行</span>
              <Button
                size="sm"
                className="ml-auto h-8 text-xs"
                disabled={!canReplay || replay.isPending}
                onClick={async () => {
                  try {
                    const result = await replay.mutateAsync({ name: functionName, invocationId });
                    setReplayResult(result);
                    if (result.ok) toast.success("重放通过");
                    else toast.warning("重放未通过");
                  } catch (error) {
                    toast.error(errorMessage(error));
                  }
                }}
              >
                {replay.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCcw className="size-3.5" />}
                用这次的入参重放
              </Button>
            </div>
            {detail?.inputTruncated ? <p className="text-[11px] text-muted-foreground">入参未保留，无法重放</p> : null}
            {replayResult ? <TestResultView result={replayResult} limits={catalog?.limits} /> : null}
          </div>
        </div>
      </ScrollArea>
    </>
  );
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 rounded-lg border px-2 py-1.5">
      <p className="text-[10px] text-muted-foreground">{label}</p>
      <div className="mt-0.5 flex min-w-0 items-center truncate text-xs text-foreground">{children}</div>
    </div>
  );
}

function hasSchema(schema?: Record<string, unknown>) {
  return Boolean(schema && Object.keys((schema.properties as object) ?? {}).length > 0);
}

/** 真实调用：写操作真实生效、写调用审计。可代某个应用用户发起（来源记为控制台） */
function InvokeDialog({ appKey, selected, onClose }: { appKey: string; selected: AppFunction; onClose: () => void }) {
  const [input, setInput] = useState(selected.inputSample || "{}");
  const [user, setUser] = useState<PickedUser | null>(null);
  const [result, setResult] = useState<AppFunctionResult | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const invoke = useInvokeFunctionMutation(appKey);
  const callable = selected.status === "active" && Boolean(selected.activeVersion);

  async function run() {
    let parsed: unknown;
    try {
      parsed = JSON.parse(input);
    } catch {
      toast.error("输入不是合法 JSON");
      return;
    }
    setFailure(null);
    try {
      const response = await invoke.mutateAsync({
        name: selected.name,
        payload: { input: parsed, asUserId: user?.id }
      });
      setResult(response);
      toast.success("调用成功");
    } catch (error) {
      setResult(null);
      setFailure(errorMessage(error));
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>真实调用</DialogTitle>
          <DialogDescription className="font-mono text-xs">
            {selected.name}
            {selected.activeVersion ? ` ${selected.activeVersion}` : ""}
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[68vh] pr-3">
          <div className="space-y-4">
            <Alert className="border-amber-500/40 bg-amber-500/5">
              <AlertTriangle className="text-amber-600 dark:text-amber-400" />
              <AlertDescription className="text-xs">
                {callable ? "写操作会真实生效并记入调用审计。" : "函数当前不可被调用。"}
              </AlertDescription>
            </Alert>
            <div className="space-y-1.5">
              <Label className="text-xs">以用户身份</Label>
              <div className="flex items-center gap-2">
                <div className="min-w-0 flex-1">
                  <UserPicker appKey={appKey} value={user} onChange={setUser} placeholder="不选则以管理员身份" />
                </div>
                {user ? (
                  <Button size="icon-sm" variant="ghost" aria-label="清除用户" onClick={() => setUser(null)}>
                    <X className="size-4" />
                  </Button>
                ) : null}
              </div>
            </div>
            <div className="space-y-1.5">
              <div className="flex items-center gap-2">
                <Label className="text-xs">入参</Label>
                <Badge variant="outline" size="sm" className="ml-auto font-normal">
                  {hasSchema(selected.inputSchema) ? "按契约校验" : "未配置契约"}
                </Badge>
              </div>
              <JsonEditor
                height={180}
                value={input}
                onChange={setInput}
                schema={hasSchema(selected.inputSchema) ? selected.inputSchema : undefined}
              />
            </div>
            {failure ? (
              <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs text-destructive">{failure}</div>
            ) : null}
            {result ? (
              <div className="space-y-3 rounded-lg border p-3">
                <div className="flex items-center gap-2 text-xs">
                  <Badge variant="success" size="sm">
                    调用成功
                  </Badge>
                  <code className="font-mono text-[11px] text-muted-foreground">{result.eventId}</code>
                  <CopyButton value={result.eventId} label="事件 ID" />
                </div>
                {result.output !== undefined && result.output !== null ? (
                  <DetailSection title="返回值">
                    <JsonBlock value={result.output} />
                  </DetailSection>
                ) : null}
                <DetailSection title={`副作用（${result.effects?.length ?? 0}）`}>
                  <EffectList effects={result.effects ?? []} timeline />
                </DetailSection>
              </div>
            ) : null}
          </div>
        </ScrollArea>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
          <Button disabled={invoke.isPending || !callable} onClick={run}>
            {invoke.isPending ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
            执行
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
