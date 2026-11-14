"use client";

import { useState, type ReactNode } from "react";
import { Check, ChevronsUpDown, Copy } from "lucide-react";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { copyText } from "@/components/tickets/workspace/ticket-inspector";

// 反馈台各面板共用的小件：分类标记、属性选择器、事实行。

// ─────────────── 分类 ───────────────

const CATEGORY_TONE_BY_KEY: Record<string, string> = {
  feedback_bug: "bg-red-500",
  feedback_feature: "bg-sky-500",
  feedback_experience: "bg-amber-500",
  feedback_other: "bg-slate-400"
};

const CATEGORY_TONE_BY_NAME: Record<string, string> = {
  问题反馈: "bg-red-500",
  功能建议: "bg-sky-500",
  体验吐槽: "bg-amber-500",
  其他: "bg-slate-400"
};

export function categoryTone(categoryKey?: string, name?: string) {
  return (
    (categoryKey ? CATEGORY_TONE_BY_KEY[categoryKey] : undefined) ??
    (name ? CATEGORY_TONE_BY_NAME[name] : undefined) ??
    "bg-muted-foreground/40"
  );
}

export function CategoryDot({ name, categoryKey }: { name?: string; categoryKey?: string }) {
  return <span aria-hidden className={cn("size-2 shrink-0 rounded-full", categoryTone(categoryKey, name))} />;
}

/** 分类标记：一个色点加名字。列表里几十行同时出现，实心徽标会把整列染花 */
export function CategoryTag({ name, categoryKey, className }: { name?: string; categoryKey?: string; className?: string }) {
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground", className)}>
      <span aria-hidden className={cn("size-1.5 rounded-full", categoryTone(categoryKey, name))} />
      {name || "未分类"}
    </span>
  );
}

/** 正文摘要压成一行：换行、Markdown 记号都不该出现在列表里 */
export function flattenPreview(text?: string) {
  return (text ?? "")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/[#>*_`~]+/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

// ─────────────── 属性选择器 ───────────────

export type PropertyOption = {
  value: string;
  label: string;
  /** 选项前的图形：状态色点、头像等 */
  leading?: ReactNode;
  /** 额外的检索词（账号、拼音等） */
  keywords?: string;
};

/**
 * 属性胶囊：平时只是一行里的一个值，点开是可检索的选项列表，选中即生效。
 *
 * 取代「标签在上、下拉框在下」的表单排法：状态、分类、优先级、受理人四项并排
 * 只占一行，不再各占一段纵向空间。不可编辑时退化成纯文本胶囊。
 */
export function PropertyMenu({
  label,
  value,
  options,
  onSelect,
  children,
  editable = true,
  searchable = false,
  open: controlledOpen,
  onOpenChange,
  footer
}: {
  /** 无障碍名与搜索框占位 */
  label: string;
  value?: string;
  options: PropertyOption[];
  onSelect: (value: string) => void;
  /** 胶囊里显示的内容 */
  children: ReactNode;
  editable?: boolean;
  searchable?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  footer?: ReactNode;
}) {
  const [innerOpen, setInnerOpen] = useState(false);
  const open = controlledOpen ?? innerOpen;
  const setOpen = onOpenChange ?? setInnerOpen;

  const chip = "inline-flex h-7 max-w-48 items-center gap-1.5 rounded-full border px-2.5 text-xs font-medium text-foreground";

  if (!editable) {
    return <span className={chip}>{children}</span>;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={label}
          className={cn(
            chip,
            "outline-none transition-colors hover:bg-muted focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 data-[state=open]:bg-muted"
          )}
        >
          {children}
          <ChevronsUpDown className="size-3 shrink-0 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-56 p-0">
        <Command>
          {searchable ? <CommandInput placeholder={`搜索${label}`} /> : null}
          <CommandList className="max-h-72">
            <CommandEmpty>没有匹配项</CommandEmpty>
            <CommandGroup>
              {options.map((option) => (
                <CommandItem
                  key={option.value}
                  value={`${option.label} ${option.keywords ?? ""}`}
                  onSelect={() => {
                    setOpen(false);
                    if (option.value !== value) onSelect(option.value);
                  }}
                >
                  {option.leading}
                  <span className="min-w-0 flex-1 truncate">{option.label}</span>
                  {option.value === value ? <Check className="size-3.5 text-muted-foreground" /> : null}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
          {footer}
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// ─────────────── 事实行 ───────────────

/** 一组「标签，值」行。字段多的地方用它，不给每个字段单独画卡片 */
export function Facts({ children, className }: { children: ReactNode; className?: string }) {
  return <dl className={cn("divide-y text-sm", className)}>{children}</dl>;
}

export function Fact({
  label,
  value,
  mono,
  copyable,
  children
}: {
  label: string;
  /** 纯文本值；为空时整行不渲染 */
  value?: string | number | null;
  mono?: boolean;
  /** 点击复制，值为机器标识（ID、IP、邮箱）时打开 */
  copyable?: boolean;
  /** 需要自定义排版的值，优先于 value */
  children?: ReactNode;
}) {
  const text = value === undefined || value === null || value === "" ? "" : String(value);
  if (!text && !children) return null;
  return (
    <div className="flex items-baseline justify-between gap-4 py-2">
      <dt className="shrink-0 text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("min-w-0 text-right text-foreground", mono && "font-mono text-xs")}>
        {children ??
          (copyable ? (
            <button
              type="button"
              onClick={() => void copyText(text, label)}
              className="group/copy inline-flex max-w-full items-center gap-1.5 rounded text-right outline-none transition-colors hover:text-primary focus-visible:ring-2 focus-visible:ring-ring/40"
            >
              <span className="truncate">{text}</span>
              <Copy className="size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/copy:opacity-100 group-focus-visible/copy:opacity-100" />
            </button>
          ) : (
            <span className="break-words">{text}</span>
          ))}
      </dd>
    </div>
  );
}

/** 面板里的小节标题 */
export function PanelSection({
  title,
  action,
  children,
  className
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("space-y-2", className)}>
      <div className="flex h-6 items-center justify-between">
        <h3 className="text-xs font-medium text-muted-foreground">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}
