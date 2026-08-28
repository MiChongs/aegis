"use client";

import { Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Flame, Globe, Globe2, KeyRound, Mail, Network, Palette, ShieldCheck, Sparkles, TrendingUp } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { EmptyState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { BrandingPanel } from "@/components/configuration/branding-panel";
import { FirewallConfigPanel } from "@/components/security/firewall-config-panel";
import { PlatformSecurityPanel } from "@/components/security/platform-security-panel";
import { LDAPConfigPanel } from "@/components/security/ldap-config-panel";
import { OIDCConfigPanel } from "@/components/security/oidc-config-panel";
import { SAMLConfigPanel } from "@/components/security/saml-config-panel";
import { AdminCaptchaConfigPanel } from "@/components/security/admin-captcha-config-panel";
import { EgressConfigPanel } from "@/components/configuration/egress-config-panel";
import { TrafficRampPanel } from "@/components/configuration/traffic-ramp-panel";
import { EmailChannelPanel } from "@/components/email/email-channel-panel";
import { PLATFORM_EMAIL_SCOPE } from "@/lib/email-hooks";
import { AIChannelPanel } from "@/components/ai/ai-channel-panel";
import { PLATFORM_AI_SCOPE } from "@/lib/api/ai";

/**
 * 平台级配置中心 —— **不含任何应用级配置**。
 *
 * 这里过去混着两种作用域：前 5 个 Tab 跟着右上角的应用选择器走，
 * 「品牌」「系统安全」却是平台级、完全忽略那个选择器；
 * 其中「访问策略」「密码策略」还与 /apps?tab=policy 完全重复。
 * 现在应用级配置全部归 /apps（那里的应用选择器就是作用域），
 * 本页去掉选择器，只承载对整个平台生效的配置。
 *
 * 与 /security 的分工：本页管**配置**（改了什么会生效），
 * /security 管**运行态**（发生了什么：拦截日志、封禁、地理风控）。
 */
const TABS = [
  { value: "branding", label: "品牌与外观", icon: Palette },
  { value: "firewall", label: "防火墙与限流", icon: Flame },
  { value: "traffic-ramp", label: "流量爬坡", icon: TrendingUp },
  { value: "egress", label: "出海代理", icon: Globe2 },
  { value: "email", label: "邮件", icon: Mail },
  { value: "ai", label: "AI 服务", icon: Sparkles },
  { value: "security", label: "安全模块", icon: ShieldCheck },
  { value: "ldap", label: "LDAP", icon: Network },
  { value: "oidc", label: "OIDC", icon: Globe },
  { value: "saml", label: "SAML", icon: ShieldCheck },
  { value: "admin-captcha", label: "管理员验证码", icon: KeyRound }
] as const;

const VALID_TABS = new Set(TABS.map((t) => t.value as string));

function ConfigurationPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const operator = useAuthStore((state) => state.operator);
  const isSuperAdmin = Boolean(operator?.isSuperAdmin);

  const rawTab = searchParams.get("tab");
  const tab = rawTab && VALID_TABS.has(rawTab) ? rawTab : "branding";

  if (!isSuperAdmin) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Configuration" title="配置" />
        <EmptyState
          title="需要超级管理员权限"
          description="平台级配置对整个平台的所有应用生效，仅超级管理员可见。应用自身的配置请前往「应用与内容 → 应用」。"
        />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Configuration"
        title="配置"
        description="对整个平台生效的配置。单个应用的策略、邮件、支付等配置在「应用与内容 → 应用」。"
      />

      <Tabs
        value={tab}
        onValueChange={(v) => router.replace(`/configuration?tab=${v}`, { scroll: false })}
        className="space-y-5"
      >
        <TabsList className="w-full justify-start overflow-x-auto">
          {TABS.map(({ value, label, icon: Icon }, index) => (
            <div key={value} className="flex items-center">
              {/* 认证联邦三项（LDAP/OIDC/SAML）与前面的平台防护分开 */}
              {index === 7 && <span aria-hidden className="mx-1.5 h-4 w-px shrink-0 bg-border" />}
              {index === 10 && <span aria-hidden className="mx-1.5 h-4 w-px shrink-0 bg-border" />}
              <TabsTrigger value={value}>
                <Icon className="size-4" />
                {label}
              </TabsTrigger>
            </div>
          ))}
        </TabsList>

        <TabsContent value="branding"><BrandingPanel /></TabsContent>
        <TabsContent value="firewall"><FirewallConfigPanel /></TabsContent>
        <TabsContent value="traffic-ramp"><TrafficRampPanel /></TabsContent>
        <TabsContent value="egress"><EgressConfigPanel /></TabsContent>
        <TabsContent value="email"><EmailChannelPanel scope={PLATFORM_EMAIL_SCOPE} /></TabsContent>
        <TabsContent value="ai"><AIChannelPanel scope={PLATFORM_AI_SCOPE} /></TabsContent>
        <TabsContent value="security"><PlatformSecurityPanel /></TabsContent>
        <TabsContent value="ldap"><LDAPConfigPanel /></TabsContent>
        <TabsContent value="oidc"><OIDCConfigPanel /></TabsContent>
        <TabsContent value="saml"><SAMLConfigPanel /></TabsContent>
        <TabsContent value="admin-captcha"><AdminCaptchaConfigPanel /></TabsContent>
      </Tabs>
    </div>
  );
}

export default function ConfigurationPage() {
  return (
    <Suspense>
      <ConfigurationPageInner />
    </Suspense>
  );
}
