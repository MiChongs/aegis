"use client";

import { useState } from "react";
import {
  Fingerprint,
  Globe,
  Info,
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
  SliderField,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";

type PolicyForm = {
  loginCheckDevice: boolean;
  /** 换绑冷却秒数，字符串态以支持编辑中的空串 */
  deviceRebindInterval: string;
  loginCheckIp: boolean;
  loginCheckUser: boolean;
  multiDeviceLogin: boolean;
  multiDeviceLimit: number;
  registerCheckIp: boolean;
};

const defaultForm: PolicyForm = {
  loginCheckDevice: false,
  deviceRebindInterval: "0",
  loginCheckIp: false,
  loginCheckUser: false,
  multiDeviceLogin: true,
  multiDeviceLimit: 5,
  registerCheckIp: false
};

function seed(data?: Record<string, unknown> | null): PolicyForm {
  if (!data) return defaultForm;
  return {
    loginCheckDevice: Boolean(data.loginCheckDevice),
    deviceRebindInterval: String(Number(data.loginCheckDeviceTimeOut) || 0),
    loginCheckIp: Boolean(data.loginCheckIp),
    loginCheckUser: Boolean(data.loginCheckUser),
    multiDeviceLogin: data.multiDeviceLogin !== false,
    multiDeviceLimit: Number(data.multiDeviceLimit) || 5,
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
        multiDeviceLogin: form.multiDeviceLogin,
        multiDeviceLimit: form.multiDeviceLimit,
        registerCheckIp: form.registerCheckIp
      });
      toast.success("认证与会话策略已保存");
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
        description="把账号锁定到固定的设备 / 网络 / 属地。三项默认关闭，开启后属于强绑定策略。"
        aside={<StatusDot active={strictBindingOn} labelActive="强绑定已开启" labelInactive="未开启" />}
      >
        <div className="space-y-5">
          <FieldGroup label="绑定维度" hint="与该用户上一次被放行的登录比对">
            <div className="grid gap-2.5 sm:grid-cols-3">
              <SwitchRow
                icon={<MonitorSmartphone className="size-3.5" />}
                label="设备绑定"
                hint="登录须携带设备标识，且与已绑定设备一致"
                checked={form.loginCheckDevice}
                onChange={(v) => patch("loginCheckDevice", v)}
              />
              <SwitchRow
                icon={<Globe className="size-3.5" />}
                label="登录 IP 校验"
                hint="换到别的网段即拦截（IPv4 比到 /24）"
                checked={form.loginCheckIp}
                onChange={(v) => patch("loginCheckIp", v)}
              />
              <SwitchRow
                icon={<MapPin className="size-3.5" />}
                label="登录属地校验"
                hint="国家 + 省/州 与上次不一致即拦截"
                checked={form.loginCheckUser}
                onChange={(v) => patch("loginCheckUser", v)}
              />
            </div>
          </FieldGroup>

          <FieldGroup label="设备换绑冷却" hint="仅在「设备绑定」开启时生效">
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
              <div className="rounded-xl bg-muted p-3">
                <div className="flex gap-2 text-[11px] leading-relaxed text-muted-foreground">
                  <Info className="mt-0.5 size-3.5 shrink-0" />
                  <p>
                    冷却期从<strong className="text-foreground">上次换绑</strong>起算，不是从上次登录起算 ——
                    否则用户只要天天登录就永远换不了设备。填 0 表示允许随时换绑（仍要求携带设备标识）。
                  </p>
                </div>
              </div>
            </div>
          </FieldGroup>

          {strictBindingOn ? (
            <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 p-3">
              <div className="flex gap-2 text-[11px] leading-relaxed text-amber-700 dark:text-amber-400">
                <Info className="mt-0.5 size-3.5 shrink-0" />
                <p>
                  强绑定策略下，用户换宽带 / 换手机 / 出差都会被拦在门外。
                  唯一的解绑出口是下方的<strong>「登录绑定」</strong>重置 —— 开启前请确认客服流程已就位。
                </p>
              </div>
            </div>
          ) : null}

          <div className="flex justify-end pt-1">
            <Button size="sm" disabled={mutation.isPending} onClick={handleSave}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中..." : "保存策略"}
            </Button>
          </div>
        </div>
      </SectionCard>

      <SectionCard
        icon={<MonitorSmartphone className="size-4" />}
        title="会话与注册"
        description="同时在线设备数与注册准入。"
      >
        <div className="space-y-5">
          <FieldGroup label="多设备会话" hint="超出上限时踢掉最旧的会话">
            <div className="grid gap-3 sm:grid-cols-2">
              <SwitchRow
                label="允许多设备同时在线"
                hint="关闭后新登录会挤掉上一个会话"
                checked={form.multiDeviceLogin}
                onChange={(v) => patch("multiDeviceLogin", v)}
              />
              <SliderField
                label="同时在线上限"
                value={form.multiDeviceLimit}
                min={1}
                max={20}
                step={1}
                unit="台"
                disabled={!form.multiDeviceLogin}
                valueLabel={form.multiDeviceLogin ? `${form.multiDeviceLimit} 台` : "1 台（单设备）"}
                onChange={(v) => patch("multiDeviceLimit", v)}
              />
            </div>
          </FieldGroup>

          <FieldGroup label="注册准入" hint="验证码要求在「验证码」Tab 配置">
            <div className="grid gap-2.5 sm:grid-cols-2">
              <SwitchRow
                icon={<UserPlus className="size-3.5" />}
                label="注册 IP 唯一"
                hint="同一 IP 只允许注册一个账号"
                checked={form.registerCheckIp}
                onChange={(v) => patch("registerCheckIp", v)}
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

      <LoginBaselineCard appKey={appKey} enabled={strictBindingOn} />
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
      toast.success(`用户 ${queriedUserId} 的登录绑定已重置`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "重置失败");
    }
  }

  return (
    <SectionCard
      icon={<Link2Off className="size-4" />}
      title="登录绑定"
      description="查看并重置单个用户的绑定基线。设备 / IP / 属地校验都以此为判定依据。"
      aside={
        enabled ? null : (
          <Badge variant="outline" className="text-[10px]">
            三项校验均未开启，基线不参与判定
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

        {queriedUserId === null ? (
          <p className="text-[11px] text-muted-foreground">输入用户 ID 后可查看该用户当前绑定的设备、网段与属地。</p>
        ) : baselineQuery.isLoading ? (
          <Skeleton className="h-24 w-full rounded-xl" />
        ) : !baselineQuery.data?.bound ? (
          <div className="rounded-xl bg-muted p-4 text-center text-xs text-muted-foreground">
            用户 {queriedUserId} 当前没有绑定基线，下次登录将按首次处理。
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
                      当前绑定的设备、网段与属地会被清除，该用户下次登录将按首次处理并重新建立基线。
                      换绑冷却也会一并重新计时。
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
