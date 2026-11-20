"use client";

import { useState } from "react";
import { Check, FilePlus2, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { errorMessage } from "@/components/users/detail/kit";
import type {
  CosmeticCatalog,
  CosmeticItem,
  CosmeticItemPayload,
  CosmeticSlot,
  CosmeticUnlockMode,
  Design
} from "@/lib/api/cosmetics";
import { useSaveCosmeticItemMutation } from "@/lib/cosmetic-hooks";
import { cn } from "@/lib/utils";
import { SlotDesign } from "./cosmetic-preview";
import { SLOT_META, SLOT_ORDER, raritiesOf, slotLabel } from "./cosmetic-shared";

/**
 * 新建 / 编辑装扮的基本信息（设计稿走设计器）。新建时可以套用模板：模板来自目录，按槽位过滤，
 * 卡片上用渲染器直接播放模板的设计稿。校验与后端 `ValidateItemInput` 同规则同说法，提交以后端为准。
 */

const CODE_PATTERN = /^[a-z0-9][a-z0-9_\-.]*$/;

type Form = {
  slot: CosmeticSlot;
  template: string;
  code: string;
  name: string;
  description: string;
  rarity: string;
  series: string;
  mode: CosmeticUnlockMode;
  tierIds: number[];
  minLevel: string;
  feature: string;
  hint: string;
  hidden: boolean;
  sortOrder: number;
};

function seed(item: CosmeticItem | null, slot: CosmeticSlot): Form {
  return {
    slot: item?.slot ?? slot,
    template: "",
    code: item?.code ?? "",
    name: item?.name ?? "",
    description: item?.description ?? "",
    rarity: item?.rarity ?? "common",
    series: item?.series ?? "",
    mode: item?.unlock?.mode ?? "grant",
    tierIds: item?.unlock?.tierIds ?? [],
    minLevel: item?.unlock?.minLevel ? String(item.unlock.minLevel) : "10",
    feature: item?.unlock?.feature ?? "",
    hint: item?.unlock?.hint ?? "",
    hidden: item?.hidden ?? false,
    sortOrder: item?.sortOrder ?? 0
  };
}

function validate(form: Form): string | null {
  const code = form.code.trim().toLowerCase();
  if (!code) return "请填写装扮编码";
  if (code.length > 48 || !CODE_PATTERN.test(code)) return "编码只能用小写字母、数字、下划线、短横线与点，且以字母或数字开头，最长 48 位";
  if (!form.name.trim()) return "请填写装扮名称";
  if (Array.from(form.name.trim()).length > 32) return "名称不能超过 32 字";
  if (Array.from(form.description.trim()).length > 200) return "简介不能超过 200 字";
  if (Array.from(form.series.trim()).length > 32) return "系列名不能超过 32 字";
  if (Array.from(form.hint.trim()).length > 64) return "获取说明不能超过 64 字";
  if (form.mode === "level") {
    const level = Number(form.minLevel);
    if (!Number.isInteger(level) || level < 1 || level > 10000) return "等级解锁需要填写 1–10000 之间的等级";
  }
  if (form.mode === "feature" && !form.feature) return "功能权益需要选择一个功能";
  if (form.mode === "vip" && form.tierIds.length > 20) return "最多限定 20 个会员档位";
  return null;
}

function payloadOf(form: Form, create: boolean): CosmeticItemPayload {
  return {
    slot: create ? form.slot : undefined,
    code: form.code.trim().toLowerCase(),
    name: form.name.trim(),
    description: form.description.trim(),
    rarity: form.rarity,
    series: form.series.trim(),
    unlock: {
      mode: form.mode,
      tierIds: form.mode === "vip" ? form.tierIds : undefined,
      minLevel: form.mode === "level" ? Number(form.minLevel) : undefined,
      feature: form.mode === "feature" ? form.feature : undefined,
      hint: form.hint.trim()
    },
    sortOrder: form.sortOrder,
    hidden: form.hidden,
    template: create && form.template ? form.template : undefined
  };
}

export function CosmeticItemDialog({
  open,
  onOpenChange,
  appKey,
  catalog,
  item,
  defaultSlot = "avatar_frame",
  onCreated
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  catalog?: CosmeticCatalog | null;
  /** 编辑时给出；新建时为 null */
  item: CosmeticItem | null;
  defaultSlot?: CosmeticSlot;
  onCreated?: (item: CosmeticItem, openDesigner: boolean) => void;
}) {
  const create = !item;
  const [form, setForm] = useState<Form>(() => seed(item, defaultSlot));
  const [openDesigner, setOpenDesigner] = useState(true);
  const save = useSaveCosmeticItemMutation(appKey);
  const patch = (next: Partial<Form>) => setForm((current) => ({ ...current, ...next }));
  const templates = (catalog?.templates ?? []).filter((template) => template.slot === form.slot);
  const problem = validate(form);

  const pickTemplate = (key: string) => {
    const template = templates.find((entry) => entry.key === key);
    setForm((current) => ({
      ...current,
      template: key,
      name: template && (!current.name || current.template) ? template.name : current.name,
      rarity: template ? template.rarity : current.rarity,
      code: template && (!current.code || current.template) ? template.key.replace(/_/g, "-") : current.code
    }));
  };

  const submit = async () => {
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      const saved = await save.mutateAsync({ itemId: item?.id, payload: payloadOf(form, create) });
      toast.success(create ? `已创建「${saved.name}」` : "已保存");
      onOpenChange(false);
      if (create) onCreated?.(saved, openDesigner);
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !save.isPending && onOpenChange(next)}>
      <DialogContent className="flex max-h-[92dvh] flex-col gap-0 p-0 sm:max-w-3xl">
        <DialogHeader className="border-b px-6 pt-5 pb-4">
          <DialogTitle>{create ? "新建装扮" : `编辑「${item?.name}」`}</DialogTitle>
          <DialogDescription>{create ? "基本信息与获取方式，外观在设计器里制作" : "基本信息与获取方式"}</DialogDescription>
        </DialogHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="flex flex-col gap-6 px-6 py-5">
            {create ? (
              <div className="flex flex-col gap-2">
                <Label className="text-xs text-muted-foreground">槽位</Label>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {SLOT_ORDER.map((slot) => {
                    const meta = SLOT_META[slot];
                    const active = form.slot === slot;
                    return (
                      <button
                        key={slot}
                        type="button"
                        aria-pressed={active}
                        onClick={() => patch({ slot, template: "" })}
                        className={cn(
                          "flex items-center gap-2 rounded-lg border px-3 py-2.5 text-left text-sm transition-colors",
                          active ? "border-primary/50 bg-primary/5 font-medium" : "hover:bg-accent"
                        )}
                      >
                        <meta.icon className="size-4 text-muted-foreground" />
                        {slotLabel(slot, catalog)}
                      </button>
                    );
                  })}
                </div>
                {catalog?.slots.find((spec) => spec.key === form.slot)?.hint ? (
                  <p className="text-xs text-muted-foreground">{catalog.slots.find((spec) => spec.key === form.slot)?.hint}</p>
                ) : null}
              </div>
            ) : null}

            {create ? (
              <div className="flex flex-col gap-2">
                <Label className="text-xs text-muted-foreground">模板</Label>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  <TemplateTile
                    active={!form.template}
                    title="空白"
                    onClick={() => patch({ template: "" })}
                    preview={<FilePlus2 className="size-6 text-muted-foreground" />}
                  />
                  {templates.map((template) => (
                    <TemplateTile
                      key={template.key}
                      active={form.template === template.key}
                      title={template.name}
                      description={template.description}
                      onClick={() => pickTemplate(template.key)}
                      preview={<TemplatePreview slot={template.slot} design={template.design} name={template.name} />}
                    />
                  ))}
                </div>
              </div>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="名称">
                <Input value={form.name} maxLength={32} onChange={(event) => patch({ name: event.target.value })} placeholder="例如：鎏金光环" />
              </Field>
              <Field label="编码">
                <Input
                  value={form.code}
                  maxLength={48}
                  className="font-mono"
                  onChange={(event) => patch({ code: event.target.value.toLowerCase().replace(/\s+/g, "-") })}
                  placeholder="golden-halo"
                />
              </Field>
              <Field label="稀有度">
                <Select value={form.rarity} onValueChange={(rarity) => patch({ rarity })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {raritiesOf(catalog).map((rarity) => (
                      <SelectItem key={rarity.key} value={rarity.key}>
                        <span className="size-2 rounded-full" style={{ backgroundColor: rarity.color }} />
                        {rarity.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="系列">
                <Input value={form.series} maxLength={32} onChange={(event) => patch({ series: event.target.value })} placeholder="选填" />
              </Field>
              <Field label="简介" className="sm:col-span-2">
                <Textarea
                  value={form.description}
                  maxLength={200}
                  rows={2}
                  onChange={(event) => patch({ description: event.target.value })}
                  placeholder="选填"
                />
              </Field>
            </div>

            <div className="flex flex-col gap-3 rounded-xl border p-4">
              <Label className="text-xs text-muted-foreground">获取方式</Label>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
                {(catalog?.unlockModes ?? []).map((mode) => (
                  <button
                    key={mode.key}
                    type="button"
                    aria-pressed={form.mode === mode.key}
                    title={mode.hint}
                    onClick={() => patch({ mode: mode.key })}
                    className={cn(
                      "rounded-lg border px-3 py-2 text-sm transition-colors",
                      form.mode === mode.key ? "border-primary/50 bg-primary/5 font-medium" : "hover:bg-accent"
                    )}
                  >
                    {mode.label}
                  </button>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">{catalog?.unlockModes.find((mode) => mode.key === form.mode)?.hint}</p>

              {form.mode === "vip" ? (
                <div className="flex flex-col gap-2">
                  <span className="text-xs text-muted-foreground">限定档位（不选即任意有效会员）</span>
                  {catalog?.tiers.length ? (
                    <div className="flex flex-wrap gap-2">
                      {catalog.tiers.map((tier) => {
                        const checked = form.tierIds.includes(tier.id);
                        return (
                          <label key={tier.id} className="flex cursor-pointer items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm">
                            <Checkbox
                              checked={checked}
                              onCheckedChange={(value) =>
                                patch({ tierIds: value ? [...form.tierIds, tier.id] : form.tierIds.filter((id) => id !== tier.id) })
                              }
                            />
                            {tier.name}
                          </label>
                        );
                      })}
                    </div>
                  ) : (
                    <span className="text-xs text-muted-foreground">暂无会员档位</span>
                  )}
                </div>
              ) : null}

              {form.mode === "level" ? (
                <Field label="最低等级">
                  <Input
                    value={form.minLevel}
                    inputMode="numeric"
                    className="sm:max-w-40"
                    onChange={(event) => patch({ minLevel: event.target.value.replace(/\D/g, "") })}
                  />
                </Field>
              ) : null}

              {form.mode === "feature" ? (
                <Field label="功能">
                  {catalog?.features.length ? (
                    <Select value={form.feature} onValueChange={(feature) => patch({ feature })}>
                      <SelectTrigger className="w-full sm:w-72">
                        <SelectValue placeholder="选择功能" />
                      </SelectTrigger>
                      <SelectContent position="popper">
                        {catalog.features.map((feature) => (
                          <SelectItem key={feature.tag} value={feature.tag} disabled={!feature.isActive}>
                            {feature.name || feature.tag}
                            <span className="ml-1.5 font-mono text-xs text-muted-foreground">{feature.tag}</span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <span className="text-xs text-muted-foreground">暂无功能标识</span>
                  )}
                </Field>
              ) : null}

              <Field label="获取说明">
                <Input value={form.hint} maxLength={64} onChange={(event) => patch({ hint: event.target.value })} placeholder="选填，例如：积分商城兑换" />
              </Field>
            </div>

            <label className="flex items-center justify-between gap-4 rounded-xl border px-4 py-3">
              <span className="flex flex-col gap-0.5">
                <span className="text-sm font-medium">不在装扮中心列出</span>
                <span className="text-xs text-muted-foreground">仍可发放，持有者仍可佩戴</span>
              </span>
              <Switch checked={form.hidden} onCheckedChange={(hidden) => patch({ hidden })} />
            </label>
          </div>
        </ScrollArea>

        <DialogFooter className="flex-row items-center justify-between gap-2 border-t px-6 py-3 sm:justify-between">
          {create ? (
            <label className="flex cursor-pointer items-center gap-2 text-sm text-muted-foreground">
              <Checkbox checked={openDesigner} onCheckedChange={(value) => setOpenDesigner(Boolean(value))} />
              创建后打开设计器
            </label>
          ) : (
            <span className="text-xs text-destructive">{problem && form.name ? problem : ""}</span>
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>
              取消
            </Button>
            <Button onClick={() => void submit()} disabled={save.isPending}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Check />}
              {create ? "创建" : "保存"}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

function TemplateTile({
  active,
  title,
  description,
  preview,
  onClick
}: {
  active: boolean;
  title: string;
  description?: string;
  preview: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      title={description}
      className={cn(
        "flex flex-col overflow-hidden rounded-xl border text-left transition-colors",
        active ? "border-primary ring-2 ring-primary/20" : "hover:border-foreground/20"
      )}
    >
      <div className="flex h-24 items-center justify-center bg-muted/40">{preview}</div>
      <div className="flex items-center justify-between gap-2 border-t px-2.5 py-2">
        <span className="truncate text-xs font-medium">{title}</span>
        {active ? <Check className="size-3.5 text-primary" /> : null}
      </div>
    </button>
  );
}

function TemplatePreview({ slot, design, name }: { slot: CosmeticSlot; design: Design; name: string }) {
  if (slot === "name_style") return <SlotDesign slot={slot} design={design} width={140} height={60} nameText={name} fontSize={20} />;
  if (slot === "profile_card") return <SlotDesign slot={slot} design={design} width={150} height={84} />;
  return <SlotDesign slot={slot} design={design} width={80} height={80} />;
}
