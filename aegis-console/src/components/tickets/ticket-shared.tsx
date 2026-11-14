"use client";

import type { ReactNode } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { joinApiUrl } from "@/lib/api/client";
import type {
  TicketAttachment,
  TicketClientInfo,
  TicketItem,
  TicketKind,
  TicketPriority,
  TicketSLAState,
  TicketSource,
  TicketStatus
} from "@/lib/api/tickets";

// 工单模块共享的展示映射与格式化。
// 枚举中文名与后端 /api/admin/tickets/metadata 保持一致；
// 这里的常量只作为「元数据尚未加载完成」时的兜底，避免首帧出现英文枚举。

export const STATUS_LABEL: Record<TicketStatus, string> = {
  open: "待受理",
  processing: "处理中",
  pending_user: "待用户补充",
  pending_third_party: "等待第三方",
  resolved: "已解决",
  closed: "已关闭",
  cancelled: "已撤销"
};

export const PRIORITY_LABEL: Record<TicketPriority, string> = {
  urgent: "紧急",
  high: "高",
  normal: "中",
  low: "低"
};

export const SLA_LABEL: Record<TicketSLAState, string> = {
  ontime: "正常",
  warning: "预警",
  breached: "超时",
  paused: "已暂停",
  met: "达标"
};

type BadgeVariant = "default" | "secondary" | "outline" | "success" | "warning" | "danger" | "info";

const STATUS_VARIANT: Record<TicketStatus, BadgeVariant> = {
  open: "warning",
  processing: "info",
  pending_user: "secondary",
  pending_third_party: "secondary",
  resolved: "success",
  closed: "outline",
  cancelled: "outline"
};

const PRIORITY_VARIANT: Record<TicketPriority, BadgeVariant> = {
  urgent: "danger",
  high: "warning",
  normal: "info",
  low: "outline"
};

const SLA_VARIANT: Record<TicketSLAState, BadgeVariant> = {
  ontime: "outline",
  warning: "warning",
  breached: "danger",
  paused: "secondary",
  met: "success"
};

export function StatusBadge({ status, label }: { status: TicketStatus; label?: string }) {
  return (
    <Badge variant={STATUS_VARIANT[status] ?? "outline"} size="sm">
      {label ?? STATUS_LABEL[status] ?? status}
    </Badge>
  );
}

export function PriorityBadge({ priority, label }: { priority: TicketPriority; label?: string }) {
  return (
    <Badge variant={PRIORITY_VARIANT[priority] ?? "outline"} size="sm">
      {label ?? PRIORITY_LABEL[priority] ?? priority}
    </Badge>
  );
}

/** SLA 徽标。正常态不渲染，避免列表里满屏都是「正常」噪声 */
export function SLABadge({ state }: { state: TicketSLAState }) {
  if (state === "ontime") return null;
  return (
    <Badge variant={SLA_VARIANT[state] ?? "outline"} size="sm">
      SLA {SLA_LABEL[state] ?? state}
    </Badge>
  );
}

/** 相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前，超过 30 天回落到日期 */
export function formatRelativeTime(value?: string | null): string {
  if (!value) return "—";
  const target = new Date(value).getTime();
  if (Number.isNaN(target)) return "—";
  const diff = Date.now() - target;
  if (diff < 0) return formatDateTime(value);
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days <= 30) return `${days} 天前`;
  return formatDateTime(value);
}

export function formatDateTime(value?: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", { hour12: false });
}

/** 毫秒时长 → 人类可读（用于平均首响 / 平均解决时长） */
export function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return "—";
  const minutes = Math.round(ms / 60_000);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const restMinutes = minutes % 60;
  if (hours < 24) return restMinutes > 0 ? `${hours} 小时 ${restMinutes} 分` : `${hours} 小时`;
  const days = Math.floor(hours / 24);
  const restHours = hours % 24;
  return restHours > 0 ? `${days} 天 ${restHours} 小时` : `${days} 天`;
}

/** 到期倒计时。已过期返回「已超时 N」并标红，供列表快速扫视 */
export function formatDue(value?: string | null): { text: string; overdue: boolean } {
  if (!value) return { text: "—", overdue: false };
  const target = new Date(value).getTime();
  if (Number.isNaN(target)) return { text: "—", overdue: false };
  const diff = target - Date.now();
  if (diff <= 0) return { text: `已超时 ${formatDuration(-diff)}`, overdue: true };
  return { text: `剩余 ${formatDuration(diff)}`, overdue: false };
}

export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

// ─────────────── 类型 / 来源 / 提交端 ───────────────

export const KIND_LABEL: Record<TicketKind, string> = {
  ticket: "工单",
  feedback: "意见反馈"
};

export const SOURCE_LABEL: Record<TicketSource, string> = {
  console: "控制台",
  app: "应用内",
  web: "网页",
  api: "开放接口",
  email: "邮件",
  bot: "机器人",
  import: "导入"
};

/** 平台标识 → 中文名；未登记的原样展示 */
const PLATFORM_LABEL: Record<string, string> = {
  android: "Android",
  ios: "iOS",
  harmony: "HarmonyOS",
  web: "网页",
  windows: "Windows",
  macos: "macOS",
  linux: "Linux"
};

export function formatPlatform(value?: string): string {
  if (!value) return "";
  return PLATFORM_LABEL[value.toLowerCase()] ?? value;
}

/** 读取 metadata.client，字段类型不符时丢弃，避免把对象直接渲染进文本 */
export function getTicketClient(metadata?: Record<string, unknown>): TicketClientInfo | null {
  const raw = metadata?.client;
  if (!raw || typeof raw !== "object") return null;
  const record = raw as Record<string, unknown>;
  const pick = (key: string) => (typeof record[key] === "string" && record[key] ? (record[key] as string) : undefined);
  const client: TicketClientInfo = { platform: pick("platform"), version: pick("version"), device: pick("device") };
  return client.platform || client.version || client.device ? client : null;
}

/** 列表行用的「平台 版本」短文本 */
export function formatClientShort(client: TicketClientInfo | null): string {
  if (!client) return "";
  return [formatPlatform(client.platform), client.version ? `v${client.version.replace(/^v/i, "")}` : ""]
    .filter(Boolean)
    .join(" ");
}

// ─────────────── 反馈分类 ───────────────

const FEEDBACK_CATEGORY_VARIANT: Record<string, BadgeVariant> = {
  feedback_bug: "danger",
  feedback_feature: "info",
  feedback_experience: "warning",
  feedback_other: "secondary"
};

const FEEDBACK_CATEGORY_BY_NAME: Record<string, BadgeVariant> = {
  问题反馈: "danger",
  功能建议: "info",
  体验吐槽: "warning",
  其他: "secondary"
};

/** 反馈分类徽标。优先按分类标识配色，自建分类回落到名称或中性色 */
export function FeedbackCategoryBadge({ name, categoryKey }: { name?: string; categoryKey?: string }) {
  if (!name) {
    return (
      <Badge variant="outline" size="sm">
        未分类
      </Badge>
    );
  }
  const variant =
    (categoryKey ? FEEDBACK_CATEGORY_VARIANT[categoryKey] : undefined) ?? FEEDBACK_CATEGORY_BY_NAME[name] ?? "outline";
  return (
    <Badge variant={variant} size="sm">
      {name}
    </Badge>
  );
}

// ─────────────── 附件 ───────────────

export function isImageAttachment(file: TicketAttachment): boolean {
  if (file.kind) return file.kind === "image";
  return (file.contentType || "").toLowerCase().startsWith("image/");
}

/** downloadUrl 是相对 Aegis 根的路径，控制台与 API 分域部署时需补全 */
export function attachmentUrl(file: TicketAttachment): string {
  if (!file.downloadUrl) return "";
  return joinApiUrl(file.downloadUrl);
}

// ─────────────── 工作台共用 ───────────────

/** 列表行与状态选择器里的状态色点，和 STATUS_VARIANT 同一色系 */
export const STATUS_DOT: Record<TicketStatus, string> = {
  open: "bg-amber-500",
  processing: "bg-sky-500",
  pending_user: "bg-violet-500",
  pending_third_party: "bg-slate-400",
  resolved: "bg-emerald-500",
  closed: "bg-muted-foreground/40",
  cancelled: "bg-muted-foreground/40"
};

export function StatusDot({ status, className }: { status: TicketStatus; className?: string }) {
  return <span aria-hidden className={cn("inline-block size-2 shrink-0 rounded-full", STATUS_DOT[status], className)} />;
}

const TERMINAL_STATUSES: TicketStatus[] = ["resolved", "closed", "cancelled"];

export function isTerminalStatus(status: TicketStatus) {
  return TERMINAL_STATUSES.includes(status);
}

/** 最后一条是用户发的、且还没结单：说明在等我们回 */
export function isAwaitingReply(item: Pick<TicketItem, "lastMessageRole" | "status">) {
  return item.lastMessageRole === "requester" && !isTerminalStatus(item.status);
}

// 头像底色按名字散列，同一个人在列表、会话、属性栏里颜色一致
const AVATAR_TONES = [
  "bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300",
  "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
  "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  "bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
  "bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300",
  "bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-300",
  "bg-orange-100 text-orange-700 dark:bg-orange-950 dark:text-orange-300",
  "bg-indigo-100 text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300"
];

function avatarTone(seed: string) {
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) hash = (hash * 31 + seed.charCodeAt(i)) | 0;
  return AVATAR_TONES[Math.abs(hash) % AVATAR_TONES.length];
}

/** 取名字的首字（中文）或前两个字母（英文），qq_ 这类前缀跳过 */
export function nameInitials(name?: string) {
  const text = (name || "").replace(/^(qq|wx|wechat|google|github)_/i, "").trim();
  if (!text) return "?";
  const first = Array.from(text)[0];
  if (/[一-鿿]/.test(first)) return first;
  return text.slice(0, 2).toUpperCase();
}

export function PersonAvatar({
  name,
  src,
  size = "default",
  preview = false,
  className
}: {
  name?: string;
  /** 头像地址；没有或加载失败时退回按名字散列的色块 */
  src?: string;
  size?: "sm" | "default" | "lg";
  /** 点击放大查看 */
  preview?: boolean;
  className?: string;
}) {
  return (
    <Avatar size={size} preview={preview} className={className}>
      {src ? <AvatarImage src={src} alt={name || "头像"} /> : null}
      <AvatarFallback className={cn("font-medium", avatarTone(name || "?"))}>{nameInitials(name)}</AvatarFallback>
    </Avatar>
  );
}

/** 键盘提示，工作台的快捷键说明与工具提示共用 */
export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded border bg-muted px-1 font-mono text-[10px] font-medium text-muted-foreground",
        className
      )}
    >
      {children}
    </kbd>
  );
}
