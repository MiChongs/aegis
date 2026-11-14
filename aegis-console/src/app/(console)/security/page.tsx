"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Flame, LockKeyhole, MapPinned } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AccountSecurityPanel } from "@/components/security/account-security-panel";
import { FirewallLogsPanel } from "@/components/security/firewall-logs-panel";
import { IPBanPanel } from "@/components/security/ip-ban-panel";
import { GeoBanPanel } from "@/components/security/geo-ban-panel";
import { GeoIntelPanel } from "@/components/security/geo-intel-panel";
import { usePermissionChecker } from "@/lib/permissions";

/**
 * 安全**运行态** —— 发生了什么。
 *
 * 平台安全的**配置**（安全模块、LDAP / OIDC / SAML、管理员验证码、防火墙规则）
 * 已统一迁到 /configuration。此前防火墙的规则配置在 /configuration「系统安全」、
 * 拦截日志与封禁在这里，同一件事被拆到两个页面，排查时要来回跳。
 */
// 账户安全是管理员自己的二步验证与通行密钥，不挂权限点
const TABS = [
  { value: "account", label: "账户安全", icon: LockKeyhole, permission: undefined },
  { value: "firewall", label: "防火墙", icon: Flame, permission: "system:firewall:read" },
  { value: "geo-intel", label: "地理风控", icon: MapPinned, permission: "system:geo:read" }
] as const;

function SecurityPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const can = usePermissionChecker();
  const visible = TABS.filter((t) => can(t.permission));
  const shown = new Set<string>(visible.map((t) => t.value));
  // 旧链接（含已下线的 ?tab=overview）与无权查看的页签都回退到第一个可见页签
  const rawTab = searchParams.get("tab");
  const tab = rawTab && shown.has(rawTab) ? rawTab : visible[0].value;
  const [firewallView, setFirewallView] = useState<"logs" | "bans" | "geo">("logs");

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="安全" />

      <Tabs
        value={tab}
        onValueChange={(v) => router.replace(`/security?tab=${v}`, { scroll: false })}
        className="space-y-6"
      >
        <TabsList className="w-full justify-start overflow-x-auto">
          {visible.map(({ value, label, icon: Icon }) => (
            <TabsTrigger key={value} value={value}>
              <Icon className="size-4" />
              {label}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="account">
          <AccountSecurityPanel />
        </TabsContent>

        {shown.has("firewall") ? (
          <TabsContent value="firewall">
            <div className="space-y-5">
              {/* 子视图切换 */}
              <div className="flex w-fit items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
                {(
                  [
                    { key: "logs", label: "拦截日志" },
                    { key: "bans", label: "IP 封禁" },
                    { key: "geo", label: "地域封禁" }
                  ] as const
                ).map(({ key, label }) => (
                  <button
                    key={key}
                    className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                      firewallView === key
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground"
                    }`}
                    onClick={() => setFirewallView(key)}
                  >
                    {label}
                  </button>
                ))}
              </div>

              {firewallView === "logs" ? (
                <FirewallLogsPanel />
              ) : firewallView === "bans" ? (
                <IPBanPanel />
              ) : (
                <GeoBanPanel />
              )}
            </div>
          </TabsContent>
        ) : null}

        {shown.has("geo-intel") ? (
          <TabsContent value="geo-intel">
            <GeoIntelPanel />
          </TabsContent>
        ) : null}
      </Tabs>
    </div>
  );
}

export default function SecurityPage() {
  return (
    <Suspense>
      <SecurityPageInner />
    </Suspense>
  );
}
