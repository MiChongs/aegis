"use client";

import { LazyMotion, domAnimation } from "motion/react";
import { PublicHeader } from "@/components/brand/public-header";
import { SiteFooter } from "@/components/brand/site-footer";
import { ArchitectureSection } from "@/components/brand/home/architecture-section";
import { CapabilitiesSection } from "@/components/brand/home/capabilities-section";
import { ClosingSection } from "@/components/brand/home/closing-section";
import { FaqSection } from "@/components/brand/home/faq-section";
import { FeaturesSection } from "@/components/brand/home/features-section";
import { HeroSection } from "@/components/brand/home/hero-section";
import { IntegrationSection } from "@/components/brand/home/integration-section";
import { IntroOverlay } from "@/components/brand/home/intro-overlay";
import { MetricsSection } from "@/components/brand/home/metrics-section";
import { SponsorsSection } from "@/components/brand/home/sponsors-section";

/**
 * 首页。
 *
 * 这一版相对旧版拆掉的东西，以及为什么：
 *
 * - **写死的深色画布**（`bg-[#060a12] text-white` + 满屏 `white/6`）。
 *   它让首页成了全站唯一一个不认主题的页面：浅色模式下从控制台点回首页，
 *   画面会毫无预告地黑掉一屏。现在每一个颜色都走 shadcn 的语义令牌，
 *   深浅两色各自成立，也就自动跟着 next-themes 走。
 * - **750vh 的 sticky 视差**（核心能力 4 屏 + 技术栈 2.5 屏）。
 *   为了播两段"卡片浮入"的动画，凭空要了六七屏的滚动距离 ——
 *   用户以为在往下翻内容，实际什么都没翻到。现在动效只有一种：
 *   进入视口时淡入上移一次，`prefers-reduced-motion` 下完全不放。
 * - **中英并列的分区副标题**（"核心能力 / Core Capabilities"）。
 *   英文那一行不承担任何信息，只是把每个标题的视觉重量翻了一倍。
 *
 * 反过来，首页比控制台**多**一层视觉：强调色、光晕、底纹与入场动效。
 * 控制台通体 zinc 单色是对的（长时间盯着看的界面该克制），但同一套克制
 * 搬到门面上就只剩朴素。这一层的令牌与 keyframes 都定义在 globals.css 的
 * `--home-*` 命名空间下，只在这棵子树里取值，不会渗进控制台。
 *
 * 分区顺序即阅读顺序：这是什么（首屏）→ 有多少（数字）→ 替我做完了什么
 * （核心能力）→ 具体都有什么（能力全景）→ 怎么搭起来的（架构）→
 * 怎么接（接入）→ 还有什么顾虑（常见问题）→ 站在谁的肩膀上（赞助商）→
 * 从哪进去（收尾）。
 */
export function BrandHome() {
  return (
    <LazyMotion features={domAnimation}>
      {/* 冷开场盖在整页之上，页面内容始终在 DOM 里 ——
          爬虫与读屏软件看到的是完整首页，不受这一层影响。 */}
      <IntroOverlay />

      <div className="flex min-h-screen flex-col bg-background text-foreground">
        <PublicHeader current="home" navLabel="主页导航" />

        <main className="flex-1">
          <HeroSection />
          <MetricsSection />
          <FeaturesSection />
          <CapabilitiesSection />
          <ArchitectureSection />
          <IntegrationSection />
          <FaqSection />
          <SponsorsSection />
          <ClosingSection />
        </main>

        <SiteFooter />
      </div>
    </LazyMotion>
  );
}
