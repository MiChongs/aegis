"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  Gauge,
  Image as ImageIcon,
  Loader2,
  RefreshCw,
  RotateCcw,
  Timer,
  Waves
} from "lucide-react";
import type { CaptchaDynamicConfig, CaptchaDynamicPreview } from "@/lib/api/captcha";
import { usePreviewDynamicCaptchaMutation } from "@/lib/admin-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/** 动态验证码外观设计器：应用级与平台级共用，改参数即时出样张。 */

type Props = {
  value: CaptchaDynamicConfig;
  onChange: (next: CaptchaDynamicConfig) => void;
  /** 样张走哪条路由：应用面板按应用授权，平台面板按平台授权 */
  scope: "app" | "platform";
  appKey?: string | null;
  enabled?: boolean;
  disabledHint?: string;
};

const MODE_OPTIONS = [
  { value: "alnum", label: "字母 + 数字", hint: "可选空间最大" },
  { value: "alpha", label: "纯字母", hint: "适合语音播报场景" },
  { value: "digit", label: "纯数字", hint: "键盘友好，可选空间最小" }
] as const;

/** 预设：一次给一整套自洽参数 */
const PRESETS: Array<{ key: string; label: string; hint: string; value: CaptchaDynamicConfig }> = [
  {
    key: "light",
    label: "轻快",
    hint: "干扰低、动得少、体积小",
    value: { length: 4, width: 220, height: 76, frames: 10, frameDelayMs: 110, mode: "alnum", noise: 20, wobble: 25 }
  },
  {
    key: "standard",
    label: "标准",
    hint: "辨识度与对抗性平衡",
    value: { length: 5, width: 240, height: 80, frames: 12, frameDelayMs: 90, mode: "alnum", noise: 45, wobble: 55 }
  },
  {
    key: "strong",
    label: "强对抗",
    hint: "字符更多、干扰更密，识别更难",
    value: { length: 6, width: 300, height: 96, frames: 18, frameDelayMs: 70, mode: "alnum", noise: 78, wobble: 80 }
  },
  {
    key: "accessible",
    label: "无障碍",
    hint: "几乎不抖不扭，纯数字",
    value: { length: 4, width: 260, height: 88, frames: 8, frameDelayMs: 140, mode: "digit", noise: 12, wobble: 0 }
  }
];

const DEFAULT_CONFIG = PRESETS[1].value;

export function normalizeDynamicConfig(input?: Partial<CaptchaDynamicConfig> | null): CaptchaDynamicConfig {
  return {
    ...DEFAULT_CONFIG,
    ...input,
    mode: String(input?.mode ?? DEFAULT_CONFIG.mode)
  };
}

export function DynamicCaptchaDesigner({ value, onChange, scope, appKey, enabled = true, disabledHint }: Props) {
  const previewMutation = usePreviewDynamicCaptchaMutation(scope, appKey);
  const [preview, setPreview] = useState<CaptchaDynamicPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  // 拖滑块时防抖，避免每帧一次请求
  const debounced = useDebouncedValue(value, 350);
  const signature = useMemo(
    () => [debounced.length, debounced.width, debounced.height, debounced.frames, debounced.frameDelayMs, debounced.mode, debounced.noise, debounced.wobble].join("/"),
    [debounced]
  );

  // mutateAsync 每次渲染都是新引用，进依赖会变成自触发循环
  const runPreviewRef = useRef(previewMutation.mutateAsync);
  useEffect(() => {
    runPreviewRef.current = previewMutation.mutateAsync;
  });

  const canPreview = scope === "platform" || Boolean(appKey);

  useEffect(() => {
    if (!canPreview) return;
    let cancelled = false;
    runPreviewRef
      .current(debounced)
      .then((result) => {
        if (cancelled) return;
        setPreview(result);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "样张渲染失败");
      });
    return () => {
      cancelled = true;
    };
    // 依赖 signature 而非 debounced：对象每次渲染都是新引用
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature, nonce, canPreview]);

  const patch = useCallback(
    (part: Partial<CaptchaDynamicConfig>) => onChange({ ...value, ...part }),
    [onChange, value]
  );

  const activePreset = PRESETS.find((preset) => isSameConfig(preset.value, value))?.key ?? "";
  const clamped = preview ? diffFields(value, preview.applied) : [];

  return (
    <div className="space-y-4">
      {!enabled ? (
        <div className="flex items-start gap-2.5 rounded-2xl border border-amber-200 bg-amber-50/60 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-200">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <span>{disabledHint ?? "动态验证码当前未启用，这里的调整不会有人看到。"}</span>
        </div>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        <PreviewPane
          preview={preview}
          error={error}
          loading={previewMutation.isPending}
          canPreview={canPreview}
          onRefresh={() => setNonce((n) => n + 1)}
        />

        <div className="space-y-5 rounded-2xl border p-5">
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <Label className="text-xs text-muted-foreground">预设</Label>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 gap-1 px-2 text-xs text-muted-foreground"
                onClick={() => onChange({ ...DEFAULT_CONFIG })}
              >
                <RotateCcw className="size-3" />
                恢复默认
              </Button>
            </div>
            <div className="flex flex-wrap gap-2">
              {PRESETS.map((preset) => (
                <Tooltip key={preset.key}>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      variant={activePreset === preset.key ? "default" : "outline"}
                      size="sm"
                      className="h-8 rounded-full px-3.5 text-xs"
                      onClick={() => onChange({ ...preset.value })}
                    >
                      {preset.label}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="max-w-64">{preset.hint}</TooltipContent>
                </Tooltip>
              ))}
              {activePreset === "" ? (
                <Badge variant="outline" className="h-8 rounded-full px-3 text-xs font-normal text-muted-foreground">
                  自定义
                </Badge>
              ) : null}
            </div>
          </div>

          <div className="grid gap-5 sm:grid-cols-2">
            <SliderField
              label="字符数"
              value={value.length}
              min={3}
              max={8}
              suffix=" 位"
              hint={value.length <= 4 ? "好认，可猜空间小" : value.length >= 7 ? "更难猜，输入更长" : "常规选择"}
              onChange={(next) => patch({ length: next })}
            />
            <SliderField
              label="干扰强度"
              icon={<Waves className="size-3.5" />}
              value={value.noise}
              min={0}
              max={100}
              hint={describeNoise(value.noise)}
              onChange={(next) => patch({ noise: next })}
            />
            <SliderField
              label="运动幅度"
              icon={<Gauge className="size-3.5" />}
              value={value.wobble}
              min={0}
              max={100}
              hint={describeWobble(value.wobble)}
              onChange={(next) => patch({ wobble: next })}
            />
            <SliderField
              label="帧数"
              value={value.frames}
              min={4}
              max={40}
              suffix=" 帧"
              hint="帧越多越流畅，体积越大"
              onChange={(next) => patch({ frames: next })}
            />
            <SliderField
              label="帧间隔"
              icon={<Timer className="size-3.5" />}
              value={value.frameDelayMs}
              min={20}
              max={300}
              step={10}
              suffix=" ms"
              hint={`一轮 ${formatDuration(value.frames * value.frameDelayMs)}`}
              onChange={(next) => patch({ frameDelayMs: next })}
            />
            <SliderField
              label="画布宽度"
              icon={<ImageIcon className="size-3.5" />}
              value={value.width}
              min={80}
              max={640}
              step={10}
              suffix=" px"
              hint={`当前 ${value.width} × ${value.height}`}
              onChange={(next) => patch({ width: next })}
            />
            <SliderField
              label="画布高度"
              value={value.height}
              min={40}
              max={240}
              step={4}
              suffix=" px"
              hint="过矮会让字形挤在一起"
              onChange={(next) => patch({ height: next })}
            />
            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">字符集</Label>
              <ToggleGroup
                type="single"
                variant="outline"
                size="sm"
                value={value.mode}
                onValueChange={(next) => next && patch({ mode: next })}
                className="w-full"
              >
                {MODE_OPTIONS.map((option) => (
                  <ToggleGroupItem key={option.value} value={option.value} className="flex-1 text-xs">
                    {option.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
              <p className="text-[11px] text-muted-foreground">
                {MODE_OPTIONS.find((option) => option.value === value.mode)?.hint ?? "已剔除易混字符"}
              </p>
            </div>
          </div>

          {clamped.length > 0 ? (
            <div className="rounded-xl border border-amber-200 bg-amber-50/60 px-3.5 py-2.5 text-xs text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-200">
              超出范围已自动调整：{clamped.join("、")}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

// ── 预览区 ──

function PreviewPane({
  preview,
  error,
  loading,
  canPreview,
  onRefresh
}: {
  preview: CaptchaDynamicPreview | null;
  error: string | null;
  loading: boolean;
  canPreview: boolean;
  onRefresh: () => void;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-2xl border p-5">
      <div className="flex items-center justify-between gap-2">
        <div className="text-sm font-medium">实时预览</div>
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="size-8 shrink-0"
          onClick={onRefresh}
          disabled={loading || !canPreview}
          aria-label="换一张"
        >
          <RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
        </Button>
      </div>

      <div className="relative flex min-h-32 items-center justify-center overflow-hidden rounded-xl border border-dashed bg-[linear-gradient(45deg,var(--muted)_25%,transparent_25%,transparent_75%,var(--muted)_75%),linear-gradient(45deg,var(--muted)_25%,transparent_25%,transparent_75%,var(--muted)_75%)] bg-[length:16px_16px] bg-[position:0_0,8px_8px] p-3">
        {/* 加载时保留上一张，避免闪白 */}
        {preview ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={preview.imageData}
            alt="动态验证码样张"
            className={cn("max-w-full rounded-md shadow-sm transition-opacity", loading && "opacity-60")}
            style={{ imageRendering: "auto" }}
          />
        ) : error ? (
          <div className="px-4 text-center text-xs text-destructive">{error}</div>
        ) : canPreview ? (
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        ) : (
          <div className="px-4 text-center text-xs text-muted-foreground">请先选择应用</div>
        )}
      </div>

      {preview ? (
        <div className="space-y-2.5">
          <div className="flex items-center gap-2 text-sm">
            <span className="text-xs text-muted-foreground">答案</span>
            <code className="rounded-md bg-muted px-2 py-0.5 font-mono text-sm tracking-[0.2em]">
              {preview.answer}
            </code>
          </div>
          <div className="flex flex-wrap gap-1.5">
            <Metric label={`${preview.frames} 帧`} />
            <Metric label={`一轮 ${formatDuration(preview.durationMs)}`} />
            <Metric label={`${preview.width} × ${preview.height}`} />
            <Metric label={formatBytes(preview.byteSize)} tone={preview.byteSize > 150_000 ? "warn" : "muted"} />
          </div>
          {preview.byteSize > 150_000 ? (
            <p className="text-[11px] text-amber-700 dark:text-amber-300">
              体积偏大，弱网加载慢，可减少帧数或缩小画布。
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function Metric({ label, tone = "muted" }: { label: string; tone?: "muted" | "warn" }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "rounded-full px-2.5 py-0.5 text-[11px] font-normal",
        tone === "warn" ? "border-amber-300 text-amber-700 dark:border-amber-800 dark:text-amber-300" : "text-muted-foreground"
      )}
    >
      {label}
    </Badge>
  );
}

// ── 滑块 ──

function SliderField({
  label,
  icon,
  value,
  min,
  max,
  step = 1,
  suffix = "",
  hint,
  onChange
}: {
  label: string;
  icon?: React.ReactNode;
  value: number;
  min: number;
  max: number;
  step?: number;
  suffix?: string;
  hint?: string;
  onChange: (value: number) => void;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <Label className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {icon}
          {label}
        </Label>
        <span className="font-mono text-xs tabular-nums">
          {value}
          {suffix}
        </span>
      </div>
      <Slider
        value={[value]}
        min={min}
        max={max}
        step={step}
        onValueChange={([next]) => onChange(next)}
        aria-label={label}
      />
      {hint ? <p className="text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

// ── 文案与格式化 ──

function describeNoise(value: number) {
  if (value === 0) return "无噪点与干扰线";
  if (value < 25) return "少量噪点";
  if (value < 60) return "适中";
  if (value < 85) return "偏强";
  return "很强，建议配合较大画布";
}

function describeWobble(value: number) {
  if (value === 0) return "字形不动，仅逐帧变色";
  if (value < 30) return "轻微浮动";
  if (value < 65) return "适中";
  return "很强，辨识度下降";
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2).replace(/\.?0+$/, "")}s`;
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)}MB`;
}

function isSameConfig(a: CaptchaDynamicConfig, b: CaptchaDynamicConfig) {
  return (
    a.length === b.length &&
    a.width === b.width &&
    a.height === b.height &&
    a.frames === b.frames &&
    a.frameDelayMs === b.frameDelayMs &&
    a.mode === b.mode &&
    a.noise === b.noise &&
    a.wobble === b.wobble
  );
}

/** 被服务端夹过的字段，形如「帧数 60（13）」 */
function diffFields(draft: CaptchaDynamicConfig, applied: CaptchaDynamicConfig) {
  const labels: Array<[keyof CaptchaDynamicConfig, string]> = [
    ["length", "字符数"],
    ["width", "宽度"],
    ["height", "高度"],
    ["frames", "帧数"],
    ["frameDelayMs", "帧间隔"],
    ["mode", "字符集"],
    ["noise", "干扰强度"],
    ["wobble", "运动幅度"]
  ];
  return labels
    .filter(([key]) => draft[key] !== applied[key])
    .map(([key, label]) => `${label} ${draft[key]}（${applied[key]}）`);
}
