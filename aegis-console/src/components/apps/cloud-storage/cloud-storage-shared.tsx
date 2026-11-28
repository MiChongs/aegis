"use client";

import { Layers } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type {
  CloudCollectionKind,
  CloudItemEncoding,
  CloudPlan,
  CloudRevisionSource,
  CloudStorageTarget,
  CloudUserState
} from "@/lib/api/cloud-storage";
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
  pool: "存储池",
  r2: "Cloudflare R2",
  s3: "Amazon S3",
  minio: "MinIO",
  rustfs: "RustFS",
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
  if (target.provider === "pool") return `${target.configName} · 存储池`;
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

// ── 云服务：档位与同步集合 ──

/**
 * 内容领域与集合目录，与后端 `cloudstorage.SyncDomains` / `SyncCollections`（sync.go）同一份。
 * 管理端配置接口不下发目录，档位编辑器的集合多选、用户详情的集合名都读这里；
 * 后端加集合时这里跟着加。目录外的集合也能同步（名字合法即可），显示为原始键。
 */
export const CLOUD_SYNC_DOMAINS: Array<{ key: string; name: string }> = [
  { key: "music", name: "听歌" },
  { key: "video", name: "影视" },
  { key: "anime", name: "番剧" },
  { key: "drama", name: "短剧" },
  { key: "novel", name: "小说" },
  { key: "audiobook", name: "听书" },
  { key: "comic", name: "漫画" },
  { key: "app", name: "应用" }
];

export const CLOUD_SYNC_COLLECTIONS: Array<{ key: string; name: string; domain: string; kind: CloudCollectionKind }> = [
  { key: "music.history", name: "听歌记录", domain: "music", kind: "history" },
  { key: "music.favorites", name: "收藏的歌", domain: "music", kind: "library" },
  { key: "music.playlists", name: "歌单", domain: "music", kind: "library" },
  { key: "music.playlist-tracks", name: "歌单里的歌", domain: "music", kind: "library" },
  { key: "video.history", name: "影视观看记录", domain: "video", kind: "history" },
  { key: "video.favorites", name: "影视收藏", domain: "video", kind: "library" },
  { key: "anime.history", name: "番剧观看记录", domain: "anime", kind: "history" },
  { key: "anime.follows", name: "追番", domain: "anime", kind: "library" },
  { key: "drama.history", name: "短剧观看记录", domain: "drama", kind: "history" },
  { key: "drama.follows", name: "追剧", domain: "drama", kind: "library" },
  { key: "novel.history", name: "阅读记录", domain: "novel", kind: "history" },
  { key: "novel.bookshelf", name: "书架", domain: "novel", kind: "library" },
  { key: "audiobook.history", name: "收听记录", domain: "audiobook", kind: "history" },
  { key: "audiobook.shelf", name: "听书书架", domain: "audiobook", kind: "library" },
  { key: "comic.history", name: "漫画阅读记录", domain: "comic", kind: "history" },
  { key: "comic.shelf", name: "漫画书架", domain: "comic", kind: "library" },
  { key: "app.favorites", name: "收藏的工具", domain: "app", kind: "library" },
  { key: "app.settings", name: "偏好设置", domain: "app", kind: "state" }
];

/** 与后端 cloudstorage.ValidCollection 同一条规则。 */
export const COLLECTION_RE = /^[a-z][a-z0-9]{0,23}\.[a-z][a-z0-9_-]{0,39}$/;

export function collectionName(key: string) {
  return CLOUD_SYNC_COLLECTIONS.find((item) => item.key === key)?.name ?? key;
}

export function domainName(key: string) {
  return CLOUD_SYNC_DOMAINS.find((item) => item.key === key)?.name ?? key;
}

export const COLLECTION_KIND_LABELS: Record<string, string> = {
  history: "历史",
  library: "收藏",
  state: "状态"
};

/** 档位能否同步这个集合（空 = 全部）。 */
export function planAllows(plan: Pick<CloudPlan, "collections"> | null | undefined, collection: string) {
  const list = plan?.collections ?? [];
  return list.length === 0 || list.includes(collection);
}

/** 用户的总占用：条目与同步记录共用一份配额，管理端账目里 usedBytes 只是条目。 */
export function cloudUsedBytes(state: Pick<CloudUserState, "usedBytes" | "syncBytes">) {
  return Number(state.usedBytes ?? 0) + Number(state.syncBytes ?? 0);
}

/** 用户行上的档位名。 */
export function PlanBadge({ name, className }: { name?: string | null; className?: string }) {
  if (!name) return null;
  return (
    <Badge variant="secondary" size="sm" className={cn("gap-1 font-normal", className)}>
      <Layers className="size-3" />
      {name}
    </Badge>
  );
}
