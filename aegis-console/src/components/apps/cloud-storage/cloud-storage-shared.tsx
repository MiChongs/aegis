"use client";

import { Badge } from "@/components/ui/badge";
import type { CloudItemEncoding, CloudRevisionSource, CloudStorageTarget } from "@/lib/api/cloud-storage";
import { cn } from "@/lib/utils";

/**
 * 用户云存储区块与用户详情「云存储」页签共用的展示原语。
 *
 * 字节数一律按 1024 进位并写 KB / MB / GB：后端的配额与上限都是 2 的幂
 * （20 MiB、1 MiB），按 1000 进位会把「20 MB 配额」显示成 20.97 MB，
 * 管理员会以为配置被改过。
 */

export const MiB = 1024 * 1024;

export function formatBytes(value?: number | null) {
  const bytes = Number(value ?? 0);
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = bytes;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : size >= 100 ? 0 : size >= 10 ? 1 : 2;
  return `${size.toFixed(digits).replace(/\.0+$|(\.\d*[1-9])0+$/, "$1")} ${units[unit]}`;
}

/** 字节 → 表单里的 MB 字符串（去掉无意义的小数）。 */
export function bytesToMBInput(bytes?: number | null) {
  const value = Number(bytes ?? 0) / MiB;
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(3)));
}

/** 表单里的 MB 字符串 → 字节；非法输入返回 NaN。 */
export function mbInputToBytes(value: string) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return Number.NaN;
  return Math.round(parsed * MiB);
}

export function usagePercent(used?: number | null, quota?: number | null) {
  if (!quota || quota <= 0) return 0;
  return Math.min(100, Math.round((Number(used ?? 0) / quota) * 1000) / 10);
}

/**
 * 配额条。三档颜色只看比例：90% 以上是「下一次写入就可能被拒」，
 * 70% 以上值得留意，其余不打扰。
 */
export function QuotaMeter({
  used,
  quota,
  className,
  compact = false
}: {
  used: number;
  quota: number;
  className?: string;
  compact?: boolean;
}) {
  const percent = usagePercent(used, quota);
  const tone = percent >= 90 ? "bg-red-500" : percent >= 70 ? "bg-amber-500" : "bg-emerald-500";
  return (
    <div className={cn("space-y-1", className)}>
      <div className={cn("relative w-full overflow-hidden rounded-full bg-muted", compact ? "h-1.5" : "h-2")}>
        <div className={cn("h-full rounded-full transition-all", tone)} style={{ width: `${Math.max(percent, used > 0 ? 1 : 0)}%` }} />
      </div>
      {!compact ? (
        <div className="flex items-center justify-between text-[11px] text-muted-foreground tabular-nums">
          <span>
            {formatBytes(used)} / {formatBytes(quota)}
          </span>
          <span>{percent}%</span>
        </div>
      ) : null}
    </div>
  );
}

export const PROVIDER_LABELS: Record<string, string> = {
  s3: "Amazon S3",
  minio: "MinIO",
  aliyun_oss: "阿里云 OSS",
  tencent_cos: "腾讯云 COS",
  qiniu_kodo: "七牛云 Kodo",
  webdav: "WebDAV",
  onedrive: "OneDrive",
  dropbox: "Dropbox",
  google_drive: "Google Drive",
  azure_blob: "Azure Blob",
  local: "本地存储"
};

export function providerLabel(provider?: string | null) {
  return provider ? PROVIDER_LABELS[provider] ?? provider : "—";
}

export function describeTarget(target?: CloudStorageTarget | null) {
  if (!target || target.error) return "未找到可用存储";
  const scope = target.scope === "global" ? "平台级" : "应用级";
  return `${target.configName} · ${providerLabel(target.provider)} · ${scope}`;
}

export const ENCODING_LABELS: Record<CloudItemEncoding, string> = {
  json: "JSON",
  text: "文本",
  base64: "二进制"
};

export const SOURCE_LABELS: Record<CloudRevisionSource, string> = {
  write: "写入",
  upload: "上传",
  rollback: "回滚",
  admin: "管理端"
};

export function EncodingBadge({ encoding }: { encoding: CloudItemEncoding }) {
  return (
    <Badge variant="outline" size="sm" className="font-normal">
      {ENCODING_LABELS[encoding] ?? encoding}
    </Badge>
  );
}

export function formatDateTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() < 2000) return "—";
  return date.toLocaleString("zh-CN", { hour12: false });
}

/** 命名空间的展示：有目录名时「名称 + 键」，否则只显示键。 */
export function NamespaceLabel({ namespace, name }: { namespace: string; name?: string }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      {name && name !== namespace ? <span className="truncate font-medium">{name}</span> : null}
      <code className="truncate font-mono text-[11px] text-muted-foreground">{namespace}</code>
    </span>
  );
}
