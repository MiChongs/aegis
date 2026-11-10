"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { AegisMark } from "@/components/brand/aegis-mark";
import { AUTH_EASE } from "@/components/auth/auth-motion";
import { loadPreview, loadWallpaper, wallpaperCache, wallpaperSources } from "@/components/auth/auth-wallpaper";
import { LoginThemeToggle } from "@/components/auth/login-theme-toggle";
import type { BingWallpaper } from "@/lib/bing-wallpaper";
import { cn } from "@/lib/utils";

/** 加载层至少停留多久：太快一闪而过像是出了错，进度环也来不及走完 */
const MIN_INTRO_MS = 900;
/** 最多等高清图多久：超时先进入页面，高清图在后台下完再盖上来，登录不为背景等待 */
const MAX_INTRO_MS = 4000;
/** 进度走到 100% 后留一拍再退场，让环合拢被看见 */
const SETTLE_MS = 260;

type Phase = "loading" | "ready";

/**
 * 登录 / 注册页的舞台：必应每日一图铺满全屏，玻璃卡片承载表单。
 *
 * 加载顺序（首次进入）：
 *   1. 深色加载层：会扫光的盾徽 + 真实下载进度环；同时先拉 22KB 的小图垫底；
 *   2. 高清图下完并解码（或等满 4 秒）后加载层退场：盾徽放大淡出，背景从放大状态收拢，
 *      模糊小图被清晰大图盖住，看起来像「对上了焦」；
 *   3. 页眉、卡片、页脚依次浮入。
 * 同一会话内再进来（登录 ↔ 注册）直接就位，不重播。
 *
 * 表单从一开始就挂载（验证码配置等请求提前发出），加载期间 `inert` 挡住焦点与点击。
 */
export function AuthStage({
  wallpaper,
  footnote,
  children
}: {
  wallpaper: BingWallpaper | null;
  /** 右下角的版权声明 */
  footnote: string;
  children: ReactNode;
}) {
  const reduced = useReducedMotion();
  const [phase, setPhase] = useState<Phase>(() => (wallpaperCache.introPlayed ? "ready" : "loading"));
  // 没有壁纸时没有可报的进度，环一开始就是旋转弧
  const [progress, setProgress] = useState<number | null>(wallpaper ? 0 : null);
  const [preview, setPreview] = useState<string | null>(null);
  const [full, setFull] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    const started = performance.now();
    let finished = false;
    const timers: number[] = [];

    const finish = (extra = 0) => {
      if (finished) return;
      finished = true;
      const wait = Math.max(MIN_INTRO_MS - (performance.now() - started), 0) + extra;
      timers.push(
        window.setTimeout(() => {
          wallpaperCache.markIntroPlayed();
          setPhase("ready");
        }, wait)
      );
    };

    if (!wallpaper) {
      finish();
      return () => timers.forEach((timer) => window.clearTimeout(timer));
    }

    // 本次会话已下过的图（登录 ↔ 注册来回切）直接从缓存取，不再垫小图
    const sources = wallpaperSources(wallpaper.base);
    if (!wallpaperCache.get(sources.full)) {
      void loadPreview(sources.preview).then((src) => {
        if (!controller.signal.aborted && src) setPreview(src);
      });
    }
    timers.push(window.setTimeout(() => finish(), MAX_INTRO_MS));
    loadWallpaper(sources.full, setProgress, controller.signal)
      .then((src) => {
        setFull(src);
        finish(SETTLE_MS);
      })
      .catch(() => {
        if (!controller.signal.aborted) finish();
      });

    return () => {
      controller.abort();
      timers.forEach((timer) => window.clearTimeout(timer));
    };
  }, [wallpaper]);

  const ready = phase === "ready";
  const enter = (delay: number, y = 16) =>
    reduced
      ? { initial: { opacity: 0 }, animate: ready ? { opacity: 1 } : undefined, transition: { duration: 0.3, delay } }
      : {
          initial: { opacity: 0, y },
          animate: ready ? { opacity: 1, y: 0 } : undefined,
          transition: { duration: 0.8, delay, ease: AUTH_EASE }
        };

  return (
    <div className="relative isolate min-h-svh overflow-hidden bg-zinc-950">
      <Backdrop wallpaper={wallpaper} preview={preview} full={full} ready={ready} reduced={reduced} />

      <div
        inert={!ready}
        className="relative z-10 flex min-h-svh flex-col px-5 pt-[max(1.25rem,env(safe-area-inset-top))] pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:px-8 lg:px-12"
      >
        <m.header {...enter(0.15, -10)} className="flex h-12 items-center justify-between">
          <Link href="/" aria-label="Aegis 首页" className="flex items-center gap-2.5 text-white">
            <span className="flex size-9 items-center justify-center rounded-xl bg-white/15 ring-1 ring-white/25 backdrop-blur-md">
              <AegisMark static className="size-5" />
            </span>
            <span className="text-[15px] font-semibold tracking-tight [text-shadow:0_1px_3px_rgba(0,0,0,0.45)]">
              Aegis 控制台
            </span>
          </Link>
          <LoginThemeToggle className="text-white/80 hover:bg-white/15 hover:text-white" />
        </m.header>

        <main className="flex flex-1 items-center justify-center py-10 lg:justify-end lg:pr-[4vw] xl:pr-[8vw]">
          <m.section
            initial={reduced ? { opacity: 0 } : { opacity: 0, y: 32, scale: 0.97 }}
            animate={ready ? { opacity: 1, y: 0, scale: 1 } : undefined}
            transition={reduced ? { duration: 0.3 } : { duration: 0.9, delay: 0.3, ease: AUTH_EASE }}
            className={cn(
              "w-full max-w-[420px] rounded-[28px] p-7 sm:p-10",
              "bg-background/80 ring-1 ring-black/5 backdrop-blur-2xl backdrop-saturate-150",
              "shadow-[0_32px_90px_-24px_rgba(0,0,0,0.65)] dark:bg-zinc-950/70 dark:ring-white/10"
            )}
          >
            {children}
          </m.section>
        </main>

        <m.footer
          {...enter(0.55, 10)}
          className="flex flex-col gap-3 text-white sm:flex-row sm:items-end sm:justify-between [text-shadow:0_1px_3px_rgba(0,0,0,0.5)]"
        >
          {wallpaper ? <WallpaperCaption wallpaper={wallpaper} /> : <span />}
          <p className="text-[11px] text-white/60">{footnote}</p>
        </m.footer>
      </div>

      <AnimatePresence>
        {ready ? null : <IntroOverlay key="intro" progress={progress} reduced={reduced} />}
      </AnimatePresence>
    </div>
  );
}

/**
 * 背景三层：没有壁纸时的点阵底 → 模糊小图 → 清晰大图，最上面压一层平涂暗幕保证白字可读（不用渐变）。
 * 大图在舞台就位时从 1.08 倍收拢到原尺寸；就位之后才到的大图只做淡入。
 */
function Backdrop({
  wallpaper,
  preview,
  full,
  ready,
  reduced
}: {
  wallpaper: BingWallpaper | null;
  preview: string | null;
  full: string | null;
  ready: boolean;
  reduced: boolean | null;
}) {
  return (
    <div aria-hidden className="absolute inset-0 -z-10 select-none">
      {wallpaper ? null : (
        <svg className="absolute inset-0 size-full text-white/[0.07]">
          <defs>
            <pattern id="auth-stage-dots" width="22" height="22" patternUnits="userSpaceOnUse">
              <circle cx="1.5" cy="1.5" r="1.1" fill="currentColor" />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#auth-stage-dots)" />
        </svg>
      )}

      {preview ? (
        <m.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.6 }}
          className="absolute inset-0 scale-110 bg-cover bg-center blur-2xl"
          style={{ backgroundImage: `url("${preview}")` }}
        />
      ) : null}

      {full ? (
        <m.div
          initial={reduced ? { opacity: 0 } : { opacity: 0, scale: ready ? 1 : 1.08 }}
          animate={ready ? { opacity: 1, scale: 1 } : undefined}
          transition={
            reduced
              ? { duration: 0.5 }
              : { opacity: { duration: 1.2, ease: AUTH_EASE }, scale: { duration: 2.4, ease: AUTH_EASE } }
          }
          className="absolute inset-0 bg-cover bg-center will-change-transform"
          style={{ backgroundImage: `url("${full}")` }}
        />
      ) : null}

      <div className="absolute inset-0 bg-black/25 dark:bg-black/45" />
    </div>
  );
}

/** 左下角：当日标题与图片说明、版权归属，点开是必应上的介绍 */
function WallpaperCaption({ wallpaper }: { wallpaper: BingWallpaper }) {
  const detail = [wallpaper.caption, wallpaper.credit ? `© ${wallpaper.credit}` : ""].filter(Boolean).join("  ");
  const content = (
    <>
      {wallpaper.title ? <span className="block text-[13px] font-medium text-white/95">{wallpaper.title}</span> : null}
      {detail ? <span className="mt-0.5 block text-[11px] text-white/65">{detail}</span> : null}
    </>
  );
  return wallpaper.link ? (
    <a
      href={wallpaper.link}
      target="_blank"
      rel="noreferrer"
      className="max-w-md min-w-0 transition-opacity hover:opacity-80"
    >
      {content}
    </a>
  ) : (
    <div className="max-w-md min-w-0">{content}</div>
  );
}

const RING_RADIUS = 46;

/**
 * 首次进入的加载层：扫光盾徽 + 进度环 + 百分比。
 * 知道总长时环按真实字节走；不知道时（拿不到 Content-Length、或退回 <img>）转为一段旋转弧。
 * 退场：盾徽放大淡出，底色同时褪去，露出正在收拢的背景。
 */
function IntroOverlay({ progress, reduced }: { progress: number | null; reduced: boolean | null }) {
  const percent = progress === null ? null : Math.round(progress * 100);
  return (
    <m.div
      role="progressbar"
      aria-label="正在加载"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent ?? undefined}
      initial={false}
      exit={{ opacity: 0 }}
      transition={{ duration: reduced ? 0.3 : 0.8, ease: AUTH_EASE }}
      className="fixed inset-0 z-50 grid place-items-center bg-zinc-950"
    >
      <m.div
        exit={reduced ? { opacity: 0 } : { opacity: 0, scale: 1.25 }}
        transition={{ duration: 0.6, ease: AUTH_EASE }}
        className="flex flex-col items-center gap-6"
      >
        <div className="relative size-28">
          <svg viewBox="0 0 100 100" className="absolute inset-0 size-full -rotate-90">
            <circle cx="50" cy="50" r={RING_RADIUS} fill="none" stroke="rgba(255,255,255,0.1)" strokeWidth="1.5" />
            {progress === null ? null : (
              <m.circle
                cx="50"
                cy="50"
                r={RING_RADIUS}
                fill="none"
                stroke="white"
                strokeWidth="1.5"
                strokeLinecap="round"
                initial={{ pathLength: 0 }}
                animate={{ pathLength: Math.max(progress, 0.02) }}
                transition={{ duration: reduced ? 0 : 0.35, ease: "easeOut" }}
              />
            )}
          </svg>
          {progress === null ? (
            <m.svg
              viewBox="0 0 100 100"
              className="absolute inset-0 size-full"
              animate={reduced ? undefined : { rotate: 360 }}
              transition={{ duration: 1.1, ease: "linear", repeat: Infinity }}
            >
              <circle
                cx="50"
                cy="50"
                r={RING_RADIUS}
                fill="none"
                stroke="white"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeDasharray={`${2 * Math.PI * RING_RADIUS * 0.22} ${2 * Math.PI * RING_RADIUS}`}
              />
            </m.svg>
          ) : null}
          <div className="absolute inset-0 grid place-items-center">
            <AegisMark className="size-11 text-white" />
          </div>
        </div>
        <div className="h-4 font-mono text-[11px] tracking-[0.2em] text-white/55 tabular-nums">
          {percent === null ? "" : `${percent}%`}
        </div>
      </m.div>
    </m.div>
  );
}
