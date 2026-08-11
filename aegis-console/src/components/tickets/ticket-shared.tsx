"use client";

import { Badge } from "@/components/ui/badge";
import type { TicketPriority, TicketSLAState, TicketStatus } from "@/lib/api/tickets";

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
