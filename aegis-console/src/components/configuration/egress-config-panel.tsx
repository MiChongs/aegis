"use client";

import { useMemo, useState } from "react";
import {
  Activity,
  Database,
  Globe2,
  Plus,
  RefreshCw,
  Route,
  Save,
  Trash2,
  Undo2,
  Waypoints,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type {
  EgressAction,
  EgressEndpoint,
  EgressExplanation,
  EgressRule,
  EgressSettings,
  EgressStrategy,
  EgressTestResult,
} from "@/lib/api/egress";
import {
  useEgressExplainMutation,
  useEgressProbeMutation,
  useEgressSettingsQuery,
  useEgressTestMutation,
  useResetEgressSettingsMutation,
  useUpdateEgressSettingsMutation,
} from "@/lib/egress-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

/**
 * 出海代理网关配置面板。
 *
 * 编辑对象是**一整张路由表**（端点 + 规则），因此草稿态就是整份配置，
 * 保存时整份提交 —— 与后端 `PUT /api/admin/system/egress` 的整份替换语义一致。
 *
 * 密钥字段永远从后端拿不到明文（只有 `passwordSet` / `privateKeySet`），
 * 留空提交即表示保持原值；要真正清空得勾「清除密钥」。
 */

// ── 常量与小工具 ──

const ACTION_LABEL: Record<EgressAction, string> = {
  proxy: "走代理",
  direct: "直连",
  reject: "拒绝",
};

const STRATEGY_LABEL: Record<EgressStrategy, string> = {
  failover: "故障转移",
  round_robin: "轮询",
  random: "随机",
  weighted: "加权随机",
  latency: "最低延迟",
};

/** 需要 host:port 的协议；direct 端点不需要地址 */
function needsAddress(protocol: string) {
  return protocol !== "direct";
}

function fmtDate(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return Number.isNaN(d.getTime())
    ? "—"
    : d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function fmtBytes(n: number) {
  if (!n) return "0";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx += 1;
  }
  return `${value.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
}

/** 多行/逗号分隔文本 ⇄ 字符串数组。域名后缀动辄上百条，用 textarea 比 chips 实用得多。 */
function parseList(raw: string): string[] {
  return raw
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function joinList(items?: string[] | null) {
  return (items || []).join("\n");
}

function emptyEndpoint(index: number): EgressEndpoint {
  return {
    name: `endpoint-${index + 1}`,
    enabled: true,
    protocol: "socks5h",
    address: "",
    weight: 1,
    tls: { enabled: false },
    ssh: {},
    shadowsocks: {},
  };
}

function emptyRule(index: number): EgressRule {
  return {
    name: `rule-${index + 1}`,
    enabled: true,
    priority: (index + 1) * 10,
    action: "proxy",
    endpoints: [],
    strategy: "failover",
    match: { domainSuffixes: [] },
  };
}

type Draft = {
  enabled: boolean;
  defaultAction: EgressAction;
  defaultEndpoints: string[];
  defaultStrategy: EgressStrategy;
  dialTimeoutMs: number;
  tlsHandshakeTimeoutMs: number;
  responseHeaderTimeoutMs: number;
  idleConnTimeoutMs: number;
  maxIdleConnsPerHost: number;
  health: EgressSettings["health"];
  endpoints: EgressEndpoint[];
  rules: EgressRule[];
};

function makeDraft(v: EgressSettings): Draft {
  return {
    enabled: v.enabled,
    defaultAction: v.defaultAction,
    defaultEndpoints: v.defaultEndpoints || [],
    defaultStrategy: v.defaultStrategy,
    dialTimeoutMs: v.dialTimeoutMs,
    tlsHandshakeTimeoutMs: v.tlsHandshakeTimeoutMs,
    responseHeaderTimeoutMs: v.responseHeaderTimeoutMs,
    idleConnTimeoutMs: v.idleConnTimeoutMs,
    maxIdleConnsPerHost: v.maxIdleConnsPerHost,
    health: { ...v.health },
    // 深拷贝：直接引用查询缓存会让编辑污染缓存对象
    endpoints: v.endpoints.map((item) => ({
      ...item,
      tls: { ...item.tls },
      ssh: { ...item.ssh },
      shadowsocks: { ...item.shadowsocks },
    })),
    rules: v.rules.map((item) => ({ ...item, match: { ...item.match } })),
  };
}

// ── 主组件 ──

export function EgressConfigPanel() {
  const operator = useAuthStore((s) => s.operator);
  const isSuperAdmin = Boolean(operator?.isSuperAdmin);
  const settingsQ = useEgressSettingsQuery(isSuperAdmin);
  const settings = settingsQ.data;

  if (!isSuperAdmin) {
    return <EmptyState title="无访问权限" description="出海代理网关决定平台所有对外调用的线路，仅超级管理员可配置。" />;
  }
  if (settingsQ.isLoading || !settings) {
    return <LoadingState title="加载出海网关配置" />;
  }

  // 用配置版本作 key 整体重挂载编辑器：配置真正变了（保存 / 重置 / 热重载）才重灌草稿。
  // 30s 一次的运行态刷新不改版本号，因此不会把正在编辑的内容冲掉。
  // 这样也不需要「effect 里 setState」那套同步写法。
  return <EgressEditor key={`${settings.reloadVersion}:${settings.source}`} settings={settings} />;
}

function EgressEditor({ settings }: { settings: EgressSettings }) {
  const updateMut = useUpdateEgressSettingsMutation();
  const resetMut = useResetEgressSettingsMutation();
  const probeMut = useEgressProbeMutation();

  const pristine = useMemo(() => makeDraft(settings), [settings.reloadVersion, settings.source]); // eslint-disable-line react-hooks/exhaustive-deps
  const [draft, setDraft] = useState<Draft>(() => makeDraft(settings));

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((s) => ({ ...s, [key]: value }));

  const patchEndpoint = (index: number, patch: Partial<EgressEndpoint>) =>
    setDraft((s) => ({ ...s, endpoints: s.endpoints.map((item, i) => (i === index ? { ...item, ...patch } : item)) }));

  const patchRule = (index: number, patch: Partial<EgressRule>) =>
    setDraft((s) => ({ ...s, rules: s.rules.map((item, i) => (i === index ? { ...item, ...patch } : item)) }));

  async function handleSave() {
    try {
      await updateMut.mutateAsync({
        enabled: draft.enabled,
        defaultAction: draft.defaultAction,
        defaultEndpoints: draft.defaultEndpoints,
        defaultStrategy: draft.defaultStrategy,
        dialTimeoutMs: draft.dialTimeoutMs,
        tlsHandshakeTimeoutMs: draft.tlsHandshakeTimeoutMs,
        responseHeaderTimeoutMs: draft.responseHeaderTimeoutMs,
        idleConnTimeoutMs: draft.idleConnTimeoutMs,
        maxIdleConnsPerHost: draft.maxIdleConnsPerHost,
        health: draft.health,
        endpoints: draft.endpoints,
        rules: draft.rules,
      });
      toast.success("出海网关配置已保存并热生效");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleReset() {
    try {
      await resetMut.mutateAsync();
      toast.success("已恢复为环境变量配置");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "重置失败");
    }
  }

  async function handleProbe() {
    try {
      const result = await probeMut.mutateAsync();
      const failed = result.results.filter((item) => !item.ok);
      if (failed.length === 0) {
        toast.success(`${result.results.length} 个端点全部可达`);
      } else {
        toast.warning(`${failed.length}/${result.results.length} 个端点不可达：${failed.map((f) => f.endpoint).join("、")}`);
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "探测失败");
    }
  }

  const runtime = settings.runtime;
  const healthyCount = runtime.endpoints.filter((item) => item.healthy && item.enabled).length;
  const endpointNames = draft.endpoints.map((item) => item.name).filter(Boolean);

  return (
    <div className="space-y-6">
      {/* 状态概览 */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatusCard icon={Globe2} label="出海网关" value={settings.enabled ? "启用" : "关闭"} ok={settings.enabled} />
        <StatusCard
          icon={Waypoints}
          label="端点健康"
          value={`${healthyCount}/${runtime.endpoints.length}`}
          ok={runtime.endpoints.length === 0 || healthyCount > 0}
          sub={`${runtime.rules.length} 条规则`}
        />
        <StatusCard
          icon={Activity}
          label="路由计数"
          value={`${runtime.proxied} 代理`}
          ok
          sub={`${runtime.direct} 直连 · ${runtime.rejected} 拒绝 · ${runtime.failed} 失败`}
        />
        <StatusCard
          icon={Database}
          label="配置来源"
          value={settings.source === "database" ? "数据库" : settings.source || "环境变量"}
          ok
          sub={`v${settings.reloadVersion} · ${fmtDate(settings.reloadedAt)}`}
        />
      </div>

      <Separator />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">出海代理网关</h2>
          <p className="text-sm text-muted-foreground">
            按目标域名后缀决定出站流量走直连还是境外线路，保存后热重载生效
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => void handleProbe()} disabled={probeMut.isPending}>
            <RefreshCw className={`size-3.5 ${probeMut.isPending ? "animate-spin" : ""}`} /> 立即探测
          </Button>
          <Button variant="outline" size="sm" onClick={() => void handleReset()} disabled={resetMut.isPending}>
            <Undo2 className="size-3.5" /> 恢复环境变量
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setDraft(pristine)}
            disabled={updateMut.isPending}
          >
            撤销改动
          </Button>
          <Button size="sm" onClick={() => void handleSave()} disabled={updateMut.isPending}>
            <Save className="size-3.5" /> {updateMut.isPending ? "保存中..." : "保存"}
          </Button>
        </div>
      </div>

      <Accordion type="multiple" defaultValue={["basic", "endpoints", "rules", "tools"]} className="space-y-3">
        {/* ── 基础 ── */}
        <AccordionItem value="basic" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">基础策略</AccordionTrigger>
          <AccordionContent className="space-y-4 pb-4">
            <SwitchRow
              label="启用出海网关"
              hint="关闭时所有出站一律直连，并继续尊重 HTTP_PROXY 等环境变量"
              checked={draft.enabled}
              onCheckedChange={(v) => set("enabled", v)}
            />
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="未命中规则时" hint="direct = 白名单出海（推荐）">
                <Select value={draft.defaultAction} onValueChange={(v) => set("defaultAction", v as EgressAction)}>
                  <SelectTrigger className="h-8 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {settings.catalog.actions.map((action) => (
                      <SelectItem key={action} value={action}>
                        {ACTION_LABEL[action]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="默认选路策略">
                <Select value={draft.defaultStrategy} onValueChange={(v) => set("defaultStrategy", v as EgressStrategy)}>
                  <SelectTrigger className="h-8 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {settings.catalog.strategies.map((item) => (
                      <SelectItem key={item} value={item}>
                        {STRATEGY_LABEL[item]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="默认端点" hint="仅当默认动作为「走代理」时使用">
                <EndpointPicker
                  available={endpointNames}
                  selected={draft.defaultEndpoints}
                  onChange={(next) => set("defaultEndpoints", next)}
                />
              </Field>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* ── 端点 ── */}
        <AccordionItem value="endpoints" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            出海端点（{draft.endpoints.length}）
          </AccordionTrigger>
          <AccordionContent className="space-y-3 pb-4">
            {draft.endpoints.length === 0 ? (
              <p className="text-sm text-muted-foreground">还没有端点。先加一条线路，再在下面写规则把域名指过来。</p>
            ) : null}
            {draft.endpoints.map((endpoint, index) => (
              <EndpointEditor
                key={index}
                endpoint={endpoint}
                stat={runtime.endpoints.find((item) => item.name === endpoint.name)}
                protocols={settings.catalog.protocols}
                methods={settings.catalog.shadowsocksMethods}
                peers={endpointNames.filter((name) => name !== endpoint.name)}
                onChange={(patch) => patchEndpoint(index, patch)}
                onRemove={() =>
                  set(
                    "endpoints",
                    draft.endpoints.filter((_, i) => i !== index)
                  )
                }
              />
            ))}
            <Button
              variant="outline"
              size="sm"
              onClick={() => set("endpoints", [...draft.endpoints, emptyEndpoint(draft.endpoints.length)])}
            >
              <Plus className="size-3.5" /> 添加端点
            </Button>
          </AccordionContent>
        </AccordionItem>

        {/* ── 规则 ── */}
        <AccordionItem value="rules" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
            路由规则（{draft.rules.length}）
          </AccordionTrigger>
          <AccordionContent className="space-y-3 pb-4">
            <p className="text-xs text-muted-foreground">
              按 priority 升序匹配，先命中先生效。域名后缀按标签边界匹配：
              <code className="mx-1 rounded bg-muted px-1">google.com</code>
              命中 www.google.com，不命中 notgoogle.com。
            </p>
            {draft.rules.map((rule, index) => (
              <RuleEditor
                key={index}
                rule={rule}
                matched={runtime.rules.find((item) => item.name === rule.name)?.matched}
                actions={settings.catalog.actions}
                strategies={settings.catalog.strategies}
                endpointNames={endpointNames}
                onChange={(patch) => patchRule(index, patch)}
                onRemove={() =>
                  set(
                    "rules",
                    draft.rules.filter((_, i) => i !== index)
                  )
                }
              />
            ))}
            <Button variant="outline" size="sm" onClick={() => set("rules", [...draft.rules, emptyRule(draft.rules.length)])}>
              <Plus className="size-3.5" /> 添加规则
            </Button>
          </AccordionContent>
        </AccordionItem>

        {/* ── 健康检查 ── */}
        <AccordionItem value="health" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">健康检查</AccordionTrigger>
          <AccordionContent className="space-y-3 pb-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <SwitchRow
                label="主动探测"
                checked={draft.health.enabled}
                onCheckedChange={(v) => set("health", { ...draft.health, enabled: v })}
              />
              <SwitchRow
                label="被动熔断"
                hint="拨号失败即计数，达到阈值后进入冷却"
                checked={draft.health.passiveEnabled}
                onCheckedChange={(v) => set("health", { ...draft.health, passiveEnabled: v })}
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-4">
              <NumberField
                label="探测间隔(秒)"
                value={draft.health.intervalSeconds}
                onChange={(v) => set("health", { ...draft.health, intervalSeconds: v })}
              />
              <NumberField
                label="探测超时(秒)"
                value={draft.health.timeoutSeconds}
                onChange={(v) => set("health", { ...draft.health, timeoutSeconds: v })}
              />
              <NumberField
                label="失败阈值"
                value={draft.health.failureThreshold}
                onChange={(v) => set("health", { ...draft.health, failureThreshold: v })}
              />
              <NumberField
                label="恢复阈值"
                value={draft.health.successThreshold}
                onChange={(v) => set("health", { ...draft.health, successThreshold: v })}
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="探测地址">
                <Input
                  className="h-8 font-mono text-sm"
                  value={draft.health.probeUrl || ""}
                  placeholder={settings.catalog.defaultProbeUrl}
                  onChange={(e) => set("health", { ...draft.health, probeUrl: e.target.value })}
                />
              </Field>
              <NumberField
                label="熔断冷却(秒)"
                value={draft.health.cooldownSeconds}
                onChange={(v) => set("health", { ...draft.health, cooldownSeconds: v })}
              />
            </div>
            <SwitchRow
              label="全部不健康时仍然尝试"
              hint="关掉意味着一次探测误判就会切断整条出海链路"
              checked={draft.health.allowUnhealthy !== false}
              onCheckedChange={(v) => set("health", { ...draft.health, allowUnhealthy: v })}
            />
          </AccordionContent>
        </AccordionItem>

        {/* ── 连接参数 ── */}
        <AccordionItem value="timeouts" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">连接参数</AccordionTrigger>
          <AccordionContent className="pb-4">
            <div className="grid gap-3 sm:grid-cols-3 lg:grid-cols-5">
              <NumberField label="拨号超时(ms)" value={draft.dialTimeoutMs} onChange={(v) => set("dialTimeoutMs", v)} />
              <NumberField
                label="TLS 握手(ms)"
                value={draft.tlsHandshakeTimeoutMs}
                onChange={(v) => set("tlsHandshakeTimeoutMs", v)}
              />
              <NumberField
                label="响应头超时(ms)"
                value={draft.responseHeaderTimeoutMs}
                onChange={(v) => set("responseHeaderTimeoutMs", v)}
              />
              <NumberField
                label="空闲连接(ms)"
                value={draft.idleConnTimeoutMs}
                onChange={(v) => set("idleConnTimeoutMs", v)}
              />
              <NumberField
                label="每主机空闲连接数"
                value={draft.maxIdleConnsPerHost}
                onChange={(v) => set("maxIdleConnsPerHost", v)}
              />
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* ── 运行态与工具 ── */}
        <AccordionItem value="tools" className="rounded-xl border px-4">
          <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">运行态与排障</AccordionTrigger>
          <AccordionContent className="space-y-4 pb-4">
            <RuntimeTable settings={settings} />
            <Separator />
            <div className="grid gap-4 lg:grid-cols-2">
              <TestCard endpointNames={endpointNames} defaultUrl={settings.catalog.defaultProbeUrl} />
              <ExplainCard />
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

// ── 端点编辑器 ──

function EndpointEditor({
  endpoint,
  stat,
  protocols,
  methods,
  peers,
  onChange,
  onRemove,
}: {
  endpoint: EgressEndpoint;
  stat?: EgressSettings["runtime"]["endpoints"][number];
  protocols: string[];
  methods: string[];
  peers: string[];
  onChange: (patch: Partial<EgressEndpoint>) => void;
  onRemove: () => void;
}) {
  const protocol = endpoint.protocol;
  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="h-8 w-40 font-mono text-sm"
          value={endpoint.name}
          placeholder="端点名"
          onChange={(e) => onChange({ name: e.target.value })}
        />
        <Select value={protocol} onValueChange={(v) => onChange({ protocol: v })}>
          <SelectTrigger className="h-8 w-36 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {protocols.map((item) => (
              <SelectItem key={item} value={item}>
                {item}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {needsAddress(protocol) ? (
          <Input
            className="h-8 w-52 font-mono text-sm"
            value={endpoint.address || ""}
            placeholder="host:port"
            onChange={(e) => onChange({ address: e.target.value })}
          />
        ) : null}
        {stat ? (
          <Badge variant={stat.healthy ? "success" : "danger"} size="sm">
            {stat.healthy ? "健康" : "不可用"}
            {stat.latencyMs ? ` · ${stat.latencyMs}ms` : ""}
          </Badge>
        ) : null}
        <div className="ml-auto flex items-center gap-2">
          <Switch checked={endpoint.enabled !== false} onCheckedChange={(v) => onChange({ enabled: v })} />
          <Button variant="ghost" size="sm" onClick={onRemove}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-4">
        {protocol === "ssh" ? (
          <Field label="SSH 用户">
            <Input
              className="h-8 text-sm"
              value={endpoint.ssh.user || ""}
              onChange={(e) => onChange({ ssh: { ...endpoint.ssh, user: e.target.value } })}
            />
          </Field>
        ) : (
          <Field label="用户名">
            <Input className="h-8 text-sm" value={endpoint.username || ""} onChange={(e) => onChange({ username: e.target.value })} />
          </Field>
        )}
        <Field label="口令" hint={endpoint.passwordSet ? "已配置，留空保持不变" : undefined}>
          <Input
            className="h-8 text-sm"
            type="password"
            value={protocol === "shadowsocks" ? endpoint.shadowsocks.password || "" : endpoint.password || ""}
            placeholder={endpoint.passwordSet ? "••••••" : ""}
            onChange={(e) =>
              protocol === "shadowsocks"
                ? onChange({ shadowsocks: { ...endpoint.shadowsocks, password: e.target.value } })
                : onChange({ password: e.target.value })
            }
          />
        </Field>
        <Field label="上一跳 via" hint="经另一个端点连出去">
          <Select value={endpoint.via || "__none__"} onValueChange={(v) => onChange({ via: v === "__none__" ? "" : v })}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">直接连出</SelectItem>
              {peers.map((name) => (
                <SelectItem key={name} value={name}>
                  {name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="权重 / 地区">
          <div className="flex gap-2">
            <Input
              className="h-8 w-16 text-sm"
              type="number"
              min={1}
              value={endpoint.weight ?? 1}
              onChange={(e) => onChange({ weight: Number(e.target.value) || 1 })}
            />
            <Input
              className="h-8 flex-1 text-sm"
              value={endpoint.region || ""}
              placeholder="hk"
              onChange={(e) => onChange({ region: e.target.value })}
            />
          </div>
        </Field>
      </div>

      {protocol === "shadowsocks" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="加密方式">
            <Select
              value={endpoint.shadowsocks.method || methods[0] || "aes-256-gcm"}
              onValueChange={(v) => onChange({ shadowsocks: { ...endpoint.shadowsocks, method: v } })}
            >
              <SelectTrigger className="h-8 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {methods.map((item) => (
                  <SelectItem key={item} value={item}>
                    {item}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
      ) : null}

      {protocol === "ssh" ? (
        <Field label="SSH 私钥 PEM" hint={endpoint.privateKeySet ? "已配置，留空保持不变" : "留空则用口令认证"}>
          <Textarea
            className="min-h-20 font-mono text-xs"
            value={endpoint.ssh.privateKeyPem || ""}
            placeholder={endpoint.privateKeySet ? "已配置（留空保持不变）" : "-----BEGIN OPENSSH PRIVATE KEY-----"}
            onChange={(e) => onChange({ ssh: { ...endpoint.ssh, privateKeyPem: e.target.value } })}
          />
        </Field>
      ) : null}

      {protocol === "https" || protocol === "trojan" || endpoint.tls.enabled ? (
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="TLS SNI">
            <Input
              className="h-8 text-sm"
              value={endpoint.tls.serverName || ""}
              onChange={(e) => onChange({ tls: { ...endpoint.tls, serverName: e.target.value } })}
            />
          </Field>
          <SwitchRow
            label="跳过证书校验"
            checked={Boolean(endpoint.tls.insecureSkipVerify)}
            onCheckedChange={(v) => onChange({ tls: { ...endpoint.tls, insecureSkipVerify: v } })}
          />
          <SwitchRow
            label="启用 TLS"
            checked={endpoint.tls.enabled}
            onCheckedChange={(v) => onChange({ tls: { ...endpoint.tls, enabled: v } })}
          />
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-3">
        <Field label="探测地址" hint="留空用全局；纯内网跳板可填 - 退化为 TCP 探测">
          <Input
            className="h-8 font-mono text-xs"
            value={endpoint.probeUrl || ""}
            onChange={(e) => onChange({ probeUrl: e.target.value })}
          />
        </Field>
        <Field label="备注">
          <Input className="h-8 text-sm" value={endpoint.remark || ""} onChange={(e) => onChange({ remark: e.target.value })} />
        </Field>
        <SwitchRow
          label="清除已存密钥"
          hint="保存时把该端点的口令与私钥清空"
          checked={Boolean(endpoint.clearSecrets)}
          onCheckedChange={(v) => onChange({ clearSecrets: v })}
        />
      </div>

      {protocol === "http" ? (
        <SwitchRow
          label="明文请求用 absolute-URI 转发"
          hint="仅当代理禁止 CONNECT 到 80 端口时才需要"
          checked={Boolean(endpoint.httpForwardMode)}
          onCheckedChange={(v) => onChange({ httpForwardMode: v })}
        />
      ) : null}
    </div>
  );
}

// ── 规则编辑器 ──

function RuleEditor({
  rule,
  matched,
  actions,
  strategies,
  endpointNames,
  onChange,
  onRemove,
}: {
  rule: EgressRule;
  matched?: number;
  actions: EgressAction[];
  strategies: EgressStrategy[];
  endpointNames: string[];
  onChange: (patch: Partial<EgressRule>) => void;
  onRemove: () => void;
}) {
  const action = rule.action || "proxy";
  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="h-8 w-44 text-sm"
          value={rule.name}
          placeholder="规则名"
          onChange={(e) => onChange({ name: e.target.value })}
        />
        <Input
          className="h-8 w-20 text-sm"
          type="number"
          value={rule.priority ?? 0}
          onChange={(e) => onChange({ priority: Number(e.target.value) || 0 })}
        />
        <Select value={action} onValueChange={(v) => onChange({ action: v as EgressAction })}>
          <SelectTrigger className="h-8 w-28 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {actions.map((item) => (
              <SelectItem key={item} value={item}>
                {ACTION_LABEL[item]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {action === "proxy" ? (
          <Select value={rule.strategy || "failover"} onValueChange={(v) => onChange({ strategy: v as EgressStrategy })}>
            <SelectTrigger className="h-8 w-32 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {strategies.map((item) => (
                <SelectItem key={item} value={item}>
                  {STRATEGY_LABEL[item]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        {typeof matched === "number" ? (
          <Badge variant="info" size="sm">
            命中 {matched}
          </Badge>
        ) : null}
        <div className="ml-auto flex items-center gap-2">
          <Switch checked={rule.enabled !== false} onCheckedChange={(v) => onChange({ enabled: v })} />
          <Button variant="ghost" size="sm" onClick={onRemove}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      {action === "proxy" ? (
        <Field label="端点（按策略选路）">
          <EndpointPicker
            available={endpointNames}
            selected={rule.endpoints || []}
            onChange={(next) => onChange({ endpoints: next })}
          />
        </Field>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="域名后缀" hint="每行一条，*. 前缀可省略">
          <Textarea
            className="min-h-24 font-mono text-xs"
            value={joinList(rule.match.domainSuffixes)}
            placeholder={"*.google.com\ngoogleapis.com"}
            onChange={(e) => onChange({ match: { ...rule.match, domainSuffixes: parseList(e.target.value) } })}
          />
        </Field>
        <Field label="例外后缀" hint="优先于上面的匹配">
          <Textarea
            className="min-h-24 font-mono text-xs"
            value={joinList(rule.match.excludeDomainSuffixes)}
            onChange={(e) => onChange({ match: { ...rule.match, excludeDomainSuffixes: parseList(e.target.value) } })}
          />
        </Field>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <Field label="调用方 profile" hint="如 payment.* ；与域名条件是「与」">
          <Input
            className="h-8 font-mono text-xs"
            value={(rule.match.profiles || []).join(",")}
            onChange={(e) => onChange({ match: { ...rule.match, profiles: parseList(e.target.value) } })}
          />
        </Field>
        <Field label="端口" hint="留空不限；与域名条件是「与」">
          <Input
            className="h-8 font-mono text-xs"
            value={(rule.match.ports || []).join(",")}
            onChange={(e) =>
              onChange({
                match: {
                  ...rule.match,
                  ports: parseList(e.target.value)
                    .map((item) => Number(item))
                    .filter((item) => Number.isFinite(item) && item > 0),
                },
              })
            }
          />
        </Field>
        <Field label="目标 CIDR" hint="目标是字面 IP 时生效">
          <Input
            className="h-8 font-mono text-xs"
            value={(rule.match.cidrs || []).join(",")}
            onChange={(e) => onChange({ match: { ...rule.match, cidrs: parseList(e.target.value) } })}
          />
        </Field>
      </div>
    </div>
  );
}

// ── 运行态表 ──

function RuntimeTable({ settings }: { settings: EgressSettings }) {
  const rows = settings.runtime.endpoints;
  if (rows.length === 0) {
    return <p className="text-sm text-muted-foreground">还没有端点，运行态为空。</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-xs text-muted-foreground">
          <tr className="border-b">
            <th className="px-2 py-2 text-left font-medium">端点</th>
            <th className="px-2 py-2 text-left font-medium">链路</th>
            <th className="px-2 py-2 text-left font-medium">状态</th>
            <th className="px-2 py-2 text-right font-medium">延迟</th>
            <th className="px-2 py-2 text-right font-medium">成功/失败</th>
            <th className="px-2 py-2 text-right font-medium">出/入流量</th>
            <th className="px-2 py-2 text-left font-medium">最近错误</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((item) => (
            <tr key={item.name} className="border-b last:border-0">
              <td className="px-2 py-2">
                <div className="font-mono text-xs">{item.name}</div>
                <div className="text-[10px] text-muted-foreground">
                  {item.protocol}
                  {item.region ? ` · ${item.region}` : ""}
                </div>
              </td>
              <td className="px-2 py-2 font-mono text-[10px] text-muted-foreground">
                {(item.chain || [item.name]).join(" → ")}
              </td>
              <td className="px-2 py-2">
                <Badge variant={!item.enabled ? "secondary" : item.healthy ? "success" : "danger"} size="sm">
                  {!item.enabled ? "已停用" : item.healthy ? "健康" : "不可用"}
                </Badge>
              </td>
              <td className="px-2 py-2 text-right font-mono text-xs">{item.latencyMs ? `${item.latencyMs}ms` : "—"}</td>
              <td className="px-2 py-2 text-right font-mono text-xs">
                {item.successes}/{item.failures}
              </td>
              <td className="px-2 py-2 text-right font-mono text-xs">
                {fmtBytes(item.bytesOut)} / {fmtBytes(item.bytesIn)}
              </td>
              <td className="max-w-56 truncate px-2 py-2 text-xs text-muted-foreground" title={item.lastError}>
                {item.lastError || "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ── 自测 ──

function TestCard({ endpointNames, defaultUrl }: { endpointNames: string[]; defaultUrl: string }) {
  const testMut = useEgressTestMutation();
  const [url, setUrl] = useState("");
  const [endpoint, setEndpoint] = useState("__auto__");
  const [profile, setProfile] = useState("");
  const [result, setResult] = useState<EgressTestResult | null>(null);

  async function run() {
    try {
      const data = await testMut.mutateAsync({
        url: url.trim() || undefined,
        endpoint: endpoint === "__auto__" ? undefined : endpoint,
        profile: profile.trim() || undefined,
      });
      setResult(data);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "自测失败");
    }
  }

  return (
    <div className="space-y-3 rounded-lg border p-3">
      <div className="flex items-center gap-2 text-sm font-semibold">
        <Activity className="size-4" /> 连通性自测
      </div>
      <p className="text-xs text-muted-foreground">
        走的是与业务完全相同的路径。把地址指向「查询出口 IP」类服务，可以直接确认落地是否在境外。
      </p>
      <Input className="h-8 font-mono text-xs" placeholder={defaultUrl} value={url} onChange={(e) => setUrl(e.target.value)} />
      <div className="grid grid-cols-2 gap-2">
        <Select value={endpoint} onValueChange={setEndpoint}>
          <SelectTrigger className="h-8 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__auto__">按规则自动选路</SelectItem>
            {endpointNames.map((name) => (
              <SelectItem key={name} value={name}>
                指定端点：{name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="h-8 font-mono text-xs"
          placeholder="模拟 profile（可选）"
          value={profile}
          onChange={(e) => setProfile(e.target.value)}
        />
      </div>
      <Button size="sm" onClick={() => void run()} disabled={testMut.isPending}>
        {testMut.isPending ? "测试中..." : "开始测试"}
      </Button>
      {result ? (
        <div className="space-y-1 rounded-md bg-muted/40 p-2 text-xs">
          <div className="flex items-center gap-2">
            <Badge variant={result.ok ? "success" : "danger"} size="sm">
              {result.ok ? `HTTP ${result.statusCode}` : "失败"}
            </Badge>
            <span className="font-mono">{result.latencyMs}ms</span>
            <span className="text-muted-foreground">
              {ACTION_LABEL[result.action]}
              {result.endpoint ? ` · ${result.endpoint}` : ""}
              {result.rule ? ` · 规则 ${result.rule}` : ""}
            </span>
          </div>
          {result.error ? <div className="text-destructive">{result.error}</div> : null}
          {result.body ? <pre className="max-h-24 overflow-auto whitespace-pre-wrap font-mono">{result.body}</pre> : null}
        </div>
      ) : null}
    </div>
  );
}

// ── 路由解释 ──

function ExplainCard() {
  const explainMut = useEgressExplainMutation();
  const [host, setHost] = useState("");
  const [profile, setProfile] = useState("");
  const [result, setResult] = useState<EgressExplanation | null>(null);

  async function run() {
    if (!host.trim()) {
      toast.error("请填写域名");
      return;
    }
    try {
      setResult(await explainMut.mutateAsync({ host: host.trim(), profile: profile.trim() || undefined }));
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "查询失败");
    }
  }

  return (
    <div className="space-y-3 rounded-lg border p-3">
      <div className="flex items-center gap-2 text-sm font-semibold">
        <Route className="size-4" /> 路由解释
      </div>
      <p className="text-xs text-muted-foreground">不发起真实连接，只回答「这个域名会走哪条线、为什么」。</p>
      <div className="grid grid-cols-2 gap-2">
        <Input
          className="h-8 font-mono text-xs"
          placeholder="api.stripe.com"
          value={host}
          onChange={(e) => setHost(e.target.value)}
        />
        <Input
          className="h-8 font-mono text-xs"
          placeholder="profile（可选）"
          value={profile}
          onChange={(e) => setProfile(e.target.value)}
        />
      </div>
      <Button size="sm" variant="outline" onClick={() => void run()} disabled={explainMut.isPending}>
        {explainMut.isPending ? "查询中..." : "查询"}
      </Button>
      {result ? (
        <div className="space-y-2 rounded-md bg-muted/40 p-2 text-xs">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={result.action === "proxy" ? "success" : result.action === "reject" ? "danger" : "secondary"} size="sm">
              {ACTION_LABEL[result.action]}
            </Badge>
            <span className="font-mono">{result.endpoint || "—"}</span>
            <span className="text-muted-foreground">
              规则 {result.rule}
              {result.reason ? ` · ${result.reason}` : ""}
            </span>
          </div>
          {result.chain?.length ? (
            <div className="font-mono text-[10px] text-muted-foreground">链路：{result.chain.join(" → ")}</div>
          ) : null}
          {result.error ? <div className="text-destructive">{result.error}</div> : null}
          {result.evaluated.length ? (
            <ul className="space-y-0.5">
              {result.evaluated.map((item, index) => (
                <li key={`${item.rule}-${index}`} className="flex gap-2">
                  <span className={item.matched ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground"}>
                    {item.matched ? "命中" : "跳过"}
                  </span>
                  <span className="font-mono">{item.rule}</span>
                  {item.reason ? <span className="text-muted-foreground">{item.reason}</span> : null}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

// ── 复用小组件 ──

function EndpointPicker({
  available,
  selected,
  onChange,
}: {
  available: string[];
  selected: string[];
  onChange: (next: string[]) => void;
}) {
  if (available.length === 0) {
    return <p className="text-xs text-muted-foreground">先添加端点</p>;
  }
  return (
    <div className="flex flex-wrap gap-1.5">
      {available.map((name) => {
        const active = selected.includes(name);
        return (
          <button
            key={name}
            type="button"
            onClick={() => onChange(active ? selected.filter((item) => item !== name) : [...selected, name])}
            className={`rounded-md border px-2 py-1 font-mono text-xs transition-colors ${
              active ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground hover:bg-muted"
            }`}
          >
            {active ? `${selected.indexOf(name) + 1}. ` : ""}
            {name}
          </button>
        );
      })}
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
      {hint ? <p className="text-[10px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function NumberField({ label, value, onChange }: { label: string; value?: number; onChange: (v: number) => void }) {
  return (
    <Field label={label}>
      <Input
        className="h-8 text-sm"
        type="number"
        min={0}
        value={value ?? 0}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
      />
    </Field>
  );
}

function SwitchRow({
  label,
  hint,
  checked,
  onCheckedChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
      <div>
        <div className="text-sm">{label}</div>
        {hint ? <div className="text-[10px] text-muted-foreground">{hint}</div> : null}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function StatusCard({
  icon: Icon,
  label,
  value,
  sub,
  ok,
}: {
  icon: typeof Globe2;
  label: string;
  value: string;
  sub?: string;
  ok?: boolean;
}) {
  return (
    <div className="rounded-xl border p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className={`size-3.5 ${ok ? "text-emerald-500" : "text-muted-foreground"}`} />
        {label}
      </div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
      {sub ? <div className="text-[10px] text-muted-foreground">{sub}</div> : null}
    </div>
  );
}
