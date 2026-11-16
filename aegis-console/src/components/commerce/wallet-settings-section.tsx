"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowLeftRight, CircleAlert, Loader2, RotateCcw, Save, Settings2, Wallet } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Section, StateBlock, errorMessage } from "@/components/users/detail/kit";
import { compareDecimal, isPositiveDecimal } from "@/components/users/detail/user-assets-adjust";
import type { WalletSettings, WalletSettingsView } from "@/lib/api/types";
import { useUpdateWalletSettingsMutation, useWalletSettingsQuery } from "@/lib/commerce-hooks";
import { AmountInput, WalletField, WalletToggleRow, moneyProblem } from "./wallet-config-fields";

const NOTICE_MAX = 500;

/** 后端用 "0" 表示「不限」；表单里留空更直观 */
function unlimitedToBlank(value?: string) {
  const text = (value ?? "").trim();
  return !text || !isPositiveDecimal(text) ? "" : text;
}

function draftFrom(view: WalletSettingsView): WalletSettings {
  return {
    rechargeEnabled: view.rechargeEnabled,
    customAmountEnabled: view.customAmountEnabled,
    rechargeMin: view.rechargeMin ?? "",
    rechargeMax: view.rechargeMax ?? "",
    exchangeEnabled: view.exchangeEnabled,
    exchangeMin: view.exchangeMin ?? "",
    exchangeMax: unlimitedToBlank(view.exchangeMax),
    exchangeDailyMax: unlimitedToBlank(view.exchangeDailyMax),
    notice: view.notice ?? ""
  };
}

const MONEY_KEYS = ["rechargeMin", "rechargeMax", "exchangeMin", "exchangeMax", "exchangeDailyMax"] as const;

function sameDraft(a: WalletSettings, b: WalletSettings) {
  const sameMoney = MONEY_KEYS.every((key) => {
    // 「1」与「1.00」是同一个金额
    const cmp = compareDecimal(a[key].trim() || "0", b[key].trim() || "0");
    return cmp === null ? a[key].trim() === b[key].trim() : cmp === 0;
  });
  return (
    sameMoney &&
    a.rechargeEnabled === b.rechargeEnabled &&
    a.customAmountEnabled === b.customAmountEnabled &&
    a.exchangeEnabled === b.exchangeEnabled &&
    a.notice.trim() === b.notice.trim()
  );
}

/** 与后端 Settings.Validate 同口径，按出现顺序返回问题 */
function settingsProblems(draft: WalletSettings): string[] {
  const problems = [
    moneyProblem("自定义充值下限", draft.rechargeMin),
    moneyProblem("自定义充值上限", draft.rechargeMax),
    moneyProblem("单笔兑换下限", draft.exchangeMin),
    moneyProblem("单笔兑换上限", draft.exchangeMax, true),
    moneyProblem("每日兑换上限", draft.exchangeDailyMax, true)
  ].filter((item): item is string => Boolean(item));
  if (problems.length) return problems;
  if ((compareDecimal(draft.rechargeMin, draft.rechargeMax) ?? 0) > 0) problems.push("自定义充值下限不能大于上限");
  if (isPositiveDecimal(draft.exchangeMax) && (compareDecimal(draft.exchangeMin, draft.exchangeMax) ?? 0) > 0) {
    problems.push("单笔兑换下限不能大于上限");
  }
  if (isPositiveDecimal(draft.exchangeDailyMax) && (compareDecimal(draft.exchangeMin, draft.exchangeDailyMax) ?? 0) > 0) {
    problems.push("单笔兑换下限不能大于每日上限");
  }
  if ([...draft.notice.trim()].length > NOTICE_MAX) problems.push(`说明文字最多 ${NOTICE_MAX} 字`);
  return problems;
}

/**
 * 钱包设置：充值开关与自定义金额范围、余额兑换积分的开关与额度、钱包页说明文字。
 *
 * 兑换比例与币种只读展示：余额兑积分与在线直购积分共用「交易设置」的同一个比例，
 * 两条路价格不一致时用户自然会走便宜的那条（docs/wallet.md）。
 * 后端 PUT 是全量覆盖，九个字段整体提交。
 */
export function WalletSettingsSection({ appKey, canConfig }: { appKey: string; canConfig: boolean }) {
  const query = useWalletSettingsQuery(appKey);

  if (query.isLoading) {
    return (
      <Section title="钱包设置" icon={Settings2} tone="primary">
        <div className="grid gap-4 lg:grid-cols-2">
          <Skeleton className="h-48 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
        </div>
      </Section>
    );
  }

  if (!query.data) {
    return (
      <Section title="钱包设置" icon={Settings2} tone="primary">
        <StateBlock kind="error" description={errorMessage(query.error, "钱包设置加载失败")} onRetry={() => query.refetch()} />
      </Section>
    );
  }

  // 换应用时以空白重挂，草稿不串到别的应用
  return <WalletSettingsForm key={appKey} appKey={appKey} view={query.data} canConfig={canConfig} />;
}

function WalletSettingsForm({
  appKey,
  view,
  canConfig
}: {
  appKey: string;
  view: WalletSettingsView;
  canConfig: boolean;
}) {
  const mutation = useUpdateWalletSettingsMutation(appKey);
  const [draft, setDraft] = useState<WalletSettings>(() => draftFrom(view));
  const saved = draftFrom(view);
  const patch = <K extends keyof WalletSettings>(key: K, value: WalletSettings[K]) =>
    setDraft((current) => ({ ...current, [key]: value }));

  const dirty = !sameDraft(draft, saved);
  const problems = settingsProblems(draft);
  const readOnly = !canConfig || mutation.isPending;
  const currency = view.currency || "CNY";
  const noticeLength = [...draft.notice].length;

  const save = async () => {
    if (problems.length) {
      toast.error(problems[0]);
      return;
    }
    try {
      const result = await mutation.mutateAsync({
        ...draft,
        rechargeMin: draft.rechargeMin.trim(),
        rechargeMax: draft.rechargeMax.trim(),
        exchangeMin: draft.exchangeMin.trim(),
        exchangeMax: draft.exchangeMax.trim() || "0",
        exchangeDailyMax: draft.exchangeDailyMax.trim() || "0",
        notice: draft.notice.trim()
      });
      setDraft(draftFrom(result));
      toast.success("钱包设置已保存");
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section
      title="钱包设置"
      icon={Settings2}
      tone="primary"
      description={canConfig ? "控制用户端的余额充值与余额兑换积分，保存后立即生效" : "当前账号没有修改钱包设置的权限，仅可查看"}
      actions={
        canConfig ? (
          <>
            <Button variant="ghost" size="sm" disabled={!dirty || mutation.isPending} onClick={() => setDraft(saved)}>
              <RotateCcw />
              还原
            </Button>
            <Button size="sm" disabled={!dirty || problems.length > 0 || mutation.isPending} onClick={save}>
              {mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存设置
            </Button>
          </>
        ) : null
      }
      footer={
        dirty && problems.length ? (
          <p className="flex items-center gap-1.5 text-xs text-red-600 dark:text-red-400">
            <CircleAlert className="size-3.5 shrink-0" />
            {problems[0]}
          </p>
        ) : null
      }
    >
      <div className="grid gap-6 lg:grid-cols-2">
        {/* 充值 */}
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Wallet className="size-4 text-muted-foreground" />
            余额充值
          </div>
          <WalletToggleRow
            label="开放充值"
            hint="关闭后用户端不能发起余额充值，已支付的订单照常入账"
            checked={draft.rechargeEnabled}
            disabled={readOnly}
            onChange={(value) => patch("rechargeEnabled", value)}
          />
          <WalletToggleRow
            label="允许自定义金额"
            hint="关闭后只能按充值档位购买"
            checked={draft.customAmountEnabled}
            disabled={readOnly || !draft.rechargeEnabled}
            onChange={(value) => patch("customAmountEnabled", value)}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <WalletField label="自定义金额下限">
              <AmountInput
                value={draft.rechargeMin}
                suffix={currency}
                disabled={readOnly || !draft.rechargeEnabled || !draft.customAmountEnabled}
                invalid={Boolean(moneyProblem("下限", draft.rechargeMin))}
                onChange={(value) => patch("rechargeMin", value)}
              />
            </WalletField>
            <WalletField label="自定义金额上限">
              <AmountInput
                value={draft.rechargeMax}
                suffix={currency}
                disabled={readOnly || !draft.rechargeEnabled || !draft.customAmountEnabled}
                invalid={Boolean(moneyProblem("上限", draft.rechargeMax))}
                onChange={(value) => patch("rechargeMax", value)}
              />
            </WalletField>
          </div>
          <p className="text-xs text-muted-foreground">单笔范围只约束自定义金额，按档位充值不受此限。</p>
        </div>

        {/* 兑换 */}
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <ArrowLeftRight className="size-4 text-muted-foreground" />
            余额兑换积分
          </div>
          <WalletToggleRow
            label="开放兑换"
            hint="只有充值余额（本金）能兑换，赠送余额不能兑换"
            checked={draft.exchangeEnabled}
            disabled={readOnly}
            onChange={(value) => patch("exchangeEnabled", value)}
          />
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/30 px-3 py-2.5">
            <div className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">兑换比例</span>
              <span className="text-sm font-medium tabular-nums">
                1 {currency} = {view.rate.toLocaleString("zh-CN")} 积分
              </span>
            </div>
            <p className="max-w-64 text-xs text-muted-foreground">
              比例与币种和在线购买积分共用，在应用
              <Link href={`/apps/${encodeURIComponent(appKey)}?tab=payment`} className="mx-0.5 text-foreground underline-offset-2 hover:underline">
                支付
              </Link>
              的「交易设置」里修改
            </p>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <WalletField label="单笔下限">
              <AmountInput
                value={draft.exchangeMin}
                suffix={currency}
                disabled={readOnly || !draft.exchangeEnabled}
                invalid={Boolean(moneyProblem("下限", draft.exchangeMin))}
                onChange={(value) => patch("exchangeMin", value)}
              />
            </WalletField>
            <WalletField label="单笔上限">
              <AmountInput
                value={draft.exchangeMax}
                suffix={currency}
                placeholder="不限"
                disabled={readOnly || !draft.exchangeEnabled}
                invalid={Boolean(moneyProblem("上限", draft.exchangeMax, true))}
                onChange={(value) => patch("exchangeMax", value)}
              />
            </WalletField>
            <WalletField label="每日上限">
              <AmountInput
                value={draft.exchangeDailyMax}
                suffix={currency}
                placeholder="不限"
                disabled={readOnly || !draft.exchangeEnabled}
                invalid={Boolean(moneyProblem("上限", draft.exchangeDailyMax, true))}
                onChange={(value) => patch("exchangeDailyMax", value)}
              />
            </WalletField>
          </div>
          <p className="text-xs text-muted-foreground">额度按每人兑换掉的本金计算，留空表示不限；每日按应用默认时区的自然日统计。</p>
        </div>

        {/* 说明 */}
        <WalletField
          label="钱包页说明"
          hint="显示在用户端钱包页底部，如充值须知、到账说明，可留空"
          counter={`${noticeLength}/${NOTICE_MAX}`}
          className="lg:col-span-2"
        >
          <Textarea
            value={draft.notice}
            rows={3}
            maxLength={NOTICE_MAX}
            disabled={readOnly}
            placeholder="例如：充值金额不支持提现；赠送余额只能消费，不能兑换积分。"
            onChange={(event) => patch("notice", event.target.value)}
          />
        </WalletField>
      </div>
    </Section>
  );
}
