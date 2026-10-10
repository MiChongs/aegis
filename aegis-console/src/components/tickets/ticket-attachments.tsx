"use client";

import { useMemo, useState } from "react";
import { Download, FileText, ImageOff } from "lucide-react";
import { LightboxOverlay } from "@/components/ui/image-lightbox";
import type { TicketAttachment } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { attachmentUrl, formatBytes, isImageAttachment } from "./ticket-shared";

// 工单 / 反馈附件展示：图片排成缩略图网格，点击进入灯箱；其余文件显示为带大小的下载条。
//
// 图片一律原生 <img>：downloadUrl 指向同源 /api/storage/proxy/*，
// 交给 Next 图像优化器回源会被私网 SSRF 防护拦下（与 Banner 管理同一原因）。
// 下载地址是 30 分钟有效的限时链接，抽屉停留过久后需重新打开工单刷新。

type Props = {
  attachments: TicketAttachment[];
  /** 缩略图边长，会话气泡内用小号，属性页汇总用大号 */
  size?: "sm" | "md";
  className?: string;
};

export function TicketAttachmentList({ attachments, size = "sm", className }: Props) {
  const { images, files } = useMemo(() => {
    const images: TicketAttachment[] = [];
    const files: TicketAttachment[] = [];
    for (const file of attachments) {
      if (isImageAttachment(file) && file.downloadUrl) images.push(file);
      else files.push(file);
    }
    return { images, files };
  }, [attachments]);

  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const slides = useMemo(
    () => images.map((file) => ({ src: attachmentUrl(file), alt: file.fileName, description: file.fileName })),
    [images]
  );

  if (attachments.length === 0) return null;

  return (
    <div className={cn("space-y-2", className)}>
      {images.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {images.map((file, index) => (
            <AttachmentThumb
              key={file.id}
              file={file}
              size={size}
              onOpen={() => setLightboxIndex(index)}
            />
          ))}
        </div>
      ) : null}

      {files.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {files.map((file) => (
            <AttachmentFileChip key={file.id} file={file} />
          ))}
        </div>
      ) : null}

      {slides.length > 0 ? (
        <LightboxOverlay
          open={lightboxIndex !== null}
          index={lightboxIndex ?? 0}
          onClose={() => setLightboxIndex(null)}
          slides={slides}
        />
      ) : null}
    </div>
  );
}

function AttachmentThumb({
  file,
  size,
  onOpen
}: {
  file: TicketAttachment;
  size: "sm" | "md";
  onOpen: () => void;
}) {
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");

  return (
    <button
      type="button"
      onClick={onOpen}
      title={`${file.fileName}（${formatBytes(file.sizeBytes)}）`}
      className={cn(
        "relative overflow-hidden rounded-lg border bg-muted transition-colors hover:border-primary/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        size === "sm" ? "size-20" : "size-24"
      )}
    >
      {state === "loading" ? <span className="absolute inset-0 animate-pulse bg-muted" /> : null}
      {state === "error" ? (
        <span className="absolute inset-0 flex flex-col items-center justify-center gap-1 text-muted-foreground">
          <ImageOff className="size-4" />
          <span className="text-[10px]">加载失败</span>
        </span>
      ) : null}
      {/* eslint-disable-next-line @next/next/no-img-element -- 同源存储代理地址，见文件头注释 */}
      <img
        src={attachmentUrl(file)}
        alt={file.fileName}
        loading="lazy"
        decoding="async"
        onLoad={() => setState("ready")}
        onError={() => setState("error")}
        className={cn("size-full object-cover", state !== "ready" && "opacity-0")}
      />
    </button>
  );
}

export function AttachmentFileChip({ file }: { file: TicketAttachment }) {
  const url = attachmentUrl(file);
  const content = (
    <>
      <FileText className="size-3.5 shrink-0" />
      <span className="max-w-48 truncate text-foreground">{file.fileName}</span>
      <span className="shrink-0 text-[10px]">{formatBytes(file.sizeBytes)}</span>
      {url ? <Download className="size-3 shrink-0" /> : null}
    </>
  );
  const chipClass =
    "inline-flex items-center gap-1.5 rounded-lg border bg-background px-2 py-1 text-xs text-muted-foreground";

  if (!url) {
    return <span className={chipClass}>{content}</span>;
  }
  // 限时链接可能与控制台不同源，<a download> 会被浏览器忽略，统一新窗口打开
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      download={file.fileName}
      className={cn(chipClass, "hover:bg-accent")}
    >
      {content}
    </a>
  );
}
