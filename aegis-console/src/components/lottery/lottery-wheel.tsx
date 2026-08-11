"use client";

import { useCallback, useRef, useState } from "react";
import { m, LazyMotion, domAnimation } from "motion/react";
import type { LotteryPrize, LotteryDrawResult } from "@/lib/api/lottery";

const SEGMENT_COLORS = [
  ["#ef4444", "#fee2e2"], // red
  ["#f59e0b", "#fef3c7"], // amber
  ["#10b981", "#d1fae5"], // emerald
  ["#3b82f6", "#dbeafe"], // blue
  ["#8b5cf6", "#ede9fe"], // violet
  ["#ec4899", "#fce7f3"], // pink
  ["#06b6d4", "#cffafe"], // cyan
  ["#f97316", "#ffedd5"]  // orange
];

export function LotteryWheel({
  prizes,
  onDraw,
  disabled
}: {
  prizes: LotteryPrize[];
  onDraw: () => Promise<LotteryDrawResult>;
  disabled?: boolean;
}) {
  const [spinning, setSpinning] = useState(false);
  const [rotation, setRotation] = useState(0);
  const cumulativeRotation = useRef(0);

  const count = prizes.length;
  const segAngle = count > 0 ? 360 / count : 360;

  const handleDraw = useCallback(async () => {
    if (spinning || disabled || count === 0) return;
    setSpinning(true);

    try {
      const result = await onDraw();
      const idx = result.prizeIndex;

      // 计算目标角度：让中奖奖品对准顶部（12点钟方向）
      // 每个奖品扇区从 i*segAngle 开始，中心在 i*segAngle + segAngle/2
      // 轮盘顺时针旋转，所以目标 = 360 - (idx*segAngle + segAngle/2)
      const prizeCenter = idx * segAngle + segAngle / 2;
      const targetOffset = 360 - prizeCenter;

      // 额外旋转圈数（5-8圈）让效果更好
      const extraSpins = (5 + Math.floor(Math.random() * 3)) * 360;
      const newRotation = cumulativeRotation.current + extraSpins + targetOffset + (360 - (cumulativeRotation.current % 360));
      cumulativeRotation.current = newRotation;

      setRotation(newRotation);
    } catch {
      setSpinning(false);
    }
  }, [spinning, disabled, count, onDraw, segAngle]);

  const handleAnimationComplete = useCallback(() => {
    setSpinning(false);
  }, []);

  if (count === 0) {
    return (
      <div className="flex aspect-square max-w-[400px] items-center justify-center rounded-full border-2 border-dashed border-border">
        <span className="text-sm text-muted-foreground">暂无奖品</span>
      </div>
    );
  }

  const size = 400;
  const cx = size / 2;
  const cy = size / 2;
  const r = size / 2 - 8;

  return (
    <LazyMotion features={domAnimation}>
      <div className="relative mx-auto w-full max-w-[400px]">
        {/* 指针 */}
        <div className="absolute left-1/2 top-0 z-10 -translate-x-1/2 -translate-y-1">
          <div className="h-0 w-0 border-x-[12px] border-t-[20px] border-x-transparent border-t-primary" />
        </div>

        <m.div
          animate={{ rotate: rotation }}
          transition={{
            type: "spring",
            stiffness: 12,
            damping: 14,
            mass: 2.5
          }}
          onAnimationComplete={handleAnimationComplete}
          className="aspect-square"
        >
          <svg viewBox={`0 0 ${size} ${size}`} className="h-full w-full drop-shadow-lg">
            {prizes.map((prize, i) => {
              const startAngle = (i * segAngle * Math.PI) / 180;
              const endAngle = ((i + 1) * segAngle * Math.PI) / 180;
              const midAngle = (startAngle + endAngle) / 2;

              const x1 = cx + r * Math.sin(startAngle);
              const y1 = cy - r * Math.cos(startAngle);
              const x2 = cx + r * Math.sin(endAngle);
              const y2 = cy - r * Math.cos(endAngle);

              const largeArc = segAngle > 180 ? 1 : 0;

              // 文字位置（距中心60%处）
              const textR = r * 0.62;
              const textX = cx + textR * Math.sin(midAngle);
              const textY = cy - textR * Math.cos(midAngle);
              const textRotation = (midAngle * 180) / Math.PI;

              const colors = SEGMENT_COLORS[i % SEGMENT_COLORS.length];

              return (
                <g key={prize.id}>
                  <path
                    d={`M ${cx} ${cy} L ${x1} ${y1} A ${r} ${r} 0 ${largeArc} 1 ${x2} ${y2} Z`}
                    fill={colors[0]}
                    stroke="white"
                    strokeWidth="2"
                  />
                  <text
                    x={textX}
                    y={textY}
                    fill="white"
                    fontSize={count > 6 ? "11" : "13"}
                    fontWeight="bold"
                    textAnchor="middle"
                    dominantBaseline="central"
                    transform={`rotate(${textRotation}, ${textX}, ${textY})`}
                  >
                    {prize.name.length > 6 ? prize.name.slice(0, 5) + "..." : prize.name}
                  </text>
                </g>
              );
            })}

            {/* 中心按钮 */}
            <circle cx={cx} cy={cy} r={38} fill="white" stroke="#e5e7eb" strokeWidth="3" />
            <circle cx={cx} cy={cy} r={32} fill={spinning || disabled ? "#9ca3af" : "#ef4444"} className="cursor-pointer" />
            <text
              x={cx}
              y={cy}
              fill="white"
              fontSize="14"
              fontWeight="bold"
              textAnchor="middle"
              dominantBaseline="central"
              className="pointer-events-none select-none"
            >
              {spinning ? "..." : "抽奖"}
            </text>

            {/* 透明按钮覆盖层 */}
            <circle
              cx={cx}
              cy={cy}
              r={38}
              fill="transparent"
              className="cursor-pointer"
              onClick={handleDraw}
            />
          </svg>
        </m.div>
      </div>
    </LazyMotion>
  );
}
