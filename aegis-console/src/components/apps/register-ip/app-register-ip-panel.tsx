"use client";

import { useState } from "react";
import { BarChart3, FlaskConical, ListTree, Network, ScrollText, SearchCode, ShieldBan, SlidersHorizontal, Smartphone } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { RegDeviceInspectPanel } from "@/components/apps/register-ip/register-device-inspect-panel";
import { RegIPEventsPanel } from "@/components/apps/register-ip/register-ip-events-panel";
import { RegIPInspectPanel } from "@/components/apps/register-ip/register-ip-inspect-panel";
import { RegIPListsPanel } from "@/components/apps/register-ip/register-ip-lists-panel";
import { RegIPOverviewPanel } from "@/components/apps/register-ip/register-ip-overview-panel";
import { RegIPPolicyPanel } from "@/components/apps/register-ip/register-ip-policy-panel";
import { RegIPRegistrationsPanel } from "@/components/apps/register-ip/register-ip-registrations-panel";
import { defaultRegIPPolicy } from "@/components/apps/register-ip/register-ip-shared";
import { RegIPSimulatorPanel } from "@/components/apps/register-ip/register-ip-simulator-panel";
import type { RegIPPolicy } from "@/lib/api/register-ip";
import { useRegIPPolicyQuery } from "@/lib/register-ip-hooks";
import { cn } from "@/lib/utils";

/**
 * 注册准入区块：地址与设备两条互补的线。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 开没开、这几天注册与拦截多少、哪些地址 / 网段注册最多、谁在惩罚期、此刻有没有突增 |
 * | 策略 | 配额规则、网段口径、惩罚期、突增检测、运营商与属地、故障与缺 IP 处置 |
 * | IP 查询 | 一个地址此刻的全部状况：名单、惩罚期、各规则用量、注册过的账号、地址下的设备；解除限制 |
 * | 设备查询 | 一台设备此刻的全部状况：标识是否有效、名单、惩罚期、设备规则用量、注册过的账号、用过的地址；解除限制 |
 * | 名单 | 放行 / 拒绝 / 放宽的地址、网段与设备，命中次数与到期 |
 * | 拦截记录 | 每一次拒绝、加验、观察、惩罚与解除 |
 * | 注册台账 | 每个新账号从哪个地址注册；累计规则按它数 |
 * | 模拟 | 用草稿或已保存的策略，拿一个地址试一次注册 |
 *
 * 两张查询页互相跳：地址下的设备点进设备查询，设备用过的地址点进 IP 查询，顺着一条线摸出整个出口池。
 * 策略草稿放在这一层：模拟要能拿未保存的草稿试，切视图也不该丢改动。
 * 视图状态不进 URL，`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "policy", label: "策略", icon: SlidersHorizontal },
  { key: "inspect", label: "IP 查询", icon: SearchCode },
  { key: "device", label: "设备查询", icon: Smartphone },
  { key: "lists", label: "名单", icon: ShieldBan },
  { key: "events", label: "拦截记录", icon: ListTree },
  { key: "registrations", label: "注册台账", icon: ScrollText },
  { key: "simulate", label: "模拟", icon: FlaskConical }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppRegisterIPPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  const [inspectIP, setInspectIP] = useState<string>("");
  const [inspectDevice, setInspectDevice] = useState<string>("");
  const policyQuery = useRegIPPolicyQuery(appKey);

  const [draft, setDraft] = useState<{ scope: string; value: RegIPPolicy } | null>(null);
  const scope = appKey ?? "";
  const saved = policyQuery.data?.policy;
  const fallback = policyQuery.data?.defaults ?? defaultRegIPPolicy();
  const policy = draft?.scope === scope ? draft.value : (saved ?? fallback);
  const dirty = draft?.scope === scope && JSON.stringify(draft.value) !== JSON.stringify(saved ?? fallback);

  if (!appKey) {
    return <NoAppSelected icon={<Network className="size-5" />} />;
  }

  const openIP = (ip: string) => {
    setInspectIP(ip);
    setView("inspect");
  };
  const openDevice = (device: string) => {
    setInspectDevice(device);
    setView("device");
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
            {item.key === "policy" && dirty ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="有未保存的改动" /> : null}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <RegIPOverviewPanel appKey={appKey} onOpenPolicy={() => setView("policy")} onOpenIP={openIP} onOpenDevice={openDevice} />
      ) : null}
      {view === "policy" ? (
        <RegIPPolicyPanel
          appKey={appKey}
          policy={policy}
          defaults={fallback}
          configured={policyQuery.data?.configured ?? false}
          loading={policyQuery.isLoading}
          dirty={dirty}
          onChange={(value) => setDraft({ scope, value })}
          onReset={() => setDraft(null)}
          onSaved={() => setDraft(null)}
          onSimulate={() => setView("simulate")}
        />
      ) : null}
      {view === "inspect" ? (
        <RegIPInspectPanel appKey={appKey} ip={inspectIP} onIPChange={setInspectIP} onOpenDevice={openDevice} />
      ) : null}
      {view === "device" ? (
        <RegDeviceInspectPanel appKey={appKey} device={inspectDevice} onDeviceChange={setInspectDevice} onOpenIP={openIP} />
      ) : null}
      {view === "lists" ? <RegIPListsPanel appKey={appKey} onOpenIP={openIP} onOpenDevice={openDevice} /> : null}
      {view === "events" ? <RegIPEventsPanel appKey={appKey} onOpenIP={openIP} onOpenDevice={openDevice} /> : null}
      {view === "registrations" ? <RegIPRegistrationsPanel appKey={appKey} onOpenIP={openIP} onOpenDevice={openDevice} /> : null}
      {view === "simulate" ? <RegIPSimulatorPanel appKey={appKey} draft={policy} dirty={dirty} /> : null}
    </div>
  );
}
