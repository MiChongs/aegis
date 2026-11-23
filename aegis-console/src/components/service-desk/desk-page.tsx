"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { motion } from "motion/react";
import { parseAsString, useQueryState } from "nuqs";
import { ArrowUpRight, Keyboard, Plus, type LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import type { TicketKind } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { useDeskApps } from "./desk-app-rail";
import { DeskProvider, useDesk } from "./desk-context";
import { DeskCreateDialog } from "./desk-create-dialog";
import { ShortcutList } from "./desk-overview";
import { useDeskState } from "./desk-state";
import { DeskWorkspace } from "./desk-workspace";

// 服务台页面外壳：标题 + 页签 + 右侧动作，下面是当前页签的内容。
// 「工作台」页签锁满屏高度（滚动条长在各栏自己身上），其余页签按内容自然滚动。
// 页签记在地址栏 ?tab=；工作台是默认值，不写进地址栏。

export type DeskTab = {
  value: string;
  label: string;
  icon: LucideIcon;
  content?: ReactNode;
  /** 指向别处的页签（比如反馈的分类配置在工单中心里），点了直接跳 */
  href?: string;
};

export function DeskPage({ kind, tabs, eyebrow }: { kind: TicketKind; tabs: DeskTab[]; eyebrow?: string }) {
  return (
    <DeskProvider kind={kind}>
      <DeskPageInner tabs={tabs} eyebrow={eyebrow} />
    </DeskProvider>
  );
}

// 旧书签：工单中心以前的「工单台 / 我的待办」两个页签都落到工作台
const LEGACY_DESK_TABS = new Set(["inbox", "mine"]);

function DeskPageInner({ tabs, eyebrow }: { tabs: DeskTab[]; eyebrow?: string }) {
  const desk = useDesk();
  const [rawTab, setTab] = useQueryState("tab", parseAsString.withDefault("desk").withOptions({ history: "replace", clearOnDefault: true }));
  const tab = LEGACY_DESK_TABS.has(rawTab) || !tabs.some((item) => item.value === rawTab && !item.href) ? "desk" : rawTab;
  const [state, setState] = useDeskState();
  const [createOpen, setCreateOpen] = useState(false);
  const { totals } = useDeskApps();
  const onDesk = tab === "desk";

  return (
    <div className={cn("flex flex-col gap-4", onDesk && "h-[calc(100dvh-5.5rem)] min-h-[560px] lg:h-[calc(100dvh-6rem)]")}>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
        <div className="min-w-0">
          {eyebrow ? <p className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">{eyebrow}</p> : null}
          <h1 className="text-xl font-semibold tracking-tight text-foreground">{desk.title}</h1>
        </div>

        <nav aria-label={`${desk.title}页签`} className="flex items-center gap-0.5 rounded-xl bg-muted/70 p-1">
          {tabs.map((item) => {
            const active = !item.href && item.value === tab;
            const inner = (
              <>
                {active ? (
                  <motion.span
                    layoutId={`desk-tab-${desk.kind}`}
                    className="absolute inset-0 rounded-lg bg-background shadow-sm ring-1 ring-border/60"
                    transition={{ type: "spring", stiffness: 500, damping: 40 }}
                  />
                ) : null}
                <item.icon className="relative size-3.5" />
                <span className="relative">{item.label}</span>
                {item.value === "desk" && totals.active > 0 ? (
                  <span className="relative rounded-full bg-primary/10 px-1.5 text-[11px] font-medium leading-4 tabular-nums text-primary">
                    {totals.active > 999 ? "999+" : totals.active}
                  </span>
                ) : null}
                {item.href ? <ArrowUpRight className="relative size-3 opacity-60" /> : null}
              </>
            );
            const className = cn(
              "relative flex h-8 items-center gap-1.5 rounded-lg px-3 text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/40",
              active ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
            );
            return item.href ? (
              <Link key={item.value} href={item.href} className={className}>
                {inner}
              </Link>
            ) : (
              <button key={item.value} type="button" onClick={() => void setTab(item.value)} aria-current={active ? "page" : undefined} className={className}>
                {inner}
              </button>
            );
          })}
        </nav>

        {onDesk ? (
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <Popover>
              <Tooltip>
                <TooltipTrigger asChild>
                  <PopoverTrigger asChild>
                    <Button size="icon-sm" variant="ghost" aria-label="快捷键">
                      <Keyboard />
                    </Button>
                  </PopoverTrigger>
                </TooltipTrigger>
                <TooltipContent>快捷键</TooltipContent>
              </Tooltip>
              <PopoverContent align="end" className="w-80">
                <ShortcutList />
              </PopoverContent>
            </Popover>
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Plus />
              新建{desk.noun}
            </Button>
          </div>
        ) : null}
      </div>

      {onDesk ? (
        <DeskWorkspace state={state} setState={setState} />
      ) : (
        <WidgetBoundary title="加载失败">{tabs.find((item) => item.value === tab)?.content}</WidgetBoundary>
      )}

      <DeskCreateDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(id) => void setState({ id })}
        defaultApp={state.app}
      />
    </div>
  );
}
