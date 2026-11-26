"use client";

import { useState } from "react";
import { Eye, Gauge, HardDrive, Power, PowerOff, ScanSearch, ShieldCheck, ShieldHalf, type LucideIcon } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { NumberField, Panel, Segmented, SettingRow } from "@/components/traffic/traffic-kit";
import { cn } from "@/lib/utils";
import { PARANOIA_META, RuleSeverityBadge, THRESHOLD_PRESETS, formatBytes } from "../firewall-kit";
import { Hint, type SettingsForm, type SettingsSetter, type SettingsUpdater } from "./settings-draft";
import { ByteField } from "./waf-shared";

/* ── 引擎模式 + 总开关 ── */

type EngineMode = "off" | "observe" | "block";

export function engineModeOf(form: Pick<SettingsForm, "corazaEnabled" | "corazaDetectionOnly">): EngineMode {
  if (!form.corazaEnabled) return "off";
  return form.corazaDetectionOnly ? "observe" : "block";
}

const MODES: { value: EngineMode; label: string; hint: string; icon: LucideIcon; active: string; iconTone: string }[] = [
  {
    value: "off",
    label: "关闭",
    hint: "不检查请求载荷，只保留入口段（名单、封禁、签名、自定义规则、限流）",
    icon: PowerOff,
    active: "border-foreground/40 bg-muted",
    iconTone: "bg-muted text-muted-foreground"
  },
  {
    value: "observe",
    label: "观察",
    hint: "照常计分，越过阈值只记一条观察事件，不拦截",
    icon: Eye,
    active: "border-amber-500/60 bg-amber-500/8",
    iconTone: "bg-amber-500/12 text-amber-600 dark:text-amber-400"
  },
  {
    value: "block",
    label: "拦截",
    hint: "入站异常分达到阈值即拦截",
    icon: ShieldCheck,
    active: "border-emerald-500/60 bg-emerald-500/8",
    iconTone: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400"
  }
];

export function EnginePanel({ form, update, crsVersion }: { form: SettingsForm; update: SettingsUpdater; crsVersion?: string }) {
  const mode = engineModeOf(form);
  const choose = (next: EngineMode) =>
    update((current) => ({
      ...current,
      corazaEnabled: next !== "off",
      // 关闭时保留原来的观察 / 拦截选择，再打开能回到原状态
      corazaDetectionOnly: next === "off" ? current.corazaDetectionOnly : next === "observe"
    }));

  return (
    <Panel
      icon={ShieldHalf}
      tone={mode === "block" ? "emerald" : mode === "observe" ? "amber" : "muted"}
      title="WAF 引擎"
      aside={crsVersion ? <Badge variant="outline" size="sm">CRS {crsVersion}</Badge> : null}
    >
      <div role="radiogroup" aria-label="引擎模式" className="grid min-w-0 gap-2 @2xl:grid-cols-3">
        {MODES.map((item) => {
          const selected = item.value === mode;
          const Icon = item.icon;
          return (
            <button
              key={item.value}
              type="button"
              role="radio"
              aria-checked={selected}
              onClick={() => choose(item.value)}
              className={cn(
                "flex min-w-0 items-start gap-3 rounded-xl border border-border p-3 text-left transition-colors hover:bg-accent/60 disabled:pointer-events-none disabled:opacity-60",
                selected && item.active
              )}
            >
              <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg", selected ? item.iconTone : "bg-muted text-muted-foreground")}>
                <Icon className="size-4" />
              </span>
              <span className="min-w-0">
                <span className="flex items-center gap-2 text-sm font-semibold">
                  {item.label}
                  {selected ? <span className="size-1.5 rounded-full bg-current" aria-hidden /> : null}
                </span>
                <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">{item.hint}</span>
              </span>
            </button>
          );
        })}
      </div>
    </Panel>
  );
}

export function MasterSwitchPanel({ form, set }: { form: SettingsForm; set: SettingsSetter }) {
  const [confirming, setConfirming] = useState(false);
  return (
    <Panel
      icon={form.enabled ? Power : PowerOff}
      tone={form.enabled ? "primary" : "rose"}
      title="整个防火墙"
      aside={
        <Switch
          checked={form.enabled}
          onCheckedChange={(next) => (next ? set("enabled", true) : setConfirming(true))}
          aria-label="整个防火墙"
        />
      }
    >
      {form.enabled ? (
        <Hint>入口段、封禁、限流与 WAF 按下方设置工作。</Hint>
      ) : (
        <Hint tone="danger">已关闭：所有检查、限流与封禁都不生效，任何请求都直接放行。</Hint>
      )}
      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>关闭整个防火墙？</AlertDialogTitle>
            <AlertDialogDescription>保存后所有副本停止全部检查，已封禁的 IP 与地域也会放行。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => set("enabled", false)}>
              关闭
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Panel>
  );
}

/* ── 灵敏度：偏执等级 + 入站阈值 ── */

const LEVELS = [1, 2, 3, 4] as const;

function paranoiaParts(level: number) {
  const label = PARANOIA_META[level]?.label ?? `PL${level}`;
  const [code, ...rest] = label.split(" ");
  return { code, name: rest.join(" "), hint: PARANOIA_META[level]?.hint ?? "" };
}

export function SensitivityPanel({ form, update, dimmed }: { form: SettingsForm; update: SettingsUpdater; dimmed: boolean }) {
  const blocking = form.corazaParanoia;
  const detection = Math.max(form.corazaDetectionParanoia, blocking);

  const setBlocking = (level: number) =>
    update((current) => ({
      ...current,
      corazaParanoia: level,
      corazaDetectionParanoia: Math.max(current.corazaDetectionParanoia, level)
    }));

  return (
    <Panel icon={Gauge} tone="sky" title="灵敏度" className={cn(dimmed && "opacity-60")}>
      <div className="grid min-w-0 gap-6 @4xl:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-3">
          <div className="grid min-w-0 grid-cols-2 gap-2 @2xl:grid-cols-4">
            {LEVELS.map((level) => {
              const state = level <= blocking ? "block" : level <= detection ? "score" : "off";
              const parts = paranoiaParts(level);
              return (
                <div
                  key={level}
                  className={cn(
                    "flex min-w-0 flex-col gap-1 rounded-xl border p-2.5",
                    state === "block" && "border-sky-500/40 bg-sky-500/8",
                    state === "score" && "border-dashed border-amber-500/50 bg-amber-500/5",
                    state === "off" && "border-border opacity-70"
                  )}
                >
                  <div className="flex items-center justify-between gap-1">
                    <span className="font-mono text-xs font-semibold">{parts.code}</span>
                    <span
                      className={cn(
                        "rounded px-1 text-[10px] leading-4 font-medium",
                        state === "block" && "bg-sky-500/15 text-sky-700 dark:text-sky-300",
                        state === "score" && "bg-amber-500/15 text-amber-700 dark:text-amber-300",
                        state === "off" && "bg-muted text-muted-foreground"
                      )}
                    >
                      {state === "block" ? "参与拦截" : state === "score" ? "只记录" : "不检测"}
                    </span>
                  </div>
                  <span className="text-sm font-medium">{parts.name}</span>
                  <span className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{parts.hint}</span>
                </div>
              );
            })}
          </div>
          <div className="flex flex-col divide-y divide-border">
            <SettingRow title="拦截等级" value={paranoiaParts(blocking).code}>
              <Segmented
                value={String(blocking)}
                options={LEVELS.map((level) => ({ value: String(level), label: `PL${level}` }))}
                onChange={(value) => setBlocking(Number(value))}
                ariaLabel="拦截等级"
              />
            </SettingRow>
            <SettingRow title="检测等级" value={paranoiaParts(detection).code}>
              <Segmented
                value={String(detection)}
                options={LEVELS.filter((level) => level >= blocking).map((level) => ({ value: String(level), label: `PL${level}` }))}
                onChange={(value) => update((current) => ({ ...current, corazaDetectionParanoia: Number(value) }))}
                ariaLabel="检测等级"
              />
            </SettingRow>
          </div>
          <Hint>检测等级不低于拦截等级；高出的部分只计分记录，不参与拦截，可用来先观察更高等级的误报。</Hint>
        </div>

        <ThresholdEditor value={form.corazaAnomalyThreshold} onChange={(value) => update((current) => ({ ...current, corazaAnomalyThreshold: value }))} />
      </div>
    </Panel>
  );
}

const WEIGHTS = [
  { severity: "critical", score: 5 },
  { severity: "error", score: 4 },
  { severity: "warning", score: 3 },
  { severity: "notice", score: 2 }
] as const;

const EXAMPLES = [
  { label: "1 条警告", score: 3 },
  { label: "1 条严重", score: 5 },
  { label: "1 条错误 + 1 条提示", score: 6 },
  { label: "2 条严重", score: 10 },
  { label: "4 条严重", score: 20 }
] as const;

function ThresholdEditor({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  const threshold = Math.max(5, value || 5);
  const criticalHits = threshold / 5;
  const max = Math.max(25, Math.ceil((threshold * 1.3) / 5) * 5);
  const step = 5 * Math.max(1, Math.ceil(max / 5 / 5));
  const ticks: number[] = [];
  for (let tick = 0; tick <= max; tick += step) ticks.push(tick);
  const pct = (score: number) => `${Math.min(100, (score / max) * 100)}%`;

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex min-w-0 flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">入站阈值</div>
          <div className="flex items-baseline gap-1.5">
            <span className="text-3xl leading-9 font-semibold tracking-tight tabular-nums">{threshold}</span>
            <span className="text-sm text-muted-foreground">分</span>
            <span className="ml-1 text-sm text-muted-foreground">
              {Number.isInteger(criticalHits) ? `= ${criticalHits} 条严重命中` : `≈ ${criticalHits.toFixed(1)} 条严重命中`}
            </span>
          </div>
        </div>
        <NumberField value={value} min={5} max={10000} onChange={onChange} suffix="分" className="w-28" ariaLabel="入站阈值" invalid={value < 5} />
      </div>

      <div className="flex min-w-0 flex-wrap gap-1.5">
        {THRESHOLD_PRESETS.map((preset) => (
          <button
            key={preset.value}
            type="button"
            title={preset.hint}
            onClick={() => onChange(preset.value)}
            className={cn(
              "inline-flex h-7 items-center gap-1.5 rounded-full border border-border px-2.5 text-xs transition-colors hover:bg-accent disabled:pointer-events-none disabled:opacity-60",
              threshold === preset.value && "border-primary bg-primary/10 text-primary"
            )}
          >
            <span className="font-medium">{preset.label}</span>
            <span className="tabular-nums opacity-70">{preset.value}</span>
          </button>
        ))}
      </div>
      {THRESHOLD_PRESETS.find((preset) => preset.value === threshold) ? (
        <Hint>{THRESHOLD_PRESETS.find((preset) => preset.value === threshold)?.hint}</Hint>
      ) : null}

      {/* 计分尺：阈值左侧放行，右侧拦截 */}
      <div className="min-w-0 pt-1">
        <div className="relative h-3 w-full overflow-hidden rounded-full bg-muted">
          <div className="absolute inset-y-0 left-0 bg-emerald-500/25" style={{ width: pct(threshold) }} />
          <div className="absolute inset-y-0 right-0 bg-rose-500/30" style={{ left: pct(threshold) }} />
        </div>
        <div className="relative h-5">
          {ticks.map((tick) => (
            <span
              key={tick}
              className="absolute top-0.5 -translate-x-1/2 text-[10px] text-muted-foreground tabular-nums"
              style={{ left: pct(tick) }}
            >
              {tick}
            </span>
          ))}
          <span
            className="absolute -top-4 h-5 w-0.5 -translate-x-1/2 rounded-full bg-foreground"
            style={{ left: pct(threshold) }}
            aria-hidden
          />
        </div>
        <div className="flex justify-between text-[11px] text-muted-foreground">
          <span>低于阈值：计分记录</span>
          <span>达到阈值：拦截</span>
        </div>
      </div>

      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
        {WEIGHTS.map((item) => (
          <span key={item.severity} className="inline-flex items-center gap-1 text-xs text-muted-foreground">
            <RuleSeverityBadge severity={item.severity} />
            <span className="tabular-nums">{item.score} 分</span>
          </span>
        ))}
      </div>

      <div className="grid min-w-0 gap-1">
        {EXAMPLES.map((example) => {
          const blocked = example.score >= threshold;
          return (
            <div key={example.label} className="flex min-w-0 items-center gap-2 text-xs">
              <span className="min-w-0 flex-1 truncate text-muted-foreground">{example.label}</span>
              <span className="tabular-nums text-muted-foreground">{example.score} 分</span>
              <span
                className={cn(
                  "w-10 shrink-0 rounded px-1 text-center text-[11px] leading-5 font-medium",
                  blocked ? "bg-rose-500/12 text-rose-700 dark:text-rose-300" : "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300"
                )}
              >
                {blocked ? "拦截" : "放行"}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ── 出站检测与请求体 ── */

const MiB = 1024 * 1024;

export function InspectionPanels({ form, set, dimmed }: { form: SettingsForm; set: SettingsSetter; dimmed: boolean }) {
  const memoryTooLarge = form.requestBodyMemory > form.requestBodyLimit;
  return (
    <div className={cn("grid min-w-0 gap-4 @4xl:grid-cols-2", dimmed && "opacity-60")}>
      <Panel
        icon={ScanSearch}
        tone={form.corazaResponseInspection ? "violet" : "muted"}
        title="出站检测"
        bodyClassName="pb-2 sm:pb-3"
        aside={
          <Switch
            checked={form.corazaResponseInspection}
            onCheckedChange={(checked) => set("corazaResponseInspection", checked)}
            aria-label="出站检测"
          />
        }
      >
        <Hint>对加密之前的明文响应跑 95x 数据泄露规则，只记录，不拦截。</Hint>
        <div className={cn("flex flex-col divide-y divide-border", !form.corazaResponseInspection && "opacity-55")}>
          <SettingRow title="出站阈值" value={`${form.corazaOutboundThreshold} 分`}>
            <NumberField
              value={form.corazaOutboundThreshold}
              min={1}
              max={10000}
              suffix="分"
              onChange={(value) => set("corazaOutboundThreshold", value)}
              className="w-28"
              ariaLabel="出站阈值"
            />
          </SettingRow>
          <SettingRow title="响应体读取上限" value={formatBytes(form.corazaResponseBodyLimit)}>
            <ByteField
              value={form.corazaResponseBodyLimit}
              max={8 * MiB}
              onChange={(value) => set("corazaResponseBodyLimit", value)}
              ariaLabel="响应体读取上限"
            />
          </SettingRow>
        </div>
      </Panel>

      <Panel icon={HardDrive} tone="sky" title="请求体与参数" bodyClassName="pb-2 sm:pb-3">
        <div className="flex flex-col divide-y divide-border">
          <SettingRow title="请求体检查上限" value={formatBytes(form.requestBodyLimit)}>
            <ByteField value={form.requestBodyLimit} min={1024} onChange={(value) => set("requestBodyLimit", value)} ariaLabel="请求体检查上限" />
          </SettingRow>
          <SettingRow
            title="内存缓冲"
            value={formatBytes(form.requestBodyMemory)}
            error={memoryTooLarge ? "内存缓冲不能大于请求体检查上限" : undefined}
          >
            <ByteField value={form.requestBodyMemory} min={1024} onChange={(value) => set("requestBodyMemory", value)} ariaLabel="内存缓冲" />
          </SettingRow>
          <SettingRow title="参数个数上限" value={`${form.corazaArgumentsLimit} 个`}>
            <NumberField
              value={form.corazaArgumentsLimit}
              min={50}
              max={100000}
              step={50}
              onChange={(value) => set("corazaArgumentsLimit", value)}
              className="w-28"
              ariaLabel="参数个数上限"
              invalid={form.corazaArgumentsLimit < 50}
            />
          </SettingRow>
          <SettingRow title="检查管理端请求体">
            <Switch
              checked={form.corazaInspectAdminBody}
              onCheckedChange={(checked) => set("corazaInspectAdminBody", checked)}
              aria-label="检查管理端请求体"
            />
          </SettingRow>
          {form.corazaInspectAdminBody ? (
            <Hint tone="warn" className="py-2">
              /api/admin/ 的请求里满是规则表达式、远程函数代码与邮件模板，开启后误报会明显增多，保存前建议先在观察模式下运行。
            </Hint>
          ) : null}
        </div>
      </Panel>
    </div>
  );
}

/** WAF 页签的保存前校验 */
export function wafValidation(form: SettingsForm): string | null {
  if (form.corazaAnomalyThreshold < 5) return "入站阈值不能低于 5";
  if (form.corazaOutboundThreshold < 1) return "出站阈值至少为 1";
  if (form.corazaArgumentsLimit < 50) return "参数个数上限不能低于 50";
  if (form.requestBodyLimit < 1024) return "请求体检查上限过小";
  if (form.requestBodyMemory > form.requestBodyLimit) return "内存缓冲不能大于请求体检查上限";
  if (form.corazaResponseBodyLimit > 8 * MiB) return "响应体读取上限不能超过 8 MiB";
  return null;
}
