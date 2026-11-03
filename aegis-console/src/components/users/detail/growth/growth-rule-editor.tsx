"use client";

/**
 * 倍率规则编辑器：新建与修改共用。左边是表单，右边是实时推演（这笔收入在当前规则 + 这条草稿下会入账多少）。
 *
 * 倍率输入拆成「× / ÷」与一个数，而不是让人写分数：×1.5 与 ÷3 是管理员脑子里的说法，分数只在提交时换算。
 * 有效期用常用时长快捷选，也可以填具体的起止时间；不填结束时间就一直生效到撤销。
 */

import * as React from "react";
import { ArrowRight, Calculator, Coins, Divide, Loader2, Sparkles, X as XIcon } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import {
  factorLabel,
  type GrowthCatalog,
  type GrowthKind,
  type GrowthPreview,
  type GrowthRounding,
  type GrowthRule,
  type GrowthRulePayload
} from "@/lib/api/growth";
import { useGrowthPreviewMutation, useSaveGrowthRuleMutation } from "@/lib/growth-hooks";
import { cn } from "@/lib/utils";
import { Segmented } from "../user-assets-adjust";
import { EFFECT_META, EffectPill, effectOf, fractionFrom, fractionToInput } from "./growth-kit";

const DURATIONS: Array<{ key: string; label: string; hours: number | null }> = [
  { key: "1d", label: "1 天", hours: 24 },
  { key: "3d", label: "3 天", hours: 72 },
  { key: "7d", label: "7 天", hours: 168 },
  { key: "30d", label: "30 天", hours: 720 },
  { key: "forever", label: "长期", hours: null },
  { key: "custom", label: "自定义", hours: null }
];

const CAP_QUICK = [0, 50, 100, 500, 1000, 5000];
const PENALTY_REASONS = ["刷分行为", "违规使用外挂", "多账号套利", "异常设备集中", "申诉处理中"];
const BOOST_REASONS = ["活动奖励", "内测贡献", "客诉补偿", "优质用户激励"];

type Scope = "user" | "app";

/** datetime-local 的值 ↔ ISO。 */
function toLocalInput(iso?: string | null) {
  if (!iso) return "";
  const date = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function fromLocalInput(value: string) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

type Draft = {
  scope: Scope;
  kind: GrowthKind;
  mode: "mul" | "div";
  value: string;
  rounding: GrowthRounding;
  dailyCap: string;
  sources: string[];
  duration: string;
  startsAt: string;
  endsAt: string;
  enabled: boolean;
  label: string;
  reason: string;
};

/** 快捷入口给新规则预填的倍率、上限与时长。 */
export type GrowthRuleSeed = { numerator: number; denominator: number; dailyCap?: number; duration?: string; reason?: string };

function draftFrom(rule: GrowthRule | null, kind: GrowthKind, scope: Scope, seed?: GrowthRuleSeed): Draft {
  if (!rule) {
    const factor = seed ? fractionToInput(seed.numerator, seed.denominator) : { mode: "div" as const, value: "2" };
    return {
      scope,
      kind,
      mode: factor.mode,
      value: factor.value,
      rounding: "floor",
      dailyCap: String(seed?.dailyCap ?? 0),
      sources: [],
      duration: seed?.duration ?? "7d",
      startsAt: "",
      endsAt: "",
      enabled: true,
      label: "",
      reason: seed?.reason ?? ""
    };
  }
  const input = fractionToInput(rule.numerator, rule.denominator);
  return {
    scope: rule.userId > 0 ? "user" : "app",
    kind: rule.kind,
    mode: input.mode,
    value: input.value,
    rounding: rule.rounding,
    dailyCap: String(rule.dailyCap),
    sources: rule.sources ?? [],
    duration: rule.startsAt || rule.endsAt ? "custom" : "forever",
    startsAt: toLocalInput(rule.startsAt),
    endsAt: toLocalInput(rule.endsAt),
    enabled: rule.enabled,
    label: rule.label,
    reason: rule.reason
  };
}

type EditorProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  userId: number;
  catalog?: GrowthCatalog;
  /** 给出时是修改 */
  rule?: GrowthRule | null;
  defaultKind?: GrowthKind;
  defaultScope?: Scope;
  seed?: GrowthRuleSeed;
};

export function GrowthRuleEditor(props: EditorProps) {
  const save = useSaveGrowthRuleMutation(props.appKey);
  // 表单状态放在正文组件里：对话框关闭时正文卸载，下次打开按当时的规则 / 预填重新初始化
  return (
    <Dialog open={props.open} onOpenChange={(next) => (!save.isPending ? props.onOpenChange(next) : undefined)}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-3xl">
        {props.open ? <RuleEditorBody {...props} save={save} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function RuleEditorBody({
  open,
  onOpenChange,
  appKey,
  userId,
  catalog,
  rule,
  defaultKind = "integral",
  defaultScope = "user",
  seed,
  save
}: EditorProps & { save: ReturnType<typeof useSaveGrowthRuleMutation> }) {
  const editing = Boolean(rule);
  const [draft, setDraft] = React.useState<Draft>(() => draftFrom(rule ?? null, defaultKind, defaultScope, seed));
  const [previewBase, setPreviewBase] = React.useState("100");
  const [previewSource, setPreviewSource] = React.useState("signin");
  const [preview, setPreview] = React.useState<GrowthPreview | null>(null);
  const [openedAt] = React.useState(() => Date.now());
  const previewMutation = useGrowthPreviewMutation(appKey);
  const runPreview = previewMutation.mutateAsync;

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((prev) => ({ ...prev, [key]: value }));
  const limit = catalog?.limits.maxFactorTerm ?? 1000;
  const fraction = fractionFrom(draft.mode, draft.value, limit);
  const cap = Number.parseInt(draft.dailyCap || "0", 10);
  const capValid = Number.isSafeInteger(cap) && cap >= 0 && cap <= (catalog?.limits.maxDailyCap ?? 100_000_000);
  const effect = fraction ? effectOf(fraction.num, fraction.den, capValid ? cap : 0) : "neutral";
  const kindSources = React.useMemo(() => (catalog?.sources ?? []).filter((s) => s.kinds.includes(draft.kind)), [catalog, draft.kind]);

  const period = React.useMemo(() => {
    const preset = DURATIONS.find((d) => d.key === draft.duration);
    if (draft.duration === "custom") return { startsAt: fromLocalInput(draft.startsAt), endsAt: fromLocalInput(draft.endsAt) };
    if (!preset?.hours) return { startsAt: null, endsAt: null };
    return { startsAt: new Date(openedAt).toISOString(), endsAt: new Date(openedAt + preset.hours * 3_600_000).toISOString() };
  }, [draft.duration, draft.startsAt, draft.endsAt, openedAt]);

  const problems: string[] = [];
  if (!fraction) problems.push(`倍率需为 0–${limit} 之间、最多 3 位小数的数`);
  if (!capValid) problems.push("每日上限无效");
  if (fraction && fraction.num === fraction.den && capValid && cap === 0) problems.push("×1 且不限量的规则没有作用");
  if (period.startsAt && period.endsAt && new Date(period.endsAt) <= new Date(period.startsAt)) problems.push("结束时间需要晚于开始时间");
  if (draft.reason.length > (catalog?.limits.maxReason ?? 200)) problems.push("原因过长");

  const payload: GrowthRulePayload | null = fraction
    ? {
        userId: draft.scope === "user" ? userId : 0,
        kind: draft.kind,
        numerator: fraction.num,
        denominator: fraction.den,
        rounding: draft.rounding,
        dailyCap: capValid ? cap : 0,
        sources: draft.sources,
        startsAt: period.startsAt,
        endsAt: period.endsAt,
        enabled: draft.enabled,
        label: draft.label.trim(),
        reason: draft.reason.trim()
      }
    : null;

  // 推演：表单变化 300ms 后重算（服务端用这个人当前生效的规则 + 草稿）
  const payloadKey = JSON.stringify(payload);
  React.useEffect(() => {
    if (!open || !payload || problems.length) return;
    const base = Number.parseInt(previewBase, 10);
    if (!Number.isSafeInteger(base) || base <= 0) return;
    const source = kindSources.some((s) => s.key === previewSource) ? previewSource : kindSources[0]?.key ?? "signin";
    const timer = setTimeout(() => {
      runPreview({
        userId: draft.scope === "user" ? userId : 0,
        kind: draft.kind,
        source,
        base,
        draft: payload,
        excludeRuleId: rule?.id
      })
        .then(setPreview)
        .catch(() => setPreview(null));
    }, 300);
    return () => clearTimeout(timer);
    // payloadKey 汇总了草稿的全部字段
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, payloadKey, previewBase, previewSource, kindSources, runPreview]);

  async function submit() {
    if (!payload || problems.length) return;
    try {
      await save.mutateAsync({ ruleId: rule?.id, payload });
      toast.success(editing ? "规则已更新" : draft.scope === "app" ? "全应用规则已生效" : "规则已生效", {
        description: `${draft.kind === "integral" ? "积分" : "经验"} ${factorLabel(payload.numerator, payload.denominator)}${payload.dailyCap ? `，每日上限 ${payload.dailyCap}` : ""}`
      });
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  const presets = catalog?.presets ?? [];
  const reasons = effect === "boost" ? BOOST_REASONS : PENALTY_REASONS;

  return (
    <>
      <DialogHeader>
        <DialogTitle>{editing ? "修改倍率规则" : "新建倍率规则"}</DialogTitle>
        <DialogDescription>
          同一类型下所有生效规则叠乘，每日上限取最严的一条。管理员调整、充值、会员赠送、折算与退回不受影响。
        </DialogDescription>
      </DialogHeader>

      <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_240px]">
        <div className="flex min-w-0 flex-col gap-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="作用对象">
              <Segmented
                value={draft.scope}
                onChange={(value) => set("scope", value)}
                stretch
                options={[
                  { value: "user", label: "仅此用户", disabled: editing },
                  { value: "app", label: "全应用", disabled: editing }
                ]}
              />
            </Field>
            <Field label="类型">
              <Segmented
                value={draft.kind}
                onChange={(value) => setDraft((prev) => ({ ...prev, kind: value, sources: [] }))}
                stretch
                options={[
                  { value: "integral", label: "积分", icon: Coins, disabled: editing },
                  { value: "experience", label: "经验", icon: Sparkles, disabled: editing }
                ]}
              />
            </Field>
          </div>

          <Field label="倍率" hint="÷ 表示获取变慢（处罚），× 表示加速（奖励）；0 表示冻结获取。">
            <div className="flex flex-wrap gap-1.5">
              {presets.map((preset) => {
                const active = fraction?.num === preset.numerator && fraction?.den === preset.denominator;
                return (
                  <button
                    key={preset.key}
                    type="button"
                    onClick={() => setDraft((prev) => ({ ...prev, ...fractionToInput(preset.numerator, preset.denominator) }))}
                    className={cn(
                      "h-7 rounded-md border px-2.5 text-xs font-medium tabular-nums transition-colors",
                      active ? "border-primary bg-primary/10 text-primary" : "hover:bg-accent"
                    )}
                  >
                    {preset.label}
                  </button>
                );
              })}
            </div>
            <div className="mt-2 flex items-center gap-2">
              <Segmented
                value={draft.mode}
                onChange={(value) => set("mode", value)}
                options={[
                  { value: "mul", label: "乘", icon: XIcon },
                  { value: "div", label: "除", icon: Divide }
                ]}
              />
              <Input
                value={draft.value}
                onChange={(event) => set("value", event.target.value)}
                inputMode="decimal"
                className="h-8 w-28 tabular-nums"
                aria-label="倍率数值"
              />
              {fraction ? (
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  = <span className="font-mono">{fraction.num}/{fraction.den}</span>
                  <EffectPill effect={effect} />
                </span>
              ) : (
                <span className="text-xs text-red-600 dark:text-red-400">无效</span>
              )}
            </div>
          </Field>

          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="取整方式">
              <Select value={draft.rounding} onValueChange={(value) => set("rounding", value as GrowthRounding)}>
                <SelectTrigger className="h-8 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(catalog?.roundings ?? []).map((item) => (
                    <SelectItem key={item.key} value={item.key}>
                      <div className="flex flex-col">
                        <span>{item.label}</span>
                        <span className="text-xs text-muted-foreground">{item.description}</span>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="每日上限" hint="当天受管来源实际入账的合计，0 为不限">
              <Input
                value={draft.dailyCap}
                onChange={(event) => set("dailyCap", event.target.value.replace(/[^\d]/g, ""))}
                inputMode="numeric"
                className="h-8 tabular-nums"
              />
              <div className="mt-1.5 flex flex-wrap gap-1">
                {CAP_QUICK.map((value) => (
                  <button
                    key={value}
                    type="button"
                    onClick={() => set("dailyCap", String(value))}
                    className={cn(
                      "h-6 rounded px-1.5 text-[11px] tabular-nums transition-colors",
                      cap === value ? "bg-primary/10 text-primary" : "text-muted-foreground hover:bg-accent"
                    )}
                  >
                    {value === 0 ? "不限" : value.toLocaleString("zh-CN")}
                  </button>
                ))}
              </div>
            </Field>
          </div>

          <Field label="来源范围" hint={draft.sources.length ? `仅作用于选中的 ${draft.sources.length} 个来源` : "未选择时作用于全部默认来源（带「默认」标记的）"}>
            <div className="flex flex-wrap gap-1.5">
              {kindSources.map((source) => {
                const active = draft.sources.includes(source.key);
                return (
                  <button
                    key={source.key}
                    type="button"
                    title={source.description}
                    onClick={() =>
                      set("sources", active ? draft.sources.filter((key) => key !== source.key) : [...draft.sources, source.key])
                    }
                    className={cn(
                      "inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs transition-colors",
                      active ? "border-primary bg-primary/10 text-primary" : "hover:bg-accent",
                      !active && !draft.sources.length && source.default && "border-dashed"
                    )}
                  >
                    {source.label}
                    {source.default ? <span className="text-[10px] text-muted-foreground">默认</span> : null}
                  </button>
                );
              })}
            </div>
          </Field>

          <Field label="有效期">
            <div className="flex flex-wrap gap-1.5">
              {DURATIONS.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => set("duration", item.key)}
                  className={cn(
                    "h-7 rounded-md border px-2.5 text-xs transition-colors",
                    draft.duration === item.key ? "border-primary bg-primary/10 text-primary" : "hover:bg-accent"
                  )}
                >
                  {item.label}
                </button>
              ))}
            </div>
            {draft.duration === "custom" ? (
              <div className="mt-2 grid gap-2 sm:grid-cols-2">
                <Input type="datetime-local" value={draft.startsAt} onChange={(event) => set("startsAt", event.target.value)} aria-label="开始时间" className="h-8" />
                <Input type="datetime-local" value={draft.endsAt} onChange={(event) => set("endsAt", event.target.value)} aria-label="结束时间" className="h-8" />
              </div>
            ) : null}
          </Field>

          <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
            <Field label="名称（可选）">
              <Input value={draft.label} maxLength={catalog?.limits.maxLabel ?? 32} onChange={(event) => set("label", event.target.value)} placeholder="例如：双倍经验周" className="h-8" />
            </Field>
            <label className="flex h-8 items-center gap-2 text-sm">
              <Switch checked={draft.enabled} onCheckedChange={(value) => set("enabled", value)} />
              立即启用
            </label>
          </div>

          <Field label="原因">
            <Textarea value={draft.reason} onChange={(event) => set("reason", event.target.value)} rows={2} placeholder="写给其他管理员看的说明，会进入调控记录" />
            <div className="mt-1.5 flex flex-wrap gap-1">
              {reasons.map((reason) => (
                <button key={reason} type="button" onClick={() => set("reason", reason)} className="h-6 rounded px-1.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent">
                  {reason}
                </button>
              ))}
            </div>
          </Field>
        </div>

        <aside className="flex flex-col gap-3 rounded-xl border bg-muted/30 p-3.5">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Calculator className="size-4 text-muted-foreground" />
            实时推演
          </div>
          <div className="grid grid-cols-[1fr_auto] gap-2">
            <Select value={kindSources.some((s) => s.key === previewSource) ? previewSource : kindSources[0]?.key} onValueChange={setPreviewSource}>
              <SelectTrigger className="h-8 w-full bg-background">
                <SelectValue placeholder="来源" />
              </SelectTrigger>
              <SelectContent>
                {kindSources.map((source) => (
                  <SelectItem key={source.key} value={source.key}>
                    {source.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input value={previewBase} onChange={(event) => setPreviewBase(event.target.value.replace(/[^\d]/g, ""))} className="h-8 w-20 bg-background tabular-nums" aria-label="推演数量" />
          </div>
          {preview ? (
            <div className="flex flex-col gap-2.5">
              <PreviewRow label="现在" value={preview.before.granted} base={preview.base} capped={preview.before.capped} />
              <div className="flex justify-center text-muted-foreground">
                <ArrowRight className="size-4 rotate-90" />
              </div>
              <PreviewRow label="加上这条" value={preview.after.granted} base={preview.base} capped={preview.after.capped} emphasize />
              {draft.rounding === "random" ? (
                <p className="text-xs text-muted-foreground">按概率进位，长期期望 {preview.expectedAfter.toFixed(2)}</p>
              ) : null}
              {preview.usedToday > 0 ? <p className="text-xs text-muted-foreground">今日已入账 {preview.usedToday.toLocaleString("zh-CN")}</p> : null}
            </div>
          ) : (
            <div className="flex flex-1 items-center justify-center py-6 text-xs text-muted-foreground">
              {previewMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : problems[0] ?? "填好倍率后显示"}
            </div>
          )}
          <div className="mt-auto rounded-lg bg-background/80 p-2.5 text-xs leading-5 text-muted-foreground">
            <span className={cn("font-medium", effect !== "neutral" && "text-foreground")}>{EFFECT_META[effect].label}</span>
            ：{draft.scope === "app" ? "全应用每个用户" : "这个用户"}的{draft.kind === "integral" ? "积分" : "经验"}
            {fraction ? (fraction.num === 0 ? "暂停获取" : `按 ${factorLabel(fraction.num, fraction.den)} 入账`) : "—"}
            {cap > 0 ? `，每天最多 ${cap.toLocaleString("zh-CN")}` : ""}。
          </div>
        </aside>
      </div>

      {problems.length ? (
        <div className="flex flex-wrap gap-1.5">
          {problems.map((problem) => (
            <Badge key={problem} variant="warning" size="sm">
              {problem}
            </Badge>
          ))}
        </div>
      ) : null}

      <DialogFooter>
        <Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>
          取消
        </Button>
        <Button onClick={submit} disabled={!payload || problems.length > 0 || save.isPending}>
          {save.isPending ? <Loader2 className="animate-spin" /> : null}
          {editing ? "保存修改" : "生效"}
        </Button>
      </DialogFooter>
    </>
  );
}


function Field({ label, hint, children }: { label: string; hint?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Label className="text-xs font-medium text-muted-foreground">{label}</Label>
      {children}
      {hint ? <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function PreviewRow({ label, value, base, capped, emphasize }: { label: string; value: number; base: number; capped?: boolean; emphasize?: boolean }) {
  const effect = value === 0 ? "freeze" : value < base ? "penalty" : value > base ? "boost" : "neutral";
  return (
    <div className={cn("flex items-center justify-between rounded-lg px-3 py-2", emphasize ? "bg-background shadow-sm" : "bg-background/60")}>
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="flex items-baseline gap-1.5">
        <span className="text-xs text-muted-foreground line-through decoration-muted-foreground/40">{value !== base ? base.toLocaleString("zh-CN") : null}</span>
        <span
          className={cn(
            "text-xl font-semibold tabular-nums",
            effect === "boost" && "text-emerald-600 dark:text-emerald-400",
            effect === "penalty" && "text-amber-600 dark:text-amber-400",
            effect === "freeze" && "text-red-600 dark:text-red-400"
          )}
        >
          {value.toLocaleString("zh-CN")}
        </span>
        {capped ? <span className="text-[10px] text-red-600 dark:text-red-400">封顶</span> : null}
      </span>
    </div>
  );
}
