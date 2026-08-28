"use client";

import { FormEvent, useMemo, useState } from "react";
import {
  Activity,
  Database,
  Eraser,
  Gauge,
  Layers,
  RefreshCw,
  Save,
  TrendingUp,
} from "lucide-react";
import { Area, CartesianGrid, ComposedChart, Line, Tooltip, XAxis, YAxis } from "recharts";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import type { TrafficRampSettings, TrafficRampState } from "@/lib/api/traffic-ramp";
import {
  useResetTrafficRampStatsMutation,
  useTrafficRampSettingsQuery,
  useTrafficRampStatsQuery,
  useUpdateTrafficRampSettingsMutation,
} from "@/lib/traffic-ramp-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";

/**
 * 突发流量爬坡 —— 运行态统计 + 热配置（仅超级管理员）。
 *
 * 上半屏是「现在发生了什么」：状态机、当前准入上限、队列/在途水位、
 * 每秒放行-排队-拒绝的时间序列；下半屏是「怎么调」：所有参数保存即热生效。
 */

// ── 状态机展示 ──

const STATE_META: Record<TrafficRampState, { label: string; className: string }> = {
  stable: { label: "稳态", className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  ramping: { label: "爬坡中", className: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  saturated: { label: "饱和", className: "bg-red-500/15 text-red-600 dark:text-red-400" },
  cooldown: { label: "回落中", className: "bg-sky-500/15 text-sky-600 dark:text-sky-400" },
};

const RANGE_OPTIONS = [
  { value: "60", label: "最近 1 分钟" },
  { value: "300", label: "最近 5 分钟" },
  { value: "900", label: "最近 15 分钟" },
] as const;

const chartConfig = {
  arrivals: { label: "到达" },
  admitted: { label: "放行" },
  queued: { label: "排队放行" },
  rejected: { label: "拒绝" },
  limit: { label: "准入上限" },
} satisfies ChartConfig;

const CHART_LABELS: Record<string, string> = {
  arrivals: "到达",
  admitted: "直接放行",
  queued: "排队放行",
  rejected: "拒绝",
  limit: "准入上限",
};

// ── 表单状态（数值输入一律存字符串，提交时转换，与防火墙面板一致） ──

type Draft = {
  enabled: boolean;
  baselineRps: string;
  maxRps: string;
  rampStepPct: string;
  rampIntervalMs: string;
  cooldownSeconds: string;
  queueSize: string;
  queueTimeoutMs: string;
  maxConcurrent: string;
  exemptPathPrefixes: string[];
  exemptAdmin: boolean;
  retryAfterSeconds: string;
};

function makeSeed(v: TrafficRampSettings): Draft {
  return {
    enabled: v.enabled,
    baselineRps: String(v.baselineRps || 300),
    maxRps: String(v.maxRps || 3000),
    rampStepPct: String(v.rampStepPct || 20),
    rampIntervalMs: String(v.rampIntervalMs || 1000),
    cooldownSeconds: String(v.cooldownSeconds || 30),
    queueSize: String(v.queueSize ?? 1000),
    queueTimeoutMs: String(v.queueTimeoutMs || 2000),
    maxConcurrent: String(v.maxConcurrent ?? 0),
    exemptPathPrefixes: v.exemptPathPrefixes || [],
    exemptAdmin: v.exemptAdmin,
    retryAfterSeconds: String(v.retryAfterSeconds || 3),
  };
}

function fmtDate(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return isNaN(d.getTime())
    ? "—"
    : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function fmtClock(tsSec: number) {
  return new Date(tsSec * 1000).toLocaleTimeString("zh-CN", { hour12: false });
}

// ── 主组件 ──

export function TrafficRampPanel() {
  const operator = useAuthStore((s) => s.operator);
  const isSuperAdmin = Boolean(operator?.isSuperAdmin);
  const settingsQ = useTrafficRampSettingsQuery(isSuperAdmin);
  const updateMut = useUpdateTrafficRampSettingsMutation();
  const resetMut = useResetTrafficRampStatsMutation();

  const [range, setRange] = useState<string>("300");
  const statsQ = useTrafficRampStatsQuery(Number(range), isSuperAdmin);
  const stats = statsQ.data;

  const settings = settingsQ.data;
  const seed = useMemo(() => (settings ? makeSeed(settings) : null), [settings]);
  const seedKey = useMemo(() => (seed ? JSON.stringify(seed) : ""), [seed]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [syncedKey, setSyncedKey] = useState("");

  // 服务端配置到达 / 变化时同步表单种子。写在渲染期而不是 useEffect：
  // 这是 React 官方的「随 props 调整 state」模式，少一轮级联渲染。
  if (seed && seedKey !== syncedKey) {
    setDraft(seed);
    setSyncedKey(seedKey);
  }

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft((s) => (s ? { ...s, [k]: v } : s));

  async function handleSave(e?: FormEvent) {
    e?.preventDefault();
    if (!draft) return;
    try {
      await updateMut.mutateAsync({
        enabled: draft.enabled,
        baselineRps: Number(draft.baselineRps || 300),
        maxRps: Number(draft.maxRps || 3000),
        rampStepPct: Number(draft.rampStepPct || 20),
        rampIntervalMs: Number(draft.rampIntervalMs || 1000),
        cooldownSeconds: Number(draft.cooldownSeconds || 30),
        queueSize: Number(draft.queueSize || 0),
        queueTimeoutMs: Number(draft.queueTimeoutMs || 2000),
        maxConcurrent: Number(draft.maxConcurrent || 0),
        exemptPathPrefixes: draft.exemptPathPrefixes,
        exemptAdmin: draft.exemptAdmin,
        retryAfterSeconds: Number(draft.retryAfterSeconds || 3),
      });
      toast.success("流量爬坡配置已保存，即刻生效");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleResetStats() {
    try {
      await resetMut.mutateAsync();
      toast.success("统计已清零");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "清零失败");
    }
  }

  if (!isSuperAdmin) return <EmptyState title="无访问权限" description="仅超级管理员可查看与配置流量爬坡。" />;
  if (settingsQ.isLoading || !draft || !settings) return <LoadingState title="加载流量爬坡配置" />;

  const stateMeta = stats ? STATE_META[stats.state] ?? STATE_META.stable : STATE_META.stable;
  const totalRejected = stats
    ? stats.totalRejectedRate + stats.totalRejectedTimeout + stats.totalRejectedLoad
    : 0;
  const chartData = (stats?.series ?? []).map((p) => ({
    time: fmtClock(p.ts),
    arrivals: p.arrivals,
    admitted: p.admitted,
    queued: p.queued,
    rejected: p.rejected,
    limit: p.limit,
  }));

  return (
    <div className="space-y-6">
      {/* ── 运行态概览 ── */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard
          icon={Activity}
          label="爬坡状态"
          value={stats?.enabled ? stateMeta.label : "未启用"}
          ok={Boolean(stats?.enabled)}
          badgeClassName={stats?.enabled ? stateMeta.className : undefined}
          sub={stats?.lastBurstAt ? `上次突发 ${fmtDate(stats.lastBurstAt)}` : "尚未发生突发"}
        />
        <StatusCard
          icon={Gauge}
          label="当前准入上限"
          value={stats ? `${Math.round(stats.currentLimit)} req/s` : "—"}
          ok
          sub={stats ? `基线 ${stats.baselineRps} · 顶 ${stats.maxRps}` : undefined}
        />
        <StatusCard
          icon={Layers}
          label="在途 / 排队"
          value={stats ? `${stats.inflight} / ${stats.queueDepth}` : "—"}
          ok={!stats || stats.queueCapacity === 0 || stats.queueDepth < stats.queueCapacity}
          sub={stats ? `队列容量 ${stats.queueCapacity || "不排队"}` : undefined}
        />
        <StatusCard
          icon={RefreshCw}
          label="热重载"
          value={`v${settings.reloadVersion || 0}`}
          ok
          sub={`${settings.source === "database" ? "数据库" : "环境变量"} · ${fmtDate(settings.reloadedAt)}`}
        />
      </div>

      {/* ── 时间序列 ── */}
      <Card>
        <CardContent className="p-4">
          <div className="mb-2 flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <TrendingUp className="size-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold">每秒流量与准入上限</h3>
              {stats?.enabled === false && (
                <Badge variant="outline" className="text-[10px]">未启用 · 无整形数据</Badge>
              )}
            </div>
            <div className="flex items-center gap-2">
              <Select value={range} onValueChange={setRange}>
                <SelectTrigger className="h-7 w-32 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {RANGE_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => void handleResetStats()} disabled={resetMut.isPending}>
                <Eraser className="size-3" /> 清零统计
              </Button>
            </div>
          </div>
          {statsQ.isLoading ? (
            <div className="flex h-[220px] items-center justify-center text-xs text-muted-foreground">加载统计…</div>
          ) : chartData.length > 0 ? (
            <ChartContainer config={chartConfig} className="h-[220px] w-full">
              <ComposedChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: -12 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                <XAxis
                  dataKey="time"
                  tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                  axisLine={false}
                  tickLine={false}
                  tickMargin={8}
                  minTickGap={60}
                />
                <YAxis
                  tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                  axisLine={false}
                  tickLine={false}
                  allowDecimals={false}
                  width={40}
                />
                <Tooltip
                  cursor={{ stroke: "var(--muted-foreground)", strokeOpacity: 0.2 }}
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="rounded-lg border bg-popover px-3 py-2 shadow-md">
                        <div className="text-[11px] text-muted-foreground">{label}</div>
                        <div className="mt-1 space-y-0.5">
                          {payload.map((p) => (
                            <div key={p.dataKey as string} className="flex items-center justify-between gap-4 text-xs">
                              <span className="flex items-center gap-1.5">
                                <span className="inline-block size-2 rounded-sm" style={{ background: p.color }} />
                                {CHART_LABELS[String(p.dataKey)] || String(p.dataKey)}
                              </span>
                              <span className="font-semibold tabular-nums">{Number(p.value).toLocaleString()}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    );
                  }}
                />
                <Area dataKey="arrivals" type="monotone" fill="#a1a1aa" fillOpacity={0.18} stroke="#a1a1aa" strokeWidth={1} />
                <Area dataKey="admitted" type="monotone" fill="#10b981" fillOpacity={0.25} stroke="#10b981" strokeWidth={1.5} />
                <Area dataKey="queued" type="monotone" fill="#f59e0b" fillOpacity={0.3} stroke="#f59e0b" strokeWidth={1} />
                <Area dataKey="rejected" type="monotone" fill="#ef4444" fillOpacity={0.3} stroke="#ef4444" strokeWidth={1} />
                <Line dataKey="limit" type="stepAfter" stroke="#6366f1" strokeWidth={1.5} strokeDasharray="5 3" dot={false} />
              </ComposedChart>
            </ChartContainer>
          ) : (
            <div className="flex h-[220px] items-center justify-center text-xs text-muted-foreground">暂无统计数据</div>
          )}
        </CardContent>
      </Card>

      {/* ── 累计计数 ── */}
      <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-6">
        <MetricCard label="到达" value={stats?.totalArrivals} />
        <MetricCard label="直接放行" value={stats?.totalAdmitted} tone="emerald" />
        <MetricCard label="排队放行" value={stats?.totalQueuedAdmitted} tone="amber" />
        <MetricCard
          label="拒绝"
          value={totalRejected}
          tone="red"
          sub={stats ? `限速 ${stats.totalRejectedRate} · 超时 ${stats.totalRejectedTimeout} · 并发 ${stats.totalRejectedLoad}` : undefined}
        />
        <MetricCard label="豁免放行" value={stats?.totalExempt} />
        <MetricCard
          label="爬坡次数"
          value={stats?.rampEvents}
          sub={stats ? `峰值 ${stats.peakArrivalRps} req/s · 统计自 ${fmtDate(stats.statsSince)}` : undefined}
        />
      </div>

      <Separator />

      {/* ── 配置表单 ── */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">爬坡配置</h2>
          <p className="text-sm text-muted-foreground">
            保存后热重载即刻生效并持久化；所有速率均为单实例口径。
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => { setDraft(seed); setSyncedKey(seedKey); }} disabled={updateMut.isPending}>
            重置
          </Button>
          <Button size="sm" onClick={() => void handleSave()} disabled={updateMut.isPending}>
            <Save className="size-3.5" /> {updateMut.isPending ? "保存中..." : "保存"}
          </Button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <SwitchRow
          label="启用流量爬坡"
          description="突发洪峰从基线速率逐步放行，超出部分排队削峰或拒绝"
          checked={draft.enabled}
          onCheckedChange={(v) => set("enabled", v)}
        />
        <SwitchRow
          label="管理端豁免"
          description="洪峰时管理员仍可进入控制台查看统计、调整参数（建议开启）"
          checked={draft.exemptAdmin}
          onCheckedChange={(v) => set("exemptAdmin", v)}
        />
      </div>

      <Accordion type="multiple" defaultValue={["rate", "queue", "exempt"]} className="space-y-3">
        <AccordionItem value="rate" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">速率与爬坡节奏</AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
              <Field label="基线速率 (req/s)">
                <Input className="h-8 text-sm" type="number" min={1} value={draft.baselineRps} onChange={(e) => set("baselineRps", e.target.value)} />
              </Field>
              <Field label="爬坡上限 (req/s)">
                <Input className="h-8 text-sm" type="number" min={1} value={draft.maxRps} onChange={(e) => set("maxRps", e.target.value)} />
              </Field>
              <Field label="每步抬升 (% 基线)">
                <Input className="h-8 text-sm" type="number" min={1} max={1000} value={draft.rampStepPct} onChange={(e) => set("rampStepPct", e.target.value)} />
              </Field>
              <Field label="爬坡周期 (ms)">
                <Input className="h-8 text-sm" type="number" min={100} max={60000} step={100} value={draft.rampIntervalMs} onChange={(e) => set("rampIntervalMs", e.target.value)} />
              </Field>
              <Field label="回落冷静期 (s)">
                <Input className="h-8 text-sm" type="number" min={1} max={3600} value={draft.cooldownSeconds} onChange={(e) => set("cooldownSeconds", e.target.value)} />
              </Field>
            </div>
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              需求逼近当前上限时，每个周期把上限抬高「基线 × 每步抬升%」，直至爬坡上限；
              需求持续低于基线超过冷静期后按同样步长回落。多副本部署时集群吞吐 = 副本数 × 上限。
            </p>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="queue" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">排队削峰与并发保护</AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Field label="排队容量 (0 = 不排队)">
                <Input className="h-8 text-sm" type="number" min={0} max={100000} value={draft.queueSize} onChange={(e) => set("queueSize", e.target.value)} />
              </Field>
              <Field label="排队超时 (ms)">
                <Input className="h-8 text-sm" type="number" min={1} max={60000} step={100} value={draft.queueTimeoutMs} onChange={(e) => set("queueTimeoutMs", e.target.value)} />
              </Field>
              <Field label="在途并发上限 (0 = 不限)">
                <Input className="h-8 text-sm" type="number" min={0} value={draft.maxConcurrent} onChange={(e) => set("maxConcurrent", e.target.value)} />
              </Field>
              <Field label="Retry-After (s)">
                <Input className="h-8 text-sm" type="number" min={1} max={600} value={draft.retryAfterSeconds} onChange={(e) => set("retryAfterSeconds", e.target.value)} />
              </Field>
            </div>
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              超出准入上限的请求先进入等待队列（削峰填谷），等到令牌即放行；
              队列已满或等待超时回 429 并携带 Retry-After。并发上限是第二道闸门：
              慢接口堆积时进入速率正常也可能拖垮进程。
            </p>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="exempt" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">豁免路径</AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <PrefixListField
              items={draft.exemptPathPrefixes}
              onChange={(v) => set("exemptPathPrefixes", v)}
              placeholder="/api/v1/apps/critical-app"
            />
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              命中这些前缀的请求不参与整形（/healthz、/readyz、/api/ws 恒免，无需登记）。
            </p>
          </AccordionContent>
        </AccordionItem>
      </Accordion>

      {settings.updatedAt && (
        <p className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Database className="size-3" />
          最后修改 {fmtDate(settings.updatedAt)}
          {settings.updatedBy ? ` · 管理员 #${settings.updatedBy}` : ""}
        </p>
      )}
    </div>
  );
}

// ── 辅助组件 ──

function StatusCard({ icon: Icon, label, value, ok, sub, badgeClassName }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  ok?: boolean;
  sub?: string;
  badgeClassName?: string;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
      <Icon className={`size-5 shrink-0 ${ok ? "text-emerald-500" : "text-muted-foreground"}`} />
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        {badgeClassName ? (
          <Badge className={`mt-0.5 border-transparent text-xs font-semibold ${badgeClassName}`}>{value}</Badge>
        ) : (
          <div className="text-sm font-semibold truncate">{value}</div>
        )}
        {sub && <div className="text-[10px] text-muted-foreground truncate">{sub}</div>}
      </div>
    </div>
  );
}

function MetricCard({ label, value, sub, tone }: {
  label: string;
  value?: number;
  sub?: string;
  tone?: "emerald" | "amber" | "red";
}) {
  const toneClass =
    tone === "emerald" ? "text-emerald-600 dark:text-emerald-400"
    : tone === "amber" ? "text-amber-600 dark:text-amber-400"
    : tone === "red" ? "text-red-600 dark:text-red-400"
    : "";
  return (
    <div className="rounded-xl border px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={`text-lg font-semibold tabular-nums ${toneClass}`}>
        {typeof value === "number" ? value.toLocaleString() : "—"}
      </div>
      {sub && <div className="text-[10px] text-muted-foreground truncate">{sub}</div>}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}

function SwitchRow({ label, description, checked, onCheckedChange }: {
  label: string;
  description?: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-xl border px-4 py-3">
      <div className="min-w-0">
        <Label className="text-sm cursor-pointer">{label}</Label>
        {description && <p className="mt-0.5 text-[11px] text-muted-foreground">{description}</p>}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

// 路径前缀便捷输入：回车 / 加号添加，支持逗号与换行批量粘贴。
function PrefixListField({ items, onChange, placeholder }: {
  items: string[];
  onChange: (v: string[]) => void;
  placeholder: string;
}) {
  const [input, setInput] = useState("");

  function handleAdd() {
    const val = input.trim();
    if (!val || items.includes(val)) return;
    onChange([...items, val]);
    setInput("");
  }

  function handlePaste(e: React.ClipboardEvent) {
    const text = e.clipboardData.getData("text");
    if (text.includes("\n") || text.includes(",")) {
      e.preventDefault();
      const parsed = text.split(/[\n,]/).map((s) => s.trim()).filter(Boolean);
      onChange([...new Set([...items, ...parsed])]);
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <Input
          className="h-8 flex-1 font-mono text-sm"
          placeholder={placeholder}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); handleAdd(); } }}
          onPaste={handlePaste}
        />
        <Button type="button" size="sm" variant="outline" className="h-8 px-2.5" onClick={handleAdd} disabled={!input.trim()}>
          添加
        </Button>
      </div>
      {items.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {items.map((item, i) => (
            <span key={`${item}-${i}`} className="inline-flex items-center gap-1 rounded-lg border bg-muted/40 px-2.5 py-1 font-mono text-xs">
              {item}
              <button
                type="button"
                className="rounded p-0.5 text-muted-foreground hover:text-destructive"
                onClick={() => onChange(items.filter((_, idx) => idx !== i))}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
