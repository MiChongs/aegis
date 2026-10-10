"use client";

import { Badge } from "@/components/ui/badge";
import type { AdConsentSource, AdPolicyMode, SplashAdStatus } from "@/lib/api/ad-policy";

/** 开屏记录的结局 → 标签与徽标色。与后端 adpolicy.Splash* 一一对应。 */
const SPLASH_STATUS_META: Record<SplashAdStatus, { label: string; variant: "success" | "info" | "warning" | "danger" }> = {
  shown: { label: "已展示", variant: "success" },
  clicked: { label: "已点击", variant: "info" },
  failed: { label: "失败", variant: "danger" },
  timeout: { label: "超时", variant: "warning" }
};

export const SPLASH_STATUS_OPTIONS: Array<{ value: SplashAdStatus; label: string }> = [
  { value: "shown", label: "已展示" },
  { value: "clicked", label: "已点击" },
  { value: "failed", label: "失败" },
  { value: "timeout", label: "超时" }
];

export function SplashStatusBadge({ status }: { status: SplashAdStatus }) {
  const meta = SPLASH_STATUS_META[status] ?? { label: status, variant: "warning" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export function ConsentBadge({ accepted, outdated }: { accepted: boolean; outdated?: boolean }) {
  if (outdated) {
    return (
      <Badge variant="warning" size="sm">
        {accepted ? "同意（旧版本）" : "拒绝（旧版本）"}
      </Badge>
    );
  }
  return (
    <Badge variant={accepted ? "success" : "danger"} size="sm">
      {accepted ? "已同意" : "已拒绝"}
    </Badge>
  );
}

export function ModeBadge({ mode }: { mode: AdPolicyMode }) {
  return (
    <Badge variant={mode === "full" ? "success" : "warning"} size="sm">
      {mode === "full" ? "完整服务" : "基础服务"}
    </Badge>
  );
}

const SOURCE_LABELS: Record<AdConsentSource, string> = {
  app: "App",
  web: "官网",
  guest_sync: "登录前选择"
};

export const CONSENT_SOURCE_OPTIONS: Array<{ value: AdConsentSource; label: string }> = [
  { value: "app", label: "App" },
  { value: "web", label: "官网" },
  { value: "guest_sync", label: "登录前选择" }
];

export function consentSourceLabel(source?: string) {
  if (!source) return "—";
  return SOURCE_LABELS[source as AdConsentSource] ?? source;
}

/** 毫秒数的人话形式。 */
export function formatMillis(value: number) {
  if (!value) return "—";
  if (value < 1000) return `${value} 毫秒`;
  return `${(value / 1000).toFixed(1)} 秒`;
}

/** 间隔秒数的人话形式。 */
export function formatInterval(seconds: number) {
  if (seconds <= 0) return "不限";
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}
