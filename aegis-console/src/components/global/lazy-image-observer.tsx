"use client";

import { useEffect } from "react";

/**
 * 全局图片懒加载：
 *   - 为所有 <img> 自动补上 loading="lazy" + decoding="async"
 *     （原生浏览器懒加载 + 异步解码，节省首屏主线程）
 *   - 通过 MutationObserver 持续监听动态插入的图片
 *   - 已显式声明 loading="eager"、data-eager、data-priority 的不动
 *     （例如首屏 LCP 图、视口内关键图、next/image 的 priority）
 *   - 仅影响原生 <img>，不会干扰 next/image（next/image 自带懒加载 + 优化）
 */
function enhance(img: HTMLImageElement) {
  if (img.dataset.aegisLazy === "1") return;

  // 显式声明不懒加载的，放行
  if (img.getAttribute("loading") === "eager") return;
  if (img.dataset.eager === "true" || img.dataset.priority === "true") return;

  if (!img.hasAttribute("loading")) img.setAttribute("loading", "lazy");
  if (!img.hasAttribute("decoding")) img.setAttribute("decoding", "async");

  img.dataset.aegisLazy = "1";
}

export function LazyImageObserver() {
  useEffect(() => {
    // 首次扫描现有 DOM
    document.querySelectorAll("img").forEach((el) => enhance(el as HTMLImageElement));

    // 持续监听新插入的节点（React 重新渲染、异步内容、富文本 innerHTML 等）
    const mo = new MutationObserver((records) => {
      for (const rec of records) {
        rec.addedNodes.forEach((node) => {
          if (node.nodeType !== 1) return;
          const el = node as HTMLElement;
          if (el.tagName === "IMG") {
            enhance(el as HTMLImageElement);
            return;
          }
          // 子树里的 <img>
          el.querySelectorAll?.("img").forEach((i) => enhance(i as HTMLImageElement));
        });
      }
    });

    mo.observe(document.body, { childList: true, subtree: true });
    return () => mo.disconnect();
  }, []);

  return null;
}
