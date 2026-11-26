"use client";

import { useState } from "react";
import { ChevronRight, Globe2, MapPinned, ShieldBan, Zap } from "lucide-react";
import { GeoBanPanel } from "@/components/security/geo-ban-panel";
import { IPBanPanel } from "@/components/security/ip-ban-panel";
import { Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { cn } from "@/lib/utils";

/**
 * 防火墙 → 封禁。IP 封禁与地域封禁沿用 security/ 下的两个面板，这里只负责切换与讲清顺序：
 * 入口段里封禁排在白 / 黑名单之后、可信网段之前（可信网段也挡不住封禁）。
 * 自动封禁只数 action = block 的事件，观察模式下的「本该拦截」不计数（docs/firewall.md §6）。
 */

type View = "ip" | "geo";

const ORDER = [
  { icon: ShieldBan, label: "IP 精确封禁", hint: "手动或自动封禁的单个地址" },
  { icon: Globe2, label: "地域规则", hint: "国家、省份、城市、ASN、运营商" },
  { icon: MapPinned, label: "地理围栏", hint: "按坐标范围" }
] as const;

export function FirewallBansTab() {
  const [view, setView] = useState<View>("ip");
  return (
    <TabStack>
      <div className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-card p-3 sm:p-4">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <Segmented
            value={view}
            onChange={setView}
            size="default"
            ariaLabel="封禁类型"
            options={[
              { value: "ip", label: "IP 封禁" },
              { value: "geo", label: "地域封禁" }
            ]}
          />
          <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
            <Zap className="size-3.5 shrink-0" />
            自动封禁只统计真实拦截，观察模式的命中不计数
          </span>
        </div>
        <ol className="flex min-w-0 flex-col gap-1.5 @2xl:flex-row @2xl:items-stretch" aria-label="封禁的判定顺序">
          {ORDER.map((step, index) => {
            const Icon = step.icon;
            const current = (view === "ip" && index === 0) || (view === "geo" && index === 1);
            return (
              <li key={step.label} className="flex min-w-0 items-center gap-1.5 @2xl:flex-1">
                <div
                  className={cn(
                    "flex min-w-0 flex-1 items-center gap-2 rounded-lg border border-border px-2.5 py-2",
                    current && "border-primary/50 bg-primary/5"
                  )}
                >
                  <span className="grid size-5 shrink-0 place-items-center rounded-full bg-muted text-[11px] font-semibold tabular-nums">{index + 1}</span>
                  <Icon className={cn("size-4 shrink-0", current ? "text-primary" : "text-muted-foreground")} />
                  <span className="min-w-0">
                    <span className="block truncate text-xs font-medium">{step.label}</span>
                    <span className="block truncate text-[11px] text-muted-foreground">{step.hint}</span>
                  </span>
                </div>
                {index < ORDER.length - 1 ? <ChevronRight className="hidden size-4 shrink-0 text-muted-foreground @2xl:block" aria-hidden /> : null}
              </li>
            );
          })}
        </ol>
        <p className="text-xs leading-5 text-muted-foreground">
          按上述顺序判定，先命中的生效。封禁排在可信网段之前，可信网段也会被封禁挡住；关闭整个防火墙后封禁不再生效。
        </p>
      </div>

      {view === "ip" ? <IPBanPanel /> : <GeoBanPanel />}
    </TabStack>
  );
}
