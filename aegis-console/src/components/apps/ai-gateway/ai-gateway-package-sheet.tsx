"use client";

import { useState } from "react";
import { CalendarRange, Coins, Loader2, Palette, Save, ShieldCheck, Tag } from "lucide-react";
import { toast } from "sonner";
import { SheetBlock, ToggleTile } from "@/components/ai/ai-sheet-parts";
import { PACKAGE_TONES, PACKAGE_TONE_KEYS, formatYuan, trimDecimal, useNow } from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import {
  GUEST,
  PackageCard,
  quotePackage,
  type QuoteIdentity
} from "@/components/apps/ai-gateway/ai-gateway-package-card";
import type { TierOption } from "@/components/apps/ai-gateway/ai-gateway-member-tiers";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { useSaveAIGatewayPackageMutation } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayPackage, AIGatewayPackagePayload } from "@/lib/api/ai-gateway";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { cn } from "@/lib/utils";

const BADGES = ["热销", "推荐", "超值", "限时", "新人"];
const VALIDITY = [7, 30, 90, 180, 365];
const MONEY_RE = /^\d+(\.\d{1,2})?$/;
const POINTS_RE = /^\d+(\.\d{1,6})?$/;
const INT_RE = /^\d+$/;

/** 编辑中的额度包：数字按字符串存，时间按 datetime-local 的值存。 */
type PackageDraft = {
  name: string;
  subtitle: string;
  description: string;
  badge: string;
  tone: AIGatewayPackagePayload["tone"];
  featured: boolean;
  points: string;
  bonusPoints: string;
  price: string;
  originalPrice: string;
  validityDays: string;
  /** all / vip / tiers（指定档位：audience = vip 且 tierIds 非空）。 */
  audience: "all" | "vip" | "tiers";
  tierIds: number[];
  memberDiscount: boolean;
  perUserLimit: string;
  monthlyLimit: string;
  stock: string;
  startsAt: string;
  endsAt: string;
  enabled: boolean;
  sortOrder: string;
};

export type PackageSheetTarget = { mode: "create"; sortOrder: number } | { mode: "edit"; pkg: AIGatewayPackage };

function toDraft(target: PackageSheetTarget): PackageDraft {
  if (target.mode === "create") {
    return {
      name: "",
      subtitle: "",
      description: "",
      badge: "",
      tone: "blue",
      featured: false,
      points: "10000",
      bonusPoints: "0",
      price: "10",
      originalPrice: "0",
      validityDays: "30",
      audience: "all",
      tierIds: [],
      memberDiscount: true,
      perUserLimit: "0",
      monthlyLimit: "0",
      stock: "0",
      startsAt: "",
      endsAt: "",
      enabled: false,
      sortOrder: String(target.sortOrder)
    };
  }
  const pkg = target.pkg;
  return {
    name: pkg.name,
    subtitle: pkg.subtitle ?? "",
    description: pkg.description ?? "",
    badge: pkg.badge ?? "",
    tone: pkg.tone ?? "blue",
    featured: pkg.featured,
    points: trimDecimal(pkg.points),
    bonusPoints: trimDecimal(pkg.bonusPoints),
    price: trimDecimal(pkg.price),
    originalPrice: trimDecimal(pkg.originalPrice),
    validityDays: String(pkg.validityDays ?? 0),
    audience: (pkg.tierIds ?? []).length > 0 ? "tiers" : pkg.audience,
    tierIds: pkg.tierIds ?? [],
    memberDiscount: pkg.memberDiscount,
    perUserLimit: String(pkg.perUserLimit ?? 0),
    monthlyLimit: String(pkg.monthlyLimit ?? 0),
    stock: String(pkg.stock ?? 0),
    startsAt: pkg.startsAt ? toLocalInputValue(pkg.startsAt) : "",
    endsAt: pkg.endsAt ? toLocalInputValue(pkg.endsAt) : "",
    enabled: pkg.enabled,
    sortOrder: String(pkg.sortOrder ?? 0)
  };
}

function toPayload(draft: PackageDraft): AIGatewayPackagePayload {
  return {
    name: draft.name.trim(),
    subtitle: draft.subtitle.trim(),
    description: draft.description.trim(),
    badge: draft.badge.trim(),
    tone: draft.tone,
    featured: draft.featured,
    points: draft.points.trim() || "0",
    bonusPoints: draft.bonusPoints.trim() || "0",
    price: draft.price.trim() || "0",
    originalPrice: draft.originalPrice.trim() || "0",
    validityDays: Number(draft.validityDays) || 0,
    audience: draft.audience === "all" ? "all" : "vip",
    tierIds: draft.audience === "tiers" ? draft.tierIds : [],
    memberDiscount: draft.memberDiscount,
    perUserLimit: Number(draft.perUserLimit) || 0,
    monthlyLimit: Number(draft.monthlyLimit) || 0,
    stock: Number(draft.stock) || 0,
    startsAt: fromLocalInputValue(draft.startsAt) ?? null,
    endsAt: fromLocalInputValue(draft.endsAt) ?? null,
    enabled: draft.enabled,
    sortOrder: Number(draft.sortOrder) || 0
  };
}

function validate(draft: PackageDraft): string | null {
  if (!draft.name.trim()) return "请填写名称";
  if (!POINTS_RE.test(draft.points.trim()) || Number(draft.points) <= 0) return "额度需为正数";
  if (!POINTS_RE.test(draft.bonusPoints.trim() || "0")) return "赠送额度需为非负数";
  if (!MONEY_RE.test(draft.price.trim())) return "售价需为非负数，最多 2 位小数";
  if (!MONEY_RE.test(draft.originalPrice.trim() || "0")) return "划线价需为非负数，最多 2 位小数";
  if (Number(draft.originalPrice) > 0 && Number(draft.originalPrice) <= Number(draft.price)) return "划线价应高于售价（不显示就填 0）";
  for (const [label, value] of [["有效天数", draft.validityDays], ["每人限购", draft.perUserLimit], ["每月限购", draft.monthlyLimit], ["库存", draft.stock], ["排序", draft.sortOrder]] as const) {
    if (!INT_RE.test(value.trim() || "0")) return `${label}需为非负整数`;
  }
  if (draft.audience === "tiers" && draft.tierIds.length === 0) return "选择可购买的会员档位";
  const starts = fromLocalInputValue(draft.startsAt);
  const ends = fromLocalInputValue(draft.endsAt);
  if (starts && ends && starts >= ends) return "售卖结束时间需晚于开始时间";
  return null;
}

export function AIGatewayPackageSheet({
  appKey,
  target,
  tiers,
  identities,
  tierName,
  onClose
}: {
  appKey: string;
  target: PackageSheetTarget | null;
  tiers: TierOption[];
  identities: QuoteIdentity[];
  tierName: (id: number) => string;
  onClose: () => void;
}) {
  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-5xl">
        {target ? (
          <SheetBody
            key={target.mode === "edit" ? target.pkg.id : "new"}
            appKey={appKey}
            target={target}
            tiers={tiers}
            identities={identities}
            tierName={tierName}
            onClose={onClose}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function SheetBody({
  appKey,
  target,
  tiers,
  identities,
  tierName,
  onClose
}: {
  appKey: string;
  target: PackageSheetTarget;
  tiers: TierOption[];
  identities: QuoteIdentity[];
  tierName: (id: number) => string;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState<PackageDraft>(() => toDraft(target));
  const [identityKey, setIdentityKey] = useState(GUEST.key);
  const saveMutation = useSaveAIGatewayPackageMutation(appKey);
  const now = useNow(60_000);
  const patch = (changes: Partial<PackageDraft>) => setDraft((current) => ({ ...current, ...changes }));
  const payload = toPayload(draft);
  const identity = identities.find((item) => item.key === identityKey) ?? GUEST;
  const sold = target.mode === "edit" ? target.pkg.sold : 0;

  const save = async () => {
    const error = validate(draft);
    if (error) {
      toast.error(error);
      return;
    }
    try {
      await saveMutation.mutateAsync({ id: target.mode === "edit" ? target.pkg.id : undefined, payload });
      toast.success(target.mode === "edit" ? "额度包已保存" : draft.enabled ? "额度包已上架" : "额度包已创建（未上架）");
      onClose();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  return (
    <>
      <SheetHeader className="shrink-0 border-b px-5 py-4 sm:px-6">
        <SheetTitle>{target.mode === "edit" ? `编辑「${target.pkg.name}」` : "新建额度包"}</SheetTitle>
        <SheetDescription>用户用钱包余额购买，得到一个额度批次；改价只影响之后的订单</SheetDescription>
      </SheetHeader>
      <div className="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[minmax(0,1fr)_340px] lg:overflow-hidden">
        <div className="space-y-4 p-5 sm:p-6 lg:overflow-y-auto">
          <SheetBlock icon={<Tag className="size-3.5" />} title="展示">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="名称">
                <Input className="h-8 text-xs" maxLength={64} value={draft.name} onChange={(event) => patch({ name: event.target.value })} />
              </Field>
              <Field label="副标题">
                <Input className="h-8 text-xs" maxLength={64} placeholder="日常对话够用一个月" value={draft.subtitle}
                  onChange={(event) => patch({ subtitle: event.target.value })} />
              </Field>
              <Field label="说明" className="sm:col-span-2">
                <Textarea rows={2} maxLength={500} className="text-xs" value={draft.description} onChange={(event) => patch({ description: event.target.value })} />
              </Field>
              <Field label="角标">
                <div className="flex flex-wrap items-center gap-1.5">
                  <Input className="h-8 w-24 text-xs" maxLength={8} value={draft.badge} onChange={(event) => patch({ badge: event.target.value })} />
                  {BADGES.map((badge) => (
                    <Button key={badge} size="xs" variant={draft.badge === badge ? "secondary" : "outline"} onClick={() => patch({ badge: draft.badge === badge ? "" : badge })}>
                      {badge}
                    </Button>
                  ))}
                </div>
              </Field>
              <Field label="排序" hint="小的在前">
                <Input className="h-8 w-24 font-mono text-xs" inputMode="numeric" value={draft.sortOrder} onChange={(event) => patch({ sortOrder: event.target.value })} />
              </Field>
            </div>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <Field label="配色">
                <div className="flex flex-wrap gap-1.5">
                  {PACKAGE_TONE_KEYS.map((key) => (
                    <button
                      key={key}
                      type="button"
                      aria-label={PACKAGE_TONES[key].label}
                      onClick={() => patch({ tone: key })}
                      className={cn(
                        "grid size-8 place-items-center rounded-full ring-offset-2 ring-offset-background transition-shadow",
                        PACKAGE_TONES[key].solid,
                        draft.tone === key ? "ring-2 ring-foreground" : "hover:ring-2 hover:ring-border"
                      )}
                    >
                      {draft.tone === key ? <Palette className="size-3.5 text-white" /> : null}
                    </button>
                  ))}
                </div>
              </Field>
              <ToggleTile label="主推" hint="卡片加描边与星标，商店里排在显眼位置" checked={draft.featured} onChange={(featured) => patch({ featured })} />
            </div>
          </SheetBlock>

          <SheetBlock icon={<Coins className="size-3.5" />} title="额度与价格">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Field label="额度">
                <Input className="h-8 font-mono text-xs" inputMode="decimal" value={draft.points} onChange={(event) => patch({ points: event.target.value })} />
              </Field>
              <Field label="赠送额度" hint="同一批次、同一到期">
                <Input className="h-8 font-mono text-xs" inputMode="decimal" value={draft.bonusPoints} onChange={(event) => patch({ bonusPoints: event.target.value })} />
              </Field>
              <Field label="售价（元）">
                <Input className="h-8 font-mono text-xs" inputMode="decimal" value={draft.price} onChange={(event) => patch({ price: event.target.value })} />
              </Field>
              <Field label="划线价（元）" hint="0 不显示">
                <Input className="h-8 font-mono text-xs" inputMode="decimal" value={draft.originalPrice} onChange={(event) => patch({ originalPrice: event.target.value })} />
              </Field>
            </div>
            <div className="mt-3">
              <Field label="有效期" hint="0 为永久；永久批次排在限期批次之后扣">
                <div className="flex flex-wrap items-center gap-1.5">
                  <Input className="h-8 w-20 font-mono text-xs" inputMode="numeric" value={draft.validityDays} onChange={(event) => patch({ validityDays: event.target.value })} />
                  <span className="text-xs text-muted-foreground">天</span>
                  {VALIDITY.map((days) => (
                    <Button key={days} size="xs" variant={draft.validityDays === String(days) ? "secondary" : "outline"} onClick={() => patch({ validityDays: String(days) })}>
                      {days} 天
                    </Button>
                  ))}
                  <Button size="xs" variant={draft.validityDays === "0" ? "secondary" : "outline"} onClick={() => patch({ validityDays: "0" })}>
                    永久
                  </Button>
                </div>
              </Field>
            </div>
          </SheetBlock>

          <SheetBlock icon={<ShieldCheck className="size-3.5" />} title="谁能买、买多少">
            <div className="space-y-3">
              <Field label="购买对象">
                <div className="flex flex-wrap gap-1.5">
                  {([["all", "所有人"], ["vip", "仅会员"], ["tiers", "指定档位"]] as const).map(([key, label]) => (
                    <Button key={key} size="sm" variant={draft.audience === key ? "secondary" : "outline"} onClick={() => patch({ audience: key })}>
                      {label}
                    </Button>
                  ))}
                </div>
              </Field>
              {draft.audience === "tiers" ? (
                <Field label="可购买的档位" hint="持有任一选中档位的会员可买">
                  <div className="flex flex-wrap gap-1.5">
                    {tiers.length === 0 ? <span className="text-xs text-muted-foreground">应用还没有会员档位</span> : null}
                    {tiers.map((tier) => {
                      const on = draft.tierIds.includes(tier.id);
                      return (
                        <button
                          key={tier.id}
                          type="button"
                          onClick={() => patch({ tierIds: on ? draft.tierIds.filter((id) => id !== tier.id) : [...draft.tierIds, tier.id] })}
                          className={cn(
                            "h-7 rounded-full border px-3 text-xs transition-colors",
                            on ? "border-amber-500/50 bg-amber-500/15 text-amber-700 dark:text-amber-300" : "hover:bg-muted"
                          )}
                        >
                          {tier.name}
                        </button>
                      );
                    })}
                  </div>
                </Field>
              ) : null}
              <ToggleTile label="参与会员折扣" hint="会员买时按其档位（或通用会员）的额度包折扣与多送比例"
                checked={draft.memberDiscount} onChange={(memberDiscount) => patch({ memberDiscount })} />
              <div className="grid gap-3 sm:grid-cols-3">
                <Field label="每人累计限购" hint="0 不限">
                  <Input className="h-8 font-mono text-xs" inputMode="numeric" value={draft.perUserLimit} onChange={(event) => patch({ perUserLimit: event.target.value })} />
                </Field>
                <Field label="每人每月限购" hint="0 不限">
                  <Input className="h-8 font-mono text-xs" inputMode="numeric" value={draft.monthlyLimit} onChange={(event) => patch({ monthlyLimit: event.target.value })} />
                </Field>
                <Field label="总库存" hint={sold > 0 ? `已售 ${sold} · 0 不限` : "0 不限"}>
                  <Input className="h-8 font-mono text-xs" inputMode="numeric" value={draft.stock} onChange={(event) => patch({ stock: event.target.value })} />
                </Field>
              </div>
            </div>
          </SheetBlock>

          <SheetBlock icon={<CalendarRange className="size-3.5" />} title="上架与售卖时间">
            <div className="space-y-3">
              <ToggleTile label="上架" hint="下架后用户看不到，已买的批次不受影响" checked={draft.enabled} onChange={(enabled) => patch({ enabled })} />
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="开始售卖" hint="留空即刻">
                  <Input type="datetime-local" className="h-8 font-mono text-xs" value={draft.startsAt} onChange={(event) => patch({ startsAt: event.target.value })} />
                </Field>
                <Field label="结束售卖" hint="留空长期">
                  <Input type="datetime-local" className="h-8 font-mono text-xs" value={draft.endsAt} onChange={(event) => patch({ endsAt: event.target.value })} />
                </Field>
              </div>
            </div>
          </SheetBlock>
        </div>

        <aside className="space-y-4 border-t bg-muted/30 p-5 lg:overflow-y-auto lg:border-l lg:border-t-0">
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium text-muted-foreground">按谁的身份预览</Label>
            <div className="flex flex-wrap gap-1">
              {identities.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => setIdentityKey(item.key)}
                  className={cn(
                    "h-7 rounded-full border px-2.5 text-[11px] transition-colors",
                    identityKey === item.key ? "border-foreground/30 bg-foreground text-background" : "bg-background hover:bg-muted"
                  )}
                >
                  {item.label}
                </button>
              ))}
            </div>
          </div>
          <PackageCard pkg={{ ...payload, sold }} identity={identity} tierName={tierName} now={now} />
          <div className="overflow-hidden rounded-xl border bg-background">
            <div className="border-b px-3 py-2 text-[11px] font-medium text-muted-foreground">各身份到手</div>
            <table className="w-full text-[11px]">
              <tbody>
                {identities.map((item) => {
                  const quote = quotePackage(payload, item);
                  return (
                    <tr key={item.key} className={cn("border-b last:border-0", identityKey === item.key && "bg-muted/50")}>
                      <td className="px-3 py-1.5">{item.label}</td>
                      <td className="px-2 py-1.5 text-right font-mono tabular-nums">{formatYuan(quote.finalPrice)}</td>
                      <td className="px-2 py-1.5 text-right font-mono tabular-nums">{formatPoints(quote.totalPoints)}</td>
                      <td className="px-3 py-1.5 text-right font-mono tabular-nums text-muted-foreground">
                        {quote.finalPrice > 0 && quote.totalPoints > 0 ? `${formatYuan(quote.unitPrice)}/万` : "—"}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <p className="border-t px-3 py-2 text-[10px] leading-relaxed text-muted-foreground">
              与后端报价同一公式：折后价按分向下取整（至少 0.01），会员多送按额度向下取整。实际以购买时的报价为准。
            </p>
          </div>
        </aside>
      </div>
      <div className="flex shrink-0 items-center justify-end gap-2 border-t px-5 py-3 sm:px-6">
        <Button size="sm" variant="outline" onClick={onClose}>
          取消
        </Button>
        <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
          {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
          {target.mode === "edit" ? "保存" : "创建"}
        </Button>
      </div>
    </>
  );
}

function Field({ label, hint, className, children }: { label: string; hint?: string; className?: string; children: React.ReactNode }) {
  return (
    <div className={cn("min-w-0 space-y-1.5", className)}>
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {hint ? <span className="truncate text-[10px] text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </div>
  );
}
