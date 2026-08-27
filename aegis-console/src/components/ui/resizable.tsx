"use client";

import { Group, Panel, Separator, useDefaultLayout } from "react-resizable-panels";
import type {
  GroupProps,
  LayoutStorage,
  PanelProps,
  SeparatorProps
} from "react-resizable-panels";
import { cn } from "@/lib/utils";

/**
 * 可拖拽分栏 —— react-resizable-panels v4 的项目皮肤。
 *
 * 为什么用库而不是自己拖：这件事的难点不在 pointermove，而在
 * 键盘可达（`role=separator` + 方向键 + Home/End）、命中区要比那条 1px 的线宽、
 * 拖到边界时的约束传播、以及窗口变化后各分栏怎么分摊。自己写一份的结局
 * 是前者能用、后四件慢慢地不对。
 *
 * v4 与老版 shadcn 那份不是同一套 API：
 * `PanelGroup/PanelResizeHandle/direction/autoSaveId` 已经换成
 * `Group/Separator/orientation/defaultLayout`，尺寸也支持 px / rem / % 混用。
 * 抄网上的 `resizable.tsx` 片段会直接报「导出不存在」。
 */

/**
 * 布局存储。
 *
 * **必须显式传给 `useDefaultLayout`** —— 它的默认值是裸 `localStorage`，
 * 而这个默认值是在调用时求值的：服务端渲染那一趟会直接 ReferenceError，
 * 整页白屏。这里包一层，SSR 阶段返回 null（读到「没有存过」），
 * 水合之后 `useSyncExternalStore` 再把真实布局换上去。
 */
const layoutStorage: LayoutStorage = {
  getItem: (key) => (typeof window === "undefined" ? null : window.localStorage.getItem(key)),
  setItem: (key, value) => {
    if (typeof window !== "undefined") window.localStorage.setItem(key, value);
  }
};

/**
 * 记住用户拖出来的分栏比例。
 *
 * `onlySaveAfterUserInteractions` 是刻意打开的：窗口缩放、面板折叠、约束重算
 * 也会产生布局变化，把它们一并存下来会让「我上次拖的比例」被一次窗口缩放悄悄改写。
 */
export function usePanelLayout(options: { id: string; panelIds?: string[] }) {
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    ...options,
    storage: layoutStorage,
    onlySaveAfterUserInteractions: true
  });
  return { defaultLayout, onLayoutChanged };
}

export function ResizableGroup({ className, ...props }: GroupProps) {
  return <Group className={cn("size-full", className)} {...props} />;
}

/**
 * 分栏。
 *
 * `overflow` 默认改成 `hidden`（库的默认是 `auto`）：这一套布局里每一格的
 * 滚动条都该长在它自己的内容区上，让分栏本身滚会把工具条、页签一起卷走。
 * 需要滚动就在内部再套一层 `overflow-y-auto`。
 *
 * ⚠️ 这个改法只能走 `style`：库把 `overflow: auto` 写成了内联样式，
 * 类名压不过它。
 */
export function ResizablePanel({ className, style, ...props }: PanelProps) {
  return (
    <Panel
      className={cn("min-h-0 min-w-0", className)}
      style={{ overflow: "hidden", ...style }}
      {...props}
    />
  );
}

/**
 * 分隔条。
 *
 * 可见的是 1px 的线，但命中区由库按 `resizeTargetMinimumSize` 撑开
 * （鼠标 10px / 触屏 20px），所以不需要再叠一层 `::after` 热区。
 * `aria-orientation` 与分栏方向是**垂直**关系：横向分栏里的分隔条是竖线。
 */
export function ResizableHandle({ className, ...props }: SeparatorProps) {
  return (
    <Separator
      className={cn(
        "relative shrink-0 bg-border transition-colors duration-150",
        "aria-[orientation=vertical]:w-px aria-[orientation=horizontal]:h-px",
        // hover / active / focus 三态都由库写在 data-separator 上
        "data-[separator=hover]:bg-primary/50 data-[separator=active]:bg-primary",
        "data-[separator=focus]:bg-primary focus-visible:outline-none",
        className
      )}
      {...props}
    />
  );
}

export { usePanelRef, useGroupRef } from "react-resizable-panels";
export type { PanelImperativeHandle } from "react-resizable-panels";
