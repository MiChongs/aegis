"use client";

import { useSyncExternalStore } from "react";

/**
 * 拼音检索（命令面板专用）。
 *
 * 中文控制台里「输 yhgl 找到用户管理」是刚需 —— 侧边栏有 130+ 个跳转目标，
 * 靠中文逐字输入远慢于首字母。`pinyin-pro` 的 `match()` 同时吃全拼与首字母
 * （`yonghu` / `yhgl` 都命中「用户管理」），比自己拼一张首字母表准得多。
 *
 * 词典约 200KB，所以**按需动态加载**：只有第一次打开命令面板（或首屏空闲）
 * 才会拉，且拉取失败会静默退回纯文本匹配，面板本身永远可用。
 *
 * 用 `useSyncExternalStore` 而不是 `useEffect` + `setState` 通知就绪，
 * 与 `use-client-value.ts` / `developer-credentials.ts` 同一约束。
 */

type MatchFn = (text: string, query: string) => number[] | null;

let matcher: MatchFn | null = null;
/** 只置位不复位：拉取失败就永久退回纯文本匹配，不在每次打开面板时重试 */
let started = false;
const listeners = new Set<() => void>();

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** 触发词典加载（幂等）。只能在事件回调 / effect 中调用，不要在渲染期调用。 */
export function preloadPinyin(): void {
  if (started) return;
  started = true;
  import("pinyin-pro")
    .then((mod) => {
      matcher = mod.match as MatchFn;
      for (const listener of listeners) listener();
    })
    .catch(() => {
      // 词典拉不到时保持 matcher 为 null，pinyinMatches() 恒返回 false
    });
}

/** 词典是否已就绪；就绪时会触发一次重渲染，让已输入的关键词重新过一遍拼音匹配 */
export function usePinyinReady(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => matcher !== null,
    () => false
  );
}

/**
 * 这段拼音/首字母在文本里命中的字符下标；未命中或词典未就绪返回 `null`
 * （后者由调用方的纯文本匹配兜底）。
 *
 * 返回下标而不是布尔值，是为了让调用方能分辨"连着的"和"跳着的"：
 * `yh` 在「用户」上是相邻两字，在「远程函数」上是第 1、3 字，
 * 前者显然更像用户想找的那个。
 */
export function pinyinMatchIndices(text: string, query: string): number[] | null {
  if (!matcher || !query) return null;
  try {
    const hit = matcher(text, query);
    return hit && hit.length ? hit : null;
  } catch {
    return null;
  }
}
