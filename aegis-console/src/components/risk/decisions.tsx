"use client";

import { useCallback, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import {
  AlertTriangle,
  Ban,
  ChevronDown,
  Copy,
  Eye,
  Fingerprint,
  Globe2,
  ListFilter,
  MonitorSmartphone,
  Puzzle,
  RefreshCw,
  RotateCcw,
  ScrollText,
  SearchX,
  ShieldCheck,
  ShieldX,
  SlidersHorizontal,
  UserRound,
  X,
  type LucideIcon,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { CountryFlag } from "@/components/ui/country-flag";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import type { DecisionDetail, DecisionRow, ListHit, RiskAction, TraceRule } from "@/lib/api/risk";
import { formatDateTime, formatWith, localDateKey } from "@/lib/datetime";
import { useRiskDashboard, useRiskDecision, useRiskDecisions, useRiskFeatures, useRiskModels, useRiskRules, useRiskScenes } from "@/lib/risk-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { useAppName } from "./kit/app-scope";
import { KindBadge } from "./kit/badges";
import {
  ACTION_EXPLAIN,
  ACTION_RESULT,
  LEVEL_WORD,
  decisionHeadline,
  decisionReason,
  latencyText,
  listEntrySentence,
  percent,
  placeOf,
  scoreWord,
  when,
} from "./kit/humanize";
import { ACTION_LABEL, ACTION_ORDER, DIM_LABEL, LIST_TYPE_LABEL, formatWindow } from "./kit/labels";
import { TraceView, decidedByText } from "./kit/trace-view";

/* ── 时间窗（总览与流水共用） ── */

export const RANGE_PRESETS = [
  { value: "1h", label: "1 小时", seconds: 3600 },
  { value: "24h", label: "24 小时", seconds: 86400 },
  { value: "7d", label: "7 天", seconds: 7 * 86400 },
  { value: "30d", label: "30 天", seconds: 30 * 86400 },
] as const;

export type RangeValue = (typeof RANGE_PRESETS)[number]["value"];

/**
 * 按分钟取整的「现在」。查询键里的起点由它推出：每分钟才变一次，
 * 既不会每次渲染都换键重查，窗口也不会停在打开页面那一刻。
 */
let clock = 0;
const floorMinute = (ms: number) => Math.floor(ms / 60_000) * 60_000;
function readClock() {
  if (!clock) clock = floorMinute(Date.now());
  return clock;
}
function subscribeClock(onChange: () => void) {
  const tick = () => {
    const next = floorMinute(Date.now());
    if (next !== clock) {
      clock = next;
      onChange();
    }
  };
  tick();
  const id = setInterval(tick, 15_000);
  return () => clearInterval(id);
}

/** 时间窗起点（毫秒时间戳字符串，后端同时接受 RFC3339 与毫秒） */
export function useRangeFrom(range: RangeValue) {
  const now = useSyncExternalStore(subscribeClock, readClock, readClock);
  const seconds = RANGE_PRESETS.find((r) => r.value === range)?.seconds ?? 86400;
  return String(now - seconds * 1000);
}

export function errorText(error: unknown, fallback = "读取失败") {
  return error instanceof Error && error.message ? error.message : fallback;
}

/* ── 处置与分数的视觉（总览、复核共用） ── */

export const ACTION_UI: Record<RiskAction, { icon: LucideIcon; text: string; soft: string; dot: string; color: string }> = {
  pass: { icon: ShieldCheck, text: "text-emerald-600 dark:text-emerald-400", soft: "bg-emerald-500/12", dot: "bg-emerald-500", color: "var(--color-emerald-500, #10b981)" },
  review: { icon: Eye, text: "text-sky-600 dark:text-sky-400", soft: "bg-sky-500/12", dot: "bg-sky-500", color: "var(--color-sky-500, #0ea5e9)" },
  captcha: { icon: Puzzle, text: "text-amber-600 dark:text-amber-400", soft: "bg-amber-500/12", dot: "bg-amber-500", color: "var(--color-amber-500, #f59e0b)" },
  block: { icon: ShieldX, text: "text-red-600 dark:text-red-400", soft: "bg-red-500/12", dot: "bg-red-500", color: "var(--color-red-500, #ef4444)" },
  ban: { icon: Ban, text: "text-red-700 dark:text-red-300", soft: "bg-red-700/15", dot: "bg-red-700", color: "var(--color-red-700, #b91c1c)" },
};

const uiOf = (a: string) => ACTION_UI[a as RiskAction] ?? ACTION_UI.pass;

export function ActionGlyph({ action, size = "default", className }: { action: RiskAction | string; size?: "sm" | "default" | "lg"; className?: string }) {
  const ui = uiOf(action);
  const Icon = ui.icon;
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center",
        size === "sm" && "size-7 rounded-lg",
        size === "default" && "size-9 rounded-xl",
        size === "lg" && "size-11 rounded-xl",
        ui.soft,
        ui.text,
        className
      )}
    >
      <Icon className={size === "lg" ? "size-5" : size === "sm" ? "size-3.5" : "size-4.5"} />
    </span>
  );
}

const SCORE_BANDS = [
  { from: 0, to: 30, track: "stroke-emerald-500/20", fill: "stroke-emerald-500", seg: "bg-emerald-500" },
  { from: 30, to: 60, track: "stroke-sky-500/20", fill: "stroke-sky-500", seg: "bg-sky-500" },
  { from: 60, to: 80, track: "stroke-amber-500/20", fill: "stroke-amber-500", seg: "bg-amber-500" },
  { from: 80, to: 100, track: "stroke-red-500/20", fill: "stroke-red-500", seg: "bg-red-500" },
] as const;

export const SCORE_TONE_TEXT = {
  success: "text-emerald-600 dark:text-emerald-400",
  info: "text-sky-600 dark:text-sky-400",
  warning: "text-amber-600 dark:text-amber-400",
  danger: "text-red-600 dark:text-red-400",
} as const;

/** 风险分仪表：半环分四段（低、中、高、极高），指针落在本次分数上 */
export function ScoreDial({ score, size = 184, className }: { score: number; size?: number; className?: string }) {
  const stroke = Math.round(size * 0.08);
  const r = size / 2 - stroke / 2 - 4;
  const cx = size / 2;
  const cy = size / 2;
  const height = Math.round(size / 2 + stroke / 2 + 6);
  const v = Math.max(0, Math.min(100, score));
  const pt = (s: number) => {
    const a = Math.PI * (1 - s / 100);
    return [cx + r * Math.cos(a), cy - r * Math.sin(a)] as const;
  };
  const arc = (a: number, b: number) => {
    const [x1, y1] = pt(a);
    const [x2, y2] = pt(b);
    return `M${x1.toFixed(2)},${y1.toFixed(2)} A${r},${r} 0 0 1 ${x2.toFixed(2)},${y2.toFixed(2)}`;
  };
  const gap = 0.9;
  const word = scoreWord(score);
  const [mx, my] = pt(v);

  return (
    <figure className={cn("flex flex-col items-center", className)}>
      <div className="relative" style={{ width: size, height }}>
        <svg width={size} height={height} viewBox={`0 0 ${size} ${height}`} aria-hidden className="overflow-visible">
          {SCORE_BANDS.map((b) => {
            const start = b.from === 0 ? 0 : b.from + gap;
            const end = b.to === 100 ? 100 : b.to - gap;
            const fillEnd = Math.min(v, end);
            return (
              <g key={b.from}>
                <path d={arc(start, end)} fill="none" strokeWidth={stroke} className={b.track} />
                {fillEnd > start + 0.2 ? <path d={arc(start, fillEnd)} fill="none" strokeWidth={stroke} className={b.fill} /> : null}
              </g>
            );
          })}
          <circle cx={mx} cy={my} r={stroke * 0.62} strokeWidth={2.5} className="fill-background stroke-foreground" />
        </svg>
        <div className="absolute inset-x-0 bottom-0 flex flex-col items-center">
          <span className="text-4xl leading-10 font-semibold tracking-tight tabular-nums">{score}</span>
          <span className={cn("text-xs font-medium", SCORE_TONE_TEXT[word.tone])}>{word.label}</span>
        </div>
      </div>
    </figure>
  );
}

/** 列表里的风险分：数字加四格刻度 */
export function ScorePill({ score, className }: { score: number; className?: string }) {
  const word = scoreWord(score);
  return (
    <div className={cn("flex w-14 shrink-0 flex-col items-end gap-1", className)} title={`风险分 ${score}，${word.label}`}>
      <span className={cn("text-lg leading-6 font-semibold tabular-nums", SCORE_TONE_TEXT[word.tone])}>{score}</span>
      <span aria-hidden className="flex gap-0.5">
        {SCORE_BANDS.map((b) => (
          <span key={b.from} className={cn("h-1 w-2.5 rounded-full", score > b.from || (b.from === 0 && score > 0) ? b.seg : "bg-muted")} />
        ))}
      </span>
    </div>
  );
}

/** 错误状态：说明 + 重试 */
export function ErrorState({ error, onRetry, className }: { error: unknown; onRetry?: () => void; className?: string }) {
  return (
    <div className={cn("flex flex-col items-center gap-3 px-4 py-10 text-center", className)}>
      <span className="grid size-10 place-items-center rounded-full bg-red-500/10 text-red-600 dark:text-red-400">
        <AlertTriangle className="size-5" />
      </span>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium">暂时无法读取</p>
        <p className="max-w-md text-xs leading-5 text-muted-foreground">{errorText(error)}</p>
      </div>
      {onRetry ? (
        <Button size="sm" variant="outline" onClick={onRetry}>
          <RefreshCw />
          重试
        </Button>
      ) : null}
    </div>
  );
}

/** 「今天」「昨天」「11月6日 周四」 */
export function dayHeading(iso: string) {
  const key = localDateKey(iso);
  const today = localDateKey(new Date());
  const yesterday = localDateKey(new Date(Date.now() - 86_400_000));
  if (key === today) return "今天";
  if (key === yesterday) return "昨天";
  return formatWith(iso, { month: "long", day: "numeric", weekday: "short" });
}

let regionNames: Intl.DisplayNames | null | undefined;
/** 两位国家代码 → 中文名 */
export function regionName(code?: string) {
  if (!code) return "";
  if (regionNames === undefined) {
    try {
      regionNames = new Intl.DisplayNames(["zh-CN"], { type: "region" });
    } catch {
      regionNames = null;
    }
  }
  try {
    return regionNames?.of(code.toUpperCase()) ?? code;
  } catch {
    return code;
  }
}

function copy(text: string, what = "内容") {
  navigator.clipboard.writeText(text).then(
    () => toast.success(`已复制${what}`),
    () => toast.error("复制失败")
  );
}

/* ── 流水 ── */

const PAGE_SIZE = 50;

interface Filters {
  scene: string;
  action: string;
  user: string;
  device: string;
  ip: string;
  account: string;
  rule: string;
  shadow: boolean;
  range: RangeValue;
}

const EMPTY: Filters = { scene: "", action: "", user: "", device: "", ip: "", account: "", rule: "", shadow: false, range: "24h" };
const TEXT_KEYS = ["user", "account", "ip", "device"] as const;

/** 地址里的 user：用户详情的「全部记录」跳过来时带上，作为初始筛选 */
function urlUserOf(raw: string | null) {
  const v = (raw ?? "").trim();
  return /^\d+$/.test(v) && Number(v) > 0 ? v : "";
}

export function RiskDecisions({ app }: { app?: number }) {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const urlUser = urlUserOf(params.get("user"));
  const [filters, setFilters] = useState<Filters>(() => ({ ...EMPTY, user: urlUser }));
  const [page, setPage] = useState(1);
  // 地址里的用户变了（从另一位用户详情再跳过来）就跟着换：渲染期比较上一次的值，不用 effect
  const [seenUrlUser, setSeenUrlUser] = useState(urlUser);
  if (seenUrlUser !== urlUser) {
    setSeenUrlUser(urlUser);
    setFilters((f) => ({ ...f, user: urlUser }));
    setPage(1);
  }
  const [openId, setOpenId] = useState<string | null>(null);
  const [moreOpen, setMoreOpen] = useState(false);
  const scenes = useRiskScenes();
  const rules = useRiskRules();
  const sceneName = useSceneName();
  const appName = useAppName();

  const set = <K extends keyof Filters>(key: K, value: Filters[K]) => {
    setFilters((f) => ({ ...f, [key]: value }));
    setPage(1);
  };

  /** 地址里带着 user 时一并去掉，刷新后不会又套上 */
  const dropUrlUser = () => {
    if (!params.get("user")) return;
    const q = new URLSearchParams(params.toString());
    q.delete("user");
    router.replace(`${pathname}?${q.toString()}`, { scroll: false });
  };
  const clearUser = () => {
    set("user", "");
    dropUrlUser();
  };
  const resetFilters = () => {
    setFilters((f) => ({ ...EMPTY, range: f.range }));
    setPage(1);
    dropUrlUser();
  };

  // 四个文本框合成一个字符串防抖：传对象的话每次渲染都是新引用，计时器永远停不下来
  const [dUser = "", dDevice = "", dIp = "", dAccount = ""] = useDebouncedValue(
    [filters.user, filters.device, filters.ip, filters.account].join("\u0000")
  ).split("\u0000");
  const from = useRangeFrom(filters.range);
  const userId = Number(dUser.trim());
  const query = useRiskDecisions({
    app,
    scene: filters.scene || undefined,
    action: filters.action || undefined,
    user: Number.isFinite(userId) && userId > 0 ? userId : undefined,
    device: dDevice.trim() || undefined,
    ip: dIp.trim() || undefined,
    account: dAccount.trim() || undefined,
    rule: filters.rule || undefined,
    shadow: filters.shadow || undefined,
    from,
    page,
    limit: PAGE_SIZE,
  });
  // 看板接口支持应用、场景、用户；分布条跟着这三个条件走
  const dashboard = useRiskDashboard({
    app,
    from,
    scene: filters.scene || undefined,
    user: Number.isFinite(userId) && userId > 0 ? userId : undefined,
  });

  const rows = useMemo(() => query.data?.list ?? [], [query.data]);
  const total = query.data?.total ?? 0;
  const userFilter = Number.isFinite(userId) && userId > 0 ? String(userId) : "";
  const textCount = TEXT_KEYS.filter((k) => filters[k].trim()).length + (filters.rule ? 1 : 0);
  // 看板不支持的条件：登录名、IP、设备、规则、影子评估（看板只统计正式评估）。带了这些，分布条的次数与列表对不上
  const unsupported = !!(dAccount.trim() || dIp.trim() || dDevice.trim() || filters.rule || filters.shadow);
  const dirty = JSON.stringify({ ...filters, range: "" }) !== JSON.stringify({ ...EMPTY, range: "" });
  const rangeLabel = RANGE_PRESETS.find((r) => r.value === filters.range)?.label ?? "24 小时";

  // 处置分布：时间窗内（已按场景、用户过滤）各处置的次数，兼作处置筛选
  const mix = useMemo(() => {
    const out: Record<RiskAction, number> = { pass: 0, review: 0, captcha: 0, block: 0, ban: 0 };
    const d = dashboard.data;
    if (!d || dashboard.isPlaceholderData) return null;
    for (const kv of d.byAction ?? []) if (kv.key in out) out[kv.key as RiskAction] += kv.count;
    return out;
  }, [dashboard.data, dashboard.isPlaceholderData]);

  const sceneRules = useMemo(
    () => (rules.data ?? []).filter((r) => !filters.scene || r.scene === filters.scene || r.scene === "*"),
    [rules.data, filters.scene]
  );

  const groups = useMemo(() => {
    const out: Array<{ key: string; label: string; rows: DecisionRow[] }> = [];
    for (const row of rows) {
      const key = localDateKey(row.ts);
      let g = out[out.length - 1];
      if (!g || g.key !== key) {
        g = { key, label: dayHeading(row.ts), rows: [] };
        out.push(g);
      }
      g.rows.push(row);
    }
    return out;
  }, [rows]);

  const headline = (() => {
    const scope = filters.scene ? `${sceneName(filters.scene)}场景` : "";
    const what = filters.action ? `${ACTION_RESULT[filters.action as RiskAction]}的` : "";
    const noun = filters.shadow ? "影子评估" : "决策";
    return `过去 ${rangeLabel}${scope ? `，${scope}` : ""}共有 ${total.toLocaleString("zh-CN")} 条${what}${noun}`;
  })();
  const subline = (() => {
    if (unsupported) return "已按查找条件筛选。";
    const who = userFilter ? `仅看用户 #${userFilter}，` : "";
    if (!mix) return userFilter ? `仅看用户 #${userFilter} 的决策。` : "";
    const sum = ACTION_ORDER.reduce((s, a) => s + mix[a], 0);
    if (!sum) return userFilter ? `仅看用户 #${userFilter} 的决策。` : "";
    const blocked = mix.block + mix.ban;
    const challenged = mix.review + mix.captcha;
    if (!blocked && !challenged) return `${who}这段时间的请求全部放行。`;
    return who + [blocked ? `拦截 ${blocked.toLocaleString("zh-CN")} 次，占 ${percent(blocked, sum)}` : "", challenged ? `要求验证或复核 ${challenged.toLocaleString("zh-CN")} 次` : ""]
      .filter(Boolean)
      .join("，")
      .concat("。");
  })();

  return (
    <TabStack>
      <section className="flex min-w-0 flex-col gap-4 rounded-2xl border border-border bg-card p-4 sm:p-5">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="flex min-h-12 min-w-0 flex-col gap-1">
            {query.isLoading ? (
              <>
                <Skeleton className="h-6 w-72 max-w-full" />
                <Skeleton className="h-4 w-48 max-w-full" />
              </>
            ) : (
              <>
                <h3 className="text-base leading-6 font-semibold tracking-tight">{headline}</h3>
                <p className="min-h-5 text-sm leading-5 text-muted-foreground">{subline}</p>
              </>
            )}
          </div>
          <Segmented value={filters.range} options={RANGE_PRESETS} onChange={(v) => set("range", v)} ariaLabel="时间范围" />
        </div>

        {/* 带了看板不支持的条件时，分布条的次数与列表对不上，只保留筛选作用 */}
        <ActionStrip mix={unsupported ? null : mix} value={filters.action} onChange={(v) => set("action", v)} />

        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Select value={filters.scene || "all"} onValueChange={(v) => {
              setFilters((f) => ({ ...f, scene: v === "all" ? "" : v, rule: "" }));
              setPage(1);
            }}>
            <SelectTrigger size="sm" className="h-8 w-40">
              <SelectValue placeholder="全部场景" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部场景</SelectItem>
              {(scenes.data ?? []).map((s) => (
                <SelectItem key={s.key} value={s.key}>
                  {s.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button size="sm" variant={moreOpen || textCount ? "secondary" : "outline"} className="h-8" onClick={() => setMoreOpen((o) => !o)} aria-expanded={moreOpen}>
            <SlidersHorizontal />
            精确查找
            {textCount ? <span className="rounded-full bg-foreground px-1.5 text-[11px] leading-4 text-background tabular-nums">{textCount}</span> : null}
          </Button>
          <Label className="inline-flex h-8 items-center gap-2 px-1 text-xs font-normal text-muted-foreground">
            <Switch checked={filters.shadow} onCheckedChange={(v) => set("shadow", v)} />
            只看影子评估
          </Label>
          {userFilter ? (
            <span className="inline-flex h-8 items-center gap-1 rounded-lg border border-sky-500/40 bg-sky-500/8 pr-1 pl-2.5 text-xs text-sky-700 dark:text-sky-300">
              <UserRound className="size-3.5" />
              仅看用户 #{userFilter}
              <button
                type="button"
                aria-label="清除用户筛选"
                onClick={clearUser}
                className="grid size-6 place-items-center rounded-md transition-colors hover:bg-sky-500/15"
              >
                <X className="size-3.5" />
              </button>
            </span>
          ) : null}
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto h-8"
            disabled={!dirty}
            onClick={resetFilters}
          >
            <RotateCcw />
            清除筛选
          </Button>
        </div>

        {moreOpen ? (
          <div className="grid min-w-0 grid-cols-1 gap-2 rounded-xl bg-muted/40 p-3 sm:grid-cols-2 xl:grid-cols-5">
            <Input className="h-8 bg-background" inputMode="numeric" placeholder="用户编号" value={filters.user} onChange={(e) => set("user", e.target.value)} />
            <Input className="h-8 bg-background" placeholder="登录名" value={filters.account} onChange={(e) => set("account", e.target.value)} />
            <Input className="h-8 bg-background font-mono text-xs" placeholder="IP 地址" value={filters.ip} onChange={(e) => set("ip", e.target.value)} />
            <Input className="h-8 bg-background font-mono text-xs" placeholder="设备标识" value={filters.device} onChange={(e) => set("device", e.target.value)} />
            <Select value={filters.rule || "all"} onValueChange={(v) => set("rule", v === "all" ? "" : v)}>
              <SelectTrigger size="sm" className="h-8 w-full bg-background">
                <SelectValue placeholder="命中的规则" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">不限规则</SelectItem>
                {sceneRules.map((r) => (
                  <SelectItem key={`${r.scene}-${r.key}`} value={r.key}>
                    {r.name || r.key}
                    {!filters.scene && r.scene !== "*" ? <span className="ml-1 text-xs text-muted-foreground">{sceneName(r.scene)}</span> : null}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}
      </section>

      <Panel
        icon={ScrollText}
        tone="sky"
        title="决策记录"
        aside={
          query.isFetching && !query.isLoading ? (
            <span className="text-xs text-muted-foreground">正在刷新</span>
          ) : total > 0 ? (
            <span className="text-xs text-muted-foreground tabular-nums">
              第 {((page - 1) * PAGE_SIZE + 1).toLocaleString("zh-CN")} 至 {Math.min(page * PAGE_SIZE, total).toLocaleString("zh-CN")} 条
            </span>
          ) : null
        }
        bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
      >
        {query.isError && !query.data ? (
          <ErrorState error={query.error} onRetry={() => query.refetch()} className="border-t" />
        ) : query.isLoading ? (
          <DecisionListSkeleton />
        ) : rows.length === 0 ? (
          <div className="flex flex-col items-center border-t pb-6">
            <CardEmpty
              icon={dirty ? SearchX : ScrollText}
              title={dirty ? "没有符合条件的决策" : "这段时间没有决策"}
              description={dirty ? "可以放宽筛选条件或扩大时间范围" : "场景发布并接入请求后，评估结果会出现在这里"}
            />
            {dirty ? (
              <Button size="sm" variant="outline" onClick={resetFilters}>
                <RotateCcw />
                清除筛选
              </Button>
            ) : null}
          </div>
        ) : (
          <>
            <div className={cn("border-t transition-opacity", query.isFetching && "opacity-70")}>
              {groups.map((g) => (
                <div key={g.key}>
                  <div className="flex items-center gap-2 border-b bg-muted/30 px-4 py-1.5 text-xs text-muted-foreground sm:px-5">
                    <span className="font-medium text-foreground">{g.label}</span>
                    <span className="tabular-nums">{g.rows.length} 条</span>
                  </div>
                  <ul className="divide-y">
                    {g.rows.map((row) => (
                      <DecisionItem key={row.id} row={row} sceneName={sceneName} appName={app ? undefined : appName} onOpen={() => setOpenId(row.id)} />
                    ))}
                  </ul>
                </div>
              ))}
            </div>
            <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} total={total} unit="条" onPage={setPage} />
          </>
        )}
      </Panel>

      <DecisionDetailDialog id={openId} open={!!openId} onOpenChange={(o) => !o && setOpenId(null)} />
    </TabStack>
  );
}

/** 处置分布条：一条按次数分段的横条，下面是可点选的处置筛选 */
function ActionStrip({ mix, value, onChange }: { mix: Record<RiskAction, number> | null; value: string; onChange: (v: string) => void }) {
  const sum = mix ? ACTION_ORDER.reduce((s, a) => s + mix[a], 0) : 0;
  return (
    <div className="flex min-w-0 flex-col gap-2.5">
      <div className="flex h-2.5 w-full min-w-0 gap-0.5 overflow-hidden rounded-full bg-muted">
        {mix && sum > 0
          ? ACTION_ORDER.filter((a) => mix[a] > 0).map((a) => (
              <span
                key={a}
                className={cn("h-full transition-opacity", ACTION_UI[a].dot, value && value !== a && "opacity-25")}
                style={{ flexGrow: Math.max(mix[a] / sum, 0.012), flexBasis: 0 }}
              />
            ))
          : null}
      </div>
      <div className="flex min-w-0 flex-wrap gap-1.5" role="group" aria-label="按处置筛选">
        <StripChip active={!value} label="全部" count={mix ? sum : undefined} onClick={() => onChange("")} />
        {ACTION_ORDER.map((a) => (
          <StripChip
            key={a}
            active={value === a}
            dot={ACTION_UI[a].dot}
            label={ACTION_LABEL[a]}
            count={mix ? mix[a] : undefined}
            onClick={() => onChange(value === a ? "" : a)}
          />
        ))}
      </div>
    </div>
  );
}

function StripChip({ active, label, count, dot, onClick }: { active: boolean; label: string; count?: number; dot?: string; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-xs transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        active ? "border-foreground/20 bg-foreground/5 font-medium text-foreground" : "border-border text-muted-foreground hover:bg-accent/60 hover:text-foreground"
      )}
    >
      {dot ? <span aria-hidden className={cn("size-2 rounded-[3px]", dot)} /> : <ListFilter className="size-3.5" />}
      {label}
      {count !== undefined ? <span className="tabular-nums text-muted-foreground">{count.toLocaleString("zh-CN")}</span> : null}
    </button>
  );
}

function DecisionItem({
  row,
  sceneName,
  appName,
  onOpen,
}: {
  row: DecisionRow;
  sceneName: (k: string) => string;
  appName?: (id?: number) => string;
  onOpen: () => void;
}) {
  const t = when(row.ts);
  const place = [regionName(row.country), row.city].filter(Boolean).join(" ") || placeOf(row);
  const hits = row.hits?.length ?? 0;
  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        className="flex w-full min-w-0 items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none sm:px-5"
      >
        <ActionGlyph action={row.action} />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 truncate text-sm font-medium">{decisionHeadline(row, sceneName)}</span>
            {row.shadow ? <Badge variant="secondary" size="sm">影子</Badge> : null}
          </div>
          <p className="min-w-0 truncate text-xs leading-5 text-muted-foreground">{decisionReason(row)}</p>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] leading-4 text-muted-foreground">
            <time dateTime={row.ts} title={t.absolute} className="tabular-nums">
              {t.relative}
            </time>
            {place ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                {row.country ? <CountryFlag code={row.country} size={12} /> : null}
                <span className="truncate">{place}</span>
              </span>
            ) : null}
            {row.ip ? <span className="font-mono">{row.ip}</span> : null}
            {appName ? <span className="truncate">{appName(row.appId)}</span> : null}
            {hits ? <span>命中 {hits} 条规则</span> : null}
          </div>
        </div>
        <ScorePill score={row.score} />
      </button>
    </li>
  );
}

function DecisionListSkeleton() {
  return (
    <div className="border-t" aria-hidden>
      <div className="border-b bg-muted/30 px-4 py-1.5 sm:px-5">
        <Skeleton className="h-4 w-16" />
      </div>
      <div className="divide-y">
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 px-4 py-3 sm:px-5">
            <Skeleton className="size-9 rounded-xl" />
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <Skeleton className="h-4 w-2/5" />
              <Skeleton className="h-3.5 w-3/5" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <div className="flex w-14 flex-col items-end gap-1.5">
              <Skeleton className="h-5 w-8" />
              <Skeleton className="h-1 w-12" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/** 场景键 → 名称 */
export function useSceneName() {
  const scenes = useRiskScenes();
  const map = useMemo(() => new Map((scenes.data ?? []).map((s) => [s.key, s.name])), [scenes.data]);
  return useCallback((key: string) => map.get(key) ?? key, [map]);
}

export function Subject({ userId, account }: { userId: number; account: string }) {
  if (!userId && !account) return <span className="text-muted-foreground">匿名访客</span>;
  return (
    <div className="flex min-w-0 flex-col leading-tight">
      {account ? <span className="truncate text-sm">{account}</span> : null}
      {userId ? <span className={cn("truncate tabular-nums", account ? "text-xs text-muted-foreground" : "text-sm")}>用户 #{userId}</span> : null}
    </div>
  );
}

/* ── 决策详情 ── */

export function DecisionDetailDialog({ id, open, onOpenChange }: { id: string | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  const detail = useRiskDecision(open ? id : null);
  const sceneName = useSceneName();
  const appName = useAppName();
  const d = detail.data?.decision;
  const t = when(d?.ts);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] gap-5 overflow-y-auto sm:max-w-4xl">
        <DialogHeader className="min-w-0">
          {d ? (
            <>
              <DialogTitle className="pr-6 text-lg leading-7">{decisionHeadline(d, sceneName)}</DialogTitle>
              <DialogDescription className="flex flex-wrap gap-x-4 gap-y-0.5 tabular-nums">
                <span>{t.absolute}</span>
                <span>{t.relative}</span>
                <span>{appName(d.appId)}</span>
              </DialogDescription>
            </>
          ) : (
            <>
              <DialogTitle>决策详情</DialogTitle>
              <DialogDescription>{detail.isError ? "读取失败" : "正在读取"}</DialogDescription>
            </>
          )}
        </DialogHeader>
        {detail.isError ? (
          <ErrorState error={detail.error} onRetry={() => detail.refetch()} className="rounded-xl border" />
        ) : !detail.data ? (
          <DetailSkeleton />
        ) : (
          <DecisionDetailBody detail={detail.data} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function DetailSkeleton() {
  return (
    <div className="flex flex-col gap-6" aria-hidden>
      <div className="grid gap-5 rounded-xl border p-4 sm:grid-cols-[200px_minmax(0,1fr)] sm:p-5">
        <Skeleton className="mx-auto h-[104px] w-[184px] rounded-t-full" />
        <div className="flex flex-col gap-2.5">
          <Skeleton className="h-7 w-32" />
          <Skeleton className="h-4 w-64 max-w-full" />
          <Skeleton className="h-4 w-48 max-w-full" />
          <Skeleton className="mt-2 h-3 w-full rounded-full" />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <Skeleton className="h-4 w-20" />
        <Skeleton className="h-12 w-full rounded-lg" />
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-32 rounded-xl" />
        ))}
      </div>
    </div>
  );
}

const pick = (obj: unknown, path: string): unknown =>
  path.split(".").reduce<unknown>((o, k) => (o && typeof o === "object" ? (o as Record<string, unknown>)[k] : undefined), obj);

function DecisionDetailBody({ detail }: { detail: DecisionDetail }) {
  const d = detail.decision;
  const trace = detail.trace;
  const [traceOpen, setTraceOpen] = useState(false);
  const reasons = (d.reasons ?? []).filter(Boolean);
  const ruleHits = (trace?.rules ?? []).filter((r) => r.hit).sort((a, b) => Number(a.mode === "observe") - Number(b.mode === "observe") || b.score - a.score);
  const listHits = trace?.listHits ?? [];
  const commands = (trace?.commands ?? []).filter((c): c is Record<string, unknown> => !!c && typeof c === "object");
  const ui = uiOf(d.action);

  return (
    <div className="flex min-w-0 flex-col gap-6">
      {/* 结论 */}
      <section className="grid min-w-0 gap-5 rounded-xl border border-border p-4 sm:grid-cols-[200px_minmax(0,1fr)] sm:items-center sm:p-5">
        <ScoreDial score={d.score} className="mx-auto" />
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <ActionGlyph action={d.action} size="sm" />
              <span className={cn("text-xl leading-7 font-semibold tracking-tight", ui.text)}>{ACTION_RESULT[d.action] ?? d.action}</span>
              {d.shadow ? <Badge variant="secondary">影子评估，未实际生效</Badge> : null}
            </div>
            <p className="text-sm leading-6 text-muted-foreground">
              {ACTION_EXPLAIN[d.action] ?? ""}。{decidedByText(trace?.decidedBy)}。
            </p>
          </div>
          <dl className="flex min-w-0 flex-wrap gap-x-6 gap-y-2">
            <Fact label="风险等级" value={LEVEL_WORD[d.level] ?? d.level} />
            <Fact label="评估用时" value={latencyText(d.latencyUs)} />
            {d.release ? <Fact label="规则版本" value={d.release} mono /> : null}
          </dl>
          <ScoreComposition rules={trace?.rules ?? null} score={d.score} />
        </div>
      </section>

      {/* 为什么 */}
      <DetailSection index={1} title="为什么是这个结论">
        {reasons.length ? (
          <ol className="flex flex-col gap-2">
            {reasons.map((r, i) => (
              <li key={i} className="flex min-w-0 gap-3 rounded-lg bg-muted/40 px-3 py-2.5 text-sm leading-6">
                <span className="grid size-5 shrink-0 translate-y-0.5 place-items-center rounded-full bg-amber-500/15 text-[11px] font-medium text-amber-700 tabular-nums dark:text-amber-300">
                  {i + 1}
                </span>
                <span className="min-w-0">{r}</span>
              </li>
            ))}
          </ol>
        ) : (
          <p className="rounded-lg bg-muted/40 px-3 py-2.5 text-sm text-muted-foreground">{decisionReason(d)}。</p>
        )}
      </DetailSection>

      {/* 环境 */}
      <DetailSection index={2} title="这次请求的环境">
        <EnvironmentGrid detail={detail} />
      </DetailSection>

      {/* 命中 */}
      <DetailSection index={3} title="命中的规则与名单">
        <div className="flex min-w-0 flex-col gap-4">
          {ruleHits.length ? (
            <ul className="flex min-w-0 flex-col divide-y rounded-xl border">
              {ruleHits.map((r) => (
                <RuleHitLine key={r.key} rule={r} />
              ))}
            </ul>
          ) : (
            <p className="rounded-lg bg-muted/40 px-3 py-2.5 text-sm text-muted-foreground">
              {(d.hits ?? []).length ? `命中 ${d.hits.length} 条规则，未记录明细。` : "没有命中任何规则。"}
            </p>
          )}
          {listHits.length ? (
            <div className="flex min-w-0 flex-col gap-2">
              <h5 className="text-xs font-medium text-muted-foreground">名单</h5>
              <ListHitTable hits={listHits} />
            </div>
          ) : null}
          {commands.length ? (
            <div className="flex min-w-0 flex-col gap-2">
              <h5 className="text-xs font-medium text-muted-foreground">随之执行的处置</h5>
              <ul className="flex min-w-0 flex-col gap-1.5">
                {commands.map((c, i) => (
                  <li key={i} className="flex min-w-0 items-center gap-2.5 rounded-lg border border-red-500/25 bg-red-500/5 px-3 py-2 text-sm">
                    <Ban className="size-4 shrink-0 text-red-600 dark:text-red-400" />
                    <span className="min-w-0 break-all">{commandSentence(c)}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          <SignalFeatures features={detail.features ?? {}} models={detail.models ?? {}} />
        </div>
      </DetailSection>

      {/* 轨迹 */}
      <DetailSection index={4} title="完整轨迹">
        {trace ? (
          <Collapsible open={traceOpen} onOpenChange={setTraceOpen}>
            <CollapsibleTrigger className="flex w-full items-center gap-2 rounded-xl border border-dashed px-4 py-3 text-left text-sm text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground">
              <ChevronDown className={cn("size-4 transition-transform", traceOpen && "rotate-180")} />
              {traceOpen ? "收起评估环节" : `展开评估环节、耗时与全部 ${(trace.rules ?? []).length} 条规则`}
            </CollapsibleTrigger>
            <CollapsibleContent className="pt-4">
              <TraceView trace={trace} />
            </CollapsibleContent>
          </Collapsible>
        ) : (
          <p className="rounded-lg bg-muted/40 px-3 py-2.5 text-sm text-muted-foreground">这次评估没有记录轨迹。</p>
        )}
      </DetailSection>

      <footer className="flex min-w-0 flex-wrap items-center gap-2 border-t pt-4 text-xs text-muted-foreground">
        <span>决策编号</span>
        <span className="min-w-0 truncate font-mono">{d.id}</span>
        <Button size="icon" variant="ghost" className="size-7" aria-label="复制决策编号" onClick={() => copy(d.id, "决策编号")}>
          <Copy className="size-3.5" />
        </Button>
      </footer>
    </div>
  );
}

function DetailSection({ index, title, children }: { index: number; title: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h4 className="flex items-center gap-2 text-sm font-semibold">
        <span className="grid size-5 place-items-center rounded-md bg-muted text-[11px] font-medium text-muted-foreground tabular-nums">{index}</span>
        {title}
      </h4>
      {children}
    </section>
  );
}

/* ── 分数构成 ── */

const DIM_COLOR: Record<string, string> = {
  network: "bg-sky-500",
  device: "bg-violet-500",
  account: "bg-amber-500",
  behavior: "bg-rose-500",
  geo: "bg-teal-500",
  client: "bg-indigo-500",
  list: "bg-slate-500",
  graph: "bg-orange-500",
  model: "bg-fuchsia-500",
  custom: "bg-zinc-500",
};

function ScoreComposition({ rules, score }: { rules: TraceRule[] | null; score: number }) {
  const parts = useMemo(() => {
    const map = new Map<string, { dim: string; score: number; count: number }>();
    for (const r of rules ?? []) {
      if (!r.hit || r.mode === "observe" || !r.score) continue;
      const cur = map.get(r.dimension) ?? { dim: r.dimension, score: 0, count: 0 };
      cur.score += r.score;
      cur.count += 1;
      map.set(r.dimension, cur);
    }
    return [...map.values()].sort((a, b) => b.score - a.score);
  }, [rules]);
  const positive = parts.filter((p) => p.score > 0);
  const sum = positive.reduce((s, p) => s + p.score, 0);

  if (!rules) return null;
  if (!positive.length) {
    return <p className="text-xs text-muted-foreground">{score > 0 ? "分数构成未记录。" : "没有规则为这次请求计分。"}</p>;
  }
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>分数构成</span>
        <span className="tabular-nums">{positive.length} 个维度</span>
      </div>
      <div className="flex h-3 w-full min-w-0 gap-0.5 overflow-hidden rounded-full bg-muted">
        {positive.map((p) => (
          <span key={p.dim} className={cn("h-full", DIM_COLOR[p.dim] ?? "bg-zinc-400")} style={{ flexGrow: p.score / sum, flexBasis: 0 }} title={`${DIM_LABEL[p.dim] ?? p.dim} +${p.score}`} />
        ))}
      </div>
      <div className="flex min-w-0 flex-wrap gap-x-4 gap-y-1">
        {parts.map((p) => (
          <span key={p.dim} className="inline-flex items-center gap-1.5 text-xs">
            <span aria-hidden className={cn("size-2 rounded-[3px]", DIM_COLOR[p.dim] ?? "bg-zinc-400")} />
            <span className="text-muted-foreground">{DIM_LABEL[p.dim] ?? p.dim}</span>
            <span className="font-medium tabular-nums">
              {p.score > 0 ? "+" : ""}
              {p.score}
            </span>
          </span>
        ))}
      </div>
    </div>
  );
}

function RuleHitLine({ rule }: { rule: TraceRule }) {
  const observe = rule.mode === "observe";
  return (
    <li className="flex min-w-0 items-start gap-3 px-3 py-2.5">
      <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", observe ? "bg-sky-500" : DIM_COLOR[rule.dimension] ?? "bg-amber-500")} aria-hidden />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-sm font-medium">{rule.name || "未命名规则"}</span>
          {rule.dimension ? <span className="text-xs text-muted-foreground">{DIM_LABEL[rule.dimension] ?? rule.dimension}</span> : null}
          {observe ? <Badge variant="info" size="sm">观察中，不计分</Badge> : null}
          {rule.decision && !observe ? <Badge variant="danger" size="sm">直接判定为{ACTION_LABEL[rule.decision as RiskAction] ?? rule.decision}</Badge> : null}
        </div>
        {rule.reason ? <p className="text-xs leading-5 text-muted-foreground">{rule.reason}</p> : null}
      </div>
      <span className={cn("shrink-0 text-sm font-semibold tabular-nums", observe ? "text-muted-foreground" : "text-amber-600 dark:text-amber-400")}>
        {rule.score > 0 ? "+" : ""}
        {rule.score}
      </span>
    </li>
  );
}

/* ── 环境 ── */

const DEVICE_LABEL: Record<string, string> = {
  platform: "平台", brand: "品牌", model: "型号", os_version: "系统版本", app_version: "应用版本", screen: "屏幕", locale: "语言",
  timezone: "时区", emulator: "模拟器", rooted: "已 Root 或越狱",
};

type Signal = { label: string; tone: "bad" | "warn" | "ok" | "neutral" };

function envSignals(env: Record<string, unknown> | undefined, device: Record<string, string> | null): Signal[] {
  const out: Signal[] = [];
  const on = (p: string) => pick(env, p) === true;
  if (on("net.tor")) out.push({ label: "Tor 出口节点", tone: "bad" });
  if (on("net.proxy")) out.push({ label: "使用代理", tone: "warn" });
  if (on("net.vpn")) out.push({ label: "使用 VPN", tone: "warn" });
  if (on("net.datacenter")) out.push({ label: "机房 IP", tone: "warn" });
  if (on("net.private")) out.push({ label: "内网地址", tone: "neutral" });
  const ipScore = Number(pick(env, "net.score") ?? 0);
  if (ipScore > 0) out.push({ label: `IP 情报分 ${ipScore}`, tone: ipScore >= 60 ? "bad" : ipScore >= 30 ? "warn" : "neutral" });
  if (on("device.emulator") || device?.emulator === "true") out.push({ label: "模拟器", tone: "bad" });
  if (on("device.rooted") || device?.rooted === "true") out.push({ label: "已 Root 或越狱", tone: "warn" });
  if (env && pick(env, "device.present") === false && !device) out.push({ label: "未携带设备信息", tone: "neutral" });
  if (on("client.bot")) out.push({ label: "爬虫特征", tone: "bad" });
  if (on("client.headless")) out.push({ label: "无头浏览器", tone: "bad" });
  if (on("client.empty")) out.push({ label: "未带 User-Agent", tone: "warn" });
  if (on("captcha_passed")) out.push({ label: "已通过验证码", tone: "ok" });
  if (env && "logged_in" in env) out.push({ label: env.logged_in ? "已登录" : "未登录", tone: "neutral" });
  return out;
}

const SIGNAL_TONE: Record<Signal["tone"], string> = {
  bad: "border-red-500/30 bg-red-500/8 text-red-700 dark:text-red-300",
  warn: "border-amber-500/30 bg-amber-500/8 text-amber-700 dark:text-amber-300",
  ok: "border-emerald-500/30 bg-emerald-500/8 text-emerald-700 dark:text-emerald-300",
  neutral: "border-border bg-muted/50 text-muted-foreground",
};

function EnvironmentGrid({ detail }: { detail: DecisionDetail }) {
  const d = detail.decision;
  const env = detail.trace?.env;
  const device = detail.device && Object.keys(detail.device).length ? detail.device : null;
  const scenes = useRiskScenes();
  const appName = useAppName();
  const fieldLabel = useMemo(() => {
    const s = (scenes.data ?? []).find((x) => x.key === d.scene);
    return new Map((s?.fields ?? []).map((f) => [f.key, f.label]));
  }, [scenes.data, d.scene]);
  const signals = envSignals(env, device);
  const risky = signals.filter((s) => s.tone === "bad" || s.tone === "warn");

  const place = [regionName(d.country), detail.province, d.city].filter((v, i, a) => v && a.indexOf(v) === i).join(" ");
  const browser = [pick(env, "client.browser"), pick(env, "client.version")].filter(Boolean).join(" ");
  const os = String(pick(env, "client.os") ?? "");
  const deviceName = device ? [device.brand, device.model].filter(Boolean).join(" ") : "";
  const deviceOs = device ? [device.platform, device.os_version].filter(Boolean).join(" ") : "";
  const deviceRest = device
    ? Object.entries(device).filter(([k, v]) => v && !["brand", "model", "platform", "os_version", "emulator", "rooted"].includes(k))
    : [];
  const extra = Object.entries(detail.extra ?? {}).filter(([, v]) => v !== null && v !== undefined && v !== "");

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className={cn("flex min-w-0 flex-wrap items-center gap-1.5 rounded-xl px-3 py-2.5", risky.length ? "bg-amber-500/8" : "bg-emerald-500/8")}>
        <span className={cn("mr-1 text-xs font-medium", risky.length ? "text-amber-700 dark:text-amber-300" : "text-emerald-700 dark:text-emerald-300")}>
          {risky.length ? `发现 ${risky.length} 项异常` : "未发现网络或设备异常"}
        </span>
        {signals.map((s) => (
          <span key={s.label} className={cn("inline-flex h-6 items-center rounded-md border px-2 text-xs", SIGNAL_TONE[s.tone])}>
            {s.label}
          </span>
        ))}
      </div>

      <div className="grid min-w-0 gap-3 sm:grid-cols-2">
        <EnvCard icon={UserRound} title="账号">
          <EnvLine primary={d.account || (d.userId ? `用户 #${d.userId}` : "匿名访客")} secondary={d.account && d.userId ? `用户 #${d.userId}` : undefined} />
          <EnvLine primary={appName(d.appId)} label="应用" />
        </EnvCard>
        <EnvCard icon={Globe2} title="网络">
          <EnvLine
            primary={
              d.ip ? (
                <span className="inline-flex items-center gap-1.5">
                  {d.country ? <CountryFlag code={d.country} size={14} /> : null}
                  <span className="font-mono text-xs">{d.ip}</span>
                  <CopyButton text={d.ip} what="IP" />
                </span>
              ) : (
                "未知 IP"
              )
            }
            secondary={place || "地区未知"}
          />
          {detail.isp || detail.asn ? <EnvLine label="运营商" primary={[detail.isp, detail.asn ? `AS${detail.asn}` : ""].filter(Boolean).join(" ")} /> : null}
        </EnvCard>
        <EnvCard icon={MonitorSmartphone} title="设备">
          {device || d.deviceId ? (
            <>
              <EnvLine primary={deviceName || "未知型号"} secondary={deviceOs || undefined} />
              {d.deviceId ? (
                <EnvLine
                  label="设备标识"
                  primary={
                    <span className="inline-flex min-w-0 items-center gap-1">
                      <span className="min-w-0 truncate font-mono text-xs">{d.deviceId}</span>
                      <CopyButton text={d.deviceId} what="设备标识" />
                    </span>
                  }
                />
              ) : null}
              {deviceRest.length ? (
                <dl className="grid grid-cols-2 gap-x-3 gap-y-1.5 pt-1">
                  {deviceRest.map(([k, v]) => (
                    <Fact key={k} label={DEVICE_LABEL[k] ?? k.replace(/^x\./, "")} value={v} />
                  ))}
                </dl>
              ) : null}
            </>
          ) : (
            <p className="text-sm text-muted-foreground">请求未携带设备信息</p>
          )}
        </EnvCard>
        <EnvCard icon={Fingerprint} title="客户端">
          {browser || os || detail.ua ? (
            <>
              <EnvLine primary={browser || "未知浏览器"} secondary={[os, pick(env, "client.mobile") === true ? "移动端" : ""].filter(Boolean).join(" ") || undefined} />
              {detail.ua ? (
                <p className="line-clamp-2 font-mono text-[11px] leading-4 break-all text-muted-foreground" title={detail.ua}>
                  {detail.ua}
                </p>
              ) : null}
            </>
          ) : (
            <p className="text-sm text-muted-foreground">未带 User-Agent</p>
          )}
        </EnvCard>
      </div>

      {extra.length ? (
        <div className="flex min-w-0 flex-col gap-2 rounded-xl border p-3 sm:p-4">
          <h5 className="text-xs font-medium text-muted-foreground">业务参数</h5>
          <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-2 md:grid-cols-3">
            {extra.map(([k, v]) => (
              <Fact key={k} label={fieldLabel.get(k) ?? k} value={valueText(v)} wrap={typeof v === "object"} />
            ))}
          </dl>
        </div>
      ) : null}
    </div>
  );
}

function valueText(v: unknown): string {
  if (typeof v === "boolean") return v ? "是" : "否";
  if (typeof v === "number") return v.toLocaleString("zh-CN");
  if (typeof v === "string") return v;
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

function EnvCard({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-2.5 rounded-xl border p-3 sm:p-4">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Icon className="size-3.5" />
        {title}
      </div>
      {children}
    </div>
  );
}

function EnvLine({ primary, secondary, label }: { primary: ReactNode; secondary?: string; label?: string }) {
  return (
    <div className="flex min-w-0 flex-col">
      {label ? <span className="text-[11px] leading-4 text-muted-foreground">{label}</span> : null}
      <span className="min-w-0 truncate text-sm leading-5">{primary}</span>
      {secondary ? <span className="truncate text-xs leading-4 text-muted-foreground">{secondary}</span> : null}
    </div>
  );
}

function CopyButton({ text, what }: { text: string; what: string }) {
  return (
    <button
      type="button"
      aria-label={`复制${what}`}
      onClick={() => copy(text, what)}
      className="grid size-5 shrink-0 place-items-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
    >
      <Copy className="size-3" />
    </button>
  );
}

/* ── 特征与模型 ── */

function SignalFeatures({ features, models }: { features: Record<string, number>; models: Record<string, number> }) {
  const featureList = useRiskFeatures();
  const modelList = useRiskModels();
  const [all, setAll] = useState(false);
  const fname = useMemo(() => new Map((featureList.data ?? []).map((f) => [f.key, f.name])), [featureList.data]);
  const mname = useMemo(() => new Map((modelList.data ?? []).map((m) => [m.key, m.name])), [modelList.data]);
  const entries = Object.entries(features)
    .map(([k, v]) => ({ key: k, name: fname.get(k) || k, value: v }))
    .sort((a, b) => Number(b.value !== 0) - Number(a.value !== 0) || a.name.localeCompare(b.name, "zh-CN"));
  const nonzero = entries.filter((e) => e.value !== 0);
  const shown = all ? entries : nonzero.slice(0, 9);
  const modelEntries = Object.entries(models);

  if (!entries.length && !modelEntries.length) return null;
  return (
    <div className="flex min-w-0 flex-col gap-3">
      {modelEntries.length ? (
        <div className="flex min-w-0 flex-col gap-2">
          <h5 className="text-xs font-medium text-muted-foreground">模型评分</h5>
          <ul className="flex min-w-0 flex-col gap-2">
            {modelEntries.map(([k, v]) => {
              const pct = Math.max(0, Math.min(1, v)) * 100;
              return (
                <li key={k} className="flex min-w-0 items-center gap-3">
                  <span className="w-36 shrink-0 truncate text-sm">{mname.get(k) || k}</span>
                  <div className="h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                    <div className={cn("h-full rounded-full", pct >= 80 ? "bg-red-500" : pct >= 50 ? "bg-amber-500" : "bg-sky-500")} style={{ width: `${pct}%` }} />
                  </div>
                  <span className="w-12 shrink-0 text-right text-xs tabular-nums">{Math.round(pct)}%</span>
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}
      {entries.length ? (
        <div className="flex min-w-0 flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <h5 className="text-xs font-medium text-muted-foreground">行为特征</h5>
            <span className="text-xs text-muted-foreground tabular-nums">
              {nonzero.length} 项有值，共 {entries.length} 项
            </span>
          </div>
          {shown.length ? (
            <dl className="grid min-w-0 grid-cols-2 gap-2 md:grid-cols-3">
              {shown.map((e) => (
                <div key={e.key} className={cn("flex min-w-0 flex-col rounded-lg border px-3 py-2", e.value === 0 && "border-dashed")}>
                  <dt className="truncate text-[11px] leading-4 text-muted-foreground" title={e.name}>
                    {e.name}
                  </dt>
                  <dd className={cn("text-base leading-6 font-semibold tabular-nums", e.value === 0 && "text-muted-foreground")}>{formatNumber(e.value)}</dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">所有特征均为 0。</p>
          )}
          {entries.length > shown.length || all ? (
            <Button size="sm" variant="ghost" className="h-8 self-start text-xs" onClick={() => setAll((v) => !v)}>
              <ChevronDown className={cn("transition-transform", all && "rotate-180")} />
              {all ? "只看有值的特征" : `查看全部 ${entries.length} 项`}
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

/* ── 处置命令 ── */

const TARGET_NOUN: Record<string, string> = { device: "设备", ip: "IP", account: "登录名", user: "用户" };

function commandSentence(c: Record<string, unknown>): string {
  const kind = String(c.kind ?? "");
  const target = String(c.target ?? "");
  const value = String(c.value ?? "");
  const seconds = Number(c.durationSeconds ?? c.duration_seconds ?? 0);
  const noun = TARGET_NOUN[target] ?? "对象";
  const who = target === "user" ? `用户 #${value}` : `${noun} ${value}`;
  const span = seconds > 0 ? `，期限 ${formatWindow(seconds)}` : "，长期有效";
  switch (kind) {
    case "list_add":
      return `将${who} 加入黑名单${span}`;
    case "list_remove":
      return `将${who} 移出名单`;
    case "account_ban":
      return `封禁${who}${span}`;
    case "account_unban":
      return `解封${who}`;
    case "firewall_ban":
      return `在防火墙封禁 IP ${value}${span}`;
    case "firewall_unban":
      return `在防火墙解封 IP ${value}`;
    case "revoke_sessions":
      return `让${who} 的登录全部失效`;
    default:
      return `对${who} 执行处置`;
  }
}

/* ── 名单命中 ── */

export function ListHitTable({ hits }: { hits: ListHit[] }) {
  return (
    <ul className="flex min-w-0 flex-col divide-y overflow-hidden rounded-xl border">
      {hits.map((h, i) => (
        <li key={`${h.listType}-${h.value}-${i}`} className="flex min-w-0 items-start gap-3 px-3 py-2.5">
          <span
            aria-hidden
            className={cn(
              "mt-1.5 size-2 shrink-0 rounded-full",
              h.kind === "block" ? "bg-red-500" : h.kind === "allow" ? "bg-emerald-500" : "bg-amber-500"
            )}
          />
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <span className="text-sm">{listEntrySentence({ type: h.listType, kind: h.kind })}</span>
              <KindBadge kind={h.kind} />
            </div>
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
              <span className="shrink-0">{LIST_TYPE_LABEL[h.listType] ?? h.listType}</span>
              <span className="min-w-0 font-mono break-all text-foreground">{h.value}</span>
            </div>
            {h.reason ? <p className="text-xs leading-5 text-muted-foreground">{h.reason}</p> : null}
          </div>
        </li>
      ))}
    </ul>
  );
}

/* ── 小件 ── */

export function Fact({ label, value, mono, wrap, className }: { label: string; value: ReactNode; mono?: boolean; wrap?: boolean; className?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      <dt className="truncate text-[11px] leading-4 text-muted-foreground">{label}</dt>
      <dd className={cn("text-sm leading-5", wrap ? "break-all" : "truncate", mono && "font-mono text-xs leading-5")} title={typeof value === "string" ? value : undefined}>
        {value}
      </dd>
    </div>
  );
}

function formatNumber(v: number) {
  if (Number.isInteger(v)) return v.toLocaleString("zh-CN");
  return v.toLocaleString("zh-CN", { maximumFractionDigits: 3 });
}

