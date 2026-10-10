"use client";

import { useEffect, useState } from "react";
import { BarChart3, CalendarCheck2, Calculator, ListChecks, Loader2, RotateCcw, Save, Settings2, TriangleAlert, X } from "lucide-react";
import { toast } from "sonner";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { SignInOverview } from "@/components/apps/signin-reward/signin-overview";
import { SignInPolicyEditor } from "@/components/apps/signin-reward/signin-policy-editor";
import { SignInRecords } from "@/components/apps/signin-reward/signin-records";
import { clonePolicy, isPolicyDirty, localPolicyProblems } from "@/components/apps/signin-reward/signin-shared";
import { SignInSimulator } from "@/components/apps/signin-reward/signin-simulator";
import { ConfirmActionDialog } from "@/components/users/online/session-shared";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import {
  useAdminAppSignInRewardQuery,
  useResetAdminAppSignInRewardMutation,
  useUpdateAdminAppSignInRewardMutation
} from "@/lib/admin-hooks";
import type { SignInRewardPolicy } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * 签到奖励区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 今天多少人签、连签结构、发了多少积分经验、谁连签最久 |
 * | 记录 | 某个用户某一天签到拿了什么、命中了哪条规则 |
 * | 策略 | 基础奖励、连签成长、加成规则、里程碑怎么配 |
 * | 试算 | 按草稿逐日推演，保存之前先看清每天会发多少 |
 *
 * 草稿放在这一层而不是策略视图里：切到「试算」时算的正是正在编辑、尚未保存的那一份，
 * 切回来也不会丢。有未保存修改时底部常驻保存条，离开页面前浏览器会提示。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "records", label: "签到记录", icon: ListChecks },
  { key: "policy", label: "奖励策略", icon: Settings2 },
  { key: "simulator", label: "试算", icon: Calculator }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppSignInRewardPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  const rewardQuery = useAdminAppSignInRewardQuery(appKey);
  const saveMutation = useUpdateAdminAppSignInRewardMutation(appKey);
  const resetMutation = useResetAdminAppSignInRewardMutation(appKey);

  const saved = rewardQuery.data?.policy;
  const [baseline, setBaseline] = useState<SignInRewardPolicy | undefined>(undefined);
  const [draft, setDraft] = useState<SignInRewardPolicy | null>(null);
  const [saveErrors, setSaveErrors] = useState<string[] | null>(null);
  const [resetOpen, setResetOpen] = useState(false);

  // 服务端策略变了（首次加载、别处保存后重新拉取）：草稿没改过就跟上，改过就保留编辑中的内容
  if (saved && saved !== baseline) {
    if (!draft || !isPolicyDirty(baseline, draft)) setDraft(clonePolicy(saved));
    setBaseline(saved);
  }

  const dirty = isPolicyDirty(saved, draft);

  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  if (!appKey) return <NoAppSelected icon={<CalendarCheck2 className="size-5" />} />;

  if (rewardQuery.isLoading || !draft || !saved) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full max-w-md rounded-xl" />
        <Skeleton className="h-28 w-full rounded-2xl" />
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-24 rounded-2xl" />)}
        </div>
        <Skeleton className="h-72 w-full rounded-2xl" />
      </div>
    );
  }

  const update = (fn: (current: SignInRewardPolicy) => SignInRewardPolicy) => {
    setDraft((current) => (current ? fn(current) : current));
    setSaveErrors(null);
  };

  async function save() {
    if (!draft) return;
    const problems = localPolicyProblems(draft);
    if (problems.length) {
      setSaveErrors(problems);
      setView("policy");
      return;
    }
    try {
      const result = await saveMutation.mutateAsync({ policy: draft });
      setDraft(clonePolicy(result.policy));
      setSaveErrors(null);
      toast.success("签到奖励策略已保存，新签到立即按此计算");
    } catch (err) {
      const message = err instanceof ApiError ? err.message : "保存失败";
      setSaveErrors(message.split(/;\s*/).filter(Boolean));
      setView("policy");
    }
  }

  async function reset() {
    try {
      const result = await resetMutation.mutateAsync();
      setDraft(clonePolicy(result.policy));
      setSaveErrors(null);
      setResetOpen(false);
      toast.success("已恢复系统默认策略");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "恢复失败");
    }
  }

  return (
    <div className={cn("space-y-4", dirty && "pb-20")}>
      <div className="-mx-1 overflow-x-auto px-1 [scrollbar-width:none]">
        <div className="inline-flex min-w-max gap-1 rounded-xl border bg-muted/40 p-1">
          {VIEWS.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setView(item.key)}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium whitespace-nowrap transition-colors",
                view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
              {item.key === "policy" && dirty ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="有未保存的修改" /> : null}
            </button>
          ))}
        </div>
      </div>

      {saveErrors && view === "policy" ? (
        <div className="flex gap-2.5 rounded-xl border border-red-200 bg-red-50/60 p-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
          <TriangleAlert className="mt-0.5 size-4 shrink-0" />
          <div className="min-w-0 flex-1 space-y-1">
            <p className="font-medium">策略未保存，请先修正以下问题</p>
            <ul className="list-disc space-y-0.5 pl-4 text-xs break-all">{saveErrors.map((line) => <li key={line}>{line}</li>)}</ul>
          </div>
          <button type="button" aria-label="关闭" className="shrink-0 opacity-70 hover:opacity-100" onClick={() => setSaveErrors(null)}>
            <X className="size-4" />
          </button>
        </div>
      ) : null}

      {view === "overview" ? <SignInOverview appKey={appKey} policy={saved} onEditPolicy={() => setView("policy")} /> : null}
      {view === "records" ? <SignInRecords appKey={appKey} policy={saved} /> : null}
      {view === "policy" ? <SignInPolicyEditor draft={draft} update={update} onRequestReset={() => setResetOpen(true)} /> : null}
      {view === "simulator" ? <SignInSimulator appKey={appKey} draft={draft} saved={saved} dirty={dirty} /> : null}

      {dirty ? (
        <div className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/95 px-4 py-3 shadow-[0_-4px_16px_rgba(0,0,0,0.06)] md:sticky md:inset-x-auto md:bottom-4 md:rounded-2xl md:border md:shadow-lg">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-sm">
              <span className="size-2 rounded-full bg-amber-500" />
              <span className="font-medium">奖励策略有未保存的修改</span>
              {view !== "simulator" ? (
                <button type="button" className="hidden text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline sm:inline" onClick={() => setView("simulator")}>
                  先试算
                </button>
              ) : null}
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1.5"
                disabled={saveMutation.isPending}
                onClick={() => {
                  setDraft(clonePolicy(saved));
                  setSaveErrors(null);
                }}
              >
                <RotateCcw className="size-3.5" />
                放弃修改
              </Button>
              <Button size="sm" className="h-8 gap-1.5" disabled={saveMutation.isPending} onClick={() => void save()}>
                {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                保存并生效
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      <ConfirmActionDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title="恢复系统默认策略？"
        description="将删除本应用的自定义签到策略并立即改用系统默认策略，未保存的修改也会丢失。已发放的奖励不受影响。"
        confirmLabel="恢复默认"
        pending={resetMutation.isPending}
        onConfirm={reset}
      />
    </div>
  );
}
