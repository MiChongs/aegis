"use client";

import { useState } from "react";
import { CircleAlert, Loader2, Save, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { QuickChips, addDecimal, isPositiveDecimal } from "@/components/users/detail/user-assets-adjust";
import { errorMessage } from "@/components/users/detail/kit";
import type { AdminWalletPackage, WalletPackagePayload } from "@/lib/api/types";
import { useSaveWalletPackageMutation } from "@/lib/commerce-hooks";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { formatMoney } from "./commerce-format";
import { AmountInput, WalletField, moneyProblem } from "./wallet-config-fields";

export type WalletPackageEditorTarget = { mode: "create"; sortOrder: number } | { mode: "edit"; item: AdminWalletPackage };

const TITLE_MAX = 60;
const BADGE_MAX = 20;
const DESCRIPTION_MAX = 200;
const BADGE_PRESETS = ["首充", "超值", "推荐", "限时"];
const BONUS_INTEGRAL_MAX = 100_000_000;
const PER_USER_LIMIT_MAX = 10_000;

type Form = {
  title: string;
  amount: string;
  bonusAmount: string;
  bonusIntegral: string;
  badge: string;
  description: string;
  perUserLimit: string;
  isActive: boolean;
  startsAt: string;
  endsAt: string;
};

function seed(target: WalletPackageEditorTarget): Form {
  if (target.mode === "create") {
    return {
      title: "",
      amount: "",
      bonusAmount: "",
      bonusIntegral: "",
      badge: "",
      description: "",
      perUserLimit: "",
      isActive: true,
      startsAt: "",
      endsAt: ""
    };
  }
  const item = target.item;
  return {
    title: item.title,
    amount: item.amount,
    bonusAmount: isPositiveDecimal(item.bonusAmount) ? item.bonusAmount : "",
    bonusIntegral: item.bonusIntegral ? String(item.bonusIntegral) : "",
    badge: item.badge ?? "",
    description: item.description ?? "",
    perUserLimit: item.perUserLimit ? String(item.perUserLimit) : "",
    isActive: item.isActive,
    startsAt: toLocalInputValue(item.startsAt),
    endsAt: toLocalInputValue(item.endsAt)
  };
}

const runes = (value: string) => [...value.trim()].length;

/** 与后端 RechargePackage.Validate 同口径，按出现顺序返回问题 */
function formProblems(form: Form): string[] {
  const problems: string[] = [];
  if (!form.title.trim()) problems.push("请填写档位名称");
  else if (runes(form.title) > TITLE_MAX) problems.push(`档位名称最多 ${TITLE_MAX} 字`);
  if (runes(form.badge) > BADGE_MAX) problems.push(`角标最多 ${BADGE_MAX} 字`);
  if (runes(form.description) > DESCRIPTION_MAX) problems.push(`档位说明最多 ${DESCRIPTION_MAX} 字`);
  const amount = moneyProblem("充值金额", form.amount);
  if (amount) problems.push(amount);
  const bonus = moneyProblem("赠送余额", form.bonusAmount, true);
  if (bonus) problems.push(bonus);
  const integral = Number(form.bonusIntegral || "0");
  if (!Number.isSafeInteger(integral) || integral > BONUS_INTEGRAL_MAX) problems.push("赠送积分必须在 0–100,000,000 之间");
  const limit = Number(form.perUserLimit || "0");
  if (!Number.isSafeInteger(limit) || limit > PER_USER_LIMIT_MAX) problems.push("每人限购次数必须在 0–10000 之间");
  const startsAt = fromLocalInputValue(form.startsAt);
  const endsAt = fromLocalInputValue(form.endsAt);
  if (startsAt && endsAt && endsAt <= startsAt) problems.push("结束时间必须晚于开始时间");
  return problems;
}

/**
 * 充值档位的新建与编辑。
 *
 * 用户付 amount，本金入账 amount，另赠 bonusAmount 赠送余额与 bonusIntegral 积分。
 * 下单时服务端固化档位快照，所以这里的修改只影响之后的订单。
 */
export function WalletPackageEditor({
  appKey,
  target,
  currency,
  onClose
}: {
  appKey: string;
  target: WalletPackageEditorTarget | null;
  currency: string;
  onClose: () => void;
}) {
  return (
    <Dialog open={Boolean(target)} onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="sm:max-w-xl">
        {target ? (
          <EditorBody
            key={target.mode === "edit" ? `edit-${target.item.id}` : "create"}
            appKey={appKey}
            target={target}
            currency={currency}
            onClose={onClose}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function EditorBody({
  appKey,
  target,
  currency,
  onClose
}: {
  appKey: string;
  target: WalletPackageEditorTarget;
  currency: string;
  onClose: () => void;
}) {
  const mutation = useSaveWalletPackageMutation(appKey);
  const [form, setForm] = useState<Form>(() => seed(target));
  // 空白新建表单不一上来就报「请填写档位名称」，动过任意一项之后再提示
  const [touched, setTouched] = useState(false);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => {
    setTouched(true);
    setForm((current) => ({ ...current, [key]: value }));
  };
  const editing = target.mode === "edit" ? target.item : null;
  const problems = formProblems(form);

  const amountOk = !moneyProblem("充值金额", form.amount);
  const bonusOk = !moneyProblem("赠送余额", form.bonusAmount, true);
  const bonus = form.bonusAmount.trim() || "0";
  const credited = amountOk && bonusOk ? addDecimal(form.amount.trim(), bonus) : null;
  const integral = Number(form.bonusIntegral || "0");
  const summary =
    credited !== null
      ? [
          `用户支付 ${formatMoney(form.amount.trim(), currency)}，到账 ${formatMoney(credited, currency)}`,
          isPositiveDecimal(bonus) ? `（充值 ${formatMoney(form.amount.trim())} / 赠送 ${formatMoney(bonus)}）` : "",
          integral > 0 ? `，另得 ${integral.toLocaleString("zh-CN")} 积分` : ""
        ].join("")
      : null;

  const submit = async () => {
    if (problems.length) {
      toast.error(problems[0]);
      return;
    }
    const payload: WalletPackagePayload = {
      title: form.title.trim(),
      amount: form.amount.trim(),
      bonusAmount: bonus,
      bonusIntegral: integral,
      badge: form.badge.trim(),
      description: form.description.trim(),
      perUserLimit: Number(form.perUserLimit || "0"),
      isActive: form.isActive,
      sortOrder: editing ? editing.sortOrder : target.mode === "create" ? target.sortOrder : 0,
      startsAt: fromLocalInputValue(form.startsAt) ?? null,
      endsAt: fromLocalInputValue(form.endsAt) ?? null
    };
    try {
      await mutation.mutateAsync({ packageId: editing?.id, payload });
      toast.success(editing ? "充值档位已保存" : "充值档位已创建");
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle className="text-base">{editing ? "编辑充值档位" : "新建充值档位"}</DialogTitle>
        <DialogDescription>
          {editing
            ? `已售 ${(editing.stat?.orders ?? 0).toLocaleString("zh-CN")} 笔。修改只影响之后的订单，已下的订单按下单时的快照履约。`
            : "充值金额进入充值余额（本金），赠送部分进入赠送余额，只能消费，不能兑换积分。"}
        </DialogDescription>
      </DialogHeader>

      <div className="flex max-h-[65vh] flex-col gap-4 overflow-y-auto pr-1">
        <WalletField label="档位名称" counter={`${runes(form.title)}/${TITLE_MAX}`}>
          <Input
            value={form.title}
            maxLength={TITLE_MAX}
            placeholder="例如：充 100 送 20"
            onChange={(event) => patch("title", event.target.value)}
          />
        </WalletField>

        <div className="grid gap-4 sm:grid-cols-3">
          <WalletField label="充值金额">
            <AmountInput value={form.amount} suffix={currency} onChange={(value) => patch("amount", value)} />
          </WalletField>
          <WalletField label="赠送余额">
            <AmountInput value={form.bonusAmount} suffix={currency} placeholder="不赠送" onChange={(value) => patch("bonusAmount", value)} />
          </WalletField>
          <WalletField label="赠送积分">
            <AmountInput value={form.bonusIntegral} suffix="积分" placeholder="不赠送" integer onChange={(value) => patch("bonusIntegral", value)} />
          </WalletField>
        </div>
        {summary ? <p className="-mt-2 text-xs text-muted-foreground">{summary}</p> : null}

        <div className="grid gap-4 sm:grid-cols-2">
          <WalletField label="角标" counter={`${runes(form.badge)}/${BADGE_MAX}`}>
            <Input value={form.badge} maxLength={BADGE_MAX} placeholder="不显示" onChange={(event) => patch("badge", event.target.value)} />
            <QuickChips items={BADGE_PRESETS} value={form.badge.trim()} onPick={(value) => patch("badge", value)} />
          </WalletField>
          <WalletField label="每人限购" hint="留空不限；首充专享设为 1。退款不让出名额">
            <AmountInput value={form.perUserLimit} suffix="次" placeholder="不限" integer onChange={(value) => patch("perUserLimit", value)} />
            <QuickChips items={["1"]} value={form.perUserLimit} onPick={(value) => patch("perUserLimit", value)} render={() => "首充专享"} />
          </WalletField>
        </div>

        <WalletField label="说明" counter={`${runes(form.description)}/${DESCRIPTION_MAX}`}>
          <Textarea
            value={form.description}
            rows={2}
            maxLength={DESCRIPTION_MAX}
            placeholder="选填，显示在档位下方"
            onChange={(event) => patch("description", event.target.value)}
          />
        </WalletField>

        <div className="grid gap-4 sm:grid-cols-2">
          <WalletField label="开始时间" hint="留空即立即开始">
            <DateTimeInput value={form.startsAt} onChange={(value) => patch("startsAt", value)} />
          </WalletField>
          <WalletField label="结束时间" hint="留空即长期有效；结束后不再下发">
            <DateTimeInput value={form.endsAt} onChange={(value) => patch("endsAt", value)} />
          </WalletField>
        </div>
      </div>

      {problems.length && (touched || editing) ? (
        <p className="flex items-center gap-1.5 text-xs text-red-600 dark:text-red-400">
          <CircleAlert className="size-3.5 shrink-0" />
          {problems[0]}
        </p>
      ) : null}

      <DialogFooter className="items-center sm:justify-between">
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <Switch checked={form.isActive} onCheckedChange={(value) => patch("isActive", value)} />
          {form.isActive ? "保存后上架" : "保存为下架"}
        </label>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClose} disabled={mutation.isPending}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={mutation.isPending || problems.length > 0}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            {editing ? "保存修改" : "创建档位"}
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}

function DateTimeInput({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="relative">
      <Input type="datetime-local" value={value} onChange={(event) => onChange(event.target.value)} className={value ? "pr-8" : undefined} />
      {value ? (
        <button
          type="button"
          aria-label="清除"
          onClick={() => onChange("")}
          className="absolute top-1/2 right-2 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      ) : null}
    </div>
  );
}
