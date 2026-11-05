"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, CornerDownLeft, History, Search, X } from "lucide-react";
import { appSections, findAppSection, findAppSectionGroup } from "@/lib/app-sections";
import { useAppSectionNavStore } from "@/lib/app-section-nav-store";
import { preloadPinyin } from "@/lib/pinyin-search";
import { useIsClient } from "@/lib/use-client-value";
import { cn } from "@/lib/utils";
import { useSectionFilter } from "@/components/apps/detail/use-section-filter";

/**
 * 宽屏（≥1280）左侧配置目录。
 *
 * 三十多个区块排成一列，要解决的是「找得到」和「回得去」：
 *
 * - 顶部筛选框：中文、拼音首字母、说明里的词都能命中；按 `/` 聚焦，
 *   上下键在结果里移动、回车进入，手不用离开键盘。
 * - 「最近」：最近进过的几个区块（跨应用记忆），日常反复配的那几项一步到位。
 * - 分组可折叠且记住状态；当前区块所在的组始终展开，组头带着当前位置的提示点。
 *
 * 整块限高在视口内自己滚动，13 寸屏上也够得着最后一组。
 */
export function AppSectionRail({
  section,
  onSelect
}: {
  section: string;
  onSelect: (section: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const groups = useSectionFilter(query);
  const activeGroup = findAppSectionGroup(section)?.key;
  const listRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const filtering = query.trim().length > 0;

  // 持久化状态只在客户端读，服务端快照一律按「没有记录」渲染，免得水合对不上
  const isClient = useIsClient();
  const storedFolded = useAppSectionNavStore((s) => s.folded);
  const storedRecent = useAppSectionNavStore((s) => s.recent);
  const toggleFolded = useAppSectionNavStore((s) => s.toggleFolded);
  const clearRecent = useAppSectionNavStore((s) => s.clearRecent);
  const folded = isClient ? storedFolded : [];
  const recent = (isClient ? storedRecent : [])
    .filter((key) => key !== section)
    .map((key) => findAppSection(key))
    .filter((item) => item !== undefined)
    .slice(0, 3);

  // 筛选结果摊平，键盘上下键按这个顺序走
  const flat = useMemo(() => groups.flatMap((group) => group.sections.map((item) => item.key)), [groups]);
  const cursorKey = filtering ? flat[Math.min(cursor, flat.length - 1)] : undefined;

  // 深链进来时把选中项滚进可视区，否则落在第 30 项时导航里看不到高亮
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-section="${section}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [section]);

  useEffect(() => {
    if (!cursorKey) return;
    listRef.current?.querySelector<HTMLElement>(`[data-section="${cursorKey}"]`)?.scrollIntoView({ block: "nearest" });
  }, [cursorKey]);

  // 高度跟着卡片在视口里的实际位置走。只写 `max-h: 100svh - 顶栏` 不够：页面还没滚动时
  // 卡片上方压着头部名片，下半截落在屏幕外；列表又是 overscroll-contain，
  // 在目录上滚轮只滚列表不带页面，最后几组永远看不到。
  useEffect(() => {
    const panel = panelRef.current;
    if (!panel) return;
    let frame = 0;
    const fit = () => {
      frame = 0;
      if (panel.offsetParent === null) return; // 窄屏时导航隐藏
      const top = panel.getBoundingClientRect().top;
      const available = window.innerHeight - Math.max(0, top) - 16;
      panel.style.maxHeight = `${Math.max(240, Math.floor(available))}px`;
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(fit);
    };
    fit();
    // capture：控制台的滚动容器不一定是 window，任何祖先滚动都要重算
    window.addEventListener("scroll", schedule, { capture: true, passive: true });
    window.addEventListener("resize", schedule);
    const observer = new ResizeObserver(schedule);
    observer.observe(document.documentElement);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      window.removeEventListener("scroll", schedule, { capture: true });
      window.removeEventListener("resize", schedule);
      observer.disconnect();
    };
  }, []);

  // `/` 聚焦筛选框（输入框、编辑器里按下时不抢）
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
      if (!inputRef.current || inputRef.current.offsetParent === null) return; // 窄屏时导航隐藏
      event.preventDefault();
      inputRef.current.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const pick = (key: string) => {
    onSelect(key);
    if (filtering) {
      setQuery("");
      setCursor(0);
    }
  };

  const position = appSections.findIndex((item) => item.key === section) + 1;

  return (
    <nav aria-label="应用配置区块" className="hidden xl:block">
      <div
        ref={panelRef}
        className="sticky top-[4.5rem] flex max-h-[calc(100svh-5.5rem)] flex-col overflow-hidden rounded-2xl border border-border bg-card"
        style={{ boxShadow: "var(--shadow-soft)" }}
      >
        {/* 标题 + 筛选 */}
        <div className="shrink-0 space-y-2.5 px-3 pt-3 pb-2.5">
          <div className="flex items-baseline justify-between px-1">
            <span className="text-xs font-semibold tracking-tight">配置目录</span>
            <span className="font-mono text-[10px] text-muted-foreground tabular-nums">
              {position > 0 ? `${position} / ${appSections.length}` : appSections.length}
            </span>
          </div>
          <label className="group flex h-9 items-center gap-2 rounded-xl border border-transparent bg-muted/60 px-2.5 text-xs transition-colors focus-within:border-ring/60 focus-within:bg-background">
            <Search className="size-3.5 shrink-0 text-muted-foreground" />
            <input
              ref={inputRef}
              value={query}
              onFocus={preloadPinyin}
              onChange={(event) => {
                setQuery(event.target.value);
                setCursor(0);
              }}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  setQuery("");
                  inputRef.current?.blur();
                } else if (event.key === "ArrowDown" && flat.length) {
                  event.preventDefault();
                  setCursor((value) => (value + 1) % flat.length);
                } else if (event.key === "ArrowUp" && flat.length) {
                  event.preventDefault();
                  setCursor((value) => (value - 1 + flat.length) % flat.length);
                } else if (event.key === "Enter") {
                  const target = cursorKey ?? flat[0];
                  if (target) pick(target);
                }
              }}
              placeholder="筛选区块"
              aria-label="筛选区块"
              className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
            />
            {filtering ? (
              <button
                type="button"
                aria-label="清空"
                onClick={() => setQuery("")}
                className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            ) : (
              <kbd className="shrink-0 rounded border border-border bg-background px-1.5 font-mono text-[10px] leading-4 text-muted-foreground group-focus-within:hidden">
                /
              </kbd>
            )}
          </label>
        </div>

        <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-2">
          {/* 最近访问 */}
          {!filtering && recent.length > 0 && (
            <div className="mb-1 rounded-xl bg-muted/35 p-1.5">
              <div className="flex items-center justify-between px-1.5 pb-1">
                <span className="flex items-center gap-1 text-[10px] font-medium text-muted-foreground">
                  <History className="size-3" />
                  最近
                </span>
                <button
                  type="button"
                  onClick={clearRecent}
                  className="text-[10px] text-muted-foreground/70 transition-colors hover:text-foreground"
                >
                  清除
                </button>
              </div>
              <div className="space-y-px">
                {recent.map((item) => {
                  const Icon = item.icon;
                  return (
                    <button
                      key={item.key}
                      type="button"
                      onClick={() => pick(item.key)}
                      title={item.summary}
                      className="flex w-full items-center gap-2 rounded-lg px-1.5 py-1 text-left text-xs text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
                    >
                      <Icon className="size-3.5 shrink-0" />
                      <span className="truncate">{item.title}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {groups.length === 0 && (
            <div className="px-2 py-8 text-center text-xs text-muted-foreground">
              没有匹配「{query.trim()}」的区块
            </div>
          )}

          {groups.map((group) => {
            const isActiveGroup = group.key === activeGroup;
            const open = filtering || isActiveGroup || !folded.includes(group.key);
            const GroupIcon = group.icon;
            return (
              <div key={group.key} className="pt-1.5">
                <button
                  type="button"
                  onClick={() => toggleFolded(group.key)}
                  disabled={filtering || isActiveGroup}
                  aria-expanded={open}
                  className="group/head flex w-full items-center gap-2 rounded-lg px-1.5 py-1.5 text-left transition-colors enabled:hover:bg-accent/60"
                >
                  <span
                    className={cn(
                      "grid size-5 shrink-0 place-items-center rounded-md",
                      isActiveGroup ? "bg-primary/12 text-primary" : "bg-muted text-muted-foreground"
                    )}
                  >
                    <GroupIcon className="size-3" />
                  </span>
                  <span
                    className={cn(
                      "flex-1 truncate text-[11px] font-semibold tracking-wide",
                      isActiveGroup ? "text-foreground" : "text-muted-foreground"
                    )}
                  >
                    {group.title}
                  </span>
                  <span className="font-mono text-[10px] text-muted-foreground/80 tabular-nums">{group.sections.length}</span>
                  <ChevronDown
                    className={cn(
                      "size-3 shrink-0 text-muted-foreground transition-transform duration-200",
                      !open && "-rotate-90",
                      (filtering || isActiveGroup) && "opacity-0"
                    )}
                  />
                </button>

                {open && (
                  <div className="relative mt-0.5 ml-[1.0625rem] space-y-px border-l border-border/70 pl-1.5">
                    {group.sections.map((item) => {
                      const active = item.key === section;
                      const pointed = item.key === cursorKey;
                      const Icon = item.icon;
                      return (
                        <button
                          key={item.key}
                          type="button"
                          data-section={item.key}
                          aria-current={active ? "page" : undefined}
                          onClick={() => pick(item.key)}
                          onMouseEnter={() => {
                            if (filtering) setCursor(flat.indexOf(item.key));
                          }}
                          title={item.summary}
                          className={cn(
                            "relative flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition-colors",
                            active
                              ? "bg-primary/10 font-medium text-primary"
                              : pointed
                                ? "bg-accent text-foreground"
                                : "text-muted-foreground hover:bg-accent/60 hover:text-foreground"
                          )}
                        >
                          {active && (
                            <span aria-hidden className="absolute inset-y-1.5 -left-[0.4375rem] w-[3px] rounded-full bg-primary" />
                          )}
                          <Icon className="size-3.5 shrink-0" />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate">{item.title}</span>
                            {filtering && (
                              <span className="block truncate text-[10px] font-normal text-muted-foreground">{item.summary}</span>
                            )}
                          </span>
                          {pointed && <CornerDownLeft className="size-3 shrink-0 text-muted-foreground" />}
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </div>

        <div className="flex shrink-0 items-center justify-center gap-3 border-t border-border/70 px-3 py-2 text-[10px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border px-1 font-mono leading-4">/</kbd>
            筛选
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border px-1 font-mono leading-4">↑↓</kbd>
            选择
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border px-1 font-mono leading-4">↵</kbd>
            进入
          </span>
        </div>
      </div>
    </nav>
  );
}
