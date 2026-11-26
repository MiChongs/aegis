"use client";

import { useMemo, useState } from "react";
import { Check, Hash, Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { RuleIdPill, RuleSeverityBadge, groupName } from "@/components/firewall/firewall-kit";
import type { FirewallRuleInfo } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";
import { RuleIdChip } from "./exclusion-format";

/**
 * 规则编号选择：从规则目录按编号 / 说明 / 组名搜索，也能直接输入目录里没有的编号。
 * 目录按组分段，组内按编号排；已选的条目打勾，再点一次取消。
 */
export function RuleIdPicker({
  value,
  onChange,
  rules,
  loading
}: {
  value: number[];
  onChange: (next: number[]) => void;
  rules: FirewallRuleInfo[];
  loading?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const byId = useMemo(() => new Map(rules.map((rule) => [rule.id, rule])), [rules]);
  const groups = useMemo(() => {
    const map = new Map<string, FirewallRuleInfo[]>();
    for (const rule of [...rules].sort((a, b) => a.id - b.id)) {
      map.set(rule.group, [...(map.get(rule.group) ?? []), rule]);
    }
    return [...map.entries()];
  }, [rules]);

  const selected = new Set(value);
  const toggle = (id: number) => onChange(selected.has(id) ? value.filter((item) => item !== id) : [...value, id]);
  const manual = /^\d{3,8}$/.test(search.trim()) ? Number(search.trim()) : null;

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {value.length ? (
        <div className="flex min-w-0 flex-wrap gap-1.5">
          {value.map((id) => (
            <RuleIdChip key={id} id={id} info={byId.get(id)} className="pr-0.5">
              <button
                type="button"
                aria-label={`移除 #${id}`}
                onClick={() => onChange(value.filter((item) => item !== id))}
                className="grid size-4 shrink-0 place-items-center rounded text-muted-foreground hover:bg-foreground/10 hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            </RuleIdChip>
          ))}
        </div>
      ) : null}
      <Popover
        modal
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) setSearch("");
        }}
      >
        <PopoverTrigger asChild>
          <Button type="button" size="sm" variant="outline" className="w-fit">
            <Plus />
            选择规则
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-[min(92vw,460px)] p-0">
          <Command>
            <CommandInput value={search} onValueChange={setSearch} placeholder="搜索编号、说明或组名，也可直接输入编号" />
            <CommandList className="max-h-[min(60vh,380px)]">
              <CommandEmpty>{loading ? "正在加载规则目录" : "没有匹配的规则"}</CommandEmpty>
              {manual !== null && !byId.has(manual) ? (
                <CommandGroup heading="手动添加">
                  <CommandItem
                    forceMount
                    value={`manual ${manual}`}
                    onSelect={() => {
                      if (!selected.has(manual)) onChange([...value, manual]);
                      setSearch("");
                    }}
                  >
                    <Hash />
                    <span className="font-mono text-xs">#{manual}</span>
                    <span className="text-xs text-muted-foreground">目录里没有这条规则</span>
                  </CommandItem>
                </CommandGroup>
              ) : null}
              {groups.map(([code, items]) => (
                <CommandGroup key={code} heading={`${groupName(code)} · ${code}`}>
                  {items.map((rule) => (
                    <CommandItem
                      key={rule.id}
                      value={`${rule.id} ${rule.message} ${groupName(rule.group)} ${(rule.tags ?? []).join(" ")}`}
                      onSelect={() => toggle(rule.id)}
                      className="items-start"
                    >
                      <Check className={cn("mt-0.5", selected.has(rule.id) ? "opacity-100" : "opacity-0")} />
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span className="flex min-w-0 items-center gap-1.5">
                          <RuleIdPill id={rule.id} />
                          <RuleSeverityBadge severity={rule.severity} />
                          <span className="font-mono text-[10px] text-muted-foreground">PL{rule.paranoia}</span>
                          {!rule.active ? <span className="text-[10px] text-muted-foreground">未生效</span> : null}
                        </span>
                        <span className={cn("line-clamp-2 text-xs", !rule.active && "text-muted-foreground")}>{rule.message || "—"}</span>
                      </div>
                    </CommandItem>
                  ))}
                </CommandGroup>
              ))}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}
