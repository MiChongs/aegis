"use client";

import { useState } from "react";
import { useParams } from "next/navigation";
import { Check, ChevronsUpDown, Shirt } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import type { CosmeticItem, CosmeticSlot } from "@/lib/api/cosmetics";
import { useCosmeticCatalogQuery, useCosmeticItemsQuery } from "@/lib/cosmetic-hooks";
import { cosmeticTermLabel, useCosmeticNamesVersion } from "@/lib/cosmetic-names";
import { cn } from "@/lib/utils";
import { CosmeticItemPreview } from "./cosmetic-preview";
import { RarityBadge, SLOT_ORDER, isPlateSlot, slotLabel } from "./cosmetic-shared";

/**
 * 其它系统的奖励表单接入装扮（目录里 value="cosmetic"：refId = 装扮编号、amount = 天数，0 为永久）。
 *
 * 奖励编辑器多数拿不到 appKey，这里从路由参数取（它们都挂在 /apps/[appKey] 下），调用方也可以显式传。
 * 选择器只列上架中的装扮（GET cosmetics/items?status=active），每项用渲染器画缩略预览。
 */

export function useRouteAppKey(appKey?: string | null) {
  const params = useParams<{ appKey?: string }>();
  if (appKey) return appKey;
  const raw = params?.appKey;
  return raw ? decodeURIComponent(String(raw)) : null;
}

/**
 * 让奖励摘要（describeReward 一类纯函数）里的装扮名称可用：取一次装扮列表登记名字，并订阅登记表，
 * 名字晚到时调用它的组件自动重绘。没有装扮读取权限时静默失败，摘要退回「装扮 #编号」。
 */
export function useCosmeticRewardNames(appKey?: string | null) {
  const key = useRouteAppKey(appKey);
  useCosmeticItemsQuery(key);
  return useCosmeticNamesVersion();
}

export function CosmeticPicker({
  appKey,
  value,
  onChange,
  slot,
  disabled,
  className
}: {
  appKey?: string | null;
  value: number;
  onChange: (id: number, item?: CosmeticItem) => void;
  /** 只列某个槽位 */
  slot?: CosmeticSlot;
  disabled?: boolean;
  className?: string;
}) {
  const key = useRouteAppKey(appKey);
  const [open, setOpen] = useState(false);
  const query = useCosmeticItemsQuery(key, { status: "active", slot });
  const all = useCosmeticItemsQuery(key);
  const catalog = useCosmeticCatalogQuery(key).data;
  const items = query.data ?? [];
  const selected = items.find((item) => item.id === value) ?? all.data?.find((item) => item.id === value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          disabled={disabled || !key}
          className={cn("h-auto min-h-8 w-full justify-between gap-2 px-2 py-1 font-normal sm:w-72", className)}
        >
          {selected && key ? (
            <span className="flex min-w-0 items-center gap-2">
              <span className={cn("flex h-7 shrink-0 items-center justify-center overflow-hidden rounded bg-muted/60", isPlateSlot(selected.slot) ? "w-14" : "w-7")}>
                <CosmeticItemPreview
                  appKey={key}
                  item={selected}
                  width={selected.slot === "name_style" || isPlateSlot(selected.slot) ? 56 : 28}
                  height={28}
                  nameText={selected.name}
                />
              </span>
              <span className="truncate text-sm">{selected.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{slotLabel(selected.slot, catalog)}</span>
              {selected.status !== "active" ? <span className="shrink-0 text-xs text-amber-600 dark:text-amber-400">未上架</span> : null}
            </span>
          ) : (
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <Shirt className="size-4" />
              {value ? `装扮 #${value}` : "选择装扮"}
            </span>
          )}
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[340px] p-0" align="start">
        <Command>
          <CommandInput placeholder="搜索装扮" />
          <CommandList className="max-h-80">
            <CommandEmpty>{query.isLoading ? "加载中" : query.isError ? "无法读取装扮" : "暂无上架中的装扮"}</CommandEmpty>
            {SLOT_ORDER.filter((s) => items.some((item) => item.slot === s)).map((s) => (
              <CommandGroup key={s} heading={slotLabel(s, catalog)}>
                {items
                  .filter((item) => item.slot === s)
                  .map((item) => (
                    <CommandItem
                      key={item.id}
                      value={`${item.name} ${item.code} ${item.id}`}
                      onSelect={() => {
                        onChange(item.id, item);
                        setOpen(false);
                      }}
                      className="gap-2"
                    >
                      <span className={cn("flex h-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted/60", isPlateSlot(item.slot) ? "w-[72px]" : "w-9")}>
                        {key ? (
                          <CosmeticItemPreview
                            appKey={key}
                            item={item}
                            width={item.slot === "name_style" || isPlateSlot(item.slot) ? 72 : 36}
                            height={36}
                            nameText={item.name}
                          />
                        ) : null}
                      </span>
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-sm">{item.name}</span>
                        <span className="truncate font-mono text-[11px] text-muted-foreground">{item.code}</span>
                      </span>
                      <RarityBadge rarity={item.rarity} catalog={catalog} />
                      {item.id === value ? <Check className="size-3.5" /> : null}
                    </CommandItem>
                  ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

/**
 * 奖励行里的装扮：选择器 + 天数（0 为永久）。传了 equip 时多一个「发放时佩戴」开关（物品发放用；
 * 发放后替换该槽位原有佩戴，勋章放第一个空位，已永久持有也会戴上）。
 */
export function CosmeticRewardInput({
  appKey,
  refId,
  amount,
  max = 3650,
  onChange,
  disabled,
  equip
}: {
  appKey?: string | null;
  refId: number;
  amount: string | number;
  max?: number;
  onChange: (next: { refId?: number; amount?: string; equip?: boolean }) => void;
  disabled?: boolean;
  equip?: boolean;
}) {
  const days = Number(amount) || 0;
  return (
    <div className="flex flex-wrap items-center gap-2">
      <CosmeticPicker appKey={appKey} value={refId} onChange={(id) => onChange({ refId: id })} disabled={disabled} />
      <Input
        type="number"
        className="h-8 w-24"
        value={String(amount)}
        min={0}
        max={max}
        disabled={disabled}
        onChange={(event) => onChange({ amount: event.target.value })}
      />
      <span className="text-xs text-muted-foreground">天（{cosmeticTermLabel(days)}）</span>
      {equip !== undefined ? (
        <label
          className="flex items-center gap-1.5 text-xs text-muted-foreground"
          title="发放后戴到它的槽位上：替换原有佩戴，勋章放第一个空位（满了不戴）；已永久持有也会戴上"
        >
          <Switch size="sm" checked={equip} disabled={disabled} onCheckedChange={(checked) => onChange({ equip: checked })} />
          发放时佩戴
        </label>
      ) : null}
    </div>
  );
}
