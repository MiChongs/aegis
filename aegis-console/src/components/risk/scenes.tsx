"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  AlertTriangle,
  ChevronRight,
  CircleDashed,
  CirclePause,
  Eye,
  FlaskConical,
  Gauge,
  Plus,
  RotateCw,
  Search,
  ShieldCheck,
  Workflow,
  type LucideIcon,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Field, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import type { RiskStatus, Scene, SceneView } from "@/lib/api/risk";
import { useRiskMeta, useRiskScenes, useRiskStatus, useSaveScene } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { when } from "./kit/humanize";
import { errorText, keyError } from "./studio/kit";

/**
 * 场景列表：每个业务场景现在是否在起作用、以什么方式起作用。
 *
 * 「是否生效」看三层：场景是否启用、是否发布过正式版本、正式版本里的规则是拦截还是只观察。
 * 顶部的分段条把全部场景按这个结论归类；每行的流量条画出正式版本与候选版本各分到多少请求。
 */

type Phase = "off" | "draft" | "error" | "shadow" | "canary" | "live";
type Effect = "enforce" | "observe" | "trial" | "draft" | "off";

interface SceneInfo {
  scene: SceneView;
  phase: Phase;
  effect: Effect;
  enforce: number;
  observe: number;
  version?: string;
  candidate?: string;
  canary: number;
  error?: string;
  publishedAt?: string;
  publishedBy?: string;
}

const EFFECT_META: Record<Effect, { label: string; hint: string; bar: string; plate: string; icon: LucideIcon }> = {
  enforce: { label: "拦截生效", hint: "有规则在实际处置请求", bar: "bg-emerald-500", plate: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400", icon: ShieldCheck },
  observe: { label: "仅观察", hint: "规则只记录，不处置请求", bar: "bg-sky-500", plate: "bg-sky-500/12 text-sky-600 dark:text-sky-400", icon: Eye },
  trial: { label: "新版本试运行", hint: "候选版本在灰度或影子中", bar: "bg-amber-500", plate: "bg-amber-500/12 text-amber-600 dark:text-amber-400", icon: FlaskConical },
  draft: { label: "未发布", hint: "还没有正式版本", bar: "bg-muted-foreground/40", plate: "bg-muted text-muted-foreground", icon: CircleDashed },
  off: { label: "停用或异常", hint: "已停用或版本加载失败", bar: "bg-red-500", plate: "bg-red-500/12 text-red-600 dark:text-red-400", icon: CirclePause },
};
const EFFECT_ORDER: Effect[] = ["enforce", "observe", "trial", "draft", "off"];

type LoadedScene = NonNullable<NonNullable<RiskStatus["decision"]>["scenes"]>[number];

/** 规则的生效与观察条数由接口给出；旧版本接口没有这两个字段时只知道总数 */
const rulesSplit = (s: SceneView) => typeof s.ruleEnforce === "number" && typeof s.ruleObserve === "number";

function describeScene(scene: SceneView, loaded: LoadedScene | undefined): SceneInfo {
  const st = scene.state;
  const split = rulesSplit(scene);
  const enforce = split ? scene.ruleEnforce! : scene.ruleCount ?? 0;
  const observe = split ? scene.ruleObserve! : 0;
  const canary = st?.candidateId ? st.canaryPercent || 0 : 0;
  let phase: Phase = "live";
  if (!scene.enabled) phase = "off";
  else if (!st?.primaryId) phase = "draft";
  else if (loaded?.error) phase = "error";
  else if (st.candidateId && st.shadow) phase = "shadow";
  else if (st.candidateId && canary > 0) phase = "canary";

  let effect: Effect;
  if (phase === "off" || phase === "error") effect = "off";
  else if (phase === "draft") effect = "draft";
  else if (phase === "shadow" || phase === "canary") effect = "trial";
  else effect = enforce > 0 ? "enforce" : "observe";

  return {
    scene,
    phase,
    effect,
    enforce,
    observe,
    version: loaded?.version || undefined,
    candidate: loaded?.candidate || undefined,
    canary,
    error: loaded?.error,
    publishedAt: scene.lastPublishedAt || undefined,
    publishedBy: scene.lastPublishedBy || undefined,
  };
}

/** 场景现在的一句话 */
function sceneSentence(i: SceneInfo, rulesKnown: boolean): string {
  const ruleText = !rulesKnown
    ? `${i.enforce} 条规则`
    : i.enforce + i.observe === 0
      ? "没有启用的规则"
      : i.observe === 0
        ? `${i.enforce} 条规则全部拦截生效`
        : i.enforce === 0
          ? `${i.observe} 条规则全部只观察`
          : `${i.enforce} 条规则拦截生效，${i.observe} 条只观察`;
  const v = i.version ? `正式版本 ${i.version}` : "正式版本";
  switch (i.phase) {
    case "off":
      return "场景已停用，不参与风控判断";
    case "draft":
      return i.enforce + i.observe ? `尚未发布，草稿中有 ${i.enforce + i.observe} 条规则，发布前不影响业务` : "尚未发布，发布前不影响业务";
    case "error":
      return `${v}加载失败，请到工作台重新发布`;
    case "shadow":
      return `${v}处理全部请求，新版本${i.candidate ? ` ${i.candidate}` : ""}在旁观察；${ruleText}`;
    case "canary":
      return `${v}处理 ${100 - i.canary}% 请求，新版本${i.candidate ? ` ${i.candidate}` : ""}灰度 ${i.canary}%；${ruleText}`;
    default:
      return `${v}处理全部请求；${ruleText}`;
  }
}

/* ════════════════════════ 页面 ════════════════════════ */

export function RiskScenes() {
  const router = useRouter();
  const scenes = useRiskScenes();
  const status = useRiskStatus();
  const [creating, setCreating] = useState(false);
  const [filter, setFilter] = useState<Effect | "all">("all");
  const [keyword, setKeyword] = useState("");

  const infos = useMemo(() => {
    const loaded = new Map((status.data?.decision?.scenes ?? []).map((s) => [s.scene, s]));
    return (scenes.data ?? []).map((s) => describeScene(s, loaded.get(s.key)));
  }, [scenes.data, status.data]);
  const rulesKnown = (scenes.data ?? []).every(rulesSplit);
  const counts = useMemo(() => {
    const c = Object.fromEntries(EFFECT_ORDER.map((e) => [e, 0])) as Record<Effect, number>;
    for (const i of infos) c[i.effect] += 1;
    return c;
  }, [infos]);
  const shown = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    return infos
      .filter((i) => filter === "all" || i.effect === filter)
      .filter((i) => !k || i.scene.name.toLowerCase().includes(k) || i.scene.key.includes(k))
      .sort((a, b) => EFFECT_ORDER.indexOf(a.effect) - EFFECT_ORDER.indexOf(b.effect) || a.scene.name.localeCompare(b.scene.name, "zh-CN"));
  }, [infos, filter, keyword]);
  const open = (key: string) => router.push(`/risk-control/scenes/${encodeURIComponent(key)}`);

  return (
    <TabStack>
      {scenes.isError && !scenes.data ? (
        <Panel icon={AlertTriangle} tone="rose" title="场景读取失败">
          <div className="flex flex-wrap items-center gap-3">
            <p className="min-w-0 flex-1 text-sm text-muted-foreground">{errorText(scenes.error, "读取失败")}</p>
            <Button size="sm" variant="outline" onClick={() => scenes.refetch()}>
              <RotateCw />
              重试
            </Button>
          </div>
        </Panel>
      ) : scenes.isLoading ? (
        <ScenesSkeleton />
      ) : (
        <>
          <EffectSummary infos={infos} counts={counts} rulesKnown={rulesKnown} active={filter} onPick={(e) => setFilter((cur) => (cur === e ? "all" : e))} />

          <Panel
            icon={Workflow}
            tone="primary"
            title="全部场景"
            aside={
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus />
                新建场景
              </Button>
            }
            bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
          >
            <div className="flex min-w-0 flex-wrap items-center gap-2 border-t px-4 py-3 sm:px-5">
              <div className="relative w-full min-w-0 sm:w-56">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input className="h-8 pl-8" placeholder="场景名称或键" value={keyword} onChange={(e) => setKeyword(e.target.value)} />
              </div>
              <Segmented
                value={filter}
                options={[
                  { value: "all" as const, label: `全部 ${infos.length}` },
                  ...EFFECT_ORDER.filter((e) => counts[e] > 0).map((e) => ({ value: e, label: `${EFFECT_META[e].label} ${counts[e]}` })),
                ]}
                onChange={setFilter}
                ariaLabel="生效情况"
              />
            </div>
            {infos.length === 0 ? (
              <div className="flex flex-col items-center gap-1 border-t pb-6">
                <CardEmpty icon={Workflow} title="还没有场景" description="场景对应一个需要风控判断的业务动作，如登录、抽奖" />
                <Button size="sm" onClick={() => setCreating(true)}>
                  <Plus />
                  新建场景
                </Button>
              </div>
            ) : shown.length === 0 ? (
              <div className="flex flex-col items-center gap-3 border-t px-5 py-10 text-center">
                <Search className="size-5 text-muted-foreground" />
                <p className="text-sm text-muted-foreground">没有符合条件的场景</p>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    setFilter("all");
                    setKeyword("");
                  }}
                >
                  清除筛选
                </Button>
              </div>
            ) : (
              <ul className="flex flex-col">
                {shown.map((i) => (
                  <SceneRow key={i.scene.key} info={i} rulesKnown={rulesKnown} onOpen={() => open(i.scene.key)} />
                ))}
              </ul>
            )}
          </Panel>
        </>
      )}

      {creating ? <CreateSceneDialog open={creating} onOpenChange={setCreating} existing={new Set((scenes.data ?? []).map((s) => s.key))} /> : null}
    </TabStack>
  );
}

/* ════════════════════════ 生效总览 ════════════════════════ */

function EffectSummary({
  infos,
  counts,
  rulesKnown,
  active,
  onPick,
}: {
  infos: SceneInfo[];
  counts: Record<Effect, number>;
  rulesKnown: boolean;
  active: Effect | "all";
  onPick: (e: Effect) => void;
}) {
  const total = infos.length;
  const published = infos.filter((i) => i.phase !== "draft" && i.phase !== "off").length;
  const enforceRules = infos.reduce((a, i) => a + (i.phase === "draft" || i.phase === "off" ? 0 : i.enforce), 0);
  const observeRules = infos.reduce((a, i) => a + (i.phase === "draft" || i.phase === "off" ? 0 : i.observe), 0);
  const latest = infos
    .filter((i) => i.publishedAt)
    .sort((a, b) => new Date(b.publishedAt!).getTime() - new Date(a.publishedAt!).getTime())[0];
  const latestWhen = when(latest?.publishedAt);

  return (
    <Panel
      icon={Gauge}
      tone="emerald"
      title="生效情况"
      aside={
        <span className="text-xs text-muted-foreground">
          <span className="font-medium text-foreground tabular-nums">{published}</span> / {total} 个场景已发布
        </span>
      }
    >
      <div className="flex min-w-0 flex-col gap-5">
        <div className="flex h-9 w-full min-w-0 gap-1 overflow-hidden rounded-xl">
          {total === 0 ? (
            <div className="h-full w-full rounded-xl border border-dashed" />
          ) : (
            EFFECT_ORDER.filter((e) => counts[e] > 0).map((e) => (
              <button
                key={e}
                type="button"
                title={`${EFFECT_META[e].label} ${counts[e]} 个`}
                onClick={() => onPick(e)}
                className={cn(
                  "flex h-full min-w-8 items-center justify-center rounded-md text-xs font-semibold text-white tabular-nums transition-[opacity,flex-grow] duration-500 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                  EFFECT_META[e].bar,
                  e === "draft" && "text-foreground",
                  active !== "all" && active !== e && "opacity-30"
                )}
                style={{ flexGrow: counts[e] }}
              >
                {counts[e]}
              </button>
            ))
          )}
        </div>

        <div className="grid min-w-0 gap-x-6 gap-y-3 @2xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <ul className="grid min-w-0 grid-cols-1 gap-x-4 gap-y-2 @md:grid-cols-2 @4xl:grid-cols-3">
            {EFFECT_ORDER.map((e) => {
              const Icon = EFFECT_META[e].icon;
              return (
                <li key={e} className="flex min-w-0 items-center gap-2.5">
                  <span className={cn("grid size-7 shrink-0 place-items-center rounded-lg", EFFECT_META[e].plate)}>
                    <Icon className="size-3.5" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-baseline gap-1.5">
                      <span className="truncate text-sm">{EFFECT_META[e].label}</span>
                      <span className="text-sm font-semibold tabular-nums">{counts[e]}</span>
                    </span>
                    <span className="block truncate text-[11px] text-muted-foreground">{EFFECT_META[e].hint}</span>
                  </span>
                </li>
              );
            })}
          </ul>
          <div className="grid min-w-0 grid-cols-2 gap-3 border-t pt-3 @2xl:border-t-0 @2xl:border-l @2xl:pt-0 @2xl:pl-6">
            <div className="flex min-w-0 flex-col gap-1.5">
              <span className="text-[11px] text-muted-foreground">已发布场景的规则</span>
              {rulesKnown ? (
                <>
                  <span className="text-sm">
                    <span className="font-semibold tabular-nums">{enforceRules}</span> 拦截
                    <span className="mx-1.5 text-muted-foreground/50">/</span>
                    <span className="font-semibold tabular-nums">{observeRules}</span> 观察
                  </span>
                  <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-muted">
                    <div className="h-full bg-emerald-500" style={{ width: `${enforceRules + observeRules ? (enforceRules / (enforceRules + observeRules)) * 100 : 0}%` }} />
                    <div className="h-full bg-sky-500" style={{ width: `${enforceRules + observeRules ? (observeRules / (enforceRules + observeRules)) * 100 : 0}%` }} />
                  </div>
                </>
              ) : (
                <span className="text-sm">
                  共 <span className="font-semibold tabular-nums">{enforceRules}</span> 条
                </span>
              )}
            </div>
            <div className="flex min-w-0 flex-col gap-1.5">
              <span className="text-[11px] text-muted-foreground">最近一次发布</span>
              {latest ? (
                <>
                  <span className="truncate text-sm font-medium" title={latestWhen.absolute}>
                    {latestWhen.relative}
                  </span>
                  <span className="truncate text-[11px] text-muted-foreground">
                    {latest.scene.name}
                    {latest.publishedBy ? `，${latest.publishedBy}` : ""}
                  </span>
                </>
              ) : (
                <span className="text-sm text-muted-foreground">暂无</span>
              )}
            </div>
          </div>
        </div>
      </div>
    </Panel>
  );
}

/* ════════════════════════ 场景行 ════════════════════════ */

function SceneRow({ info: i, rulesKnown, onOpen }: { info: SceneInfo; rulesKnown: boolean; onOpen: () => void }) {
  const meta = EFFECT_META[i.effect];
  const Icon = meta.icon;
  const published = when(i.publishedAt);
  const s = i.scene;
  const traits = [
    s.captchaSupported ? "可要求验证码" : null,
    s.enforcement?.autoBanAllowed ? "允许自动封禁" : null,
    s.failMode === "closed" ? "出错时拒绝请求" : "出错时放行请求",
    s.flow?.nodes?.length ? "使用决策流" : null,
    s.policy?.trim() ? "附加场景策略" : null,
    s.appIds?.length ? `限 ${s.appIds.length} 个应用` : null,
  ].filter(Boolean) as string[];

  return (
    <li className="border-t">
      <button
        type="button"
        onClick={onOpen}
        className="group flex w-full min-w-0 flex-col gap-3 px-4 py-4 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none sm:px-5 @3xl:flex-row @3xl:items-center @3xl:gap-5"
      >
        <div className="flex min-w-0 flex-1 items-start gap-3">
          <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl", meta.plate)}>
            <Icon className="size-4.5" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <span className="truncate text-sm font-semibold">{s.name}</span>
              <span className="font-mono text-[11px] text-muted-foreground">{s.key}</span>
              {s.builtin ? (
                <Badge variant="outline" size="sm">
                  内置
                </Badge>
              ) : null}
              <Badge variant={i.effect === "enforce" ? "success" : i.effect === "observe" ? "info" : i.effect === "trial" ? "warning" : i.effect === "off" ? "danger" : "secondary"} size="sm">
                {i.phase === "error" ? "加载失败" : i.phase === "off" ? "已停用" : meta.label}
              </Badge>
            </div>
            <p className={cn("text-xs leading-5", i.phase === "error" ? "text-red-600 dark:text-red-400" : "text-foreground/80")}>{sceneSentence(i, rulesKnown)}</p>
            {s.description ? <p className="line-clamp-1 text-[11px] text-muted-foreground">{s.description}</p> : null}
            <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
              {traits.map((t) => (
                <span key={t}>{t}</span>
              ))}
            </div>
          </div>
        </div>

        <div className="flex min-w-0 items-center gap-4 pl-13 @3xl:w-[22rem] @3xl:shrink-0 @3xl:pl-0">
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <TrafficBar info={i} />
            <span className="truncate text-[11px] text-muted-foreground" title={published.absolute || undefined}>
              {i.publishedAt
                ? `${published.relative}发布${i.publishedBy ? `，${i.publishedBy}` : ""}`
                : i.phase === "draft"
                  ? "尚未发布"
                  : "已发布"}
            </span>
          </div>
          <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
        </div>
      </button>
    </li>
  );
}

/** 流量分配：正式版本与候选版本各分到多少请求 */
function TrafficBar({ info: i }: { info: SceneInfo }) {
  if (i.phase === "draft" || i.phase === "off") {
    return (
      <div className="flex h-6 items-center justify-center rounded-md border border-dashed text-[11px] text-muted-foreground">
        {i.phase === "off" ? "不接收请求" : "没有正式版本"}
      </div>
    );
  }
  const primary = i.phase === "canary" ? 100 - i.canary : 100;
  const primaryTone = i.phase === "error" ? "bg-red-500/15 text-red-700 dark:text-red-300" : i.effect === "observe" ? "bg-sky-500/15 text-sky-700 dark:text-sky-300" : "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300";
  return (
    <div className="relative flex h-6 min-w-0 gap-0.5 overflow-hidden rounded-md">
      <div className={cn("flex min-w-0 items-center px-2 text-[11px] font-medium tabular-nums", primaryTone)} style={{ width: `${primary}%` }}>
        <span className="truncate">
          {i.version ?? "正式"} {primary}%
        </span>
      </div>
      {i.phase === "canary" ? (
        <div className="flex min-w-10 items-center justify-center bg-amber-500 px-1.5 text-[11px] font-medium text-white tabular-nums" style={{ width: `${i.canary}%` }}>
          <span className="truncate">{i.canary}%</span>
        </div>
      ) : null}
      {i.phase === "shadow" ? (
        <span className="absolute inset-y-1 right-1 inline-flex items-center rounded border border-dashed border-amber-500 bg-background/80 px-1.5 text-[10px] font-medium text-amber-700 dark:text-amber-300">
          {i.candidate ?? "新版本"} 观察
        </span>
      ) : null}
    </div>
  );
}

function ScenesSkeleton() {
  return (
    <>
      <div className="flex flex-col gap-5 rounded-2xl border bg-card p-4 sm:p-5">
        <Skeleton className="h-5 w-28" />
        <Skeleton className="h-9 rounded-xl" />
        <div className="grid gap-3 sm:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-9 rounded-lg" />
          ))}
        </div>
      </div>
      <div className="flex flex-col rounded-2xl border bg-card">
        <div className="flex items-center gap-3 p-4 sm:px-5">
          <Skeleton className="size-7 rounded-lg" />
          <Skeleton className="h-4 w-20" />
        </div>
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 border-t px-4 py-4 sm:px-5">
            <Skeleton className="size-10 rounded-xl" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-72 max-w-full" />
            </div>
            <Skeleton className="hidden h-6 w-72 rounded-md sm:block" />
          </div>
        ))}
      </div>
    </>
  );
}

/* ════════════════════════ 新建 ════════════════════════ */

function CreateSceneDialog({ open, onOpenChange, existing }: { open: boolean; onOpenChange: (open: boolean) => void; existing: Set<string> }) {
  const router = useRouter();
  const meta = useRiskMeta();
  const save = useSaveScene();
  const [form, setForm] = useState({ key: "", name: "", description: "" });
  const [touched, setTouched] = useState(false);
  const kErr = keyError(form.key) ?? (existing.has(form.key) ? "场景键已存在" : null);
  const nErr = form.name.trim() ? null : "必填";

  const submit = () => {
    setTouched(true);
    if (kErr || nErr) return;
    const scene: Partial<Scene> = {
      key: form.key,
      name: form.name.trim(),
      description: form.description.trim(),
      appIds: [],
      fields: [],
      timeoutMs: 200,
      failMode: "open",
      captchaSupported: false,
      defaultAction: "",
      enforcement: { targets: ["device"], durationSeconds: 86400, autoBanAllowed: false },
      listTypes: [],
      features: [],
      models: [],
      bands: meta.data?.defaultBands ?? [],
      flow: null,
      policy: "",
      enabled: true,
    };
    save.mutate(
      { key: form.key, scene },
      {
        onSuccess: () => {
          toast.success("场景已创建");
          onOpenChange(false);
          router.push(`/risk-control/scenes/${encodeURIComponent(form.key)}`);
        },
        onError: (e) => toast.error(errorText(e, "创建失败")),
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建场景</DialogTitle>
          <DialogDescription>创建后进入工作台配置规则，发布前不影响业务。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field label="名称" error={touched ? nErr : null}>
            <Input value={form.name} placeholder="提现" onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </Field>
          <Field label="场景键（业务接入时使用）" error={touched ? kErr : null}>
            <Input className="font-mono" value={form.key} placeholder="withdraw" onChange={(e) => setForm({ ...form, key: e.target.value.trim() })} />
          </Field>
          <Field label="说明">
            <Textarea rows={3} value={form.description} placeholder="选填" onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={submit} disabled={save.isPending || !meta.data}>
            创建并配置
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
