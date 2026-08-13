"use client";

import type { ReactNode } from "react";
import { m, useReducedMotion } from "motion/react";
import { cn } from "@/lib/utils";

/**
 * 首页分区的公共排版原语。
 *
 * 三件事收在这里，分区组件因此只剩内容：容器宽度与横向内边距、
 * 「eyebrow → 标题 → 描述」的标题组、以及入场动效。
 *
 * 动效刻意只有一种（淡入 + 上移 16px，进入视口触发一次）。旧版首页给
 * 每个分区各配了一套 sticky 视差，代价是 750vh 的滚动距离换四屏内容 ——
 * 访客以为自己在往下翻，实际什么都没翻到。
 */

export const SECTION_CONTAINER = "mx-auto w-full max-w-7xl px-5 md:px-8";

const EASE: [number, number, number, number] = [0.22, 1, 0.36, 1];

export function Reveal({
  children,
  delay = 0,
  className,
}: {
  children: ReactNode;
  delay?: number;
  className?: string;
}) {
  const reduced = useReducedMotion();

  if (reduced) return <div className={className}>{children}</div>;

  return (
    <m.div
      initial={{ opacity: 0, y: 16 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-60px" }}
      transition={{ duration: 0.55, ease: EASE, delay }}
      className={className}
    >
      {children}
    </m.div>
  );
}

export function SectionHeading({
  eyebrow,
  title,
  description,
  align = "start",
  className,
}: {
  eyebrow?: string;
  title: string;
  description?: string;
  align?: "start" | "center";
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col gap-3",
        align === "center" && "items-center text-center",
        className
      )}
    >
      {eyebrow ? (
        <span className="text-xs font-medium tracking-[0.18em] text-muted-foreground uppercase">
          {eyebrow}
        </span>
      ) : null}
      <h2 className="text-2xl font-semibold tracking-tight text-balance sm:text-3xl md:text-4xl">
        {title}
      </h2>
      {description ? (
        <p
          className={cn(
            "max-w-2xl text-sm leading-relaxed text-pretty text-muted-foreground sm:text-base",
            align === "center" && "mx-auto"
          )}
        >
          {description}
        </p>
      ) : null}
    </div>
  );
}

export function Section({
  id,
  children,
  className,
  bordered = false,
}: {
  id?: string;
  children: ReactNode;
  className?: string;
  /** 上沿画一条分隔线。相邻两个分区都开时只会看到一条 */
  bordered?: boolean;
}) {
  return (
    <section
      id={id}
      className={cn(
        "scroll-mt-20 py-16 md:py-24",
        bordered && "border-t",
        className
      )}
    >
      <div className={SECTION_CONTAINER}>{children}</div>
    </section>
  );
}
