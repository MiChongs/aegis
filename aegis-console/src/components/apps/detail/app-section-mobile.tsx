"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, ChevronLeft, ChevronRight, Search, X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { adjacentAppSections, findAppSection, findAppSectionGroup } from "@/lib/app-sections";
import { preloadPinyin } from "@/lib/pinyin-search";
import { cn } from "@/lib/utils";
import { useSectionFilter } from "@/components/apps/detail/use-section-filter";

/**
 * 窄屏与窄桌面（<1280）区块导航：吸顶的「当前区块」条 + 全屏选择器。
 *
 * 旧版窄屏是一条三十多个胶囊的横向滚动条，组与组之间只靠一根竖线区分，
 * 想去「积分商城」得横着划五六屏，划过头了也不知道。这里拆成两层：
 *
 * - 吸顶条只说「我在哪」：组名 + 区块名，左右箭头按目录顺序翻页；
 *   下面一排只放**同组**的兄弟区块，最多十来个，一划就到头。
 * - 跨组跳转点区块名打开全屏选择器：分组列表 + 筛选框，每项带一句说明。
 */
export function AppSectionMobileBar({
  section,
  onSelect
}: {
  section: string;
  onSelect: (section: string) => void;
}) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const meta = findAppSection(section);
  const group = findAppSectionGroup(section);
  const { prev, next } = adjacentAppSections(section);
  const chipsRef = useRef<HTMLDivElement>(null);

  // 同组胶囊里把选中项滚到可见处（不动页面纵向滚动）
  useEffect(() => {
    const strip = chipsRef.current;
    const el = strip?.querySelector<HTMLElement>(`[data-section="${section}"]`);
    if (!strip || !el) return;
    const left = el.offsetLeft - (strip.clientWidth - el.offsetWidth) / 2;
    strip.scrollTo({ left: Math.max(0, left), behavior: "smooth" });
  }, [section]);

  if (!meta || !group) return null;
  const Icon = meta.icon;

  return (
    <>
      <div className="sticky top-14 z-20 -mx-4 border-b border-border bg-background/90 backdrop-blur-md supports-[backdrop-filter]:bg-background/75 lg:-mx-6 xl:hidden">
        <div className="flex items-center gap-1.5 px-4 pt-2 pb-1.5 lg:px-6">
          <button
            type="button"
            onClick={() => {
              preloadPinyin();
              setPickerOpen(true);
            }}
            className="flex min-w-0 flex-1 items-center gap-2.5 rounded-xl py-1 pr-2 text-left transition-colors active:bg-accent"
          >
            <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
              <Icon className="size-4" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[10px] leading-tight text-muted-foreground">{group.title}</span>
              <span className="flex items-center gap-1 text-sm leading-tight font-semibold">
                <span className="truncate">{meta.title}</span>
                <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
              </span>
            </span>
          </button>
          <StepButton label="上一个区块" disabled={!prev} onClick={() => prev && onSelect(prev.key)}>
            <ChevronLeft className="size-4" />
          </StepButton>
          <StepButton label="下一个区块" disabled={!next} onClick={() => next && onSelect(next.key)}>
            <ChevronRight className="size-4" />
          </StepButton>
        </div>

        {group.sections.length > 1 && (
          <div
            ref={chipsRef}
            className="flex items-center gap-1.5 overflow-x-auto px-4 pb-2 [scrollbar-width:none] lg:px-6 [&::-webkit-scrollbar]:hidden"
          >
            {group.sections.map((item) => {
              const active = item.key === section;
              return (
                <button
                  key={item.key}
                  type="button"
                  data-section={item.key}
                  aria-current={active ? "page" : undefined}
                  onClick={() => onSelect(item.key)}
                  className={cn(
                    "h-7 shrink-0 rounded-full border px-3 text-xs transition-colors",
                    active
                      ? "border-foreground bg-foreground font-medium text-background"
                      : "border-border bg-card text-muted-foreground active:bg-accent"
                  )}
                >
                  {item.title}
                </button>
              );
            })}
          </div>
        )}
      </div>

      <AppSectionPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        section={section}
        onSelect={(key) => {
          setPickerOpen(false);
          onSelect(key);
        }}
      />
    </>
  );
}

function StepButton({
  label,
  disabled,
  onClick,
  children
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className="grid size-9 shrink-0 place-items-center rounded-xl border border-border bg-card text-muted-foreground transition-colors active:bg-accent disabled:opacity-35"
    >
      {children}
    </button>
  );
}

/**
 * 全屏区块选择器。手机上铺满视口（`100dvh` 跟着地址栏伸缩），平板上是居中浮层。
 * 打开时不自动聚焦筛选框：手机上一聚焦就弹键盘，挡住一半的列表。
 */
function AppSectionPicker({
  open,
  onOpenChange,
  section,
  onSelect
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  section: string;
  onSelect: (section: string) => void;
}) {
  const [query, setQuery] = useState("");
  const groups = useSectionFilter(query);
  const listRef = useRef<HTMLDivElement>(null);

  return (
    <DialogPrimitive.Root
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) setQuery("");
      }}
    >
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/40 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0 max-sm:hidden" />
        <DialogPrimitive.Content
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            listRef.current?.querySelector<HTMLElement>(`[data-section="${section}"]`)?.scrollIntoView({ block: "center" });
          }}
          className={cn(
            "fixed z-50 flex flex-col overflow-hidden bg-background outline-none",
            "inset-0 h-[100dvh] w-screen",
            "sm:inset-auto sm:top-[8svh] sm:left-1/2 sm:h-auto sm:max-h-[84svh] sm:w-[min(36rem,calc(100vw-2rem))] sm:-translate-x-1/2 sm:rounded-2xl sm:border sm:border-border sm:shadow-xl",
            "duration-200 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:slide-in-from-bottom-4"
          )}
        >
          <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 pt-[max(0.75rem,env(safe-area-inset-top))] pb-3">
            <DialogPrimitive.Title className="flex-1 text-base font-semibold">切换区块</DialogPrimitive.Title>
            <DialogPrimitive.Description className="sr-only">选择要配置的应用区块</DialogPrimitive.Description>
            <DialogPrimitive.Close
              aria-label="关闭"
              className="grid size-9 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <X className="size-4" />
            </DialogPrimitive.Close>
          </div>

          <div className="shrink-0 px-4 pt-3 pb-2">
            <label className="flex h-10 items-center gap-2 rounded-xl bg-muted/70 px-3 focus-within:ring-1 focus-within:ring-ring">
              <Search className="size-4 shrink-0 text-muted-foreground" />
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && groups[0]?.sections[0]) onSelect(groups[0].sections[0].key);
                }}
                enterKeyHint="go"
                placeholder="搜索区块，支持拼音首字母"
                className="min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-sm placeholder:text-muted-foreground sm:text-sm"
              />
              {query && (
                <button type="button" aria-label="清空" onClick={() => setQuery("")} className="shrink-0 text-muted-foreground">
                  <X className="size-4" />
                </button>
              )}
            </label>
          </div>

          <div
            ref={listRef}
            className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pb-[max(1rem,env(safe-area-inset-bottom))]"
          >
            {groups.length === 0 && (
              <div className="py-16 text-center text-sm text-muted-foreground">没有匹配的区块</div>
            )}
            {groups.map((group) => (
              <section key={group.key} className="pt-3">
                <h3 className="sticky top-0 z-10 bg-background/95 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-muted-foreground backdrop-blur">
                  {group.title}
                </h3>
                <div className="overflow-hidden rounded-2xl border border-border bg-card">
                  {group.sections.map((item, index) => {
                    const active = item.key === section;
                    const Icon = item.icon;
                    return (
                      <button
                        key={item.key}
                        type="button"
                        data-section={item.key}
                        onClick={() => onSelect(item.key)}
                        className={cn(
                          "flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors active:bg-accent",
                          index > 0 && "border-t border-border/70",
                          active && "bg-primary/5"
                        )}
                      >
                        <span
                          className={cn(
                            "grid size-9 shrink-0 place-items-center rounded-xl",
                            active ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
                          )}
                        >
                          <Icon className="size-4" />
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className={cn("block truncate text-sm", active && "font-semibold")}>{item.title}</span>
                          <span className="block truncate text-xs text-muted-foreground">{item.summary}</span>
                        </span>
                        {active && <Check className="size-4 shrink-0 text-primary" />}
                      </button>
                    );
                  })}
                </div>
              </section>
            ))}
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
