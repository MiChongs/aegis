"use client";

import { Search } from "lucide-react";
import { Kbd, useMetaKeyLabel } from "@/components/layout/sidebar/sidebar-shared";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 命令面板入口。
 *
 * 展开态是一条输入框状的按钮（看得出能搜什么、也看得见快捷键），
 * 折叠态退化成图标，靠 Tooltip 补回那两条信息。
 */
export function SearchTrigger({
  collapsed,
  onOpen,
  className
}: {
  collapsed: boolean;
  onOpen: () => void;
  className?: string;
}) {
  const metaKey = useMetaKeyLabel();

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={onOpen}
            className={cn(
              "flex size-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground",
              className
            )}
          >
            <Search className="size-4" />
            <span className="sr-only">搜索</span>
          </button>
        </TooltipTrigger>
        <TooltipContent side="right" sideOffset={8}>
          <p>搜索与跳转</p>
          <p className="text-[10px] text-background/70">{metaKey} K</p>
        </TooltipContent>
      </Tooltip>
    );
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "flex w-full items-center gap-2 rounded-lg border border-sidebar-border bg-background/60 px-2.5 py-1.5 text-xs text-muted-foreground",
        "transition-colors hover:border-ring/40 hover:bg-background hover:text-foreground",
        className
      )}
    >
      <Search className="size-3.5 shrink-0" />
      <span className="min-w-0 flex-1 truncate text-left">搜索或跳转…</span>
      <Kbd className="shrink-0">{metaKey} K</Kbd>
    </button>
  );
}
