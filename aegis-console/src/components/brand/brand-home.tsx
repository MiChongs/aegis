"use client";

import { useRef } from "react";
import Link from "next/link";
import {
  LazyMotion,
  domAnimation,
  m,
  useReducedMotion,
  useScroll,
  useTransform,
  type MotionValue,
} from "motion/react";
import {
  Shield,
  Users,
  Layers,
  Lock,
  Globe,
  Zap,
  Database,
  Eye,
  Workflow,
  ArrowRight,
  Server,
  Bell,
  Key,
  Activity,
  FileText,
  Cloud,
} from "lucide-react";
import {
  SiGo,
  SiPostgresql,
  SiRedis,
  SiNatsdotio,
  SiTemporal,
  SiOwasp,
  SiJsonwebtokens,
  SiNextdotjs,
  SiReact,
  SiOpentelemetry,
  SiGin,
  SiTailwindcss,
} from "@icons-pack/react-simple-icons";
import { PublicEntryActions } from "@/components/brand/public-entry-actions";
import { PublicHeader } from "@/components/brand/public-header";
import { LegalFooter } from "@/components/legal/legal-footer";
import { appConfig } from "@/lib/env";

/* ── 数据 ── */

const stickyFeatures = [
  {
    icon: Shield,
    title: "安全认证",
    en: "Secure Authentication",
    desc: "JWT 签名令牌 + OAuth2 社交登录 + TOTP / Passkey 多因子认证，构建纵深防御体系。",
    highlight: "6 种 OAuth2 提供商",
  },
  {
    icon: Users,
    title: "多租户隔离",
    en: "Multi-Tenant Isolation",
    desc: "每个应用独立用户库、权限策略和配置空间，数据天然隔离，互不干扰。",
    highlight: "应用级数据隔离",
  },
  {
    icon: Eye,
    title: "安全防护",
    en: "Security Infrastructure",
    desc: "Coraza WAF + OWASP CRS v4 规则集，IP 风控、防重放攻击、智能限流多层联防。",
    highlight: "WAF + 防重放 + 限流",
  },
  {
    icon: Zap,
    title: "实时通信",
    en: "Realtime Engine",
    desc: "WebSocket 双向通道 + NATS JetStream 事件总线，毫秒级推送到每个在线终端。",
    highlight: "毫秒级推送",
  },
];

const capabilities = [
  { icon: Lock, label: "RBAC 权限", en: "Role-Based Access", desc: "Casbin 细粒度策略引擎" },
  { icon: Globe, label: "OAuth2 集成", en: "Social Login", desc: "QQ / 微信 / GitHub / Google" },
  { icon: Database, label: "多云存储", en: "Multi-Cloud", desc: "Azure / S3 / OSS / COS / Qiniu" },
  { icon: Workflow, label: "工作流引擎", en: "Workflow", desc: "可视化编排 + Temporal" },
  { icon: Layers, label: "全栈监控", en: "Monitoring", desc: "组件健康度 + 崩溃恢复" },
  { icon: Bell, label: "通知推送", en: "Notifications", desc: "站内信 + WebSocket 实时" },
  { icon: Key, label: "Passkey", en: "WebAuthn", desc: "无密码生物认证" },
  { icon: Activity, label: "积分签到", en: "Points", desc: "自动签到 + 经验等级" },
  { icon: FileText, label: "版本管理", en: "Releases", desc: "多渠道版本发布" },
];

const techStack: { layer: string; tech: string; icon: React.ComponentType<{ className?: string; size?: number; style?: React.CSSProperties }>; color: string }[] = [
  { layer: "Language", tech: "Go", icon: SiGo, color: "#00ADD8" },
  { layer: "HTTP Framework", tech: "Gin v1.12", icon: SiGin, color: "#008ECF" },
  { layer: "Primary Database", tech: "PostgreSQL 17", icon: SiPostgresql, color: "#4169E1" },
  { layer: "Cache & Session", tech: "Redis 8", icon: SiRedis, color: "#FF4438" },
  { layer: "Message Queue", tech: "NATS JetStream", icon: SiNatsdotio, color: "#27AAE1" },
  { layer: "Workflow Engine", tech: "Temporal", icon: SiTemporal, color: "#8B5CF6" },
  { layer: "WAF", tech: "Coraza + OWASP CRS v4", icon: SiOwasp, color: "#F0F0F0" },
  { layer: "Auth", tech: "JWT + Passkey + TOTP", icon: SiJsonwebtokens, color: "#FB015B" },
  { layer: "Frontend", tech: "Next.js 16", icon: SiNextdotjs, color: "#FFFFFF" },
  { layer: "UI Library", tech: "React 19", icon: SiReact, color: "#61DAFB" },
  { layer: "Styling", tech: "Tailwind CSS 4", icon: SiTailwindcss, color: "#06B6D4" },
  { layer: "Observability", tech: "OpenTelemetry", icon: SiOpentelemetry, color: "#F5A800" },
];

const stats = [
  { value: "20+", label: "服务模块", en: "Service Modules" },
  { value: "6", label: "OAuth2 提供商", en: "OAuth2 Providers" },
  { value: "7", label: "存储后端", en: "Storage Backends" },
  { value: "10+", label: "支付方式", en: "Payment Methods" },
];

/* ================================================================ */

export function BrandHome() {
  return (
    <LazyMotion features={domAnimation}>
      <div className="relative bg-[#060a12] text-white">

        <PublicHeader current="home" navLabel="主页导航" />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 1: HERO — 一屏，一句话，两个按钮
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <HeroSection />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 2: STATS — 数字滚入 + 视差偏移
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <StatsBar />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 3: STICKY FEATURES — Apple 风格逐帧
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <StickyFeatures />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 4: CAPABILITIES — 视差网格
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <CapabilitiesGrid />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 5: ARCHITECTURE — 架构层图
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <ArchitectureLayers />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 6: TECH STACK — 横向视差滚动
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <HorizontalTechStack items={techStack} />

        {/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
            SECTION 7: CTA — 缩放入场
           ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */}
        <section className="relative px-5 py-20 md:px-8 md:py-32 xl:px-12">
          <div className="mx-auto max-w-360">
            <m.div
              initial={{ opacity: 0, y: 40 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true, margin: "-80px" }}
              transition={{ duration: 0.7, ease: [0.22, 1, 0.36, 1] }}
              className="relative flex flex-col items-center gap-6 overflow-hidden rounded-2xl border border-white/6 px-6 py-14 text-center md:gap-8 md:rounded-3xl md:px-16 md:py-24"
              style={{
                background: "radial-gradient(ellipse 60% 80% at 50% 0%, rgba(255,255,255,0.04), transparent), rgba(255,255,255,0.015)",
              }}
            >
              {/* 装饰性网格 */}
              <div
                className="pointer-events-none absolute inset-0 opacity-20"
                style={{
                  backgroundImage: "linear-gradient(rgba(255,255,255,0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.04) 1px, transparent 1px)",
                  backgroundSize: "40px 40px",
                  maskImage: "radial-gradient(ellipse 60% 60% at 50% 50%, black, transparent)",
                }}
              />
              <h2 className="relative text-2xl font-bold tracking-tight md:text-5xl">开始使用</h2>
              <p className="relative max-w-md text-sm text-white/45 leading-relaxed md:text-base">
                几分钟内完成部署，获得完整的多租户用户管理能力。
              </p>
              <Link
                href="/login"
                className="relative inline-flex items-center gap-2 rounded-full bg-white px-7 py-3 text-sm font-semibold text-[#101114] transition-all hover:brightness-95 md:px-8 md:py-3.5"
              >
                进入管理 <ArrowRight className="size-4" />
              </Link>
            </m.div>
          </div>
        </section>

        {/* ━━ FOOTER ━━ */}
        <footer className="relative border-t border-white/6 px-5 py-8 md:px-8 xl:px-12">
          <div className="mx-auto max-w-360">
            <LegalFooter />
          </div>
        </footer>
      </div>
    </LazyMotion>
  );
}

/* ================================================================
   HERO — 首屏

   这一版拆掉的东西，以及为什么：

   - **滚动条纹背景（LoginBackground）**：一整块持续位移的图案压在标题底下，
     视线被它牵着走，而首屏唯一要读的是那句话和那两个按钮。
   - **130vh + sticky 缩放视差**：为了播一段"标题缩小淡出"的动画，凭空多要了
     30vh 的滚动距离 —— 用户以为在往下翻内容，实际什么都没翻到。
   - **超大 AEGIS 字标**（clamp 到 11rem）：它只说明了自己叫什么，
     而访客第一秒要知道的是这东西干什么用。品牌标识留在导航栏里就够了。

   换上的是一屏之内讲完的三段：定位 → 一句主张 → 两个入口，
   底下压一条静态的技术栈标记 —— 它是"这东西真实由什么构成"，不是装饰。
   ================================================================ */

const HERO_MARKS = [
  { icon: SiGo, label: "Go 1.26" },
  { icon: SiPostgresql, label: "PostgreSQL" },
  { icon: SiRedis, label: "Redis" },
  { icon: SiNatsdotio, label: "NATS JetStream" },
  { icon: SiTemporal, label: "Temporal" },
] as const;

const HERO_EASE: [number, number, number, number] = [0.22, 1, 0.36, 1];

function HeroSection() {
  const reduced = useReducedMotion();

  const rise = (delay: number) =>
    reduced
      ? { initial: false as const, animate: { opacity: 1, y: 0 } }
      : {
          initial: { opacity: 0, y: 20 },
          animate: { opacity: 1, y: 0 },
          transition: { duration: 0.7, ease: HERO_EASE, delay },
        };

  return (
    <section className="relative flex min-h-[calc(100svh-4rem)] items-center overflow-hidden px-5 py-20 md:px-8 md:py-24 xl:px-12">
      {/* 唯一一层背景：顶部一道极淡的静态辉光，把导航栏坐进画面里。
          不动、不循环、不叠颗粒 —— 它的作用是别让整块纯色显得是渲染失败。 */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            "radial-gradient(ellipse 70% 45% at 50% -10%, rgba(255,255,255,0.05), transparent 70%)",
        }}
      />

      <div className="relative z-10 mx-auto w-full max-w-360">
        <div className="max-w-4xl">
          <m.div {...rise(0.05)} className="mb-7 inline-flex items-center gap-2.5 rounded-full border border-white/10 bg-white/3 py-1.5 pr-4 pl-2.5 text-xs text-white/60">
            <span className="relative flex size-1.5">
              <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400/70" />
              <span className="relative inline-flex size-1.5 rounded-full bg-emerald-400" />
            </span>
            多租户身份底座 · {appConfig.platformName} Identity Fabric
          </m.div>

          <m.h1
            {...rise(0.12)}
            className="text-[clamp(2.25rem,6.2vw,4.75rem)] leading-[1.06] font-semibold tracking-tight text-balance"
          >
            为每一个应用，
            <br />
            建立可治理的身份边界。
          </m.h1>

          <m.p
            {...rise(0.2)}
            className="mt-6 max-w-2xl text-base leading-8 text-white/55 md:text-lg md:leading-9"
          >
            认证、授权、组织、资产与风控收在同一套底座里。
            每个应用一套独立的用户库与策略，接入方只需要认识一个命名空间。
          </m.p>

          <m.div {...rise(0.28)} className="mt-10">
            <PublicEntryActions secondaryHref="/developers" secondaryLabel="查看接入文档" />
          </m.div>
        </div>

        {/* 技术栈标记：说清这东西真实跑在什么上面，比任何形容词都短 */}
        <m.div
          {...rise(0.38)}
          className="mt-16 flex flex-wrap items-center gap-x-8 gap-y-4 border-t border-white/6 pt-8 md:mt-20 md:gap-x-12"
        >
          {HERO_MARKS.map((mark) => (
            <span key={mark.label} className="flex items-center gap-2 text-xs text-white/45 md:text-[13px]">
              <mark.icon className="size-4 shrink-0 text-white/35" />
              {mark.label}
            </span>
          ))}
        </m.div>
      </div>
    </section>
  );
}

/* ================================================================
   STATS BAR — 数字从下方滑入 + 不同速度视差
   ================================================================ */
function StatsBar() {
  const ref = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target: ref, offset: ["start end", "end start"] });
  const bgY = useTransform(scrollYProgress, [0, 1], [40, -40]);

  return (
    <section ref={ref} className="relative overflow-hidden border-y border-white/6 py-16 md:py-20">
      {/* 视差背景光 */}
      <m.div
        className="pointer-events-none absolute inset-0"
        style={{
          y: bgY,
          background: "radial-gradient(ellipse 80% 100% at 50% 120%, rgba(255,255,255,0.04), transparent)",
        }}
      />
      <div className="relative z-10 mx-auto grid max-w-360 grid-cols-2 gap-8 px-5 md:grid-cols-4 md:gap-6 md:px-8 xl:px-12">
        {stats.map((s, i) => (
          <m.div
            key={s.label}
            initial={{ opacity: 0, y: 30 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true, margin: "-40px" }}
            transition={{ duration: 0.5, delay: i * 0.1, ease: [0.22, 1, 0.36, 1] }}
            className="text-center"
          >
            <span
              className="block text-3xl font-bold tracking-tight md:text-5xl"
              style={{ fontFamily: "var(--font-data)" }}
            >
              {s.value}
            </span>
            <span className="mt-2 block text-sm text-white/50">{s.label}</span>
            <span className="block text-[11px] text-white/25">{s.en}</span>
          </m.div>
        ))}
      </div>
    </section>
  );
}

/* ================================================================
   STICKY FEATURES — Apple 风格：桌面 sticky / 移动端卡片列表
   ================================================================ */
function StickyFeatures() {
  const containerRef = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target: containerRef, offset: ["start start", "end end"] });

  return (
    <>
      {/* 桌面端 */}
      <div ref={containerRef} className="hidden md:block" style={{ height: `${(stickyFeatures.length + 1) * 150}vh` }}>
        <div className="sticky top-0 flex h-screen items-center overflow-hidden">
          {/* 背景光晕 */}
          <m.div
            className="pointer-events-none absolute inset-0"
            style={{
              opacity: useTransform(scrollYProgress, [0, 0.15, 0.85, 1], [0, 1, 1, 0]),
              background: "radial-gradient(ellipse 50% 60% at 50% 50%, rgba(255,255,255,0.03), transparent)",
            }}
          />
          {/* 装饰线条 — 跟随滚动 */}
          <m.div
            className="pointer-events-none absolute left-1/2 top-0 h-full w-px -translate-x-1/2 hidden lg:block"
            style={{
              opacity: useTransform(scrollYProgress, [0, 0.1, 0.9, 1], [0, 0.08, 0.08, 0]),
              background: "linear-gradient(180deg, transparent, rgba(255,255,255,0.15), transparent)",
            }}
          />

          <div className="relative z-10 mx-auto w-full max-w-360 px-8 xl:px-12">
            <div className="grid items-center gap-20 lg:grid-cols-2">
              <div>
                <m.h2
                  className="mb-2 text-3xl font-bold tracking-tight md:text-5xl"
                  style={{ opacity: useTransform(scrollYProgress, [0, 0.08], [0, 1]) }}
                >
                  核心能力
                </m.h2>
                <m.p
                  className="mb-12 text-base text-white/40"
                  style={{ opacity: useTransform(scrollYProgress, [0, 0.08], [0, 1]) }}
                >
                  Core Capabilities
                </m.p>

                <div className="space-y-6">
                  {stickyFeatures.map((f, i) => (
                    <StickyFeatureRow key={f.title} feature={f} index={i} total={stickyFeatures.length} progress={scrollYProgress} />
                  ))}
                </div>
              </div>

              {/* 右侧：大图标 + 高亮标签 */}
              <div className="hidden lg:flex lg:items-center lg:justify-center">
                <div className="relative size-80">
                  {stickyFeatures.map((f, i) => (
                    <StickyFeatureVisual key={f.title} feature={f} index={i} total={stickyFeatures.length} progress={scrollYProgress} />
                  ))}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 移动端 */}
      <section className="px-5 py-20 md:hidden">
        <h2 className="mb-3 text-2xl font-bold tracking-tight">核心能力</h2>
        <p className="mb-8 text-sm text-white/40">Core Capabilities</p>
        <div className="space-y-4">
          {stickyFeatures.map((f, i) => (
            <m.div
              key={f.title}
              initial={{ opacity: 0, y: 24 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true, margin: "-30px" }}
              transition={{ duration: 0.45, delay: i * 0.08, ease: [0.22, 1, 0.36, 1] }}
              className="flex items-start gap-3 rounded-2xl border border-white/6 bg-white/2 p-4"
            >
              <f.icon className="mt-0.5 size-5 shrink-0 text-white/50" strokeWidth={1.5} />
              <div>
                <h3 className="text-sm font-semibold">
                  {f.title}
                  <span className="ml-1.5 text-[11px] font-normal text-white/30">{f.en}</span>
                </h3>
                <p className="mt-1 text-xs leading-relaxed text-white/45">{f.desc}</p>
                <span className="mt-2 inline-block rounded-full border border-white/8 px-2.5 py-0.5 text-[10px] text-white/40">
                  {f.highlight}
                </span>
              </div>
            </m.div>
          ))}
        </div>
      </section>
    </>
  );
}

function StickyFeatureRow({ feature, index, total, progress }: {
  feature: (typeof stickyFeatures)[number]; index: number; total: number; progress: MotionValue<number>;
}) {
  const seg = 1 / (total + 1);
  const start = (index + 0.5) * seg, peak = (index + 1) * seg, end = (index + 1.5) * seg;
  const opacity = useTransform(progress, [start, peak, end], [0.12, 1, 0.12]);
  const x = useTransform(progress, [start, peak, end], [-6, 0, 6]);
  const scale = useTransform(progress, [start, peak, end], [0.97, 1, 0.97]);

  return (
    <m.div style={{ opacity, x, scale }} className="flex items-start gap-4 origin-left">
      <feature.icon className="mt-1 size-5 shrink-0 text-white/70" strokeWidth={1.5} />
      <div>
        <h3 className="text-base font-semibold">
          {feature.title}
          <span className="ml-2 text-xs font-normal text-white/30">{feature.en}</span>
        </h3>
        <p className="mt-1.5 text-sm leading-relaxed text-white/50">{feature.desc}</p>
        <m.span
          className="mt-2 inline-block rounded-full border border-white/10 px-3 py-0.5 text-[11px] text-white/40"
          style={{ opacity: useTransform(progress, [start, peak, end], [0, 1, 0]) }}
        >
          {feature.highlight}
        </m.span>
      </div>
    </m.div>
  );
}

function StickyFeatureVisual({ feature, index, total, progress }: {
  feature: (typeof stickyFeatures)[number];
  index: number; total: number; progress: MotionValue<number>;
}) {
  const seg = 1 / (total + 1);
  const start = (index + 0.5) * seg, peak = (index + 1) * seg, end = (index + 1.5) * seg;
  const opacity = useTransform(progress, [start, peak - 0.02, peak, peak + 0.02, end], [0, 0, 1, 1, 0]);
  const scale = useTransform(progress, [start, peak, end], [0.8, 1, 0.8]);
  const y = useTransform(progress, [start, peak, end], [30, 0, -30]);

  return (
    <m.div className="absolute inset-0 flex flex-col items-center justify-center gap-5" style={{ opacity, scale, y }}>
      <div
        className="flex size-52 items-center justify-center rounded-3xl border border-white/8"
        style={{ background: "radial-gradient(circle at 30% 30%, rgba(255,255,255,0.08), rgba(255,255,255,0.02))" }}
      >
        <feature.icon className="size-20 text-white/25" strokeWidth={0.8} />
      </div>
      <span className="rounded-full border border-white/10 bg-white/4 px-4 py-1.5 text-xs font-medium text-white/60">
        {feature.highlight}
      </span>
    </m.div>
  );
}

/* ================================================================
   CAPABILITIES GRID — 视差 + 交错入场
   ================================================================ */
function CapabilitiesGrid() {
  const ref = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target: ref, offset: ["start end", "end start"] });
  const bgY = useTransform(scrollYProgress, [0, 1], [60, -60]);

  return (
    <section ref={ref} className="relative overflow-hidden px-5 py-20 md:px-8 md:py-32 xl:px-12">
      {/* 视差背景光斑 */}
      <m.div
        className="pointer-events-none absolute inset-0"
        style={{
          y: bgY,
          background: "radial-gradient(ellipse 40% 50% at 70% 30%, rgba(255,255,255,0.03), transparent), radial-gradient(ellipse 30% 40% at 20% 80%, rgba(255,255,255,0.02), transparent)",
        }}
      />

      <div className="relative z-10 mx-auto max-w-360">
        <m.h2
          className="mb-3 text-2xl font-bold tracking-tight md:mb-4 md:text-5xl"
          initial={{ opacity: 0, y: 30 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-80px" }}
          transition={{ duration: 0.6 }}
        >
          更多能力
        </m.h2>
        <m.p
          className="mb-10 text-sm text-white/40 md:mb-16 md:text-base"
          initial={{ opacity: 0 }}
          whileInView={{ opacity: 1 }}
          viewport={{ once: true }}
          transition={{ duration: 0.6, delay: 0.15 }}
        >
          And More
        </m.p>

        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 md:gap-4">
          {capabilities.map((c, i) => (
            <m.div
              key={c.label}
              initial={{ opacity: 0, y: 30 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true, margin: "-40px" }}
              transition={{ duration: 0.5, delay: i * 0.05, ease: [0.22, 1, 0.36, 1] }}
              className="group rounded-2xl border border-white/6 bg-white/2 p-5 transition-colors hover:border-white/12 hover:bg-white/4 md:p-6"
            >
              <div className="flex items-center gap-3">
                <c.icon className="size-5 shrink-0 text-white/35 transition-colors group-hover:text-white/65" strokeWidth={1.5} />
                <span className="text-sm font-semibold">{c.label}</span>
                <span className="hidden text-xs text-white/25 sm:inline">{c.en}</span>
              </div>
              <p className="mt-2 text-xs leading-relaxed text-white/40">{c.desc}</p>
            </m.div>
          ))}
        </div>
      </div>
    </section>
  );
}

/* ================================================================
   ARCHITECTURE LAYERS — 层叠卡片 + 视差深度
   ================================================================ */
const archLayers = [
  { label: "Frontend", items: "Next.js 16 / React 19 / Tailwind CSS 4 / shadcn/ui", icon: Layers },
  { label: "API Gateway", items: "Gin / WAF / Rate Limit / Replay Guard / CORS", icon: Shield },
  { label: "Service Layer", items: "Auth / Admin / User / Storage / Payment / Workflow", icon: Server },
  { label: "Data Layer", items: "PostgreSQL / Redis / NATS / Temporal", icon: Database },
  { label: "Infrastructure", items: "GeoIP / OpenTelemetry / Crash Recovery / Docker", icon: Cloud },
];

function ArchitectureLayers() {
  const ref = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target: ref, offset: ["start end", "end start"] });

  return (
    <section ref={ref} className="relative overflow-hidden px-5 py-20 md:px-8 md:py-32 xl:px-12">
      {/* 背景网格 */}
      <div
        className="pointer-events-none absolute inset-0 opacity-15"
        style={{
          backgroundImage: "linear-gradient(rgba(255,255,255,0.03) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.03) 1px, transparent 1px)",
          backgroundSize: "48px 48px",
          maskImage: "radial-gradient(ellipse 60% 60% at 50% 50%, black, transparent)",
        }}
      />

      <div className="relative z-10 mx-auto max-w-360">
        <m.h2
          className="mb-3 text-2xl font-bold tracking-tight md:mb-4 md:text-5xl"
          initial={{ opacity: 0, y: 30 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-80px" }}
          transition={{ duration: 0.6 }}
        >
          架构分层
        </m.h2>
        <m.p
          className="mb-10 text-sm text-white/40 md:mb-16 md:text-base"
          initial={{ opacity: 0 }}
          whileInView={{ opacity: 1 }}
          viewport={{ once: true }}
          transition={{ duration: 0.6, delay: 0.15 }}
        >
          Architecture Overview
        </m.p>

        <div className="space-y-3 md:space-y-4">
          {archLayers.map((layer, i) => {
            const layerY = useTransform(scrollYProgress, [0, 1], [30 * (archLayers.length - i), -20 * (archLayers.length - i)]);
            return (
              <m.div
                key={layer.label}
                initial={{ opacity: 0, x: -40 }}
                whileInView={{ opacity: 1, x: 0 }}
                viewport={{ once: true, margin: "-30px" }}
                transition={{ duration: 0.5, delay: i * 0.08, ease: [0.22, 1, 0.36, 1] }}
                style={{ y: layerY }}
                className="flex items-center gap-4 rounded-2xl border border-white/6 bg-white/2 px-5 py-4 md:gap-5 md:px-8 md:py-5"
              >
                <layer.icon className="size-5 shrink-0 text-white/30" strokeWidth={1.5} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline gap-2">
                    <span className="text-[11px] font-semibold uppercase tracking-widest text-white/50">{layer.label}</span>
                    <span className="hidden text-[10px] text-white/20 md:inline">Layer {archLayers.length - i}</span>
                  </div>
                  <p className="mt-0.5 truncate text-xs text-white/35 md:text-sm md:text-white/45">{layer.items}</p>
                </div>
                {/* 层级深度条 */}
                <div className="hidden h-1.5 w-16 overflow-hidden rounded-full bg-white/6 md:block">
                  <m.div
                    className="h-full rounded-full bg-white/15"
                    initial={{ width: 0 }}
                    whileInView={{ width: `${((archLayers.length - i) / archLayers.length) * 100}%` }}
                    viewport={{ once: true }}
                    transition={{ duration: 0.8, delay: 0.3 + i * 0.1, ease: [0.22, 1, 0.36, 1] }}
                  />
                </div>
              </m.div>
            );
          })}
        </div>
      </div>
    </section>
  );
}

/* ================================================================
   TECH STACK — Sticky 视差 + 图标 + 网格
   桌面端：sticky pinning，卡片随滚动从底部浮入 + 不同深度视差
   移动端：普通交错入场
   ================================================================ */
function HorizontalTechStack({ items }: { items: typeof techStack }) {
  const stickyRef = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target: stickyRef, offset: ["start start", "end end"] });

  // 背景视差层
  const bgY = useTransform(scrollYProgress, [0, 1], [80, -80]);
  const gridOpacity = useTransform(scrollYProgress, [0, 0.15, 0.85, 1], [0, 0.12, 0.12, 0]);

  return (
    <>
      {/* 桌面端：sticky 视差 */}
      <div ref={stickyRef} className="relative hidden md:block" style={{ height: "250vh" }}>
        <div className="sticky top-0 flex h-screen flex-col justify-center overflow-hidden px-8 xl:px-12">
          {/* 视差背景光 */}
          <m.div
            className="pointer-events-none absolute inset-0"
            style={{
              y: bgY,
              background: "radial-gradient(ellipse 60% 50% at 50% 40%, rgba(255,255,255,0.03), transparent), radial-gradient(ellipse 40% 40% at 80% 70%, rgba(255,255,255,0.02), transparent)",
            }}
          />
          {/* 视差网格 */}
          <m.div
            className="pointer-events-none absolute inset-0"
            style={{
              opacity: gridOpacity,
              backgroundImage: "linear-gradient(rgba(255,255,255,0.05) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.05) 1px, transparent 1px)",
              backgroundSize: "56px 56px",
              maskImage: "radial-gradient(ellipse 60% 60% at 50% 50%, black, transparent)",
            }}
          />

          <div className="relative z-10 mx-auto w-full max-w-360">
            <m.h2
              className="mb-2 text-3xl font-bold tracking-tight md:text-5xl"
              style={{ opacity: useTransform(scrollYProgress, [0, 0.12], [0, 1]) }}
            >
              技术栈
            </m.h2>
            <m.p
              className="mb-12 text-base text-white/40"
              style={{ opacity: useTransform(scrollYProgress, [0, 0.12], [0, 1]) }}
            >
              Technology Stack
            </m.p>

            <div className="grid grid-cols-3 gap-4 lg:grid-cols-4 xl:grid-cols-6">
              {items.map((t, i) => (
                <TechCardParallax key={t.layer} item={t} index={i} total={items.length} progress={scrollYProgress} />
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* 移动端：普通网格 */}
      <section className="px-5 py-20 md:hidden">
        <h2 className="mb-3 text-2xl font-bold tracking-tight">技术栈</h2>
        <p className="mb-8 text-sm text-white/40">Technology Stack</p>
        <div className="grid grid-cols-2 gap-3">
          {items.map((t, i) => (
            <m.div
              key={t.layer}
              initial={{ opacity: 0, y: 20 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true, margin: "-20px" }}
              transition={{ duration: 0.4, delay: i * 0.04, ease: [0.22, 1, 0.36, 1] }}
              className="group flex flex-col items-center gap-3 rounded-2xl border border-white/8 bg-white/3 p-4 text-center"
            >
              <t.icon className="size-7 transition-colors" style={{ color: `${t.color}66` }} />
              <div>
                <p className="text-xs font-semibold text-white/90">{t.tech}</p>
                <span className="text-[9px] uppercase tracking-widest text-white/30">{t.layer}</span>
              </div>
            </m.div>
          ))}
        </div>
      </section>
    </>
  );
}

/* 单个技术卡片 — 桌面端视差：每张卡片基于自身位置有不同的 y 偏移和入场时机 */
function TechCardParallax({ item, index, total, progress }: {
  item: (typeof techStack)[number]; index: number; total: number; progress: MotionValue<number>;
}) {
  // 每张卡片在不同滚动点入场，模拟错落浮入
  const stagger = 0.08 + (index / total) * 0.5; // 0.08 ~ 0.58
  const opacity = useTransform(progress, [stagger - 0.08, stagger, 1], [0, 1, 1]);
  const y = useTransform(progress, [stagger - 0.08, stagger, 1], [60, 0, 0]);
  const scale = useTransform(progress, [stagger - 0.08, stagger, 1], [0.9, 1, 1]);

  return (
    <m.div
      style={{ opacity, y, scale }}
      className="group flex flex-col items-center gap-4 rounded-2xl border border-white/8 bg-white/3 p-6 text-center transition-colors hover:border-white/15 hover:bg-white/6"
    >
      <div
        className="flex size-14 items-center justify-center rounded-xl border border-white/6 transition-colors group-hover:border-white/12"
        style={{ background: "radial-gradient(circle at 30% 30%, rgba(255,255,255,0.06), rgba(255,255,255,0.02))" }}
      >
        <item.icon
          className="size-7 transition-all duration-300 group-hover:scale-110"
          style={{ color: `${item.color}88` }}
        />
      </div>
      <div>
        <p className="text-sm font-semibold text-white/90">{item.tech}</p>
        <span className="mt-1 block text-[10px] font-medium uppercase tracking-widest text-white/35">{item.layer}</span>
      </div>
    </m.div>
  );
}
