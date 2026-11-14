"use client";

import { Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Flame, Globe, Globe2, KeyRound, Mail, Network, Palette, ShieldCheck, Sparkles } from "lucide-react";
import { usePermissionChecker, usePermissionsLoading } from "@/lib/permissions";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { BrandingPanel } from "@/components/configuration/branding-panel";
import { FirewallConfigPanel } from "@/components/security/firewall-config-panel";
import { PlatformSecurityPanel } from "@/components/security/platform-security-panel";
import { LDAPConfigPanel } from "@/components/security/ldap-config-panel";
import { OIDCConfigPanel } from "@/components/security/oidc-config-panel";
import { SAMLConfigPanel } from "@/components/security/saml-config-panel";
import { AdminCaptchaConfigPanel } from "@/components/security/admin-captcha-config-panel";
import { EgressGatewayPanel } from "@/components/configuration/egress/egress-gateway-panel";
import { EmailWorkspace } from "@/components/email/email-workspace";
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
  { value: "branding", label: "品牌与外观", icon: Palette, permission: "system:config:read" },
  { value: "firewall", label: "防火墙与限流", icon: Flame, permission: "system:config:read" },
  { value: "egress", label: "出海代理", icon: Globe2, permission: "system:egress:read" },
  { value: "email", label: "邮件", icon: Mail, permission: "email:read" },
  { value: "ai", label: "AI 服务", icon: Sparkles, permission: "ai:read" },
  { value: "security", label: "安全模块", icon: ShieldCheck, permission: "system:config:read" },
  { value: "ldap", label: "LDAP", icon: Network, permission: "system:config:read" },
  { value: "oidc", label: "OIDC", icon: Globe, permission: "system:config:read" },
  { value: "saml", label: "SAML", icon: ShieldCheck, permission: "system:config:read" },
  { value: "admin-captcha", label: "管理员验证码", icon: KeyRound, permission: "system:config:read" }
] as const;

function ConfigurationPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const can = usePermissionChecker();
  const loading = usePermissionsLoading();
  const visible = TABS.filter((t) => can(t.permission));
  const shown = new Set<string>(visible.map((t) => t.value));

  const rawTab = searchParams.get("tab");
  const tab = rawTab && shown.has(rawTab) ? rawTab : visible[0]?.value;

  if (!tab) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Configuration" title="配置" />
        {loading ? <LoadingState title="加载中" /> : <EmptyState title="缺少「平台设置：查看」权限" />}
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="Configuration" title="配置" />

      <Tabs
        value={tab}
        onValueChange={(v) => router.replace(`/configuration?tab=${v}`, { scroll: false })}
        className="space-y-5"
      >
        <TabsList className="w-full justify-start overflow-x-auto">
          {visible.map(({ value, label, icon: Icon }, index) => (
            <div key={value} className="flex items-center">
              {/* 认证联邦三项（LDAP/OIDC/SAML）与前面的平台防护分开 */}
              {index > 0 && (value === "ldap" || value === "admin-captcha") && (
                <span aria-hidden className="mx-1.5 h-4 w-px shrink-0 bg-border" />
              )}
              <TabsTrigger value={value}>
                <Icon className="size-4" />
                {label}
              </TabsTrigger>
            </div>
          ))}
        </TabsList>

        {shown.has("branding") ? <TabsContent value="branding"><BrandingPanel /></TabsContent> : null}
        {shown.has("firewall") ? <TabsContent value="firewall"><FirewallConfigPanel /></TabsContent> : null}
        {shown.has("egress") ? <TabsContent value="egress"><EgressGatewayPanel /></TabsContent> : null}
        {shown.has("email") ? <TabsContent value="email"><EmailWorkspace scope={PLATFORM_EMAIL_SCOPE} /></TabsContent> : null}
        {shown.has("ai") ? <TabsContent value="ai"><AIChannelPanel scope={PLATFORM_AI_SCOPE} /></TabsContent> : null}
        {shown.has("security") ? <TabsContent value="security"><PlatformSecurityPanel /></TabsContent> : null}
        {shown.has("ldap") ? <TabsContent value="ldap"><LDAPConfigPanel /></TabsContent> : null}
        {shown.has("oidc") ? <TabsContent value="oidc"><OIDCConfigPanel /></TabsContent> : null}
        {shown.has("saml") ? <TabsContent value="saml"><SAMLConfigPanel /></TabsContent> : null}
        {shown.has("admin-captcha") ? <TabsContent value="admin-captcha"><AdminCaptchaConfigPanel /></TabsContent> : null}
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
