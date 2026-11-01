"use client";

import { useMemo, useState } from "react";
import { ScrollText, Search } from "lucide-react";
import { rangeStart, RANGES, type RangeKey } from "@/components/apps/register-ip/register-ip-events-panel";
import { DeviceText, IPText, locationText, methodLabel, METHODS, METHOD_META, Pager } from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useRegIPRegistrationsQuery } from "@/lib/register-ip-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const LIMIT = 20;

/**
 * 注册台账：每个新账号从哪个地址、用什么方式注册的。累计规则按它数；
 * 「已解除」的行不再计入上限，账号本身不受影响。删号后行仍保留（账号显示为已删除）。
 */
export function RegIPRegistrationsPanel({
  appKey,
  onOpenIP,
  onOpenDevice
}: {
  appKey: string;
  onOpenIP: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  const [method, setMethod] = useState("all");
  const [released, setReleased] = useState<"all" | "active" | "released">("all");
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
      method: method === "all" ? undefined : method,
      released: released === "all" ? undefined : released === "released",
      ip: debouncedIP || undefined,
      deviceId: debouncedDevice || undefined,
      keyword: debouncedKeyword || undefined,
      from: rangeStart(range),
      page,
      limit: LIMIT
    }),
    [method, released, debouncedIP, debouncedDevice, debouncedKeyword, range, page]
  );
  const query = useRegIPRegistrationsQuery(appKey, params);
  const items = query.data?.items ?? [];
  const reset = <T,>(set: (value: T) => void) => (value: T) => {
    set(value);
    setPage(1);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
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
        <Select value={released} onValueChange={reset((value: string) => setReleased(value as typeof released))}>
          <SelectTrigger size="sm" className="w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">全部</SelectItem>
            <SelectItem value="active" className="text-xs">计入上限</SelectItem>
            <SelectItem value="released" className="text-xs">已解除</SelectItem>
          </SelectContent>
        </Select>
        <ToggleGroup type="single" size="sm" variant="outline" value={range} onValueChange={(value) => value && reset(setRange)(value as RangeKey)}>
          {RANGES.map((item) => (
            <ToggleGroupItem key={item.key} value={item.key} className="h-8 px-2.5 text-xs">
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <Input value={ip} onChange={(event) => reset(setIP)(event.target.value)} placeholder="地址或网段，如 1.2.3.0/24" className="h-8 w-48 font-mono text-xs" />
        <Input value={device} onChange={(event) => reset(setDevice)(event.target.value)} placeholder="设备标识" className="h-8 w-44 font-mono text-xs" />
        <div className="relative min-w-44 flex-1 sm:max-w-60">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input value={keyword} onChange={(event) => reset(setKeyword)(event.target.value)} placeholder="账号、设备、属地、运营商" className="h-8 pl-8 text-xs" />
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
            <ScrollText className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">暂无注册</span>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-32">时间</TableHead>
                <TableHead>账号</TableHead>
                <TableHead>方式</TableHead>
                <TableHead>地址 / 设备</TableHead>
                <TableHead>属地</TableHead>
                <TableHead>状态</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id} className={cn(item.releasedAt && "opacity-70")}>
                  <TableCell className="text-xs">
                    <div>{formatRelative(item.createdAt)}</div>
                    <div className="text-[10px] text-muted-foreground">{formatDateTime(item.createdAt)}</div>
                  </TableCell>
                  <TableCell className="text-xs">
                    <div className="truncate font-medium">{item.account || (item.userId ? `UID-${item.userId}` : "—")}</div>
                    {!item.userId ? <div className="text-[10px] text-muted-foreground">账号已删除</div> : null}
                  </TableCell>
                  <TableCell className="text-xs">{methodLabel(item.method)}</TableCell>
                  <TableCell>
                    <IPText value={item.ip} onClick={() => onOpenIP(item.ip)} />
                    {item.deviceId ? (
                      <div>
                        <DeviceText value={item.deviceId} className="text-[10px]" onClick={() => onOpenDevice(item.deviceId)} />
                      </div>
                    ) : (
                      <div className="text-[10px] text-muted-foreground">未上报设备</div>
                    )}
                  </TableCell>
                  <TableCell className="max-w-56 text-xs">
                    <div className="truncate">{locationText(item)}</div>
                    {item.isp ? <div className="truncate text-[10px] text-muted-foreground">{item.isp}</div> : null}
                  </TableCell>
                  <TableCell>
                    {item.releasedAt ? (
                      <Badge variant="outline" size="sm" title={formatDateTime(item.releasedAt)}>
                        已解除
                      </Badge>
                    ) : (
                      <Badge variant="secondary" size="sm">计入上限</Badge>
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
