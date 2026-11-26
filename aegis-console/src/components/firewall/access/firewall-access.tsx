"use client";

import { useState, type ReactNode } from "react";
import { Fingerprint, Network, Ruler, ShieldAlert, ShieldBan, Timer, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
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
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ChipInput, FieldError, NumberField, Panel, SettingRow, TabStack } from "@/components/traffic/traffic-kit";
import { useIPBanModesQuery } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import { FIREWALL_DEFAULTS, Hint, ReadOnlyNotice, SaveBar, SettingsMeta, invalidIndexes, useSettingsDraft, type SettingsForm, type SettingsKey, type SettingsSetter } from "../waf/settings-draft";
import { TuningField } from "../waf/waf-tuning";
import {
  RATE_UNITS,
  describeRate,
  formatRate,
  isIPOrCIDR,
  isRedirectURL,
  isValidRate,
  normalizeIP,
  parseRate,
  type RateUnit
} from "./access-validators";

/**
 * 防火墙 → 访问控制（入口段，docs/firewall.md §2）：解密之前只看连接、请求行与请求头的那些检查。
 * 顺序：白 / 黑名单 → 封禁 → 可信网段 → 方法与长度 → 扫描器 UA → 探测路径与快速签名 → 自定义规则 → 限流。
 *
 * 白名单是这一页最危险的字段：非空即进入「只放行这些」模式，管理员自己也在其中。
 * 所以从空变为非空、或改动非空白名单时，保存前再确认一次。
 */

const ACCESS_KEYS = [
  "allowedCIDRs",
  "blockedCIDRs",
  "trustedCIDRs",
  "globalRate",
  "authRate",
  "adminRate",
  "maxPathLength",
  "maxQueryLength",
  "blockedUserAgents",
  "blockedPathPrefix",
  "blockedSignatures",
  "defaultBanMode",
  "tarpitDelayMs",
  "banRedirectUrl"
] as const satisfies readonly SettingsKey[];

function accessValidation(form: SettingsForm): string | null {
  if (form.allowedCIDRs.some((value) => !isIPOrCIDR(value))) return "白名单里有无效的 IP 或网段";
  if (form.blockedCIDRs.some((value) => !isIPOrCIDR(value))) return "黑名单里有无效的 IP 或网段";
  if (form.trustedCIDRs.some((value) => !isIPOrCIDR(value))) return "可信网段里有无效的 IP 或网段";
  if (!isValidRate(form.globalRate) || !isValidRate(form.authRate) || !isValidRate(form.adminRate)) return "限流次数必须大于 0";
  if (form.maxPathLength <= 0 || form.maxQueryLength <= 0) return "长度上限必须大于 0";
  if (form.tarpitDelayMs < 0 || form.tarpitDelayMs > 30000) return "拖延时长应在 0–30000 毫秒之间";
  if (!isRedirectURL(form.banRedirectUrl)) return "重定向地址应为站内路径或 http(s) 地址";
  return null;
}

export function FirewallAccessTab() {
  const draft = useSettingsDraft(ACCESS_KEYS);
  const [confirmAllowlist, setConfirmAllowlist] = useState(false);
  const { form, base, settings, set, canWrite } = draft;

  if (!form || !base || !settings) {
    if (draft.query.isError) {
      return <div className="rounded-2xl border border-border bg-card py-16 text-center text-sm text-muted-foreground">设置加载失败</div>;
    }
    return (
      <TabStack>
        <Skeleton className="h-12 rounded-xl" />
        <Skeleton className="h-96 rounded-2xl" />
        <div className="grid gap-4 @4xl:grid-cols-2">
          <Skeleton className="h-56 rounded-2xl" />
          <Skeleton className="h-56 rounded-2xl" />
        </div>
      </TabStack>
    );
  }

  const blockedReason = accessValidation(form);
  const allowlistChanged = draft.changed.includes("allowedCIDRs") && form.allowedCIDRs.length > 0;

  const save = () => {
    if (allowlistChanged) {
      setConfirmAllowlist(true);
      return;
    }
    draft.submit();
  };

  return (
    <TabStack>
      <SettingsMeta settings={settings} />
      {!canWrite ? <ReadOnlyNotice /> : null}

      <fieldset disabled={!canWrite || draft.pending} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
        <IPListsPanel form={form} set={set} />
        <RatePanel form={form} set={set} />
        <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
          <LengthPanel form={form} set={set} />
          <BanResponsePanel form={form} set={set} />
        </div>
        <SignaturePanel form={form} set={set} />
      </fieldset>

      {canWrite ? <SaveBar count={draft.changed.length} pending={draft.pending} blockedReason={blockedReason} onDiscard={draft.discard} onSave={save} /> : null}

      <AlertDialog open={confirmAllowlist} onOpenChange={setConfirmAllowlist}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>启用白名单模式？</AlertDialogTitle>
            <AlertDialogDescription>
              保存后只有下列 {form.allowedCIDRs.length} 个地址或网段能访问整个服务，包括控制台。请确认你当前的出口 IP 在其中。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex max-h-40 flex-wrap gap-1.5 overflow-y-auto rounded-lg border border-border p-2">
            {form.allowedCIDRs.map((item) => (
              <span key={item} className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs">
                {item}
              </span>
            ))}
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => draft.submit()}>
              保存并生效
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

/* ── IP 名单 ── */

function IPListsPanel({ form, set }: { form: SettingsForm; set: SettingsSetter }) {
  const allowlistOn = form.allowedCIDRs.length > 0;
  return (
    <Panel icon={Network} tone="primary" title="IP 名单">
      <div className="flex min-w-0 flex-col gap-5">
        <div className={cn("flex min-w-0 flex-col gap-2 rounded-xl border p-3", allowlistOn ? "border-rose-500/50 bg-rose-500/5" : "border-rose-500/25")}>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <TriangleAlert className="size-4 shrink-0 text-rose-600 dark:text-rose-400" />
            <span className="text-sm font-medium">白名单</span>
            <span className="text-xs text-muted-foreground tabular-nums">{form.allowedCIDRs.length} 条</span>
            {allowlistOn ? (
              <Badge variant="danger" size="sm">
                白名单模式
              </Badge>
            ) : (
              <Badge variant="outline" size="sm">
                未启用
              </Badge>
            )}
          </div>
          <Hint tone="danger">
            非空时只放行这些地址，其余一律拒绝（40391），控制台与管理员也不例外。填错一条就可能把所有人挡在外面，包括你自己。
          </Hint>
          <IPListInput values={form.allowedCIDRs} onChange={(next) => set("allowedCIDRs", next)} label="白名单" placeholder="203.0.113.0/24" />
        </div>

        <IPListBlock
          title="黑名单网段"
          hint="命中直接拒绝（40391），排在封禁检查之前"
          values={form.blockedCIDRs}
          onChange={(next) => set("blockedCIDRs", next)}
          placeholder="198.51.100.0/24"
        />
        <IPListBlock
          title="可信网段"
          hint="跳过签名、自定义规则、限流与 WAF，封禁仍生效。用于监控探针、办公出口；与白名单是两回事"
          values={form.trustedCIDRs}
          onChange={(next) => set("trustedCIDRs", next)}
          placeholder="10.0.0.0/8"
        />
      </div>
    </Panel>
  );
}

function IPListBlock({
  title,
  hint,
  values,
  onChange,
  placeholder
}: {
  title: string;
  hint: ReactNode;
  values: string[];
  onChange: (next: string[]) => void;
  placeholder: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-xs text-muted-foreground tabular-nums">{values.length} 条</span>
      </div>
      <Hint>{hint}</Hint>
      <IPListInput values={values} onChange={onChange} label={title} placeholder={placeholder} />
    </div>
  );
}

/** 只接受 IPv4 / IPv6 / CIDR：新加入的条目先校验，不合法的不进列表 */
function IPListInput({ values, onChange, label, placeholder }: { values: string[]; onChange: (next: string[]) => void; label: string; placeholder: string }) {
  const invalid = invalidIndexes(values, isIPOrCIDR);
  const handle = (next: string[]) => {
    const existing = new Set(values);
    const rejected = next.filter((item) => !existing.has(item) && !isIPOrCIDR(item));
    if (rejected.length) {
      toast.error(`不是有效的 IP 或网段：${rejected.slice(0, 3).join("、")}${rejected.length > 3 ? " 等" : ""}`);
    }
    onChange(next.filter((item) => existing.has(item) || isIPOrCIDR(item)));
  };
  return (
    <>
      <ChipInput value={values} onChange={handle} placeholder={placeholder} invalid={invalid} normalize={normalizeIP} ariaLabel={label} />
      {invalid.size ? <FieldError message="标红的条目不是有效的 IP 或网段" /> : null}
    </>
  );
}

/* ── 限流 ── */

const RATES: { key: "globalRate" | "authRate" | "adminRate"; title: string; scope: string }[] = [
  { key: "globalRate", title: "全局", scope: "所有请求" },
  { key: "authRate", title: "认证", scope: "/api/auth/、发送邮件验证码、接入网关的 /auth/ 路径" },
  { key: "adminRate", title: "管理端", scope: "/api/admin/ 与少量管理类应用接口" }
];

function RatePanel({ form, set }: { form: SettingsForm; set: SettingsSetter }) {
  return (
    <Panel icon={Timer} tone="amber" title="限流" bodyClassName="pb-2 sm:pb-3">
      <Hint>按来源 IP 计数，各副本共用 Redis 计数。一个请求可能同时计入全局与认证 / 管理端两项，任一项超出即返回 429。</Hint>
      <div className="flex flex-col divide-y divide-border">
        {RATES.map((item) => (
          <RateRow key={item.key} title={item.title} scope={item.scope} value={form[item.key]} onChange={(next) => set(item.key, next)} />
        ))}
      </div>
    </Panel>
  );
}

function RateRow({ title, scope, value, onChange }: { title: string; scope: string; value: string; onChange: (value: string) => void }) {
  const parsed = parseRate(value) ?? { count: 0, unit: "M" as RateUnit };
  const valid = isValidRate(value);
  return (
    <div className="flex min-w-0 flex-col gap-2 py-3 @2xl:flex-row @2xl:items-center @2xl:gap-4">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <span className="text-sm">{title}</span>
          <span className={cn("text-xs tabular-nums", valid ? "text-muted-foreground" : "text-rose-600 dark:text-rose-400")}>{describeRate(value)}</span>
        </div>
        <div className="truncate text-[11px] text-muted-foreground" title={scope}>
          {scope}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        <NumberField
          value={parsed.count}
          min={1}
          max={10_000_000}
          onChange={(count) => onChange(formatRate(count, parsed.unit))}
          className="w-28"
          ariaLabel={`${title}限流次数`}
          invalid={!valid}
        />
        <span className="text-xs text-muted-foreground">次 /</span>
        <Select value={parsed.unit} onValueChange={(unit) => onChange(formatRate(parsed.count, unit as RateUnit))}>
          <SelectTrigger size="sm" className="h-8 w-[84px]" aria-label={`${title}限流周期`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {RATE_UNITS.map((unit) => (
              <SelectItem key={unit.value} value={unit.value}>
                {unit.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="hidden w-16 font-mono text-[11px] text-muted-foreground @2xl:inline">{value}</span>
      </div>
    </div>
  );
}

/* ── 请求长度 ── */

function LengthPanel({ form, set }: { form: SettingsForm; set: SettingsSetter }) {
  return (
    <Panel icon={Ruler} tone="sky" title="请求长度" bodyClassName="pb-2 sm:pb-3">
      <div className="flex flex-col divide-y divide-border">
        <SettingRow title="路径长度上限" value={`${form.maxPathLength.toLocaleString("zh-CN")} 字节`}>
          <NumberField
            value={form.maxPathLength}
            min={1}
            max={65536}
            step={256}
            suffix="字节"
            onChange={(value) => set("maxPathLength", value)}
            className="w-32"
            ariaLabel="路径长度上限"
            invalid={form.maxPathLength <= 0}
          />
        </SettingRow>
        <SettingRow title="查询串长度上限" value={`${form.maxQueryLength.toLocaleString("zh-CN")} 字节`}>
          <NumberField
            value={form.maxQueryLength}
            min={1}
            max={65536}
            step={256}
            suffix="字节"
            onChange={(value) => set("maxQueryLength", value)}
            className="w-32"
            ariaLabel="查询串长度上限"
            invalid={form.maxQueryLength <= 0}
          />
        </SettingRow>
      </div>
      <Hint className="pt-1 pb-2">加密 GET 的查询串（_payload 密文）不受查询串长度限制。</Hint>
    </Panel>
  );
}

/* ── 封禁响应默认值 ── */

function BanResponsePanel({ form, set }: { form: SettingsForm; set: SettingsSetter }) {
  const modesQuery = useIPBanModesQuery();
  const modes = modesQuery.data?.modes ?? [];
  const current = modes.find((mode) => mode.value === form.defaultBanMode);
  const redirectValid = isRedirectURL(form.banRedirectUrl);

  return (
    <Panel icon={ShieldBan} tone="rose" title="封禁响应" bodyClassName="pb-2 sm:pb-3">
      <div className="flex flex-col divide-y divide-border">
        <SettingRow title="默认响应方式" stacked>
          <Select value={form.defaultBanMode || "forbidden"} onValueChange={(value) => set("defaultBanMode", value)}>
            <SelectTrigger size="sm" className="w-full" aria-label="默认响应方式">
              <SelectValue placeholder={modesQuery.isLoading ? "加载中" : "选择响应方式"} />
            </SelectTrigger>
            <SelectContent className="max-h-80">
              {modes.length ? (
                modes.map((mode) => (
                  <SelectItem key={mode.value} value={mode.value} title={mode.description}>
                    {mode.label}
                  </SelectItem>
                ))
              ) : (
                <SelectItem value={form.defaultBanMode || "forbidden"}>{form.defaultBanMode || "forbidden"}</SelectItem>
              )}
            </SelectContent>
          </Select>
          {current?.description ? <Hint className="pt-1.5">{current.description}</Hint> : null}
        </SettingRow>
        <SettingRow title="拖延时长" value={`${(form.tarpitDelayMs / 1000).toLocaleString("zh-CN")} 秒`}>
          <NumberField
            value={form.tarpitDelayMs}
            min={0}
            max={30000}
            step={500}
            suffix="毫秒"
            onChange={(value) => set("tarpitDelayMs", value)}
            className="w-32"
            ariaLabel="拖延时长"
            invalid={form.tarpitDelayMs < 0 || form.tarpitDelayMs > 30000}
          />
        </SettingRow>
        <SettingRow title="重定向地址" stacked error={redirectValid ? undefined : "应为站内路径或 http(s) 地址"}>
          <Input
            value={form.banRedirectUrl}
            onChange={(event) => set("banRedirectUrl", event.target.value)}
            placeholder="/"
            className="h-8 font-mono text-xs"
            aria-label="重定向地址"
            aria-invalid={!redirectValid || undefined}
          />
        </SettingRow>
      </div>
      <Hint className="pt-1 pb-2">封禁条目没有单独指定方式时使用默认方式；拖延时长用于「拖延响应」，重定向地址用于「重定向」。</Hint>
    </Panel>
  );
}

/* ── 特征拦截 ── */

function SignaturePanel({ form, set }: { form: SettingsForm; set: SettingsSetter }) {
  const lower = (raw: string) => raw.trim().toLowerCase();
  return (
    <Panel icon={Fingerprint} tone="violet" title="特征拦截">
      <div className="grid min-w-0 gap-x-6 gap-y-5 @4xl:grid-cols-2">
        <TuningField
          title="扫描器 UA 片段"
          hint="User-Agent 小写后包含任一片段即拒绝（40394）"
          values={form.blockedUserAgents}
          defaults={FIREWALL_DEFAULTS.blockedUserAgents}
          onChange={(next) => set("blockedUserAgents", next)}
          placeholder="sqlmap"
          normalize={lower}
          clearToDefaults
        />
        <TuningField
          title="探测路径前缀"
          hint="路径小写后以任一前缀开头即拒绝（40395）"
          values={form.blockedPathPrefix}
          defaults={FIREWALL_DEFAULTS.blockedPathPrefix}
          onChange={(next) => set("blockedPathPrefix", next)}
          valid={(value) => value.startsWith("/")}
          error="路径前缀必须以 / 开头"
          placeholder="/.git"
          normalize={lower}
          clearToDefaults
        />
        <div className="@4xl:col-span-2">
          <TuningField
            title="快速签名"
            hint={
              <>
                路径与原始查询串小写后做子串匹配，命中即拒绝（40395）。在解密之前执行，对加密查询串不生效；
                解密后的载荷由 WAF 检查。
              </>
            }
            values={form.blockedSignatures}
            defaults={FIREWALL_DEFAULTS.blockedSignatures}
            onChange={(next) => set("blockedSignatures", next)}
            placeholder="union+select"
            normalize={lower}
            aside={
              <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
                <ShieldAlert className="size-3" />
                入口段
              </span>
            }
          />
        </div>
      </div>
    </Panel>
  );
}
