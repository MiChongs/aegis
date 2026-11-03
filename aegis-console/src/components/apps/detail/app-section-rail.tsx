"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronRight, Search, X } from "lucide-react";
import { findAppSectionGroup } from "@/lib/app-sections";
import { preloadPinyin } from "@/lib/pinyin-search";
import { cn } from "@/lib/utils";
import { useSectionFilter } from "@/components/apps/detail/use-section-filter";

/**
 * 宽屏左侧区块导航。
 *
 * 旧版只是一列吸顶按钮，没有高度上限：三十多个区块在 13 寸屏上排不下，
 * 底部「广告服务」「物品发放」永远够不着，只能先滚主内容。这里把导航
 * 限在视口内、自己滚动，顶部加一个筛选框（中文、拼音首字母都行），
 * 分组可以折叠，当前区块所在的组始终展开。
 */
export function AppSectionRail({
  section,
  onSelect
}: {
  section: string;
  onSelect: (section: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [folded, setFolded] = useState<Set<string>>(() => new Set());
  const groups = useSectionFilter(query);
  const activeGroup = findAppSectionGroup(section)?.key;
  const listRef = useRef<HTMLDivElement>(null);
  const filtering = query.trim().length > 0;

  // 深链进来时把选中项滚进可视区，否则落在第 30 项时导航里看不到高亮
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-section="${section}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [section]);

  const toggle = (key: string) =>
    setFolded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <nav aria-label="应用配置区块" className="hidden lg:block">
      <div className="sticky top-[4.5rem] flex max-h-[calc(100svh-5.5rem)] flex-col overflow-hidden rounded-2xl border border-border bg-card">
        <div className="shrink-0 border-b border-border p-2">
          <label className="flex h-8 items-center gap-2 rounded-lg bg-muted/60 px-2.5 text-xs focus-within:ring-1 focus-within:ring-ring">
            <Search className="size-3.5 shrink-0 text-muted-foreground" />
            <input
              value={query}
              onFocus={preloadPinyin}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Escape") setQuery("");
                if (event.key === "Enter" && groups[0]?.sections[0]) {
                  onSelect(groups[0].sections[0].key);
                  setQuery("");
                }
              }}
              placeholder="筛选区块"
              className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
            />
            {filtering && (
              <button
                type="button"
                aria-label="清空"
                onClick={() => setQuery("")}
                className="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            )}
          </label>
        </div>

        <div ref={listRef} className="min-h-0 flex-1 space-y-1 overflow-y-auto overscroll-contain p-2">
          {groups.length === 0 && (
            <div className="px-2 py-6 text-center text-xs text-muted-foreground">没有匹配的区块</div>
          )}
          {groups.map((group) => {
            const open = filtering || group.key === activeGroup || !folded.has(group.key);
            return (
              <div key={group.key}>
                <button
                  type="button"
                  onClick={() => toggle(group.key)}
                  disabled={filtering || group.key === activeGroup}
                  className="flex w-full items-center gap-1 rounded-md px-2 py-1.5 text-[10px] font-semibold tracking-[0.12em] text-muted-foreground transition-colors enabled:hover:text-foreground"
                >
                  <ChevronRight className={cn("size-3 transition-transform duration-200", open && "rotate-90")} />
                  <span className="flex-1 text-left">{group.title}</span>
                  <span className="font-mono font-normal tracking-normal tabular-nums opacity-70">{group.sections.length}</span>
                </button>
                {open && (
                  <div className="space-y-px pb-1">
                    {group.sections.map((item) => {
                      const active = item.key === section;
                      const Icon = item.icon;
                      return (
                        <button
                          key={item.key}
                          type="button"
                          data-section={item.key}
                          aria-current={active ? "page" : undefined}
                          onClick={() => {
                            onSelect(item.key);
                            if (filtering) setQuery("");
                          }}
                          title={item.summary}
                          className={cn(
                            "relative flex w-full items-center gap-2 rounded-lg py-1.5 pr-2 pl-3 text-left text-xs transition-colors",
                            active
                              ? "bg-accent font-medium text-foreground"
                              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                          )}
                        >
                          {active && <span aria-hidden className="absolute inset-y-1.5 left-1 w-0.5 rounded-full bg-primary" />}
                          <Icon className="size-3.5 shrink-0" />
                          <span className="truncate">{item.title}</span>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </nav>
  );
}
