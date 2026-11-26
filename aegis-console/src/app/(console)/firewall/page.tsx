"use client";

import { Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Activity, Ban, FlaskConical, ListChecks, ListFilter, Network, ScrollText, ShieldHalf } from "lucide-react";
import { EmptyState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FirewallStatusStrip } from "@/components/firewall/firewall-status-strip";
import { FirewallOverviewTab } from "@/components/firewall/overview/firewall-overview";
import { FirewallEventsTab } from "@/components/firewall/events/firewall-events";
import { FirewallRulesTab } from "@/components/firewall/rules/firewall-rules";
import { FirewallExclusionsTab } from "@/components/firewall/exclusions/firewall-exclusions";
import { FirewallSandboxTab } from "@/components/firewall/sandbox/firewall-sandbox";
import { FirewallWafTab } from "@/components/firewall/waf/firewall-waf";
import { FirewallAccessTab } from "@/components/firewall/access/firewall-access";
import { FirewallBansTab } from "@/components/firewall/bans/firewall-bans";
import { usePermission } from "@/lib/permissions";

/**
 * 防火墙工作台（规则集 v2，docs/firewall.md）。页签同步到 `?tab=`，侧边栏三级项与面包屑据此定位。
 * 每个页签只在激活时挂载：总览与事件的轮询不该在看规则时继续跑。
 */
const TABS = [
  { value: "overview", label: "总览", icon: Activity },
  { value: "events", label: "事件", icon: ScrollText },
  { value: "rules", label: "自定义规则", icon: ListFilter },
  { value: "waf", label: "WAF 引擎", icon: ShieldHalf },
  { value: "exclusions", label: "WAF 例外", icon: ListChecks },
  { value: "access", label: "访问控制", icon: Network },
  { value: "bans", label: "封禁", icon: Ban },
  { value: "sandbox", label: "测试台", icon: FlaskConical }
] as const;

export type FirewallTab = (typeof TABS)[number]["value"];
const VALID = new Set<string>(TABS.map((tab) => tab.value));

function FirewallPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const raw = searchParams.get("tab");
  const tab: FirewallTab = raw && VALID.has(raw) ? (raw as FirewallTab) : "overview";
  const go = (value: FirewallTab) => router.replace(`/firewall?tab=${value}`, { scroll: false });

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="安全与风控" title="防火墙" />
      <FirewallStatusStrip onNavigate={go} />
      <Tabs value={tab} onValueChange={(value) => go(value as FirewallTab)} className="gap-4">
        <TabsList className="w-full justify-start overflow-x-auto sm:w-fit">
          {TABS.map(({ value, label, icon: Icon }) => (
            <TabsTrigger key={value} value={value} className="flex-none">
              <Icon className="size-4" />
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="overview">{tab === "overview" ? <FirewallOverviewTab onNavigate={go} /> : null}</TabsContent>
        <TabsContent value="events">{tab === "events" ? <FirewallEventsTab onNavigate={go} /> : null}</TabsContent>
        <TabsContent value="rules">{tab === "rules" ? <FirewallRulesTab onNavigate={go} /> : null}</TabsContent>
        <TabsContent value="waf">{tab === "waf" ? <FirewallWafTab onNavigate={go} /> : null}</TabsContent>
        <TabsContent value="exclusions">{tab === "exclusions" ? <FirewallExclusionsTab onNavigate={go} /> : null}</TabsContent>
        <TabsContent value="access">{tab === "access" ? <FirewallAccessTab /> : null}</TabsContent>
        <TabsContent value="bans">{tab === "bans" ? <FirewallBansTab /> : null}</TabsContent>
        <TabsContent value="sandbox">{tab === "sandbox" ? <FirewallSandboxTab onNavigate={go} /> : null}</TabsContent>
      </Tabs>
    </div>
  );
}

export default function FirewallPage() {
  const { allowed } = usePermission("system:firewall:read");
  if (!allowed) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="安全与风控" title="防火墙" />
        <EmptyState title="缺少「防火墙：查看」权限" />
      </div>
    );
  }
  return (
    <Suspense>
      <FirewallPageInner />
    </Suspense>
  );
}
