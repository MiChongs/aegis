"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  Activity,
  ArrowDown,
  ArrowUp,
  Boxes,
  Cable,
  CloudDownload,
  Cpu,
  Download,
  FileClock,
  Globe2,
  Loader2,
  Network,
  Play,
  RefreshCw,
  RotateCcw,
  ScrollText,
  Square,
  Stethoscope,
  Waypoints
} from "lucide-react";
import { toast } from "sonner";
import { EgressEditor, type EgressEditorSeed } from "@/components/configuration/egress/egress-editor";
import { ACTION_LABEL, CORE_STATUS_LABEL, PROTOCOL_LABEL, delayTone, formatBytes, formatSpeed } from "@/components/configuration/egress/egress-meta";
import {
  ConnectionsPanel,
  DiagnosticsPanel,
  GroupsPanel,
  LogsPanel,
  ProvidersPanel,
  VersionsPanel
} from "@/components/configuration/egress/egress-runtime-panels";
import { ToneIcon, TONE_TEXT } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { EgressGraphView, MihomoCoreStatusView } from "@/lib/api/egress";
import { ApiError } from "@/lib/api/client";
import { useAuthStore } from "@/lib/auth-store";
import { formatClockSeconds, formatDateTime, formatRelative } from "@/lib/datetime";
import {
  useEgressGraphQuery,
  useEgressProbeMutation,
  useInstallMihomoMutation,
  useMihomoStatusQuery,
  useRestartMihomoMutation,
  useStopMihomoMutation
} from "@/lib/egress-hooks";
import { cn } from "@/lib/utils";

/**
 * 出海代理（平台配置中心的一个页签）。
 *
 * 上半部分是网关与 mihomo 核心的运行态（实时速率曲线、内存、判定计数），入口是全屏画布编辑器；
 * 下半部分按页签看线路健康、策略组（测速与切换）、订阅与规则集、在途连接、核心日志、诊断与版本回滚。
 * 全部接口限超级管理员。
 */

export function EgressGatewayPanel() {
  const isSuperAdmin = useAuthStore((state) => Boolean(state.operator?.isSuperAdmin));
  const query = useEgressGraphQuery(isSuperAdmin);
  const [editorOpen, setEditorOpen] = useState(false);
  const [seed, setSeed] = useState<EgressEditorSeed | null>(null);
  const [tab, setTab] = useState("lines");

  if (!isSuperAdmin) return <EmptyState title="仅超级管理员可配置出海网关" description="路由表决定平台所有对外调用走哪条线，影响面等同于防火墙。" />;
  if (query.isLoading) return <LoadingState title="加载出海网关" />;
  if (query.error || !query.data) return <EmptyState title="出海网关不可用" description={query.error instanceof Error ? query.error.message : undefined} />;

  const view = query.data;
  const coreUsed = view.compile.summary.mihomoEnabled || view.core.status !== "disabled";
  const openEditor = (next?: EgressEditorSeed | null) => {
    setSeed(next ?? null);
    setEditorOpen(true);
  };

  return (
    <div className="space-y-5">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
        <GatewayCard view={view} onOpen={() => openEditor()} />
        <CoreCard view={view} />
      </div>

      <Tabs value={tab} onValueChange={setTab} className="gap-4">
        <TabsList className="flex h-auto w-full flex-wrap justify-start gap-1 sm:w-auto">
          <TabsTrigger value="lines" className="text-xs">
            <Waypoints /> 线路与规则
          </TabsTrigger>
          <TabsTrigger value="groups" className="text-xs" disabled={!coreUsed}>
            <Boxes /> 策略组
          </TabsTrigger>
          <TabsTrigger value="providers" className="text-xs" disabled={!coreUsed}>
            <CloudDownload /> 订阅与规则集
          </TabsTrigger>
          <TabsTrigger value="connections" className="text-xs" disabled={!coreUsed}>
            <Cable /> 连接
          </TabsTrigger>
          <TabsTrigger value="logs" className="text-xs" disabled={!coreUsed}>
            <ScrollText /> 核心日志
          </TabsTrigger>
          <TabsTrigger value="diagnostics" className="text-xs">
            <Stethoscope /> 诊断
          </TabsTrigger>
          <TabsTrigger value="versions" className="text-xs">
            <FileClock /> 版本
          </TabsTrigger>
        </TabsList>
        <TabsContent value="lines">
          <LinesPanel view={view} />
        </TabsContent>
        <TabsContent value="groups">{tab === "groups" ? <GroupsPanel view={view} /> : null}</TabsContent>
        <TabsContent value="providers">{tab === "providers" ? <ProvidersPanel /> : null}</TabsContent>
        <TabsContent value="connections">{tab === "connections" ? <ConnectionsPanel /> : null}</TabsContent>
        <TabsContent value="logs">{tab === "logs" ? <LogsPanel /> : null}</TabsContent>
        <TabsContent value="diagnostics">
          <DiagnosticsPanel view={view} />
        </TabsContent>
        <TabsContent value="versions">{tab === "versions" ? <VersionsPanel view={view} onOpen={(next) => openEditor(next)} /> : null}</TabsContent>
      </Tabs>

      <EgressEditor
        open={editorOpen}
        seed={seed}
        onOpenChange={(open) => {
          setEditorOpen(open);
          if (!open) setSeed(null);
        }}
      />
    </div>
  );
}

function GatewayCard({ view, onOpen }: { view: EgressGraphView; onOpen: () => void }) {
  const runtime = view.runtime;
  const summary = view.compile.summary;
  const probe = useEgressProbeMutation();
  const errors = view.compile.issues.filter((issue) => issue.level === "error").length;
  const source =
    runtime.source === "graph" ? `画布第 ${view.appliedVersion} 版` : runtime.source === "database" ? "旧版控制台配置" : runtime.source === "disabled" ? "未配置" : "环境变量";
  const healthy = runtime.endpoints.filter((item) => item.enabled && item.healthy).length;
  const enabledEndpoints = runtime.endpoints.filter((item) => item.enabled).length;

  return (
    <section className="flex flex-col rounded-2xl border bg-card p-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={Globe2} tone={runtime.enabled ? "primary" : "neutral"} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">出海网关</h3>
            <Badge variant={runtime.enabled ? "success" : "secondary"} size="sm">
              {runtime.enabled ? "已启用" : "未启用"}
            </Badge>
            {view.dirty ? (
              <Badge variant="warning" size="sm">
                草稿未应用
              </Badge>
            ) : null}
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">
            当前配置来自{source}
            {view.appliedAt && runtime.source === "graph" ? `，${formatRelative(view.appliedAt)}应用` : ""}；未命中规则时{ACTION_LABEL[runtime.defaultAction ?? "direct"]}
          </p>
        </div>
      </div>

      <div className="mt-4 grid grid-cols-4 gap-2">
        {[
          { label: "判定", value: runtime.routed },
          { label: "走代理", value: runtime.proxied, tone: "text-emerald-600 dark:text-emerald-400" },
          { label: "直连", value: runtime.direct, tone: "text-sky-600 dark:text-sky-400" },
          { label: "拒绝", value: runtime.rejected, tone: "text-red-600 dark:text-red-400" }
        ].map((item) => (
          <div key={item.label} className="rounded-xl bg-muted/60 px-2 py-2.5 text-center">
            <div className="text-[11px] text-muted-foreground">{item.label}</div>
            <div className={cn("text-lg font-semibold tabular-nums", item.tone)}>{item.value.toLocaleString()}</div>
          </div>
        ))}
      </div>

      <dl className="mt-4 grid grid-cols-3 gap-2 text-xs">
        <div>
          <dt className="text-muted-foreground">规则</dt>
          <dd className="font-medium tabular-nums">{summary.activeRules} 条</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">线路健康</dt>
          <dd className={cn("font-medium tabular-nums", healthy < enabledEndpoints && "text-amber-600 dark:text-amber-400")}>
            {healthy} / {enabledEndpoints}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">UDP 判定 / 选不出线路</dt>
          <dd className="font-medium tabular-nums">
            {(runtime.routedUdp ?? 0).toLocaleString()} / <span className={cn(runtime.failed > 0 && "text-red-600 dark:text-red-400")}>{runtime.failed.toLocaleString()} 次</span>
          </dd>
        </div>
      </dl>

      <div className="mt-auto flex flex-wrap gap-2 pt-5">
        <Button onClick={onOpen} className="gap-1.5">
          <Network />
          打开画布编辑
        </Button>
        <Button
          variant="outline"
          className="gap-1.5"
          disabled={probe.isPending || !runtime.endpoints.length}
          onClick={() =>
            probe.mutate(undefined, {
              onSuccess: (data) => {
                const udp = data.results.filter((item) => item.udpChecked);
                toast.success(
                  `探测完成：TCP ${data.results.filter((item) => item.ok).length} / ${data.results.length} 条可用` +
                    (udp.length ? `，UDP ${udp.filter((item) => item.udpOk).length} / ${udp.length} 条可用` : "")
                );
              },
              onError: (error) => toast.error(error instanceof ApiError ? error.message : "探测失败")
            })
          }
        >
          {probe.isPending ? <Loader2 className="animate-spin" /> : <Activity />}
          立即探测线路
        </Button>
        {errors ? <span className="self-center text-xs text-red-600 dark:text-red-400">草稿里有 {errors} 个错误</span> : null}
      </div>
    </section>
  );
}

const chartConfig = {
  down: { label: "下行", color: "#10b981" },
  up: { label: "上行", color: "#0ea5e9" }
};

function CoreCard({ view }: { view: EgressGraphView }) {
  const enabled = view.compile.summary.mihomoEnabled || view.core.status !== "disabled";
  const status = useMihomoStatusQuery(enabled);
  const install = useInstallMihomoMutation();
  const restart = useRestartMihomoMutation();
  const stop = useStopMihomoMutation();
  const core: MihomoCoreStatusView = status.data ?? { ...view.core, traffic: [] };
  const label = CORE_STATUS_LABEL[core.status] ?? CORE_STATUS_LABEL.disabled;
  const rows = useMemo(
    () => (core.traffic ?? []).map((sample) => ({ time: formatClockSeconds(sample.time), up: sample.up, down: sample.down })),
    [core.traffic]
  );
  const busy = install.isPending || restart.isPending || stop.isPending;
  const run = (action: "install" | "restart" | "stop") => {
    const done = (message: string) => () => toast.success(message);
    const fail = (error: unknown) => toast.error(error instanceof ApiError ? error.message : "操作失败", { duration: 8000 });
    if (action === "install") install.mutate(undefined, { onSuccess: (data) => toast.success(`已安装 mihomo ${data.version}`), onError: fail });
    if (action === "restart") restart.mutate(undefined, { onSuccess: done("核心已重启"), onError: fail });
    if (action === "stop") stop.mutate(undefined, { onSuccess: done("核心已停止，mihomo 线路暂时不可用"), onError: fail });
  };

  return (
    <section className="rounded-2xl border bg-card p-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={Cpu} tone={label.tone} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">mihomo 核心</h3>
            <Badge variant={label.tone === "success" ? "success" : label.tone === "danger" ? "danger" : label.tone === "warning" ? "warning" : "secondary"} size="sm">
              {label.label}
            </Badge>
            {core.version ? <span className="font-mono text-xs text-muted-foreground">{core.version}</span> : null}
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {core.status === "running"
              ? `PID ${core.pid ?? "—"}，${core.startedAt ? `${formatRelative(core.startedAt)}启动` : ""}，${core.proxies} 个节点、${core.groups} 个策略组${core.providers ? `、${core.providers} 个订阅` : ""}`
              : core.status === "disabled"
                ? "画布上没有 mihomo 节点时不运行；添加节点、策略组或订阅并应用后自动拉起"
                : (core.lastError ?? `平台 ${core.platform}`)}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          {!core.binaryPresent || core.status === "missing" ? (
            <Button size="sm" variant="outline" className="gap-1" disabled={busy} onClick={() => run("install")}>
              {install.isPending ? <Loader2 className="animate-spin" /> : <Download />}
              安装核心
            </Button>
          ) : (
            <>
              <Button size="icon" variant="ghost" className="size-8" title="升级到最新版" disabled={busy} onClick={() => run("install")}>
                {install.isPending ? <Loader2 className="animate-spin" /> : <Download />}
              </Button>
              <Button size="icon" variant="ghost" className="size-8" title={core.status === "running" ? "重启核心" : "启动核心"} disabled={busy || core.status === "disabled"} onClick={() => run("restart")}>
                {restart.isPending ? <Loader2 className="animate-spin" /> : core.status === "running" ? <RotateCcw /> : <Play />}
              </Button>
              <Button size="icon" variant="ghost" className="size-8 hover:text-destructive" title="停止核心" disabled={busy || core.status !== "running"} onClick={() => run("stop")}>
                <Square />
              </Button>
            </>
          )}
        </div>
      </div>

      {core.status === "running" ? (
        <>
          <div className="mt-4 grid grid-cols-3 gap-2">
            <Speed icon={ArrowDown} label="下行" value={formatSpeed(core.downloadSpeed)} className="text-emerald-600 dark:text-emerald-400" />
            <Speed icon={ArrowUp} label="上行" value={formatSpeed(core.uploadSpeed)} className="text-sky-600 dark:text-sky-400" />
            <Speed icon={Cpu} label="内存" value={formatBytes(core.memoryInUse)} />
          </div>
          <ChartContainer config={chartConfig} className="mt-3 h-[132px] w-full">
            <AreaChart data={rows} margin={{ left: 0, right: 4, top: 6, bottom: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="time" tickLine={false} axisLine={false} fontSize={10} minTickGap={48} />
              <YAxis tickLine={false} axisLine={false} fontSize={10} width={56} tickFormatter={(value: number) => formatSpeed(value)} />
              <ChartTooltip content={<ChartTooltipContent formatter={(value, name) => `${chartConfig[name as keyof typeof chartConfig]?.label ?? name} ${formatSpeed(Number(value))}`} />} />
              <Area type="monotone" dataKey="down" stroke="var(--color-down)" fill="var(--color-down)" fillOpacity={0.14} strokeWidth={1.5} isAnimationActive={false} />
              <Area type="monotone" dataKey="up" stroke="var(--color-up)" fill="var(--color-up)" fillOpacity={0.1} strokeWidth={1.5} isAnimationActive={false} />
            </AreaChart>
          </ChartContainer>
        </>
      ) : (
        <div className="mt-4 rounded-xl border border-dashed px-4 py-6 text-center text-xs leading-5 text-muted-foreground">
          {core.status === "missing" || !core.binaryPresent
            ? core.autoInstall
              ? "核心程序还不在本机。应用含 mihomo 节点的画布时会自动从 GitHub 发布页下载并校验，也可以现在手动安装。"
              : "核心程序还不在本机，自动安装已关闭（EGRESS_MIHOMO_AUTO_INSTALL）。"
            : core.status === "crashed"
              ? `核心异常退出，已重试 ${core.restarts} 次，会按退避自动拉起。`
              : "核心没有在运行。"}
          <div className="mt-1 font-mono text-[11px]">{core.binary}</div>
        </div>
      )}
      {core.appliedAt ? <p className="mt-2 text-right text-[11px] text-muted-foreground">配置 {core.configHash} 于 {formatDateTime(core.appliedAt)} 载入</p> : null}
    </section>
  );
}

function Speed({ icon: Icon, label, value, className }: { icon: React.ComponentType<{ className?: string }>; label: string; value: string; className?: string }) {
  return (
    <div className="rounded-xl bg-muted/60 px-3 py-2">
      <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
        <Icon className="size-3" />
        {label}
      </div>
      <div className={cn("text-base font-semibold tabular-nums", className)}>{value}</div>
    </div>
  );
}

function LinesPanel({ view }: { view: EgressGraphView }) {
  const runtime = view.runtime;
  if (!runtime.endpoints.length && !runtime.rules.length) {
    return <EmptyState title="还没有线路与规则" description="打开画布编辑，从节点库拖出规则与线路，或导入一份 mihomo 配置。" />;
  }
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <section className="overflow-hidden rounded-2xl border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h4 className="text-sm font-semibold">线路</h4>
          <span className="text-xs text-muted-foreground">{runtime.endpoints.length} 条</span>
        </div>
        <div className="divide-y">
          {runtime.endpoints.map((endpoint) => {
            const mihomo = endpoint.protocol === "mihomo";
            return (
              <div key={endpoint.name} className="flex items-center gap-3 px-4 py-2.5">
                <span className={cn("size-2 shrink-0 rounded-full", !endpoint.enabled ? "bg-muted-foreground/40" : endpoint.healthy ? "bg-emerald-500" : "bg-red-500")} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{mihomo ? endpoint.remark || endpoint.name : endpoint.name}</span>
                    <Badge variant="outline" size="sm">
                      {mihomo ? "mihomo" : (PROTOCOL_LABEL[endpoint.protocol] ?? endpoint.protocol)}
                    </Badge>
                  </div>
                  <div className="truncate text-[11px] text-muted-foreground">
                    {endpoint.chain && endpoint.chain.length > 1 ? `经 ${endpoint.chain.slice(1).join(" ← ")}，` : ""}
                    {endpoint.dials.toLocaleString()} 次连接，{endpoint.failures} 次失败，{formatBytes(endpoint.bytesIn + endpoint.bytesOut)}
                    {endpoint.lastError ? `，${endpoint.lastError}` : ""}
                  </div>
                  <div className="truncate text-[11px] text-muted-foreground">
                    {endpoint.udp
                      ? `UDP ${endpoint.udpHealthy ? (endpoint.udpLatencyMs ? `${endpoint.udpLatencyMs} ms` : "可用") : `不可用${endpoint.udpLastError ? `：${endpoint.udpLastError}` : ""}`}，${endpoint.udpSessions.toLocaleString()} 个会话，${endpoint.packetsIn.toLocaleString()} / ${endpoint.packetsOut.toLocaleString()} 个报文`
                      : "只承载 TCP"}
                  </div>
                </div>
                <span className={cn("shrink-0 text-xs font-semibold tabular-nums", TONE_TEXT[delayTone(endpoint.latencyMs)], !endpoint.latencyMs && "text-muted-foreground")}>
                  {endpoint.latencyMs ? `${endpoint.latencyMs} ms` : "—"}
                </span>
              </div>
            );
          })}
        </div>
      </section>
      <section className="overflow-hidden rounded-2xl border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h4 className="text-sm font-semibold">规则命中</h4>
          <span className="text-xs text-muted-foreground">本进程启动以来</span>
        </div>
        <div className="divide-y">
          {runtime.rules.map((rule, index) => {
            const max = Math.max(1, ...runtime.rules.map((item) => item.matched));
            return (
              <div key={rule.name} className="space-y-1 px-4 py-2.5">
                <div className="flex items-center gap-2 text-sm">
                  <span className="w-6 shrink-0 text-xs tabular-nums text-muted-foreground">#{index + 1}</span>
                  <span className="min-w-0 flex-1 truncate font-medium">{rule.name}</span>
                  {rule.networks?.length === 1 ? <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{rule.networks[0].toUpperCase()}</span> : null}
                  <span className="shrink-0 text-xs text-muted-foreground">{ACTION_LABEL[rule.action]}</span>
                  <span className="w-16 shrink-0 text-right text-xs font-semibold tabular-nums">{rule.matched.toLocaleString()}</span>
                </div>
                <div className="ml-8 h-1 overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-violet-500" style={{ width: `${(rule.matched / max) * 100}%` }} />
                </div>
              </div>
            );
          })}
        </div>
      </section>
      <div className="flex justify-end xl:col-span-2">
        <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
          <RefreshCw className="size-3" /> 每 30 秒刷新
        </span>
      </div>
    </div>
  );
}
