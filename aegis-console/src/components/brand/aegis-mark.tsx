import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

type AegisMarkProps = ComponentProps<"svg"> & {
  title?: string;
  /** 关闭动画，渲染为纯静态 mark（用于密集列表 / 打印 / 图标化场景） */
  static?: boolean;
};

// 盾牌轮廓；scan / base / check 三条路径共享同一几何定义，保证动画层完美贴合。
const SHIELD_PATH =
  "M32 6.5 50.5 12.7v16.5c0 12.9-7.4 24.5-18.5 29-11.1-4.5-18.5-16.1-18.5-29V12.7L32 6.5Z";

export function AegisMark({
  className,
  title = "Aegis",
  static: isStatic = false,
  ...props
}: AegisMarkProps) {
  return (
    <svg
      viewBox="0 0 64 64"
      fill="none"
      role="img"
      aria-label={title}
      className={cn("size-4", isStatic ? "" : "aegis-mark", className)}
      {...props}
    >
      {/* 盾牌底色 */}
      <path d={SHIELD_PATH} fill="currentColor" fillOpacity="0.12" />

      {/* 盾牌常亮描边 */}
      <path
        className={isStatic ? undefined : "aegis-shield-base"}
        d={SHIELD_PATH}
        stroke="currentColor"
        strokeWidth="3"
        strokeLinejoin="round"
      />

      {/* 扫描描边：覆盖在底色描边之上，做 stroke-dashoffset 扫描动画 */}
      {!isStatic ? (
        <path
          className="aegis-shield-scan"
          d={SHIELD_PATH}
          stroke="currentColor"
          strokeWidth="3"
          strokeLinecap="round"
          strokeLinejoin="round"
          pathLength={100}
        />
      ) : null}

      {/* 内部 A 字形 */}
      <path
        className={isStatic ? undefined : "aegis-shield-glyph"}
        d="M32 18.5 42.5 43h-5.8l-1.8-4.8h-6.2L26.8 43H21l11-24.5Zm1.1 14.2-1.3-3.2c-.4-1-.8-2-1.2-3.2-.4 1.2-.8 2.3-1.2 3.2l-1.3 3.2h4.9Z"
        fill="currentColor"
      />

      {/* A 下方横杠 */}
      <path
        className={isStatic ? undefined : "aegis-shield-glyph"}
        d="M27.8 35h8.4"
        stroke="currentColor"
        strokeWidth="2.6"
        strokeLinecap="round"
      />

      {/* 右上装饰点（扫描脉冲的"就绪指示灯"） */}
      <path
        className={isStatic ? undefined : "aegis-shield-dot"}
        d="M46.4 17.1c0 1.7-1.4 3.1-3.1 3.1s-3.1-1.4-3.1-3.1S41.6 14 43.3 14s3.1 1.4 3.1 3.1Z"
        fill="currentColor"
      />

      {/* 动画样式：内联 <style> 走 SVG，确保服务端渲染友好，不依赖 "use client" */}
      {!isStatic ? (
        <style>{`
          .aegis-mark .aegis-shield-base { opacity: 0.45; }
          .aegis-mark .aegis-shield-scan {
            opacity: 0.95;
            stroke-dasharray: 22 78;
            animation: aegis-scan 2.6s linear infinite;
            filter: drop-shadow(0 0 1.2px currentColor);
          }
          @keyframes aegis-scan {
            0%   { stroke-dashoffset: 100; }
            100% { stroke-dashoffset: 0; }
          }
          .aegis-mark .aegis-shield-glyph {
            transform-box: fill-box;
            transform-origin: center;
            animation: aegis-glyph 2.6s ease-in-out infinite;
          }
          @keyframes aegis-glyph {
            0%, 100% { opacity: 0.75; }
            50%      { opacity: 1;    }
          }
          .aegis-mark .aegis-shield-dot {
            transform-box: fill-box;
            transform-origin: center;
            animation: aegis-dot 1.4s ease-in-out infinite;
          }
          @keyframes aegis-dot {
            0%, 100% { opacity: 0.55; transform: scale(0.85); }
            50%      { opacity: 1.00; transform: scale(1.15); }
          }
          @media (prefers-reduced-motion: reduce) {
            .aegis-mark .aegis-shield-scan,
            .aegis-mark .aegis-shield-glyph,
            .aegis-mark .aegis-shield-dot {
              animation: none;
            }
            .aegis-mark .aegis-shield-scan { stroke-dasharray: none; opacity: 1; }
            .aegis-mark .aegis-shield-glyph,
            .aegis-mark .aegis-shield-dot { opacity: 1; transform: none; }
          }
        `}</style>
      ) : null}
    </svg>
  );
}
