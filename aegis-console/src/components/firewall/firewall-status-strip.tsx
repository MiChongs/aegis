"use client";

import type { ReactNode } from "react";
import { AlertTriangle, Gauge, ListChecks, ListFilter, Lock, ShieldHalf, Timer } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { useFirewallRuntimeQuery } from "@/lib/firewall-hooks";
import { formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { PARANOIA_META } from "./firewall-kit";

type Tab = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/**
 * 页面顶部的运行态条：一眼看清「防火墙现在是什么状态」——拦截还是观察、规则集版本、
 * 生效规则数、阈值、本副本的检测量与延迟。每一格都能点进对应页签。
 * 计数是**本副本**自启动以来的（多副本各自独立），所以标注了「本副本」。
 */
export function FirewallStatusStrip({ onNavigate }: { onNavigate: (tab: Tab) => void }) {
  const runtime = useFirewallRuntimeQuery();
  const data = runtime.data;

  if (!data) {
    return <Skeleton className="h-[76px] w-full rounded-xl" />;
  }

  const mode = !data.enabled ? "off" : !data.wafActive ? "edge" : data.detectionOnly ? "observe" : "block";
  const modeMeta = {
    off: { label: "防火墙已关闭", hint: "所有检查均未生效", className: "bg-muted text-muted-foreground", ring: "bg-muted-foreground/40" },
    edge: { label: "仅入口防护", hint: "WAF 未开启：只有 IP、签名、自定义规则与限流", className: "bg-sky-500/10 text-sky-700 dark:text-sky-300", ring: "bg-sky-500" },
    observe: { label: "观察模式", hint: "WAF 只记录「本该拦截」，不拦", className: "bg-amber-500/10 text-amber-700 dark:text-amber-300", ring: "bg-amber-500" },
    block: { label: "拦截模式", hint: "越过阈值即拦截", className: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300", ring: "bg-emerald-500" }
  }[mode];
  const c = data.counters;

  return (
    <div className="space-y-2">
      <div className="flex flex-col gap-2 rounded-xl border bg-card p-2 lg:flex-row lg:items-stretch">
        <button
          type="button"
          onClick={() => onNavigate("waf")}
          className={cn("flex min-w-[220px] items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:opacity-90", modeMeta.className)}
        >
          <span className="relative flex size-2.5 shrink-0">
            {mode === "block" || mode === "observe" ? (
              <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", modeMeta.ring)} />
            ) : null}
            <span className={cn("relative inline-flex size-2.5 rounded-full", modeMeta.ring)} />
          </span>
          <span className="min-w-0">
            <span className="block text-sm font-semibold leading-5">{modeMeta.label}</span>
            <span className="block truncate text-xs opacity-80">{modeMeta.hint}</span>
          </span>
        </button>

        <div className="grid flex-1 grid-cols-2 gap-1 sm:grid-cols-3 xl:grid-cols-6">
          <Cell icon={ShieldHalf} label="规则集" onClick={() => onNavigate("waf")}>
            {data.crsVersion ? `CRS ${data.crsVersion}` : "未加载"}
            <Sub>v{data.rulesetVersion} · {data.activeRules}/{data.totalRules} 条生效</Sub>
          </Cell>
          <Cell icon={Gauge} label="入站阈值" onClick={() => onNavigate("waf")}>
            {data.threshold} 分
            <Sub>{PARANOIA_META[data.paranoia]?.label ?? `PL${data.paranoia}`}</Sub>
          </Cell>
          <Cell icon={ListFilter} label="自定义规则" onClick={() => onNavigate("rules")}>
            {data.customRules} 条
            <Sub>{data.ruleErrors.length ? `${data.ruleErrors.length} 条未生效` : "全部生效"}</Sub>
          </Cell>
          <Cell icon={ListChecks} label="WAF 例外" onClick={() => onNavigate("exclusions")}>
            {data.exclusions} 条
            <Sub>按路径与字段排除</Sub>
          </Cell>
          <Cell icon={Lock} label="本副本 · 解密后检测" onClick={() => onNavigate("events")}>
            {c.decrypted.toLocaleString()}
            <Sub>
              共检测 {c.inspected.toLocaleString()} · 拦截 {(c.wafBlocked + c.edgeBlocked).toLocaleString()}
            </Sub>
          </Cell>
          <Cell icon={Timer} label="本副本 · WAF 耗时" onClick={() => onNavigate("overview")}>
            {c.avgWafMicros >= 1000 ? `${(c.avgWafMicros / 1000).toFixed(1)} ms` : `${c.avgWafMicros} µs`}
            <Sub>热更新 #{data.reloadVersion} · {data.reloadedAt ? formatRelative(data.reloadedAt) : "—"}</Sub>
          </Cell>
        </div>
      </div>
      {data.ruleErrors.length > 0 ? (
        <button
          type="button"
          onClick={() => onNavigate("rules")}
          className="flex w-full items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-left text-xs text-amber-800 dark:text-amber-300"
        >
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0">
            有 {data.ruleErrors.length} 条自定义规则在本副本编译失败、没有生效：{data.ruleErrors[0]}
          </span>
        </button>
      ) : null}
      {c.wafErrors > 0 ? (
        <div className="flex items-center gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
          <AlertTriangle className="size-3.5 shrink-0" />
          本副本自启动以来有 {c.wafErrors} 次 WAF 处理失败（请求被拒为 50310），请查看服务日志「firewall coraza inspect failed」。
        </div>
      ) : null}
    </div>
  );
}

function Cell({ icon: Icon, label, children, onClick }: { icon: typeof Gauge; label: string; children: ReactNode; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="min-w-0 rounded-lg px-3 py-2 text-left transition-colors hover:bg-accent">
      <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Icon className="size-3" />
        <span className="truncate">{label}</span>
      </span>
      <span className="mt-0.5 block truncate text-sm font-semibold tabular-nums">{children}</span>
    </button>
  );
}

function Sub({ children }: { children: ReactNode }) {
  return <span className="block truncate text-[11px] font-normal text-muted-foreground">{children}</span>;
}
