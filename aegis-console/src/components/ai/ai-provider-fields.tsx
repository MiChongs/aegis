"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ExternalLink, Eye, EyeOff, Info, Settings2 } from "lucide-react";
import type { AIConfigField, AIProviderMeta } from "@/lib/api/ai";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

/**
 * AI 供应商的动态配置表单。
 *
 * 字段全部来自后端目录下发的 schema，**本文件不认识任何一家供应商** ——
 * 与邮件 / 支付的 provider-fields 同一套做法：后端新增一家供应商时这里零改动。
 */

type ChangeHandler = (key: string, value: string) => void;

const GROUPS: Array<{ key: string; title: string; hint?: string }> = [
  { key: "credential", title: "服务商凭据", hint: "仅存于服务端，加密落库、永不回传" },
  { key: "endpoint", title: "端点与模型" },
  { key: "advanced", title: "高级选项" },
  { key: "other", title: "其他" }
];

function FieldShell({ field, children }: { field: AIConfigField; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">
        {field.label}
        {field.required && <span className="text-destructive"> *</span>}
      </Label>
      {children}
      {field.help && <p className="text-[11px] leading-relaxed text-muted-foreground">{field.help}</p>}
    </div>
  );
}

function SecretInput({
  field,
  value,
  configured,
  onChange
}: {
  field: AIConfigField;
  value: string;
  configured: boolean;
  onChange: (v: string) => void;
}) {
  const [show, setShow] = useState(false);
  return (
    <div className="flex gap-1">
      <Input
        type={show ? "text" : "password"}
        className="h-8 font-mono text-xs"
        placeholder={configured ? "已配置，留空即不修改" : field.placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <Button type="button" size="icon" variant="ghost" className="size-8 shrink-0" onClick={() => setShow(!show)}>
        {show ? <EyeOff className="size-3" /> : <Eye className="size-3" />}
      </Button>
    </div>
  );
}

/** 键值对字段（附加请求头）。值以 JSON 对象字符串存放，编辑态用「键=值」逐行表示。 */
function KeyValueField({ field, value, onChange }: { field: AIConfigField; value: string; onChange: (v: string) => void }) {
  const text = useMemo(() => {
    if (!value.trim()) return "";
    try {
      const parsed = JSON.parse(value) as Record<string, string>;
      return Object.entries(parsed)
        .map(([k, v]) => `${k}=${v}`)
        .join("\n");
    } catch {
      return value;
    }
  }, [value]);

  return (
    <FieldShell field={field}>
      <Textarea
        className="text-xs font-mono"
        rows={3}
        placeholder={"X-Gateway-Token=abc\nX-Route=cn"}
        value={text}
        onChange={(e) => {
          const map: Record<string, string> = {};
          for (const line of e.target.value.split("\n")) {
            const index = line.indexOf("=");
            if (index <= 0) continue;
            const key = line.slice(0, index).trim();
            if (!key) continue;
            map[key] = line.slice(index + 1).trim();
          }
          onChange(Object.keys(map).length ? JSON.stringify(map) : "");
        }}
      />
    </FieldShell>
  );
}

function DynamicField({
  field,
  settings,
  secrets,
  secretSet,
  onSetting,
  onSecret
}: {
  field: AIConfigField;
  settings: Record<string, string>;
  secrets: Record<string, string>;
  secretSet: Record<string, boolean>;
  onSetting: ChangeHandler;
  onSecret: ChangeHandler;
}) {
  if (field.secret) {
    return (
      <FieldShell field={field}>
        <SecretInput
          field={field}
          value={secrets[field.key] ?? ""}
          configured={Boolean(secretSet[field.key])}
          onChange={(v) => onSecret(field.key, v)}
        />
      </FieldShell>
    );
  }

  const raw = settings[field.key] ?? (field.default !== undefined && field.default !== null ? String(field.default) : "");

  switch (field.type) {
    case "switch": {
      const checked = raw === "true";
      return (
        <label className="flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/40">
          <Switch
            checked={checked}
            onCheckedChange={(v) => onSetting(field.key, v ? "true" : "false")}
            className="mt-0.5 shrink-0"
          />
          <span className="min-w-0 space-y-0.5">
            <span className="block text-xs">{field.label}</span>
            {field.help && <span className="block text-[11px] leading-relaxed text-muted-foreground">{field.help}</span>}
          </span>
        </label>
      );
    }

    case "select":
      return (
        <FieldShell field={field}>
          <Select value={raw || String(field.options?.[0]?.value ?? "")} onValueChange={(v) => onSetting(field.key, v)}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(field.options ?? []).map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldShell>
      );

    case "number":
      return (
        <FieldShell field={field}>
          <Input
            type="number"
            className="h-8 text-sm"
            placeholder={field.placeholder}
            value={raw}
            onChange={(e) => onSetting(field.key, e.target.value)}
          />
        </FieldShell>
      );

    case "textarea":
      return (
        <FieldShell field={field}>
          <Textarea
            className="font-mono text-xs"
            rows={4}
            placeholder={field.placeholder}
            value={raw}
            onChange={(e) => onSetting(field.key, e.target.value)}
          />
        </FieldShell>
      );

    case "kv":
      return <KeyValueField field={field} value={raw} onChange={(v) => onSetting(field.key, v)} />;

    default:
      return (
        <FieldShell field={field}>
          <Input
            className={`h-8 text-sm${field.type === "url" ? " font-mono" : ""}`}
            placeholder={field.placeholder}
            value={raw}
            onChange={(e) => onSetting(field.key, e.target.value)}
          />
        </FieldShell>
      );
  }
}

function ProviderNotes({ meta }: { meta: AIProviderMeta }) {
  if (!meta.notes?.length) return null;
  return (
    <ul className="space-y-1 rounded-lg border bg-muted/30 px-3 py-2.5">
      {meta.notes.map((note) => (
        <li key={note} className="flex gap-1.5 text-[11px] leading-relaxed text-muted-foreground">
          <Info className="mt-0.5 size-3 shrink-0" />
          <span>{note}</span>
        </li>
      ))}
    </ul>
  );
}

export function AIProviderFields({
  meta,
  settings,
  secrets,
  secretSet,
  onSetting,
  onSecret
}: {
  meta?: AIProviderMeta | null;
  settings: Record<string, string>;
  secrets: Record<string, string>;
  secretSet: Record<string, boolean>;
  onSetting: ChangeHandler;
  onSecret: ChangeHandler;
}) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const { grouped, advancedFields } = useMemo(() => {
    const all = meta?.fields ?? [];
    const adv = all.filter((f) => f.advanced);
    const groups = new Map<string, AIConfigField[]>();
    for (const field of all) {
      if (field.advanced) continue;
      const key = GROUPS.some((g) => g.key === field.group) ? (field.group as string) : "other";
      const list = groups.get(key) ?? [];
      list.push(field);
      groups.set(key, list);
    }
    return { grouped: groups, advancedFields: adv };
  }, [meta]);

  if (!meta) {
    return <p className="text-xs text-muted-foreground">正在加载供应商配置项…</p>;
  }

  return (
    <div className="@container/form space-y-4">
      <ProviderNotes meta={meta} />

      {GROUPS.map((group) => {
        const list = grouped.get(group.key);
        if (!list?.length) return null;
        return (
          <div key={group.key} className="space-y-2.5">
            <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
              <h5 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{group.title}</h5>
              {group.hint && <span className="text-[10px] text-muted-foreground/70">{group.hint}</span>}
            </div>
            <div className="grid gap-3 @xl/form:grid-cols-2">
              {list.map((field) => (
                <div
                  key={field.key}
                  className={field.type === "textarea" || field.type === "kv" ? "@xl/form:col-span-2" : undefined}
                >
                  <DynamicField
                    field={field}
                    settings={settings}
                    secrets={secrets}
                    secretSet={secretSet}
                    onSetting={onSetting}
                    onSecret={onSecret}
                  />
                </div>
              ))}
            </div>
          </div>
        );
      })}

      {advancedFields.length > 0 && (
        <Collapsible open={showAdvanced} onOpenChange={setShowAdvanced}>
          <CollapsibleTrigger asChild>
            <Button type="button" size="sm" variant="ghost" className="h-7 w-full justify-start gap-1.5 px-2 text-xs">
              <Settings2 className="size-3" />
              高级选项
              <span className="text-muted-foreground">({advancedFields.length})</span>
              <ChevronDown className={`ml-auto size-3 transition-transform ${showAdvanced ? "rotate-180" : ""}`} />
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-3">
            <div className="grid gap-3 @xl/form:grid-cols-2">
              {advancedFields.map((field) => (
                <div
                  key={field.key}
                  className={field.type === "textarea" || field.type === "kv" ? "@xl/form:col-span-2" : undefined}
                >
                  <DynamicField
                    field={field}
                    settings={settings}
                    secrets={secrets}
                    secretSet={secretSet}
                    onSetting={onSetting}
                    onSecret={onSecret}
                  />
                </div>
              ))}
            </div>
          </CollapsibleContent>
        </Collapsible>
      )}

      {meta.docUrl && (
        <a
          href={meta.docUrl}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1 text-[11px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          <ExternalLink className="size-3" />
          查看 {meta.name} 官方接入文档
        </a>
      )}
    </div>
  );
}

/** 用后端下发的字段默认值 + 建议型号初始化新建配置。 */
export function buildDefaultAISettings(meta?: AIProviderMeta | null): Record<string, string> {
  const settings: Record<string, string> = {};
  for (const field of meta?.fields ?? []) {
    if (field.secret) continue;
    if (field.default === undefined || field.default === null) continue;
    settings[field.key] = String(field.default);
  }
  // 建议型号直接预填进「可用型号」：绝大多数用户第一次配置时不知道该填什么标识，
  // 预填的清单既是可用值又是格式示范，不想要可以整段删掉。
  if (meta?.suggestedModels?.length && !settings.models) {
    settings.models = meta.suggestedModels.join("\n");
  }
  return settings;
}

/** 供应商能力标签。toolCalls 决定 Agent 面板可不可用，最值得显眼。 */
export const AI_CAPABILITY_LABELS: Array<{ key: keyof AIProviderMeta["capabilities"]; label: string }> = [
  { key: "toolCalls", label: "工具调用" },
  { key: "streaming", label: "流式输出" },
  { key: "vision", label: "图像输入" },
  { key: "jsonMode", label: "JSON 输出" },
  { key: "reasoning", label: "思考块" }
];
