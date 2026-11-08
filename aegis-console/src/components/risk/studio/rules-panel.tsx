"use client";

import { useEffect, useMemo, useState } from "react";
import { CheckCircle2, Download, Pencil, Plus, Trash2, XCircle } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Field, NumberField, Segmented } from "@/components/traffic/traffic-kit";
import { ActionBadge } from "@/components/risk/kit/badges";
import { ACTION_LABEL, DIM_LABEL } from "@/components/risk/kit/labels";
import type { CompileIssue, RiskAction, RiskMeta, Rule, RuleMode, Scene } from "@/lib/api/risk";
import { useApplyTemplate, useDeleteRule, useRiskFeatures, useRiskModels, useRiskRules, useRiskTemplates, useSaveRule, useValidateExpr } from "@/lib/risk-hooks";

import { CodeEditor } from "./code-editor";
import { buildVarOptions, completionsOf, ConditionBuilder, exprToTree, treeToExpr, type CondTree, type VarOption } from "./condition-builder";
import { errorText, IssueList, keyError, StudioSection } from "./kit";

const MODE_OPTIONS = [{ value: "enforce" as RuleMode, label: "生效" }, { value: "observe" as RuleMode, label: "观察" }];

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
  return { scene: sceneKey, key: "", name: "", dimension: "custom", expr: "", score: 20, mode: "enforce", decision: "", priority: 0, reason: "", enabled: true };
}

function RuleDialog({ rule, sceneKey, scene, meta, options, readOnly, onClose, onSaved }: {
  rule: Partial<Rule>;
  sceneKey: string;
  scene: Scene;
  meta: RiskMeta;
  options: VarOption[];
  readOnly: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const save = useSaveRule();
  const [form, setForm] = useState<Partial<Rule>>(rule);
  const initialTree = useMemo(() => exprToTree(rule.expr ?? ""), [rule.expr]);
  const [tab, setTab] = useState<"visual" | "expr">(initialTree ? "visual" : "expr");
  const [tree, setTree] = useState<CondTree>(initialTree ?? { join: "&&", groups: [{ join: "&&", rows: [] }] });
  const [touched, setTouched] = useState(false);
  const expr = tab === "visual" ? treeToExpr(tree, options) : form.expr ?? "";
  const check = useExprCheck(expr);
  const isNew = !rule.id;
  const kErr = keyError(form.key ?? "");
  const nErr = form.name?.trim() ? null : "必填";
  const eErr = expr.trim() ? null : "必填";

  const switchTab = (next: string) => {
    if (next === "expr" && tab === "visual") {
      setForm({ ...form, expr: treeToExpr(tree, options) });
      setTab("expr");
    } else if (next === "visual" && tab === "expr") {
      const parsed = exprToTree(form.expr ?? "");
      if (!parsed) {
        toast.warning("这条表达式超出可视化编辑范围");
        return;
      }
      setTree(parsed);
      setTab("visual");
    }
  };

  const submit = () => {
    setTouched(true);
    if (kErr || nErr || eErr) return;
    save.mutate({ ...form, scene: form.scene || sceneKey, expr }, {
      onSuccess: () => {
        toast.success("规则已保存");
        onSaved();
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {readOnly ? "查看规则" : isNew ? "新建规则" : "编辑规则"}
            {form.scene === "*" ? <Badge variant="info" size="sm">全部场景</Badge> : null}
          </DialogTitle>
        </DialogHeader>
        <fieldset disabled={readOnly} className="flex min-w-0 flex-col gap-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="规则键" error={touched ? kErr : null}>
              <Input className="font-mono" value={form.key ?? ""} disabled={!isNew} onChange={(e) => setForm({ ...form, key: e.target.value.trim() })} />
            </Field>
            <Field label="名称" error={touched ? nErr : null}>
              <Input value={form.name ?? ""} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </Field>
            <Field label="维度">
              <Select value={form.dimension || "custom"} onValueChange={(dimension) => setForm({ ...form, dimension })}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {meta.dimensions.map((d) => <SelectItem key={d} value={d}>{DIM_LABEL[d] ?? d}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
          </div>

          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">条件</span>
              <span className="flex-1" />
              {expr.trim() ? (
                check ? (
                  check.ok ? (
                    <span className="inline-flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400"><CheckCircle2 className="size-3.5" />编译通过</span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-xs text-red-600 dark:text-red-400"><XCircle className="size-3.5" />编译失败</span>
                  )
                ) : (
                  <span className="text-xs text-muted-foreground">校验中</span>
                )
              ) : null}
            </div>
            <Tabs value={tab} onValueChange={switchTab}>
              <TabsList>
                <TabsTrigger value="visual">可视化</TabsTrigger>
                <TabsTrigger value="expr">表达式</TabsTrigger>
              </TabsList>
              <TabsContent value="visual" className="mt-2">
                <ConditionBuilder tree={tree} onChange={setTree} options={options} readOnly={readOnly} />
                {expr ? <pre className="mt-2 rounded-md bg-muted px-2.5 py-1.5 font-mono text-[11px] break-all whitespace-pre-wrap">{expr}</pre> : null}
              </TabsContent>
              <TabsContent value="expr" className="mt-2">
                <CodeEditor
                  language="risk-expr"
                  value={form.expr ?? ""}
                  onChange={(v) => setForm({ ...form, expr: v })}
                  completions={completionsOf(options)}
                  readOnly={readOnly}
                  height={120}
                  placeholder="F.ip_scene_10m > 30 && net.proxy"
                />
              </TabsContent>
            </Tabs>
            {touched && eErr ? <p className="text-xs text-red-600 dark:text-red-400">{eErr}</p> : null}
            {check && !check.ok && check.error ? <pre className="rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 font-mono text-[11px] break-all whitespace-pre-wrap text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">{check.error}</pre> : null}
          </div>

          <div className="grid gap-3 sm:grid-cols-4">
            <Field label="分数">
              <NumberField value={form.score ?? 0} onChange={(score) => setForm({ ...form, score })} step={5} />
            </Field>
            <Field label="模式">
              <Segmented value={(form.mode as RuleMode) || "enforce"} options={MODE_OPTIONS} onChange={(mode) => setForm({ ...form, mode })} />
            </Field>
            <Field label="强制处置">
              <Select value={form.decision || "none"} onValueChange={(v) => setForm({ ...form, decision: v === "none" ? "" : (v as RiskAction) })}>
                <SelectTrigger className="h-8 w-full" size="sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">不强制</SelectItem>
                  {meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
            <Field label="优先级">
              <NumberField value={form.priority ?? 0} onChange={(priority) => setForm({ ...form, priority })} />
            </Field>
          </div>

          <Field label="命中原因（支持 {{F.x}}、{{ip}} 占位）">
            <Input value={form.reason ?? ""} placeholder="IP 10 分钟请求 {{F.ip_scene_10m}} 次" onChange={(e) => setForm({ ...form, reason: e.target.value })} />
          </Field>
          <Field label="说明">
            <Textarea rows={2} value={form.description ?? ""} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </Field>
          {scene.fields?.length ? (
            <p className="text-xs text-muted-foreground">场景入参：{scene.fields.map((f) => `extra.${f.key}`).join("、")}</p>
          ) : null}
        </fieldset>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{readOnly ? "关闭" : "取消"}</Button>
          {!readOnly ? <Button onClick={submit} disabled={save.isPending}>保存</Button> : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TemplateDialog({ sceneKey, onClose, onApplied }: { sceneKey: string; onClose: () => void; onApplied: () => void }) {
  const templates = useRiskTemplates();
  const apply = useApplyTemplate();
  const [observe, setObserve] = useState(true);
  const list = (templates.data ?? []).filter((t) => t.scenes.includes(sceneKey) || t.scenes.includes("*"));

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>安装模板</DialogTitle>
        </DialogHeader>
        <label className="flex items-center gap-2 text-sm">
          <Checkbox checked={observe} onCheckedChange={(v) => setObserve(v === true)} />
          以观察模式安装
        </label>
        {templates.isLoading ? (
          <div className="flex flex-col gap-2">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-24 rounded-lg" />)}</div>
        ) : list.length === 0 ? (
          <p className="text-sm text-muted-foreground">暂无适用模板</p>
        ) : (
          <div className="flex flex-col gap-2">
            {list.map((t) => (
              <div key={t.key} className="flex min-w-0 flex-col gap-2 rounded-lg border p-3">
                <div className="flex min-w-0 items-start gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 text-sm font-medium">
                      {t.name}
                      {t.scenes.includes("*") ? <Badge variant="secondary" size="sm">通用</Badge> : null}
                    </div>
                    <div className="text-xs text-muted-foreground">{t.description}</div>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={apply.isPending}
                    onClick={() => apply.mutate({ template: t.key, scene: sceneKey, observe }, {
                      onSuccess: (r) => {
                        toast.success(`安装 ${r.installed} 条，跳过 ${r.skipped} 条`);
                        onApplied();
                      },
                      onError: (e) => toast.error(errorText(e, "安装失败")),
                    })}
                  >
                    安装
                  </Button>
                </div>
                <div className="flex flex-wrap gap-1">
                  {t.rules.map((r) => <Badge key={r.key} variant="outline" size="sm">{r.name}</Badge>)}
                </div>
              </div>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export function RulesPanel({ sceneKey, scene, meta, issues, onChanged }: {
  sceneKey: string;
  scene: Scene;
  meta: RiskMeta;
  issues: CompileIssue[];
  onChanged: () => void;
}) {
  const rules = useRiskRules(sceneKey);
  const features = useRiskFeatures();
  const models = useRiskModels();
  const save = useSaveRule();
  const del = useDeleteRule();
  const [editing, setEditing] = useState<{ rule: Partial<Rule>; readOnly: boolean } | null>(null);
  const [installing, setInstalling] = useState(false);
  const [deleting, setDeleting] = useState<Rule | null>(null);

  const options = useMemo(
    () => buildVarOptions(meta.variables, features.data ?? [], models.data ?? [], scene.fields ?? []),
    [meta.variables, features.data, models.data, scene.fields],
  );
  const list = [...(rules.data ?? [])]
    .filter((r) => r.scene === sceneKey || r.scene === "*")
    .sort((a, b) => b.priority - a.priority || a.key.localeCompare(b.key));
  const issueKeys = new Set(issues.map((i) => i.key));

  const toggle = (r: Rule) => save.mutate({ ...r, enabled: !r.enabled }, {
    onSuccess: () => {
      toast.success(r.enabled ? "已停用" : "已启用");
      onChanged();
    },
    onError: (e) => toast.error(errorText(e)),
  });

  return (
    <StudioSection
      title={`规则 ${list.length}`}
      aside={
        <>
          <Button size="sm" variant="outline" onClick={() => setInstalling(true)}><Download />安装模板</Button>
          <Button size="sm" onClick={() => setEditing({ rule: emptyRule(sceneKey), readOnly: false })}><Plus />新建规则</Button>
        </>
      }
    >
      {issues.length ? <IssueList issues={issues} /> : null}
      {rules.isLoading ? (
        <div className="flex flex-col gap-2">{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-10 rounded-md" />)}</div>
      ) : list.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">暂无规则</p>
      ) : (
        <div className="min-w-0 overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-12">启用</TableHead>
                <TableHead>规则</TableHead>
                <TableHead>维度</TableHead>
                <TableHead className="min-w-56">条件</TableHead>
                <TableHead className="text-right">分数</TableHead>
                <TableHead>模式</TableHead>
                <TableHead>强制处置</TableHead>
                <TableHead className="text-right">优先级</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((r) => {
                const global = r.scene === "*";
                return (
                  <TableRow key={r.id} className={r.enabled ? undefined : "opacity-60"}>
                    <TableCell>
                      <Switch checked={r.enabled} disabled={global || save.isPending} onCheckedChange={() => toggle(r)} aria-label="启用" />
                    </TableCell>
                    <TableCell>
                      <div className="flex min-w-0 items-center gap-1.5">
                        <span className="truncate text-sm">{r.name}</span>
                        {global ? <Badge variant="info" size="sm">全部场景</Badge> : null}
                        {r.templateKey ? <Badge variant="outline" size="sm">模板</Badge> : null}
                        {issueKeys.has(r.key) ? <Badge variant="danger" size="sm">有问题</Badge> : null}
                      </div>
                      <div className="font-mono text-[11px] text-muted-foreground">{r.key}</div>
                    </TableCell>
                    <TableCell className="text-xs">{DIM_LABEL[r.dimension] ?? r.dimension}</TableCell>
                    <TableCell><code className="line-clamp-2 font-mono text-[11px] break-all">{r.expr}</code></TableCell>
                    <TableCell className="text-right tabular-nums">{r.score}</TableCell>
                    <TableCell>
                      <Badge variant={r.mode === "observe" ? "info" : "success"} size="sm">{r.mode === "observe" ? "观察" : "生效"}</Badge>
                    </TableCell>
                    <TableCell>{r.decision ? <ActionBadge action={r.decision} /> : <span className="text-xs text-muted-foreground">无</span>}</TableCell>
                    <TableCell className="text-right tabular-nums">{r.priority}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-0.5">
                        <Button size="icon-sm" variant="ghost" aria-label={global ? "查看" : "编辑"} onClick={() => setEditing({ rule: r, readOnly: global })}>
                          <Pencil className="size-3.5" />
                        </Button>
                        {!global ? (
                          <Button size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setDeleting(r)}>
                            <Trash2 className="size-3.5" />
                          </Button>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}

      {editing ? (
        <RuleDialog
          key={editing.rule.id ?? "new"}
          rule={editing.rule}
          sceneKey={sceneKey}
          scene={scene}
          meta={meta}
          options={options}
          readOnly={editing.readOnly}
          onClose={() => setEditing(null)}
          onSaved={onChanged}
        />
      ) : null}
      {installing ? <TemplateDialog sceneKey={sceneKey} onClose={() => setInstalling(false)} onApplied={onChanged} /> : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除规则</AlertDialogTitle>
            <AlertDialogDescription>删除「{deleting?.name}」，下次发布后不再参与评估。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => deleting && del.mutate(deleting.id, {
                onSuccess: () => {
                  toast.success("规则已删除");
                  onChanged();
                },
                onError: (e) => toast.error(errorText(e, "删除失败")),
              })}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </StudioSection>
  );
}
