"use client";

import { useState } from "react";
import {
  CheckCircle2,
  Copy,
  FileCode2,
  Fingerprint,
  Globe,
  KeyRound,
  Loader2,
  RotateCcw,
  Save,
  Search,
  Settings2,
  Shield,
  ShieldCheck,
  Users,
  XCircle
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { testSAMLMetadata } from "@/lib/api/system";
import { useAdminSystemSettingsQuery, useUpdateAdminSystemSettingsMutation } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import type { SAMLSettings, SAMLTestResult } from "@/lib/api/types";
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

/**
 * SAML 2.0 管理员单点登录配置。
 *
 * 后端的 saml_handlers / saml_dto / SAMLService 一直是完整的（authorize / metadata /
 * callback / exchange / test 五条路由都在），但控制台从来没有入口 ——
 * 只能靠 .env 或直接改数据库配。这个面板补上那个缺口。
 */

type Draft = {
  enabled: boolean;
  idpMetadataURL: string;
  idpMetadataXML: string;
  entityID: string;
  metadataURL: string;
  acsURL: string;
  spCertificate: string;
  spPrivateKey: string;
  nameIDFormat: string;
  signAuthnRequests: boolean;
  forceAuthn: boolean;
  allowIdpInitiated: boolean;
  allowedDomains: string;
  adminGroupAttribute: string;
  adminGroupValue: string;
  attrAccount: string;
  attrDisplayName: string;
  attrEmail: string;
  attrPhone: string;
  attrGroups: string;
  fallbackToLocal: boolean;
  frontendCallbackURL: string;
};

/** NameID 格式：SAML 规范里这几个是 IdP 侧真正常见的取值 */
const NAMEID_FORMATS: Array<{ value: string; label: string }> = [
  { value: "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified", label: "unspecified（默认，兼容性最好）" },
  { value: "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress", label: "emailAddress（邮箱）" },
  { value: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent", label: "persistent（IdP 侧稳定标识）" },
  { value: "urn:oasis:names:tc:SAML:2.0:nameid-format:transient", label: "transient（一次性标识）" }
];

function seedDraft(saml?: SAMLSettings): Draft {
  return {
    enabled: saml?.enabled ?? false,
    idpMetadataURL: saml?.idpMetadataURL ?? "",
    idpMetadataXML: "",
    entityID: saml?.entityID ?? "",
    metadataURL: saml?.metadataURL ?? "",
    acsURL: saml?.acsURL ?? "",
    spCertificate: saml?.spCertificate ?? "",
    spPrivateKey: "",
    nameIDFormat: saml?.nameIDFormat || NAMEID_FORMATS[0].value,
    signAuthnRequests: saml?.signAuthnRequests ?? false,
    forceAuthn: saml?.forceAuthn ?? false,
    allowIdpInitiated: saml?.allowIdpInitiated ?? false,
    allowedDomains: (saml?.allowedDomains ?? []).join("\n"),
    adminGroupAttribute: saml?.adminGroupAttribute ?? "",
    adminGroupValue: saml?.adminGroupValue ?? "",
    attrAccount: saml?.attrMapping?.account || "uid",
    attrDisplayName: saml?.attrMapping?.displayName || "displayName",
    attrEmail: saml?.attrMapping?.email || "email",
    attrPhone: saml?.attrMapping?.phone || "phone",
    attrGroups: saml?.attrMapping?.groups || "groups",
    fallbackToLocal: saml?.fallbackToLocal ?? true,
    frontendCallbackURL: saml?.frontendCallbackURL ?? ""
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

function SwitchRow({
  label,
  hint,
  checked,
  onCheckedChange
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
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

export function SAMLConfigPanel() {
  const settingsQuery = useAdminSystemSettingsQuery();
  const updateMutation = useUpdateAdminSystemSettingsMutation();
  const token = useAuthStore((s) => s.accessToken);
  const saml = settingsQuery.data?.saml;
  const [testResult, setTestResult] = useState<SAMLTestResult | null>(null);
  const [testing, setTesting] = useState(false);

  // 未编辑过时直接从服务端配置派生；「重置」把草稿清空即可回到服务端值。
  // 不用 useEffect 同步（会触发级联渲染，也过不了 react-hooks/set-state-in-effect）。
  const [localDraft, setLocalDraft] = useState<Draft | null>(null);
  const draft = localDraft ?? seedDraft(saml);

  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setLocalDraft({ ...draft, [key]: value });

  async function handleTest() {
    if (!token) return;
    if (!draft.idpMetadataURL.trim() && !draft.idpMetadataXML.trim()) {
      toast.error("请填写 IdP 元数据 URL 或粘贴元数据 XML");
      return;
    }
    setTesting(true);
    setTestResult(null);
    try {
      const result = await testSAMLMetadata(token, {
        idpMetadataURL: draft.idpMetadataURL.trim() || undefined,
        idpMetadataXML: draft.idpMetadataXML.trim() || undefined
      });
      setTestResult(result);
      if (result.metadataOK) toast.success(`元数据解析成功 (${result.latencyMs}ms)`);
      else toast.error(result.error || "元数据解析失败");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "测试请求失败");
    } finally {
      setTesting(false);
    }
  }

  async function handleSave() {
    try {
      const payload: Record<string, unknown> = {
        enabled: draft.enabled,
        idpMetadataURL: draft.idpMetadataURL.trim(),
        entityID: draft.entityID.trim(),
        metadataURL: draft.metadataURL.trim(),
        acsURL: draft.acsURL.trim(),
        spCertificate: draft.spCertificate.trim(),
        nameIDFormat: draft.nameIDFormat,
        signAuthnRequests: draft.signAuthnRequests,
        forceAuthn: draft.forceAuthn,
        allowIdpInitiated: draft.allowIdpInitiated,
        allowedDomains: draft.allowedDomains.split(/\r?\n|,/).map((s) => s.trim()).filter(Boolean),
        adminGroupAttribute: draft.adminGroupAttribute.trim(),
        adminGroupValue: draft.adminGroupValue.trim(),
        attrMapping: {
          account: draft.attrAccount.trim(),
          displayName: draft.attrDisplayName.trim(),
          email: draft.attrEmail.trim(),
          phone: draft.attrPhone.trim(),
          groups: draft.attrGroups.trim()
        },
        fallbackToLocal: draft.fallbackToLocal,
        frontendCallbackURL: draft.frontendCallbackURL.trim()
      };
      // 元数据 XML 与 SP 私钥留空即保持不变 —— 编辑态不回显，
      // 直接下发空串会把已配好的凭据清掉
      if (draft.idpMetadataXML.trim()) payload.idpMetadataXML = draft.idpMetadataXML.trim();
      if (draft.spPrivateKey.trim()) payload.spPrivateKey = draft.spPrivateKey.trim();
      await updateMutation.mutateAsync({ saml: payload as never });
      toast.success("SAML 配置已保存并热重载");
      // 清空草稿：重新从服务端派生，密钥/XML 输入框回到空白（两者都不回显）
      setLocalDraft(null);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "保存失败");
    }
  }

  async function copy(value: string, label: string) {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`${label}已复制`);
    } catch {
      toast.error("复制失败，请手动选择");
    }
  }

  if (settingsQuery.isLoading) return <LoadingState title="加载中" />;

  const idpSourceLabel = draft.idpMetadataURL
    ? "元数据 URL"
    : saml?.hasIdpMetadataXML || draft.idpMetadataXML
      ? "元数据 XML"
      : "--";

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatusCard icon={ShieldCheck} label="SAML 状态" value={draft.enabled ? "已启用" : "未启用"} ok={draft.enabled} />
        <StatusCard icon={FileCode2} label="IdP 元数据" value={idpSourceLabel} ok={idpSourceLabel !== "--"} />
        <StatusCard
          icon={KeyRound}
          label="SP 私钥"
          value={saml?.hasSpPrivateKey ? "已设置" : "未设置"}
          ok={saml?.hasSpPrivateKey}
        />
        <StatusCard icon={Shield} label="数据来源" value={saml?.source === "database" ? "数据库" : "未配置"} />
      </div>

      <Separator />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">SAML 2.0 认证配置</h3>
          <p className="text-xs text-muted-foreground">
            配置管理员单点登录（仅管理员层级；应用用户的第三方登录在「应用 → 第三方登录」）
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setLocalDraft(null)} disabled={updateMutation.isPending}>
            <RotateCcw className="size-3.5" /> 重置
          </Button>
          <Button size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      <Accordion type="multiple" defaultValue={["idp", "sp"]} className="space-y-2">
        <AccordionItem value="idp" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Globe className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">身份提供方（IdP）</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="space-y-4 px-4 pb-4">
            <SwitchRow
              label="启用 SAML 认证"
              hint="启用后登录页显示 SAML SSO 按钮"
              checked={draft.enabled}
              onCheckedChange={(v) => patch("enabled", v)}
            />
            <Row label="IdP 元数据 URL" hint="填 URL 时服务端会定期拉取；与下方 XML 二选一">
              <Input
                value={draft.idpMetadataURL}
                onChange={(e) => patch("idpMetadataURL", e.target.value)}
                placeholder="https://idp.example.com/app/exk.../sso/saml/metadata"
              />
            </Row>
            <Row
              label="IdP 元数据 XML"
              hint={
                saml?.hasIdpMetadataXML
                  ? "已保存 XML，留空表示不修改。IdP 不提供公开元数据 URL 时用这里粘贴。"
                  : "IdP 不提供公开元数据 URL 时，把 XML 全文粘在这里"
              }
            >
              <Textarea
                value={draft.idpMetadataXML}
                onChange={(e) => patch("idpMetadataXML", e.target.value)}
                rows={5}
                className="font-mono text-[10px]"
                placeholder={saml?.hasIdpMetadataXML ? "******（已保存，留空不修改）" : "<EntityDescriptor ...>"}
              />
            </Row>
            <Row label="NameID 格式" hint="必须与 IdP 侧配置一致，否则断言里取不到账号">
              <Select value={draft.nameIDFormat} onValueChange={(v) => patch("nameIDFormat", v)}>
                <SelectTrigger>
                  <SelectValue placeholder="选择 NameID 格式" />
                </SelectTrigger>
                <SelectContent>
                  {NAMEID_FORMATS.map((f) => (
                    <SelectItem key={f.value} value={f.value}>{f.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Row>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="sp" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Fingerprint className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">服务提供方（SP，即本平台）</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="space-y-4 px-4 pb-4">
            <div className="rounded-lg bg-muted p-3 text-[10px] leading-relaxed text-muted-foreground">
              下面三项要填进 IdP 的应用配置里。留空时后端会按 ACS / 元数据地址互相推导
              （<code className="font-mono">/callback</code> ↔ <code className="font-mono">/metadata</code>），Entity ID 默认取元数据地址。
            </div>
            <Row label="Entity ID" hint="本平台的 SAML 实体标识">
              <div className="flex gap-2">
                <Input value={draft.entityID} onChange={(e) => patch("entityID", e.target.value)} placeholder="http://localhost:8088/api/admin/auth/saml/metadata" />
                <Button variant="outline" size="sm" onClick={() => copy(draft.entityID, "Entity ID")} disabled={!draft.entityID}>
                  <Copy className="size-3.5" />
                </Button>
              </div>
            </Row>
            <Row label="ACS URL" hint="断言消费地址，IdP 认证后 POST 到这里">
              <div className="flex gap-2">
                <Input value={draft.acsURL} onChange={(e) => patch("acsURL", e.target.value)} placeholder="http://localhost:8088/api/admin/auth/saml/callback" />
                <Button variant="outline" size="sm" onClick={() => copy(draft.acsURL, "ACS URL")} disabled={!draft.acsURL}>
                  <Copy className="size-3.5" />
                </Button>
              </div>
            </Row>
            <Row label="SP 元数据 URL" hint="供 IdP 反向拉取本平台元数据">
              <div className="flex gap-2">
                <Input value={draft.metadataURL} onChange={(e) => patch("metadataURL", e.target.value)} placeholder="http://localhost:8088/api/admin/auth/saml/metadata" />
                <Button variant="outline" size="sm" onClick={() => copy(draft.metadataURL, "元数据 URL")} disabled={!draft.metadataURL}>
                  <Copy className="size-3.5" />
                </Button>
              </div>
            </Row>
            <Row label="前端 Callback URL" hint="SAML 认证完成后跳转的控制台地址">
              <Input
                value={draft.frontendCallbackURL}
                onChange={(e) => patch("frontendCallbackURL", e.target.value)}
                placeholder="http://localhost:3000/login/oidc-callback"
              />
            </Row>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="signing" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <KeyRound className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">请求签名</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="space-y-4 px-4 pb-4">
            <SwitchRow
              label="签名 AuthnRequest"
              hint="开启后必须同时配好 SP 证书与私钥，否则 IdP 会拒绝请求"
              checked={draft.signAuthnRequests}
              onCheckedChange={(v) => patch("signAuthnRequests", v)}
            />
            <SwitchRow
              label="强制重新认证（ForceAuthn）"
              hint="每次登录都要求 IdP 重新验证身份，忽略 IdP 侧已有会话"
              checked={draft.forceAuthn}
              onCheckedChange={(v) => patch("forceAuthn", v)}
            />
            <SwitchRow
              label="允许 IdP 发起的登录"
              hint="允许从 IdP 门户直接进入控制台（无 SP 侧 AuthnRequest）"
              checked={draft.allowIdpInitiated}
              onCheckedChange={(v) => patch("allowIdpInitiated", v)}
            />
            {draft.signAuthnRequests && !draft.spCertificate.trim() && !saml?.hasSpPrivateKey ? (
              <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-2.5 text-[10px] text-amber-700 dark:text-amber-400">
                已开启签名但尚未配置 SP 证书与私钥，保存后 SAML 登录会失败。
              </div>
            ) : null}
            <Row label="SP 证书（PEM）" hint="公钥证书，同时提供给 IdP 验签">
              <Textarea
                value={draft.spCertificate}
                onChange={(e) => patch("spCertificate", e.target.value)}
                rows={4}
                className="font-mono text-[10px]"
                placeholder="-----BEGIN CERTIFICATE-----"
              />
            </Row>
            <Row
              label="SP 私钥（PEM）"
              hint={saml?.hasSpPrivateKey ? "已设置（AES-GCM 加密存储），留空不修改" : "AES-GCM 加密存储，永不回显"}
            >
              <Textarea
                value={draft.spPrivateKey}
                onChange={(e) => patch("spPrivateKey", e.target.value)}
                rows={4}
                className="font-mono text-[10px]"
                placeholder={saml?.hasSpPrivateKey ? "******（已设置，留空不修改）" : "-----BEGIN PRIVATE KEY-----"}
              />
            </Row>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="access" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Users className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">访问控制</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="space-y-4 px-4 pb-4">
            <Row label="允许的邮箱域名" hint="每行一个，为空则不限制">
              <Textarea
                value={draft.allowedDomains}
                onChange={(e) => patch("allowedDomains", e.target.value)}
                rows={3}
                placeholder="example.com"
              />
            </Row>
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="管理员组属性名" hint="断言中承载组信息的属性">
                <Input value={draft.adminGroupAttribute} onChange={(e) => patch("adminGroupAttribute", e.target.value)} placeholder="groups" />
              </Row>
              <Row label="管理员组值" hint="必须匹配的属性值">
                <Input value={draft.adminGroupValue} onChange={(e) => patch("adminGroupValue", e.target.value)} placeholder="aegis-admin" />
              </Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="mapping" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Settings2 className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">属性映射</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="账号"><Input value={draft.attrAccount} onChange={(e) => patch("attrAccount", e.target.value)} placeholder="uid" /></Row>
              <Row label="显示名称"><Input value={draft.attrDisplayName} onChange={(e) => patch("attrDisplayName", e.target.value)} placeholder="displayName" /></Row>
              <Row label="邮箱"><Input value={draft.attrEmail} onChange={(e) => patch("attrEmail", e.target.value)} placeholder="email" /></Row>
              <Row label="电话"><Input value={draft.attrPhone} onChange={(e) => patch("attrPhone", e.target.value)} placeholder="phone" /></Row>
              <Row label="用户组"><Input value={draft.attrGroups} onChange={(e) => patch("attrGroups", e.target.value)} placeholder="groups" /></Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="fallback" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Shield className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">回退策略</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <SwitchRow
              label="SAML 失败时回退本地密码"
              hint="关闭后 IdP 不可用将没有任何登录入口 —— 除非你确实需要强制 SSO，否则保持开启"
              checked={draft.fallbackToLocal}
              onCheckedChange={(v) => patch("fallbackToLocal", v)}
            />
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="test" className="overflow-hidden rounded-xl border border-b-0">
          <AccordionTrigger className="px-4 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Search className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">元数据解析测试</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="space-y-4 px-4 pb-4">
            <Button variant="outline" size="sm" onClick={handleTest} disabled={testing}>
              {testing ? <Loader2 className="size-3.5 animate-spin" /> : <Search className="size-3.5" />}
              解析 IdP 元数据
            </Button>
            <p className="text-[10px] text-muted-foreground">
              用当前表单里的 URL / XML 解析，不依赖已保存的配置 —— 保存前就能确认 IdP 侧填对了。
            </p>
            {(testing || testResult) && (
              <div className="space-y-2 rounded-lg border bg-muted/30 p-3">
                <div className="flex items-center gap-2 text-xs">
                  {testResult === null && testing ? (
                    <Loader2 className="size-3.5 animate-spin text-muted-foreground" />
                  ) : testResult?.metadataOK ? (
                    <CheckCircle2 className="size-3.5 text-emerald-600 dark:text-emerald-400" />
                  ) : (
                    <XCircle className="size-3.5 text-red-600 dark:text-red-400" />
                  )}
                  <span>IdP 元数据</span>
                </div>
                {testResult?.metadataOK && (
                  <div className="grid gap-1.5 text-[10px]">
                    <div><span className="text-muted-foreground">Entity ID: </span><span className="font-mono">{testResult.entityID}</span></div>
                    {testResult.ssoRedirectURL ? (
                      <div><span className="text-muted-foreground">SSO Redirect: </span><span className="truncate font-mono">{testResult.ssoRedirectURL}</span></div>
                    ) : null}
                    {testResult.ssoPostURL ? (
                      <div><span className="text-muted-foreground">SSO POST: </span><span className="truncate font-mono">{testResult.ssoPostURL}</span></div>
                    ) : null}
                    {testResult.singleLogoutURL ? (
                      <div><span className="text-muted-foreground">SLO: </span><span className="truncate font-mono">{testResult.singleLogoutURL}</span></div>
                    ) : null}
                    <div><span className="text-muted-foreground">签名证书: </span><span className="font-mono">{testResult.certificateCount} 份</span></div>
                    {testResult.supportedNameIdFormats?.length ? (
                      <div className="space-y-1">
                        <span className="text-muted-foreground">支持的 NameID：</span>
                        <div className="flex flex-wrap gap-1">
                          {testResult.supportedNameIdFormats.map((f) => (
                            <Badge
                              key={f}
                              variant={f === draft.nameIDFormat ? "success" : "outline"}
                              className="text-[9px]"
                            >
                              {f.split(":").pop()}
                            </Badge>
                          ))}
                        </div>
                      </div>
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

function StatusCard({
  icon: Icon,
  label,
  value,
  ok
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  ok?: boolean;
}) {
  return (
    <div className="space-y-1 rounded-xl border bg-card px-4 py-3">
      <div className="flex items-center gap-2 text-muted-foreground">
        <Icon className="size-3.5" />
        <span className="text-[10px] font-medium uppercase tracking-widest">{label}</span>
      </div>
      <div className="flex items-center gap-2">
        <span className="truncate text-sm font-semibold">{value}</span>
        {ok !== undefined && <Badge variant={ok ? "success" : "outline"} className="text-[9px]">{ok ? "OK" : "--"}</Badge>}
      </div>
    </div>
  );
}
