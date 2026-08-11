"use client";

import { appSectionGroups, findAppSection } from "@/lib/app-sections";
import { cn } from "@/lib/utils";

/**
 * 应用详情页的区块导航。
 *
 * 13 个区块平铺成一排按钮时（旧版形状），组与组之间的关系完全看不出来，
 * 而且窄屏下会横向溢出、后半截根本发现不了。这里改成竖向分组列表：
 * 组标题承担「这一段是关于什么的」，选中项常驻可见，不随内容滚动。
 */
export function AppSectionNav({
  section,
  onSelect
}: {
  section: string;
  onSelect: (section: string) => void;
}) {
  return (
    <>
      {/* 宽屏：左侧常驻竖向导航 */}
      <nav className="hidden lg:block">
        <div className="sticky top-4 space-y-4">
          {appSectionGroups.map((group) => (
            <div key={group.key} className="space-y-1">
              <div className="px-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                {group.title}
              </div>
              {group.sections.map((item) => {
                const active = item.key === section;
                const Icon = item.icon;
                return (
                  <button
                    key={item.key}
                    type="button"
                    onClick={() => onSelect(item.key)}
                    title={item.summary}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition-colors",
                      active
                        ? "bg-accent font-medium text-foreground"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                    )}
                  >
                    <Icon className={cn("size-3.5 shrink-0", active && "text-foreground")} />
                    <span className="truncate">{item.title}</span>
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      </nav>

      {/* 窄屏：横向滚动的分组胶囊，组间用竖线分隔 */}
      <nav className="-mx-1 flex items-center gap-1 overflow-x-auto px-1 pb-1 lg:hidden">
        {appSectionGroups.map((group, index) => (
          <div key={group.key} className="flex items-center gap-1">
            {index > 0 && <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />}
            {group.sections.map((item) => {
              const active = item.key === section;
              const Icon = item.icon;
              return (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => onSelect(item.key)}
                  className={cn(
                    "inline-flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs transition-colors",
                    active
                      ? "bg-accent font-medium text-foreground"
                      : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                  )}
                >
                  <Icon className="size-3.5" />
                  {item.title}
                </button>
              );
            })}
          </div>
        ))}
      </nav>
    </>
  );
}

/** 内容区顶部的区块标题，让「左边选了什么」在右边有回声 */
export function AppSectionTitle({ section }: { section: string }) {
  const meta = findAppSection(section);
  if (!meta) return null;
  const Icon = meta.icon;
  return (
    <div className="flex items-center gap-2">
      <Icon className="size-4 text-muted-foreground" />
      <h2 className="text-sm font-semibold tracking-tight">{meta.title}</h2>
      <span className="truncate text-xs text-muted-foreground">{meta.summary}</span>
    </div>
  );
}
