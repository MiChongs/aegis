"use client";

import { useMemo, useState, type ReactNode } from "react";
import { AlertTriangle, Globe2, Loader2, MoreHorizontal, Pencil, Plus, Power, PowerOff, RefreshCw, Search, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { CountryFlag } from "@/components/ui/country-flag";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CardEmpty } from "@/components/overview/overview-kit";
import { FieldError, Segmented } from "@/components/traffic/traffic-kit";
import { useDeleteGeoBanMutation, useGeoBansQuery, useToggleGeoBanMutation, useUpsertGeoBanMutation } from "@/lib/admin-hooks";
import type { GeoBanEntry, GeoBanScope, IPBanMode } from "@/lib/api/types";
import { formatDateTime, formatRelative, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { banErrorMessage, useGeoBanBatchMutation } from "@/lib/firewall-ban-hooks";
import { cn } from "@/lib/utils";
import { countryName } from "../overview/overview-parts";
import { FormBlock, ModeSelect } from "./ban-fields";
import { formatSpan, GEO_SCOPE_META, geoScopeLabel, useBanModes, useNow, type BanModes } from "./ban-format";

/**
 * 地域规则：国家 / 省州 / 城市 / ASN / 运营商。规则按（作用域, 值）唯一，同值再保存即覆盖；
 * 私网地址不走地域规则。启停与删除都能批量。列表一次全取（规则数量级是几十条），筛选在前端做。
 */

const SCOPES: GeoBanScope[] = ["country", "region", "city", "asn", "isp"];
type ScopeFilter = GeoBanScope | "all";
type StateFilter = "all" | "enabled" | "disabled" | "expired";

const COMMON_COUNTRIES = ["CN", "US", "RU", "KR", "JP", "VN", "IN", "BR", "DE", "FR", "GB", "HK", "TW", "SG", "NL", "CA", "AU", "IR", "KP", "TH"] as const;

const GEO_GRID =
  "@4xl:grid @4xl:grid-cols-[1rem_4.5rem_minmax(0,1fr)_7rem_minmax(0,1fr)_4.5rem_5rem_5.5rem_2.25rem_1.75rem] @4xl:items-center @4xl:gap-3";

function isExpired(rule: GeoBanEntry, now: number) {
  return Boolean(rule.expiresAt && Date.parse(rule.expiresAt) <= now);
}

function normalizeScopeValue(scope: GeoBanScope, raw: string) {
  const value = raw.trim();
  if (scope === "country") return value.toUpperCase();
  if (scope === "asn") {
    const digits = value.replace(/^as/i, "");
    return /^\d+$/.test(digits) ? `AS${digits}` : value.toUpperCase();
  }
  return value;
}

type EditorTarget = { key: number; rule: GeoBanEntry | null };

export function GeoRules({ canBan }: { canBan: boolean }) {
  const now = useNow();
  const modes = useBanModes();
  const query = useGeoBansQuery();
  const toggle = useToggleGeoBanMutation();
  const batch = useGeoBanBatchMutation();
  const remove = useDeleteGeoBanMutation();
  const rules = useMemo(() => query.data ?? [], [query.data]);

  const [keyword, setKeyword] = useState("");
  const [scope, setScope] = useState<ScopeFilter>("all");
  const [stateFilter, setStateFilter] = useState<StateFilter>("all");
  const [selected, setSelected] = useState<Set<number>>(() => new Set());
  const [editor, setEditor] = useState<EditorTarget | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<GeoBanEntry[] | null>(null);

  const changeFilter = (apply: () => void) => {
    apply();
    setSelected(new Set());
  };

  const visible = useMemo(() => {
    const needle = keyword.trim().toLowerCase();
    return rules.filter((rule) => {
      if (scope !== "all" && rule.scopeType !== scope) return false;
      const expired = isExpired(rule, now);
      if (stateFilter === "enabled" && (!rule.enabled || expired)) return false;
      if (stateFilter === "disabled" && rule.enabled) return false;
      if (stateFilter === "expired" && !expired) return false;
      if (!needle) return true;
      const hay = [rule.scopeValue, rule.reason, rule.scopeType === "country" ? countryName(rule.scopeValue) : ""].join(" ").toLowerCase();
      return hay.includes(needle);
    });
  }, [rules, keyword, scope, stateFilter, now]);

  const summary = useMemo(() => {
    let enabled = 0;
    let matches = 0;
    for (const rule of rules) {
      if (rule.enabled && !isExpired(rule, now)) enabled += 1;
      matches += rule.matchCount;
    }
    return { enabled, disabled: rules.length - enabled, matches };
  }, [rules, now]);

  const selectedRules = visible.filter((rule) => selected.has(rule.id));
  const allVisible = visible.length > 0 && selectedRules.length === visible.length;

  const toggleOne = (id: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAll = () => setSelected(allVisible ? new Set() : new Set(visible.map((rule) => rule.id)));

  const runBatch = (action: "enable" | "disable" | "delete", ids: number[]) =>
    batch.mutate(
      { ids, action },
      {
        onSuccess: () => {
          setSelected(new Set());
          setConfirmDelete(null);
        }
      }
    );

  const deleteRules = (list: GeoBanEntry[]) => {
    if (list.length === 1) {
      remove.mutate(list[0]!.id, {
        onSuccess: () => {
          toast.success("已删除规则");
          setConfirmDelete(null);
          setSelected((prev) => {
            const next = new Set(prev);
            next.delete(list[0]!.id);
            return next;
          });
        },
        onError: (error) => toast.error(banErrorMessage(error, "删除失败"))
      });
      return;
    }
    runBatch(
      "delete",
      list.map((rule) => rule.id)
    );
  };

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <Card className="min-w-0 gap-0 overflow-hidden py-0">
        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-0 flex-1 basis-48">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(event) => changeFilter(() => setKeyword(event.target.value))}
              placeholder="值 / 国家名 / 原因"
              className="h-9 pl-8"
              aria-label="搜索规则"
            />
          </div>
          <Segmented
            value={stateFilter}
            onChange={(next) => changeFilter(() => setStateFilter(next))}
            ariaLabel="规则状态"
            options={[
              { value: "all", label: "全部" },
              { value: "enabled", label: "生效中" },
              { value: "disabled", label: "已停用" },
              { value: "expired", label: "已过期" }
            ]}
          />
          <Button size="icon" variant="ghost" aria-label="刷新" onClick={() => void query.refetch()}>
            <RefreshCw className={cn(query.isFetching && "animate-spin")} />
          </Button>
          {canBan ? (
            <Button onClick={() => setEditor({ key: Date.now(), rule: null })}>
              <Plus />
              新增规则
            </Button>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
          <Segmented
            value={scope}
            onChange={(next) => changeFilter(() => setScope(next))}
            ariaLabel="作用域"
            options={[{ value: "all" as ScopeFilter, label: "全部作用域" }, ...SCOPES.map((value) => ({ value: value as ScopeFilter, label: GEO_SCOPE_META[value]!.label }))]}
          />
          <span className="ml-auto text-[11px] text-muted-foreground tabular-nums">
            生效 <span className="font-medium text-foreground">{summary.enabled}</span> · 未生效 {summary.disabled} · 累计命中 {summary.matches.toLocaleString("zh-CN")}
          </span>
        </div>

        {visible.length > 0 ? (
          <div className={cn("flex items-center gap-3 border-b bg-muted/30 px-4 py-2 text-[11px] font-medium text-muted-foreground", GEO_GRID)}>
            <Checkbox
              checked={allVisible ? true : selectedRules.length > 0 ? "indeterminate" : false}
              onCheckedChange={toggleAll}
              aria-label="全选"
              className="bg-background"
            />
            <span className="@4xl:hidden">{selectedRules.length > 0 ? `已选 ${selectedRules.length} 条` : "全选"}</span>
            <span className="hidden @4xl:block">作用域</span>
            <span className="hidden @4xl:block">值</span>
            <span className="hidden @4xl:block">响应模式</span>
            <span className="hidden @4xl:block">原因</span>
            <span className="hidden text-right @4xl:block">命中</span>
            <span className="hidden @4xl:block">最近命中</span>
            <span className="hidden @4xl:block">有效期</span>
            <span className="hidden @4xl:block">启用</span>
            <span className="hidden @4xl:block" />
          </div>
        ) : null}

        {query.isError && !query.data ? (
          <div className="p-4">
            <CardEmpty icon={AlertTriangle} title={banErrorMessage(query.error, "地域规则读取失败")} />
          </div>
        ) : !query.data ? (
          <div className="flex flex-col">
            {Array.from({ length: 5 }).map((_, index) => (
              <div key={index} className="border-b px-4 py-3 last:border-b-0">
                <Skeleton className="h-8 w-full rounded-md" />
              </div>
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="flex flex-col items-center gap-2 p-6">
            <CardEmpty
              icon={Globe2}
              title={rules.length === 0 ? "还没有地域规则" : "没有符合条件的规则"}
              description={rules.length === 0 ? "按国家、省份、城市、ASN 或运营商整片封禁。" : undefined}
            />
            {rules.length === 0 && canBan ? (
              <Button size="sm" onClick={() => setEditor({ key: Date.now(), rule: null })}>
                <Plus />
                新增规则
              </Button>
            ) : null}
          </div>
        ) : (
          <div className="flex flex-col">
            {visible.map((rule) => (
              <GeoRow
                key={rule.id}
                rule={rule}
                now={now}
                modes={modes}
                canBan={canBan}
                selected={selected.has(rule.id)}
                toggling={toggle.isPending && toggle.variables?.id === rule.id}
                onSelect={() => toggleOne(rule.id)}
                onToggle={(enabled) =>
                  toggle.mutate(
                    { id: rule.id, enabled },
                    {
                      onSuccess: () => toast.success(enabled ? "已启用规则" : "已停用规则"),
                      onError: (error) => toast.error(banErrorMessage(error, "切换失败"))
                    }
                  )
                }
                onEdit={() => setEditor({ key: Date.now(), rule })}
                onDelete={() => setConfirmDelete([rule])}
              />
            ))}
          </div>
        )}
      </Card>

      {selectedRules.length > 0 ? (
        <div
          role="toolbar"
          aria-label="批量操作"
          className="sticky bottom-4 z-20 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-2xl border bg-card/95 px-3 py-2.5 shadow-lg backdrop-blur supports-[backdrop-filter]:bg-card/85 sm:px-4"
        >
          <div className="flex items-center gap-1.5">
            <Button size="icon-xs" variant="ghost" aria-label="取消选择" onClick={() => setSelected(new Set())}>
              <X />
            </Button>
            <span className="text-sm font-medium tabular-nums">已选 {selectedRules.length} 条</span>
          </div>
          {canBan ? (
            <div className="ml-auto flex flex-wrap items-center gap-1.5">
              <Button
                size="sm"
                variant="outline"
                disabled={batch.isPending}
                onClick={() =>
                  runBatch(
                    "enable",
                    selectedRules.map((rule) => rule.id)
                  )
                }
              >
                <Power />
                启用
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={batch.isPending}
                onClick={() =>
                  runBatch(
                    "disable",
                    selectedRules.map((rule) => rule.id)
                  )
                }
              >
                <PowerOff />
                停用
              </Button>
              <Button size="sm" variant="destructive" disabled={batch.isPending} onClick={() => setConfirmDelete(selectedRules)}>
                <Trash2 />
                删除
              </Button>
            </div>
          ) : (
            <span className="ml-auto text-xs text-muted-foreground">没有修改地域规则的权限</span>
          )}
        </div>
      ) : null}

      <GeoRuleEditor target={editor} rules={rules} modes={modes} onClose={() => setEditor(null)} />

      <AlertDialog open={confirmDelete !== null} onOpenChange={(open) => !open && setConfirmDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmDelete && confirmDelete.length > 1 ? `删除 ${confirmDelete.length} 条地域规则` : "删除地域规则"}</AlertDialogTitle>
            <AlertDialogDescription>删除后立即不再拦截，累计命中数随之清除，无法恢复。只想暂时放行可以改为停用。</AlertDialogDescription>
          </AlertDialogHeader>
          {confirmDelete ? (
            <div className="flex max-h-40 flex-wrap gap-1 overflow-y-auto">
              {confirmDelete.slice(0, 30).map((rule) => (
                <span key={rule.id} className="inline-flex h-6 items-center gap-1 rounded-md bg-muted px-1.5 text-[11px]">
                  <span className="text-muted-foreground">{geoScopeLabel(rule.scopeType)}</span>
                  <span className="font-mono">{rule.scopeValue}</span>
                </span>
              ))}
              {confirmDelete.length > 30 ? <span className="text-[11px] text-muted-foreground">等 {confirmDelete.length} 条</span> : null}
            </div>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={batch.isPending || remove.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={(event) => {
                event.preventDefault();
                if (confirmDelete) deleteRules(confirmDelete);
              }}
            >
              {batch.isPending || remove.isPending ? <Loader2 className="animate-spin" /> : null}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/* ── 行 ── */

function GeoRow({
  rule,
  now,
  modes,
  canBan,
  selected,
  toggling,
  onSelect,
  onToggle,
  onEdit,
  onDelete
}: {
  rule: GeoBanEntry;
  now: number;
  modes: BanModes;
  canBan: boolean;
  selected: boolean;
  toggling: boolean;
  onSelect: () => void;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const expired = isExpired(rule, now);
  const left = rule.expiresAt ? Date.parse(rule.expiresAt) - now : null;
  const expiry = !rule.expiresAt ? (
    <span className="text-xs text-muted-foreground">永久</span>
  ) : expired ? (
    <span className="text-xs text-muted-foreground" title={`到期于 ${formatDateTime(rule.expiresAt)}`}>
      已过期
    </span>
  ) : (
    <span
      className={cn("text-xs tabular-nums", left !== null && left <= 86_400_000 && "text-amber-700 dark:text-amber-400")}
      title={`到期于 ${formatDateTime(rule.expiresAt)}`}
    >
      剩 {formatSpan(left ?? 0)}
    </span>
  );
  const mode = (
    <span
      title={modes.label(rule.mode)}
      className={cn(
        "inline-flex h-5 max-w-full items-center truncate rounded-md border px-1.5 text-[11px] whitespace-nowrap",
        rule.mode ? "border-border text-foreground/85" : "border-dashed text-muted-foreground"
      )}
    >
      {modes.short(rule.mode)}
    </span>
  );
  const lastMatch = rule.lastMatchAt ? (
    <span className="text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(rule.lastMatchAt)}>
      {formatRelative(rule.lastMatchAt, "—", now)}
    </span>
  ) : (
    <span className="text-[11px] text-muted-foreground">从未命中</span>
  );

  return (
    <div
      className={cn(
        "flex min-w-0 items-start gap-3 border-b px-4 py-3 transition-colors last:border-b-0 hover:bg-muted/40",
        selected && "bg-primary/5 hover:bg-primary/8",
        (!rule.enabled || expired) && "text-muted-foreground",
        GEO_GRID,
        "@4xl:py-2"
      )}
    >
      <span className="flex h-5 items-center @4xl:h-auto">
        <Checkbox checked={selected} onCheckedChange={onSelect} aria-label={`选择 ${rule.scopeValue}`} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5 @4xl:contents">
        <div className="flex min-w-0 items-center gap-2 @4xl:contents">
          <span className="inline-flex h-5 w-fit shrink-0 items-center rounded-md bg-muted px-1.5 text-[11px] font-medium text-foreground/80">{geoScopeLabel(rule.scopeType)}</span>
          <span className="flex min-w-0 items-center gap-1.5">
            {rule.scopeType === "country" ? <CountryFlag code={rule.scopeValue} size={14} /> : null}
            <span className={cn("truncate font-mono text-[13px] font-medium", rule.enabled && !expired && "text-foreground")} title={rule.scopeValue}>
              {rule.scopeValue}
            </span>
            {rule.scopeType === "country" ? <span className="shrink-0 truncate text-xs text-muted-foreground">{countryName(rule.scopeValue)}</span> : null}
          </span>
          <span className="ml-auto flex shrink-0 items-center @4xl:hidden">
            <Switch checked={rule.enabled} disabled={!canBan || toggling} onCheckedChange={onToggle} aria-label={rule.enabled ? "停用规则" : "启用规则"} />
          </span>
        </div>
        <span className="hidden min-w-0 @4xl:flex">{mode}</span>
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 @4xl:hidden">
          {mode}
          <span className="text-[11px] tabular-nums">命中 {rule.matchCount.toLocaleString("zh-CN")}</span>
          {lastMatch}
          <span className="ml-auto">{expiry}</span>
        </span>
        {rule.reason ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="min-w-0 cursor-default truncate text-xs">{rule.reason}</span>
            </TooltipTrigger>
            <TooltipContent side="top" align="start" className="max-w-sm text-xs">
              {rule.reason}
            </TooltipContent>
          </Tooltip>
        ) : (
          <span className="hidden text-xs text-muted-foreground @4xl:block">—</span>
        )}
        <span className="hidden text-right font-data text-xs tabular-nums @4xl:block">{rule.matchCount.toLocaleString("zh-CN")}</span>
        <span className="hidden @4xl:block">{lastMatch}</span>
        <span className="hidden @4xl:block">{expiry}</span>
        <span className="hidden @4xl:flex">
          <Switch checked={rule.enabled} disabled={!canBan || toggling} onCheckedChange={onToggle} aria-label={rule.enabled ? "停用规则" : "启用规则"} />
        </span>
      </div>
      {canBan ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon-xs" variant="ghost" aria-label="更多操作" className="-mr-1 shrink-0 text-muted-foreground">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-36">
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil />
              编辑
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 />
              删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <span className="hidden @4xl:block" />
      )}
    </div>
  );
}

/* ── 编辑器 ── */

const EXPIRY_PRESETS = [
  { value: "7", label: "7 天", days: 7 },
  { value: "30", label: "30 天", days: 30 },
  { value: "90", label: "90 天", days: 90 }
] as const;

function GeoRuleEditor({ target, rules, modes, onClose }: { target: EditorTarget | null; rules: GeoBanEntry[]; modes: BanModes; onClose: () => void }) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(820px,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-xl">
        {target ? <GeoEditorBody key={target.key} rule={target.rule} rules={rules} modes={modes} onClose={onClose} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function GeoEditorBody({ rule, rules, modes, onClose }: { rule: GeoBanEntry | null; rules: GeoBanEntry[]; modes: BanModes; onClose: () => void }) {
  const editing = rule !== null;
  const upsert = useUpsertGeoBanMutation();
  const [scopeType, setScopeType] = useState<GeoBanScope>(rule?.scopeType ?? "country");
  const [scopeValue, setScopeValue] = useState(rule?.scopeValue ?? "");
  const [mode, setMode] = useState<string>(rule?.mode ?? "");
  const [reason, setReason] = useState(rule?.reason ?? "");
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [expiryChoice, setExpiryChoice] = useState<string>(rule?.expiresAt ? "custom" : "never");
  const [expiresAt, setExpiresAt] = useState(rule?.expiresAt ? toLocalInputValue(rule.expiresAt) : "");
  const [error, setError] = useState<string | null>(null);

  const value = normalizeScopeValue(scopeType, scopeValue);
  const meta = GEO_SCOPE_META[scopeType]!;
  const duplicate = !editing && value ? rules.find((item) => item.scopeType === scopeType && item.scopeValue.toLowerCase() === value.toLowerCase()) : undefined;
  const countryInvalid = scopeType === "country" && value !== "" && !/^[A-Z]{2}$/.test(value);

  const pickExpiry = (choice: string) => {
    setExpiryChoice(choice);
    const preset = EXPIRY_PRESETS.find((item) => item.value === choice);
    if (preset) setExpiresAt(toLocalInputValue(Date.now() + preset.days * 86_400_000));
    if (choice === "custom" && !expiresAt) setExpiresAt(toLocalInputValue(Date.now() + 7 * 86_400_000));
  };

  const save = async () => {
    setError(null);
    if (!value) {
      setError("请填写作用域值");
      return;
    }
    if (countryInvalid) {
      setError("国家请填两位 ISO 代码，如 CN");
      return;
    }
    let expiry: string | undefined;
    if (expiryChoice !== "never") {
      expiry = fromLocalInputValue(expiresAt);
      if (!expiry) {
        setError("请选择到期时间");
        return;
      }
      if (Date.parse(expiry) <= Date.now()) {
        setError("到期时间早于现在");
        return;
      }
    }
    try {
      await upsert.mutateAsync({ scopeType, scopeValue: value, mode: mode as IPBanMode, reason: reason.trim(), enabled, expiresAt: expiry });
      toast.success(editing ? "已保存规则" : duplicate ? "已覆盖原有规则" : "已新增规则");
      onClose();
    } catch (err) {
      setError(banErrorMessage(err, "保存失败"));
    }
  };

  return (
    <>
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>{editing ? "编辑地域规则" : "新增地域规则"}</DialogTitle>
        <DialogDescription>按归属地或网络整片封禁；私网地址不受地域规则影响。</DialogDescription>
      </DialogHeader>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        <FormBlock label="作用域" hint={editing ? "已有规则不能改作用域" : undefined}>
          <Segmented
            value={scopeType}
            onChange={(next) => {
              if (!editing) setScopeType(next);
            }}
            ariaLabel="作用域"
            className={cn(editing && "pointer-events-none opacity-60")}
            options={SCOPES.map((scope) => ({ value: scope, label: GEO_SCOPE_META[scope]!.label }))}
          />
          <p className="text-[11px] leading-4 text-muted-foreground">{meta.hint}</p>
        </FormBlock>

        <FormBlock label="值" htmlFor="geo-value">
          <Input
            id="geo-value"
            value={scopeValue}
            disabled={editing}
            onChange={(event) => setScopeValue(event.target.value)}
            placeholder={meta.placeholder}
            className="font-mono"
            spellCheck={false}
          />
          {scopeType === "country" && value && !countryInvalid ? (
            <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
              <CountryFlag code={value} size={12} />
              {countryName(value)}
            </span>
          ) : null}
          {scopeType === "country" && !editing ? (
            <div className="flex flex-wrap gap-1">
              {COMMON_COUNTRIES.map((code) => (
                <button
                  key={code}
                  type="button"
                  onClick={() => setScopeValue(code)}
                  className={cn(
                    "inline-flex h-6 items-center gap-1 rounded-md border px-1.5 text-[11px] transition-colors hover:bg-accent",
                    value === code ? "border-primary/40 bg-primary/10 text-primary" : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <CountryFlag code={code} size={12} />
                  {countryName(code)}
                </button>
              ))}
            </div>
          ) : null}
          {duplicate ? (
            <p className="flex items-center gap-1.5 text-[11px] text-amber-700 dark:text-amber-400">
              <AlertTriangle className="size-3.5 shrink-0" />
              已有同一条规则（{duplicate.enabled ? "启用中" : "已停用"}），保存会覆盖它的设置。
            </p>
          ) : null}
        </FormBlock>

        <FormBlock label="响应模式">
          <ModeSelect value={mode} onChange={setMode} modes={modes} />
        </FormBlock>

        <FormBlock label="原因" htmlFor="geo-reason">
          <Input id="geo-reason" value={reason} maxLength={200} onChange={(event) => setReason(event.target.value)} placeholder="可选，例如：攻击来源集中" />
        </FormBlock>

        <FormBlock label="有效期">
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={expiryChoice}
            onValueChange={(next) => next && pickExpiry(next)}
            className="max-w-full flex-wrap"
            aria-label="有效期"
          >
            <ToggleGroupItem value="never" className="px-2.5 text-xs">
              永久
            </ToggleGroupItem>
            {EXPIRY_PRESETS.map((item) => (
              <ToggleGroupItem key={item.value} value={item.value} className="px-2.5 text-xs">
                {item.label}
              </ToggleGroupItem>
            ))}
            <ToggleGroupItem value="custom" className="px-2.5 text-xs">
              指定时间
            </ToggleGroupItem>
          </ToggleGroup>
          {expiryChoice !== "never" ? (
            <Input
              type="datetime-local"
              value={expiresAt}
              onChange={(event) => {
                setExpiresAt(event.target.value);
                setExpiryChoice("custom");
              }}
              className="h-8 w-56 text-sm"
              aria-label="到期时间"
            />
          ) : null}
        </FormBlock>

        <ToggleRow title="启用" hint="停用的规则保留设置，但不拦截">
          <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="启用" />
        </ToggleRow>
      </div>
      <DialogFooter className="flex-col gap-2 border-t px-5 py-3 sm:flex-row sm:items-center">
        <FieldError message={error} className="min-w-0 flex-1 sm:truncate" />
        {!error ? <span className="hidden flex-1 sm:block" /> : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={upsert.isPending || !value} onClick={() => void save()}>
            {upsert.isPending ? <Loader2 className="animate-spin" /> : null}
            {editing ? "保存" : duplicate ? "覆盖" : "新增"}
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}

function ToggleRow({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex min-w-0 cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
      <span className="min-w-0">
        <span className="block text-sm">{title}</span>
        {hint ? <span className="block text-[11px] text-muted-foreground">{hint}</span> : null}
      </span>
      {children}
    </label>
  );
}
