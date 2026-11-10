"use client";

/**
 * 封禁表单的共享部件：时长、范围、封禁级别、原因类别、内部原因、对外说明与通知，
 * 外加把它们合成请求体的 `useBanDraft()`。
 *
 * 详情页的「新建封禁」与列表页的「批量封禁」用的是同一套，两处的默认值、
 * 校验与文案因此不会各说各话。
 *
 * 时长用预设而不是让人先选「临时 / 永久」再填时间：绝大多数封禁就是那几档，
 * 自定义只是兜底。预设的解除时间在**提交那一刻**才按当时时间换算，
 * 表单开着放一会儿再提交也不会少封几分钟。
 */

import * as React from "react";
import { BellRing, CalendarClock, Globe2, Infinity as InfinityIcon, LogIn, LogOut, MessageSquareReply, UserRound } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AccountBanLevel, AccountBanReasonCode, AccountBanScope, AdminUserBanCreateInput } from "@/lib/api/types";
import { cn } from "@/lib/utils";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

export const BAN_REASON_MAX = 500;

export type BanDurationKey = "1h" | "1d" | "7d" | "30d" | "custom" | "permanent";

export const BAN_DURATIONS: Array<{ key: BanDurationKey; label: string; ms?: number }> = [
  { key: "1h", label: "1 小时", ms: HOUR },
  { key: "1d", label: "1 天", ms: DAY },
  { key: "7d", label: "7 天", ms: 7 * DAY },
  { key: "30d", label: "30 天", ms: 30 * DAY },
  { key: "custom", label: "自定义" },
  { key: "permanent", label: "永久" }
];

export const BAN_SCOPE_OPTIONS: Array<{
  value: AccountBanScope;
  title: string;
  detail: string;
  icon: React.ComponentType<{ className?: string }>;
}> = [
  { value: "all", title: "全部访问", detail: "立即下线所有设备，拒绝任何访问", icon: LogOut },
  { value: "login", title: "仅登录", detail: "拒绝新的登录，已登录的设备照常使用", icon: LogIn }
];

export const BAN_LEVEL_OPTIONS: Array<{
  value: AccountBanLevel;
  title: string;
  detail: string;
  icon: React.ComponentType<{ className?: string }>;
}> = [
  { value: "app", title: "本应用", detail: "只封禁这个应用里的这个账号", icon: UserRound },
  {
    value: "platform",
    title: "全站",
    detail: "同一人在所有应用的账号都封，并拦截用这些邮箱、手机号、第三方账号、设备再注册或登录",
    icon: Globe2
  }
];

export const BAN_LEVEL_LABEL: Record<string, string> = { app: "本应用", platform: "全站" };

/** 原因类别与留空对外说明时用户看到的标准说明（服务端按用户语言给出，这里是中文版本的示意） */
export const BAN_REASON_CODES: Array<{ value: AccountBanReasonCode; label: string; standard: string }> = [
  { value: "policy", label: "违反使用条款", standard: "账号存在违反使用条款的行为。" },
  { value: "fraud", label: "欺诈或作弊", standard: "账号存在欺诈或作弊行为。" },
  { value: "spam", label: "垃圾信息或批量注册", standard: "账号存在发送垃圾信息或批量注册的行为。" },
  { value: "abuse", label: "滥用服务", standard: "账号存在滥用服务的行为。" },
  { value: "security", label: "账号安全风险", standard: "账号存在安全风险，已暂停访问。" },
  { value: "payment", label: "支付风险", standard: "账号存在支付风险。" },
  { value: "other", label: "其他", standard: "账号存在违规行为。" }
];

export const BAN_REASON_CODE_LABEL: Record<string, string> = Object.fromEntries(
  BAN_REASON_CODES.map((item) => [item.value, item.label])
);

export const BAN_SOURCE_LABEL: Record<string, string> = {
  manual: "管理员",
  risk: "风控平台",
  platform: "全站封禁连带"
};

export const BAN_REASON_PRESETS = [
  "发布违规内容",
  "恶意刷量",
  "批量注册小号",
  "账号共享或转卖",
  "欺诈交易",
  "骚扰他人"
];

/** 每隔 `interval` 毫秒刷新一次的当前时间，给预览与倒计时用。 */
export function useNow(interval = 30_000) {
  const [now, setNow] = React.useState(() => Date.now());
  React.useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), interval);
    return () => window.clearInterval(timer);
  }, [interval]);
  return now;
}

/** `datetime-local` 的值是本地时间且无时区。 */
function parseLocal(value: string) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function toLocalInput(date: Date) {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function formatBanTime(date: Date) {
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

export function useBanDraft() {
  const [duration, setDuration] = React.useState<BanDurationKey>("1d");
  const [customEnd, setCustomEnd] = React.useState("");
  const [scope, setScope] = React.useState<AccountBanScope>("all");
  const [reason, setReason] = React.useState("");
  const [level, setLevel] = React.useState<AccountBanLevel>("app");
  const [reasonCode, setReasonCode] = React.useState<AccountBanReasonCode>("policy");
  const [publicReason, setPublicReason] = React.useState("");
  const [appealable, setAppealable] = React.useState(true);
  const [notify, setNotify] = React.useState(true);

  /** 解除时间：null 表示永久，undefined 表示自定义时间还没填对。 */
  function resolveEnd(now: number): Date | null | undefined {
    if (duration === "permanent") return null;
    if (duration === "custom") {
      const date = parseLocal(customEnd);
      return date && date.getTime() > now ? date : undefined;
    }
    const preset = BAN_DURATIONS.find((item) => item.key === duration);
    return new Date(now + (preset?.ms ?? DAY));
  }

  function validate(now: number) {
    if (!reason.trim()) return "请填写封禁原因";
    if (resolveEnd(now) === undefined) return "请选择晚于现在的解除时间";
    return null;
  }

  function toPayload(now: number): AdminUserBanCreateInput {
    const end = resolveEnd(now);
    return {
      banType: end ? "temporary" : "permanent",
      banScope: scope,
      reason: reason.trim(),
      endAt: end ? end.toISOString() : null,
      level,
      reasonCode,
      publicReason: publicReason.trim(),
      appealable,
      notify
    };
  }

  function reset() {
    setDuration("1d");
    setCustomEnd("");
    setScope("all");
    setReason("");
    setLevel("app");
    setReasonCode("policy");
    setPublicReason("");
    setAppealable(true);
    setNotify(true);
  }

  return {
    duration,
    setDuration,
    customEnd,
    setCustomEnd,
    scope,
    setScope,
    reason,
    setReason,
    level,
    setLevel,
    reasonCode,
    setReasonCode,
    publicReason,
    setPublicReason,
    appealable,
    setAppealable,
    notify,
    setNotify,
    resolveEnd,
    validate,
    toPayload,
    reset
  };
}

export type BanDraft = ReturnType<typeof useBanDraft>;

/** 一句话说清这次封禁的后果，放在提交按钮旁边。 */
export function describeBanDraft(draft: BanDraft) {
  const preset = BAN_DURATIONS.find((item) => item.key === draft.duration);
  const prefix = draft.level === "platform" ? "全站" : "";
  const length =
    draft.duration === "permanent"
      ? `${prefix}永久封禁`
      : draft.duration === "custom"
        ? `${prefix}封禁至指定时间`
        : `${prefix}封禁 ${preset?.label ?? ""}`;
  const effect = draft.scope === "all" ? "立即下线所有设备" : "已登录的设备不受影响";
  const appeal = draft.appealable ? "" : "，不可申诉";
  return `${length}，${effect}${appeal}`;
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return <Label className="text-xs font-medium text-muted-foreground">{children}</Label>;
}

export function BanDurationField({ draft, now }: { draft: BanDraft; now: number }) {
  const end = draft.resolveEnd(now);
  return (
    <div className="space-y-2">
      <FieldLabel>封禁时长</FieldLabel>
      <ToggleGroup
        type="single"
        value={draft.duration}
        onValueChange={(value) => {
          if (!value) return;
          const key = value as BanDurationKey;
          if (key === "custom" && !draft.customEnd) {
            draft.setCustomEnd(toLocalInput(new Date(now + 3 * DAY)));
          }
          draft.setDuration(key);
        }}
        className="grid w-full grid-cols-3 gap-1 rounded-xl border bg-muted/40 p-1 sm:grid-cols-6"
      >
        {BAN_DURATIONS.map((item) => (
          <ToggleGroupItem
            key={item.key}
            value={item.key}
            className="h-8 rounded-lg px-2 text-xs data-[state=on]:bg-background data-[state=on]:font-medium data-[state=on]:shadow-sm"
          >
            {item.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      {draft.duration === "custom" ? (
        <Input
          type="datetime-local"
          value={draft.customEnd}
          min={toLocalInput(new Date(now))}
          onChange={(event) => draft.setCustomEnd(event.target.value)}
        />
      ) : null}
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {end === null ? (
          <>
            <InfinityIcon className="size-3.5" />
            永久有效，只能手动解除
          </>
        ) : end ? (
          <>
            <CalendarClock className="size-3.5" />
            <span>
              将于 <span className="font-medium tabular-nums text-foreground">{formatBanTime(end)}</span> 自动解除
            </span>
          </>
        ) : (
          <>
            <CalendarClock className="size-3.5" />
            <span className="text-red-600 dark:text-red-400">解除时间必须晚于现在</span>
          </>
        )}
      </div>
    </div>
  );
}

export function BanScopeField({ draft, compact = false }: { draft: BanDraft; compact?: boolean }) {
  return (
    <div className="space-y-2">
      <FieldLabel>生效范围</FieldLabel>
      <RadioGroup
        value={draft.scope}
        onValueChange={(value) => draft.setScope(value as AccountBanScope)}
        className={cn("grid gap-2", compact ? "grid-cols-1" : "sm:grid-cols-2")}
      >
        {BAN_SCOPE_OPTIONS.map((option) => {
          const Icon = option.icon;
          const checked = draft.scope === option.value;
          return (
            <label
              key={option.value}
              className={cn(
                "flex cursor-pointer items-center gap-3 rounded-xl border p-3 transition-colors",
                checked
                  ? "border-red-500/40 bg-red-500/5 ring-1 ring-inset ring-red-500/20"
                  : "hover:bg-accent/40"
              )}
            >
              <span
                className={cn(
                  "grid size-9 shrink-0 place-items-center rounded-lg transition-colors",
                  checked
                    ? "bg-red-500/10 text-red-600 dark:text-red-400"
                    : "bg-muted text-muted-foreground"
                )}
              >
                <Icon className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">{option.title}</span>
                <span className="block text-xs leading-5 text-muted-foreground">{option.detail}</span>
              </span>
              <RadioGroupItem value={option.value} className="shrink-0" />
            </label>
          );
        })}
      </RadioGroup>
    </div>
  );
}

export function BanReasonField({ draft, rows = 4 }: { draft: BanDraft; rows?: number }) {
  const length = draft.reason.length;
  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between gap-2">
        <FieldLabel>内部原因</FieldLabel>
        <span className="text-xs tabular-nums text-muted-foreground">
          {length}/{BAN_REASON_MAX}
        </span>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {BAN_REASON_PRESETS.map((preset) => {
          const active = draft.reason.trim() === preset;
          return (
            <button
              key={preset}
              type="button"
              onClick={() => draft.setReason(preset)}
              className={cn(
                "rounded-full border px-2.5 py-1 text-xs transition-colors",
                active
                  ? "border-foreground/20 bg-foreground text-background"
                  : "text-muted-foreground hover:bg-accent hover:text-foreground"
              )}
            >
              {preset}
            </button>
          );
        })}
      </div>
      <Textarea
        rows={rows}
        value={draft.reason}
        maxLength={BAN_REASON_MAX}
        placeholder="只在控制台显示，用户看不到"
        onChange={(event) => draft.setReason(event.target.value)}
        className="resize-none"
      />
    </div>
  );
}

/** 封禁级别：本应用 / 全站。全站会连带同一人的所有账号，选中时整块转为警示色。 */
export function BanLevelField({ draft, compact = false }: { draft: BanDraft; compact?: boolean }) {
  return (
    <div className="space-y-2">
      <FieldLabel>封禁范围</FieldLabel>
      <RadioGroup
        value={draft.level}
        onValueChange={(value) => draft.setLevel(value as AccountBanLevel)}
        className={cn("grid gap-2", compact ? "grid-cols-1" : "sm:grid-cols-2")}
      >
        {BAN_LEVEL_OPTIONS.map((option) => {
          const Icon = option.icon;
          const checked = draft.level === option.value;
          const strong = option.value === "platform";
          return (
            <label
              key={option.value}
              className={cn(
                "flex cursor-pointer items-start gap-3 rounded-xl border p-3 transition-colors",
                checked
                  ? strong
                    ? "border-red-500/50 bg-red-500/[0.07] ring-1 ring-inset ring-red-500/25"
                    : "border-foreground/20 bg-accent/50 ring-1 ring-inset ring-foreground/10"
                  : "hover:bg-accent/40"
              )}
            >
              <span
                className={cn(
                  "grid size-9 shrink-0 place-items-center rounded-lg transition-colors",
                  checked && strong
                    ? "bg-red-500/10 text-red-600 dark:text-red-400"
                    : checked
                      ? "bg-foreground/10 text-foreground"
                      : "bg-muted text-muted-foreground"
                )}
              >
                <Icon className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">{option.title}</span>
                <span className="block text-xs leading-5 text-muted-foreground">{option.detail}</span>
              </span>
              <RadioGroupItem value={option.value} className="mt-0.5 shrink-0" />
            </label>
          );
        })}
      </RadioGroup>
    </div>
  );
}

/** 原因类别：决定用户看到的标准说明，也用于统计。 */
export function BanReasonCodeField({ draft }: { draft: BanDraft }) {
  return (
    <div className="space-y-2">
      <FieldLabel>原因类别</FieldLabel>
      <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="原因类别">
        {BAN_REASON_CODES.map((item) => {
          const active = draft.reasonCode === item.value;
          return (
            <button
              key={item.value}
              type="button"
              role="radio"
              aria-checked={active}
              onClick={() => draft.setReasonCode(item.value)}
              className={cn(
                "rounded-full border px-2.5 py-1 text-xs transition-colors",
                active
                  ? "border-foreground/20 bg-foreground text-background"
                  : "text-muted-foreground hover:bg-accent hover:text-foreground"
              )}
            >
              {item.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** 对用户显示的说明：与内部原因分开；留空时用户看到所选类别的标准说明。 */
export function BanPublicReasonField({ draft, rows = 3 }: { draft: BanDraft; rows?: number }) {
  const standard = BAN_REASON_CODES.find((item) => item.value === draft.reasonCode)?.standard ?? "";
  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between gap-2">
        <FieldLabel>对用户显示的说明</FieldLabel>
        <span className="text-xs tabular-nums text-muted-foreground">
          {draft.publicReason.length}/{BAN_REASON_MAX}
        </span>
      </div>
      <Textarea
        rows={rows}
        value={draft.publicReason}
        maxLength={BAN_REASON_MAX}
        placeholder={`留空时显示：${standard}`}
        onChange={(event) => draft.setPublicReason(event.target.value)}
        className="resize-none"
      />
      <p className="text-xs leading-5 text-muted-foreground">显示在封禁页面与通知里，会按用户语言发送标准说明的译文，自填内容原样发送。</p>
    </div>
  );
}

function SwitchRow({
  icon: Icon,
  title,
  detail,
  checked,
  onChange
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  detail: string;
  checked: boolean;
  onChange: (next: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-xl border px-3 py-2.5 transition-colors hover:bg-accent/40">
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
        <Icon className="size-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block text-xs leading-5 text-muted-foreground">{detail}</span>
      </span>
      <Switch checked={checked} onCheckedChange={onChange} className="mt-1 shrink-0" />
    </label>
  );
}

/** 申诉与通知开关 */
export function BanNoticeField({ draft, compact = false }: { draft: BanDraft; compact?: boolean }) {
  return (
    <div className={cn("grid gap-2", compact ? "grid-cols-1" : "sm:grid-cols-2")}>
      <SwitchRow
        icon={MessageSquareReply}
        title="允许申诉"
        detail={draft.appealable ? "用户可以在封禁页面提交复审" : "用户看不到申诉入口"}
        checked={draft.appealable}
        onChange={draft.setAppealable}
      />
      <SwitchRow
        icon={BellRing}
        title="发送通知"
        detail={draft.notify ? "按用户语言发邮件与站内消息" : "不发邮件，也不发站内消息"}
        checked={draft.notify}
        onChange={draft.setNotify}
      />
    </div>
  );
}
