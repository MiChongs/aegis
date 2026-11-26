"use client";

import { useEffect, useRef, useState, type ReactNode, type Ref } from "react";
import { BookOpen, CircleAlert, CircleCheck, Code2, LayoutTemplate, Loader2, Save, Sigma, Variable } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Field, FieldError, NumberField, Segmented, SettingRow } from "@/components/traffic/traffic-kit";
import { RULE_ACTION_META } from "@/components/firewall/firewall-kit";
import { FIREWALL_RULE_VARIABLES, type FirewallRule, type FirewallRuleAction, type FirewallRuleInput } from "@/lib/api/firewall";
import type { IPBanMode } from "@/lib/api/types";
import { useIPBanModesQuery } from "@/lib/admin-hooks";
import {
  firewallErrorMessage,
  useCreateFirewallRuleMutation,
  useUpdateFirewallRuleMutation,
  useValidateFirewallRuleMutation
} from "@/lib/firewall-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { EXPR_FUNCTIONS, EXPR_OPERATORS, ExprTokens, RULE_TEMPLATES, type RuleTemplate } from "./rule-expression";

/**
 * 自定义规则编辑器（新建与修改共用）。
 *
 * 表达式停止输入 400ms 后交给后端 `rules/validate` 试编译，错误就地显示；保存时后端还会用
 * 「改动之后」的整套规则再编译一次（40090），失败由 mutation 统一提示，弹窗保持打开。
 * 响应方式与封禁时长只对「拦截」有意义，其他动作保存时一律清零，免得留下看不见的旧值。
 */

export type RuleEditorTarget = {
  /** 每次打开换一个 key，表单从 seed 重新初始化 */
  key: number;
  rule?: FirewallRule;
  seed: FirewallRuleInput;
};

const ACTION_ORDER: FirewallRuleAction[] = ["block", "allow", "skip_waf", "log"];

const BAN_PRESETS = [
  { value: "0", label: "不封禁" },
  { value: "600", label: "10 分钟" },
  { value: "3600", label: "1 小时" },
  { value: "86400", label: "24 小时" },
  { value: "604800", label: "7 天" },
  { value: "-1", label: "永久" },
  { value: "custom", label: "自定义" }
] as const;

const DEFAULT_MODE = "__default";

type Form = Required<FirewallRuleInput>;

function seedForm(seed: FirewallRuleInput): Form {
  return {
    name: seed.name ?? "",
    description: seed.description ?? "",
    enabled: seed.enabled ?? true,
    priority: seed.priority ?? 100,
    expression: seed.expression ?? "",
    action: seed.action ?? "block",
    blockMode: seed.blockMode ?? "",
    banSeconds: seed.banSeconds ?? 0
  };
}

export function RuleEditorDialog({
  target,
  open,
  onOpenChange
}: {
  target: RuleEditorTarget | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(920px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        {target ? <RuleEditorBody key={target.key} target={target} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function RuleEditorBody({ target, onDone }: { target: RuleEditorTarget; onDone: () => void }) {
  const [form, setForm] = useState<Form>(() => seedForm(target.rule ?? target.seed));
  const [customBan, setCustomBan] = useState(() => !BAN_PRESETS.some((preset) => preset.value === String(form.banSeconds)));
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const create = useCreateFirewallRuleMutation();
  const update = useUpdateFirewallRuleMutation();
  const banModes = useIPBanModesQuery();
  const isNew = !target.rule;
  const set = (patch: Partial<Form>) => setForm((current) => ({ ...current, ...patch }));

  // 表达式实时校验
  const expression = form.expression.trim();
  const debounced = useDebouncedValue(expression, 400);
  const validate = useValidateFirewallRuleMutation();
  const { mutate: runValidate } = validate;
  useEffect(() => {
    if (debounced) runValidate({ expression: debounced, action: form.action });
  }, [debounced, form.action, runValidate]);
  const settled = validate.variables?.expression === expression && !validate.isPending && debounced === expression;
  const status: { state: "empty" | "checking" | "ok" | "error" | "unknown"; message?: string } = !expression
    ? { state: "empty" }
    : !settled
      ? { state: "checking" }
      : validate.isError
        ? { state: "unknown", message: firewallErrorMessage(validate.error, "暂时无法校验") }
        : validate.data?.valid
          ? { state: "ok" }
          : { state: "error", message: validate.data?.error || "表达式无效" };

  const insert = (snippet: string) => {
    const el = textareaRef.current;
    const value = form.expression;
    const start = el?.selectionStart ?? value.length;
    const end = el?.selectionEnd ?? value.length;
    const before = value.slice(0, start);
    const after = value.slice(end);
    const lead = before && !/[\s([!]$/.test(before) ? " " : "";
    const trail = after && !/^[\s)\],]/.test(after) ? " " : "";
    const next = `${before}${lead}${snippet}${trail}${after}`;
    set({ expression: next });
    const caret = before.length + lead.length + snippet.length;
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(caret, caret);
    });
  };

  const applyTemplate = (tpl: RuleTemplate) => {
    setForm((current) => ({
      ...current,
      name: current.name.trim() ? current.name : tpl.input.name,
      description: current.description.trim() ? current.description : (tpl.input.description ?? ""),
      expression: tpl.input.expression,
      action: tpl.input.action,
      blockMode: tpl.input.blockMode ?? "",
      banSeconds: tpl.input.banSeconds ?? 0
    }));
    setCustomBan(false);
  };

  const saving = create.isPending || update.isPending;
  const saveError = create.error ?? update.error;
  const canSave = Boolean(form.name.trim() && expression && status.state !== "error" && !saving);

  const save = () => {
    const input: FirewallRuleInput = {
      name: form.name.trim(),
      description: form.description.trim(),
      enabled: form.enabled,
      priority: Math.max(0, Math.round(form.priority)),
      expression,
      action: form.action,
      blockMode: form.action === "block" ? form.blockMode : "",
      banSeconds: form.action === "block" ? form.banSeconds : 0
    };
    if (target.rule) update.mutate({ id: target.rule.id, input }, { onSuccess: onDone });
    else create.mutate(input, { onSuccess: onDone });
  };

  const banValue = customBan ? "custom" : String(form.banSeconds);

  return (
    <>
      <DialogHeader className="border-b border-border px-5 py-4 pr-12">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <DialogTitle className="min-w-0 truncate">{isNew ? "新建规则" : `编辑规则「${target.rule?.name}」`}</DialogTitle>
          <ValidationBadge state={status.state} />
        </div>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="grid min-w-0 lg:grid-cols-[minmax(0,1fr)_340px]">
          <div className="flex min-w-0 flex-col gap-5 px-5 py-5">
            <div className="grid min-w-0 gap-4 sm:grid-cols-2">
              <Field label="名称">
                <Input value={form.name} maxLength={64} onChange={(event) => set({ name: event.target.value })} placeholder="例如：拦截扫描器" />
              </Field>
              <Field label="说明">
                <Input value={form.description} maxLength={200} onChange={(event) => set({ description: event.target.value })} />
              </Field>
            </div>

            <div className="flex min-w-0 flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">动作</span>
              <div role="radiogroup" aria-label="动作" className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {ACTION_ORDER.map((action) => {
                  const meta = RULE_ACTION_META[action];
                  const active = form.action === action;
                  return (
                    <button
                      key={action}
                      type="button"
                      role="radio"
                      aria-checked={active}
                      onClick={() => set({ action })}
                      className={cn(
                        "flex min-w-0 flex-col items-start gap-1 rounded-xl border px-3 py-2.5 text-left transition-colors",
                        active ? ACTION_ACTIVE[meta.tone] : "border-border hover:bg-accent"
                      )}
                    >
                      <span className="text-sm font-semibold">{meta.label}</span>
                      <span className="text-[11px] leading-4 text-muted-foreground">{meta.hint}</span>
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="flex min-w-0 flex-col gap-1.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">表达式</span>
                <span className="text-[11px] text-muted-foreground">结果为 true 即命中</span>
              </div>
              <ExpressionEditor
                ref={textareaRef}
                value={form.expression}
                onChange={(expressionValue) => set({ expression: expressionValue })}
                invalid={status.state === "error"}
              />
              {status.state === "error" || status.state === "unknown" ? (
                <p
                  className={cn(
                    "rounded-lg px-2.5 py-1.5 font-mono text-[11px] leading-4 break-all whitespace-pre-wrap",
                    status.state === "error" ? "bg-rose-500/10 text-rose-700 dark:text-rose-300" : "bg-muted text-muted-foreground"
                  )}
                >
                  {status.message}
                </p>
              ) : null}
            </div>

            {form.action === "block" ? (
              <div className="flex min-w-0 flex-col divide-y divide-border rounded-xl border border-border px-3">
                <SettingRow title="拦截响应方式">
                  <Select
                    value={form.blockMode || DEFAULT_MODE}
                    onValueChange={(value) => set({ blockMode: value === DEFAULT_MODE ? "" : (value as IPBanMode) })}
                  >
                    <SelectTrigger size="sm" className="w-48 max-w-[52vw]" aria-label="拦截响应方式">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent align="end">
                      <SelectItem value={DEFAULT_MODE}>默认（403 JSON）</SelectItem>
                      {(banModes.data?.modes ?? [])
                        .filter((mode) => mode.value)
                        .map((mode) => (
                          <SelectItem key={mode.value} value={mode.value}>
                            {mode.label}
                          </SelectItem>
                        ))}
                    </SelectContent>
                  </Select>
                </SettingRow>
                <SettingRow title="同时封禁来源 IP" stacked>
                  <div className="flex min-w-0 flex-col gap-2">
                    <Segmented
                      value={banValue}
                      options={BAN_PRESETS}
                      ariaLabel="封禁时长"
                      onChange={(value) => {
                        if (value === "custom") {
                          setCustomBan(true);
                          if (form.banSeconds <= 0) set({ banSeconds: 1800 });
                        } else {
                          setCustomBan(false);
                          set({ banSeconds: Number(value) });
                        }
                      }}
                    />
                    {customBan ? (
                      <NumberField
                        value={form.banSeconds}
                        min={1}
                        step={60}
                        suffix="秒"
                        onChange={(value) => set({ banSeconds: Math.max(1, Math.round(value)) })}
                        className="w-40"
                        ariaLabel="封禁秒数"
                      />
                    ) : null}
                  </div>
                </SettingRow>
              </div>
            ) : null}

            <div className="flex min-w-0 flex-col divide-y divide-border rounded-xl border border-border px-3">
              <SettingRow title="优先级" value="越小越先求值">
                <NumberField value={form.priority} min={0} step={10} onChange={(priority) => set({ priority })} className="w-24" ariaLabel="优先级" />
              </SettingRow>
              <SettingRow title="启用">
                <Switch checked={form.enabled} onCheckedChange={(enabled) => set({ enabled })} aria-label="启用" />
              </SettingRow>
            </div>
          </div>

          <aside className="min-w-0 border-t border-border bg-muted/20 px-4 py-4 lg:border-t-0 lg:border-l">
            <ReferencePanel onInsert={insert} onTemplate={applyTemplate} />
          </aside>
        </div>
      </div>

      <DialogFooter className="flex-row items-center gap-2 border-t border-border px-5 py-3">
        <FieldError message={saveError ? firewallErrorMessage(saveError) : null} className="mr-auto min-w-0 flex-1 truncate" />
        <Button variant="ghost" onClick={onDone}>
          取消
        </Button>
        <Button onClick={save} disabled={!canSave}>
          {saving ? <Loader2 className="animate-spin" /> : <Save />}
          保存
        </Button>
      </DialogFooter>
    </>
  );
}

const ACTION_ACTIVE: Record<string, string> = {
  danger: "border-rose-500/50 bg-rose-500/8 ring-1 ring-rose-500/30",
  success: "border-emerald-500/50 bg-emerald-500/8 ring-1 ring-emerald-500/30",
  warning: "border-amber-500/50 bg-amber-500/8 ring-1 ring-amber-500/30",
  info: "border-sky-500/50 bg-sky-500/8 ring-1 ring-sky-500/30"
};

function ValidationBadge({ state }: { state: "empty" | "checking" | "ok" | "error" | "unknown" }) {
  if (state === "checking")
    return (
      <Badge variant="outline" size="sm">
        <Loader2 className="animate-spin" />
        校验中
      </Badge>
    );
  if (state === "ok")
    return (
      <Badge variant="success" size="sm">
        <CircleCheck />
        表达式有效
      </Badge>
    );
  if (state === "error")
    return (
      <Badge variant="danger" size="sm">
        <CircleAlert />
        表达式有误
      </Badge>
    );
  return null;
}

/* ── 表达式输入：透明 textarea 叠在着色层上 ── */

function ExpressionEditor({
  ref,
  value,
  onChange,
  invalid
}: {
  ref: Ref<HTMLTextAreaElement>;
  value: string;
  onChange: (value: string) => void;
  invalid?: boolean;
}) {
  // 两层同处一个 grid 单元：高度由较高者撑开，不用同步滚动。字体、内边距、换行规则必须逐项一致。
  const shared = "col-start-1 row-start-1 min-h-28 w-full px-3 py-2.5 font-mono text-[13px] leading-6 break-all whitespace-pre-wrap";
  return (
    <div
      className={cn(
        "grid min-w-0 rounded-lg border border-input bg-transparent transition-colors focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50 dark:bg-input/30",
        invalid && "border-rose-500/60 focus-within:border-rose-500/60 focus-within:ring-rose-500/20"
      )}
    >
      <pre aria-hidden className={cn(shared, "pointer-events-none m-0 overflow-hidden")}>
        <ExprTokens source={value} />
        {/* 末尾换行在 pre 里不占高度，补一个空格 */}{" "}
      </pre>
      <textarea
        ref={ref}
        value={value}
        spellCheck={false}
        autoCapitalize="off"
        autoComplete="off"
        autoCorrect="off"
        aria-label="表达式"
        aria-invalid={invalid || undefined}
        placeholder={`path startsWith "/api/admin/" && method != "GET"`}
        onChange={(event) => onChange(event.target.value)}
        className={cn(shared, "resize-none overflow-hidden bg-transparent text-transparent caret-foreground outline-none selection:bg-sky-500/25 placeholder:text-muted-foreground")}
      />
    </div>
  );
}

/* ── 参考面板 ── */

function ReferencePanel({ onInsert, onTemplate }: { onInsert: (snippet: string) => void; onTemplate: (tpl: RuleTemplate) => void }) {
  return (
    <Tabs defaultValue="variables" className="gap-3">
      <TabsList className="w-full">
        <TabsTrigger value="variables" className="flex-1 text-xs">
          <Variable className="size-3.5" />
          变量
        </TabsTrigger>
        <TabsTrigger value="functions" className="flex-1 text-xs">
          <Sigma className="size-3.5" />
          函数
        </TabsTrigger>
        <TabsTrigger value="templates" className="flex-1 text-xs">
          <LayoutTemplate className="size-3.5" />
          模板
        </TabsTrigger>
      </TabsList>

      <TabsContent value="variables" className="flex max-h-[420px] flex-col gap-1 overflow-y-auto lg:max-h-[560px]">
        {FIREWALL_RULE_VARIABLES.map((item) => (
          <div key={item.name} className="flex min-w-0 flex-col gap-1 rounded-lg px-2 py-1.5 hover:bg-accent/60">
            <div className="flex min-w-0 items-center gap-1.5">
              <button
                type="button"
                onClick={() => onInsert(item.name)}
                title={`插入 ${item.name}`}
                className="rounded-md bg-sky-500/10 px-1.5 font-mono text-xs leading-5 text-sky-700 transition-colors hover:bg-sky-500/20 dark:text-sky-400"
              >
                {item.name}
              </button>
              <span className="min-w-0 flex-1 truncate text-xs">{item.label}</span>
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{item.type}</span>
            </div>
            <button
              type="button"
              onClick={() => onInsert(item.example)}
              title="插入示例"
              className="min-w-0 truncate rounded-md text-left font-mono text-[11px] leading-4 text-muted-foreground transition-colors hover:text-foreground"
            >
              <ExprTokens source={item.example} />
            </button>
          </div>
        ))}
      </TabsContent>

      <TabsContent value="functions" className="flex flex-col gap-3">
        <RefGroup icon={Code2} title="函数">
          {EXPR_FUNCTIONS.map((item) => (
            <SnippetRow key={item.snippet} snippet={item.snippet} label={item.label} onInsert={onInsert} />
          ))}
        </RefGroup>
        <RefGroup icon={BookOpen} title="运算符">
          {EXPR_OPERATORS.map((item) => (
            <SnippetRow key={item.snippet} snippet={item.snippet} label={item.label} onInsert={onInsert} />
          ))}
        </RefGroup>
      </TabsContent>

      <TabsContent value="templates" className="flex flex-col gap-2">
        {RULE_TEMPLATES.map((tpl) => (
          <button
            key={tpl.key}
            type="button"
            onClick={() => onTemplate(tpl)}
            className="flex min-w-0 flex-col gap-1.5 rounded-xl border border-border bg-card p-2.5 text-left transition-colors hover:border-foreground/30 hover:bg-accent/40"
          >
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="min-w-0 flex-1 truncate text-xs font-semibold">{tpl.name}</span>
              <span className="shrink-0 text-[11px] text-muted-foreground">{RULE_ACTION_META[tpl.input.action].label}</span>
            </span>
            <code className="block font-mono text-[11px] leading-4 break-all whitespace-pre-wrap">
              <ExprTokens source={tpl.input.expression} />
            </code>
          </button>
        ))}
      </TabsContent>
    </Tabs>
  );
}

function RefGroup({ icon: Icon, title, children }: { icon: typeof Code2; title: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-1">
      <h4 className="flex items-center gap-1.5 px-2 text-[11px] font-medium text-muted-foreground">
        <Icon className="size-3" />
        {title}
      </h4>
      {children}
    </section>
  );
}

function SnippetRow({ snippet, label, onInsert }: { snippet: string; label: string; onInsert: (snippet: string) => void }) {
  return (
    <button
      type="button"
      onClick={() => onInsert(snippet)}
      className="flex min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent/60"
    >
      <code className="min-w-0 flex-1 truncate font-mono text-[11px]">
        <ExprTokens source={snippet} />
      </code>
      <span className="shrink-0 text-[11px] text-muted-foreground">{label}</span>
    </button>
  );
}

