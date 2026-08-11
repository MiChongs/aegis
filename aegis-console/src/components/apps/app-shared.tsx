"use client";

import { useCallback } from "react";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { GovernanceState } from "@/lib/api/platform-governance";
import type { AppSummary } from "@/lib/api/types";

/**
 * 应用列表与应用详情共用的展示原语。
 *
 * 状态徽标必须只有一处实现：同一个应用在列表卡片、表格行、详情页头部
 * 三处出现，措辞或配色任一处不同，管理员都会怀疑自己看到的是两个东西。
 */

/* ── 格式化 ── */

export function formatAppDate(value?: string | null, withTime = true) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    ...(withTime ? { hour: "2-digit", minute: "2-digit" } : {})
  });
}

export function formatCount(value?: number | null) {
  return typeof value === "number" && Number.isFinite(value) ? value.toLocaleString("zh-CN") : "—";
}

/* ── 标识块 ── */

// 名称 → 固定色相。同一个应用在任何位置都是同一个颜色，扫列表时靠颜色定位比读文字快
const TILE_TONES = [
  "bg-sky-500/12 text-sky-600 ring-sky-500/25 dark:text-sky-300",
  "bg-emerald-500/12 text-emerald-600 ring-emerald-500/25 dark:text-emerald-300",
  "bg-violet-500/12 text-violet-600 ring-violet-500/25 dark:text-violet-300",
  "bg-amber-500/12 text-amber-600 ring-amber-500/25 dark:text-amber-300",
  "bg-rose-500/12 text-rose-600 ring-rose-500/25 dark:text-rose-300",
  "bg-cyan-500/12 text-cyan-600 ring-cyan-500/25 dark:text-cyan-300"
];

function toneOf(seed: string) {
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  return TILE_TONES[hash % TILE_TONES.length];
}

export function AppTile({
  name,
  seed,
  className,
  size = "default"
}: {
  name: string;
  /** 取色种子，传 appKey 保证改名不会换色 */
  seed: string;
  className?: string;
  size?: "sm" | "default" | "lg";
}) {
  const label = (name || "?").trim().slice(0, 1).toUpperCase();
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center rounded-xl font-semibold ring-1 ring-inset",
        size === "sm" && "size-8 text-sm",
        size === "default" && "size-10 text-base",
        size === "lg" && "size-12 rounded-2xl text-lg",
        toneOf(seed || name),
        className
      )}
    >
      {label}
    </span>
  );
}

/* ── 状态徽标 ── */

export const GOVERNANCE_STATE_LABEL: Record<GovernanceState, string> = {
  active: "正常",
  restricted: "部分受限",
  frozen: "冻结",
  suspended: "停运",
  banned: "封禁",
  archived: "归档"
};

/**
 * 三个开关的徽标。
 *
 * 只在**关闭**时用 danger/warning 上色：全部正常时一排绿色徽标反而会盖过
 * 真正需要注意的那一行，扫列表的人得逐个读才能发现异常。
 */
export function AppStatusBadges({
  app,
  governance,
  size = "sm"
}: {
  app: Pick<AppSummary, "status" | "registerStatus" | "loginStatus">;
  governance?: GovernanceState | null;
  size?: "sm" | "default";
}) {
  const governed = governance && governance !== "active" ? governance : null;
  return (
    <div className="flex flex-wrap items-center gap-1">
      <Badge size={size} variant={app.status ? "success" : "danger"}>
        {app.status ? "已启用" : "已停用"}
      </Badge>
      {!app.registerStatus && (
        <Badge size={size} variant="warning">
          注册关闭
        </Badge>
      )}
      {!app.loginStatus && (
        <Badge size={size} variant="warning">
          登录关闭
        </Badge>
      )}
      {governed && (
        <Badge size={size} variant="danger">
          平台{GOVERNANCE_STATE_LABEL[governed] ?? governed}
        </Badge>
      )}
    </div>
  );
}

/* ── AppKey ── */

export function useCopyAppKey() {
  return useCallback((appKey?: string | null) => {
    if (!appKey) return;
    void navigator.clipboard.writeText(appKey);
    toast.success("AppKey 已复制");
  }, []);
}

export function AppKeyText({ appKey, className }: { appKey?: string | null; className?: string }) {
  const copy = useCopyAppKey();
  if (!appKey) return <span className={cn("text-xs text-muted-foreground", className)}>—</span>;
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
      <code className="truncate font-mono text-xs text-muted-foreground">{appKey}</code>
      <button
        type="button"
        title="复制 AppKey"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          copy(appKey);
        }}
        className="shrink-0 rounded p-0.5 text-muted-foreground/70 transition-colors hover:text-foreground"
      >
        <Copy className="size-3" />
      </button>
    </span>
  );
}
