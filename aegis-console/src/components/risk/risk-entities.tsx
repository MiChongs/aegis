"use client";

import { useState } from "react";
import { Fingerprint, Globe2, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  useRefreshIPReputationMutation, useRiskDeviceQuery, useRiskDevicesQuery,
  useRiskIPQuery, useRiskIPsQuery, useUpdateDeviceTagMutation, useUpdateIPTagMutation,
} from "@/lib/risk-hooks";
import type { RiskAssessment, RiskEntitySummary } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { Pagination } from "./risk-assessments";
import {
  ActionBadge, DetailRow, LevelBadge, SceneBadge, TagBadge, ellipsisMiddle,
  fmtDateTime, fmtNumber, fmtRelative, useRiskCatalog,
} from "./risk-shared";

/**
 * 设备指纹与 IP 风险库。
 *
 * 两处结构性修复：
 *   1. **设备档案此前从来没有被写过。** 表只被「新设备」规则读、从不写，
 *      于是 device_age_hours 恒为 0（该规则对每个请求都命中），
 *      而这个页签永远是空的。写入点现在挂在每次评估上。
 *   2. **只能看「可疑」的。** 排查时最常见的需求是「这个设备号到底有没有在档」，
 *      而按标签过滤的列表回答不了。现在默认全量，可切到只看有风险的。
 */

const PAGE_SIZE = 25;

export function RiskDevicesPanel({ onQueryAssessments }: { onQueryAssessments?: (deviceId: string) => void }) {
  const { deviceTags } = useRiskCatalog();
  const [keyword, setKeyword] = useState("");
  const [tag, setTag] = useState("");
  const [onlyRisk, setOnlyRisk] = useState(false);
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<string | null>(null);

  const query = useRiskDevicesQuery({
    keyword: keyword.trim() || undefined,
    tag: tag || undefined,
    onlyRisk: onlyRisk ? "true" : undefined,
    page, pageSize: PAGE_SIZE,
  });
  const tagMut = useUpdateDeviceTagMutation();

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const changeTag = async (id: number, next: string) => {
    try {
      await tagMut.mutateAsync({ id, tag: next });
      toast.success("标记已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "更新失败");
    }
  };

  return (
    <div className="space-y-3">
      <Card><CardContent className="flex flex-wrap items-end gap-3 p-3">
        <div className="space-y-1">
          <Label className="text-[10px] text-muted-foreground">搜索</Label>
          <Input value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
            placeholder="设备号、IP 或 UA" className="h-8 w-56 text-xs" />
        </div>
        <div className="space-y-1">
          <Label className="text-[10px] text-muted-foreground">标记</Label>
          <Select value={tag || "all"} onValueChange={(v) => { setTag(v === "all" ? "" : v); setPage(1); }}>
            <SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部</SelectItem>
              {deviceTags.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-center gap-2 pb-1">
          <Switch checked={onlyRisk} onCheckedChange={(v) => { setOnlyRisk(v); setPage(1); }} />
          <Label className="text-xs">只看有风险标记</Label>
        </div>
        <span className="ml-auto text-xs text-muted-foreground">共 {fmtNumber(total)} 台设备在档</span>
      </CardContent></Card>

      {query.isLoading && items.length === 0 ? (
        <div className="space-y-1.5">{Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="h-11 animate-pulse rounded-lg bg-muted/50" />
        ))}</div>
      ) : items.length === 0 ? (
        <EmptyState title="没有匹配的设备"
          description="设备档案在每次风险评估时自动写入，客户端未上报设备标识则不会有记录" />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>设备标识</TableHead>
                  <TableHead className="w-32">最近 IP</TableHead>
                  <TableHead className="w-16 text-right">出现</TableHead>
                  <TableHead className="w-32">首次与最近</TableHead>
                  <TableHead className="w-28">标记</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((device) => (
                  <TableRow key={device.id} className="text-xs">
                    <TableCell className="max-w-0">
                      <button type="button" onClick={() => setDetailId(device.deviceId)}
                        className="block w-full truncate text-left font-mono hover:underline" title={device.deviceId}>
                        {ellipsisMiddle(device.deviceId, 20, 10)}
                      </button>
                      {device.userAgent && (
                        <div className="truncate text-[10px] text-muted-foreground" title={device.userAgent}>
                          {device.userAgent}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="font-mono">{device.lastIp || "--"}</TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{fmtNumber(device.seenCount)}</TableCell>
                    <TableCell className="whitespace-nowrap text-[10px] text-muted-foreground">
                      {fmtRelative(device.firstSeenAt)}
                      <br />
                      {fmtRelative(device.lastSeenAt)}
                    </TableCell>
                    <TableCell>
                      <Select value={device.riskTag} onValueChange={(v) => changeTag(device.id, v)}>
                        <SelectTrigger className="h-7 w-24 text-[10px]"><SelectValue /></SelectTrigger>
                        <SelectContent>
                          {deviceTags.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell>
                      <Button variant="ghost" size="sm" className="h-7 px-2 text-[10px]"
                        onClick={() => onQueryAssessments?.(device.deviceId)}>
                        <Search className="size-3" />评估记录
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {detailId && <DeviceDetailSheet deviceId={detailId} onClose={() => setDetailId(null)} />}
    </div>
  );
}

export function RiskIPsPanel({ onQueryAssessments }: { onQueryAssessments?: (ip: string) => void }) {
  const { ipTags } = useRiskCatalog();
  const [keyword, setKeyword] = useState("");
  const [tag, setTag] = useState("");
  const [onlyRisk, setOnlyRisk] = useState(false);
  const [page, setPage] = useState(1);
  const [detailIP, setDetailIP] = useState<string | null>(null);

  const query = useRiskIPsQuery({
    keyword: keyword.trim() || undefined,
    tag: tag || undefined,
    onlyRisk: onlyRisk ? "true" : undefined,
    page, pageSize: PAGE_SIZE,
  });
  const tagMut = useUpdateIPTagMutation();

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const changeTag = async (id: number, next: string) => {
    try {
      await tagMut.mutateAsync({ id, tag: next });
      toast.success("标记已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "更新失败");
    }
  };

  return (
    <div className="space-y-3">
      <Card><CardContent className="flex flex-wrap items-end gap-3 p-3">
        <div className="space-y-1">
          <Label className="text-[10px] text-muted-foreground">搜索</Label>
          <Input value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
            placeholder="IP、国家、运营商或 ASN" className="h-8 w-56 text-xs" />
        </div>
        <div className="space-y-1">
          <Label className="text-[10px] text-muted-foreground">标记</Label>
          <Select value={tag || "all"} onValueChange={(v) => { setTag(v === "all" ? "" : v); setPage(1); }}>
            <SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部</SelectItem>
              {ipTags.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-center gap-2 pb-1">
          <Switch checked={onlyRisk} onCheckedChange={(v) => { setOnlyRisk(v); setPage(1); }} />
          <Label className="text-xs">只看有风险标记</Label>
        </div>
        <span className="ml-auto text-xs text-muted-foreground">共 {fmtNumber(total)} 个 IP 在档</span>
      </CardContent></Card>

      {query.isLoading && items.length === 0 ? (
        <div className="space-y-1.5">{Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="h-11 animate-pulse rounded-lg bg-muted/50" />
        ))}</div>
      ) : items.length === 0 ? (
        <EmptyState title="没有匹配的 IP"
          description="IP 档案在风险评估时自动写入，未配置情报源时只有本地归属地与计数" />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-36">IP</TableHead>
                  <TableHead className="w-16 text-right">信誉分</TableHead>
                  <TableHead className="w-40">归属</TableHead>
                  <TableHead>特征</TableHead>
                  <TableHead className="w-28 text-right">请求与拦截</TableHead>
                  <TableHead className="w-28">标记</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((record) => (
                  <TableRow key={record.id} className="text-xs">
                    <TableCell>
                      <button type="button" onClick={() => setDetailIP(record.ip)}
                        className="font-mono hover:underline">{record.ip}</button>
                      {record.source && (
                        <div className="text-[10px] text-muted-foreground">来源 {record.source}</div>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <span className={cn("font-mono font-semibold tabular-nums",
                        record.riskScore >= 75 ? "text-rose-600 dark:text-rose-400"
                          : record.riskScore >= 40 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
                        {record.riskScore}
                      </span>
                    </TableCell>
                    <TableCell className="max-w-0">
                      <div className="truncate">{[record.country, record.region].filter(Boolean).join(" ") || "--"}</div>
                      <div className="truncate text-[10px] text-muted-foreground">{record.isp || record.asn || "--"}</div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {record.isProxy && <Badge variant="outline" size="sm">代理</Badge>}
                        {record.isVpn && <Badge variant="outline" size="sm">VPN</Badge>}
                        {record.isTor && <Badge variant="danger" size="sm">Tor</Badge>}
                        {record.isDatacenter && <Badge variant="outline" size="sm">机房</Badge>}
                        {!record.isProxy && !record.isVpn && !record.isTor && !record.isDatacenter && (
                          <span className="text-[10px] text-muted-foreground">—</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {fmtNumber(record.totalRequests)}
                      <span className="text-muted-foreground"> / </span>
                      <span className={record.totalBlocks > 0 ? "text-rose-600 dark:text-rose-400" : ""}>
                        {fmtNumber(record.totalBlocks)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Select value={record.riskTag} onValueChange={(v) => changeTag(record.id, v)}>
                        <SelectTrigger className="h-7 w-24 text-[10px]"><SelectValue /></SelectTrigger>
                        <SelectContent>
                          {ipTags.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell>
                      <Button variant="ghost" size="sm" className="h-7 px-2 text-[10px]"
                        onClick={() => onQueryAssessments?.(record.ip)}>
                        <Search className="size-3" />评估记录
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {detailIP && <IPDetailSheet ip={detailIP} onClose={() => setDetailIP(null)} />}
    </div>
  );
}

/* ══════════════════════════════════════════════════════════
   详情抽屉
   ══════════════════════════════════════════════════════════ */

function DeviceDetailSheet({ deviceId, onClose }: { deviceId: string; onClose: () => void }) {
  const query = useRiskDeviceQuery(deviceId);
  const detail = query.data;

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-2"><Fingerprint className="size-4" />设备详情</SheetTitle>
          <SheetDescription className="break-all font-mono text-[11px]">{deviceId}</SheetDescription>
        </SheetHeader>
        <ScrollArea className="min-h-0 flex-1">
          {query.isLoading || !detail ? (
            <div className="space-y-3 p-5">
              {Array.from({ length: 5 }, (_, i) => <div key={i} className="h-14 animate-pulse rounded-lg bg-muted/50" />)}
            </div>
          ) : (
            <div className="space-y-5 px-5 py-4">
              <EntityStats summary={detail.summary} />

              <section className="space-y-2">
                <SectionTitle>档案</SectionTitle>
                <Card><CardContent className="p-3">
                  <DetailRow label="风险标记"><TagBadge tag={detail.device.riskTag} kind="device" /></DetailRow>
                  <DetailRow label="出现次数" mono>{fmtNumber(detail.device.seenCount)}</DetailRow>
                  <DetailRow label="最近 IP" mono>{detail.device.lastIp || "--"}</DetailRow>
                  <DetailRow label="User-Agent" mono>{detail.device.userAgent || "--"}</DetailRow>
                  <DetailRow label="首次出现">{fmtDateTime(detail.device.firstSeenAt)}</DetailRow>
                  <DetailRow label="最近出现">{fmtDateTime(detail.device.lastSeenAt)}</DetailRow>
                  {detail.device.note && <DetailRow label="备注">{detail.device.note}</DetailRow>}
                </CardContent></Card>
              </section>

              {Object.keys(detail.device.fingerprint ?? {}).length > 0 && (
                <section className="space-y-2">
                  <SectionTitle>客户端特征</SectionTitle>
                  <pre className="overflow-x-auto rounded-lg border bg-muted/30 p-2.5 font-mono text-[10px]">
                    {JSON.stringify(detail.device.fingerprint, null, 2)}
                  </pre>
                </section>
              )}

              <ChipList title="使用过的 IP" items={detail.ips} />
              <ChipList title="关联过的账号" items={detail.accounts} />
              <RecentAssessments items={detail.recent} />
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function IPDetailSheet({ ip, onClose }: { ip: string; onClose: () => void }) {
  const query = useRiskIPQuery(ip);
  const refreshMut = useRefreshIPReputationMutation();
  const tagMut = useUpdateIPTagMutation();
  const { ipTags } = useRiskCatalog();
  const [note, setNote] = useState("");
  const detail = query.data;

  const refresh = async () => {
    try {
      await refreshMut.mutateAsync(ip);
      toast.success("已向情报源重新拉取");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "刷新失败");
    }
  };

  const applyTag = async (next: string) => {
    if (!detail?.record.id) { toast.error("该 IP 尚无档案，无法标记"); return; }
    try {
      await tagMut.mutateAsync({ id: detail.record.id, tag: next, note: note.trim() || undefined });
      toast.success("标记已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "更新失败");
    }
  };

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-2"><Globe2 className="size-4" />IP 详情</SheetTitle>
          <SheetDescription className="font-mono text-[11px]">{ip}</SheetDescription>
        </SheetHeader>
        <ScrollArea className="min-h-0 flex-1">
          {query.isLoading || !detail ? (
            <div className="space-y-3 p-5">
              {Array.from({ length: 5 }, (_, i) => <div key={i} className="h-14 animate-pulse rounded-lg bg-muted/50" />)}
            </div>
          ) : (
            <div className="space-y-5 px-5 py-4">
              <EntityStats summary={detail.summary} />

              <section className="space-y-2">
                <div className="flex items-center justify-between">
                  <SectionTitle>情报档案</SectionTitle>
                  <Button variant="outline" size="sm" className="h-7 text-xs" disabled={refreshMut.isPending}
                    onClick={refresh}>
                    <RefreshCw className={cn("size-3.5", refreshMut.isPending && "animate-spin")} />重新拉取
                  </Button>
                </div>
                <Card><CardContent className="p-3">
                  <DetailRow label="风险标记"><TagBadge tag={detail.record.riskTag} kind="ip" /></DetailRow>
                  <DetailRow label="信誉分" mono>{detail.record.riskScore} / 100</DetailRow>
                  <DetailRow label="归属">
                    {[detail.record.country, detail.record.region].filter(Boolean).join(" ") || "未知"}
                  </DetailRow>
                  <DetailRow label="运营商">{detail.record.isp || "--"}</DetailRow>
                  <DetailRow label="ASN" mono>{detail.record.asn || "--"}</DetailRow>
                  <DetailRow label="出口特征">
                    <div className="flex flex-wrap gap-1">
                      {detail.record.isProxy && <Badge variant="outline" size="sm">代理</Badge>}
                      {detail.record.isVpn && <Badge variant="outline" size="sm">VPN</Badge>}
                      {detail.record.isTor && <Badge variant="danger" size="sm">Tor</Badge>}
                      {detail.record.isDatacenter && <Badge variant="outline" size="sm">机房</Badge>}
                      {!detail.record.isProxy && !detail.record.isVpn && !detail.record.isTor && !detail.record.isDatacenter
                        && <span className="text-muted-foreground">无</span>}
                    </div>
                  </DetailRow>
                  <DetailRow label="累计请求" mono>
                    {fmtNumber(detail.record.totalRequests)}（其中拦截 {fmtNumber(detail.record.totalBlocks)}）
                  </DetailRow>
                  <DetailRow label="情报来源">
                    {detail.record.source
                      ? <Badge variant={detail.record.source === "manual" ? "info" : "outline"} size="sm">
                          {detail.record.source === "manual" ? "人工标注" : detail.record.source}
                        </Badge>
                      : "--"}
                  </DetailRow>
                  {detail.record.note && <DetailRow label="备注">{detail.record.note}</DetailRow>}
                  <DetailRow label="首次与最近">
                    {fmtDateTime(detail.record.firstSeenAt)} / {fmtDateTime(detail.record.lastSeenAt)}
                  </DetailRow>
                </CardContent></Card>
              </section>

              <section className="space-y-2">
                <SectionTitle>人工处置</SectionTitle>
                <p className="text-[10px] text-muted-foreground">
                  人工标注的来源记为 manual，不会被后续的情报刷新覆盖。
                </p>
                <Textarea rows={2} className="text-xs" value={note} placeholder="备注，可留空"
                  onChange={(e) => setNote(e.target.value)} />
                <div className="flex flex-wrap gap-1.5">
                  {ipTags.map((t) => (
                    <Button key={t.value} variant={detail.record.riskTag === t.value ? "default" : "outline"}
                      size="sm" className="h-7 text-xs" disabled={tagMut.isPending}
                      onClick={() => applyTag(t.value)}>
                      {t.label}
                    </Button>
                  ))}
                </div>
              </section>

              <ChipList title="关联设备" items={(detail.devices ?? []).map((d) => ellipsisMiddle(d.deviceId, 14, 8))} />
              <ChipList title="关联过的账号" items={detail.accounts} />
              <RecentAssessments items={detail.recent} />
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">{children}</h4>;
}

function EntityStats({ summary }: { summary: RiskEntitySummary }) {
  return (
    <section className="grid grid-cols-4 gap-2">
      <Tile label="历史评估" value={fmtNumber(summary.totalAssessments)} />
      <Tile label="其中拦截" value={fmtNumber(summary.blocked)} tone="danger" />
      <Tile label="平均分" value={summary.avgScore.toFixed(1)} />
      <Tile label="峰值分" value={String(summary.maxScore)} />
    </section>
  );
}

function Tile({ label, value, tone }: { label: string; value: string; tone?: "danger" }) {
  return (
    <div className="rounded-lg border p-2.5 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className={cn("font-mono text-lg font-bold tabular-nums", tone === "danger" && "text-rose-600 dark:text-rose-400")}>
        {value}
      </div>
    </div>
  );
}

function ChipList({ title, items }: { title: string; items: string[] | null | undefined }) {
  const list = items ?? [];
  if (list.length === 0) return null;
  return (
    <section className="space-y-1.5">
      <SectionTitle>{title}（{list.length}）</SectionTitle>
      <div className="flex flex-wrap gap-1">
        {list.map((item) => (
          <code key={item} className="rounded border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px]">{item}</code>
        ))}
      </div>
    </section>
  );
}

function RecentAssessments({ items }: { items: RiskAssessment[] | null | undefined }) {
  const list = items ?? [];
  if (list.length === 0) return null;
  return (
    <section className="space-y-2">
      <SectionTitle>最近评估</SectionTitle>
      <Separator />
      <div className="space-y-1">
        {list.map((item) => (
          <div key={item.id} className="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[11px]">
            <SceneBadge scene={item.scene} />
            <LevelBadge level={item.riskLevel} />
            <ActionBadge action={item.action} />
            <span className="truncate font-mono text-muted-foreground">{item.account || "--"}</span>
            <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
              {item.totalScore} 分，{fmtRelative(item.createdAt)}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
