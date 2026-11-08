"use client";

import { useState, type ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import {
  AlertTriangle, Ban, CheckCircle2, ChevronDown, Eye, FlaskConical, Gavel, Globe, LoaderCircle, Play, Plus, RotateCcw, ShieldCheck, Smartphone, Sparkles, Trash2, User,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { JsonEditor } from "@/components/functions/json-editor";
import { Field, Segmented } from "@/components/traffic/traffic-kit";
import { AppScopeSelect } from "@/components/risk/kit/app-scope";
import { LEVEL_WORD, latencyText } from "@/components/risk/kit/humanize";
import { DIM_LABEL, FEATURE_KIND_LABEL, formatWindow, KIND_LABEL, LIST_TYPE_LABEL, SOURCE_LABEL, STAGE_LABEL } from "@/components/risk/kit/labels";
import { TraceView } from "@/components/risk/kit/trace-view";
import type { EvalResponse, Feature, ListHit, ListKind, ListType, RiskAction, RiskMeta, Scene, SceneField, SimRequest, SimulateResult } from "@/lib/api/risk";
import { useSimulateScene } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import type { VarOption } from "./condition-builder";
import {
  ACTION_SWATCH, ACTION_VERDICT, bandOf, bandRange, Callout, EmptyBlock, errorText, ScoreBandBar, StudioSection,
} from "./kit";

/**
 * 模拟评估：填一次假想请求，对草稿（或线上版本）跑完整评估。
 * 结论读成一句话（「这次请求会被拦截，因为……」），分数构成画成瀑布：每条生效规则从上一条的终点接着加。
 */

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
}

const EMPTY: Form = {
  userId: "", account: "", ip: "203.0.113.7", userAgent: "Mozilla/5.0 (Linux; Android 14) Chrome/130.0 Mobile", phone: "", email: "", amount: "",
  deviceId: "", platform: "", brand: "", model: "", osVersion: "", emulator: false, rooted: false,
  country: "", ipScore: "", proxy: false, vpn: false, tor: false, datacenter: false,
  captchaPassed: false, captchaUnavailable: false,
};

const PRESETS: Array<{ key: string; title: string; hint: string; form: Partial<Form>; lists?: ListHit[] }> = [
  { key: "normal", title: "普通用户", hint: "家庭宽带、真机、正常浏览器", form: { ip: "203.0.113.7", platform: "android", brand: "Xiaomi", model: "23127PN0CC", osVersion: "14", country: "CN" } },
  { key: "emulator", title: "模拟器设备", hint: "模拟器且已 Root", form: { platform: "android", brand: "Google", model: "sdk_gphone64_x86_64", emulator: true, rooted: true } },
  { key: "proxy", title: "代理与机房网络", hint: "代理、VPN、机房 IP，情报分高", form: { ip: "198.51.100.23", proxy: true, vpn: true, datacenter: true, ipScore: "85", country: "US" } },
  { key: "bot", title: "脚本客户端", hint: "爬虫类 User-Agent", form: { userAgent: "python-requests/2.32.3", platform: "", brand: "", model: "" } },
  { key: "blocked", title: "黑名单 IP", hint: "IP 在黑名单中", form: { ip: "192.0.2.66" }, lists: [{ listType: "ip", kind: "block", value: "192.0.2.66" }] },
];

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

/** 业务入参按字段类型转成值 */
function extraValue(field: SceneField, raw: string | boolean | undefined): unknown {
  if (field.type === "bool") return raw === true ? true : raw === false ? false : undefined;
  const s = typeof raw === "string" ? raw.trim() : "";
  if (!s) return undefined;
  if (field.type === "number") return Number.isFinite(Number(s)) ? Number(s) : undefined;
  if (field.type === "list") return s.split(/[,，、]/).map((x) => x.trim()).filter(Boolean);
  if (field.type === "object") {
    try {
      return JSON.parse(s) as unknown;
    } catch {
      return undefined;
    }
  }
  return s;
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className={cn("flex cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2 text-sm transition-colors hover:bg-accent/40", checked && "border-foreground/30 bg-accent/50")}>
      <span className="min-w-0 truncate">{label}</span>
      <Switch checked={checked} onCheckedChange={onChange} aria-label={label} />
    </label>
  );
}

function FormGroup({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground"><Icon className="size-3.5" />{title}</div>
      {children}
    </div>
  );
}

/* ════════════════════════ 结果 ════════════════════════ */

const VERDICT_ICON: Record<RiskAction, LucideIcon> = { pass: CheckCircle2, review: Eye, captcha: ShieldCheck, block: Ban, ban: Gavel };

const TARGET_WORD: Record<string, string> = { device: "设备", ip: "IP", account: "账号", user: "用户" };
type Cmd = { id?: string; kind?: string; target?: string; value?: string; duration_seconds?: number; durationSeconds?: number; reason?: string };

/** 处置命令的说法：「设备 abc 加入黑名单 1 天」 */
function commandText(c: Cmd): string {
  const who = `${TARGET_WORD[c.target ?? ""] ?? "对象"} ${c.value ?? ""}`.trim();
  const sec = c.duration_seconds ?? c.durationSeconds ?? 0;
  const span = sec ? `，期限 ${formatWindow(sec)}` : "，长期有效";
  switch (c.kind) {
    case "list_add": return `${who} 加入黑名单${span}`;
    case "account_ban": return `封禁账号 ${c.value ?? ""}${span}`;
    case "firewall_ban": return `在网关防火墙屏蔽 ${c.value ?? ""}${span}`;
    case "revoke_sessions": return `账号 ${c.value ?? ""} 强制下线`;
    case "list_remove": return `${who} 移出名单`;
    default: return `对${who}执行处置`;
  }
}

const FORCE_WORD: Record<RiskAction, string> = { pass: "放行", review: "转人工复核", captcha: "先完成图形验证码", block: "拦截", ban: "拦截并封禁" };

/** 「因为……」：按决定方给出原因 */
function because(r: EvalResponse, scene: Scene): string {
  const reasons = (r.reasons ?? []).filter(Boolean);
  const listHit = (r.list_hits ?? [])[0];
  const enforced = (r.hits ?? []).filter((h) => h.mode !== "observe");
  const forcing = enforced.find((h) => h.decision === r.action);
  const band = bandOf(scene.bands ?? [], r.score);
  switch (r.decided_by) {
    case "list":
      return listHit ? `${LIST_TYPE_LABEL[listHit.list_type as ListType] ?? "请求信息"} ${listHit.value} 在${KIND_LABEL[listHit.kind as ListKind] ?? "名单"}中` : reasons[0] ?? "命中了名单";
    case "no_release":
      return "这个场景还没有发布，按放行处理";
    case "scope":
      return "请求所属的应用不在场景的作用范围内";
    case "policy":
      return reasons.length ? `场景策略做了修正：${reasons[reasons.length - 1]}` : "场景策略修正了结论";
    case "flow":
      return `总分 ${r.score} 分，按决策流判定${reasons.length ? `：${reasons[reasons.length - 1]}` : ""}`;
    case "rule":
      if (forcing) return `规则「${forcing.name}」命中后要求${FORCE_WORD[r.action] ?? "处置"}`;
      return `命中 ${enforced.length} 条规则，总分 ${r.score} 分${band ? `，落在${bandRange(band)}` : ""}`;
    default:
      if (!(r.hits ?? []).length) return "没有命中任何规则";
      if (!enforced.length) return "命中的规则都在观察中，不影响结果";
      return reasons[0] ?? `总分 ${r.score} 分`;
  }
}

/** 分数构成瀑布：生效规则依次累加，观察规则画成虚框不计入 */
function ScoreWaterfall({ r }: { r: EvalResponse }) {
  const hits = r.hits ?? [];
  const enforced = hits.filter((h) => h.mode !== "observe").sort((a, b) => b.score - a.score);
  const observed = hits.filter((h) => h.mode === "observe");
  const sum = enforced.reduce((s, h) => s + h.score, 0);
  const adjust = r.score - sum;
  const domain = Math.max(100, r.score, ...enforced.reduce<number[]>((acc, h) => [...acc, (acc[acc.length - 1] ?? 0) + h.score], []), ...observed.map((h) => Math.max(0, h.score)));
  const pct = (v: number) => `${(Math.max(0, v) / domain) * 100}%`;
  const sw = ACTION_SWATCH[r.action] ?? ACTION_SWATCH.pass;

  if (!hits.length) return <p className="text-sm text-muted-foreground">没有命中任何规则，总分为 0。</p>;

  const rows = enforced.reduce<Array<{ h: (typeof enforced)[number]; start: number; width: number; end: number }>>((out, h) => {
    const prev = out.length ? out[out.length - 1].end : 0;
    out.push({ h, start: Math.min(prev, prev + h.score), width: Math.abs(h.score), end: prev + h.score });
    return out;
  }, []);

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      {rows.map(({ h, start, width }) => (
        <div key={h.key} className="grid min-w-0 grid-cols-[minmax(0,9rem)_minmax(0,1fr)_3.5rem] items-center gap-3 text-xs sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)_3.5rem]">
          <span className="min-w-0 truncate" title={h.name}>{h.name}</span>
          <span className="relative h-5 rounded bg-muted/50">
            <span className={cn("absolute inset-y-0 rounded", h.score < 0 ? "bg-emerald-500/70" : "bg-foreground/70")} style={{ left: pct(start), width: pct(width) }} />
          </span>
          <span className="text-right font-medium tabular-nums">{h.score > 0 ? "+" : ""}{h.score}</span>
        </div>
      ))}
      {adjust !== 0 && enforced.length ? (
        <div className="grid min-w-0 grid-cols-[minmax(0,9rem)_minmax(0,1fr)_3.5rem] items-center gap-3 text-xs text-muted-foreground sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)_3.5rem]">
          <span>{adjust < 0 && r.score === 0 ? "不低于 0 分" : "其他调整"}</span>
          <span className="relative h-5 rounded bg-muted/50">
            <span className="absolute inset-y-0 rounded border border-dashed border-muted-foreground/60" style={{ left: pct(Math.min(sum, r.score)), width: pct(Math.abs(adjust)) }} />
          </span>
          <span className="text-right tabular-nums">{adjust > 0 ? "+" : ""}{adjust}</span>
        </div>
      ) : null}
      {observed.map((h) => (
        <div key={h.key} className="grid min-w-0 grid-cols-[minmax(0,9rem)_minmax(0,1fr)_3.5rem] items-center gap-3 text-xs text-muted-foreground sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)_3.5rem]">
          <span className="flex min-w-0 items-center gap-1.5"><span className="truncate" title={h.name}>{h.name}</span><Badge variant="info" size="sm">观察</Badge></span>
          <span className="relative h-5 rounded bg-muted/30">
            <span className="absolute inset-y-0 rounded border border-dashed border-sky-500/60" style={{ left: 0, width: pct(Math.abs(h.score)) }} />
          </span>
          <span className="text-right tabular-nums line-through">{h.score > 0 ? "+" : ""}{h.score}</span>
        </div>
      ))}
      <div className="mt-1 grid min-w-0 grid-cols-[minmax(0,9rem)_minmax(0,1fr)_3.5rem] items-center gap-3 border-t pt-2.5 text-xs sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)_3.5rem]">
        <span className="font-medium">总分</span>
        <span className="relative h-5 rounded bg-muted/50">
          <span className={cn("absolute inset-y-0 left-0 rounded", sw.solid)} style={{ width: pct(r.score) }} />
        </span>
        <span className="text-right font-semibold tabular-nums">{r.score}</span>
      </div>
    </div>
  );
}

function ResultView({ result, scene, draft, names }: { result: SimulateResult; scene: Scene; draft: boolean; names: Map<string, string> }) {
  const r = result.result;
  const sw = ACTION_SWATCH[r.action] ?? ACTION_SWATCH.pass;
  const Icon = VERDICT_ICON[r.action] ?? CheckCircle2;
  const dims = Object.entries(r.dimension_scores ?? {}).filter(([, v]) => v !== 0).sort((a, b) => b[1] - a[1]);
  const dimMax = Math.max(1, ...dims.map(([, v]) => Math.abs(v)));
  const models = Object.entries(r.model_scores ?? {});
  const commands = (r.commands ?? []) as Cmd[];
  const degraded = r.degraded ?? [];
  const features = Object.entries(result.features ?? result.trace?.features ?? {}).sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]));
  const [showFeatures, setShowFeatures] = useState(false);

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <section className={cn("flex min-w-0 flex-col gap-4 rounded-xl border p-4 sm:p-5", sw.ring, sw.soft)}>
        <div className="flex min-w-0 items-start gap-3">
          <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl", sw.solid, sw.onSolid)}><Icon className="size-5" /></span>
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <h3 className="text-base leading-6 font-semibold">{ACTION_VERDICT[r.action] ?? "评估完成"}</h3>
            <p className="text-sm leading-6 text-foreground/80">因为{because(r, scene)}。</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Badge variant="outline" size="default">{LEVEL_WORD[r.risk_level] ?? r.risk_level}</Badge>
          <Badge variant="outline" size="default">{draft ? "按草稿评估" : r.release ? `按线上版本 ${r.release} 评估` : "按线上版本评估"}</Badge>
          {r.latency_us ? <Badge variant="outline" size="default">用时 {latencyText(r.latency_us)}</Badge> : null}
        </div>
        {(r.reasons ?? []).length > 1 ? (
          <ul className="flex flex-col gap-1">
            {(r.reasons ?? []).map((x, i) => <li key={i} className="flex gap-2 text-xs leading-5"><span className="text-muted-foreground tabular-nums">{i + 1}</span><span>{x}</span></li>)}
          </ul>
        ) : null}
      </section>

      {degraded.length ? (
        <Callout tone="warning" icon={AlertTriangle} title="部分数据没有取到">
          {degraded.map((d) => SOURCE_LABEL[d] ?? STAGE_LABEL[d] ?? "数据源").join("、")}未能按时返回，结论可能偏轻。
        </Callout>
      ) : null}

      <StudioSection title="总分落在哪里" aside={<span className="text-2xl leading-none font-semibold tabular-nums"><NumberFlow value={r.score} suffix=" 分" /></span>}>
        {scene.bands?.length ? <ScoreBandBar bands={scene.bands} marker={r.score} /> : <p className="text-xs text-muted-foreground">草稿没有分数段</p>}
        {r.decided_by !== "default" && r.decided_by !== "rule" ? <p className="text-xs text-muted-foreground">本次结论不由分数段决定，分数段仅供参考。</p> : null}
      </StudioSection>

      <StudioSection title="分数构成" description="生效规则依次累加；观察中的规则只记录，不计入总分。">
        <ScoreWaterfall r={r} />
      </StudioSection>

      {dims.length || models.length ? (
        <div className="grid min-w-0 gap-5 lg:grid-cols-2">
          {dims.length ? (
            <StudioSection title="各归类得分">
              <ul className="flex flex-col gap-1.5">
                {dims.map(([d, v]) => (
                  <li key={d} className="grid grid-cols-[4.5rem_minmax(0,1fr)_2.5rem] items-center gap-3 text-xs">
                    <span className="truncate">{DIM_LABEL[d] ?? d}</span>
                    <span className="h-2 overflow-hidden rounded-full bg-muted"><span className="block h-full rounded-full bg-foreground/70" style={{ width: `${(Math.abs(v) / dimMax) * 100}%` }} /></span>
                    <span className="text-right tabular-nums">{v}</span>
                  </li>
                ))}
              </ul>
            </StudioSection>
          ) : null}
          {models.length ? (
            <StudioSection title="模型分">
              <ul className="flex flex-col gap-1.5">
                {models.map(([k, v]) => (
                  <li key={k} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_3rem] items-center gap-3 text-xs">
                    <span className="truncate">{names.get(`M.${k}`) ?? "未命名模型"}</span>
                    <span className="h-2 overflow-hidden rounded-full bg-muted"><span className="block h-full rounded-full bg-foreground/70" style={{ width: `${Math.min(1, Math.max(0, v)) * 100}%` }} /></span>
                    <span className="text-right tabular-nums">{v.toFixed(2)}</span>
                  </li>
                ))}
              </ul>
            </StudioSection>
          ) : null}
        </div>
      ) : null}

      {(r.list_hits ?? []).length || commands.length ? (
        <StudioSection title="名单与处置">
          {(r.list_hits ?? []).length ? (
            <ul className="flex flex-col gap-1.5">
              {(r.list_hits ?? []).map((l, i) => (
                <li key={i} className="flex flex-wrap items-center gap-2 text-sm">
                  <Badge variant={l.kind === "block" ? "danger" : l.kind === "allow" ? "success" : "warning"} size="sm">{KIND_LABEL[l.kind as ListKind] ?? "名单"}</Badge>
                  <span>{LIST_TYPE_LABEL[l.list_type as ListType] ?? "请求信息"} <span className="font-mono text-xs">{l.value}</span></span>
                  {l.reason ? <span className="text-xs text-muted-foreground">{l.reason}</span> : null}
                </li>
              ))}
            </ul>
          ) : null}
          {commands.length ? (
            <div className="flex flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">正式运行时将执行（模拟不会真正执行）</span>
              <ul className="flex flex-col gap-1.5">
                {commands.map((c, i) => (
                  <li key={c.id ?? i} className="flex items-start gap-2 rounded-lg border px-3 py-2 text-sm">
                    <Gavel className="mt-0.5 size-3.5 shrink-0 text-red-600 dark:text-red-400" />
                    <span className="min-w-0 break-words">{commandText(c)}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </StudioSection>
      ) : null}

      {result.trace ? (
        <StudioSection title="评估过程" description="每一步的用时与规则命中情况。">
          <TraceView trace={result.trace} />
        </StudioSection>
      ) : null}

      {features.length ? (
        <Collapsible open={showFeatures} onOpenChange={setShowFeatures}>
          <CollapsibleTrigger asChild>
            <button type="button" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
              <ChevronDown className={cn("size-3.5 transition-transform", showFeatures && "rotate-180")} />
              本次取到的特征值（{features.length} 个）
            </button>
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-2">
            <FeatureValues values={features} names={names} />
          </CollapsibleContent>
        </Collapsible>
      ) : null}
    </div>
  );
}

function FeatureValues({ values, names }: { values: Array<[string, number]>; names: Map<string, string> }) {
  return (
    <ul className="grid gap-x-6 gap-y-1 rounded-lg border p-3 sm:grid-cols-2">
      {values.map(([k, v]) => (
        <li key={k} className="flex min-w-0 items-center justify-between gap-3 text-xs">
          <span className="min-w-0 truncate text-muted-foreground">{names.get(k) ?? "未命名特征"}</span>
          <span className={cn("shrink-0 tabular-nums", v !== 0 && "font-medium")}>{Number.isInteger(v) ? v : v.toFixed(2)}</span>
        </li>
      ))}
    </ul>
  );
}

/* ════════════════════════ 面板 ════════════════════════ */

export function SimulatePanel({ sceneKey, scene, meta, bundleFeatures, dirty, released, options }: {
  sceneKey: string;
  options: VarOption[];
  scene: Scene;
  meta: RiskMeta;
  bundleFeatures: Feature[];
  dirty: boolean;
  /** 是否已有线上版本 */
  released: boolean;
}) {
  const simulate = useSimulateScene();
  const [form, setForm] = useState<Form>(EMPTY);
  const [mode, setMode] = useState<"fixed" | "live">("fixed");
  const [draft, setDraft] = useState(true);
  const [features, setFeatures] = useState<Record<string, string>>({});
  const [lists, setLists] = useState<ListHit[]>([]);
  const [extraValues, setExtraValues] = useState<Record<string, string | boolean>>({});
  const [extraJson, setExtraJson] = useState("{}");
  const [jsonMode, setJsonMode] = useState(false);
  const [extraError, setExtraError] = useState<string | null>(null);
  const [ranDraft, setRanDraft] = useState(true);
  const [featureQ, setFeatureQ] = useState("");
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const fields = scene.fields ?? [];
  const sourceError = simulate.error as (Error & { status?: number }) | null;
  const names = new Map<string, string>([
    ...options.filter((o) => o.name.startsWith("M.")).map((o) => [o.name, o.label] as [string, string]),
    ...bundleFeatures.map((f) => [f.key, f.name] as [string, string]),
  ]);

  const applyPreset = (p: (typeof PRESETS)[number]) => {
    setForm({ ...EMPTY, appId: form.appId, ...p.form });
    setLists(p.lists ?? []);
    if (p.lists?.length) setMode("fixed");
    toast.success(`已填入「${p.title}」`);
  };

  const run = () => {
    let extra: Record<string, unknown> | undefined;
    if (jsonMode) {
      if (extraJson.trim()) {
        try {
          const parsed = JSON.parse(extraJson) as unknown;
          if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("需要一个对象");
          extra = Object.keys(parsed).length ? (parsed as Record<string, unknown>) : undefined;
        } catch (e) {
          setExtraError(errorText(e, "格式有误"));
          return;
        }
      }
    } else if (fields.length) {
      const entries = fields.map((f) => [f.key, extraValue(f, extraValues[f.key])] as const).filter(([, v]) => v !== undefined);
      extra = entries.length ? Object.fromEntries(entries) : undefined;
    }
    setExtraError(null);
    const fixed = Object.fromEntries(bundleFeatures.map((f) => [f.key, num(features[f.key] ?? "") ?? f.default ?? 0]));
    setRanDraft(draft);
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
      { onError: (e) => toast.error(errorText(e, "评估失败")) },
    );
  };

  const fq = featureQ.trim().toLowerCase();
  const shownFeatures = bundleFeatures.filter((f) => !fq || f.name.toLowerCase().includes(fq));
  const changedFeatures = Object.values(features).filter((v) => v.trim()).length;

  return (
    <div className="grid min-w-0 items-start gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
      <div className="flex min-w-0 flex-col gap-5">
        <StudioSection
          icon={FlaskConical}
          title="假想一次请求"
          description="填写请求信息后开始评估，不会产生真实处置。"
          aside={
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="outline"><Sparkles />快速填充</Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                {PRESETS.map((p) => (
                  <DropdownMenuItem key={p.key} onSelect={() => applyPreset(p)} className="flex-col items-start gap-0.5">
                    <span className="text-sm">{p.title}</span>
                    <span className="text-xs text-muted-foreground">{p.hint}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          }
        >
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="评估哪一份">
              <Segmented
                value={draft ? "draft" : "live"}
                onChange={(v) => setDraft(v === "draft")}
                options={[{ value: "draft", label: "草稿" }, ...(released ? [{ value: "live", label: "线上版本" }] : [])]}
                ariaLabel="评估哪一份"
              />
            </Field>
            <Field label="特征从哪里来">
              <Segmented
                value={mode}
                onChange={setMode}
                options={[{ value: "fixed" as const, label: "手动设定" }, { value: "live" as const, label: "实时数据" }]}
                ariaLabel="特征从哪里来"
              />
            </Field>
          </div>
          {draft && dirty ? <Callout tone="warning">有未保存的修改，评估使用的是已保存的草稿。</Callout> : null}
          {mode === "live" ? <Callout>按真实的计数、关系图与名单取值，结果与线上一致。</Callout> : null}

          <FormGroup icon={User} title="请求方">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="应用"><AppScopeSelect value={form.appId} onChange={(v) => set("appId", v)} className="h-9 w-full" allLabel="不指定" /></Field>
              <Field label="用户编号"><Input inputMode="numeric" value={form.userId} placeholder="未登录可留空" onChange={(e) => set("userId", e.target.value)} /></Field>
              <Field label="登录名"><Input value={form.account} onChange={(e) => set("account", e.target.value)} /></Field>
              <Field label="手机号"><Input value={form.phone} onChange={(e) => set("phone", e.target.value)} /></Field>
              <Field label="邮箱"><Input value={form.email} onChange={(e) => set("email", e.target.value)} /></Field>
              <Field label="金额"><Input inputMode="decimal" value={form.amount} onChange={(e) => set("amount", e.target.value)} /></Field>
            </div>
          </FormGroup>

          <FormGroup icon={Globe} title="网络">
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="IP 地址"><Input className="font-mono" value={form.ip} onChange={(e) => set("ip", e.target.value)} /></Field>
              <Field label="国家或地区代码"><Input className="uppercase" maxLength={2} placeholder="CN" value={form.country} onChange={(e) => set("country", e.target.value.toUpperCase())} /></Field>
              <Field label="IP 情报分"><Input inputMode="numeric" placeholder="0 至 100" value={form.ipScore} onChange={(e) => set("ipScore", e.target.value)} /></Field>
            </div>
            <div className="grid gap-2 sm:grid-cols-4">
              <Toggle label="代理" checked={form.proxy} onChange={(v) => set("proxy", v)} />
              <Toggle label="VPN" checked={form.vpn} onChange={(v) => set("vpn", v)} />
              <Toggle label="Tor" checked={form.tor} onChange={(v) => set("tor", v)} />
              <Toggle label="机房 IP" checked={form.datacenter} onChange={(v) => set("datacenter", v)} />
            </div>
          </FormGroup>

          <FormGroup icon={Smartphone} title="设备与客户端">
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="设备编号"><Input className="font-mono" value={form.deviceId} onChange={(e) => set("deviceId", e.target.value)} /></Field>
              <Field label="平台"><Input value={form.platform} placeholder="android" onChange={(e) => set("platform", e.target.value)} /></Field>
              <Field label="品牌"><Input value={form.brand} onChange={(e) => set("brand", e.target.value)} /></Field>
              <Field label="型号"><Input value={form.model} onChange={(e) => set("model", e.target.value)} /></Field>
              <Field label="系统版本"><Input value={form.osVersion} onChange={(e) => set("osVersion", e.target.value)} /></Field>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <Toggle label="模拟器" checked={form.emulator} onChange={(v) => set("emulator", v)} />
              <Toggle label="已 Root 或越狱" checked={form.rooted} onChange={(v) => set("rooted", v)} />
            </div>
            <Field label="User-Agent"><Input className="font-mono text-xs" value={form.userAgent} onChange={(e) => set("userAgent", e.target.value)} /></Field>
          </FormGroup>

          <FormGroup icon={ShieldCheck} title="验证码">
            <div className="grid gap-2 sm:grid-cols-2">
              <Toggle label="已通过图形验证码" checked={form.captchaPassed} onChange={(v) => set("captchaPassed", v)} />
              <Toggle label="应用未配置验证码" checked={form.captchaUnavailable} onChange={(v) => set("captchaUnavailable", v)} />
            </div>
          </FormGroup>

          <FormGroup icon={Plus} title="业务入参">
            {!jsonMode && fields.length ? (
              <div className="grid gap-3 sm:grid-cols-2">
                {fields.map((f) => (
                  <Field key={f.key} label={`${f.label || f.key}${f.required ? "（必填）" : ""}`}>
                    {f.type === "bool" ? (
                      <Select value={extraValues[f.key] === true ? "yes" : extraValues[f.key] === false ? "no" : "unset"} onValueChange={(v) => setExtraValues({ ...extraValues, [f.key]: v === "yes" ? true : v === "no" ? false : "" })}>
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="unset">不传</SelectItem>
                          <SelectItem value="yes">是</SelectItem>
                          <SelectItem value="no">否</SelectItem>
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input
                        value={typeof extraValues[f.key] === "string" ? (extraValues[f.key] as string) : ""}
                        inputMode={f.type === "number" ? "decimal" : undefined}
                        placeholder={f.type === "list" ? "多个值用逗号分隔" : f.type === "object" ? "JSON 对象" : undefined}
                        onChange={(e) => setExtraValues({ ...extraValues, [f.key]: e.target.value })}
                      />
                    )}
                  </Field>
                ))}
              </div>
            ) : !jsonMode ? (
              <p className="text-xs text-muted-foreground">场景没有登记业务入参。</p>
            ) : (
              <Field label="以 JSON 填写" error={extraError}>
                <JsonEditor height={120} value={extraJson} onChange={setExtraJson} />
              </Field>
            )}
            <Button size="xs" variant="ghost" className="w-fit text-muted-foreground" onClick={() => setJsonMode(!jsonMode)}>
              {jsonMode ? "按字段填写" : "以 JSON 填写"}
            </Button>
          </FormGroup>
        </StudioSection>

        {mode === "fixed" ? (
          <>
            <StudioSection
              title="特征取值"
              description={bundleFeatures.length ? `留空即用默认值。已手动设定 ${changedFeatures} 个。` : undefined}
              aside={changedFeatures ? <Button size="xs" variant="ghost" onClick={() => setFeatures({})}><RotateCcw />全部恢复默认</Button> : null}
            >
              {bundleFeatures.length === 0 ? (
                <p className="text-sm text-muted-foreground">草稿没有用到特征。</p>
              ) : (
                <>
                  {bundleFeatures.length > 8 ? <Input className="h-8" placeholder="搜索特征" value={featureQ} onChange={(e) => setFeatureQ(e.target.value)} /> : null}
                  <div className="grid gap-x-4 gap-y-2 sm:grid-cols-2">
                    {shownFeatures.map((f) => (
                      <div key={f.key} className="flex min-w-0 items-center gap-2">
                        <div className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate text-xs" title={f.name}>{f.name}</span>
                          <span className="truncate text-[11px] text-muted-foreground">{FEATURE_KIND_LABEL[f.kind]}</span>
                        </div>
                        <Input
                          className={cn("h-8 w-20 text-right tabular-nums", features[f.key]?.trim() && "border-foreground/40")}
                          inputMode="decimal"
                          placeholder={String(f.default ?? 0)}
                          value={features[f.key] ?? ""}
                          aria-label={f.name}
                          onChange={(e) => setFeatures({ ...features, [f.key]: e.target.value })}
                        />
                      </div>
                    ))}
                  </div>
                </>
              )}
            </StudioSection>
            <StudioSection
              title="假设命中名单"
              aside={
                <Button size="xs" variant="outline" onClick={() => setLists([...lists, { listType: "ip", kind: "block", value: form.ip }])}>
                  <Plus />
                  添加
                </Button>
              }
            >
              {lists.length === 0 ? <p className="text-sm text-muted-foreground">不假设任何名单命中。</p> : null}
              {lists.map((l, i) => (
                <div key={i} className="grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1fr)] items-center gap-2 sm:grid-cols-[8rem_minmax(0,1fr)_7rem_auto]">
                  <Select value={l.listType} onValueChange={(v) => setLists(lists.map((x, j) => (j === i ? { ...x, listType: v as ListType } : x)))}>
                    <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>{meta.listTypes.map((t) => <SelectItem key={t} value={t}>{LIST_TYPE_LABEL[t]}</SelectItem>)}</SelectContent>
                  </Select>
                  <Input className="h-8 font-mono text-xs" value={l.value} placeholder="取值" onChange={(e) => setLists(lists.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} />
                  <Select value={l.kind} onValueChange={(v) => setLists(lists.map((x, j) => (j === i ? { ...x, kind: v as ListKind } : x)))}>
                    <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>{(["block", "allow", "watch"] as ListKind[]).map((k) => <SelectItem key={k} value={k}>在{KIND_LABEL[k]}中</SelectItem>)}</SelectContent>
                  </Select>
                  <Button size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setLists(lists.filter((_, j) => j !== i))}>
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))}
            </StudioSection>
          </>
        ) : null}

        <div className="sticky bottom-4 z-10 flex justify-end">
          <Button onClick={run} disabled={simulate.isPending} className="shadow-sm">
            {simulate.isPending ? <LoaderCircle className="animate-spin" /> : <Play />}
            开始评估
          </Button>
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-5 xl:sticky xl:top-4">
        {simulate.isPending ? (
          <div className="flex flex-col gap-4">
            <Skeleton className="h-36 rounded-xl" />
            <Skeleton className="h-28 rounded-xl" />
            <Skeleton className="h-48 rounded-xl" />
          </div>
        ) : sourceError ? (
          <Callout tone="danger" title={sourceError.status === 422 ? "草稿校验不通过，无法评估" : "评估失败"}>
            {errorText(sourceError, "请稍后重试")}
          </Callout>
        ) : simulate.data ? (
          <ResultView key={simulate.submittedAt} result={simulate.data} scene={scene} draft={ranDraft} names={names} />
        ) : (
          <EmptyBlock
            icon={FlaskConical}
            title="还没有评估结果"
            description="在左侧填写请求，或用「快速填充」选一个典型情形，然后开始评估。"
            action={<Button size="sm" onClick={run}><Play />开始评估</Button>}
            className="bg-card"
          />
        )}
      </div>
    </div>
  );
}
