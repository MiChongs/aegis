"use client";

import { useState } from "react";
import {
  Bell,
  CalendarClock,
  FileText,
  Fingerprint,
  Gavel,
  Hourglass,
  KeyRound,
  ListChecks,
  Loader2,
  LogIn,
  LogOut,
  Mail,
  Plus,
  RotateCcw,
  Save,
  ShieldAlert,
  Smartphone,
  Timer,
  Trash2,
  UserX,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import {
  FieldGroup,
  ModeCard,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import {
  GUARD_KEYS,
  GUARD_LEVEL_META,
  GUARD_META,
  LOGIN_BEHAVIOR_META,
  MODE_META,
  VERIFY_METHOD_META
} from "@/components/apps/account-deletion/account-deletion-shared";
import { AutoSkeleton } from "@/components/ui/auto-skeleton";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type {
  DeletionCatalog,
  DeletionConfig,
  DeletionGuardKey,
  DeletionGuardLevel,
  DeletionGuards,
  DeletionLoginBehavior,
  DeletionMode,
  DeletionSettings,
  DeletionSettingsPayload,
  DeletionVerifyMethod
} from "@/lib/api/account-deletion";
import {
  useDeletionCatalogQuery,
  useDeletionSettingsQuery,
  useSaveDeletionSettingsMutation
} from "@/lib/account-deletion-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { cn } from "@/lib/utils";

const REASON_CODE_PATTERN = /^[a-z0-9_-]{1,64}$/;

const VERIFY_ICON: Record<DeletionVerifyMethod, React.ReactNode> = {
  password: <KeyRound className="size-4" />,
  email: <Mail className="size-4" />,
  phone: <Smartphone className="size-4" />
};

const LOGIN_ICON: Record<DeletionLoginBehavior, React.ReactNode> = {
  keep: <LogIn className="size-4" />,
  cancel: <RotateCcw className="size-4" />
};

const LEVEL_ON_CLASS: Record<DeletionGuardLevel, string> = {
  off: "data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm",
  warn: "data-[state=on]:bg-amber-500/15 data-[state=on]:text-amber-700 dark:data-[state=on]:text-amber-300",
  block: "data-[state=on]:bg-red-500/15 data-[state=on]:text-red-700 dark:data-[state=on]:text-red-300"
};

/** 兜底默认值：目录还没到时也能渲染出一份完整的表单（与后端 DefaultConfig 一致） */
const FALLBACK: DeletionConfig = {
  allowImmediate: false,
  allowCooling: true,
  defaultMode: "cooling",
  coolingDays: 15,
  requireReview: false,
  verifyMethods: ["password", "email", "phone"],
  requireTotp: true,
  recentLoginMinutes: 10,
  reasons: [],
  reasonRequired: false,
  allowCustomReason: true,
  noticeTitle: "注销须知",
  noticeContent: "",
  agreementLabel: "",
  supportContact: "",
  guards: {
    activeBan: "block",
    pendingOrders: "block",
    pendingRefunds: "block",
    walletBalance: "warn",
    activeVip: "warn",
    lifetimeVip: "warn",
    points: "off"
  },
  loginDuringCooling: "keep",
  signOutOthers: true,
  remindBeforeHours: 24,
  reregisterDays: 0,
  notifyEmail: true
};

type ReasonDraft = { key: number; code: string; label: string };

type Form = {
  enabled: boolean;
  allowImmediate: boolean;
  allowCooling: boolean;
  defaultMode: DeletionMode;
  coolingDays: string;
  requireReview: boolean;
  verifyMethods: DeletionVerifyMethod[];
  requireTotp: boolean;
  recentLoginMinutes: string;
  reasons: ReasonDraft[];
  reasonRequired: boolean;
  allowCustomReason: boolean;
  noticeTitle: string;
  noticeContent: string;
  agreementLabel: string;
  supportContact: string;
  guards: DeletionGuards;
  loginDuringCooling: DeletionLoginBehavior;
  signOutOthers: boolean;
  remindBeforeHours: string;
  reregisterDays: string;
  notifyEmail: boolean;
};

function seed(settings?: DeletionSettings, defaults?: DeletionConfig): Form {
  const source: DeletionConfig = { ...FALLBACK, ...defaults, ...settings?.config };
  return {
    enabled: Boolean(settings?.enabled),
    allowImmediate: Boolean(source.allowImmediate),
    allowCooling: Boolean(source.allowCooling),
    defaultMode: source.defaultMode,
    coolingDays: String(source.coolingDays ?? 15),
    requireReview: Boolean(source.requireReview),
    verifyMethods: [...(source.verifyMethods ?? [])],
    requireTotp: Boolean(source.requireTotp),
    recentLoginMinutes: String(source.recentLoginMinutes ?? 0),
    reasons: (source.reasons ?? []).map((reason, index) => ({ key: index + 1, code: reason.code, label: reason.label })),
    reasonRequired: Boolean(source.reasonRequired),
    allowCustomReason: Boolean(source.allowCustomReason),
    noticeTitle: source.noticeTitle ?? "",
    noticeContent: source.noticeContent ?? "",
    agreementLabel: source.agreementLabel ?? "",
    supportContact: source.supportContact ?? "",
    guards: { ...FALLBACK.guards, ...source.guards },
    loginDuringCooling: source.loginDuringCooling ?? "keep",
    signOutOthers: Boolean(source.signOutOthers),
    remindBeforeHours: String(source.remindBeforeHours ?? 0),
    reregisterDays: String(source.reregisterDays ?? 0),
    notifyEmail: Boolean(source.notifyEmail)
  };
}

const toInt = (value: string) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

function toPayload(form: Form, methodOrder: DeletionVerifyMethod[]): DeletionSettingsPayload {
  // 只剩一种方式时默认方式就是它（与后端 NormalizeConfig 一致，避免「未保存的修改」误报）
  const defaultMode: DeletionMode =
    form.allowCooling && !form.allowImmediate
      ? "cooling"
      : form.allowImmediate && !form.allowCooling
        ? "immediate"
        : form.defaultMode;
  return {
    enabled: form.enabled,
    config: {
      allowImmediate: form.allowImmediate,
      allowCooling: form.allowCooling,
      defaultMode,
      coolingDays: toInt(form.coolingDays),
      requireReview: form.requireReview,
      verifyMethods: methodOrder.filter((method) => form.verifyMethods.includes(method)),
      requireTotp: form.requireTotp,
      recentLoginMinutes: toInt(form.recentLoginMinutes),
      reasons: form.reasons.map((reason) => ({
        code: reason.code.trim().toLowerCase(),
        label: reason.label.trim()
      })),
      reasonRequired: form.reasonRequired,
      allowCustomReason: form.allowCustomReason,
      noticeTitle: form.noticeTitle.trim(),
      noticeContent: form.noticeContent.replace(/\r\n/g, "\n").trim(),
      agreementLabel: form.agreementLabel.trim(),
      supportContact: form.supportContact.trim(),
      guards: form.guards,
      loginDuringCooling: form.loginDuringCooling,
      signOutOthers: form.signOutOthers,
      remindBeforeHours: toInt(form.remindBeforeHours),
      reregisterDays: toInt(form.reregisterDays),
      notifyEmail: form.notifyEmail
    }
  };
}

/** 加载期间喂给骨架的占位表单 */
const PLACEHOLDER_FORM = seed(undefined, {
  ...FALLBACK,
  reasons: [
    { code: "no_longer_used", label: "不再使用" },
    { code: "privacy", label: "担心隐私安全" },
    { code: "other", label: "其他原因" }
  ],
  noticeContent: "注销须知正文".repeat(12),
  agreementLabel: "我已阅读并同意注销须知"
});

/**
 * 配置：开关与全部配置一起保存（PUT /account-deletion/settings）。
 *
 * 枚举、默认值与上限来自目录（`/account-deletion/catalog`），目录没有下发的几项上限在本文件顶部；
 * 校验以后端为准，失败时原因原样 toast。草稿按应用绑定，无草稿即从服务端派生。
 * 没保存过时后端回的就是默认配置，界面上标「未配置」。
 */
export function DeletionConfigPanel({ appKey }: { appKey: string }) {
  const can = useAppKeyPermissionChecker(appKey);
  const canWrite = can("app:user:deletion:settings");
  const catalogQuery = useDeletionCatalogQuery(appKey);
  const settingsQuery = useDeletionSettingsQuery(appKey);
  const saveMutation = useSaveDeletionSettingsMutation(appKey);
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);

  const catalog: DeletionCatalog | undefined = catalogQuery.data;
  const settings = settingsQuery.data;
  const limits = catalog?.limits ?? {};
  const methodOrder = catalog?.verifyMethods?.length
    ? catalog.verifyMethods
    : (["password", "email", "phone"] as DeletionVerifyMethod[]);
  const guardKeys = catalog?.guardKeys?.length ? catalog.guardKeys : GUARD_KEYS;
  const guardLevels = catalog?.guardLevels?.length
    ? catalog.guardLevels
    : (["off", "warn", "block"] as DeletionGuardLevel[]);
  const loginBehaviors = catalog?.loginBehaviors?.length
    ? catalog.loginBehaviors
    : (["keep", "cancel"] as DeletionLoginBehavior[]);
  const modes = catalog?.modes?.length ? catalog.modes : (["cooling", "immediate"] as DeletionMode[]);
  const minCooling = limits.minCoolingDays ?? 1;
  const maxCooling = limits.maxCoolingDays ?? 180;
  const maxRecent = limits.maxRecentLoginMinutes ?? 1440;
  const maxRemind = limits.maxRemindBeforeHours ?? 720;
  const maxReregister = limits.maxReregisterDays ?? 3650;
  const maxReasons = limits.maxReasons ?? 20;
  const maxNotice = limits.maxNoticeRunes ?? 8000;
  // 字数上限由 catalog.limits 下发；目录还没到时用与后端一致的默认值
  const maxReasonCode = limits.maxReasonCodeRunes ?? 64;
  const maxReasonLabel = limits.maxReasonLabelRunes ?? 60;
  const maxNoticeTitle = limits.maxNoticeTitleRunes ?? 60;
  const maxAgreement = limits.maxAgreementRunes ?? 120;
  const maxSupport = limits.maxSupportRunes ?? 200;

  const loading = settingsQuery.isLoading || catalogQuery.isLoading;
  const saved = loading ? PLACEHOLDER_FORM : seed(settings, catalog?.defaults);
  const form = draft?.scope === appKey ? draft.value : saved;
  const dirty =
    !loading && JSON.stringify(toPayload(form, methodOrder)) !== JSON.stringify(toPayload(saved, methodOrder));
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope: appKey, value: { ...form, [key]: value } });

  const noMode = !form.allowCooling && !form.allowImmediate;
  const bothModes = form.allowCooling && form.allowImmediate;
  const noVerify = form.verifyMethods.length === 0 && toInt(form.recentLoginMinutes) === 0;
  const reasonCodes = form.reasons.map((reason) => reason.code.trim().toLowerCase());
  const reasonUnreachable = form.reasonRequired && form.reasons.length === 0 && !form.allowCustomReason;

  const toggleMethod = (method: DeletionVerifyMethod, on: boolean) =>
    patch(
      "verifyMethods",
      on ? [...form.verifyMethods.filter((item) => item !== method), method] : form.verifyMethods.filter((item) => item !== method)
    );

  const patchReason = (key: number, next: Partial<ReasonDraft>) =>
    patch(
      "reasons",
      form.reasons.map((reason) => (reason.key === key ? { ...reason, ...next } : reason))
    );

  const addReason = () => {
    const key = form.reasons.reduce((max, reason) => Math.max(max, reason.key), 0) + 1;
    let index = form.reasons.length + 1;
    while (reasonCodes.includes(`reason_${index}`)) index += 1;
    patch("reasons", [...form.reasons, { key, code: `reason_${index}`, label: "" }]);
  };

  const save = async () => {
    try {
      await saveMutation.mutateAsync(toPayload(form, methodOrder));
      setDraft(null);
      toast.success("注销配置已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <AutoSkeleton loading={loading}>
      <div className="space-y-4">
        <SectionCard
          icon={<UserX className="size-4" />}
          title="账号注销"
          aside={
            <div className="flex items-center gap-2">
              {settings && !settings.configured ? (
                <Badge variant="outline" size="sm">
                  未配置
                </Badge>
              ) : null}
              <StatusDot active={Boolean(settings?.enabled)} labelActive="已开启" labelInactive="未开启" />
            </div>
          }
        >
          <SwitchRow
            label="允许用户在 App 内注销账号"
            icon={<UserX className="size-4" />}
            checked={form.enabled}
            onChange={(value) => patch("enabled", value)}
          />
        </SectionCard>

        <SectionCard icon={<Hourglass className="size-4" />} title="注销方式">
          <div className="space-y-5">
            <div className="grid gap-2 sm:grid-cols-2">
              <SwitchRow
                label={MODE_META.cooling.label}
                hint={MODE_META.cooling.description}
                icon={<Hourglass className="size-4" />}
                checked={form.allowCooling}
                onChange={(value) => patch("allowCooling", value)}
              />
              <SwitchRow
                label={MODE_META.immediate.label}
                hint={MODE_META.immediate.description}
                icon={<Zap className="size-4" />}
                checked={form.allowImmediate}
                onChange={(value) => patch("allowImmediate", value)}
              />
            </div>
            {noMode ? <Warning>至少开放一种注销方式</Warning> : null}

            {bothModes ? (
              <FieldGroup label="默认方式">
                <RadioGroup value={form.defaultMode} onValueChange={(value) => patch("defaultMode", value as DeletionMode)}>
                  <div className="grid gap-2 sm:grid-cols-2">
                    {modes.map((mode) => (
                      <ModeCard
                        key={mode}
                        value={mode}
                        active={form.defaultMode === mode}
                        title={MODE_META[mode]?.label ?? mode}
                        description={mode === "cooling" ? "App 上默认选中冷静期注销" : "App 上默认选中立即注销"}
                        icon={mode === "cooling" ? <Hourglass className="size-4" /> : <Zap className="size-4" />}
                      />
                    ))}
                  </div>
                </RadioGroup>
              </FieldGroup>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-3">
              <NumberField
                label="冷静期"
                value={form.coolingDays}
                onChange={(value) => patch("coolingDays", value)}
                min={minCooling}
                max={maxCooling}
                unit={`天，${minCooling}–${maxCooling}`}
                disabled={!form.allowCooling}
              />
            </div>

            <SwitchRow
              label="提交后需人工审核"
              icon={<Gavel className="size-4" />}
              checked={form.requireReview}
              onChange={(value) => patch("requireReview", value)}
            />
          </div>
        </SectionCard>

        <SectionCard icon={<Fingerprint className="size-4" />} title="身份验证">
          <div className="space-y-5">
            <FieldGroup label="验证方式" hint="任选其一">
              <div className="grid gap-2 sm:grid-cols-3">
                {methodOrder.map((method) => (
                  <SwitchRow
                    key={method}
                    label={VERIFY_METHOD_META[method]?.label ?? method}
                    icon={VERIFY_ICON[method]}
                    checked={form.verifyMethods.includes(method)}
                    onChange={(value) => toggleMethod(method, value)}
                  />
                ))}
              </div>
            </FieldGroup>
            <SwitchRow
              label="开启两步验证的账号需再验证动态码"
              hint="可用恢复码代替"
              icon={<ShieldAlert className="size-4" />}
              checked={form.requireTotp}
              onChange={(value) => patch("requireTotp", value)}
            />
            <div className="grid gap-4 sm:grid-cols-3">
              <NumberField
                label="无可用验证方式时凭最近登录放行"
                value={form.recentLoginMinutes}
                onChange={(value) => patch("recentLoginMinutes", value)}
                min={0}
                max={maxRecent}
                unit={`分钟，0 为不放行`}
              />
            </div>
            {noVerify ? <Warning>至少选择一种验证方式，或允许凭最近登录放行</Warning> : null}
          </div>
        </SectionCard>

        <SectionCard icon={<ListChecks className="size-4" />} title="注销原因">
          <div className="space-y-4">
            <FieldGroup label="原因选项" hint={`${form.reasons.length}/${maxReasons}`}>
              <div className="space-y-2">
                {form.reasons.length === 0 ? (
                  <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                    暂无原因选项
                  </p>
                ) : (
                  <div className="hidden grid-cols-[minmax(0,2fr)_minmax(0,3fr)_2.25rem] gap-2 px-1 text-[11px] text-muted-foreground sm:grid">
                    <span>标识</span>
                    <span>名称</span>
                    <span />
                  </div>
                )}
                {form.reasons.map((reason, index) => {
                  const code = reasonCodes[index];
                  const codeInvalid = !REASON_CODE_PATTERN.test(code) || reasonCodes.indexOf(code) !== index;
                  const labelInvalid = !reason.label.trim();
                  return (
                    <div
                      key={reason.key}
                      className="grid grid-cols-[minmax(0,1fr)_2.25rem] gap-2 rounded-xl bg-muted/50 p-2 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)_2.25rem] sm:bg-transparent sm:p-0"
                    >
                      <Input
                        value={reason.code}
                        maxLength={maxReasonCode}
                        placeholder="标识"
                        aria-label="原因标识"
                        aria-invalid={codeInvalid}
                        onChange={(event) => patchReason(reason.key, { code: event.target.value.toLowerCase() })}
                        className="col-start-1 row-start-2 bg-background font-mono text-xs sm:row-start-1"
                      />
                      <Input
                        value={reason.label}
                        maxLength={maxReasonLabel}
                        placeholder="名称"
                        aria-label="原因名称"
                        aria-invalid={labelInvalid}
                        onChange={(event) => patchReason(reason.key, { label: event.target.value })}
                        className="col-start-1 row-start-1 bg-background text-xs sm:col-start-2"
                      />
                      <Button
                        size="icon"
                        variant="ghost"
                        className="col-start-2 row-span-2 row-start-1 size-9 self-center sm:col-start-3 sm:row-span-1"
                        aria-label="移除原因"
                        onClick={() => patch("reasons", form.reasons.filter((item) => item.key !== reason.key))}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  );
                })}
                {form.reasons.some(
                  (reason, index) =>
                    !REASON_CODE_PATTERN.test(reasonCodes[index]) || reasonCodes.indexOf(reasonCodes[index]) !== index
                ) ? (
                  <Warning>标识只能由小写字母、数字、下划线与连字符组成，且不能重复</Warning>
                ) : null}
                {form.reasons.length < maxReasons ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
                    onClick={addReason}
                  >
                    <Plus className="size-3.5" /> 添加原因
                  </Button>
                ) : null}
              </div>
            </FieldGroup>
            <div className="grid gap-2 sm:grid-cols-2">
              <SwitchRow
                label="必须填写原因"
                checked={form.reasonRequired}
                onChange={(value) => patch("reasonRequired", value)}
              />
              <SwitchRow
                label="允许用户自填原因"
                hint={`最多 ${limits.maxReasonTextRunes ?? 500} 字`}
                checked={form.allowCustomReason}
                onChange={(value) => patch("allowCustomReason", value)}
              />
            </div>
            {reasonUnreachable ? <Warning>必须填写原因时，至少提供一个原因选项或允许自填</Warning> : null}
          </div>
        </SectionCard>

        <SectionCard icon={<FileText className="size-4" />} title="注销须知">
          <div className="space-y-5">
            <TextField
              label="标题"
              value={form.noticeTitle}
              maxLength={maxNoticeTitle}
              onChange={(value) => patch("noticeTitle", value)}
            />
            <FieldGroup label="正文" hint={`${form.noticeContent.length}/${maxNotice}`}>
              <Textarea
                value={form.noticeContent}
                rows={10}
                maxLength={maxNotice}
                onChange={(event) => patch("noticeContent", event.target.value)}
                className="text-sm leading-6"
              />
              {catalog?.defaults && form.noticeContent.trim() !== catalog.defaults.noticeContent.trim() ? (
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 text-xs text-muted-foreground"
                  onClick={() => patch("noticeContent", catalog.defaults.noticeContent)}
                >
                  <RotateCcw className="size-3.5" /> 恢复默认正文
                </Button>
              ) : null}
            </FieldGroup>
            <div className="grid gap-4 sm:grid-cols-2">
              <TextField
                label="同意勾选文案"
                value={form.agreementLabel}
                maxLength={maxAgreement}
                onChange={(value) => patch("agreementLabel", value)}
              />
              <TextField
                label="客服联系方式"
                value={form.supportContact}
                maxLength={maxSupport}
                placeholder="不设置"
                onChange={(value) => patch("supportContact", value)}
              />
            </div>
          </div>
        </SectionCard>

        <SectionCard icon={<ShieldAlert className="size-4" />} title="前置条件">
          <ul className="divide-y divide-border overflow-hidden rounded-xl border">
            {guardKeys.map((key: DeletionGuardKey) => {
              const meta = GUARD_META[key];
              const level = form.guards[key] ?? "off";
              return (
                <li key={key} className="flex flex-col gap-2.5 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0">
                    <div className="text-sm font-medium">{meta?.label ?? key}</div>
                    <div className="text-xs text-muted-foreground">{meta?.description}</div>
                  </div>
                  <ToggleGroup
                    type="single"
                    value={level}
                    onValueChange={(value) =>
                      value && patch("guards", { ...form.guards, [key]: value as DeletionGuardLevel })
                    }
                    className="shrink-0 self-start rounded-lg border bg-muted/40 p-0.5 sm:self-auto"
                  >
                    {guardLevels.map((option) => (
                      <ToggleGroupItem
                        key={option}
                        value={option}
                        className={cn("h-7 rounded-md px-3 text-xs", LEVEL_ON_CLASS[option])}
                      >
                        {GUARD_LEVEL_META[option]?.label ?? option}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                </li>
              );
            })}
          </ul>
          <p className="mt-2.5 text-[11px] text-muted-foreground">阻止：不满足时不能提交；提示：仅在注销页提醒</p>
        </SectionCard>

        <SectionCard icon={<CalendarClock className="size-4" />} title="冷静期与通知">
          <div className="space-y-5">
            <FieldGroup label="冷静期内登录">
              <RadioGroup
                value={form.loginDuringCooling}
                onValueChange={(value) => patch("loginDuringCooling", value as DeletionLoginBehavior)}
              >
                <div className="grid gap-2 sm:grid-cols-2">
                  {loginBehaviors.map((behavior) => (
                    <ModeCard
                      key={behavior}
                      value={behavior}
                      active={form.loginDuringCooling === behavior}
                      title={LOGIN_BEHAVIOR_META[behavior]?.label ?? behavior}
                      description={LOGIN_BEHAVIOR_META[behavior]?.description ?? ""}
                      icon={LOGIN_ICON[behavior] ?? <LogIn className="size-4" />}
                    />
                  ))}
                </div>
              </RadioGroup>
            </FieldGroup>
            <SwitchRow
              label="提交后退出其他设备的登录"
              icon={<LogOut className="size-4" />}
              checked={form.signOutOthers}
              onChange={(value) => patch("signOutOthers", value)}
            />
            <div className="grid gap-4 sm:grid-cols-3">
              <NumberField
                label="到期前提醒"
                value={form.remindBeforeHours}
                onChange={(value) => patch("remindBeforeHours", value)}
                min={0}
                max={maxRemind}
                unit="小时，0 为不提醒"
              />
            </div>
            <SwitchRow
              label="向绑定邮箱发送进度通知"
              icon={<Bell className="size-4" />}
              checked={form.notifyEmail}
              onChange={(value) => patch("notifyEmail", value)}
            />
          </div>
        </SectionCard>

        <SectionCard icon={<Timer className="size-4" />} title="重新注册限制">
          <div className="grid gap-4 sm:grid-cols-3">
            <NumberField
              label="注销后限制重新注册"
              value={form.reregisterDays}
              onChange={(value) => patch("reregisterDays", value)}
              min={0}
              max={maxReregister}
              unit="天，0 为不限制"
              hint="同一账号名、邮箱、手机号"
            />
          </div>
        </SectionCard>

        <div className="sticky bottom-3 z-10 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-card px-4 py-3">
          <span className="text-xs text-muted-foreground">
            {!canWrite
              ? "没有修改权限"
              : settings?.updatedAt
                ? `${settings.updatedBy ? `${settings.updatedBy} ` : ""}保存于 ${formatDateTimeMinute(settings.updatedAt)}`
                : "尚未保存"}
          </span>
          {canWrite ? (
            <div className="flex items-center gap-2">
              {dirty ? (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 gap-1.5"
                  disabled={saveMutation.isPending}
                  onClick={() => setDraft(null)}
                >
                  <RotateCcw className="size-3.5" />
                  放弃修改
                </Button>
              ) : null}
              <Button
                size="sm"
                className="h-8 gap-1.5"
                disabled={!dirty || saveMutation.isPending}
                onClick={() => void save()}
              >
                {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                保存
              </Button>
            </div>
          ) : null}
        </div>
      </div>
    </AutoSkeleton>
  );
}

function Warning({ children }: { children: React.ReactNode }) {
  return <p className="text-xs text-amber-600 dark:text-amber-400">{children}</p>;
}

function TextField({
  label,
  value,
  onChange,
  maxLength,
  placeholder
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  maxLength?: number;
  placeholder?: string;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {maxLength ? (
          <span className="text-[10px] text-muted-foreground tabular-nums">
            {value.length}/{maxLength}
          </span>
        ) : null}
      </div>
      <Input
        value={value}
        maxLength={maxLength}
        placeholder={placeholder}
        className="text-xs"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}
