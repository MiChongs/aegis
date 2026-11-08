"use client";

import Link from "next/link";
import { Crosshair, ExternalLink, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { GraphNode } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

import { KIND_COLOR, describe, isBanned, isWeakIP } from "./graph-model";

/** 用户点 ID 是 u:<应用>:<用户> */
export function parseUserVid(id: string) {
  const [, app, uid] = id.split(":");
  return { app: Number(app), uid: Number(uid) };
}

/** 选中节点的说明卡：一句话身份 + 若干条人话事实 + 操作 */
export function NodeDetail({
  node, degree, appKey, appId, onClose, onExpand, className,
}: {
  node: GraphNode;
  degree: number;
  appKey?: string;
  appId?: number;
  onClose?: () => void;
  onExpand?: (n: GraphNode) => void;
  className?: string;
}) {
  const d = describe(node, degree);
  const banned = isBanned(node);
  const weak = isWeakIP(node);
  const user = node.kind === "user" ? parseUserVid(node.id) : null;
  const sameApp = user && appId !== undefined && user.app === appId;
  return (
    <div className={cn("rounded-xl border bg-card p-4", className)}>
      <div className="flex items-start gap-3">
        <span
          className="mt-0.5 size-9 shrink-0 rounded-full ring-4 ring-background"
          style={{ background: weak ? "#94a3b8" : KIND_COLOR[node.kind], boxShadow: banned ? "0 0 0 2px #ef4444" : undefined }}
        />
        <div className="min-w-0 flex-1">
          <div className={cn("truncate text-sm font-semibold", banned && "text-red-600 dark:text-red-400")}>{d.title}</div>
          <div className="truncate text-xs text-muted-foreground">{d.subtitle}</div>
        </div>
        {onClose ? (
          <Button size="icon" variant="ghost" className="size-7 shrink-0" onClick={onClose} aria-label="关闭">
            <X className="size-4" />
          </Button>
        ) : null}
      </div>
      {d.facts.length ? (
        <dl className="mt-3 space-y-2 text-xs">
          {d.facts.map((f) => (
            <div key={f.label + f.value} className="grid grid-cols-[88px_minmax(0,1fr)] gap-2">
              <dt className="text-muted-foreground">{f.label}</dt>
              <dd
                className={cn(
                  "break-words",
                  f.tone === "danger" && "text-red-600 dark:text-red-400",
                  f.tone === "warning" && "text-amber-600 dark:text-amber-400",
                  f.tone === "muted" && "text-muted-foreground",
                )}
              >
                {f.value}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      <div className="mt-4 flex flex-wrap gap-2">
        {onExpand ? (
          <Button size="sm" variant="outline" className="h-8" onClick={() => onExpand(node)}>
            <Crosshair className="size-3.5" />
            以它为中心展开
          </Button>
        ) : null}
        {sameApp && appKey ? (
          <Button asChild size="sm" variant="outline" className="h-8">
            <Link href={`/app-users/${appKey}/${user.uid}?tab=risk`}>
              <ExternalLink className="size-3.5" />
              打开这位用户
            </Link>
          </Button>
        ) : user && appId !== undefined ? (
          <span className="self-center text-xs text-muted-foreground">属于应用 #{user.app}</span>
        ) : null}
      </div>
    </div>
  );
}
