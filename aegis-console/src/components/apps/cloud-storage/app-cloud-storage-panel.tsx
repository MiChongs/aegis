"use client";

import { useState } from "react";
import { BarChart3, Cloud, Settings2, Users } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { CloudStorageConfigPanel } from "@/components/apps/cloud-storage/cloud-storage-config-panel";
import { CloudStorageOverviewPanel } from "@/components/apps/cloud-storage/cloud-storage-overview-panel";
import { CloudStorageUsersPanel } from "@/components/apps/cloud-storage/cloud-storage-users-panel";
import { cn } from "@/lib/utils";

/**
 * 用户云存储区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 有多少人在用、占了多少、写到了哪里、哪些命名空间最大、谁用得最多 |
 * | 用户 | 每个人用了多少、有没有被冻结；点进去是那个人的条目与修订 |
 * | 配置 | 开不开、写到哪个存储配置、每人多少空间、留几个修订、回收站留几天 |
 *
 * 单个用户的管理不在这里展开：它在用户详情的「云存储」页签，与资料、钱包同一处 ——
 * 管理员处理一个人的问题时，不该在两个页面之间来回切。
 * 视图状态不进 URL，理由同卡密区块：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "users", label: "用户", icon: Users },
  { key: "config", label: "配置", icon: Settings2 }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppCloudStoragePanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");

  if (!appKey) {
    return <NoAppSelected icon={<Cloud className="size-5" />} />;
  }

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
              view === item.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <CloudStorageOverviewPanel
          appKey={appKey}
          onOpenConfig={() => setView("config")}
          onOpenUsers={() => setView("users")}
        />
      ) : null}
      {view === "users" ? <CloudStorageUsersPanel appKey={appKey} /> : null}
      {view === "config" ? <CloudStorageConfigPanel appKey={appKey} /> : null}
    </div>
  );
}
