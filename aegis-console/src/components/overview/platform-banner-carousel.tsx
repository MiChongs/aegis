"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import Autoplay from "embla-carousel-autoplay";
import ClassNames from "embla-carousel-class-names";
import useEmblaCarousel from "embla-carousel-react";
import type { EmblaCarouselType } from "embla-carousel";
import { ArrowUpRight, ChevronLeft, ChevronRight } from "lucide-react";
import { useActivePlatformBannersQuery } from "@/lib/admin-hooks";
import type { PlatformBannerItem, PlatformBannerType } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * 平台 Banner 轮播 · 极简 · 显示器原生帧率
 *
 * 设计原则：
 *   1. 全交给 Embla 的内部 rAF 动画机做滚动，**不写任何 applyTween / CSS transition**
 *      覆盖到 slide 或其内层；Embla 的 animate 循环本身就是跟显示器刷新率一致
 *      的 requestAnimationFrame，浏览器在 120/144/240Hz 下会自动跑满。
 *   2. 任何"帧插值"层（CSS transition / animation）都会把 JS-driven 的高刷帧
 *      重新压到自己的缓动曲线里，反而掉帧。移除所有此类装饰。
 *   3. 只保留最必要的 UI：导航箭头（悬停）、分页点、类型胶囊、标题组。
 *      去掉了进度条、计数器、播放/暂停、内层缩放/透明度、视差位移、vignette。
 *
 * 高刷要点：
 *   - `.embla__container` 挂 `will-change: transform`：提升合成层，避免重绘
 *   - 不用 `transition`，让 Embla 直接控制 translate
 *   - Autoplay 静默工作，无任何动画装饰 UI
 */

const AUTOPLAY_DELAY = 6000;

const TYPE_LABEL: Record<PlatformBannerType, string> = {
  info: "公告",
  notice: "通知",
  maintenance: "维护",
  release: "版本",
  security: "安全"
};

// 类型色渐变基底 —— 图片未加载/失败时可读兜底
const TYPE_GRADIENT: Record<PlatformBannerType, string> = {
  info:        "linear-gradient(135deg, #334155 0%, #0f172a 100%)",
  notice:      "linear-gradient(135deg, #1e40af 0%, #082f49 100%)",
  maintenance: "linear-gradient(135deg, #b45309 0%, #431407 100%)",
  release:     "linear-gradient(135deg, #15803d 0%, #022c22 100%)",
  security:    "linear-gradient(135deg, #b91c1c 0%, #450a0a 100%)"
};

export function PlatformBannerCarousel() {
  const query = useActivePlatformBannersQuery();
  const items = query.data ?? [];
  const canLoop = items.length > 1;

  // Autoplay 单实例：plugins 数组不支持 render 间热替换
  const autoplayRef = useRef(
    Autoplay({
      delay: AUTOPLAY_DELAY,
      stopOnInteraction: false,
      stopOnMouseEnter: true,
      stopOnFocusIn: true,
      playOnInit: true
    })
  );
  const classNamesRef = useRef(
    ClassNames({
      snapped: "is-snapped",
      inView: "is-in-view",
      draggable: "is-draggable",
      dragging: "is-dragging",
      loop: "is-loop"
    })
  );

  // Options 稳定引用
  const options = useMemo(
    () => ({
      loop: canLoop,
      align: "start" as const,
      // duration 是 Embla 内部 tick 数（~60fps 基准），设小一些让切换干净利落
      duration: 24,
      containScroll: false as const,
      skipSnaps: false,
      dragFree: false,
      watchDrag: canLoop,
      watchResize: true,
      slidesToScroll: 1
    }),
    [canLoop]
  );

  const [emblaRef, emblaApi] = useEmblaCarousel(options, [
    autoplayRef.current,
    classNamesRef.current
  ]);

  const [selectedIndex, setSelectedIndex] = useState(0);
  const [scrollSnaps, setScrollSnaps] = useState<number[]>([]);

  const handleSelect = useCallback((api: EmblaCarouselType) => {
    setSelectedIndex(api.selectedScrollSnap());
  }, []);

  useEffect(() => {
    if (!emblaApi) return;
    setScrollSnaps(emblaApi.scrollSnapList());
    handleSelect(emblaApi);

    emblaApi.on("select", handleSelect);
    emblaApi.on("reInit", (api) => {
      setScrollSnaps(api.scrollSnapList());
      handleSelect(api);
    });
  }, [emblaApi, handleSelect]);

  useEffect(() => {
    if (!emblaApi) return;
    emblaApi.reInit(options, [autoplayRef.current, classNamesRef.current]);
  }, [emblaApi, options, items.length]);

  // 🚀 图片预热：<link rel="preload" as="image">
  //   - 首张 fetchpriority=high 集中带宽
  //   - 其他 auto 后台继续拉
  //   - 实际缓存命中由 public/sw.js 里的 CacheFirst 策略接管，
  //     Cache Storage API 命中不经主线程，比任何 IDB 包装都快
  //   - 首次 miss → SW 存入缓存；此后访问（含刷新 / 跨会话）直接 L2 命中，0 网络
  useEffect(() => {
    if (items.length === 0 || typeof document === "undefined") return;
    const links: HTMLLinkElement[] = [];
    items.forEach((item, i) => {
      const src = (item.imageDisplayUrl || item.imageUrl || "").trim();
      if (!src) return;
      const link = document.createElement("link");
      link.rel = "preload";
      link.as = "image";
      link.href = src;
      link.setAttribute("fetchpriority", i === 0 ? "high" : "auto");
      document.head.appendChild(link);
      links.push(link);
    });
    return () => {
      links.forEach((l) => l.parentElement?.removeChild(l));
    };
  }, [items]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLElement>) => {
      if (!canLoop) return;
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        emblaApi?.scrollPrev();
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        emblaApi?.scrollNext();
      }
    },
    [canLoop, emblaApi]
  );

  if (query.isLoading) {
    // 骨架同步"普通层 + overflow-hidden + rounded-xl"的最小策略
    return (
      <div className="h-full min-h-[220px] animate-pulse overflow-hidden rounded-xl border bg-card" />
    );
  }
  if (items.length === 0) return null;

  return (
    <section
      aria-label="平台横幅"
      aria-roledescription="carousel"
      tabIndex={canLoop ? 0 : undefined}
      onKeyDown={handleKeyDown}
      // 🔑 唯一可靠方案：用 `mask-image: linear-gradient(#000,#000)` 强制生成
      //   一个 alpha 全不透明的**合成层 mask**。mask 层在 DOM 合成树里是
      //   carousel 自身的合成描述，**它天然尊重元素的 `border-radius`**
      //   —— 这是 Chromium 内部 layer tree 的硬规则，不受"transform 后代"
      //   的合成层边界 bug 影响。
      //
      //   之前的 4 角 div 遮罩方案视觉上会"漏馅"：遮罩的 `bg-background`
      //   色盖在深色图片上变成醒目的浅灰色豁口（就是你截图里看到的那种
      //   "角落方块"）。本质是 **填色 ≠ 裁切**，浅色页底色遮不住暗色图。
      //
      //   mask-image 则是真裁切：被 mask 透明区的像素完全不绘制，背后露
      //   出真正的页面背景。无论里面图片深浅，外观一致、干净。
      //
      //   linear-gradient(#000,#000) 是单色填充，浏览器会做成 1×1 纯色
      //   mask，合成成本和 backdrop-blur 无关；先前掉帧的真凶是 backdrop-blur
      //   + 懒加载，现已移除，本方案引入的 mask 合成层成本可忽略。
      style={{
        maskImage: "linear-gradient(#000, #000)",
        WebkitMaskImage: "linear-gradient(#000, #000)"
      }}
      className="group/carousel relative flex h-full min-h-[220px] flex-col overflow-hidden rounded-xl border bg-card focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 focus-visible:ring-offset-2"
    >
      <div ref={emblaRef} className="h-full overflow-hidden">
        <div
          className="flex h-full touch-pan-y"
          // Embla translate3d 唯一合成层 —— 四角遮罩在 DOM 更上层，不受影响
          style={{ willChange: "transform" }}
        >
          {items.map((item, i) => (
            <div
              key={item.id}
              className="embla__slide relative flex min-w-0 shrink-0 grow-0 basis-full"
              role="group"
              aria-roledescription="slide"
              aria-label={`第 ${i + 1} 张，共 ${items.length} 张`}
            >
              <SlideLink href={item.clickUrl}>
                <SlideContent item={item} eager={i === 0} />
              </SlideLink>
            </div>
          ))}
        </div>
      </div>

      {canLoop && (
        <>
          <NavButton aria-label="上一张" side="left" onClick={() => emblaApi?.scrollPrev()}>
            <ChevronLeft className="size-4" />
          </NavButton>
          <NavButton aria-label="下一张" side="right" onClick={() => emblaApi?.scrollNext()}>
            <ChevronRight className="size-4" />
          </NavButton>
        </>
      )}

      {canLoop && (
        <div className="absolute bottom-3.5 left-1/2 z-20 flex -translate-x-1/2 items-center gap-1.5">
          {scrollSnaps.map((_, i) => (
            <button
              key={i}
              type="button"
              aria-label={`跳转第 ${i + 1} 张`}
              aria-current={i === selectedIndex ? "true" : undefined}
              onClick={() => emblaApi?.scrollTo(i)}
              className={cn(
                "h-1 rounded-full bg-white/50 transition-[width,background-color] duration-300 ease-out hover:bg-white/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70",
                i === selectedIndex ? "w-6 bg-white" : "w-1"
              )}
            />
          ))}
        </div>
      )}

    </section>
  );
}

// ─────────────── 单张幻灯片：全覆盖图 + 极简文字层 ───────────────

function SlideContent({ item, eager }: { item: PlatformBannerItem; eager: boolean }) {
  const imgSrc = (item.imageDisplayUrl || item.imageUrl || "").trim();
  const typeLabel = TYPE_LABEL[item.type] ?? TYPE_LABEL.info;
  const typeGradient = TYPE_GRADIENT[item.type] ?? TYPE_GRADIENT.info;
  const [imgState, setImgState] = useState<"idle" | "loaded" | "error">(imgSrc ? "idle" : "error");
  const imgRef = useRef<HTMLImageElement | null>(null);

  // 🚀 预解码：onLoad 时走 HTMLImageElement.decode() 在主线程外完成解码再淡入。
  // 缓存命中由 Service Worker（public/sw.js 的 CacheFirst）负责：
  //   Cache Storage API 命中不经 JS / IDB，直接从磁盘兑付，通常 <5ms。
  const handleImgLoad = useCallback(() => {
    const el = imgRef.current;
    if (el && typeof el.decode === "function") {
      el.decode().then(() => setImgState("loaded")).catch(() => setImgState("loaded"));
    } else {
      setImgState("loaded");
    }
  }, []);

  return (
    <article
      className="relative flex h-full min-h-[220px] w-full min-w-0 flex-col overflow-hidden"
    >
      {/* ⓪ 基底：error 态直接显示类型渐变；loading/loaded 态用中性深色占位，
          让用户在 shimmer 期间看到的是"中性骨架"而非"饱和渐变"，观感更接近
          加载中而非已呈现最终内容。图加载完成后 img 会把这层完全覆盖。 */}
      <div
        className="absolute inset-0"
        style={{
          background:
            imgState === "error"
              ? typeGradient
              : // 中性暗色骨架：足够暗以衬托白字 + 足够朴素避免被误认为最终图
                "linear-gradient(135deg, #27272a 0%, #18181b 100%)"
        }}
        aria-hidden
      />

      {/* ① Shimmer 骨架：仅 loading 期间出现，让用户感知"正在载入"而不是"就这样了" */}
      {imgSrc && imgState === "idle" ? (
        <div
          className="pointer-events-none absolute inset-0 overflow-hidden"
          aria-hidden
        >
          <div
            className="absolute inset-y-0 -inset-x-[40%]"
            style={{
              background:
                "linear-gradient(110deg, transparent 30%, rgba(255,255,255,0.09) 50%, transparent 70%)",
              animation: "banner-img-shimmer 1.4s ease-in-out infinite"
            }}
          />
        </div>
      ) : null}

      {/* ② 主图 —— 原生 img + 预解码 + IDB 缓存优先 */}
      {imgSrc ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          ref={imgRef}
          src={imgSrc}
          alt={item.title || typeLabel}
          loading="eager"
          fetchPriority={eager ? "high" : "auto"}
          decoding="async"
          draggable={false}
          className={cn(
            "pointer-events-none absolute inset-0 size-full object-cover transition-opacity duration-300 ease-out",
            imgState === "loaded" ? "opacity-100" : "opacity-0"
          )}
          onLoad={handleImgLoad}
          onError={() => setImgState("error")}
        />
      ) : null}


      {/* 底部暗渐变 —— 文字可读性所必需，非装饰 */}
      <div
        className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/75 via-black/20 to-transparent"
        aria-hidden
      />

      {/* 右上类型胶囊
          —— 🚀 去掉 backdrop-blur-md：backdrop-filter 每帧都要对背景做高斯模糊合成，
             每张 slide × 1 badge，切换时 GPU 负载倍增；改用纯黑半透更廉价且视觉无差别 */}
      <div className="absolute right-4 top-4 z-10">
        <span className="inline-flex items-center gap-1.5 rounded-full border border-white/20 bg-black/55 px-2.5 py-1 text-[11px] font-medium text-white">
          {typeLabel}
        </span>
      </div>

      {/* 左下文字层 */}
      <div className="relative z-10 mt-auto flex min-w-0 max-w-[85%] flex-col gap-1.5 p-5 text-white sm:max-w-[72%] sm:p-6">
        <h3
          className={cn(
            "line-clamp-2 text-lg font-semibold leading-snug tracking-tight text-white",
            "sm:text-xl lg:text-2xl",
            "[text-shadow:0_1px_3px_rgba(0,0,0,0.55)]"
          )}
          style={{ textWrap: "balance" } as React.CSSProperties}
        >
          {item.title || "（未命名横幅）"}
        </h3>

        {item.description ? (
          <p
            className={cn(
              "line-clamp-2 text-xs leading-relaxed text-white/85 sm:text-sm",
              "[text-shadow:0_1px_2px_rgba(0,0,0,0.45)]"
            )}
          >
            {item.description}
          </p>
        ) : null}

        {item.clickUrl ? (
          <div className="mt-1 inline-flex items-center gap-1 text-xs font-medium text-white/85 transition-colors group-hover/carousel:text-white">
            查看详情
            <ArrowUpRight className="size-3.5 transition-transform group-hover/carousel:translate-x-0.5 group-hover/carousel:-translate-y-0.5" />
          </div>
        ) : null}
      </div>
    </article>
  );
}

// ─────────────── Link 分流：内部走 next/link prefetch ───────────────

function SlideLink({ href, children }: { href?: string; children: React.ReactNode }) {
  if (!href) return <div className="flex size-full">{children}</div>;
  const trimmed = href.trim();
  const isInternal =
    trimmed.startsWith("/") &&
    !trimmed.startsWith("//") &&
    !trimmed.startsWith("/api/") &&
    !trimmed.startsWith("/_next");
  if (isInternal) {
    return (
      <Link href={trimmed} prefetch className="flex size-full cursor-pointer">
        {children}
      </Link>
    );
  }
  return (
    <a href={trimmed} target="_blank" rel="noreferrer" className="flex size-full cursor-pointer">
      {children}
    </a>
  );
}

// ─────────────── 左右切换按钮 —— 悬停/聚焦才显形 ───────────────

function NavButton({
  children,
  side,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { side: "left" | "right" }) {
  // 🔑 去掉 backdrop-blur-md：
  //   backdrop-filter 会对身后的 carousel 内容做采样合成，触发浏览器在 section 上
  //   重新评估合成层边界，是"悬停时圆角变矩形"的直接触发器。
  //   改用不透明度更高的纯半透黑，视觉上等价的"玻璃按钮"感，但不再产生
  //   backdrop 合成层，也不会扰动父级 clip-path 的裁剪链。
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "absolute top-1/2 z-20 grid size-9 -translate-y-1/2 place-items-center rounded-full border border-white/20 bg-black/55 text-white shadow-[0_1px_4px_rgba(0,0,0,0.35)] transition",
        side === "left" ? "left-3" : "right-3",
        "opacity-0 hover:bg-black/70 focus-visible:opacity-100 group-hover/carousel:opacity-100",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/60"
      )}
    >
      {children}
    </button>
  );
}
