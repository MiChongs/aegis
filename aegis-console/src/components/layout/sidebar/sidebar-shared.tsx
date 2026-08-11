"use client";

import { motion, useReducedMotion } from "motion/react";
import { Star } from "lucide-react";
import { useIsClient } from "@/lib/use-client-value";
import { useSidebarStore } from "@/lib/sidebar-store";
import { cn } from "@/lib/utils";

/**
 * 与 `(console)/template.tsx` 的路由过渡同一条曲线（macOS Big Sur 起的系统默认）。
 * 侧边栏高亮滑动与内容区淡入用同一节奏，切页时才像"一次动作"而不是两件事各动各的。
 */
export const SIDEBAR_EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];

/** 分组折叠 / 子树展开的高度动画时长 */
export const SIDEBAR_COLLAPSE_DURATION = 0.22;

/**
 * 当前项的高亮块。
 *
 * 用 `layoutId` 让它在切页时**从上一项滑到这一项**，而不是这边消失那边出现 ——
 * 视线因此被牵着走，不需要重新扫一遍侧边栏找"我现在在哪"。
 *
 * 同一个 `layoutId` 在同一 `LayoutGroup` 内只能有一个实例，所以收藏区 / 导航树 /
 * 移动端抽屉各自套一层 `<LayoutGroup id>` 做命名空间 —— 否则同一页同时出现在
 * 收藏区和导航树时，两个实例会互相抢这块高亮，其中一个直接消失。
 */
export function ActivePill({ layoutId, className }: { layoutId: string; className?: string }) {
  const reduced = useReducedMotion();
  return (
    <motion.span
      aria-hidden
      layoutId={layoutId}
      className={cn("absolute inset-0 rounded-lg bg-sidebar-accent ring-1 ring-sidebar-border/70", className)}
      transition={reduced ? { duration: 0 } : { type: "spring", stiffness: 520, damping: 44, mass: 0.7 }}
    />
  );
}

/** 三级子项的当前标记：一根短竖线，跟着高亮文字一起滑 */
export function ActiveChildBar({ layoutId }: { layoutId: string }) {
  const reduced = useReducedMotion();
  return (
    <motion.span
      aria-hidden
      layoutId={layoutId}
      className="absolute top-1/2 -left-[9px] h-3.5 w-0.5 -translate-y-1/2 rounded-full bg-foreground"
      transition={reduced ? { duration: 0 } : { type: "spring", stiffness: 560, damping: 46, mass: 0.6 }}
    />
  );
}

/**
 * 收藏开关。
 *
 * 未收藏时只在悬浮 / 键盘聚焦时显形 —— 常驻会让每一行都挂着一颗灰星，
 * 真正被收藏的那几项反而认不出来。
 */
export function PinToggle({
  targetKey,
  title,
  size = "default"
}: {
  targetKey: string;
  title: string;
  size?: "default" | "sm";
}) {
  const pinned = useSidebarStore((s) => s.pinned.includes(targetKey));
  const togglePin = useSidebarStore((s) => s.togglePin);

  return (
    <button
      type="button"
      aria-pressed={pinned}
      title={pinned ? `取消收藏「${title}」` : `收藏「${title}」`}
      onClick={(event) => {
        // 星标叠在整行的点击区上，不拦住就会顺带触发跳转
        event.preventDefault();
        event.stopPropagation();
        togglePin(targetKey);
      }}
      className={cn(
        "relative z-10 flex shrink-0 items-center justify-center rounded-md transition-all",
        "hover:bg-background/80 focus-visible:opacity-100 focus-visible:ring-[2px] focus-visible:ring-ring/50 focus-visible:outline-none",
        size === "sm" ? "size-5" : "size-6",
        pinned ? "opacity-100" : "opacity-0 group-hover:opacity-100"
      )}
    >
      <Star
        className={cn(
          size === "sm" ? "size-3" : "size-3.5",
          pinned ? "fill-amber-400 text-amber-500" : "text-muted-foreground/70"
        )}
      />
      <span className="sr-only">{pinned ? "取消收藏" : "收藏"}</span>
    </button>
  );
}

/**
 * 快捷键修饰键的显示名。
 *
 * 服务端与首帧统一按 `Ctrl` 渲染，避免水合不一致；Mac 上会在挂载后换成 ⌘。
 */
export function useMetaKeyLabel(): string {
  const isClient = useIsClient();
  if (!isClient) return "Ctrl";
  return /Mac|iPhone|iPad/i.test(navigator.userAgent) ? "⌘" : "Ctrl";
}

/** 键帽 */
export function Kbd({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "pointer-events-none inline-flex h-4.5 select-none items-center gap-0.5 rounded border border-border bg-muted px-1 font-sans text-[10px] font-medium text-muted-foreground",
        className
      )}
    >
      {children}
    </kbd>
  );
}
