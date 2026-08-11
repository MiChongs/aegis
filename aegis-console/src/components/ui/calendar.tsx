"use client";

import * as React from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { DayPicker, type DayPickerProps } from "react-day-picker";
import { zhCN } from "react-day-picker/locale";

import { cn } from "@/lib/utils";

/**
 * 日历（react-day-picker v10）。
 *
 * 刻意**不引 react-day-picker 自带的 style.css** —— 那份样式写死了颜色与尺寸，
 * 深浅色切换会失效（与 globals.css 里不引 highlight.js 主题 CSS 同一条约束）。
 * 这里把 v10 的 classNames 槽位逐项映射到项目的主题变量上。
 *
 * 升级 react-day-picker 时先核对 `UI` / `DayFlag` / `SelectionState` 三个枚举的键名：
 * 键名对不上不会报错，只会**静默丢样式**，表现为日历变成一堆没对齐的按钮。
 */
export type CalendarProps = DayPickerProps & {
  /** 是否显示"今天"的强调边框，默认显示 */
  highlightToday?: boolean;
};

export function Calendar({
  className,
  classNames,
  showOutsideDays = true,
  highlightToday = true,
  ...props
}: CalendarProps) {
  return (
    <DayPicker
      locale={zhCN}
      showOutsideDays={showOutsideDays}
      className={cn("p-3", className)}
      classNames={{
        months: "flex flex-col gap-4 sm:flex-row",
        month: "flex flex-col gap-3",
        month_caption: "flex h-7 items-center justify-center",
        caption_label: "text-sm font-medium",
        nav: "flex items-center gap-1",
        button_previous: cn(
          "absolute left-3 top-3 inline-flex size-7 items-center justify-center rounded-md",
          "text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
          "disabled:pointer-events-none disabled:opacity-40"
        ),
        button_next: cn(
          "absolute right-3 top-3 inline-flex size-7 items-center justify-center rounded-md",
          "text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
          "disabled:pointer-events-none disabled:opacity-40"
        ),
        month_grid: "w-full border-collapse",
        weekdays: "flex",
        weekday: "w-8 text-[11px] font-normal text-muted-foreground",
        week: "mt-1 flex w-full",
        day: cn(
          "relative size-8 p-0 text-center text-sm",
          // 区间中段铺满整格，首尾各留一侧圆角 —— 否则选区会断成一串独立方块
          "[&:has([data-range-middle])]:bg-accent",
          "[&:has([data-range-start])]:rounded-l-md [&:has([data-range-start])]:bg-accent",
          "[&:has([data-range-end])]:rounded-r-md [&:has([data-range-end])]:bg-accent"
        ),
        day_button: cn(
          "inline-flex size-8 items-center justify-center rounded-md p-0 font-normal tabular-nums",
          "transition-colors hover:bg-accent hover:text-accent-foreground",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          "aria-selected:bg-primary aria-selected:text-primary-foreground",
          "data-[range-middle]:bg-transparent data-[range-middle]:text-foreground"
        ),
        today: highlightToday ? "font-semibold text-primary" : "",
        outside: "text-muted-foreground/40",
        disabled: "text-muted-foreground/30",
        hidden: "invisible",
        range_start: "",
        range_end: "",
        range_middle: "",
        ...classNames
      }}
      components={{
        Chevron: ({ orientation, ...chevronProps }) =>
          orientation === "left" ? (
            <ChevronLeft className="size-4" {...chevronProps} />
          ) : (
            <ChevronRight className="size-4" {...chevronProps} />
          )
      }}
      {...props}
    />
  );
}
