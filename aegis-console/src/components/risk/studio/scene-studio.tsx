"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft, Ban, Boxes, Brain, FlaskConical, GitBranch, Gavel, Layers, ListChecks, LoaderCircle, Rocket, Save, ScrollText, ShieldAlert, ShieldCheck, Sigma, Undo2, Workflow,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { when } from "@/components/risk/kit/humanize";
import { DURATION_PRESETS, formatWindow } from "@/components/risk/kit/labels";
import type { CompileIssue, Feature, Scene, SceneView } from "@/lib/api/risk";
import { cn } from "@/lib/utils";
import {
  RISK_KEYS, useRiskDraftBundle, useRiskFeatures, useRiskMeta, useRiskModels, useRiskReleases, useRiskRules, useRiskScene, useSaveScene, useValidateScene,
} from "@/lib/risk-hooks";

import { buildVarOptions } from "./condition-builder";
import { BasicPanel, EnforcementPanel, FeaturesPanel, ListsPanel, ModelsPanel, PolicyPanel } from "./config-panels";
import { FlowPanel } from "./flow-panel";
import { tableRowCount } from "./jdm";
import { ConfirmDialog, ErrorBlock, errorText, PANEL_TITLE, stableJson } from "./kit";
import { ScenePipeline, type PipelineStage } from "./pipeline";
import { PublishDialog, ReleasePanel } from "./release-panel";
import { RulesPanel } from "./rules-panel";
import { SimulatePanel } from "./simulate-panel";

/**
 * 场景工作台（/risk-control/scenes/[key]）：评估管线总览 + 逐环节编辑 + 模拟评估 + 发布。
 *
 * 场景本身（基本、名单、特征、模型、决策流、策略、处置）是一份草稿，整份 PUT 保存（后端 saveScene 是覆盖写）；
 * 规则是独立资源，在规则面板里逐条保存。检查、模拟、发布都针对已保存的草稿，
 * 所以有未保存修改时不能发布，免得「发出去的和屏幕上的不是一份」。
 *
 * 草稿按场景键绑定，无草稿时从服务端派生。
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

function StudioSkeleton() {
  return (
    <div className="page-stack">
      <Skeleton className="h-5 w-24 rounded-md" />
      <div className="flex flex-col gap-2"><Skeleton className="h-8 w-72 rounded-lg" /><Skeleton className="h-4 w-96 max-w-full rounded-md" /></div>
      <Skeleton className="h-36 rounded-xl" />
      <Skeleton className="h-96 rounded-xl" />
    </div>
  );
}

function BackLink() {
  return (
    <Link href="/risk-control?tab=scenes" className="inline-flex w-fit items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
      <ArrowLeft className="size-4" />
      全部场景
    </Link>
  );
}

function StudioInner({ sceneKey }: { sceneKey: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const qc = useQueryClient();
  const sceneQ = useRiskScene(sceneKey);
  const meta = useRiskMeta();
  const rules = useRiskRules(sceneKey);
  const features = useRiskFeatures();
  const models = useRiskModels();
  const bundle = useRiskDraftBundle(sceneKey);
  const releases = useRiskReleases(sceneKey);
  const save = useSaveScene();
  const validate = useValidateScene();
  const { mutate: runValidate } = validate;

  const [draft, setDraft] = useState<{ scope: string; value: Scene } | null>(null);
  const [revision, setRevision] = useState(0);
  const [publishing, setPublishing] = useState(false);
  const [discarding, setDiscarding] = useState(false);

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
    if (!form.name.trim()) {
      toast.error("请填写场景名称");
      select("basic");
      return;
    }
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
    setDiscarding(false);
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
  const options = useMemo(
    () => (meta.data ? buildVarOptions(meta.data.variables, features.data ?? [], models.data ?? [], form?.fields ?? []) : []),
    [meta.data, features.data, models.data, form?.fields],
  );

  if (sceneQ.isPending || meta.isPending) return <StudioSkeleton />;
  if (!form || !meta.data) {
    const err = sceneQ.error ?? meta.error;
    const notFound = (err as { status?: number } | null)?.status === 404 || (!err && !sceneQ.data);
    return (
      <div className="page-stack">
        <BackLink />
        <ErrorBlock
          title={notFound ? "找不到这个场景" : "场景读取失败"}
          error={notFound ? new Error("场景可能已被删除，请返回场景列表查看。") : err}
          onRetry={notFound ? undefined : () => { sceneQ.refetch(); meta.refetch(); }}
        />
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
  const durationLabel = enf.durationSeconds ? DURATION_PRESETS.find((p) => p.seconds === enf.durationSeconds)?.label ?? formatWindow(enf.durationSeconds) : "长期";
  const ic = (k: PanelKey) => issuesBy.get(k)?.length ?? 0;
  const apps = form.appIds?.length ?? 0;

  const stages: PipelineStage[] = [
    { key: "basic", phase: "prepare", title: "基本设置", icon: Workflow, issues: ic("basic"), summary: [apps ? `${apps} 个应用` : "全部应用", `最多等待 ${form.timeoutMs} 毫秒`] },
    { key: "lists", phase: "prepare", title: "名单", icon: ListChecks, issues: 0, summary: [form.listTypes.length ? `检查 ${form.listTypes.length} 类名单` : "检查全部名单", "黑名单直接拦截"] },
    { key: "features", phase: "prepare", title: "特征", icon: Layers, issues: ic("features"), idle: sourceFeatures.length === 0, summary: [sourceFeatures.length ? `用到 ${sourceFeatures.length} 个` : "未使用"] },
    { key: "derived", phase: "prepare", title: "派生特征", icon: Sigma, issues: ic("derived"), idle: derivedKeys.size === 0, summary: [derivedKeys.size ? `用到 ${derivedKeys.size} 个` : "未使用"] },
    { key: "models", phase: "prepare", title: "模型", icon: Brain, issues: ic("models"), idle: form.models.length === 0, summary: [form.models.length ? `${form.models.length} 个模型` : "未使用"] },
    {
      key: "rules", phase: "decide", title: "规则", icon: ScrollText, issues: ic("rules"), idle: enabledRules.length === 0,
      summary: rules.isLoading ? ["读取中"] : [enabledRules.length ? `${enabledRules.length} 条启用` : "还没有规则", observeRules ? `${observeRules} 条观察中` : enabledRules.length ? "全部生效" : ""].filter(Boolean),
    },
    {
      key: "flow", phase: "decide", title: "决策流", icon: GitBranch, issues: ic("flow"),
      summary: [form.flow ? (flowRows !== null ? `决策表 ${flowRows} 行` : `决策图 ${form.flow.nodes?.length ?? 0} 个节点`) : `分数段 ${form.bands.length} 档`],
    },
    { key: "policy", phase: "decide", title: "场景策略", icon: Gavel, issues: ic("policy"), idle: !form.policy.trim(), summary: [form.policy.trim() ? "已启用" : "未启用", "内置护栏始终生效"] },
    {
      key: "enforcement", phase: "act", title: "处置方式", icon: Ban, issues: 0,
      summary: enf.autoBanAllowed ? ["自动封禁", `${enf.targets.length} 类对象，${durationLabel}`] : ["只拦截不封禁"],
    },
  ];

  const status = !state?.primaryId
    ? { label: "尚未发布", tone: "secondary" as const }
    : state.candidateId
      ? state.shadow
        ? { label: `影子验证中 ${versionOf(state.candidateId) ?? ""}`.trim(), tone: "info" as const }
        : { label: `灰度中 ${state.canaryPercent}%`, tone: "warning" as const }
      : { label: `线上运行 ${versionOf(state.primaryId) ?? ""}`.trim(), tone: "success" as const };
  const validation = validate.data;
  const publishBlock = dirty
    ? "有未保存的修改，请先保存。"
    : validation && !validation.ok
      ? `草稿有 ${validation.issues.length} 个问题，处理后才能发布。`
      : null;
  const panelProps = { scene: form, patch, meta: m };
  const ruleName = new Map(ruleList.map((r) => [r.key, r.name]));
  const featureName = new Map((features.data ?? []).map((f) => [f.key, f.name]));
  const modelName = new Map((models.data ?? []).map((x) => [x.key, x.name]));
  const nameOf = (i: CompileIssue) => {
    if (!i.key) return undefined;
    if (i.stage === "rules") return ruleName.get(i.key);
    if (i.stage === "features") return featureName.get(i.key);
    if (i.stage === "models") return modelName.get(i.key);
    return undefined;
  };
  const jump = (i: CompileIssue) => {
    const k = stageOf(i, derivedKeys);
    if (k) select(k);
  };
  const updated = when(sceneQ.data?.updatedAt);
  const title = PANEL_TITLE[panel];

  return (
    <div className="page-stack">
      <div className="flex min-w-0 flex-col gap-3">
        <BackLink />
        <div className="flex min-w-0 flex-wrap items-start gap-x-4 gap-y-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <h1 className="min-w-0 text-xl font-semibold tracking-tight">{form.name || "未命名场景"}</h1>
              <Badge variant={status.tone} size="sm">{status.label}</Badge>
              {form.builtin ? <Badge variant="outline" size="sm">内置场景</Badge> : null}
              {!form.enabled ? <Badge variant="secondary" size="sm">已停用</Badge> : null}
              {validation ? (
                validation.ok ? (
                  <Badge variant="success" size="sm"><ShieldCheck />检查通过</Badge>
                ) : (
                  <button type="button" onClick={() => select("release")}>
                    <Badge variant="danger" size="sm"><ShieldAlert />{validation.issues.length} 个问题</Badge>
                  </button>
                )
              ) : validate.isPending ? <Badge variant="outline" size="sm"><LoaderCircle className="animate-spin" />检查中</Badge> : null}
            </div>
            <p className="text-sm leading-6 text-muted-foreground">
              {form.description || "暂无用途说明"}
              {updated.relative ? <span className="ml-2 text-xs" title={updated.absolute}>{updated.relative}由 {sceneQ.data?.updatedBy || "未知"} 修改</span> : null}
            </p>
          </div>
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={revalidate} disabled={validate.isPending}>
              {validate.isPending ? <LoaderCircle className="animate-spin" /> : <ShieldCheck />}
              检查
            </Button>
            <Button size="sm" variant={dirty ? "default" : "outline"} onClick={doSave} disabled={!dirty || save.isPending}>
              {save.isPending ? <LoaderCircle className="animate-spin" /> : <Save />}
              保存
            </Button>
            <Button size="sm" variant={dirty ? "outline" : "default"} onClick={() => setPublishing(true)}>
              <Rocket />
              发布
            </Button>
          </div>
        </div>
      </div>

      <section className="flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="text-sm font-semibold">评估管线</span>
            <span className="text-xs text-muted-foreground">一次请求依次经过下面的环节，点击环节进行编辑。</span>
          </div>
          <div role="tablist" aria-label="工具" className="flex flex-wrap gap-2">
            {([
              { key: "simulate", label: "模拟评估", icon: FlaskConical },
              { key: "release", label: "发布与版本", icon: Boxes },
            ] as const).map((t) => (
              <button
                key={t.key}
                type="button"
                role="tab"
                aria-selected={panel === t.key}
                onClick={() => select(t.key)}
                className={cn(
                  "inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-sm transition-colors",
                  panel === t.key ? "border-foreground bg-foreground text-background" : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                )}
              >
                <t.icon className="size-4" />
                {t.label}
              </button>
            ))}
          </div>
        </div>
        <ScenePipeline stages={stages} selected={panel} onSelect={select} />
      </section>

      <div key={`${panel}-${revision}`} className="flex min-w-0 flex-col gap-4">
        {title && panel !== "rules" ? (
          <header className="flex min-w-0 flex-col gap-0.5">
            <h2 className="text-base font-semibold">{title.title}</h2>
            <p className="text-xs text-muted-foreground">{title.hint}</p>
          </header>
        ) : null}
        {panel === "basic" ? <BasicPanel {...panelProps} issues={issuesBy.get("basic") ?? []} nameOf={nameOf} /> : null}
        {panel === "lists" ? <ListsPanel {...panelProps} issues={[]} /> : null}
        {panel === "features" ? <FeaturesPanel {...panelProps} issues={issuesBy.get("features") ?? []} mode="source" bundleFeatures={bundleFeatures} options={options} nameOf={nameOf} /> : null}
        {panel === "derived" ? <FeaturesPanel {...panelProps} issues={issuesBy.get("derived") ?? []} mode="derived" bundleFeatures={bundleFeatures} options={options} nameOf={nameOf} /> : null}
        {panel === "models" ? <ModelsPanel {...panelProps} issues={issuesBy.get("models") ?? []} nameOf={nameOf} /> : null}
        {panel === "rules" ? (
          <RulesPanel sceneKey={sceneKey} scene={form} meta={m} issues={issuesBy.get("rules") ?? []} options={options} onChanged={revalidate} nameOf={nameOf} />
        ) : null}
        {panel === "flow" ? (
          <FlowPanel
            scene={form}
            patch={patch}
            meta={m}
            options={options}
            featureKeys={bundleFeatures.map((f) => f.key)}
            modelKeys={form.models}
            issues={issuesBy.get("flow") ?? []}
            nameOf={nameOf}
          />
        ) : null}
        {panel === "policy" ? <PolicyPanel {...panelProps} issues={issuesBy.get("policy") ?? []} nameOf={nameOf} /> : null}
        {panel === "enforcement" ? <EnforcementPanel {...panelProps} issues={[]} /> : null}
        {panel === "simulate" ? (
          <SimulatePanel sceneKey={sceneKey} scene={form} meta={m} bundleFeatures={bundleFeatures} dirty={dirty} released={!!state?.primaryId} options={options} />
        ) : null}
        {panel === "release" ? (
          <ReleasePanel
            sceneKey={sceneKey}
            validation={validation}
            validating={validate.isPending}
            validateError={validate.error}
            onValidate={revalidate}
            dirty={dirty}
            onPublish={() => setPublishing(true)}
            options={options}
            nameOf={nameOf}
            onJump={jump}
          />
        ) : null}
      </div>

      {dirty ? (
        <div className="sticky bottom-4 z-20 flex min-w-0 flex-wrap items-center gap-3 rounded-xl border bg-card/95 px-4 py-3 backdrop-blur">
          <span aria-hidden className="size-2 rounded-full bg-amber-500" />
          <span className="min-w-0 flex-1 text-sm">场景设置有未保存的修改</span>
          <Button size="sm" variant="ghost" onClick={() => setDiscarding(true)}>
            <Undo2 />
            放弃修改
          </Button>
          <Button size="sm" onClick={doSave} disabled={save.isPending}>
            {save.isPending ? <LoaderCircle className="animate-spin" /> : <Save />}
            保存
          </Button>
        </div>
      ) : null}

      {publishing ? (
        <PublishDialog
          sceneKey={sceneKey}
          state={state}
          open={publishing}
          onOpenChange={setPublishing}
          disabledReason={publishBlock}
          options={options}
          primaryVersion={versionOf(state?.primaryId)}
        />
      ) : null}
      <ConfirmDialog
        open={discarding}
        onOpenChange={setDiscarding}
        title="放弃未保存的修改"
        description="场景设置会恢复到上次保存的状态。规则的改动已逐条保存，不受影响。"
        confirmLabel="放弃修改"
        danger
        onConfirm={discard}
      />
    </div>
  );
}

export function SceneStudio({ sceneKey }: { sceneKey: string }) {
  return (
    <Suspense fallback={<StudioSkeleton />}>
      <StudioInner sceneKey={sceneKey} />
    </Suspense>
  );
}
