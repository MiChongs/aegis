"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { adjacentAppSections, findAppSection, findAppSectionGroup } from "@/lib/app-sections";

/**
 * 内容区顶部的区块标题：让「左边选了什么」在右边有回声，补一句说明，
 * 右侧按目录顺序翻到上一个 / 下一个区块（挨个检查配置时不用回左边找）。
 * 窄屏不画：吸顶条已经写着组名与区块名，也有同样的翻页按钮。
 */
export function AppSectionHeading({
  section,
  onSelect
}: {
  section: string;
  onSelect: (section: string) => void;
}) {
  const meta = findAppSection(section);
  const group = findAppSectionGroup(section);
  if (!meta || !group) return null;
  const Icon = meta.icon;
  const GroupIcon = group.icon;
  const { prev, next } = adjacentAppSections(section);

  return (
    <div className="hidden min-w-0 items-center gap-3 xl:flex">
      <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary ring-1 ring-primary/15 ring-inset">
        <Icon className="size-[18px]" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
          <GroupIcon className="size-3" />
          {group.title}
          <ChevronRight className="size-3 opacity-60" />
          <span className="text-foreground/80">{meta.title}</span>
        </div>
        <div className="flex min-w-0 items-baseline gap-2">
          <h2 className="shrink-0 text-lg leading-7 font-semibold tracking-tight">{meta.title}</h2>
          <p className="truncate text-xs text-muted-foreground">{meta.summary}</p>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <StepLink direction="prev" title={prev?.title} onClick={() => prev && onSelect(prev.key)} />
        <StepLink direction="next" title={next?.title} onClick={() => next && onSelect(next.key)} />
      </div>
    </div>
  );
}

function StepLink({ direction, title, onClick }: { direction: "prev" | "next"; title?: string; onClick: () => void }) {
  const prev = direction === "prev";
  return (
    <button
      type="button"
      disabled={!title}
      onClick={onClick}
      aria-label={title ? `${prev ? "上一个" : "下一个"}：${title}` : prev ? "没有上一个区块" : "没有下一个区块"}
      className="group inline-flex h-8 max-w-40 items-center gap-1 rounded-lg border border-border bg-card px-2 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
    >
      {prev && <ChevronLeft className="size-3.5 shrink-0" />}
      <span className="hidden truncate 2xl:inline">{title ?? (prev ? "已是第一个" : "已是最后一个")}</span>
      {!prev && <ChevronRight className="size-3.5 shrink-0" />}
    </button>
  );
}
