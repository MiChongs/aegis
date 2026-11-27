"use client";

import { useState } from "react";
import { ArrowDown, ArrowUp, Loader2, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { FeatureChecklist, PerkTagList } from "@/components/apps/vip/vip-shared";
import { useSaveAdminVipTierMutation } from "@/lib/vip-hooks";
import { VIP_PERK_LIMITS, type VipFeature, type VipPerk, type VipTier } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

type Props = {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 为空即新建 */
  tier?: VipTier | null;
  features: VipFeature[];
};

/** 一行手填标签。rowId 只给 React 当 key（增删、排序时输入框不串行），不发给后端 */
type PerkRow = { rowId: string; label: string; detail: string };

type Form = { name: string; description: string; features: string[]; perks: PerkRow[]; sortOrder: string };

let perkRowSeq = 0;
const nextPerkRowId = () => `perk-${++perkRowSeq}`;

function seed(tier?: VipTier | null): Form {
  return {
    name: tier?.name ?? "",
    description: tier?.description ?? "",
    features: tier?.features ?? [],
    perks: (tier?.perks ?? []).map((perk) => ({ rowId: `saved-${perk.key}`, label: perk.label, detail: perk.detail ?? "" })),
    sortOrder: String(tier?.sortOrder ?? 0)
  };
}

/** 按字符（码点）计长，与后端 `len([]rune(s))` 同一口径；先去首尾空白，后端也会去 */
const charCount = (value: string) => Array.from(value.trim()).length;

/** 手填标签的前端检查：只是提示，最终以后端 400 的中文原因为准 */
function checkPerks(rows: PerkRow[]) {
  const filled = rows.filter((row) => row.label.trim());
  const seen = new Set<string>();
  const duplicates = new Set<string>();
  for (const row of filled) {
    const label = row.label.trim();
    if (seen.has(label)) duplicates.add(label);
    seen.add(label);
  }
  const tooLong = filled.some(
    (row) => charCount(row.label) > VIP_PERK_LIMITS.label || charCount(row.detail) > VIP_PERK_LIMITS.detail
  );
  const orphanDetail = rows.some((row) => !row.label.trim() && row.detail.trim());
  return { filled, duplicates, tooLong, orphanDetail, tooMany: seen.size > VIP_PERK_LIMITS.count };
}

/**
 * 档位编辑抽屉。草稿按「正在编辑哪一个」绑定，与套餐编辑同一条约束（不用 effect 同步）。
 *
 * 档位的功能就是会员的权益：保存即对档位下全部在期会员生效，卖这个档位的套餐一起变。
 */
export function VipTierEditor({ appKey, open, onOpenChange, tier, features }: Props) {
  const scope = tier ? `tier:${tier.id}` : "new";
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const form = draft?.scope === scope ? draft.value : seed(tier);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const saveMutation = useSaveAdminVipTierMutation(appKey);

  const submit = async () => {
    const name = form.name.trim();
    if (!name) {
      toast.error("档位名称不能为空");
      return;
    }
    const perkCheck = checkPerks(form.perks);
    if (perkCheck.tooLong) {
      toast.error(`权益标签不超过 ${VIP_PERK_LIMITS.label} 个字，说明不超过 ${VIP_PERK_LIMITS.detail} 个字`);
      return;
    }
    if (perkCheck.tooMany) {
      toast.error(`每个档位最多 ${VIP_PERK_LIMITS.count} 个补充权益标签`);
      return;
    }
    if (perkCheck.orphanDetail) {
      toast.error("有说明的权益标签需要填写标签文字");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        id: tier?.id,
        name,
        description: form.description.trim(),
        features: form.features,
        // 整组发送：空数组即清空；同字重复的后端会去重，前端先提示
        perks: perkCheck.filled.map((row) => {
          const detail = row.detail.trim();
          return detail ? { label: row.label.trim(), detail } : { label: row.label.trim() };
        }),
        sortOrder: Number(form.sortOrder) || 0
      });
      toast.success(tier ? "档位已更新" : "档位已创建");
      setDraft(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存档位失败");
    }
  };

  const affected = tier ? tier.members : 0;
  const perkCheck = checkPerks(form.perks);
  const autoPerks = tier?.autoPerks ?? [];
  const previewPerks: VipPerk[] = [
    ...autoPerks,
    ...perkCheck.filled.map((row, index) => ({
      key: `custom.${index + 1}`,
      label: row.label.trim(),
      detail: row.detail.trim() || undefined,
      source: "custom" as const
    }))
  ];

  const updatePerk = (rowId: string, field: "label" | "detail", value: string) =>
    patch(
      "perks",
      form.perks.map((row) => (row.rowId === rowId ? { ...row, [field]: value } : row))
    );
  const movePerk = (index: number, offset: -1 | 1) => {
    const target = index + offset;
    if (target < 0 || target >= form.perks.length) return;
    const next = [...form.perks];
    [next[index], next[target]] = [next[target], next[index]];
    patch("perks", next);
  };
  const removePerk = (rowId: string) =>
    patch(
      "perks",
      form.perks.filter((row) => row.rowId !== rowId)
    );
  const addPerk = () => patch("perks", [...form.perks, { rowId: nextPerkRowId(), label: "", detail: "" }]);

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
          <SheetTitle>{tier ? "编辑档位" : "新建档位"}</SheetTitle>
          <SheetDescription>
            {tier && affected > 0 ? `修改功能会立即作用于 ${affected} 位在期会员` : "会员的权益由档位决定"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => patch("name", event.target.value)}
                  placeholder="如：永久会员、高级会员"
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
                  placeholder="选填"
                />
              </div>
            </div>

            <div className="space-y-2">
              <div className="flex items-baseline justify-between gap-2">
                <Label className="text-xs">包含的功能</Label>
                <span className="text-[11px] text-muted-foreground">
                  已选 {form.features.length} / {features.length}
                </span>
              </div>
              <FeatureChecklist catalog={features} value={form.features} onChange={(next) => patch("features", next)} />
            </div>

            <div className="space-y-3">
              <div className="flex items-baseline justify-between gap-2">
                <Label className="text-xs">权益标签</Label>
                <span className="text-[11px] text-muted-foreground">只用于会员页展示，与功能无关</span>
              </div>

              <div className="space-y-1.5 rounded-xl border border-border bg-muted/30 px-3 py-2.5">
                <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-0.5">
                  <span className="text-[11px] font-medium">自动生成</span>
                  <span className="text-[11px] text-muted-foreground">按 AI 计费设置自动生成，改价后自动更新</span>
                </div>
                {tier ? (
                  <PerkTagList perks={autoPerks} emptyHint="当前 AI 计费设置没有为这个档位生成标签" />
                ) : (
                  <p className="text-[11px] text-muted-foreground">保存档位后按 AI 计费设置生成</p>
                )}
              </div>

              <div className="space-y-2">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="text-[11px] font-medium">补充标签</span>
                  <span
                    className={cn(
                      "text-[11px] tabular-nums text-muted-foreground",
                      perkCheck.tooMany && "text-destructive"
                    )}
                  >
                    {perkCheck.filled.length} / {VIP_PERK_LIMITS.count}
                  </span>
                </div>

                {form.perks.length === 0 ? (
                  <p className="rounded-xl border border-dashed border-border px-3 py-3 text-center text-[11px] text-muted-foreground">
                    暂无补充标签，如「专属客服」「新功能抢先体验」
                  </p>
                ) : (
                  <ol className="space-y-2">
                    {form.perks.map((row, index) => {
                      const labelLength = charCount(row.label);
                      const detailLength = charCount(row.detail);
                      const labelOver = labelLength > VIP_PERK_LIMITS.label;
                      const detailOver = detailLength > VIP_PERK_LIMITS.detail;
                      const duplicate = perkCheck.duplicates.has(row.label.trim());
                      return (
                        <li key={row.rowId} className="flex items-start gap-2 rounded-xl border border-border p-2.5">
                          <span className="mt-1.5 w-4 shrink-0 text-center text-[11px] tabular-nums text-muted-foreground">
                            {index + 1}
                          </span>
                          <div className="min-w-0 flex-1 space-y-1.5">
                            <div className="relative">
                              <Input
                                value={row.label}
                                onChange={(event) => updatePerk(row.rowId, "label", event.target.value)}
                                placeholder="标签文字，如：专属客服"
                                aria-invalid={labelOver || duplicate || undefined}
                                className="h-8 pr-14 text-xs"
                              />
                              <span
                                className={cn(
                                  "pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-[10px] tabular-nums text-muted-foreground",
                                  labelOver && "text-destructive"
                                )}
                              >
                                {labelLength}/{VIP_PERK_LIMITS.label}
                              </span>
                            </div>
                            <div className="relative">
                              <Input
                                value={row.detail}
                                onChange={(event) => updatePerk(row.rowId, "detail", event.target.value)}
                                placeholder="说明（选填），悬停或展开时显示"
                                aria-invalid={detailOver || undefined}
                                className="h-8 pr-14 text-xs"
                              />
                              <span
                                className={cn(
                                  "pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-[10px] tabular-nums text-muted-foreground",
                                  detailOver && "text-destructive"
                                )}
                              >
                                {detailLength}/{VIP_PERK_LIMITS.detail}
                              </span>
                            </div>
                            {duplicate ? (
                              <p className="text-[10px] text-amber-600 dark:text-amber-400">
                                与其他标签同字，保存时只保留第一个
                              </p>
                            ) : null}
                          </div>
                          <div className="flex shrink-0 flex-col gap-0.5">
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-xs"
                              disabled={index === 0}
                              onClick={() => movePerk(index, -1)}
                            >
                              <ArrowUp className="size-3" />
                              <span className="sr-only">上移</span>
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-xs"
                              disabled={index === form.perks.length - 1}
                              onClick={() => movePerk(index, 1)}
                            >
                              <ArrowDown className="size-3" />
                              <span className="sr-only">下移</span>
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-xs"
                              className="text-muted-foreground hover:text-destructive"
                              onClick={() => removePerk(row.rowId)}
                            >
                              <Trash2 className="size-3" />
                              <span className="sr-only">删除</span>
                            </Button>
                          </div>
                        </li>
                      );
                    })}
                  </ol>
                )}

                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="w-full"
                  disabled={form.perks.length >= VIP_PERK_LIMITS.count}
                  onClick={addPerk}
                >
                  <Plus className="size-3.5" />
                  添加标签
                </Button>
              </div>

              {previewPerks.length > 0 ? (
                <div className="space-y-1.5">
                  <span className="text-[11px] text-muted-foreground">会员页展示顺序（自动的在前）</span>
                  <PerkTagList perks={previewPerks} emptyHint={null} />
                </div>
              ) : null}
            </div>
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
