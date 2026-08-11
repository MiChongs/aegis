"use client";

import { useCallback, useEffect, useState } from "react";
import { PixelIntro } from "@/components/background/pixel-intro";

// ─────────────────────────────────────────────────────────────────────
//  IntroOverlay — 全站首次加载进入动画（高性能 + 深浅色自适应 + 科技感）
//
//  行为：
//   - 仅在浏览器首次打开本应用时展示（localStorage 持久化，刷新页面不再显示）。
//   - 通过 root layout 内挂载，叠加在所有路由之上。
//   - 底层暗幕 + 扫描线 + 中心辉光 + Pixel Dissolve 动画。
//   - 完成后淡出并卸载 DOM，不留任何 runtime 副作用。
//   - `prefers-reduced-motion` 下直接跳过。
// ─────────────────────────────────────────────────────────────────────

const STORAGE_KEY = "aegis.intro.shown";
const TOTAL_MS = 2400;
const FADE_OUT_MS = 520;

// SSR 阶段永远返回 "idle"（不渲染任何 DOM），挂载后再决定是否展示动画；
// 避免因 typeof window 分支导致服务端 / 客户端首次渲染的 DOM 不一致而触发
// React 19 的 hydration mismatch 错误。
type Phase = "idle" | "visible" | "fading" | "gone";

export function IntroOverlay() {
  const [phase, setPhase] = useState<Phase>("idle");

  // 挂载后（仅客户端）判定是否展示；通过 microtask 异步 setState，
  // 避免 React 19 规则 "setState synchronously within an effect" 告警，
  // 同时保证在首帧浏览器 paint 前就完成 state 切换。
  useEffect(() => {
    let cancelled = false;
    const resolve = () => {
      if (cancelled) return;
      try {
        if (window.localStorage.getItem(STORAGE_KEY) === "1") {
          setPhase("gone");
          return;
        }
        if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
          window.localStorage.setItem(STORAGE_KEY, "1");
          setPhase("gone");
          return;
        }
        window.localStorage.setItem(STORAGE_KEY, "1");
        setPhase("visible");
      } catch {
        setPhase("visible");
      }
    };
    queueMicrotask(resolve);
    return () => {
      cancelled = true;
    };
  }, []);

  const handleDone = useCallback(() => {
    setPhase("fading");
  }, []);

  useEffect(() => {
    if (phase !== "fading") return;
    const t = setTimeout(() => setPhase("gone"), FADE_OUT_MS);
    return () => clearTimeout(t);
  }, [phase]);

  if (phase === "idle" || phase === "gone") return null;

  return (
    <div
      aria-hidden="true"
      className="aegis-intro-overlay"
      data-phase={phase}
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 2147483647, // 永远在最上层
        pointerEvents: "none",
        opacity: phase === "fading" ? 0 : 1,
        transition: `opacity ${FADE_OUT_MS}ms cubic-bezier(0.22, 1, 0.36, 1)`,
      }}
    >
      {/* 基底：深色模式纯黑 / 浅色模式纯白，保证动画对比度 */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          background: "var(--intro-bg)",
        }}
      />

      {/* 扫描线（CSS background，GPU 合成，0 运行开销） */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          backgroundImage:
            "repeating-linear-gradient(0deg, var(--intro-scanline) 0 1px, transparent 1px 4px)",
          opacity: 0.28,
          mixBlendMode: "overlay",
        }}
      />

      {/* 中心品牌辉光 */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          background:
            "radial-gradient(circle at 50% 50%, var(--intro-glow) 0%, transparent 55%)",
          opacity: 0.55,
        }}
      />

      {/* 像素 dissolve 主视觉 */}
      <PixelIntro totalMs={TOTAL_MS} onDone={handleDone} gap={14} />

      {/* Aegis 字样 + 扫描指示，强化未来感 */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 12,
          fontFamily:
            '"Oxanium","Eurostile","JetBrains Mono","SF Mono",ui-monospace,monospace',
          color: "var(--intro-fg)",
          textShadow: "0 0 18px var(--intro-glow)",
          letterSpacing: "0.42em",
        }}
      >
        <span
          style={{
            fontSize: "clamp(44px, 8vw, 96px)",
            fontWeight: 700,
            lineHeight: 1,
          }}
        >
          AEGIS
        </span>
        <span
          style={{
            fontSize: 11,
            letterSpacing: "0.55em",
            color: "var(--intro-muted)",
          }}
        >
          <span
            style={{
              display: "inline-block",
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: "var(--intro-accent)",
              marginRight: 10,
              boxShadow: "0 0 10px var(--intro-accent)",
              animation: "aegis-intro-blink 0.9s steps(2) infinite",
              verticalAlign: "middle",
            }}
          />
          INITIALIZING IDENTITY FABRIC
        </span>
      </div>

      {/* 主题 CSS 变量 & 闪烁关键帧 */}
      <style>{`
        .aegis-intro-overlay {
          --intro-bg: #fafaf8;
          --intro-fg: #141413;
          --intro-muted: #5e5d59;
          --intro-glow: rgba(217, 119, 87, 0.35);
          --intro-accent: #c96442;
          --intro-scanline: rgba(20, 20, 19, 0.35);
        }
        @media (prefers-color-scheme: dark) {
          .aegis-intro-overlay {
            --intro-bg: #0a0a0a;
            --intro-fg: #f5f4ed;
            --intro-muted: #9c9b94;
            --intro-glow: rgba(217, 119, 87, 0.5);
            --intro-accent: #d97757;
            --intro-scanline: rgba(217, 119, 87, 0.28);
          }
        }
        :root.dark .aegis-intro-overlay {
          --intro-bg: #0a0a0a;
          --intro-fg: #f5f4ed;
          --intro-muted: #9c9b94;
          --intro-glow: rgba(217, 119, 87, 0.5);
          --intro-accent: #d97757;
          --intro-scanline: rgba(217, 119, 87, 0.28);
        }
        @keyframes aegis-intro-blink {
          0%, 50%  { opacity: 1; }
          51%,100% { opacity: 0.25; }
        }
      `}</style>
    </div>
  );
}
