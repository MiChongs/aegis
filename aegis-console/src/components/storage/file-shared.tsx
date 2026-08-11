"use client";

import * as React from "react";
import {
  Archive, FileAudio, FileCode2, FileImage, FileSpreadsheet, FileText, FileVideo,
  File as FileIcon, Folder, Loader2, ShieldAlert,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { storageThumbnailUrl } from "@/lib/api/storage-resource";
import { useAdminToken } from "@/lib/admin-hooks";
import type { StorageObject } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/* ================================================================== */
/*  文件分类                                                           */
/* ================================================================== */

export type FileKind = "image" | "video" | "audio" | "pdf" | "archive" | "code" | "sheet" | "text" | "other";

const KIND_ICONS: Record<FileKind, React.ComponentType<{ className?: string }>> = {
  image: FileImage,
  video: FileVideo,
  audio: FileAudio,
  pdf: FileText,
  archive: Archive,
  code: FileCode2,
  sheet: FileSpreadsheet,
  text: FileText,
  other: FileIcon,
};

/** 每类一个色相。同色系的图标等于没有图标 —— 扫一眼列表分不出图片和压缩包 */
const KIND_TONES: Record<FileKind, string> = {
  image: "text-sky-500",
  video: "text-violet-500",
  audio: "text-pink-500",
  pdf: "text-red-500",
  archive: "text-amber-500",
  code: "text-emerald-500",
  sheet: "text-green-600",
  text: "text-slate-500",
  other: "text-muted-foreground",
};

export const KIND_LABELS: Record<FileKind, string> = {
  image: "图片", video: "视频", audio: "音频", pdf: "PDF",
  archive: "压缩包", code: "代码", sheet: "表格", text: "文本", other: "其他",
};

const ARCHIVE_TYPES = new Set([
  "application/zip", "application/x-tar", "application/gzip", "application/x-7z-compressed",
  "application/x-rar-compressed", "application/vnd.rar", "application/x-bzip2", "application/x-xz",
]);
const SHEET_TYPES = new Set([
  "application/vnd.ms-excel",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "text/csv",
]);
const CODE_TYPES = new Set([
  "application/json", "application/xml", "text/xml", "application/javascript",
  "text/javascript", "text/html", "text/css", "application/x-sh", "application/sql",
]);

/** 取不含参数的 media type（text/plain; charset=utf-8 → text/plain） */
export function mediaTypeOf(contentType: string): string {
  return (contentType || "").split(";")[0]!.trim().toLowerCase();
}

export function fileKindOf(contentType: string): FileKind {
  const media = mediaTypeOf(contentType);
  if (media.startsWith("image/")) return "image";
  if (media.startsWith("video/")) return "video";
  if (media.startsWith("audio/")) return "audio";
  if (media === "application/pdf") return "pdf";
  if (ARCHIVE_TYPES.has(media)) return "archive";
  if (SHEET_TYPES.has(media)) return "sheet";
  if (CODE_TYPES.has(media)) return "code";
  if (media.startsWith("text/")) return "text";
  return "other";
}

export function FileKindIcon({ contentType, className }: { contentType: string; className?: string }) {
  const kind = fileKindOf(contentType);
  const Icon = KIND_ICONS[kind];
  return <Icon className={cn("size-4 shrink-0", KIND_TONES[kind], className)} />;
}

export function FolderIcon({ className }: { className?: string }) {
  return <Folder className={cn("size-4 shrink-0 text-amber-500", className)} />;
}

/**
 * 矢量图交给浏览器内联渲染，位图才走服务端缩略图。
 * SVG 是 XML 描述而不是位图，Go 的 image.Decode 解不了它。
 */
export function canThumbnail(obj: StorageObject): boolean {
  const media = mediaTypeOf(obj.contentType);
  return media.startsWith("image/") && media !== "image/svg+xml" && obj.size > 0;
}

/* ================================================================== */
/*  缩略图                                                             */
/* ================================================================== */

/*
 * 缩略图接口要 Authorization 头，<img src> 带不了，因此走 fetch → Blob URL。
 * 与 ui/avatar.tsx 同一套做法：进程内 LRU 缓存复用同一张图，
 * 翻页来回切时不会反复解码。
 */
const THUMB_CACHE = new Map<string, string>();
const THUMB_PENDING = new Map<string, Promise<string | null>>();
const THUMB_MAX_ENTRIES = 300;

function evictThumbLRU() {
  while (THUMB_CACHE.size > THUMB_MAX_ENTRIES) {
    const oldest = THUMB_CACHE.keys().next().value;
    if (!oldest) break;
    const url = THUMB_CACHE.get(oldest);
    if (url) URL.revokeObjectURL(url);
    THUMB_CACHE.delete(oldest);
  }
}

/** 对象被删除 / 重传后调用，避免继续显示已经不对的旧图 */
export function evictThumbnailCache(objectId?: number) {
  const prefix = objectId ? `${objectId}:` : "";
  for (const [key, url] of THUMB_CACHE) {
    if (!prefix || key.startsWith(prefix)) {
      URL.revokeObjectURL(url);
      THUMB_CACHE.delete(key);
    }
  }
}

function loadThumbnail(cacheKey: string, url: string, token: string): Promise<string | null> {
  const hit = THUMB_CACHE.get(cacheKey);
  if (hit) return Promise.resolve(hit);
  const pending = THUMB_PENDING.get(cacheKey);
  if (pending) return pending;

  const task = fetch(url, { headers: { Authorization: `Bearer ${token}`, "X-Admin-Token": token } })
    .then((response) => (response.ok ? response.blob() : null))
    .then((blob) => {
      THUMB_PENDING.delete(cacheKey);
      if (!blob) return null;
      const objectUrl = URL.createObjectURL(blob);
      THUMB_CACHE.set(cacheKey, objectUrl);
      evictThumbLRU();
      return objectUrl;
    })
    .catch(() => {
      THUMB_PENDING.delete(cacheKey);
      return null;
    });

  THUMB_PENDING.set(cacheKey, task);
  return task;
}

type ThumbnailState = "idle" | "loading" | "ready" | "failed";

/**
 * 缩略图。拿不到时静默退回类型图标 —— 「这个文件坏了」和
 * 「这个格式没有预览」在文件列表里是同一种无关紧要，不值得报错打断。
 */
export function FileThumbnail({
  object, size = 192, className, iconClassName,
}: {
  object: StorageObject;
  size?: number;
  className?: string;
  iconClassName?: string;
}) {
  const token = useAdminToken();
  const eligible = canThumbnail(object);
  const cacheKey = `${object.id}:${object.etag || object.size}:${size}`;

  /*
   * 缓存命中在**渲染期派生**，不用 effect 往 state 里回写 —— 后者既触发
   * 级联渲染，也过不了 react-hooks/set-state-in-effect（与 ui/avatar.tsx
   * 同一个形状）。effect 只负责发起请求，setState 落在异步回调里。
   */
  const syncHit = React.useMemo(() => (eligible ? THUMB_CACHE.get(cacheKey) ?? null : null), [cacheKey, eligible]);
  const [fetched, setFetched] = React.useState<{ key: string; url: string | null } | null>(null);

  React.useEffect(() => {
    if (!eligible || !token || THUMB_CACHE.has(cacheKey)) return;
    let alive = true;
    loadThumbnail(cacheKey, storageThumbnailUrl(object.id, size), token).then((url) => {
      if (alive) setFetched({ key: cacheKey, url });
    });
    return () => { alive = false; };
  }, [cacheKey, eligible, object.id, size, token]);

  const resolved = fetched?.key === cacheKey ? fetched.url : syncHit;
  const state: ThumbnailState = !eligible
    ? "idle"
    : resolved
      ? "ready"
      : fetched?.key === cacheKey
        ? "failed"
        : "loading";
  const src = resolved;

  // SVG 走代理直链由浏览器渲染，这里只给图标占位
  if (!eligible || state === "failed") {
    return (
      <div className={cn("flex items-center justify-center bg-muted/30", className)}>
        <FileKindIcon contentType={object.contentType} className={cn("size-8", iconClassName)} />
      </div>
    );
  }
  if (state === "loading" || !src) {
    return (
      <div className={cn("flex items-center justify-center bg-muted/30", className)}>
        <Loader2 className="size-4 animate-spin text-muted-foreground/50" />
      </div>
    );
  }
  return (
    <div className={cn("flex items-center justify-center overflow-hidden bg-muted/30", className)}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={src} alt={object.fileName || object.objectKey} className="size-full object-cover" loading="lazy" />
    </div>
  );
}

/* ================================================================== */
/*  展示原语                                                           */
/* ================================================================== */

const STATUS_META: Record<string, { label: string; variant: "success" | "danger" | "warning" | "outline" }> = {
  active: { label: "活跃", variant: "success" },
  deleted: { label: "回收站", variant: "danger" },
  pending_review: { label: "待审核", variant: "warning" },
};

export function ObjectStatusBadge({ status, className }: { status: string; className?: string }) {
  const meta = STATUS_META[status] ?? { label: status, variant: "outline" as const };
  return <Badge variant={meta.variant} className={cn("text-[9px]", className)}>{meta.label}</Badge>;
}

/**
 * 上传时声明的类型与魔数判定不符 —— 后端已按魔数改判并把声明值留在
 * metadata 里。这不是错误，但值得管理员看一眼：改扩展名绕过类型限制
 * 的尝试就长这样。
 */
export function MasqueradeBadge({ metadata }: { metadata: Record<string, unknown> }) {
  const declared = metadata?.["aegis:declaredContentType"];
  if (typeof declared !== "string" || !declared) return null;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant="warning" className="gap-1 text-[9px]">
          <ShieldAlert className="size-2.5" />
          类型不符
        </Badge>
      </TooltipTrigger>
      <TooltipContent>上传时声明为 {declared}，实际内容按魔数判定为另一类</TooltipContent>
    </Tooltip>
  );
}

/* ================================================================== */
/*  格式化                                                             */
/* ================================================================== */

const SIZE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), SIZE_UNITS.length - 1);
  const value = bytes / 1024 ** exponent;
  return `${value.toFixed(exponent === 0 ? 0 : value >= 100 ? 0 : 1)} ${SIZE_UNITS[exponent]}`;
}

/** 解析「10MB」「512k」这类输入；解析不出来返回 undefined，由调用方当作没填 */
export function parseSizeInput(raw: string): number | undefined {
  const matched = /^\s*([\d.]+)\s*([kmgt]?)i?b?\s*$/i.exec(raw);
  if (!matched) return undefined;
  const value = Number.parseFloat(matched[1]!);
  if (!Number.isFinite(value)) return undefined;
  const scale = { "": 1, k: 1024, m: 1024 ** 2, g: 1024 ** 3, t: 1024 ** 4 }[matched[2]!.toLowerCase()] ?? 1;
  return Math.round(value * scale);
}

export function formatDateTime(value?: string | null): string {
  if (!value) return "--";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN", { hour12: false });
}

export function formatDate(value?: string | null): string {
  if (!value) return "--";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleDateString("zh-CN");
}

export function baseName(objectKey: string): string {
  const parts = objectKey.split("/");
  return parts[parts.length - 1] || objectKey;
}

/** 目录路径 → 面包屑分段 */
export function folderSegments(folder: string): Array<{ name: string; path: string }> {
  if (!folder) return [];
  const parts = folder.split("/").filter(Boolean);
  return parts.map((name, index) => ({ name, path: parts.slice(0, index + 1).join("/") }));
}
