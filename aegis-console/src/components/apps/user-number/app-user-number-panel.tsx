"use client";

import { useState } from "react";
import { BarChart3, Hash, ListTree, Lock, Search, SlidersHorizontal } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { defaultNumberSettings, NUMBER_READ, NUMBER_WRITE, type NumberHoldKind, type NumberSettings } from "@/lib/api/user-number";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { usePermissionsLoading } from "@/lib/permissions";
import { useNumberCatalogQuery, useNumberSettingsQuery } from "@/lib/user-number-hooks";
import { cn } from "@/lib/utils";
import { NumberHoldsPanel, NumberInspectPanel } from "./number-holds-panel";
import { NumberOverviewPanel } from "./number-overview-panel";
import { NumberRecordsPanel } from "./number-records-panel";
import { NumberSettingsPanel } from "./number-settings-panel";

/**
 * 用户号码区块（docs/user-number.md）。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 各级号码有多少、换号与摇号热不热、占着多少号、还有多少权益没用 |
 * | 设置 | 开关、位数、冷却与冻结、摇号权重与号运、规律落在哪一级；三档号运下的概率试算 |
 * | 号码查询 | 一个号是几级、命中哪些规律、谁在用、有没有被占 |
 * | 占用 | 保留号（批量添加、放出）、冻结号、摇号候选 |
 * | 记录 | 换号记录、权益流水、摇号单，可按用户筛选 |
 *
 * 设置草稿放在这一层：试算要拿未保存的草稿算，切视图也不该丢改动。视图状态不进 URL（`?tab=` 已被区块占用）。
 * 读要 `app:number:read`，改设置与保留号要 `app:number:write`；指定 / 回滚 / 调权益在用户详情里，要 `app:number:grant`。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "settings", label: "设置", icon: SlidersHorizontal },
  { key: "inspect", label: "号码查询", icon: Search },
  { key: "holds", label: "占用", icon: Lock },
  { key: "records", label: "记录", icon: ListTree }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppUserNumberPanel({ appKey }: { appKey?: string | null }) {
  if (!appKey) return <NoAppSelected icon={<Hash className="size-5" />} />;
  return <NumberWorkspace appKey={appKey} />;
}

function NumberWorkspace({ appKey }: { appKey: string }) {
  const can = useAppKeyPermissionChecker(appKey);
  const permissionsLoading = usePermissionsLoading();
  const canRead = can(NUMBER_READ);
  const canWrite = can(NUMBER_WRITE);

  const [view, setView] = useState<ViewKey>("overview");
  const [holdKind, setHoldKind] = useState<NumberHoldKind | "">("");
  const catalogQuery = useNumberCatalogQuery(canRead ? appKey : null);
  const settingsQuery = useNumberSettingsQuery(canRead ? appKey : null);

  const [draft, setDraft] = useState<{ scope: string; value: NumberSettings } | null>(null);
  const saved = settingsQuery.data;
  const baseline = saved ?? defaultNumberSettings();
  const settings = draft?.scope === appKey ? draft.value : baseline;
  const dirty = draft?.scope === appKey && JSON.stringify(draft.value) !== JSON.stringify(baseline);
  const tiers = catalogQuery.data?.tiers;

  if (permissionsLoading) return <LoadingState title="加载中" />;
  if (!canRead) return <EmptyState title="缺少「用户号码：查看」权限" />;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
            {item.key === "settings" && dirty ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="有未保存的改动" /> : null}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <NumberOverviewPanel
          appKey={appKey}
          tiers={tiers}
          onOpenHolds={(kind) => {
            setHoldKind(kind);
            setView("holds");
          }}
        />
      ) : null}
      {view === "settings" ? (
        <NumberSettingsPanel
          appKey={appKey}
          settings={settings}
          saved={saved}
          catalog={catalogQuery.data}
          loading={settingsQuery.isLoading}
          dirty={dirty}
          canWrite={canWrite}
          onChange={(value) => setDraft({ scope: appKey, value })}
          onReset={() => setDraft(null)}
          onSaved={() => setDraft(null)}
        />
      ) : null}
      {view === "inspect" ? <NumberInspectPanel appKey={appKey} tiers={tiers} canWrite={canWrite} /> : null}
      {view === "holds" ? (
        <NumberHoldsPanel appKey={appKey} tiers={tiers} canWrite={canWrite} kind={holdKind} onKindChange={setHoldKind} />
      ) : null}
      {view === "records" ? <NumberRecordsPanel appKey={appKey} tiers={tiers} /> : null}
    </div>
  );
}
