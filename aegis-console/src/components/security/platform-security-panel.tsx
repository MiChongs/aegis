"use client";

import { useEffect, useMemo, useState } from "react";
import { KeyRound, Fingerprint, ShieldCheck, RotateCcw, Save, Clock, RefreshCw, Database } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { useAdminSystemSettingsQuery, useUpdateAdminSystemSettingsMutation } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";

// ── 表单状态 ──

type Draft = {
  challengeTTLSeconds: string;
  totpEnabled: boolean;
  recoveryCodesEnabled: boolean;
  passkeyEnabled: boolean;
  totpRuntimeEnabled: boolean;
  totpIssuer: string;
  totpEnrollmentTTLSeconds: string;
  totpSkew: string;
  totpDigits: string;
  recoveryEnabled: boolean;
  recoveryCount: string;
  recoveryLength: string;
  passkeyRuntimeEnabled: boolean;
  passkeyDisplayName: string;
  passkeyRPID: string;
  passkeyOrigins: string;
  passkeyTopOrigins: string;
  passkeyChallengeTTLSeconds: string;
  passkeyUserVerification: string;
};

function listToText(v?: string[] | null) { return (v || []).join("\n"); }
function textToList(v: string) { return v.split(/\r?\n|,/).map(s => s.trim()).filter(Boolean); }

function makeSeed(s?: {
  challengeTTLSeconds?: number;
  modules?: { totpEnabled?: boolean; recoveryCodesEnabled?: boolean; passkeyEnabled?: boolean };
  totp?: { enabled?: boolean; issuer?: string; enrollmentTTLSeconds?: number; skew?: number; digits?: number };
  recoveryCodes?: { enabled?: boolean; count?: number; length?: number };
  passkey?: { enabled?: boolean; rpDisplayName?: string; rpId?: string; rpOrigins?: string[]; rpTopOrigins?: string[]; challengeTTLSeconds?: number; userVerification?: string };
}): Draft {
  return {
    challengeTTLSeconds: String(s?.challengeTTLSeconds || 300),
    totpEnabled: s?.modules?.totpEnabled !== false,
    recoveryCodesEnabled: s?.modules?.recoveryCodesEnabled !== false,
    passkeyEnabled: s?.modules?.passkeyEnabled !== false,
    totpRuntimeEnabled: s?.totp?.enabled !== false,
    totpIssuer: s?.totp?.issuer || "",
    totpEnrollmentTTLSeconds: String(s?.totp?.enrollmentTTLSeconds || 600),
    totpSkew: String(s?.totp?.skew || 1),
    totpDigits: String(s?.totp?.digits || 6),
    recoveryEnabled: s?.recoveryCodes?.enabled !== false,
    recoveryCount: String(s?.recoveryCodes?.count || 10),
    recoveryLength: String(s?.recoveryCodes?.length || 12),
    passkeyRuntimeEnabled: s?.passkey?.enabled !== false,
    passkeyDisplayName: s?.passkey?.rpDisplayName || "",
    passkeyRPID: s?.passkey?.rpId || "",
    passkeyOrigins: listToText(s?.passkey?.rpOrigins),
    passkeyTopOrigins: listToText(s?.passkey?.rpTopOrigins),
    passkeyChallengeTTLSeconds: String(s?.passkey?.challengeTTLSeconds || 300),
    passkeyUserVerification: s?.passkey?.userVerification || "preferred",
  };
}

function fmtDate(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

// ── 组件 ──

export function PlatformSecurityPanel() {
  const operator = useAuthStore((s) => s.operator);
  const settingsQuery = useAdminSystemSettingsQuery(Boolean(operator?.isSuperAdmin));
  const updateMutation = useUpdateAdminSystemSettingsMutation();
  const security = settingsQuery.data?.security;
  const seed = useMemo(() => security ? makeSeed(security) : null, [security]);
  const seedKey = useMemo(() => seed ? JSON.stringify(seed) : "", [seed]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [syncedKey, setSyncedKey] = useState("");

  useEffect(() => {
    if (!seed || seedKey === syncedKey) return;
    setDraft(seed);
    setSyncedKey(seedKey);
  }, [seed, seedKey, syncedKey]);

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft(s => s ? { ...s, [k]: v } : s);

  async function handleSave() {
    if (!draft) return;
    try {
      await updateMutation.mutateAsync({
        security: {
          challengeTTLSeconds: Number(draft.challengeTTLSeconds || 0),
          modules: { totpEnabled: draft.totpEnabled, recoveryCodesEnabled: draft.recoveryCodesEnabled, passkeyEnabled: draft.passkeyEnabled },
          totp: { enabled: draft.totpRuntimeEnabled, issuer: draft.totpIssuer.trim(), enrollmentTTLSeconds: Number(draft.totpEnrollmentTTLSeconds || 0), skew: Number(draft.totpSkew || 0), digits: Number(draft.totpDigits || 0) },
          recoveryCodes: { enabled: draft.recoveryEnabled, count: Number(draft.recoveryCount || 0), length: Number(draft.recoveryLength || 0) },
          passkey: { enabled: draft.passkeyRuntimeEnabled, rpDisplayName: draft.passkeyDisplayName.trim(), rpId: draft.passkeyRPID.trim(), rpOrigins: textToList(draft.passkeyOrigins), rpTopOrigins: textToList(draft.passkeyTopOrigins), challengeTTLSeconds: Number(draft.passkeyChallengeTTLSeconds || 0), userVerification: draft.passkeyUserVerification },
        },
      });
      toast.success("平台安全策略已更新");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "更新失败");
    }
  }

  if (!operator?.isSuperAdmin) return <EmptyState title="无访问权限" description="仅超级管理员可调整平台安全策略。" />;
  if (settingsQuery.isLoading || !draft || !security) return <LoadingState title="加载安全配置" />;

  return (
    <div className="space-y-6">
      {/* ── 状态概览 ── */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard icon={ShieldCheck} label="主密钥" value={security.masterKeyConfigured ? "已配置" : "缺失"} ok={security.masterKeyConfigured} />
        <StatusCard icon={Database} label="数据来源" value={security.source === "database" ? "数据库" : "环境变量"} ok />
        <StatusCard icon={Clock} label="挑战时效" value={`${security.challengeTTLSeconds}s`} ok />
        <StatusCard icon={RefreshCw} label="热重载" value={`v${security.reloadVersion}`} sub={fmtDate(security.reloadedAt)} ok />
      </div>

      {/* ── 运行时模块状态 ── */}
      {security.runtimeModules?.length ? (
        <div className="grid gap-3 sm:grid-cols-3">
          {security.runtimeModules.map((m) => (
            <div key={m.key} className="flex items-center justify-between rounded-xl border px-4 py-3">
              <div>
                <div className="text-sm font-medium">{m.name}</div>
                <div className="text-xs text-muted-foreground">{m.message || "—"}</div>
              </div>
              <Badge variant={m.enabled && m.ready ? "success" : m.enabled ? "warning" : "outline"}>
                {m.enabled ? (m.ready ? "就绪" : "异常") : "关闭"}
              </Badge>
            </div>
          ))}
        </div>
      ) : null}

      <Separator />

      {/* ── 配置表单 ── */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">安全策略配置</h2>
          <p className="text-sm text-muted-foreground">保存后立即生效</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => { setDraft(seed); setSyncedKey(seedKey); }} disabled={updateMutation.isPending}>
            <RotateCcw className="size-3.5" /> 重置
          </Button>
          <Button size="sm" onClick={() => void handleSave()} disabled={updateMutation.isPending}>
            <Save className="size-3.5" /> {updateMutation.isPending ? "保存中..." : "保存"}
          </Button>
        </div>
      </div>

      {/* 全局设置 */}
      <Row label="全局挑战时效 (秒)">
        <Input className="h-8 w-28 text-sm" value={draft.challengeTTLSeconds} onChange={(e) => set("challengeTTLSeconds", e.target.value)} />
      </Row>

      {/* 模块开关 */}
      <div className="grid gap-3 sm:grid-cols-3">
        <SwitchRow label="TOTP 模块" checked={draft.totpEnabled} onCheckedChange={(v) => set("totpEnabled", v)} />
        <SwitchRow label="恢复码模块" checked={draft.recoveryCodesEnabled} onCheckedChange={(v) => set("recoveryCodesEnabled", v)} />
        <SwitchRow label="Passkey 模块" checked={draft.passkeyEnabled} onCheckedChange={(v) => set("passkeyEnabled", v)} />
      </div>

      {/* ── 详细配置 Accordion ── */}
      <Accordion type="multiple" defaultValue={["totp", "recovery", "passkey"]} className="space-y-3">
        {/* TOTP */}
        <AccordionItem value="totp" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            <span className="flex items-center gap-2"><KeyRound className="size-4 text-muted-foreground" /> TOTP 动态口令</span>
          </AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <SwitchRow label="启用运行时" checked={draft.totpRuntimeEnabled} onCheckedChange={(v) => set("totpRuntimeEnabled", v)} />
            <Row label="Issuer">
              <Input className="h-8 text-sm" placeholder="应用名称" value={draft.totpIssuer} onChange={(e) => set("totpIssuer", e.target.value)} />
            </Row>
            <div className="grid gap-3 sm:grid-cols-3">
              <Row label="配置时效 (秒)">
                <Input className="h-8 text-sm" value={draft.totpEnrollmentTTLSeconds} onChange={(e) => set("totpEnrollmentTTLSeconds", e.target.value)} />
              </Row>
              <Row label="Skew">
                <Input className="h-8 text-sm" value={draft.totpSkew} onChange={(e) => set("totpSkew", e.target.value)} />
              </Row>
              <Row label="位数">
                <Select value={draft.totpDigits} onValueChange={(v) => set("totpDigits", v)}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="6">6 位</SelectItem>
                    <SelectItem value="8">8 位</SelectItem>
                  </SelectContent>
                </Select>
              </Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* 恢复码 */}
        <AccordionItem value="recovery" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            <span className="flex items-center gap-2"><ShieldCheck className="size-4 text-muted-foreground" /> 恢复码</span>
          </AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <SwitchRow label="启用运行时" checked={draft.recoveryEnabled} onCheckedChange={(v) => set("recoveryEnabled", v)} />
            <div className="grid gap-3 sm:grid-cols-2">
              <Row label="生成数量">
                <Input className="h-8 text-sm" value={draft.recoveryCount} onChange={(e) => set("recoveryCount", e.target.value)} />
              </Row>
              <Row label="每码长度">
                <Input className="h-8 text-sm" value={draft.recoveryLength} onChange={(e) => set("recoveryLength", e.target.value)} />
              </Row>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Passkey */}
        <AccordionItem value="passkey" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            <span className="flex items-center gap-2"><Fingerprint className="size-4 text-muted-foreground" /> Passkey / WebAuthn</span>
          </AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <SwitchRow label="启用运行时" checked={draft.passkeyRuntimeEnabled} onCheckedChange={(v) => set("passkeyRuntimeEnabled", v)} />
            <div className="grid gap-3 sm:grid-cols-2">
              <Row label="RP Display Name">
                <Input className="h-8 text-sm" placeholder="Aegis" value={draft.passkeyDisplayName} onChange={(e) => set("passkeyDisplayName", e.target.value)} />
              </Row>
              <Row label="RP ID">
                <Input className="h-8 text-sm" placeholder="example.com" value={draft.passkeyRPID} onChange={(e) => set("passkeyRPID", e.target.value)} />
              </Row>
            </div>
            <Row label="来源 Origins（每行一个）">
              <Textarea className="text-sm" rows={3} placeholder="https://example.com" value={draft.passkeyOrigins} onChange={(e) => set("passkeyOrigins", e.target.value)} />
            </Row>
            <Row label="Top Origins（每行一个）">
              <Textarea className="text-sm" rows={2} value={draft.passkeyTopOrigins} onChange={(e) => set("passkeyTopOrigins", e.target.value)} />
            </Row>
            <div className="grid gap-3 sm:grid-cols-2">
              <Row label="挑战时效 (秒)">
                <Input className="h-8 text-sm" value={draft.passkeyChallengeTTLSeconds} onChange={(e) => set("passkeyChallengeTTLSeconds", e.target.value)} />
              </Row>
              <Row label="用户验证">
                <Select value={draft.passkeyUserVerification} onValueChange={(v) => set("passkeyUserVerification", v)}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="preferred">preferred</SelectItem>
                    <SelectItem value="required">required</SelectItem>
                    <SelectItem value="discouraged">discouraged</SelectItem>
                  </SelectContent>
                </Select>
              </Row>
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

// ── 辅助组件 ──

function StatusCard({ icon: Icon, label, value, sub, ok }: {
  icon: React.ComponentType<{ className?: string }>; label: string; value: string; sub?: string; ok?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
      <Icon className={`size-5 shrink-0 ${ok ? "text-emerald-500" : "text-amber-500"}`} />
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-sm font-semibold truncate">{value}</div>
        {sub && <div className="text-[10px] text-muted-foreground">{sub}</div>}
      </div>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

function SwitchRow({ label, checked, onCheckedChange }: { label: string; checked: boolean; onCheckedChange: (v: boolean) => void }) {
  return (
    <div className="flex items-center justify-between rounded-xl border px-4 py-3">
      <Label className="text-sm cursor-pointer">{label}</Label>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}
