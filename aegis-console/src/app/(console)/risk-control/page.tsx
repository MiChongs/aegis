"use client";

import { Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Activity, Boxes, Brain, ClipboardCheck, Layers, ListChecks, Network, ScrollText, Server, Workflow,
} from "lucide-react";

import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AppScopeSelect } from "@/components/risk/kit/app-scope";
import { RiskOverview } from "@/components/risk/overview";
import { RiskScenes } from "@/components/risk/scenes";
import { RiskFeatures } from "@/components/risk/features";
import { RiskLists } from "@/components/risk/lists";
import { RiskPatterns } from "@/components/risk/patterns";
import { RiskModels } from "@/components/risk/models";
import { RiskDecisions } from "@/components/risk/decisions";
import { RiskCases } from "@/components/risk/cases";
import { RiskGraphExplorer } from "@/components/risk/graph-explorer";
import { RiskSystem } from "@/components/risk/system";

/**
 * 风控平台（docs/risk-platform.md）。数据与配置都在独立服务 risk-admin 里，经主服务转发。
 *
 * 页签与应用作用域同步到 `?tab=&app=`，侧边栏三级项与深链都依赖它；每个页签只在激活时挂载。
 * 场景的完整配置（管线、规则、决策流、策略、模拟、发布）在 /risk-control/scenes/[key]。
 */
const TABS = [
  { value: "overview", label: "总览", icon: Activity, scoped: true },
  { value: "scenes", label: "场景", icon: Workflow, scoped: false },
  { value: "features", label: "特征", icon: Layers, scoped: false },
  { value: "lists", label: "名单", icon: ListChecks, scoped: true },
  { value: "patterns", label: "行为模式", icon: Boxes, scoped: false },
  { value: "models", label: "模型", icon: Brain, scoped: false },
  { value: "decisions", label: "决策流水", icon: ScrollText, scoped: true },
  { value: "cases", label: "复核", icon: ClipboardCheck, scoped: true },
  { value: "graph", label: "关系图谱", icon: Network, scoped: true },
  { value: "system", label: "系统", icon: Server, scoped: false },
] as const;

type TabValue = (typeof TABS)[number]["value"];
const VALID = new Set<string>(TABS.map((t) => t.value));

function RiskControlInner() {
  const params = useSearchParams();
  const router = useRouter();
  const raw = params.get("tab");
  const tab: TabValue = raw && VALID.has(raw) ? (raw as TabValue) : "overview";
  const appRaw = Number(params.get("app"));
  const app = Number.isFinite(appRaw) && appRaw > 0 ? appRaw : undefined;
  const scoped = TABS.find((t) => t.value === tab)?.scoped;

  const go = (next: { tab?: string; app?: number | null }) => {
    const q = new URLSearchParams(params.toString());
    if (next.tab) q.set("tab", next.tab);
    if (next.app !== undefined) {
      if (next.app) q.set("app", String(next.app));
      else q.delete("app");
    }
    router.replace(`/risk-control?${q.toString()}`, { scroll: false });
  };

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="安全"
        title="风控平台"
        action={scoped ? <AppScopeSelect value={app} onChange={(v) => go({ app: v ?? null })} /> : undefined}
      />
      <Tabs value={tab} onValueChange={(v) => go({ tab: v })} className="gap-4">
        <TabsList className="w-full justify-start overflow-x-auto sm:w-fit">
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value} className="gap-1.5">
              <t.icon className="size-4" />
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="overview">{tab === "overview" && <RiskOverview app={app} onNavigate={(t) => go({ tab: t })} />}</TabsContent>
        <TabsContent value="scenes">{tab === "scenes" && <RiskScenes />}</TabsContent>
        <TabsContent value="features">{tab === "features" && <RiskFeatures />}</TabsContent>
        <TabsContent value="lists">{tab === "lists" && <RiskLists app={app} />}</TabsContent>
        <TabsContent value="patterns">{tab === "patterns" && <RiskPatterns />}</TabsContent>
        <TabsContent value="models">{tab === "models" && <RiskModels />}</TabsContent>
        <TabsContent value="decisions">{tab === "decisions" && <RiskDecisions app={app} />}</TabsContent>
        <TabsContent value="cases">{tab === "cases" && <RiskCases app={app} />}</TabsContent>
        <TabsContent value="graph">{tab === "graph" && <RiskGraphExplorer app={app} />}</TabsContent>
        <TabsContent value="system">{tab === "system" && <RiskSystem />}</TabsContent>
      </Tabs>
    </div>
  );
}

export default function RiskControlPage() {
  return (
    <Suspense>
      <RiskControlInner />
    </Suspense>
  );
}
