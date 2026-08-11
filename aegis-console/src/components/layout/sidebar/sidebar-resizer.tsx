"use client";

import { useRef } from "react";
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarStore } from "@/lib/sidebar-store";

/** 键盘微调步长：一次一格，够用又不至于按半天 */
const KEYBOARD_STEP = 16;

/**
 * 侧边栏右边缘的宽度拖拽手柄。
 *
 * 为什么值得做：这份导航里既有「总览」这样两个字的页面，也有「iOS/Android 标识 → 营销名」
 * 这种一行放不下的。固定宽度只能二选一 —— 要么常年截断，要么常年浪费横向空间。
 *
 * 拖拽期间父级要关掉 `transition-[width]`（`onResizingChange`），
 * 否则每一帧都在补间上一帧，手感会明显发黏。
 */
export function SidebarResizer({ onResizingChange }: { onResizingChange: (resizing: boolean) => void }) {
  const width = useSidebarStore((s) => s.width);
  const setWidth = useSidebarStore((s) => s.setWidth);
  const resetWidth = useSidebarStore((s) => s.resetWidth);
  const drag = useRef<{ startX: number; startWidth: number } | null>(null);

  function endDrag() {
    if (!drag.current) return;
    drag.current = null;
    onResizingChange(false);
    document.body.style.removeProperty("cursor");
    document.body.style.removeProperty("user-select");
  }

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="调整侧边栏宽度"
      aria-valuenow={width}
      aria-valuemin={SIDEBAR_MIN_WIDTH}
      aria-valuemax={SIDEBAR_MAX_WIDTH}
      tabIndex={0}
      title="拖动调整宽度，双击恢复默认"
      onDoubleClick={resetWidth}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        drag.current = { startX: event.clientX, startWidth: width };
        event.currentTarget.setPointerCapture(event.pointerId);
        onResizingChange(true);
        // 拖过内容区时不让浏览器把文字选中
        document.body.style.setProperty("cursor", "col-resize");
        document.body.style.setProperty("user-select", "none");
      }}
      onPointerMove={(event) => {
        if (!drag.current) return;
        setWidth(drag.current.startWidth + (event.clientX - drag.current.startX));
      }}
      onPointerUp={(event) => {
        if (drag.current) event.currentTarget.releasePointerCapture(event.pointerId);
        endDrag();
      }}
      onPointerCancel={endDrag}
      onLostPointerCapture={endDrag}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft") {
          event.preventDefault();
          setWidth(width - KEYBOARD_STEP);
        } else if (event.key === "ArrowRight") {
          event.preventDefault();
          setWidth(width + KEYBOARD_STEP);
        } else if (event.key === "Home") {
          event.preventDefault();
          resetWidth();
        }
      }}
      className="group absolute inset-y-0 right-0 z-30 hidden w-1.5 cursor-col-resize touch-none outline-none lg:block"
    >
      <span
        aria-hidden
        className="absolute inset-y-0 right-0 w-0.5 bg-transparent transition-colors group-hover:bg-ring/40 group-focus-visible:bg-ring/60 group-active:bg-ring/60"
      />
    </div>
  );
}
