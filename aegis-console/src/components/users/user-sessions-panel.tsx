"use client";

import { useState } from "react";
import { Globe2, Loader2, Monitor, Server, Smartphone, Trash2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { SessionDetailView } from "@/lib/api/apps";
import { classifyIp, isServerSideAddress } from "@/lib/geo/private-network";
import {
  useAdminUserSessionsQuery,
  useRevokeAdminUserSessionMutation,
  useRevokeAdminUserSessionsBatchMutation,
  useRevokeAdminAppUserSessionsMutation
} from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

function formatTime(value?: string | null) {
  if (!value) return "—";
  const d = new Date(value);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function parseUA(ua?: string) {
  if (!ua) return { device: "未知设备", browser: "" };
  const isMobile = /mobile|android|iphone|ipad/i.test(ua);
  const browserMatch = ua.match(/(Chrome|Firefox|Safari|Edge|Opera|Brave|Arc)\/[\d.]+/i);
  const browser = browserMatch ? browserMatch[0] : "";
  const osMatch = ua.match(/(Windows|Mac OS X|Linux|Android|iOS)\s?[\d._]*/i);
  const os = osMatch ? osMatch[0].replace(/_/g, ".") : "";
  return { device: isMobile ? "移动设备" : "桌面设备", browser, os };
}

/**
 * 位置文案。
 *
 * 内网 / 回环来源不说「未知位置」——它不是查不到，而是这条会话本来就来自
 * 服务端自己那一侧（本机调试、反代未透传真实 IP、内网客户端）。
 * 把这两件事混成一句「未知」，排查的人会一直去追一个不存在的地理位置。
 */
function locationText(s: SessionDetailView): { text: string; server: boolean; hint?: string } {
  if (isServerSideAddress(s.ip, s.isPrivate)) {
    const cls = classifyIp(s.ip);
    return { text: cls.label, server: true, hint: cls.detail };
  }
  const parts = [s.city, s.region, s.country].filter(Boolean);
  if (parts.length) return { text: parts.join(", "), server: false };
  return { text: s.location || "未知位置", server: false };
}

export function UserSessionsPanel({ appKey, userId }: { appKey?: string | null; userId?: number | null }) {
  const sessionsQuery = useAdminUserSessionsQuery(appKey, userId);
  const revokeMutation = useRevokeAdminUserSessionMutation(appKey, userId);
  const batchRevokeMutation = useRevokeAdminUserSessionsBatchMutation(appKey, userId);
  const revokeAllMutation = useRevokeAdminAppUserSessionsMutation(appKey, userId);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const sessions = sessionsQuery.data?.items || [];
  const loading = sessionsQuery.isLoading;

  function toggleSelect(hash: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(hash)) next.delete(hash);
      else next.add(hash);
      return next;
    });
  }

  function toggleAll() {
    if (selected.size === sessions.length) setSelected(new Set());
    else setSelected(new Set(sessions.map((s) => s.tokenHash)));
  }

  async function handleRevokeSingle(hash: string) {
    try {
      await revokeMutation.mutateAsync(hash);
      setSelected((prev) => { const n = new Set(prev); n.delete(hash); return n; });
      toast.success("会话已撤销");
    } catch (err) { toast.error(err instanceof ApiError ? err.message : "撤销失败"); }
  }

  async function handleRevokeBatch() {
    if (!selected.size) return;
    try {
      const result = await batchRevokeMutation.mutateAsync(Array.from(selected));
      setSelected(new Set());
      toast.success(`已撤销 ${result.revoked} 个会话`);
    } catch (err) { toast.error(err instanceof ApiError ? err.message : "批量撤销失败"); }
  }

  async function handleRevokeAll() {
    try {
      await revokeAllMutation.mutateAsync();
      setSelected(new Set());
      toast.success("已踢出所有会话");
      void sessionsQuery.refetch();
    } catch (err) { toast.error(err instanceof ApiError ? err.message : "踢出失败"); }
  }

  if (loading) {
    return <div className="space-y-2">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}</div>;
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <span className="text-xs tabular-nums text-muted-foreground">{sessions.length} 个活跃会话</span>
        <div className="flex gap-1.5">
          {selected.size > 0 && (
            <Button size="sm" variant="outline" className="h-7 gap-1 text-xs text-destructive hover:text-destructive" disabled={batchRevokeMutation.isPending} onClick={handleRevokeBatch}>
              {batchRevokeMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <XCircle className="size-3" />}
              撤销选中 ({selected.size})
            </Button>
          )}
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs text-destructive hover:text-destructive" disabled={revokeAllMutation.isPending || !sessions.length} onClick={handleRevokeAll}>
            {revokeAllMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
            全部踢出
          </Button>
        </div>
      </div>

      {sessions.length === 0 ? (
        <div className="py-8 text-center text-sm text-muted-foreground">无活跃会话</div>
      ) : (<>
        <TooltipProvider delayDuration={120}>
          <div className="overflow-hidden rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-8">
                    <Checkbox checked={selected.size === sessions.length && sessions.length > 0} onCheckedChange={toggleAll} />
                  </TableHead>
                  <TableHead>位置</TableHead>
                  <TableHead>设备</TableHead>
                  <TableHead>IP</TableHead>
                  <TableHead>登录时间</TableHead>
                  <TableHead className="w-16" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.map((s) => {
                  const ua = parseUA(s.userAgent);
                  const DeviceIcon = /mobile|android|iphone|ipad/i.test(s.userAgent || "") ? Smartphone : Monitor;
                  const loc = locationText(s);
                  return (
                    <TableRow key={s.tokenHash}>
                      <TableCell>
                        <Checkbox checked={selected.has(s.tokenHash)} onCheckedChange={() => toggleSelect(s.tokenHash)} />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1.5" title={loc.hint}>
                          {loc.server ? (
                            <Server className="size-3 shrink-0 text-violet-500" />
                          ) : (
                            <Globe2 className="size-3 shrink-0 text-muted-foreground" />
                          )}
                          <span className="text-xs">{loc.text}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="flex items-center gap-1.5">
                              <DeviceIcon className="size-3 shrink-0 text-muted-foreground" />
                              <span className="max-w-[120px] truncate text-xs">{ua.browser || ua.os || ua.device}</span>
                            </div>
                          </TooltipTrigger>
                          <TooltipContent side="top" className="max-w-xs text-xs">
                            <div>{ua.os} · {ua.browser}</div>
                            {s.provider && <Badge variant="outline" className="mt-1 text-[9px]">{s.provider}</Badge>}
                          </TooltipContent>
                        </Tooltip>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">{s.ip || "—"}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{formatTime(s.issuedAt)}</TableCell>
                      <TableCell>
                        <Button size="sm" variant="ghost" className="h-6 gap-1 px-1.5 text-[10px] text-destructive hover:text-destructive" disabled={revokeMutation.isPending} onClick={() => void handleRevokeSingle(s.tokenHash)}>
                          <XCircle className="size-3" />
                          撤销
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </TooltipProvider>
      </>)}
    </div>
  );
}
