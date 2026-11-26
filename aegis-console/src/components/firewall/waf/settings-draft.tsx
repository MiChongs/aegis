"use client";

import { useMemo, useState, type ReactNode } from "react";
import { Database, Loader2, Lock, Save, ServerCog, Undo2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { FirewallSettings, FirewallSettingsPatch } from "@/lib/api/firewall";
import { formatDateTime } from "@/lib/datetime";
import { useFirewallSettingsQuery, useUpdateFirewallSettingsMutation } from "@/lib/firewall-hooks";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";

/**
 * 「WAF 引擎」与「访问控制」两个页签共用的设置草稿。
 *
 * - 草稿按 `updatedAt` 绑定（CLAUDE.md「草稿按作用域绑定」）：别的管理员先保存了，库里的
 *   updatedAt 变化，本地草稿自动作废，而不是把对方的改动覆盖回去。不用 reloadVersion：
 *   它是**副本**的热更新计数，多副本时两次 GET 打到不同副本就会不一样，草稿会无故丢失。
 * - 每个页签只管自己那几个字段（`keys`），保存时只发真正改过的字段（部分更新）。
 *   列表字段发 `[]` 表示显式清空，这正是「把最后一条删掉」的语义。
 * - 后端保存前用改动后的整套配置试编译 WAF，失败返回 40090 并带原因；错误提示由 hook 统一弹。
 */

export type SettingsForm = Required<FirewallSettingsPatch>;
export type SettingsKey = keyof SettingsForm;
export type SettingsSetter = <K extends SettingsKey>(key: K, value: SettingsForm[K]) => void;
export type SettingsUpdater = (fn: (current: SettingsForm) => SettingsForm) => void;

const LIST_KEYS = [
  "allowedCIDRs",
  "blockedCIDRs",
  "blockedUserAgents",
  "blockedPathPrefix",
  "corazaRemovedRules",
  "corazaDisabledGroups",
  "corazaBodyExemptPrefixes",
  "corazaSensitiveArgs",
  "corazaRichTextArgs",
  "corazaExtraContentTypes",
  "trustedCIDRs",
  "blockedSignatures"
] as const satisfies readonly SettingsKey[];

const META_KEYS = ["source", "reloadVersion", "reloadedAt", "updatedBy", "updatedAt", "rulesetVersion"] as const;

export function seedForm(settings: FirewallSettings): SettingsForm {
  const copy: Record<string, unknown> = { ...settings };
  for (const key of META_KEYS) delete copy[key];
  const form = copy as SettingsForm;
  for (const key of LIST_KEYS) {
    const value = settings[key];
    form[key] = Array.isArray(value) ? [...value] : [];
  }
  form.banRedirectUrl = settings.banRedirectUrl ?? "";
  return form;
}

/** 列表字段按集合比较：顺序不同不算改动（停用一组再打开、移除再恢复都不该留下「未保存」） */
function same(a: unknown, b: unknown) {
  if (Array.isArray(a) || Array.isArray(b)) {
    const left = [...((a as string[] | undefined) ?? [])].sort();
    const right = [...((b as string[] | undefined) ?? [])].sort();
    return JSON.stringify(left) === JSON.stringify(right);
  }
  return a === b;
}

export function useSettingsDraft(keys: readonly SettingsKey[]) {
  const query = useFirewallSettingsQuery();
  const save = useUpdateFirewallSettingsMutation();
  const canWrite = usePermissionChecker()("system:firewall:policy");
  const [draft, setDraft] = useState<{ scope: string; value: SettingsForm } | null>(null);

  const settings = query.data;
  const scope = settings ? `${settings.source}|${settings.updatedAt ?? ""}` : "";
  const base = useMemo(() => (settings ? seedForm(settings) : null), [settings]);
  const form = draft && draft.scope === scope ? draft.value : base;

  const changed = useMemo(() => (form && base ? keys.filter((key) => !same(form[key], base[key])) : []), [base, form, keys]);

  const update = (fn: (current: SettingsForm) => SettingsForm) => {
    if (!form) return;
    setDraft({ scope, value: fn(form) });
  };
  const set = <K extends SettingsKey>(key: K, value: SettingsForm[K]) => update((current) => ({ ...current, [key]: value }));

  const patch = useMemo(() => {
    const out: FirewallSettingsPatch = {};
    if (!form) return out;
    for (const key of changed) {
      (out as Record<string, unknown>)[key] = form[key];
    }
    return out;
  }, [changed, form]);

  const submit = () => {
    if (!changed.length) return;
    save.mutate(patch, { onSuccess: () => setDraft(null) });
  };

  return {
    query,
    settings,
    form,
    base,
    changed,
    dirty: changed.length > 0,
    patch,
    set,
    update,
    discard: () => setDraft(null),
    submit,
    pending: save.isPending,
    canWrite
  };
}

/* ── 来源与热更新信息 ── */

export function SettingsMeta({ settings, aside }: { settings: FirewallSettings; aside?: ReactNode }) {
  const fromEnv = settings.source === "environment";
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-border bg-card px-3 py-2 sm:px-4">
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1.5">
        {fromEnv ? (
          <Badge variant="warning" size="sm">
            <ServerCog />
            环境变量
          </Badge>
        ) : settings.source === "database" ? (
          <Badge variant="success" size="sm">
            <Database />
            数据库
          </Badge>
        ) : (
          <Badge variant="danger" size="sm">
            设置不可用
          </Badge>
        )}
        <span className="min-w-0 text-xs text-muted-foreground">
          {fromEnv ? "还没在控制台保存过，当前取自环境变量" : settings.updatedAt ? `更新于 ${formatDateTime(settings.updatedAt)}` : "以数据库为准"}
        </span>
        <span className="text-xs text-muted-foreground tabular-nums">
          本副本热更新 #{settings.reloadVersion}
          {settings.reloadedAt ? ` · ${formatDateTime(settings.reloadedAt)}` : ""}
        </span>
        <span className="text-xs text-muted-foreground tabular-nums">规则集 v{settings.rulesetVersion}</span>
      </div>
      {aside ? <div className="flex shrink-0 flex-wrap items-center gap-2">{aside}</div> : null}
    </div>
  );
}

export function ReadOnlyNotice() {
  return (
    <div className="flex items-center gap-2 rounded-xl border border-border bg-muted/50 px-3 py-2 text-xs text-muted-foreground sm:px-4">
      <Lock className="size-3.5 shrink-0" />
      只读：缺少「防火墙：策略」权限
    </div>
  );
}

/* ── 吸底保存条 ── */

export function SaveBar({
  count,
  pending,
  blockedReason,
  onDiscard,
  onSave
}: {
  count: number;
  pending: boolean;
  /** 有校验错误时禁止保存，并说明原因 */
  blockedReason?: string | null;
  onDiscard: () => void;
  onSave: () => void;
}) {
  if (count <= 0) return null;
  return (
    <div className="sticky bottom-4 z-20 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-2xl border border-border bg-card px-4 py-3 shadow-lg sm:px-5">
      <span className={cn("size-2 shrink-0 rounded-full", blockedReason ? "bg-rose-500" : "bg-amber-500")} aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="text-sm">有 {count} 项未保存的修改</div>
        {blockedReason ? <div className="text-xs text-rose-600 dark:text-rose-400">{blockedReason}</div> : null}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Button size="sm" variant="ghost" onClick={onDiscard} disabled={pending}>
          <Undo2 />
          放弃
        </Button>
        <Button size="sm" onClick={onSave} disabled={pending || Boolean(blockedReason)}>
          {pending ? <Loader2 className="animate-spin" /> : <Save />}
          保存并生效
        </Button>
      </div>
    </div>
  );
}

/* ── 小件 ── */

/** 一行说明（只在真正需要解释后果的地方用） */
export function Hint({ children, tone = "muted", className }: { children: ReactNode; tone?: "muted" | "warn" | "danger"; className?: string }) {
  return (
    <p
      className={cn(
        "text-xs leading-5",
        tone === "muted" && "text-muted-foreground",
        tone === "warn" && "text-amber-700 dark:text-amber-400",
        tone === "danger" && "text-rose-600 dark:text-rose-400",
        className
      )}
    >
      {children}
    </p>
  );
}

/** 「恢复默认」：当前值与内置默认值不同时才出现 */
export function RestoreDefault({ current, defaults, onRestore }: { current: string[]; defaults: readonly string[]; onRestore: (next: string[]) => void }) {
  if (same(current, defaults)) return null;
  return (
    <Button type="button" size="xs" variant="ghost" onClick={() => onRestore([...defaults])}>
      <Undo2 />
      恢复默认
    </Button>
  );
}

/** 与后端 config.go 的内置清单一致（列表字段 nil = 用这些） */
export const FIREWALL_DEFAULTS = {
  corazaRemovedRules: ["920350", "920300", "932235", "932236", "932240", "932250"],
  corazaBodyExemptPrefixes: ["/api/ai/v1/", "/api/public/pay/callback/", "/api/email/webhook/"],
  corazaSensitiveArgs: [
    "password",
    "passwd",
    "pwd",
    "oldPassword",
    "newPassword",
    "confirmPassword",
    "currentPassword",
    "secret",
    "clientSecret",
    "appSecret",
    "privateKey",
    "signature",
    "totpCode",
    "recoveryCode"
  ],
  corazaRichTextArgs: ["content", "html", "markdown", "richText", "reply", "body", "notes"],
  corazaExtraContentTypes: [
    "application/octet-stream",
    "text/plain",
    "application/gzip",
    "application/x-ndjson",
    "application/csp-report",
    "application/reports+json"
  ],
  /** 这两项清空即回到内置清单（后端 len == 0 → 默认），不能显式清空 */
  blockedUserAgents: ["sqlmap", "nikto", "acunetix", "nessus", "wpscan", "gobuster", "dirbuster", "masscan", "nmap", "zgrab", "nuclei"],
  blockedPathPrefix: ["/.env", "/.git", "/.svn", "/wp-admin", "/wp-login", "/phpmyadmin", "/vendor/phpunit", "/_ignition", "/hnap1"],
  blockedSignatures: [
    "../",
    "..\\",
    "%2e%2e",
    "/.git/",
    "/.env",
    "/vendor/phpunit",
    "/etc/passwd",
    "/proc/self/environ",
    "<script",
    "%3cscript",
    "javascript:",
    "onerror=",
    "onload=",
    "union+select",
    "union%20select",
    "sleep(",
    "benchmark(",
    "load_file(",
    "information_schema",
    ";cat+",
    "|cat+",
    "$(curl",
    "$(wget",
    "`curl",
    "`wget",
    "${jndi:",
    "%24%7bjndi"
  ]
} as const;

/** 有错误的条目下标（ChipInput 的 invalid） */
export function invalidIndexes(values: string[], valid: (value: string) => boolean) {
  const out = new Set<number>();
  values.forEach((value, index) => {
    if (!valid(value)) out.add(index);
  });
  return out;
}
