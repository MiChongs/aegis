"use client";

import { useMemo, useState } from "react";
import { ListTree, Search } from "lucide-react";
import {
  DeviceText,
  EVENT_KINDS,
  IPText,
  KIND_META,
  KindBadge,
  locationText,
  methodLabel,
  METHODS,
  METHOD_META,
  Pager,
  REASON_META
} from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { RegIPEventKind, RegIPReason } from "@/lib/api/register-ip";
import { formatDateTime, formatRelative, startOfLocalDay } from "@/lib/datetime";
import { useRegIPEventsQuery } from "@/lib/register-ip-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;
export const RANGES = [
  { key: "today", label: "今天", days: 0 },
  { key: "7", label: "7 天", days: 7 },
  { key: "30", label: "30 天", days: 30 },
  { key: "all", label: "全部", days: -1 }
] as const;
export type RangeKey = (typeof RANGES)[number]["key"];

/** 今天 0 点往前推 days 天（浏览器时区）；负数表示不限 */
export function rangeStart(key: RangeKey) {
  const days = RANGES.find((item) => item.key === key)?.days ?? -1;
  if (days < 0) return undefined;
  const start = startOfLocalDay();
  start.setDate(start.getDate() - days);
  return start.toISOString();
}

/** 拦截记录：拒绝、加验、观察、加验后放行、惩罚与解除。地址筛选支持网段 */
export function RegIPEventsPanel({
  appKey,
  onOpenIP,
  onOpenDevice
}: {
  appKey: string;
  onOpenIP: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  const [kind, setKind] = useState<RegIPEventKind | "all">("all");
  const [reason, setReason] = useState<RegIPReason | "all">("all");
  const [method, setMethod] = useState<string>("all");
  const [range, setRange] = useState<RangeKey>("7");
  const [ip, setIP] = useState("");
  const [device, setDevice] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const debouncedIP = useDebouncedValue(ip.trim(), 400);
  const debouncedDevice = useDebouncedValue(device.trim(), 400);
  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const params = useMemo(
    () => ({
      kind: kind === "all" ? undefined : kind,
      reason: reason === "all" ? undefined : reason,
      method: method === "all" ? undefined : method,
      ip: debouncedIP || undefined,
      deviceId: debouncedDevice || undefined,
      keyword: debouncedKeyword || undefined,
      from: rangeStart(range),
      page,
      limit: LIMIT
    }),
    [kind, reason, method, debouncedIP, debouncedDevice, debouncedKeyword, range, page]
  );
  const query = useRegIPEventsQuery(appKey, params);
  const items = query.data?.items ?? [];
  const reset = <T,>(set: (value: T) => void) => (value: T) => {
    set(value);
    setPage(1);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={kind} onValueChange={reset((value: string) => setKind(value as RegIPEventKind | "all"))}>
          <SelectTrigger size="sm" className="w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">全部类型</SelectItem>
            {EVENT_KINDS.map((item) => (
              <SelectItem key={item} value={item} className="text-xs">
                {KIND_META[item].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={reason} onValueChange={reset((value: string) => setReason(value as RegIPReason | "all"))}>
          <SelectTrigger size="sm" className="w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">全部原因</SelectItem>
            {(Object.keys(REASON_META) as Array<keyof typeof REASON_META>)
              .filter((item) => item !== "disabled" && item !== "list_allow")
              .map((item) => (
                <SelectItem key={item} value={item} className="text-xs">
                  {REASON_META[item].label}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
        <Select value={method} onValueChange={reset(setMethod)}>
          <SelectTrigger size="sm" className="w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">全部方式</SelectItem>
            {METHODS.map((item) => (
              <SelectItem key={item} value={item} className="text-xs">
                {METHOD_META[item].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup type="single" size="sm" variant="outline" value={range} onValueChange={(value) => value && reset(setRange)(value as RangeKey)}>
          {RANGES.map((item) => (
            <ToggleGroupItem key={item.key} value={item.key} className="h-8 px-2.5 text-xs">
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <Input value={ip} onChange={(event) => reset(setIP)(event.target.value)} placeholder="地址或网段" className="h-8 w-40 font-mono text-xs" />
        <Input value={device} onChange={(event) => reset(setDevice)(event.target.value)} placeholder="设备标识" className="h-8 w-44 font-mono text-xs" />
        <div className="relative min-w-44 flex-1 sm:max-w-60">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input value={keyword} onChange={(event) => reset(setKeyword)(event.target.value)} placeholder="账号、设备、属地、规则" className="h-8 pl-8 text-xs" />
        </div>
      </div>

      <div className="rounded-xl border border-border bg-card">
        {query.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2, 3, 4].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : query.error ? (
          <p className="px-4 py-10 text-center text-sm text-rose-600">{query.error.message}</p>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-14 text-center">
            <ListTree className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">暂无记录</span>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-32">时间</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>地址</TableHead>
                <TableHead>注册</TableHead>
                <TableHead>原因</TableHead>
                <TableHead className="text-right">计数</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((event) => (
                <TableRow key={event.id}>
                  <TableCell className="text-xs">
                    <div>{formatRelative(event.createdAt)}</div>
                    <div className="text-[10px] text-muted-foreground">{formatDateTime(event.createdAt)}</div>
                  </TableCell>
                  <TableCell>
                    <KindBadge kind={event.kind} />
                  </TableCell>
                  <TableCell className="max-w-56">
                    {event.ip || event.subnet ? (
                      <IPText value={event.ip || event.subnet} onClick={() => onOpenIP((event.ip || event.subnet).split("/")[0])} />
                    ) : (
                      <DeviceText value={event.deviceId} onClick={() => onOpenDevice(event.deviceId)} />
                    )}
                    <div className="truncate text-[10px] text-muted-foreground">
                      {locationText(event)}
                      {event.isp ? ` · ${event.isp}` : ""}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    {event.kind === "released" || event.kind === "penalized" ? (
                      <span className="text-muted-foreground">{event.operator || "系统"}</span>
                    ) : (
                      <>
                        <div className="truncate">{event.account || "—"}</div>
                        <div className="text-[10px] text-muted-foreground">{methodLabel(event.method)}</div>
                        {event.deviceId ? <DeviceText value={event.deviceId} className="text-[10px]" onClick={() => onOpenDevice(event.deviceId)} /> : null}
                      </>
                    )}
                  </TableCell>
                  <TableCell className="max-w-80 text-xs">
                    <div className="flex items-center gap-1.5">
                      {event.reason ? (
                        <Badge variant="outline" size="sm" className="font-normal">
                          {REASON_META[event.reason as keyof typeof REASON_META]?.label ?? event.reason}
                        </Badge>
                      ) : null}
                      {event.ruleName ? <span className="truncate">{event.ruleName}</span> : null}
                    </div>
                    {event.detail ? <div className="mt-0.5 line-clamp-2 text-[10px] text-muted-foreground">{event.detail}</div> : null}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">
                    {event.limit ? (
                      <span>
                        {event.count}
                        <span className="text-muted-foreground"> / {event.limit}</span>
                      </span>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>
      <Pager total={query.data?.total ?? 0} page={page} limit={LIMIT} onPage={setPage} />
    </div>
  );
}
