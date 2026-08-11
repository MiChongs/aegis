"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { Database, Plus, RefreshCw, Save, Shield, Trash2, X } from "lucide-react";
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
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";

// ── 表单状态 ──

type Draft = {
  enabled: boolean;
  globalRate: string;
  authRate: string;
  adminRate: string;
  corazaEnabled: boolean;
  corazaParanoia: string;
  requestBodyLimit: string;
  requestBodyMemory: string;
  allowedCIDRs: string[];
  blockedCIDRs: string[];
  blockedUserAgents: string[];
  blockedPathPrefix: string[];
  maxPathLength: string;
  maxQueryLength: string;
  defaultBanMode: string;
  tarpitDelayMs: string;
};

function makeSeed(v?: Record<string, unknown> | null): Draft {
  const fw = v as {
    enabled?: boolean; globalRate?: string; authRate?: string; adminRate?: string;
    corazaEnabled?: boolean; corazaParanoia?: number; requestBodyLimit?: number; requestBodyMemory?: number;
    allowedCIDRs?: string[]; blockedCIDRs?: string[]; blockedUserAgents?: string[]; blockedPathPrefix?: string[];
    maxPathLength?: number; maxQueryLength?: number;
    defaultBanMode?: string; tarpitDelayMs?: number;
  } | null;
  return {
    enabled: fw?.enabled !== false,
    globalRate: fw?.globalRate || "1200-M",
    authRate: fw?.authRate || "180-M",
    adminRate: fw?.adminRate || "360-M",
    corazaEnabled: fw?.corazaEnabled !== false,
    corazaParanoia: String(fw?.corazaParanoia || 1),
    requestBodyLimit: String(fw?.requestBodyLimit || 0),
    requestBodyMemory: String(fw?.requestBodyMemory || 0),
    allowedCIDRs: fw?.allowedCIDRs || [],
    blockedCIDRs: fw?.blockedCIDRs || [],
    blockedUserAgents: fw?.blockedUserAgents || [],
    blockedPathPrefix: fw?.blockedPathPrefix || [],
    maxPathLength: String(fw?.maxPathLength || 0),
    maxQueryLength: String(fw?.maxQueryLength || 0),
    defaultBanMode: fw?.defaultBanMode || "forbidden",
    tarpitDelayMs: String(fw?.tarpitDelayMs || 5000),
  };
}

function fmtDate(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

// ── 主组件 ──

export function FirewallConfigPanel() {
  const operator = useAuthStore((s) => s.operator);
  const settingsQ = useAdminSystemSettingsQuery(Boolean(operator?.isSuperAdmin));
  const updateMut = useUpdateAdminSystemSettingsMutation();
  const fw = settingsQ.data?.firewall;
  const seed = useMemo(() => fw ? makeSeed(fw as unknown as Record<string, unknown>) : null, [fw]);
  const seedKey = useMemo(() => seed ? JSON.stringify(seed) : "", [seed]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [syncedKey, setSyncedKey] = useState("");

  useEffect(() => {
    if (!seed || seedKey === syncedKey) return;
    setDraft(seed);
    setSyncedKey(seedKey);
  }, [seed, seedKey, syncedKey]);

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft(s => s ? { ...s, [k]: v } : s);

  async function handleSave(e?: FormEvent) {
    e?.preventDefault();
    if (!draft) return;
    try {
      await updateMut.mutateAsync({
        firewall: {
          enabled: draft.enabled,
          globalRate: draft.globalRate,
          authRate: draft.authRate,
          adminRate: draft.adminRate,
          corazaEnabled: draft.corazaEnabled,
          corazaParanoia: Number(draft.corazaParanoia || 1),
          requestBodyLimit: Number(draft.requestBodyLimit || 0),
          requestBodyMemory: Number(draft.requestBodyMemory || 0),
          allowedCIDRs: draft.allowedCIDRs,
          blockedCIDRs: draft.blockedCIDRs,
          blockedUserAgents: draft.blockedUserAgents,
          blockedPathPrefix: draft.blockedPathPrefix,
          maxPathLength: Number(draft.maxPathLength || 0),
          maxQueryLength: Number(draft.maxQueryLength || 0),
          defaultBanMode: draft.defaultBanMode || "forbidden",
          tarpitDelayMs: Number(draft.tarpitDelayMs || 5000),
        },
      });
      toast.success("防火墙配置已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (!operator?.isSuperAdmin) return <EmptyState title="无访问权限" description="仅超级管理员可配置防火墙。" />;
  if (settingsQ.isLoading || !draft || !fw) return <LoadingState title="加载防火墙配置" />;

  return (
    <div className="space-y-6">
      {/* 状态概览 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard icon={Shield} label="防火墙" value={fw.enabled ? "启用" : "关闭"} ok={fw.enabled} />
        <StatusCard icon={Shield} label="Coraza WAF" value={fw.corazaEnabled ? `L${fw.corazaParanoia}` : "关闭"} ok={fw.corazaEnabled} />
        <StatusCard icon={RefreshCw} label="热重载" value={`v${fw.reloadVersion || 0}`} sub={fmtDate(fw.reloadedAt)} ok />
        <StatusCard icon={Database} label="来源" value={fw.source === "database" ? "数据库" : "环境变量"} ok />
      </div>

      <Separator />

      {/* 操作栏 */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">防火墙配置</h2>
          <p className="text-sm text-muted-foreground">保存后热重载生效</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => { setDraft(seed); setSyncedKey(seedKey); }} disabled={updateMut.isPending}>
            重置
          </Button>
          <Button size="sm" onClick={() => void handleSave()} disabled={updateMut.isPending}>
            <Save className="size-3.5" /> {updateMut.isPending ? "保存中..." : "保存"}
          </Button>
        </div>
      </div>

      {/* 主开关 */}
      <div className="grid gap-3 sm:grid-cols-2">
        <SwitchRow label="防火墙总开关" checked={draft.enabled} onCheckedChange={v => set("enabled", v)} />
        <SwitchRow label="Coraza WAF" checked={draft.corazaEnabled} onCheckedChange={v => set("corazaEnabled", v)} />
      </div>

      <Accordion type="multiple" defaultValue={["rate", "limits", "lists", "ban"]} className="space-y-3">
        {/* ── 限流 ── */}
        <AccordionItem value="rate" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">限流配置</AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="全局限流"><Input className="h-8 text-sm font-mono" value={draft.globalRate} onChange={e => set("globalRate", e.target.value)} placeholder="1200-M" /></Field>
              <Field label="认证限流"><Input className="h-8 text-sm font-mono" value={draft.authRate} onChange={e => set("authRate", e.target.value)} placeholder="180-M" /></Field>
              <Field label="管理限流"><Input className="h-8 text-sm font-mono" value={draft.adminRate} onChange={e => set("adminRate", e.target.value)} placeholder="360-M" /></Field>
            </div>
            <p className="text-[10px] text-muted-foreground">格式: 次数-周期（M=分钟, H=小时, D=天）</p>
          </AccordionContent>
        </AccordionItem>

        {/* ── 请求限制 ── */}
        <AccordionItem value="limits" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">请求限制</AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Field label="WAF 等级">
                <Select value={draft.corazaParanoia} onValueChange={v => set("corazaParanoia", v)}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1">1 - 基础</SelectItem>
                    <SelectItem value="2">2 - 标准</SelectItem>
                    <SelectItem value="3">3 - 严格</SelectItem>
                    <SelectItem value="4">4 - 偏执</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label="请求体上限 (bytes)"><Input className="h-8 text-sm" value={draft.requestBodyLimit} onChange={e => set("requestBodyLimit", e.target.value)} /></Field>
              <Field label="内存缓冲 (bytes)"><Input className="h-8 text-sm" value={draft.requestBodyMemory} onChange={e => set("requestBodyMemory", e.target.value)} /></Field>
              <Field label="最大路径长度"><Input className="h-8 text-sm" value={draft.maxPathLength} onChange={e => set("maxPathLength", e.target.value)} /></Field>
            </div>
            <Field label="最大查询长度"><Input className="h-8 text-sm w-40" value={draft.maxQueryLength} onChange={e => set("maxQueryLength", e.target.value)} /></Field>
          </AccordionContent>
        </AccordionItem>

        {/* ── IP 封禁响应模式 ── */}
        <AccordionItem value="ban" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            IP 封禁响应模式（平台级全局默认）
          </AccordionTrigger>
          <AccordionContent className="pb-4 space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="默认响应模式">
                <Select value={draft.defaultBanMode} onValueChange={v => set("defaultBanMode", v)}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="forbidden">拦截 (HTTP 403) · 默认</SelectItem>
                    <SelectItem value="silent_drop">完全不响应（关闭 TCP）</SelectItem>
                    <SelectItem value="connection_reset">连接重置</SelectItem>
                    <SelectItem value="tarpit">拖延响应 (Tarpit)</SelectItem>
                    <SelectItem value="stealth_404">伪装 404</SelectItem>
                    <SelectItem value="teapot">418 彩蛋</SelectItem>
                    <SelectItem value="rate_choke">严格限速</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Tarpit 延迟 (ms)">
                <Input
                  className="h-8 text-sm"
                  type="number"
                  min={0}
                  max={30000}
                  step={500}
                  value={draft.tarpitDelayMs}
                  onChange={e => set("tarpitDelayMs", e.target.value)}
                />
              </Field>
            </div>
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              封禁单个 IP 时可覆盖此默认；未显式指定的封禁均使用这里配置的全局模式。
              「完全不响应」会在 TCP 层直接关闭连接，客户端感知为连接超时 / 重置。
            </p>
          </AccordionContent>
        </AccordionItem>

        {/* ── 黑白名单 ── */}
        <AccordionItem value="lists" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">黑白名单</AccordionTrigger>
          <AccordionContent className="pb-4 space-y-5">
            <TagListField label="允许 CIDR（白名单）" placeholder="192.168.1.0/24" items={draft.allowedCIDRs} onChange={v => set("allowedCIDRs", v)} />
            <Separator />
            <TagListField label="拦截 CIDR（黑名单）" placeholder="10.0.0.0/8" items={draft.blockedCIDRs} onChange={v => set("blockedCIDRs", v)} />
            <Separator />
            <TagListField label="拦截 User-Agent" placeholder="sqlmap" items={draft.blockedUserAgents} onChange={v => set("blockedUserAgents", v)} />
            <Separator />
            <TagListField label="拦截路径前缀" placeholder="/.git" items={draft.blockedPathPrefix} onChange={v => set("blockedPathPrefix", v)} />
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

// ── 辅助组件 ──

function StatusCard({ icon: Icon, label, value, ok, sub }: {
  icon: React.ComponentType<{ className?: string }>; label: string; value: string; ok?: boolean; sub?: string;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
      <Icon className={`size-5 shrink-0 ${ok ? "text-emerald-500" : "text-muted-foreground"}`} />
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-sm font-semibold truncate">{value}</div>
        {sub && <div className="text-[10px] text-muted-foreground">{sub}</div>}
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}

function SwitchRow({ label, checked, onCheckedChange }: { label: string; checked: boolean; onCheckedChange: (v: boolean) => void }) {
  return (
    <div className="flex items-center justify-between rounded-xl border px-4 py-3">
      <Label className="text-sm cursor-pointer">{label}</Label>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

// ── Tag 列表输入（CIDR / UA / 路径前缀的便捷输入） ──

function TagListField({ label, placeholder, items, onChange }: {
  label: string; placeholder: string; items: string[]; onChange: (v: string[]) => void;
}) {
  const [input, setInput] = useState("");

  function handleAdd() {
    const val = input.trim();
    if (!val || items.includes(val)) return;
    onChange([...items, val]);
    setInput("");
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter") { e.preventDefault(); handleAdd(); }
  }

  function handleRemove(idx: number) {
    onChange(items.filter((_, i) => i !== idx));
  }

  function handlePaste(e: React.ClipboardEvent) {
    const text = e.clipboardData.getData("text");
    if (text.includes("\n") || text.includes(",")) {
      e.preventDefault();
      const parsed = text.split(/[\n,]/).map(s => s.trim()).filter(Boolean);
      const unique = [...new Set([...items, ...parsed])];
      onChange(unique);
    }
  }

  return (
    <div className="space-y-2">
      <Label className="text-xs">{label}</Label>
      <div className="flex gap-2">
        <Input
          className="h-8 text-sm font-mono flex-1"
          placeholder={placeholder}
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
        />
        <Button type="button" size="sm" variant="outline" className="h-8 px-2.5" onClick={handleAdd} disabled={!input.trim()}>
          <Plus className="size-3.5" />
        </Button>
      </div>
      {items.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {items.map((item, i) => (
            <span key={`${item}-${i}`} className="inline-flex items-center gap-1 rounded-lg border bg-muted/40 px-2.5 py-1 font-mono text-xs">
              {item}
              <button type="button" className="rounded p-0.5 text-muted-foreground hover:text-destructive" onClick={() => handleRemove(i)}>
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}
      {items.length > 0 && (
        <div className="flex gap-2">
          <button type="button" className="text-[10px] text-muted-foreground hover:text-destructive" onClick={() => onChange([])}>
            <Trash2 className="inline size-3 mr-0.5" />清空全部
          </button>
          <span className="text-[10px] text-muted-foreground">{items.length} 条</span>
        </div>
      )}
    </div>
  );
}
