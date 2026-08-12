"use client";

import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useTheme } from "next-themes";
import { animate } from "motion";
import { useReducedMotion } from "motion/react";

/**
 * 深浅色切换的扩散动画
 * ─────────────────────────────────────────────────────────────────────────
 * 三层分工，每一层都交给已经在用的库，本文件只负责把它们接起来：
 *
 * | 关注点 | 由谁负责 |
 * |---|---|
 * | 主题状态、持久化、**跟随系统** | `next-themes` |
 * | 新旧画面的真实快照 | 浏览器原生 View Transitions API |
 * | 缓动、降级路径的动画 | `motion` |
 *
 * 圆形扩散必须由 View Transitions 来做：它把切换前后的页面各截一张图，
 * 我们只是把「新」那张按圆形裁剪着放大 —— 露出来的是真正渲染好的新主题。
 * 自己糊一个圆形遮罩只能画出一块纯色，盖过去之后底下什么时候换主题都得靠猜。
 *
 * 没有专门装一个第三方切换库：npm 上对口的 `react-theme-switch-animation`
 * 把 react/react-dom 写成了 **dependencies** 且限定 `^17 || ^18`，
 * 装进这个 React 19 项目会在依赖树里落下第二份 React，hooks 直接失效。
 *
 * ── 眼睛舒适 ──
 *   - 620ms + 重 ease-out（与侧边栏 / 路由过渡同一条曲线），足够长到不是一次闪光
 *   - `prefers-reduced-motion` 下完全不放动画，直接换色
 *   - 跟随系统导致的自动切换走另一套：全局 280ms 颜色过渡（见 globals.css）。
 *     那种切换没有"从哪里开始"这回事 —— 用户没点任何东西，
 *     凭空从屏幕中央炸开一个圆只会让人以为自己误触了什么。
 */

export type ThemeChoice = "light" | "dark" | "system";
type ResolvedTheme = "light" | "dark";

/** 与 `sidebar-shared.ts` / 登录页同一条曲线，全站切换节奏一致 */
const REVEAL_EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];
const REVEAL_DURATION = 0.62;

/** 触发点：鼠标事件、元素、或显式坐标 */
type RevealOrigin = { x: number; y: number };
type OriginSource = { clientX: number; clientY: number; currentTarget?: EventTarget | null } | Element | RevealOrigin | null | undefined;

type ViewTransitionDocument = Document & {
  startViewTransition?: (callback: () => void | Promise<void>) => { ready: Promise<void>; finished: Promise<void> };
};

function viewportCenter(): RevealOrigin {
  return { x: window.innerWidth / 2, y: window.innerHeight / 2 };
}

function elementCenter(element: Element): RevealOrigin {
  const rect = element.getBoundingClientRect();
  return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
}

/**
 * 解析扩散圆心。
 *
 * 键盘激活（Enter / Space）的点击事件坐标是 (0,0)，直接用会让动画从左上角开始，
 * 看起来像是页面在往外倒 —— 这种情况退回按钮自身的几何中心。
 */
function resolveOrigin(source: OriginSource): RevealOrigin {
  if (!source) return viewportCenter();
  if (source instanceof Element) return elementCenter(source);
  if ("clientX" in source) {
    if (source.clientX !== 0 || source.clientY !== 0) {
      return { x: source.clientX, y: source.clientY };
    }
    if (source.currentTarget instanceof Element) return elementCenter(source.currentTarget);
    return viewportCenter();
  }
  return { x: source.x, y: source.y };
}

/** 圆要盖满整屏，半径取到最远的那个角 */
function coverRadius(origin: RevealOrigin) {
  return Math.hypot(
    Math.max(origin.x, window.innerWidth - origin.x),
    Math.max(origin.y, window.innerHeight - origin.y)
  );
}

function supportsViewTransition() {
  return typeof document !== "undefined" && typeof (document as ViewTransitionDocument).startViewTransition === "function";
}

/**
 * 等 next-themes 把 class 真正写到 `<html>` 上。
 *
 * 它的 `setTheme` 只改 React state，落到 DOM 是在一个 `useEffect` 里 ——
 * `flushSync` 也保证不了 passive effect 同步执行。View Transitions 的回调
 * 支持返回 Promise 并会等它 resolve 之后才截「新」那张图，
 * 所以这里盯着 class 变化，比猜几帧可靠。
 *
 * 兜底 300ms 超时：宁可动画退化成一次普通淡入，也不能把页面卡在快照上。
 */
function waitForThemeClass(target: ResolvedTheme) {
  return new Promise<void>((resolve) => {
    const root = document.documentElement;
    if (root.classList.contains(target)) {
      resolve();
      return;
    }
    let timer = 0;
    const observer = new MutationObserver(() => {
      if (!root.classList.contains(target)) return;
      observer.disconnect();
      window.clearTimeout(timer);
      resolve();
    });
    observer.observe(root, { attributes: true, attributeFilter: ["class"] });
    timer = window.setTimeout(() => {
      observer.disconnect();
      resolve();
    }, 300);
  });
}

/** 原生 View Transitions：把「新」快照按圆形裁剪着放大 */
async function revealWithViewTransition(origin: RevealOrigin, target: ResolvedTheme, apply: () => void) {
  const doc = document as ViewTransitionDocument;
  const transition = doc.startViewTransition!(async () => {
    apply();
    await waitForThemeClass(target);
  });

  try {
    await transition.ready;
  } catch {
    // 被后一次切换打断：状态已经换过去了，不放动画即可
    return;
  }

  const radius = coverRadius(origin);
  // 伪元素动画只能走 WAAPI —— motion 的 animate() 没有 pseudoElement 选项。
  // 缓动值仍取自本文件顶部那一条，和 motion 驱动的降级路径保持同一手感。
  document.documentElement.animate(
    {
      clipPath: [
        `circle(0px at ${origin.x}px ${origin.y}px)`,
        `circle(${radius}px at ${origin.x}px ${origin.y}px)`
      ]
    },
    {
      duration: REVEAL_DURATION * 1000,
      easing: `cubic-bezier(${REVEAL_EASE.join(",")})`,
      fill: "forwards",
      pseudoElement: "::view-transition-new(root)"
    }
  );
}

/**
 * 降级路径（Firefox 等尚未支持 View Transitions 的浏览器）：
 * 用 motion 推一块纯色圆盖过去，盖满的瞬间换主题，再淡出。
 *
 * 观感不如快照方案 —— 扩散过程中看到的是一块纯色而不是新主题的真实画面 ——
 * 但至少不是"啪"地一下换色。
 */
async function revealWithOverlay(origin: RevealOrigin, target: ResolvedTheme, apply: () => void) {
  const overlay = document.createElement("div");
  overlay.className = "theme-reveal-overlay";
  overlay.dataset.to = target;
  overlay.setAttribute("aria-hidden", "true");
  overlay.style.clipPath = `circle(0px at ${origin.x}px ${origin.y}px)`;
  document.body.appendChild(overlay);

  try {
    await animate(
      overlay,
      { clipPath: `circle(${coverRadius(origin)}px at ${origin.x}px ${origin.y}px)` },
      { duration: REVEAL_DURATION * 0.55, ease: REVEAL_EASE }
    );
    apply();
    await waitForThemeClass(target);
    await animate(overlay, { opacity: [1, 0] }, { duration: REVEAL_DURATION * 0.45, ease: "easeOut" });
  } finally {
    overlay.remove();
  }
}

const noopSubscribe = () => () => {};

/**
 * 当前系统偏好。next-themes 的 `systemTheme` 在挂载完成前是 undefined，
 * 而"跟随系统"这一档需要在第一帧就知道自己会落到哪一色，
 * 才能判断这次切换到底换不换颜色。
 */
function useSystemPreference(): ResolvedTheme {
  return useSyncExternalStore(
    (onChange) => {
      const query = window.matchMedia("(prefers-color-scheme: dark)");
      query.addEventListener("change", onChange);
      return () => query.removeEventListener("change", onChange);
    },
    () => (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"),
    () => "light"
  );
}

/**
 * 全站唯一的主题切换入口。
 *
 * 任何 `setTheme` 都应该经由这里 —— 直接调 next-themes 的版本会跳过扩散动画，
 * 表现为"有的按钮有动画、有的没有"，而这种不一致比统一没有动画更显眼。
 */
export function useThemeTransition() {
  const { theme, setTheme: setThemeRaw, resolvedTheme } = useTheme();
  const reduced = useReducedMotion();
  const systemTheme = useSystemPreference();

  const isDark = resolvedTheme === "dark";

  const setTheme = useCallback(
    (next: ThemeChoice, source?: OriginSource) => {
      const target: ResolvedTheme = next === "system" ? systemTheme : next;
      const apply = () => setThemeRaw(next);

      // 只是换了"来源"（light → system 且系统本来就是浅色）时颜色不变，
      // 放一圈扩散动画等于告诉用户发生了什么事，其实什么都没发生。
      const sameColor = target === resolvedTheme;

      if (sameColor || reduced || typeof window === "undefined") {
        apply();
        return;
      }

      const origin = resolveOrigin(source);
      if (supportsViewTransition()) {
        void revealWithViewTransition(origin, target, apply);
      } else {
        void revealWithOverlay(origin, target, apply);
      }
    },
    [reduced, resolvedTheme, setThemeRaw, systemTheme]
  );

  const toggle = useCallback(
    (source?: OriginSource) => setTheme(isDark ? "light" : "dark", source),
    [isDark, setTheme]
  );

  return { theme, resolvedTheme, systemTheme, isDark, setTheme, toggle };
}

/**
 * 跟随系统时给自动切换补一层标记。
 *
 * next-themes 自己已经在监听 `prefers-color-scheme`，颜色会跟着变；
 * 这里额外在切换前后给 `<html>` 挂 400ms 的 `theme-os-shift`，
 * 让 globals.css 把那次过渡放慢一档 —— 夜里系统自动转深色时，
 * 默认那 280ms 对着屏幕的人仍然嫌快。
 *
 * 挂在 Providers 上渲染 null，全站一份。
 */
export function SystemThemeWatcher() {
  const { theme } = useTheme();

  useEffect(() => {
    if (theme !== "system") return;
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    let timer = 0;
    const onChange = () => {
      const root = document.documentElement;
      root.classList.add("theme-os-shift");
      window.clearTimeout(timer);
      timer = window.setTimeout(() => root.classList.remove("theme-os-shift"), 900);
    };
    query.addEventListener("change", onChange);
    return () => {
      query.removeEventListener("change", onChange);
      window.clearTimeout(timer);
      document.documentElement.classList.remove("theme-os-shift");
    };
  }, [theme]);

  return null;
}
