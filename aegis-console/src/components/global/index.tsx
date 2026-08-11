"use client";

import { LazyImageObserver } from "./lazy-image-observer";
import { RoutePrefetcher } from "./route-prefetcher";

/**
 * 全局增强：路由预加载 + 图片懒加载。
 * 零 UI 输出，挂一次即可。推荐放在 Providers 内部，所有路由生效。
 */
export function GlobalEnhancers() {
  return (
    <>
      <RoutePrefetcher />
      <LazyImageObserver />
    </>
  );
}
