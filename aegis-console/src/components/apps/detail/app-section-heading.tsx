"use client";

import { ChevronRight } from "lucide-react";
import { findAppSection, findAppSectionGroup } from "@/lib/app-sections";

/**
 * 内容区顶部的区块标题，让「左边选了什么」在右边有回声，并补上一句说明。
 * 窄屏不画：吸顶条已经写着组名与区块名，再来一遍只是占高度。
 */
export function AppSectionHeading({ section }: { section: string }) {
  const meta = findAppSection(section);
  const group = findAppSectionGroup(section);
  if (!meta) return null;
  const Icon = meta.icon;
  return (
    <div className="hidden min-w-0 items-center gap-3 xl:flex">
      <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary">
        <Icon className="size-4" />
      </span>
      <div className="min-w-0">
        <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
          {group?.title}
          <ChevronRight className="size-3" />
        </div>
        <div className="flex min-w-0 items-baseline gap-2">
          <h2 className="shrink-0 text-base font-semibold tracking-tight">{meta.title}</h2>
          <p className="truncate text-xs text-muted-foreground">{meta.summary}</p>
        </div>
      </div>
    </div>
  );
}
