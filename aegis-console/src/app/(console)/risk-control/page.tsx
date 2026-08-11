"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Activity, Clock, Eye, FlaskConical, Globe2, Shield, ShieldAlert, Smartphone,
} from "lucide-react";

import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RiskActionsPanel } from "@/components/risk/risk-actions";
import { RiskAssessmentsPanel } from "@/components/risk/risk-assessments";
import { RiskDashboardPanel } from "@/components/risk/risk-dashboard";
import { RiskDevicesPanel, RiskIPsPanel } from "@/components/risk/risk-entities";
import { RiskReviewsPanel } from "@/components/risk/risk-reviews";
import { RiskRulesPanel } from "@/components/risk/risk-rules";
import { RiskSimulatorPanel } from "@/components/risk/risk-simulator";
import { RiskCatalogProvider } from "@/components/risk/risk-shared";

/**
 * 风控中心。
 *
 * 八个页签分三段职责：
 *   **看** —— 大盘 / 评估记录 / 复核（发生了什么、拦得对不对）
 *   **配** —— 规则 / 处置策略（怎么判、判了怎么办）
 *   **查** —— 设备 / IP（谁在打）
 * 模拟器横跨"配"与"看"：改规则之前先在这里试。
 *
 * 页签同步到 URL（`?tab=`），侧边栏三级子项与深链都依赖它 ——
 * 只存在组件 state 里的话，导航子项点了不会切面板。
 */
const TABS = [
  { value: "dashboard", label: "风控大盘", icon: Activity },
  { value: "assessments", label: "评估记录", icon: Eye },
  { value: "reviews", label: "人工复核", icon: Clock },
  { value: "rules", label: "风险规则", icon: Shield },
  { value: "actions", label: "处置策略", icon: ShieldAlert },
  { value: "simulator", label: "规则模拟", icon: FlaskConical },
  { value: "devices", label: "设备指纹", icon: Smartphone },
  { value: "ips", label: "IP 风险库", icon: Globe2 },
] as const;

const VALID_TABS = new Set<string>(TABS.map((tab) => tab.value));

function RiskControlPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const rawTab = searchParams.get("tab");
  const tab = rawTab && VALID_TABS.has(rawTab) ? rawTab : "dashboard";

  // 跨页签的「带着一个实体去看它的记录」。大盘的 Top 榜、设备/IP 列表都会用到，
  // 否则排查时只能手动复制 IP 再切页签粘贴。
  const [focusRuleId, setFocusRuleId] = useState<number | null>(null);
  const [presetIP, setPresetIP] = useState<string | null>(null);
  const [presetDevice, setPresetDevice] = useState<string | null>(null);

  const go = (next: string) => router.replace(`/risk-control?tab=${next}`, { scroll: false });

  const inspectAssessmentsBy = (patch: { ip?: string; deviceId?: string }) => {
    setPresetIP(patch.ip ?? null);
    setPresetDevice(patch.deviceId ?? null);
    go("assessments");
  };

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="风控中心" />

      <Tabs value={tab} onValueChange={go} className="space-y-4">
        <TabsList className="w-full justify-start overflow-x-auto">
          {TABS.map(({ value, label, icon: Icon }) => (
            <TabsTrigger key={value} value={value}>
              <Icon className="size-3.5" />
              {label}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="dashboard">
          <RiskDashboardPanel
            onInspectRule={(ruleId) => { setFocusRuleId(ruleId); go("rules"); }}
            onInspectIP={(ip) => inspectAssessmentsBy({ ip })}
            onInspectDevice={(deviceId) => inspectAssessmentsBy({ deviceId })}
          />
        </TabsContent>

        <TabsContent value="assessments">
          <RiskAssessmentsPanel
            presetIP={presetIP}
            presetDevice={presetDevice}
            onClearPreset={() => { setPresetIP(null); setPresetDevice(null); }}
          />
        </TabsContent>

        <TabsContent value="reviews"><RiskReviewsPanel /></TabsContent>

        <TabsContent value="rules">
          <RiskRulesPanel focusRuleId={focusRuleId} onFocusHandled={() => setFocusRuleId(null)} />
        </TabsContent>

        <TabsContent value="actions"><RiskActionsPanel /></TabsContent>

        <TabsContent value="simulator"><RiskSimulatorPanel /></TabsContent>

        <TabsContent value="devices">
          <RiskDevicesPanel onQueryAssessments={(deviceId) => inspectAssessmentsBy({ deviceId })} />
        </TabsContent>

        <TabsContent value="ips">
          <RiskIPsPanel onQueryAssessments={(ip) => inspectAssessmentsBy({ ip })} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default function RiskControlPage() {
  return (
    // useSearchParams 必须包在 Suspense 边界内，否则 next build 报错、
    // 整页退化为客户端渲染（与 console-shell 的 withActiveTab 同一约束）。
    <Suspense fallback={<div className="page-stack"><SectionHeading eyebrow="控制台" title="风控中心" /></div>}>
      {/* 目录（场景 / 等级 / 条件参数 schema）整页共用一份，由 Provider 统一持有 */}
      <RiskCatalogProvider>
        <RiskControlPageInner />
      </RiskCatalogProvider>
    </Suspense>
  );
}
