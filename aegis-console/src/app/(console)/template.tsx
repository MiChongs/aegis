"use client";

import { motion, useReducedMotion } from "motion/react";

/**
 * macOS 原生路由过渡。
 *
 * 对照的真实系统动效：
 *   - System Settings（Ventura/Sonoma）侧栏切换 → 内容区淡入 + 微缩放
 *   - App Store 左侧分类切换 → 同上
 *   - Safari 标签切换、Finder 列视图切换 → 极短 crossfade
 *
 * Apple 的设计约束（克制第一）：
 *   1. 纯 fade + 亚感官 scale（1% 内），不滑动、不模糊、不弹跳
 *   2. 时长 ~280ms，单程 ease-out，没有"overshoot"（Apple 导航不反弹）
 *   3. 缓动曲线固定用 Big Sur 以后的系统默认：[0.32, 0.72, 0, 1]
 *      —— SwiftUI 的 .easeInOut 背后的 CAMediaTimingFunction，
 *         被 Apple 官方在 WWDC 描述为"更符合人眼生理的减速"
 *   4. 缩放锚点 `center center`，让内容"从空间里稳定浮现"
 *
 * 为何只有进场没有退场：
 *   macOS 系统导航（System Settings / App Store）实际上也不做显式 exit —
 *   旧内容在新内容到达的瞬间直接换帧，靠新内容的 enter fade 掩盖切换。
 *   我们依赖 Next.js App Router 的 template.tsx：每次导航强制重建实例，
 *   动画物理上只触发一次，不受 Suspense / concurrent render / StrictMode 干扰。
 *
 * prefers-reduced-motion：退化为纯 150ms opacity，不缩放。
 */
export default function ConsoleTemplate({ children }: { children: React.ReactNode }) {
  const prefersReduced = useReducedMotion();

  // Apple macOS Big Sur 起的系统默认缓动曲线（CAMediaTimingFunction 默认值）
  const APPLE_EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];

  if (prefersReduced) {
    return (
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.2, ease: APPLE_EASE }}
        style={{ minWidth: 0 }}
      >
        {children}
      </motion.div>
    );
  }

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.985 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{
        // 不同属性独立节奏：
        //  - opacity 先行收束（0.42s），让画面"先清晰"
        //  - scale 稍慢完成（0.52s），制造"持续向前"的绵延感
        //  两者错位约 100ms 形成 macOS 标志性的"从容落位"观感
        duration: 0.42,
        ease: APPLE_EASE,
        scale: { duration: 0.52, ease: APPLE_EASE },
        opacity: { duration: 0.42, ease: APPLE_EASE }
      }}
      style={{
        minWidth: 0,
        willChange: "opacity, transform",
        transformOrigin: "center center"
      }}
    >
      {children}
    </motion.div>
  );
}
