"use client";

import { useMemo, useState } from "react";
import { ArrowRight, Boxes, ListX, Lock, Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Panel } from "@/components/traffic/traffic-kit";
import type { FirewallCatalog, FirewallRuleGroup, FirewallRuleInfo } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";
import { RuleIdPill, RuleSeverityBadge, groupName } from "../firewall-kit";
import { FIREWALL_DEFAULTS, Hint, RestoreDefault, type SettingsForm, type SettingsSetter, type SettingsUpdater } from "./settings-draft";
import { KIND_LABEL, makeRuleStatus, type FirewallTab } from "./waf-shared";

/* ── 规则组 ── */

const SECTIONS: { title: string; kinds: FirewallRuleGroup["kind"][] }[] = [
  { title: "请求检测", kinds: ["request"] },
  { title: "核心与 Aegis 专属", kinds: ["aegis", "core"] },
  { title: "出站检测", kinds: ["response"] }
];

export function RuleGroupsPanel({
  form,
  update,
  catalog,
  dimmed
}: {
  form: SettingsForm;
  update: SettingsUpdater;
  catalog?: FirewallCatalog;
  dimmed: boolean;
}) {
  const disabled = useMemo(() => new Set(form.corazaDisabledGroups), [form.corazaDisabledGroups]);

  const activeByGroup = useMemo(() => {
    const out = new Map<string, number>();
    if (!catalog) return out;
    const status = makeRuleStatus(form, catalog.groups);
    for (const rule of catalog.rules) {
      if (status(rule) === "active") out.set(rule.group, (out.get(rule.group) ?? 0) + 1);
    }
    return out;
  }, [catalog, form]);

  const toggle = (code: string, enabled: boolean) =>
    update((current) => {
      const rest = current.corazaDisabledGroups.filter((item) => item !== code);
      return { ...current, corazaDisabledGroups: enabled ? rest : [...rest, code].sort() };
    });

  const offCount = catalog ? catalog.groups.filter((group) => !group.locked && disabled.has(group.code)).length : 0;
  const activeTotal = [...activeByGroup.values()].reduce((sum, count) => sum + count, 0);

  return (
    <Panel
      icon={Boxes}
      tone="primary"
      title="规则组"
      className={cn(dimmed && "opacity-60")}
      aside={
        catalog ? (
          <span className="text-xs text-muted-foreground tabular-nums">
            {activeTotal} / {catalog.totalRules} 条生效{offCount ? ` · 已停用 ${offCount} 组` : ""}
          </span>
        ) : null
      }
    >
      {!catalog ? (
        <div className="grid gap-2 @2xl:grid-cols-2 @5xl:grid-cols-3">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-[104px] rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="flex min-w-0 flex-col gap-5">
          {SECTIONS.map((section) => {
            const groups = catalog.groups.filter((group) => section.kinds.includes(group.kind));
            if (!groups.length) return null;
            const isResponse = section.kinds.includes("response");
            return (
              <section key={section.title} className="flex min-w-0 flex-col gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <h4 className="text-xs font-medium text-muted-foreground">{section.title}</h4>
                  {isResponse && !form.corazaResponseInspection ? (
                    <span className="rounded bg-amber-500/12 px-1.5 text-[11px] leading-5 text-amber-700 dark:text-amber-300">出站检测未开启，以下各组不生效</span>
                  ) : null}
                </div>
                <div className="grid min-w-0 gap-2 @2xl:grid-cols-2 @5xl:grid-cols-3">
                  {groups.map((group) => (
                    <GroupCard
                      key={group.code}
                      group={group}
                      enabled={group.locked || !disabled.has(group.code)}
                      inert={group.kind === "response" && !form.corazaResponseInspection}
                      active={activeByGroup.get(group.code) ?? 0}
                      onToggle={(next) => toggle(group.code, next)}
                    />
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </Panel>
  );
}

function GroupCard({
  group,
  enabled,
  inert,
  active,
  onToggle
}: {
  group: FirewallRuleGroup;
  enabled: boolean;
  /** 出站组在出站检测关闭时：开关照常能改，但当前不生效 */
  inert: boolean;
  active: number;
  onToggle: (enabled: boolean) => void;
}) {
  const live = enabled && !inert;
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5 rounded-xl border border-border p-3 transition-colors", !live && "bg-muted/40")}>
      <div className="flex min-w-0 items-center gap-2">
        <span className="inline-flex h-5 shrink-0 items-center rounded-md bg-muted px-1.5 font-mono text-[11px] tabular-nums">{group.code}</span>
        <span className={cn("min-w-0 flex-1 truncate text-sm font-medium", !live && "text-muted-foreground")}>{group.name || groupName(group.code)}</span>
        {group.locked ? (
          <span title="不能停用" className="grid size-6 shrink-0 place-items-center text-muted-foreground">
            <Lock className="size-3.5" />
          </span>
        ) : (
          <Switch size="sm" checked={enabled} onCheckedChange={onToggle} aria-label={`${group.name || group.code}`} />
        )}
      </div>
      {group.description ? <p className="line-clamp-2 text-xs leading-5 text-muted-foreground">{group.description}</p> : null}
      <div className="mt-auto flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground tabular-nums">
        <span className="rounded border border-border px-1 leading-4">{KIND_LABEL[group.kind] ?? group.kind}</span>
        <span>
          {live ? active : 0} / {group.ruleCount} 条生效
        </span>
        {group.file ? <span className="min-w-0 truncate font-mono opacity-70">{group.file}</span> : null}
      </div>
    </div>
  );
}

/* ── 全局移除的规则 ── */

export function RemovedRulesPanel({
  form,
  set,
  catalog,
  onNavigate,
  onOpenRule,
  dimmed
}: {
  form: SettingsForm;
  set: SettingsSetter;
  catalog?: FirewallCatalog;
  onNavigate: (tab: FirewallTab) => void;
  onOpenRule: (id: number) => void;
  dimmed: boolean;
}) {
  const ruleMap = useMemo(() => new Map((catalog?.rules ?? []).map((rule) => [rule.id, rule])), [catalog]);
  const removed = form.corazaRemovedRules;
  const remove = (id: string) => set("corazaRemovedRules", removed.filter((item) => item !== id));
  const add = (id: number) => {
    const text = String(id);
    if (removed.includes(text)) return;
    set("corazaRemovedRules", [...removed, text].sort((a, b) => Number(a) - Number(b)));
  };

  return (
    <Panel
      icon={ListX}
      tone="rose"
      title="全局移除的规则"
      className={cn(dimmed && "opacity-60")}
      aside={
        <>
          <span className="text-xs text-muted-foreground tabular-nums">{removed.length} 条</span>
          <RestoreDefault current={removed} defaults={FIREWALL_DEFAULTS.corazaRemovedRules} onRestore={(next) => set("corazaRemovedRules", next)} />
          <RulePicker rules={catalog?.rules ?? []} exclude={removed} onPick={add} />
        </>
      }
    >
      <div className="mb-3 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 rounded-xl border border-amber-500/30 bg-amber-500/8 px-3 py-2">
        <Hint tone="warn" className="min-w-0 flex-1">
          全局移除会让所有接口都失去这条检测。只在某个路径或字段上误报的，请改用 WAF 例外。
        </Hint>
        <Button type="button" size="xs" variant="outline" onClick={() => onNavigate("exclusions")}>
          WAF 例外
          <ArrowRight />
        </Button>
      </div>
      {removed.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border py-6 text-center text-xs text-muted-foreground">没有全局移除任何规则</div>
      ) : (
        <div className="grid min-w-0 gap-1.5 @3xl:grid-cols-2">
          {removed.map((id) => {
            const rule = ruleMap.get(Number(id));
            return (
              <div key={id} className="flex min-w-0 items-center gap-2 rounded-lg border border-border px-2 py-1.5">
                <RuleIdPill id={Number(id)} onClick={() => onOpenRule(Number(id))} />
                {rule ? <RuleSeverityBadge severity={rule.severity} /> : null}
                <span className={cn("min-w-0 flex-1 truncate text-xs", !rule && "text-muted-foreground")} title={rule?.message}>
                  {rule ? rule.message : catalog ? "目录中没有这条规则" : "…"}
                </span>
                <button
                  type="button"
                  aria-label={`恢复规则 ${id}`}
                  onClick={() => remove(id)}
                  className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
                >
                  <X className="size-3.5" />
                </button>
              </div>
            );
          })}
        </div>
      )}
    </Panel>
  );
}

const PICKER_LIMIT = 50;

function RulePicker({ rules, exclude, onPick }: { rules: FirewallRuleInfo[]; exclude: string[]; onPick: (id: number) => void }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const { items, total } = useMemo(() => {
    const skip = new Set(exclude.map(Number));
    const q = query.trim().toLowerCase();
    const matched = rules.filter((rule) => {
      if (skip.has(rule.id)) return false;
      if (!q) return true;
      return (
        String(rule.id).startsWith(q.replace(/^#/, "")) ||
        rule.message.toLowerCase().includes(q) ||
        groupName(rule.group).toLowerCase().includes(q) ||
        (rule.tags ?? []).some((tag) => tag.toLowerCase().includes(q))
      );
    });
    return { items: matched.slice(0, PICKER_LIMIT), total: matched.length };
  }, [exclude, query, rules]);

  const rawId = /^#?\d{5,7}$/.test(query.trim()) ? Number(query.trim().replace(/^#/, "")) : null;
  const rawUnknown = rawId !== null && !rules.some((rule) => rule.id === rawId) && !exclude.includes(String(rawId));

  const pick = (id: number) => {
    onPick(id);
    setOpen(false);
    setQuery("");
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button type="button" size="xs" variant="outline">
          <Plus />
          移除规则
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(92vw,440px)] p-0">
        <Command shouldFilter={false}>
          <CommandInput value={query} onValueChange={setQuery} placeholder="规则编号、说明、标签或组名" />
          <CommandList className="max-h-[320px]">
            <CommandEmpty>没有匹配的规则</CommandEmpty>
            {rawUnknown && rawId !== null ? (
              <CommandGroup>
                <CommandItem value={`raw-${rawId}`} onSelect={() => pick(rawId)}>
                  按编号移除 #{rawId}
                </CommandItem>
              </CommandGroup>
            ) : null}
            {items.length ? (
              <CommandGroup heading={total > items.length ? `前 ${items.length} 条 / 共 ${total} 条` : `${total} 条`}>
                {items.map((rule) => (
                  <CommandItem key={rule.id} value={String(rule.id)} onSelect={() => pick(rule.id)} className="items-start">
                    <span className="mt-0.5 shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">#{rule.id}</span>
                    <span className="min-w-0 flex-1">
                      <span className="line-clamp-2 text-xs">{rule.message}</span>
                      <span className="mt-0.5 block text-[11px] text-muted-foreground">
                        {groupName(rule.group)} · PL{rule.paranoia}
                      </span>
                    </span>
                    <RuleSeverityBadge severity={rule.severity} className="mt-0.5" />
                  </CommandItem>
                ))}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
