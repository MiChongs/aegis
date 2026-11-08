"use client";

import { useState } from "react";
import Link from "next/link";
import {
  AtSign, Ban, Bot, Brain, CircleCheck, Code2, ExternalLink, Eye, Flag, Gavel, Globe, Info, Layers, ListChecks, Mail, Network, Phone, Plus, Search, Server, Settings2,
  ShieldOff, Smartphone, Sigma, Timer, Trash2, User, Workflow, type LucideIcon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Field, NumberField, SettingRow } from "@/components/traffic/traffic-kit";
import { KindBadge } from "@/components/risk/kit/badges";
import { LEVEL_WORD } from "@/components/risk/kit/humanize";
import {
  ACTION_LABEL, DURATION_PRESETS, ENTITY_LABEL, EVENT_LABEL, FEATURE_KIND_LABEL, formatWindow, GRAPH_TEMPLATE_LABEL, LIST_TYPE_LABEL,
} from "@/components/risk/kit/labels";
import type { CompileIssue, EnforceTarget, Feature, FeatureKind, FieldType, ListType, RiskAction, RiskLevel, RiskMeta, Scene, ScoreBand, SceneField } from "@/lib/api/risk";
import { useRiskFeatures, useRiskModels } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { CodeEditor } from "./code-editor";
import { describeExpr, tokensText, type VarOption } from "./condition-builder";
import {
  ACTION_SWATCH, AppMultiSelect, BandLegend, bandProblems, Callout, ConfirmDialog, EmptyBlock, ErrorBlock, IssueList, KEY_PATTERN, ListSkeleton, OptionCard, ScoreBandBar, StudioSection,
} from "./kit";

export interface PanelProps {
  scene: Scene;
  patch: (p: Partial<Scene>) => void;
  meta: RiskMeta;
  issues: CompileIssue[];
  nameOf?: (issue: CompileIssue) => string | undefined;
}

const FIELD_TYPE_LABEL: Record<FieldType, string> = { string: "文本", number: "数字", bool: "是或否", list: "列表", object: "对象" };

/* ════════════════════════ 基本设置 ════════════════════════ */

function FieldsEditor({ fields, onChange }: { fields: SceneField[]; onChange: (f: SceneField[]) => void }) {
  const set = (i: number, p: Partial<SceneField>) => onChange(fields.map((f, j) => (j === i ? { ...f, ...p } : f)));
  const keyCount = new Map<string, number>();
  for (const f of fields) keyCount.set(f.key, (keyCount.get(f.key) ?? 0) + 1);
  const errorOf = (f: SceneField) => {
    if (!f.key) return "请填写字段名";
    if (!KEY_PATTERN.test(f.key)) return "字段名只能用小写字母、数字、下划线，且以字母开头";
    if ((keyCount.get(f.key) ?? 0) > 1) return "字段名重复";
    return null;
  };

  if (!fields.length) {
    return (
      <EmptyBlock
        icon={Settings2}
        title="没有业务入参"
        description="业务方随请求额外传入的信息（如订单金额、活动编号）在这里登记后，规则里就能直接引用。"
        action={<Button size="sm" variant="outline" onClick={() => onChange([{ key: "", label: "", type: "string" }])}><Plus />添加字段</Button>}
      />
    );
  }
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="hidden grid-cols-[minmax(0,1fr)_minmax(0,1fr)_8rem_5rem_2rem] gap-2 px-1 text-xs text-muted-foreground sm:grid">
        <span>显示名称</span><span>字段名</span><span>类型</span><span>必填</span><span />
      </div>
      {fields.map((f, i) => {
        const err = errorOf(f);
        return (
          <div key={i} className="flex min-w-0 flex-col gap-1">
            <div className="grid min-w-0 grid-cols-2 items-center gap-2 rounded-lg border p-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_8rem_5rem_2rem] sm:border-0 sm:p-0">
              <Input className="h-9" value={f.label} placeholder="订单金额" onChange={(e) => set(i, { label: e.target.value })} aria-label="显示名称" />
              <Input className="h-9 font-mono text-xs" value={f.key} placeholder="order_amount" aria-invalid={err ? true : undefined} onChange={(e) => set(i, { key: e.target.value.trim() })} aria-label="字段名" />
              <Select value={f.type} onValueChange={(v) => set(i, { type: v as FieldType })}>
                <SelectTrigger className="h-9 w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(Object.keys(FIELD_TYPE_LABEL) as FieldType[]).map((t) => <SelectItem key={t} value={t}>{FIELD_TYPE_LABEL[t]}</SelectItem>)}
                </SelectContent>
              </Select>
              <label className="flex items-center gap-2 text-sm">
                <Switch checked={!!f.required} onCheckedChange={(v) => set(i, { required: v })} aria-label="必填" />
                <span className="sm:hidden">必填</span>
              </label>
              <Button size="icon-sm" variant="ghost" aria-label="删除字段" onClick={() => onChange(fields.filter((_, j) => j !== i))}>
                <Trash2 className="size-3.5" />
              </Button>
            </div>
            {err ? <p className="px-1 text-xs text-red-600 dark:text-red-400">{err}</p> : null}
          </div>
        );
      })}
      <Button size="xs" variant="outline" className="w-fit" onClick={() => onChange([...fields, { key: "", label: "", type: "string" }])}>
        <Plus />
        添加字段
      </Button>
    </div>
  );
}

const TIMEOUT_PRESETS = [100, 200, 500, 1000];

export function BasicPanel({ scene, patch, meta, issues, nameOf }: PanelProps) {
  const apps = scene.appIds?.length ?? 0;
  const summary = [
    apps ? `作用于 ${apps} 个应用` : "作用于全部应用",
    `最多等待 ${scene.timeoutMs} 毫秒`,
    scene.failMode === "closed" ? "超时或出错时拒绝请求" : "超时或出错时放行请求",
  ].join("，");

  return (
    <div className="flex min-w-0 flex-col gap-5">
      {issues.length ? <IssueList issues={issues} nameOf={nameOf} /> : null}
      <Callout tone={scene.enabled ? "info" : "warning"} icon={scene.enabled ? Info : ShieldOff} title={scene.enabled ? "场景运行中" : "场景已停用"}>
        {scene.enabled ? `${summary}。` : "停用后这个场景不再参与评估。"}
      </Callout>

      <StudioSection icon={Info} title="场景信息">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="名称" error={scene.name.trim() ? null : "请填写场景名称"}>
            <Input value={scene.name} onChange={(e) => patch({ name: e.target.value })} />
          </Field>
          <Field label="作用的应用">
            <AppMultiSelect value={scene.appIds ?? []} onChange={(appIds) => patch({ appIds })} />
          </Field>
        </div>
        <Field label="用途说明">
          <Textarea rows={2} value={scene.description ?? ""} placeholder="这个场景在什么业务环节评估" onChange={(e) => patch({ description: e.target.value })} />
        </Field>
      </StudioSection>

      <StudioSection icon={Timer} title="运行方式" description="决定评估慢了或出错时，业务该怎么处理请求。">
        <div className="flex flex-col divide-y">
          <SettingRow title="启用场景">
            <Switch checked={scene.enabled} onCheckedChange={(enabled) => patch({ enabled })} aria-label="启用场景" />
          </SettingRow>
          <SettingRow title="最长等待" value={`${scene.timeoutMs} 毫秒`} stacked>
            <div className="flex flex-wrap items-center gap-2">
              <NumberField className="w-32" value={scene.timeoutMs} min={1} max={3000} step={50} suffix="毫秒" onChange={(timeoutMs) => patch({ timeoutMs })} ariaLabel="最长等待" />
              {TIMEOUT_PRESETS.map((t) => (
                <Button key={t} size="xs" variant={scene.timeoutMs === t ? "secondary" : "ghost"} onClick={() => patch({ timeoutMs: t })}>{t} 毫秒</Button>
              ))}
            </div>
          </SettingRow>
          <SettingRow title="超时或出错时" stacked>
            <div role="radiogroup" aria-label="超时或出错时" className="grid gap-2 sm:grid-cols-2">
              <OptionCard selected={scene.failMode !== "closed"} onSelect={() => patch({ failMode: "open" })} icon={CircleCheck} title="放行请求" description="优先保证业务可用" />
              <OptionCard selected={scene.failMode === "closed"} onSelect={() => patch({ failMode: "closed" })} icon={Ban} title="拒绝请求" description="优先保证安全，适合支付等高危环节" />
            </div>
          </SettingRow>
          <SettingRow title="业务端能弹出图形验证码">
            <Switch checked={scene.captchaSupported} onCheckedChange={(captchaSupported) => patch({ captchaSupported })} aria-label="业务端能弹出图形验证码" />
          </SettingRow>
          {!scene.captchaSupported ? <p className="pb-2 text-xs text-muted-foreground">关闭时，「要求验证码」的结论会改为转人工复核。</p> : null}
          <SettingRow title="未命中任何规则时">
            <Select value={scene.defaultAction || "none"} onValueChange={(v) => patch({ defaultAction: v === "none" ? "" : (v as RiskAction) })}>
              <SelectTrigger size="sm" className="h-8 w-40"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="none">按决策流处理</SelectItem>
                {meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}
              </SelectContent>
            </Select>
          </SettingRow>
        </div>
      </StudioSection>

      <StudioSection icon={Settings2} title="业务入参" description="业务方随请求传入的附加字段，规则与模拟评估里可直接使用。">
        <FieldsEditor fields={scene.fields ?? []} onChange={(fields) => patch({ fields })} />
      </StudioSection>
    </div>
  );
}

/* ════════════════════════ 名单 ════════════════════════ */

const LIST_ICON: Record<ListType, LucideIcon> = {
  ip: Globe, cidr: Network, device: Smartphone, account: AtSign, user: User, email_domain: Mail, phone_prefix: Phone, asn: Server, country: Flag, ua_keyword: Bot,
};

export function ListsPanel({ scene, patch, meta }: PanelProps) {
  const selected = scene.listTypes ?? [];
  const all = selected.length === 0;
  const toggle = (t: ListType) => {
    const base = all ? meta.listTypes : selected;
    const next = base.includes(t) ? base.filter((x) => x !== t) : [...base, t];
    if (next.length === 0) return;
    patch({ listTypes: next.length === meta.listTypes.length ? [] : next });
  };
  const count = all ? meta.listTypes.length : selected.length;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <StudioSection
        icon={ListChecks}
        title="评估前先查这些名单"
        description={all ? "检查全部类型的名单。" : `只检查 ${count} 类名单，其余类型不查。`}
        aside={
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={all} onCheckedChange={(v) => patch({ listTypes: v ? [] : [...meta.listTypes] })} aria-label="检查全部类型" />
            全部类型
          </label>
        }
      >
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
          {meta.listTypes.map((t) => {
            const on = all || selected.includes(t);
            const Icon = LIST_ICON[t] ?? ListChecks;
            return (
              <label
                key={t}
                className={cn(
                  "flex min-w-0 cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5 text-sm transition-colors hover:bg-accent/40",
                  on ? "border-foreground/30 bg-accent/50" : "text-muted-foreground",
                )}
              >
                <Checkbox checked={on} onCheckedChange={() => toggle(t)} />
                <Icon className="size-4 shrink-0" />
                <span className="min-w-0 truncate">{LIST_TYPE_LABEL[t] ?? t}</span>
              </label>
            );
          })}
        </div>
      </StudioSection>

      <StudioSection icon={Workflow} title="命中名单后" description="名单先于规则评估，结论由内置护栏统一执行。">
        <ul className="grid gap-2 md:grid-cols-3">
          {[
            { kind: "block", title: "直接拦截", text: "不再看规则与总分，本次请求被拒绝。" },
            { kind: "allow", title: "直接放行", text: "跳过后续的规则与决策流。" },
            { kind: "watch", title: "照常评估", text: "结论不变，规则里可用「命中观察名单」加分。" },
          ].map((x) => (
            <li key={x.kind} className="flex min-w-0 flex-col gap-2 rounded-lg border p-3">
              <div className="flex items-center gap-2"><KindBadge kind={x.kind} /><span className="text-sm font-medium">{x.title}</span></div>
              <p className="text-xs leading-5 text-muted-foreground">{x.text}</p>
            </li>
          ))}
        </ul>
        <Link href="/risk-control?tab=lists" className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          管理名单
          <ExternalLink className="size-3" />
        </Link>
      </StudioSection>
    </div>
  );
}

/* ════════════════════════ 特征 ════════════════════════ */

const SOURCE_KINDS: FeatureKind[] = ["velocity", "stream", "graph", "vector"];
const KIND_HINT: Record<FeatureKind, string> = {
  velocity: "实时计数，本次请求也计算在内",
  stream: "从历史事件中统计，适合较长的时间范围",
  graph: "账号、设备、IP 之间的关联关系",
  vector: "按设备码找几乎相同的设备",
  derived: "由请求信息组合出的判断",
};

/** 特征的一句话说明：「按设备统计 1 天内出现的不同账号数」 */
export function featureSummary(f: Feature, options: VarOption[] = []): string {
  const who = f.entity ? ENTITY_LABEL[f.entity as keyof typeof ENTITY_LABEL] ?? "" : "";
  switch (f.kind) {
    case "velocity": {
      const v = f.velocity;
      if (!v) return KIND_HINT.velocity;
      const what = v.agg === "distinct" ? `出现的不同${ENTITY_LABEL[v.distinctOf as keyof typeof ENTITY_LABEL] ?? "对象"}数` : "的请求次数";
      return `按${who || "对象"}统计 ${formatWindow(v.windowSeconds)}内${what}${v.allScenes ? "，跨全部场景" : ""}`;
    }
    case "stream": {
      const s = f.stream;
      if (!s) return KIND_HINT.stream;
      const events = (s.events ?? []).map((e) => EVENT_LABEL[e] ?? e).join("、") || "事件";
      const agg = s.agg === "count" ? "次数" : s.agg === "distinct" ? "不同取值数" : s.agg === "sum" ? (s.field === "amount" ? "累计金额" : "累计值") : s.agg === "max" ? "最大值" : "平均值";
      return `按${who || "对象"}统计 ${formatWindow(s.windowSeconds)}内${events}的${agg}${s.conditions?.length ? "（带筛选条件）" : ""}`;
    }
    case "graph": {
      const g = f.graph;
      if (!g) return KIND_HINT.graph;
      return `${GRAPH_TEMPLATE_LABEL[g.template] ?? "关联关系"}${g.withinDays ? `，看近 ${g.withinDays} 天` : ""}`;
    }
    case "vector": {
      const v = f.vector;
      if (!v) return KIND_HINT.vector;
      const out = v.output === "similar_banned" ? "其中已被封禁的数量" : v.output === "similar_count" ? "设备数" : "最高相似度";
      return `在设备码最相近的 ${v.topK} 台设备中，相似度不低于 ${Math.round(v.minScore * 100)}%（按相差的字符数算）的${out}`;
    }
    default: {
      const { tokens, loose } = describeExpr(f.expr ?? "", options);
      const text = tokensText(tokens);
      if (!text) return KIND_HINT.derived;
      return loose ? `按${text}计算` : `${text}时为是`;
    }
  }
}

export function FeaturesPanel({ scene, patch, issues, mode, bundleFeatures, options, nameOf }: PanelProps & { mode: "source" | "derived"; bundleFeatures: Feature[]; options?: VarOption[] }) {
  const features = useRiskFeatures();
  const [q, setQ] = useState("");
  const kinds: FeatureKind[] = mode === "derived" ? ["derived"] : SOURCE_KINDS;
  const chosen = new Set(scene.features ?? []);
  const auto = new Set(bundleFeatures.map((f) => f.key).filter((k) => !chosen.has(k)));
  const issueKeys = new Set(issues.map((i) => i.key));
  const toggle = (key: string) => patch({ features: chosen.has(key) ? [...chosen].filter((k) => k !== key) : [...chosen, key] });
  const needle = q.trim().toLowerCase();
  const list = (features.data ?? []).filter((f) => kinds.includes(f.kind)).filter((f) => !needle || `${f.name} ${f.description ?? ""}`.toLowerCase().includes(needle));
  const used = (features.data ?? []).filter((f) => kinds.includes(f.kind) && (chosen.has(f.key) || auto.has(f.key))).length;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      {issues.length ? <IssueList issues={issues} nameOf={nameOf} /> : null}
      <div className="flex min-w-0 flex-wrap items-center gap-3">
        <Callout className="min-w-64 flex-1" icon={mode === "derived" ? Sigma : Layers}>
          {mode === "derived" ? "派生特征由请求信息组合而成，" : "特征是评估时取用的统计数据，"}
          规则、决策流、模型用到的会自动带上，其余可手动勾选供策略使用。当前用到 {used} 个。
        </Callout>
        <div className="relative w-full sm:w-64">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-9 pl-8" placeholder="搜索特征" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
      </div>
      {features.isLoading ? (
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{Array.from({ length: 6 }, (_, i) => <ListSkeleton key={i} rows={1} />)}</div>
      ) : features.isError ? (
        <ErrorBlock title="特征读取失败" error={features.error} onRetry={() => features.refetch()} />
      ) : list.length === 0 ? (
        <EmptyBlock icon={Layers} title={needle ? "没有匹配的特征" : mode === "derived" ? "还没有派生特征" : "还没有特征"} description={needle ? undefined : "在特征库中创建后，这里即可选用。"} />
      ) : (
        kinds.map((kind) => {
          const items = list.filter((f) => f.kind === kind);
          if (!items.length) return null;
          return (
            <StudioSection
              key={kind}
              title={FEATURE_KIND_LABEL[kind]}
              description={KIND_HINT[kind]}
              aside={<Badge variant="secondary" size="sm">{items.length} 个</Badge>}
            >
              <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                {items.map((f) => {
                  const isAuto = auto.has(f.key);
                  const on = chosen.has(f.key) || isAuto;
                  return (
                    <label
                      key={f.key}
                      className={cn(
                        "flex min-w-0 cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:bg-accent/40",
                        on && "border-foreground/30 bg-accent/50",
                        isAuto && "cursor-default",
                      )}
                    >
                      <Checkbox className="mt-0.5" checked={on} disabled={isAuto} onCheckedChange={() => toggle(f.key)} />
                      <span className="flex min-w-0 flex-1 flex-col gap-1">
                        <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                          <span className="min-w-0 text-sm leading-5">{f.name}</span>
                          {isAuto ? <Badge variant="info" size="sm">已被引用</Badge> : null}
                          {issueKeys.has(f.key) ? <Badge variant="danger" size="sm">有问题</Badge> : null}
                        </span>
                        <span className="text-xs leading-5 text-muted-foreground">{f.description || featureSummary(f, options)}</span>
                      </span>
                    </label>
                  );
                })}
              </div>
            </StudioSection>
          );
        })
      )}
      <Link href="/risk-control?tab=features" className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
        管理特征库
        <ExternalLink className="size-3" />
      </Link>
    </div>
  );
}

/* ════════════════════════ 模型 ════════════════════════ */

export function ModelsPanel({ scene, patch, issues, nameOf }: PanelProps) {
  const models = useRiskModels();
  const features = useRiskFeatures();
  const chosen = new Set(scene.models ?? []);
  const issueKeys = new Set(issues.map((i) => i.key));
  const toggle = (key: string) => patch({ models: chosen.has(key) ? [...chosen].filter((k) => k !== key) : [...chosen, key] });
  const known = new Set((models.data ?? []).map((m) => m.key));
  const missing = [...chosen].filter((k) => models.data && !known.has(k));
  const featureName = new Map((features.data ?? []).map((f) => [f.key, f.name]));
  const inputName = (s: string) => featureName.get(s.replace(/^F\./, "")) ?? null;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      {issues.length ? <IssueList issues={issues} nameOf={nameOf} /> : null}
      <StudioSection
        icon={Brain}
        title="参与评估的模型"
        description="模型给出 0 到 1 之间的风险分，规则与决策流可以引用。"
        aside={<Badge variant="secondary" size="sm">已选 {chosen.size} 个</Badge>}
      >
        {models.isLoading ? (
          <ListSkeleton rows={2} itemClassName="h-20" />
        ) : models.isError ? (
          <ErrorBlock title="模型读取失败" error={models.error} onRetry={() => models.refetch()} />
        ) : !models.data?.length ? (
          <EmptyBlock icon={Brain} title="还没有模型" description="在模型库上传训练好的模型后，这里即可选用。" />
        ) : (
          <div className="grid gap-2 lg:grid-cols-2">
            {models.data.map((m) => {
              const on = chosen.has(m.key);
              const names = m.inputs.map(inputName).filter(Boolean) as string[];
              return (
                <label
                  key={m.key}
                  className={cn("flex min-w-0 cursor-pointer items-start gap-3 rounded-lg border px-3.5 py-3 transition-colors hover:bg-accent/40", on && "border-foreground/30 bg-accent/50")}
                >
                  <Checkbox className="mt-0.5" checked={on} onCheckedChange={() => toggle(m.key)} />
                  <span className="flex min-w-0 flex-1 flex-col gap-1.5">
                    <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <span className="text-sm font-medium">{m.name}</span>
                      <Badge variant="outline" size="sm">{m.format === "lightgbm" ? "LightGBM" : "XGBoost"}</Badge>
                      {issueKeys.has(m.key) ? <Badge variant="danger" size="sm">有问题</Badge> : null}
                    </span>
                    {m.description ? <span className="text-xs leading-5 text-muted-foreground">{m.description}</span> : null}
                    <span className="text-xs leading-5 text-muted-foreground">
                      使用 {m.inputs.length} 项输入{names.length ? `：${names.slice(0, 4).join("、")}${names.length > 4 ? ` 等` : ""}` : ""}
                    </span>
                  </span>
                </label>
              );
            })}
          </div>
        )}
        {missing.length ? (
          <Callout
            tone="danger"
            title={`有 ${missing.length} 个已选模型已从模型库删除`}
            action={<Button size="xs" variant="outline" onClick={() => patch({ models: [...chosen].filter((k) => known.has(k)) })}>移除</Button>}
          >
            发布前需要移除，否则校验不通过。
          </Callout>
        ) : null}
        <Link href="/risk-control?tab=models" className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          管理模型库
          <ExternalLink className="size-3" />
        </Link>
      </StudioSection>
    </div>
  );
}

/* ════════════════════════ 场景策略 ════════════════════════ */

const POLICY_EXAMPLE = `package aegis.risk.scene

import rego.v1

# 老用户保护：前面各步提议拦截时，注册较早的用户（user_id < 100）改为加验
decision := {"action": "captcha", "reasons": array.concat(input.proposed.reasons, ["老用户保护"])} if {
	input.proposed.action == "block"
	input.request.user_id > 0
	input.request.user_id < 100
}
`;

function policyProblem(src: string): string | null {
  const body = src.replace(/^\s*(#[^\n]*\n\s*)*/, "");
  if (!body.trim()) return null;
  if (!/^package\s+aegis\.risk\.scene\b/.test(body)) return "第一行需要是 package aegis.risk.scene";
  if (!/\bdecision\b/.test(body)) return "需要给出 decision";
  return null;
}

const POLICY_INPUTS: Array<[string, string]> = [
  ["input.proposed", "前面各步得出的结论"],
  ["input.request", "请求信息"],
  ["input.score", "总分"],
  ["input.hits", "命中的规则"],
  ["input.dimensions", "各归类得分"],
  ["input.features", "特征取值"],
  ["input.models", "模型分"],
  ["input.lists", "名单命中"],
  ["input.scene", "场景设置"],
];

const GUARD_STEPS = [
  "命中黑名单时拦截",
  "命中白名单时放行",
  "要求验证码而请求已通过验证码时放行",
  "要求验证码而场景或应用不支持验证码时，改为转人工复核",
  "结论为封禁而场景不允许自动封禁时，改为只拦截",
];

export function PolicyPanel({ scene, patch, meta, issues, nameOf }: PanelProps) {
  const [confirm, setConfirm] = useState<"insert" | "clear" | null>(null);
  const [guardOpen, setGuardOpen] = useState(false);
  const has = !!scene.policy?.trim();
  const problem = policyProblem(scene.policy ?? "");

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <StudioSection
        icon={Code2}
        title="场景策略"
        description="在规则与决策流之后，对结论做最后的业务修正，例如保护老用户、节假日放宽。不需要时留空。"
        aside={
          <>
            <Badge variant={has ? "info" : "secondary"} size="sm">{has ? "已启用" : "未启用"}</Badge>
            <Button size="xs" variant="outline" onClick={() => (has ? setConfirm("insert") : patch({ policy: POLICY_EXAMPLE }))}>插入示例</Button>
            {has ? <Button size="xs" variant="ghost" onClick={() => setConfirm("clear")}>清空</Button> : null}
          </>
        }
      >
        {issues.length ? <IssueList issues={issues} nameOf={nameOf} /> : null}
        <CodeEditor language="rego" lineNumbers value={scene.policy ?? ""} onChange={(policy) => patch({ policy })} height={340} placeholder="package aegis.risk.scene" />
        {problem ? <Callout tone="warning">{problem}</Callout> : null}
        <div className="flex min-w-0 flex-col gap-2">
          <span className="text-xs text-muted-foreground">策略里能读取</span>
          <ul className="grid gap-x-4 gap-y-1.5 sm:grid-cols-2 lg:grid-cols-3">
            {POLICY_INPUTS.map(([code, label]) => (
              <li key={code} className="flex min-w-0 items-center gap-2 text-xs">
                <code className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{code}</code>
                <span className="min-w-0 truncate text-muted-foreground">{label}</span>
              </li>
            ))}
          </ul>
        </div>
      </StudioSection>

      <StudioSection icon={ShieldOff} title="内置护栏" description="每个场景在场景策略之后都会经过这一层，不能修改。">
        <ol className="flex flex-col gap-1.5">
          {GUARD_STEPS.map((s, i) => (
            <li key={i} className="flex items-start gap-2.5 text-sm">
              <span className="mt-0.5 grid size-5 shrink-0 place-items-center rounded-full bg-muted text-[11px] font-medium tabular-nums">{i + 1}</span>
              <span className="leading-6">{s}</span>
            </li>
          ))}
        </ol>
        <Collapsible open={guardOpen} onOpenChange={setGuardOpen}>
          <CollapsibleTrigger asChild>
            <Button size="xs" variant="ghost" className="w-fit text-muted-foreground">{guardOpen ? "收起代码" : "查看代码"}</Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-2">
            <CodeEditor language="rego" lineNumbers readOnly value={meta.guardPolicy} height={300} />
          </CollapsibleContent>
        </Collapsible>
      </StudioSection>

      <ConfirmDialog
        open={!!confirm}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={confirm === "clear" ? "清空场景策略" : "用示例替换现有策略"}
        description={confirm === "clear" ? "清空后结论不再经过场景策略修正。保存前可以放弃修改恢复。" : "现有策略会被示例覆盖。保存前可以放弃修改恢复。"}
        confirmLabel={confirm === "clear" ? "清空" : "替换"}
        danger
        onConfirm={() => {
          patch({ policy: confirm === "clear" ? "" : POLICY_EXAMPLE });
          setConfirm(null);
        }}
      />
    </div>
  );
}

/* ════════════════════════ 处置方式 ════════════════════════ */

const TARGETS: Array<{ value: EnforceTarget; label: string; detail: string; icon: LucideIcon }> = [
  { value: "device", label: "设备", detail: "设备加入黑名单", icon: Smartphone },
  { value: "ip", label: "IP", detail: "IP 加入黑名单", icon: Globe },
  { value: "account", label: "账号", detail: "封禁账号并强制下线", icon: User },
  { value: "firewall", label: "网关防火墙", detail: "在网关层直接屏蔽该 IP", icon: Server },
];

export function EnforcementPanel({ scene, patch }: PanelProps) {
  const enf = scene.enforcement ?? { targets: [], durationSeconds: 86400, autoBanAllowed: false };
  const set = (p: Partial<typeof enf>) => patch({ enforcement: { ...enf, ...p } });
  const presets = DURATION_PRESETS.some((p) => p.seconds === enf.durationSeconds)
    ? DURATION_PRESETS
    : [...DURATION_PRESETS, { label: formatWindow(enf.durationSeconds), seconds: enf.durationSeconds }];
  const targetText = TARGETS.filter((t) => enf.targets.includes(t.value)).map((t) => t.label).join("、");
  const durationText = enf.durationSeconds ? `，期限 ${formatWindow(enf.durationSeconds)}` : "，长期有效";
  const sentence = !enf.autoBanAllowed
    ? "结论为封禁时，只拦截本次请求，不执行封禁。"
    : enf.targets.length
      ? `结论为封禁时，立即处置${targetText}${durationText}，无需人工确认。`
      : "已允许自动封禁，但还没有选择处置对象。";

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <Callout tone={enf.autoBanAllowed ? (enf.targets.length ? "danger" : "warning") : "info"} icon={enf.autoBanAllowed ? Gavel : Eye} title="结论为封禁时">
        {sentence}
      </Callout>

      <StudioSection icon={Gavel} title="是否自动封禁">
        <div role="radiogroup" aria-label="是否自动封禁" className="grid gap-2 sm:grid-cols-2">
          <OptionCard selected={!enf.autoBanAllowed} onSelect={() => set({ autoBanAllowed: false })} icon={Eye} title="只拦截" description="封禁结论降为拦截，由人工决定是否封禁" />
          <OptionCard selected={enf.autoBanAllowed} onSelect={() => set({ autoBanAllowed: true })} icon={Gavel} accent="ban" title="自动封禁" description="按下面的对象与期限立即执行" />
        </div>
      </StudioSection>

      <StudioSection title="封禁对象" description={enf.autoBanAllowed ? "可多选。" : "开启自动封禁后生效。"} className={cn(!enf.autoBanAllowed && "opacity-70")}>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {TARGETS.map((t) => {
            const on = enf.targets.includes(t.value);
            return (
              <label key={t.value} className={cn("flex min-w-0 cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:bg-accent/40", on && "border-foreground/30 bg-accent/50")}>
                <Checkbox
                  className="mt-0.5"
                  checked={on}
                  onCheckedChange={() => set({ targets: on ? enf.targets.filter((x) => x !== t.value) : [...enf.targets, t.value] })}
                />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="inline-flex items-center gap-1.5 text-sm font-medium"><t.icon className="size-3.5 text-muted-foreground" />{t.label}</span>
                  <span className="text-xs leading-5 text-muted-foreground">{t.detail}</span>
                </span>
              </label>
            );
          })}
        </div>
      </StudioSection>

      <StudioSection title="封禁期限" className={cn(!enf.autoBanAllowed && "opacity-70")}>
        <div className="flex flex-wrap gap-2">
          {presets.map((p) => (
            <Button key={p.seconds} size="sm" variant={enf.durationSeconds === p.seconds ? "default" : "outline"} onClick={() => set({ durationSeconds: p.seconds })}>
              {p.seconds ? `封 ${p.label}` : "长期"}
            </Button>
          ))}
        </div>
      </StudioSection>
    </div>
  );
}

/* ════════════════════════ 分数段 ════════════════════════ */

export function BandsEditor({ bands, onChange, meta, compact }: { bands: ScoreBand[]; onChange: (b: ScoreBand[]) => void; meta: RiskMeta; compact?: boolean }) {
  const [confirmReset, setConfirmReset] = useState(false);
  const set = (i: number, patch: Partial<ScoreBand>) => onChange(bands.map((b, j) => (j === i ? { ...b, ...patch } : b)));
  const problems = bandProblems(bands);
  const order = bands.map((b, i) => ({ b, i })).sort((x, y) => x.b.min - y.b.min);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {bands.length ? <ScoreBandBar bands={bands} /> : null}
      {!compact && bands.length ? <BandLegend bands={bands} /> : null}
      {problems.length ? (
        <Callout tone="warning" title="分数段不完整">
          <ul className="flex flex-col gap-0.5">{problems.map((p, i) => <li key={i}>{p}</li>)}</ul>
        </Callout>
      ) : null}
      <ul className="flex min-w-0 flex-col divide-y rounded-lg border">
        {order.map(({ b, i }) => {
          const sw = ACTION_SWATCH[b.action] ?? ACTION_SWATCH.pass;
          const open = b.max <= 0;
          return (
            <li key={i} className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5">
              <span aria-hidden className={cn("h-8 w-1.5 shrink-0 rounded-full", sw.solid)} />
              <div className="flex items-center gap-1.5 text-sm">
                <NumberField className="w-20" value={b.min} min={0} onChange={(min) => set(i, { min })} ariaLabel="起始分" />
                <span className="text-muted-foreground">至</span>
                {open ? (
                  <Button size="sm" variant="outline" className="w-20" title="改为具体分数" onClick={() => set(i, { max: Math.max(b.min, 100) })}>不限</Button>
                ) : (
                  <NumberField className="w-20" value={b.max} min={0} onChange={(max) => set(i, { max })} ariaLabel="结束分" />
                )}
                <span className="text-muted-foreground">分</span>
                {!open ? <Button size="xs" variant="ghost" className="text-muted-foreground" onClick={() => set(i, { max: 0 })}>改为不限</Button> : null}
              </div>
              <span className="text-sm text-muted-foreground">处置为</span>
              <Select value={b.action} onValueChange={(v) => set(i, { action: v as RiskAction })}>
                <SelectTrigger size="sm" className="h-8 w-32"><SelectValue /></SelectTrigger>
                <SelectContent>{meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}</SelectContent>
              </Select>
              <span className="text-sm text-muted-foreground">记为</span>
              <Select value={b.level} onValueChange={(v) => set(i, { level: v as RiskLevel })}>
                <SelectTrigger size="sm" className="h-8 w-32"><SelectValue /></SelectTrigger>
                <SelectContent>{meta.levels.map((l) => <SelectItem key={l} value={l}>{LEVEL_WORD[l]}</SelectItem>)}</SelectContent>
              </Select>
              <span className="flex-1" />
              <Button size="icon-sm" variant="ghost" aria-label="删除分数段" onClick={() => onChange(bands.filter((_, j) => j !== i))}>
                <Trash2 className="size-3.5" />
              </Button>
            </li>
          );
        })}
        {bands.length === 0 ? <li className="px-3 py-6 text-center text-sm text-muted-foreground">还没有分数段</li> : null}
      </ul>
      <div className="flex flex-wrap gap-2">
        <Button size="xs" variant="outline" onClick={() => {
          const top = [...bands].sort((a, b) => b.min - a.min)[0];
          const start = top ? (top.max > 0 ? top.max + 1 : top.min + 10) : 0;
          onChange([...bands, { min: start, max: 0, action: "block", level: "high" }]);
        }}>
          <Plus />
          添加分数段
        </Button>
        <Button size="xs" variant="ghost" onClick={() => setConfirmReset(true)}>恢复默认</Button>
      </div>
      <ConfirmDialog
        open={confirmReset}
        onOpenChange={setConfirmReset}
        title="恢复默认分数段"
        description={<div className="flex flex-col gap-3"><span>当前分数段会被替换为：</span><BandLegend bands={meta.defaultBands} className="sm:grid-cols-1 xl:grid-cols-1" /></div>}
        confirmLabel="恢复默认"
        onConfirm={() => {
          onChange(meta.defaultBands);
          setConfirmReset(false);
        }}
      />
    </div>
  );
}
