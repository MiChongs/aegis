"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";

import { ErrorState, errorText } from "@/components/risk/decisions";
import { Field, FieldError, NumberField, Segmented, SettingRow } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import type { AppealAutoReview, BanAppealSettings } from "@/lib/api/ban-appeals";
import { useAIConfigsQuery } from "@/lib/ai-hooks";
import { useBanAppealSettings, useSaveBanAppealSettings } from "@/lib/ban-appeal-hooks";
import { cn } from "@/lib/utils";

import { pct } from "./appeal-kit";

const AUTO_REVIEW: ReadonlyArray<{ value: AppealAutoReview; label: string; detail: string }> = [
  { value: "off", label: "关闭", detail: "不跑模型，全部由人工复审。" },
  { value: "suggest", label: "只给建议", detail: "模型先审一遍并记下结论与建议回复，由人工拍板。" },
  {
    value: "auto",
    label: "自动处理",
    detail:
      "模型建议通过、置信度达到通过门槛、风控证据不是「证据确凿」且封禁不是管理员手动下的，自动通过；模型建议驳回、置信度达到驳回门槛且风控证据确凿，自动驳回；其余转人工。",
  },
];

/** 申诉设置（应用级）：是否开放、模型复审方式与门槛、次数与冷却、内容长度、邮件里的申诉页、AI 通道。 */
export function AppealSettingsDialog({ appKey, appName, open, onOpenChange }: { appKey: string; appName?: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const query = useBanAppealSettings(open ? appKey : undefined);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] gap-5 overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>申诉设置</DialogTitle>
          <DialogDescription>{appName ? `${appName}的封禁申诉规则` : "这个应用的封禁申诉规则"}，保存后立即生效。</DialogDescription>
        </DialogHeader>
        {query.isLoading ? (
          <div className="space-y-3">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        ) : query.isError || !query.data ? (
          <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : (
          <SettingsForm key={appKey} appKey={appKey} initial={query.data} onDone={() => onOpenChange(false)} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function problemsOf(s: BanAppealSettings) {
  const p: Partial<Record<keyof BanAppealSettings, string>> = {};
  if (s.minLength < 1) p.minLength = "至少 1 个字";
  if (s.maxLength <= s.minLength) p.maxLength = "要大于最少字数";
  if (s.maxLength > 10000) p.maxLength = "最多 10000 个字";
  if (s.maxAppealsPerBan < 1) p.maxAppealsPerBan = "至少 1 次";
  if (s.cooldownHours < 0) p.cooldownHours = "不能是负数";
  if (s.allowDays < 0) p.allowDays = "不能是负数";
  if (s.appealUrl && !/^https?:\/\/\S+$/i.test(s.appealUrl)) p.appealUrl = "请填写以 http:// 或 https:// 开头的地址";
  return p;
}

function SettingsForm({ appKey, initial, onDone }: { appKey: string; initial: BanAppealSettings; onDone: () => void }) {
  const [s, setS] = useState<BanAppealSettings>(initial);
  const set = (p: Partial<BanAppealSettings>) => setS((prev) => ({ ...prev, ...p }));
  const save = useSaveBanAppealSettings(appKey);
  const configs = useAIConfigsQuery({ kind: "app", appKey });
  const problems = problemsOf(s);
  const invalid = Object.keys(problems).length > 0;
  const auto = s.autoReview === "auto";
  const modelOn = s.autoReview !== "off";
  const mode = AUTO_REVIEW.find((m) => m.value === s.autoReview) ?? AUTO_REVIEW[1];
  const knownConfig = s.aiConfigId === 0 || (configs.data ?? []).some((c) => c.id === s.aiConfigId);

  async function submit() {
    try {
      await save.mutateAsync({ ...s, appealUrl: s.appealUrl.trim() });
      toast.success("申诉设置已保存");
      onDone();
    } catch (e) {
      toast.error("保存失败", { description: errorText(e) });
    }
  }

  return (
    <>
      <div className="flex flex-col divide-y">
        <SettingRow title="开放申诉" value={s.enabled ? "被封禁的用户可以提交复审" : "用户看不到申诉入口"}>
          <Switch checked={s.enabled} onCheckedChange={(v) => set({ enabled: v })} aria-label="开放申诉" />
        </SettingRow>

        <div className={cn("flex flex-col gap-5 py-4", !s.enabled && "opacity-60")}>
          <div className="flex flex-col gap-2">
            <span className="text-sm">模型复审</span>
            <Segmented value={s.autoReview} options={AUTO_REVIEW} onChange={(v) => set({ autoReview: v })} ariaLabel="模型复审" />
            <p className="text-xs leading-5 text-muted-foreground">{mode.detail}</p>
          </div>

          <div className={cn("grid gap-5 sm:grid-cols-2", !auto && "opacity-60")}>
            <ThresholdField
              label="自动通过门槛"
              value={s.approveThreshold}
              disabled={!auto}
              onChange={(v) => set({ approveThreshold: v })}
              hint="模型建议通过且置信度不低于它时自动通过"
            />
            <ThresholdField
              label="自动驳回门槛"
              value={s.rejectThreshold}
              disabled={!auto}
              onChange={(v) => set({ rejectThreshold: v })}
              hint="模型建议驳回且置信度不低于它时自动驳回"
            />
          </div>

          <SettingRow title="管理员手动下的封禁也可自动通过" value={s.autoApproveManual ? "允许" : "只处理风控与全站连带的封禁"} className={cn(!auto && "opacity-60")}>
            <Switch checked={s.autoApproveManual} disabled={!auto} onCheckedChange={(v) => set({ autoApproveManual: v })} aria-label="手动封禁也可自动通过" />
          </SettingRow>

          <Field label="AI 通道" className={cn(!modelOn && "opacity-60")}>
            <Select value={String(s.aiConfigId)} onValueChange={(v) => set({ aiConfigId: Number(v) })} disabled={!modelOn}>
              <SelectTrigger className="h-9 w-full sm:w-72" size="sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">自动（应用通道，没有时用平台通道）</SelectItem>
                {(configs.data ?? []).map((c) => (
                  <SelectItem key={c.id} value={String(c.id)}>
                    {c.name}
                    {c.enabled ? "" : "（已停用）"}
                  </SelectItem>
                ))}
                {!knownConfig ? <SelectItem value={String(s.aiConfigId)}>通道 #{s.aiConfigId}</SelectItem> : null}
              </SelectContent>
            </Select>
          </Field>
        </div>

        <div className={cn("grid gap-4 py-4 sm:grid-cols-3", !s.enabled && "opacity-60")}>
          <Field label="每条封禁最多申诉" error={problems.maxAppealsPerBan}>
            <NumberField value={s.maxAppealsPerBan} min={1} max={20} suffix="次" invalid={!!problems.maxAppealsPerBan} onChange={(v) => set({ maxAppealsPerBan: Math.round(v) })} ariaLabel="每条封禁最多申诉次数" />
          </Field>
          <Field label="两次申诉至少间隔" error={problems.cooldownHours}>
            <NumberField value={s.cooldownHours} min={0} max={720} suffix="小时" invalid={!!problems.cooldownHours} onChange={(v) => set({ cooldownHours: Math.round(v) })} ariaLabel="冷却小时" />
          </Field>
          <Field label="通过后白名单保护" error={problems.allowDays}>
            <NumberField value={s.allowDays} min={0} max={365} suffix="天" invalid={!!problems.allowDays} onChange={(v) => set({ allowDays: Math.round(v) })} ariaLabel="白名单保护天数" />
          </Field>
          <Field label="申诉内容最少" error={problems.minLength}>
            <NumberField value={s.minLength} min={1} suffix="字" invalid={!!problems.minLength} onChange={(v) => set({ minLength: Math.round(v) })} ariaLabel="最少字数" />
          </Field>
          <Field label="申诉内容最多" error={problems.maxLength}>
            <NumberField value={s.maxLength} min={1} max={10000} suffix="字" invalid={!!problems.maxLength} onChange={(v) => set({ maxLength: Math.round(v) })} ariaLabel="最多字数" />
          </Field>
        </div>

        <div className={cn("flex flex-col gap-1.5 py-4", !s.enabled && "opacity-60")}>
          <span className="text-xs text-muted-foreground">申诉页地址</span>
          <Input value={s.appealUrl} placeholder="https://example.com/appeal" aria-invalid={!!problems.appealUrl || undefined} onChange={(e) => set({ appealUrl: e.target.value })} />
          <FieldError message={problems.appealUrl} />
          <p className="text-xs leading-5 text-muted-foreground">封禁邮件里「提交申诉」按钮打开的地址，申诉凭证以 ?token= 拼在后面。留空时邮件提示用户到 App 里申诉。</p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="ghost" onClick={onDone}>取消</Button>
        <Button disabled={invalid || save.isPending} onClick={() => void submit()}>
          {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
          保存
        </Button>
      </DialogFooter>
    </>
  );
}

function ThresholdField({ label, value, onChange, disabled, hint }: { label: string; value: number; onChange: (v: number) => void; disabled?: boolean; hint: string }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-sm">{label}</span>
        <span className="text-sm font-semibold tabular-nums">{pct(value)}</span>
      </div>
      <Slider value={[Math.round(value * 100)]} min={50} max={100} step={1} disabled={disabled} onValueChange={([v]) => onChange(v / 100)} aria-label={label} />
      <p className="text-xs leading-5 text-muted-foreground">{hint}</p>
    </div>
  );
}
