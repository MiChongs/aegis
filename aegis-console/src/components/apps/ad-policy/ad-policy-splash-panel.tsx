"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { MonitorSmartphone } from "lucide-react";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import {
  SPLASH_STATUS_OPTIONS,
  SplashStatusBadge,
  formatMillis
} from "@/components/apps/ad-policy/ad-policy-shared";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { formatCardDate } from "@/components/apps/card-key/card-key-shared";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
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
import type { SplashAdStatus } from "@/lib/api/ad-policy";
import { useSplashAdEventsQuery } from "@/lib/ad-policy-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const PAGE_SIZE = 20;

/**
 * 开屏记录。每一行是一次开屏尝试的结局；登录用户挂在账号上，未登录时只有设备标识。
 * 「用户说开屏太频繁」时按账号筛，看间隔与次数；「开屏一直不出」时按失败筛，看平台给的错误码。
 */
export function AdPolicySplashPanel({ appKey }: { appKey: string }) {
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<SplashAdStatus | undefined>(undefined);
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const debouncedKeyword = useDebouncedValue(keyword, 300);

  const listQuery = useSplashAdEventsQuery(appKey, {
    status,
    keyword: debouncedKeyword || undefined,
    start: range.start,
    end: range.end,
    page,
    limit: PAGE_SIZE
  });
  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? Math.ceil(total / PAGE_SIZE));
  const filtered = Boolean(keyword || status || range.start || range.end);

  return (
    <SectionCard icon={<MonitorSmartphone className="size-4" />} title="开屏记录">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号、设备或错误码"
          className="h-8 w-56"
        />
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            setStatus(value === "all" ? undefined : (value as SplashAdStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue placeholder="全部结果" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部结果</SelectItem>
            {SPLASH_STATUS_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <CommerceRangePicker
          value={range}
          onChange={(next) => {
            setRange(next);
            setPage(1);
          }}
        />
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无开屏记录"}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>账号</TableHead>
              <TableHead>结果</TableHead>
              <TableHead>广告位</TableHead>
              <TableHead>加载 / 展示</TableHead>
              <TableHead className="text-right">时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className="text-xs">
                  {item.userId ? (
                    <Link
                      href={`/app-users/${encodeURIComponent(appKey)}/${item.userId}?tab=ad-policy`}
                      className="font-medium hover:underline"
                    >
                      {item.account || `#${item.userId}`}
                    </Link>
                  ) : (
                    <span className="text-muted-foreground">未登录</span>
                  )}
                  {item.deviceId ? (
                    <p className="mt-0.5 max-w-44 truncate font-mono text-[11px] text-muted-foreground">{item.deviceId}</p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <SplashStatusBadge status={item.status} />
                  {item.errorCode || item.errorMessage ? (
                    <p
                      className="mt-1 max-w-48 truncate text-[11px] text-muted-foreground"
                      title={item.errorMessage || undefined}
                    >
                      {[item.errorCode, item.errorMessage].filter(Boolean).join(" ")}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">{item.placementId || "—"}</TableCell>
                <TableCell className="text-xs tabular-nums text-muted-foreground">
                  {formatMillis(item.loadMs)} / {formatMillis(item.shownMs)}
                </TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {formatCardDate(item.occurredAt)}
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
