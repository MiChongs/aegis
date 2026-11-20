"use client";

import { useState } from "react";
import { useDropzone } from "react-dropzone";
import { Film, ImageIcon, Images, Loader2, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Segmented } from "@/components/users/detail/user-assets-adjust";
import {
  FilterBar,
  FilterSelect,
  Pager,
  SearchField,
  Section,
  StateBlock,
  errorMessage,
  useDebouncedValue
} from "@/components/users/detail/kit";
import type { CosmeticAsset, CosmeticCatalog } from "@/lib/api/cosmetics";
import {
  useCosmeticAssetsQuery,
  useDeleteCosmeticAssetMutation,
  useUploadCosmeticAssetMutation
} from "@/lib/cosmetic-hooks";
import { formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { formatBytes, formatCount } from "./cosmetic-shared";
import { resolveAssetUrl } from "./renderer";

const PAGE_SIZE = 48;
const ACCEPT_FALLBACK = ["image/png", "image/webp", "image/gif", "image/jpeg", "image/svg+xml"];

/**
 * 上传素材：拖放或点选，可多选；同内容的文件后端返回已有那条（按 sha256 去重），这里据此提示「已存在」。
 * 设计器的素材面板复用这一块。
 */
export function useAssetUploader(appKey: string, catalog?: CosmeticCatalog | null) {
  const upload = useUploadCosmeticAssetMutation(appKey);
  const [busy, setBusy] = useState(0);
  const maxBytes = catalog?.vocabulary?.limits?.maxAssetBytes ?? 8 << 20;

  const uploadFiles = async (files: File[], kind: "image" | "sprite") => {
    const uploaded: CosmeticAsset[] = [];
    for (const file of files) {
      if (file.size > maxBytes) {
        toast.error(`「${file.name}」超过 ${formatBytes(maxBytes)}`);
        continue;
      }
      setBusy((value) => value + 1);
      const started = Date.now();
      try {
        const asset = await upload.mutateAsync({ file, kind, name: file.name.replace(/\.[^.]+$/, "") });
        uploaded.push(asset);
        const existing = new Date(asset.createdAt).getTime() < started - 5_000;
        if (existing) toast.info(`「${file.name}」与已有素材「${asset.name}」内容相同，已直接使用`);
      } catch (error) {
        toast.error(`「${file.name}」上传失败：${errorMessage(error)}`);
      } finally {
        setBusy((value) => value - 1);
      }
    }
    if (uploaded.length > 1) toast.success(`已上传 ${uploaded.length} 个素材`);
    else if (uploaded.length === 1) toast.success("上传成功");
    return uploaded;
  };

  return { uploadFiles, busy: busy > 0, maxBytes };
}

export function AssetDropzone({
  appKey,
  catalog,
  kind,
  compact,
  disabled,
  onUploaded
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  kind: "image" | "sprite";
  compact?: boolean;
  disabled?: boolean;
  onUploaded?: (assets: CosmeticAsset[]) => void;
}) {
  const { uploadFiles, busy, maxBytes } = useAssetUploader(appKey, catalog);
  const types = catalog?.assetTypes?.length ? catalog.assetTypes : ACCEPT_FALLBACK;
  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    accept: Object.fromEntries(types.map((type) => [type, []])),
    multiple: true,
    disabled: disabled || busy,
    onDrop: async (accepted, rejected) => {
      for (const reject of rejected) toast.error(`「${reject.file.name}」不是支持的图片格式`);
      if (accepted.length) {
        const assets = await uploadFiles(accepted, kind);
        if (assets.length) onUploaded?.(assets);
      }
    }
  });
  return (
    <div
      {...getRootProps()}
      className={cn(
        "flex cursor-pointer flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed text-center transition-colors",
        compact ? "px-3 py-4" : "px-6 py-8",
        isDragActive ? "border-primary bg-primary/5" : "hover:bg-muted/40",
        (disabled || busy) && "cursor-not-allowed opacity-70"
      )}
    >
      <input {...getInputProps()} />
      {busy ? <Loader2 className="size-5 animate-spin text-muted-foreground" /> : <Upload className="size-5 text-muted-foreground" />}
      <span className="text-sm">{busy ? "上传中" : isDragActive ? "松开即上传" : kind === "sprite" ? "拖入序列帧网格图" : "拖入或点击上传图片"}</span>
      {!compact ? <span className="text-xs text-muted-foreground">PNG / WEBP / GIF / JPEG / SVG，单个不超过 {formatBytes(maxBytes)}，可多选</span> : null}
    </div>
  );
}

export function CosmeticsAssetsPanel({
  appKey,
  catalog,
  can
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  can: { write: boolean; delete: boolean };
}) {
  const [kind, setKind] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [uploadKind, setUploadKind] = useState<"image" | "sprite">("image");
  const [deleting, setDeleting] = useState<CosmeticAsset | null>(null);
  const debounced = useDebouncedValue(keyword.trim());
  const query = useCosmeticAssetsQuery(appKey, { kind, keyword: debounced, page, limit: PAGE_SIZE });
  const remove = useDeleteCosmeticAssetMutation(appKey);
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  const doDelete = async () => {
    if (!deleting) return;
    try {
      await remove.mutateAsync(deleting.id);
      toast.success(`已删除「${deleting.name}」`);
      setDeleting(null);
    } catch (error) {
      toast.error(errorMessage(error, "删除失败"));
      setDeleting(null);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {can.write ? (
        <Section
          title="上传素材"
          icon={Upload}
          tone="primary"
          actions={
            <Segmented
              value={uploadKind}
              onChange={setUploadKind}
              options={[
                { value: "image", label: "图片" },
                { value: "sprite", label: "序列帧" }
              ]}
              ariaLabel="素材类型"
            />
          }
        >
          <AssetDropzone appKey={appKey} catalog={catalog} kind={uploadKind} onUploaded={() => setPage(1)} />
        </Section>
      ) : null}

      <Section
        title="素材库"
        icon={Images}
        tone="info"
        description="同一应用的装扮共用，按内容去重"
        actions={<Badge variant="outline" size="sm" className="tabular-nums">{formatCount(total)} 个</Badge>}
        toolbar={
          <FilterBar>
            <FilterSelect
              value={kind}
              onChange={(value) => {
                setKind(value);
                setPage(1);
              }}
              label="类型"
              options={[
                { value: "image", label: "图片" },
                { value: "sprite", label: "序列帧" }
              ]}
            />
            <SearchField
              value={keyword}
              onChange={(value) => {
                setKeyword(value);
                setPage(1);
              }}
              placeholder="素材名称"
            />
          </FilterBar>
        }
        footer={total > PAGE_SIZE ? <Pager page={page} limit={PAGE_SIZE} total={total} onPage={setPage} /> : undefined}
      >
        {query.isLoading ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
            {Array.from({ length: 12 }, (_, index) => (
              <Skeleton key={index} className="aspect-square rounded-lg" />
            ))}
          </div>
        ) : query.isError && !query.data ? (
          <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <StateBlock compact icon={Images} title="暂无素材" />
        ) : (
          <div className={cn("grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6", query.isFetching && "opacity-70")}>
            {rows.map((asset) => (
              <AssetTile key={asset.id} asset={asset} canDelete={can.delete} onDelete={() => setDeleting(asset)} />
            ))}
          </div>
        )}
      </Section>

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && !remove.isPending && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除素材「{deleting?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>文件会从对象存储中删除，不可恢复。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <Button variant="destructive" onClick={() => void doDelete()} disabled={remove.isPending}>
              {remove.isPending ? <Loader2 className="animate-spin" /> : null}
              删除
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export function AssetThumb({ asset, className }: { asset: Pick<CosmeticAsset, "url" | "name">; className?: string }) {
  return (
    <div className={cn("flex items-center justify-center overflow-hidden bg-muted/50", className)}>
      {asset.url ? (
        // eslint-disable-next-line @next/next/no-img-element -- 存储代理地址，不经 next/image 优化
        <img src={resolveAssetUrl(asset.url)} alt={asset.name} loading="lazy" className="max-h-full max-w-full object-contain" />
      ) : (
        <ImageIcon className="size-5 text-muted-foreground" />
      )}
    </div>
  );
}

function AssetTile({ asset, canDelete, onDelete }: { asset: CosmeticAsset; canDelete: boolean; onDelete: () => void }) {
  const used = asset.usedBy > 0;
  return (
    <div className="group flex flex-col overflow-hidden rounded-lg border bg-card">
      <div className="relative">
        <AssetThumb asset={asset} className="aspect-square p-2" />
        <span className="absolute top-1.5 left-1.5 flex gap-1">
          {asset.kind === "sprite" ? (
            <Badge variant="info" size="sm">
              <Film />
              序列帧
            </Badge>
          ) : null}
        </span>
        {canDelete ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="absolute top-1.5 right-1.5">
                <Button
                  size="icon-xs"
                  variant="outline"
                  className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                  disabled={used}
                  onClick={onDelete}
                  aria-label="删除素材"
                >
                  <Trash2 />
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent>{used ? `被 ${asset.usedBy} 件装扮引用，不能删除` : "删除"}</TooltipContent>
          </Tooltip>
        ) : null}
      </div>
      <div className="flex flex-col gap-0.5 border-t px-2.5 py-2">
        <span className="truncate text-xs font-medium" title={asset.name}>
          {asset.name}
        </span>
        <span className="truncate text-[11px] tabular-nums text-muted-foreground">
          {asset.width}×{asset.height}，{formatBytes(asset.size)}
        </span>
        <span className="flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
          <span className={used ? "text-foreground" : undefined}>{used ? `${asset.usedBy} 件引用` : "未引用"}</span>
          <span>{formatRelative(asset.createdAt)}</span>
        </span>
      </div>
    </div>
  );
}
