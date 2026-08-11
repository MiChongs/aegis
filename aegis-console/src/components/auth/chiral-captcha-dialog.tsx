"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  CheckCircle2,
  CircleDashed,
  Loader2,
  MousePointerClick,
  RefreshCw,
  Undo2,
  XCircle,
} from "lucide-react";
import { generateCaptcha, verifyCaptchaClick } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import type { CaptchaGenerateResult } from "@/lib/api/captcha";

type ClickPos = { x: number; y: number };

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onVerified: (captchaId: string) => void;
};

function decodeChiralCount(encoded?: string) {
  if (!encoded) return 0;
  try {
    const raw = atob(encoded.replace(/-/g, "+").replace(/_/g, "/"));
    if (raw.length !== 8) return 0;
    const salt = new Uint8Array(4);
    const val = new Uint8Array(4);
    for (let i = 0; i < 4; i++) {
      salt[i] = raw.charCodeAt(i);
      val[i] = raw.charCodeAt(i + 4);
    }
    const sv = new DataView(salt.buffer);
    const vv = new DataView(val.buffer);
    return (sv.getUint32(0, true) ^ vv.getUint32(0, true)) >>> 0;
  } catch {
    return 0;
  }
}

export function ChiralCaptchaDialog({ open, onOpenChange, onVerified }: Props) {
  const [captcha, setCaptcha] = useState<CaptchaGenerateResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [clicks, setClicks] = useState<ClickPos[]>([]);
  const [result, setResult] = useState<"success" | "fail" | null>(null);
  const imgRef = useRef<HTMLImageElement>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setClicks([]);
    setResult(null);
    try {
      const res = await generateCaptcha({ type: "chiral", purpose: "admin_login" });
      setCaptcha(res);
    } catch {
      setCaptcha(null);
    } finally {
      setLoading(false);
    }
  }, []);

  const handleOpenChange = useCallback((nextOpen: boolean) => {
    if (nextOpen) {
      void refresh();
    } else {
      setCaptcha(null);
      setClicks([]);
      setResult(null);
    }
    onOpenChange(nextOpen);
  }, [onOpenChange, refresh]);

  useEffect(() => {
    if (!open) {
      setCaptcha(null);
      setClicks([]);
      setResult(null);
    }
  }, [open]);

  function handleImageClick(e: React.MouseEvent<HTMLImageElement>) {
    if (!imgRef.current || !captcha || result === "success" || verifying) return;
    const rect = imgRef.current.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;
    setClicks((prev) => [...prev, { x, y }]);
    setResult(null);
  }

  function handleUndo() {
    setClicks((prev) => prev.slice(0, -1));
    setResult(null);
  }

  function handleRemovePoint(index: number) {
    setClicks((prev) => prev.filter((_, i) => i !== index));
    setResult(null);
  }

  function handleReset() {
    setClicks([]);
    setResult(null);
  }

  async function handleVerify() {
    if (!captcha || clicks.length === 0) return;
    setVerifying(true);
    setResult(null);
    try {
      const res = await verifyCaptchaClick({
        captchaId: captcha.captchaId,
        clicks,
      });
      if (res.valid) {
        setResult("success");
        onVerified(captcha.captchaId);
        window.setTimeout(() => handleOpenChange(false), 700);
      } else {
        setResult("fail");
      }
    } catch {
      setResult("fail");
    } finally {
      setVerifying(false);
    }
  }

  const chiralCount = decodeChiralCount(captcha?.chiralCount);
  const selectedCount = clicks.length;
  const remainingCount = chiralCount > 0 ? Math.max(chiralCount - selectedCount, 0) : 0;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="border-border bg-background p-0 shadow-none sm:max-w-4xl">
        <DialogHeader className="gap-1 border-b px-5 py-4 sm:px-6">
          <DialogTitle className="flex items-center gap-2 text-lg">
            <MousePointerClick className="size-4 text-foreground/70" />
            手性碳验证
          </DialogTitle>
          <DialogDescription className="text-sm text-muted-foreground">
            点击图中的手性碳位置
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 px-5 py-4 sm:px-6">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
            <span>已选 {selectedCount} 个</span>
            {chiralCount > 0 ? <span>目标 {chiralCount} 个</span> : null}
            {remainingCount > 0 ? <span>剩余 {remainingCount} 个</span> : null}
            {result === "success" ? <span className="text-emerald-600">验证通过</span> : null}
            {result === "fail" ? <span className="text-destructive">验证失败</span> : null}
          </div>

          <div className="overflow-hidden border bg-white">
            {loading ? (
              <div className="flex h-[320px] items-center justify-center sm:h-[420px]">
                <div className="flex flex-col items-center gap-2 text-slate-500">
                  <Loader2 className="size-6 animate-spin" />
                  <p className="text-sm">加载中...</p>
                </div>
              </div>
            ) : captcha?.imageData ? (
              <div className="relative">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  ref={imgRef}
                  src={captcha.imageData}
                  alt="分子结构手性碳验证题面"
                  className="h-[320px] w-full cursor-crosshair select-none object-contain p-4 sm:h-[420px]"
                  draggable={false}
                  onClick={handleImageClick}
                />

                {clicks.map((pos, i) => (
                  <div
                    key={`${pos.x}-${pos.y}-${i}`}
                    className="pointer-events-none absolute flex size-8 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-sky-500 bg-sky-100"
                    style={{ left: `${pos.x * 100}%`, top: `${pos.y * 100}%` }}
                  >
                    <span className="text-[11px] font-semibold text-sky-700">{i + 1}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex h-[320px] items-center justify-center sm:h-[420px]">
                <div className="flex flex-col items-center gap-2 text-slate-500">
                  <CircleDashed className="size-6" />
                  <p className="text-sm">加载失败</p>
                </div>
              </div>
            )}
          </div>

          <div className="flex min-h-9 flex-wrap gap-2">
            {clicks.length === 0 ? (
              <div className="text-sm text-muted-foreground">未选择点位</div>
            ) : (
              clicks.map((point, index) => (
                <button
                  key={`chip-${point.x}-${point.y}-${index}`}
                  type="button"
                  onClick={() => handleRemovePoint(index)}
                  className="border border-border bg-background px-3 py-1.5 text-sm text-foreground transition-colors hover:border-foreground/30"
                >
                  点位 {index + 1}
                </button>
              ))
            )}
          </div>
        </div>

        <DialogFooter className="border-t px-5 py-4 sm:px-6">
          <div className="flex w-full flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="gap-1.5"
                disabled={clicks.length === 0 || result === "success"}
                onClick={handleUndo}
              >
                <Undo2 className="size-3.5" />
                撤销
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="gap-1.5"
                disabled={clicks.length === 0 || result === "success"}
                onClick={handleReset}
              >
                清空
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="gap-1.5"
                disabled={loading || verifying}
                onClick={() => void refresh()}
              >
                <RefreshCw className={cn("size-3.5", (loading || verifying) && "animate-spin")} />
                刷新
              </Button>
            </div>

            <div className="flex flex-col-reverse gap-2 sm:flex-row">
              <DialogClose asChild>
                <Button type="button" variant="outline">
                  取消
                </Button>
              </DialogClose>
              <Button
                type="button"
                className="min-w-28 gap-1.5"
                disabled={clicks.length === 0 || verifying || result === "success" || loading}
                onClick={() => void handleVerify()}
              >
                {verifying ? <Loader2 className="size-4 animate-spin" /> : result === "success" ? <CheckCircle2 className="size-4" /> : result === "fail" ? <XCircle className="size-4" /> : <MousePointerClick className="size-4" />}
                {verifying ? "验证中" : "确认验证"}
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
