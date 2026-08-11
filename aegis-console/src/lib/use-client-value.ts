"use client";

import { useSyncExternalStore } from "react";

/** 这类值在组件生命周期内不会变化，订阅函数永远不触发更新 */
const noopSubscribe = () => () => {};

/**
 * 是否已运行在浏览器端。
 *
 * 用 `useSyncExternalStore` 而不是 `useEffect` + `setMounted(true)`：
 * 后者会触发级联渲染，且被 `react-hooks/purity` 规则拒绝。
 * React 会自行处理服务端快照与客户端快照的差异，不产生水合告警。
 */
export function useIsClient(): boolean {
  return useSyncExternalStore(
    noopSubscribe,
    () => true,
    () => false
  );
}

/** 当前页面 origin；服务端渲染阶段返回空串 */
export function useOrigin(): string {
  return useSyncExternalStore(
    noopSubscribe,
    () => window.location.origin,
    () => ""
  );
}
