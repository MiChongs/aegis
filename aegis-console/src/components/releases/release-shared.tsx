"use client";

import type { ReactNode } from "react";
import type { Release, ReleaseStatus, ReleaseUpdateType, ReleaseVisibility } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

/** 发布中心共用的取值表与小部件。取值与后端 internal/domain/app/release.go 一一对应。 */

export const DATA_FONT = "font-mono tabular-nums";

export const RELEASE_PLATFORMS = [
  { value: "android", label: "Android" },
  { value: "ios", label: "iOS" },
  { value: "harmony", label: "HarmonyOS" },
  { value: "windows", label: "Windows" },
  { value: "macos", label: "macOS" },
  { value: "linux", label: "Linux" },
  { value: "web", label: "Web" },
  { value: "all", label: "全平台" }
];

export const RELEASE_STATUSES: Array<{
  value: ReleaseStatus;
  label: string;
  variant: "success" | "warning" | "secondary" | "danger" | "info";
}> = [
  { value: "draft", label: "草稿", variant: "secondary" },
  { value: "scheduled", label: "待发布", variant: "info" },
  { value: "published", label: "下发中", variant: "success" },
  { value: "paused", label: "已暂停", variant: "warning" },
  { value: "revoked", label: "已撤回", variant: "danger" }
];

export const UPDATE_TYPES: Array<{ value: ReleaseUpdateType; label: string; hint: string }> = [
  { value: "optional", label: "可选更新", hint: "用户可跳过此版本" },
  { value: "recommended", label: "推荐更新", hint: "醒目提示，仍可稍后" },
  { value: "force", label: "强制更新", hint: "不更新无法继续使用" }
];

export const VISIBILITIES: Array<{ value: ReleaseVisibility; label: string; hint: string }> = [
  { value: "public", label: "公开", hint: "所有人可见，含未登录用户与官网" },
  { value: "signed_in", label: "仅登录用户", hint: "未登录用户与官网访客不可见" },
  { value: "testers", label: "仅内测名单", hint: "只有内测名单中的用户与设备可见" }
];

export const COMMON_ABIS = ["arm64-v8a", "armeabi-v7a", "x86_64", "x86", "universal"];

export const ROLLOUT_PRESETS = [1, 5, 10, 25, 50, 100];

export const CHANNEL_LEVELS = [
  { value: "stable", label: "正式" },
  { value: "beta", label: "Beta" },
  { value: "alpha", label: "Alpha" },
  { value: "canary", label: "Canary" },
  { value: "nightly", label: "Nightly" }
];

export function platformLabel(value?: string) {
  return RELEASE_PLATFORMS.find((item) => item.value === value)?.label ?? value ?? "—";
}

export function updateTypeLabel(value?: string) {
  return UPDATE_TYPES.find((item) => item.value === value)?.label ?? "可选更新";
}

export function visibilityLabel(value?: string) {
  return VISIBILITIES.find((item) => item.value === value)?.label ?? "公开";
}

export function ReleaseStatusBadge({ release, className }: { release: Pick<Release, "status" | "effectiveStatus">; className?: string }) {
  const meta =
    RELEASE_STATUSES.find((item) => item.value === (release.effectiveStatus || release.status)) ?? RELEASE_STATUSES[0];
  return (
    <Badge variant={meta.variant} className={className}>
      {meta.label}
    </Badge>
  );
}

export function UpdateTypeBadge({ value }: { value?: string }) {
  if (value === "force") return <Badge variant="danger">强制更新</Badge>;
  if (value === "recommended") return <Badge variant="warning">推荐更新</Badge>;
  return <Badge variant="outline">可选更新</Badge>;
}

export function ChannelDot({ color, className }: { color?: string; className?: string }) {
  return (
    <span
      className={cn("inline-block size-2 shrink-0 rounded-full bg-muted-foreground/40", className)}
      style={color ? { backgroundColor: color } : undefined}
    />
  );
}

export function formatBytes(bytes?: number | null) {
  const value = bytes ?? 0;
  if (value <= 0) return "—";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function percent(part: number, whole: number) {
  if (!whole) return "—";
  return `${((part / whole) * 100).toFixed(1)}%`;
}

/** 一行标签值。详情面板里大量用到，统一行高与对齐。 */
export function FieldRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 py-1.5 text-sm">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right">{children}</span>
    </div>
  );
}

/** 逗号、空格、换行分隔的列表 → 去重后的数组。 */
export function parseList(text: string): string[] {
  const out: string[] = [];
  for (const raw of text.split(/[\s,，;；]+/)) {
    const item = raw.trim();
    if (item && !out.includes(item)) out.push(item);
  }
  return out;
}

export function parseIdList(text: string): number[] {
  return parseList(text)
    .map((item) => Number(item))
    .filter((item) => Number.isInteger(item) && item > 0);
}

/** 当前浏览器时区下的 datetime-local 值 → ISO。 */
export function localInputToIso(value: string): string | null {
  if (!value.trim()) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}
