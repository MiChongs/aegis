"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Check, ChevronsUpDown } from "lucide-react";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { appSectionHref } from "@/lib/app-sections";
import { cn } from "@/lib/utils";
import type { AppSummary } from "@/lib/api/types";
import { AppTile, appHealth } from "@/components/apps/app-shared";

/**
 * 换应用但留在当前区块（`/apps/A?tab=oauth` → `/apps/B?tab=oauth`）。
 *
 * 旧版是一个 44 宽的下拉框：应用一多就要滚着找，窄屏上还把头部挤成两行。
 * 这里换成可搜索的命令列表，名称、AppKey、数字 ID 都能搜；弹层宽度按视口夹住，
 * 手机上也不会伸出屏幕。
 */
export function AppSwitcher({
  app,
  apps,
  section,
  compact = false
}: {
  app: AppSummary;
  apps: AppSummary[];
  section: string;
  /** 窄屏只画一个图标按钮 */
  compact?: boolean;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);

  if (apps.length <= 1) return null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        {compact ? (
          <button
            type="button"
            aria-label="切换应用"
            className="grid size-9 shrink-0 place-items-center rounded-xl text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <ChevronsUpDown className="size-4" />
          </button>
        ) : (
          <button
            type="button"
            className="inline-flex h-8 max-w-56 items-center gap-2 rounded-lg border border-border bg-background px-2.5 text-xs transition-colors hover:bg-accent"
          >
            <span className="text-muted-foreground">切换应用</span>
            <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
          </button>
        )}
      </PopoverTrigger>
      <PopoverContent align="end" sideOffset={6} className="w-[min(22rem,calc(100vw-1.5rem))] p-0">
        <Command
          filter={(value, search) => (value.toLowerCase().includes(search.trim().toLowerCase()) ? 1 : 0)}
        >
          <CommandInput placeholder="搜索名称、AppKey 或 ID" className="text-xs" />
          <CommandList className="max-h-[min(22rem,60svh)]">
            <CommandEmpty className="py-6 text-center text-xs text-muted-foreground">没有匹配的应用</CommandEmpty>
            {apps.map((item) => {
              const health = appHealth(item);
              const current = item.appKey === app.appKey;
              return (
                <CommandItem
                  key={item.appKey}
                  value={`${item.name} ${item.appKey} ${item.id}`}
                  onSelect={() => {
                    setOpen(false);
                    if (!current) router.push(appSectionHref(item.appKey, section));
                  }}
                  className="gap-2.5 px-2 py-2"
                >
                  <AppTile name={item.name} seed={item.appKey} size="sm" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="truncate text-xs font-medium">{item.name}</span>
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">#{item.id}</span>
                    </div>
                    <div className="truncate font-mono text-[10px] text-muted-foreground">{item.appKey}</div>
                  </div>
                  <span
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      health.tone === "ok" && "bg-emerald-500",
                      health.tone === "partial" && "bg-amber-500",
                      health.tone === "off" && "bg-zinc-400",
                      health.tone === "governed" && "bg-red-500"
                    )}
                    title={health.label}
                  />
                  <Check className={cn("size-3.5 shrink-0", current ? "opacity-100" : "opacity-0")} />
                </CommandItem>
              );
            })}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
