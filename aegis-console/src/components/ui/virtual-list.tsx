"use client";

import * as React from "react";
import { ScrollArea as ScrollAreaPrimitive } from "radix-ui";
import {
  defaultRangeExtractor,
  useVirtualizer,
  type Range,
  type Virtualizer
} from "@tanstack/react-virtual";
import { ScrollBar } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

// ─────────────────────────── 类型 ───────────────────────────

export type VirtualListHandle = {
  /** 滚到指定索引；align 默认 auto */
  scrollToIndex: (
    index: number,
    options?: { align?: "start" | "center" | "end" | "auto"; behavior?: "auto" | "smooth" }
  ) => void;
  scrollToTop: (smooth?: boolean) => void;
  scrollToBottom: (smooth?: boolean) => void;
  /** 强制重新测量所有动态尺寸（适合容器宽度变化后调用） */
  measure: () => void;
  /** 拿到原始 virtualizer 做高级控制 */
  getVirtualizer: () => Virtualizer<HTMLDivElement, Element> | null;
};

export type VirtualItemMeta = {
  start: number;
  size: number;
  /** 当前行是否以 sticky 形式吸顶 */
  isSticky: boolean;
};

export type VirtualListProps<T> = {
  items: readonly T[];
  /** 估算高度：固定 px 或按索引返回；动态高度给平均值，真实值由 measureElement 回填 */
  estimateSize: number | ((index: number) => number);
  renderItem: (item: T, index: number, meta: VirtualItemMeta) => React.ReactNode;

  /** 容器尺寸：纵向传高度（默认 "100%"），横向则代表宽度 */
  height?: number | string;
  overscan?: number;
  horizontal?: boolean;
  /** 稳定 key（过滤/排序/动画场景务必传） */
  getItemKey?: (index: number) => string | number;

  /** 吸顶索引（仅纵向）；同时活动的 sticky 取"滚过的最近一个" */
  stickyIndices?: number[];

  /** 滚动到底部距离阈值触发（px）；配合 hasMore/isLoading 去抖 */
  onEndReached?: () => void;
  endReachedThreshold?: number;
  hasMore?: boolean;
  isLoading?: boolean;

  /** 空 / 加载 / 底部占位自定义 */
  empty?: React.ReactNode;
  loader?: React.ReactNode;

  className?: string;
  viewportClassName?: string;
  innerClassName?: string;

  /** Radix ScrollArea 显示模式：hover=悬浮条，always=常显，auto=原生，scroll=滚动时显示 */
  scrollAreaType?: "auto" | "always" | "scroll" | "hover";
  ariaLabel?: string;

  /** 首次 / 重挂载时自动滚到底部（聊天/日志场景） */
  initialScrollToBottom?: boolean;

  ref?: React.Ref<VirtualListHandle>;
};

// ─────────────────────────── 实现 ───────────────────────────

export function VirtualList<T>({
  items,
  estimateSize,
  renderItem,
  height = "100%",
  overscan = 8,
  horizontal = false,
  getItemKey,
  stickyIndices,
  onEndReached,
  endReachedThreshold = 240,
  hasMore = false,
  isLoading = false,
  empty,
  loader,
  className,
  viewportClassName,
  innerClassName,
  scrollAreaType = "hover",
  ariaLabel,
  initialScrollToBottom = false,
  ref
}: VirtualListProps<T>) {
  const viewportRef = React.useRef<HTMLDivElement>(null);

  const stickySet = React.useMemo(() => new Set(stickyIndices ?? []), [stickyIndices]);

  // rangeExtractor：始终保留 sticky 索引在渲染范围内
  const rangeExtractor = React.useCallback(
    (range: Range) => {
      if (stickySet.size === 0) return defaultRangeExtractor(range);
      const next = new Set([...stickySet, ...defaultRangeExtractor(range)]);
      return Array.from(next).sort((a, b) => a - b);
    },
    [stickySet]
  );

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => viewportRef.current,
    estimateSize: typeof estimateSize === "function" ? estimateSize : () => estimateSize,
    overscan,
    horizontal,
    getItemKey,
    rangeExtractor
  });

  // ── 命令式 handle（React 19 原生 ref 作为 prop） ──
  React.useImperativeHandle(
    ref,
    () => ({
      scrollToIndex: (index, options) =>
        virtualizer.scrollToIndex(index, {
          align: options?.align ?? "auto",
          behavior: options?.behavior ?? "auto"
        }),
      scrollToTop: (smooth = false) =>
        virtualizer.scrollToOffset(0, { behavior: smooth ? "smooth" : "auto" }),
      scrollToBottom: (smooth = false) =>
        virtualizer.scrollToOffset(virtualizer.getTotalSize(), {
          behavior: smooth ? "smooth" : "auto"
        }),
      measure: () => virtualizer.measure(),
      getVirtualizer: () => virtualizer
    }),
    [virtualizer]
  );

  // ── 初始贴底（聊天/日志） ──
  const didInitialScroll = React.useRef(false);
  React.useEffect(() => {
    if (!initialScrollToBottom || didInitialScroll.current) return;
    if (items.length === 0) return;
    virtualizer.scrollToOffset(virtualizer.getTotalSize(), { behavior: "auto" });
    didInitialScroll.current = true;
  }, [initialScrollToBottom, items.length, virtualizer]);

  // ── 无限加载 ──
  const firedRef = React.useRef(false);
  React.useEffect(() => {
    if (!onEndReached) return;
    const el = viewportRef.current;
    if (!el) return;

    const handle = () => {
      const offset = horizontal ? el.scrollLeft : el.scrollTop;
      const contentSize = horizontal ? el.scrollWidth : el.scrollHeight;
      const clientSize = horizontal ? el.clientWidth : el.clientHeight;
      const remain = contentSize - clientSize - offset;
      if (remain <= endReachedThreshold) {
        if (!firedRef.current && hasMore && !isLoading) {
          firedRef.current = true;
          onEndReached();
        }
      } else {
        firedRef.current = false;
      }
    };

    el.addEventListener("scroll", handle, { passive: true });
    // 初次挂载也检查一遍（内容不足一屏时立刻触发）
    handle();
    return () => el.removeEventListener("scroll", handle);
  }, [onEndReached, endReachedThreshold, hasMore, isLoading, horizontal, items.length]);

  // items 变化时重置触发锁
  React.useEffect(() => {
    firedRef.current = false;
  }, [items.length]);

  // ── 动态 sticky：算出当前应"吸顶"的那一个 sticky index ──
  const scrollOffset = virtualizer.scrollOffset ?? 0;
  const activeStickyIndex = React.useMemo(() => {
    if (stickySet.size === 0) return -1;
    const sorted = [...stickySet].sort((a, b) => a - b);
    let active = -1;
    for (const idx of sorted) {
      const m = virtualizer.measurementsCache[idx];
      if (!m) continue;
      if (m.start <= scrollOffset) active = idx;
      else break;
    }
    return active;
  }, [stickySet, scrollOffset, virtualizer.measurementsCache]);

  // ── 派生渲染数据 ──
  const virtualItems = virtualizer.getVirtualItems();
  const totalSize = virtualizer.getTotalSize();
  const sizeKey = horizontal ? "width" : "height";
  const showEmpty = items.length === 0 && !isLoading;
  const showLoader = isLoading || (hasMore && !!loader);

  return (
    <ScrollAreaPrimitive.Root
      type={scrollAreaType}
      className={cn("relative overflow-hidden rounded-md", className)}
      style={{ [sizeKey]: height } as React.CSSProperties}
    >
      <ScrollAreaPrimitive.Viewport
        ref={viewportRef}
        className={cn("size-full rounded-[inherit] outline-none", viewportClassName)}
        aria-label={ariaLabel}
        role="list"
      >
        {showEmpty ? (
          <div className="flex h-full min-h-32 items-center justify-center text-sm text-muted-foreground">
            {empty ?? "暂无数据"}
          </div>
        ) : (
          <div
            className={cn("relative", innerClassName)}
            style={{
              [sizeKey]: totalSize,
              width: horizontal ? totalSize : "100%"
            } as React.CSSProperties}
          >
            {virtualItems.map((v) => {
              const item = items[v.index];
              if (item === undefined) return null;

              const isActiveSticky = v.index === activeStickyIndex;
              const isDefinedSticky = stickySet.has(v.index);

              const style: React.CSSProperties = isActiveSticky
                ? {
                    position: "sticky",
                    top: 0,
                    left: 0,
                    zIndex: 2,
                    [sizeKey]: v.size,
                    width: horizontal ? v.size : "100%"
                  }
                : {
                    position: "absolute",
                    top: 0,
                    left: 0,
                    [sizeKey]: v.size,
                    width: horizontal ? v.size : "100%",
                    transform: horizontal
                      ? `translateX(${v.start}px)`
                      : `translateY(${v.start}px)`,
                    // 非活动的 sticky 行也参与正常定位，只是被预渲染保留在 DOM 里
                    zIndex: isDefinedSticky ? 1 : undefined
                  };

              return (
                <div
                  key={v.key}
                  data-index={v.index}
                  data-sticky={isDefinedSticky ? "" : undefined}
                  data-active-sticky={isActiveSticky ? "" : undefined}
                  ref={virtualizer.measureElement}
                  role="listitem"
                  className={cn(
                    isActiveSticky && "bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80"
                  )}
                  style={style}
                >
                  {renderItem(item, v.index, { start: v.start, size: v.size, isSticky: isActiveSticky })}
                </div>
              );
            })}
          </div>
        )}

        {showLoader ? (
          <div className="flex items-center justify-center gap-2 py-3 text-xs text-muted-foreground">
            {loader ?? (
              <>
                <span className="size-1.5 animate-pulse rounded-full bg-muted-foreground/60" />
                加载中…
              </>
            )}
          </div>
        ) : null}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar orientation={horizontal ? "horizontal" : "vertical"} />
      <ScrollAreaPrimitive.Corner className="bg-transparent" />
    </ScrollAreaPrimitive.Root>
  );
}

// 原始 API 透出给高级场景
export { defaultRangeExtractor, useVirtualizer } from "@tanstack/react-virtual";
export type { Range, Virtualizer } from "@tanstack/react-virtual";
