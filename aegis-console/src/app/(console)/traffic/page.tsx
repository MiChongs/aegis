"use client";

import { Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Activity, GitCompare, History, ListFilter, SlidersHorizontal } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TrafficGate } from "@/components/traffic/traffic-gate";
import { TrafficHistoryTab } from "@/components/traffic/traffic-history";
import { TrafficOverviewTab } from "@/components/traffic/traffic-overview";
import { TrafficRevisionsTab } from "@/components/traffic/traffic-revisions";
import { TrafficRulesTab } from "@/components/traffic/traffic-rules";
import { TrafficSettingsTab } from "@/components/traffic/traffic-settings";

/**
 * 流量管理（内置插件 traffic）。页签同步到 `?tab=`，侧边栏三级项与面包屑据此定位。
 * 每个页签只在激活时挂载：总览与规则页的 2 秒轮询不该在看历史时继续跑。
 */
const TABS = [
  { value: "overview", label: "总览", icon: Activity },
  { value: "rules", label: "规则", icon: ListFilter },
  { value: "settings", label: "全局设置", icon: SlidersHorizontal },
  { value: "history", label: "历史", icon: History },
  { value: "revisions", label: "版本", icon: GitCompare }
] as const;

type TabValue = (typeof TABS)[number]["value"];
const VALID = new Set<string>(TABS.map((tab) => tab.value));

function TrafficPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const raw = searchParams.get("tab");
  const tab: TabValue = raw && VALID.has(raw) ? (raw as TabValue) : "overview";

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="流量管理" />
      <Tabs value={tab} onValueChange={(value) => router.replace(`/traffic?tab=${value}`, { scroll: false })} className="gap-4">
        <TabsList className="w-full justify-start overflow-x-auto sm:w-fit">
          {TABS.map(({ value, label, icon: Icon }) => (
            <TabsTrigger key={value} value={value} className="flex-none">
              <Icon className="size-4" />
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="overview">{tab === "overview" ? <TrafficOverviewTab /> : null}</TabsContent>
        <TabsContent value="rules">{tab === "rules" ? <TrafficRulesTab /> : null}</TabsContent>
        <TabsContent value="settings">{tab === "settings" ? <TrafficSettingsTab /> : null}</TabsContent>
        <TabsContent value="history">{tab === "history" ? <TrafficHistoryTab /> : null}</TabsContent>
        <TabsContent value="revisions">{tab === "revisions" ? <TrafficRevisionsTab /> : null}</TabsContent>
      </Tabs>
    </div>
  );
}

export default function TrafficPage() {
  return (
    <TrafficGate title="流量管理">
      <Suspense>
        <TrafficPageInner />
      </Suspense>
    </TrafficGate>
  );
}
