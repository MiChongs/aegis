"use client";

import { useState } from "react";
import { Film, Replace } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Segmented } from "@/components/users/detail/user-assets-adjust";
import { SearchField, StateBlock, useDebouncedValue } from "@/components/users/detail/kit";
import type { CosmeticAsset, CosmeticCatalog } from "@/lib/api/cosmetics";
import { useCosmeticAssetsQuery } from "@/lib/cosmetic-hooks";
import { cn } from "@/lib/utils";
import { AssetDropzone, AssetThumb } from "../cosmetics-assets-panel";
import { useDesigner } from "./designer-store";

type AssetBrowserProps = {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  readOnly: boolean;
  dense?: boolean;
  onInsert: (asset: CosmeticAsset) => void;
  onReplace?: (asset: CosmeticAsset) => void;
};

/**
 * 设计器的素材面板：素材库网格（点一下插入为图层；选中了图片 / 序列帧图层时可以「替换」它的素材）、
 * 直接上传后插入、本件已引用的素材。插入时自动写进 design.assets（同一素材复用同一个键）。
 */
export function AssetBrowser(props: AssetBrowserProps) {
  const design = useDesigner((s) => s.design);
  const used = Object.values(design.assets ?? {}).map((ref) => ref.id);
  return <AssetLibrary {...props} usedIds={used} usedLabel="本件已引用" />;
}

/**
 * 素材库浏览（不依赖设计器）：搜索、上传、网格点选。`usedIds` 里的素材另起一组置顶（设计器里是
 * 「本件已引用」，工坊零件库里是「已是零件」）。
 */
export function AssetLibrary({
  appKey,
  catalog,
  readOnly,
  dense,
  usedIds,
  usedLabel,
  onInsert,
  onReplace
}: AssetBrowserProps & { usedIds?: number[]; usedLabel?: string }) {
  const [keyword, setKeyword] = useState("");
  const [kind, setKind] = useState<"image" | "sprite">("image");
  const debounced = useDebouncedValue(keyword.trim());
  const query = useCosmeticAssetsQuery(appKey, { keyword: debounced, limit: 120 });
  const used = new Set(usedIds ?? []);
  const assets = query.data?.items ?? [];
  const inUse = assets.filter((asset) => used.has(asset.id));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 flex-col gap-2 border-b p-2.5">
        {!readOnly ? (
          <>
            <Segmented
              value={kind}
              onChange={setKind}
              stretch
              options={[
                { value: "image", label: "图片" },
                { value: "sprite", label: "序列帧" }
              ]}
              ariaLabel="上传类型"
            />
            <AssetDropzone
              appKey={appKey}
              catalog={catalog}
              kind={kind}
              compact
              onUploaded={(uploaded) => {
                if (uploaded.length === 1) onInsert(uploaded[0]);
              }}
            />
          </>
        ) : null}
        <SearchField value={keyword} onChange={setKeyword} placeholder="搜索素材" className="sm:w-full" />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-2.5">
        {inUse.length ? (
          <div className="mb-3 flex flex-col gap-1.5">
            <span className="text-[11px] font-semibold text-muted-foreground">{usedLabel}</span>
            <AssetGrid assets={inUse} dense={dense} readOnly={readOnly} onInsert={onInsert} onReplace={onReplace} />
          </div>
        ) : null}
        <span className="mb-1.5 block text-[11px] font-semibold text-muted-foreground">素材库</span>
        {query.isLoading ? (
          <div className="grid grid-cols-3 gap-1.5">
            {Array.from({ length: 9 }, (_, i) => (
              <span key={i} className="aspect-square animate-pulse rounded-md bg-muted" />
            ))}
          </div>
        ) : assets.length === 0 ? (
          <StateBlock compact title="暂无素材" />
        ) : (
          <AssetGrid assets={assets} dense={dense} readOnly={readOnly} onInsert={onInsert} onReplace={onReplace} />
        )}
      </div>
    </div>
  );
}

function AssetGrid({
  assets,
  dense,
  readOnly,
  onInsert,
  onReplace
}: {
  assets: CosmeticAsset[];
  dense?: boolean;
  readOnly: boolean;
  onInsert: (asset: CosmeticAsset) => void;
  onReplace?: (asset: CosmeticAsset) => void;
}) {
  return (
    <div className={cn("grid gap-1.5", dense ? "grid-cols-3" : "grid-cols-4 sm:grid-cols-6")}>
      {assets.map((asset) => (
        <div key={asset.id} className="group relative overflow-hidden rounded-md border bg-card">
          <button
            type="button"
            disabled={readOnly}
            onClick={() => onInsert(asset)}
            className="block w-full"
            title={`${asset.name}（${asset.width}×${asset.height}）`}
          >
            <AssetThumb asset={asset} className="aspect-square p-1" />
          </button>
          {asset.kind === "sprite" ? (
            <Badge variant="info" size="sm" className="pointer-events-none absolute top-1 left-1 h-4 px-1">
              <Film />
            </Badge>
          ) : null}
          {onReplace && !readOnly ? (
            <Button
              size="icon-xs"
              variant="secondary"
              className="absolute right-1 bottom-1 opacity-0 transition-opacity group-hover:opacity-100"
              onClick={() => onReplace(asset)}
              title="替换选中图层的素材"
              aria-label="替换选中图层的素材"
            >
              <Replace />
            </Button>
          ) : null}
        </div>
      ))}
    </div>
  );
}

export function AssetPickerDialog({
  open,
  onOpenChange,
  appKey,
  catalog,
  kind,
  title,
  description,
  usedIds,
  usedLabel,
  onPick
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  catalog?: CosmeticCatalog | null;
  kind: "image" | "sprite";
  title?: string;
  description?: string;
  /** 置顶的一组素材（设计器传本件已引用的） */
  usedIds?: number[];
  usedLabel?: string;
  onPick: (asset: CosmeticAsset) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[78dvh] flex-col gap-0 p-0 sm:max-w-2xl">
        <DialogHeader className="border-b px-5 pt-4 pb-3">
          <DialogTitle>{title ?? (kind === "sprite" ? "选择序列帧素材" : "选择图片素材")}</DialogTitle>
          <DialogDescription>{description ?? "点选素材插入为新图层，或上传后直接插入"}</DialogDescription>
        </DialogHeader>
        <AssetLibrary
          appKey={appKey}
          catalog={catalog}
          readOnly={false}
          usedIds={usedIds}
          usedLabel={usedLabel ?? "本件已引用"}
          onInsert={(asset) => {
            onPick(asset);
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
