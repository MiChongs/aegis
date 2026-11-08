"use client";

import { useMemo, useState, type ReactNode } from "react";
import {
  ArrowRight, CircleCheck, CircleDashed, FileDiff, GitCompare, History, LoaderCircle, Minus, Pencil, Plus, RotateCcw, Rocket, ShieldAlert, ShieldCheck, Split, Undo2, Eye,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/traffic/traffic-kit";
import { when } from "@/components/risk/kit/humanize";
import { ACTION_LABEL, DIM_LABEL } from "@/components/risk/kit/labels";
import type { CompileIssue, Release, ReleaseState, RiskAction, Rule, StreamSyncResult, ValidateResult } from "@/lib/api/risk";
import {
  usePromoteScene, usePublishScene, useRiskDraftBundle, useRiskRelease, useRiskReleases, useRollbackScene, useSceneTraffic, useWithdrawScene,
} from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { CodeDiff } from "./code-editor";
import type { VarOption } from "./condition-builder";
import { Callout, ConfirmDialog, EmptyBlock, ErrorBlock, errorText, IssueList, OptionCard, SplitRing, stableJson, StudioSection } from "./kit";
import { ruleSentence } from "./rules-panel";

type PublishMode = "full" | "canary" | "shadow";

/* ════════════════════════ 草稿与线上的差异 ════════════════════════ */

type BundleRule = Pick<Rule, "key" | "name" | "dimension" | "expr" | "score" | "mode" | "priority"> & { decision?: RiskAction | ""; reason?: string };
type Bundle = Record<string, unknown> & { rules?: BundleRule[]; features?: Array<{ key: string; name?: string }>; models?: unknown[]; scene?: Record<string, unknown> };

export interface BundleDiff {
  added: BundleRule[];
  removed: BundleRule[];
  changed: Array<{ before: BundleRule; after: BundleRule; what: string[] }>;
  other: string[];
  total: number;
}

const VOLATILE = new Set(["version", "checksum", "publishedAt", "publishedBy", "note"]);
const SECTION_LABEL: Record<string, string> = {
  bands: "分数段", flow: "决策流", policy: "场景策略", listTypes: "名单", models: "模型",
};

function stripVolatile(bundle: Record<string, unknown> | undefined) {
  if (!bundle) return {};
  const rest = { ...bundle };
  for (const k of VOLATILE) delete rest[k];
  return rest;
}

const modeWord = (m?: string) => (m === "observe" ? "观察" : "生效");
const decisionWord = (d?: string) => (d ? ACTION_LABEL[d as RiskAction] ?? d : "只加分");
const same = (a: unknown, b: unknown) => stableJson(a ?? null) === stableJson(b ?? null);
const isEmpty = (v: unknown) => v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0);

/** 两份发布包的差异，按规则与其他设置分开说 */
export function diffBundles(base: Bundle | undefined, next: Bundle | undefined): BundleDiff {
  const a = new Map((base?.rules ?? []).map((r) => [r.key, r]));
  const b = new Map((next?.rules ?? []).map((r) => [r.key, r]));
  const added = [...b.values()].filter((r) => !a.has(r.key));
  const removed = [...a.values()].filter((r) => !b.has(r.key));
  const changed: BundleDiff["changed"] = [];
  for (const after of b.values()) {
    const before = a.get(after.key);
    if (!before) continue;
    const what: string[] = [];
    if (before.name !== after.name) what.push("名称");
    if ((before.expr ?? "").trim() !== (after.expr ?? "").trim()) what.push("条件");
    if ((before.score ?? 0) !== (after.score ?? 0)) what.push(`分数 ${before.score ?? 0} 改为 ${after.score ?? 0}`);
    if ((before.decision ?? "") !== (after.decision ?? "")) what.push(`处置 ${decisionWord(before.decision)} 改为 ${decisionWord(after.decision)}`);
    if ((before.mode ?? "enforce") !== (after.mode ?? "enforce")) what.push(`${modeWord(before.mode)} 改为 ${modeWord(after.mode)}`);
    if ((before.reason ?? "") !== (after.reason ?? "")) what.push("命中原因");
    if (before.dimension !== after.dimension) what.push(`归类改为${DIM_LABEL[after.dimension] ?? "其他"}`);
    if ((before.priority ?? 0) !== (after.priority ?? 0)) what.push("评估顺序");
    if (what.length) changed.push({ before, after, what });
  }

  const other: string[] = [];
  const sa = (base?.scene ?? {}) as Record<string, unknown>;
  const sb = (next?.scene ?? {}) as Record<string, unknown>;
  if (!same(sa.enforcement, sb.enforcement)) other.push("处置方式");
  const sceneRest = (s: Record<string, unknown>) => Object.fromEntries(Object.entries(s).filter(([k, v]) => k !== "enforcement" && !isEmpty(v)));
  if (!same(sceneRest(sa), sceneRest(sb))) other.push("基本设置");
  const fa = new Set((base?.features ?? []).map((f) => f.key));
  const fb = new Set((next?.features ?? []).map((f) => f.key));
  const fAdd = [...fb].filter((k) => !fa.has(k)).length;
  const fDel = [...fa].filter((k) => !fb.has(k)).length;
  const fChanged = !same(base?.features, next?.features);
  if (fAdd || fDel) other.push(`特征${fAdd ? `新增 ${fAdd} 个` : ""}${fAdd && fDel ? "、" : ""}${fDel ? `移除 ${fDel} 个` : ""}`);
  else if (fChanged) other.push("特征定义");
  const keys = new Set([...Object.keys(base ?? {}), ...Object.keys(next ?? {})]);
  for (const k of keys) {
    if (VOLATILE.has(k) || k === "rules" || k === "scene" || k === "features") continue;
    const va = base?.[k];
    const vb = next?.[k];
    if (isEmpty(va) && isEmpty(vb)) continue;
    if (!same(va, vb)) other.push(SECTION_LABEL[k] ?? "其他设置");
  }
  const uniq = [...new Set(other)];
  return { added, removed, changed, other: uniq, total: added.length + removed.length + changed.length + uniq.length };
}

function useDraftDiff(sceneKey: string, baseId: number | null | undefined) {
  const draft = useRiskDraftBundle(sceneKey);
  const base = useRiskRelease(baseId ?? null);
  const diff = useMemo(
    () => (draft.data && (!baseId || base.data) ? diffBundles(baseId ? (base.data?.bundle as Bundle | undefined) : undefined, draft.data as Bundle) : null),
    [draft.data, base.data, baseId],
  );
  return { diff, draft, base, loading: draft.isLoading || (!!baseId && base.isLoading), error: draft.error ?? base.error };
}

function DiffRuleItem({ rule, options, tone, children }: { rule: BundleRule; options: VarOption[]; tone: "add" | "del" | "mod"; children?: ReactNode }) {
  const Icon = tone === "add" ? Plus : tone === "del" ? Minus : Pencil;
  return (
    <li className="flex min-w-0 items-start gap-2.5 px-3 py-2.5">
      <span
        className={cn(
          "mt-0.5 grid size-5 shrink-0 place-items-center rounded-md",
          tone === "add" && "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
          tone === "del" && "bg-red-500/15 text-red-700 dark:text-red-300",
          tone === "mod" && "bg-amber-500/15 text-amber-700 dark:text-amber-300",
        )}
      >
        <Icon className="size-3" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className={cn("text-sm font-medium", tone === "del" && "text-muted-foreground line-through")}>{rule.name || "未命名规则"}</span>
        <span className="text-xs leading-5 text-muted-foreground">{ruleSentence(rule, options)}{rule.mode === "observe" ? "（观察）" : ""}</span>
        {children}
      </div>
    </li>
  );
}

function DiffSummary({ diff, options, firstRelease }: { diff: BundleDiff; options: VarOption[]; firstRelease: boolean }) {
  if (diff.total === 0) {
    return <EmptyBlock icon={CircleCheck} title="草稿与线上版本一致" description="没有需要发布的改动。" />;
  }
  const groups = [
    { key: "add", title: firstRelease ? "将上线的规则" : "新增的规则", items: diff.added },
    { key: "mod", title: "修改的规则", items: diff.changed.map((c) => c.after) },
    { key: "del", title: "移除的规则（删除或停用）", items: diff.removed },
  ] as const;
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap gap-2">
        <Badge variant="success" size="default">新增 {diff.added.length}</Badge>
        <Badge variant="warning" size="default">修改 {diff.changed.length}</Badge>
        <Badge variant="danger" size="default">移除 {diff.removed.length}</Badge>
        {diff.other.length ? <Badge variant="outline" size="default">其他设置 {diff.other.length} 项</Badge> : null}
      </div>
      {groups.map((g) =>
        g.items.length ? (
          <div key={g.key} className="flex min-w-0 flex-col gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">{g.title}</span>
            <ul className="flex min-w-0 flex-col divide-y rounded-lg border">
              {g.items.map((r) => {
                const change = g.key === "mod" ? diff.changed.find((c) => c.after.key === r.key) : undefined;
                return (
                  <DiffRuleItem key={r.key} rule={r} options={options} tone={g.key}>
                    {change ? (
                      <span className="mt-1 flex flex-wrap gap-1">{change.what.map((w) => <Badge key={w} variant="outline" size="sm">{w}</Badge>)}</span>
                    ) : null}
                  </DiffRuleItem>
                );
              })}
            </ul>
          </div>
        ) : null,
      )}
      {diff.other.length ? (
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-xs font-medium text-muted-foreground">其他有改动的设置</span>
          <div className="flex flex-wrap gap-1.5">{diff.other.map((o) => <Badge key={o} variant="secondary" size="default">{o}</Badge>)}</div>
        </div>
      ) : null}
    </div>
  );
}

/* ════════════════════════ 发布弹窗 ════════════════════════ */

function streamSummary(s: StreamSyncResult | undefined) {
  if (!s) return "";
  const parts: string[] = [];
  if (s.created?.length) parts.push(`新建 ${s.created.length} 个流式特征`);
  if (s.dropped?.length) parts.push(`清理 ${s.dropped.length} 个`);
  return parts.join("，");
}

const CANARY_PRESETS = [5, 10, 25, 50];

function TrafficRing({ percent, shadow, size = 112 }: { percent: number; shadow?: boolean; size?: number }) {
  return (
    <SplitRing
      size={size}
      thickness={11}
      parts={shadow
        ? [{ value: 100, className: "stroke-emerald-500" }, { value: 100, className: "stroke-sky-500", dashed: true }]
        : [{ value: 100 - percent, className: "stroke-emerald-500" }, { value: percent, className: "stroke-amber-400" }]}
    >
      <span className="text-lg leading-6 font-semibold tabular-nums">{shadow ? "影子" : `${percent}%`}</span>
      <span className="text-[11px] text-muted-foreground">{shadow ? "只记录" : "新版本"}</span>
    </SplitRing>
  );
}

export function PublishDialog({ sceneKey, state, open, onOpenChange, disabledReason, options = [], primaryVersion }: {
  sceneKey: string;
  state?: ReleaseState;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 非空时不能发布（有未保存修改 / 校验不通过） */
  disabledReason?: string | null;
  options?: VarOption[];
  primaryVersion?: string;
}) {
  const publish = usePublishScene();
  const [note, setNote] = useState("");
  const [mode, setMode] = useState<PublishMode>("full");
  const [percent, setPercent] = useState(10);
  const hasPrimary = !!state?.primaryId;
  const hasCandidate = !!state?.candidateId;
  const { diff, loading } = useDraftDiff(sceneKey, state?.primaryId);

  const submit = () => publish.mutate(
    { key: sceneKey, note: note.trim(), mode, canaryPercent: mode === "canary" ? percent : undefined },
    {
      onSuccess: (r) => {
        const extra = streamSummary(r.streams);
        const what = mode === "full" ? "已上线" : mode === "canary" ? `已开始灰度 ${percent}%` : "已开始影子验证";
        toast.success(`${r.release.version} ${what}${extra ? `，${extra}` : ""}`);
        onOpenChange(false);
        setNote("");
      },
      onError: (e) => toast.error(errorText(e, "发布失败")),
    },
  );

  const label = mode === "full" ? "全量发布" : mode === "canary" ? `开始灰度 ${percent}%` : "开始影子验证";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>发布草稿</DialogTitle>
          <DialogDescription>
            {hasPrimary ? `当前线上版本 ${primaryVersion ?? ""}。` : "这是首次发布。"}
            {loading ? "" : diff ? (diff.total ? ` 本次包含 ${diff.added.length} 条新增、${diff.changed.length} 条修改、${diff.removed.length} 条移除的规则${diff.other.length ? `，以及${diff.other.join("、")}的改动` : ""}。` : " 草稿与线上版本一致。") : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-5">
          <div role="radiogroup" aria-label="发布方式" className="grid gap-2">
            <OptionCard selected={mode === "full"} onSelect={() => setMode("full")} icon={Rocket} title="全量发布" description="草稿立即接管全部请求" />
            <OptionCard
              selected={mode === "canary"}
              onSelect={() => setMode("canary")}
              icon={Split}
              title="灰度发布"
              description={hasPrimary ? "按比例分出一部分请求给新版本，其余仍走线上版本" : "需要先有一个线上版本"}
              disabled={!hasPrimary}
            />
            <OptionCard
              selected={mode === "shadow"}
              onSelect={() => setMode("shadow")}
              icon={Eye}
              title="影子验证"
              description={hasPrimary ? "新版本旁路评估全部请求，结论只记录、不生效，用于对比" : "需要先有一个线上版本"}
              disabled={!hasPrimary}
            />
          </div>
          {hasCandidate && mode !== "full" ? <Callout tone="warning">已有候选版本在运行，新的候选版本会替换它。</Callout> : null}
          {hasCandidate && mode === "full" ? <Callout tone="warning">全量发布会同时撤下正在运行的候选版本。</Callout> : null}
          {mode === "canary" ? (
            <div className="flex min-w-0 items-center gap-5 rounded-lg border p-4">
              <TrafficRing percent={percent} />
              <div className="flex min-w-0 flex-1 flex-col gap-3">
                <span className="text-sm"><span className="font-semibold tabular-nums">{percent}%</span> 的请求使用新版本</span>
                <Slider min={1} max={99} step={1} value={[percent]} onValueChange={(v) => setPercent(v[0] ?? 10)} aria-label="灰度比例" />
                <div className="flex flex-wrap gap-1">
                  {CANARY_PRESETS.map((p) => <Button key={p} size="xs" variant={percent === p ? "secondary" : "ghost"} onClick={() => setPercent(p)}>{p}%</Button>)}
                </div>
              </div>
            </div>
          ) : null}
          <Field label="发布说明">
            <Textarea rows={3} value={note} placeholder="例如：IP 批量注册改为直接拦截" onChange={(e) => setNote(e.target.value)} />
          </Field>
          {disabledReason ? <Callout tone="danger" title="暂时不能发布">{disabledReason}</Callout> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button onClick={submit} disabled={publish.isPending || !!disabledReason}>
            {publish.isPending ? <LoaderCircle className="animate-spin" /> : <Rocket />}
            {label}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ════════════════════════ 面板 ════════════════════════ */

type Confirm = { kind: "withdraw" } | { kind: "drop" } | { kind: "promote" } | { kind: "rollback"; release: Release };

function LiveCard({ sceneKey, state, primary, candidate, loading, onPublish, onConfirm }: {
  sceneKey: string;
  state?: ReleaseState;
  primary?: Release;
  candidate?: Release;
  loading: boolean;
  onPublish: () => void;
  onConfirm: (c: Confirm) => void;
}) {
  const traffic = useSceneTraffic();
  const [pending, setPending] = useState<number | null>(null);
  const percent = pending ?? state?.canaryPercent ?? 10;
  const done = (msg: string) => ({
    onSuccess: () => {
      toast.success(msg);
      setPending(null);
    },
    onError: (e: unknown) => toast.error(errorText(e)),
  });

  if (loading) return <Skeleton className="h-56 rounded-xl" />;

  const live = !!state?.primaryId;
  const pv = primary?.version ?? (state?.primaryId ? `#${state.primaryId}` : "");
  const cv = candidate?.version ?? (state?.candidateId ? `#${state.candidateId}` : "");
  const shadow = !!state?.candidateId && !!state?.shadow;
  const canary = !!state?.candidateId && !state?.shadow;
  const p = when(primary?.publishedAt);

  return (
    <StudioSection
      icon={Rocket}
      title="线上状态"
      aside={<Button size="sm" onClick={onPublish}><Rocket />发布草稿</Button>}
    >
      <div className="flex min-w-0 flex-col items-center gap-5 sm:flex-row sm:items-start">
        {!live ? (
          <SplitRing size={112} thickness={11} parts={[]}>
            <span className="text-sm font-medium text-muted-foreground">未发布</span>
          </SplitRing>
        ) : canary ? (
          <TrafficRing percent={percent} />
        ) : shadow ? (
          <TrafficRing percent={0} shadow />
        ) : (
          <SplitRing size={112} thickness={11} parts={[{ value: 100, className: "stroke-emerald-500" }]}>
            <span className="text-lg leading-6 font-semibold tabular-nums">100%</span>
            <span className="text-[11px] text-muted-foreground">线上版本</span>
          </SplitRing>
        )}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          {!live ? (
            <p className="text-sm leading-6">场景还没有上线，所有请求按放行处理。发布草稿后开始生效。</p>
          ) : (
            <ul className="flex flex-col gap-2 text-sm">
              <li className="flex min-w-0 flex-wrap items-center gap-2">
                <span aria-hidden className="size-2.5 rounded-full bg-emerald-500" />
                <span>线上版本 <span className="font-semibold">{pv}</span></span>
                {canary ? <span className="text-muted-foreground tabular-nums">承接 {100 - percent}%</span> : null}
                {primary ? <span className="text-xs text-muted-foreground" title={p.absolute}>{p.relative}由 {primary.publishedBy || "未知"} 发布</span> : null}
              </li>
              {state?.candidateId ? (
                <li className="flex min-w-0 flex-wrap items-center gap-2">
                  <span aria-hidden className={cn("size-2.5 rounded-full", shadow ? "border-2 border-dashed border-sky-500" : "bg-amber-400")} />
                  <span>候选版本 <span className="font-semibold">{cv}</span></span>
                  <span className="text-muted-foreground">{shadow ? "影子验证中，旁路评估全部请求，结论只记录" : `灰度中，承接 ${percent}%`}</span>
                </li>
              ) : null}
            </ul>
          )}
          {canary ? (
            <div className="flex min-w-0 flex-col gap-2 rounded-lg border p-3">
              <span className="text-xs text-muted-foreground">调整灰度比例</span>
              <Slider min={1} max={99} step={1} value={[percent]} onValueChange={(v) => setPending(v[0] ?? percent)} aria-label="灰度比例" />
              <div className="flex flex-wrap items-center gap-1">
                {CANARY_PRESETS.map((x) => <Button key={x} size="xs" variant={percent === x ? "secondary" : "ghost"} onClick={() => setPending(x)}>{x}%</Button>)}
                <span className="flex-1" />
                {pending !== null && pending !== state?.canaryPercent ? (
                  <>
                    <Button size="xs" variant="ghost" onClick={() => setPending(null)}>还原</Button>
                    <Button size="xs" disabled={traffic.isPending} onClick={() => traffic.mutate({ key: sceneKey, canaryPercent: percent }, done(`灰度比例已调为 ${percent}%`))}>应用 {percent}%</Button>
                  </>
                ) : null}
              </div>
            </div>
          ) : null}
          <div className="flex flex-wrap gap-2">
            {state?.candidateId ? (
              <>
                <Button size="sm" onClick={() => onConfirm({ kind: "promote" })}><CircleCheck />转正</Button>
                {shadow ? (
                  <Button size="sm" variant="outline" disabled={traffic.isPending} onClick={() => traffic.mutate({ key: sceneKey, canaryPercent: 10 }, done("已改为灰度 10%"))}><Split />改为灰度</Button>
                ) : (
                  <Button size="sm" variant="outline" disabled={traffic.isPending} onClick={() => traffic.mutate({ key: sceneKey, shadow: true }, done("已改为影子验证"))}><Eye />改为影子验证</Button>
                )}
                <Button size="sm" variant="ghost" onClick={() => onConfirm({ kind: "drop" })}>放弃候选版本</Button>
              </>
            ) : null}
            {live ? (
              <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive" onClick={() => onConfirm({ kind: "withdraw" })}>
                <Undo2 />
                撤回线上版本
              </Button>
            ) : null}
          </div>
        </div>
      </div>
    </StudioSection>
  );
}

function ValidationCard({ validation, validating, validateError, onValidate, dirty, nameOf, onJump }: {
  validation?: ValidateResult;
  validating: boolean;
  validateError?: unknown;
  onValidate: () => void;
  dirty: boolean;
  nameOf?: (issue: CompileIssue) => string | undefined;
  onJump?: (issue: CompileIssue) => void;
}) {
  const ok = validation?.ok;
  const n = validation?.issues?.length ?? 0;
  return (
    <StudioSection
      icon={ok ? ShieldCheck : ShieldAlert}
      title="发布前检查"
      aside={
        <Button size="sm" variant="outline" onClick={onValidate} disabled={validating}>
          {validating ? <LoaderCircle className="animate-spin" /> : <ShieldCheck />}
          重新检查
        </Button>
      }
    >
      {validating && !validation ? (
        <Skeleton className="h-24 rounded-lg" />
      ) : validateError && !validation ? (
        <ErrorBlock title="检查失败" error={validateError} onRetry={onValidate} />
      ) : validation ? (
        <>
          <div className={cn("flex items-center gap-3 rounded-lg px-3 py-3", ok ? "bg-emerald-500/10" : "bg-red-500/10")}>
            {ok ? <CircleCheck className="size-5 text-emerald-600 dark:text-emerald-400" /> : <ShieldAlert className="size-5 text-red-600 dark:text-red-400" />}
            <div className="flex flex-col">
              <span className="text-sm font-medium">{ok ? "草稿可以发布" : `有 ${n} 个问题需要处理`}</span>
              <span className="text-xs text-muted-foreground tabular-nums">包含 {validation.rules} 条规则、{validation.features} 个特征、{validation.models} 个模型</span>
            </div>
          </div>
          {!ok ? <IssueList issues={validation.issues ?? []} nameOf={nameOf} onJump={onJump} /> : null}
        </>
      ) : (
        <p className="text-sm text-muted-foreground">尚未检查</p>
      )}
      {dirty ? <Callout tone="warning">有未保存的修改。检查与发布只针对已保存的草稿。</Callout> : null}
    </StudioSection>
  );
}

export function ReleasePanel({ sceneKey, validation, validating, validateError, onValidate, dirty, onPublish, options = [], nameOf, onJump }: {
  sceneKey: string;
  validation?: ValidateResult;
  validating: boolean;
  validateError?: unknown;
  onValidate: () => void;
  dirty: boolean;
  onPublish: () => void;
  options?: VarOption[];
  nameOf?: (issue: CompileIssue) => string | undefined;
  onJump?: (issue: CompileIssue) => void;
}) {
  const releases = useRiskReleases(sceneKey);
  const promote = usePromoteScene();
  const rollback = useRollbackScene();
  const withdraw = useWithdrawScene();
  const traffic = useSceneTraffic();
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [compareId, setCompareId] = useState<number | null>(null);
  const [rawOpen, setRawOpen] = useState(false);
  const list = releases.data?.releases ?? [];
  const state = releases.data?.state;
  const byId = new Map(list.map((r) => [r.id, r]));
  const primary = state?.primaryId ? byId.get(state.primaryId) : undefined;
  const candidate = state?.candidateId ? byId.get(state.candidateId) : undefined;
  const baseId = compareId ?? state?.primaryId ?? null;
  const baseRelease = baseId ? byId.get(baseId) : undefined;
  const { diff, draft, base, loading: diffLoading, error: diffError } = useDraftDiff(sceneKey, baseId);

  const done = (msg: string) => ({
    onSuccess: () => {
      toast.success(msg);
      setConfirm(null);
    },
    onError: (e: unknown) => toast.error(errorText(e)),
  });

  const runConfirm = () => {
    if (!confirm) return;
    if (confirm.kind === "withdraw") withdraw.mutate({ key: sceneKey }, done("已撤回，场景不再评估"));
    if (confirm.kind === "drop") traffic.mutate({ key: sceneKey, drop: true }, done("已放弃候选版本"));
    if (confirm.kind === "promote") promote.mutate({ key: sceneKey }, done(`${candidate?.version ?? "候选版本"} 已转正`));
    if (confirm.kind === "rollback") rollback.mutate({ key: sceneKey, releaseId: confirm.release.id }, done(`已回滚到 ${confirm.release.version}`));
  };

  const pv = primary?.version ?? "线上版本";
  const cv = candidate?.version ?? "候选版本";
  const confirmText: Record<Confirm["kind"], { title: string; body: string; action: string; danger?: boolean }> = {
    withdraw: { title: `撤回线上版本 ${primary?.version ?? ""}`, body: "场景将停止评估，所有请求按未发布处理（放行）。发布记录会保留，可随时重新发布或回滚。", action: "撤回", danger: true },
    drop: { title: `放弃候选版本 ${cv}`, body: `候选版本立即停止接收流量，${pv} 继续承接全部请求。`, action: "放弃", danger: true },
    promote: { title: `${cv} 转正`, body: `${cv} 将接管全部请求，${pv} 保留在发布记录中，可随时回滚。`, action: "转正" },
    rollback: { title: "回滚", body: "", action: "回滚", danger: true },
  };
  const c = confirm ? confirmText[confirm.kind] : null;
  const confirmBody = confirm?.kind === "rollback"
    ? `${confirm.release.version} 立即成为线上版本，承接全部请求${state?.candidateId ? "，正在运行的候选版本一并撤下" : ""}。`
    : c?.body;
  const confirmTitle = confirm?.kind === "rollback" ? `回滚到 ${confirm.release.version}` : c?.title;
  const pendingConfirm = withdraw.isPending || traffic.isPending || promote.isPending || rollback.isPending;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      {releases.isError ? <ErrorBlock title="发布记录读取失败" error={releases.error} onRetry={() => releases.refetch()} /> : null}
      <div className="grid min-w-0 items-start gap-5 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <LiveCard sceneKey={sceneKey} state={state} primary={primary} candidate={candidate} loading={releases.isLoading} onPublish={onPublish} onConfirm={setConfirm} />
        <ValidationCard validation={validation} validating={validating} validateError={validateError} onValidate={onValidate} dirty={dirty} nameOf={nameOf} onJump={onJump} />
      </div>

      <StudioSection
        icon={FileDiff}
        title={baseRelease ? `草稿与 ${baseRelease.version} 的差异` : "草稿将上线的内容"}
        description={compareId && compareId !== state?.primaryId ? "正在与历史版本对比。" : baseRelease ? "与线上版本对比，发布后这些改动生效。" : "首次发布，下面是草稿中的全部规则。"}
        aside={
          list.length ? (
            <Select value={baseId ? String(baseId) : undefined} onValueChange={(v) => setCompareId(Number(v))}>
              <SelectTrigger size="sm" className="h-8 w-48"><SelectValue placeholder="选择对比版本" /></SelectTrigger>
              <SelectContent>
                {list.map((r) => (
                  <SelectItem key={r.id} value={String(r.id)}>
                    {r.version}{r.id === state?.primaryId ? "（线上）" : r.id === state?.candidateId ? "（候选）" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null
        }
      >
        {diffLoading ? (
          <div className="flex flex-col gap-2"><Skeleton className="h-6 w-64 rounded-md" /><Skeleton className="h-32 rounded-lg" /></div>
        ) : diffError ? (
          <ErrorBlock title="差异读取失败" error={diffError} onRetry={() => { draft.refetch(); base.refetch(); }} />
        ) : diff ? (
          <DiffSummary diff={diff} options={options} firstRelease={!baseId} />
        ) : null}
        {dirty ? <Callout tone="warning">有未保存的修改，差异只包含已保存的部分。</Callout> : null}
        {baseId && diff && diff.total > 0 ? (
          <Collapsible open={rawOpen} onOpenChange={setRawOpen}>
            <CollapsibleTrigger asChild>
              <Button size="xs" variant="ghost" className="w-fit text-muted-foreground"><GitCompare />{rawOpen ? "收起逐行对比" : "逐行对比"}</Button>
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-2 flex flex-col gap-2">
              <div className="grid grid-cols-2 text-xs text-muted-foreground">
                <span>{baseRelease?.version ?? ""}</span>
                <span>草稿</span>
              </div>
              <CodeDiff original={stableJson(stripVolatile(base.data?.bundle))} modified={stableJson(stripVolatile(draft.data))} />
            </CollapsibleContent>
          </Collapsible>
        ) : null}
      </StudioSection>

      <StudioSection icon={History} title="发布记录" description={list.length ? `共 ${list.length} 个版本。` : undefined}>
        {releases.isLoading ? (
          <div className="flex flex-col gap-2">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-14 rounded-lg" />)}</div>
        ) : list.length === 0 ? (
          <EmptyBlock icon={CircleDashed} title="还没有发布过" description="第一次发布后，每个版本都会记在这里，可随时回滚。" />
        ) : (
          <ol className="relative flex min-w-0 flex-col">
            {list.map((r, i) => {
              const isPrimary = r.id === state?.primaryId;
              const isCandidate = r.id === state?.candidateId;
              const t = when(r.publishedAt);
              return (
                <li key={r.id} className="relative flex min-w-0 gap-3 pb-4 last:pb-0">
                  {i < list.length - 1 ? <span aria-hidden className="absolute top-5 bottom-0 left-[7px] w-px bg-border" /> : null}
                  <span
                    aria-hidden
                    className={cn(
                      "relative mt-1 size-[15px] shrink-0 rounded-full border-2 bg-card",
                      isPrimary ? "border-emerald-500 bg-emerald-500" : isCandidate ? "border-amber-400 bg-amber-400" : "border-muted-foreground/40",
                    )}
                  />
                  <div className="flex min-w-0 flex-1 flex-col gap-1 sm:flex-row sm:items-start sm:gap-3">
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-sm font-semibold">{r.version}</span>
                        {isPrimary ? <Badge variant="success" size="sm">线上</Badge> : null}
                        {isCandidate ? <Badge variant="warning" size="sm">{state?.shadow ? "影子验证" : `灰度 ${state?.canaryPercent ?? 0}%`}</Badge> : null}
                        <span className="text-xs text-muted-foreground" title={t.absolute}>{t.relative}由 {r.publishedBy || "未知"} 发布</span>
                      </div>
                      <p className={cn("text-sm leading-6 break-words", !r.note && "text-muted-foreground")}>{r.note || "未填写说明"}</p>
                    </div>
                    <div className="flex shrink-0 gap-1">
                      <Button size="xs" variant="ghost" onClick={() => setCompareId(r.id)} disabled={baseId === r.id}>
                        <ArrowRight />
                        与草稿对比
                      </Button>
                      {!isPrimary ? (
                        <Button size="xs" variant="outline" onClick={() => setConfirm({ kind: "rollback", release: r })}>
                          <RotateCcw />
                          回滚到此版本
                        </Button>
                      ) : null}
                    </div>
                  </div>
                </li>
              );
            })}
          </ol>
        )}
      </StudioSection>

      <ConfirmDialog
        open={!!confirm}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={confirmTitle}
        description={confirmBody}
        confirmLabel={c?.action ?? "确定"}
        danger={c?.danger}
        pending={pendingConfirm}
        onConfirm={runConfirm}
      />
    </div>
  );
}
