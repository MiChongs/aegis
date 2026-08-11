"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { LotteryPrize, LotteryDrawResult } from "@/lib/api/lottery";

const PRIZE_TYPE_LABELS: Record<string, string> = {
  points: "积分",
  experience: "经验值",
  item: "实物",
  coupon: "兑换码",
  custom: "自定义"
};

/**
 * 3x3 九宫格抽奖组件
 *
 * 奖品排列顺序（跑马灯方向）:
 *  0  1  2
 *  7  X  3
 *  6  5  4
 *
 * 中心格为抽奖按钮
 */
const GRID_ORDER = [
  [0, 0], // 位置0 -> row=0 col=0
  [0, 1], // 位置1 -> row=0 col=1
  [0, 2], // 位置2 -> row=0 col=2
  [1, 2], // 位置3 -> row=1 col=2
  [2, 2], // 位置4 -> row=2 col=2
  [2, 1], // 位置5 -> row=2 col=1
  [2, 0], // 位置6 -> row=2 col=0
  [1, 0]  // 位置7 -> row=1 col=0
] as const;

export function LotteryGrid({
  prizes,
  onDraw,
  disabled
}: {
  prizes: LotteryPrize[];
  onDraw: () => Promise<LotteryDrawResult>;
  disabled?: boolean;
}) {
  const [spinning, setSpinning] = useState(false);
  const [highlightIndex, setHighlightIndex] = useState(-1);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const resolveRef = useRef<((result: LotteryDrawResult) => void) | null>(null);

  // 清理定时器
  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  // 填充到8个位置
  const gridPrizes = Array.from({ length: 8 }, (_, i) => prizes[i] || null);

  const runAnimation = useCallback(
    (targetIndex: number, onComplete: () => void) => {
      let step = 0;
      // 至少跑3圈再停到目标位置
      const totalSteps = 3 * 8 + targetIndex + Math.floor(Math.random() * 8);
      let speed = 60;

      function tick() {
        const currentPos = step % 8;
        setHighlightIndex(currentPos);
        step++;

        if (step >= totalSteps) {
          onComplete();
          return;
        }

        // 最后10步开始减速
        const remaining = totalSteps - step;
        if (remaining < 10) {
          speed = 60 + (10 - remaining) * 40;
        } else if (remaining < 20) {
          speed = 60 + (20 - remaining) * 8;
        }

        timerRef.current = setTimeout(tick, speed);
      }

      tick();
    },
    []
  );

  const handleDraw = useCallback(async () => {
    if (spinning || disabled || prizes.length === 0) return;
    setSpinning(true);

    try {
      const result = await onDraw();
      const targetIdx = result.prizeIndex % 8;

      await new Promise<void>((resolve) => {
        runAnimation(targetIdx, resolve);
      });

      // 保持最终高亮
      setHighlightIndex(targetIdx);

      // 短暂延时后重置
      await new Promise((r) => setTimeout(r, 800));
    } catch {
      // 错误已由调用方处理
    } finally {
      setSpinning(false);
      setHighlightIndex(-1);
    }
  }, [spinning, disabled, prizes.length, onDraw, runAnimation]);

  // 构建 3x3 网格
  const grid: (LotteryPrize | "button" | null)[][] = [
    [null, null, null],
    [null, "button", null],
    [null, null, null]
  ];

  for (let i = 0; i < 8; i++) {
    const [row, col] = GRID_ORDER[i];
    grid[row][col] = gridPrizes[i];
  }

  return (
    <div className="mx-auto grid w-full max-w-[400px] grid-cols-3 gap-2">
      {grid.flat().map((cell, flatIdx) => {
        if (cell === "button") {
          return (
            <button
              key="center-btn"
              disabled={spinning || disabled || prizes.length === 0}
              onClick={handleDraw}
              className={cn(
                "flex aspect-square items-center justify-center rounded-2xl border-2 text-base font-bold transition-all",
                spinning || disabled
                  ? "cursor-not-allowed border-muted bg-muted text-muted-foreground"
                  : "border-primary bg-primary text-primary-foreground hover:opacity-90 active:scale-95"
              )}
            >
              {spinning ? "抽奖中..." : "抽奖"}
            </button>
          );
        }

        // 计算这个 flatIdx 对应的跑马灯位置
        const row = Math.floor(flatIdx / 3);
        const col = flatIdx % 3;
        const orderIdx = GRID_ORDER.findIndex(([r, c]) => r === row && c === col);
        const isHighlighted = orderIdx === highlightIndex;
        const prize = cell as LotteryPrize | null;

        return (
          <div
            key={flatIdx}
            className={cn(
              "flex aspect-square flex-col items-center justify-center rounded-2xl border-2 p-2 text-center transition-all duration-100",
              isHighlighted
                ? "scale-105 border-primary bg-primary/10 dark:bg-primary/20"
                : "border-border/60 bg-card"
            )}
          >
            {prize ? (
              <>
                <div className="text-xs font-semibold leading-tight text-foreground">
                  {prize.name.length > 6 ? prize.name.slice(0, 5) + "..." : prize.name}
                </div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">
                  {PRIZE_TYPE_LABELS[prize.type] || prize.type}
                </div>
              </>
            ) : (
              <div className="text-xs text-muted-foreground">空</div>
            )}
          </div>
        );
      })}
    </div>
  );
}
