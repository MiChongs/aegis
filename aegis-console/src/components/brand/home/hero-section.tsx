"use client";

import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { GrainGradient } from "@paper-design/shaders-react";
import { Activity, ArrowRight, ShieldCheck, Users } from "lucide-react";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { Area, AreaChart } from "recharts";
import { AspectRatio } from "@/components/ui/aspect-ratio";
import { Button } from "@/components/ui/button";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { hero } from "@/components/brand/home/home-content";
import { SECTION_CONTAINER } from "@/components/brand/home/section";
import { MaskLine, TEXT_EASE } from "@/components/brand/home/visuals";
import { useAuthStore } from "@/lib/auth-store";
import { cn } from "@/lib/utils";

/* ── 首屏着色器取色 ──
   淡紫双色 GrainGradient 静帧（speed=0，本身不动画），底部再以一层渐变融回
   页面背景。色值全部来自 globals.css 的令牌，深浅两套各给一份：浅色是浅底上
   的淡紫，深色是近黑底上的深紫。深色若沿用浅色那两档，贴角色块会亮过前景字，
   标题、说明与顶栏右侧的按钮一起糊掉。 */
const HERO_SHADER_TOKENS = ["--background", "--home-hero-ring", "--home-hero-core"] as const;

/**
 * 盯 `<html>` 的 class，主题一换就重新取色。
 *
 * 不用 next-themes 的 `resolvedTheme` 驱动：它把 class 写到 DOM 是在
 * Provider 自己的 effect 里，而子组件的 effect 先于父组件执行 ——
 * 在 effect 里读到的永远是切换**前**那套颜色（表现为切到深色后首屏
 * 还是浅底，白字压在浅底上整段消失）。MutationObserver 回调发生在
 * class 真正落到 DOM 之后，不依赖 React 的执行顺序。
 */
function subscribeRootClass(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
  return () => observer.disconnect();
}

const shaderColorCache = new Map<string, string>();

/**
 * 任意 CSS 颜色 → `rgba(r, g, b, a)`。
 *
 * 着色器只解析 #hex / rgb() / hsl()，而令牌哪天换成 oklch()（shadcn 新版的
 * 默认写法）它会静默退回黑色。画一个像素再读回来，浏览器认得的写法就都能用。
 */
function toShaderColor(value: string) {
  const cached = shaderColorCache.get(value);
  if (cached) return cached;
  const context = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  if (!context) return value;
  context.fillStyle = value;
  context.fillRect(0, 0, 1, 1);
  const [r, g, b, a] = context.getImageData(0, 0, 1, 1).data;
  const color = `rgba(${r}, ${g}, ${b}, ${Math.round((a / 255) * 1000) / 1000})`;
  shaderColorCache.set(value, color);
  return color;
}

/** 快照是拼接后的字符串：值不变时引用也不变，useSyncExternalStore 才不会反复重渲 */
function readHeroPalette() {
  const computed = getComputedStyle(document.documentElement);
  return HERO_SHADER_TOKENS.map((token) =>
    toShaderColor(computed.getPropertyValue(token).trim())
  ).join("|");
}

/** 服务端与水合那一帧返回 null：着色器只在浏览器里挂载，挂上之后再淡入 */
function useHeroPalette() {
  const snapshot = useSyncExternalStore(subscribeRootClass, readHeroPalette, () => null);
  return useMemo(() => {
    if (!snapshot) return null;
    const [back, ring, core] = snapshot.split("|");
    return { back, colors: [ring, core] };
  }, [snapshot]);
}

/* ── 标题第二行：轮播打字机 ──
   逐字入场（自下而上 + 淡入）构成打字节奏，打完停留数秒，
   整行向上淡出后换下一条。全部短语等长（八字），轮换零位移。
   不用 filter: blur 做聚焦：逐字的模糊动画每帧都要重绘，循环播放会卡。 */

/** 每字入场间隔（秒）—— 打字机的「击键」节奏 */
const TYPE_STAGGER = 0.055;
/** 单字入场时长（秒） */
const TYPE_CHAR_DURATION = 0.45;
/** 整行打完后的停留时长（秒） */
const TYPE_HOLD = 2.6;
/** 首次入场前的静默（等标题第一行升起），与后续轮换前的静默（秒） */
const TYPE_FIRST_LEAD = 0.5;
const TYPE_CYCLE_LEAD = 0.15;

function RotatingTypewriter({
  phrases,
  className,
}: {
  phrases: readonly string[];
  className?: string;
}) {
  const reduced = useReducedMotion();
  // 只增不减的轮次计数：短语下标取模得出，同时兼作 AnimatePresence 的 key
  const [round, setRound] = useState(0);

  const text = phrases[round % phrases.length];
  const chars = useMemo(() => Array.from(text), [text]);
  // 首轮要给标题第一行让出入场时间，此后轮换只留一小拍静默
  const lead = round === 0 ? TYPE_FIRST_LEAD : TYPE_CYCLE_LEAD;

  useEffect(() => {
    if (reduced || phrases.length <= 1) return;
    const typedAt = lead + chars.length * TYPE_STAGGER + TYPE_CHAR_DURATION;
    const timer = setTimeout(
      () => setRound((current) => current + 1),
      (typedAt + TYPE_HOLD) * 1000
    );
    return () => clearTimeout(timer);
  }, [round, lead, chars.length, reduced, phrases.length]);

  if (reduced) return <span className={cn("block", className)}>{phrases[0]}</span>;

  return (
    <span className={cn("block", className)}>
      <AnimatePresence mode="wait" initial={false}>
        <m.span
          key={round}
          className="block"
          aria-label={text}
          exit={{
            opacity: 0,
            y: "-0.18em",
            transition: { duration: 0.32, ease: "easeIn" },
          }}
        >
          {chars.map((char, charIndex) => (
            <m.span
              key={`${round}-${charIndex}`}
              aria-hidden
              className="inline-block"
              initial={{ opacity: 0, y: "0.45em" }}
              animate={{ opacity: 1, y: 0 }}
              transition={{
                duration: TYPE_CHAR_DURATION,
                ease: TEXT_EASE,
                delay: lead + charIndex * TYPE_STAGGER,
              }}
            >
              {char === " " ? "\u00A0" : char}
            </m.span>
          ))}
        </m.span>
      </AnimatePresence>
    </span>
  );
}

/**
 * 首屏。全高全宽：着色器背景铺满整个视口，内容在视口内垂直居中。
 *
 * 版面只保留四样东西：一行定位、两行标题（第二行是品牌紫的轮播
 * 打字机短语）、一段说明、两个入口，右侧是控制台预览卡。元信息条、
 * 能力域三列、脚注与技术栈跑马灯全部移出 —— 它们的信息在下方分区
 * 各有正式位置。
 *
 * 文字一律自下而上入场（标题走 MaskLine 从行内下沿升起，其余走
 * 淡入上移）；轮播短语额外逐字入场。全部动效由 motion 驱动，
 * `prefers-reduced-motion` 下静止呈现第一条短语。
 */
export function HeroSection() {
  const reduced = useReducedMotion();
  const palette = useHeroPalette();
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);

  // 统一的自下而上入场：淡入 + 上移
  const rise = (delay: number) =>
    reduced
      ? {}
      : {
          initial: { opacity: 0, y: 24 },
          animate: { opacity: 1, y: 0 },
          transition: { duration: 0.7, ease: TEXT_EASE, delay },
        };

  return (
    // -mt-16 把首屏拉到顶栏底下：顶栏在顶部是透明的，着色器背景应当
    // 从页面最上沿开始，而不是从顶栏下沿开始。
    <section className="relative -mt-16 overflow-hidden">
      {/* 着色器底层：取到主题色再挂载，SSR 输出与首帧一致，挂载后淡入 */}
      {palette ? (
        <GrainGradient
          aria-hidden
          className="pointer-events-none absolute inset-0 animate-in fade-in duration-1000"
          colors={palette.colors}
          colorBack={palette.back}
          softness={0.51}
          intensity={0.5}
          noise={0.25}
          shape="corners"
          speed={0}
          scale={1.04}
          rotation={184}
        />
      ) : null}
      {/* 底部融回页面背景：首屏与下一分区之间不留硬边 */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-background to-transparent md:h-56"
      />

      {/* 全高：内容在视口内垂直居中，短视口下由上下内边距兜底 */}
      <div
        className={`${SECTION_CONTAINER} relative flex min-h-svh items-center pt-28 pb-24 md:pt-32 md:pb-32`}
      >
        <div className="grid w-full items-center gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,25rem)] lg:gap-14 xl:grid-cols-[minmax(0,1fr)_minmax(0,30rem)]">
          <div>

            {/* 第一行从自己那一行的下沿升上来，像被印出来的；
                第二行是轮播打字机：逐字淡入上移，打完停留后换下一条。
                中文大标题不收紧字距：tracking-tight 是给拉丁字形留的，
                汉字方块字挤在一起只会糊成一版，这里给一点正字距透气。 */}
            <h1 className="mt-4 text-[clamp(2.5rem,5.8vw,4.5rem)] leading-[1.18] font-semibold tracking-[0.02em]">
              <MaskLine delay={0.15}>{hero.title}</MaskLine>
              <RotatingTypewriter
                phrases={hero.titleAccents}
                className="text-(--home-hero-ink)"
              />
            </h1>

            <m.p
              {...rise(0.5)}
              className="mt-6 max-w-xl text-sm leading-relaxed tracking-[0.01em] text-pretty text-(--home-hero-muted) md:text-base"
            >
              {hero.description}
            </m.p>

            <m.div {...rise(0.65)} className="mt-8 flex flex-wrap gap-3 max-sm:flex-col">
              <Button asChild size="lg" className="rounded-full">
                <Link href={authenticated ? "/overview" : "/login"}>
                  {authenticated ? hero.primary.authed : hero.primary.guest}
                  <ArrowRight />
                </Link>
              </Button>
              <Button asChild size="lg" variant="outline" className="rounded-full">
                <Link href={hero.secondary.href}>{hero.secondary.label}</Link>
              </Button>
            </m.div>
          </div>
        </div>
      </div>
    </section>
  );
}
