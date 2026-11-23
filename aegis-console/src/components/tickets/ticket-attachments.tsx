"use client";

import { useMemo } from "react";
import { Download, FileText } from "lucide-react";
import { ImageGrid, type LightboxSlide } from "@/components/ui/image-lightbox";
import type { TicketAttachment } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { attachmentUrl, formatBytes, isImageAttachment } from "./ticket-shared";

// 工单 / 反馈附件展示：图片排成缩略图网格，点开进入查看器（原位展开、缩放、旋转、下载）；
// 其余文件显示为带大小的下载条。
//
// 图片一律原生 <img>：downloadUrl 指向同源 /api/storage/proxy/*，
// 交给 Next 图像优化器回源会被私网 SSRF 防护拦下（与 Banner 管理同一原因）。
// 下载地址是 30 分钟有效的限时链接，停留过久后需重新打开工单刷新。

type Props = {
  attachments: TicketAttachment[];
  /** 缩略图边长，会话气泡内用小号，资料栏汇总用大号 */
  size?: "sm" | "md";
  /** 最多显示几格，其余收进最后一格的「+N」 */
  max?: number;
  className?: string;
};

export function TicketAttachmentList({ attachments, size = "sm", max, className }: Props) {
  const { images, files } = useMemo(() => {
    const images: LightboxSlide[] = [];
    const files: TicketAttachment[] = [];
    for (const file of attachments) {
      if (isImageAttachment(file) && file.downloadUrl) {
        images.push({
          src: attachmentUrl(file),
          alt: file.fileName,
          fileName: file.fileName,
          size: file.sizeBytes,
          description: file.fileName
        });
      } else files.push(file);
    }
    return { images, files };
  }, [attachments]);

  if (attachments.length === 0) return null;

  return (
    <div className={cn("space-y-2", className)}>
      {images.length > 0 ? <ImageGrid images={images} max={max} thumbClassName={size === "sm" ? "size-20" : "size-24"} /> : null}
      {files.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {files.map((file) => (
            <AttachmentFileChip key={file.id} file={file} />
          ))}
        </div>
      ) : null}
    </div>
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
