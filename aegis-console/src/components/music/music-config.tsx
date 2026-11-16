"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { CalendarClock, Gauge, Layers, Loader2, RotateCcw, Save, Scale, Settings2, Shuffle, Sparkles, ToggleRight, Waypoints, type LucideIcon } from "lucide-react";
import { Panel, SettingRow, TabStack } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type { MusicConfigInput } from "@/lib/api/music";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useMusicConfigQuery, useUpdateMusicConfigMutation } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import {
  CONFIG_GROUPS,
  CRON_PRESETS,
  cronLabel,
  getPath,
  setPath,
  toConfigInput,
  validateConfig,
  type FieldSpec,
  type NumberFieldSpec
} from "./music-config-fields";
import { MusicErrorPanel, PanelSkeleton } from "./music-kit";

const GROUP_ICON: Record<string, LucideIcon> = {
  switch: ToggleRight,
  window: CalendarClock,
  weights: Scale,
  als: Sparkles,
  itemcf: Waypoints,
  blend: Layers,
  diversity: Shuffle,
  publish: Gauge
};

/**
 * 应用级音乐推荐配置（GET / PUT /config）。
 *
 * 草稿按 appKey 绑定（见 aegis-console/CLAUDE.md「表单状态」）：没有草稿就从服务端派生，切换应用不会串改动。
 * 前端按契约的取值范围先校验，有越界项时保存不可用；没有写权限时整张表只读。
 */
export function MusicConfigPanel({ appKey, canManage }: { appKey: string; canManage: boolean }) {
  const query = useMusicConfigQuery(appKey);
  const mutation = useUpdateMusicConfigMutation(appKey);
  const [draft, setDraft] = useState<{ scope: string; value: MusicConfigInput } | null>(null);

  const seed = useMemo(() => (query.data ? toConfigInput(query.data) : null), [query.data]);
  const form = draft?.scope === appKey ? draft.value : seed;
  const errors = useMemo(() => (form ? validateConfig(form) : {}), [form]);
  const errorCount = Object.keys(errors).length;
  const dirty = Boolean(form && seed && JSON.stringify(form) !== JSON.stringify(seed));

  if (!query.data || !form) {
    if (query.isError) return <MusicErrorPanel icon={Settings2} title="配置" error={query.error} onRetry={() => query.refetch()} />;
    return (
      <TabStack>
        <PanelSkeleton height={60} />
        <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
          <PanelSkeleton height={200} />
          <PanelSkeleton height={200} />
        </div>
      </TabStack>
    );
  }

  const patch = (path: string, value: unknown) => setDraft({ scope: appKey, value: setPath(form, path, value) });

  const save = async () => {
    try {
      await mutation.mutateAsync(form);
      setDraft(null);
      toast.success("配置已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  return (
    <TabStack>
      <section className="flex min-w-0 flex-wrap items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3 sm:px-5">
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="text-sm font-medium">{dirty ? "有未保存的修改" : "已是最新配置"}</span>
          <span className="truncate text-[11px] text-muted-foreground">
            {query.data.updatedAt ? `${formatDateTimeMinute(query.data.updatedAt)} 由 ${query.data.updatedBy || "系统"} 更新` : "尚未修改过，使用默认配置"}
          </span>
        </div>
        {errorCount ? <span className="text-xs text-rose-600 dark:text-rose-400">{errorCount} 项超出范围</span> : null}
        {canManage ? (
          <div className="flex shrink-0 gap-2">
            <Button size="sm" variant="outline" disabled={!dirty || mutation.isPending} onClick={() => setDraft(null)}>
              <RotateCcw />
              还原
            </Button>
            <Button size="sm" disabled={!dirty || errorCount > 0 || mutation.isPending} onClick={save}>
              {mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          </div>
        ) : (
          <span className="text-xs text-muted-foreground">只读</span>
        )}
      </section>

      <div className="grid min-w-0 items-start gap-4 @4xl:grid-cols-2">
        {CONFIG_GROUPS.map((group) => (
          <Panel key={group.key} icon={GROUP_ICON[group.key]} tone="muted" title={group.title} bodyClassName="pb-2 sm:pb-3">
            <div className="grid min-w-0 divide-y divide-border/60">
              {group.fields.map((field) => (
                <ConfigField
                  key={field.path}
                  field={field}
                  value={getPath(form, field.path)}
                  error={errors[field.path]}
                  disabled={!canManage}
                  onChange={(v) => patch(field.path, v)}
                />
              ))}
            </div>
          </Panel>
        ))}
      </div>
    </TabStack>
  );
}

function ConfigField({
  field,
  value,
  error,
  disabled,
  onChange
}: {
  field: FieldSpec;
  value: unknown;
  error?: string;
  disabled: boolean;
  onChange: (value: unknown) => void;
}) {
  if (field.kind === "bool") {
    return (
      <SettingRow title={field.label}>
        <Switch checked={Boolean(value)} disabled={disabled} onCheckedChange={onChange} aria-label={field.label} />
      </SettingRow>
    );
  }
  if (field.kind === "cron") {
    const current = String(value ?? "");
    return (
      <SettingRow title={field.label} value={current ? cronLabel(current) : "不定时"} error={error} stacked>
        <div className="flex min-w-0 flex-col gap-2">
          <div className="flex flex-wrap gap-1.5">
            {CRON_PRESETS.map((preset) => (
              <Button
                key={preset.value || "none"}
                type="button"
                size="xs"
                variant={current.trim() === preset.value ? "secondary" : "outline"}
                disabled={disabled}
                onClick={() => onChange(preset.value)}
              >
                {preset.label}
              </Button>
            ))}
          </div>
          <Input
            value={current}
            disabled={disabled}
            onChange={(e) => onChange(e.target.value)}
            placeholder="分 时 日 月 星期，留空不定时"
            aria-label={field.label}
            aria-invalid={Boolean(error) || undefined}
            className="h-8 font-mono text-xs"
          />
        </div>
      </SettingRow>
    );
  }
  return (
    <SettingRow title={field.label} error={error}>
      <DecimalInput spec={field} value={typeof value === "number" ? value : Number.NaN} disabled={disabled} invalid={Boolean(error)} onChange={onChange} />
    </SettingRow>
  );
}

/**
 * 小数输入：`type=number` 在输入「0.」这类中间态时会把值报成空串，受控写法下没法敲小数点。
 * 这里保留用户敲的原文，能解析成数字时才往上报；外部值变了（还原、换应用）再同步原文。
 */
function DecimalInput({
  spec,
  value,
  disabled,
  invalid,
  onChange
}: {
  spec: NumberFieldSpec;
  value: number;
  disabled: boolean;
  invalid: boolean;
  onChange: (value: number) => void;
}) {
  const [text, setText] = useState(Number.isFinite(value) ? String(value) : "");
  const [seen, setSeen] = useState(value);
  if (!Object.is(value, seen)) {
    setSeen(value);
    setText(Number.isFinite(value) ? String(value) : "");
  }
  return (
    <div className="relative flex w-32 min-w-0 items-center">
      <Input
        value={text}
        disabled={disabled}
        inputMode="decimal"
        aria-label={spec.label}
        aria-invalid={invalid || undefined}
        title={`${spec.min} 到 ${spec.max}`}
        onChange={(e) => {
          const raw = e.target.value.replace(/[^\d.\-]/g, "");
          setText(raw);
          const next = raw.trim() === "" || raw === "-" ? Number.NaN : Number(raw);
          setSeen(next);
          onChange(next);
        }}
        className={cn("h-8 text-right text-xs tabular-nums", spec.unit && "pr-9")}
      />
      {spec.unit ? <span className="pointer-events-none absolute right-2.5 text-xs text-muted-foreground">{spec.unit}</span> : null}
    </div>
  );
}
