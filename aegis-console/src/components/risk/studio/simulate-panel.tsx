"use client";

import { useState } from "react";
import { Play, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { JsonEditor } from "@/components/functions/json-editor";
import { Field, Figure, Segmented } from "@/components/traffic/traffic-kit";
import { AppScopeSelect } from "@/components/risk/kit/app-scope";
import { ActionBadge, LevelBadge, ScoreMeter } from "@/components/risk/kit/badges";
import { DECIDED_BY_LABEL, DIM_LABEL, FEATURE_KIND_LABEL, KIND_LABEL, LIST_TYPE_LABEL, formatMicros } from "@/components/risk/kit/labels";
import { TraceView } from "@/components/risk/kit/trace-view";
import type { Feature, ListHit, ListKind, ListType, RiskMeta, SimRequest, SimulateResult } from "@/lib/api/risk";
import { useSimulateScene } from "@/lib/risk-hooks";

import { errorText, IssueList, StudioSection } from "./kit";

interface Form {
  appId?: number;
  userId: string;
  account: string;
  ip: string;
  userAgent: string;
  phone: string;
  email: string;
  amount: string;
  deviceId: string;
  platform: string;
  brand: string;
  model: string;
  osVersion: string;
  emulator: boolean;
  rooted: boolean;
  country: string;
  ipScore: string;
  proxy: boolean;
  vpn: boolean;
  tor: boolean;
  datacenter: boolean;
  captchaPassed: boolean;
  captchaUnavailable: boolean;
  extra: string;
}

const EMPTY: Form = {
  userId: "", account: "", ip: "203.0.113.7", userAgent: "Mozilla/5.0 (Linux; Android 14) Chrome/130.0 Mobile", phone: "", email: "", amount: "",
  deviceId: "", platform: "", brand: "", model: "", osVersion: "", emulator: false, rooted: false,
  country: "", ipScore: "", proxy: false, vpn: false, tor: false, datacenter: false,
  captchaPassed: false, captchaUnavailable: false, extra: "{}",
};

const num = (s: string) => (s.trim() && Number.isFinite(Number(s)) ? Number(s) : undefined);
const str = (s: string) => s.trim() || undefined;

function toRequest(f: Form, extra: Record<string, unknown> | undefined): SimRequest {
  return {
    appId: f.appId,
    userId: num(f.userId),
    account: str(f.account),
    ip: str(f.ip),
    userAgent: str(f.userAgent),
    phone: str(f.phone),
    email: str(f.email),
    amount: num(f.amount),
    captchaPassed: f.captchaPassed || undefined,
    captchaUnavailable: f.captchaUnavailable || undefined,
    device: {
      id: str(f.deviceId), platform: str(f.platform), brand: str(f.brand), model: str(f.model), osVersion: str(f.osVersion),
      emulator: f.emulator || undefined, rooted: f.rooted || undefined,
    },
    geo: {
      country: str(f.country), ipScore: num(f.ipScore), proxy: f.proxy || undefined, vpn: f.vpn || undefined, tor: f.tor || undefined, datacenter: f.datacenter || undefined,
    },
    extra,
  };
}

function Check({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <Checkbox checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      {label}
    </label>
  );
}

function ResultView({ result }: { result: SimulateResult }) {
  const r = result.result;
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <StudioSection title="结果">
        <div className="flex flex-wrap items-center gap-3">
          <ActionBadge action={r.action} size="default" />
          <LevelBadge level={r.risk_level} size="default" />
          <span className="text-sm text-muted-foreground">由{DECIDED_BY_LABEL[r.decided_by] ?? r.decided_by}决定</span>
          {r.degraded?.length ? <Badge variant="warning" size="sm">降级 {r.degraded.join("、")}</Badge> : null}
        </div>
        <div className="grid gap-3 sm:grid-cols-4">
          <div className="sm:col-span-2">
            <div className="text-[11px] text-muted-foreground">分数</div>
            <ScoreMeter score={r.score} />
          </div>
          <Figure label="耗时" value={r.latency_us ? formatMicros(r.latency_us) : "—"} />
          <Figure label="版本" value={r.release || "草稿"} />
        </div>
        {r.reasons?.length ? (
          <ul className="flex flex-col gap-1 text-sm">
            {r.reasons.map((x, i) => <li key={i} className="rounded-md bg-muted px-2.5 py-1.5">{x}</li>)}
          </ul>
        ) : null}
        {r.hits?.length ? (
          <div className="flex flex-wrap gap-1.5">
            {r.hits.map((h) => (
              <Badge key={h.key} variant={h.mode === "observe" ? "info" : "outline"} size="sm">
                {h.name}
                <span className="text-muted-foreground">{DIM_LABEL[h.dimension] ?? h.dimension} +{h.score}</span>
                {h.mode === "observe" ? "观察" : null}
              </Badge>
            ))}
          </div>
        ) : null}
        {r.commands?.length ? (
          <div className="flex flex-col gap-1">
            {r.commands.map((c) => (
              <div key={c.id} className="flex flex-wrap items-center gap-2 font-mono text-xs">
                <Badge variant="danger" size="sm">{c.kind}</Badge>
                <span>{c.target}</span>
                <span>{c.value}</span>
              </div>
            ))}
          </div>
        ) : null}
      </StudioSection>
      {result.trace ? <TraceView trace={result.trace} /> : null}
    </div>
  );
}

export function SimulatePanel({ sceneKey, meta, bundleFeatures, dirty }: {
  sceneKey: string;
  meta: RiskMeta;
  bundleFeatures: Feature[];
  dirty: boolean;
}) {
  const simulate = useSimulateScene();
  const [form, setForm] = useState<Form>(EMPTY);
  const [mode, setMode] = useState<"fixed" | "live">("fixed");
  const [draft, setDraft] = useState(true);
  const [features, setFeatures] = useState<Record<string, string>>({});
  const [lists, setLists] = useState<ListHit[]>([]);
  const [extraError, setExtraError] = useState<string | null>(null);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm({ ...form, [k]: v });
  const issues = simulate.error && (simulate.error as { status?: number }).status === 422 ? [{ stage: "", message: errorText(simulate.error) }] : [];

  const run = () => {
    let extra: Record<string, unknown> | undefined;
    if (form.extra.trim()) {
      try {
        const parsed = JSON.parse(form.extra) as unknown;
        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("需要 JSON 对象");
        extra = Object.keys(parsed).length ? (parsed as Record<string, unknown>) : undefined;
        setExtraError(null);
      } catch (e) {
        setExtraError(errorText(e, "JSON 格式错误"));
        return;
      }
    }
    const fixed = Object.fromEntries(
      bundleFeatures.map((f) => [f.key, num(features[f.key] ?? "") ?? f.default ?? 0]),
    );
    simulate.mutate(
      {
        key: sceneKey,
        input: {
          request: toRequest(form, extra),
          draft,
          mode,
          features: mode === "fixed" ? fixed : undefined,
          lists: mode === "fixed" ? lists.filter((l) => l.value.trim()) : undefined,
        },
      },
      { onError: (e) => toast.error(errorText(e, "模拟失败")) },
    );
  };

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      <div className="flex min-w-0 flex-col gap-4">
        <StudioSection
          title="请求"
          aside={
            <Button size="sm" onClick={run} disabled={simulate.isPending}>
              <Play />
              运行
            </Button>
          }
        >
          <div className="flex flex-wrap items-center gap-3">
            <Segmented value={mode} options={[{ value: "fixed" as const, label: "手动给特征" }, { value: "live" as const, label: "真实数据源" }]} onChange={setMode} />
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={draft} onCheckedChange={setDraft} />
              用草稿
            </label>
            {draft && dirty ? <Badge variant="warning" size="sm">有未保存的修改</Badge> : null}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="应用"><AppScopeSelect value={form.appId} onChange={(v) => set("appId", v)} className="h-9 w-full" allLabel="不指定" /></Field>
            <Field label="用户 ID"><Input inputMode="numeric" value={form.userId} onChange={(e) => set("userId", e.target.value)} /></Field>
            <Field label="账号"><Input value={form.account} onChange={(e) => set("account", e.target.value)} /></Field>
            <Field label="IP"><Input className="font-mono" value={form.ip} onChange={(e) => set("ip", e.target.value)} /></Field>
            <Field label="手机号"><Input value={form.phone} onChange={(e) => set("phone", e.target.value)} /></Field>
            <Field label="邮箱"><Input value={form.email} onChange={(e) => set("email", e.target.value)} /></Field>
            <Field label="金额"><Input inputMode="decimal" value={form.amount} onChange={(e) => set("amount", e.target.value)} /></Field>
            <Field label="国家"><Input className="uppercase" maxLength={2} value={form.country} onChange={(e) => set("country", e.target.value.toUpperCase())} /></Field>
          </div>
          <Field label="User-Agent"><Input className="font-mono text-xs" value={form.userAgent} onChange={(e) => set("userAgent", e.target.value)} /></Field>
        </StudioSection>

        <StudioSection title="设备与网络">
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="设备 ID"><Input className="font-mono" value={form.deviceId} onChange={(e) => set("deviceId", e.target.value)} /></Field>
            <Field label="平台"><Input value={form.platform} placeholder="android" onChange={(e) => set("platform", e.target.value)} /></Field>
            <Field label="品牌"><Input value={form.brand} onChange={(e) => set("brand", e.target.value)} /></Field>
            <Field label="型号"><Input value={form.model} onChange={(e) => set("model", e.target.value)} /></Field>
            <Field label="系统版本"><Input value={form.osVersion} onChange={(e) => set("osVersion", e.target.value)} /></Field>
            <Field label="IP 情报分"><Input inputMode="numeric" value={form.ipScore} onChange={(e) => set("ipScore", e.target.value)} /></Field>
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2">
            <Check label="模拟器" checked={form.emulator} onChange={(v) => set("emulator", v)} />
            <Check label="已 Root" checked={form.rooted} onChange={(v) => set("rooted", v)} />
            <Check label="代理" checked={form.proxy} onChange={(v) => set("proxy", v)} />
            <Check label="VPN" checked={form.vpn} onChange={(v) => set("vpn", v)} />
            <Check label="Tor" checked={form.tor} onChange={(v) => set("tor", v)} />
            <Check label="机房 IP" checked={form.datacenter} onChange={(v) => set("datacenter", v)} />
            <Check label="已过验证码" checked={form.captchaPassed} onChange={(v) => set("captchaPassed", v)} />
            <Check label="应用无验证码" checked={form.captchaUnavailable} onChange={(v) => set("captchaUnavailable", v)} />
          </div>
          <Field label="场景入参（extra）" error={extraError}>
            <JsonEditor height={110} value={form.extra} onChange={(v) => set("extra", v)} />
          </Field>
        </StudioSection>

        {mode === "fixed" ? (
          <>
            <StudioSection title={`特征 ${bundleFeatures.length}`}>
              {bundleFeatures.length === 0 ? (
                <p className="text-sm text-muted-foreground">草稿没有用到特征</p>
              ) : (
                <div className="grid gap-2 sm:grid-cols-2">
                  {bundleFeatures.map((f) => (
                    <div key={f.key} className="flex min-w-0 items-center gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-xs">{f.name}</div>
                        <div className="truncate font-mono text-[10px] text-muted-foreground">F.{f.key} {FEATURE_KIND_LABEL[f.kind]}</div>
                      </div>
                      <Input
                        className="h-8 w-24 text-right tabular-nums"
                        inputMode="decimal"
                        placeholder={String(f.default ?? 0)}
                        value={features[f.key] ?? ""}
                        onChange={(e) => setFeatures({ ...features, [f.key]: e.target.value })}
                      />
                    </div>
                  ))}
                </div>
              )}
            </StudioSection>
            <StudioSection
              title="名单命中"
              aside={
                <Button size="xs" variant="outline" onClick={() => setLists([...lists, { listType: "ip", kind: "block", value: form.ip }])}>
                  <Plus />
                  添加
                </Button>
              }
            >
              {lists.length === 0 ? <p className="text-sm text-muted-foreground">无</p> : null}
              {lists.map((l, i) => (
                <div key={i} className="grid grid-cols-[8rem_7rem_minmax(0,1fr)_auto] items-center gap-2">
                  <Select value={l.listType} onValueChange={(v) => setLists(lists.map((x, j) => (j === i ? { ...x, listType: v as ListType } : x)))}>
                    <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>{meta.listTypes.map((t) => <SelectItem key={t} value={t}>{LIST_TYPE_LABEL[t]}</SelectItem>)}</SelectContent>
                  </Select>
                  <Select value={l.kind} onValueChange={(v) => setLists(lists.map((x, j) => (j === i ? { ...x, kind: v as ListKind } : x)))}>
                    <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>{(["block", "allow", "watch"] as ListKind[]).map((k) => <SelectItem key={k} value={k}>{KIND_LABEL[k]}</SelectItem>)}</SelectContent>
                  </Select>
                  <Input className="h-8 font-mono text-xs" value={l.value} onChange={(e) => setLists(lists.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} />
                  <Button size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setLists(lists.filter((_, j) => j !== i))}>
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))}
            </StudioSection>
          </>
        ) : null}
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        {issues.length ? <IssueList issues={issues} /> : null}
        {simulate.data ? (
          <ResultView result={simulate.data} />
        ) : (
          <div className="grid min-h-48 place-items-center rounded-xl border border-dashed text-sm text-muted-foreground">
            {simulate.isPending ? "运行中" : "暂无结果"}
          </div>
        )}
      </div>
    </div>
  );
}
