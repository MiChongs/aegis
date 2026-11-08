"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import NumberFlow from "@number-flow/react";
import { Bar, CartesianGrid, ComposedChart, Line, ReferenceLine, XAxis, YAxis } from "recharts";
import {
  Activity, AlertTriangle, Ban, CalendarCheck, ChevronDown, ChevronRight, ClipboardCheck, Fingerprint, Gift, Globe, History,
  KeyRound, Link2, LogIn, MapPin, Monitor, Network, QrCode, RotateCcw, ScrollText, ShieldAlert, ShieldCheck, ShieldQuestion,
  Siren, Smartphone, Sparkles, Tablet, Ticket, UserCog, UserPlus, Users, Wallet, type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";

import { TonalIcon, type KitTone } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty } from "@/components/overview/overview-kit";
import { DecisionDetailDialog, useSceneName } from "@/components/risk/decisions";
import { analyze, displayName, narrative, peopleDegree } from "@/components/risk/graph/graph-model";
import { NodeDetail } from "@/components/risk/graph/node-detail";
import { RelationList, RelationNarrative } from "@/components/risk/graph/relation-summary";
import { RiskGraph } from "@/components/risk/graph/risk-graph";
import { ACTION_RESULT, countText, decisionReason, eventName, percent, remaining, scoreWord, when } from "@/components/risk/kit/humanize";
import {
  ACTION_COLOR, ACTION_LABEL, ACTION_ORDER, DIM_LABEL, DURATION_PRESETS, ENTITY_LABEL, EVENT_LABEL, GRAPH_TEMPLATE_LABEL, KIND_LABEL,
  SEVERITY_LABEL, formatWindow,
} from "@/components/risk/kit/labels";
import { Segmented } from "@/components/traffic/traffic-kit";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { CountryFlag } from "@/components/ui/country-flag";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AdminAppUserDetail } from "@/lib/api/types";
import type {
  AlertRow, DecisionRow, DisposeInput, EntityUse, EventRow, Feature, GraphNode, KV, ListEntry, RiskAction, RiskCase, RiskEntity,
  Rule, SceneUse, SimilarDevice, Subgraph, UserRiskProfile,
} from "@/lib/api/risk";
import { getRiskMeta, listModels, listRules } from "@/lib/api/risk";
import { useAdminToken } from "@/lib/admin-hooks";
import { formatClock, formatRelative, formatWith, localDateKey } from "@/lib/datetime";
import { RISK_KEYS, useDispose, useRiskFeatures, useRiskGraph, useRiskUserProfile } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { EmptyRow, Panel } from "./user-detail-shared";

/**
 * 用户详情 → 风险。
 *
 * 版面自上而下：风险概况（一段叙述 + 风险分仪表 + 处置构成环）、关系网络、按天的风险时间线与处置、
 * 设备卡片墙、IP 与触达场景、决策与业务事件、告警与复核、特征快照。每条记录都写成一句话（谁、做了什么、结果、原因），
 * 不摆字段名。各面板统一用 user-detail-shared 的 Panel，节间距 16。
 */
export function UserRiskTab({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const appId = user.appid;
  const [days, setDays] = useState(30);
  const [openDecision, setOpenDecision] = useState<string | null>(null);
  const profile = useRiskUserProfile(appId, userId, days);
  const data = profile.data;
  const loading = profile.isLoading;
  const name = user.nickname || user.account || `用户 #${userId}`;
  const sceneName = useSceneName();
  const look = useLookup(data);

  if (!appId) {
    return <Panel title="风险" icon={<ShieldAlert className="size-4" />}><EmptyRow text="读不到用户所属应用" /></Panel>;
  }
  if (profile.isError) {
    return (
      <Panel title="风险概况" icon={<ShieldAlert className="size-4" />} action={<RangeToggle days={days} onChange={setDays} />}>
        <div className="flex flex-col items-center gap-3 py-6 text-center">
          <CardEmpty icon={AlertTriangle} title="暂时读不到风险数据" description="风控服务没有响应，请稍后重试" />
          {(profile.error as Error)?.message ? (
            <p className="max-w-md text-xs text-muted-foreground">{(profile.error as Error).message}</p>
          ) : null}
          <Button size="sm" variant="outline" onClick={() => profile.refetch()} disabled={profile.isFetching}>
            <RotateCcw className="size-3.5" />
            重试
          </Button>
        </div>
      </Panel>
    );
  }

  const errors = data?.errors ?? {};

  return (
    <div className="space-y-4">
      <RiskHero
        days={days}
        onDays={setDays}
        profile={data}
        loading={loading}
        refreshing={profile.isFetching && !loading}
        sceneName={sceneName}
        look={look}
        appId={appId}
      />

      <RiskGraphPanel appKey={appKey} appId={appId} graph={data?.graph} days={days} name={name} loading={loading} error={errors.graph} />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <TimelinePanel timeline={data?.timeline ?? []} days={days} loading={loading} error={errors.timeline} />
        <DisposePanel
          appId={appId}
          userId={userId}
          name={name}
          account={user.account}
          lists={data?.lists ?? []}
          devices={data?.devices ?? []}
          ips={data?.ips ?? []}
          look={look}
          loading={loading}
        />
      </div>

      <DevicesPanel devices={data?.devices ?? []} total={data?.summary?.devices ?? 0} appKey={appKey} look={look} loading={loading} />

      <div className="grid gap-4 xl:grid-cols-2">
        <IPsPanel ips={data?.ips ?? []} total={data?.summary?.ips ?? 0} loading={loading} />
        <div className="flex min-w-0 flex-col gap-4">
          <ScenesPanel rows={data?.scenes ?? []} total={data?.summary?.total ?? 0} sceneName={sceneName} loading={loading} error={errors.scenes} />
          <RulesPanel rows={data?.rules ?? []} names={data?.names?.rules} meta={data?.ruleMeta} total={data?.summary?.total ?? 0} loading={loading} error={errors.rules} />
        </div>
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <DecisionsPanel
          rows={data?.decisions ?? []}
          appId={appId}
          userId={userId}
          sceneName={sceneName}
          look={look}
          loading={loading}
          error={errors.decisions}
          onOpen={setOpenDecision}
        />
        <EventsPanel rows={data?.events ?? []} sceneName={sceneName} look={look} loading={loading} error={errors.events} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)]">
        <AlertsCasesPanel
          alerts={data?.alerts ?? []}
          cases={data?.cases ?? []}
          appId={appId}
          sceneName={sceneName}
          look={look}
          loading={loading}
          errors={errors}
          onOpenDecision={setOpenDecision}
        />
        <FeaturesPanel snaps={data?.features ?? []} names={data?.names} sceneName={sceneName} loading={loading} error={errors.features} />
      </div>

      <DecisionDetailDialog id={openDecision} open={!!openDecision} onOpenChange={(o) => !o && setOpenDecision(null)} />
    </div>
  );
}

// ── 关系图 ──

function RiskGraphPanel({ appKey, appId, graph, days, name, loading, error }: {
  appKey: string; appId: number; graph?: Subgraph; days: number; name: string; loading: boolean; error?: string;
}) {
  const [center, setCenter] = useState<string | null>(null);
  const [selected, setSelected] = useState<GraphNode | null>(null);
  const [depth, setDepth] = useState(2);
  const recentered = useRiskGraph(center || depth !== 2 ? { type: "vid", value: center ?? graph?.center ?? "", depth, fanout: 40, max: 300, days } : null);
  const shown = (center || depth !== 2) && recentered.data ? recentered.data : graph;

  const stats = useMemo(() => (shown ? analyze(shown) : null), [shown]);
  const degree = useMemo(() => (shown ? peopleDegree(shown) : new Map<string, number>()), [shown]);
  const centerNode = shown?.nodes.find((n) => n.id === shown.center);
  const centerName = center && centerNode ? displayName(centerNode) : name;
  const expand = (n: GraphNode) => {
    setSelected(null);
    setCenter(n.id === graph?.center ? null : n.id);
  };

  return (
    <Panel
      title="关系网络"
      icon={<Network className="size-4" />}
      description="所选时间内与这位用户有关的设备、网络、第三方账号、邀请与支付关系。多人共用的出口和机房、代理地址只画成灰色虚线，不当作关联依据"
      action={
        <div className="flex items-center gap-2">
          <Select value={String(depth)} onValueChange={(v) => setDepth(Number(v))}>
            <SelectTrigger size="sm" className="h-8 w-24"><SelectValue /></SelectTrigger>
            <SelectContent>
              {[1, 2, 3].map((d) => <SelectItem key={d} value={String(d)}>{d} 跳</SelectItem>)}
            </SelectContent>
          </Select>
          {center ? (
            <Button size="sm" variant="outline" onClick={() => { setCenter(null); setSelected(null); }}>
              <RotateCcw className="size-3.5" />
              回到本人
            </Button>
          ) : null}
        </div>
      }
    >
      {loading ? (
        <div className="space-y-3">
          <Skeleton className="h-24 rounded-xl" />
          <Skeleton className="h-[560px] rounded-xl" />
        </div>
      ) : !shown || shown.nodes.length === 0 || !stats ? (
        <EmptyRow text={error ? `关系网络暂时不可用：${error}` : "这段时间里还没有关系数据：用户登录或产生业务后才会出现"} />
      ) : (
        <div className="space-y-4">
          <RelationNarrative text={narrative(centerName, days, stats)} stats={stats} />
          <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
            <div className="relative min-w-0 rounded-xl border">
              {recentered.isFetching ? (
                <div className="absolute top-14 right-3 z-10 rounded-md bg-background/85 px-2 py-1 text-xs text-muted-foreground">展开中</div>
              ) : null}
              <RiskGraph graph={shown} height={600} selectedId={selected?.id} onSelect={setSelected} onExpand={expand} />
              {shown.truncated ? (
                <p className="border-t px-3 py-2 text-xs text-amber-600 dark:text-amber-400">关系太多，只画出了最近的一部分；双击某个点可以以它为中心看得更细</p>
              ) : null}
            </div>
            <div className="min-w-0 space-y-4">
              {selected ? (
                <NodeDetail
                  node={selected}
                  degree={degree.get(selected.id) ?? 0}
                  appKey={appKey}
                  appId={appId}
                  onClose={() => setSelected(null)}
                  onExpand={selected.id === shown.center ? undefined : expand}
                />
              ) : null}
              <div>
                <div className="mb-2 flex items-center justify-between px-1">
                  <span className="text-sm font-medium">关联的人</span>
                  <span className="text-xs text-muted-foreground">按关联强弱排列</span>
                </div>
                <RelationList relations={stats.relations} appKey={appKey} appId={appId} focusId={selected?.id} onFocus={setSelected} />
              </div>
            </div>
          </div>
        </div>
      )}
    </Panel>
  );
}

// ── 说法、配色与查表 ──

type Tone = "danger" | "warning" | "success" | "info" | "muted";

const TONE_TEXT: Record<Tone, string> = {
  danger: "text-rose-600 dark:text-rose-400",
  warning: "text-amber-600 dark:text-amber-400",
  success: "text-emerald-600 dark:text-emerald-400",
  info: "text-sky-600 dark:text-sky-400",
  muted: "text-muted-foreground",
};

const TONE_SOFT: Record<Tone, string> = {
  danger: "bg-rose-500/10 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300",
  warning: "bg-amber-500/10 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
  success: "bg-emerald-500/10 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
  info: "bg-sky-500/10 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
  muted: "bg-muted text-muted-foreground",
};

const TONE_DOT: Record<Tone, string> = {
  danger: "bg-rose-500", warning: "bg-amber-500", success: "bg-emerald-500", info: "bg-sky-500", muted: "bg-muted-foreground/40",
};

const TONE_BORDER: Record<Tone, string> = {
  danger: "border-rose-300/70 dark:border-rose-500/40",
  warning: "border-amber-300/70 dark:border-amber-500/40",
  success: "border-emerald-300/70 dark:border-emerald-500/40",
  info: "border-sky-300/70 dark:border-sky-500/40",
  muted: "border-border",
};

const KIT_TONE: Record<Tone, KitTone> = { danger: "rose", warning: "amber", success: "emerald", info: "sky", muted: "muted" };
const ACTION_TONE_OF: Record<RiskAction, Tone> = { pass: "success", review: "info", captcha: "warning", block: "danger", ban: "danger" };
const KIND_TONE_OF: Record<string, Tone> = { block: "danger", watch: "warning", allow: "success" };

function scoreTone(score: number): Tone {
  return scoreWord(score).tone;
}

function str(v: unknown) {
  return typeof v === "string" ? v.trim() : "";
}

function num(v: unknown) {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function uniqText(parts: Array<string | undefined | null>) {
  const out: string[] = [];
  for (const p of parts) {
    const t = (p ?? "").trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}

const REGION = (() => {
  try {
    return new Intl.DisplayNames(["zh-CN"], { type: "region" });
  } catch {
    return null;
  }
})();

const LANGUAGE = (() => {
  try {
    return new Intl.DisplayNames(["zh-CN"], { type: "language" });
  } catch {
    return null;
  }
})();

/** 「CN」→「中国」，「HK」→「中国香港」；已经是名称的原样返回 */
function regionName(code?: string | null) {
  const c = (code ?? "").trim();
  if (!/^[A-Za-z]{2}$/.test(c)) return c;
  try {
    return (REGION?.of(c.toUpperCase()) ?? c).replace("特别行政区", "");
  } catch {
    return c;
  }
}

function languageName(tag?: string) {
  const t = (tag ?? "").trim().replace("_", "-");
  if (!t) return "";
  try {
    return LANGUAGE?.of(t) ?? t;
  } catch {
    return t;
  }
}

/** 「中国 广东 广州」 */
function placeText(country?: string | null, province?: string | null, city?: string | null) {
  return uniqText([regionName(country), province, city]).join(" ");
}

function shortId(id: string) {
  return id.length > 12 ? `${id.slice(0, 6)}…${id.slice(-4)}` : id;
}

// 设备

const BRAND_LABEL: Record<string, string> = {
  xiaomi: "小米", huawei: "华为", honor: "荣耀", oppo: "OPPO", vivo: "vivo", oneplus: "一加", meizu: "魅族", samsung: "三星",
  apple: "Apple", google: "Google", realme: "真我", iqoo: "iQOO", nubia: "努比亚", zte: "中兴", lenovo: "联想", motorola: "摩托罗拉",
  sony: "索尼", asus: "华硕", redmi: "Redmi", poco: "POCO", hmd: "HMD", nokia: "诺基亚", tcl: "TCL", hisense: "海信",
};

const PLATFORM_LABEL: Record<string, string> = {
  android: "Android", ios: "iOS", ipados: "iPadOS", windows: "Windows", macos: "macOS", linux: "Linux", harmonyos: "鸿蒙", ohos: "鸿蒙", web: "网页",
};

function brandName(b: string) {
  return BRAND_LABEL[b.trim().toLowerCase()] ?? b.trim();
}

function platformName(p?: string) {
  const k = (p ?? "").trim().toLowerCase();
  return PLATFORM_LABEL[k] ?? (p ?? "").trim();
}

type Fp = Record<string, string>;

function fpOf(d?: EntityUse, node?: GraphNode): Fp {
  const out: Fp = {};
  const props = node?.props ?? {};
  for (const k of ["platform", "brand", "model", "os_version", "app_version", "screen", "locale", "timezone"]) {
    const v = d?.fingerprint?.[k] || str(props[k]);
    if (v) out[k] = v;
  }
  if (d?.fingerprint?.emulator === "true" || props.emulator === true) out.emulator = "true";
  if (d?.fingerprint?.rooted === "true" || props.rooted === true) out.rooted = "true";
  return out;
}

/** 「小米 24031PN0DC」 */
function deviceTitle(fp: Fp): string {
  const brand = fp.brand ?? "";
  const model = fp.model ?? "";
  if (model) {
    if (!brand) return model;
    const b = brandName(brand);
    if (model.toLowerCase().startsWith(brand.toLowerCase())) {
      const rest = model.slice(brand.length).trim();
      return rest ? `${b} ${rest}` : b;
    }
    return `${b} ${model}`;
  }
  if (brand) return `${brandName(brand)} 设备`;
  const plat = platformName(fp.platform);
  return plat ? `${plat} 设备` : "";
}

/** 「Android 15」 */
function deviceSystem(fp: Fp): string {
  const plat = platformName(fp.platform);
  const os = fp.os_version ?? "";
  if (!plat) return os ? `系统 ${os}` : "";
  if (!os) return plat;
  return os.toLowerCase().includes(plat.toLowerCase()) ? os : `${plat} ${os}`;
}

function deviceIcon(fp: Fp): LucideIcon {
  const p = (fp.platform ?? "").toLowerCase();
  if (p === "ipados") return Tablet;
  if (p === "windows" || p === "macos" || p === "linux") return Monitor;
  if (p === "web") return Globe;
  return Smartphone;
}

// IP

function ipFlags(use: EntityUse): Array<{ label: string; tone: Tone }> {
  const n = use.net ?? {};
  const out: Array<{ label: string; tone: Tone }> = [];
  if (n.v6) out.push({ label: "IPv6 线路", tone: "muted" });
  else if (use.value.includes(":")) out.push({ label: "IPv6", tone: "muted" });
  if (n.vpn) out.push({ label: "VPN", tone: "warning" });
  else if (n.proxy) out.push({ label: "代理", tone: "warning" });
  if (n.hosting) out.push({ label: "机房出口", tone: "warning" });
  if (n.shared) out.push({ label: n.users ? `${n.users} 人共用` : "多人共用", tone: "muted" });
  return out;
}

function isRiskyIP(use: EntityUse) {
  const n = use.net ?? {};
  return !!(n.vpn || n.proxy || n.hosting);
}

/** 「中国电信」，没有名称时给自治域号 */
function ispText(use: EntityUse) {
  return (use.isp ?? "").trim() || (use.asn ? `AS${use.asn}` : "");
}

function ipPlace(use: EntityUse) {
  return placeText(use.country, use.province, use.city);
}

/** 相似设备的说法：「张三的小米 14」，缺名字时「用户 #123 的设备」 */
function twinName(n: SimilarDevice) {
  const model = (n.model ?? "").trim();
  const who = (n.userName ?? "").trim();
  if (who) return `${who}的${model || "设备"}`;
  if (n.userId) return `用户 #${n.userId} 的${model || "设备"}`;
  return model || `设备 ${shortId(n.deviceId)}`;
}

// 查表：设备型号（指纹缺项时用关系图里的设备点补）

interface Lookup {
  device: (id: string) => EntityUse | undefined;
  deviceNode: (id: string) => GraphNode | undefined;
  fingerprint: (id: string) => Fp;
  deviceLabel: (id: string) => string;
}

function useLookup(data?: UserRiskProfile): Lookup {
  return useMemo(() => {
    const devices = new Map((data?.devices ?? []).map((d) => [d.value, d]));
    const nodes = new Map((data?.graph?.nodes ?? []).map((n) => [n.id, n]));
    const deviceNode = (id: string) => nodes.get(`d:${id}`);
    const fingerprint = (id: string) => fpOf(devices.get(id), deviceNode(id));
    const deviceLabel = (id: string) => deviceTitle(fingerprint(id)) || `设备 ${shortId(id)}`;
    return { device: (id: string) => devices.get(id), deviceNode, fingerprint, deviceLabel };
  }, [data]);
}

// 老版本画像没有 names 时才去读配置（与 risk-hooks 共用缓存键）

function useFallbackRules(enabled: boolean) {
  const t = useAdminToken();
  return useQuery({ queryKey: [...RISK_KEYS.rules, ""], queryFn: () => listRules(t as string), enabled: !!t && enabled });
}

function useFallbackModels(enabled: boolean) {
  const t = useAdminToken();
  return useQuery({ queryKey: RISK_KEYS.models, queryFn: () => listModels(t as string), enabled: !!t && enabled });
}

function useFallbackMeta(enabled: boolean) {
  const t = useAdminToken();
  return useQuery({ queryKey: RISK_KEYS.meta, queryFn: () => getRiskMeta(t as string), enabled: !!t && enabled, staleTime: 10 * 60_000 });
}

// ── 小组件 ──

function RangeToggle({ days, onChange }: { days: number; onChange: (d: number) => void }) {
  return (
    <ToggleGroup type="single" size="sm" variant="outline" value={String(days)} onValueChange={(v) => v && onChange(Number(v))} aria-label="时间范围">
      {[7, 30, 90, 180].map((d) => (
        <ToggleGroupItem key={d} value={String(d)} className="px-3 text-xs">{d} 天</ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

function ScoreChip({ score }: { score: number }) {
  const w = scoreWord(score);
  return (
    <span title={w.label} className={cn("inline-flex shrink-0 items-baseline gap-0.5 rounded-md px-1.5 py-0.5 text-xs font-medium tabular-nums", TONE_SOFT[w.tone])}>
      {score}
      <span className="text-[10px] font-normal opacity-80">分</span>
    </span>
  );
}

function Pill({ tone, children, title }: { tone: Tone; children: ReactNode; title?: string }) {
  return (
    <span title={title} className={cn("inline-flex shrink-0 items-center rounded-md px-1.5 py-0.5 text-[11px] leading-4 font-medium", TONE_SOFT[tone])}>
      {children}
    </span>
  );
}

/** 时间：相对时间，悬停给完整时间 */
function When({ iso, className }: { iso?: string | null; className?: string }) {
  const w = when(iso);
  if (!w.relative) return null;
  return <time dateTime={iso ?? undefined} title={w.absolute} className={className}>{w.relative}</time>;
}

/** 一行灰色的补充信息，自动用逗号连接 */
function MetaLine({ parts, className }: { parts: Array<ReactNode | null | undefined | false>; className?: string }) {
  const items = parts.filter((p) => p !== null && p !== undefined && p !== false && p !== "");
  if (!items.length) return null;
  return (
    <div className={cn("flex min-w-0 flex-wrap items-center gap-x-1 text-xs leading-5 text-muted-foreground", className)}>
      {items.map((p, i) => (
        <span key={i} className="min-w-0">
          {p}
          {i < items.length - 1 ? "，" : ""}
        </span>
      ))}
    </div>
  );
}

function MoreButton({ total, shown, open, onToggle, unit = "条" }: { total: number; shown: number; open: boolean; onToggle: () => void; unit?: string }) {
  if (total <= shown && !open) return null;
  return (
    <Button size="sm" variant="ghost" className="mt-2 h-8 w-full text-xs text-muted-foreground" onClick={onToggle}>
      <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
      {open ? "收起" : `展开全部 ${total} ${unit}`}
    </Button>
  );
}

function PanelError({ text }: { text: string }) {
  return <CardEmpty icon={AlertTriangle} title={text} description="请稍后刷新重试" />;
}

function RowsSkeleton({ rows = 4, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("space-y-3", className)}>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-start gap-3">
          <Skeleton className="size-8 shrink-0 rounded-lg" />
          <div className="min-w-0 flex-1 space-y-1.5 pt-0.5">
            <Skeleton className="h-4 rounded" style={{ width: `${70 - (i % 3) * 12}%` }} />
            <Skeleton className="h-3 w-1/2 rounded" />
          </div>
          <Skeleton className="h-5 w-12 shrink-0 rounded-md" />
        </div>
      ))}
    </div>
  );
}

function useExpand(limit: number) {
  const [open, setOpen] = useState(false);
  return { open, toggle: () => setOpen((v) => !v), take: <T,>(rows: T[]) => (open ? rows : rows.slice(0, limit)), limit };
}

// ── 风险概况 ──

function actionCounts(profile?: UserRiskProfile): Record<RiskAction, number> {
  const out: Record<RiskAction, number> = { pass: 0, review: 0, captcha: 0, block: 0, ban: 0 };
  const timeline = profile?.timeline ?? [];
  if (timeline.length) {
    for (const t of timeline) out[t.action] = (out[t.action] ?? 0) + t.count;
    return out;
  }
  const s = profile?.summary;
  if (!s?.total) return out;
  out.block = s.blocked;
  out.review = s.reviewed;
  out.captcha = s.captcha;
  out.pass = Math.max(0, s.total - s.blocked - s.reviewed - s.captcha);
  return out;
}

function topPlace(ips: EntityUse[]): { place: string; share: number } | null {
  const by = new Map<string, number>();
  let total = 0;
  for (const p of ips) {
    const place = uniqText([regionName(p.country), p.province, p.city]).slice(-1)[0] ?? "";
    if (!place) continue;
    by.set(place, (by.get(place) ?? 0) + p.count);
    total += p.count;
  }
  const best = [...by.entries()].sort((a, b) => b[1] - a[1])[0];
  return best && total ? { place: best[0], share: best[1] / total } : null;
}

function riskStory(args: {
  days: number; profile: UserRiskProfile; sceneName: (k: string) => string; look: Lookup;
}): string {
  const { days, profile, sceneName, look } = args;
  const s = profile.summary;
  const total = s?.total ?? 0;
  const out: string[] = [];
  if (!total) {
    out.push(`近 ${days} 天没有风控评估记录。`);
  } else {
    const parts = [`近 ${days} 天评估 ${countText(total)}`];
    if (s!.blocked) parts.push(`其中 ${s!.blocked} 次被拦截`);
    if (s!.reviewed) parts.push(`${s!.reviewed} 次转人工复核`);
    if (s!.captcha) parts.push(`${s!.captcha} 次要求验证码`);
    if (!s!.blocked && !s!.reviewed && !s!.captcha) parts.push("全部正常放行");
    out.push(`${parts.join("，")}。`);
    const top = profile.scenes?.[0];
    let score = `最高风险分 ${s!.maxScore}，属于${scoreWord(s!.maxScore).label}，平均 ${Math.round(s!.avgScore)} 分`;
    if (top && (profile.scenes?.length ?? 0) > 1) score += `；评估主要来自${sceneName(top.key)}场景，占 ${percent(top.count, total)}`;
    else if (top) score += `；评估全部来自${sceneName(top.key)}场景`;
    out.push(`${score}。`);
  }
  const devices = profile.devices ?? [];
  const deviceTotal = Math.max(s?.devices ?? 0, devices.length);
  const ips = profile.ips ?? [];
  const ipTotal = Math.max(s?.ips ?? 0, ips.length);
  if (deviceTotal || ipTotal) {
    const bits: string[] = [];
    if (deviceTotal) {
      let d = `用过 ${deviceTotal} 台设备`;
      const emu = devices.filter((x) => look.fingerprint(x.value).emulator === "true").length;
      const root = devices.filter((x) => look.fingerprint(x.value).rooted === "true").length;
      const twin = devices.filter((x) => (x.similar ?? []).some((n) => n.banned && n.score >= 0.9)).length;
      const extra: string[] = [];
      if (emu) extra.push(`${emu} 台是模拟器`);
      if (root) extra.push(`${root} 台已 Root`);
      if (twin) extra.push(`${twin} 台与已封禁的设备高度相似`);
      if (extra.length) d += `，其中 ${extra.join("，")}`;
      bits.push(d);
    }
    if (ipTotal) {
      let p = `${deviceTotal ? "" : "用过 "}${ipTotal} 个 IP`;
      const place = topPlace(ips);
      if (place) p += place.share >= 0.999 ? `，都在${place.place}` : `，主要在${place.place}`;
      const risky = ips.filter(isRiskyIP).length;
      if (risky) p += `，${risky} 个是机房、代理或 VPN 出口`;
      bits.push(p);
    }
    out.push(`${bits.join("；")}。`);
  }
  const alerts = profile.alerts?.length ?? 0;
  const pending = (profile.cases ?? []).filter((c) => c.status === "pending").length;
  const tail: string[] = [];
  if (alerts) tail.push(`触发 ${alerts} 次行为模式告警`);
  if (pending) tail.push(`${pending} 张复核单等待处理`);
  if (tail.length) out.push(`${tail.join("，")}。`);
  return out.join("");
}

interface Notice { key: string; tone: Tone; text: string; sub?: string; href?: string; link?: string }

function noticesOf(profile: UserRiskProfile, look: Lookup, appId: number): Notice[] {
  const out: Notice[] = [];
  const push = (e: ListEntry, subject: string) => {
    const r = remaining(e.exp);
    if (r.expired) return;
    out.push({
      key: e.id,
      tone: KIND_TONE_OF[e.kind] ?? "muted",
      text: `${subject}在${KIND_LABEL[e.kind] ?? "名单"}中，${r.text}`,
      sub: e.reason || undefined,
    });
  };
  for (const e of profile.lists ?? []) push(e, "这位用户");
  for (const d of profile.devices ?? []) for (const e of d.lists ?? []) push(e, `设备 ${look.deviceLabel(d.value)}`);
  for (const p of profile.ips ?? []) for (const e of p.lists ?? []) push(e, `IP ${p.value}`);
  const order: Record<Tone, number> = { danger: 0, warning: 1, info: 2, success: 3, muted: 4 };
  out.sort((a, b) => order[a.tone] - order[b.tone]);
  const pending = (profile.cases ?? []).filter((c) => c.status === "pending").length;
  if (pending) {
    out.push({ key: "pending", tone: "warning", text: `有 ${pending} 张复核单等待处理`, href: `/risk-control?tab=cases&app=${appId}`, link: "去复核" });
  }
  return out;
}

function RiskHero({ days, onDays, profile, loading, refreshing, sceneName, look, appId }: {
  days: number; onDays: (d: number) => void; profile?: UserRiskProfile; loading: boolean; refreshing: boolean;
  sceneName: (k: string) => string; look: Lookup; appId: number;
}) {
  const s = profile?.summary;
  const total = s?.total ?? 0;
  const counts = useMemo(() => actionCounts(profile), [profile]);
  const story = useMemo(() => (profile ? riskStory({ days, profile, sceneName, look }) : ""), [days, profile, sceneName, look]);
  const notices = useMemo(() => (profile ? noticesOf(profile, look, appId) : []), [profile, look, appId]);
  const figures: Array<{ label: string; value: number; unit: string; tone?: Tone }> = [
    { label: "风控评估", value: total, unit: "次" },
    { label: "拦截与封禁", value: s?.blocked ?? 0, unit: "次", tone: s?.blocked ? "danger" : undefined },
    { label: "人工复核", value: s?.reviewed ?? 0, unit: "次", tone: s?.reviewed ? "info" : undefined },
    { label: "验证码", value: s?.captcha ?? 0, unit: "次", tone: s?.captcha ? "warning" : undefined },
    { label: "设备", value: Math.max(s?.devices ?? 0, profile?.devices?.length ?? 0), unit: "台" },
    { label: "IP", value: Math.max(s?.ips ?? 0, profile?.ips?.length ?? 0), unit: "个" },
  ];

  return (
    <Panel
      title="风险概况"
      icon={<ShieldAlert className="size-4" />}
      description={loading ? "正在读取" : total && s?.lastSeen ? `最近一次评估在 ${formatRelative(s.lastSeen)}` : `近 ${days} 天没有评估`}
      action={
        <>
          {refreshing ? <span className="text-xs text-muted-foreground">正在更新</span> : null}
          <RangeToggle days={days} onChange={onDays} />
        </>
      }
    >
      {loading ? (
        <HeroSkeleton />
      ) : (
        <div className="grid gap-5 xl:grid-cols-[auto_minmax(0,1fr)] xl:gap-6">
          <div className="flex flex-col items-center justify-center gap-6 rounded-xl bg-muted/40 p-4 sm:flex-row sm:items-center sm:justify-around xl:justify-center">
            <ScoreGauge max={s?.maxScore ?? 0} avg={s?.avgScore ?? 0} empty={!total} />
            <div aria-hidden className="hidden h-28 w-px bg-border sm:block" />
            <ActionRing counts={counts} total={total} />
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <p className="text-[15px] leading-7 text-pretty">{story}</p>
            {notices.length ? (
              <ul className="space-y-1.5">
                {notices.slice(0, 4).map((n) => (
                  <li key={n.key} className={cn("flex items-start gap-2 rounded-lg px-3 py-2 text-sm", TONE_SOFT[n.tone])}>
                    {n.tone === "danger" ? <Ban className="mt-0.5 size-3.5 shrink-0" /> : n.tone === "success" ? <ShieldCheck className="mt-0.5 size-3.5 shrink-0" /> : <ShieldQuestion className="mt-0.5 size-3.5 shrink-0" />}
                    <div className="min-w-0 flex-1">
                      <span>{n.text}</span>
                      {n.sub ? <span className="opacity-80">，原因：{n.sub}</span> : null}
                    </div>
                    {n.href ? (
                      <Link href={n.href} className="inline-flex shrink-0 items-center gap-0.5 text-xs font-medium underline-offset-4 hover:underline">
                        {n.link}
                        <ChevronRight className="size-3" />
                      </Link>
                    ) : null}
                  </li>
                ))}
              </ul>
            ) : null}
            <div className="mt-auto overflow-hidden border-t pt-4">
              <dl className="-ml-px grid grid-cols-3 gap-y-3 sm:grid-cols-6">
              {figures.map((f) => (
                <div key={f.label} className="min-w-0 border-l pl-3">
                  <dt className="truncate text-xs text-muted-foreground">{f.label}</dt>
                  <dd className={cn("mt-0.5 flex items-baseline gap-0.5 text-xl leading-7 font-semibold tracking-tight tabular-nums", f.tone && TONE_TEXT[f.tone])}>
                    <NumberFlow value={f.value} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
                    <span className="text-xs font-normal text-muted-foreground">{f.unit}</span>
                  </dd>
                </div>
              ))}
              </dl>
            </div>
          </div>
        </div>
      )}
    </Panel>
  );
}

function HeroSkeleton() {
  return (
    <div className="grid gap-5 xl:grid-cols-[auto_minmax(0,1fr)] xl:gap-6">
      <div className="flex flex-col items-center justify-center gap-6 rounded-xl bg-muted/40 p-4 sm:flex-row sm:justify-around xl:justify-center">
        <div className="flex flex-col items-center gap-2">
          <Skeleton className="h-[100px] w-[200px] rounded-t-full rounded-b-none" />
          <Skeleton className="h-3 w-20 rounded" />
        </div>
        <div className="flex items-center gap-4">
          <Skeleton className="size-[128px] rounded-full" />
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-3 w-24 rounded" />)}
          </div>
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-4">
        <div className="space-y-2.5">
          <Skeleton className="h-4 w-full rounded" />
          <Skeleton className="h-4 w-11/12 rounded" />
          <Skeleton className="h-4 w-3/5 rounded" />
        </div>
        <Skeleton className="h-9 w-full rounded-lg" />
        <div className="mt-auto grid grid-cols-3 gap-3 border-t pt-4 sm:grid-cols-6">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="space-y-1.5">
              <Skeleton className="h-3 w-12 rounded" />
              <Skeleton className="h-6 w-14 rounded" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** 风险分半环仪表：四段底色对应低、中、高、极高，实线为最高分，圆点为平均分 */
function ScoreGauge({ max, avg, empty }: { max: number; avg: number; empty: boolean }) {
  const W = 200;
  const H = 112;
  const cx = 100;
  const cy = 100;
  const r = 84;
  const stroke = 12;
  const clamp = (v: number) => Math.min(1, Math.max(0, v / 100));
  const pt = (t: number) => [cx - r * Math.cos(Math.PI * t), cy - r * Math.sin(Math.PI * t)] as const;
  const arc = (t0: number, t1: number) => {
    const [x0, y0] = pt(t0);
    const [x1, y1] = pt(t1);
    return `M${x0.toFixed(2)},${y0.toFixed(2)} A${r},${r} 0 0 1 ${x1.toFixed(2)},${y1.toFixed(2)}`;
  };
  const bands: Array<[number, number, string]> = [
    [0, 0.3, "stroke-emerald-500/25"], [0.3, 0.6, "stroke-sky-500/25"], [0.6, 0.8, "stroke-amber-500/25"], [0.8, 1, "stroke-rose-500/25"],
  ];
  const tone = scoreTone(max);
  const valueStroke: Record<Tone, string> = {
    success: "stroke-emerald-500", info: "stroke-sky-500", warning: "stroke-amber-500", danger: "stroke-rose-500", muted: "stroke-muted-foreground",
  };
  const v = clamp(max);
  const [ax, ay] = pt(clamp(avg));
  const word = scoreWord(max);
  return (
    <figure className="flex shrink-0 flex-col items-center" role="img" aria-label={empty ? "没有评估" : `最高风险分 ${max}，${word.label}，平均 ${Math.round(avg)} 分`}>
      <div className="relative" style={{ width: W, height: H }}>
        <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} aria-hidden className="overflow-visible">
          {bands.map(([a, b, cls]) => (
            <path key={a} d={arc(a + (a ? 0.004 : 0), b - (b < 1 ? 0.004 : 0))} fill="none" strokeWidth={stroke} className={cls} />
          ))}
          {!empty && v > 0.002 ? (
            <path d={arc(0, v)} fill="none" strokeWidth={stroke} strokeLinecap="round" className={cn(valueStroke[tone], "transition-[d] duration-700")} />
          ) : null}
          {!empty ? <circle cx={ax} cy={ay} r={4.5} className="fill-background stroke-foreground" strokeWidth={2} /> : null}
          <text x={cx - r} y={H - 1} textAnchor="middle" className="fill-muted-foreground text-[10px]">0</text>
          <text x={cx + r} y={H - 1} textAnchor="middle" className="fill-muted-foreground text-[10px]">100</text>
        </svg>
        <div className="absolute inset-x-0 bottom-1 flex flex-col items-center">
          {empty ? (
            <span className="text-2xl leading-8 font-semibold text-muted-foreground">—</span>
          ) : (
            <span className={cn("text-3xl leading-9 font-semibold tracking-tight tabular-nums", TONE_TEXT[tone])}>
              <NumberFlow value={max} locales="zh-CN" />
            </span>
          )}
        </div>
      </div>
      <figcaption className="mt-1 flex flex-col items-center gap-0.5">
        <span className="text-xs font-medium">{empty ? "最高风险分" : `最高风险分，${word.label}`}</span>
        <span className="text-[11px] text-muted-foreground tabular-nums">{empty ? "这段时间没有评估" : `平均 ${Math.round(avg)} 分`}</span>
      </figcaption>
    </figure>
  );
}

/** 处置构成环：每种处置占全部评估的比例 */
function ActionRing({ counts, total }: { counts: Record<RiskAction, number>; total: number }) {
  const size = 128;
  const stroke = 14;
  const r = (size - stroke) / 2;
  const C = 2 * Math.PI * r;
  const sum = ACTION_ORDER.reduce((n, a) => n + (counts[a] ?? 0), 0) || total;
  const present = ACTION_ORDER.filter((a) => (counts[a] ?? 0) > 0);
  const gap = present.length > 1 ? 3 : 0;
  const lens = present.map((a) => (sum ? (counts[a] / sum) * C : 0));
  const segs = present.map((a, i) => ({ a, len: lens[i]!, offset: lens.slice(0, i).reduce((n, l) => n + l, 0) }));
  return (
    <div className="flex items-center gap-4">
      <div className="relative shrink-0" style={{ width: size, height: size }}>
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden className="-rotate-90">
          <circle cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={stroke} className="stroke-muted" />
          {segs.map((s) => (
            <circle
              key={s.a}
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              strokeWidth={stroke}
              stroke={ACTION_COLOR[s.a]}
              strokeDasharray={`${Math.max(0.5, s.len - gap)} ${C}`}
              strokeDashoffset={-s.offset}
              className="transition-[stroke-dasharray,stroke-dashoffset] duration-700"
            />
          ))}
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-xl leading-6 font-semibold tabular-nums">
            <NumberFlow value={sum} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
          </span>
          <span className="text-[11px] text-muted-foreground">次评估</span>
        </div>
      </div>
      <ul className="min-w-[7.5rem] space-y-1.5 text-xs">
        {ACTION_ORDER.map((a) => (
          <li key={a} className={cn("flex items-center gap-2", !counts[a] && "opacity-45")}>
            <span className="size-2 shrink-0 rounded-full" style={{ background: ACTION_COLOR[a] }} />
            <span className="flex-1">{ACTION_LABEL[a]}</span>
            <span className="tabular-nums text-muted-foreground">{counts[a] ? percent(counts[a], sum) : "0"}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// ── 风险时间线 ──

const timelineConfig: ChartConfig = {
  ...Object.fromEntries(ACTION_ORDER.map((a) => [a, { label: ACTION_LABEL[a], color: ACTION_COLOR[a] }])),
  maxScore: { label: "最高风险分", color: "var(--foreground)" },
} as ChartConfig;

type DayRow = { key: string; label: string; full: string; total: number; maxScore?: number } & Partial<Record<RiskAction, number>>;

function TimelinePanel({ timeline, days, loading, error }: {
  timeline: NonNullable<UserRiskProfile["timeline"]>; days: number; loading: boolean; error?: string;
}) {
  const { rows, totals, busiest, riskiest } = useMemo(() => {
    const byDay = new Map<string, DayRow>();
    const make = (key: string, at: Date): DayRow => ({
      key,
      label: `${at.getMonth() + 1}/${at.getDate()}`,
      full: formatWith(at, { month: "long", day: "numeric", weekday: "short" }),
      total: 0,
    });
    const today = new Date();
    for (let i = days - 1; i >= 0; i--) {
      const at = new Date(today.getFullYear(), today.getMonth(), today.getDate() - i);
      const key = localDateKey(at);
      byDay.set(key, make(key, at));
    }
    const totals: Record<RiskAction, number> = { pass: 0, review: 0, captcha: 0, block: 0, ban: 0 };
    for (const t of timeline) {
      const key = localDateKey(t.day);
      if (!key) continue;
      const row = byDay.get(key) ?? make(key, new Date(t.day));
      row[t.action] = (row[t.action] ?? 0) + t.count;
      row.total += t.count;
      row.maxScore = Math.max(row.maxScore ?? 0, t.maxScore);
      byDay.set(key, row);
      totals[t.action] += t.count;
    }
    const rows = [...byDay.values()].sort((a, b) => a.key.localeCompare(b.key));
    const active = rows.filter((r) => r.total > 0);
    const busiest = active.reduce<DayRow | null>((m, r) => (!m || r.total > m.total ? r : m), null);
    const riskiest = active.reduce<DayRow | null>((m, r) => (!m || (r.maxScore ?? 0) > (m.maxScore ?? 0) ? r : m), null);
    return { rows, totals, busiest, riskiest };
  }, [timeline, days]);

  const sum = ACTION_ORDER.reduce((n, a) => n + totals[a], 0);
  const highlight = busiest && riskiest
    ? busiest.key === riskiest.key
      ? `${busiest.full}最活跃，评估 ${busiest.total} 次，最高 ${busiest.maxScore ?? 0} 分。`
      : `${riskiest.full}风险最高，达到 ${riskiest.maxScore ?? 0} 分；${busiest.full}评估最多，共 ${busiest.total} 次。`
    : "";

  return (
    <Panel title="风险时间线" icon={<History className="size-4" />} description="柱为当天的评估结果，折线为当天最高风险分，虚线为高风险分界">
      {loading ? (
        <div className="space-y-3">
          <div className="flex gap-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-6 w-20 rounded-full" />)}</div>
          <Skeleton className="h-[260px] rounded-xl" />
        </div>
      ) : error ? (
        <PanelError text="暂时读不到风险时间线" />
      ) : !sum ? (
        <CardEmpty icon={History} title="这段时间没有评估" description="用户登录、抽奖、兑换等操作经过风控后，会按天显示在这里" />
      ) : (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-1.5">
            {ACTION_ORDER.filter((a) => totals[a]).map((a) => (
              <span key={a} className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs">
                <span className="size-2 rounded-full" style={{ background: ACTION_COLOR[a] }} />
                {ACTION_LABEL[a]}
                <span className="font-medium tabular-nums">{totals[a].toLocaleString("zh-CN")}</span>
              </span>
            ))}
          </div>
          {highlight ? <p className="text-sm text-muted-foreground">{highlight}</p> : null}
          <ChartContainer config={timelineConfig} className="aspect-auto h-[260px] w-full">
            <ComposedChart data={rows} margin={{ left: -12, right: 0, top: 8, bottom: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={20} />
              <YAxis yAxisId="n" tickLine={false} axisLine={false} fontSize={11} allowDecimals={false} width={40} />
              <YAxis yAxisId="s" orientation="right" tickLine={false} axisLine={false} fontSize={11} domain={[0, 100]} ticks={[0, 30, 60, 80, 100]} width={32} />
              <ReferenceLine yAxisId="s" y={60} stroke="var(--muted-foreground)" strokeOpacity={0.5} strokeDasharray="4 4" />
              <ChartTooltip
                cursor={{ fill: "var(--muted)", opacity: 0.5 }}
                content={<ChartTooltipContent labelFormatter={(_, payload) => (payload?.[0]?.payload as DayRow | undefined)?.full ?? ""} />}
              />
              {ACTION_ORDER.map((a, i) => (
                <Bar
                  key={a}
                  yAxisId="n"
                  dataKey={a}
                  stackId="a"
                  fill={`var(--color-${a})`}
                  radius={i === ACTION_ORDER.length - 1 ? [3, 3, 0, 0] : 0}
                  maxBarSize={22}
                />
              ))}
              <Line yAxisId="s" dataKey="maxScore" type="monotone" stroke="var(--color-maxScore)" strokeWidth={1.5} dot={{ r: 2 }} activeDot={{ r: 3.5 }} connectNulls />
            </ComposedChart>
          </ChartContainer>
        </div>
      )}
    </Panel>
  );
}

// ── 名单与处置 ──

type Target = DisposeInput["targets"][number];

interface DisposeAction {
  key: string;
  title: string;
  hint: string;
  icon: LucideIcon;
  tone: Tone;
  input: Omit<DisposeInput, "durationSeconds" | "reason">;
}

function DisposePanel({ appId, userId, name, account, lists, devices, ips, look, loading }: {
  appId: number; userId: number; name: string; account?: string; lists: ListEntry[]; devices: EntityUse[]; ips: EntityUse[];
  look: Lookup; loading: boolean;
}) {
  const dispose = useDispose();
  const [pending, setPending] = useState<DisposeAction | null>(null);
  const [reason, setReason] = useState("");
  const [duration, setDuration] = useState(86400);
  const device = devices[0]?.value;
  const ip = ips[0]?.value;
  const deviceName = device ? look.deviceLabel(device) : "";

  const entries = useMemo(() => {
    const all = [
      ...lists.map((l) => ({ entry: l, subject: "这位用户" })),
      ...devices.flatMap((d) => (d.lists ?? []).map((l) => ({ entry: l, subject: `设备 ${look.deviceLabel(d.value)}` }))),
      ...ips.flatMap((p) => (p.lists ?? []).map((l) => ({ entry: l, subject: `IP ${p.value}` }))),
    ];
    const order: Record<string, number> = { block: 0, watch: 1, allow: 2 };
    return all.sort((a, b) => (order[a.entry.kind] ?? 3) - (order[b.entry.kind] ?? 3));
  }, [lists, devices, ips, look]);

  const base = { appId, userId, account, deviceId: device, ip };
  const actions: DisposeAction[] = [
    {
      key: "block", title: "拉黑用户与最近设备", icon: Ban, tone: "danger",
      hint: device ? `用户与 ${deviceName} 加入黑名单` : "用户加入黑名单",
      input: { ...base, kind: "block", targets: ["user", ...(device ? (["device"] as const) : [])] },
    },
    { key: "ban", title: "封号并踢下线", icon: ShieldAlert, tone: "danger", hint: "封禁账号，所有登录立即失效", input: { ...base, kind: "block", targets: ["ban_account"] } },
    { key: "watch", title: "加入观察名单", icon: ShieldQuestion, tone: "warning", hint: "重点留意，不影响正常使用", input: { ...base, kind: "watch", targets: ["user"] } },
    { key: "allow", title: "加入白名单", icon: ShieldCheck, tone: "success", hint: "风控评估直接放行", input: { ...base, kind: "allow", targets: ["user"] } },
    ...(ip
      ? [{ key: "firewall", title: "防火墙封最近 IP", icon: Globe, tone: "danger" as const, hint: `在入口拒绝 ${ip} 的全部请求`, input: { ...base, kind: "block" as const, targets: ["firewall" as const] } }]
      : []),
  ];

  const objectText = (targets: Target[]) => {
    const out: string[] = [];
    if (targets.includes("user")) out.push(`用户 ${name}`);
    if (targets.includes("ban_account")) out.push(`账号 ${name}`);
    if (targets.includes("device") && device) out.push(`设备 ${deviceName}`);
    if (targets.includes("firewall") && ip) out.push(`IP ${ip}`);
    return out.join("、");
  };

  const close = () => {
    setPending(null);
    setReason("");
  };

  return (
    <Panel title="名单与处置" icon={<ShieldCheck className="size-4" />} description="用户、最近设备与 IP 当前所在的名单">
      {loading ? (
        <div className="space-y-4">
          <Skeleton className="h-16 rounded-xl" />
          <div className="grid gap-2 sm:grid-cols-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-[60px] rounded-xl" />)}</div>
        </div>
      ) : (
        <div className="space-y-4">
          {entries.length ? (
            <ul className="space-y-1.5">
              {entries.map(({ entry: l, subject }) => {
                const tone = KIND_TONE_OF[l.kind] ?? "muted";
                const r = remaining(l.exp);
                return (
                  <li key={l.id} className={cn("rounded-xl border px-3 py-2.5", r.expired ? "opacity-60" : TONE_BORDER[tone])}>
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex min-w-0 items-center gap-2 text-sm">
                        <span className={cn("size-2 shrink-0 rounded-full", TONE_DOT[tone])} />
                        <span className="min-w-0 truncate">{subject}在{KIND_LABEL[l.kind] ?? "名单"}中</span>
                      </div>
                      <span className={cn("shrink-0 text-xs", r.expired ? "text-muted-foreground" : TONE_TEXT[tone])}>{r.text}</span>
                    </div>
                    <MetaLine className="mt-0.5 pl-4" parts={[l.reason ? `原因：${l.reason}` : null, l.by ? `由 ${l.by} 加入` : null, l.at ? <When iso={l.at} /> : null]} />
                  </li>
                );
              })}
            </ul>
          ) : (
            <div className="flex items-center gap-3 rounded-xl bg-muted/40 px-3 py-3">
              <TonalIcon icon={ShieldCheck} tone="emerald" size="sm" />
              <div className="min-w-0">
                <div className="text-sm">不在任何名单中</div>
                <div className="text-xs text-muted-foreground">用户、最近设备与 IP 均未被列入名单</div>
              </div>
            </div>
          )}
          <div>
            <div className="mb-2 text-xs font-medium text-muted-foreground">处置</div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-1 2xl:grid-cols-2">
              {actions.map((a) => (
                <button
                  key={a.key}
                  type="button"
                  onClick={() => setPending(a)}
                  className="group flex min-w-0 items-start gap-3 rounded-xl border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                >
                  <TonalIcon icon={a.icon} tone={KIT_TONE[a.tone]} size="sm" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">{a.title}</span>
                    <span className="block truncate text-xs text-muted-foreground">{a.hint}</span>
                  </span>
                  <ChevronRight className="mt-1.5 size-3.5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
      <AlertDialog open={!!pending} onOpenChange={(o) => !o && close()}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
            <AlertDialogDescription>
              {pending ? `将对${objectText(pending.input.targets)}执行，${pending.hint}。处置立即生效并记入审计。` : null}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">期限</Label>
              <Segmented
                value={String(duration)}
                onChange={(v) => setDuration(Number(v))}
                options={DURATION_PRESETS.map((p) => ({ value: String(p.seconds), label: p.label }))}
                ariaLabel="期限"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="risk-dispose-reason" className="text-xs text-muted-foreground">原因</Label>
              <Textarea id="risk-dispose-reason" placeholder="会显示在名单与封禁记录上" value={reason} onChange={(e) => setReason(e.target.value)} rows={3} />
            </div>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={dispose.isPending}
              className={cn(pending?.tone === "danger" && "bg-destructive text-white hover:bg-destructive/90")}
              onClick={() => {
                if (!pending) return;
                dispose.mutate({ ...pending.input, durationSeconds: duration, reason: reason.trim() || pending.title }, {
                  onSuccess: (res) => toast.success("已处置", { description: Object.values(res ?? {}).join("，") || undefined }),
                  onError: (e) => toast.error("处置失败", { description: (e as Error).message }),
                });
                close();
              }}
            >
              确认，期限{duration ? ` ${formatWindow(duration)}` : "为永久"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Panel>
  );
}

// ── 设备卡片墙 ──

function DevicesPanel({ devices, total, appKey, look, loading }: {
  devices: EntityUse[]; total: number; appKey: string; look: Lookup; loading: boolean;
}) {
  const more = useExpand(6);
  const count = Math.max(total, devices.length);
  const emu = devices.filter((d) => look.fingerprint(d.value).emulator === "true").length;
  const twin = devices.filter((d) => (d.similar ?? []).some((n) => n.banned && n.score >= 0.9)).length;
  const desc = !count
    ? undefined
    : [
        `用过 ${count} 台设备`,
        emu ? `${emu} 台是模拟器` : "",
        twin ? `${twin} 台与已封禁的设备高度相似` : "",
      ].filter(Boolean).join("，");

  return (
    <Panel title="设备" icon={<Fingerprint className="size-4" />} description={desc}>
      {loading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="space-y-3 rounded-xl border p-4">
              <div className="flex items-center gap-3">
                <Skeleton className="size-10 rounded-xl" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-2/3 rounded" />
                  <Skeleton className="h-3 w-1/3 rounded" />
                </div>
              </div>
              <Skeleton className="h-3 w-4/5 rounded" />
              <Skeleton className="h-12 rounded-lg" />
            </div>
          ))}
        </div>
      ) : !devices.length ? (
        <CardEmpty icon={Smartphone} title="没有设备记录" description="用户在客户端产生评估后，会在这里列出用过的设备" />
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {more.take(devices).map((d, i) => <DeviceCard key={d.value} use={d} latest={i === 0} appKey={appKey} look={look} />)}
          </div>
          <MoreButton total={devices.length} shown={more.limit} open={more.open} onToggle={more.toggle} unit="台" />
        </>
      )}
    </Panel>
  );
}

function DeviceCard({ use, latest, appKey, look }: { use: EntityUse; latest: boolean; appKey: string; look: Lookup }) {
  const fp = look.fingerprint(use.value);
  const node = look.deviceNode(use.value);
  const Icon = deviceIcon(fp);
  const title = deviceTitle(fp) || "未知型号";
  const system = deviceSystem(fp);
  const lists = (use.lists ?? []).filter((l) => !remaining(l.exp).expired);
  const blocked = lists.some((l) => l.kind === "block") || node?.props?.banned === true;
  const similar = (use.similar ?? []).filter((n) => n.score >= 0.9).sort((a, b) => Number(b.banned) - Number(a.banned) || b.score - a.score);
  const bannedTwins = similar.filter((n) => n.banned).length;
  const tone: Tone = blocked || bannedTwins ? "danger" : fp.emulator === "true" || fp.rooted === "true" ? "warning" : "muted";
  const place = placeText(use.country, null, use.city);
  const env = uniqText([
    fp.app_version ? `客户端 ${fp.app_version}` : "",
    fp.screen ? `屏幕 ${fp.screen.replace(/x/i, "×")}` : "",
    languageName(fp.locale),
    fp.timezone ?? "",
  ]);

  return (
    <article className={cn("flex min-w-0 flex-col gap-3 rounded-xl border p-4", TONE_BORDER[tone])}>
      <header className="flex items-start gap-3">
        <TonalIcon icon={Icon} tone={KIT_TONE[tone === "muted" ? "info" : tone]} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <h4 className="truncate text-sm font-semibold" title={title}>{title}</h4>
            {latest ? <Badge variant="secondary" size="sm">最近使用</Badge> : null}
          </div>
          <div className="truncate text-xs text-muted-foreground">{system || "系统未知"}</div>
        </div>
      </header>

      {fp.emulator === "true" || fp.rooted === "true" || lists.length ? (
        <div className="flex flex-wrap gap-1">
          {fp.emulator === "true" ? <Pill tone="danger">模拟器</Pill> : null}
          {fp.rooted === "true" ? <Pill tone="warning">已 Root</Pill> : null}
          {lists.map((l) => <Pill key={l.id} tone={KIND_TONE_OF[l.kind] ?? "muted"} title={l.reason}>{KIND_LABEL[l.kind] ?? "名单"}</Pill>)}
        </div>
      ) : null}

      <p className="text-sm leading-6">
        首次 <When iso={use.firstSeen} />，最近 <When iso={use.lastSeen} />，评估 {countText(use.count)}
        {place ? `，常在${place}` : ""}。
      </p>

      {env.length ? <MetaLine parts={env} /> : null}

      {use.similar ? (
        similar.length ? (
          <div className={cn("rounded-lg px-3 py-2.5", bannedTwins ? TONE_SOFT.danger : "bg-muted/50")}>
            <div className="flex items-center gap-1.5 text-xs font-medium">
              <Sparkles className="size-3.5 shrink-0" />
              {bannedTwins ? `与 ${similar.length} 台设备高度相似，其中 ${bannedTwins} 台的用户已被封禁` : `与 ${similar.length} 台设备高度相似`}
            </div>
            <ul className="mt-1.5 space-y-0.5">
              {similar.slice(0, 4).map((n) => (
                <li key={n.deviceId} className="flex items-center justify-between gap-2 text-xs">
                  {n.userId ? (
                    <Link href={`/app-users/${appKey}/${n.userId}?tab=risk`} className="min-w-0 truncate underline-offset-4 hover:underline" title={`设备编号 ${n.deviceId}`}>
                      {twinName(n)}
                    </Link>
                  ) : (
                    <span className="min-w-0 truncate" title={`设备编号 ${n.deviceId}`}>{twinName(n)}</span>
                  )}
                  <span className="flex shrink-0 items-center gap-1.5 tabular-nums">
                    {n.banned ? <span className="text-rose-600 dark:text-rose-400">已封禁</span> : null}
                    <span className="text-muted-foreground">相似度 {Math.round(n.score * 100)}%</span>
                  </span>
                </li>
              ))}
            </ul>
            {similar.length > 4 ? <div className="mt-1 text-xs text-muted-foreground">另有 {similar.length - 4} 台</div> : null}
          </div>
        ) : (
          <div className="text-xs text-muted-foreground">未发现高度相似的设备</div>
        )
      ) : null}

      <footer className="mt-auto truncate border-t pt-2 font-mono text-[11px] text-muted-foreground" title={use.value}>
        设备编号 {use.value}
      </footer>
    </article>
  );
}

// ── IP ──

const PLACE_COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"];

function IPsPanel({ ips, total, loading }: { ips: EntityUse[]; total: number; loading: boolean }) {
  const more = useExpand(8);
  const places = useMemo(() => {
    const by = new Map<string, number>();
    let sum = 0;
    for (const p of ips) {
      const key = ipPlace(p) || "位置未知";
      by.set(key, (by.get(key) ?? 0) + p.count);
      sum += p.count;
    }
    const sorted = [...by.entries()].sort((a, b) => b[1] - a[1]);
    const head = sorted.slice(0, 4);
    const rest = sorted.slice(4).reduce((n, [, c]) => n + c, 0);
    return { list: rest ? [...head, ["其他", rest] as [string, number]] : head, sum };
  }, [ips]);
  const count = Math.max(total, ips.length);
  const lead = places.list[0];
  const desc = count ? `用过 ${count} 个 IP${lead && lead[0] !== "位置未知" ? `，${places.list.length === 1 ? "都在" : "主要在"}${lead[0]}` : ""}` : undefined;

  return (
    <Panel title="IP" icon={<Globe className="size-4" />} description={desc}>
      {loading ? (
        <div className="space-y-4">
          <Skeleton className="h-2.5 rounded-full" />
          <RowsSkeleton rows={5} />
        </div>
      ) : !ips.length ? (
        <CardEmpty icon={Globe} title="没有 IP 记录" description="用户产生评估后，会在这里列出用过的网络地址" />
      ) : (
        <div className="space-y-4">
          {places.sum ? (
            <div className="space-y-2">
              <div className="flex h-2.5 overflow-hidden rounded-full bg-muted">
                {places.list.map(([k, c], i) => (
                  <div key={k} title={`${k}，${percent(c, places.sum)}`} className="h-full border-r-2 border-card last:border-r-0" style={{ width: `${(c / places.sum) * 100}%`, background: PLACE_COLORS[i % PLACE_COLORS.length] }} />
                ))}
              </div>
              <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
                {places.list.map(([k, c], i) => (
                  <span key={k} className="inline-flex items-center gap-1.5">
                    <span className="size-2 rounded-full" style={{ background: PLACE_COLORS[i % PLACE_COLORS.length] }} />
                    {k}
                    <span className="tabular-nums text-muted-foreground">{percent(c, places.sum)}</span>
                  </span>
                ))}
              </div>
            </div>
          ) : null}
          <ul className="divide-y">
            {more.take(ips).map((p) => {
              const flags = ipFlags(p);
              const lists = (p.lists ?? []).filter((l) => !remaining(l.exp).expired);
              const where = ipPlace(p);
              const isp = ispText(p);
              const line = p.net?.v6 && p.net.line && p.net.line !== p.value ? p.net.line : "";
              return (
                <li key={p.value} className="flex items-start gap-3 py-2.5 first:pt-0 last:pb-0">
                  <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-muted">
                    {/^[A-Za-z]{2}$/.test(p.country) ? <CountryFlag code={p.country} size={16} /> : <MapPin className="size-3.5 text-muted-foreground" />}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <span className="truncate font-mono text-sm" title={p.value}>{p.value}</span>
                      {flags.map((f) => <Pill key={f.label} tone={f.tone}>{f.label}</Pill>)}
                      {lists.map((l) => <Pill key={l.id} tone={KIND_TONE_OF[l.kind] ?? "muted"} title={l.reason}>{KIND_LABEL[l.kind] ?? "名单"}</Pill>)}
                    </div>
                    <MetaLine parts={[where || "位置未知", isp || "运营商未知", `首次 ${formatRelative(p.firstSeen)}`]} />
                    {line ? (
                      <div className="truncate text-xs text-muted-foreground" title="同一条 IPv6 线路上的地址按整条线路计">
                        所在线路 <span className="font-mono">{line}</span>
                      </div>
                    ) : null}
                  </div>
                  <div className="shrink-0 text-right">
                    <div className="text-xs"><When iso={p.lastSeen} /></div>
                    <div className="text-xs text-muted-foreground tabular-nums">{countText(p.count)}</div>
                  </div>
                </li>
              );
            })}
          </ul>
          <MoreButton total={ips.length} shown={more.limit} open={more.open} onToggle={more.toggle} unit="个" />
        </div>
      )}
    </Panel>
  );
}

// ── 触达场景与命中规则 ──

function ScenesPanel({ rows, total, sceneName, loading, error }: {
  rows: SceneUse[]; total: number; sceneName: (k: string) => string; loading: boolean; error?: string;
}) {
  const more = useExpand(6);
  const max = Math.max(1, ...rows.map((r) => r.count));
  const sum = total || rows.reduce((n, r) => n + r.count, 0);
  const anyFlagged = rows.some((r) => (r.flagged ?? 0) > 0);
  return (
    <Panel
      title="触达场景"
      icon={<Activity className="size-4" />}
      description={rows.length ? `经过 ${rows.length} 个风控场景` : undefined}
      action={
        anyFlagged ? (
          <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
            <span className="inline-flex items-center gap-1"><span className="size-2 rounded-full bg-rose-500" />拦截</span>
            <span className="inline-flex items-center gap-1"><span className="size-2 rounded-full bg-amber-500" />复核或验证码</span>
            <span className="inline-flex items-center gap-1"><span className="size-2 rounded-full bg-primary/60" />放行</span>
          </div>
        ) : undefined
      }
    >
      {loading ? (
        <BarsSkeleton />
      ) : error ? (
        <PanelError text="暂时读不到场景分布" />
      ) : !rows.length ? (
        <CardEmpty icon={Activity} title="没有经过风控场景" />
      ) : (
        <>
          <ul className="space-y-2.5">
            {more.take(rows).map((r) => {
              const blocked = Math.min(r.blocked ?? 0, r.count);
              const flagged = Math.min(Math.max(r.flagged ?? 0, blocked), r.count);
              const held = flagged - blocked;
              const passed = r.count - flagged;
              const width = Math.max(2, (r.count / max) * 100);
              const seg = (n: number) => `${r.count ? (n / r.count) * 100 : 0}%`;
              const note = [
                blocked ? `拦截 ${blocked} 次` : "",
                held ? `${held} 次转复核或验证码` : "",
              ].filter(Boolean).join("，");
              return (
                <li key={r.key} className="space-y-1">
                  <div className="flex items-baseline justify-between gap-3 text-sm">
                    <span className="min-w-0 truncate">{sceneName(r.key)}</span>
                    <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                      {countText(r.count)}，占 {percent(r.count, sum)}
                    </span>
                  </div>
                  <div className="h-2 overflow-hidden rounded-full bg-muted">
                    <div className="flex h-full overflow-hidden rounded-full transition-[width] duration-500" style={{ width: `${width}%` }}>
                      {blocked ? <div className="h-full bg-rose-500" style={{ width: seg(blocked) }} /> : null}
                      {held ? <div className="h-full bg-amber-500" style={{ width: seg(held) }} /> : null}
                      {passed ? <div className="h-full bg-primary/60" style={{ width: seg(passed) }} /> : null}
                    </div>
                  </div>
                  {note ? (
                    <div className={cn("text-xs", blocked ? TONE_TEXT.danger : TONE_TEXT.warning)}>
                      {note}，未直接放行占 {percent(flagged, r.count)}
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
          <MoreButton total={rows.length} shown={more.limit} open={more.open} onToggle={more.toggle} unit="个" />
        </>
      )}
    </Panel>
  );
}

const RULE_DECISION: Record<RiskAction, string> = {
  pass: "直接放行", review: "直接转人工复核", captcha: "直接要求验证码", block: "直接拦截", ban: "直接封禁",
};

type RuleInfo = { name?: string; dimension?: string; score?: number; mode?: Rule["mode"]; decision?: string; note?: string };

function RulesPanel({ rows, names, meta, total, loading, error }: {
  rows: KV[]; names?: Record<string, string>; meta?: UserRiskProfile["ruleMeta"]; total: number; loading: boolean; error?: string;
}) {
  // 画像带了规则信息（ruleMeta / names）就不再请求规则配置；老版本接口才退回去查
  const rules = useFallbackRules(!meta && !names && rows.length > 0);
  const byKey = useMemo(() => {
    const m = new Map<string, Rule>();
    for (const r of rules.data ?? []) if (!m.has(r.key) || r.scene === "*") m.set(r.key, r);
    return m;
  }, [rules.data]);
  const known = !!(meta || names || rules.data);
  const infoOf = (key: string): RuleInfo => {
    const m = meta?.[key];
    const r = byKey.get(key);
    return {
      name: m?.name || names?.[key] || r?.name,
      dimension: m?.dimension ?? r?.dimension,
      score: m?.score ?? r?.score,
      mode: m?.mode ?? r?.mode,
      decision: m?.decision ?? r?.decision,
      note: r?.description || r?.reason,
    };
  };
  const more = useExpand(6);
  const max = Math.max(1, ...rows.map((r) => r.count));
  return (
    <Panel title="命中最多的规则" icon={<ShieldAlert className="size-4" />} description={rows.length ? `共命中 ${rows.length} 条规则` : undefined}>
      {loading ? (
        <BarsSkeleton />
      ) : error ? (
        <PanelError text="暂时读不到命中规则" />
      ) : !rows.length ? (
        <CardEmpty icon={ShieldCheck} title="没有命中任何风险规则" />
      ) : (
        <>
          <ul className="space-y-3">
            {more.take(rows).map((r) => {
              const rule = infoOf(r.key);
              const observe = rule.mode === "observe";
              const decision = rule.decision as RiskAction | undefined;
              return (
                <li key={r.key} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate text-sm" title={rule.note || undefined}>{rule.name || (known ? "已删除的规则" : r.key)}</span>
                    {rule.dimension ? <Pill tone="muted">{DIM_LABEL[rule.dimension] ?? "其他"}</Pill> : null}
                    {observe ? <Pill tone="info">观察中</Pill> : null}
                    {!observe && decision && RULE_DECISION[decision] ? <Pill tone={ACTION_TONE_OF[decision]}>{RULE_DECISION[decision]}</Pill> : null}
                    {!observe && !decision && rule.score ? <Pill tone={rule.score >= 40 ? "danger" : rule.score >= 20 ? "warning" : "muted"}>+{rule.score} 分</Pill> : null}
                  </div>
                  <span className="text-right text-xs text-muted-foreground tabular-nums">
                    {countText(r.count)}{total ? `，${percent(r.count, total)}` : ""}
                  </span>
                  <div className="col-span-2 h-1.5 overflow-hidden rounded-full bg-muted">
                    <div className={cn("h-full rounded-full transition-[width] duration-500", observe ? "bg-sky-500/60" : "bg-rose-500/70")} style={{ width: `${Math.max(2, (r.count / max) * 100)}%` }} />
                  </div>
                </li>
              );
            })}
          </ul>
          <MoreButton total={rows.length} shown={more.limit} open={more.open} onToggle={more.toggle} />
        </>
      )}
    </Panel>
  );
}

function BarsSkeleton() {
  return (
    <div className="space-y-3">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className="space-y-1.5">
          <div className="flex justify-between"><Skeleton className="h-4 w-28 rounded" /><Skeleton className="h-3 w-16 rounded" /></div>
          <Skeleton className="h-2 rounded-full" style={{ width: `${90 - i * 18}%` }} />
        </div>
      ))}
    </div>
  );
}

// ── 决策记录 ──

function DecisionsPanel({ rows, appId, userId, sceneName, look, loading, error, onOpen }: {
  rows: DecisionRow[]; appId: number; userId: number; sceneName: (k: string) => string; look: Lookup; loading: boolean; error?: string;
  onOpen: (id: string) => void;
}) {
  const [filter, setFilter] = useState<"all" | "risky">("all");
  const more = useExpand(8);
  const risky = rows.filter((r) => r.action !== "pass");
  const shown = filter === "risky" ? risky : rows;
  return (
    <Panel
      title="决策记录"
      icon={<ScrollText className="size-4" />}
      description={rows.length ? `最近 ${rows.length} 次评估，其中 ${risky.length} 次未直接放行` : undefined}
      action={
        <>
          {rows.length ? (
            <Segmented value={filter} onChange={setFilter} options={[{ value: "all", label: "全部" }, { value: "risky", label: "未放行" }]} ariaLabel="筛选决策" />
          ) : null}
          <Button asChild size="sm" variant="ghost" className="h-8 text-xs">
            <Link href={`/risk-control?tab=decisions&app=${appId}&user=${userId}`}>
              全部记录
              <ChevronRight className="size-3.5" />
            </Link>
          </Button>
        </>
      }
    >
      {loading ? (
        <RowsSkeleton rows={6} />
      ) : error ? (
        <PanelError text="暂时读不到决策记录" />
      ) : !shown.length ? (
        <CardEmpty icon={ScrollText} title={rows.length ? "这段时间的评估都已放行" : "没有决策记录"} />
      ) : (
        <>
          <ul className="-mx-2 space-y-0.5">
            {more.take(shown).map((d) => {
              const tone = ACTION_TONE_OF[d.action] ?? "muted";
              const place = placeText(d.country, null, d.city);
              return (
                <li key={d.id}>
                  <button
                    type="button"
                    onClick={() => onOpen(d.id)}
                    className="group flex w-full min-w-0 items-start gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                  >
                    <span className={cn("mt-1 grid size-6 shrink-0 place-items-center rounded-full", TONE_SOFT[tone])}>
                      <span className={cn("size-2 rounded-full", TONE_DOT[tone])} />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="min-w-0 truncate text-sm font-medium">
                          {sceneName(d.scene)}请求<span className={TONE_TEXT[tone]}>{ACTION_RESULT[d.action] ?? ACTION_LABEL[d.action]}</span>
                        </span>
                      </span>
                      <span className="mt-0.5 block text-xs leading-5 text-pretty text-foreground/80">{decisionReason(d)}</span>
                      <MetaLine parts={[<When key="t" iso={d.ts} />, place, d.ip, d.deviceId ? look.deviceLabel(d.deviceId) : null]} />
                    </span>
                    <span className="flex shrink-0 items-center gap-1">
                      <ScoreChip score={d.score} />
                      <ChevronRight className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          <MoreButton total={shown.length} shown={more.limit} open={more.open} onToggle={more.toggle} />
        </>
      )}
    </Panel>
  );
}

// ── 业务事件 ──

function eventLook(type: string): { icon: LucideIcon; tone: Tone } {
  if (type === "login.fail" || type === "login.locked") return { icon: LogIn, tone: "danger" };
  if (type.startsWith("login") || type === "oauth.login" || type === "qr.confirm") return { icon: type === "qr.confirm" ? QrCode : LogIn, tone: "info" };
  if (type.startsWith("register")) return { icon: UserPlus, tone: "success" };
  if (type === "payment.refund") return { icon: Wallet, tone: "warning" };
  if (type.startsWith("payment")) return { icon: Wallet, tone: "success" };
  if (type.startsWith("invite")) return { icon: Users, tone: "info" };
  if (type === "signin") return { icon: CalendarCheck, tone: "muted" };
  if (type === "lottery.draw" || type === "ad.reward" || type === "vip.trial") return { icon: Gift, tone: "muted" };
  if (type === "cardkey.redeem" || type === "points.redeem") return { icon: Ticket, tone: "muted" };
  if (type === "account.ban") return { icon: Ban, tone: "danger" };
  if (type === "account.unban") return { icon: ShieldCheck, tone: "success" };
  if (type === "cep.alert") return { icon: Siren, tone: "danger" };
  if (type === "geo.anomaly") return { icon: MapPin, tone: "warning" };
  if (type.startsWith("password") || type.startsWith("passkey") || type === "verify_code.sent") return { icon: KeyRound, tone: "muted" };
  if (type.startsWith("oauth")) return { icon: Link2, tone: "muted" };
  if (type.startsWith("profile") || type.startsWith("contact") || type.startsWith("account")) return { icon: UserCog, tone: type === "account.delete_request" ? "warning" : "muted" };
  if (type === "decision") return { icon: ScrollText, tone: "muted" };
  return { icon: Activity, tone: "muted" };
}

function eventSentence(e: EventRow, sceneName: (k: string) => string): string {
  let head = EVENT_LABEL[e.type] ? eventName(e.type) : "其他业务事件";
  if (e.type === "decision" && e.scene) head = `${sceneName(e.scene)}风控评估`;
  const extra: string[] = [];
  if (e.amount) extra.push(`金额 ${e.amount.toLocaleString("zh-CN")}`);
  if (e.targetUserId) extra.push(`对象为用户 #${e.targetUserId}`);
  return extra.length ? `${head}，${extra.join("，")}` : head;
}

function dayTitle(iso: string) {
  const key = localDateKey(iso);
  const now = new Date();
  if (key === localDateKey(now)) return "今天";
  if (key === localDateKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1))) return "昨天";
  return formatWith(iso, { month: "long", day: "numeric", weekday: "short" });
}

function EventsPanel({ rows, sceneName, look, loading, error }: {
  rows: EventRow[]; sceneName: (k: string) => string; look: Lookup; loading: boolean; error?: string;
}) {
  const more = useExpand(10);
  const groups: Array<{ key: string; title: string; items: EventRow[] }> = [];
  for (const e of more.take(rows)) {
    const key = localDateKey(e.ts);
    const last = groups[groups.length - 1];
    if (last && last.key === key) last.items.push(e);
    else groups.push({ key, title: dayTitle(e.ts), items: [e] });
  }
  const failed = rows.filter((e) => eventLook(e.type).tone === "danger").length;
  return (
    <Panel
      title="业务事件"
      icon={<History className="size-4" />}
      description={rows.length ? `最近 ${rows.length} 条${failed ? `，${failed} 条异常` : ""}` : undefined}
    >
      {loading ? (
        <RowsSkeleton rows={6} />
      ) : error ? (
        <PanelError text="暂时读不到业务事件" />
      ) : !rows.length ? (
        <CardEmpty icon={History} title="没有业务事件" description="登录、注册、支付、签到、邀请等操作会出现在这里" />
      ) : (
        <>
          <div className="space-y-4">
            {groups.map((g) => (
              <section key={g.key}>
                <h4 className="mb-1.5 text-xs font-medium text-muted-foreground">{g.title}</h4>
                <ol className="relative space-y-2.5 before:absolute before:top-2 before:bottom-2 before:left-[13px] before:w-px before:bg-border">
                  {g.items.map((e) => {
                    const { icon: Icon, tone } = eventLook(e.type);
                    return (
                      <li key={e.id} className="relative flex items-start gap-3">
                        <span className={cn("relative z-10 grid size-7 shrink-0 place-items-center rounded-full ring-4 ring-card", TONE_SOFT[tone])}>
                          <Icon className="size-3.5" />
                        </span>
                        <div className="min-w-0 flex-1 pt-0.5">
                          <div className="flex items-baseline justify-between gap-2">
                            <span className={cn("min-w-0 truncate text-sm", tone === "danger" && TONE_TEXT.danger)}>{eventSentence(e, sceneName)}</span>
                            <time className="shrink-0 text-xs text-muted-foreground tabular-nums" title={when(e.ts).absolute}>{formatClock(e.ts)}</time>
                          </div>
                          <MetaLine parts={[placeText(e.country, null, e.city), e.ip, e.deviceId ? look.deviceLabel(e.deviceId) : null]} />
                        </div>
                      </li>
                    );
                  })}
                </ol>
              </section>
            ))}
          </div>
          <MoreButton total={rows.length} shown={more.limit} open={more.open} onToggle={more.toggle} />
        </>
      )}
    </Panel>
  );
}

// ── 告警与复核 ──

function verdictOf(c: RiskCase): { label: string; tone: Tone } {
  if (c.status === "pending") return { label: "待复核", tone: "warning" };
  if (c.verdict === "reject") return { label: "确认有风险", tone: "danger" };
  if (c.verdict === "approve") return { label: "已放行", tone: "success" };
  return { label: "已结案", tone: "muted" };
}

const SEVERITY_TONE: Record<string, Tone> = { low: "info", medium: "warning", high: "danger", critical: "danger" };

function AlertsCasesPanel({ alerts, cases, appId, sceneName, look, loading, errors, onOpenDecision }: {
  alerts: AlertRow[]; cases: RiskCase[]; appId: number; sceneName: (k: string) => string; look: Lookup; loading: boolean;
  errors: Record<string, string>; onOpenDecision: (id: string) => void;
}) {
  const pending = cases.filter((c) => c.status === "pending").length;
  const casesHref = `/risk-control?tab=cases&app=${appId}`;
  const figures: Array<{ label: string; value: number; tone?: Tone }> = [
    { label: "行为告警", value: alerts.length, tone: alerts.length ? "danger" : undefined },
    { label: "待复核", value: pending, tone: pending ? "warning" : undefined },
    { label: "已结案", value: cases.length - pending },
  ];
  const entityText = (a: AlertRow) => {
    if (!a.entity || a.entity === "user") return "";
    if (a.entity === "device") return `识别自设备 ${look.deviceLabel(a.value || a.deviceId)}`;
    return `识别自${ENTITY_LABEL[a.entity as RiskEntity] ?? "对象"} ${a.value}`;
  };

  return (
    <Panel
      title="告警与复核"
      icon={<Siren className="size-4" />}
      action={
        <Button asChild size="sm" variant="ghost" className="h-8 text-xs">
          <Link href={casesHref}>
            复核中心
            <ChevronRight className="size-3.5" />
          </Link>
        </Button>
      }
    >
      {loading ? (
        <div className="space-y-4">
          <div className="grid grid-cols-3 gap-2">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-14 rounded-xl" />)}</div>
          <RowsSkeleton rows={3} />
        </div>
      ) : (
        <div className="space-y-5">
          <div className="grid grid-cols-3 gap-2">
            {figures.map((f) => (
              <div key={f.label} className={cn("rounded-xl px-3 py-2", f.tone ? TONE_SOFT[f.tone] : "bg-muted/50")}>
                <div className="text-xs opacity-80">{f.label}</div>
                <div className="text-lg leading-7 font-semibold tabular-nums"><NumberFlow value={f.value} locales="zh-CN" /></div>
              </div>
            ))}
          </div>

          <section>
            <h4 className="mb-2 text-xs font-medium text-muted-foreground">行为模式告警</h4>
            {errors.alerts ? (
              <p className="text-sm text-muted-foreground">暂时读不到告警</p>
            ) : !alerts.length ? (
              <p className="text-sm text-muted-foreground">没有触发行为模式告警</p>
            ) : (
              <ul className="space-y-2">
                {alerts.map((a) => {
                  const tone = SEVERITY_TONE[a.severity] ?? "warning";
                  return (
                    <li key={a.id} className="flex items-start gap-3">
                      <TonalIcon icon={Siren} tone={KIT_TONE[tone]} size="sm" />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-baseline justify-between gap-2">
                          <span className="min-w-0 text-sm">触发「{a.patternName || "未命名的行为模式"}」</span>
                          <When iso={a.ts} className="shrink-0 text-xs text-muted-foreground" />
                        </div>
                        <MetaLine parts={[`严重程度${SEVERITY_LABEL[a.severity] ?? "未知"}`, `串联 ${a.eventIds?.length ?? 0} 个事件`, entityText(a)]} />
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </section>

          <section>
            <h4 className="mb-2 text-xs font-medium text-muted-foreground">复核单</h4>
            {errors.cases ? (
              <p className="text-sm text-muted-foreground">暂时读不到复核单</p>
            ) : !cases.length ? (
              <p className="text-sm text-muted-foreground">没有复核单</p>
            ) : (
              <ul className="space-y-2">
                {cases.map((c) => {
                  const v = verdictOf(c);
                  return (
                    <li key={c.id} className={cn("rounded-xl border px-3 py-2.5", c.status === "pending" && TONE_BORDER.warning)}>
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0 text-sm">
                          {sceneName(c.scene)}请求{ACTION_RESULT[c.action] ?? ACTION_LABEL[c.action]}，风险分 {c.score}
                        </div>
                        <Pill tone={v.tone}>{v.label}</Pill>
                      </div>
                      <p className="mt-0.5 text-xs leading-5 text-foreground/80">{decisionReason({ ...c, hits: [] })}</p>
                      <MetaLine
                        parts={[
                          <When key="t" iso={c.createdAt} />,
                          c.status !== "pending" && c.reviewer ? `由 ${c.reviewer} 复核` : null,
                          c.status !== "pending" && c.note ? `备注：${c.note}` : null,
                        ]}
                      />
                      <div className="mt-1.5 flex flex-wrap gap-1.5">
                        {c.status === "pending" ? (
                          <Button asChild size="sm" variant="outline" className="h-7 text-xs">
                            <Link href={casesHref}>
                              <ClipboardCheck className="size-3.5" />
                              去复核
                            </Link>
                          </Button>
                        ) : null}
                        {c.decisionId ? (
                          <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => onOpenDecision(c.decisionId)}>
                            查看决策
                          </Button>
                        ) : null}
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
        </div>
      )}
    </Panel>
  );
}

// ── 特征快照 ──

type FeatureGroup = "velocity" | "stream" | "graph" | "vector" | "derived" | "request" | "other";

const GROUP_TITLE: Record<FeatureGroup, string> = {
  velocity: "近期频次", stream: "业务累计", graph: "关联关系", vector: "相似设备", derived: "综合指标", request: "请求信息", other: "其他",
};
const GROUP_ORDER: FeatureGroup[] = ["velocity", "stream", "graph", "vector", "derived", "request", "other"];

const ENTITY_UNIT: Partial<Record<RiskEntity, string>> = { device: "台", user: "位", ip: "个", account: "个", phone: "个", email_domain: "个", ip_subnet: "个", app: "个" };
const GRAPH_UNIT: Record<string, string> = { invite_children: "人", invite_tree: "人" };

function featureUnit(f?: Feature): string {
  if (!f) return "";
  switch (f.kind) {
    case "velocity":
      return f.velocity?.agg === "distinct" ? (ENTITY_UNIT[f.velocity.distinctOf as RiskEntity] ?? "个") : "次";
    case "stream":
      return f.stream?.agg === "count" ? "次" : f.stream?.agg === "distinct" ? "个" : "";
    case "graph":
      return GRAPH_UNIT[f.graph?.template ?? ""] ?? "个";
    case "vector":
      return f.vector?.output === "max_score" ? "" : "台";
    default:
      return "";
  }
}

function featureHint(f?: Feature): string {
  if (!f) return "";
  switch (f.kind) {
    case "velocity":
      return f.velocity ? `近 ${formatWindow(f.velocity.windowSeconds)}${f.velocity.allScenes ? "，全部场景" : "，本场景"}` : "";
    case "stream":
      return f.stream ? `近 ${formatWindow(f.stream.windowSeconds)}，${f.stream.events.map((e) => EVENT_LABEL[e] ?? "业务事件").slice(0, 2).join("、")}` : "";
    case "graph":
      return f.graph ? `${GRAPH_TEMPLATE_LABEL[f.graph.template] ?? "关系查询"}${f.graph.withinDays ? `，近 ${f.graph.withinDays} 天` : ""}` : "";
    case "vector":
      return f.vector ? `相似度不低于 ${Math.round(f.vector.minScore * 100)}%` : "";
    default:
      return f.description ?? "";
  }
}

function featureValue(v: number, f?: Feature): string {
  if (f?.kind === "vector" && f.vector?.output === "max_score") return `${Math.round(v * 100)}%`;
  if (Number.isInteger(v)) return v.toLocaleString("zh-CN");
  return v.toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}

function FeaturesPanel({ snaps, names, sceneName, loading, error }: {
  snaps: NonNullable<UserRiskProfile["features"]>; names?: UserRiskProfile["names"]; sceneName: (k: string) => string; loading: boolean; error?: string;
}) {
  // 名称优先取画像带回的 names；特征定义仍要读，用来给出单位、统计窗口与分组
  const defs = useRiskFeatures();
  const meta = useFallbackMeta(!names?.features && snaps.length > 0);
  const models = useFallbackModels(!names?.models && snaps.some((x) => Object.keys(x.models ?? {}).length > 0));
  const [scene, setScene] = useState<string | null>(null);
  const [mode, setMode] = useState<"set" | "all">("set");
  const cur = snaps.find((s) => s.scene === scene) ?? snaps[0];

  const featureMap = useMemo(() => new Map((defs.data ?? []).map((f) => [f.key, f])), [defs.data]);
  const varMap = useMemo(() => new Map((meta.data?.variables ?? []).map((v) => [v.name, v.label])), [meta.data]);
  const modelMap = useMemo(() => new Map((models.data ?? []).map((m) => [m.key, m.name])), [models.data]);

  const { groups, nonZero, totalCount } = useMemo(() => {
    const entries = Object.entries(cur?.features ?? {});
    const out = new Map<FeatureGroup, Array<{ key: string; name: string; value: string; unit: string; hint: string; zero: boolean }>>();
    let nz = 0;
    for (const [k, v] of entries) {
      const f = featureMap.get(k);
      const group: FeatureGroup = f ? f.kind : varMap.has(k) ? "request" : "other";
      const zero = !v;
      if (!zero) nz++;
      if (mode === "set" && zero) continue;
      const name = names?.features?.[k] || f?.name || varMap.get(k) || (names?.features || defs.data ? "未登记的指标" : k);
      const item = { key: k, name, value: featureValue(v, f), unit: featureUnit(f), hint: featureHint(f), zero };
      out.set(group, [...(out.get(group) ?? []), item]);
    }
    for (const list of out.values()) list.sort((a, b) => Number(a.zero) - Number(b.zero) || a.name.localeCompare(b.name, "zh-CN"));
    return { groups: GROUP_ORDER.filter((g) => out.get(g)?.length).map((g) => ({ g, items: out.get(g)! })), nonZero: nz, totalCount: entries.length };
  }, [cur, featureMap, varMap, mode, defs.data, names?.features]);

  const modelRows = Object.entries(cur?.models ?? {});

  return (
    <Panel
      title="特征快照"
      icon={<Sparkles className="size-4" />}
      description={cur ? `${sceneName(cur.scene)}场景，${formatRelative(cur.ts)}评估时的取值` : undefined}
      action={
        cur ? (
          <>
            <Segmented value={mode} onChange={setMode} options={[{ value: "set", label: "有值的" }, { value: "all", label: "全部" }]} ariaLabel="显示范围" />
            {snaps.length > 1 ? (
              <Select value={cur.scene} onValueChange={setScene}>
                <SelectTrigger size="sm" className="h-8 w-36"><SelectValue /></SelectTrigger>
                <SelectContent>{snaps.map((s) => <SelectItem key={s.scene} value={s.scene}>{sceneName(s.scene)}</SelectItem>)}</SelectContent>
              </Select>
            ) : null}
          </>
        ) : undefined
      }
    >
      {loading ? (
        <div className="space-y-3">
          <Skeleton className="h-3 w-20 rounded" />
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-[72px] rounded-xl" />)}</div>
        </div>
      ) : error ? (
        <PanelError text="暂时读不到特征快照" />
      ) : !cur ? (
        <CardEmpty icon={Sparkles} title="没有特征快照" description="用户经过风控评估后，会记录当时用于判定的各项指标" />
      ) : (
        <div className="space-y-5">
          <p className="text-sm text-muted-foreground">
            共 {totalCount} 项指标，{nonZero} 项有值{modelRows.length ? `，${modelRows.length} 个模型给出评分` : ""}。
          </p>
          {modelRows.length ? (
            <section>
              <h4 className="mb-2 text-xs font-medium text-muted-foreground">模型评分</h4>
              <ul className="grid gap-2 sm:grid-cols-2">
                {modelRows.map(([k, v]) => {
                  const ratio = v <= 1 ? v : 0;
                  const tone: Tone = ratio >= 0.8 ? "danger" : ratio >= 0.5 ? "warning" : "success";
                  return (
                    <li key={k} className="rounded-xl border px-3 py-2.5">
                      <div className="flex items-baseline justify-between gap-2">
                        <span className="min-w-0 truncate text-sm">{names?.models?.[k] || modelMap.get(k) || (names?.models || models.data ? "未登记的模型" : k)}</span>
                        <span className={cn("shrink-0 text-sm font-semibold tabular-nums", TONE_TEXT[tone])}>
                          {v <= 1 ? `${(v * 100).toFixed(1)}%` : v.toFixed(2)}
                        </span>
                      </div>
                      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
                        <div className={cn("h-full rounded-full", TONE_DOT[tone])} style={{ width: `${Math.max(2, ratio * 100)}%` }} />
                      </div>
                    </li>
                  );
                })}
              </ul>
            </section>
          ) : null}
          {groups.length ? (
            groups.map(({ g, items }) => (
              <section key={g}>
                <h4 className="mb-2 text-xs font-medium text-muted-foreground">{GROUP_TITLE[g]}</h4>
                <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                  {items.map((it) => (
                    <li key={it.key} className={cn("min-w-0 rounded-xl border px-3 py-2.5", it.zero && "border-dashed")}>
                      <div className="truncate text-xs text-muted-foreground" title={it.name}>{it.name}</div>
                      <div className={cn("mt-0.5 flex items-baseline gap-1 text-lg leading-7 font-semibold tabular-nums", it.zero && "text-muted-foreground")}>
                        {it.value}
                        {it.unit ? <span className="text-xs font-normal text-muted-foreground">{it.unit}</span> : null}
                      </div>
                      {it.hint ? <div className="truncate text-[11px] text-muted-foreground" title={it.hint}>{it.hint}</div> : null}
                    </li>
                  ))}
                </ul>
              </section>
            ))
          ) : (
            <p className="text-sm text-muted-foreground">这次评估的各项指标都是 0，可以切到「全部」查看。</p>
          )}
        </div>
      )}
    </Panel>
  );
}
