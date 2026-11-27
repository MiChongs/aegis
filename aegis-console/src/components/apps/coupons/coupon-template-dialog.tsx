"use client";

import { useState } from "react";
import { Loader2, Lock, Save } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import type { AudienceKind, CouponKind, CouponScope, CouponTemplate, CouponTemplatePayload, CouponTheme, CouponValidityMode } from "@/lib/api/coupons";
import { useSaveCouponTemplateMutation } from "@/lib/coupon-hooks";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useAdminVipPlansQuery } from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";
import { AUDIENCE_LABEL, CouponTicket, PRICE_SCOPES, SCOPE_LABEL, ThemePicker } from "./coupon-shared";

/** 草稿：数字一律按字符串存（存 number 的输入框清不空） */
type Form = {
  name: string;
  description: string;
  theme: CouponTheme;
  kind: CouponKind;
  amount: string;
  /** 折扣按「几折」输入：8.5 → rate 85 */
  fold: string;
  maxDiscount: string;
  minAmount: string;
  scopes: CouponScope[];
  planIds: number[];
  stackWithPromotion: boolean;
  validityMode: CouponValidityMode;
  validFrom: string;
  validUntil: string;
  validDays: string;
  validDelayDays: string;
  totalQuantity: string;
  perUserLimit: string;
  claimable: boolean;
  claimStartsAt: string;
  claimEndsAt: string;
  audienceKind: AudienceKind;
  newUserDays: string;
  refundReturn: boolean;
};

function seed(t?: CouponTemplate | null): Form {
  return {
    name: t?.name ?? "",
    description: t?.description ?? "",
    theme: t?.theme ?? "red",
    kind: t?.kind ?? "amount_off",
    amount: t && t.kind === "amount_off" ? t.amount : "5",
    fold: t && t.kind === "percent_off" && t.rate ? String(t.rate / 10) : "8.5",
    maxDiscount: t && t.maxDiscount !== "0" && t.maxDiscount !== "0.00" ? t.maxDiscount : "",
    minAmount: t && t.minAmount !== "0" && t.minAmount !== "0.00" ? t.minAmount : "",
    scopes: t?.scopes?.length ? t.scopes : ["vip_purchase"],
    planIds: t?.planIds ?? [],
    stackWithPromotion: t?.stackWithPromotion ?? false,
    validityMode: t?.validityMode ?? "relative",
    validFrom: toLocalInputValue(t?.validFrom ?? null),
    validUntil: toLocalInputValue(t?.validUntil ?? null),
    validDays: String(t?.validDays || 7),
    validDelayDays: String(t?.validDelayDays ?? 0),
    totalQuantity: t && t.totalQuantity > 0 ? String(t.totalQuantity) : "",
    perUserLimit: t && t.perUserLimit > 0 ? String(t.perUserLimit) : "1",
    claimable: t?.claimable ?? false,
    claimStartsAt: toLocalInputValue(t?.claimStartsAt ?? null),
    claimEndsAt: toLocalInputValue(t?.claimEndsAt ?? null),
    audienceKind: t?.audience?.kind ?? "all",
    newUserDays: String(t?.audience?.newUserDays || 7),
    refundReturn: t?.refundReturn ?? true
  };
}

function toPayload(form: Form): CouponTemplatePayload {
  const fixed = form.validityMode === "fixed";
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    theme: form.theme,
    kind: form.kind,
    amount: form.kind === "amount_off" ? form.amount.trim() || "0" : "0",
    rate: form.kind === "percent_off" ? Math.round((Number(form.fold) || 0) * 10) : 0,
    maxDiscount: form.kind === "percent_off" ? form.maxDiscount.trim() || "0" : "0",
    minAmount: form.minAmount.trim() || "0",
    scopes: form.scopes,
    planIds: form.scopes.includes("vip_purchase") ? form.planIds : [],
    stackWithPromotion: form.stackWithPromotion,
    validityMode: form.validityMode,
    validFrom: fixed ? fromLocalInputValue(form.validFrom) ?? null : null,
    validUntil: fixed ? fromLocalInputValue(form.validUntil) ?? null : null,
    validDays: fixed ? 0 : Number(form.validDays) || 0,
    validDelayDays: fixed ? 0 : Number(form.validDelayDays) || 0,
    totalQuantity: Number(form.totalQuantity) || 0,
    perUserLimit: Number(form.perUserLimit) || 0,
    claimable: form.claimable,
    claimStartsAt: form.claimable ? fromLocalInputValue(form.claimStartsAt) ?? null : null,
    claimEndsAt: form.claimable ? fromLocalInputValue(form.claimEndsAt) ?? null : null,
    audience: { kind: form.audienceKind, newUserDays: form.audienceKind === "new_user" ? Number(form.newUserDays) || 0 : 0 },
    refundReturn: form.refundReturn
  };
}

/**
 * 新建 / 编辑一种券。已经发出过券（issuedCount > 0）的模板，规则字段（方式、面额、门槛、范围、叠加、有效期）锁定：
 * 用户手里的券改了面额就对不上了，服务端同样会拒（42322）。名称、说明、色调、库存、领取设置与退款返还仍可改。
 * 每次打开都由调用方换 key 重新挂载，草稿从 seed 派生，不用 effect 同步。
 */
export function CouponTemplateDialog({
  appKey,
  open,
  onOpenChange,
  template
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  template?: CouponTemplate | null;
}) {
  const [form, setForm] = useState<Form>(() => seed(template));
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => setForm((prev) => ({ ...prev, [key]: value }));
  const save = useSaveCouponTemplateMutation(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const locked = (template?.issuedCount ?? 0) > 0;
  const payload = toPayload(form);

  const submit = async () => {
    if (!payload.name) return toast.error("请填写券的名称");
    if (payload.scopes.length === 0) return toast.error("请至少选择一种适用订单");
    try {
      await save.mutateAsync({ id: template?.id, payload });
      toast.success(template ? "已保存" : "已创建（草稿），上线后才能发放");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  const toggleScope = (scope: CouponScope, checked: boolean) =>
    patch("scopes", checked ? PRICE_SCOPES.filter((item) => item === scope || form.scopes.includes(item)) : form.scopes.filter((item) => item !== scope));
  const togglePlan = (id: number, checked: boolean) =>
    patch("planIds", checked ? [...form.planIds, id].sort((a, b) => a - b) : form.planIds.filter((item) => item !== id));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-hidden p-0 sm:max-w-4xl">
        <DialogHeader className="px-6 pt-6">
          <DialogTitle>{template ? "编辑优惠券" : "新建优惠券"}</DialogTitle>
          <DialogDescription>新建的券是草稿，上线后才能发放与领取；一张订单最多用一张券。</DialogDescription>
        </DialogHeader>
        <div className="grid max-h-[calc(92vh-11rem)] gap-6 overflow-y-auto px-6 pb-2 md:grid-cols-[minmax(0,1fr)_300px]">
          <div className="min-w-0 space-y-5">
            <Field label="名称">
              <Input value={form.name} maxLength={32} placeholder="例如：新人立减券" onChange={(e) => patch("name", e.target.value)} />
            </Field>
            <Field label="说明" hint="展示在券详情里，可不填">
              <Textarea rows={2} value={form.description} maxLength={200} onChange={(e) => patch("description", e.target.value)} />
            </Field>

            {locked ? (
              <div className="flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
                <Lock className="mt-0.5 size-3.5 shrink-0" />
                已发出 {template?.issuedCount} 张，优惠方式、面额、门槛、适用范围、叠加与有效期不能再改；要换规则请新建一种券。
              </div>
            ) : null}

            <fieldset disabled={locked} className="space-y-5 disabled:opacity-60">
              <Field label="优惠方式">
                <ToggleGroup type="single" variant="outline" size="sm" value={form.kind} onValueChange={(value) => value && patch("kind", value as CouponKind)}>
                  <ToggleGroupItem value="amount_off" className="px-4 text-xs">立减</ToggleGroupItem>
                  <ToggleGroupItem value="percent_off" className="px-4 text-xs">折扣</ToggleGroupItem>
                </ToggleGroup>
              </Field>
              <div className="grid gap-4 sm:grid-cols-3">
                {form.kind === "amount_off" ? (
                  <Field label="立减金额" hint="元">
                    <Input inputMode="decimal" value={form.amount} onChange={(e) => patch("amount", e.target.value)} />
                  </Field>
                ) : (
                  <>
                    <Field label="折扣" hint="0.1–9.9 折">
                      <Input inputMode="decimal" value={form.fold} onChange={(e) => patch("fold", e.target.value)} />
                    </Field>
                    <Field label="最多减" hint="元，空为不封顶">
                      <Input inputMode="decimal" value={form.maxDiscount} placeholder="不封顶" onChange={(e) => patch("maxDiscount", e.target.value)} />
                    </Field>
                  </>
                )}
                <Field label="使用门槛" hint="满多少元可用，空为无门槛">
                  <Input inputMode="decimal" value={form.minAmount} placeholder="无门槛" onChange={(e) => patch("minAmount", e.target.value)} />
                </Field>
              </div>

              <Field label="适用订单">
                <div className="flex flex-wrap gap-2">
                  {PRICE_SCOPES.map((scope) => (
                    <label
                      key={scope}
                      className={cn(
                        "flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-1.5 text-xs",
                        form.scopes.includes(scope) ? "border-primary/40 bg-primary/5" : "border-border"
                      )}
                    >
                      <Checkbox checked={form.scopes.includes(scope)} onCheckedChange={(checked) => toggleScope(scope, checked === true)} />
                      {SCOPE_LABEL[scope]}
                    </label>
                  ))}
                </div>
                <p className="text-[11px] text-muted-foreground">余额充值不能用券（充进去的本金要等于实付）；积分充值用券后仍按原价发积分。</p>
              </Field>

              {form.scopes.includes("vip_purchase") ? (
                <Field label="限定套餐" hint={form.planIds.length ? `已选 ${form.planIds.length} 个` : "不选即全部套餐"}>
                  {plans.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{plansQuery.isLoading ? "加载中" : "暂无付费套餐"}</p>
                  ) : (
                    <div className="grid max-h-40 gap-1.5 overflow-y-auto rounded-lg border p-2 sm:grid-cols-2">
                      {plans.map((plan) => (
                        <label key={plan.id} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-accent/40">
                          <Checkbox checked={form.planIds.includes(plan.id)} onCheckedChange={(checked) => togglePlan(plan.id, checked === true)} />
                          <span className="min-w-0 flex-1 truncate">
                            {plan.name}
                            <span className="ml-1 text-muted-foreground">
                              {vipPlanTerm(plan)} · {plan.price}
                            </span>
                          </span>
                        </label>
                      ))}
                    </div>
                  )}
                </Field>
              ) : null}

              <SwitchRow
                label="可与优惠活动同享"
                hint="开启后在活动价之后再用券（活动也要允许与券同享才叠加）；关闭时只用券与只用活动取更便宜的那个"
                checked={form.stackWithPromotion}
                onChange={(value) => patch("stackWithPromotion", value)}
              />

              <Field label="有效期">
                <ToggleGroup
                  type="single"
                  variant="outline"
                  size="sm"
                  value={form.validityMode}
                  onValueChange={(value) => value && patch("validityMode", value as CouponValidityMode)}
                >
                  <ToggleGroupItem value="relative" className="px-4 text-xs">领取后生效</ToggleGroupItem>
                  <ToggleGroupItem value="fixed" className="px-4 text-xs">固定时段</ToggleGroupItem>
                </ToggleGroup>
                {form.validityMode === "relative" ? (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="有效天数">
                      <Input type="number" min={1} max={3650} value={form.validDays} onChange={(e) => patch("validDays", e.target.value)} />
                    </Field>
                    <Field label="延迟生效" hint="天，0 为立即可用">
                      <Input type="number" min={0} max={365} value={form.validDelayDays} onChange={(e) => patch("validDelayDays", e.target.value)} />
                    </Field>
                  </div>
                ) : (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="生效时间" hint="空为发放时">
                      <Input type="datetime-local" value={form.validFrom} onChange={(e) => patch("validFrom", e.target.value)} />
                    </Field>
                    <Field label="失效时间">
                      <Input type="datetime-local" value={form.validUntil} onChange={(e) => patch("validUntil", e.target.value)} />
                    </Field>
                  </div>
                )}
              </Field>
            </fieldset>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="发放总量" hint={template?.issuedCount ? `已发 ${template.issuedCount} 张；空为不限` : "全部渠道共用，空为不限"}>
                <Input type="number" min={0} value={form.totalQuantity} placeholder="不限" onChange={(e) => patch("totalQuantity", e.target.value)} />
              </Field>
              <Field label="每人限领" hint="只限领券中心，空为不限">
                <Input type="number" min={0} max={1000} value={form.perUserLimit} placeholder="不限" onChange={(e) => patch("perUserLimit", e.target.value)} />
              </Field>
            </div>

            <SwitchRow
              label="放进领券中心"
              hint="用户在 App 与官网的领券中心自己领；关闭时只能由管理员、返券活动或其它奖励渠道发放"
              checked={form.claimable}
              onChange={(value) => patch("claimable", value)}
            />
            {form.claimable ? (
              <div className="space-y-4 rounded-xl border p-3">
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="开抢时间" hint="空为上线即可领">
                    <Input type="datetime-local" value={form.claimStartsAt} onChange={(e) => patch("claimStartsAt", e.target.value)} />
                  </Field>
                  <Field label="领取截止" hint="空为不截止">
                    <Input type="datetime-local" value={form.claimEndsAt} onChange={(e) => patch("claimEndsAt", e.target.value)} />
                  </Field>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="谁能领">
                    <Select value={form.audienceKind} onValueChange={(value) => patch("audienceKind", value as AudienceKind)}>
                      <SelectTrigger className="h-9 w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {(Object.keys(AUDIENCE_LABEL) as AudienceKind[]).map((kind) => (
                          <SelectItem key={kind} value={kind}>
                            {AUDIENCE_LABEL[kind]}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  {form.audienceKind === "new_user" ? (
                    <Field label="注册天数内" hint="1–365 天">
                      <Input type="number" min={1} max={365} value={form.newUserDays} onChange={(e) => patch("newUserDays", e.target.value)} />
                    </Field>
                  ) : null}
                </div>
              </div>
            ) : null}

            <SwitchRow
              label="全额退款后返还"
              hint="订单全额退款时把这张券还给用户（还在有效期内时）"
              checked={form.refundReturn}
              onChange={(value) => patch("refundReturn", value)}
            />
          </div>

          <aside className="space-y-4 md:sticky md:top-0 md:self-start">
            <Field label="色调">
              <ThemePicker value={form.theme} onChange={(theme) => patch("theme", theme)} />
            </Field>
            <Field label="预览">
              <CouponTicket
                template={{
                  name: form.name,
                  theme: form.theme,
                  kind: payload.kind,
                  amount: payload.amount,
                  rate: payload.rate,
                  minAmount: payload.minAmount,
                  maxDiscount: payload.maxDiscount,
                  validityMode: payload.validityMode,
                  validFrom: payload.validFrom,
                  validUntil: payload.validUntil,
                  validDays: payload.validDays,
                  validDelayDays: payload.validDelayDays,
                  scopes: payload.scopes
                }}
              />
            </Field>
            <p className="text-[11px] leading-5 text-muted-foreground">
              券面在 App 与官网按色调取主题色绘制；优惠后每单至少要付 0.01 元，渠道不接受 0 元订单。
            </p>
          </aside>
        </div>
        <DialogFooter className="border-t px-6 py-4">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs font-medium">{label}</Label>
        {hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </div>
  );
}
