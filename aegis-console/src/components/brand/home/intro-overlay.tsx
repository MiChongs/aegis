"use client";

import { useEffect, useState } from "react";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { AegisMark } from "@/components/brand/aegis-mark";
import { intro } from "@/components/brand/home/home-content";
import { SplitChars, TEXT_EASE } from "@/components/brand/home/visuals";
import { useIsClient } from "@/lib/use-client-value";

/**
 * 冷开场。
 *
 * 一块墨底压在整页之上，三个问题依次出现，最后落到字标上再淡出。
 * 它讲的不是修辞：**认证 / 授权 / 信任**就是这个平台干的三件事，
 * 而「你是谁 / 你能做什么 / 凭什么相信」是它们各自要回答的那句话。
 * 开场结束时首屏正好用同样三行接住，动画因此是内容的一部分而不是前置广告。
 *
 * 一个每次进站都要看完的开场，第二次就变成了阻塞。所以四条闸门缺一不可：
 *
 * 1. **每个会话只播一次**（sessionStorage）。第二次进来直接是首屏。
 * 2. **随时可跳过**：任意键、点击、滚动、触摸，外加一个始终可见的跳过按钮。
 * 3. **进度条必须看得见。** 等待可以忍受的前提是知道还剩多久；
 *    一个不知道什么时候结束的黑屏，第三秒就会被当成页面挂了。
 * 4. **`prefers-reduced-motion` 下根本不挂载。**
 *
 * 首屏内容本身一直在 DOM 里（这一层只是盖在上面），因此爬虫与读屏软件
 * 看到的是完整页面，不受开场影响。
 */

const SESSION_KEY = "aegis:home-intro-played";

/** 每一拍停留多久（毫秒）。总时长 = 三拍 + 落版，约 3.9s */
const BEAT_MS = 850;
const FINALE_MS = 1350;

export function IntroOverlay() {
  const isClient = useIsClient();
  const reduced = useReducedMotion();

  // 惰性初始化只读不写，StrictMode 双调用也返回同一个值。
  // 服务端返回 false，配合下方的 isClient 闸门，水合时两边都渲染 null。
  const [eligible] = useState(
    () => typeof window !== "undefined" && window.sessionStorage.getItem(SESSION_KEY) !== "1"
  );
  const [beat, setBeat] = useState(0);
  const [done, setDone] = useState(false);

  const playing = isClient && eligible && !reduced && !done;
  const total = intro.beats.length;

  // 标记「本会话已播过」。写在 effect 里而不是渲染期：
  // 渲染期写存储在 StrictMode 下会执行两次，且这是副作用不是取值。
  useEffect(() => {
    if (!isClient || !eligible) return;
    window.sessionStorage.setItem(SESSION_KEY, "1");
  }, [eligible, isClient]);

  // 推进节拍。setState 在 timeout 回调里（异步），不是 effect 体内的同步调用。
  useEffect(() => {
    if (!playing) return;
    const last = beat >= total;
    const timer = window.setTimeout(
      () => {
        if (last) setDone(true);
        else setBeat((current) => current + 1);
      },
      last ? FINALE_MS : BEAT_MS
    );
    return () => window.clearTimeout(timer);
  }, [beat, playing, total]);

  // 任意输入都跳过。滚动也算：想往下看的人不该被拦着。
  useEffect(() => {
    if (!playing) return;
    const skip = () => setDone(true);
    const events: (keyof WindowEventMap)[] = ["keydown", "pointerdown", "wheel", "touchstart"];
    for (const type of events) window.addEventListener(type, skip, { passive: true });
    return () => {
      for (const type of events) window.removeEventListener(type, skip);
    };
  }, [playing]);

  // 播放期间锁滚动，否则跳过时页面会停在一个用户没意识到自己滚过的位置
  useEffect(() => {
    if (!playing) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [playing]);

  const elapsed = Math.min(beat, total) * BEAT_MS;
  const duration = total * BEAT_MS + FINALE_MS;

  return (
    <AnimatePresence>
      {playing ? (
        <m.div
          key="home-intro"
          className="home-intro flex flex-col"
          initial={{ opacity: 1 }}
          exit={{ opacity: 0, scale: 1.05, filter: "blur(6px)" }}
          transition={{ duration: 0.75, ease: [0.65, 0, 0.35, 1] }}
          role="presentation"
        >
          <div className="home-intro__scan" aria-hidden />
          <div className="home-intro__grain" aria-hidden />

          {/* 角标：给这块黑屏一个"设备/片头"的框，而不是一片空 */}
          <div className="relative flex items-center justify-between px-5 pt-5 font-mono text-[10px] tracking-[0.28em] uppercase opacity-45 md:px-10 md:pt-8">
            <span>Aegis</span>
            <span>{intro.slate}</span>
          </div>

          <div className="relative flex flex-1 items-center px-5 md:px-10">
            <div className="mx-auto w-full max-w-5xl">
              <AnimatePresence mode="wait">
                {beat < total ? (
                  <m.div
                    key={`beat-${beat}`}
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0, y: -14, transition: { duration: 0.32, ease: "easeIn" } }}
                    transition={{ duration: 0.35 }}
                  >
                    <div className="flex items-center gap-3 font-mono text-[11px] tracking-[0.3em] uppercase">
                      <span
                        className="inline-block h-px w-8"
                        style={{ background: "var(--home-intro-accent)" }}
                        aria-hidden
                      />
                      <span style={{ color: "var(--home-intro-accent)" }}>
                        {String(beat + 1).padStart(2, "0")}
                      </span>
                      <span className="opacity-55">{intro.beats[beat]!.kicker}</span>
                    </div>
                    <p className="mt-5 text-[clamp(1.65rem,5.2vw,3.9rem)] leading-[1.14] font-semibold tracking-tight text-balance">
                      <SplitChars text={intro.beats[beat]!.line} stagger={0.03} />
                    </p>
                  </m.div>
                ) : (
                  <m.div
                    key="finale"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ duration: 0.45 }}
                    className="flex flex-col gap-6"
                  >
                    <m.div
                      className="flex items-center gap-4"
                      initial={{ opacity: 0, y: 12 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ duration: 0.7, ease: TEXT_EASE }}
                    >
                      <AegisMark className="size-9 md:size-11" />
                      <span
                        className="text-2xl font-bold md:text-3xl"
                        style={{ fontFamily: "var(--font-data)", letterSpacing: "0.3em" }}
                      >
                        AEGIS
                      </span>
                    </m.div>
                    <div className="flex flex-col gap-3">
                      <p className="text-[clamp(1.6rem,5vw,3.4rem)] leading-tight font-semibold tracking-tight text-balance">
                        <SplitChars text={intro.finale.title} delay={0.18} stagger={0.03} />
                      </p>
                      <m.p
                        className="font-mono text-xs tracking-wide opacity-55 md:text-sm"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 0.55 }}
                        transition={{ duration: 0.6, delay: 0.55 }}
                      >
                        {intro.finale.sub}
                      </m.p>
                    </div>
                  </m.div>
                )}
              </AnimatePresence>
            </div>
          </div>

          {/* 底部：进度条 + 跳过。两者一起出现才有意义 ——
              进度条说明"还有多久"，跳过按钮说明"你也可以不等"。 */}
          <div className="relative flex items-center gap-4 px-5 pb-5 md:px-10 md:pb-8">
            <div className="h-px flex-1 bg-current/15">
              <m.div
                className="h-full origin-left"
                style={{ background: "var(--home-intro-accent)" }}
                initial={{ scaleX: elapsed / duration }}
                animate={{ scaleX: 1 }}
                transition={{ duration: (duration - elapsed) / 1000, ease: "linear" }}
              />
            </div>
            <button
              type="button"
              onClick={() => setDone(true)}
              className="font-mono text-[10px] tracking-[0.26em] uppercase opacity-55 transition-opacity hover:opacity-100"
            >
              {intro.skip}
            </button>
          </div>
        </m.div>
      ) : null}
    </AnimatePresence>
  );
}
