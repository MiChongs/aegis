"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { navigationItems } from "@/lib/navigation";

type IdleWindow = Window & {
  requestIdleCallback?: (cb: IdleRequestCallback, opts?: IdleRequestOptions) => number;
  cancelIdleCallback?: (handle: number) => void;
};

/**
 * 全局路由预加载：
 *   1. 首屏空闲时批量 prefetch 侧边栏所有导航路由（navigationItems 已扁平化全部分组），
 *      后续点击零等待；页内子项只是 `?tab=` 查询串，与父页面同属一条路由，无需单独预取
 *   2. 监听 `pointerover` 冒泡：任何内部 <a href="/..."> 鼠标悬浮时立即 prefetch
 *      —— Next.js App Router 的 <Link> 已在视口内自动 prefetch，这里兜底覆盖
 *      原生 <a> 以及视口外的导航控件
 *   3. 外链、API 路径、hash 路径、Next.js 静态资源不处理
 */
export function RoutePrefetcher() {
  const router = useRouter();

  useEffect(() => {
    const seen = new Set<string>();

    const shouldSkip = (href: string): boolean => {
      if (!href) return true;
      if (!href.startsWith("/")) return true; // 外链 / mailto: / tel: / #hash
      if (href.startsWith("//")) return true; // protocol-relative
      if (href.startsWith("/api/")) return true;
      if (href.startsWith("/_next")) return true;
      return false;
    };

    const safePrefetch = (href: string) => {
      if (shouldSkip(href) || seen.has(href)) return;
      seen.add(href);
      try {
        router.prefetch(href);
      } catch {
        // Next.js 在服务端渲染 / 未就绪 / 动态段缺失时可能抛错，静默吞掉
      }
    };

    // 1) 首屏 idle 预热所有侧边栏路由
    const w = window as IdleWindow;
    const idleRun = w.requestIdleCallback
      ? w.requestIdleCallback(() => navigationItems.forEach((i) => safePrefetch(i.href)), { timeout: 2000 })
      : (window.setTimeout(() => navigationItems.forEach((i) => safePrefetch(i.href)), 500) as unknown as number);

    // 2) 悬浮预加载（指针进入即 prefetch，比 hover 300ms 延迟更早）
    const onPointerOver = (e: PointerEvent) => {
      const target = e.target as Element | null;
      const anchor = target?.closest?.("a[href]") as HTMLAnchorElement | null;
      if (!anchor) return;
      const href = anchor.getAttribute("href");
      if (!href) return;
      safePrefetch(href);
    };

    document.addEventListener("pointerover", onPointerOver, { passive: true, capture: true });

    return () => {
      if (w.cancelIdleCallback) w.cancelIdleCallback(idleRun);
      else clearTimeout(idleRun);
      document.removeEventListener("pointerover", onPointerOver, { capture: true } as EventListenerOptions);
    };
  }, [router]);

  return null;
}
