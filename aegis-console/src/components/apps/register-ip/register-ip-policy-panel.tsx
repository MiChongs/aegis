"use client";

import { useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  Eye,
  FlaskConical,
  Flame,
  Globe2,
  ListChecks,
  LockKeyhole,
  Network,
  Plus,
  RotateCcw,
  Save,
  ServerCog,
  ShieldCheck,
  Smartphone,
  Sparkles,
  Trash2,
  TriangleAlert,
  X
} from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, ModeCard, SectionCard, SliderField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  ACTION_META,
  ActionToggle,
  DeviceActionToggle,
  deviceScope,
  distinctScope,
  durationLabel,
  LimitStepper,
  MethodChips,
  newRuleId,
  ruleSummary,
  SCOPE_META,
  SCOPES,
  ScopeGlyph,
  unitOf,
  WindowPicker,
  windowLabel
} from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RadioGroup } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import type { RegIPPolicy, RegIPRule, RegIPScope } from "@/lib/api/register-ip";
import { useSaveRegIPPolicyMutation } from "@/lib/register-ip-hooks";
import { cn } from "@/lib/utils";

const MAX_RULES = 20;

/** 规则模板：一键加一条常见的配额 */
const RULE_TEMPLATES: Array<Omit<RegIPRule, "id" | "enabled"> & { title: string }> = [
  { title: "同一地址只能注册 1 个", name: "同一地址累计 1 个", scope: "ip", windowSeconds: 0, limit: 1, action: "reject" },
  { title: "同一地址每小时 3 个，超出加验", name: "同一地址每小时", scope: "ip", windowSeconds: 3600, limit: 3, action: "challenge" },
  { title: "同一网段每天 50 个", name: "同一网段每天", scope: "subnet", windowSeconds: 86400, limit: 50, action: "reject" },
  { title: "同一设备只能注册 1 个", name: "同一设备累计 1 个", scope: "device", windowSeconds: 0, limit: 1, action: "reject" },
  { title: "同一设备每天 3 个", name: "同一设备每天", scope: "device", windowSeconds: 86400, limit: 3, action: "reject" },
  { title: "同一地址每小时最多 10 台设备，超出加验", name: "地址下的设备每小时", scope: "ip_devices", windowSeconds: 3600, limit: 10, action: "challenge" },
  { title: "同一设备每天最多换 5 个地址", name: "设备换地址每天", scope: "device_ips", windowSeconds: 86400, limit: 5, action: "reject" },
  { title: "同一城市每 10 分钟 100 个，观察", name: "同一城市每 10 分钟", scope: "city", windowSeconds: 600, limit: 100, action: "observe" },
  { title: "整个应用每分钟 200 个", name: "整个应用每分钟", scope: "app", windowSeconds: 60, limit: 200, action: "challenge" }
];

/**
 * 策略编辑。左边逐项配置，右边是保存栏与「读出来」的策略：把规则翻译成一句句人话，并列出会被后端拒绝的配置，
 * 保存前就知道哪里不对。校验口径与后端 regip.Policy.Normalize 一致。
 */
export function RegIPPolicyPanel({
  appKey,
  policy,
  defaults,
  configured,
  loading,
  dirty,
  onChange,
  onReset,
  onSaved,
  onSimulate
}: {
  appKey: string;
  policy: RegIPPolicy;
  defaults: RegIPPolicy;
  configured: boolean;
  loading: boolean;
  dirty: boolean;
  onChange: (policy: RegIPPolicy) => void;
  onReset: () => void;
  onSaved: () => void;
  onSimulate: () => void;
}) {
  const mutation = useSaveRegIPPolicyMutation(appKey);
  const patch = <K extends keyof RegIPPolicy>(key: K, value: RegIPPolicy[K]) => onChange({ ...policy, [key]: value });
  const patchRule = (index: number, next: RegIPRule) => patch("rules", policy.rules.map((rule, i) => (i === index ? next : rule)));
  const moveRule = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= policy.rules.length) return;
    const rules = [...policy.rules];
    [rules[index], rules[target]] = [rules[target], rules[index]];
    patch("rules", rules);
  };
  const addRule = (template?: (typeof RULE_TEMPLATES)[number]) => {
    const base = template ?? { name: "", scope: "ip" as RegIPScope, windowSeconds: 3600, limit: 5, action: "reject" as const };
    patch("rules", [
      ...policy.rules,
      { id: newRuleId(), enabled: true, name: base.name, scope: base.scope, windowSeconds: base.windowSeconds, limit: base.limit, action: base.action }
    ]);
  };
  const issues = validate(policy);

  async function handleSave() {
    if (issues.length) {
      toast.error(issues[0]);
      return;
    }
    try {
      await mutation.mutateAsync({
        ...policy,
        rules: policy.rules.map((rule) => ({ ...rule, name: rule.name.trim(), methods: rule.methods?.length ? rule.methods : null })),
        rejectMessage: policy.rejectMessage.trim()
      });
      onSaved();
      toast.success("已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  if (loading) {
    return (
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
        <div className="space-y-4">
          <Skeleton className="h-60 w-full rounded-2xl" />
          <Skeleton className="h-80 w-full rounded-2xl" />
        </div>
        <Skeleton className="h-96 w-full rounded-2xl" />
      </div>
    );
  }

  const off = !policy.enabled;

  return (
    <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <div className="space-y-4">
        <SectionCard
          icon={<ShieldCheck className="size-4" />}
          title="注册准入"
          description="四种注册方式（密码、短信、第三方、卡密）建号前统一过这里；地址与设备两条线互补"
          aside={
            <label className="flex cursor-pointer items-center gap-2 text-xs font-medium">
              {policy.enabled ? "已开启" : "未开启"}
              <Switch checked={policy.enabled} onCheckedChange={(value) => patch("enabled", value)} />
            </label>
          }
        >
          <div className={cn("space-y-4", off && "opacity-60")}>
            {!configured ? (
              <p className="rounded-lg bg-muted px-3 py-2 text-xs text-muted-foreground">
                当前沿用旧开关「注册 IP 唯一」推导的策略，保存后改用这里的配置
              </p>
            ) : null}
            <FieldGroup label="运行模式">
              <RadioGroup value={policy.mode} onValueChange={(value) => patch("mode", value as RegIPPolicy["mode"])} className="grid gap-2.5 sm:grid-cols-2">
                <ModeCard value="enforce" active={policy.mode === "enforce"} title="拦截" description="按规则拒绝或要求验证码" icon={<ShieldCheck className="size-4" />} />
                <ModeCard
                  value="observe"
                  active={policy.mode === "observe"}
                  title="观察"
                  description="只记录本该拦截的注册，一律放行；新规则先观察几天"
                  icon={<Eye className="size-4" />}
                />
              </RadioGroup>
            </FieldGroup>
          </div>
        </SectionCard>

        <SectionCard
          icon={<ListChecks className="size-4" />}
          title="配额规则"
          description="每条规则在自己的主体与窗口里独立计数；多条同时超限时 拒绝 > 加验 > 观察，同档按顺序取第一条。缺了地址或设备，只依赖那一边的规则不参与"
          aside={<Badge variant="outline" size="sm">{policy.rules.length} / {MAX_RULES}</Badge>}
        >
          <div className={cn("space-y-3", off && "opacity-60")}>
            {policy.rules.length === 0 ? (
              <p className="rounded-xl border border-dashed border-border py-8 text-center text-xs text-muted-foreground">
                还没有配额规则：注册只受名单、惩罚期与附加限制约束
              </p>
            ) : (
              policy.rules.map((rule, index) => (
                <RuleEditor
                  key={rule.id}
                  rule={rule}
                  index={index}
                  count={policy.rules.length}
                  onChange={(next) => patchRule(index, next)}
                  onMove={(delta) => moveRule(index, delta)}
                  onRemove={() => patch("rules", policy.rules.filter((_, i) => i !== index))}
                />
              ))
            )}
            <div className="flex flex-wrap items-center gap-2">
              <Button size="sm" variant="outline" disabled={policy.rules.length >= MAX_RULES} onClick={() => addRule()}>
                <Plus className="size-3.5" /> 添加规则
              </Button>
              <Select
                value=""
                onValueChange={(value) => {
                  const template = RULE_TEMPLATES[Number(value)];
                  if (template) addRule(template);
                }}
              >
                <SelectTrigger size="sm" className="w-44 text-xs" disabled={policy.rules.length >= MAX_RULES}>
                  <Sparkles className="size-3.5" />
                  <SelectValue placeholder="从模板添加" />
                </SelectTrigger>
                <SelectContent>
                  {RULE_TEMPLATES.map((template, index) => (
                    <SelectItem key={template.title} value={String(index)} className="text-xs">
                      {template.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <FieldGroup label="拒绝提示" hint="留空用默认文案；可用 {limit}、{window}">
              <Input
                value={policy.rejectMessage}
                maxLength={200}
                onChange={(event) => patch("rejectMessage", event.target.value)}
                placeholder="例如：当前网络{window}最多注册 {limit} 个账号"
                className="text-xs"
              />
            </FieldGroup>
          </div>
        </SectionCard>

        <SectionCard
          icon={<LockKeyhole className="size-4" />}
          title="惩罚期"
          description="屡次被配额拒绝的地址整体禁止注册，再犯时长翻倍"
          aside={<Switch checked={policy.penalty.enabled} onCheckedChange={(value) => patch("penalty", { ...policy.penalty, enabled: value })} />}
        >
          <div className={cn("grid gap-4 sm:grid-cols-2", (off || !policy.penalty.enabled) && "opacity-60")}>
            <FieldGroup label="触发条件">
              <div className="flex flex-wrap items-center gap-2 rounded-xl bg-muted px-3 py-2.5 text-xs">
                <WindowPicker
                  value={policy.penalty.strikeWindowSeconds}
                  allowLifetime={false}
                  onChange={(value) => patch("penalty", { ...policy.penalty, strikeWindowSeconds: value })}
                />
                <span>内被拒</span>
                <LimitStepper
                  value={policy.penalty.strikes}
                  min={1}
                  max={1000}
                  unit="次"
                  zeroLabel=""
                  onChange={(value) => patch("penalty", { ...policy.penalty, strikes: value })}
                />
              </div>
            </FieldGroup>
            <FieldGroup label="地址一侧的主体">
              <div className="flex gap-2">
                {(["ip", "subnet"] as const).map((scope) => (
                  <button
                    key={scope}
                    type="button"
                    onClick={() => patch("penalty", { ...policy.penalty, scope })}
                    className={cn(
                      "flex flex-1 items-center gap-2 rounded-xl px-3 py-2.5 text-xs transition-colors",
                      policy.penalty.scope === scope ? "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30" : "bg-muted hover:bg-muted-foreground/10"
                    )}
                  >
                    <ScopeGlyph scope={scope} className="size-6" />
                    {SCOPE_META[scope].label}
                  </button>
                ))}
              </div>
            </FieldGroup>
            <div className="sm:col-span-2">
              <SwitchRow
                icon={<Smartphone className="size-3.5" />}
                label="设备也进惩罚期"
                hint="同一设备、设备换地址的规则拒绝时，惩罚落在设备上：进了惩罚期的设备换任何网络都注册不了"
                checked={policy.penalty.device}
                onChange={(value) => patch("penalty", { ...policy.penalty, device: value })}
              />
            </div>
            <FieldGroup label="首次时长">
              <WindowPicker value={policy.penalty.baseSeconds} allowLifetime={false} onChange={(value) => patch("penalty", { ...policy.penalty, baseSeconds: value })} />
            </FieldGroup>
            <FieldGroup label="最长时长">
              <WindowPicker value={policy.penalty.maxSeconds} allowLifetime={false} onChange={(value) => patch("penalty", { ...policy.penalty, maxSeconds: value })} />
            </FieldGroup>
            <div className="sm:col-span-2">
              <SliderField
                label="每次递增倍数"
                value={policy.penalty.multiplier}
                min={1}
                max={10}
                step={0.5}
                valueLabel={`×${policy.penalty.multiplier}`}
                hint={penaltyLadder(policy.penalty)}
                onChange={(value) => patch("penalty", { ...policy.penalty, multiplier: value })}
              />
            </div>
          </div>
        </SectionCard>

        <SectionCard
          icon={<Flame className="size-4" />}
          title="注册突增检测"
          description="最近几分钟的注册数明显高于平常（EWMA 基线 + 标准差）时触发，并收紧全部配额"
          aside={<Switch checked={policy.surge.enabled} onCheckedChange={(value) => patch("surge", { ...policy.surge, enabled: value })} />}
        >
          <div className={cn("grid gap-3 sm:grid-cols-2", (off || !policy.surge.enabled) && "opacity-60")}>
            <SliderField
              label="观察窗口"
              value={policy.surge.windowMinutes}
              min={1}
              max={60}
              unit="分钟"
              onChange={(value) => patch("surge", { ...policy.surge, windowMinutes: value })}
            />
            <SliderField
              label="基线长度"
              value={policy.surge.baselineMinutes}
              min={15}
              max={360}
              step={15}
              unit="分钟"
              hint="至少是观察窗口的 3 倍"
              onChange={(value) => patch("surge", { ...policy.surge, baselineMinutes: value })}
            />
            <SliderField
              label="灵敏度"
              value={policy.surge.sensitivity}
              min={1}
              max={20}
              step={0.5}
              valueLabel={`${policy.surge.sensitivity}σ`}
              hint="越小越敏感"
              onChange={(value) => patch("surge", { ...policy.surge, sensitivity: value })}
            />
            <SliderField
              label="收紧系数"
              value={Math.round(policy.surge.tightenFactor * 100)}
              min={5}
              max={100}
              step={5}
              valueLabel={policy.surge.tightenFactor >= 1 ? "不收紧" : `配额 × ${Math.round(policy.surge.tightenFactor * 100)}%`}
              onChange={(value) => patch("surge", { ...policy.surge, tightenFactor: value / 100 })}
            />
            <FieldGroup label="最小注册数" hint="窗口内不到这个数永不触发">
              <LimitStepper
                value={policy.surge.minRegistrations}
                min={1}
                max={100000}
                unit="个"
                zeroLabel=""
                onChange={(value) => patch("surge", { ...policy.surge, minRegistrations: value })}
              />
            </FieldGroup>
            <FieldGroup label="触发后">
              <ActionToggle value={policy.surge.action} onChange={(value) => patch("surge", { ...policy.surge, action: value })} />
            </FieldGroup>
          </div>
        </SectionCard>

        <SectionCard
          icon={<Smartphone className="size-4" />}
          title="设备标识"
          description="客户端经 X-Device-Id（或请求体 deviceId）上报；占位值（unknown、全 0 等）在接入层就当成没上报"
        >
          <div className={cn("space-y-3", off && "opacity-60")}>
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <div className="min-w-0">
                <div className="text-xs font-medium">没上报设备标识时</div>
                <div className="text-[10px] text-muted-foreground">放行时只受地址一侧约束</div>
              </div>
              <DeviceActionToggle value={policy.device.missing} onChange={(value) => patch("device", { ...policy.device, missing: value })} />
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <div className="min-w-0">
                <div className="text-xs font-medium">上报了无效标识时</div>
                <div className="text-[10px] text-muted-foreground">过短、字符过于单一（aaaaaaaa）、含空白或非 ASCII；无效标识不参与任何设备规则</div>
              </div>
              <DeviceActionToggle value={policy.device.invalid} onChange={(value) => patch("device", { ...policy.device, invalid: value })} />
            </div>
            <SliderField
              label="有效标识最短长度"
              value={policy.device.minLength}
              min={4}
              max={64}
              unit="位"
              hint="UUID 36 位、Android ID 16 位、IDFV 36 位"
              onChange={(value) => patch("device", { ...policy.device, minLength: value })}
            />
          </div>
        </SectionCard>

        <SectionCard icon={<Globe2 className="size-4" />} title="运营商与属地" description="属地与运营商由 IP 定位得出；查询失败时不参与判定">
          <div className={cn("space-y-5", off && "opacity-60")}>
            <div className="space-y-2.5">
              <SwitchRow
                icon={<ServerCog className="size-3.5" />}
                label="限制机房与云厂商出口"
                hint="运营商名称包含任一关键词（不分大小写）"
                checked={policy.isp.enabled}
                onChange={(value) => patch("isp", { ...policy.isp, enabled: value })}
              />
              {policy.isp.enabled ? (
                <div className="space-y-2.5 rounded-xl bg-muted px-3 py-3">
                  <TagEditor
                    values={policy.isp.keywords}
                    onChange={(keywords) => patch("isp", { ...policy.isp, keywords })}
                    placeholder="输入关键词后回车"
                    onRestore={() => patch("isp", { ...policy.isp, keywords: defaults.isp.keywords })}
                  />
                  <div className="flex items-center justify-between gap-3 text-xs">
                    <span className="text-muted-foreground">命中后</span>
                    <ActionToggle value={policy.isp.action} onChange={(value) => patch("isp", { ...policy.isp, action: value })} />
                  </div>
                </div>
              ) : null}
            </div>
            <FieldGroup label="属地限制" hint="名单项写「国家」或「国家/省」，如 中国、中国/广东">
              <div className="space-y-2.5">
                <div className="inline-flex rounded-lg bg-muted p-0.5">
                  {(
                    [
                      ["off", "不限制"],
                      ["allow", "只允许名单内"],
                      ["deny", "拒绝名单内"]
                    ] as const
                  ).map(([mode, label]) => (
                    <button
                      key={mode}
                      type="button"
                      onClick={() => patch("geo", { ...policy.geo, mode })}
                      className={cn(
                        "rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors",
                        policy.geo.mode === mode ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
                      )}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                {policy.geo.mode !== "off" ? (
                  <div className="space-y-2.5 rounded-xl bg-muted px-3 py-3">
                    <TagEditor values={policy.geo.regions} onChange={(regions) => patch("geo", { ...policy.geo, regions })} placeholder="例如 中国 或 中国/广东，回车添加" />
                    <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
                      <span className="text-muted-foreground">不符合时</span>
                      <ActionToggle value={policy.geo.action} onChange={(value) => patch("geo", { ...policy.geo, action: value })} />
                    </div>
                    <SwitchRow
                      label="属地查不到时也按上面处置"
                      hint="IPv6、内网地址与定位失败都算查不到；关闭时放行"
                      checked={policy.geo.unknown === "reject"}
                      onChange={(value) => patch("geo", { ...policy.geo, unknown: value ? "reject" : "allow" })}
                    />
                  </div>
                ) : null}
              </div>
            </FieldGroup>
          </div>
        </SectionCard>

        <SectionCard icon={<Network className="size-4" />} title="网段口径与兜底">
          <div className={cn("space-y-4", off && "opacity-60")}>
            <div className="grid gap-3 sm:grid-cols-3">
              <SliderField
                label="IPv4 网段"
                value={policy.network.ipv4Subnet}
                min={8}
                max={32}
                valueLabel={`/${policy.network.ipv4Subnet}`}
                hint={`${(2 ** (32 - policy.network.ipv4Subnet)).toLocaleString("zh-CN")} 个地址`}
                onChange={(value) => patch("network", { ...policy.network, ipv4Subnet: value })}
              />
              <SliderField
                label="IPv6 单个地址"
                value={policy.network.ipv6Host}
                min={32}
                max={128}
                step={8}
                valueLabel={`/${policy.network.ipv6Host}`}
                hint="一户一段，推荐 /64"
                onChange={(value) =>
                  patch("network", { ...policy.network, ipv6Host: value, ipv6Subnet: Math.min(policy.network.ipv6Subnet, value) })
                }
              />
              <SliderField
                label="IPv6 网段"
                value={policy.network.ipv6Subnet}
                min={16}
                max={policy.network.ipv6Host}
                step={4}
                valueLabel={`/${policy.network.ipv6Subnet}`}
                hint="推荐 /48"
                onChange={(value) => patch("network", { ...policy.network, ipv6Subnet: value })}
              />
            </div>
            <div className="grid gap-2.5 sm:grid-cols-2">
              <SwitchRow
                label="缺少注册 IP 时拒绝"
                hint="接入层没透传客户端地址时；关闭则放行"
                checked={policy.missingIp === "reject"}
                onChange={(value) => patch("missingIp", value ? "reject" : "allow")}
              />
              <SwitchRow
                label="计数存储故障时拒绝"
                hint="Redis / 数据库不可用时；关闭则放行并记录"
                checked={policy.failMode === "closed"}
                onChange={(value) => patch("failMode", value ? "closed" : "open")}
              />
            </div>
          </div>
        </SectionCard>
      </div>

      <aside className="space-y-4 xl:sticky xl:top-4">
        <section className="rounded-2xl border border-border bg-card p-4" style={{ boxShadow: "var(--shadow-soft)" }}>
          <div className="flex items-center justify-between gap-2">
            <span className="text-sm font-semibold">{dirty ? "有未保存的改动" : "已是最新"}</span>
            {dirty ? <Badge variant="warning" size="sm">草稿</Badge> : null}
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" disabled={!dirty || mutation.isPending} onClick={handleSave}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中..." : "保存策略"}
            </Button>
            <Button size="sm" variant="outline" disabled={!dirty} onClick={onReset}>
              <X className="size-3.5" /> 放弃
            </Button>
            <Button size="sm" variant="outline" onClick={onSimulate}>
              <FlaskConical className="size-3.5" /> 用草稿模拟
            </Button>
          </div>
          <Button
            size="xs"
            variant="link"
            className="mt-2 h-auto px-0 text-[11px] text-muted-foreground"
            onClick={() => onChange({ ...defaults, enabled: policy.enabled })}
          >
            <RotateCcw className="size-3" /> 恢复推荐值
          </Button>
        </section>

        {issues.length ? (
          <section className="space-y-1.5 rounded-2xl border border-amber-500/30 bg-amber-500/[0.06] p-4">
            <div className="flex items-center gap-1.5 text-xs font-semibold text-amber-700 dark:text-amber-300">
              <TriangleAlert className="size-3.5" /> 保存前需要修正
            </div>
            <ul className="list-disc space-y-1 pl-4 text-[11px] text-amber-800 dark:text-amber-200">
              {issues.map((issue) => (
                <li key={issue}>{issue}</li>
              ))}
            </ul>
          </section>
        ) : null}

        <section className="rounded-2xl border border-border bg-card p-4">
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">读出来是这样</div>
          <ol className="space-y-2 text-xs leading-relaxed">
            {readable(policy).map((line, index) => (
              <li key={index} className="flex gap-2">
                <span className="mt-[3px] grid size-4 shrink-0 place-items-center rounded-full bg-muted text-[10px] tabular-nums text-muted-foreground">
                  {index + 1}
                </span>
                <span>{line}</span>
              </li>
            ))}
          </ol>
        </section>
      </aside>
    </div>
  );
}

function RuleEditor({
  rule,
  index,
  count,
  onChange,
  onMove,
  onRemove
}: {
  rule: RegIPRule;
  index: number;
  count: number;
  onChange: (rule: RegIPRule) => void;
  onMove: (delta: number) => void;
  onRemove: () => void;
}) {
  return (
    <div className={cn("rounded-xl bg-muted p-3 ring-1 ring-inset ring-transparent", rule.enabled ? "ring-border" : "opacity-60")}>
      <div className="flex flex-wrap items-center gap-2">
        <Switch checked={rule.enabled} onCheckedChange={(enabled) => onChange({ ...rule, enabled })} aria-label="启用" />
        <Input
          value={rule.name}
          maxLength={60}
          onChange={(event) => onChange({ ...rule, name: event.target.value })}
          placeholder={ruleSummary(rule)}
          className="h-8 min-w-40 flex-1 bg-background text-xs"
        />
        <div className="flex items-center gap-0.5">
          <Button size="icon-sm" variant="ghost" className="size-7" disabled={index === 0} onClick={() => onMove(-1)} aria-label="上移">
            <ArrowUp className="size-3.5" />
          </Button>
          <Button size="icon-sm" variant="ghost" className="size-7" disabled={index === count - 1} onClick={() => onMove(1)} aria-label="下移">
            <ArrowDown className="size-3.5" />
          </Button>
          <Button size="icon-sm" variant="ghost" className="size-7 text-rose-600 hover:text-rose-700" onClick={onRemove} aria-label="删除">
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 text-xs">
        <Select
          value={rule.scope}
          onValueChange={(value) => {
            const scope = value as RegIPScope;
            // 整个应用与去重型主体不能累计：切过去时给一个窗口
            const needsWindow = scope === "app" || distinctScope(scope);
            onChange({ ...rule, scope, windowSeconds: needsWindow && rule.windowSeconds === 0 ? 3600 : rule.windowSeconds });
          }}
        >
          <SelectTrigger size="sm" className="w-36 bg-background text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SCOPES.map((scope) => {
              const Icon = SCOPE_META[scope].icon;
              return (
                <SelectItem key={scope} value={scope} className="text-xs">
                  <Icon className="size-3.5" />
                  {SCOPE_META[scope].label}
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        <WindowPicker
          value={rule.windowSeconds}
          allowLifetime={rule.scope !== "app" && !distinctScope(rule.scope)}
          onChange={(windowSeconds) => onChange({ ...rule, windowSeconds })}
        />
        <span className="text-muted-foreground">最多</span>
        <LimitStepper
          value={rule.limit}
          min={1}
          max={1_000_000}
          unit={unitOf(rule.scope)}
          zeroLabel=""
          onChange={(limit) => onChange({ ...rule, limit })}
          className="bg-background"
        />
        <span className="text-muted-foreground">超出</span>
        <ActionToggle value={rule.action} onChange={(action) => onChange({ ...rule, action })} />
      </div>
      <div className="mt-2.5 flex flex-wrap items-center justify-between gap-2">
        <MethodChips value={rule.methods} onChange={(methods) => onChange({ ...rule, methods })} />
        <span className="text-[10px] text-muted-foreground">{SCOPE_META[rule.scope].hint}</span>
      </div>
    </div>
  );
}

/** 标签输入：回车或逗号添加，点叉删除 */
function TagEditor({
  values,
  onChange,
  placeholder,
  onRestore
}: {
  values: string[];
  onChange: (values: string[]) => void;
  placeholder: string;
  onRestore?: () => void;
}) {
  const [text, setText] = useState("");
  const commit = () => {
    const parts = text
      .split(/[,，\n]/)
      .map((part) => part.trim())
      .filter(Boolean)
      .filter((part) => !values.some((v) => v.toLowerCase() === part.toLowerCase()));
    if (parts.length) onChange([...values, ...parts]);
    setText("");
  };
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1.5">
        {values.map((value) => (
          <span key={value} className="inline-flex items-center gap-1 rounded-md bg-background px-1.5 py-0.5 text-[11px] ring-1 ring-inset ring-border">
            {value}
            <button type="button" aria-label={`删除 ${value}`} onClick={() => onChange(values.filter((v) => v !== value))}>
              <X className="size-3 text-muted-foreground hover:text-foreground" />
            </button>
          </span>
        ))}
        {!values.length ? <span className="text-[11px] text-muted-foreground">还没有</span> : null}
      </div>
      <div className="flex gap-2">
        <Textarea
          rows={1}
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              commit();
            }
          }}
          onBlur={commit}
          placeholder={placeholder}
          className="min-h-8 resize-none bg-background py-1.5 text-xs"
        />
        {onRestore ? (
          <Button size="sm" variant="outline" className="shrink-0" onClick={onRestore}>
            推荐
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function penaltyLadder(penalty: RegIPPolicy["penalty"]) {
  const steps: string[] = [];
  for (let level = 1; level <= 4; level++) {
    const seconds = Math.min(penalty.maxSeconds, penalty.baseSeconds * penalty.multiplier ** (level - 1));
    steps.push(durationLabel(Math.round(seconds)));
    if (seconds >= penalty.maxSeconds) break;
  }
  return steps.join(" → ");
}

/** 与后端 Normalize 同一套校验，文案也对齐 */
function validate(policy: RegIPPolicy): string[] {
  const issues: string[] = [];
  policy.rules.forEach((rule, index) => {
    const label = rule.name.trim() || `第 ${index + 1} 条规则`;
    if (rule.scope === "app" && rule.windowSeconds === 0) issues.push(`${label}：整个应用的规则需要设统计窗口`);
    if (distinctScope(rule.scope) && rule.windowSeconds === 0) issues.push(`${label}：不同设备数 / 不同地址数需要设统计窗口`);
    if (rule.windowSeconds > 0 && rule.windowSeconds < 60) issues.push(`${label}：统计窗口至少 1 分钟`);
    if (rule.windowSeconds > 365 * 86400) issues.push(`${label}：统计窗口最长 365 天`);
    if (rule.limit < 1) issues.push(`${label}：上限至少为 1`);
  });
  if (policy.penalty.enabled && policy.penalty.maxSeconds < policy.penalty.baseSeconds) issues.push("惩罚期：最长时长不能短于首次时长");
  if (policy.penalty.maxSeconds > 30 * 86400 || policy.penalty.baseSeconds > 30 * 86400) issues.push("惩罚期：时长不超过 30 天");
  if (policy.surge.baselineMinutes < policy.surge.windowMinutes * 3) issues.push("突增检测：基线至少是观察窗口的 3 倍");
  if (policy.isp.enabled && !policy.isp.keywords.length) issues.push("运营商限制：至少填一个关键词");
  if (policy.geo.mode !== "off" && !policy.geo.regions.length) issues.push("属地限制：至少填一个属地");
  return issues;
}

/** 把策略翻译成一句句话 */
function readable(policy: RegIPPolicy): string[] {
  if (!policy.enabled) return ["没开启：注册不受地址限制，只记注册台账"];
  const lines: string[] = [];
  if (policy.mode === "observe") lines.push("观察模式：下面每一条都只记录，不拦截");
  const rules = policy.rules.filter((rule) => rule.enabled);
  for (const rule of rules) {
    const methods = rule.methods?.length ? `（仅${rule.methods.map((m) => ({ password: "密码", sms: "短信", oauth: "第三方", cardkey: "卡密" })[m]).join("、")}注册）` : "";
    const verb = rule.scope === "ip_devices" ? "最多出现" : rule.scope === "device_ips" ? "最多使用" : "最多注册";
    lines.push(
      `${SCOPE_META[rule.scope].label}${windowLabel(rule.windowSeconds)}${verb} ${rule.limit} ${unitOf(rule.scope)}，超出${ACTION_META[rule.action].label}${methods}`
    );
  }
  if (!rules.length) lines.push("没有配额规则");
  const deviceRules = rules.filter((rule) => deviceScope(rule.scope) || rule.scope === "ip_devices").length;
  if (deviceRules && policy.device.missing === "allow") lines.push(`没上报设备标识的注册跳过 ${deviceRules} 条设备规则，只受地址一侧约束`);
  if (policy.device.missing !== "allow") lines.push(`没上报设备标识时${policy.device.missing === "reject" ? "拒绝" : "要求加验"}`);
  if (policy.device.invalid !== "allow") lines.push(`设备标识无效（短于 ${policy.device.minLength} 位或明显伪造）时${policy.device.invalid === "reject" ? "拒绝" : "要求加验"}`);
  if (policy.penalty.enabled) {
    lines.push(
      `${durationLabel(policy.penalty.strikeWindowSeconds)}内被拒 ${policy.penalty.strikes} 次，该${policy.penalty.scope === "ip" ? "地址" : "网段"}${policy.penalty.device ? "（设备规则则是该设备）" : ""}禁止注册 ${penaltyLadder(policy.penalty)}`
    );
  }
  if (policy.surge.enabled) {
    lines.push(
      `最近 ${policy.surge.windowMinutes} 分钟注册数超过平常 ${policy.surge.sensitivity}σ（且至少 ${policy.surge.minRegistrations} 个）时${ACTION_META[policy.surge.action].label}${
        policy.surge.tightenFactor < 1 ? `，全部配额收紧到 ${Math.round(policy.surge.tightenFactor * 100)}%` : ""
      }`
    );
  }
  if (policy.isp.enabled) lines.push(`运营商含「${policy.isp.keywords.slice(0, 3).join("、")}」等 ${policy.isp.keywords.length} 个关键词时${ACTION_META[policy.isp.action].label}`);
  if (policy.geo.mode !== "off") {
    lines.push(`${policy.geo.mode === "allow" ? "不在" : "在"}「${policy.geo.regions.slice(0, 3).join("、")}」的地址${ACTION_META[policy.geo.action].label}`);
  }
  lines.push(`IPv4 按 /${policy.network.ipv4Subnet} 算网段；IPv6 按 /${policy.network.ipv6Host} 算一个地址、/${policy.network.ipv6Subnet} 算网段`);
  if (policy.missingIp === "reject") lines.push("拿不到注册 IP 时拒绝");
  if (policy.failMode === "closed") lines.push("计数存储故障时拒绝注册");
  return lines;
}
