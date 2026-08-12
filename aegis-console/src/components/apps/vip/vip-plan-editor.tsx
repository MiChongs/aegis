"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useSaveAdminVipPlanMutation } from "@/lib/vip-hooks";
import type { VipFeature, VipPlan, VipPlanKind } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

type Props = {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 为空即新建 */
  plan?: VipPlan | null;
  features: VipFeature[];
  /** 该应用是否已有启用中的试用套餐（不含正在编辑的这个） */
  hasOtherActiveTrial: boolean;
};

type Form = {
  name: string;
  kind: VipPlanKind;
  trialDeviceLimited: boolean;
  features: string[];
  durationDays: string;
  price: string;
  originalPrice: string;
  bonusIntegral: string;
  description: string;
  isActive: boolean;
  sortOrder: string;
};

function seed(plan?: VipPlan | null): Form {
  return {
    name: plan?.name ?? "",
    kind: plan?.kind ?? "paid",
    trialDeviceLimited: plan?.trialDeviceLimited ?? false,
    features: plan?.features ?? [],
    durationDays: String(plan?.durationDays ?? 30),
    price: plan?.price ?? "",
    originalPrice: plan?.originalPrice ?? "",
    bonusIntegral: String(plan?.bonusIntegral ?? 0),
    description: plan?.description ?? "",
    isActive: plan?.isActive ?? true,
    sortOrder: String(plan?.sortOrder ?? 0)
  };
}

/**
 * 套餐编辑抽屉。
 *
 * 草稿按「正在编辑哪一个」绑定（`draft.scope`），不用 `useEffect` 同步 ——
 * 与配置面板同一条约束：effect 同步既触发级联渲染，也会把上一个套餐的
 * 未保存改动串到下一个上。
 */
export function VipPlanEditor({ appKey, open, onOpenChange, plan, features, hasOtherActiveTrial }: Props) {
  const scope = plan ? `plan:${plan.id}` : "new";
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const form = draft?.scope === scope ? draft.value : seed(plan);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const saveMutation = useSaveAdminVipPlanMutation(appKey);
  const isTrial = form.kind === "trial";
  const trialConflict = isTrial && form.isActive && hasOtherActiveTrial;

  const toggleFeature = (tag: string) => {
    patch(
      "features",
      form.features.includes(tag) ? form.features.filter((item) => item !== tag) : [...form.features, tag]
    );
  };

  const submit = async () => {
    const name = form.name.trim();
    if (!name) {
      toast.error("套餐名称不能为空");
      return;
    }
    const durationDays = Number(form.durationDays);
    if (!Number.isFinite(durationDays) || durationDays <= 0) {
      toast.error("套餐时长必须大于 0 天");
      return;
    }
    // 试用恒 0 元：后端有 CHECK 约束，这里先拦一道，免得让人填完再被拒
    const price = isTrial ? "0" : form.price.trim() || "0";
    if (!isTrial && Number.isNaN(Number(price))) {
      toast.error("套餐价格格式无效");
      return;
    }

    try {
      await saveMutation.mutateAsync({
        id: plan?.id,
        name,
        kind: form.kind,
        trialDeviceLimited: isTrial ? form.trialDeviceLimited : false,
        features: form.features,
        durationDays,
        price,
        originalPrice: form.originalPrice.trim() || undefined,
        bonusIntegral: Number(form.bonusIntegral) || 0,
        description: form.description.trim(),
        isActive: form.isActive,
        sortOrder: Number(form.sortOrder) || 0
      });
      toast.success(plan ? "套餐已更新" : "套餐已创建");
      setDraft(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存套餐失败");
    }
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) setDraft(null);
        onOpenChange(next);
      }}
    >
      <SheetContent side="right" className="flex w-[92vw] max-w-xl flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{plan ? "编辑套餐" : "新建套餐"}</SheetTitle>
          <SheetDescription>
            {isTrial ? "试用套餐：0 元、一人一次，由「领取」入口发放" : "付费套餐：出现在客户端的购买列表里"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            {/* 种类：决定这个套餐是被买的还是被领的 */}
            <div className="space-y-2">
              <Label className="text-xs">套餐种类</Label>
              <div className="grid grid-cols-2 gap-2">
                {(["paid", "trial"] as const).map((kind) => (
                  <button
                    key={kind}
                    type="button"
                    onClick={() => patch("kind", kind)}
                    className={cn(
                      "rounded-xl border px-3 py-2.5 text-left transition-colors",
                      form.kind === kind
                        ? "border-primary/50 bg-primary/5 ring-1 ring-inset ring-primary/30"
                        : "border-border hover:bg-muted/60"
                    )}
                  >
                    <div className="text-xs font-medium">{kind === "paid" ? "付费套餐" : "试用套餐"}</div>
                    <div className="mt-0.5 text-[10px] leading-snug text-muted-foreground">
                      {kind === "paid" ? "余额或在线支付购买，可重复续费" : "一人一次，不能购买"}
                    </div>
                  </button>
                ))}
              </div>
              {trialConflict ? (
                <p className="text-[11px] text-destructive">
                  该应用已有一个启用中的试用套餐。每个应用至多一个 —— 多于一个时「点领取到底领哪个」没有答案。
                  请先停用原来那个，或把这个存为停用。
                </p>
              ) : null}
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => patch("name", event.target.value)}
                  placeholder={isTrial ? "如：7 天免费试用" : "如：月度会员"}
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">时长（天）</Label>
                <Input
                  inputMode="numeric"
                  value={form.durationDays}
                  onChange={(event) => patch("durationDays", event.target.value)}
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">价格</Label>
                <Input
                  inputMode="decimal"
                  value={isTrial ? "0" : form.price}
                  disabled={isTrial}
                  onChange={(event) => patch("price", event.target.value)}
                  placeholder="0.00"
                />
                {isTrial ? (
                  <p className="text-[10px] text-muted-foreground">试用恒为 0 元：定价大于 0 等于开了一个可反复触发的免费入口</p>
                ) : null}
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">原价（划线价，可空）</Label>
                <Input
                  inputMode="decimal"
                  value={form.originalPrice}
                  disabled={isTrial}
                  onChange={(event) => patch("originalPrice", event.target.value)}
                  placeholder="仅用于展示折扣"
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">赠送积分</Label>
                <Input
                  inputMode="numeric"
                  value={form.bonusIntegral}
                  onChange={(event) => patch("bonusIntegral", event.target.value)}
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">排序（小的在前）</Label>
                <Input
                  inputMode="numeric"
                  value={form.sortOrder}
                  onChange={(event) => patch("sortOrder", event.target.value)}
                />
              </div>

              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">描述</Label>
                <Textarea
                  rows={2}
                  value={form.description}
                  onChange={(event) => patch("description", event.target.value)}
                  placeholder="客户端上展示给用户的一句话"
                />
              </div>
            </div>

            {/* 功能标识：这个套餐解锁哪些能力 */}
            <div className="space-y-2">
              <div className="flex items-baseline justify-between gap-2">
                <Label className="text-xs">解锁的功能</Label>
                <span className="text-[11px] text-muted-foreground">
                  已选 {form.features.length} / {features.length}
                </span>
              </div>
              {features.length === 0 ? (
                <p className="rounded-xl border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
                  还没有功能标识。不勾任何一项也能卖 —— 那就是「只是会员」，
                  服务端校验只回答是不是会员。要按能力细分请先去「功能标识」建目录。
                </p>
              ) : (
                <div className="space-y-1.5 rounded-xl border border-border p-2">
                  {features.map((feature) => (
                    <label
                      key={feature.tag}
                      className={cn(
                        "flex cursor-pointer items-start gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-muted/60",
                        !feature.isActive && "opacity-60"
                      )}
                    >
                      <Checkbox
                        checked={form.features.includes(feature.tag)}
                        onCheckedChange={() => toggleFeature(feature.tag)}
                        className="mt-0.5"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate text-xs font-medium">{feature.name}</span>
                          <code className="rounded bg-muted px-1 text-[10px] text-muted-foreground">{feature.tag}</code>
                          {!feature.isActive ? (
                            <span className="text-[10px] text-muted-foreground">已停用</span>
                          ) : null}
                        </div>
                        {feature.description ? (
                          <p className="truncate text-[10px] text-muted-foreground">{feature.description}</p>
                        ) : null}
                      </div>
                    </label>
                  ))}
                </div>
              )}
              <p className="text-[10px] leading-snug text-muted-foreground">
                开通时会把这份清单<b>快照</b>进账本：之后改套餐配置，已经卖出去的会员不会当场少一项权益。
              </p>
            </div>

            {isTrial ? (
              <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
                <div className="min-w-0 space-y-0.5">
                  <div className="text-xs font-medium">同一设备只能领一次</div>
                  <div className="text-[10px] leading-snug text-muted-foreground">
                    防注册小号反复领。开启后请求必须带设备标识，否则<b>拒领</b>而不是放行 ——
                    放行等于这个开关不存在。
                  </div>
                </div>
                <Switch
                  checked={form.trialDeviceLimited}
                  onCheckedChange={(value) => patch("trialDeviceLimited", value)}
                />
              </label>
            ) : null}

            <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <div className="min-w-0 space-y-0.5">
                <div className="text-xs font-medium">启用</div>
                <div className="text-[10px] leading-snug text-muted-foreground">
                  {isTrial ? "停用后客户端的试用入口会整个消失" : "停用后不再出现在购买列表里，已购买的不受影响"}
                </div>
              </div>
              <Switch checked={form.isActive} onCheckedChange={(value) => patch("isActive", value)} />
            </label>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
