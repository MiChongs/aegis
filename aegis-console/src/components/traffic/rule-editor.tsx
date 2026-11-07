"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import {
  ArrowLeft,
  CircleCheck,
  CircleAlert,
  FileText,
  Filter,
  Gauge,
  Hourglass,
  Layers,
  Loader2,
  Plus,
  Save,
  ShieldBan,
  Trash2,
  TrendingUp,
  Workflow
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { SectionHeading } from "@/components/ui/section-heading";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAdminToken } from "@/lib/admin-hooks";
import type {
  ConcurrencySpec,
  HeaderMatch,
  HeaderOp,
  LimitKey,
  LimitSpec,
  MatchSpec,
  QueueSpec,
  ResponseSpec,
  TrafficClass,
  TrafficMode,
  TrafficRule,
  WarmupSpec
} from "@/lib/api/traffic";
import {
  trafficErrorMessage,
  useCreateTrafficRuleMutation,
  useTrafficRuleValidation,
  useUpdateTrafficRuleMutation,
  validateRuleNow
} from "@/lib/traffic-hooks";
import { AppKeyPicker } from "./app-key-picker";
import { RuleLiveStats } from "./rule-live-stats";
import { RulePipeline } from "./rule-pipeline";
import { RuleSimulator } from "./rule-simulator";
import { ChipInput, Field, FieldError, NumberField, Panel, Segmented, SettingRow } from "./traffic-kit";
import {
  CLASS_LABEL,
  CLASS_ORDER,
  HEADER_OP_LABEL,
  HEADER_OP_ORDER,
  KEY_LABEL,
  KEY_ORDER,
  METHODS,
  MODE_LABEL,
  MODE_ORDER,
  PERIODS,
  STATUS_OPTIONS,
  blankLimit,
  fmtDuration,
  fmtNum,
  limitPhrase,
  periodLabel,
  warmupCurve
} from "./traffic-format";

/**
 * 规则编辑（新建与修改共用）。
 *
 * 左栏是表单，右栏吸顶：草稿的实时流水线、模拟请求，以及已发布规则的运行态。
 * 草稿每次停止变化 400ms 交给后端校验，字段级错误就地标出；保存前再同步校验一次。
 */

const DEFAULT_RESPONSE: ResponseSpec = { status: 429, code: 42910, message: "服务繁忙，请稍后再试", retryAfterSeconds: 3 };
const DEFAULT_CONCURRENCY: ConcurrencySpec = { max: 100, adaptive: false, minLimit: 10, maxLimit: 400, latencyToleranceX10: 20 };
const DEFAULT_QUEUE: QueueSpec = { size: 100, timeoutMs: 2000 };
const DEFAULT_WARMUP: WarmupSpec = { baselineRps: 300, maxRps: 3000, stepPct: 20, intervalMs: 1000, cooldownSeconds: 30 };

export function RuleEditor({
  initial,
  ruleId,
  defaultResponse
}: {
  initial: TrafficRule;
  /** 修改已有规则时传入；为空即新建 */
  ruleId?: string;
  defaultResponse?: ResponseSpec | null;
}) {
  const router = useRouter();
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const create = useCreateTrafficRuleMutation();
  const update = useUpdateTrafficRuleMutation();
  const [draft, setDraft] = useState<TrafficRule | null>(null);
  const [checking, setChecking] = useState(false);
  const rule = draft ?? initial;
  const isNew = !ruleId;
  const set = (patch: Partial<TrafficRule>) => setDraft({ ...rule, ...patch });
  const setMatch = (patch: Partial<MatchSpec>) => setDraft({ ...rule, match: { ...rule.match, ...patch } });

  const validation = useTrafficRuleValidation(rule);
  const errors = useMemo(() => validation.data?.errors ?? [], [validation.data]);
  const errorOf = (field: string) => errors.find((item) => item.field === field)?.message;
  const indexErrors = (prefix: string) => {
    const indexes = new Set<number>();
    let message: string | undefined;
    for (const item of errors) {
      const matched = item.field.startsWith(`${prefix}[`) ? /^\w[\w.]*\[(\d+)\]/.exec(item.field) : null;
      if (matched) {
        indexes.add(Number(matched[1]));
        message ??= item.message;
      }
    }
    return { indexes, message };
  };

  const saving = create.isPending || update.isPending || checking;
  const backHref = "/traffic?tab=rules";

  const save = async () => {
    if (!token) return;
    setChecking(true);
    try {
      const result = await validateRuleNow(queryClient, token, rule);
      if (result.errors.length) {
        toast.error(result.errors[0]!.message);
        return;
      }
    } catch (error) {
      toast.error(trafficErrorMessage(error, "校验失败"));
      return;
    } finally {
      setChecking(false);
    }
    const done = () => {
      toast.success(isNew ? "规则已创建" : "规则已保存");
      router.push(backHref);
    };
    if (isNew) create.mutate({ rule, position: "bottom" }, { onSuccess: done });
    else update.mutate({ ruleId: ruleId!, rule }, { onSuccess: done });
  };

  const paths = indexErrors("match.paths");
  const excludePaths = indexErrors("match.excludePaths");
  const cidrs = indexErrors("match.cidrs");
  const excludeCidrs = indexErrors("match.excludeCidrs");
  const methods = indexErrors("match.methods");

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="流量管理"
        title={isNew ? "新建规则" : rule.name || "未命名规则"}
        action={
          <>
            {validation.isFetching ? (
              <Badge variant="outline" size="sm">
                <Loader2 className="animate-spin" />
                校验中
              </Badge>
            ) : errors.length ? (
              <Badge variant="danger" size="sm">
                <CircleAlert />
                {errors.length} 处问题
              </Badge>
            ) : validation.data ? (
              <Badge variant="success" size="sm">
                <CircleCheck />
                校验通过
              </Badge>
            ) : null}
            <Button size="sm" variant="ghost" asChild>
              <Link href={backHref}>
                <ArrowLeft />
                返回
              </Link>
            </Button>
            <Button size="sm" onClick={save} disabled={saving}>
              {saving ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          </>
        }
      />

      <div className="@container min-w-0">
        <div className="grid min-w-0 gap-4 @5xl:grid-cols-[minmax(0,1fr)_minmax(0,380px)]">
          <div className="flex min-w-0 flex-col gap-4">
            {/* 基本信息 */}
            <Panel icon={FileText} tone="primary" title="基本信息">
              <div className="flex flex-col gap-4">
                <Field label="名称" error={errorOf("name")}>
                  <Input value={rule.name} maxLength={64} onChange={(event) => set({ name: event.target.value })} aria-invalid={Boolean(errorOf("name")) || undefined} />
                </Field>
                <Field label="说明" error={errorOf("description")}>
                  <Textarea value={rule.description} maxLength={500} rows={2} onChange={(event) => set({ description: event.target.value })} className="min-h-16" />
                </Field>
                <div className="flex flex-col divide-y divide-border border-t border-border">
                  <SettingRow title="模式">
                    <Segmented<TrafficMode>
                      value={rule.mode}
                      options={MODE_ORDER.map((mode) => ({ value: mode, label: MODE_LABEL[mode] }))}
                      onChange={(mode) => set({ mode })}
                      ariaLabel="模式"
                    />
                  </SettingRow>
                  <SettingRow title="优先级">
                    <Segmented<TrafficClass>
                      value={rule.class}
                      options={CLASS_ORDER.map((value) => ({ value, label: CLASS_LABEL[value] }))}
                      onChange={(value) => set({ class: value })}
                      ariaLabel="优先级"
                    />
                  </SettingRow>
                  <SettingRow title="命中即止" value={rule.terminal ? "是" : "否"}>
                    <Switch checked={rule.terminal} onCheckedChange={(terminal) => set({ terminal })} aria-label="命中即止" />
                  </SettingRow>
                  <SettingRow title="启用" value={rule.enabled ? "开启" : "关闭"}>
                    <Switch checked={rule.enabled} onCheckedChange={(enabled) => set({ enabled })} aria-label="启用" />
                  </SettingRow>
                </div>
              </div>
            </Panel>

            {/* 匹配条件 */}
            <Panel icon={Filter} tone="sky" title="匹配条件">
              <div className="flex flex-col gap-4">
                <div className="flex min-w-0 flex-col gap-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-muted-foreground">方法</span>
                    <span className="text-xs text-muted-foreground">{rule.match.methods.length ? `${rule.match.methods.length} 种` : "全部"}</span>
                  </div>
                  <ToggleGroup
                    type="multiple"
                    variant="outline"
                    size="sm"
                    value={rule.match.methods}
                    onValueChange={(next) => setMatch({ methods: METHODS.filter((method) => next.includes(method)) })}
                    className="max-w-full flex-wrap"
                    aria-label="方法"
                  >
                    {METHODS.map((method) => (
                      <ToggleGroupItem key={method} value={method} className="font-mono text-[11px]">
                        {method}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                  <FieldError message={methods.message} />
                </div>
                <div className="grid min-w-0 gap-4 @2xl:grid-cols-2">
                  <Field label="路径" error={paths.message}>
                    <ChipInput value={rule.match.paths} onChange={(next) => setMatch({ paths: next })} invalid={paths.indexes} placeholder="/api/auth/*" ariaLabel="路径" />
                  </Field>
                  <Field label="排除路径" error={excludePaths.message}>
                    <ChipInput
                      value={rule.match.excludePaths}
                      onChange={(next) => setMatch({ excludePaths: next })}
                      invalid={excludePaths.indexes}
                      placeholder="/api/auth/refresh"
                      ariaLabel="排除路径"
                    />
                  </Field>
                  <Field label="主机">
                    <ChipInput
                      value={rule.match.hosts}
                      onChange={(next) => setMatch({ hosts: next })}
                      normalize={(raw) => raw.toLowerCase()}
                      placeholder="*.example.com"
                      ariaLabel="主机"
                    />
                  </Field>
                  <div className="flex min-w-0 flex-col gap-1.5">
                    <span className="text-xs text-muted-foreground">应用</span>
                    <AppKeyPicker value={rule.match.appKeys} onChange={(next) => setMatch({ appKeys: next })} />
                  </div>
                  <Field label="网段" error={cidrs.message}>
                    <ChipInput value={rule.match.cidrs} onChange={(next) => setMatch({ cidrs: next })} invalid={cidrs.indexes} placeholder="10.0.0.0/8" ariaLabel="网段" />
                  </Field>
                  <Field label="排除网段" error={excludeCidrs.message}>
                    <ChipInput
                      value={rule.match.excludeCidrs}
                      onChange={(next) => setMatch({ excludeCidrs: next })}
                      invalid={excludeCidrs.indexes}
                      placeholder="127.0.0.1"
                      ariaLabel="排除网段"
                    />
                  </Field>
                </div>
                <HeaderConditions value={rule.match.headers} onChange={(headers) => setMatch({ headers })} />
              </div>
            </Panel>

            {/* 限速 */}
            <Panel
              icon={Gauge}
              tone="primary"
              title="限速"
              aside={
                <Button size="sm" variant="outline" disabled={rule.limits.length >= 8} onClick={() => set({ limits: [...rule.limits, blankLimit()] })}>
                  <Plus />
                  添加
                </Button>
              }
            >
              <div className="flex flex-col gap-3">
                <FieldError message={errorOf("limits")} />
                {rule.limits.length === 0 ? (
                  <div className="grid min-h-16 place-items-center rounded-xl border border-dashed border-border text-xs text-muted-foreground">暂无限速</div>
                ) : (
                  rule.limits.map((limit, index) => (
                    <LimitRow
                      key={index}
                      index={index}
                      limit={limit}
                      error={errorOf(`limits[${index}].name`)}
                      onChange={(next) => set({ limits: rule.limits.map((item, i) => (i === index ? next : item)) })}
                      onRemove={() => set({ limits: rule.limits.filter((_, i) => i !== index) })}
                    />
                  ))
                )}
              </div>
            </Panel>

            {/* 并发 */}
            <Panel
              icon={Layers}
              tone={rule.concurrency ? "violet" : "muted"}
              title="并发"
              aside={
                <Switch
                  checked={Boolean(rule.concurrency)}
                  onCheckedChange={(on) => set({ concurrency: on ? { ...DEFAULT_CONCURRENCY } : null })}
                  aria-label="并发"
                />
              }
            >
              {rule.concurrency ? <ConcurrencyFields spec={rule.concurrency} onChange={(concurrency) => set({ concurrency })} /> : null}
            </Panel>

            {/* 排队 */}
            <Panel
              icon={Hourglass}
              tone={rule.queue ? "amber" : "muted"}
              title="排队"
              aside={<Switch checked={Boolean(rule.queue)} onCheckedChange={(on) => set({ queue: on ? { ...DEFAULT_QUEUE } : null })} aria-label="排队" />}
            >
              {rule.queue ? (
                <div className="grid min-w-0 gap-4 @xl:grid-cols-2">
                  <Field label="容量">
                    <NumberField value={rule.queue.size} min={1} max={1_000_000} suffix="个" onChange={(size) => set({ queue: { ...rule.queue!, size } })} ariaLabel="容量" />
                  </Field>
                  <Field label="最长等待">
                    <NumberField
                      value={rule.queue.timeoutMs}
                      min={10}
                      max={60_000}
                      step={100}
                      suffix="毫秒"
                      onChange={(timeoutMs) => set({ queue: { ...rule.queue!, timeoutMs } })}
                      ariaLabel="最长等待"
                    />
                  </Field>
                </div>
              ) : null}
            </Panel>

            {/* 预热爬坡 */}
            <Panel
              icon={TrendingUp}
              tone={rule.warmup ? "emerald" : "muted"}
              title="预热爬坡"
              aside={<Switch checked={Boolean(rule.warmup)} onCheckedChange={(on) => set({ warmup: on ? { ...DEFAULT_WARMUP } : null })} aria-label="预热爬坡" />}
            >
              {rule.warmup ? <WarmupFields spec={rule.warmup} onChange={(warmup) => set({ warmup })} /> : null}
            </Panel>

            {/* 拒绝响应 */}
            <Panel
              icon={ShieldBan}
              tone={rule.response ? "rose" : "muted"}
              title="拒绝响应"
              aside={
                <>
                  {!rule.response ? (
                    <span className="text-xs text-muted-foreground tabular-nums">默认 {(defaultResponse ?? DEFAULT_RESPONSE).status}</span>
                  ) : null}
                  <Switch
                    checked={Boolean(rule.response)}
                    onCheckedChange={(on) => set({ response: on ? { ...(defaultResponse ?? DEFAULT_RESPONSE) } : null })}
                    aria-label="覆盖拒绝响应"
                  />
                </>
              }
            >
              {rule.response ? <ResponseFields spec={rule.response} onChange={(response) => set({ response })} /> : null}
            </Panel>
          </div>

          <aside className="flex min-w-0 flex-col gap-4 @5xl:sticky @5xl:top-4 @5xl:max-h-[calc(100dvh-2rem)] @5xl:self-start @5xl:overflow-y-auto">
            <Panel icon={Workflow} tone="sky" title="预览">
              <RulePipeline rule={rule} defaultResponse={defaultResponse} orientation="vertical" />
            </Panel>
            <RuleSimulator rule={rule} />
            {ruleId ? <RuleLiveStats ruleId={ruleId} /> : null}
          </aside>
        </div>
      </div>
    </div>
  );
}

/* ── 请求头条件 ── */

function HeaderConditions({ value, onChange }: { value: HeaderMatch[]; onChange: (next: HeaderMatch[]) => void }) {
  const patch = (index: number, next: Partial<HeaderMatch>) => onChange(value.map((item, i) => (i === index ? { ...item, ...next } : item)));
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">请求头</span>
        <Button size="xs" variant="ghost" onClick={() => onChange([...value, { name: "", op: "equals", value: "" }])}>
          <Plus />
          添加条件
        </Button>
      </div>
      {value.map((header, index) => {
        const valueless = header.op === "exists" || header.op === "absent";
        return (
          <div key={index} className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-2 @xl:grid-cols-[minmax(0,1fr)_112px_minmax(0,1fr)_auto]">
            <Input value={header.name} placeholder="X-Client" aria-label="请求头名" onChange={(event) => patch(index, { name: event.target.value })} className="h-8 font-mono text-xs" />
            <Select value={header.op} onValueChange={(op) => patch(index, { op: op as HeaderOp })}>
              <SelectTrigger size="sm" className="w-28 text-xs @xl:w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {HEADER_OP_ORDER.map((op) => (
                  <SelectItem key={op} value={op} className="text-xs">
                    {HEADER_OP_LABEL[op]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={valueless ? "" : header.value}
              disabled={valueless}
              aria-label="请求头值"
              onChange={(event) => patch(index, { value: event.target.value })}
              className="h-8 font-mono text-xs"
            />
            <Button size="icon-sm" variant="ghost" aria-label="移除条件" onClick={() => onChange(value.filter((_, i) => i !== index))}>
              <Trash2 />
            </Button>
          </div>
        );
      })}
    </div>
  );
}

/* ── 限速行 ── */

const NAMED_KEYS: LimitKey[] = ["header", "query"];
const FALLBACK_KEYS: LimitKey[] = ["header", "query", "token", "app"];

function LimitRow({
  index,
  limit,
  error,
  onChange,
  onRemove
}: {
  index: number;
  limit: LimitSpec;
  error?: string;
  onChange: (next: LimitSpec) => void;
  onRemove: () => void;
}) {
  const patch = (next: Partial<LimitSpec>) => onChange({ ...limit, ...next });
  const preset = PERIODS.some((period) => period.ms === limit.periodMs);
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-xl border border-border bg-background/60 p-3">
      <div className="flex min-w-0 items-center gap-2">
        <span className="grid size-6 shrink-0 place-items-center rounded-md bg-muted font-mono text-[11px] tabular-nums">{index + 1}</span>
        <span className="min-w-0 flex-1 truncate font-mono text-sm font-medium">{limitPhrase(limit)}</span>
        {limit.scope === "cluster" ? (
          <Badge variant="outline" size="sm">
            集群
          </Badge>
        ) : null}
        <Button size="icon-sm" variant="ghost" aria-label="移除限速" onClick={onRemove}>
          <Trash2 />
        </Button>
      </div>
      <div className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4">
        <Field label="按">
          <Select value={limit.key} onValueChange={(key) => patch({ key: key as LimitKey })}>
            <SelectTrigger size="sm" className="w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {KEY_ORDER.map((key) => (
                <SelectItem key={key} value={key} className="text-xs">
                  {KEY_LABEL[key]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        {NAMED_KEYS.includes(limit.key) ? (
          <Field label={limit.key === "header" ? "请求头名" : "参数名"} error={error}>
            <Input
              value={limit.name}
              aria-invalid={Boolean(error) || undefined}
              onChange={(event) => patch({ name: event.target.value })}
              placeholder={limit.key === "header" ? "X-Device-Id" : "uid"}
              className="h-8 font-mono text-xs"
            />
          </Field>
        ) : null}
        <Field label="次数">
          <NumberField value={limit.rate} min={1} onChange={(rate) => patch({ rate })} ariaLabel="次数" />
        </Field>
        <Field label="每">
          <Select value={String(limit.periodMs)} onValueChange={(value) => patch({ periodMs: Number(value) })}>
            <SelectTrigger size="sm" className="w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {!preset ? (
                <SelectItem value={String(limit.periodMs)} className="text-xs">
                  {periodLabel(limit.periodMs)}
                </SelectItem>
              ) : null}
              {PERIODS.map((period) => (
                <SelectItem key={period.ms} value={String(period.ms)} className="text-xs">
                  {period.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="突发">
          <NumberField value={limit.burst} min={1} onChange={(burst) => patch({ burst })} ariaLabel="突发" />
        </Field>
        <Field label="作用域">
          <Segmented
            value={limit.scope}
            options={[
              { value: "local", label: "单实例" },
              { value: "cluster", label: "集群" }
            ]}
            onChange={(scope) => patch({ scope })}
            ariaLabel="作用域"
          />
        </Field>
        {FALLBACK_KEYS.includes(limit.key) ? (
          <Field label="缺键时">
            <Select value={limit.fallback} onValueChange={(fallback) => patch({ fallback: fallback as LimitSpec["fallback"] })}>
              <SelectTrigger size="sm" className="w-full text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="ip" className="text-xs">
                  改按 IP
                </SelectItem>
                <SelectItem value="skip" className="text-xs">
                  不限制
                </SelectItem>
              </SelectContent>
            </Select>
          </Field>
        ) : null}
      </div>
    </div>
  );
}

/* ── 并发 ── */

function ConcurrencyFields({ spec, onChange }: { spec: ConcurrencySpec; onChange: (next: ConcurrencySpec) => void }) {
  const patch = (next: Partial<ConcurrencySpec>) => onChange({ ...spec, ...next });
  return (
    <div className="flex flex-col gap-4">
      <div className="grid min-w-0 gap-4 @xl:grid-cols-2">
        <Field label={spec.adaptive ? "初始上限" : "上限"}>
          <NumberField value={spec.max} min={1} onChange={(max) => patch({ max })} ariaLabel="并发上限" />
        </Field>
        <div className="flex min-w-0 items-end">
          <SettingRow title="自适应" value={spec.adaptive ? "开启" : "关闭"} className="w-full py-0">
            <Switch checked={spec.adaptive} onCheckedChange={(adaptive) => patch({ adaptive })} aria-label="自适应" />
          </SettingRow>
        </div>
      </div>
      {spec.adaptive ? (
        <div className="grid min-w-0 gap-4 @xl:grid-cols-3">
          <Field label="下限">
            <NumberField value={spec.minLimit} min={1} onChange={(minLimit) => patch({ minLimit })} ariaLabel="下限" />
          </Field>
          <Field label="上限">
            <NumberField value={spec.maxLimit} min={1} onChange={(maxLimit) => patch({ maxLimit })} ariaLabel="上限" />
          </Field>
          <div className="flex min-w-0 flex-col gap-1.5">
            <div className="flex items-center justify-between text-xs">
              <span className="text-muted-foreground">延迟容忍</span>
              <span className="tabular-nums">{(spec.latencyToleranceX10 / 10).toFixed(1)} 倍</span>
            </div>
            <Slider
              value={[spec.latencyToleranceX10]}
              min={11}
              max={100}
              step={1}
              onValueChange={(next) => patch({ latencyToleranceX10: next[0] ?? spec.latencyToleranceX10 })}
              aria-label="延迟容忍"
              className="mt-2"
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

/* ── 预热 ── */

const curveConfig = {
  ceiling: { label: "上限", theme: { light: "#10b981", dark: "#34d399" } }
} satisfies ChartConfig;

function WarmupFields({ spec, onChange }: { spec: WarmupSpec; onChange: (next: WarmupSpec) => void }) {
  const patch = (next: Partial<WarmupSpec>) => onChange({ ...spec, ...next });
  const curve = useMemo(() => warmupCurve(spec), [spec]);
  return (
    <div className="flex flex-col gap-4">
      <div className="grid min-w-0 grid-cols-2 gap-4 @2xl:grid-cols-5">
        <Field label="基线">
          <NumberField value={spec.baselineRps} min={1} suffix="/秒" onChange={(baselineRps) => patch({ baselineRps })} ariaLabel="基线" />
        </Field>
        <Field label="最高">
          <NumberField value={spec.maxRps} min={1} suffix="/秒" onChange={(maxRps) => patch({ maxRps })} ariaLabel="最高" />
        </Field>
        <Field label="每步升">
          <NumberField value={spec.stepPct} min={1} max={1000} suffix="%" onChange={(stepPct) => patch({ stepPct })} ariaLabel="每步升" />
        </Field>
        <Field label="步长">
          <NumberField value={spec.intervalMs} min={100} max={60_000} step={100} suffix="毫秒" onChange={(intervalMs) => patch({ intervalMs })} ariaLabel="步长" />
        </Field>
        <Field label="回落等待" className="col-span-2 @2xl:col-span-1">
          <NumberField value={spec.cooldownSeconds} min={1} max={3600} suffix="秒" onChange={(cooldownSeconds) => patch({ cooldownSeconds })} ariaLabel="回落等待" />
        </Field>
      </div>
      <div className="flex min-w-0 flex-col gap-2 rounded-xl border border-border bg-background/60 p-3">
        <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
          <span className="text-xs font-medium">爬坡曲线</span>
          <span className="text-xs text-muted-foreground tabular-nums">
            {fmtNum(Math.max(1, spec.baselineRps))} 至 {fmtNum(Math.max(spec.baselineRps, spec.maxRps))}/秒
          </span>
          <span className="ml-auto text-xs tabular-nums">
            {curve.reached ? `${fmtDuration(curve.seconds * 1000)} 到顶` : `${fmtDuration(curve.seconds * 1000)} 后仍未到顶`}
          </span>
        </div>
        <ChartContainer config={curveConfig} className="aspect-auto h-[150px] w-full">
          <LineChart data={curve.points} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="t" tickLine={false} axisLine={false} tickMargin={6} minTickGap={32} tickFormatter={(value: number) => `${value}s`} />
            <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(value: number) => fmtNum(value)} />
            <ChartTooltip content={<ChartTooltipContent indicator="line" labelFormatter={(_, payload) => `${payload?.[0]?.payload?.t ?? 0} 秒`} />} />
            <Line dataKey="ceiling" type="stepAfter" stroke="var(--color-ceiling)" strokeWidth={2} dot={false} isAnimationActive={false} />
          </LineChart>
        </ChartContainer>
      </div>
    </div>
  );
}

/* ── 拒绝响应 ── */

function ResponseFields({ spec, onChange }: { spec: ResponseSpec; onChange: (next: ResponseSpec) => void }) {
  const patch = (next: Partial<ResponseSpec>) => onChange({ ...spec, ...next });
  return (
    <div className="flex flex-col gap-4">
      <div className="grid min-w-0 grid-cols-2 gap-4 @2xl:grid-cols-3">
        <Field label="状态码" className="col-span-2 @2xl:col-span-1">
          <Segmented
            value={String(spec.status)}
            options={STATUS_OPTIONS.map((status) => ({ value: String(status), label: String(status) }))}
            onChange={(value) => patch({ status: Number(value) })}
            ariaLabel="状态码"
          />
        </Field>
        <Field label="业务码">
          <NumberField value={spec.code} min={1} onChange={(code) => patch({ code })} ariaLabel="业务码" />
        </Field>
        <Field label="重试间隔">
          <NumberField value={spec.retryAfterSeconds} min={1} max={3600} suffix="秒" onChange={(retryAfterSeconds) => patch({ retryAfterSeconds })} ariaLabel="重试间隔" />
        </Field>
      </div>
      <Field label="提示文案">
        <Input value={spec.message} maxLength={200} onChange={(event) => patch({ message: event.target.value })} />
      </Field>
    </div>
  );
}
