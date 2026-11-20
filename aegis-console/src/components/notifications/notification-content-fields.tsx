"use client";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import {
  USER_NOTIFICATION_CATEGORIES,
  USER_NOTIFICATION_LEVELS,
  USER_NOTIFICATION_ROUTES,
  isHttpsUrl,
  type UserNotificationAction
} from "@/lib/user-notification-meta";

/**
 * 站内信内容的一套字段：标题、正文、分类、级别、跳转、按钮文案、配图、过期时间。
 *
 * 用户列表的「发通知」（bulk-notify-dialog）与应用详情的「站内信群发」共用。
 * 草稿是受控的纯数据，校验与载荷各一个纯函数，规则与后端 Deliver 一致
 * （docs/notification-center.md「消息形状」），提交仍以后端为准（42230 / 42231 / 40010）。
 */

export type NotificationActionKind = "none" | "route" | "url";

export const NOTIFICATION_ACTION_KINDS: { value: NotificationActionKind; label: string }[] = [
  { value: "none", label: "无" },
  { value: "route", label: "应用内页面" },
  { value: "url", label: "链接" }
];

const DAY = 24 * 60 * 60 * 1000;

export const NOTIFICATION_EXPIRE_PRESETS = [
  { label: "1 天", days: 1 },
  { label: "7 天", days: 7 },
  { label: "30 天", days: 30 }
];

export type NotificationContentDraft = {
  title: string;
  content: string;
  category: string;
  level: string;
  actionKind: NotificationActionKind;
  route: string;
  url: string;
  actionLabel: string;
  image: string;
  /** datetime-local 的值（本地时间） */
  expiresAt: string;
};

export function emptyNotificationContent(): NotificationContentDraft {
  return {
    title: "",
    content: "",
    category: "announcement",
    level: "info",
    actionKind: "none",
    route: USER_NOTIFICATION_ROUTES[0].value,
    url: "",
    actionLabel: "",
    image: "",
    expiresAt: ""
  };
}

export type NotificationContentErrors = {
  url: string | null;
  image: string | null;
  expiresAt: string | null;
  actionLabel: string | null;
};

/**
 * 校验草稿。`expiresAfter` 给出时过期时间还要晚于它（定时发送时为定时时间）。
 * 返回逐字段的错误与能否提交。
 */
export function checkNotificationContent(
  draft: NotificationContentDraft,
  options: { now: number; requireContent?: boolean; expiresAfter?: { at: number; message: string } | null }
): { errors: NotificationContentErrors; ready: boolean } {
  const url = draft.url.trim();
  const image = draft.image.trim();
  const expiresIso = fromLocalInputValue(draft.expiresAt);
  const expiresMs = expiresIso ? new Date(expiresIso).getTime() : null;

  let expiresError: string | null = null;
  if (draft.expiresAt) {
    if (expiresMs === null || expiresMs <= options.now) expiresError = "必须晚于现在";
    else if (expiresMs > options.now + 366 * DAY) expiresError = "不能超过一年";
    else if (options.expiresAfter && expiresMs <= options.expiresAfter.at) expiresError = options.expiresAfter.message;
  }

  const errors: NotificationContentErrors = {
    url: draft.actionKind === "url" && url && !isHttpsUrl(url) ? "必须是 https 地址" : null,
    image: image && !isHttpsUrl(image) ? "必须是 https 地址" : null,
    expiresAt: expiresError,
    actionLabel: draft.actionKind !== "none" && [...draft.actionLabel.trim()].length > 16 ? "不能超过 16 个字" : null
  };

  const ready =
    draft.title.trim().length > 0 &&
    (!options.requireContent || draft.content.trim().length > 0) &&
    !errors.url &&
    !errors.image &&
    !errors.expiresAt &&
    !errors.actionLabel &&
    (draft.actionKind !== "url" || url.length > 0);

  return { errors, ready };
}

export function notificationActionOf(draft: NotificationContentDraft): UserNotificationAction | undefined {
  const label = draft.actionLabel.trim() || undefined;
  if (draft.actionKind === "route") return { type: "route", target: draft.route, label };
  if (draft.actionKind === "url") return { type: "url", target: draft.url.trim(), label };
  return undefined;
}

/** 草稿转请求体里的内容部分（空值不下发） */
export function notificationContentPayload(draft: NotificationContentDraft) {
  return {
    title: draft.title.trim(),
    content: draft.content.trim() || undefined,
    category: draft.category,
    level: draft.level,
    action: notificationActionOf(draft),
    image: draft.image.trim() || undefined,
    expiresAt: fromLocalInputValue(draft.expiresAt)
  };
}

export function NotificationField({
  label,
  hint,
  error,
  children
}: {
  label: string;
  hint?: string;
  error?: string | null;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
      {error ? (
        <p className="text-[11px] text-destructive">{error}</p>
      ) : hint ? (
        <p className="text-[11px] text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  );
}

function OptionSelect({
  value,
  onChange,
  options
}: {
  value: string;
  onChange: (value: string) => void;
  options: ReadonlyArray<{ value: string; label: string }>;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger size="sm" className="h-8 w-full text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((item) => (
          <SelectItem key={item.value} value={item.value} className="text-xs">
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function NotificationContentFields({
  value,
  onChange,
  errors,
  now,
  categories = USER_NOTIFICATION_CATEGORIES,
  contentOptional
}: {
  value: NotificationContentDraft;
  onChange: (patch: Partial<NotificationContentDraft>) => void;
  errors: NotificationContentErrors;
  now: number;
  /** 分类选项，默认取 user-notification-meta；群发编辑器传后端目录 */
  categories?: ReadonlyArray<{ value: string; label: string }>;
  /** 正文可空（空时后端用标题兜底） */
  contentOptional?: boolean;
}) {
  return (
    <div className="space-y-3">
      <NotificationField label="标题">
        <Input
          value={value.title}
          maxLength={200}
          className="h-8 text-xs"
          placeholder="例如：版本更新"
          onChange={(event) => onChange({ title: event.target.value })}
        />
      </NotificationField>
      <NotificationField label="正文" hint={contentOptional ? "不填时使用标题" : undefined}>
        <Textarea rows={4} value={value.content} placeholder="正文内容" onChange={(event) => onChange({ content: event.target.value })} />
      </NotificationField>

      <div className="grid grid-cols-2 gap-3">
        <NotificationField label="分类">
          <OptionSelect value={value.category} onChange={(category) => onChange({ category })} options={categories} />
        </NotificationField>
        <NotificationField label="级别">
          <OptionSelect value={value.level} onChange={(level) => onChange({ level })} options={USER_NOTIFICATION_LEVELS} />
        </NotificationField>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <NotificationField label="跳转">
          <OptionSelect
            value={value.actionKind}
            onChange={(actionKind) => onChange({ actionKind: actionKind as NotificationActionKind })}
            options={NOTIFICATION_ACTION_KINDS}
          />
        </NotificationField>
        {value.actionKind === "route" ? (
          <NotificationField label="页面">
            <OptionSelect value={value.route} onChange={(route) => onChange({ route })} options={USER_NOTIFICATION_ROUTES} />
          </NotificationField>
        ) : null}
        {value.actionKind !== "none" ? (
          <NotificationField label="按钮文案" hint={value.actionKind === "route" ? "可不填" : undefined} error={errors.actionLabel}>
            <Input
              value={value.actionLabel}
              className="h-8 text-xs"
              placeholder="例如：立即查看"
              onChange={(event) => onChange({ actionLabel: event.target.value })}
            />
          </NotificationField>
        ) : null}
      </div>
      {value.actionKind === "url" ? (
        <NotificationField label="链接" error={errors.url}>
          <Input
            value={value.url}
            className="h-8 font-mono text-xs"
            placeholder="https://"
            onChange={(event) => onChange({ url: event.target.value })}
          />
        </NotificationField>
      ) : null}

      <NotificationField label="配图地址" hint="可不填" error={errors.image}>
        <Input
          value={value.image}
          className="h-8 font-mono text-xs"
          placeholder="https://"
          onChange={(event) => onChange({ image: event.target.value })}
        />
      </NotificationField>

      <NotificationField label="过期时间" hint="过期后不再出现在用户的消息列表中，不填即长期有效" error={errors.expiresAt}>
        <div className="flex flex-wrap items-center gap-1.5">
          <Input
            type="datetime-local"
            value={value.expiresAt}
            min={toLocalInputValue(now)}
            className="h-8 w-52 font-mono text-xs tabular-nums"
            onChange={(event) => onChange({ expiresAt: event.target.value })}
          />
          {NOTIFICATION_EXPIRE_PRESETS.map((preset) => (
            <button
              key={preset.days}
              type="button"
              onClick={() => onChange({ expiresAt: toLocalInputValue(now + preset.days * DAY) })}
              className="rounded-md border border-border bg-muted px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
            >
              {preset.label}
            </button>
          ))}
          {value.expiresAt ? (
            <button
              type="button"
              onClick={() => onChange({ expiresAt: "" })}
              className="px-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
            >
              清除
            </button>
          ) : null}
        </div>
      </NotificationField>
    </div>
  );
}
