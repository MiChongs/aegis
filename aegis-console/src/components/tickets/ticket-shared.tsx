"use client";

import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { joinApiUrl } from "@/lib/api/client";
import type {
  TicketAttachment,
  TicketClientInfo,
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

/** 页头计数卡片，工单中心与意见反馈共用 */
export function MetricTile({
  label,
  value,
  icon,
  highlight,
  danger
}: {
  label: string;
  value: number;
  icon: ReactNode;
  highlight?: boolean;
  danger?: boolean;
}) {
  return (
    <Card className={cn(highlight && "border-primary/40")}>
      <CardContent className="flex items-center gap-3 p-4">
        <span
          className={cn(
            "flex size-8 items-center justify-center rounded-lg bg-muted text-muted-foreground",
            danger && "bg-destructive/10 text-destructive"
          )}
        >
          {icon}
        </span>
        <div>
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className={cn("text-lg font-semibold", danger ? "text-destructive" : "text-foreground")}>{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}
