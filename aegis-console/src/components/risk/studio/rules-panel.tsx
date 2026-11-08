"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Ban, CheckCircle2, ChevronDown, Download, Eye, Gavel, LoaderCircle, MoreHorizontal, Pencil, Plus, ScrollText, Search, ShieldCheck, Sigma, Trash2, XCircle, Zap,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Field, NumberField, Segmented } from "@/components/traffic/traffic-kit";
import { DIM_LABEL } from "@/components/risk/kit/labels";
import type { CompileIssue, RiskAction, RiskMeta, Rule, RuleHit, RuleMode, RuleTemplate, Scene } from "@/lib/api/risk";
import { countText, percent, when } from "@/components/risk/kit/humanize";
import { useApplyTemplate, useDeleteRule, useRiskRuleHits, useRiskRules, useRiskTemplates, useSaveRule, useValidateExpr } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { CodeEditor } from "./code-editor";
import {
  completionsOf, ConditionBuilder, describeExpr, describeTree, exprToTree, SentenceView, tokensText, treeToExpr, varLabel, type CondTree, type VarOption,
} from "./condition-builder";
import {
  ACTION_SWATCH, ACTION_VERB, Callout, ConfirmDialog, EmptyBlock, ErrorBlock, errorText, IssueList, keyError, ListSkeleton, OptionCard, ruleOutcome, StudioSection,
} from "./kit";

/**
 * 规则面板：规则读成一句话（「当……时，加 50 分」），可就地启停、切换观察 / 生效；
 * 编辑在弹窗里，条件用可视化构建器或表达式，两边可互转。
 */

/** 条件即时校验：停止输入 400 ms 后提交一次，只采纳与当前文本一致的结果 */
function useExprCheck(expr: string) {
  const validate = useValidateExpr();
  const { mutate } = validate;
  const [result, setResult] = useState<{ expr: string; ok: boolean; error?: string } | null>(null);
  useEffect(() => {
    if (!expr.trim()) return;
    const timer = setTimeout(() => {
      mutate(expr, {
        onSuccess: (r) => setResult({ expr, ok: r.ok, error: r.error }),
        onError: (e) => setResult({ expr, ok: false, error: errorText(e, "校验失败") }),
      });
    }, 400);
    return () => clearTimeout(timer);
  }, [expr, mutate]);
  return result && result.expr === expr ? result : null;
}

function emptyRule(sceneKey: string): Partial<Rule> {
  return {
    scene: sceneKey, key: `rule_${Date.now().toString(36)}`, name: "", dimension: "custom", expr: "", score: 20, mode: "observe",
    decision: "", priority: 0, reason: "", enabled: true,
  };
}

/** 命中原因里的占位符换成名称：「IP 1 天注册 {{F.ip_register_1d}} 个」→「IP 1 天注册「IP 1 天注册数」个」 */
export function reasonText(reason: string | undefined, options: VarOption[]): string {
  return (reason ?? "").replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, name: string) => `「${varLabel(options, name)}」`);
}

/** 规则的整句：「当……时，加 50 分」 */
export function ruleSentence(rule: Pick<Rule, "expr" | "score" | "decision" | "mode">, options: VarOption[]): string {
  const text = tokensText(describeExpr(rule.expr ?? "", options).tokens) || "任意请求";
  return `当${text}${/[A-Za-z0-9]$/.test(text) ? " " : ""}时，${ruleOutcome(rule)}`;
}

const DECISIONS: Array<{ value: RiskAction | ""; title: string; hint: string; icon: typeof Sigma }> = [
  { value: "", title: "只加分", hint: "由总分决定处置", icon: Sigma },
  { value: "review", title: "转人工复核", hint: "放行并生成复核单", icon: Eye },
  { value: "captcha", title: "要求验证码", hint: "通过后才能继续", icon: ShieldCheck },
  { value: "block", title: "直接拦截", hint: "拒绝本次请求", icon: Ban },
  { value: "ban", title: "拦截并封禁", hint: "按处置方式执行封禁", icon: Gavel },
  { value: "pass", title: "直接放行", hint: "不再看总分", icon: CheckCircle2 },
];

const SCORE_PRESETS = [10, 20, 30, 50, 80, 100];

/* ── 编辑弹窗 ── */

function RuleDialog({ rule, sceneKey, meta, options, readOnly, onClose, onSaved }: {
  rule: Partial<Rule>;
  sceneKey: string;
  meta: RiskMeta;
  options: VarOption[];
  readOnly: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const save = useSaveRule();
  const [form, setForm] = useState<Partial<Rule>>(rule);
  const initialTree = useMemo(() => exprToTree(rule.expr ?? "", options), [rule.expr, options]);
  const [tab, setTab] = useState<"visual" | "expr">(initialTree ? "visual" : "expr");
  const [tree, setTree] = useState<CondTree>(initialTree ?? { join: "&&", groups: [{ join: "&&", rows: [{ variable: "", op: "true", value: "" }] }] });
  const [touched, setTouched] = useState(false);
  const [advanced, setAdvanced] = useState(false);
  const expr = tab === "visual" ? treeToExpr(tree, options) : form.expr ?? "";
  const check = useExprCheck(expr);
  const isNew = !rule.id;
  const kErr = keyError(form.key ?? "");
  const nErr = form.name?.trim() ? null : "请填写规则名称";
  const eErr = expr.trim() ? null : "请至少设置一个条件";
  const compileFailed = !!check && !check.ok;
  const set = (p: Partial<Rule>) => setForm((f) => ({ ...f, ...p }));

  const reading = tab === "visual" ? { tokens: describeTree(tree, options), loose: false } : describeExpr(form.expr ?? "", options);
  const preview = { ...form, expr };

  const switchTab = (next: string) => {
    if (next === "expr" && tab === "visual") {
      set({ expr: treeToExpr(tree, options) });
      setTab("expr");
    } else if (next === "visual" && tab === "expr") {
      const parsed = exprToTree(form.expr ?? "", options);
      if (!parsed) {
        toast.warning("这条表达式较复杂，只能用表达式编辑");
        return;
      }
      setTree(parsed);
      setTab("visual");
    }
  };

  const submit = () => {
    setTouched(true);
    if (kErr) setAdvanced(true);
    if (kErr || nErr || eErr || compileFailed) return;
    save.mutate({ ...form, scene: form.scene || sceneKey, expr }, {
      onSuccess: () => {
        toast.success(isNew ? "规则已创建" : "规则已保存");
        onSaved();
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };

  const placeholders = options.filter((o) => o.kind === "number" || o.kind === "string");

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-center gap-2">
            {readOnly ? "查看规则" : isNew ? "新建规则" : "编辑规则"}
            {form.scene === "*" ? <Badge variant="info" size="sm">所有场景共用</Badge> : null}
          </DialogTitle>
          <DialogDescription asChild>
            <div className="rounded-lg border bg-muted/40 px-3 py-2.5 text-sm text-foreground">
              <SentenceView
                tokens={reading.tokens}
                prefix="当"
                empty="尚未设置条件"
                suffix={<span>{reading.tokens.length ? " 时，" : "，"}<span className="font-semibold">{ruleOutcome(preview as Rule)}</span></span>}
              />
              {form.mode === "observe" ? <span className="ml-2 align-middle"><Badge variant="info" size="sm">观察中，不影响结果</Badge></span> : null}
            </div>
          </DialogDescription>
        </DialogHeader>

        <fieldset disabled={readOnly} className="flex min-w-0 flex-col gap-6">
          <div className="grid gap-3 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
            <Field label="规则名称" error={touched ? nErr : null}>
              <Input value={form.name ?? ""} placeholder="例如：IP 批量注册" onChange={(e) => set({ name: e.target.value })} />
            </Field>
            <Field label="归类">
              <Select value={form.dimension || "custom"} onValueChange={(dimension) => set({ dimension })}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {meta.dimensions.map((d) => <SelectItem key={d} value={d}>{DIM_LABEL[d] ?? d}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
          </div>

          <section className="flex min-w-0 flex-col gap-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <h4 className="text-sm font-medium">什么时候命中</h4>
              <span className="flex-1" />
              {expr.trim() ? (
                check ? (
                  check.ok ? (
                    <span className="inline-flex items-center gap-1 text-xs text-emerald-700 dark:text-emerald-400"><CheckCircle2 className="size-3.5" />条件可用</span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-xs text-red-600 dark:text-red-400"><XCircle className="size-3.5" />条件有误</span>
                  )
                ) : (
                  <span className="inline-flex items-center gap-1 text-xs text-muted-foreground"><LoaderCircle className="size-3.5 animate-spin" />检查中</span>
                )
              ) : null}
            </div>
            <Tabs value={tab} onValueChange={switchTab}>
              <TabsList>
                <TabsTrigger value="visual">逐条设置</TabsTrigger>
                <TabsTrigger value="expr">表达式</TabsTrigger>
              </TabsList>
              <TabsContent value="visual" className="mt-3">
                <ConditionBuilder tree={tree} onChange={setTree} options={options} readOnly={readOnly} />
              </TabsContent>
              <TabsContent value="expr" className="mt-3 flex flex-col gap-2">
                <CodeEditor
                  language="risk-expr"
                  value={form.expr ?? ""}
                  onChange={(v) => set({ expr: v })}
                  completions={completionsOf(options)}
                  readOnly={readOnly}
                  height={120}
                  placeholder="输入条件，可用补全选择变量与特征"
                />
                {reading.loose && reading.tokens.length ? (
                  <p className="text-xs leading-5 text-muted-foreground">读作：{tokensText(reading.tokens)}</p>
                ) : null}
              </TabsContent>
            </Tabs>
            {touched && eErr ? <p className="text-xs text-red-600 dark:text-red-400">{eErr}</p> : null}
            {compileFailed && check?.error ? <Callout tone="danger" title="条件无法运行">{check.error}</Callout> : null}
          </section>

          <section className="flex min-w-0 flex-col gap-2.5">
            <h4 className="text-sm font-medium">命中后</h4>
            <div role="radiogroup" aria-label="命中后的处置" className="grid gap-2 sm:grid-cols-3">
              {DECIDES(meta).map((d) => (
                <OptionCard
                  key={d.value || "none"}
                  selected={(form.decision || "") === d.value}
                  onSelect={() => set({ decision: d.value })}
                  title={d.title}
                  description={d.hint}
                  icon={d.icon}
                  accent={d.value || undefined}
                  disabled={readOnly}
                />
              ))}
            </div>
            <div className="flex min-w-0 flex-wrap items-center gap-2 pt-1">
              <span className="text-sm">{form.decision ? "同时计入" : "加"}</span>
              <NumberField className="w-24" value={form.score ?? 0} onChange={(score) => set({ score })} step={5} suffix="分" ariaLabel="分数" />
              <div className="flex flex-wrap gap-1">
                {SCORE_PRESETS.map((s) => (
                  <Button key={s} type="button" size="xs" variant={form.score === s ? "secondary" : "ghost"} onClick={() => set({ score: s })}>{s}</Button>
                ))}
              </div>
            </div>
            <p className="text-xs text-muted-foreground">各规则的分数累加为总分，再按决策流得出处置。</p>
          </section>

          <section className="flex min-w-0 flex-col gap-2.5">
            <h4 className="text-sm font-medium">运行方式</h4>
            <div role="radiogroup" aria-label="运行方式" className="grid gap-2 sm:grid-cols-2">
              <OptionCard selected={form.mode !== "observe"} onSelect={() => set({ mode: "enforce" })} icon={Zap} title="生效" description="命中后计分并参与处置" disabled={readOnly} />
              <OptionCard selected={form.mode === "observe"} onSelect={() => set({ mode: "observe" })} icon={Eye} title="观察" description="只记录命中，不影响结果，适合新规则试运行" disabled={readOnly} />
            </div>
          </section>

          <section className="flex min-w-0 flex-col gap-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <h4 className="text-sm font-medium">命中原因</h4>
              <span className="flex-1" />
              {!readOnly && placeholders.length ? (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button type="button" size="xs" variant="outline"><Plus />插入数值</Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="max-h-72 w-72 overflow-y-auto">
                    <DropdownMenuLabel>插入后显示为实际取值</DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    {placeholders.map((o) => (
                      <DropdownMenuItem key={o.name} onSelect={() => set({ reason: `${form.reason ?? ""}{{${o.name}}}` })}>
                        <span className="min-w-0 flex-1 truncate">{o.label}</span>
                        <span className="text-[11px] text-muted-foreground">{o.group}</span>
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </div>
            <Input value={form.reason ?? ""} placeholder="例如：IP 1 天注册过多账号" onChange={(e) => set({ reason: e.target.value })} />
            {/\{\{/.test(form.reason ?? "") ? <p className="text-xs text-muted-foreground">显示为：{reasonText(form.reason, options)}</p> : (
              <p className="text-xs text-muted-foreground">写进决策记录与复核单，帮助判断为什么被处置。</p>
            )}
          </section>

          <Field label="备注">
            <Textarea rows={2} value={form.description ?? ""} placeholder="给其他管理员看的说明" onChange={(e) => set({ description: e.target.value })} />
          </Field>

          <Collapsible open={advanced} onOpenChange={setAdvanced}>
            <CollapsibleTrigger asChild>
              <button type="button" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
                <ChevronDown className={cn("size-3.5 transition-transform", advanced && "rotate-180")} />
                高级设置
              </button>
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-3 grid gap-3 sm:grid-cols-2">
              <Field label="规则标识（创建后不能修改）" error={touched ? kErr : null}>
                <Input className="font-mono" value={form.key ?? ""} disabled={!isNew || readOnly} onChange={(e) => set({ key: e.target.value.trim() })} />
              </Field>
              <Field label="评估顺序（数值大的先评估）">
                <NumberField value={form.priority ?? 0} onChange={(priority) => set({ priority })} ariaLabel="评估顺序" />
              </Field>
            </CollapsibleContent>
          </Collapsible>
        </fieldset>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{readOnly ? "关闭" : "取消"}</Button>
          {!readOnly ? (
            <Button onClick={submit} disabled={save.isPending || compileFailed}>
              {save.isPending ? <LoaderCircle className="animate-spin" /> : null}
              {isNew ? "创建规则" : "保存"}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 只列出接口给出的处置；「只加分」始终在 */
function DECIDES(meta: RiskMeta) {
  return DECISIONS.filter((d) => d.value === "" || meta.actions.includes(d.value as RiskAction));
}

/* ── 模板 ── */

function TemplateCard({ template, installed, options, observe, pending, onInstall }: {
  template: RuleTemplate;
  installed: number;
  options: VarOption[];
  observe: boolean;
  pending: boolean;
  onInstall: () => void;
}) {
  const [open, setOpen] = useState(false);
  const total = template.rules.length;
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-lg border p-3.5">
      <div className="flex min-w-0 items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-sm font-medium">{template.name}</span>
            {template.scenes.includes("*") ? <Badge variant="secondary" size="sm">通用</Badge> : null}
            {installed > 0 ? <Badge variant={installed >= total ? "success" : "info"} size="sm">已装 {installed}/{total}</Badge> : null}
          </div>
          {template.description ? <p className="text-xs leading-5 text-muted-foreground">{template.description}</p> : null}
        </div>
        <Button size="sm" variant={installed >= total ? "outline" : "default"} disabled={pending} onClick={onInstall}>
          {pending ? <LoaderCircle className="animate-spin" /> : <Download />}
          {installed >= total ? "重新安装" : observe ? "以观察安装" : "安装并生效"}
        </Button>
      </div>
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger asChild>
          <button type="button" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
            <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
            包含 {total} 条规则
          </button>
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-2">
          <ul className="flex flex-col divide-y rounded-md border">
            {template.rules.map((r) => (
              <li key={r.key} className="flex min-w-0 flex-col gap-0.5 px-3 py-2">
                <span className="text-xs font-medium">{r.name}</span>
                <span className="text-xs leading-5 text-muted-foreground">{ruleSentence(r, options)}</span>
              </li>
            ))}
          </ul>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}

function TemplateDialog({ sceneKey, rules, options, onClose, onApplied }: {
  sceneKey: string;
  rules: Rule[];
  options: VarOption[];
  onClose: () => void;
  onApplied: () => void;
}) {
  const templates = useRiskTemplates();
  const apply = useApplyTemplate();
  const [observe, setObserve] = useState(true);
  const list = (templates.data ?? []).filter((t) => t.scenes.includes(sceneKey) || t.scenes.includes("*"));
  const keys = new Set(rules.filter((r) => r.scene === sceneKey).map((r) => r.key));

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>从模板安装</DialogTitle>
          <DialogDescription>模板是预先写好的一组规则，已存在的同名规则会跳过。</DialogDescription>
        </DialogHeader>
        <div role="radiogroup" aria-label="安装方式" className="grid gap-2 sm:grid-cols-2">
          <OptionCard selected={observe} onSelect={() => setObserve(true)} icon={Eye} title="先观察" description="只记录命中，确认无误后再改为生效" />
          <OptionCard selected={!observe} onSelect={() => setObserve(false)} icon={Zap} title="直接生效" description="安装后下次发布即参与处置" />
        </div>
        {templates.isLoading ? (
          <ListSkeleton rows={3} itemClassName="h-24" />
        ) : templates.isError ? (
          <ErrorBlock error={templates.error} onRetry={() => templates.refetch()} />
        ) : list.length === 0 ? (
          <EmptyBlock icon={ScrollText} title="没有适用于这个场景的模板" />
        ) : (
          <div className="flex flex-col gap-2.5">
            {list.map((t) => (
              <TemplateCard
                key={t.key}
                template={t}
                installed={t.rules.filter((r) => keys.has(r.key)).length}
                options={options}
                observe={observe}
                pending={apply.isPending && apply.variables?.template === t.key}
                onInstall={() => apply.mutate({ template: t.key, scene: sceneKey, observe }, {
                  onSuccess: (r) => {
                    toast.success(r.skipped ? `已安装 ${r.installed} 条，${r.skipped} 条已存在` : `已安装 ${r.installed} 条规则`);
                    onApplied();
                  },
                  onError: (e) => toast.error(errorText(e, "安装失败")),
                })}
              />
            ))}
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>完成</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── 概览图 ── */

function RulesOverview({ list, dim, onDim }: { list: Rule[]; dim: string | null; onDim: (d: string | null) => void }) {
  const enforce = list.filter((r) => r.enabled && r.mode !== "observe");
  const observe = list.filter((r) => r.enabled && r.mode === "observe");
  const off = list.filter((r) => !r.enabled);
  const decisive = enforce.filter((r) => r.decision).length;
  const total = list.length || 1;

  const dims = new Map<string, { score: number; count: number }>();
  for (const r of list) {
    const d = dims.get(r.dimension) ?? { score: 0, count: 0 };
    d.count += 1;
    if (r.enabled && r.mode !== "observe") d.score += Math.max(0, r.score);
    dims.set(r.dimension, d);
  }
  const dimRows = [...dims.entries()].sort((a, b) => b[1].score - a[1].score || b[1].count - a[1].count);
  const maxScore = Math.max(1, ...dimRows.map(([, d]) => d.score));

  const parts = [
    { label: "生效", n: enforce.length, cls: "bg-emerald-500" },
    { label: "观察", n: observe.length, cls: "bg-sky-500" },
    { label: "停用", n: off.length, cls: "bg-muted-foreground/30" },
  ];

  return (
    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
      <div className="flex min-w-0 flex-col gap-3 rounded-lg border bg-muted/20 p-4">
        <div className="flex items-baseline gap-2">
          <span className="text-3xl leading-none font-semibold tabular-nums">{list.length}</span>
          <span className="text-sm text-muted-foreground">条规则</span>
        </div>
        <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-muted" role="img" aria-label="规则状态构成">
          {parts.map((p) => (p.n ? <div key={p.label} className={cn("h-full border-r-2 border-card last:border-r-0", p.cls)} style={{ width: `${(p.n / total) * 100}%` }} /> : null))}
        </div>
        <ul className="flex flex-wrap gap-x-4 gap-y-1.5 text-xs">
          {parts.map((p) => (
            <li key={p.label} className="inline-flex items-center gap-1.5">
              <span aria-hidden className={cn("size-2 rounded-[3px]", p.cls)} />
              <span className="text-muted-foreground">{p.label}</span>
              <span className="font-medium tabular-nums">{p.n}</span>
            </li>
          ))}
        </ul>
        <p className="text-xs leading-5 text-muted-foreground">
          {decisive ? `${decisive} 条生效规则命中即直接处置，其余按总分处置。` : "生效规则都只加分，处置由总分决定。"}
        </p>
      </div>

      <div className="flex min-w-0 flex-col gap-2.5 rounded-lg border bg-muted/20 p-4">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium">各归类的生效分值</span>
          <span className="flex-1" />
          {dim ? <Button size="xs" variant="ghost" onClick={() => onDim(null)}>显示全部</Button> : null}
        </div>
        {dimRows.length === 0 ? <p className="text-xs text-muted-foreground">暂无规则</p> : null}
        <ul className="flex flex-col gap-1.5">
          {dimRows.map(([d, v]) => (
            <li key={d}>
              <button
                type="button"
                onClick={() => onDim(dim === d ? null : d)}
                className={cn(
                  "grid w-full grid-cols-[4.5rem_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-1.5 py-1 text-left text-xs transition-colors hover:bg-accent/50",
                  dim === d && "bg-accent",
                  dim && dim !== d && "opacity-50",
                )}
              >
                <span className="truncate">{DIM_LABEL[d] ?? d}</span>
                <span className="h-2 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-foreground/70 transition-[width] duration-500" style={{ width: `${(v.score / maxScore) * 100}%` }} />
                </span>
                <span className="w-24 text-right text-muted-foreground tabular-nums">{v.score} 分，{v.count} 条</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

/* ── 规则行 ── */

const HIT_DAYS = 7;

/** 近 7 天每天的命中次数，竖条高度按当天占最多那天的比例 */
function HitSpark({ daily, days, observe }: { daily: number[]; days: string[]; observe: boolean }) {
  const max = Math.max(1, ...daily);
  return (
    <span className="inline-flex h-4 items-end gap-[2px]" aria-hidden>
      {daily.map((n, i) => {
        const d = days[i] ? new Date(`${days[i]}T00:00:00Z`) : null;
        return (
          <span
            key={days[i] ?? i}
            title={d ? `${d.getUTCMonth() + 1} 月 ${d.getUTCDate()} 日命中 ${n.toLocaleString("zh-CN")} 次` : undefined}
            className={cn("w-[3px] rounded-[1px]", n ? (observe ? "bg-sky-500/70" : "bg-primary/80") : "bg-muted-foreground/20")}
            style={{ height: `${n ? Math.max(18, (n / max) * 100) : 12}%` }}
          />
        );
      })}
    </span>
  );
}

/** 「近 7 天命中 1,204 次，占评估 3.1%」「近 7 天未命中」 */
function HitLine({ hit, days, decisions, loading, enabled, observe }: {
  hit?: RuleHit; days: string[]; decisions: number; loading: boolean; enabled: boolean; observe: boolean;
}) {
  if (loading) return <span className="inline-block h-3 w-28 animate-pulse rounded bg-muted" />;
  if (!hit || !hit.total) {
    return <span>{enabled ? `近 ${HIT_DAYS} 天未命中` : `停用期间不统计命中`}</span>;
  }
  const last = when(hit.lastHour);
  return (
    <span className="inline-flex min-w-0 items-center gap-2">
      <HitSpark daily={hit.daily} days={days} observe={observe} />
      <span className="tabular-nums">
        近 {HIT_DAYS} 天命中 {countText(hit.total)}
        {decisions ? `，占评估 ${percent(hit.total, decisions)}` : ""}
      </span>
      {last.relative ? <span title={last.absolute}>最近在 {last.relative}</span> : null}
    </span>
  );
}

function RuleRow({ rule, options, issue, busy, hits, onToggle, onMode, onEdit, onDelete }: {
  rule: Rule;
  hits: { hit?: RuleHit; days: string[]; decisions: number; loading: boolean };
  options: VarOption[];
  issue: boolean;
  busy: boolean;
  onToggle: () => void;
  onMode: (mode: RuleMode) => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const global = rule.scene === "*";
  const { tokens } = describeExpr(rule.expr, options);
  const observe = rule.mode === "observe";
  const decision = rule.decision ? (rule.decision as RiskAction) : null;
  const sw = decision ? ACTION_SWATCH[decision] : null;

  return (
    <li className={cn("flex min-w-0 gap-3 px-4 py-3.5 transition-colors hover:bg-accent/30", !rule.enabled && "opacity-60")}>
      <div className="pt-0.5">
        <Switch checked={rule.enabled} disabled={global || busy} onCheckedChange={onToggle} aria-label={rule.enabled ? "停用规则" : "启用规则"} />
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <button type="button" onClick={onEdit} className="min-w-0 truncate text-left text-sm font-medium hover:underline">{rule.name || "未命名规则"}</button>
          {!rule.enabled ? <Badge variant="secondary" size="sm">已停用</Badge> : observe ? <Badge variant="info" size="sm">观察中</Badge> : null}
          {global ? <Badge variant="outline" size="sm">所有场景共用</Badge> : null}
          {rule.templateKey ? <Badge variant="outline" size="sm">来自模板</Badge> : null}
          {issue ? <Badge variant="danger" size="sm">有问题</Badge> : null}
        </div>
        <div className="text-sm text-foreground/90">
          <SentenceView tokens={tokens} prefix="当" empty="未设置条件" suffix={<span> 时，<span className="font-medium">{ruleOutcome(rule)}</span></span>} />
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <HitLine {...hits} enabled={rule.enabled} observe={observe} />
          <span>归类：{DIM_LABEL[rule.dimension] ?? rule.dimension}</span>
          {rule.priority ? <span>评估顺序 {rule.priority}</span> : null}
          {rule.reason ? <span className="min-w-0 truncate">原因：{reasonText(rule.reason, options)}</span> : null}
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end gap-2">
        <div className="flex items-center gap-1">
          <span
            className={cn(
              "inline-flex h-7 min-w-14 items-center justify-center rounded-md px-2 text-sm font-semibold tabular-nums",
              sw ? cn(sw.soft, sw.text) : "bg-muted",
              observe && "opacity-60",
            )}
            title={ruleOutcome(rule)}
          >
            {decision ? ACTION_VERB[decision].replace("直接", "") : `${rule.score > 0 ? "+" : ""}${rule.score}`}
          </span>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" aria-label="更多操作"><MoreHorizontal className="size-4" /></Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={onEdit}>
                {global ? <Eye /> : <Pencil />}
                {global ? "查看" : "编辑"}
              </DropdownMenuItem>
              {!global ? (
                <>
                  <DropdownMenuItem disabled={busy} onSelect={() => onMode(observe ? "enforce" : "observe")}>
                    {observe ? <Zap /> : <Eye />}
                    {observe ? "改为生效" : "改为观察"}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                    <Trash2 />
                    删除
                  </DropdownMenuItem>
                </>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        {!global ? (
          <Segmented
            value={observe ? "observe" : "enforce"}
            options={[{ value: "enforce" as RuleMode, label: "生效" }, { value: "observe" as RuleMode, label: "观察" }]}
            onChange={(m) => m !== rule.mode && onMode(m)}
            ariaLabel="运行方式"
            className={cn(busy && "pointer-events-none opacity-60")}
          />
        ) : null}
      </div>
    </li>
  );
}

/* ── 面板 ── */

type StatusFilter = "all" | "enforce" | "observe" | "off" | "idle";

export function RulesPanel({ sceneKey, meta, issues, options, onChanged, nameOf }: {
  sceneKey: string;
  scene: Scene;
  meta: RiskMeta;
  issues: CompileIssue[];
  options: VarOption[];
  onChanged: () => void;
  nameOf?: (issue: CompileIssue) => string | undefined;
}) {
  const rules = useRiskRules(sceneKey);
  const ruleHits = useRiskRuleHits(sceneKey, HIT_DAYS);
  const hitMap = useMemo(() => new Map((ruleHits.data?.rules ?? []).map((h) => [h.rule, h])), [ruleHits.data]);
  const hitsOf = (r: Rule) => ({
    hit: hitMap.get(r.key),
    days: ruleHits.data?.days ?? [],
    decisions: ruleHits.data?.decisions ?? 0,
    loading: ruleHits.isLoading,
  });
  const save = useSaveRule();
  const del = useDeleteRule();
  const [editing, setEditing] = useState<{ rule: Partial<Rule>; readOnly: boolean } | null>(null);
  const [installing, setInstalling] = useState(false);
  const [deleting, setDeleting] = useState<Rule | null>(null);
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [dim, setDim] = useState<string | null>(null);

  const all = useMemo(
    () => [...(rules.data ?? [])].filter((r) => r.scene === sceneKey || r.scene === "*").sort((a, b) => b.priority - a.priority || a.name.localeCompare(b.name, "zh-CN")),
    [rules.data, sceneKey],
  );
  const issueKeys = new Set(issues.map((i) => i.key));
  const needle = q.trim().toLowerCase();
  const list = all.filter((r) => {
    if (dim && r.dimension !== dim) return false;
    if (status === "enforce" && !(r.enabled && r.mode !== "observe")) return false;
    if (status === "observe" && !(r.enabled && r.mode === "observe")) return false;
    if (status === "off" && r.enabled) return false;
    if (status === "idle" && !(r.enabled && ruleHits.data && !hitMap.get(r.key)?.total)) return false;
    if (!needle) return true;
    return `${r.name} ${ruleSentence(r, options)} ${r.reason ?? ""} ${r.description ?? ""}`.toLowerCase().includes(needle);
  });
  const busyId = save.isPending ? save.variables?.id : undefined;

  const update = (r: Rule, patch: Partial<Rule>, msg: string) => save.mutate({ ...r, ...patch }, {
    onSuccess: () => {
      toast.success(msg);
      onChanged();
    },
    onError: (e) => toast.error(errorText(e)),
  });

  const create = () => setEditing({ rule: emptyRule(sceneKey), readOnly: false });

  return (
    <StudioSection
      icon={ScrollText}
      title="规则"
      description="规则命中后加分或直接处置，分数累加后由决策流得出结论。改动在下次发布后上线。"
      aside={
        <>
          <Button size="sm" variant="outline" onClick={() => setInstalling(true)}><Download />从模板安装</Button>
          <Button size="sm" onClick={create}><Plus />新建规则</Button>
        </>
      }
    >
      {issues.length ? <IssueList issues={issues} nameOf={nameOf} /> : null}

      {rules.isLoading ? (
        <>
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]"><ListSkeleton rows={1} itemClassName="h-36" /><ListSkeleton rows={1} itemClassName="h-36" /></div>
          <ListSkeleton rows={4} />
        </>
      ) : rules.isError ? (
        <ErrorBlock title="规则读取失败" error={rules.error} onRetry={() => rules.refetch()} />
      ) : all.length === 0 ? (
        <EmptyBlock
          icon={ScrollText}
          title="这个场景还没有规则"
          description="可以从模板一次装好一组常用规则，也可以从头写一条。新规则建议先以观察方式运行。"
          action={
            <>
              <Button size="sm" variant="outline" onClick={() => setInstalling(true)}><Download />从模板安装</Button>
              <Button size="sm" onClick={create}><Plus />新建规则</Button>
            </>
          }
        />
      ) : (
        <>
          <RulesOverview list={all} dim={dim} onDim={setDim} />
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <div className="relative min-w-48 flex-1 sm:max-w-72">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input className="h-8 pl-8" placeholder="搜索名称、条件或原因" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            <Segmented
              value={status}
              onChange={setStatus}
              ariaLabel="按状态筛选"
              options={[
                { value: "all" as const, label: "全部" },
                { value: "enforce" as const, label: "生效" },
                { value: "observe" as const, label: "观察" },
                { value: "off" as const, label: "停用" },
                { value: "idle" as const, label: `${HIT_DAYS} 天未命中` },
              ]}
            />
            {dim ? <Badge variant="secondary" size="default">归类：{DIM_LABEL[dim] ?? dim}</Badge> : null}
            <span className="ml-auto text-xs text-muted-foreground tabular-nums">显示 {list.length} 条</span>
          </div>
          {list.length === 0 ? (
            <EmptyBlock icon={Search} title="没有符合筛选的规则" action={<Button size="sm" variant="outline" onClick={() => { setQ(""); setStatus("all"); setDim(null); }}>清除筛选</Button>} />
          ) : (
            <ul className="flex min-w-0 flex-col divide-y overflow-hidden rounded-lg border">
              {list.map((r) => (
                <RuleRow
                  key={r.id}
                  rule={r}
                  options={options}
                  issue={issueKeys.has(r.key)}
                  busy={busyId === r.id}
                  hits={hitsOf(r)}
                  onToggle={() => update(r, { enabled: !r.enabled }, r.enabled ? `已停用「${r.name}」` : `已启用「${r.name}」`)}
                  onMode={(mode) => update(r, { mode }, mode === "observe" ? `「${r.name}」改为观察` : `「${r.name}」已生效`)}
                  onEdit={() => setEditing({ rule: r, readOnly: r.scene === "*" })}
                  onDelete={() => setDeleting(r)}
                />
              ))}
            </ul>
          )}
        </>
      )}

      {editing ? (
        <RuleDialog
          key={editing.rule.id ?? editing.rule.key ?? "new"}
          rule={editing.rule}
          sceneKey={sceneKey}
          meta={meta}
          options={options}
          readOnly={editing.readOnly}
          onClose={() => setEditing(null)}
          onSaved={onChanged}
        />
      ) : null}
      {installing ? <TemplateDialog sceneKey={sceneKey} rules={all} options={options} onClose={() => setInstalling(false)} onApplied={onChanged} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除规则「${deleting?.name ?? ""}」`}
        description="删除后不能恢复。线上版本仍会使用这条规则，直到下次发布。"
        confirmLabel="删除"
        danger
        pending={del.isPending}
        onConfirm={() => deleting && del.mutate(deleting.id, {
          onSuccess: () => {
            toast.success("规则已删除");
            setDeleting(null);
            onChanged();
          },
          onError: (e) => toast.error(errorText(e, "删除失败")),
        })}
      />
    </StudioSection>
  );
}
