"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  CircleCheck,
  CircleX,
  Database,
  FolderOpen,
  Gauge,
  HeartPulse,
  Loader2,
  Pause,
  Play,
  RefreshCw,
  RotateCcw,
  Search,
  Trash2,
  Undo2,
  X
} from "lucide-react";
import { toast } from "sonner";
import { Field } from "@/components/apps/item-grant/item-grant-fields";
import type { EgressEditorSeed } from "@/components/configuration/egress/egress-editor";
import { OptionSelect } from "@/components/configuration/egress/egress-fields";
import { ACTION_LABEL, delayTone, formatBytes } from "@/components/configuration/egress/egress-meta";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { TONE_TEXT } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import type { EgressExplanation, EgressGraphView, EgressTestResult, MihomoConnection, MihomoProxyInfo } from "@/lib/api/egress";
import { ApiError } from "@/lib/api/client";
import { formatClockSeconds, formatDateTime, formatRelative } from "@/lib/datetime";
import {
  useCloseMihomoConnectionMutation,
  useDiscardEgressGraphMutation,
  useEgressExplainMutation,
  useEgressGraphHistoryQuery,
  useEgressGraphVersionMutation,
  useEgressTestMutation,
  useMihomoConnectionsQuery,
  useMihomoDNSMutation,
  useMihomoDelayMutation,
  useMihomoLogsQuery,
  useMihomoMaintenanceMutation,
  useMihomoProviderMutation,
  useMihomoProxiesQuery,
  useMihomoRulesQuery,
  useResetEgressSettingsMutation,
  useRollbackEgressGraphMutation,
  useSelectMihomoMutation
} from "@/lib/egress-hooks";
import { cn } from "@/lib/utils";

/** 出海代理页下半部分的运行态页签：策略组、订阅与规则集、连接、日志、诊断、版本。 */

function fail(error: unknown) {
  toast.error(error instanceof ApiError ? error.message : "操作失败");
}

function lastDelay(info?: MihomoProxyInfo) {
  const history = info?.history ?? [];
  return history.length ? history[history.length - 1].delay : 0;
}

function CoreUnavailable({ error }: { error: unknown }) {
  return <EmptyState title="核心没有在运行" description={error instanceof Error ? error.message : "应用含 mihomo 节点的画布后，核心会自动拉起。"} />;
}

// ── 策略组 ──

const SELECTABLE = new Set(["Selector", "URLTest", "Fallback"]);
const GROUP_TYPES = new Set(["Selector", "URLTest", "Fallback", "LoadBalance", "Relay"]);
const GROUP_LABEL: Record<string, string> = { Selector: "手动选择", URLTest: "自动测速", Fallback: "故障转移", LoadBalance: "负载均衡", Relay: "链式中继" };

export function GroupsPanel({ view }: { view: EgressGraphView }) {
  const query = useMihomoProxiesQuery(true);
  const delay = useMihomoDelayMutation();
  const select = useSelectMihomoMutation();
  const [testing, setTesting] = useState<string | null>(null);
  const [search, setSearch] = useState("");

  const order = useMemo(() => Object.values(view.compile.mihomoNames ?? {}), [view.compile.mihomoNames]);
  if (query.isLoading) return <LoadingState title="读取策略组" />;
  if (query.error || !query.data) return <CoreUnavailable error={query.error} />;
  const proxies = query.data.proxies;
  const groups = Object.values(proxies)
    .filter((item) => GROUP_TYPES.has(item.type) && item.name !== "GLOBAL")
    .sort((a, b) => (order.indexOf(a.name) + 1 || 1e6) - (order.indexOf(b.name) + 1 || 1e6));
  const keyword = search.trim().toLowerCase();

  const testGroup = (name: string) => {
    setTesting(name);
    delay.mutate(
      { name, group: true, url: view.catalog.mihomo.defaultTestUrl, timeoutMs: 5000 },
      {
        onSuccess: (data) => toast.success(`${name}：${Object.values(data.delays).filter(Boolean).length} 个节点可用`),
        onError: fail,
        onSettled: () => {
          setTesting(null);
          void query.refetch();
        }
      }
    );
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <div className="relative max-w-xs flex-1">
          <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 pl-8 text-xs" placeholder="搜索节点" value={search} onChange={(event) => setSearch(event.target.value)} />
        </div>
        <Button size="sm" variant="ghost" className="gap-1 text-xs" onClick={() => void query.refetch()}>
          <RefreshCw className={cn(query.isFetching && "animate-spin")} /> 刷新
        </Button>
      </div>
      {!groups.length ? <EmptyState title="核心里没有策略组" /> : null}
      <div className="grid gap-3 lg:grid-cols-2">
        {groups.map((group) => {
          const members = (group.all ?? []).filter((member) => !keyword || member.toLowerCase().includes(keyword));
          const selectable = SELECTABLE.has(group.type);
          return (
            <section key={group.name} className="rounded-2xl border bg-card p-4">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-semibold">{group.name}</span>
                    <Badge variant="outline" size="sm">
                      {GROUP_LABEL[group.type] ?? group.type}
                    </Badge>
                  </div>
                  <div className="mt-0.5 truncate text-xs text-muted-foreground">
                    当前 <span className="font-medium text-foreground">{group.now ?? "—"}</span>
                    {group.fixed ? "（已固定）" : ""}，共 {(group.all ?? []).length} 个成员
                  </div>
                </div>
                {group.fixed ? (
                  <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => select.mutate({ group: group.name, name: "" }, { onSuccess: () => void query.refetch(), onError: fail })}>
                    取消固定
                  </Button>
                ) : null}
                <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" disabled={testing === group.name} onClick={() => testGroup(group.name)}>
                  {testing === group.name ? <Loader2 className="animate-spin" /> : <Gauge />}
                  测速
                </Button>
              </div>
              <div className="mt-3 grid grid-cols-2 gap-1.5 sm:grid-cols-3">
                {members.map((member) => {
                  const info = proxies[member];
                  const value = lastDelay(info);
                  const active = member === group.now;
                  return (
                    <button
                      key={member}
                      type="button"
                      disabled={!selectable || select.isPending}
                      onClick={() =>
                        select.mutate(
                          { group: group.name, name: member },
                          { onSuccess: () => (toast.success(`${group.name} 已切换到 ${member}`), void query.refetch()), onError: fail }
                        )
                      }
                      className={cn(
                        "rounded-xl border px-2.5 py-2 text-left transition-colors",
                        active ? "border-amber-500/70 bg-amber-500/8" : "hover:border-foreground/20",
                        selectable ? "cursor-pointer" : "cursor-default"
                      )}
                    >
                      <div className="truncate text-xs font-medium" title={member}>
                        {member}
                      </div>
                      <div className="mt-0.5 flex items-center justify-between text-[11px]">
                        <span className="truncate text-muted-foreground">{info?.type ?? ""}</span>
                        <span className={cn("shrink-0 tabular-nums", TONE_TEXT[delayTone(value)], !value && "text-muted-foreground")}>{value ? `${value} ms` : "—"}</span>
                      </div>
                    </button>
                  );
                })}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}

// ── 订阅与规则集 ──

export function ProvidersPanel() {
  const query = useMihomoProxiesQuery(true);
  const action = useMihomoProviderMutation();
  const maintenance = useMihomoMaintenanceMutation();
  const [busy, setBusy] = useState<string | null>(null);
  if (query.isLoading) return <LoadingState title="读取订阅" />;
  if (query.error || !query.data) return <CoreUnavailable error={query.error} />;
  const providers = Object.values(query.data.providers ?? {}).filter((item) => item.vehicleType !== "Compatible");
  const ruleProviders = Object.values(query.data.ruleProviders ?? {});
  const run = (kind: "proxy" | "rule", name: string, verb: "update" | "healthcheck") => {
    setBusy(`${kind}:${name}:${verb}`);
    action.mutate(
      { kind, name, action: verb },
      {
        onSuccess: () => toast.success(verb === "update" ? `「${name}」已更新` : `「${name}」健康检查完成`),
        onError: fail,
        onSettled: () => {
          setBusy(null);
          void query.refetch();
        }
      }
    );
  };
  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-2xl border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h4 className="text-sm font-semibold">订阅</h4>
          <span className="text-xs text-muted-foreground">{providers.length} 个</span>
        </div>
        {!providers.length ? <div className="px-4 py-6 text-center text-xs text-muted-foreground">画布上没有订阅节点</div> : null}
        <div className="divide-y">
          {providers.map((provider) => {
            const usage = provider.subscriptionInfo;
            const used = usage ? usage.Upload + usage.Download : 0;
            return (
              <div key={provider.name} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{provider.name}</span>
                    <Badge variant="outline" size="sm">
                      {provider.vehicleType}
                    </Badge>
                  </div>
                  <div className="text-[11px] text-muted-foreground">
                    {(provider.proxies ?? []).length} 个节点，{provider.updatedAt ? `${formatRelative(provider.updatedAt)}更新` : "尚未更新"}
                    {usage && usage.Total > 0 ? `，已用 ${formatBytes(used)} / ${formatBytes(usage.Total)}` : ""}
                    {usage?.Expire ? `，${formatDateTime(usage.Expire * 1000)} 到期` : ""}
                  </div>
                  {usage && usage.Total > 0 ? (
                    <div className="mt-1.5 h-1.5 max-w-sm overflow-hidden rounded-full bg-muted">
                      <div className="h-full rounded-full bg-cyan-500" style={{ width: `${Math.min(100, (used / usage.Total) * 100)}%` }} />
                    </div>
                  ) : null}
                </div>
                <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" disabled={busy !== null} onClick={() => run("proxy", provider.name, "healthcheck")}>
                  {busy === `proxy:${provider.name}:healthcheck` ? <Loader2 className="animate-spin" /> : <HeartPulse />}
                  健康检查
                </Button>
                <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" disabled={busy !== null} onClick={() => run("proxy", provider.name, "update")}>
                  {busy === `proxy:${provider.name}:update` ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                  更新
                </Button>
              </div>
            );
          })}
        </div>
      </section>
      <section className="overflow-hidden rounded-2xl border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h4 className="text-sm font-semibold">规则集</h4>
          <span className="text-xs text-muted-foreground">{ruleProviders.length} 个</span>
        </div>
        {!ruleProviders.length ? <div className="px-4 py-6 text-center text-xs text-muted-foreground">分流表里没有规则集</div> : null}
        <div className="divide-y">
          {ruleProviders.map((provider) => (
            <div key={provider.name} className="flex items-center gap-3 px-4 py-2.5">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">{provider.name}</div>
                <div className="text-[11px] text-muted-foreground">
                  {provider.behavior} {provider.format}，{provider.ruleCount.toLocaleString()} 条，{provider.updatedAt ? `${formatRelative(provider.updatedAt)}更新` : "尚未更新"}
                </div>
              </div>
              <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" disabled={busy !== null} onClick={() => run("rule", provider.name, "update")}>
                {busy === `rule:${provider.name}:update` ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                更新
              </Button>
            </div>
          ))}
        </div>
      </section>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          className="gap-1 text-xs"
          disabled={maintenance.isPending}
          onClick={() => maintenance.mutate("geo", { onSuccess: () => toast.success("Geo 数据库已更新"), onError: fail })}
        >
          <Database /> 更新 Geo 数据库
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="gap-1 text-xs"
          disabled={maintenance.isPending}
          onClick={() => maintenance.mutate("flush-dns", { onSuccess: () => toast.success("DNS 缓存已清空"), onError: fail })}
        >
          <Trash2 /> 清空 DNS 缓存
        </Button>
      </div>
    </div>
  );
}

// ── 连接 ──

function connectionHost(connection: MihomoConnection) {
  const meta = connection.metadata;
  const host = String(meta.host || meta.sniffHost || meta.destinationIP || "");
  return `${host}${meta.destinationPort ? `:${String(meta.destinationPort)}` : ""}`;
}

function duration(start: string, now: number) {
  const seconds = Math.max(0, Math.round((now - new Date(start).getTime()) / 1000));
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
  return `${Math.floor(seconds / 3600)} 时 ${Math.floor((seconds % 3600) / 60)} 分`;
}

export function ConnectionsPanel() {
  const [paused, setPaused] = useState(false);
  const [search, setSearch] = useState("");
  const query = useMihomoConnectionsQuery(true, paused);
  const close = useCloseMihomoConnectionMutation();
  const [confirmAll, setConfirmAll] = useState(false);
  if (query.isLoading) return <LoadingState title="读取连接" />;
  if (query.error || !query.data) return <CoreUnavailable error={query.error} />;
  const data = query.data;
  const now = query.dataUpdatedAt;
  const keyword = search.trim().toLowerCase();
  const connections = (data.connections ?? [])
    .filter((connection) => !keyword || `${connectionHost(connection)} ${(connection.chains ?? []).join(" ")} ${connection.rule} ${connection.rulePayload}`.toLowerCase().includes(keyword))
    .sort((a, b) => new Date(b.start).getTime() - new Date(a.start).getTime());

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative max-w-xs flex-1">
          <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 pl-8 text-xs" placeholder="按目标、线路或规则筛选" value={search} onChange={(event) => setSearch(event.target.value)} />
        </div>
        <span className="text-xs text-muted-foreground tabular-nums">
          {(data.connections ?? []).length} 条在途，累计下行 {formatBytes(data.downloadTotal)}、上行 {formatBytes(data.uploadTotal)}
        </span>
        <div className="ml-auto flex gap-1">
          <Button size="sm" variant="ghost" className="gap-1 text-xs" onClick={() => setPaused(!paused)}>
            {paused ? <Play /> : <Pause />}
            {paused ? "继续" : "暂停"}
          </Button>
          <Button size="sm" variant="outline" className="gap-1 text-xs hover:text-destructive" disabled={!(data.connections ?? []).length} onClick={() => setConfirmAll(true)}>
            <X /> 全部关闭
          </Button>
        </div>
      </div>
      <div className="overflow-x-auto rounded-2xl border bg-card">
        <table className="w-full min-w-[760px] text-xs">
          <thead className="border-b bg-muted/40 text-left text-[11px] text-muted-foreground">
            <tr>
              <th className="px-3 py-2 font-medium">目标</th>
              <th className="px-3 py-2 font-medium">线路</th>
              <th className="px-3 py-2 font-medium">规则</th>
              <th className="px-3 py-2 text-right font-medium">下行 / 上行</th>
              <th className="px-3 py-2 text-right font-medium">时长</th>
              <th className="w-10 px-2 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y">
            {connections.map((connection) => (
              <tr key={connection.id} className="hover:bg-muted/30">
                <td className="max-w-[240px] truncate px-3 py-2 font-mono" title={connectionHost(connection)}>
                  {connectionHost(connection)}
                </td>
                <td className="max-w-[220px] truncate px-3 py-2">{[...(connection.chains ?? [])].reverse().join(" → ")}</td>
                <td className="max-w-[200px] truncate px-3 py-2 text-muted-foreground">
                  {connection.rule}
                  {connection.rulePayload ? ` ${connection.rulePayload}` : ""}
                </td>
                <td className="px-3 py-2 text-right tabular-nums">
                  {formatBytes(connection.download)} / {formatBytes(connection.upload)}
                </td>
                <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">{duration(connection.start, now)}</td>
                <td className="px-2 py-2">
                  <Button size="icon" variant="ghost" className="size-6 hover:text-destructive" onClick={() => close.mutate(connection.id, { onError: fail })} aria-label="关闭连接">
                    <X className="size-3" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!connections.length ? <div className="px-4 py-8 text-center text-xs text-muted-foreground">没有在途连接</div> : null}
      </div>
      <ConfirmActionDialog
        open={confirmAll}
        onOpenChange={setConfirmAll}
        title="关闭全部连接"
        description="经 mihomo 的在途请求会立即中断，调用方会收到连接错误并按各自的重试策略处理。"
        confirmLabel="全部关闭"
        destructive
        pending={close.isPending}
        onConfirm={() => close.mutate(undefined, { onSuccess: () => (toast.success("已关闭全部连接"), setConfirmAll(false)), onError: fail })}
      />
    </div>
  );
}

// ── 日志 ──

const LEVEL_STYLE: Record<string, string> = {
  error: "text-red-600 dark:text-red-400",
  fatal: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-sky-600 dark:text-sky-400",
  debug: "text-muted-foreground"
};

export function LogsPanel() {
  const [paused, setPaused] = useState(false);
  const [level, setLevel] = useState<string | undefined>(undefined);
  const [search, setSearch] = useState("");
  const query = useMihomoLogsQuery(0, true, paused);
  const boxRef = useRef<HTMLDivElement>(null);
  const lines = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    return (query.data?.items ?? []).filter(
      (line) => (!level || line.level === level || (level === "error" && line.level === "fatal")) && (!keyword || line.message.toLowerCase().includes(keyword))
    );
  }, [query.data, level, search]);
  useEffect(() => {
    if (!paused && boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight;
  }, [lines, paused]);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative max-w-xs flex-1">
          <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 pl-8 text-xs" placeholder="搜索日志" value={search} onChange={(event) => setSearch(event.target.value)} />
        </div>
        <div className="w-32">
          <OptionSelect value={level} options={["error", "warning", "info", "debug"]} labels={{ error: "错误", warning: "警告", info: "信息", debug: "调试" }} noneLabel="全部级别" onChange={setLevel} />
        </div>
        <Button size="sm" variant="ghost" className="ml-auto gap-1 text-xs" onClick={() => setPaused(!paused)}>
          {paused ? <Play /> : <Pause />}
          {paused ? "继续" : "暂停"}
        </Button>
      </div>
      <div ref={boxRef} className="h-[460px] overflow-y-auto rounded-2xl border bg-muted/30 p-3 font-mono text-[11px] leading-5">
        {lines.map((line) => (
          <div key={line.seq} className="flex gap-2">
            <span className="shrink-0 tabular-nums text-muted-foreground">{formatClockSeconds(line.time)}</span>
            <span className={cn("w-14 shrink-0 uppercase", LEVEL_STYLE[line.level] ?? "text-muted-foreground")}>{line.level}</span>
            <span className={cn("min-w-0 break-all", line.source === "aegis" && "font-semibold")}>{line.message}</span>
          </div>
        ))}
        {!lines.length ? <div className="py-10 text-center text-muted-foreground">没有日志</div> : null}
      </div>
      <p className="text-[11px] text-muted-foreground">保留最近 2000 行；加粗的是网关托管核心时记下的事件（启动、热重载、安装、自动拉起）。核心日志级别在画布的「mihomo 核心」节点里改。</p>
    </div>
  );
}

// ── 诊断 ──

export function DiagnosticsPanel({ view }: { view: EgressGraphView }) {
  const test = useEgressTestMutation();
  const explain = useEgressExplainMutation();
  const dns = useMihomoDNSMutation();
  const coreRunning = view.core.status === "running";
  const rules = useMihomoRulesQuery(coreRunning);
  const [url, setUrl] = useState("https://api.ipify.org?format=json");
  const [endpoint, setEndpoint] = useState<string | undefined>(undefined);
  const [profile, setProfile] = useState("");
  const [testResult, setTestResult] = useState<EgressTestResult | null>(null);
  const [host, setHost] = useState("api.stripe.com");
  const [explanation, setExplanation] = useState<EgressExplanation | null>(null);
  const [dnsName, setDnsName] = useState("www.google.com");
  const [dnsResult, setDnsResult] = useState<Record<string, unknown> | null>(null);
  const endpointLabels = Object.fromEntries(view.runtime.endpoints.map((item) => [item.name, item.protocol === "mihomo" ? `${item.remark || item.name}（mihomo）` : item.name]));

  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <section className="space-y-3 rounded-2xl border bg-card p-4">
        <div>
          <h4 className="text-sm font-semibold">出站自测</h4>
          <p className="text-xs text-muted-foreground">真实发一次请求。把地址指向查询出口 IP 的服务，可以确认落地在哪里。</p>
        </div>
        <Field label="地址">
          <Input className="h-8 font-mono text-xs" value={url} onChange={(event) => setUrl(event.target.value)} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="指定线路" hint="留空按路由表">
            <OptionSelect value={endpoint} options={view.runtime.endpoints.map((item) => item.name)} labels={endpointLabels} noneLabel="按路由表" onChange={setEndpoint} />
          </Field>
          <Field label="模拟调用方">
            <Input className="h-8 font-mono text-xs" placeholder="payment.stripe" value={profile} onChange={(event) => setProfile(event.target.value)} />
          </Field>
        </div>
        <Button
          size="sm"
          disabled={test.isPending}
          onClick={() => test.mutate({ url, endpoint, profile: profile || undefined, timeoutMs: 15000 }, { onSuccess: setTestResult, onError: fail })}
        >
          {test.isPending ? <Loader2 className="animate-spin" /> : <Play />}
          发送请求
        </Button>
        {testResult ? (
          <div className={cn("space-y-1 rounded-xl border p-3 text-xs", testResult.ok ? "border-emerald-500/40" : "border-red-500/40")}>
            <div className="flex items-center gap-1.5 font-medium">
              {testResult.ok ? <CircleCheck className="size-3.5 text-emerald-500" /> : <CircleX className="size-3.5 text-red-500" />}
              {testResult.ok ? `HTTP ${testResult.statusCode}，${testResult.latencyMs} ms` : (testResult.error ?? "失败")}
            </div>
            <div className="text-muted-foreground">
              {ACTION_LABEL[testResult.action]}
              {testResult.rule ? `，规则 ${testResult.rule}` : ""}
              {testResult.chain?.length ? `，经 ${testResult.chain.join(" ← ")}` : ""}
            </div>
            {testResult.body ? <pre className="max-h-32 overflow-auto rounded-lg bg-muted/60 p-2 font-mono text-[11px] whitespace-pre-wrap">{testResult.body}</pre> : null}
          </div>
        ) : null}
      </section>

      <section className="space-y-3 rounded-2xl border bg-card p-4">
        <div>
          <h4 className="text-sm font-semibold">路由解释</h4>
          <p className="text-xs text-muted-foreground">不发请求，只看这个目标会命中哪条规则、走哪条线路。画布编辑器的「追踪」会把路径画出来。</p>
        </div>
        <div className="flex gap-2">
          <Input className="h-8 font-mono text-xs" value={host} onChange={(event) => setHost(event.target.value)} />
          <Button size="sm" disabled={!host.trim() || explain.isPending} onClick={() => explain.mutate({ host: host.trim() }, { onSuccess: setExplanation, onError: fail })}>
            {explain.isPending ? <Loader2 className="animate-spin" /> : <Search />}
            解释
          </Button>
        </div>
        {explanation ? (
          <div className="space-y-1.5 rounded-xl border p-3 text-xs">
            <div className="flex justify-between">
              <span className="text-muted-foreground">结果</span>
              <span className="font-medium">
                {ACTION_LABEL[explanation.action]}
                {explanation.endpoint ? `，${explanation.chain?.join(" ← ") ?? explanation.endpoint}` : ""}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">规则</span>
              <span>{explanation.rule === "default" ? "未命中（兜底）" : explanation.rule}</span>
            </div>
            {explanation.reason ? (
              <div className="flex justify-between">
                <span className="text-muted-foreground">原因</span>
                <span className="font-mono">{explanation.reason}</span>
              </div>
            ) : null}
            {explanation.error ? <div className="text-red-600 dark:text-red-400">{explanation.error}</div> : null}
          </div>
        ) : null}
      </section>

      {coreRunning ? (
        <section className="space-y-3 rounded-2xl border bg-card p-4">
          <div>
            <h4 className="text-sm font-semibold">核心 DNS</h4>
            <p className="text-xs text-muted-foreground">经 mihomo 的 DNS 模块解析（核心 DNS 关闭时走系统解析）。</p>
          </div>
          <div className="flex gap-2">
            <Input className="h-8 font-mono text-xs" value={dnsName} onChange={(event) => setDnsName(event.target.value)} />
            <Button size="sm" disabled={!dnsName.trim() || dns.isPending} onClick={() => dns.mutate({ name: dnsName.trim(), type: "A" }, { onSuccess: setDnsResult, onError: fail })}>
              {dns.isPending ? <Loader2 className="animate-spin" /> : <Search />}
              解析
            </Button>
          </div>
          {dnsResult ? (
            <div className="space-y-1 rounded-xl border p-3 font-mono text-[11px]">
              {((dnsResult.Answer as Array<{ name: string; type: number; TTL: number; data: string }> | undefined) ?? []).map((answer, index) => (
                <div key={index} className="flex justify-between gap-2">
                  <span className="truncate">{answer.data}</span>
                  <span className="shrink-0 text-muted-foreground">TTL {answer.TTL}</span>
                </div>
              ))}
              {!(dnsResult.Answer as unknown[] | undefined)?.length ? <div className="text-muted-foreground">没有解析结果（状态 {String(dnsResult.Status ?? "")}）</div> : null}
            </div>
          ) : null}
        </section>
      ) : null}

      {coreRunning ? (
        <section className="space-y-2 rounded-2xl border bg-card p-4">
          <div className="flex items-center justify-between">
            <h4 className="text-sm font-semibold">核心生效的规则</h4>
            <span className="text-xs text-muted-foreground">{(rules.data?.items ?? []).length} 条</span>
          </div>
          <div className="max-h-72 space-y-0.5 overflow-y-auto font-mono text-[11px]">
            {(rules.data?.items ?? []).map((rule) => (
              <div key={rule.index} className="flex items-center gap-2">
                <span className="w-8 shrink-0 text-right text-muted-foreground">{rule.index + 1}</span>
                <span className={cn("min-w-0 flex-1 truncate", rule.type === "InUser" && "text-muted-foreground")}>
                  {rule.type},{rule.payload}
                </span>
                <span className="shrink-0 truncate">{rule.proxy}</span>
                {rule.extra ? <span className="w-12 shrink-0 text-right tabular-nums text-muted-foreground">{rule.extra.hitCount}</span> : null}
              </div>
            ))}
          </div>
          <p className="text-[11px] text-muted-foreground">InUser 开头的几条是网关自动置顶的分发规则：每个被引用的策略组或节点一条。</p>
        </section>
      ) : null}
    </div>
  );
}

// ── 版本 ──

export function VersionsPanel({ view, onOpen }: { view: EgressGraphView; onOpen: (seed: EgressEditorSeed) => void }) {
  const history = useEgressGraphHistoryQuery(true);
  const load = useEgressGraphVersionMutation();
  const rollback = useRollbackEgressGraphMutation();
  const discard = useDiscardEgressGraphMutation();
  const reset = useResetEgressSettingsMutation();
  const [pending, setPending] = useState<number | null>(null);
  const [confirmReset, setConfirmReset] = useState(false);
  const items = history.data?.items ?? [];
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-2">
        {view.dirty ? (
          <Button size="sm" variant="outline" className="gap-1 text-xs" disabled={discard.isPending} onClick={() => discard.mutate(undefined, { onSuccess: () => toast.success("草稿已恢复为已应用的版本"), onError: fail })}>
            <Undo2 /> 丢弃草稿
          </Button>
        ) : null}
        <Button size="sm" variant="outline" className="gap-1 text-xs hover:text-destructive" onClick={() => setConfirmReset(true)}>
          <RotateCcw /> 恢复为环境变量配置
        </Button>
      </div>
      {history.isLoading ? <LoadingState title="读取版本记录" /> : null}
      {!history.isLoading && !items.length ? <EmptyState title="还没有应用过画布" description="每次应用记一个版本，保留最近 20 个。" /> : null}
      <div className="divide-y overflow-hidden rounded-2xl border bg-card">
        {items.map((item) => (
          <div key={item.version} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span className="text-sm font-semibold tabular-nums">第 {item.version} 版</span>
                {item.version === view.appliedVersion && view.active ? (
                  <Badge variant="success" size="sm">
                    当前
                  </Badge>
                ) : null}
                <span className="text-xs text-muted-foreground">{formatDateTime(item.appliedAt)}</span>
              </div>
              <div className="truncate text-[11px] text-muted-foreground">
                {item.note ? `${item.note}，` : ""}
                {item.summary.activeRules} 条规则，{item.summary.endpoints} 条线路
                {item.summary.mihomoEnabled ? `，${item.summary.groups} 个策略组，${item.summary.proxies} 个节点` : ""}
              </div>
            </div>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 text-xs"
              disabled={load.isPending}
              onClick={() => load.mutate(item.version, { onSuccess: (data) => onOpen({ graph: data.graph, from: `第 ${item.version} 版` }), onError: fail })}
            >
              <FolderOpen /> 载入画布
            </Button>
            <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" disabled={item.version === view.appliedVersion && view.active} onClick={() => setPending(item.version)}>
              <RotateCcw /> 回滚
            </Button>
          </div>
        ))}
      </div>
      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={`回滚到第 ${pending ?? ""} 版`}
        description="这一版会被重新应用（记为新版本），mihomo 核心与网关路由表一起切换；当前草稿会被它覆盖。"
        confirmLabel="回滚"
        pending={rollback.isPending}
        onConfirm={() =>
          pending !== null &&
          rollback.mutate(pending, {
            onSuccess: (data) => {
              toast.success(`已回滚，当前为第 ${data.appliedVersion} 版`);
              setPending(null);
            },
            onError: fail
          })
        }
      />
      <ConfirmActionDialog
        open={confirmReset}
        onOpenChange={setConfirmReset}
        title="恢复为环境变量配置"
        description="撤掉画布与旧版控制台的配置，网关回到 .env 里的 EGRESS_* 设置，mihomo 核心停止。画布草稿会保留，之后可以再应用。"
        confirmLabel="恢复"
        destructive
        pending={reset.isPending}
        onConfirm={() =>
          reset.mutate(undefined, {
            onSuccess: () => {
              toast.success("已恢复为环境变量配置");
              setConfirmReset(false);
            },
            onError: fail
          })
        }
      />
    </div>
  );
}
