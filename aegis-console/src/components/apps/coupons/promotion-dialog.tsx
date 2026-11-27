"use client";

import { useState } from "react";
import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import type { AudienceKind, CouponScope, CouponTheme, DiscountMode, Promotion, PromotionKind, PromotionPayload } from "@/lib/api/coupons";
import { useWalletPackagesQuery } from "@/lib/commerce-hooks";
import { useSavePromotionMutation } from "@/lib/coupon-hooks";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useAdminVipPlansQuery } from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";
import {
  AUDIENCE_LABEL,
  CouponPicker,
  DISCOUNT_MODE_LABEL,
  PRICE_SCOPES,
  PROMOTION_KIND_LABEL,
  REBATE_SCOPES,
  SCOPE_LABEL,
  THEME_VAR,
  ThemePicker,
  defaultPromotionTag,
  promotionRuleText,
  themeStyle
} from "./coupon-shared";
import { Field } from "./coupon-template-dialog";

type TierDraft = { key: number; minAmount: string; off: string };
type RebateDraft = { key: number; templateId: number; count: string };

type Form = {
  name: string;
  description: string;
  tag: string;
  theme: CouponTheme;
  kind: PromotionKind;
  discountMode: DiscountMode;
  fold: string;
  amount: string;
  maxDiscount: string;
  tiers: TierDraft[];
  rebateMin: string;
  rebateCoupons: RebateDraft[];
  rebateIntegral: string;
  priority: string;
  scopes: CouponScope[];
  planIds: number[];
  packageIds: number[];
  audienceKind: AudienceKind;
  newUserDays: string;
  stackWithCoupon: boolean;
  startsAt: string;
  endsAt: string;
  perUserLimit: string;
  totalLimit: string;
  budget: string;
};

const zeroish = (value?: string | null) => !value || /^0+(\.0*)?$/.test(value.trim());

function seed(p?: Promotion | null): Form {
  const rule = p?.rule;
  return {
    name: p?.name ?? "",
    description: p?.description ?? "",
    // 服务端读出时把空角标补成了默认值；与默认一致的视为没设置，规则改了角标跟着变
    tag: p && p.tag !== defaultPromotionTag(p) ? p.tag : "",
    theme: p?.theme ?? "red",
    kind: p?.kind ?? "discount",
    discountMode: rule?.discountMode ?? "percent",
    fold: rule?.rate ? String(rule.rate / 10) : "8",
    amount: rule && !zeroish(rule.amount) ? rule.amount : "",
    maxDiscount: rule && !zeroish(rule.maxDiscount) ? rule.maxDiscount : "",
    tiers: rule?.tiers?.length
      ? rule.tiers.map((tier, index) => ({ key: index + 1, minAmount: tier.minAmount, off: tier.off }))
      : [{ key: 1, minAmount: "100", off: "10" }],
    rebateMin: rule && !zeroish(rule.minAmount) ? rule.minAmount : "",
    rebateCoupons: (rule?.rebateCoupons ?? []).map((item, index) => ({ key: index + 1, templateId: item.templateId, count: String(item.count) })),
    rebateIntegral: rule?.rebateIntegral ? String(rule.rebateIntegral) : "",
    priority: String(p?.priority ?? 0),
    scopes: p?.scopes?.length ? p.scopes : ["vip_purchase"],
    planIds: p?.planIds ?? [],
    packageIds: p?.packageIds ?? [],
    audienceKind: p?.audience?.kind ?? "all",
    newUserDays: String(p?.audience?.newUserDays || 7),
    stackWithCoupon: p?.stackWithCoupon ?? true,
    startsAt: toLocalInputValue(p?.startsAt ?? null),
    endsAt: toLocalInputValue(p?.endsAt ?? null),
    perUserLimit: p && p.perUserLimit > 0 ? String(p.perUserLimit) : "",
    totalLimit: p && p.totalLimit > 0 ? String(p.totalLimit) : "",
    budget: p && !zeroish(p.budget) ? p.budget : ""
  };
}

function toPayload(form: Form): PromotionPayload {
  const rebate = form.kind === "rebate";
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    tag: form.tag.trim(),
    theme: form.theme,
    kind: form.kind,
    rule: {
      discountMode: form.kind === "discount" ? form.discountMode : undefined,
      rate: form.kind === "discount" && form.discountMode === "percent" ? Math.round((Number(form.fold) || 0) * 10) : 0,
      amount: form.kind === "discount" && form.discountMode !== "percent" ? form.amount.trim() || "0" : "0",
      maxDiscount: form.kind === "discount" && form.discountMode === "percent" ? form.maxDiscount.trim() || "0" : "0",
      tiers: form.kind === "threshold" ? form.tiers.map((tier) => ({ minAmount: tier.minAmount.trim() || "0", off: tier.off.trim() || "0" })) : [],
      minAmount: rebate ? form.rebateMin.trim() || "0" : "0",
      rebateCoupons: rebate
        ? form.rebateCoupons.filter((item) => item.templateId > 0).map((item) => ({ templateId: item.templateId, count: Number(item.count) || 1 }))
        : [],
      rebateIntegral: rebate ? Number(form.rebateIntegral) || 0 : 0
    },
    priority: Number(form.priority) || 0,
    scopes: form.scopes,
    planIds: form.scopes.includes("vip_purchase") ? form.planIds : [],
    packageIds: rebate && form.scopes.includes("wallet_recharge") ? form.packageIds : [],
    audience: { kind: form.audienceKind, newUserDays: form.audienceKind === "new_user" ? Number(form.newUserDays) || 0 : 0 },
    stackWithCoupon: rebate ? true : form.stackWithCoupon,
    startsAt: fromLocalInputValue(form.startsAt) ?? null,
    endsAt: fromLocalInputValue(form.endsAt) ?? null,
    perUserLimit: Number(form.perUserLimit) || 0,
    totalLimit: Number(form.totalLimit) || 0,
    budget: rebate ? "0" : form.budget.trim() || "0"
  };
}

/**
 * 新建 / 编辑一期活动。三种类型：限时折扣（打折 / 立减 / 特价）、满减（最多 5 档）、支付返券（付款满额后发券、发积分，不改价格）。
 * 一笔订单只用一个价格活动（取优惠最多的），返券活动另算；已有订单参与的活动不能改类型（42322）。
 */
export function PromotionDialog({
  appKey,
  open,
  onOpenChange,
  promotion
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  promotion?: Promotion | null;
}) {
  const [form, setForm] = useState<Form>(() => seed(promotion));
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => setForm((prev) => ({ ...prev, [key]: value }));
  const save = useSavePromotionMutation(appKey);
  const plans = (useAdminVipPlansQuery(appKey).data ?? []).filter((plan) => plan.kind !== "trial");
  const packages = useWalletPackagesQuery(appKey).data ?? [];
  const kindLocked = (promotion?.usedCount ?? 0) > 0;
  const scopeChoices = form.kind === "rebate" ? REBATE_SCOPES : PRICE_SCOPES;
  const payload = toPayload(form);
  const fixedPrice = form.kind === "discount" && form.discountMode === "fixed_price";

  const setKind = (kind: PromotionKind) => {
    const allowed = kind === "rebate" ? REBATE_SCOPES : PRICE_SCOPES;
    setForm((prev) => ({ ...prev, kind, scopes: prev.scopes.filter((scope) => allowed.includes(scope)) }));
  };
  const toggleScope = (scope: CouponScope, checked: boolean) =>
    patch("scopes", checked ? scopeChoices.filter((item) => item === scope || form.scopes.includes(item)) : form.scopes.filter((item) => item !== scope));
  const toggleId = (key: "planIds" | "packageIds", id: number, checked: boolean) =>
    patch(key, checked ? [...form[key], id].sort((a, b) => a - b) : form[key].filter((item) => item !== id));
  const nextKey = (items: Array<{ key: number }>) => items.reduce((max, item) => Math.max(max, item.key), 0) + 1;

  const submit = async () => {
    if (!payload.name) return toast.error("请填写活动名称");
    if (payload.scopes.length === 0) return toast.error("请至少选择一种适用订单");
    if (fixedPrice && (payload.scopes.length !== 1 || payload.scopes[0] !== "vip_purchase" || payload.planIds.length === 0)) {
      return toast.error("特价只能用于指定的会员套餐：适用订单只选会员套餐，并勾选套餐");
    }
    try {
      await save.mutateAsync({ id: promotion?.id, payload });
      toast.success(promotion ? "已保存" : "已创建（草稿），上线后生效");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-hidden p-0 sm:max-w-4xl">
        <DialogHeader className="px-6 pt-6">
          <DialogTitle>{promotion ? "编辑优惠活动" : "新建优惠活动"}</DialogTitle>
          <DialogDescription>活动自动生效，不用领；一笔订单只用优惠最多的一个价格活动，返券活动另算。</DialogDescription>
        </DialogHeader>
        <div className="grid max-h-[calc(92vh-11rem)] gap-6 overflow-y-auto px-6 pb-2 md:grid-cols-[minmax(0,1fr)_280px]">
          <div className="min-w-0 space-y-5">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_160px]">
              <Field label="活动名称">
                <Input value={form.name} maxLength={32} placeholder="例如：双十二会员 8 折" onChange={(e) => patch("name", e.target.value)} />
              </Field>
              <Field label="角标" hint="空为按规则生成">
                <Input value={form.tag} maxLength={12} placeholder={defaultPromotionTag(payload)} onChange={(e) => patch("tag", e.target.value)} />
              </Field>
            </div>
            <Field label="说明" hint="展示在活动页，可不填">
              <Textarea rows={2} value={form.description} maxLength={200} onChange={(e) => patch("description", e.target.value)} />
            </Field>

            <Field label="活动类型" hint={kindLocked ? "已有订单参与，类型不能再改" : undefined}>
              <ToggleGroup type="single" variant="outline" size="sm" value={form.kind} disabled={kindLocked} onValueChange={(value) => value && setKind(value as PromotionKind)}>
                {(Object.keys(PROMOTION_KIND_LABEL) as PromotionKind[]).map((kind) => (
                  <ToggleGroupItem key={kind} value={kind} className="px-4 text-xs">
                    {PROMOTION_KIND_LABEL[kind]}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>

            {form.kind === "discount" ? (
              <div className="space-y-4 rounded-xl border p-3">
                <ToggleGroup type="single" variant="outline" size="sm" value={form.discountMode} onValueChange={(value) => value && patch("discountMode", value as DiscountMode)}>
                  {(Object.keys(DISCOUNT_MODE_LABEL) as DiscountMode[]).map((mode) => (
                    <ToggleGroupItem key={mode} value={mode} className="px-4 text-xs">
                      {DISCOUNT_MODE_LABEL[mode]}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
                {form.discountMode === "percent" ? (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="折扣" hint="0.1–9.9 折">
                      <Input inputMode="decimal" value={form.fold} onChange={(e) => patch("fold", e.target.value)} />
                    </Field>
                    <Field label="最多减" hint="元，空为不封顶">
                      <Input inputMode="decimal" value={form.maxDiscount} placeholder="不封顶" onChange={(e) => patch("maxDiscount", e.target.value)} />
                    </Field>
                  </div>
                ) : (
                  <Field label={form.discountMode === "fixed_price" ? "特价" : "立减金额"} hint={form.discountMode === "fixed_price" ? "元，原价更高时按这个价卖" : "元"}>
                    <Input inputMode="decimal" className="sm:w-48" value={form.amount} onChange={(e) => patch("amount", e.target.value)} />
                  </Field>
                )}
              </div>
            ) : null}

            {form.kind === "threshold" ? (
              <div className="space-y-2 rounded-xl border p-3">
                <div className="text-xs text-muted-foreground">按订单原价取能达到的最高一档；门槛越高减得要越多。</div>
                {form.tiers.map((tier, index) => (
                  <div key={tier.key} className="flex items-center gap-2 text-xs">
                    <span className="w-10 shrink-0 text-muted-foreground">第 {index + 1} 档</span>
                    <span>满</span>
                    <Input
                      inputMode="decimal"
                      className="h-8 w-24"
                      value={tier.minAmount}
                      onChange={(e) => patch("tiers", form.tiers.map((t) => (t.key === tier.key ? { ...t, minAmount: e.target.value } : t)))}
                    />
                    <span>减</span>
                    <Input
                      inputMode="decimal"
                      className="h-8 w-24"
                      value={tier.off}
                      onChange={(e) => patch("tiers", form.tiers.map((t) => (t.key === tier.key ? { ...t, off: e.target.value } : t)))}
                    />
                    <span>元</span>
                    {form.tiers.length > 1 ? (
                      <Button size="icon" variant="ghost" className="size-7" aria-label="删除这一档" onClick={() => patch("tiers", form.tiers.filter((t) => t.key !== tier.key))}>
                        <Trash2 className="size-3.5" />
                      </Button>
                    ) : null}
                  </div>
                ))}
                {form.tiers.length < 5 ? (
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-8 border-dashed text-xs"
                    onClick={() => patch("tiers", [...form.tiers, { key: nextKey(form.tiers), minAmount: "", off: "" }])}
                  >
                    <Plus className="size-3.5" />
                    加一档
                  </Button>
                ) : null}
              </div>
            ) : null}

            {form.kind === "rebate" ? (
              <div className="space-y-4 rounded-xl border p-3">
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="实付门槛" hint="元，空为付款即返">
                    <Input inputMode="decimal" value={form.rebateMin} placeholder="付款即返" onChange={(e) => patch("rebateMin", e.target.value)} />
                  </Field>
                  <Field label="返积分" hint="空为不返">
                    <Input type="number" min={0} value={form.rebateIntegral} placeholder="不返" onChange={(e) => patch("rebateIntegral", e.target.value)} />
                  </Field>
                </div>
                <Field label="返券" hint="最多 5 种，每种 1–20 张">
                  <div className="space-y-2">
                    {form.rebateCoupons.map((item) => (
                      <div key={item.key} className="flex flex-wrap items-center gap-2">
                        <CouponPicker
                          appKey={appKey}
                          value={item.templateId}
                          excludeIds={form.rebateCoupons.map((other) => other.templateId)}
                          onChange={(id) => patch("rebateCoupons", form.rebateCoupons.map((r) => (r.key === item.key ? { ...r, templateId: id } : r)))}
                        />
                        <Input
                          type="number"
                          min={1}
                          max={20}
                          className="h-8 w-20"
                          value={item.count}
                          onChange={(e) => patch("rebateCoupons", form.rebateCoupons.map((r) => (r.key === item.key ? { ...r, count: e.target.value } : r)))}
                        />
                        <span className="text-xs text-muted-foreground">张</span>
                        <Button size="icon" variant="ghost" className="size-7" aria-label="移除" onClick={() => patch("rebateCoupons", form.rebateCoupons.filter((r) => r.key !== item.key))}>
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    ))}
                    {form.rebateCoupons.length < 5 ? (
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-8 border-dashed text-xs"
                        onClick={() => patch("rebateCoupons", [...form.rebateCoupons, { key: nextKey(form.rebateCoupons), templateId: 0, count: "1" }])}
                      >
                        <Plus className="size-3.5" />
                        添加返的券
                      </Button>
                    ) : null}
                  </div>
                </Field>
                <p className="text-[11px] text-muted-foreground">返的券在付款结算时发放（券暂停中也能发）；全额退款时收回还没用的券与返的积分。</p>
              </div>
            ) : null}

            <Field label="适用订单">
              <div className="flex flex-wrap gap-2">
                {scopeChoices.map((scope) => (
                  <label
                    key={scope}
                    className={cn("flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-1.5 text-xs", form.scopes.includes(scope) ? "border-primary/40 bg-primary/5" : "border-border")}
                  >
                    <Checkbox checked={form.scopes.includes(scope)} onCheckedChange={(checked) => toggleScope(scope, checked === true)} />
                    {SCOPE_LABEL[scope]}
                  </label>
                ))}
              </div>
              {form.kind !== "rebate" ? <p className="text-[11px] text-muted-foreground">余额充值不能打折，只能参加支付返券活动。</p> : null}
            </Field>

            {form.scopes.includes("vip_purchase") ? (
              <Field label="限定套餐" hint={form.planIds.length ? `已选 ${form.planIds.length} 个` : fixedPrice ? "特价必须指定套餐" : "不选即全部套餐"}>
                <div className="grid max-h-40 gap-1.5 overflow-y-auto rounded-lg border p-2 sm:grid-cols-2">
                  {plans.length === 0 ? <p className="text-xs text-muted-foreground">暂无付费套餐</p> : null}
                  {plans.map((plan) => (
                    <label key={plan.id} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-accent/40">
                      <Checkbox checked={form.planIds.includes(plan.id)} onCheckedChange={(checked) => toggleId("planIds", plan.id, checked === true)} />
                      <span className="min-w-0 flex-1 truncate">
                        {plan.name}
                        <span className="ml-1 text-muted-foreground">
                          {vipPlanTerm(plan)} · {plan.price}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              </Field>
            ) : null}

            {form.kind === "rebate" && form.scopes.includes("wallet_recharge") ? (
              <Field label="限定充值档位" hint={form.packageIds.length ? `已选 ${form.packageIds.length} 个` : "不选即全部档位与自选金额"}>
                <div className="grid max-h-40 gap-1.5 overflow-y-auto rounded-lg border p-2 sm:grid-cols-2">
                  {packages.length === 0 ? <p className="text-xs text-muted-foreground">暂无充值档位</p> : null}
                  {packages.map((item) => (
                    <label key={item.id} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-accent/40">
                      <Checkbox checked={form.packageIds.includes(item.id)} onCheckedChange={(checked) => toggleId("packageIds", item.id, checked === true)} />
                      <span className="min-w-0 flex-1 truncate">
                        {item.title}
                        <span className="ml-1 text-muted-foreground">{item.amount} 元</span>
                      </span>
                    </label>
                  ))}
                </div>
              </Field>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="开始时间" hint="空为上线即开始">
                <Input type="datetime-local" value={form.startsAt} onChange={(e) => patch("startsAt", e.target.value)} />
              </Field>
              <Field label="结束时间" hint="空为不结束">
                <Input type="datetime-local" value={form.endsAt} onChange={(e) => patch("endsAt", e.target.value)} />
              </Field>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="参与人群">
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

            <div className="grid gap-4 sm:grid-cols-3">
              <Field label="每人参与" hint="次，空为不限">
                <Input type="number" min={0} value={form.perUserLimit} placeholder="不限" onChange={(e) => patch("perUserLimit", e.target.value)} />
              </Field>
              <Field label="总名额" hint={promotion?.usedCount ? `已用 ${promotion.usedCount}` : "次，空为不限"}>
                <Input type="number" min={0} value={form.totalLimit} placeholder="不限" onChange={(e) => patch("totalLimit", e.target.value)} />
              </Field>
              {form.kind !== "rebate" ? (
                <Field label="优惠预算" hint={promotion ? `已让利 ${promotion.discountTotal}` : "元，空为不限"}>
                  <Input inputMode="decimal" value={form.budget} placeholder="不限" onChange={(e) => patch("budget", e.target.value)} />
                </Field>
              ) : null}
            </div>
            <Field label="优先级" hint="优惠相同时优先级高的先用；返券活动按优先级取一个">
              <Input type="number" className="sm:w-32" value={form.priority} onChange={(e) => patch("priority", e.target.value)} />
            </Field>

            {form.kind !== "rebate" ? (
              <SwitchRow
                label="可与优惠券同享"
                hint="开启后活动价之后还能用券（券也要允许同享才叠加）；关闭时用券就不享受这个活动"
                checked={form.stackWithCoupon}
                onChange={(value) => patch("stackWithCoupon", value)}
              />
            ) : null}
          </div>

          <aside className="space-y-4 md:sticky md:top-0 md:self-start">
            <Field label="色调">
              <ThemePicker value={form.theme} onChange={(theme) => patch("theme", theme)} />
            </Field>
            <Field label="预览">
              <div style={themeStyle(form.theme)} className={cn(THEME_VAR, "space-y-2 rounded-xl border border-[color-mix(in_oklab,var(--coupon)_35%,transparent)] bg-[color-mix(in_oklab,var(--coupon)_8%,var(--card))] p-3")}>
                <span className="inline-flex rounded-md bg-[var(--coupon)] px-2 py-0.5 text-[11px] font-semibold text-white">
                  {form.tag.trim() || defaultPromotionTag(payload)}
                </span>
                <div className="text-sm font-semibold">{form.name || "未命名的活动"}</div>
                <div className="text-[11px] text-muted-foreground">{promotionRuleText(payload)}</div>
                <div className="text-[11px] text-muted-foreground">{payload.scopes.map((scope) => SCOPE_LABEL[scope]).join("、") || "未选适用订单"}</div>
              </div>
            </Field>
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
