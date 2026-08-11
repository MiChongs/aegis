"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { CheckCircle2, ChevronRight, MousePointerClick, Pause, Play, RefreshCw, ShieldCheck, Volume2 } from "lucide-react";
import { generateCaptcha } from "@/lib/api-client";
import { ChiralCaptchaDialog } from "./chiral-captcha-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { AdminCaptchaPublicConfig } from "@/lib/api/captcha";

type CaptchaFieldProps = {
  config: AdminCaptchaPublicConfig;
  captchaId: string;
  onCaptchaIdChange: (id: string) => void;
};

/**
 * 验证码输入字段（支持静态图片/GIF动画/音频WAV）
 * 供登录和注册表单复用
 */
export function CaptchaField({ config, captchaId, onCaptchaIdChange }: CaptchaFieldProps) {
  const [imageData, setImageData] = useState<string | null>(null);
  const [audioData, setAudioData] = useState<string | null>(null);
  const [mimeType, setMimeType] = useState<string>("image/png");
  const [loading, setLoading] = useState(false);
  const [audioMode, setAudioMode] = useState(false);
  const [chiralOpen, setChiralOpen] = useState(false);
  const [chiralVerified, setChiralVerified] = useState(false);

  const isChiral = config.type === "chiral";
  const requestType = isChiral ? "chiral" : audioMode ? "audio" : (config.type || "image");

  // 手性碳验证码：不需要自动生成，由 Dialog 内部处理
  const refresh = useCallback(async () => {
    if (isChiral) return;
    setLoading(true);
    try {
      const result = await generateCaptcha({
        type: requestType,
        purpose: "admin_login"
      });
      setImageData(result.imageData || null);
      setAudioData(result.audioData || null);
      setMimeType(result.mimeType || "image/png");
      onCaptchaIdChange(result.captchaId);
    } catch {
      setImageData(null);
      setAudioData(null);
    } finally {
      setLoading(false);
    }
  }, [isChiral, requestType, onCaptchaIdChange]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const isAudio = mimeType === "audio/wav" || !!audioData;

  // 手性碳验证码：Dialog 模式
  if (isChiral) {
    return (
      <div className="grid gap-2">
        <Label>验证码</Label>
        <div
          className={cn(
            "rounded-2xl border px-4 py-3 transition-colors",
            chiralVerified
              ? "border-emerald-300 bg-background"
              : "border-border bg-background hover:border-foreground/20"
          )}
        >
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                {chiralVerified ? (
                  <span className="flex items-center gap-1 text-sm font-medium text-emerald-700 dark:text-emerald-300">
                    <CheckCircle2 className="size-4" />
                    手性碳验证
                  </span>
                ) : (
                  <span className="flex items-center gap-1 text-sm font-medium text-foreground">
                    <MousePointerClick className="size-4 text-muted-foreground" />
                    手性碳验证
                  </span>
                )}
              </div>
              <p className="text-xs text-muted-foreground">{chiralVerified ? "已完成" : "点击开始"}</p>
            </div>

            <Button
              type="button"
              variant={chiralVerified ? "secondary" : "outline"}
              className="h-9 gap-2 px-4"
              onClick={() => setChiralOpen(true)}
            >
              {chiralVerified ? <ShieldCheck className="size-4" /> : <MousePointerClick className="size-4" />}
              {chiralVerified ? "重新检查" : "开始验证"}
              <ChevronRight className="size-4 opacity-70" />
            </Button>
          </div>
        </div>
        <input type="hidden" name="captchaId" value={captchaId} />
        <input type="hidden" name="captchaAnswer" value={chiralVerified ? "verified" : ""} />
        <ChiralCaptchaDialog
          open={chiralOpen}
          onOpenChange={setChiralOpen}
          onVerified={(id) => {
            onCaptchaIdChange(id);
            setChiralVerified(true);
          }}
        />
      </div>
    );
  }

  // 标准验证码（图片/GIF/音频）
  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between">
        <Label htmlFor="captchaAnswer">验证码</Label>
        {config.type !== "audio" && (
          <button
            type="button"
            className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors"
            onClick={() => setAudioMode((v) => !v)}
          >
            <Volume2 className="size-3" />
            {audioMode ? "切换图片" : "听声音"}
          </button>
        )}
      </div>

      <div className="flex items-center gap-2">
        {loading ? (
          <Skeleton className="h-10 w-28 shrink-0 rounded" />
        ) : isAudio && audioData ? (
          <AudioPlayer key={audioData} src={audioData} />
        ) : imageData ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={imageData}
            alt="验证码"
            className="h-10 shrink-0 cursor-pointer rounded border"
            onClick={() => void refresh()}
          />
        ) : (
          <div className="flex h-10 w-28 shrink-0 items-center justify-center rounded border border-dashed text-xs text-muted-foreground">
            加载失败
          </div>
        )}
        <Input
          id="captchaAnswer"
          name="captchaAnswer"
          autoComplete="off"
          placeholder={isAudio ? "输入听到的数字" : "请输入验证码"}
          className="flex-1"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-10 shrink-0"
          onClick={() => void refresh()}
          disabled={loading}
        >
          <RefreshCw className={`size-4 ${loading ? "animate-spin" : ""}`} />
        </Button>
      </div>
      <input type="hidden" name="captchaId" value={captchaId} />
    </div>
  );
}

// ── 自定义音频播放器 ──

function AudioPlayer({ src }: { src: string }) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const [playing, setPlaying] = useState(false);
  const [progress, setProgress] = useState(0);

  useEffect(() => {
    const el = audioRef.current;
    if (!el) return;
    const onTimeUpdate = () => {
      if (el.duration) setProgress((el.currentTime / el.duration) * 100);
    };
    const onEnded = () => { setPlaying(false); setProgress(0); };
    el.addEventListener("timeupdate", onTimeUpdate);
    el.addEventListener("ended", onEnded);
    return () => {
      el.removeEventListener("timeupdate", onTimeUpdate);
      el.removeEventListener("ended", onEnded);
    };
  }, []);

  function toggle() {
    const el = audioRef.current;
    if (!el) return;
    if (playing) {
      el.pause();
      setPlaying(false);
    } else {
      void el.play();
      setPlaying(true);
    }
  }

  return (
    <div className="flex h-10 w-36 shrink-0 items-center gap-1.5 rounded-lg border px-2">
      <audio ref={audioRef} src={src} preload="auto" />
      <button type="button" onClick={toggle} className="flex size-6 shrink-0 items-center justify-center rounded-full bg-foreground text-background transition-transform hover:scale-110">
        {playing ? <Pause className="size-3" /> : <Play className="size-3 ml-0.5" />}
      </button>
      <div className="relative h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
        <div className="absolute inset-y-0 left-0 rounded-full bg-foreground transition-all duration-150" style={{ width: `${progress}%` }} />
      </div>
    </div>
  );
}
