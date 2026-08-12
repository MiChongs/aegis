"use client";

import { useState } from "react";
import { Check, ChevronsUpDown, Coins } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/**
 * 常用记账币种。
 *
 * **不是白名单** —— 后端只校验形状（3 位大写字母），因为 ISO 4217 每年都在增删，
 * 维护一张会过期的清单只会让某天上线的合法币种被拒。这里列的是「省得打字的常用项」，
 * 搜索框里直接敲一个表外的三字母代码同样能选。
 */
const COMMON_CURRENCIES: Array<{ code: string; label: string; symbol: string }> = [
  { code: "CNY", label: "人民币", symbol: "¥" },
  { code: "USD", label: "美元", symbol: "$" },
  { code: "EUR", label: "欧元", symbol: "€" },
  { code: "GBP", label: "英镑", symbol: "£" },
  { code: "JPY", label: "日元", symbol: "¥" },
  { code: "KRW", label: "韩元", symbol: "₩" },
  { code: "HKD", label: "港币", symbol: "HK$" },
  { code: "TWD", label: "新台币", symbol: "NT$" },
  { code: "SGD", label: "新加坡元", symbol: "S$" },
  { code: "AUD", label: "澳元", symbol: "A$" },
  { code: "CAD", label: "加元", symbol: "C$" },
  { code: "INR", label: "印度卢比", symbol: "₹" },
  { code: "RUB", label: "俄罗斯卢布", symbol: "₽" },
  { code: "BRL", label: "巴西雷亚尔", symbol: "R$" },
  { code: "THB", label: "泰铢", symbol: "฿" },
  { code: "MYR", label: "马来西亚林吉特", symbol: "RM" }
];

const CODE_PATTERN = /^[A-Z]{3}$/;

export function currencyLabel(code?: string) {
  const upper = (code || "").toUpperCase();
  const known = COMMON_CURRENCIES.find((item) => item.code === upper);
  return known ? `${known.code} · ${known.label}` : upper;
}

/**
 * 币种选择。
 *
 * 原来是一个 `maxLength=3` 的文本框 + 一行「必须是 3 位字母」的提示 ——
 * 那等于要求管理员背下 ISO 4217。这里改成搜中文名也能命中的选择器，
 * 同时保留「敲一个表外代码直接用」的出口。
 */
export function CurrencySelect({
  value,
  onChange,
  disabled
}: {
  value: string;
  onChange: (code: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");

  const upper = keyword.trim().toUpperCase();
  const filtered = COMMON_CURRENCIES.filter(
    (item) =>
      !upper ||
      item.code.includes(upper) ||
      item.label.includes(keyword.trim()) ||
      item.symbol.includes(keyword.trim())
  );
  // 表里没有但形状合法：允许直接采用，不挡住合法的冷门币种
  const custom = CODE_PATTERN.test(upper) && !COMMON_CURRENCIES.some((item) => item.code === upper) ? upper : null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="h-9 w-full justify-between px-3 text-xs font-normal"
        >
          <span className="flex min-w-0 items-center gap-1.5">
            <Coins className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate font-mono">{value || "未设置"}</span>
            <span className="truncate text-muted-foreground">
              {COMMON_CURRENCIES.find((item) => item.code === value)?.label ?? ""}
            </span>
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[--radix-popover-trigger-width] p-0">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="搜索代码或中文名，如 USD / 美元"
            value={keyword}
            onValueChange={setKeyword}
            className="text-xs"
          />
          <CommandList>
            {filtered.length === 0 && !custom ? (
              <CommandEmpty className="py-6 text-xs">没有匹配的币种，输入 3 位 ISO 4217 代码可直接使用</CommandEmpty>
            ) : null}
            {custom ? (
              <CommandGroup heading="自定义">
                <CommandItem
                  value={custom}
                  onSelect={() => {
                    onChange(custom);
                    setOpen(false);
                  }}
                  className="gap-2 text-xs"
                >
                  <Check className={cn("size-3.5", value === custom ? "opacity-100" : "opacity-0")} />
                  <span className="font-mono">{custom}</span>
                  <span className="text-muted-foreground">直接使用这个代码</span>
                </CommandItem>
              </CommandGroup>
            ) : null}
            {filtered.length > 0 ? (
              <CommandGroup heading="常用">
                {filtered.map((item) => (
                  <CommandItem
                    key={item.code}
                    value={item.code}
                    onSelect={() => {
                      onChange(item.code);
                      setOpen(false);
                    }}
                    className="gap-2 text-xs"
                  >
                    <Check className={cn("size-3.5", value === item.code ? "opacity-100" : "opacity-0")} />
                    <span className="w-10 font-mono">{item.code}</span>
                    <span className="flex-1">{item.label}</span>
                    <span className="text-muted-foreground">{item.symbol}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
