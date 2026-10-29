"use client";

import { useNodeRender, type WorkflowNodeEntity } from "@flowgram.ai/free-layout-editor";
import { Copy, Plus, Trash2 } from "lucide-react";
import { Field } from "@/components/apps/item-grant/item-grant-fields";
import {
  KeyValueInput,
  ListInput,
  OptionSelect,
  OptionalNumber,
  SecretInput,
  SectionTitle,
  ToggleRow
} from "@/components/configuration/egress/egress-fields";
import {
  CoreInspector,
  GroupInspector,
  ProviderInspector,
  ProxyInspector,
  RulesInspector
} from "@/components/configuration/egress/egress-inspector-mihomo";
import { TargetOrder, useConnectedTargets, type InspectorContext, type Patch } from "@/components/configuration/egress/egress-inspector-shared";
import { ACTION_LABEL, NODE_VISUAL, PROTOCOL_LABEL, STRATEGY_LABEL, nodeTitle } from "@/components/configuration/egress/egress-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import type {
  EgressAction,
  EgressEndpoint,
  EgressGatewayData,
  EgressGraphCatalog,
  EgressHealth,
  EgressIssue,
  EgressMatch,
  EgressNodeType,
  EgressRuleData,
  EgressStrategy
} from "@/lib/api/egress";
import { cn } from "@/lib/utils";

/**
 * 出海网关画布的节点检查器：选中一个节点时右侧显示它的配置。改动立刻写回节点表单（进撤销历史）。
 * 网关侧（网关设置、路由规则、网关线路）在这里，mihomo 侧在 egress-inspector-mihomo。
 */

export function EgressInspector({
  node,
  issues,
  context,
  onDuplicate,
  onDelete
}: {
  node: WorkflowNodeEntity;
  issues: EgressIssue[];
  context: InspectorContext;
  onDuplicate: () => void;
  onDelete: () => void;
}) {
  const { data: rawData, updateData } = useNodeRender(node);
  const type = node.flowNodeType as EgressNodeType;
  const spec = context.catalog.nodes.find((candidate) => candidate.type === type);
  const visual = NODE_VISUAL[type];
  const data = (rawData ?? {}) as Record<string, unknown>;
  const patch: Patch = (next) => updateData({ ...data, ...next });

  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={visual.icon} tone={visual.tone} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold">{nodeTitle(type, data)}</div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{spec?.hint}</p>
        </div>
        <div className="flex shrink-0 gap-1">
          {!spec?.single ? (
            <Button size="icon" variant="ghost" className="size-7" onClick={onDuplicate} aria-label="复制节点">
              <Copy className="size-3.5" />
            </Button>
          ) : null}
          <Button size="icon" variant="ghost" className="size-7 hover:text-destructive" onClick={onDelete} aria-label="删除节点">
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      {issues.length ? (
        <div className="space-y-1.5">
          {issues.map((issue, index) => (
            <div
              key={index}
              className={cn(
                "rounded-lg px-2.5 py-1.5 text-xs leading-5",
                issue.level === "error" ? "bg-red-500/10 text-red-700 dark:text-red-300" : "bg-amber-500/10 text-amber-700 dark:text-amber-300"
              )}
            >
              {issue.message}
            </div>
          ))}
        </div>
      ) : null}

      {type === "gateway" ? <GatewayInspector node={node} data={data as unknown as EgressGatewayData} patch={patch} context={context} /> : null}
      {type === "rule" ? <RuleInspector node={node} data={data as unknown as EgressRuleData} patch={patch} context={context} /> : null}
      {type === "endpoint" ? <EndpointInspector data={data as unknown as EgressEndpoint} patch={patch} context={context} /> : null}
      {type === "m_proxy" ? <ProxyInspector data={data} patch={patch} context={context} /> : null}
      {type === "m_group" ? <GroupInspector node={node} data={data} patch={patch} context={context} /> : null}
      {type === "m_provider" ? <ProviderInspector data={data} patch={patch} context={context} /> : null}
      {type === "m_rules" ? <RulesInspector data={data} patch={patch} context={context} /> : null}
      {type === "m_core" ? <CoreInspector data={data} patch={patch} context={context} /> : null}

      {type !== "m_rules" ? (
        <Field label="备注" hint="只显示在画布上，不影响路由">
          <Textarea rows={2} className="min-h-0 text-xs" value={(type === "endpoint" ? (data.remark as string) : (data.note as string)) ?? ""} onChange={(event) => patch(type === "endpoint" ? { remark: event.target.value } : { note: event.target.value })} />
        </Field>
      ) : null}
    </div>
  );
}

function ActionSegment({ value, onChange }: { value: EgressAction; onChange: (value: EgressAction) => void }) {
  const tone: Record<EgressAction, string> = {
    proxy: "data-[on=true]:bg-emerald-500/12 data-[on=true]:text-emerald-700 dark:data-[on=true]:text-emerald-300",
    direct: "data-[on=true]:bg-sky-500/12 data-[on=true]:text-sky-700 dark:data-[on=true]:text-sky-300",
    reject: "data-[on=true]:bg-red-500/12 data-[on=true]:text-red-700 dark:data-[on=true]:text-red-300"
  };
  return (
    <div className="grid grid-cols-3 gap-1 rounded-xl bg-muted/60 p-1">
      {(["proxy", "direct", "reject"] as EgressAction[]).map((action) => (
        <button
          key={action}
          type="button"
          data-on={value === action}
          onClick={() => onChange(action)}
          className={cn("rounded-lg py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground data-[on=true]:shadow-sm", tone[action])}
        >
          {ACTION_LABEL[action]}
        </button>
      ))}
    </div>
  );
}

function StrategySelect({ value, catalog, onChange }: { value?: EgressStrategy; catalog: EgressGraphCatalog; onChange: (value: EgressStrategy) => void }) {
  return (
    <OptionSelect
      value={value ?? "failover"}
      options={catalog.strategies}
      labels={STRATEGY_LABEL}
      allowNone={false}
      onChange={(next) => onChange((next ?? "failover") as EgressStrategy)}
    />
  );
}

// ── 网关设置 ──

function GatewayInspector({ node, data, patch, context }: { node: WorkflowNodeEntity; data: EgressGatewayData; patch: Patch; context: InspectorContext }) {
  const targets = useConnectedTargets(node, "fallback", data.order, context.revision);
  const health = data.health ?? ({ enabled: true, passiveEnabled: true } as EgressHealth);
  const setHealth = (next: Partial<EgressHealth>) => patch({ health: { ...health, ...next } });
  return (
    <div className="space-y-5">
      <ToggleRow label="启用出海网关" hint="关闭后平台所有出站一律直连（仍尊重 HTTP_PROXY 环境变量）" checked={data.enabled} onChange={(enabled) => patch({ enabled })} />

      <section className="space-y-3">
        <SectionTitle>未命中任何规则时</SectionTitle>
        <ActionSegment value={data.defaultAction ?? "direct"} onChange={(defaultAction) => patch({ defaultAction })} />
        {(data.defaultAction ?? "direct") === "proxy" ? (
          <>
            <TargetOrder
              targets={targets}
              onChange={(order) => patch({ order })}
              empty="从卡片右侧「未命中」拖线到线路、策略组或 mihomo 分流"
              hint="按顺序是故障转移的优先级；其它策略下顺序无关"
            />
            {targets.length > 1 ? (
              <Field label="选路策略">
                <StrategySelect value={data.defaultStrategy} catalog={context.catalog} onChange={(defaultStrategy) => patch({ defaultStrategy })} />
              </Field>
            ) : null}
          </>
        ) : (
          <p className="text-[11px] leading-4 text-muted-foreground">
            {(data.defaultAction ?? "direct") === "direct" ? "白名单出海：只有命中规则的流量才走代理，推荐。" : "未命中的出站一律拒绝，只适合严格管控的部署。"}
          </p>
        )}
      </section>

      <section className="space-y-3">
        <SectionTitle>健康检查</SectionTitle>
        <div className="space-y-1.5 rounded-xl border p-2.5">
          <ToggleRow label="主动探测" hint="定时经每条线路请求探测地址" checked={health.enabled} onChange={(enabled) => setHealth({ enabled })} />
          <ToggleRow label="被动熔断" hint="拨号失败即计数，达到阈值进入冷却" checked={health.passiveEnabled} onChange={(passiveEnabled) => setHealth({ passiveEnabled })} />
          <ToggleRow label="全部不健康时仍然尝试" hint="关掉意味着探测误判会直接切断出海" checked={health.allowUnhealthy ?? true} onChange={(allowUnhealthy) => setHealth({ allowUnhealthy })} />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="探测间隔">
            <OptionalNumber value={health.intervalSeconds} onChange={(intervalSeconds) => setHealth({ intervalSeconds })} suffix="秒" placeholder="60" />
          </Field>
          <Field label="探测超时">
            <OptionalNumber value={health.timeoutSeconds} onChange={(timeoutSeconds) => setHealth({ timeoutSeconds })} suffix="秒" placeholder="8" />
          </Field>
          <Field label="失败几次判死">
            <OptionalNumber value={health.failureThreshold} onChange={(failureThreshold) => setHealth({ failureThreshold })} placeholder="2" />
          </Field>
          <Field label="成功几次恢复">
            <OptionalNumber value={health.successThreshold} onChange={(successThreshold) => setHealth({ successThreshold })} placeholder="1" />
          </Field>
          <Field label="冷却时长">
            <OptionalNumber value={health.cooldownSeconds} onChange={(cooldownSeconds) => setHealth({ cooldownSeconds })} suffix="秒" placeholder="30" />
          </Field>
        </div>
        <Field label="探测地址">
          <Input className="h-8 font-mono text-xs" value={health.probeUrl ?? ""} placeholder={context.catalog.defaultProbeUrl} onChange={(event) => setHealth({ probeUrl: event.target.value })} />
        </Field>
      </section>

      <section className="space-y-3">
        <SectionTitle>连接参数</SectionTitle>
        <div className="grid grid-cols-2 gap-3">
          <Field label="拨号超时">
            <OptionalNumber value={data.dialTimeoutMs} onChange={(dialTimeoutMs) => patch({ dialTimeoutMs })} suffix="毫秒" placeholder="10000" />
          </Field>
          <Field label="TLS 握手超时">
            <OptionalNumber value={data.tlsHandshakeTimeoutMs} onChange={(tlsHandshakeTimeoutMs) => patch({ tlsHandshakeTimeoutMs })} suffix="毫秒" placeholder="10000" />
          </Field>
          <Field label="响应头超时">
            <OptionalNumber value={data.responseHeaderTimeoutMs} onChange={(responseHeaderTimeoutMs) => patch({ responseHeaderTimeoutMs })} suffix="毫秒" placeholder="30000" />
          </Field>
          <Field label="空闲连接保留">
            <OptionalNumber value={data.idleConnTimeoutMs} onChange={(idleConnTimeoutMs) => patch({ idleConnTimeoutMs })} suffix="毫秒" placeholder="90000" />
          </Field>
          <Field label="每目标空闲连接">
            <OptionalNumber value={data.maxIdleConnsPerHost} onChange={(maxIdleConnsPerHost) => patch({ maxIdleConnsPerHost })} placeholder="8" />
          </Field>
        </div>
      </section>
    </div>
  );
}

// ── 路由规则 ──

const MATCH_FIELDS: Array<{ key: keyof EgressMatch; label: string; placeholder: string; hint?: string }> = [
  { key: "domainSuffixes", label: "域名后缀", placeholder: "stripe.com\ngoogleapis.com", hint: "按标签边界匹配：google.com 命中 www.google.com，不命中 notgoogle.com" },
  { key: "domains", label: "完整域名", placeholder: "api.openai.com" },
  { key: "domainKeywords", label: "域名关键字", placeholder: "google" },
  { key: "domainRegexps", label: "域名正则", placeholder: "^api\\d+\\.example\\.com$" },
  { key: "cidrs", label: "目标网段", placeholder: "8.8.8.0/24", hint: "只对直接写 IP 的目标生效" },
  { key: "excludeDomainSuffixes", label: "排除后缀", placeholder: "cn.example.com", hint: "优先于上面的条件，命中就不走这条规则" }
];

function RuleInspector({ node, data, patch, context }: { node: WorkflowNodeEntity; data: EgressRuleData; patch: Patch; context: InspectorContext }) {
  const targets = useConnectedTargets(node, "hit", data.order, context.revision);
  const match = data.match ?? {};
  const setMatch = (next: Partial<EgressMatch>) => patch({ match: { ...match, ...next } });
  const action = data.action ?? "proxy";
  const profiles = match.profiles ?? [];
  return (
    <div className="space-y-5">
      <div className="grid grid-cols-[1fr_auto] items-end gap-3">
        <Field label="规则名称">
          <Input className="h-8 text-xs" value={data.name ?? ""} onChange={(event) => patch({ name: event.target.value })} />
        </Field>
        <ToggleRow label="启用" checked={data.enabled !== false} onChange={(enabled) => patch({ enabled })} />
      </div>
      {context.ruleStat ? (
        <div className="flex items-center justify-between rounded-lg bg-muted/60 px-3 py-2 text-xs">
          <span className="text-muted-foreground">本进程启动以来命中</span>
          <span className="font-semibold tabular-nums">{context.ruleStat.matched.toLocaleString()} 次</span>
        </div>
      ) : null}

      <section className="space-y-3">
        <SectionTitle>命中后</SectionTitle>
        <ActionSegment value={action} onChange={(next) => patch({ action: next })} />
        {action === "proxy" ? (
          <>
            <TargetOrder targets={targets} onChange={(order) => patch({ order })} empty="从卡片右侧「命中」拖线到线路、策略组、节点或 mihomo 分流" hint="按顺序是故障转移的优先级" />
            {targets.length > 1 ? (
              <Field label="选路策略">
                <StrategySelect value={data.strategy} catalog={context.catalog} onChange={(strategy) => patch({ strategy })} />
              </Field>
            ) : null}
          </>
        ) : null}
      </section>

      <section className="space-y-3">
        <SectionTitle aside={<ToggleRow label="匹配全部" checked={Boolean(match.matchAll)} onChange={(matchAll) => setMatch({ matchAll })} />}>匹配条件</SectionTitle>
        <p className="text-[11px] leading-4 text-muted-foreground">不同条件之间是「同时满足」，同一条件里的多项是「任一」。</p>
        {MATCH_FIELDS.map((field) => (
          <Field key={field.key} label={field.label} hint={field.hint}>
            <ListInput value={match[field.key]} rows={field.key === "domainSuffixes" ? 4 : 2} placeholder={field.placeholder} onChange={(items) => setMatch({ [field.key]: items.length ? items : undefined })} />
          </Field>
        ))}
        <div className="grid grid-cols-2 gap-3">
          <Field label="目标端口">
            <Input
              className="h-8 font-mono text-xs"
              placeholder="443, 8443"
              value={(match.ports ?? []).join(", ")}
              onChange={(event) =>
                setMatch({
                  ports: event.target.value
                    .split(/[,\s]+/)
                    .map((item) => Number.parseInt(item, 10))
                    .filter((item) => Number.isFinite(item) && item > 0 && item <= 65535)
                })
              }
            />
          </Field>
          <Field label="协议">
            <div className="flex h-8 items-center gap-1">
              {["https", "http", "tcp"].map((scheme) => {
                const on = (match.schemes ?? []).includes(scheme);
                return (
                  <button
                    key={scheme}
                    type="button"
                    onClick={() => setMatch({ schemes: on ? (match.schemes ?? []).filter((item) => item !== scheme) : [...(match.schemes ?? []), scheme] })}
                    className={cn("h-7 flex-1 rounded-md border font-mono text-[11px] transition-colors", on ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground hover:text-foreground")}
                  >
                    {scheme}
                  </button>
                );
              })}
            </div>
          </Field>
        </div>
        <Field
          label="调用方"
          hint="平台里每个出站客户端都有调用方标识，可以让同一个域名在不同业务走不同线路"
        >
          <div className="space-y-1.5">
            <div className="flex flex-wrap gap-1">
              {profiles.map((profile) => (
                <span key={profile} className="inline-flex items-center gap-1 rounded-md bg-violet-500/10 px-1.5 font-mono text-[11px] leading-6 text-violet-700 dark:text-violet-300">
                  {profile}
                  <button type="button" onClick={() => setMatch({ profiles: profiles.filter((item) => item !== profile) })} aria-label="移除">
                    ×
                  </button>
                </span>
              ))}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="sm" variant="outline" className="h-6 gap-1 px-2 text-[11px]">
                    <Plus className="size-3" /> 添加
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="max-h-80 w-64 overflow-y-auto">
                  <DropdownMenuLabel className="text-xs">平台里的调用方</DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  {context.catalog.profiles
                    .filter((option) => !profiles.includes(option.name))
                    .map((option) => (
                      <DropdownMenuItem key={option.name} className="justify-between gap-2 text-xs" onSelect={() => setMatch({ profiles: [...profiles, option.name] })}>
                        <span className="truncate">{option.label}</span>
                        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{option.name}</span>
                      </DropdownMenuItem>
                    ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <Input
              className="h-8 font-mono text-xs"
              placeholder="手动输入后回车，如 payment.*"
              onKeyDown={(event) => {
                if (event.key !== "Enter") return;
                const value = event.currentTarget.value.trim().toLowerCase();
                if (value && !profiles.includes(value)) setMatch({ profiles: [...profiles, value] });
                event.currentTarget.value = "";
              }}
            />
          </div>
        </Field>
      </section>
    </div>
  );
}

// ── 网关线路 ──

function EndpointInspector({ data, patch, context }: { data: EgressEndpoint; patch: Patch; context: InspectorContext }) {
  const protocol = data.protocol ?? "socks5h";
  const secretKept = context.catalog.secretKept;
  const tls = data.tls ?? { enabled: false };
  const ssh = data.ssh ?? {};
  const shadowsocks = data.shadowsocks ?? {};
  const stat = context.endpointStat;
  const auth = ["http", "https", "socks5", "socks5h"].includes(protocol);
  const tlsOptional = ["http", "socks5", "socks5h"].includes(protocol);
  const tlsActive = protocol === "https" || protocol === "trojan" || tls.enabled;
  return (
    <div className="space-y-5">
      {stat ? (
        <div className="grid grid-cols-3 gap-2 rounded-xl border p-2.5 text-center">
          <Metric label="状态" value={stat.healthy ? "可用" : "不可用"} tone={stat.healthy ? "text-emerald-600 dark:text-emerald-400" : "text-red-600 dark:text-red-400"} />
          <Metric label="延迟" value={stat.latencyMs ? `${stat.latencyMs} ms` : "—"} />
          <Metric label="连接" value={`${stat.successes}/${stat.dials}`} />
        </div>
      ) : null}
      <div className="grid grid-cols-[1fr_auto] items-end gap-3">
        <Field label="线路名称" hint="字母、数字、下划线、连字符和点">
          <Input className="h-8 font-mono text-xs" value={data.name ?? ""} onChange={(event) => patch({ name: event.target.value })} />
        </Field>
        <ToggleRow label="启用" checked={data.enabled !== false} onChange={(enabled) => patch({ enabled })} />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field label="协议">
          <OptionSelect value={protocol} options={context.catalog.protocols} labels={PROTOCOL_LABEL} allowNone={false} onChange={(next) => patch({ protocol: next ?? "socks5h" })} />
        </Field>
        {protocol !== "direct" ? (
          <Field label="地址">
            <Input className="h-8 font-mono text-xs" placeholder="host:port" value={data.address ?? ""} onChange={(event) => patch({ address: event.target.value })} />
          </Field>
        ) : null}
      </div>
      {auth || protocol === "trojan" ? (
        <div className="grid grid-cols-2 gap-3">
          {auth ? (
            <Field label="用户名">
              <Input className="h-8 text-xs" value={data.username ?? ""} onChange={(event) => patch({ username: event.target.value })} />
            </Field>
          ) : null}
          <Field label="密码" className={auth ? undefined : "col-span-2"}>
            <SecretInput value={data.password} secretKept={secretKept} onChange={(password) => patch({ password })} />
          </Field>
        </div>
      ) : null}

      {protocol === "shadowsocks" ? (
        <section className="space-y-3">
          <SectionTitle>Shadowsocks</SectionTitle>
          <div className="grid grid-cols-2 gap-3">
            <Field label="加密方式">
              <OptionSelect value={shadowsocks.method ?? "aes-256-gcm"} options={context.catalog.shadowsocksMethods} allowNone={false} onChange={(method) => patch({ shadowsocks: { ...shadowsocks, method } })} />
            </Field>
            <Field label="密码">
              <SecretInput value={shadowsocks.password} secretKept={secretKept} onChange={(password) => patch({ shadowsocks: { ...shadowsocks, password } })} />
            </Field>
          </div>
        </section>
      ) : null}

      {protocol === "ssh" ? (
        <section className="space-y-3">
          <SectionTitle>SSH</SectionTitle>
          <div className="grid grid-cols-2 gap-3">
            <Field label="用户">
              <Input className="h-8 text-xs" value={ssh.user ?? ""} onChange={(event) => patch({ ssh: { ...ssh, user: event.target.value } })} />
            </Field>
            <Field label="密码">
              <SecretInput value={ssh.password} secretKept={secretKept} onChange={(password) => patch({ ssh: { ...ssh, password } })} />
            </Field>
          </div>
          <Field label="私钥" hint="PEM 全文，与密码二选一">
            <SecretInput multiline value={ssh.privateKeyPem} secretKept={secretKept} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" onChange={(privateKeyPem) => patch({ ssh: { ...ssh, privateKeyPem } })} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="私钥口令">
              <SecretInput value={ssh.passphrase} secretKept={secretKept} onChange={(passphrase) => patch({ ssh: { ...ssh, passphrase } })} />
            </Field>
            <Field label="保活间隔">
              <OptionalNumber value={ssh.keepAliveSeconds} suffix="秒" onChange={(keepAliveSeconds) => patch({ ssh: { ...ssh, keepAliveSeconds } })} />
            </Field>
          </div>
          <Field label="主机指纹" hint="SHA256:… 留空不校验，只应在可控的内网跳板上这样做">
            <Input className="h-8 font-mono text-xs" value={ssh.hostKeyFingerprint ?? ""} onChange={(event) => patch({ ssh: { ...ssh, hostKeyFingerprint: event.target.value } })} />
          </Field>
        </section>
      ) : null}

      {protocol !== "direct" && protocol !== "ssh" && protocol !== "shadowsocks" ? (
        <section className="space-y-3">
          <SectionTitle aside={tlsOptional ? <ToggleRow label="TLS" checked={tls.enabled} onChange={(enabled) => patch({ tls: { ...tls, enabled } })} /> : <Badge size="sm" variant="secondary">TLS 必开</Badge>}>
            TLS
          </SectionTitle>
          {tlsActive ? (
            <>
              <div className="grid grid-cols-2 gap-3">
                <Field label="SNI">
                  <Input className="h-8 font-mono text-xs" placeholder="留空用地址" value={tls.serverName ?? ""} onChange={(event) => patch({ tls: { ...tls, serverName: event.target.value } })} />
                </Field>
                <Field label="最低版本">
                  <OptionSelect value={tls.minVersion} options={["1.2", "1.3"]} noneLabel="默认（1.2）" onChange={(minVersion) => patch({ tls: { ...tls, minVersion } })} />
                </Field>
              </div>
              <ToggleRow label="跳过证书校验" hint="只用于自签名证书的自建跳板" checked={Boolean(tls.insecureSkipVerify)} onChange={(insecureSkipVerify) => patch({ tls: { ...tls, insecureSkipVerify } })} />
              <Field label="ALPN">
                <ListInput value={tls.alpn} rows={2} placeholder="h2" onChange={(alpn) => patch({ tls: { ...tls, alpn } })} />
              </Field>
              <Field label="自定义 CA" hint="PEM，服务端证书由私有 CA 签发时填写">
                <Textarea className="min-h-16 font-mono text-xs" value={tls.caPem ?? ""} onChange={(event) => patch({ tls: { ...tls, caPem: event.target.value } })} spellCheck={false} />
              </Field>
              <Field label="客户端证书" hint="双向 TLS，与私钥一起填">
                <Textarea className="min-h-16 font-mono text-xs" value={tls.clientCertPem ?? ""} onChange={(event) => patch({ tls: { ...tls, clientCertPem: event.target.value } })} spellCheck={false} />
              </Field>
              <Field label="客户端私钥">
                <SecretInput multiline value={tls.clientKeyPem} secretKept={secretKept} onChange={(clientKeyPem) => patch({ tls: { ...tls, clientKeyPem } })} />
              </Field>
            </>
          ) : null}
        </section>
      ) : null}

      <section className="space-y-3">
        <SectionTitle>选路与探测</SectionTitle>
        <div className="grid grid-cols-2 gap-3">
          <Field label="权重" hint="加权随机时用">
            <OptionalNumber value={data.weight} placeholder="1" onChange={(weight) => patch({ weight })} />
          </Field>
          <Field label="地区">
            <Input className="h-8 text-xs" placeholder="hk / us / jp" value={data.region ?? ""} onChange={(event) => patch({ region: event.target.value })} />
          </Field>
          <Field label="拨号超时">
            <OptionalNumber value={data.dialTimeoutMs} suffix="毫秒" placeholder="跟随网关" onChange={(dialTimeoutMs) => patch({ dialTimeoutMs })} />
          </Field>
          <Field label="探测地址">
            <Input className="h-8 font-mono text-xs" placeholder="跟随网关" value={data.probeUrl ?? ""} onChange={(event) => patch({ probeUrl: event.target.value })} />
          </Field>
        </div>
        {protocol === "http" ? (
          <ToggleRow label="明文请求用转发模式" hint="部分企业代理禁止 CONNECT 到 80 端口时打开" checked={Boolean(data.httpForwardMode)} onChange={(httpForwardMode) => patch({ httpForwardMode })} />
        ) : null}
        {protocol === "http" || protocol === "https" ? (
          <Field label="CONNECT 附加请求头">
            <KeyValueInput value={data.headers} placeholder="Proxy-Authorization: Basic …" onChange={(headers) => patch({ headers })} />
          </Field>
        ) : null}
        <p className="text-[11px] leading-4 text-muted-foreground">要先经过另一条线路（跳板 → 落地），从卡片右侧「经由」拖线到上一跳。</p>
      </section>
    </div>
  );
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className={cn("text-sm font-semibold tabular-nums", tone)}>{value}</div>
    </div>
  );
}
