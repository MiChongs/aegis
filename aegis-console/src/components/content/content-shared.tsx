"use client";

import type { ReactNode } from "react";
import {
  Archive,
  CalendarClock,
  CircleAlert,
  FileEdit,
  Gift,
  Image as ImageIcon,
  LayoutPanelTop,
  Megaphone,
  MonitorPlay,
  PanelTop,
  Radio,
  Rocket,
  ShieldAlert,
  Wrench
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { BannerItem, NoticeItem } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

/**
 * 内容中心的**枚举目录与口径**。
 *
 * 与后端 `internal/domain/app/types.go` 的白名单一一对应。在面板里各写一份
 * 会让「后端加了一档、控制台选不出来」和「控制台能选、保存时报不支持」
 * 这两种漂移同时成为可能，而两边都不会有报错提示。
 */

/* ───────────────────────── Banner 展示位 ───────────────────────── */

export type BannerSlotMeta = {
  value: string;
  label: string;
  hint: string;
  icon: LucideIcon;
  /** 预览用的画布比例，贴近该位在客户端上的真实形状 */
  aspect: string;
};

export const BANNER_SLOTS: BannerSlotMeta[] = [
  { value: "hero", label: "首页轮播", hint: "首屏顶部横幅", icon: LayoutPanelTop, aspect: "16 / 6" },
  { value: "popup", label: "启动弹窗", hint: "进入应用后弹出", icon: MonitorPlay, aspect: "3 / 4" },
  { value: "splash", label: "开屏", hint: "冷启动全屏展示", icon: PanelTop, aspect: "9 / 16" },
  { value: "notice", label: "通知条", hint: "顶部细条提示", icon: Radio, aspect: "16 / 3" },
  { value: "card", label: "卡片位", hint: "列表内嵌推广卡", icon: ImageIcon, aspect: "4 / 3" }
];

export function bannerSlot(value?: string | null): BannerSlotMeta {
  return BANNER_SLOTS.find((item) => item.value === value) ?? BANNER_SLOTS[0];
}

/* ───────────────────────── 公告枚举 ───────────────────────── */

export type NoticeTypeMeta = { value: string; label: string; icon: LucideIcon };

export const NOTICE_TYPES: NoticeTypeMeta[] = [
  { value: "notice", label: "通知", icon: Megaphone },
  { value: "activity", label: "活动", icon: Gift },
  { value: "maintenance", label: "维护", icon: Wrench },
  { value: "update", label: "更新", icon: Rocket },
  { value: "security", label: "安全", icon: ShieldAlert }
];

export function noticeType(value?: string | null): NoticeTypeMeta {
  return NOTICE_TYPES.find((item) => item.value === value) ?? NOTICE_TYPES[0];
}

export type NoticeLevelMeta = {
  value: string;
  label: string;
  variant: "secondary" | "warning" | "danger";
  dot: string;
};

export const NOTICE_LEVELS: NoticeLevelMeta[] = [
  { value: "normal", label: "普通", variant: "secondary", dot: "bg-sky-500" },
  { value: "important", label: "重要", variant: "warning", dot: "bg-amber-500" },
  { value: "critical", label: "紧急", variant: "danger", dot: "bg-red-500" }
];

export function noticeLevel(value?: string | null): NoticeLevelMeta {
  return NOTICE_LEVELS.find((item) => item.value === value) ?? NOTICE_LEVELS[0];
}

export type NoticeStatusMeta = {
  value: string;
  label: string;
  variant: "secondary" | "success" | "warning";
  icon: LucideIcon;
};

export const NOTICE_STATUSES: NoticeStatusMeta[] = [
  { value: "draft", label: "草稿", variant: "secondary", icon: FileEdit },
  { value: "published", label: "已发布", variant: "success", icon: Rocket },
  { value: "archived", label: "已归档", variant: "warning", icon: Archive }
];

export function noticeStatus(value?: string | null): NoticeStatusMeta {
  return NOTICE_STATUSES.find((item) => item.value === value) ?? NOTICE_STATUSES[0];
}

/* ───────────────────────── 投放态 ───────────────────────── */

export type ScheduleState = "live" | "scheduled" | "expired" | "disabled";

export type ScheduleMeta = {
  state: ScheduleState;
  label: string;
  variant: "success" | "info" | "secondary" | "warning";
};

/**
 * 投放态是**推导出来的结论**，不是字段罗列。
 *
 * 「启用」这个开关本身回答不了「用户现在看不看得到」——
 * 一条启用但结束时间已过的 Banner，开关是开的，客户端上什么都没有。
 * 界面上必须直接说结论，否则管理员要自己拿三个字段和当前时间做心算。
 */
export function resolveSchedule(
  enabled: boolean | undefined,
  startTime?: string | null,
  endTime?: string | null,
  now: number = Date.now()
): ScheduleMeta {
  if (enabled === false) {
    return { state: "disabled", label: "已停用", variant: "secondary" };
  }
  const start = startTime ? new Date(startTime).getTime() : null;
  const end = endTime ? new Date(endTime).getTime() : null;
  if (start !== null && !Number.isNaN(start) && start > now) {
    return { state: "scheduled", label: "待开始", variant: "info" };
  }
  if (end !== null && !Number.isNaN(end) && end < now) {
    return { state: "expired", label: "已结束", variant: "warning" };
  }
  return { state: "live", label: "投放中", variant: "success" };
}

/** 公告的投放态另加一层状态机：草稿与归档根本不参与时间窗判定。 */
export function resolveNoticeSchedule(item: NoticeItem, now: number = Date.now()): ScheduleMeta {
  if (item.status !== "published") {
    const meta = noticeStatus(item.status);
    return { state: "disabled", label: meta.label, variant: "secondary" };
  }
  return resolveSchedule(true, item.startTime, item.endTime, now);
}

export function ScheduleBadge({ meta, className }: { meta: ScheduleMeta; className?: string }) {
  return (
    <Badge variant={meta.variant} size="sm" className={className}>
      {meta.state === "scheduled" ? <CalendarClock className="size-3" /> : null}
      {meta.state === "expired" ? <CircleAlert className="size-3" /> : null}
      {meta.label}
    </Badge>
  );
}

/* ───────────────────────── 格式化 ───────────────────────── */

const dateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit"
});

const shortFormatter = new Intl.DateTimeFormat("zh-CN", {
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit"
});

export function formatDateTime(value?: string | null, fallback = "—") {
  if (!value) return fallback;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? fallback : dateTimeFormatter.format(date);
}

export function formatShortTime(value?: string | null, fallback = "—") {
  if (!value) return fallback;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? fallback : shortFormatter.format(date);
}

/** 投放窗口写成一行。两端都为空时说「长期」，而不是留两个破折号让人猜。 */
export function formatWindow(startTime?: string | null, endTime?: string | null) {
  if (!startTime && !endTime) return "长期有效";
  if (startTime && !endTime) return `${formatShortTime(startTime)} 起`;
  if (!startTime && endTime) return `至 ${formatShortTime(endTime)}`;
  return `${formatShortTime(startTime)} — ${formatShortTime(endTime)}`;
}

export function formatCount(value?: number | null) {
  const num = value ?? 0;
  if (num < 10000) return String(num);
  if (num < 100000000) return `${(num / 10000).toFixed(num % 10000 === 0 ? 0 : 1)}万`;
  return `${(num / 100000000).toFixed(1)}亿`;
}

/**
 * 点击率。曝光为 0 时返回 null 而不是 0% ——
 * 「没人看过」和「看过但没人点」是两回事，后者才说明素材有问题。
 */
export function clickRate(views?: number | null, clicks?: number | null): string | null {
  const v = views ?? 0;
  if (v <= 0) return null;
  return `${(((clicks ?? 0) / v) * 100).toFixed(1)}%`;
}

/** ISO → `<input type="datetime-local">` 需要的本地时间字符串。 */
export function toLocalInput(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

/** 本地时间字符串 → ISO。留空发 `null` 表示清除，发 `undefined` 会被后端当成不修改。 */
export function fromLocalInput(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const date = new Date(trimmed);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

/* ───────────────────────── 展示原语 ───────────────────────── */

export function StatTile({
  label,
  value,
  hint,
  icon: Icon,
  tone = "default"
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  icon?: LucideIcon;
  tone?: "default" | "positive" | "muted";
}) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {Icon ? <Icon className="size-3.5" /> : null}
        {label}
      </div>
      <div
        className={cn(
          "mt-1 text-2xl font-semibold tabular-nums tracking-tight",
          tone === "positive" && "text-emerald-600 dark:text-emerald-400",
          tone === "muted" && "text-muted-foreground"
        )}
      >
        {value}
      </div>
      {hint ? <div className="mt-0.5 text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

/** 图片缺失时的占位。用展示位图标而不是一个灰块，能看出这条是哪个位的。 */
export function BannerThumbFallback({ slot, className }: { slot: BannerSlotMeta; className?: string }) {
  const Icon = slot.icon;
  return (
    <div className={cn("flex size-full items-center justify-center bg-muted text-muted-foreground", className)}>
      <Icon className="size-5" />
    </div>
  );
}

export function bannerPreviewSrc(item: BannerItem) {
  return item.headerDisplayUrl || item.header || "";
}
