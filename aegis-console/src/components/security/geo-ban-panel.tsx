"use client";

import { useMemo, useState } from "react";
import { Globe2, Loader2, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/ui/data-state";
import { SurfaceCard } from "@/components/ui/surface-card";
import { CountryFlag } from "@/components/ui/country-flag";
import {
  Tooltip as UITooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip";

import {
  useGeoBansQuery,
  useUpsertGeoBanMutation,
  useToggleGeoBanMutation,
  useDeleteGeoBanMutation,
  useIPBanModesQuery
} from "@/lib/admin-hooks";
import type { GeoBanEntry, GeoBanScope, IPBanMode } from "@/lib/api/types";

const scopeLabels: Record<GeoBanScope, string> = {
  country: "国家",
  region: "省/州",
  city: "城市",
  asn: "ASN",
  isp: "ISP"
};

const scopePlaceholders: Record<GeoBanScope, string> = {
  country: "ISO 2 位代码，如 CN / US / RU",
  region: "格式 国-省，如 CN-BJ / US-CA",
  city: "城市名称，如 Shanghai",
  asn: "AS 编号，如 AS15169",
  isp: "ISP 子串匹配，如 Amazon / Alibaba"
};

const commonCountries = [
  { code: "CN", label: "中国" },
  { code: "US", label: "美国" },
  { code: "RU", label: "俄罗斯" },
  { code: "KR", label: "韩国" },
  { code: "JP", label: "日本" },
  { code: "VN", label: "越南" },
  { code: "IN", label: "印度" },
  { code: "BR", label: "巴西" },
  { code: "DE", label: "德国" },
  { code: "FR", label: "法国" },
  { code: "GB", label: "英国" },
  { code: "HK", label: "香港" },
  { code: "TW", label: "台湾" },
  { code: "SG", label: "新加坡" },
  { code: "NL", label: "荷兰" },
  { code: "CA", label: "加拿大" },
  { code: "AU", label: "澳大利亚" },
  { code: "IR", label: "伊朗" },
  { code: "KP", label: "朝鲜" },
  { code: "TH", label: "泰国" }
];

const banModeBadges: Record<string, string> = {
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

function fmtTime(iso?: string | null) {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString("zh-CN", {
      year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit"
    });
  } catch {
    return iso;
  }
}

function FormField({
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
        {hint && <span className="text-[10.5px] text-muted-foreground/80 truncate">{hint}</span>}
      </div>
      {children}
    </div>
  );
}

export function GeoBanPanel() {
  const q = useGeoBansQuery();
  const upsert = useUpsertGeoBanMutation();
  const toggleMut = useToggleGeoBanMutation();
  const del = useDeleteGeoBanMutation();
  const modesQ = useIPBanModesQuery();

  const data = q.data ?? [];
  const modes = modesQ.data?.modes ?? [];
  const defaultMode = modesQ.data?.default ?? "forbidden";

  const [showDialog, setShowDialog] = useState(false);
  const [editing, setEditing] = useState<GeoBanEntry | null>(null);
  const [scopeType, setScopeType] = useState<GeoBanScope>("country");
  const [scopeValue, setScopeValue] = useState("");
  const [mode, setMode] = useState<IPBanMode | "__default__">("__default__");
  const [reason, setReason] = useState("");
  const [enabled, setEnabled] = useState(true);

  const activeCount = useMemo(() => data.filter((r) => r.enabled).length, [data]);
  const countryCount = useMemo(() => data.filter((r) => r.scopeType === "country" && r.enabled).length, [data]);

  function openCreate() {
    setEditing(null);
    setScopeType("country");
    setScopeValue("");
    setMode("__default__");
    setReason("");
    setEnabled(true);
    setShowDialog(true);
  }

  function openEdit(row: GeoBanEntry) {
    setEditing(row);
    setScopeType(row.scopeType);
    setScopeValue(row.scopeValue);
    setMode(row.mode || "__default__");
    setReason(row.reason);
    setEnabled(row.enabled);
    setShowDialog(true);
  }

  async function handleSave() {
    if (!scopeValue.trim()) {
      toast.error("请输入作用域值");
      return;
    }
    try {
      await upsert.mutateAsync({
        scopeType,
        scopeValue: scopeValue.trim(),
        mode: mode === "__default__" ? "" : (mode as IPBanMode),
        reason: reason.trim(),
        enabled
      });
      toast.success(editing ? "已更新规则" : "已新增规则");
      setShowDialog(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "保存失败");
    }
  }

  async function handleToggle(row: GeoBanEntry, next: boolean) {
    try {
      await toggleMut.mutateAsync({ id: row.id, enabled: next });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "切换失败");
    }
  }

  async function handleDelete(row: GeoBanEntry) {
    if (!confirm(`确认删除规则「${scopeLabels[row.scopeType]} · ${row.scopeValue}」？`)) return;
    try {
      await del.mutateAsync(row.id);
      toast.success("已删除");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "删除失败");
    }
  }

  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-5">
        {/* 顶部操作栏 */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400">
              <Globe2 className="size-4" />
            </div>
            <div className="flex flex-col">
              <span className="text-sm font-semibold leading-tight">地域 / ASN 封禁</span>
              <span className="text-[11px] leading-tight text-muted-foreground">
                {activeCount > 0 ? (
                  <>
                    共 <span className="font-medium text-foreground">{activeCount}</span> 条生效中
                    {countryCount > 0 && <>（{countryCount} 个国家）</>}
                  </>
                ) : (
                  <>暂无生效中的地域封禁规则</>
                )}
                <span className="ml-2 border-l border-border pl-2 text-muted-foreground/80">
                  优先级：<span className="font-medium text-foreground">IP 精确封禁</span> → 地域规则
                </span>
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => q.refetch()} disabled={q.isFetching}>
              <RefreshCw className={cn("size-3.5", q.isFetching && "animate-spin")} />
              刷新
            </Button>
            <Button size="sm" className="h-8 gap-1.5" onClick={openCreate}>
              <Plus className="size-3.5" />
              新增规则
            </Button>
          </div>
        </div>

        {/* 列表 */}
        {q.isLoading ? (
          <SurfaceCard>
            <div className="space-y-3">
              {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}
            </div>
          </SurfaceCard>
        ) : data.length === 0 ? (
          <div className="space-y-3">
            <EmptyState
              title="暂无地域封禁规则"
              description="添加规则可按国家、省份、城市、ASN 或 ISP 封禁整片网络来源"
            />
            <div className="flex justify-center">
              <Button size="sm" onClick={openCreate}><Plus className="size-3.5" />新增规则</Button>
            </div>
          </div>
        ) : (
          <Card className="overflow-hidden">
            <div className="overflow-x-auto">
              <Table className="table-fixed">
                <colgroup>
                  <col className="w-[88px]" />
                  <col className="w-[200px]" />
                  <col className="w-[128px]" />
                  <col />
                  <col className="w-[96px]" />
                  <col className="w-[96px]" />
                  <col className="w-[160px]" />
                  <col className="w-[112px]" />
                </colgroup>
                <TableHeader>
                  <TableRow className="bg-muted/40 hover:bg-muted/40">
                    <TableHead className="pl-4">作用域</TableHead>
                    <TableHead>值</TableHead>
                    <TableHead>响应模式</TableHead>
                    <TableHead>原因</TableHead>
                    <TableHead className="text-right">命中次数</TableHead>
                    <TableHead className="text-center">启用</TableHead>
                    <TableHead>更新时间</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.map((row) => (
                    <TableRow key={row.id} className="group">
                      <TableCell className="pl-4">
                        <Badge variant="outline" size="sm">{scopeLabels[row.scopeType]}</Badge>
                      </TableCell>
                      <TableCell>
                        <span className="block truncate font-mono text-[13px] font-medium">
                          {row.scopeValue}
                        </span>
                      </TableCell>
                      <TableCell>
                        <Badge variant={row.mode ? "danger" : "secondary"} size="sm">
                          {row.mode ? banModeBadges[row.mode] ?? row.mode : `默认（${banModeBadges[defaultMode] ?? defaultMode}）`}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm">
                        {row.reason ? (
                          <UITooltip>
                            <TooltipTrigger asChild>
                              <span className="block truncate text-foreground/90">{row.reason}</span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="max-w-sm text-xs">{row.reason}</TooltipContent>
                          </UITooltip>
                        ) : (
                          <span className="text-muted-foreground/60">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">
                        {row.matchCount > 0 ? (
                          <span className="font-medium text-foreground">{row.matchCount.toLocaleString()}</span>
                        ) : (
                          <span className="text-muted-foreground/60">0</span>
                        )}
                      </TableCell>
                      <TableCell className="text-center">
                        <Switch
                          checked={row.enabled}
                          onCheckedChange={(v) => handleToggle(row, v)}
                          disabled={toggleMut.isPending}
                          aria-label={row.enabled ? "禁用规则" : "启用规则"}
                        />
                      </TableCell>
                      <TableCell className="text-xs tabular-nums text-muted-foreground">
                        {fmtTime(row.updatedAt)}
                      </TableCell>
                      <TableCell className="pr-4 text-right">
                        <div className="inline-flex items-center gap-1">
                          <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => openEdit(row)}>
                            编辑
                          </Button>
                          <UITooltip>
                            <TooltipTrigger asChild>
                              <Button
                                size="icon"
                                variant="ghost"
                                className="size-7 text-muted-foreground hover:text-destructive"
                                onClick={() => handleDelete(row)}
                              >
                                <Trash2 className="size-3.5" />
                                <span className="sr-only">删除</span>
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent side="left" className="text-xs">删除</TooltipContent>
                          </UITooltip>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </Card>
        )}

        {/* 新增 / 编辑 Dialog */}
        <Dialog open={showDialog} onOpenChange={setShowDialog}>
          <DialogContent className="max-w-lg p-0 gap-0 overflow-hidden">
            <DialogHeader className="flex flex-row items-start gap-3 border-b border-border bg-muted/30 px-5 py-4 space-y-0">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400">
                <Globe2 className="size-4" />
              </div>
              <div className="flex-1 space-y-0.5">
                <DialogTitle className="text-sm font-semibold">
                  {editing ? "编辑地域封禁" : "新增地域封禁"}
                </DialogTitle>
                <DialogDescription className="text-xs leading-relaxed">
                  按国家、省份、城市、ASN 或 ISP 封禁整片网络来源；IP 精确封禁优先生效
                </DialogDescription>
              </div>
            </DialogHeader>

            <div className="max-h-[calc(100vh-220px)] overflow-y-auto px-5 py-4">
              <div className="space-y-3.5">
                <div className="grid grid-cols-2 gap-3">
                  <FormField label="作用域" required>
                    <Select
                      value={scopeType}
                      onValueChange={(v) => setScopeType(v as GeoBanScope)}
                      disabled={!!editing}
                    >
                      <SelectTrigger className="h-9 text-sm"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="country" className="text-sm">国家（ISO 代码）</SelectItem>
                        <SelectItem value="region" className="text-sm">省 / 州</SelectItem>
                        <SelectItem value="city" className="text-sm">城市</SelectItem>
                        <SelectItem value="asn" className="text-sm">ASN</SelectItem>
                        <SelectItem value="isp" className="text-sm">ISP（子串）</SelectItem>
                      </SelectContent>
                    </Select>
                  </FormField>

                  <FormField label="响应模式">
                    <Select
                      value={mode === "__default__" ? "__default__" : String(mode)}
                      onValueChange={(v) => setMode(v === "__default__" ? "__default__" : (v as IPBanMode))}
                    >
                      <SelectTrigger className="h-9 text-sm"><SelectValue /></SelectTrigger>
                      <SelectContent className="max-h-72">
                        <SelectItem value="__default__" className="text-sm">
                          平台默认 · {banModeBadges[defaultMode] ?? defaultMode}
                        </SelectItem>
                        {modes.map((m) => (
                          <SelectItem key={m.value} value={m.value} className="text-sm">
                            {m.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </FormField>
                </div>

                <FormField label="作用域值" required hint={scopePlaceholders[scopeType]}>
                  <Input
                    className="h-9 font-mono text-sm"
                    value={scopeValue}
                    onChange={(e) => setScopeValue(e.target.value)}
                    placeholder={scopePlaceholders[scopeType]}
                    disabled={!!editing}
                  />
                </FormField>

                {scopeType === "country" && !editing && (
                  <div className="flex flex-wrap gap-1.5 rounded-lg border border-border/60 bg-muted/30 p-2">
                    {commonCountries.map((c) => (
                      <button
                        key={c.code}
                        type="button"
                        onClick={() => setScopeValue(c.code)}
                        className={cn(
                          "inline-flex items-center gap-1 rounded-md border border-border bg-background px-2 py-1 text-[11px] transition-colors",
                          "hover:border-primary/40 hover:bg-accent",
                          scopeValue === c.code && "border-primary/50 bg-primary/10 text-primary"
                        )}
                      >
                        <CountryFlag code={c.code} size={14} />
                        {c.label}
                      </button>
                    ))}
                  </div>
                )}

                {/* 模式描述 */}
                {(() => {
                  if (mode === "__default__") {
                    return (
                      <p className="flex items-start gap-1.5 rounded-md border border-border/60 bg-muted/40 px-2.5 py-2 text-[11.5px] leading-relaxed text-muted-foreground">
                        <span className="mt-0.5 inline-block size-1.5 shrink-0 rounded-full bg-muted-foreground/40" />
                        沿用平台默认响应模式
                      </p>
                    );
                  }
                  const desc = modes.find((m) => m.value === mode)?.description;
                  if (!desc) return null;
                  return (
                    <p className="flex items-start gap-1.5 rounded-md border border-border/60 bg-muted/40 px-2.5 py-2 text-[11.5px] leading-relaxed text-muted-foreground">
                      <span className="mt-0.5 inline-block size-1.5 shrink-0 rounded-full bg-amber-500/60" />
                      {desc}
                    </p>
                  );
                })()}

                <FormField label="备注原因">
                  <Input
                    className="h-9 text-sm"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="可选，将写入审计日志"
                  />
                </FormField>

                <label className="flex cursor-pointer items-center justify-between rounded-lg border border-border bg-muted/40 px-3 py-2.5">
                  <div className="flex flex-col gap-0.5">
                    <span className="text-[12px] font-medium">立即启用</span>
                    <span className="text-[10.5px] text-muted-foreground">关闭后可保留规则但不参与匹配</span>
                  </div>
                  <Switch checked={enabled} onCheckedChange={setEnabled} />
                </label>
              </div>
            </div>

            <DialogFooter className="border-t border-border bg-muted/30 px-5 py-3">
              <Button variant="ghost" size="sm" onClick={() => setShowDialog(false)}>取消</Button>
              <Button size="sm" onClick={handleSave} disabled={upsert.isPending}>
                {upsert.isPending ? <><Loader2 className="mr-1 size-3 animate-spin" />保存中...</> : (editing ? "保存更新" : "确认新增")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}
