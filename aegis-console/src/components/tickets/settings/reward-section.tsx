"use client";

import { useState } from "react";
import { Coins, Save } from "lucide-react";
import { notify } from "@/lib/notify";
import { useTicketCategoriesQuery } from "@/lib/ticket-hooks";
import { useFeedbackRewardPolicyQuery, useSaveFeedbackRewardPolicyMutation } from "@/lib/feedback-reward-hooks";
import type { FeedbackRewardPolicy } from "@/lib/api/feedback-rewards";
import type { TicketCategory } from "@/lib/api/tickets";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { formatDateTime } from "../ticket-shared";
import { Field, ListSkeleton, SectionEmpty, SectionError, SectionHeader, SwitchRow, reportError } from "./settings-shared";

// 反馈奖励：按应用配置「有效反馈奖励积分」的策略（后端 docs/feedback-rewards.md）。
//
// 只有应用范围有这一节的内容（奖励发的是该应用用户的积分）；平台级范围给一行说明。
// 建议积分只是发放时的预填值，上限才是硬约束。所有数字 0 = 不限（默认建议积分为 0 时不预填）。
// 改策略要该应用的「工单配置」权限，能不能保存以后端为准。

type PolicyForm = {
  enabled: boolean;
  defaultPoints: string;
  maxPoints: string;
  userDailyLimit: string;
  userMonthlyLimit: string;
  categoryPoints: Record<string, string>;
};

function formFromPolicy(policy: FeedbackRewardPolicy): PolicyForm {
  const categoryPoints: Record<string, string> = {};
  for (const [key, value] of Object.entries(policy.categoryPoints ?? {})) categoryPoints[key] = String(value);
  return {
    enabled: policy.enabled,
    defaultPoints: String(policy.defaultPoints || ""),
    maxPoints: String(policy.maxPoints || ""),
    userDailyLimit: String(policy.userDailyLimit || ""),
    userMonthlyLimit: String(policy.userMonthlyLimit || ""),
    categoryPoints
  };
}

const toNumber = (value: string) => (value.trim() === "" ? 0 : Number(value));
const digitsOnly = (value: string) => value.replace(/[^\d]/g, "");

/** 前端先做一遍与后端相同的校验，给出就近的提示；最终以后端为准 */
function validate(form: PolicyForm): string {
  const max = toNumber(form.maxPoints);
  const daily = toNumber(form.userDailyLimit);
  const monthly = toNumber(form.userMonthlyLimit);
  if (max > 0 && toNumber(form.defaultPoints) > max) return "默认建议积分不能超过单条上限";
  for (const value of Object.values(form.categoryPoints)) {
    if (max > 0 && toNumber(value) > max) return "分类的建议积分不能超过单条上限";
  }
  if (daily > 0 && monthly > 0 && daily > monthly) return "每用户每日上限不能超过每月上限";
  return "";
}

export function RewardSection({ appId }: { appId: number }) {
  const policyQuery = useFeedbackRewardPolicyQuery(appId);
  const categoriesQuery = useTicketCategoriesQuery(appId);

  if (appId === 0) {
    return (
      <div className="space-y-4">
        <SectionHeader title="反馈奖励" description="给被采纳的意见反馈发积分。" />
        <SectionEmpty icon={Coins} title="反馈奖励按应用配置" description="奖励发的是应用用户的积分，请在上方选择一个应用。" />
      </div>
    );
  }
  if (policyQuery.isError) return <SectionError error={policyQuery.error} onRetry={() => void policyQuery.refetch()} />;
  if (!policyQuery.data || categoriesQuery.isLoading) return <ListSkeleton rows={3} />;

  const categories = (categoriesQuery.data ?? []).filter((item) => (item.kind ?? "ticket") === "feedback" && item.enabled);
  const policy = policyQuery.data;
  return <RewardForm key={`${appId}-${policy.updatedAt ?? "default"}`} appId={appId} policy={policy} categories={categories} />;
}

function RewardForm({ appId, policy, categories }: { appId: number; policy: FeedbackRewardPolicy; categories: TicketCategory[] }) {
  const saveMut = useSaveFeedbackRewardPolicyMutation();
  const [form, setForm] = useState<PolicyForm>(() => formFromPolicy(policy));
  const error = validate(form);
  const set = (patch: Partial<PolicyForm>) => setForm((current) => ({ ...current, ...patch }));

  const save = async () => {
    if (error) return;
    const categoryPoints: Record<string, number> = {};
    for (const [key, value] of Object.entries(form.categoryPoints)) {
      const points = toNumber(value);
      if (points > 0) categoryPoints[key] = points;
    }
    try {
      await saveMut.mutateAsync({
        appid: appId,
        enabled: form.enabled,
        defaultPoints: toNumber(form.defaultPoints),
        categoryPoints,
        maxPoints: toNumber(form.maxPoints),
        userDailyLimit: toNumber(form.userDailyLimit),
        userMonthlyLimit: toNumber(form.userMonthlyLimit)
      });
      notify.success(form.enabled ? "反馈奖励已保存并开启" : "反馈奖励已保存（未开启）");
    } catch (failure) {
      reportError(failure, "保存失败");
    }
  };

  const numberInput = (value: string, onChange: (next: string) => void, label: string, placeholder = "不限") => (
    <Input
      inputMode="numeric"
      value={value}
      onChange={(event) => onChange(digitsOnly(event.target.value))}
      placeholder={placeholder}
      aria-label={label}
      className="h-8 tabular-nums"
    />
  );

  return (
    <div className="space-y-5">
      <SectionHeader
        title="反馈奖励"
        description="在反馈详情里给被采纳的反馈发积分，也可以在「解决」时勾选同时发放；一条反馈只能有一笔有效奖励，误发可撤回扣回。"
        action={
          <Button size="sm" disabled={Boolean(error) || saveMut.isPending} onClick={() => void save()}>
            <Save />
            保存
          </Button>
        }
      />

      <SwitchRow
        title="开启反馈奖励"
        description={
          policy.configured
            ? `上次由 ${policy.updatedByName || "管理员"} 于 ${formatDateTime(policy.updatedAt)} 修改`
            : "尚未配置：下面是推荐的默认值，保存后生效"
        }
        control={<Switch checked={form.enabled} onCheckedChange={(enabled) => set({ enabled })} aria-label="开启反馈奖励" />}
      />

      <div className="space-y-2">
        <h3 className="text-sm font-medium text-foreground">建议积分</h3>
        <p className="text-xs text-muted-foreground">发放时按反馈分类预填，处理人可以改；分类留空时用默认值。</p>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <div className="flex items-center gap-3 px-4 py-2.5">
            <span className="min-w-0 flex-1 text-sm text-foreground">默认</span>
            <div className="w-28">{numberInput(form.defaultPoints, (defaultPoints) => set({ defaultPoints }), "默认建议积分", "不预填")}</div>
          </div>
          {categories.map((category) => (
            <div key={category.id} className="flex items-center gap-3 px-4 py-2.5">
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm text-foreground">{category.name}</span>
                <span className="block truncate font-mono text-[11px] text-muted-foreground">{category.key}</span>
              </span>
              <div className="w-28">
                {numberInput(
                  form.categoryPoints[category.key] ?? "",
                  (value) => set({ categoryPoints: { ...form.categoryPoints, [category.key]: value } }),
                  `${category.name}的建议积分`,
                  "用默认值"
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="space-y-2">
        <h3 className="text-sm font-medium text-foreground">上限</h3>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="单条上限" hint="一条反馈最多发多少">
            {numberInput(form.maxPoints, (maxPoints) => set({ maxPoints }), "单条上限")}
          </Field>
          <Field label="每用户每日" hint="同一用户一天内累计">
            {numberInput(form.userDailyLimit, (userDailyLimit) => set({ userDailyLimit }), "每用户每日上限")}
          </Field>
          <Field label="每用户每月" hint="同一用户一个自然月累计">
            {numberInput(form.userMonthlyLimit, (userMonthlyLimit) => set({ userMonthlyLimit }), "每用户每月上限")}
          </Field>
        </div>
        <p className="text-xs text-muted-foreground">留空或 0 表示不限；已撤回的奖励不计入累计。</p>
      </div>

      {error ? <p className="text-sm text-destructive">{error}</p> : null}
    </div>
  );
}
