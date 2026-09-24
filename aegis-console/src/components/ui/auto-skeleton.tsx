"use client";

import { useLayoutEffect, useRef, type ReactNode } from "react";
import { AutoSkeleton as AutoSkeletonCore, type SkeletonConfig } from "auto-skeleton-react";
import { useReducedMotion } from "motion/react";

/**
 * 自动骨架屏：照着子树真实渲染出来的 DOM 量出骨架，不必再手写一份「长得像」的
 * 占位组件。手写的那份迟早会和真实布局对不上，而且不会有任何报错提示。
 *
 * 底层是 auto-skeleton-react。直接用它有六处与本项目不合，这层包装各补一处：
 *
 * 1. **配色**：它把 `#e0e0e0` 写死在内联样式里，深色模式下是一排刺眼的亮灰条。
 *    这里改走 `--color-input`。不用 `<Skeleton>` 的 `bg-accent`：它只保留卡片的
 *    边框、不保留底色，骨架直接落在页面底色上，而浅色档的 `--accent` 与 `--background`
 *    是同一个色值（都是 `#f4f4f5`），一整页骨架条会完全看不见。`--input` 是浅色档里
 *    唯一与页面底色有差别的中性令牌，深色档里它与 `--accent` 相同。卡片底色由
 *    `globals.css` 补回。
 * 2. **动效**：它的 pulse 不认 `prefers-reduced-motion`，reduce 档下这里直接关掉。
 * 3. **可访问性**：加载期间子树会被渲染成两份（一份透明占位、一份藏起来量尺寸），
 *    两份里的输入框都还能被 Tab 聚焦、被读屏软件读到。加载期间整块 `inert`。
 * 4. **首帧闪烁**：它要等下一帧量完尺寸，才把真实内容那一层设成透明，这一帧里
 *    占位数据会原样露出来。`globals.css` 按 `aria-busy` 提前把那一层压掉。
 * 5. **定位元素**：它把绝对 / 固定定位的元素也当普通块排进文档流。头像上那层
 *    「更换」浮层会被挤成一摞字条，卡片凭空高出一截，加载完整页往上跳。
 * 6. **控件被判成文字**：它的分类器偏向文字，带字的按钮、标签页、徽标都会按
 *    「高度 ÷ 行高」拆成两行字条。量尺寸之前给这几类控件标上 `data-skeleton-role`，
 *    让它们各出一整块；头像同理标成图片。
 *
 * 用法约束：
 * - 骨架量的是**加载期间渲染出来的东西**。列表没有数据就什么都不渲染，量出来也是空的。
 *   加载期间要喂占位数据，让子树按真实形状渲染（见 `/profile` 的占位账号）。
 * - 不要包图表、地图、Monaco、虚拟列表：子树多挂一份的代价太高，上游也写明这几类量不准。
 * - config 必须是稳定引用：它是量尺寸那个 effect 的依赖，每次渲染传一个新对象，
 *   就会每次渲染都重新量一遍。下面两份都是模块级常量。
 */

/** 文字条这类自身没有圆角的元素用这个值，与 `<Skeleton>` 的 `rounded-md`（calc(var(--radius) - 2px)）对齐 */
const FALLBACK_RADIUS = 14;

/** 上游默认的两条要保留（传了 ignoreSelectors 就是整组替换），后面是第 5 条 */
const IGNORE_SELECTORS = [".no-skeleton", "[data-skeleton-ignore]", ".absolute", ".fixed", ".sr-only"];

/** 第 6 条：选择器 → 骨架角色。开发者自己写了 data-skeleton-role 的不覆盖 */
const ROLE_HINTS: ReadonlyArray<readonly [selector: string, role: string]> = [
  ['[data-slot="avatar"]', "image"],
  ['button, [role="button"], [role="tab"], [role="combobox"], a[data-slot="button"], [data-slot="badge"]', "button"],
];

const PULSE: Partial<SkeletonConfig> = {
  animation: "pulse",
  baseColor: "var(--color-input)",
  borderRadius: FALLBACK_RADIUS,
  ignoreSelectors: IGNORE_SELECTORS,
};

const STILL: Partial<SkeletonConfig> = { ...PULSE, animation: "none" };

export function AutoSkeleton({
  loading,
  children,
  className,
}: {
  loading: boolean;
  children: ReactNode;
  className?: string;
}) {
  const reduced = useReducedMotion();
  const rootRef = useRef<HTMLDivElement>(null);

  // 子组件的 layout effect 先于父组件执行：上游此刻刚排好「下一帧量尺寸」，
  // 这里在同一次提交里赶在它之前把角色标上。只加属性、不动 React 管理的任何东西。
  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!loading || !root) return;
    for (const [selector, role] of ROLE_HINTS) {
      for (const element of root.querySelectorAll(selector)) {
        if (!element.hasAttribute("data-skeleton-role")) element.setAttribute("data-skeleton-role", role);
      }
    }
  }, [loading]);

  return (
    <div ref={rootRef} data-slot="auto-skeleton" aria-busy={loading} inert={loading} className={className}>
      <AutoSkeletonCore loading={loading} config={reduced ? STILL : PULSE}>
        {children}
      </AutoSkeletonCore>
    </div>
  );
}
