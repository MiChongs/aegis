"use client";

import { useState } from "react";
import { Flame, FlaskConical, LockKeyhole, MapPin, Play, ShieldQuestion, Smartphone } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  Block,
  DecisionBanner,
  DeviceQuality,
  DeviceText,
  Fact,
  IPText,
  ListKindBadge,
  locationText,
  METHODS,
  METHOD_META,
  RuleHitList,
  SCOPE_META,
  unitOf
} from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api-client";
import type { RegIPMethod, RegIPPolicy, RegIPSimulateInput, RegIPSimulation } from "@/lib/api/register-ip";
import { useSimulateRegIPMutation } from "@/lib/register-ip-hooks";
import { cn } from "@/lib/utils";

/**
 * 模拟一次注册：草稿（或已保存的）策略 + 一个地址和 / 或一台设备。计数取真实值、只数不占，不会写台账也不会占名额；
 * 可以改写某条规则的已有注册数、属地、突增与惩罚期，看「如果……会怎样」。
 */
export function RegIPSimulatorPanel({ appKey, draft, dirty }: { appKey: string; draft: RegIPPolicy; dirty: boolean }) {
  const mutation = useSimulateRegIPMutation(appKey);
  const [useDraft, setUseDraft] = useState(true);
  const [ip, setIP] = useState("");
  const [device, setDevice] = useState("");
  const [devicePenalty, setDevicePenalty] = useState("");
  const [method, setMethod] = useState<RegIPMethod>("password");
  const [captchaPassed, setCaptchaPassed] = useState(false);
  const [counts, setCounts] = useState<Record<string, string>>({});
  const [overrideLocation, setOverrideLocation] = useState(false);
  const [location, setLocation] = useState({ country: "中国", province: "", city: "", isp: "" });
  const [surge, setSurge] = useState(false);
  const [penalty, setPenalty] = useState("");
  const [result, setResult] = useState<RegIPSimulation | null>(null);

  async function run() {
    const parsedCounts: Record<string, number> = {};
    for (const [id, value] of Object.entries(counts)) {
      if (value.trim() !== "" && Number.isFinite(Number(value))) parsedCounts[id] = Math.max(0, Math.floor(Number(value)));
    }
    const input: RegIPSimulateInput = {
      policy: useDraft ? draft : null,
      ip: ip.trim(),
      deviceId: device.trim() || undefined,
      method,
      captchaPassed,
      counts: Object.keys(parsedCounts).length ? parsedCounts : undefined,
      location: overrideLocation ? { ...location, resolved: Boolean(location.country) } : null,
      surgeActive: surge ? true : null,
      penaltySeconds: penalty.trim() ? Math.max(0, Number(penalty) || 0) * 60 : null,
      devicePenaltySeconds: devicePenalty.trim() ? Math.max(0, Number(devicePenalty) || 0) * 60 : null
    };
    try {
      setResult(await mutation.mutateAsync(input));
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  }

  const rules = draft.rules.filter((rule) => rule.enabled);

  return (
    <div className="grid items-start gap-4 xl:grid-cols-[380px_minmax(0,1fr)]">
      <Block
        title={
          <span className="flex items-center gap-1.5">
            <FlaskConical className="size-4 text-muted-foreground" /> 场景
          </span>
        }
        bodyClassName="space-y-4 p-4"
      >
        <SwitchRow
          label={useDraft ? (dirty ? "用草稿策略（有未保存改动）" : "用当前编辑器里的策略") : "用已保存的策略"}
          hint="关闭则按库里已保存的策略判"
          checked={useDraft}
          onChange={setUseDraft}
        />
        <FieldGroup label="注册地址">
          <Input value={ip} onChange={(event) => setIP(event.target.value)} placeholder="1.2.3.4 或 2408:8207::1，留空模拟缺少 IP" className="font-mono text-xs" />
        </FieldGroup>
        <FieldGroup label="设备标识" hint="留空模拟没上报">
          <div className="relative">
            <Smartphone className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-fuchsia-500" />
            <Input value={device} onChange={(event) => setDevice(event.target.value)} placeholder="6f9619ff-8b86-d011-b42d-00c04fc964ff" className="pl-8 font-mono text-xs" />
          </div>
        </FieldGroup>
        <FieldGroup label="注册方式">
          <div className="flex flex-wrap gap-1.5">
            {METHODS.map((item) => {
              const Icon = METHOD_META[item].icon;
              return (
                <button
                  key={item}
                  type="button"
                  onClick={() => setMethod(item)}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-xs transition-colors",
                    method === item ? "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30" : "bg-muted hover:bg-muted-foreground/10"
                  )}
                >
                  <Icon className="size-3.5" />
                  {METHOD_META[item].label}
                </button>
              );
            })}
          </div>
        </FieldGroup>
        <SwitchRow icon={<ShieldQuestion className="size-3.5" />} label="已通过图形验证码" hint="加验型处置放行" checked={captchaPassed} onChange={setCaptchaPassed} />

        {rules.length ? (
          <FieldGroup label="改写已有注册数" hint="留空用真实计数">
            <div className="space-y-1.5">
              {rules.map((rule) => (
                <div key={rule.id} className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-xs">
                    <span className="text-muted-foreground">{SCOPE_META[rule.scope].short} · </span>
                    {rule.name || rule.id}
                  </span>
                  <Input
                    type="number"
                    min={0}
                    value={counts[rule.id] ?? ""}
                    onChange={(event) => setCounts({ ...counts, [rule.id]: event.target.value })}
                    placeholder="真实"
                    className="h-7 w-20 text-xs tabular-nums"
                  />
                  <span className="w-16 shrink-0 text-right text-[10px] text-muted-foreground">
                    / {rule.limit} {unitOf(rule.scope)}
                  </span>
                </div>
              ))}
            </div>
          </FieldGroup>
        ) : null}

        <SwitchRow icon={<MapPin className="size-3.5" />} label="改写属地与运营商" checked={overrideLocation} onChange={setOverrideLocation} />
        {overrideLocation ? (
          <div className="grid grid-cols-2 gap-2">
            {(["country", "province", "city", "isp"] as const).map((key) => (
              <Input
                key={key}
                value={location[key]}
                onChange={(event) => setLocation({ ...location, [key]: event.target.value })}
                placeholder={{ country: "国家", province: "省", city: "市", isp: "运营商" }[key]}
                className="h-8 text-xs"
              />
            ))}
          </div>
        ) : null}
        <SwitchRow icon={<Flame className="size-3.5" />} label="假定正在突增" checked={surge} onChange={setSurge} />
        <div className="flex items-center gap-2 rounded-xl bg-muted px-3 py-2.5">
          <LockKeyhole className="size-3.5 text-muted-foreground" />
          <span className="flex-1 text-xs font-medium">假定地址惩罚期剩余</span>
          <Input type="number" min={0} value={penalty} onChange={(event) => setPenalty(event.target.value)} placeholder="真实" className="h-7 w-20 bg-background text-xs" />
          <span className="text-[11px] text-muted-foreground">分钟</span>
        </div>
        <div className="flex items-center gap-2 rounded-xl bg-muted px-3 py-2.5">
          <Smartphone className="size-3.5 text-muted-foreground" />
          <span className="flex-1 text-xs font-medium">假定设备惩罚期剩余</span>
          <Input
            type="number"
            min={0}
            value={devicePenalty}
            onChange={(event) => setDevicePenalty(event.target.value)}
            placeholder="真实"
            className="h-7 w-20 bg-background text-xs"
          />
          <span className="text-[11px] text-muted-foreground">分钟</span>
        </div>
        <Button className="w-full" disabled={mutation.isPending} onClick={run}>
          <Play className="size-3.5" />
          {mutation.isPending ? "模拟中..." : "模拟注册"}
        </Button>
      </Block>

      <div className="space-y-4">
        {!result ? (
          <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed border-border py-20 text-center">
            <FlaskConical className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">填好场景后点「模拟注册」</span>
            <span className="text-[11px] text-muted-foreground">只数不占：不会写台账、不会占名额，也不会进惩罚期</span>
          </div>
        ) : (
          <>
            <div className="flex items-center gap-2">
              <Badge variant={result.draft ? "warning" : "secondary"} size="sm">{result.draft ? "按草稿策略" : "按已保存策略"}</Badge>
              {result.storeFailed ? <Badge variant="danger" size="sm">计数存储不可用，按故障处置推演</Badge> : null}
            </div>
            <DecisionBanner decision={result.decision} />
            <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
              <Block title="配额规则">
                <RuleHitList rules={result.decision.rules} />
              </Block>
              <Block title="事实">
                <Fact label="地址">{result.address ? <IPText value={result.address.ip} /> : "缺少 IP"}</Fact>
                {result.address ? (
                  <>
                    <Fact label="按单个地址计">
                      <span className="font-mono">{result.address.host}</span>
                    </Fact>
                    <Fact label="按网段计">
                      <span className="font-mono">{result.address.subnet}</span>
                    </Fact>
                  </>
                ) : null}
                <Fact label="设备">{result.device.id ? <DeviceText value={result.device.id} /> : "未上报"}</Fact>
                <Fact label="设备质量">
                  <DeviceQuality device={result.device} />
                </Fact>
                <Fact label="属地">{result.location.resolved ? locationText(result.location) : "查不到"}</Fact>
                <Fact label="运营商">{result.location.isp || "—"}</Fact>
                <Fact label="名单">
                  {result.list ? (
                    <span className="inline-flex items-center gap-1.5">
                      <ListKindBadge kind={result.list.kind} factor={result.list.factor} />
                      <span className="text-[10px] text-muted-foreground">{result.list.target === "device" ? "设备名单" : "地址名单"}</span>
                    </span>
                  ) : (
                    "未命中"
                  )}
                </Fact>
                <Fact label="地址惩罚期">{result.penaltySeconds ? `剩余 ${Math.ceil(result.penaltySeconds / 60)} 分钟` : "无"}</Fact>
                <Fact label="设备惩罚期">{result.devicePenaltySeconds ? `剩余 ${Math.ceil(result.devicePenaltySeconds / 60)} 分钟` : "无"}</Fact>
                <Fact label="突增">
                  {result.surge.active ? (
                    <Badge variant="danger" size="sm">突增中</Badge>
                  ) : (
                    <span className="text-muted-foreground">
                      最近 {result.surge.current} 个 / 触发线 {result.surge.threshold}
                    </span>
                  )}
                </Fact>
              </Block>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
