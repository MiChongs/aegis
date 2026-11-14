"use client";

import { Bug, Database, MemoryStick, Server, ServerCog } from "lucide-react";
import { CrashLogPanel } from "@/components/monitor/crash-log-panel";
import { DatabasePanel } from "@/components/monitor/database-panel";
import { MemoryPanel } from "@/components/monitor/memory-panel";
import { SystemInfoPanel } from "@/components/monitor/system-info-panel";
import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { Card } from "@/components/ui/card";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePermissionChecker } from "@/lib/permissions";

const TABS = [
  { value: "host", label: "主机", icon: Server, permission: "system:config:read", render: () => <SystemInfoPanel /> },
  { value: "database", label: "数据库", icon: Database, permission: "system:database:read", render: () => <DatabasePanel /> },
  { value: "memory", label: "内存", icon: MemoryStick, permission: "system:memory:read", render: () => <MemoryPanel /> },
  { value: "crash", label: "崩溃日志", icon: Bug, permission: "system:crashlog:read", render: () => <CrashLogPanel /> }
] as const;

/** 当前管理员能看的运行面板；全都没有时调用方不挂载本卡片 */
export function useRuntimeTabs() {
  const can = usePermissionChecker();
  return TABS.filter((tab) => can(tab.permission));
}

/**
 * 系统运行：四块面板合进一张分页卡片，只挂载当前分页，未打开的面板也不再轮询。
 * 手机上分页条放不下时横向滑动。每块按各自的查看权限显示。
 */
export function RuntimeSection() {
  const tabs = useRuntimeTabs();
  if (!tabs.length) return null;
  return (
    <Card aria-label="系统运行" className="min-w-0 gap-0 py-0">
      <Tabs defaultValue={tabs[0].value} className="gap-0">
        <div className="flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:px-5">
          <div className="flex items-center gap-3">
            <TonalIcon icon={ServerCog} tone="muted" size="sm" />
            <h2 className="text-sm font-semibold">系统运行</h2>
          </div>
          <ScrollArea className="-mx-4 sm:mx-0 sm:ml-auto">
            <TabsList className="mx-4 sm:mx-0">
              {tabs.map((tab) => (
                <TabsTrigger key={tab.value} value={tab.value} className="px-3 text-xs">
                  <tab.icon className="size-3.5" />
                  {tab.label}
                </TabsTrigger>
              ))}
            </TabsList>
            <ScrollBar orientation="horizontal" className="h-1.5" />
          </ScrollArea>
        </div>
        {tabs.map((tab) => (
          <TabsContent key={tab.value} value={tab.value} className="min-w-0 p-4 sm:p-5">
            <WidgetBoundary title={`${tab.label}信息加载失败`}>{tab.render()}</WidgetBoundary>
          </TabsContent>
        ))}
      </Tabs>
    </Card>
  );
}
