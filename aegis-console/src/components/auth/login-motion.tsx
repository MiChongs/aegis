"use client";

import type { ReactNode } from "react";
import { LazyMotion, domMax } from "motion/react";

/**
 * 登录页的动画能力边界。
 *
 * 用 `domMax` 而不是 `domAnimation`：卡片在「登录 → 双因子 → 成功」三态之间高度差很大，
 * 靠 `layout` 把高度补间成一次形变，需要 layout projection —— 那正是 `domAnimation`
 * 砍掉的部分。少了它高度会瞬跳，卡片下方的页脚跟着抖一下。
 *
 * `strict` 一并打开：强制全页只能用 `m.*`，避免有人随手写 `motion.*`
 * 把完整包重新拉回这个本该轻量的路由。
 */
export function LoginMotionProvider({ children }: { children: ReactNode }) {
  return (
    <LazyMotion features={domMax} strict>
      {children}
    </LazyMotion>
  );
}

/** 与侧边栏 / 路由过渡同一条曲线，登录页不另起一套节奏 */
export const LOGIN_EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];
