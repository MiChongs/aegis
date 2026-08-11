"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  MemoryStick,
  Activity,
  Trash2,
  RefreshCw,
  AlertTriangle,
  TrendingUp,
  TrendingDown,
  Minus,
  Database,
  Layers,
  Settings2,
  Shield,
  Zap,
} from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import {
  getMemorySnapshot,
  forceGC,
  setGOGC,
  flushMemoryCache,
  type MemorySnapshot,
  type PoolStats,
  type MemoryAlert,
  type LeakIndicator,
} from "@/lib/api/system";
import { LoadingState } from "@/components/ui/data-state";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";

// ── 工具函数 ──

function fmtBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

function fmtPercent(value: number): string {
  return `${value.toFixed(1)}%`;
}

function fmtNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

// ── 进度条 ──

function ProgressBar({ value, color = "bg-primary" }: { value: number; color?: string }) {
  const pct = Math.min(100, Math.max(0, value));
  const barColor = pct > 90 ? "bg-destructive" : pct > 70 ? "bg-amber-500" : color;
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
      <div className={`h-full rounded-full transition-all duration-500 ${barColor}`} style={{ width: `${pct}%` }} />
    </div>
  );
}

// ── 趋势图标 ──

function TrendIcon({ trending }: { trending: string }) {
  if (trending === "rising") return <TrendingUp className="size-3.5 text-destructive" />;
  if (trending === "declining") return <TrendingDown className="size-3.5 text-emerald-500" />;
  return <Minus className="size-3.5 text-muted-foreground" />;
}

// ── 主组件 ──

export function MemoryPanel() {
  const accessToken = useAuthStore((s) => s.accessToken);
  const operator = useAuthStore((s) => s.operator);
  const queryClient = useQueryClient();
  const [gogcInput, setGogcInput] = useState("");

  const query = useQuery({
    queryKey: ["memory-snapshot", accessToken],
    queryFn: () => getMemorySnapshot(accessToken!),
    enabled: Boolean(accessToken && operator?.isSuperAdmin),
    refetchInterval: 15_000,
  });

  const gcMutation = useMutation({
    mutationFn: () => forceGC(accessToken!),
    onSuccess: () => {
      toast.success("强制 GC 已执行");
      queryClient.invalidateQueries({ queryKey: ["memory-snapshot"] });
    },
    onError: () => toast.error("GC 执行失败"),
  });

  const gogcMutation = useMutation({
    mutationFn: (value: number) => setGOGC(accessToken!, value),
    onSuccess: (data) => {
      toast.success(`GOGC 已更新: ${data.old} → ${data.new}`);
      setGogcInput("");
      queryClient.invalidateQueries({ queryKey: ["memory-snapshot"] });
    },
    onError: () => toast.error("GOGC 设置失败"),
  });

  const flushMutation = useMutation({
    mutationFn: () => flushMemoryCache(accessToken!),
    onSuccess: () => {
      toast.success("缓存已清空");
      queryClient.invalidateQueries({ queryKey: ["memory-snapshot"] });
    },
    onError: () => toast.error("缓存清空失败"),
  });

  if (!operator?.isSuperAdmin) return null;
  if (query.isLoading) return <LoadingState title="加载内存管理信息" />;
  if (!query.data) return null;

  const snap = query.data;
  const metrics = snap.metrics;
  const heapPct = metrics && metrics.sys > 0 ? (metrics.heapAlloc / metrics.sys) * 100 : 0;

  return (
    <div className="space-y-4 rounded-xl border p-5">
      {/* 标题栏 */}
      <div className="flex items-center gap-2">
        <MemoryStick className="size-5 text-muted-foreground" />
        <h2 className="text-base font-semibold">内存管理</h2>
        <div className="ml-auto flex items-center gap-2">
          {snap.gcTuner && (
            <Badge variant="outline" className="text-[10px]">
              GOGC {snap.gcTuner.gogc} · 调优 {fmtNumber(snap.gcTuner.tuneCount)} 次
            </Badge>
          )}
          <Badge variant="outline" className="text-[10px]">
            {snap.config.gcAutoTune ? "自动调优" : "手动模式"}
          </Badge>
        </div>
      </div>

      <Separator />

      {/* 核心指标概览 */}
      {metrics && (
        <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
          <MetricCard
            icon={MemoryStick}
            label="堆内存"
            value={fmtBytes(metrics.heapAlloc)}
            sub={`/ ${fmtBytes(metrics.sys)}`}
            progress={heapPct}
          />
          <MetricCard
            icon={Layers}
            label="堆对象"
            value={fmtNumber(metrics.heapObjects)}
            sub={`栈 ${fmtBytes(metrics.stackInuse)}`}
          />
          <MetricCard
            icon={Activity}
            label="Goroutines"
            value={String(metrics.goroutines)}
            sub={`GC ${metrics.numGC} 次`}
          />
          <MetricCard
            icon={Zap}
            label="累计分配"
            value={fmtBytes(metrics.totalAlloc)}
            sub={`暂停 ${(metrics.pauseTotalNs / 1e6).toFixed(1)}ms`}
          />
        </div>
      )}

      {/* 堆使用率 */}
      {metrics && (
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span>堆: {fmtBytes(metrics.heapInuse)} 使用 / {fmtBytes(metrics.heapIdle)} 空闲</span>
          <div className="flex-1"><ProgressBar value={heapPct} color="bg-sky-500" /></div>
          <span>{fmtPercent(heapPct)}</span>
        </div>
      )}

      {/* GC 调优器 + 操作按钮 */}
      <div className="flex flex-wrap items-center gap-2">
        <button
          onClick={() => gcMutation.mutate()}
          disabled={gcMutation.isPending}
          className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium hover:bg-muted transition-colors disabled:opacity-50"
        >
          <RefreshCw className={`size-3.5 ${gcMutation.isPending ? "animate-spin" : ""}`} />
          强制 GC
        </button>
        <button
          onClick={() => flushMutation.mutate()}
          disabled={flushMutation.isPending}
          className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium hover:bg-muted transition-colors disabled:opacity-50"
        >
          <Trash2 className="size-3.5" />
          清空缓存
        </button>
        <div className="flex items-center gap-1.5">
          <input
            type="number"
            min={10}
            max={500}
            placeholder="GOGC"
            value={gogcInput}
            onChange={(e) => setGogcInput(e.target.value)}
            className="w-20 rounded-lg border px-2 py-1.5 text-xs bg-transparent"
          />
          <button
            onClick={() => {
              const v = parseInt(gogcInput);
              if (v >= 10 && v <= 500) gogcMutation.mutate(v);
              else toast.error("GOGC 值必须在 10~500 之间");
            }}
            disabled={gogcMutation.isPending}
            className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium hover:bg-muted transition-colors disabled:opacity-50"
          >
            <Settings2 className="size-3.5" />
            设置 GOGC
          </button>
        </div>
      </div>

      {/* 对象池统计 */}
      {snap.pools.length > 0 && (
        <>
          <Separator />
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Database className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">对象池</span>
            </div>
            <div className="grid gap-2 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
              {snap.pools.map((pool) => (
                <PoolCard key={pool.name} pool={pool} />
              ))}
            </div>
          </div>
        </>
      )}

      {/* 缓存统计 */}
      <Separator />
      <div className="space-y-2">
        <div className="flex items-center gap-1.5">
          <Layers className="size-4 text-muted-foreground" />
          <span className="text-sm font-medium">本地缓存</span>
          <span className="text-[10px] text-muted-foreground ml-auto">
            {snap.cache.size} / {snap.cache.maxEntries} 条目
          </span>
        </div>
        <div className="grid gap-3 grid-cols-2 sm:grid-cols-4">
          <MiniStat label="命中" value={fmtNumber(snap.cache.hits)} />
          <MiniStat label="未命中" value={fmtNumber(snap.cache.misses)} />
          <MiniStat label="命中率" value={fmtPercent(snap.cache.hitRate)} />
          <MiniStat label="淘汰 / 过期" value={`${fmtNumber(snap.cache.evictions)} / ${fmtNumber(snap.cache.expired)}`} />
        </div>
        <ProgressBar value={(snap.cache.size / snap.cache.maxEntries) * 100} color="bg-violet-500" />
      </div>

      {/* 泄漏检测 */}
      {snap.leakReport && (
        <>
          <Separator />
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Shield className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">泄漏检测</span>
              {snap.leakReport.suspicious ? (
                <Badge variant="danger" className="text-[10px] ml-2">可疑泄漏</Badge>
              ) : (
                <Badge variant="outline" className="text-[10px] ml-2">正常</Badge>
              )}
            </div>
            <p className="text-xs text-muted-foreground">{snap.leakReport.summary}</p>
            <div className="grid gap-2 grid-cols-1 sm:grid-cols-2">
              {snap.leakReport.indicators.map((ind) => (
                <LeakIndicatorCard key={ind.name} indicator={ind} />
              ))}
            </div>
          </div>
        </>
      )}

      {/* 告警历史 */}
      {snap.alerts.length > 0 && (
        <>
          <Separator />
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <AlertTriangle className="size-4 text-amber-500" />
              <span className="text-sm font-medium">最近告警</span>
              <Badge variant="outline" className="text-[10px] ml-auto">{snap.alerts.length} 条</Badge>
            </div>
            <div className="max-h-40 overflow-y-auto space-y-1">
              {[...snap.alerts].reverse().slice(0, 10).map((alert, i) => (
                <AlertRow key={i} alert={alert} />
              ))}
            </div>
          </div>
        </>
      )}

      {/* 配置信息 */}
      <Separator />
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-[10px] text-muted-foreground">
        <span>监控间隔: {snap.config.monitorInterval}</span>
        <span>GC 调优间隔: {snap.config.gcTuneInterval}</span>
        <span>内存上限: {snap.config.memoryLimitMB > 0 ? `${snap.config.memoryLimitMB} MB` : "自动"}</span>
        <span>缓存 TTL: {snap.config.cacheTTL}</span>
        <span>泄漏窗口: {snap.config.leakWindow} 点</span>
      </div>
    </div>
  );
}

// ── 辅助组件 ──

function MetricCard({ icon: Icon, label, value, sub, progress }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  sub?: string;
  progress?: number;
}) {
  return (
    <div className="space-y-2 rounded-xl border px-4 py-3">
      <div className="flex items-center gap-1.5">
        <Icon className="size-4 text-muted-foreground" />
        <span className="text-xs font-medium">{label}</span>
      </div>
      <div className="text-lg font-semibold leading-none">
        {value}
        {sub && <span className="text-xs font-normal text-muted-foreground ml-1">{sub}</span>}
      </div>
      {progress !== undefined && <ProgressBar value={progress} />}
    </div>
  );
}

function PoolCard({ pool }: { pool: PoolStats }) {
  return (
    <div className="flex items-center justify-between rounded-lg border px-3 py-2">
      <div className="min-w-0">
        <div className="text-[10px] text-muted-foreground truncate">{pool.name}</div>
        <div className="text-xs font-semibold">
          命中率 {fmtPercent(pool.hitRate)}
        </div>
      </div>
      <div className="text-right text-[10px] text-muted-foreground shrink-0 ml-2">
        <div>获取 {fmtNumber(pool.gets)}</div>
        <div>归还 {fmtNumber(pool.puts)}</div>
      </div>
    </div>
  );
}

function LeakIndicatorCard({ indicator }: { indicator: LeakIndicator }) {
  return (
    <div className={`flex items-center gap-2.5 rounded-lg border px-3 py-2 ${indicator.suspectedLeak ? "border-destructive/50 bg-destructive/5" : ""}`}>
      <TrendIcon trending={indicator.trending} />
      <div className="min-w-0 flex-1">
        <div className="text-[10px] text-muted-foreground">{indicator.name}</div>
        <div className="text-xs font-semibold">
          {indicator.latestValue > 1024 * 1024 ? fmtBytes(indicator.latestValue) : fmtNumber(indicator.latestValue)}
          <span className="font-normal text-muted-foreground ml-1">
            ({indicator.deltaPercent > 0 ? "+" : ""}{indicator.deltaPercent.toFixed(1)}%)
          </span>
        </div>
        {indicator.alertMessage && (
          <div className="text-[10px] text-destructive mt-0.5">{indicator.alertMessage}</div>
        )}
      </div>
      <div className="text-[10px] text-muted-foreground text-right shrink-0">
        <div>{indicator.samples} 样本</div>
        <div>连升 {indicator.consecutiveUp}</div>
      </div>
    </div>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border px-3 py-2 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="text-xs font-semibold">{value}</div>
    </div>
  );
}

function AlertRow({ alert }: { alert: MemoryAlert }) {
  const time = new Date(alert.time).toLocaleTimeString("zh-CN", { hour12: false });
  return (
    <div className="flex items-center gap-2 text-xs">
      <AlertTriangle className={`size-3 shrink-0 ${alert.level === "critical" ? "text-destructive" : "text-amber-500"}`} />
      <span className="text-muted-foreground shrink-0">{time}</span>
      <span className="truncate">{alert.message}</span>
      <span className="text-muted-foreground shrink-0 ml-auto">{alert.value.toFixed(1)}</span>
    </div>
  );
}
