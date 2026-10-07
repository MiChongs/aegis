"use client";

import type { ReactNode } from "react";
import type { AuditLog } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 审计日志共用的取值表、格式化与小部件。
 *
 * 操作名、模块、对象都由后端按操作目录给出（operationName / moduleLabel / targetLabel），
 * 这里只负责排版，不再从 action 或路径里猜。
 */

export const DATA_FONT = "font-mono tabular-nums";

export type AuditView = "operation" | "read" | "all";

export const AUDIT_VIEWS: Array<{ value: AuditView; label: string; hint: string }> = [
  { value: "operation", label: "操作", hint: "新建、修改、删除、导出与登录等，不含查看" },
  { value: "read", label: "查看", hint: "打开列表、详情与统计" },
  { value: "all", label: "全部", hint: "操作与查看" }
];

export const AUDIT_STATUSES: Array<{ value: string; label: string; variant: "success" | "danger" | "warning" }> = [
  { value: "success", label: "成功", variant: "success" },
  { value: "failed", label: "失败", variant: "danger" },
  { value: "denied", label: "已拒绝", variant: "warning" },
  { value: "blocked", label: "已拦截", variant: "danger" }
];

// 逗号分隔的取值由后端按「任一匹配」处理：「高及以上」与顶部「今日高风险」的计数口径一致
export const HIGH_RISK_SEVERITIES = "high,critical";

export const AUDIT_SEVERITIES: Array<{ value: string; label: string }> = [
  { value: HIGH_RISK_SEVERITIES, label: "高及以上" },
  { value: "critical", label: "严重" },
  { value: "high", label: "高" },
  { value: "medium", label: "中" },
  { value: "low", label: "低" },
  { value: "info", label: "普通" }
];

export const AUDIT_RANGES: Array<{ value: string; label: string }> = [
  { value: "today", label: "今天" },
  { value: "7d", label: "近 7 天" },
  { value: "30d", label: "近 30 天" }
];

export function statusMeta(status?: string) {
  return AUDIT_STATUSES.find((item) => item.value === status) ?? AUDIT_STATUSES[1];
}

export function severityLabel(severity?: string) {
  return AUDIT_SEVERITIES.find((item) => item.value === severity)?.label ?? "普通";
}

export function isFailure(log: Pick<AuditLog, "status">) {
  return log.status !== "success";
}

export function roleLabel(role?: string) {
  if (!role) return "";
  if (role === "super_admin") return "超级管理员";
  return role;
}

export function kindLabel(kind?: string) {
  return { read: "查看", write: "变更", export: "导出", auth: "身份" }[kind ?? ""] ?? "变更";
}

/** 对象的一行描述：「安装包 voyage-1.0.apk」「版本 #12」。没有对象时返回空串。 */
export function targetText(log: Pick<AuditLog, "targetType" | "targetLabel">) {
  return [log.targetType, log.targetLabel].filter(Boolean).join(" ");
}

/** 一句话描述整条日志，详情抽屉顶部使用。 */
export function describeSentence(log: AuditLog) {
  const actor = log.adminName || "未知管理员";
  const where = log.appName ? `在应用「${log.appName}」` : "";
  const target = log.targetLabel ? ` ${log.targetLabel}` : "";
  const base = `${actor} ${where}${log.operationName}${target}`.replace(/\s+/g, " ").trim();
  if (log.status === "success") return base;
  const result = statusMeta(log.status).label;
  return log.errorMessage ? `${base}，${result}：${log.errorMessage}` : `${base}，${result}`;
}

// ── 时间 ──

const pad = (n: number) => String(n).padStart(2, "0");

export function formatAbsolute(value?: string) {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function formatClock(value?: string) {
  if (!value) return "—";
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? "—" : `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** 相对时间：刚刚 / 5 分钟前 / 3 小时前；超过一天显示时刻（日期由分组标题给出）。 */
export function formatRelative(value: string | undefined, now: number) {
  if (!value) return "—";
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return "—";
  const seconds = Math.max(0, Math.floor((now - time) / 1000));
  if (seconds < 60) return "刚刚";
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 6 * 3600) return `${Math.floor(seconds / 3600)} 小时前`;
  return formatClock(value);
}

export function dayKey(value: string) {
  const d = new Date(value);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

const WEEKDAYS = ["星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"];

export function dayLabel(key: string, now: number) {
  const today = dayKey(new Date(now).toISOString());
  const yesterday = dayKey(new Date(now - 86_400_000).toISOString());
  if (key === today) return "今天";
  if (key === yesterday) return "昨天";
  const [y, m, d] = key.split("-").map(Number);
  const date = new Date(y, m - 1, d);
  const sameYear = y === new Date(now).getFullYear();
  return `${sameYear ? "" : `${y} 年 `}${m} 月 ${d} 日 ${WEEKDAYS[date.getDay()]}`;
}

export function formatBytes(bytes?: number) {
  if (!bytes || bytes <= 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function formatLatency(ms?: number) {
  if (ms === undefined || ms === null) return "—";
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms} ms`;
}

// ── 小部件 ──

export function AdminAvatar({ name, className }: { name?: string; className?: string }) {
  const initial = (name || "?").trim().charAt(0).toUpperCase();
  return (
    <span
      className={cn(
        "inline-flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground",
        className
      )}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

export function StatusBadge({ status }: { status?: string }) {
  const meta = statusMeta(status);
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 风险徽标：只有中及以上才显示，普通与低不占位，避免每一行都是一枚徽标。 */
export function RiskBadge({ severity, always = false }: { severity?: string; always?: boolean }) {
  if (!always && severity !== "medium" && severity !== "high" && severity !== "critical") return null;
  const variant = severity === "medium" ? "warning" : severity === "high" || severity === "critical" ? "danger" : "secondary";
  return (
    <Badge
      variant={variant}
      size="sm"
      className={cn(severity === "critical" && "border-red-600 bg-red-600 text-white dark:border-red-500 dark:bg-red-600 dark:text-white")}
    >
      {severityLabel(severity)}风险
    </Badge>
  );
}

export function ModuleBadge({ label }: { label?: string }) {
  if (!label) return null;
  return (
    <span className="inline-flex shrink-0 items-center rounded-md border px-1.5 text-[11px] leading-[18px] text-muted-foreground">
      {label}
    </span>
  );
}

/** 悬停显示全文的单行截断文本。 */
export function TruncatedText({ text, className, children }: { text: string; className?: string; children?: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn("block truncate", className)}>{children ?? text}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm whitespace-pre-wrap break-words">{text}</TooltipContent>
    </Tooltip>
  );
}
