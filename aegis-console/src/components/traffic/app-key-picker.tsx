"use client";

import { useState } from "react";
import { Check, Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";

/** 应用选择：从应用列表里勾选，不让人手抄 appKey */
export function AppKeyPicker({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
  const appsQuery = useAdminAppsQuery();
  const [open, setOpen] = useState(false);
  const apps = appsQuery.data ?? [];
  const byKey = new Map(apps.map((app) => [app.appKey, app]));
  const toggle = (appKey: string) => onChange(value.includes(appKey) ? value.filter((key) => key !== appKey) : [...value, appKey]);

  return (
    <div className="flex min-h-9 min-w-0 flex-wrap items-center gap-1.5 rounded-md border border-input px-2 py-1.5 dark:bg-input/30">
      {value.map((appKey) => {
        const app = byKey.get(appKey);
        return (
          <span key={appKey} className="inline-flex max-w-full min-w-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs">
            <span className="min-w-0 truncate">{app?.name ?? appKey}</span>
            {app ? <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{appKey}</span> : null}
            <button
              type="button"
              aria-label={`移除 ${app?.name ?? appKey}`}
              onClick={() => toggle(appKey)}
              className="grid size-4 shrink-0 place-items-center rounded text-muted-foreground hover:bg-foreground/10 hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          </span>
        );
      })}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button type="button" size="xs" variant="ghost" className="text-muted-foreground">
            <Plus />
            {value.length ? "添加" : "全部应用"}
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-0">
          <Command>
            <CommandInput placeholder="搜索应用" />
            <CommandList>
              <CommandEmpty>{appsQuery.isLoading ? "加载中" : "无匹配应用"}</CommandEmpty>
              <CommandGroup>
                {apps.map((app) => {
                  const selected = value.includes(app.appKey);
                  return (
                    <CommandItem key={app.appKey} value={`${app.name} ${app.appKey}`} onSelect={() => toggle(app.appKey)}>
                      <Check className={cn("size-3.5", selected ? "opacity-100" : "opacity-0")} />
                      <span className="min-w-0 flex-1 truncate">{app.name}</span>
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{app.appKey}</span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}
