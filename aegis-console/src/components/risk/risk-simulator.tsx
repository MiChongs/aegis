"use client";

import { useMemo, useState } from "react";
import { FlaskConical, Loader2, RotateCcw, Wand2 } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useSimulateRiskMutation } from "@/lib/risk-hooks";
import type { RiskEvalResult, RiskSimulatePayload, RiskVariableCatalog } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  ConditionFields, ConditionHint, ConditionTypeSelect,
  defaultConditionData, normalizeConditionData, type ConditionData,
} from "./risk-condition-form";
import { ActionBadge, LevelBadge, useRiskCatalog } from "./risk-shared";

/**
 * 规则模拟器。
 *
 * 重构前它只能「选一条已存在的规则 + 填 IP 和 UA」，而且结果里只列命中项。
 * 这有两个致命短板：
 *   1. 想试的规则必须先保存 —— 于是"先存一条错的再改"成了常规操作。
 *   2. 环境变量完全依赖真实情报，`ip_is_proxy` 之类的组合在控制台上构造不出来，
 *      写好的表达式只能上线之后靠真实攻击去验证。
 *
 * 现在两件事都能做：草稿规则直接试跑，环境变量可逐项覆写。
 * 判定走的是与线上完全相同的一段代码 —— 模拟器与线上各写一套判定，
 * 是「模拟通过、上线不中」这类问题的根源。
 */

const UA_PRESETS = [
  { label: "桌面 Chrome", value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36" },
  { label: "iPhone Safari", value: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1" },
  { label: "Android Chrome", value: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36" },
  { label: "Googlebot", value: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" },
  { label: "curl", value: "curl/8.4.0" },
];

/** 最常被用来构造场景的几个覆写项，其余走「更多变量」。 */
const QUICK_OVERRIDES: Array<{ key: string; label: string; type: "bool" | "number" | "text" }> = [
  { key: "ip_is_proxy", label: "IP 是代理", type: "bool" },
  { key: "ip_is_vpn", label: "IP 是 VPN", type: "bool" },
  { key: "ip_is_tor", label: "IP 是 Tor 出口", type: "bool" },
  { key: "ip_is_datacenter", label: "IP 属机房", type: "bool" },
  { key: "ip_trusted", label: "IP 已加白", type: "bool" },
  { key: "ip_risk_score", label: "IP 信誉分", type: "number" },
  { key: "geo_country", label: "归属国家代码", type: "text" },
  { key: "ip_request_count", label: "窗口内请求数", type: "number" },
  { key: "device_age_hours", label: "设备存续小时", type: "number" },
  { key: "device_accounts_seen", label: "设备关联账号数", type: "number" },
  { key: "ip_accounts_seen", label: "IP 关联账号数", type: "number" },
];

export function RiskSimulatorPanel() {
  const { scenes, condition, metadata } = useRiskCatalog();
  const simulateMut = useSimulateRiskMutation();

  const [scene, setScene] = useState("login");
  const [ip, setIP] = useState("203.0.113.7");
  const [account, setAccount] = useState("");
  const [deviceId, setDeviceId] = useState("");
  const [userAgent, setUserAgent] = useState(UA_PRESETS[0].value);
  const [overrides, setOverrides] = useState<Record<string, unknown>>({});
  const [showAllVariables, setShowAllVariables] = useState(false);

  const [useDraft, setUseDraft] = useState(false);
  const [draftType, setDraftType] = useState("custom_expr");
  const [draftScore, setDraftScore] = useState("30");
  const [draftData, setDraftData] = useState<ConditionData>({ expression: "" });

  const [result, setResult] = useState<RiskEvalResult | null>(null);

  const draftCatalog = condition(draftType);

  const run = async () => {
    const payload: RiskSimulatePayload = {
      scene, ip: ip.trim(), account: account.trim(), deviceId: deviceId.trim(), userAgent,
      overrides: Object.keys(overrides).length > 0 ? overrides : undefined,
    };
    if (useDraft) {
      payload.draft = {
        name: "草稿规则",
        scene,
        conditionType: draftType,
        conditionData: normalizeConditionData(draftCatalog, draftData),
        score: Number(draftScore) || 20,
      };
    }
    try {
      setResult(await simulateMut.mutateAsync(payload));
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  };

  const reset = () => {
    setOverrides({});
    setResult(null);
  };

  const changeDraftType = (next: string) => {
    setDraftType(next);
    setDraftData(next === "custom_expr" ? { expression: "" } : defaultConditionData(condition(next)));
  };

  return (
    // 两栏各自滚动：输入项很多，让整页滚动会把"执行模拟"按钮推到屏幕外，
    // 而调参数时最需要的就是它一直在手边。
    <div className="grid gap-4 lg:grid-cols-2 lg:items-start">
      <Card className="flex flex-col lg:max-h-[calc(100vh-12rem)]">
        <CardContent className="flex min-h-0 flex-1 flex-col gap-4 p-4">
          <div className="flex items-center gap-2">
            <FlaskConical className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">模拟输入</h3>
            <Button variant="ghost" size="sm" className="ml-auto h-7 text-xs" onClick={reset}>
              <RotateCcw className="size-3.5" />重置
            </Button>
          </div>

          <ScrollArea className="min-h-0 flex-1 pr-3">
            <div className="space-y-4">
              <section className="space-y-3">
                <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">请求维度</h4>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1">
                    <Label className="text-xs">场景</Label>
                    <Select value={scene} onValueChange={setScene}>
                      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {scenes.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">IP 地址</Label>
                    <Input className="h-8 font-mono text-xs" value={ip} onChange={(e) => setIP(e.target.value)}
                      placeholder="203.0.113.7" />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">账号</Label>
                    <Input className="h-8 text-xs" value={account} onChange={(e) => setAccount(e.target.value)}
                      placeholder="zhangsan" />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">设备标识</Label>
                    <Input className="h-8 font-mono text-xs" value={deviceId} onChange={(e) => setDeviceId(e.target.value)}
                      placeholder="留空表示未上报" />
                  </div>
                  <div className="space-y-1 sm:col-span-2">
                    <Label className="text-xs">User-Agent</Label>
                    <Textarea rows={2} className="font-mono text-xs" value={userAgent}
                      onChange={(e) => setUserAgent(e.target.value)} />
                    <div className="flex flex-wrap gap-1">
                      {UA_PRESETS.map((preset) => (
                        <button key={preset.label} type="button" onClick={() => setUserAgent(preset.value)}
                          className={cn("rounded border px-1.5 py-0.5 text-[10px] transition-colors hover:bg-accent",
                            userAgent === preset.value && "border-primary/50 bg-accent")}>
                          {preset.label}
                        </button>
                      ))}
                    </div>
                  </div>
                </div>
              </section>

              <Separator />

              <section className="space-y-2">
                <div className="flex items-center justify-between">
                  <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">环境覆写</h4>
                  <button type="button" onClick={() => setShowAllVariables((v) => !v)}
                    className="text-[10px] text-muted-foreground underline-offset-2 hover:underline">
                    {showAllVariables ? "只看常用" : "更多变量"}
                  </button>
                </div>
                <p className="text-[10px] text-muted-foreground">
                  留空使用真实解析结果，填值则直接覆盖。
                </p>
                <OverrideEditor
                  variables={metadata?.variables ?? []}
                  showAll={showAllVariables}
                  values={overrides}
                  onChange={setOverrides} />
              </section>

              <Separator />

              <section className="space-y-3">
                <div className="flex items-center gap-2">
                  <Switch checked={useDraft} onCheckedChange={setUseDraft} />
                  <Label className="text-xs">试跑未保存的草稿规则</Label>
                </div>
                {useDraft && (
                  <div className="space-y-3 rounded-lg border p-3">
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="space-y-1">
                        <Label className="text-xs">条件类型</Label>
                        <ConditionTypeSelect value={draftType} onChange={changeDraftType} />
                      </div>
                      <div className="space-y-1">
                        <Label className="text-xs">命中得分</Label>
                        <Input type="number" className="h-8 font-mono text-xs" value={draftScore}
                          onChange={(e) => setDraftScore(e.target.value)} />
                      </div>
                    </div>
                    <ConditionHint catalog={draftCatalog} />
                    {draftType === "custom_expr" ? (
                      <div className="space-y-1">
                        <Label className="text-xs">表达式</Label>
                        <Textarea rows={3} className="font-mono text-xs"
                          value={String(draftData.expression ?? "")}
                          placeholder="ip_is_datacenter and ip_accounts_seen >= 3"
                          onChange={(e) => setDraftData({ ...draftData, expression: e.target.value })} />
                      </div>
                    ) : (
                      <ConditionFields catalog={draftCatalog} value={draftData} onChange={setDraftData} />
                    )}
                  </div>
                )}
              </section>
            </div>
          </ScrollArea>

          <Button className="w-full" disabled={simulateMut.isPending} onClick={run}>
            {simulateMut.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Wand2 className="size-3.5" />}
            执行模拟
          </Button>
        </CardContent>
      </Card>

      <SimulationResult result={result} pending={simulateMut.isPending} />
    </div>
  );
}

function OverrideEditor({ variables, showAll, values, onChange }: {
  variables: RiskVariableCatalog[];
  showAll: boolean;
  values: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
}) {
  const byName = useMemo(() => new Map(variables.map((v) => [v.name, v])), [variables]);

  const rows = useMemo(() => {
    if (!showAll) return QUICK_OVERRIDES;
    return variables
      // 这些由请求维度直接给出，重复覆写只会造成混淆
      .filter((v) => !["scene", "ip", "device_id", "account", "user_agent", "extra"].includes(v.name))
      .map((v) => ({
        key: v.name,
        label: v.description,
        type: v.type === "bool" ? "bool" as const : (v.type === "int" || v.type === "float") ? "number" as const : "text" as const,
      }));
  }, [showAll, variables]);

  const patch = (key: string, value: unknown) => {
    const next = { ...values };
    if (value === "" || value === undefined) delete next[key];
    else next[key] = value;
    onChange(next);
  };

  return (
    <div className="grid gap-x-3 gap-y-1.5 sm:grid-cols-2">
      {rows.map((row) => {
        const meta = byName.get(row.key);
        const active = row.key in values;
        return (
          <div key={row.key} className={cn("flex items-center gap-2 rounded-md border px-2 py-1",
            active && "border-primary/40 bg-primary/5")}>
            <span className="min-w-0 flex-1 truncate text-[11px]" title={meta?.description}>
              {row.label}
            </span>
            {row.type === "bool" ? (
              <Switch checked={Boolean(values[row.key])}
                onCheckedChange={(checked) => patch(row.key, checked ? true : "")} />
            ) : (
              <Input
                type={row.type === "number" ? "number" : "text"}
                className="h-6 w-24 font-mono text-[10px]"
                value={values[row.key] === undefined ? "" : String(values[row.key])}
                placeholder="真实值"
                onChange={(e) => patch(row.key, row.type === "number"
                  ? (e.target.value === "" ? "" : Number(e.target.value))
                  : e.target.value)} />
            )}
          </div>
        );
      })}
    </div>
  );
}

function SimulationResult({ result, pending }: { result: RiskEvalResult | null; pending: boolean }) {
  const { conditionLabel } = useRiskCatalog();

  if (!result) {
    return (
      <Card>
        <CardContent className="flex h-full min-h-[320px] flex-col items-center justify-center gap-2 p-4 text-center">
          <FlaskConical className={cn("size-8 text-muted-foreground/40", pending && "animate-pulse")} />
          <p className="text-sm text-muted-foreground">执行模拟后在此显示逐条判据</p>
          <p className="max-w-xs text-xs text-muted-foreground/80">
            结果包含全部参评规则与判据，未命中的也会列出
          </p>
        </CardContent>
      </Card>
    );
  }

  const evaluated = result.evaluatedRules ?? [];
  const hits = evaluated.filter((rule) => rule.hit);
  const misses = evaluated.filter((rule) => !rule.hit);
  const errored = evaluated.filter((rule) => rule.error);

  return (
    <Card className="flex flex-col lg:max-h-[calc(100vh-12rem)]">
      <CardContent className="flex min-h-0 flex-1 flex-col gap-3 p-4">
        <h3 className="text-sm font-semibold">模拟结果</h3>

        <div className="grid grid-cols-3 gap-2">
          <div className="rounded-lg border p-2.5 text-center">
            <div className="text-[10px] text-muted-foreground">总分</div>
            <div className="font-mono text-2xl font-bold tabular-nums">{result.totalScore}</div>
          </div>
          <div className="flex flex-col items-center justify-center gap-1 rounded-lg border p-2.5">
            <div className="text-[10px] text-muted-foreground">等级</div>
            <LevelBadge level={result.riskLevel} />
          </div>
          <div className="flex flex-col items-center justify-center gap-1 rounded-lg border p-2.5">
            <div className="text-[10px] text-muted-foreground">处置</div>
            <ActionBadge action={result.action} />
          </div>
        </div>

        {result.actionDetail && (
          <p className="rounded-lg border bg-muted/30 px-3 py-1.5 text-xs text-muted-foreground">
            {result.actionDetail}
          </p>
        )}

        {errored.length > 0 && (
          <p className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300">
            {errored.length} 条规则评估出错，按未命中处理
          </p>
        )}

        <ScrollArea className="min-h-0 flex-1 pr-3">
          <div className="space-y-3">
            <RuleGroup title={`命中 ${hits.length} 条`} rules={hits} hit conditionLabel={conditionLabel} />
            <RuleGroup title={`未命中 ${misses.length} 条`} rules={misses} conditionLabel={conditionLabel} />
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  );
}

function RuleGroup({ title, rules, hit, conditionLabel }: {
  title: string;
  rules: NonNullable<RiskEvalResult["evaluatedRules"]>;
  hit?: boolean;
  conditionLabel: (v: string) => string;
}) {
  if (rules.length === 0) return null;
  return (
    <section className="space-y-1">
      <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">{title}</h4>
      {rules.map((rule, index) => (
        <div key={`${rule.ruleId}-${index}`}
          className={cn("rounded-lg border px-2.5 py-1.5 text-xs",
            hit ? "border-amber-300/70 bg-amber-50/50 dark:border-amber-900/50 dark:bg-amber-950/20" : "opacity-70")}>
          <div className="flex items-center gap-1.5">
            <span className="min-w-0 flex-1 truncate font-medium">{rule.ruleName}</span>
            <Badge variant="outline" size="sm">{conditionLabel(rule.conditionType)}</Badge>
            <span className={cn("shrink-0 font-mono font-semibold tabular-nums",
              hit ? "text-amber-700 dark:text-amber-400" : "text-muted-foreground")}>
              {hit ? `+${rule.score}` : `(${rule.score})`}
            </span>
          </div>
          {rule.reason && <p className="mt-0.5 text-[11px] text-muted-foreground">{rule.reason}</p>}
          {rule.error && (
            <p className="mt-0.5 font-mono text-[10px] text-rose-600 dark:text-rose-400">评估出错：{rule.error}</p>
          )}
        </div>
      ))}
    </section>
  );
}
