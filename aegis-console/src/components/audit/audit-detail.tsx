"use client";

import { useMemo, useState, type ReactNode } from "react";
import { ChevronDown, Copy, FileText, Layers, Loader2 } from "lucide-react";
import { toast } from "sonner";
import type { AuditLog } from "@/lib/api/types";
import { useAuditLogQuery } from "@/lib/audit-hooks";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { JsonViewer } from "@/components/ui/json-viewer";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  DATA_FONT,
  ModuleBadge,
  RiskBadge,
  StatusBadge,
  describeSentence,
  formatAbsolute,
  formatBytes,
  formatLatency,
  isFailure,
  kindLabel,
  roleLabel,
  severityLabel
} from "./audit-shared";

/**
 * 审计详情。顶部一句话说清这条记录，下面按「结果、对象、变更、文件、请求、来源、追踪」分区。
 * 列表里的数据先用来渲染，详情接口返回后替换（字段一致，详情接口保证拿到最新的展示推导）。
 */
export function AuditDetail({
  log,
  open,
  onOpenChange,
  onSameSession,
  onSameAdmin
}: {
  log: AuditLog | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSameSession: (sessionId: string) => void;
  onSameAdmin: (adminId: number) => void;
}) {
  const detailQuery = useAuditLogQuery(open ? log?.id : null);
  const item = detailQuery.data ?? log;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[94vw] max-w-2xl flex-col gap-0 p-0">
        {item ? (
          <>
            <SheetHeader className="shrink-0 space-y-2 border-b px-6 py-4">
              <div className="flex flex-wrap items-center gap-1.5">
                <StatusBadge status={item.status} />
                <RiskBadge severity={item.severity} always />
                <ModuleBadge label={item.moduleLabel} />
                <span className="text-[11px] text-muted-foreground">{kindLabel(item.kind)}</span>
                {detailQuery.isFetching ? <Loader2 className="size-3 animate-spin text-muted-foreground" /> : null}
              </div>
              <SheetTitle className="text-base leading-snug">{describeSentence(item)}</SheetTitle>
              <SheetDescription className={cn("text-xs", DATA_FONT)}>{formatAbsolute(item.createdAt)}</SheetDescription>
            </SheetHeader>
            <ScrollArea className="min-h-0 flex-1">
              <div className="space-y-5 px-6 py-5">
                <DetailBody log={item} onSameSession={onSameSession} onSameAdmin={onSameAdmin} />
              </div>
            </ScrollArea>
          </>
        ) : (
          <div className="space-y-3 p-6">
            <SheetTitle className="sr-only">审计详情</SheetTitle>
            <Skeleton className="h-5 w-2/3" />
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function DetailBody({
  log,
  onSameSession,
  onSameAdmin
}: {
  log: AuditLog;
  onSameSession: (sessionId: string) => void;
  onSameAdmin: (adminId: number) => void;
}) {
  const changes = (log.changes ?? {}) as Record<string, unknown>;
  const diff = changes.diff as { before?: unknown; after?: unknown } | undefined;
  const files = Array.isArray(changes.files) ? (changes.files as Array<{ name?: string; size?: number }>) : [];
  const body = changes.request_body;
  const query = changes.query;
  const params = changes.route_params;
  const headers = changes.headers;

  return (
    <>
      <Section title="结果">
        <Fields
          rows={[
            ["结果", <StatusBadge key="s" status={log.status} />],
            ["失败原因", isFailure(log) ? log.errorMessage || "未返回原因" : null],
            ["错误码", log.errorCode],
            ["风险等级", severityLabel(log.severity)],
            ["耗时", formatLatency(log.latencyMs)]
          ]}
        />
      </Section>

      <Section title="操作">
        <Fields
          rows={[
            ["操作", log.operationName],
            ["模块", log.moduleLabel],
            ["对象", [log.targetType, log.targetLabel].filter(Boolean).join(" ") || null],
            ["应用", log.appName ? `${log.appName}（ID ${log.appId}）` : null],
            [
              "操作者",
              <span key="a" className="flex flex-wrap items-center gap-2">
                <span>
                  {log.adminName || "未知管理员"}
                  {log.adminRole ? <span className="ml-1.5 text-muted-foreground">{roleLabel(log.adminRole)}</span> : null}
                </span>
                {log.adminId ? (
                  <Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => onSameAdmin(log.adminId)}>
                    查看此人的操作
                  </Button>
                ) : null}
              </span>
            ],
            ["说明", log.summary && log.summary !== `${log.operationName} ${log.targetLabel ?? ""}`.trim() ? log.summary : null]
          ]}
        />
      </Section>

      {diff && (diff.before !== undefined || diff.after !== undefined) ? (
        <Section title="变更内容">
          <DiffTable before={diff.before} after={diff.after} />
        </Section>
      ) : null}

      {files.length > 0 ? (
        <Section title="上传的文件">
          <div className="divide-y rounded-lg border">
            {files.map((file, index) => (
              <div key={`${file.name}-${index}`} className="flex items-center gap-2 px-3 py-2 text-sm">
                <FileText className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{file.name || "未命名文件"}</span>
                <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{formatBytes(file.size)}</span>
              </div>
            ))}
          </div>
        </Section>
      ) : null}

      {hasContent(body) || hasContent(query) || hasContent(params) ? (
        <Section title="请求参数" hint="敏感字段已脱敏，显示为 [REDACTED]">
          <div className="space-y-2">
            {hasContent(params) ? <JsonBlock title="路径参数" value={params} defaultOpen /> : null}
            {hasContent(query) ? <JsonBlock title="查询参数" value={query} defaultOpen /> : null}
            {hasContent(body) ? <JsonBlock title="请求体" value={body} defaultOpen={!hasContent(diff)} /> : null}
          </div>
        </Section>
      ) : null}

      <Section title="请求">
        <Fields
          mono
          rows={[
            ["方法", log.method],
            ["路径", log.path],
            ["路由", log.route],
            ["状态码", log.statusCode ? String(log.statusCode) : null],
            ["请求大小", formatBytes(log.requestSize)],
            ["响应大小", formatBytes(log.responseSize)]
          ]}
        />
        {log.responseSnippet ? <JsonBlock title="响应摘要" value={log.responseSnippet} /> : null}
      </Section>

      <Section title="来源">
        <Fields
          rows={[
            ["IP", log.ip],
            ["地点", log.location],
            ["运营商", log.isp],
            ["浏览器", log.browser],
            ["系统", log.os],
            ["User-Agent", log.userAgent ? <span key="ua" className={cn("break-all text-xs", DATA_FONT)}>{log.userAgent}</span> : null]
          ]}
        />
        {hasContent(headers) ? <JsonBlock title="请求头" value={headers} /> : null}
      </Section>

      <Section title="追踪">
        <Fields
          rows={[
            ["请求 ID", log.requestId ? <CopyValue key="r" value={log.requestId} /> : null],
            ["Trace ID", log.traceId ? <CopyValue key="t" value={log.traceId} /> : null],
            ["会话 ID", log.sessionId ? <CopyValue key="s" value={log.sessionId} /> : null],
            ["日志 ID", <CopyValue key="i" value={String(log.id)} />]
          ]}
        />
        {log.sessionId ? (
          <Button variant="outline" size="sm" className="mt-2" onClick={() => onSameSession(log.sessionId as string)}>
            <Layers className="size-3.5" />
            查看同一会话的操作
          </Button>
        ) : null}
      </Section>
    </>
  );
}

function hasContent(value: unknown) {
  if (value === undefined || value === null || value === "") return false;
  if (typeof value === "object") return Object.keys(value as object).length > 0;
  return true;
}

function Section({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-xs font-semibold text-muted-foreground">{title}</h3>
        {hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </section>
  );
}

function Fields({ rows, mono }: { rows: Array<[string, ReactNode]>; mono?: boolean }) {
  const visible = rows.filter(([, value]) => value !== null && value !== undefined && value !== "" && value !== "—");
  if (visible.length === 0) return <p className="text-xs text-muted-foreground">无</p>;
  return (
    <dl className="divide-y rounded-lg border">
      {visible.map(([label, value]) => (
        <div key={label} className="grid grid-cols-[88px_1fr] gap-3 px-3 py-2 text-sm">
          <dt className="text-xs leading-5 text-muted-foreground">{label}</dt>
          <dd className={cn("min-w-0 break-words", mono && typeof value === "string" && cn("text-xs leading-5", DATA_FONT))}>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function CopyValue({ value }: { value: string }) {
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard.writeText(value).then(
          () => toast.success("已复制"),
          () => toast.error("复制失败")
        );
      }}
      className={cn("group inline-flex max-w-full items-center gap-1.5 text-left text-xs", DATA_FONT)}
      title="点击复制"
    >
      <span className="truncate">{value}</span>
      <Copy className="size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
    </button>
  );
}

function stringify(value: unknown) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function JsonBlock({ title, value, defaultOpen = false }: { title: string; value: unknown; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const text = stringify(value);
  const lines = text.split("\n").length;
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="rounded-lg border">
      <CollapsibleTrigger className="flex w-full items-center justify-between px-3 py-2 text-xs">
        <span className="font-medium">{title}</span>
        <ChevronDown className={cn("size-3.5 text-muted-foreground transition-transform", open && "rotate-180")} />
      </CollapsibleTrigger>
      <CollapsibleContent className="border-t">
        <JsonViewer value={text} language={typeof value === "string" && !looksLikeJSON(value) ? "text" : "json"} height={Math.min(320, 20 * lines + 24)} />
      </CollapsibleContent>
    </Collapsible>
  );
}

function looksLikeJSON(value: string) {
  const trimmed = value.trim();
  return trimmed.startsWith("{") || trimmed.startsWith("[");
}

/** 变更前后的字段对比。只比较第一层字段，嵌套值按 JSON 展示；默认只列有变化的字段。 */
function DiffTable({ before, after }: { before: unknown; after: unknown }) {
  const [showAll, setShowAll] = useState(false);
  const rows = useMemo(() => {
    const b = isRecord(before) ? before : { 值: before };
    const a = isRecord(after) ? after : { 值: after };
    const keys = Array.from(new Set([...Object.keys(b), ...Object.keys(a)]));
    return keys.map((key) => {
      const left = b[key];
      const right = a[key];
      return { key, left, right, changed: stringify(left ?? null) !== stringify(right ?? null) };
    });
  }, [before, after]);
  const changed = rows.filter((row) => row.changed);
  const visible = showAll ? rows : changed;

  return (
    <div className="space-y-2">
      <div className="overflow-hidden rounded-lg border">
        <div className="grid grid-cols-[120px_1fr_1fr] gap-3 border-b bg-muted/30 px-3 py-1.5 text-[11px] font-medium text-muted-foreground">
          <span>字段</span>
          <span>变更前</span>
          <span>变更后</span>
        </div>
        {visible.length === 0 ? (
          <p className="px-3 py-4 text-center text-xs text-muted-foreground">字段没有变化</p>
        ) : (
          <div className="divide-y">
            {visible.map((row) => (
              <div key={row.key} className="grid grid-cols-[120px_1fr_1fr] gap-3 px-3 py-2 text-xs">
                <span className={cn("break-all", DATA_FONT)}>{row.key}</span>
                <DiffValue value={row.left} tone={row.changed ? "before" : undefined} />
                <DiffValue value={row.right} tone={row.changed ? "after" : undefined} />
              </div>
            ))}
          </div>
        )}
      </div>
      {rows.length !== changed.length ? (
        <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setShowAll((value) => !value)}>
          {showAll ? `只看变更的 ${changed.length} 个字段` : `显示全部 ${rows.length} 个字段`}
        </Button>
      ) : null}
    </div>
  );
}

function DiffValue({ value, tone }: { value: unknown; tone?: "before" | "after" }) {
  const empty = value === undefined || value === null || value === "";
  return (
    <span
      className={cn(
        "min-w-0 whitespace-pre-wrap break-all rounded px-1",
        DATA_FONT,
        tone === "before" && "bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300",
        tone === "after" && "bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300",
        empty && "text-muted-foreground"
      )}
    >
      {empty ? "空" : stringify(value)}
    </span>
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
