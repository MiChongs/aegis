"use client";

import { useEffect, useState } from "react";
import {
  CheckCircle2, Database, Key, Loader2, Lock, Network,
  RotateCcw, Save, Search, Settings2, Shield, Users, XCircle
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { testLDAPConnection } from "@/lib/api/system";
import { useAdminSystemSettingsQuery, useUpdateAdminSystemSettingsMutation } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import type { LDAPSettings, LDAPTestResult } from "@/lib/api/types";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/data-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";

type Draft = {
  enabled: boolean;
  server: string;
  port: string;
  useTLS: boolean;
  useStartTLS: boolean;
  skipTLSVerify: boolean;
  bindDN: string;
  bindPassword: string;
  baseDN: string;
  userFilter: string;
  userFilterPreset: string;
  userAttribute: string;
  groupBaseDN: string;
  groupFilter: string;
  groupAttribute: string;
  adminGroupDN: string;
  attrAccount: string;
  attrDisplayName: string;
  attrEmail: string;
  attrPhone: string;
  connectionTimeoutSeconds: string;
  searchTimeoutSeconds: string;
  fallbackToLocal: boolean;
};

const FILTER_PRESETS: Record<string, { filter: string; attr: string; label: string }> = {
  ad: { filter: "(sAMAccountName=%s)", attr: "sAMAccountName", label: "Active Directory" },
  openldap: { filter: "(uid=%s)", attr: "uid", label: "OpenLDAP" },
  mail: { filter: "(mail=%s)", attr: "mail", label: "Email" },
  custom: { filter: "", attr: "", label: "Custom" },
};

function seedDraft(ldap?: LDAPSettings): Draft {
  const preset = Object.entries(FILTER_PRESETS).find(
    ([, v]) => v.filter === (ldap?.userFilter || "(sAMAccountName=%s)")
  );
  return {
    enabled: ldap?.enabled ?? false,
    server: ldap?.server ?? "",
    port: String(ldap?.port || 389),
    useTLS: ldap?.useTLS ?? false,
    useStartTLS: ldap?.useStartTLS ?? false,
    skipTLSVerify: ldap?.skipTLSVerify ?? false,
    bindDN: ldap?.bindDN ?? "",
    bindPassword: "",
    baseDN: ldap?.baseDN ?? "",
    userFilter: ldap?.userFilter ?? "(sAMAccountName=%s)",
    userFilterPreset: preset?.[0] ?? "custom",
    userAttribute: ldap?.userAttribute ?? "sAMAccountName",
    groupBaseDN: ldap?.groupBaseDN ?? "",
    groupFilter: ldap?.groupFilter ?? "(member=%s)",
    groupAttribute: ldap?.groupAttribute ?? "cn",
    adminGroupDN: ldap?.adminGroupDN ?? "",
    attrAccount: ldap?.attrMapping?.account ?? "sAMAccountName",
    attrDisplayName: ldap?.attrMapping?.displayName ?? "displayName",
    attrEmail: ldap?.attrMapping?.email ?? "mail",
    attrPhone: ldap?.attrMapping?.phone ?? "telephoneNumber",
    connectionTimeoutSeconds: String(ldap?.connectionTimeoutSeconds || 10),
    searchTimeoutSeconds: String(ldap?.searchTimeoutSeconds || 15),
    fallbackToLocal: ldap?.fallbackToLocal ?? true,
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

function TestStep({ label, ok }: { label: string; ok: boolean | null }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      {ok === null ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> :
       ok ? <CheckCircle2 className="size-3.5 text-emerald-600 dark:text-emerald-400" /> :
       <XCircle className="size-3.5 text-red-600 dark:text-red-400" />}
      <span className={ok === false ? "text-red-600 dark:text-red-400" : ""}>{label}</span>
    </div>
  );
}

export function LDAPConfigPanel() {
  const settingsQuery = useAdminSystemSettingsQuery();
  const updateMutation = useUpdateAdminSystemSettingsMutation();
  const token = useAuthStore((s) => s.accessToken);
  const ldap = settingsQuery.data?.ldap;
  const [draft, setDraft] = useState<Draft>(() => seedDraft(ldap));
  const [testResult, setTestResult] = useState<LDAPTestResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [testAccount, setTestAccount] = useState("");

  useEffect(() => {
    if (ldap) setDraft(seedDraft(ldap));
  }, [ldap]);

  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));

  const handlePresetChange = (preset: string) => {
    const p = FILTER_PRESETS[preset];
    if (!p) return;
    patch("userFilterPreset", preset);
    if (preset !== "custom") {
      patch("userFilter", p.filter);
      patch("userAttribute", p.attr);
    }
  };

  const handleTest = async () => {
    if (!token || !draft.server || !draft.bindDN || !draft.baseDN) {
      toast.error("请填写服务器、绑定 DN 和 Base DN");
      return;
    }
    const pwd = draft.bindPassword || (ldap?.hasBindPassword ? "__use_saved__" : "");
    if (!pwd) {
      toast.error("请输入绑定密码");
      return;
    }
    setTesting(true);
    setTestResult(null);
    try {
      const result = await testLDAPConnection(token, {
        server: draft.server,
        port: Number(draft.port) || 389,
        useTLS: draft.useTLS,
        useStartTLS: draft.useStartTLS,
        skipTLSVerify: draft.skipTLSVerify,
        bindDN: draft.bindDN,
        bindPassword: pwd,
        baseDN: draft.baseDN,
        userFilter: draft.userFilter || "(sAMAccountName=%s)",
        testAccount: testAccount.trim(),
        connectionTimeoutSeconds: Number(draft.connectionTimeoutSeconds) || 10,
      });
      setTestResult(result);
      if (result.searchOK) toast.success(`连接成功 (${result.latencyMs}ms)`);
      else toast.error(result.error || "连接测试未完全通过");
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
        server: draft.server.trim(),
        port: Number(draft.port) || 389,
        useTLS: draft.useTLS,
        useStartTLS: draft.useStartTLS,
        skipTLSVerify: draft.skipTLSVerify,
        bindDN: draft.bindDN.trim(),
        baseDN: draft.baseDN.trim(),
        userFilter: draft.userFilter.trim(),
        userAttribute: draft.userAttribute.trim(),
        groupBaseDN: draft.groupBaseDN.trim(),
        groupFilter: draft.groupFilter.trim(),
        groupAttribute: draft.groupAttribute.trim(),
        adminGroupDN: draft.adminGroupDN.trim(),
        attrMapping: {
          account: draft.attrAccount.trim(),
          displayName: draft.attrDisplayName.trim(),
          email: draft.attrEmail.trim(),
          phone: draft.attrPhone.trim(),
        },
        connectionTimeoutSeconds: Number(draft.connectionTimeoutSeconds) || 10,
        searchTimeoutSeconds: Number(draft.searchTimeoutSeconds) || 15,
        fallbackToLocal: draft.fallbackToLocal,
      };
      if (draft.bindPassword) payload.bindPassword = draft.bindPassword;
      await updateMutation.mutateAsync({ ldap: payload as never });
      toast.success("LDAP 配置已保存并热重载");
      patch("bindPassword", "");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading) return <LoadingState title="加载中" />;

  return (
    <div className="space-y-5">
      {/* 状态概览 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard icon={Network} label="LDAP 状态" value={draft.enabled ? "已启用" : "未启用"} ok={draft.enabled} />
        <StatusCard icon={Database} label="服务器" value={draft.server || "--"} />
        <StatusCard icon={Key} label="绑定密码" value={ldap?.hasBindPassword ? "已设置" : "未设置"} ok={ldap?.hasBindPassword} />
        <StatusCard icon={Shield} label="数据来源" value={ldap?.source === "database" ? "数据库" : "未配置"} />
      </div>

      <Separator />

      {/* 操作栏 */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h3 className="text-sm font-semibold">LDAP 认证配置</h3>
          <p className="text-xs text-muted-foreground">配置 LDAP/Active Directory 统一身份认证（仅管理员层级）</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setDraft(seedDraft(ldap))} disabled={updateMutation.isPending}>
            <RotateCcw className="size-3.5" /> 重置
          </Button>
          <Button size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      <Accordion type="multiple" defaultValue={["connection"]} className="space-y-2">
        {/* 基础连接 */}
        <AccordionItem value="connection" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Network className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">基础连接</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <SwitchRow label="启用 LDAP 认证" hint="启用后管理员登录优先使用 LDAP 验证" checked={draft.enabled} onCheckedChange={(v) => patch("enabled", v)} />
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="服务器地址"><Input value={draft.server} onChange={(e) => patch("server", e.target.value)} placeholder="ldap.example.com" /></Row>
              <Row label="端口"><Input value={draft.port} onChange={(e) => patch("port", e.target.value.replace(/\D/g, ""))} placeholder="389" /></Row>
            </div>
            <Row label="连接超时（秒）"><Input value={draft.connectionTimeoutSeconds} onChange={(e) => patch("connectionTimeoutSeconds", e.target.value.replace(/\D/g, ""))} placeholder="10" className="w-32" /></Row>
          </AccordionContent>
        </AccordionItem>

        {/* TLS */}
        <AccordionItem value="tls" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Lock className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">TLS 安全</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-3">
            <SwitchRow label="LDAPS (TLS)" hint="使用 ldaps:// 加密连接（端口 636）" checked={draft.useTLS} onCheckedChange={(v) => patch("useTLS", v)} />
            <SwitchRow label="StartTLS" hint="在普通连接上升级为 TLS（与 LDAPS 互斥）" checked={draft.useStartTLS} onCheckedChange={(v) => patch("useStartTLS", v)} />
            <SwitchRow label="跳过证书验证" hint="仅用于开发环境，生产环境请使用有效证书" checked={draft.skipTLSVerify} onCheckedChange={(v) => patch("skipTLSVerify", v)} />
          </AccordionContent>
        </AccordionItem>

        {/* 绑定认证 + 测试 */}
        <AccordionItem value="bind" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Key className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">绑定认证</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <Row label="绑定 DN" hint="用于搜索用户的服务账号"><Input value={draft.bindDN} onChange={(e) => patch("bindDN", e.target.value)} placeholder="cn=admin,dc=example,dc=com" /></Row>
            <Row label="绑定密码" hint={ldap?.hasBindPassword ? "已设置，留空不修改" : ""}><Input type="password" value={draft.bindPassword} onChange={(e) => patch("bindPassword", e.target.value)} placeholder={ldap?.hasBindPassword ? "******（已设置）" : "输入密码"} /></Row>
            <Separator />
            <div className="space-y-3">
              <div className="flex items-end gap-2">
                <Row label="测试用户（可选）"><Input value={testAccount} onChange={(e) => setTestAccount(e.target.value)} placeholder="admin" className="w-48" /></Row>
                <Button variant="outline" size="sm" onClick={handleTest} disabled={testing}>
                  {testing ? <Loader2 className="size-3.5 animate-spin" /> : <Search className="size-3.5" />}
                  测试连接
                </Button>
              </div>
              {(testing || testResult) && (
                <div className="rounded-lg border bg-muted/30 p-3 space-y-1.5">
                  <TestStep label="TCP 连接" ok={testing && !testResult ? null : testResult?.connected ?? false} />
                  <TestStep label="LDAP 绑定" ok={testing && !testResult ? null : testResult?.bindSuccess ?? false} />
                  <TestStep label="BaseDN 搜索" ok={testing && !testResult ? null : testResult?.searchOK ?? false} />
                  {testAccount && <TestStep label={`用户搜索: ${testResult?.userDN || testAccount}`} ok={testing && !testResult ? null : testResult?.userFound ?? false} />}
                  {testResult?.error && <p className="text-[10px] text-red-600 dark:text-red-400 mt-1">{testResult.error}</p>}
                  {testResult?.latencyMs !== undefined && <p className="text-[10px] text-muted-foreground">耗时 {testResult.latencyMs}ms</p>}
                </div>
              )}
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* 用户搜索 */}
        <AccordionItem value="search" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Search className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">用户搜索</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <Row label="Base DN" hint="用户搜索的根目录"><Input value={draft.baseDN} onChange={(e) => patch("baseDN", e.target.value)} placeholder="dc=example,dc=com" /></Row>
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="过滤器预设">
                <Select value={draft.userFilterPreset} onValueChange={handlePresetChange}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {Object.entries(FILTER_PRESETS).map(([k, v]) => <SelectItem key={k} value={k}>{v.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Row>
              <Row label="用户属性"><Input value={draft.userAttribute} onChange={(e) => patch("userAttribute", e.target.value)} placeholder="sAMAccountName" /></Row>
            </div>
            {draft.userFilterPreset === "custom" && (
              <Row label="自定义过滤器" hint="%s 将替换为用户名"><Input value={draft.userFilter} onChange={(e) => patch("userFilter", e.target.value)} placeholder="(uid=%s)" /></Row>
            )}
            <Row label="搜索超时（秒）"><Input value={draft.searchTimeoutSeconds} onChange={(e) => patch("searchTimeoutSeconds", e.target.value.replace(/\D/g, ""))} placeholder="15" className="w-32" /></Row>
          </AccordionContent>
        </AccordionItem>

        {/* 组映射 */}
        <AccordionItem value="group" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Users className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">组映射（可选）</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <Row label="组 Base DN"><Input value={draft.groupBaseDN} onChange={(e) => patch("groupBaseDN", e.target.value)} placeholder="ou=groups,dc=example,dc=com" /></Row>
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="组过滤器" hint="%s 替换为用户 DN"><Input value={draft.groupFilter} onChange={(e) => patch("groupFilter", e.target.value)} placeholder="(member=%s)" /></Row>
              <Row label="组属性"><Input value={draft.groupAttribute} onChange={(e) => patch("groupAttribute", e.target.value)} placeholder="cn" /></Row>
            </div>
            <Row label="管理员组 DN" hint="仅此组成员可登录管理台"><Input value={draft.adminGroupDN} onChange={(e) => patch("adminGroupDN", e.target.value)} placeholder="cn=admins,ou=groups,dc=example,dc=com" /></Row>
          </AccordionContent>
        </AccordionItem>

        {/* 属性映射 */}
        <AccordionItem value="mapping" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Settings2 className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">属性映射</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="账号"><Input value={draft.attrAccount} onChange={(e) => patch("attrAccount", e.target.value)} placeholder="sAMAccountName" /></Row>
              <Row label="显示名称"><Input value={draft.attrDisplayName} onChange={(e) => patch("attrDisplayName", e.target.value)} placeholder="displayName" /></Row>
              <Row label="邮箱"><Input value={draft.attrEmail} onChange={(e) => patch("attrEmail", e.target.value)} placeholder="mail" /></Row>
              <Row label="电话"><Input value={draft.attrPhone} onChange={(e) => patch("attrPhone", e.target.value)} placeholder="telephoneNumber" /></Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* 回退策略 */}
        <AccordionItem value="fallback" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2">
              <Shield className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">回退策略</span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4">
            <SwitchRow
              label="LDAP 失败时回退本地密码"
              hint="启用后，当 LDAP 服务不可用或未找到用户时，将尝试本地密码验证"
              checked={draft.fallbackToLocal}
              onCheckedChange={(v) => patch("fallbackToLocal", v)}
            />
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

function StatusCard({ icon: Icon, label, value, ok }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  ok?: boolean;
}) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3 space-y-1">
      <div className="flex items-center gap-2 text-muted-foreground">
        <Icon className="size-3.5" />
        <span className="text-[10px] font-medium uppercase tracking-widest">{label}</span>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-sm font-semibold">{value}</span>
        {ok !== undefined && (
          <Badge variant={ok ? "success" : "outline"} className="text-[9px]">
            {ok ? "OK" : "--"}
          </Badge>
        )}
      </div>
    </div>
  );
}
