"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField } from "@/components/apps/app-config-primitives";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type {
  CardKeyKind,
  CardKeyReward,
  CardKeyRewardSpec,
  CardKeyValidityMode
} from "@/lib/api/card-key";
import { useCardKeyCatalogQuery, useGenerateCardKeysMutation } from "@/lib/card-key-hooks";
import { useAdminVipPlansQuery } from "@/lib/vip-hooks";

/**
 * 数字项一律按**字符串**存。
 *
 * 存 number 的话输入框清不空：清空得到 `Number("") === 0`，界面上立刻跳回 0，
 * 用户没法先删干净再重打。提交时统一转一次即可。
 */
type Form = {
  name: string;
  kind: CardKeyKind;
  remark: string;
  count: string;
  codePrefix: string;
  segments: string;
  segmentLength: string;
  maxDevices: string;
  validityMode: CardKeyValidityMode;
  validityDays: string;
  validUntil: string;
  /** 已勾选的权益：type → 取值 */
  rewards: Record<string, { amount: string; money: string; refId: number }>;
};

function seed(): Form {
  return {
    name: "",
    kind: "redeem",
    remark: "",
    count: "100",
    codePrefix: "",
    segments: "4",
    segmentLength: "4",
    maxDevices: "1",
    validityMode: "permanent",
    validityDays: "30",
    validUntil: "",
    rewards: {}
  };
}

/**
 * 生成卡密。
 *
 * 权益表单**完全由后端目录驱动**（`/card-keys/catalog`）：勾选项、单位、上下界、
 * 提示文案都来自那份目录。在这里另抄一份枚举会同时招来「后端加了一档控制台
 * 配不出来」与「控制台能配、保存时报不支持」，而两边都没有报错提示。
 *
 * 草稿用「作用域绑定」而不是 `useEffect` 同步：抽屉关闭即清空，
 * 与配置面板、会员套餐编辑器同一条约束。
 */
export function CardKeyBatchEditor({
  appKey,
  open,
  onOpenChange
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useCardKeyCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const generateMutation = useGenerateCardKeysMutation(appKey);

  const [draft, setDraft] = useState<Form | null>(null);
  const form = draft ?? seed();
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ ...form, [key]: value });

  const catalog = catalogQuery.data?.rewards ?? [];
  const maxRewards = catalogQuery.data?.maxRewards ?? 6;
  const plans = plansQuery.data ?? [];
  const selected = Object.keys(form.rewards);

  const toggleReward = (spec: CardKeyRewardSpec, checked: boolean) => {
    const next = { ...form.rewards };
    if (checked) {
      if (selected.length >= maxRewards) {
        toast.error(`一张卡最多配置 ${maxRewards} 项权益`);
        return;
      }
      next[spec.type] = { amount: String(spec.min ?? 1), money: "10.00", refId: plans[0]?.id ?? 0 };
    } else {
      delete next[spec.type];
    }
    patch("rewards", next);
  };

  const patchReward = (type: string, field: "amount" | "money" | "refId", value: string | number) => {
    const current = form.rewards[type];
    if (!current) return;
    patch("rewards", { ...form.rewards, [type]: { ...current, [field]: value } });
  };

  const buildRewards = (): CardKeyReward[] =>
    catalog
      .filter((spec) => form.rewards[spec.type])
      .map((spec) => {
        const value = form.rewards[spec.type];
        switch (spec.value) {
          case "amount":
            return { type: spec.type, amount: Number(value.amount) };
          case "money":
            return { type: spec.type, money: value.money };
          default:
            return { type: spec.type, refId: Number(value.refId) };
        }
      });

  const submit = async () => {
    if (!form.name.trim()) {
      toast.error("请填写批次名称");
      return;
    }
    const rewards = buildRewards();
    if (form.kind === "redeem" && rewards.length === 0) {
      toast.error("兑换卡至少要配置一项权益，否则是一张废卡");
      return;
    }
    if (form.validityMode === "fixed_until" && !form.validUntil) {
      toast.error("「统一到期」需要选择到期时间");
      return;
    }

    try {
      const batch = await generateMutation.mutateAsync({
        name: form.name.trim(),
        kind: form.kind,
        remark: form.remark.trim(),
        count: Number(form.count) || 0,
        codePrefix: form.codePrefix.trim(),
        segments: Number(form.segments) || 4,
        segmentLength: Number(form.segmentLength) || 4,
        rewards,
        maxDevices: Number(form.maxDevices) || 1,
        validityMode: form.validityMode,
        validityDays: Number(form.validityDays) || 0,
        validUntil: form.validUntil ? new Date(form.validUntil).toISOString() : null
      });
      toast.success(`已生成 ${batch.total} 张卡密，可在批次上导出 CSV`);
      setDraft(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "生成失败");
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
          <SheetTitle>生成卡密</SheetTitle>
          <SheetDescription>
            {form.kind === "login"
              ? "授权卡：卡就是登录凭证，首次使用自动建号并绑定，可限制绑定设备数"
              : "兑换卡：给已登录用户发权益，核销一次即作废"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            <FieldGroup label="批次名称" hint="只在控制台展示，用于区分几批卡（如「618 活动」）">
              <Input
                value={form.name}
                onChange={(event) => patch("name", event.target.value)}
                placeholder="例如：618 活动首发"
              />
            </FieldGroup>

            <FieldGroup label="卡密类型" hint="这一项生成后不可更改">
              <Select
                value={form.kind}
                onValueChange={(value) => patch("kind", value as CardKeyKind)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="redeem">兑换卡 · 给已登录用户发权益</SelectItem>
                  <SelectItem value="login">授权卡 · 卡即登录凭证</SelectItem>
                </SelectContent>
              </Select>
            </FieldGroup>

            <div className="grid gap-4 sm:grid-cols-2">
              <NumberField
                label="生成数量"
                value={form.count}
                onChange={(value) => patch("count", value)}
                min={1}
                max={10000}
                unit="张"
              />
              <FieldGroup label="卡面前缀" hint="留空即无前缀。可用 A–Z 与 0–9">
                <Input
                  value={form.codePrefix}
                  onChange={(event) => patch("codePrefix", event.target.value.toUpperCase())}
                  placeholder="VIP"
                  maxLength={16}
                />
              </FieldGroup>
              <NumberField
                label="卡面段数"
                value={form.segments}
                onChange={(value) => patch("segments", value)}
                min={1}
                max={8}
                unit="段"
              />
              <NumberField
                label="每段位数"
                value={form.segmentLength}
                onChange={(value) => patch("segmentLength", value)}
                min={3}
                max={12}
                unit="位"
                hint="字符集已剔除易混的 I / O / 0 / 1"
              />
            </div>

            <FieldGroup
              label="有效期"
              hint="「激活即计时」是卡密的常态：卖出去到被使用之间的时间不该算进用户的授权期"
            >
              <Select
                value={form.validityMode}
                onValueChange={(value) => patch("validityMode", value as CardKeyValidityMode)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="permanent">永不过期</SelectItem>
                  <SelectItem value="days_from_first_use">激活即计时</SelectItem>
                  <SelectItem value="fixed_until">统一到期</SelectItem>
                </SelectContent>
              </Select>
            </FieldGroup>

            {form.validityMode === "days_from_first_use" ? (
              <NumberField
                label="有效天数"
                value={form.validityDays}
                onChange={(value) => patch("validityDays", value)}
                min={1}
                max={3650}
                unit="天"
              />
            ) : null}

            {form.validityMode === "fixed_until" ? (
              <FieldGroup label="到期时间">
                <Input
                  type="datetime-local"
                  value={form.validUntil}
                  onChange={(event) => patch("validUntil", event.target.value)}
                />
              </FieldGroup>
            ) : null}

            {form.kind === "login" ? (
              <NumberField
                label="可绑定设备数"
                value={form.maxDevices}
                onChange={(value) => patch("maxDevices", value)}
                min={1}
                max={64}
                unit="台"
                hint="超出后新设备登录被拒；管理员可在卡上解绑，用户换电脑时靠它"
              />
            ) : null}

            <FieldGroup
              label="随卡发放的权益"
              hint={
                form.kind === "login"
                  ? "授权卡可以不带权益（能登录本身就是权益）；带的话在首次激活时发放"
                  : `兑换卡至少配一项，最多 ${maxRewards} 项`
              }
            >
              {catalogQuery.isLoading ? (
                <p className="text-xs text-muted-foreground">正在读取权益目录…</p>
              ) : (
                <div className="space-y-2">
                  {catalog.map((spec) => {
                    const value = form.rewards[spec.type];
                    return (
                      <div key={spec.type} className="rounded-lg border border-border p-3">
                        <label className="flex items-start gap-2.5">
                          <Checkbox
                            checked={Boolean(value)}
                            onCheckedChange={(checked) => toggleReward(spec, Boolean(checked))}
                          />
                          <span className="min-w-0 space-y-0.5">
                            <span className="block text-xs font-medium">{spec.label}</span>
                            <span className="block text-xs text-muted-foreground">{spec.hint}</span>
                          </span>
                        </label>

                        {value ? (
                          <div className="mt-2.5 pl-7">
                            {spec.value === "amount" ? (
                              <div className="flex items-center gap-2">
                                <Input
                                  type="number"
                                  className="h-8 w-32"
                                  value={value.amount}
                                  min={spec.min}
                                  max={spec.max}
                                  onChange={(event) =>
                                    patchReward(spec.type, "amount", event.target.value)
                                  }
                                />
                                <span className="text-xs text-muted-foreground">
                                  {spec.unit}（{spec.min}–{spec.max}）
                                </span>
                              </div>
                            ) : null}

                            {spec.value === "money" ? (
                              <div className="flex items-center gap-2">
                                {/* 金额按字符串走：JSON number 是双精度浮点，0.1 会失真 */}
                                <Input
                                  className="h-8 w-32"
                                  value={value.money}
                                  inputMode="decimal"
                                  onChange={(event) =>
                                    patchReward(spec.type, "money", event.target.value)
                                  }
                                />
                                <span className="text-xs text-muted-foreground">元</span>
                              </div>
                            ) : null}

                            {spec.value === "ref" ? (
                              plans.length === 0 ? (
                                <p className="text-xs text-muted-foreground">
                                  该应用还没有会员套餐，请先在「会员」区块创建。
                                </p>
                              ) : (
                                <Select
                                  value={String(value.refId || plans[0].id)}
                                  onValueChange={(next) =>
                                    patchReward(spec.type, "refId", Number(next))
                                  }
                                >
                                  <SelectTrigger className="h-8 w-56">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {plans.map((plan) => (
                                      <SelectItem key={plan.id} value={String(plan.id)}>
                                        {plan.name}（{plan.durationDays} 天）
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                              )
                            ) : null}

                            {spec.needsLoginCard ? (
                              <p className="mt-1.5 text-xs text-amber-600 dark:text-amber-500">
                                领取人名下必须有仍在授权期内的授权卡，否则整张卡兑换失败。
                              </p>
                            ) : null}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              )}
            </FieldGroup>

            <FieldGroup label="备注" hint="只在控制台可见">
              <Textarea
                value={form.remark}
                onChange={(event) => patch("remark", event.target.value)}
                rows={2}
              />
            </FieldGroup>

            <p className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
              <Label className="text-xs font-medium">生成之后</Label>
              <span className="mt-1 block">
                卡面在批次上导出 CSV。授权卡还需要在「接入」区块把 <b>卡密</b> 勾进登录方式，
                否则客户端调 <code className="font-mono">/auth/login</code> 会被拒。
              </span>
            </p>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={generateMutation.isPending}>
            {generateMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Save className="size-3.5" />
            )}
            生成
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
