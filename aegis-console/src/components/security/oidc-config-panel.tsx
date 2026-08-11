"use client";

import { useEffect, useState } from "react";
import {
  CheckCircle2, Globe, Key, Loader2, Lock, Network,
  RotateCcw, Save, Search, Settings2, Shield, Users, XCircle
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { testOIDCDiscovery } from "@/lib/api/system";
import { useAdminSystemSettingsQuery, useUpdateAdminSystemSettingsMutation } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import type { OIDCSettings, OIDCTestResult } from "@/lib/api/types";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/data-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

type Draft = {
  enabled: boolean;
  issuerURL: string;
  clientID: string;
  clientSecret: string;
  redirectURL: string;
  scopes: string;
  allowedDomains: string;
  adminGroupClaim: string;
  adminGroupValue: string;
  attrAccount: string;
  attrDisplayName: string;
  attrEmail: string;
  attrPhone: string;
  fallbackToLocal: boolean;
  frontendCallbackURL: string;
};

const PRESETS: Record<string, { issuer: string; label: string }> = {
  keycloak: { issuer: "https://{host}/realms/{realm}", label: "Keycloak" },
  auth0: { issuer: "https://{tenant}.auth0.com/", label: "Auth0" },
  azure: { issuer: "https://login.microsoftonline.com/{tenantId}/v2.0", label: "Azure AD" },
  google: { issuer: "https://accounts.google.com", label: "Google Workspace" },
  casdoor: { issuer: "https://{host}", label: "Casdoor" },
};

function seedDraft(oidc?: OIDCSettings): Draft {
  return {
    enabled: oidc?.enabled ?? false,
    issuerURL: oidc?.issuerURL ?? "",
    clientID: oidc?.clientID ?? "",
    clientSecret: "",
    redirectURL: oidc?.redirectURL ?? "",
    scopes: (oidc?.scopes ?? ["openid", "profile", "email"]).join(", "),
    allowedDomains: (oidc?.allowedDomains ?? []).join("\n"),
    adminGroupClaim: oidc?.adminGroupClaim ?? "",
    adminGroupValue: oidc?.adminGroupValue ?? "",
    attrAccount: oidc?.attrMapping?.account ?? "preferred_username",
    attrDisplayName: oidc?.attrMapping?.displayName ?? "name",
    attrEmail: oidc?.attrMapping?.email ?? "email",
    attrPhone: oidc?.attrMapping?.phone ?? "phone_number",
    fallbackToLocal: oidc?.fallbackToLocal ?? true,
    frontendCallbackURL: oidc?.frontendCallbackURL ?? "",
  };
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs font-medium">{label}</Label>
      {children}
      {hint && <p className="text-[10px] text-muted-foreground">{hint}</p>}
    </div>
  );
}

function SwitchRow({ label, hint, checked, onCheckedChange }: {
  label: string; hint?: string; checked: boolean; onCheckedChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 py-1">
      <div>
        <span className="text-xs font-medium">{label}</span>
        {hint && <p className="text-[10px] text-muted-foreground">{hint}</p>}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

export function OIDCConfigPanel() {
  const settingsQuery = useAdminSystemSettingsQuery();
  const updateMutation = useUpdateAdminSystemSettingsMutation();
  const token = useAuthStore((s) => s.accessToken);
  const oidc = settingsQuery.data?.oidc;
  const [draft, setDraft] = useState<Draft>(() => seedDraft(oidc));
  const [testResult, setTestResult] = useState<OIDCTestResult | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (oidc) setDraft(seedDraft(oidc));
  }, [oidc]);

  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));

  const handleTest = async () => {
    if (!token || !draft.issuerURL.trim()) {
      toast.error("请填写 Issuer URL");
      return;
    }
    setTesting(true);
    setTestResult(null);
    try {
      const result = await testOIDCDiscovery(token, { issuerURL: draft.issuerURL.trim() });
      setTestResult(result);
      if (result.discoveryOK) toast.success(`Discovery 成功 (${result.latencyMs}ms)`);
      else toast.error(result.error || "Discovery 失败");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "测试请求失败");
    } finally {
      setTesting(false);
    }
  };

  const handleSave = async () => {
    try {
      const payload: Record<string, unknown> = {
        enabled: draft.enabled,
        issuerURL: draft.issuerURL.trim(),
        clientID: draft.clientID.trim(),
        redirectURL: draft.redirectURL.trim(),
        scopes: draft.scopes.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean),
        allowedDomains: draft.allowedDomains.split(/\r?\n|,/).map((s) => s.trim()).filter(Boolean),
        adminGroupClaim: draft.adminGroupClaim.trim(),
        adminGroupValue: draft.adminGroupValue.trim(),
        attrMapping: {
          account: draft.attrAccount.trim(),
          displayName: draft.attrDisplayName.trim(),
          email: draft.attrEmail.trim(),
          phone: draft.attrPhone.trim(),
        },
        fallbackToLocal: draft.fallbackToLocal,
        frontendCallbackURL: draft.frontendCallbackURL.trim(),
      };
      if (draft.clientSecret) payload.clientSecret = draft.clientSecret;
      await updateMutation.mutateAsync({ oidc: payload as never });
      toast.success("OIDC 配置已保存并热重载");
      patch("clientSecret", "");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading) return <LoadingState title="加载中" />;

  return (
    <div className="space-y-5">
      {/* 状态概览 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard icon={Network} label="OIDC 状态" value={draft.enabled ? "已启用" : "未启用"} ok={draft.enabled} />
        <StatusCard icon={Globe} label="Issuer" value={draft.issuerURL ? new URL(draft.issuerURL).host : "--"} />
        <StatusCard icon={Key} label="Client Secret" value={oidc?.hasClientSecret ? "已设置" : "未设置"} ok={oidc?.hasClientSecret} />
        <StatusCard icon={Shield} label="数据来源" value={oidc?.source === "database" ? "数据库" : "未配置"} />
      </div>

      <Separator />

      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold">OIDC 认证配置</h3>
          <p className="text-xs text-muted-foreground">配置 OpenID Connect 统一身份认证（仅管理员层级）</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setDraft(seedDraft(oidc))} disabled={updateMutation.isPending}>
            <RotateCcw className="size-3.5" /> 重置
          </Button>
          <Button size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      <Accordion type="multiple" defaultValue={["provider"]} className="space-y-2">
        {/* 提供商配置 */}
        <AccordionItem value="provider" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Globe className="size-4 text-muted-foreground" /><span className="text-sm font-medium">提供商配置</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <SwitchRow label="启用 OIDC 认证" hint="启用后登录页显示 SSO 按钮" checked={draft.enabled} onCheckedChange={(v) => patch("enabled", v)} />
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-xs text-muted-foreground">快速填充：</span>
              {Object.entries(PRESETS).map(([k, v]) => (
                <Button key={k} variant="outline" size="sm" className="h-6 text-[10px] px-2"
                  onClick={() => patch("issuerURL", v.issuer)}>{v.label}</Button>
              ))}
            </div>
            <Row label="Issuer URL" hint="OIDC Discovery 端点（/.well-known/openid-configuration）"><Input value={draft.issuerURL} onChange={(e) => patch("issuerURL", e.target.value)} placeholder="https://auth.example.com/realms/master" /></Row>
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="Client ID"><Input value={draft.clientID} onChange={(e) => patch("clientID", e.target.value)} placeholder="aegis-admin" /></Row>
              <Row label="Client Secret" hint={oidc?.hasClientSecret ? "已设置，留空不修改" : ""}><Input type="password" value={draft.clientSecret} onChange={(e) => patch("clientSecret", e.target.value)} placeholder={oidc?.hasClientSecret ? "******（已设置）" : "输入密钥"} /></Row>
            </div>
            <Row label="Redirect URL" hint="后端 OIDC 回调地址"><Input value={draft.redirectURL} onChange={(e) => patch("redirectURL", e.target.value)} placeholder="http://localhost:8088/api/admin/auth/oidc/callback" /></Row>
            <Row label="前端 Callback URL" hint="OIDC 认证完成后跳转的前端地址"><Input value={draft.frontendCallbackURL} onChange={(e) => patch("frontendCallbackURL", e.target.value)} placeholder="http://localhost:3000/login/oidc-callback" /></Row>
          </AccordionContent>
        </AccordionItem>

        {/* 授权范围 */}
        <AccordionItem value="scopes" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Lock className="size-4 text-muted-foreground" /><span className="text-sm font-medium">授权范围</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <Row label="Scopes" hint="逗号或空格分隔"><Input value={draft.scopes} onChange={(e) => patch("scopes", e.target.value)} placeholder="openid profile email" /></Row>
          </AccordionContent>
        </AccordionItem>

        {/* 访问控制 */}
        <AccordionItem value="access" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Users className="size-4 text-muted-foreground" /><span className="text-sm font-medium">访问控制</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <Row label="允许的邮箱域名" hint="每行一个，为空则不限制"><Textarea value={draft.allowedDomains} onChange={(e) => patch("allowedDomains", e.target.value)} placeholder="example.com" rows={3} /></Row>
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="管理员组 Claim" hint="ID Token 中的 claim 字段名"><Input value={draft.adminGroupClaim} onChange={(e) => patch("adminGroupClaim", e.target.value)} placeholder="groups" /></Row>
              <Row label="管理员组值" hint="必须匹配的 claim 值"><Input value={draft.adminGroupValue} onChange={(e) => patch("adminGroupValue", e.target.value)} placeholder="aegis-admin" /></Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* 属性映射 */}
        <AccordionItem value="mapping" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Settings2 className="size-4 text-muted-foreground" /><span className="text-sm font-medium">属性映射</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="账号"><Input value={draft.attrAccount} onChange={(e) => patch("attrAccount", e.target.value)} placeholder="preferred_username" /></Row>
              <Row label="显示名称"><Input value={draft.attrDisplayName} onChange={(e) => patch("attrDisplayName", e.target.value)} placeholder="name" /></Row>
              <Row label="邮箱"><Input value={draft.attrEmail} onChange={(e) => patch("attrEmail", e.target.value)} placeholder="email" /></Row>
              <Row label="电话"><Input value={draft.attrPhone} onChange={(e) => patch("attrPhone", e.target.value)} placeholder="phone_number" /></Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* 回退策略 */}
        <AccordionItem value="fallback" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Shield className="size-4 text-muted-foreground" /><span className="text-sm font-medium">回退策略</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <SwitchRow label="OIDC 失败时回退本地密码" hint="启用后，OIDC 服务不可用时仍可使用本地密码登录"
              checked={draft.fallbackToLocal} onCheckedChange={(v) => patch("fallbackToLocal", v)} />
          </AccordionContent>
        </AccordionItem>

        {/* 连接测试 */}
        <AccordionItem value="test" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Search className="size-4 text-muted-foreground" /><span className="text-sm font-medium">连接测试</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <div className="flex items-end gap-2">
              <Button variant="outline" size="sm" onClick={handleTest} disabled={testing}>
                {testing ? <Loader2 className="size-3.5 animate-spin" /> : <Search className="size-3.5" />}
                测试 Discovery
              </Button>
            </div>
            {(testing || testResult) && (
              <div className="rounded-lg border bg-muted/30 p-3 space-y-2">
                <div className="flex items-center gap-2 text-xs">
                  {testResult?.discoveryOK === undefined && testing ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> :
                   testResult?.discoveryOK ? <CheckCircle2 className="size-3.5 text-emerald-600 dark:text-emerald-400" /> :
                   <XCircle className="size-3.5 text-red-600 dark:text-red-400" />}
                  <span>Discovery 文档</span>
                </div>
                {testResult?.discoveryOK && (
                  <div className="grid gap-1.5 text-[10px]">
                    <div><span className="text-muted-foreground">Issuer: </span><span className="font-mono">{testResult.issuer}</span></div>
                    <div><span className="text-muted-foreground">Auth: </span><span className="font-mono truncate">{testResult.authEndpoint}</span></div>
                    <div><span className="text-muted-foreground">Token: </span><span className="font-mono truncate">{testResult.tokenEndpoint}</span></div>
                    <div><span className="text-muted-foreground">UserInfo: </span><span className="font-mono truncate">{testResult.userInfoEndpoint}</span></div>
                    <div><span className="text-muted-foreground">JWKS: </span><span className="font-mono truncate">{testResult.jwksEndpoint}</span></div>
                    {testResult.supportedScopes?.length ? (
                      <div><span className="text-muted-foreground">Scopes: </span><span>{testResult.supportedScopes.join(", ")}</span></div>
                    ) : null}
                  </div>
                )}
                {testResult?.error && <p className="text-[10px] text-red-600 dark:text-red-400">{testResult.error}</p>}
                {testResult?.latencyMs !== undefined && <p className="text-[10px] text-muted-foreground">耗时 {testResult.latencyMs}ms</p>}
              </div>
            )}
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

function StatusCard({ icon: Icon, label, value, ok }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string; value: string; ok?: boolean;
}) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3 space-y-1">
      <div className="flex items-center gap-2 text-muted-foreground">
        <Icon className="size-3.5" />
        <span className="text-[10px] font-medium uppercase tracking-widest">{label}</span>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-sm font-semibold truncate">{value}</span>
        {ok !== undefined && <Badge variant={ok ? "success" : "outline"} className="text-[9px]">{ok ? "OK" : "--"}</Badge>}
      </div>
    </div>
  );
}
