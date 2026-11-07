"use client";

import { useCallback, useState, type CSSProperties } from "react";
import NumberFlow from "@number-flow/react";
import { useReducedMotion } from "motion/react";
import type { TrafficMode } from "@/lib/api/traffic";
import { cn } from "@/lib/utils";
import { STREAM, fmtNum, type StreamKey } from "./traffic-format";

/**
 * 闸门图：左侧来水（上一秒到达数）汇成一条水道，依次穿过各条生效规则的闸门，
 * 过闸后分流成放行 / 排队 / 观察 / 拒绝四股，每股宽度按上一秒速率占比。
 *
 * 闸门的开度 = 1 − 该规则上一秒的拒绝占比：拒得越多闸板合得越紧。观察模式的闸板用
 * 紫色，放行模式的闸板淡显。水流里的虚线随速率加快；减少动态效果时虚线静止。
 *
 * 用真实像素坐标绘制（ResizeObserver 量容器宽度），而不是 viewBox 缩放：
 * 后者在 390px 宽的手机上会把标签缩成 4px。
 */

export type FlowGateRule = {
  id: string;
  name: string;
  mode: TrafficMode;
  arrivals: number;
  rejected: number;
  observed: number;
};

const OUTPUTS: StreamKey[] = ["admitted", "queued", "observed", "rejected"];

function useMeasuredWidth() {
  const [width, setWidth] = useState(0);
  const ref = useCallback((node: HTMLDivElement | null) => {
    if (!node) return;
    const observer = new ResizeObserver((entries) => {
      const next = Math.round(entries[0]?.contentRect.width ?? 0);
      setWidth((prev) => (prev === next ? prev : next));
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  return [ref, width] as const;
}

function dashDuration(rate: number) {
  if (rate <= 0) return 0;
  return Math.min(2.6, Math.max(0.32, 2.6 - Math.log10(1 + rate) * 0.55));
}

export function FlowGate({
  arrivals,
  admitted,
  queued,
  observed,
  rejected,
  gates,
  paused,
  overload
}: {
  arrivals: number;
  admitted: number;
  queued: number;
  observed: number;
  rejected: number;
  gates: FlowGateRule[];
  paused?: boolean;
  overload?: boolean;
}) {
  const [ref, width] = useMeasuredWidth();
  const reduceMotion = useReducedMotion();
  const compact = width > 0 && width < 560;
  const H = compact ? 264 : 320;

  const values: Record<StreamKey, number> = { arrivals, admitted, queued, observed, rejected };
  const W = Math.max(width, 300);
  const leftW = compact ? 72 : 112;
  const rightW = compact ? 88 : 128;
  const x0 = leftW;
  const x1 = W - rightW;
  const span = Math.max(80, x1 - x0);
  const gx0 = x0 + span * 0.1;
  const gx1 = x0 + span * 0.5;
  const xs = x0 + span * 0.6;
  const mid = (xs + x1) / 2;
  const cy = H / 2 + 10;
  const maxBand = H * (compact ? 0.34 : 0.38);
  const channelTop = cy - maxBand / 2 - 5;
  const channelBot = cy + maxBand / 2 + 5;

  const sum = OUTPUTS.reduce((acc, key) => acc + Math.max(0, values[key]), 0);
  const total = Math.max(arrivals, sum);
  const widths = OUTPUTS.map((key) => (values[key] > 0 && total > 0 ? Math.max(3, (values[key] / total) * maxBand) : 0));
  const sumW = widths.reduce((acc, w) => acc + w, 0);
  const slotTop = H * 0.17;
  const slotBottom = H * 0.88;
  const slotY = OUTPUTS.map((_, i) => slotTop + ((slotBottom - slotTop) * i) / (OUTPUTS.length - 1));

  const start = cy - sumW / 2;
  const bands = OUTPUTS.map((key, i) => {
    const w = widths[i]!;
    const top0 = start + widths.slice(0, i).reduce((acc, value) => acc + value, 0);
    const ty = slotY[i]!;
    const top1 = ty - w / 2;
    const bot0 = top0 + w;
    const bot1 = ty + w / 2;
    const yc0 = top0 + w / 2;
    return {
      key,
      w,
      ty,
      area: `M${x0},${top0} L${xs},${top0} C${mid},${top0} ${mid},${top1} ${x1},${top1} L${x1},${bot1} C${mid},${bot1} ${mid},${bot0} ${xs},${bot0} L${x0},${bot0} Z`,
      center: `M${x0},${yc0} L${xs},${yc0} C${mid},${yc0} ${mid},${ty} ${x1},${ty}`,
      duration: dashDuration(values[key])
    };
  });

  // 闸门：放不下就把末尾的合成「+N」
  const spacing = compact ? 46 : 66;
  const capacity = Math.max(1, Math.floor((gx1 - gx0) / spacing) + 1);
  const overflow = gates.length > capacity;
  const shown = overflow ? gates.slice(0, capacity - 1) : gates;
  const rest = overflow ? gates.slice(capacity - 1) : [];
  const gateList = [
    ...shown.map((gate) => ({ ...gate, more: false })),
    ...(rest.length
      ? [
          {
            id: "__more",
            name: `+${rest.length}`,
            mode: "enforce" as TrafficMode,
            arrivals: rest.reduce((acc, item) => acc + item.arrivals, 0),
            rejected: rest.reduce((acc, item) => acc + item.rejected, 0),
            observed: rest.reduce((acc, item) => acc + item.observed, 0),
            more: true
          }
        ]
      : [])
  ];
  const gateX = (i: number) =>
    gateList.length === 1 ? (gx0 + gx1) / 2 : gx0 + (i * (gx1 - gx0)) / Math.max(1, gateList.length - 1);
  const gateWidth = gateList.length > 1 ? (gx1 - gx0) / (gateList.length - 1) - 6 : 96;

  const idle = total <= 0;

  return (
    <div ref={ref} className="relative w-full min-w-0 select-none" style={{ minHeight: H }}>
      {width > 0 ? (
        <>
          <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} role="img" aria-label="流量闸门" className={cn("block", paused && "opacity-45")}>
            <style>{`.tf-dash{animation:tf-flow var(--tf-dur) linear infinite}@keyframes tf-flow{to{stroke-dashoffset:-16}}@media (prefers-reduced-motion: reduce){.tf-dash{animation:none}}`}</style>

            {/* 水道 */}
            <rect x={x0} y={channelTop} width={xs - x0} height={channelBot - channelTop} rx={6} className="fill-muted/70" />
            <line x1={x0} x2={xs} y1={channelTop} y2={channelTop} strokeWidth={1.5} className="stroke-border" />
            <line x1={x0} x2={xs} y1={channelBot} y2={channelBot} strokeWidth={1.5} className="stroke-border" />

            {/* 来水口 */}
            <rect
              x={x0 - 10}
              y={cy - Math.max(sumW, 22) / 2}
              width={10}
              height={Math.max(sumW, 22)}
              rx={3}
              className={idle ? "fill-muted-foreground/30" : STREAM.arrivals.solid}
            />

            {/* 四股流 */}
            {bands.map((band) =>
              band.w > 0 ? (
                <g key={band.key}>
                  <path d={band.area} className={STREAM[band.key].fill} />
                  <rect x={x1} y={band.ty - band.w / 2} width={6} height={band.w} rx={2} className={STREAM[band.key].solid} />
                  <path
                    d={band.center}
                    fill="none"
                    strokeWidth={Math.min(2.5, Math.max(1.2, band.w * 0.16))}
                    strokeDasharray="4 12"
                    strokeLinecap="round"
                    className={cn(STREAM[band.key].stroke, !reduceMotion && !paused && band.duration > 0 && "tf-dash")}
                    style={{ "--tf-dur": `${band.duration}s` } as CSSProperties}
                  />
                </g>
              ) : (
                <path key={band.key} d={`M${xs},${cy} C${mid},${cy} ${mid},${band.ty} ${x1},${band.ty}`} fill="none" strokeWidth={1} strokeDasharray="2 4" className="stroke-border" />
              )
            )}

            {idle ? (
              <line x1={x0} x2={xs} y1={cy} y2={cy} strokeWidth={1} strokeDasharray="2 4" className="stroke-muted-foreground/50" />
            ) : null}

            {/* 闸门 */}
            {gateList.map((gate, i) => {
              const gx = gateX(i);
              const ratio =
                gate.mode === "observe"
                  ? gate.arrivals > 0 ? Math.min(1, gate.observed / gate.arrivals) : 0
                  : gate.mode === "bypass"
                    ? 0
                    : gate.arrivals > 0 ? Math.min(1, gate.rejected / gate.arrivals) : 0;
              const close = ratio * maxBand * 0.42;
              const postClass =
                gate.mode === "observe" ? "fill-violet-500" : gate.mode === "bypass" ? "fill-muted-foreground/40" : gate.more ? "fill-muted-foreground" : "fill-foreground";
              return (
                <g key={gate.id}>
                  <line x1={gx} x2={gx} y1={channelTop} y2={channelBot} strokeWidth={1} strokeDasharray="3 3" className="stroke-foreground/25" />
                  <rect x={gx - 4} y={channelTop - 16} width={8} height={16 + close} rx={2} className={cn(postClass, "transition-[height] duration-500")} />
                  <rect x={gx - 4} y={channelBot - close} width={8} height={16 + close} rx={2} className={cn(postClass, "transition-[y,height] duration-500")} />
                  <rect x={gx - 9} y={channelTop - 19} width={18} height={4} rx={1.5} className="fill-foreground/70" />
                  <rect x={gx - 9} y={channelBot + 15} width={18} height={4} rx={1.5} className="fill-foreground/70" />
                </g>
              );
            })}
          </svg>

          {/* 来水标签 */}
          <div className="absolute flex flex-col" style={{ left: 0, top: cy - 30, width: leftW - 18 }}>
            <span className="text-xs text-muted-foreground">到达</span>
            <span className="truncate text-xl leading-7 font-semibold tracking-tight tabular-nums sm:text-2xl">
              <NumberFlow value={arrivals} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
            </span>
            <span className="text-[11px] text-muted-foreground">每秒</span>
          </div>

          {/* 闸门标签 */}
          {gateList.map((gate, i) => (
            <div key={gate.id}>
              <div
                className={cn(
                  "absolute -translate-x-1/2 truncate text-center text-[11px] leading-4 font-medium",
                  gate.mode === "observe" && "text-violet-600 dark:text-violet-400",
                  gate.mode === "bypass" && "text-muted-foreground"
                )}
                style={{ left: gateX(i), top: channelTop - 40, maxWidth: Math.max(40, Math.min(120, gateWidth)) }}
                title={gate.name}
              >
                {gate.name}
              </div>
              {gate.rejected > 0 ? (
                <div
                  className="absolute -translate-x-1/2 text-center text-[11px] leading-4 whitespace-nowrap text-rose-600 tabular-nums dark:text-rose-400"
                  style={{ left: gateX(i), top: channelBot + 24 }}
                >
                  拒 {fmtNum(gate.rejected)}
                </div>
              ) : null}
            </div>
          ))}
          {gateList.length === 0 ? (
            <div className="absolute -translate-x-1/2 text-[11px] text-muted-foreground" style={{ left: (gx0 + gx1) / 2, top: channelTop - 28 }}>
              无生效规则
            </div>
          ) : null}

          {/* 出水标签 */}
          {bands.map((band) => (
            <div key={band.key} className="absolute flex min-w-0 flex-col" style={{ left: x1 + 14, top: band.ty - 20, width: rightW - 18 }}>
              <span className={cn("text-[11px] leading-4 font-medium", STREAM[band.key].text)}>{STREAM[band.key].label}</span>
              <span className="truncate text-base leading-6 font-semibold tracking-tight tabular-nums">
                <NumberFlow value={values[band.key]} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
              </span>
            </div>
          ))}

          {paused || overload ? (
            <div className="absolute top-0 right-0 flex gap-1.5">
              {paused ? (
                <span className="rounded-full border border-border bg-card px-2 py-0.5 text-[11px] font-medium text-muted-foreground">总开关已关闭</span>
              ) : null}
              {overload ? (
                <span className="rounded-full bg-rose-500/12 px-2 py-0.5 text-[11px] font-medium text-rose-700 dark:text-rose-300">过载保护中</span>
              ) : null}
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
