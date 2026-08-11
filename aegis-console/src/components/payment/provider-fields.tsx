"use client";

import { useMemo, useState } from "react";
import { Check, ChevronDown, Copy, Eye, EyeOff, ExternalLink, Settings2 } from "lucide-react";
import type { PaymentConfigField, PaymentProviderMeta } from "@/lib/api/types";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

type ChangeHandler = (key: string, value: unknown) => void;

// ── 分区定义 ──────────────────────────
// 顺序即渲染顺序；后端字段的 group 落在这里，未知 group 归入「其他」

const GROUPS: Array<{ key: string; title: string; hint?: string }> = [
  { key: "credential", title: "商户凭据", hint: "来自支付平台后台，仅存于服务端" },
  { key: "gateway", title: "网关与回调" },
  { key: "limit", title: "交易限额", hint: "由网关层统一强制执行，0 表示不限制" },
  { key: "other", title: "其他" }
];

// ── 单字段渲染 ──────────────────────────

function FieldShell({
  field,
  children
}: {
  field: PaymentConfigField;
  children: React.ReactNode;
}) {
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

function SecretInput({ field, value, onChange }: { field: PaymentConfigField; value: string; onChange: (v: string) => void }) {
  const [show, setShow] = useState(false);
  return (
    <div className="flex gap-1">
      <Input
        type={show ? "text" : "password"}
        className="h-8 font-mono text-xs"
        placeholder={field.placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <Button type="button" size="icon" variant="ghost" className="size-8 shrink-0" onClick={() => setShow(!show)}>
        {show ? <EyeOff className="size-3" /> : <Eye className="size-3" />}
      </Button>
    </div>
  );
}

/** 按后端下发的字段类型动态渲染控件 */
function DynamicField({
  field,
  data,
  onChange
}: {
  field: PaymentConfigField;
  data: Record<string, unknown>;
  onChange: ChangeHandler;
}) {
  const raw = data[field.key];

  switch (field.type) {
    case "switch": {
      const checked = raw === undefined ? field.default === true : !!raw;
      return (
        <label className="flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/40">
          <Switch checked={checked} onCheckedChange={(v) => onChange(field.key, v)} className="mt-0.5 shrink-0" />
          <span className="min-w-0 space-y-0.5">
            <span className="block text-xs">{field.label}</span>
            {field.help && <span className="block text-[11px] leading-relaxed text-muted-foreground">{field.help}</span>}
          </span>
        </label>
      );
    }

    case "select": {
      const current = String(raw ?? field.default ?? field.options?.[0]?.value ?? "");
      return (
        <FieldShell field={field}>
          <Select value={current} onValueChange={(v) => onChange(field.key, v)}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(field.options ?? []).map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldShell>
      );
    }

    case "number":
      return (
        <FieldShell field={field}>
          <Input
            type="number"
            className="h-8 text-sm"
            placeholder={field.placeholder}
            value={raw === undefined || raw === null ? "" : String(raw)}
            onChange={(e) => onChange(field.key, e.target.value === "" ? "" : Number(e.target.value))}
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
            value={String(raw ?? "")}
            onChange={(e) => onChange(field.key, e.target.value)}
          />
        </FieldShell>
      );

    case "tags": {
      const text = Array.isArray(raw) ? (raw as string[]).join(", ") : String(raw ?? "");
      return (
        <FieldShell field={field}>
          <Textarea
            className="text-xs"
            rows={2}
            placeholder={field.placeholder}
            value={text}
            onChange={(e) => {
              const items = e.target.value
                .split(/[,，\n]/)
                .map((s) => s.trim())
                .filter(Boolean);
              onChange(field.key, items.length ? items : "");
            }}
          />
        </FieldShell>
      );
    }

    case "secret":
      return (
        <FieldShell field={field}>
          <SecretInput field={field} value={String(raw ?? "")} onChange={(v) => onChange(field.key, v)} />
        </FieldShell>
      );

    default:
      return (
        <FieldShell field={field}>
          <Input
            className="h-8 text-sm"
            placeholder={field.placeholder}
            value={String(raw ?? "")}
            onChange={(e) => onChange(field.key, e.target.value)}
          />
        </FieldShell>
      );
  }
}

// ── 回调地址卡片 ──────────────────────────

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

/**
 * 回调地址提示：把后端下发的 callbackPath 拼成完整地址供一键复制。
 *
 * 基址优先取显式配置的 API 直连地址，未配置（同源代理部署）时退回当前 origin，
 * 因此控制台与后端不同域时也不会给出错误的地址。
 */
function CallbackCard({ meta }: { meta: PaymentProviderMeta }) {
  const origin = useOrigin();
  const base = appConfig.apiBaseUrl || origin;
  const url = meta.callbackPath ? `${base}${meta.callbackPath}` : "";

  if (!url && !meta.callbackNote) return null;

  return (
    <div className="space-y-2 rounded-lg border border-dashed bg-muted/40 px-3 py-2.5">
      {url && (
        <div className="flex items-start gap-1.5">
          <span className="mt-1.5 shrink-0 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">回调地址</span>
          {/* 地址要原样填进上游后台，截断会让人复制/核对时看不出差异，因此换行显示 */}
          <code className="min-w-0 flex-1 rounded bg-background/70 px-2 py-1 font-mono text-[11px] leading-relaxed break-all">
            {url}
          </code>
          <CopyButton value={url} />
        </div>
      )}
      {meta.callbackNote && (
        <p className="text-[11px] leading-relaxed text-muted-foreground">{meta.callbackNote}</p>
      )}
    </div>
  );
}

// ── 能力标签 ──────────────────────────

const CAPABILITY_LABELS: Array<{ key: keyof PaymentProviderMeta["capabilities"]; label: string }> = [
  { key: "redirect", label: "收银台跳转" },
  { key: "qrcode", label: "扫码支付" },
  { key: "webhookSignature", label: "回调验签" },
  { key: "remoteQuery", label: "上游查单" },
  { key: "sandbox", label: "沙箱环境" },
  { key: "subMerchant", label: "服务商模式" }
];

export function PaymentCapabilityBadges({ meta }: { meta: PaymentProviderMeta }) {
  const active = CAPABILITY_LABELS.filter((c) => meta.capabilities?.[c.key]);
  if (!active.length) return null;
  return (
    <div className="flex flex-wrap gap-1">
      {active.map((c) => (
        <Badge key={c.key} variant="secondary" size="sm" className="font-normal">
          {c.label}
        </Badge>
      ))}
    </div>
  );
}

// ── 导出：动态配置表单 ──────────────────────────

export function PaymentProviderFields({
  meta,
  data,
  onChange
}: {
  meta?: PaymentProviderMeta | null;
  data: Record<string, unknown>;
  onChange: ChangeHandler;
}) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const { grouped, advancedFields } = useMemo(() => {
    const all = meta?.fields ?? [];
    const adv = all.filter((f) => f.advanced);
    const groups = new Map<string, PaymentConfigField[]>();
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
    return <p className="text-xs text-muted-foreground">正在加载渠道配置项…</p>;
  }

  if (!meta.fields?.length) {
    return (
      <div className="space-y-3">
        <CallbackCard meta={meta} />
        <div className="rounded-lg border bg-muted/30 px-3 py-3 text-xs leading-relaxed text-muted-foreground">
          <p className="font-medium text-foreground">该渠道无需额外参数</p>
          {meta.description && <p className="mt-1">{meta.description}</p>}
        </div>
      </div>
    );
  }

  return (
    // 分栏跟随抽屉宽度（@container/form），不跟随视口：
    // 视口断点会在窄窗口里把抽屉也排成两列，每列只剩 200 余像素，标签与提示全被挤到换行
    <div className="@container/form space-y-4">
      <CallbackCard meta={meta} />

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
                <div key={field.key} className={field.type === "textarea" ? "@xl/form:col-span-2" : undefined}>
                  <DynamicField field={field} data={data} onChange={onChange} />
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
                <div key={field.key} className={field.type === "textarea" ? "@xl/form:col-span-2" : undefined}>
                  <DynamicField field={field} data={data} onChange={onChange} />
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
 * 新建渠道时调用，避免用户必须手动填写每一个有合理默认的字段。
 */
export function buildDefaultConfigData(meta?: PaymentProviderMeta | null): Record<string, unknown> {
  const data: Record<string, unknown> = {};
  for (const field of meta?.fields ?? []) {
    if (field.default !== undefined && field.default !== null) {
      data[field.key] = field.default;
    }
  }
  return data;
}
