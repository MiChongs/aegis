"use client";

import { useState } from "react";
import { BarChart3, Images, Puzzle, ReceiptText, ScanEye, Shirt, Sparkles } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { useCosmeticCatalogQuery, useCosmeticItemsQuery, useCosmeticStatsQuery } from "@/lib/cosmetic-hooks";
import { CosmeticsAssetsPanel } from "./cosmetics-assets-panel";
import { CosmeticsLibraryPanel, type LibraryFilter } from "./cosmetics-library-panel";
import { CosmeticsOverviewPanel } from "./cosmetics-overview-panel";
import { CosmeticsOwnershipsPanel, type OwnershipFilter } from "./cosmetics-ownerships-panel";
import { CosmeticsWorkshopItemsPanel } from "./cosmetics-workshop-items-panel";
import { CosmeticsWorkshopPartsPanel } from "./cosmetics-workshop-parts-panel";
import { CosmeticDesigner } from "./designer/cosmetic-designer";

/**
 * 个性装扮区块（docs/cosmetics.md）。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 有多少件、多少人持有与佩戴、从哪里发出去的、哪些最受欢迎、还有几份草稿没发布；开关与勋章位数 |
 * | 装扮库 | 每个槽位有哪些装扮、长什么样、上没上架；新建（可套模板）、复制、排序、上下架、删除，进设计器 |
 * | 素材库 | 上传与管理设计稿引用的图片和序列帧 |
 * | 持有记录 | 谁从哪里得到了哪一件、到什么时候 |
 * | 零件库 | 自定义工坊里用户能用哪些官方图片与序列帧（工坊开关与额度在概览的「自定义工坊」卡片） |
 * | 作品巡查 | 用户在工坊里做了什么；下架（原因作者可见）与恢复 |
 *
 * 设计器是全屏弹层（designer/），关闭时刷新列表上的「未发布修改」与预览图。
 * 视图状态不进 URL：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "library", label: "装扮库", icon: Shirt },
  { key: "assets", label: "素材库", icon: Images },
  { key: "ownerships", label: "持有记录", icon: ReceiptText },
  { key: "workshop-parts", label: "零件库", icon: Puzzle },
  { key: "workshop-items", label: "作品巡查", icon: ScanEye }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppCosmeticsPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  const [libraryFilter, setLibraryFilter] = useState<{ value: LibraryFilter; key: number }>({ value: {}, key: 0 });
  const [ownershipFilter, setOwnershipFilter] = useState<{ value: OwnershipFilter; key: number }>({ value: {}, key: 0 });
  const [designerItem, setDesignerItem] = useState<number | null>(null);

  const can = useAppKeyPermissionChecker(appKey);
  const catalogQuery = useCosmeticCatalogQuery(appKey);
  const itemsQuery = useCosmeticItemsQuery(appKey);
  const statsQuery = useCosmeticStatsQuery(appKey);

  if (!appKey) {
    return <NoAppSelected icon={<Sparkles className="size-5" />} />;
  }

  const catalog = catalogQuery.data;
  const permissions = {
    write: can("app:cosmetic:write"),
    delete: can("app:cosmetic:delete"),
    publish: can("app:cosmetic:publish")
  };

  const openLibrary = (filter: LibraryFilter = {}) => {
    setLibraryFilter((current) => ({ value: filter, key: current.key + 1 }));
    setView("library");
  };
  const openOwnerships = (filter: OwnershipFilter = {}) => {
    setOwnershipFilter((current) => ({ value: filter, key: current.key + 1 }));
    setView("ownerships");
  };

  const drafts = statsQuery.data?.draftsPending ?? 0;

  return (
    <div className="flex flex-col gap-4">
      <Tabs value={view} onValueChange={(value) => setView(value as ViewKey)} className="gap-4">
        <div className="-mx-1 overflow-x-auto px-1">
          <TabsList className="h-10">
            {VIEWS.map((item) => (
              <TabsTrigger key={item.key} value={item.key} className="gap-1.5 px-3">
                <item.icon />
                {item.label}
                {item.key === "library" && itemsQuery.data ? (
                  <span className="text-xs font-normal tabular-nums text-muted-foreground">{itemsQuery.data.length}</span>
                ) : null}
                {item.key === "library" && drafts > 0 ? (
                  <Badge variant="warning" size="sm" className="tabular-nums">
                    {drafts}
                  </Badge>
                ) : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>

      {view === "overview" ? (
        <CosmeticsOverviewPanel
          appKey={appKey}
          catalog={catalog}
          canWrite={permissions.write}
          onOpenLibrary={openLibrary}
          onOpenAssets={() => setView("assets")}
          onOpenOwnerships={openOwnerships}
          onOpenItem={setDesignerItem}
        />
      ) : null}
      {view === "library" ? (
        <CosmeticsLibraryPanel
          key={libraryFilter.key}
          appKey={appKey}
          catalog={catalog}
          can={permissions}
          initialFilter={libraryFilter.value}
          onOpenDesigner={setDesignerItem}
          onOpenOwnerships={(cosmeticId) => openOwnerships({ cosmeticId })}
        />
      ) : null}
      {view === "assets" ? <CosmeticsAssetsPanel appKey={appKey} catalog={catalog} can={permissions} /> : null}
      {view === "workshop-parts" ? <CosmeticsWorkshopPartsPanel appKey={appKey} catalog={catalog} can={permissions} /> : null}
      {view === "workshop-items" ? (
        <CosmeticsWorkshopItemsPanel appKey={appKey} catalog={catalog} canWrite={permissions.write} />
      ) : null}
      {view === "ownerships" ? (
        <CosmeticsOwnershipsPanel key={ownershipFilter.key} appKey={appKey} catalog={catalog} initial={ownershipFilter.value} />
      ) : null}

      <CosmeticDesigner
        appKey={appKey}
        itemId={designerItem}
        catalog={catalog}
        can={permissions}
        onClose={() => setDesignerItem(null)}
      />
    </div>
  );
}
