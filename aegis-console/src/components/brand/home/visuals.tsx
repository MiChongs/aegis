"use client";

import { useEffect, useMemo, useRef, type ReactNode } from "react";
import {
  animate,
  m,
  useInView,
  useMotionTemplate,
  useMotionValue,
  useMotionValueEvent,
  useReducedMotion,
} from "motion/react";
import { cn } from "@/lib/utils";

/**
 * 首页的视觉原语。
 *
 * 这一层的存在理由：控制台通体 zinc 单色是对的（长时间盯着看的界面该克制），
 * 但同一套克制搬到门面上就只剩朴素。所以首页额外有一层底纹、光晕与动效，
 * 全部由 `globals.css` 里 `--home-*` 那组令牌着色，深浅两套各自成立。
 *
 * 三条约束：
 *
 * 1. **只做 transform / opacity。** 装饰层不该引起重排，也因此
 *    `prefers-reduced-motion` 下可以整体停掉而不丢任何信息。
 * 2. **不在动画帧里 setState。** 鼠标跟随与数字滚动都走 MotionValue，
 *    前者用 `useMotionTemplate` 直接喂给 style，后者写 `textContent`。
 * 3. **数字的静态值必须出现在 SSR 输出里。** 滚动只是动效，
 *    没有 JS 的时候页面上得是真实数字而不是 0。
 */

/* ─────────────────────────── 底纹 ─────────────────────────── */

type PatternProps = {
  variant?: "grid" | "dots";
  /** 网格边长，默认 56px */
  size?: number;
  /** 溶解遮罩，默认顶部椭圆 */
  mask?: string;
  className?: string;
};

export function Pattern({ variant = "grid", size, mask, className }: PatternProps) {
  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-0",
        variant === "grid" ? "home-grid-pattern" : "home-dot-pattern",
        className
      )}
      style={
        {
          ...(size ? { "--home-grid-size": `${size}px` } : {}),
          ...(mask ? { "--home-grid-mask": mask } : {}),
        } as React.CSSProperties
      }
    />
  );
}

/**
 * 胶片颗粒。
 *
 * 取代此前那两团大面积彩色光晕：发光球是生成式视觉最容易被认出来的特征之一，
 * 而且在浅色模式下只会把底色拖灰。颗粒给的是质感不是光 —— 一层极淡的静态噪声，
 * 把纯色平面变成纸面，看不见但感觉得到。
 */
export function Grain({ className }: { className?: string }) {
  return <div aria-hidden className={cn("home-grain", className)} />;
}

/** 中性暗角，把视线压回版心。不带色相，因此不会和强调色打架。 */
export function Vignette({ size, className }: { size?: string; className?: string }) {
  return (
    <div
      aria-hidden
      className={cn("home-vignette", className)}
      style={size ? ({ "--home-vignette-size": size } as React.CSSProperties) : undefined}
    />
  );
}

/* ─────────────────────────── 聚光卡片 ─────────────────────────── */

/**
 * 鼠标跟随的高光。指针位置写进 MotionValue 再由 `useMotionTemplate`
 * 拼成 background，全程零 re-render —— 每帧 setState 会把整张卡重渲一遍。
 */
export function SpotlightCard({
  children,
  className,
  as: Tag = "div",
}: {
  children: ReactNode;
  className?: string;
  as?: "div" | "article";
}) {
  const reduced = useReducedMotion();
  const x = useMotionValue(-9999);
  const y = useMotionValue(-9999);
  const background = useMotionTemplate`radial-gradient(16rem circle at ${x}px ${y}px, var(--home-spotlight), transparent 70%)`;

  return (
    <Tag
      className={cn("group/spotlight relative overflow-hidden", className)}
      onMouseMove={
        reduced
          ? undefined
          : (event: React.MouseEvent<HTMLElement>) => {
              const rect = event.currentTarget.getBoundingClientRect();
              x.set(event.clientX - rect.left);
              y.set(event.clientY - rect.top);
            }
      }
      onMouseLeave={
        reduced
          ? undefined
          : () => {
              x.set(-9999);
              y.set(-9999);
            }
      }
    >
      {reduced ? null : (
        <m.div
          aria-hidden
          className="pointer-events-none absolute inset-0 opacity-0 transition-opacity duration-300 group-hover/spotlight:opacity-100"
          style={{ background }}
        />
      )}
      {children}
    </Tag>
  );
}

/* ─────────────────────────── 跑马灯 ─────────────────────────── */

/**
 * 无缝横向滚动。同一组内容渲染两遍，轨道位移 -50% 时首尾像素重合。
 * 第二遍对读屏软件隐藏，否则技术栈会被念两次。
 */
export function Marquee({
  children,
  duration = 42,
  reverse = false,
  className,
}: {
  children: ReactNode;
  duration?: number;
  reverse?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "home-marquee-viewport relative overflow-hidden",
        "[mask-image:linear-gradient(90deg,transparent,#000_8%,#000_92%,transparent)]",
        className
      )}
    >
      <div
        className={cn("home-marquee", reverse && "home-marquee--reverse")}
        style={{ "--home-marquee-duration": `${duration}s` } as React.CSSProperties}
      >
        <div className="flex shrink-0 items-center">{children}</div>
        <div className="flex shrink-0 items-center" aria-hidden>
          {children}
        </div>
      </div>
    </div>
  );
}

/* ─────────────────────────── 文字动画 ─────────────────────────── */

/** 与全站切换同一条曲线族，收尾极慢，像被"推"到位而不是弹到位 */
export const TEXT_EASE: [number, number, number, number] = [0.16, 1, 0.3, 1];

/**
 * 遮罩逐行揭示。
 *
 * 外层 `overflow-hidden` 是关键：文字从自己那一行的下沿升上来，
 * 看起来是被"印"出来的。淡入 + 位移做不出这个效果 —— 那只是元素在飘。
 */
export function MaskLine({
  children,
  delay = 0,
  duration = 0.9,
  className,
}: {
  children: ReactNode;
  delay?: number;
  duration?: number;
  className?: string;
}) {
  const reduced = useReducedMotion();

  if (reduced) return <span className={cn("block", className)}>{children}</span>;

  return (
    <span className={cn("block overflow-hidden", className)}>
      <m.span
        className="block"
        initial={{ y: "108%" }}
        animate={{ y: 0 }}
        transition={{ duration, ease: TEXT_EASE, delay }}
      >
        {children}
      </m.span>
    </span>
  );
}

/**
 * 逐字入场。
 *
 * 空格必须渲染成 `&nbsp;`：`inline-block` 会把纯空格折叠掉，
 * 拆完字之后整句会粘成一团。
 */
export function SplitChars({
  text,
  delay = 0,
  stagger = 0.035,
  className,
}: {
  text: string;
  delay?: number;
  stagger?: number;
  className?: string;
}) {
  const reduced = useReducedMotion();
  const chars = useMemo(() => Array.from(text), [text]);

  if (reduced) return <span className={className}>{text}</span>;

  return (
    <span className={className} aria-label={text}>
      {chars.map((char, index) => (
        <m.span
          key={`${char}-${index}`}
          aria-hidden
          className="inline-block"
          initial={{ opacity: 0, y: "0.4em", filter: "blur(4px)" }}
          animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
          transition={{ duration: 0.55, ease: TEXT_EASE, delay: delay + index * stagger }}
        >
          {char === " " ? "\u00A0" : char}
        </m.span>
      ))}
    </span>
  );
}

/**
 * 打字机。
 *
 * 用 `steps()` 沿宽度展开而不是逐字 setState：后者每个字符一次 re-render，
 * 一行 20 字就是 20 次。宽度动画只跑在合成层上。
 * 代价是只能用等宽字体（不然裁切点会落在字形中间），这里正好都是 mono 标签。
 */
export function Typewriter({
  text,
  delay = 0,
  duration = 1.1,
  className,
}: {
  text: string;
  delay?: number;
  duration?: number;
  className?: string;
}) {
  const reduced = useReducedMotion();

  if (reduced) return <span className={className}>{text}</span>;

  return (
    <span className={cn("inline-flex overflow-hidden whitespace-nowrap", className)}>
      <m.span
        className="inline-block whitespace-nowrap"
        initial={{ clipPath: "inset(0 100% 0 0)" }}
        animate={{ clipPath: "inset(0 0% 0 0)" }}
        transition={{ duration, ease: "linear", delay }}
      >
        {text}
      </m.span>
    </span>
  );
}

/* ─────────────────────────── 数字滚动 ─────────────────────────── */

/**
 * 进入视口时从 0 滚到目标值。
 *
 * 静态文本直接渲染在 span 里（SSR 输出的就是真实数字），动画只是把
 * `textContent` 逐帧改写；因此 JS 没跑起来、或者 reduce 档下，
 * 页面上仍然是那个数，不会停在 0。
 */
export function CountUp({ value, className }: { value: string; className?: string }) {
  const reduced = useReducedMotion();
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true, margin: "-40px" });

  // "1000+" → { target: 1000, suffix: "+" }；纯文本则 target 为 null
  const { target, suffix } = useMemo(() => {
    const matched = /^(\d+)(.*)$/.exec(value);
    return matched
      ? { target: Number(matched[1]), suffix: matched[2] ?? "" }
      : { target: null, suffix: "" };
  }, [value]);

  const count = useMotionValue(target ?? 0);

  useMotionValueEvent(count, "change", (latest) => {
    if (ref.current) ref.current.textContent = `${Math.round(latest)}${suffix}`;
  });

  useEffect(() => {
    if (target === null || reduced || !inView) return;
    count.set(0);
    const controls = animate(count, target, {
      duration: 1.4,
      ease: [0.22, 1, 0.36, 1],
    });
    return () => controls.stop();
  }, [count, inView, reduced, target]);

  return (
    <span ref={ref} className={className}>
      {value}
    </span>
  );
}
