"use client";

import { useMemo, useState } from "react";
import { VirtualList } from "@/components/ui/virtual-list";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import {
  Activity,
  AlertOctagon,
  CheckCircle2,
  Clock,
  Copy,
  Download,
  Filter,
  Globe,
  Layers,
  MapPin,
  Monitor,
  Network,
  RotateCcw,
  Search,
  ShieldAlert,
  Timer,
  User,
  XCircle,
  Zap,
} from "lucide-react";
import { useAuditLogsInfiniteQuery, useAuditStatsQuery } from "@/lib/admin-hooks";
import type { AuditLog } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { JsonViewer } from "@/components/ui/json-viewer";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

// ─── 常量 ─────────────────────────────────────────

const CATEGORY_OPTIONS = [
  { value: "all", label: "全部业务域" },
  { value: "auth", label: "认证" },
  { value: "admin", label: "管理员" },
  { value: "user", label: "用户" },
  { value: "app", label: "应用" },
  { value: "rbac", label: "角色权限" },
  { value: "content", label: "内容" },
  { value: "storage", label: "存储" },
  { value: "workflow", label: "工作流" },
  { value: "release", label: "发布" },
  { value: "settings", label: "平台设置" },
  { value: "security", label: "安全" },
  { value: "payment", label: "支付" },
  { value: "points", label: "积分" },
  { value: "email", label: "邮件" },
  { value: "audit", label: "审计" },
];

const SEVERITY_OPTIONS = [
  { value: "all", label: "全部严重度" },
  { value: "info", label: "普通" },
  { value: "low", label: "低" },
  { value: "medium", label: "中" },
  { value: "high", label: "高" },
  { value: "critical", label: "严重" },
];

const STATUS_OPTIONS = [
  { value: "all", label: "全部状态" },
  { value: "success", label: "成功" },
  { value: "failed", label: "失败" },
  { value: "denied", label: "拒绝" },
  { value: "blocked", label: "拦截" },
];

const CATEGORY_LABEL: Record<string, string> = Object.fromEntries(
  CATEGORY_OPTIONS.filter((o) => o.value !== "all").map((o) => [o.value, o.label])
);

const SEVERITY_LABEL: Record<string, string> = {
  info: "普通",
  low: "低",
  medium: "中",
  high: "高",
  critical: "严重",
};

const STATUS_LABEL: Record<string, string> = {
  success: "成功",
  failed: "失败",
  denied: "拒绝",
  blocked: "拦截",
};

// ─── 工具函数 ─────────────────────────────────────

function fmtTime(v?: string) {
  if (!v) return "--";
  return new Date(v).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function fmtBytes(v?: number) {
  if (!v || v <= 0) return "--";
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`;
  return `${(v / 1024 / 1024).toFixed(2)} MB`;
}

function severityVariant(severity?: string): "secondary" | "success" | "warning" | "danger" {
  switch (severity) {
    case "critical":
    case "high":
      return "danger";
    case "medium":
      return "warning";
    case "low":
      return "success";
    default:
      return "secondary";
  }
}

function statusVariant(status: string): "success" | "warning" | "danger" | "secondary" {
  switch (status) {
    case "success":
      return "success";
    case "failed":
      return "danger";
    case "denied":
    case "blocked":
      return "warning";
    default:
      return "secondary";
  }
}

function statusCodeClass(code?: number) {
  // 用项目自定义 utility（globals.css）代替 Tailwind `dark:` 变体，
  // 避免 Tailwind v4 JIT 对某些 dark: 类缓存不重编、或 radix/next-themes
  // 挂载时序下 dark variant 未生效的问题。
  if (!code) return "status-code-idle";
  if (code >= 500) return "status-code-5xx";
  if (code >= 400) return "status-code-4xx";
  if (code >= 300) return "status-code-3xx";
  return "status-code-success";
}

function methodClass(method?: string) {
  // 同上：改走 chip-method CSS 级联，保证深浅色模式都稳定收敛。
  switch (method?.toUpperCase()) {
    case "POST":
      return "chip-method chip-method--post";
    case "PUT":
    case "PATCH":
      return "chip-method chip-method--put";
    case "DELETE":
      return "chip-method chip-method--delete";
    case "GET":
      return "chip-method chip-method--get";
    default:
      return "chip-method chip-method--other";
  }
}

function locationText(log: AuditLog) {
  const parts = [log.country, log.region, log.city].map((x) => (x || "").trim()).filter(Boolean);
  return parts.join(" · ");
}

async function copyToClipboard(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success("已复制");
  } catch {
    toast.error("复制失败");
  }
}

// ─── 页面主体 ────────────────────────────────────

export default function AuditPage() {
  const [keyword, setKeyword] = useState("");
  const [action, setAction] = useState("");
  const [category, setCategory] = useState("");
  const [severity, setSeverity] = useState("");
  const [status, setStatus] = useState("");
  const [startTime, setStartTime] = useState("");
  const [endTime, setEndTime] = useState("");
  const [selected, setSelected] = useState<AuditLog | null>(null);

  const statsQuery = useAuditStatsQuery();
  const logsQuery = useAuditLogsInfiniteQuery(
    { action, category, severity, status, keyword, startTime, endTime },
    50
  );

  const stats = statsQuery.data;

  // 累平所有页返回的 items —— VirtualList 只关心一个扁平数组
  const logs = useMemo<AuditLog[]>(
    () => logsQuery.data?.pages.flatMap((p) => p.items) ?? [],
    [logsQuery.data]
  );
  const total = logsQuery.data?.pages[0]?.total ?? 0;
  const hasMore = Boolean(logsQuery.hasNextPage);
  const isFetchingMore = logsQuery.isFetchingNextPage;

  const exportQS = useMemo(() => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries({ action, category, severity, status, keyword, startTime, endTime })) {
      if (v) q.set(k, v);
    }
    return q.toString();
  }, [action, category, severity, status, keyword, startTime, endTime]);

  const resetFilters = () => {
    setAction("");
    setCategory("");
    setSeverity("");
    setStatus("");
    setKeyword("");
    setStartTime("");
    setEndTime("");
  };

  const hasAnyFilter = Boolean(action || category || severity || status || keyword || startTime || endTime);

  // 与表头一致的列宽定义：一次定义双处使用，保证对齐
  const GRID_COLS =
    "180px 140px 90px 72px 180px minmax(220px, 1fr) 72px 72px 72px 140px 72px";

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="审计日志" description="完整记录管理员操作，包含请求追踪、地理信息、响应摘要与严重度分级。" />

      {/* 统计卡片（局部兜底：单个 StatCard 的某个字段异常不影响页面其余） */}
      {stats && (
        <WidgetBoundary title="统计加载失败" className="min-h-[84px]">
        <div className="grid gap-3 grid-cols-2 md:grid-cols-3 lg:grid-cols-6">
          <StatCard icon={<Activity className="size-3.5" />} label="今日操作" value={stats.todayCount} />
          <StatCard icon={<Clock className="size-3.5" />} label="本周操作" value={stats.weekCount} />
          <StatCard
            icon={<XCircle className="size-3.5" />}
            label="今日失败"
            value={stats.failedToday ?? 0}
            tone={stats.failedToday ? "danger" : undefined}
          />
          <StatCard
            icon={<ShieldAlert className="size-3.5" />}
            label="今日高危"
            value={stats.criticalToday ?? 0}
            tone={stats.criticalToday ? "warning" : undefined}
          />
          <StatCard
            icon={<Timer className="size-3.5" />}
            label="平均耗时"
            value={stats.avgLatencyMs ?? 0}
            hint="ms"
          />
          <StatCard icon={<User className="size-3.5" />} label="最活跃" value={stats.topAdmins?.[0]?.label || "--"} />
        </div>
        </WidgetBoundary>
      )}

      {/* 过滤器 */}
      <div className="rounded-xl border bg-card p-3 space-y-2.5">
        <div className="flex items-center gap-2 flex-wrap">
          <div className="relative flex-1 min-w-[240px]">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索管理员/摘要/路径/IP/错误信息/变更内容..."
              className="h-8 pl-8 text-xs"
            />
          </div>
          <Select value={category || "all"} onValueChange={(v) => setCategory(v === "all" ? "" : v)}>
            <SelectTrigger className="w-32 h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {CATEGORY_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={severity || "all"} onValueChange={(v) => setSeverity(v === "all" ? "" : v)}>
            <SelectTrigger className="w-28 h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {SEVERITY_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={status || "all"} onValueChange={(v) => setStatus(v === "all" ? "" : v)}>
            <SelectTrigger className="w-24 h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {STATUS_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <Input type="datetime-local" value={startTime} onChange={(e) => setStartTime(e.target.value)} className="h-8 w-44 text-xs" />
          <span className="text-xs text-muted-foreground">至</span>
          <Input type="datetime-local" value={endTime} onChange={(e) => setEndTime(e.target.value)} className="h-8 w-44 text-xs" />
          <Input
            value={action}
            onChange={(e) => setAction(e.target.value)}
            placeholder="Action 前缀（如 user.）"
            className="h-8 w-44 text-xs"
          />
          {hasAnyFilter && (
            <Button variant="ghost" size="sm" className="h-8" onClick={resetFilters}>
              <RotateCcw className="size-3.5" /> 重置
            </Button>
          )}
          <div className="flex-1" />
          <Button variant="outline" size="sm" className="h-8" asChild>
            <a href={`/api/admin/system/audit-logs/export?${exportQS}`} target="_blank" rel="noreferrer">
              <Download className="size-3.5" /> 导出 CSV
            </a>
          </Button>
        </div>
      </div>

      {/* 日志列表（虚拟化 + 无限滚动 + 局部兜底 + resetKeys 跟随筛选） */}
      <WidgetBoundary
        title="列表加载失败"
        description="审计日志拉取或渲染出错，可点击重试。"
        resetKeys={[action, category, severity, status, keyword, startTime, endTime]}
      >
      <div className="rounded-xl border bg-card overflow-hidden flex flex-col">
        {/* 外层横向滚动：确保窄屏下仍可看到所有列；内层纵向虚拟化 */}
        <div className="overflow-x-auto">
          <div className="min-w-[1280px]">
            {/* 表头 */}
            <div
              className="grid items-center bg-muted/40 border-b text-[11px] font-medium uppercase tracking-wider text-muted-foreground px-3 h-9"
              style={{ gridTemplateColumns: GRID_COLS }}
              role="row"
            >
              <div>时间</div>
              <div>操作者</div>
              <div>类别</div>
              <div>严重度</div>
              <div>动作</div>
              <div>摘要</div>
              <div>方法</div>
              <div>状态码</div>
              <div>耗时</div>
              <div>IP</div>
              <div>结果</div>
            </div>

            {/* 虚拟化行 */}
            <VirtualList<AuditLog>
              items={logs}
              estimateSize={48}
              overscan={12}
              height={560}
              getItemKey={(i) => logs[i]?.id ?? i}
              isLoading={logsQuery.isLoading}
              hasMore={hasMore}
              onEndReached={() => {
                if (hasMore && !isFetchingMore) logsQuery.fetchNextPage();
              }}
              endReachedThreshold={320}
              ariaLabel="审计日志列表"
              className="border-0"
              viewportClassName="bg-card"
              empty={
                <div className="inline-flex flex-col items-center gap-2 py-12 text-muted-foreground">
                  <Filter className="size-5 opacity-40" />
                  <span className="text-xs">暂无符合条件的审计记录</span>
                </div>
              }
              loader={
                <span className="inline-flex items-center gap-2">
                  <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground/60" />
                  {isFetchingMore ? "加载更多…" : "滚动触底自动加载"}
                </span>
              }
              renderItem={(log) => (
                <div
                  role="row"
                  tabIndex={0}
                  onClick={() => setSelected(log)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      setSelected(log);
                    }
                  }}
                  className="grid items-center gap-0 cursor-pointer border-b px-3 h-12 text-xs transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
                  style={{ gridTemplateColumns: GRID_COLS }}
                >
                  <div className="font-mono text-muted-foreground whitespace-nowrap truncate">{fmtTime(log.createdAt)}</div>
                  <div className="flex flex-col leading-tight min-w-0">
                    <span className="font-medium truncate">{log.adminName || "--"}</span>
                    {log.adminRole ? <span className="text-[10px] text-muted-foreground truncate">{log.adminRole}</span> : null}
                  </div>
                  <div>
                    {log.category ? (
                      <Badge variant="outline" className="min-w-[64px] justify-center font-normal">
                        {CATEGORY_LABEL[log.category] ?? log.category}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground">--</span>
                    )}
                  </div>
                  <div>
                    <Badge variant={severityVariant(log.severity)} className="min-w-[56px] justify-center">
                      {SEVERITY_LABEL[log.severity ?? "info"] ?? log.severity ?? "info"}
                    </Badge>
                  </div>
                  <div className="min-w-0">
                    <code className="font-mono text-[11px] text-muted-foreground truncate block">{log.action}</code>
                  </div>
                  <div className="min-w-0">
                    <div className="truncate" title={log.summary || log.detail}>
                      {log.summary || log.detail || "--"}
                    </div>
                  </div>
                  <div>
                    {log.method ? (
                      <span className={methodClass(log.method)}>{log.method}</span>
                    ) : (
                      <span className="text-muted-foreground">--</span>
                    )}
                  </div>
                  <div>
                    <span className={cn("font-mono font-semibold", statusCodeClass(log.statusCode))}>
                      {log.statusCode || "--"}
                    </span>
                  </div>
                  <div className="font-mono text-muted-foreground whitespace-nowrap">
                    {log.latencyMs ? `${log.latencyMs}ms` : "--"}
                  </div>
                  <div className="font-mono text-muted-foreground whitespace-nowrap truncate">{log.ip || "--"}</div>
                  <div>
                    <Badge variant={statusVariant(log.status)} className="min-w-[56px] justify-center">
                      {STATUS_LABEL[log.status] ?? log.status}
                    </Badge>
                  </div>
                </div>
              )}
            />
          </div>
        </div>

        {/* 底栏：总数 + 已加载 */}
        {total > 0 && (
          <div className="flex items-center justify-between border-t bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
            <span>
              共 <span className="font-semibold text-foreground">{total}</span> 条 · 已加载
              <span className="font-semibold text-foreground"> {logs.length}</span>
            </span>
            {hasMore ? <span className="text-[11px]">滚动到底部自动加载更多</span> : <span className="text-[11px]">已全部加载</span>}
          </div>
        )}
      </div>
      </WidgetBoundary>

      {/* 详情 Sheet */}
      <AuditDetailSheet log={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

// ─── 统计卡片 ────────────────────────────────────
// 保持与项目 shadcn/ui 设计语言一致：基础 bg-card + border，
// tone 只做温和的文字着色，不加渐变条 / 胶囊图标 / 脉冲指示灯等装饰。

function StatCard({
  label,
  value,
  icon,
  tone,
  hint,
}: {
  label: string;
  value: string | number;
  icon: React.ReactNode;
  tone?: "danger" | "warning";
  hint?: string;
}) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3 space-y-1">
      <div
        className={cn(
          "flex items-center gap-1.5 text-xs text-muted-foreground",
          tone === "danger" && "text-red-600 dark:text-red-400",
          tone === "warning" && "text-amber-600 dark:text-amber-400",
        )}
      >
        {icon}
        <span className="text-[11px] font-medium truncate">{label}</span>
      </div>
      <div className="flex items-baseline gap-1.5">
        <span className="text-lg font-semibold tracking-tight truncate">{value}</span>
        {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
      </div>
    </div>
  );
}

// ─── 详情 Sheet ──────────────────────────────────

function AuditDetailSheet({ log, onClose }: { log: AuditLog | null; onClose: () => void }) {
  const isOpen = Boolean(log);

  return (
    <Sheet open={isOpen} onOpenChange={(o) => !o && onClose()}>
      <SheetContent
        side="right"
        // 自适应宽度：小屏全屏；中屏起用 clamp 的 vw 策略，最多占 96vw，
        // 保证 UUID / Trace ID / Request ID 等长值永远不会被 sheet 右缘裁切。
        className="w-full p-0 flex flex-col gap-0 sm:max-w-none"
        style={{ maxWidth: "min(64rem, 96vw)", width: "clamp(360px, 96vw, 64rem)" }}
      >
        {log && (
          <>
            <SheetHeader className="px-6 pt-6 pb-4 border-b">
              <div className="flex items-center gap-2 flex-wrap">
                <Badge variant={severityVariant(log.severity)} className="tracking-normal">
                  {SEVERITY_LABEL[log.severity ?? "info"] ?? log.severity ?? "info"}
                </Badge>
                <Badge variant={statusVariant(log.status)} className="tracking-normal">
                  {STATUS_LABEL[log.status] ?? log.status}
                </Badge>
                {log.category && (
                  <Badge variant="outline" className="tracking-normal font-normal">
                    {CATEGORY_LABEL[log.category] ?? log.category}
                  </Badge>
                )}
                <span className="text-xs text-muted-foreground font-mono ml-auto">#{log.id}</span>
              </div>
              <SheetTitle className="text-base leading-6">
                {log.summary || log.detail || log.action}
              </SheetTitle>
              <SheetDescription className="text-xs font-mono">
                {log.action} · {fmtTime(log.createdAt)}
              </SheetDescription>
            </SheetHeader>

            <ScrollArea className="flex-1">
              <div className="px-6 py-5 space-y-6">
                {/* 身份 & 会话 */}
                <Section icon={<User className="size-3.5" />} title="操作者">
                  <Grid>
                    <Field label="管理员" value={log.adminName || "--"} />
                    <Field label="管理员 ID" value={String(log.adminId || "--")} mono />
                    <Field label="角色" value={log.adminRole || "--"} />
                    <Field label="会话 ID" value={log.sessionId || "--"} mono copyable />
                  </Grid>
                </Section>

                {/* 动作 */}
                <Section icon={<Zap className="size-3.5" />} title="动作">
                  <Grid>
                    <Field label="Action" value={log.action} mono copyable />
                    <Field label="业务域" value={log.category ? (CATEGORY_LABEL[log.category] ?? log.category) : "--"} />
                    <Field label="严重度" value={SEVERITY_LABEL[log.severity ?? "info"] ?? log.severity ?? "--"} />
                    <Field label="资源" value={log.resource || "--"} />
                    <Field label="资源 ID" value={log.resourceId || "--"} mono copyable />
                  </Grid>
                  {log.detail && (
                    <FieldBlock label="描述">
                      <p className="text-sm leading-6 break-all">{log.detail}</p>
                    </FieldBlock>
                  )}
                </Section>

                {/* 请求追踪 */}
                <Section icon={<Network className="size-3.5" />} title="请求追踪">
                  <Grid>
                    <Field label="Request ID" value={log.requestId || "--"} mono copyable />
                    <Field label="Trace ID" value={log.traceId || "--"} mono copyable />
                    <Field
                      label="HTTP 方法"
                      value={
                        log.method ? (
                          <span className={methodClass(log.method)} style={{ fontSize: "11px", padding: "2px 8px" }}>
                            {log.method}
                          </span>
                        ) : "--"
                      }
                    />
                    <Field
                      label="状态码"
                      value={
                        log.statusCode ? (
                          <span className={cn("font-mono font-semibold", statusCodeClass(log.statusCode))}>
                            {log.statusCode}
                          </span>
                        ) : "--"
                      }
                    />
                    <Field label="耗时" value={log.latencyMs ? `${log.latencyMs} ms` : "--"} mono />
                    <Field label="请求体" value={fmtBytes(log.requestSize)} mono />
                    <Field label="响应体" value={fmtBytes(log.responseSize)} mono />
                  </Grid>
                  {(log.path || log.route) && (
                    <div className="space-y-3">
                      {log.path && <PathRow label="实际路径" value={log.path} />}
                      {log.route && log.route !== log.path && (
                        <PathRow label="路由模板" value={log.route} />
                      )}
                    </div>
                  )}
                </Section>

                {/* 结果 */}
                {(log.errorCode || log.errorMessage || log.responseSnippet) && (
                  <Section
                    icon={log.status === "success" ? <CheckCircle2 className="size-3.5" /> : <AlertOctagon className="size-3.5" />}
                    title="执行结果"
                    tone={log.status === "success" ? undefined : "danger"}
                  >
                    {(log.errorCode || log.errorMessage) && (
                      <Grid>
                        {log.errorCode && <Field label="错误码" value={log.errorCode} mono copyable />}
                        {log.errorMessage && <Field label="错误信息" value={log.errorMessage} />}
                      </Grid>
                    )}
                    {log.responseSnippet && (
                      <FieldBlock label="响应摘要">
                        <WidgetBoundary title="JSON 渲染失败" description="响应内容可能超长或非法，切换到原文重试。">
                          <JsonViewer value={log.responseSnippet} height={220} />
                        </WidgetBoundary>
                      </FieldBlock>
                    )}
                  </Section>
                )}

                {/* 终端 & 地理 */}
                <Section icon={<MapPin className="size-3.5" />} title="终端与地理">
                  <Grid>
                    <Field label="IP" value={log.ip || "--"} mono copyable />
                    <Field
                      label="地理位置"
                      value={locationText(log) || "--"}
                      icon={locationText(log) ? <Globe className="size-3 text-muted-foreground" /> : undefined}
                    />
                    <Field label="ISP" value={log.isp || "--"} />
                  </Grid>
                  {log.userAgent && (
                    <FieldBlock label="User-Agent">
                      <div className="flex items-start gap-2">
                        <Monitor className="size-3.5 text-muted-foreground shrink-0 mt-0.5" />
                        <span className="font-mono text-[11px] break-all text-muted-foreground">{log.userAgent}</span>
                      </div>
                    </FieldBlock>
                  )}
                </Section>

                {/* 上下文 JSON */}
                {log.changes && Object.keys(log.changes).length > 0 && (
                  <Section icon={<Layers className="size-3.5" />} title="请求上下文">
                    <WidgetBoundary title="JSON 渲染失败" description="数据结构可能超深/含循环引用，点击重试即可。">
                      <JsonViewer value={JSON.stringify(log.changes, null, 2)} height={420} />
                    </WidgetBoundary>
                  </Section>
                )}
              </div>
            </ScrollArea>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

// ─── 详情区块基础组件 ────────────────────────────

function Section({
  icon,
  title,
  tone,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  tone?: "danger";
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className={cn(
        "flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-widest",
        tone === "danger" ? "text-red-600 dark:text-red-400" : "text-muted-foreground",
      )}>
        {icon}
        {title}
      </div>
      <Separator />
      <div className="space-y-3">{children}</div>
    </section>
  );
}

function Grid({ children }: { children: React.ReactNode }) {
  // 响应式：<md 单列（UUID 等长值独占整行），≥md 双列，≥xl 继续双列但带更宽的列间距
  return <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-3">{children}</div>;
}

function Field({
  label,
  value,
  mono = false,
  copyable = false,
  icon,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
  copyable?: boolean;
  icon?: React.ReactNode;
}) {
  const valueText = typeof value === "string" ? value : "";
  return (
    <div className="min-w-0 space-y-1">
      <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{label}</div>
      <div className="flex items-start gap-1.5 min-w-0">
        {icon ? <span className="shrink-0 mt-0.5">{icon}</span> : null}
        <div
          className={cn(
            "flex-1 min-w-0 break-all overflow-wrap-anywhere",
            mono ? "font-mono text-[11px] leading-5" : "text-xs leading-5",
          )}
          style={{ overflowWrap: "anywhere", wordBreak: "break-word" }}
        >
          {value}
        </div>
        {copyable && valueText && valueText !== "--" && (
          <button
            type="button"
            onClick={() => copyToClipboard(valueText)}
            className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
            aria-label={`复制 ${label}`}
          >
            <Copy className="size-3" />
          </button>
        )}
      </div>
    </div>
  );
}

function FieldBlock({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{label}</div>
      {children}
    </div>
  );
}

function PathRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{label}</span>
        <button
          type="button"
          onClick={() => copyToClipboard(value)}
          className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          aria-label={`复制 ${label}`}
        >
          <Copy className="size-3" />
          <span>复制</span>
        </button>
      </div>
      <code
        className="block font-mono text-[11px] leading-5 bg-muted/50 border rounded-md px-2.5 py-1.5"
        style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", overflowWrap: "anywhere" }}
      >
        {value}
      </code>
    </div>
  );
}

// CodeBlock 已迁移到共享的 <JsonViewer>（基于 monaco-editor）。
// 若未来其他页面需要同样的 JSON 只读视图，直接 import JsonViewer 即可。
