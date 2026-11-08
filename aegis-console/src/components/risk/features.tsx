"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  FileCode2,
  Fingerprint,
  Layers,
  Pencil,
  Plus,
  RefreshCw,
  RotateCw,
  Search,
  Sigma,
  Timer,
  Trash2,
  Waypoints,
  XCircle,
  type LucideIcon,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Field, NumberField, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import type { Agg, CondOp, Condition, Feature, FeatureKind, RiskEntity, RiskMeta, StreamSyncResult } from "@/lib/api/risk";
import { useDeleteFeature, useRiskFeatureSQL, useRiskFeatures, useRiskMeta, useRiskScenes, useSaveFeature, useSyncStreams, useValidateExpr } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { when } from "./kit/humanize";
import { ENTITY_LABEL, EVENT_LABEL, FEATURE_KIND_LABEL, GRAPH_TEMPLATE_LABEL, WINDOW_PRESETS, formatWindow } from "./kit/labels";
import { CodeEditor } from "./studio/code-editor";
import { buildVarOptions, completionsOf } from "./studio/condition-builder";
import { ConditionRows, errorText, formatCondValue, keyError, MultiPick } from "./studio/kit";

/**
 * 特征：规则与模型读到的那些数字。
 *
 * 按种类排成看板，每张卡片用一句话说清它算的是什么（「统计某 IP 10 分钟内的登录失败次数」），
 * 句子由窗口、聚合方式、主体、事件推导，不摆配置原文。卡片底部标出哪些场景在用它。
 */

const KINDS: FeatureKind[] = ["velocity", "stream", "graph", "vector", "derived"];
const AGG_LABEL: Record<Agg, string> = { count: "次数", distinct: "去重个数", sum: "总和", max: "最大值", avg: "平均值" };
const VECTOR_OUTPUT_LABEL = { similar_count: "相似设备数", similar_banned: "相似且已封禁的设备数", max_score: "最高相似度" } as const;
const MAX_VELOCITY_WINDOW = 30 * 86400;

const KIND_META: Record<FeatureKind, { icon: LucideIcon; hint: string; plate: string; accent: string }> = {
  velocity: { icon: Timer, hint: "实时累计，本次请求计入在内", plate: "bg-sky-500/12 text-sky-600 dark:text-sky-400", accent: "bg-sky-500" },
  stream: { icon: Activity, hint: "从业务事件聚合，适合长窗口与业务结果", plate: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400", accent: "bg-emerald-500" },
  graph: { icon: Waypoints, hint: "账号、设备、IP 之间的关联", plate: "bg-violet-500/12 text-violet-600 dark:text-violet-400", accent: "bg-violet-500" },
  vector: { icon: Fingerprint, hint: "找出指纹相近的设备", plate: "bg-amber-500/12 text-amber-600 dark:text-amber-400", accent: "bg-amber-500" },
  derived: { icon: Sigma, hint: "用表达式组合请求字段与其他特征", plate: "bg-rose-500/12 text-rose-600 dark:text-rose-400", accent: "bg-rose-500" },
};

/** 主体在句子里的说法：「某 IP」「某台设备」 */
const ENTITY_SUBJECT: Partial<Record<RiskEntity, string>> = {
  user: "某用户", account: "某账号", device: "某台设备", ip: "某 IP", ip_subnet: "某网段", phone: "某手机号", email_domain: "某邮箱域名", app: "某应用",
};

/** 事件字段的中文名 */
const COLUMN_LABEL: Record<string, string> = {
  id: "事件编号", type: "事件类型", scene: "场景", ts: "时间", app_id: "应用", user_id: "用户", account: "账号", ip: "IP", ip_subnet: "网段",
  device_id: "设备", country: "国家或地区", province: "省份", city: "城市", isp: "运营商", asn: "ASN", phone: "手机号", phone_prefix: "号段",
  email_domain: "邮箱域名", amount: "金额", target_user_id: "对象用户", action: "处置结果", level: "风险等级", score: "风险分", shadow: "影子流量",
};

const OP_WORD: Record<CondOp, string> = { eq: "为", ne: "不为", gt: "大于", gte: "不小于", lt: "小于", lte: "不大于", in: "属于", not_in: "不属于" };

/** 中文句子里夹英文词时补空格（「同一 IP 先」），再去掉标点前多余的空格 */
export const sp = (w: string) => `${/^[A-Za-z0-9#]/.test(w) ? " " : ""}${w}${/[A-Za-z0-9]$/.test(w) ? " " : ""}`;
export const tidy = (s: string) => s.replace(/\s+([，。、；：）」])/g, "$1").replace(/([（「])\s+/g, "$1").replace(/ {2,}/g, " ").trim();

/** 事件字段 → 中文名（行为模式页共用） */
export const column = (c?: string) => (c ? COLUMN_LABEL[c] ?? c : "");
const subjectOf = (e?: string) => (e ? ENTITY_SUBJECT[e as RiskEntity] ?? `某${ENTITY_LABEL[e as RiskEntity] ?? e}` : "每次请求");
const eventsOf = (events: string[]) => (events.length ? events.map((e) => EVENT_LABEL[e] ?? e).join("或") : "全部事件");

function conditionsText(conds?: Condition[]): string {
  if (!conds?.length) return "";
  return `，只算${conds.map((c) => `${sp(column(c.field))}${OP_WORD[c.op] ?? c.op} ${formatCondValue(c.value)} `).join("且")}的事件`;
}

/** 特征的人话描述 */
export function featureSentence(f: Feature, sceneName?: (k: string) => string): string {
  return tidy(rawSentence(f, sceneName));
}

function rawSentence(f: Feature, sceneName?: (k: string) => string): string {
  const subj = subjectOf(f.entity);
  const cross = f.global ? "，跨应用合并计算" : "";
  switch (f.kind) {
    case "velocity": {
      const v = f.velocity;
      if (!v) return "";
      const win = formatWindow(v.windowSeconds);
      const where = v.allScenes ? "在全部场景" : "在本场景";
      if (v.agg === "distinct") return `统计${subj} ${win}内${where}出现过的不同${sp(ENTITY_LABEL[v.distinctOf as RiskEntity] ?? v.distinctOf ?? "")}数${cross}`;
      return `统计${subj} ${win}内${where}的请求次数${cross}`;
    }
    case "stream": {
      const s = f.stream;
      if (!s) return "";
      const win = formatWindow(s.windowSeconds);
      const ev = eventsOf(s.events);
      const scenes = s.scenes?.length ? `（限${s.scenes.map((k) => sceneName?.(k) ?? k).join("、")}）` : "";
      let body: string;
      switch (s.agg) {
        case "count":
          body = `${ev}${scenes}次数`;
          break;
        case "distinct":
          body = `${ev}${scenes}涉及的不同${sp(column(s.field))}数`;
          break;
        default:
          body = `${ev}${scenes}的${sp(column(s.field))}${AGG_LABEL[s.agg]}`;
      }
      return `统计${subj} ${win}内的${body}${conditionsText(s.conditions)}${cross}`;
    }
    case "graph": {
      const g = f.graph;
      if (!g) return "";
      return `查询${subj}${GRAPH_TEMPLATE_LABEL[g.template] ?? g.template}${g.withinDays ? `，只看近 ${g.withinDays} 天` : ""}`;
    }
    case "vector": {
      const v = f.vector;
      if (!v) return "";
      const lib = v.collection === "behavior" ? "行为序列" : "设备指纹";
      return `在${lib}中找与当前设备最相近的 ${v.topK} 个，相似度不低于 ${v.minScore}，取${VECTOR_OUTPUT_LABEL[v.output] ?? v.output}`;
    }
    case "derived":
      return f.expr?.trim() ? "由下面的表达式计算" : "表达式未填写";
  }
}

function blank(kind: FeatureKind): Feature {
  const base: Feature = { key: "", name: "", kind, entity: "", default: 0, global: false };
  switch (kind) {
    case "velocity":
      return { ...base, entity: "ip", velocity: { windowSeconds: 600, agg: "count", allScenes: false } };
    case "stream":
      return { ...base, entity: "account", stream: { events: ["login.fail"], agg: "count", windowSeconds: 3600, conditions: [] } };
    case "graph":
      return { ...base, entity: "user", graph: { template: "device_accounts", withinDays: 30, cacheTtl: 120 } };
    case "vector":
      return { ...base, vector: { collection: "device", topK: 20, minScore: 0.95, output: "similar_count" } };
    case "derived":
      return { ...base, expr: "" };
  }
}

/** 保存时只带当前种类的定义块，切换种类留下的其它块不下发 */
function clean(f: Feature): Feature {
  const out: Feature = { key: f.key, name: f.name.trim(), kind: f.kind, entity: f.entity, global: f.global, default: f.default, description: f.description };
  if (f.kind === "velocity") out.velocity = f.velocity;
  if (f.kind === "stream") out.stream = { ...f.stream!, field: f.stream?.agg === "count" ? "" : f.stream?.field };
  if (f.kind === "graph") out.graph = f.graph;
  if (f.kind === "vector") out.vector = f.vector;
  if (f.kind === "derived") {
    out.expr = f.expr;
    out.entity = "";
  }
  return out;
}

/* ════════════════════════ 页面 ════════════════════════ */

export function RiskFeatures() {
  const meta = useRiskMeta();
  const features = useRiskFeatures();
  const scenes = useRiskScenes();
  const del = useDeleteFeature();
  const sync = useSyncStreams();
  const [editing, setEditing] = useState<{ feature: Feature; isNew: boolean } | null>(null);
  const [deleting, setDeleting] = useState<Feature | null>(null);
  const [sqlOf, setSqlOf] = useState<Feature | null>(null);
  const [syncResult, setSyncResult] = useState<StreamSyncResult | null>(null);
  const [keyword, setKeyword] = useState("");
  const [usage, setUsage] = useState<"all" | "used" | "unused">("all");
  const list = useMemo(() => features.data ?? [], [features.data]);

  const sceneName = useMemo(() => {
    const m = new Map((scenes.data ?? []).map((s) => [s.key, s.name]));
    return (k: string) => m.get(k) ?? k;
  }, [scenes.data]);
  /** 特征键 → 引用它的场景名 */
  const usedBy = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const s of scenes.data ?? []) for (const k of s.features ?? []) m.set(k, [...(m.get(k) ?? []), s.name]);
    return m;
  }, [scenes.data]);

  const shown = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    return list.filter((f) => {
      if (usage === "used" && !usedBy.has(f.key)) return false;
      if (usage === "unused" && usedBy.has(f.key)) return false;
      if (!k) return true;
      return [f.key, f.name, f.description ?? "", featureSentence(f, sceneName)].some((v) => v.toLowerCase().includes(k));
    });
  }, [list, keyword, usage, usedBy, sceneName]);

  const runSync = () =>
    sync.mutate(undefined, {
      onSuccess: (r) => {
        setSyncResult(r);
        const failed = Object.keys(r.failed ?? {}).length;
        if (failed) toast.error(`${failed} 个流式特征同步失败`);
        else toast.success("流式特征已同步");
      },
      onError: (e) => toast.error(errorText(e, "同步失败")),
    });

  const filtered = !!keyword.trim() || usage !== "all";

  return (
    <TabStack>
      <Panel
        icon={Layers}
        tone="sky"
        title="特征看板"
        aside={
          <>
            <Button size="sm" variant="outline" onClick={runSync} disabled={sync.isPending}>
              <RefreshCw className={sync.isPending ? "animate-spin" : undefined} />
              同步流式特征
            </Button>
            <Button size="sm" disabled={!meta.data} onClick={() => setEditing({ feature: blank("velocity"), isNew: true })}>
              <Plus />
              新建特征
            </Button>
          </>
        }
      >
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <div className="relative w-full min-w-0 sm:w-64">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input className="h-8 pl-8" placeholder="名称、键或描述" value={keyword} onChange={(e) => setKeyword(e.target.value)} />
            </div>
            <Segmented
              value={usage}
              options={[
                { value: "all" as const, label: `全部 ${list.length}` },
                { value: "used" as const, label: "场景在用" },
                { value: "unused" as const, label: "未被使用" },
              ]}
              onChange={setUsage}
              ariaLabel="使用情况"
            />
            {features.data ? <KindShare features={list} /> : null}
          </div>

          {features.isError && !features.data ? (
            <div className="flex flex-col items-center gap-3 py-10 text-center">
              <AlertTriangle className="size-5 text-red-500" />
              <p className="text-sm text-muted-foreground">{errorText(features.error, "特征读取失败")}</p>
              <Button size="sm" variant="outline" onClick={() => features.refetch()}>
                <RotateCw />
                重试
              </Button>
            </div>
          ) : features.isLoading ? (
            <BoardSkeleton />
          ) : list.length === 0 ? (
            <div className="flex flex-col items-center gap-1 pb-4">
              <CardEmpty icon={Layers} title="还没有特征" description="特征是规则与模型使用的数值，如某 IP 的近期登录失败次数" />
              <Button size="sm" disabled={!meta.data} onClick={() => setEditing({ feature: blank("velocity"), isNew: true })}>
                <Plus />
                新建特征
              </Button>
            </div>
          ) : (
            <div className="-mx-4 overflow-x-auto px-4 pb-1 sm:-mx-5 sm:px-5">
              <div className="grid min-w-max grid-flow-col auto-cols-[17.5rem] gap-3 @6xl:min-w-0 @6xl:auto-cols-fr">
                {KINDS.map((k) => (
                  <KindColumn
                    key={k}
                    kind={k}
                    items={shown.filter((f) => f.kind === k)}
                    total={list.filter((f) => f.kind === k).length}
                    filtered={filtered}
                    usedBy={usedBy}
                    sceneName={sceneName}
                    canEdit={!!meta.data}
                    onCreate={() => setEditing({ feature: blank(k), isNew: true })}
                    onEdit={(f) => setEditing({ feature: { ...blank(f.kind), ...f }, isNew: false })}
                    onSql={setSqlOf}
                    onDelete={setDeleting}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      </Panel>

      {editing && meta.data ? (
        <FeatureDialog initial={editing.feature} isNew={editing.isNew} meta={meta.data} features={list} sceneName={sceneName} onClose={() => setEditing(null)} />
      ) : null}
      {sqlOf ? <SqlDialog feature={sqlOf} onClose={() => setSqlOf(null)} /> : null}
      {syncResult ? <SyncResultDialog result={syncResult} features={list} onClose={() => setSyncResult(null)} /> : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除特征「{deleting?.name}」</AlertDialogTitle>
            <AlertDialogDescription>
              {deleting && usedBy.get(deleting.key)?.length
                ? `${usedBy.get(deleting.key)!.join("、")}正在使用它，删除后这些场景下次发布会失败。`
                : "引用它的规则与表达式下次发布时会失败。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() =>
                deleting &&
                del.mutate(deleting.key, {
                  onSuccess: () => toast.success("特征已删除"),
                  onError: (e) => toast.error(errorText(e, "删除失败")),
                })
              }
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

/** 五类特征的数量占比（细条） */
function KindShare({ features }: { features: Feature[] }) {
  const total = features.length || 1;
  return (
    <div className="ml-auto hidden min-w-0 items-center gap-3 @3xl:flex">
      <div className="flex h-2 w-40 overflow-hidden rounded-full bg-muted">
        {KINDS.map((k) => {
          const n = features.filter((f) => f.kind === k).length;
          return n ? <div key={k} className={cn("h-full", KIND_META[k].accent)} style={{ width: `${(n / total) * 100}%` }} title={`${FEATURE_KIND_LABEL[k]} ${n}`} /> : null;
        })}
      </div>
    </div>
  );
}

/* ── 看板列 ── */

function KindColumn({
  kind,
  items,
  total,
  filtered,
  usedBy,
  sceneName,
  canEdit,
  onCreate,
  onEdit,
  onSql,
  onDelete,
}: {
  kind: FeatureKind;
  items: Feature[];
  total: number;
  filtered: boolean;
  usedBy: Map<string, string[]>;
  sceneName: (k: string) => string;
  canEdit: boolean;
  onCreate: () => void;
  onEdit: (f: Feature) => void;
  onSql: (f: Feature) => void;
  onDelete: (f: Feature) => void;
}) {
  const m = KIND_META[kind];
  const Icon = m.icon;
  return (
    <section className="flex min-w-0 flex-col gap-2 rounded-xl bg-muted/40 p-2">
      <header className="flex min-w-0 items-start gap-2.5 px-1.5 pt-1.5 pb-1">
        <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg", m.plate)}>
          <Icon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-1.5">
            <h4 className="truncate text-sm font-semibold">{FEATURE_KIND_LABEL[kind]}</h4>
            <span className="text-xs text-muted-foreground tabular-nums">{filtered && items.length !== total ? `${items.length} / ${total}` : total}</span>
          </div>
          <p className="text-[11px] leading-4 text-muted-foreground">{m.hint}</p>
        </div>
        <Button size="icon-xs" variant="ghost" aria-label={`新建${FEATURE_KIND_LABEL[kind]}特征`} disabled={!canEdit} onClick={onCreate}>
          <Plus />
        </Button>
      </header>
      {items.length === 0 ? (
        <button
          type="button"
          disabled={!canEdit || filtered}
          onClick={onCreate}
          className="rounded-lg border border-dashed px-3 py-6 text-center text-xs text-muted-foreground transition-colors enabled:hover:bg-background/60"
        >
          {filtered ? "没有匹配的特征" : "暂无，点此新建"}
        </button>
      ) : (
        items.map((f) => (
          <FeatureCard key={f.key} feature={f} accent={m.accent} usedBy={usedBy.get(f.key)} sceneName={sceneName} canEdit={canEdit} onEdit={() => onEdit(f)} onSql={() => onSql(f)} onDelete={() => onDelete(f)} />
        ))
      )}
    </section>
  );
}

function FeatureCard({
  feature: f,
  accent,
  usedBy,
  sceneName,
  canEdit,
  onEdit,
  onSql,
  onDelete,
}: {
  feature: Feature;
  accent: string;
  usedBy?: string[];
  sceneName: (k: string) => string;
  canEdit: boolean;
  onEdit: () => void;
  onSql: () => void;
  onDelete: () => void;
}) {
  const updated = when(f.updatedAt);
  return (
    <article
      title={updated.relative ? `${updated.relative}更新${f.updatedBy ? `，${f.updatedBy}` : ""}` : undefined}
      className="group relative flex min-w-0 flex-col gap-2 overflow-hidden rounded-lg border bg-card p-3 pl-3.5 shadow-xs">
      <span aria-hidden className={cn("absolute inset-y-3 left-0 w-[3px] rounded-r-full", accent)} />
      <div className="flex min-w-0 items-start gap-2">
        <div className="min-w-0 flex-1">
          <h5 className="text-sm leading-5 font-medium break-words">{f.name}</h5>
          <span className="font-mono text-[11px] text-muted-foreground">F.{f.key}</span>
        </div>
        <div className="flex shrink-0 gap-0.5 opacity-100 transition-opacity @6xl:opacity-0 @6xl:group-focus-within:opacity-100 @6xl:group-hover:opacity-100">
          {f.kind === "stream" ? (
            <Button size="icon-xs" variant="ghost" aria-label="查看 SQL" onClick={onSql}>
              <FileCode2 />
            </Button>
          ) : null}
          <Button size="icon-xs" variant="ghost" aria-label="编辑" disabled={!canEdit} onClick={onEdit}>
            <Pencil />
          </Button>
          <Button size="icon-xs" variant="ghost" className="hover:text-destructive" aria-label="删除" onClick={onDelete}>
            <Trash2 />
          </Button>
        </div>
      </div>
      <p className="text-xs leading-5 text-foreground/85">{featureSentence(f, sceneName)}</p>
      {f.kind === "derived" && f.expr?.trim() ? (
        <code className="block rounded-md bg-muted px-2 py-1.5 font-mono text-[11px] leading-4 break-all text-foreground/80">{f.expr}</code>
      ) : null}
      {f.description ? <p className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{f.description}</p> : null}
      <div className="flex min-w-0 flex-wrap items-center gap-1.5 border-t pt-2 text-[11px] text-muted-foreground">
        {usedBy?.length ? (
          <span className="min-w-0 truncate text-foreground/80" title={usedBy.join("、")}>
            {usedBy.length > 2 ? `${usedBy.slice(0, 2).join("、")}等 ${usedBy.length} 个场景在用` : `${usedBy.join("、")}在用`}
          </span>
        ) : (
          <span>暂无场景使用</span>
        )}
        <span className="flex-1" />
        <span title={`取不到数据时按 ${f.default} 计算`} className="tabular-nums">
          缺省 {f.default}
        </span>
        {f.global ? (
          <Badge variant="info" size="sm">
            跨应用
          </Badge>
        ) : null}
      </div>
    </article>
  );
}

function BoardSkeleton() {
  return (
    <div className="grid grid-flow-col auto-cols-[17.5rem] gap-3 overflow-hidden @6xl:auto-cols-fr">
      {KINDS.map((k, i) => (
        <div key={k} className="flex flex-col gap-2 rounded-xl bg-muted/40 p-2">
          <div className="flex items-center gap-2.5 px-1.5 pt-1.5 pb-1">
            <Skeleton className="size-8 rounded-lg" />
            <div className="flex flex-1 flex-col gap-1.5">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="h-3 w-36" />
            </div>
          </div>
          {Array.from({ length: 4 - (i % 3) }).map((_, j) => (
            <div key={j} className="flex flex-col gap-2 rounded-lg border bg-card p-3">
              <Skeleton className="h-4 w-28" />
              <Skeleton className="h-3 w-full" />
              <Skeleton className="h-3 w-3/4" />
              <Skeleton className="mt-1 h-3 w-24" />
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

/* ════════════════════════ 编辑 ════════════════════════ */

function WindowPicker({ value, onChange, max }: { value: number; onChange: (v: number) => void; max?: number }) {
  const presets = WINDOW_PRESETS.filter((p) => !max || p.seconds <= max);
  const options = presets.some((p) => p.seconds === value) ? presets : [...presets, { label: formatWindow(value), seconds: value }];
  return <Segmented value={String(value)} options={options.map((p) => ({ value: String(p.seconds), label: p.label }))} onChange={(v) => onChange(Number(v))} ariaLabel="时间窗口" />;
}

function EntitySelect({ value, onChange, meta, className }: { value: string; onChange: (v: RiskEntity) => void; meta: RiskMeta; className?: string }) {
  return (
    <Select value={value || undefined} onValueChange={(v) => onChange(v as RiskEntity)}>
      <SelectTrigger className={className ?? "w-full"}>
        <SelectValue placeholder="选择主体" />
      </SelectTrigger>
      <SelectContent>
        {meta.entities.map((e) => (
          <SelectItem key={e} value={e}>
            {ENTITY_LABEL[e] ?? e}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** 派生表达式即时校验。校验接口按布尔表达式编译，数值表达式报类型不符时视为通过（保存时以数值再编一次） */
function useDerivedCheck(expr: string) {
  const { mutate } = useValidateExpr();
  const [result, setResult] = useState<{ expr: string; ok: boolean; error?: string } | null>(null);
  useEffect(() => {
    if (!expr.trim()) return;
    const timer = setTimeout(() => {
      mutate(expr, {
        onSuccess: (r) => setResult({ expr, ok: r.ok || /expected bool/i.test(r.error ?? ""), error: r.error }),
        onError: (e) => setResult({ expr, ok: false, error: errorText(e) }),
      });
    }, 400);
    return () => clearTimeout(timer);
  }, [expr, mutate]);
  return result && result.expr === expr ? result : null;
}

function FeatureDialog({
  initial,
  isNew,
  meta,
  features,
  sceneName,
  onClose,
}: {
  initial: Feature;
  isNew: boolean;
  meta: RiskMeta;
  features: Feature[];
  sceneName: (k: string) => string;
  onClose: () => void;
}) {
  const save = useSaveFeature();
  const scenes = useRiskScenes();
  const [f, setF] = useState<Feature>(initial);
  const [touched, setTouched] = useState(false);
  const check = useDerivedCheck(f.kind === "derived" ? f.expr ?? "" : "");
  const kErr = keyError(f.key) ?? (isNew && features.some((x) => x.key === f.key) ? "特征键已存在" : null);
  const nErr = f.name.trim() ? null : "必填";
  const set = (p: Partial<Feature>) => setF({ ...f, ...p });
  const options = buildVarOptions(meta.variables, features.filter((x) => x.key !== f.key), [], []);
  const KindIcon = KIND_META[f.kind].icon;

  const submit = () => {
    setTouched(true);
    if (kErr || nErr) return;
    save.mutate(clean(f), {
      onSuccess: () => {
        toast.success("特征已保存");
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };

  const form = (
    <div className="flex min-w-0 flex-col gap-5">
      {isNew ? (
        <Field label="种类">
          <div className="grid min-w-0 grid-cols-2 gap-1.5 sm:grid-cols-5">
            {KINDS.map((k) => {
              const Icon = KIND_META[k].icon;
              const on = f.kind === k;
              return (
                <button
                  key={k}
                  type="button"
                  aria-pressed={on}
                  onClick={() => setF({ ...blank(k), key: f.key, name: f.name, default: f.default, description: f.description })}
                  className={cn(
                    "flex min-w-0 items-center gap-2 rounded-lg border px-2.5 py-2 text-xs transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                    on ? "border-primary/50 bg-primary/10 font-medium" : "border-border text-muted-foreground hover:bg-muted/50"
                  )}
                >
                  <span className={cn("grid size-6 shrink-0 place-items-center rounded-md", KIND_META[k].plate)}>
                    <Icon className="size-3.5" />
                  </span>
                  <span className="truncate">{FEATURE_KIND_LABEL[k]}</span>
                </button>
              );
            })}
          </div>
        </Field>
      ) : null}

      <div className="flex min-w-0 items-start gap-3 rounded-xl border bg-muted/30 px-3 py-2.5">
        <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg", KIND_META[f.kind].plate)}>
          <KindIcon className="size-4" />
        </span>
        <div className="min-w-0">
          <p className="text-sm leading-5">{featureSentence(f, sceneName) || "补全下方设置后，这里会说明特征算的是什么"}</p>
          <p className="text-[11px] text-muted-foreground">{KIND_META[f.kind].hint}</p>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="名称" error={touched ? nErr : null}>
          <Input value={f.name} placeholder="同 IP 10 分钟登录失败次数" onChange={(e) => set({ name: e.target.value })} />
        </Field>
        <Field label="特征键（规则里写作 F.键）" error={touched ? kErr : null}>
          <Input className="font-mono" disabled={!isNew} value={f.key} placeholder="ip_login_fail_10m" onChange={(e) => set({ key: e.target.value.trim() })} />
        </Field>
      </div>

      {f.kind === "velocity" && f.velocity ? (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="按谁统计">
              <EntitySelect value={f.entity ?? ""} onChange={(entity) => set({ entity })} meta={meta} />
            </Field>
            <Field label="统计什么">
              <Segmented
                value={f.velocity.agg}
                options={[
                  { value: "count" as const, label: "请求次数" },
                  { value: "distinct" as const, label: "不同个数" },
                ]}
                onChange={(agg) => set({ velocity: { ...f.velocity!, agg, distinctOf: agg === "distinct" ? f.velocity!.distinctOf || "account" : undefined } })}
              />
            </Field>
            {f.velocity.agg === "distinct" ? (
              <Field label="数哪一类">
                <EntitySelect value={f.velocity.distinctOf ?? ""} onChange={(distinctOf) => set({ velocity: { ...f.velocity!, distinctOf } })} meta={meta} />
              </Field>
            ) : null}
          </div>
          <Field label="时间窗口">
            <WindowPicker max={MAX_VELOCITY_WINDOW} value={f.velocity.windowSeconds} onChange={(windowSeconds) => set({ velocity: { ...f.velocity!, windowSeconds } })} />
          </Field>
          <div className="flex flex-wrap gap-6">
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={!!f.velocity.allScenes} onCheckedChange={(allScenes) => set({ velocity: { ...f.velocity!, allScenes } })} />
              全部场景合并统计
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={!!f.global} onCheckedChange={(global) => set({ global })} />
              跨应用合并统计
            </label>
          </div>
        </>
      ) : null}

      {f.kind === "stream" && f.stream ? (
        <>
          <Field label="统计哪些事件">
            <MultiPick
              options={meta.eventTypes.map((e) => ({ value: e.type, label: EVENT_LABEL[e.type] ?? e.label, hint: e.type }))}
              value={f.stream.events}
              onChange={(events) => set({ stream: { ...f.stream!, events } })}
              emptyLabel="选择事件"
            />
          </Field>
          <Field label="只看这些场景">
            <MultiPick
              options={(scenes.data ?? []).map((s) => ({ value: s.key, label: s.name, hint: s.key }))}
              value={f.stream.scenes ?? []}
              onChange={(sc) => set({ stream: { ...f.stream!, scenes: sc } })}
              emptyLabel="全部场景"
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="按谁统计">
              <EntitySelect value={f.entity ?? ""} onChange={(entity) => set({ entity })} meta={meta} />
            </Field>
            <Field label="怎么算">
              <Select value={f.stream.agg} onValueChange={(v) => set({ stream: { ...f.stream!, agg: v as Agg } })}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(AGG_LABEL) as Agg[]).map((a) => (
                    <SelectItem key={a} value={a}>
                      {AGG_LABEL[a]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            {f.stream.agg !== "count" ? (
              <Field label="对哪个字段">
                <Select value={f.stream.field || undefined} onValueChange={(field) => set({ stream: { ...f.stream!, field } })}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择字段" />
                  </SelectTrigger>
                  <SelectContent>
                    {meta.streamColumns.map((c) => (
                      <SelectItem key={c} value={c}>
                        {column(c)}
                        <span className="font-mono text-[10px] text-muted-foreground">{c}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            ) : null}
          </div>
          <Field label="时间窗口">
            <WindowPicker value={f.stream.windowSeconds} onChange={(windowSeconds) => set({ stream: { ...f.stream!, windowSeconds } })} />
          </Field>
          <Field label="只算满足条件的事件">
            <ConditionRows value={f.stream.conditions ?? []} onChange={(conditions) => set({ stream: { ...f.stream!, conditions } })} columns={meta.streamColumns} />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={!!f.global} onCheckedChange={(global) => set({ global })} />
            跨应用合并统计
          </label>
        </>
      ) : null}

      {f.kind === "graph" && f.graph ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="查询什么关联">
            <Select value={f.graph.template} onValueChange={(template) => set({ graph: { ...f.graph!, template } })}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {meta.graphTemplates.map((t) => (
                  <SelectItem key={t} value={t}>
                    {GRAPH_TEMPLATE_LABEL[t] ?? t}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="从谁出发">
            <EntitySelect value={f.entity ?? ""} onChange={(entity) => set({ entity })} meta={meta} />
          </Field>
          <Field label="只看近几天（0 为不限）">
            <NumberField value={f.graph.withinDays ?? 0} min={0} suffix="天" onChange={(withinDays) => set({ graph: { ...f.graph!, withinDays } })} />
          </Field>
          <Field label="结果缓存">
            <NumberField value={f.graph.cacheTtl ?? 60} min={0} suffix="秒" onChange={(cacheTtl) => set({ graph: { ...f.graph!, cacheTtl } })} />
          </Field>
        </div>
      ) : null}

      {f.kind === "vector" && f.vector ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="在哪里找">
            <Select value={f.vector.collection} onValueChange={(collection) => set({ vector: { ...f.vector!, collection } })}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="device">设备指纹</SelectItem>
                <SelectItem value="behavior">行为序列</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="取什么结果">
            <Select value={f.vector.output} onValueChange={(v) => set({ vector: { ...f.vector!, output: v as keyof typeof VECTOR_OUTPUT_LABEL } })}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(VECTOR_OUTPUT_LABEL) as Array<keyof typeof VECTOR_OUTPUT_LABEL>).map((o) => (
                  <SelectItem key={o} value={o}>
                    {VECTOR_OUTPUT_LABEL[o]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="最多比较几台（1 到 100）">
            <NumberField value={f.vector.topK} min={1} max={100} onChange={(topK) => set({ vector: { ...f.vector!, topK } })} />
          </Field>
          <Field label="最低相似度（0 到 1）">
            <NumberField value={f.vector.minScore} min={0} max={1} step={0.01} onChange={(minScore) => set({ vector: { ...f.vector!, minScore } })} />
          </Field>
        </div>
      ) : null}

      {f.kind === "derived" ? (
        <Field label="表达式">
          <CodeEditor
            language="risk-expr"
            value={f.expr ?? ""}
            onChange={(expr) => set({ expr })}
            completions={completionsOf(options)}
            height={110}
            placeholder="net.proxy || net.vpn || net.tor"
          />
          {f.expr?.trim() ? (
            check ? (
              check.ok ? (
                <span className="inline-flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400">
                  <CheckCircle2 className="size-3.5" />
                  表达式可用
                </span>
              ) : (
                <pre className="rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 font-mono text-[11px] break-all whitespace-pre-wrap text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
                  <XCircle className="mr-1 inline size-3.5" />
                  {check.error}
                </pre>
              )
            ) : (
              <span className="text-xs text-muted-foreground">检查中</span>
            )
          ) : null}
        </Field>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="取不到数据时按多少计算">
          <NumberField value={f.default} onChange={(v) => set({ default: v })} />
        </Field>
      </div>
      <Field label="说明">
        <Textarea rows={2} value={f.description ?? ""} placeholder="选填" onChange={(e) => set({ description: e.target.value })} />
      </Field>
    </div>
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{isNew ? "新建特征" : `编辑「${initial.name}」`}</DialogTitle>
          <DialogDescription>保存后，场景需重新发布才会用上新的定义。</DialogDescription>
        </DialogHeader>
        {!isNew && initial.kind === "stream" ? (
          <Tabs defaultValue="form">
            <TabsList>
              <TabsTrigger value="form">定义</TabsTrigger>
              <TabsTrigger value="sql">SQL 预览</TabsTrigger>
            </TabsList>
            <TabsContent value="form" className="mt-3">
              {form}
            </TabsContent>
            <TabsContent value="sql" className="mt-3">
              <SqlView featureKey={initial.key} />
            </TabsContent>
          </Tabs>
        ) : (
          form
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={save.isPending}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── SQL 预览 ── */

function SqlView({ featureKey }: { featureKey: string }) {
  const sql = useRiskFeatureSQL(featureKey);
  if (sql.isLoading) return <Skeleton className="h-[360px] rounded-lg" />;
  if (!sql.data) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed px-4 py-10 text-center">
        <p className="text-sm text-muted-foreground">{errorText(sql.error, "暂无 SQL")}</p>
        <Button size="sm" variant="outline" onClick={() => sql.refetch()}>
          <RotateCw />
          重试
        </Button>
      </div>
    );
  }
  return <CodeEditor language="sql" readOnly lineNumbers value={sql.data.sql} height={360} />;
}

function SqlDialog({ feature, onClose }: { feature: Feature; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>「{feature.name}」的 SQL</DialogTitle>
          <DialogDescription>由特征定义自动生成，只读。</DialogDescription>
        </DialogHeader>
        <SqlView featureKey={feature.key} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── 同步结果 ── */

function SyncResultDialog({ result, features, onClose }: { result: StreamSyncResult; features: Feature[]; onClose: () => void }) {
  const name = (k: string) => features.find((f) => f.key === k)?.name ?? k;
  const failed = Object.entries(result.failed ?? {});
  const created = result.created ?? [];
  const dropped = result.dropped ?? [];
  const groups: Array<{ title: string; items: string[]; tone: "success" | "secondary" }> = [
    { title: `新建 ${created.length} 个`, items: created, tone: "success" },
    { title: `清理 ${dropped.length} 个`, items: dropped, tone: "secondary" },
  ];
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{failed.length ? `同步完成，${failed.length} 个失败` : "同步完成"}</DialogTitle>
          <DialogDescription>{created.length + dropped.length === 0 && !failed.length ? "流式特征与定义一致，无需变更。" : "流式特征已按当前定义更新。"}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4 text-sm">
          {groups.map((g) =>
            g.items.length ? (
              <div key={g.title} className="flex flex-col gap-1.5">
                <span className="text-xs text-muted-foreground">{g.title}</span>
                <div className="flex flex-wrap gap-1">
                  {g.items.map((k) => (
                    <Badge key={k} variant={g.tone} size="sm" title={`F.${k}`}>
                      {name(k)}
                    </Badge>
                  ))}
                </div>
              </div>
            ) : null
          )}
          {failed.length ? (
            <div className="flex flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">失败 {failed.length} 个</span>
              <ul className="flex flex-col gap-1">
                {failed.map(([k, msg]) => (
                  <li key={k} className="rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 text-xs text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
                    <span className="font-medium">{name(k)}</span>
                    <span className="ml-1.5 break-all">{msg}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
