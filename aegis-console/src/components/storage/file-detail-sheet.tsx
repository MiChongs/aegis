"use client";

import * as React from "react";
import {
  Copy, Download, ExternalLink, Link2, Loader2, RotateCcw, Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { LightboxOverlay } from "@/components/ui/image-lightbox";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import {
  usePermanentDeleteObjectMutation, useRestoreObjectMutation,
  useSoftDeleteObjectMutation, useStorageObjectLinkMutation,
} from "@/lib/admin-hooks";
import type { StorageObject } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  FileKindIcon, KIND_LABELS, MasqueradeBadge, ObjectStatusBadge,
  baseName, evictThumbnailCache, fileKindOf, formatBytes, formatDateTime, mediaTypeOf,
} from "./file-shared";

/* ================================================================== */
/*  文件详情抽屉                                                       */
/* ================================================================== */

type Props = {
  object: StorageObject | null;
  onOpenChange: (open: boolean) => void;
  /** 删除 / 恢复后通知列表清掉选中态 */
  onMutated?: (objectId: number) => void;
  canPurge: boolean;
  configLabel?: string;
};

export function FileDetailSheet({ object, onOpenChange, onMutated, canPurge, configLabel }: Props) {
  return (
    <Sheet open={Boolean(object)} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full gap-0 p-0 sm:max-w-xl">
        {object ? (
          <FileDetailBody
            key={object.id}
            object={object}
            onOpenChange={onOpenChange}
            onMutated={onMutated}
            canPurge={canPurge}
            configLabel={configLabel}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

/*
 * 拆出 Body 并用 key={object.id} 重挂载：预览地址、灯箱开合都是这个对象独有的
 * 状态，换一个文件必须全部重置。靠 useEffect 同步既触发级联渲染，
 * 也过不了 react-hooks/set-state-in-effect（与门户凭据、工单详情同一条约束）。
 */
function FileDetailBody({ object, onOpenChange, onMutated, canPurge, configLabel }: Props & { object: StorageObject }) {
  const linkMutation = useStorageObjectLinkMutation();
  const softDelete = useSoftDeleteObjectMutation();
  const restore = useRestoreObjectMutation();
  const permanentDelete = usePermanentDeleteObjectMutation();

  const [previewUrl, setPreviewUrl] = React.useState<string | null>(null);
  const [lightboxOpen, setLightboxOpen] = React.useState(false);

  const kind = fileKindOf(object.contentType);
  const media = mediaTypeOf(object.contentType);
  const displayName = object.fileName || baseName(object.objectKey);
  const inlineRenderable = kind === "image" || kind === "video" || kind === "audio" || kind === "pdf";

  // 预览地址是**限时票据**，不能预取一堆挂着。进抽屉才签一张，
  // 关掉即作废（服务端 TTL 到点自动清）。
  React.useEffect(() => {
    if (!inlineRenderable) return;
    let alive = true;
    linkMutation
      .mutateAsync({ objectId: object.id, download: false, expiresIn: 900 })
      .then((link) => { if (alive) setPreviewUrl(link.url); })
      .catch(() => { if (alive) setPreviewUrl(null); });
    return () => { alive = false; };
    // linkMutation 每次渲染都是新引用，挂进依赖会无限重签票据
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [object.id, inlineRenderable]);

  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`${label}已复制`);
    } catch {
      toast.error("复制失败，请手动选择");
    }
  };

  const download = async () => {
    try {
      const link = await linkMutation.mutateAsync({ objectId: object.id, download: true, expiresIn: 600 });
      // 票据地址免登录且限时，直接开新窗即可；用 <a download> 会被跨域忽略
      window.open(link.url, "_blank", "noopener,noreferrer");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "生成下载地址失败");
    }
  };

  const runMutation = async (
    action: () => Promise<unknown>, successText: string, failText: string, close: boolean,
  ) => {
    try {
      await action();
      evictThumbnailCache(object.id);
      toast.success(successText);
      onMutated?.(object.id);
      if (close) onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : failText);
    }
  };

  const busy = softDelete.isPending || restore.isPending || permanentDelete.isPending;

  return (
    <>
      <SheetHeader className="border-b px-5 py-4">
        <SheetTitle className="flex items-center gap-2 text-base">
          <FileKindIcon contentType={object.contentType} className="size-4" />
          <span className="min-w-0 flex-1 truncate">{displayName}</span>
        </SheetTitle>
        <SheetDescription className="flex flex-wrap items-center gap-1.5">
          <ObjectStatusBadge status={object.status} />
          <Badge variant="outline" className="text-[9px]">{KIND_LABELS[kind]}</Badge>
          <Badge variant="outline" className="text-[9px]">{formatBytes(object.size)}</Badge>
          <MasqueradeBadge metadata={object.metadata} />
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {/* 预览 */}
        <FilePreview
          object={object}
          media={media}
          kind={kind}
          previewUrl={previewUrl}
          pending={inlineRenderable && !previewUrl && linkMutation.isPending}
          onExpand={() => setLightboxOpen(true)}
        />

        {/* 元数据 */}
        <dl className="grid gap-x-4 gap-y-2 text-xs sm:grid-cols-[auto_1fr]">
          <Fact label="对象键" value={object.objectKey} mono copyable onCopy={copy} />
          <Fact label="内容类型" value={object.contentType || "--"} mono />
          <Fact label="大小" value={`${formatBytes(object.size)}（${object.size.toLocaleString("zh-CN")} 字节）`} />
          <Fact label="ETag" value={object.etag || "--"} mono />
          <Fact label="存储配置" value={configLabel ? `${configLabel}（#${object.configId}）` : `#${object.configId}`} />
          <Fact label="所属应用" value={object.appId ? `#${object.appId}` : "平台级"} />
          <Fact
            label="上传者"
            value={`${object.uploaderType === "admin" ? "管理员" : "用户"}${object.uploadedBy ? ` #${object.uploadedBy}` : ""}`}
          />
          <Fact label="上传时间" value={formatDateTime(object.createdAt)} />
          {object.deletedAt ? <Fact label="删除时间" value={formatDateTime(object.deletedAt)} /> : null}
        </dl>

        {Object.keys(object.metadata || {}).length > 0 ? (
          <div className="space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">自定义元数据</span>
            {/* 刻意不用 JsonViewer：那是 Monaco 驱动的，为几行键值对拉起整个编辑器
                会把 Monaco 引到这个页面上（见 aegis-console/CLAUDE.md 的加载约束） */}
            <pre className="max-h-56 overflow-auto rounded-lg border bg-muted/20 p-2.5 font-mono text-[11px] leading-relaxed">
              {JSON.stringify(object.metadata, null, 2)}
            </pre>
          </div>
        ) : null}
      </div>

      {/* 操作条 */}
      <div className="flex flex-wrap gap-1.5 border-t px-5 py-3">
        <Button variant="outline" size="sm" className="gap-1" onClick={() => copy(object.objectKey, "对象键")}>
          <Copy className="size-3.5" />复制路径
        </Button>
        <Button
          variant="outline" size="sm" className="gap-1"
          disabled={linkMutation.isPending}
          onClick={async () => {
            try {
              const link = await linkMutation.mutateAsync({ objectId: object.id, download: false, expiresIn: 3600 });
              await copy(link.url, "临时访问地址");
            } catch (error) {
              toast.error(error instanceof ApiError ? error.message : "签发地址失败");
            }
          }}
        >
          <Link2 className="size-3.5" />复制临时链接
        </Button>
        <Button variant="outline" size="sm" className="gap-1" disabled={linkMutation.isPending} onClick={download}>
          {linkMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
          下载
        </Button>

        <div className="flex-1" />

        {object.status === "deleted" ? (
          <>
            <Button
              variant="outline" size="sm" className="gap-1" disabled={busy}
              onClick={() => runMutation(() => restore.mutateAsync(object.id), "已恢复", "恢复失败", false)}
            >
              <RotateCcw className="size-3.5" />恢复
            </Button>
            {canPurge ? (
              <Button
                variant="destructive" size="sm" className="gap-1" disabled={busy}
                onClick={() => runMutation(() => permanentDelete.mutateAsync(object.id), "已永久删除", "删除失败", true)}
              >
                <Trash2 className="size-3.5" />永久删除
              </Button>
            ) : null}
          </>
        ) : (
          <Button
            variant="destructive" size="sm" className="gap-1" disabled={busy}
            onClick={() => runMutation(() => softDelete.mutateAsync(object.id), "已移入回收站", "删除失败", true)}
          >
            <Trash2 className="size-3.5" />移入回收站
          </Button>
        )}
      </div>

      {previewUrl && kind === "image" ? (
        <LightboxOverlay
          open={lightboxOpen}
          onClose={() => setLightboxOpen(false)}
          slides={[{ src: previewUrl, alt: displayName, description: object.objectKey }]}
        />
      ) : null}
    </>
  );
}

/* ------------------------------------------------------------------ */

function FilePreview({
  object, media, kind, previewUrl, pending, onExpand,
}: {
  object: StorageObject;
  media: string;
  kind: string;
  previewUrl: string | null;
  pending: boolean;
  onExpand: () => void;
}) {
  const frame = "flex min-h-40 items-center justify-center overflow-hidden rounded-xl border bg-muted/20";

  if (pending) {
    return <div className={frame}><Loader2 className="size-5 animate-spin text-muted-foreground" /></div>;
  }
  if (!previewUrl) {
    return (
      <div className={cn(frame, "flex-col gap-2 text-muted-foreground")}>
        <FileKindIcon contentType={object.contentType} className="size-10" />
        <span className="text-[10px]">{object.contentType || "未知类型"} · 无法内联预览</span>
      </div>
    );
  }
  if (kind === "image") {
    return (
      <button type="button" onClick={onExpand} className={cn(frame, "group w-full cursor-zoom-in p-2")}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={previewUrl}
          alt={object.fileName || object.objectKey}
          className="max-h-64 w-auto max-w-full rounded-lg object-contain transition-transform group-hover:scale-[1.01]"
        />
      </button>
    );
  }
  if (kind === "video") {
    return <video src={previewUrl} controls preload="metadata" className={cn(frame, "w-full")} />;
  }
  if (kind === "audio") {
    return (
      <div className={cn(frame, "px-4 py-6")}>
        <audio src={previewUrl} controls preload="metadata" className="w-full" />
      </div>
    );
  }
  if (media === "application/pdf") {
    return (
      <div className="space-y-2">
        <object data={previewUrl} type="application/pdf" className={cn(frame, "h-72 w-full")}>
          <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
            <FileKindIcon contentType={object.contentType} className="size-10" />
            <span className="text-[10px]">浏览器未内置 PDF 阅读器</span>
          </div>
        </object>
        <Button asChild variant="ghost" size="sm" className="w-full gap-1">
          <a href={previewUrl} target="_blank" rel="noreferrer"><ExternalLink className="size-3.5" />新窗口打开</a>
        </Button>
      </div>
    );
  }
  return (
    <div className={cn(frame, "flex-col gap-2 text-muted-foreground")}>
      <FileKindIcon contentType={object.contentType} className="size-10" />
      <span className="text-[10px]">{object.contentType}</span>
    </div>
  );
}

function Fact({
  label, value, mono, copyable, onCopy,
}: {
  label: string;
  value: string;
  mono?: boolean;
  copyable?: boolean;
  onCopy?: (value: string, label: string) => void;
}) {
  return (
    <>
      <dt className="text-muted-foreground sm:whitespace-nowrap">{label}</dt>
      <dd className={cn("flex min-w-0 items-start gap-1 break-all", mono && "font-mono text-[11px]")}>
        <span className="min-w-0 flex-1">{value}</span>
        {copyable ? (
          <button type="button" title={`复制${label}`} className="shrink-0 text-muted-foreground hover:text-foreground" onClick={() => onCopy?.(value, label)}>
            <Copy className="size-3" />
          </button>
        ) : null}
      </dd>
    </>
  );
}
