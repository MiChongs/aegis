"use client";

import { useState } from "react";
import { ChevronDown, ChevronUp, PartyPopper } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import type { LotteryDrawResult } from "@/lib/api/lottery";

const PRIZE_TYPE_LABELS: Record<string, string> = {
  points: "积分",
  experience: "经验值",
  item: "实物",
  coupon: "兑换码",
  custom: "自定义"
};

export function LotteryResultDialog({
  result,
  open,
  onClose
}: {
  result: LotteryDrawResult | null;
  open: boolean;
  onClose: () => void;
}) {
  const [showProof, setShowProof] = useState(false);

  if (!result) return null;

  const { prize, drawSeed, drawProof, seedHash } = result;

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <div className="flex justify-center pb-2">
            <div className="flex size-14 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-900/40">
              <PartyPopper className="size-7 text-amber-600 dark:text-amber-400" />
            </div>
          </div>
          <DialogTitle className="text-center text-xl">
            恭喜中奖！
          </DialogTitle>
          <DialogDescription className="text-center">
            您获得了以下奖品
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-3 py-4">
          <div className="text-2xl font-bold text-foreground">{prize.name}</div>
          <div className="flex items-center gap-2">
            <Badge variant="secondary">
              {PRIZE_TYPE_LABELS[prize.type] || prize.type}
            </Badge>
            {prize.value && (
              <span className="text-sm text-muted-foreground">
                价值: {prize.value}
              </span>
            )}
          </div>
        </div>

        {/* 可折叠的验证信息 */}
        {(drawSeed || drawProof || seedHash) && (
          <div className="rounded-xl border border-border/60 bg-muted/30 p-3">
            <button
              className="flex w-full items-center justify-between text-xs text-muted-foreground"
              onClick={() => setShowProof(!showProof)}
            >
              <span>抽奖证明</span>
              {showProof ? (
                <ChevronUp className="size-3.5" />
              ) : (
                <ChevronDown className="size-3.5" />
              )}
            </button>
            {showProof && (
              <div className="mt-2 space-y-1.5 text-xs">
                {seedHash && (
                  <div>
                    <span className="text-muted-foreground">种子哈希: </span>
                    <span className="break-all font-mono text-foreground">{seedHash}</span>
                  </div>
                )}
                {drawSeed && (
                  <div>
                    <span className="text-muted-foreground">抽奖种子: </span>
                    <span className="break-all font-mono text-foreground">{drawSeed}</span>
                  </div>
                )}
                {drawProof && (
                  <div>
                    <span className="text-muted-foreground">证明: </span>
                    <span className="break-all font-mono text-foreground">{drawProof}</span>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button className="w-full" onClick={onClose}>
            确定
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
