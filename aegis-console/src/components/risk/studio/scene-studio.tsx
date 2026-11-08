"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft, Ban, Boxes, Brain, FlaskConical, GitBranch, Gavel, Layers, ListChecks, Rocket, Save, ScrollText, ShieldCheck, Sigma, Undo2, Workflow,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { ACTION_LABEL, DURATION_PRESETS, formatWindow } from "@/components/risk/kit/labels";
import type { CompileIssue, Feature, Scene, SceneView } from "@/lib/api/risk";
import { formatShortDateTime } from "@/lib/datetime";
import {
  RISK_KEYS, useRiskDraftBundle, useRiskMeta, useRiskReleases, useRiskRules, useRiskScene, useSaveScene, useValidateScene,
} from "@/lib/risk-hooks";

import { BasicPanel, EnforcementPanel, FeaturesPanel, ListsPanel, ModelsPanel, PolicyPanel } from "./config-panels";
import { FlowPanel } from "./flow-panel";
import { tableRowCount } from "./jdm";
import { errorText, stableJson, UnsavedBadge } from "./kit";
import { ScenePipeline, type PipelineStage } from "./pipeline";
import { PublishDialog, ReleasePanel } from "./release-panel";
import { RulesPanel } from "./rules-panel";
import { SimulatePanel } from "./simulate-panel";

/**
 * 场景工作台（/risk-control/scenes/[key]）：管线总览 + 逐步编辑 + 模拟 + 发布。
 *
 * 场景本身（基本、名单、特征、模型、决策流、策略、处置）是一份草稿，整份 PUT 保存（后端 saveScene 是覆盖写）；
 * 规则是独立资源，在规则面板里逐条保存。校验、模拟、发布都针对已保存的草稿，
 * 所以有未保存修改时发布按钮禁用，免得「发出去的和屏幕上的不是一份」。
 *
 * 草稿按场景键绑定（CLAUDE.md「表单状态：草稿按作用域绑定」），无草稿时从服务端派生。
 */

type PanelKey = "basic" | "lists" | "features" | "derived" | "models" | "rules" | "flow" | "policy" | "enforcement" | "simulate" | "release";
const PANELS = new Set<string>(["basic", "lists", "features", "derived", "models", "rules", "flow", "policy", "enforcement", "simulate", "release"]);

function toScene(v: SceneView): Scene {
  const { state: _state, ruleCount: _count, ...rest } = v;
  void _state;
  void _count;
  return {
    ...rest,
    appIds: rest.appIds ?? [],
    fields: rest.fields ?? [],
    listTypes: rest.listTypes ?? [],
    features: rest.features ?? [],
    models: rest.models ?? [],
    bands: rest.bands ?? [],
    flow: rest.flow ?? null,
    policy: rest.policy ?? "",
    defaultAction: rest.defaultAction ?? "",
    enforcement: rest.enforcement ?? { targets: [], durationSeconds: 86400, autoBanAllowed: false },
  };
}

function stageOf(issue: CompileIssue, derivedKeys: Set<string>): PanelKey | null {
  switch (issue.stage) {
    case "scene": return "basic";
    case "features": return issue.key && derivedKeys.has(issue.key) ? "derived" : "features";
    case "models": return "models";
    case "rules": return "rules";
    case "bands":
    case "flow": return "flow";
    case "policy": return "policy";
    default: return null;
  }
}

function StudioInner({ sceneKey }: { sceneKey: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const qc = useQueryClient();
  const sceneQ = useRiskScene(sceneKey);
  const meta = useRiskMeta();
  const rules = useRiskRules(sceneKey);
  const bundle = useRiskDraftBundle(sceneKey);
  const releases = useRiskReleases(sceneKey);
  const save = useSaveScene();
  const validate = useValidateScene();
  const { mutate: runValidate } = validate;

  const [draft, setDraft] = useState<{ scope: string; value: Scene } | null>(null);
  const [revision, setRevision] = useState(0);
  const [publishing, setPublishing] = useState(false);

  const raw = params.get("panel");
  const panel: PanelKey = raw && PANELS.has(raw) ? (raw as PanelKey) : "rules";
  const select = (next: string) => {
    const q = new URLSearchParams(params.toString());
    q.set("panel", next);
    router.replace(`${pathname}?${q.toString()}`, { scroll: false });
  };

  useEffect(() => {
    runValidate(sceneKey);
  }, [sceneKey, runValidate]);
  const revalidate = () => runValidate(sceneKey);

  const server = useMemo(() => (sceneQ.data ? toScene(sceneQ.data) : null), [sceneQ.data]);
  const form = draft && draft.scope === sceneKey ? draft.value : server;
  const dirty = !!(draft && draft.scope === sceneKey && server && stableJson(draft.value) !== stableJson(server));
  const patch = (p: Partial<Scene>) => {
    if (!form) return;
    setDraft({ scope: sceneKey, value: { ...form, ...p } });
  };

  const doSave = () => {
    if (!form) return;
    save.mutate({ key: sceneKey, scene: form }, {
      onSuccess: (saved) => {
        qc.setQueryData<SceneView>(RISK_KEYS.scene(sceneKey), (old) => (old ? { ...old, ...saved } : old));
        setDraft(null);
        setRevision((r) => r + 1);
        toast.success("场景已保存");
        revalidate();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };
  const discard = () => {
    setDraft(null);
    setRevision((r) => r + 1);
  };

  const bundleFeatures = useMemo(() => ((bundle.data?.features as Feature[] | undefined) ?? []), [bundle.data]);
  const derivedKeys = useMemo(() => new Set(bundleFeatures.filter((f) => f.kind === "derived").map((f) => f.key)), [bundleFeatures]);
  const issues = useMemo(() => validate.data?.issues ?? [], [validate.data]);
  const issuesBy = useMemo(() => {
    const m = new Map<PanelKey, CompileIssue[]>();
    for (const i of issues) {
      const k = stageOf(i, derivedKeys);
      if (k) m.set(k, [...(m.get(k) ?? []), i]);
    }
    return m;
  }, [issues, derivedKeys]);

  if (sceneQ.isLoading || meta.isLoading) {
    return (
      <div className="page-stack">
        <Skeleton className="h-12 w-80 rounded-lg" />
        <Skeleton className="h-[150px] rounded-xl" />
        <Skeleton className="h-96 rounded-xl" />
      </div>
    );
  }
  if (!form || !meta.data) {
    return (
      <div className="page-stack">
        <Link href="/risk-control?tab=scenes" className="inline-flex w-fit items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" />
          场景
        </Link>
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">{errorText(sceneQ.error ?? meta.error, "场景不存在")}</div>
      </div>
    );
  }

  const m = meta.data;
  const state = releases.data?.state ?? sceneQ.data?.state;
  const versionOf = (id: number | null | undefined) => (id ? releases.data?.releases.find((r) => r.id === id)?.version : undefined);
  const ruleList = (rules.data ?? []).filter((r) => r.scene === sceneKey || r.scene === "*");
  const enabledRules = ruleList.filter((r) => r.enabled);
  const observeRules = enabledRules.filter((r) => r.mode === "observe").length;
  const sourceFeatures = bundleFeatures.filter((f) => f.kind !== "derived");
  const flowRows = tableRowCount(form.flow);
  const enf = form.enforcement;
  const durationLabel = DURATION_PRESETS.find((p) => p.seconds === enf.durationSeconds)?.label ?? formatWindow(enf.durationSeconds);
  const ic = (k: PanelKey) => issuesBy.get(k)?.length ?? 0;

  const stages: PipelineStage[] = [
    { key: "basic", title: "装配", icon: Workflow, issues: ic("basic"), summary: [`${form.fields?.length ?? 0} 个入参`, `超时 ${form.timeoutMs} ms`, form.failMode === "closed" ? "出错拒绝" : "出错放行"] },
    { key: "lists", title: "名单", icon: ListChecks, issues: 0, summary: [form.listTypes.length ? `${form.listTypes.length} 类名单` : "全部类型", "黑名单直接拦截"] },
    { key: "features", title: "特征", icon: Layers, issues: ic("features"), idle: sourceFeatures.length === 0, summary: [`${sourceFeatures.length} 个特征`, `勾选 ${form.features.length}`] },
    { key: "derived", title: "派生", icon: Sigma, issues: ic("derived"), idle: derivedKeys.size === 0, summary: [derivedKeys.size ? `${derivedKeys.size} 个派生特征` : "未使用"] },
    { key: "models", title: "模型", icon: Brain, issues: ic("models"), idle: form.models.length === 0, summary: [form.models.length ? `${form.models.length} 个模型` : "未配置"] },
    { key: "rules", title: "规则", icon: ScrollText, issues: ic("rules"), idle: enabledRules.length === 0, summary: [`${enabledRules.length} 条规则`, observeRules ? `观察 ${observeRules}` : "全部生效"] },
    {
      key: "flow", title: "决策流", icon: GitBranch, issues: ic("flow"),
      summary: [form.flow ? (flowRows !== null ? `决策表 ${flowRows} 行` : `决策图 ${form.flow.nodes?.length ?? 0} 个节点`) : `分数段 ${form.bands.length} 档`],
    },
    { key: "policy", title: "策略", icon: Gavel, issues: ic("policy"), idle: !form.policy.trim(), summary: [form.policy.trim() ? "已配场景策略" : "仅内置护栏"] },
    {
      key: "enforcement", title: "处置", icon: Ban, issues: 0,
      summary: [enf.autoBanAllowed ? "允许自动封禁" : "只拦不封", `${enf.targets.length} 类对象`, durationLabel],
    },
  ];

  const status = !state?.primaryId
    ? { label: "未发布", tone: "secondary" as const }
    : state.candidateId
      ? state.shadow
        ? { label: `影子 ${versionOf(state.candidateId) ?? ""}`, tone: "info" as const }
        : { label: `灰度 ${state.canaryPercent}%`, tone: "warning" as const }
      : { label: `正式 ${versionOf(state.primaryId) ?? ""}`, tone: "success" as const };
  const validation = validate.data;
  const publishBlock = dirty ? "有未保存的修改" : validation && !validation.ok ? "草稿校验不通过" : null;
  const panelProps = { scene: form, patch, meta: m };

  return (
    <div className="page-stack">
      <div className="flex min-w-0 flex-col gap-3">
        <Link href="/risk-control?tab=scenes" className="inline-flex w-fit items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" />
          场景
        </Link>
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
            <h1 className="truncate text-xl font-semibold tracking-tight">{form.name}</h1>
            <span className="font-mono text-sm text-muted-foreground">{sceneKey}</span>
            {form.builtin ? <Badge variant="outline" size="sm">内置</Badge> : null}
            <Badge variant={status.tone} size="sm">{status.label}</Badge>
            {!form.enabled ? <Badge variant="secondary" size="sm">已停用</Badge> : null}
            {validation ? (
              validation.ok ? <Badge variant="success" size="sm">校验通过</Badge> : <Badge variant="danger" size="sm">{validation.issues.length} 个问题</Badge>
            ) : null}
            <UnsavedBadge dirty={dirty} />
            <span className="text-xs text-muted-foreground">更新于 {formatShortDateTime(sceneQ.data?.updatedAt)}</span>
          </div>
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            {dirty ? (
              <Button size="sm" variant="ghost" onClick={discard}>
                <Undo2 />
                放弃
              </Button>
            ) : null}
            <Button size="sm" variant={dirty ? "default" : "outline"} onClick={doSave} disabled={!dirty || save.isPending}>
              <Save />
              保存
            </Button>
            <Button size="sm" variant="outline" onClick={revalidate} disabled={validate.isPending}>
              <ShieldCheck />
              校验
            </Button>
            <Button size="sm" variant="outline" onClick={() => setPublishing(true)}>
              <Rocket />
              发布
            </Button>
          </div>
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <ScenePipeline stages={stages} selected={panel} onSelect={select} />
        <div className="flex flex-wrap items-center gap-2">
          {([
            { key: "simulate", label: "模拟", icon: FlaskConical },
            { key: "release", label: "发布与版本", icon: Boxes },
          ] as const).map((t) => (
            <button
              key={t.key}
              type="button"
              onClick={() => select(t.key)}
              className={cn(
                "inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-sm transition-colors",
                panel === t.key ? "border-primary bg-primary/5 text-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
              )}
            >
              <t.icon className="size-4" />
              {t.label}
            </button>
          ))}
          {form.defaultAction ? <span className="text-xs text-muted-foreground">无命中时 {ACTION_LABEL[form.defaultAction]}</span> : null}
        </div>
      </div>

      <div key={`${panel}-${revision}`} className="min-w-0">
        {panel === "basic" ? <BasicPanel {...panelProps} issues={issuesBy.get("basic") ?? []} /> : null}
        {panel === "lists" ? <ListsPanel {...panelProps} issues={[]} /> : null}
        {panel === "features" ? <FeaturesPanel {...panelProps} issues={issuesBy.get("features") ?? []} mode="source" bundleFeatures={bundleFeatures} /> : null}
        {panel === "derived" ? <FeaturesPanel {...panelProps} issues={issuesBy.get("derived") ?? []} mode="derived" bundleFeatures={bundleFeatures} /> : null}
        {panel === "models" ? <ModelsPanel {...panelProps} issues={issuesBy.get("models") ?? []} /> : null}
        {panel === "rules" ? <RulesPanel sceneKey={sceneKey} scene={form} meta={m} issues={issuesBy.get("rules") ?? []} onChanged={revalidate} /> : null}
        {panel === "flow" ? (
          <FlowPanel
            scene={form}
            patch={patch}
            meta={m}
            featureKeys={bundleFeatures.map((f) => f.key)}
            modelKeys={form.models}
            issues={issuesBy.get("flow") ?? []}
          />
        ) : null}
        {panel === "policy" ? <PolicyPanel {...panelProps} issues={issuesBy.get("policy") ?? []} /> : null}
        {panel === "enforcement" ? <EnforcementPanel {...panelProps} issues={[]} /> : null}
        {panel === "simulate" ? <SimulatePanel sceneKey={sceneKey} meta={m} bundleFeatures={bundleFeatures} dirty={dirty} /> : null}
        {panel === "release" ? (
          <ReleasePanel
            sceneKey={sceneKey}
            validation={validation}
            validating={validate.isPending}
            onValidate={revalidate}
            dirty={dirty}
            onPublish={() => setPublishing(true)}
          />
        ) : null}
      </div>

      {publishing ? (
        <PublishDialog
          sceneKey={sceneKey}
          state={state}
          open={publishing}
          onOpenChange={setPublishing}
          disabledReason={publishBlock}
        />
      ) : null}
    </div>
  );
}

export function SceneStudio({ sceneKey }: { sceneKey: string }) {
  return (
    <Suspense fallback={<div className="page-stack"><Skeleton className="h-12 w-80 rounded-lg" /><Skeleton className="h-[150px] rounded-xl" /></div>}>
      <StudioInner sceneKey={sceneKey} />
    </Suspense>
  );
}
