"use client";

import { useState } from "react";
import Link from "next/link";
import { AlertTriangle, ExternalLink, Plus, Trash2 } from "lucide-react";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Field, NumberField, Segmented, SettingRow } from "@/components/traffic/traffic-kit";
import { KindBadge } from "@/components/risk/kit/badges";
import { ACTION_LABEL, DURATION_PRESETS, FEATURE_KIND_LABEL, formatWindow, LIST_TYPE_LABEL } from "@/components/risk/kit/labels";
import type { CompileIssue, EnforceTarget, Feature, FeatureKind, FieldType, ListType, RiskAction, RiskMeta, Scene, SceneField } from "@/lib/api/risk";
import { useRiskFeatures, useRiskModels } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { CodeEditor } from "./code-editor";
import { AppMultiSelect, IssueList, StudioSection } from "./kit";

export interface PanelProps {
  scene: Scene;
  patch: (p: Partial<Scene>) => void;
  meta: RiskMeta;
  issues: CompileIssue[];
}

const FIELD_TYPE_LABEL: Record<FieldType, string> = { string: "文本", number: "数值", bool: "布尔", list: "列表", object: "对象" };

/* ── 基本 ── */

function FieldsEditor({ fields, onChange }: { fields: SceneField[]; onChange: (f: SceneField[]) => void }) {
  const set = (i: number, p: Partial<SceneField>) => onChange(fields.map((f, j) => (j === i ? { ...f, ...p } : f)));
  return (
    <div className="flex min-w-0 flex-col gap-2">
      {fields.length ? (
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_7rem_4rem_auto] gap-2 text-xs text-muted-foreground">
          <span>键（extra.键）</span><span>名称</span><span>类型</span><span>必填</span><span />
        </div>
      ) : null}
      {fields.map((f, i) => (
        <div key={i} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_7rem_4rem_auto] items-center gap-2">
          <Input className="h-8 font-mono text-xs" value={f.key} onChange={(e) => set(i, { key: e.target.value.trim() })} />
          <Input className="h-8 text-xs" value={f.label} onChange={(e) => set(i, { label: e.target.value })} />
          <Select value={f.type} onValueChange={(v) => set(i, { type: v as FieldType })}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {(Object.keys(FIELD_TYPE_LABEL) as FieldType[]).map((t) => <SelectItem key={t} value={t}>{FIELD_TYPE_LABEL[t]}</SelectItem>)}
            </SelectContent>
          </Select>
          <Checkbox checked={!!f.required} onCheckedChange={(v) => set(i, { required: v === true })} aria-label="必填" />
          <Button size="icon-sm" variant="ghost" aria-label="删除字段" onClick={() => onChange(fields.filter((_, j) => j !== i))}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      ))}
      <Button size="xs" variant="outline" className="w-fit" onClick={() => onChange([...fields, { key: "", label: "", type: "string" }])}>
        <Plus />
        添加字段
      </Button>
    </div>
  );
}

export function BasicPanel({ scene, patch, meta, issues }: PanelProps) {
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {issues.length ? <IssueList issues={issues} /> : null}
      <StudioSection title="场景">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="名称">
            <Input value={scene.name} onChange={(e) => patch({ name: e.target.value })} />
          </Field>
          <Field label="场景键">
            <Input className="font-mono" value={scene.key} disabled />
          </Field>
        </div>
        <Field label="说明">
          <Textarea rows={2} value={scene.description ?? ""} onChange={(e) => patch({ description: e.target.value })} />
        </Field>
        <Field label="作用应用">
          <AppMultiSelect value={scene.appIds ?? []} onChange={(appIds) => patch({ appIds })} />
        </Field>
      </StudioSection>

      <StudioSection title="入参字段">
        <FieldsEditor fields={scene.fields ?? []} onChange={(fields) => patch({ fields })} />
      </StudioSection>

      <StudioSection title="运行">
        <div className="flex flex-col divide-y">
          <SettingRow title="启用">
            <Switch checked={scene.enabled} onCheckedChange={(enabled) => patch({ enabled })} />
          </SettingRow>
          <SettingRow title="超时预算" value={`${scene.timeoutMs} ms`}>
            <NumberField className="w-28" value={scene.timeoutMs} min={1} max={3000} step={50} suffix="ms" onChange={(timeoutMs) => patch({ timeoutMs })} />
          </SettingRow>
          <SettingRow title="出错时">
            <Segmented
              value={scene.failMode}
              options={[{ value: "open" as const, label: "放行" }, { value: "closed" as const, label: "拒绝" }]}
              onChange={(failMode) => patch({ failMode })}
            />
          </SettingRow>
          <SettingRow title="可出图形验证码">
            <Switch checked={scene.captchaSupported} onCheckedChange={(captchaSupported) => patch({ captchaSupported })} />
          </SettingRow>
          <SettingRow title="无规则命中时的处置">
            <Select value={scene.defaultAction || "none"} onValueChange={(v) => patch({ defaultAction: v === "none" ? "" : (v as RiskAction) })}>
              <SelectTrigger size="sm" className="h-8 w-36"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="none">按决策结果</SelectItem>
                {meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}
              </SelectContent>
            </Select>
          </SettingRow>
        </div>
      </StudioSection>
    </div>
  );
}

/* ── 名单 ── */

export function ListsPanel({ scene, patch, meta }: PanelProps) {
  const selected = scene.listTypes ?? [];
  const all = selected.length === 0;
  const toggle = (t: ListType) => {
    const base = all ? meta.listTypes : selected;
    const next = base.includes(t) ? base.filter((x) => x !== t) : [...base, t];
    patch({ listTypes: next.length === meta.listTypes.length ? [] : next });
  };
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <StudioSection title="查哪些名单" aside={<Badge variant={all ? "secondary" : "info"} size="sm">{all ? "全部类型" : `${selected.length} 类`}</Badge>}>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
          {meta.listTypes.map((t) => (
            <label key={t} className={cn("flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors hover:bg-accent/40", (all || selected.includes(t)) && "border-primary/40 bg-primary/5")}>
              <Checkbox checked={all || selected.includes(t)} onCheckedChange={() => toggle(t)} />
              {LIST_TYPE_LABEL[t] ?? t}
            </label>
          ))}
        </div>
        {!all ? (
          <Button size="xs" variant="ghost" className="w-fit" onClick={() => patch({ listTypes: [] })}>恢复全部类型</Button>
        ) : null}
      </StudioSection>
      <StudioSection title="命中后">
        <div className="flex flex-col divide-y text-sm">
          <div className="flex items-center gap-3 py-2"><KindBadge kind="block" /><span>内置护栏直接拦截</span></div>
          <div className="flex items-center gap-3 py-2"><KindBadge kind="allow" /><span>放行，跳过后续裁决</span></div>
          <div className="flex items-center gap-3 py-2"><KindBadge kind="watch" /><span>变量 <code className="font-mono text-xs">list.watch</code> 为真，由规则决定加分</span></div>
        </div>
        <Link href="/risk-control?tab=lists" className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          名单库
          <ExternalLink className="size-3" />
        </Link>
      </StudioSection>
    </div>
  );
}

/* ── 特征 ── */

const SOURCE_KINDS: FeatureKind[] = ["velocity", "stream", "graph", "vector"];

export function FeaturesPanel({ scene, patch, issues, mode, bundleFeatures }: PanelProps & { mode: "source" | "derived"; bundleFeatures: Feature[] }) {
  const features = useRiskFeatures();
  const kinds: FeatureKind[] = mode === "derived" ? ["derived"] : SOURCE_KINDS;
  const chosen = new Set(scene.features ?? []);
  const auto = new Set(bundleFeatures.map((f) => f.key).filter((k) => !chosen.has(k)));
  const issueKeys = new Set(issues.map((i) => i.key));
  const toggle = (key: string) => patch({ features: chosen.has(key) ? [...chosen].filter((k) => k !== key) : [...chosen, key] });
  const list = (features.data ?? []).filter((f) => kinds.includes(f.kind));

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {issues.length ? <IssueList issues={issues} /> : null}
      <p className="text-xs text-muted-foreground">规则、决策流、模型引用到的特征会自动带上，无需勾选。</p>
      {features.isLoading ? (
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} className="h-14 rounded-lg" />)}</div>
      ) : (
        kinds.map((kind) => {
          const items = list.filter((f) => f.kind === kind);
          if (!items.length) return null;
          return (
            <StudioSection key={kind} title={FEATURE_KIND_LABEL[kind]} aside={<Badge variant="secondary" size="sm">{items.length}</Badge>}>
              <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                {items.map((f) => {
                  const isAuto = auto.has(f.key);
                  const on = chosen.has(f.key) || isAuto;
                  return (
                    <label key={f.key} className={cn("flex min-w-0 cursor-pointer items-start gap-2 rounded-lg border px-3 py-2 transition-colors hover:bg-accent/40", on && "border-primary/40 bg-primary/5")}>
                      <Checkbox className="mt-0.5" checked={on} disabled={isAuto} onCheckedChange={() => toggle(f.key)} />
                      <span className="min-w-0 flex-1">
                        <span className="flex min-w-0 items-center gap-1.5">
                          <span className="truncate text-sm">{f.name}</span>
                          {isAuto ? <Badge variant="info" size="sm">自动引用</Badge> : null}
                          {issueKeys.has(f.key) ? <Badge variant="danger" size="sm">有问题</Badge> : null}
                        </span>
                        <span className="block truncate font-mono text-[11px] text-muted-foreground">F.{f.key}</span>
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
        管理特征
        <ExternalLink className="size-3" />
      </Link>
    </div>
  );
}

/* ── 模型 ── */

export function ModelsPanel({ scene, patch, issues }: PanelProps) {
  const models = useRiskModels();
  const chosen = new Set(scene.models ?? []);
  const issueKeys = new Set(issues.map((i) => i.key));
  const toggle = (key: string) => patch({ models: chosen.has(key) ? [...chosen].filter((k) => k !== key) : [...chosen, key] });
  const known = new Set((models.data ?? []).map((m) => m.key));
  const missing = [...chosen].filter((k) => models.data && !known.has(k));

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {issues.length ? <IssueList issues={issues} /> : null}
      <StudioSection title="模型" aside={<Badge variant="secondary" size="sm">已选 {chosen.size}</Badge>}>
        {models.isLoading ? (
          <div className="flex flex-col gap-2">{[0, 1].map((i) => <Skeleton key={i} className="h-16 rounded-lg" />)}</div>
        ) : !models.data?.length ? (
          <p className="text-sm text-muted-foreground">暂无模型</p>
        ) : (
          <div className="flex flex-col gap-2">
            {models.data.map((m) => (
              <label key={m.key} className={cn("flex min-w-0 cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 transition-colors hover:bg-accent/40", chosen.has(m.key) && "border-primary/40 bg-primary/5")}>
                <Checkbox className="mt-0.5" checked={chosen.has(m.key)} onCheckedChange={() => toggle(m.key)} />
                <span className="flex min-w-0 flex-1 flex-col gap-1.5">
                  <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                    <span className="text-sm font-medium">{m.name}</span>
                    <span className="font-mono text-[11px] text-muted-foreground">M.{m.key}</span>
                    <Badge variant="outline" size="sm">{m.format === "lightgbm" ? "LightGBM" : "XGBoost"}</Badge>
                    {issueKeys.has(m.key) ? <Badge variant="danger" size="sm">有问题</Badge> : null}
                  </span>
                  <span className="flex min-w-0 flex-wrap gap-1">
                    {m.inputs.map((input, i) => (
                      <span key={i} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{input}</span>
                    ))}
                  </span>
                </span>
              </label>
            ))}
          </div>
        )}
        {missing.length ? (
          <div className="flex flex-wrap items-center gap-1.5 text-xs text-red-600 dark:text-red-400">
            已删除：
            {missing.map((k) => (
              <button key={k} type="button" className="rounded bg-red-50 px-1.5 py-0.5 font-mono dark:bg-red-950/60" onClick={() => toggle(k)}>{k} ×</button>
            ))}
          </div>
        ) : null}
        <Link href="/risk-control?tab=models" className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          管理模型
          <ExternalLink className="size-3" />
        </Link>
      </StudioSection>
    </div>
  );
}

/* ── 策略 ── */

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
  if (!/^package\s+aegis\.risk\.scene\b/.test(body)) return "第一行必须是 package aegis.risk.scene";
  if (!/\bdecision\b/.test(body)) return "没有定义 decision";
  return null;
}

export function PolicyPanel({ scene, patch, meta, issues }: PanelProps) {
  const [confirm, setConfirm] = useState(false);
  const problem = policyProblem(scene.policy ?? "");
  const insert = () => (scene.policy?.trim() ? setConfirm(true) : patch({ policy: POLICY_EXAMPLE }));
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <StudioSection
        title="场景策略"
        aside={
          <>
            <Badge variant={scene.policy?.trim() ? "info" : "secondary"} size="sm">{scene.policy?.trim() ? "已配置" : "未配置"}</Badge>
            <Button size="xs" variant="outline" onClick={insert}>插入示例</Button>
            {scene.policy?.trim() ? <Button size="xs" variant="ghost" onClick={() => patch({ policy: "" })}>清空</Button> : null}
          </>
        }
      >
        {issues.length ? <IssueList issues={issues} /> : null}
        <CodeEditor language="rego" lineNumbers value={scene.policy ?? ""} onChange={(policy) => patch({ policy })} height={340} placeholder="package aegis.risk.scene" />
        {problem ? <p className="text-xs text-red-600 dark:text-red-400">{problem}</p> : null}
        <div className="flex flex-wrap gap-1.5 text-[11px] text-muted-foreground">
          {["input.request", "input.scene", "input.features", "input.models", "input.score", "input.dimensions", "input.hits", "input.lists", "input.proposed"].map((v) => (
            <code key={v} className="rounded bg-muted px-1.5 py-0.5 font-mono">{v}</code>
          ))}
        </div>
      </StudioSection>
      <StudioSection title="内置护栏" aside={<Badge variant="secondary" size="sm">只读</Badge>}>
        <CodeEditor language="rego" lineNumbers readOnly value={meta.guardPolicy} height={300} />
      </StudioSection>
      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>替换场景策略</AlertDialogTitle>
            <AlertDialogDescription>现有策略将被示例覆盖。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => patch({ policy: POLICY_EXAMPLE })}>替换</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/* ── 处置 ── */

const TARGETS: Array<{ value: EnforceTarget; label: string; detail: string }> = [
  { value: "device", label: "设备", detail: "设备加入黑名单" },
  { value: "ip", label: "IP", detail: "IP 加入黑名单" },
  { value: "account", label: "账号", detail: "封号并踢下线" },
  { value: "firewall", label: "防火墙", detail: "网关层封禁 IP" },
];

export function EnforcementPanel({ scene, patch }: PanelProps) {
  const enf = scene.enforcement ?? { targets: [], durationSeconds: 86400, autoBanAllowed: false };
  const set = (p: Partial<typeof enf>) => patch({ enforcement: { ...enf, ...p } });
  const presets = DURATION_PRESETS.some((p) => p.seconds === enf.durationSeconds)
    ? DURATION_PRESETS
    : [...DURATION_PRESETS, { label: formatWindow(enf.durationSeconds), seconds: enf.durationSeconds }];
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <StudioSection title="封禁对象">
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {TARGETS.map((t) => {
            const on = enf.targets.includes(t.value);
            return (
              <label key={t.value} className={cn("flex cursor-pointer items-start gap-2 rounded-lg border px-3 py-2.5 transition-colors hover:bg-accent/40", on && "border-primary/40 bg-primary/5")}>
                <Checkbox
                  className="mt-0.5"
                  checked={on}
                  onCheckedChange={() => set({ targets: on ? enf.targets.filter((x) => x !== t.value) : [...enf.targets, t.value] })}
                />
                <span className="flex flex-col">
                  <span className="text-sm font-medium">{t.label}</span>
                  <span className="text-xs text-muted-foreground">{t.detail}</span>
                </span>
              </label>
            );
          })}
        </div>
      </StudioSection>
      <StudioSection title="封禁时长">
        <Segmented
          value={String(enf.durationSeconds)}
          options={presets.map((p) => ({ value: String(p.seconds), label: p.label }))}
          onChange={(v) => set({ durationSeconds: Number(v) })}
        />
      </StudioSection>
      <StudioSection title="自动封禁" aside={<Switch checked={enf.autoBanAllowed} onCheckedChange={(autoBanAllowed) => set({ autoBanAllowed })} />}>
        {enf.autoBanAllowed ? (
          <div className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
            最终处置为封禁时，按上述对象立即执行，无需人工确认。
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">封禁降为拦截，不执行封禁。</p>
        )}
        {enf.autoBanAllowed && enf.targets.length === 0 ? <p className="text-xs text-amber-600 dark:text-amber-400">未选择封禁对象</p> : null}
      </StudioSection>
    </div>
  );
}
