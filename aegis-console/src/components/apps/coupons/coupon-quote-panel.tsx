"use client";

import { useState } from "react";
import { Calculator, CheckCircle2, Gift, Info, Loader2, Sparkles, Star, XCircle } from "lucide-react";
import { toast } from "sonner";
import { formatMoney } from "@/components/commerce/commerce-format";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import { Section, StateBlock } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ApiError } from "@/lib/api-client";
import type { CouponQuote, CouponScope } from "@/lib/api/coupons";
import { useWalletPackagesQuery } from "@/lib/commerce-hooks";
import { useCouponQuoteMutation } from "@/lib/coupon-hooks";
import { useAdminVipPlansQuery } from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";
import { couponNameOf } from "@/lib/coupon-names";
import { REASON_LABEL, REBATE_SCOPES, SCOPE_LABEL, THEME_VAR, themeStyle } from "./coupon-shared";
import { Field } from "./coupon-template-dialog";

type Mode = "auto" | "none" | "pick";

/**
 * 试算台：替某个用户把一笔订单过一遍定价引擎（只算不写），看命中哪个活动、手里每张券能不能用、
 * 为什么不能用、最后要付多少、能不能拿到返券。排查「为什么我的券用不了」时用它，比翻规则快。
 */
export function CouponQuotePanel({ appKey }: { appKey: string }) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [purpose, setPurpose] = useState<CouponScope>("vip_purchase");
  const [planId, setPlanId] = useState(0);
  const [packageId, setPackageId] = useState(0);
  const [amount, setAmount] = useState("");
  const [mode, setMode] = useState<Mode>("auto");
  const [couponId, setCouponId] = useState(0);
  const [quote, setQuote] = useState<CouponQuote | null>(null);
  const run = useCouponQuoteMutation(appKey);
  const plans = (useAdminVipPlansQuery(appKey).data ?? []).filter((plan) => plan.kind !== "trial" && plan.isActive);
  const packages = useWalletPackagesQuery(purpose === "wallet_recharge" ? appKey : null).data ?? [];

  const calculate = async (override?: { mode: Mode; couponId: number }) => {
    if (!user) return toast.error("请先选择用户");
    const m = override?.mode ?? mode;
    const cid = override?.couponId ?? couponId;
    try {
      const result = await run.mutateAsync({
        userId: user.id,
        purpose: purpose === "goods" ? "" : purpose,
        planId: purpose === "vip_purchase" ? planId || undefined : undefined,
        packageId: purpose === "wallet_recharge" ? packageId || undefined : undefined,
        amount: amount.trim() || undefined,
        couponMode: m === "pick" ? undefined : m,
        couponId: m === "pick" ? cid || undefined : undefined
      });
      setQuote(result);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "试算失败");
    }
  };

  const needsAmount = purpose === "goods" || purpose === "integral_purchase" || (purpose === "wallet_recharge" && !packageId);

  return (
    <div className="grid gap-4 lg:grid-cols-[340px_minmax(0,1fr)]">
      <Section title="试算条件" icon={Calculator} tone="primary">
        <div className="space-y-4">
          <Field label="用户">
            <UserPicker appKey={appKey} value={user} onChange={setUser} />
          </Field>
          <Field label="订单类型">
            <Select value={purpose} onValueChange={(value) => setPurpose(value as CouponScope)}>
              <SelectTrigger className="h-9 w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {REBATE_SCOPES.map((scope) => (
                  <SelectItem key={scope} value={scope}>
                    {SCOPE_LABEL[scope]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {purpose === "vip_purchase" ? (
            <Field label="套餐">
              <Select value={planId ? String(planId) : ""} onValueChange={(value) => setPlanId(Number(value))}>
                <SelectTrigger className="h-9 w-full">
                  <SelectValue placeholder="选择在售套餐" />
                </SelectTrigger>
                <SelectContent position="popper">
                  {plans.map((plan) => (
                    <SelectItem key={plan.id} value={String(plan.id)}>
                      {plan.name}（{vipPlanTerm(plan)} · {plan.price}）
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          {purpose === "wallet_recharge" ? (
            <Field label="充值档位" hint="不选即自选金额">
              <Select value={packageId ? String(packageId) : "0"} onValueChange={(value) => setPackageId(Number(value))}>
                <SelectTrigger className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectItem value="0">自选金额</SelectItem>
                  {packages.map((item) => (
                    <SelectItem key={item.id} value={String(item.id)}>
                      {item.title}（{item.amount} 元）
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          {needsAmount ? (
            <Field label="原价" hint="元">
              <Input inputMode="decimal" value={amount} placeholder="例如 30" onChange={(e) => setAmount(e.target.value)} />
            </Field>
          ) : null}
          {purpose !== "wallet_recharge" ? (
            <Field label="用券">
              <Select value={mode} onValueChange={(value) => setMode(value as Mode)}>
                <SelectTrigger className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">自动用最优的券</SelectItem>
                  <SelectItem value="none">不用券</SelectItem>
                  <SelectItem value="pick" disabled={!quote?.coupons.length}>
                    指定一张（先算一次再在右侧点选）
                  </SelectItem>
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          <Button className="w-full" onClick={() => void calculate()} disabled={run.isPending || !user || (purpose === "vip_purchase" && !planId)}>
            {run.isPending ? <Loader2 className="animate-spin" /> : <Calculator />}
            试算
          </Button>
        </div>
      </Section>

      {quote ? (
        <QuoteResult
          quote={quote}
          selectedCouponId={mode === "pick" ? couponId : quote.coupon?.id ?? 0}
          onPick={(id) => {
            setMode("pick");
            setCouponId(id);
            void calculate({ mode: "pick", couponId: id });
          }}
        />
      ) : (
        <Section title="结果" icon={Sparkles} tone="neutral">
          <StateBlock compact icon={Calculator} title="选用户与订单后试算" description="结果与这个用户此刻真的下单时一致：活动名额、预算、人群与券的状态都按实时数据算。" />
        </Section>
      )}
    </div>
  );
}

function QuoteResult({ quote, selectedCouponId, onPick }: { quote: CouponQuote; selectedCouponId: number; onPick: (id: number) => void }) {
  return (
    <div className="space-y-4">
      <Section title="价格" icon={Sparkles} tone="success">
        <div className="space-y-2 text-sm">
          <Line label="原价" value={formatMoney(quote.originalAmount)} />
          <Line
            label={quote.promotion ? `活动 · ${quote.promotion.name}` : "活动"}
            value={quote.promotion ? `−${formatMoney(quote.promotionDiscount)}` : "未命中"}
            tone={quote.promotion ? "minus" : "muted"}
            badge={quote.promotion?.tag}
            theme={quote.promotion?.theme}
          />
          <Line
            label={quote.coupon ? `券 · ${quote.coupon.name || quote.coupon.face}` : "券"}
            value={quote.coupon ? `−${formatMoney(quote.couponDiscount)}` : "未用券"}
            tone={quote.coupon ? "minus" : "muted"}
          />
          <div className="flex items-baseline justify-between border-t pt-2">
            <span className="font-medium">应付</span>
            <span className="font-mono text-2xl font-semibold tabular-nums">{formatMoney(quote.payableAmount)}</span>
          </div>
          {quote.notes.map((note) => (
            <p key={note} className="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-300">
              <Info className="mt-0.5 size-3.5 shrink-0" />
              {note}
            </p>
          ))}
          {quote.rebate ? (
            <div className={cn("flex items-start gap-2 rounded-lg border px-3 py-2 text-xs", quote.rebate.eligible ? "border-emerald-500/30 bg-emerald-500/5" : "border-dashed")}>
              <Gift className="mt-0.5 size-3.5 shrink-0" />
              <div className="min-w-0">
                <div className="font-medium">
                  {quote.rebate.name}
                  <span className="ml-1.5 text-muted-foreground">{quote.rebate.tag}</span>
                </div>
                <div className="text-muted-foreground">
                  {quote.rebate.eligible ? "付款后发放：" : `再付 ${formatMoney(quote.rebate.gap)} 元可得：`}
                  {[
                    ...(quote.rebate.coupons ?? []).map((item) => `${item.templateName || couponNameOf(item.templateId) || `优惠券 #${item.templateId}`} ${item.count} 张`),
                    ...(quote.rebate.integral ? [`积分 ${quote.rebate.integral}`] : [])
                  ].join("、")}
                </div>
              </div>
            </div>
          ) : null}
        </div>
      </Section>

      <Section title={`手里的券（${quote.coupons.length}）`} description="可用与被订单占用、还没过期的券；点一张按它重新试算" icon={Star} tone="violet" flush>
        {quote.coupons.length === 0 ? (
          <StateBlock compact title="这个用户没有可用的券" />
        ) : (
          <ul className="divide-y">
            {quote.coupons.map((option) => (
              <li key={option.coupon.id}>
                <button
                  type="button"
                  disabled={!option.usable}
                  onClick={() => onPick(option.coupon.id)}
                  className={cn(
                    "flex w-full items-center gap-3 px-4 py-2.5 text-left text-xs transition-colors sm:px-5",
                    option.usable ? "hover:bg-accent/40" : "cursor-not-allowed opacity-70",
                    selectedCouponId === option.coupon.id && "bg-primary/5"
                  )}
                >
                  <span style={themeStyle(option.coupon.template?.theme)} className={cn(THEME_VAR, "h-8 w-1 shrink-0 rounded-full bg-[var(--coupon)]")} />
                  {option.usable ? <CheckCircle2 className="size-4 shrink-0 text-emerald-600" /> : <XCircle className="size-4 shrink-0 text-muted-foreground" />}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate font-medium">{option.coupon.template?.name ?? `#${option.coupon.templateId}`}</span>
                      {option.best ? <Badge className="h-4 px-1 text-[10px]">最优</Badge> : null}
                      {selectedCouponId === option.coupon.id ? (
                        <Badge variant="outline" className="h-4 px-1 text-[10px]">
                          已选
                        </Badge>
                      ) : null}
                    </div>
                    <div className="truncate text-muted-foreground">
                      {option.face}
                      {option.usable
                        ? option.dropsPromotion
                          ? " · 用它就不享受当前活动"
                          : ""
                        : ` · ${option.reason || REASON_LABEL[option.reasonCode ?? ""] || "不可用"}`}
                    </div>
                  </div>
                  {option.usable ? (
                    <div className="shrink-0 text-right">
                      <div className="font-mono text-rose-600">−{formatMoney(option.totalDiscount)}</div>
                      <div className="font-mono text-[11px] text-muted-foreground">付 {formatMoney(option.payable)}</div>
                    </div>
                  ) : null}
                </button>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}

function Line({
  label,
  value,
  tone,
  badge,
  theme
}: {
  label: string;
  value: string;
  tone?: "minus" | "muted";
  badge?: string;
  theme?: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
        <span className="truncate">{label}</span>
        {badge ? (
          <span style={themeStyle(theme)} className={cn(THEME_VAR, "shrink-0 rounded bg-[var(--coupon)] px-1.5 py-px text-[10px] font-semibold text-white")}>
            {badge}
          </span>
        ) : null}
      </span>
      <span className={cn("shrink-0 font-mono tabular-nums", tone === "minus" && "text-rose-600", tone === "muted" && "text-muted-foreground")}>{value}</span>
    </div>
  );
}
