"use client";

/**
 * 效果与记录：近 14 天规则作用前后的对比柱、近 30 天各来源的收入（规则作用前 → 实际入账）、调控记录时间线。
 * 柱图是纯 div：数据最多 14 根，没必要拉起图表库。
 */

import * as React from "react";
import { ArrowRight, BarChart3, History, Layers } from "lucide-react";
import type { GrowthCatalog, GrowthEvent, GrowthKind, GrowthSourceStat, GrowthUsage } from "@/lib/api/growth";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { Section, StateBlock } from "../kit";
import { Segmented } from "../user-assets-adjust";

const ACTION_META: Record<string, { label: string; dot: string }> = {
  rule_create: { label: "新建规则", dot: "bg-primary" },
  rule_update: { label: "修改规则", dot: "bg-sky-500" },
  rule_revoke: { label: "撤销规则", dot: "bg-muted-foreground" },
  rule_delete: { label: "删除规则", dot: "bg-muted-foreground" },
  level_cap: { label: "等级封顶", dot: "bg-red-500" },
  level_uncap: { label: "解除封顶", dot: "bg-emerald-500" }
};

function lastDays(count: number) {
  const out: string[] = [];
  const today = new Date();
  for (let i = count - 1; i >= 0; i--) {
    const d = new Date(today.getFullYear(), today.getMonth(), today.getDate() - i);
    out.push(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`);
  }
  return out;
}

export function GrowthTrend({ usage }: { usage: GrowthUsage[] }) {
  const [kind, setKind] = React.useState<GrowthKind>("integral");
  const days = React.useMemo(() => lastDays(14), []);
  const rows = days.map((day) => usage.find((item) => item.day === day && item.kind === kind) ?? { day, kind, base: 0, granted: 0, suppressed: 0, events: 0 });
  const peak = Math.max(1, ...rows.map((row) => Math.max(row.base, row.granted)));
  const totals = rows.reduce((acc, row) => ({ base: acc.base + row.base, granted: acc.granted + row.granted }), { base: 0, granted: 0 });
  const delta = totals.granted - totals.base;
  return (
    <Section
      title="规则作用效果"
      icon={BarChart3}
      tone="info"
      description="近 14 天受规则作用的收入：浅色是规则作用前，深色是实际入账"
      actions={
        <Segmented
          value={kind}
          onChange={setKind}
          options={[
            { value: "integral", label: "积分" },
            { value: "experience", label: "经验" }
          ]}
        />
      }
    >
      {totals.base === 0 ? (
        <StateBlock icon={BarChart3} kind="empty" title="近 14 天没有被规则作用的收入" description="规则生效后，每一笔受管收入都会在这里留下对比。" />
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-sm">
            <span className="text-muted-foreground">
              规则作用前 <span className="font-semibold text-foreground tabular-nums">{totals.base.toLocaleString("zh-CN")}</span>
            </span>
            <ArrowRight className="size-3.5 self-center text-muted-foreground" />
            <span className="text-muted-foreground">
              实际入账 <span className="font-semibold text-foreground tabular-nums">{totals.granted.toLocaleString("zh-CN")}</span>
            </span>
            <span className={cn("text-xs font-medium tabular-nums", delta < 0 ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400")}>
              {delta < 0 ? "" : "+"}
              {delta.toLocaleString("zh-CN")}
            </span>
          </div>
          <div className="flex h-36 items-end gap-1.5">
            {rows.map((row) => (
              <div key={row.day} className="group flex min-w-0 flex-1 flex-col items-center gap-1" title={`${row.day}：作用前 ${row.base}，实际 ${row.granted}，${row.events} 笔`}>
                <div className="relative flex h-28 w-full items-end justify-center">
                  <div className="absolute bottom-0 w-full rounded-t-[4px] bg-muted-foreground/15" style={{ height: `${(row.base / peak) * 100}%` }} />
                  <div
                    className={cn("relative w-3/5 rounded-t-[4px] transition-[height] duration-500", row.granted < row.base ? "bg-amber-500" : "bg-emerald-500")}
                    style={{ height: `${(row.granted / peak) * 100}%` }}
                  />
                </div>
                <span className="text-[10px] text-muted-foreground tabular-nums">{row.day.slice(8)}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Section>
  );
}

export function GrowthSourceStats({
  integral,
  experience,
  catalog,
  days
}: {
  integral: GrowthSourceStat[];
  experience: GrowthSourceStat[];
  catalog?: GrowthCatalog;
  days: number;
}) {
  const [kind, setKind] = React.useState<GrowthKind>("integral");
  const stats = kind === "integral" ? integral : experience;
  const peak = Math.max(1, ...stats.map((item) => Math.max(item.base, item.granted)));
  const label = (key: string) => catalog?.sources.find((s) => s.key === key)?.label ?? SOURCE_FALLBACK[key] ?? key;
  return (
    <Section
      title="收入来源"
      icon={Layers}
      tone="primary"
      description={`近 ${days} 天各来源的收入；有规则作用过的来源显示作用前后的差额`}
      actions={
        <Segmented
          value={kind}
          onChange={setKind}
          options={[
            { value: "integral", label: "积分" },
            { value: "experience", label: "经验" }
          ]}
        />
      }
    >
      {!stats.length ? (
        <StateBlock icon={Layers} kind="empty" title={`近 ${days} 天没有${kind === "integral" ? "积分" : "经验"}收入`} />
      ) : (
        <ul className="flex flex-col gap-3">
          {stats.map((item) => {
            const diff = item.granted - item.base;
            return (
              <li key={item.source} className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate">{label(item.source)}</span>
                    <span className="text-xs text-muted-foreground tabular-nums">{item.count} 笔</span>
                    {item.touched > 0 ? <span className="text-[11px] text-muted-foreground">规则作用 {item.touched} 笔</span> : null}
                  </span>
                  <span className="flex shrink-0 items-baseline gap-1.5 tabular-nums">
                    {diff !== 0 ? <span className="text-xs text-muted-foreground line-through">{item.base.toLocaleString("zh-CN")}</span> : null}
                    <span className="font-semibold">{item.granted.toLocaleString("zh-CN")}</span>
                    {diff !== 0 ? (
                      <span className={cn("text-xs", diff < 0 ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400")}>
                        {diff > 0 ? "+" : ""}
                        {diff.toLocaleString("zh-CN")}
                      </span>
                    ) : null}
                  </span>
                </div>
                <div className="relative h-1.5 overflow-hidden rounded-full bg-muted">
                  <div className="absolute inset-y-0 left-0 rounded-full bg-muted-foreground/20" style={{ width: `${(item.base / peak) * 100}%` }} />
                  <div
                    className={cn("absolute inset-y-0 left-0 rounded-full", diff < 0 ? "bg-amber-500" : diff > 0 ? "bg-emerald-500" : "bg-primary")}
                    style={{ width: `${(item.granted / peak) * 100}%` }}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}

/** 不在受管来源目录里的流水来源（管理员调整、充值……）。 */
const SOURCE_FALLBACK: Record<string, string> = {
  admin: "管理员调整",
  admin_adjust: "管理员调整",
  admin_manual: "管理员调整",
  payment_order: "充值",
  vip_transaction: "会员赠送",
  vip_conversion: "会员折算",
  points_store: "积分商城",
  ai_gateway: "AI 调用",
  refund: "退回"
};

export function GrowthTimeline({ events }: { events: GrowthEvent[] }) {
  return (
    <Section title="调控记录" icon={History} tone="neutral" description="这个用户与全应用的规则变化、等级封顶与解除">
      {!events.length ? (
        <StateBlock icon={History} kind="empty" title="还没有调控记录" />
      ) : (
        <ol className="relative flex flex-col gap-4 before:absolute before:inset-y-1 before:left-[5px] before:w-px before:bg-border">
          {events.map((event) => {
            const meta = ACTION_META[event.action] ?? { label: event.action, dot: "bg-muted-foreground" };
            return (
              <li key={event.id} className="relative flex gap-3 pl-5">
                <span className={cn("absolute top-1.5 left-0 size-[11px] rounded-full border-2 border-background", meta.dot)} />
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <div className="flex flex-wrap items-center gap-x-2 text-sm">
                    <span className="font-medium">{meta.label}</span>
                    {event.userId === 0 ? <span className="text-[11px] text-muted-foreground">全应用</span> : null}
                    <span className="text-xs text-muted-foreground" title={formatDateTime(event.createdAt)}>
                      {formatRelative(event.createdAt)}
                    </span>
                  </div>
                  <p className="text-xs leading-5 text-foreground/80">{event.summary}</p>
                  {event.note || event.operator ? (
                    <p className="text-[11px] text-muted-foreground">
                      {event.operator ? `操作人 ${event.operator}` : ""}
                      {event.note ? `${event.operator ? "，" : ""}${event.note}` : ""}
                    </p>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </Section>
  );
}
