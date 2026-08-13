"use client";

import Link from "next/link";
import { Activity, ArrowRight, BookOpen, ShieldCheck, Users } from "lucide-react";
import {
  SiGin,
  SiGo,
  SiJsonwebtokens,
  SiNatsdotio,
  SiNextdotjs,
  SiOpentelemetry,
  SiOwasp,
  SiPostgresql,
  SiReact,
  SiRedis,
  SiStripe,
  SiTailwindcss,
  SiTemporal,
} from "@icons-pack/react-simple-icons";
import { m, useReducedMotion } from "motion/react";
import { Area, AreaChart } from "recharts";
import { AspectRatio } from "@/components/ui/aspect-ratio";
import { Button } from "@/components/ui/button";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { hero, heroStack } from "@/components/brand/home/home-content";
import { SECTION_CONTAINER } from "@/components/brand/home/section";
import {
  Grain,
  Marquee,
  MaskLine,
  Pattern,
  TEXT_EASE,
  Typewriter,
  Vignette,
} from "@/components/brand/home/visuals";
import { useAuthStore } from "@/lib/auth-store";

/** 跑马灯图标：与 `heroStack` 按名称对应，缺图标的条目退化为纯文字 */
const STACK_ICONS: Record<string, React.ComponentType<{ className?: string }>> = {
  "Go 1.26": SiGo,
  Gin: SiGin,
  PostgreSQL: SiPostgresql,
  Redis: SiRedis,
  "NATS JetStream": SiNatsdotio,
  Temporal: SiTemporal,
  OpenTelemetry: SiOpentelemetry,
  "Coraza WAF": SiOwasp,
  "JWT / Passkey": SiJsonwebtokens,
  Stripe: SiStripe,
  "Next.js 16": SiNextdotjs,
  "React 19": SiReact,
  "Tailwind CSS 4": SiTailwindcss,
};

/**
 * 首屏。
 *
 * 这一版换掉的是**结构**，不只是配色：
 *
 * - 旧的「胶囊徽章 + 一句定位 + 一句副标题」是每个 SaaS 落地页都长的样子，
 *   它把最贵的那块版面用在了一句谁都能写的话上。现在顶部是一行等宽元信息
 *   （编号 / 产品名 / 协议版本），像仪器面板的标签条，而不是一枚装饰徽章。
 * - 版面中央是**三个问句**，与冷开场逐字打出来的是同样三行。开场落幕、
 *   首屏接住，读者会认出这是同一件事的延续；它们同时又是三个能力域的名字。
 * - 蓝紫渐变字与大团发光全部删除 —— 那是生成式配色最容易被认出来的两样东西。
 *   现在只有墨色、一档暖铜强调，以及一层几乎看不见的胶片颗粒。
 */
export function HeroSection() {
  const reduced = useReducedMotion();
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
    // -mt-16 把首屏拉到顶栏底下：顶栏在顶部是透明的，不这样做的话底纹从
    // 顶栏下沿才开始，页面最上方会横着一道没有网格的空带，像渲染缺了一块。
    <section className="relative -mt-16 overflow-hidden border-b">
      <Pattern
        size={64}
        mask="linear-gradient(180deg, #000 0%, #000 55%, transparent 92%)"
        className="opacity-70"
      />
      <Vignette size="82% 76%" />
      <Grain />

      <div className={`${SECTION_CONTAINER} relative pt-28 pb-12 md:pt-36 md:pb-16`}>
        {/* 元信息条：编号 / 产品 / 版本，两侧压细线，像仪器面板的标签 */}
        <m.div
          {...fade(0.05)}
          className="flex items-center gap-3 border-y py-2.5 font-mono text-[10px] tracking-[0.24em] text-muted-foreground uppercase md:text-[11px]"
        >
          <span style={{ color: "var(--home-accent)" }}>{hero.meta.index}</span>
          <span className="h-3 w-px bg-border" aria-hidden />
          <Typewriter text={hero.meta.label} delay={0.35} duration={1.1} className="min-w-0" />
          <span className="ml-auto hidden shrink-0 sm:inline">{hero.meta.version}</span>
        </m.div>

        <div className="mt-10 grid gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,25rem)] lg:gap-14 xl:grid-cols-[minmax(0,1fr)_minmax(0,30rem)]">
          <div>
            {/* 标题逐行从自己那一行的下沿升上来，像被印出来的；
                淡入加位移做不出这个效果，那只是元素在飘。 */}
            <h1 className="text-[clamp(2.3rem,6.2vw,4.5rem)] leading-[1.06] font-semibold tracking-tight">
              <MaskLine delay={0.25}>{hero.title}</MaskLine>
              <MaskLine
                delay={0.36}
                className="mt-1 text-[0.52em] leading-snug font-normal text-muted-foreground"
              >
                {hero.subtitle}
              </MaskLine>
            </h1>

            <m.div {...fade(0.6)} className="mt-8 max-w-xl">
              <p className="text-sm leading-relaxed text-pretty text-muted-foreground">
                {hero.description}
              </p>
            </m.div>

            {/* 三个能力域：与冷开场的三拍一一对应，读者会认出这是同一份目录 */}
            <dl className="mt-8 grid grid-cols-1 border-t sm:grid-cols-3">
              {hero.pillars.map((pillar, index) => (
                <m.div
                  key={pillar.label}
                  {...fade(0.66 + index * 0.08)}
                  className="border-b py-3 sm:border-b-0 sm:pr-5 sm:not-first:border-l sm:not-first:pl-5"
                >
                  <dt className="flex items-center gap-2 text-sm font-medium">
                    <span
                      className="size-1.5 shrink-0"
                      style={{ background: "var(--home-accent)" }}
                      aria-hidden
                    />
                    {pillar.label}
                  </dt>
                  <dd className="mt-1.5 font-mono text-[11px] tracking-wide text-muted-foreground">
                    {pillar.items}
                  </dd>
                </m.div>
              ))}
            </dl>

            {/* 两个 CTA 是并列的两件事，不是一个分段控件，因此用间距分开 */}
            <m.div {...fade(0.9)} className="mt-9 flex flex-col gap-4">
              <div className="flex flex-wrap gap-3 max-sm:flex-col">
                <Button asChild size="lg" className="rounded-none">
                  <Link href={authenticated ? "/overview" : "/login"}>
                    {authenticated ? hero.primary.authed : hero.primary.guest}
                    <ArrowRight />
                  </Link>
                </Button>
                <Button asChild size="lg" variant="outline" className="rounded-none">
                  <Link href={hero.secondary.href}>
                    <BookOpen />
                    {hero.secondary.label}
                  </Link>
                </Button>
              </div>
              <p className="font-mono text-[11px] tracking-wide text-muted-foreground">
                {hero.footnote}
              </p>
            </m.div>
          </div>

          <m.div {...fade(1)} className="max-lg:-mx-1">
            <ConsolePreview />
          </m.div>
        </div>
      </div>

      {/* 技术栈跑马灯：说清这东西真实由什么构成，比一行形容词更有说服力 */}
      <div className="relative border-t py-4">
        <Marquee duration={52}>
          {heroStack.map((name) => {
            const Icon = STACK_ICONS[name];
            return (
              <span
                key={name}
                className="mx-5 flex items-center gap-2 font-mono text-xs tracking-wide whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground md:mx-7"
              >
                {Icon ? <Icon className="size-3.5 shrink-0" /> : null}
                {name}
              </span>
            );
          })}
        </Marquee>
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
    // 直角 + 实边框，不用圆角卡和投影：投影与大圆角是"漂浮玻璃卡"那一套的底座，
    // 而这一页要的是印刷品的硬边。
    <div className="overflow-hidden border bg-card">
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
            <div key={tile.label} className="border bg-background/60 p-2.5">
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
        <div className="border bg-background/60 p-2">
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
              className="flex items-center gap-2 border bg-background/60 px-2.5 py-1.5"
            >
              <span
                className="size-1.5 shrink-0"
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
