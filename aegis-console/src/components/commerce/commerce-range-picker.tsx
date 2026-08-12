"use client";

import { useState } from "react";
import type { DateRange } from "react-day-picker";
import { CalendarDays, ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

/**
 * 交易时间窗。
 *
 * 原来是「一个下拉 + 两个 `<input type=date>`」，自定义区间要手打两次
 * `2026-03-21` 这种格式，打错了还没有提示。这里换成一个按钮：
 * 常用区间一键选，要精确到某几天就在日历上刷一下。
 *
 * 对外仍然只吐 `{ start, end }` 两个 RFC3339 字符串 ——
 * 「结束日含当天」这条规则在这里落一次，各面板不必各自记得补 23:59:59。
 */
export type CommerceRange = { start?: string; end?: string };

const PRESETS = [
  { key: "7", label: "近 7 天", days: 6 },
  { key: "30", label: "近 30 天", days: 29 },
  { key: "90", label: "近 90 天", days: 89 },
  { key: "365", label: "近一年", days: 364 }
] as const;

function toDateInput(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function parseDateInput(value?: string) {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

/** 起于当天 00:00、止于当天 23:59:59。选到 21 号却看不到当天的单是这类筛选最常见的投诉。 */
function rangeOf(from: Date, to: Date): CommerceRange {
  const start = new Date(from);
  start.setHours(0, 0, 0, 0);
  const end = new Date(to);
  end.setHours(23, 59, 59, 999);
  return { start: start.toISOString(), end: end.toISOString() };
}

function presetRange(days: number): CommerceRange {
  const to = new Date();
  const from = new Date();
  from.setDate(from.getDate() - days);
  return rangeOf(from, to);
}

export function commerceRangePreset(days: number) {
  return presetRange(days);
}

export function CommerceRangePicker({
  value,
  onChange
}: {
  value: CommerceRange;
  onChange: (range: CommerceRange) => void;
}) {
  const [open, setOpen] = useState(false);
  const from = parseDateInput(value.start);
  const to = parseDateInput(value.end);
  const selected: DateRange | undefined = from ? { from, to } : undefined;

  const label = !from && !to ? "全部时间" : `${from ? toDateInput(from) : "不限"} ~ ${to ? toDateInput(to) : "至今"}`;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          size="sm"
          variant="outline"
          data-active={Boolean(from || to)}
          className="h-8 gap-1.5 text-xs data-[active=true]:border-foreground/40"
        >
          <CalendarDays className="size-3.5" />
          <span className="max-w-[186px] truncate tabular-nums">{label}</span>
          <ChevronDown className="size-3 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-auto p-0">
        <div className="flex flex-wrap gap-1 border-b p-2">
          {PRESETS.map((preset) => (
            <Button
              key={preset.key}
              size="sm"
              variant="ghost"
              className="h-7 text-xs"
              onClick={() => {
                onChange(presetRange(preset.days));
                setOpen(false);
              }}
            >
              {preset.label}
            </Button>
          ))}
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-xs text-muted-foreground"
            onClick={() => {
              onChange({});
              setOpen(false);
            }}
          >
            全部时间
          </Button>
        </div>
        <Calendar
          mode="range"
          numberOfMonths={2}
          defaultMonth={from}
          selected={selected}
          onSelect={(range) => {
            if (!range?.from) {
              onChange({});
              return;
            }
            onChange(rangeOf(range.from, range.to ?? range.from));
          }}
        />
        <p className="border-t px-3 py-2 text-[11px] leading-4 text-muted-foreground">
          含起止当天。实收按到账时间、退款按退款成功时间、钱包按流水时间统计。
        </p>
      </PopoverContent>
    </Popover>
  );
}
