"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { GrainGradient } from "@paper-design/shaders-react";
import { useTheme } from "next-themes";
import { Activity, ArrowRight, ShieldCheck, Users } from "lucide-react";
import { m, useReducedMotion } from "motion/react";
import { Area, AreaChart } from "recharts";
import { AspectRatio } from "@/components/ui/aspect-ratio";
import { Button } from "@/components/ui/button";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { hero } from "@/components/brand/home/home-content";
import { SECTION_CONTAINER } from "@/components/brand/home/section";
import { MaskLine, TEXT_EASE } from "@/components/brand/home/visuals";
import { useAuthStore } from "@/lib/auth-store";

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

/**
 * 首屏。
 *
 * 版面只保留四样东西：一行定位、两行标题（第二行用品牌紫）、一段说明、
 * 两个入口，右侧是控制台预览卡。元信息条、能力域三列、脚注与技术栈
 * 跑马灯全部移出 —— 它们的信息在下方分区各有正式位置。
 *
 * 背景是一张 GrainGradient 静帧：淡紫色贴边、版心留白，colorBack 实时
 * 跟随主题背景色，底部渐变融回页面，不与正文抢对比度。
 */
export function HeroSection() {
  const reduced = useReducedMotion();
  const { probeRef, color: shaderBack } = useThemeBackdrop();
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);

  const fade = (delay: number) =>
    reduced
      ? {}
      : {
          initial: { opacity: 0, y: 14 },
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

      <div className={`${SECTION_CONTAINER} relative pt-36 pb-20 md:pt-44 md:pb-28`}>
        <div className="grid items-center gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,25rem)] lg:gap-14 xl:grid-cols-[minmax(0,1fr)_minmax(0,30rem)]">
          <div>
            <m.p
              {...fade(0.05)}
              className="text-sm font-medium tracking-wide text-muted-foreground"
            >
              {hero.eyebrow}
            </m.p>

            {/* 标题逐行从自己那一行的下沿升上来，像被印出来的；
                淡入加位移做不出这个效果，那只是元素在飘。 */}
            <h1 className="mt-4 text-[clamp(2.4rem,5.6vw,4.25rem)] leading-[1.12] font-semibold tracking-tight">
              <MaskLine delay={0.15}>{hero.title}</MaskLine>
              <MaskLine delay={0.28} className="text-violet-600 dark:text-violet-400">
                {hero.titleAccent}
              </MaskLine>
            </h1>

            <m.p
              {...fade(0.5)}
              className="mt-6 max-w-xl text-sm leading-relaxed text-pretty text-muted-foreground md:text-base"
            >
              {hero.description}
            </m.p>

            <m.div {...fade(0.65)} className="mt-8 flex flex-wrap gap-3 max-sm:flex-col">
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

          <m.div {...fade(0.8)} className="max-lg:-mx-1">
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
