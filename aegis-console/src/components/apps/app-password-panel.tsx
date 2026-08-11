"use client";

import { useMemo, useState } from "react";
import {
  CheckCircle2,
  Eye,
  EyeOff,
  FlaskConical,
  History,
  KeyRound,
  Loader2,
  RotateCcw,
  Save,
  ShieldCheck,
  Timer,
  XCircle
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { cn } from "@/lib/utils";
import type { PasswordPolicy, PasswordPolicyStats, PasswordPolicyTestResult } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import {
  useAdminAppPasswordPolicyQuery,
  usePasswordPolicyTemplatesQuery,
  useResetAdminAppPasswordPolicyMutation,
  useTestAdminAppPasswordPolicyMutation,
  useUpdateAdminAppPasswordPolicyMutation
} from "@/lib/admin-hooks";
import {
  FieldGroup,
  NoAppSelected,
  SectionCard,
  SliderField,
  SwitchRow
} from "@/components/apps/app-config-primitives";

type Form = {
  name: string;
  description: string;
  minLength: number;
  maxLength: number;
  requireUppercase: boolean;
  requireLowercase: boolean;
  requireNumbers: boolean;
  requireSpecialChars: boolean;
  /** 0-100，与后端 AnalyzePasswordStrength 同量纲 */
  minScore: number;
  /** 天，0 = 永不过期 */
  maxAge: number;
  /** 条，0 = 不限制；后端上限 20 */
  preventReuse: number;
};

const defaultForm: Form = {
  name: "默认密码策略",
  description: "",
  minLength: 8,
  maxLength: 128,
  requireUppercase: false,
  requireLowercase: true,
  requireNumbers: true,
  requireSpecialChars: false,
  minScore: 40,
  maxAge: 365,
  preventReuse: 5
};

function seed(policy?: PasswordPolicy | null): Form {
  if (!policy) return defaultForm;
  return {
    name: policy.name ?? defaultForm.name,
    description: policy.description ?? "",
    minLength: policy.minLength ?? defaultForm.minLength,
    maxLength: policy.maxLength ?? defaultForm.maxLength,
    requireUppercase: Boolean(policy.requireUppercase),
    requireLowercase: policy.requireLowercase !== false,
    requireNumbers: policy.requireNumbers !== false,
    requireSpecialChars: Boolean(policy.requireSpecialChars),
    // 这三项的 0 是有效取值，不能用 `?? 默认值` 兜底 —— 那会把「不限制」改回默认
    minScore: policy.minScore ?? defaultForm.minScore,
    maxAge: policy.maxAge ?? defaultForm.maxAge,
    preventReuse: policy.preventReuse ?? defaultForm.preventReuse
  };
}

/** 强度分档，与后端 AnalyzePasswordStrength 的 level 分界一致（80/60/40/20） */
function scoreTier(score: number): { label: string; tone: string; bar: string } {
  if (score >= 80) return { label: "极强", tone: "text-emerald-600 dark:text-emerald-400", bar: "bg-emerald-500" };
  if (score >= 60) return { label: "强", tone: "text-emerald-600 dark:text-emerald-400", bar: "bg-emerald-500" };
  if (score >= 40) return { label: "中等", tone: "text-amber-600 dark:text-amber-400", bar: "bg-amber-500" };
  if (score >= 20) return { label: "弱", tone: "text-orange-600 dark:text-orange-400", bar: "bg-orange-500" };
  return { label: "极弱", tone: "text-red-600 dark:text-red-400", bar: "bg-red-500" };
}

const TEMPLATE_LABELS: Record<string, string> = {
  basic: "基础",
  standard: "标准",
  strict: "严格",
  enterprise: "企业"
};

export function AppPasswordPanel({ appKey }: { appKey?: string | null }) {
  const policyQuery = useAdminAppPasswordPolicyQuery(appKey);
  const templatesQuery = usePasswordPolicyTemplatesQuery();
  const updateMutation = useUpdateAdminAppPasswordPolicyMutation(appKey);
  const resetMutation = useResetAdminAppPasswordPolicyMutation(appKey);
  const testMutation = useTestAdminAppPasswordPolicyMutation(appKey);

  const view = policyQuery.data;
  const [testInput, setTestInput] = useState("");
  const [showTestInput, setShowTestInput] = useState(false);

  // 草稿按 appKey 绑定作用域，无草稿时从服务端数据派生（不用 useEffect 同步）。
  // templateKey 一起放进草稿：它描述的是「当前这份草稿基于哪个模板」，
  // 与表单值同生同灭，分开存会出现「表单已重置、下拉框还停在某个模板」。
  const [draft, setDraft] = useState<{ scope: string; value: Form; templateKey: string } | null>(null);
  const scope = appKey ?? "";
  const active = draft?.scope === scope ? draft : null;
  const form = active?.value ?? seed(view?.policy);
  // 旧实现把 Select 的 value 写死成 "__custom__"，于是套用任何模板后下拉框仍显示
  // 「自定义」，管理员无法确认套用成功 —— 这里如实反映当前草稿的来源。
  const templateKey = active?.templateKey ?? "__custom__";

  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value }, templateKey: "__custom__" });

  const templates = templatesQuery.data?.templates ?? {};

  function applyTemplate(key: string) {
    if (key === "__custom__") {
      setDraft({ scope, value: form, templateKey: "__custom__" });
      return;
    }
    const template = templates[key];
    if (!template) return;
    setDraft({ scope, value: seed(template), templateKey: key });
    toast.success(`已套用「${template.name || TEMPLATE_LABELS[key] || key}」模板，保存后生效`);
  }

  async function handleSave() {
    if (form.minLength > form.maxLength) {
      toast.error("最小长度不能大于最大长度");
      return;
    }
    try {
      await updateMutation.mutateAsync({
        policy: {
          name: form.name.trim() || defaultForm.name,
          description: form.description.trim(),
          minLength: form.minLength,
          maxLength: form.maxLength,
          requireUppercase: form.requireUppercase,
          requireLowercase: form.requireLowercase,
          requireNumbers: form.requireNumbers,
          requireSpecialChars: form.requireSpecialChars,
          minScore: form.minScore,
          maxAge: form.maxAge,
          preventReuse: form.preventReuse
        }
      });
      toast.success("密码策略已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleReset() {
    try {
      await resetMutation.mutateAsync();
      // 丢弃草稿，让表单回到刚拉回来的服务端默认值
      setDraft(null);
      toast.success("已恢复默认密码策略");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "恢复默认失败");
    }
  }

  async function handleTest() {
    if (!testInput.trim()) return;
    try {
      await testMutation.mutateAsync({ password: testInput });
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "测试失败");
    }
  }

  if (!appKey) return <NoAppSelected icon={<KeyRound className="size-8" />} />;
  if (policyQuery.isLoading) {
    return (
      <div className="grid gap-5 xl:grid-cols-[1.2fr_0.8fr]">
        <Skeleton className="h-[32rem] w-full rounded-2xl" />
        <Skeleton className="h-80 w-full rounded-2xl" />
      </div>
    );
  }

  return (
    <div className="grid gap-5 xl:grid-cols-[1.2fr_0.8fr] xl:items-start">
      <SectionCard
        icon={<KeyRound className="size-4" />}
        title="密码策略"
        description="强度、长度与生命周期。所有字段在注册、改密、管理员重置三条链路上统一生效。"
        aside={
          view?.policy?.isDefault ? (
            <Badge variant="outline" className="text-[10px]">使用平台默认</Badge>
          ) : (
            <Badge variant="success" className="text-[10px]">已自定义</Badge>
          )
        }
        footer={
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-[11px] text-muted-foreground">
              {templateKey === "__custom__" ? "当前为自定义配置" : `基于「${TEMPLATE_LABELS[templateKey] ?? templateKey}」模板`}
            </span>
            <div className="flex gap-2">
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button size="sm" variant="outline" disabled={resetMutation.isPending}>
                    <RotateCcw className="size-3.5" />
                    恢复默认
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>恢复为平台默认密码策略？</AlertDialogTitle>
                    <AlertDialogDescription>
                      当前应用的自定义密码策略会被清除，改为跟随平台默认值。已注册用户的密码不受影响，
                      但下次改密时按新策略校验。
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>取消</AlertDialogCancel>
                    <AlertDialogAction onClick={handleReset}>确认恢复</AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
              <Button size="sm" disabled={updateMutation.isPending} onClick={handleSave}>
                <Save className="size-3.5" />
                {updateMutation.isPending ? "保存中..." : "保存策略"}
              </Button>
            </div>
          </div>
        }
      >
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-[1fr_minmax(0,180px)]">
            <div className="space-y-1.5">
              <label className="text-[11px] font-medium">策略名称</label>
              <Input value={form.name} onChange={(e) => patch("name", e.target.value)} placeholder="默认密码策略" />
            </div>
            <div className="space-y-1.5">
              <label className="text-[11px] font-medium">套用模板</label>
              <Select value={templateKey} onValueChange={applyTemplate}>
                <SelectTrigger>
                  <SelectValue placeholder="选择模板" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__custom__">自定义</SelectItem>
                  {Object.entries(templates).map(([key, template]) => (
                    <SelectItem key={key} value={key}>
                      {template.name || TEMPLATE_LABELS[key] || key}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <label className="text-[11px] font-medium">说明</label>
            <Textarea
              value={form.description}
              onChange={(e) => patch("description", e.target.value)}
              rows={2}
              placeholder="这条策略的适用范围与来源，便于后续接手的人判断能否调整"
            />
          </div>

          <FieldGroup label="长度限制">
            <div className="grid gap-4 sm:grid-cols-2">
              <SliderField
                label="最小长度"
                value={form.minLength}
                min={1}
                max={50}
                unit="字符"
                onChange={(v) => patch("minLength", v)}
              />
              <SliderField
                label="最大长度"
                value={form.maxLength}
                min={16}
                max={256}
                step={8}
                unit="字符"
                onChange={(v) => patch("maxLength", v)}
              />
            </div>
            {form.minLength > form.maxLength ? (
              <p className="text-[10px] text-destructive">最小长度不能大于最大长度</p>
            ) : null}
          </FieldGroup>

          <FieldGroup label="字符复杂度">
            <div className="grid gap-2.5 sm:grid-cols-2 xl:grid-cols-4">
              <SwitchRow label="大写字母" hint="A-Z" checked={form.requireUppercase} onChange={(v) => patch("requireUppercase", v)} />
              <SwitchRow label="小写字母" hint="a-z" checked={form.requireLowercase} onChange={(v) => patch("requireLowercase", v)} />
              <SwitchRow label="数字" hint="0-9" checked={form.requireNumbers} onChange={(v) => patch("requireNumbers", v)} />
              <SwitchRow label="特殊字符" hint="!@#$% 等" checked={form.requireSpecialChars} onChange={(v) => patch("requireSpecialChars", v)} />
            </div>
          </FieldGroup>

          <FieldGroup label="强度门槛" hint="与右侧测试器同一套评分">
            <SliderField
              label="最低强度分"
              value={form.minScore}
              min={0}
              max={100}
              step={5}
              valueLabel={form.minScore === 0 ? "不校验强度" : `${form.minScore} 分 · ${scoreTier(form.minScore).label}`}
              hint="0-100 分制"
              onChange={(v) => patch("minScore", v)}
            />
          </FieldGroup>

          <FieldGroup label="生命周期" hint="0 均表示关闭该项约束">
            <div className="grid gap-4 sm:grid-cols-2">
              <SliderField
                label="密码有效期"
                value={form.maxAge}
                min={0}
                max={730}
                step={5}
                valueLabel={form.maxAge === 0 ? "永不过期" : `${form.maxAge} 天`}
                hint="到期后登录返回「须改密」"
                onChange={(v) => patch("maxAge", v)}
              />
              <SliderField
                label="禁止重用近期密码"
                value={form.preventReuse}
                min={0}
                max={20}
                valueLabel={form.preventReuse === 0 ? "不限制" : `最近 ${form.preventReuse} 个`}
                hint="逐条 bcrypt 比对，上限 20"
                onChange={(v) => patch("preventReuse", v)}
              />
            </div>
          </FieldGroup>

          <div className="grid gap-3 sm:grid-cols-2">
            <LifecycleNote
              icon={<Timer className="size-3.5" />}
              title={form.maxAge === 0 ? "密码永不过期" : `密码 ${form.maxAge} 天后过期`}
              body={
                form.maxAge === 0
                  ? "已存在的过期时间会在下次改密时被清除。"
                  : "过期判定在登录时现算，不依赖定时任务；到期用户登录仍成功，但结果里带 passwordChangeRequired。"
              }
            />
            <LifecycleNote
              icon={<History className="size-3.5" />}
              title={form.preventReuse === 0 ? "不限制密码重用" : `禁止重用最近 ${form.preventReuse} 个密码`}
              body={
                form.preventReuse === 0
                  ? "设为 0 会同时清空该用户已积累的历史密码记录。"
                  : "历史只存哈希；改密与管理员重置都会写入并裁剪到该条数。"
              }
            />
          </div>
        </div>
      </SectionCard>

      <div className="space-y-5">
        <ComplianceCard stats={view?.stats} minScore={form.minScore} />

        <SectionCard
          icon={<FlaskConical className="size-4" />}
          title="策略测试"
          description="用当前已保存的策略校验一个候选密码。"
        >
          <div className="space-y-4">
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Input
                  type={showTestInput ? "text" : "password"}
                  value={testInput}
                  onChange={(e) => setTestInput(e.target.value)}
                  placeholder="输入密码进行测试"
                  className="pr-9"
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void handleTest();
                  }}
                />
                {testInput && (
                  <button
                    type="button"
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    onClick={() => setShowTestInput(!showTestInput)}
                    aria-label={showTestInput ? "隐藏密码" : "显示密码"}
                  >
                    {showTestInput ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </button>
                )}
              </div>
              <Button size="sm" disabled={!testInput.trim() || testMutation.isPending} onClick={handleTest}>
                {testMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <FlaskConical className="size-3.5" />}
                测试
              </Button>
            </div>
            <p className="text-[10px] text-muted-foreground">
              测试走服务端评分，密码不会被记录（响应里只回掩码）。未保存的改动不参与本次校验。
            </p>
            {testMutation.data ? <TestResultView result={testMutation.data} /> : null}
          </div>
        </SectionCard>
      </div>
    </div>
  );
}

function LifecycleNote({ icon, title, body }: { icon: React.ReactNode; title: string; body: string }) {
  return (
    <div className="rounded-xl bg-muted p-3">
      <div className="flex items-center gap-1.5 text-xs font-medium">
        <span className="text-muted-foreground">{icon}</span>
        {title}
      </div>
      <p className="mt-1 text-[10px] leading-relaxed text-muted-foreground">{body}</p>
    </div>
  );
}

/**
 * 合规看板。取代原先直接把 stats 对象 JSON.stringify 打屏的做法 ——
 * 那种展示方式让「达标率 0%」和「还没有用户」看起来完全一样。
 */
function ComplianceCard({ stats, minScore }: { stats?: PasswordPolicyStats; minScore: number }) {
  const hasUsers = (stats?.totalUsers ?? 0) > 0;
  return (
    <SectionCard
      icon={<ShieldCheck className="size-4" />}
      title="合规概况"
      description={`按当前生效策略的最低强度分（${minScore}）统计。`}
    >
      {!stats || !hasUsers ? (
        <div className="rounded-xl bg-muted p-4 text-center text-xs text-muted-foreground">
          该应用还没有用户，暂无合规数据。
        </div>
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Metric label="总用户" value={stats.totalUsers} />
            <Metric label="密码用户" value={stats.passwordUsers} hint="其余为纯第三方登录" />
          </div>
          <div className="space-y-2">
            <div className="flex items-baseline justify-between text-xs">
              <span className="font-medium">强度达标</span>
              <span className="font-mono tabular-nums">
                {stats.compliantUsers} / {stats.totalUsers}
                <span className="ml-1.5 text-muted-foreground">{stats.complianceRate}%</span>
              </span>
            </div>
            <Progress value={stats.complianceRate} className="h-1.5" />
          </div>
          <div className="space-y-2">
            <div className="flex items-baseline justify-between text-xs">
              <span className="font-medium">须改密</span>
              <span className="font-mono tabular-nums">
                {stats.needChangeUsers}
                <span className="ml-1.5 text-muted-foreground">{stats.needChangeRate}%</span>
              </span>
            </div>
            <Progress value={stats.needChangeRate} className="h-1.5" />
            <p className="text-[10px] text-muted-foreground">
              含被管理员标记的强制改密，以及密码已超过有效期的用户。
            </p>
          </div>
        </div>
      )}
    </SectionCard>
  );
}

function Metric({ label, value, hint }: { label: string; value: number; hint?: string }) {
  return (
    <div className="rounded-xl bg-muted px-3 py-2.5">
      <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</div>
      <div className="mt-0.5 font-mono text-lg tabular-nums leading-none">{value}</div>
      {hint && <div className="mt-1 text-[10px] text-muted-foreground">{hint}</div>}
    </div>
  );
}

/** 结构化展示测试结果，取代原先的 JSON dump */
function TestResultView({ result }: { result: PasswordPolicyTestResult }) {
  const summary = result.result;
  const analysis = result.strengthAnalysis;
  const score = summary?.score ?? analysis?.score ?? 0;
  const tier = scoreTier(score);
  const violations = summary?.violations ?? [];
  const patterns = analysis?.details?.hasCommonPatterns ?? [];
  // 带位置的结构化明细优先；后端未升级时回落到只有标签的旧字段
  const patternDetails = analysis?.details?.patterns ?? [];

  const checks = useMemo(
    () => [
      { label: "小写字母", ok: Boolean(analysis?.details?.hasLowercase) },
      { label: "大写字母", ok: Boolean(analysis?.details?.hasUppercase) },
      { label: "数字", ok: Boolean(analysis?.details?.hasNumbers) },
      { label: "特殊字符", ok: Boolean(analysis?.details?.hasSpecialChars) }
    ],
    [analysis]
  );

  return (
    <div className="space-y-4 rounded-xl border border-border bg-muted/30 p-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          {summary?.isValid ? (
            <CheckCircle2 className="size-4 text-emerald-600 dark:text-emerald-400" />
          ) : (
            <XCircle className="size-4 text-destructive" />
          )}
          <span className="text-sm font-medium">{summary?.isValid ? "符合策略" : "不符合策略"}</span>
        </div>
        <span className={cn("font-mono text-sm tabular-nums", tier.tone)}>
          {score} 分 · {tier.label}
        </span>
      </div>

      <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted-foreground/20">
        <div className={cn("h-full rounded-full transition-all", tier.bar)} style={{ width: `${score}%` }} />
      </div>

      <div className="grid grid-cols-2 gap-1.5">
        {checks.map((c) => (
          <div key={c.label} className="flex items-center gap-1.5 text-[11px]">
            {c.ok ? (
              <CheckCircle2 className="size-3 text-emerald-600 dark:text-emerald-400" />
            ) : (
              <XCircle className="size-3 text-muted-foreground/60" />
            )}
            <span className={c.ok ? "" : "text-muted-foreground"}>{c.label}</span>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-2 gap-2 text-[10px]">
        <div className="rounded-lg bg-card px-2.5 py-1.5">
          <span className="text-muted-foreground">长度 </span>
          <span className="font-mono">{analysis?.details?.length ?? 0}</span>
          <span className="text-muted-foreground"> 字符 / {analysis?.details?.byteLength ?? 0} 字节</span>
        </div>
        <div className="rounded-lg bg-card px-2.5 py-1.5">
          <span className="text-muted-foreground">猜测熵 </span>
          <span className="font-mono">{(analysis?.details?.entropy ?? 0).toFixed(1)}</span>
          <span className="text-muted-foreground"> bit</span>
        </div>
        {/* 破解时长比分数更能让人理解「这个密码到底有多弱」 */}
        <div className="col-span-2 rounded-lg bg-card px-2.5 py-1.5">
          <span className="text-muted-foreground">离线破解（bcrypt 量级）约需 </span>
          <span className="font-mono">{analysis?.crackTime ?? "—"}</span>
        </div>
      </div>

      {patternDetails.length > 0 ? (
        <div className="space-y-1.5">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">命中弱模式</div>
          <div className="flex flex-wrap gap-1.5">
            {patternDetails.map((p, i) => (
              <Badge key={`${p.kind}-${p.start}-${i}`} variant="danger" className="text-[9px]">
                {p.label}
                <span className="ml-1 opacity-70 tabular-nums">
                  第 {p.start}-{p.end} 位
                </span>
              </Badge>
            ))}
          </div>
        </div>
      ) : (
        patterns.length > 0 && (
          <div className="space-y-1.5">
            <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">命中弱模式</div>
            <div className="flex flex-wrap gap-1.5">
              {patterns.map((p) => (
                <Badge key={p} variant="danger" className="text-[9px]">{p}</Badge>
              ))}
            </div>
          </div>
        )
      )}

      {violations.length > 0 && (
        <div className="space-y-1.5">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">违反项</div>
          <ul className="space-y-1">
            {violations.map((v) => (
              <li key={v} className="flex gap-1.5 text-[11px] text-destructive">
                <XCircle className="mt-0.5 size-3 shrink-0" />
                <span>{v}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {summary?.recommendations?.length ? (
        <div className="space-y-1.5">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">改进建议</div>
          <ul className="space-y-1">
            {summary.recommendations.map((r, i) => (
              <li key={`${r.type ?? "rec"}-${i}`} className="flex items-start gap-1.5 text-[11px] text-muted-foreground">
                <Badge
                  variant={r.priority === "high" ? "warning" : "outline"}
                  className="mt-0.5 shrink-0 text-[9px]"
                >
                  {r.priority === "high" ? "高" : "中"}
                </Badge>
                <span>{r.message}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
