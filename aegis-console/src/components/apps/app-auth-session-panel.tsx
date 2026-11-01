"use client";

import { useState } from "react";
import Link from "next/link";
import {
  ArrowRight,
  Fingerprint,
  Globe,
  Link2Off,
  MapPin,
  MonitorSmartphone,
  Save,
  Search,
  Shield,
  UserPlus
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import {
  useAdminAppLoginBaselineQuery,
  useAdminAppPolicyQuery,
  useResetAdminAppLoginBaselineMutation,
  useUpdateAdminAppPolicyMutation
} from "@/lib/admin-hooks";
import {
  FieldGroup,
  NoAppSelected,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import { OVERFLOW_META } from "@/components/apps/device-limit/device-limit-shared";
import { appSectionHref } from "@/lib/app-sections";
import { useDeviceLimitPolicyQuery } from "@/lib/device-limit-hooks";
import { useRegIPPolicyQuery } from "@/lib/register-ip-hooks";

type PolicyForm = {
  loginCheckDevice: boolean;
  /** 换绑冷却秒数，字符串态以支持编辑中的空串 */
  deviceRebindInterval: string;
  loginCheckIp: boolean;
  loginCheckUser: boolean;
  registerCheckIp: boolean;
};

const defaultForm: PolicyForm = {
  loginCheckDevice: false,
  deviceRebindInterval: "0",
  loginCheckIp: false,
  loginCheckUser: false,
  registerCheckIp: false
};

function seed(data?: Record<string, unknown> | null): PolicyForm {
  if (!data) return defaultForm;
  return {
    loginCheckDevice: Boolean(data.loginCheckDevice),
    deviceRebindInterval: String(Number(data.loginCheckDeviceTimeOut) || 0),
    loginCheckIp: Boolean(data.loginCheckIp),
    loginCheckUser: Boolean(data.loginCheckUser),
    registerCheckIp: Boolean(data.registerCheckIp)
  };
}

/** 秒数转人类可读，用于换绑冷却的即时反馈 */
function humanizeSeconds(seconds: number): string {
  if (seconds <= 0) return "不限制换绑";
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`;
  if (seconds < 86400) return `${(seconds / 3600).toFixed(seconds % 3600 === 0 ? 0 : 1)} 小时`;
  return `${(seconds / 86400).toFixed(seconds % 86400 === 0 ? 0 : 1)} 天`;
}

export function AppAuthSessionPanel({ appKey }: { appKey?: string | null }) {
  const policyQuery = useAdminAppPolicyQuery(appKey);
  const mutation = useUpdateAdminAppPolicyMutation(appKey);

  // 草稿按 appKey 绑定作用域：没有本应用的草稿时直接从服务端数据派生。
  // 这样既不需要 useEffect 同步（会触发级联渲染），切换应用时也会自动
  // 丢弃属于上一个应用的未保存改动 —— 用 effect 同步反而容易把草稿串到别的应用上。
  const [draft, setDraft] = useState<{ scope: string; value: PolicyForm } | null>(null);
  const scope = appKey ?? "";
  const form = draft?.scope === scope ? draft.value : seed(policyQuery.data as Record<string, unknown> | undefined);

  const patch = <K extends keyof PolicyForm>(key: K, value: PolicyForm[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const rebindSeconds = Math.max(0, Number(form.deviceRebindInterval) || 0);
  const strictBindingOn = form.loginCheckDevice || form.loginCheckIp || form.loginCheckUser;

  async function handleSave() {
    try {
      await mutation.mutateAsync({
        loginCheckDevice: form.loginCheckDevice,
        loginCheckDeviceTimeOut: rebindSeconds,
        loginCheckIp: form.loginCheckIp,
        loginCheckUser: form.loginCheckUser,
        registerCheckIp: form.registerCheckIp
      });
      toast.success("已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (!appKey) return <NoAppSelected icon={<Shield className="size-8" />} />;
  if (policyQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-64 w-full rounded-2xl" />
        <Skeleton className="h-48 w-full rounded-2xl" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <SectionCard
        icon={<Fingerprint className="size-4" />}
        title="登录身份绑定"
        aside={<StatusDot active={strictBindingOn} labelActive="强绑定已开启" labelInactive="未开启" />}
      >
        <div className="space-y-5">
          <FieldGroup label="绑定维度">
            <div className="grid gap-2.5 sm:grid-cols-3">
              <SwitchRow
                icon={<MonitorSmartphone className="size-3.5" />}
                label="设备绑定"
                checked={form.loginCheckDevice}
                onChange={(v) => patch("loginCheckDevice", v)}
              />
              <SwitchRow
                icon={<Globe className="size-3.5" />}
                label="登录 IP 校验"
                checked={form.loginCheckIp}
                onChange={(v) => patch("loginCheckIp", v)}
              />
              <SwitchRow
                icon={<MapPin className="size-3.5" />}
                label="登录属地校验"
                checked={form.loginCheckUser}
                onChange={(v) => patch("loginCheckUser", v)}
              />
            </div>
          </FieldGroup>

          <FieldGroup label="设备换绑冷却">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_1fr] sm:items-start">
              <NumberField
                label="冷却时长"
                unit="秒"
                min={0}
                max={31536000}
                value={form.deviceRebindInterval}
                disabled={!form.loginCheckDevice}
                onChange={(v) => patch("deviceRebindInterval", v)}
                hint={`当前：${humanizeSeconds(rebindSeconds)}`}
              />
            </div>
          </FieldGroup>

          <div className="flex justify-end pt-1">
            <Button size="sm" disabled={mutation.isPending} onClick={handleSave}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中..." : "保存策略"}
            </Button>
          </div>
        </div>
      </SectionCard>

      <DeviceLimitEntry appKey={appKey} />

      <RegisterIPEntry appKey={appKey} />

      <LoginBaselineCard appKey={appKey} enabled={strictBindingOn} />
    </div>
  );
}

/**
 * 注册 IP 与设备控制已移到「注册准入」区块（地址 / 网段 / 设备配额、名单、惩罚期、突增检测），这里只留入口与当前状态。
 * 旧开关「注册 IP 唯一」本页照原值回传：没保存过新策略时它仍决定推导出的策略，保存过之后后端不再改它。
 */
function RegisterIPEntry({ appKey }: { appKey: string }) {
  const query = useRegIPPolicyQuery(appKey);
  const policy = query.data?.policy;
  const rules = policy?.rules.filter((rule) => rule.enabled).length ?? 0;
  const summary = policy
    ? [
        policy.mode === "observe" ? "观察模式" : "",
        rules ? `${rules} 条配额规则` : "无配额规则",
        policy.penalty.enabled ? "惩罚期" : "",
        policy.surge.enabled ? "突增检测" : "",
        !query.data?.configured ? "沿用旧开关" : ""
      ]
        .filter(Boolean)
        .join("，")
    : "";
  return (
    <div
      className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-card px-5 py-4"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <div className="flex min-w-0 items-center gap-3">
        <div className="grid size-9 shrink-0 place-items-center rounded-xl bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
          <UserPlus className="size-4" />
        </div>
        <div className="min-w-0 space-y-1">
          <h3 className="text-sm font-semibold tracking-tight">注册准入</h3>
          {query.isLoading ? (
            <Skeleton className="h-3.5 w-40" />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <StatusDot active={Boolean(policy?.enabled)} labelActive="注册准入已开启" labelInactive="注册准入未开启" />
              {policy?.enabled && summary ? <span className="text-xs text-muted-foreground">{summary}</span> : null}
            </div>
          )}
        </div>
      </div>
      <Button size="sm" variant="outline" asChild>
        <Link href={appSectionHref(appKey, "register-ip")}>
          管理
          <ArrowRight className="size-3.5" />
        </Link>
      </Button>
    </div>
  );
}

/**
 * 同时登录设备数已移到「登录设备」区块（多维度上限、超限处置、在线设备），这里只留一个入口与当前状态，
 * 免得管理员在老位置找不到它。旧的 multiDeviceLogin 两个字段本页不再提交，后端也不再从这里写，
 * 只在没保存过新策略时用库里原有的值推导上限。
 */
function DeviceLimitEntry({ appKey }: { appKey: string }) {
  const query = useDeviceLimitPolicyQuery(appKey);
  const policy = query.data?.policy;
  const summary = policy
    ? [
        policy.maxDevices > 0 ? `默认 ${policy.maxDevices} 台` : "不限总数",
        OVERFLOW_META[policy.overflow]?.short,
        policy.tiers.length ? `${policy.tiers.length} 档会员规则` : ""
      ]
        .filter(Boolean)
        .join("，")
    : "";
  return (
    <div
      className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-card px-5 py-4"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <div className="flex min-w-0 items-center gap-3">
        <div className="grid size-9 shrink-0 place-items-center rounded-xl bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
          <MonitorSmartphone className="size-4" />
        </div>
        <div className="min-w-0 space-y-1">
          <h3 className="text-sm font-semibold tracking-tight">同时登录设备</h3>
          {query.isLoading ? (
            <Skeleton className="h-3.5 w-40" />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <StatusDot active={Boolean(policy?.enabled)} labelActive="已开启" labelInactive="未开启" />
              {policy?.enabled && summary ? <span className="text-xs text-muted-foreground">{summary}</span> : null}
            </div>
          )}
        </div>
      </div>
      <Button size="sm" variant="outline" asChild>
        <Link href={appSectionHref(appKey, "device-limit")}>
          管理
          <ArrowRight className="size-3.5" />
        </Link>
      </Button>
    </div>
  );
}

/**
 * 登录绑定查询与重置。
 *
 * 强绑定策略一旦开启就必须有解绑出口，否则「换了个宽带」等于账号报废。
 * 这里按用户 ID 查当前基线并支持一键重置，下次登录重新建立。
 */
function LoginBaselineCard({ appKey, enabled }: { appKey: string; enabled: boolean }) {
  const [input, setInput] = useState("");
  const [queriedUserId, setQueriedUserId] = useState<number | null>(null);
  const baselineQuery = useAdminAppLoginBaselineQuery(appKey, queriedUserId);
  const resetMutation = useResetAdminAppLoginBaselineMutation(appKey);

  const parsed = Number(input.trim());
  const canQuery = Boolean(input.trim()) && Number.isFinite(parsed) && parsed > 0;
  const baseline = baselineQuery.data?.baseline;

  async function handleReset() {
    if (!queriedUserId) return;
    try {
      await resetMutation.mutateAsync(queriedUserId);
      toast.success("登录绑定已重置");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "重置失败");
    }
  }

  return (
    <SectionCard
      icon={<Link2Off className="size-4" />}
      title="登录绑定"
      aside={
        enabled ? null : (
          <Badge variant="outline" className="text-[10px]">
            校验未开启
          </Badge>
        )
      }
    >
      <div className="space-y-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="输入用户 ID"
            className="sm:max-w-60"
            onKeyDown={(e) => {
              if (e.key === "Enter" && canQuery) setQueriedUserId(parsed);
            }}
          />
          <Button size="sm" variant="outline" disabled={!canQuery} onClick={() => setQueriedUserId(parsed)}>
            <Search className="size-3.5" />
            查询绑定
          </Button>
        </div>

        {queriedUserId === null ? null : baselineQuery.isLoading ? (
          <Skeleton className="h-24 w-full rounded-xl" />
        ) : !baselineQuery.data?.bound ? (
          <div className="rounded-xl bg-muted p-4 text-center text-xs text-muted-foreground">
            暂无绑定基线
          </div>
        ) : (
          <div className="space-y-3">
            <dl className="grid gap-2.5 sm:grid-cols-2">
              <BaselineItem label="绑定设备" value={baseline?.deviceId} mono />
              <BaselineItem label="上次登录 IP" value={baseline?.ip} mono />
              <BaselineItem label="登录属地" value={baseline?.region} />
              <BaselineItem label="上次换绑" value={formatDateTime(baseline?.deviceBoundAt)} />
            </dl>
            <div className="flex justify-end">
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button size="sm" variant="outline" disabled={resetMutation.isPending}>
                    <Link2Off className="size-3.5" />
                    重置绑定
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>重置用户 {queriedUserId} 的登录绑定？</AlertDialogTitle>
                    <AlertDialogDescription>
                      将清除该用户的设备、网段与属地绑定。
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>取消</AlertDialogCancel>
                    <AlertDialogAction onClick={handleReset}>确认重置</AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          </div>
        )}
      </div>
    </SectionCard>
  );
}

function BaselineItem({ label, value, mono }: { label: string; value?: string | null; mono?: boolean }) {
  return (
    <div className="rounded-xl bg-muted px-3 py-2.5">
      <dt className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 truncate text-xs", mono && "font-mono", !value && "text-muted-foreground")}>
        {value || "未记录"}
      </dd>
    </div>
  );
}

function formatDateTime(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}
