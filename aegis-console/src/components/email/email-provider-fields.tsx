"use client";

import { useMemo, useState } from "react";
import { Check, ChevronDown, Copy, ExternalLink, Eye, EyeOff, Info, Settings2, ShieldAlert } from "lucide-react";
import type { EmailConfigField, EmailProviderMeta } from "@/lib/api/types";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

/**
 * 邮件服务商的动态配置表单。
 *
 * 字段全部来自后端 `Describe()` 下发的 schema，**本文件不认识任何一家服务商** ——
 * 与支付渠道的 `provider-fields.tsx` 同一套做法：后端新增一家服务商时，
 * 服务商卡片与配置表单自动出现，这里零改动。
 */

type ChangeHandler = (key: string, value: string) => void;

/** 分区顺序即渲染顺序；后端字段的 group 落在这里，未知 group 归入「其他」。 */
const GROUPS: Array<{ key: string; title: string; hint?: string }> = [
  { key: "credential", title: "服务商凭据", hint: "仅存于服务端，加密落库、永不回传" },
  { key: "sender", title: "发件人身份" },
  { key: "webhook", title: "投递回执", hint: "回填送达 / 退信 / 投诉状态" },
  { key: "advanced", title: "高级选项" },
  { key: "other", title: "其他" }
];

function FieldShell({ field, children }: { field: EmailConfigField; children: React.ReactNode }) {
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

/**
 * 密钥输入框。
 *
 * 已配置时占位符写「已配置，留空即不修改」而不是显示掩码字符：掩码看起来像
 * 一个真实值，用户会以为提交时会连它一起发出去，于是不敢改任何别的字段。
 */
function SecretInput({
  field,
  value,
  configured,
  onChange
}: {
  field: EmailConfigField;
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

/** 键值对字段。值以 JSON 对象字符串存放，编辑态用「键=值」逐行表示。 */
function KeyValueField({ value, onChange, field }: { field: EmailConfigField; value: string; onChange: (v: string) => void }) {
  const text = useMemo(() => {
    if (!value.trim()) return "";
    try {
      const parsed = JSON.parse(value) as Record<string, string>;
      return Object.entries(parsed)
        .map(([k, v]) => `${k}=${v}`)
        .join("\n");
    } catch {
      // 存的不是合法 JSON 时原样显示，让用户看得见问题所在而不是拿到一个空框
      return value;
    }
  }, [value]);

  return (
    <FieldShell field={field}>
      <Textarea
        className="text-xs font-mono"
        rows={3}
        placeholder={"env=prod\nteam=growth"}
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
  field: EmailConfigField;
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
            type={field.type === "email" ? "email" : "text"}
            placeholder={field.placeholder}
            value={raw}
            onChange={(e) => onSetting(field.key, e.target.value)}
          />
        </FieldShell>
      );
  }
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      type="button"
      size="icon"
      variant="ghost"
      className="size-7 shrink-0"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1600);
        } catch {
          // 剪贴板不可用（非安全上下文）时静默失败，用户仍可手动选中复制
        }
      }}
    >
      {copied ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
    </Button>
  );
}

/** 回调令牌未填时的占位文案。留一个中文占位比留空好：留空的地址看起来是完整的。 */
const TOKEN_PLACEHOLDER = "<在下面填写回调令牌>";

/**
 * 把后端下发的 webhookPath 模板拼成完整地址。
 *
 * 三个占位符：`{scope}` 作用域段、`{config}` 配置名（为空时连同前面的 `/` 一起去掉）、
 * `{token}` 回调令牌。令牌只存在于**当前编辑态**——服务端从不回传密钥，
 * 因此关掉弹窗再打开时它会退回占位符，这是刻意的而不是 bug。
 */
function buildWebhookURL(template: string, base: string, scopeSegment: string, configName: string, token: string) {
  let path = template.replace("{scope}", encodeURIComponent(scopeSegment));
  const name = configName.trim();
  path = name ? path.replace("{config}", encodeURIComponent(name)) : path.replace("/{config}", "");
  path = path.replace("{token}", token.trim() ? encodeURIComponent(token.trim()) : TOKEN_PLACEHOLDER);
  return `${base}${path}`;
}

/**
 * 投递回执地址提示：拼成完整地址供一键复制。
 *
 * 基址优先取显式配置的 API 直连地址，未配置（同源代理部署）时退回当前 origin ——
 * 控制台与后端不同域时给出的地址必须指向**后端**，否则填进服务商后台的是一个
 * 永远 404 的地址，而回执收不到这件事在界面上完全看不出来。
 */
function WebhookCard({
  meta,
  scopeSegment,
  configName,
  token
}: {
  meta: EmailProviderMeta;
  scopeSegment: string;
  configName: string;
  token: string;
}) {
  const origin = useOrigin();
  const base = appConfig.apiBaseUrl || origin;
  if (!meta.webhookPath) return null;

  const needsToken = meta.webhookPath.includes("{token}");
  const url = buildWebhookURL(meta.webhookPath, base, scopeSegment, configName, token);
  const ready = !needsToken || Boolean(token.trim());

  return (
    <div className="space-y-2 rounded-lg border border-dashed bg-muted/40 px-3 py-2.5">
      <div className="flex items-start gap-1.5">
        <span className="mt-1.5 shrink-0 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
          回执地址
        </span>
        {/* 地址要原样填进上游后台，截断会让人复制/核对时看不出差异，因此换行显示 */}
        <code className="min-w-0 flex-1 rounded bg-background/70 px-2 py-1 font-mono text-[11px] leading-relaxed break-all">
          {url}
        </code>
        <CopyButton value={url} />
      </div>
      {needsToken && (
        <p className="flex gap-1.5 text-[11px] leading-relaxed text-amber-600 dark:text-amber-400">
          <ShieldAlert className="mt-0.5 size-3 shrink-0" />
          <span>
            这家不对回执签名，**地址本身就是凭据**，请当密钥保管、不要贴进工单或聊天群。
            {ready
              ? "令牌只在本次编辑时显示，重新打开这个弹窗会退回占位符 —— 服务端从不回传密钥。"
              : "先在下面填好「回调令牌」，这里就会显示可直接复制的完整地址。"}
          </span>
        </p>
      )}
      {meta.webhookNote && <p className="text-[11px] leading-relaxed text-muted-foreground">{meta.webhookNote}</p>}
    </div>
  );
}

/** 接入注意事项。这些是「配了却发不出去」的高频原因，值得摆在表单顶部而不是文档里。 */
function ProviderNotes({ meta }: { meta: EmailProviderMeta }) {
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

export function EmailProviderFields({
  meta,
  settings,
  secrets,
  secretSet,
  scopeSegment,
  configName,
  onSetting,
  onSecret
}: {
  meta?: EmailProviderMeta | null;
  settings: Record<string, string>;
  secrets: Record<string, string>;
  secretSet: Record<string, boolean>;
  /** 回执地址里的作用域段：应用 id，或 `platform`。 */
  scopeSegment: string;
  /** 回执地址里的配置名段；为空时该段会被去掉（落到默认配置）。 */
  configName: string;
  onSetting: ChangeHandler;
  onSecret: ChangeHandler;
}) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const { grouped, advancedFields } = useMemo(() => {
    const all = meta?.fields ?? [];
    const adv = all.filter((f) => f.advanced);
    const groups = new Map<string, EmailConfigField[]>();
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
    return <p className="text-xs text-muted-foreground">正在加载服务商配置项…</p>;
  }

  return (
    // 分栏跟随抽屉宽度（@container/form），不跟随视口：视口断点会在窄窗口里
    // 把抽屉也排成两列，每列只剩 200 余像素，标签与提示全被挤到换行
    <div className="@container/form space-y-4">
      <ProviderNotes meta={meta} />
      <WebhookCard
        meta={meta}
        scopeSegment={scopeSegment}
        configName={configName}
        token={secrets.webhookSecret ?? ""}
      />

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
                <div key={field.key} className={field.type === "textarea" || field.type === "kv" ? "@xl/form:col-span-2" : undefined}>
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
                <div key={field.key} className={field.type === "textarea" || field.type === "kv" ? "@xl/form:col-span-2" : undefined}>
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

/**
 * 用后端下发的字段默认值初始化配置数据。
 *
 * 新建通道时调用，避免用户必须手动填写每一个有合理默认的字段
 * （SMTP 端口、加密方式、AWS 地域这类字段几乎总是取默认值）。
 */
export function buildDefaultEmailSettings(meta?: EmailProviderMeta | null): Record<string, string> {
  const settings: Record<string, string> = {};
  for (const field of meta?.fields ?? []) {
    if (field.secret) continue;
    if (field.default === undefined || field.default === null) continue;
    settings[field.key] = String(field.default);
  }
  return settings;
}

/** 服务商能力标签。attachments 是最值得显眼的一项 —— 它决定凭证怎么寄。 */
export const EMAIL_CAPABILITY_LABELS: Array<{ key: keyof EmailProviderMeta["capabilities"]; label: string }> = [
  { key: "attachments", label: "支持附件" },
  { key: "webhook", label: "投递回执" },
  { key: "tracking", label: "打开/点击追踪" },
  { key: "tags", label: "邮件标签" }
];
