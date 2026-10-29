"use client";

import { useMemo, useState } from "react";
import type { WorkflowNodeEntity } from "@flowgram.ai/free-layout-editor";
import { ArrowDown, ArrowUp, Gauge, LibraryBig, Plus, Wand2 } from "lucide-react";
import { Collapsible, Field } from "@/components/apps/item-grant/item-grant-fields";
import {
  OptionSelect,
  OptionalNumber,
  RemoveButton,
  SectionTitle,
  SpecForm,
  ToggleRow,
  YamlEditor
} from "@/components/configuration/egress/egress-fields";
import { TargetOrder, useConnectedTargets, type InspectorContext, type Patch } from "@/components/configuration/egress/egress-inspector-shared";
import { defaultProxy, shortCode } from "@/components/configuration/egress/egress-meta";
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
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import type { MihomoCoreData, MihomoGroupData, MihomoProviderData, MihomoProxyData, MihomoRuleRow, MihomoRulesData } from "@/lib/api/egress";
import { cn } from "@/lib/utils";

/**
 * mihomo 侧节点的检查器：节点、策略组、订阅、分流、核心。
 * 表单只覆盖常用字段，「YAML」页签可以编辑全部键（核心支持的东西都能写）。
 */

function FormYamlTabs({ form, yaml }: { form: React.ReactNode; yaml: React.ReactNode }) {
  return (
    <Tabs defaultValue="form" className="gap-4">
      <TabsList className="w-full">
        <TabsTrigger value="form" className="flex-1 text-xs">
          表单
        </TabsTrigger>
        <TabsTrigger value="yaml" className="flex-1 text-xs">
          YAML
        </TabsTrigger>
      </TabsList>
      <TabsContent value="form">{form}</TabsContent>
      <TabsContent value="yaml">{yaml}</TabsContent>
    </Tabs>
  );
}

// ── 节点 ──

export function ProxyInspector({ data, patch, context }: { data: Record<string, unknown>; patch: Patch; context: InspectorContext }) {
  const proxy = ((data as MihomoProxyData).proxy ?? {}) as Record<string, unknown>;
  const catalog = context.catalog;
  const type = String(proxy.type ?? "");
  const spec = catalog.mihomo.proxyTypes.find((item) => item.type === type);
  const setProxy = (next: Record<string, unknown>) => patch({ proxy: next });
  const name = String(proxy.name ?? "");

  const changeType = (next: string) => {
    // 换协议时保留名称与服务器，其余按新协议的默认值重来（不同协议的字段大多不通用）。
    const fresh = defaultProxy(next, catalog, name);
    const nextSpec = catalog.mihomo.proxyTypes.find((item) => item.type === next);
    if (nextSpec?.server) Object.assign(fresh, { server: proxy.server ?? "", port: proxy.port ?? 443 });
    setProxy(fresh);
  };

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-[1fr_auto] items-end gap-3">
        <Field label="名称" hint="策略组成员与分流目标都按名称引用">
          <Input className="h-8 text-xs" value={name} onChange={(event) => setProxy({ ...proxy, name: event.target.value })} />
        </Field>
        {context.onDelay && name ? (
          <Button size="sm" variant="outline" className="h-8 gap-1 text-xs" onClick={() => context.onDelay?.(name, false)}>
            <Gauge /> 测速
          </Button>
        ) : null}
      </div>
      <FormYamlTabs
        form={
          <div className="space-y-5">
            <Field label="协议">
              <Select value={type || "vless"} onValueChange={changeType}>
                <SelectTrigger size="sm" className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {catalog.mihomo.proxyTypes.map((item) => (
                    <SelectItem key={item.type} value={item.type} className="text-xs">
                      {item.label}
                    </SelectItem>
                  ))}
                  {type && !spec ? (
                    <SelectItem value={type} className="text-xs">
                      {type}（在 YAML 里编辑）
                    </SelectItem>
                  ) : null}
                </SelectContent>
              </Select>
            </Field>
            {spec?.hint ? <p className="-mt-3 text-[11px] leading-4 text-muted-foreground">{spec.hint}</p> : null}
            {spec?.server || (!spec && proxy.server !== undefined) ? (
              <div className="grid grid-cols-[1fr_96px] gap-3">
                <Field label={<>服务器<span className="ml-0.5 text-red-500">*</span></>}>
                  <Input className="h-8 font-mono text-xs" placeholder="example.com" value={String(proxy.server ?? "")} onChange={(event) => setProxy({ ...proxy, server: event.target.value })} />
                </Field>
                <Field label="端口">
                  <OptionalNumber value={proxy.port} onChange={(port) => setProxy({ ...proxy, port })} />
                </Field>
              </div>
            ) : null}
            {spec ? (
              <SpecForm fields={spec.fields} values={proxy} onChange={setProxy} secretKept={catalog.secretKept} />
            ) : (
              <p className="rounded-lg bg-muted/60 px-3 py-2 text-xs leading-5 text-muted-foreground">这种协议没有表单，在「YAML」里填写全部字段；核心会在应用时校验。</p>
            )}
            <p className="text-[11px] leading-4 text-muted-foreground">要先经过另一个节点或策略组再连出去（前置代理），从卡片右侧「前置代理」拖线。</p>
          </div>
        }
        yaml={
          <YamlEditor
            value={proxy}
            onCommit={setProxy}
            rows={18}
            hint={`节点的全部字段（mihomo 原样）。前置代理（dialer-proxy）由连线决定，这里写了会被覆盖。显示 ${catalog.secretKept} 的密钥原样保留即保持原值。`}
          />
        }
      />
    </div>
  );
}

// ── 策略组 ──

export function GroupInspector({ node, data, patch, context }: { node: WorkflowNodeEntity; data: Record<string, unknown>; patch: Patch; context: InspectorContext }) {
  const groupData = data as unknown as MihomoGroupData;
  const group = (groupData.group ?? {}) as Record<string, unknown>;
  const catalog = context.catalog;
  const type = String(group.type ?? "select");
  const spec = catalog.mihomo.groupTypes.find((item) => item.type === type);
  const members = useConnectedTargets(node, "members", groupData.order, context.revision);
  const builtins = groupData.builtins ?? [];
  const setGroup = (next: Record<string, unknown>) => patch({ group: next });
  const name = String(group.name ?? "");

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-[1fr_auto] items-end gap-3">
        <Field label="名称">
          <Input className="h-8 text-xs" value={name} onChange={(event) => setGroup({ ...group, name: event.target.value })} />
        </Field>
        {context.onDelay && name ? (
          <Button size="sm" variant="outline" className="h-8 gap-1 text-xs" onClick={() => context.onDelay?.(name, true)}>
            <Gauge /> 全组测速
          </Button>
        ) : null}
      </div>
      <FormYamlTabs
        form={
          <div className="space-y-5">
            <div className="grid grid-cols-2 gap-1.5">
              {catalog.mihomo.groupTypes.map((item) => (
                <button
                  key={item.type}
                  type="button"
                  onClick={() => setGroup({ ...group, type: item.type })}
                  className={cn(
                    "rounded-xl border px-2.5 py-2 text-left transition-colors",
                    item.type === type ? "border-amber-500/70 bg-amber-500/8" : "hover:border-foreground/20 hover:bg-accent/40"
                  )}
                >
                  <div className="text-xs font-medium">{item.label}</div>
                  <div className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{item.hint}</div>
                </button>
              ))}
            </div>

            <section className="space-y-3">
              <SectionTitle>成员</SectionTitle>
              <TargetOrder
                targets={members}
                onChange={(order) => patch({ order })}
                empty="从卡片右侧「成员」拖线到节点、策略组或订阅"
                hint={type === "fallback" || type === "relay" ? "顺序就是优先级（链式中继是转发顺序）" : type === "select" ? "第一个成员是默认选项" : undefined}
              />
              <div className="space-y-1.5">
                <div className="text-xs font-medium">附加内置出站</div>
                <div className="flex flex-wrap gap-1">
                  {catalog.mihomo.builtins.map((builtin) => {
                    const on = builtins.includes(builtin);
                    return (
                      <button
                        key={builtin}
                        type="button"
                        onClick={() => patch({ builtins: on ? builtins.filter((item) => item !== builtin) : [...builtins, builtin] })}
                        className={cn(
                          "h-7 rounded-md border px-2 font-mono text-[11px] transition-colors",
                          on ? "border-amber-500/70 bg-amber-500/10 text-amber-700 dark:text-amber-300" : "text-muted-foreground hover:text-foreground"
                        )}
                      >
                        {builtin}
                      </button>
                    );
                  })}
                </div>
                {builtins.length ? (
                  <ToggleRow label="内置出站排在最前" checked={Boolean(groupData.builtinsFirst)} onChange={(builtinsFirst) => patch({ builtinsFirst })} />
                ) : null}
              </div>
            </section>
            <SpecForm fields={spec?.fields} values={group} onChange={setGroup} secretKept={catalog.secretKept} />
          </div>
        }
        yaml={
          <YamlEditor
            value={group}
            omit={["proxies", "use"]}
            onCommit={(next) => setGroup(next)}
            rows={16}
            hint="策略组的全部字段。成员（proxies / use）由连线与内置出站决定，不在这里写。"
          />
        }
      />
    </div>
  );
}

// ── 订阅 ──

export function ProviderInspector({ data, patch, context }: { data: Record<string, unknown>; patch: Patch; context: InspectorContext }) {
  const providerData = data as unknown as MihomoProviderData;
  const provider = (providerData.provider ?? {}) as Record<string, unknown>;
  const setProvider = (next: Record<string, unknown>) => patch({ provider: next });
  return (
    <div className="space-y-5">
      <Field label="名称" hint="策略组的「成员」连到订阅即引用它的全部节点（可在策略组里再按正则筛选）">
        <Input className="h-8 text-xs" value={providerData.name ?? ""} onChange={(event) => patch({ name: event.target.value })} />
      </Field>
      <FormYamlTabs
        form={<SpecForm fields={context.catalog.mihomo.providerFields} values={provider} onChange={setProvider} secretKept={context.catalog.secretKept} />}
        yaml={<YamlEditor value={provider} onCommit={setProvider} rows={16} hint="订阅的全部字段（mihomo 原样），如 override 里的 additional-prefix、payload 内联节点等。" />}
      />
      <p className="text-[11px] leading-4 text-muted-foreground">订阅节点要先经过某个节点或策略组（前置代理），从卡片右侧「前置代理」拖线，会写进 override.dialer-proxy。</p>
    </div>
  );
}

// ── 分流 ──

function splitRule(raw: string): MihomoRuleRow | null {
  const text = raw.trim();
  if (!text || text.startsWith("#")) return null;
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    if (char === "(") depth++;
    else if (char === ")") depth--;
    else if (char === "," && depth === 0) {
      parts.push(text.slice(start, i).trim());
      start = i + 1;
    }
  }
  parts.push(text.slice(start).trim());
  const type = parts[0].toUpperCase();
  if (type === "MATCH") return parts[1] ? { id: shortCode(), type, target: parts[1] } : null;
  if (parts.length < 3) return null;
  return { id: shortCode(), type, payload: parts[1], target: parts[2], params: parts.slice(3) };
}

function ruleText(rule: MihomoRuleRow) {
  if (rule.type === "MATCH") return `MATCH,${rule.target}`;
  return [rule.type, rule.payload ?? "", rule.target, ...(rule.params ?? [])].join(",");
}

/** 常用规则集：MetaCubeX meta-rules-dat 的 mrs 格式。 */
const RULE_SET_PRESETS: Array<{ name: string; label: string; behavior: "domain" | "ipcidr"; path: string }> = [
  { name: "openai", label: "OpenAI", behavior: "domain", path: "geo/geosite/openai.mrs" },
  { name: "anthropic", label: "Anthropic", behavior: "domain", path: "geo/geosite/anthropic.mrs" },
  { name: "google", label: "Google", behavior: "domain", path: "geo/geosite/google.mrs" },
  { name: "github", label: "GitHub", behavior: "domain", path: "geo/geosite/github.mrs" },
  { name: "telegram", label: "Telegram", behavior: "domain", path: "geo/geosite/telegram.mrs" },
  { name: "stripe", label: "Stripe", behavior: "domain", path: "geo/geosite/stripe.mrs" },
  { name: "paypal", label: "PayPal", behavior: "domain", path: "geo/geosite/paypal.mrs" },
  { name: "microsoft", label: "Microsoft", behavior: "domain", path: "geo/geosite/microsoft.mrs" },
  { name: "apple", label: "Apple", behavior: "domain", path: "geo/geosite/apple.mrs" },
  { name: "cn", label: "中国大陆域名", behavior: "domain", path: "geo/geosite/cn.mrs" },
  { name: "geolocation-!cn", label: "境外域名", behavior: "domain", path: "geo/geosite/geolocation-!cn.mrs" },
  { name: "cn-ip", label: "中国大陆 IP", behavior: "ipcidr", path: "geo/geoip/cn.mrs" },
  { name: "private-ip", label: "私有网段", behavior: "ipcidr", path: "geo/geoip/private.mrs" }
];

/** 从核心默认的 Geo 地址里取出 GitHub 镜像前缀。 */
function mirrorPrefix(context: InspectorContext) {
  const geoip = context.catalog.defaultCore.settings.geoxUrl?.geoip ?? "";
  const index = geoip.indexOf("https://github.com/");
  return index > 0 ? geoip.slice(0, index) : "";
}

export function RulesInspector({ data, patch, context }: { data: Record<string, unknown>; patch: Patch; context: InspectorContext }) {
  const rulesData = data as unknown as MihomoRulesData;
  const rules = rulesData.rules ?? [];
  const providers = rulesData.ruleProviders ?? {};
  const catalog = context.catalog;
  const targets = useMemo(() => [...context.mihomoTargets, ...catalog.mihomo.builtins], [context.mihomoTargets, catalog.mihomo.builtins]);
  const firstGroup = context.mihomoTargets[0] ?? "DIRECT";
  const setRules = (next: MihomoRuleRow[]) => patch({ rules: next });
  const setProviders = (next: Record<string, Record<string, unknown>>) => patch({ ruleProviders: next });
  const [text, setText] = useState<string | null>(null);

  const insert = (row: MihomoRuleRow) => {
    const matchIndex = rules.findIndex((rule) => rule.type === "MATCH");
    if (matchIndex >= 0 && row.type !== "MATCH") setRules([...rules.slice(0, matchIndex), row, ...rules.slice(matchIndex)]);
    else setRules([...rules, row]);
  };
  const addPresetSet = (preset: (typeof RULE_SET_PRESETS)[number]) => {
    const url = `${mirrorPrefix(context)}https://github.com/MetaCubeX/meta-rules-dat/raw/meta/${preset.path}`;
    setProviders({ ...providers, [preset.name]: { type: "http", behavior: preset.behavior, format: "mrs", url, interval: 86400 } });
    insert({
      id: shortCode(),
      type: "RULE-SET",
      payload: preset.name,
      target: preset.name === "cn" || preset.name === "cn-ip" || preset.name === "private-ip" ? "DIRECT" : firstGroup,
      params: preset.behavior === "ipcidr" ? ["no-resolve"] : []
    });
  };

  return (
    <Tabs defaultValue="table" className="gap-4">
      <TabsList className="w-full">
        <TabsTrigger value="table" className="flex-1 text-xs">
          规则表
        </TabsTrigger>
        <TabsTrigger value="text" className="flex-1 text-xs">
          文本
        </TabsTrigger>
        <TabsTrigger value="sets" className="flex-1 text-xs">
          规则集 {Object.keys(providers).length ? `(${Object.keys(providers).length})` : ""}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="table" className="space-y-3">
        <p className="text-[11px] leading-4 text-muted-foreground">从上往下匹配，第一条命中的生效；「兜底」写在最后。网关规则连到这张表的流量才按它分流。</p>
        <div className="space-y-1.5">
          {rules.map((rule, index) => (
            <RuleRowEditor
              key={rule.id || index}
              rule={rule}
              index={index}
              count={rules.length}
              targets={targets}
              ruleSets={Object.keys(providers)}
              context={context}
              onChange={(next) => setRules(rules.map((item, i) => (i === index ? next : item)))}
              onMove={(delta) => {
                const next = [...rules];
                const [item] = next.splice(index, 1);
                next.splice(index + delta, 0, item);
                setRules(next);
              }}
              onRemove={() => setRules(rules.filter((_, i) => i !== index))}
            />
          ))}
        </div>
        <div className="flex flex-wrap gap-1.5">
          <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" onClick={() => insert({ id: shortCode(), type: "DOMAIN-SUFFIX", payload: "", target: firstGroup })}>
            <Plus /> 添加规则
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="outline" className="h-7 gap-1 text-xs">
                <Wand2 /> 常用规则
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-64">
              <DropdownMenuLabel className="text-xs">插入到兜底之前</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {[
                { label: "私有网段直连", rule: { type: "GEOIP", payload: "private", target: "DIRECT", params: ["no-resolve"] } },
                { label: "中国大陆 IP 直连", rule: { type: "GEOIP", payload: "CN", target: "DIRECT", params: ["no-resolve"] } },
                { label: "中国大陆域名直连", rule: { type: "GEOSITE", payload: "cn", target: "DIRECT" } },
                { label: `OpenAI 走 ${firstGroup}`, rule: { type: "GEOSITE", payload: "openai", target: firstGroup } },
                { label: `Google 走 ${firstGroup}`, rule: { type: "GEOSITE", payload: "google", target: firstGroup } },
                { label: `GitHub 走 ${firstGroup}`, rule: { type: "GEOSITE", payload: "github", target: firstGroup } },
                { label: "广告拦截", rule: { type: "GEOSITE", payload: "category-ads-all", target: "REJECT" } }
              ].map((item) => (
                <DropdownMenuItem key={item.label} className="text-xs" onSelect={() => insert({ id: shortCode(), ...item.rule })}>
                  {item.label}
                </DropdownMenuItem>
              ))}
              {!rules.some((rule) => rule.type === "MATCH") ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem className="text-xs" onSelect={() => setRules([...rules, { id: shortCode(), type: "MATCH", target: firstGroup }])}>
                    兜底走 {firstGroup}
                  </DropdownMenuItem>
                </>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TabsContent>

      <TabsContent value="text" className="space-y-2">
        <Textarea
          rows={18}
          className="min-h-0 font-mono text-xs leading-5"
          spellCheck={false}
          value={text ?? rules.map((rule) => `${rule.enabled === false ? "# " : ""}${ruleText(rule)}`).join("\n")}
          onChange={(event) => setText(event.target.value)}
        />
        <p className="text-[11px] leading-4 text-muted-foreground">一行一条，格式与 mihomo 配置相同：类型,内容,目标[,no-resolve]。以 # 开头的行按停用处理。</p>
        {text !== null ? (
          <div className="flex justify-end gap-1.5">
            <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setText(null)}>
              撤回
            </Button>
            <Button
              size="sm"
              className="h-7 text-xs"
              onClick={() => {
                const next: MihomoRuleRow[] = [];
                for (const line of text.split("\n")) {
                  const disabled = line.trim().startsWith("#");
                  const row = splitRule(disabled ? line.trim().slice(1) : line);
                  if (row) next.push(disabled ? { ...row, enabled: false } : row);
                }
                setRules(next);
                setText(null);
              }}
            >
              应用修改
            </Button>
          </div>
        ) : null}
      </TabsContent>

      <TabsContent value="sets" className="space-y-3">
        <p className="text-[11px] leading-4 text-muted-foreground">规则集由核心定时下载，在规则表里用「规则集」类型引用。</p>
        {Object.entries(providers).map(([name, provider]) => (
          <Collapsible
            key={name}
            defaultOpen={false}
            title={
              <span className="flex items-center gap-1.5">
                <LibraryBig className="size-3.5 text-pink-500" />
                {name}
                <span className="font-normal text-muted-foreground">
                  {String(provider.behavior ?? "")} {String(provider.format ?? "")}
                </span>
              </span>
            }
            aside={
              <RemoveButton
                label="删除规则集"
                onClick={() => {
                  const next = { ...providers };
                  delete next[name];
                  setProviders(next);
                }}
              />
            }
          >
            <SpecForm
              fields={catalog.mihomo.ruleProviderFields}
              values={provider}
              onChange={(next) => setProviders({ ...providers, [name]: next })}
              secretKept={catalog.secretKept}
            />
          </Collapsible>
        ))}
        <div className="flex flex-wrap gap-1.5">
          <NewRuleSetButton existing={Object.keys(providers)} onAdd={(name) => setProviders({ ...providers, [name]: { type: "http", behavior: "domain", format: "mrs", url: "", interval: 86400 } })} />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="sm" variant="outline" className="h-7 gap-1 text-xs">
                <Wand2 /> 常用规则集
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-80 w-60 overflow-y-auto">
              <DropdownMenuLabel className="text-xs">加入规则集并插入一条规则</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {RULE_SET_PRESETS.filter((preset) => !providers[preset.name]).map((preset) => (
                <DropdownMenuItem key={preset.name} className="justify-between text-xs" onSelect={() => addPresetSet(preset)}>
                  {preset.label}
                  <span className="font-mono text-[10px] text-muted-foreground">{preset.name}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TabsContent>
    </Tabs>
  );
}

function NewRuleSetButton({ existing, onAdd }: { existing: string[]; onAdd: (name: string) => void }) {
  const [name, setName] = useState("");
  const valid = name.trim() && !existing.includes(name.trim()) && !name.includes(",");
  return (
    <div className="flex items-center gap-1.5">
      <Input className="h-7 w-32 text-xs" placeholder="规则集名称" value={name} onChange={(event) => setName(event.target.value)} />
      <Button
        size="sm"
        variant="outline"
        className="h-7 gap-1 text-xs"
        disabled={!valid}
        onClick={() => {
          onAdd(name.trim());
          setName("");
        }}
      >
        <Plus /> 添加
      </Button>
    </div>
  );
}

function RuleRowEditor({
  rule,
  index,
  count,
  targets,
  ruleSets,
  context,
  onChange,
  onMove,
  onRemove
}: {
  rule: MihomoRuleRow;
  index: number;
  count: number;
  targets: string[];
  ruleSets: string[];
  context: InspectorContext;
  onChange: (rule: MihomoRuleRow) => void;
  onMove: (delta: number) => void;
  onRemove: () => void;
}) {
  const spec = context.catalog.mihomo.ruleTypes.find((item) => item.type === rule.type);
  const disabled = rule.enabled === false;
  const params = rule.params ?? [];
  const toggleParam = (param: string) => onChange({ ...rule, params: params.includes(param) ? params.filter((item) => item !== param) : [...params, param] });
  return (
    <div className={cn("space-y-1.5 rounded-xl border p-2", disabled && "opacity-55")}>
      <div className="flex items-center gap-1.5">
        <span className="w-5 shrink-0 text-center text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
        <Select value={rule.type} onValueChange={(type) => onChange({ ...rule, type })}>
          <SelectTrigger size="sm" className="h-7 w-[118px] shrink-0 text-[11px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {context.catalog.mihomo.ruleTypes.map((item) => (
              <SelectItem key={item.type} value={item.type} className="text-xs">
                {item.label}
                <span className="ml-1 font-mono text-[10px] text-muted-foreground">{item.type}</span>
              </SelectItem>
            ))}
            {!spec ? (
              <SelectItem value={rule.type} className="text-xs">
                {rule.type}
              </SelectItem>
            ) : null}
          </SelectContent>
        </Select>
        <Select value={rule.target || "DIRECT"} onValueChange={(target) => onChange({ ...rule, target })}>
          <SelectTrigger size="sm" className="h-7 min-w-0 flex-1 text-[11px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectLabel className="text-[11px]">策略组与节点</SelectLabel>
              {targets
                .filter((target) => !context.catalog.mihomo.builtins.includes(target))
                .map((target) => (
                  <SelectItem key={target} value={target} className="text-xs">
                    {target}
                  </SelectItem>
                ))}
            </SelectGroup>
            <SelectGroup>
              <SelectLabel className="text-[11px]">内置</SelectLabel>
              {context.catalog.mihomo.builtins.map((target) => (
                <SelectItem key={target} value={target} className="font-mono text-xs">
                  {target}
                </SelectItem>
              ))}
            </SelectGroup>
            {rule.target && !targets.includes(rule.target) ? (
              <SelectItem value={rule.target} className="text-xs text-red-600">
                {rule.target}（不存在）
              </SelectItem>
            ) : null}
          </SelectContent>
        </Select>
        <Switch checked={!disabled} onCheckedChange={(on) => onChange({ ...rule, enabled: on ? undefined : false })} aria-label="启用" />
      </div>
      <div className="flex items-center gap-1.5 pl-6">
        {spec?.noPayload ? (
          <span className="flex-1 text-[11px] text-muted-foreground">{spec.hint}</span>
        ) : rule.type === "RULE-SET" && ruleSets.length ? (
          <div className="min-w-0 flex-1">
            <OptionSelect value={rule.payload} options={ruleSets} allowNone={false} onChange={(payload) => onChange({ ...rule, payload })} />
          </div>
        ) : (
          <Input className="h-7 min-w-0 flex-1 font-mono text-[11px]" placeholder={spec?.example} value={rule.payload ?? ""} onChange={(event) => onChange({ ...rule, payload: event.target.value })} />
        )}
        {(spec?.params ?? []).map((param) => (
          <button
            key={param}
            type="button"
            onClick={() => toggleParam(param)}
            className={cn("h-7 shrink-0 rounded-md border px-1.5 font-mono text-[10px]", params.includes(param) ? "border-pink-500/60 bg-pink-500/10 text-pink-700 dark:text-pink-300" : "text-muted-foreground")}
            title={param === "no-resolve" ? "目标是域名时不解析成 IP 再匹配" : "按来源地址匹配"}
          >
            {param}
          </button>
        ))}
        <Button size="icon" variant="ghost" className="size-7" disabled={index === 0} onClick={() => onMove(-1)} aria-label="上移">
          <ArrowUp className="size-3" />
        </Button>
        <Button size="icon" variant="ghost" className="size-7" disabled={index === count - 1} onClick={() => onMove(1)} aria-label="下移">
          <ArrowDown className="size-3" />
        </Button>
        <RemoveButton label="删除规则" onClick={onRemove} />
      </div>
    </div>
  );
}

// ── 核心 ──

const MODE_OPTIONS = [
  { value: "rule", label: "规则", hint: "按分流表走" },
  { value: "global", label: "全局", hint: "交给核心的流量全走 GLOBAL 组" },
  { value: "direct", label: "直连", hint: "核心直连一切（排障用）" }
];

export function CoreInspector({ data, patch, context }: { data: Record<string, unknown>; patch: Patch; context: InspectorContext }) {
  const core = data as unknown as MihomoCoreData;
  const settings = core.settings ?? ({} as MihomoCoreData["settings"]);
  const setSettings = (next: Partial<MihomoCoreData["settings"]>) => patch({ settings: { ...settings, ...next } });
  const geox = settings.geoxUrl ?? {};
  return (
    <div className="space-y-5">
      <section className="space-y-3">
        <SectionTitle>运行模式</SectionTitle>
        <div className="grid grid-cols-3 gap-1.5">
          {MODE_OPTIONS.map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => setSettings({ mode: option.value })}
              className={cn("rounded-xl border px-2 py-2 text-left transition-colors", (settings.mode ?? "rule") === option.value ? "border-primary bg-primary/8" : "hover:bg-accent/40")}
            >
              <div className="text-xs font-medium">{option.label}</div>
              <div className="text-[11px] leading-4 text-muted-foreground">{option.hint}</div>
            </button>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="日志级别">
            <OptionSelect value={settings.logLevel ?? "info"} options={["silent", "error", "warning", "info", "debug"]} allowNone={false} onChange={(logLevel) => setSettings({ logLevel })} />
          </Field>
          <Field label="全局客户端指纹">
            <OptionSelect value={settings.globalClientFingerprint} options={["chrome", "firefox", "safari", "ios", "android", "edge", "random"]} onChange={(globalClientFingerprint) => setSettings({ globalClientFingerprint })} />
          </Field>
          <Field label="TCP 保活空闲">
            <OptionalNumber value={settings.keepAliveIdle} suffix="秒" onChange={(keepAliveIdle) => setSettings({ keepAliveIdle })} />
          </Field>
          <Field label="TCP 保活间隔">
            <OptionalNumber value={settings.keepAliveInterval} suffix="秒" onChange={(keepAliveInterval) => setSettings({ keepAliveInterval })} />
          </Field>
        </div>
        <Field label="订阅下载 User-Agent">
          <Input className="h-8 font-mono text-xs" placeholder="clash.meta" value={settings.globalUa ?? ""} onChange={(event) => setSettings({ globalUa: event.target.value })} />
        </Field>
        <div className="space-y-1.5 rounded-xl border p-2.5">
          <ToggleRow label="统一延迟" hint="延迟测试去掉握手开销，不同协议之间可比" checked={settings.unifiedDelay} onChange={(unifiedDelay) => setSettings({ unifiedDelay })} />
          <ToggleRow label="TCP 并发连接" hint="目标有多个 IP 时同时连接，取最快的" checked={settings.tcpConcurrent} onChange={(tcpConcurrent) => setSettings({ tcpConcurrent })} />
          <ToggleRow label="IPv6" checked={settings.ipv6} onChange={(ipv6) => setSettings({ ipv6 })} />
        </div>
      </section>

      <section className="space-y-3">
        <SectionTitle>Geo 数据库</SectionTitle>
        <div className="space-y-1.5 rounded-xl border p-2.5">
          <ToggleRow label="使用 dat 格式" hint="关闭时 GeoIP 用 mmdb" checked={settings.geodataMode} onChange={(geodataMode) => setSettings({ geodataMode })} />
          <ToggleRow label="自动更新" checked={settings.geoAutoUpdate} onChange={(geoAutoUpdate) => setSettings({ geoAutoUpdate })} />
        </div>
        {settings.geoAutoUpdate ? (
          <Field label="更新间隔">
            <OptionalNumber value={settings.geoUpdateInterval} suffix="小时" placeholder="24" onChange={(geoUpdateInterval) => setSettings({ geoUpdateInterval })} />
          </Field>
        ) : null}
        {(["geoip", "geosite", "mmdb", "asn"] as const).map((key) => (
          <Field key={key} label={`${key} 下载地址`}>
            <Input
              className="h-8 font-mono text-[11px]"
              placeholder="留空用核心默认（GitHub）"
              value={geox[key] ?? ""}
              onChange={(event) => setSettings({ geoxUrl: { ...geox, [key]: event.target.value } })}
            />
          </Field>
        ))}
      </section>

      <section className="space-y-3">
        <SectionTitle aside={<Switch checked={core.dnsEnabled} onCheckedChange={(dnsEnabled) => patch({ dnsEnabled, dns: core.dns ?? { enable: true } })} />}>核心 DNS</SectionTitle>
        {core.dnsEnabled ? (
          <FormYamlTabs
            form={<SpecForm fields={context.catalog.mihomo.dnsFields} values={core.dns ?? {}} onChange={(dns) => patch({ dns })} secretKept={context.catalog.secretKept} />}
            yaml={<YamlEditor value={core.dns} onCommit={(dns) => patch({ dns })} rows={14} hint="dns 段的全部字段（fake-ip、fallback-filter、hosts 解析等）。" />}
          />
        ) : (
          <p className="text-[11px] leading-4 text-muted-foreground">关闭时核心用系统 DNS 解析节点地址与直连目标；网关交给核心的域名由节点在远端解析。</p>
        )}
      </section>

      <section className="space-y-3">
        <SectionTitle>其它顶层键</SectionTitle>
        <YamlEditor
          value={core.extra}
          onCommit={(extra) => patch({ extra })}
          rows={8}
          hint={`hosts、sniffer、ntp、experimental 等原样写进配置。入站、控制器、TUN 由网关托管，写了会被忽略：${context.catalog.mihomo.reservedKeys.slice(0, 8).join("、")} 等。`}
        />
      </section>
      <ListHint />
    </div>
  );
}

function ListHint() {
  return (
    <p className="rounded-lg bg-muted/60 px-3 py-2 text-[11px] leading-5 text-muted-foreground">
      核心以旁车进程运行，只监听本机回环地址；网关经一条带鉴权的 SOCKS5 入站把流量交给它。安装、重启、日志与连接在出海代理页的「核心」页签里。
    </p>
  );
}
