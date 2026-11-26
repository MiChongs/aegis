"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { LockKeyhole, MapPinned } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AccountSecurityPanel } from "@/components/security/account-security-panel";
import { GeoIntelPanel } from "@/components/security/geo-intel-panel";
import { usePermissionChecker } from "@/lib/permissions";

/**
 * 账户安全与地理风控。
 *
 * 防火墙（事件、规则、例外、WAF 设置、封禁、测试台）有独立工作台 /firewall：
 * 以前它的配置在 /configuration、日志与封禁在这里，同一件事拆在两页，排查时要来回跳。
 * 旧链接 /security?tab=firewall 转到 /firewall?tab=events。
 */
// 账户安全是管理员自己的二步验证与通行密钥，不挂权限点
const TABS = [
  { value: "account", label: "账户安全", icon: LockKeyhole, permission: undefined },
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
  useEffect(() => {
    if (rawTab === "firewall") router.replace("/firewall?tab=events");
  }, [rawTab, router]);

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
