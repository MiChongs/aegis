"use client";

import { useMemo, useState } from "react";
import { Activity, ChevronRight, Loader2, Pencil, Plus, Shield, Trash2 } from "lucide-react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  useCreateRiskRuleMutation, useDeleteRiskRuleMutation, useRiskRuleQuery,
  useRiskRulesQuery, useUpdateRiskRuleMutation, useValidateExpressionMutation,
} from "@/lib/risk-hooks";
import type { RiskRule } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  ConditionFields, ConditionHint, ConditionTypeSelect, ExpressionReference, ExpressionVerdict,
  conditionDataToForm, defaultConditionData, normalizeConditionData, validateConditionData,
  type ConditionData,
} from "./risk-condition-form";
import {
  DetailRow, InlineEmpty, LevelBadge, SceneBadge, fmtBucket, fmtDateTime, fmtNumber, fmtRelative,
  useRiskCatalog,
} from "./risk-shared";

/**
 * 风险规则。
 *
 * 相比重构前多出四件在实际运维里天天要用的能力：
 *   - **编辑**（此前只能新建和删除，改一个阈值要删掉重配）
 *   - **启停开关**（后端一直支持，前端没接）
 *   - **命中计数**（这条规则到底有没有在生效，是个可以直接看的数字）
 *   - **规则详情**（它最近拦下了哪些请求、命中趋势如何）
 */

type RuleForm = {
  name: string;
  description: string;
  scene: string;
  conditionType: string;
  score: string;
  priority: string;
  isActive: boolean;
  conditionData: ConditionData;
};

const EMPTY_FORM: RuleForm = {
  name: "", description: "", scene: "login", conditionType: "ip_frequency",
  score: "20", priority: "100", isActive: true, conditionData: {},
};

export function RiskRulesPanel({ focusRuleId, onFocusHandled }: {
  focusRuleId?: number | null;
  onFocusHandled?: () => void;
}) {
  const { scenes, conditions, conditionLabel } = useRiskCatalog();
  const [sceneFilter, setSceneFilter] = useState("");
  const [keyword, setKeyword] = useState("");
  const [editing, setEditing] = useState<RiskRule | "new" | null>(null);
  const [pendingDelete, setPendingDelete] = useState<RiskRule | null>(null);
  const [detailId, setDetailId] = useState<number | null>(null);

  const rulesQuery = useRiskRulesQuery(sceneFilter || undefined);
  const updateMut = useUpdateRiskRuleMutation();
  const deleteMut = useDeleteRiskRuleMutation();

  // 外部（大盘 Top 榜）点进来时打开详情
  const activeDetailId = focusRuleId ?? detailId;

  const rules = useMemo(() => {
    const list = rulesQuery.data ?? [];
    const kw = keyword.trim().toLowerCase();
    if (!kw) return list;
    return list.filter((rule) =>
      rule.name.toLowerCase().includes(kw) ||
      rule.description.toLowerCase().includes(kw) ||
      conditionLabel(rule.conditionType).toLowerCase().includes(kw));
  }, [rulesQuery.data, keyword, conditionLabel]);

  const toggleActive = async (rule: RiskRule, isActive: boolean) => {
    try {
      await updateMut.mutateAsync({ id: rule.id, data: { isActive } });
      toast.success(isActive ? "规则已启用" : "规则已停用");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={sceneFilter || "all"} onValueChange={(v) => setSceneFilter(v === "all" ? "" : v)}>
          <SelectTrigger className="h-8 w-32 text-xs"><SelectValue placeholder="全部场景" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部场景</SelectItem>
            {scenes.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <Input value={keyword} onChange={(e) => setKeyword(e.target.value)}
          placeholder="搜索规则名 / 说明 / 条件" className="h-8 w-56 text-xs" />
        <span className="text-xs text-muted-foreground">
          共 {rules.length} 条，启用 {rules.filter((r) => r.isActive).length} 条
        </span>
        <Button size="sm" className="ml-auto" onClick={() => setEditing("new")}>
          <Plus className="size-3.5" />新建规则
        </Button>
      </div>

      {rulesQuery.isLoading ? (
        <div className="space-y-2">{Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="h-16 animate-pulse rounded-xl bg-muted/50" />
        ))}</div>
      ) : rules.length === 0 ? (
        <EmptyState title="暂无规则"
          description={conditions.length === 0
            ? "目录加载中"
            : "没有规则时所有请求得分为 0，风控不会生效"} />
      ) : (
        <div className="space-y-2">
          {rules.map((rule) => (
            <RuleRow key={rule.id} rule={rule}
              onToggle={(next) => toggleActive(rule, next)}
              onEdit={() => setEditing(rule)}
              onDelete={() => setPendingDelete(rule)}
              onInspect={() => setDetailId(rule.id)} />
          ))}
        </div>
      )}

      {editing && (
        <RuleEditorSheet rule={editing === "new" ? null : editing} onClose={() => setEditing(null)} />
      )}

      {activeDetailId && (
        <RuleDetailSheet ruleId={activeDetailId} onClose={() => { setDetailId(null); onFocusHandled?.(); }} />
      )}

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除规则 {pendingDelete?.name}</AlertDialogTitle>
            <AlertDialogDescription>
删除后不再参与评估，历史记录中的命中留痕会保留
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={async () => {
              if (!pendingDelete) return;
              try {
                await deleteMut.mutateAsync(pendingDelete.id);
                toast.success("规则已删除");
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "删除失败");
              } finally {
                setPendingDelete(null);
              }
            }}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function RuleRow({ rule, onToggle, onEdit, onDelete, onInspect }: {
  rule: RiskRule;
  onToggle: (next: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
  onInspect: () => void;
}) {
  const { conditionLabel } = useRiskCatalog();
  // 从未命中过的启用规则值得被指出来：要么阈值配得过高，要么条件依赖的
  // 数据源没配 —— 两种都属于"以为防住了其实没有"。
  const neverHit = rule.isActive && rule.hitCount === 0;

  return (
    <div className={cn("group flex items-center gap-3 rounded-xl border bg-card px-4 py-3",
      !rule.isActive && "opacity-60")}>
      <Shield className={cn("size-4 shrink-0", rule.isActive ? "text-primary" : "text-muted-foreground")} />

      <button type="button" onClick={onInspect} className="min-w-0 flex-1 text-left">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-sm font-medium">{rule.name}</span>
          <SceneBadge scene={rule.scene} />
          <Badge variant="outline" size="sm">{conditionLabel(rule.conditionType)}</Badge>
          <Badge variant="outline" size="sm" className="font-mono">+{rule.score} 分</Badge>
          {neverHit && <Badge variant="warning" size="sm">从未命中</Badge>}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          {rule.description && <span className="truncate">{rule.description}</span>}
          <span className="tabular-nums">优先级 {rule.priority}</span>
          <span className="tabular-nums">累计命中 {fmtNumber(rule.hitCount)} 次</span>
          {rule.lastHitAt && <span>最近 {fmtRelative(rule.lastHitAt)}</span>}
        </div>
      </button>

      <div className="flex shrink-0 items-center gap-1">
        <Switch checked={rule.isActive} onCheckedChange={onToggle} aria-label="启用规则" />
        <Button variant="ghost" size="sm" className="h-7 px-2 opacity-0 group-hover:opacity-100" onClick={onEdit}>
          <Pencil className="size-3.5" />
        </Button>
        <Button variant="ghost" size="sm" className="h-7 px-2 text-destructive opacity-0 group-hover:opacity-100" onClick={onDelete}>
          <Trash2 className="size-3.5" />
        </Button>
        <ChevronRight className="size-4 text-muted-foreground" />
      </div>
    </div>
  );
}

/* ══════════════════════════════════════════════════════════
   规则编辑器
   ══════════════════════════════════════════════════════════ */

function RuleEditorSheet({ rule, onClose }: { rule: RiskRule | null; onClose: () => void }) {
  const { scenes, condition } = useRiskCatalog();
  const createMut = useCreateRiskRuleMutation();
  const updateMut = useUpdateRiskRuleMutation();
  const validateMut = useValidateExpressionMutation();

  // 草稿从服务端数据派生一次即可，不用 effect 同步（与配置面板同一约定：
  // effect 同步既触发级联渲染，也过不了 react-hooks/set-state-in-effect）。
  const [form, setForm] = useState<RuleForm>(() => {
    if (!rule) return { ...EMPTY_FORM, conditionData: {} };
    return {
      name: rule.name,
      description: rule.description,
      scene: rule.scene,
      conditionType: rule.conditionType,
      score: String(rule.score),
      priority: String(rule.priority),
      isActive: rule.isActive,
      conditionData: rule.conditionData ?? {},
    };
  });
  const [exprState, setExprState] = useState<{ status: "idle" | "checking" | "valid" | "invalid"; message?: string }>({ status: "idle" });

  const catalog = condition(form.conditionType);
  const isExpr = form.conditionType === "custom_expr";

  // 表单值：list 类型在编辑态是逗号分隔文本
  const formData = useMemo(() => conditionDataToForm(catalog, form.conditionData), [catalog, form.conditionData]);

  const changeConditionType = (next: string) => {
    const nextCatalog = condition(next);
    // 换条件类型时参数整组重置：留着上一种条件的参数会被后端校验拒绝，
    // 而错误信息里说的是一个用户根本没看到的字段。
    setForm((prev) => ({ ...prev, conditionType: next, conditionData: defaultConditionData(nextCatalog) }));
    setExprState({ status: "idle" });
  };

  const checkExpression = async (expression: string) => {
    if (!expression.trim()) { setExprState({ status: "idle" }); return; }
    setExprState({ status: "checking" });
    try {
      const result = await validateMut.mutateAsync(expression);
      setExprState(result.valid
        ? { status: "valid" }
        : { status: "invalid", message: result.error ?? "表达式无法编译" });
    } catch (error) {
      setExprState({ status: "invalid", message: error instanceof ApiError ? error.message : "校验请求失败" });
    }
  };

  const submit = async () => {
    if (!form.name.trim()) { toast.error("请填写规则名称"); return; }
    const normalized = normalizeConditionData(catalog, formData);
    const invalid = validateConditionData(catalog, formData);
    if (invalid) { toast.error(invalid); return; }
    if (isExpr && exprState.status === "invalid") { toast.error("请先修正表达式"); return; }

    const payload = {
      name: form.name.trim(),
      description: form.description.trim(),
      scene: form.scene,
      conditionType: form.conditionType,
      conditionData: normalized,
      score: Number(form.score) || 20,
      priority: Number(form.priority) || 100,
      isActive: form.isActive,
    };
    try {
      if (rule) {
        await updateMut.mutateAsync({ id: rule.id, data: payload });
        toast.success("规则已更新");
      } else {
        await createMut.mutateAsync(payload);
        toast.success("规则已创建");
      }
      onClose();
    } catch (error) {
      // 后端把「条件参数不合法 / 表达式编译失败」按 400 返回并带上具体原因，
      // 原样透出去 —— 这正是管理员需要看到的那一行。
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const pending = createMut.isPending || updateMut.isPending;

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle>{rule ? `编辑规则 ${rule.name}` : "新建风险规则"}</SheetTitle>
          <SheetDescription>
保存前校验场景、条件参数与表达式语法，不通过不会写入
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-5 py-4">
            <section className="space-y-3">
              <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">基础信息</h4>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1 sm:col-span-2">
                  <Label className="text-xs">规则名称 <span className="text-rose-500">*</span></Label>
                  <Input className="h-8 text-xs" value={form.name} placeholder="例如：同 IP 高频登录"
                    onChange={(e) => setForm({ ...form, name: e.target.value })} />
                </div>
                <div className="space-y-1 sm:col-span-2">
                  <Label className="text-xs">说明</Label>
                  <Input className="h-8 text-xs" value={form.description} placeholder="给同事看的一句话解释"
                    onChange={(e) => setForm({ ...form, description: e.target.value })} />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">场景</Label>
                  <Select value={form.scene} onValueChange={(v) => setForm({ ...form, scene: v })}>
                    <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {scenes.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">条件类型</Label>
                  <ConditionTypeSelect value={form.conditionType} onChange={changeConditionType} />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">命中得分</Label>
                  <Input type="number" min={1} className="h-8 font-mono text-xs" value={form.score}
                    onChange={(e) => setForm({ ...form, score: e.target.value })} />
                  <p className="text-[10px] text-muted-foreground">命中后累加到总分</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">优先级</Label>
                  <Input type="number" min={1} className="h-8 font-mono text-xs" value={form.priority}
                    onChange={(e) => setForm({ ...form, priority: e.target.value })} />
                  <p className="text-[10px] text-muted-foreground">数值越小越先评估</p>
                </div>
                <div className="flex items-center gap-2 sm:col-span-2">
                  <Switch checked={form.isActive} onCheckedChange={(v) => setForm({ ...form, isActive: v })} />
                  <Label className="text-xs">创建后立即启用</Label>
                </div>
              </div>
            </section>

            <Separator />

            <section className="space-y-3">
              <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">条件参数</h4>
              <ConditionHint catalog={catalog} />
              {isExpr ? (
                <div className="space-y-2">
                  <Label className="text-xs">表达式 <span className="text-rose-500">*</span></Label>
                  <Textarea rows={4} className="font-mono text-xs"
                    value={String(formData.expression ?? "")}
                    placeholder="ip_request_count > 100 and device_age_hours < 24"
                    onChange={(e) => setForm({ ...form, conditionData: { ...form.conditionData, expression: e.target.value } })}
                    onBlur={(e) => checkExpression(e.target.value)} />
                  <ExpressionVerdict state={exprState} />
                  <ExpressionReference onInsert={(snippet) => {
                    const current = String(formData.expression ?? "");
                    const next = current ? `${current} ${snippet}` : snippet;
                    setForm((prev) => ({ ...prev, conditionData: { ...prev.conditionData, expression: next } }));
                    setExprState({ status: "idle" });
                  }} />
                </div>
              ) : (
                <ConditionFields catalog={catalog} value={formData}
                  onChange={(next) => setForm({ ...form, conditionData: next })} />
              )}
            </section>
          </div>
        </ScrollArea>

        <div className="flex items-center justify-end gap-2 border-t px-5 py-3">
          <Button variant="ghost" size="sm" onClick={onClose}>取消</Button>
          <Button size="sm" disabled={pending} onClick={submit}>
            {pending && <Loader2 className="size-3.5 animate-spin" />}
            {rule ? "保存修改" : "创建规则"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

/* ══════════════════════════════════════════════════════════
   规则详情
   ══════════════════════════════════════════════════════════ */

const HIT_CHART_CONFIG: ChartConfig = {
  count: { label: "命中次数", theme: { light: "#6366f1", dark: "#818cf8" } },
};

function RuleDetailSheet({ ruleId, onClose }: { ruleId: number; onClose: () => void }) {
  const { conditionLabel } = useRiskCatalog();
  const query = useRiskRuleQuery(ruleId);
  const detail = query.data;

  const series = useMemo(
    () => (detail?.series ?? []).map((point) => ({ ...point, label: fmtBucket(point.time, "hour") })),
    [detail?.series],
  );

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-2">
            <Shield className="size-4" />
            {detail?.rule.name ?? "规则详情"}
          </SheetTitle>
          <SheetDescription>{detail?.explanation ?? "加载中…"}</SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {query.isLoading || !detail ? (
            <div className="space-y-3 p-5">
              {Array.from({ length: 5 }, (_, i) => <div key={i} className="h-12 animate-pulse rounded-lg bg-muted/50" />)}
            </div>
          ) : (
            <div className="space-y-5 px-5 py-4">
              <section className="grid grid-cols-3 gap-2">
                <MiniStat label="区间命中" value={fmtNumber(detail.stats.hits)} />
                <MiniStat label="其中拦截" value={fmtNumber(detail.stats.blocked)} tone="danger" />
                <MiniStat label="累计命中" value={fmtNumber(detail.rule.hitCount)} />
              </section>

              <section className="space-y-2">
                <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">定义</h4>
                <Card><CardContent className="p-3">
                  <DetailRow label="场景"><SceneBadge scene={detail.rule.scene} /></DetailRow>
                  <DetailRow label="条件类型">{conditionLabel(detail.rule.conditionType)}</DetailRow>
                  <DetailRow label="命中得分" mono>+{detail.rule.score}</DetailRow>
                  <DetailRow label="优先级" mono>{detail.rule.priority}</DetailRow>
                  <DetailRow label="状态">
                    <Badge variant={detail.rule.isActive ? "success" : "outline"} size="sm">
                      {detail.rule.isActive ? "启用中" : "已停用"}
                    </Badge>
                  </DetailRow>
                  <DetailRow label="条件参数" mono>
                    <pre className="whitespace-pre-wrap rounded bg-muted/50 px-2 py-1 text-[10px]">
                      {JSON.stringify(detail.rule.conditionData, null, 2)}
                    </pre>
                  </DetailRow>
                  <DetailRow label="最近命中">{fmtRelative(detail.rule.lastHitAt)}</DetailRow>
                  <DetailRow label="更新时间">{fmtDateTime(detail.rule.updatedAt)}</DetailRow>
                </CardContent></Card>
              </section>

              <section className="space-y-2">
                <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">命中趋势</h4>
                {series.length === 0 ? (
                  <InlineEmpty text="该区间内没有命中，可能是阈值偏高" />
                ) : (
                  <ChartContainer config={HIT_CHART_CONFIG} className="h-[180px] w-full">
                    <BarChart data={series} margin={{ top: 8, right: 8, bottom: 0, left: -20 }}>
                      <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                      <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} fontSize={10} />
                      <YAxis tickLine={false} axisLine={false} width={36} allowDecimals={false} fontSize={10} />
                      <ChartTooltip content={<ChartTooltipContent />} />
                      <Bar dataKey="count" fill="var(--color-count)" radius={[3, 3, 0, 0]} />
                    </BarChart>
                  </ChartContainer>
                )}
              </section>

              <section className="space-y-2">
                <h4 className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
                  <Activity className="size-3" />最近命中
                </h4>
                {(detail.recentHits ?? []).length === 0 ? (
                  <InlineEmpty text="暂无命中记录" height={80} />
                ) : (
                  <div className="space-y-1">
                    {(detail.recentHits ?? []).map((item) => (
                      <div key={item.id} className="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[11px]">
                        <LevelBadge level={item.riskLevel} />
                        <span className="font-mono">{item.ip || "--"}</span>
                        <span className="truncate text-muted-foreground">{item.account || "--"}</span>
                        <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                          {item.totalScore} 分，{fmtRelative(item.createdAt)}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function MiniStat({ label, value, tone }: { label: string; value: string; tone?: "danger" }) {
  return (
    <div className="rounded-lg border p-2.5 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className={cn("font-mono text-lg font-bold tabular-nums", tone === "danger" && "text-rose-600 dark:text-rose-400")}>
        {value}
      </div>
    </div>
  );
}
