"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { GrainGradient } from "@paper-design/shaders-react";
import { useTheme } from "next-themes";
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

/* ── 首屏着色器底色 ──
   淡紫双色 GrainGradient 静帧（speed=0，本身不动画）：浅紫贴边、中心留白，
   底部再以一层渐变融回页面背景。colorBack 跟随主题背景色，深浅两套各自成立。 */
const HERO_SHADER_COLORS = ["#a78bfa", "#e9d5ff"];

/** colorBack 兜底常量：与 globals.css 的 --background 保持一致 */
const HERO_SHADER_BACK = { light: "#f4f4f5", dark: "#09090b" } as const;

/**
 * 取当前主题下 `--background` 的实际渲染色。
 *
 * 不直接读 CSS 变量原文：着色器只解析 #hex / rgb() / hsl()，而令牌将来
 * 可能改成 oklch 等写法。改为量一个挂了 `bg-background` 的探针元素 ——
 * 浏览器把计算值序列化成 rgb()，主题切换、品牌自定义 CSS 全都自动生效。
 */
function useThemeBackdrop() {
  const { resolvedTheme } = useTheme();
  const probeRef = useRef<HTMLDivElement>(null);
  const [color, setColor] = useState<string | null>(null);

  useEffect(() => {
    const probe = probeRef.current;
    if (!probe) return;
    const measured = getComputedStyle(probe).backgroundColor;
    setColor(
      /^(#|rgb|hsl)/i.test(measured)
        ? measured
        : HERO_SHADER_BACK[resolvedTheme === "dark" ? "dark" : "light"]
    );
  }, [resolvedTheme]);

  return { probeRef, color };
}

/* ── 标题第二行：轮播打字机 ──
   逐字入场（自下而上 + 模糊聚焦）构成打字节奏，打完停留数秒，
   整行向上模糊退场后换下一条。全部短语等长（八字），轮换零位移。 */

/** 每字入场间隔（秒）—— 打字机的「击键」节奏 */
const TYPE_STAGGER = 0.055;
/** 单字从模糊到聚焦的时长（秒） */
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
            filter: "blur(8px)",
            transition: { duration: 0.32, ease: "easeIn" },
          }}
        >
          {chars.map((char, charIndex) => (
            <m.span
              key={`${round}-${charIndex}`}
              aria-hidden
              className="inline-block"
              initial={{ opacity: 0, y: "0.45em", filter: "blur(10px)" }}
              animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
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
 * 淡入上移）；轮播短语额外带逐字模糊聚焦。全部动效由 motion 驱动，
 * `prefers-reduced-motion` 下静止呈现第一条短语。
 */
export function HeroSection() {
  const reduced = useReducedMotion();
  const { probeRef, color: shaderBack } = useThemeBackdrop();
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
      {/* 主题背景色探针：着色器 colorBack 的取值来源 */}
      <div
        ref={probeRef}
        aria-hidden
        className="pointer-events-none absolute size-px bg-background opacity-0"
      />
      {/* 着色器底层：等探针量到主题色再挂载，SSR 输出与首帧一致，挂载后淡入 */}
      {shaderBack ? (
        <GrainGradient
          aria-hidden
          className="pointer-events-none absolute inset-0 animate-in fade-in duration-1000"
          colors={HERO_SHADER_COLORS}
          colorBack={shaderBack}
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
            <m.p
              {...rise(0.05)}
              className="text-sm font-medium tracking-[0.08em] text-muted-foreground"
            >
              {hero.eyebrow}
            </m.p>

            {/* 第一行从自己那一行的下沿升上来，像被印出来的；
                第二行是轮播打字机：逐字模糊聚焦入场，打完停留后换下一条。
                中文大标题不收紧字距：tracking-tight 是给拉丁字形留的，
                汉字方块字挤在一起只会糊成一版，这里给一点正字距透气。 */}
            <h1 className="mt-4 text-[clamp(2.5rem,5.8vw,4.5rem)] leading-[1.18] font-semibold tracking-[0.02em]">
              <MaskLine delay={0.15}>{hero.title}</MaskLine>
              <RotatingTypewriter
                phrases={hero.titleAccents}
                className="text-violet-600 dark:text-violet-400"
              />
            </h1>

            <m.p
              {...rise(0.5)}
              className="mt-6 max-w-xl text-sm leading-relaxed tracking-[0.01em] text-pretty text-muted-foreground md:text-base"
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

          <m.div {...rise(0.8)} className="max-lg:-mx-1">
            <ConsolePreview />
          </m.div>
        </div>
      </div>
    </section>
  );
}

/* ── 控制台预览卡 ── */

const previewChartConfig = {
  logins: { label: "登录", color: "var(--home-accent)" },
  sessions: { label: "会话", color: "var(--home-accent-alt)" },
} satisfies ChartConfig;

const previewChartData = [
  { t: "00", logins: 320, sessions: 210 },
  { t: "03", logins: 180, sessions: 140 },
  { t: "06", logins: 260, sessions: 190 },
  { t: "09", logins: 720, sessions: 480 },
  { t: "12", logins: 910, sessions: 640 },
  { t: "15", logins: 840, sessions: 700 },
  { t: "18", logins: 1180, sessions: 860 },
  { t: "21", logins: 960, sessions: 720 },
];

const previewTiles = [
  { icon: Users, label: "在线会话", value: "8,412", delta: "+6.2%" },
  { icon: Activity, label: "今日登录", value: "23,910", delta: "+12.4%" },
  { icon: ShieldCheck, label: "风控拦截", value: "148", delta: "-3.1%" },
];

const previewEvents = [
  { tone: "ok", text: "app_web · 会员开通", meta: "刚刚" },
  { tone: "warn", text: "风控命中 · 异地登录", meta: "12s" },
  { tone: "muted", text: "app_ios · Passkey 绑定", meta: "48s" },
];

function ConsolePreview() {
  return (
    // 圆角与柔和投影：首屏底色是柔和的淡紫渐变，硬边直角卡在这里
    // 会显得像贴错了页面；圆角卡与药丸按钮属于同一套形状语言。
    <div className="overflow-hidden rounded-2xl border bg-card shadow-[0_8px_32px_-12px_rgb(0_0_0/0.18)]">
      <div className="flex items-center gap-2 border-b bg-muted/40 px-3 py-2">
        <span className="flex gap-1.5" aria-hidden>
          <span className="size-2 rounded-full bg-border" />
          <span className="size-2 rounded-full bg-border" />
          <span className="size-2 rounded-full bg-border" />
        </span>
        <span className="ml-1 truncate font-mono text-[11px] text-muted-foreground">
          aegis.console <span className="text-foreground">/ 概览</span>
        </span>
        <span className="ml-auto flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground">
          <span
            className="size-1.5 rounded-full"
            style={{ background: "var(--home-accent)" }}
            aria-hidden
          />
          live
        </span>
      </div>

      <div className="space-y-2.5 p-2.5">
        <div className="grid grid-cols-3 gap-2">
          {previewTiles.map((tile) => (
            <div key={tile.label} className="rounded-lg border bg-background/60 p-2.5">
              <tile.icon className="size-3.5 text-muted-foreground" strokeWidth={1.75} />
              <p
                className="mt-2 text-sm font-semibold tracking-tight tabular-nums"
                style={{ fontFamily: "var(--font-data)" }}
              >
                {tile.value}
              </p>
              <p className="mt-0.5 truncate text-[10px] text-muted-foreground">{tile.label}</p>
              <p
                className="font-mono text-[10px] tabular-nums"
                style={{
                  color: tile.delta.startsWith("-")
                    ? "var(--home-accent-warm)"
                    : "var(--home-accent)",
                }}
              >
                {tile.delta}
              </p>
            </div>
          ))}
        </div>

        {/* 趋势图：用 AspectRatio 固定比例，卡片宽度变化时图不会被压扁 */}
        <div className="rounded-lg border bg-background/60 p-2">
          <AspectRatio ratio={16 / 7}>
            <ChartContainer config={previewChartConfig} className="aspect-auto size-full">
              <AreaChart data={previewChartData} margin={{ top: 6, right: 4, bottom: 0, left: 4 }}>
                <defs>
                  <linearGradient id="home-hero-logins" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-logins)" stopOpacity={0.3} />
                    <stop offset="100%" stopColor="var(--color-logins)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <Area
                  dataKey="sessions"
                  type="linear"
                  stroke="var(--color-sessions)"
                  strokeWidth={1}
                  strokeDasharray="3 3"
                  fill="none"
                  isAnimationActive={false}
                />
                <Area
                  dataKey="logins"
                  type="linear"
                  stroke="var(--color-logins)"
                  strokeWidth={1.75}
                  fill="url(#home-hero-logins)"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ChartContainer>
          </AspectRatio>
        </div>

        <ul className="space-y-1.5">
          {previewEvents.map((event) => (
            <li
              key={event.text}
              className="flex items-center gap-2 rounded-lg border bg-background/60 px-2.5 py-1.5"
            >
              <span
                className="size-1.5 shrink-0 rounded-full"
                style={{
                  background:
                    event.tone === "warn"
                      ? "var(--home-accent-warm)"
                      : event.tone === "ok"
                        ? "var(--home-accent)"
                        : "var(--home-accent-alt)",
                }}
                aria-hidden
              />
              <span className="truncate text-[11px] text-muted-foreground">{event.text}</span>
              <span className="ml-auto shrink-0 font-mono text-[10px] text-muted-foreground/70">
                {event.meta}
              </span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
