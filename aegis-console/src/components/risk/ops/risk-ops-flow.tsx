"use client";

import { useMemo, useState, type ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import {
  AlertTriangle, ArrowLeft, ArrowRight, Ban, CheckCircle2, ClipboardCheck, Flame, ListX, Loader2, Network, RotateCcw, ShieldCheck,
  ShieldOff, Smartphone, UserX, XCircle, type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";

import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { listEntrySentence, remaining } from "@/components/risk/kit/humanize";
import { Segmented } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { ListEntry, RiskCase } from "@/lib/api/risk";
import {
  purgeConfirmText, type OpsEntity, type OpsInspection, type OpsPurgeResult, type OpsRelated, type OpsReleaseResult, type OpsSubject,
  type OpsUserResult,
} from "@/lib/api/risk-ops";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useCanOperateRisk, usePurgeAccounts, useReleaseAccounts, useRiskInspection } from "@/lib/risk-ops-hooks";
import { cn } from "@/lib/utils";

import { AccountAvatar, AccountState, MeasureRow, ReasonChip, StatTile, Stepper, accountMeta, accountName } from "./ops-kit";

/**
 * 账号处置流程：一键解除风控 / 删除风险账号（连同关联账号）。
 *
 * 三步：选择账号（起点账号 + 关系图找到的关联账号，按关联强弱与理由列出）→ 核对（解除：逐项勾选要撤销的封号、
 * 防火墙封禁、名单与复核单；删除：核对名单、选择是否拉黑设备、输入确认文字）→ 完成（逐个账号的结果）。
 * 后端：主服务 /api/admin/system/risk-ops/*（internal/service/risk_ops.go）。
 */

export type RiskOpsMode = "release" | "purge";

const MODE: Record<RiskOpsMode, { title: string; icon: LucideIcon; tone: "emerald" | "rose"; steps: string[]; lead: string }> = {
  release: {
    title: "解除风控",
    icon: ShieldCheck,
    tone: "emerald",
    steps: ["选择账号", "核对处置", "完成"],
    lead: "撤销风控对这些账号的封号、防火墙封禁与名单，待复核单一并结案。",
  },
  purge: {
    title: "删除风险账号",
    icon: UserX,
    tone: "rose",
    steps: ["选择账号", "确认删除", "完成"],
    lead: "彻底删除这些账号，可同时拉黑它们用过的设备，防止换号再来。",
  },
};

const ALLOW_OPTIONS = [
  { value: "0", label: "不加" },
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
] as const;

const BLOCK_OPTIONS = [
  { value: "30", label: "30 天" },
  { value: "180", label: "180 天" },
  { value: "0", label: "永久" },
] as const;

export function RiskOpsFlow({
  open, onOpenChange, mode, appId, userIds, onFinished,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mode: RiskOpsMode;
  appId?: number;
  userIds: number[];
  /** 执行完成（含部分失败）后回调；删除流程里可用来离开已删除账号的详情页 */
  onFinished?: (result: { mode: RiskOpsMode; done: number[] }) => void;
}) {
  // 关闭即卸载内容：每次打开都从第一步开始
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(860px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        {open ? (
          <FlowBody
            mode={mode}
            appId={appId}
            seed={userIds}
            onClose={() => onOpenChange(false)}
            onFinished={onFinished}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function FlowBody({
  mode, appId, seed, onClose, onFinished,
}: {
  mode: RiskOpsMode;
  appId?: number;
  seed: number[];
  onClose: () => void;
  onFinished?: (result: { mode: RiskOpsMode; done: number[] }) => void;
}) {
  const meta = MODE[mode];
  const canOperate = useCanOperateRisk();
  const [step, setStep] = useState(0);
  const [picked, setPicked] = useState<Set<number> | null>(null);
  const scope = useRiskInspection(appId, seed, true);
  const related = useMemo(() => scope.data?.related ?? [], [scope.data]);

  // 默认选中：强关联且正被封禁（或被名单拦截）的账号
  const defaults = useMemo(() => {
    const s = new Set<number>();
    for (const r of related) if (r.strength === "strong" && (r.user?.ban || r.banned)) s.add(r.userId);
    return s;
  }, [related]);
  const chosen = picked ?? defaults;
  const targets = useMemo(() => {
    const ids = [...seed];
    for (const r of related) if (chosen.has(r.userId) && !ids.includes(r.userId)) ids.push(r.userId);
    return ids;
  }, [seed, related, chosen]);

  const [result, setResult] = useState<OpsReleaseResult | OpsPurgeResult | null>(null);

  return (
    <>
      <DialogHeader className="gap-3 border-b px-6 pt-5 pb-4 text-left">
        <div className="flex items-start gap-3 pr-8">
          <TonalIcon icon={meta.icon} tone={meta.tone} size="lg" />
          <div className="min-w-0">
            <DialogTitle className="text-base">{meta.title}</DialogTitle>
            <DialogDescription className="mt-0.5 text-xs leading-5">{meta.lead}</DialogDescription>
          </div>
        </div>
        <Stepper steps={meta.steps} current={step} />
      </DialogHeader>

      {!appId ? (
        <div className="p-6 text-sm text-muted-foreground">读不到账号所属的应用。</div>
      ) : step === 0 ? (
        <ScopeStep
          mode={mode}
          inspection={scope.data}
          loading={scope.isLoading}
          error={scope.error as Error | null}
          onRetry={() => scope.refetch()}
          related={related}
          chosen={chosen}
          setChosen={(next) => setPicked(next)}
          targets={targets}
          onCancel={onClose}
          onNext={() => setStep(1)}
        />
      ) : step === 1 ? (
        mode === "release" ? (
          <ReleaseStep
            appId={appId}
            targets={targets}
            canOperate={canOperate}
            onBack={() => setStep(0)}
            onDone={(r) => {
              setResult(r);
              setStep(2);
              onFinished?.({ mode, done: r.users.filter((u) => u.ok).map((u) => u.userId) });
            }}
          />
        ) : (
          <PurgeStep
            appId={appId}
            targets={targets}
            canOperate={canOperate}
            onBack={() => setStep(0)}
            onDone={(r) => {
              setResult(r);
              setStep(2);
              onFinished?.({ mode, done: r.users.filter((u) => u.ok).map((u) => u.userId) });
            }}
          />
        )
      ) : result ? (
        <ResultStep mode={mode} result={result} onClose={onClose} />
      ) : null}
    </>
  );
}

// ── 公共：滚动区与底栏 ──

function Body({ children }: { children: ReactNode }) {
  return <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-5">{children}</div>;
}

function Footer({ children, hint }: { children: ReactNode; hint?: ReactNode }) {
  return (
    <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-6 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="text-xs text-muted-foreground">{hint}</div>
      <div className="flex items-center justify-end gap-2">{children}</div>
    </div>
  );
}

function Section({ title, hint, action, children }: { title: string; hint?: ReactNode; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="space-y-2.5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium">{title}</h3>
          {hint ? <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p> : null}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function LoadingRows({ n = 3 }: { n?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: n }, (_, i) => (
        <div key={i} className="flex items-center gap-3 rounded-xl border p-3">
          <Skeleton className="size-9 rounded-full" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-64" />
          </div>
        </div>
      ))}
    </div>
  );
}

function LoadError({ error, onRetry }: { error: Error | null; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed py-10 text-center">
      <AlertTriangle className="size-6 text-amber-500" />
      <div>
        <p className="text-sm font-medium">读不到这些账号的风控情况</p>
        {error?.message ? <p className="mt-1 max-w-md text-xs text-muted-foreground">{error.message}</p> : null}
      </div>
      <Button size="sm" variant="outline" onClick={onRetry}>
        <RotateCcw className="size-3.5" />
        重试
      </Button>
    </div>
  );
}

function SoftErrors({ errors }: { errors?: Record<string, string> }) {
  const list = Object.entries(errors ?? {});
  if (!list.length) return null;
  return (
    <div className="flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-800/60 dark:bg-amber-950/40 dark:text-amber-200">
      <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
      <div className="space-y-0.5">
        <p>部分数据没有读到，清单可能不完整：</p>
        {list.slice(0, 3).map(([k, v]) => (
          <p key={k} className="text-amber-700/80 dark:text-amber-300/80">{v}</p>
        ))}
      </div>
    </div>
  );
}

// ── 第一步：选择账号 ──

function SubjectCard({ subject }: { subject: OpsSubject }) {
  const r = subject.recent;
  const blocked = r && (r.blocked > 0 || r.reviewed > 0);
  return (
    <div className="flex items-start gap-3 rounded-xl border bg-card p-3">
      <AccountAvatar user={subject.user} id={subject.userId} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{accountName(subject.user, subject.userId)}</span>
          <AccountState user={subject.user} graphBanned={subject.graphBanned} />
        </div>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">{accountMeta(subject.user, subject.userId)}</p>
        {blocked ? (
          <p className="mt-1.5 text-xs leading-5 text-foreground/80">
            近 30 天被拦截 {r.blocked} 次{r.reviewed ? `、转人工 ${r.reviewed} 次` : ""}
            {r.lastAt ? `，最近一次在${formatRelative(r.lastAt)}` : ""}
            {r.lastReason ? `：${r.lastReason.split("；")[0]}` : ""}
          </p>
        ) : null}
      </div>
    </div>
  );
}

function RelatedRow({ item, checked, onToggle }: { item: OpsRelated; checked: boolean; onToggle: (v: boolean) => void }) {
  return (
    <label className={cn("flex cursor-pointer items-start gap-3 rounded-xl border p-3 transition-colors hover:bg-muted/40", checked && "border-primary/40 bg-primary/[0.03]")}>
      <Checkbox checked={checked} onCheckedChange={(v) => onToggle(v === true)} className="mt-2.5" />
      <AccountAvatar user={item.user} id={item.userId} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{accountName(item.user, item.userId)}</span>
          <AccountState user={item.user} graphBanned={item.banned} />
          {item.strength === "weak" ? <Badge variant="outline" size="sm">弱关联</Badge> : null}
        </div>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">{accountMeta(item.user, item.userId)}</p>
        <div className="mt-1.5 flex flex-wrap gap-1">
          {item.reasons.slice(0, 4).map((r, i) => (
            <ReasonChip key={`${r.kind}-${r.vid ?? i}`} reason={r} />
          ))}
          {item.reasons.length > 4 ? <span className="self-center text-[11px] text-muted-foreground">另有 {item.reasons.length - 4} 处关联</span> : null}
        </div>
      </div>
    </label>
  );
}

function ScopeStep({
  mode, inspection, loading, error, onRetry, related, chosen, setChosen, targets, onCancel, onNext,
}: {
  mode: RiskOpsMode;
  inspection?: OpsInspection;
  loading: boolean;
  error: Error | null;
  onRetry: () => void;
  related: OpsRelated[];
  chosen: Set<number>;
  setChosen: (next: Set<number>) => void;
  targets: number[];
  onCancel: () => void;
  onNext: () => void;
}) {
  const [showWeak, setShowWeak] = useState(false);
  const strong = related.filter((r) => r.strength === "strong");
  const weak = related.filter((r) => r.strength === "weak");
  const visible = showWeak ? related : strong;
  const subjects = inspection?.subjects ?? [];
  const toggle = (id: number, v: boolean) => {
    const next = new Set(chosen);
    if (v) next.add(id);
    else next.delete(id);
    setChosen(next);
  };
  const pick = (pred: (r: OpsRelated) => boolean) => setChosen(new Set(related.filter(pred).map((r) => r.userId)));
  const extra = targets.length - subjects.length;

  return (
    <>
      <Body>
        {error ? <LoadError error={error} onRetry={onRetry} /> : null}
        <Section title={subjects.length > 1 ? `选中的 ${subjects.length} 个账号` : "这个账号"}>
          {loading ? <LoadingRows n={1} /> : <div className="grid gap-2">{subjects.map((s) => <SubjectCard key={s.userId} subject={s} />)}</div>}
        </Section>

        <Section
          title="关联账号"
          hint={
            loading
              ? "正在从关系图里找共用设备、手机号、邮箱、第三方账号或付款账号的账号"
              : related.length
                ? `关系图找到 ${strong.length} 个强关联${weak.length ? `、${weak.length} 个只共用网络线路的弱关联` : ""}账号，勾选后会${mode === "release" ? "一起解除" : "一起删除"}`
                : "关系图里没有找到与它共用设备、手机号、邮箱、第三方账号或付款账号的账号"
          }
          action={
            related.length ? (
              <div className="flex flex-wrap items-center gap-1.5">
                <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => pick((r) => r.strength === "strong" && !!(r.user?.ban || r.banned))}>
                  选中封禁中的
                </Button>
                <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => pick((r) => r.strength === "strong")}>
                  选中全部强关联
                </Button>
                <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setChosen(new Set())}>
                  清空
                </Button>
              </div>
            ) : null
          }
        >
          {loading ? (
            <LoadingRows n={2} />
          ) : related.length ? (
            <div className="space-y-2">
              {visible.map((r) => (
                <RelatedRow key={r.userId} item={r} checked={chosen.has(r.userId)} onToggle={(v) => toggle(r.userId, v)} />
              ))}
              {!showWeak && weak.length ? (
                <button
                  type="button"
                  onClick={() => setShowWeak(true)}
                  className="w-full rounded-xl border border-dashed py-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
                >
                  还有 {weak.length} 个只是共用过网络线路的账号（家人、同事也可能这样），点击查看
                </button>
              ) : null}
            </div>
          ) : (
            <div className="flex items-center gap-3 rounded-xl border border-dashed px-4 py-5 text-sm text-muted-foreground">
              <Network className="size-4 shrink-0" />
              没有关联账号，只处置上面的账号。
            </div>
          )}
        </Section>
        <SoftErrors errors={inspection?.errors} />
      </Body>
      <Footer hint={extra > 0 ? `共 ${targets.length} 个账号，含 ${extra} 个关联账号` : `共 ${targets.length} 个账号`}>
        <Button variant="ghost" onClick={onCancel}>取消</Button>
        <Button onClick={onNext} disabled={loading || !!error || !subjects.length}>
          下一步
          <ArrowRight className="size-4" />
        </Button>
      </Footer>
    </>
  );
}

// ── 第二步（解除）：核对要撤销的处置 ──

const SOURCE_WORD: Record<string, string> = { rule: "风控规则", cep: "行为模式", review: "人工复核", manual: "人工添加", import: "导入" };

function entryTitle(e: ListEntry, entity?: OpsEntity) {
  const sentence = listEntrySentence(e);
  if (!entity) return sentence;
  const label = entity.kind === "device" ? entity.label || "这台设备" : entity.kind === "ip" ? `IP ${entity.value}` : entity.label;
  return sentence.replace(/^这(台设备|个 IP|个登录名)/, label);
}

function entryDetail(e: ListEntry, entity?: OpsEntity) {
  const parts = [remaining(e.exp).text];
  if (e.source) parts.push(`来源 ${SOURCE_WORD[e.source] ?? e.source}`);
  if (e.reason) parts.push(e.reason);
  if (entity?.kind === "ip" && entity.users && entity.users > 1) parts.push(`这个 IP 上有 ${entity.users} 个账号`);
  return parts.join("，");
}

interface Plan {
  bans: Array<{ key: string; subject: OpsSubject; def: boolean }>;
  firewall: Array<{ key: string; id: number; subject: OpsSubject; ip: string; reason: string; source: string; exp?: string | null; def: boolean }>;
  entries: Array<{ key: string; entry: ListEntry; subject: OpsSubject; entity?: OpsEntity; def: boolean }>;
  cases: Array<{ subject: OpsSubject; cases: RiskCase[] }>;
}

function buildPlan(subjects: OpsSubject[]): Plan {
  const plan: Plan = { bans: [], firewall: [], entries: [], cases: [] };
  const seen = new Set<string>();
  for (const s of subjects) {
    if (s.user?.ban) plan.bans.push({ key: `ban:${s.user.ban.id}`, subject: s, def: s.user.banFromRisk });
    const addEntry = (e: ListEntry, entity?: OpsEntity) => {
      if (e.kind === "allow" || seen.has(`list:${e.id}`)) return;
      seen.add(`list:${e.id}`);
      plan.entries.push({ key: `list:${e.id}`, entry: e, subject: s, entity, def: true });
    };
    for (const e of s.lists) addEntry(e);
    for (const ent of s.entities) {
      for (const e of ent.lists) addEntry(e, ent);
      for (const b of ent.firewall ?? []) {
        if (seen.has(`fw:${b.id}`)) continue;
        seen.add(`fw:${b.id}`);
        plan.firewall.push({ key: `fw:${b.id}`, id: b.id, subject: s, ip: b.ip, reason: b.reason, source: b.source, exp: b.expiresAt, def: b.source === "risk" });
      }
    }
    if (s.cases.length) plan.cases.push({ subject: s, cases: s.cases });
  }
  return plan;
}

function ReleaseStep({
  appId, targets, canOperate, onBack, onDone,
}: {
  appId: number;
  targets: number[];
  canOperate: boolean;
  onBack: () => void;
  onDone: (r: OpsReleaseResult) => void;
}) {
  const q = useRiskInspection(appId, targets, false);
  const plan = useMemo(() => buildPlan(q.data?.subjects ?? []), [q.data]);
  const [overrides, setOverrides] = useState<Record<string, boolean>>({});
  const [closeCases, setCloseCases] = useState(true);
  const [allowDays, setAllowDays] = useState<"0" | "7" | "30">("7");
  const [note, setNote] = useState("");
  const release = useReleaseAccounts();
  const on = (key: string, def: boolean) => overrides[key] ?? def;
  const set = (key: string, v: boolean) => setOverrides((o) => ({ ...o, [key]: v }));

  const picked = {
    bans: plan.bans.filter((b) => on(b.key, b.def)),
    firewall: plan.firewall.filter((f) => on(f.key, f.def)),
    entries: plan.entries.filter((e) => on(e.key, e.def)),
  };
  const caseCount = plan.cases.reduce((n, c) => n + c.cases.length, 0);
  const nothing = !plan.bans.length && !plan.firewall.length && !plan.entries.length && !caseCount;
  const summary = [
    picked.bans.length ? `解封 ${picked.bans.length} 个账号` : "",
    picked.firewall.length ? `撤销 ${picked.firewall.length} 条防火墙封禁` : "",
    picked.entries.length ? `移出 ${picked.entries.length} 条名单` : "",
    closeCases && caseCount ? `结案 ${caseCount} 张复核单` : "",
    allowDays !== "0" ? `加 ${allowDays} 天白名单保护` : "",
  ].filter(Boolean);

  const submit = async () => {
    try {
      const r = await release.mutateAsync({
        appId,
        userIds: targets,
        banIds: picked.bans.map((b) => b.subject.user!.ban!.id),
        ipBanIds: picked.firewall.map((f) => f.id),
        entryIds: picked.entries.map((e) => e.entry.id),
        closeCases,
        allowDays: Number(allowDays),
        note: note.trim(),
      });
      toast.success("已解除风控");
      onDone(r);
    } catch (err) {
      toast.error((err as Error).message || "解除失败");
    }
  };

  const bySubject = (s: OpsSubject) => ({
    bans: plan.bans.filter((b) => b.subject.userId === s.userId),
    firewall: plan.firewall.filter((f) => f.subject.userId === s.userId),
    entries: plan.entries.filter((e) => e.subject.userId === s.userId),
    cases: plan.cases.find((c) => c.subject.userId === s.userId)?.cases ?? [],
  });

  return (
    <>
      <Body>
        {q.error ? <LoadError error={q.error as Error} onRetry={() => q.refetch()} /> : null}
        {q.isLoading ? (
          <LoadingRows n={Math.min(targets.length, 3)} />
        ) : (
          <Section
            title="要撤销的处置"
            hint={nothing ? undefined : "默认勾选风控下发的处置；管理员手动封禁的默认不动，需要时可以勾上"}
          >
            {nothing ? (
              <div className="flex items-center gap-3 rounded-xl border border-dashed px-4 py-5 text-sm text-muted-foreground">
                <ShieldCheck className="size-4 shrink-0 text-emerald-500" />
                这些账号身上没有风控处置。仍可以加一段白名单保护，避免之后被误拦。
              </div>
            ) : (
              <div className="space-y-3">
                {(q.data?.subjects ?? []).map((s) => {
                  const m = bySubject(s);
                  const empty = !m.bans.length && !m.firewall.length && !m.entries.length && !m.cases.length;
                  return (
                    <div key={s.userId} className="overflow-hidden rounded-xl border">
                      <div className="flex items-center gap-3 border-b bg-muted/30 px-3 py-2.5">
                        <AccountAvatar user={s.user} id={s.userId} size="sm" />
                        <span className="min-w-0 flex-1 truncate text-sm font-medium">{accountName(s.user, s.userId)}</span>
                        <AccountState user={s.user} graphBanned={s.graphBanned} />
                      </div>
                      {empty ? (
                        <p className="px-4 py-3 text-xs text-muted-foreground">没有风控处置</p>
                      ) : (
                        <div className="divide-y divide-border/60 p-1">
                          {m.bans.map((b) => {
                            const ban = s.user!.ban!;
                            return (
                              <MeasureRow
                                key={b.key}
                                checked={on(b.key, b.def)}
                                onChange={(v) => set(b.key, v)}
                                icon={Ban}
                                tone="rose"
                                title={
                                  b.def
                                    ? `风控平台在${formatDateTime(ban.startAt)}封禁了这个账号`
                                    : `${ban.bannedByAdminName || "管理员"}在${formatDateTime(ban.startAt)}手动封禁了这个账号`
                                }
                                detail={[ban.endAt ? `到 ${formatDateTime(ban.endAt)} 自动解封` : "永久封禁", ban.reason].filter(Boolean).join("，")}
                                aside={b.def ? undefined : <Badge variant="warning" size="sm">人工封禁</Badge>}
                              />
                            );
                          })}
                          {m.firewall.map((f) => (
                            <MeasureRow
                              key={f.key}
                              checked={on(f.key, f.def)}
                              onChange={(v) => set(f.key, v)}
                              icon={Flame}
                              tone="amber"
                              title={`IP ${f.ip} 被防火墙拦截`}
                              detail={[remaining(f.exp).text, f.source === "risk" ? "风控下发" : "防火墙规则", f.reason].filter(Boolean).join("，")}
                              aside={f.source === "risk" ? undefined : <Badge variant="outline" size="sm">非风控</Badge>}
                            />
                          ))}
                          {m.entries.map((e) => (
                            <MeasureRow
                              key={e.key}
                              checked={on(e.key, e.def)}
                              onChange={(v) => set(e.key, v)}
                              icon={e.entity?.kind === "device" ? Smartphone : ListX}
                              tone={e.entry.kind === "block" ? "rose" : "sky"}
                              title={entryTitle(e.entry, e.entity)}
                              detail={entryDetail(e.entry, e.entity)}
                            />
                          ))}
                          {m.cases.length ? (
                            <div className="flex items-start gap-3 px-3 py-2.5">
                              <span className="w-4" />
                              <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-sky-500/12 text-sky-600 dark:text-sky-400">
                                <ClipboardCheck className="size-3.5" />
                              </span>
                              <span className="min-w-0 flex-1 text-sm leading-5">
                                有 {m.cases.length} 张待复核单
                                <span className="mt-0.5 block text-xs text-muted-foreground">
                                  {closeCases ? "将按「判定为正常」结案" : "保持待复核"}
                                </span>
                              </span>
                            </div>
                          ) : null}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </Section>
        )}

        <Section title="解除之后">
          <div className="divide-y rounded-xl border">
            <div className="flex items-center justify-between gap-4 px-4 py-3">
              <div>
                <p className="text-sm">待复核单按「判定为正常」结案</p>
                <p className="mt-0.5 text-xs text-muted-foreground">结论会写进复核记录，供规则调优参考</p>
              </div>
              <Switch checked={closeCases} onCheckedChange={setCloseCases} />
            </div>
            <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm">白名单保护</p>
                <p className="mt-0.5 text-xs text-muted-foreground">保护期内风控规则不再拦截这些账号，避免刚解除又被拦</p>
              </div>
              <Segmented value={allowDays} options={ALLOW_OPTIONS} onChange={setAllowDays} ariaLabel="白名单保护期" />
            </div>
            <div className="space-y-2 px-4 py-3">
              <Label htmlFor="ops-note" className="text-sm font-normal">说明</Label>
              <Textarea
                id="ops-note"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                maxLength={200}
                rows={2}
                placeholder="写给审计看的原因，例如：用户申诉，核实为误判"
              />
            </div>
          </div>
        </Section>
      </Body>
      <Footer hint={summary.length ? `将${summary.join("、")}` : "没有勾选任何处置"}>
        <Button variant="ghost" onClick={onBack} disabled={release.isPending}>
          <ArrowLeft className="size-4" />
          上一步
        </Button>
        <Button
          onClick={submit}
          disabled={!canOperate || release.isPending || q.isLoading || !!q.error || !summary.length}
          title={canOperate ? undefined : "仅超级管理员可解除风控"}
          className="bg-emerald-600 text-white hover:bg-emerald-600/90"
        >
          {release.isPending ? <Loader2 className="size-4 animate-spin" /> : <ShieldOff className="size-4" />}
          解除风控
        </Button>
      </Footer>
    </>
  );
}

// ── 第二步（删除）：确认 ──

function PurgeStep({
  appId, targets, canOperate, onBack, onDone,
}: {
  appId: number;
  targets: number[];
  canOperate: boolean;
  onBack: () => void;
  onDone: (r: OpsPurgeResult) => void;
}) {
  const q = useRiskInspection(appId, targets, false);
  const subjects = useMemo(() => q.data?.subjects ?? [], [q.data]);
  const live = subjects.filter((s) => s.user);
  const devices = useMemo(() => {
    const set = new Set<string>();
    for (const s of subjects) for (const e of s.entities) if (e.kind === "device" && e.value) set.add(e.value);
    return set.size;
  }, [subjects]);
  const [blockDevices, setBlockDevices] = useState(true);
  const [blockDays, setBlockDays] = useState<"30" | "180" | "0">("180");
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState("");
  const purge = usePurgeAccounts();
  const phrase = purgeConfirmText(targets.length);
  const ready = confirm.trim() === phrase;

  const submit = async () => {
    try {
      const r = await purge.mutateAsync({
        appId, userIds: targets, blockDevices: blockDevices && devices > 0, blockDays: Number(blockDays), note: note.trim(), confirm: confirm.trim(),
      });
      if (r.deleted) toast.success(`已删除 ${r.deleted} 个账号`);
      else toast.error("没有账号被删除");
      onDone(r);
    } catch (err) {
      toast.error((err as Error).message || "删除失败");
    }
  };

  return (
    <>
      <Body>
        {q.error ? <LoadError error={q.error as Error} onRetry={() => q.refetch()} /> : null}
        <div className="flex items-start gap-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-rose-800 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-200">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <div className="space-y-1 text-xs leading-5">
            <p className="text-sm font-medium">删除后无法恢复</p>
            <p>账号、资料、余额、积分、会员与登录会话一并清除，工单与订单留档但不再关联账号。风控关系图会保留这些账号的痕迹，换号再来仍能被关联到。</p>
          </div>
        </div>

        <Section title={`将删除 ${targets.length} 个账号`}>
          {q.isLoading ? (
            <LoadingRows n={Math.min(targets.length, 3)} />
          ) : (
            <div className="max-h-72 divide-y overflow-y-auto rounded-xl border">
              {subjects.map((s) => {
                const devs = s.entities.filter((e) => e.kind === "device").length;
                return (
                  <div key={s.userId} className="flex items-center gap-3 px-3 py-2.5">
                    <AccountAvatar user={s.user} id={s.userId} size="sm" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm">{accountName(s.user, s.userId)}</span>
                        <AccountState user={s.user} graphBanned={s.graphBanned} />
                      </div>
                      <p className="truncate text-xs text-muted-foreground">
                        {accountMeta(s.user, s.userId)}
                        {devs ? `，用过 ${devs} 台设备` : ""}
                      </p>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Section>

        <Section title="同时">
          <div className="divide-y rounded-xl border">
            <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-3">
                <Switch checked={blockDevices} onCheckedChange={setBlockDevices} disabled={!devices} className="mt-0.5" />
                <div>
                  <p className="text-sm">拉黑这些账号用过的设备</p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {devices ? `共 ${devices} 台设备，拉黑后这些设备上新注册、登录都会被风控拦截` : "没有记录到这些账号用过的设备"}
                  </p>
                </div>
              </div>
              {blockDevices && devices ? <Segmented value={blockDays} options={BLOCK_OPTIONS} onChange={setBlockDays} ariaLabel="拉黑时长" /> : null}
            </div>
            <div className="space-y-2 px-4 py-3">
              <Label htmlFor="ops-purge-note" className="text-sm font-normal">说明</Label>
              <Textarea
                id="ops-purge-note"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                maxLength={200}
                rows={2}
                placeholder="写给审计看的原因，例如：同一设备批量注册的刷量账号"
              />
            </div>
          </div>
        </Section>

        <Section title="确认">
          <div className="space-y-2 rounded-xl border px-4 py-3">
            <Label htmlFor="ops-confirm" className="text-sm font-normal">
              请输入 <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[13px]">{phrase}</span> 以确认
            </Label>
            <Input id="ops-confirm" value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder={phrase} autoComplete="off" />
          </div>
        </Section>
      </Body>
      <Footer hint={live.length < targets.length ? `${targets.length - live.length} 个账号已不存在，会被跳过` : undefined}>
        <Button variant="ghost" onClick={onBack} disabled={purge.isPending}>
          <ArrowLeft className="size-4" />
          上一步
        </Button>
        <Button
          variant="destructive"
          onClick={submit}
          disabled={!canOperate || !ready || purge.isPending || q.isLoading}
          title={canOperate ? undefined : "仅超级管理员可删除账号"}
        >
          {purge.isPending ? <Loader2 className="size-4 animate-spin" /> : <UserX className="size-4" />}
          删除 {targets.length} 个账号
        </Button>
      </Footer>
    </>
  );
}

// ── 第三步：结果 ──

function ResultStep({ mode, result, onClose }: { mode: RiskOpsMode; result: OpsReleaseResult | OpsPurgeResult; onClose: () => void }) {
  const users: OpsUserResult[] = result.users;
  const failed = users.filter((u) => !u.ok);
  const tiles =
    mode === "release"
      ? (() => {
          const r = result as OpsReleaseResult;
          return [
            { value: r.unbanned, label: "个账号已解封", tone: "emerald" as const },
            { value: r.listsRemoved, label: "条名单已移出" },
            { value: r.ipUnbanned, label: "条防火墙封禁已撤销" },
            { value: r.casesClosed, label: "张复核单已结案" },
          ];
        })()
      : (() => {
          const r = result as OpsPurgeResult;
          return [
            { value: r.deleted, label: "个账号已删除", tone: "rose" as const },
            { value: r.devicesBlocked, label: "台设备已拉黑" },
            { value: r.casesClosed, label: "张复核单已结案" },
          ];
        })();
  const allOk = !failed.length && !result.problems.length;
  return (
    <>
      <Body>
        <div className="flex flex-col items-center gap-2 py-2 text-center">
          {allOk ? <CheckCircle2 className="size-10 text-emerald-500" /> : <AlertTriangle className="size-10 text-amber-500" />}
          <p className="text-base font-medium">{allOk ? (mode === "release" ? "风控已解除" : "账号已删除") : "已完成，部分没有成功"}</p>
          {mode === "release" && (result as OpsReleaseResult).allowed ? (
            <p className="text-xs text-muted-foreground">已加白名单保护，保护期内风控规则不再拦截这些账号</p>
          ) : null}
        </div>
        <div className={cn("grid gap-2", tiles.length === 4 ? "grid-cols-2 sm:grid-cols-4" : "grid-cols-3")}>
          {tiles.map((t) => (
            <StatTile key={t.label} value={<NumberFlow value={t.value} />} label={t.label} tone={"tone" in t ? t.tone : "default"} />
          ))}
        </div>
        {result.problems.length ? (
          <div className="space-y-1 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs leading-5 text-amber-800 dark:border-amber-800/60 dark:bg-amber-950/40 dark:text-amber-200">
            {result.problems.map((p) => (
              <p key={p}>{p}</p>
            ))}
          </div>
        ) : null}
        <Section title="逐个账号">
          <div className="max-h-64 divide-y overflow-y-auto rounded-xl border">
            {users.map((u) => (
              <div key={u.userId} className="flex items-center gap-3 px-3 py-2">
                {u.ok ? <CheckCircle2 className="size-4 shrink-0 text-emerald-500" /> : <XCircle className="size-4 shrink-0 text-rose-500" />}
                <span className="min-w-0 flex-1 truncate text-sm">{u.name}</span>
                <span className={cn("shrink-0 text-xs", u.ok ? "text-muted-foreground" : "text-rose-600 dark:text-rose-400")}>
                  {u.ok ? u.done || (mode === "release" ? "已处理" : "已删除") : u.error}
                </span>
              </div>
            ))}
          </div>
        </Section>
      </Body>
      <Footer>
        <Button onClick={onClose}>完成</Button>
      </Footer>
    </>
  );
}
