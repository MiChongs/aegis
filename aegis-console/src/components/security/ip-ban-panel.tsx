"use client";

import { useCallback, useMemo, useState } from "react";
import {
  Ban,
  ChevronLeft,
  ChevronRight,
  Clock,
  Globe,
  Loader2,
  Plus,
  RefreshCw,
  ShieldOff,
  Unlock
} from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/ui/data-state";
import { SurfaceCard } from "@/components/ui/surface-card";
import {
  Tooltip as UITooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip";

import { useIPBansQuery, useCreateIPBanMutation, useDeleteIPBanMutation, useIPBanModesQuery } from "@/lib/admin-hooks";
import type { IPBanEntry, IPBanMode } from "@/lib/api/types";
import type { IPBanListParams } from "@/lib/api/system";

// ── 常量 ──────────────────────────

const statusConfig: Record<string, { label: string; variant: "success" | "secondary" | "outline" | "danger" }> = {
  active: { label: "生效中", variant: "danger" },
  expired: { label: "已过期", variant: "secondary" },
  revoked: { label: "已解封", variant: "outline" }
};

const sourceLabels: Record<string, string> = {
  manual: "手动",
  auto: "自动"
};

const banModeLabels: Record<string, string> = {
  forbidden: "HTTP 403",
  silent_drop: "不响应",
  connection_reset: "连接重置",
  tarpit: "拖延",
  stealth_404: "伪装 404",
  stealth_503: "伪装 503",
  teapot: "418 彩蛋",
  gone: "410 永久移除",
  rate_choke: "严格限速",
  redirect: "302 重定向",
  honeypot: "蜜罐",
  fake_empty: "200 空响应",
  random_error: "随机 5xx",
  slow_response: "逐字节慢响应",
  bandwidth_choke: "带宽抑制",
  random_delay: "随机延迟",
  zip_bomb: "压缩炸弹",
  chunked_infinite: "无限流",
  infinite_redirect: "无限跳转",
  mirror_request: "请求回显",
  fake_login: "伪装登录",
  random_garbage: "随机垃圾",
  cursed_headers: "诅咒头部",
  json_bomb: "JSON 炸弹",
  cookie_bomb: "Cookie 膨胀",
  reverse_slowloris: "反向 Slowloris"
};

const durationOptions = [
  { label: "1 小时", value: 3600 },
  { label: "6 小时", value: 21600 },
  { label: "24 小时", value: 86400 },
  { label: "7 天", value: 604800 },
  { label: "30 天", value: 2592000 },
  { label: "永久", value: 0 }
];

function formatTime(iso: string) {
  try {
    return new Date(iso).toLocaleString("zh-CN", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit"
    });
  } catch {
    return iso;
  }
}

function formatDuration(seconds: number) {
  if (seconds === 0) return "永久";
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)} 小时`;
  return `${Math.round(seconds / 86400)} 天`;
}

function formatRemaining(expiresAt?: string | null) {
  if (!expiresAt) return "永久";
  const remaining = new Date(expiresAt).getTime() - Date.now();
  if (remaining <= 0) return "已过期";
  const hours = Math.floor(remaining / 3600_000);
  if (hours < 1) return `${Math.ceil(remaining / 60_000)} 分钟`;
  if (hours < 24) return `${hours} 小时`;
  return `${Math.floor(hours / 24)} 天 ${hours % 24} 小时`;
}

function geoLabel(ban: Pick<IPBanEntry, "country" | "city" | "region">) {
  return [ban.country, ban.region, ban.city].filter(Boolean).join(" · ") || null;
}

// ── 主组件 ──────────────────────────

export function IPBanPanel() {
  const [filterStatus, setFilterStatus] = useState("active");
  const [filterSource, setFilterSource] = useState("all");
  const [filterIP, setFilterIP] = useState("");
  const [page, setPage] = useState(1);
  const [showBanDialog, setShowBanDialog] = useState(false);
  const [showUnbanDialog, setShowUnbanDialog] = useState<IPBanEntry | null>(null);

  // 封禁表单状态
  const [banIP, setBanIP] = useState("");
  const [banReason, setBanReason] = useState("");
  const [banDuration, setBanDuration] = useState(86400);
  const [banMode, setBanMode] = useState<IPBanMode | "__default__">("__default__");
  const [banTarpitMs, setBanTarpitMs] = useState<number>(0);

  const modesQuery = useIPBanModesQuery();
  const modes = modesQuery.data?.modes ?? [];
  const defaultMode = modesQuery.data?.default ?? "forbidden";

  const queryParams = useMemo<IPBanListParams>(() => {
    const p: IPBanListParams = { page, pageSize: 20, status: filterStatus };
    if (filterSource !== "all") p.source = filterSource;
    if (filterIP.trim()) p.ip = filterIP.trim();
    return p;
  }, [page, filterStatus, filterSource, filterIP]);

  const bansQuery = useIPBansQuery(queryParams);
  const createMutation = useCreateIPBanMutation();
  const deleteMutation = useDeleteIPBanMutation();

  const banData = bansQuery.data;

  // 统计
  const activeBans = useMemo(() => {
    if (filterStatus !== "active") return null;
    return banData?.total ?? null;
  }, [banData, filterStatus]);

  const handleBan = useCallback(async () => {
    if (!banIP.trim()) {
      toast.error("请输入 IP 地址");
      return;
    }
    try {
      const payload: Parameters<typeof createMutation.mutateAsync>[0] = {
        ip: banIP.trim(),
        reason: banReason.trim() || "手动封禁",
        duration: banDuration
      };
      if (banMode !== "__default__" && banMode) {
        payload.mode = banMode;
      }
      if (banMode === "tarpit" && banTarpitMs > 0) {
        payload.delayMs = banTarpitMs;
      }
      await createMutation.mutateAsync(payload);
      toast.success(`已封禁 ${banIP.trim()}`);
      setShowBanDialog(false);
      setBanIP("");
      setBanReason("");
      setBanDuration(86400);
      setBanMode("__default__");
      setBanTarpitMs(0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "封禁失败");
    }
  }, [banIP, banReason, banDuration, banMode, banTarpitMs, createMutation]);

  const handleUnban = useCallback(async () => {
    if (!showUnbanDialog) return;
    try {
      await deleteMutation.mutateAsync(showUnbanDialog.id);
      toast.success(`已解封 ${showUnbanDialog.ip}`);
      setShowUnbanDialog(null);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "解封失败");
    }
  }, [showUnbanDialog, deleteMutation]);

  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-5">
        {/* 顶部操作栏 */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-destructive/10 text-destructive">
              <Ban className="size-4" />
            </div>
            <div className="flex flex-col">
              <span className="text-sm font-semibold leading-tight">IP 封禁管理</span>
              <span className="text-[11px] leading-tight text-muted-foreground">
                {activeBans !== null && activeBans > 0 ? (
                  <>当前有 <span className="font-medium text-destructive">{activeBans}</span> 个 IP 处于封禁状态</>
                ) : activeBans !== null ? (
                  <>当前没有 IP 处于封禁状态</>
                ) : (
                  <>&nbsp;</>
                )}
                <span className="ml-2 border-l border-border pl-2 text-muted-foreground/80">
                  默认响应模式：<span className="font-medium text-foreground">{banModeLabels[defaultMode] ?? defaultMode}</span>
                </span>
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => bansQuery.refetch()} disabled={bansQuery.isFetching}>
              <RefreshCw className={`size-3.5 ${bansQuery.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
            <Button size="sm" className="h-8 gap-1.5" onClick={() => setShowBanDialog(true)}>
              <Plus className="size-3.5" />
              封禁 IP
            </Button>
          </div>
        </div>

        {/* 过滤器 */}
        <div className="flex flex-wrap items-end gap-3 rounded-xl border border-border bg-muted/40 px-4 py-3">
          <FilterField label="状态">
            <Select value={filterStatus} onValueChange={(v) => { setFilterStatus(v); setPage(1); }}>
              <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="active">生效中</SelectItem>
                <SelectItem value="expired">已过期</SelectItem>
                <SelectItem value="revoked">已解封</SelectItem>
              </SelectContent>
            </Select>
          </FilterField>
          <FilterField label="来源">
            <Select value={filterSource} onValueChange={(v) => { setFilterSource(v); setPage(1); }}>
              <SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="manual">手动</SelectItem>
                <SelectItem value="auto">自动</SelectItem>
              </SelectContent>
            </Select>
          </FilterField>
          <FilterField label="IP 地址">
            <Input
              placeholder="精确匹配"
              className="h-8 w-40 font-mono text-xs"
              value={filterIP}
              onChange={(e) => setFilterIP(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && setPage(1)}
            />
          </FilterField>
        </div>

        {/* 封禁列表 */}
        {bansQuery.isLoading ? (
          <SurfaceCard>
            <div className="space-y-3">
              {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}
            </div>
          </SurfaceCard>
        ) : !banData?.items?.length ? (
          <EmptyState title="暂无封禁记录" description="当前过滤条件下没有 IP 封禁记录" />
        ) : (
          <Card className="overflow-hidden">
            <div className="overflow-x-auto">
              <Table className="table-fixed">
                <colgroup>
                  <col className="w-[160px]" />
                  <col className="w-[180px]" />
                  <col className="w-[88px]" />
                  <col className="w-[96px]" />
                  <col className="w-[112px]" />
                  <col />
                  <col className="w-[88px]" />
                  <col className="w-[120px]" />
                  <col className="w-[160px]" />
                  <col className="w-[72px]" />
                </colgroup>
                <TableHeader>
                  <TableRow className="bg-muted/40 hover:bg-muted/40">
                    <TableHead className="pl-4">IP</TableHead>
                    <TableHead>位置</TableHead>
                    <TableHead>来源</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>响应模式</TableHead>
                    <TableHead>原因</TableHead>
                    <TableHead className="text-right">时长</TableHead>
                    <TableHead className="text-right">剩余</TableHead>
                    <TableHead>封禁时间</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {banData.items.map((ban) => (
                    <TableRow key={ban.id} className="group">
                      <TableCell className="pl-4 align-middle">
                        <span className="block truncate font-mono text-[13px] font-medium">{ban.ip}</span>
                      </TableCell>

                      <TableCell className="align-middle text-xs text-muted-foreground">
                        {geoLabel(ban) ? (
                          <UITooltip>
                            <TooltipTrigger asChild>
                              <div className="flex min-w-0 items-center gap-1.5">
                                {ban.countryCode && (
                                  <span className="shrink-0 rounded-sm bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                                    {ban.countryCode}
                                  </span>
                                )}
                                <span className="min-w-0 flex-1 truncate">{geoLabel(ban)}</span>
                              </div>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="text-xs">
                              <div>{geoLabel(ban)}</div>
                              {ban.isp && <div className="text-muted-foreground">{ban.isp}</div>}
                            </TooltipContent>
                          </UITooltip>
                        ) : (
                          <span className="text-muted-foreground/60">—</span>
                        )}
                      </TableCell>

                      <TableCell className="align-middle">
                        <Badge variant={ban.source === "auto" ? "secondary" : "outline"} size="sm">
                          {sourceLabels[ban.source] ?? ban.source}
                        </Badge>
                      </TableCell>

                      <TableCell className="align-middle">
                        <StatusBadge status={ban.status} />
                      </TableCell>

                      <TableCell className="align-middle">
                        <ModeBadge mode={ban.mode} defaultMode={defaultMode} />
                      </TableCell>

                      <TableCell className="align-middle text-sm">
                        {ban.reason ? (
                          <UITooltip>
                            <TooltipTrigger asChild>
                              <span className="block min-w-0 truncate text-foreground/90">{ban.reason}</span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="max-w-sm text-xs">
                              {ban.reason}
                            </TooltipContent>
                          </UITooltip>
                        ) : (
                          <span className="text-muted-foreground/60">—</span>
                        )}
                      </TableCell>

                      <TableCell className="text-right align-middle text-xs tabular-nums text-muted-foreground">
                        {formatDuration(ban.duration)}
                      </TableCell>

                      <TableCell className="text-right align-middle text-xs tabular-nums">
                        {ban.status === "active" ? (
                          <span className="font-medium text-amber-600 dark:text-amber-400">
                            {formatRemaining(ban.expiresAt)}
                          </span>
                        ) : (
                          <span className="text-muted-foreground/60">—</span>
                        )}
                      </TableCell>

                      <TableCell className="align-middle text-xs tabular-nums text-muted-foreground">
                        {formatTime(ban.createdAt)}
                      </TableCell>

                      <TableCell className="pr-4 text-right align-middle">
                        {ban.status === "active" ? (
                          <UITooltip>
                            <TooltipTrigger asChild>
                              <Button
                                size="icon"
                                variant="ghost"
                                className="size-8 text-muted-foreground hover:text-destructive"
                                onClick={() => setShowUnbanDialog(ban)}
                              >
                                <Unlock className="size-3.5" />
                                <span className="sr-only">解封</span>
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent side="left" className="text-xs">解封</TooltipContent>
                          </UITooltip>
                        ) : (
                          <span className="text-muted-foreground/40">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            {/* 分页 */}
            <div className="flex items-center justify-between border-t border-border bg-muted/20 px-4 py-2.5">
              <span className="text-xs tabular-nums text-muted-foreground">
                共 <span className="font-medium text-foreground">{banData.total.toLocaleString()}</span> 条 · 第{" "}
                <span className="font-medium text-foreground">{banData.page}</span> / {banData.totalPages || 1} 页
              </span>
              <div className="flex items-center gap-1">
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7"
                  disabled={banData.page <= 1}
                  onClick={() => setPage(banData.page - 1)}
                  aria-label="上一页"
                >
                  <ChevronLeft className="size-3.5" />
                </Button>
                <span className="min-w-8 text-center text-xs tabular-nums text-foreground">{banData.page}</span>
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7"
                  disabled={banData.page >= banData.totalPages}
                  onClick={() => setPage(banData.page + 1)}
                  aria-label="下一页"
                >
                  <ChevronRight className="size-3.5" />
                </Button>
              </div>
            </div>
          </Card>
        )}

        {/* 封禁弹窗 */}
        <Dialog open={showBanDialog} onOpenChange={setShowBanDialog}>
          <DialogContent className="max-w-md p-0 gap-0 overflow-hidden">
            {/* 头部 */}
            <DialogHeader className="flex flex-row items-start gap-3 border-b border-border bg-muted/30 px-5 py-4 space-y-0">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-destructive/10 text-destructive">
                <Ban className="size-4" />
              </div>
              <div className="flex-1 space-y-0.5">
                <DialogTitle className="text-sm font-semibold">封禁 IP</DialogTitle>
                <DialogDescription className="text-xs leading-relaxed">
                  封禁后该 IP 的所有请求将按选定模式拦截
                </DialogDescription>
              </div>
            </DialogHeader>

            {/* 正文 */}
            <div className="max-h-[calc(100vh-220px)] overflow-y-auto px-5 py-4">
              <div className="space-y-3.5">
                <FormRow
                  label="IP 地址"
                  required
                >
                  <Input
                    placeholder="如 1.2.3.4 / 2001:db8::1"
                    className="h-9 font-mono text-sm"
                    value={banIP}
                    onChange={(e) => setBanIP(e.target.value)}
                    autoFocus
                  />
                </FormRow>

                <FormRow label="封禁原因">
                  <Input
                    placeholder="可选，将写入审计日志"
                    className="h-9 text-sm"
                    value={banReason}
                    onChange={(e) => setBanReason(e.target.value)}
                  />
                </FormRow>

                <div className="grid grid-cols-2 gap-3">
                  <FormRow label="封禁时长">
                    <Select value={String(banDuration)} onValueChange={(v) => setBanDuration(Number(v))}>
                      <SelectTrigger className="h-9 text-sm"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {durationOptions.map((opt) => (
                          <SelectItem key={opt.value} value={String(opt.value)} className="text-sm">{opt.label}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </FormRow>

                  <FormRow label="响应模式">
                    <Select
                      value={banMode === "__default__" ? "__default__" : String(banMode)}
                      onValueChange={(v) => setBanMode((v === "__default__" ? "__default__" : (v as IPBanMode)))}
                    >
                      <SelectTrigger className="h-9 text-sm"><SelectValue /></SelectTrigger>
                      <SelectContent className="max-h-72">
                        <SelectItem value="__default__" className="text-sm">
                          平台默认 · {banModeLabels[defaultMode] ?? defaultMode}
                        </SelectItem>
                        {modes.map((m) => (
                          <SelectItem key={m.value} value={m.value} className="text-sm">
                            {m.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </FormRow>
                </div>

                {/* 当前模式描述（独立一行，不再塞进 Select Item） */}
                {(() => {
                  if (banMode === "__default__") {
                    return (
                      <p className="flex items-start gap-1.5 rounded-md border border-border/60 bg-muted/40 px-2.5 py-2 text-[11.5px] leading-relaxed text-muted-foreground">
                        <span className="mt-0.5 inline-block size-1.5 shrink-0 rounded-full bg-muted-foreground/40" />
                        沿用平台默认模式；可在「平台设置 / 防火墙」中修改全局默认
                      </p>
                    );
                  }
                  const desc = modes.find((m) => m.value === banMode)?.description;
                  if (!desc) return null;
                  return (
                    <p className="flex items-start gap-1.5 rounded-md border border-border/60 bg-muted/40 px-2.5 py-2 text-[11.5px] leading-relaxed text-muted-foreground">
                      <span className="mt-0.5 inline-block size-1.5 shrink-0 rounded-full bg-destructive/60" />
                      {desc}
                    </p>
                  );
                })()}

                {banMode === "tarpit" && (
                  <FormRow
                    label="Tarpit 延迟"
                    hint="0 = 使用平台默认，最大 30000"
                  >
                    <div className="flex items-center gap-2">
                      <Input
                        type="number"
                        min={0}
                        max={30000}
                        step={500}
                        className="h-9 text-sm"
                        placeholder="5000"
                        value={banTarpitMs || ""}
                        onChange={(e) => setBanTarpitMs(Number(e.target.value) || 0)}
                      />
                      <span className="text-xs text-muted-foreground">毫秒</span>
                    </div>
                  </FormRow>
                )}
              </div>
            </div>

            {/* 底部操作 */}
            <DialogFooter className="border-t border-border bg-muted/30 px-5 py-3">
              <Button variant="ghost" size="sm" onClick={() => setShowBanDialog(false)}>取消</Button>
              <Button variant="destructive" size="sm" disabled={createMutation.isPending} onClick={handleBan}>
                {createMutation.isPending ? <><Loader2 className="mr-1 size-3 animate-spin" />封禁中...</> : "确认封禁"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 解封确认弹窗 */}
        <Dialog open={showUnbanDialog !== null} onOpenChange={(open) => !open && setShowUnbanDialog(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle className="text-sm">确认解封</DialogTitle>
              <DialogDescription>
                确定要解封 IP <span className="font-mono font-semibold text-foreground">{showUnbanDialog?.ip}</span> 吗？解封后该 IP 将可以正常访问。
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="ghost" size="sm" onClick={() => setShowUnbanDialog(null)}>取消</Button>
              <Button size="sm" disabled={deleteMutation.isPending} onClick={handleUnban}>
                {deleteMutation.isPending ? <><Loader2 className="mr-1 size-3 animate-spin" />解封中...</> : "确认解封"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}

// ── 子组件 ──────────────────────────

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <label className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</label>
      {children}
    </div>
  );
}

function FormRow({
  label,
  required,
  hint,
  children
}: {
  label: string;
  required?: boolean;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[12px] font-medium">
          {label}
          {required && <span className="ml-1 text-destructive">*</span>}
        </Label>
        {hint && <span className="text-[10.5px] text-muted-foreground/80">{hint}</span>}
      </div>
      {children}
    </div>
  );
}

function ModeBadge({ mode, defaultMode }: { mode: string; defaultMode: string }) {
  const isDefault = !mode;
  const effective = mode || defaultMode || "forbidden";
  const label = banModeLabels[effective] ?? effective;
  const variant: "outline" | "secondary" | "danger" | "warning" =
    effective === "silent_drop" || effective === "connection_reset"
      ? "danger"
      : effective === "tarpit" || effective === "rate_choke"
        ? "warning"
        : effective === "stealth_404" || effective === "teapot"
          ? "secondary"
          : "outline";
  return (
    <UITooltip>
      <TooltipTrigger asChild>
        <Badge variant={variant} size="sm">
          {label}
          {isDefault && <span className="ml-1 text-muted-foreground/70">·默认</span>}
        </Badge>
      </TooltipTrigger>
      <TooltipContent side="top" className="text-xs">
        {isDefault ? `沿用平台默认（${banModeLabels[defaultMode] ?? defaultMode}）` : label}
      </TooltipContent>
    </UITooltip>
  );
}

function StatusBadge({ status }: { status: string }) {
  const cfg = statusConfig[status] ?? { label: status, variant: "outline" as const };
  return (
    <Badge variant={cfg.variant} size="sm" className="gap-1">
      <span
        className={cn(
          "size-1.5 rounded-full",
          cfg.variant === "danger" && "bg-red-500",
          cfg.variant === "secondary" && "bg-muted-foreground/60",
          cfg.variant === "outline" && "bg-muted-foreground/40",
          cfg.variant === "success" && "bg-emerald-500"
        )}
      />
      {cfg.label}
    </Badge>
  );
}
