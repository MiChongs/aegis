"use client";

import { useState } from "react";
import Link from "next/link";
import { BadgeCheck, ChevronRight, Globe, Mail, Phone, Smartphone, UserPlus, Users, Wallet } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { GraphNode } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

import { REASON_LABEL, displayName, isBanned, providerName, seen, type Reason, type Relation, type RelationStats } from "./graph-model";
import { parseUserVid } from "./node-detail";

const REASON_ICON: Record<Reason["kind"], React.ComponentType<{ className?: string }>> = {
  device: Smartphone, phone: Phone, identity: BadgeCheck, payer: Wallet, invited_by: UserPlus, invited: UserPlus,
  email: Mail, account: Users, ip: Globe, target: Users,
};

/** 叙述式摘要：一段话 + 几个关键数字 */
export function RelationNarrative({ text, stats }: { text: string; stats: RelationStats }) {
  const chips: Array<{ label: string; value: string }> = [];
  chips.push({ label: "设备", value: String(stats.devices.length) });
  chips.push({ label: "IP", value: stats.weakIPs.length ? `${stats.ips.length}，其中共用 ${stats.weakIPs.length}` : String(stats.ips.length) });
  if (stats.identities.length) chips.push({ label: "第三方账号", value: [...new Set(stats.identities.map((i) => providerName(String(i.props?.provider ?? ""))))].join("、") });
  if (stats.payers.length) chips.push({ label: "付款账号", value: String(stats.payers.length) });
  chips.push({ label: "关联的人", value: String(stats.relations.length) });
  return (
    <div className="rounded-xl bg-muted/40 p-4">
      <p className="text-sm leading-relaxed">{text}</p>
      <div className="mt-3 flex flex-wrap gap-2">
        {chips.map((c) => (
          <span key={c.label} className="inline-flex items-center gap-1.5 rounded-full border bg-background px-2.5 py-1 text-xs">
            <span className="text-muted-foreground">{c.label}</span>
            <span className="font-medium tabular-nums">{c.value}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

function reasonText(r: Reason) {
  const via = r.via ? displayName(r.via) : "";
  switch (r.kind) {
    case "device":
    case "phone":
    case "email":
    case "ip":
    case "account":
      return via ? `${REASON_LABEL[r.kind]}：${via}` : REASON_LABEL[r.kind];
    case "identity":
      return `同一个第三方账号：${via}`;
    case "payer":
      return `同一个付款账号：${via}`;
    default:
      return REASON_LABEL[r.kind];
  }
}

/** 关联的人：按关联强弱排序，每人列出关联理由（经由哪台设备、哪个手机号……） */
export function RelationList({
  relations, appKey, appId, onFocus, focusId,
}: {
  relations: Relation[];
  appKey?: string;
  appId?: number;
  onFocus?: (n: GraphNode) => void;
  focusId?: string | null;
}) {
  const [all, setAll] = useState(false);
  if (!relations.length) {
    return <p className="px-1 text-sm text-muted-foreground">没有发现与其他用户的关联。</p>;
  }
  const shown = all ? relations : relations.slice(0, 8);
  return (
    <div className="space-y-1">
      {shown.map((r) => {
        const name = displayName(r.user);
        const { app, uid } = parseUserVid(r.user.id);
        const banned = isBanned(r.user);
        const last = Math.max(...r.reasons.map((x) => x.lastSeen || 0));
        return (
          <div
            key={r.user.id}
            className={cn("group rounded-lg px-2 py-2 transition-colors hover:bg-muted", focusId === r.user.id && "bg-muted")}
          >
            <div className="flex items-center gap-2">
              <button type="button" className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => onFocus?.(r.user)}>
                <span
                  className={cn(
                    "flex size-7 shrink-0 items-center justify-center rounded-full bg-indigo-500/12 text-xs font-medium text-indigo-600 dark:text-indigo-300",
                    banned && "bg-red-500/12 text-red-600 dark:text-red-400",
                  )}
                >
                  {Array.from(name)[0] ?? "?"}
                </span>
                <span className="min-w-0">
                  <span className={cn("block truncate text-sm", banned && "text-red-600 dark:text-red-400")}>
                    {name}
                    {banned ? <span className="ml-1.5 text-xs">已封禁</span> : null}
                  </span>
                  {last ? <span className="block text-xs text-muted-foreground">最近关联 {seen(last)}</span> : null}
                </span>
              </button>
              {appKey && app === appId ? (
                <Link
                  href={`/app-users/${appKey}/${uid}?tab=risk`}
                  className="shrink-0 rounded-md p-1 text-muted-foreground opacity-0 transition-colors group-hover:opacity-100 hover:text-foreground"
                  aria-label="打开这位用户"
                >
                  <ChevronRight className="size-4" />
                </Link>
              ) : appId !== undefined && app !== appId ? (
                <span className="shrink-0 text-xs text-muted-foreground">应用 #{app}</span>
              ) : null}
            </div>
            <div className="mt-1.5 flex flex-wrap gap-1 pl-9">
              {r.reasons.slice(0, 3).map((x) => {
                const Icon = REASON_ICON[x.kind];
                return (
                  <span key={x.kind + (x.via?.id ?? "")} className="inline-flex max-w-full items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                    <Icon className="size-3 shrink-0" />
                    <span className="truncate">{reasonText(x)}</span>
                  </span>
                );
              })}
              {r.reasons.length > 3 ? <span className="text-xs text-muted-foreground">等 {r.reasons.length} 条</span> : null}
            </div>
          </div>
        );
      })}
      {relations.length > 8 ? (
        <Button size="sm" variant="ghost" className="mt-1 h-8 w-full text-xs" onClick={() => setAll((v) => !v)}>
          {all ? "收起" : `查看全部 ${relations.length} 位`}
        </Button>
      ) : null}
    </div>
  );
}
