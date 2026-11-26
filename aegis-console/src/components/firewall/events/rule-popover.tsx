"use client";

import { useState } from "react";
import { ListFilter } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { groupName, groupOfRule, PARANOIA_META, RuleIdPill, RuleSeverityBadge } from "@/components/firewall/firewall-kit";
import { firewallErrorMessage, useFirewallCatalogRuleQuery } from "@/lib/firewall-hooks";
import { formatCompact } from "@/lib/datetime";

/**
 * 点规则编号看规则本身：目录里的说明、所在组、偏执等级、是否生效，以及最近命中。
 * 只在打开时请求（详情接口会顺带查最近 5 条事件）。
 */
export function RulePopover({ id, onFilter }: { id: number; onFilter?: (id: number) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`查看规则 ${id}`}
          className="inline-flex rounded-md focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <RuleIdPill id={id} className="hover:bg-accent hover:text-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(22rem,calc(100vw-2rem))] p-0">
        {open ? (
          <RuleDetail
            id={id}
            onFilter={
              onFilter
                ? () => {
                    setOpen(false);
                    onFilter(id);
                  }
                : undefined
            }
          />
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

function RuleDetail({ id, onFilter }: { id: number; onFilter?: () => void }) {
  const query = useFirewallCatalogRuleQuery(id);
  const rule = query.data;

  if (query.isError) {
    return (
      <div className="flex flex-col gap-1 p-3">
        <div className="flex items-center gap-2">
          <RuleIdPill id={id} />
          <span className="text-xs text-muted-foreground">{groupName(groupOfRule(id))}</span>
        </div>
        <p className="text-xs text-muted-foreground">{firewallErrorMessage(query.error, "规则目录中没有这条规则")}</p>
      </div>
    );
  }

  if (!rule) {
    return (
      <div className="flex flex-col gap-2 p-3">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-4 w-48" />
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      <div className="flex flex-col gap-2 p-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <RuleIdPill id={rule.id} />
          <RuleSeverityBadge severity={rule.severity} />
          <span className="text-xs font-medium">{rule.groupName || groupName(rule.group)}</span>
          {rule.removed ? (
            <Badge variant="outline" size="sm">
              已移除
            </Badge>
          ) : rule.active ? (
            <Badge variant="success" size="sm">
              生效中
            </Badge>
          ) : (
            <Badge variant="outline" size="sm">
              未生效
            </Badge>
          )}
        </div>
        <p className="text-sm leading-5">{rule.message || "—"}</p>
        <dl className="grid grid-cols-3 gap-2 text-[11px]">
          <div>
            <dt className="text-muted-foreground">偏执等级</dt>
            <dd className="font-medium" title={PARANOIA_META[rule.paranoia]?.hint}>
              {rule.paranoia ? `PL${rule.paranoia}` : "—"}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">阶段</dt>
            <dd className="font-medium tabular-nums">{rule.phase || "—"}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">最近命中</dt>
            <dd className="font-medium tabular-nums">{rule.recentTotal.toLocaleString("zh-CN")} 次</dd>
          </div>
        </dl>
        {rule.tags && rule.tags.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {rule.tags.map((tag) => (
              <span key={tag} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                {tag}
              </span>
            ))}
          </div>
        ) : null}
      </div>
      {rule.recent.length > 0 ? (
        <ul className="flex flex-col border-t px-3 py-2">
          {rule.recent.map((event) => (
            <li key={event.id} className="flex min-w-0 items-center gap-2 py-1 text-[11px]">
              <span className="w-16 shrink-0 text-muted-foreground tabular-nums">{formatCompact(event.blockedAt)}</span>
              <span className="min-w-0 flex-1 truncate font-mono" title={event.path}>
                {event.method} {event.path}
              </span>
              <span className="shrink-0 font-mono text-muted-foreground">{event.ip}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {onFilter ? (
        <div className="border-t p-2">
          <Button size="sm" variant="ghost" className="h-7 w-full justify-start text-xs" onClick={onFilter}>
            <ListFilter className="size-3.5" />
            只看命中这条规则的事件
          </Button>
        </div>
      ) : null}
    </div>
  );
}
