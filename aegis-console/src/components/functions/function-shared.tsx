"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  Archive,
  Boxes,
  CalendarClock,
  ChevronDown,
  Coins,
  Database,
  Gamepad2,
  Gift,
  Globe,
  Info,
  Megaphone,
  Network,
  PenLine,
  ScrollText,
  Search,
  ShieldAlert,
  SquareTerminal,
  TrendingUp,
  User,
  UserRound,
  Workflow,
  Zap,
  type LucideIcon
} from "lucide-react";
import { ApiError } from "@/lib/api/client";
import {
  CAPABILITY_GROUP_LABELS,
  CAPABILITY_RISK_LABELS,
  type AppFunctionEffect,
  type AppFunctionLogEntry,
  type AppFunctionTestResult,
  type AppFunctionUsage,
  type FunctionCapability,
  type FunctionCapabilityGroup,
  type FunctionRuntimeLimits
} from "@/lib/api/app-functions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export function errorMessage(error: unknown) {
  return error instanceof ApiError
    ? error.message
    : error instanceof Error
      ? error.message
      : "操作失败";
}

export function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString("zh-CN") : "—";
}

/** 相对时间：列表里「3 分钟前」比一串完整时间戳好读，完整时间放 title */
export function formatRelative(value?: string | null) {
  if (!value) return "—";
  const diff = Date.now() - new Date(value).getTime();
  const future = diff < 0;
  const abs = Math.abs(diff);
  const minute = 60 * 1000;
  const hour = 60 * minute;
  const day = 24 * hour;
  let text: string;
  if (abs < minute) text = "不到 1 分钟";
  else if (abs < hour) text = `${Math.round(abs / minute)} 分钟`;
  else if (abs < day) text = `${Math.round(abs / hour)} 小时`;
  else if (abs < 30 * day) text = `${Math.round(abs / day)} 天`;
  else return formatTime(value);
  return future ? `${text}后` : `${text}前`;
}

export function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

export function formatDuration(ms: number) {
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)} s` : `${ms.toFixed(1)} ms`;
}

/**
 * 成功率在没有任何调用时显示「—」而不是 0%。
 *
 * 「没人调过」和「调了但全失败」会导出完全相反的结论，
 * 把前者显示成 0% 会让人以为函数坏了（与 Banner 点击率同一条约定）。
 */
export function formatRate(rate: number, total: number) {
  if (!total) return "—";
  return `${(rate * 100).toFixed(1)}%`;
}

export function prettyJson(value: unknown) {
  if (value === undefined) return "";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

// ── 调用来源 / 调用方 / 状态的统一口径 ──
// 概览、调用审计、触发器三处都显示这几个词，各写一份就会出现「同一条调用这里叫事件、那里叫钩子」。

export const SOURCE_LABELS: Record<string, string> = {
  api: "接入方",
  event: "事件",
  schedule: "定时",
  console: "控制台",
  function: "函数编排"
};

export const SOURCE_ICONS: Record<string, LucideIcon> = {
  api: Network,
  event: Zap,
  schedule: CalendarClock,
  console: SquareTerminal,
  function: Workflow
};

export const CALLER_LABELS: Record<string, string> = {
  user: "应用用户",
  app: "服务端密钥",
  admin: "管理员",
  event: "事件",
  schedule: "定时",
  function: "函数"
};

export const INVOCATION_STATUS: Record<string, { label: string; variant: "success" | "danger" | "warning" }> = {
  success: { label: "成功", variant: "success" },
  error: { label: "失败", variant: "danger" },
  running: { label: "执行中", variant: "warning" }
};

export const JOB_STATUS: Record<string, { label: string; variant: "success" | "danger" | "warning" | "info" }> = {
  pending: { label: "排队中", variant: "info" },
  running: { label: "执行中", variant: "warning" },
  succeeded: { label: "成功", variant: "success" },
  failed: { label: "失败", variant: "danger" }
};

export function SourceBadge({ source, className }: { source?: string; className?: string }) {
  if (!source) return null;
  const Icon = SOURCE_ICONS[source] ?? Network;
  return (
    <Badge variant="outline" size="sm" className={cn("gap-1 font-normal", className)}>
      <Icon className="size-3" />
      {SOURCE_LABELS[source] ?? source}
    </Badge>
  );
}

export function InvocationStatusBadge({ status }: { status: string }) {
  const meta = INVOCATION_STATUS[status];
  return (
    <Badge variant={meta?.variant ?? "outline"} size="sm">
      {meta?.label ?? status}
    </Badge>
  );
}

const RISK_VARIANT: Record<string, "success" | "warning" | "danger"> = {
  low: "success",
  medium: "warning",
  high: "danger"
};

export function RiskBadge({ risk }: { risk: string }) {
  return (
    <Badge variant={RISK_VARIANT[risk] ?? "secondary"} size="sm">
      {CAPABILITY_RISK_LABELS[risk] ?? risk}
    </Badge>
  );
}

export function StatTile({
  label,
  value,
  hint,
  tone
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: "default" | "danger" | "success";
}) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p
        className={cn(
          "mt-1 font-mono text-lg",
          tone === "danger" && "text-destructive",
          tone === "success" && "text-emerald-600 dark:text-emerald-400"
        )}
      >
        {value}
      </p>
      {hint ? <p className="mt-0.5 text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

// ── 能力分组 ──

/** 分组图标只是展示映射；分组本身（键、标题、说明、顺序）以后端目录为准 */
export const CAPABILITY_GROUP_ICONS: Record<string, LucideIcon> = {
  identity: UserRound,
  asset: Coins,
  rights: Gift,
  growth: TrendingUp,
  engage: Gamepad2,
  state: Database,
  reach: Megaphone,
  intel: Globe,
  audit: ScrollText,
  egress: Network,
  compose: Workflow,
  legacy: Archive
};

export function capabilityGroupIcon(key: string) {
  return CAPABILITY_GROUP_ICONS[key] ?? Boxes;
}

/**
 * 按后端下发的分组顺序整理能力。
 *
 * 目录里出现了分组表里没有的键（旧后端没下发 groups，或后端只加了能力没加分组），
 * 就按出现顺序补在末尾，标题退回兜底表 —— 宁可标题朴素，也不能让一项能力因此勾不上。
 */
export function resolveCapabilityGroups(
  capabilities: FunctionCapability[],
  groups?: FunctionCapabilityGroup[]
) {
  const ordered: FunctionCapabilityGroup[] = [...(groups ?? [])];
  const known = new Set(ordered.map((item) => item.key));
  for (const item of capabilities) {
    if (!known.has(item.group)) {
      known.add(item.group);
      ordered.push({ key: item.group, label: CAPABILITY_GROUP_LABELS[item.group] ?? item.group, hint: "" });
    }
  }
  return ordered;
}

type CapabilityFilter = "all" | "selected" | "read" | "write" | "high";

const CAPABILITY_FILTERS: Array<{ key: CapabilityFilter; label: string }> = [
  { key: "all", label: "全部" },
  { key: "selected", label: "已选" },
  { key: "read", label: "只读" },
  { key: "write", label: "写入" },
  { key: "high", label: "高风险" }
];

/**
 * 能力勾选器。分组、风险、说明全部来自后端目录 ——
 * 后端新增一种能力，这里零改动即自动出现。
 *
 * 能力从十几项长到四十多项之后，平铺一张网格就找不到东西了，所以有了：
 * 按分组分节（标题与说明来自 `catalog.groups`）、搜索（名称 / 键 / 调用形态）、
 * 按读写与风险过滤、组内全选，以及顶部那行「选了几项、几项高风险」的汇总 ——
 * 高风险能力藏在第六组里被顺手勾上，是这个界面最该防的事。
 *
 * 旧能力名（deprecated）只在**已经勾着**时才渲染：它们对新函数没有任何作用，
 * 但直接藏掉会让存量函数的设置页看起来少了一项，保存时又被悄悄清掉。
 */
export function CapabilityPicker({
  catalog,
  groups,
  selected,
  onChange,
  disabled
}: {
  catalog: FunctionCapability[];
  groups?: FunctionCapabilityGroup[];
  selected: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
}) {
  const [keyword, setKeyword] = useState("");
  const [filter, setFilter] = useState<CapabilityFilter>("all");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const visibleCatalog = useMemo(
    () => catalog.filter((item) => !item.deprecated || selectedSet.has(item.key)),
    [catalog, selectedSet]
  );

  const sections = useMemo(() => {
    const query = keyword.trim().toLowerCase();
    const matches = (item: FunctionCapability) => {
      if (filter === "selected" && !selectedSet.has(item.key)) return false;
      if (filter === "read" && item.mutating) return false;
      if (filter === "write" && !item.mutating) return false;
      if (filter === "high" && item.risk !== "high") return false;
      if (!query) return true;
      return [item.label, item.key, item.api, item.hint].some((text) =>
        text?.toLowerCase().includes(query)
      );
    };
    return resolveCapabilityGroups(visibleCatalog, groups)
      .map((group) => {
        const all = visibleCatalog.filter((item) => item.group === group.key);
        return { group, all, items: all.filter(matches) };
      })
      .filter((section) => section.items.length > 0);
  }, [visibleCatalog, groups, keyword, filter, selectedSet]);

  const summary = useMemo(() => {
    const picked = catalog.filter((item) => selectedSet.has(item.key));
    return {
      count: picked.length,
      high: picked.filter((item) => item.risk === "high").length,
      write: picked.filter((item) => item.mutating).length,
      user: picked.filter((item) => item.requiresUser).length
    };
  }, [catalog, selectedSet]);

  function toggle(key: string, checked: boolean) {
    onChange(checked ? [...selected, key] : selected.filter((item) => item !== key));
  }

  function toggleGroup(items: FunctionCapability[]) {
    // 只动当前筛选下看得见的那几项：全选一个被「只读」筛过的分组，
    // 不该把同组里看不见的写入能力也一并勾上
    const keys = items.filter((item) => !item.deprecated).map((item) => item.key);
    const allOn = keys.every((key) => selectedSet.has(key));
    if (allOn) onChange(selected.filter((key) => !keys.includes(key)));
    else onChange(Array.from(new Set([...selected, ...keys])));
  }

  if (!catalog.length) {
    return <p className="text-xs text-muted-foreground">能力目录加载中…</p>;
  }

  const searching = Boolean(keyword.trim()) || filter !== "all";

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-44 flex-1">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-7 text-xs"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder={`搜索 ${visibleCatalog.length} 项能力`}
          />
        </div>
        <div className="flex items-center gap-0.5 rounded-lg border p-0.5">
          {CAPABILITY_FILTERS.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setFilter(item.key)}
              className={cn(
                "rounded-md px-2 py-1 text-[11px] transition-colors",
                filter === item.key
                  ? "bg-muted font-medium text-foreground"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border bg-muted/30 px-3 py-2 text-xs">
        <span>
          已选 <span className="font-data font-medium tabular-nums">{summary.count}</span> 项
        </span>
        <span className="text-muted-foreground">
          写入 <span className="font-data tabular-nums text-foreground">{summary.write}</span>
        </span>
        <span className="text-muted-foreground">
          需用户身份 <span className="font-data tabular-nums text-foreground">{summary.user}</span>
        </span>
        <span className={cn("flex items-center gap-1", summary.high ? "text-destructive" : "text-muted-foreground")}>
          <ShieldAlert className="size-3.5" />
          高风险 <span className="font-data font-medium tabular-nums">{summary.high}</span>
        </span>
        {summary.count && !disabled ? (
          <button
            type="button"
            className="ml-auto text-[11px] text-muted-foreground hover:text-foreground"
            onClick={() => onChange([])}
          >
            全部清空
          </button>
        ) : null}
      </div>

      {!sections.length ? (
        <p className="rounded-lg border border-dashed py-8 text-center text-xs text-muted-foreground">
          无匹配的能力
        </p>
      ) : null}

      <div className="space-y-2">
        {sections.map(({ group, all, items }) => {
          const Icon = capabilityGroupIcon(group.key);
          const pickedInGroup = all.filter((item) => selectedSet.has(item.key)).length;
          const open = searching || !collapsed[group.key];
          const allOn = items.filter((item) => !item.deprecated).every((item) => selectedSet.has(item.key));
          return (
            <section key={group.key} className="rounded-xl border">
              <div className="flex items-center gap-2 px-3 py-2">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-2 text-left"
                  onClick={() => setCollapsed((current) => ({ ...current, [group.key]: !current[group.key] }))}
                  aria-expanded={open}
                >
                  <span
                    className={cn(
                      "grid size-6 shrink-0 place-items-center rounded-md",
                      pickedInGroup ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground"
                    )}
                  >
                    <Icon className="size-3.5" />
                  </span>
                  <span className="min-w-0">
                    <span className="flex items-center gap-1.5 text-xs font-medium">
                      {group.label}
                      <span className="font-data text-[11px] font-normal text-muted-foreground tabular-nums">
                        {pickedInGroup}/{all.length}
                      </span>
                    </span>
                    {group.hint ? (
                      <span className="block truncate text-[11px] text-muted-foreground">{group.hint}</span>
                    ) : null}
                  </span>
                  <ChevronDown
                    className={cn("ml-auto size-3.5 shrink-0 text-muted-foreground transition-transform", !open && "-rotate-90")}
                  />
                </button>
                {!disabled ? (
                  <Button size="xs" variant="ghost" className="shrink-0 text-[11px]" onClick={() => toggleGroup(items)}>
                    {allOn ? "清空本组" : "全选本组"}
                  </Button>
                ) : null}
              </div>

              {open ? (
                <div className="grid gap-1.5 border-t p-2 sm:grid-cols-2">
                  {items.map((item) => (
                    <CapabilityOption
                      key={item.key}
                      item={item}
                      checked={selectedSet.has(item.key)}
                      disabled={disabled}
                      onToggle={(value) => toggle(item.key, value)}
                    />
                  ))}
                </div>
              ) : null}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function CapabilityOption({
  item,
  checked,
  disabled,
  onToggle
}: {
  item: FunctionCapability;
  checked: boolean;
  disabled?: boolean;
  onToggle: (checked: boolean) => void;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-2.5 rounded-lg border p-2.5 text-xs transition-colors",
        checked
          ? item.risk === "high"
            ? "border-destructive/50 bg-destructive/5"
            : "border-primary/50 bg-primary/5"
          : "border-transparent bg-muted/30 hover:bg-muted/60",
        disabled && "pointer-events-none opacity-60"
      )}
    >
      <Checkbox
        className="mt-0.5"
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onToggle(value === true)}
      />
      <span className="min-w-0 flex-1 space-y-1">
        <span className="flex flex-wrap items-center gap-1">
          <span className="font-medium">{item.label}</span>
          {item.hint ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Info className="size-3 shrink-0 text-muted-foreground" aria-label="说明" />
              </TooltipTrigger>
              <TooltipContent className="max-w-72">{item.hint}</TooltipContent>
            </Tooltip>
          ) : null}
        </span>
        <code className="block truncate font-mono text-[10px] text-muted-foreground" title={item.api}>
          {item.api}
        </code>
        <span className="flex flex-wrap items-center gap-1">
          <RiskBadge risk={item.risk} />
          {item.mutating ? (
            <Badge variant="outline" size="sm" className="gap-1 font-normal">
              <PenLine className="size-3" />
              写入
            </Badge>
          ) : null}
          {item.requiresUser ? (
            <Badge variant="outline" size="sm" className="gap-1 font-normal">
              <User className="size-3" />
              需用户身份
            </Badge>
          ) : null}
          {item.deprecated ? (
            <Badge variant="outline" size="sm" className="font-normal">
              已废弃{item.replacedBy ? `，改用 ${item.replacedBy}` : ""}
            </Badge>
          ) : null}
        </span>
      </span>
    </label>
  );
}

/**
 * 副作用清单。
 *
 * 试跑产生的每一条都带「未执行」标记 —— 少了这个标记，一份看起来
 * 「发了 100 积分、开了 30 天会员」的清单会被当成真的发生过。
 * `timeline` 画成竖向时间线（调用详情用），按脚本发起的先后排列。
 */
export function EffectList({ effects, timeline }: { effects: AppFunctionEffect[]; timeline?: boolean }) {
  if (!effects.length) {
    return <p className="py-6 text-center text-xs text-muted-foreground">无副作用</p>;
  }
  if (timeline) {
    return (
      <ol className="relative space-y-2 pl-5 before:absolute before:inset-y-1 before:left-[7px] before:w-px before:bg-border">
        {effects.map((effect, index) => (
          <li key={`${effect.type}-${index}`} className="relative">
            <span
              aria-hidden
              className={cn(
                "absolute -left-5 top-1 grid size-3.5 place-items-center rounded-full border-2 bg-background",
                effect.simulated ? "border-amber-500" : "border-emerald-500"
              )}
            />
            <div className="flex flex-wrap items-center gap-1.5 text-xs">
              <span className="font-data text-[10px] text-muted-foreground tabular-nums">#{index + 1}</span>
              <code className="font-mono text-[11px] font-medium">{effect.type}</code>
              {effect.simulated ? (
                <Badge variant="warning" size="sm" className="gap-1">
                  <AlertTriangle className="size-3" />
                  未执行
                </Badge>
              ) : (
                <Badge variant="success" size="sm">
                  已执行
                </Badge>
              )}
            </div>
            <pre className="mt-1 max-h-40 overflow-auto rounded-md border bg-muted/40 p-1.5 font-mono text-[10px] text-muted-foreground">
              {prettyJson(effect.arguments)}
            </pre>
          </li>
        ))}
      </ol>
    );
  }
  return (
    <div className="space-y-1.5">
      {effects.map((effect, index) => (
        <div
          key={`${effect.type}-${index}`}
          className="flex items-start gap-2 rounded-lg border p-2 text-xs"
        >
          <Badge variant={effect.simulated ? "outline" : "secondary"} size="sm" className="shrink-0">
            {effect.type}
          </Badge>
          <code className="min-w-0 flex-1 break-all font-mono text-[11px] text-muted-foreground">
            {JSON.stringify(effect.arguments)}
          </code>
          {effect.simulated ? (
            <Badge variant="warning" size="sm" className="shrink-0 gap-1">
              <AlertTriangle className="size-3" />
              未执行
            </Badge>
          ) : null}
        </div>
      ))}
    </div>
  );
}

/**
 * 结构化日志。级别着色、可过滤；相对耗时是脚本里唯一的计时手段 ——
 * 沙箱没有 timer，「哪一步慢」只能靠日志之间的间隔看出来。
 */
export function LogList({ logs }: { logs: AppFunctionLogEntry[] }) {
  const [level, setLevel] = useState("all");
  const counts = useMemo(() => {
    const map: Record<string, number> = {};
    for (const entry of logs) map[entry.level] = (map[entry.level] ?? 0) + 1;
    return map;
  }, [logs]);
  const shown = level === "all" ? logs : logs.filter((entry) => entry.level === level);

  if (!logs.length) {
    return <p className="py-4 text-center text-xs text-muted-foreground">无日志</p>;
  }
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="flex items-center gap-0.5 border-b bg-muted/30 p-1">
        {["all", "info", "warn", "error"].map((key) =>
          key === "all" || counts[key] ? (
            <button
              key={key}
              type="button"
              onClick={() => setLevel(key)}
              className={cn(
                "rounded px-1.5 py-0.5 font-mono text-[10px] transition-colors",
                level === key ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground"
              )}
            >
              {key === "all" ? `全部 ${logs.length}` : `${key} ${counts[key]}`}
            </button>
          ) : null
        )}
      </div>
      <div className="max-h-72 space-y-px overflow-auto p-1.5">
        {shown.map((entry, index) => (
          <div
            key={index}
            className={cn(
              "flex gap-2 rounded px-1 py-0.5 font-mono text-[11px]",
              entry.level === "error" && "bg-destructive/5",
              entry.level === "warn" && "bg-amber-500/5"
            )}
          >
            <span className="w-14 shrink-0 text-right text-muted-foreground tabular-nums">
              {entry.elapsedMs.toFixed(1)}ms
            </span>
            <span
              className={cn(
                "w-9 shrink-0",
                entry.level === "error" && "text-destructive",
                entry.level === "warn" && "text-amber-600 dark:text-amber-400",
                entry.level === "info" && "text-sky-600 dark:text-sky-400",
                entry.level === "debug" && "text-muted-foreground"
              )}
            >
              {entry.level}
            </span>
            <span className="min-w-0 break-all">{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * SDK 额度用量：对着目录里的单次上限画进度条。
 * 只给一个数字的话，作者判断不出「12 次调用」离上限是远还是近。
 */
export function UsageMeters({
  usage,
  limits
}: {
  usage: AppFunctionUsage;
  limits?: FunctionRuntimeLimits;
}) {
  const rows = [
    { label: "SDK 调用", value: usage.calls, max: limits?.maxSdkCalls },
    { label: "写操作", value: usage.mutations, max: limits?.maxSdkMutations },
    { label: "出站请求", value: usage.fetches, max: limits?.maxSdkFetches }
  ];
  return (
    <div className="grid gap-2 sm:grid-cols-3">
      {rows.map((row) => {
        const ratio = row.max ? Math.min(1, row.value / row.max) : 0;
        return (
          <div key={row.label} className="space-y-1.5 rounded-lg border p-2.5">
            <div className="flex items-baseline justify-between gap-2 text-xs">
              <span className="text-muted-foreground">{row.label}</span>
              <span className="font-data tabular-nums">
                {row.value}
                {row.max ? <span className="text-muted-foreground"> / {row.max}</span> : null}
              </span>
            </div>
            <Progress
              value={ratio * 100}
              className={cn(
                "h-1.5 bg-muted",
                ratio >= 0.9
                  ? "[&>[data-slot=progress-indicator]]:bg-destructive"
                  : ratio >= 0.6
                    ? "[&>[data-slot=progress-indicator]]:bg-amber-500"
                    : "[&>[data-slot=progress-indicator]]:bg-emerald-500"
              )}
            />
          </div>
        );
      })}
    </div>
  );
}

export function JsonBlock({ value, className }: { value: unknown; className?: string }) {
  return (
    <pre
      className={cn(
        "max-h-64 overflow-auto rounded-lg border bg-muted/40 p-2.5 font-mono text-[11px] leading-relaxed",
        className
      )}
    >
      {prettyJson(value)}
    </pre>
  );
}

/** 小节标题：调用详情、重放结果、触发结果共用 */
export function DetailSection({
  title,
  action,
  children
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <h4 className="text-xs font-medium text-muted-foreground">{title}</h4>
        {action}
      </div>
      {children}
    </section>
  );
}

/**
 * 试跑形状的结果（重放回来的也是它）。判成功看 ok，不看接口是否报错；
 * 副作用一律是「未执行」—— 它们只被记下，没有发生。
 */
export function TestResultView({
  result,
  limits
}: {
  result: AppFunctionTestResult;
  limits?: FunctionRuntimeLimits;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <Badge variant={result.ok ? "success" : "danger"} size="sm">
          {result.ok ? "通过" : result.businessCode ? "被脚本拒绝" : "执行失败"}
        </Badge>
        <span className="font-data text-muted-foreground tabular-nums">{formatDuration(result.durationMs)}</span>
      </div>
      {result.error ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
          {result.businessCode ? (
            <span className="mr-1.5 font-mono text-[11px] text-muted-foreground">{result.businessCode}</span>
          ) : null}
          {result.error}
          {result.errorLine ? (
            <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">
              第 {result.errorLine} 行
            </span>
          ) : null}
        </div>
      ) : null}
      {result.output !== undefined && result.output !== null ? (
        <DetailSection title="返回值">
          <JsonBlock value={result.output} />
        </DetailSection>
      ) : null}
      <DetailSection title="额度">
        <UsageMeters
          usage={{ calls: result.sdkCalls, mutations: result.sdkMutations, fetches: result.sdkFetches }}
          limits={limits}
        />
      </DetailSection>
      <DetailSection title={`日志（${result.logs.length}）`}>
        <LogList logs={result.logs} />
      </DetailSection>
      <DetailSection title={`副作用（${result.effects.length}）`}>
        <EffectList effects={result.effects} timeline />
      </DetailSection>
    </div>
  );
}

/** 只在勾了高危能力时出现的提示条：默认状态下不该有常驻警告。 */
export function HighRiskNotice({
  catalog,
  selected
}: {
  catalog: FunctionCapability[];
  selected: string[];
}) {
  const risky = catalog.filter((item) => item.risk === "high" && selected.includes(item.key));
  if (!risky.length) return null;
  return (
    <div className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
      <ShieldAlert className="mt-0.5 size-3.5 shrink-0 text-destructive" />
      <span className="min-w-0">
        已启用高风险能力：
        <span className="font-medium">{risky.map((item) => item.label).join("、")}</span>
      </span>
    </div>
  );
}
