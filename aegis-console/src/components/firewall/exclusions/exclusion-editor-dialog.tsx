"use client";

import { useState } from "react";
import { FlaskConical, Loader2, Save, ShieldAlert, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ChipInput, Field, FieldError, Segmented, SettingRow } from "@/components/traffic/traffic-kit";
import type { ExclusionPathMatch, FirewallExclusionInput } from "@/lib/api/firewall";
import {
  firewallErrorMessage,
  useCreateFirewallExclusionMutation,
  useFirewallCatalogQuery,
  useUpdateFirewallExclusionMutation
} from "@/lib/firewall-hooks";
import { cn } from "@/lib/utils";
import {
  EXCLUSION_METHODS,
  ExclusionSentence,
  PATH_MATCH_META,
  TAG_LABEL,
  TAG_SUGGESTIONS,
  TARGET_EXAMPLES,
  isTargetScoped
} from "./exclusion-format";
import { RuleIdPicker } from "./rule-id-picker";

/**
 * WAF 例外编辑器。后端保存前用「改动之后」的整套例外试编译 Coraza 指令，拼错返回 40093：
 * mutation 已统一弹提示，这里只在成功时关窗，失败保持打开并在底栏留一行原因。
 */

export type ExclusionEditorTarget = {
  key: number;
  /** 修改已有例外时为其 ID */
  id?: number;
  seed: FirewallExclusionInput;
};

type Form = Required<FirewallExclusionInput>;

function seedForm(seed: FirewallExclusionInput): Form {
  return {
    name: seed.name ?? "",
    note: seed.note ?? "",
    enabled: seed.enabled ?? true,
    pathMatch: seed.pathMatch ?? "prefix",
    path: seed.path ?? "",
    methods: seed.methods ?? [],
    ruleIds: seed.ruleIds ?? [],
    tags: seed.tags ?? [],
    targets: seed.targets ?? []
  };
}

export function toExclusionInput(form: Form): FirewallExclusionInput {
  return {
    name: form.name.trim(),
    note: form.note.trim(),
    enabled: form.enabled,
    pathMatch: form.pathMatch,
    path: form.path.trim(),
    methods: form.methods,
    ruleIds: form.ruleIds,
    tags: form.tags,
    targets: form.targets
  };
}

export function ExclusionEditorDialog({
  target,
  open,
  onOpenChange,
  onSandbox
}: {
  target: ExclusionEditorTarget | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 「在测试台验证」：带着当前表单去测试台 */
  onSandbox: (input: FirewallExclusionInput, editId?: number) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(900px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        {target ? <ExclusionEditorBody key={target.key} target={target} onDone={() => onOpenChange(false)} onSandbox={onSandbox} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function ExclusionEditorBody({
  target,
  onDone,
  onSandbox
}: {
  target: ExclusionEditorTarget;
  onDone: () => void;
  onSandbox: (input: FirewallExclusionInput, editId?: number) => void;
}) {
  const [form, setForm] = useState<Form>(() => seedForm(target.seed));
  const catalog = useFirewallCatalogQuery();
  const create = useCreateFirewallExclusionMutation();
  const update = useUpdateFirewallExclusionMutation();
  const set = (patch: Partial<Form>) => setForm((current) => ({ ...current, ...patch }));

  const input = toExclusionInput(form);
  const scoped = isTargetScoped(form);
  const saving = create.isPending || update.isPending;
  const saveError = create.error ?? update.error;
  const regexError = (() => {
    if (form.pathMatch !== "regex" || !form.path.trim()) return null;
    try {
      new RegExp(form.path.trim());
      return null;
    } catch {
      return "正则无法解析";
    }
  })();
  const missing = !input.name ? "填写名称" : !input.path ? "填写路径" : !form.ruleIds.length && !form.tags.length ? "至少选择一条规则或一个标签" : null;
  const canSave = !missing && !regexError && !saving;

  const save = () => {
    if (target.id) update.mutate({ id: target.id, input }, { onSuccess: onDone });
    else create.mutate(input, { onSuccess: onDone });
  };

  return (
    <>
      <DialogHeader className="border-b border-border px-5 py-4 pr-12">
        <DialogTitle className="truncate">{target.id ? `编辑例外「${target.seed.name}」` : "新建 WAF 例外"}</DialogTitle>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex min-w-0 flex-col gap-5 px-5 py-5">
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            <Field label="名称">
              <Input value={form.name} maxLength={64} onChange={(event) => set({ name: event.target.value })} placeholder="例如：反馈正文放过 XSS" />
            </Field>
            <Field label="备注">
              <Input value={form.note} maxLength={200} onChange={(event) => set({ note: event.target.value })} />
            </Field>
          </div>

          {/* 作用范围 */}
          <section className="flex min-w-0 flex-col gap-3">
            <h4 className="text-xs font-medium text-muted-foreground">作用于哪些请求</h4>
            <div className="flex min-w-0 flex-col gap-1.5">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <Segmented<ExclusionPathMatch>
                  value={form.pathMatch}
                  options={(Object.keys(PATH_MATCH_META) as ExclusionPathMatch[]).map((value) => ({ value, label: PATH_MATCH_META[value].label }))}
                  onChange={(pathMatch) => set({ pathMatch })}
                  ariaLabel="路径匹配方式"
                />
                <span className="text-[11px] text-muted-foreground">{PATH_MATCH_META[form.pathMatch].hint}</span>
              </div>
              <Input
                value={form.path}
                onChange={(event) => set({ path: event.target.value })}
                placeholder={form.pathMatch === "regex" ? "^/api/v1/apps/[^/]+/feedback$" : "/api/v1/apps/"}
                className="font-mono text-xs"
                aria-label="路径"
                aria-invalid={Boolean(regexError) || undefined}
              />
              <FieldError message={regexError} />
            </div>
            <div className="flex min-w-0 flex-col gap-1.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">方法</span>
                <span className="text-[11px] text-muted-foreground">{form.methods.length ? `${form.methods.length} 种` : "不选即全部"}</span>
              </div>
              <ToggleGroup
                type="multiple"
                variant="outline"
                size="sm"
                value={form.methods}
                onValueChange={(next) => set({ methods: EXCLUSION_METHODS.filter((method) => next.includes(method)) })}
                className="max-w-full flex-wrap"
                aria-label="方法"
              >
                {EXCLUSION_METHODS.map((method) => (
                  <ToggleGroupItem key={method} value={method} className="font-mono text-[11px]">
                    {method}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
          </section>

          {/* 排除什么 */}
          <section className="flex min-w-0 flex-col gap-3">
            <h4 className="text-xs font-medium text-muted-foreground">排除哪些规则</h4>
            <Field label="规则编号">
              <RuleIdPicker value={form.ruleIds} onChange={(ruleIds) => set({ ruleIds })} rules={catalog.data?.rules ?? []} loading={catalog.isLoading} />
            </Field>
            <Field label="规则标签">
              <ChipInput value={form.tags} onChange={(tags) => set({ tags })} placeholder="attack-xss" ariaLabel="规则标签" normalize={(raw) => raw.toLowerCase()} />
              <Suggestions
                items={TAG_SUGGESTIONS.filter((tag) => !form.tags.includes(tag)).map((tag) => ({ value: tag, label: TAG_LABEL[tag] }))}
                onPick={(tag) => set({ tags: [...form.tags, tag] })}
              />
            </Field>
          </section>

          {/* 检查目标 */}
          <section
            className={cn(
              "flex min-w-0 flex-col gap-3 rounded-xl border p-3.5",
              scoped ? "border-emerald-500/30 bg-emerald-500/5" : "border-amber-500/40 bg-amber-500/5"
            )}
          >
            <div className="flex min-w-0 items-start gap-2">
              {scoped ? (
                <ShieldCheck className="mt-0.5 size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
              ) : (
                <ShieldAlert className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
              )}
              <div className="min-w-0">
                <p className="text-sm font-medium">{scoped ? "只移除检查目标（推荐）" : "整条移除"}</p>
                <p className="text-xs text-muted-foreground">
                  {scoped
                    ? "这些规则照常检查请求的其他部分，只是不再看下面的字段。"
                    : "这些规则对匹配的请求完全不执行。填写检查目标可以只放过误报的字段。"}
                </p>
              </div>
            </div>
            <ChipInput value={form.targets} onChange={(targets) => set({ targets })} placeholder="ARGS:json.content" ariaLabel="检查目标" className="bg-background" />
            <Suggestions
              items={TARGET_EXAMPLES.filter((item) => !form.targets.includes(item.value))}
              onPick={(value) => set({ targets: [...form.targets, value] })}
              mono
            />
          </section>

          <div className="flex min-w-0 flex-col divide-y divide-border rounded-xl border border-border px-3">
            <SettingRow title="启用">
              <Switch checked={form.enabled} onCheckedChange={(enabled) => set({ enabled })} aria-label="启用" />
            </SettingRow>
          </div>

          <div className="rounded-xl border border-border bg-muted/30 px-3.5 py-3">
            <span className="mb-1 block text-[11px] font-medium text-muted-foreground">效果</span>
            <ExclusionSentence input={input} />
          </div>
        </div>
      </div>

      <DialogFooter className="flex-col gap-2 border-t border-border px-5 py-3 sm:flex-row sm:items-center">
        <FieldError message={saveError ? firewallErrorMessage(saveError) : null} className="min-w-0 flex-1 break-all sm:truncate" />
        {!saveError && missing ? <span className="min-w-0 flex-1 text-xs text-muted-foreground">{missing}</span> : null}
        {!saveError && !missing ? <span className="hidden flex-1 sm:block" /> : null}
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Button variant="outline" disabled={!input.path || Boolean(regexError)} onClick={() => onSandbox(input, target.id)}>
            <FlaskConical />
            在测试台验证
          </Button>
          <Button variant="ghost" onClick={onDone}>
            取消
          </Button>
          <Button onClick={save} disabled={!canSave}>
            {saving ? <Loader2 className="animate-spin" /> : <Save />}
            保存
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}

function Suggestions({ items, onPick, mono }: { items: { value: string; label?: string }[]; onPick: (value: string) => void; mono?: boolean }) {
  if (!items.length) return null;
  return (
    <div className="flex min-w-0 flex-wrap gap-1.5">
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          onClick={() => onPick(item.value)}
          title={item.label}
          className="inline-flex max-w-full min-w-0 items-center gap-1 rounded-md border border-dashed border-border px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
        >
          <span className={cn("truncate", mono && "font-mono")}>{item.value}</span>
          {item.label ? <span className="hidden truncate opacity-70 sm:inline">{item.label}</span> : null}
        </button>
      ))}
    </div>
  );
}
