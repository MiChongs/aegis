"use client";

import { useState } from "react";
import { Cpu, Loader2, Power, Save, ShieldBan, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import type { TrafficSettings } from "@/lib/api/traffic";
import { useSaveTrafficSettingsMutation, useTrafficConfigQuery } from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { ChipInput, NumberField, Panel, Segmented, SettingRow, TabStack } from "./traffic-kit";
import { STATUS_OPTIONS, fmtDuration, fmtNum } from "./traffic-format";

function seed(settings: TrafficSettings): TrafficSettings {
  return {
    ...settings,
    exemptPaths: Array.isArray(settings.exemptPaths) ? settings.exemptPaths : [],
    overload: { ...settings.overload },
    response: { ...settings.response },
    cluster: { enabled: Boolean(settings.cluster?.enabled) }
  };
}

const onOff = (on: boolean) => (on ? "开启" : "关闭");

export function TrafficSettingsTab() {
  const configQuery = useTrafficConfigQuery();
  const save = useSaveTrafficSettingsMutation();
  // 草稿绑定修订号：别人先改了配置（修订号变化）时自动丢弃本地草稿，而不是覆盖回去
  const [draft, setDraft] = useState<{ revision: number; value: TrafficSettings } | null>(null);
  const config = configQuery.data;

  if (!config) {
    return (
      <TabStack>
        <div className="grid gap-4 @4xl:grid-cols-2">
          <Skeleton className="h-80 rounded-2xl" />
          <Skeleton className="h-80 rounded-2xl" />
        </div>
        <Skeleton className="h-64 rounded-2xl" />
      </TabStack>
    );
  }

  const server = seed(config.settings);
  const form = draft?.revision === config.revision ? draft.value : server;
  const dirty = JSON.stringify(form) !== JSON.stringify(server);
  const patch = (update: (current: TrafficSettings) => TrafficSettings) =>
    setDraft({ revision: config.revision, value: update(form) });

  const submit = () =>
    save.mutate(form, {
      onSuccess: () => {
        setDraft(null);
        toast.success("全局设置已保存");
      }
    });

  return (
    <TabStack className={cn(dirty && "pb-20")}>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
        <Panel icon={Power} tone="primary" title="总控" bodyClassName="pb-2 sm:pb-3">
          <div className="flex flex-col divide-y divide-border">
            <SettingRow title="总开关" value={onOff(form.enabled)}>
              <Switch checked={form.enabled} onCheckedChange={(enabled) => patch((s) => ({ ...s, enabled }))} aria-label="总开关" />
            </SettingRow>
            <SettingRow title="管理端免检" value={onOff(form.exemptAdmin)}>
              <Switch checked={form.exemptAdmin} onCheckedChange={(exemptAdmin) => patch((s) => ({ ...s, exemptAdmin }))} aria-label="管理端免检" />
            </SettingRow>
            <SettingRow title="集群协同" value={onOff(form.cluster.enabled)}>
              <Switch
                checked={form.cluster.enabled}
                onCheckedChange={(enabled) => patch((s) => ({ ...s, cluster: { enabled } }))}
                aria-label="集群协同"
              />
            </SettingRow>
            <SettingRow title="跟踪键上限" value={fmtNum(form.maxTrackedKeys)}>
              <NumberField
                value={form.maxTrackedKeys}
                min={10_000}
                max={50_000_000}
                step={10_000}
                onChange={(maxTrackedKeys) => patch((s) => ({ ...s, maxTrackedKeys }))}
                className="w-32"
                ariaLabel="跟踪键上限"
              />
            </SettingRow>
            <SettingRow title="免检路径" value={`${form.exemptPaths.length} 条`} stacked>
              <ChipInput
                value={form.exemptPaths}
                onChange={(exemptPaths) => patch((s) => ({ ...s, exemptPaths }))}
                placeholder="/api/public/*"
                ariaLabel="免检路径"
              />
            </SettingRow>
          </div>
        </Panel>

        <Panel
          icon={Cpu}
          tone={form.overload.enabled ? "emerald" : "muted"}
          title="过载保护"
          bodyClassName="pb-2 sm:pb-3"
          aside={
            <Switch
              checked={form.overload.enabled}
              onCheckedChange={(enabled) => patch((s) => ({ ...s, overload: { ...s.overload, enabled } }))}
              aria-label="过载保护"
            />
          }
        >
          <div className={cn("flex flex-col divide-y divide-border", !form.overload.enabled && "opacity-55")}>
            <SettingRow title="CPU 阈值" value={`${form.overload.cpuThreshold}%`} stacked>
              <CpuThresholdSlider
                value={form.overload.cpuThreshold}
                disabled={!form.overload.enabled}
                onChange={(cpuThreshold) => patch((s) => ({ ...s, overload: { ...s.overload, cpuThreshold } }))}
              />
            </SettingRow>
            <SettingRow title="在途上限" value={form.overload.maxInflight > 0 ? fmtNum(form.overload.maxInflight) : "不限"}>
              <NumberField
                value={form.overload.maxInflight}
                min={0}
                max={10_000_000}
                onChange={(maxInflight) => patch((s) => ({ ...s, overload: { ...s.overload, maxInflight } }))}
                className="w-32"
                ariaLabel="在途上限"
              />
            </SettingRow>
            <SettingRow title="冷却时间" value={fmtDuration(form.overload.cooldownMs)}>
              <NumberField
                value={form.overload.cooldownMs}
                min={100}
                max={60_000}
                step={100}
                suffix="毫秒"
                onChange={(cooldownMs) => patch((s) => ({ ...s, overload: { ...s.overload, cooldownMs } }))}
                className="w-36"
                ariaLabel="冷却时间"
              />
            </SettingRow>
          </div>
        </Panel>
      </div>

      <Panel icon={ShieldBan} tone="rose" title="默认拒绝响应">
        <div className="grid min-w-0 gap-5 @3xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
          <div className="flex flex-col divide-y divide-border">
            <SettingRow title="状态码" value={form.response.status}>
              <Segmented
                value={String(form.response.status)}
                options={STATUS_OPTIONS.map((status) => ({ value: String(status), label: String(status) }))}
                onChange={(value) => patch((s) => ({ ...s, response: { ...s.response, status: Number(value) } }))}
                ariaLabel="状态码"
              />
            </SettingRow>
            <SettingRow title="业务码">
              <NumberField
                value={form.response.code}
                min={1}
                onChange={(code) => patch((s) => ({ ...s, response: { ...s.response, code } }))}
                className="w-32"
                ariaLabel="业务码"
              />
            </SettingRow>
            <SettingRow title="重试间隔" value={`${form.response.retryAfterSeconds} 秒`}>
              <NumberField
                value={form.response.retryAfterSeconds}
                min={1}
                max={3600}
                suffix="秒"
                onChange={(retryAfterSeconds) => patch((s) => ({ ...s, response: { ...s.response, retryAfterSeconds } }))}
                className="w-28"
                ariaLabel="重试间隔"
              />
            </SettingRow>
            <SettingRow title="提示文案" stacked>
              <Input
                value={form.response.message}
                maxLength={200}
                onChange={(event) => patch((s) => ({ ...s, response: { ...s.response, message: event.target.value } }))}
                aria-label="提示文案"
              />
            </SettingRow>
          </div>
          <ResponsePreview settings={form} />
        </div>
      </Panel>

      {dirty ? (
        <div className="sticky bottom-4 z-20 flex min-w-0 flex-wrap items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3 sm:px-5">
          <span className="size-2 shrink-0 rounded-full bg-amber-500" aria-hidden />
          <span className="min-w-0 flex-1 text-sm">未保存</span>
          <Button size="sm" variant="ghost" onClick={() => setDraft(null)} disabled={save.isPending}>
            <Undo2 />
            放弃
          </Button>
          <Button size="sm" onClick={submit} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            保存
          </Button>
        </div>
      ) : null}
    </TabStack>
  );
}

function CpuThresholdSlider({ value, disabled, onChange }: { value: number; disabled: boolean; onChange: (value: number) => void }) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <span className="w-8 shrink-0 text-[11px] text-muted-foreground tabular-nums">20%</span>
      <Slider
        value={[value]}
        min={20}
        max={100}
        step={1}
        disabled={disabled}
        onValueChange={(next) => onChange(next[0] ?? value)}
        aria-label="CPU 阈值"
      />
      <span className="w-10 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">100%</span>
    </div>
  );
}

/** 被拒绝的请求实际收到的响应 */
export function ResponsePreview({ settings }: { settings: Pick<TrafficSettings, "response"> }) {
  const r = settings.response;
  return (
    <figure className="flex min-w-0 flex-col overflow-hidden rounded-xl border border-border bg-muted/40 font-mono text-xs">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <span className="rounded bg-rose-500/15 px-1.5 py-0.5 font-semibold text-rose-700 dark:text-rose-300">{r.status}</span>
        <span className="text-muted-foreground">{r.status === 429 ? "Too Many Requests" : r.status === 503 ? "Service Unavailable" : "Forbidden"}</span>
      </div>
      <div className="px-3 py-2 text-muted-foreground">
        Retry-After: <span className="text-foreground">{r.retryAfterSeconds}</span>
      </div>
      <pre className="min-w-0 border-t border-border px-3 py-2 break-all whitespace-pre-wrap">
        {JSON.stringify({ code: r.code, message: r.message }, null, 2)}
      </pre>
    </figure>
  );
}
