"use client";

import * as React from "react";
import Image from "next/image";
import { useDropzone, type FileRejection } from "react-dropzone";
import { AlertCircle, ImagePlus, Link as LinkIcon, Loader2, Trash2, Upload } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export type ImageDropzoneUploadResult = {
  /** 浏览器侧可直接访问的预览 URL（如代理 URL / 外链 / CDN） */
  url: string;
  /** 其它附带数据（如后端返回的持久化引用 storage://...）；父组件自行处理 */
  [key: string]: unknown;
};

export type ImageDropzoneProps = {
  /** 当前预览的图片 URL（受控）；父组件通常传入 form 的展示地址 */
  value?: string;
  /** URL 变化时触发：
   *  - 上传成功：result 来自 onUpload 返回值（含 url 及附加字段）
   *  - 手动粘贴：仅包含 { url }
   *  - 清除：url 为空字符串 */
  onChange: (result: ImageDropzoneUploadResult) => void;
  /** 实际执行上传，返回 { url, ...extras } */
  onUpload: (file: File) => Promise<ImageDropzoneUploadResult>;
  /** 允许的 mime，默认图片全集 */
  accept?: Record<string, string[]>;
  /** 最大大小（字节），默认 10 MB */
  maxSize?: number;
  /** 预览区高宽比（CSS aspect-ratio），默认 3/1 */
  aspect?: string;
  /** 是否显示手动输入 URL 的兜底输入框（默认 true） */
  allowUrlInput?: boolean;
  /** 辅助文案 */
  description?: string;
  /** 禁用 */
  disabled?: boolean;
  className?: string;
};

/**
 * 图片拖放上传组件（react-dropzone）。
 *
 * 设计取向：
 *   - 视觉与 shadcn/ui 一致：rounded-lg + border-dashed + bg-card/bg-muted 层次
 *   - Radix 语义：role="button" + focus-visible 环 + aria-label
 *   - 拖拽态：active 高亮边框 + 半透明提示层
 *   - 拒绝态：destructive 色徽带文案
 *   - 浅色扁平 / 深色轻辉光，全部通过 CSS 变量驱动，不硬编码
 *   - 兜底：同时提供 URL 输入框，允许直接粘贴外链，兼顾便捷
 */
export function ImageDropzone({
  value,
  onChange,
  onUpload,
  accept = {
    "image/jpeg": [".jpg", ".jpeg"],
    "image/png": [".png"],
    "image/gif": [".gif"],
    "image/webp": [".webp"],
    "image/svg+xml": [".svg"]
  },
  maxSize = 10 * 1024 * 1024,
  aspect = "3 / 1",
  allowUrlInput = true,
  description = "拖拽图片到此处，或点击选择文件（支持 JPG / PNG / GIF / WEBP / SVG，≤10 MB）",
  disabled = false,
  className
}: ImageDropzoneProps) {
  const [uploading, setUploading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [urlDraft, setUrlDraft] = React.useState(value ?? "");

  React.useEffect(() => setUrlDraft(value ?? ""), [value]);

  const handleDrop = React.useCallback(
    async (accepted: File[], rejections: FileRejection[]) => {
      setError(null);
      if (rejections.length > 0) {
        const first = rejections[0]?.errors?.[0];
        setError(first?.message || "文件无法接收");
        return;
      }
      const file = accepted[0];
      if (!file) return;
      try {
        setUploading(true);
        const result = await onUpload(file);
        onChange(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : "上传失败，请重试");
      } finally {
        setUploading(false);
      }
    },
    [onUpload, onChange]
  );

  const { getRootProps, getInputProps, isDragActive, isDragReject, open } = useDropzone({
    onDrop: handleDrop,
    accept,
    maxSize,
    multiple: false,
    disabled: disabled || uploading,
    // 我们用自己的按钮 open；避免整个预览区也被点击触发选择
    noClick: Boolean(value),
    noKeyboard: Boolean(value)
  });

  const hasPreview = Boolean(value);

  return (
    <div className={cn("space-y-2", className)}>
      <div
        {...getRootProps()}
        className={cn(
          "group relative overflow-hidden rounded-lg border border-dashed bg-card transition-colors",
          "focus-within:outline-none focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background",
          isDragActive && !isDragReject && "border-primary/70 bg-accent/30",
          isDragReject && "border-destructive/70 bg-destructive/5",
          !isDragActive && !isDragReject && "hover:border-border/80",
          disabled && "pointer-events-none opacity-60"
        )}
        style={{ aspectRatio: aspect }}
        aria-label={hasPreview ? "Banner 图片预览（可替换）" : "拖放或点击上传 Banner 图片"}
      >
        <input {...getInputProps()} />

        {hasPreview ? (
          <>
            {/* next/image 预览：
                  - 用户输入的 URL 可能是 data:/blob:/任意外链，走 unoptimized 跳过 Next 图像管线
                    兼容性最强，不依赖 next.config 的 remotePatterns 额外放行
                  - fill + sizes 按父容器 aspect 撑开；object-cover 充满 */}
            <Image
              src={value ?? ""}
              alt="Banner 预览"
              fill
              sizes="(max-width: 640px) 100vw, 600px"
              unoptimized
              className="object-cover"
              onError={(e) => {
                (e.currentTarget as HTMLImageElement).style.display = "none";
              }}
            />
            {/* 悬浮操作层 */}
            <div
              className={cn(
                "absolute inset-0 flex items-center justify-center gap-2",
                "bg-background/70 opacity-0 backdrop-blur-sm transition-opacity",
                "group-hover:opacity-100 focus-within:opacity-100",
                isDragActive && "opacity-100"
              )}
            >
              <Button type="button" size="sm" variant="secondary" onClick={open} disabled={uploading}>
                {uploading ? <Loader2 className="size-3.5 animate-spin" /> : <Upload className="size-3.5" />}
                {uploading ? "上传中…" : "替换"}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={(e) => {
                  e.stopPropagation();
                  onChange({ url: "" });
                  setUrlDraft("");
                }}
                disabled={uploading}
              >
                <Trash2 className="size-3.5" /> 清除
              </Button>
            </div>
          </>
        ) : (
          <div className="flex size-full flex-col items-center justify-center gap-2 px-4 text-center">
            {uploading ? (
              <>
                <Loader2 className="size-5 animate-spin text-muted-foreground" />
                <span className="text-xs text-muted-foreground">上传中…</span>
              </>
            ) : (
              <>
                <div className="grid size-10 place-items-center rounded-full border bg-background">
                  <ImagePlus className="size-4 text-muted-foreground" />
                </div>
                <div className="space-y-0.5">
                  <p className="text-sm font-medium">
                    {isDragActive
                      ? isDragReject
                        ? "文件类型或大小不符"
                        : "松开鼠标上传"
                      : "拖放图片或点击上传"}
                  </p>
                  <p className="text-[11px] text-muted-foreground">{description}</p>
                </div>
              </>
            )}
          </div>
        )}
      </div>

      {error ? (
        <div
          role="alert"
          className="inline-flex items-center gap-1.5 rounded-md border border-destructive/40 bg-destructive/5 px-2.5 py-1.5 text-xs text-destructive"
        >
          <AlertCircle className="size-3.5 shrink-0" />
          <span className="truncate">{error}</span>
        </div>
      ) : null}

      {allowUrlInput ? (
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <LinkIcon className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={urlDraft}
              onChange={(e) => setUrlDraft(e.target.value)}
              onBlur={() => {
                const next = urlDraft.trim();
                if (next !== (value ?? "")) onChange({ url: next });
              }}
              placeholder="或粘贴图片 URL..."
              className="h-8 pl-8 text-xs"
              disabled={disabled || uploading}
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}
