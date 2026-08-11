"use client";

import { LazyMotion, domAnimation, m } from "motion/react";
import { AegisMark } from "@/components/brand/aegis-mark";

const stagger = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.12, delayChildren: 0.3 } }
};

const fadeUp = {
  hidden: { opacity: 0, y: 18 },
  visible: {
    opacity: 1,
    y: 0,
    transition: {
      duration: 0.7,
      ease: [0.22, 1, 0.36, 1] as [number, number, number, number]
    }
  }
};

/**
 * 登录页右侧品牌文案
 *
 * 设计语言：编辑级 masthead（静态盾徽 + 竖线分隔 + 主标 + 描述段）
 *
 * 抗磨砂关键：
 *   - 全部文字和盾徽挂**主题感知 halo**（`--login-brand-shadow` / `--login-brand-mark-shadow`）
 *     深色模式：0 0 14px 黑色光晕 + 0 1px 2px 黑色锐边 —— 颗粒再密，文字也有 2px 护圈
 *     浅色模式：白色柔光晕 + 极轻黑投影 —— 让白底上的深色字不被高频颗粒刺糊
 *   - 描述段主色从 `text-muted-foreground`（#a1a1aa 灰）改为 `text-foreground/75`
 *     直接用主题前景色衰减，在有色背景上对比度远胜中灰
 *   - 竖线分隔从 `bg-border`（会被底色吞掉）改为 `bg-[var(--login-brand-divider)]`
 *     主题感知的明亮/暗度确保始终可见
 */
export function LoginBrandPanel() {
  return (
    <LazyMotion features={domAnimation}>
      <m.div
        className="flex flex-col gap-6 text-foreground"
        variants={stagger}
        initial="hidden"
        animate="visible"
        style={{ textShadow: "var(--login-brand-shadow)" }}
      >
        {/* Masthead —— 静态盾徽 + 主题感知竖线 + 高对比标签 */}
        <m.div variants={fadeUp} className="flex items-center gap-3.5">
          <AegisMark
            static
            className="size-8 text-foreground"
            style={{ filter: "var(--login-brand-mark-shadow)" }}
          />
          <span
            className="h-6 w-px"
            style={{ backgroundColor: "var(--login-brand-divider)" }}
            aria-hidden
          />
          <span className="text-[13px] font-medium tracking-normal text-foreground/80">
            Identity Fabric · 身份底座
          </span>
        </m.div>

        <m.h1
          variants={fadeUp}
          className="text-[44px] font-semibold leading-[1.02] tracking-tight text-foreground sm:text-[56px] lg:text-[64px]"
        >
          Aegis
          <span className="ml-3 font-light text-foreground/70">Console</span>
        </m.h1>

        <m.p
          variants={fadeUp}
          className="max-w-md text-base leading-7 text-foreground/75 sm:text-[17px] sm:leading-8"
        >
          企业级身份底座 ——
          <br className="hidden sm:block" /> 为每个应用建立清晰、稳定、可治理的
          <br className="hidden sm:block" /> 用户与权限边界。
        </m.p>
      </m.div>
    </LazyMotion>
  );
}
