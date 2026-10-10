"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { History, Users } from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  CONSENT_SOURCE_OPTIONS,
  ConsentBadge,
  consentSourceLabel
} from "@/components/apps/ad-policy/ad-policy-shared";
import { formatCardDate } from "@/components/apps/card-key/card-key-shared";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AdConsentSource } from "@/lib/api/ad-policy";
import { useAdConsentLogsQuery, useAdConsentsQuery } from "@/lib/ad-policy-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";

const PAGE_SIZE = 20;

type ChoiceFilter = "all" | "accepted" | "declined" | "outdated";

/**
 * 用户的选择。
 *
 * 「当前」每人一行，回答「谁现在是什么状态」；「历史」逐次变更，回答「他什么时候、
 * 在哪里同意或撤回过」—— 用户投诉被限制功能、或被问到凭什么展示广告时看这里。
 */
export function AdPolicyConsentsPanel({ appKey }: { appKey: string }) {
  const [view, setView] = useState<"current" | "history">("current");
  const [keyword, setKeyword] = useState("");
  const [choice, setChoice] = useState<ChoiceFilter>("all");
  const [source, setSource] = useState<AdConsentSource | undefined>(undefined);
  const [page, setPage] = useState(1);
  const debouncedKeyword = useDebouncedValue(keyword, 300);

  const consentsQuery = useAdConsentsQuery(view === "current" ? appKey : null, {
    accepted: choice === "accepted" ? true : choice === "declined" ? false : undefined,
    outdated: choice === "outdated" ? true : undefined,
    source,
    keyword: debouncedKeyword || undefined,
    page,
    limit: PAGE_SIZE
  });
  const logsQuery = useAdConsentLogsQuery(view === "history" ? appKey : null, {
    keyword: debouncedKeyword || undefined,
    page,
    limit: PAGE_SIZE
  });

  const activeQuery = view === "current" ? consentsQuery : logsQuery;
  const total = activeQuery.data?.total ?? 0;
  const totalPages = Math.max(1, activeQuery.data?.totalPages ?? Math.ceil(total / PAGE_SIZE));
  const consents = useMemo(() => consentsQuery.data?.items ?? [], [consentsQuery.data]);
  const logs = useMemo(() => logsQuery.data?.items ?? [], [logsQuery.data]);
  const filtered = Boolean(keyword || (view === "current" && (choice !== "all" || source)));
  const userHref = (userId?: number) =>
    userId ? `/app-users/${encodeURIComponent(appKey)}/${userId}?tab=ad-policy` : undefined;

  return (
    <SectionCard
      icon={view === "current" ? <Users className="size-4" /> : <History className="size-4" />}
      title="用户选择"
      aside={
        <ToggleGroup
          type="single"
          size="sm"
          value={view}
          onValueChange={(value) => {
            if (!value) return;
            setView(value as "current" | "history");
            setPage(1);
          }}
        >
          <ToggleGroupItem value="current" className="h-7 px-2.5 text-xs">
            当前
          </ToggleGroupItem>
          <ToggleGroupItem value="history" className="h-7 px-2.5 text-xs">
            历史
          </ToggleGroupItem>
        </ToggleGroup>
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号或设备"
          className="h-8 w-56"
        />
        {view === "current" ? (
          <>
            <Select
              value={choice}
              onValueChange={(value) => {
                setChoice(value as ChoiceFilter);
                setPage(1);
              }}
            >
              <SelectTrigger className="h-8 w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部选择</SelectItem>
                <SelectItem value="accepted">已同意</SelectItem>
                <SelectItem value="declined">已拒绝</SelectItem>
                <SelectItem value="outdated">待重新选择</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={source ?? "all"}
              onValueChange={(value) => {
                setSource(value === "all" ? undefined : (value as AdConsentSource));
                setPage(1);
              }}
            >
              <SelectTrigger className="h-8 w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部来源</SelectItem>
                {CONSENT_SOURCE_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        ) : null}
      </div>

      {activeQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : (view === "current" ? consents.length : logs.length) === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无记录"}
        </p>
      ) : view === "current" ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>账号</TableHead>
              <TableHead>选择</TableHead>
              <TableHead>条款版本</TableHead>
              <TableHead>来源</TableHead>
              <TableHead className="text-right">时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {consents.map((item) => (
              <TableRow key={item.userId}>
                <TableCell className="text-xs">
                  <AccountLink href={userHref(item.userId)} label={item.account || `#${item.userId ?? ""}`} />
                  {item.deviceId ? (
                    <p className="mt-0.5 max-w-44 truncate font-mono text-[11px] text-muted-foreground">{item.deviceId}</p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <ConsentBadge accepted={item.accepted} outdated={item.outdated} />
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums">v{item.version}</TableCell>
                <TableCell className="text-xs text-muted-foreground">{consentSourceLabel(item.source)}</TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {formatCardDate(item.decidedAt)}
                  {item.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{item.clientIp}</p> : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>账号</TableHead>
              <TableHead>选择</TableHead>
              <TableHead>条款版本</TableHead>
              <TableHead>来源</TableHead>
              <TableHead className="text-right">时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {logs.map((item) => (
              <TableRow key={item.id}>
                <TableCell className="text-xs">
                  <AccountLink href={userHref(item.userId)} label={item.account || `#${item.userId}`} />
                  {item.deviceId ? (
                    <p className="mt-0.5 max-w-44 truncate font-mono text-[11px] text-muted-foreground">{item.deviceId}</p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <ConsentBadge accepted={item.accepted} />
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums">v{item.version}</TableCell>
                <TableCell className="text-xs text-muted-foreground">{consentSourceLabel(item.source)}</TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {formatCardDate(item.createdAt)}
                  {item.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{item.clientIp}</p> : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <AdPolicyPager total={total} page={page} totalPages={totalPages} onPage={setPage} />
    </SectionCard>
  );
}

function AccountLink({ href, label }: { href?: string; label: string }) {
  if (!href) return <span>{label}</span>;
  return (
    <Link href={href} className="font-medium hover:underline">
      {label}
    </Link>
  );
}
