"use client";

import { useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import { toast } from "sonner";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  CircleDashed,
  Cpu,
  History,
  Inbox,
  Loader2,
  MinusCircle,
  Network,
  RefreshCw,
  RotateCw,
  Search,
  Server,
  Waypoints,
  XCircle,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import type { AuditEntry, BackfillStep, BackfillStepStatus, GraphBackfillState, RiskStatus } from "@/lib/api/risk";
import { formatClock, formatDate, formatDateTime, localDateKey, parseInstant } from "@/lib/datetime";
import { useRiskAudit, useRiskFeatures, useRiskGraphBackfill, useRiskModels, useRiskPatterns, useRiskStatus, useStartGraphBackfill } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { errorText, useSceneName } from "./decisions";
import { countText, when } from "./kit/humanize";

/**
 * 系统状态：风控平台各组件是否健康、决策服务加载了哪些版本、关系图数据整理到哪一步、谁改过什么。
 *
 * 主角是运行拓扑：请求从左到右依次经过在线决策、实时数据、特征计算、存储分析四层，每个组件同时标出
 * 管理服务与决策服务两边看到的连接状态（决策服务只汇报它直接依赖的那几个）。
 */

/* ── 时钟：渲染期不直接读 Date.now() ── */

function subscribeClock(cb: () => void) {
  const id = setInterval(cb, 5_000);
  return () => clearInterval(id);
}
const readClock = () => Math.floor(Date.now() / 5_000) * 5_000;
const readServerClock = () => 0;
const useNow = () => useSyncExternalStore(subscribeClock, readClock, readServerClock);

/* ════════════════════════ 拓扑定义 ════════════════════════ */

interface Node {
  key: string;
  name: string;
  role: string;
  /** 决策服务依赖表里的键 */
  dep?: string;
}

const LANES: Array<{ title: string; hint: string; nodes: Node[] }> = [
  { title: "在线决策", hint: "接收请求并给出结论", nodes: [{ key: "risk-decision", name: "决策服务", role: "在线判断每次请求" }] },
  {
    title: "实时数据",
    hint: "毫秒级读写",
    nodes: [
      { key: "redis", name: "Redis Cluster", role: "名单索引与滑窗计数", dep: "redis" },
      { key: "etcd", name: "etcd", role: "版本发布与服务发现", dep: "etcd" },
      { key: "nats", name: "NATS JetStream", role: "业务事件传递", dep: "nats" },
    ],
  },
  {
    title: "特征计算",
    hint: "长窗口与关联",
    nodes: [
      { key: "risingwave", name: "RisingWave", role: "流式特征聚合" },
      { key: "nebula", name: "NebulaGraph", role: "账号设备关系图", dep: "nebula" },
      { key: "milvus", name: "Milvus", role: "相似设备检索", dep: "milvus" },
    ],
  },
  {
    title: "存储与分析",
    hint: "配置、名单与流水",
    nodes: [
      { key: "postgres", name: "PostgreSQL", role: "配置与名单" },
      { key: "clickhouse", name: "ClickHouse", role: "决策流水与分析" },
    ],
  },
];
const KNOWN = new Set(LANES.flatMap((l) => l.nodes.map((n) => n.key)));

type Health = "ok" | "bad" | "unknown";
const healthOf = (raw: string | undefined): Health => (raw === undefined ? "unknown" : raw === "ok" ? "ok" : "bad");

/* ════════════════════════ 页面 ════════════════════════ */

export function RiskSystem() {
  const status = useRiskStatus();

  return (
    <TabStack>
      {status.isError && !status.data ? (
        <Panel icon={AlertTriangle} tone="rose" title="系统状态读取失败">
          <div className="flex flex-wrap items-center gap-3">
            <p className="min-w-0 flex-1 text-sm text-muted-foreground">{errorText(status.error)}</p>
            <Button size="sm" variant="outline" onClick={() => status.refetch()}>
              <RotateCw />
              重试
            </Button>
          </div>
        </Panel>
      ) : !status.data ? (
        <StatusSkeleton />
      ) : (
        <>
          <HealthBanner status={status.data} />
          <Topology status={status.data} />
          <LoadedVersions status={status.data} />
        </>
      )}
      <GraphDataPanel />
      <AuditPanel />
    </TabStack>
  );
}

/* ── 总体结论 ── */

function HealthBanner({ status }: { status: RiskStatus }) {
  const comps = status.components ?? {};
  const keys = [...new Set([...KNOWN, ...Object.keys(comps)])];
  const bad = keys.filter((k) => healthOf(comps[k]) === "bad");
  const unknown = keys.filter((k) => healthOf(comps[k]) === "unknown").length;
  const scenes = status.decision?.scenes ?? [];
  const sceneErrors = scenes.filter((s) => s.error).length;
  const spool = status.spoolPending ?? 0;
  const nameOf = (k: string) => LANES.flatMap((l) => l.nodes).find((n) => n.key === k)?.name ?? k;
  const ok = bad.length === 0 && sceneErrors === 0 && !!status.decision;
  const headline = !status.decision ? "决策服务没有响应" : bad.length ? `${bad.length} 个组件异常` : sceneErrors ? `${sceneErrors} 个场景版本加载失败` : "全部组件运行正常";
  const sub = !status.decision
    ? "请求将按各场景的出错策略处理"
    : bad.length
      ? `${bad.map(nameOf).join("、")}连接异常`
      : unknown
        ? `${unknown} 个组件未汇报状态`
        : `${keys.length} 个组件全部连通`;

  return (
    <div className="grid min-w-0 gap-4 @3xl:grid-cols-[minmax(0,1.6fr)_repeat(3,minmax(0,1fr))]">
      <div
        className={cn(
          "flex min-w-0 items-center gap-4 rounded-2xl border p-4 sm:px-5",
          ok ? "border-emerald-500/40 bg-emerald-500/5" : "border-red-500/40 bg-red-500/5"
        )}
      >
        <span className={cn("relative grid size-11 shrink-0 place-items-center rounded-xl", ok ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" : "bg-red-500/15 text-red-600 dark:text-red-400")}>
          {ok ? <CheckCircle2 className="size-5" /> : <AlertTriangle className="size-5" />}
          <span className={cn("absolute -top-0.5 -right-0.5 size-2.5 rounded-full ring-2 ring-card", ok ? "bg-emerald-500" : "animate-pulse bg-red-500")} />
        </span>
        <div className="min-w-0">
          <div className={cn("text-base font-semibold", ok ? "text-emerald-700 dark:text-emerald-300" : "text-red-700 dark:text-red-300")}>{headline}</div>
          <div className="text-xs leading-5 text-muted-foreground">{sub}</div>
        </div>
      </div>
      <Figure label="决策实例" value={status.decision?.instance || "—"} mono />
      <Figure label="已加载场景" value={String(scenes.length)} tone={sceneErrors ? "danger" : undefined} sub={sceneErrors ? `${sceneErrors} 个加载失败` : "版本均已就绪"} />
      <Figure label="待补发事件" value={spool.toLocaleString("zh-CN")} tone={spool > 0 ? "warn" : undefined} sub={spool > 0 ? "事件总线恢复后自动补发" : "没有积压"} />
    </div>
  );
}

function Figure({ label, value, sub, tone, mono }: { label: string; value: string; sub?: string; tone?: "warn" | "danger"; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col justify-center gap-1 rounded-2xl border border-border bg-card p-4">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={cn(
          "truncate text-xl leading-7 font-semibold tabular-nums",
          mono && "font-mono text-sm leading-7",
          tone === "warn" && "text-amber-600 dark:text-amber-400",
          tone === "danger" && "text-red-600 dark:text-red-400"
        )}
        title={value}
      >
        {value}
      </span>
      {sub ? <span className="truncate text-[11px] text-muted-foreground">{sub}</span> : null}
    </div>
  );
}

/* ── 运行拓扑 ── */

function Topology({ status }: { status: RiskStatus }) {
  const comps = status.components ?? {};
  const deps = status.decision?.dependencies ?? {};
  const extra = Object.keys(comps).filter((k) => !KNOWN.has(k));
  const lanes: typeof LANES = extra.length ? [...LANES, { title: "其他", hint: "", nodes: extra.map((k): Node => ({ key: k, name: k, role: "" })) }] : LANES;

  return (
    <Panel
      icon={Network}
      tone="sky"
      title="运行拓扑"
      aside={
        <div className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
          <Legend tone="ok" label="正常" />
          <Legend tone="bad" label="异常" />
          <Legend tone="unknown" label="未汇报" />
        </div>
      }
    >
      <div className="flex min-w-0 flex-col gap-3 @4xl:flex-row @4xl:items-stretch">
        {lanes.map((lane, li) => (
          <div key={lane.title} className="flex min-w-0 flex-1 flex-col gap-3 @4xl:flex-row @4xl:items-stretch">
            {li > 0 ? (
              <div aria-hidden className="flex shrink-0 items-center justify-center text-muted-foreground/60">
                <ChevronDown className="size-4 @4xl:hidden" />
                <ChevronRight className="hidden size-4 @4xl:block" />
              </div>
            ) : null}
            <section className="flex min-w-0 flex-1 flex-col gap-2 rounded-xl bg-muted/40 p-2.5">
              <header className="px-1">
                <h4 className="text-xs font-semibold">{lane.title}</h4>
                {lane.hint ? <p className="text-[11px] text-muted-foreground">{lane.hint}</p> : null}
              </header>
              {lane.nodes.map((n) => (
                <NodeCard key={n.key} node={n} admin={comps[n.key]} decision={n.dep ? deps[n.dep] : undefined} decisionAlive={!!status.decision} />
              ))}
            </section>
          </div>
        ))}
      </div>
    </Panel>
  );
}

function NodeCard({ node, admin, decision, decisionAlive }: { node: Node; admin: string | undefined; decision: string | undefined; decisionAlive: boolean }) {
  const a = healthOf(admin);
  const d = node.dep && decisionAlive ? healthOf(decision) : null;
  const worst: Health = a === "bad" || d === "bad" ? "bad" : a === "unknown" && (d === null || d === "unknown") ? "unknown" : "ok";
  const error = (a === "bad" ? admin : undefined) ?? (d === "bad" ? decision : undefined);
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col gap-2 rounded-lg border bg-card px-3 py-2.5",
        worst === "bad" && "border-red-500/50 bg-red-500/5",
        worst === "unknown" && "border-dashed"
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <Dot tone={worst} pulse={worst === "bad"} />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{node.name}</span>
      </div>
      {node.role ? <span className="truncate text-[11px] text-muted-foreground">{node.role}</span> : null}
      <div className="flex min-w-0 flex-wrap gap-1">
        <Probe label="管理端" tone={a} />
        {d ? <Probe label="决策端" tone={d} /> : null}
      </div>
      {error ? (
        <p className="line-clamp-2 font-mono text-[11px] break-all text-red-600 dark:text-red-400" title={error}>
          {error}
        </p>
      ) : null}
    </div>
  );
}

function Dot({ tone, pulse }: { tone: Health; pulse?: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "size-2 shrink-0 rounded-full",
        tone === "ok" && "bg-emerald-500",
        tone === "bad" && "bg-red-500",
        tone === "unknown" && "bg-muted-foreground/50",
        pulse && "animate-pulse"
      )}
    />
  );
}

function Probe({ label, tone }: { label: string; tone: Health }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] leading-4",
        tone === "ok" && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
        tone === "bad" && "bg-red-500/10 text-red-700 dark:text-red-300",
        tone === "unknown" && "bg-muted text-muted-foreground"
      )}
    >
      <Dot tone={tone} />
      {label}
      {tone === "ok" ? "正常" : tone === "bad" ? "异常" : "未汇报"}
    </span>
  );
}

function Legend({ tone, label }: { tone: Health; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <Dot tone={tone} />
      {label}
    </span>
  );
}

/* ── 已加载版本 ── */

function LoadedVersions({ status }: { status: RiskStatus }) {
  const sceneName = useSceneName();
  const d = status.decision;
  const scenes = [...(d?.scenes ?? [])].sort((a, b) => Number(!!b.error) - Number(!!a.error) || sceneName(a.scene).localeCompare(sceneName(b.scene), "zh-CN"));

  return (
    <Panel icon={Cpu} tone="primary" title="决策服务已加载的版本" bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0">
      {!d ? (
        <div className="border-t">
          <CardEmpty icon={Cpu} title="决策服务没有响应" description="恢复后这里会列出每个场景正在运行的版本" />
        </div>
      ) : scenes.length === 0 ? (
        <div className="border-t">
          <CardEmpty icon={Cpu} title="还没有加载任何场景" description="场景发布后会自动加载" />
        </div>
      ) : (
        <ul className="grid min-w-0 border-t @3xl:grid-cols-2">
          {scenes.map((s) => {
            const loaded = when(s.loaded_at ? new Date(s.loaded_at).toISOString() : null);
            const sentence = s.error
              ? "版本加载失败，请到场景工作台重新发布"
              : s.candidate
                ? s.shadow
                  ? `正在运行 ${s.version || "正式版本"}，新版本 ${s.candidate} 在旁观察`
                  : `正在运行 ${s.version || "正式版本"}，新版本 ${s.candidate} 灰度 ${s.canary_percent ?? 0}%`
                : `正在运行 ${s.version || "正式版本"}，处理全部请求`;
            return (
              <li key={s.scene} className="flex min-w-0 items-start gap-3 border-b px-4 py-3 sm:px-5 @3xl:odd:border-r">
                <Dot tone={s.error ? "bad" : "ok"} pulse={!!s.error} />
                <div className="-mt-1 flex min-w-0 flex-1 flex-col gap-0.5">
                  <div className="flex min-w-0 items-baseline gap-2">
                    <span className="truncate text-sm font-medium">{sceneName(s.scene)}</span>
                    {s.candidate ? (
                      <Badge variant={s.shadow ? "info" : "warning"} size="sm">
                        {s.shadow ? "影子观察" : `灰度 ${s.canary_percent ?? 0}%`}
                      </Badge>
                    ) : null}
                  </div>
                  <span className={cn("text-xs leading-5", s.error ? "text-red-600 dark:text-red-400" : "text-foreground/80")}>{sentence}</span>
                  {s.error ? (
                    <span className="line-clamp-2 font-mono text-[11px] break-all text-red-600/80 dark:text-red-400/80" title={s.error}>
                      {s.error}
                    </span>
                  ) : null}
                  <span className="text-[11px] text-muted-foreground" title={loaded.absolute}>
                    {loaded.relative ? `${loaded.relative}加载` : "加载时间未知"}
                    {s.checksum ? <span className="ml-2 font-mono">{s.checksum.slice(0, 10)}</span> : null}
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}

function StatusSkeleton() {
  return (
    <>
      <div className="grid gap-4 @3xl:grid-cols-[minmax(0,1.6fr)_repeat(3,minmax(0,1fr))]">
        <Skeleton className="h-[84px] rounded-2xl" />
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-[84px] rounded-2xl" />
        ))}
      </div>
      <div className="flex flex-col gap-4 rounded-2xl border bg-card p-4 sm:p-5">
        <Skeleton className="h-5 w-24" />
        <div className="grid gap-3 @4xl:grid-cols-4">
          {[1, 3, 3, 2].map((n, i) => (
            <div key={i} className="flex flex-col gap-2 rounded-xl bg-muted/40 p-2.5">
              <Skeleton className="h-4 w-16" />
              {Array.from({ length: n }).map((_, j) => (
                <Skeleton key={j} className="h-[76px] rounded-lg" />
              ))}
            </div>
          ))}
        </div>
      </div>
    </>
  );
}

/* ════════════════════════ 关系图数据 ════════════════════════ */

const STEP_META: Record<BackfillStepStatus, { label: string; bar: string; text: string; icon: typeof CheckCircle2 }> = {
  done: { label: "完成", bar: "bg-emerald-500", text: "text-emerald-600 dark:text-emerald-400", icon: CheckCircle2 },
  running: { label: "进行中", bar: "bg-sky-500 animate-pulse", text: "text-sky-600 dark:text-sky-400", icon: Loader2 },
  pending: { label: "等待", bar: "bg-muted-foreground/20", text: "text-muted-foreground", icon: CircleDashed },
  skipped: { label: "跳过", bar: "bg-muted-foreground/40", text: "text-muted-foreground", icon: MinusCircle },
  failed: { label: "失败", bar: "bg-red-500", text: "text-red-600 dark:text-red-400", icon: XCircle },
};

function durationText(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? `${m} 分 ${s % 60} 秒` : `${m} 分钟`;
  return `${Math.floor(m / 60)} 小时 ${m % 60} 分`;
}

/** 来源列表与本次进度合并：没开始的来源补成「等待」 */
function mergeSteps(state: GraphBackfillState): BackfillStep[] {
  const steps = state.run?.progress?.steps ?? [];
  const byKey = new Map(steps.map((s) => [s.key, s]));
  const fromSources = (state.sources ?? []).map(
    (src) => byKey.get(src.key) ?? { key: src.key, label: src.label, detail: src.detail, status: "pending" as const, rows: 0, seconds: 0 }
  );
  const rest = steps.filter((s) => !(state.sources ?? []).some((src) => src.key === s.key));
  return [...fromSources, ...rest];
}

function GraphDataPanel() {
  const q = useRiskGraphBackfill();
  const start = useStartGraphBackfill();
  const now = useNow();
  const [confirm, setConfirm] = useState(false);
  const state = q.data;
  const run = state?.run ?? null;
  const p = run?.progress ?? null;
  const running = !!state?.running;
  const steps = state ? mergeSteps(state) : [];
  const finished = steps.filter((s) => s.status === "done" || s.status === "skipped" || s.status === "failed").length;
  const started = parseInstant(run?.startedAt ?? p?.startedAt)?.getTime();
  const ended = parseInstant(run?.finishedAt ?? p?.finishedAt)?.getTime();
  const status = running ? "running" : run?.status;

  const begin = () =>
    start.mutate(undefined, {
      onSuccess: (r) => toast.success(r.started ? "已开始重新整理关系图数据" : "已有一次整理正在进行"),
      onError: (e) => toast.error(errorText(e, "无法开始整理")),
    });

  let summary: ReactNode;
  if (!run) summary = "还没有整理过关系图数据。";
  else if (status === "running")
    summary = (
      <>
        正在整理，已进行 {started && now ? durationText(now - started) || "不到 1 秒" : "片刻"}
        {run.operator ? `，由 ${run.operator} 发起` : ""}。
      </>
    );
  else if (status === "failed")
    summary = (
      <>
        {started ? `${formatDateTime(started)} 开始的整理失败` : "上次整理失败"}
        {p?.error ? `：${p.error}` : "。"}
      </>
    );
  else
    summary = (
      <>
        {ended ? `${formatDate(ended)} ${formatClock(ended)} 完成` : "已完成"}
        {started && ended ? `，用时 ${durationText(ended - started)}` : ""}
        {run.operator ? `，由 ${run.operator} 发起` : ""}。
      </>
    );

  const totals: Array<{ label: string; value: number; sub?: string }> = p
    ? [
        { label: "用户", value: p.users },
        { label: "IP", value: p.ips, sub: p.hostingIps || p.sharedIps ? `机房出口 ${p.hostingIps}，共享出口 ${p.sharedIps}` : undefined },
        { label: "设备", value: p.devices, sub: p.vectors ? `设备码向量 ${p.vectors}` : undefined },
        { label: "IPv6 线路", value: p.v6Lines, sub: p.v6Addrs ? `含 ${p.v6Addrs.toLocaleString("zh-CN")} 个地址` : undefined },
        { label: "删除的代理地址", value: p.proxyIps },
        { label: "删除的假设备码", value: p.badDevices ?? 0 },
      ]
    : [];

  return (
    <Panel
      icon={Waypoints}
      tone="violet"
      title="关系图数据"
      aside={
        <Button size="sm" variant="outline" disabled={running || start.isPending || !state} onClick={() => setConfirm(true)}>
          {running || start.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          {running ? "整理中" : "重新整理"}
        </Button>
      }
    >
      {q.isError && !q.data ? (
        <div className="flex flex-wrap items-center gap-3">
          <p className="min-w-0 flex-1 text-sm text-muted-foreground">{errorText(q.error, "关系图数据状态读取失败")}</p>
          <Button size="sm" variant="outline" onClick={() => q.refetch()}>
            <RotateCw />
            重试
          </Button>
        </div>
      ) : q.isLoading || !state ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-4 w-72 max-w-full" />
          <div className="grid grid-cols-2 gap-3 @2xl:grid-cols-5">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-[68px] rounded-xl" />
            ))}
          </div>
          <Skeleton className="h-2.5 rounded-full" />
          <div className="grid gap-2 @2xl:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        </div>
      ) : (
        <div className="flex min-w-0 flex-col gap-5">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {status ? (
              <Badge variant={status === "running" ? "info" : status === "failed" ? "danger" : "success"} size="sm">
                {status === "running" ? "整理中" : status === "failed" ? "失败" : "已完成"}
              </Badge>
            ) : null}
            <p className={cn("min-w-0 flex-1 text-sm", status === "failed" ? "text-red-600 dark:text-red-400" : "text-foreground/85")}>{summary}</p>
          </div>

          {totals.length ? (
            <div className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-5">
              {totals.map((t) => (
                <div key={t.label} className="flex min-w-0 flex-col gap-1 rounded-xl border bg-card px-3 py-2.5">
                  <span className="text-[11px] text-muted-foreground">{t.label}</span>
                  <span className="text-lg leading-6 font-semibold tracking-tight tabular-nums">
                    <NumberFlow value={t.value || 0} locales="zh-CN" />
                  </span>
                  {t.sub ? <span className="truncate text-[11px] text-muted-foreground">{t.sub}</span> : null}
                </div>
              ))}
            </div>
          ) : null}

          {steps.length ? (
            <div className="flex min-w-0 flex-col gap-3">
              <div className="flex min-w-0 items-center gap-3">
                <div className="flex h-2.5 min-w-0 flex-1 gap-0.5 overflow-hidden rounded-full">
                  {steps.map((s) => (
                    <span key={s.key} title={`${s.label}：${STEP_META[s.status]?.label ?? s.status}`} className={cn("h-full flex-1 transition-colors", STEP_META[s.status]?.bar ?? "bg-muted")} />
                  ))}
                </div>
                <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                  {finished} / {steps.length} 个来源
                </span>
              </div>
              <ol className="grid min-w-0 gap-2 @2xl:grid-cols-2 @5xl:grid-cols-3">
                {steps.map((s, i) => {
                  const m = STEP_META[s.status] ?? STEP_META.pending;
                  const Icon = m.icon;
                  return (
                    <li
                      key={s.key}
                      className={cn(
                        "flex min-w-0 items-start gap-2.5 rounded-lg border px-3 py-2",
                        s.status === "running" && "border-sky-500/50 bg-sky-500/5",
                        s.status === "failed" && "border-red-500/50 bg-red-500/5",
                        s.status === "pending" && "border-dashed"
                      )}
                    >
                      <span className="mt-0.5 w-4 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{i + 1}</span>
                      <div className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-sm">{s.label}</span>
                        {s.detail ? <span className="line-clamp-1 text-[11px] text-muted-foreground">{s.detail}</span> : null}
                        {s.error ? <span className="line-clamp-2 text-[11px] break-all text-red-600 dark:text-red-400">{s.error}</span> : null}
                      </div>
                      <span className={cn("flex shrink-0 flex-col items-end gap-0.5 text-[11px]", m.text)}>
                        <span className="inline-flex items-center gap-1">
                          <Icon className={cn("size-3.5", s.status === "running" && "animate-spin")} />
                          {m.label}
                        </span>
                        {s.rows ? <span className="text-muted-foreground tabular-nums">{countText(s.rows, "条")}</span> : null}
                      </span>
                    </li>
                  );
                })}
              </ol>
            </div>
          ) : null}
        </div>
      )}

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重新整理关系图数据</AlertDialogTitle>
            <AlertDialogDescription>
              将从业务数据重新汇总用户、设备、IP 之间的关系，通常需要几分钟。期间关系图照常可用，已有数据不会被清空。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={begin}>开始整理</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Panel>
  );
}

/* ════════════════════════ 操作记录 ════════════════════════ */

const AUDIT_LABEL: Record<string, string> = {
  "case.review": "完成复核", "cep.delete": "删除行为模式", "cep.save": "保存行为模式", dispose: "手工处置",
  "feature.delete": "删除特征", "feature.save": "保存特征", "list.add": "添加名单", "list.rebuild": "重建名单索引",
  "list.remove": "移除名单", "model.delete": "删除模型", "model.upload": "上传模型", "model.update": "更新模型",
  "rule.delete": "删除规则", "rule.save": "保存规则", "scene.delete": "删除场景", "scene.publish": "发布场景",
  "scene.save": "保存场景", "scene.promote": "全量发布场景", "scene.rollback": "回滚场景", "scene.withdraw": "撤回候选版本",
  "scene.traffic": "调整场景流量", "stream.sync": "同步流式特征", "template.apply": "安装规则模板", "graph.backfill": "重新整理关系图数据",
};

const AUDIT_TONE = (action: string): "danger" | "warning" | "info" | "muted" => {
  if (action.endsWith(".delete") || action === "list.remove") return "danger";
  if (action === "dispose" || action === "case.review" || action.startsWith("scene.publish") || action === "scene.rollback" || action === "scene.promote") return "warning";
  if (action.endsWith(".save") || action === "list.add" || action === "model.upload") return "info";
  return "muted";
};

const TONE_DOT: Record<ReturnType<typeof AUDIT_TONE>, string> = {
  danger: "bg-red-500",
  warning: "bg-amber-500",
  info: "bg-sky-500",
  muted: "bg-muted-foreground/50",
};

/** 审计详情里常见字段的中文名 */
const DETAIL_LABEL: Record<string, string> = {
  listType: "名单类型", kind: "种类", values: "取值", reason: "原因", durationSeconds: "时长（秒）", appId: "应用", scenes: "场景",
  note: "备注", mode: "方式", canaryPercent: "灰度比例", releaseId: "版本编号", id: "编号", verdict: "结论", targets: "处置对象",
  template: "模板", scene: "场景", observe: "观察模式", name: "名称", key: "键", enabled: "启用", ids: "编号", version: "版本",
  shadow: "影子", drop: "撤下候选", removed: "移除条数", entries: "条数", inputs: "输入", format: "格式",
};

function AuditPanel() {
  const audit = useRiskAudit(200);
  const now = useNow();
  const sceneName = useSceneName();
  const features = useRiskFeatures();
  const patterns = useRiskPatterns();
  const models = useRiskModels();
  const [keyword, setKeyword] = useState("");
  const [open, setOpen] = useState<Set<number>>(new Set());

  const targetName = useMemo(() => {
    const f = new Map((features.data ?? []).map((x) => [x.key, x.name]));
    const p = new Map((patterns.data ?? []).map((x) => [x.key, x.name]));
    const m = new Map((models.data ?? []).map((x) => [x.key, x.name]));
    return (e: AuditEntry): string => {
      const t = e.target ?? "";
      if (!t) return "";
      if (e.action.startsWith("scene.") || e.action === "template.apply") return sceneName(t);
      if (e.action.startsWith("feature.")) return f.get(t) ?? t;
      if (e.action.startsWith("cep.")) return p.get(t) ?? t;
      if (e.action.startsWith("model.")) return m.get(t) || t;
      return t;
    };
  }, [features.data, patterns.data, models.data, sceneName]);

  const rows = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    const list = audit.data ?? [];
    if (!k) return list;
    return list.filter((e) => [e.operator, e.action, AUDIT_LABEL[e.action] ?? "", e.target, targetName(e)].some((v) => (v ?? "").toLowerCase().includes(k)));
  }, [audit.data, keyword, targetName]);

  const groups = useMemo(() => {
    const out: Array<{ day: string; items: AuditEntry[] }> = [];
    for (const e of rows) {
      const day = localDateKey(e.at);
      const last = out[out.length - 1];
      if (last && last.day === day) last.items.push(e);
      else out.push({ day, items: [e] });
    }
    return out;
  }, [rows]);
  const today = now ? localDateKey(now) : "";
  const yesterday = now ? localDateKey(now - 86_400_000) : "";

  const toggle = (id: number) =>
    setOpen((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <Panel
      icon={History}
      tone="amber"
      title="操作记录"
      aside={
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 w-52 pl-8" placeholder="操作人、动作或对象" value={keyword} onChange={(e) => setKeyword(e.target.value)} />
        </div>
      }
      bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
    >
      {audit.isError && !audit.data ? (
        <div className="flex flex-wrap items-center gap-3 border-t px-4 py-6 sm:px-5">
          <p className="min-w-0 flex-1 text-sm text-muted-foreground">{errorText(audit.error, "操作记录读取失败")}</p>
          <Button size="sm" variant="outline" onClick={() => audit.refetch()}>
            <RotateCw />
            重试
          </Button>
        </div>
      ) : audit.isLoading ? (
        <div className="flex flex-col gap-3 border-t p-4 sm:px-5">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="flex items-center gap-3">
              <Skeleton className="h-3 w-10" />
              <Skeleton className="size-2 rounded-full" />
              <Skeleton className="h-4 flex-1" />
            </div>
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="border-t">
          <CardEmpty icon={Inbox} title={keyword.trim() ? "没有匹配的操作记录" : "还没有操作记录"} />
        </div>
      ) : (
        <div className="max-h-[640px] overflow-y-auto border-t">
          {groups.map((g) => (
            <section key={g.day}>
              <h5 className="sticky top-0 z-10 border-b bg-card/95 px-4 py-1.5 text-[11px] font-medium text-muted-foreground backdrop-blur sm:px-5">
                {g.day === today ? "今天" : g.day === yesterday ? "昨天" : formatDate(g.items[0]!.at)}
              </h5>
              <ol className="relative flex flex-col py-1">
                {g.items.map((e) => (
                  <AuditRow key={e.id} entry={e} target={targetName(e)} open={open.has(e.id)} onToggle={() => toggle(e.id)} />
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
    </Panel>
  );
}

function AuditRow({ entry, target, open, onToggle }: { entry: AuditEntry; target: string; open: boolean; onToggle: () => void }) {
  const detail = entry.detail;
  const hasDetail = detail !== null && detail !== undefined && !(typeof detail === "object" && Object.keys(detail as object).length === 0);
  const tone = AUDIT_TONE(entry.action);
  const label = AUDIT_LABEL[entry.action] ?? entry.action;
  const body = (
    <>
      <span className="w-11 shrink-0 pt-0.5 text-right text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(entry.at)}>
        {formatClock(entry.at)}
      </span>
      <span className="relative flex w-3 shrink-0 justify-center pt-1.5">
        <span aria-hidden className={cn("size-2 rounded-full ring-4 ring-card", TONE_DOT[tone])} />
      </span>
      <span className="min-w-0 flex-1 text-sm leading-5">
        <span className="font-medium">{entry.operator || "系统"}</span>
        <span className="text-foreground/80"> {label}</span>
        {target ? (
          <span className="text-foreground/80">
            「<span className={cn(target === entry.target && "font-mono text-xs")}>{target}</span>」
          </span>
        ) : null}
      </span>
      {hasDetail ? <ChevronDown className={cn("mt-0.5 size-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} /> : null}
    </>
  );
  return (
    <li className="px-4 sm:px-5">
      {hasDetail ? (
        <button type="button" aria-expanded={open} onClick={onToggle} className="flex w-full min-w-0 items-start gap-3 rounded-md py-1.5 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none">
          {body}
        </button>
      ) : (
        <div className="flex min-w-0 items-start gap-3 py-1.5">{body}</div>
      )}
      {open && hasDetail ? <AuditDetail detail={detail} /> : null}
    </li>
  );
}

function AuditDetail({ detail }: { detail: unknown }) {
  const isObject = typeof detail === "object" && detail !== null && !Array.isArray(detail);
  const entries = isObject ? Object.entries(detail as Record<string, unknown>).filter(([, v]) => v !== null && v !== undefined && v !== "") : [];
  return (
    <div className="mb-2 ml-20 rounded-lg border bg-muted/30 px-3 py-2">
      {isObject ? (
        <dl className="grid min-w-0 gap-x-4 gap-y-1.5 @2xl:grid-cols-[minmax(6rem,auto)_minmax(0,1fr)]">
          {entries.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-[11px] text-muted-foreground">{DETAIL_LABEL[k] ?? k}</dt>
              <dd className="min-w-0 text-xs break-all">{detailValue(v)}</dd>
            </div>
          ))}
        </dl>
      ) : (
        <code className="font-mono text-[11px] break-all">{detailValue(detail)}</code>
      )}
    </div>
  );
}

function detailValue(v: unknown): ReactNode {
  if (typeof v === "boolean") return v ? "是" : "否";
  if (typeof v === "number" || typeof v === "string") return String(v);
  if (Array.isArray(v) && v.every((x) => typeof x !== "object" || x === null)) return v.length ? v.map(String).join("、") : "无";
  return <code className="font-mono text-[11px] whitespace-pre-wrap">{JSON.stringify(v, null, 2)}</code>;
}
