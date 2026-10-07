"use client";

import { useEffect, useRef, useState, type RefObject } from "react";
import { ChevronRight, Search, X } from "lucide-react";
import { MethodText } from "@/components/developers/method-badge";
import { readableSummary, type FlatOperation } from "@/lib/api/openapi";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

type Group = { name: string; items: FlatOperation[] };

function OperationLink({
  operation,
  active,
  onSelect
}: {
  operation: FlatOperation;
  active: boolean;
  onSelect: (operation: FlatOperation) => void;
}) {
  const summary = readableSummary(operation);
  return (
    <button
      type="button"
      title={summary ? `${summary}\n${operation.path}` : operation.path}
      data-op-key={operation.key}
      onClick={() => onSelect(operation)}
      aria-current={active ? "true" : undefined}
      className={cn(
        "group/op relative flex w-full items-center gap-2 rounded-md py-1.5 pr-2 pl-3 text-left transition-colors",
        active
          ? "bg-primary/8 text-foreground"
          : "text-muted-foreground hover:bg-muted/70 hover:text-foreground"
      )}
    >
      {active ? (
        <span className="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-primary" aria-hidden />
      ) : null}
      <MethodText method={operation.method} />
      <span className="min-w-0 flex-1">
        {summary ? (
          <span className={cn("block truncate text-[12.5px]", active && "font-medium")}>{summary}</span>
        ) : (
          <span className={cn("block truncate font-mono text-[11.5px]", active && "font-medium")} dir="rtl">
            {/* 路径从左截断：同组路径前缀相同，有区分度的是尾部 */}
            <bdi>{operation.path}</bdi>
          </span>
        )}
        {summary ? (
          <span className="block truncate font-mono text-[10.5px] text-muted-foreground/80">
            {operation.path}
          </span>
        ) : null}
      </span>
      {operation.deprecated ? (
        <span className="size-1.5 shrink-0 rounded-full bg-amber-500" title="已废弃" />
      ) : null}
    </button>
  );
}

/**
 * 接口目录。
 *
 * 无关键词时按分组折叠展示，当前分组默认展开、其余按需点开；
 * 有关键词时跨全部分组检索，结果仍按分组归类，便于判断命中的是哪一类接口。
 */
export function OperationNav({
  groups,
  activeTag,
  selectedKey,
  keyword,
  onKeywordChange,
  onSelect,
  searchRef,
  className
}: {
  groups: Group[];
  activeTag: string;
  selectedKey?: string;
  keyword: string;
  onKeywordChange: (value: string) => void;
  onSelect: (operation: FlatOperation) => void;
  searchRef?: RefObject<HTMLInputElement | null>;
  className?: string;
}) {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set([activeTag]));
  const listRef = useRef<HTMLDivElement>(null);
  const normalized = keyword.trim().toLowerCase();

  // 当前分组变化（分享链接、跨组检索后选中）时自动展开，但不收起用户已展开的分组
  const [lastActive, setLastActive] = useState(activeTag);
  if (lastActive !== activeTag) {
    setLastActive(activeTag);
    setExpanded((current) => new Set(current).add(activeTag));
  }

  const filtered = normalized
    ? groups
        .map((group) => ({
          name: group.name,
          items: group.items.filter((operation) =>
            `${operation.method} ${operation.path} ${operation.summary || ""} ${operation.operationId || ""} ${operation.tag}`
              .toLowerCase()
              .includes(normalized)
          )
        }))
        .filter((group) => group.items.length)
    : groups;
  const hitCount = filtered.reduce((sum, group) => sum + group.items.length, 0);

  // 选中项滚入可视区，分享链接打开时也能在目录里看到它
  useEffect(() => {
    if (!selectedKey) return;
    const list = listRef.current;
    const node = list?.querySelector<HTMLElement>(`[data-op-key="${CSS.escape(selectedKey)}"]`);
    if (!list || !node) return;
    // 只滚动目录自身；scrollIntoView 会连带滚动窗口，分享链接打开时页面会跳过页头
    const listRect = list.getBoundingClientRect();
    const nodeRect = node.getBoundingClientRect();
    if (nodeRect.top < listRect.top) {
      list.scrollTop -= listRect.top - nodeRect.top + 8;
    } else if (nodeRect.bottom > listRect.bottom) {
      list.scrollTop += nodeRect.bottom - listRect.bottom + 8;
    }
  }, [selectedKey]);

  function toggle(name: string) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }

  return (
    <div className={cn("flex min-h-0 flex-col gap-3", className)}>
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          ref={searchRef}
          className="h-9 pr-9 pl-9 text-[13px]"
          value={keyword}
          onChange={(event) => onKeywordChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape") onKeywordChange("");
          }}
          placeholder="搜索接口"
          aria-label="搜索路径、方法、摘要或分组"
        />
        {keyword ? (
          <button
            type="button"
            onClick={() => onKeywordChange("")}
            className="absolute top-1/2 right-2 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label="清空搜索"
          >
            <X className="size-3.5" />
          </button>
        ) : (
          <kbd className="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 rounded border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">
            /
          </kbd>
        )}
      </div>

      <div ref={listRef} className="-mr-2 min-h-0 flex-1 overflow-y-auto overscroll-contain pr-2">
        {normalized ? (
          <p className="px-1 pb-2 text-[12px] text-muted-foreground">共 {hitCount} 个匹配结果</p>
        ) : null}

        <div className="space-y-1">
          {filtered.map((group) => {
            const open = Boolean(normalized) || expanded.has(group.name);
            const containsSelected = group.items.some((item) => item.key === selectedKey);
            return (
              <section key={group.name}>
                <button
                  type="button"
                  onClick={() => toggle(group.name)}
                  disabled={Boolean(normalized)}
                  aria-expanded={open}
                  className={cn(
                    "flex w-full items-center gap-1.5 rounded-md px-1.5 py-1.5 text-left text-[13px] transition-colors enabled:hover:bg-muted/70",
                    containsSelected ? "font-semibold text-foreground" : "font-medium text-foreground/85"
                  )}
                >
                  <ChevronRight
                    className={cn(
                      "size-3.5 shrink-0 text-muted-foreground transition-transform duration-200",
                      open && "rotate-90"
                    )}
                  />
                  <span className="min-w-0 flex-1 truncate">{group.name}</span>
                  <span className="rounded-full bg-muted px-1.5 font-mono text-[10.5px] tabular-nums text-muted-foreground">
                    {group.items.length}
                  </span>
                </button>
                {open ? (
                  <div className="mt-0.5 mb-2 ml-[13px] space-y-px border-l pl-1.5">
                    {group.items.map((operation) => (
                      <OperationLink
                        key={operation.key}
                        operation={operation}
                        active={operation.key === selectedKey}
                        onSelect={onSelect}
                      />
                    ))}
                  </div>
                ) : null}
              </section>
            );
          })}
        </div>

        {!filtered.length ? (
          <div className="px-2 py-12 text-center">
            <Search className="mx-auto size-5 text-muted-foreground/60" />
            <p className="mt-2 text-[13px] text-muted-foreground">没有匹配的接口</p>
          </div>
        ) : null}
      </div>
    </div>
  );
}
