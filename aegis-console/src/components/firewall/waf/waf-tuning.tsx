"use client";

import type { ReactNode } from "react";
import { SlidersHorizontal } from "lucide-react";
import { ChipInput, FieldError, Panel } from "@/components/traffic/traffic-kit";
import { cn } from "@/lib/utils";
import { FIREWALL_DEFAULTS, Hint, RestoreDefault, invalidIndexes, type SettingsForm, type SettingsSetter } from "./settings-draft";

/**
 * 调优档：规则集 v2 用「按字段 / 按路径」的精确排除取代 v1 的「JSON 请求体一律免攻击规则」。
 * 对应指令 1100110+（请求体豁免前缀）、1100300（敏感字段）、1100310（富文本字段）与 1100001 的内容类型白名单。
 */

const isPathPrefix = (value: string) => value.startsWith("/") && !/\s/.test(value);
const isArgName = (value: string) => /^[A-Za-z0-9_.\-[\]]{1,64}$/.test(value);
const isContentType = (value: string) => /^[a-z0-9][a-z0-9!#$&^_.+-]*\/[a-z0-9*][a-z0-9!#$&^_.+*-]*$/.test(value);

export function tuningValidation(form: SettingsForm): string | null {
  if (form.corazaSensitiveArgs.some((value) => !isArgName(value))) return "敏感字段里有无效的字段名";
  if (form.corazaRichTextArgs.some((value) => !isArgName(value))) return "富文本字段里有无效的字段名";
  if (form.corazaBodyExemptPrefixes.some((value) => !isPathPrefix(value))) return "请求体豁免路径必须以 / 开头";
  if (form.corazaExtraContentTypes.some((value) => !isContentType(value))) return "额外内容类型的格式应为 type/subtype";
  return null;
}

export function TuningPanel({ form, set, dimmed }: { form: SettingsForm; set: SettingsSetter; dimmed: boolean }) {
  return (
    <Panel icon={SlidersHorizontal} tone="violet" title="调优档" className={cn(dimmed && "opacity-60")}>
      <div className="mb-4 rounded-xl border border-border bg-muted/40 px-3 py-2.5 text-xs leading-5 text-muted-foreground">
        规则集 v2 按字段与路径精确排除，取代 v1 的「JSON 请求体一律免检」：JSON 里的 SQL 注入、XSS 与命令注入照常检查，
        只有下列字段与路径例外。字段按最后一段匹配、不区分大小写，<span className="font-mono">password</span> 同时匹配表单字段与{" "}
        <span className="font-mono">json.user.password</span>。
      </div>
      <div className="grid min-w-0 gap-x-6 gap-y-5 @4xl:grid-cols-2">
        <TuningField
          title="敏感字段"
          hint="免全部攻击规则，命中数据里的值打码"
          values={form.corazaSensitiveArgs}
          defaults={FIREWALL_DEFAULTS.corazaSensitiveArgs}
          onChange={(next) => set("corazaSensitiveArgs", next)}
          valid={isArgName}
          error="字段名只能包含字母、数字与 _ . - [ ]"
          placeholder="password"
        />
        <TuningField
          title="富文本字段"
          hint="免 XSS / SQLi / RCE / 注入，保留 LFI、RFI、SSRF 与 Java 检测"
          values={form.corazaRichTextArgs}
          defaults={FIREWALL_DEFAULTS.corazaRichTextArgs}
          onChange={(next) => set("corazaRichTextArgs", next)}
          valid={isArgName}
          error="字段名只能包含字母、数字与 _ . - [ ]"
          placeholder="content"
        />
        <TuningField
          title="请求体豁免路径"
          hint="按前缀匹配，只检查请求头与 URI、不检查请求体"
          values={form.corazaBodyExemptPrefixes}
          defaults={FIREWALL_DEFAULTS.corazaBodyExemptPrefixes}
          onChange={(next) => set("corazaBodyExemptPrefixes", next)}
          valid={isPathPrefix}
          error="路径必须以 / 开头"
          placeholder="/api/ai/v1/"
          normalize={(raw) => raw.trim()}
        />
        <TuningField
          title="额外允许的内容类型"
          hint="CRS 内容类型白名单之外、Aegis 实际接收的类型"
          values={form.corazaExtraContentTypes}
          defaults={FIREWALL_DEFAULTS.corazaExtraContentTypes}
          onChange={(next) => set("corazaExtraContentTypes", next)}
          valid={isContentType}
          error="格式应为 type/subtype，例如 application/x-ndjson"
          placeholder="application/x-ndjson"
          normalize={(raw) => raw.trim().toLowerCase()}
        />
      </div>
    </Panel>
  );
}

export function TuningField({
  title,
  hint,
  values,
  defaults,
  onChange,
  valid,
  error,
  placeholder,
  normalize,
  aside,
  clearToDefaults
}: {
  title: string;
  hint?: ReactNode;
  values: string[];
  defaults?: readonly string[];
  onChange: (next: string[]) => void;
  valid?: (value: string) => boolean;
  error?: string;
  placeholder?: string;
  normalize?: (raw: string) => string;
  aside?: ReactNode;
  /** 后端把空列表当成「没填」、回到内置清单（不能显式清空） */
  clearToDefaults?: boolean;
}) {
  const invalid = valid ? invalidIndexes(values, valid) : undefined;
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex min-h-6 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-xs text-muted-foreground tabular-nums">{values.length} 条</span>
        <span className="flex-1" />
        {aside}
        {defaults ? <RestoreDefault current={values} defaults={defaults} onRestore={onChange} /> : null}
      </div>
      {hint ? <Hint>{hint}</Hint> : null}
      <ChipInput value={values} onChange={onChange} placeholder={placeholder} invalid={invalid} normalize={normalize} ariaLabel={title} />
      {invalid?.size ? <FieldError message={error} /> : null}
      {values.length === 0 && defaults?.length ? (
        <Hint tone="warn">{clearToDefaults ? "清空后保存会回到内置清单" : "已清空：不再使用内置清单"}</Hint>
      ) : null}
    </div>
  );
}
